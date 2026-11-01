package notifications

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
)

// ---- account deletion and export -------------------------------------------

// DeleteAccount removes the user's inbox, devices, preferences and pending
// push tickets.
func (s *Service) DeleteAccount(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	for _, q := range []string{
		`DELETE FROM notifications WHERE user_id = $1`,
		`DELETE FROM notification_devices WHERE user_id = $1`,
		`DELETE FROM notification_preferences WHERE user_id = $1`,
		`DELETE FROM notification_push_tickets WHERE user_id = $1`,
	} {
		if _, err := tx.Exec(ctx, q, userID); err != nil {
			return err
		}
	}
	return nil
}

type exportDevice struct {
	Platform   string    `json:"platform"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
}

type exportPreference struct {
	Kind      string    `json:"kind"`
	Push      bool      `json:"push"`
	Email     bool      `json:"email"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ExportAccount returns saved preferences, registered devices (platform and
// dates; tokens are credentials and stay out) and the latest 500 inbox rows.
func (s *Service) ExportAccount(ctx context.Context, userID uuid.UUID) (any, error) {
	rows, err := s.d.Pool.Query(ctx, `SELECT kind, push, email, updated_at FROM notification_preferences
		WHERE user_id = $1 ORDER BY kind`, userID)
	if err != nil {
		return nil, err
	}
	prefs, err := pgx.CollectRows(rows, pgx.RowToStructByPos[exportPreference])
	if err != nil {
		return nil, err
	}
	rows, err = s.d.Pool.Query(ctx, `SELECT platform, created_at, last_seen_at FROM notification_devices
		WHERE user_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	devices, err := pgx.CollectRows(rows, pgx.RowToStructByPos[exportDevice])
	if err != nil {
		return nil, err
	}
	inbox, err := s.inbox(ctx, userID, false, cursor{}, 500)
	if err != nil {
		return nil, err
	}
	return map[string]any{"preferences": prefs, "devices": devices, "inbox": inbox}, nil
}

// ---- development seed ------------------------------------------------------

// seedNS keys the deterministic ids of seed rows, so re-seeding is a no-op.
var seedNS = uuid.MustParse("5d1f3c55-8f5e-4c43-9a43-1b3f7f1f0100")

// SeedDev gives Sam (users[1]) a small inbox and one development device, so
// the inbox, badge and the admin test send have something to show.
func (s *Service) SeedDev(ctx context.Context, users []uuid.UUID) error {
	if len(users) < 2 {
		return nil
	}
	sam := users[1]
	now := time.Now().UTC()
	rows := []struct {
		key, title, body string
		age              time.Duration
		read             bool
	}{
		{"test-1", "Test notification", "Sent from the admin console.", 72 * time.Hour, true},
		{"test-2", "Push check", "Sent from the admin console to check push on your devices.", 26 * time.Hour, false},
		{"test-3", "Test notification", "Sent from the admin console.", 2 * time.Hour, false},
	}
	for _, r := range rows {
		created := now.Add(-r.age)
		var readAt *time.Time
		if r.read {
			t := created.Add(time.Hour)
			readAt = &t
		}
		if _, err := s.d.Pool.Exec(ctx, `
			INSERT INTO notifications (id, user_id, kind, title, body, link, read_at, created_at)
			VALUES ($1, $2, $3, $4, $5, '', $6, $7) ON CONFLICT (id) DO NOTHING`,
			uuid.NewSHA1(seedNS, []byte(r.key)), sam, kindTest, r.title, r.body, readAt, created); err != nil {
			return err
		}
	}
	_, err := s.d.Pool.Exec(ctx, `
		INSERT INTO notification_devices (token, user_id, platform) VALUES ('ExponentPushToken[scaffold-dev-sam]', $1, 'ios')
		ON CONFLICT (token) DO NOTHING`, sam)
	return err
}

// ---- agent tools -----------------------------------------------------------

func (s *Service) Tools() []app.Tool {
	return []app.Tool{
		{
			Name:        "notifications_list_inbox",
			Description: "List the user's recent notifications, newest first, with the unread count.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"unread_only":{"type":"boolean","description":"Only unread notifications."},"limit":{"type":"integer","minimum":1,"maximum":50,"default":20}},"additionalProperties":false}`),
			ReadOnly:    true,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					UnreadOnly bool `json:"unread_only"`
					Limit      int  `json:"limit"`
				}
				if len(input) > 0 {
					if err := json.Unmarshal(input, &in); err != nil {
						return nil, err
					}
				}
				if in.Limit <= 0 || in.Limit > 50 {
					in.Limit = 20
				}
				if s.agentCall(ctx) {
					// Only notifications about what the person lets agents read,
					// and only those in the count.
					items, err := s.agentInbox(ctx, caller, in.UnreadOnly, in.Limit)
					if err != nil {
						return nil, err
					}
					unread, err := s.agentUnread(ctx, caller)
					if err != nil {
						return nil, err
					}
					return map[string]any{"items": items, "unread_count": unread}, nil
				}
				items, err := s.inbox(ctx, caller.UserID, in.UnreadOnly, cursor{}, in.Limit)
				if err != nil {
					return nil, err
				}
				unread, err := unreadCount(ctx, s.d.Pool, caller.UserID)
				if err != nil {
					return nil, err
				}
				return map[string]any{"items": items, "unread_count": unread}, nil
			},
		},
		{
			Name:        "notifications_get_preferences",
			Description: "Show which channels (push, email) are on for each notification kind. The in-app inbox is always on.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
			ReadOnly:    true,
			Handler: func(ctx context.Context, caller *auth.Identity, _ json.RawMessage) (any, error) {
				items, err := s.preferences(ctx, caller.UserID)
				if err != nil {
					return nil, err
				}
				return Preferences{Items: items}, nil
			},
		},
		{
			Name:        "notifications_mark_read",
			Description: "Mark notifications read: pass ids, or all=true for every unread notification. Returns the new unread count.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"ids":{"type":"array","items":{"type":"string","format":"uuid"},"maxItems":500},"all":{"type":"boolean"}},"additionalProperties":false}`),
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				in, err := decodeMarkRead(input)
				if err != nil {
					return nil, err
				}
				if s.agentCall(ctx) {
					// An agent marks only what it may read (and learns only that count).
					items, err := s.markTargets(ctx, caller, in)
					if err != nil {
						return nil, err
					}
					if len(items) > 0 {
						ids := make([]uuid.UUID, len(items))
						for i, it := range items {
							ids[i] = it.ID
						}
						if _, err := s.markReadFor(ctx, caller.UserID, markReadReq{IDs: ids}); err != nil {
							return nil, err
						}
					}
					n, err := s.agentUnread(ctx, caller)
					if err != nil {
						return nil, err
					}
					return UnreadCount{UnreadCount: n}, nil
				}
				n, err := s.markReadFor(ctx, caller.UserID, in)
				if err != nil {
					return nil, err
				}
				return UnreadCount{UnreadCount: n}, nil
			},
			Preview: s.markReadPreview,
		},
	}
}

