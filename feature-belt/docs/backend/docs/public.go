package docs

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/httpx"
)

// PublicPage is a published page as anyone with the link sees it: read-only,
// without who wrote what, plus the subpages the same link opens.
type PublicPage struct {
	Page     *Page         `json:"page"`
	Blocks   []Block       `json:"blocks"`
	Refs     Refs          `json:"refs"`
	Views    []View        `json:"views"`
	Children []PageSummary `json:"children"`
	Path     []Crumb       `json:"path"` // from the published page down to this one's parent
	RootID   uuid.UUID     `json:"root_id"`
	Database *DatabaseInfo `json:"database"`
}

// publicTarget resolves a link token to a page it opens (the published page,
// or id when given and inside it). Every refusal is the same 404: a bad,
// revoked or expired link, public sharing or publishing switched off for
// the link's creator (access checks both: docs.publish is the kind's
// LinkPermission), a trashed page, or one moderation took down.
func (s *Service) publicTarget(ctx context.Context, token string, id *uuid.UUID) (*Page, *access.Link, error) {
	l, err := access.ResolveLink(ctx, s.d.Pool, token)
	if err != nil {
		return nil, nil, err
	}
	if l.Ref.Kind != Kind {
		return nil, nil, access.ErrLinkNotFound
	}
	root, err := loadPage(ctx, s.d.Pool, l.Ref.ID, false)
	if err != nil || root.DeletedAt != nil {
		return nil, nil, access.ErrLinkNotFound
	}
	if blocked, err := s.publicBlocked(ctx, root.ID); err != nil || blocked {
		return nil, nil, access.ErrLinkNotFound
	}
	if id == nil || *id == root.ID {
		root.MyLevel = l.Level
		return root, l, nil
	}
	lvl, err := access.LinkLevel(ctx, s.d.Pool, token, Kind, *id)
	if err != nil || lvl < access.View {
		return nil, nil, errPageNotFound
	}
	p, err := loadPage(ctx, s.d.Pool, *id, false)
	if err != nil || p.DeletedAt != nil {
		return nil, nil, errPageNotFound
	}
	if blocked, err := s.publicBlocked(ctx, p.ID); err != nil || blocked {
		return nil, nil, errPageNotFound
	}
	p.MyLevel = lvl
	return p, l, nil
}

func (s *Service) publicBlocked(ctx context.Context, id uuid.UUID) (bool, error) {
	var blocked bool
	err := s.d.Pool.QueryRow(ctx, `SELECT public_blocked_at IS NOT NULL FROM docs_pages WHERE id = $1`, id).Scan(&blocked)
	return blocked, err
}

func scrub(p *Page) {
	p.CreatedBy, p.UpdatedBy = nil, nil
}

// publicPage: GET /docs/public/{token} and /docs/public/{token}/pages/{id}.
func (s *Service) publicPage(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	token := chi.URLParam(r, "token")
	var id *uuid.UUID
	if raw := chi.URLParam(r, "id"); raw != "" {
		v, err := uuid.Parse(raw)
		if err != nil {
			return errPageNotFound
		}
		id = &v
	}
	p, l, err := s.publicTarget(ctx, token, id)
	if err != nil {
		return err
	}
	detail, err := s.pageDetail(ctx, uuid.Nil, p, false)
	if err != nil {
		return err
	}
	scrub(p)
	for i := range detail.Blocks {
		detail.Blocks[i].CreatedBy, detail.Blocks[i].UpdatedBy = nil, nil
	}
	// Pages the same link opens keep their titles; others stay hidden.
	for key, rp := range detail.Refs.Pages {
		if lvl, err := access.LinkLevel(ctx, s.d.Pool, token, Kind, rp.ID); err == nil && lvl >= access.View {
			if target, err := loadPage(ctx, s.d.Pool, rp.ID, false); err == nil && target.DeletedAt == nil {
				t := displayTitle(target.Title)
				rp.Title, rp.Icon, rp.Kind = &t, target.Icon, target.Kind
				detail.Refs.Pages[key] = rp
			}
		}
	}
	if detail.Database != nil {
		if lvl, err := access.LinkLevel(ctx, s.d.Pool, token, Kind, detail.Database.ID); err == nil && lvl >= access.View {
			if dbp, err := loadPage(ctx, s.d.Pool, detail.Database.ID, false); err == nil {
				t := displayTitle(dbp.Title)
				detail.Database.Title = &t
			}
		}
	}
	out := PublicPage{Page: p, Blocks: detail.Blocks, Refs: detail.Refs, Views: detail.Views, Database: detail.Database, RootID: l.Ref.ID,
		Children: []PageSummary{}, Path: []Crumb{}}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT `+summaryCols+`, 1, false FROM docs_pages p
		JOIN access_resources ar ON ar.kind = 'docs.page' AND ar.id = p.id AND ar.inherits
		WHERE p.parent_id = $1 AND NOT p.is_row AND p.deleted_at IS NULL AND p.public_blocked_at IS NULL
		ORDER BY p.position, p.id LIMIT 200`, p.ID)
	if err != nil {
		return err
	}
	if out.Children, err = scanSummaries(rows, func(ps *PageSummary) []any { return []any{&ps.HasChildren} }); err != nil {
		return err
	}
	if p.ID != l.Ref.ID {
		rows, err := s.d.Pool.Query(ctx, `
			WITH RECURSIVE up (id, depth) AS (
				SELECT parent_id, 1 FROM docs_pages WHERE id = $1
				UNION ALL
				SELECT d.parent_id, up.depth + 1 FROM up JOIN docs_pages d ON d.id = up.id
				WHERE up.id <> $2 AND d.parent_id IS NOT NULL AND up.depth < 32
			)
			SELECT d.id, d.title, d.icon, d.kind FROM up JOIN docs_pages d ON d.id = up.id ORDER BY up.depth DESC`, p.ID, l.Ref.ID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var c Crumb
			if err := rows.Scan(&c.ID, &c.Title, &c.Icon, &c.Kind); err != nil {
				rows.Close()
				return err
			}
			c.Title = displayTitle(c.Title)
			out.Path = append(out.Path, c)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex")
	httpx.OK(w, out)
	return nil
}

// publicFile: GET /docs/public/{token}/files/{fileID}: a download link for
// a file on a page the link opens.
func (s *Service) publicFile(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	token := chi.URLParam(r, "token")
	fileID, err := httpx.URLParamUUID(r, "fileID")
	if err != nil {
		return err
	}
	f, key, err := s.loadFile(ctx, fileID)
	if err != nil {
		return err
	}
	if _, _, err := s.publicTarget(ctx, token, &f.PageID); err != nil {
		return httpx.NotFound("File not found.")
	}
	if err := s.sign(ctx, f, key); err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.OK(w, f)
	return nil
}

// publicRows: GET /docs/public/{token}/databases/{id}/rows?view=: rows of a
// database the link opens (rows restricted from it are left out).
func (s *Service) publicRows(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	token := chi.URLParam(r, "token")
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	dbp, _, err := s.publicTarget(ctx, token, &id)
	if err != nil {
		return err
	}
	if dbp.Kind != "database" {
		return errNotDatabase
	}
	views, err := loadViews(ctx, s.d.Pool, id)
	if err != nil {
		return err
	}
	rq, err := rowsRequest(r, dbp, views)
	if err != nil {
		return err
	}
	rq.public = true
	out, err := s.queryRows(ctx, rq)
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.OK(w, out)
	return nil
}
