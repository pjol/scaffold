package identity

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/notify"
	"scaffoldapp/backend/internal/realtime"
)

func TestParseBadgeLabels(t *testing.T) {
	got, err := parseBadgeLabels("paid=Patron, org = Verified business")
	if err != nil {
		t.Fatal(err)
	}
	if got[BadgePaid] != "Patron" || got[BadgeOrg] != "Verified business" || got[BadgeOfficial] != "Official" || got[BadgeNotable] != "Notable" {
		t.Errorf("labels = %v", got)
	}
	for _, bad := range []string{"vip=VIP", "paid=", "paid"} {
		if _, err := parseBadgeLabels(bad); err == nil {
			t.Errorf("parseBadgeLabels(%q): want an error", bad)
		}
	}
}

func TestParseBadgeCategories(t *testing.T) {
	got, err := parseBadgeCategories(defaultBadgeCategories)
	if err != nil || len(got) != 4 || got[0] != (Category{"business", BadgeOrg, "Business"}) || got[3].Kind != BadgeNotable {
		t.Fatalf("defaults = %+v, %v", got, err)
	}
	if got, err := parseBadgeCategories(" club:org:Sports club ,"); err != nil || len(got) != 1 || got[0].Label != "Sports club" {
		t.Errorf("got %+v, %v", got, err)
	}
	for _, bad := range []string{"club:official:Club", "Club:org:Club", "club:org:", "club:org", "a:org:A,a:org:B"} {
		if _, err := parseBadgeCategories(bad); err == nil {
			t.Errorf("parseBadgeCategories(%q): want an error", bad)
		}
	}
}

func TestDomains(t *testing.T) {
	for in, want := range map[string]string{
		"https://www.Example.com/about": "example.com",
		" shop.example.co.uk. ":         "shop.example.co.uk",
		"example.com:8443":              "example.com",
	} {
		if got := normalizeDomain(in); got != want {
			t.Errorf("normalizeDomain(%q) = %q; want %q", in, got, want)
		}
	}
	for _, d := range []string{"example.com", "a-b.example.org"} {
		if !validDomain(d) {
			t.Errorf("validDomain(%q) = false", d)
		}
	}
	for _, d := range []string{"localhost", "1.2.3.4", "-a.com", "a..com", "exa_mple.com"} {
		if validDomain(d) {
			t.Errorf("validDomain(%q) = true", d)
		}
	}
	if !publicDomain("gmail.com") || !publicDomain("me.github.io") || publicDomain("example.com") {
		t.Error("publicDomain wrong")
	}
}

// fakeResolver answers TXT lookups from a map; err for names it doesn't know.
type fakeResolver struct {
	records map[string][]string
	err     error
}

func (f fakeResolver) LookupTXT(_ context.Context, name string) ([]string, error) {
	if r, ok := f.records[name]; ok {
		return r, nil
	}
	if f.err != nil {
		return nil, f.err
	}
	return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
}

func TestFindTXT(t *testing.T) {
	ctx := context.Background()
	name := "_" + verifyTag + ".example.com"
	want := verifyTag + "=tok123"
	cases := []struct {
		name   string
		r      fakeResolver
		found  bool
		detail string
	}{
		{"record at the verify name", fakeResolver{records: map[string][]string{name: {"other", `"` + want + `"`}}}, true, ""},
		{"record at the apex", fakeResolver{records: map[string][]string{"example.com": {"v=spf1 -all", want}}}, true, ""},
		{"another token", fakeResolver{records: map[string][]string{name: {verifyTag + "=old"}}}, false, "Found a " + verifyTag + " record, but not this one. Copy the value again."},
		{"nothing yet", fakeResolver{}, false, "No TXT record at " + name + " yet. DNS changes can take a while to appear."},
		{"lookup fails", fakeResolver{err: &net.DNSError{Err: "i/o timeout", IsTimeout: true}}, false,
			"The DNS lookup for " + name + " failed (timed out). Try again in a minute."},
	}
	for _, c := range cases {
		found, detail := findTXT(ctx, c.r, "example.com", "tok123")
		if found != c.found || detail != c.detail {
			t.Errorf("%s: findTXT = %v, %q; want %v, %q", c.name, found, detail, c.found, c.detail)
		}
	}
}

