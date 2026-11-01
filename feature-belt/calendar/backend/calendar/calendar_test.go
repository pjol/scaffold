package calendar

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	l, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestWeeklyKeepsWallClockAcrossDST(t *testing.T) {
	ny := mustLoc(t, "America/New_York")
	r, err := ParseRule("FREQ=WEEKLY;BYDAY=MO,FR", ny)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 26, 10, 0, 0, 0, ny) // Monday, EDT
	got := r.Expand(start, ny, start, start.AddDate(0, 0, 14), 10, nil)
	if len(got) != 4 {
		t.Fatalf("want 4 occurrences, got %d: %v", len(got), got)
	}
	for _, g := range got {
		if h := g.In(ny).Hour(); h != 10 {
			t.Errorf("%v is %d:00 local, want 10:00", g, h)
		}
	}
	// Mon Oct 26 is UTC-4 (14:00Z); Mon Nov 2 is UTC-5 (15:00Z).
	if got[0].UTC().Hour() != 14 || got[2].UTC().Hour() != 15 {
		t.Errorf("UTC hours across the change: %v", got)
	}
}

func TestMonthlyByDateSkipsShortMonths(t *testing.T) {
	r, _ := ParseRule("FREQ=MONTHLY;COUNT=4", time.UTC)
	got := r.Expand(time.Date(2026, 1, 31, 9, 0, 0, 0, time.UTC), time.UTC, time.Time{}, time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC), 10, nil)
	want := []string{"2026-01-31", "2026-03-31", "2026-05-31", "2026-07-31"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i, g := range got {
		if g.Format(time.DateOnly) != want[i] {
			t.Errorf("occurrence %d = %s, want %s", i, g.Format(time.DateOnly), want[i])
		}
	}
}

func TestMonthlyNthWeekday(t *testing.T) {
	r, err := ParseRule("FREQ=MONTHLY;BYDAY=2TU,-1FR", time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC) // 2nd Tuesday of Nov 2026
	got := r.Expand(start, time.UTC, start, time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), 10, nil)
	want := []string{"2026-11-10", "2026-11-27", "2026-12-08", "2026-12-25"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i, g := range got {
		if g.Format(time.DateOnly) != want[i] {
			t.Errorf("occurrence %d = %s, want %s", i, g.Format(time.DateOnly), want[i])
		}
	}
}

func TestUntilIntervalAndExclude(t *testing.T) {
	la := mustLoc(t, "America/Los_Angeles")
	r, err := ParseRule("FREQ=DAILY;INTERVAL=2;UNTIL=20261110", la)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 11, 2, 8, 0, 0, 0, la)
	skip := map[int64]bool{time.Date(2026, 11, 6, 8, 0, 0, 0, la).Unix(): true}
	got := r.Expand(start, la, start, start.AddDate(1, 0, 0), 20, skip)
	if len(got) != 4 { // 2, 4, (6 skipped), 8, 10
		t.Fatalf("got %d: %v", len(got), got)
	}
	last, ok := r.Last(start, la)
	if !ok || last.In(la).Day() != 10 {
		t.Errorf("last = %v", last)
	}
	if n := r.CountBefore(start, la, time.Date(2026, 11, 8, 8, 0, 0, 0, la)); n != 3 {
		t.Errorf("CountBefore = %d, want 3", n)
	}
	if s := r.String(false, la); !strings.Contains(s, "UNTIL=2026111") || !strings.Contains(s, "INTERVAL=2") {
		t.Errorf("String() = %s", s)
	}
}

func TestParseRuleRejectsOutsideSubset(t *testing.T) {
	for _, s := range []string{"FREQ=HOURLY", "FREQ=WEEKLY;BYSETPOS=1", "FREQ=WEEKLY;COUNT=2;UNTIL=20270101", "INTERVAL=2", "FREQ=WEEKLY;BYDAY=2MO"} {
		if _, err := ParseRule(s, time.UTC); err == nil {
			t.Errorf("%q: want an error", s)
		}
	}
}

