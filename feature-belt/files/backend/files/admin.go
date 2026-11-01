package files

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/audit"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/moderation"
	"scaffoldapp/backend/internal/notify"
	"scaffoldapp/backend/internal/orgs"
)

// Staff see what is shared publicly (with traffic) and turn public access
// off for an item and everything under it. Reports on files come through the
// moderation queue (target files.item); its "remove" does the same.

// AdminPublicItem is a publicly shared (or staff-disabled) item for staff.
type AdminPublicItem struct {
	ID             uuid.UUID  `json:"id"`
	Name           string     `json:"name"`
	Type           string     `json:"type"`
	MimeType       string     `json:"mime_type"`
	Size           int64      `json:"size"`
	Preview        string     `json:"preview"`
	OrgID          uuid.UUID  `json:"org_id"`
	OrgName        string     `json:"org_name"`
	CreatedBy      *Person    `json:"created_by"`
	Links          int        `json:"links"`           // working links on the item itself
	LinkCreatedAt  *time.Time `json:"link_created_at"` // newest
	Views30d       int        `json:"views_30d"`
	Downloads30d   int        `json:"downloads_30d"`
	DisabledAt     *time.Time `json:"disabled_at"`
	DisabledReason string     `json:"disabled_reason"`
	DisabledBy     *Person    `json:"disabled_by"`
	TrashedAt      *time.Time `json:"trashed_at"`
	URL            *string    `json:"url,omitempty"` // detail only: a preview link
}

const adminItemSelect = `
	SELECT i.id, i.name, i.type, i.mime_type, i.size, i.text_like, i.org_id, o.name,
	       i.created_by, cu.display_name, cu.avatar_url,
	       (SELECT count(*) FROM access_links k WHERE k.resource_kind = 'files.item' AND k.resource_id = i.id
	          AND k.revoked_at IS NULL AND (k.expires_at IS NULL OR k.expires_at > now()))::int,
	       (SELECT max(k.created_at) FROM access_links k WHERE k.resource_kind = 'files.item' AND k.resource_id = i.id
	          AND k.revoked_at IS NULL AND (k.expires_at IS NULL OR k.expires_at > now())),
	       COALESCE((SELECT sum(views) FROM files_public_stats st WHERE st.item_id = i.id AND st.day > current_date - 30), 0)::int,
	       COALESCE((SELECT sum(downloads) FROM files_public_stats st WHERE st.item_id = i.id AND st.day > current_date - 30), 0)::int,
	       i.public_disabled_at, i.public_disabled_reason, i.public_disabled_by, du.display_name, du.avatar_url, i.trashed_at
	FROM files_items i
	JOIN orgs o ON o.id = i.org_id
	LEFT JOIN users cu ON cu.id = i.created_by
	LEFT JOIN users du ON du.id = i.public_disabled_by`

func scanAdminItem(row pgx.Row) (*AdminPublicItem, error) {
	var a AdminPublicItem
	var textLike bool
	var cb, db_ *uuid.UUID
	var cn, ca, dn, da *string
	if err := row.Scan(&a.ID, &a.Name, &a.Type, &a.MimeType, &a.Size, &textLike, &a.OrgID, &a.OrgName, &cb, &cn, &ca,
		&a.Links, &a.LinkCreatedAt, &a.Views30d, &a.Downloads30d, &a.DisabledAt, &a.DisabledReason, &db_, &dn, &da, &a.TrashedAt); err != nil {
		return nil, err
	}
	a.Preview = previewKind(a.Type, a.MimeType, a.Name, textLike)
	a.CreatedBy = person(cb, cn, ca)
	a.DisabledBy = person(db_, dn, da)
	return &a, nil
}

type adminCursor struct {
	T  time.Time `json:"t"`
	ID uuid.UUID `json:"id"`
}

