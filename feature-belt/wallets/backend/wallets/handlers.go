package wallets

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

var (
	errAddressTaken   = httpx.NewError(http.StatusConflict, "wallets_address_taken", "That wallet is linked to another account.")
	errAlreadyLinked  = httpx.NewError(http.StatusConflict, "wallets_already_linked", "That wallet is already linked to your account.")
	errChallenge      = httpx.NewError(http.StatusBadRequest, "wallets_challenge_invalid", "This signing request expired or was already used. Start again.")
	errSignature      = httpx.NewError(http.StatusBadRequest, "wallets_signature_invalid", "The signature doesn't match this wallet. Sign with the wallet you're linking.")
	errEmbeddedOff    = httpx.NewError(http.StatusServiceUnavailable, "wallets_embedded_unconfigured", "Embedded wallets aren't set up on this server.")
	errIdentityToken  = httpx.NewError(http.StatusBadRequest, "wallets_identity_token_invalid", "Couldn't verify your wallet session. Sign in to your wallet again.")
	errNoEmbedded     = httpx.NewError(http.StatusUnprocessableEntity, "wallets_no_embedded_wallet", "There's no embedded wallet on this account yet. Create one, then try again.")
	errPrivyTaken     = httpx.NewError(http.StatusConflict, "wallets_privy_account_taken", "That wallet account is linked to another user.")
	errUnknownChain   = httpx.NewError(http.StatusBadRequest, "wallets_chain_unknown", "That chain isn't available.")
	errWalletNotFound = httpx.NotFound("Wallet not found.")
)

func (s *Service) limitError() *httpx.Error {
	return httpx.NewError(http.StatusConflict, "wallets_limit_reached",
		"You can link up to "+strconv.Itoa(s.cfg.maxPerUser)+" wallets. Remove one first.")
}

