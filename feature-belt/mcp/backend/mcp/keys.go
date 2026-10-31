package mcp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/audit"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// Personal API keys. A key is "scaffoldapp_sk_" + 40 random base62
// characters, shown once at creation. Only its SHA-256 is stored, with a
// short display prefix. Keys are managed with the user's normal session
// (Clerk), never with another API key.

const (
	keyPrefix    = "scaffoldapp_sk_"
	secretLength = 40
	shownChars   = 6
	maxKeys      = 10
	base62       = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
)

// Scopes a key can carry.
const (
	ScopeRead  = "read"
	ScopeWrite = "write"
)

// newSecret returns a fresh key, its hash and its display prefix.
func newSecret() (secret, hash, prefix string) {
	var b strings.Builder
	b.WriteString(keyPrefix)
	buf := make([]byte, 64)
	for n := 0; n < secretLength; {
		_, _ = rand.Read(buf)
		for _, x := range buf {
			if x >= 248 { // 248 = 4×62: reject to keep the alphabet uniform
				continue
			}
			b.WriteByte(base62[int(x)%62])
			if n++; n == secretLength {
				break
			}
		}
	}
	secret = b.String()
	return secret, hashSecret(secret), secret[:len(keyPrefix)+shownChars]
}

func hashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// APIKey is a key's metadata (never the secret).
type APIKey struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Scopes     []string   `json:"scopes"`
	LastUsedAt *time.Time `json:"last_used_at"`
	CreatedAt  time.Time  `json:"created_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
}

// CreatedKey is the one response that carries the secret.
type CreatedKey struct {
	Key    APIKey `json:"key"`
	Secret string `json:"secret"`
}

const keyCols = `id, name, prefix, scopes, last_used_at, created_at, revoked_at`

type cursor struct {
	T  time.Time `json:"t"`
	ID uuid.UUID `json:"id"`
}

func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// listKeys is GET /v1/mcp/keys: the caller's active keys, newest first.
func (s *Service) listKeys(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	p := httpx.ParsePage(r)
	var c cursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	rows, err := s.d.Pool.Query(r.Context(), `
		SELECT `+keyCols+` FROM mcp_api_keys
		WHERE user_id = $1 AND revoked_at IS NULL
		  AND ($2::timestamptz IS NULL OR (created_at, id) < ($2, $3))
		ORDER BY created_at DESC, id DESC LIMIT $4`, id.UserID, nullTime(c.T), c.ID, p.Limit+1)
	if err != nil {
		return err
	}
	keys, err := pgx.CollectRows(rows, pgx.RowToStructByPos[APIKey])
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(keys, p.Limit, func(k APIKey) any { return cursor{k.CreatedAt, k.ID} }))
	return nil
}

type createKeyReq struct {
	Name   string   `json:"name"`
	Scopes []string `json:"scopes"`
}

// normaliseScopes accepts ["read"] or ["read","write"] (["write"] implies read).
func normaliseScopes(in []string) ([]string, error) {
	if len(in) == 0 {
		return []string{ScopeRead}, nil
	}
	write := false
	for _, sc := range in {
		switch strings.ToLower(strings.TrimSpace(sc)) {
		case ScopeRead:
		case ScopeWrite:
			write = true
		default:
			return nil, httpx.BadRequest("Scopes must be read, or read and write.")
		}
	}
	if write {
		return []string{ScopeRead, ScopeWrite}, nil
	}
	return []string{ScopeRead}, nil
}

// createKey is POST /v1/mcp/keys. The secret is in this response only.
func (s *Service) createKey(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	var req createKeyReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	name := strings.Join(strings.Fields(req.Name), " ")
	if n := len([]rune(name)); n == 0 || n > 60 {
		return httpx.BadRequest("Name the key (up to 60 characters).")
	}
	scopes, err := normaliseScopes(req.Scopes)
	if err != nil {
		return err
	}
	secret, hash, prefix := newSecret()
	var key APIKey
	err = db.WithTx(r.Context(), s.d.Pool, func(tx pgx.Tx) error {
		// Serialise key creation per user so the limit holds under concurrency.
		if _, err := tx.Exec(r.Context(), `SELECT id FROM users WHERE id = $1 FOR UPDATE`, id.UserID); err != nil {
			return err
		}
		var active int
		if err := tx.QueryRow(r.Context(), `SELECT count(*) FROM mcp_api_keys WHERE user_id = $1 AND revoked_at IS NULL`,
			id.UserID).Scan(&active); err != nil {
			return err
		}
		if active >= maxKeys {
			return httpx.NewError(http.StatusConflict, "key_limit", "You have 10 API keys. Revoke one to create another.")
		}
		rows, err := tx.Query(r.Context(), `
			INSERT INTO mcp_api_keys (user_id, name, prefix, secret_hash, scopes)
			VALUES ($1, $2, $3, $4, $5) RETURNING `+keyCols, id.UserID, name, prefix, hash, scopes)
		if err != nil {
			return err
		}
		key, err = pgx.CollectExactlyOneRow(rows, pgx.RowToStructByPos[APIKey])
		return err
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, CreatedKey{Key: key, Secret: secret})
	return nil
}

// revokeKey is DELETE /v1/mcp/keys/{id}: the owner revokes a key.
func (s *Service) revokeKey(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	keyID, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	tag, err := s.d.Pool.Exec(r.Context(), `
		UPDATE mcp_api_keys SET revoked_at = now(), updated_at = now()
		WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`, keyID, id.UserID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.ErrNotFound
	}
	httpx.NoContent(w)
	return nil
}

// ---- admin -----------------------------------------------------------------

// AdminKey is a key as staff see it.
type AdminKey struct {
	APIKey
	UserID    uuid.UUID  `json:"user_id"`
	UserEmail string     `json:"user_email"`
	RevokedBy *uuid.UUID `json:"revoked_by"`
}

// adminListKeys is GET /v1/admin/mcp/keys?user_id=&status=active|revoked.
func (s *Service) adminListKeys(w http.ResponseWriter, r *http.Request) error {
	p := httpx.ParsePage(r)
	var c cursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	q := r.URL.Query()
	var userID *uuid.UUID
	if v := q.Get("user_id"); v != "" {
		u, err := uuid.Parse(v)
		if err != nil {
			return httpx.BadRequest("Invalid user_id.")
		}
		userID = &u
	}
	status := q.Get("status")
	if !slices.Contains([]string{"", "active", "revoked"}, status) {
		return httpx.BadRequest("Status must be active or revoked.")
	}
	rows, err := s.d.Pool.Query(r.Context(), `
		SELECT k.id, k.name, k.prefix, k.scopes, k.last_used_at, k.created_at, k.revoked_at, k.user_id, u.email, k.revoked_by
		FROM mcp_api_keys k JOIN users u ON u.id = k.user_id
		WHERE ($1::uuid IS NULL OR k.user_id = $1)
		  AND ($2 = '' OR ($2 = 'active') = (k.revoked_at IS NULL))
		  AND ($3::timestamptz IS NULL OR (k.created_at, k.id) < ($3, $4))
		ORDER BY k.created_at DESC, k.id DESC LIMIT $5`, userID, status, nullTime(c.T), c.ID, p.Limit+1)
	if err != nil {
		return err
	}
	defer rows.Close()
	list := []AdminKey{}
	for rows.Next() {
		var k AdminKey
		if err := rows.Scan(&k.ID, &k.Name, &k.Prefix, &k.Scopes, &k.LastUsedAt, &k.CreatedAt, &k.RevokedAt,
			&k.UserID, &k.UserEmail, &k.RevokedBy); err != nil {
			return err
		}
		list = append(list, k)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(list, p.Limit, func(k AdminKey) any { return cursor{k.CreatedAt, k.ID} }))
	return nil
}

// adminRevokeKey is DELETE /v1/admin/mcp/keys/{id}, audited.
func (s *Service) adminRevokeKey(w http.ResponseWriter, r *http.Request) error {
	staff := auth.MustFromContext(r.Context())
	keyID, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	err = db.WithTx(r.Context(), s.d.Pool, func(tx pgx.Tx) error {
		var owner uuid.UUID
		var prefix string
		err := tx.QueryRow(r.Context(), `
			UPDATE mcp_api_keys SET revoked_at = now(), revoked_by = $2, updated_at = now()
			WHERE id = $1 AND revoked_at IS NULL RETURNING user_id, prefix`, keyID, staff.UserID).Scan(&owner, &prefix)
		if db.IsNoRows(err) {
			return httpx.ErrNotFound
		}
		if err != nil {
			return err
		}
		return audit.Log(r.Context(), tx, staff.UserID, "mcp.key.revoke", "mcp_api_key", keyID.String(),
			map[string]any{"user_id": owner, "prefix": prefix})
	})
	if err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

// ---- resolution ------------------------------------------------------------

// principal is an authenticated MCP caller.
type principal struct {
	identity *auth.Identity
	keyID    uuid.UUID
	scopes   []string
}

func (p *principal) canWrite() bool { return slices.Contains(p.scopes, ScopeWrite) }

var (
	errBadKey = httpx.NewError(http.StatusUnauthorized, "unauthenticated",
		"Send a valid API key as Authorization: Bearer <key>.")
	errInactive = httpx.NewError(http.StatusForbidden, "account_suspended",
		"This account can't use API keys. Contact support.")
)

// resolveKey turns a bearer secret into a principal. Suspended, banned and
// deleted accounts are refused.
func (s *Service) resolveKey(ctx context.Context, secret string) (*principal, error) {
	if !strings.HasPrefix(secret, keyPrefix) || len(secret) != len(keyPrefix)+secretLength {
		return nil, errBadKey
	}
	p := &principal{identity: &auth.Identity{}}
	var role *string
	id := p.identity
	err := s.d.Pool.QueryRow(ctx, `
		SELECT k.id, k.scopes, u.id, u.auth_subject, u.email, u.display_name, u.admin_role, u.status,
		       COALESCE(u.terms_version_accepted, ''), COALESCE(u.privacy_version_accepted, '')
		FROM mcp_api_keys k JOIN users u ON u.id = k.user_id
		WHERE k.secret_hash = $1 AND k.revoked_at IS NULL`, hashSecret(secret),
	).Scan(&p.keyID, &p.scopes, &id.UserID, &id.Subject, &id.Email, &id.DisplayName, &role, &id.Status,
		&id.TermsVersion, &id.PrivacyVersion)
	if db.IsNoRows(err) {
		return nil, errBadKey
	}
	if err != nil {
		return nil, err
	}
	if role != nil {
		id.AdminRole = *role
	}
	if id.Status != "active" {
		return nil, errInactive
	}
	// At most one write a minute per key.
	if _, err := s.d.Pool.Exec(ctx, `
		UPDATE mcp_api_keys SET last_used_at = now()
		WHERE id = $1 AND (last_used_at IS NULL OR last_used_at < now() - interval '1 minute')`, p.keyID); err != nil {
		return nil, err
	}
	return p, nil
}
