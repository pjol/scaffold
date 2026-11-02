package crm

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/httpx"
)

// The CRM's routes serve connected records too: org routes take ?source=
// (app, all, or a connection id) and record routes take an xref
// (<connection id>~<platform id>) where the app's own records have a UUID.
// Each wrapper below resolves the Source once and otherwise hands the
// request to the app's own handler unchanged.

type handlerFn = func(http.ResponseWriter, *http.Request) error

func zoneOf(r *http.Request) *time.Location {
	if loc, err := time.LoadLocation(r.URL.Query().Get("tz")); err == nil && r.URL.Query().Get("tz") != "" {
		return loc
	}
	return time.UTC
}

// orgSource resolves ?source= on an org route: nil for the app's own records.
func (s *Service) orgSource(r *http.Request) (Source, string, error) {
	ctx := r.Context()
	orgID := orgParam(r)
	id, _, err := s.sourceParam(ctx, r, orgID)
	if err != nil {
		return nil, "", err
	}
	if id == SourceApp || id == SourceAll {
		return nil, id, nil
	}
	src, err := s.source(ctx, me(r), orgID, id)
	return src, id, err
}

// recordSource resolves an xref route parameter: nil, "" for a UUID (the
// app's own record).
func (s *Service) recordSource(r *http.Request, param string) (Source, string, error) {
	raw := chi.URLParam(r, param)
	if _, err := uuid.Parse(raw); err == nil {
		return nil, "", nil
	}
	connID, pid, ok := parseXRef(raw)
	if !ok {
		return nil, "", httpx.NotFound("Not found.")
	}
	src, err := s.sourceForRecord(r.Context(), me(r), connID)
	return src, pid, err
}

// ---- org routes ---------------------------------------------------------------

func (s *Service) xListRecords(o objectType, native handlerFn) handlerFn {
	return func(w http.ResponseWriter, r *http.Request) error {
		src, id, err := s.orgSource(r)
		if err != nil {
			return err
		}
		if id == SourceAll {
			return s.mergedList(w, r, o)
		}
		if src == nil {
			return native(w, r)
		}
		spec, err := parseListSpec(r.URL.Query().Get)
		if err != nil {
			return err
		}
		page, err := src.List(r.Context(), o, spec)
		if err != nil {
			return err
		}
		httpx.OK(w, page)
		return nil
	}
}

func (s *Service) xCreateRecord(o objectType, native handlerFn) handlerFn {
	return func(w http.ResponseWriter, r *http.Request) error {
		src, id, err := s.orgSource(r)
		if err != nil {
			return err
		}
		if id == SourceAll {
			return httpx.BadRequest("Pick where the new " + o.label() + " goes.")
		}
		if src == nil {
			return native(w, r)
		}
		in, err := decodeFields(r)
		if err != nil {
			return err
		}
		delete(in, "allow_duplicate")
		rec, err := src.Create(r.Context(), o, in)
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusCreated, rec)
		return nil
	}
}

func (s *Service) xBoard(native handlerFn) handlerFn {
	return func(w http.ResponseWriter, r *http.Request) error {
		src, id, err := s.orgSource(r)
		if err != nil {
			return err
		}
		if id == SourceAll {
			return httpx.BadRequest("A board shows one source's pipeline. Pick a source.")
		}
		if src == nil {
			return native(w, r)
		}
		spec, err := parseListSpec(r.URL.Query().Get)
		if err != nil {
			return err
		}
		b, err := src.Board(r.Context(), r.URL.Query().Get("pipeline_id"), spec)
		if err != nil {
			return err
		}
		httpx.OK(w, b)
		return nil
	}
}

func (s *Service) xDashboard(native handlerFn) handlerFn {
	return func(w http.ResponseWriter, r *http.Request) error {
		src, id, err := s.orgSource(r)
		if err != nil {
			return err
		}
		if id == SourceAll {
			return httpx.BadRequest("The dashboard shows one source at a time. Pick a source.")
		}
		if src == nil {
			return native(w, r)
		}
		d, err := src.Dashboard(r.Context(), r.URL.Query().Get("pipeline_id"), zoneOf(r))
		if err != nil {
			return err
		}
		httpx.OK(w, d)
		return nil
	}
}

