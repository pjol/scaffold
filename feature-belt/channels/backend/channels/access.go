package channels

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/realtime"
)

// access is what the caller may do with one channel. loadAccess returns it
// only when they may read the channel at all.
type access struct {
	ChannelID uuid.UUID
	OrgID     uuid.UUID
	Kind      string
	Layout    string
	Name      string // channel name; "" for DMs
	IsDefault bool
	CreatedBy *uuid.UUID
	Archived  bool
	ForumTags []string
	OrgRole   string // the caller's org role
	Member    bool   // the caller is a channel member
	Role      string // the caller's channel role when a member
	UserID    uuid.UUID
}

func (a *access) direct() bool { return a.Kind == kindDM || a.Kind == kindGroupDM }

// loadAccess returns the caller's access to a channel: an org member, and a
// channel member unless the channel is public. Anything else is
// errChannelNotFound, so channel ids can't be probed.
func (s *Service) loadAccess(ctx context.Context, q db.Querier, channelID, userID uuid.UUID) (*access, error) {
	a := access{ChannelID: channelID, UserID: userID}
	var role *string
	err := q.QueryRow(ctx, `
		SELECT c.org_id, c.kind, c.layout, COALESCE(c.name, ''), c.is_default, c.created_by, c.archived_at IS NOT NULL, c.forum_tags,
		       om.role, cm.role
		FROM channels c
		JOIN org_members om ON om.org_id = c.org_id AND om.user_id = $2
		LEFT JOIN channel_members cm ON cm.channel_id = c.id AND cm.user_id = $2
		WHERE c.id = $1`, channelID, userID).
		Scan(&a.OrgID, &a.Kind, &a.Layout, &a.Name, &a.IsDefault, &a.CreatedBy, &a.Archived, &a.ForumTags, &a.OrgRole, &role)
	if db.IsNoRows(err) {
		return nil, errChannelNotFound
	}
	if err != nil {
		return nil, err
	}
	a.Member, a.Role = role != nil, deref(role)
	if !a.Member && a.Kind != kindPublic {
		return nil, errChannelNotFound
	}
	return &a, nil
}

// requireMember returns errNotMember unless the caller is in the channel.
func (a *access) requireMember() error {
	if !a.Member {
		return errNotMember
	}
	return nil
}

// requireWritable: a member of a channel that isn't archived.
func (a *access) requireWritable() error {
	if err := a.requireMember(); err != nil {
		return err
	}
	if a.Archived {
		return errArchived
	}
	return nil
}

// can asks the permission policy about this channel.
func (s *Service) can(ctx context.Context, q db.Querier, a *access, act Action) (bool, error) {
	var count int
	if act == ActMentionAll {
		if err := q.QueryRow(ctx, `SELECT count(*) FROM channel_members WHERE channel_id = $1`, a.ChannelID).Scan(&count); err != nil {
			return false, err
		}
	}
	return allowed(ctx, q, PermissionRequest{
		Action: act, OrgID: a.OrgID, UserID: a.UserID, OrgRole: a.OrgRole,
		Channel: &PermissionChannel{ID: a.ChannelID, Kind: a.Kind, IsDefault: a.IsDefault, CreatedBy: a.CreatedBy, MemberCount: count, Role: a.Role},
	})
}

// require refuses with 403 when the policy says no.
func (s *Service) require(ctx context.Context, q db.Querier, a *access, act Action, refusal string) error {
	ok, err := s.can(ctx, q, a, act)
	if err != nil {
		return err
	}
	if !ok {
		return httpx.Forbidden(refusal)
	}
	return nil
}

// messageAccess is a message with the caller's access to its channel.
type messageAccess struct {
	*access
	MessageID    uuid.UUID
	SenderID     *uuid.UUID
	Kind         string
	Body         string
	ThreadRootID *uuid.UUID
	Deleted      bool
	HasFiles     bool
	CreatedAt    time.Time
	Title        *string
}

// loadMessage reads a message the caller may read, or errMessageNotFound.
func (s *Service) loadMessage(ctx context.Context, q db.Querier, msgID, userID uuid.UUID) (*messageAccess, error) {
	var m messageAccess
	var channelID uuid.UUID
	err := q.QueryRow(ctx, `
		SELECT m.channel_id, m.sender_id, m.kind, m.body, m.thread_root_id, m.deleted_at IS NOT NULL, m.created_at, m.title,
		       EXISTS (SELECT 1 FROM channel_attachments a WHERE a.message_id = m.id)
		FROM channel_messages m WHERE m.id = $1`, msgID).
		Scan(&channelID, &m.SenderID, &m.Kind, &m.Body, &m.ThreadRootID, &m.Deleted, &m.CreatedAt, &m.Title, &m.HasFiles)
	if db.IsNoRows(err) {
		return nil, errMessageNotFound
	}
	if err != nil {
		return nil, err
	}
	a, err := s.loadAccess(ctx, q, channelID, userID)
	if err != nil {
		return nil, errMessageNotFound
	}
	m.access, m.MessageID = a, msgID
	return &m, nil
}

