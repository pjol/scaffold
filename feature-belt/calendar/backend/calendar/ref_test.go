package calendar

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
)

func TestEventFromURL(t *testing.T) {
	id := uuid.New()
	occ := time.Date(2026, 11, 9, 17, 0, 0, 0, time.UTC)
	for raw, want := range map[string]struct {
		ok  bool
		occ *time.Time
	}{
		"/calendar/event/" + id.String():                                                                 {true, nil},
		"/calendar/event/" + id.String() + "/":                                                           {true, nil},
		"/calendar/event/" + id.String() + "?occurrence=2026-11-09T17:00:00Z":                            {true, &occ},
		"/calendar/event/" + id.String() + "?occurrence=" + url.QueryEscape("2026-11-09T09:00:00-08:00"): {true, &occ},
		"/calendar/event/" + id.String() + "?occurrence=tuesday":                                         {false, nil},
		"/calendar/event/not-a-uuid":                                                                     {false, nil},
		"/calendar/event/" + id.String() + "/edit":                                                       {false, nil},
		"/calendar/links/" + id.String():                                                                 {false, nil},
		"/calendar":                                                                                      {false, nil},
		"/docs/" + id.String():                                                                           {false, nil},
	} {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		got, o, ok := EventFromURL(u)
		if ok != want.ok || (ok && got != id) || (want.occ == nil) != (o == nil) || (o != nil && !o.Equal(*want.occ)) {
			t.Errorf("EventFromURL(%q) = %v, %v, %v; want ok=%v occ=%v", raw, got, o, ok, want.ok, want.occ)
		}
	}
	if _, _, ok := EventFromURL(nil); ok {
		t.Error("nil URL")
	}
}

func TestEventRefRefusesWithoutViewer(t *testing.T) {
	if _, err := EventRef(context.Background(), nil, nil, uuid.New(), nil); !errors.Is(err, ErrEventNotFound) {
		t.Fatalf("nil viewer: %v", err)
	}
	if _, err := EventRef(context.Background(), nil, &auth.Identity{UserID: uuid.New()}, uuid.Nil, nil); !errors.Is(err, ErrEventNotFound) {
		t.Fatalf("nil event id: %v", err)
	}
}

// offPolicy is an agent policy with everything off.
type offPolicy struct{}

func (offPolicy) Name() string { return "agents" }
func (offPolicy) FeatureLevel(context.Context, *auth.Identity, string) (app.AgentLevel, app.AgentLevel, error) {
	return app.AgentOff, app.AgentOff, nil
}
func (offPolicy) ResourceLevel(context.Context, *auth.Identity, string, string, string) (app.AgentLevel, error) {
	return app.AgentOff, nil
}
func (offPolicy) ResourceLevels(_ context.Context, _ *auth.Identity, _, _ string, ids []string) (map[string]app.AgentLevel, error) {
	out := map[string]app.AgentLevel{}
	for _, id := range ids {
		out[id] = app.AgentOff
	}
	return out, nil
}
func (offPolicy) GateTool(context.Context, *auth.Identity, string, app.Tool, json.RawMessage) (app.ToolDecision, error) {
	return app.ToolDecision{}, nil
}

