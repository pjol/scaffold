package calls

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
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/notify"
	"scaffoldapp/backend/internal/realtime"
)

// testPool creates a throwaway database on TEST_DATABASE_URL (a role that may
// CREATE DATABASE), applies every migration, and drops it afterwards.
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
	name := fmt.Sprintf("calls_test_%d", time.Now().UnixNano())
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
		pool.Close()
		_, _ = admin.Exec(ctx, "DROP DATABASE "+name+" WITH (FORCE)")
		_ = admin.Close(ctx)
	})
	if err := db.Migrate(ctx, pool, false); err != nil {
		t.Fatal(err)
	}
	return pool
}

// A meeting another feature made is edited and cancelled there: REST
// refuses its title, schedule, invitees and cancel (the room's own
// settings still work), the seams keep working, and changes that keep
// people out re-check their live subscriptions.
func TestSourcedMeetingsAndRevokes(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	broker := realtime.NewMemory()
	var mu sync.Mutex
	var revokes []string
	sctx, stop := context.WithCancel(ctx)
	defer stop()
	go broker.Subscribe(sctx, func(ev realtime.Event) {
		if ev.Topic == realtime.ControlTopic && ev.Type == realtime.EventRevoke {
			mu.Lock()
			revokes = append(revokes, string(ev.Data))
			mu.Unlock()
		}
	})
	time.Sleep(50 * time.Millisecond)
	s := &Service{d: &app.Deps{Pool: pool, Broker: broker, Notify: notify.Realtime{Broker: broker},
		Cfg: &config.Config{PublicWebURL: "https://app.test"}}}
	current.Store(s)

	var host, guest uuid.UUID
	for name, id := range map[string]*uuid.UUID{"host": &host, "guest": &guest} {
		if err := pool.QueryRow(ctx, `INSERT INTO users (auth_subject, email, display_name) VALUES ($1, $1 || '@example.test', $1) RETURNING id`,
			"test_"+name).Scan(id); err != nil {
			t.Fatal(err)
		}
	}
	me := &auth.Identity{UserID: host, Status: "active"}
	sourced, err := CreateMeeting(ctx, pool, MeetingInput{HostID: host, Title: "Planning", Access: AccessInvited,
		InviteeIDs: []uuid.UUID{guest}, Source: &Source{Kind: "calendar.event", ID: uuid.NewString()}})
	if err != nil {
		t.Fatal(err)
	}
	plain, err := CreateMeeting(ctx, pool, MeetingInput{HostID: host, Title: "Ad hoc"})
	if err != nil {
		t.Fatal(err)
	}

	call := func(h func(http.ResponseWriter, *http.Request) error, method string, id uuid.UUID, body string, params ...string) string {
		t.Helper()
		r := httptest.NewRequest(method, "/", strings.NewReader(body))
		rc := chi.NewRouteContext()
		rc.URLParams.Add("id", id.String())
		for i := 0; i+1 < len(params); i += 2 {
			rc.URLParams.Add(params[i], params[i+1])
		}
		r = r.WithContext(context.WithValue(auth.WithIdentity(ctx, me), chi.RouteCtxKey, rc))
		err := h(httptest.NewRecorder(), r)
		var he *httpx.Error
		switch {
		case err == nil:
			return "ok"
		case errors.As(err, &he):
			return he.Code
		}
		return err.Error()
	}

	refused := "calls_managed_by_source"
	for name, got := range map[string]string{
		"title":          call(s.updateMeeting, "PATCH", sourced.ID, `{"title":"Renamed"}`),
		"schedule":       call(s.updateMeeting, "PATCH", sourced.ID, `{"clear_times":true}`),
		"cancel":         call(s.cancelMeeting, "DELETE", sourced.ID, ``),
		"invite":         call(s.addInvitees, "POST", sourced.ID, fmt.Sprintf(`{"user_ids":[%q]}`, uuid.New())),
		"remove someone": call(s.removeInvitee, "DELETE", sourced.ID, ``, "userID", guest.String()),
	} {
		if got != refused {
			t.Errorf("%s on a sourced meeting: %s, want %s", name, got, refused)
		}
	}
	if got := call(s.updateMeeting, "PATCH", sourced.ID, `{"locked":true}`); got != "ok" {
		t.Errorf("locking a sourced meeting's room: %s, want ok", got)
	}
	if got := call(s.updateMeeting, "PATCH", plain.ID, `{"title":"Renamed"}`); got != "ok" {
		t.Errorf("renaming an ordinary meeting: %s, want ok", got)
	}
	// The calendar's own path still edits and cancels it.
	title := "Planning (moved)"
	if m, err := UpdateMeeting(ctx, pool, sourced.ID, MeetingUpdate{Title: &title}); err != nil || m.Title != title {
		t.Errorf("seam update: %v %q", err, m.Title)
	}
	if err := CancelMeeting(ctx, pool, sourced.ID); err != nil {
		t.Errorf("seam cancel: %v", err)
	}
	if got := call(s.cancelMeeting, "DELETE", plain.ID, ``); got != "ok" {
		t.Errorf("cancelling an ordinary meeting: %s, want ok", got)
	}

	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	topics := map[string]int{}
	for _, raw := range revokes {
		var m struct {
			Topic string `json:"topic"`
		}
		_ = json.Unmarshal([]byte(raw), &m)
		topics[m.Topic]++
	}
	// lock, seam update, seam cancel on the sourced one; cancel on the plain one.
	if topics[meetingTopic(sourced.ID)] < 3 || topics[meetingTopic(plain.ID)] < 1 {
		t.Errorf("revokes = %v, want the meetings' topics re-checked after lock, update and cancel", topics)
	}
}

// The create preview names who can join, not just the title and time.
func TestCreateDetails(t *testing.T) {
	start := time.Date(2026, 11, 2, 15, 0, 0, 0, time.UTC)
	got := createDetails(createPreview{Title: "Review", StartsAt: &start}, "Acme", time.UTC)
	for _, want := range []string{"Title: Review", "Organization: Acme", "guests without an account", "members of Acme join directly", "Nobody is invited"} {
		if !strings.Contains(got, want) {
			t.Errorf("createDetails = %q, missing %q", got, want)
		}
	}
	if got := createDetails(createPreview{Access: AccessInvited}, "", time.UTC); !strings.Contains(got, "only invited people") || !strings.Contains(got, "Unscheduled") {
		t.Errorf("invited, unscheduled: %q", got)
	}
}
