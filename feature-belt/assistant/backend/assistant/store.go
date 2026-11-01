package assistant

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

var isNoRows = db.IsNoRows

// ---- conversations ---------------------------------------------------------

// Conversation is a conversation as clients see it. Running is true while a
// turn streams (clients resume with GET …/messages and Accept:
// text/event-stream).
type Conversation struct {
	ID            uuid.UUID `json:"id"`
	Title         string    `json:"title"`
	Running       bool      `json:"running"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	LastMessageAt time.Time `json:"last_message_at"`
}

const convCols = `c.id, c.title,
	EXISTS (SELECT 1 FROM assistant_messages m WHERE m.conversation_id = c.id AND m.status = 'streaming'),
	c.created_at, c.updated_at, c.last_message_at`

// convRow is the internal view, with compaction state.
type convRow struct {
	ID               uuid.UUID
	UserID           uuid.UUID
	Title            string
	Summary          string
	SummaryThroughAt *time.Time
	SummaryThroughID *uuid.UUID
	ContextTokens    int
}

// loadConv returns the caller's conversation (404 for anyone else's).
func (s *Service) loadConv(ctx context.Context, q db.Querier, userID, convID uuid.UUID, lock bool) (*convRow, error) {
	sql := `SELECT id, user_id, title, summary, summary_through_at, summary_through_id, context_tokens
		FROM assistant_conversations WHERE id = $1 AND user_id = $2`
	if lock {
		sql += ` FOR UPDATE`
	}
	var c convRow
	err := q.QueryRow(ctx, sql, convID, userID).Scan(&c.ID, &c.UserID, &c.Title, &c.Summary,
		&c.SummaryThroughAt, &c.SummaryThroughID, &c.ContextTokens)
	if isNoRows(err) {
		return nil, httpx.ErrNotFound
	}
	return &c, err
}

func (s *Service) conversationDTO(ctx context.Context, userID, convID uuid.UUID) (*Conversation, error) {
	rows, err := s.d.Pool.Query(ctx, `SELECT `+convCols+` FROM assistant_conversations c WHERE c.id = $1 AND c.user_id = $2`, convID, userID)
	if err != nil {
		return nil, err
	}
	c, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByPos[Conversation])
	if isNoRows(err) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// ---- messages --------------------------------------------------------------

// DisplayPart is a renderable piece of a message.
type DisplayPart struct {
	Type string `json:"type"` // text
	Text string `json:"text"`
}

// MessageError is a failed turn's error as clients see it.
type MessageError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

// TurnUsage is one turn's token accounting.
type TurnUsage struct {
	Usage
	Rounds    int  `json:"rounds"`
	Estimated bool `json:"estimated"`
}

// MessageDTO is a message as clients see it.
type MessageDTO struct {
	ID             uuid.UUID     `json:"id"`
	ConversationID uuid.UUID     `json:"conversation_id"`
	Role           string        `json:"role"`
	Parts          []DisplayPart `json:"parts"`
	Activity       []Activity    `json:"activity"`
	Provider       string        `json:"provider"`
	Model          string        `json:"model"`
	Status         string        `json:"status"` // streaming | complete | cancelled | error
	StopReason     string        `json:"stop_reason"`
	Error          *MessageError `json:"error"`
	Usage          *TurnUsage    `json:"usage"`
	CostUSD        *string       `json:"cost_usd"`
	Removed        bool          `json:"removed"`
	CreatedAt      time.Time     `json:"created_at"`
	CompletedAt    *time.Time    `json:"completed_at"`
}

const msgCols = `id, conversation_id, role, parts, activity, provider, model, status, stop_reason, error, usage,
	cost_usd::text, removed_at IS NOT NULL, created_at, completed_at`

func scanMessage(row pgx.Row) (*MessageDTO, error) {
	var m MessageDTO
	var usage TurnUsage
	err := row.Scan(&m.ID, &m.ConversationID, &m.Role, &m.Parts, &m.Activity, &m.Provider, &m.Model, &m.Status,
		&m.StopReason, &m.Error, &usage, &m.CostUSD, &m.Removed, &m.CreatedAt, &m.CompletedAt)
	if err != nil {
		return nil, err
	}
	if m.Role == RoleAssistant {
		m.Usage = &usage
	}
	if m.Parts == nil {
		m.Parts = []DisplayPart{}
	}
	if m.Activity == nil {
		m.Activity = []Activity{}
	}
	if m.Removed {
		m.Parts = []DisplayPart{}
	}
	return &m, nil
}

func (s *Service) loadMessage(ctx context.Context, id uuid.UUID) (*MessageDTO, error) {
	return scanMessage(s.d.Pool.QueryRow(ctx, `SELECT `+msgCols+` FROM assistant_messages WHERE id = $1`, id))
}

// latestAssistant returns the newest assistant message of a conversation
// (nil when there is none).
func (s *Service) latestAssistant(ctx context.Context, convID uuid.UUID) (*MessageDTO, error) {
	m, err := scanMessage(s.d.Pool.QueryRow(ctx, `SELECT `+msgCols+` FROM assistant_messages
		WHERE conversation_id = $1 AND role = 'assistant' ORDER BY created_at DESC, id DESC LIMIT 1`, convID))
	if isNoRows(err) {
		return nil, nil
	}
	return m, err
}

func textParts(text string) []DisplayPart {
	if strings.TrimSpace(text) == "" {
		return []DisplayPart{}
	}
	return []DisplayPart{{Type: "text", Text: text}}
}

// historyRow is a stored message as the history builder needs it.
type historyRow struct {
	ID        uuid.UUID
	Role      string
	Parts     []DisplayPart
	Activity  []Activity
	Items     []Message
	Status    string
	Removed   bool
	CreatedAt time.Time
}

// historyRows returns the messages after the summary point, oldest first,
// excluding the turn being answered.
func (s *Service) historyRows(ctx context.Context, conv *convRow, exclude uuid.UUID) ([]historyRow, error) {
	var afterAt *time.Time
	var afterID uuid.UUID
	if conv.SummaryThroughAt != nil && conv.SummaryThroughID != nil {
		afterAt, afterID = conv.SummaryThroughAt, *conv.SummaryThroughID
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT id, role, parts, activity, items, status, removed_at IS NOT NULL, created_at
		FROM assistant_messages
		WHERE conversation_id = $1 AND id <> $2
		  AND ($3::timestamptz IS NULL OR (created_at, id) > ($3, $4))
		ORDER BY created_at, id`, conv.ID, exclude, afterAt, afterID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[historyRow])
}
