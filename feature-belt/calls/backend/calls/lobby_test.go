package calls

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/notify"
	"scaffoldapp/backend/internal/realtime"
)

// Hosts see, and "Admit all" admits, only people still on the waiting
// page: someone who stops checking in or closes the page drops out, and a
// check-in from the same page brings them back.
func TestLobbyOnlyPeopleStillThere(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	broker := realtime.NewMemory()
	s := &Service{d: &app.Deps{Pool: pool, Broker: broker, Notify: notify.Realtime{Broker: broker},
		Cfg: &config.Config{PublicWebURL: "https://app.test"}}}
	current.Store(s)

	var host uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO users (auth_subject, email, display_name) VALUES ('test_host', 'host@example.test', 'Host') RETURNING id`).Scan(&host); err != nil {
		t.Fatal(err)
	}
	m, err := CreateMeeting(ctx, pool, MeetingInput{HostID: host, Title: "Open house", Access: AccessOpen})
	if err != nil {
		t.Fatal(err)
	}

	do := func(h func(http.ResponseWriter, *http.Request) error, body string, params ...string) (*httptest.ResponseRecorder, error) {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		rc := chi.NewRouteContext()
		for i := 0; i+1 < len(params); i += 2 {
			rc.URLParams.Add(params[i], params[i+1])
		}
		r = r.WithContext(context.WithValue(auth.WithIdentity(ctx, &auth.Identity{UserID: host, Status: "active"}), chi.RouteCtxKey, rc))
		w := httptest.NewRecorder()
		return w, h(w, r)
	}
	knock := func(name string) GuestKnock {
		t.Helper()
		w, err := do(s.guestKnock, `{"name":"`+name+`"}`, "code", m.Code)
		if err != nil {
			t.Fatalf("knock %s: %v", name, err)
		}
		var k GuestKnock
		if err := json.Unmarshal(w.Body.Bytes(), &k); err != nil {
			t.Fatal(err)
		}
		return k
	}
	waiting := func() []string {
		t.Helper()
		w, err := do(s.listLobby, ``, "id", m.ID.String())
		if err != nil {
			t.Fatal(err)
		}
		var page httpx.Page[LobbyEntry]
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		names := []string{}
		for _, e := range page.Items {
			names = append(names, e.Name)
		}
		return names
	}
	status := func(k GuestKnock) string {
		t.Helper()
		w, err := do(s.guestStatus, `{"key":"`+k.Key+`"}`, "id", k.ID.String())
		if err != nil {
			t.Fatal(err)
		}
		var e LobbyEntry
		_ = json.Unmarshal(w.Body.Bytes(), &e)
		return e.Status
	}
	silence := func(k GuestKnock) { // no check-in for longer than the lease
		t.Helper()
		if _, err := pool.Exec(ctx, `UPDATE calls_lobby SET seen_at = now() - interval '2 minutes' WHERE id = $1`, k.ID); err != nil {
			t.Fatal(err)
		}
	}

	ana, ben, cy := knock("Ana"), knock("Ben"), knock("Cy")
	if got := waiting(); len(got) != 3 {
		t.Fatalf("waiting = %v, want all three", got)
	}

	// Ben stops checking in: hosts stop seeing him before the sweep runs, and after it.
	silence(ben)
	if got := strings.Join(waiting(), ","); got != "Ana,Cy" {
		t.Errorf("after Ben went quiet: %q, want Ana,Cy", got)
	}
	if err := s.sweepLobby(ctx); err != nil {
		t.Fatal(err)
	}
	var away bool
	if err := pool.QueryRow(ctx, `SELECT away_at IS NOT NULL FROM calls_lobby WHERE id = $1`, ben.ID).Scan(&away); err != nil || !away {
		t.Errorf("Ben set aside after the sweep: %v %v", away, err)
	}

	// Cy closes the page: the beacon drops her at once.
	if _, err := do(s.guestLeave, `{"key":"`+cy.Key+`"}`, "id", cy.ID.String()); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(waiting(), ","); got != "Ana" {
		t.Errorf("after Cy left: %q, want Ana", got)
	}
	// A wrong key leaves nothing and says nothing exists.
	if _, err := do(s.guestLeave, `{"key":"cgk_wrong"}`, "id", ana.ID.String()); err == nil {
		t.Error("leave with a wrong key: want not found")
	}

	// Ben comes back (his page checks in again): he's waiting again.
	if got := status(ben); got != "waiting" {
		t.Errorf("Ben's status after checking in: %q", got)
	}
	if got := strings.Join(waiting(), ","); got != "Ana,Ben" {
		t.Errorf("after Ben came back: %q, want Ana,Ben", got)
	}

	// Admit all lets in only the people still there.
	if _, err := do(s.admit, `{"all":true}`, "id", m.ID.String()); err != nil {
		t.Fatal(err)
	}
	for k, want := range map[*GuestKnock]string{&ana: "admitted", &ben: "admitted", &cy: "waiting"} {
		var got string
		if err := pool.QueryRow(ctx, `SELECT status FROM calls_lobby WHERE id = $1`, k.ID).Scan(&got); err != nil || got != want {
			t.Errorf("%s: status %q (%v), want %q", k.Name, got, err, want)
		}
	}
	// Checking in after a decision changes nothing.
	if got := status(ana); got != "admitted" {
		t.Errorf("Ana's status after admit: %q", got)
	}
}

// Search hits name their org (OrgID, Context) and a search scoped to
// another org leaves that org's meetings out.
func TestSearchNamesOrg(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	broker := realtime.NewMemory()
	s := &Service{d: &app.Deps{Pool: pool, Broker: broker, Notify: notify.Realtime{Broker: broker},
		Cfg: &config.Config{PublicWebURL: "https://app.test"}}}
	current.Store(s)
	var host, club, other uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO users (auth_subject, email, display_name) VALUES ('test_host', 'host@example.test', 'Host') RETURNING id`).Scan(&host); err != nil {
		t.Fatal(err)
	}
	for slug, id := range map[string]*uuid.UUID{"river-club": &club, "other-org": &other} {
		if err := pool.QueryRow(ctx, `INSERT INTO orgs (name, slug) VALUES (initcap(replace($1, '-', ' ')), $1) RETURNING id`, slug).Scan(id); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'owner')`, *id, host); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := CreateMeeting(ctx, pool, MeetingInput{HostID: host, OrgID: &club, Title: "Standup club", Access: AccessOrg}); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateMeeting(ctx, pool, MeetingInput{HostID: host, Title: "Standup solo", Access: AccessInvited}); err != nil {
		t.Fatal(err)
	}
	me := &auth.Identity{UserID: host, Status: "active"}
	got := map[string]string{}
	hits, err := s.Search(ctx, me, "standup", 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hits {
		got[h.Title] = h.OrgID + "|" + h.Context
	}
	if got["Standup club"] != club.String()+"|River Club · Calls" || got["Standup solo"] != "|Calls" {
		t.Errorf("hits = %v", got)
	}
	scoped, err := s.Search(app.WithSearchOrg(ctx, other), me, "standup", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(scoped) != 1 || scoped[0].Title != "Standup solo" {
		t.Errorf("scoped to another org: %v, want only the meeting in no org", scoped)
	}
}
