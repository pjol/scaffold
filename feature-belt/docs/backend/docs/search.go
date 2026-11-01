package docs

import (
	"context"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
)

// searchOpts narrows a page search.
type searchOpts struct {
	orgID    *uuid.UUID // nil: every org the caller belongs to
	kind     string     // "", page or database
	noRows   bool       // leave database rows out
	minLevel access.Level
	limit    int
}

// SearchHit is a page that matched, with the best snippet.
type SearchHit struct {
	PageSummary
	OrgID uuid.UUID `json:"org_id"`
	Score float64   `json:"score"`
}

var wordRE = regexp.MustCompile(`[\p{L}\p{N}]+`)

// tsQuery turns free text into a prefix-matching tsquery ("q3 pla" →
// 'q3':* & 'pla':*), or "" when it has no words.
func tsQuery(q string) string {
	words := wordRE.FindAllString(strings.ToLower(q), 8)
	parts := make([]string, 0, len(words))
	for _, w := range words {
		parts = append(parts, "'"+w+"':*")
	}
	return strings.Join(parts, " & ")
}

// searchPages finds pages by title and block text among those the caller
// can open (agent-capped through ctx): the one query behind the search
// route, global search and the agent tool.
func (s *Service) searchPages(ctx context.Context, me uuid.UUID, query string, o searchOpts) ([]SearchHit, error) {
	tq := tsQuery(query)
	if tq == "" || o.limit <= 0 {
		return []SearchHit{}, nil
	}
	rows, err := s.d.Pool.Query(ctx, `
		WITH v AS MATERIALIZED (SELECT id, level FROM access_visible($1, 'docs.page', $2)),
		tq AS (SELECT to_tsquery('simple', $3) AS q),
		hits AS (
			SELECT p.id AS page_id, 1.0::float8 AS score, ''::text AS snip
			FROM docs_pages p, tq WHERE to_tsvector('simple', p.title) @@ tq.q
			UNION ALL
			SELECT x.page_id, 0.6, x.plain FROM (
				SELECT b.page_id, b.plain FROM docs_blocks b, tq WHERE to_tsvector('simple', b.plain) @@ tq.q LIMIT 5000) x
		),
		best AS (
			SELECT page_id, max(score) AS score, (array_agg(snip ORDER BY score, length(snip)) FILTER (WHERE snip <> ''))[1] AS snip
			FROM hits GROUP BY page_id
		)
		SELECT `+summaryCols+`, v.level, p.org_id, best.score + CASE WHEN lower(p.title) = lower($4) THEN 0.3 ELSE 0 END, COALESCE(best.snip, '')
		FROM best
		JOIN docs_pages p ON p.id = best.page_id
		JOIN v ON v.id = p.id
		WHERE p.deleted_at IS NULL AND ($5::uuid IS NULL OR p.org_id = $5)
		  AND p.org_id IN (SELECT org_id FROM org_members WHERE user_id = $1)
		  AND ($6 = '' OR p.kind = $6) AND (NOT $7 OR NOT p.is_row) AND v.level >= $8
		ORDER BY 12 DESC, p.updated_at DESC, p.id LIMIT $9`,
		me, access.ViaAgent(ctx), tq, strings.TrimSpace(query), o.orgID, o.kind, o.noRows, int(o.minLevel), o.limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SearchHit{}
	q := strings.ToLower(strings.TrimSpace(query))
	for rows.Next() {
		var h SearchHit
		var lvl int
		var snip string
		if err := rows.Scan(&h.ID, &h.ParentID, &h.Kind, &h.IsRow, &h.Title, &h.Icon, &h.Position, &h.UpdatedAt, &h.DeletedAt, &lvl, &h.OrgID, &h.Score, &snip); err != nil {
			return nil, err
		}
		h.MyLevel = access.Level(lvl)
		h.Title = displayTitle(h.Title)
		h.Snippet = snippetAround(snip, q)
		h.Score = min(h.Score, 1)
		out = append(out, h)
	}
	return out, rows.Err()
}

// snippetAround returns up to 140 characters of text around the first word
// of q.
func snippetAround(text, q string) string {
	text = strings.Join(strings.Fields(text), " ")
	r := []rune(text)
	if len(r) <= 140 {
		return text
	}
	at := 0
	if words := wordRE.FindAllString(q, 1); len(words) > 0 {
		if i := strings.Index(strings.ToLower(text), words[0]); i >= 0 {
			at = utf8.RuneCountInString(text[:i])
		}
	}
	start := max(0, at-40)
	end := min(start+140, len(r))
	out := string(r[start:end])
	if start > 0 {
		out = "…" + out
	}
	if end < len(r) {
		out += "…"
	}
	return out
}

// searchRoute: GET /docs/orgs/{orgID}/search?q=&kind=&rows=false&min_level=edit&limit=
// (pickers and the in-workspace search). An empty q returns recent pages.
func (s *Service) searchRoute(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	m, _ := orgs.FromContext(ctx)
	qp := r.URL.Query()
	o := searchOpts{orgID: &m.OrgID, kind: qp.Get("kind"), noRows: qp.Get("rows") == "false", minLevel: access.View, limit: 20}
	if o.kind != "" && o.kind != "page" && o.kind != "database" {
		return httpx.BadRequest("kind must be page or database.")
	}
	if l, ok := access.ParseLevel(qp.Get("min_level")); ok && l > access.None {
		o.minLevel = l
	}
	if v, err := strconv.Atoi(qp.Get("limit")); err == nil && v > 0 {
		o.limit = min(v, 50)
	}
	q := strings.TrimSpace(qp.Get("q"))
	if q == "" {
		rows, err := s.d.Pool.Query(ctx, `
			SELECT `+summaryCols+`, access_level($1, 'docs.page', p.id, $3), p.org_id, 0.5::float8, ''
			FROM docs_visits v JOIN docs_pages p ON p.id = v.page_id
			WHERE v.user_id = $1 AND p.org_id = $2 AND p.deleted_at IS NULL AND ($4 = '' OR p.kind = $4) AND (NOT $5 OR NOT p.is_row)
			  AND access_level($1, 'docs.page', p.id, $3) >= $6
			ORDER BY v.visited_at DESC, p.id LIMIT $7`, me, m.OrgID, access.ViaAgent(ctx), o.kind, o.noRows, int(o.minLevel), o.limit)
		if err != nil {
			return err
		}
		hits, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (SearchHit, error) {
			var h SearchHit
			var lvl int
			var snip string
			err := row.Scan(&h.ID, &h.ParentID, &h.Kind, &h.IsRow, &h.Title, &h.Icon, &h.Position, &h.UpdatedAt, &h.DeletedAt, &lvl, &h.OrgID, &h.Score, &snip)
			h.MyLevel = access.Level(lvl)
			h.Title = displayTitle(h.Title)
			return h, err
		})
		if err != nil {
			return err
		}
		if hits == nil {
			hits = []SearchHit{}
		}
		httpx.OK(w, map[string]any{"items": hits})
		return nil
	}
	hits, err := s.searchPages(ctx, me, q, o)
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": hits})
	return nil
}

