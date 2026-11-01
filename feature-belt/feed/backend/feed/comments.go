package feed

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/notify"
)

const commentCols = `c.id, c.post_id, c.parent_id, c.author_id, u.display_name, u.avatar_url, c.body, c.edited_at, c.created_at`

// listCommentsHandler returns a post's comments and replies in one flat
// list, oldest first, keyset on (created_at, id). A reply always sorts after
// its parent, so clients thread by parent_id as pages arrive. Deleted,
// removed and hidden comments appear only as placeholders for replies that
// are still visible.
func (s *Service) listCommentsHandler(w http.ResponseWriter, r *http.Request) error {
	caller := auth.MustFromContext(r.Context())
	ctx := r.Context()
	pid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	v, err := s.viewerFor(ctx, caller.UserID)
	if err != nil {
		return err
	}
	if _, err := s.visiblePost(ctx, s.d.Pool, v, pid); err != nil {
		return err
	}
	p := httpx.ParsePage(r)
	var c cursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT `+commentCols+`,
		  CASE WHEN c.removed_at IS NOT NULL THEN 'removed'
		       WHEN c.deleted_at IS NOT NULL THEN 'deleted'
		       WHEN c.author_id = ANY($2) THEN 'hidden'
		       ELSE 'visible' END
		FROM feed_comments c JOIN users u ON u.id = c.author_id
		WHERE c.post_id = $1
		  AND ($3::timestamptz IS NULL OR (c.created_at, c.id) > ($3, $4::uuid))
		  AND ((c.deleted_at IS NULL AND c.removed_at IS NULL AND NOT (c.author_id = ANY($2)))
		       OR (c.parent_id IS NULL AND EXISTS (
		            SELECT 1 FROM feed_comments r WHERE r.parent_id = c.id
		              AND r.deleted_at IS NULL AND r.removed_at IS NULL AND NOT (r.author_id = ANY($2)))))
		ORDER BY c.created_at, c.id LIMIT $5`, pid, v.blocked, nullTime(c.T), c.ID, p.Limit+1)
	if err != nil {
		return err
	}
	comments, err := scanComments(rows)
	if err != nil {
		return err
	}
	page := httpx.NewPage(comments, p.Limit, func(c Comment) any { return cursor{c.CreatedAt, c.ID} })
	if err := s.hydrateComments(ctx, caller.UserID, page.Items); err != nil {
		return err
	}
	httpx.OK(w, page)
	return nil
}

func scanComments(rows pgx.Rows) ([]Comment, error) {
	defer rows.Close()
	out := []Comment{}
	for rows.Next() {
		var c Comment
		var a Author
		if err := rows.Scan(&c.ID, &c.PostID, &c.ParentID, &a.ID, &a.DisplayName, &a.AvatarURL,
			&c.Body, &c.EditedAt, &c.CreatedAt, &c.Status); err != nil {
			return nil, err
		}
		if c.Status == CommentVisible {
			c.Author = &a
		} else {
			c.Body, c.EditedAt = "", nil
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Service) loadComment(ctx context.Context, id, viewerID uuid.UUID) (Comment, error) {
	rows, err := s.d.Pool.Query(ctx, `
		SELECT `+commentCols+`, 'visible'::text FROM feed_comments c JOIN users u ON u.id = c.author_id
		WHERE c.id = $1 AND c.deleted_at IS NULL AND c.removed_at IS NULL`, id)
	if err != nil {
		return Comment{}, err
	}
	list, err := scanComments(rows)
	if err != nil {
		return Comment{}, err
	}
	if len(list) == 0 {
		return Comment{}, httpx.NotFound("That comment isn't available.")
	}
	if err := s.hydrateComments(ctx, viewerID, list); err != nil {
		return Comment{}, err
	}
	return list[0], nil
}

func (s *Service) commentCount(ctx context.Context, postID uuid.UUID) int {
	var n int
	_ = s.d.Pool.QueryRow(ctx, `
		SELECT count(*) FROM feed_comments WHERE post_id = $1 AND deleted_at IS NULL AND removed_at IS NULL`, postID).Scan(&n)
	return n
}

// ---- create ----------------------------------------------------------------

type commentReq struct {
	Body     string         `json:"body"`
	ParentID *uuid.UUID     `json:"parent_id"` // reply to this comment; replies to a reply attach to its parent
	Mentions []mentionInput `json:"mentions"`
}

func (s *Service) createCommentHandler(w http.ResponseWriter, r *http.Request) error {
	caller := auth.MustFromContext(r.Context())
	ctx := r.Context()
	pid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req commentReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if strings.TrimSpace(req.Body) == "" {
		return httpx.BadRequest("Write a comment first.")
	}
	if err := checkText(ctx, req.Body, s.cfg.commentMax, "Your comment"); err != nil {
		return err
	}
	v, err := s.viewerFor(ctx, caller.UserID)
	if err != nil {
		return err
	}
	post, err := s.visiblePost(ctx, s.d.Pool, v, pid)
	if err != nil {
		return err
	}
	var parentID *uuid.UUID
	var replyTo uuid.UUID // author of the comment being answered
	if req.ParentID != nil {
		var parentPost, parentAuthor uuid.UUID
		var grandparent *uuid.UUID
		err := s.d.Pool.QueryRow(ctx, `
			SELECT post_id, parent_id, author_id FROM feed_comments
			WHERE id = $1 AND deleted_at IS NULL AND removed_at IS NULL`, *req.ParentID).
			Scan(&parentPost, &grandparent, &parentAuthor)
		if db.IsNoRows(err) || (err == nil && (parentPost != pid || v.blocks(parentAuthor))) {
			return httpx.NotFound("That comment isn't available.")
		}
		if err != nil {
			return err
		}
		top := *req.ParentID
		if grandparent != nil {
			top = *grandparent
		}
		parentID, replyTo = &top, parentAuthor
	}
	mentions, err := s.resolveMentions(ctx, caller.UserID, req.Body, req.Mentions)
	if err != nil {
		return err
	}
	var cid uuid.UUID
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO feed_comments (post_id, parent_id, author_id, body) VALUES ($1, $2, $3, $4) RETURNING id`,
			pid, parentID, caller.UserID, req.Body).Scan(&cid); err != nil {
			return err
		}
		return insertMentions(ctx, tx, pid, &cid, mentions)
	})
	if err != nil {
		return err
	}
	c, err := s.loadComment(ctx, cid, caller.UserID)
	if err != nil {
		return err
	}
	s.publish(ctx, post.audience, EventComment, map[string]any{
		"post_id": pid, "comment_id": cid, "parent_id": parentID, "author_id": caller.UserID,
		"comment_count": s.commentCount(ctx, pid), "deleted": false,
	})

	name := actorName(caller.DisplayName)
	link := "/feed/posts/" + pid.String()
	data := map[string]any{"post_id": pid, "comment_id": cid}
	body := preview(req.Body)
	s.afterCommit(ctx, func(ctx context.Context) {
		// One notification per person: a reply beats a comment beats a mention.
		sent := map[uuid.UUID]bool{caller.UserID: true}
		send := func(to uuid.UUID, kind, title string) {
			if sent[to] {
				return
			}
			sent[to] = true
			s.notifyUser(ctx, caller.UserID, notify.Notification{UserID: to, Kind: kind, Title: title, Body: body, Link: link, Data: data})
		}
		if parentID != nil {
			send(replyTo, NotifyReply, name+" replied to your comment")
		}
		send(post.authorID, NotifyComment, name+" commented on your post")
		for _, m := range mentions {
			send(m.userID, NotifyMention, name+" mentioned you")
		}
	})
	httpx.JSON(w, http.StatusCreated, c)
	return nil
}

