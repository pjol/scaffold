package social

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/bg"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/moderation"
	"scaffoldapp/backend/internal/notify"
)

// Database tests: a throwaway database on TEST_DATABASE_URL (a role that may
// CREATE DATABASE) with every migration applied, dropped afterwards.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		t.Skip("TEST_DATABASE_URL not set: a Postgres URL whose role can create databases")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("social_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(base)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = bg.Wait(ctx)
		pool.Close()
		_, _ = admin.Exec(ctx, "DROP DATABASE "+name+" WITH (FORCE)")
		_ = admin.Close(ctx)
	})
	if err := db.Migrate(ctx, pool, false); err != nil {
		t.Fatal(err)
	}
	return pool
}

type sent struct {
	mu sync.Mutex
	n  []notify.Notification
}

func (s *sent) Send(_ context.Context, n notify.Notification) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n = append(s.n, n)
	return nil
}

func (s *sent) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.n)
}

func newTestService(pool *pgxpool.Pool, out *sent) *Service {
	return &Service{d: &app.Deps{Pool: pool, Notify: out}, handleCooldown: 7 * 24 * time.Hour, handleRedirect: 30 * 24 * time.Hour,
		maxFollowing: 2000, bioMax: 280, retention: 90 * 24 * time.Hour}
}

func mkUser(t *testing.T, pool *pgxpool.Pool, name string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := pool.QueryRow(context.Background(), `INSERT INTO users (auth_subject, email, display_name) VALUES ($1, $2, $3) RETURNING id`,
		"t_"+uuid.NewString(), uuid.NewString()[:8]+"@x.test", name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func counts(t *testing.T, pool *pgxpool.Pool, id uuid.UUID) (followers, following int) {
	t.Helper()
	if err := pool.QueryRow(context.Background(), `SELECT followers, following FROM social_profiles WHERE user_id = $1`, id).Scan(&followers, &following); err != nil {
		t.Fatal(err)
	}
	return
}

func TestDBProfilesAndHandles(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := newTestService(pool, &sent{})
	a, b, c := mkUser(t, pool, "Sam Rivera"), mkUser(t, pool, "Sam Rivera"), mkUser(t, pool, "Jo Lee")

	refs, err := Refs(ctx, pool, []uuid.UUID{a, b, c, uuid.New()})
	if err != nil || len(refs) != 3 {
		t.Fatalf("Refs = %v, %v", refs, err)
	}
	if refs[a].Handle != "sam_rivera" || refs[b].Handle == "sam_rivera" || !ValidHandle(refs[b].Handle) || refs[c].Handle != "jo_lee" {
		t.Errorf("generated handles: %q %q %q", refs[a].Handle, refs[b].Handle, refs[c].Handle)
	}

	// Change: the old handle redirects and stays reserved; the cooldown applies.
	change := func(id uuid.UUID, h string) error {
		return db.WithTx(ctx, pool, func(tx pgx.Tx) error {
			_, err := changeHandle(ctx, tx, id, h, s.handleCooldown, s.handleRedirect)
			return err
		})
	}
	if err := change(a, "sammy"); err != nil {
		t.Fatal(err)
	}
	if id, cur, err := resolveHandle(ctx, pool, "@Sam_Rivera"); err != nil || id != a || cur != "sammy" {
		t.Errorf("old handle resolves to %v %q %v", id, cur, err)
	}
	if err := change(c, "sam_rivera"); !errors.Is(err, errHandleTaken) {
		t.Errorf("someone else took a held handle: %v", err)
	}
	var he *httpx.Error
	if err := change(a, "sammy_r"); !errors.As(err, &he) || he.Code != "handle_cooldown" {
		t.Errorf("cooldown: %v", err)
	}
	if err := change(c, "sammy"); !errors.Is(err, errHandleTaken) {
		t.Errorf("took a current handle: %v", err)
	}
	// Taking back one's own old handle is allowed (cooldown aside).
	s.handleCooldown = 0
	if err := db.WithTx(ctx, pool, func(tx pgx.Tx) error {
		_, err := changeHandle(ctx, tx, a, "sam_rivera", 0, s.handleRedirect)
		return err
	}); err != nil {
		t.Errorf("take back: %v", err)
	}
	if id, cur, _ := resolveHandle(ctx, pool, "sammy"); id != a || cur != "sam_rivera" {
		t.Errorf("sammy now redirects: %v %q", id, cur)
	}
	if free, _ := handleFree(ctx, pool, c, "sammy"); free {
		t.Error("sammy is held for a")
	}

	// Deletion frees nothing at once: the handle is held, unlinked.
	if err := db.WithTx(ctx, pool, func(tx pgx.Tx) error { return s.DeleteAccount(ctx, tx, a) }); err != nil {
		t.Fatal(err)
	}
	if _, _, err := resolveHandle(ctx, pool, "sam_rivera"); !errors.Is(err, ErrNotFound) {
		t.Errorf("a deleted account's handle resolves: %v", err)
	}
	if free, _ := handleFree(ctx, pool, c, "sam_rivera"); free {
		t.Error("a deleted account's handle is free at once")
	}
}

func TestDBFollowsBlocksAndFeed(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	out := &sent{}
	s := newTestService(pool, out)
	a, b, c := mkUser(t, pool, "Ann"), mkUser(t, pool, "Ben"), mkUser(t, pool, "Cy")

	if _, err := s.follow(ctx, a, a); !errors.Is(err, ErrSelfFollow) {
		t.Errorf("self follow: %v", err)
	}
	for i := 0; i < 2; i++ { // idempotent
		if _, err := s.follow(ctx, a, b); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.follow(ctx, c, b); err != nil {
		t.Fatal(err)
	}
	if f, g := counts(t, pool, b); f != 2 || g != 0 {
		t.Errorf("b counts %d/%d", f, g)
	}
	if f, g := counts(t, pool, a); f != 0 || g != 1 {
		t.Errorf("a counts %d/%d", f, g)
	}
	// One notification per follower per hour, whatever the churn.
	if err := unfollow(ctx, pool, a, b); err != nil {
		t.Fatal(err)
	}
	if _, err := s.follow(ctx, a, b); err != nil {
		t.Fatal(err)
	}
	_ = bg.Wait(ctx)
	if out.count() != 2 {
		t.Errorf("notifications sent = %d, want 2 (a once, c once)", out.count())
	}
	s.maxFollowing = 1
	if _, err := s.follow(ctx, a, c); err == nil {
		t.Error("follow cap not applied")
	}
	s.maxFollowing = 2000

	// Feed: b's activity, gated by section and visible_after.
	RegisterSection("t_posts", "Posts", VisibilityFollowers)
	RegisterKind(Kind{ID: "t.post", Section: "t_posts"})
	rec := func(ref, title string, after time.Time) {
		if err := Record(ctx, pool, Activity{ActorID: b, Kind: "t.post", RefID: ref, Data: map[string]any{"title": title}, VisibleAfter: after}); err != nil {
			t.Fatal(err)
		}
	}
	rec("1", "First", time.Time{})
	rec("2", "Later", time.Now().Add(time.Hour))
	rec("1", "First (edited)", time.Time{}) // same ref: updates
	feed, err := s.Feed(ctx, a, "", 20)
	if err != nil || len(feed.Items) != 1 || feed.Items[0].Title != "First (edited)" {
		t.Fatalf("a's feed = %+v, %v", feed.Items, err)
	}
	own, _ := s.ProfileActivity(ctx, b, b, "", 20)
	if len(own.Items) != 2 {
		t.Errorf("the owner sees delayed items: %d", len(own.Items))
	}
	stranger := mkUser(t, pool, "Dee")
	if got, _ := s.ProfileActivity(ctx, b, stranger, "", 20); len(got.Items) != 0 {
		t.Errorf("a stranger sees followers-only items: %+v", got.Items)
	}
	if _, err := pool.Exec(ctx, `UPDATE social_profiles SET privacy = '{"t_posts":"private"}' WHERE user_id = $1`, b); err != nil {
		t.Fatal(err)
	}
	if feed, _ := s.Feed(ctx, a, "", 20); len(feed.Items) != 0 {
		t.Errorf("private section in the feed: %+v", feed.Items)
	}
	if ok, _ := CanSee(ctx, pool, b, a, "t_posts"); ok {
		t.Error("CanSee private")
	}
	if ok, _ := CanSee(ctx, pool, b, a, SectionConnections); !ok {
		t.Error("CanSee public")
	}
	if err := RemoveActivity(ctx, pool, "t.post", "1"); err != nil {
		t.Fatal(err)
	}

	// c's feed shows "a followed b"? No: c follows b only. a's own profile
	// has "Followed Ben"; a block between c and a hides that item from c.
	if _, err := s.follow(ctx, c, a); err != nil {
		t.Fatal(err)
	}
	feed, _ = s.Feed(ctx, c, "", 20)
	if len(feed.Items) != 1 || feed.Items[0].Kind != KindFollowed || feed.Items[0].Subject == nil || feed.Items[0].Subject.UserID != b {
		t.Fatalf("c's feed = %+v", feed.Items)
	}
	if _, err := moderation.Block(ctx, pool, b, c); err != nil {
		t.Fatal(err)
	}
	if feed, _ := s.Feed(ctx, c, "", 20); len(feed.Items) != 0 {
		t.Errorf("an item about someone c is blocked by: %+v", feed.Items)
	}

	// The block refuses follows and, once repaired, removes edges both ways.
	if _, err := s.follow(ctx, b, c); !errors.Is(err, ErrBlocked) {
		t.Errorf("follow across a block: %v", err)
	}
	page, err := s.listEdges(ctx, b, a, "followers", httpx.PageParams{Limit: 20})
	if err != nil || len(page.Items) != 1 || page.Items[0].UserID != a {
		t.Errorf("b's followers after the block = %+v, %v", page.Items, err)
	}
	if f, _ := counts(t, pool, b); f != 1 {
		t.Errorf("b's followers count after repair = %d", f)
	}
	if _, g := counts(t, pool, c); g != 1 {
		t.Errorf("c's following count after repair = %d", g)
	}

	// The sweep finds blocks nobody looked at.
	d, e := mkUser(t, pool, "Dan"), mkUser(t, pool, "Eve")
	if _, err := s.follow(ctx, d, e); err != nil {
		t.Fatal(err)
	}
	if _, err := moderation.Block(ctx, pool, e, d); err != nil {
		t.Fatal(err)
	}
	var n int64
	for m := 0; m < 1440; m++ {
		k, err := sweepBlocked(ctx, pool, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(m)*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		n += k
	}
	if n != 1 {
		t.Errorf("sweep removed %d follows, want 1", n)
	}

	// Suggestions and search leave out blocks either way.
	sug, err := s.suggestions(ctx, c, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range sug {
		if p.UserID == b || p.UserID == c || p.UserID == a {
			t.Errorf("suggested %s (blocked, self or followed)", p.Handle)
		}
	}
	people, _, err := s.searchPeople(ctx, c, "be", 10)
	if err != nil || len(people) != 0 {
		t.Errorf("search found a blocker: %+v %v", people, err)
	}
	if people, _, _ := s.searchPeople(ctx, a, "be", 10); len(people) != 1 {
		t.Errorf("search by name prefix: %+v", people)
	}

	// Deletion recounts the other side.
	if err := db.WithTx(ctx, pool, func(tx pgx.Tx) error { return s.DeleteAccount(ctx, tx, a) }); err != nil {
		t.Fatal(err)
	}
	if f, _ := counts(t, pool, b); f != 0 {
		t.Errorf("b's followers after a's deletion = %d", f)
	}
}

// TestDBHTTP drives the routes as clients do (JSON in, JSON out).
func TestDBHTTP(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := newTestService(pool, &sent{})
	a, b := mkUser(t, pool, "Ann Archer"), mkUser(t, pool, "Ben Bell")
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			id := uuid.MustParse(req.Header.Get("X-User"))
			next.ServeHTTP(w, req.WithContext(auth.WithIdentity(req.Context(), &auth.Identity{UserID: id, Status: "active"})))
		})
	})
	s.MountUser(r)
	call := func(as uuid.UUID, method, path, body string, want int, out any) {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("X-User", as.String())
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != want {
			t.Fatalf("%s %s = %d %s, want %d", method, path, w.Code, w.Body.String(), want)
		}
		if out != nil {
			if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
				t.Fatalf("%s %s: %v", method, path, err)
			}
		}
	}

	var me MyProfile
	call(a, "GET", "/social/me", "", 200, &me)
	if me.User.Handle != "ann_archer" || len(me.Privacy) < 2 || me.Limits.MaxLinks != MaxLinks {
		t.Fatalf("me = %+v", me)
	}
	call(a, "PATCH", "/social/me", `{"bio":"  Hello  ","links":[{"url":"example.com","label":"Site"}],"privacy":{"activity":"followers"}}`, 200, &me)
	if me.Bio != "Hello" || len(me.Links) != 1 || me.Links[0].URL != "https://example.com" {
		t.Errorf("patched = %+v", me)
	}
	call(a, "PATCH", "/social/me", `{"privacy":{"activity":"friends"}}`, 400, nil)
	call(a, "PATCH", "/social/me", `{"links":[{"url":"javascript:alert(1)"}]}`, 400, nil)

	var e struct {
		Error httpx.Error `json:"error"`
	}
	call(a, "PUT", "/social/me/handle", `{"handle":"Admin"}`, 422, &e)
	if e.Error.Code != "handle_reserved" {
		t.Errorf("reserved: %+v", e.Error)
	}
	call(a, "PUT", "/social/me/handle", `{"handle":"@Ann"}`, 200, &me)
	if me.User.Handle != "ann" || me.HandleChangeAt == nil {
		t.Errorf("changed = %+v", me)
	}
	var check HandleCheck
	call(b, "GET", "/social/handles/ann_archer", "", 200, &check)
	if check.Available || check.Code != "handle_taken" {
		t.Errorf("held handle check = %+v", check)
	}
	call(b, "GET", "/social/handles/bennett", "", 200, &check)
	if !check.Available {
		t.Errorf("free handle check = %+v", check)
	}

	var p Profile
	call(b, "GET", "/social/profiles/ann_archer", "", 200, &p) // old handle
	if p.User.Handle != "ann" || p.Viewer.IsSelf || p.Bio != "Hello" || p.Sections[SectionActivity] {
		t.Errorf("profile via old handle = %+v", p)
	}
	call(b, "GET", "/social/profiles/nobody_here", "", 404, nil)

	var st FollowState
	call(b, "PUT", "/social/follows/"+a.String(), "", 200, &st)
	if !st.Following || st.Followers != 1 {
		t.Errorf("follow = %+v", st)
	}
	call(b, "PUT", "/social/follows/"+b.String(), "", 422, nil)
	call(b, "GET", "/social/profiles/ann", "", 200, &p)
	if !p.Viewer.Following || p.Followers != 1 || !p.Sections[SectionActivity] {
		t.Errorf("profile after follow = %+v", p)
	}
	var people httpx.Page[Person]
	call(b, "GET", "/social/profiles/ann/followers", "", 200, &people)
	if len(people.Items) != 1 || people.Items[0].UserID != b || people.Items[0].Since == nil {
		t.Errorf("followers = %+v", people)
	}
	var mine Profile
	call(a, "GET", "/social/profiles/me", "", 200, &mine)
	if !mine.Viewer.IsSelf || mine.User.UserID != a {
		t.Errorf("me profile = %+v", mine)
	}

	var feed httpx.Page[ActivityItem]
	call(a, "GET", "/social/feed", "", 200, &feed)
	if len(feed.Items) != 0 { // a follows nobody
		t.Errorf("a's feed = %+v", feed.Items)
	}
	call(b, "GET", "/social/profiles/ben_bell/activity", "", 200, &feed)
	if len(feed.Items) != 1 || feed.Items[0].Link != "/u/ann" || feed.Items[0].Title != "Followed Ann Archer" {
		t.Errorf("b's activity = %+v", feed.Items)
	}
	call(a, "GET", "/social/profiles/ben_bell/activity", "", 200, &feed)
	if len(feed.Items) != 1 || feed.Items[0].Title != "Followed you" {
		t.Errorf("b's activity as a = %+v", feed.Items)
	}
	var found struct {
		Items []Person `json:"items"`
	}
	call(a, "GET", "/social/people?q=Ben", "", 200, &found)
	if len(found.Items) != 1 || !found.Items[0].FollowsYou {
		t.Errorf("people = %+v", found.Items)
	}
	call(a, "GET", "/social/suggestions", "", 200, &found)
	if len(found.Items) != 1 || found.Items[0].UserID != b {
		t.Errorf("suggestions = %+v", found.Items)
	}
	call(b, "DELETE", "/social/follows/"+a.String(), "", 200, &st)
	if st.Following || st.Followers != 0 {
		t.Errorf("unfollow = %+v", st)
	}

	// Agent tools read as the caller.
	tools := map[string]app.Tool{}
	for _, tl := range s.Tools() {
		tools[tl.Name] = tl
		if !tl.ReadOnly {
			t.Errorf("%s is not read-only", tl.Name)
		}
	}
	got, err := tools["social_get_profile"].Handler(ctx, &auth.Identity{UserID: b}, json.RawMessage(`{"handle":"@ann"}`))
	if err != nil || got.(map[string]any)["profile"].(Profile).User.UserID != a {
		t.Errorf("social_get_profile = %v, %v", got, err)
	}
	if _, err := tools["social_list_feed"].Handler(ctx, &auth.Identity{UserID: b}, nil); err != nil {
		t.Errorf("social_list_feed: %v", err)
	}

	// Export carries the profile and the follow lists.
	exp, err := s.ExportAccount(ctx, a)
	if err != nil || exp.(map[string]any)["profile"] == nil {
		t.Errorf("export = %v, %v", exp, err)
	}
}
