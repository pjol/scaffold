package repos

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// Personal git credentials: what a person types as the password when git
// asks (any username). A secret is credPrefix + 40 base62 characters, shown
// once; only its sha256 is stored. They are made and revoked with the
// person's own session (Settings → Git credentials), never by an agent, and
// they open nothing but the git endpoint (/git/…).

const (
	credPrefix       = "scaffoldapp_git_"
	credSecretLength = 40
	credShownChars   = 4
	maxCredentials   = 20
	base62           = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
)

// Credential is a git credential's metadata (never the secret).
type Credential struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Access     string     `json:"access"` // read | write
	ExpiresAt  *time.Time `json:"expires_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

// CreatedCredential is the one response that carries the secret.
type CreatedCredential struct {
	Credential Credential `json:"credential"`
	Secret     string     `json:"secret"`
}

const credentialCols = `id, name, prefix, access, expires_at, last_used_at, created_at`

func newCredentialSecret() (secret, hash, prefix string) {
	var b strings.Builder
	b.WriteString(credPrefix)
	buf := make([]byte, 64)
	for n := 0; n < credSecretLength; {
		_, _ = rand.Read(buf)
		for _, x := range buf {
			if x >= 248 { // 4×62: rejection keeps the alphabet uniform
				continue
			}
			b.WriteByte(base62[int(x)%62])
			if n++; n == credSecretLength {
				break
			}
		}
	}
	secret = b.String()
	return secret, hashCredential(secret), secret[:len(credPrefix)+credShownChars]
}

func hashCredential(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// listCredentials is GET /v1/repos/credentials: the caller's live
// credentials (not revoked, not expired), newest first.
func (s *Service) listCredentials(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	p := httpx.ParsePage(r)
	var c cursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	rows, err := s.d.Pool.Query(r.Context(), `
		SELECT `+credentialCols+` FROM repos_git_credentials
		WHERE user_id = $1 AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > now())
		  AND ($2::timestamptz IS NULL OR (created_at, id) < ($2, $3))
		ORDER BY created_at DESC, id DESC LIMIT $4`, me.UserID, nullTime(c.T), c.ID, p.Limit+1)
	if err != nil {
		return err
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Credential])
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(list, p.Limit, func(c Credential) any { return cursor{T: c.CreatedAt, ID: c.ID} }))
	return nil
}

type createCredentialReq struct {
	Name          string `json:"name"`
	Access        string `json:"access"`
	ExpiresInDays *int   `json:"expires_in_days"` // nil or 0: never
}

// createCredential is POST /v1/repos/credentials. The secret is in this
// response only.
func (s *Service) createCredential(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	if _, ok := auth.AgentFrom(r.Context()); ok {
		return errAgentForbidden
	}
	var req createCredentialReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	name := strings.Join(strings.Fields(req.Name), " ")
	if n := len([]rune(name)); n == 0 || n > 60 {
		return httpx.BadRequest("Name the credential (up to 60 characters), for example the computer it's for.")
	}
	if req.Access != "read" && req.Access != "write" {
		return httpx.BadRequest("Access must be read or write.")
	}
	var expires *time.Time
	if req.ExpiresInDays != nil && *req.ExpiresInDays != 0 {
		d := *req.ExpiresInDays
		if d < 1 || d > 366 {
			return httpx.BadRequest("Expiry must be 1 to 366 days, or never.")
		}
		t := time.Now().Add(time.Duration(d) * 24 * time.Hour)
		expires = &t
	}
	secret, hash, prefix := newCredentialSecret()
	var cred Credential
	err := db.WithTx(r.Context(), s.d.Pool, func(tx pgx.Tx) error {
		// One creator at a time per person, so the cap holds.
		if _, err := tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(hashtextextended('repos.cred:' || $1::text, 0))`, me.UserID); err != nil {
			return err
		}
		var live int
		if err := tx.QueryRow(r.Context(), `SELECT count(*) FROM repos_git_credentials
			WHERE user_id = $1 AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > now())`, me.UserID).Scan(&live); err != nil {
			return err
		}
		if live >= maxCredentials {
			return httpx.NewError(http.StatusConflict, "credential_limit", "You have 20 git credentials. Revoke one to make another.")
		}
		rows, err := tx.Query(r.Context(), `
			INSERT INTO repos_git_credentials (user_id, name, prefix, secret_hash, access, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6) RETURNING `+credentialCols, me.UserID, name, prefix, hash, req.Access, expires)
		if err != nil {
			return err
		}
		cred, err = pgx.CollectExactlyOneRow(rows, pgx.RowToStructByPos[Credential])
		return err
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, CreatedCredential{Credential: cred, Secret: secret})
	return nil
}

// revokeCredential is DELETE /v1/repos/credentials/{id}.
func (s *Service) revokeCredential(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	tag, err := s.d.Pool.Exec(r.Context(), `UPDATE repos_git_credentials SET revoked_at = now()
		WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`, id, me.UserID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.NotFound("Credential not found.")
	}
	httpx.NoContent(w)
	return nil
}

// gitPrincipal is who a git request runs as.
type gitPrincipal struct {
	identity *auth.Identity
	credID   uuid.UUID
	write    bool // the credential allows pushes
}

// resolveCredential turns a git password into a person. It answers nil
// (no error) for anything that isn't a live credential of an active or
// suspended person; suspended people can fetch but not push.
func (s *Service) resolveCredential(ctx context.Context, secret string) (*gitPrincipal, error) {
	if !strings.HasPrefix(secret, credPrefix) || len(secret) != len(credPrefix)+credSecretLength {
		return nil, nil
	}
	p := &gitPrincipal{identity: &auth.Identity{}}
	id := p.identity
	var access string
	var role *string
	err := s.d.Pool.QueryRow(ctx, `
		SELECT c.id, c.access, u.id, u.auth_subject, u.email, u.display_name, u.admin_role, u.status
		FROM repos_git_credentials c JOIN users u ON u.id = c.user_id
		WHERE c.secret_hash = $1 AND c.revoked_at IS NULL AND (c.expires_at IS NULL OR c.expires_at > now())`,
		hashCredential(secret)).Scan(&p.credID, &access, &id.UserID, &id.Subject, &id.Email, &id.DisplayName, &role, &id.Status)
	if db.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if role != nil {
		id.AdminRole = *role
	}
	if id.Status != "active" && id.Status != "suspended" {
		return nil, nil
	}
	p.write = access == "write" && id.Status == "active"
	// At most one write a minute per credential.
	if _, err := s.d.Pool.Exec(ctx, `UPDATE repos_git_credentials SET last_used_at = now()
		WHERE id = $1 AND (last_used_at IS NULL OR last_used_at < now() - interval '1 minute')`, p.credID); err != nil {
		return nil, err
	}
	return p, nil
}
