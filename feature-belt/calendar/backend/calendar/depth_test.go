package calendar

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/access"
)

func weekdays(start, end int) []HoursInterval {
	var out []HoursInterval
	for d := 1; d <= 5; d++ {
		out = append(out, HoursInterval{Day: d, Start: start, End: end})
	}
	return out
}

func TestPickRoundRobinPriorityThenWeightThenRecency(t *testing.T) {
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	t0 := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	// Higher priority wins even with more bookings.
	got, _ := pickRoundRobin([]hostAvail{{ID: a, Priority: 2, Weight: 100, Count: 0}, {ID: b, Priority: 3, Weight: 100, Count: 9}})
	if got.ID != b {
		t.Fatalf("priority: got %v, want b", got.ID)
	}
	// Same priority: fewest bookings for their weight. b has weight 200 and
	// 3 bookings (1.5 per 100) vs a's 2 per 100.
	got, _ = pickRoundRobin([]hostAvail{{ID: a, Priority: 2, Weight: 100, Count: 2}, {ID: b, Priority: 2, Weight: 200, Count: 3}})
	if got.ID != b {
		t.Fatalf("weight: got %v, want b", got.ID)
	}
	// Equal share: the least recently booked (never booked first).
	got, _ = pickRoundRobin([]hostAvail{
		{ID: a, Priority: 2, Weight: 100, Count: 1, LastBooked: t0.Add(time.Hour)},
		{ID: b, Priority: 2, Weight: 100, Count: 1, LastBooked: t0},
		{ID: c, Priority: 2, Weight: 100, Count: 1, LastBooked: t0.Add(2 * time.Hour)},
	})
	if got.ID != b {
		t.Fatalf("recency: got %v, want b", got.ID)
	}
	if _, ok := pickRoundRobin(nil); ok {
		t.Fatal("no free host must pick nobody")
	}
	// Rotation: booking the picked host moves the next booking on.
	pool := []hostAvail{{ID: a, Priority: 2, Weight: 100}, {ID: b, Priority: 2, Weight: 100}, {ID: c, Priority: 2, Weight: 100}}
	seen := map[uuid.UUID]int{}
	for i := 0; i < 9; i++ {
		h, _ := pickRoundRobin(pool)
		seen[h.ID]++
		for j := range pool {
			if pool[j].ID == h.ID {
				pool[j].Count++
				pool[j].LastBooked = t0.Add(time.Duration(i) * time.Minute)
			}
		}
	}
	for _, id := range []uuid.UUID{a, b, c} {
		if seen[id] != 3 {
			t.Fatalf("rotation uneven: %v", seen)
		}
	}
}

// Collective slots need every host free inside their own hours: a New York
// host 9–17 and a London host 9–17 overlap 9:00–12:00 New York time, and
// 9:00–13:00 in the weeks between the US and UK clock changes.
func TestCollectiveIntersectsHoursAcrossZonesAndDST(t *testing.T) {
	ny, london := mustLoc(t, "America/New_York"), mustLoc(t, "Europe/London")
	base := slotRules{loc: ny, weekly: weekdays(0, 1440), duration: time.Hour, interval: 30 * time.Minute}
	nyRules, ldRules := base, base
	nyRules.weekly = weekdays(9*60, 17*60)
	ldRules.loc, ldRules.weekly = london, weekdays(9*60, 17*60)
	hosts := []hostAvail{{ID: uuid.New(), Rules: &nyRules}, {ID: uuid.New(), Rules: &ldRules}}

	check := func(day time.Time, wantFirst, wantLast int) {
		t.Helper()
		from := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, ny)
		cands := generateSlots(base, from, from.Add(24*time.Hour), nil, 500)
		got := teamSlots(cands, hosts, LinkCollective, 0, 0, 500)
		if len(got) == 0 {
			t.Fatalf("%s: no collective slots", day.Format(time.DateOnly))
		}
		first, last := got[0].StartsAt.In(ny), got[len(got)-1].StartsAt.In(ny)
		if first.Hour()*60+first.Minute() != wantFirst || last.Hour()*60+last.Minute() != wantLast {
			t.Fatalf("%s: slots %s–%s NY, want %d–%d minutes", day.Format(time.DateOnly), first.Format("15:04"), last.Format("15:04"), wantFirst, wantLast)
		}
		rr := teamSlots(cands, hosts, LinkRoundRobin, 0, 0, 500)
		if len(rr) <= len(got) {
			t.Fatalf("round robin should offer the union (%d) over the intersection (%d)", len(rr), len(got))
		}
	}
	// Tue Nov 10 2026: NY UTC-5, London UTC+0 → London 17:00 = NY 12:00; last 1-hour start 11:00.
	check(time.Date(2026, 11, 10, 12, 0, 0, 0, ny), 9*60, 11*60)
	// Tue Mar 17 2026: NY on EDT (UTC-4), London still GMT → London 17:00 = NY 13:00; last start 12:00.
	check(time.Date(2026, 3, 17, 12, 0, 0, 0, ny), 9*60, 12*60)
}

