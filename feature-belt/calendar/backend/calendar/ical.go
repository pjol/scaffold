package calendar

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// ---- writing (RFC 5545 + iTIP methods from RFC 5546) ----------------------

// icsPerson is an ORGANIZER or ATTENDEE.
type icsPerson struct {
	Name, Email string
	Response    string // needs_action | accepted | tentative | declined (attendees)
	RSVP        bool
}

// icsEvent is one VEVENT. Single events are written in UTC; series masters
// and their exceptions use TZID local times (with a VTIMEZONE) so calendar
// apps expand the rule across daylight-saving changes like we do.
type icsEvent struct {
	UID          string
	Sequence     int
	Start, End   time.Time
	AllDay       bool
	Loc          *time.Location
	Rule         string // RRULE value, masters only
	ExDates      []time.Time
	RecurrenceID *time.Time
	Summary      string
	Description  string
	Location     string
	URL          string
	Cancelled    bool
	Transparent  bool
	Organizer    *icsPerson
	Attendees    []icsPerson
	Created      time.Time
	Modified     time.Time
}

func (e icsEvent) zoned() bool { return !e.AllDay && (e.Rule != "" || e.RecurrenceID != nil) }

// renderICS writes a VCALENDAR. method is PUBLISH (feeds, downloads),
// REQUEST (invites and updates) or CANCEL.
func renderICS(method, name string, events []icsEvent) []byte {
	var b strings.Builder
	line := func(l string) { foldLine(&b, l) }
	line("BEGIN:VCALENDAR")
	line("VERSION:2.0")
	line("PRODID:-//Scaffold App//Calendar//EN")
	line("CALSCALE:GREGORIAN")
	line("METHOD:" + method)
	if name != "" {
		line("X-WR-CALNAME:" + escapeText(name))
	}

	// One VTIMEZONE per zone used by zoned events, covering their span.
	type span struct {
		loc      *time.Location
		from, to time.Time
	}
	zones := map[string]*span{}
	var order []string
	for _, e := range events {
		if !e.zoned() || e.Loc == nil || e.Loc == time.UTC {
			continue
		}
		to := e.End.AddDate(2, 0, 0)
		if e.Rule != "" {
			to = e.Start.AddDate(10, 0, 0)
		}
		z, ok := zones[e.Loc.String()]
		if !ok {
			z = &span{loc: e.Loc, from: e.Start, to: to}
			zones[e.Loc.String()] = z
			order = append(order, e.Loc.String())
		}
		if e.Start.Before(z.from) {
			z.from = e.Start
		}
		if to.After(z.to) {
			z.to = to
		}
	}
	for _, name := range order {
		z := zones[name]
		writeVTimezone(line, z.loc, z.from, z.to)
	}

	stamp := time.Now().UTC().Format(utcStamp)
	for _, e := range events {
		line("BEGIN:VEVENT")
		line("UID:" + e.UID)
		line("DTSTAMP:" + stamp)
		if !e.Created.IsZero() {
			line("CREATED:" + e.Created.UTC().Format(utcStamp))
		}
		if !e.Modified.IsZero() {
			line("LAST-MODIFIED:" + e.Modified.UTC().Format(utcStamp))
		}
		line("SEQUENCE:" + strconv.Itoa(e.Sequence))
		line(dateProp("DTSTART", e.Start, e))
		line(dateProp("DTEND", e.End, e))
		if e.RecurrenceID != nil {
			line(dateProp("RECURRENCE-ID", *e.RecurrenceID, e))
		}
		if e.Rule != "" {
			line("RRULE:" + e.Rule)
			for _, x := range e.ExDates {
				line(dateProp("EXDATE", x, e))
			}
		}
		line("SUMMARY:" + escapeText(e.Summary))
		if e.Description != "" {
			line("DESCRIPTION:" + escapeText(e.Description))
		}
		if e.Location != "" {
			line("LOCATION:" + escapeText(e.Location))
		}
		if e.URL != "" {
			line("URL:" + e.URL)
		}
		if e.Organizer != nil {
			line("ORGANIZER" + personParams(*e.Organizer, false) + ":mailto:" + e.Organizer.Email)
		}
		for _, a := range e.Attendees {
			line("ATTENDEE" + personParams(a, true) + ":mailto:" + a.Email)
		}
		switch {
		case e.Cancelled || method == "CANCEL":
			line("STATUS:CANCELLED")
		default:
			line("STATUS:CONFIRMED")
		}
		if e.Transparent {
			line("TRANSP:TRANSPARENT")
		} else {
			line("TRANSP:OPAQUE")
		}
		line("END:VEVENT")
	}
	line("END:VCALENDAR")
	return []byte(b.String())
}

