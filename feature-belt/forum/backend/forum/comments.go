package forum

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/audit"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/moderation"
)

const commentCols = `m.id, m.post_id, m.parent_id, m.depth, m.author_id, u.display_name, u.avatar_url, m.body, m.labels,
	m.mod_reasons, m.status, m.score, m.created_at, m.edited_at`

const commentFrom = ` FROM forum_comments m JOIN users u ON u.id = m.author_id `

func scanComment(row pgx.Row) (commentRow, error) {
	var c commentRow
	err := row.Scan(&c.ID, &c.PostID, &c.ParentID, &c.Depth, &c.AuthorID, &c.AuthorName, &c.AuthorAvatar, &c.Body, &c.Labels,
		&c.ModReasons, &c.Status, &c.Score, &c.CreatedAt, &c.EditedAt)
	return c, err
}

func (s *Service) loadComment(ctx context.Context, q db.Querier, id uuid.UUID, lock bool) (commentRow, error) {
	sql := `SELECT ` + commentCols + commentFrom + ` WHERE m.id = $1`
	if lock {
		sql += ` FOR UPDATE OF m`
	}
	c, err := scanComment(q.QueryRow(ctx, sql, id))
	if db.IsNoRows(err) {
		return c, httpx.NotFound("That comment doesn't exist.")
	}
	return c, err
}

// maxCommentsLoaded bounds one post's tree; beyond it the oldest are shown.
const maxCommentsLoaded = 5000

