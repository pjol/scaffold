package calendar

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/notify"
	"scaffoldapp/backend/internal/orgs"
)

// attendeeInput names a platform user (user_id, from the people picker) or
// an external guest (email, optional name).
type attendeeInput struct {
	UserID *uuid.UUID `json:"user_id"`
	Email  string     `json:"email"`
	Name   string     `json:"name"`
}

// eventInput is the create/update body. On update only the fields sent
// change; any time field means the full set is sent (all_day, timezone and
// starts_at/ends_at or start_date/end_date).
type eventInput struct {
	Title           *string          `json:"title"`
	Description     *string          `json:"description"`
	Location        *string          `json:"location"`
	StartsAt        *time.Time       `json:"starts_at"`
	EndsAt          *time.Time       `json:"ends_at"`
	StartDate       *string          `json:"start_date"`
	EndDate         *string          `json:"end_date"`
	Timezone        *string          `json:"timezone"`
	AllDay          *bool            `json:"all_day"`
	Recurrence      json.RawMessage  `json:"recurrence"`
	Visibility      *string          `json:"visibility"`
	ShowAs          *string          `json:"show_as"`
	Color           *string          `json:"color"`
	Reminders       *[]int           `json:"reminders"`
	Attendees       *[]attendeeInput `json:"attendees"`
	Conference      *string          `json:"conference"`
	SendUpdates     *bool            `json:"send_updates"`
	Scope           string           `json:"scope"`
	OccurrenceStart *time.Time       `json:"occurrence_start"`
}

type resolvedAttendee struct {
	userID *uuid.UUID
	email  string
	name   string
}

func (a resolvedAttendee) key() string {
	if a.userID != nil {
		return "u:" + a.userID.String()
	}
	return "e:" + a.email
}

// patch is a validated eventInput.
type patch struct {
	title, description, location *string
	timeSet                      bool
	start, end                   time.Time
	allDay                       bool
	tz                           string
	recSet                       bool
	rec                          *Recurrence
	visibility, showAs, color    *string
	reminders                    *[]int
	attendees                    *[]resolvedAttendee
	conference                   string
	sendUpdates                  bool
}

func (s *Service) parsePatch(ctx context.Context, callerID uuid.UUID, in eventInput, create bool, defaultTZ string, allowExternal bool) (patch, error) {
	p := patch{sendUpdates: in.SendUpdates == nil || *in.SendUpdates}
	if in.Title != nil || create {
		t := ""
		if in.Title != nil {
			t = *in.Title
		}
		t, ok := cleanText(t, maxTitle)
		if t == "" {
			return p, httpx.BadRequest("Add a title.")
		}
		if !ok {
			return p, httpx.BadRequest("Titles are up to 200 characters.")
		}
		p.title = &t
	}
	if in.Description != nil {
		d, ok := cleanText(*in.Description, maxDescription)
		if !ok {
			return p, httpx.BadRequest("Descriptions are up to 8000 characters.")
		}
		p.description = &d
	}
	if in.Location != nil {
		l, ok := cleanText(*in.Location, maxLocation)
		if !ok {
			return p, httpx.BadRequest("Locations are up to 500 characters.")
		}
		p.location = &l
	}
	if create || in.StartsAt != nil || in.EndsAt != nil || in.StartDate != nil || in.EndDate != nil || in.AllDay != nil || in.Timezone != nil {
		if err := p.parseTimes(in, defaultTZ); err != nil {
			return p, err
		}
	}
	if len(in.Recurrence) > 0 {
		p.recSet = true
		if string(in.Recurrence) != "null" {
			var rc Recurrence
			if err := json.Unmarshal(in.Recurrence, &rc); err != nil {
				return p, httpx.BadRequest("recurrence must be an object or null.")
			}
			p.rec = &rc
		}
	}
	if in.Visibility != nil {
		if *in.Visibility != "busy" && *in.Visibility != "details" {
			return p, httpx.BadRequest("visibility must be busy or details.")
		}
		p.visibility = in.Visibility
	}
	if in.ShowAs != nil {
		if *in.ShowAs != "busy" && *in.ShowAs != "free" {
			return p, httpx.BadRequest("show_as must be busy or free.")
		}
		p.showAs = in.ShowAs
	}
	if in.Color != nil {
		if !validColor(*in.Color) {
			return p, httpx.BadRequest("Pick one of the calendar colors.")
		}
		p.color = in.Color
	}
	if in.Reminders != nil {
		r, err := validateReminders(*in.Reminders)
		if err != nil {
			return p, err
		}
		p.reminders = &r
	}
	if in.Attendees != nil {
		list, err := s.resolveAttendees(ctx, callerID, *in.Attendees, allowExternal)
		if err != nil {
			return p, err
		}
		p.attendees = &list
	}
	if in.Conference != nil {
		switch *in.Conference {
		case "", "add", "remove":
			p.conference = *in.Conference
		default:
			return p, httpx.BadRequest("conference must be add or remove.")
		}
		if p.conference == "add" && !VideoAvailable() {
			return p, httpx.NewError(http.StatusConflict, "video_unavailable", "Video calls aren't set up in this app.")
		}
	}
	return p, nil
}

func (p *patch) parseTimes(in eventInput, defaultTZ string) error {
	p.timeSet = true
	p.tz = defaultTZ
	if in.Timezone != nil {
		p.tz = *in.Timezone
	}
	if !validZone(p.tz) {
		return httpx.BadRequest("Pick a valid time zone.")
	}
	loc := zoneOr(p.tz, time.UTC)
	p.allDay = in.AllDay != nil && *in.AllDay
	if p.allDay {
		if in.StartDate == nil || in.EndDate == nil {
			return httpx.BadRequest("All-day events need start_date and end_date.")
		}
		a, err1 := time.ParseInLocation(time.DateOnly, *in.StartDate, loc)
		b, err2 := time.ParseInLocation(time.DateOnly, *in.EndDate, loc)
		if err1 != nil || err2 != nil || b.Before(a) {
			return httpx.BadRequest("Dates must look like 2026-11-02, the end on or after the start.")
		}
		if b.Sub(a) > 92*24*time.Hour {
			return httpx.BadRequest("All-day events last up to 92 days.")
		}
		p.start = a.UTC()
		p.end = time.Date(b.Year(), b.Month(), b.Day()+1, 0, 0, 0, 0, loc).UTC()
		return nil
	}
	if in.StartsAt == nil || in.EndsAt == nil {
		return httpx.BadRequest("Events need starts_at and ends_at.")
	}
	p.start, p.end = in.StartsAt.UTC().Truncate(time.Minute), in.EndsAt.UTC().Truncate(time.Minute)
	if !p.end.After(p.start) {
		return httpx.BadRequest("The end must be after the start.")
	}
	if p.end.Sub(p.start) > maxEventLength {
		return httpx.BadRequest("Events last up to 14 days; use an all-day event for longer.")
	}
	return nil
}

