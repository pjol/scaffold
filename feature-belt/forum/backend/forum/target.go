package forum

import (
	"context"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/moderation"
)

// Posts and comments as moderation targets: reports snapshot them, the
// queue removes (Remove, idempotent) and approves or restores them
// (Restorer), and Changed announces the result after the staff transaction.

type postTarget struct{ s *Service }

func parseTargetID(id string) (uuid.UUID, error) {
	u, err := uuid.Parse(id)
	if err != nil {
		return uuid.Nil, httpx.ErrNotFound
	}
	return u, nil
}

func (t postTarget) Snapshot(ctx context.Context, id string) (moderation.Snapshot, error) {
	uid, err := parseTargetID(id)
	if err != nil {
		return moderation.Snapshot{}, err
	}
	p, err := t.s.loadPost(ctx, t.s.d.Pool, uid, false)
	if err != nil {
		return moderation.Snapshot{}, err
	}
	if p.Status == StatusDeleted {
		return moderation.Snapshot{}, httpx.ErrNotFound
	}
	text := strings.TrimSpace(p.Title + "\n\n" + p.Body)
	if p.URL != nil {
		text += "\n\n" + *p.URL
	}
	author := p.AuthorID
	return moderation.Snapshot{AuthorID: &author, Text: text, MediaKeys: []string{}, Link: postPath(p.ID)}, nil
}

func (t postTarget) Remove(ctx context.Context, tx pgx.Tx, id string) error {
	uid, err := parseTargetID(id)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE forum_posts SET status = 'removed', removed_at = COALESCE(removed_at, now()), pinned = false
		WHERE id = $1 AND status <> 'deleted'`, uid)
	return err
}

func (t postTarget) Restore(ctx context.Context, tx pgx.Tx, id string) error {
	uid, err := parseTargetID(id)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE forum_posts SET status = 'visible', removed_at = NULL, mod_reasons = '{}'
		WHERE id = $1 AND status IN ('held', 'removed')`, uid)
	return err
}

// Changed announces the new status; a first approval sends the mention
// notifications held back while the post was held.
func (t postTarget) Changed(ctx context.Context, id string, restored bool) {
	uid, err := parseTargetID(id)
	if err != nil {
		return
	}
	p, err := t.s.loadPost(ctx, t.s.d.Pool, uid, false)
	if err != nil {
		return
	}
	t.s.publish(ctx, p.ID, EventStatus, map[string]any{"post_id": p.ID, "target_kind": "post", "target_id": p.ID, "status": p.Status})
	if !restored || p.Status != StatusVisible {
		return
	}
	tag, err := t.s.d.Pool.Exec(ctx, `UPDATE forum_posts SET notified = true WHERE id = $1 AND NOT notified`, p.ID)
	if err != nil || tag.RowsAffected() == 0 {
		return
	}
	mentions, err := t.s.mentionedUsers(ctx, t.s.d.Pool, "post", p.ID)
	if err != nil {
		slog.WarnContext(ctx, "forum: mentions after approval", "err", err)
		return
	}
	t.s.afterVisiblePost(ctx, p, mentions)
}

type commentTarget struct{ s *Service }

func (t commentTarget) Snapshot(ctx context.Context, id string) (moderation.Snapshot, error) {
	uid, err := parseTargetID(id)
	if err != nil {
		return moderation.Snapshot{}, err
	}
	c, err := t.s.loadComment(ctx, t.s.d.Pool, uid, false)
	if err != nil {
		return moderation.Snapshot{}, err
	}
	if c.Status == StatusDeleted {
		return moderation.Snapshot{}, httpx.ErrNotFound
	}
	author := c.AuthorID
	return moderation.Snapshot{AuthorID: &author, Text: c.Body, MediaKeys: []string{},
		Link: postPath(c.PostID) + "?comment=" + c.ID.String()}, nil
}

// setCommentStatus moves a comment to status and keeps its post's
// comment_count to visible comments.
func setCommentStatus(ctx context.Context, tx pgx.Tx, id uuid.UUID, status string, from ...string) error {
	var (
		postID uuid.UUID
		prev   string
	)
	err := tx.QueryRow(ctx, `SELECT post_id, status FROM forum_comments WHERE id = $1 FOR UPDATE`, id).Scan(&postID, &prev)
	if err != nil {
		return err
	}
	allowed := false
	for _, f := range from {
		if prev == f {
			allowed = true
		}
	}
	if !allowed || prev == status {
		return nil
	}
	removedAt := "COALESCE(removed_at, now())"
	reasons := "mod_reasons"
	if status == StatusVisible {
		removedAt, reasons = "NULL", "'{}'"
	}
	if _, err := tx.Exec(ctx, `UPDATE forum_comments SET status = $2, removed_at = `+removedAt+`, mod_reasons = `+reasons+` WHERE id = $1`, id, status); err != nil {
		return err
	}
	delta := 0
	switch {
	case prev == StatusVisible && status != StatusVisible:
		delta = -1
	case prev != StatusVisible && status == StatusVisible:
		delta = 1
	}
	if delta != 0 {
		_, err = tx.Exec(ctx, `UPDATE forum_posts SET comment_count = greatest(comment_count + $2, 0) WHERE id = $1`, postID, delta)
	}
	return err
}

func (t commentTarget) Remove(ctx context.Context, tx pgx.Tx, id string) error {
	uid, err := parseTargetID(id)
	if err != nil {
		return err
	}
	return setCommentStatus(ctx, tx, uid, StatusRemoved, StatusVisible, StatusHeld)
}

func (t commentTarget) Restore(ctx context.Context, tx pgx.Tx, id string) error {
	uid, err := parseTargetID(id)
	if err != nil {
		return err
	}
	return setCommentStatus(ctx, tx, uid, StatusVisible, StatusHeld, StatusRemoved)
}

// Changed announces the new status; a first approval sends the reply,
// comment and mention notifications held back while it was held.
func (t commentTarget) Changed(ctx context.Context, id string, restored bool) {
	uid, err := parseTargetID(id)
	if err != nil {
		return
	}
	c, err := t.s.loadComment(ctx, t.s.d.Pool, uid, false)
	if err != nil {
		return
	}
	t.s.publish(ctx, c.PostID, EventStatus, map[string]any{"post_id": c.PostID, "target_kind": "comment", "target_id": c.ID, "status": c.Status})
	if !restored || c.Status != StatusVisible {
		return
	}
	tag, err := t.s.d.Pool.Exec(ctx, `UPDATE forum_comments SET notified = true WHERE id = $1 AND NOT notified`, c.ID)
	if err != nil || tag.RowsAffected() == 0 {
		return
	}
	p, err := t.s.loadPost(ctx, t.s.d.Pool, c.PostID, false)
	if err != nil {
		return
	}
	var parent *commentRow
	if c.ParentID != nil {
		if pc, err := t.s.loadComment(ctx, t.s.d.Pool, *c.ParentID, false); err == nil {
			parent = &pc
		}
	}
	mentions, err := t.s.mentionedUsers(ctx, t.s.d.Pool, "comment", c.ID)
	if err != nil {
		slog.WarnContext(ctx, "forum: mentions after approval", "err", err)
		return
	}
	t.s.afterVisibleComment(ctx, p, c, parent, mentions, p.CommentCount)
}