func (s *Service) loadComments(ctx context.Context, postID uuid.UUID) ([]commentRow, error) {
	rows, err := s.d.Pool.Query(ctx, `SELECT `+commentCols+commentFrom+` WHERE m.post_id = $1 ORDER BY m.created_at, m.id LIMIT $2`,
		postID, maxCommentsLoaded)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []commentRow{}
	for rows.Next() {
		c, err := scanComment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// tree builds the viewer's comment tree of a post (or of one comment's replies).
func (s *Service) tree(ctx context.Context, v viewer, postID uuid.UUID, sort string, root *uuid.UUID) ([]Comment, error) {
	rows, err := s.loadComments(ctx, postID)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	votes, err := s.viewerVotes(ctx, v.id, "comment", ids)
	if err != nil {
		return nil, err
	}
	mentions, err := s.mentionsFor(ctx, "comment", ids)
	if err != nil {
		return nil, err
	}
	return buildTree(rows, treeOpts{Sort: sort, Viewer: v.id, Staff: v.staff, Blocked: v.blockedSet, Votes: votes,
		Mentions: mentions, MaxChildren: MaxChildren, Root: root}), nil
}

func validSort(sort string) (string, error) {
	switch sort {
	case "":
		return "top", nil
	case "top", "new", "old":
		return sort, nil
	}
	return "", httpx.BadRequest("sort must be top, new or old.")
}

// listCommentsHandler: GET /v1/forum/posts/{id}/comments?sort=top|new|old&cursor=&limit=
// pages the top-level comments, each with its replies.
func (s *Service) listCommentsHandler(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	sort, err := validSort(r.URL.Query().Get("sort"))
	if err != nil {
		return err
	}
	v, err := s.viewerOf(ctx, auth.MustFromContext(ctx))
	if err != nil {
		return err
	}
	p, err := s.loadPost(ctx, s.d.Pool, id, false)
	if err != nil {
		return err
	}
	if !v.canSeePost(p) {
		return httpx.NotFound("That post doesn't exist.")
	}
	all, err := s.tree(ctx, v, id, sort, nil)
	if err != nil {
		return err
	}
	return writeCommentPage(w, r, all, sort)
}

func writeCommentPage(w http.ResponseWriter, r *http.Request, all []Comment, sort string) error {
	pp := httpx.ParsePage(r)
	var after *commentCursor
	if pp.Cursor != "" {
		after = &commentCursor{}
		if err := httpx.DecodeCursor(pp.Cursor, after); err != nil {
			return err
		}
	}
	items, next := pageComments(all, sort, after, pp.Limit)
	page := httpx.Page[Comment]{Items: items}
	if next != nil {
		page.NextCursor = httpx.EncodeCursor(next)
	}
	httpx.OK(w, page)
	return nil
}

// repliesHandler: GET /v1/forum/comments/{id}/replies?cursor= pages one
// comment's direct replies (oldest first), each with its replies: "more
// replies" and threads deeper than a client shows.
func (s *Service) repliesHandler(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	v, err := s.viewerOf(ctx, auth.MustFromContext(ctx))
	if err != nil {
		return err
	}
	c, err := s.loadComment(ctx, s.d.Pool, id, false)
	if err != nil {
		return err
	}
	p, err := s.loadPost(ctx, s.d.Pool, c.PostID, false)
	if err != nil {
		return err
	}
	if !v.canSeePost(p) {
		return httpx.NotFound("That comment doesn't exist.")
	}
	all, err := s.tree(ctx, v, c.PostID, "old", &id)
	if err != nil {
		return err
	}
	return writeCommentPage(w, r, all, "old")
}

// commentView renders one comment for its author right after a write.
func (s *Service) commentView(ctx context.Context, v viewer, c commentRow) (Comment, error) {
	votes, err := s.viewerVotes(ctx, v.id, "comment", []uuid.UUID{c.ID})
	if err != nil {
		return Comment{}, err
	}
	mentions, err := s.mentionsFor(ctx, "comment", []uuid.UUID{c.ID})
	if err != nil {
		return Comment{}, err
	}
	out, ok := renderOne(c, treeOpts{Viewer: v.id, Staff: true, Votes: votes, Mentions: mentions})
	if !ok {
		return Comment{}, httpx.NotFound("That comment doesn't exist.")
	}
	return out, nil
}

// ---- create ----

type createCommentReq struct {
	Body     string      `json:"body"`
	ParentID *uuid.UUID  `json:"parent_id"`
	Mentions []uuid.UUID `json:"mentions"`
}

func (s *Service) validCommentBody(b string) (string, error) {
	b = strings.TrimSpace(b)
	if n := utf8.RuneCountInString(b); n == 0 || n > s.cfg.commentMax {
		return "", httpx.BadRequest(fmt.Sprintf("A comment needs 1 to %d characters.", s.cfg.commentMax))
	}
	return b, nil
}

// createCommentHandler: POST /v1/forum/posts/{id}/comments. Screened; a
// reply to a comment at depth 8 attaches beside it.
func (s *Service) createCommentHandler(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	caller := auth.MustFromContext(ctx)
	postID, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req createCommentReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	body, err := s.validCommentBody(req.Body)
	if err != nil {
		return err
	}
	v, err := s.viewerOf(ctx, caller)
	if err != nil {
		return err
	}
	p, err := s.loadPost(ctx, s.d.Pool, postID, false)
	if err != nil {
		return err
	}
	if p.Status != StatusVisible && !(p.Status == StatusHeld && (p.AuthorID == v.id || v.staff)) {
		return httpx.NotFound("That post doesn't exist.")
	}
	if v.blocks(p.AuthorID) {
		return errBlocked
	}
	mod, err := s.canModerate(ctx, v, p.Community.ID)
	if err != nil {
		return err
	}
	comm, err := s.communityByID(ctx, s.d.Pool, p.Community.ID)
	if err != nil {
		return err
	}
	if p.Locked && !mod {
		return errPostLocked
	}
	if comm.Locked && !mod {
		return errCommunityLocked
	}
	var parent *commentRow
	if req.ParentID != nil {
		pc, err := s.loadComment(ctx, s.d.Pool, *req.ParentID, false)
		if err != nil {
			return err
		}
		if pc.PostID != postID || pc.Status == StatusDeleted || pc.Status == StatusRemoved {
			return httpx.NotFound("That comment doesn't exist.")
		}
		if v.blocks(pc.AuthorID) {
			return errBlocked
		}
		parent = &pc
	}
	if err := moderation.CheckPosting(ctx, s.d.Pool, caller.UserID); err != nil { // the clearer refusal first
		return err
	}
	if err := s.checkRate(ctx, "forum_comments", caller.UserID, 30, 10, "comments"); err != nil {
		return err
	}
	mentions, err := s.validMentions(ctx, v, body, req.Mentions)
	if err != nil {
		return err
	}
	k, err := Karma(ctx, s.d.Pool, caller.UserID, time.Time{})
	if err != nil {
		return err
	}
	parentID, depth := depthFor(parent)
	parentText := p.Title
	if parent != nil {
		parentText = parent.Body
	}
	id := uuid.New()
	link := postPath(postID) + "?comment=" + id.String()
	dec, err := moderation.Screen(ctx, s.d.Pool, moderation.Item{Kind: TargetComment, ID: id.String(), AuthorID: caller.UserID,
		Body: body, Parent: parentText, Place: s.placeWords(p.Community, comm.Description), Link: link, Karma: &k})
	if err != nil {
		return err
	}
	labels := mergeLabels(dec.Labels, s.appLabels(ctx, LabelInput{Kind: "comment", AuthorID: caller.UserID, Community: comm, Body: body}))
	status := dec.Status()
	var count int
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO forum_comments (id, post_id, parent_id, depth, author_id, body, labels, mod_reasons, status, notified)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			id, postID, parentID, depth, caller.UserID, body, labels, dec.Reasons(), status, status == StatusVisible); err != nil {
			return err
		}
		if status == StatusVisible {
			if err := tx.QueryRow(ctx, `UPDATE forum_posts SET comment_count = comment_count + 1 WHERE id = $1 RETURNING comment_count`,
				postID).Scan(&count); err != nil {
				return err
			}
		}
		if err := saveMentions(ctx, tx, "comment", id, mentions); err != nil {
			return err
		}
		return moderation.Record(ctx, tx, dec)
	})
	if err != nil {
		return err
	}
	c, err := s.loadComment(ctx, s.d.Pool, id, false)
	if err != nil {
		return err
	}
	if status == StatusVisible {
		s.afterVisibleComment(ctx, p, c, parent, mentions, count)
	}
	out, err := s.commentView(ctx, v, c)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, CommentResult{Comment: out, Moderation: dec.Outcome()})
	return nil
}