const utcStamp = "20060102T150405Z"

func dateProp(name string, t time.Time, e icsEvent) string {
	switch {
	case e.AllDay:
		loc := e.Loc
		if loc == nil {
			loc = time.UTC
		}
		return name + ";VALUE=DATE:" + t.In(loc).Format("20060102")
	case e.zoned() && e.Loc != nil && e.Loc != time.UTC:
		return name + ";TZID=" + e.Loc.String() + ":" + t.In(e.Loc).Format("20060102T150405")
	default:
		return name + ":" + t.UTC().Format(utcStamp)
	}
}

var partstat = map[string]string{"needs_action": "NEEDS-ACTION", "accepted": "ACCEPTED", "tentative": "TENTATIVE", "declined": "DECLINED"}

func personParams(p icsPerson, attendee bool) string {
	s := ""
	if p.Name != "" {
		s += ";CN=" + quoteParam(p.Name)
	}
	if attendee {
		s += ";ROLE=REQ-PARTICIPANT;CUTYPE=INDIVIDUAL"
		if ps, ok := partstat[p.Response]; ok {
			s += ";PARTSTAT=" + ps
		}
		if p.RSVP {
			s += ";RSVP=TRUE"
		}
	}
	return s
}

// quoteParam quotes a parameter value; DQUOTE can't appear inside one.
func quoteParam(v string) string {
	v = strings.Map(func(r rune) rune {
		if r == '"' || r < 0x20 {
			return -1
		}
		return r
	}, v)
	return `"` + v + `"`
}

// writeVTimezone describes loc's offsets between from and to as explicit
// observances (one per transition), read from Go's zone database.
func writeVTimezone(line func(string), loc *time.Location, from, to time.Time) {
	line("BEGIN:VTIMEZONE")
	line("TZID:" + loc.String())
	t := from.AddDate(-1, 0, 0).In(loc)
	end := to.AddDate(1, 0, 0)
	_, off := t.Zone()
	observance := func(at time.Time, fromOff, toOff int, dst bool, abbr string) {
		kind := "STANDARD"
		if dst {
			kind = "DAYLIGHT"
		}
		line("BEGIN:" + kind)
		line("DTSTART:" + at.UTC().Add(time.Duration(fromOff)*time.Second).Format("20060102T150405"))
		line("TZOFFSETFROM:" + formatOffset(fromOff))
		line("TZOFFSETTO:" + formatOffset(toOff))
		if abbr != "" && !strings.ContainsAny(abbr, "+-") {
			line("TZNAME:" + abbr)
		}
		line("END:" + kind)
	}
	abbr, _ := t.Zone()
	observance(t, off, off, t.IsDST(), abbr)
	for i := 0; i < 200; i++ {
		_, next := t.ZoneBounds()
		if next.IsZero() || next.After(end) {
			break
		}
		n := next.In(loc)
		abbr, toOff := n.Zone()
		observance(n, off, toOff, n.IsDST(), abbr)
		off, t = toOff, n
	}
	line("END:VTIMEZONE")
}

func formatOffset(sec int) string {
	sign := "+"
	if sec < 0 {
		sign, sec = "-", -sec
	}
	return fmt.Sprintf("%s%02d%02d", sign, sec/3600, sec%3600/60)
}

