package baskets

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/audit"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/exchange"
	"scaffoldapp/backend/internal/httpx"
)

// MountUser: discovery, preview, search and create for signed-in people.
func (s *Service) MountUser(r chi.Router) {
	r.With(auth.RateLimit(60, time.Minute)).Post("/baskets/preview", httpx.Handler(s.handlePreview))
	r.Get("/baskets/search", httpx.Handler(s.handleSearch))
	r.Get("/baskets", httpx.Handler(s.handleList))
	r.Get("/baskets/mine", httpx.Handler(s.handleMine))
	r.Get("/baskets/{symbol}", httpx.Handler(s.handleGet))
	w := r.With(auth.RequireActive)
	w.With(auth.RateLimit(20, time.Hour)).Post("/baskets", httpx.Handler(s.handleCreate))
	w.With(auth.RateLimit(10, time.Hour)).Post("/baskets/{symbol}/report", httpx.Handler(s.handleReport))
}

// sourceOf names where a request came from (the client's X-Client-Platform).
func sourceOf(r *http.Request) string {
	switch p := strings.ToLower(strings.TrimSpace(r.Header.Get("X-Client-Platform"))); p {
	case "web", "ios", "android":
		return p
	}
	return "api"
}

func (s *Service) handlePreview(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	var in Input
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	p, err := s.Preview(r.Context(), me.UserID, in)
	if err != nil {
		return err
	}
	httpx.OK(w, p)
	return nil
}

func (s *Service) handleSearch(w http.ResponseWriter, r *http.Request) error {
	limit := 20
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 {
		limit = min(v, 50)
	}
	items, err := s.Search(r.Context(), r.URL.Query().Get("q"), limit)
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": items})
	return nil
}

// Search ranks markets a basket may hold (the app's Resolver when set).
func (s *Service) Search(ctx context.Context, q string, limit int) ([]Candidate, error) {
	ix, err := s.index(ctx)
	if err != nil {
		return nil, err
	}
	items, err := s.resolver(ix).Search(ctx, strings.TrimSpace(q), limit)
	if err != nil {
		return nil, err
	}
	out := make([]Candidate, 0, len(items))
	for _, c := range items {
		if _, listed := ix.bySymbol[strings.ToUpper(c.Symbol)]; listed {
			out = append(out, c)
		}
	}
	return out, nil
}

func (s *Service) handleCreate(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	var req CreateRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	res, err := s.Create(r.Context(), me.UserID, sourceOf(r), req)
	if err != nil {
		return err
	}
	status := http.StatusOK
	if res.Created {
		status = http.StatusCreated
	}
	httpx.JSON(w, status, res)
	return nil
}

// parseList reads sort, status, q, limit and cursor.
func parseList(r *http.Request) (listQuery, error) {
	qs := r.URL.Query()
	pg := httpx.ParsePage(r)
	lq := listQuery{sort: qs.Get("sort"), status: qs.Get("status"), q: qs.Get("q"), limit: pg.Limit}
	switch lq.sort {
	case "":
		lq.sort = "new"
	case "new", "popular":
	default:
		return lq, httpx.BadRequest("sort must be new or popular.")
	}
	switch lq.status {
	case "":
		lq.status = "live"
	case "live", StatusArchived, "all":
	default:
		return lq, httpx.BadRequest("status must be live, archived or all.")
	}
	if len(lq.q) > 100 {
		return lq, httpx.BadRequest("q is at most 100 characters.")
	}
	if pg.Cursor != "" {
		var c listCursor
		if err := httpx.DecodeCursor(pg.Cursor, &c); err != nil {
			return lq, err
		}
		if lq.sort == "popular" && c.P == "" {
			return lq, httpx.BadRequest("Invalid cursor.")
		}
		lq.after = &c
	}
	return lq, nil
}

