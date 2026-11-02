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

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/httpx"
)

// Google Calendar API v3, from the connection's endpoints ("calendar", else
// "api" + /calendar/v3, else www.googleapis.com). Sync: singles, series
// masters and exceptions since the horizon with events.list
// (singleEvents=false) and its nextSyncToken, continued incrementally (a 410
// starts over); series are expanded with events.instances per window.

type googleCalendar struct {
	p    platform
	base string
	conn *calConn
}

func newGoogleCalendar(c *calConn, client *http.Client) *googleCalendar {
	base := c.Endpoints["calendar"]
	if api := strings.TrimRight(c.Endpoints["api"], "/"); base == "" && api != "" {
		base = api + "/calendar/v3"
	}
	if base == "" {
		base = "https://www.googleapis.com/calendar/v3"
	}
	return &googleCalendar{p: platform{client: client, name: "Google Calendar"}, base: strings.TrimRight(base, "/"), conn: c}
}

type gTime struct {
	Date     string `json:"date,omitempty"`
	DateTime string `json:"dateTime,omitempty"`
	TimeZone string `json:"timeZone,omitempty"`
}

type gPerson struct {
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
	Self        bool   `json:"self"`
}

type gAttendee struct {
	Email          string `json:"email"`
	DisplayName    string `json:"displayName,omitempty"`
	ResponseStatus string `json:"responseStatus,omitempty"`
	Self           bool   `json:"self,omitempty"`
	Organizer      bool   `json:"organizer,omitempty"`
	Optional       bool   `json:"optional,omitempty"`
	Resource       bool   `json:"resource,omitempty"`
}

type gConference struct {
	ConferenceSolution struct {
		Key struct {
			Type string `json:"type"`
		} `json:"key"`
		Name string `json:"name"`
	} `json:"conferenceSolution"`
	EntryPoints []struct {
		EntryPointType string `json:"entryPointType"`
		URI            string `json:"uri"`
	} `json:"entryPoints"`
	CreateRequest *struct {
		Status struct {
			StatusCode string `json:"statusCode"`
		} `json:"status"`
	} `json:"createRequest"`
}

type gEvent struct {
	ID                string       `json:"id"`
	ETag              string       `json:"etag"`
	Status            string       `json:"status"`
	HTMLLink          string       `json:"htmlLink"`
	Summary           string       `json:"summary"`
	Description       string       `json:"description"`
	Location          string       `json:"location"`
	Transparency      string       `json:"transparency"`
	Visibility        string       `json:"visibility"`
	Updated           string       `json:"updated"`
	Start             gTime        `json:"start"`
	End               gTime        `json:"end"`
	OriginalStartTime *gTime       `json:"originalStartTime"`
	RecurringEventID  string       `json:"recurringEventId"`
	Recurrence        []string     `json:"recurrence"`
	Organizer         gPerson      `json:"organizer"`
	Attendees         []gAttendee  `json:"attendees"`
	HangoutLink       string       `json:"hangoutLink"`
	ConferenceData    *gConference `json:"conferenceData"`
	GuestsCanModify   bool         `json:"guestsCanModify"`
	Locked            bool         `json:"locked"`
	PrivateCopy       bool         `json:"privateCopy"`
	EventType         string       `json:"eventType"`
}

type gEventList struct {
	Items         []gEvent `json:"items"`
	NextPageToken string   `json:"nextPageToken"`
	NextSyncToken string   `json:"nextSyncToken"`
	TimeZone      string   `json:"timeZone"`
}

func (g *googleCalendar) calURL(calID string, parts ...string) string {
	u := g.base + "/calendars/" + url.PathEscape(calID)
	for _, p := range parts {
		u += "/" + url.PathEscape(p)
	}
	return u
}