const sampleICS = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:test\r\n" +
	"BEGIN:VEVENT\r\nUID:a@x\r\nDTSTART;TZID=America/New_York:20261026T100000\r\nDTEND;TZID=America/New_York:20261026T110000\r\n" +
	"RRULE:FREQ=WEEKLY;COUNT=4\r\nEXDATE;TZID=America/New_York:20261102T100000\r\nSUMMARY:Weekly\r\nEND:VEVENT\r\n" +
	"BEGIN:VEVENT\r\nUID:a@x\r\nRECURRENCE-ID;TZID=America/New_York:20261109T100000\r\nDTSTART;TZID=America/New_York:20261109T130000\r\n" +
	"DTEND;TZID=America/New_York:20261109T140000\r\nSUMMARY:Moved\r\nEND:VEVENT\r\n" +
	"BEGIN:VEVENT\r\nUID:b@x\r\nDTSTART:20261028T170000Z\r\nDURATION:PT30M\r\nSUMMARY:Single\r\nEND:VEVENT\r\n" +
	"BEGIN:VEVENT\r\nUID:c@x\r\nDTSTART;VALUE=DATE:20261030\r\nSUMMARY:Holiday\r\nEND:VEVENT\r\n" +
	"BEGIN:VEVENT\r\nUID:d@x\r\nDTSTART:20261029T170000Z\r\nDTEND:20261029T180000Z\r\nTRANSP:TRANSPARENT\r\nSUMMARY:Free\r\nEND:VEVENT\r\n" +
	"BEGIN:VEVENT\r\nUID:e@x\r\nDTSTART:20261029T190000Z\r\nDTEND:20261029T200000Z\r\nSTATUS:CANCELLED\r\nSUMMARY:Off\r\nEND:VEVENT\r\n" +
	"END:VCALENDAR\r\n"

func TestICalBusy(t *testing.T) {
	from := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	spans, err := icalBusy([]byte(sampleICS), time.UTC, from, to, 100)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range spans {
		got = append(got, s.Start.Format("01-02T15:04")+"/"+s.End.Format("15:04"))
	}
	// Oct 26 14:00Z (EDT), Oct 28 single, Nov 2 excluded, Nov 9 moved to 13:00 EST = 18:00Z, Nov 16 15:00Z.
	want := []string{"10-26T14:00/15:00", "10-28T17:00/17:30", "11-09T18:00/19:00", "11-16T15:00/16:00"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("busy = %v\nwant   %v", got, want)
	}
}

func TestRenderICSRoundTrip(t *testing.T) {
	ny := mustLoc(t, "America/New_York")
	rule := "FREQ=WEEKLY;BYDAY=MO"
	ev := icsEvent{UID: "x@calendar.test", Start: time.Date(2026, 10, 26, 10, 0, 0, 0, ny), End: time.Date(2026, 10, 26, 11, 0, 0, 0, ny),
		Loc: ny, Rule: rule, Summary: strings.Repeat("Long title, with; specials ", 6), Organizer: &icsPerson{Name: "Sam", Email: "sam@example.com"},
		Attendees: []icsPerson{{Name: "Alex", Email: "alex@example.com", Response: "needs_action", RSVP: true}}}
	out := string(renderICS("REQUEST", "", []icsEvent{ev}))
	for _, l := range strings.Split(out, "\r\n") {
		if len(l) > 75 {
			t.Errorf("unfolded line over 75 octets: %q", l)
		}
	}
	for _, want := range []string{"METHOD:REQUEST", "BEGIN:VTIMEZONE", "TZID:America/New_York", "BEGIN:DAYLIGHT", "BEGIN:STANDARD",
		"DTSTART;TZID=America/New_York:20261026T100000", "RRULE:" + rule, "PARTSTAT=NEEDS-ACTION", "ORGANIZER;CN=\"Sam\":mailto:sam@example.com"} {
		if !strings.Contains(strings.ReplaceAll(out, "\r\n ", ""), want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	cal, err := parseICal([]byte(out))
	if err != nil {
		t.Fatal(err)
	}
	spans, err := icalBusy([]byte(out), time.UTC, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC), 10)
	if err != nil || len(cal.Children) < 2 {
		t.Fatal(err)
	}
	if len(spans) != 3 || spans[1].Start.UTC().Hour() != 15 {
		t.Errorf("round trip spans = %v", spans)
	}
}

func TestSlotsAcrossDSTWithBuffersAndOverrides(t *testing.T) {
	ny := mustLoc(t, "America/New_York")
	var weekly []HoursInterval
	for d := 1; d <= 5; d++ {
		weekly = append(weekly, HoursInterval{Day: d, Start: 9 * 60, End: 12 * 60})
	}
	busyStart := time.Date(2026, 10, 30, 10, 0, 0, 0, ny) // Friday 10:00–10:30 EDT
	busy := []busyItem{{Start: busyStart, End: busyStart.Add(30 * time.Minute), BlockStart: busyStart, BlockEnd: busyStart.Add(30 * time.Minute)}}
	r := slotRules{
		loc: ny, weekly: weekly, duration: 30 * time.Minute, interval: 30 * time.Minute, after: 15 * time.Minute,
		overrides: map[string][]TimeInterval{"2026-11-03": {}}, // Tuesday off
		earliest:  time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC),
	}
	slots := generateSlots(r, time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC), busy, 100)
	byDay := map[string][]string{}
	for _, s := range slots {
		byDay[s.StartsAt.In(ny).Format(time.DateOnly)] = append(byDay[s.StartsAt.In(ny).Format(time.DateOnly)], s.StartsAt.UTC().Format("15:04"))
	}
	// Friday (EDT, UTC-4): 9:00 open (buffer ends 9:45), 9:30 blocked (its buffer runs into 10:00), 10:00 busy, 10:30… open.
	if got := strings.Join(byDay["2026-10-30"], ","); got != "13:00,14:30,15:00,15:30" {
		t.Errorf("Friday slots (UTC) = %s", got)
	}
	// Monday after the change (EST, UTC-5): 9:00 local = 14:00Z.
	if got := byDay["2026-11-02"]; len(got) != 6 || got[0] != "14:00" {
		t.Errorf("Monday slots (UTC) = %v", got)
	}
	if len(byDay["2026-11-03"]) != 0 {
		t.Errorf("Tuesday override should block the day, got %v", byDay["2026-11-03"])
	}
}

