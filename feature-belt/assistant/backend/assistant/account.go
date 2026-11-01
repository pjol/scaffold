package assistant

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
	"scaffoldapp/backend/internal/moderation"
)

// ---- account deletion and export -------------------------------------------

// DeleteAccount removes the user's conversations, messages, usage rows and
// consents (AI conversations are personal content: deleted, not kept).
func (s *Service) DeleteAccount(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	s.turns.cancelUser(userID, errStopped)
	for _, q := range []string{
		`DELETE FROM assistant_messages WHERE user_id = $1`,
		`DELETE FROM assistant_conversations WHERE user_id = $1`,
		`DELETE FROM assistant_usage_daily WHERE user_id = $1`,
		`DELETE FROM assistant_consents WHERE user_id = $1`,
	} {
		if _, err := tx.Exec(ctx, q, userID); err != nil {
			return err
		}
	}
	return nil
}

type exportMessage struct {
	Role       string     `json:"role"`
	Text       string     `json:"text"`
	Tools      []string   `json:"tools,omitempty"`
	Status     string     `json:"status"`
	Provider   string     `json:"provider,omitempty"`
	Model      string     `json:"model,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	RemovedAt  *time.Time `json:"removed_at,omitempty"`
}

type exportConversation struct {
	ID        uuid.UUID       `json:"id"`
	Title     string          `json:"title"`
	Summary   string          `json:"summary,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
	Messages  []exportMessage `json:"messages"`
}

type exportUsage struct {
	Day          string `json:"day"`
	Messages     int    `json:"messages"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	CostUSD      string `json:"cost_usd"`
}

type exportConsent struct {
	Provider   string    `json:"provider"`
	AcceptedAt time.Time `json:"accepted_at"`
}

// ExportAccount returns the user's conversations with their messages, plus
// usage and consent records.
func (s *Service) ExportAccount(ctx context.Context, userID uuid.UUID) (any, error) {
	out := struct {
		Conversations []exportConversation `json:"conversations"`
		Usage         []exportUsage        `json:"usage"`
		Consents      []exportConsent      `json:"consents"`
	}{Conversations: []exportConversation{}, Usage: []exportUsage{}, Consents: []exportConsent{}}

	rows, err := s.d.Pool.Query(ctx, `SELECT id, title, summary, created_at FROM assistant_conversations
		WHERE user_id = $1 ORDER BY created_at, id`, userID)
	if err != nil {
		return nil, err
	}
	index := map[uuid.UUID]int{}
	for rows.Next() {
		var c exportConversation
		if err := rows.Scan(&c.ID, &c.Title, &c.Summary, &c.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		c.Messages = []exportMessage{}
		index[c.ID] = len(out.Conversations)
		out.Conversations = append(out.Conversations, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = s.d.Pool.Query(ctx, `SELECT conversation_id, role, parts, activity, status, provider, model, created_at, removed_at
		FROM assistant_messages WHERE user_id = $1 ORDER BY created_at, id`, userID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var convID uuid.UUID
		var parts []DisplayPart
		var activity []Activity
		var m exportMessage
		if err := rows.Scan(&convID, &m.Role, &parts, &activity, &m.Status, &m.Provider, &m.Model, &m.CreatedAt, &m.RemovedAt); err != nil {
			rows.Close()
			return nil, err
		}
		m.Text = displayText(parts)
		for _, a := range activity {
			m.Tools = append(m.Tools, a.Name+": "+a.Summary)
		}
		if i, ok := index[convID]; ok {
			out.Conversations[i].Messages = append(out.Conversations[i].Messages, m)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = s.d.Pool.Query(ctx, `SELECT day::text, messages, input_tokens + cache_read_tokens + cache_write_tokens,
		output_tokens, cost_usd::text FROM assistant_usage_daily WHERE user_id = $1 ORDER BY day`, userID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var u exportUsage
		if err := rows.Scan(&u.Day, &u.Messages, &u.InputTokens, &u.OutputTokens, &u.CostUSD); err != nil {
			rows.Close()
			return nil, err
		}
		out.Usage = append(out.Usage, u)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = s.d.Pool.Query(ctx, `SELECT provider, created_at FROM assistant_consents WHERE user_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var c exportConsent
		if err := rows.Scan(&c.Provider, &c.AcceptedAt); err != nil {
			rows.Close()
			return nil, err
		}
		out.Consents = append(out.Consents, c)
	}
	rows.Close()
	return out, rows.Err()
}

