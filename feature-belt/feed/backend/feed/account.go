package feed

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// DeleteAccount deletes the user's posts (with everything on them), their
// comments and reactions, mentions of them and their seen marker. A
// top-level comment that other people replied to keeps a blank placeholder
// so those replies stay in place. Stored media goes with the core storage
// cleanup (every feed_media/<user>/ object).
func (s *Service) DeleteAccount(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	stmts := []string{
		// Their replies first, so the next statements only see other people's.
		`DELETE FROM feed_comments WHERE author_id = $1 AND parent_id IS NOT NULL`,
		`UPDATE feed_comments c SET body = '', deleted_at = COALESCE(c.deleted_at, now()), updated_at = now()
		 WHERE c.author_id = $1 AND EXISTS (SELECT 1 FROM feed_comments r WHERE r.parent_id = c.id)`,
		`DELETE FROM feed_comments c
		 WHERE c.author_id = $1 AND NOT EXISTS (SELECT 1 FROM feed_comments r WHERE r.parent_id = c.id)`,
		`DELETE FROM feed_mentions
		 WHERE user_id = $1 OR comment_id IN (SELECT id FROM feed_comments WHERE author_id = $1)`,
		`DELETE FROM feed_post_reactions WHERE user_id = $1`,
		`DELETE FROM feed_comment_reactions WHERE user_id = $1`,
		`DELETE FROM feed_posts WHERE author_id = $1`,
		`DELETE FROM feed_seen WHERE user_id = $1`,
	}
	for _, q := range stmts {
		if _, err := tx.Exec(ctx, q, userID); err != nil {
			return err
		}
	}
	return nil
}

type exportPost struct {
	ID        uuid.UUID  `json:"id"`
	Kind      string     `json:"kind"`
	Audience  string     `json:"audience"`
	Body      string     `json:"body"`
	LinkURL   *string    `json:"link_url"`
	Media     []string   `json:"media"`
	CreatedAt time.Time  `json:"created_at"`
	EditedAt  *time.Time `json:"edited_at"`
	RemovedAt *time.Time `json:"removed_at"`
}

type exportComment struct {
	ID        uuid.UUID  `json:"id"`
	PostID    uuid.UUID  `json:"post_id"`
	ParentID  *uuid.UUID `json:"parent_id"`
	Body      string     `json:"body"`
	CreatedAt time.Time  `json:"created_at"`
	EditedAt  *time.Time `json:"edited_at"`
	DeletedAt *time.Time `json:"deleted_at"`
	RemovedAt *time.Time `json:"removed_at"`
}

type exportReaction struct {
	TargetKind string    `json:"target_kind"`
	TargetID   uuid.UUID `json:"target_id"`
	Reaction   string    `json:"reaction"`
	CreatedAt  time.Time `json:"created_at"`
}

