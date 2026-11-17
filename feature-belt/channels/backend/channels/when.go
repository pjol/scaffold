package channels

// Times people type for reminders and scheduled messages: "in 20 minutes",
// "tomorrow", "next week", "friday at 3pm", "at 15:30", "2026-12-01 9am".
// Days without a time mean 9:00 in the person's zone.

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	errWhenUnknown = errors.New("unknown time")
	errWhenPast    = errors.New("time has passed")
	errWhenFar     = errors.New("too far ahead")
)

const (
	defaultHour = 9
	maxAhead    = 366 * 24 * time.Hour
)

var (
	relRe     = regexp.MustCompile(`^in\s+(\d{1,4}|an?|one)\s*(m|mins?|minutes?|h|hrs?|hours?|d|days?|w|wks?|weeks?)$`)
	clockRe   = regexp.MustCompile(`^(?:at\s+)?(\d{1,2})(?::(\d{2}))?\s*(am|pm|a\.m\.|p\.m\.)?$`)
	isoDateRe = regexp.MustCompile(`^(?:on\s+)?(\d{4})-(\d{2})-(\d{2})$`)
)

var weekdays = map[string]time.Weekday{
	"sunday": time.Sunday, "sun": time.Sunday, "monday": time.Monday, "mon": time.Monday,
	"tuesday": time.Tuesday, "tue": time.Tuesday, "tues": time.Tuesday, "wednesday": time.Wednesday, "wed": time.Wednesday,
	"thursday": time.Thursday, "thu": time.Thursday, "thurs": time.Thursday, "friday": time.Friday, "fri": time.Friday,
	"saturday": time.Saturday, "sat": time.Saturday,
}

// parseWhen turns a phrase into a time after now, in loc.
func parseWhen(phrase string, now time.Time, loc *time.Location) (time.Time, error) {
	p := strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(phrase)), " "))
	p = strings.TrimSuffix(p, ".")
	if p == "" {
		return time.Time{}, errWhenUnknown
	}
	now = now.In(loc)
	t, err := parseWhenIn(p, now, loc)
	if err != nil {
		return time.Time{}, err
	}
	if !t.After(now) {
		return time.Time{}, errWhenPast
	}
	if t.Sub(now) > maxAhead {
		return time.Time{}, errWhenFar
	}
	return t, nil
}

func parseWhenIn(p string, now time.Time, loc *time.Location) (time.Time, error) {
	if m := relRe.FindStringSubmatch(p); m != nil {
		n := 1
		if v, err := strconv.Atoi(m[1]); err == nil {
			n = v
		}
		if n <= 0 {
			return time.Time{}, errWhenUnknown
		}
		var unit time.Duration
		switch m[2][0] {
		case 'm':
			unit = time.Minute
		case 'h':
			unit = time.Hour
		case 'd':
			unit = 24 * time.Hour
		case 'w':
			unit = 7 * 24 * time.Hour
		}
		if time.Duration(n)*unit > maxAhead {
			return time.Time{}, errWhenFar
		}
		if unit >= 24*time.Hour { // calendar days keep the wall-clock time across DST
			days := int(time.Duration(n) * unit / (24 * time.Hour))
			return now.AddDate(0, 0, days), nil
		}
		return now.Add(time.Duration(n) * unit), nil
	}
	if t, err := time.ParseInLocation(time.RFC3339, strings.ToUpper(p), loc); err == nil {
		return t, nil
	}

	// A day part and a time part, in either order: "tomorrow at 3pm", "3pm friday".
	words := strings.Fields(p)
	for split := 0; split <= len(words); split++ {
		a, b := strings.Join(words[:split], " "), strings.Join(words[split:], " ")
		if t, ok := dayAndClock(a, b, now, loc); ok {
			return t, nil
		}
		if t, ok := dayAndClock(b, a, now, loc); ok {
			return t, nil
		}
	}
	return time.Time{}, errWhenUnknown
}

// dayAndClock combines a day phrase (may be "") with a clock phrase (may be "").
func dayAndClock(dayPart, clockPart string, now time.Time, loc *time.Location) (time.Time, bool) {
	if dayPart == "" && clockPart == "" {
		return time.Time{}, false
	}
	hour, minute, hasClock := defaultHour, 0, false
	if clockPart != "" {
		h, m, ok := parseClock(clockPart)
		if !ok {
			return time.Time{}, false
		}
		hour, minute, hasClock = h, m, true
	}
	if dayPart == "" {
		// A time alone: today if it's still ahead, else tomorrow.
		t := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, loc)
		if !t.After(now) {
			t = t.AddDate(0, 0, 1)
		}
		return t, true
	}
	y, mo, d, ok := parseDay(dayPart, now, loc)
	if !ok {
		return time.Time{}, false
	}
	if dayPart == "tonight" && !hasClock {
		hour = 20
	}
	return time.Date(y, mo, d, hour, minute, 0, 0, loc), true
}

