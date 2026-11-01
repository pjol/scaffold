package assistant

import (
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
)

type cursor struct {
	T  time.Time `json:"t"`
	ID uuid.UUID `json:"id"`
}

func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// listConversations is GET /v1/assistant/conversations: the caller's
// conversations, most recently active first (keyset on last_message_at, id).
func (s *Service) listConversations(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	p := httpx.ParsePage(r)
	var c cursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	rows, err := s.d.Pool.Query(r.Context(), `
		SELECT `+convCols+` FROM assistant_conversations c
		WHERE c.user_id = $1
		  AND ($2 = '' OR c.title ILIKE '%' || $2 || '%')
		  AND ($3::timestamptz IS NULL OR (c.last_message_at, c.id) < ($3, $4))
		ORDER BY c.last_message_at DESC, c.id DESC LIMIT $5`,
		id.UserID, escapeLike(q), nullTime(c.T), c.ID, p.Limit+1)
	if err != nil {
		return err
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Conversation])
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(list, p.Limit, func(c Conversation) any { return cursor{c.LastMessageAt, c.ID} }))
	return nil
}

// escapeLike makes user input literal inside ILIKE.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

type createConvReq struct {
	Title string `json:"title"`
}

func (s *Service) createConversation(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	var req createConvReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	title, err := cleanTitle(req.Title)
	if err != nil {
		return err
	}
	var convID uuid.UUID
	if err := s.d.Pool.QueryRow(r.Context(), `
		INSERT INTO assistant_conversations (user_id, title) VALUES ($1, $2) RETURNING id`,
		id.UserID, title).Scan(&convID); err != nil {
		return err
	}
	c, err := s.conversationDTO(r.Context(), id.UserID, convID)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, c)
	return nil
}

func cleanTitle(raw string) (string, error) {
	t, _ := cleanText(raw)
	t = strings.Join(strings.Fields(t), " ")
	if len([]rune(t)) > 120 {
		return "", httpx.BadRequest("Titles can be up to 120 characters.")
	}
	return t, nil
}

func (s *Service) getConversation(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	convID, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	c, err := s.conversationDTO(r.Context(), id.UserID, convID)
	if err != nil {
		return err
	}
	httpx.OK(w, c)
	return nil
}

type renameReq struct {
	Title *string `json:"title"`
}

func (s *Service) renameConversation(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	convID, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req renameReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.Title == nil {
		return httpx.BadRequest("Send a title.")
	}
	title, err := cleanTitle(*req.Title)
	if err != nil {
		return err
	}
	if title == "" {
		return httpx.BadRequest("Titles can't be empty.")
	}
	tag, err := s.d.Pool.Exec(r.Context(), `
		UPDATE assistant_conversations SET title = $3, updated_at = now() WHERE id = $1 AND user_id = $2`,
		convID, id.UserID, title)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.ErrNotFound
	}
	return s.getConversation(w, r)
}

// deleteConversation hard-deletes a conversation and its messages (AI
// conversations are personal data with no other viewer). A running turn is
// stopped; its final write finds no row.
func (s *Service) deleteConversation(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	convID, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	tag, err := s.d.Pool.Exec(r.Context(), `DELETE FROM assistant_conversations WHERE id = $1 AND user_id = $2`, convID, id.UserID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.ErrNotFound
	}
	if t := s.turns.get(convID); t != nil {
		t.cancel(errStopped)
	}
	httpx.NoContent(w)
	return nil
}

// messagesOrResume is GET /v1/assistant/conversations/{id}/messages. With
// Accept: text/event-stream it resumes the running turn (or replays the last
// reply and done); otherwise it returns a page of messages, newest first
// (keyset on created_at, id).
func (s *Service) messagesOrResume(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	convID, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	if _, err := s.loadConv(r.Context(), s.d.Pool, id.UserID, convID, false); err != nil {
		return err
	}
	if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		return s.resume(w, r, convID)
	}
	p := httpx.ParsePage(r)
	var c cursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	rows, err := s.d.Pool.Query(r.Context(), `
		SELECT `+msgCols+` FROM assistant_messages
		WHERE conversation_id = $1 AND ($2::timestamptz IS NULL OR (created_at, id) < ($2, $3))
		ORDER BY created_at DESC, id DESC LIMIT $4`, convID, nullTime(c.T), c.ID, p.Limit+1)
	if err != nil {
		return err
	}
	defer rows.Close()
	list := []MessageDTO{}
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return err
		}
		list = append(list, *m)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(list, p.Limit, func(m MessageDTO) any { return cursor{m.CreatedAt, m.ID} }))
	return nil
}
