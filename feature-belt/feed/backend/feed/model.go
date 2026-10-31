package feed

import (
	"context"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/moderation"
)

// Author is the public face of a post or comment author.
type Author struct {
	ID          uuid.UUID `json:"id"`
	DisplayName string    `json:"display_name"`
	AvatarURL   *string   `json:"avatar_url"`
}

// Media is one attachment on a post: an image, a video or a document.
type Media struct {
	ID          uuid.UUID `json:"id"`
	Kind        string    `json:"kind"` // image | video | file
	Key         string    `json:"key"`  // storage key; send it back when editing
	URL         string    `json:"url"`
	ContentType string    `json:"content_type"`
	Width       *int      `json:"width"`
	Height      *int      `json:"height"`
	Name        *string   `json:"name"` // documents: the file name to show
}

// Mention is a resolved @mention. Start and Length are UTF-16 offsets of the
// "@Name" token in the text (JavaScript string indexes).
type Mention struct {
	UserID      uuid.UUID `json:"user_id"`
	DisplayName string    `json:"display_name"`
	Start       int       `json:"start"`
	Length      int       `json:"length"`
}

// Post is a feed post or announcement as a viewer sees it.
type Post struct {
	ID            uuid.UUID      `json:"id"`
	Kind          string         `json:"kind"` // post | announcement
	Audience      string         `json:"audience"`
	Author        Author         `json:"author"`
	Body          string         `json:"body"`
	LinkURL       *string        `json:"link_url"`
	Media         []Media        `json:"media"`
	Mentions      []Mention      `json:"mentions"`
	Reactions     map[string]int `json:"reactions"` // reaction → count, non-zero only
	ReactionCount int            `json:"reaction_count"`
	MyReaction    *string        `json:"my_reaction"`
	CommentCount  int            `json:"comment_count"`
	PinnedUntil   *time.Time     `json:"pinned_until"`
	EditedAt      *time.Time     `json:"edited_at"`
	CreatedAt     time.Time      `json:"created_at"`
	// Staff views only.
	RemovedAt *time.Time `json:"removed_at,omitempty"`
}

// Comment statuses. Anything but visible renders as a placeholder that keeps
// a thread's replies in place; its body, author and mentions are blank.
const (
	CommentVisible = "visible"
	CommentDeleted = "deleted" // deleted by its author or the post author
	CommentRemoved = "removed" // removed by staff
	CommentHidden  = "hidden"  // written by someone the viewer has a block with
)

// Comment is one comment or reply (ParentID set). Replies are one level deep.
type Comment struct {
	ID            uuid.UUID      `json:"id"`
	PostID        uuid.UUID      `json:"post_id"`
	ParentID      *uuid.UUID     `json:"parent_id"`
	Author        *Author        `json:"author"`
	Body          string         `json:"body"`
	Mentions      []Mention      `json:"mentions"`
	Reactions     map[string]int `json:"reactions"`
	ReactionCount int            `json:"reaction_count"`
	MyReaction    *string        `json:"my_reaction"`
	Status        string         `json:"status"`
	EditedAt      *time.Time     `json:"edited_at"`
	CreatedAt     time.Time      `json:"created_at"`
}

// ReactionSummary is the response to a reaction change and the
// feed.reaction realtime payload.
type ReactionSummary struct {
	TargetKind    string         `json:"target_kind"` // post | comment
	TargetID      uuid.UUID      `json:"target_id"`
	PostID        uuid.UUID      `json:"post_id"`
	Reactions     map[string]int `json:"reactions"`
	ReactionCount int            `json:"reaction_count"`
	MyReaction    *string        `json:"my_reaction,omitempty"`
}

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

// viewer is what every read filters on: the audiences the user reads and the
// users they have a block with (either direction).
type viewer struct {
	id        uuid.UUID
	audiences []string
	blocked   []uuid.UUID
}

func (s *Service) viewerFor(ctx context.Context, userID uuid.UUID) (viewer, error) {
	auds, err := s.scope.Audiences(ctx, userID)
	if err != nil {
		return viewer{}, err
	}
	blocked, err := moderation.BlockedIDs(ctx, s.d.Pool, userID)
	if err != nil {
		return viewer{}, err
	}
	// pgx sends a nil slice as NULL, and "x = ANY(NULL)" is NULL, which would
	// filter every row out. Always send a (possibly empty) array.
	if auds == nil {
		auds = []string{}
	}
	if blocked == nil {
		blocked = []uuid.UUID{}
	}
	return viewer{id: userID, audiences: auds, blocked: blocked}, nil
}

func (v viewer) blocks(id uuid.UUID) bool {
	for _, b := range v.blocked {
		if b == id {
			return true
		}
	}
	return false
}

func (v viewer) reads(audience string) bool {
	for _, a := range v.audiences {
		if a == audience {
			return true
		}
	}
	return false
}