func (s *Service) xProperties(native handlerFn) handlerFn {
	return func(w http.ResponseWriter, r *http.Request) error {
		src, _, err := s.orgSource(r)
		if err != nil {
			return err
		}
		if src == nil {
			return native(w, r)
		}
		ctx := r.Context()
		if v := r.URL.Query().Get("object"); v != "" {
			o, ok := parseObject(v)
			if !ok {
				return httpx.BadRequest("object is company, contact or deal.")
			}
			list, err := src.Properties(ctx, o)
			if err != nil {
				return err
			}
			httpx.OK(w, map[string]any{"items": list})
			return nil
		}
		out := map[string][]Property{}
		for _, o := range objectTypes {
			list, err := src.Properties(ctx, o)
			if err != nil {
				return err
			}
			out[string(o)] = list
		}
		httpx.OK(w, out)
		return nil
	}
}

func (s *Service) xPipelines(native handlerFn) handlerFn {
	return func(w http.ResponseWriter, r *http.Request) error {
		src, _, err := s.orgSource(r)
		if err != nil {
			return err
		}
		if src == nil {
			return native(w, r)
		}
		list, err := src.Pipelines(r.Context())
		if err != nil {
			return err
		}
		httpx.OK(w, map[string]any{"items": list})
		return nil
	}
}

func (s *Service) xTasks(native handlerFn) handlerFn {
	return func(w http.ResponseWriter, r *http.Request) error {
		src, id, err := s.orgSource(r)
		if err != nil {
			return err
		}
		if src == nil || id == SourceAll {
			return native(w, r)
		}
		q := r.URL.Query()
		limit, _ := strconv.Atoi(q.Get("limit"))
		page, err := src.Tasks(r.Context(), q.Get("bucket"), zoneOf(r), q.Get("cursor"), limit)
		if err != nil {
			return err
		}
		httpx.OK(w, page)
		return nil
	}
}

func (s *Service) xTaskSummary(native handlerFn) handlerFn {
	return func(w http.ResponseWriter, r *http.Request) error {
		src, id, err := s.orgSource(r)
		if err != nil {
			return err
		}
		if src == nil || id == SourceAll {
			return native(w, r)
		}
		sum, err := src.TaskSummary(r.Context(), zoneOf(r))
		if err != nil {
			return err
		}
		httpx.OK(w, sum)
		return nil
	}
}

// xSearch: ?source=<connection id> searches that account; ?source=all adds
// every connected account (time-boxed) to the app's own hits.
func (s *Service) xSearch(native handlerFn) handlerFn {
	return func(w http.ResponseWriter, r *http.Request) error {
		src, id, err := s.orgSource(r)
		if err != nil {
			return err
		}
		ctx := r.Context()
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		types := parseTypes(r.URL.Query().Get("types"))
		limit := httpx.ParsePage(r).Limit
		if src != nil {
			hits, err := src.Search(ctx, q, types, limit)
			if err != nil {
				return err
			}
			httpx.OK(w, map[string]any{"items": hits})
			return nil
		}
		if id != SourceAll {
			return native(w, r)
		}
		orgID := orgParam(r)
		st, err := s.sourceSettings(ctx, orgID)
		if err != nil {
			return err
		}
		items := []any{}
		if st.allowsApp() {
			hits, err := s.search(ctx, me(r), []uuid.UUID{orgID}, q, types, limit)
			if err != nil {
				return err
			}
			for _, h := range hits {
				items = append(items, h)
			}
		}
		for _, h := range s.connectedSearch(ctx, me(r), orgID, st, q, types, limit) {
			items = append(items, h)
		}
		httpx.OK(w, map[string]any{"items": items})
		return nil
	}
}

// connectedSearch searches each connected account in parallel, each given
// a short time; slow or failing accounts are left out.
func (s *Service) connectedSearch(ctx context.Context, userID, orgID uuid.UUID, st sourceSettings, q string, types []objectType, limit int) []XSearchHit {
	conns, err := s.connectedFor(ctx, userID, orgID, st)
	if err != nil || len(conns) == 0 {
		return nil
	}
	writable := s.writable(ctx, userID, orgID, st)
	ctx, cancel := context.WithTimeout(ctx, connectedSearchTimeout)
	defer cancel()
	var mu sync.Mutex
	var wg sync.WaitGroup
	out := []XSearchHit{}
	for _, c := range conns {
		if c.Status != "" && c.Status != "active" {
			continue
		}
		wg.Add(1)
		go func(c app.Connection) {
			defer wg.Done()
			src, err := s.openSource(ctx, userID, orgID, c, writable[c.ID])
			if err != nil {
				return
			}
			hits, err := src.Search(ctx, q, types, limit)
			if err != nil {
				return
			}
			mu.Lock()
			out = append(out, hits...)
			mu.Unlock()
		}(c)
	}
	wg.Wait()
	return out
}

