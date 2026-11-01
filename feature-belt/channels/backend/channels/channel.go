package channels

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/moderation"
	"scaffoldapp/backend/internal/orgs"
	"scaffoldapp/backend/internal/realtime"
)

// ChannelPermissions are what the caller may do in one channel.
type ChannelPermissions struct {
	Post          bool `json:"post"` // member, channel not archived
	Edit          bool `json:"edit"`
	Archive       bool `json:"archive"`
	RemoveMembers bool `json:"remove_members"`
	MentionAll    bool `json:"mention_all"`
	SetDefault    bool `json:"set_default"`
}

// ChannelDetail is GET /channels/channels/{id}.
type ChannelDetail struct {
	SidebarChannel
	Description   string             `json:"description"`
	ForumTags     []string           `json:"forum_tags"`
	CreatedBy     *UserRef           `json:"created_by"`
	CreatedAt     time.Time          `json:"created_at"`
	Member        bool               `json:"member"` // false: previewing a public channel
	Role          string             `json:"role"`   // admin | member | ""
	PinnedCount   int                `json:"pinned_count"`
	Permissions   ChannelPermissions `json:"permissions"`
	HiddenUserIDs []uuid.UUID        `json:"hidden_user_ids"` // a block either way: drop their live events
}

func (s *Service) getChannel(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context()).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	d, err := s.detail(r.Context(), me, id)
	if err != nil {
		return err
	}
	httpx.OK(w, d)
	return nil
}

// detail is the caller's view of a channel they may read.
func (s *Service) detail(ctx context.Context, me, id uuid.UUID) (*ChannelDetail, error) {
	a, err := s.loadAccess(ctx, s.d.Pool, id, me)
	if err != nil {
		return nil, err
	}
	d := &ChannelDetail{Member: a.Member, Role: a.Role, ForumTags: a.ForumTags, HiddenUserIDs: []uuid.UUID{}}
	if d.ForumTags == nil {
		d.ForumTags = []string{}
	}
	if a.Member {
		list, err := s.sidebar(ctx, me, a.OrgID, []uuid.UUID{id})
		if err != nil {
			return nil, err
		}
		if len(list) == 1 {
			d.SidebarChannel = list[0]
		}
	}
	var createdBy *uuid.UUID
	if d.ID == uuid.Nil { // previewing, archived, or a DM not in the sidebar
		d.SidebarChannel = SidebarChannel{ID: id, OrgID: a.OrgID, Kind: a.Kind, Layout: a.Layout, Name: a.Name, People: []UserRef{}, Notify: "default"}
		if err := s.d.Pool.QueryRow(ctx, `
			SELECT c.topic, c.is_default, c.archived_at IS NOT NULL, c.last_message_at,
			       (SELECT count(*) FROM channel_members x WHERE x.channel_id = c.id)::int,
			       COALESCE(cm.notify, 'default'), COALESCE(cm.muted, false), cm.starred_at IS NOT NULL, cm.last_read_at
			FROM channels c LEFT JOIN channel_members cm ON cm.channel_id = c.id AND cm.user_id = $2 WHERE c.id = $1`, id, me).
			Scan(&d.Topic, &d.IsDefault, &d.Archived, &d.LastMessageAt, &d.MemberCount, &d.Notify, &d.Muted, &d.Starred, &d.LastReadAt); err != nil {
			return nil, err
		}
		if a.direct() {
			people, err := s.dmPeople(ctx, s.d.Pool, me, []uuid.UUID{id})
			if err != nil {
				return nil, err
			}
			d.People, d.Name = people[id], dmTitle(people[id])
		}
	}
	if err := s.d.Pool.QueryRow(ctx, `
		SELECT description, created_by, created_at,
		       (SELECT count(*) FROM channel_messages m WHERE m.channel_id = c.id AND m.pinned_at IS NOT NULL AND m.deleted_at IS NULL)::int
		FROM channels c WHERE c.id = $1`, id).Scan(&d.Description, &createdBy, &d.CreatedAt, &d.PinnedCount); err != nil {
		return nil, err
	}
	if createdBy != nil {
		users, err := loadUsers(ctx, s.d.Pool, []uuid.UUID{*createdBy})
		if err != nil {
			return nil, err
		}
		if u, ok := users[*createdBy]; ok {
			d.CreatedBy = &u
		}
	}
	d.Permissions.Post = a.Member && !a.Archived
	for act, dst := range map[Action]*bool{ActEdit: &d.Permissions.Edit, ActArchive: &d.Permissions.Archive,
		ActRemoveMember: &d.Permissions.RemoveMembers, ActMentionAll: &d.Permissions.MentionAll} {
		if *dst, err = s.can(ctx, s.d.Pool, a, act); err != nil {
			return nil, err
		}
	}
	if a.Kind == kindPublic {
		if d.Permissions.SetDefault, err = allowed(ctx, s.d.Pool, PermissionRequest{Action: ActSetDefault, OrgID: a.OrgID, UserID: me, OrgRole: a.OrgRole}); err != nil {
			return nil, err
		}
	}
	blocked, err := moderation.BlockedIDs(ctx, s.d.Pool, me)
	if err != nil {
		return nil, err
	}
	if len(blocked) > 0 {
		rows, err := s.d.Pool.Query(ctx, `SELECT user_id FROM channel_members WHERE channel_id = $1 AND user_id = ANY($2)`, id, blocked)
		if err != nil {
			return nil, err
		}
		if d.HiddenUserIDs, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID]); err != nil {
			return nil, err
		}
		if d.HiddenUserIDs == nil {
			d.HiddenUserIDs = []uuid.UUID{}
		}
	}
	return d, nil
}

