package calendar

import (
	"strings"
	"testing"
	"time"

	"scaffoldapp/backend/internal/app"
)

func TestExternalTargetStaysInAgentIDRules(t *testing.T) {
	conn := "6f1c0f1e-8a7b-4c62-9d55-1f2a3b4c5d6e"
	got := externalTarget(conn, "en.usa#holiday@group.v.calendar.google.com", "abc_20261102T170000Z")
	if strings.ContainsAny(got, "#@ ") || !strings.HasPrefix(got, conn+":") {
		t.Fatalf("unsafe target %q", got)
	}
	long := strings.Repeat("A", 160) + "="
	a := externalTarget(conn, long, long+"1")
	b := externalTarget(conn, long, long+"2")
	if len(a) > 200 || len(b) > 200 || a == b {
		t.Fatalf("long ids must stay distinct and short: %q %q", a, b)
	}
}

func TestSubtractSpan(t *testing.T) {
	at := func(h int) time.Time { return time.Date(2026, 11, 2, h, 0, 0, 0, time.UTC) }
	got := subtractSpan([]busySpan{{at(9), at(12)}, {at(14), at(15)}}, at(10), at(11))
	if len(got) != 3 || !got[0].End.Equal(at(10)) || !got[1].Start.Equal(at(11)) {
		t.Fatalf("got %+v", got)
	}
}

func TestGoogleEventMapping(t *testing.T) {
	g := &googleCalendar{conn: &calConn{Connection: app.Connection{ID: "c", Provider: "google", Account: "sam@acme.test"}, writeCap: CapGoogleWrite}}
	cal := extCalendar{ID: "sam@acme.test", CanWrite: true}
	allDay := g.toExt(gEvent{ID: "x", Summary: "Offsite", Start: gTime{Date: "2026-11-07"}, End: gTime{Date: "2026-11-09"}, Transparency: "transparent",
		Organizer: gPerson{Email: "sam@acme.test", Self: true}}, cal, "America/Los_Angeles")
	if !allDay.AllDay || allDay.StartDate != "2026-11-07" || allDay.EndDate != "2026-11-08" || allDay.ShowAs != "free" || !allDay.CanEdit {
		t.Fatalf("all-day: %+v", allDay)
	}
	inv := g.toExt(gEvent{ID: "y", Summary: "Plan", Location: "https://acme.zoom.us/j/81234567890", Start: gTime{DateTime: "2026-11-04T11:00:00-08:00"},
		End: gTime{DateTime: "2026-11-04T12:00:00-08:00"}, Organizer: gPerson{Email: "ceo@acme.test"},
		Attendees: []gAttendee{{Email: "sam@acme.test", Self: true, ResponseStatus: "needsAction"}}}, cal, "")
	if inv.MyResponse != ResponseNeedsAction || inv.ConferenceProvider != ProviderZoom || inv.CanEdit || !inv.Start.Equal(time.Date(2026, 11, 4, 19, 0, 0, 0, time.UTC)) {
		t.Fatalf("invitation: %+v", inv)
	}
}

func TestGraphTimesAndRecurrence(t *testing.T) {
	v, ok := parseGraphTime(msTime{DateTime: "2026-11-02T17:00:00.0000000", TimeZone: "UTC"})
	if !ok || !v.Equal(time.Date(2026, 11, 2, 17, 0, 0, 0, time.UTC)) {
		t.Fatalf("utc: %v", v)
	}
	loc := mustLoc(t, "America/Los_Angeles")
	r := Rule{Freq: "MONTHLY", Interval: 1, ByDay: []WeekdayNum{{Day: time.Tuesday, N: 2}}, Count: 5}
	rec, err := graphRecurrence(r, time.Date(2026, 11, 10, 18, 0, 0, 0, time.UTC), loc, "Pacific Standard Time")
	if err != nil {
		t.Fatal(err)
	}
	p := rec["pattern"].(map[string]any)
	if p["type"] != "relativeMonthly" || p["index"] != "second" || rec["range"].(map[string]any)["numberOfOccurrences"] != 5 {
		t.Fatalf("recurrence: %+v", rec)
	}
}
