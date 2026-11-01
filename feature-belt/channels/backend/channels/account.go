package channels

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/realtime"
)

// ---- deletion ------------------------------------------------------------------

// DeleteAccount erases the person's part of every workspace: their messages
// become "Message deleted" tombstones (text, titles and attachments gone),
// their reactions, mentions, saved items, follows and channel memberships go.
// In DMs the membership row stays so the other people still see the
// conversation with "Deleted user"; DMs left without anyone are deleted.
func (s *Service) DeleteAccount(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	rows, err := tx.Query(ctx, `SELECT channel_id FROM channel_members WHERE user_id = $1`, userID)
	if err != nil {
		return err
	}
	channelIDs, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return err
	}
	s.deletedMembership.Store(userID, channelIDs)
	for _, sql := range []string{
		`DELETE FROM channel_attachments a USING channel_messages m WHERE m.id = a.message_id AND m.sender_id = $1`,
		`DELETE FROM channel_mentions WHERE user_id = $1 OR message_id IN (SELECT id FROM channel_messages WHERE sender_id = $1)`,
		`UPDATE channel_messages SET body = '', title = CASE WHEN title IS NULL THEN NULL ELSE 'Deleted post' END, tags = '{}',
		   client_id = NULL, edited_at = NULL, mention_channel = false, pinned_at = NULL, pinned_by = NULL,
		   deleted_at = COALESCE(deleted_at, now()), deleted_by = COALESCE(deleted_by, 'account_deleted'), updated_at = now()
		 WHERE sender_id = $1 AND kind <> 'system'`,
		`DELETE FROM channel_reactions WHERE user_id = $1`,
		`DELETE FROM channel_saved WHERE user_id = $1`,
		`DELETE FROM channel_thread_follows WHERE user_id = $1`,
		`DELETE FROM channel_autojoins WHERE user_id = $1`,
		`DELETE FROM channel_members cm USING channels c WHERE c.id = cm.channel_id AND cm.user_id = $1 AND c.kind IN ('public', 'private')`,
		`UPDATE channel_members SET notify = 'default', muted = false, starred_at = NULL, updated_at = now() WHERE user_id = $1`,
	} {
		if _, err := tx.Exec(ctx, sql, userID); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `
		DELETE FROM channels c WHERE c.id = ANY($1) AND c.kind IN ('dm', 'group_dm')
		  AND NOT EXISTS (SELECT 1 FROM channel_members cm JOIN users u ON u.id = cm.user_id
		                  WHERE cm.channel_id = c.id AND u.id <> $2 AND u.status <> 'deleted')`, channelIDs, userID)
	return err
}

// AccountDeleted drops the person's live subscriptions to their channels.
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

type exportMembership struct {
	ChannelID uuid.UUID  `json:"channel_id"`
	OrgID     uuid.UUID  `json:"org_id"`
	Kind      string     `json:"kind"`
	Name      string     `json:"name"`
	Role      string     `json:"role"`
	Notify    string     `json:"notify"`
	Muted     bool       `json:"muted"`
	Starred   bool       `json:"starred"`
	JoinedAt  time.Time  `json:"joined_at"`
	LastRead  *time.Time `json:"last_read_at"`
}

type exportAttachment struct {
	FileName    string `json:"file_name"`
	ContentType string `json:"content_type"`
	SizeBytes   *int64 `json:"size_bytes"`
	URL         string `json:"url"` // signed, valid for 24 hours
}

type exportMessage struct {
	ID           uuid.UUID          `json:"id"`
	ChannelID    uuid.UUID          `json:"channel_id"`
	ThreadRootID *uuid.UUID         `json:"thread_root_id"`
	Title        *string            `json:"title"`
	Body         string             `json:"body"`
	Attachments  []exportAttachment `json:"attachments"`
	CreatedAt    time.Time          `json:"created_at"`
	EditedAt     *time.Time         `json:"edited_at"`
	DeletedAt    *time.Time         `json:"deleted_at"`
}

type exportReaction struct {
	MessageID uuid.UUID `json:"message_id"`
	Emoji     string    `json:"emoji"`
	CreatedAt time.Time `json:"created_at"`
}