func TestConflictsBuffersMayOverlap(t *testing.T) {
	base := time.Date(2026, 11, 2, 15, 0, 0, 0, time.UTC)
	// An earlier booking 15:00–15:30 with a 15-minute after-buffer.
	busy := []busyItem{{Start: base, End: base.Add(30 * time.Minute), BlockStart: base, BlockEnd: base.Add(45 * time.Minute)}}
	at := func(m int) (time.Time, time.Time) {
		s := base.Add(time.Duration(m) * time.Minute)
		return s, s.Add(30 * time.Minute)
	}
	s, e := at(30)
	if !conflicts(s, e, s, e, busy) {
		t.Error("15:30 starts inside the other booking's after-buffer")
	}
	s, e = at(45)
	if conflicts(s, e, s.Add(-15*time.Minute), e, busy) {
		t.Error("15:45 with a 15-minute before-buffer may overlap the other buffer")
	}
}

func TestShiftSeriesKeepsLocalTime(t *testing.T) {
	ny := mustLoc(t, "America/New_York")
	base := time.Date(2026, 10, 26, 10, 0, 0, 0, ny) // EDT
	inst := time.Date(2026, 11, 9, 10, 0, 0, 0, ny)  // EST
	moved := time.Date(2026, 11, 9, 11, 0, 0, 0, ny)
	s, e := shiftSeries(base, inst, moved, moved.Add(time.Hour), ny, ny, false)
	if s.In(ny).Hour() != 11 || s.In(ny).Day() != 26 || e.Sub(s) != time.Hour {
		t.Errorf("shifted to %v – %v", s.In(ny), e.In(ny))
	}
}

func TestCheckFeedURL(t *testing.T) {
	for _, bad := range []string{"http://127.0.0.1/x.ics", "http://localhost:8000/a.ics", "http://169.254.169.254/latest", "http://[::1]/a",
		"http://10.0.0.5/a.ics", "ftp://example.com/a.ics", "https://user:pw@example.com/a.ics", "http://intranet/a.ics"} {
		if _, err := checkFeedURL(bad, false); err == nil {
			t.Errorf("%s: want refused", bad)
		}
	}
	u, err := checkFeedURL("webcal://calendar.example.com/private/abc/basic.ics", false)
	if err != nil || u.Scheme != "https" {
		t.Errorf("webcal → %v, %v", u, err)
	}
	if _, err := checkFeedURL("http://127.0.0.1:9000/a.ics", true); err != nil {
		t.Errorf("development should allow local addresses: %v", err)
	}
}

