package channels

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

// ---- moderation target ("channels.message") ------------------------------------------

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
	var channelID uuid.UUID
	var sender, root *uuid.UUID
	var body string
	var title *string
	err = t.s.d.Pool.QueryRow(ctx, `
		SELECT channel_id, sender_id, body, title, thread_root_id FROM channel_messages WHERE id = $1 AND kind = 'text'`, msgID).
		Scan(&channelID, &sender, &body, &title, &root)
	if err != nil {
		return moderation.Snapshot{}, err // pgx.ErrNoRows: moderation answers "no longer exists"
	}
	rows, err := t.s.d.Pool.Query(ctx, `SELECT storage_key FROM channel_attachments WHERE message_id = $1 ORDER BY position`, msgID)
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
	if title != nil {
		body = *title + "\n\n" + body
	}
	link := fmt.Sprintf("/channels/%s?message=%s", channelID, msgID)
	if root != nil {
		link = fmt.Sprintf("/channels/%s?thread=%s&message=%s", channelID, *root, msgID)
	}
	return moderation.Snapshot{AuthorID: sender, Text: body, MediaKeys: keys, Link: link}, nil
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
		UPDATE channel_messages SET deleted_at = NULL, deleted_by = NULL, updated_at = now() WHERE id = $1 AND deleted_by = 'staff'`, msgID)
	return err
}

func (messageTarget) exists(ctx context.Context, q db.Querier, id string) (uuid.UUID, error) {
	msgID, err := uuid.Parse(id)
	if err != nil {
		return uuid.Nil, httpx.ErrNotFound
	}
	var ok bool
	if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM channel_messages WHERE id = $1 AND kind = 'text')`, msgID).Scan(&ok); err != nil {
		return uuid.Nil, err
	}
	if !ok {
		return uuid.Nil, httpx.ErrNotFound
	}
	return msgID, nil
}

// ---- staff routes ------------------------------------------------------------------

// AdminChannel is a channel as staff see it for moderation context: its
// org, members and messages, with hidden content still readable. Each view
// is audited.
type AdminChannel struct {
	ID        uuid.UUID    `json:"id"`
	OrgID     uuid.UUID    `json:"org_id"`
	OrgName   string       `json:"org_name"`
	Kind      string       `json:"kind"`
	Layout    string       `json:"layout"`
	Name      string       `json:"name"`
	Archived  bool         `json:"archived"`
	CreatedAt time.Time    `json:"created_at"`
	Members   []UserRef    `json:"members"`
	Messages  *MessagePage `json:"messages"`
	Thread    *MessagePage `json:"thread"` // with ?thread=: that thread's replies
}

// GET /admin/channels/channels/{id}?around=|before=|after=&thread=<root id>
func (s *Service) adminChannel(w http.ResponseWriter, r *http.Request) error {
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
	out := AdminChannel{ID: id, Members: []UserRef{}}
	err = s.d.Pool.QueryRow(ctx, `
		SELECT c.org_id, o.name, c.kind, c.layout, COALESCE(c.name, ''), c.archived_at IS NOT NULL, c.created_at
		FROM channels c JOIN orgs o ON o.id = c.org_id WHERE c.id = $1`, id).
		Scan(&out.OrgID, &out.OrgName, &out.Kind, &out.Layout, &out.Name, &out.Archived, &out.CreatedAt)
	if db.IsNoRows(err) {
		return errChannelNotFound
	}
	if err != nil {
		return err
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT u.id, u.display_name, u.avatar_url, u.status FROM channel_members cm JOIN users u ON u.id = cm.user_id
		WHERE cm.channel_id = $1 ORDER BY lower(u.display_name), u.id LIMIT 200`, id)
	if err != nil {
		return err
	}
	for rows.Next() {
		var uid uuid.UUID
		var name, status string
		var avatar *string
		if err := rows.Scan(&uid, &name, &avatar, &status); err != nil {
			rows.Close()
			return err
		}
		out.Members = append(out.Members, userRef(uid, name, avatar, status))
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if out.Name == "" {
		names := make([]string, len(out.Members))
		for i, m := range out.Members {
			names[i] = m.DisplayName
		}
		out.Name = strings.Join(names, ", ")
	}
	win.channelID, win.staff = id, true
	var threadID *uuid.UUID
	if v := r.URL.Query().Get("thread"); v != "" {
		tid, err := uuid.Parse(v)
		if err != nil {
			return httpx.BadRequest("Invalid thread.")
		}
		threadID = &tid
		tw := win
		tw.rootID = &tid
		if out.Thread, err = s.pageMessages(ctx, s.d.Pool, tw, uuid.Nil); err != nil {
			return err
		}
		win.before, win.after, win.around = nil, nil, &tid
		markDeletedBy(out.Thread)
	}
	if out.Messages, err = s.pageMessages(ctx, s.d.Pool, win, uuid.Nil); err != nil {
		return err
	}
	markDeletedBy(out.Messages)
	if err := audit.Log(ctx, s.d.Pool, staff.UserID, "channels.channel.view", "channels.channel", id.String(),
		map[string]any{"before": win.before, "after": win.after, "around": win.around, "thread": threadID}); err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

func markDeletedBy(p *MessagePage) {
	if p == nil {
		return
	}
	for _, m := range p.Items {
		m.DeletedBy = m.deletedBy
	}
}

type adminRemoveReq struct {
	Reason string `json:"reason"`
}

// DELETE /admin/channels/messages/{id} {reason} — hides the message for
// members (a tombstone); the text and files stay for the moderation record.
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
	var a access
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var sender *uuid.UUID
		err := tx.QueryRow(ctx, `
			SELECT m.channel_id, c.org_id, c.kind, c.layout, COALESCE(c.name, ''), m.sender_id
			FROM channel_messages m JOIN channels c ON c.id = m.channel_id
			WHERE m.id = $1 AND m.kind <> 'system' FOR UPDATE OF m`, msgID).
			Scan(&a.ChannelID, &a.OrgID, &a.Kind, &a.Layout, &a.Name, &sender)
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
		return audit.Log(ctx, tx, staff.UserID, "channels.message.remove", "channels.message", msgID.String(), map[string]any{
			"channel_id": a.ChannelID, "org_id": a.OrgID, "sender_id": sender, "reason": req.Reason, "already_removed": !hidden,
		})
	})
	if err != nil {
		return err
	}
	if msg, err := s.message(ctx, s.d.Pool, msgID, uuid.Nil); err == nil {
		s.publishUpdated(ctx, &a, msg, true)
	}
	httpx.NoContent(w)
	return nil
}