func (s *Service) page(ctx context.Context, rows []basketRow, caller uuid.UUID, limit int, sortBy string) (httpx.Page[Summary], error) {
	items := make([]Summary, len(rows))
	ptrs := make([]*Summary, len(rows))
	for i, b := range rows {
		items[i] = s.summary(b, caller)
		ptrs[i] = &items[i]
	}
	if err := fillLines(ctx, s.d.Pool, ptrs); err != nil {
		return httpx.Page[Summary]{}, err
	}
	pop := map[uuid.UUID]string{}
	for _, b := range rows {
		pop[b.ID] = b.Popularity.String()
	}
	return httpx.NewPage(items, limit, func(it Summary) any {
		c := listCursor{T: it.CreatedAt, ID: it.ID}
		if sortBy == "popular" {
			c.P = pop[it.ID]
		}
		return c
	}), nil
}

func (s *Service) handleList(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	lq, err := parseList(r)
	if err != nil {
		return err
	}
	rows, err := s.list(r.Context(), lq)
	if err != nil {
		return err
	}
	pg, err := s.page(r.Context(), rows, me.UserID, lq.limit, lq.sort)
	if err != nil {
		return err
	}
	httpx.OK(w, pg)
	return nil
}

func (s *Service) handleMine(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	pg := httpx.ParsePage(r)
	var after *listCursor
	if pg.Cursor != "" {
		var c listCursor
		if err := httpx.DecodeCursor(pg.Cursor, &c); err != nil {
			return err
		}
		after = &c
	}
	rows, err := s.mine(r.Context(), me.UserID, pg.Limit, after)
	if err != nil {
		return err
	}
	items := make([]Summary, len(rows))
	ptrs := make([]*Summary, len(rows))
	cursors := map[uuid.UUID]listCursor{}
	for i, m := range rows {
		items[i] = s.summary(m.basketRow, me.UserID)
		ptrs[i] = &items[i]
		cursors[m.ID] = listCursor{T: m.EnteredAt, ID: m.EntryID}
	}
	if err := fillLines(r.Context(), s.d.Pool, ptrs); err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(items, pg.Limit, func(it Summary) any { return cursors[it.ID] }))
	return nil
}

func (s *Service) handleGet(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	b, err := basketBy(r.Context(), s.d.Pool, "symbol", chi.URLParam(r, "symbol"))
	if err != nil {
		return err
	}
	d, err := s.detail(r.Context(), b, me.UserID)
	if err != nil {
		return err
	}
	httpx.OK(w, d)
	return nil
}

// ReportRequest is POST /v1/baskets/{symbol}/report.
type ReportRequest struct {
	Reason string `json:"reason"` // name | description | spam | other
	Note   string `json:"note"`
}

