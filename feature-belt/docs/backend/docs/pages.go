package docs

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
)

// Cover is a page's banner: an uploaded image or a colour from the palette,
// with the image's vertical focus (0 top … 100 bottom).
type Cover struct {
	FileID   *uuid.UUID `json:"file_id,omitempty"`
	Color    string     `json:"color,omitempty"`
	Gallery  string     `json:"gallery,omitempty"` // a built-in cover (gradients drawn from the palette)
	Position int        `json:"position"`
}

// coverColors are the palette roles a colour cover (or a lucide icon) may use.
var coverColors = map[string]bool{"neutral": true, "primary": true, "accent": true, "success": true, "warning": true, "danger": true, "info": true}

// coverGallery names the built-in covers; clients draw each from two palette
// roles (shared/pages.ts DOCS_COVER_GALLERY), so they follow the theme.
var coverGallery = map[string]bool{
	"dawn": true, "lagoon": true, "dusk": true, "meadow": true, "ember": true, "glacier": true,
	"orchid": true, "slate": true, "citrus": true, "tide": true, "aurora": true, "sand": true,
}

// CustomIcon is a page icon other than an emoji: an icon from the app's set
// (lucide names) tinted with a palette role, or an image uploaded to the page.
type CustomIcon struct {
	Type   string     `json:"type"` // lucide | image
	Name   string     `json:"name,omitempty"`
	Color  string     `json:"color,omitempty"`
	FileID *uuid.UUID `json:"file_id,omitempty"`
}

var iconNameRE = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// pageFonts are the page typefaces: the app's, a serif and a monospace.
var pageFonts = map[string]bool{"default": true, "serif": true, "mono": true}

