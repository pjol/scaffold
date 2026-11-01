package calls

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// Times people read (invitation notifications, agent previews, search
// snippets) are worded in the reader's time zone and always name it, so a
// fallback zone is still read correctly.

// UserZone returns the time zone a person reads times in, or nil when it
// isn't known. The foundation keeps no zone per person; the app wires one
// with SetUserZone (for example a calendar feature's setting).
type UserZone func(ctx context.Context, userID uuid.UUID) *time.Location

var userZone atomic.Pointer[UserZone]

// SetUserZone wires where people's time zones come from (call it from
// stitching code); nil unwires it. Without it, times are worded in the zone
// the request came from (the creator's device, sent as time_zone), an
// agent's own UTC offset in previews, else UTC.
func SetUserZone(fn UserZone) {
	if fn == nil {
		userZone.Store(nil)
		return
	}
	userZone.Store(&fn)
}

// zoneFor is userID's zone, else fallback, else UTC.
func zoneFor(ctx context.Context, userID uuid.UUID, fallback *time.Location) *time.Location {
	if fn := userZone.Load(); fn != nil {
		if loc := (*fn)(ctx, userID); loc != nil {
			return loc
		}
	}
	if fallback != nil {
		return fallback
	}
	return time.UTC
}

// requestZone reads an IANA zone a client sent ("America/Los_Angeles");
// nil when empty or unknown, so a bad value never fails the request.
func requestZone(name string) *time.Location {
	name = strings.TrimSpace(name)
	if name == "" || name == "Local" || len(name) > 64 {
		return nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil
	}
	return loc
}

// whenText words a meeting time in loc: "Mon, Nov 2, 2026 · 7:00 AM – 7:30 AM PST",
// "Mon, Nov 2, 2026 · 7:00 AM PST" without an end.
func whenText(start time.Time, end *time.Time, loc *time.Location) string {
	ls := start.In(loc)
	zone := zoneName(ls)
	switch {
	case end == nil || !end.After(start):
		return ls.Format("Mon, Jan 2, 2006 · 3:04 PM") + " " + zone
	case ls.Format(time.DateOnly) == end.In(loc).Format(time.DateOnly):
		return ls.Format("Mon, Jan 2, 2006 · 3:04 PM") + " – " + end.In(loc).Format("3:04 PM") + " " + zone
	default:
		return ls.Format("Mon, Jan 2, 2006, 3:04 PM") + " – " + end.In(loc).Format("Mon, Jan 2, 3:04 PM") + " " + zone
	}
}

// zoneName is t's zone abbreviation ("PST"), or its offset ("UTC+5:30")
// where the zone has no letters for it.
func zoneName(t time.Time) string {
	name, off := t.Zone()
	if name != "" && !strings.ContainsAny(name, "+-0123456789") {
		return name
	}
	if off == 0 {
		return "UTC"
	}
	sign := "+"
	if off < 0 {
		sign, off = "−", -off
	}
	h, m := off/3600, off%3600/60
	if m == 0 {
		return fmt.Sprintf("UTC%s%d", sign, h)
	}
	return fmt.Sprintf("UTC%s%d:%02d", sign, h, m)
}
