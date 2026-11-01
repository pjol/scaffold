package moderation

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
)

// Tools exposes the caller's blocks and reports, blocking (a protective,
// reversible write) and, for staff, the queue summary. Unblocking and
// filing reports stay in the app, where a person confirms them.
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
			Name:        "moderation_block_user",
			Description: "Block a person for the user: hides their content and stops messages in both directions. Unblocking is done in the app.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"user_id":{"type":"string","format":"uuid"}},"required":["user_id"],"additionalProperties":false}`),
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					UserID uuid.UUID `json:"user_id"`
				}
				if err := json.Unmarshal(input, &in); err != nil || in.UserID == uuid.Nil {
					return nil, httpx.BadRequest("user_id must be a user id.")
				}
				if in.UserID == caller.UserID {
					return nil, httpx.BadRequest("You can't block yourself.")
				}
				return Block(ctx, s.d.Pool, caller.UserID, in.UserID)
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
