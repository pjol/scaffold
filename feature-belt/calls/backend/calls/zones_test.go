package calls

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func mustZone(t *testing.T, name string) *time.Location {
	t.Helper()
	l, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// The same UTC hour reads an hour earlier in Los Angeles once daylight
// saving ends (Nov 1, 2026), and the zone is named either way.
func TestWhenTextAcrossDST(t *testing.T) {
	la := mustZone(t, "America/Los_Angeles")
	before := time.Date(2026, 10, 30, 15, 0, 0, 0, time.UTC)
	after := time.Date(2026, 11, 2, 15, 0, 0, 0, time.UTC)
	end := after.Add(30 * time.Minute)
	if got, want := whenText(before, nil, la), "Fri, Oct 30, 2026 · 8:00 AM PDT"; got != want {
		t.Errorf("before: %q, want %q", got, want)
	}
	if got, want := whenText(after, &end, la), "Mon, Nov 2, 2026 · 7:00 AM – 7:30 AM PST"; got != want {
		t.Errorf("after: %q, want %q", got, want)
	}
	late := time.Date(2026, 11, 3, 7, 0, 0, 0, time.UTC) // 11 PM PST the day before
	lateEnd := late.Add(2 * time.Hour)
	if got, want := whenText(late, &lateEnd, la), "Mon, Nov 2, 2026, 11:00 PM – Tue, Nov 3, 1:00 AM PST"; got != want {
		t.Errorf("overnight: %q, want %q", got, want)
	}
	if got, want := whenText(after, nil, mustZone(t, "Asia/Kolkata")), "Mon, Nov 2, 2026 · 8:30 PM IST"; got != want {
		t.Errorf("kolkata: %q, want %q", got, want)
	}
	if got, want := whenText(after, nil, time.FixedZone("", 5*3600+45*60)), "Mon, Nov 2, 2026 · 8:45 PM UTC+5:45"; got != want {
		t.Errorf("fixed offset: %q, want %q", got, want)
	}
	if got, want := whenText(after, nil, mustZone(t, "America/Sao_Paulo")), "Mon, Nov 2, 2026 · 12:00 PM UTC−3"; got != want {
		t.Errorf("numeric abbreviation: %q, want %q", got, want)
	}
}

// Invitations use each invitee's zone when the app wires one, else the
// inviter's (the request's), else UTC.
func TestInviteTextUsesRecipientZone(t *testing.T) {
	ny, la := mustZone(t, "America/New_York"), mustZone(t, "America/Los_Angeles")
	known := uuid.New()
	SetUserZone(func(_ context.Context, id uuid.UUID) *time.Location {
		if id == known {
			return ny
		}
		return nil
	})
	defer SetUserZone(nil)
	start := time.Date(2026, 11, 2, 15, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	m := Meeting{Title: "Design review", StartsAt: &start, EndsAt: &end}
	ctx := context.Background()
	if got, want := inviteText("Sam", m, zoneFor(ctx, known, la)), "Sam invited you to Design review on Mon, Nov 2, 2026 · 10:00 AM – 11:00 AM EST."; got != want {
		t.Errorf("known zone: %q, want %q", got, want)
	}
	if got, want := inviteText("Sam", m, zoneFor(ctx, uuid.New(), la)), "Sam invited you to Design review on Mon, Nov 2, 2026 · 7:00 AM – 8:00 AM PST."; got != want {
		t.Errorf("inviter's zone: %q, want %q", got, want)
	}
	SetUserZone(nil)
	if got, want := inviteText("Sam", Meeting{StartsAt: &start}, zoneFor(ctx, known, nil)), "Sam invited you to a meeting on Mon, Nov 2, 2026 · 3:00 PM UTC."; got != want {
		t.Errorf("no zone: %q, want %q", got, want)
	}
}

// An agent's preview reads in the reviewer's zone, else in the offset the
// agent wrote the time with.
func TestCreateSummaryZones(t *testing.T) {
	start, err := time.Parse(time.RFC3339, "2026-11-02T07:00:00-08:00")
	if err != nil {
		t.Fatal(err)
	}
	end := start.Add(time.Hour)
	in := createPreview{Title: "Design review", StartsAt: &start, EndsAt: &end}
	if got, want := createSummary(in, mustZone(t, "America/Los_Angeles")), "Create a meeting “Design review” on Mon, Nov 2, 2026 · 7:00 AM – 8:00 AM PST"; got != want {
		t.Errorf("reviewer zone: %q, want %q", got, want)
	}
	if got := createSummary(in, start.Location()); got != "Create a meeting “Design review” on Mon, Nov 2, 2026 · 7:00 AM – 8:00 AM UTC−8" && got != "Create a meeting “Design review” on Mon, Nov 2, 2026 · 7:00 AM – 8:00 AM PST" {
		t.Errorf("agent offset: %q", got)
	}
	if got, want := createSummary(createPreview{}, time.UTC), "Create a meeting link"; got != want {
		t.Errorf("no time: %q, want %q", got, want)
	}
}

func TestRequestZone(t *testing.T) {
	if requestZone("America/Los_Angeles") == nil {
		t.Error("valid zone refused")
	}
	for _, bad := range []string{"", "Local", "Mars/Olympus", "../../etc/passwd"} {
		if requestZone(bad) != nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