// ---- edit / delete ----

type editCommentReq struct {
	Body     string       `json:"body"`
	Mentions *[]uuid.UUID `json:"mentions"`
}

// editCommentHandler: PATCH /v1/forum/comments/{id}. Re-screened.
func (s *Service) editCommentHandler(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	caller := auth.MustFromContext(ctx)
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req editCommentReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	c, err := s.loadComment(ctx, s.d.Pool, id, false)
	if err != nil {
		return err
	}
	if c.AuthorID != caller.UserID {
		return httpx.NotFound("That comment doesn't exist.")
	}
	if err := editableStatus(c.Status, "comment"); err != nil {
		return err
	}
	body, err := s.validCommentBody(req.Body)
	if err != nil {
		return err
	}
	v, err := s.viewerOf(ctx, caller)
	if err != nil {
		return err
	}
	var mentions []uuid.UUID
	if req.Mentions != nil {
		if mentions, err = s.validMentions(ctx, v, body, *req.Mentions); err != nil {
			return err
		}
	}
	p, err := s.loadPost(ctx, s.d.Pool, c.PostID, false)
	if err != nil {
		return err
	}
	comm, err := s.communityByID(ctx, s.d.Pool, p.Community.ID)
	if err != nil {
		return err
	}
	parentText := p.Title
	if c.ParentID != nil {
		if pc, err := s.loadComment(ctx, s.d.Pool, *c.ParentID, false); err == nil {
			parentText = pc.Body
		}
	}
	k, err := Karma(ctx, s.d.Pool, caller.UserID, time.Time{})
	if err != nil {
		return err
	}
	dec, err := moderation.Screen(ctx, s.d.Pool, moderation.Item{Kind: TargetComment, ID: id.String(), AuthorID: caller.UserID,
		Body: body, Parent: parentText, Place: s.placeWords(p.Community, comm.Description),
		Link: postPath(c.PostID) + "?comment=" + id.String(), Karma: &k, Edit: true})
	if err != nil {
		return err
	}
	var status string
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		cur, err := s.loadComment(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if err := editableStatus(cur.Status, "comment"); err != nil {
			return err
		}
		status = cur.Status
		if dec.Status() != StatusVisible {
			status = dec.Status()
		}
		if _, err := tx.Exec(ctx, `UPDATE forum_comments SET body = $2, labels = $3, status = $4, mod_reasons = $5, edited_at = now()
			WHERE id = $1`, id, body, mergeLabels(cur.Labels, dec.Labels), status, dec.Reasons()); err != nil {
			return err
		}
		if status != StatusVisible {
			if _, err := tx.Exec(ctx, `UPDATE forum_posts SET comment_count = greatest(comment_count - 1, 0) WHERE id = $1`, c.PostID); err != nil {
				return err
			}
		}
		if req.Mentions != nil {
			if err := saveMentions(ctx, tx, "comment", id, mentions); err != nil {
				return err
			}
		}
		return moderation.Record(ctx, tx, dec)
	})
	if err != nil {
		return err
	}
	if c, err = s.loadComment(ctx, s.d.Pool, id, false); err != nil {
		return err
	}
	s.publish(ctx, c.PostID, EventStatus, map[string]any{"post_id": c.PostID, "target_kind": "comment", "target_id": id, "status": status})
	out, err := s.commentView(ctx, v, c)
	if err != nil {
		return err
	}
	httpx.OK(w, CommentResult{Comment: out, Moderation: dec.Outcome()})
	return nil
}

