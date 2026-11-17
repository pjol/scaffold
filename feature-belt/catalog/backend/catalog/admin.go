package catalog

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
	"scaffoldapp/backend/internal/httpx"
)

func (s *Service) MountAdmin(r chi.Router) {
	r.Get("/catalog/status", httpx.Handler(s.adminStatus))
	r.Get("/catalog/runs", httpx.Handler(s.adminRuns))
	r.Get("/catalog/items", httpx.Handler(s.adminItems))
	r.Get("/catalog/items/{key}", httpx.Handler(s.adminItem))
	admin := r.With(auth.RequireAdminRole("admin"))
	admin.With(auth.RateLimit(10, time.Minute)).Post("/catalog/sync", httpx.Handler(s.adminSync))
	admin.Post("/catalog/items/{key}/retire", httpx.Handler(s.adminRetire))
	admin.Post("/catalog/items/{key}/restore", httpx.Handler(s.adminRestore))
}

// Status is GET /v1/admin/catalog/status.
type Status struct {
	Source           *SourceStatus `json:"source"`       // null: no source, so no syncs
	NightlyHour      *int          `json:"nightly_hour"` // UTC; null: no nightly sync
	NextSyncAt       *time.Time    `json:"next_sync_at"`
	MaxRetirePercent int           `json:"max_retire_percent"`
	Version          int64         `json:"version"` // stored
	Loaded           LoadedStatus  `json:"loaded"`  // this instance's copy
	Running          *SyncRun      `json:"running"`
	Last             *SyncRun      `json:"last"` // the latest finished run
}

// SourceStatus names the configured source and, when it says, where it reads.
type SourceStatus struct {
	Name   string `json:"name"`
	Detail string `json:"detail"`
}

// LoadedStatus is the index this instance serves.
type LoadedStatus struct {
	Version        int64     `json:"version"`
	At             time.Time `json:"at"`
	Items          int       `json:"items"`
	Active         int       `json:"active"`
	Retired        int       `json:"retired"`
	RetiredByStaff int       `json:"retired_by_staff"`
}

func (s *Service) adminStatus(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	x := s.idx.Load()
	out := Status{MaxRetirePercent: s.cfg.maxRetirePct, Loaded: LoadedStatus{Version: x.version, At: x.loadedAt, Items: len(x.recs),
		Active: x.nAct, Retired: len(x.recs) - x.nAct}}
	for _, rc := range x.recs {
		if rc.retiredBy == RetiredByStaff {
			out.Loaded.RetiredByStaff++
		}
	}
	if s.src != nil {
		out.Source = &SourceStatus{Name: s.src.Name()}
		if d, ok := s.src.(SourceInfo); ok {
			out.Source.Detail = d.Describe()
		}
	}
	if err := s.d.Pool.QueryRow(ctx, `SELECT version FROM catalog_state`).Scan(&out.Version); err != nil {
		return err
	}
	if run, err := scanRun(s.d.Pool.QueryRow(ctx, `SELECT `+runCols+` FROM catalog_sync_runs WHERE status = 'running' LIMIT 1`)); err == nil {
		out.Running = &run
	} else if !db.IsNoRows(err) {
		return err
	}
	if run, err := scanRun(s.d.Pool.QueryRow(ctx, `SELECT `+runCols+` FROM catalog_sync_runs WHERE status <> 'running'
 ORDER BY started_at DESC, id DESC LIMIT 1`)); err == nil {
		out.Last = &run
	} else if !db.IsNoRows(err) {
		return err
	}
	if h := s.cfg.syncHour; h >= 0 {
		out.NightlyHour = &h
		if s.src != nil {
			next, err := s.nextNightly(ctx, time.Now().UTC())
			if err != nil {
				return err
			}
			out.NextSyncAt = &next
		}
	}
	httpx.OK(w, out)
	return nil
}