func (s *Service) handleReport(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	var req ReportRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	switch req.Reason {
	case "name", "description", "spam", "other":
	default:
		return httpx.BadRequest("reason must be name, description, spam or other.")
	}
	note := strings.TrimSpace(req.Note)
	if len([]rune(note)) > 500 {
		return httpx.BadRequest("A note is at most 500 characters.")
	}
	b, err := basketBy(ctx, s.d.Pool, "symbol", chi.URLParam(r, "symbol"))
	if err != nil {
		return err
	}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `INSERT INTO basket_reports (basket_id, reporter_id, reason, note) VALUES ($1,$2,$3,$4)
 ON CONFLICT (basket_id, reporter_id) DO UPDATE SET reason = EXCLUDED.reason, note = EXCLUDED.note, created_at = now(), resolved_at = NULL
 WHERE basket_reports.resolved_at IS NOT NULL`, b.ID, me.UserID, req.Reason, note)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE baskets SET open_reports = open_reports + 1 WHERE id = $1`, b.ID)
		return err
	})
	if err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

// ---- Admin → Baskets ----

// MountAdmin: staff list, rename, hide and resolve reports; admins archive.
func (s *Service) MountAdmin(r chi.Router) {
	r.Get("/baskets", httpx.Handler(s.adminList))
	r.Patch("/baskets/{symbol}", httpx.Handler(s.adminRename))
	r.Post("/baskets/{symbol}/hide", httpx.Handler(s.adminHide))
	r.Post("/baskets/{symbol}/reports/resolve", httpx.Handler(s.adminResolve))
	r.With(auth.RequireAdminRole("admin")).Post("/baskets/{symbol}/archive", httpx.Handler(s.adminArchive))
}

func (s *Service) adminList(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	lq, err := parseList(r)
	if err != nil {
		return err
	}
	lq.admin, lq.sort = true, "new"
	if r.URL.Query().Get("status") == "" {
		lq.status = "all"
	}
	switch lq.filter = r.URL.Query().Get("filter"); lq.filter {
	case "", "all", "reported", "hidden":
	default:
		return httpx.BadRequest("filter must be all, reported or hidden.")
	}
	rows, err := s.list(ctx, lq)
	if err != nil {
		return err
	}
	pg, err := s.page(ctx, rows, me.UserID, lq.limit, "new")
	if err != nil {
		return err
	}
	out := httpx.Page[AdminBasket]{Items: make([]AdminBasket, len(pg.Items)), NextCursor: pg.NextCursor}
	byID := map[uuid.UUID]basketRow{}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, b := range rows {
		byID[b.ID] = b
		ids = append(ids, b.ID)
	}
	reasons := map[uuid.UUID][]string{}
	rr, err := s.d.Pool.Query(ctx, `SELECT basket_id, reason FROM basket_reports WHERE basket_id = ANY($1) AND resolved_at IS NULL
 GROUP BY basket_id, reason ORDER BY basket_id, count(*) DESC`, ids)
	if err != nil {
		return err
	}
	for rr.Next() {
		var id uuid.UUID
		var reason string
		if err := rr.Scan(&id, &reason); err != nil {
			rr.Close()
			return err
		}
		reasons[id] = append(reasons[id], reason)
	}
	rr.Close()
	if err := rr.Err(); err != nil {
		return err
	}
	for i, it := range pg.Items {
		b := byID[it.ID]
		out.Items[i] = AdminBasket{Summary: it, OpenReports: b.OpenReports, Source: b.Source, Reasons: reasons[it.ID]}
		if out.Items[i].Reasons == nil {
			out.Items[i].Reasons = []string{}
		}
	}
	httpx.OK(w, out)
	return nil
}

// staffWrite loads and locks a basket, runs fn and writes the audit row, all
// in one transaction; it then publishes the change and returns the admin view.
func (s *Service) staffWrite(w http.ResponseWriter, r *http.Request, action string, fn func(tx pgx.Tx, b basketRow) (map[string]any, error)) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	var b basketRow
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		cur, err := basketByLock(ctx, tx, "symbol", chi.URLParam(r, "symbol"), true)
		if err != nil {
			return err
		}
		data, err := fn(tx, cur)
		if err != nil {
			return err
		}
		if data == nil {
			data = map[string]any{}
		}
		data["symbol"] = cur.Symbol
		if err := audit.Log(ctx, tx, me.UserID, action, "basket", cur.ID.String(), data); err != nil {
			return err
		}
		b, err = basketBy(ctx, tx, "id", cur.ID.String())
		return err
	})
	if err != nil {
		return err
	}
	s.markets.clear()
	s.oracle().Reload()
	s.publish(ctx, b)
	ptr := s.summary(b, me.UserID)
	if err := fillLines(ctx, s.d.Pool, []*Summary{&ptr}); err != nil {
		return err
	}
	httpx.OK(w, AdminBasket{Summary: ptr, OpenReports: b.OpenReports, Source: b.Source, Reasons: []string{}})
	return nil
}

// RenameRequest is PATCH /v1/admin/baskets/{symbol}.
type RenameRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

func (s *Service) adminRename(w http.ResponseWriter, r *http.Request) error {
	var req RenameRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.Name == nil && req.Description == nil {
		return httpx.BadRequest("Nothing to change.")
	}
	ctx := r.Context()
	return s.staffWrite(w, r, "baskets.rename", func(tx pgx.Tx, b basketRow) (map[string]any, error) {
		name, desc := b.Name, b.Description
		var err error
		if req.Name != nil {
			if name, err = s.cleanName(ctx, *req.Name); err != nil {
				return nil, err
			}
		}
		if req.Description != nil {
			if desc, err = s.cleanDescription(ctx, *req.Description); err != nil {
				return nil, err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE baskets SET name = $2, description = $3, updated_at = now() WHERE id = $1`, b.ID, name, desc); err != nil {
			return nil, err
		}
		if err := s.relist(ctx, tx, b, name, desc); err != nil {
			return nil, err
		}
		return map[string]any{"before": map[string]string{"name": b.Name, "description": b.Description},
			"after": map[string]string{"name": name, "description": desc}}, nil
	})
}