// decodeMarkRead reads and checks {ids} or {all} (markReadFor's rules).
func decodeMarkRead(input json.RawMessage) (markReadReq, error) {
	var in markReadReq
	if len(input) > 0 {
		if err := json.Unmarshal(input, &in); err != nil {
			return in, httpx.BadRequest("Invalid input: " + err.Error())
		}
	}
	switch {
	case in.All && len(in.IDs) > 0:
		return in, httpx.BadRequest("Send ids or all, not both.")
	case !in.All && len(in.IDs) == 0:
		return in, httpx.BadRequest("Send ids or all.")
	case len(in.IDs) > maxMarkReadIDs:
		return in, httpx.BadRequest("Mark at most 500 at a time.")
	}
	return in, nil
}

// markReadPreview is what the person reviews when their agent scope makes
// marking read a suggestion: the notifications the agent can see that would
// turn read ("all" also marks the rest when the person accepts).
func (s *Service) markReadPreview(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
	in, err := decodeMarkRead(input)
	if err != nil {
		return app.ToolPreview{}, err
	}
	items, err := s.markTargets(ctx, caller, in)
	if err != nil {
		return app.ToolPreview{}, err
	}
	pv := app.ToolPreview{Summary: fmt.Sprintf("Mark %d notifications read", len(items)), Link: "/notifications"}
	switch {
	case in.All:
		pv.Summary = "Mark all unread notifications read"
	case len(items) == 1:
		pv.Summary = "Mark “" + items[0].Title + "” read"
	}
	var before, after strings.Builder
	for i, it := range items {
		if i == 20 {
			fmt.Fprintf(&before, "… and %d more\n", len(items)-i)
			fmt.Fprintf(&after, "… and %d more\n", len(items)-i)
			break
		}
		fmt.Fprintf(&before, "Unread: %s\n", it.Title)
		fmt.Fprintf(&after, "Read: %s\n", it.Title)
	}
	pv.Before, pv.After = before.String(), after.String()
	return pv, nil
}
