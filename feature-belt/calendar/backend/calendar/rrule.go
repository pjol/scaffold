package calendar

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Rule is the RRULE subset (RFC 5545 §3.3.10) this package reads and writes:
// FREQ DAILY/WEEKLY/MONTHLY/YEARLY, INTERVAL, COUNT, UNTIL, BYDAY (weekdays,
// with ordinals for MONTHLY and YEARLY+BYMONTH), BYMONTHDAY, BYMONTH and
// WKST. Occurrences are computed on wall-clock time in the event's zone, so
// a 10:00 meeting stays at 10:00 across daylight-saving changes.
type Rule struct {
	Freq       string
	Interval   int
	Count      int        // 0 = no count
	Until      *time.Time // last allowed start (inclusive), UTC
	ByDay      []WeekdayNum
	ByMonthDay []int
	ByMonth    []int
	WeekStart  time.Weekday
}

// WeekdayNum is a BYDAY entry: a weekday, optionally the Nth (or Nth from
// the end when negative) in the month or year.
type WeekdayNum struct {
	Day time.Weekday
	N   int
}

var dayCodes = map[string]time.Weekday{"SU": time.Sunday, "MO": time.Monday, "TU": time.Tuesday, "WE": time.Wednesday, "TH": time.Thursday, "FR": time.Friday, "SA": time.Saturday}

var codeOfDay = [...]string{"SU", "MO", "TU", "WE", "TH", "FR", "SA"}

// errRule is returned for rules outside the supported subset.
var errRule = errors.New("unsupported recurrence rule")

// ParseRule reads an RRULE value (without the "RRULE:" prefix). loc
// resolves a date-only or floating UNTIL as the end of that local day/time.
func ParseRule(s string, loc *time.Location) (Rule, error) {
	r := Rule{Interval: 1, WeekStart: time.Monday}
	s = strings.TrimPrefix(strings.TrimSpace(s), "RRULE:")
	if s == "" {
		return r, fmt.Errorf("%w: empty", errRule)
	}
	for part := range strings.SplitSeq(s, ";") {
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			return r, fmt.Errorf("%w: %q", errRule, part)
		}
		k, v = strings.ToUpper(strings.TrimSpace(k)), strings.ToUpper(strings.TrimSpace(v))
		switch k {
		case "FREQ":
			switch v {
			case "DAILY", "WEEKLY", "MONTHLY", "YEARLY":
				r.Freq = v
			default:
				return r, fmt.Errorf("%w: FREQ=%s", errRule, v)
			}
		case "INTERVAL":
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > 999 {
				return r, fmt.Errorf("%w: INTERVAL=%s", errRule, v)
			}
			r.Interval = n
		case "COUNT":
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > 10000 {
				return r, fmt.Errorf("%w: COUNT=%s", errRule, v)
			}
			r.Count = n
		case "UNTIL":
			t, err := parseUntil(v, loc)
			if err != nil {
				return r, err
			}
			r.Until = &t
		case "BYDAY":
			for d := range strings.SplitSeq(v, ",") {
				d = strings.TrimSpace(d)
				if len(d) < 2 {
					return r, fmt.Errorf("%w: BYDAY=%s", errRule, v)
				}
				wd, ok := dayCodes[d[len(d)-2:]]
				if !ok {
					return r, fmt.Errorf("%w: BYDAY=%s", errRule, v)
				}
				n := 0
				if num := d[:len(d)-2]; num != "" {
					var err error
					if n, err = strconv.Atoi(strings.TrimPrefix(num, "+")); err != nil || n == 0 || n < -53 || n > 53 {
						return r, fmt.Errorf("%w: BYDAY=%s", errRule, v)
					}
				}
				r.ByDay = append(r.ByDay, WeekdayNum{Day: wd, N: n})
			}
		case "BYMONTHDAY":
			for d := range strings.SplitSeq(v, ",") {
				n, err := strconv.Atoi(strings.TrimSpace(d))
				if err != nil || n == 0 || n < -31 || n > 31 {
					return r, fmt.Errorf("%w: BYMONTHDAY=%s", errRule, v)
				}
				r.ByMonthDay = append(r.ByMonthDay, n)
			}
		case "BYMONTH":
			for d := range strings.SplitSeq(v, ",") {
				n, err := strconv.Atoi(strings.TrimSpace(d))
				if err != nil || n < 1 || n > 12 {
					return r, fmt.Errorf("%w: BYMONTH=%s", errRule, v)
				}
				r.ByMonth = append(r.ByMonth, n)
			}
		case "WKST":
			wd, ok := dayCodes[v]
			if !ok {
				return r, fmt.Errorf("%w: WKST=%s", errRule, v)
			}
			r.WeekStart = wd
		default:
			return r, fmt.Errorf("%w: %s", errRule, k)
		}
	}
	if r.Freq == "" {
		return r, fmt.Errorf("%w: FREQ missing", errRule)
	}
	if r.Count > 0 && r.Until != nil {
		return r, fmt.Errorf("%w: COUNT and UNTIL together", errRule)
	}
	for _, d := range r.ByDay {
		if d.N != 0 && r.Freq != "MONTHLY" && !(r.Freq == "YEARLY" && len(r.ByMonth) > 0) {
			return r, fmt.Errorf("%w: numbered BYDAY needs FREQ=MONTHLY", errRule)
		}
	}
	if r.Freq == "YEARLY" && len(r.ByDay) > 0 && len(r.ByMonth) == 0 {
		return r, fmt.Errorf("%w: yearly BYDAY without BYMONTH", errRule)
	}
	return r, nil
}