// ---- settings --------------------------------------------------------------------------

type updateReq struct {
	Name        *string   `json:"name"`
	Topic       *string   `json:"topic"`
	Description *string   `json:"description"`
	ForumTags   *[]string `json:"forum_tags"`
	Kind        *string   `json:"kind"`       // "private" converts a public channel (one way)
	IsDefault   *bool     `json:"is_default"` // public channels: everyone joins it once
}

// PATCH /channels/channels/{id}. Any member sets the topic; the rest needs
// ActEdit (default: org admins, the channel's admins).
func (s *Service) updateChannel(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req updateReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	a, err := s.loadAccess(ctx, s.d.Pool, id, me)
	if err != nil {
		return err
	}
	if err := a.requireWritable(); err != nil {
		return err
	}
	if a.direct() && (req.Name != nil || req.Description != nil || req.ForumTags != nil || req.Kind != nil || req.IsDefault != nil) {
		return httpx.BadRequest("Direct messages only have a topic.")
	}
	if req.Name != nil || req.Description != nil || req.ForumTags != nil || req.Kind != nil {
		if err := s.require(ctx, s.d.Pool, a, ActEdit, "Only the channel's admins and the "+orgs.Noun()+"'s admins can change that."); err != nil {
			return err
		}
	}
	var events []uuid.UUID
	sets, args := []string{}, []any{id}
	add := func(col string, v any) {
		args = append(args, v)
		sets = append(sets, col+" = $"+strconv.Itoa(len(args)))
	}
	if req.Name != nil {
		name, err := cleanName(*req.Name)
		if err != nil {
			return err
		}
		if err := checkText(ctx, name); err != nil {
			return err
		}
		if name != a.Name {
			add("name", name)
		}
	}
	if req.Topic != nil {
		t := strings.TrimSpace(*req.Topic)
		if utf8.RuneCountInString(t) > 250 {
			return httpx.BadRequest("Topics can be up to 250 characters.")
		}
		if err := checkText(ctx, t); err != nil {
			return err
		}
		add("topic", t)
	}
	if req.Description != nil {
		d := strings.TrimSpace(*req.Description)
		if utf8.RuneCountInString(d) > 1000 {
			return httpx.BadRequest("Descriptions can be up to 1000 characters.")
		}
		if err := checkText(ctx, d); err != nil {
			return err
		}
		add("description", d)
	}
	if req.ForumTags != nil {
		tags, err := cleanTags(*req.ForumTags)
		if err != nil {
			return err
		}
		if err := checkText(ctx, tags...); err != nil {
			return err
		}
		add("forum_tags", tags)
	}
	if req.Kind != nil && *req.Kind != a.Kind {
		if *req.Kind != kindPrivate || a.Kind != kindPublic {
			return httpx.BadRequest("A public channel can be made private; a private one can't be made public.")
		}
		if a.IsDefault {
			return httpx.BadRequest("Default channels stay public.")
		}
		add("kind", kindPrivate)
	}
	if req.IsDefault != nil && *req.IsDefault != a.IsDefault {
		if a.Kind != kindPublic || (req.Kind != nil && *req.Kind == kindPrivate) {
			return httpx.BadRequest("Only public channels can be default channels.")
		}
		ok, err := allowed(ctx, s.d.Pool, PermissionRequest{Action: ActSetDefault, OrgID: a.OrgID, UserID: me, OrgRole: a.OrgRole})
		if err != nil {
			return err
		}
		if !ok {
			return httpx.Forbidden("Only owners and admins choose default channels.")
		}
		add("is_default", *req.IsDefault)
	}
	if len(sets) > 0 {
		err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE channels SET `+strings.Join(sets, ", ")+`, updated_at = now() WHERE id = $1`, args...)
			if db.IsUniqueViolation(err, "channels_name_uidx") {
				return httpx.Conflict("That name is taken. Choose another.")
			}
			if err != nil {
				return err
			}
			for _, ev := range []struct {
				changed bool
				code    string
			}{{req.Name != nil && slicesContains(sets, "name"), "renamed"}, {req.Topic != nil, "topic"}, {req.Kind != nil && slicesContains(sets, "kind"), "converted"}} {
				if ev.changed {
					mid, err := insertSystem(ctx, tx, id, me, ev.code, nil)
					if err != nil {
						return err
					}
					events = append(events, mid)
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
		s.announce(ctx, id, events...)
		s.publish(ctx, id, EventChannel, ChannelEvent{ChannelID: id, Reason: "updated"})
		s.sidebarAll(ctx, id, "updated")
		if req.Kind != nil && slicesContains(sets, "kind") {
			s.revokeNonMembers(ctx, id)
		}
	}
	d, err := s.detail(ctx, me, id)
	if err != nil {
		return err
	}
	httpx.OK(w, d)
	return nil
}

func slicesContains(sets []string, col string) bool {
	for _, s := range sets {
		if strings.HasPrefix(s, col+" =") {
			return true
		}
	}
	return false
}

// revokeNonMembers drops subscriptions of org members who were previewing a
// channel that just became private. The hub re-checks each one.
func (s *Service) revokeNonMembers(ctx context.Context, channelID uuid.UUID) {
	rows, err := s.d.Pool.Query(ctx, `
		SELECT om.user_id FROM channels c JOIN org_members om ON om.org_id = c.org_id
		WHERE c.id = $1 AND NOT EXISTS (SELECT 1 FROM channel_members x WHERE x.channel_id = c.id AND x.user_id = om.user_id)`, channelID)
	if err != nil {
		slog.WarnContext(ctx, "channels: list previewers", "err", err)
		return
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return
	}
	for _, uid := range ids {
		_ = realtime.Revoke(context.WithoutCancel(ctx), s.d.Broker, uid, realtime.Topic(topicKind, channelID))
	}
}

// POST /channels/channels/{id}/archive
func (s *Service) archive(w http.ResponseWriter, r *http.Request) error {
	return s.setArchived(w, r, true)
}

// DELETE /channels/channels/{id}/archive
func (s *Service) unarchive(w http.ResponseWriter, r *http.Request) error {
	return s.setArchived(w, r, false)
}

func (s *Service) setArchived(w http.ResponseWriter, r *http.Request, archived bool) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	a, err := s.loadAccess(ctx, s.d.Pool, id, me)
	if err != nil {
		return err
	}
	if a.direct() {
		return httpx.BadRequest("Direct messages can't be archived. Close them from the sidebar.")
	}
	if err := s.require(ctx, s.d.Pool, a, ActArchive, "Only the channel's admins and the "+orgs.Noun()+"'s admins can archive it."); err != nil {
		return err
	}
	if a.Archived == archived {
		return s.respondDetail(w, r, me, id)
	}
	var ev uuid.UUID
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if archived {
			if _, err := tx.Exec(ctx, `UPDATE channels SET archived_at = now(), archived_by = $2, is_default = false, updated_at = now() WHERE id = $1`, id, me); err != nil {
				return err
			}
			ev, err = insertSystem(ctx, tx, id, me, "archived", nil)
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE channels SET archived_at = NULL, archived_by = NULL, updated_at = now() WHERE id = $1`, id); err != nil {
			return err
		}
		ev, err = insertSystem(ctx, tx, id, me, "unarchived", nil)
		return err
	})
	if err != nil {
		return err
	}
	s.announce(ctx, id, ev)
	s.publish(ctx, id, EventChannel, ChannelEvent{ChannelID: id, Reason: "updated"})
	s.sidebarAll(ctx, id, "updated")
	return s.respondDetail(w, r, me, id)
}