func TestTeamSlotsRespectBusyAndBuffers(t *testing.T) {
	utc := time.UTC
	day := time.Date(2026, 11, 10, 0, 0, 0, 0, utc)
	base := slotRules{loc: utc, weekly: weekdays(9*60, 12*60), duration: 30 * time.Minute, interval: 30 * time.Minute}
	cands := generateSlots(base, day, day.Add(24*time.Hour), nil, 100) // 9:00 … 11:30
	busyA := []busyItem{{Start: day.Add(9 * time.Hour), End: day.Add(10 * time.Hour), BlockStart: day.Add(9 * time.Hour), BlockEnd: day.Add(10 * time.Hour)}}
	a := hostAvail{ID: uuid.New(), Busy: busyA}
	b := hostAvail{ID: uuid.New(), Busy: []busyItem{{Start: day.Add(11 * time.Hour), End: day.Add(12 * time.Hour), BlockStart: day.Add(11 * time.Hour), BlockEnd: day.Add(12 * time.Hour)}}}
	coll := teamSlots(cands, []hostAvail{a, b}, LinkCollective, 0, 0, 100)
	if len(coll) != 2 || coll[0].StartsAt != day.Add(10*time.Hour) {
		t.Fatalf("collective: %v", coll)
	}
	rr := teamSlots(cands, []hostAvail{a, b}, LinkRoundRobin, 0, 0, 100)
	if len(rr) != len(cands) {
		t.Fatalf("round robin should cover every slot: %d of %d", len(rr), len(cands))
	}
	// A 15-minute buffer after keeps a's 9:30 meeting-end clear: 10:00 still
	// fits (buffers only touch meetings), 9:30 doesn't.
	free := freeHosts(Slot{StartsAt: day.Add(9*time.Hour + 30*time.Minute), EndsAt: day.Add(10 * time.Hour)}, []hostAvail{a}, 0, 15*time.Minute)
	if len(free) != 0 {
		t.Fatal("a is busy at 9:30")
	}
	if len(teamSlots(cands, nil, LinkRoundRobin, 0, 0, 10)) != 0 {
		t.Fatal("no hosts, no slots")
	}
}

func TestOffHoursKeepFindTimeInsideEveryonesDay(t *testing.T) {
	la, london := mustLoc(t, "America/Los_Angeles"), mustLoc(t, "Europe/London")
	day := time.Date(2026, 11, 10, 0, 0, 0, 0, la)
	from, to := day, day.Add(24*time.Hour)
	// Priya in London works 9–17 London = 1:00–9:00 LA; Sam in LA works 8–17.
	busy := offHours(london, weekdays(9*60, 17*60), from, to)
	rules := slotRules{loc: la, weekly: weekdays(8*60, 17*60), duration: 30 * time.Minute, interval: 30 * time.Minute}
	got := generateSlots(rules, from, to, busy, 100)
	if len(got) == 0 {
		t.Fatal("expected overlap 8:00–9:00 LA")
	}
	for _, sl := range got {
		if h := sl.StartsAt.In(la).Hour(); h != 8 {
			t.Fatalf("slot %s outside the 8:00–9:00 LA overlap", sl.StartsAt.In(la).Format("15:04"))
		}
	}
	if len(got) != 2 {
		t.Fatalf("want 8:00 and 8:30, got %d", len(got))
	}
	// Weekends are entirely off hours.
	sat := time.Date(2026, 11, 14, 0, 0, 0, 0, la)
	if off := offHours(london, weekdays(9*60, 17*60), sat, sat.Add(24*time.Hour)); len(off) != 1 || !off[0].Start.Equal(sat) {
		t.Fatalf("weekend off hours: %v", off)
	}
}

