package calendar

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"scaffoldapp/backend/internal/httpx"
)

// Microsoft Graph (Outlook calendars), from the connection's endpoints
// ("graph", else "api", else graph.microsoft.com/v1.0). Times are asked for
// in UTC (Prefer: outlook.timezone). The default calendar is synced per
// month with calendarView/delta (expanded occurrences, @removed for
// deletions, a 410 starts the month over); other calendars and windows
// outside the horizon are read live with calendarView. Busy time for the
// default calendar comes from getSchedule.

type graphCalendar struct {
	p    platform
	base string
	conn *calConn
}

func newGraphCalendar(c *calConn, client *http.Client) *graphCalendar {
	base := c.Endpoints["graph"]
	if base == "" {
		base = c.Endpoints["api"]
	}
	if base == "" {
		base = "https://graph.microsoft.com/v1.0"
	}
	return &graphCalendar{
		p:    platform{client: client, name: "Outlook Calendar", headers: map[string]string{"Prefer": `outlook.timezone="UTC", odata.maxpagesize=100`}},
		base: strings.TrimRight(base, "/"), conn: c,
	}
}

type msTime struct {
	DateTime string `json:"dateTime"`
	TimeZone string `json:"timeZone"`
}

type msEmail struct {
	Name    string `json:"name,omitempty"`
	Address string `json:"address"`
}

type msAttendee struct {
	Type   string `json:"type,omitempty"`
	Status *struct {
		Response string `json:"response"`
	} `json:"status,omitempty"`
	EmailAddress msEmail `json:"emailAddress"`
}

type msEvent struct {
	ID      string `json:"id"`
	Removed *struct {
		Reason string `json:"reason"`
	} `json:"@removed"`
	ChangeKey             string `json:"changeKey"`
	Subject               string `json:"subject"`
	BodyPreview           string `json:"bodyPreview"`
	Body                  *struct {
		ContentType string `json:"contentType"`
		Content     string `json:"content"`
	} `json:"body"`
	Start                 msTime       `json:"start"`
	End                   msTime       `json:"end"`
	IsAllDay              bool         `json:"isAllDay"`
	IsCancelled           bool         `json:"isCancelled"`
	IsOrganizer           bool         `json:"isOrganizer"`
	ShowAs                string       `json:"showAs"`
	Type                  string       `json:"type"`
	SeriesMasterID        string       `json:"seriesMasterId"`
	WebLink               string       `json:"webLink"`
	Location              *struct {
		DisplayName string `json:"displayName"`
	} `json:"location"`
	Organizer *struct {
		EmailAddress msEmail `json:"emailAddress"`
	} `json:"organizer"`
	Attendees      []msAttendee `json:"attendees"`
	ResponseStatus *struct {
		Response string `json:"response"`
	} `json:"responseStatus"`
	IsOnlineMeeting       bool   `json:"isOnlineMeeting"`
	OnlineMeetingProvider string `json:"onlineMeetingProvider"`
	OnlineMeeting         *struct {
		JoinURL string `json:"joinUrl"`
	} `json:"onlineMeeting"`
	OriginalStartTimeZone string `json:"originalStartTimeZone"`
	LastModifiedDateTime  string `json:"lastModifiedDateTime"`
	Sensitivity           string `json:"sensitivity"`
}

type msEventPage struct {
	Value     []msEvent `json:"value"`
	NextLink  string    `json:"@odata.nextLink"`
	DeltaLink string    `json:"@odata.deltaLink"`
}