// ---- database tests ------------------------------------------------------------
//
// A throwaway database on TEST_DATABASE_URL (a role that may CREATE DATABASE)
// with every migration applied.

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
		name := fmt.Sprintf("identity_test_%d", time.Now().UnixNano())
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

func testService(t *testing.T) *Service {
	t.Helper()
	pool := testPool(t)
	t.Setenv("IDENTITY_PAID_MIN_ACCOUNT_DAYS", "30")
	broker := realtime.NewMemory()
	s := New(&app.Deps{Pool: pool, Cfg: &config.Config{Env: "development"}, Broker: broker, Notify: notify.Realtime{Broker: broker}})
	if err := config.Check(); err != nil {
		t.Fatal(err)
	}
	return s
}

// testUser creates an active account ageDays old.
func testUser(t *testing.T, pool *pgxpool.Pool, ageDays int) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	sub := "test_" + uuid.NewString()
	if err := pool.QueryRow(context.Background(), `INSERT INTO users (auth_subject, email, created_at)
		VALUES ($1, $1 || '@example.test', now() - make_interval(days => $2)) RETURNING id`, sub, ageDays).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func inTx[T any](t *testing.T, pool *pgxpool.Pool, fn func(tx pgx.Tx) (T, error)) (T, error) {
	t.Helper()
	var out T
	err := db.WithTx(context.Background(), pool, func(tx pgx.Tx) error {
		var err error
		out, err = fn(tx)
		return err
	})
	return out, err
}

func setPaid(t *testing.T, pool *pgxpool.Pool, user uuid.UUID, active bool, until time.Time) string {
	t.Helper()
	ch, err := inTx(t, pool, func(tx pgx.Tx) (PaidBadgeChange, error) {
		return SetPaidBadge(context.Background(), tx, user, active, until)
	})
	if err != nil {
		t.Fatal(err)
	}
	return ch.Outcome
}

func badgeKind(t *testing.T, pool *pgxpool.Pool, user uuid.UUID) string {
	t.Helper()
	b, err := BadgeOf(context.Background(), pool, user)
	if err != nil {
		t.Fatal(err)
	}
	if b == nil {
		return ""
	}
	return b.Kind
}

func TestBadgePrecedenceDB(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	pool := s.d.Pool
	a, b := testUser(t, pool, 60), testUser(t, pool, 60)
	if got := setPaid(t, pool, a, true, time.Now().Add(30*24*time.Hour)); got != PaidGranted {
		t.Fatalf("paid grant = %s", got)
	}
	if k := badgeKind(t, pool, a); k != BadgePaid {
		t.Fatalf("badge = %q; want paid", k)
	}
	for _, kind := range []string{BadgeNotable, BadgeOrg} {
		if _, err := grant(ctx, pool, grantInput{userID: a, kind: kind, label: "Riverside Bikes", source: SourceAdmin}); err != nil {
			t.Fatal(err)
		}
	}
	held, err := BadgeOf(ctx, pool, a)
	if err != nil || held == nil || held.Kind != BadgeOrg || held.Label != "Riverside Bikes" || held.KindLabel != "Organization" {
		t.Fatalf("highest badge = %+v, %v", held, err)
	}
	set, err := BadgeSet(ctx, pool, []uuid.UUID{a, b})
	if err != nil || len(set) != 1 || set[a].Kind != BadgeOrg {
		t.Fatalf("BadgeSet = %+v, %v", set, err)
	}
	// An expired badge stops counting at once; the job then ends it.
	if _, err := pool.Exec(ctx, `UPDATE identity_badges SET expires_at = now() - interval '1 minute' WHERE user_id = $1 AND kind = 'org'`, a); err != nil {
		t.Fatal(err)
	}
	if k := badgeKind(t, pool, a); k != BadgeNotable {
		t.Fatalf("after the org badge expired: %q; want notable", k)
	}
	if err := s.expireBadges(ctx); err != nil {
		t.Fatal(err)
	}
	var reason string
	if err := pool.QueryRow(ctx, `SELECT revoked_reason FROM identity_badges WHERE user_id = $1 AND kind = 'org'`, a).Scan(&reason); err != nil || reason != "expired" {
		t.Fatalf("expired badge reason = %q, %v", reason, err)
	}
}