func parseUntil(v string, loc *time.Location) (time.Time, error) {
	switch {
	case len(v) == 8:
		d, err := time.ParseInLocation("20060102", v, loc)
		if err != nil {
			return time.Time{}, fmt.Errorf("%w: UNTIL=%s", errRule, v)
		}
		return d.AddDate(0, 0, 1).Add(-time.Second).UTC(), nil
	case strings.HasSuffix(v, "Z"):
		t, err := time.Parse("20060102T150405Z", v)
		if err != nil {
			return time.Time{}, fmt.Errorf("%w: UNTIL=%s", errRule, v)
		}
		return t.UTC(), nil
	default:
		t, err := time.ParseInLocation("20060102T150405", v, loc)
		if err != nil {
			return time.Time{}, fmt.Errorf("%w: UNTIL=%s", errRule, v)
		}
		return t.UTC(), nil
	}
}

// String writes the rule in canonical order. UNTIL is written in UTC, or as
// a date for all-day series (RFC 5545 wants UNTIL to match DTSTART's type).
func (r Rule) String(allDay bool, loc *time.Location) string {
	parts := []string{"FREQ=" + r.Freq}
	if r.Interval > 1 {
		parts = append(parts, "INTERVAL="+strconv.Itoa(r.Interval))
	}
	if r.Count > 0 {
		parts = append(parts, "COUNT="+strconv.Itoa(r.Count))
	}
	if r.Until != nil {
		if allDay {
			parts = append(parts, "UNTIL="+r.Until.In(loc).Format("20060102"))
		} else {
			parts = append(parts, "UNTIL="+r.Until.UTC().Format("20060102T150405Z"))
		}
	}
	if len(r.ByDay) > 0 {
		codes := make([]string, 0, len(r.ByDay))
		for _, d := range r.ByDay {
			c := codeOfDay[d.Day]
			if d.N != 0 {
				c = strconv.Itoa(d.N) + c
			}
			codes = append(codes, c)
		}
		parts = append(parts, "BYDAY="+strings.Join(codes, ","))
	}
	if len(r.ByMonthDay) > 0 {
		parts = append(parts, "BYMONTHDAY="+joinInts(r.ByMonthDay))
	}
	if len(r.ByMonth) > 0 {
		parts = append(parts, "BYMONTH="+joinInts(r.ByMonth))
	}
	if r.WeekStart != time.Monday {
		parts = append(parts, "WKST="+codeOfDay[r.WeekStart])
	}
	return strings.Join(parts, ";")
}

func joinInts(v []int) string {
	s := make([]string, len(v))
	for i, n := range v {
		s[i] = strconv.Itoa(n)
	}
	return strings.Join(s, ",")
}

// expansion caps keep a hostile rule from spinning: at most this many
// periods are walked per expansion.
const maxPeriods = 50000

