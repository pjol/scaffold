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

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/moderation"
	"scaffoldapp/backend/internal/storage"
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

const nilUUID = `'00000000-0000-0000-0000-000000000000'::uuid`

// ---- inbox -------------------------------------------------------------------

// GET /chat/conversations?archived=true&cursor=&limit= — newest activity first.
func (s *Service) listConversations(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context()).UserID
	p := httpx.ParsePage(r)
	var c cursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	items, err := s.inbox(r.Context(), me, r.URL.Query().Get("archived") == "true", c, p.Limit+1)
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(items, p.Limit, func(c Conversation) any { return cursor{c.LastMessageAt, c.ID} }))
	return nil
}

// inbox returns up to limit conversations after c. A direct conversation
// shows for the other person once it has a message, and disappears for
// someone who deleted it until a new message arrives.
func (s *Service) inbox(ctx context.Context, me uuid.UUID, archived bool, c cursor, limit int) ([]Conversation, error) {
	blocked, err := moderation.BlockedIDs(ctx, s.d.Pool, me)
	if err != nil {
		return nil, err
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT c.id
		FROM chat_members mb JOIN chat_conversations c ON c.id = mb.conversation_id
		WHERE mb.user_id = $1 AND mb.left_at IS NULL
		  AND (mb.archived_at IS NOT NULL) = $2
		  AND ($3::timestamptz IS NULL OR (c.last_message_at, c.id) < ($3, $4))
		  AND (c.kind = 'group' OR (c.created_by = $1 AND mb.cleared_at IS NULL) OR EXISTS (
		        SELECT 1 FROM chat_messages x WHERE x.conversation_id = c.id
		          AND (mb.cleared_at IS NULL OR x.created_at > mb.cleared_at)))
		ORDER BY c.last_message_at DESC, c.id DESC
		LIMIT $5`, me, archived, nullTime(c.T), c.ID, limit)
	if err != nil {
		return nil, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, err
	}
	return s.conversationsByID(ctx, s.d.Pool, me, ids, blocked)
}

// conversationsByID builds the caller's view of each conversation, in order:
// unread count, read pointers, last visible message, peer.
func (s *Service) conversationsByID(ctx context.Context, q db.Querier, me uuid.UUID, ids, blocked []uuid.UUID) ([]Conversation, error) {
	out := []Conversation{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := q.Query(ctx, `
		SELECT c.id, c.kind, c.title, c.avatar_key, c.user_low, c.user_high, c.created_at, c.last_message_at,
		       mb.role, mb.muted_until, mb.archived_at, mb.last_read_message_id, mb.last_read_at,
		       (SELECT count(*) FROM chat_members o WHERE o.conversation_id = c.id AND o.left_at IS NULL),
		       (SELECT count(*) FROM chat_messages u
		         WHERE u.conversation_id = c.id
		           AND (u.created_at, u.id) > (COALESCE(mb.last_read_at, '-infinity'), COALESCE(mb.last_read_message_id, `+nilUUID+`))
		           AND u.sender_id <> $1 AND u.kind = 'text' AND u.deleted_at IS NULL
		           AND (mb.cleared_at IS NULL OR u.created_at > mb.cleared_at)
		           AND u.sender_id <> ALL($3)),
		       rs.last_read_message_id, rs.last_read_at, lm.id
		FROM unnest($2::uuid[]) WITH ORDINALITY AS p(id, ord)
		JOIN chat_conversations c ON c.id = p.id
		JOIN chat_members mb ON mb.conversation_id = c.id AND mb.user_id = $1 AND mb.left_at IS NULL
		LEFT JOIN LATERAL (
		    SELECT o.last_read_message_id, o.last_read_at FROM chat_members o
		    WHERE o.conversation_id = c.id AND o.user_id <> $1 AND o.left_at IS NULL
		    ORDER BY o.last_read_at ASC NULLS FIRST, o.last_read_message_id ASC NULLS FIRST
		    LIMIT 1
		) rs ON true
		LEFT JOIN LATERAL (
		    SELECT x.id FROM chat_messages x
		    WHERE x.conversation_id = c.id
		      AND (mb.cleared_at IS NULL OR x.created_at > mb.cleared_at)
		      AND (x.kind = 'system' OR x.sender_id <> ALL($3))
		    ORDER BY x.created_at DESC, x.id DESC
		    LIMIT 1
		) lm ON true
		ORDER BY p.ord`, me, ids, blocked)
	if err != nil {
		return nil, err
	}
	type row struct {
		c                Conversation
		avatarKey        *string
		low, high        *uuid.UUID
		mutedUntil       *time.Time
		archivedAt       *time.Time
		readID, othersID *uuid.UUID
		readAt, othersAt *time.Time
		lastID           *uuid.UUID
	}
	var list []row
	for rows.Next() {
		var x row
		if err := rows.Scan(&x.c.ID, &x.c.Kind, &x.c.Title, &x.avatarKey, &x.low, &x.high, &x.c.CreatedAt, &x.c.LastMessageAt,
			&x.c.Role, &x.mutedUntil, &x.archivedAt, &x.readID, &x.readAt, &x.c.MemberCount, &x.c.Unread,
			&x.othersID, &x.othersAt, &x.lastID); err != nil {
			rows.Close()
			return nil, err
		}
		list = append(list, x)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var lastIDs, peerIDs []uuid.UUID
	for _, x := range list {
		if x.lastID != nil {
			lastIDs = append(lastIDs, *x.lastID)
		}
		if x.c.Kind == kindDirect && x.low != nil && x.high != nil {
			peerIDs = append(peerIDs, peerOf(me, *x.low, *x.high))
		}
	}
	last, err := s.messagesByID(ctx, q, lastIDs, false)
	if err != nil {
		return nil, err
	}
	peers, err := loadUsers(ctx, q, peerIDs)
	if err != nil {
		return nil, err
	}
	isBlocked := uuidSet(blocked)
	now := time.Now()
	for _, x := range list {
		c := x.c
		c.Archived = x.archivedAt != nil
		c.LastRead = readMark(x.readID, x.readAt)
		c.OthersRead = readMark(x.othersID, x.othersAt)
		if x.mutedUntil != nil && x.mutedUntil.After(now) {
			c.Muted = true
			if x.mutedUntil.Before(mutedForever) {
				c.MutedUntil = x.mutedUntil
			}
		}
		if x.lastID != nil {
			c.LastMessage = last[*x.lastID]
		}
		if c.Kind == kindDirect && x.low != nil && x.high != nil {
			pid := peerOf(me, *x.low, *x.high)
			peer := peers[pid]
			c.Peer, c.Title, c.AvatarURL, c.Blocked = &peer, peer.DisplayName, peer.AvatarURL, isBlocked[pid]
		} else if x.avatarKey != nil {
			u := s.d.Storage.PublicURL(*x.avatarKey)
			c.AvatarURL = &u
		}
		out = append(out, c)
	}
	return out, nil
}

func peerOf(me, low, high uuid.UUID) uuid.UUID {
	if low == me {
		return high
	}
	return low
}

// loadUsers returns UserRefs for ids (missing ids are left out).
func loadUsers(ctx context.Context, q db.Querier, ids []uuid.UUID) (map[uuid.UUID]UserRef, error) {
	out := map[uuid.UUID]UserRef{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := q.Query(ctx, `SELECT id, display_name, avatar_url, status FROM users WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var name, status string
		var avatar *string
		if err := rows.Scan(&id, &name, &avatar, &status); err != nil {
			return nil, err
		}
		out[id] = userRef(id, name, avatar, status)
	}
	return out, rows.Err()
}

