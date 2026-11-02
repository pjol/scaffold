package calendar

import (
	"context"
	"encoding/json"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
)

// Agent tools for connected calendars. Reads go through the same routes'
// code and drop what the connection's agent scope hides; writes are
// separate tools per platform with Source set (the assistant asks before
// each call and frames the output as third-party), Target kind
// calendar.external, an exact preview in the person's zone, and the
// connection's scope checked in Preview (suggest) and Handler (edit).

// connectedTools are the connected-calendar tools (read and write).
func (s *Service) connectedTools() []app.Tool {
	tools := []app.Tool{
		{
			Name:        "calendar_list_sources",
			Description: "List the user's connected calendar accounts (Google Calendar, Outlook) with their calendars: ids, names, whether events can be added, and the meeting links each can make. Use the ids with calendar_list_events (source) and the connected write tools.",
			InputSchema: json.RawMessage(`{"type": "object", "properties": {}, "additionalProperties": false}`),
			ReadOnly:    true,
			Handler:     s.toolListSources,
		},
		{
			Name:        "calendar_get_connected_event",
			Description: "Get one event from a connected calendar (Google Calendar, Outlook): notes, guests and their replies, meeting link. Ids come from calendar_list_events.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"connection_id": {"type": "string"},
					"calendar_id": {"type": "string"},
					"event_id": {"type": "string"}
				},
				"required": ["connection_id", "calendar_id", "event_id"],
				"additionalProperties": false
			}`),
			ReadOnly: true,
			Target:   externalTargetOf,
			Handler: func(ctx context.Context, caller *auth.Identity, raw json.RawMessage) (any, error) {
				var in connToolRef
				if err := decodeTool(raw, &in); err != nil {
					return nil, err
				}
				return s.connectedEvent(ctx, caller, uuid.Nil, in.ConnectionID, in.CalendarID, in.EventID)
			},
		},
	}
	for _, p := range []struct{ platform, prefix, product string }{
		{platformGoogle, "calendar_google_", "Google Calendar"},
		{platformMicrosoft, "calendar_outlook_", "Outlook Calendar"},
	} {
		p := p
		tools = append(tools,
			app.Tool{
				Name:        p.prefix + "create_event",
				Description: "Create an event on one of the user's connected " + p.product + " calendars (ids from calendar_list_sources). Guests may only be colleagues (by user id); " + p.product + " doesn't email them for this call. conference_provider adds a meeting link the calendar offers.",
				InputSchema: json.RawMessage(connCreateSchema),
				Source:      p.product,
				Target:      externalTargetOf,
				Preview: func(ctx context.Context, caller *auth.Identity, raw json.RawMessage) (app.ToolPreview, error) {
					return s.previewConnectedCreate(ctx, caller, p.platform, raw)
				},
				Handler: func(ctx context.Context, caller *auth.Identity, raw json.RawMessage) (any, error) {
					var in connToolCreate
					if err := decodeTool(raw, &in); err != nil {
						return nil, err
					}
					if err := s.toolPlatform(ctx, caller, in.ConnectionID, p.platform, app.AgentEdit, in.CalendarID, ""); err != nil {
						return nil, err
					}
					return s.createConnected(ctx, caller, uuid.Nil, in.ConnectionID, in.CalendarID, in.input(), false)
				},
			},
			app.Tool{
				Name:        p.prefix + "move_event",
				Description: "Move or resize an event the user organizes on a connected " + p.product + " calendar. scope \"all\" moves its whole series by the same amount.",
				InputSchema: json.RawMessage(connMoveSchema),
				Source:      p.product,
				Target:      externalTargetOf,
				Preview: func(ctx context.Context, caller *auth.Identity, raw json.RawMessage) (app.ToolPreview, error) {
					return s.previewConnectedMove(ctx, caller, p.platform, raw)
				},
				Handler: func(ctx context.Context, caller *auth.Identity, raw json.RawMessage) (any, error) {
					var in connToolMove
					if err := decodeTool(raw, &in); err != nil {
						return nil, err
					}
					if err := s.toolPlatform(ctx, caller, in.ConnectionID, p.platform, app.AgentEdit, in.CalendarID, in.EventID); err != nil {
						return nil, err
					}
					return s.updateConnected(ctx, caller, uuid.Nil, in.ConnectionID, in.CalendarID, in.EventID, in.input(), false)
				},
			},
			app.Tool{
				Name:        p.prefix + "rsvp",
				Description: "Reply to an invitation on a connected " + p.product + " calendar: accepted, tentative or declined. " + p.product + " tells the organizer.",
				InputSchema: json.RawMessage(connRSVPSchema),
				Source:      p.product,
				Target:      externalTargetOf,
				Preview: func(ctx context.Context, caller *auth.Identity, raw json.RawMessage) (app.ToolPreview, error) {
					return s.previewConnectedRSVP(ctx, caller, p.platform, raw)
				},
				Handler: func(ctx context.Context, caller *auth.Identity, raw json.RawMessage) (any, error) {
					var in connToolRSVP
					if err := decodeTool(raw, &in); err != nil {
						return nil, err
					}
					if err := s.toolPlatform(ctx, caller, in.ConnectionID, p.platform, app.AgentEdit, in.CalendarID, in.EventID); err != nil {
						return nil, err
					}
					return s.respondConnected(ctx, caller, uuid.Nil, in.ConnectionID, in.CalendarID, in.EventID, in.Response)
				},
			},
		)
	}
	return tools
}

const connCreateSchema = `{
	"type": "object",
	"properties": {
		"connection_id": {"type": "string"},
		"calendar_id": {"type": "string"},
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
		"conference_provider": {"type": "string", "enum": ["app", "google_meet", "teams", "zoom"]}
	},
	"required": ["connection_id", "calendar_id", "title"],
	"additionalProperties": false
}`

const connMoveSchema = `{
	"type": "object",
	"properties": {
		"connection_id": {"type": "string"},
		"calendar_id": {"type": "string"},
		"event_id": {"type": "string"},
		"starts_at": {"type": "string", "format": "date-time"},
		"ends_at": {"type": "string", "format": "date-time"},
		"all_day": {"type": "boolean"},
		"start_date": {"type": "string"},
		"end_date": {"type": "string"},
		"timezone": {"type": "string"},
		"scope": {"type": "string", "enum": ["this", "all"]}
	},
	"required": ["connection_id", "calendar_id", "event_id"],
	"additionalProperties": false
}`

const connRSVPSchema = `{
	"type": "object",
	"properties": {
		"connection_id": {"type": "string"},
		"calendar_id": {"type": "string"},
		"event_id": {"type": "string"},
		"response": {"type": "string", "enum": ["accepted", "tentative", "declined"]}
	},
	"required": ["connection_id", "calendar_id", "event_id", "response"],
	"additionalProperties": false
}`

type connToolRef struct {
	ConnectionID string `json:"connection_id"`
	CalendarID   string `json:"calendar_id"`
	EventID      string `json:"event_id"`
}

type connToolCreate struct {
	connToolRef
	Title              string      `json:"title"`
	StartsAt           *time.Time  `json:"starts_at"`
	EndsAt             *time.Time  `json:"ends_at"`
	AllDay             *bool       `json:"all_day"`
	StartDate          *string     `json:"start_date"`
	EndDate            *string     `json:"end_date"`
	Timezone           *string     `json:"timezone"`
	Description        *string     `json:"description"`
	Location           *string     `json:"location"`
	AttendeeIDs        []uuid.UUID `json:"attendee_user_ids"`
	ConferenceProvider string      `json:"conference_provider"`
}

func (in connToolCreate) input() connectedInput {
	out := connectedInput{Title: &in.Title, StartsAt: in.StartsAt, EndsAt: in.EndsAt, AllDay: in.AllDay, StartDate: in.StartDate,
		EndDate: in.EndDate, Timezone: in.Timezone, Description: in.Description, Location: in.Location}
	if len(in.AttendeeIDs) > 0 {
		list := make([]attendeeInput, 0, len(in.AttendeeIDs))
		for _, id := range in.AttendeeIDs {
			list = append(list, attendeeInput{UserID: &id})
		}
		out.Attendees = &list
	}
	if in.ConferenceProvider != "" {
		add := "add"
		out.Conference, out.ConferenceProvider = &add, in.ConferenceProvider
	}
	return out
}

type connToolMove struct {
	connToolRef
	StartsAt  *time.Time `json:"starts_at"`
	EndsAt    *time.Time `json:"ends_at"`
	AllDay    *bool      `json:"all_day"`
	StartDate *string    `json:"start_date"`
	EndDate   *string    `json:"end_date"`
	Timezone  *string    `json:"timezone"`
	Scope     string     `json:"scope"`
}

func (in connToolMove) input() connectedInput {
	return connectedInput{StartsAt: in.StartsAt, EndsAt: in.EndsAt, AllDay: in.AllDay, StartDate: in.StartDate, EndDate: in.EndDate,
		Timezone: in.Timezone, Scope: in.Scope}
}

type connToolRSVP struct {
	connToolRef
	Response string `json:"response"`
}

// externalTargetOf names the connected calendar or event a call acts on
// (the kind alone for an empty input).
func externalTargetOf(raw json.RawMessage) (string, string) {
	var in connToolRef
	_ = json.Unmarshal(raw, &in)
	if in.ConnectionID == "" || in.CalendarID == "" {
		return KindExternal, ""
	}
	return KindExternal, externalTarget(in.ConnectionID, in.CalendarID, in.EventID)
}

// toolPlatform checks a connection belongs to the tool's platform and that
// the connection's agent scope reaches need for the calendar or event.
func (s *Service) toolPlatform(ctx context.Context, caller *auth.Identity, connID, platform string, need app.AgentLevel, calID, eventID string) error {
	if connID == "" || calID == "" {
		return httpx.BadRequest("Pass connection_id and calendar_id (from calendar_list_sources).")
	}
	conns, err := s.connectedFor(ctx, caller.UserID, uuid.Nil)
	if err != nil {
		return err
	}
	for _, c := range conns {
		if c.ID == connID {
			if c.Provider != platform {
				return httpx.BadRequest("That connection is " + productLabel(c.Provider) + ": use its own tools.")
			}
			return s.agentAllows(ctx, caller, connID, calID, eventID, need)
		}
	}
	return errConnectionGone
}

func (s *Service) toolListSources(ctx context.Context, caller *auth.Identity, _ json.RawMessage) (any, error) {
	out, err := s.sourcesFor(ctx, caller.UserID, uuid.Nil)
	if err != nil {
		return nil, err
	}
	policy := app.FindAgentPolicy(s.d.Modules)
	if _, agent := auth.AgentFrom(ctx); !agent || policy == nil {
		return out, nil
	}
	keep := out.Sources[:0]
	for _, src := range out.Sources {
		if src.Type == "connected" {
			_, top, err := policy.FeatureLevel(ctx, caller, app.ConnectionScope(src.ID))
			if err != nil {
				return nil, err
			}
			if top < app.AgentRead {
				continue
			}
		}
		keep = append(keep, src)
	}
	out.Sources = keep
	return out, nil
}

// connectedWhen words an event's time for a reviewer.
func connectedWhen(start, end time.Time, allDay bool, loc *time.Location) string {
	if allDay {
		return formatWhen(start, end, true, time.UTC)
	}
	return formatWhen(start, end, false, loc)
}

func (s *Service) previewConnectedCreate(ctx context.Context, caller *auth.Identity, platform string, raw json.RawMessage) (app.ToolPreview, error) {
	var in connToolCreate
	if err := decodeTool(raw, &in); err != nil {
		return app.ToolPreview{}, err
	}
	if err := s.toolPlatform(ctx, caller, in.ConnectionID, platform, app.AgentSuggest, in.CalendarID, ""); err != nil {
		return app.ToolPreview{}, err
	}
	sess, err := s.session(ctx, caller.UserID, uuid.Nil, in.ConnectionID, true)
	if err != nil {
		return app.ToolPreview{}, err
	}
	cal, listed := s.connectedCalendar(ctx, caller.UserID, uuid.Nil, sess.conn, in.CalendarID)
	if !listed || !cal.CanWrite {
		return app.ToolPreview{}, httpx.BadRequest("Pick a calendar the account can add events to (calendar_list_sources).")
	}
	st, err := s.loadSettings(ctx, s.d.Pool, caller.UserID)
	if err != nil {
		return app.ToolPreview{}, err
	}
	w, _, err := s.connectedWrite(ctx, caller.UserID, in.input(), true, st.Timezone, false)
	if err != nil {
		return app.ToolPreview{}, err
	}
	loc := s.zoneFor(ctx, s.d.Pool, caller.UserID, zoneOr(w.Timezone, time.UTC))
	where := sess.conn.label()
	if cal.Name != "" && cal.Name != sess.conn.Account {
		where += " (" + cal.Name + ")"
	}
	when := connectedWhen(w.Start, w.End, w.AllDay, loc)
	guests, err := s.previewGuests(ctx, caller.UserID, in.AttendeeIDs)
	if err != nil {
		return app.ToolPreview{}, err
	}
	lines := []string{strings.TrimSpace(in.Title), "When: " + when, "Calendar: " + where}
	if in.Location != nil && strings.TrimSpace(*in.Location) != "" {
		lines = append(lines, "Where: "+strings.TrimSpace(*in.Location))
	}
	if in.ConferenceProvider != "" {
		lines = append(lines, "Video call: "+providerName(in.ConferenceProvider))
	}
	if len(guests) > 0 {
		lines = append(lines, "Guests (not emailed): "+strings.Join(guests, ", "))
	}
	after := strings.Join(lines, "\n")
	if in.Description != nil && strings.TrimSpace(*in.Description) != "" {
		after += "\n\n" + strings.TrimSpace(*in.Description)
	}
	return app.ToolPreview{Summary: "Add “" + strings.TrimSpace(in.Title) + "” " + when + " to " + where, After: after, Link: "/calendar"}, nil
}

func (s *Service) previewConnectedMove(ctx context.Context, caller *auth.Identity, platform string, raw json.RawMessage) (app.ToolPreview, error) {
	var in connToolMove
	if err := decodeTool(raw, &in); err != nil {
		return app.ToolPreview{}, err
	}
	if err := s.toolPlatform(ctx, caller, in.ConnectionID, platform, app.AgentSuggest, in.CalendarID, in.EventID); err != nil {
		return app.ToolPreview{}, err
	}
	sess, err := s.session(ctx, caller.UserID, uuid.Nil, in.ConnectionID, true)
	if err != nil {
		return app.ToolPreview{}, err
	}
	cur, err := sess.ad.event(ctx, in.CalendarID, in.EventID)
	if err != nil {
		return app.ToolPreview{}, s.connError(sess.conn, sess.conn.writeCap, err)
	}
	if !cur.CanEdit {
		return app.ToolPreview{}, httpx.Forbidden("Only the organizer can move this event.")
	}
	w, _, err := s.connectedWrite(ctx, caller.UserID, in.input(), false, cur.Timezone, false)
	if err != nil {
		return app.ToolPreview{}, err
	}
	if !w.TimeSet {
		return app.ToolPreview{}, httpx.BadRequest("Pass the new starts_at and ends_at (or all-day dates).")
	}
	loc := s.zoneFor(ctx, s.d.Pool, caller.UserID, zoneOr(cur.Timezone, time.UTC))
	before := connectedWhen(cur.Start, cur.End, cur.AllDay, loc)
	after := connectedWhen(w.Start, w.End, w.AllDay, loc)
	summary := "Move “" + cur.Title + "” to " + after + " in " + sess.conn.label()
	if in.Scope == ScopeAll && cur.SeriesID != "" {
		summary = "Move every event of “" + cur.Title + "” by the same amount as " + before + " → " + after + " in " + sess.conn.label()
	}
	if len(cur.Attendees) > 0 {
		summary += " (" + productLabel(sess.conn.Provider) + " won't email the guests)"
	}
	return app.ToolPreview{Summary: summary, Before: cur.Title + "\nWhen: " + before, After: cur.Title + "\nWhen: " + after, Link: "/calendar"}, nil
}

func (s *Service) previewConnectedRSVP(ctx context.Context, caller *auth.Identity, platform string, raw json.RawMessage) (app.ToolPreview, error) {
	var in connToolRSVP
	if err := decodeTool(raw, &in); err != nil {
		return app.ToolPreview{}, err
	}
	if err := s.toolPlatform(ctx, caller, in.ConnectionID, platform, app.AgentSuggest, in.CalendarID, in.EventID); err != nil {
		return app.ToolPreview{}, err
	}
	words := map[string]string{ResponseAccepted: "Going", ResponseTentative: "Maybe", ResponseDeclined: "Not going"}[in.Response]
	if words == "" {
		return app.ToolPreview{}, httpx.BadRequest("response must be accepted, tentative or declined.")
	}
	sess, err := s.session(ctx, caller.UserID, uuid.Nil, in.ConnectionID, true)
	if err != nil {
		return app.ToolPreview{}, err
	}
	cur, err := sess.ad.event(ctx, in.CalendarID, in.EventID)
	if err != nil {
		return app.ToolPreview{}, s.connError(sess.conn, sess.conn.writeCap, err)
	}
	loc := s.zoneFor(ctx, s.d.Pool, caller.UserID, zoneOr(cur.Timezone, time.UTC))
	when := connectedWhen(cur.Start, cur.End, cur.AllDay, loc)
	prev := map[string]string{ResponseAccepted: "Going", ResponseTentative: "Maybe", ResponseDeclined: "Not going", ResponseNeedsAction: "No reply"}[cur.MyResponse]
	return app.ToolPreview{Summary: "Reply “" + words + "” to “" + cur.Title + "” " + when + " on " + sess.conn.label() + " (the organizer is told)",
		Before: "Your reply: " + prev, After: "Your reply: " + words, Link: "/calendar"}, nil
}

// providerName is what people call a meeting provider.
func providerName(id string) string {
	switch id {
	case ProviderGoogleMeet:
		return "Google Meet"
	case ProviderTeams:
		return "Microsoft Teams"
	case ProviderZoom:
		return "Zoom"
	}
	return "Video call"
}

// ---- search --------------------------------------------------------------------

// connectedSearch finds the caller's connected events by title or place in a
// window around today, each connection time-boxed, as the caller (agents:
// the connection's scope). Hits link to the day in the calendar.
func (s *Service) connectedSearch(ctx context.Context, caller *auth.Identity, q string, limit int) []app.SearchResult {
	if s.connections() == nil {
		return nil
	}
	now := time.Now().UTC().Truncate(time.Hour)
	from, to := now.AddDate(0, 0, -30), now.AddDate(0, 0, 60)
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	res, err := s.connectedRange(cctx, caller, uuid.Nil, from, to, "")
	if err != nil {
		return nil
	}
	conns := map[string]*calConn{}
	if list, err := s.connectedFor(ctx, caller.UserID, uuid.Nil); err == nil {
		for _, c := range list {
			conns[c.ID] = c
		}
	}
	zone := s.zoneFor(ctx, s.d.Pool, caller.UserID, time.UTC)
	needle := strings.ToLower(q)
	// Upcoming first (soonest), then past (latest), so a series' hit is its next one.
	sort.SliceStable(res.Items, func(a, b int) bool {
		ua, ub := !res.Items[a].EndsAt.Before(now), !res.Items[b].EndsAt.Before(now)
		if ua != ub {
			return ua
		}
		if ua {
			return res.Items[a].StartsAt.Before(res.Items[b].StartsAt)
		}
		return res.Items[a].StartsAt.After(res.Items[b].StartsAt)
	})
	var out []app.SearchResult
	seen := map[string]bool{}
	for _, e := range res.Items {
		title, place := strings.ToLower(e.Title), strings.ToLower(e.Location)
		if !strings.Contains(title, needle) && !strings.Contains(place, needle) {
			continue
		}
		key := e.ConnectionID + "|" + e.SeriesID
		if e.SeriesID != "" && seen[key] {
			continue // one hit per series
		}
		seen[key] = true
		score := 0.5
		switch {
		case strings.HasPrefix(title, needle):
			score = 0.85
		case strings.Contains(title, needle):
			score = 0.7
		}
		snippet := formatWhen(e.StartsAt, e.EndsAt, e.AllDay, zone)
		if e.Location != "" {
			snippet += " · " + e.Location
		}
		label := e.CalendarName
		if c := conns[e.ConnectionID]; c != nil {
			label = c.label()
		}
		date := e.StartsAt.In(zone).Format(time.DateOnly)
		if e.StartDate != nil {
			date = *e.StartDate
		}
		out = append(out, app.SearchResult{Kind: KindExternal, ID: e.EventID, Title: e.Title, Snippet: snippet,
			Link: "/calendar?view=day&date=" + date + "&open=" + url.QueryEscape(e.EventID), Icon: "calendar", Score: score, At: e.StartsAt,
			Context: app.SearchContext(label), Source: e.Source})
		if len(out) >= limit {
			break
		}
	}
	return out
}