// Page is a page, database or database row as the API returns it.
type Page struct {
	ID        uuid.UUID      `json:"id"`
	OrgID     uuid.UUID      `json:"org_id"`
	ParentID  *uuid.UUID     `json:"parent_id"`
	Kind      string         `json:"kind"`
	IsRow     bool           `json:"is_row"`
	Inline    bool           `json:"inline"`
	Title     string         `json:"title"`
	Icon      *string        `json:"icon"`
	Cover     *Cover         `json:"cover"`
	Position  string         `json:"position"`
	FullWidth bool           `json:"full_width"`
	Locked    bool           `json:"locked"`
	Props     map[string]any `json:"props"`
	Schema    []Property     `json:"schema"`
	Version   int64          `json:"version"`
	CreatedBy *uuid.UUID     `json:"created_by"`
	UpdatedBy *uuid.UUID     `json:"updated_by"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt *time.Time     `json:"deleted_at"`
	MyLevel   access.Level   `json:"my_level"`
	// Page chrome and status (docs 1.2.0).
	CustomIcon    *CustomIcon `json:"custom_icon"`
	Font          string      `json:"font"`
	SmallText     bool        `json:"small_text"`
	IsTemplate    bool        `json:"is_template"`
	Verified      bool        `json:"verified"` // verified and not expired
	VerifiedBy    *uuid.UUID  `json:"verified_by"`
	VerifiedAt    *time.Time  `json:"verified_at"`
	VerifiedUntil *time.Time  `json:"verified_until"`
}

// PageSummary is a page in a list (sidebar, search, trash, backlinks).
type PageSummary struct {
	ID          uuid.UUID    `json:"id"`
	ParentID    *uuid.UUID   `json:"parent_id"`
	Kind        string       `json:"kind"`
	IsRow       bool         `json:"is_row"`
	Title       string       `json:"title"`
	Icon        *string      `json:"icon"`
	Position    string       `json:"position"`
	HasChildren bool         `json:"has_children"`
	MyLevel     access.Level `json:"my_level"`
	UpdatedAt   time.Time    `json:"updated_at"`
	DeletedAt   *time.Time   `json:"deleted_at,omitempty"`
	Snippet     string       `json:"snippet,omitempty"`
	// CustomIcon is filled by the lists that select it (sidebar, children,
	// recent, quick find); search hits carry the emoji only.
	CustomIcon *CustomIcon `json:"custom_icon,omitempty"`
	// Trash lists only: who trashed it and the page it was in (when the
	// caller can open that page).
	DeletedBy   *RefUser   `json:"deleted_by,omitempty"`
	ParentTitle *string    `json:"parent_title,omitempty"`
	trash       *trashInfo `json:"-"`
}

const pageCols = `p.id, p.org_id, p.parent_id, p.kind, p.is_row, p.inline, p.title, p.icon, p.cover, p.position, p.full_width,
	p.locked, p.props, p.schema, p.version, p.created_by, p.updated_by, p.created_at, p.updated_at, p.deleted_at,
	p.custom_icon, p.font, p.small_text, p.is_template, p.verified_by, p.verified_at, p.verified_until`

func scanPage(row pgx.Row) (*Page, error) {
	var p Page
	var cover, props, schema, customIcon []byte
	err := row.Scan(&p.ID, &p.OrgID, &p.ParentID, &p.Kind, &p.IsRow, &p.Inline, &p.Title, &p.Icon, &cover, &p.Position, &p.FullWidth,
		&p.Locked, &props, &schema, &p.Version, &p.CreatedBy, &p.UpdatedBy, &p.CreatedAt, &p.UpdatedAt, &p.DeletedAt,
		&customIcon, &p.Font, &p.SmallText, &p.IsTemplate, &p.VerifiedBy, &p.VerifiedAt, &p.VerifiedUntil)
	if err != nil {
		return nil, err
	}
	if len(cover) > 0 {
		_ = json.Unmarshal(cover, &p.Cover)
	}
	if len(customIcon) > 0 {
		_ = json.Unmarshal(customIcon, &p.CustomIcon)
	}
	p.Verified = p.VerifiedAt != nil && (p.VerifiedUntil == nil || p.VerifiedUntil.After(time.Now()))
	p.Props = map[string]any{}
	_ = json.Unmarshal(props, &p.Props)
	if len(schema) > 0 {
		_ = json.Unmarshal(schema, &p.Schema)
	}
	return &p, nil
}

func loadPage(ctx context.Context, q db.Querier, id uuid.UUID, lock bool) (*Page, error) {
	sql := `SELECT ` + pageCols + ` FROM docs_pages p WHERE p.id = $1`
	if lock {
		sql += ` FOR UPDATE`
	}
	p, err := scanPage(q.QueryRow(ctx, sql, id))
	if db.IsNoRows(err) {
		return nil, errPageNotFound
	}
	return p, err
}

func pageTitle(ctx context.Context, q db.Querier, id uuid.UUID) (string, error) {
	var t string
	err := q.QueryRow(ctx, `SELECT title FROM docs_pages WHERE id = $1`, id).Scan(&t)
	if db.IsNoRows(err) {
		return "", errPageNotFound
	}
	return displayTitle(t), err
}

func displayTitle(t string) string {
	if strings.TrimSpace(t) == "" {
		return "Untitled"
	}
	return t
}

// requirePage checks the caller's level (agent-capped through ctx) and loads
// the page. 404 without view, 403 below min.
func requirePage(ctx context.Context, q db.Querier, me, id uuid.UUID, min access.Level) (*Page, error) {
	lvl, err := access.Require(ctx, q, me, Kind, id, min)
	if err != nil {
		return nil, err
	}
	p, err := loadPage(ctx, q, id, false)
	if err != nil {
		return nil, err
	}
	p.MyLevel = lvl
	return p, nil
}

// editable refuses changes to trashed pages (and, for content, locked ones).
func editable(p *Page, content bool) error {
	if p.DeletedAt != nil {
		return errInTrash
	}
	if content && p.Locked {
		return errLocked
	}
	return nil
}

// hasChildrenSQL is true when the page p has a child page the caller ($1,
// agent flag $3) can open. Rows are listed by their database, not the tree.
const hasChildrenSQL = `EXISTS (SELECT 1 FROM docs_pages c WHERE c.parent_id = p.id AND NOT c.is_row AND c.deleted_at IS NULL
	AND access_level($1, 'docs.page', c.id, $3) >= 1)`

const summaryCols = `p.id, p.parent_id, p.kind, p.is_row, p.title, p.icon, p.position, p.updated_at, p.deleted_at`

func scanSummaries(rows pgx.Rows, extra func(*PageSummary) []any) ([]PageSummary, error) {
	defer rows.Close()
	out := []PageSummary{}
	for rows.Next() {
		var ps PageSummary
		var lvl int
		dest := []any{&ps.ID, &ps.ParentID, &ps.Kind, &ps.IsRow, &ps.Title, &ps.Icon, &ps.Position, &ps.UpdatedAt, &ps.DeletedAt, &lvl}
		if extra != nil {
			dest = append(dest, extra(&ps)...)
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		ps.MyLevel = access.Level(lvl)
		out = append(out, ps)
	}
	return out, rows.Err()
}

// ---- sidebar, recent, trash ------------------------------------------------------

// Sidebar is the workspace tree's top level for the caller.
type Sidebar struct {
	Favorites        []PageSummary    `json:"favorites"`
	Teamspaces       []Teamspace      `json:"teamspaces"`
	HiddenTeamspaces []access.TeamRef `json:"hidden_teamspaces"`
	Shared           []PageSummary    `json:"shared"`
	Private          []PageSummary    `json:"private"`
	CanCreateShared  bool             `json:"can_create_shared"`
}

// Teamspace is a team the caller is on with the top-level pages shared with
// it (the pages a team works in). Pages shared with several of the caller's
// teams show under the first by name.
type Teamspace struct {
	Team  access.TeamRef `json:"team"`
	Pages []PageSummary  `json:"pages"`
}

func decodeCustomIcon(raw []byte) *CustomIcon {
	if len(raw) == 0 {
		return nil
	}
	var c CustomIcon
	if json.Unmarshal(raw, &c) != nil || c.Type == "" {
		return nil
	}
	return &c
}

// summaries scans rows of summaryCols, level, has_children, custom_icon
// (and extra columns after them).
func summaries(rows pgx.Rows, extra func(*PageSummary) []any) ([]PageSummary, error) {
	var icons [][]byte
	list, err := scanSummaries(rows, func(ps *PageSummary) []any {
		icons = append(icons, nil)
		dest := []any{&ps.HasChildren, &icons[len(icons)-1]}
		if extra != nil {
			dest = append(dest, extra(ps)...)
		}
		return dest
	})
	for i := range list {
		list[i].CustomIcon = decodeCustomIcon(icons[i])
	}
	return list, err
}

// sidebar: GET /docs/orgs/{orgID}/sidebar. Roots are the pages the caller
// can open whose parent they can't (top-level pages, and pages shared with
// them deep inside someone else's tree). Private = top-level pages they own
// that have no grants at all; a root shared with one of the caller's teams
// goes under that teamspace (unless they hid it); everything else is
// Shared. Workspace templates stay out of the tree.
func (s *Service) sidebar(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	m, _ := orgs.FromContext(ctx)
	agent := access.ViaAgent(ctx)
	teams, err := access.TeamsOf(ctx, s.d.Pool, m.OrgID, me)
	if err != nil {
		return err
	}
	hidden := map[uuid.UUID]bool{}
	if len(teams) > 0 {
		rows, err := s.d.Pool.Query(ctx, `SELECT team_id FROM docs_teamspace_prefs WHERE user_id = $1 AND hidden`, me)
		if err != nil {
			return err
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		if err != nil {
			return err
		}
		for _, id := range ids {
			hidden[id] = true
		}
	}
	teamIDs := make([]uuid.UUID, 0, len(teams))
	for _, t := range teams {
		teamIDs = append(teamIDs, t.ID)
	}
	rows, err := s.d.Pool.Query(ctx, `
		WITH v AS MATERIALIZED (SELECT id, level FROM access_visible($1, 'docs.page', $3))
		SELECT `+summaryCols+`, v.level, `+hasChildrenSQL+`, p.custom_icon,
		       COALESCE(p.parent_id IS NULL AND ar.owner_id = $1
		        AND NOT EXISTS (SELECT 1 FROM access_grants g WHERE g.resource_kind = 'docs.page' AND g.resource_id = p.id), false),
		       COALESCE((SELECT array_agg(g.subject_id) FROM access_grants g WHERE g.resource_kind = 'docs.page' AND g.resource_id = p.id
		        AND g.subject_type = 'team' AND g.subject_id = ANY ($4)), '{}')
		FROM docs_pages p
		JOIN v ON v.id = p.id
		JOIN access_resources ar ON ar.kind = 'docs.page' AND ar.id = p.id
		WHERE p.org_id = $2 AND p.deleted_at IS NULL AND NOT p.is_row AND NOT p.is_template
		  AND (p.parent_id IS NULL OR NOT EXISTS (SELECT 1 FROM v vp WHERE vp.id = p.parent_id))
		ORDER BY p.position, p.id LIMIT 500`, me, m.OrgID, agent, teamIDs)
	if err != nil {
		return err
	}
	var private []bool
	var granted [][]uuid.UUID
	all, err := summaries(rows, func(ps *PageSummary) []any {
		private = append(private, false)
		granted = append(granted, nil)
		return []any{&private[len(private)-1], &granted[len(granted)-1]}
	})
	if err != nil {
		return err
	}
	out := Sidebar{Favorites: []PageSummary{}, Teamspaces: []Teamspace{}, HiddenTeamspaces: []access.TeamRef{}, Shared: []PageSummary{}, Private: []PageSummary{}}
	byTeam := map[uuid.UUID][]PageSummary{}
	for i, ps := range all {
		switch {
		case private[i]:
			out.Private = append(out.Private, ps)
		case len(granted[i]) > 0:
			// The first of the caller's teams (by name) it is shared with.
			placed := false
			for _, t := range teams {
				if slices.Contains(granted[i], t.ID) {
					if !hidden[t.ID] {
						byTeam[t.ID] = append(byTeam[t.ID], ps)
					}
					placed = true
					break
				}
			}
			if !placed {
				out.Shared = append(out.Shared, ps)
			}
		default:
			out.Shared = append(out.Shared, ps)
		}
	}
	for _, t := range teams {
		if hidden[t.ID] {
			out.HiddenTeamspaces = append(out.HiddenTeamspaces, t)
			continue
		}
		pages := byTeam[t.ID]
		if pages == nil {
			pages = []PageSummary{}
		}
		out.Teamspaces = append(out.Teamspaces, Teamspace{Team: t, Pages: pages})
	}
	rows, err = s.d.Pool.Query(ctx, `
		SELECT `+summaryCols+`, access_level($1, 'docs.page', p.id, $3), `+hasChildrenSQL+`, p.custom_icon
		FROM docs_favorites f JOIN docs_pages p ON p.id = f.page_id
		WHERE f.user_id = $1 AND p.org_id = $2 AND p.deleted_at IS NULL AND access_level($1, 'docs.page', p.id, $3) >= 1
		ORDER BY f.created_at, p.id LIMIT 100`, me, m.OrgID, agent)
	if err != nil {
		return err
	}
	if out.Favorites, err = summaries(rows, nil); err != nil {
		return err
	}
	if out.CanCreateShared, err = access.Allowed(ctx, s.d.Pool, m.OrgID, me, PermCreateShared); err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

// children: GET /docs/pages/{id}/children: the child pages the caller can open.
func (s *Service) children(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	if _, err := access.Require(ctx, s.d.Pool, me, Kind, id, access.View); err != nil {
		return err
	}
	list, err := s.childPages(ctx, s.d.Pool, me, id)
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": list})
	return nil
}

func (s *Service) childPages(ctx context.Context, q db.Querier, me, id uuid.UUID) ([]PageSummary, error) {
	rows, err := q.Query(ctx, `
		SELECT `+summaryCols+`, access_level($1, 'docs.page', p.id, $3), `+hasChildrenSQL+`, p.custom_icon
		FROM docs_pages p
		WHERE p.parent_id = $2 AND NOT p.is_row AND p.deleted_at IS NULL AND access_level($1, 'docs.page', p.id, $3) >= 1
		ORDER BY p.position, p.id LIMIT 500`, me, id, access.ViaAgent(ctx))
	if err != nil {
		return nil, err
	}
	return summaries(rows, nil)
}

// recent: GET /docs/orgs/{orgID}/recent: the last 30 pages the caller opened.
func (s *Service) recent(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	m, _ := orgs.FromContext(ctx)
	rows, err := s.d.Pool.Query(ctx, `
		SELECT `+summaryCols+`, access_level($1, 'docs.page', p.id, $3), false, p.custom_icon
		FROM docs_visits v JOIN docs_pages p ON p.id = v.page_id
		WHERE v.user_id = $1 AND p.org_id = $2 AND p.deleted_at IS NULL AND NOT p.is_template AND access_level($1, 'docs.page', p.id, $3) >= 1
		ORDER BY v.visited_at DESC, p.id LIMIT 30`, me, m.OrgID, access.ViaAgent(ctx))
	if err != nil {
		return err
	}
	list, err := summaries(rows, nil)
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": list})
	return nil
}

type trashCursor struct {
	T  time.Time `json:"t"`
	ID uuid.UUID `json:"id"`
}

// trash: GET /docs/orgs/{orgID}/trash?q=&by=me&kind=&in=&cursor=: pages
// the caller can edit that were trashed (the page each trashing started
// from), newest first, with who trashed them and where they were.
func (s *Service) trash(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	m, _ := orgs.FromContext(ctx)
	pp := httpx.ParsePage(r)
	var c trashCursor
	if err := httpx.DecodeCursor(pp.Cursor, &c); err != nil {
		return err
	}
	qp := r.URL.Query()
	q := strings.TrimSpace(qp.Get("q"))
	kind := qp.Get("kind")
	if kind != "" && kind != "page" && kind != "database" {
		return httpx.BadRequest("kind must be page or database.")
	}
	var by *uuid.UUID
	if qp.Get("by") == "me" {
		by = &me
	}
	var in *uuid.UUID
	if raw := qp.Get("in"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return httpx.BadRequest("in must be a page id.")
		}
		in = &id
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT `+summaryCols+`, access_level($1, 'docs.page', p.id, $3), false, p.custom_icon,
		       p.deleted_by, du.display_name,
		       CASE WHEN pp.id IS NOT NULL AND access_level($1, 'docs.page', pp.id, $3) >= 1 THEN pp.title END
		FROM docs_pages p
		LEFT JOIN users du ON du.id = p.deleted_by AND du.status <> 'deleted'
		LEFT JOIN docs_pages pp ON pp.id = p.parent_id
		WHERE p.org_id = $2 AND p.deleted_at IS NOT NULL AND p.trash_root_id = p.id
		  AND ($4 = '' OR p.title ILIKE '%' || $4 || '%')
		  AND ($5::timestamptz IS NULL OR (p.deleted_at, p.id) < ($5, $6))
		  AND ($8::uuid IS NULL OR p.deleted_by = $8)
		  AND ($9 = '' OR p.kind = $9)
		  AND ($10::uuid IS NULL OR p.parent_id = $10)
		  AND access_level($1, 'docs.page', p.id, $3) >= 3
		ORDER BY p.deleted_at DESC, p.id DESC LIMIT $7`,
		me, m.OrgID, access.ViaAgent(ctx), likeEscape(q), nullTime(c.T), c.ID, pp.Limit+1, by, kind, in)
	if err != nil {
		return err
	}
	list, err := summaries(rows, func(ps *PageSummary) []any {
		ps.trash = &trashInfo{}
		return []any{&ps.trash.byID, &ps.trash.byName, &ps.ParentTitle}
	})
	if err != nil {
		return err
	}
	for i := range list {
		if t := list[i].trash; t != nil && t.byID != nil && t.byName != nil {
			list[i].DeletedBy = &RefUser{ID: *t.byID, Name: *t.byName}
		}
		if list[i].ParentTitle != nil {
			list[i].ParentTitle = ptr(displayTitle(*list[i].ParentTitle))
		}
	}
	httpx.OK(w, httpx.NewPage(list, pp.Limit, func(ps PageSummary) any { return trashCursor{T: *ps.DeletedAt, ID: ps.ID} }))
	return nil
}