func (s *Service) respondDetail(w http.ResponseWriter, r *http.Request, me, id uuid.UUID) error {
	d, err := s.detail(r.Context(), me, id)
	if err != nil {
		return err
	}
	httpx.OK(w, d)
	return nil
}

// ---- membership -------------------------------------------------------------------------

// POST /channels/channels/{id}/join — public channels.
func (s *Service) join(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	a, err := s.loadAccess(ctx, s.d.Pool, id, me)
	if err != nil {
		return err
	}
	if a.Member {
		return s.respondDetail(w, r, me, id)
	}
	if a.Archived {
		return errArchived
	}
	var ev uuid.UUID
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if err := joinChannel(ctx, tx, id, me, &me, roleMember); err != nil {
			return err
		}
		ev, err = insertSystem(ctx, tx, id, me, "joined", nil)
		return err
	})
	if err != nil {
		return err
	}
	s.announce(ctx, id, ev)
	s.publish(ctx, id, EventChannel, ChannelEvent{ChannelID: id, Reason: "members", UserID: &me})
	s.sidebarEvent(ctx, me, a.OrgID, id, "added", nil, false)
	return s.respondDetail(w, r, me, id)
}

// DELETE /channels/channels/{id}/membership — leave a channel, or close a
// DM in your sidebar (it comes back with the next message).
func (s *Service) leave(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	a, err := s.loadAccess(ctx, s.d.Pool, id, me)
	if err != nil {
		return err
	}
	if err := a.requireMember(); err != nil {
		return err
	}
	if a.direct() {
		if _, err := s.d.Pool.Exec(ctx, `UPDATE channel_members SET hidden_at = now(), updated_at = now() WHERE channel_id = $1 AND user_id = $2`, id, me); err != nil {
			return err
		}
		s.sidebarEvent(ctx, me, a.OrgID, id, "removed", nil, false)
		httpx.NoContent(w)
		return nil
	}
	return s.removeFromChannel(w, r, a, me, me)
}