func TestPaidBadgeRulesDB(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	pool := s.d.Pool
	until := time.Now().Add(30 * 24 * time.Hour).UTC().Truncate(time.Second)

	// Too new: withheld, then shown by housekeeping once old enough.
	young := testUser(t, pool, 3)
	if got := setPaid(t, pool, young, true, until); got != PaidIneligible {
		t.Fatalf("young account: %s", got)
	}
	if k := badgeKind(t, pool, young); k != "" {
		t.Fatalf("young account shows %q", k)
	}
	if err := CheckPaidEligible(ctx, pool, young); err == nil {
		t.Fatal("CheckPaidEligible passed a 3-day-old account")
	} else {
		var he *httpx.Error
		if !errors.As(err, &he) || he.Status != http.StatusUnprocessableEntity || he.Code != "not_eligible" {
			t.Fatalf("CheckPaidEligible error = %v", err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET created_at = now() - interval '40 days' WHERE id = $1`, young); err != nil {
		t.Fatal(err)
	}
	if err := s.grantWithheldPaid(ctx); err != nil {
		t.Fatal(err)
	}
	if k := badgeKind(t, pool, young); k != BadgePaid {
		t.Fatalf("withheld badge not shown once eligible: %q", k)
	}

	// Suspended accounts don't get it.
	suspended := testUser(t, pool, 60)
	if _, err := pool.Exec(ctx, `UPDATE users SET status = 'suspended' WHERE id = $1`, suspended); err != nil {
		t.Fatal(err)
	}
	if got := setPaid(t, pool, suspended, true, until); got != PaidIneligible {
		t.Fatalf("suspended: %s", got)
	}

	// Renew, end, and repeat the end (idempotent).
	u := testUser(t, pool, 60)
	if got := setPaid(t, pool, u, true, until); got != PaidGranted {
		t.Fatalf("grant: %s", got)
	}
	if got := setPaid(t, pool, u, true, until); got != PaidUnchanged {
		t.Fatalf("same again: %s", got)
	}
	if got := setPaid(t, pool, u, true, until.Add(30*24*time.Hour)); got != PaidRenewed {
		t.Fatalf("renewal: %s", got)
	}
	if got := setPaid(t, pool, u, false, time.Time{}); got != PaidEnded {
		t.Fatalf("end: %s", got)
	}
	if got := setPaid(t, pool, u, false, time.Time{}); got != PaidUnchanged {
		t.Fatalf("end again: %s", got)
	}

	// Staff removal holds it against billing until an admin lifts it.
	if got := setPaid(t, pool, u, true, until); got != PaidGranted {
		t.Fatalf("regrant: %s", got)
	}
	staff := testUser(t, pool, 400)
	if _, err := inTx(t, pool, func(tx pgx.Tx) (bool, error) {
		return revokeBadge(ctx, tx, u, BadgePaid, "Impersonating a shop.", staff)
	}); err != nil {
		t.Fatal(err)
	}
	if got := setPaid(t, pool, u, true, until.Add(60*24*time.Hour)); got != PaidHeld {
		t.Fatalf("billing re-grant after a staff removal: %s", got)
	}
	if k := badgeKind(t, pool, u); k != "" {
		t.Fatalf("held badge shows: %q", k)
	}
	if err := CheckPaidEligible(ctx, pool, u); err == nil {
		t.Fatal("a held badge must not be buyable")
	}
	type restored struct {
		lifted bool
		ch     PaidBadgeChange
	}
	r, err := inTx(t, pool, func(tx pgx.Tx) (restored, error) {
		lifted, ch, err := restorePaid(ctx, tx, u)
		return restored{lifted, ch}, err
	})
	if err != nil || !r.lifted || r.ch.Outcome != PaidGranted {
		t.Fatalf("restorePaid = %+v, %v", r, err)
	}
	var exp time.Time
	if err := pool.QueryRow(ctx, `SELECT expires_at FROM identity_badges WHERE user_id = $1 AND kind = 'paid'`, u).Scan(&exp); err != nil ||
		!exp.Equal(until.Add(60*24*time.Hour)) {
		t.Fatalf("restored badge ends %v (%v); want the end billing reported while held", exp, err)
	}
	if _, err := inTx(t, pool, func(tx pgx.Tx) (restored, error) {
		lifted, ch, err := restorePaid(ctx, tx, u)
		return restored{lifted, ch}, err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestApplicationLifecycleDB(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	pool := s.d.Pool
	s.SetResolver(fakeResolver{records: map[string][]string{}})
	applicant, staff := testUser(t, pool, 10), testUser(t, pool, 400)

	d, err := s.addDomain(ctx, applicant, "https://www.RiversideBikes.example/")
	if err != nil || d.Domain != "riversidebikes.example" || d.TxtName != "_"+verifyTag+".riversidebikes.example" {
		t.Fatalf("addDomain = %+v, %v", d, err)
	}
	if _, err := s.addDomain(ctx, applicant, "someone@gmail.com"); err == nil {
		t.Fatal("a public email domain was accepted")
	}
	if _, err := s.checkDomain(ctx, applicant, d.Domain); err == nil {
		t.Fatal("a missing record verified")
	}
	if _, err := pool.Exec(ctx, `UPDATE identity_domains SET checked_at = NULL WHERE user_id = $1`, applicant); err != nil {
		t.Fatal(err)
	}
	s.SetResolver(fakeResolver{records: map[string][]string{d.TxtName: {d.TxtValue}}})
	if got, err := s.checkDomain(ctx, applicant, d.Domain); err != nil || !got.Verified {
		t.Fatalf("checkDomain = %+v, %v", got, err)
	}

	in := ApplicationInput{Category: "business", DisplayLabel: "Riverside Bikes", Evidence: []Evidence{
		{Kind: EvidenceDomain, Ref: "riversidebikes.example", Verified: false},
		{Kind: EvidenceURL, Ref: "https://maps.example/riverside", Verified: true},
	}}
	a, err := s.apply(ctx, applicant, in)
	if err != nil {
		t.Fatal(err)
	}
	if !a.Evidence[0].Verified || a.Evidence[1].Verified {
		t.Fatalf("verified must be computed, not taken from the client: %+v", a.Evidence)
	}
	if _, err := s.apply(ctx, applicant, in); !errors.Is(err, errApplicationOpen) {
		t.Fatalf("second open application: %v", err)
	}
	if _, err := s.decideApplication(ctx, applicant, a.ID, Decision{Decision: "approve"}); err == nil {
		t.Fatal("an applicant decided their own application")
	}
	if _, err := s.decideApplication(ctx, staff, a.ID, Decision{Decision: "needs_info"}); err == nil {
		t.Fatal("needs_info without a note was accepted")
	}
	if _, err := s.decideApplication(ctx, staff, a.ID, Decision{Decision: "needs_info", Note: "Add a link to your shop's page."}); err != nil {
		t.Fatal(err)
	}
	in.Note = "Here it is."
	if a, err = s.resubmit(ctx, applicant, a.ID, in); err != nil || a.Status != AppSubmitted {
		t.Fatalf("resubmit = %+v, %v", a, err)
	}
	out, err := s.decideApplication(ctx, staff, a.ID, Decision{Decision: "approve", ExpiresInDays: 30})
	if err != nil || out.Status != AppApproved {
		t.Fatalf("approve = %+v, %v", out, err)
	}
	if len(out.Badges) != 1 || out.Badges[0].Kind != BadgeOrg || out.Badges[0].ExpiresAt == nil ||
		out.Badges[0].ExpiresAt.Sub(time.Now()) < 29*24*time.Hour {
		t.Fatalf("approval badges = %+v", out.Badges)
	}
	var audited int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM admin_audit WHERE target_id = $1`, a.ID.String()).Scan(&audited); err != nil || audited != 2 {
		t.Fatalf("audit rows = %d, %v; want 2", audited, err)
	}
	if _, err := s.withdraw(ctx, applicant, a.ID); !errors.Is(err, errNotOpen) {
		t.Fatalf("withdraw a decided application: %v", err)
	}

	// Deletion removes badges, applications and domains.
	if _, err := inTx(t, pool, func(tx pgx.Tx) (struct{}, error) { return struct{}{}, s.DeleteAccount(ctx, tx, applicant) }); err != nil {
		t.Fatal(err)
	}
	var left int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM identity_badges WHERE user_id = $1)
		+ (SELECT count(*) FROM identity_badge_applications WHERE user_id = $1)
		+ (SELECT count(*) FROM identity_domains WHERE user_id = $1)`, applicant).Scan(&left); err != nil || left != 0 {
		t.Fatalf("rows left after deletion: %d, %v", left, err)
	}
}
