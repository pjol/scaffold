package presence

import (
	"testing"
	"time"
)

func mustQuiet(t *testing.T, sc Schedule) *quiet {
	t.Helper()
	q, err := sc.parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return q
}

func at(t *testing.T, loc *time.Location, s string) time.Time {
	t.Helper()
	v, err := time.ParseInLocation("2006-01-02 15:04", s, loc)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestScheduleParse(t *testing.T) {
	bad := []Schedule{
		{TimeZone: ""},
		{TimeZone: "Mars/Olympus"},
		{TimeZone: "UTC", Days: [7]*QuietHours{{Start: "22:00", End: "22:00"}}},
		{TimeZone: "UTC", Days: [7]*QuietHours{{Start: "24:00", End: "08:00"}}},
		{TimeZone: "UTC", Days: [7]*QuietHours{{Start: "7:00", End: "08:00"}}},
		{TimeZone: "UTC", Days: [7]*QuietHours{{Start: "22:00", End: "24:30"}}},
	}
	for i, sc := range bad {
		if _, err := sc.parse(); err == nil {
			t.Errorf("bad[%d] parsed", i)
		}
	}
	if _, err := (Schedule{TimeZone: "UTC", Days: [7]*QuietHours{{Start: "00:00", End: "24:00"}}}).parse(); err != nil {
		t.Errorf("all day: %v", err)
	}
}

func TestQuietOvernightWeekdays(t *testing.T) {
	berlin, _ := time.LoadLocation("Europe/Berlin")
	night := &QuietHours{Start: "22:00", End: "08:00"}
	// Weeknights only: Sunday to Thursday nights.
	q := mustQuiet(t, Schedule{TimeZone: "Europe/Berlin", Days: [7]*QuietHours{night, night, night, night, night, nil, nil}})

	// Wednesday 2026-11-04 23:30 Berlin: inside, until Thursday 08:00.
	end, ok := q.activeEnd(at(t, berlin, "2026-11-04 23:30"))
	if !ok || !end.Equal(at(t, berlin, "2026-11-05 08:00")) {
		t.Fatalf("Wed night: %v %v", end, ok)
	}
	// Thursday 07:59: still inside yesterday's window.
	if _, ok := q.activeEnd(at(t, berlin, "2026-11-05 07:59")); !ok {
		t.Fatal("Thu 07:59 should be quiet")
	}
	// Thursday 08:00: out.
	if _, ok := q.activeEnd(at(t, berlin, "2026-11-05 08:00")); ok {
		t.Fatal("Thu 08:00 should not be quiet")
	}
	// Friday 23:00: no window starts on Friday.
	if _, ok := q.activeEnd(at(t, berlin, "2026-11-06 23:00")); ok {
		t.Fatal("Fri night should not be quiet")
	}
	// Next change from Thursday noon is Thursday 22:00.
	if next := q.nextChange(at(t, berlin, "2026-11-05 12:00")); !next.Equal(at(t, berlin, "2026-11-05 22:00")) {
		t.Fatalf("next change: %v", next)
	}
}

func TestQuietWeekendChains(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	day := &QuietHours{Start: "00:00", End: "24:00"}
	fri := &QuietHours{Start: "18:00", End: "24:00"}
	q := mustQuiet(t, Schedule{TimeZone: "America/New_York", Days: [7]*QuietHours{day, nil, nil, nil, nil, fri, day}})
	// Friday 19:00: Friday evening, all of Saturday and Sunday chain to Monday 00:00.
	end, ok := q.activeEnd(at(t, ny, "2026-11-06 19:00"))
	if !ok || !end.Equal(at(t, ny, "2026-11-09 00:00")) {
		t.Fatalf("weekend: %v %v", end, ok)
	}
}

func TestQuietAcrossDST(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	night := &QuietHours{Start: "22:00", End: "07:00"}
	q := mustQuiet(t, Schedule{TimeZone: "America/New_York", Days: [7]*QuietHours{night, night, night, night, night, night, night}})
	// DST ends 2026-11-01 02:00 → 01:00: the Saturday night window still ends at 07:00 local.
	end, ok := q.activeEnd(at(t, ny, "2026-11-01 03:00"))
	if !ok || end.In(ny).Hour() != 7 || end.In(ny).Day() != 1 {
		t.Fatalf("dst: %v %v", end.In(ny), ok)
	}
}

func TestEffectiveDNDAndSkip(t *testing.T) {
	utc := time.UTC
	night := &QuietHours{Start: "22:00", End: "06:00"}
	q := mustQuiet(t, Schedule{TimeZone: "UTC", Days: [7]*QuietHours{night, night, night, night, night, night, night}})
	now := at(t, utc, "2026-11-05 23:00")
	end := at(t, utc, "2026-11-06 06:00")
	if got := effectiveDND(nil, q, nil, now); got == nil || !got.Equal(end) {
		t.Fatalf("schedule only: %v", got)
	}
	later := at(t, utc, "2026-11-06 09:00")
	if got := effectiveDND(&later, q, nil, now); got == nil || !got.Equal(later) {
		t.Fatalf("manual longer wins: %v", got)
	}
	if got := effectiveDND(nil, q, &end, now); got != nil {
		t.Fatalf("skipped stretch should be off: %v", got)
	}
	// The skip only covers that night: the next night is quiet again.
	if got := effectiveDND(nil, q, &end, at(t, utc, "2026-11-06 22:30")); got == nil {
		t.Fatal("next night should be quiet")
	}
	past := at(t, utc, "2026-11-05 20:00")
	if got := effectiveDND(&past, nil, nil, now); got != nil {
		t.Fatalf("expired manual: %v", got)
	}
}
