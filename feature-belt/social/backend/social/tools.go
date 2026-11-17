package social

import (
	"context"
	"encoding/json"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
)

// Tools are read-only. There is no follow or unfollow tool: a follow is seen
// by others and notifies the person followed, so it stays the person's own
// act (the same rule that keeps blocking away from agents).
func (s *Service) Tools() []app.Tool {
	return []app.Tool{
		{
			Name:        "social_get_profile",
			Description: "Read a person's profile by handle (or \"me\"): name, bio, links, follower counts, whether they follow the user, and their latest activity the user may see.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"handle": {"type": "string", "minLength": 1, "description": "Handle with or without @, or \"me\"."},
					"activity": {"type": "integer", "minimum": 0, "maximum": 25, "description": "How many recent activity items to include (default 10)."}
				},
				"required": ["handle"],
				"additionalProperties": false
			}`),
			ReadOnly: true,
			Handler:  s.toolProfile,
		},
		{
			Name:        "social_list_feed",
			Description: "List recent activity from the people the user follows, newest first. Pass next_cursor back for older items.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"limit": {"type": "integer", "minimum": 1, "maximum": 50, "description": "Items per page (default 20)."},
					"cursor": {"type": "string", "description": "next_cursor from the previous page."}
				},
				"additionalProperties": false
			}`),
			ReadOnly: true,
			Handler:  s.toolFeed,
		},
		{
			Name:        "social_search_people",
			Description: "Find people by handle or name prefix (at least 2 characters).",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"q": {"type": "string", "minLength": 2},
					"limit": {"type": "integer", "minimum": 1, "maximum": 25}
				},
				"required": ["q"],
				"additionalProperties": false
			}`),
			ReadOnly: true,
			Handler:  s.toolSearch,
		},
	}
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

func (s *Service) toolProfile(ctx context.Context, caller *auth.Identity, raw json.RawMessage) (any, error) {
	in := struct {
		Handle   string `json:"handle"`
		Activity *int   `json:"activity"`
	}{}
	if err := decodeTool(raw, &in); err != nil {
		return nil, err
	}
	owner := caller.UserID
	if h := NormalizeHandle(in.Handle); h != "me" {
		id, _, err := resolveHandle(ctx, s.d.Pool, h)
		if err != nil {
			return nil, err
		}
		owner = id
	}
	p, err := BuildProfile(ctx, s.d.Pool, owner, caller.UserID)
	if err != nil {
		return nil, err
	}
	n := 10
	if in.Activity != nil {
		n = max(0, min(*in.Activity, 25))
	}
	out := map[string]any{"profile": p, "activity": []ActivityItem{}}
	if n > 0 {
		act, err := s.ProfileActivity(ctx, owner, caller.UserID, "", n)
		if err != nil {
			return nil, err
		}
		out["activity"] = act.Items
	}
	return out, nil
}

func (s *Service) toolFeed(ctx context.Context, caller *auth.Identity, raw json.RawMessage) (any, error) {
	in := struct {
		Limit  int    `json:"limit"`
		Cursor string `json:"cursor"`
	}{}
	if err := decodeTool(raw, &in); err != nil {
		return nil, err
	}
	if in.Limit <= 0 || in.Limit > 50 {
		in.Limit = 20
	}
	return s.Feed(ctx, caller.UserID, in.Cursor, in.Limit)
}

func (s *Service) toolSearch(ctx context.Context, caller *auth.Identity, raw json.RawMessage) (any, error) {
	in := struct {
		Q     string `json:"q"`
		Limit int    `json:"limit"`
	}{}
	if err := decodeTool(raw, &in); err != nil {
		return nil, err
	}
	if in.Limit <= 0 || in.Limit > 25 {
		in.Limit = 10
	}
	people, _, err := s.searchPeople(ctx, caller.UserID, in.Q, in.Limit)
	if err != nil {
		return nil, err
	}
	return map[string]any{"people": people}, nil
}