// adminPublicList: GET /v1/admin/files/public?status=active|disabled&cursor= (support).
func (s *Service) adminPublicList(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	p := httpx.ParsePage(r)
	status := r.URL.Query().Get("status")
	var where, at string
	switch status {
	case "", "active":
		at = `(SELECT max(k.created_at) FROM access_links k WHERE k.resource_kind = 'files.item' AND k.resource_id = i.id
			AND k.revoked_at IS NULL AND (k.expires_at IS NULL OR k.expires_at > now()))`
		where = `i.trashed_at IS NULL AND i.public_disabled_at IS NULL AND ` + at + ` IS NOT NULL`
	case "disabled":
		at = `i.public_disabled_at`
		where = `i.public_disabled_at IS NOT NULL`
	default:
		return httpx.BadRequest("status must be active or disabled.")
	}
	args := []any{}
	sql := adminItemSelect + ` WHERE ` + where
	if p.Cursor != "" {
		var c adminCursor
		if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
			return err
		}
		args = append(args, c.T, c.ID)
		sql += ` AND (` + at + `, i.id) < ($1, $2)`
	}
	args = append(args, p.Limit+1)
	sql += fmt.Sprintf(` ORDER BY %s DESC, i.id DESC LIMIT $%d`, at, len(args))
	rows, err := s.d.Pool.Query(ctx, sql, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	list := []*AdminPublicItem{}
	for rows.Next() {
		a, err := scanAdminItem(rows)
		if err != nil {
			return err
		}
		list = append(list, a)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(list, p.Limit, func(a *AdminPublicItem) any {
		t := a.DisabledAt
		if status != "disabled" {
			t = a.LinkCreatedAt
		}
		if t == nil {
			t = &time.Time{}
		}
		return adminCursor{T: *t, ID: a.ID}
	}))
	return nil
}

// adminItem: GET /v1/admin/files/items/{id} (support). Staff get a preview
// link only for items that are or were public, or that someone reported.
func (s *Service) adminItem(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id, err := idParam(r)
	if err != nil {
		return err
	}
	a, err := scanAdminItem(s.d.Pool.QueryRow(ctx, adminItemSelect+` WHERE i.id = $1`, id))
	if db.IsNoRows(err) {
		return errItemNotFound
	}
	if err != nil {
		return err
	}
	var exposed bool
	if err := s.d.Pool.QueryRow(ctx, `
		SELECT $2 OR EXISTS (SELECT 1 FROM access_links WHERE resource_kind = 'files.item' AND resource_id = $1)
		    OR EXISTS (SELECT 1 FROM moderation_cases WHERE target_kind = 'files.item' AND target_id = $1::text)`,
		id, a.DisabledAt != nil).Scan(&exposed); err != nil {
		return err
	}
	if !exposed {
		if exposed, err = s.publiclyReachable(ctx, s.d.Pool, id); err != nil {
			return err
		}
	}
	if exposed && a.Type == TypeFile {
		it, err := s.getItem(ctx, s.d.Pool, uuid.Nil, id)
		if err == nil {
			if u, err := s.signedURL(ctx, it, nil, true); err == nil {
				a.URL = &u.URL
			}
		}
	}
	httpx.OK(w, a)
	return nil
}

type disableReq struct {
	Reason string `json:"reason"`
}

// adminDisable: POST /v1/admin/files/items/{id}/disable {reason} (support,
// audited). Public links stop opening the item and everything under it; the
// people who can open it in the org still can. Tells its owner.
func (s *Service) adminDisable(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := idParam(r)
	if err != nil {
		return err
	}
	var req disableReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	reason := strings.TrimSpace(req.Reason)
	if len([]rune(reason)) > 500 {
		return httpx.BadRequest("Keep the reason under 500 characters.")
	}
	var notifyUser *uuid.UUID
	var name string
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var orgID uuid.UUID
		err := tx.QueryRow(ctx, `
			UPDATE files_items SET public_disabled_at = now(), public_disabled_by = $2, public_disabled_reason = $3
			WHERE id = $1 RETURNING org_id, name, COALESCE(owner_id, created_by)`, id, me, reason).Scan(&orgID, &name, &notifyUser)
		if db.IsNoRows(err) {
			return errItemNotFound
		}
		if err != nil {
			return err
		}
		return audit.Log(ctx, tx, me, "files.public.disable", Kind, id.String(), map[string]any{"org_id": orgID, "reason": reason})
	})
	if err != nil {
		return err
	}
	if notifyUser != nil && s.d.Notify != nil {
		if err := s.d.Notify.Send(context.WithoutCancel(ctx), notify.Notification{
			UserID: *notifyUser, Kind: KindPublicDisabled,
			Title: "Public links to “" + name + "” were turned off",
			Body:  "Staff turned off public access. People in your " + orgs.Noun() + " can still open it.",
			Link:  itemLink(id),
		}); err != nil {
			slog.WarnContext(ctx, "files: notify disable", "err", err)
		}
	}
	return s.adminItem(w, r)
}