// Search implements app.SearchProvider: page titles and text the caller can
// open, through the same query as the search route, in one org when the
// search is scoped (app.SearchOrg). Context is the org, then the parent page
// when the caller can open it.
func (s *Service) Search(ctx context.Context, caller *auth.Identity, query string, limit int) ([]app.SearchResult, error) {
	o := searchOpts{minLevel: access.View, limit: limit}
	if id, ok := app.SearchOrg(ctx); ok {
		o.orgID = &id
	}
	hits, err := s.searchPages(ctx, caller.UserID, query, o)
	if err != nil {
		return nil, err
	}
	orgNames, parents, err := s.searchContext(ctx, caller.UserID, hits)
	if err != nil {
		return nil, err
	}
	out := make([]app.SearchResult, 0, len(hits))
	for _, h := range hits {
		icon := "file-text"
		if h.Kind == "database" {
			icon = "table"
		}
		title := h.Title
		if h.Icon != nil {
			title = *h.Icon + " " + title
		}
		parent := ""
		if h.ParentID != nil {
			parent = parents[*h.ParentID]
		}
		out = append(out, app.SearchResult{
			Kind: Kind, ID: h.ID.String(), Title: title, Snippet: h.Snippet, Link: "/docs/" + h.ID.String(),
			Icon: icon, Score: h.Score, At: h.UpdatedAt,
			OrgID: h.OrgID.String(), Context: app.SearchContext(orgNames[h.OrgID], parent),
		})
	}
	return out, nil
}

// searchContext looks up the org names of hits and the titles of their parent
// pages the caller can open (a parent they can't open stays unnamed).
func (s *Service) searchContext(ctx context.Context, me uuid.UUID, hits []SearchHit) (map[uuid.UUID]string, map[uuid.UUID]string, error) {
	orgNames, parents := map[uuid.UUID]string{}, map[uuid.UUID]string{}
	if len(hits) == 0 {
		return orgNames, parents, nil
	}
	var orgIDs, parentIDs []uuid.UUID
	for _, h := range hits {
		orgIDs = append(orgIDs, h.OrgID)
		if h.ParentID != nil {
			parentIDs = append(parentIDs, *h.ParentID)
		}
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT id, name, true FROM orgs WHERE id = ANY ($2)
		UNION ALL
		SELECT p.id, p.title, false FROM docs_pages p
		WHERE p.id = ANY ($3) AND p.deleted_at IS NULL AND access_level($1, 'docs.page', p.id, $4) >= $5`,
		me, orgIDs, parentIDs, access.ViaAgent(ctx), int(access.View))
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id    uuid.UUID
			name  string
			isOrg bool
		)
		if err := rows.Scan(&id, &name, &isOrg); err != nil {
			return nil, nil, err
		}
		if isOrg {
			orgNames[id] = name
		} else {
			parents[id] = displayTitle(name)
		}
	}
	return orgNames, parents, rows.Err()
}