// ---- one conversation ----------------------------------------------------------

func (s *Service) getConversation(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context()).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	if _, err := loadMember(r.Context(), s.d.Pool, id, me, false); err != nil {
		return err
	}
	d, err := s.detail(r.Context(), me, id)
	if err != nil {
		return err
	}
	httpx.OK(w, d)
	return nil
}

// detail is the caller's conversation view plus its active members.
func (s *Service) detail(ctx context.Context, me, id uuid.UUID) (*ConversationDetail, error) {
	blocked, err := moderation.BlockedIDs(ctx, s.d.Pool, me)
	if err != nil {
		return nil, err
	}
	convs, err := s.conversationsByID(ctx, s.d.Pool, me, []uuid.UUID{id}, blocked)
	if err != nil {
		return nil, err
	}
	if len(convs) == 0 {
		return nil, errConversationNotFound
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT mb.user_id, u.display_name, u.avatar_url, u.status, mb.role, mb.joined_at, mb.last_read_message_id, mb.last_read_at
		FROM chat_members mb JOIN users u ON u.id = mb.user_id
		WHERE mb.conversation_id = $1 AND mb.left_at IS NULL
		ORDER BY CASE mb.role WHEN 'owner' THEN 0 WHEN 'admin' THEN 1 ELSE 2 END, mb.joined_at, mb.user_id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	d := &ConversationDetail{Conversation: convs[0], Members: []Member{}, HiddenUserIDs: []uuid.UUID{}}
	hidden := uuidSet(blocked)
	for rows.Next() {
		var uid uuid.UUID
		var name, status string
		var avatar *string
		var m Member
		var readID *uuid.UUID
		var readAt *time.Time
		if err := rows.Scan(&uid, &name, &avatar, &status, &m.Role, &m.JoinedAt, &readID, &readAt); err != nil {
			return nil, err
		}
		m.User, m.LastRead = userRef(uid, name, avatar, status), readMark(readID, readAt)
		d.Members = append(d.Members, m)
		if hidden[uid] {
			d.HiddenUserIDs = append(d.HiddenUserIDs, uid)
		}
	}
	return d, rows.Err()
}

// ---- create ------------------------------------------------------------------

type createReq struct {
	Kind      string      `json:"kind"`       // direct | group
	UserID    *uuid.UUID  `json:"user_id"`    // direct: the other person
	Title     string      `json:"title"`      // group
	MemberIDs []uuid.UUID `json:"member_ids"` // group: everyone but the caller
	AvatarKey string      `json:"avatar_key"` // group, optional (purpose chat_avatar)
}

// POST /chat/conversations. A direct conversation is unique per pair: an
// existing one is returned (200) instead of a new one (201).
func (s *Service) createConversation(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context()).UserID
	var req createReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	var (
		id      uuid.UUID
		created bool
		err     error
	)
	switch req.Kind {
	case kindDirect:
		id, created, err = s.createDirect(r.Context(), me, req)
	case kindGroup:
		id, err = s.createGroup(r.Context(), me, req)
		created = err == nil
	default:
		return httpx.BadRequest("Kind must be direct or group.")
	}
	if err != nil {
		return err
	}
	d, err := s.detail(r.Context(), me, id)
	if err != nil {
		return err
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	httpx.JSON(w, status, d)
	return nil
}

func (s *Service) createDirect(ctx context.Context, me uuid.UUID, req createReq) (uuid.UUID, bool, error) {
	if req.UserID == nil {
		return uuid.Nil, false, httpx.BadRequest("Choose who to message.")
	}
	other := *req.UserID
	if other == me {
		return uuid.Nil, false, httpx.BadRequest("You can't start a conversation with yourself.")
	}
	low, high := orderedPair(me, other)
	var id uuid.UUID
	created := false
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if err := checkReachable(ctx, tx, me, other); err != nil {
			return err
		}
		find := `SELECT id FROM chat_conversations WHERE kind = 'direct' AND user_low = $1 AND user_high = $2`
		err := tx.QueryRow(ctx, find, low, high).Scan(&id)
		if db.IsNoRows(err) {
			if err := s.checkNewQuota(ctx, tx, me); err != nil {
				return err
			}
			err = tx.QueryRow(ctx, `
				INSERT INTO chat_conversations (kind, user_low, user_high, created_by)
				VALUES ('direct', $1, $2, $3)
				ON CONFLICT (user_low, user_high) WHERE kind = 'direct' DO NOTHING
				RETURNING id`, low, high, me).Scan(&id)
			switch {
			case err == nil:
				created = true
			case db.IsNoRows(err): // created concurrently by the other person
				err = tx.QueryRow(ctx, find, low, high).Scan(&id)
			}
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO chat_members (conversation_id, user_id, added_by) VALUES ($1, $2, $2), ($1, $3, $2)
			ON CONFLICT (conversation_id, user_id) DO NOTHING`, id, me, other); err != nil {
			return err
		}
		// Opening it again brings the caller's copy back from the archive.
		_, err = tx.Exec(ctx, `
			UPDATE chat_members SET archived_at = NULL, updated_at = now()
			WHERE conversation_id = $1 AND user_id = $2 AND archived_at IS NOT NULL`, id, me)
		return err
	})
	return id, created, err
}

func (s *Service) createGroup(ctx context.Context, me uuid.UUID, req createReq) (uuid.UUID, error) {
	title, err := cleanTitle(ctx, req.Title)
	if err != nil {
		return uuid.Nil, err
	}
	others := dedupe(req.MemberIDs, me)
	if len(others) == 0 {
		return uuid.Nil, httpx.BadRequest("Add at least 1 person.")
	}
	if len(others)+1 > s.cfg.groupMax {
		return uuid.Nil, httpx.BadRequest(fmt.Sprintf("Groups can have up to %d members.", s.cfg.groupMax))
	}
	var avatar *string
	if req.AvatarKey != "" {
		if !storage.KeyOwnedBy(req.AvatarKey, PurposeAvatar, me) {
			return uuid.Nil, httpx.BadRequest("Upload the picture first, then send its key.")
		}
		avatar = &req.AvatarKey
	}
	var id, created uuid.UUID
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if err := s.checkNewQuota(ctx, tx, me); err != nil {
			return err
		}
		if avatar != nil {
			if err := checkNotDeleted(ctx, tx, *avatar); err != nil {
				return err
			}
		}
		for _, uid := range others {
			if err := checkReachable(ctx, tx, me, uid); err != nil {
				return err
			}
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO chat_conversations (kind, title, avatar_key, created_by) VALUES ('group', $1, $2, $3)
			RETURNING id`, title, avatar, me).Scan(&id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO chat_members (conversation_id, user_id, role, added_by)
			SELECT $1, u, CASE WHEN u = $2 THEN 'owner' ELSE 'member' END, $2
			FROM unnest($3::uuid[]) AS u`, id, me, append([]uuid.UUID{me}, others...)); err != nil {
			return err
		}
		created, _, err = insertSystem(ctx, tx, id, me, "created", nil)
		return err
	})
	if err != nil {
		return uuid.Nil, err
	}
	s.announce(ctx, convHead{ID: id, Kind: kindGroup, Title: title}, created)
	for _, uid := range append([]uuid.UUID{me}, others...) {
		s.inboxEvent(ctx, uid, id, "added", nil)
	}
	return id, nil
}

// checkNewQuota enforces CHAT_NEW_CONVERSATIONS_PER_DAY (rolling 24 hours).
func (s *Service) checkNewQuota(ctx context.Context, tx pgx.Tx, me uuid.UUID) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('chat.new:' || $1::text, 0))`, me); err != nil {
		return err
	}
	var n int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM chat_conversations WHERE created_by = $1 AND created_at > now() - interval '24 hours'`,
		me).Scan(&n); err != nil {
		return err
	}
	if n >= s.cfg.newPerDay {
		return httpx.NewError(http.StatusTooManyRequests, "rate_limited",
			fmt.Sprintf("You can start %d conversations a day. Try again tomorrow.", s.cfg.newPerDay))
	}
	return nil
}

// checkReachable refuses people who are gone, blocked either way, or outside
// the app's ContactPolicy.
func checkReachable(ctx context.Context, q db.Querier, from, to uuid.UUID) error {
	var status string
	err := q.QueryRow(ctx, `SELECT status FROM users WHERE id = $1`, to).Scan(&status)
	if db.IsNoRows(err) || status == "deleted" || status == "banned" {
		return errPersonNotFound
	}
	if err != nil {
		return err
	}
	blocked, err := moderation.IsBlocked(ctx, q, from, to)
	if err != nil {
		return err
	}
	if blocked {
		return errBlocked
	}
	if contactPolicy != nil {
		return contactPolicy(ctx, q, from, to)
	}
	return nil
}

func cleanTitle(ctx context.Context, raw string) (string, error) {
	t := strings.Join(strings.Fields(raw), " ")
	if n := utf8.RuneCountInString(t); n == 0 || n > 80 {
		return "", httpx.BadRequest("Group names must be 1 to 80 characters.")
	}
	if err := moderation.Check(ctx, t); err != nil {
		return "", err
	}
	return t, nil
}

// checkNotDeleted refuses a picture key already queued for deletion.
func checkNotDeleted(ctx context.Context, q db.Querier, key string) error {
	var gone bool
	if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM chat_deleted_objects WHERE storage_key = $1)`, key).Scan(&gone); err != nil {
		return err
	}
	if gone {
		return httpx.Conflict("That picture was deleted. Upload it again.")
	}
	return nil
}

