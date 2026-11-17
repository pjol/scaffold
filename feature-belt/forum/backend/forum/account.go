package forum

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// DeleteAccount runs inside the account-deletion transaction:
//   - the user's posts and comments are deleted: text, link and labels
//     cleared, status "deleted" (placeholders keep other people's replies in
//     their threads);
//   - their votes are withdrawn from the scores they moved and deleted;
//   - mentions of them and their moderator seats are deleted.
//
// Votes they received stay (they are other people's), so other people's
// karma is unchanged.
func (s *Service) DeleteAccount(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	if _, err := tx.Exec(ctx, `
		UPDATE forum_posts SET status = 'deleted', title = '', body = '', url = NULL, domain = NULL, labels = '{}',
		  mod_reasons = '{}', pinned = false, removed_at = COALESCE(removed_at, now())
		WHERE author_id = $1`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE forum_posts p SET comment_count = greatest(p.comment_count - x.n, 0)
		FROM (SELECT post_id, count(*) AS n FROM forum_comments WHERE author_id = $1 AND status = 'visible' GROUP BY post_id) x
		WHERE p.id = x.post_id`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE forum_comments SET status = 'deleted', body = '', labels = '{}', mod_reasons = '{}', removed_at = COALESCE(removed_at, now())
		WHERE author_id = $1`, userID); err != nil {
		return err
	}
	// withdraw their votes: scores move back; hot is left as it was
	if _, err := tx.Exec(ctx, `
		UPDATE forum_posts p SET score = p.score - x.s, ups = p.ups - x.u, downs = p.downs - x.d
		FROM (SELECT target_id, sum(value) AS s, count(*) FILTER (WHERE value = 1) AS u, count(*) FILTER (WHERE value = -1) AS d
		      FROM forum_votes WHERE user_id = $1 AND target_kind = 'post' GROUP BY target_id) x
		WHERE p.id = x.target_id`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE forum_comments c SET score = c.score - x.s, ups = c.ups - x.u, downs = c.downs - x.d
		FROM (SELECT target_id, sum(value) AS s, count(*) FILTER (WHERE value = 1) AS u, count(*) FILTER (WHERE value = -1) AS d
		      FROM forum_votes WHERE user_id = $1 AND target_kind = 'comment' GROUP BY target_id) x
		WHERE c.id = x.target_id`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM forum_votes WHERE user_id = $1`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM forum_mentions WHERE user_id = $1`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM forum_mentions m USING forum_posts p
		WHERE m.target_kind = 'post' AND m.target_id = p.id AND p.author_id = $1`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM forum_mentions m USING forum_comments c
		WHERE m.target_kind = 'comment' AND m.target_id = c.id AND c.author_id = $1`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM forum_moderators WHERE user_id = $1`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE forum_moderators SET added_by = NULL WHERE added_by = $1`, userID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE forum_communities SET created_by = NULL WHERE created_by = $1`, userID)
	return err
}

// ExportAccount returns the user's posts, comments, votes and karma.
func (s *Service) ExportAccount(ctx context.Context, userID uuid.UUID) (any, error) {
	type post struct {
		ID        uuid.UUID  `json:"id"`
		Community string     `json:"community"`
		Title     string     `json:"title"`
		Body      string     `json:"body"`
		URL       *string    `json:"url"`
		Status    string     `json:"status"`
		Score     int        `json:"score"`
		CreatedAt time.Time  `json:"created_at"`
		EditedAt  *time.Time `json:"edited_at"`
	}
	type comment struct {
		ID        uuid.UUID  `json:"id"`
		PostID    uuid.UUID  `json:"post_id"`
		ParentID  *uuid.UUID `json:"parent_id"`
		Body      string     `json:"body"`
		Status    string     `json:"status"`
		Score     int        `json:"score"`
		CreatedAt time.Time  `json:"created_at"`
		EditedAt  *time.Time `json:"edited_at"`
	}
	type vote struct {
		TargetKind string    `json:"target_kind"`
		TargetID   uuid.UUID `json:"target_id"`
		Value      int       `json:"value"`
		CreatedAt  time.Time `json:"created_at"`
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT p.id, c.slug, p.title, p.body, p.url, p.status, p.score, p.created_at, p.edited_at
		FROM forum_posts p JOIN forum_communities c ON c.id = p.community_id WHERE p.author_id = $1 ORDER BY p.created_at`, userID)
	if err != nil {
		return nil, err
	}
	posts, err := pgx.CollectRows(rows, pgx.RowToStructByPos[post])
	if err != nil {
		return nil, err
	}
	rows, err = s.d.Pool.Query(ctx, `SELECT id, post_id, parent_id, body, status, score, created_at, edited_at
		FROM forum_comments WHERE author_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	comments, err := pgx.CollectRows(rows, pgx.RowToStructByPos[comment])
	if err != nil {
		return nil, err
	}
	rows, err = s.d.Pool.Query(ctx, `SELECT target_kind, target_id, value, created_at FROM forum_votes WHERE user_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	votes, err := pgx.CollectRows(rows, pgx.RowToStructByPos[vote])
	if err != nil {
		return nil, err
	}
	karma, err := Karma(ctx, s.d.Pool, userID, time.Time{})
	if err != nil {
		return nil, err
	}
	if posts == nil {
		posts = []post{}
	}
	if comments == nil {
		comments = []comment{}
	}
	if votes == nil {
		votes = []vote{}
	}
	return map[string]any{"posts": posts, "comments": comments, "votes": votes, "karma": karma}, nil
}
