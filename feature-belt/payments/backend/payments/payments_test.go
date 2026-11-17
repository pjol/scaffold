package payments

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/notify"
	"scaffoldapp/backend/internal/realtime"
)

func TestValidReturnPath(t *testing.T) {
	prefixes := []string{"/payments/", "/settings"}
	ok := map[string]string{
		"/payments/success":         "/payments/success",
		"/payments/cancel?plan=pro": "/payments/cancel?plan=pro",
		"/settings":                 "/settings",
		"/settings/billing":         "/settings/billing",
	}
	for in, want := range ok {
		got, err := validReturnPath("p", in, prefixes)
		if err != nil || got != want {
			t.Errorf("validReturnPath(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{
		"", "payments/success", "//evil.example/payments/x", "https://evil.example/payments/x",
		"/settingsx", "/paymentsx", "/payments/../admin", "/payments/\\evil", "/payments/x#frag", "/admin",
	} {
		if got, err := validReturnPath("p", in, prefixes); err == nil {
			t.Errorf("validReturnPath(%q) = %q; want an error", in, got)
		}
	}
}

func TestWithParam(t *testing.T) {
	if got := withParam("/payments/success", "checkout_id", "a b"); got != "/payments/success?checkout_id=a+b" {
		t.Errorf("got %q", got)
	}
	if got := withParam("/payments/success?x=1", "k", "v"); got != "/payments/success?x=1&k=v" {
		t.Errorf("got %q", got)
	}
}

func TestFeeAmount(t *testing.T) {
	cases := []struct {
		amount int64
		bps    int
		want   int64
	}{
		{10000, 250, 250}, {999, 250, 24}, {0, 250, 0}, {5000, 0, 0}, {1, 10000, 1}, {-5, 100, 0},
	}
	for _, c := range cases {
		if got := feeAmount(c.amount, c.bps); got != c.want {
			t.Errorf("feeAmount(%d, %d) = %d; want %d", c.amount, c.bps, got, c.want)
		}
	}
}

func TestAccessUntil(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	end := now.Add(10 * 24 * time.Hour)
	grace := 24 * time.Hour
	cases := []struct {
		name string
		sub  Subscription
		want time.Time
	}{
		{"active renews with grace", Subscription{Status: SubActive, CurrentPeriodEnd: end}, end.Add(grace)},
		{"past due keeps access while retried", Subscription{Status: SubPastDue, CurrentPeriodEnd: end}, end.Add(grace)},
		{"cancel at period end lapses exactly", Subscription{Status: SubActive, CurrentPeriodEnd: end, CancelAtPeriodEnd: true}, end},
		{"scheduled cancel inside the period", Subscription{Status: SubActive, CurrentPeriodEnd: end, CancelAt: end.Add(-time.Hour)}, end.Add(-time.Hour)},
		{"scheduled cancel after the period renews", Subscription{Status: SubActive, CurrentPeriodEnd: end, CancelAt: end.Add(72 * time.Hour)}, end.Add(grace)},
		{"canceled ends at ended_at", Subscription{Status: SubCanceled, EndedAt: now.Add(-time.Hour)}, now.Add(-time.Hour)},
		{"unpaid ends now", Subscription{Status: SubUnpaid, CurrentPeriodEnd: end}, now},
		{"incomplete has no access", Subscription{Status: SubIncomplete, CurrentPeriodEnd: end}, now},
	}
	for _, c := range cases {
		if got := accessUntil(&c.sub, grace, now); !got.Equal(c.want) {
			t.Errorf("%s: accessUntil = %s; want %s", c.name, got, c.want)
		}
	}
}

func TestParseSeedPrices(t *testing.T) {
	got, err := parseSeedPrices(" pro_monthly=price_123:pro, lifetime=price_456 ,")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != (seedEntry{"pro_monthly", "price_123", "pro"}) || got[1] != (seedEntry{"lifetime", "price_456", ""}) {
		t.Errorf("got %+v", got)
	}
	for _, bad := range []string{"pro", "Pro=price_1", "pro=", "pro=price_1:Bad-Key"} {
		if _, err := parseSeedPrices(bad); err == nil {
			t.Errorf("parseSeedPrices(%q): want an error", bad)
		}
	}
}

func TestOrderStatus(t *testing.T) {
	if orderStatus(1000, 0) != "paid" || orderStatus(1000, 400) != "partially_refunded" || orderStatus(1000, 1000) != "refunded" {
		t.Error("orderStatus thresholds wrong")
	}
}

func TestChangeCause(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	at := func(d time.Duration) *time.Time { v := now.Add(d); return &v }
	row := func(until *time.Time) rowState { return rowState{exists: true, until: until} }
	cases := []struct {
		name          string
		before, after rowState
		want          string
	}{
		{"new grant", rowState{}, row(at(time.Hour)), EntitlementGranted},
		{"new grant with no end", rowState{}, row(nil), EntitlementGranted},
		{"inserted already ended", rowState{}, row(at(-time.Hour)), ""},
		{"renewal", row(at(time.Hour)), row(at(48 * time.Hour)), EntitlementExtended},
		{"cancel at period end", row(at(48 * time.Hour)), row(at(time.Hour)), EntitlementShortened},
		{"same period re-synced", row(at(time.Hour)), row(at(time.Hour)), ""},
		{"ended", row(at(time.Hour)), row(at(-time.Minute)), EntitlementRevoked},
		{"ends exactly now", row(at(time.Hour)), row(at(0)), EntitlementRevoked},
		{"reactivated", row(at(-time.Hour)), row(at(time.Hour)), EntitlementGranted},
		{"still ended", row(at(-time.Hour)), row(at(-time.Minute)), ""},
		{"term becomes lifetime", row(at(time.Hour)), row(nil), EntitlementExtended},
		{"lifetime gets an end", row(nil), row(at(time.Hour)), EntitlementShortened},
	}
	for _, c := range cases {
		if got := changeCause(c.before, c.after, now); got != c.want {
			t.Errorf("%s: changeCause = %q; want %q", c.name, got, c.want)
		}
	}
}

// ---- database tests ------------------------------------------------------------
//
// A throwaway database on TEST_DATABASE_URL (a role that may CREATE DATABASE)
// with every migration applied. The provider is a stub that only names
// itself: these tests drive sync.go with provider objects directly.

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
		name := fmt.Sprintf("payments_test_%d", time.Now().UnixNano())
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

type stubGateway struct{ Gateway }

func (stubGateway) Name() string { return "test" }

// recorder keeps every announced change, by user; failFor makes the hook fail.
var rec = struct {
	sync.Mutex
	once    sync.Once
	changes map[uuid.UUID][]EntitlementChange
	failFor map[uuid.UUID]bool
	after   map[uuid.UUID]int // AfterCommit work that ran, by user
}{changes: map[uuid.UUID][]EntitlementChange{}, failFor: map[uuid.UUID]bool{}, after: map[uuid.UUID]int{}}

func testService(t *testing.T) *Service {
	t.Helper()
	pool := testPool(t)
	rec.once.Do(func() {
		OnEntitlementChange(func(ctx context.Context, tx pgx.Tx, ch EntitlementChange) error {
			rec.Lock()
			defer rec.Unlock()
			if rec.failFor[ch.UserID] {
				return errors.New("hook refused")
			}
			rec.changes[ch.UserID] = append(rec.changes[ch.UserID], ch)
			user := ch.UserID
			AfterCommit(ctx, "payments.test", func(context.Context) error {
				rec.Lock()
				defer rec.Unlock()
				rec.after[user]++
				return nil
			})
			return nil
		})
	})
	return &Service{d: &app.Deps{Pool: pool, Notify: notify.Realtime{Broker: realtime.NewMemory()}},
		gw: stubGateway{}, cfg: settings{renewalGrace: 24 * time.Hour}}
}

// afterRan waits up to 5 seconds for user's AfterCommit work to have run n times.
func afterRan(user uuid.UUID, n int) bool {
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		rec.Lock()
		got := rec.after[user]
		rec.Unlock()
		if got >= n {
			return got == n
		}
	}
	return false
}

// take returns and forgets the changes announced for user.
func take(user uuid.UUID) []EntitlementChange {
	rec.Lock()
	defer rec.Unlock()
	out := rec.changes[user]
	delete(rec.changes, user)
	return out
}

func testUser(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	sub := "test_" + uuid.NewString()
	if err := pool.QueryRow(context.Background(), `INSERT INTO users (auth_subject, email) VALUES ($1, $1 || '@example.test')
		RETURNING id`, sub).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func testKey(prefix string) string { return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano()) }

// expectOne asserts exactly one change with the given cause and hold.
func expectOne(t *testing.T, user uuid.UUID, cause string, active bool, until *time.Time) EntitlementChange {
	t.Helper()
	got := take(user)
	if len(got) != 1 {
		t.Fatalf("want one %s change, got %+v", cause, got)
	}
	ch := got[0]
	if ch.Cause != cause || ch.Active != active || (until == nil) != (ch.Until == nil) || (until != nil && !until.Equal(*ch.Until)) {
		t.Fatalf("got %+v; want cause %s active %v until %v", ch, cause, active, until)
	}
	return ch
}

func TestEntitlementChangesDB(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	pool := s.d.Pool
	user := testUser(t, pool)
	key, plan := testKey("ent"), testKey("plan")
	if _, err := pool.Exec(ctx, `INSERT INTO payment_prices (key, provider, provider_price_id, kind, amount_minor, currency,
		billing_interval, interval_count, entitlement) VALUES ($1, 'test', $1, 'recurring', 500, 'usd', 'month', 1, $2)`, plan, key); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	sync1 := func(sub Subscription) {
		t.Helper()
		sub.ID, sub.CustomerID, sub.PriceID, sub.Quantity = "sub_"+plan, "cus_"+plan, plan, 1
		if err := s.inTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
			_, err := s.syncSubscription(ctx, tx, &sub, subHint{userID: &user})
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	at := func(d time.Duration) *time.Time { v := now.Add(d); return &v }
	grace := s.cfg.renewalGrace
	month, twoMonths := 30*24*time.Hour, 60*24*time.Hour

	sync1(Subscription{Status: SubActive, CurrentPeriodEnd: *at(month)})
	ch := expectOne(t, user, EntitlementGranted, true, at(month+grace))
	if ch.Source != "subscription" || ch.SourceID == uuid.Nil || ch.Key != key {
		t.Fatalf("source not named: %+v", ch)
	}
	if !afterRan(user, 1) {
		t.Fatal("AfterCommit work didn't run once after the commit")
	}

	sync1(Subscription{Status: SubActive, CurrentPeriodEnd: *at(month)})
	if got := take(user); len(got) != 0 {
		t.Fatalf("a re-sync of the same period announced %+v", got)
	}
	sync1(Subscription{Status: SubActive, CurrentPeriodEnd: *at(twoMonths)})
	expectOne(t, user, EntitlementExtended, true, at(twoMonths+grace))
	sync1(Subscription{Status: SubActive, CurrentPeriodEnd: *at(twoMonths), CancelAtPeriodEnd: true})
	expectOne(t, user, EntitlementShortened, true, at(twoMonths))

	// A second source with no end keeps the key through the subscription's end.
	order := uuid.New()
	days := (*int)(nil)
	price := &AdminPrice{CatalogPrice: CatalogPrice{Key: plan, Entitlement: &key, EntitlementDays: days}}
	if err := s.inTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return s.grantOrder(ctx, tx, Order{ID: order, UserID: user}, price)
	}); err != nil {
		t.Fatal(err)
	}
	expectOne(t, user, EntitlementGranted, true, nil)
	sync1(Subscription{Status: SubCanceled, EndedAt: *at(-time.Minute)})
	ch = expectOne(t, user, EntitlementRevoked, true, nil)
	if ch.Source != "subscription" {
		t.Fatalf("revoked the wrong source: %+v", ch)
	}
	if err := s.inTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return revokeSource(ctx, tx, "order", order, "", time.Now().UTC())
	}); err != nil {
		t.Fatal(err)
	}
	expectOne(t, user, EntitlementRevoked, false, nil)
	if ok, err := HasEntitlement(ctx, pool, user, key); err != nil || ok {
		t.Fatalf("HasEntitlement after every end = %v, %v", ok, err)
	}

	// The sweep announces an end no sync reported, once. Ends announced
	// above are already marked.
	if _, err := pool.Exec(ctx, `INSERT INTO payment_entitlements (user_id, key, source, source_id, active_until)
		VALUES ($1, $2, 'order', $3, now() - interval '1 hour')`, user, key, uuid.New()); err != nil {
		t.Fatal(err)
	}
	if err := s.sweepLapses(ctx); err != nil {
		t.Fatal(err)
	}
	expectOne(t, user, EntitlementLapsed, false, nil)
	if err := s.sweepLapses(ctx); err != nil {
		t.Fatal(err)
	}
	if got := take(user); len(got) != 0 {
		t.Fatalf("the sweep announced a lapse twice: %+v", got)
	}
}

