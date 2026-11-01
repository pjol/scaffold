package events

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
)

// Tools exposes read-only event tools to agents, run as the caller.
func (s *Service) Tools() []app.Tool {
	return []app.Tool{
		{
			Name:        "events_list_upcoming",
			Description: "List upcoming events the user can see, soonest first, with their RSVP. Filter to events they host or attend. Pass next_cursor back for more.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"filter": {"type": "string", "enum": ["all", "hosting", "attending"], "description": "Default all."},
					"limit": {"type": "integer", "minimum": 1, "maximum": 50, "description": "Events per page (default 20)."},
					"cursor": {"type": "string", "description": "next_cursor from the previous page."}
				},
				"additionalProperties": false
			}`),
			ReadOnly: true,
			Handler:  s.toolList,
		},
		{
			Name:        "events_get",
			Description: "Get one event by id: time, place, hosts, RSVP counts and the user's own RSVP.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {"id": {"type": "string", "format": "uuid"}},
				"required": ["id"],
				"additionalProperties": false
			}`),
			ReadOnly: true,
			Handler:  s.toolGet,
		},
	}
}

func (s *Service) toolList(ctx context.Context, caller *auth.Identity, raw json.RawMessage) (any, error) {
	var in struct {
		Filter string `json:"filter"`
		Limit  int    `json:"limit"`
		Cursor string `json:"cursor"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, httpx.BadRequest("Invalid input: " + err.Error())
		}
	}
	if in.Limit <= 0 || in.Limit > 50 {
		in.Limit = 20
	}
	switch in.Filter {
	case "", "all":
		in.Filter = ""
	case "hosting", "attending":
	default:
		return nil, httpx.BadRequest("filter must be all, hosting or attending.")
	}
	return s.listEvents(ctx, caller.UserID, listFilter{when: "upcoming", mine: in.Filter, limit: in.Limit, cursor: in.Cursor})
}

func (s *Service) toolGet(ctx context.Context, caller *auth.Identity, raw json.RawMessage) (any, error) {
	var in struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, httpx.BadRequest("Invalid input: " + err.Error())
	}
	id, err := uuid.Parse(in.ID)
	if err != nil {
		return nil, httpx.BadRequest("id must be a uuid.")
	}
	e, _, err := s.loadEvent(ctx, id, caller.UserID)
	if err != nil {
		return nil, err
	}
	return e, nil
}
