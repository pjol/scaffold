package calendar

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
)

// Tools exposes calendar tools to agents, run as the caller with the same
// checks as the REST routes: only the caller's calendar, colleagues'
// free/busy only, and one write that never emails anyone outside the app.
func (s *Service) Tools() []app.Tool {
	return append([]app.Tool{
		{
			Name:        "calendar_list_events",
			Description: "List events on the user's calendar between two times (at most 31 days apart): their own events and invitations with their reply, recurring events expanded, busy blocks from calendars they subscribed to, and (under connected) events from their connected Google and Outlook calendars, each with its source.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"from": {"type": "string", "format": "date-time", "description": "Start of the window (RFC 3339)."},
					"to": {"type": "string", "format": "date-time", "description": "End of the window (RFC 3339)."},
					"source": {"type": "string", "description": "all (default), app (this app's calendar only), or a connection id from calendar_list_sources."}
				},
				"required": ["from", "to"],
				"additionalProperties": false
			}`),
			ReadOnly: true,
			Handler:  s.toolListEvents,
		},
		{
			Name:        "calendar_get_event",
			Description: "Get one event the user owns or is invited to: description, guests and their replies, repeat rule. Pass occurrence_start for one occurrence of a recurring event.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"event_id": {"type": "string", "format": "uuid"},
					"occurrence_start": {"type": "string", "format": "date-time"}
				},
				"required": ["event_id"],
				"additionalProperties": false
			}`),
			ReadOnly: true,
			Handler:  s.toolGetEvent,
			Target:   eventTarget,
		},
		{
			Name:        "calendar_find_free_time",
			Description: "Find times in the user's working hours when they (and optionally colleagues from their " + orgs.Nouns() + ") are all free. Colleagues are given by user id from calendar_search_people.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"duration_minutes": {"type": "integer", "minimum": 5, "maximum": 480},
					"from": {"type": "string", "format": "date-time"},
					"to": {"type": "string", "format": "date-time", "description": "At most 31 days after from."},
					"user_ids": {"type": "array", "items": {"type": "string", "format": "uuid"}, "maxItems": 20}
				},
				"required": ["duration_minutes", "from", "to"],
				"additionalProperties": false
			}`),
			ReadOnly: true,
			Handler:  s.toolFindTime,
		},
		{
			Name:        "calendar_search_people",
			Description: "Search people who share " + orgs.NounA() + " with the user, by name. Returns user ids for free/busy and invitations.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {"query": {"type": "string", "maxLength": 100}},
				"required": ["query"],
				"additionalProperties": false
			}`),
			ReadOnly: true,
			Handler:  s.toolSearchPeople,
		},
		{
			Name:        "calendar_list_booking_links",
			Description: "List the user's booking links with their public URL, audience (internal or external), lengths and whether they're active.",
			InputSchema: json.RawMessage(`{"type": "object", "properties": {}, "additionalProperties": false}`),
			ReadOnly:    true,
			Handler:     s.toolListLinks,
		},
		{
			Name:        "calendar_list_bookings",
			Description: "List bookings made through the user's booking links (role host, default) or that the user made on someone else's (role booker).",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"when": {"type": "string", "enum": ["upcoming", "past", "cancelled"], "description": "Default upcoming."},
					"role": {"type": "string", "enum": ["host", "booker"]},
					"cursor": {"type": "string"}
				},
				"additionalProperties": false
			}`),
			ReadOnly: true,
			Handler:  s.toolListBookings,
		},
		{
			Name:        "calendar_create_event",
			Description: "Create an event on the user's own calendar. Guests may only be colleagues from the user's " + orgs.Nouns() + " (by user id); they get an in-app invitation. Nobody outside the app is emailed.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"title": {"type": "string", "maxLength": 200},
					"starts_at": {"type": "string", "format": "date-time"},
					"ends_at": {"type": "string", "format": "date-time"},
					"all_day": {"type": "boolean"},
					"start_date": {"type": "string", "description": "All-day: first day, YYYY-MM-DD."},
					"end_date": {"type": "string", "description": "All-day: last day (inclusive), YYYY-MM-DD."},
					"timezone": {"type": "string", "description": "IANA zone; default the user's calendar zone."},
					"description": {"type": "string", "maxLength": 8000},
					"location": {"type": "string", "maxLength": 500},
					"attendee_user_ids": {"type": "array", "items": {"type": "string", "format": "uuid"}, "maxItems": 50},
					"recurrence": {
						"type": "object",
						"properties": {
							"freq": {"type": "string", "enum": ["daily", "weekly", "monthly", "yearly"]},
							"interval": {"type": "integer", "minimum": 1, "maximum": 99},
							"by_day": {"type": "array", "items": {"type": "string"}},
							"by_month_day": {"type": "array", "items": {"type": "integer"}},
							"count": {"type": "integer", "minimum": 1, "maximum": 999},
							"until": {"type": "string", "description": "YYYY-MM-DD"}
						},
						"required": ["freq"],
						"additionalProperties": false
					}
				},
				"required": ["title"],
				"additionalProperties": false
			}`),
			ReadOnly: false,
			Handler:  s.toolCreateEvent,
			// A new event has no id yet: the kind alone (the feature's level
			// decides).
			Target:  func(json.RawMessage) (string, string) { return KindEvent, "" },
			Preview: s.previewCreate,
		},
	}, s.connectedTools()...)
}