// Expand returns the series' occurrence starts (UTC, ascending) with
// from <= start < to, at most max of them. dtstart is the first occurrence
// (wall clock in loc) and counts toward COUNT whether or not it matches the
// BY rules, as calendar apps do. exclude lists starts to skip (cancelled
// occurrences); they still count toward COUNT.
func (r Rule) Expand(dtstart time.Time, loc *time.Location, from, to time.Time, max int, exclude map[int64]bool) []time.Time {
	var out []time.Time
	r.walk(dtstart, loc, func(t time.Time) bool {
		if !t.Before(to) || len(out) >= max {
			return false
		}
		if !t.Before(from) && !exclude[t.Unix()] {
			out = append(out, t)
		}
		return true
	})
	return out
}

// Last returns the start of the final occurrence, or ok=false when the
// series never ends (no COUNT or UNTIL).
func (r Rule) Last(dtstart time.Time, loc *time.Location) (time.Time, bool) {
	if r.Count == 0 && r.Until == nil {
		return time.Time{}, false
	}
	last := dtstart.UTC()
	r.walk(dtstart, loc, func(t time.Time) bool {
		last = t
		return true
	})
	return last, true
}

// Nth returns the start of the n-th occurrence (0-based) and whether it exists.
func (r Rule) Nth(dtstart time.Time, loc *time.Location, n int) (time.Time, bool) {
	i := 0
	var found time.Time
	ok := false
	r.walk(dtstart, loc, func(t time.Time) bool {
		if i == n {
			found, ok = t, true
			return false
		}
		i++
		return true
	})
	return found, ok
}

// CountBefore is how many occurrences start before t.
func (r Rule) CountBefore(dtstart time.Time, loc *time.Location, t time.Time) int {
	n := 0
	r.walk(dtstart, loc, func(s time.Time) bool {
		if !s.Before(t) {
			return false
		}
		n++
		return true
	})
	return n
}

// Occurs reports whether t is an occurrence start of the series.
func (r Rule) Occurs(dtstart time.Time, loc *time.Location, t time.Time) bool {
	found := false
	r.walk(dtstart, loc, func(s time.Time) bool {
		if s.Equal(t) {
			found = true
			return false
		}
		return s.Before(t)
	})
	return found
}

// walk calls fn with every occurrence start in order until fn returns
// false or the series ends.
func (r Rule) walk(dtstart time.Time, loc *time.Location, fn func(time.Time) bool) {
	ls := dtstart.In(loc)
	start := ls.UTC()
	h, mi, sec := ls.Clock()
	count := 0
	emit := func(t time.Time) bool {
		if r.Until != nil && t.After(*r.Until) {
			return false
		}
		if r.Count > 0 && count >= r.Count {
			return false
		}
		count++
		return fn(t)
	}
	if !emit(start) {
		return
	}
	interval := max(r.Interval, 1)
	for p := 0; p < maxPeriods; p++ {
		dates := r.periodDates(ls, p*interval)
		if dates == nil {
			return
		}
		for _, d := range dates {
			t := time.Date(d.y, d.m, d.d, h, mi, sec, 0, loc).UTC()
			if !t.After(start) {
				continue
			}
			if !emit(t) {
				return
			}
		}
	}
}

type ymd struct {
	y int
	m time.Month
	d int
}

func daysIn(y int, m time.Month) int { return time.Date(y, m+1, 0, 0, 0, 0, 0, time.UTC).Day() }