func TestSlotRulesContainsUsesLocalDate(t *testing.T) {
	tokyo := mustLoc(t, "Asia/Tokyo")
	r := slotRules{loc: tokyo, weekly: weekdays(9*60, 18*60)}
	start := time.Date(2026, 11, 10, 9, 0, 0, 0, tokyo)
	if !r.contains(start, start.Add(time.Hour)) {
		t.Fatal("9:00–10:00 Tokyo is inside")
	}
	if r.contains(start.Add(-30*time.Minute), start.Add(30*time.Minute)) {
		t.Fatal("8:30 starts before the window")
	}
	if r.contains(start.Add(8*time.Hour+30*time.Minute), start.Add(9*time.Hour+30*time.Minute)) {
		t.Fatal("17:30–18:30 runs past the window")
	}
}

func TestRoomConflict(t *testing.T) {
	at := func(h, m int) time.Time { return time.Date(2026, 11, 10, h, m, 0, 0, time.UTC) }
	busy := []Slot{{StartsAt: at(10, 0), EndsAt: at(11, 0)}}
	if c := roomConflict([]Slot{{StartsAt: at(11, 0), EndsAt: at(12, 0)}}, busy); c != nil {
		t.Fatal("back to back isn't a conflict")
	}
	if c := roomConflict([]Slot{{StartsAt: at(9, 0), EndsAt: at(9, 30)}, {StartsAt: at(10, 30), EndsAt: at(11, 30)}}, busy); c == nil || !c.StartsAt.Equal(at(10, 30)) {
		t.Fatalf("overlap at 10:30 missed: %v", c)
	}
}

func TestOutOfOfficeDeclinesOnlyOverlappingInvitations(t *testing.T) {
	at := func(d, h int) time.Time { return time.Date(2026, 11, d, h, 0, 0, 0, time.UTC) }
	ooo := block{row: eventRow{EventType: TypeOutOfOffice, AutoDecline: DeclineNew}, start: at(13, 0), end: at(14, 0)}
	focusOff := block{row: eventRow{EventType: TypeFocus, AutoDecline: DeclineNone}, start: at(12, 9), end: at(12, 11)}
	blocks := []block{focusOff, ooo}
	if b := decliningBlock(blocks, at(13, 10), at(13, 11)); b == nil || b.row.EventType != TypeOutOfOffice {
		t.Fatal("an invitation during out of office is declined")
	}
	if b := decliningBlock(blocks, at(12, 9), at(12, 10)); b != nil {
		t.Fatal("focus time with auto-decline off declines nothing")
	}
	if b := decliningBlock(blocks, at(14, 0), at(14, 1)); b != nil {
		t.Fatal("right after it ends isn't declined")
	}
	if got := declineText(ooo); got != "Declined automatically: out of office." {
		t.Fatalf("default message: %q", got)
	}
	ooo.row.DeclineMessage = "Back Monday."
	if declineText(ooo) != "Back Monday." {
		t.Fatal("the person's message is used")
	}
}