// EventRef answers what each viewer's calendar would show: the owner, a
// guest and someone with "all details" see the event; a colleague with the
// default free/busy sees "Busy" and the time in their own zone, nothing
// else; an event on a calendar not shared with them, an outsider, a
// cancelled event or occurrence, an occurrence outside the series, a
// missing id and an agent whose scope is off get ErrEventNotFound.
func TestEventRefAccess(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	registerAccessKinds()
	prev := calendarService.Load()
	calendarService.Store(&Service{d: &app.Deps{Pool: pool}})
	t.Cleanup(func() { calendarService.Store(prev) })
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	user := func(name string) uuid.UUID {
		var id uuid.UUID
		must(pool.QueryRow(ctx, `INSERT INTO users (auth_subject, email, display_name) VALUES ($1, $1 || '@example.test', $2) RETURNING id`,
			"test_"+name, name).Scan(&id))
		return id
	}
	owner, guest, colleague, reader, outsider := user("Sam"), user("Gus"), user("Cole"), user("Rae"), user("Otto")
	var org uuid.UUID
	must(pool.QueryRow(ctx, `INSERT INTO orgs (name, slug) VALUES ('Acme', 'acme-cal') RETURNING id`).Scan(&org))
	for _, u := range []uuid.UUID{owner, guest, colleague, reader} {
		_, err := pool.Exec(ctx, `INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'member')`, org, u)
		must(err)
	}
	_, err := pool.Exec(ctx, `INSERT INTO calendar_settings (user_id, timezone) VALUES ($1, 'Europe/London')`, colleague)
	must(err)
	calendar := func(name string, primary bool, general access.Level) uuid.UUID {
		var id uuid.UUID
		must(pool.QueryRow(ctx, `INSERT INTO calendar_calendars (owner_id, org_id, name, is_primary) VALUES ($1, $2, $3, $4) RETURNING id`,
			owner, org, name, primary).Scan(&id))
		must(access.CreateResource(ctx, pool, access.NewResource{Kind: KindCalendar, ID: id, OrgID: org, OwnerID: owner, General: general}))
		return id
	}
	primary := calendar("Sam", true, access.View) // colleagues see free/busy, as ensurePrimary makes it
	private := calendar("Personal", false, access.None)
	must(access.AddGrant(ctx, pool, access.Ref{Kind: KindCalendar, ID: primary}, access.SubjectRef{Type: "user", ID: reader}, access.Comment, owner))

	start := time.Date(2026, 11, 9, 18, 0, 0, 0, time.UTC)
	event := func(cal uuid.UUID, title, status string, rrule *string) uuid.UUID {
		var id uuid.UUID
		must(pool.QueryRow(ctx, `
			INSERT INTO calendar_events (owner_id, calendar_id, title, description, location, conference_url, starts_at, ends_at, timezone, status, rrule)
			VALUES ($1, $2, $3, E'\n  Bring the numbers \nand the deck', 'Room 4', 'https://meet.example/abc', $4, $5, 'America/Los_Angeles', $6, $7) RETURNING id`,
			owner, cal, title, start, start.Add(time.Hour), status, rrule).Scan(&id))
		return id
	}
	review := event(primary, "Salary review", "confirmed", nil)
	dentist := event(private, "Dentist", "confirmed", nil)
	gone := event(primary, "Cancelled sync", "cancelled", nil)
	weekly := "FREQ=WEEKLY;BYDAY=MO"
	series := event(primary, "Weekly sync", "confirmed", &weekly)
	skipped := start.Add(7 * 24 * time.Hour)
	_, err = pool.Exec(ctx, `
		INSERT INTO calendar_events (owner_id, calendar_id, recurring_event_id, original_start, title, starts_at, ends_at, timezone, status)
		VALUES ($1, $2, $3, $4, 'Weekly sync', $4, $5, 'America/Los_Angeles', 'cancelled')`, owner, primary, series, skipped, skipped.Add(time.Hour))
	must(err)
	_, err = pool.Exec(ctx, `INSERT INTO calendar_attendees (event_id, user_id) VALUES ($1, $2)`, review, guest)
	must(err)

	as := func(u uuid.UUID) *auth.Identity { return &auth.Identity{UserID: u} }
	full := func(name string, viewer uuid.UUID) {
		t.Helper()
		got, err := EventRef(ctx, pool, as(viewer), review, nil)
		if err != nil || got.Busy || got.Title != "Salary review" || got.Location != "Room 4" || got.Snippet != "Bring the numbers" ||
			got.Organizer != "Sam" || got.When != "Mon, Nov 9, 2026 · 10:00 AM – 11:00 AM PST" || got.Path != "/calendar/event/"+review.String() ||
			got.Occurrence != nil || got.UpdatedAt == nil || got.Repeat != "" {
			t.Fatalf("%s's card: %+v, %v", name, got, err)
		}
	}
	full("owner", owner)
	full("guest", guest)
	full("all-details reader", reader)

	// Free/busy: the time in their own zone, and nothing about the event.
	got, err := EventRef(ctx, pool, as(colleague), review, nil)
	if err != nil || !got.Busy || got.Title != "Busy" || got.When != "Mon, Nov 9, 2026 · 6:00 PM – 7:00 PM GMT" ||
		got.Location != "" || got.Snippet != "" || got.Organizer != "" || got.Repeat != "" || got.UpdatedAt != nil || got.EventType != TypeDefault ||
		got.Path != "/calendar?view=day&date=2026-11-09" {
		t.Fatalf("free/busy card: %+v, %v", got, err)
	}

	// A series: the occurrence asked for, with how it repeats.
	occ := start
	if got, err := EventRef(ctx, pool, as(owner), series, &occ); err != nil || got.Occurrence == nil || !got.Occurrence.Equal(start) ||
		got.Repeat != "Every week on Monday" || got.Path != "/calendar/event/"+series.String()+"?occurrence="+url.QueryEscape("2026-11-09T18:00:00Z") {
		t.Fatalf("occurrence card: %+v, %v", got, err)
	}

	tuesday, cancelledOcc := start.Add(24*time.Hour), skipped
	for name, c := range map[string]struct {
		viewer uuid.UUID
		id     uuid.UUID
		occ    *time.Time
	}{
		"free/busy viewer, unshared calendar": {colleague, dentist, nil},
		"outsider":                            {outsider, review, nil},
		"cancelled event":                     {owner, gone, nil},
		"occurrence outside the series":       {owner, series, &tuesday},
		"cancelled occurrence":                {owner, series, &cancelledOcc},
		"missing":                             {owner, uuid.New(), nil},
	} {
		if got, err := EventRef(ctx, pool, as(c.viewer), c.id, c.occ); !errors.Is(err, ErrEventNotFound) {
			t.Errorf("%s: %+v, %v; want ErrEventNotFound", name, got, err)
		}
	}

	// Agents: without a policy they read what the person reads; with the
	// person's calendar scope off they get nothing.
	agentCtx := auth.WithAgent(ctx, auth.Agent{Via: "mcp", Client: "Test"})
	if _, err := EventRef(agentCtx, pool, as(owner), review, nil); err != nil {
		t.Fatalf("agent, no policy: %v", err)
	}
	calendarService.Store(&Service{d: &app.Deps{Pool: pool, Modules: []app.Module{offPolicy{}}}})
	if got, err := EventRef(agentCtx, pool, as(owner), review, nil); !errors.Is(err, ErrEventNotFound) {
		t.Fatalf("agent with calendar off: %+v, %v", got, err)
	}
	if got, err := EventRef(agentCtx, pool, as(colleague), review, nil); !errors.Is(err, ErrEventNotFound) {
		t.Fatalf("agent with calendar off, free/busy: %+v, %v", got, err)
	}
	if _, err := EventRef(ctx, pool, as(owner), review, nil); err != nil {
		t.Fatalf("the owner still reads it: %v", err)
	}
}