// ---- development seed ------------------------------------------------------

// SeedDev gives the second dev identity (Sam) one finished conversation, so
// the history screen has something to show. It records no consent: the
// consent dialog is part of what development exercises.
func (s *Service) SeedDev(ctx context.Context, users []uuid.UUID) error {
	if len(users) < 2 {
		return nil
	}
	sam := users[1]
	ns := uuid.MustParse("6f1b3a52-7a0e-4f63-9d0e-2a3f1c5b8e71")
	convID := uuid.NewSHA1(ns, []byte("assistant:conversation:"+sam.String()))
	userMsg := uuid.NewSHA1(ns, []byte("assistant:message:1:"+sam.String()))
	replyMsg := uuid.NewSHA1(ns, []byte("assistant:message:2:"+sam.String()))
	question := "What should I pack for a two-day hike with one night in a hut?"
	answer := "For two days with a hut night, pack light and layered:\n\n" +
		"- Rain jacket, warm mid-layer, spare socks and a hat\n" +
		"- 2 litres of water, lunch for both days and snacks\n" +
		"- Map or offline maps, headlamp, small first-aid kit\n" +
		"- Sleeping-bag liner and earplugs (most huts provide blankets)\n" +
		"- Cash for the hut, in case cards aren't accepted\n\n" +
		"Check the hut's booking page for what it supplies and the forecast the day before."
	return execAll(ctx, s, []stmt{
		{`INSERT INTO assistant_conversations (id, user_id, title, created_at, updated_at, last_message_at)
		  VALUES ($1, $2, 'Packing for a hut hike', now() - interval '2 days', now() - interval '2 days', now() - interval '2 days')
		  ON CONFLICT (id) DO NOTHING`, []any{convID, sam}},
		{`INSERT INTO assistant_messages (id, conversation_id, user_id, role, parts, created_at)
		  VALUES ($1, $2, $3, 'user', $4, now() - interval '2 days')
		  ON CONFLICT (id) DO NOTHING`, []any{userMsg, convID, sam, textParts(question)}},
		{`INSERT INTO assistant_messages (id, conversation_id, user_id, role, parts, provider, model, status, stop_reason,
		    usage, created_at, completed_at)
		  VALUES ($1, $2, $3, 'assistant', $4, 'seed', 'seed', 'complete', 'end_turn', '{"rounds":1,"estimated":true}',
		    now() - interval '2 days' + interval '1 second', now() - interval '2 days' + interval '6 seconds')
		  ON CONFLICT (id) DO NOTHING`, []any{replyMsg, convID, sam, textParts(answer)}},
	})
}

type stmt struct {
	sql  string
	args []any
}

func execAll(ctx context.Context, s *Service, stmts []stmt) error {
	for _, st := range stmts {
		if _, err := s.d.Pool.Exec(ctx, st.sql, st.args...); err != nil {
			return err
		}
	}
	return nil
}

// ---- moderation target -----------------------------------------------------

// messageTarget makes assistant replies reportable (Play's generative-AI
// policy and Apple 1.2 require in-app reporting of offensive output).
type messageTarget struct{ s *Service }