func messageError(reason string) *httpx.Error {
	e := httpx.NewError(http.StatusBadRequest, "wallets_message_invalid", "The signed message doesn't match this app. Start again.")
	e.Details = map[string]any{"reason": reason}
	return e
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// lockUser serialises one user's wallet mutations (primary flag, limit).
func lockUser(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('wallets:' || $1::text, 0))`, userID)
	return err
}

func cleanLabel(raw string) (string, error) {
	label := strings.TrimSpace(raw)
	if len([]rune(label)) > 40 {
		return "", httpx.BadRequest("Labels can be up to 40 characters.")
	}
	return label, nil
}

// ---- list ----------------------------------------------------------------------

func (s *Service) listWallets(ctx context.Context, q db.Querier, userID uuid.UUID) ([]Wallet, error) {
	rows, err := q.Query(ctx, `SELECT `+walletCols+` FROM wallets WHERE user_id = $1
		ORDER BY is_primary DESC, created_at, id`, userID)
	if err != nil {
		return nil, err
	}
	return scanWallets(rows)
}

// list returns every linked wallet (capped by WALLETS_MAX_PER_USER, so one
// page), primary first.
func (s *Service) list(w http.ResponseWriter, r *http.Request) error {
	list, err := s.listWallets(r.Context(), s.d.Pool, auth.MustFromContext(r.Context()).UserID)
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.Page[Wallet]{Items: list})
	return nil
}

// ---- external wallets: SIWE ------------------------------------------------------

type challengeReq struct {
	Address string `json:"address"`
	ChainID int64  `json:"chain_id"`
}

// Challenge is the message the wallet signs with personal_sign.
type Challenge struct {
	Message   string    `json:"message"`
	Nonce     string    `json:"nonce"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (s *Service) challenge(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	var req challengeReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	addr, err := ParseAddress(req.Address)
	if err != nil {
		return httpx.BadRequest("Enter a valid wallet address (0x followed by 40 hex characters).")
	}
	if req.ChainID == 0 {
		req.ChainID = s.registry.DefaultChainID()
	}
	if req.ChainID == 0 {
		req.ChainID = 1 // SIWE needs a chain id; an EOA signature is valid on every chain
	}
	if req.ChainID < 0 || req.ChainID > 1<<53 {
		return httpx.BadRequest("Invalid chain_id.")
	}
	var owner uuid.UUID
	switch err := s.d.Pool.QueryRow(r.Context(), `SELECT user_id FROM wallets WHERE address = $1`, addr.Lower()).Scan(&owner); {
	case err == nil && owner == id.UserID:
		return errAlreadyLinked
	case err == nil:
		return errAddressTaken
	case !db.IsNoRows(err):
		return err
	}
	var count int
	if err := s.d.Pool.QueryRow(r.Context(), `SELECT count(*) FROM wallets WHERE user_id = $1`, id.UserID).Scan(&count); err != nil {
		return err
	}
	if count >= s.cfg.maxPerUser {
		return s.limitError()
	}

	var nb [16]byte
	if _, err := rand.Read(nb[:]); err != nil {
		return err
	}
	now := s.now().UTC().Truncate(time.Second)
	expires := now.Add(challengeTTL)
	msg := siweMessage{
		Domain: s.cfg.siweDomain, Address: addr.Hex(),
		Statement: "Link this wallet to your Scaffold App account.",
		URI:       s.cfg.siweURI, Version: "1", ChainID: req.ChainID,
		Nonce: hex.EncodeToString(nb[:]), IssuedAt: now, ExpirationTime: &expires,
	}
	if _, err := s.d.Pool.Exec(r.Context(), `
		INSERT INTO wallet_nonces (nonce, user_id, address, chain_id, expires_at) VALUES ($1, $2, $3, $4, $5)`,
		msg.Nonce, id.UserID, addr.Lower(), req.ChainID, expires); err != nil {
		return err
	}
	httpx.OK(w, Challenge{Message: msg.String(), Nonce: msg.Nonce, ExpiresAt: expires})
	return nil
}

type linkReq struct {
	Message   string `json:"message"`
	Signature string `json:"signature"`
	Label     string `json:"label"`
}

func (s *Service) link(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	var req linkReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	label, err := cleanLabel(req.Label)
	if err != nil {
		return err
	}
	msg, err := parseSIWE(req.Message)
	if err != nil {
		return messageError(err.Error())
	}
	sig, err := decodeHex(req.Signature)
	if err != nil || len(sig) < 65 || len(sig) > 4096 {
		return errSignature
	}

	// Consume the challenge first: one attempt per nonce, pass or fail.
	var stored string
	var chainID int64
	err = s.d.Pool.QueryRow(r.Context(), `
		UPDATE wallet_nonces SET used_at = now()
		WHERE nonce = $1 AND user_id = $2 AND used_at IS NULL AND expires_at > now()
		RETURNING address, chain_id`, msg.Nonce, id.UserID).Scan(&stored, &chainID)
	if db.IsNoRows(err) {
		return errChallenge
	}
	if err != nil {
		return err
	}
	addr, err := ParseAddress(stored)
	if err != nil {
		return err
	}
	if err := msg.verify(siweExpect{
		Domain: s.cfg.siweDomain, URI: s.cfg.siweURI, Address: addr, ChainID: chainID, Nonce: msg.Nonce,
	}, s.now()); err != nil {
		return messageError(err.Error())
	}

	// EOAs: recover the signer. Contract wallets: ask the contract (EIP-1271)
	// on the signed chain, when that chain is configured.
	hash := personalHash([]byte(req.Message))
	proof := ""
	if len(sig) == 65 {
		if signer, err := recoverAddress(hash, sig); err == nil && signer == addr {
			proof = "siwe"
		}
	}
	if proof == "" {
		if chain, ok := s.registry.Chain(chainID); ok && s.rpc.verifyEIP1271(r.Context(), chain, addr, hash, sig) {
			proof = "eip1271"
		}
	}
	if proof == "" {
		return errSignature
	}

	var wallet Wallet
	var created bool
	err = db.WithTx(r.Context(), s.d.Pool, func(tx pgx.Tx) error {
		var err error
		wallet, created, err = s.insertWallet(r.Context(), tx, id.UserID, addr, newWallet{
			kind: "external", proof: proof, chainID: &chainID, label: label,
		})
		return err
	})
	if err != nil {
		return err
	}
	if !created {
		httpx.OK(w, wallet)
		return nil
	}
	s.publishChanged(r.Context(), id.UserID, wallet.ID, "linked")
	httpx.JSON(w, http.StatusCreated, wallet)
	return nil
}

type newWallet struct {
	kind    string  // external | embedded
	proof   string  // siwe | eip1271 | privy
	chainID *int64  // chain named in the SIWE message
	subject *string // Privy user id for embedded wallets
	label   string
}

// insertWallet links addr to userID inside tx. Re-linking an address the
// user already owns returns the existing row (created=false); an address
// owned by someone else is a conflict.
func (s *Service) insertWallet(ctx context.Context, tx pgx.Tx, userID uuid.UUID, addr Address, nw newWallet) (Wallet, bool, error) {
	if err := lockUser(ctx, tx, userID); err != nil {
		return Wallet{}, false, err
	}
	var owner uuid.UUID
	err := tx.QueryRow(ctx, `SELECT user_id FROM wallets WHERE address = $1`, addr.Lower()).Scan(&owner)
	switch {
	case err == nil && owner != userID:
		return Wallet{}, false, errAddressTaken
	case err == nil:
		if nw.subject != nil {
			if _, err := tx.Exec(ctx, `UPDATE wallets SET provider_subject = COALESCE(provider_subject, $2), updated_at = now()
				WHERE address = $1 AND proof = 'privy'`, addr.Lower(), *nw.subject); err != nil {
				return Wallet{}, false, err
			}
		}
		rows, err := tx.Query(ctx, `SELECT `+walletCols+` FROM wallets WHERE address = $1`, addr.Lower())
		if err != nil {
			return Wallet{}, false, err
		}
		list, err := scanWallets(rows)
		if err != nil || len(list) == 0 {
			return Wallet{}, false, err
		}
		return list[0], false, nil
	case !db.IsNoRows(err):
		return Wallet{}, false, err
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM wallets WHERE user_id = $1`, userID).Scan(&count); err != nil {
		return Wallet{}, false, err
	}
	if count >= s.cfg.maxPerUser {
		return Wallet{}, false, s.limitError()
	}
	rows, err := tx.Query(ctx, `
		INSERT INTO wallets (user_id, address, kind, proof, verified_chain_id, provider_subject, label, is_primary)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING `+walletCols, userID, addr.Lower(), nw.kind, nw.proof, nw.chainID, nw.subject, nw.label, count == 0)
	if err != nil {
		return Wallet{}, false, err
	}
	list, err := scanWallets(rows)
	if db.IsUniqueViolation(err, "wallets_address_key") {
		return Wallet{}, false, errAddressTaken
	}
	if err != nil || len(list) == 0 {
		return Wallet{}, false, err
	}
	return list[0], true, nil
}

// ---- embedded wallets: Privy ---------------------------------------------------

type embeddedReq struct {
	IdentityToken string `json:"identity_token"`
	Address       string `json:"address"` // optional: register only this wallet
	Label         string `json:"label"`
}

// registerEmbedded links the Privy-managed wallets listed in a verified
// identity token. The Privy user is bound to this account: another account
// can't register the same Privy user's wallets.
func (s *Service) registerEmbedded(w http.ResponseWriter, r *http.Request) error {
	if s.privy == nil {
		return errEmbeddedOff
	}
	id := auth.MustFromContext(r.Context())
	var req embeddedReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	label, err := cleanLabel(req.Label)
	if err != nil {
		return err
	}
	ident, err := s.privy.verify(req.IdentityToken, s.now())
	if err != nil {
		return errIdentityToken
	}
	addrs := ident.Wallets
	if req.Address != "" {
		want, err := ParseAddress(req.Address)
		if err != nil {
			return httpx.BadRequest("Enter a valid wallet address (0x followed by 40 hex characters).")
		}
		addrs = nil
		for _, a := range ident.Wallets {
			if a == want {
				addrs = []Address{a}
			}
		}
		if addrs == nil {
			return httpx.NewError(http.StatusBadRequest, "wallets_not_in_token", "That address isn't an embedded wallet on this account.")
		}
	}
	if len(addrs) == 0 {
		return errNoEmbedded
	}
	var out []Wallet
	var created []uuid.UUID
	err = db.WithTx(r.Context(), s.d.Pool, func(tx pgx.Tx) error {
		var other bool
		if err := tx.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM wallets
			WHERE proof = 'privy' AND provider_subject = $1 AND user_id <> $2)`, ident.Subject, id.UserID).Scan(&other); err != nil {
			return err
		}
		if other {
			return errPrivyTaken
		}
		for _, a := range addrs {
			wlt, isNew, err := s.insertWallet(r.Context(), tx, id.UserID, a, newWallet{
				kind: "embedded", proof: "privy", subject: &ident.Subject, label: label,
			})
			if err != nil {
				return err
			}
			out = append(out, wlt)
			if isNew {
				created = append(created, wlt.ID)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, wid := range created {
		s.publishChanged(r.Context(), id.UserID, wid, "linked")
	}
	httpx.OK(w, httpx.Page[Wallet]{Items: out})
	return nil
}

// ---- manage -------------------------------------------------------------------

type updateReq struct {
	Label     *string `json:"label"`
	IsPrimary *bool   `json:"is_primary"`
}

func (s *Service) update(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	wid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req updateReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.IsPrimary != nil && !*req.IsPrimary {
		return httpx.BadRequest("Make another wallet primary instead.")
	}
	var label *string
	if req.Label != nil {
		l, err := cleanLabel(*req.Label)
		if err != nil {
			return err
		}
		label = &l
	}
	err = db.WithTx(r.Context(), s.d.Pool, func(tx pgx.Tx) error {
		if err := lockUser(r.Context(), tx, id.UserID); err != nil {
			return err
		}
		var exists bool
		if err := tx.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM wallets WHERE id = $1 AND user_id = $2)`,
			wid, id.UserID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return errWalletNotFound
		}
		if req.IsPrimary != nil {
			if _, err := tx.Exec(r.Context(), `UPDATE wallets SET is_primary = false, updated_at = now()
				WHERE user_id = $1 AND is_primary AND id <> $2`, id.UserID, wid); err != nil {
				return err
			}
		}
		_, err := tx.Exec(r.Context(), `
			UPDATE wallets SET label = COALESCE($3, label), is_primary = is_primary OR $4, updated_at = now()
			WHERE id = $1 AND user_id = $2`, wid, id.UserID, label, req.IsPrimary != nil)
		return err
	})
	if err != nil {
		return err
	}
	rows, err := s.d.Pool.Query(r.Context(), `SELECT `+walletCols+` FROM wallets WHERE id = $1`, wid)
	if err != nil {
		return err
	}
	list, err := scanWallets(rows)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		return errWalletNotFound
	}
	s.publishChanged(r.Context(), id.UserID, wid, "updated")
	httpx.OK(w, list[0])
	return nil
}

func (s *Service) unlink(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	wid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	owner := id.UserID
	err = db.WithTx(r.Context(), s.d.Pool, func(tx pgx.Tx) error {
		_, _, err := removeWallet(r.Context(), tx, wid, &owner)
		return err
	})
	if err != nil {
		return err
	}
	s.publishChanged(r.Context(), id.UserID, wid, "unlinked")
	httpx.NoContent(w)
	return nil
}

// removeWallet unlinks a wallet (only the owner's when owner is set),
// detaches its transfers from the user and promotes the next wallet to
// primary. It returns the owner and lowercase address.
func removeWallet(ctx context.Context, tx pgx.Tx, walletID uuid.UUID, owner *uuid.UUID) (uuid.UUID, string, error) {
	var userID uuid.UUID
	if owner != nil {
		userID = *owner
	} else if err := tx.QueryRow(ctx, `SELECT user_id FROM wallets WHERE id = $1`, walletID).Scan(&userID); err != nil {
		if db.IsNoRows(err) {
			return uuid.Nil, "", errWalletNotFound
		}
		return uuid.Nil, "", err
	}
	if err := lockUser(ctx, tx, userID); err != nil {
		return uuid.Nil, "", err
	}
	var address string
	var wasPrimary bool
	err := tx.QueryRow(ctx, `DELETE FROM wallets WHERE id = $1 AND user_id = $2 RETURNING address, is_primary`,
		walletID, userID).Scan(&address, &wasPrimary)
	if db.IsNoRows(err) {
		return uuid.Nil, "", errWalletNotFound
	}
	if err != nil {
		return uuid.Nil, "", err
	}
	if err := detachTransfers(ctx, tx, userID, address); err != nil {
		return uuid.Nil, "", err
	}
	if wasPrimary {
		if _, err := tx.Exec(ctx, `
			UPDATE wallets SET is_primary = true, updated_at = now()
			WHERE id = (SELECT id FROM wallets WHERE user_id = $1 ORDER BY created_at, id LIMIT 1)`, userID); err != nil {
			return uuid.Nil, "", err
		}
	}
	return userID, address, nil
}

// detachTransfers drops the user's link to transfers of an address they no
// longer own, and deletes transfers no linked user still needs.
func detachTransfers(ctx context.Context, tx pgx.Tx, userID uuid.UUID, address string) error {
	if _, err := tx.Exec(ctx, `UPDATE wallet_transfers SET from_user_id = NULL
		WHERE from_user_id = $1 AND from_address = $2`, userID, address); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE wallet_transfers SET to_user_id = NULL
		WHERE to_user_id = $1 AND to_address = $2`, userID, address); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `DELETE FROM wallet_transfers WHERE from_user_id IS NULL AND to_user_id IS NULL`)
	return err
}

// ---- balances -----------------------------------------------------------------

// BalancesResponse is GET /v1/wallets/balances.
type BalancesResponse struct {
	Items     []Balance `json:"items"`
	FetchedAt time.Time `json:"fetched_at"`
}

func (s *Service) getBalances(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	walletID, chainID, err := s.filters(r)
	if err != nil {
		return err
	}
	res, err := s.loadBalances(r.Context(), id.UserID, walletID, chainID)
	if err != nil {
		return err
	}
	httpx.OK(w, res)
	return nil
}

// filters reads the optional ?wallet_id= and ?chain_id= query params.
func (s *Service) filters(r *http.Request) (*uuid.UUID, int64, error) {
	var walletID *uuid.UUID
	if v := r.URL.Query().Get("wallet_id"); v != "" {
		wid, err := uuid.Parse(v)
		if err != nil {
			return nil, 0, httpx.BadRequest("Invalid wallet_id.")
		}
		walletID = &wid
	}
	var chainID int64
	if v := r.URL.Query().Get("chain_id"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n <= 0 {
			return nil, 0, httpx.BadRequest("Invalid chain_id.")
		}
		chainID = n
	}
	return walletID, chainID, nil
}

// loadBalances reads native and token balances for the user's wallets on
// every configured chain (or one wallet / one chain), chains in parallel.
func (s *Service) loadBalances(ctx context.Context, userID uuid.UUID, walletID *uuid.UUID, chainID int64) (*BalancesResponse, error) {
	chains := s.registry.Chains()
	if chainID != 0 {
		c, ok := s.registry.Chain(chainID)
		if !ok {
			return nil, errUnknownChain
		}
		chains = []*Chain{c}
	}
	list, err := s.listWallets(ctx, s.d.Pool, userID)
	if err != nil {
		return nil, err
	}
	if walletID != nil {
		var one []Wallet
		for _, wl := range list {
			if wl.ID == *walletID {
				one = append(one, wl)
			}
		}
		if one == nil {
			return nil, errWalletNotFound
		}
		list = one
	}
	items := []Balance{}
	type span struct{ chain, from, to int }
	var spans []span
	for ci, c := range chains {
		start := len(items)
		for _, wl := range list {
			items = append(items, Balance{WalletID: wl.ID, Address: wl.Address, ChainID: c.ID,
				Symbol: c.NativeSymbol, Decimals: *c.NativeDecimals})
			for _, t := range c.Tokens {
				ta := t.Address
				items = append(items, Balance{WalletID: wl.ID, Address: wl.Address, ChainID: c.ID,
					TokenAddress: &ta, Symbol: t.Symbol, Decimals: *t.Decimals})
			}
		}
		spans = append(spans, span{ci, start, len(items)})
	}
	now := s.now()
	var wg sync.WaitGroup
	for _, sp := range spans {
		ptrs := make([]*Balance, 0, sp.to-sp.from)
		for i := sp.from; i < sp.to; i++ {
			ptrs = append(ptrs, &items[i])
		}
		wg.Add(1)
		go func(c *Chain, ptrs []*Balance) {
			defer wg.Done()
			s.readBalances(ctx, c, ptrs, now)
		}(chains[sp.chain], ptrs)
	}
	wg.Wait()
	return &BalancesResponse{Items: items, FetchedAt: now.UTC()}, nil
}

// ---- activity -----------------------------------------------------------------

type keyCursor struct {
	T  time.Time `json:"t"`
	ID uuid.UUID `json:"id"`
}

func (s *Service) activity(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	walletID, chainID, err := s.filters(r)
	if err != nil {
		return err
	}
	p := httpx.ParsePage(r)
	var c keyCursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	page, err := s.loadActivity(r.Context(), id.UserID, walletID, chainID, c, p.Limit)
	if err != nil {
		return err
	}
	httpx.OK(w, page)
	return nil
}

// loadActivity pages the user's transfers newest first, keyset on
// (occurred_at, id): block time, so backfilled transfers land in chain order.
func (s *Service) loadActivity(ctx context.Context, userID uuid.UUID, walletID *uuid.UUID, chainID int64, c keyCursor, limit int) (httpx.Page[Transfer], error) {
	address := ""
	if walletID != nil {
		if err := s.d.Pool.QueryRow(ctx, `SELECT address FROM wallets WHERE id = $1 AND user_id = $2`,
			*walletID, userID).Scan(&address); err != nil {
			if db.IsNoRows(err) {
				return httpx.Page[Transfer]{}, errWalletNotFound
			}
			return httpx.Page[Transfer]{}, err
		}
	}
	var after *time.Time
	if !c.T.IsZero() {
		after = &c.T
	}
	// One index range per side, merged; UNION folds transfers between two of
	// the user's own wallets into one row.
	rows, err := s.d.Pool.Query(ctx, `
		SELECT * FROM (
		  (SELECT `+transferCols+` FROM wallet_transfers
		   WHERE from_user_id = $1 AND ($2::text = '' OR from_address = $2) AND ($3::bigint = 0 OR chain_id = $3)
		     AND ($4::timestamptz IS NULL OR (occurred_at, id) < ($4, $5::uuid))
		   ORDER BY occurred_at DESC, id DESC LIMIT $6)
		  UNION
		  (SELECT `+transferCols+` FROM wallet_transfers
		   WHERE to_user_id = $1 AND ($2::text = '' OR to_address = $2) AND ($3::bigint = 0 OR chain_id = $3)
		     AND ($4::timestamptz IS NULL OR (occurred_at, id) < ($4, $5::uuid))
		   ORDER BY occurred_at DESC, id DESC LIMIT $6)
		) t ORDER BY occurred_at DESC, id DESC LIMIT $6`,
		userID, address, chainID, after, c.ID, limit+1)
	if err != nil {
		return httpx.Page[Transfer]{}, err
	}
	trs, err := pgx.CollectRows(rows, pgx.RowToStructByPos[transferRow])
	if err != nil {
		return httpx.Page[Transfer]{}, err
	}
	out := make([]Transfer, 0, len(trs))
	for _, t := range trs {
		out = append(out, s.view(t, userID))
	}
	return httpx.NewPage(out, limit, func(t Transfer) any { return keyCursor{t.OccurredAt, t.ID} }), nil
}
