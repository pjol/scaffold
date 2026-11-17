package calendar

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
)

// depthTools are 1.3.0's agent tools: calendars and rooms the user can
// see, booking a room on an event they may change, out of office and focus
// time on their own calendar, and proposing a new time as a guest. Every
// one runs with the REST routes' checks (access levels are capped for
// agents); none shares, emails anyone outside the app or changes access.
func (s *Service) depthTools() []app.Tool {
	return []app.Tool{
		{
			Name:        "calendar_list_calendars",
			Description: "List the user's calendars (their own, their teams', and colleagues' calendars they added) and calendars shared with them they haven't added, each with the user's level: view (free/busy only), comment (all details), edit (make changes) or manage.",
			InputSchema: json.RawMessage(`{"type": "object", "properties": {}, "additionalProperties": false}`),
			ReadOnly:    true,
			Handler:     s.toolListCalendars,
		},
		{
			Name:        "calendar_list_rooms",
			Description: "List the rooms and resources of one of the user's " + orgs.Nouns() + " they can see, optionally only those free for a whole window and seating at least capacity people. Busy times only, never who booked them.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"org_id": {"type": "string", "format": "uuid"},
					"from": {"type": "string", "format": "date-time"},
					"to": {"type": "string", "format": "date-time", "description": "At most 31 days after from."},
					"capacity": {"type": "integer", "minimum": 1},
					"free_only": {"type": "boolean", "description": "Only rooms free for the whole window (needs from and to)."}
				},
				"required": ["org_id"],
				"additionalProperties": false
			}`),
			ReadOnly: true,
			Handler:  s.toolListRooms,
		},
		{
			Name:        "calendar_book_room",
			Description: "Book a room or resource for an event the user may change (every occurrence of a series). Fails with room_busy when it's taken then.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"event_id": {"type": "string", "format": "uuid"},
					"room_id": {"type": "string", "format": "uuid"}
				},
				"required": ["event_id", "room_id"],
				"additionalProperties": false
			}`),
			Handler: s.toolBookRoom,
			Target:  eventTarget,
			Preview: s.previewBookRoom,
		},
		{
			Name:        "calendar_create_time_block",
			Description: "Put out of office or focus time on the user's own calendar. It shows them busy and, with auto_decline new (default), declines invitations to one-off events that arrive during it (all also declines ones already received), with decline_message for organizers. Focus time may repeat.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"type": {"type": "string", "enum": ["out_of_office", "focus"]},
					"title": {"type": "string", "maxLength": 200},
					"starts_at": {"type": "string", "format": "date-time"},
					"ends_at": {"type": "string", "format": "date-time"},
					"all_day": {"type": "boolean"},
					"start_date": {"type": "string", "description": "All-day: first day, YYYY-MM-DD."},
					"end_date": {"type": "string", "description": "All-day: last day (inclusive), YYYY-MM-DD."},
					"timezone": {"type": "string"},
					"auto_decline": {"type": "string", "enum": ["none", "new", "all"]},
					"decline_message": {"type": "string", "maxLength": 500},
					"recurrence": {
						"type": "object",
						"properties": {
							"freq": {"type": "string", "enum": ["daily", "weekly"]},
							"interval": {"type": "integer", "minimum": 1, "maximum": 99},
							"by_day": {"type": "array", "items": {"type": "string"}},
							"count": {"type": "integer", "minimum": 1, "maximum": 999},
							"until": {"type": "string"}
						},
						"required": ["freq"],
						"additionalProperties": false
					}
				},
				"required": ["type"],
				"additionalProperties": false
			}`),
			Handler: s.toolCreateBlock,
			Target:  func(json.RawMessage) (string, string) { return KindEvent, "" },
			Preview: s.previewCreateBlock,
		},
		{
			Name:        "calendar_propose_time",
			Description: "As a guest of a one-off event, propose a new time to its organizer with an optional note (they accept or keep the time). Nobody outside the app is emailed.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"event_id": {"type": "string", "format": "uuid"},
					"starts_at": {"type": "string", "format": "date-time"},
					"ends_at": {"type": "string", "format": "date-time"},
					"note": {"type": "string", "maxLength": 500}
				},
				"required": ["event_id", "starts_at", "ends_at"],
				"additionalProperties": false
			}`),
			Handler: s.toolProposeTime,
			Target:  eventTarget,
			Preview: s.previewProposeTime,
		},
	}
}

