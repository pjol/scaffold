package files

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// Public link options. The links themselves are access's (Share… creates,
// expires and revokes them); files adds a password, downloads off, and a
// count of opens, keyed by the link. People who manage the item change
// them; agents never do.

const unlockTTL = 12 * time.Hour

var (
	errLinkPassword = httpx.NewError(http.StatusUnauthorized, "link_password_required", "This link needs a password.")
	errWrongPass    = httpx.NewError(http.StatusUnauthorized, "link_password_wrong", "That password isn't right.")
	errDownloadOff  = httpx.NewError(http.StatusForbidden, "download_disabled", "The owner turned downloads off for this link.")
)

// LinkOptions are a public link's files options.
type LinkOptions struct {
	HasPassword   bool       `json:"has_password"`
	AllowDownload bool       `json:"allow_download"`
	Opens         int64      `json:"opens"`
	LastOpenedAt  *time.Time `json:"last_opened_at"`
	passwordHash  *string
}

// linkOptions reads a link's options (defaults when none were set).
func (s *Service) linkOptions(ctx context.Context, linkID uuid.UUID) (LinkOptions, error) {
	o := LinkOptions{AllowDownload: true}
	err := s.d.Pool.QueryRow(ctx, `SELECT password_hash, allow_download, opens, last_opened_at FROM files_link_settings WHERE link_id = $1`, linkID).
		Scan(&o.passwordHash, &o.AllowDownload, &o.Opens, &o.LastOpenedAt)
	if db.IsNoRows(err) {
		return LinkOptions{AllowDownload: true}, nil
	}
	o.HasPassword = o.passwordHash != nil
	return o, err
}