// resolveAttendees checks guests: platform users must share an
// organization with the caller; an email that belongs to such a colleague
// becomes them; other emails are external guests (refused when
// allowExternal is false, as for agent tools). The caller is skipped.
func (s *Service) resolveAttendees(ctx context.Context, callerID uuid.UUID, in []attendeeInput, allowExternal bool) ([]resolvedAttendee, error) {
	if len(in) > maxAttendees {
		return nil, httpx.BadRequest("Up to 100 guests per event.")
	}
	var ids []uuid.UUID
	var emails []string
	for _, a := range in {
		switch {
		case a.UserID != nil:
			ids = append(ids, *a.UserID)
		case a.Email != "":
			e := normalizeEmail(a.Email)
			if e == "" {
				return nil, httpx.BadRequest(a.Email + " isn't a valid email address.")
			}
			emails = append(emails, e)
		default:
			return nil, httpx.BadRequest("Each guest needs a user_id or an email.")
		}
	}
	byEmail := map[string]uuid.UUID{}
	if len(emails) > 0 {
		rows, err := s.d.Pool.Query(ctx, `
			SELECT u.id, lower(u.email) FROM users u
			WHERE lower(u.email) = ANY($2) AND u.status = 'active' AND u.id <> $1
			  AND EXISTS (SELECT 1 FROM org_members a JOIN org_members b ON b.org_id = a.org_id
			              WHERE a.user_id = $1 AND b.user_id = u.id)`, callerID, emails)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id uuid.UUID
			var e string
			if err := rows.Scan(&id, &e); err != nil {
				rows.Close()
				return nil, err
			}
			byEmail[e] = id
		}
		rows.Close()
	}
	if len(ids) > 0 {
		ok, err := coMembers(ctx, s.d.Pool, callerID, ids)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			if id != callerID && !ok[id] {
				return nil, httpx.BadRequest("You can invite people from your " + orgs.Nouns() + ", or anyone by email.")
			}
		}
	}
	seen := map[string]bool{}
	out := []resolvedAttendee{}
	add := func(a resolvedAttendee) {
		if a.userID != nil && *a.userID == callerID {
			return
		}
		if !seen[a.key()] {
			seen[a.key()] = true
			out = append(out, a)
		}
	}
	for _, a := range in {
		if a.UserID != nil {
			id := *a.UserID
			add(resolvedAttendee{userID: &id})
			continue
		}
		e := normalizeEmail(a.Email)
		if id, ok := byEmail[e]; ok {
			add(resolvedAttendee{userID: &id})
			continue
		}
		if !allowExternal {
			return nil, httpx.BadRequest("Guests outside your " + orgs.Nouns() + " can't be invited from here.")
		}
		name, _ := cleanText(a.Name, 100)
		add(resolvedAttendee{email: e, name: name})
	}
	return out, nil
}

// ---- handlers -----------------------------------------------------------------

// RangeResponse is a calendar window: occurrences plus busy time from
// subscribed calendars.
type RangeResponse struct {
	Items     []Occurrence   `json:"items"`
	Busy      []ExternalBusy `json:"busy"`
	Truncated bool           `json:"truncated"`
}

// ExternalBusy is a busy block from a subscribed calendar (no details).
type ExternalBusy struct {
	SubscriptionID uuid.UUID `json:"subscription_id"`
	Name           string    `json:"name"`
	Color          string    `json:"color"`
	StartsAt       time.Time `json:"starts_at"`
	EndsAt         time.Time `json:"ends_at"`
}