// An agent's create call is previewed in the reviewer's zone, with the
// zone named, on both sides of the US daylight-saving change (Nov 1, 2026).
func TestCreateSummaryUsesReviewerZoneAcrossDST(t *testing.T) {
	la := mustLoc(t, "America/Los_Angeles")
	at := func(s string) *time.Time {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return &v
	}
	cases := []struct {
		start, end, want string
	}{
		{"2026-10-30T15:00:00Z", "2026-10-30T16:00:00Z", "Add “Design review” Fri, Oct 30, 2026 · 8:00 AM – 9:00 AM PDT"},
		{"2026-11-02T15:00:00Z", "2026-11-02T16:00:00Z", "Add “Design review” Mon, Nov 2, 2026 · 7:00 AM – 8:00 AM PST with 2 colleagues"},
	}
	for i, c := range cases {
		in := createPreview{Title: " Design review ", StartsAt: at(c.start), EndsAt: at(c.end)}
		if i == 1 {
			in.AttendeeIDs = make([]uuid.UUID, 2)
		}
		if got := createSummary(in, la); got != c.want {
			t.Errorf("createSummary(%s) = %q, want %q", c.start, got, c.want)
		}
	}
	// Without a zone of their own the reviewer still reads a named zone.
	utc := createSummary(createPreview{Title: "Sync", StartsAt: at("2026-11-02T15:00:00Z"), EndsAt: at("2026-11-02T15:30:00Z")}, time.UTC)
	if !strings.HasSuffix(utc, "3:00 PM – 3:30 PM UTC") {
		t.Errorf("UTC fallback = %q", utc)
	}
}

// All-day events keep their own dates whoever reads them.
func TestCreateSummaryAllDay(t *testing.T) {
	got := createSummary(createPreview{Title: "Offsite", AllDay: true, StartDate: "2026-11-02", EndDate: "2026-11-03"}, mustLoc(t, "Asia/Tokyo"))
	if want := "Add “Offsite” Mon, Nov 2 – Tue, Nov 3, 2026 (all day)"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	one := createSummary(createPreview{Title: "Holiday", AllDay: true, StartDate: "2026-11-26"}, mustLoc(t, "America/Los_Angeles"))
	if want := "Add “Holiday” Thu, Nov 26, 2026 (all day)"; one != want {
		t.Errorf("got %q, want %q", one, want)
	}
}

// Notification wording names the reader's zone and follows its offset.
func TestFormatWhenAcrossDST(t *testing.T) {
	ny := mustLoc(t, "America/New_York")
	before := formatWhen(time.Date(2026, 10, 30, 14, 0, 0, 0, time.UTC), time.Date(2026, 10, 30, 15, 0, 0, 0, time.UTC), false, ny)
	after := formatWhen(time.Date(2026, 11, 2, 14, 0, 0, 0, time.UTC), time.Date(2026, 11, 2, 15, 0, 0, 0, time.UTC), false, ny)
	if before != "Fri, Oct 30, 2026 · 10:00 AM – 11:00 AM EDT" || after != "Mon, Nov 2, 2026 · 9:00 AM – 10:00 AM EST" {
		t.Errorf("before %q, after %q", before, after)
	}
}

// The review of an agent's create shows the whole event: when, how it
// repeats, where, guests by name, then the description.
func TestCreateDetails(t *testing.T) {
	la := mustLoc(t, "America/Los_Angeles")
	start, end := time.Date(2026, 11, 2, 15, 0, 0, 0, time.UTC), time.Date(2026, 11, 2, 16, 0, 0, 0, time.UTC)
	in := createPreview{Title: "Design review", StartsAt: &start, EndsAt: &end, Location: "Room 4",
		Description: "Bring the mocks.", Recurrence: &Recurrence{Freq: "weekly", ByDay: []string{"MO"}}}
	got := createDetails(in, la, la, []string{"Jordan Lee", "Sam Park"})
	want := "Design review\nWhen: Mon, Nov 2, 2026 · 7:00 AM – 8:00 AM PST\nRepeats: Every week on Monday\nWhere: Room 4\nGuests: Jordan Lee, Sam Park\n\nBring the mocks."
	if got != want {
		t.Errorf("createDetails = %q, want %q", got, want)
	}
}

// Public calendar routes never take an AI agent's credential.
func TestPublicRoutesRefuseAgentCredential(t *testing.T) {
	h := refuseAgentCredential(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	for _, c := range []struct {
		header string
		want   int
	}{{"", http.StatusNoContent}, {"Bearer dev:sam:sam@x.test", http.StatusNoContent}, {"Bearer agt_x_key_abc", http.StatusUnauthorized}} {
		req := httptest.NewRequest(http.MethodGet, "/calendar/feed/t.ics", nil)
		if c.header != "" {
			req.Header.Set("Authorization", c.header)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Errorf("%q: status %d, want %d", c.header, rec.Code, c.want)
		}
	}
}