func (s *Service) removeFromChannel(w http.ResponseWriter, r *http.Request, a *access, actor, target uuid.UUID) error {
	ctx := r.Context()
	var ev uuid.UUID
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM channel_members WHERE channel_id = $1 AND user_id = $2`, a.ChannelID, target)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.NotFound("That person isn't in this channel.")
		}
		if _, err := tx.Exec(ctx, `
			UPDATE channel_thread_follows f SET following = false, updated_at = now()
			FROM channel_messages m WHERE m.id = f.root_id AND m.channel_id = $1 AND f.user_id = $2`, a.ChannelID, target); err != nil {
			return err
		}
		code := "removed"
		if actor == target {
			code = "left"
		}
		ev, err = insertSystem(ctx, tx, a.ChannelID, actor, code, &target)
		return err
	})
	if err != nil {
		return err
	}
	bg := context.WithoutCancel(ctx)
	if a.Kind != kindPublic {
		if err := realtime.Revoke(bg, s.d.Broker, target, realtime.Topic(topicKind, a.ChannelID)); err != nil {
			slog.WarnContext(ctx, "channels: revoke", "channel_id", a.ChannelID, "err", err)
		}
	}
	s.announce(bg, a.ChannelID, ev)
	s.publish(bg, a.ChannelID, EventChannel, ChannelEvent{ChannelID: a.ChannelID, Reason: "members", UserID: &target})
	s.sidebarEvent(bg, target, a.OrgID, a.ChannelID, "removed", nil, false)
	httpx.NoContent(w)
	return nil
}

type addMembersReq struct {
	UserIDs []uuid.UUID `json:"user_ids"`
}

// POST /channels/channels/{id}/members — any member adds org members.
func (s *Service) addMembers(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req addMembersReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	ids := dedupe(req.UserIDs, me)
	if len(ids) == 0 || len(ids) > 500 {
		return httpx.BadRequest("Choose 1 to 500 people.")
	}
	a, err := s.loadAccess(ctx, s.d.Pool, id, me)
	if err != nil {
		return err
	}
	if err := a.requireWritable(); err != nil {
		return err
	}
	if a.direct() {
		return httpx.BadRequest("People can't be added to a direct message. Start a new one with everyone.")
	}
	var added, events []uuid.UUID
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if err := requireOrgMembers(ctx, tx, a.OrgID, ids); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT u FROM unnest($2::uuid[]) u WHERE NOT EXISTS (SELECT 1 FROM channel_members x WHERE x.channel_id = $1 AND x.user_id = u)`, id, ids)
		if err != nil {
			return err
		}
		if added, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID]); err != nil {
			return err
		}
		for _, uid := range added {
			if err := joinChannel(ctx, tx, id, uid, &me, roleMember); err != nil {
				return err
			}
			ev, err := insertSystem(ctx, tx, id, me, "added", &uid)
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
	s.announce(ctx, id, events...)
	for _, uid := range added {
		u := uid
		s.publish(ctx, id, EventChannel, ChannelEvent{ChannelID: id, Reason: "members", UserID: &u})
		s.sidebarEvent(ctx, uid, a.OrgID, id, "added", nil, false)
	}
	return s.respondDetail(w, r, me, id)
}