const connectedSearchTimeout = 2500 * time.Millisecond

// ---- record routes ----------------------------------------------------------------

func (s *Service) xGetRecord(o objectType, native handlerFn) handlerFn {
	return func(w http.ResponseWriter, r *http.Request) error {
		src, pid, err := s.recordSource(r, "id")
		if err != nil {
			return err
		}
		if src == nil {
			return native(w, r)
		}
		d, err := src.Get(r.Context(), o, pid)
		if err != nil {
			return err
		}
		httpx.OK(w, d)
		return nil
	}
}

func (s *Service) xUpdateRecord(o objectType, native handlerFn) handlerFn {
	return func(w http.ResponseWriter, r *http.Request) error {
		src, pid, err := s.recordSource(r, "id")
		if err != nil {
			return err
		}
		if src == nil {
			return native(w, r)
		}
		in, err := decodeFields(r)
		if err != nil {
			return err
		}
		rec, err := src.Update(r.Context(), o, pid, in)
		if err != nil {
			return err
		}
		httpx.OK(w, rec)
		return nil
	}
}

func (s *Service) xTimeline(o objectType, native handlerFn) handlerFn {
	return func(w http.ResponseWriter, r *http.Request) error {
		src, pid, err := s.recordSource(r, "id")
		if err != nil {
			return err
		}
		if src == nil {
			return native(w, r)
		}
		q := r.URL.Query()
		limit, _ := strconv.Atoi(q.Get("limit"))
		page, err := src.Timeline(r.Context(), o, pid, q.Get("filter"), q.Get("cursor"), limit)
		if err != nil {
			return err
		}
		httpx.OK(w, page)
		return nil
	}
}

func (s *Service) xCreateActivity(o objectType, native handlerFn) handlerFn {
	return func(w http.ResponseWriter, r *http.Request) error {
		src, pid, err := s.recordSource(r, "id")
		if err != nil {
			return err
		}
		if src == nil {
			return native(w, r)
		}
		var req xActivityReq
		if err := httpx.Decode(r, &req); err != nil {
			return err
		}
		a, err := src.LogActivity(r.Context(), o, pid, req)
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusCreated, a)
		return nil
	}
}

func (s *Service) xMoveStage(native handlerFn) handlerFn {
	return func(w http.ResponseWriter, r *http.Request) error {
		src, pid, err := s.recordSource(r, "id")
		if err != nil {
			return err
		}
		if src == nil {
			return native(w, r)
		}
		var req struct {
			StageID string `json:"stage_id"`
		}
		if err := httpx.Decode(r, &req); err != nil {
			return err
		}
		if req.StageID == "" {
			return httpx.BadRequest("stage_id is the stage to move to.")
		}
		d, err := src.MoveStage(r.Context(), pid, req.StageID)
		if err != nil {
			return err
		}
		httpx.OK(w, d)
		return nil
	}
}

// xStageHistory: platforms keep their own stage history; connected deals
// answer with none (the record page hides the card).
func (s *Service) xStageHistory(native handlerFn) handlerFn {
	return func(w http.ResponseWriter, r *http.Request) error {
		src, _, err := s.recordSource(r, "id")
		if err != nil {
			return err
		}
		if src == nil {
			return native(w, r)
		}
		httpx.OK(w, map[string]any{"stays": []any{}, "totals": []any{}})
		return nil
	}
}

func (s *Service) xUpdateActivity(native handlerFn) handlerFn {
	return func(w http.ResponseWriter, r *http.Request) error {
		src, pid, err := s.recordSource(r, "activityID")
		if err != nil {
			return err
		}
		if src == nil {
			return native(w, r)
		}
		var req struct {
			Done *bool `json:"done"`
		}
		if err := httpx.Decode(r, &req); err != nil {
			return err
		}
		if req.Done == nil {
			return httpx.BadRequest("Edit " + src.Connection().Provider + " activities on the platform; from here you can mark tasks done.")
		}
		a, err := src.SetTaskDone(r.Context(), pid, *req.Done)
		if err != nil {
			return err
		}
		httpx.OK(w, a)
		return nil
	}
}

// xNotHere answers record routes connected records don't take here.
func (s *Service) xNotHere(param, what string, native handlerFn) handlerFn {
	return func(w http.ResponseWriter, r *http.Request) error {
		raw := chi.URLParam(r, param)
		if _, err := uuid.Parse(raw); err == nil {
			return native(w, r)
		}
		if _, _, ok := parseXRef(raw); ok {
			return httpx.NewError(http.StatusBadRequest, "not_on_connected", what)
		}
		return native(w, r)
	}
}