// ---- edit ------------------------------------------------------------------

type updateCommentReq struct {
	Body     string         `json:"body"`
	Mentions []mentionInput `json:"mentions"`
}

func (s *Service) updateCommentHandler(w http.ResponseWriter, r *http.Request) error {
	caller := auth.MustFromContext(r.Context())
	ctx := r.Context()
	cid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req updateCommentReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if strings.TrimSpace(req.Body) == "" {
		return httpx.BadRequest("Write a comment first.")
	}
	if err := checkText(ctx, req.Body, s.cfg.commentMax, "Your comment"); err != nil {
		return err
	}
	mentions, err := s.resolveMentions(ctx, caller.UserID, req.Body, req.Mentions)
	if err != nil {
		return err
	}
	var postID uuid.UUID
	var audience string
	var added []mentionRow
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			SELECT c.post_id, p.audience FROM feed_comments c JOIN feed_posts p ON p.id = c.post_id
			WHERE c.id = $1 AND c.author_id = $2 AND c.deleted_at IS NULL AND c.removed_at IS NULL
			  AND p.removed_at IS NULL
			FOR UPDATE OF c`, cid, caller.UserID).Scan(&postID, &audience); err != nil {
			if db.IsNoRows(err) {
				return httpx.NotFound("That comment isn't available.")
			}
			return err
		}
		rows, err := tx.Query(ctx, `DELETE FROM feed_mentions WHERE comment_id = $1 RETURNING user_id`, cid)
		if err != nil {
			return err
		}
		before, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		if err != nil {
			return err
		}
		had := map[uuid.UUID]bool{}
		for _, u := range before {
			had[u] = true
		}
		for _, m := range mentions {
			if !had[m.userID] {
				added = append(added, m)
			}
		}
		if err := insertMentions(ctx, tx, postID, &cid, mentions); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE feed_comments SET body = $2, edited_at = now(), updated_at = now() WHERE id = $1`, cid, req.Body)
		return err
	})
	if err != nil {
		return err
	}
	c, err := s.loadComment(ctx, cid, caller.UserID)
	if err != nil {
		return err
	}
	s.publish(ctx, audience, EventComment, map[string]any{
		"post_id": postID, "comment_id": cid, "parent_id": c.ParentID, "author_id": caller.UserID,
		"comment_count": s.commentCount(ctx, postID), "deleted": false,
	})
	name := actorName(caller.DisplayName)
	s.afterCommit(ctx, func(ctx context.Context) {
		for _, m := range added {
			s.notifyUser(ctx, caller.UserID, notify.Notification{
				UserID: m.userID, Kind: NotifyMention, Title: name + " mentioned you", Body: preview(req.Body),
				Link: "/feed/posts/" + postID.String(), Data: map[string]any{"post_id": postID, "comment_id": cid},
			})
		}
	})
	httpx.OK(w, c)
	return nil
}

