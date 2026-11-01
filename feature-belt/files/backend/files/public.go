package files

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// Public links (anyone with the URL, no account) resolve through access:
// ResolveLink for the shared item, LinkLevel for anything under a shared
// folder. Both apply the live checks (revoked, expired, the creator still
// holding access.share_public). Staff can turn public access off for an item
// and everything under it; trashed items are never public.

// PublicItem is an item as a public link shows it: no people, no sharing.
type PublicItem struct {
	ID        uuid.UUID  `json:"id"`
	ParentID  *uuid.UUID `json:"parent_id"` // null at the link's own item
	Type      string     `json:"type"`
	Name      string     `json:"name"`
	MimeType  string     `json:"mime_type"`
	Size      int64      `json:"size"`
	Preview   string     `json:"preview"`
	UpdatedAt time.Time  `json:"updated_at"`
	TypeInfo  *TypeInfo  `json:"type_info"`
	ThumbURL  *string    `json:"thumb_url"`
}

// PublicView is GET /v1/files/public/{token}[/items/{id}].
type PublicView struct {
	Level     access.Level `json:"level"`
	ExpiresAt *time.Time   `json:"expires_at"`
	RootID    uuid.UUID    `json:"root_id"`
	Item      PublicItem   `json:"item"`
	Path      []Crumb      `json:"path"` // from the link's item down to the parent
}

var errPublicDisabled = httpx.NewError(http.StatusNotFound, "public_disabled", "This item isn't available publicly anymore.")

func toPublic(it *Item, root uuid.UUID) PublicItem {
	p := PublicItem{ID: it.ID, ParentID: it.ParentID, Type: it.Type, Name: it.Name, MimeType: it.MimeType, Size: it.Size,
		Preview: it.Preview, UpdatedAt: it.UpdatedAt, TypeInfo: it.TypeInfo, ThumbURL: it.ThumbURL}
	if it.ID == root {
		p.ParentID = nil
	}
	return p
}