// hydratePosts fills media, mentions, reactions and comment counts for a
// page of posts in four queries (no per-row subqueries).
func (s *Service) hydratePosts(ctx context.Context, viewerID uuid.UUID, posts []Post) error {
	if len(posts) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(posts))
	idx := make(map[uuid.UUID]int, len(posts))
	for i := range posts {
		ids[i] = posts[i].ID
		idx[posts[i].ID] = i
		posts[i].Media = []Media{}
		posts[i].Mentions = []Mention{}
		posts[i].Reactions = map[string]int{}
	}

	rows, err := s.d.Pool.Query(ctx, `
		SELECT post_id, id, kind, key, content_type, width, height, name
		FROM feed_media WHERE post_id = ANY($1) ORDER BY post_id, position`, ids)
	if err != nil {
		return err
	}
	for rows.Next() {
		var pid uuid.UUID
		var m Media
		if err := rows.Scan(&pid, &m.ID, &m.Kind, &m.Key, &m.ContentType, &m.Width, &m.Height, &m.Name); err != nil {
			rows.Close()
			return err
		}
		m.URL = s.d.Storage.PublicURL(m.Key)
		p := &posts[idx[pid]]
		p.Media = append(p.Media, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	rows, err = s.d.Pool.Query(ctx, `
		SELECT m.post_id, m.user_id, u.display_name, m.start_idx, m.length
		FROM feed_mentions m JOIN users u ON u.id = m.user_id
		WHERE m.post_id = ANY($1) AND m.comment_id IS NULL
		ORDER BY m.post_id, m.start_idx`, ids)
	if err != nil {
		return err
	}
	for rows.Next() {
		var pid uuid.UUID
		var m Mention
		if err := rows.Scan(&pid, &m.UserID, &m.DisplayName, &m.Start, &m.Length); err != nil {
			rows.Close()
			return err
		}
		p := &posts[idx[pid]]
		p.Mentions = append(p.Mentions, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	rows, err = s.d.Pool.Query(ctx, `
		SELECT post_id, reaction, count(*), bool_or(user_id = $2)
		FROM feed_post_reactions WHERE post_id = ANY($1) GROUP BY post_id, reaction`, ids, viewerID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var pid uuid.UUID
		var reaction string
		var n int
		var mine bool
		if err := rows.Scan(&pid, &reaction, &n, &mine); err != nil {
			rows.Close()
			return err
		}
		p := &posts[idx[pid]]
		p.Reactions[reaction] = n
		p.ReactionCount += n
		if mine {
			r := reaction
			p.MyReaction = &r
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	rows, err = s.d.Pool.Query(ctx, `
		SELECT post_id, count(*) FROM feed_comments
		WHERE post_id = ANY($1) AND deleted_at IS NULL AND removed_at IS NULL
		GROUP BY post_id`, ids)
	if err != nil {
		return err
	}
	for rows.Next() {
		var pid uuid.UUID
		var n int
		if err := rows.Scan(&pid, &n); err != nil {
			rows.Close()
			return err
		}
		posts[idx[pid]].CommentCount = n
	}
	rows.Close()
	return rows.Err()
}

// hydrateComments fills mentions and reactions for visible comments.
func (s *Service) hydrateComments(ctx context.Context, viewerID uuid.UUID, comments []Comment) error {
	ids := make([]uuid.UUID, 0, len(comments))
	idx := make(map[uuid.UUID]int, len(comments))
	for i := range comments {
		comments[i].Mentions = []Mention{}
		comments[i].Reactions = map[string]int{}
		if comments[i].Status == CommentVisible {
			ids = append(ids, comments[i].ID)
			idx[comments[i].ID] = i
		}
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT m.comment_id, m.user_id, u.display_name, m.start_idx, m.length
		FROM feed_mentions m JOIN users u ON u.id = m.user_id
		WHERE m.comment_id = ANY($1) ORDER BY m.comment_id, m.start_idx`, ids)
	if err != nil {
		return err
	}
	for rows.Next() {
		var cid uuid.UUID
		var m Mention
		if err := rows.Scan(&cid, &m.UserID, &m.DisplayName, &m.Start, &m.Length); err != nil {
			rows.Close()
			return err
		}
		c := &comments[idx[cid]]
		c.Mentions = append(c.Mentions, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	rows, err = s.d.Pool.Query(ctx, `
		SELECT comment_id, reaction, count(*), bool_or(user_id = $2)
		FROM feed_comment_reactions WHERE comment_id = ANY($1) GROUP BY comment_id, reaction`, ids, viewerID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var cid uuid.UUID
		var reaction string
		var n int
		var mine bool
		if err := rows.Scan(&cid, &reaction, &n, &mine); err != nil {
			rows.Close()
			return err
		}
		c := &comments[idx[cid]]
		c.Reactions[reaction] = n
		c.ReactionCount += n
		if mine {
			r := reaction
			c.MyReaction = &r
		}
	}
	rows.Close()
	return rows.Err()
}