type trashInfo struct {
	byID   *uuid.UUID
	byName *string
}

// ---- one page --------------------------------------------------------------------

// Crumb is an ancestor in a page's path.
type Crumb struct {
	ID    uuid.UUID `json:"id"`
	Title string    `json:"title"`
	Icon  *string   `json:"icon"`
	Kind  string    `json:"kind"`
}

// RefUser and RefPage resolve mentions and links for display. A page the
// caller can't open has Title nil ("No access").
type RefUser struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	AvatarURL *string   `json:"avatar_url"`
}

type RefPage struct {
	ID      uuid.UUID `json:"id"`
	Title   *string   `json:"title"`
	Icon    *string   `json:"icon"`
	Kind    string    `json:"kind"`
	Deleted bool      `json:"deleted"`
}

type Refs struct {
	Users map[string]RefUser `json:"users"`
	Pages map[string]RefPage `json:"pages"`
}

// DatabaseInfo is a row's database (schema to render the row's properties).
type DatabaseInfo struct {
	ID     uuid.UUID  `json:"id"`
	Title  *string    `json:"title"`
	Icon   *string    `json:"icon"`
	Schema []Property `json:"schema"`
}

// PageDetail is GET /docs/pages/{id}.
type PageDetail struct {
	Page          *Page          `json:"page"`
	Path          []Crumb        `json:"path"`
	Blocks        []Block        `json:"blocks"`
	Refs          Refs           `json:"refs"`
	Views         []View         `json:"views"`
	Database      *DatabaseInfo  `json:"database"`
	Favorite      bool           `json:"favorite"`
	CommentCounts map[string]int `json:"comment_counts"` // open threads per block id; "" = the page
	Published     bool           `json:"published"`
	Following     bool           `json:"following"` // the caller gets edits and comments (follow.go)
}