func dedupe(ids []uuid.UUID, except uuid.UUID) []uuid.UUID {
	seen := map[uuid.UUID]bool{except: true, uuid.Nil: true}
	out := []uuid.UUID{}
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// insertSystem records a membership or settings event and bumps the
// conversation's activity time. It returns the message id and time.
func insertSystem(ctx context.Context, tx pgx.Tx, convID, actor uuid.UUID, event string, subject *uuid.UUID) (uuid.UUID, time.Time, error) {
	var id uuid.UUID
	var at time.Time
	if err := tx.QueryRow(ctx, `
		INSERT INTO chat_messages (conversation_id, sender_id, kind, body, subject_user_id)
		VALUES ($1, $2, 'system', $3, $4) RETURNING id, created_at`, convID, actor, event, subject).Scan(&id, &at); err != nil {
		return uuid.Nil, time.Time{}, err
	}
	_, err := tx.Exec(ctx, `
		UPDATE chat_conversations SET last_message_at = GREATEST(last_message_at, $2), updated_at = now() WHERE id = $1`,
		convID, at)
	return id, at, err
}

// ---- group settings ------------------------------------------------------------

type updateReq struct {
	Title     *string `json:"title"`
	AvatarKey *string `json:"avatar_key"` // "" removes the picture
}

// PATCH /chat/conversations/{id} — group name and picture (owner or admin).
func (s *Service) updateConversation(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context()).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req updateReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	var title string
	if req.Title != nil {
		if title, err = cleanTitle(r.Context(), *req.Title); err != nil {
			return err
		}
	}
	if req.AvatarKey != nil && *req.AvatarKey != "" && !storage.KeyOwnedBy(*req.AvatarKey, PurposeAvatar, me) {
		return httpx.BadRequest("Upload the picture first, then send its key.")
	}
	var head convHead
	var events []uuid.UUID
	err = db.WithTx(r.Context(), s.d.Pool, func(tx pgx.Tx) error {
		mb, err := loadMember(r.Context(), tx, id, me, true)
		if err != nil {
			return err
		}
		if mb.Kind != kindGroup {
			return errNotGroup
		}
		if !mb.canManage() {
			return errNotGroupAdmin
		}
		head = mb.head()
		if req.Title != nil && title != mb.Title {
			if _, err := tx.Exec(r.Context(), `UPDATE chat_conversations SET title = $2, updated_at = now() WHERE id = $1`, id, title); err != nil {
				return err
			}
			ev, _, err := insertSystem(r.Context(), tx, id, me, "title_changed", nil)
			if err != nil {
				return err
			}
			events, head.Title = append(events, ev), title
		}
		if req.AvatarKey != nil && *req.AvatarKey != deref(mb.AvatarKey) {
			var next *string
			if *req.AvatarKey != "" {
				if err := checkNotDeleted(r.Context(), tx, *req.AvatarKey); err != nil {
					return err
				}
				next = req.AvatarKey
			}
			if mb.AvatarKey != nil {
				if err := queueObjects(r.Context(), tx, *mb.AvatarKey); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(r.Context(), `UPDATE chat_conversations SET avatar_key = $2, updated_at = now() WHERE id = $1`, id, next); err != nil {
				return err
			}
			ev, _, err := insertSystem(r.Context(), tx, id, me, "avatar_changed", nil)
			if err != nil {
				return err
			}
			events = append(events, ev)
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.announce(r.Context(), head, events...)
	s.kickObjectCleanup(r.Context())
	d, err := s.detail(r.Context(), me, id)
	if err != nil {
		return err
	}
	httpx.OK(w, d)
	return nil
}

// ---- the reader's own state ------------------------------------------------------

// DELETE /chat/conversations/{id}: leave a group; in a direct conversation,
// delete it for me (history so far hidden, back with the next message).
func (s *Service) leaveConversation(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context()).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	mb, err := loadMember(r.Context(), s.d.Pool, id, me, false)
	if err != nil {
		return err
	}
	if mb.Kind == kindGroup {
		if err := s.removeFromGroup(r.Context(), me, id, me); err != nil {
			return err
		}
		httpx.NoContent(w)
		return nil
	}
	if _, err := s.d.Pool.Exec(r.Context(), `
		UPDATE chat_members SET cleared_at = clock_timestamp(), archived_at = now(), updated_at = now()
		WHERE conversation_id = $1 AND user_id = $2 AND left_at IS NULL`, id, me); err != nil {
		return err
	}
	s.inboxEvent(r.Context(), me, id, "removed", nil)
	httpx.NoContent(w)
	return nil
}

// setOwnState runs an UPDATE on the caller's membership row and tells the
// caller's other devices.
func (s *Service) setOwnState(w http.ResponseWriter, r *http.Request, set string, args ...any) error {
	me := auth.MustFromContext(r.Context()).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	tag, err := s.d.Pool.Exec(r.Context(), `
		UPDATE chat_members SET `+set+`, updated_at = now()
		WHERE conversation_id = $1 AND user_id = $2 AND left_at IS NULL`, append([]any{id, me}, args...)...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errConversationNotFound
	}
	s.inboxEvent(r.Context(), me, id, "updated", nil)
	httpx.NoContent(w)
	return nil
}

func (s *Service) archive(w http.ResponseWriter, r *http.Request) error {
	return s.setOwnState(w, r, "archived_at = now()")
}

func (s *Service) unarchive(w http.ResponseWriter, r *http.Request) error {
	return s.setOwnState(w, r, "archived_at = NULL")
}

type muteReq struct {
	Until   *time.Time `json:"until"`
	Forever bool       `json:"forever"`
}

// PUT /chat/conversations/{id}/mute {until} or {forever: true}: no
// notifications until then. Unread counts still show in the conversation.
func (s *Service) mute(w http.ResponseWriter, r *http.Request) error {
	var req muteReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	until := mutedForever
	switch {
	case req.Forever && req.Until == nil:
	case !req.Forever && req.Until != nil:
		if !req.Until.After(time.Now()) {
			return httpx.BadRequest("Pick a time in the future.")
		}
		until = req.Until.UTC()
	default:
		return httpx.BadRequest("Send either until or forever.")
	}
	return s.setOwnState(w, r, "muted_until = $3", until)
}

func (s *Service) unmute(w http.ResponseWriter, r *http.Request) error {
	return s.setOwnState(w, r, "muted_until = NULL")
}

type readReq struct {
	MessageID *uuid.UUID `json:"message_id"` // newest message the client showed; omitted = newest in the conversation
}

// ReadEvent is published as chat.read.
type ReadEvent struct {
	ConversationID uuid.UUID `json:"conversation_id"`
	UserID         uuid.UUID `json:"user_id"`
	LastRead       ReadMark  `json:"last_read"`
}

// POST /chat/conversations/{id}/read — moves the read pointer forward only.
func (s *Service) markRead(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context()).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req readReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if _, err := loadMember(r.Context(), s.d.Pool, id, me, false); err != nil {
		return err
	}
	var mark ReadMark
	if req.MessageID != nil {
		err = s.d.Pool.QueryRow(r.Context(), `
			SELECT id, created_at FROM chat_messages WHERE id = $1 AND conversation_id = $2`, *req.MessageID, id).
			Scan(&mark.MessageID, &mark.At)
		if db.IsNoRows(err) {
			return errCursorScope
		}
	} else {
		err = s.d.Pool.QueryRow(r.Context(), `
			SELECT id, created_at FROM chat_messages WHERE conversation_id = $1
			ORDER BY created_at DESC, id DESC LIMIT 1`, id).Scan(&mark.MessageID, &mark.At)
		if db.IsNoRows(err) {
			httpx.NoContent(w)
			return nil
		}
	}
	if err != nil {
		return err
	}
	tag, err := s.d.Pool.Exec(r.Context(), `
		UPDATE chat_members SET last_read_message_id = $3, last_read_at = $4, updated_at = now()
		WHERE conversation_id = $1 AND user_id = $2 AND left_at IS NULL
		  AND (last_read_at IS NULL OR (last_read_at, COALESCE(last_read_message_id, `+nilUUID+`)) < ($4, $3))`,
		id, me, mark.MessageID, mark.At)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		s.publish(r.Context(), id, EventRead, ReadEvent{ConversationID: id, UserID: me, LastRead: mark})
		s.inboxEvent(r.Context(), me, id, "read", nil)
	}
	httpx.NoContent(w)
	return nil
}

// TypingEvent is published as chat.typing. Clients show it for about 6
// seconds; senders repeat it every 3 seconds while typing.
type TypingEvent struct {
	ConversationID uuid.UUID `json:"conversation_id"`
	User           UserRef   `json:"user"`
}

// POST /chat/conversations/{id}/typing — no database write.
func (s *Service) typing(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	convID, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	mb, err := loadMember(r.Context(), s.d.Pool, convID, id.UserID, false)
	if err != nil {
		return err
	}
	if mb.Kind == kindDirect {
		blocked, err := moderation.IsBlocked(r.Context(), s.d.Pool, id.UserID, mb.peer(id.UserID))
		if err != nil {
			return err
		}
		if blocked {
			httpx.NoContent(w)
			return nil
		}
	}
	s.publish(r.Context(), convID, EventTyping, TypingEvent{ConversationID: convID,
		User: userRef(id.UserID, id.DisplayName, nil, id.Status)})
	httpx.NoContent(w)
	return nil
}

// ---- unread, people, search -------------------------------------------------------

// Unread is GET /chat/unread: totals for the tab badge (muted and archived
// conversations excluded).
type Unread struct {
	Conversations int `json:"conversations"`
	Messages      int `json:"messages"`
}

func (s *Service) unread(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context()).UserID
	u, err := s.unreadTotals(r.Context(), me)
	if err != nil {
		return err
	}
	httpx.OK(w, u)
	return nil
}

