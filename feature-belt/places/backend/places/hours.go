package places

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"scaffoldapp/backend/internal/httpx"
)

func badf(format string, a ...any) error { return httpx.BadRequest(fmt.Sprintf(format, a...)) }

// Hours are a place's weekly opening hours, Monday first. A day with no
// intervals is closed; a close earlier than its open runs past midnight
// (18:00–02:00). Several intervals per day cover split shifts.
type Hours struct {
	Timezone string `json:"timezone"` // IANA zone, "" = the viewer's local time
	Note     string `json:"note"`     // "Closed on public holidays"
	Weekly   Week   `json:"weekly"`
}

// Week holds each day's intervals.
type Week struct {
	Mon []Interval `json:"mon"`
	Tue []Interval `json:"tue"`
	Wed []Interval `json:"wed"`
	Thu []Interval `json:"thu"`
	Fri []Interval `json:"fri"`
	Sat []Interval `json:"sat"`
	Sun []Interval `json:"sun"`
}

// Interval is one opening, "HH:MM" 24-hour local time; close may be 24:00.
type Interval struct {
	Open  string `json:"open"`
	Close string `json:"close"`
}

const maxIntervalsPerDay = 4

func (w *Week) days() []struct {
	name string
	list *[]Interval
} {
	return []struct {
		name string
		list *[]Interval
	}{{"Monday", &w.Mon}, {"Tuesday", &w.Tue}, {"Wednesday", &w.Wed}, {"Thursday", &w.Thu},
		{"Friday", &w.Fri}, {"Saturday", &w.Sat}, {"Sunday", &w.Sun}}
}

// parseHours decodes and validates client hours. raw "null" clears them.
func parseHours(raw json.RawMessage) (*Hours, error) {
	if len(raw) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		return nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var h Hours
	if err := dec.Decode(&h); err != nil {
		return nil, badf("Hours need timezone, note and weekly with mon to sun lists of {open, close}.")
	}
	h.Timezone = strings.TrimSpace(h.Timezone)
	if h.Timezone != "" {
		if h.Timezone == "Local" {
			return nil, badf("Use an IANA time zone like America/Los_Angeles.")
		}
		if _, err := time.LoadLocation(h.Timezone); err != nil {
			return nil, badf("Unknown time zone %q. Use an IANA zone like America/Los_Angeles.", h.Timezone)
		}
	}
	h.Note = strings.TrimSpace(h.Note)
	if len([]rune(h.Note)) > 200 {
		return nil, badf("The hours note must be 200 characters or fewer.")
	}
	for _, d := range h.Weekly.days() {
		if *d.list == nil {
			*d.list = []Interval{}
		}
		if len(*d.list) > maxIntervalsPerDay {
			return nil, badf("%s has more than %d openings.", d.name, maxIntervalsPerDay)
		}
		for i, iv := range *d.list {
			open, ok1 := clockMinutes(iv.Open)
			closing, ok2 := clockMinutes(iv.Close)
			if !ok1 || !ok2 || open == 24*60 {
				return nil, badf("%s hours must be HH:MM, like 09:00 to 17:30.", d.name)
			}
			if open == closing {
				return nil, badf("%s opens and closes at the same time.", d.name)
			}
			(*d.list)[i] = Interval{Open: iv.Open, Close: iv.Close}
		}
	}
	return &h, nil
}

// clockMinutes parses "HH:MM" (00:00 to 24:00) into minutes after midnight.
func clockMinutes(s string) (int, bool) {
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
