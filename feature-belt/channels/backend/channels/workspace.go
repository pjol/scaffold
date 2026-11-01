package channels

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"sort"
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
)

// SidebarChannel is one channel or DM in the caller's sidebar.
type SidebarChannel struct {
	ID            uuid.UUID  `json:"id"`
	OrgID         uuid.UUID  `json:"org_id"`
	Kind          string     `json:"kind"`   // public | private | dm | group_dm
	Layout        string     `json:"layout"` // chat | forum
	Name          string     `json:"name"`   // channel handle, or the DM's people ("You" for a note to self)
	Topic         string     `json:"topic"`
	IsDefault     bool       `json:"is_default"`
	Archived      bool       `json:"archived"`
	People        []UserRef  `json:"people"` // DMs: the other people (up to 8)
	MemberCount   int        `json:"member_count"`
	Unread        int        `json:"unread"`   // timeline messages after your read pointer (capped at 100)
	Mentions      int        `json:"mentions"` // of those, ones that mention you (every message in a DM)
	Muted         bool       `json:"muted"`
	Notify        string     `json:"notify"` // default | all | mentions | nothing
	Starred       bool       `json:"starred"`
	LastReadAt    *time.Time `json:"last_read_at"`
	LastMessageAt time.Time  `json:"last_message_at"`
}

// WorkspacePermissions are the workspace-wide actions the caller may take.
type WorkspacePermissions struct {
	CreatePublic  bool `json:"create_public"`
	CreatePrivate bool `json:"create_private"`
	SetDefault    bool `json:"set_default"`
}

// Workspace is GET /channels/orgs/{orgID}/workspace: everything the sidebar shows.
type Workspace struct {
	OrgID         uuid.UUID            `json:"org_id"`
	Role          string               `json:"role"` // the caller's org role
	Channels      []SidebarChannel     `json:"channels"`
	Permissions   WorkspacePermissions `json:"permissions"`
	ThreadsUnread int                  `json:"threads_unread"` // followed threads with unread replies
	LaterCount    int                  `json:"later_count"`    // open saved items
	Config        Config               `json:"config"`
}

// GET /channels/orgs/{orgID}/workspace. The first load in an org creates its
// default channels; every load joins the caller to default channels they
// haven't been in yet (orgs has no membership hook, so this is lazy).
func (s *Service) workspace(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	m, _ := orgs.FromContext(ctx)
	if err := s.ensureDefaults(ctx, m.OrgID, me); err != nil {
		return err
	}
	ws := Workspace{OrgID: m.OrgID, Role: m.Role, Config: s.configValue()}
	var err error
	if ws.Channels, err = s.sidebar(ctx, me, m.OrgID, nil); err != nil {
		return err
	}
	for act, dst := range map[Action]*bool{ActCreatePublic: &ws.Permissions.CreatePublic, ActCreatePrivate: &ws.Permissions.CreatePrivate, ActSetDefault: &ws.Permissions.SetDefault} {
		if *dst, err = allowed(ctx, s.d.Pool, PermissionRequest{Action: act, OrgID: m.OrgID, UserID: me, OrgRole: m.Role}); err != nil {
			return err
		}
	}
	if ws.ThreadsUnread, err = s.threadsUnread(ctx, me, m.OrgID); err != nil {
		return err
	}
	if err := s.d.Pool.QueryRow(ctx, `
		SELECT count(*) FROM channel_saved sv
		JOIN channel_messages x ON x.id = sv.message_id JOIN channels c ON c.id = x.channel_id
		WHERE sv.user_id = $1 AND c.org_id = $2 AND sv.completed_at IS NULL`, me, m.OrgID).Scan(&ws.LaterCount); err != nil {
		return err
	}
	httpx.OK(w, ws)
	return nil
}