// DELETE /channels/channels/{id}/members/{userID} — ActRemoveMember, or
// yourself (leave).
func (s *Service) removeMember(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	target, err := httpx.URLParamUUID(r, "userID")
	if err != nil {
		return err
	}
	a, err := s.loadAccess(ctx, s.d.Pool, id, me)
	if err != nil {
		return err
	}
	if target == me {
		return s.leave(w, r)
	}
	if err := a.requireMember(); err != nil {
		return err
	}
	if a.direct() {
		return httpx.BadRequest("People can't be removed from a direct message.")
	}
	if err := s.require(ctx, s.d.Pool, a, ActRemoveMember, "Only the channel's admins and the "+orgs.Noun()+"'s admins can remove people."); err != nil {
		return err
	}
	return s.removeFromChannel(w, r, a, me, target)
}

// ChannelMember is one person in a channel's member list.
type ChannelMember struct {
	User     UserRef   `json:"user"`
	Role     string    `json:"role"`
	JoinedAt time.Time `json:"joined_at"`
}

// GET /channels/channels/{id}/members?q=&cursor= — by name.
func (s *Service) listMembers(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	if _, err := s.loadAccess(ctx, s.d.Pool, id, me); err != nil {
		return err
	}
	p := httpx.ParsePage(r)
	var c struct {
		N  string    `json:"n"`
		ID uuid.UUID `json:"id"`
	}
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	rows, err := s.d.Pool.Query(ctx, `
		SELECT u.id, u.display_name, u.avatar_url, u.status, cm.role, cm.joined_at, lower(u.display_name)
		FROM channel_members cm JOIN users u ON u.id = cm.user_id
		WHERE cm.channel_id = $1
		  AND ($2 = '' OR lower(u.display_name) LIKE '%' || $2 || '%' ESCAPE '\')
		  AND ($3 = '' AND $4::uuid = `+nilUUID+` OR (lower(u.display_name), u.id) > ($3, $4))
		ORDER BY lower(u.display_name), u.id LIMIT $5`, id, escapeLike(q), c.N, c.ID, p.Limit+1)
	if err != nil {
		return err
	}
	defer rows.Close()
	type keyed struct {
		ChannelMember
		key string
	}
	var list []keyed
	for rows.Next() {
		var k keyed
		var uid uuid.UUID
		var name, status string
		var avatar *string
		if err := rows.Scan(&uid, &name, &avatar, &status, &k.Role, &k.JoinedAt, &k.key); err != nil {
			return err
		}
		k.User = userRef(uid, name, avatar, status)
		list = append(list, k)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	page := httpx.NewPage(list, p.Limit, func(k keyed) any { return map[string]any{"n": k.key, "id": k.User.ID} })
	out := httpx.Page[ChannelMember]{Items: make([]ChannelMember, len(page.Items)), NextCursor: page.NextCursor}
	for i, k := range page.Items {
		out.Items[i] = k.ChannelMember
	}
	httpx.OK(w, out)
	return nil
}

// ---- the reader's own state ----------------------------------------------------------------

type prefsReq struct {
	Notify  *string `json:"notify"` // default | all | mentions | nothing
	Muted   *bool   `json:"muted"`
	Starred *bool   `json:"starred"`
}

// PUT /channels/channels/{id}/prefs
func (s *Service) setPrefs(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req prefsReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.Notify != nil {
		switch *req.Notify {
		case "default", "all", "mentions", "nothing":
		default:
			return httpx.BadRequest("notify must be default, all, mentions or nothing.")
		}
	}
	a, err := s.loadAccess(ctx, s.d.Pool, id, me)
	if err != nil {
		return err
	}
	if err := a.requireMember(); err != nil {
		return err
	}
	if _, err := s.d.Pool.Exec(ctx, `
		UPDATE channel_members SET notify = COALESCE($3, notify), muted = COALESCE($4, muted),
		  starred_at = CASE WHEN $5::boolean IS NULL THEN starred_at WHEN $5 THEN COALESCE(starred_at, now()) ELSE NULL END,
		  updated_at = now()
		WHERE channel_id = $1 AND user_id = $2`, id, me, req.Notify, req.Muted, req.Starred); err != nil {
		return err
	}
	s.sidebarEvent(ctx, me, a.OrgID, id, "updated", nil, false)
	return s.respondDetail(w, r, me, id)
}

type readReq struct {
	MessageID *uuid.UUID `json:"message_id"` // newest message the client showed; omitted = the newest
}

// POST /channels/channels/{id}/read — moves the timeline read pointer forward.
func (s *Service) markRead(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req readReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	a, err := s.loadAccess(ctx, s.d.Pool, id, me)
	if err != nil {
		return err
	}
	if !a.Member {
		httpx.NoContent(w) // previewing: nothing to mark
		return nil
	}
	var msgID uuid.UUID
	var at time.Time
	if req.MessageID != nil {
		err = s.d.Pool.QueryRow(ctx, `SELECT id, created_at FROM channel_messages WHERE id = $1 AND channel_id = $2`, *req.MessageID, id).Scan(&msgID, &at)
		if db.IsNoRows(err) {
			return errCursorScope
		}
	} else {
		err = s.d.Pool.QueryRow(ctx, `
			SELECT id, created_at FROM channel_messages WHERE channel_id = $1 AND (thread_root_id IS NULL OR also_in_channel)
			ORDER BY created_at DESC, id DESC LIMIT 1`, id).Scan(&msgID, &at)
		if db.IsNoRows(err) {
			httpx.NoContent(w)
			return nil
		}
	}
	if err != nil {
		return err
	}
	tag, err := s.d.Pool.Exec(ctx, `
		UPDATE channel_members SET last_read_message_id = $3, last_read_at = $4, updated_at = now()
		WHERE channel_id = $1 AND user_id = $2
		  AND (last_read_at IS NULL OR (last_read_at, COALESCE(last_read_message_id, `+nilUUID+`)) < ($4, $3))`, id, me, msgID, at)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		s.sidebarEvent(ctx, me, a.OrgID, id, "read", nil, false)
	}
	httpx.NoContent(w)
	return nil
}

