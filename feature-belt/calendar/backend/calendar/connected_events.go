package calendar

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

func chiParam(r *http.Request, name string) string { return chi.URLParam(r, name) }

// ConnectedEvent is one event (or instance) from a connected calendar in the
// shape of an Occurrence, so the same views draw it, plus where it lives.
// EventID is its reference "<connection id>:<calendar id>:<event id>".
type ConnectedEvent struct {
	EventID            string         `json:"event_id"`
	OccurrenceStart    *time.Time     `json:"occurrence_start"`
	Recurring          bool           `json:"recurring"`
	Modified           bool           `json:"modified"`
	Title              string         `json:"title"`
	Location           string         `json:"location"`
	ConferenceURL      *string        `json:"conference_url"`
	ConferenceProvider string         `json:"conference_provider"`
	StartsAt           time.Time      `json:"starts_at"`
	EndsAt             time.Time      `json:"ends_at"`
	AllDay             bool           `json:"all_day"`
	StartDate          *string        `json:"start_date"`
	EndDate            *string        `json:"end_date"`
	Timezone           string         `json:"timezone"`
	Color              string         `json:"color"`
	ShowAs             string         `json:"show_as"`
	Visibility         string         `json:"visibility"`
	Role               string         `json:"role"` // owner (the account organizes it and changes are allowed) | attendee
	MyResponse         *string        `json:"my_response"`
	Owner              Person         `json:"owner"`
	AttendeeCount      int            `json:"attendee_count"`
	BookingID          *uuid.UUID     `json:"booking_id"`
	Source             *app.SourceRef `json:"source"`
	ConnectionID       string         `json:"connection_id"`
	CalendarID         string         `json:"calendar_id"`
	CalendarName       string         `json:"calendar_name"`
	ExternalID         string         `json:"external_id"`
	SeriesID           string         `json:"series_id"`
	WebURL             string         `json:"web_url"`
	CanEdit            bool           `json:"can_edit"`
	CanRespond         bool           `json:"can_respond"`
}

// ConnectedAttendee is a guest of a connected event.
type ConnectedAttendee struct {
	Email     string `json:"email"`
	Name      string `json:"name"`
	Response  string `json:"response"`
	Organizer bool   `json:"organizer"`
	Self      bool   `json:"self"`
	Optional  bool   `json:"optional"`
}

// ConnectedEventDetail is a connected event with its notes and guests.
type ConnectedEventDetail struct {
	ConnectedEvent
	Description string              `json:"description"`
	Attendees   []ConnectedAttendee `json:"attendees"`
	Organizer   ConnectedAttendee   `json:"organizer"`
}

// ConnectedRange is GET /v1/calendar/connected/events: events of the
// caller's visible connected calendars, and what couldn't be read.
type ConnectedRange struct {
	Items  []ConnectedEvent `json:"items"`
	Errors []SourceError    `json:"errors"`
}

func eventRef(connID, calID, eventID string) string { return connID + ":" + calID + ":" + eventID }

// toConnected maps a platform event for the views.
func toConnected(c *calConn, cal ConnectedCalendar, e extEvent) ConnectedEvent {
	out := ConnectedEvent{
		EventID: eventRef(c.ID, cal.ID, e.ID), Recurring: e.Recurring, Title: e.Title, Location: e.Location,
		ConferenceProvider: e.ConferenceProvider, StartsAt: e.Start, EndsAt: e.End, AllDay: e.AllDay, Timezone: e.Timezone,
		Color: cal.Color, ShowAs: e.ShowAs, Visibility: "busy", Role: "attendee", Source: c.source(e.WebURL),
		ConnectionID: c.ID, CalendarID: cal.ID, CalendarName: cal.Name, ExternalID: e.ID, SeriesID: e.SeriesID, WebURL: e.WebURL,
		CanEdit: e.CanEdit && c.writeCap != "",
	}
	if out.Color == "" {
		out.Color = "info"
	}
	if e.ConferenceURL != "" {
		u := e.ConferenceURL
		out.ConferenceURL = &u
	}
	if e.AllDay {
		a, b := e.StartDate, e.EndDate
		out.StartDate, out.EndDate = &a, &b
	}
	if out.CanEdit {
		out.Role = "owner"
	}
	if e.MyResponse != "" {
		r := e.MyResponse
		out.MyResponse = &r
		out.CanRespond = c.writeCap != ""
	}
	name := e.Organizer.Name
	if name == "" {
		name = e.Organizer.Email
	}
	if e.OrganizerSelf {
		name = c.Account
	}
	out.Owner = Person{DisplayName: name}
	for _, a := range e.Attendees {
		if !a.Resource {
			out.AttendeeCount++
		}
	}
	return out
}

func toConnectedDetail(c *calConn, cal ConnectedCalendar, e extEvent) ConnectedEventDetail {
	d := ConnectedEventDetail{ConnectedEvent: toConnected(c, cal, e), Description: e.Description, Attendees: []ConnectedAttendee{},
		Organizer: ConnectedAttendee{Email: e.Organizer.Email, Name: e.Organizer.Name, Organizer: true, Self: e.OrganizerSelf, Response: ResponseAccepted}}
	for _, a := range e.Attendees {
		if a.Resource {
			continue
		}
		d.Attendees = append(d.Attendees, ConnectedAttendee{Email: a.Email, Name: a.Name, Response: a.Response, Organizer: a.Organizer, Self: a.Self, Optional: a.Optional})
	}
	return d
}

