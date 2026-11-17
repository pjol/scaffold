package docs

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
)

// FindHit is a quick-find result: the page, where it lives and who made it.
type FindHit struct {
	PageSummary
	ParentTitle *string   `json:"parent_title"` // the parent page, when the caller can open it
	CreatedBy   *RefUser  `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
}

// find: GET /docs/orgs/{orgID}/find?q=&kind=&created_by=me|<id>&edited=
// today|week|month|year&in=<page id>&sort=relevance|edited|created&limit=
// (member, 120/min): quick find (⌘P) in the workspace, by title and text,
// with filters; without q, the recently edited pages that match. Only pages
// the caller can open, never templates.
func (s *Service) find(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	m, _ := orgs.FromContext(ctx)
	qp := r.URL.Query()
	kind := qp.Get("kind")
	if kind != "" && kind != "page" && kind != "database" {
		return httpx.BadRequest("kind must be page or database.")
	}
	var by *uuid.UUID
	switch v := qp.Get("created_by"); v {
	case "":
	case "me":
		by = &me
	default:
		id, err := uuid.Parse(v)
		if err != nil {
			return httpx.BadRequest("created_by must be me or a person's id.")
		}
		by = &id
	}
	var since *time.Time
	now := time.Now()
	switch qp.Get("edited") {
	case "":
	case "today":
		t := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		since = &t
	case "week":
		t := now.AddDate(0, 0, -7)
		since = &t
	case "month":
		t := now.AddDate(0, -1, 0)
		since = &t
	case "year":
		t := now.AddDate(-1, 0, 0)
		since = &t
	default:
		return httpx.BadRequest("edited must be today, week, month or year.")
	}
	var in *uuid.UUID
	if v := qp.Get("in"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			return httpx.BadRequest("in must be a page id.")
		}
		if _, err := access.Require(ctx, s.d.Pool, me, Kind, id, access.View); err != nil {
			return err
		}
		in = &id
	}
	sort := qp.Get("sort")
	if sort == "" {
		sort = "relevance"
	}
	order := map[string]string{"relevance": "score DESC, p.updated_at DESC", "edited": "p.updated_at DESC", "created": "p.created_at DESC"}[sort]
	if order == "" {
		return httpx.BadRequest("sort must be relevance, edited or created.")
	}
	limit := 20
	if v, err := strconv.Atoi(qp.Get("limit")); err == nil && v > 0 {
		limit = min(v, 50)
	}
	q := strings.TrimSpace(qp.Get("q"))
	tq := tsQuery(q)
	if q != "" && tq == "" {
		httpx.OK(w, map[string]any{"items": []FindHit{}})
		return nil
	}
	rows, err := s.d.Pool.Query(ctx, `
		WITH v AS MATERIALIZED (SELECT id, level FROM access_visible($1, 'docs.page', $3)),
		scope AS (
			SELECT x.id FROM (
				WITH RECURSIVE tree (id, depth) AS (
					SELECT $8::uuid, 0 WHERE $8::uuid IS NOT NULL
					UNION ALL
					SELECT c.id, t.depth + 1 FROM tree t JOIN docs_pages c ON c.parent_id = t.id WHERE t.depth < 32
				) SELECT id FROM tree) x
		),
		hits AS (
			SELECT p.id AS page_id, 1.0::float8 AS score, ''::text AS snip
			FROM docs_pages p WHERE p.org_id = $2 AND ($4 = '' OR to_tsvector('simple', p.title) @@ to_tsquery('simple', $4))
			UNION ALL
			SELECT x.page_id, 0.6, x.plain FROM (
				SELECT b.page_id, b.plain FROM docs_blocks b WHERE $4 <> '' AND to_tsvector('simple', b.plain) @@ to_tsquery('simple', $4) LIMIT 5000) x
		),
		best AS (
			SELECT page_id, max(score) AS score, (array_agg(snip ORDER BY score, length(snip)) FILTER (WHERE snip <> ''))[1] AS snip
			FROM hits GROUP BY page_id
		)
		SELECT `+summaryCols+`, v.level, false, p.custom_icon,
		       CASE WHEN pp.id IS NOT NULL AND EXISTS (SELECT 1 FROM v vp WHERE vp.id = pp.id) THEN pp.title END,
		       cu.id, cu.display_name, p.created_at, COALESCE(best.snip, ''),
		       best.score + CASE WHEN lower(p.title) = lower($5) THEN 0.3 ELSE 0 END AS score
		FROM best
		JOIN docs_pages p ON p.id = best.page_id
		JOIN v ON v.id = p.id
		LEFT JOIN docs_pages pp ON pp.id = p.parent_id
		LEFT JOIN users cu ON cu.id = p.created_by AND cu.status <> 'deleted'
		WHERE p.org_id = $2 AND p.deleted_at IS NULL AND NOT p.is_template AND NOT p.is_row
		  AND ($6 = '' OR p.kind = $6)
		  AND ($7::uuid IS NULL OR p.created_by = $7)
		  AND ($8::uuid IS NULL OR p.id IN (SELECT id FROM scope WHERE id <> $8))
		  AND ($9::timestamptz IS NULL OR p.updated_at >= $9)
		ORDER BY `+order+`, p.id LIMIT $10`,
		me, m.OrgID, access.ViaAgent(ctx), tq, q, kind, by, in, since, limit)
	if err != nil {
		return err
	}
	type extra struct {
		parent    *string
		uid       *uuid.UUID
		uname     *string
		createdAt time.Time
		snip      string
		score     float64
	}
	var ex []extra
	list, err := summaries(rows, func(ps *PageSummary) []any {
		ex = append(ex, extra{})
		e := &ex[len(ex)-1]
		return []any{&e.parent, &e.uid, &e.uname, &e.createdAt, &e.snip, &e.score}
	})
	if err != nil {
		return err
	}
	out := make([]FindHit, len(list))
	ql := strings.ToLower(q)
	for i, ps := range list {
		ps.Title = displayTitle(ps.Title)
		ps.Snippet = snippetAround(ex[i].snip, ql)
		h := FindHit{PageSummary: ps, CreatedAt: ex[i].createdAt}
		if ex[i].parent != nil {
			h.ParentTitle = ptr(displayTitle(*ex[i].parent))
		}
		if ex[i].uid != nil && ex[i].uname != nil {
			h.CreatedBy = &RefUser{ID: *ex[i].uid, Name: *ex[i].uname}
		}
		out[i] = h
	}
	httpx.OK(w, map[string]any{"items": out})
	return nil
}