// Snapshot captures the reply and the prompt that produced it, so staff can
// judge it in context. AI output has no human author.
func (t messageTarget) Snapshot(ctx context.Context, id string) (moderation.Snapshot, error) {
	msgID, err := uuid.Parse(id)
	if err != nil {
		return moderation.Snapshot{}, httpx.ErrNotFound
	}
	var convID uuid.UUID
	var parts []DisplayPart
	var created time.Time
	err = t.s.d.Pool.QueryRow(ctx, `SELECT conversation_id, parts, created_at FROM assistant_messages
		WHERE id = $1 AND role = 'assistant'`, msgID).Scan(&convID, &parts, &created)
	if isNoRows(err) {
		return moderation.Snapshot{}, httpx.ErrNotFound
	}
	if err != nil {
		return moderation.Snapshot{}, err
	}
	var prompt []DisplayPart
	err = t.s.d.Pool.QueryRow(ctx, `SELECT parts FROM assistant_messages
		WHERE conversation_id = $1 AND role = 'user' AND created_at < $2 ORDER BY created_at DESC, id DESC LIMIT 1`,
		convID, created).Scan(&prompt)
	if err != nil && !isNoRows(err) {
		return moderation.Snapshot{}, err
	}
	text := "AI reply:\n" + displayText(parts)
	if p := displayText(prompt); p != "" {
		text += "\n\nPrompt:\n" + p
	}
	return moderation.Snapshot{Text: truncateRunes(text, 8000), MediaKeys: []string{}, Link: "/assistant/" + convID.String()}, nil
}

// Remove hides the reply for its owner and from future prompts.
func (t messageTarget) Remove(ctx context.Context, tx pgx.Tx, id string) error {
	msgID, err := uuid.Parse(id)
	if err != nil {
		return httpx.ErrNotFound
	}
	tag, err := tx.Exec(ctx, `UPDATE assistant_messages SET removed_at = now(), updated_at = now()
		WHERE id = $1 AND role = 'assistant' AND removed_at IS NULL`, msgID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.ErrNotFound
	}
	return nil
}

// ---- agent tools -----------------------------------------------------------

// Tools are the assistant's own read tools (also served over MCP when the
// mcp feature is installed).
func (s *Service) Tools() []app.Tool {
	return []app.Tool{
		{
			Name:        "assistant_get_usage",
			Description: "Get the user's assistant usage for today (messages and tokens) against the daily limits, and when they reset. Call this when the user asks how much they can still use the assistant.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
			ReadOnly:    true,
			Handler: func(ctx context.Context, caller *auth.Identity, _ json.RawMessage) (any, error) {
				return s.usageToday(ctx, caller)
			},
		},
		{
			Name:        "assistant_search_conversations",
			Description: "List the user's assistant conversations, most recent first, optionally filtered by words in the title. Call this when the user refers to an earlier conversation.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{` +
				`"query":{"type":"string","maxLength":100,"description":"Words to look for in conversation titles."},` +
				`"limit":{"type":"integer","minimum":1,"maximum":20,"description":"How many conversations to return (default 10)."}},` +
				`"additionalProperties":false}`),
			ReadOnly: true,
			Handler:  s.searchConversationsTool,
		},
	}
}

func (s *Service) searchConversationsTool(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
	var in struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, httpx.BadRequest("Invalid input.")
	}
	if in.Limit <= 0 {
		in.Limit = 10
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT `+convCols+` FROM assistant_conversations c
		WHERE c.user_id = $1 AND ($2 = '' OR c.title ILIKE '%' || $2 || '%')
		ORDER BY c.last_message_at DESC, c.id DESC LIMIT $3`,
		caller.UserID, escapeLike(strings.TrimSpace(in.Query)), min(in.Limit, 20))
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Conversation])
	if err != nil {
		return nil, err
	}
	items := make([]map[string]any, 0, len(list))
	for _, c := range list {
		items = append(items, map[string]any{"id": c.ID, "title": c.Title, "last_message_at": c.LastMessageAt,
			"link": fmt.Sprintf("/assistant/%s", c.ID)})
	}
	return map[string]any{"items": items}, nil
}