func decodeTool(raw json.RawMessage, dst any) error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return httpx.BadRequest("Invalid input: " + err.Error())
	}
	return nil
}

func (s *Service) toolListEvents(ctx context.Context, caller *auth.Identity, raw json.RawMessage) (any, error) {
	var in struct {
		From   string `json:"from"`
		To     string `json:"to"`
		Source string `json:"source"`
	}
	if err := decodeTool(raw, &in); err != nil {
		return nil, err
	}
	from, to, err := parseWindow(in.From, in.To, 31*24*time.Hour)
	if err != nil {
		return nil, err
	}
	readable, err := s.agentReadable(ctx, caller)
	if err != nil {
		return nil, err
	}
	type withConnected struct {
		RangeResponse
		Connected       []ConnectedEvent `json:"connected"`
		ConnectedErrors []SourceError    `json:"connected_errors,omitempty"`
	}
	res := withConnected{Connected: []ConnectedEvent{}}
	if in.Source != "" && in.Source != "all" && in.Source != "app" {
		c, err := s.connectedRange(ctx, caller, uuid.Nil, from, to, in.Source)
		if err != nil {
			return nil, err
		}
		res.RangeResponse = RangeResponse{Items: []Occurrence{}, Busy: []ExternalBusy{}}
		res.Connected, res.ConnectedErrors = c.Items, c.Errors
		return res, nil
	}
	if in.Source != "app" {
		c, err := s.connectedRange(ctx, caller, uuid.Nil, from, to, "")
		if err != nil {
			return nil, err
		}
		res.Connected, res.ConnectedErrors = c.Items, c.Errors
	}
	out, err := s.listRange(ctx, caller.UserID, from, to)
	if err != nil {
		return nil, err
	}
	items := out.Items[:0]
	for _, o := range out.Items {
		if readable(KindEvent, o.EventID) {
			items = append(items, o)
		}
	}
	busy := out.Busy[:0]
	for _, b := range out.Busy {
		if readable(KindSubscription, b.SubscriptionID) {
			busy = append(busy, b)
		}
	}
	out.Items, out.Busy = items, busy
	res.RangeResponse = out
	return res, nil
}

func (s *Service) toolGetEvent(ctx context.Context, caller *auth.Identity, raw json.RawMessage) (any, error) {
	var in struct {
		EventID         string     `json:"event_id"`
		OccurrenceStart *time.Time `json:"occurrence_start"`
	}
	if err := decodeTool(raw, &in); err != nil {
		return nil, err
	}
	id, err := uuid.Parse(in.EventID)
	if err != nil {
		return nil, httpx.BadRequest("event_id must be a uuid.")
	}
	res, err := s.resolve(ctx, s.d.Pool, caller.UserID, id, in.OccurrenceStart)
	if err != nil {
		return nil, err
	}
	readable, err := s.agentReadable(ctx, caller)
	if err != nil {
		return nil, err
	}
	if !readable(KindEvent, res.base.ID) {
		return nil, errEventNotFound
	}
	return s.detail(ctx, s.d.Pool, caller.UserID, res)
}

