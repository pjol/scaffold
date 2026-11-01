package wallets

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/notify"
	"scaffoldapp/backend/internal/webhooks"
)

// The indexer contract (FEATURE.md documents it for indexer authors):
//
//	POST /webhooks/wallets/indexer
//	X-Signature: sha256=<hex HMAC-SHA256 of the raw body with WALLETS_INDEXER_SECRET>
//	{"transfers":[{"chain_id":8453,"token_address":"0x…","tx_hash":"0x…","log_index":3,
//	  "block_number":123,"block_timestamp":1730000000,"from":"0x…","to":"0x…","amount":"1500000"}]}
//
// Transfers of tokens not in WALLETS_CHAINS, or between addresses nobody has
// linked, are ignored. Each (chain, tx, log index) is handled once; a replay
// only attaches owners who linked the address since.

type indexerPayload struct {
	Transfers []indexerTransfer `json:"transfers"`
}

type indexerTransfer struct {
	ChainID        int64           `json:"chain_id"`
	TokenAddress   string          `json:"token_address"`
	TxHash         string          `json:"tx_hash"`
	LogIndex       int             `json:"log_index"`
	BlockNumber    int64           `json:"block_number"`
	BlockTimestamp int64           `json:"block_timestamp"` // unix seconds
	From           string          `json:"from"`
	To             string          `json:"to"`
	Amount         json.RawMessage `json:"amount"` // base units: decimal string (or a JSON integer)
}

// validTransfer is a checked transfer with its token resolved.
type validTransfer struct {
	chain      *Chain
	token      *Token
	tokenAddr  Address
	txHash     string
	logIndex   int
	block      int64
	occurredAt time.Time
	from, to   Address
	amount     string
}

const (
	maxIndexerBatch = 500
	// notifyMaxAge keeps backfills and re-indexes from notifying about old
	// transfers; they are still recorded.
	notifyMaxAge = time.Hour
)

var txHashRe = regexp.MustCompile(`^0x[0-9a-fA-F]{64}$`)

// IndexerResult is the webhook response.
type IndexerResult struct {
	Received   int `json:"received"`
	Recorded   int `json:"recorded"`
	Duplicates int `json:"duplicates"`
	Ignored    int `json:"ignored"`
}

func (s *Service) indexerWebhook(w http.ResponseWriter, r *http.Request) {
	if s.cfg.indexerSecret == "" {
		httpx.Fail(w, r, httpx.NewError(http.StatusServiceUnavailable, "wallets_indexer_unconfigured", "The indexer webhook isn't set up on this server."))
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 2<<20))
	if err != nil {
		httpx.Fail(w, r, httpx.BadRequest("Unreadable body."))
		return
	}
	if !webhooks.VerifyHexHMAC(s.cfg.indexerSecret, r.Header.Get("X-Signature"), body) {
		httpx.Fail(w, r, httpx.NewError(http.StatusUnauthorized, "bad_signature", "Signature check failed."))
		return
	}
	var p indexerPayload
	if err := json.Unmarshal(body, &p); err != nil {
		httpx.Fail(w, r, httpx.BadRequest("Unexpected payload."))
		return
	}
	if len(p.Transfers) > maxIndexerBatch {
		httpx.Fail(w, r, httpx.BadRequest(fmt.Sprintf("Send at most %d transfers per request.", maxIndexerBatch)))
		return
	}
	ctx := context.WithoutCancel(r.Context())
	res := IndexerResult{Received: len(p.Transfers)}
	now := s.now()

	var valid []validTransfer
	for i, t := range p.Transfers {
		vt, known, err := s.checkTransfer(t, now)
		if err != nil {
			e := httpx.BadRequest(fmt.Sprintf("Transfer %d: %v.", i, err))
			e.Details = map[string]any{"index": i}
			httpx.Fail(w, r, e)
			return
		}
		if !known {
			res.Ignored++
			continue
		}
		valid = append(valid, vt)
	}

	// Skip transfers between addresses nobody has linked without touching
	// webhook_events, so a later replay can still record them.
	linked, err := s.linkedSet(ctx, valid)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	for _, vt := range valid {
		if !linked[vt.from] && !linked[vt.to] {
			res.Ignored++
			continue
		}
		var rec transferRow
		eventID := fmt.Sprintf("%d:%s:%d", vt.chain.ID, vt.txHash, vt.logIndex)
		dup, err := webhooks.Once(ctx, s.d.Pool, "wallets.indexer", eventID, func(tx pgx.Tx) error {
			var err error
			rec, err = upsertTransfer(ctx, tx, vt)
			return err
		})
		if err == nil && dup {
			// Already handled: attach anyone who linked an address since, quietly.
			_, err = upsertTransfer(ctx, s.d.Pool, vt)
		}
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		if dup {
			res.Duplicates++
			continue
		}
		res.Recorded++
		s.announce(ctx, vt, rec, now)
	}
	httpx.OK(w, res)
}

