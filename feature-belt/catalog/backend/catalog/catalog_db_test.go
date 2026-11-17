package catalog

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/db"
)

// Database tests: a throwaway database on TEST_DATABASE_URL (a role that may
// CREATE DATABASE) with every migration applied.

var (
	dbOnce sync.Once
	dbPool *pgxpool.Pool
	dbErr  error
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		t.Skip("TEST_DATABASE_URL not set: a Postgres URL whose role can create databases")
	}
	dbOnce.Do(func() {
		ctx := context.Background()
		admin, err := pgx.Connect(ctx, base)
		if err != nil {
			dbErr = err
			return
		}
		defer admin.Close(ctx)
		name := fmt.Sprintf("catalog_test_%d", time.Now().UnixNano())
		if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
			dbErr = err
			return
		}
		cfg, err := pgxpool.ParseConfig(base)
		if err != nil {
			dbErr = err
			return
		}
		cfg.ConnConfig.Database = name
		if dbPool, err = pgxpool.NewWithConfig(ctx, cfg); err != nil {
			dbErr = err
			return
		}
		dbErr = db.Migrate(ctx, dbPool, false)
	})
	if dbErr != nil {
		t.Fatal(dbErr)
	}
	return dbPool
}

// listSource serves a fixed list.
type listSource struct{ items []Item }

func (l *listSource) Name() string { return "test" }
func (l *listSource) Fetch(_ context.Context, emit func(Item) error) error {
	for _, it := range l.items {
		if err := emit(it); err != nil {
			return err
		}
	}
	return nil
}

func run(t *testing.T, s *Service, src Source, force bool) SyncRun {
	t.Helper()
	ctx := context.Background()
	r, err := s.startRun(ctx, s.d.Pool, TriggerApp, src.Name(), force)
	if err != nil {
		t.Fatal(err)
	}
	if r, err = s.execute(ctx, r, src, force); err != nil {
		t.Fatalf("sync: %v (%+v)", err, r)
	}
	return r
}

func storedItem(t *testing.T, pool *pgxpool.Pool, key string) rec {
	t.Helper()
	r, err := scanRec(pool.QueryRow(context.Background(), `SELECT `+itemCols+` FROM catalog_items WHERE key = $1`, key))
	if err != nil {
		t.Fatalf("%s: %v", key, err)
	}
	return r
}

