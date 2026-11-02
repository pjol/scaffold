package calendar

import (
	"context"
	"slices"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/db"
)

// busyItem is time someone can't be booked. block is the item widened by
// its booking's buffers (equal to start/end for everything else).
type busyItem struct {
	Start, End           time.Time
	BlockStart, BlockEnd time.Time
	Title                string // set only when the owner shares details
}

// Slot is a bookable or free time.
type Slot struct {
	StartsAt time.Time `json:"starts_at"`
	EndsAt   time.Time `json:"ends_at"`
}

// busyFor collects userID's busy time overlapping [from, to): their own
// events and invitations they haven't declined that show as busy, their
// bookings (with buffers), busy blocks from subscribed calendars, and busy
// time from connected calendars that count for availability.
// excludeBooking leaves one booking out (rescheduling it).
func (s *Service) busyFor(ctx context.Context, q db.Querier, userID uuid.UUID, from, to time.Time, excludeBooking uuid.UUID) ([]busyItem, error) {
	// Buffers reach up to a day past a booking's own time.
	occ, err := s.occurrences(ctx, q, userID, from.Add(-24*time.Hour), to.Add(24*time.Hour), 20000)
	if err != nil {
		return nil, err
	}
	blocks := map[uuid.UUID][2]time.Time{}
	rows, err := q.Query(ctx, `SELECT id, lower(block), upper(block) FROM calendar_bookings
		WHERE host_id = $1 AND status = 'confirmed' AND block && tstzrange($2, $3)`, userID, from.Add(-24*time.Hour), to.Add(24*time.Hour))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id uuid.UUID
		var a, b time.Time
		if err := rows.Scan(&id, &a, &b); err != nil {
			rows.Close()
			return nil, err
		}
		blocks[id] = [2]time.Time{a, b}
	}
	rows.Close()

	var out []busyItem
	for _, o := range occ {
		if o.ShowAs != "busy" || (o.MyResponse != nil && *o.MyResponse == ResponseDeclined) {
			continue
		}
		if o.BookingID != nil && *o.BookingID == excludeBooking {
			continue
		}
		it := busyItem{Start: o.StartsAt, End: o.EndsAt, BlockStart: o.StartsAt, BlockEnd: o.EndsAt}
		if o.BookingID != nil && o.Role == "owner" {
			if b, ok := blocks[*o.BookingID]; ok {
				it.BlockStart, it.BlockEnd = b[0], b[1]
			}
		}
		if it.BlockEnd.After(from) && it.BlockStart.Before(to) {
			if o.Visibility == "details" {
				it.Title = o.Title
			}
			out = append(out, it)
		}
	}
	rows, err = q.Query(ctx, `
		SELECT lower(b.span), upper(b.span) FROM calendar_busy_blocks b
		JOIN calendar_subscriptions s ON s.id = b.subscription_id
		WHERE b.user_id = $1 AND s.enabled AND b.span && tstzrange($2, $3)`, userID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var a, b time.Time
		if err := rows.Scan(&a, &b); err != nil {
			return nil, err
		}
		out = append(out, busyItem{Start: a, End: b, BlockStart: a, BlockEnd: b})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	// Connected calendars marked as counting (free/busy only). A booking
	// being moved isn't blocked by its own platform copy.
	var spans []busySpan
	if ctx.Value(skipConnectedBusy{}) == nil {
		spans = s.connectedBusy(ctx, userID, from, to)
	}
	if excludeBooking != uuid.Nil && len(spans) > 0 {
		var a, b time.Time
		if err := q.QueryRow(ctx, `SELECT starts_at, ends_at FROM calendar_bookings WHERE id = $1 AND ext_event_id IS NOT NULL`, excludeBooking).Scan(&a, &b); err == nil {
			spans = subtractSpan(spans, a, b)
		}
	}
	for _, sp := range spans {
		out = append(out, busyItem{Start: sp.Start, End: sp.End, BlockStart: sp.Start, BlockEnd: sp.End})
	}
	slices.SortFunc(out, func(a, b busyItem) int { return a.Start.Compare(b.Start) })
	return out, nil
}

type skipConnectedBusy struct{}

// withoutConnectedBusy leaves connected calendars out of busyFor: the check
// a booking repeats inside its transaction (against other bookings) runs
// after the platform copy of the booking itself was written, and connected
// busy time was checked moments before.
func withoutConnectedBusy(ctx context.Context) context.Context {
	return context.WithValue(ctx, skipConnectedBusy{}, true)
}