func escapeText(v string) string {
	r := strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\r\n", `\n`, "\n", `\n`, "\r", `\n`)
	return r.Replace(v)
}

// foldLine writes l with CRLF, folding at 75 octets on rune boundaries.
func foldLine(b *strings.Builder, l string) {
	limit := 75
	for len(l) > limit {
		cut := limit
		for cut > 0 && !utf8.RuneStart(l[cut]) {
			cut--
		}
		b.WriteString(l[:cut])
		b.WriteString("\r\n ")
		l = l[cut:]
		limit = 74 // the leading space counts
	}
	b.WriteString(l)
	b.WriteString("\r\n")
}

// ---- reading (external calendars) ---------------------------------------

type icalProp struct {
	Name   string
	Params map[string]string
	Value  string
}

type icalComponent struct {
	Name     string
	Props    []icalProp
	Children []*icalComponent
}

func (c *icalComponent) prop(name string) (icalProp, bool) {
	for _, p := range c.Props {
		if p.Name == name {
			return p, true
		}
	}
	return icalProp{}, false
}

func (c *icalComponent) props(name string) []icalProp {
	var out []icalProp
	for _, p := range c.Props {
		if p.Name == name {
			out = append(out, p)
		}
	}
	return out
}

var errNotICal = errors.New("not an iCalendar file")