func hashUnlock(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

// checkUnlocked passes links without a password, and links with one when
// proof shows the visitor typed it (live, 12 hours).
func (s *Service) checkUnlocked(ctx context.Context, linkID uuid.UUID, proof string) error {
	o, err := s.linkOptions(ctx, linkID)
	if err != nil || !o.HasPassword {
		return err
	}
	if proof == "" || len(proof) > 100 {
		return errLinkPassword
	}
	var ok bool
	if err := s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM files_link_unlocks WHERE token_hash = $1 AND link_id = $2 AND expires_at > now())`,
		hashUnlock(proof), linkID).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return errLinkPassword
	}
	return nil
}

// countOpen records that a link was opened (best effort).
func (s *Service) countOpen(ctx context.Context, l *access.Link) {
	if _, err := s.d.Pool.Exec(ctx, `
		INSERT INTO files_link_settings (link_id, item_id, opens, last_opened_at) VALUES ($1, $2, 1, now())
		ON CONFLICT (link_id) DO UPDATE SET opens = files_link_settings.opens + 1, last_opened_at = now()`, l.ID, l.Ref.ID); err != nil {
		slog.WarnContext(ctx, "files: count link open", "err", err)
	}
}

type unlockReq struct {
	Password string `json:"password"`
}

// publicPassword: POST /v1/files/public/{token}/password {password} (public, 10
// tries per IP per minute) → {proof, expires_at}: pass it as ?proof= on
// the link's other public routes.
func (s *Service) publicPassword(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	var req unlockReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	l, err := access.ResolveLink(ctx, s.d.Pool, chi.URLParam(r, "token"))
	if err != nil {
		return err
	}
	if l.Ref.Kind != Kind {
		return access.ErrLinkNotFound
	}
	o, err := s.linkOptions(ctx, l.ID)
	if err != nil {
		return err
	}
	if !o.HasPassword {
		httpx.OK(w, map[string]any{"proof": "", "expires_at": nil})
		return nil
	}
	if bcrypt.CompareHashAndPassword([]byte(*o.passwordHash), []byte(req.Password)) != nil {
		return errWrongPass
	}
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return err
	}
	token := base64.RawURLEncoding.EncodeToString(b[:])
	exp := time.Now().Add(unlockTTL)
	if _, err := s.d.Pool.Exec(ctx, `INSERT INTO files_link_unlocks (token_hash, link_id, expires_at) VALUES ($1, $2, $3)`, hashUnlock(token), l.ID, exp); err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"proof": token, "expires_at": exp})
	return nil
}

// PublicLinkInfo is a working public link on an item as its managers see it.
type PublicLinkInfo struct {
	ID        uuid.UUID    `json:"id"`
	Level     access.Level `json:"level"`
	ExpiresAt *time.Time   `json:"expires_at"`
	CreatedBy *Person      `json:"created_by"`
	CreatedAt time.Time    `json:"created_at"`
	LinkOptions
}

// publicLinks: GET /v1/files/items/{id}/public-links (manage) → the item's
// working links with their options.
func (s *Service) publicLinks(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := idParam(r)
	if err != nil {
		return err
	}
	if _, err := s.load(ctx, s.d.Pool, me, id, access.Manage); err != nil && !isLocked(err) {
		return err
	}
	list, err := s.listPublicLinks(ctx, s.d.Pool, id)
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": list})
	return nil
}

func (s *Service) listPublicLinks(ctx context.Context, q db.Querier, itemID uuid.UUID) ([]PublicLinkInfo, error) {
	rows, err := q.Query(ctx, `
		SELECT k.id, k.level, k.expires_at, k.created_by, u.display_name, u.avatar_url, k.created_at,
		       st.password_hash IS NOT NULL, COALESCE(st.allow_download, true), COALESCE(st.opens, 0), st.last_opened_at
		FROM access_links k LEFT JOIN users u ON u.id = k.created_by LEFT JOIN files_link_settings st ON st.link_id = k.id
		WHERE k.resource_kind = $1 AND k.resource_id = $2 AND k.revoked_at IS NULL AND (k.expires_at IS NULL OR k.expires_at > now())
		ORDER BY k.created_at DESC LIMIT 50`, Kind, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PublicLinkInfo{}
	for rows.Next() {
		var l PublicLinkInfo
		var lvl int
		var by *uuid.UUID
		var name, avatar *string
		if err := rows.Scan(&l.ID, &lvl, &l.ExpiresAt, &by, &name, &avatar, &l.CreatedAt, &l.HasPassword, &l.AllowDownload, &l.Opens, &l.LastOpenedAt); err != nil {
			return nil, err
		}
		l.Level = access.Level(lvl)
		l.CreatedBy = person(by, name, avatar)
		out = append(out, l)
	}
	return out, rows.Err()
}

type linkOptionsReq struct {
	// Password: a new password (4–128 characters); "" removes it; omitted keeps it.
	Password      *string `json:"password"`
	AllowDownload *bool   `json:"allow_download"`
}

// putLinkOptions: PUT /v1/files/items/{id}/public-links/{linkID} {password?, allow_download?} (manage; people only).
func (s *Service) putLinkOptions(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	if access.ViaAgent(ctx) {
		return httpx.NewError(http.StatusForbidden, "agent_forbidden", "AI agents can't change public links.")
	}
	id, err := idParam(r)
	if err != nil {
		return err
	}
	linkID, err := uuid.Parse(chi.URLParam(r, "linkID"))
	if err != nil {
		return httpx.NotFound("Link not found.")
	}
	var req linkOptionsReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	// Who may change it comes first; then the password's own rules.
	if _, err := s.load(ctx, s.d.Pool, me, id, access.Manage); err != nil && !isLocked(err) {
		return err
	}
	var hash *string
	if req.Password != nil && *req.Password != "" {
		if n := utf8.RuneCountInString(*req.Password); n < 4 || n > 128 {
			return httpx.BadRequest("Use a password of 4 to 128 characters.")
		}
		h, err := bcrypt.GenerateFromPassword([]byte(*req.Password), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		v := string(h)
		hash = &v
	}
	var out []PublicLinkInfo
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if _, err := s.load(ctx, tx, me, id, access.Manage); err != nil && !isLocked(err) {
			return err
		}
		var ok bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM access_links WHERE id = $1 AND resource_kind = $2 AND resource_id = $3 AND revoked_at IS NULL)`,
			linkID, Kind, id).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return httpx.NotFound("Link not found.")
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO files_link_settings (link_id, item_id, password_hash, allow_download, updated_by, updated_at)
			VALUES ($1, $2, $3, COALESCE($4, true), $5, now())
			ON CONFLICT (link_id) DO UPDATE SET
				password_hash = CASE WHEN $6 THEN $3 ELSE files_link_settings.password_hash END,
				allow_download = COALESCE($4, files_link_settings.allow_download),
				updated_by = $5, updated_at = now()`,
			linkID, id, hash, req.AllowDownload, me, req.Password != nil); err != nil {
			return err
		}
		if req.Password != nil {
			if _, err := tx.Exec(ctx, `DELETE FROM files_link_unlocks WHERE link_id = $1`, linkID); err != nil {
				return err
			}
		}
		out, err = s.listPublicLinks(ctx, tx, id)
		return err
	})
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": out})
	return nil
}

// isLocked reports a lock refusal (managers change link options on locked files).
func isLocked(err error) bool {
	var e *httpx.Error
	return errors.As(err, &e) && e.Code == "item_locked"
}