// periodDates lists the candidate dates (ascending) in the period offset
// periods after the one containing ls. nil ends the walk (year past 9999).
func (r Rule) periodDates(ls time.Time, offset int) []ymd {
	var out []ymd
	switch r.Freq {
	case "DAILY":
		d := time.Date(ls.Year(), ls.Month(), ls.Day()+offset, 12, 0, 0, 0, time.UTC)
		if d.Year() > 9999 {
			return nil
		}
		if r.matchesFilters(d) && r.matchesDayFilter(d) {
			out = append(out, ymd{d.Year(), d.Month(), d.Day()})
		}
		return nonNil(out)
	case "WEEKLY":
		base := time.Date(ls.Year(), ls.Month(), ls.Day(), 12, 0, 0, 0, time.UTC)
		back := (int(base.Weekday()) - int(r.WeekStart) + 7) % 7
		weekStart := base.AddDate(0, 0, -back+7*offset)
		if weekStart.Year() > 9999 {
			return nil
		}
		days := r.ByDay
		if len(days) == 0 {
			days = []WeekdayNum{{Day: ls.Weekday()}}
		}
		for _, wd := range days {
			d := weekStart.AddDate(0, 0, (int(wd.Day)-int(r.WeekStart)+7)%7)
			if r.matchesFilters(d) {
				out = append(out, ymd{d.Year(), d.Month(), d.Day()})
			}
		}
	case "MONTHLY":
		first := time.Date(ls.Year(), ls.Month()+time.Month(offset), 1, 12, 0, 0, 0, time.UTC)
		if first.Year() > 9999 {
			return nil
		}
		if len(r.ByMonth) > 0 && !slices.Contains(r.ByMonth, int(first.Month())) {
			return []ymd{}
		}
		out = r.monthDates(first.Year(), first.Month(), ls.Day())
	case "YEARLY":
		y := ls.Year() + offset
		if y > 9999 {
			return nil
		}
		months := r.ByMonth
		if len(months) == 0 {
			months = []int{int(ls.Month())}
		}
		for _, m := range months {
			out = append(out, r.monthDates(y, time.Month(m), ls.Day())...)
		}
	}
	slices.SortFunc(out, func(a, b ymd) int {
		return time.Date(a.y, a.m, a.d, 0, 0, 0, 0, time.UTC).Compare(time.Date(b.y, b.m, b.d, 0, 0, 0, 0, time.UTC))
	})
	return nonNil(slices.Compact(out))
}

func nonNil(v []ymd) []ymd {
	if v == nil {
		return []ymd{}
	}
	return v
}

// monthDates expands BYMONTHDAY / BYDAY inside one month; with neither, the
// series' own day of month (skipped when the month is too short).
func (r Rule) monthDates(y int, m time.Month, defaultDay int) []ymd {
	n := daysIn(y, m)
	var byMonthDay []int
	for _, d := range r.ByMonthDay {
		if d < 0 {
			d = n + d + 1
		}
		if d >= 1 && d <= n {
			byMonthDay = append(byMonthDay, d)
		}
	}
	var byDay []int
	for _, wd := range r.ByDay {
		var matches []int
		for d := 1; d <= n; d++ {
			if time.Date(y, m, d, 12, 0, 0, 0, time.UTC).Weekday() == wd.Day {
				matches = append(matches, d)
			}
		}
		switch {
		case wd.N > 0 && wd.N <= len(matches):
			byDay = append(byDay, matches[wd.N-1])
		case wd.N < 0 && -wd.N <= len(matches):
			byDay = append(byDay, matches[len(matches)+wd.N])
		case wd.N == 0:
			byDay = append(byDay, matches...)
		}
	}
	var days []int
	switch {
	case len(r.ByMonthDay) > 0 && len(r.ByDay) > 0:
		for _, d := range byMonthDay {
			if slices.Contains(byDay, d) {
				days = append(days, d)
			}
		}
	case len(r.ByMonthDay) > 0:
		days = byMonthDay
	case len(r.ByDay) > 0:
		days = byDay
	default:
		if defaultDay <= n {
			days = []int{defaultDay}
		}
	}
	out := make([]ymd, 0, len(days))
	for _, d := range days {
		out = append(out, ymd{y, m, d})
	}
	return out
}

// matchesFilters applies BYMONTH and BYMONTHDAY as limits (DAILY, WEEKLY).
func (r Rule) matchesFilters(d time.Time) bool {
	if len(r.ByMonth) > 0 && !slices.Contains(r.ByMonth, int(d.Month())) {
		return false
	}
	if len(r.ByMonthDay) > 0 {
		n := daysIn(d.Year(), d.Month())
		ok := false
		for _, md := range r.ByMonthDay {
			if md < 0 {
				md = n + md + 1
			}
			if md == d.Day() {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

// matchesDayFilter applies BYDAY as a limit (DAILY).
func (r Rule) matchesDayFilter(d time.Time) bool {
	if len(r.ByDay) == 0 {
		return true
	}
	for _, wd := range r.ByDay {
		if wd.Day == d.Weekday() {
			return true
		}
	}
	return false
}