// ---- sessions with calendars ------------------------------------------------------

// calendarsOf lists a connection's calendars (cached briefly).
func (s *Service) calendarsOf(ctx context.Context, userID, org uuid.UUID, c *calConn) ([]extCalendar, error) {
	if list, ok := cache.calendars(userID, c.ID); ok {
		return list, nil
	}
	sess, err := s.openConn(ctx, userID, org, c, false)
	if err != nil {
		return nil, err
	}
	list, err := sess.ad.calendars(ctx)
	if err != nil {
		return nil, s.connError(c, c.readCap, err)
	}
	cache.putCalendars(userID, c.ID, list)
	return list, nil
}

// viewCalendars are a connection's calendars with the person's choices and
// colours, in the order (and colours) the sources list gives them.
func (s *Service) viewCalendars(ctx context.Context, userID, org uuid.UUID, conns []*calConn) (map[string][]ConnectedCalendar, map[string]error, error) {
	prefs, err := s.calendarPrefs(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	out := map[string][]ConnectedCalendar{}
	errs := map[string]error{}
	colorIdx := 0
	for _, c := range conns {
		if c.Status != "" && c.Status != "active" {
			continue
		}
		cals, err := s.calendarsOf(ctx, userID, org, c)
		if err != nil {
			errs[c.ID] = err
			continue
		}
		for _, cal := range cals {
			out[c.ID] = append(out[c.ID], applyPrefs(cal, prefs[prefKey{c.ID, cal.ID}], &colorIdx))
		}
	}
	return out, errs, nil
}

// ---- reading ---------------------------------------------------------------------

// connectedRange reads events overlapping [from, to) from the caller's visible
// connected calendars (only one connection when only is set), live through
// the per-person cache, as the caller (agents: filtered by each
// connection's scope). Booking copies the app already shows are left out.
func (s *Service) connectedRange(ctx context.Context, caller *auth.Identity, org uuid.UUID, from, to time.Time, only string) (ConnectedRange, error) {
	out := ConnectedRange{Items: []ConnectedEvent{}, Errors: []SourceError{}}
	if s.connections() == nil {
		return out, nil
	}
	conns, err := s.connectedFor(ctx, caller.UserID, org)
	if err != nil {
		return out, err
	}
	if conns, err = s.agentConnReadable(ctx, caller, conns); err != nil {
		return out, err
	}
	cals, calErrs, err := s.viewCalendars(ctx, caller.UserID, org, conns)
	if err != nil {
		return out, err
	}
	mirrors, err := s.mirrorKeys(ctx, caller.UserID)
	if err != nil {
		return out, err
	}
	for id, e := range calErrs {
		out.Errors = append(out.Errors, *sourceError(id, "", e))
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	type job struct {
		c   *calConn
		cal ConnectedCalendar
	}
	var jobs []job
	for _, c := range conns {
		if only != "" && c.ID != only {
			continue
		}
		for _, cal := range cals[c.ID] {
			if !cal.Hidden {
				jobs = append(jobs, job{c, cal})
			}
		}
	}
	sessions := map[string]*calSession{}
	for _, j := range jobs {
		if _, ok := sessions[j.c.ID]; ok {
			continue
		}
		sess, err := s.openConn(ctx, caller.UserID, org, j.c, false)
		if err != nil {
			out.Errors = append(out.Errors, *sourceError(j.c.ID, "", err))
			sessions[j.c.ID] = nil
			continue
		}
		sessions[j.c.ID] = sess
	}
	results := make([][]ConnectedEvent, len(jobs))
	errs := make([]error, len(jobs))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6)
	for i, j := range jobs {
		sess := sessions[j.c.ID]
		if sess == nil {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			st := cache.state(caller.UserID, j.c.ID, j.cal.ID)
			evs, err := sess.ad.events(ctx, extCalendar{ID: j.cal.ID, Name: j.cal.Name, Primary: j.cal.Primary, CanWrite: j.cal.CanWrite,
				Timezone: j.cal.Timezone, platform: j.c.Provider, connID: j.c.ID, account: j.c.Account}, st, from, to)
			if err != nil {
				errs[i] = s.connError(j.c, j.c.readCap, err)
				return
			}
			list := make([]ConnectedEvent, 0, len(evs))
			for _, e := range evs {
				if mirrors[mirrorKey{j.c.ID, j.cal.ID, e.ID}] {
					continue
				}
				list = append(list, toConnected(j.c, j.cal, e))
			}
			results[i] = list
		}()
	}
	wg.Wait()
	byConn := map[string][]ConnectedEvent{}
	for i, j := range jobs {
		if errs[i] != nil {
			out.Errors = append(out.Errors, *sourceError(j.c.ID, j.cal.ID, errs[i]))
			continue
		}
		byConn[j.c.ID] = append(byConn[j.c.ID], results[i]...)
	}
	for _, c := range conns {
		items, err := s.agentEventsReadable(ctx, caller, c.ID, byConn[c.ID])
		if err != nil {
			return out, err
		}
		out.Items = append(out.Items, items...)
	}
	sort.SliceStable(out.Items, func(a, b int) bool { return out.Items[a].StartsAt.Before(out.Items[b].StartsAt) })
	if len(out.Items) > maxOccurrences {
		out.Items = out.Items[:maxOccurrences]
	}
	return out, nil
}

func (s *Service) connectedListHandler(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	org, err := s.orgParam(r.Context(), r, id.UserID)
	if err != nil {
		return err
	}
	from, to, err := parseWindow(r.URL.Query().Get("from"), r.URL.Query().Get("to"), maxWindow)
	if err != nil {
		return err
	}
	out, err := s.connectedRange(r.Context(), id, org, from, to, r.URL.Query().Get("source"))
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

// connectedCalendar finds one of a session's calendars with the caller's
// choices (colour, name), or a stub when it isn't listed.
func (s *Service) connectedCalendar(ctx context.Context, userID, org uuid.UUID, c *calConn, calID string) (ConnectedCalendar, bool) {
	cals, _, err := s.viewCalendars(ctx, userID, org, []*calConn{c})
	if err == nil {
		for _, cal := range cals[c.ID] {
			if cal.ID == calID {
				return cal, true
			}
		}
	}
	return ConnectedCalendar{ID: calID, Color: "info"}, false
}

// connectedEvent reads one event live (detail sheet, tools).
func (s *Service) connectedEvent(ctx context.Context, caller *auth.Identity, org uuid.UUID, connID, calID, eventID string) (ConnectedEventDetail, error) {
	if err := s.agentAllows(ctx, caller, connID, calID, eventID, app.AgentRead); err != nil {
		return ConnectedEventDetail{}, err
	}
	sess, err := s.session(ctx, caller.UserID, org, connID, false)
	if err != nil {
		return ConnectedEventDetail{}, err
	}
	e, err := sess.ad.event(ctx, calID, eventID)
	if err != nil {
		return ConnectedEventDetail{}, s.connError(sess.conn, sess.conn.readCap, err)
	}
	cal, _ := s.connectedCalendar(ctx, caller.UserID, org, sess.conn, calID)
	e.CanEdit = e.CanEdit && cal.CanWrite
	return toConnectedDetail(sess.conn, cal, e), nil
}

func (s *Service) connectedGetHandler(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	org, err := s.orgParam(r.Context(), r, id.UserID)
	if err != nil {
		return err
	}
	connID, calID, eventID, err := connPath(r)
	if err != nil {
		return err
	}
	d, err := s.connectedEvent(r.Context(), id, org, connID, calID, eventID)
	if err != nil {
		return err
	}
	httpx.OK(w, d)
	return nil
}

// ---- writing -----------------------------------------------------------------------

// connectedInput is the create/update body for a connected event: the event
// dialog's fields, guests by colleague or email (the platform invites them).
type connectedInput struct {
	Title                  *string          `json:"title"`
	Description            *string          `json:"description"`
	Location               *string          `json:"location"`
	StartsAt               *time.Time       `json:"starts_at"`
	EndsAt                 *time.Time       `json:"ends_at"`
	StartDate              *string          `json:"start_date"`
	EndDate                *string          `json:"end_date"`
	Timezone               *string          `json:"timezone"`
	AllDay                 *bool            `json:"all_day"`
	Recurrence             json.RawMessage  `json:"recurrence"`
	ShowAs                 *string          `json:"show_as"`
	Attendees              *[]attendeeInput `json:"attendees"`
	Conference             *string          `json:"conference"` // "add" | "remove"
	ConferenceProvider     string           `json:"conference_provider"`
	ConferenceConnectionID string           `json:"conference_connection_id"`
	SendUpdates            *bool            `json:"send_updates"`
	Scope                  string           `json:"scope"` // this | all (a series)
}

// connectedWrite validates in into a platform write. allowExternal=false
// (agents) refuses guests who aren't colleagues; colleagues are invited by
// their account address.
func (s *Service) connectedWrite(ctx context.Context, userID uuid.UUID, in connectedInput, create bool, defaultTZ string, allowExternal bool) (extWrite, patch, error) {
	ev := eventInput{Title: in.Title, Description: in.Description, Location: in.Location, StartsAt: in.StartsAt, EndsAt: in.EndsAt,
		StartDate: in.StartDate, EndDate: in.EndDate, Timezone: in.Timezone, AllDay: in.AllDay, Recurrence: in.Recurrence,
		ShowAs: in.ShowAs, Attendees: in.Attendees, SendUpdates: in.SendUpdates}
	p, err := s.parsePatch(ctx, userID, ev, create, defaultTZ, allowExternal)
	if err != nil {
		return extWrite{}, p, err
	}
	w := extWrite{Title: p.title, Description: p.description, Location: p.location, ShowAs: p.showAs, SendUpdates: p.sendUpdates}
	if p.timeSet {
		loc := zoneOr(p.tz, time.UTC)
		w.TimeSet, w.Start, w.End, w.AllDay, w.Timezone = true, p.start, p.end, p.allDay, p.tz
		if p.allDay {
			a, b := allDayDates(p.start, p.end, loc)
			w.StartDate, w.EndDate = *a, *b
		}
	}
	if p.recSet {
		w.RuleSet = true
		if p.rec != nil {
			if !p.timeSet {
				return w, p, httpx.BadRequest("Send the event's times with a repeat change.")
			}
			rule, err := p.rec.toRule(p.start, zoneOr(p.tz, time.UTC))
			if err != nil {
				return w, p, err
			}
			w.Rule = &rule
		}
	}
	if p.attendees != nil {
		list, err := s.attendeeEmails(ctx, *p.attendees)
		if err != nil {
			return w, p, err
		}
		w.Attendees = &list
	}
	return w, p, nil
}

// attendeeEmails turns resolved guests into platform guests: colleagues by
// their account address, others as typed.
func (s *Service) attendeeEmails(ctx context.Context, list []resolvedAttendee) ([]extAttendee, error) {
	var ids []uuid.UUID
	for _, a := range list {
		if a.userID != nil {
			ids = append(ids, *a.userID)
		}
	}
	emails := map[uuid.UUID][2]string{}
	if len(ids) > 0 {
		rows, err := s.d.Pool.Query(ctx, `SELECT id, email, display_name FROM users WHERE id = ANY($1)`, ids)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id uuid.UUID
			var email, name string
			if err := rows.Scan(&id, &email, &name); err != nil {
				rows.Close()
				return nil, err
			}
			emails[id] = [2]string{email, name}
		}
		rows.Close()
	}
	out := []extAttendee{}
	for _, a := range list {
		if a.userID != nil {
			if e, ok := emails[*a.userID]; ok && e[0] != "" {
				out = append(out, extAttendee{Email: e[0], Name: e[1]})
			}
			continue
		}
		out = append(out, extAttendee{Email: a.email, Name: a.name})
	}
	return out, nil
}

// connectedMeeting resolves "add a video call" on a connected calendar: the
// platform's own link when the option is native there, else a room from the
// wired Conferencing (written into the event). It returns the room to record
// (and cancel when the write fails).
func (s *Service) connectedMeeting(ctx context.Context, userID, org uuid.UUID, sess *calSession, calID string, w *extWrite, provider, connID, title string, guests []extAttendee) (*Conference, error) {
	cal, _ := s.connectedCalendar(ctx, userID, org, sess.conn, calID)
	for _, o := range cal.Conferencing {
		if o.ID == provider {
			w.Meeting = provider
			return nil, nil
		}
	}
	opt, ok := seamOption(ctx, userID, org, provider, connID)
	if !ok {
		return nil, httpx.NewError(http.StatusConflict, "video_unavailable", "That meeting provider isn't available for this calendar.")
	}
	c := conferencingProvider()
	if c == nil {
		return nil, httpx.NewError(http.StatusConflict, "video_unavailable", "Video calls aren't set up in this app.")
	}
	req := ConferenceRequest{HostID: userID, Title: title, StartsAt: w.Start, EndsAt: w.End, Audience: AudienceInternal,
		Provider: opt.ID, ConnectionID: opt.ConnectionID}
	if len(guests) > 0 {
		req.Audience = AudienceExternal
		for _, g := range guests {
			req.GuestEmails = append(req.GuestEmails, g.Email)
		}
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	conf, err := c.Create(cctx, req)
	if err != nil {
		var he *httpx.Error
		if errors.As(err, &he) {
			return nil, he
		}
		slog.WarnContext(ctx, "calendar: conference create failed", "err", err)
		return nil, httpx.NewError(http.StatusBadGateway, "video_failed", "Couldn't create the video call. Try again.")
	}
	if conf.Provider == "" {
		conf.Provider = opt.ID
	}
	w.JoinURL, w.JoinLabel = conf.JoinURL, opt.Label
	return &conf, nil
}

// createConnected adds an event to a connected calendar the caller can write.
func (s *Service) createConnected(ctx context.Context, caller *auth.Identity, org uuid.UUID, connID, calID string, in connectedInput, allowExternal bool) (ConnectedEventDetail, error) {
	if err := s.agentAllows(ctx, caller, connID, calID, "", app.AgentEdit); err != nil {
		return ConnectedEventDetail{}, err
	}
	sess, err := s.session(ctx, caller.UserID, org, connID, true)
	if err != nil {
		return ConnectedEventDetail{}, err
	}
	cal, listed := s.connectedCalendar(ctx, caller.UserID, org, sess.conn, calID)
	if listed && !cal.CanWrite {
		return ConnectedEventDetail{}, httpx.Forbidden("This account can only read that calendar.")
	}
	st, err := s.loadSettings(ctx, s.d.Pool, caller.UserID)
	if err != nil {
		return ConnectedEventDetail{}, err
	}
	w, _, err := s.connectedWrite(ctx, caller.UserID, in, true, st.Timezone, allowExternal)
	if err != nil {
		return ConnectedEventDetail{}, err
	}
	if !allowExternal {
		w.SendUpdates = false // agents never have a platform email anyone
	}
	var conf *Conference
	if in.Conference != nil && *in.Conference == "add" {
		var guests []extAttendee
		if w.Attendees != nil {
			guests = *w.Attendees
		}
		if conf, err = s.connectedMeeting(ctx, caller.UserID, org, sess, calID, &w, in.ConferenceProvider, in.ConferenceConnectionID, *w.Title, guests); err != nil {
			return ConnectedEventDetail{}, err
		}
	}
	e, err := sess.ad.create(ctx, calID, w)
	if err != nil {
		if conf != nil {
			s.cancelConferenceNow(ctx, conf.ID)
		}
		return ConnectedEventDetail{}, s.connError(sess.conn, sess.conn.writeCap, err)
	}
	if w.Meeting != "" && e.ConferenceURL == "" {
		slog.WarnContext(ctx, "calendar: platform added no meeting link", "provider", w.Meeting)
	}
	if conf != nil {
		if err := s.recordConference(ctx, caller.UserID, connID, calID, e.ID, *conf); err != nil {
			slog.WarnContext(ctx, "calendar: recording a connected room failed", "err", err)
		}
	}
	cache.stale(caller.UserID, connID, calID)
	s.changed(ctx, "connected", caller.UserID)
	return toConnectedDetail(sess.conn, cal, e), nil
}

func (s *Service) connectedCreateHandler(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	org, err := s.orgParam(r.Context(), r, id.UserID)
	if err != nil {
		return err
	}
	connID, calID, _, err := connPath(r)
	if err != nil {
		return err
	}
	var in connectedInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	d, err := s.createConnected(r.Context(), id, org, connID, calID, in, true)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, d)
	return nil
}

// updateConnected changes a connected event: one instance (scope this,
// default) or its whole series (scope all: time changes shift the series by
// the same amount).
func (s *Service) updateConnected(ctx context.Context, caller *auth.Identity, org uuid.UUID, connID, calID, eventID string, in connectedInput, allowExternal bool) (ConnectedEventDetail, error) {
	if err := s.agentAllows(ctx, caller, connID, calID, eventID, app.AgentEdit); err != nil {
		return ConnectedEventDetail{}, err
	}
	sess, err := s.session(ctx, caller.UserID, org, connID, true)
	if err != nil {
		return ConnectedEventDetail{}, err
	}
	cur, err := sess.ad.event(ctx, calID, eventID)
	if err != nil {
		return ConnectedEventDetail{}, s.connError(sess.conn, sess.conn.writeCap, err)
	}
	if !cur.CanEdit {
		return ConnectedEventDetail{}, httpx.Forbidden("Only the organizer can change this event on " + productLabel(sess.conn.Provider) + ".")
	}
	w, p, err := s.connectedWrite(ctx, caller.UserID, in, false, cur.Timezone, allowExternal)
	if err != nil {
		return ConnectedEventDetail{}, err
	}
	if !allowExternal {
		w.SendUpdates = false
	}
	target := eventID
	if in.Scope == ScopeAll && cur.SeriesID != "" {
		master, err := sess.ad.event(ctx, calID, cur.SeriesID)
		if err != nil {
			return ConnectedEventDetail{}, s.connError(sess.conn, sess.conn.writeCap, err)
		}
		if w.TimeSet {
			shift := w.Start.Sub(cur.Start)
			length := w.End.Sub(w.Start)
			w.Start = master.Start.Add(shift)
			w.End = w.Start.Add(length)
			if w.AllDay {
				loc := zoneOr(w.Timezone, time.UTC)
				a, b := allDayDates(w.Start, w.End, loc)
				w.StartDate, w.EndDate = *a, *b
			}
		}
		target = master.ID
	} else if w.RuleSet && cur.SeriesID != "" {
		return ConnectedEventDetail{}, httpx.BadRequest("Change the repeat on all events of the series.")
	}
	var conf *Conference
	if in.Conference != nil {
		switch *in.Conference {
		case "add":
			title := cur.Title
			if p.title != nil {
				title = *p.title
			}
			if !w.TimeSet {
				w.Start, w.End = cur.Start, cur.End
			}
			if conf, err = s.connectedMeeting(ctx, caller.UserID, org, sess, calID, &w, in.ConferenceProvider, in.ConferenceConnectionID, title, cur.Attendees); err != nil {
				return ConnectedEventDetail{}, err
			}
			if w.JoinURL != "" && w.Description == nil {
				d := cur.Description
				w.Description = &d
			}
		case "remove":
			w.Meeting = "remove"
		}
	}
	e, err := sess.ad.update(ctx, calID, target, w)
	if err != nil {
		if conf != nil {
			s.cancelConferenceNow(ctx, conf.ID)
		}
		return ConnectedEventDetail{}, s.connError(sess.conn, sess.conn.writeCap, err)
	}
	// A room made here follows the event's time; a replaced or removed one closes.
	if old, ok := s.recordedConference(ctx, caller.UserID, connID, calID, target); ok {
		switch {
		case conf != nil || (in.Conference != nil && *in.Conference == "remove"):
			s.forgetConference(ctx, caller.UserID, connID, calID, target)
			s.cancelConferenceNow(ctx, old)
		case w.TimeSet || p.title != nil:
			s.syncConnectedConference(ctx, caller.UserID, old, e)
		}
	}
	if conf != nil {
		if err := s.recordConference(ctx, caller.UserID, connID, calID, target, *conf); err != nil {
			slog.WarnContext(ctx, "calendar: recording a connected room failed", "err", err)
		}
	}
	cache.stale(caller.UserID, connID, calID)
	s.changed(ctx, "connected", caller.UserID)
	cal, _ := s.connectedCalendar(ctx, caller.UserID, org, sess.conn, calID)
	return toConnectedDetail(sess.conn, cal, e), nil
}

func (s *Service) connectedUpdateHandler(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	org, err := s.orgParam(r.Context(), r, id.UserID)
	if err != nil {
		return err
	}
	connID, calID, eventID, err := connPath(r)
	if err != nil {
		return err
	}
	var in connectedInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	d, err := s.updateConnected(r.Context(), id, org, connID, calID, eventID, in, true)
	if err != nil {
		return err
	}
	httpx.OK(w, d)
	return nil
}

// deleteConnected removes a connected event (scope all: its whole series);
// the platform tells guests as it normally does.
func (s *Service) connectedDeleteHandler(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	ctx := r.Context()
	org, err := s.orgParam(ctx, r, id.UserID)
	if err != nil {
		return err
	}
	connID, calID, eventID, err := connPath(r)
	if err != nil {
		return err
	}
	sess, err := s.session(ctx, id.UserID, org, connID, true)
	if err != nil {
		return err
	}
	cur, err := sess.ad.event(ctx, calID, eventID)
	if err != nil {
		if errors.Is(err, errPlatformGone) {
			httpx.NoContent(w)
			return nil
		}
		return s.connError(sess.conn, sess.conn.writeCap, err)
	}
	if !cur.CanEdit {
		return httpx.Forbidden("Only the organizer can delete this event on " + productLabel(sess.conn.Provider) + ". Decline it instead.")
	}
	target := eventID
	if r.URL.Query().Get("scope") == ScopeAll && cur.SeriesID != "" {
		target = cur.SeriesID
	}
	if err := sess.ad.remove(ctx, calID, target, r.URL.Query().Get("send_updates") != "false"); err != nil {
		return s.connError(sess.conn, sess.conn.writeCap, err)
	}
	if old, ok := s.recordedConference(ctx, id.UserID, connID, calID, target); ok {
		s.forgetConference(ctx, id.UserID, connID, calID, target)
		s.cancelConferenceNow(ctx, old)
	}
	cache.stale(id.UserID, connID, calID)
	s.changed(ctx, "connected", id.UserID)
	httpx.NoContent(w)
	return nil
}

// respondConnected records the account's reply on the platform.
func (s *Service) respondConnected(ctx context.Context, caller *auth.Identity, org uuid.UUID, connID, calID, eventID, response string) (ConnectedEventDetail, error) {
	switch response {
	case ResponseAccepted, ResponseTentative, ResponseDeclined:
	default:
		return ConnectedEventDetail{}, httpx.BadRequest("response must be accepted, tentative or declined.")
	}
	if err := s.agentAllows(ctx, caller, connID, calID, eventID, app.AgentEdit); err != nil {
		return ConnectedEventDetail{}, err
	}
	sess, err := s.session(ctx, caller.UserID, org, connID, true)
	if err != nil {
		return ConnectedEventDetail{}, err
	}
	e, err := sess.ad.respond(ctx, calID, eventID, response)
	if err != nil {
		return ConnectedEventDetail{}, s.connError(sess.conn, sess.conn.writeCap, err)
	}
	cache.stale(caller.UserID, connID, calID)
	s.changed(ctx, "connected", caller.UserID)
	cal, _ := s.connectedCalendar(ctx, caller.UserID, org, sess.conn, calID)
	return toConnectedDetail(sess.conn, cal, e), nil
}

func (s *Service) connectedRSVPHandler(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	org, err := s.orgParam(r.Context(), r, id.UserID)
	if err != nil {
		return err
	}
	connID, calID, eventID, err := connPath(r)
	if err != nil {
		return err
	}
	var in struct {
		Response string `json:"response"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	d, err := s.respondConnected(r.Context(), id, org, connID, calID, eventID, in.Response)
	if err != nil {
		return err
	}
	httpx.OK(w, d)
	return nil
}

// ---- rooms made for connected events ------------------------------------------------

func (s *Service) recordConference(ctx context.Context, userID uuid.UUID, connID, calID, eventID string, c Conference) error {
	provider := c.Provider
	if provider == "" {
		provider = ProviderApp
	}
	_, err := s.d.Pool.Exec(ctx, `
		INSERT INTO calendar_connected_conferences (user_id, connection_id, calendar_id, event_id, conference_id, provider)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (user_id, connection_id, calendar_id, event_id) DO UPDATE SET conference_id = $5, provider = $6`,
		userID, connID, calID, eventID, c.ID, provider)
	return err
}

func (s *Service) recordedConference(ctx context.Context, userID uuid.UUID, connID, calID, eventID string) (string, bool) {
	var id string
	err := s.d.Pool.QueryRow(ctx, `SELECT conference_id FROM calendar_connected_conferences
		WHERE user_id = $1 AND connection_id = $2 AND calendar_id = $3 AND event_id = $4`, userID, connID, calID, eventID).Scan(&id)
	return id, err == nil && id != ""
}

func (s *Service) forgetConference(ctx context.Context, userID uuid.UUID, connID, calID, eventID string) {
	if _, err := s.d.Pool.Exec(ctx, `DELETE FROM calendar_connected_conferences
		WHERE user_id = $1 AND connection_id = $2 AND calendar_id = $3 AND event_id = $4`, userID, connID, calID, eventID); err != nil {
		slog.WarnContext(ctx, "calendar: forgetting a connected room failed", "err", err)
	}
}

// syncConnectedConference moves a room made for a connected event to its
// new time or title (best-effort).
func (s *Service) syncConnectedConference(ctx context.Context, userID uuid.UUID, confID string, e extEvent) {
	c := conferencingProvider()
	if c == nil {
		return
	}
	req := ConferenceRequest{HostID: userID, Title: e.Title, StartsAt: e.Start, EndsAt: e.End, Audience: AudienceExternal}
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if _, err := c.Update(cctx, confID, req); err != nil {
		slog.WarnContext(ctx, "calendar: conference update failed", "err", err)
	}
}

// ---- busy time for availability ------------------------------------------------------

// connectedBusy is userID's busy time from the connected calendars they
// marked as counting (their primary calendars by default), through the
// platforms' free/busy (times only), cached for a minute. A platform that
// doesn't answer is skipped (logged) rather than blocking every slot.
func (s *Service) connectedBusy(ctx context.Context, userID uuid.UUID, from, to time.Time) []busySpan {
	if s.connections() == nil {
		return nil
	}
	from, to = from.Truncate(time.Minute), to.Truncate(time.Minute)
	if spans, ok := cache.busyFor(userID, from, to); ok {
		return spans
	}
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	conns, err := s.connectedFor(ctx, userID, uuid.Nil)
	if err != nil {
		slog.WarnContext(ctx, "calendar: connected busy: connections", "err", err)
		return nil
	}
	cals, _, err := s.viewCalendars(ctx, userID, uuid.Nil, conns)
	if err != nil {
		return nil
	}
	var out []busySpan
	failed := false
	for _, c := range conns {
		var busy []extCalendar
		for _, cal := range cals[c.ID] {
			if cal.Busy {
				busy = append(busy, extCalendar{ID: cal.ID, Name: cal.Name, Primary: cal.Primary, platform: c.Provider, connID: c.ID, account: c.Account})
			}
		}
		if len(busy) == 0 {
			continue
		}
		sess, err := s.openConn(ctx, userID, uuid.Nil, c, false)
		if err != nil {
			failed = true
			continue
		}
		spans, err := sess.ad.freeBusy(ctx, busy, from, to)
		if err != nil {
			slog.WarnContext(ctx, "calendar: connected free/busy failed", "provider", c.Provider, "err", err)
			failed = true
			continue
		}
		out = append(out, spans...)
	}
	if !failed {
		cache.putBusy(userID, from, to, out)
	}
	return out
}

// subtractSpan removes [a, b) from spans (a booking being moved shouldn't be
// blocked by its own platform copy).
func subtractSpan(spans []busySpan, a, b time.Time) []busySpan {
	out := spans[:0:0]
	for _, s := range spans {
		if !s.End.After(a) || !s.Start.Before(b) {
			out = append(out, s)
			continue
		}
		if s.Start.Before(a) {
			out = append(out, busySpan{Start: s.Start, End: a})
		}
		if s.End.After(b) {
			out = append(out, busySpan{Start: b, End: s.End})
		}
	}
	return out
}

// ---- booking copies ---------------------------------------------------------------------

type mirrorKey struct{ conn, cal, event string }

// mirrorKeys are the platform copies of the host's confirmed bookings: the
// app's own event shows each, so the copy is left out of connected views.
func (s *Service) mirrorKeys(ctx context.Context, userID uuid.UUID) (map[mirrorKey]bool, error) {
	rows, err := s.d.Pool.Query(ctx, `SELECT ext_connection_id, ext_calendar_id, ext_event_id FROM calendar_bookings
		WHERE host_id = $1 AND status = 'confirmed' AND ext_event_id IS NOT NULL AND ends_at > now() - interval '400 days'`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[mirrorKey]bool{}
	for rows.Next() {
		var k mirrorKey
		if err := rows.Scan(&k.conn, &k.cal, &k.event); err != nil {
			return nil, err
		}
		out[k] = true
	}
	return out, rows.Err()
}

// connectedOp is an outbox item changing a booking's platform copy after
// the booking changed: move (new time) or delete.
type connectedOp struct {
	UserID       uuid.UUID `json:"user_id"`
	ConnectionID string    `json:"connection_id"`
	CalendarID   string    `json:"calendar_id"`
	EventID      string    `json:"event_id"`
	Op           string    `json:"op"` // move | delete
	StartsAt     time.Time `json:"starts_at"`
	EndsAt       time.Time `json:"ends_at"`
	Timezone     string    `json:"timezone"`
}

func enqueueConnectedOp(ctx context.Context, q db.Querier, op connectedOp) error {
	return enqueue(ctx, q, "connected_event", op)
}

// deliverConnected runs one connectedOp. Connections that are gone or need
// the person can't be retried into working: those are dropped (logged).
func (s *Service) deliverConnected(ctx context.Context, op connectedOp) error {
	sess, err := s.session(ctx, op.UserID, uuid.Nil, op.ConnectionID, true)
	if err == nil {
		switch op.Op {
		case "delete":
			err = sess.ad.remove(ctx, op.CalendarID, op.EventID, false)
		case "move":
			_, err = sess.ad.update(ctx, op.CalendarID, op.EventID, extWrite{TimeSet: true, Start: op.StartsAt, End: op.EndsAt, Timezone: op.Timezone})
		}
	}
	if err == nil {
		cache.stale(op.UserID, op.ConnectionID, op.CalendarID)
		return nil
	}
	switch app.ConnectionErrorCode(err) {
	case app.ConnectionUnavailable, app.ConnectionRateLimited:
		return err
	}
	if errors.Is(err, errPlatformGone) {
		return nil
	}
	var he *httpx.Error
	if errors.As(err, &he) && he.Status >= 500 {
		return err
	}
	slog.WarnContext(ctx, "calendar: dropping a connected calendar change", "op", op.Op, "provider_err", err)
	return nil
}

// ---- conferencing options ---------------------------------------------------------------

// conferenceOptionsFor are the meeting providers for a target calendar
// (connID/calID empty: the app's own): the wired Conferencing's, plus the
// platform's own link on a connected calendar (which replaces the same
// provider made another way).
func (s *Service) conferenceOptionsFor(ctx context.Context, userID, org uuid.UUID, connID, calID string) ([]ConferenceOption, *calSession, error) {
	opts := seamOptions(ctx, userID, org)
	if connID == "" {
		return opts, nil, nil
	}
	sess, err := s.session(ctx, userID, org, connID, true)
	if err != nil {
		return nil, nil, err
	}
	cal, listed := s.connectedCalendar(ctx, userID, org, sess.conn, calID)
	if !listed {
		return nil, nil, httpx.NotFound("That calendar isn't in " + sess.conn.label() + ".")
	}
	if !cal.CanWrite {
		return nil, nil, httpx.BadRequest("This account can only read that calendar.")
	}
	out := []ConferenceOption{}
	native := map[string]bool{}
	for _, o := range cal.Conferencing {
		native[o.ID] = true
	}
	for _, o := range opts {
		if !native[o.ID] {
			out = append(out, o)
		}
	}
	return append(out, cal.Conferencing...), sess, nil
}

// trimProvider normalises a provider id ("" = the app's own).
func trimProvider(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ProviderApp
	}
	return p
}

// ---- for other features ------------------------------------------------------------

var calendarService atomic.Pointer[Service]


// PlatformMeeting is an event with a platform meeting link (Google Meet,
// Teams, Zoom) on a person's calendar or one of their connected calendars,
// for a meetings feature's lists (through stitching).
type PlatformMeeting struct {
	Ref        string // connected: "<connection id>:<calendar id>:<event id>"; the app's own: the event id
	Title      string
	StartsAt   time.Time
	EndsAt     time.Time
	AllDay     bool
	Provider   string // google_meet | teams | zoom
	JoinURL    string
	Organizer  string
	MyResponse string // needs_action | accepted | tentative | declined | "" (organizer)
	Link       string // app path that opens it in the calendar
	Source     *app.SourceRef // nil for the app's own events
}

// PlatformMeetings lists caller's events with platform meeting links
// overlapping [from, to): their own calendar's and their visible connected
// calendars' (live, as caller; agents: each connection's scope), declined
// ones left out, by start. Without the calendar module it returns none.
func PlatformMeetings(ctx context.Context, caller *auth.Identity, orgID uuid.UUID, from, to time.Time) ([]PlatformMeeting, error) {
	s := calendarService.Load()
	if s == nil || caller == nil {
		return nil, nil
	}
	if to.Sub(from) > maxWindow {
		to = from.Add(maxWindow)
	}
	zone := s.zoneFor(ctx, s.d.Pool, caller.UserID, time.UTC)
	out := []PlatformMeeting{}
	own, err := s.occurrences(ctx, s.d.Pool, caller.UserID, from, to, maxOccurrences)
	if err != nil {
		return nil, err
	}
	readable, err := s.agentReadable(ctx, caller)
	if err != nil {
		return nil, err
	}
	for _, o := range own {
		if o.ConferenceURL == nil || (o.MyResponse != nil && *o.MyResponse == ResponseDeclined) || !readable(KindEvent, o.EventID) {
			continue
		}
		_, provider := meetingIn(*o.ConferenceURL)
		if provider == "" {
			continue // the app's own call: its meetings feature lists it
		}
		m := PlatformMeeting{Ref: o.EventID.String(), Title: o.Title, StartsAt: o.StartsAt, EndsAt: o.EndsAt, AllDay: o.AllDay, Provider: provider,
			JoinURL: *o.ConferenceURL, Organizer: o.Owner.DisplayName, Link: eventLink(o.EventID)}
		if o.OccurrenceStart != nil {
			m.Link += "?occurrence=" + url.QueryEscape(o.OccurrenceStart.Format(time.RFC3339))
		}
		if o.MyResponse != nil {
			m.MyResponse = *o.MyResponse
		}
		out = append(out, m)
	}
	res, err := s.connectedRange(ctx, caller, orgID, from, to, "")
	if err != nil {
		return nil, err
	}
	for _, e := range res.Items {
		if e.ConferenceURL == nil || e.ConferenceProvider == "" || e.ConferenceProvider == ProviderApp || (e.MyResponse != nil && *e.MyResponse == ResponseDeclined) {
			continue
		}
		date := e.StartsAt.In(zone).Format(time.DateOnly)
		if e.StartDate != nil {
			date = *e.StartDate
		}
		m := PlatformMeeting{Ref: e.EventID, Title: e.Title, StartsAt: e.StartsAt, EndsAt: e.EndsAt, AllDay: e.AllDay,
			Provider: e.ConferenceProvider, JoinURL: *e.ConferenceURL, Organizer: e.Owner.DisplayName, Source: e.Source,
			Link: "/calendar?view=day&date=" + date + "&open=" + url.QueryEscape(e.EventID)}
		if e.MyResponse != nil {
			m.MyResponse = *e.MyResponse
		}
		out = append(out, m)
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].StartsAt.Before(out[b].StartsAt) })
	return out, nil
}