func (s *Service) toolFindTime(ctx context.Context, caller *auth.Identity, raw json.RawMessage) (any, error) {
	var in struct {
		Duration int      `json:"duration_minutes"`
		From     string   `json:"from"`
		To       string   `json:"to"`
		UserIDs  []string `json:"user_ids"`
	}
	if err := decodeTool(raw, &in); err != nil {
		return nil, err
	}
	if in.Duration < 5 || in.Duration > 480 {
		return nil, httpx.BadRequest("duration_minutes must be 5 to 480.")
	}
	from, to, err := parseWindow(in.From, in.To, 31*24*time.Hour)
	if err != nil {
		return nil, err
	}
	ids, err := parseUserIDs(strings.Join(in.UserIDs, ","), 20)
	if err != nil {
		return nil, err
	}
	return s.findTime(ctx, caller.UserID, ids, from, to, time.Duration(in.Duration)*time.Minute, 30)
}

func (s *Service) toolSearchPeople(ctx context.Context, caller *auth.Identity, raw json.RawMessage) (any, error) {
	var in struct {
		Query string `json:"query"`
	}
	if err := decodeTool(raw, &in); err != nil {
		return nil, err
	}
	q, _ := cleanText(in.Query, 100)
	people, err := s.searchPeople(ctx, caller.UserID, q, 20)
	if err != nil {
		return nil, err
	}
	return map[string]any{"items": people}, nil
}