// Sharing levels: view = free/busy (redacted), comment = details (read
// only), edit = changes, owner = everything.
func TestSharingLevels(t *testing.T) {
	if (eventAccess{level: access.View}).canView() {
		t.Fatal("free/busy can't open an event")
	}
	if a := (eventAccess{level: access.Comment}); !a.canView() || a.canEdit() {
		t.Fatal("details: view, no edit")
	}
	if a := (eventAccess{level: access.Edit}); !a.canView() || !a.canEdit() {
		t.Fatal("make changes: view and edit")
	}
	if a := (eventAccess{isOwner: true}); !a.canEdit() {
		t.Fatal("owner edits")
	}
	if a := (eventAccess{attendee: &Attendee{}}); !a.canView() || a.canEdit() {
		t.Fatal("guest views only")
	}
	conf := "https://meet.example/abc"
	row := eventRow{ID: uuid.New(), Title: "Salary review", Location: "Room 4", ConferenceURL: &conf, Visibility: "busy",
		StartsAt: time.Now(), EndsAt: time.Now().Add(time.Hour), Timezone: "UTC", EventType: TypeDefault}
	o := occurrenceOf(viewerRow{eventRow: row, redact: true, attendeeCount: 3}, row, nil)
	if o.Title != "Busy" || o.Location != "" || o.ConferenceURL != nil || o.AttendeeCount != 0 || !o.Redacted {
		t.Fatalf("free/busy view leaks details: %+v", o)
	}
	row.Visibility = "details"
	if o := occurrenceOf(viewerRow{eventRow: row, redact: true}, row, nil); o.Title != "Salary review" || o.Location != "" {
		t.Fatalf("a details event shows its title only: %+v", o)
	}
	if o := occurrenceOf(viewerRow{eventRow: row}, row, nil); o.Location != "Room 4" || o.Role != "calendar" {
		t.Fatalf("details level sees everything, role calendar: %+v", o)
	}
}

func TestBookerGuestsAndAnswers(t *testing.T) {
	l := BookingLink{AllowGuests: true}
	got, err := bookerGuests(l, []string{"A@example.com", "a@example.com", "me@example.com"}, "me@example.com")
	if err != nil || len(got) != 1 || got[0] != "a@example.com" {
		t.Fatalf("guests: %v %v", got, err)
	}
	if _, err := bookerGuests(BookingLink{}, []string{"a@example.com"}, "me@example.com"); err == nil {
		t.Fatal("a link without guests refuses them")
	}
	if _, err := bookerGuests(l, []string{"not an email"}, ""); err == nil {
		t.Fatal("invalid guest email")
	}
	qs := []Question{
		{ID: "m", Label: "Topics", Type: "multi_choice", Options: []string{"A", "B", "C"}, Required: true},
		{ID: "c", Label: "I agree", Type: "checkbox", Required: true},
		{ID: "e", Label: "Work email", Type: "email"},
	}
	ans, err := validateAnswers(qs, []answerInput{{QuestionID: "m", Value: "B\nA\nB"}, {QuestionID: "c", Value: "yes"}, {QuestionID: "e", Value: "Sam@Example.com"}})
	if err != nil || len(ans) != 3 || ans[0].Value != "B\nA" || ans[2].Value != "sam@example.com" {
		t.Fatalf("answers: %+v %v", ans, err)
	}
	if _, err := validateAnswers(qs, []answerInput{{QuestionID: "m", Value: "D"}, {QuestionID: "c", Value: "yes"}}); err == nil {
		t.Fatal("unknown option")
	}
	if _, err := validateAnswers(qs, []answerInput{{QuestionID: "m", Value: "A"}}); err == nil {
		t.Fatal("required checkbox")
	}
}

func TestWorkLocations(t *testing.T) {
	got, err := validateWorkLocations([]WorkLocation{{Day: 3, Type: "home"}, {Day: 1, Type: "office", Label: " HQ "}})
	if err != nil || got[0].Day != 1 || got[0].Label != "HQ" {
		t.Fatalf("%v %v", got, err)
	}
	if _, err := validateWorkLocations([]WorkLocation{{Day: 1, Type: "office"}, {Day: 1, Type: "home"}}); err == nil {
		t.Fatal("one per day")
	}
	if _, err := validateWorkLocations([]WorkLocation{{Day: 1, Type: "moon"}}); err == nil {
		t.Fatal("unknown type")
	}
}