// relist writes a renamed basket's market and feed, keeping their spec and parts.
func (s *Service) relist(ctx context.Context, tx pgx.Tx, b basketRow, name, desc string) error {
	ms, err := exchange.Markets(ctx, tx)
	if err != nil {
		return err
	}
	lines, err := basketLinesOf(ctx, tx, b.ID)
	if err != nil {
		return err
	}
	var parts []string
	items := make([]bitem, len(lines))
	for i, l := range lines {
		parts = append(parts, strconv.Itoa(l.Quantity)+" "+l.Name)
		items[i] = bitem{symbol: l.Market, name: l.Name, feed: l.Feed, qty: l.Quantity}
	}
	for _, m := range ms {
		if m.Symbol != b.Symbol {
			continue
		}
		cm := m.CatalogMarket
		cm.Name = name
		cm.Description = "Holds " + strings.Join(parts, ", ") + "."
		if desc != "" {
			cm.Description = desc + "\n\n" + cm.Description
		}
		if len(cm.Description) > 2000 {
			cm.Description = cm.Description[:1997] + "…"
		}
		if err := exchange.UpsertCatalog(ctx, tx, []exchange.CatalogMarket{cm}); err != nil {
			return err
		}
	}
	return s.oracle().EnsureBasketFeed(ctx, tx, basketFeed(b.Symbol, name, items))
}

// HideRequest is POST /v1/admin/baskets/{symbol}/hide.
type HideRequest struct {
	Hidden bool `json:"hidden"`
}

func (s *Service) adminHide(w http.ResponseWriter, r *http.Request) error {
	var req HideRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	ctx := r.Context()
	action := "baskets.unhide"
	if req.Hidden {
		action = "baskets.hide"
	}
	return s.staffWrite(w, r, action, func(tx pgx.Tx, b basketRow) (map[string]any, error) {
		if _, err := tx.Exec(ctx, `UPDATE baskets SET hidden = $2, updated_at = now() WHERE id = $1`, b.ID, req.Hidden); err != nil {
			return nil, err
		}
		data := map[string]any{"hidden": req.Hidden}
		if req.Hidden && b.Status == StatusActive {
			to, err := s.windDown(ctx, tx, b)
			if err != nil {
				return nil, err
			}
			data["status"] = to
		}
		return data, nil
	})
}

// windDown moves an active basket to reduce_only while people hold
// positions, else archives it.
func (s *Service) windDown(ctx context.Context, tx pgx.Tx, b basketRow) (string, error) {
	u, err := exchange.UsageOf(ctx, tx, b.Symbol, time.Now())
	if err != nil {
		return "", err
	}
	to := StatusArchived
	if u.Holders > 0 {
		to = StatusReduceOnly
	}
	return to, s.setStatus(ctx, tx, b, to)
}

func (s *Service) adminArchive(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	return s.staffWrite(w, r, "baskets.archive", func(tx pgx.Tx, b basketRow) (map[string]any, error) {
		if b.Status != StatusActive {
			if b.Status == StatusArchived {
				return nil, httpx.Conflict(b.Symbol + " is archived already.")
			}
			return nil, httpx.Conflict(b.Symbol + " is winding down already. It's archived once every position is closed.")
		}
		to, err := s.windDown(ctx, tx, b)
		return map[string]any{"status": to}, err
	})
}

func (s *Service) adminResolve(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	return s.staffWrite(w, r, "baskets.reports.resolve", func(tx pgx.Tx, b basketRow) (map[string]any, error) {
		tag, err := tx.Exec(ctx, `UPDATE basket_reports SET resolved_at = now() WHERE basket_id = $1 AND resolved_at IS NULL`, b.ID)
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE baskets SET open_reports = 0, updated_at = now() WHERE id = $1`, b.ID); err != nil {
			return nil, err
		}
		return map[string]any{"resolved": tag.RowsAffected()}, nil
	})
}