// nextNightly is when the nightly sync next starts: today's hour if it hasn't
// run today (a past hour means within minutes), else tomorrow's.
func (s *Service) nextNightly(ctx context.Context, now time.Time) (time.Time, error) {
	due := time.Date(now.Year(), now.Month(), now.Day(), s.cfg.syncHour, 0, 0, 0, time.UTC)
	var ran bool
	if err := s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM catalog_sync_runs WHERE trigger = 'schedule' AND started_at >= $1)`, due).Scan(&ran); err != nil {
		return due, err
	}
	if ran {
		return due.AddDate(0, 0, 1), nil
	}
	return due, nil
}

type runCursor struct {
	T time.Time `json:"t"`
	ID uuid.UUID `json:"id"`
}

func (s *Service) adminRuns(w http.ResponseWriter, r *http.Request) error {
	pg := httpx.ParsePage(r)
	var c runCursor
	if err := httpx.DecodeCursor(pg.Cursor, &c); err != nil {
		return err
	}
	args := []any{pg.Limit + 1}
	where := ""
	if pg.Cursor != "" {
		where = `WHERE (started_at, id) < ($2, $3)`
		args = append(args, c.T, c.ID)
	}
	rows, err := s.d.Pool.Query(r.Context(), `SELECT `+runCols+` FROM catalog_sync_runs `+where+` ORDER BY started_at DESC, id DESC LIMIT $1`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	var runs []SyncRun
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return err
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(runs, pg.Limit, func(run SyncRun) any { return runCursor{T: run.StartedAt, ID: run.ID} }))
	return nil
}

// adminSync starts a sync now. force skips the safety valve (after a refused
// run, when staff know the source really shrank).
func (s *Service) adminSync(w http.ResponseWriter, r *http.Request) error {
	if s.src == nil {
		return errNoSource
	}
	var body struct {
		Force bool `json:"force"`
	}
	if err := httpx.Decode(r, &body); err != nil {
		return err
	}
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	var run SyncRun
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var err error
		if run, err = s.startRun(ctx, tx, TriggerStaff, s.src.Name(), body.Force); err != nil {
			return err
		}
		return audit.Log(ctx, tx, me.UserID, "catalog.sync", "catalog_sync_run", run.ID.String(), map[string]any{"source": run.Source, "force": body.Force})
	})
	if err != nil {
		return err
	}
	s.launch(ctx, run, s.src, body.Force)
	httpx.JSON(w, http.StatusAccepted, run)
	return nil
}

type itemCursor struct {
	N string `json:"n"`
	K string `json:"k"`
}

// adminItems lists stored items (the database, not this instance's copy) by
// name: q matches the name, key or an alias; status is all, active or retired.
func (s *Service) adminItems(w http.ResponseWriter, r *http.Request) error {
	pg := httpx.ParsePage(r)
	var c itemCursor
	if err := httpx.DecodeCursor(pg.Cursor, &c); err != nil {
		return err
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	conds, args := []string{"true"}, []any{}
	arg := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	if q != "" {
		p := arg("%" + escapeLike(q) + "%")
		conds = append(conds, "(name ILIKE "+p+" OR key ILIKE "+p+" OR array_to_string(aliases, ' ') ILIKE "+p+")")
	}
	switch r.URL.Query().Get("status") {
	case "", "all":
	case "active":
		conds = append(conds, "retired_at IS NULL")
	case "retired":
		conds = append(conds, "retired_at IS NOT NULL")
	default:
		return httpx.BadRequest("status must be all, active or retired.")
	}
	if pg.Cursor != "" {
		conds = append(conds, "(lower(name), key) > ("+arg(c.N)+", "+arg(c.K)+")")
	}
	lim := arg(pg.Limit + 1)
	rows, err := s.d.Pool.Query(r.Context(), `SELECT `+itemCols+` FROM catalog_items WHERE `+strings.Join(conds, " AND ")+`
 ORDER BY lower(name), key LIMIT `+lim, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	var items []Item
	for rows.Next() {
		rc, err := scanRec(rows)
		if err != nil {
			return err
		}
		items = append(items, recItem(rc))
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(items, pg.Limit, func(it Item) any { return itemCursor{N: strings.ToLower(it.Name), K: it.Key} }))
	return nil
}

func (s *Service) adminItem(w http.ResponseWriter, r *http.Request) error {
	rc, err := scanRec(s.d.Pool.QueryRow(r.Context(), `SELECT `+itemCols+` FROM catalog_items WHERE key = $1`, chi.URLParam(r, "key")))
	if db.IsNoRows(err) {
		return httpx.NotFound("No item with that key.")
	}
	if err != nil {
		return err
	}
	httpx.OK(w, recItem(rc))
	return nil
}

// adminRetire retires an item by hand: it stays retired (and out of search)
// until staff restore it, whatever the source says. Audited.
func (s *Service) adminRetire(w http.ResponseWriter, r *http.Request) error {
	return s.setRetired(w, r, true)
}

// adminRestore makes a retired item active again. One the source no longer
// lists is retired again by the next sync. Audited.
func (s *Service) adminRestore(w http.ResponseWriter, r *http.Request) error {
	return s.setRetired(w, r, false)
}

func (s *Service) setRetired(w http.ResponseWriter, r *http.Request, retire bool) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	key := chi.URLParam(r, "key")
	var out rec
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		cur, err := scanRec(tx.QueryRow(ctx, `SELECT `+itemCols+` FROM catalog_items WHERE key = $1 FOR UPDATE`, key))
		if db.IsNoRows(err) {
			return httpx.NotFound("No item with that key.")
		}
		if err != nil {
			return err
		}
		out = cur
		if (retire && cur.retiredBy == RetiredByStaff) || (!retire && cur.active()) {
			return nil // already so
		}
		stmt, action := `UPDATE catalog_items SET retired_by = NULL, retired_at = NULL, updated_at = now() WHERE key = $1 RETURNING `+itemCols, "catalog.item.restore"
		if retire {
			stmt, action = `UPDATE catalog_items SET retired_by = 'staff', retired_at = coalesce(retired_at, now()), updated_at = now() WHERE key = $1 RETURNING `+itemCols, "catalog.item.retire"
		}
		if out, err = scanRec(tx.QueryRow(ctx, stmt, key)); err != nil {
			return err
		}
		if err := bumpVersion(ctx, tx); err != nil {
			return err
		}
		was := cur.retiredBy
		if was == "" {
			was = "active"
		}
		return audit.Log(ctx, tx, me.UserID, action, "catalog_item", key, map[string]any{"name": cur.name, "was": was})
	})
	if err != nil {
		return err
	}
	s.reloadSoon()
	httpx.OK(w, recItem(out))
	return nil
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
