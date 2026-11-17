package docs

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
)

// PublishSettings are a published page's options; they apply to its public
// link and the subpages it opens. The link itself (expiry, stop) is access's.
type PublishSettings struct {
	SearchIndexing bool       `json:"search_indexing"` // let search engines list it
	AllowDuplicate bool       `json:"allow_duplicate"` // signed-in visitors may copy it into their workspace
	Theme          string     `json:"theme"`           // system | light | dark
	ShowOutline    bool       `json:"show_outline"`    // the headings outline beside the page
	UpdatedAt      *time.Time `json:"updated_at,omitempty"`
}

var publishThemes = map[string]bool{"system": true, "light": true, "dark": true}

func defaultPublishSettings() PublishSettings {
	return PublishSettings{Theme: "system", ShowOutline: true}
}

func (s *Service) publishSettings(ctx context.Context, q db.Querier, pageID uuid.UUID) (PublishSettings, error) {
	out := defaultPublishSettings()
	var at time.Time
	err := q.QueryRow(ctx, `SELECT search_indexing, allow_duplicate, theme, show_outline, updated_at FROM docs_publish_settings WHERE page_id = $1`, pageID).
		Scan(&out.SearchIndexing, &out.AllowDuplicate, &out.Theme, &out.ShowOutline, &at)
	if db.IsNoRows(err) {
		return out, nil
	}
	if err == nil {
		out.UpdatedAt = &at
	}
	return out, err
}

// getPublishSettings: GET /docs/pages/{id}/publish (view).
func (s *Service) getPublishSettings(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	if _, err := access.Require(ctx, s.d.Pool, me, Kind, id, access.View); err != nil {
		return err
	}
	out, err := s.publishSettings(ctx, s.d.Pool, id)
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

type publishSettingsReq struct {
	SearchIndexing *bool   `json:"search_indexing"`
	AllowDuplicate *bool   `json:"allow_duplicate"`
	Theme          *string `json:"theme"`
	ShowOutline    *bool   `json:"show_outline"`
}

// putPublishSettings: PATCH /docs/pages/{id}/publish (manage, like
// publishing itself) {search_indexing?, allow_duplicate?, theme?,
// show_outline?}.
func (s *Service) putPublishSettings(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req publishSettingsReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if _, err := requirePage(ctx, s.d.Pool, me, id, access.Manage); err != nil {
		return err
	}
	var out PublishSettings
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		cur, err := s.publishSettings(ctx, tx, id)
		if err != nil {
			return err
		}
		if req.SearchIndexing != nil {
			cur.SearchIndexing = *req.SearchIndexing
		}
		if req.AllowDuplicate != nil {
			cur.AllowDuplicate = *req.AllowDuplicate
		}
		if req.Theme != nil {
			if !publishThemes[*req.Theme] {
				return httpx.BadRequest("theme must be system, light or dark.")
			}
			cur.Theme = *req.Theme
		}
		if req.ShowOutline != nil {
			cur.ShowOutline = *req.ShowOutline
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO docs_publish_settings (page_id, search_indexing, allow_duplicate, theme, show_outline, updated_by)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (page_id) DO UPDATE SET search_indexing = EXCLUDED.search_indexing, allow_duplicate = EXCLUDED.allow_duplicate,
			  theme = EXCLUDED.theme, show_outline = EXCLUDED.show_outline, updated_by = EXCLUDED.updated_by, updated_at = now()`,
			id, cur.SearchIndexing, cur.AllowDuplicate, cur.Theme, cur.ShowOutline, me); err != nil {
			return err
		}
		out, err = s.publishSettings(ctx, tx, id)
		return err
	})
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

// PublicMeta is what the web app's server needs to render a published page's
// <head>: its title and whether search engines may index it.
type PublicMeta struct {
	Title    string `json:"title"`
	Indexing bool   `json:"indexing"`
	Theme    string `json:"theme"`
}

// publicMeta: GET /docs/public/{token}/meta[?page=] (public).
func (s *Service) publicMeta(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	token := chi.URLParam(r, "token")
	var id *uuid.UUID
	if raw := r.URL.Query().Get("page"); raw != "" {
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
	set, err := s.publishSettings(ctx, s.d.Pool, l.Ref.ID)
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.OK(w, PublicMeta{Title: displayTitle(p.Title), Indexing: set.SearchIndexing, Theme: set.Theme})
	return nil
}

type publicDuplicateReq struct {
	OrgID  uuid.UUID  `json:"org_id"`
	PageID *uuid.UUID `json:"page_id"` // a subpage the link opens; default the published page
}

// duplicatePublic: POST /docs/public/{token}/duplicate (signed in; 20/h)
// {org_id, page_id?}: when the publisher allows it, copy the published page
// (and the subpages the same link opens) into a workspace the caller belongs
// to, as their private page. Agent credentials never reach it (user routes).
func (s *Service) duplicatePublic(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	token := chi.URLParam(r, "token")
	var req publicDuplicateReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if role, err := orgs.RoleOf(ctx, s.d.Pool, req.OrgID, me); err != nil {
		return err
	} else if role == "" {
		return httpx.NotFound("Choose one of your workspaces.")
	}
	p, l, err := s.publicTarget(ctx, token, req.PageID)
	if err != nil {
		return err
	}
	set, err := s.publishSettings(ctx, s.d.Pool, l.Ref.ID)
	if err != nil {
		return err
	}
	if !set.AllowDuplicate {
		return httpx.Forbidden("The publisher doesn't allow copying this page.")
	}
	if p.IsRow {
		return httpx.BadRequest("Copy the database instead of one row.")
	}
	var newID uuid.UUID
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		pos, err := siblingPosition(ctx, tx, req.OrgID, nil, nil)
		if err != nil {
			return err
		}
		budget := &dupBudget{pages: maxDuplicatePages, blocks: maxDuplicateBlocks, org: req.OrgID, token: token}
		newID, err = s.copyTree(ctx, tx, me, p.ID, nil, pos, budget)
		if err != nil {
			return err
		}
		return autoFollow(ctx, tx, me, newID)
	})
	if err != nil {
		return err
	}
	out, err := loadPageWithLevel(ctx, s, me, newID)
	if err != nil {
		return err
	}
	s.treeChanged(ctx, req.OrgID)
	httpx.JSON(w, http.StatusCreated, out)
	return nil
}