// ensureDefaults creates the org's default channels the first time anyone
// opens its workspace, and joins userID to every default channel they have
// never been auto-joined to (leaving one sticks).
func (s *Service) ensureDefaults(ctx context.Context, orgID, userID uuid.UUID) error {
	var pending bool
	if err := s.d.Pool.QueryRow(ctx, `
		SELECT NOT EXISTS (SELECT 1 FROM channels WHERE org_id = $1)
		    OR EXISTS (SELECT 1 FROM channels c WHERE c.org_id = $1 AND c.is_default AND c.archived_at IS NULL
		               AND NOT EXISTS (SELECT 1 FROM channel_autojoins j WHERE j.channel_id = c.id AND j.user_id = $2))`,
		orgID, userID).Scan(&pending); err != nil || !pending {
		return err
	}
	return db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('channels.defaults:' || $1::text, 0))`, orgID); err != nil {
			return err
		}
		var fresh bool
		if err := tx.QueryRow(ctx, `SELECT NOT EXISTS (SELECT 1 FROM channels WHERE org_id = $1)`, orgID).Scan(&fresh); err != nil {
			return err
		}
		if fresh {
			for i, name := range s.cfg.defaultChannels {
				topic := ""
				if i == 0 {
					topic = "Announcements and conversations for everyone"
				}
				var id uuid.UUID
				err := tx.QueryRow(ctx, `
					INSERT INTO channels (org_id, kind, name, topic, is_default) VALUES ($1, 'public', $2, $3, true)
					ON CONFLICT (org_id, name) WHERE name IS NOT NULL DO NOTHING RETURNING id`, orgID, name, topic).Scan(&id)
				if err != nil && !db.IsNoRows(err) {
					return err
				}
			}
		}
		rows, err := tx.Query(ctx, `
			INSERT INTO channel_autojoins (channel_id, user_id)
			SELECT c.id, $2 FROM channels c
			WHERE c.org_id = $1 AND c.is_default AND c.archived_at IS NULL
			ON CONFLICT (channel_id, user_id) DO NOTHING
			RETURNING channel_id`, orgID, userID)
		if err != nil {
			return err
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		if err != nil {
			return err
		}
		for _, id := range ids {
			if err := joinChannel(ctx, tx, id, userID, nil, roleMember); err != nil {
				return err
			}
		}
		return nil
	})
}

// joinChannel adds a member whose read pointer starts at the newest timeline
// message (no wall of unread history). Existing members are left as they are.
func joinChannel(ctx context.Context, tx pgx.Tx, channelID, userID uuid.UUID, addedBy *uuid.UUID, role string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO channel_members (channel_id, user_id, role, added_by, last_read_message_id, last_read_at)
		SELECT $1, $2, $3, $4, lm.id, lm.created_at
		FROM (SELECT 1) one
		LEFT JOIN LATERAL (
		    SELECT id, created_at FROM channel_messages
		    WHERE channel_id = $1 AND (thread_root_id IS NULL OR also_in_channel)
		    ORDER BY created_at DESC, id DESC LIMIT 1
		) lm ON true
		ON CONFLICT (channel_id, user_id) DO NOTHING`, channelID, userID, role, addedBy)
	return err
}