func (s *Service) listRange(ctx context.Context, userID uuid.UUID, from, to time.Time) (RangeResponse, error) {
	items, err := s.occurrences(ctx, s.d.Pool, userID, from, to, maxOccurrences+1)
	if err != nil {
		return RangeResponse{}, err
	}
	out := RangeResponse{Items: items, Busy: []ExternalBusy{}}
	if len(items) > maxOccurrences {
		out.Items, out.Truncated = items[:maxOccurrences], true
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT s.id, s.name, s.color, lower(b.span), upper(b.span)
		FROM calendar_busy_blocks b JOIN calendar_subscriptions s ON s.id = b.subscription_id
		WHERE b.user_id = $1 AND s.enabled AND b.span && tstzrange($2, $3)
		ORDER BY lower(b.span) LIMIT 2000`, userID, from, to)
	if err != nil {
		return RangeResponse{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var b ExternalBusy
		if err := rows.Scan(&b.SubscriptionID, &b.Name, &b.Color, &b.StartsAt, &b.EndsAt); err != nil {
			return RangeResponse{}, err
		}
		out.Busy = append(out.Busy, b)
	}
	return out, rows.Err()
}

func (s *Service) listHandler(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	from, to, err := parseWindow(r.URL.Query().Get("from"), r.URL.Query().Get("to"), maxWindow)
	if err != nil {
		return err
	}
	out, err := s.listRange(r.Context(), id.UserID, from, to)
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

func parseOccurrence(r *http.Request) (*time.Time, error) {
	v := r.URL.Query().Get("occurrence")
	if v == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return nil, httpx.BadRequest("occurrence must be an RFC 3339 time.")
	}
	t = t.UTC()
	return &t, nil
}

func (s *Service) getHandler(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	eid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	occ, err := parseOccurrence(r)
	if err != nil {
		return err
	}
	res, err := s.resolve(r.Context(), s.d.Pool, id.UserID, eid, occ)
	if err != nil {
		return err
	}
	d, err := s.detail(r.Context(), s.d.Pool, id.UserID, res)
	if err != nil {
		return err
	}
	httpx.OK(w, d)
	return nil
}

func (s *Service) createHandler(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	var in eventInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	d, err := s.createEvent(r.Context(), id, in, true)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, d)
	return nil
}

// createEvent puts a new event on the caller's calendar and invites its
// guests. allowExternal=false (agent tools) refuses email guests.
func (s *Service) createEvent(ctx context.Context, caller *auth.Identity, in eventInput, allowExternal bool) (EventDetail, error) {
	st, err := s.loadSettings(ctx, s.d.Pool, caller.UserID)
	if err != nil {
		return EventDetail{}, err
	}
	p, err := s.parsePatch(ctx, caller.UserID, in, true, st.Timezone, allowExternal)
	if err != nil {
		return EventDetail{}, err
	}
	loc := zoneOr(p.tz, time.UTC)
	e := eventRow{
		ID: uuid.New(), OwnerID: caller.UserID, Title: *p.title, StartsAt: p.start, EndsAt: p.end, Timezone: p.tz,
		AllDay: p.allDay, Status: "confirmed", Visibility: "busy", ShowAs: "busy", Color: "primary",
	}
	if p.allDay {
		e.ShowAs = "free"
	} else {
		e.Reminders = toInt32(st.DefaultReminders)
	}
	applyContent(&e, p)
	if p.rec != nil {
		rule, err := p.rec.toRule(e.StartsAt, loc)
		if err != nil {
			return EventDetail{}, err
		}
		str := rule.String(e.AllDay, loc)
		e.RRule, e.SeriesUntil = &str, seriesUntil(rule, e.StartsAt, loc, e.duration())
	}
	var attendees []resolvedAttendee
	if p.attendees != nil {
		attendees = *p.attendees
	}
	var conf *Conference
	if p.conference == "add" {
		if conf, err = s.createConference(ctx, e, attendees, nil); err != nil {
			return EventDetail{}, err
		}
		e.ConferenceURL, e.ConferenceID = &conf.JoinURL, &conf.ID
	}
	var notifyUsers []uuid.UUID
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if err := insertEvent(ctx, tx, e); err != nil {
			return err
		}
		added, err := s.insertAttendees(ctx, tx, e.ID, attendees, ResponseNeedsAction)
		if err != nil {
			return err
		}
		notifyUsers = platformIDs(added)
		return s.notifyAttendees(ctx, tx, e, nil, added, inviteNew)
	})
	if err != nil {
		if conf != nil {
			s.cancelConferenceNow(ctx, conf.ID)
		}
		return EventDetail{}, err
	}
	kickOutbox()
	s.changed(ctx, "events", append(notifyUsers, caller.UserID)...)
	res, err := s.resolve(ctx, s.d.Pool, caller.UserID, e.ID, nil)
	if err != nil {
		return EventDetail{}, err
	}
	return s.detail(ctx, s.d.Pool, caller.UserID, res)
}

func toInt32(v []int) []int32 {
	out := make([]int32, len(v))
	for i, n := range v {
		out[i] = int32(n)
	}
	return out
}

func applyContent(e *eventRow, p patch) {
	if p.title != nil {
		e.Title = *p.title
	}
	if p.description != nil {
		e.Description = *p.description
	}
	if p.location != nil {
		e.Location = *p.location
	}
	if p.visibility != nil {
		e.Visibility = *p.visibility
	}
	if p.showAs != nil {
		e.ShowAs = *p.showAs
	}
	if p.color != nil {
		e.Color = *p.color
	}
	if p.reminders != nil {
		e.Reminders = toInt32(*p.reminders)
	}
}

// meaningful reports whether attendees should hear about a change.
func meaningful(a, b eventRow) bool {
	return a.Title != b.Title || a.Description != b.Description || a.Location != b.Location ||
		!a.StartsAt.Equal(b.StartsAt) || !a.EndsAt.Equal(b.EndsAt) || a.AllDay != b.AllDay || a.Timezone != b.Timezone ||
		ptrStr(a.RRule) != ptrStr(b.RRule) || ptrStr(a.ConferenceURL) != ptrStr(b.ConferenceURL) || a.Status != b.Status
}

func ptrStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func insertEvent(ctx context.Context, q db.Querier, e eventRow) error {
	if e.Reminders == nil {
		e.Reminders = []int32{}
	}
	_, err := q.Exec(ctx, `
		INSERT INTO calendar_events (id, owner_id, recurring_event_id, original_start, title, description, location,
		  conference_url, conference_id, starts_at, ends_at, timezone, all_day, rrule, series_until, status,
		  visibility, show_as, color, reminders, sequence)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21)`,
		e.ID, e.OwnerID, e.RecurringEventID, e.OriginalStart, e.Title, e.Description, e.Location,
		e.ConferenceURL, e.ConferenceID, e.StartsAt, e.EndsAt, e.Timezone, e.AllDay, e.RRule, e.SeriesUntil, e.Status,
		e.Visibility, e.ShowAs, e.Color, e.Reminders, e.Sequence)
	return err
}

// saveEvent writes every mutable column of e and bumps its sequence.
func saveEvent(ctx context.Context, q db.Querier, e eventRow) error {
	if e.Reminders == nil {
		e.Reminders = []int32{}
	}
	_, err := q.Exec(ctx, `
		UPDATE calendar_events SET title = $2, description = $3, location = $4, conference_url = $5, conference_id = $6,
		  starts_at = $7, ends_at = $8, timezone = $9, all_day = $10, rrule = $11, series_until = $12, status = $13,
		  visibility = $14, show_as = $15, color = $16, reminders = $17, original_start = $18,
		  recurring_event_id = $19, sequence = sequence + 1, updated_at = now()
		WHERE id = $1`,
		e.ID, e.Title, e.Description, e.Location, e.ConferenceURL, e.ConferenceID, e.StartsAt, e.EndsAt, e.Timezone,
		e.AllDay, e.RRule, e.SeriesUntil, e.Status, e.Visibility, e.ShowAs, e.Color, e.Reminders, e.OriginalStart,
		e.RecurringEventID)
	return err
}

func bumpSequence(ctx context.Context, q db.Querier, id uuid.UUID) error {
	_, err := q.Exec(ctx, `UPDATE calendar_events SET sequence = sequence + 1, updated_at = now() WHERE id = $1`, id)
	return err
}

// insertAttendees adds guests; externals get an RSVP token (hash stored).
func (s *Service) insertAttendees(ctx context.Context, q db.Querier, eventID uuid.UUID, list []resolvedAttendee, response string) ([]fullAttendee, error) {
	var out []fullAttendee
	for _, a := range list {
		id := uuid.New()
		var tokenHash *string
		if a.userID == nil {
			t, _ := s.tokenFor("rsvp", id)
			h := hashToken(t)
			tokenHash = &h
		}
		var respondedAt *time.Time
		if response != ResponseNeedsAction {
			now := time.Now()
			respondedAt = &now
		}
		var fa fullAttendee
		err := q.QueryRow(ctx, `
			WITH ins AS (
			  INSERT INTO calendar_attendees (id, event_id, user_id, email, name, response, token_hash, responded_at)
			  VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			  RETURNING id, user_id, email, name, response)
			SELECT ins.id, ins.user_id, COALESCE(u.display_name, ins.name), COALESCE(u.email, ins.email), ins.response
			FROM ins LEFT JOIN users u ON u.id = ins.user_id`,
			id, eventID, a.userID, a.email, a.name, response, tokenHash, respondedAt).
			Scan(&fa.ID, &fa.UserID, &fa.Name, &fa.Email, &fa.Response)
		if err != nil {
			return nil, err
		}
		out = append(out, fa)
	}
	return out, nil
}

// syncAttendees makes eventID's guest list match want. Responses of people
// who stay are kept.
func (s *Service) syncAttendees(ctx context.Context, q db.Querier, eventID uuid.UUID, want []resolvedAttendee) (added, removed, kept []fullAttendee, err error) {
	rows, err := q.Query(ctx, `
		SELECT a.id, a.user_id, COALESCE(u.display_name, a.name), COALESCE(u.email, a.email), a.response
		FROM calendar_attendees a LEFT JOIN users u ON u.id = a.user_id WHERE a.event_id = $1`, eventID)
	if err != nil {
		return nil, nil, nil, err
	}
	current, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (fullAttendee, error) {
		var a fullAttendee
		return a, row.Scan(&a.ID, &a.UserID, &a.Name, &a.Email, &a.Response)
	})
	if err != nil {
		return nil, nil, nil, err
	}
	keyOf := func(a fullAttendee) string {
		if a.UserID != nil {
			return "u:" + a.UserID.String()
		}
		return "e:" + a.Email
	}
	wantKeys := map[string]bool{}
	for _, a := range want {
		wantKeys[a.key()] = true
	}
	have := map[string]bool{}
	for _, a := range current {
		have[keyOf(a)] = true
		if wantKeys[keyOf(a)] {
			kept = append(kept, a)
			continue
		}
		removed = append(removed, a)
		if _, err := q.Exec(ctx, `DELETE FROM calendar_attendees WHERE id = $1`, a.ID); err != nil {
			return nil, nil, nil, err
		}
	}
	var toAdd []resolvedAttendee
	for _, a := range want {
		if !have[a.key()] {
			toAdd = append(toAdd, a)
		}
	}
	added, err = s.insertAttendees(ctx, q, eventID, toAdd, ResponseNeedsAction)
	return added, removed, kept, err
}

// ---- update ---------------------------------------------------------------------

func (s *Service) updateHandler(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	eid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var in eventInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	d, err := s.updateEvent(r.Context(), id, eid, in)
	if err != nil {
		return err
	}
	httpx.OK(w, d)
	return nil
}

var errScope = httpx.BadRequest("Choose which events change: this, following or all.")

func (s *Service) updateEvent(ctx context.Context, caller *auth.Identity, id uuid.UUID, in eventInput) (EventDetail, error) {
	r, err := s.resolve(ctx, s.d.Pool, caller.UserID, id, in.OccurrenceStart)
	if err != nil {
		return EventDetail{}, err
	}
	if !r.acc.isOwner {
		return EventDetail{}, errNotOrganizer
	}
	recurring := r.base.RRule != nil
	scope := in.Scope
	if !recurring {
		scope = ScopeAll
	} else if scope != ScopeThis && scope != ScopeFollowing && scope != ScopeAll {
		return EventDetail{}, errScope
	}
	p, err := s.parsePatch(ctx, caller.UserID, in, false, r.base.Timezone, true)
	if err != nil {
		return EventDetail{}, err
	}
	if scope == ScopeThis && (p.recSet || p.attendees != nil) {
		return EventDetail{}, httpx.BadRequest("Repeat and guests apply to the whole series: choose all events, or this and following.")
	}
	if scope == ScopeThis && p.timeSet && (p.allDay != r.base.AllDay || p.tz != r.base.Timezone) {
		return EventDetail{}, httpx.BadRequest("One occurrence can't change between all-day and timed, or change time zone.")
	}
	var bookingID *uuid.UUID
	var b uuid.UUID
	if err := s.d.Pool.QueryRow(ctx, `SELECT id FROM calendar_bookings WHERE event_id = $1 AND status = 'confirmed'`, r.base.ID).Scan(&b); err == nil {
		bookingID = &b
	}
	if bookingID != nil && p.rec != nil {
		return EventDetail{}, httpx.BadRequest("Booked meetings can't repeat.")
	}
	if recurring && scope == ScopeFollowing && r.occ.Equal(r.base.StartsAt) {
		scope = ScopeAll
	}

	// Video rooms are made before the transaction (a network call) and
	// cancelled if it fails.
	var conf *Conference
	if p.conference == "add" {
		target := r.instance()
		applyContent(&target, p)
		if p.timeSet {
			target.StartsAt, target.EndsAt = p.start, p.end
		}
		atts, err := loadFullAttendees(ctx, s.d.Pool, r.base.ID)
		if err != nil {
			return EventDetail{}, err
		}
		if conf, err = s.createConference(ctx, target, nil, atts); err != nil {
			return EventDetail{}, err
		}
	}

	var result struct {
		id       uuid.UUID
		occ      *time.Time
		users    []uuid.UUID
		confSync []eventRow
	}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT 1 FROM calendar_events WHERE id = $1 FOR UPDATE`, r.base.ID); err != nil {
			return err
		}
		rr, err := s.resolve(ctx, tx, caller.UserID, r.base.ID, r.occ)
		if err != nil {
			return err
		}
		var ch change
		switch {
		case !recurring:
			ch, err = s.updateSingle(ctx, tx, rr, p, conf, bookingID != nil)
		case scope == ScopeThis:
			ch, err = s.updateThis(ctx, tx, rr, p, conf)
		case scope == ScopeAll:
			ch, err = s.updateAll(ctx, tx, rr, p, conf)
		default:
			ch, err = s.updateFollowing(ctx, tx, rr, p, conf)
		}
		if err != nil {
			return err
		}
		result.id, result.occ, result.users, result.confSync = ch.id, ch.occ, ch.users, ch.confSync
		return nil
	})
	if err != nil {
		if conf != nil {
			s.cancelConferenceNow(ctx, conf.ID)
		}
		if isExclusionViolation(err) {
			return EventDetail{}, httpx.Conflict("That time overlaps another booking.")
		}
		return EventDetail{}, err
	}
	kickOutbox()
	for _, row := range result.confSync {
		s.syncConference(ctx, row)
	}
	s.changed(ctx, "events", append(result.users, caller.UserID)...)
	if bookingID != nil {
		s.changed(ctx, "bookings", caller.UserID)
	}
	res, err := s.resolve(ctx, s.d.Pool, caller.UserID, result.id, result.occ)
	if err != nil {
		return EventDetail{}, err
	}
	return s.detail(ctx, s.d.Pool, caller.UserID, res)
}

