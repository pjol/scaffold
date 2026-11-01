package feed

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
)

// Tools exposes read-only feed tools to agents, run as the caller (their
// audiences and blocks apply).
func (s *Service) Tools() []app.Tool {
	return []app.Tool{
		{
			Name:        "feed_list_posts",
			Description: "List posts in the user's feed, newest first. Pass next_cursor back to get older posts. Optionally filter by author.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"limit": {"type": "integer", "minimum": 1, "maximum": 50, "description": "Posts per page (default 20)."},
					"cursor": {"type": "string", "description": "next_cursor from the previous page."},
					"author_id": {"type": "string", "format": "uuid", "description": "Only posts by this user."}
				},
				"additionalProperties": false
			}`),
			ReadOnly: true,
			Handler:  s.toolList,
		},
		{
			Name:        "feed_search_posts",
			Description: "Search posts in the user's feed by words in their text, newest first.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"q": {"type": "string", "minLength": 1, "description": "Words to search for; each matches as a prefix."},
					"limit": {"type": "integer", "minimum": 1, "maximum": 50},
					"cursor": {"type": "string"}
				},
				"required": ["q"],
				"additionalProperties": false
			}`),
			ReadOnly: true,
			Handler:  s.toolSearch,
		},
	}
}

type toolInput struct {
	Limit    int    `json:"limit"`
	Cursor   string `json:"cursor"`
	AuthorID string `json:"author_id"`
	Q        string `json:"q"`
}

func parseToolInput(raw json.RawMessage) (toolInput, error) {
	var in toolInput
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &in); err != nil {
			return in, httpx.BadRequest("Invalid input: " + err.Error())
		}
	}
	if in.Limit <= 0 || in.Limit > 50 {
		in.Limit = 20
	}
	return in, nil
}

func (s *Service) toolList(ctx context.Context, caller *auth.Identity, raw json.RawMessage) (any, error) {
	in, err := parseToolInput(raw)
	if err != nil {
		return nil, err
	}
	f := listFilter{limit: in.Limit, cursor: in.Cursor}
	if in.AuthorID != "" {
		id, err := uuid.Parse(in.AuthorID)
		if err != nil {
			return nil, httpx.BadRequest("author_id must be a uuid.")
		}
		f.authorID = &id
	}
	v, err := s.viewerFor(ctx, caller.UserID)
	if err != nil {
		return nil, err
	}
	return s.listPosts(ctx, v, f)
}

func (s *Service) toolSearch(ctx context.Context, caller *auth.Identity, raw json.RawMessage) (any, error) {
	in, err := parseToolInput(raw)
	if err != nil {
		return nil, err
	}
	if searchQuery(in.Q) == "" {
		return nil, httpx.BadRequest("q needs at least one word.")
	}
	v, err := s.viewerFor(ctx, caller.UserID)
	if err != nil {
		return nil, err
	}
	return s.listPosts(ctx, v, listFilter{limit: in.Limit, cursor: in.Cursor, q: strings.TrimSpace(in.Q)})
}
