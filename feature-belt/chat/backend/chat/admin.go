package chat

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/audit"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/moderation"
)

// ---- moderation target ("chat.message") ------------------------------------------

// messageTarget makes messages reportable. Removal hides the message from
// members (a tombstone) but keeps its text and files for the moderation
// record; Restore undoes it when staff dismiss an auto-hidden report.
type messageTarget struct{ s *Service }

var (
	_ moderation.Target   = messageTarget{}
	_ moderation.Restorer = messageTarget{}
)

func (t messageTarget) Snapshot(ctx context.Context, id string) (moderation.Snapshot, error) {
	msgID, err := uuid.Parse(id)
	if err != nil {
		return moderation.Snapshot{}, httpx.ErrNotFound
	}
	var convID, sender uuid.UUID
	var body string
	err = t.s.d.Pool.QueryRow(ctx, `
		SELECT conversation_id, sender_id, body FROM chat_messages WHERE id = $1 AND kind = 'text'`, msgID).
		Scan(&convID, &sender, &body)
	if err != nil {
		return moderation.Snapshot{}, err // pgx.ErrNoRows: moderation answers "no longer exists"
	}
	rows, err := t.s.d.Pool.Query(ctx, `SELECT storage_key FROM chat_attachments WHERE message_id = $1 ORDER BY position`, msgID)
	if err != nil {
		return moderation.Snapshot{}, err
	}
	keys, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return moderation.Snapshot{}, err
	}
	if keys == nil {
		keys = []string{}
	}
	return moderation.Snapshot{
		AuthorID: &sender, Text: body, MediaKeys: keys,
		Link: fmt.Sprintf("/chat/%s?message=%s", convID, msgID),
	}, nil
}

func (t messageTarget) Remove(ctx context.Context, tx pgx.Tx, id string) error {
	msgID, err := t.exists(ctx, tx, id)
	if err != nil {
		return err
	}
	_, err = hideMessage(ctx, tx, msgID)
	return err
}

func (t messageTarget) Restore(ctx context.Context, tx pgx.Tx, id string) error {
	msgID, err := t.exists(ctx, tx, id)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		UPDATE chat_messages SET deleted_at = NULL, deleted_by = NULL, updated_at = now()
		WHERE id = $1 AND deleted_by = 'staff'`, msgID)
	return err
}

func (messageTarget) exists(ctx context.Context, q db.Querier, id string) (uuid.UUID, error) {
	msgID, err := uuid.Parse(id)
	if err != nil {
		return uuid.Nil, httpx.ErrNotFound
	}
	var ok bool
	if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM chat_messages WHERE id = $1 AND kind = 'text')`, msgID).Scan(&ok); err != nil {
		return uuid.Nil, err
	}
	if !ok {
		return uuid.Nil, httpx.ErrNotFound
	}
	return msgID, nil
}

// ---- staff routes ------------------------------------------------------------------

// AdminConversation is a conversation as staff see it for moderation context:
// every member (including those who left) and every message, with hidden
// content still readable. Each view is audited.
type AdminConversation struct {
	ID            uuid.UUID     `json:"id"`
	Kind          string        `json:"kind"`
	Title         string        `json:"title"`
	AvatarURL     *string       `json:"avatar_url"`
	CreatedBy     *UserRef      `json:"created_by"`
	CreatedAt     time.Time     `json:"created_at"`
	LastMessageAt time.Time     `json:"last_message_at"`
	Members       []AdminMember `json:"members"`
	Messages      *MessagePage  `json:"messages"`
}

// AdminMember is a membership row, current or past.
type AdminMember struct {
	User     UserRef    `json:"user"`
	Role     string     `json:"role"`
	JoinedAt time.Time  `json:"joined_at"`
	LeftAt   *time.Time `json:"left_at"`
}