// ---- owners -------------------------------------------------------------------------

// sourceOwners: GET /v1/crm/orgs/{orgID}/sources/{sourceID}/owners — the
// platform's owners, for owner pickers and task assignees.
func (s *Service) sourceOwners(w http.ResponseWriter, r *http.Request) error {
	src, err := s.source(r.Context(), me(r), orgParam(r), chi.URLParam(r, "sourceID"))
	if err != nil {
		return err
	}
	list, err := src.Owners(r.Context())
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": list})
	return nil
}

// ---- All: the app's records and every connected account in one list ---------------------

type mergeCursor struct {
	S map[string]mergeSource `json:"s"`
}

type mergeSource struct {
	C    string `json:"c,omitempty"` // the source's own cursor for the page in use
	K    int    `json:"k,omitempty"` // rows of that page already shown
	Done bool   `json:"d,omitempty"`
}

// SkippedSource is a source an All list couldn't include, and why.
type SkippedSource struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// mergedPage is an All list page: rows from every source in one order.
type mergedPage struct {
	Items      []any           `json:"items"`
	NextCursor *string         `json:"next_cursor"`
	Total      *int            `json:"total"`
	Skipped    []SkippedSource `json:"skipped_sources"`
}

// Sort keys an All list can order every source by.
var mergeSortKeys = map[string]bool{"name": true, "created_at": true, "updated_at": true, "last_activity_at": true, "amount": true, "close_date": true}

type mergeRow struct {
	src string
	key string // comparable: lower-cased text, RFC 3339 UTC, or a zero-padded number
	nul bool
	v   any
}