type typingReq struct {
	ThreadRootID *uuid.UUID `json:"thread_root_id"`
}

// TypingEvent is published as channels.typing. Clients show it for about 6
// seconds; senders repeat it every 3 seconds while typing.
type TypingEvent struct {
	ChannelID    uuid.UUID  `json:"channel_id"`
	ThreadRootID *uuid.UUID `json:"thread_root_id"`
	User         UserRef    `json:"user"`
}

// POST /channels/channels/{id}/typing — no database write.
func (s *Service) typing(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id := auth.MustFromContext(ctx)
	channelID, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req typingReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	a, err := s.loadAccess(ctx, s.d.Pool, channelID, id.UserID)
	if err != nil {
		return err
	}
	if err := a.requireWritable(); err != nil {
		return err
	}
	s.publish(ctx, channelID, EventTyping, TypingEvent{ChannelID: channelID, ThreadRootID: req.ThreadRootID, User: userRef(id.UserID, id.DisplayName, nil, id.Status)})
	httpx.NoContent(w)
	return nil
}

// ---- pins and files -------------------------------------------------------------------------

// GET /channels/channels/{id}/pins — newest pin first.
func (s *Service) listPins(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	if _, err := s.loadAccess(ctx, s.d.Pool, id, me); err != nil {
		return err
	}
	blocked, err := moderation.BlockedIDs(ctx, s.d.Pool, me)
	if err != nil {
		return err
	}
	rows, err := s.d.Pool.Query(ctx, messageSelect+`
		WHERE m.channel_id = $1 AND m.pinned_at IS NOT NULL AND m.deleted_at IS NULL
		  AND (m.sender_id IS NULL OR m.sender_id <> ALL($2))
		ORDER BY m.pinned_at DESC LIMIT 100`, id, blocked)
	if err != nil {
		return err
	}
	msgs, err := scanMessages(rows, false)
	if err != nil {
		return err
	}
	if err := s.hydrate(ctx, s.d.Pool, msgs, me, false); err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": msgs})
	return nil
}