// ---- delete ----------------------------------------------------------------

// deleteCommentHandler lets a comment's author, or the author of the post it
// is on, delete it. A comment with replies keeps a placeholder so the thread
// stays in place; one without is removed outright.
func (s *Service) deleteCommentHandler(w http.ResponseWriter, r *http.Request) error {
	caller := auth.MustFromContext(r.Context())
	ctx := r.Context()
	cid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var postID uuid.UUID
	var parentID *uuid.UUID
	var audience string
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var author, postAuthor uuid.UUID
		if err := tx.QueryRow(ctx, `
			SELECT c.post_id, c.parent_id, c.author_id, p.author_id, p.audience
			FROM feed_comments c JOIN feed_posts p ON p.id = c.post_id
			WHERE c.id = $1 AND c.deleted_at IS NULL AND c.removed_at IS NULL
			FOR UPDATE OF c`, cid).Scan(&postID, &parentID, &author, &postAuthor, &audience); err != nil {
			if db.IsNoRows(err) {
				return httpx.NotFound("That comment isn't available.")
			}
			return err
		}
		if caller.UserID != author && caller.UserID != postAuthor {
			return httpx.Forbidden("You can only delete your own comments, or comments on your posts.")
		}
		return deleteComment(ctx, tx, cid)
	})
	if err != nil {
		return err
	}
	s.publish(ctx, audience, EventComment, map[string]any{
		"post_id": postID, "comment_id": cid, "parent_id": parentID,
		"comment_count": s.commentCount(ctx, postID), "deleted": true,
	})
	httpx.NoContent(w)
	return nil
}

// deleteComment soft-deletes a comment that has replies (body cleared) and
// hard-deletes one that has none.
func deleteComment(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	tag, err := tx.Exec(ctx, `
		UPDATE feed_comments SET body = '', deleted_at = now(), updated_at = now()
		WHERE id = $1 AND EXISTS (SELECT 1 FROM feed_comments r WHERE r.parent_id = $1)`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		_, err = tx.Exec(ctx, `DELETE FROM feed_mentions WHERE comment_id = $1`, id)
		return err
	}
	_, err = tx.Exec(ctx, `DELETE FROM feed_comments WHERE id = $1`, id)
	return err
}

// removeComment hides a comment on staff action, keeping the row and its
// text for the moderation record.
func removeComment(ctx context.Context, q db.Querier, id uuid.UUID, by *uuid.UUID) (bool, error) {
	tag, err := q.Exec(ctx, `
		UPDATE feed_comments SET removed_at = $2, removed_by = $3, updated_at = now()
		WHERE id = $1 AND removed_at IS NULL`, id, time.Now().UTC(), by)
	return tag.RowsAffected() > 0, err
}