// sidebar lists the caller's channels in an org (only ids when given):
// channels they're in that aren't archived, and DMs that have a message (or
// that they opened) and weren't closed since the last message.
func (s *Service) sidebar(ctx context.Context, me, orgID uuid.UUID, only []uuid.UUID) ([]SidebarChannel, error) {
	blocked, err := moderation.BlockedIDs(ctx, s.d.Pool, me)
	if err != nil {
		return nil, err
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT c.id, c.org_id, c.kind, c.layout, COALESCE(c.name, ''), c.topic, c.is_default, c.archived_at IS NOT NULL, c.last_message_at,
		       cm.notify, cm.muted, cm.starred_at IS NOT NULL, cm.last_read_at,
		       (SELECT count(*) FROM channel_members x WHERE x.channel_id = c.id)::int,
		       u.unread::int, u.mentions::int
		FROM channel_members cm JOIN channels c ON c.id = cm.channel_id
		CROSS JOIN LATERAL (
		    SELECT count(*) AS unread,
		           count(*) FILTER (WHERE c.kind IN ('dm', 'group_dm') OR y.mention_channel
		                            OR EXISTS (SELECT 1 FROM channel_mentions mm WHERE mm.message_id = y.id AND mm.user_id = $1)) AS mentions
		    FROM (
		        SELECT y.id, y.mention_channel FROM channel_messages y
		        WHERE y.channel_id = c.id AND (y.thread_root_id IS NULL OR y.also_in_channel)
		          AND (y.created_at, y.id) > (COALESCE(cm.last_read_at, '-infinity'), COALESCE(cm.last_read_message_id, `+nilUUID+`))
		          AND y.kind <> 'system' AND y.deleted_at IS NULL
		          AND (y.sender_id IS NULL OR (y.sender_id <> $1 AND y.sender_id <> ALL($3)))
		        ORDER BY y.created_at DESC LIMIT 100
		    ) y
		) u
		WHERE cm.user_id = $1 AND c.org_id = $2
		  AND ($4::uuid[] IS NULL OR c.id = ANY($4))
		  AND (
		    (c.kind IN ('public', 'private') AND c.archived_at IS NULL)
		    OR (c.kind IN ('dm', 'group_dm')
		        AND (cm.hidden_at IS NULL OR c.last_message_at > cm.hidden_at)
		        AND (c.created_by = $1 OR EXISTS (SELECT 1 FROM channel_messages z WHERE z.channel_id = c.id))))
		ORDER BY c.kind IN ('dm', 'group_dm'), c.name, c.last_message_at DESC, c.id`, me, orgID, blocked, only)
	if err != nil {
		return nil, err
	}
	out := []SidebarChannel{}
	var dmIDs []uuid.UUID
	for rows.Next() {
		var c SidebarChannel
		if err := rows.Scan(&c.ID, &c.OrgID, &c.Kind, &c.Layout, &c.Name, &c.Topic, &c.IsDefault, &c.Archived, &c.LastMessageAt,
			&c.Notify, &c.Muted, &c.Starred, &c.LastReadAt, &c.MemberCount, &c.Unread, &c.Mentions); err != nil {
			rows.Close()
			return nil, err
		}
		c.People = []UserRef{}
		if c.Kind == kindDM || c.Kind == kindGroupDM {
			dmIDs = append(dmIDs, c.ID)
		}
		out = append(out, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	people, err := s.dmPeople(ctx, s.d.Pool, me, dmIDs)
	if err != nil {
		return nil, err
	}
	for i := range out {
		if p, ok := people[out[i].ID]; ok {
			out[i].People = p
			out[i].Name = dmTitle(p)
		}
	}
	return out, nil
}

// dmPeople returns, per DM, the members other than me (up to 8, by name).
func (s *Service) dmPeople(ctx context.Context, q db.Querier, me uuid.UUID, ids []uuid.UUID) (map[uuid.UUID][]UserRef, error) {
	out := map[uuid.UUID][]UserRef{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := q.Query(ctx, `
		SELECT cm.channel_id, u.id, u.display_name, u.avatar_url, u.status
		FROM channel_members cm JOIN users u ON u.id = cm.user_id
		WHERE cm.channel_id = ANY($1) AND cm.user_id <> $2
		ORDER BY cm.channel_id, u.display_name, u.id`, ids, me)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ch, uid uuid.UUID
		var name, status string
		var avatar *string
		if err := rows.Scan(&ch, &uid, &name, &avatar, &status); err != nil {
			return nil, err
		}
		if len(out[ch]) < 8 {
			out[ch] = append(out[ch], userRef(uid, name, avatar, status))
		}
	}
	for _, id := range ids {
		if _, ok := out[id]; !ok {
			out[id] = []UserRef{} // a note to yourself
		}
	}
	return out, rows.Err()
}

// dmTitle names a DM by its other people: "Jordan Lee", "Jordan, Priya and Sam", "You".
func dmTitle(people []UserRef) string {
	switch len(people) {
	case 0:
		return "You"
	case 1:
		return people[0].DisplayName
	}
	names := make([]string, len(people))
	for i, p := range people {
		names[i] = strings.Fields(p.DisplayName + " ")[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// ---- unread totals (nav badge) ---------------------------------------------------------

// OrgUnread is one org's counts.
type OrgUnread struct {
	OrgID          uuid.UUID `json:"org_id"`
	UnreadChannels int       `json:"unread_channels"` // not muted, with unread messages
	Mentions       int       `json:"mentions"`        // mentions and DM messages
}

// Unread is GET /channels/unread: totals for badges across the caller's orgs.
type Unread struct {
	Mentions       int         `json:"mentions"`
	UnreadChannels int         `json:"unread_channels"`
	Orgs           []OrgUnread `json:"orgs"`
}

func (s *Service) unreadTotals(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	rows, err := s.d.Pool.Query(ctx, `SELECT org_id FROM org_members WHERE user_id = $1 ORDER BY created_at, org_id`, me)
	if err != nil {
		return err
	}
	orgIDs, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return err
	}
	out := Unread{Orgs: []OrgUnread{}}
	for _, orgID := range orgIDs {
		list, err := s.sidebar(ctx, me, orgID, nil)
		if err != nil {
			return err
		}
		ou := OrgUnread{OrgID: orgID}
		for _, c := range list {
			ou.Mentions += c.Mentions
			if c.Unread > 0 && !c.Muted {
				ou.UnreadChannels++
			}
		}
		out.Mentions += ou.Mentions
		out.UnreadChannels += ou.UnreadChannels
		out.Orgs = append(out.Orgs, ou)
	}
	httpx.OK(w, out)
	return nil
}

// ---- browse ------------------------------------------------------------------------------

// BrowseChannel is a row in the channel browser.
type BrowseChannel struct {
	ID          uuid.UUID `json:"id"`
	Kind        string    `json:"kind"`
	Layout      string    `json:"layout"`
	Name        string    `json:"name"`
	Topic       string    `json:"topic"`
	Description string    `json:"description"`
	IsDefault   bool      `json:"is_default"`
	Archived    bool      `json:"archived"`
	MemberCount int       `json:"member_count"`
	Joined      bool      `json:"joined"`
}

// GET /channels/orgs/{orgID}/browse?q=&archived=true&cursor= — public
// channels and the private ones the caller is in, by name.
func (s *Service) browse(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	m, _ := orgs.FromContext(ctx)
	p := httpx.ParsePage(r)
	var c struct {
		Name string `json:"n"`
	}
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	q := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(r.URL.Query().Get("q"), "#")))
	rows, err := s.d.Pool.Query(ctx, `
		SELECT c.id, c.kind, c.layout, c.name, c.topic, c.description, c.is_default, c.archived_at IS NOT NULL,
		       (SELECT count(*) FROM channel_members x WHERE x.channel_id = c.id)::int,
		       EXISTS (SELECT 1 FROM channel_members x WHERE x.channel_id = c.id AND x.user_id = $2)
		FROM channels c
		WHERE c.org_id = $1 AND c.kind IN ('public', 'private')
		  AND (c.kind = 'public' OR EXISTS (SELECT 1 FROM channel_members x WHERE x.channel_id = c.id AND x.user_id = $2))
		  AND ((c.archived_at IS NOT NULL) = $3)
		  AND ($4 = '' OR c.name LIKE '%' || $4 || '%' ESCAPE '\' OR lower(c.topic) LIKE '%' || $4 || '%' ESCAPE '\')
		  AND ($5 = '' OR c.name > $5)
		ORDER BY c.name LIMIT $6`, m.OrgID, me, r.URL.Query().Get("archived") == "true", escapeLike(q), c.Name, p.Limit+1)
	if err != nil {
		return err
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[BrowseChannel])
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(list, p.Limit, func(b BrowseChannel) any { return map[string]string{"n": b.Name} }))
	return nil
}

// ---- create a channel ----------------------------------------------------------------------

type createReq struct {
	Name        string      `json:"name"`
	Kind        string      `json:"kind"`   // public | private
	Layout      string      `json:"layout"` // chat (default) | forum
	Topic       string      `json:"topic"`
	Description string      `json:"description"`
	ForumTags   []string    `json:"forum_tags"`
	MemberIDs   []uuid.UUID `json:"member_ids"`
	IsDefault   bool        `json:"is_default"`
}

var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,79}$`)

// cleanName lowercases and dashes a channel name: "Gear Swap" → "gear-swap".
func cleanName(raw string) (string, error) {
	n := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "#")))
	n = strings.Join(strings.Fields(n), "-")
	if !namePattern.MatchString(n) {
		return "", httpx.BadRequest("Channel names are 1 to 80 lowercase letters, numbers, dashes or underscores.")
	}
	return n, nil
}