func (s *Service) unreadTotals(ctx context.Context, me uuid.UUID) (Unread, error) {
	blocked, err := moderation.BlockedIDs(ctx, s.d.Pool, me)
	if err != nil {
		return Unread{}, err
	}
	var u Unread
	err = s.d.Pool.QueryRow(ctx, `
		SELECT count(DISTINCT x.conversation_id), count(x.id)
		FROM chat_members mb
		JOIN chat_messages x ON x.conversation_id = mb.conversation_id
		 AND (x.created_at, x.id) > (COALESCE(mb.last_read_at, '-infinity'), COALESCE(mb.last_read_message_id, `+nilUUID+`))
		WHERE mb.user_id = $1 AND mb.left_at IS NULL AND mb.archived_at IS NULL
		  AND (mb.muted_until IS NULL OR mb.muted_until <= now())
		  AND x.sender_id <> $1 AND x.kind = 'text' AND x.deleted_at IS NULL
		  AND (mb.cleared_at IS NULL OR x.created_at > mb.cleared_at)
		  AND x.sender_id <> ALL($2)`, me, blocked).Scan(&u.Conversations, &u.Messages)
	return u, err
}

// GET /chat/people?q= — people to start a conversation with: by name or
// exact email; without q, the people the caller talks to most recently.
func (s *Service) people(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context()).UserID
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	blocked, err := moderation.BlockedIDs(r.Context(), s.d.Pool, me)
	if err != nil {
		return err
	}
	var rows pgx.Rows
	switch {
	case q == "":
		rows, err = s.d.Pool.Query(r.Context(), `
			SELECT u.id, u.display_name, u.avatar_url, u.status
			FROM chat_members mine
			JOIN chat_members o ON o.conversation_id = mine.conversation_id AND o.user_id <> $1 AND o.left_at IS NULL
			JOIN chat_conversations c ON c.id = mine.conversation_id
			JOIN users u ON u.id = o.user_id
			WHERE mine.user_id = $1 AND mine.left_at IS NULL
			  AND u.status IN ('active', 'suspended') AND u.id <> ALL($2)
			GROUP BY u.id
			ORDER BY max(c.last_message_at) DESC, u.id
			LIMIT 20`, me, blocked)
	case utf8.RuneCountInString(q) < 2:
		return httpx.BadRequest("Type at least 2 characters.")
	default:
		like := escapeLike(strings.ToLower(q))
		rows, err = s.d.Pool.Query(r.Context(), `
			SELECT id, display_name, avatar_url, status FROM users
			WHERE status IN ('active', 'suspended') AND id <> $1 AND id <> ALL($2)
			  AND (lower(display_name) LIKE '%' || $3 || '%' ESCAPE '\' OR lower(email) = $4)
			ORDER BY (lower(display_name) LIKE ($3 || '%') ESCAPE '\') DESC, display_name, id
			LIMIT 20`, me, blocked, like, strings.ToLower(q))
	}
	if err != nil {
		return err
	}
	defer rows.Close()
	out := httpx.Page[UserRef]{Items: []UserRef{}}
	for rows.Next() {
		var id uuid.UUID
		var name, status string
		var avatar *string
		if err := rows.Scan(&id, &name, &avatar, &status); err != nil {
			return err
		}
		out.Items = append(out.Items, userRef(id, name, avatar, status))
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

// ConversationRef names a conversation in search results.
type ConversationRef struct {
	ID    uuid.UUID `json:"id"`
	Kind  string    `json:"kind"`
	Title string    `json:"title"`
}

// SearchHit is one matching message.
type SearchHit struct {
	Conversation ConversationRef `json:"conversation"`
	Message      *Message        `json:"message"`
}

// GET /chat/search?q=&conversation_id=&cursor=&limit= — the caller's own
// conversations only, newest first.
func (s *Service) search(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context()).UserID
	p := httpx.ParsePage(r)
	var c cursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	var convID *uuid.UUID
	if v := r.URL.Query().Get("conversation_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			return httpx.BadRequest("Invalid conversation_id.")
		}
		convID = &id
	}
	hits, err := s.searchMessages(r.Context(), me, r.URL.Query().Get("q"), convID, c, p.Limit+1)
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(hits, p.Limit, func(h SearchHit) any { return cursor{h.Message.CreatedAt, h.Message.ID} }))
	return nil
}

