package wallets

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
)

// ---- account deletion and export -------------------------------------------------

// DeleteAccount unlinks every wallet and drops pending challenges. Transfers
// are copies of public chain data, not this app's financial records, so the
// user's link to them is removed and rows no other linked user needs are
// deleted. (An app that moves money through these wallets keeps its own
// ledger, e.g. in the payments feature, and retains that.)
func (s *Service) DeleteAccount(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	for _, q := range []string{
		`DELETE FROM wallet_nonces WHERE user_id = $1`,
		`UPDATE wallet_transfers SET from_user_id = NULL WHERE from_user_id = $1`,
		`UPDATE wallet_transfers SET to_user_id = NULL WHERE to_user_id = $1`,
		`DELETE FROM wallets WHERE user_id = $1`,
	} {
		if _, err := tx.Exec(ctx, q, userID); err != nil {
			return err
		}
	}
	_, err := tx.Exec(ctx, `DELETE FROM wallet_transfers WHERE from_user_id IS NULL AND to_user_id IS NULL`)
	return err
}

type exportWallet struct {
	Address         string    `json:"address"`
	Kind            string    `json:"kind"`
	Label           string    `json:"label"`
	IsPrimary       bool      `json:"is_primary"`
	Proof           string    `json:"proof"`
	VerifiedChainID *int64    `json:"verified_chain_id"`
	ProviderSubject *string   `json:"provider_subject"`
	CreatedAt       time.Time `json:"created_at"`
}

// ExportAccount returns linked wallets and every transfer kept for the user.
func (s *Service) ExportAccount(ctx context.Context, userID uuid.UUID) (any, error) {
	rows, err := s.d.Pool.Query(ctx, `
		SELECT address, kind, label, is_primary, proof, verified_chain_id, provider_subject, created_at
		FROM wallets WHERE user_id = $1 ORDER BY created_at, id`, userID)
	if err != nil {
		return nil, err
	}
	wallets, err := pgx.CollectRows(rows, pgx.RowToStructByPos[exportWallet])
	if err != nil {
		return nil, err
	}
	for i := range wallets {
		wallets[i].Address = checksum(wallets[i].Address)
	}
	rows, err = s.d.Pool.Query(ctx, `SELECT `+transferCols+` FROM wallet_transfers
		WHERE from_user_id = $1 OR to_user_id = $1 ORDER BY occurred_at, id`, userID)
	if err != nil {
		return nil, err
	}
	trs, err := pgx.CollectRows(rows, pgx.RowToStructByPos[transferRow])
	if err != nil {
		return nil, err
	}
	transfers := make([]Transfer, 0, len(trs))
	for _, t := range trs {
		transfers = append(transfers, s.view(t, userID))
	}
	if wallets == nil {
		wallets = []exportWallet{}
	}
	return map[string]any{"wallets": wallets, "transfers": transfers}, nil
}

// ---- development data -------------------------------------------------------------

// Seed wallets use Anvil's well-known development accounts, so balances are
// real when the local chain runs (WALLETS_LOCAL_CHAIN=true). Their keys are
// public: never fund them on a real network.
var seedWallets = []struct {
	user    int // index into auth.DevIdentities
	address string
	label   string
}{
	{1, "0x70997970C51812dc3A010C7d01b50e0d17dc79C8", "MetaMask"},
	{2, "0x3C44CdDdB6a900fa2b585dd299e03d12FA4293BC", "Rainbow"},
}