// ChannelFile is an attachment in a channel's files list.
type ChannelFile struct {
	Attachment
	MessageID    uuid.UUID  `json:"message_id"`
	ThreadRootID *uuid.UUID `json:"thread_root_id"`
	Sender       *UserRef   `json:"sender"`
	CreatedAt    time.Time  `json:"created_at"`
}

// GET /channels/channels/{id}/files?cursor= — newest first.
func (s *Service) listFiles(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	if _, err := s.loadAccess(ctx, s.d.Pool, id, me); err != nil {
		return err
	}
	blocked, err := moderation.BlockedIDs(ctx, s.d.Pool, me)
	if err != nil {
		return err
	}
	p := httpx.ParsePage(r)
	var c cursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT a.id, a.kind, a.content_type, a.file_name, a.size_bytes, a.width, a.height, a.storage_key,
		       m.id, m.thread_root_id, m.created_at, u.id, u.display_name, u.avatar_url, u.status
		FROM channel_attachments a
		JOIN channel_messages m ON m.id = a.message_id
		LEFT JOIN users u ON u.id = m.sender_id
		WHERE m.channel_id = $1 AND m.deleted_at IS NULL AND (m.sender_id IS NULL OR m.sender_id <> ALL($2))
		  AND ($3::timestamptz IS NULL OR (m.created_at, a.id) < ($3, $4))
		ORDER BY m.created_at DESC, a.id DESC LIMIT $5`, id, blocked, nullTime(c.T), c.ID, p.Limit+1)
	if err != nil {
		return err
	}
	defer rows.Close()
	list := []ChannelFile{}
	expires := time.Now().Add(attachmentURLTTL).UTC()
	for rows.Next() {
		var f ChannelFile
		var key string
		var uid *uuid.UUID
		var name, status *string
		var avatar *string
		if err := rows.Scan(&f.ID, &f.Kind, &f.ContentType, &f.FileName, &f.SizeBytes, &f.Width, &f.Height, &key,
			&f.MessageID, &f.ThreadRootID, &f.CreatedAt, &uid, &name, &avatar, &status); err != nil {
			return err
		}
		if uid != nil {
			u := userRef(*uid, deref(name), avatar, deref(status))
			f.Sender = &u
		}
		if f.URL, err = s.signAttachment(ctx, key, f.ContentType, f.FileName, attachmentURLTTL); err != nil {
			slog.WarnContext(ctx, "channels: presign file", "attachment_id", f.ID, "err", err)
		}
		f.URLExpiresAt = expires
		list = append(list, f)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(list, p.Limit, func(f ChannelFile) any { return cursor{f.CreatedAt, f.ID} }))
	return nil
}

// ---- calls ----------------------------------------------------------------------------------

// POST /channels/channels/{id}/call → {path}: start or join the channel's
// call through the app's CallStarter (404 calls_unavailable when unwired).
func (s *Service) startCall(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	caller := auth.MustFromContext(ctx)
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	start := currentCallStarter()
	if start == nil {
		return httpx.NewError(http.StatusNotFound, "calls_unavailable", "Calls aren't set up in this app.")
	}
	a, err := s.loadAccess(ctx, s.d.Pool, id, caller.UserID)
	if err != nil {
		return err
	}
	if err := a.requireWritable(); err != nil {
		return err
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT user_id FROM channel_members WHERE channel_id = $1`, id)
	if err != nil {
		return err
	}
	members, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return err
	}
	name := a.Name
	if a.direct() {
		people, err := s.dmPeople(ctx, s.d.Pool, caller.UserID, []uuid.UUID{id})
		if err != nil {
			return err
		}
		name = dmTitle(people[id])
	}
	path, err := start(ctx, caller, CallTarget{ChannelID: id, OrgID: a.OrgID, Kind: a.Kind, Name: name, MemberIDs: members})
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]string{"path": path})
	return nil
}

// ---- system messages --------------------------------------------------------------------------

// insertSystem records a membership or settings event in the timeline and
// bumps the channel's activity time.
func insertSystem(ctx context.Context, tx pgx.Tx, channelID, actor uuid.UUID, event string, subject *uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	var at time.Time
	if err := tx.QueryRow(ctx, `
		INSERT INTO channel_messages (channel_id, sender_id, kind, body, subject_user_id)
		VALUES ($1, $2, 'system', $3, $4) RETURNING id, created_at`, channelID, actor, event, subject).Scan(&id, &at); err != nil {
		return uuid.Nil, err
	}
	_, err := tx.Exec(ctx, `UPDATE channels SET last_message_at = GREATEST(last_message_at, $2), updated_at = now() WHERE id = $1`, channelID, at)
	return id, err
}