// change is what an update touched: where the edited occurrence now lives,
// who to refresh, and rows whose video room needs the new time.
type change struct {
	id       uuid.UUID
	occ      *time.Time
	users    []uuid.UUID
	confSync []eventRow
}

// applyConference sets or clears e's room; a cleared room is cancelled
// after commit when nothing else uses it.
func applyConference(e *eventRow, p patch, conf *Conference) (cleared *string) {
	switch p.conference {
	case "add":
		if conf != nil {
			cleared = e.ConferenceID
			e.ConferenceURL, e.ConferenceID = &conf.JoinURL, &conf.ID
		}
	case "remove":
		cleared = e.ConferenceID
		e.ConferenceURL, e.ConferenceID = nil, nil
	}
	return cleared
}

func (s *Service) tellChanges(ctx context.Context, q db.Querier, base eventRow, exceptions []eventRow, p patch, added, removed, kept []fullAttendee, meaningfulChange bool) error {
	if err := s.notifyAttendees(ctx, q, base, exceptions, removed, inviteRemoved); err != nil {
		return err
	}
	if err := s.notifyAttendees(ctx, q, base, exceptions, added, inviteNew); err != nil {
		return err
	}
	if meaningfulChange && p.sendUpdates {
		return s.notifyAttendees(ctx, q, base, exceptions, kept, inviteUpdated)
	}
	return nil
}