func (s *Service) toolListCalendars(ctx context.Context, caller *auth.Identity, _ json.RawMessage) (any, error) {
	mine, others, err := s.listCalendars(ctx, caller.UserID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"items": mine, "shared": others}, nil
}

func (s *Service) toolListRooms(ctx context.Context, caller *auth.Identity, raw json.RawMessage) (any, error) {
	var in struct {
		OrgID    uuid.UUID `json:"org_id"`
		From     string    `json:"from"`
		To       string    `json:"to"`
		Capacity int       `json:"capacity"`
		FreeOnly bool      `json:"free_only"`
	}
	if err := decodeTool(raw, &in); err != nil {
		return nil, err
	}
	var member bool
	if err := s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM org_members WHERE org_id = $1 AND user_id = $2)`, in.OrgID, caller.UserID).Scan(&member); err != nil {
		return nil, err
	}
	if !member {
		return nil, httpx.NotFound("That " + orgs.Noun() + " doesn't exist.")
	}
	window := in.From != "" || in.To != ""
	var from, to time.Time
	var err error
	if window {
		if from, to, err = parseWindow(in.From, in.To, 31*24*time.Hour); err != nil {
			return nil, err
		}
	} else if in.FreeOnly {
		return nil, httpx.BadRequest("free_only needs from and to.")
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT `+roomCols+`, v.level FROM calendar_rooms r
		JOIN access_visible($1, 'calendar.room', $3) v ON v.id = r.id
		WHERE r.org_id = $2 AND r.deleted_at IS NULL AND r.active AND ($4 = 0 OR r.capacity >= $4)
		ORDER BY lower(r.building), lower(r.name), r.id LIMIT 200`, caller.UserID, in.OrgID, access.ViaAgent(ctx), in.Capacity)
	if err != nil {
		return nil, err
	}
	var list []Room
	for rows.Next() {
		var lvl int
		m, err := scanRoom(rows, &lvl)
		if err != nil {
			rows.Close()
			return nil, err
		}
		if lvl >= 1 {
			m.CanBook = lvl >= int(access.Edit)
			list = append(list, m)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := []Room{}
	for _, m := range list {
		if window {
			busy, err := s.roomBusy(ctx, s.d.Pool, m.ID, from, to, uuid.Nil)
			if err != nil {
				return nil, err
			}
			free := len(busy) == 0
			m.Busy, m.Free = busy, &free
			if in.FreeOnly && !free {
				continue
			}
		}
		out = append(out, m)
	}
	return map[string]any{"items": out}, nil
}

type bookRoomInput struct {
	EventID uuid.UUID `json:"event_id"`
	RoomID  uuid.UUID `json:"room_id"`
}

func (s *Service) toolBookRoom(ctx context.Context, caller *auth.Identity, raw json.RawMessage) (any, error) {
	if !caller.Active() {
		return nil, httpx.ErrSuspended
	}
	var in bookRoomInput
	if err := decodeTool(raw, &in); err != nil {
		return nil, err
	}
	res, err := s.resolve(ctx, s.d.Pool, caller.UserID, in.EventID, nil)
	if err != nil {
		return nil, err
	}
	if !res.acc.canEdit() {
		return nil, errNotOrganizer
	}
	rooms, err := eventRooms(ctx, s.d.Pool, res.base.ID)
	if err != nil {
		return nil, err
	}
	ids := []uuid.UUID{}
	for _, r := range rooms {
		ids = append(ids, r.ID)
	}
	if slices.Contains(ids, in.RoomID) {
		return s.detail(ctx, s.d.Pool, caller.UserID, res)
	}
	ids = append(ids, in.RoomID)
	scope := ""
	if res.base.RRule != nil {
		scope = ScopeAll
	}
	return s.updateEvent(ctx, caller, res.base.ID, eventInput{RoomIDs: &ids, Scope: scope})
}

// previewBookRoom: "Book “Board room” for “Design review” Mon, Nov 2, 2026 · 10:00 – 11:00 AM PST".
func (s *Service) previewBookRoom(ctx context.Context, caller *auth.Identity, raw json.RawMessage) (app.ToolPreview, error) {
	var in bookRoomInput
	if err := decodeTool(raw, &in); err != nil {
		return app.ToolPreview{}, err
	}
	res, err := s.resolve(ctx, s.d.Pool, caller.UserID, in.EventID, nil)
	if err != nil {
		return app.ToolPreview{}, err
	}
	if !res.acc.canEdit() {
		return app.ToolPreview{}, errNotOrganizer
	}
	lvl, err := access.LevelOf(ctx, s.d.Pool, caller.UserID, KindRoom, in.RoomID)
	if err != nil {
		return app.ToolPreview{}, err
	}
	var name string
	if lvl < access.View || s.d.Pool.QueryRow(ctx, `SELECT name FROM calendar_rooms WHERE id = $1 AND deleted_at IS NULL`, in.RoomID).Scan(&name) != nil {
		return app.ToolPreview{}, errRoomNotFound
	}
	if lvl < access.Edit {
		return app.ToolPreview{}, httpx.Forbidden("You can't book that room.")
	}
	inst := res.instance()
	when := s.whenFor(ctx, s.d.Pool, caller.UserID, inst.StartsAt, inst.EndsAt, inst.AllDay, inst.loc())
	if res.base.RRule != nil {
		if rule, ok := res.base.rule(); ok {
			when = describeRule(rule, res.base.loc()) + ", from " + when
		}
	}
	rooms, err := eventRooms(ctx, s.d.Pool, res.base.ID)
	if err != nil {
		return app.ToolPreview{}, err
	}
	before := []string{}
	for _, r := range rooms {
		before = append(before, r.Name)
	}
	after := append(slices.Clone(before), name)
	return app.ToolPreview{
		Summary: "Book “" + name + "” for “" + res.base.Title + "” " + when,
		Before:  "Rooms: " + joinOr(before, "none"), After: "Rooms: " + strings.Join(after, ", "), Link: eventLink(res.base.ID),
	}, nil
}

func joinOr(list []string, empty string) string {
	if len(list) == 0 {
		return empty
	}
	return strings.Join(list, ", ")
}

type blockInput struct {
	Type           string          `json:"type"`
	Title          string          `json:"title"`
	StartsAt       *time.Time      `json:"starts_at"`
	EndsAt         *time.Time      `json:"ends_at"`
	AllDay         *bool           `json:"all_day"`
	StartDate      *string         `json:"start_date"`
	EndDate        *string         `json:"end_date"`
	Timezone       *string         `json:"timezone"`
	AutoDecline    *string         `json:"auto_decline"`
	DeclineMessage *string         `json:"decline_message"`
	Recurrence     json.RawMessage `json:"recurrence"`
}

func (in blockInput) event() (eventInput, error) {
	if in.Type != TypeOutOfOffice && in.Type != TypeFocus {
		return eventInput{}, httpx.BadRequest("type must be out_of_office or focus.")
	}
	t := in.Type
	ev := eventInput{EventType: &t, StartsAt: in.StartsAt, EndsAt: in.EndsAt, AllDay: in.AllDay, StartDate: in.StartDate,
		EndDate: in.EndDate, Timezone: in.Timezone, AutoDecline: in.AutoDecline, DeclineMessage: in.DeclineMessage, Recurrence: in.Recurrence}
	if strings.TrimSpace(in.Title) != "" {
		title := in.Title
		ev.Title = &title
	}
	return ev, nil
}

func (s *Service) toolCreateBlock(ctx context.Context, caller *auth.Identity, raw json.RawMessage) (any, error) {
	if !caller.Active() {
		return nil, httpx.ErrSuspended
	}
	var in blockInput
	if err := decodeTool(raw, &in); err != nil {
		return nil, err
	}
	ev, err := in.event()
	if err != nil {
		return nil, err
	}
	var recent int
	if err := s.d.Pool.QueryRow(ctx, `SELECT count(*) FROM calendar_events WHERE owner_id = $1 AND recurring_event_id IS NULL
		AND created_at > now() - interval '1 hour'`, caller.UserID).Scan(&recent); err != nil {
		return nil, err
	}
	if recent >= 120 {
		return nil, httpx.ErrRateLimited
	}
	return s.createEvent(ctx, caller, ev, false)
}

// previewCreateBlock: "Add out of office Fri, Nov 13, 2026 (all day): declines new invitations".
func (s *Service) previewCreateBlock(ctx context.Context, caller *auth.Identity, raw json.RawMessage) (app.ToolPreview, error) {
	var in blockInput
	if err := decodeTool(raw, &in); err != nil {
		return app.ToolPreview{}, err
	}
	if _, err := in.event(); err != nil {
		return app.ToolPreview{}, err
	}
	eventLoc := time.UTC
	if in.Timezone != nil && validZone(*in.Timezone) {
		eventLoc = zoneOr(*in.Timezone, time.UTC)
	}
	loc := s.zoneFor(ctx, s.d.Pool, caller.UserID, eventLoc)
	label := "out of office"
	if in.Type == TypeFocus {
		label = "focus time"
	}
	cp := createPreview{Title: in.Title}
	if in.AllDay != nil && *in.AllDay && in.StartDate != nil {
		cp.AllDay, cp.StartDate = true, *in.StartDate
		if in.EndDate != nil {
			cp.EndDate = *in.EndDate
		}
	} else {
		cp.StartsAt, cp.EndsAt = in.StartsAt, in.EndsAt
	}
	when := createWhen(cp, loc)
	decline := DeclineNew
	if in.AutoDecline != nil {
		decline = *in.AutoDecline
	}
	effect := map[string]string{DeclineNone: "declines nothing", DeclineNew: "declines new invitations", DeclineAll: "declines new and existing invitations"}[decline]
	summary := "Add " + label + " " + when + ": " + effect
	lines := []string{"Type: " + label, "When: " + when, "Invitations: " + effect}
	if strings.TrimSpace(in.Title) != "" {
		lines = append([]string{strings.TrimSpace(in.Title)}, lines...)
	}
	if len(in.Recurrence) > 0 && string(in.Recurrence) != "null" {
		var rc Recurrence
		if json.Unmarshal(in.Recurrence, &rc) == nil {
			repeat := rc.Freq
			if cp.StartsAt != nil {
				if r, err := rc.toRule(*cp.StartsAt, eventLoc); err == nil {
					repeat = describeRule(r, eventLoc)
				}
			}
			lines = append(lines, "Repeats: "+repeat)
			summary += ", repeating " + rc.Freq
		}
	}
	if in.DeclineMessage != nil && strings.TrimSpace(*in.DeclineMessage) != "" {
		lines = append(lines, "Message: "+strings.TrimSpace(*in.DeclineMessage))
	}
	return app.ToolPreview{Summary: summary, After: strings.Join(lines, "\n"), Link: "/calendar"}, nil
}

type proposeInput struct {
	EventID  uuid.UUID `json:"event_id"`
	StartsAt time.Time `json:"starts_at"`
	EndsAt   time.Time `json:"ends_at"`
	Note     string    `json:"note"`
}

func (s *Service) toolProposeTime(ctx context.Context, caller *auth.Identity, raw json.RawMessage) (any, error) {
	if !caller.Active() {
		return nil, httpx.ErrSuspended
	}
	var in proposeInput
	if err := decodeTool(raw, &in); err != nil {
		return nil, err
	}
	if err := s.propose(ctx, caller, in.EventID, in.StartsAt, in.EndsAt, in.Note); err != nil {
		return nil, err
	}
	res, err := s.resolve(ctx, s.d.Pool, caller.UserID, in.EventID, nil)
	if err != nil {
		return nil, err
	}
	return s.detail(ctx, s.d.Pool, caller.UserID, res)
}

// previewProposeTime: "Propose Tue, Nov 3, 2026 · 3:00 – 3:30 PM PST for “Design review”", before = the current time.
func (s *Service) previewProposeTime(ctx context.Context, caller *auth.Identity, raw json.RawMessage) (app.ToolPreview, error) {
	var in proposeInput
	if err := decodeTool(raw, &in); err != nil {
		return app.ToolPreview{}, err
	}
	res, err := s.resolve(ctx, s.d.Pool, caller.UserID, in.EventID, nil)
	if err != nil {
		return app.ToolPreview{}, err
	}
	if res.acc.attendee == nil {
		return app.ToolPreview{}, httpx.BadRequest("Only guests propose new times. Change the event instead.")
	}
	if !in.EndsAt.After(in.StartsAt) {
		return app.ToolPreview{}, httpx.BadRequest("Propose a start before the end.")
	}
	loc := s.zoneFor(ctx, s.d.Pool, caller.UserID, res.base.loc())
	proposed := formatWhen(in.StartsAt, in.EndsAt, false, loc)
	current := formatWhen(res.base.StartsAt, res.base.EndsAt, res.base.AllDay, loc)
	after := "When: " + proposed
	if n := strings.TrimSpace(in.Note); n != "" {
		after += "\nNote: " + n
	}
	return app.ToolPreview{Summary: "Propose " + proposed + " for “" + res.base.Title + "”", Before: "When: " + current, After: after, Link: eventLink(res.base.ID)}, nil
}