func TestSyncDatabase(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := New(&app.Deps{Pool: pool, Cfg: &config.Config{Env: "development"}})
	if err := s.SeedDev(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.SeedDev(ctx, nil); err != nil { // idempotent
		t.Fatal(err)
	}
	if !s.Loaded() || len(s.idx.Load().recs) != 50 || s.Version() != 1 {
		t.Fatalf("seeded: loaded %v, %d items, version %d", s.Loaded(), len(s.idx.Load().recs), s.Version())
	}
	if r := s.Lookup(Query{Name: "Ferrum"}); !r.OK || r.Item.Key != "element-26" || r.Item.Attrs["atomic_weight"] != "55.845" {
		t.Fatalf("lookup after seed: %+v", r)
	}
	if it, ok := s.Item("element-79"); !ok || it.RefPrice == nil || it.RefPrice.String() != "4000" || it.Currency != "USD" {
		t.Fatalf("gold: %+v", it)
	}

	// the same items again: nothing changes (stored jsonb and numeric compare equal)
	var items []Item
	if err := DecodeItems(bytes.NewReader(devItems), func(it Item) error { items = append(items, it); return nil }, nil); err != nil {
		t.Fatal(err)
	}
	r := run(t, s, &listSource{items}, false)
	if r.Status != RunOK || r.Unchanged != 50 || r.Updated+r.Inserted+r.Retired != 0 || s.Version() != 1 {
		t.Fatalf("no-op sync: %+v version %d", r, s.Version())
	}

	// rename one, drop one, add one
	next := append([]Item{}, items[1:]...) // Hydrogen dropped
	next[0].Name = "Helium-4"              // renamed
	next = append(next, Item{Key: "element-118", Name: "Oganesson", Category: "Noble gas", Aliases: []string{"Og"}})
	r = run(t, s, &listSource{next}, false)
	if r.Status != RunOK || r.Inserted != 1 || r.Updated != 1 || r.Retired != 1 || s.Version() != 2 {
		t.Fatalf("change sync: %+v version %d", r, s.Version())
	}
	if h := storedItem(t, pool, "element-1"); h.retiredBy != RetiredBySource || h.retiredAt.IsZero() {
		t.Errorf("dropped item: %+v", h)
	}
	if r := s.Lookup(Query{Name: "Helium"}); !r.OK || r.Item.Name != "Helium-4" || r.Match != MatchAlias {
		t.Errorf("old name resolves as an alias: %+v", r)
	}

	// staff retire an item the source still lists; it stays retired through syncs
	var admin uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO users (auth_subject, email, admin_role) VALUES ('dev_catalog', 'catalog@example.com', 'admin') RETURNING id`).Scan(&admin); err != nil {
		t.Fatal(err)
	}
	staff := func(key string, retire bool) {
		t.Helper()
		rc := chi.NewRouteContext()
		rc.URLParams.Add("key", key)
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req = req.WithContext(context.WithValue(auth.WithIdentity(ctx, &auth.Identity{UserID: admin, AdminRole: "admin", Status: "active"}), chi.RouteCtxKey, rc))
		w := httptest.NewRecorder()
		if err := s.setRetired(w, req, retire); err != nil || w.Code != http.StatusOK {
			t.Fatalf("staff retire=%v %s: %v %d %s", retire, key, err, w.Code, w.Body.String())
		}
	}
	staff("element-26", true)
	if err := s.refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if it, _ := s.Item("element-26"); !it.Retired || it.RetiredBy != RetiredByStaff || s.Version() != 3 {
		t.Fatalf("staff retired: %+v version %d", it, s.Version())
	}
	var audits int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM admin_audit WHERE action = 'catalog.item.retire' AND target_id = 'element-26'`).Scan(&audits)
	if audits != 1 {
		t.Errorf("audit rows: %d", audits)
	}
	// Hydrogen is back in the source: restored; Iron stays retired
	r = run(t, s, &listSource{append(next, items[0])}, false)
	if r.Restored != 1 || r.Retired != 0 {
		t.Errorf("restore sync: %+v", r)
	}
	if h := storedItem(t, pool, "element-1"); !h.active() {
		t.Errorf("hydrogen not restored: %+v", h)
	}
	if fe := storedItem(t, pool, "element-26"); fe.retiredBy != RetiredByStaff {
		t.Errorf("staff retirement lost: %+v", fe)
	}
	staff("element-26", false)

	// the safety valve: 151 active items, a pull of 80 would retire 47%
	big := append([]Item{}, next...)
	big = append(big, items[0])
	for i := 0; i < 100; i++ {
		big = append(big, Item{Key: fmt.Sprintf("part-%03d", i), Name: fmt.Sprintf("Part %03d", i)})
	}
	if r = run(t, s, &listSource{big}, false); r.Status != RunOK || r.Inserted != 100 {
		t.Fatalf("grow: %+v", r)
	}
	before := s.Version()
	r = run(t, s, &listSource{big[:80]}, false)
	if r.Status != RunRefused || r.Retired != 71 || s.Version() != before {
		t.Fatalf("valve: %+v", r)
	}
	if n := storedCount(t, pool, `retired_at IS NULL`); n != 151 {
		t.Errorf("a refused sync changed rows: %d active", n)
	}
	if r = run(t, s, &listSource{big[:80]}, true); r.Status != RunOK || r.Retired != 71 || !r.Forced {
		t.Fatalf("forced: %+v", r)
	}
	if n := storedCount(t, pool, `true`); n != 151 {
		t.Errorf("rows deleted: %d left", n)
	}

	// one running sync at a time
	held, err := s.startRun(ctx, pool, TriggerApp, "test", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.startRun(ctx, pool, TriggerStaff, "test", false); err != errSyncRunning {
		t.Errorf("second run: %v", err)
	}
	// a run whose instance died (no heartbeat) is failed by the next claim
	if _, err := pool.Exec(ctx, `UPDATE catalog_sync_runs SET heartbeat_at = now() - interval '5 minutes' WHERE id = $1`, held.ID); err != nil {
		t.Fatal(err)
	}
	next2, err := s.startRun(ctx, pool, TriggerStaff, "test", false)
	if err != nil {
		t.Fatalf("stale run blocked a new one: %v", err)
	}
	var status string
	_ = pool.QueryRow(ctx, `SELECT status FROM catalog_sync_runs WHERE id = $1`, held.ID).Scan(&status)
	if status != RunFailed {
		t.Errorf("stale run status %q", status)
	}
	_ = s.finishRun(ctx, pool, SyncRun{ID: next2.ID, Status: RunFailed, Error: "test"})
}

func storedCount(t *testing.T, pool *pgxpool.Pool, where string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM catalog_items WHERE `+where).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
