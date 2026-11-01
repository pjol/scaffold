package chat

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/realtime"
)

// ---- deletion ------------------------------------------------------------------

// DeleteAccount erases the person's part of every conversation: their
// messages become "Message deleted" tombstones (text and attachment rows gone;
// the files live under their storage prefix, which core deletes after the
// commit), their reactions go, groups they own pass to the longest-serving
// admin (else member), their memberships are removed, and conversations left
// without members are deleted. A direct partner keeps the conversation and
// sees "Deleted user".
func (s *Service) DeleteAccount(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	rows, err := tx.Query(ctx, `SELECT conversation_id, role = 'owner' AND left_at IS NULL FROM chat_members WHERE user_id = $1`, userID)
	if err != nil {
		return err
	}
	var convIDs, owned []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		var owner bool
		if err := rows.Scan(&id, &owner); err != nil {
			rows.Close()
			return err
		}
		convIDs = append(convIDs, id)
		if owner {
			owned = append(owned, id)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	s.deletedMembership.Store(userID, convIDs)

	if _, err := tx.Exec(ctx, `
		DELETE FROM chat_attachments a USING chat_messages m WHERE m.id = a.message_id AND m.sender_id = $1`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE chat_messages SET body = '', client_id = NULL, edited_at = NULL,
		  deleted_at = COALESCE(deleted_at, now()), deleted_by = COALESCE(deleted_by, 'account_deleted'), updated_at = now()
		WHERE sender_id = $1 AND kind = 'text'`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM chat_reactions WHERE user_id = $1`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE chat_conversations SET avatar_key = NULL, updated_at = now() WHERE avatar_key LIKE $1`,
		PurposeAvatar+"/"+userID.String()+"/%"); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM chat_members WHERE user_id = $1`, userID); err != nil {
		return err
	}
	if len(owned) > 0 {
		if _, err := tx.Exec(ctx, `
			UPDATE chat_members nm SET role = 'owner', updated_at = now()
			FROM (
			    SELECT DISTINCT ON (conversation_id) conversation_id, user_id
			    FROM chat_members WHERE conversation_id = ANY($1) AND left_at IS NULL
			    ORDER BY conversation_id, (role = 'admin') DESC, joined_at, user_id
			) pick
			WHERE nm.conversation_id = pick.conversation_id AND nm.user_id = pick.user_id`, owned); err != nil {
			return err
		}
	}
	rows, err = tx.Query(ctx, `
		SELECT c.id FROM chat_conversations c
		WHERE c.id = ANY($1)
		  AND NOT EXISTS (SELECT 1 FROM chat_members m WHERE m.conversation_id = c.id AND m.left_at IS NULL)`, convIDs)
	if err != nil {
		return err
	}
	empty, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return err
	}
	return deleteConversations(ctx, tx, empty)
}