// getPage: GET /docs/pages/{id}: everything needed to render and edit it.
func (s *Service) getPage(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	p, err := requirePage(ctx, s.d.Pool, me, id, access.View)
	if err != nil {
		return err
	}
	out, err := s.pageDetail(ctx, me, p, true)
	if err != nil {
		return err
	}
	// ?preview=1 (hover cards) reads without counting a visit or a view.
	if p.DeletedAt == nil && r.URL.Query().Get("preview") != "1" {
		_, _ = s.d.Pool.Exec(ctx, `
			INSERT INTO docs_visits (user_id, page_id) VALUES ($1, $2)
			ON CONFLICT (user_id, page_id) DO UPDATE SET visited_at = now()
			WHERE docs_visits.visited_at < now() - interval '1 minute'`, me, id)
		s.recordView(ctx, me, id)
	}
	httpx.OK(w, out)
	return nil
}

// pageDetail assembles a page's detail for a signed-in caller (me) or a
// public link viewer (me = uuid.Nil: no path beyond the link, no favorites).
func (s *Service) pageDetail(ctx context.Context, me uuid.UUID, p *Page, member bool) (*PageDetail, error) {
	q := s.d.Pool
	blocks, err := loadBlocks(ctx, q, p.ID)
	if err != nil {
		return nil, err
	}
	out := &PageDetail{Page: p, Blocks: blocks, Path: []Crumb{}, Views: []View{}, CommentCounts: map[string]int{}}
	if p.Kind == "database" {
		if out.Views, err = loadViews(ctx, q, p.ID); err != nil {
			return nil, err
		}
	}
	if p.IsRow && p.ParentID != nil {
		dbp, err := loadPage(ctx, q, *p.ParentID, false)
		if err != nil {
			return nil, err
		}
		info := &DatabaseInfo{ID: dbp.ID, Schema: dbp.Schema, Icon: dbp.Icon}
		if member {
			if lvl, err := access.LevelOf(ctx, q, me, Kind, dbp.ID); err == nil && lvl >= access.View {
				t := displayTitle(dbp.Title)
				info.Title = &t
			}
		}
		out.Database = info
	}
	var userIDs, pageIDs []uuid.UUID
	for _, b := range blocks {
		u, pg := blockRefs(b)
		userIDs, pageIDs = append(userIDs, u...), append(pageIDs, pg...)
	}
	if p.IsRow && out.Database != nil {
		userIDs = append(userIDs, personValues(out.Database.Schema, p.Props)...)
	}
	if member {
		// Page info: who created, last edited and verified it.
		for _, u := range []*uuid.UUID{p.CreatedBy, p.UpdatedBy, p.VerifiedBy} {
			if u != nil {
				userIDs = append(userIDs, *u)
			}
		}
	}
	if out.Refs, err = s.resolveRefs(ctx, me, member, userIDs, pageIDs); err != nil {
		return nil, err
	}
	// A row's formulas, rollups and related rows, and computed properties' settings (databases.go).
	if err := s.databaseDetail(ctx, me, member, out); err != nil {
		return nil, err
	}
	if !member {
		return out, nil
	}
	if out.Path, err = s.path(ctx, me, p); err != nil {
		return nil, err
	}
	if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM docs_favorites WHERE user_id = $1 AND page_id = $2),
		COALESCE((SELECT following FROM docs_follows WHERE user_id = $1 AND page_id = $2), false)`, me, p.ID).Scan(&out.Favorite, &out.Following); err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `
		SELECT COALESCE(block_id::text, ''), count(*) FROM docs_comments
		WHERE page_id = $1 AND thread_id IS NULL AND resolved_at IS NULL AND deleted_at IS NULL AND hidden_at IS NULL
		GROUP BY 1`, p.ID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			rows.Close()
			return nil, err
		}
		out.CommentCounts[k] = n
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	err = q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM access_links WHERE resource_kind = 'docs.page' AND resource_id = $1
		AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > now()))`, p.ID).Scan(&out.Published)
	return out, err
}

// blockRefs returns the people and pages a block mentions or links to.
func blockRefs(b Block) (users, pages []uuid.UUID) {
	for _, key := range []string{"text", "caption"} {
		if _, ok := b.Props[key]; ok {
			u, p := mentionIDs(spansOf(b.Props, key))
			users, pages = append(users, u...), append(pages, p...)
		}
	}
	if b.Type == "table" {
		raw, _ := json.Marshal(b.Props["rows"])
		var rows [][][]Span
		_ = json.Unmarshal(raw, &rows)
		for _, row := range rows {
			for _, cell := range row {
				u, p := mentionIDs(cell)
				users, pages = append(users, u...), append(pages, p...)
			}
		}
	}
	for _, key := range []string{"page_id", "database_id"} {
		if id, err := uuid.Parse(propString(b.Props, key)); err == nil {
			pages = append(pages, id)
		}
	}
	return users, pages
}

