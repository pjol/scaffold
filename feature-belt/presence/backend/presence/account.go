package presence

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// ---- deletion and export -------------------------------------------------------

// DeleteAccount removes the person's devices, status and Do Not Disturb.
func (s *Service) DeleteAccount(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	if _, err := tx.Exec(ctx, `DELETE FROM presence_devices WHERE user_id = $1`, userID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `DELETE FROM presence_status WHERE user_id = $1`, userID)
	return err
}

type exportDevice struct {
	Platform   string    `json:"platform"`
	Active     bool      `json:"active"`
	LastSeenAt time.Time `json:"last_seen_at"`
	CreatedAt  time.Time `json:"created_at"`
}

type exportData struct {
	Emoji      string         `json:"emoji"`
	Text       string         `json:"text"`
	ClearAfter string         `json:"clear_after"`
	ExpiresAt  *time.Time     `json:"expires_at"`
	DNDUntil   *time.Time     `json:"dnd_until"`
	Away       bool           `json:"away"`
	Devices    []exportDevice `json:"devices"`
}

// ExportAccount returns the person's status settings and their devices'
// platforms and heartbeat times (device ids are left out).
func (s *Service) ExportAccount(ctx context.Context, userID uuid.UUID) (any, error) {
	out := exportData{ClearAfter: "never", Devices: []exportDevice{}}
	err := s.d.Pool.QueryRow(ctx, `
		SELECT emoji, text, clear_after, expires_at, dnd_until, away FROM presence_status WHERE user_id = $1`, userID).
		Scan(&out.Emoji, &out.Text, &out.ClearAfter, &out.ExpiresAt, &out.DNDUntil, &out.Away)
	if err != nil && !db.IsNoRows(err) {
		return nil, err
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT platform, active, last_seen_at, created_at FROM presence_devices WHERE user_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	if out.Devices, err = pgx.CollectRows(rows, pgx.RowToStructByPos[exportDevice]); err != nil {
		return nil, err
	}
	if out.Devices == nil {
		out.Devices = []exportDevice{}
	}
	return out, nil
}

// ---- development data --------------------------------------------------------------

// SeedDev gives three dev identities a custom status and one of them Do Not
// Disturb, so status lines and the DND mark show without setting anything.
// Online state comes only from real heartbeats. users are admin, sam, jordan,
// priya (auth.DevIdentities order). Re-running refreshes the expiry times.
func (s *Service) SeedDev(ctx context.Context, users []uuid.UUID) error {
	if len(users) < 4 {
		return nil
	}
	sam, jordan, priya := users[1], users[2], users[3]
	type seed struct {
		user              uuid.UUID
		emoji, text, when string
		expires           string // SQL interval from now, or "" for never
		dnd               string
	}
	for _, x := range []seed{
		{sam, "🎧", "Heads down on the launch plan", "4h", "4 hours", ""},
		{jordan, "📅", "In meetings until 3", "today", "6 hours", ""},
		{priya, "🌴", "Back on Monday", "week", "5 days", "2 hours"},
	} {
		if _, err := s.d.Pool.Exec(ctx, `
			INSERT INTO presence_status (user_id, emoji, text, clear_after, expires_at, dnd_until)
			VALUES ($1, $2, $3, $4, now() + $5::interval, CASE WHEN $6 = '' THEN NULL ELSE now() + $6::interval END)
			ON CONFLICT (user_id) DO UPDATE SET emoji = EXCLUDED.emoji, text = EXCLUDED.text, clear_after = EXCLUDED.clear_after,
			  expires_at = EXCLUDED.expires_at, dnd_until = EXCLUDED.dnd_until, updated_at = now()`,
			x.user, x.emoji, x.text, x.when, x.expires, x.dnd); err != nil {
			return err
		}
	}
	return nil
}

// ---- agent tools -------------------------------------------------------------------

// Tools exposes presence to agents, always as the caller and through the
// same visibility policy as GET /presence.
func (s *Service) Tools() []app.Tool {
	return []app.Tool{{
		Name:        "presence_get_status",
		Description: "Get whether people are online, away or offline, their custom status and whether Do Not Disturb is on. Only people the caller may see are returned.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{
			"user_ids":{"type":"array","items":{"type":"string","format":"uuid"},"minItems":1,"maxItems":100,"description":"The people to look up."}
		},"required":["user_ids"],"additionalProperties":false}`),
		ReadOnly: true,
		Handler:  s.toolGetStatus,
	}}
}

func (s *Service) toolGetStatus(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
	var in struct {
		UserIDs []uuid.UUID `json:"user_ids"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, httpx.BadRequest("Invalid input: " + err.Error())
	}
	if len(in.UserIDs) == 0 || len(in.UserIDs) > maxBatch {
		return nil, httpx.BadRequest("Send 1 to 100 user_ids.")
	}
	items, err := s.visiblePresence(ctx, s.d.Pool, caller.UserID, in.UserIDs)
	if err != nil {
		return nil, err
	}
	type item struct {
		UserID     uuid.UUID  `json:"user_id"`
		Name       string     `json:"name"`
		Status     string     `json:"status"`
		StatusText string     `json:"status_text,omitempty"`
		ClearsAt   *time.Time `json:"status_clears_at,omitempty"`
		DNDUntil   *time.Time `json:"do_not_disturb_until,omitempty"`
	}
	names := map[uuid.UUID]string{}
	ids := make([]uuid.UUID, 0, len(items))
	for _, p := range items {
		ids = append(ids, p.UserID)
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT id, display_name FROM users WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id uuid.UUID
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			rows.Close()
			return nil, err
		}
		names[id] = name
	}
	rows.Close()
	out := make([]item, 0, len(items))
	for _, p := range items {
		it := item{UserID: p.UserID, Name: names[p.UserID], Status: p.Status, DNDUntil: p.DNDUntil}
		if p.Custom != nil {
			it.StatusText = joinNonEmpty(p.Custom.Emoji, p.Custom.Text)
			it.ClearsAt = p.Custom.ExpiresAt
		}
		out = append(out, it)
	}
	return map[string]any{"people": out}, nil
}

func joinNonEmpty(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + " " + b
}