// checkTransfer validates one payload item. known=false means the chain or
// token isn't configured (ignored, not an error).
func (s *Service) checkTransfer(t indexerTransfer, now time.Time) (validTransfer, bool, error) {
	var vt validTransfer
	tokenAddr, err := ParseAddress(t.TokenAddress)
	if err != nil {
		return vt, false, fmt.Errorf("token_address: %v", err)
	}
	if !txHashRe.MatchString(t.TxHash) {
		return vt, false, fmt.Errorf("tx_hash must be 0x followed by 64 hex characters")
	}
	if t.LogIndex < 0 || t.BlockNumber < 0 {
		return vt, false, fmt.Errorf("log_index and block_number must be non-negative")
	}
	if t.BlockTimestamp <= 0 {
		return vt, false, fmt.Errorf("block_timestamp must be unix seconds")
	}
	occurred := time.Unix(t.BlockTimestamp, 0).UTC()
	if occurred.After(now.Add(5 * time.Minute)) {
		return vt, false, fmt.Errorf("block_timestamp is in the future (send seconds, not milliseconds)")
	}
	from, err := ParseAddress(t.From)
	if err != nil {
		return vt, false, fmt.Errorf("from: %v", err)
	}
	to, err := ParseAddress(t.To)
	if err != nil {
		return vt, false, fmt.Errorf("to: %v", err)
	}
	raw := string(bytes.TrimSpace(t.Amount))
	if strings.HasPrefix(raw, `"`) {
		if err := json.Unmarshal(t.Amount, &raw); err != nil {
			return vt, false, fmt.Errorf("amount must be a decimal string")
		}
	}
	amount, err := parseBaseUnits(raw)
	if err != nil {
		return vt, false, err
	}
	chain, token, ok := s.registry.Token(t.ChainID, tokenAddr)
	if !ok {
		return vt, false, nil
	}
	return validTransfer{
		chain: chain, token: token, tokenAddr: tokenAddr, txHash: strings.ToLower(t.TxHash),
		logIndex: t.LogIndex, block: t.BlockNumber, occurredAt: occurred, from: from, to: to,
		amount: amount.String(),
	}, true, nil
}

func (s *Service) linkedSet(ctx context.Context, list []validTransfer) (map[Address]bool, error) {
	if len(list) == 0 {
		return map[Address]bool{}, nil
	}
	addrs := make([]string, 0, 2*len(list))
	for _, vt := range list {
		addrs = append(addrs, vt.from.Lower(), vt.to.Lower())
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT address FROM wallets WHERE address = ANY($1)`, addrs)
	if err != nil {
		return nil, err
	}
	found, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	out := make(map[Address]bool, len(found))
	for _, a := range found {
		if addr, err := ParseAddress(a); err == nil {
			out[addr] = true
		}
	}
	return out, nil
}

// upsertTransfer writes a transfer with the current owners of both
// addresses. On a replay it only fills owners that were missing.
func upsertTransfer(ctx context.Context, q db.Querier, vt validTransfer) (transferRow, error) {
	rows, err := q.Query(ctx, `
		INSERT INTO wallet_transfers (chain_id, tx_hash, log_index, block_number, token_address, token_symbol,
		  token_decimals, from_address, to_address, amount, from_user_id, to_user_id, occurred_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::numeric,
		  (SELECT user_id FROM wallets WHERE address = $8), (SELECT user_id FROM wallets WHERE address = $9), $11)
		ON CONFLICT (chain_id, tx_hash, log_index) DO UPDATE SET
		  from_user_id = COALESCE(wallet_transfers.from_user_id, EXCLUDED.from_user_id),
		  to_user_id   = COALESCE(wallet_transfers.to_user_id, EXCLUDED.to_user_id)
		RETURNING `+transferCols,
		vt.chain.ID, vt.txHash, vt.logIndex, vt.block, vt.tokenAddr.Lower(), vt.token.Symbol, *vt.token.Decimals,
		vt.from.Lower(), vt.to.Lower(), vt.amount, vt.occurredAt)
	if err != nil {
		return transferRow{}, err
	}
	row, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByPos[transferRow])
	if err != nil {
		return transferRow{}, err
	}
	if row.FromUser == nil && row.ToUser == nil {
		// Both addresses were unlinked meanwhile: nobody needs this row.
		_, err = q.Exec(ctx, `DELETE FROM wallet_transfers WHERE id = $1`, row.ID)
	}
	return row, err
}

// announce publishes the transfer to each linked side and notifies the
// recipient of a recent incoming transfer. Best-effort, after commit.
func (s *Service) announce(ctx context.Context, vt validTransfer, row transferRow, now time.Time) {
	sent := map[uuid.UUID]bool{}
	for _, uid := range []*uuid.UUID{row.ToUser, row.FromUser} {
		if uid != nil && !sent[*uid] {
			sent[*uid] = true
			s.publish(ctx, *uid, EventTransfer, s.view(row, *uid))
		}
	}
	if row.ToUser == nil || (row.FromUser != nil && *row.FromUser == *row.ToUser) || now.Sub(vt.occurredAt) > notifyMaxAge {
		return
	}
	n := notify.Notification{
		UserID: *row.ToUser,
		Kind:   KindReceived,
		Title:  "Received " + formatUnits(vt.amount, *vt.token.Decimals) + " " + vt.token.Symbol,
		Body:   "On " + vt.chain.Name + ", to " + vt.to.Short() + ".",
		Link:   "/wallets",
		Data:   map[string]any{"transfer_id": row.ID, "chain_id": vt.chain.ID, "tx_hash": vt.txHash},
	}
	if err := s.d.Notify.Send(ctx, n); err != nil {
		slog.WarnContext(ctx, "wallets: notify failed", "kind", KindReceived, "err", err)
	}
}
