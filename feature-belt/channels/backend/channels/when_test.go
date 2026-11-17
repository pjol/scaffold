package channels

import (
	"testing"
	"time"
)

func TestParseWhen(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Skip("no zoneinfo")
	}
	// Monday 2 November 2026, 10:25 in Los Angeles.
	now := time.Date(2026, 11, 2, 10, 25, 0, 0, loc)
	at := func(mo time.Month, d, h, m int) time.Time { return time.Date(2026, mo, d, h, m, 0, 0, loc) }
	cases := []struct {
		in   string
		want time.Time
	}{
		{"in 20 minutes", now.Add(20 * time.Minute)},
		{"in 1 hour", now.Add(time.Hour)},
		{"in 3h", now.Add(3 * time.Hour)},
		{"in an hour", now.Add(time.Hour)},
		{"in 2 days", now.AddDate(0, 0, 2)},
		{"in a week", now.AddDate(0, 0, 7)},
		{"tomorrow", at(11, 3, 9, 0)},
		{"Tomorrow at 3pm", at(11, 3, 15, 0)},
		{"at 3pm tomorrow", at(11, 3, 15, 0)},
		{"at 3:30 pm", at(11, 2, 15, 30)},
		{"at 9am", at(11, 3, 9, 0)}, // already past today
		{"at 15:30", at(11, 2, 15, 30)},
		{"at noon", at(11, 2, 12, 0)},
		{"tonight", at(11, 2, 20, 0)},
		{"next week", at(11, 9, 9, 0)},
		{"friday", at(11, 6, 9, 0)},
		{"on friday at 10am", at(11, 6, 10, 0)},
		{"next friday", at(11, 6, 9, 0)},
		{"monday", at(11, 9, 9, 0)}, // today is Monday: a week from today
		{"2026-12-01", at(12, 1, 9, 0)},
		{"2026-12-01 at 2pm", at(12, 1, 14, 0)},
		{"2026-11-08T14:00:00-08:00", at(11, 8, 14, 0)},
	}
	for _, c := range cases {
		got, err := parseWhen(c.in, now, loc)
		if err != nil {
			t.Errorf("%q: %v", c.in, err)
			continue
		}
		if !got.Equal(c.want) {
			t.Errorf("%q = %s, want %s", c.in, got, c.want)
		}
	}
	for _, bad := range []string{"", "soon", "at 25:00", "at 13pm", "in 0 minutes", "2026-02-30", "3", "in 2 years", "2025-01-01"} {
		if got, err := parseWhen(bad, now, loc); err == nil {
			t.Errorf("%q parsed as %s", bad, got)
		}
	}
	if _, err := parseWhen("2025-01-01", now, loc); err != errWhenPast {
		t.Errorf("past date: %v", err)
	}
}

func TestParseWhenDST(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Skip("no zoneinfo")
	}
	// Saturday before DST ends (1 November 2026 is the change; use 31 October).
	now := time.Date(2026, 10, 31, 9, 0, 0, 0, loc)
	got, err := parseWhen("in 1 day", now, loc)
	if err != nil {
		t.Fatal(err)
	}
	if got.Hour() != 9 {
		t.Errorf("calendar day across DST kept the wall clock: got %s", got)
	}
}

func TestRemindCommand(t *testing.T) {
	now := time.Date(2026, 11, 2, 10, 25, 0, 0, time.UTC)
	cases := []struct {
		in, what string
		want     time.Time
	}{
		{"me to check the deploy in 20 minutes", "check the deploy", now.Add(20 * time.Minute)},
		{"me in 20 minutes to stretch", "stretch", now.Add(20 * time.Minute)},
		{"me to call Sam tomorrow at 3pm", "call Sam", time.Date(2026, 11, 3, 15, 0, 0, 0, time.UTC)},
		{"me about the launch next week", "the launch", time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC)},
		{"me tomorrow to review PR 42", "review PR 42", time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)},
		{"water the plants at 5pm", "water the plants", time.Date(2026, 11, 2, 17, 0, 0, 0, time.UTC)},
	}
	for _, c := range cases {
		what, at, err := remindCommand(c.in, now, time.UTC)
		if err != nil {
			t.Errorf("%q: %v", c.in, err)
			continue
		}
		if what != c.what || !at.Equal(c.want) {
			t.Errorf("%q = (%q, %s), want (%q, %s)", c.in, what, at, c.what, c.want)
		}
	}
	for _, bad := range []string{"", "me", "me to do something", "me in 20 minutes"} {
		if w, at, err := remindCommand(bad, now, time.UTC); err == nil {
			t.Errorf("%q parsed as (%q, %s)", bad, w, at)
		}
	}
}

func TestMatchesKeyword(t *testing.T) {
	cases := []struct {
		text string
		kw   []string
		want bool
	}{
		{"the deploy is done", []string{"deploy"}, true},
		{"redeploy please", []string{"deploy"}, false},
		{"deploy.", []string{"deploy"}, true},
		{"we need a code review today", []string{"code review"}, true},
		{"no match here", []string{"release"}, false},
		{"release-notes are up", []string{"release"}, true},
		{"", []string{"x"}, false},
	}
	for _, c := range cases {
		if got := matchesKeyword(c.text, c.kw); got != c.want {
			t.Errorf("matchesKeyword(%q, %v) = %v", c.text, c.kw, got)
		}
	}
}

func TestPlainTextEscapes(t *testing.T) {
	if got := plainText(`ok ¯\\\_(ツ)\_/¯ **done**`, nil); got != `ok ¯\_(ツ)_/¯ done` {
		t.Errorf("plainText = %q", got)
	}
}
