package docs

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
)

// PageLink is what a viewer may see of a page without opening it: enough for
// a link card (channels' unfurlers) or an assistant's context line.
type PageLink struct {
	ID         uuid.UUID    `json:"id"`
	OrgID      uuid.UUID    `json:"org_id"`
	Kind       string       `json:"kind"`   // page | database
	IsRow      bool         `json:"is_row"` // a database row (a page too)
	Title      string       `json:"title"`  // "Untitled" when empty
	Icon       string       `json:"icon"`   // emoji; "" when none
	CustomIcon *CustomIcon  `json:"custom_icon,omitempty"`
	Context    string       `json:"context"` // workspace · parent page (the parent only when the viewer can open it)
	Snippet    string       `json:"snippet"` // the first line of text, ≤ 140 characters; "" for an empty page
	Path       string       `json:"path"`    // app path: /docs/<id>
	Level      access.Level `json:"level"`   // the viewer's level, agent-capped
	UpdatedAt  time.Time    `json:"updated_at"`
}

// ErrPageNotFound is PageRef's answer when the page doesn't exist, is in the
// trash, or the viewer can't open it; the three look the same.
var ErrPageNotFound = errPageNotFound

// PageRef reads a page as viewer with the same check as GET /docs/pages/{id}:
// access_level on docs.page (sharing, inheritance, workspace membership), capped
// by the viewer's AI agent scope when ctx is an agent call (access.ViaAgent),
// and refusing trashed pages. Nothing is recorded (no visit, no view). The
// parent page is named only when the viewer can open it too.
func PageRef(ctx context.Context, q db.Querier, viewer *auth.Identity, pageID uuid.UUID) (*PageLink, error) {
	if viewer == nil || viewer.UserID == uuid.Nil || pageID == uuid.Nil {
		return nil, ErrPageNotFound
	}
	lvl, err := access.LevelOf(ctx, q, viewer.UserID, Kind, pageID)
	if err != nil {
		return nil, err
	}
	if lvl < access.View {
		return nil, ErrPageNotFound
	}
	p, err := loadPage(ctx, q, pageID, false)
	if errors.Is(err, errPageNotFound) {
		return nil, ErrPageNotFound
	}
	if err != nil {
		return nil, err
	}
	if p.DeletedAt != nil {
		return nil, ErrPageNotFound
	}
	out := &PageLink{
		ID: p.ID, OrgID: p.OrgID, Kind: p.Kind, IsRow: p.IsRow, Title: displayTitle(p.Title), CustomIcon: p.CustomIcon,
		Path: "/docs/" + p.ID.String(), Level: lvl, UpdatedAt: p.UpdatedAt,
	}
	if p.Icon != nil {
		out.Icon = *p.Icon
	}
	var orgName, parent string
	if err := q.QueryRow(ctx, `SELECT name FROM orgs WHERE id = $1`, p.OrgID).Scan(&orgName); err != nil && !db.IsNoRows(err) {
		return nil, err
	}
	if p.ParentID != nil {
		var t string
		err := q.QueryRow(ctx, `SELECT title FROM docs_pages WHERE id = $1 AND deleted_at IS NULL
			AND access_level($2, 'docs.page', id, $3) >= $4`, *p.ParentID, viewer.UserID, access.ViaAgent(ctx), int(access.View)).Scan(&t)
		if err == nil {
			parent = displayTitle(t)
		} else if !db.IsNoRows(err) {
			return nil, err
		}
	}
	out.Context = app.SearchContext(orgName, parent)
	var plain string
	err = q.QueryRow(ctx, `SELECT plain FROM docs_blocks WHERE page_id = $1 AND btrim(plain) <> ''
		ORDER BY parent_id IS NOT NULL, position LIMIT 1`, p.ID).Scan(&plain)
	if err != nil && !db.IsNoRows(err) {
		return nil, err
	}
	out.Snippet = snippetAround(plain, "")
	return out, nil
}

// PageIDFromPath reads the page id from an app path a page link opens
// ("/docs/<id>", with or without a trailing slash). Connected pages
// (/docs/c/…), forms and other docs screens answer false.
func PageIDFromPath(path string) (uuid.UUID, bool) {
	rest, ok := strings.CutPrefix(path, "/docs/")
	if !ok {
		return uuid.Nil, false
	}
	rest = strings.TrimSuffix(rest, "/")
	id, err := uuid.Parse(rest)
	if err != nil || strings.Contains(rest, "/") {
		return uuid.Nil, false
	}
	return id, true
}