// resolveRefs looks up display names for mentions. Page titles only for
// pages the caller can open (public viewers: pages the same link opens are
// resolved by the public handler; others stay untitled).
func (s *Service) resolveRefs(ctx context.Context, me uuid.UUID, member bool, users, pages []uuid.UUID) (Refs, error) {
	out := Refs{Users: map[string]RefUser{}, Pages: map[string]RefPage{}}
	if len(users) > 0 {
		rows, err := s.d.Pool.Query(ctx, `SELECT id, CASE WHEN status = 'deleted' THEN 'Deleted user' ELSE display_name END, avatar_url
			FROM users WHERE id = ANY ($1)`, dedupeIDs(users))
		if err != nil {
			return out, err
		}
		for rows.Next() {
			var u RefUser
			if err := rows.Scan(&u.ID, &u.Name, &u.AvatarURL); err != nil {
				rows.Close()
				return out, err
			}
			if !member {
				u.AvatarURL = nil
			}
			out.Users[u.ID.String()] = u
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return out, err
		}
	}
	if len(pages) > 0 {
		rows, err := s.d.Pool.Query(ctx, `
			SELECT p.id, p.title, p.icon, p.kind, p.deleted_at IS NOT NULL,
			       CASE WHEN $2 THEN access_level($3, 'docs.page', p.id, $4) ELSE 0 END
			FROM docs_pages p WHERE p.id = ANY ($1)`, dedupeIDs(pages), member, me, access.ViaAgent(ctx))
		if err != nil {
			return out, err
		}
		for rows.Next() {
			var rp RefPage
			var title string
			var lvl int
			if err := rows.Scan(&rp.ID, &title, &rp.Icon, &rp.Kind, &rp.Deleted, &lvl); err != nil {
				rows.Close()
				return out, err
			}
			if lvl >= 1 {
				t := displayTitle(title)
				rp.Title = &t
			} else {
				rp.Icon, rp.Deleted = nil, false
			}
			out.Pages[rp.ID.String()] = rp
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return out, err
		}
	}
	return out, nil
}

// path lists the page's ancestors the caller can open, top first.
func (s *Service) path(ctx context.Context, me uuid.UUID, p *Page) ([]Crumb, error) {
	rows, err := s.d.Pool.Query(ctx, `
		WITH RECURSIVE up (id, depth) AS (
			SELECT parent_id, 1 FROM docs_pages WHERE id = $1 AND parent_id IS NOT NULL
			UNION ALL
			SELECT d.parent_id, up.depth + 1 FROM up JOIN docs_pages d ON d.id = up.id WHERE d.parent_id IS NOT NULL AND up.depth < 32
		)
		SELECT d.id, d.title, d.icon, d.kind FROM up JOIN docs_pages d ON d.id = up.id
		WHERE access_level($2, 'docs.page', d.id, $3) >= 1
		ORDER BY up.depth DESC`, p.ID, me, access.ViaAgent(ctx))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Crumb{}
	for rows.Next() {
		var c Crumb
		if err := rows.Scan(&c.ID, &c.Title, &c.Icon, &c.Kind); err != nil {
			return nil, err
		}
		c.Title = displayTitle(c.Title)
		out = append(out, c)
	}
	return out, rows.Err()
}

// ---- create ----------------------------------------------------------------------

type createPageReq struct {
	ParentID *uuid.UUID `json:"parent_id"`
	Kind     string     `json:"kind"` // page (default) | database
	Title    string     `json:"title"`
	Icon     *string    `json:"icon"`
	Shared   bool       `json:"shared"`   // top level: everyone in the org can edit it
	AfterID  *uuid.UUID `json:"after_id"` // the sibling to follow; nil = last
	Inline   bool       `json:"inline"`   // a database shown as a block on its parent
	// TeamID (top level only): a page in that teamspace, which the team can
	// edit. The caller must be on the team; agents can't (it shares).
	TeamID *uuid.UUID `json:"team_id"`
}

// createPage: POST /docs/orgs/{orgID}/pages. Inside a page you can edit, or
// at the top level: private, or shared with the org (docs.create_shared).
func (s *Service) createPage(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	m, _ := orgs.FromContext(ctx)
	var req createPageReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	p, err := s.create(ctx, me, m.OrgID, req)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, p)
	return nil
}

func cleanTitle(t string) (string, error) {
	t = strings.TrimSpace(strings.ReplaceAll(cleanText(t), "\n", " "))
	if utf8.RuneCountInString(t) > maxTitleChars {
		return "", httpx.BadRequest("Keep titles under 300 characters.")
	}
	return t, nil
}

func cleanIcon(icon *string) (*string, error) {
	if icon == nil {
		return nil, nil
	}
	v := strings.TrimSpace(*icon)
	if v == "" {
		return nil, nil
	}
	if utf8.RuneCountInString(v) > 8 || len(v) > 32 || strings.ContainsAny(v, "<>\"'\n") {
		return nil, httpx.BadRequest("Choose one emoji for the icon.")
	}
	return &v, nil
}