func (s *Service) updateSingle(ctx context.Context, tx pgx.Tx, r resolved, p patch, conf *Conference, booked bool) (change, error) {
	old := r.base
	row := old
	applyContent(&row, p)
	if p.timeSet {
		row.StartsAt, row.EndsAt, row.AllDay, row.Timezone = p.start, p.end, p.allDay, p.tz
	}
	cleared := applyConference(&row, p, conf)
	if p.rec != nil {
		rule, err := p.rec.toRule(row.StartsAt, row.loc())
		if err != nil {
			return change{}, err
		}
		str := rule.String(row.AllDay, row.loc())
		row.RRule, row.SeriesUntil = &str, seriesUntil(rule, row.StartsAt, row.loc(), row.duration())
	}
	if err := saveEvent(ctx, tx, row); err != nil {
		return change{}, err
	}
	row.Sequence++ // the invite we send must carry the new SEQUENCE
	if err := enqueueConferenceCancel(ctx, tx, cleared); err != nil {
		return change{}, err
	}
	if booked && p.timeSet {
		if _, err := tx.Exec(ctx, `
			UPDATE calendar_bookings SET starts_at = $2, ends_at = $3,
			  block = tstzrange($2::timestamptz - (starts_at - lower(block)), $3::timestamptz + (upper(block) - ends_at)),
			  updated_at = now()
			WHERE event_id = $1 AND status = 'confirmed'`, row.ID, row.StartsAt, row.EndsAt); err != nil {
			return change{}, err
		}
	}
	added, removed, kept, err := s.attendeeChanges(ctx, tx, row.ID, p)
	if err != nil {
		return change{}, err
	}
	if err := s.tellChanges(ctx, tx, row, nil, p, added, removed, kept, meaningful(old, row)); err != nil {
		return change{}, err
	}
	ch := change{id: row.ID, users: platformIDs(slices.Concat(added, removed, kept))}
	if row.RRule != nil {
		st := row.StartsAt
		ch.occ = &st
	}
	if row.ConferenceID != nil && cleared == nil && p.conference != "add" && (!old.StartsAt.Equal(row.StartsAt) || old.Title != row.Title) {
		ch.confSync = []eventRow{row}
	}
	return ch, nil
}

// attendeeChanges syncs the guest list when the patch carries one; else
// everyone is "kept".
func (s *Service) attendeeChanges(ctx context.Context, q db.Querier, eventID uuid.UUID, p patch) (added, removed, kept []fullAttendee, err error) {
	if p.attendees != nil {
		return s.syncAttendees(ctx, q, eventID, *p.attendees)
	}
	kept, err = loadFullAttendees(ctx, q, eventID)
	return nil, nil, kept, err
}

func (s *Service) updateThis(ctx context.Context, tx pgx.Tx, r resolved, p patch, conf *Conference) (change, error) {
	old := r.instance()
	inst := old
	applyContent(&inst, p)
	if p.timeSet {
		inst.StartsAt, inst.EndsAt = p.start, p.end
	}
	cleared := applyConference(&inst, p, conf)
	if r.exception != nil {
		if err := saveEvent(ctx, tx, inst); err != nil {
			return change{}, err
		}
	} else {
		inst.ID = uuid.New()
		inst.RecurringEventID = &r.base.ID
		inst.OriginalStart = r.occ
		inst.RRule, inst.SeriesUntil = nil, nil
		inst.Status = "confirmed"
		if err := insertEvent(ctx, tx, inst); err != nil {
			return change{}, err
		}
	}
	if err := enqueueConferenceCancel(ctx, tx, cleared); err != nil {
		return change{}, err
	}
	if err := bumpSequence(ctx, tx, r.base.ID); err != nil {
		return change{}, err
	}
	attendees, err := loadFullAttendees(ctx, tx, r.base.ID)
	if err != nil {
		return change{}, err
	}
	if meaningful(old, inst) && p.sendUpdates {
		if err := s.tellSeries(ctx, tx, r.base.ID, inviteUpdated); err != nil {
			return change{}, err
		}
	}
	ch := change{id: r.base.ID, occ: r.occ, users: platformIDs(attendees)}
	if inst.ConferenceID != nil && cleared == nil && p.conference != "add" && !old.StartsAt.Equal(inst.StartsAt) {
		ch.confSync = []eventRow{inst}
	}
	return ch, nil
}

// shiftSeries moves a series start by the wall-clock change that moved one
// of its occurrences from instStart to newStart (newEnd sets the length),
// so "all events" edits keep local times across daylight-saving changes.
func shiftSeries(baseStart, instStart, newStart, newEnd time.Time, oldLoc, newLoc *time.Location, allDay bool) (time.Time, time.Time) {
	i, n, b := instStart.In(oldLoc), newStart.In(newLoc), baseStart.In(oldLoc)
	dayDelta := int(civil(n).Sub(civil(i)).Hours() / 24)
	bd := civil(b).AddDate(0, 0, dayDelta)
	start := time.Date(bd.Year(), bd.Month(), bd.Day(), n.Hour(), n.Minute(), 0, 0, newLoc).UTC()
	if allDay {
		days := int(civil(newEnd.In(newLoc)).Sub(civil(n)).Hours() / 24)
		return start, time.Date(bd.Year(), bd.Month(), bd.Day()+days, 0, 0, 0, 0, newLoc).UTC()
	}
	return start, start.Add(newEnd.Sub(newStart))
}

