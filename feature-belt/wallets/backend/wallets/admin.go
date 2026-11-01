package wallets

import (
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/audit"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// AdminWallet is a linked wallet as staff see it.
type AdminWallet struct {
	ID              uuid.UUID `json:"id"`
	Address         string    `json:"address"`
	Kind            string    `json:"kind"`
	Label           string    `json:"label"`
	IsPrimary       bool      `json:"is_primary"`
	Proof           string    `json:"proof"`             // siwe | eip1271 | privy
	VerifiedChainID *int64    `json:"verified_chain_id"` // chain named in the SIWE message
	ProviderSubject *string   `json:"provider_subject"`  // Privy user id for embedded wallets
	UserID          uuid.UUID `json:"user_id"`
	UserEmail       string    `json:"user_email"`
	UserDisplayName string    `json:"user_display_name"`
	CreatedAt       time.Time `json:"created_at"`
}

var hexPrefixRe = regexp.MustCompile(`^0x[0-9a-f]{4,40}$`)

// adminList finds wallets by address (full or a prefix of at least 4 hex
// digits) and/or user. Each search is audited: it ties addresses to people.
func (s *Service) adminList(w http.ResponseWriter, r *http.Request) error {
	staff := auth.MustFromContext(r.Context())
	p := httpx.ParsePage(r)
	var c keyCursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	q := r.URL.Query()
	address := strings.ToLower(strings.TrimSpace(q.Get("address")))
	if address != "" && !hexPrefixRe.MatchString(address) {
		return httpx.BadRequest("Search by a full address or its first 4 or more hex digits, starting with 0x.")
	}
	var userID *uuid.UUID
	if v := strings.TrimSpace(q.Get("user_id")); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			return httpx.BadRequest("Invalid user_id.")
		}
		userID = &id
	}
	if p.Cursor == "" {
		targetType, targetID := "wallets", "*"
		switch {
		case address != "":
			targetType, targetID = "wallet_address", address
		case userID != nil:
			targetType, targetID = "user", userID.String()
		}
		if err := audit.Log(r.Context(), s.d.Pool, staff.UserID, "wallets.lookup", targetType, targetID,
			map[string]any{"address": address, "user_id": userID}); err != nil {
			return err
		}
	}
	var after *time.Time
	if !c.T.IsZero() {
		after = &c.T
	}
	rows, err := s.d.Pool.Query(r.Context(), `
		SELECT w.id, w.address, w.kind, w.label, w.is_primary, w.proof, w.verified_chain_id, w.provider_subject,
		       w.user_id, u.email, u.display_name, w.created_at
		FROM wallets w JOIN users u ON u.id = w.user_id
		WHERE ($1::text = '' OR w.address LIKE $1 || '%') AND ($2::uuid IS NULL OR w.user_id = $2)
		  AND ($3::timestamptz IS NULL OR (w.created_at, w.id) < ($3, $4::uuid))
		ORDER BY w.created_at DESC, w.id DESC LIMIT $5`, address, userID, after, c.ID, p.Limit+1)
	if err != nil {
		return err
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[AdminWallet])
	if err != nil {
		return err
	}
	for i := range list {
		list[i].Address = checksum(list[i].Address)
	}
	httpx.OK(w, httpx.NewPage(list, p.Limit, func(a AdminWallet) any { return keyCursor{a.CreatedAt, a.ID} }))
	return nil
}

// adminUnlink removes a wallet from its user (support or admin), audited in
// the same transaction.
func (s *Service) adminUnlink(w http.ResponseWriter, r *http.Request) error {
	staff := auth.MustFromContext(r.Context())
	wid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var owner uuid.UUID
	err = db.WithTx(r.Context(), s.d.Pool, func(tx pgx.Tx) error {
		uid, address, err := removeWallet(r.Context(), tx, wid, nil)
		if err != nil {
			return err
		}
		owner = uid
		return audit.Log(r.Context(), tx, staff.UserID, "wallets.unlink", "wallet", wid.String(),
			map[string]any{"user_id": uid, "address": checksum(address)})
	})
	if err != nil {
		return err
	}
	s.publishChanged(r.Context(), owner, wid, "unlinked")
	httpx.NoContent(w)
	return nil
}