type exportSaved struct {
	MessageID   uuid.UUID  `json:"message_id"`
	SavedAt     time.Time  `json:"saved_at"`
	CompletedAt *time.Time `json:"completed_at"`
}

type exportData struct {
	Memberships []exportMembership `json:"memberships"`
	Messages    []exportMessage    `json:"messages"`
	Reactions   []exportReaction   `json:"reactions"`
	Saved       []exportSaved      `json:"saved"`
}

// ExportAccount returns the person's channel memberships and preferences,
// the messages they sent (with 24-hour links to their files), their
// reactions and saved items. Other people's messages are not included.
func (s *Service) ExportAccount(ctx context.Context, userID uuid.UUID) (any, error) {
	out := exportData{}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT c.id, c.org_id, c.kind, COALESCE(c.name, ''), cm.role, cm.notify, cm.muted, cm.starred_at IS NOT NULL, cm.joined_at, cm.last_read_at
		FROM channel_members cm JOIN channels c ON c.id = cm.channel_id WHERE cm.user_id = $1 ORDER BY cm.joined_at`, userID)
	if err != nil {
		return nil, err
	}
	if out.Memberships, err = pgx.CollectRows(rows, pgx.RowToStructByPos[exportMembership]); err != nil {
		return nil, err
	}
	rows, err = s.d.Pool.Query(ctx, `
		SELECT id, channel_id, thread_root_id, title, body, created_at, edited_at, deleted_at
		FROM channel_messages WHERE sender_id = $1 AND kind = 'text' ORDER BY created_at, id`, userID)
	if err != nil {
		return nil, err
	}
	index := map[uuid.UUID]int{}
	for rows.Next() {
		var m exportMessage
		if err := rows.Scan(&m.ID, &m.ChannelID, &m.ThreadRootID, &m.Title, &m.Body, &m.CreatedAt, &m.EditedAt, &m.DeletedAt); err != nil {
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
		FROM channel_attachments a JOIN channel_messages m ON m.id = a.message_id
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
		if a.URL, err = s.signAttachment(ctx, key, a.ContentType, a.FileName, 24*time.Hour); err != nil {
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
	rows, err = s.d.Pool.Query(ctx, `SELECT message_id, emoji, created_at FROM channel_reactions WHERE user_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	if out.Reactions, err = pgx.CollectRows(rows, pgx.RowToStructByPos[exportReaction]); err != nil {
		return nil, err
	}
	rows, err = s.d.Pool.Query(ctx, `SELECT message_id, created_at, completed_at FROM channel_saved WHERE user_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	if out.Saved, err = pgx.CollectRows(rows, pgx.RowToStructByPos[exportSaved]); err != nil {
		return nil, err
	}
	if out.Memberships == nil {
		out.Memberships = []exportMembership{}
	}
	if out.Messages == nil {
		out.Messages = []exportMessage{}
	}
	if out.Reactions == nil {
		out.Reactions = []exportReaction{}
	}
	if out.Saved == nil {
		out.Saved = []exportSaved{}
	}
	return out, nil
}

// ---- development data --------------------------------------------------------------

var seedNS = uuid.MustParse("1c6b9d5e-2f4a-4b7e-8f31-9a0d2c7e5b44")

func seedID(name string) uuid.UUID { return uuid.NewSHA1(seedNS, []byte("channels:"+name)) }

// SeedDev builds a small workspace in the orgs feature's dev orgs: in
// "Riverside Climbing Club" (Sam owner, Jordan admin, Priya member) #general
// with a thread, an @channel and a mention of Sam, #route-setting with
// formatting and reactions, #gear-swap as a forum with tagged posts, private
// #trip-planning (Sam, Jordan) with a pin, and DMs; in "Night Owls Book Club"
// a #general. Sam has unread messages, a mention, an unread thread reply and
// a saved item. Avery (admin) is in no org. Fixed ids: re-running changes nothing.
// (Channels seeds before orgs, so it ensures the same two orgs orgs creates.)
func (s *Service) SeedDev(ctx context.Context, users []uuid.UUID) error {
	if len(users) < 4 {
		return nil
	}
	sam, jordan, priya := users[1], users[2], users[3]
	return db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		org := func(name, slug string, owner uuid.UUID, members map[uuid.UUID]string) (uuid.UUID, error) {
			var id uuid.UUID
			if err := tx.QueryRow(ctx, `
				INSERT INTO orgs (name, slug, created_by) VALUES ($1, $2, $3)
				ON CONFLICT (slug) DO UPDATE SET name = orgs.name RETURNING id`, name, slug, owner).Scan(&id); err != nil {
				return id, err
			}
			for uid, role := range members {
				if _, err := tx.Exec(ctx, `INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, id, uid, role); err != nil {
					return id, err
				}
			}
			return id, nil
		}
		climb, err := org("Riverside Climbing Club", "riverside-climbing", sam, map[uuid.UUID]string{sam: "owner", jordan: "admin", priya: "member"})
		if err != nil {
			return err
		}
		owls, err := org("Night Owls Book Club", "night-owls", priya, map[uuid.UUID]string{priya: "owner", sam: "member"})
		if err != nil {
			return err
		}

		type ch struct {
			key, kind, layout, name, topic string
			org                            uuid.UUID
			def                            bool
			tags                           []string
			by                             *uuid.UUID
			members                        []uuid.UUID
		}
		dmKey := func(a, b uuid.UUID) string {
			x, y := a.String(), b.String()
			if x > y {
				x, y = y, x
			}
			return x + "," + y
		}
		chans := []ch{
			{"general", "public", "chat", "general", "Announcements and conversations for everyone", climb, true, nil, nil, []uuid.UUID{sam, jordan, priya}},
			{"route-setting", "public", "chat", "route-setting", "New problems and resets on the bouldering wall", climb, false, nil, &jordan, []uuid.UUID{sam, jordan, priya}},
			{"gear-swap", "public", "forum", "gear-swap", "Buy, sell and give away gear", climb, false, []string{"For sale", "Wanted", "Free"}, &priya, []uuid.UUID{sam, jordan, priya}},
			{"trip-planning", "private", "chat", "trip-planning", "Spring trip", climb, false, nil, &sam, []uuid.UUID{sam, jordan}},
			{"owls-general", "public", "chat", "general", "Book picks and meeting dates", owls, true, nil, nil, []uuid.UUID{priya, sam}},
		}
		ids := map[string]uuid.UUID{}
		for _, c := range chans {
			id := seedID(c.key)
			ids[c.key] = id
			tags := c.tags
			if tags == nil {
				tags = []string{}
			}
			if err := tx.QueryRow(ctx, `
				INSERT INTO channels (id, org_id, kind, layout, name, topic, forum_tags, is_default, created_by, created_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, now() - interval '4 days')
				ON CONFLICT (org_id, name) WHERE name IS NOT NULL DO UPDATE SET topic = channels.topic RETURNING id`,
				id, c.org, c.kind, c.layout, c.name, c.topic, tags, c.def, c.by).Scan(&id); err != nil {
				return err
			}
			ids[c.key] = id
			for _, uid := range c.members {
				role := roleMember
				if c.by != nil && *c.by == uid {
					role = roleAdmin
				}
				if _, err := tx.Exec(ctx, `
					INSERT INTO channel_members (channel_id, user_id, role, joined_at) VALUES ($1, $2, $3, now() - interval '4 days')
					ON CONFLICT DO NOTHING`, id, uid, role); err != nil {
					return err
				}
				if c.def {
					if _, err := tx.Exec(ctx, `INSERT INTO channel_autojoins (channel_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, id, uid); err != nil {
						return err
					}
				}
			}
		}
		for _, dm := range []struct {
			key  string
			a, b uuid.UUID
		}{{"dm-sam-jordan", sam, jordan}, {"dm-sam-priya", sam, priya}} {
			id := seedID(dm.key)
			if err := tx.QueryRow(ctx, `
				INSERT INTO channels (id, org_id, kind, dm_key, created_by, created_at) VALUES ($1, $2, 'dm', $3, $4, now() - interval '4 days')
				ON CONFLICT (org_id, dm_key) WHERE dm_key IS NOT NULL DO UPDATE SET updated_at = channels.updated_at RETURNING id`,
				id, climb, dmKey(dm.a, dm.b), dm.a).Scan(&id); err != nil {
				return err
			}
			ids[dm.key] = id
			for _, uid := range []uuid.UUID{dm.a, dm.b} {
				if _, err := tx.Exec(ctx, `INSERT INTO channel_members (channel_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, id, uid); err != nil {
					return err
				}
			}
		}

		type msg struct {
			key, channel string
			from         uuid.UUID
			body         string
			mins         int
			root         string
			title        string
			tags         []string
		}
		u := func(id uuid.UUID) string { return "<@" + id.String() + ">" }
		msgs := []msg{
			{"g1", "general", sam, "Welcome to the club workspace. Ask questions here and use #route-setting for wall updates.", 3 * 24 * 60, "", "", nil},
			{"g2", "general", jordan, "The wall closes early on **Thursday** for the competition setup.", 26 * 60, "", "", nil},
			{"g2r1", "general", sam, "Thanks. Does that include the training boards?", 25 * 60, "g2", "", nil},
			{"g2r2", "general", jordan, "Training boards stay open until 9.", 24 * 60, "g2", "", nil},
			{"g3", "general", sam, "<@channel> Membership renewals open on Monday. Renew in the first week for a free belay check.", 5 * 60, "", "", nil},
			{"g4", "general", priya, "Who's up for an evening session tomorrow? " + u(sam) + " you wanted to try the new overhang :muscle:", 40, "", "", nil},
			{"g5", "general", jordan, "Count me in for tomorrow.", 30, "", "", nil},
			{"r1", "route-setting", jordan, "Reset the **blue circuit** today: 6 new problems from V2 to V6.\n- Two slab problems on the left\n- A long traverse under the roof", 2 * 24 * 60, "", "", nil},
			{"r2", "route-setting", sam, "Nice. The V5 on the arete is great.", 2*24*60 - 30, "", "", nil},
			{"r3", "route-setting", jordan, "Next reset is the `yellow` circuit on the 12th.", 3 * 60, "", "", nil},
			{"f1", "gear-swap", priya, "Worn for 3 months, resoled once. $70 or best offer.", 2 * 24 * 60, "", "Selling: climbing shoes, size 40", []string{"For sale"}},
			{"f1r1", "gear-swap", sam, "Still available? I'd take them.", 24 * 60, "f1", "", nil},
			{"f1r2", "gear-swap", priya, "Yes. Bring cash on Thursday?", 20 * 60, "f1", "", nil},
			{"f2", "gear-swap", jordan, "Looking for a rope in good condition for the outdoor season.", 6 * 60, "", "Wanted: 60 m dynamic rope", []string{"Wanted"}},
			{"f3", "gear-swap", sam, "Three chalk bags in the lost and found box. Take one.", 60, "", "Free: chalk bags", []string{"Free"}},
			{"t1", "trip-planning", sam, "Shortlist for the spring trip: Bishop or Red Rocks.", 24 * 60, "", "", nil},
			{"t2", "trip-planning", jordan, "Red Rocks. Better weather in April.", 20 * 60, "", "", nil},
			{"d1", "dm-sam-jordan", jordan, "Can you open the gym on Saturday? I'm out of town.", 2 * 60, "", "", nil},
			{"d2", "dm-sam-jordan", sam, "Yes, I'll take it.", 110, "", "", nil},
			{"d3", "dm-sam-jordan", jordan, "Thanks!", 10, "", "", nil},
			{"p1", "dm-sam-priya", priya, "Here's the route map from last weekend: https://example.com/route-map", 3 * 60, "", "", nil},
			{"n1", "owls-general", priya, "This month's book: *The Overstory*. We meet on the 20th.", 24 * 60, "", "", nil},
		}
		for _, m := range msgs {
			var root *uuid.UUID
			if m.root != "" {
				r := seedID("msg:" + m.root)
				root = &r
			}
			tags := m.tags
			if tags == nil {
				tags = []string{}
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO channel_messages (id, channel_id, sender_id, body, thread_root_id, title, tags, mention_channel, created_at, activity_at)
				VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7, $8, now() - make_interval(mins => $9), now() - make_interval(mins => $9))
				ON CONFLICT (id) DO NOTHING`, seedID("msg:"+m.key), ids[m.channel], m.from, m.body, root, m.title, tags,
				m.key == "g3", m.mins); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO channel_mentions (message_id, user_id, kind) VALUES ($1, $2, 'user') ON CONFLICT DO NOTHING`,
			seedID("msg:g4"), sam); err != nil {
			return err
		}
		allIDs := make([]uuid.UUID, 0, len(ids))
		for _, id := range ids {
			allIDs = append(allIDs, id)
		}
		// Thread counters, activity and channel order from the messages.
		if _, err := tx.Exec(ctx, `
			UPDATE channel_messages r SET reply_count = x.n, last_reply_at = x.last, activity_at = GREATEST(r.created_at, x.last)
			FROM (SELECT thread_root_id, count(*)::int AS n, max(created_at) AS last FROM channel_messages
			      WHERE thread_root_id IS NOT NULL AND channel_id = ANY($1) GROUP BY thread_root_id) x
			WHERE r.id = x.thread_root_id`, allIDs); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE channels c SET last_message_at = COALESCE((SELECT max(created_at) FROM channel_messages m
			  WHERE m.channel_id = c.id AND m.thread_root_id IS NULL), c.last_message_at) WHERE c.id = ANY($1)`, allIDs); err != nil {
			return err
		}
		// Reactions, a pin, a saved item.
		for _, r := range []struct {
			key   string
			user  uuid.UUID
			emoji string
		}{{"r1", sam, "🔥"}, {"r1", priya, "👍"}, {"g3", jordan, "🎉"}, {"g3", priya, "🎉"}, {"f1r1", priya, "👍"}} {
			if _, err := tx.Exec(ctx, `INSERT INTO channel_reactions (message_id, user_id, emoji) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
				seedID("msg:"+r.key), r.user, r.emoji); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE channel_messages SET pinned_at = now() - interval '19 hours', pinned_by = $2 WHERE id = $1 AND pinned_at IS NULL`,
			seedID("msg:t2"), sam); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO channel_saved (user_id, message_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, sam, seedID("msg:g3")); err != nil {
			return err
		}
		// Thread follows: authors and repliers, Sam with one unread reply.
		for _, f := range []struct {
			root, readTo string
			user         uuid.UUID
		}{{"g2", "g2r2", jordan}, {"g2", "g2r1", sam}, {"f1", "f1r2", priya}, {"f1", "f1r1", sam}} {
			if _, err := tx.Exec(ctx, `
				INSERT INTO channel_thread_follows (root_id, user_id, last_read_reply_id, last_read_at)
				SELECT $1, $2, m.id, m.created_at FROM channel_messages m WHERE m.id = $3
				ON CONFLICT DO NOTHING`, seedID("msg:"+f.root), f.user, seedID("msg:"+f.readTo)); err != nil {
				return err
			}
		}
		// Read pointers: everyone has read everything, except Sam (#general
		// up to the @channel, #route-setting up to his reply, the DM up to his
		// reply). Only pointers never set are touched.
		if _, err := tx.Exec(ctx, `
			UPDATE channel_members cm SET last_read_message_id = lm.id, last_read_at = lm.created_at
			FROM channels c CROSS JOIN LATERAL (
			    SELECT id, created_at FROM channel_messages m WHERE m.channel_id = c.id AND (m.thread_root_id IS NULL OR m.also_in_channel)
			    ORDER BY created_at DESC, id DESC LIMIT 1) lm
			WHERE c.id = cm.channel_id AND c.id = ANY($1) AND cm.last_read_message_id IS NULL AND cm.user_id <> $2`, allIDs, sam); err != nil {
			return err
		}
		for _, rp := range []struct{ channel, key string }{
			{"general", "g3"}, {"route-setting", "r2"}, {"gear-swap", "f3"}, {"trip-planning", "t2"},
			{"dm-sam-jordan", "d2"}, {"dm-sam-priya", "p1"}, {"owls-general", "n1"},
		} {
			if _, err := tx.Exec(ctx, `
				UPDATE channel_members cm SET last_read_message_id = m.id, last_read_at = m.created_at
				FROM channel_messages m WHERE m.id = $3 AND cm.channel_id = $1 AND cm.user_id = $2 AND cm.last_read_message_id IS NULL`,
				ids[rp.channel], sam, seedID("msg:"+rp.key)); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `UPDATE channel_members SET starred_at = now() WHERE channel_id = $1 AND user_id = $2 AND starred_at IS NULL`,
			ids["route-setting"], sam)
		return err
	})
}