// deleteCommentHandler: DELETE /v1/forum/comments/{id}: the author's comment
// becomes a placeholder while it has replies.
func (s *Service) deleteCommentHandler(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	caller := auth.MustFromContext(ctx)
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var postID uuid.UUID
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		c, err := s.loadComment(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if c.AuthorID != caller.UserID || c.Status == StatusDeleted {
			return httpx.NotFound("That comment doesn't exist.")
		}
		postID = c.PostID
		if _, err := tx.Exec(ctx, `UPDATE forum_comments SET status = 'deleted', removed_at = now() WHERE id = $1`, id); err != nil {
			return err
		}
		if c.Status == StatusVisible {
			_, err = tx.Exec(ctx, `UPDATE forum_posts SET comment_count = greatest(comment_count - 1, 0) WHERE id = $1`, c.PostID)
		}
		return err
	})
	if err != nil {
		return err
	}
	s.publish(ctx, postID, EventStatus, map[string]any{"post_id": postID, "target_kind": "comment", "target_id": id, "status": StatusDeleted})
	httpx.NoContent(w)
	return nil
}

// moderateCommentHandler: POST /v1/forum/comments/{id}/moderate {action:
// remove|restore}. Staff and the community's moderators remove; staff
// restore when no moderation case is open. Audited.
func (s *Service) moderateCommentHandler(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	caller := auth.MustFromContext(ctx)
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req moderateReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	req.Note = clipRunes(strings.TrimSpace(req.Note), 1000)
	v, err := s.viewerOf(ctx, caller)
	if err != nil {
		return err
	}
	var (
		c      commentRow
		status string
	)
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		c, err = s.loadComment(ctx, tx, id, true)
		if err != nil {
			return err
		}
		p, err := s.loadPost(ctx, tx, c.PostID, false)
		if err != nil {
			return err
		}
		mod, err := s.canModerate(ctx, v, p.Community.ID)
		if err != nil {
			return err
		}
		if !mod {
			return httpx.Forbidden("Only moderators of this community can do that.")
		}
		delta := 0
		switch req.Action {
		case "remove":
			if c.Status != StatusVisible {
				return httpx.Conflict("This comment isn't visible.")
			}
			status, delta = StatusRemoved, -1
			_, err = tx.Exec(ctx, `UPDATE forum_comments SET status = 'removed', removed_at = now() WHERE id = $1`, id)
		case "restore":
			if !v.staff {
				return httpx.Forbidden("Only staff can restore a comment.")
			}
			if err := noOpenCase(ctx, tx, TargetComment, id); err != nil {
				return err
			}
			if c.Status != StatusRemoved {
				return httpx.Conflict("This comment isn't removed.")
			}
			status, delta = StatusVisible, 1
			_, err = tx.Exec(ctx, `UPDATE forum_comments SET status = 'visible', removed_at = NULL WHERE id = $1`, id)
		default:
			return httpx.BadRequest("action must be remove or restore.")
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE forum_posts SET comment_count = greatest(comment_count + $2, 0) WHERE id = $1`, c.PostID, delta); err != nil {
			return err
		}
		return audit.Log(ctx, tx, caller.UserID, "forum.comment."+req.Action, "forum_comment", id.String(),
			map[string]any{"post_id": c.PostID, "author_id": c.AuthorID, "note": req.Note})
	})
	if err != nil {
		return err
	}
	s.publish(ctx, c.PostID, EventStatus, map[string]any{"post_id": c.PostID, "target_kind": "comment", "target_id": id, "status": status})
	if c, err = s.loadComment(ctx, s.d.Pool, id, false); err != nil {
		return err
	}
	out, err := s.commentView(ctx, v, c)
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}