func parseClock(p string) (hour, minute int, ok bool) {
	switch strings.TrimPrefix(p, "at ") {
	case "noon", "midday":
		return 12, 0, true
	case "midnight":
		return 0, 0, true
	}
	m := clockRe.FindStringSubmatch(p)
	if m == nil {
		return 0, 0, false
	}
	h, _ := strconv.Atoi(m[1])
	if m[2] != "" {
		minute, _ = strconv.Atoi(m[2])
	}
	suffix := strings.ReplaceAll(m[3], ".", "")
	// A bare number ("at 3") reads as a clock only with "at"; "3" alone is ambiguous.
	if suffix == "" && m[2] == "" && !strings.HasPrefix(p, "at ") {
		return 0, 0, false
	}
	switch suffix {
	case "am":
		if h < 1 || h > 12 {
			return 0, 0, false
		}
		if h == 12 {
			h = 0
		}
	case "pm":
		if h < 1 || h > 12 {
			return 0, 0, false
		}
		if h != 12 {
			h += 12
		}
	}
	if h > 23 || minute > 59 {
		return 0, 0, false
	}
	return h, minute, true
}

func parseDay(p string, now time.Time, loc *time.Location) (int, time.Month, int, bool) {
	p = strings.TrimPrefix(p, "on ")
	day := func(t time.Time) (int, time.Month, int, bool) { y, m, d := t.Date(); return y, m, d, true }
	switch p {
	case "today", "tonight", "later today":
		return day(now)
	case "tomorrow", "tmrw", "tmr":
		return day(now.AddDate(0, 0, 1))
	case "next week":
		// The coming Monday (a week from Monday when today is Monday).
		ahead := (int(time.Monday) - int(now.Weekday()) + 7) % 7
		if ahead == 0 {
			ahead = 7
		}
		return day(now.AddDate(0, 0, ahead))
	}
	if m := isoDateRe.FindStringSubmatch(p); m != nil {
		y, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[2])
		d, _ := strconv.Atoi(m[3])
		t := time.Date(y, time.Month(mo), d, 0, 0, 0, 0, loc)
		if t.Month() != time.Month(mo) || t.Day() != d {
			return 0, 0, 0, false
		}
		return y, time.Month(mo), d, true
	}
	// "friday" and "next friday" are the coming one; today's weekday means a week from today.
	if wd, ok := weekdays[strings.TrimPrefix(p, "next ")]; ok {
		ahead := (int(wd) - int(now.Weekday()) + 7) % 7
		if ahead == 0 {
			ahead = 7
		}
		return day(now.AddDate(0, 0, ahead))
	}
	return 0, 0, 0, false
}

// remindCommand splits "/remind me …" text into what to remember and when:
// "me to call Sam tomorrow at 3pm", "me in 20 minutes to stretch",
// "me about the launch next week".
func remindCommand(text string, now time.Time, loc *time.Location) (what string, at time.Time, err error) {
	words := strings.Fields(strings.TrimSpace(text))
	if len(words) > 0 && strings.EqualFold(words[0], "me") {
		words = words[1:]
	}
	if len(words) == 0 {
		return "", time.Time{}, errWhenUnknown
	}
	clean := func(ws []string) string {
		s := strings.Join(ws, " ")
		for _, lead := range []string{"to ", "about ", "that "} {
			if len(s) > len(lead) && strings.EqualFold(s[:len(lead)], lead) {
				s = s[len(lead):]
				break
			}
		}
		return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "."))
	}
	var firstErr error
	// The longest time phrase at the end: "… tomorrow at 3pm".
	for i := 1; i < len(words); i++ {
		t, err := parseWhen(strings.Join(words[i:], " "), now, loc)
		if err == nil {
			if w := clean(words[:i]); w != "" {
				return w, t, nil
			}
		} else if firstErr == nil && err != errWhenUnknown {
			firstErr = err
		}
	}
	// Or at the start: "in 20 minutes to stretch".
	for j := len(words) - 1; j >= 1; j-- {
		t, err := parseWhen(strings.Join(words[:j], " "), now, loc)
		if err == nil {
			if w := clean(words[j:]); w != "" {
				return w, t, nil
			}
		} else if firstErr == nil && err != errWhenUnknown {
			firstErr = err
		}
	}
	if firstErr != nil {
		return "", time.Time{}, firstErr
	}
	return "", time.Time{}, errWhenUnknown
}