// ExportAccount returns the user's posts, comments and reactions.
func (s *Service) ExportAccount(ctx context.Context, userID uuid.UUID) (any, error) {
	rows, err := s.d.Pool.Query(ctx, `
		SELECT p.id, p.kind, p.audience, p.body, p.link_url,
		       COALESCE(array_agg(m.key ORDER BY m.position) FILTER (WHERE m.key IS NOT NULL), '{}'),
		       p.created_at, p.edited_at, p.removed_at
		FROM feed_posts p LEFT JOIN feed_media m ON m.post_id = p.id
		WHERE p.author_id = $1
		GROUP BY p.id ORDER BY p.created_at`, userID)
	if err != nil {
		return nil, err
	}
	posts, err := pgx.CollectRows(rows, pgx.RowToStructByPos[exportPost])
	if err != nil {
		return nil, err
	}
	for i := range posts {
		for j, k := range posts[i].Media {
			posts[i].Media[j] = s.d.Storage.PublicURL(k)
		}
	}
	rows, err = s.d.Pool.Query(ctx, `
		SELECT id, post_id, parent_id, body, created_at, edited_at, deleted_at, removed_at
		FROM feed_comments WHERE author_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	comments, err := pgx.CollectRows(rows, pgx.RowToStructByPos[exportComment])
	if err != nil {
		return nil, err
	}
	rows, err = s.d.Pool.Query(ctx, `
		SELECT 'post', post_id, reaction, created_at FROM feed_post_reactions WHERE user_id = $1
		UNION ALL
		SELECT 'comment', comment_id, reaction, created_at FROM feed_comment_reactions WHERE user_id = $1
		ORDER BY 4`, userID)
	if err != nil {
		return nil, err
	}
	reactions, err := pgx.CollectRows(rows, pgx.RowToStructByPos[exportReaction])
	if err != nil {
		return nil, err
	}
	return map[string]any{"posts": posts, "comments": comments, "reactions": reactions}, nil
}

// ---- dev seed --------------------------------------------------------------

var seedNS = uuid.MustParse("6b2f0b5e-3f7a-4b55-9a3e-1f0c2d7e9a11")

func seedID(name string) uuid.UUID { return uuid.NewSHA1(seedNS, []byte("feed:"+name)) }

// SeedDev adds a few posts, comments and reactions across the dev identities
// and one pinned announcement. users: admin, sam, jordan, priya.
func (s *Service) SeedDev(ctx context.Context, users []uuid.UUID) error {
	if len(users) < 4 {
		return nil
	}
	admin, sam, jordan, priya := users[0], users[1], users[2], users[3]
	var samName string
	if err := s.d.Pool.QueryRow(ctx, `SELECT display_name FROM users WHERE id = $1`, sam).Scan(&samName); err != nil {
		return err
	}
	thanks := "Photos from last night's potluck are up. Thanks @" + samName + " for organizing!"

	type post struct {
		name, kind, body string
		author           uuid.UUID
		ago              time.Duration
		pinned           *time.Time
	}
	pin := time.Now().Add(7 * 24 * time.Hour).UTC()
	posts := []post{
		{"cleanup", KindPost, "Moved Saturday's cleanup to 10am so more people can make it. Bring gloves if you have them.", sam, 26 * time.Hour, nil},
		{"ladder", KindPost, "Does anyone have a ladder I could borrow this weekend? I'll return it Sunday evening.", jordan, 20 * time.Hour, nil},
		{"maintenance", KindAnnouncement, "Scheduled maintenance on Sunday from 2am to 3am UTC. The app is read-only during that hour.", admin, 6 * time.Hour, &pin},
		{"potluck", KindPost, thanks, priya, 3 * time.Hour, nil},
		{"reading", KindPost, "Reading group meets Thursday at 7pm. We're on chapter 4.", sam, 40 * time.Minute, nil},
	}
	for _, p := range posts {
		if _, err := s.d.Pool.Exec(ctx, `
			INSERT INTO feed_posts (id, author_id, kind, audience, body, pinned_until, created_at, updated_at)
			VALUES ($1, $2, $3, 'all', $4, $5, now() - $6::interval, now() - $6::interval)
			ON CONFLICT (id) DO NOTHING`,
			seedID("post:"+p.name), p.author, p.kind, p.body, p.pinned, fmt.Sprintf("%d seconds", int(p.ago.Seconds()))); err != nil {
			return err
		}
	}
	if i := strings.Index(thanks, "@"); i >= 0 && samName != "" {
		start := len(utf16.Encode([]rune(thanks[:i])))
		length := len(utf16.Encode([]rune("@" + samName)))
		if _, err := s.d.Pool.Exec(ctx, `
			INSERT INTO feed_mentions (id, post_id, user_id, start_idx, length) VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (id) DO NOTHING`, seedID("mention:potluck"), seedID("post:potluck"), sam, start, length); err != nil {
			return err
		}
	}

	type comment struct {
		name, post, parent, body string
		author                   uuid.UUID
		ago                      time.Duration
	}
	comments := []comment{
		{"c1", "cleanup", "", "I'll be there.", jordan, 25 * time.Hour},
		{"c2", "cleanup", "c1", "Same. I'm bringing extra gloves.", priya, 24 * time.Hour},
		{"c3", "ladder", "", "I have one. Message me and I'll drop it off.", sam, 19 * time.Hour},
		{"c4", "potluck", "", "Thanks for taking these!", jordan, 2 * time.Hour},
	}
	for _, c := range comments {
		var parent *uuid.UUID
		if c.parent != "" {
			id := seedID("comment:" + c.parent)
			parent = &id
		}
		if _, err := s.d.Pool.Exec(ctx, `
			INSERT INTO feed_comments (id, post_id, parent_id, author_id, body, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, now() - $6::interval, now() - $6::interval)
			ON CONFLICT (id) DO NOTHING`,
			seedID("comment:"+c.name), seedID("post:"+c.post), parent, c.author, c.body,
			fmt.Sprintf("%d seconds", int(c.ago.Seconds()))); err != nil {
			return err
		}
	}

	type reaction struct {
		post     string
		user     uuid.UUID
		reaction string
	}
	for _, r := range []reaction{
		{"cleanup", jordan, "like"}, {"cleanup", priya, "love"}, {"ladder", sam, "like"},
		{"potluck", sam, "love"}, {"potluck", jordan, "laugh"}, {"maintenance", sam, "like"},
		{"reading", priya, "like"},
	} {
		if _, err := s.d.Pool.Exec(ctx, `
			INSERT INTO feed_post_reactions (post_id, user_id, reaction) VALUES ($1, $2, $3)
			ON CONFLICT (post_id, user_id) DO NOTHING`, seedID("post:"+r.post), r.user, r.reaction); err != nil {
			return err
		}
	}
	_, err := s.d.Pool.Exec(ctx, `
		INSERT INTO feed_comment_reactions (comment_id, user_id, reaction) VALUES ($1, $2, 'like')
		ON CONFLICT (comment_id, user_id) DO NOTHING`, seedID("comment:c3"), jordan)
	return err
}