// civil is t's calendar date at UTC midnight (for day arithmetic).
func civil(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// shiftWall moves t by the wall-clock difference between from and to.
func shiftWall(t, from, to time.Time, oldLoc, newLoc *time.Location) time.Time {
	s, _ := shiftSeries(t, from, to, to.Add(time.Hour), oldLoc, newLoc, false)
	return s
}

func (s *Service) updateAll(ctx context.Context, tx pgx.Tx, r resolved, p patch, conf *Conference) (change, error) {
	old := r.base
	row := old
	inst := r.instance()
	applyContent(&row, p)
	oldLoc := old.loc()
	if p.timeSet {
		row.StartsAt, row.EndsAt = shiftSeries(old.StartsAt, inst.StartsAt, p.start, p.end, oldLoc, zoneOr(p.tz, time.UTC), p.allDay)
		row.AllDay, row.Timezone = p.allDay, p.tz
	}
	cleared := applyConference(&row, p, conf)
	newLoc := row.loc()
	timeChanged := !row.StartsAt.Equal(old.StartsAt) || !row.EndsAt.Equal(old.EndsAt) || row.Timezone != old.Timezone || row.AllDay != old.AllDay
	ruleChanged := false
	occ := r.occ
	switch {
	case p.recSet && p.rec == nil:
		// Stop repeating: the edited occurrence becomes a single event.
		target := inst
		applyContent(&target, p)
		if p.timeSet {
			target.StartsAt, target.EndsAt = p.start, p.end
		}
		row.StartsAt, row.EndsAt = target.StartsAt, target.EndsAt
		row.RRule, row.SeriesUntil = nil, nil
		ruleChanged, occ = true, nil
	case p.recSet:
		rule, err := p.rec.toRule(row.StartsAt, newLoc)
		if err != nil {
			return change{}, err
		}
		str := rule.String(row.AllDay, newLoc)
		ruleChanged = str != ptrStr(old.RRule)
		row.RRule, row.SeriesUntil = &str, seriesUntil(rule, row.StartsAt, newLoc, row.duration())
	default:
		if rule, ok := old.rule(); ok {
			str := rule.String(row.AllDay, newLoc)
			row.RRule, row.SeriesUntil = &str, seriesUntil(rule, row.StartsAt, newLoc, row.duration())
		}
	}
	if timeChanged && occ != nil {
		t := shiftWall(*occ, old.StartsAt, row.StartsAt, oldLoc, newLoc)
		occ = &t
	}
	exceptions, err := loadExceptions(ctx, tx, old.ID)
	if err != nil {
		return change{}, err
	}
	var kept []eventRow
	for _, x := range exceptions {
		switch {
		case ruleChanged || (timeChanged && x.Status != "cancelled"):
			if _, err := tx.Exec(ctx, `DELETE FROM calendar_events WHERE id = $1`, x.ID); err != nil {
				return change{}, err
			}
			if err := enqueueConferenceCancel(ctx, tx, x.ConferenceID); err != nil {
				return change{}, err
			}
		case timeChanged:
			t := shiftWall(*x.OriginalStart, old.StartsAt, row.StartsAt, oldLoc, newLoc)
			x.OriginalStart = &t
			x.StartsAt, x.EndsAt = t, t.Add(row.duration())
			if err := saveEvent(ctx, tx, x); err != nil {
				return change{}, err
			}
			kept = append(kept, x)
		default:
			// Content-only edit: edited occurrences take the changed fields too.
			applyContent(&x, p)
			if p.conference != "" {
				x.ConferenceURL, x.ConferenceID = row.ConferenceURL, row.ConferenceID
			}
			if err := saveEvent(ctx, tx, x); err != nil {
				return change{}, err
			}
			kept = append(kept, x)
		}
	}
	if err := saveEvent(ctx, tx, row); err != nil {
		return change{}, err
	}
	row.Sequence++
	if err := enqueueConferenceCancel(ctx, tx, cleared); err != nil {
		return change{}, err
	}
	added, removed, keptA, err := s.attendeeChanges(ctx, tx, row.ID, p)
	if err != nil {
		return change{}, err
	}
	if err := s.tellChanges(ctx, tx, row, kept, p, added, removed, keptA, meaningful(old, row) || ruleChanged); err != nil {
		return change{}, err
	}
	ch := change{id: row.ID, occ: occ, users: platformIDs(slices.Concat(added, removed, keptA))}
	if row.ConferenceID != nil && cleared == nil && p.conference != "add" && timeChanged {
		ch.confSync = []eventRow{row}
	}
	return ch, nil
}

// updateFollowing splits the series at the occurrence: the original ends
// the moment before it, and a new series carries the change from there.
// Deleted occurrences after the split move to the new series; edited ones
// are replaced by the change.
func (s *Service) updateFollowing(ctx context.Context, tx pgx.Tx, r resolved, p patch, conf *Conference) (change, error) {
	old := r.base
	occ := *r.occ
	inst := r.instance()
	oldLoc := old.loc()
	rule, ok := old.rule()
	if !ok {
		return change{}, errEventNotFound
	}

	newRow := old
	newRow.ID = uuid.New()
	newRow.Sequence = 0
	applyContent(&newRow, inst2patch(inst, p))
	newRow.StartsAt, newRow.EndsAt = inst.StartsAt, inst.EndsAt
	if p.timeSet {
		newRow.StartsAt, newRow.EndsAt, newRow.AllDay, newRow.Timezone = p.start, p.end, p.allDay, p.tz
	}
	newLoc := newRow.loc()
	switch {
	case p.recSet && p.rec == nil:
		newRow.RRule, newRow.SeriesUntil = nil, nil
	case p.recSet:
		nr, err := p.rec.toRule(newRow.StartsAt, newLoc)
		if err != nil {
			return change{}, err
		}
		str := nr.String(newRow.AllDay, newLoc)
		newRow.RRule, newRow.SeriesUntil = &str, seriesUntil(nr, newRow.StartsAt, newLoc, newRow.duration())
	default:
		nr := rule
		if rule.Count > 0 {
			nr.Count = max(rule.Count-rule.CountBefore(old.StartsAt, oldLoc, occ), 1)
		}
		str := nr.String(newRow.AllDay, newLoc)
		newRow.RRule, newRow.SeriesUntil = &str, seriesUntil(nr, newRow.StartsAt, newLoc, newRow.duration())
	}
	cleared := applyConference(&newRow, p, conf)
	_ = cleared // the original series keeps its room; the new one shares it unless replaced

	// End the original series just before the split.
	until := occ.Add(-time.Second)
	truncated := rule
	truncated.Count, truncated.Until = 0, &until
	oldRow := old
	str := truncated.String(old.AllDay, oldLoc)
	oldRow.RRule, oldRow.SeriesUntil = &str, seriesUntil(truncated, old.StartsAt, oldLoc, old.duration())
	if err := saveEvent(ctx, tx, oldRow); err != nil {
		return change{}, err
	}
	if err := insertEvent(ctx, tx, newRow); err != nil {
		return change{}, err
	}

	// Move or drop exceptions from the split onwards.
	exceptions, err := loadExceptions(ctx, tx, old.ID)
	if err != nil {
		return change{}, err
	}
	for _, x := range exceptions {
		if x.OriginalStart.Before(occ) {
			continue
		}
		if x.Status == "cancelled" && newRow.RRule != nil && !x.OriginalStart.Equal(occ) {
			t := shiftWall(*x.OriginalStart, inst.StartsAt, newRow.StartsAt, oldLoc, newLoc)
			x.RecurringEventID, x.OriginalStart = &newRow.ID, &t
			x.StartsAt, x.EndsAt = t, t.Add(newRow.duration())
			if err := saveEvent(ctx, tx, x); err != nil {
				return change{}, err
			}
			continue
		}
		if _, err := tx.Exec(ctx, `DELETE FROM calendar_events WHERE id = $1`, x.ID); err != nil {
			return change{}, err
		}
		if x.ConferenceID != nil && ptrStr(x.ConferenceID) != ptrStr(newRow.ConferenceID) {
			if err := enqueueConferenceCancel(ctx, tx, x.ConferenceID); err != nil {
				return change{}, err
			}
		}
	}

	// Guests: the new series starts with the same people (and responses)
	// unless the patch names a new list.
	oldAtt, err := loadFullAttendees(ctx, tx, old.ID)
	if err != nil {
		return change{}, err
	}
	want := make([]resolvedAttendee, 0, len(oldAtt))
	responses := map[string]string{}
	for _, a := range oldAtt {
		ra := resolvedAttendee{userID: a.UserID, name: a.Name}
		if a.UserID == nil {
			ra.email = a.Email
		}
		want = append(want, ra)
		responses[ra.key()] = a.Response
	}
	if p.attendees != nil {
		want = *p.attendees
	}
	var newAtt []fullAttendee
	for _, a := range want {
		resp := responses[a.key()]
		if resp == "" {
			resp = ResponseNeedsAction
		}
		list, err := s.insertAttendees(ctx, tx, newRow.ID, []resolvedAttendee{a}, resp)
		if err != nil {
			return change{}, err
		}
		newAtt = append(newAtt, list...)
	}

	// The original series' guests hear about the shortened series; the new
	// series goes out as a new invitation (a new UID) to email guests and
	// to platform users who weren't on the original.
	if err := s.tellSeries(ctx, tx, old.ID, inviteUpdated); err != nil {
		return change{}, err
	}
	wasOn := map[string]bool{}
	for _, a := range oldAtt {
		if a.UserID != nil {
			wasOn[a.UserID.String()] = true
		}
	}
	var tell []fullAttendee
	for _, a := range newAtt {
		if a.UserID == nil || !wasOn[a.UserID.String()] {
			tell = append(tell, a)
		}
	}
	if err := s.notifyAttendees(ctx, tx, newRow, nil, tell, inviteNew); err != nil {
		return change{}, err
	}
	ch := change{id: newRow.ID, users: platformIDs(slices.Concat(oldAtt, newAtt))}
	if newRow.RRule != nil {
		st := newRow.StartsAt
		ch.occ = &st
	}
	return ch, nil
}

// inst2patch returns p for the new series of a split: content not in the
// patch comes from the occurrence being split at.
func inst2patch(inst eventRow, p patch) patch {
	q := p
	if q.title == nil {
		q.title = &inst.Title
	}
	if q.description == nil {
		q.description = &inst.Description
	}
	if q.location == nil {
		q.location = &inst.Location
	}
	return q
}

// ---- delete ---------------------------------------------------------------------

func (s *Service) deleteHandler(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	eid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	occ, err := parseOccurrence(r)
	if err != nil {
		return err
	}
	q := r.URL.Query()
	reason, _ := cleanText(q.Get("reason"), 500)
	if err := s.deleteEvent(r.Context(), id, eid, q.Get("scope"), occ, reason, q.Get("send_updates") != "false"); err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

func (s *Service) deleteEvent(ctx context.Context, caller *auth.Identity, id uuid.UUID, scope string, occ *time.Time, reason string, sendUpdates bool) error {
	r, err := s.resolve(ctx, s.d.Pool, caller.UserID, id, occ)
	if err != nil {
		return err
	}
	if !r.acc.isOwner {
		return httpx.Forbidden("Only the organizer can delete this event. Decline it instead.")
	}
	var bookingID uuid.UUID
	if err := s.d.Pool.QueryRow(ctx, `SELECT id FROM calendar_bookings WHERE event_id = $1 AND status = 'confirmed'`, r.base.ID).Scan(&bookingID); err == nil {
		return s.cancelBooking(ctx, bookingID, "host", reason)
	}
	recurring := r.base.RRule != nil
	if !recurring {
		scope = ScopeAll
	} else if scope != ScopeThis && scope != ScopeFollowing && scope != ScopeAll {
		return errScope
	}
	if recurring && scope == ScopeFollowing && r.occ.Equal(r.base.StartsAt) {
		scope = ScopeAll
	}
	var users []uuid.UUID
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT 1 FROM calendar_events WHERE id = $1 FOR UPDATE`, r.base.ID); err != nil {
			return err
		}
		rr, err := s.resolve(ctx, tx, caller.UserID, r.base.ID, r.occ)
		if err != nil {
			return err
		}
		attendees, err := loadFullAttendees(ctx, tx, rr.base.ID)
		if err != nil {
			return err
		}
		users = platformIDs(attendees)
		switch scope {
		case ScopeAll:
			exceptions, err := loadExceptions(ctx, tx, rr.base.ID)
			if err != nil {
				return err
			}
			if sendUpdates {
				if err := s.notifyAttendees(ctx, tx, rr.base, exceptions, attendees, inviteCancelled); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(ctx, `DELETE FROM calendar_events WHERE id = $1`, rr.base.ID); err != nil {
				return err
			}
			for _, e := range append(exceptions, rr.base) {
				if err := enqueueConferenceCancel(ctx, tx, e.ConferenceID); err != nil {
					return err
				}
			}
			return nil
		case ScopeThis:
			inst := rr.instance()
			if rr.exception != nil {
				inst.Status = "cancelled"
				if err := saveEvent(ctx, tx, inst); err != nil {
					return err
				}
			} else {
				inst.ID = uuid.New()
				inst.RecurringEventID, inst.OriginalStart = &rr.base.ID, rr.occ
				inst.RRule, inst.SeriesUntil, inst.Status = nil, nil, "cancelled"
				if err := insertEvent(ctx, tx, inst); err != nil {
					return err
				}
			}
			if ptrStr(inst.ConferenceID) != ptrStr(rr.base.ConferenceID) {
				if err := enqueueConferenceCancel(ctx, tx, inst.ConferenceID); err != nil {
					return err
				}
			}
		default: // following
			rule, _ := rr.base.rule()
			until := rr.occ.Add(-time.Second)
			rule.Count, rule.Until = 0, &until
			row := rr.base
			str := rule.String(row.AllDay, row.loc())
			row.RRule, row.SeriesUntil = &str, seriesUntil(rule, row.StartsAt, row.loc(), row.duration())
			if err := saveEvent(ctx, tx, row); err != nil {
				return err
			}
			rows, err := tx.Query(ctx, `DELETE FROM calendar_events WHERE recurring_event_id = $1 AND original_start >= $2 RETURNING conference_id`, row.ID, *rr.occ)
			if err != nil {
				return err
			}
			ids, err := pgx.CollectRows(rows, pgx.RowTo[*string])
			if err != nil {
				return err
			}
			for _, cid := range ids {
				if err := enqueueConferenceCancel(ctx, tx, cid); err != nil {
					return err
				}
			}
		}
		if err := bumpSequence(ctx, tx, rr.base.ID); err != nil {
			return err
		}
		if sendUpdates {
			return s.tellSeries(ctx, tx, rr.base.ID, inviteUpdated)
		}
		return nil
	})
	if err != nil {
		return err
	}
	kickOutbox()
	s.changed(ctx, "events", append(users, caller.UserID)...)
	return nil
}

// ---- RSVP -------------------------------------------------------------------------

func (s *Service) rsvpHandler(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	eid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var in struct {
		Response string `json:"response"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if in.Response != ResponseAccepted && in.Response != ResponseTentative && in.Response != ResponseDeclined {
		return httpx.BadRequest("response must be accepted, tentative or declined.")
	}
	res, err := s.resolve(r.Context(), s.d.Pool, id.UserID, eid, nil)
	if err != nil {
		return err
	}
	if res.acc.attendee == nil {
		return httpx.BadRequest("You're the organizer of this event.")
	}
	if err := s.setResponse(r.Context(), res.base, res.acc.attendee.ID, res.acc.attendee.Response, in.Response, id.DisplayName); err != nil {
		return err
	}
	s.changed(r.Context(), "events", id.UserID, res.base.OwnerID)
	res, err = s.resolve(r.Context(), s.d.Pool, id.UserID, eid, nil)
	if err != nil {
		return err
	}
	d, err := s.detail(r.Context(), s.d.Pool, id.UserID, res)
	if err != nil {
		return err
	}
	httpx.OK(w, d)
	return nil
}

var responseVerb = map[string]string{ResponseAccepted: "accepted", ResponseTentative: "might attend", ResponseDeclined: "declined"}

// setResponse records an attendee's reply (series-wide) and tells the
// organizer when it changed.
func (s *Service) setResponse(ctx context.Context, base eventRow, attendeeID uuid.UUID, prev, next, who string) error {
	return db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE calendar_attendees SET response = $2, responded_at = now() WHERE id = $1`, attendeeID, next); err != nil {
			return err
		}
		if prev == next {
			return nil
		}
		if who == "" {
			who = "A guest"
		}
		return enqueueNotify(ctx, tx, notify.Notification{
			UserID: base.OwnerID, Kind: NotifyResponse, Title: who + " " + responseVerb[next] + " " + base.Title,
			Body: s.whenFor(ctx, tx, base.OwnerID, base.StartsAt, base.EndsAt, base.AllDay, base.loc()),
			Link: eventLink(base.ID), Data: map[string]any{"event_id": base.ID},
		})
	})
}

// ---- download -----------------------------------------------------------------------

func (s *Service) eventICS(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	eid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	res, err := s.resolve(r.Context(), s.d.Pool, id.UserID, eid, nil)
	if err != nil {
		return err
	}
	var exceptions []eventRow
	if res.base.RRule != nil {
		if exceptions, err = loadExceptions(r.Context(), s.d.Pool, res.base.ID); err != nil {
			return err
		}
	}
	org, err := ownerContact(r.Context(), s.d.Pool, res.base.OwnerID)
	if err != nil {
		return err
	}
	body := renderICS("PUBLISH", res.base.Title, seriesVEvents(res.base, exceptions, &org, nil))
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="event.ics"`)
	w.Header().Set("Cache-Control", "private, no-store")
	_, _ = w.Write(body)
	return nil
}

// ---- conferencing helpers --------------------------------------------------------------

// createConference asks the provider for a room for e. Externals among
// the guests make the room open to guests; otherwise it's internal.
func (s *Service) createConference(ctx context.Context, e eventRow, want []resolvedAttendee, have []fullAttendee) (*Conference, error) {
	c := conferencingProvider()
	if c == nil {
		return nil, httpx.NewError(http.StatusConflict, "video_unavailable", "Video calls aren't set up in this app.")
	}
	req := ConferenceRequest{EventID: e.ID, HostID: e.OwnerID, Title: e.Title, StartsAt: e.StartsAt, EndsAt: e.EndsAt, Audience: AudienceInternal}
	for _, a := range want {
		if a.userID == nil {
			req.Audience = AudienceExternal
		} else {
			req.ParticipantIDs = append(req.ParticipantIDs, *a.userID)
		}
	}
	for _, a := range have {
		if a.UserID == nil {
			req.Audience = AudienceExternal
		} else {
			req.ParticipantIDs = append(req.ParticipantIDs, *a.UserID)
		}
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	conf, err := c.Create(cctx, req)
	if err != nil {
		slog.WarnContext(ctx, "calendar: conference create failed", "err", err)
		return nil, httpx.NewError(http.StatusBadGateway, "video_failed", "Couldn't create the video call. Try again.")
	}
	return &conf, nil
}

func (s *Service) cancelConferenceNow(ctx context.Context, id string) {
	if c := conferencingProvider(); c != nil {
		cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if err := c.Cancel(cctx, id); err != nil {
			slog.WarnContext(ctx, "calendar: conference cancel failed", "err", err)
		}
	}
}

// syncConference tells the provider about a new time or title (best-effort).
func (s *Service) syncConference(ctx context.Context, e eventRow) {
	c := conferencingProvider()
	if c == nil || e.ConferenceID == nil {
		return
	}
	atts, _ := loadFullAttendees(ctx, s.d.Pool, e.ID)
	req := ConferenceRequest{EventID: e.ID, HostID: e.OwnerID, Title: e.Title, StartsAt: e.StartsAt, EndsAt: e.EndsAt, Audience: AudienceInternal}
	for _, a := range atts {
		if a.UserID == nil {
			req.Audience = AudienceExternal
		} else {
			req.ParticipantIDs = append(req.ParticipantIDs, *a.UserID)
		}
	}
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if _, err := c.Update(cctx, *e.ConferenceID, req); err != nil {
		slog.WarnContext(ctx, "calendar: conference update failed", "err", err)
	}
}

func isExclusionViolation(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23P01"
}