// adminEnable: POST /v1/admin/files/items/{id}/enable (support, audited).
func (s *Service) adminEnable(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := idParam(r)
	if err != nil {
		return err
	}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var orgID uuid.UUID
		err := tx.QueryRow(ctx, `
			UPDATE files_items SET public_disabled_at = NULL, public_disabled_by = NULL, public_disabled_reason = ''
			WHERE id = $1 RETURNING org_id`, id).Scan(&orgID)
		if db.IsNoRows(err) {
			return errItemNotFound
		}
		if err != nil {
			return err
		}
		return audit.Log(ctx, tx, me, "files.public.enable", Kind, id.String(), map[string]any{"org_id": orgID})
	})
	if err != nil {
		return err
	}
	return s.adminItem(w, r)
}

// ---- moderation target ---------------------------------------------------------------

// itemTarget lets people report a file or folder (signed in: one they can
// open, or one a public link opens). Remove turns public access off.
type itemTarget struct{ s *Service }

func (t itemTarget) Snapshot(ctx context.Context, rawID string) (moderation.Snapshot, error) {
	id, err := uuid.Parse(rawID)
	if err != nil {
		return moderation.Snapshot{}, errItemNotFound
	}
	allowed := false
	if me := auth.FromContext(ctx); me != nil {
		lvl, err := access.LevelOf(ctx, t.s.d.Pool, me.UserID, Kind, id)
		if err != nil {
			return moderation.Snapshot{}, err
		}
		allowed = lvl >= access.View
	}
	if !allowed {
		if allowed, err = t.s.publiclyReachable(ctx, t.s.d.Pool, id); err != nil {
			return moderation.Snapshot{}, err
		}
	}
	if !allowed {
		return moderation.Snapshot{}, errItemNotFound
	}
	var it struct {
		name, typ, mt string
		size          int64
		textLike      bool
		author        *uuid.UUID
		content       *string
		key           *string
	}
	err = t.s.d.Pool.QueryRow(ctx, `
		SELECT i.name, i.type, i.mime_type, i.size, i.text_like, i.created_by, left(i.content_text, 600), v.storage_key
		FROM files_items i LEFT JOIN files_versions v ON v.id = i.version_id WHERE i.id = $1`, id).
		Scan(&it.name, &it.typ, &it.mt, &it.size, &it.textLike, &it.author, &it.content, &it.key)
	if db.IsNoRows(err) {
		return moderation.Snapshot{}, errItemNotFound
	}
	if err != nil {
		return moderation.Snapshot{}, err
	}
	text := it.name
	if it.typ == TypeFile {
		text += " (" + it.mt + ", " + formatSize(it.size) + ")"
	}
	if it.content != nil && *it.content != "" {
		text += "\n\n" + *it.content
	}
	snap := moderation.Snapshot{AuthorID: it.author, Text: text, MediaKeys: []string{}, Link: "/admin/files?item=" + id.String()}
	if it.key != nil && previewKind(it.typ, it.mt, it.name, it.textLike) == "image" {
		snap.MediaKeys = append(snap.MediaKeys, *it.key)
	}
	return snap, nil
}

func (t itemTarget) Remove(ctx context.Context, tx pgx.Tx, rawID string) error {
	_, err := tx.Exec(ctx, `
		UPDATE files_items SET public_disabled_at = now(), public_disabled_reason = 'Removed after a report'
		WHERE id = $1::uuid AND public_disabled_at IS NULL`, rawID)
	return err
}

func (t itemTarget) Restore(ctx context.Context, tx pgx.Tx, rawID string) error {
	_, err := tx.Exec(ctx, `
		UPDATE files_items SET public_disabled_at = NULL, public_disabled_by = NULL, public_disabled_reason = ''
		WHERE id = $1::uuid AND public_disabled_reason = 'Removed after a report'`, rawID)
	return err
}

func formatSize(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%d KB", b>>10)
	}
	return fmt.Sprintf("%d bytes", b)
}