// create makes a page or database (used by the route, import, agent tools
// and duplication's callers). It checks the caller's rights itself.
func (s *Service) create(ctx context.Context, me, orgID uuid.UUID, req createPageReq) (*Page, error) {
	if req.Kind == "" {
		req.Kind = "page"
	}
	if req.Kind != "page" && req.Kind != "database" {
		return nil, httpx.BadRequest("kind must be page or database.")
	}
	title, err := cleanTitle(req.Title)
	if err != nil {
		return nil, err
	}
	icon, err := cleanIcon(req.Icon)
	if err != nil {
		return nil, err
	}
	general := access.None
	if req.TeamID != nil {
		if req.ParentID != nil {
			return nil, httpx.BadRequest("Pages inside another page follow its sharing. Leave team_id out.")
		}
		if access.ViaAgent(ctx) {
			return nil, httpx.Forbidden("AI agents can't share pages with teams.")
		}
		var on bool
		if err := s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM access_team_members tm JOIN access_teams t ON t.id = tm.team_id
			WHERE tm.team_id = $1 AND tm.user_id = $2 AND t.org_id = $3)`, *req.TeamID, me, orgID).Scan(&on); err != nil {
			return nil, err
		}
		if !on {
			return nil, httpx.Forbidden("Only people on a team can add pages to its teamspace.")
		}
		req.Shared = false
	}
	if req.ParentID != nil {
		parent, err := requirePage(ctx, s.d.Pool, me, *req.ParentID, access.Edit)
		if err != nil {
			return nil, err
		}
		if parent.OrgID != orgID {
			return nil, errPageNotFound
		}
		if err := editable(parent, false); err != nil {
			return nil, err
		}
		if parent.Kind == "database" {
			return nil, httpx.BadRequest("Add rows from the database instead.")
		}
		req.Shared = false
	} else {
		req.Inline = false
		if req.Shared {
			ok, err := access.Allowed(ctx, s.d.Pool, orgID, me, PermCreateShared)
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, permissionRequired(PermCreateShared)
			}
			general = access.Edit
		}
	}
	if req.Kind != "database" {
		req.Inline = false
	}
	var out *Page
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		pos, err := siblingPosition(ctx, tx, orgID, req.ParentID, req.AfterID)
		if err != nil {
			return err
		}
		var schema any
		if req.Kind == "database" {
			schema = defaultSchema()
		}
		id := uuid.New()
		if _, err := tx.Exec(ctx, `
			INSERT INTO docs_pages (id, org_id, parent_id, kind, inline, title, icon, position, schema, created_by, updated_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10)`,
			id, orgID, req.ParentID, req.Kind, req.Inline, title, icon, pos, jsonOrNil(schema), me); err != nil {
			return err
		}
		res := access.NewResource{Kind: Kind, ID: id, OrgID: orgID, OwnerID: me, General: general}
		if req.ParentID != nil {
			res.Parent = &access.Ref{Kind: Kind, ID: *req.ParentID}
		}
		if err := access.CreateResource(ctx, tx, res); err != nil {
			return err
		}
		if req.TeamID != nil {
			if err := access.AddGrant(ctx, tx, access.Ref{Kind: Kind, ID: id}, access.SubjectRef{Type: "team", ID: *req.TeamID}, access.Edit, me); err != nil {
				return err
			}
		}
		if err := autoFollow(ctx, tx, me, id); err != nil {
			return err
		}
		if req.Kind == "database" {
			if _, err := tx.Exec(ctx, `INSERT INTO docs_views (database_id, name, type, position, created_by) VALUES ($1, 'Table', 'table', 'a0', $2)`, id, me); err != nil {
				return err
			}
		}
		out, err = loadPage(ctx, tx, id, false)
		return err
	})
	if err != nil {
		return nil, err
	}
	out.MyLevel = access.Manage
	if access.ViaAgent(ctx) {
		out.MyLevel = access.Edit
	}
	s.treeChanged(ctx, orgID)
	return out, nil
}

func jsonOrNil(v any) []byte {
	if v == nil {
		return nil
	}
	b, _ := json.Marshal(v)
	return b
}

// siblingPosition returns a position after afterID among the parent's
// children (or the org's top level), or after the last one.
func siblingPosition(ctx context.Context, q db.Querier, orgID uuid.UUID, parentID, afterID *uuid.UUID) (string, error) {
	var after, before string
	if afterID != nil {
		err := q.QueryRow(ctx, `
			SELECT a.position, COALESCE((SELECT min(n.position) FROM docs_pages n
			    WHERE n.org_id = a.org_id AND n.parent_id IS NOT DISTINCT FROM a.parent_id AND n.position > a.position AND n.deleted_at IS NULL), '')
			FROM docs_pages a WHERE a.id = $1 AND a.org_id = $2 AND a.parent_id IS NOT DISTINCT FROM $3`,
			*afterID, orgID, parentID).Scan(&after, &before)
		if err != nil && !db.IsNoRows(err) {
			return "", err
		}
		if err == nil {
			return keyBetween(after, before), nil
		}
	}
	err := q.QueryRow(ctx, `SELECT COALESCE(max(position), '') FROM docs_pages WHERE org_id = $1 AND parent_id IS NOT DISTINCT FROM $2`,
		orgID, parentID).Scan(&after)
	if err != nil {
		return "", err
	}
	return keyBetween(after, ""), nil
}

func permissionRequired(key string) *httpx.Error {
	title := key
	if p, ok := access.PermissionOf(key); ok {
		title = p.Title
	}
	e := httpx.NewError(http.StatusForbidden, "permission_required", "You don't have the \""+title+"\" permission in this "+orgs.Noun()+". Ask an admin.")
	e.Details = map[string]any{"permission": key}
	return e
}

// ---- update ----------------------------------------------------------------------

type updatePageReq struct {
	Title      *string         `json:"title"`
	Icon       json.RawMessage `json:"icon"`        // string, or null to clear
	CustomIcon json.RawMessage `json:"custom_icon"` // CustomIcon (clears icon), or null to clear
	Cover      json.RawMessage `json:"cover"`       // Cover, or null to clear
	FullWidth  *bool           `json:"full_width"`
	Locked     *bool           `json:"locked"`
	Font       *string         `json:"font"`
	SmallText  *bool           `json:"small_text"`
}

// updatePage: PATCH /docs/pages/{id} (edit): title, icon, cover, width, lock.
func (s *Service) updatePage(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req updatePageReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	p, err := requirePage(ctx, s.d.Pool, me, id, access.Edit)
	if err != nil {
		return err
	}
	if err := editable(p, false); err != nil {
		return err
	}
	sets := []string{"updated_at = now()", "updated_by = $2"}
	args := []any{id, me}
	add := func(col string, v any) {
		args = append(args, v)
		sets = append(sets, col+" = $"+itoa(len(args)))
	}
	treeChange := false
	if req.Title != nil {
		if p.Locked {
			return errLocked
		}
		t, err := cleanTitle(*req.Title)
		if err != nil {
			return err
		}
		add("title", t)
		treeChange = true
	}
	if len(req.Icon) > 0 {
		var icon *string
		if string(req.Icon) != "null" {
			var v string
			if err := json.Unmarshal(req.Icon, &v); err != nil {
				return httpx.BadRequest("icon must be an emoji or null.")
			}
			if icon, err = cleanIcon(&v); err != nil {
				return err
			}
		}
		add("icon", icon)
		if icon != nil {
			add("custom_icon", nil)
		}
		treeChange = true
	}
	if len(req.CustomIcon) > 0 {
		var ci *CustomIcon
		if string(req.CustomIcon) != "null" {
			if err := json.Unmarshal(req.CustomIcon, &ci); err != nil || ci == nil {
				return httpx.BadRequest("custom_icon must be {type: lucide, name, color} or {type: image, file_id}.")
			}
			if err := s.checkCustomIcon(ctx, id, ci); err != nil {
				return err
			}
			add("icon", nil)
		}
		if ci == nil {
			add("custom_icon", nil)
		} else {
			add("custom_icon", jsonOrNil(ci))
		}
		treeChange = true
	}
	if req.Font != nil {
		if !pageFonts[*req.Font] {
			return httpx.BadRequest("font must be default, serif or mono.")
		}
		add("font", *req.Font)
	}
	if req.SmallText != nil {
		add("small_text", *req.SmallText)
	}
	if len(req.Cover) > 0 {
		var cover *Cover
		if string(req.Cover) != "null" {
			if err := json.Unmarshal(req.Cover, &cover); err != nil || cover == nil {
				return httpx.BadRequest("cover must be {file_id} or {color} with a position.")
			}
			if err := s.checkCover(ctx, id, cover); err != nil {
				return err
			}
		}
		add("cover", jsonOrNil(cover))
	}
	if req.FullWidth != nil {
		add("full_width", *req.FullWidth)
	}
	if req.Locked != nil {
		add("locked", *req.Locked)
	}
	if len(args) == 2 {
		return httpx.BadRequest("Nothing to change.")
	}
	if _, err := s.d.Pool.Exec(ctx, `UPDATE docs_pages SET `+strings.Join(sets, ", ")+` WHERE id = $1`, args...); err != nil {
		return err
	}
	out, err := loadPage(ctx, s.d.Pool, id, false)
	if err != nil {
		return err
	}
	out.MyLevel = p.MyLevel
	s.pageChanged(ctx, out, treeChange)
	httpx.OK(w, out)
	return nil
}

func (s *Service) checkCover(ctx context.Context, pageID uuid.UUID, c *Cover) error {
	c.Position = min(max(c.Position, 0), 100)
	if c.FileID != nil {
		if err := s.checkPageImage(ctx, pageID, *c.FileID); err != nil {
			return httpx.BadRequest("Upload an image to this page for the cover.")
		}
		c.Color, c.Gallery = "", ""
		return nil
	}
	if c.Gallery != "" {
		if !coverGallery[c.Gallery] {
			return httpx.BadRequest("Choose a cover from the gallery or upload an image.")
		}
		c.Color = ""
		return nil
	}
	if !coverColors[c.Color] {
		return httpx.BadRequest("Choose a cover colour or upload an image.")
	}
	return nil
}

// checkPageImage: the file must be an image uploaded to this page.
func (s *Service) checkPageImage(ctx context.Context, pageID, fileID uuid.UUID) error {
	var ct string
	err := s.d.Pool.QueryRow(ctx, `SELECT content_type FROM docs_files WHERE id = $1 AND page_id = $2`, fileID, pageID).Scan(&ct)
	if db.IsNoRows(err) || (err == nil && !strings.HasPrefix(ct, "image/")) {
		return httpx.BadRequest("Upload an image to this page first.")
	}
	return err
}

// checkCustomIcon validates a lucide icon (name, palette colour) or an
// image uploaded to the page.
func (s *Service) checkCustomIcon(ctx context.Context, pageID uuid.UUID, c *CustomIcon) error {
	switch c.Type {
	case "lucide":
		if len(c.Name) > 48 || !iconNameRE.MatchString(c.Name) {
			return httpx.BadRequest("Choose an icon from the list.")
		}
		if c.Color == "" {
			c.Color = "neutral"
		}
		if !coverColors[c.Color] {
			return httpx.BadRequest("Choose an icon colour from the palette.")
		}
		c.FileID = nil
	case "image":
		if c.FileID == nil {
			return httpx.BadRequest("Upload an image for the icon.")
		}
		if err := s.checkPageImage(ctx, pageID, *c.FileID); err != nil {
			return err
		}
		c.Name, c.Color = "", ""
	default:
		return httpx.BadRequest("custom_icon.type must be lucide or image.")
	}
	return nil
}

// pageChanged announces new page fields to its viewers (and its database,
// for rows) and pings the sidebar when the title or icon moved.
func (s *Service) pageChanged(ctx context.Context, p *Page, tree bool) {
	pub := *p
	pub.MyLevel = access.None
	s.publish(ctx, pageTopic(p.ID), EventPage, map[string]any{"page": pub})
	if p.IsRow && p.ParentID != nil {
		s.publish(ctx, pageTopic(*p.ParentID), EventRows, map[string]any{"database_id": *p.ParentID, "row_id": p.ID})
	}
	if tree && !p.IsRow {
		s.treeChanged(ctx, p.OrgID)
	}
}

// ---- move ------------------------------------------------------------------------

type moveReq struct {
	ParentID *uuid.UUID `json:"parent_id"` // nil: the top level
	AfterID  *uuid.UUID `json:"after_id"`  // sibling to follow; nil: first
	Last     bool       `json:"last"`      // with no after_id: put it last instead of first
}

// movePage: POST /docs/pages/{id}/move. Under another page: edit on both.
// To the top level: manage (it stops inheriting the old parent's sharing).
func (s *Service) movePage(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req moveReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	p, err := requirePage(ctx, s.d.Pool, me, id, access.Edit)
	if err != nil {
		return err
	}
	if err := editable(p, false); err != nil {
		return err
	}
	if p.IsRow {
		return httpx.BadRequest("Database rows stay in their database. Reorder them there.")
	}
	sameParent := (req.ParentID == nil && p.ParentID == nil) || (req.ParentID != nil && p.ParentID != nil && *req.ParentID == *p.ParentID)
	if req.ParentID == nil && !sameParent && p.MyLevel < access.Manage {
		return httpx.NewError(http.StatusForbidden, "access_level_required", "Only people who manage this page can move it to the top level.")
	}
	if req.ParentID != nil && !sameParent {
		if *req.ParentID == id {
			return httpx.BadRequest("A page can't go inside itself.")
		}
		parent, err := requirePage(ctx, s.d.Pool, me, *req.ParentID, access.Edit)
		if err != nil {
			return err
		}
		if parent.OrgID != p.OrgID || parent.DeletedAt != nil {
			return errPageNotFound
		}
		if parent.Kind == "database" {
			return httpx.BadRequest("Pages can't go inside a database. Add a row instead.")
		}
	}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var pos string
		if req.AfterID != nil || req.Last {
			var err error
			if pos, err = siblingPosition(ctx, tx, p.OrgID, req.ParentID, req.AfterID); err != nil {
				return err
			}
		} else {
			var first string
			if err := tx.QueryRow(ctx, `SELECT COALESCE(min(position), '') FROM docs_pages WHERE org_id = $1 AND parent_id IS NOT DISTINCT FROM $2 AND id <> $3`,
				p.OrgID, req.ParentID, id).Scan(&first); err != nil {
				return err
			}
			pos = keyBetween("", first)
		}
		if !sameParent {
			var parentRef *access.Ref
			if req.ParentID != nil {
				parentRef = &access.Ref{Kind: Kind, ID: *req.ParentID}
			}
			if err := access.MoveResource(ctx, tx, access.Ref{Kind: Kind, ID: id}, parentRef); err != nil {
				if errors.Is(err, access.ErrCycle) {
					return httpx.BadRequest("A page can't go inside one of its own subpages.")
				}
				return err
			}
		}
		_, err := tx.Exec(ctx, `UPDATE docs_pages SET parent_id = $2, position = $3, updated_at = now(), updated_by = $4 WHERE id = $1`,
			id, req.ParentID, pos, me)
		return err
	})
	if err != nil {
		return err
	}
	if !sameParent {
		access.Changed(ctx, access.Ref{Kind: Kind, ID: id}, nil)
	}
	out, err := loadPage(ctx, s.d.Pool, id, false)
	if err != nil {
		return err
	}
	out.MyLevel, _ = access.LevelOf(ctx, s.d.Pool, me, Kind, id)
	s.pageChanged(ctx, out, true)
	httpx.OK(w, out)
	return nil
}

// ---- trash -----------------------------------------------------------------------

// trashPage: DELETE /docs/pages/{id} (edit): the page and everything under
// it go to the trash.
func (s *Service) trashPage(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	p, err := requirePage(ctx, s.d.Pool, me, id, access.Edit)
	if err != nil {
		return err
	}
	if p.DeletedAt != nil {
		httpx.NoContent(w)
		return nil
	}
	if _, err := s.d.Pool.Exec(ctx, `
		WITH RECURSIVE tree (id, depth) AS (
			SELECT $1::uuid, 0
			UNION ALL
			SELECT c.id, t.depth + 1 FROM tree t JOIN docs_pages c ON c.parent_id = t.id WHERE c.deleted_at IS NULL AND t.depth < 64
		)
		UPDATE docs_pages SET deleted_at = now(), deleted_by = $2, trash_root_id = $1, updated_at = now()
		WHERE id IN (SELECT id FROM tree) AND deleted_at IS NULL`, id, me); err != nil {
		return err
	}
	p.DeletedAt = ptr(time.Now())
	s.pageChanged(ctx, p, true)
	s.treeChanged(ctx, p.OrgID)
	httpx.NoContent(w)
	return nil
}

// restorePage: POST /docs/pages/{id}/restore (edit).
func (s *Service) restorePage(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	p, err := requirePage(ctx, s.d.Pool, me, id, access.Edit)
	if err != nil {
		return err
	}
	if p.DeletedAt == nil {
		httpx.OK(w, p)
		return nil
	}
	var root *uuid.UUID
	var parentTrashed bool
	if err := s.d.Pool.QueryRow(ctx, `
		SELECT p.trash_root_id, COALESCE((SELECT q.deleted_at IS NOT NULL FROM docs_pages q WHERE q.id = p.parent_id), false)
		FROM docs_pages p WHERE p.id = $1`, id).Scan(&root, &parentTrashed); err != nil {
		return err
	}
	if root == nil || *root != id || parentTrashed {
		return httpx.NewError(http.StatusConflict, "parent_in_trash", "This page went to the trash with a page above it. Restore that page instead.")
	}
	if _, err := s.d.Pool.Exec(ctx, `UPDATE docs_pages SET deleted_at = NULL, deleted_by = NULL, trash_root_id = NULL, updated_at = now()
		WHERE trash_root_id = $1`, id); err != nil {
		return err
	}
	out, err := loadPage(ctx, s.d.Pool, id, false)
	if err != nil {
		return err
	}
	out.MyLevel = p.MyLevel
	s.pageChanged(ctx, out, true)
	httpx.OK(w, out)
	return nil
}

// deletePermanently: DELETE /docs/pages/{id}/permanent (manage, from the
// trash): the page, its subpages, blocks, comments, history and files go.
func (s *Service) deletePermanently(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	p, err := requirePage(ctx, s.d.Pool, me, id, access.Manage)
	if err != nil {
		return err
	}
	if p.DeletedAt == nil {
		return httpx.NewError(http.StatusConflict, "not_in_trash", "Move the page to the trash first.")
	}
	keys, err := s.purge(ctx, []uuid.UUID{id})
	if err != nil {
		return err
	}
	s.deleteObjects(ctx, keys)
	httpx.NoContent(w)
	return nil
}

// purge deletes pages for good (with their subtrees) and returns the
// storage keys no other page refers to any more.
func (s *Service) purge(ctx context.Context, ids []uuid.UUID) ([]string, error) {
	var keys []string
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			WITH RECURSIVE tree (id, depth) AS (
				SELECT unnest($1::uuid[]), 0
				UNION ALL
				SELECT c.id, t.depth + 1 FROM tree t JOIN docs_pages c ON c.parent_id = t.id WHERE t.depth < 64
			)
			SELECT DISTINCT f.key FROM docs_files f WHERE f.page_id IN (SELECT id FROM tree)`, ids)
		if err != nil {
			return err
		}
		candidates, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		for _, id := range ids {
			if err := access.DeleteResource(ctx, tx, access.Ref{Kind: Kind, ID: id}); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM docs_pages WHERE id = ANY ($1)`, ids); err != nil {
			return err
		}
		if len(candidates) == 0 {
			return nil
		}
		rows, err = tx.Query(ctx, `SELECT k FROM unnest($1::text[]) k WHERE NOT EXISTS (SELECT 1 FROM docs_files f WHERE f.key = k)`, candidates)
		if err != nil {
			return err
		}
		keys, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	})
	return keys, err
}

func (s *Service) deleteObjects(ctx context.Context, keys []string) {
	if s.d.Storage == nil {
		return
	}
	for _, k := range keys {
		if err := s.d.Storage.Delete(context.WithoutCancel(ctx), k); err != nil {
			slogWarn(ctx, "docs: delete object", err)
		}
	}
}

// ---- favorites, backlinks --------------------------------------------------------

func (s *Service) favorite(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	if _, err := access.Require(ctx, s.d.Pool, me, Kind, id, access.View); err != nil {
		return err
	}
	var n int
	if err := s.d.Pool.QueryRow(ctx, `SELECT count(*) FROM docs_favorites WHERE user_id = $1`, me).Scan(&n); err != nil {
		return err
	}
	if n >= 200 {
		return httpx.Conflict("You have 200 favorites. Remove one first.")
	}
	if _, err := s.d.Pool.Exec(ctx, `INSERT INTO docs_favorites (user_id, page_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, me, id); err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

func (s *Service) unfavorite(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context()).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	if _, err := s.d.Pool.Exec(r.Context(), `DELETE FROM docs_favorites WHERE user_id = $1 AND page_id = $2`, me, id); err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

// backlinks: GET /docs/pages/{id}/backlinks: pages that mention or link to
// this one, among those the caller can open.
func (s *Service) backlinks(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	if _, err := access.Require(ctx, s.d.Pool, me, Kind, id, access.View); err != nil {
		return err
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT `+summaryCols+`, access_level($1, 'docs.page', p.id, $3), false
		FROM docs_pages p
		WHERE p.id IN (SELECT source_page_id FROM docs_links WHERE target_page_id = $2) AND p.id <> $2
		  AND p.deleted_at IS NULL AND access_level($1, 'docs.page', p.id, $3) >= 1
		ORDER BY lower(p.title), p.id LIMIT 100`, me, id, access.ViaAgent(ctx))
	if err != nil {
		return err
	}
	list, err := scanSummaries(rows, func(ps *PageSummary) []any { return []any{&ps.HasChildren} })
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": list})
	return nil
}