// conflicts reports whether a candidate meeting (start/end, widened by its
// own buffers to blockStart/blockEnd) collides with busy time: its buffers
// may not touch anyone's meeting, and its meeting may not touch another
// booking's buffers. Buffers may overlap each other.
func conflicts(start, end, blockStart, blockEnd time.Time, busy []busyItem) bool {
	for _, b := range busy {
		if blockStart.Before(b.End) && blockEnd.After(b.Start) {
			return true
		}
		if start.Before(b.BlockEnd) && end.After(b.BlockStart) {
			return true
		}
	}
	return false
}

// slotRules describe when a booking may start.
type slotRules struct {
	loc       *time.Location
	weekly    []HoursInterval
	overrides map[string][]TimeInterval // date (YYYY-MM-DD in loc) → windows; empty = unavailable
	duration  time.Duration
	interval  time.Duration // between slot starts
	before    time.Duration // buffer before
	after     time.Duration // buffer after
	earliest  time.Time     // minimum notice
	latest    time.Time     // last allowed start (how far ahead)
	maxPerDay int           // 0 = no limit
	dayCounts map[string]int
}

// windowsOn returns date's availability windows as UTC intervals, built
// from wall-clock times in the rules' zone (so 09:00 stays 09:00 across
// daylight-saving changes).
func (r slotRules) windowsOn(day time.Time) [][2]time.Time {
	date := day.Format(time.DateOnly)
	var mins []TimeInterval
	if o, ok := r.overrides[date]; ok {
		mins = o
	} else {
		for _, h := range r.weekly {
			if h.Day == int(day.Weekday()) {
				mins = append(mins, TimeInterval{Start: h.Start, End: h.End})
			}
		}
	}
	out := make([][2]time.Time, 0, len(mins))
	for _, m := range mins {
		a := time.Date(day.Year(), day.Month(), day.Day(), m.Start/60, m.Start%60, 0, 0, r.loc)
		b := time.Date(day.Year(), day.Month(), day.Day(), m.End/60, m.End%60, 0, 0, r.loc)
		if b.After(a) {
			out = append(out, [2]time.Time{a.UTC(), b.UTC()})
		}
	}
	return out
}

// generateSlots lists bookable starts in [from, to): inside a window, at
// least the notice ahead, within how far ahead, under the daily cap, and
// clear of busy time with buffers. At most limit.
func generateSlots(r slotRules, from, to time.Time, busy []busyItem, limit int) []Slot {
	out := []Slot{}
	if r.duration <= 0 || r.interval <= 0 {
		return out
	}
	lo, hi := from, to
	if r.earliest.After(lo) {
		lo = r.earliest
	}
	if !r.latest.IsZero() && r.latest.Before(hi) {
		hi = r.latest
	}
	if !hi.After(lo) {
		return out
	}
	first := lo.In(r.loc).AddDate(0, 0, -1)
	last := hi.In(r.loc).AddDate(0, 0, 1)
	for day := time.Date(first.Year(), first.Month(), first.Day(), 12, 0, 0, 0, r.loc); !day.After(last); day = day.AddDate(0, 0, 1) {
		date := day.Format(time.DateOnly)
		if r.maxPerDay > 0 && r.dayCounts[date] >= r.maxPerDay {
			continue
		}
		for _, w := range r.windowsOn(day) {
			for start := w[0]; !start.Add(r.duration).After(w[1]); start = start.Add(r.interval) {
				end := start.Add(r.duration)
				if start.Before(lo) || !start.Before(hi) {
					continue
				}
				if conflicts(start, end, start.Add(-r.before), end.Add(r.after), busy) {
					continue
				}
				out = append(out, Slot{StartsAt: start, EndsAt: end})
				if len(out) >= limit {
					return out
				}
			}
		}
	}
	return out
}

// freeTimes lists free starts of length dur in the user's working hours
// (their zone) clear of everyone's busy time, every 30 minutes.
func freeTimes(loc *time.Location, hours []HoursInterval, from, to time.Time, dur time.Duration, busy []busyItem, limit int) []Slot {
	now := time.Now().UTC().Truncate(15 * time.Minute).Add(15 * time.Minute)
	return generateSlots(slotRules{
		loc: loc, weekly: hours, duration: dur, interval: 30 * time.Minute, earliest: now,
	}, from, to, busy, limit)
}
