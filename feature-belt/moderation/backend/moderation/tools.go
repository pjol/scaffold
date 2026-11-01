package moderation

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
)

// Tools exposes the caller's blocks and reports and, for staff, the queue
// summary, all read-only. Blocking, unblocking and filing reports stay in
// the app, where the person decides: a block changes who may reach them and
// whose content they see, which AI agents never change.
func (s *Service) Tools() []app.Tool {
	limitSchema := json.RawMessage(`{"type":"object","properties":{"limit":{"type":"integer","minimum":1,"maximum":100}},"additionalProperties":false}`)
	return []app.Tool{
		{
			Name:        "moderation_list_blocks",
			Description: "List the people the user has blocked, newest first.",
			InputSchema: limitSchema,
			ReadOnly:    true,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				return blockedUsers(ctx, s.d.Pool, caller.UserID, nil, uuid.Nil, toolLimit(input))
			},
		},
		{
			Name:        "moderation_list_reports",
			Description: "List the reports the user has filed and whether each is open, actioned or closed.",
			InputSchema: limitSchema,
			ReadOnly:    true,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				return myReports(ctx, s.d.Pool, caller.UserID, nil, uuid.Nil, toolLimit(input))
			},
		},
		{
			Name:        "moderation_queue_summary",
			Description: "Staff only: open moderation cases, how many are past the 24-hour target, and recent closing times.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
			ReadOnly:    true,
			Handler: func(ctx context.Context, caller *auth.Identity, _ json.RawMessage) (any, error) {
				if !caller.IsSupport() {
					return nil, httpx.ErrForbidden
				}
				return s.loadSummary(ctx)
			},
		},
	}
}

func toolLimit(input json.RawMessage) int {
	var in struct {
		Limit int `json:"limit"`
	}
	_ = json.Unmarshal(input, &in)
	if in.Limit <= 0 || in.Limit > 100 {
		return 20
	}
	return in.Limit
}