// ---- people who left the org ---------------------------------------------------------

// departedTopicCap bounds the public channels whose topics are revoked when
// someone leaves an org (most recently active first: the ones open in clients).
const departedTopicCap = 2000

// memberRemoved is channels' orgs.OnMemberRemoved hook (removed, left, org
// deleted, account deleted; after the commit). Access checks refuse the
// person at once; this removes their memberships and follows in the org's
// channels and drops their live subscriptions now: every channel they were in
// and every public channel they could read without joining. pruneDeparted is
// the backstop for a hook that never ran (a crash after the commit).
func (s *Service) memberRemoved(ctx context.Context, orgID, userID uuid.UUID) {
	var left []uuid.UUID
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		// Re-check membership in the statement: they may have rejoined since.
		rows, err := tx.Query(ctx, `
			DELETE FROM channel_members cm USING channels c
			WHERE c.id = cm.channel_id AND c.org_id = $1 AND cm.user_id = $2
			  AND NOT EXISTS (SELECT 1 FROM org_members om WHERE om.org_id = $1 AND om.user_id = $2)
			RETURNING cm.channel_id`, orgID, userID)
		if err != nil {
			return err
		}
		if left, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID]); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			DELETE FROM channel_thread_follows f USING channel_messages m, channels c
			WHERE m.id = f.root_id AND c.id = m.channel_id AND c.org_id = $1 AND f.user_id = $2
			  AND NOT EXISTS (SELECT 1 FROM org_members om WHERE om.org_id = $1 AND om.user_id = $2)`, orgID, userID)
		return err
	})
	if err != nil {
		slog.WarnContext(ctx, "channels: remove a leaver's memberships (pruned later)", "org_id", orgID, "err", err)
	}
	var stillMember bool
	if err := s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM org_members WHERE org_id = $1 AND user_id = $2)`,
		orgID, userID).Scan(&stillMember); err != nil || stillMember {
		return
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT c.id FROM channels c
		WHERE c.org_id = $1 AND c.kind = 'public'
		ORDER BY c.last_message_at DESC, c.id LIMIT $2`, orgID, departedTopicCap)
	var public []uuid.UUID
	if err == nil {
		public, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	}
	if err != nil {
		slog.WarnContext(ctx, "channels: list public channels for a leaver", "org_id", orgID, "err", err)
	}
	revoke := map[uuid.UUID]bool{}
	for _, id := range append(left, public...) {
		if revoke[id] {
			continue
		}
		revoke[id] = true
		if err := realtime.Revoke(ctx, s.d.Broker, userID, realtime.Topic(topicKind, id)); err != nil {
			slog.WarnContext(ctx, "channels: revoke a leaver's topic", "channel_id", id, "err", err)
		}
	}
	for _, id := range left {
		s.publish(ctx, id, EventChannel, ChannelEvent{ChannelID: id, Reason: "members"})
	}
}

// pruneDeparted removes channel memberships (and follows) of people no longer
// in the channel's org, and drops their live subscriptions: the backstop for
// memberRemoved. Access checks already refuse them; this keeps member lists
// honest and stops realtime delivery to sockets opened before they left.
func (s *Service) pruneDeparted(ctx context.Context) error {
	rows, err := s.d.Pool.Query(ctx, `
		DELETE FROM channel_members cm USING channels c
		WHERE c.id = cm.channel_id
		  AND NOT EXISTS (SELECT 1 FROM org_members om WHERE om.org_id = c.org_id AND om.user_id = cm.user_id)
		RETURNING cm.channel_id, cm.user_id`)
	if err != nil {
		return err
	}
	type gone struct{ channel, user uuid.UUID }
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (gone, error) {
		var g gone
		err := r.Scan(&g.channel, &g.user)
		return g, err
	})
	if err != nil {
		return err
	}
	if _, err := s.d.Pool.Exec(ctx, `
		DELETE FROM channel_thread_follows f USING channel_messages m, channels c
		WHERE m.id = f.root_id AND c.id = m.channel_id
		  AND NOT EXISTS (SELECT 1 FROM org_members om WHERE om.org_id = c.org_id AND om.user_id = f.user_id)`); err != nil {
		return err
	}
	for _, g := range list {
		if err := realtime.Revoke(ctx, s.d.Broker, g.user, realtime.Topic(topicKind, g.channel)); err != nil {
			slog.WarnContext(ctx, "channels: revoke departed", "channel_id", g.channel, "err", err)
		}
		s.publish(ctx, g.channel, EventChannel, ChannelEvent{ChannelID: g.channel, Reason: "members"})
	}
	return nil
}