func mergeKey(v any, key string) (string, bool) {
	var title string
	var created, updated time.Time
	var last *time.Time
	var amount *int64
	var closeDate *string
	switch t := v.(type) {
	case recordish:
		b := t.base()
		title, created, updated, last = b.Title, b.CreatedAt, b.UpdatedAt, b.LastActivityAt
		if d, ok := t.(*Deal); ok {
			amount, closeDate = d.Amount, d.CloseDate
		}
	case xrecordish:
		b := t.xbase()
		title, created, updated, last = b.Title, b.CreatedAt, b.UpdatedAt, b.LastActivityAt
		if d, ok := t.(*XDeal); ok {
			amount, closeDate = d.Amount, d.CloseDate
		}
	}
	ts := func(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000000000Z") }
	switch key {
	case "name":
		return strings.ToLower(title), title == ""
	case "updated_at":
		return ts(updated), false
	case "last_activity_at":
		if last == nil {
			return "", true
		}
		return ts(*last), false
	case "amount":
		if amount == nil {
			return "", true
		}
		// Offset so negative amounts order below positive ones as text.
		return fmt.Sprintf("%020d", *amount+(1<<62)), false
	case "close_date":
		if closeDate == nil {
			return "", true
		}
		return *closeDate, false
	}
	return ts(created), false
}

// mergedList serves ?source=all: each source's page at its own cursor,
// merged in one order; the cursor remembers how far into each page the
// merge went, so nothing repeats or goes missing. Sources that can't take
// the list's filters, or don't answer, are listed in skipped_sources.
func (s *Service) mergedList(w http.ResponseWriter, r *http.Request, o objectType) error {
	ctx := r.Context()
	orgID, userID := orgParam(r), me(r)
	spec, err := parseListSpec(r.URL.Query().Get)
	if err != nil {
		return err
	}
	if !mergeSortKeys[spec.SortKey] {
		spec.SortKey, spec.SortDir = "created_at", "desc"
	}
	if spec.SortDir == "" {
		spec.SortDir = "desc"
		if spec.SortKey == "name" {
			spec.SortDir = "asc"
		}
	}
	if o != objDeal && (spec.SortKey == "amount" || spec.SortKey == "close_date") {
		spec.SortKey, spec.SortDir = "created_at", "desc"
	}
	st, err := s.sourceSettings(ctx, orgID)
	if err != nil {
		return err
	}
	cur := mergeCursor{S: map[string]mergeSource{}}
	first := spec.Cursor == ""
	if !first {
		if err := httpx.DecodeCursor(spec.Cursor, &cur); err != nil || cur.S == nil {
			return httpx.BadRequest("That page cursor isn't valid.")
		}
	}
	type fetched struct {
		id, label string
		items     []any
		next      *string
		total     *int
		err       error
	}
	var jobs []func() fetched
	if ms, seen := cur.S[SourceApp]; st.allowsApp() && (first || (seen && !ms.Done)) {
		jobs = append(jobs, func() fetched {
			f := fetched{id: SourceApp, label: appLabel}
			props, err := s.loadProperties(ctx, s.d.Pool, orgID, o)
			if err != nil {
				f.err = err
				return f
			}
			sp := spec
			sp.Cursor = ms.C
			page, err := s.queryRecords(ctx, s.d.Pool, userID, orgID, o, props, sp, true)
			if err != nil {
				f.err = err
				return f
			}
			for _, it := range page.Items {
				f.items = append(f.items, it)
			}
			f.next, f.total = page.NextCursor, page.Total
			return f
		})
	}
	conns, err := s.connectedFor(ctx, userID, orgID, st)
	if err != nil {
		return err
	}
	writable := s.writable(ctx, userID, orgID, st)
	skipped := []SkippedSource{}
	for _, c := range conns {
		c := c
		p, _ := providerByID(c.Provider)
		label := p.Label
		if c.Account != "" {
			label += " · " + c.Account
		}
		ms, seen := cur.S[c.ID]
		if !first && (!seen || ms.Done) {
			continue
		}
		if c.Status != "" && c.Status != "active" {
			if first {
				skipped = append(skipped, SkippedSource{ID: c.ID, Label: label, Code: app.ConnectionNeedsAuth, Message: label + " needs to be reconnected."})
			}
			continue
		}
		jobs = append(jobs, func() fetched {
			f := fetched{id: c.ID, label: label}
			src, err := s.openSource(ctx, userID, orgID, c, writable[c.ID])
			if err != nil {
				f.err = err
				return f
			}
			sp := spec
			sp.Cursor = ms.C
			page, err := src.List(ctx, o, sp)
			if err != nil {
				f.err = err
				return f
			}
			for _, it := range page.Items {
				f.items = append(f.items, it)
			}
			f.next, f.total = page.NextCursor, page.Total
			return f
		})
	}
	results := make([]fetched, len(jobs))
	var wg sync.WaitGroup
	for i, job := range jobs {
		wg.Add(1)
		go func(i int, job func() fetched) {
			defer wg.Done()
			results[i] = job()
		}(i, job)
	}
	wg.Wait()

	limit := spec.Limit
	if limit <= 0 {
		limit = 50
	}
	var rows []mergeRow
	pages := map[string]fetched{}
	total, haveTotal := 0, false
	for _, f := range results {
		if f.err != nil {
			if f.id == SourceApp {
				return f.err
			}
			code, msg := "error", f.label+" didn't answer."
			if he, ok := passConnErr(f.err).(*httpx.Error); ok {
				code, msg = he.Code, he.Message
			}
			skipped = append(skipped, SkippedSource{ID: f.id, Label: f.label, Code: code, Message: msg})
			continue
		}
		pages[f.id] = f
		if f.total != nil {
			total += *f.total
			haveTotal = true
		}
		skip := cur.S[f.id].K
		for i, it := range f.items {
			if i < skip {
				continue
			}
			k, nul := mergeKey(it, spec.SortKey)
			rows = append(rows, mergeRow{src: f.id, key: k, nul: nul, v: it})
		}
	}
	asc := spec.SortDir == "asc"
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.nul != b.nul {
			return !a.nul
		}
		if a.key == b.key {
			return false
		}
		if asc {
			return a.key < b.key
		}
		return a.key > b.key
	})
	// Take rows in merged order, but never past a source's page end while
	// that source has more pages: rows after it could belong earlier.
	used := map[string]int{}
	out := &mergedPage{Items: []any{}, Skipped: skipped}
	for _, row := range rows {
		if len(out.Items) >= limit {
			break
		}
		out.Items = append(out.Items, row.v)
		used[row.src]++
		f := pages[row.src]
		if cur.S[row.src].K+used[row.src] == len(f.items) && f.next != nil {
			break
		}
	}
	next := mergeCursor{S: map[string]mergeSource{}}
	more := false
	for id, f := range pages {
		ms := cur.S[id]
		k := ms.K + used[id]
		switch {
		case k < len(f.items):
			next.S[id] = mergeSource{C: ms.C, K: k}
			more = true
		case f.next != nil:
			next.S[id] = mergeSource{C: *f.next}
			more = true
		default:
			next.S[id] = mergeSource{Done: true}
		}
	}
	if more {
		out.NextCursor = httpx.EncodeCursor(next)
	}
	if first && haveTotal {
		out.Total = &total
	}
	httpx.OK(w, out)
	return nil
}