func (s *Service) searchMessages(ctx context.Context, me uuid.UUID, q string, convID *uuid.UUID, c cursor, limit int) ([]SearchHit, error) {
	q = strings.TrimSpace(q)
	if utf8.RuneCountInString(q) < 2 {
		return nil, httpx.BadRequest("Type at least 2 characters.")
	}
	if convID != nil {
		if _, err := loadMember(ctx, s.d.Pool, *convID, me, false); err != nil {
			return nil, err
		}
	}
	blocked, err := moderation.BlockedIDs(ctx, s.d.Pool, me)
	if err != nil {
		return nil, err
	}
	rows, err := s.d.Pool.Query(ctx, messageSelect+`
		JOIN chat_members mb ON mb.conversation_id = m.conversation_id AND mb.user_id = $1 AND mb.left_at IS NULL
		WHERE m.kind = 'text' AND m.deleted_at IS NULL
		  AND m.body ILIKE '%' || $2 || '%' ESCAPE '\'
		  AND (mb.cleared_at IS NULL OR m.created_at > mb.cleared_at)
		  AND m.sender_id <> ALL($3)
		  AND ($4::uuid IS NULL OR m.conversation_id = $4)
		  AND ($5::timestamptz IS NULL OR (m.created_at, m.id) < ($5, $6))
		ORDER BY m.created_at DESC, m.id DESC
		LIMIT $7`, me, escapeLike(q), blocked, convID, nullTime(c.T), c.ID, limit)
	if err != nil {
		return nil, err
	}
	msgs, err := scanMessages(rows, false)
	if err != nil {
		return nil, err
	}
	if err := s.hydrate(ctx, s.d.Pool, msgs, false); err != nil {
		return nil, err
	}
	ids := []uuid.UUID{}
	for _, m := range msgs {
		ids = append(ids, m.ConversationID)
	}
	refs, err := conversationRefs(ctx, s.d.Pool, me, ids)
	if err != nil {
		return nil, err
	}
	hits := make([]SearchHit, 0, len(msgs))
	for _, m := range msgs {
		hits = append(hits, SearchHit{Conversation: refs[m.ConversationID], Message: m})
	}
	return hits, nil
}

// conversationRefs names conversations for the caller (direct: the other person).
func conversationRefs(ctx context.Context, q db.Querier, me uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]ConversationRef, error) {
	out := map[uuid.UUID]ConversationRef{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := q.Query(ctx, `
		SELECT c.id, c.kind, c.title, u.id, u.display_name, u.status
		FROM chat_conversations c
		LEFT JOIN users u ON c.kind = 'direct' AND u.id = CASE WHEN c.user_low = $1 THEN c.user_high ELSE c.user_low END
		WHERE c.id = ANY($2)`, me, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ref ConversationRef
		var uid *uuid.UUID
		var name, status *string
		if err := rows.Scan(&ref.ID, &ref.Kind, &ref.Title, &uid, &name, &status); err != nil {
			return nil, err
		}
		if uid != nil {
			ref.Title = userRef(*uid, deref(name), nil, deref(status)).DisplayName
		}
		out[ref.ID] = ref
	}
	return out, rows.Err()
}