func (s *Service) toolListLinks(ctx context.Context, caller *auth.Identity, _ json.RawMessage) (any, error) {
	readable, err := s.agentReadable(ctx, caller)
	if err != nil {
		return nil, err
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT `+linkCols+linkFrom+`WHERE l.owner_id = $1 AND l.deleted_at IS NULL
		ORDER BY l.created_at DESC, l.id DESC LIMIT 50`, caller.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BookingLink{}
	for rows.Next() {
		l, err := s.scanLink(rows)
		if err != nil {
			return nil, err
		}
		if readable(KindBookingLink, l.ID) {
			out = append(out, l)
		}
	}
	return map[string]any{"items": out}, rows.Err()
}

func (s *Service) toolListBookings(ctx context.Context, caller *auth.Identity, raw json.RawMessage) (any, error) {
	var in struct {
		When   string `json:"when"`
		Role   string `json:"role"`
		Cursor string `json:"cursor"`
	}
	if err := decodeTool(raw, &in); err != nil {
		return nil, err
	}
	if in.When != "" && in.When != "upcoming" && in.When != "past" && in.When != "cancelled" {
		return nil, httpx.BadRequest("when must be upcoming, past or cancelled.")
	}
	if in.Role != "" && in.Role != "host" && in.Role != "booker" {
		return nil, httpx.BadRequest("role must be host or booker.")
	}
	readable, err := s.agentReadable(ctx, caller)
	if err != nil {
		return nil, err
	}
	page, err := s.listBookingsFor(ctx, caller.UserID, in.Role, in.When, 25, in.Cursor)
	if err != nil {
		return nil, err
	}
	// A booking is read through its link (host side) or its event.
	items := page.Items[:0]
	for _, b := range page.Items {
		switch {
		case b.Role == "host" && b.LinkID != nil && !readable(KindBookingLink, *b.LinkID):
		case b.EventID != nil && !readable(KindEvent, *b.EventID):
		default:
			items = append(items, b)
		}
	}
	page.Items = items
	return page, nil
}

func (s *Service) toolCreateEvent(ctx context.Context, caller *auth.Identity, raw json.RawMessage) (any, error) {
	if !caller.Active() {
		return nil, httpx.ErrSuspended
	}
	var in struct {
		Title       string          `json:"title"`
		StartsAt    *time.Time      `json:"starts_at"`
		EndsAt      *time.Time      `json:"ends_at"`
		AllDay      *bool           `json:"all_day"`
		StartDate   *string         `json:"start_date"`
		EndDate     *string         `json:"end_date"`
		Timezone    *string         `json:"timezone"`
		Description *string         `json:"description"`
		Location    *string         `json:"location"`
		AttendeeIDs []uuid.UUID     `json:"attendee_user_ids"`
		Recurrence  json.RawMessage `json:"recurrence"`
	}
	if err := decodeTool(raw, &in); err != nil {
		return nil, err
	}
	// The REST route allows 120 creates an hour; tools get the same budget.
	var recent int
	if err := s.d.Pool.QueryRow(ctx, `SELECT count(*) FROM calendar_events WHERE owner_id = $1 AND recurring_event_id IS NULL
		AND created_at > now() - interval '1 hour'`, caller.UserID).Scan(&recent); err != nil {
		return nil, err
	}
	if recent >= 120 {
		return nil, httpx.ErrRateLimited
	}
	ev := eventInput{
		Title: &in.Title, StartsAt: in.StartsAt, EndsAt: in.EndsAt, AllDay: in.AllDay, StartDate: in.StartDate,
		EndDate: in.EndDate, Timezone: in.Timezone, Description: in.Description, Location: in.Location, Recurrence: in.Recurrence,
	}
	if len(in.AttendeeIDs) > 0 {
		list := make([]attendeeInput, 0, len(in.AttendeeIDs))
		for _, id := range in.AttendeeIDs {
			list = append(list, attendeeInput{UserID: &id})
		}
		ev.Attendees = &list
	}
	return s.createEvent(ctx, caller, ev, false)
}

// Search finds the caller's events (owned or invited: title, place or
// description), booking links, and events on their connected calendars
// (live, time-boxed; Source and Context "Google Calendar · account") for
// global search. Events belong to no
// org (Context "Calendar"); members-only links name theirs (OrgID, Context
// "Org · Calendar"), and a search scoped to another org leaves them out.
func (s *Service) Search(ctx context.Context, caller *auth.Identity, query string, limit int) ([]app.SearchResult, error) {
	q, _ := cleanText(query, 100)
	if len([]rune(q)) < 2 {
		return nil, nil
	}
	readable, err := s.agentReadable(ctx, caller)
	if err != nil {
		return nil, err
	}
	like := likeEscape(q)
	zone := s.zoneFor(ctx, s.d.Pool, caller.UserID, time.UTC)
	rows, err := s.d.Pool.Query(ctx, `
		SELECT e.id, e.title, e.location, e.starts_at, e.ends_at, e.all_day, e.timezone, e.rrule, e.updated_at,
		       CASE WHEN e.title ILIKE $2 || '%' THEN 0.9 WHEN e.title ILIKE '%' || $2 || '%' THEN 0.75
		            WHEN e.location ILIKE '%' || $2 || '%' THEN 0.5 ELSE 0.4 END AS score
		FROM calendar_events e
		LEFT JOIN calendar_attendees a ON a.event_id = e.id AND a.user_id = $1
		WHERE e.recurring_event_id IS NULL AND e.status = 'confirmed' AND (e.owner_id = $1 OR a.id IS NOT NULL)
		  AND (e.title ILIKE '%' || $2 || '%' OR e.location ILIKE '%' || $2 || '%' OR e.description ILIKE '%' || $2 || '%')
		ORDER BY score DESC, e.starts_at DESC LIMIT $3`, caller.UserID, like, limit)
	if err != nil {
		return nil, err
	}
	out := []app.SearchResult{}
	for rows.Next() {
		var id uuid.UUID
		var title, location, tz string
		var start, end, updated time.Time
		var allDay bool
		var rrule *string
		var score float64
		if err := rows.Scan(&id, &title, &location, &start, &end, &allDay, &tz, &rrule, &updated, &score); err != nil {
			rows.Close()
			return nil, err
		}
		eventLoc := zoneOr(tz, time.UTC)
		snippet := formatWhen(start, end, allDay, zone)
		if allDay {
			snippet = formatWhen(start, end, true, eventLoc)
		}
		if rrule != nil {
			if r, err := ParseRule(*rrule, eventLoc); err == nil {
				snippet = describeRule(r, eventLoc) + " · from " + start.In(eventLoc).Format("Jan 2, 2006")
			}
		}
		if location != "" {
			snippet += " · " + location
		}
		if !readable(KindEvent, id) {
			continue
		}
		out = append(out, app.SearchResult{Kind: KindEvent, ID: id.String(), Title: title, Snippet: snippet,
			Link: eventLink(id), Icon: "calendar", Score: score, At: updated, Context: app.SearchContext("Calendar")})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var scope *uuid.UUID
	if id, ok := app.SearchOrg(ctx); ok {
		scope = &id
	}
	lrows, err := s.d.Pool.Query(ctx, `
		SELECT l.id, l.title, l.slug, l.updated_at, l.org_id, COALESCE(o.name, '') FROM calendar_booking_links l LEFT JOIN orgs o ON o.id = l.org_id
		WHERE l.owner_id = $1 AND l.deleted_at IS NULL AND (l.title ILIKE '%' || $2 || '%' OR l.slug ILIKE '%' || $2 || '%')
		  AND ($4::uuid IS NULL OR l.org_id IS NULL OR l.org_id = $4)
		ORDER BY l.updated_at DESC LIMIT $3`, caller.UserID, like, limit, scope)
	if err != nil {
		return nil, err
	}
	defer lrows.Close()
	for lrows.Next() {
		var id uuid.UUID
		var title, slug, orgName string
		var updated time.Time
		var orgID *uuid.UUID
		if err := lrows.Scan(&id, &title, &slug, &updated, &orgID, &orgName); err != nil {
			return nil, err
		}
		if !readable(KindBookingLink, id) {
			continue
		}
		hit := app.SearchResult{Kind: KindBookingLink, ID: id.String(), Title: title, Snippet: "/book/" + slug,
			Link: "/calendar/links/" + id.String(), Icon: "link", Score: 0.6, At: updated, Context: app.SearchContext("Calendar")}
		if orgID != nil {
			hit.OrgID, hit.Context = orgID.String(), app.SearchContext(orgName, "Calendar")
		}
		out = append(out, hit)
	}
	if err := lrows.Err(); err != nil {
		return nil, err
	}
	lrows.Close()
	return append(out, s.connectedSearch(ctx, caller, q, limit)...), nil
}

// agentReadable returns whether the request may read a calendar resource.
// Requests that aren't agent calls (and apps without an agent policy) read
// everything they could anyway; for agents it applies the user's agent
// scope: nothing when no level in the feature reaches read, else a check
// per resource (overrides can be lower than the feature's level).
func (s *Service) agentReadable(ctx context.Context, caller *auth.Identity) (func(kind string, id uuid.UUID) bool, error) {
	all := func(string, uuid.UUID) bool { return true }
	if _, ok := auth.AgentFrom(ctx); !ok {
		return all, nil
	}
	policy := app.FindAgentPolicy(s.d.Modules)
	if policy == nil {
		return all, nil
	}
	_, top, err := policy.FeatureLevel(ctx, caller, "calendar")
	if err != nil {
		return nil, err
	}
	// max is an upper bound only: a resource can still be set lower than
	// the feature, so every row is checked unless nothing is readable.
	if top < app.AgentRead {
		return func(string, uuid.UUID) bool { return false }, nil
	}
	cache := map[string]bool{}
	return func(kind string, id uuid.UUID) bool {
		key := kind + ":" + id.String()
		if v, ok := cache[key]; ok {
			return v
		}
		ok, err := app.AgentAllows(ctx, s.d.Modules, caller, "calendar", kind, id.String(), app.AgentRead)
		cache[key] = err == nil && ok
		return cache[key]
	}, nil
}

// Agent resource kinds.
const (
	KindEvent        = "calendar.event"
	KindBookingLink  = "calendar.booking_link"
	KindSubscription = "calendar.subscription"
)

func eventTarget(raw json.RawMessage) (string, string) {
	var in struct {
		EventID string `json:"event_id"`
	}
	_ = json.Unmarshal(raw, &in)
	return KindEvent, in.EventID
}

// createPreview is the part of a calendar_create_event call its preview reads.
type createPreview struct {
	Title       string      `json:"title"`
	StartsAt    *time.Time  `json:"starts_at"`
	EndsAt      *time.Time  `json:"ends_at"`
	AllDay      bool        `json:"all_day"`
	StartDate   string      `json:"start_date"`
	EndDate     string      `json:"end_date"`
	Timezone    string      `json:"timezone"`
	Description string      `json:"description"`
	Location    string      `json:"location"`
	AttendeeIDs []uuid.UUID `json:"attendee_user_ids"`
	Recurrence  *Recurrence `json:"recurrence"`
}

// previewCreate is what a user reviews when their agent's write becomes a
// suggestion, in the reviewer's own zone (else the event's): the line
// "Add “Design review” Mon, Nov 2, 2026 · 7:00 AM – 8:00 AM PST" and the
// whole event as it would be created, guests by name.
func (s *Service) previewCreate(ctx context.Context, caller *auth.Identity, raw json.RawMessage) (app.ToolPreview, error) {
	var in createPreview
	if err := decodeTool(raw, &in); err != nil {
		return app.ToolPreview{}, err
	}
	eventLoc := time.UTC
	if validZone(in.Timezone) {
		eventLoc = zoneOr(in.Timezone, time.UTC)
	}
	loc := s.zoneFor(ctx, s.d.Pool, caller.UserID, eventLoc)
	guests, err := s.previewGuests(ctx, caller.UserID, in.AttendeeIDs)
	if err != nil {
		return app.ToolPreview{}, err
	}
	return app.ToolPreview{Summary: createSummary(in, loc), After: createDetails(in, loc, eventLoc, guests), Link: "/calendar"}, nil
}

// previewGuests checks guests as the write will (colleagues only, the same
// refusal) and returns their names in input order.
func (s *Service) previewGuests(ctx context.Context, callerID uuid.UUID, ids []uuid.UUID) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	list := make([]attendeeInput, 0, len(ids))
	for _, id := range ids {
		list = append(list, attendeeInput{UserID: &id})
	}
	resolved, err := s.resolveAttendees(ctx, callerID, list, false)
	if err != nil {
		return nil, err
	}
	keep := make([]uuid.UUID, 0, len(resolved))
	for _, a := range resolved {
		if a.userID != nil {
			keep = append(keep, *a.userID)
		}
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT id, display_name FROM users WHERE id = ANY($1)`, keep)
	if err != nil {
		return nil, err
	}
	names := map[uuid.UUID]string{}
	for rows.Next() {
		var id uuid.UUID
		var n string
		if err := rows.Scan(&id, &n); err != nil {
			rows.Close()
			return nil, err
		}
		names[id] = n
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(keep))
	for _, id := range keep {
		out = append(out, names[id])
	}
	return out, nil
}