func (g *graphCalendar) calendars(ctx context.Context) ([]extCalendar, error) {
	var out []extCalendar
	next := g.base + "/me/calendars?$top=100"
	for range 10 {
		var res struct {
			Value []struct {
				ID                            string   `json:"id"`
				Name                          string   `json:"name"`
				IsDefaultCalendar             bool     `json:"isDefaultCalendar"`
				CanEdit                       bool     `json:"canEdit"`
				AllowedOnlineMeetingProviders []string `json:"allowedOnlineMeetingProviders"`
			} `json:"value"`
			NextLink string `json:"@odata.nextLink"`
		}
		if err := g.p.call(ctx, http.MethodGet, next, nil, &res); err != nil {
			return nil, err
		}
		for _, c := range res.Value {
			cal := extCalendar{ID: c.ID, Name: c.Name, Primary: c.IsDefaultCalendar, CanWrite: c.CanEdit && g.conn.writeCap != "",
				AccessRole: "reader", platform: platformMicrosoft, connID: g.conn.ID, account: g.conn.Account}
			if c.CanEdit {
				cal.AccessRole = "writer"
			}
			if slices.Contains(c.AllowedOnlineMeetingProviders, "teamsForBusiness") {
				cal.Meeting = ProviderTeams
			}
			out = append(out, cal)
		}
		if next = res.NextLink; next == "" {
			break
		}
	}
	slices.SortStableFunc(out, func(a, b extCalendar) int {
		if a.Primary != b.Primary {
			if a.Primary {
				return -1
			}
			return 1
		}
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return out, nil
}

func graphWindow(from, to time.Time) string {
	return "startDateTime=" + url.QueryEscape(from.UTC().Format("2006-01-02T15:04:05Z")) + "&endDateTime=" + url.QueryEscape(to.UTC().Format("2006-01-02T15:04:05Z"))
}

// events returns cal's occurrences overlapping [from, to).
func (g *graphCalendar) events(ctx context.Context, cal extCalendar, st *calState, from, to time.Time) ([]extEvent, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	lo, hi := connectedHorizon(time.Now())
	if !cal.Primary || from.Before(lo) || to.After(hi) {
		return g.liveWindow(ctx, cal, st, from, to)
	}
	seen := map[string]bool{}
	var out []extEvent
	for m := time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, time.UTC); m.Before(to); m = m.AddDate(0, 1, 0) {
		b, err := g.syncMonth(ctx, cal, st, m)
		if err != nil {
			return nil, err
		}
		for id, e := range b.items {
			if !seen[id] && e.overlaps(from, to) && e.Status != "cancelled" {
				seen[id] = true
				out = append(out, e)
			}
		}
	}
	return out, nil
}

// syncMonth brings one month of the default calendar up to date with
// calendarView/delta; a stale delta link (410) starts the month over once.
func (g *graphCalendar) syncMonth(ctx context.Context, cal extCalendar, st *calState, month time.Time) (*graphBucket, error) {
	key := month.Format("2006-01")
	if st.buckets == nil {
		st.buckets = map[string]*graphBucket{}
	}
	b := st.buckets[key]
	if b == nil {
		b = &graphBucket{}
		st.buckets[key] = b
	}
	if b.items != nil && time.Since(b.synced) < syncEvery {
		return b, nil
	}
	for attempt := 0; attempt < 2; attempt++ {
		full := b.delta == "" || b.items == nil
		items := b.items
		if full {
			items = map[string]extEvent{}
		}
		next := b.delta
		if full {
			next = g.base + "/me/calendarView/delta?" + graphWindow(month, month.AddDate(0, 1, 0))
		}
		delta := ""
		var err error
		for range 60 {
			var page msEventPage
			if err = g.p.call(ctx, http.MethodGet, next, nil, &page); err != nil {
				break
			}
			for _, me := range page.Value {
				if me.Removed != nil || me.IsCancelled {
					delete(items, me.ID)
					continue
				}
				items[me.ID] = g.toExt(me, cal)
			}
			if page.NextLink != "" {
				next = page.NextLink
				continue
			}
			delta = page.DeltaLink
			break
		}
		if errors.Is(err, errSyncGone) && !full {
			b.delta, b.items = "", nil
			continue
		}
		if err != nil {
			return nil, err
		}
		b.items, b.delta, b.synced = items, delta, time.Now()
		return b, nil
	}
	return nil, httpx.NewError(http.StatusBadGateway, "connection_unavailable", "Outlook couldn't start a fresh sync. Try again.")
}

// liveWindow reads [from, to) with calendarView, cached briefly.
func (g *graphCalendar) liveWindow(ctx context.Context, cal extCalendar, st *calState, from, to time.Time) ([]extEvent, error) {
	key := strconv.FormatInt(from.Unix(), 10) + "|" + strconv.FormatInt(to.Unix(), 10)
	if v, ok := st.view(key); ok {
		return v, nil
	}
	var out []extEvent
	next := g.base + "/me/calendars/" + url.PathEscape(cal.ID) + "/calendarView?" + graphWindow(from, to) + "&$top=100"
	for range 20 {
		var page msEventPage
		if err := g.p.call(ctx, http.MethodGet, next, nil, &page); err != nil {
			return nil, err
		}
		for _, me := range page.Value {
			if !me.IsCancelled {
				out = append(out, g.toExt(me, cal))
			}
		}
		if next = page.NextLink; next == "" {
			break
		}
	}
	st.putView(key, out)
	return out, nil
}

// parseGraphTime reads "2026-11-02T17:00:00.0000000" in its zone (UTC when
// asked for with outlook.timezone).
func parseGraphTime(t msTime) (time.Time, bool) {
	loc := time.UTC
	if t.TimeZone != "" && !strings.EqualFold(t.TimeZone, "UTC") {
		if iana, ok := windowsZones[t.TimeZone]; ok {
			loc = zoneOr(iana, time.UTC)
		} else {
			loc = zoneOr(t.TimeZone, time.UTC)
		}
	}
	v, err := time.ParseInLocation("2006-01-02T15:04:05.9999999", strings.TrimSuffix(t.DateTime, "Z"), loc)
	if err != nil {
		return time.Time{}, false
	}
	return v.UTC(), true
}

var graphResponses = map[string]string{"organizer": "", "accepted": ResponseAccepted, "tentativelyAccepted": ResponseTentative,
	"declined": ResponseDeclined, "notResponded": ResponseNeedsAction, "none": ResponseNeedsAction}

func (g *graphCalendar) toExt(me msEvent, cal extCalendar) extEvent {
	e := extEvent{ID: me.ID, CalendarID: cal.ID, SeriesID: me.SeriesMasterID, Title: me.Subject, Status: "confirmed",
		WebURL: me.WebLink, Stamp: me.ChangeKey, Private: me.Sensitivity == "private" || me.Sensitivity == "confidential",
		Recurring: me.Type == "occurrence" || me.Type == "exception" || me.Type == "seriesMaster"}
	if me.IsCancelled {
		e.Status = "cancelled"
	}
	if e.Title == "" {
		e.Title = "(No title)"
	}
	if me.Location != nil {
		e.Location = me.Location.DisplayName
	}
	if me.Body != nil {
		e.Description = me.Body.Content
		if strings.EqualFold(me.Body.ContentType, "html") {
			e.Description = me.BodyPreview
		}
	}
	switch me.ShowAs {
	case "free", "workingElsewhere":
		e.ShowAs = "free"
	default:
		e.ShowAs = "busy"
	}
	tz := me.OriginalStartTimeZone
	if iana, ok := windowsZones[tz]; ok {
		tz = iana
	}
	if !validZone(tz) {
		tz = cal.Timezone
	}
	if !validZone(tz) {
		tz = "UTC"
	}
	e.Timezone = tz
	start, _ := parseGraphTime(me.Start)
	end, _ := parseGraphTime(me.End)
	if me.IsAllDay {
		// All-day events float: midnight to midnight in whatever zone was
		// asked for, so the date part is the day.
		e.AllDay = true
		sd, ed := me.Start.DateTime, me.End.DateTime
		if len(sd) >= 10 && len(ed) >= 10 {
			a, err1 := time.Parse(time.DateOnly, sd[:10])
			b, err2 := time.Parse(time.DateOnly, ed[:10])
			if err1 == nil && err2 == nil {
				if !b.After(a) {
					b = a.AddDate(0, 0, 1)
				}
				loc := zoneOr(tz, time.UTC)
				e.StartDate, e.EndDate = a.Format(time.DateOnly), b.AddDate(0, 0, -1).Format(time.DateOnly)
				start = time.Date(a.Year(), a.Month(), a.Day(), 0, 0, 0, 0, loc).UTC()
				end = time.Date(b.Year(), b.Month(), b.Day(), 0, 0, 0, 0, loc).UTC()
			}
		}
	}
	if !end.After(start) {
		end = start.Add(time.Minute)
	}
	e.Start, e.End = start, end
	if t, err := time.Parse(time.RFC3339, me.LastModifiedDateTime); err == nil {
		e.Updated = t
	}
	if me.Organizer != nil {
		e.Organizer = extAttendee{Email: me.Organizer.EmailAddress.Address, Name: me.Organizer.EmailAddress.Name, Organizer: true, Self: me.IsOrganizer}
	}
	e.OrganizerSelf = me.IsOrganizer
	for _, a := range me.Attendees {
		x := extAttendee{Email: a.EmailAddress.Address, Name: a.EmailAddress.Name, Optional: a.Type == "optional", Resource: a.Type == "resource",
			Response: ResponseNeedsAction}
		if a.Status != nil {
			if r := graphResponses[a.Status.Response]; r != "" {
				x.Response = r
			}
		}
		if strings.EqualFold(x.Email, g.conn.Account) {
			x.Self = true
		}
		e.Attendees = append(e.Attendees, x)
	}
	if !me.IsOrganizer {
		e.MyResponse = ResponseNeedsAction
		if me.ResponseStatus != nil {
			if r, ok := graphResponses[me.ResponseStatus.Response]; ok && r != "" {
				e.MyResponse = r
			}
		}
	}
	if me.OnlineMeeting != nil && me.OnlineMeeting.JoinURL != "" {
		e.ConferenceURL = me.OnlineMeeting.JoinURL
		switch me.OnlineMeetingProvider {
		case "teamsForBusiness", "skypeForConsumer", "":
			e.ConferenceProvider = ProviderTeams
		}
	}
	if e.ConferenceURL == "" {
		e.ConferenceURL, e.ConferenceProvider = meetingIn(e.Location, e.Description)
	}
	e.CanEdit = cal.CanWrite && me.IsOrganizer
	return e
}

func (g *graphCalendar) event(ctx context.Context, calID, eventID string) (extEvent, error) {
	var me msEvent
	if err := g.p.call(ctx, http.MethodGet, g.base+"/me/events/"+url.PathEscape(eventID), nil, &me); err != nil {
		return extEvent{}, err
	}
	return g.toExt(me, g.calendarStub(calID)), nil
}

func (g *graphCalendar) calendarStub(calID string) extCalendar {
	return extCalendar{ID: calID, CanWrite: g.conn.writeCap != "", platform: platformMicrosoft, connID: g.conn.ID, account: g.conn.Account}
}

// windowsZoneOf is the Windows name Outlook prefers for an IANA zone, when
// the mapped set has one.
func windowsZoneOf(iana string) (string, bool) {
	if iana == "" || iana == "UTC" {
		return "UTC", true
	}
	best := ""
	for win, z := range windowsZones {
		if z == iana && (best == "" || win < best) {
			best = win
		}
	}
	return best, best != ""
}

var graphDays = [...]string{"sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday"}
var graphIndex = map[int]string{1: "first", 2: "second", 3: "third", 4: "fourth", -1: "last"}

// graphRecurrence is a rule as Graph's patternedRecurrence, for the parts
// both sides can say (one monthly date or nth weekday).
func graphRecurrence(r Rule, start time.Time, loc *time.Location, zone string) (map[string]any, error) {
	ls := start.In(loc)
	pattern := map[string]any{"interval": max(r.Interval, 1), "firstDayOfWeek": "monday"}
	switch r.Freq {
	case "DAILY":
		pattern["type"] = "daily"
	case "WEEKLY":
		pattern["type"] = "weekly"
		days := []string{}
		for _, d := range r.ByDay {
			days = append(days, graphDays[d.Day])
		}
		if len(days) == 0 {
			days = []string{graphDays[ls.Weekday()]}
		}
		pattern["daysOfWeek"] = days
	case "MONTHLY":
		switch {
		case len(r.ByDay) == 1 && graphIndex[r.ByDay[0].N] != "":
			pattern["type"] = "relativeMonthly"
			pattern["daysOfWeek"] = []string{graphDays[r.ByDay[0].Day]}
			pattern["index"] = graphIndex[r.ByDay[0].N]
		case len(r.ByDay) == 0 && len(r.ByMonthDay) <= 1:
			pattern["type"] = "absoluteMonthly"
			day := ls.Day()
			if len(r.ByMonthDay) == 1 && r.ByMonthDay[0] > 0 {
				day = r.ByMonthDay[0]
			}
			pattern["dayOfMonth"] = day
		default:
			return nil, httpx.BadRequest("Outlook repeats monthly on one date or one weekday (like the second Tuesday).")
		}
	case "YEARLY":
		pattern["type"] = "absoluteYearly"
		pattern["dayOfMonth"] = ls.Day()
		pattern["month"] = int(ls.Month())
	default:
		return nil, httpx.BadRequest("That repeat isn't supported.")
	}
	rng := map[string]any{"type": "noEnd", "startDate": ls.Format(time.DateOnly), "recurrenceTimeZone": zone}
	switch {
	case r.Count > 0:
		rng["type"], rng["numberOfOccurrences"] = "numbered", r.Count
	case r.Until != nil:
		rng["type"], rng["endDate"] = "endDate", r.Until.In(loc).Format(time.DateOnly)
	}
	return map[string]any{"pattern": pattern, "range": rng}, nil
}

func (g *graphCalendar) body(w extWrite, create bool) (map[string]any, error) {
	b := map[string]any{}
	if w.Title != nil {
		b["subject"] = *w.Title
	}
	desc := w.Description
	if w.JoinURL != "" {
		d := ""
		if desc != nil {
			d = *desc
		}
		d = withJoin(d, w.JoinLabel, w.JoinURL)
		desc = &d
		if w.Location == nil || *w.Location == "" {
			l := w.JoinURL
			w.Location = &l
		}
	}
	if desc != nil {
		b["body"] = map[string]string{"contentType": "text", "content": *desc}
	}
	if w.Location != nil {
		b["location"] = map[string]string{"displayName": *w.Location}
	}
	zone, ok := windowsZoneOf(w.Timezone)
	if !ok {
		zone = "UTC"
	}
	if w.TimeSet {
		b["isAllDay"] = w.AllDay
		if w.AllDay {
			end, _ := time.Parse(time.DateOnly, w.EndDate)
			b["start"] = msTime{DateTime: w.StartDate + "T00:00:00", TimeZone: zone}
			b["end"] = msTime{DateTime: end.AddDate(0, 0, 1).Format(time.DateOnly) + "T00:00:00", TimeZone: zone}
		} else {
			loc := time.UTC
			if zone != "UTC" {
				loc = zoneOr(w.Timezone, time.UTC)
			}
			b["start"] = msTime{DateTime: w.Start.In(loc).Format("2006-01-02T15:04:05"), TimeZone: zone}
			b["end"] = msTime{DateTime: w.End.In(loc).Format("2006-01-02T15:04:05"), TimeZone: zone}
		}
	}
	if w.ShowAs != nil {
		b["showAs"] = *w.ShowAs
	}
	if w.Attendees != nil {
		list := make([]msAttendee, 0, len(*w.Attendees))
		for _, a := range *w.Attendees {
			t := "required"
			if a.Optional {
				t = "optional"
			}
			list = append(list, msAttendee{Type: t, EmailAddress: msEmail{Address: a.Email, Name: a.Name}})
		}
		b["attendees"] = list
	}
	if w.RuleSet {
		if w.Rule == nil {
			b["recurrence"] = nil
		} else {
			if !w.TimeSet {
				return nil, httpx.BadRequest("Send the event's times with a repeat change.")
			}
			rec, err := graphRecurrence(*w.Rule, w.Start, zoneOr(w.Timezone, time.UTC), zone)
			if err != nil {
				return nil, err
			}
			b["recurrence"] = rec
		}
	}
	if w.Meeting == ProviderTeams {
		b["isOnlineMeeting"] = true
		b["onlineMeetingProvider"] = "teamsForBusiness"
	}
	return b, nil
}

// Outlook sends invitations and updates to guests itself whenever an
// organizer's event changes; it has no switch for it.
func (g *graphCalendar) create(ctx context.Context, calID string, w extWrite) (extEvent, error) {
	body, err := g.body(w, true)
	if err != nil {
		return extEvent{}, err
	}
	var me msEvent
	if err := g.p.call(ctx, http.MethodPost, g.base+"/me/calendars/"+url.PathEscape(calID)+"/events", body, &me); err != nil {
		return extEvent{}, err
	}
	return g.toExt(me, g.calendarStub(calID)), nil
}

func (g *graphCalendar) update(ctx context.Context, calID, eventID string, w extWrite) (extEvent, error) {
	if w.Meeting == "remove" {
		return extEvent{}, httpx.Conflict("Outlook keeps a Teams meeting once it's added. Remove it in Outlook.")
	}
	body, err := g.body(w, false)
	if err != nil {
		return extEvent{}, err
	}
	var me msEvent
	if err := g.p.call(ctx, http.MethodPatch, g.base+"/me/events/"+url.PathEscape(eventID), body, &me); err != nil {
		return extEvent{}, err
	}
	return g.toExt(me, g.calendarStub(calID)), nil
}

func (g *graphCalendar) remove(ctx context.Context, calID, eventID string, _ bool) error {
	err := g.p.call(ctx, http.MethodDelete, g.base+"/me/events/"+url.PathEscape(eventID), nil, nil)
	if errors.Is(err, errPlatformGone) || errors.Is(err, errSyncGone) {
		return nil
	}
	return err
}

func (g *graphCalendar) respond(ctx context.Context, calID, eventID, response string) (extEvent, error) {
	action := map[string]string{ResponseAccepted: "accept", ResponseTentative: "tentativelyAccept", ResponseDeclined: "decline"}[response]
	if action == "" {
		return extEvent{}, httpx.BadRequest("response must be accepted, tentative or declined.")
	}
	if err := g.p.call(ctx, http.MethodPost, g.base+"/me/events/"+url.PathEscape(eventID)+"/"+action, map[string]any{"sendResponse": true}, nil); err != nil {
		return extEvent{}, err
	}
	return g.event(ctx, calID, eventID)
}

// freeBusy reads the default calendar's busy time with getSchedule (by the
// account's address) and other calendars' with calendarView.
func (g *graphCalendar) freeBusy(ctx context.Context, cals []extCalendar, from, to time.Time) ([]busySpan, error) {
	var out []busySpan
	for _, c := range cals {
		if c.Primary && strings.Contains(g.conn.Account, "@") {
			var res struct {
				Value []struct {
					ScheduleItems []struct {
						Status string `json:"status"`
						Start  msTime `json:"start"`
						End    msTime `json:"end"`
					} `json:"scheduleItems"`
				} `json:"value"`
			}
			body := map[string]any{"schedules": []string{g.conn.Account}, "startTime": msTime{DateTime: from.UTC().Format("2006-01-02T15:04:05"), TimeZone: "UTC"},
				"endTime": msTime{DateTime: to.UTC().Format("2006-01-02T15:04:05"), TimeZone: "UTC"}, "availabilityViewInterval": 15}
			if err := g.p.call(ctx, http.MethodPost, g.base+"/me/calendar/getSchedule", body, &res); err != nil {
				return nil, err
			}
			for _, v := range res.Value {
				for _, it := range v.ScheduleItems {
					if it.Status == "free" || it.Status == "workingElsewhere" {
						continue
					}
					a, ok1 := parseGraphTime(it.Start)
					z, ok2 := parseGraphTime(it.End)
					if ok1 && ok2 && z.After(a) {
						out = append(out, busySpan{Start: a, End: z})
					}
				}
			}
			continue
		}
		st := &calState{}
		evs, err := g.liveWindow(ctx, c, st, from, to)
		if err != nil {
			return nil, err
		}
		for _, e := range evs {
			if e.ShowAs == "busy" && !e.AllDay {
				out = append(out, busySpan{Start: e.Start, End: e.End})
			}
		}
	}
	return out, nil
}