// AccountDeleted runs after the deletion commits: it drops the person's open
// realtime subscriptions to the conversations they were in.
func (s *Service) AccountDeleted(ctx context.Context, userID uuid.UUID) error {
	v, ok := s.deletedMembership.LoadAndDelete(userID)
	if !ok {
		return nil
	}
	var lastErr error
	for _, id := range v.([]uuid.UUID) {
		if err := realtime.Revoke(ctx, s.d.Broker, userID, realtime.Topic(topicKind, id)); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

// ---- export ----------------------------------------------------------------------

type exportConversation struct {
	ID         uuid.UUID  `json:"id"`
	Kind       string     `json:"kind"`
	Title      string     `json:"title"`
	Role       string     `json:"role"`
	JoinedAt   time.Time  `json:"joined_at"`
	LeftAt     *time.Time `json:"left_at"`
	MutedUntil *time.Time `json:"muted_until"`
	ArchivedAt *time.Time `json:"archived_at"`
}

type exportAttachment struct {
	FileName    string `json:"file_name"`
	ContentType string `json:"content_type"`
	SizeBytes   *int64 `json:"size_bytes"`
	URL         string `json:"url"` // signed, valid for 24 hours
}

type exportMessage struct {
	ID             uuid.UUID          `json:"id"`
	ConversationID uuid.UUID          `json:"conversation_id"`
	Body           string             `json:"body"`
	ReplyToID      *uuid.UUID         `json:"reply_to_id"`
	Attachments    []exportAttachment `json:"attachments"`
	CreatedAt      time.Time          `json:"created_at"`
	EditedAt       *time.Time         `json:"edited_at"`
	DeletedAt      *time.Time         `json:"deleted_at"`
}

type exportReaction struct {
	MessageID uuid.UUID `json:"message_id"`
	Emoji     string    `json:"emoji"`
	CreatedAt time.Time `json:"created_at"`
}

type exportData struct {
	Conversations []exportConversation `json:"conversations"`
	Messages      []exportMessage      `json:"messages"`
	Reactions     []exportReaction     `json:"reactions"`
}

// ExportAccount returns the person's conversations (metadata), the messages
// they sent, and their reactions. Other people's messages are not included.
func (s *Service) ExportAccount(ctx context.Context, userID uuid.UUID) (any, error) {
	out := exportData{Conversations: []exportConversation{}, Messages: []exportMessage{}, Reactions: []exportReaction{}}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT c.id, c.kind, c.title, mb.role, mb.joined_at, mb.left_at, mb.muted_until, mb.archived_at
		FROM chat_members mb JOIN chat_conversations c ON c.id = mb.conversation_id
		WHERE mb.user_id = $1 ORDER BY mb.joined_at`, userID)
	if err != nil {
		return nil, err
	}
	if out.Conversations, err = pgx.CollectRows(rows, pgx.RowToStructByPos[exportConversation]); err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, len(out.Conversations))
	for i, c := range out.Conversations {
		ids[i] = c.ID
	}
	refs, err := conversationRefs(ctx, s.d.Pool, userID, ids)
	if err != nil {
		return nil, err
	}
	for i := range out.Conversations {
		out.Conversations[i].Title = refs[out.Conversations[i].ID].Title
	}

	rows, err = s.d.Pool.Query(ctx, `
		SELECT id, conversation_id, body, reply_to_id, created_at, edited_at, deleted_at
		FROM chat_messages WHERE sender_id = $1 AND kind = 'text' ORDER BY created_at, id`, userID)
	if err != nil {
		return nil, err
	}
	index := map[uuid.UUID]int{}
	for rows.Next() {
		var m exportMessage
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Body, &m.ReplyToID, &m.CreatedAt, &m.EditedAt, &m.DeletedAt); err != nil {
			rows.Close()
			return nil, err
		}
		m.Attachments = []exportAttachment{}
		index[m.ID] = len(out.Messages)
		out.Messages = append(out.Messages, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = s.d.Pool.Query(ctx, `
		SELECT a.message_id, a.storage_key, a.file_name, a.content_type, a.size_bytes
		FROM chat_attachments a JOIN chat_messages m ON m.id = a.message_id
		WHERE m.sender_id = $1 ORDER BY a.message_id, a.position`, userID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var msgID uuid.UUID
		var key string
		var a exportAttachment
		if err := rows.Scan(&msgID, &key, &a.FileName, &a.ContentType, &a.SizeBytes); err != nil {
			rows.Close()
			return nil, err
		}
		if a.URL, err = s.d.Storage.PresignGet(ctx, key, 24*time.Hour); err != nil {
			rows.Close()
			return nil, err
		}
		if i, ok := index[msgID]; ok {
			out.Messages[i].Attachments = append(out.Messages[i].Attachments, a)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = s.d.Pool.Query(ctx, `
		SELECT message_id, emoji, created_at FROM chat_reactions WHERE user_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	if out.Reactions, err = pgx.CollectRows(rows, pgx.RowToStructByPos[exportReaction]); err != nil {
		return nil, err
	}
	return out, nil
}

// ---- development data --------------------------------------------------------------

var seedNS = uuid.MustParse("6f1c2a0e-5d1b-4c8e-9a43-3f1f0c0c4a10")

func seedID(name string) uuid.UUID { return uuid.NewSHA1(seedNS, []byte("chat:"+name)) }

// SeedDev adds a direct conversation between Sam and Jordan and a group of
// three (Sam, Jordan, Avery) with a few messages. Rows have fixed ids, so re-running changes nothing.
func (s *Service) SeedDev(ctx context.Context, users []uuid.UUID) error {
	if len(users) < 4 {
		return nil
	}
	// Avery (users[0]) rather than Priya is in the group: the moderation
	// seed has Priya blocking Jordan, which would hide their messages from
	// each other.
	avery, sam, jordan := users[0], users[1], users[2]
	return db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		low, high := orderedPair(sam, jordan)
		var dm uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO chat_conversations (id, kind, user_low, user_high, created_by, created_at)
			VALUES ($1, 'direct', $2, $3, $4, now() - interval '1 hour')
			ON CONFLICT (user_low, user_high) WHERE kind = 'direct' DO UPDATE SET updated_at = chat_conversations.updated_at
			RETURNING id`, seedID("dm"), low, high, jordan).Scan(&dm); err != nil {
			return err
		}
		group := seedID("group")
		if _, err := tx.Exec(ctx, `
			INSERT INTO chat_conversations (id, kind, title, created_by, created_at)
			VALUES ($1, 'group', 'Weekend hike', $2, now() - interval '2 hours')
			ON CONFLICT (id) DO NOTHING`, group, sam); err != nil {
			return err
		}
		for _, m := range []struct {
			conv, user uuid.UUID
			role       string
		}{{dm, sam, roleMember}, {dm, jordan, roleMember}, {group, sam, roleOwner}, {group, jordan, roleAdmin}, {group, avery, roleMember}} {
			if _, err := tx.Exec(ctx, `
				INSERT INTO chat_members (conversation_id, user_id, role, added_by, joined_at)
				VALUES ($1, $2, $3, $2, now() - interval '2 hours')
				ON CONFLICT (conversation_id, user_id) DO NOTHING`, m.conv, m.user, m.role); err != nil {
				return err
			}
		}
		type seedMsg struct {
			key        string
			conv, from uuid.UUID
			kind, body string
			minsAgo    int
		}
		msgs := []seedMsg{
			{"dm-1", dm, jordan, kindText, "Are you still up for coffee on Saturday?", 50},
			{"dm-2", dm, sam, kindText, "Yes. 10am at the usual place?", 48},
			{"dm-3", dm, jordan, kindText, "Works for me.", 47},
			{"dm-4", dm, jordan, kindText, "Running 10 minutes late, save me a seat.", 5},
			{"group-0", group, sam, kindSystem, "created", 120},
			{"group-1", group, sam, kindText, "Trail options for Sunday: Dipsea or Tennessee Valley.", 118},
			{"group-2", group, avery, kindText, "Tennessee Valley. It's flatter and the beach at the end is worth it.", 90},
			{"group-3", group, jordan, kindText, "Agreed. I can drive 3 people.", 60},
			{"group-4", group, sam, kindText, "Tennessee Valley it is. Meet at the trailhead at 9.", 30},
		}
		for _, m := range msgs {
			if _, err := tx.Exec(ctx, `
				INSERT INTO chat_messages (id, conversation_id, sender_id, kind, body, created_at)
				VALUES ($1, $2, $3, $4, $5, now() - make_interval(mins => $6))
				ON CONFLICT (id) DO NOTHING`, seedID(m.key), m.conv, m.from, m.kind, m.body, m.minsAgo); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO chat_reactions (message_id, user_id, emoji) VALUES ($1, $2, '👍'), ($1, $3, '🌊')
			ON CONFLICT DO NOTHING`, seedID("group-4"), jordan, avery); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE chat_conversations c SET last_message_at = (SELECT max(created_at) FROM chat_messages WHERE conversation_id = c.id)
			WHERE c.id = ANY($1)`, []uuid.UUID{dm, group}); err != nil {
			return err
		}
		// Read pointers: Sam has 1 unread from Jordan; Jordan and Avery have
		// unread group messages.
		for _, rp := range []struct {
			conv, user uuid.UUID
			key        string
		}{{dm, sam, "dm-3"}, {dm, jordan, "dm-4"}, {group, sam, "group-4"}, {group, jordan, "group-3"}, {group, avery, "group-2"}} {
			if _, err := tx.Exec(ctx, `
				UPDATE chat_members mb SET last_read_message_id = m.id, last_read_at = m.created_at
				FROM chat_messages m
				WHERE m.id = $3 AND mb.conversation_id = $1 AND mb.user_id = $2 AND mb.last_read_message_id IS NULL`,
				rp.conv, rp.user, seedID(rp.key)); err != nil {
				return err
			}
		}
		return nil
	})
}