func (g *googleCalendar) calendars(ctx context.Context) ([]extCalendar, error) {
	var out []extCalendar
	page := ""
	for range 10 {
		q := url.Values{"maxResults": {"250"}}
		if page != "" {
			q.Set("pageToken", page)
		}
		var res struct {
			Items []struct {
				ID                    string `json:"id"`
				Summary               string `json:"summary"`
				SummaryOverride       string `json:"summaryOverride"`
				Primary               bool   `json:"primary"`
				AccessRole            string `json:"accessRole"`
				TimeZone              string `json:"timeZone"`
				Selected              bool   `json:"selected"`
				Hidden                bool   `json:"hidden"`
				Deleted               bool   `json:"deleted"`
				ConferenceProperties  *struct {
					AllowedConferenceSolutionTypes []string `json:"allowedConferenceSolutionTypes"`
				} `json:"conferenceProperties"`
			} `json:"items"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := g.p.call(ctx, http.MethodGet, g.base+"/users/me/calendarList?"+q.Encode(), nil, &res); err != nil {
			return nil, err
		}
		for _, c := range res.Items {
			// Free/busy-only calendars can't list events.
			if c.Deleted || c.AccessRole == "freeBusyReader" {
				continue
			}
			name := c.SummaryOverride
			if name == "" {
				name = c.Summary
			}
			cal := extCalendar{ID: c.ID, Name: name, AccessRole: c.AccessRole, Timezone: c.TimeZone, Primary: c.Primary,
				CanWrite: (c.AccessRole == "owner" || c.AccessRole == "writer") && g.conn.writeCap != "",
				Hidden:   c.Hidden || !c.Selected && !c.Primary, platform: platformGoogle, connID: g.conn.ID, account: g.conn.Account}
			if c.ConferenceProperties == nil || slices.Contains(c.ConferenceProperties.AllowedConferenceSolutionTypes, "hangoutsMeet") {
				cal.Meeting = ProviderGoogleMeet
			}
			out = append(out, cal)
		}
		if page = res.NextPageToken; page == "" {
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

// events returns cal's events and instances overlapping [from, to).
func (g *googleCalendar) events(ctx context.Context, cal extCalendar, st *calState, from, to time.Time) ([]extEvent, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	lo, hi := connectedHorizon(time.Now())
	if from.Before(lo) || to.After(hi) {
		return g.liveWindow(ctx, cal, st, from, to)
	}
	if err := g.sync(ctx, cal, st, lo); err != nil {
		return nil, err
	}
	var out []extEvent
	for _, e := range st.items {
		switch {
		case e.Status == "cancelled":
		case len(e.Recurrence) > 0:
			inst, err := g.instances(ctx, cal, st, e, from, to)
			if err != nil {
				return nil, err
			}
			out = append(out, inst...)
		case e.SeriesID != "":
			// An exception: its series' instances include it, unless the
			// series itself isn't in the synced set.
			if m, ok := st.items[e.SeriesID]; (!ok || len(m.Recurrence) == 0) && e.overlaps(from, to) {
				out = append(out, e)
			}
		case e.overlaps(from, to):
			out = append(out, e)
		}
	}
	return out, nil
}

// sync brings st up to date: a full list since lo the first time, then only
// changes (syncToken). A stale token (410) starts over once.
func (g *googleCalendar) sync(ctx context.Context, cal extCalendar, st *calState, lo time.Time) error {
	if st.items != nil && time.Since(st.synced) < syncEvery {
		return nil
	}
	for attempt := 0; attempt < 2; attempt++ {
		full := st.token == "" || st.items == nil
		items := st.items
		if full {
			items = map[string]extEvent{}
		}
		touched := map[string]bool{}
		page, next := "", ""
		var err error
		for range 40 {
			q := url.Values{"maxResults": {"250"}, "singleEvents": {"false"}}
			if full {
				q.Set("timeMin", lo.UTC().Format(time.RFC3339))
				q.Set("showDeleted", "false")
			} else {
				q.Set("syncToken", st.token)
			}
			if page != "" {
				q.Set("pageToken", page)
			}
			var res gEventList
			if err = g.p.call(ctx, http.MethodGet, g.calURL(cal.ID, "events")+"?"+q.Encode(), nil, &res); err != nil {
				break
			}
			zone := res.TimeZone
			if zone == "" {
				zone = cal.Timezone
			}
			for _, ge := range res.Items {
				e := g.toExt(ge, cal, zone)
				touched[e.ID] = true
				if e.SeriesID != "" {
					touched[e.SeriesID] = true
				}
				if e.Status == "cancelled" && e.SeriesID == "" {
					delete(items, e.ID)
					continue
				}
				items[e.ID] = e
			}
			page, next = res.NextPageToken, res.NextSyncToken
			if page == "" {
				break
			}
		}
		if errors.Is(err, errSyncGone) && !full {
			st.reset()
			continue
		}
		if err != nil {
			return err
		}
		st.items, st.token, st.synced = items, next, time.Now()
		if full {
			st.instances = nil
		} else {
			for id := range touched {
				for k := range st.instances {
					if strings.HasPrefix(k, id+"|") {
						delete(st.instances, k)
					}
				}
			}
		}
		return nil
	}
	return httpx.NewError(http.StatusBadGateway, "connection_unavailable", "Google Calendar couldn't start a fresh sync. Try again.")
}

// instances expands a series over [from, to) with events.instances (the
// series' own exceptions included, cancelled ones left out), cached per
// window until the series or one of its exceptions changes.
func (g *googleCalendar) instances(ctx context.Context, cal extCalendar, st *calState, master extEvent, from, to time.Time) ([]extEvent, error) {
	key := master.ID + "|" + strconv.FormatInt(from.Unix(), 10) + "|" + strconv.FormatInt(to.Unix(), 10)
	if e, ok := st.instances[key]; ok && e.stamp == master.Stamp {
		return e.events, nil
	}
	var out []extEvent
	page := ""
	for range 8 {
		q := url.Values{"timeMin": {from.Format(time.RFC3339)}, "timeMax": {to.Format(time.RFC3339)}, "maxResults": {"250"}}
		if page != "" {
			q.Set("pageToken", page)
		}
		var res gEventList
		err := g.p.call(ctx, http.MethodGet, g.calURL(cal.ID, "events", master.ID, "instances")+"?"+q.Encode(), nil, &res)
		if errors.Is(err, errPlatformGone) {
			return nil, nil // deleted meanwhile; the next sync drops it
		}
		if err != nil {
			return nil, err
		}
		zone := res.TimeZone
		if zone == "" {
			zone = master.Timezone
		}
		for _, ge := range res.Items {
			e := g.toExt(ge, cal, zone)
			if e.Status == "cancelled" {
				continue
			}
			e.Recurring = true
			if e.SeriesID == "" {
				e.SeriesID = master.ID
			}
			out = append(out, e)
		}
		if page = res.NextPageToken; page == "" {
			break
		}
	}
	if st.instances == nil {
		st.instances = map[string]instEntry{}
	}
	if len(st.instances) > 400 {
		st.instances = map[string]instEntry{}
	}
	st.instances[key] = instEntry{at: time.Now(), stamp: master.Stamp, events: out}
	return out, nil
}

// liveWindow reads a window outside the synced horizon, expanded
// (singleEvents=true), cached briefly.
func (g *googleCalendar) liveWindow(ctx context.Context, cal extCalendar, st *calState, from, to time.Time) ([]extEvent, error) {
	key := strconv.FormatInt(from.Unix(), 10) + "|" + strconv.FormatInt(to.Unix(), 10)
	if v, ok := st.view(key); ok {
		return v, nil
	}
	var out []extEvent
	page := ""
	for range 10 {
		q := url.Values{"timeMin": {from.Format(time.RFC3339)}, "timeMax": {to.Format(time.RFC3339)}, "singleEvents": {"true"},
			"orderBy": {"startTime"}, "maxResults": {"250"}}
		if page != "" {
			q.Set("pageToken", page)
		}
		var res gEventList
		if err := g.p.call(ctx, http.MethodGet, g.calURL(cal.ID, "events")+"?"+q.Encode(), nil, &res); err != nil {
			return nil, err
		}
		zone := res.TimeZone
		if zone == "" {
			zone = cal.Timezone
		}
		for _, ge := range res.Items {
			if e := g.toExt(ge, cal, zone); e.Status != "cancelled" {
				out = append(out, e)
			}
		}
		if page = res.NextPageToken; page == "" {
			break
		}
	}
	st.putView(key, out)
	return out, nil
}

var googleResponses = map[string]string{"needsAction": ResponseNeedsAction, "accepted": ResponseAccepted, "tentative": ResponseTentative, "declined": ResponseDeclined}
var googleResponseOf = map[string]string{ResponseAccepted: "accepted", ResponseTentative: "tentative", ResponseDeclined: "declined", ResponseNeedsAction: "needsAction"}

// toExt maps a Google event; zone is the calendar's (all-day dates read in it).
func (g *googleCalendar) toExt(ge gEvent, cal extCalendar, zone string) extEvent {
	e := extEvent{ID: ge.ID, CalendarID: cal.ID, SeriesID: ge.RecurringEventID, Title: ge.Summary, Location: ge.Location,
		Description: ge.Description, Status: ge.Status, ShowAs: "busy", WebURL: ge.HTMLLink, Recurrence: ge.Recurrence,
		Stamp: ge.ETag, Private: ge.Visibility == "private" || ge.Visibility == "confidential"}
	if e.Status == "" {
		e.Status = "confirmed"
	}
	if e.Title == "" && e.Status != "cancelled" {
		e.Title = "(No title)"
	}
	if ge.Transparency == "transparent" {
		e.ShowAs = "free"
	}
	e.Recurring = ge.RecurringEventID != "" || len(ge.Recurrence) > 0
	if t, err := time.Parse(time.RFC3339, ge.Updated); err == nil {
		e.Updated = t
	}
	tz := ge.Start.TimeZone
	if !validZone(tz) {
		tz = zone
	}
	if !validZone(tz) {
		tz = "UTC"
	}
	e.Timezone = tz
	if ge.Start.Date != "" {
		e.AllDay = true
		a, err1 := time.ParseInLocation(time.DateOnly, ge.Start.Date, zoneOr(zone, time.UTC))
		b, err2 := time.ParseInLocation(time.DateOnly, ge.End.Date, zoneOr(zone, time.UTC))
		if err1 == nil {
			if err2 != nil || !b.After(a) {
				b = a.AddDate(0, 0, 1)
			}
			e.Start, e.End = a.UTC(), b.UTC()
			e.StartDate, e.EndDate = ge.Start.Date, b.AddDate(0, 0, -1).Format(time.DateOnly)
		}
	} else {
		if t, err := time.Parse(time.RFC3339, ge.Start.DateTime); err == nil {
			e.Start = t.UTC()
		}
		if t, err := time.Parse(time.RFC3339, ge.End.DateTime); err == nil {
			e.End = t.UTC()
		}
		if !e.End.After(e.Start) {
			e.End = e.Start.Add(time.Minute)
		}
	}
	e.Organizer = extAttendee{Email: ge.Organizer.Email, Name: ge.Organizer.DisplayName, Organizer: true, Self: ge.Organizer.Self}
	e.OrganizerSelf = ge.Organizer.Self
	for _, a := range ge.Attendees {
		x := extAttendee{Email: a.Email, Name: a.DisplayName, Response: googleResponses[a.ResponseStatus], Organizer: a.Organizer,
			Self: a.Self, Optional: a.Optional, Resource: a.Resource}
		if x.Response == "" {
			x.Response = ResponseNeedsAction
		}
		if a.Self && !a.Organizer {
			e.MyResponse = x.Response
		}
		e.Attendees = append(e.Attendees, x)
	}
	if e.OrganizerSelf {
		e.MyResponse = ""
	}
	if ge.HangoutLink != "" {
		e.ConferenceURL, e.ConferenceProvider = ge.HangoutLink, ProviderGoogleMeet
	} else if ge.ConferenceData != nil {
		for _, ep := range ge.ConferenceData.EntryPoints {
			if ep.EntryPointType == "video" && ep.URI != "" {
				e.ConferenceURL = ep.URI
				break
			}
		}
		switch t, n := ge.ConferenceData.ConferenceSolution.Key.Type, strings.ToLower(ge.ConferenceData.ConferenceSolution.Name); {
		case t == "hangoutsMeet":
			e.ConferenceProvider = ProviderGoogleMeet
		case strings.Contains(n, "zoom"):
			e.ConferenceProvider = ProviderZoom
		case strings.Contains(n, "teams"):
			e.ConferenceProvider = ProviderTeams
		}
	}
	if e.ConferenceURL == "" {
		e.ConferenceURL, e.ConferenceProvider = meetingIn(ge.Location, ge.Description)
	}
	editable := ge.EventType == "" || ge.EventType == "default" || ge.EventType == "outOfOffice" || ge.EventType == "focusTime"
	e.CanEdit = cal.CanWrite && editable && !ge.Locked && !ge.PrivateCopy && (ge.Organizer.Self || ge.GuestsCanModify)
	return e
}

func (g *googleCalendar) event(ctx context.Context, calID, eventID string) (extEvent, error) {
	var ge gEvent
	if err := g.p.call(ctx, http.MethodGet, g.calURL(calID, "events", eventID), nil, &ge); err != nil {
		return extEvent{}, err
	}
	cal := g.calendarStub(calID)
	return g.toExt(ge, cal, ""), nil
}

// calendarStub is enough of a calendar to map one event read by id.
func (g *googleCalendar) calendarStub(calID string) extCalendar {
	return extCalendar{ID: calID, CanWrite: g.conn.writeCap != "", platform: platformGoogle, connID: g.conn.ID, account: g.conn.Account}
}

// body is the Google event a write sends (only the fields set).
func (g *googleCalendar) body(w extWrite, create bool) map[string]any {
	b := map[string]any{}
	if w.Title != nil {
		b["summary"] = *w.Title
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
		b["description"] = *desc
	}
	if w.Location != nil {
		b["location"] = *w.Location
	}
	if w.TimeSet {
		if w.AllDay {
			end, _ := time.Parse(time.DateOnly, w.EndDate)
			b["start"] = gTime{Date: w.StartDate}
			b["end"] = gTime{Date: end.AddDate(0, 0, 1).Format(time.DateOnly)}
		} else {
			loc := zoneOr(w.Timezone, time.UTC)
			b["start"] = gTime{DateTime: w.Start.In(loc).Format(time.RFC3339), TimeZone: w.Timezone}
			b["end"] = gTime{DateTime: w.End.In(loc).Format(time.RFC3339), TimeZone: w.Timezone}
		}
	}
	if w.ShowAs != nil {
		if *w.ShowAs == "free" {
			b["transparency"] = "transparent"
		} else {
			b["transparency"] = "opaque"
		}
	}
	if w.Attendees != nil {
		list := make([]gAttendee, 0, len(*w.Attendees))
		for _, a := range *w.Attendees {
			list = append(list, gAttendee{Email: a.Email, DisplayName: a.Name, ResponseStatus: googleResponseOf[a.Response], Optional: a.Optional})
		}
		b["attendees"] = list
	}
	if w.RuleSet {
		if w.Rule == nil {
			b["recurrence"] = []string{}
		} else {
			b["recurrence"] = []string{"RRULE:" + w.Rule.String(w.AllDay, zoneOr(w.Timezone, time.UTC))}
		}
	}
	switch w.Meeting {
	case ProviderGoogleMeet:
		b["conferenceData"] = map[string]any{"createRequest": map[string]any{
			"requestId":             uuid.NewString(),
			"conferenceSolutionKey": map[string]string{"type": "hangoutsMeet"},
		}}
	case "remove":
		if !create {
			b["conferenceData"] = nil
		}
	}
	return b
}

func sendUpdatesParam(send bool) string {
	if send {
		return "all"
	}
	return "none"
}

func (g *googleCalendar) create(ctx context.Context, calID string, w extWrite) (extEvent, error) {
	q := url.Values{"conferenceDataVersion": {"1"}, "sendUpdates": {sendUpdatesParam(w.SendUpdates)}}
	var ge gEvent
	if err := g.p.call(ctx, http.MethodPost, g.calURL(calID, "events")+"?"+q.Encode(), g.body(w, true), &ge); err != nil {
		return extEvent{}, err
	}
	ge = g.awaitMeet(ctx, calID, ge)
	return g.toExt(ge, g.calendarStub(calID), w.Timezone), nil
}

// awaitMeet re-reads an event whose Meet link is still being made (Google
// answers "pending" now and then), briefly.
func (g *googleCalendar) awaitMeet(ctx context.Context, calID string, ge gEvent) gEvent {
	for i := 0; i < 3 && ge.HangoutLink == "" && ge.ConferenceData != nil && ge.ConferenceData.CreateRequest != nil &&
		ge.ConferenceData.CreateRequest.Status.StatusCode == "pending"; i++ {
		select {
		case <-ctx.Done():
			return ge
		case <-time.After(time.Duration(400*(i+1)) * time.Millisecond):
		}
		var again gEvent
		if err := g.p.call(ctx, http.MethodGet, g.calURL(calID, "events", ge.ID), nil, &again); err != nil {
			return ge
		}
		ge = again
	}
	return ge
}

func (g *googleCalendar) update(ctx context.Context, calID, eventID string, w extWrite) (extEvent, error) {
	q := url.Values{"conferenceDataVersion": {"1"}, "sendUpdates": {sendUpdatesParam(w.SendUpdates)}}
	var ge gEvent
	if err := g.p.call(ctx, http.MethodPatch, g.calURL(calID, "events", eventID)+"?"+q.Encode(), g.body(w, false), &ge); err != nil {
		return extEvent{}, err
	}
	ge = g.awaitMeet(ctx, calID, ge)
	return g.toExt(ge, g.calendarStub(calID), w.Timezone), nil
}

func (g *googleCalendar) remove(ctx context.Context, calID, eventID string, sendUpdates bool) error {
	q := url.Values{"sendUpdates": {sendUpdatesParam(sendUpdates)}}
	err := g.p.call(ctx, http.MethodDelete, g.calURL(calID, "events", eventID)+"?"+q.Encode(), nil, nil)
	if errors.Is(err, errPlatformGone) || errors.Is(err, errSyncGone) {
		return nil // already gone
	}
	return err
}

// respond sets the account's own reply: Google takes the whole guest list
// with the account's entry changed, and tells the organizer.
func (g *googleCalendar) respond(ctx context.Context, calID, eventID, response string) (extEvent, error) {
	var ge gEvent
	if err := g.p.call(ctx, http.MethodGet, g.calURL(calID, "events", eventID), nil, &ge); err != nil {
		return extEvent{}, err
	}
	found := false
	for i := range ge.Attendees {
		if ge.Attendees[i].Self {
			ge.Attendees[i].ResponseStatus = googleResponseOf[response]
			found = true
		}
	}
	if !found {
		return extEvent{}, httpx.Conflict("This account isn't a guest of that event.")
	}
	q := url.Values{"sendUpdates": {"all"}}
	var out gEvent
	if err := g.p.call(ctx, http.MethodPatch, g.calURL(calID, "events", eventID)+"?"+q.Encode(), map[string]any{"attendees": ge.Attendees}, &out); err != nil {
		return extEvent{}, err
	}
	return g.toExt(out, g.calendarStub(calID), ""), nil
}

func (g *googleCalendar) freeBusy(ctx context.Context, cals []extCalendar, from, to time.Time) ([]busySpan, error) {
	if len(cals) == 0 {
		return nil, nil
	}
	items := make([]map[string]string, 0, len(cals))
	for _, c := range cals {
		items = append(items, map[string]string{"id": c.ID})
	}
	var res struct {
		Calendars map[string]struct {
			Busy []struct {
				Start string `json:"start"`
				End   string `json:"end"`
			} `json:"busy"`
		} `json:"calendars"`
	}
	body := map[string]any{"timeMin": from.UTC().Format(time.RFC3339), "timeMax": to.UTC().Format(time.RFC3339), "items": items}
	if err := g.p.call(ctx, http.MethodPost, g.base+"/freeBusy", body, &res); err != nil {
		return nil, err
	}
	var out []busySpan
	for _, c := range res.Calendars {
		for _, b := range c.Busy {
			a, err1 := time.Parse(time.RFC3339, b.Start)
			z, err2 := time.Parse(time.RFC3339, b.End)
			if err1 == nil && err2 == nil && z.After(a) {
				out = append(out, busySpan{Start: a.UTC(), End: z.UTC()})
			}
		}
	}
	return out, nil
}
