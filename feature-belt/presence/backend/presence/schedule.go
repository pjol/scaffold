package presence

import (
	"encoding/json"
	"strconv"
	"sync"
	"time"

	"scaffoldapp/backend/internal/httpx"
)

// QuietHours is one weekday's Do Not Disturb window in the person's zone.
// An End at or before Start runs into the next day ("22:00" → "08:00");
// "00:00" → "24:00" is the whole day.
type QuietHours struct {
	Start string `json:"start"` // "22:00"
	End   string `json:"end"`   // "08:00"
}

// Schedule is a person's Do Not Disturb schedule: quiet hours per weekday,
// read in TimeZone. Pushes and emails are held during them like any Do Not
// Disturb (IsDND), and others see Do Not Disturb until they end.
type Schedule struct {
	Enabled  bool           `json:"enabled"`
	TimeZone string         `json:"time_zone"` // IANA, e.g. Europe/Berlin
	Days     [7]*QuietHours `json:"days"`      // Sunday first (time.Weekday); null = none that day
}

// quiet is a parsed Schedule: minutes after local midnight per weekday.
type quiet struct {
	loc  *time.Location
	days [7]*[2]int
}

var zones sync.Map // name → *time.Location

func loadZone(name string) (*time.Location, error) {
	if v, ok := zones.Load(name); ok {
		return v.(*time.Location), nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, err
	}
	zones.Store(name, loc)
	return loc, nil
}

// parseClock reads "HH:MM" (00:00–24:00) as minutes after midnight.
func parseClock(s string) (int, bool) {
	if len(s) != 5 || s[2] != ':' {
		return 0, false
	}
	h, err1 := strconv.Atoi(s[:2])
	m, err2 := strconv.Atoi(s[3:])
	if err1 != nil || err2 != nil || h < 0 || h > 24 || m < 0 || m > 59 || (h == 24 && m != 0) {
		return 0, false
	}
	return h*60 + m, true
}

// parse validates a schedule; the error is for the person.
func (sc Schedule) parse() (*quiet, error) {
	if sc.TimeZone == "" || sc.TimeZone == "Local" || len(sc.TimeZone) > 64 {
		return nil, httpx.BadRequest("Send time_zone: your IANA time zone, like Europe/Berlin.")
	}
	loc, err := loadZone(sc.TimeZone)
	if err != nil {
		return nil, httpx.BadRequest("Unknown time zone " + strconv.Quote(sc.TimeZone) + ".")
	}
	q := &quiet{loc: loc}
	for i, w := range sc.Days {
		if w == nil {
			continue
		}
		start, ok1 := parseClock(w.Start)
		end, ok2 := parseClock(w.End)
		if !ok1 || !ok2 || start == 24*60 {
			return nil, httpx.BadRequest("Quiet hours are times like 22:00 and 08:00.")
		}
		if start == end {
			return nil, httpx.BadRequest("Quiet hours need different start and end times.")
		}
		q.days[i] = &[2]int{start, end}
	}
	return q, nil
}

// parseStored reads a schedule row (zone and days JSON); nil when it can't
// be read (a zone removed from the tz database), which means no quiet hours.
func parseStored(zone string, days []byte) *quiet {
	if zone == "" || len(days) == 0 {
		return nil
	}
	sc := Schedule{TimeZone: zone}
	if err := json.Unmarshal(days, &sc.Days); err != nil {
		return nil
	}
	q, err := sc.parse()
	if err != nil {
		return nil
	}
	return q
}

// window is the quiet hours that start on the local day day (midnight).
func (q *quiet) window(day time.Time) (start, end time.Time, ok bool) {
	w := q.days[day.Weekday()]
	if w == nil {
		return time.Time{}, time.Time{}, false
	}
	y, m, d := day.Date()
	start = time.Date(y, m, d, 0, w[0], 0, 0, q.loc)
	endDay := d
	if w[1] <= w[0] {
		endDay++
	}
	end = time.Date(y, m, endDay, 0, w[1], 0, 0, q.loc)
	return start, end, true
}

func (q *quiet) midnight(t time.Time) time.Time {
	l := t.In(q.loc)
	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, q.loc)
}

// activeEnd returns the end of the quiet hours now is inside, with windows
// that follow on without a gap chained together (a weekend of whole days).
func (q *quiet) activeEnd(now time.Time) (time.Time, bool) {
	today := q.midnight(now)
	var end time.Time
	for _, off := range []int{-1, 0} {
		s, e, ok := q.window(today.AddDate(0, 0, off))
		if ok && !now.Before(s) && now.Before(e) && e.After(end) {
			end = e
		}
	}
	if end.IsZero() {
		return time.Time{}, false
	}
	for range 8 {
		day := q.midnight(end)
		extended := false
		for _, off := range []int{-1, 0} {
			s, e, ok := q.window(day.AddDate(0, 0, off))
			if ok && !s.After(end) && e.After(end) {
				end, extended = e, true
			}
		}
		if !extended {
			break
		}
	}
	return end, true
}

// nextChange is the first start or end of quiet hours after now, at most a
// day away (zone rules can change, so the sweeper looks again daily).
func (q *quiet) nextChange(now time.Time) time.Time {
	today := q.midnight(now)
	best := now.Add(24 * time.Hour)
	for off := -1; off <= 2; off++ {
		s, e, ok := q.window(today.AddDate(0, 0, off))
		if !ok {
			continue
		}
		for _, t := range []time.Time{s, e} {
			if t.After(now) && t.Before(best) {
				best = t
			}
		}
	}
	return best
}

// scheduledUntil is when scheduled Do Not Disturb ends, if it is on now: in
// quiet hours and not skipped ("resume notifications" skips the stretch
// until skip).
func scheduledUntil(q *quiet, skip *time.Time, now time.Time) (time.Time, bool) {
	if q == nil {
		return time.Time{}, false
	}
	end, ok := q.activeEnd(now)
	if !ok || (skip != nil && skip.After(now) && !end.After(*skip)) {
		return time.Time{}, false
	}
	return end, true
}

// effectiveDND combines Do Not Disturb set by hand (or by SetDNDUntil) with
// the schedule: on while either is, until the later end.
func effectiveDND(manual *time.Time, q *quiet, skip *time.Time, now time.Time) *time.Time {
	var out *time.Time
	if manual != nil && manual.After(now) {
		t := *manual
		out = &t
	}
	if end, ok := scheduledUntil(q, skip, now); ok && (out == nil || end.After(*out)) {
		out = &end
	}
	return out
}