// GET /admin/chat/conversations/{id}?around=<message id>|before=|after=&limit=
func (s *Service) adminConversation(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	staff := auth.MustFromContext(ctx)
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	win, err := parseWindow(r)
	if err != nil {
		return err
	}
	out := AdminConversation{ID: id, Members: []AdminMember{}}
	var avatarKey *string
	var createdBy *uuid.UUID
	err = s.d.Pool.QueryRow(ctx, `
		SELECT kind, title, avatar_key, created_by, created_at, last_message_at FROM chat_conversations WHERE id = $1`, id).
		Scan(&out.Kind, &out.Title, &avatarKey, &createdBy, &out.CreatedAt, &out.LastMessageAt)
	if db.IsNoRows(err) {
		return errConversationNotFound
	}
	if err != nil {
		return err
	}
	if avatarKey != nil {
		u := s.d.Storage.PublicURL(*avatarKey)
		out.AvatarURL = &u
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT mb.user_id, u.display_name, u.avatar_url, u.status, mb.role, mb.joined_at, mb.left_at
		FROM chat_members mb JOIN users u ON u.id = mb.user_id
		WHERE mb.conversation_id = $1
		ORDER BY mb.left_at IS NOT NULL, CASE mb.role WHEN 'owner' THEN 0 WHEN 'admin' THEN 1 ELSE 2 END, mb.joined_at`, id)
	if err != nil {
		return err
	}
	for rows.Next() {
		var uid uuid.UUID
		var name, status string
		var avatar *string
		var m AdminMember
		if err := rows.Scan(&uid, &name, &avatar, &status, &m.Role, &m.JoinedAt, &m.LeftAt); err != nil {
			rows.Close()
			return err
		}
		m.User = userRef(uid, name, avatar, status)
		out.Members = append(out.Members, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if createdBy != nil {
		users, err := loadUsers(ctx, s.d.Pool, []uuid.UUID{*createdBy})
		if err != nil {
			return err
		}
		if u, ok := users[*createdBy]; ok {
			out.CreatedBy = &u
		}
	}
	if out.Kind == kindDirect {
		names := []string{}
		for _, m := range out.Members {
			names = append(names, m.User.DisplayName)
		}
		out.Title = strings.Join(names, ", ")
	}
	win.conversationID, win.staff = id, true
	if out.Messages, err = s.pageMessages(ctx, s.d.Pool, win); err != nil {
		return err
	}
	for _, m := range out.Messages.Items {
		m.DeletedBy = m.deletedBy
	}
	if err := audit.Log(ctx, s.d.Pool, staff.UserID, "chat.conversation.view", "chat.conversation", id.String(),
		map[string]any{"before": win.before, "after": win.after, "around": win.around}); err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

type adminRemoveReq struct {
	Reason string `json:"reason"`
}

// DELETE /admin/chat/messages/{id} {reason} — hides the message for members
// (a tombstone); the text and files stay for the moderation record.
func (s *Service) adminRemoveMessage(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	staff := auth.MustFromContext(ctx)
	msgID, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req adminRemoveReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	req.Reason = strings.TrimSpace(req.Reason)
	if utf8.RuneCountInString(req.Reason) > 500 {
		return httpx.BadRequest("Keep the reason under 500 characters.")
	}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var convID, sender uuid.UUID
		err := tx.QueryRow(ctx, `
			SELECT conversation_id, sender_id FROM chat_messages WHERE id = $1 AND kind = 'text' FOR UPDATE`, msgID).
			Scan(&convID, &sender)
		if db.IsNoRows(err) {
			return errMessageNotFound
		}
		if err != nil {
			return err
		}
		hidden, err := hideMessage(ctx, tx, msgID)
		if err != nil {
			return err
		}
		return audit.Log(ctx, tx, staff.UserID, "chat.message.remove", "chat.message", msgID.String(), map[string]any{
			"conversation_id": convID, "sender_id": sender, "reason": req.Reason, "already_removed": !hidden,
		})
	})
	if err != nil {
		return err
	}
	if msg, err := s.message(ctx, s.d.Pool, msgID, false); err == nil {
		s.publishUpdated(ctx, msg, true)
	}
	httpx.NoContent(w)
	return nil
}