func TestEntitlementHookFailureRollsBackDB(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	user := testUser(t, s.d.Pool)
	key := testKey("ent")
	rec.Lock()
	rec.failFor[user] = true
	rec.Unlock()
	err := s.inTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return s.grantOrder(ctx, tx, Order{ID: uuid.New(), UserID: user}, &AdminPrice{CatalogPrice: CatalogPrice{Entitlement: &key}})
	})
	if err == nil {
		t.Fatal("a failing hook must fail the sync")
	}
	if ok, err := HasEntitlement(ctx, s.d.Pool, user, key); err != nil || ok {
		t.Fatalf("the grant survived a failing hook: %v, %v", ok, err)
	}
}

func TestEntitlementDeletionAndReplayDB(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	user := testUser(t, s.d.Pool)
	key := testKey("ent")
	grant := func() {
		t.Helper()
		if err := s.inTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
			return s.grantOrder(ctx, tx, Order{ID: uuid.New(), UserID: user}, &AdminPrice{CatalogPrice: CatalogPrice{Entitlement: &key}})
		}); err != nil {
			t.Fatal(err)
		}
		expectOne(t, user, EntitlementGranted, true, nil)
	}
	grant()
	n, err := s.ReplayEntitlements(ctx, key)
	if err != nil || n != 1 {
		t.Fatalf("ReplayEntitlements = %d, %v", n, err)
	}
	expectOne(t, user, EntitlementReplayed, true, nil)
	if !afterRan(user, 2) {
		t.Fatal("AfterCommit work from the grant and the replay didn't run")
	}

	if err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error { return deleteEntitlements(ctx, tx, user) }); err != nil {
		t.Fatal(err)
	}
	expectOne(t, user, EntitlementDeleted, false, nil)
	time.Sleep(200 * time.Millisecond)
	if !afterRan(user, 2) {
		t.Fatal("AfterCommit work queued during an account deletion ran")
	}
}
