package calendar

import (
	"context"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/config"
)

func TestPlatformGuests(t *testing.T) {
	e := extEvent{Organizer: extAttendee{Email: "Owner@Client.test"}, Attendees: []extAttendee{
		{Email: "owner@client.test"}, {Email: "me@acme.test", Self: true}, {Email: "room-4@resource.test", Resource: true}, {Email: "dana@client.test"}, {Email: ""},
	}}
	if got := platformGuests(e); !slices.Equal(got, []string{"Owner@Client.test", "dana@client.test"}) {
		t.Fatalf("guests = %v", got)
	}
	e.OrganizerSelf = true
	if got := platformGuests(e); !slices.Equal(got, []string{"owner@client.test", "dana@client.test"}) {
		t.Fatalf("guests, organizing myself = %v", got)
	}
}

// MeetingsWith lists the caller's own events (organized or accepted) with
// any of the addresses, in range, each occurrence of a series once; another
// person's events never appear, even on a calendar shared with the caller at
// "all details", and neither do declined invitations or events without the
// guest. PublicBookingLinks lists only links anyone can book.
func TestMeetingsWithAndPublicLinks(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	registerAccessKinds()
	prev := calendarService.Load()
	calendarService.Store(&Service{d: &app.Deps{Pool: pool, Cfg: &config.Config{Env: "development", PublicWebURL: "http://web.test"}}})
	t.Cleanup(func() { calendarService.Store(prev) })
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		_, err := pool.Exec(ctx, sql, args...)
		must(err)
	}
	user := func(name string) uuid.UUID {
		var id uuid.UUID
		must(pool.QueryRow(ctx, `INSERT INTO users (auth_subject, email, display_name) VALUES ($1, $1 || '@acme.test', $2) RETURNING id`,
			"seam_"+strings.ToLower(name), name).Scan(&id))
		return id
	}
	sam, pat, cole := user("Sam"), user("Pat"), user("Cole")
	var org, other uuid.UUID
	must(pool.QueryRow(ctx, `INSERT INTO orgs (name, slug) VALUES ('Acme', 'acme-seams') RETURNING id`).Scan(&org))
	must(pool.QueryRow(ctx, `INSERT INTO orgs (name, slug) VALUES ('Other', 'other-seams') RETURNING id`).Scan(&other))
	for _, u := range []uuid.UUID{sam, pat, cole} {
		exec(`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'member')`, org, u)
	}
	exec(`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'member')`, other, sam)
	primary := func(owner uuid.UUID) uuid.UUID {
		var id uuid.UUID
		must(pool.QueryRow(ctx, `INSERT INTO calendar_calendars (owner_id, org_id, name, is_primary) VALUES ($1, $2, 'Primary', true) RETURNING id`, owner, org).Scan(&id))
		must(access.CreateResource(ctx, pool, access.NewResource{Kind: KindCalendar, ID: id, OrgID: org, OwnerID: owner, General: access.View}))
		return id
	}
	samCal, patCal := primary(sam), primary(pat)
	// Pat shares their calendar with Sam at "all details": still not Sam's meetings.
	must(access.AddGrant(ctx, pool, access.Ref{Kind: KindCalendar, ID: patCal}, access.SubjectRef{Type: "user", ID: sam}, access.Comment, pat))

	start := time.Date(2026, 11, 9, 18, 0, 0, 0, time.UTC)
	event := func(owner, cal uuid.UUID, title string, at time.Time, rrule *string) uuid.UUID {
		var id uuid.UUID
		must(pool.QueryRow(ctx, `
			INSERT INTO calendar_events (owner_id, calendar_id, title, location, starts_at, ends_at, timezone, rrule)
			VALUES ($1, $2, $3, 'Room 4', $4, $5, 'America/Los_Angeles', $6) RETURNING id`, owner, cal, title, at, at.Add(time.Hour), rrule).Scan(&id))
		return id
	}
	guest := func(event uuid.UUID, email string) {
		exec(`INSERT INTO calendar_attendees (event_id, email) VALUES ($1, $2)`, event, email)
	}
	invite := func(event, u uuid.UUID, response string) {
		exec(`INSERT INTO calendar_attendees (event_id, user_id, response) VALUES ($1, $2, $3)`, event, u, response)
	}
	kickoff := event(sam, samCal, "Kickoff with Dana", start, nil)
	guest(kickoff, "dana@client.test")
	invite(kickoff, cole, "accepted")
	review := event(pat, patCal, "Pat's review with Dana", start.Add(2*time.Hour), nil)
	guest(review, "dana@client.test")
	invite(review, sam, "accepted")
	private := event(pat, patCal, "Pat and Dana alone", start.Add(4*time.Hour), nil)
	guest(private, "dana@client.test")
	declined := event(pat, patCal, "Declined with Dana", start.Add(5*time.Hour), nil)
	guest(declined, "dana@client.test")
	invite(declined, sam, "declined")
	internal := event(sam, samCal, "Team sync", start.Add(6*time.Hour), nil)
	invite(internal, cole, "accepted")
	later := event(sam, samCal, "Next month with Dana", start.Add(40*24*time.Hour), nil)
	guest(later, "dana@client.test")
	weekly := "FREQ=WEEKLY;BYDAY=MO"
	series := event(sam, samCal, "Weekly with Dana", start.Add(-7*24*time.Hour+time.Hour), &weekly)
	guest(series, "dana@client.test")

	as := &auth.Identity{UserID: sam}
	from, to := start.Add(-8*24*time.Hour), start.Add(24*time.Hour)
	got, err := MeetingsWith(ctx, as, org, []string{" DANA@client.test ", "nobody@client.test"}, from, to)
	must(err)
	var titles []string
	byTitle := map[string][]GuestMeeting{}
	for _, m := range got {
		titles = append(titles, m.Title)
		byTitle[m.Title] = append(byTitle[m.Title], m)
	}
	want := []string{"Weekly with Dana", "Kickoff with Dana", "Weekly with Dana", "Pat's review with Dana"}
	if !slices.Equal(titles, want) {
		t.Fatalf("meetings = %v, want %v", titles, want)
	}
	k := byTitle["Kickoff with Dana"][0]
	guests := slices.Clone(k.Guests)
	sort.Strings(guests)
	if k.ID != kickoff.String() || !slices.Equal(guests, []string{"dana@client.test", "seam_cole@acme.test"}) ||
		k.Location != "Room 4" || k.Source != "" || k.Link != "/calendar/event/"+kickoff.String() || !k.StartsAt.Equal(start) {
		t.Fatalf("kickoff = %+v", k)
	}
	if r := byTitle["Pat's review with Dana"][0]; !slices.Contains(r.Guests, "seam_pat@acme.test") || slices.Contains(r.Guests, "seam_sam@acme.test") {
		t.Fatalf("an invitation lists its organizer and not the caller: %+v", r)
	}
	w := byTitle["Weekly with Dana"]
	if w[0].ID == w[1].ID || !strings.HasPrefix(w[0].ID, series.String()+"@") || !strings.Contains(w[1].Link, "?occurrence=") {
		t.Fatalf("series occurrences = %+v", w)
	}

	// Pat's own view has Pat's events, never Sam's.
	patGot, err := MeetingsWith(ctx, &auth.Identity{UserID: pat}, org, []string{"dana@client.test"}, from, to)
	must(err)
	for _, m := range patGot {
		if m.Title == "Kickoff with Dana" || m.Title == "Weekly with Dana" {
			t.Fatalf("Pat sees Sam's meeting %q", m.Title)
		}
	}
	if none, err := MeetingsWith(ctx, as, org, []string{" "}, from, to); err != nil || len(none) != 0 {
		t.Fatalf("no addresses: %v, %v", none, err)
	}
	agentCtx := auth.WithAgent(ctx, auth.Agent{Via: "mcp", Client: "Test"})
	calendarService.Store(&Service{d: &app.Deps{Pool: pool, Cfg: &config.Config{Env: "development", PublicWebURL: "http://web.test"}, Modules: []app.Module{offPolicy{}}}})
	if hidden, err := MeetingsWith(agentCtx, as, org, []string{"dana@client.test"}, from, to); err != nil || len(hidden) != 0 {
		t.Fatalf("agent with calendar off: %v, %v", hidden, err)
	}

	// Booking links.
	calendarService.Store(&Service{d: &app.Deps{Pool: pool, Cfg: &config.Config{Env: "development", PublicWebURL: "http://web.test/"}}})
	var team, otherTeam uuid.UUID
	must(pool.QueryRow(ctx, `INSERT INTO access_teams (org_id, name, handle) VALUES ($1, 'Sales', 'sales') RETURNING id`, org).Scan(&team))
	must(pool.QueryRow(ctx, `INSERT INTO access_teams (org_id, name, handle) VALUES ($1, 'Ops', 'ops') RETURNING id`, other).Scan(&otherTeam))
	link := func(owner uuid.UUID, orgID *uuid.UUID, audience, slug string, extra string, teamID *uuid.UUID) uuid.UUID {
		var id uuid.UUID
		kind := "personal"
		if teamID != nil {
			kind = "round_robin"
		}
		must(pool.QueryRow(ctx, `
			INSERT INTO calendar_booking_links (owner_id, org_id, audience, slug, title, durations, location_type, timezone, kind, team_id)
			VALUES ($1, $2, $3, $4, $4, '{30}', 'video', 'UTC', $5, $6) RETURNING id`, owner, orgID, audience, slug, kind, teamID).Scan(&id))
		if extra != "" {
			exec(`UPDATE calendar_booking_links SET `+extra+` WHERE id = $1`, id)
		}
		return id
	}
	link(sam, nil, "external", "sam-intro", "", nil)
	link(sam, &other, "external", "sam-other-org", "", nil) // personal: theirs in any workspace
	link(sam, &org, "internal", "sam-members", "", nil)
	link(sam, nil, "external", "sam-paused", "active = false", nil)
	link(sam, nil, "external", "sam-deleted", "deleted_at = now()", nil)
	link(sam, nil, "external", "sam-used-once", "single_use = true, used_at = now()", nil)
	link(sam, nil, "external", "sam-once", "single_use = true", nil)
	link(pat, nil, "external", "pat-intro", "", nil)
	hosted := link(pat, &org, "external", "sales-demo", "", &team)
	exec(`INSERT INTO calendar_link_hosts (link_id, user_id) VALUES ($1, $2)`, hosted, sam)
	link(pat, &org, "external", "sales-unhosted", "", &team)
	link(sam, &other, "external", "ops-demo", "", &otherTeam)
	link(sam, &org, "internal", "sales-internal", "", &team)

	links, err := PublicBookingLinks(ctx, as, org)
	must(err)
	var urls []string
	for _, l := range links {
		urls = append(urls, l.URL)
	}
	sort.Strings(urls)
	wantURLs := []string{"http://web.test/book/sales-demo", "http://web.test/book/sam-intro", "http://web.test/book/sam-once", "http://web.test/book/sam-other-org"}
	if !slices.Equal(urls, wantURLs) {
		t.Fatalf("public links = %v, want %v", urls, wantURLs)
	}
	calendarService.Store(&Service{d: &app.Deps{Pool: pool, Cfg: &config.Config{Env: "development", PublicWebURL: "http://web.test"}, Modules: []app.Module{offPolicy{}}}})
	if hidden, err := PublicBookingLinks(agentCtx, as, org); err != nil || len(hidden) != 0 {
		t.Fatalf("agent with calendar off: %v, %v", hidden, err)
	}
	calendarService.Store(&Service{d: &app.Deps{Pool: pool, Cfg: &config.Config{Env: "development"}}})
	if none, err := PublicBookingLinks(ctx, as, org); err != nil || len(none) != 0 {
		t.Fatalf("without PUBLIC_WEB_URL: %v, %v", none, err)
	}
}