// createDetails is the event a create call would make, one fact a line,
// then the description.
func createDetails(in createPreview, loc, eventLoc *time.Location, guests []string) string {
	lines := []string{strings.TrimSpace(in.Title)}
	if when := createWhen(in, loc); when != "" {
		lines = append(lines, "When: "+when)
	}
	if in.Recurrence != nil {
		repeat := in.Recurrence.Freq
		start := time.Now()
		switch {
		case in.StartsAt != nil:
			start = *in.StartsAt
		case in.StartDate != "":
			if d, err := time.ParseInLocation(time.DateOnly, in.StartDate, eventLoc); err == nil {
				start = d
			}
		}
		if r, err := in.Recurrence.toRule(start, eventLoc); err == nil {
			repeat = describeRule(r, eventLoc)
		}
		lines = append(lines, "Repeats: "+repeat)
	}
	if l := strings.TrimSpace(in.Location); l != "" {
		lines = append(lines, "Where: "+l)
	}
	if len(guests) > 0 {
		lines = append(lines, "Guests: "+strings.Join(guests, ", "))
	}
	out := strings.Join(lines, "\n")
	if d := strings.TrimSpace(in.Description); d != "" {
		out += "\n\n" + d
	}
	return out
}

// createSummary words a create call with times in loc.
func createSummary(in createPreview, loc *time.Location) string {
	summary := "Add “" + strings.TrimSpace(in.Title) + "”"
	if when := createWhen(in, loc); when != "" {
		summary += " " + when
	}
	if in.Recurrence != nil {
		summary += ", repeating " + in.Recurrence.Freq
	}
	switch n := len(in.AttendeeIDs); {
	case n == 1:
		summary += " with 1 colleague"
	case n > 1:
		summary += " with " + strconv.Itoa(n) + " colleagues"
	}
	return summary
}

// createWhen is a create call's time in loc; all-day dates read as given.
func createWhen(in createPreview, loc *time.Location) string {
	switch {
	case in.AllDay && in.StartDate != "":
		if d, err := time.Parse(time.DateOnly, in.StartDate); err == nil {
			end := d
			if e, err := time.Parse(time.DateOnly, in.EndDate); err == nil && e.After(d) {
				end = e
			}
			return formatWhen(d, end.AddDate(0, 0, 1), true, time.UTC)
		}
	case in.StartsAt != nil && in.EndsAt != nil:
		return formatWhen(*in.StartsAt, *in.EndsAt, false, loc)
	}
	return ""
}