// publicTarget resolves a token and an item under it (nil: the link's own
// item) to the item, the link and the path from the link's item.
func (s *Service) publicTarget(ctx context.Context, token string, id *uuid.UUID) (*access.Link, *Item, []Crumb, error) {
	l, err := access.ResolveLink(ctx, s.d.Pool, token)
	if err != nil {
		return nil, nil, nil, err
	}
	if l.Ref.Kind != Kind {
		return nil, nil, nil, access.ErrLinkNotFound
	}
	target := l.Ref.ID
	if id != nil && *id != target {
		lvl, err := access.LinkLevel(ctx, s.d.Pool, token, Kind, *id)
		if err != nil {
			return nil, nil, nil, err
		}
		if lvl < access.View {
			return nil, nil, nil, access.ErrLinkNotFound
		}
		target = *id
	}
	it, err := s.getItem(ctx, s.d.Pool, uuid.Nil, target)
	if err != nil {
		if errors.Is(err, errItemNotFound) {
			return nil, nil, nil, access.ErrLinkNotFound
		}
		return nil, nil, nil, err
	}
	if it.TrashedAt != nil {
		return nil, nil, nil, access.ErrLinkNotFound
	}
	rows, err := s.d.Pool.Query(ctx, `
		WITH RECURSIVE up AS (
			SELECT id, parent_id, name, type, public_disabled_at, 0 AS depth FROM files_items WHERE id = $1
			UNION ALL
			SELECT p.id, p.parent_id, p.name, p.type, p.public_disabled_at, up.depth + 1
			FROM up JOIN files_items p ON p.id = up.parent_id WHERE up.id <> $2 AND up.depth < 64
		)
		SELECT id, name, type, public_disabled_at IS NOT NULL FROM up ORDER BY depth`, target, l.Ref.ID)
	if err != nil {
		return nil, nil, nil, err
	}
	defer rows.Close()
	var chain []Crumb
	disabled := false
	for rows.Next() {
		var c Crumb
		var off bool
		if err := rows.Scan(&c.ID, &c.Name, &c.Type, &off); err != nil {
			return nil, nil, nil, err
		}
		disabled = disabled || off
		if c.ID != target {
			chain = append(chain, c)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, nil, err
	}
	if disabled {
		return nil, nil, nil, errPublicDisabled
	}
	path := make([]Crumb, 0, len(chain))
	for i := len(chain) - 1; i >= 0; i-- {
		path = append(path, chain[i])
	}
	return l, it, path, nil
}

func (s *Service) publicView(ctx context.Context, w http.ResponseWriter, token string, id *uuid.UUID) error {
	l, it, path, err := s.publicTarget(ctx, token, id)
	if err != nil {
		return err
	}
	s.addThumbs(ctx, s.d.Pool, []*Item{it})
	httpx.OK(w, PublicView{Level: l.Level, ExpiresAt: l.ExpiresAt, RootID: l.Ref.ID, Item: toPublic(it, l.Ref.ID), Path: path})
	return nil
}

// publicRoot: GET /v1/files/public/{token} (public, per-IP limit).
func (s *Service) publicRoot(w http.ResponseWriter, r *http.Request) error {
	return s.publicView(r.Context(), w, chi.URLParam(r, "token"), nil)
}

// publicItem: GET /v1/files/public/{token}/items/{id} — an item under a
// shared folder.
func (s *Service) publicItem(w http.ResponseWriter, r *http.Request) error {
	id, err := idParam(r)
	if err != nil {
		return access.ErrLinkNotFound
	}
	return s.publicView(r.Context(), w, chi.URLParam(r, "token"), &id)
}

// publicChildren: GET /v1/files/public/{token}/items/{id}/children?sort=&dir=&cursor=
// — what a shared folder holds that the link opens (restricted subfolders
// and disabled items are left out).
func (s *Service) publicChildren(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	token := chi.URLParam(r, "token")
	id, err := idParam(r)
	if err != nil {
		return access.ErrLinkNotFound
	}
	l, folder, _, err := s.publicTarget(ctx, token, &id)
	if err != nil {
		return err
	}
	if !folder.IsFolder() {
		return errNotFolder
	}
	o := parseListOpts(r)
	o.Thumbs = true
	page, err := s.sortedPage(ctx, s.d.Pool, uuid.Nil, false,
		`i.parent_id = $3 AND i.trashed_at IS NULL AND i.public_disabled_at IS NULL`, folder.ID, o)
	if err != nil {
		return err
	}
	out := httpx.Page[PublicItem]{Items: []PublicItem{}, NextCursor: page.NextCursor}
	for _, it := range page.Items {
		lvl, err := access.LinkLevel(ctx, s.d.Pool, token, Kind, it.ID)
		if err != nil {
			return err
		}
		if lvl >= access.View {
			out.Items = append(out.Items, toPublic(it, l.Ref.ID))
		}
	}
	httpx.OK(w, out)
	return nil
}

// publicURL: GET /v1/files/public/{token}/items/{id}/url?disposition=inline|attachment
// (FILES_PUBLIC_DOWNLOADS_PER_MIN per IP). Counts a view or a download.
func (s *Service) publicURL(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id, err := idParam(r)
	if err != nil {
		return access.ErrLinkNotFound
	}
	_, it, _, err := s.publicTarget(ctx, chi.URLParam(r, "token"), &id)
	if err != nil {
		return err
	}
	inline := r.URL.Query().Get("disposition") == "inline"
	u, err := s.signedURL(ctx, it, nil, inline)
	if err != nil {
		return err
	}
	col := "downloads"
	if u.Disposition == "inline" {
		col = "views"
	}
	if _, err := s.d.Pool.Exec(ctx, `
		INSERT INTO files_public_stats (item_id, day, `+col+`) VALUES ($1, current_date, 1)
		ON CONFLICT (item_id, day) DO UPDATE SET `+col+` = files_public_stats.`+col+` + 1`, it.ID); err != nil {
		return err
	}
	httpx.OK(w, u)
	return nil
}

// publicText: GET /v1/files/public/{token}/items/{id}/text — a text file's
// content for the preview.
func (s *Service) publicText(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id, err := idParam(r)
	if err != nil {
		return access.ErrLinkNotFound
	}
	_, it, _, err := s.publicTarget(ctx, chi.URLParam(r, "token"), &id)
	if err != nil {
		return err
	}
	out, err := s.textContent(ctx, it, nil)
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

// publiclyReachable reports whether a working public link opens an item
// (on it, or on an ancestor it inherits from).
func (s *Service) publiclyReachable(ctx context.Context, q db.Querier, id uuid.UUID) (bool, error) {
	p, _ := access.PermissionOf(access.PermSharePublic)
	var ok bool
	err := q.QueryRow(ctx, `
		WITH RECURSIVE up AS (
			SELECT kind, id, parent_kind, parent_id, inherits, 0 AS depth FROM access_resources WHERE kind = 'files.item' AND id = $1
			UNION ALL
			SELECT r.kind, r.id, r.parent_kind, r.parent_id, r.inherits, up.depth + 1
			FROM up JOIN access_resources r ON r.kind = up.parent_kind AND r.id = up.parent_id
			WHERE up.inherits AND up.depth < 31
		)
		SELECT EXISTS (
			SELECT 1 FROM up
			JOIN access_links l ON l.resource_kind = up.kind AND l.resource_id = up.id
			JOIN access_resources ar ON ar.kind = l.resource_kind AND ar.id = l.resource_id
			WHERE l.revoked_at IS NULL AND (l.expires_at IS NULL OR l.expires_at > now()) AND l.created_by IS NOT NULL
			  AND (ar.org_id IS NULL OR access_permission_allowed(ar.org_id, l.created_by, 'access.share_public', $2)))`,
		id, p.DefaultRoles).Scan(&ok)
	return ok, err
}