// parseICal reads a VCALENDAR (content lines unfolded first). It is
// tolerant of LF-only files and unknown components.
func parseICal(data []byte) (*icalComponent, error) {
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	var lines []string
	for sc.Scan() {
		l := strings.TrimRight(sc.Text(), "\r")
		if (strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t")) && len(lines) > 0 {
			lines[len(lines)-1] += l[1:]
			continue
		}
		if l != "" {
			lines = append(lines, l)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	var stack []*icalComponent
	var root *icalComponent
	for _, l := range lines {
		p, ok := parseContentLine(l)
		if !ok {
			continue
		}
		switch p.Name {
		case "BEGIN":
			c := &icalComponent{Name: strings.ToUpper(p.Value)}
			if len(stack) > 0 {
				parent := stack[len(stack)-1]
				parent.Children = append(parent.Children, c)
			} else if root == nil {
				root = c
			}
			stack = append(stack, c)
			if len(stack) > 8 {
				return nil, errNotICal
			}
		case "END":
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		default:
			if len(stack) > 0 {
				c := stack[len(stack)-1]
				c.Props = append(c.Props, p)
			}
		}
	}
	if root == nil || root.Name != "VCALENDAR" {
		return nil, errNotICal
	}
	return root, nil
}

// parseContentLine splits NAME;P1=V1;P2="V,2":VALUE.
func parseContentLine(l string) (icalProp, bool) {
	p := icalProp{Params: map[string]string{}}
	i := strings.IndexAny(l, ";:")
	if i <= 0 {
		return p, false
	}
	p.Name = strings.ToUpper(l[:i])
	rest := l[i:]
	for strings.HasPrefix(rest, ";") {
		rest = rest[1:]
		eq := strings.IndexByte(rest, '=')
		if eq <= 0 {
			return p, false
		}
		key := strings.ToUpper(rest[:eq])
		rest = rest[eq+1:]
		var val string
		if strings.HasPrefix(rest, `"`) {
			end := strings.IndexByte(rest[1:], '"')
			if end < 0 {
				return p, false
			}
			val = rest[1 : end+1]
			rest = rest[end+2:]
		} else {
			end := strings.IndexAny(rest, ";:")
			if end < 0 {
				return p, false
			}
			val = rest[:end]
			rest = rest[end:]
		}
		p.Params[key] = val
	}
	if !strings.HasPrefix(rest, ":") {
		return p, false
	}
	p.Value = rest[1:]
	return p, true
}

// windowsZones maps the Windows zone names Outlook writes in TZID to IANA.
var windowsZones = map[string]string{
	"Pacific Standard Time": "America/Los_Angeles", "Mountain Standard Time": "America/Denver",
	"US Mountain Standard Time": "America/Phoenix", "Central Standard Time": "America/Chicago",
	"Eastern Standard Time": "America/New_York", "Alaskan Standard Time": "America/Anchorage",
	"Hawaiian Standard Time": "Pacific/Honolulu", "Atlantic Standard Time": "America/Halifax",
	"GMT Standard Time": "Europe/London", "Greenwich Standard Time": "Atlantic/Reykjavik",
	"W. Europe Standard Time": "Europe/Berlin", "Romance Standard Time": "Europe/Paris",
	"Central Europe Standard Time": "Europe/Budapest", "Central European Standard Time": "Europe/Warsaw",
	"E. Europe Standard Time": "Europe/Chisinau", "FLE Standard Time": "Europe/Kiev",
	"GTB Standard Time": "Europe/Bucharest", "Russian Standard Time": "Europe/Moscow",
	"India Standard Time": "Asia/Kolkata", "China Standard Time": "Asia/Shanghai",
	"Tokyo Standard Time": "Asia/Tokyo", "Korea Standard Time": "Asia/Seoul",
	"Singapore Standard Time": "Asia/Singapore", "AUS Eastern Standard Time": "Australia/Sydney",
	"E. Australia Standard Time": "Australia/Brisbane", "New Zealand Standard Time": "Pacific/Auckland",
	"South Africa Standard Time": "Africa/Johannesburg", "Arabian Standard Time": "Asia/Dubai",
	"Israel Standard Time": "Asia/Jerusalem", "Turkey Standard Time": "Europe/Istanbul",
	"SA Pacific Standard Time": "America/Bogota", "E. South America Standard Time": "America/Sao_Paulo",
	"Argentina Standard Time": "America/Argentina/Buenos_Aires", "Central Standard Time (Mexico)": "America/Mexico_City",
	"UTC": "UTC", "Coordinated Universal Time": "UTC",
}

// zoneResolver turns TZID values into locations: IANA names, Windows
// names, or (last resort) the fixed standard offset from the file's own
// VTIMEZONE.
type zoneResolver struct {
	def   *time.Location
	fixed map[string]*time.Location
	cache map[string]*time.Location
}

func newZoneResolver(cal *icalComponent, def *time.Location) *zoneResolver {
	z := &zoneResolver{def: def, fixed: map[string]*time.Location{}, cache: map[string]*time.Location{}}
	for _, c := range cal.Children {
		if c.Name != "VTIMEZONE" {
			continue
		}
		id, ok := c.prop("TZID")
		if !ok {
			continue
		}
		for _, sub := range c.Children {
			if sub.Name != "STANDARD" {
				continue
			}
			if to, ok := sub.prop("TZOFFSETTO"); ok {
				if sec, ok := parseOffset(to.Value); ok {
					z.fixed[id.Value] = time.FixedZone(id.Value, sec)
				}
			}
		}
	}
	return z
}

func (z *zoneResolver) resolve(tzid string) *time.Location {
	tzid = strings.Trim(tzid, `"`)
	if tzid == "" {
		return z.def
	}
	if l, ok := z.cache[tzid]; ok {
		return l
	}
	l := z.def
	if loc, err := time.LoadLocation(strings.TrimPrefix(tzid, "/")); err == nil {
		l = loc
	} else if iana, ok := windowsZones[tzid]; ok {
		if loc, err := time.LoadLocation(iana); err == nil {
			l = loc
		}
	} else if f, ok := z.fixed[tzid]; ok {
		l = f
	}
	z.cache[tzid] = l
	return l
}

func parseOffset(v string) (int, bool) {
	if len(v) != 5 && len(v) != 7 {
		return 0, false
	}
	sign := 1
	switch v[0] {
	case '-':
		sign = -1
	case '+':
	default:
		return 0, false
	}
	h, err1 := strconv.Atoi(v[1:3])
	m, err2 := strconv.Atoi(v[3:5])
	if err1 != nil || err2 != nil {
		return 0, false
	}
	return sign * (h*3600 + m*60), true
}

// parseICalTime reads a DATE or DATE-TIME property value. allDay reports a
// DATE (floating, read as midnight in the resolver's default zone).
func (z *zoneResolver) parseTime(p icalProp, raw string) (t time.Time, loc *time.Location, allDay bool, err error) {
	raw = strings.TrimSpace(raw)
	if p.Params["VALUE"] == "DATE" || len(raw) == 8 {
		t, err = time.ParseInLocation("20060102", raw, z.def)
		return t, z.def, true, err
	}
	if strings.HasSuffix(raw, "Z") {
		t, err = time.Parse(utcStamp, raw)
		return t, time.UTC, false, err
	}
	loc = z.resolve(p.Params["TZID"])
	t, err = time.ParseInLocation("20060102T150405", raw, loc)
	return t, loc, false, err
}

// parseDuration reads an RFC 5545 duration (P1D, PT1H30M, P2W, -PT15M).
func parseDuration(v string) (time.Duration, bool) {
	v = strings.TrimSpace(strings.ToUpper(v))
	neg := strings.HasPrefix(v, "-")
	v = strings.TrimLeft(v, "+-")
	if !strings.HasPrefix(v, "P") {
		return 0, false
	}
	v = v[1:]
	var d time.Duration
	inTime := false
	num := ""
	for _, r := range v {
		switch {
		case r >= '0' && r <= '9':
			num += string(r)
		case r == 'T':
			inTime = true
		default:
			n, err := strconv.Atoi(num)
			if err != nil {
				return 0, false
			}
			num = ""
			switch {
			case r == 'W':
				d += time.Duration(n) * 7 * 24 * time.Hour
			case r == 'D':
				d += time.Duration(n) * 24 * time.Hour
			case r == 'H' && inTime:
				d += time.Duration(n) * time.Hour
			case r == 'M' && inTime:
				d += time.Duration(n) * time.Minute
			case r == 'S' && inTime:
				d += time.Duration(n) * time.Second
			default:
				return 0, false
			}
		}
	}
	if num != "" {
		return 0, false
	}
	if neg {
		d = -d
	}
	return d, true
}

// busySpan is a busy interval read from an external calendar.
type busySpan struct{ Start, End time.Time }

// icalBusy turns an external calendar into busy intervals overlapping
// [from, to): opaque, non-cancelled events, with recurring series expanded
// (EXDATE and RECURRENCE-ID overrides honoured). All-day events count only
// when the file marks them TRANSP:OPAQUE explicitly, since calendar apps
// export birthdays and holidays as all-day entries. Floating times and
// dates are read in def, the owner's zone. At most max spans.
func icalBusy(data []byte, def *time.Location, from, to time.Time, max int) ([]busySpan, error) {
	cal, err := parseICal(data)
	if err != nil {
		return nil, err
	}
	z := newZoneResolver(cal, def)
	type vevent struct {
		c            *icalComponent
		start, end   time.Time
		loc          *time.Location
		allDay       bool
		recurrenceID *time.Time
		busy         bool
	}
	read := func(c *icalComponent) (vevent, bool) {
		ev := vevent{c: c}
		ds, ok := c.prop("DTSTART")
		if !ok {
			return ev, false
		}
		var err error
		if ev.start, ev.loc, ev.allDay, err = z.parseTime(ds, ds.Value); err != nil {
			return ev, false
		}
		if de, ok := c.prop("DTEND"); ok {
			if ev.end, _, _, err = z.parseTime(de, de.Value); err != nil {
				return ev, false
			}
		} else if du, ok := c.prop("DURATION"); ok {
			d, ok := parseDuration(du.Value)
			if !ok {
				return ev, false
			}
			ev.end = ev.start.Add(d)
		} else if ev.allDay {
			ev.end = ev.start.AddDate(0, 0, 1)
		} else {
			ev.end = ev.start
		}
		if rid, ok := c.prop("RECURRENCE-ID"); ok {
			t, _, _, err := z.parseTime(rid, rid.Value)
			if err == nil {
				ev.recurrenceID = &t
			}
		}
		status, _ := c.prop("STATUS")
		transp, hasTransp := c.prop("TRANSP")
		ev.busy = !strings.EqualFold(status.Value, "CANCELLED") && !strings.EqualFold(transp.Value, "TRANSPARENT")
		if ev.allDay && !(hasTransp && strings.EqualFold(transp.Value, "OPAQUE")) {
			ev.busy = false
		}
		return ev, ev.end.After(ev.start) || ev.recurrenceID != nil
	}

	masters := map[string]vevent{}
	overrides := map[string][]vevent{}
	var singles []vevent
	for _, c := range cal.Children {
		if c.Name != "VEVENT" {
			continue
		}
		ev, ok := read(c)
		if !ok {
			continue
		}
		uid, _ := c.prop("UID")
		switch {
		case ev.recurrenceID != nil:
			overrides[uid.Value] = append(overrides[uid.Value], ev)
		case hasProp(c, "RRULE") || hasProp(c, "RDATE"):
			masters[uid.Value] = ev
		default:
			singles = append(singles, ev)
		}
	}

	var out []busySpan
	add := func(s, e time.Time) {
		if len(out) < max && s.Before(to) && e.After(from) && e.After(s) {
			out = append(out, busySpan{s.UTC(), e.UTC()})
		}
	}
	for _, ev := range singles {
		if ev.busy {
			add(ev.start, ev.end)
		}
	}
	for uid, ev := range masters {
		dur := ev.end.Sub(ev.start)
		skip := map[int64]bool{}
		for _, x := range ev.c.props("EXDATE") {
			for v := range strings.SplitSeq(x.Value, ",") {
				if t, _, _, err := z.parseTime(x, v); err == nil {
					skip[t.Unix()] = true
				}
			}
		}
		for _, o := range overrides[uid] {
			skip[o.recurrenceID.Unix()] = true
		}
		if ev.busy {
			var starts []time.Time
			if rr, ok := ev.c.prop("RRULE"); ok {
				if rule, err := ParseRule(rr.Value, ev.loc); err == nil {
					starts = rule.Expand(ev.start, ev.loc, from.Add(-dur), to, max, skip)
				} else if !skip[ev.start.Unix()] {
					starts = []time.Time{ev.start} // outside our subset: the first occurrence only
				}
			}
			for _, rd := range ev.c.props("RDATE") {
				if rd.Params["VALUE"] == "PERIOD" {
					continue
				}
				for v := range strings.SplitSeq(rd.Value, ",") {
					if t, _, _, err := z.parseTime(rd, v); err == nil && !skip[t.Unix()] {
						starts = append(starts, t)
					}
				}
			}
			for _, s := range starts {
				add(s, s.Add(dur))
			}
		}
		for _, o := range overrides[uid] {
			if o.busy {
				add(o.start, o.end)
			}
		}
		delete(overrides, uid)
	}
	// Overrides whose master isn't in the file still describe real time.
	for _, list := range overrides {
		for _, o := range list {
			if o.busy {
				add(o.start, o.end)
			}
		}
	}
	slices.SortFunc(out, func(a, b busySpan) int { return a.Start.Compare(b.Start) })
	return out, nil
}

func hasProp(c *icalComponent, name string) bool {
	_, ok := c.prop(name)
	return ok
}

// readLimited reads at most limit bytes, failing when the body is larger.
func readLimited(r io.Reader, limit int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, errTooLarge
	}
	return b, nil
}

var errTooLarge = errors.New("calendar file too large")
