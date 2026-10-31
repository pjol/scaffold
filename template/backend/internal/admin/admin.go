// Package admin is the core console API: user lookup, status and role
// changes, and the audit log. Features add their own admin routes through
// app.AdminRoutes; every admin mutation writes audit.Log in its transaction.
package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/audit"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// Deleter is the account deletion entry point (accounts.Service).
type Deleter interface {
	DeleteAccount(ctx context.Context, userID uuid.UUID, source string) error
}

type Service struct {
	d       *app.Deps
	deleter Deleter
}

func New(d *app.Deps, deleter Deleter) *Service { return &Service{d: d, deleter: deleter} }

func (s *Service) Name() string { return "admin" }

func (s *Service) MountAdmin(r chi.Router) {
	r.Get("/overview", httpx.Handler(s.overview))
	r.Get("/users", httpx.Handler(s.listUsers))
	r.Get("/users/{id}", httpx.Handler(s.getUser))
	r.Patch("/users/{id}", httpx.Handler(s.updateUser))
	r.With(auth.RequireAdminRole("admin")).Delete("/users/{id}", httpx.Handler(s.deleteUser))
	r.Get("/audit", httpx.Handler(s.listAudit))
}

// AdminUser is a user as staff see it.
type AdminUser struct {
	ID          uuid.UUID  `json:"id"`
	Email       string     `json:"email"`
	DisplayName string     `json:"display_name"`
	AvatarURL   *string    `json:"avatar_url"`
	AdminRole   *string    `json:"admin_role"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	DeletedAt   *time.Time `json:"deleted_at"`
}

const userCols = `id, email, display_name, avatar_url, admin_role, status, created_at, deleted_at`

func (s *Service) overview(w http.ResponseWriter, r *http.Request) error {
	var out struct {
		Users     int `json:"users"`
		New7d     int `json:"new_7d"`
		Suspended int `json:"suspended"`
	}
	err := s.d.Pool.QueryRow(r.Context(), `
		SELECT count(*) FILTER (WHERE status <> 'deleted'),
		       count(*) FILTER (WHERE status <> 'deleted' AND created_at > now() - interval '7 days'),
		       count(*) FILTER (WHERE status IN ('suspended', 'banned'))
		FROM users`).Scan(&out.Users, &out.New7d, &out.Suspended)
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

type cursor struct {
	T  time.Time `json:"t"`
	ID uuid.UUID `json:"id"`
}

func (s *Service) listUsers(w http.ResponseWriter, r *http.Request) error {
	p := httpx.ParsePage(r)
	var c cursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	status := r.URL.Query().Get("status")
	rows, err := s.d.Pool.Query(r.Context(), `
		SELECT `+userCols+` FROM users
		WHERE ($1 = '' OR email ILIKE '%' || $1 || '%' OR display_name ILIKE '%' || $1 || '%' OR id::text = $1)
		  AND ($2 = '' OR status = $2)
		  AND ($3::timestamptz IS NULL OR (created_at, id) < ($3, $4))
		ORDER BY created_at DESC, id DESC LIMIT $5`,
		q, status, nullTime(c.T), c.ID, p.Limit+1)
	if err != nil {
		return err
	}
	users, err := pgx.CollectRows(rows, pgx.RowToStructByPos[AdminUser])
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(users, p.Limit, func(u AdminUser) any { return cursor{u.CreatedAt, u.ID} }))
	return nil
}

func (s *Service) getUser(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	rows, err := s.d.Pool.Query(r.Context(), `SELECT `+userCols+` FROM users WHERE id = $1`, id)
	if err != nil {
		return err
	}
	u, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByPos[AdminUser])
	if db.IsNoRows(err) {
		return httpx.ErrNotFound
	}
	if err != nil {
		return err
	}
	httpx.OK(w, u)
	return nil
}

type updateUserReq struct {
	Status    *string `json:"status"`     // active | suspended | banned
	AdminRole *string `json:"admin_role"` // "" | support | admin (admins only)
}

func (s *Service) updateUser(w http.ResponseWriter, r *http.Request) error {
	staff := auth.MustFromContext(r.Context())
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req updateUserReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.Status != nil {
		switch *req.Status {
		case "active", "suspended", "banned":
		default:
			return httpx.BadRequest("Status must be active, suspended or banned.")
		}
	}
	if req.AdminRole != nil {
		if !staff.IsAdmin() {
			return httpx.Forbidden("Only admins can change roles.")
		}
		switch *req.AdminRole {
		case "", "support", "admin":
		default:
			return httpx.BadRequest("Role must be support, admin or empty.")
		}
	}
	if id == staff.UserID {
		return httpx.BadRequest("You can't change your own account here.")
	}
	err = db.WithTx(r.Context(), s.d.Pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(r.Context(), `
			UPDATE users SET
			  status = COALESCE($2, status),
			  admin_role = CASE WHEN $3::text IS NULL THEN admin_role ELSE NULLIF($3, '') END,
			  updated_at = now()
			WHERE id = $1 AND status <> 'deleted'`, id, req.Status, req.AdminRole)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.ErrNotFound
		}
		return audit.Log(r.Context(), tx, staff.UserID, "user.update", "user", id.String(), req)
	})
	if err != nil {
		return err
	}
	return s.getUser(w, r)
}

func (s *Service) deleteUser(w http.ResponseWriter, r *http.Request) error {
	staff := auth.MustFromContext(r.Context())
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	if id == staff.UserID {
		return httpx.BadRequest("Delete your own account from Settings.")
	}
	if err := audit.Log(r.Context(), s.d.Pool, staff.UserID, "user.delete", "user", id.String(), nil); err != nil {
		return err
	}
	if err := s.deleter.DeleteAccount(context.WithoutCancel(r.Context()), id, "admin"); err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

// AuditEntry is one admin_audit row.
type AuditEntry struct {
	ID         int64           `json:"id"`
	AdminID    *uuid.UUID      `json:"admin_id"`
	AdminEmail *string         `json:"admin_email"`
	Action     string          `json:"action"`
	TargetType string          `json:"target_type"`
	TargetID   string          `json:"target_id"`
	Data       json.RawMessage `json:"data"`
	CreatedAt  time.Time       `json:"created_at"`
}

func (s *Service) listAudit(w http.ResponseWriter, r *http.Request) error {
	p := httpx.ParsePage(r)
	var c struct {
		ID int64 `json:"id"`
	}
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	q := r.URL.Query()
	rows, err := s.d.Pool.Query(r.Context(), `
		SELECT a.id, a.admin_id, u.email, a.action, a.target_type, a.target_id, a.data, a.created_at
		FROM admin_audit a LEFT JOIN users u ON u.id = a.admin_id
		WHERE ($1 = '' OR a.target_type = $1) AND ($2 = '' OR a.target_id = $2)
		  AND ($3 = 0 OR a.id < $3)
		ORDER BY a.id DESC LIMIT $4`, q.Get("target_type"), q.Get("target_id"), c.ID, p.Limit+1)
	if err != nil {
		return err
	}
	entries, err := pgx.CollectRows(rows, pgx.RowToStructByPos[AuditEntry])
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(entries, p.Limit, func(e AuditEntry) any { return map[string]int64{"id": e.ID} }))
	return nil
}

func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