// SeedDev links wallets for Sam and Jordan, and, when a token is configured,
// a few transfers between them and an outside address.
func (s *Service) SeedDev(ctx context.Context, users []uuid.UUID) error {
	for _, sw := range seedWallets {
		if sw.user >= len(users) {
			continue
		}
		a, err := ParseAddress(sw.address)
		if err != nil {
			return err
		}
		if _, err := s.d.Pool.Exec(ctx, `
			INSERT INTO wallets (user_id, address, kind, proof, verified_chain_id, label, is_primary)
			VALUES ($1, $2, 'external', 'siwe', $3, $4, NOT EXISTS (SELECT 1 FROM wallets WHERE user_id = $1))
			ON CONFLICT (address) DO NOTHING`,
			users[sw.user], a.Lower(), max(s.registry.DefaultChainID(), 1), sw.label); err != nil {
			return err
		}
	}
	var chain *Chain
	for _, c := range s.registry.Chains() {
		if len(c.Tokens) > 0 {
			chain = c
			break
		}
	}
	if chain == nil {
		return nil
	}
	token := chain.Tokens[0]
	sam, jordan := seedWallets[0].address, seedWallets[1].address
	outside := "0x9965507D1a55bcC2695C58ba16FB37d819B0A4dc" // Anvil account 5, not linked
	transfers := []struct {
		from, to string
		whole    string
		daysAgo  int
	}{
		{outside, sam, "100", 6},
		{sam, jordan, "12", 4},
		{jordan, sam, "4", 1},
	}
	now := time.Now().UTC().Truncate(time.Second)
	for i, t := range transfers {
		from, _ := ParseAddress(t.from)
		to, _ := ParseAddress(t.to)
		amount := t.whole + strings.Repeat("0", *token.Decimals)
		hash := "0x" + hex.EncodeToString(keccak256([]byte("scaffoldapp-seed:wallets:"+itoa(int64(i)))))
		if _, err := s.d.Pool.Exec(ctx, `
			INSERT INTO wallet_transfers (chain_id, tx_hash, log_index, block_number, token_address, token_symbol,
			  token_decimals, from_address, to_address, amount, from_user_id, to_user_id, occurred_at)
			VALUES ($1, $2, 0, $3, $4, $5, $6, $7, $8, $9::numeric,
			  (SELECT user_id FROM wallets WHERE address = $7), (SELECT user_id FROM wallets WHERE address = $8), $10)
			ON CONFLICT (chain_id, tx_hash, log_index) DO NOTHING`,
			chain.ID, hash, int64(1000+i), token.addr.Lower(), token.Symbol, *token.Decimals,
			from.Lower(), to.Lower(), amount, now.AddDate(0, 0, -t.daysAgo)); err != nil {
			return err
		}
	}
	return nil
}

// ---- agent tools -------------------------------------------------------------------

func (s *Service) Tools() []app.Tool {
	return []app.Tool{
		{
			Name:        "wallets_list",
			Description: "List the caller's linked EVM wallets: address, kind (external or embedded), label and which one is primary.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
			ReadOnly:    true,
			Handler: func(ctx context.Context, caller *auth.Identity, _ json.RawMessage) (any, error) {
				list, err := s.listWallets(ctx, s.d.Pool, caller.UserID)
				if err != nil {
					return nil, err
				}
				return map[string]any{"wallets": list}, nil
			},
		},
		{
			Name: "wallets_balances",
			Description: "Read native and token balances of the caller's wallets on the configured chains. " +
				"amount is in base units as a decimal string; divide by 10^decimals for display.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{
				"chain_id":{"type":"integer","minimum":1,"description":"Only this chain."},
				"wallet_id":{"type":"string","format":"uuid","description":"Only this wallet."}},
				"additionalProperties":false}`),
			ReadOnly: true,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					ChainID  int64      `json:"chain_id"`
					WalletID *uuid.UUID `json:"wallet_id"`
				}
				if err := decodeToolInput(input, &in); err != nil {
					return nil, err
				}
				return s.loadBalances(ctx, caller.UserID, in.WalletID, in.ChainID)
			},
		},
		{
			Name: "wallets_activity",
			Description: "List ERC-20 transfers to or from the caller's wallets, newest first. " +
				"amount is in base units; direction is in, out or self. Pass next_cursor back as cursor for older items.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{
				"limit":{"type":"integer","minimum":1,"maximum":50,"default":20},
				"chain_id":{"type":"integer","minimum":1},
				"wallet_id":{"type":"string","format":"uuid"},
				"cursor":{"type":"string"}},
				"additionalProperties":false}`),
			ReadOnly: true,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					Limit    int        `json:"limit"`
					ChainID  int64      `json:"chain_id"`
					WalletID *uuid.UUID `json:"wallet_id"`
					Cursor   string     `json:"cursor"`
				}
				if err := decodeToolInput(input, &in); err != nil {
					return nil, err
				}
				if in.Limit <= 0 || in.Limit > 50 {
					in.Limit = 20
				}
				var c keyCursor
				if err := httpx.DecodeCursor(in.Cursor, &c); err != nil {
					return nil, err
				}
				return s.loadActivity(ctx, caller.UserID, in.WalletID, in.ChainID, c, in.Limit)
			},
		},
	}
}

func decodeToolInput(input json.RawMessage, dst any) error {
	if len(input) == 0 || string(input) == "null" {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(input))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return httpx.BadRequest("Invalid input: " + err.Error())
	}
	return nil
}