func cleanTags(in []string) ([]string, error) {
	out := []string{}
	for _, t := range in {
		t = strings.Join(strings.Fields(t), " ")
		if t == "" || slices.Contains(out, t) {
			continue
		}
		if utf8.RuneCountInString(t) > 30 {
			return nil, httpx.BadRequest("Tags can be up to 30 characters.")
		}
		out = append(out, t)
	}
	if len(out) > 20 {
		return nil, httpx.BadRequest("A forum can have up to 20 tags.")
	}
	return out, nil
}

// checkText runs channel text (names, topics, descriptions) through the filter.
func checkText(ctx context.Context, texts ...string) error {
	for _, t := range texts {
		if strings.TrimSpace(t) != "" {
			if err := moderation.Check(ctx, t); err != nil {
				return err
			}
		}
	}
	return nil
}

// POST /channels/orgs/{orgID}/channels
func (s *Service) createChannel(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	m, _ := orgs.FromContext(ctx)
	var req createReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	name, err := cleanName(req.Name)
	if err != nil {
		return err
	}
	act := ActCreatePublic
	switch req.Kind {
	case kindPublic, "":
		req.Kind = kindPublic
	case kindPrivate:
		act = ActCreatePrivate
	default:
		return httpx.BadRequest("Kind must be public or private.")
	}
	switch req.Layout {
	case "":
		req.Layout = layoutChat
	case layoutChat, layoutForum:
	default:
		return httpx.BadRequest("Layout must be chat or forum.")
	}
	topic, desc := strings.TrimSpace(req.Topic), strings.TrimSpace(req.Description)
	if utf8.RuneCountInString(topic) > 250 || utf8.RuneCountInString(desc) > 1000 {
		return httpx.BadRequest("Topics can be up to 250 characters and descriptions up to 1000.")
	}
	tags, err := cleanTags(req.ForumTags)
	if err != nil {
		return err
	}
	if err := checkText(ctx, append([]string{name, topic, desc}, tags...)...); err != nil {
		return err
	}
	ok, err := allowed(ctx, s.d.Pool, PermissionRequest{Action: act, OrgID: m.OrgID, UserID: me, OrgRole: m.Role})
	if err != nil {
		return err
	}
	if !ok {
		return httpx.Forbidden("You can't create " + req.Kind + " channels in this " + orgs.Noun() + ".")
	}
	if req.IsDefault {
		if req.Kind != kindPublic {
			return httpx.BadRequest("Only public channels can be default channels.")
		}
		if ok, err := allowed(ctx, s.d.Pool, PermissionRequest{Action: ActSetDefault, OrgID: m.OrgID, UserID: me, OrgRole: m.Role}); err != nil || !ok {
			if err == nil {
				err = httpx.Forbidden("Only owners and admins choose default channels.")
			}
			return err
		}
	}
	others := dedupe(req.MemberIDs, me)
	if len(others) > 500 {
		return httpx.BadRequest("Add up to 500 people at a time.")
	}
	var id, created uuid.UUID
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if err := requireOrgMembers(ctx, tx, m.OrgID, others); err != nil {
			return err
		}
		err := tx.QueryRow(ctx, `
			INSERT INTO channels (org_id, kind, layout, name, topic, description, forum_tags, is_default, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
			m.OrgID, req.Kind, req.Layout, name, topic, desc, tags, req.IsDefault, me).Scan(&id)
		if db.IsUniqueViolation(err, "channels_name_uidx") {
			return httpx.Conflict("#" + name + " is taken. Choose another name.")
		}
		if err != nil {
			return err
		}
		if err := joinChannel(ctx, tx, id, me, &me, roleAdmin); err != nil {
			return err
		}
		for _, uid := range others {
			if err := joinChannel(ctx, tx, id, uid, &me, roleMember); err != nil {
				return err
			}
		}
		if req.IsDefault {
			if _, err := tx.Exec(ctx, `INSERT INTO channel_autojoins (channel_id, user_id) SELECT $1, unnest($2::uuid[]) ON CONFLICT DO NOTHING`,
				id, append([]uuid.UUID{me}, others...)); err != nil {
				return err
			}
		}
		created, err = insertSystem(ctx, tx, id, me, "created", nil)
		return err
	})
	if err != nil {
		return err
	}
	s.announce(ctx, id, created)
	for _, uid := range append([]uuid.UUID{me}, others...) {
		s.sidebarEvent(ctx, uid, m.OrgID, id, "added", nil, false)
	}
	d, err := s.detail(ctx, me, id)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, d)
	return nil
}

// requireOrgMembers refuses ids that aren't active members of the org.
func requireOrgMembers(ctx context.Context, q db.Querier, orgID uuid.UUID, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	var n int
	if err := q.QueryRow(ctx, `
		SELECT count(*) FROM org_members om JOIN users u ON u.id = om.user_id
		WHERE om.org_id = $1 AND om.user_id = ANY($2) AND u.status IN ('active', 'suspended')`, orgID, ids).Scan(&n); err != nil {
		return err
	}
	if n != len(ids) {
		return httpx.NotFound("Someone you chose isn't in this " + orgs.Noun() + ".")
	}
	return nil
}

// ---- direct and group messages -----------------------------------------------------------

type dmReq struct {
	UserIDs []uuid.UUID `json:"user_ids"` // everyone but you; empty = a note to yourself
}

// POST /channels/orgs/{orgID}/dms — 200 with the existing conversation for
// these people, 201 new.
func (s *Service) openDM(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	m, _ := orgs.FromContext(ctx)
	var req dmReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	others := dedupe(req.UserIDs, me)
	if len(others)+1 > maxGroupDM {
		return httpx.BadRequest(fmt.Sprintf("Group messages can have up to %d people. Create a private channel instead.", maxGroupDM))
	}
	kind := kindDM
	if len(others) > 1 {
		kind = kindGroupDM
	}
	everyone := append([]uuid.UUID{me}, others...)
	keys := make([]string, len(everyone))
	for i, id := range everyone {
		keys[i] = id.String()
	}
	sort.Strings(keys)
	key := strings.Join(keys, ",")
	var id uuid.UUID
	created := false
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if err := requireOrgMembers(ctx, tx, m.OrgID, others); err != nil {
			return err
		}
		for _, uid := range others {
			blocked, err := moderation.IsBlocked(ctx, tx, me, uid)
			if err != nil {
				return err
			}
			if blocked {
				return errBlocked
			}
		}
		find := `SELECT id FROM channels WHERE org_id = $1 AND dm_key = $2`
		err := tx.QueryRow(ctx, find, m.OrgID, key).Scan(&id)
		if db.IsNoRows(err) {
			err = tx.QueryRow(ctx, `
				INSERT INTO channels (org_id, kind, dm_key, created_by) VALUES ($1, $2, $3, $4)
				ON CONFLICT (org_id, dm_key) WHERE dm_key IS NOT NULL DO NOTHING RETURNING id`, m.OrgID, kind, key, me).Scan(&id)
			switch {
			case err == nil:
				created = true
			case db.IsNoRows(err): // opened concurrently
				err = tx.QueryRow(ctx, find, m.OrgID, key).Scan(&id)
			}
		}
		if err != nil {
			return err
		}
		for _, uid := range everyone {
			if err := joinChannel(ctx, tx, id, uid, &me, roleMember); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `UPDATE channel_members SET hidden_at = NULL, updated_at = now() WHERE channel_id = $1 AND user_id = $2`, id, me)
		return err
	})
	if err != nil {
		return err
	}
	s.sidebarEvent(ctx, me, m.OrgID, id, "added", nil, false)
	d, err := s.detail(ctx, me, id)
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

// ---- people and groups (pickers, mention autocomplete) --------------------------------------

// Person is an org member in pickers and mention autocomplete.
type Person struct {
	UserRef
	InChannel bool `json:"in_channel"` // with ?channel_id=: already a member
}

// GET /channels/orgs/{orgID}/people?q=&channel_id= — org members by name or
// email prefix; without q, the people you DM most recently, then by name.
func (s *Service) people(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	m, _ := orgs.FromContext(ctx)
	q := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(r.URL.Query().Get("q"), "@")))
	var channelID *uuid.UUID
	if v := r.URL.Query().Get("channel_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			return httpx.BadRequest("Invalid channel_id.")
		}
		a, err := s.loadAccess(ctx, s.d.Pool, id, me)
		if err != nil {
			return err
		}
		if a.OrgID != m.OrgID {
			return errChannelNotFound
		}
		channelID = &id
	}
	blocked, err := moderation.BlockedIDs(ctx, s.d.Pool, me)
	if err != nil {
		return err
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT u.id, u.display_name, u.avatar_url, u.status,
		       ($5::uuid IS NOT NULL AND EXISTS (SELECT 1 FROM channel_members x WHERE x.channel_id = $5 AND x.user_id = u.id))
		FROM org_members om JOIN users u ON u.id = om.user_id
		LEFT JOIN LATERAL (
		    SELECT max(c.last_message_at) AS at FROM channels c
		    JOIN channel_members a ON a.channel_id = c.id AND a.user_id = $2
		    JOIN channel_members b ON b.channel_id = c.id AND b.user_id = u.id
		    WHERE c.org_id = $1 AND c.kind = 'dm'
		) recent ON $3 = ''
		WHERE om.org_id = $1 AND u.status IN ('active', 'suspended') AND u.id <> ALL($4)
		  AND ($3 = '' OR lower(u.display_name) LIKE $3 || '%' ESCAPE '\' OR lower(u.display_name) LIKE '% ' || $3 || '%' ESCAPE '\'
		       OR lower(u.email) LIKE $3 || '%' ESCAPE '\')
		ORDER BY u.id = $2, recent.at DESC NULLS LAST, lower(u.display_name), u.id
		LIMIT 20`, m.OrgID, me, escapeLike(q), blocked, channelID)
	if err != nil {
		return err
	}
	defer rows.Close()
	out := httpx.Page[Person]{Items: []Person{}}
	for rows.Next() {
		var id uuid.UUID
		var name, status string
		var avatar *string
		var in bool
		if err := rows.Scan(&id, &name, &avatar, &status, &in); err != nil {
			return err
		}
		out.Items = append(out.Items, Person{UserRef: userRef(id, name, avatar, status), InChannel: in})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

// GET /channels/orgs/{orgID}/groups?q= — groups to @mention (empty when the
// app wired no GroupResolver).
func (s *Service) groups(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	m, _ := orgs.FromContext(ctx)
	out := httpx.Page[Group]{Items: []Group{}}
	if res := currentGroupResolver(); res != nil {
		list, err := res.SearchGroups(ctx, m.OrgID, me, strings.TrimPrefix(strings.TrimSpace(r.URL.Query().Get("q")), "@"), 20)
		if err != nil {
			return err
		}
		for _, g := range list {
			if groupIDPattern.MatchString(g.ID) && handlePattern.MatchString(g.Handle) {
				out.Items = append(out.Items, g)
			}
		}
	}
	httpx.OK(w, out)
	return nil
}
