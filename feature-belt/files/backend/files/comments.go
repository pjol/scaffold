package files

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/bg"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/moderation"
	"scaffoldapp/backend/internal/notify"
	"scaffoldapp/backend/internal/realtime"
)

// Comments on files and folders: threads with replies, @mentions that notify
// (people who can open the item), resolve and reopen, and an optional anchor
// to a PDF page or a moment in a video or recording. Commenting needs the
// comment level (edit and manage include it); reading needs view.

const (
	KindCommentMention = "files.comment_mention"
	KindCommentReply   = "files.comment_reply"
	EventComments      = "files.comments"

	maxCommentChars   = 10000
	maxMentions       = 20
	maxThreadsPerPage = 100
)

// Comment is one comment; threads carry their replies.
type Comment struct {
	ID         uuid.UUID       `json:"id"`
	ItemID     uuid.UUID       `json:"item_id"`
	ThreadID   *uuid.UUID      `json:"thread_id"`
	Author     *Person         `json:"author"`
	Body       string          `json:"body"`
	Mentions   []uuid.UUID     `json:"mentions"`
	Anchor     *CommentAnchor  `json:"anchor"`
	ResolvedAt *time.Time      `json:"resolved_at"`
	ResolvedBy *Person         `json:"resolved_by"`
	EditedAt   *time.Time      `json:"edited_at"`
	CreatedAt  time.Time       `json:"created_at"`
	Replies    []Comment       `json:"replies,omitempty"`
	Mine       bool            `json:"mine"`
}

// CommentAnchor pins a thread to a PDF page or a moment (seconds) in a video
// or recording.
type CommentAnchor struct {
	Page    *int     `json:"page,omitempty"`
	Seconds *float64 `json:"seconds,omitempty"`
}

func (a *CommentAnchor) valid() bool {
	if a == nil {
		return true
	}
	if (a.Page == nil) == (a.Seconds == nil) {
		return false
	}
	if a.Page != nil {
		return *a.Page >= 1 && *a.Page <= 100000
	}
	return *a.Seconds >= 0 && *a.Seconds <= 360000
}

type commentCursor struct {
	T  time.Time `json:"t"`
	ID uuid.UUID `json:"id"`
}

const commentColumns = `c.id, c.item_id, c.thread_id, c.author_id, au.display_name, au.avatar_url, c.body, c.mentions, c.anchor,
	c.resolved_at, c.resolved_by, ru.display_name, ru.avatar_url, c.edited_at, c.created_at`

const commentJoins = ` LEFT JOIN users au ON au.id = c.author_id LEFT JOIN users ru ON ru.id = c.resolved_by`

func scanComment(row pgx.Row, me uuid.UUID) (Comment, error) {
	var c Comment
	var authorID, resolvedByID *uuid.UUID
	var aName, aAvatar, rName, rAvatar *string
	var anchor []byte
	if err := row.Scan(&c.ID, &c.ItemID, &c.ThreadID, &authorID, &aName, &aAvatar, &c.Body, &c.Mentions, &anchor,
		&c.ResolvedAt, &resolvedByID, &rName, &rAvatar, &c.EditedAt, &c.CreatedAt); err != nil {
		return c, err
	}
	c.Author = person(authorID, aName, aAvatar)
	c.ResolvedBy = person(resolvedByID, rName, rAvatar)
	c.Mine = authorID != nil && *authorID == me
	if len(anchor) > 0 {
		var a CommentAnchor
		if json.Unmarshal(anchor, &a) == nil {
			c.Anchor = &a
		}
	}
	if c.Mentions == nil {
		c.Mentions = []uuid.UUID{}
	}
	return c, nil
}

// listComments returns an item's threads (oldest first, keyset) with their
// replies. resolved: nil all, true only resolved, false only open.
func (s *Service) listComments(ctx context.Context, q db.Querier, me, itemID uuid.UUID, resolved *bool, cursor string, limit int) (httpx.Page[Comment], error) {
	if limit <= 0 || limit > maxThreadsPerPage {
		limit = maxThreadsPerPage
	}
	args := []any{itemID, resolved}
	sql := `SELECT ` + commentColumns + ` FROM files_comments c` + commentJoins + `
		WHERE c.item_id = $1 AND c.thread_id IS NULL AND ($2::boolean IS NULL OR (c.resolved_at IS NOT NULL) = $2)`
	if cursor != "" {
		var cc commentCursor
		if err := httpx.DecodeCursor(cursor, &cc); err != nil {
			return httpx.Page[Comment]{}, err
		}
		args = append(args, cc.T, cc.ID)
		sql += ` AND (c.created_at, c.id) > ($3, $4)`
	}
	args = append(args, limit+1)
	sql += ` ORDER BY c.created_at, c.id LIMIT $` + strconv.Itoa(len(args))
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return httpx.Page[Comment]{}, err
	}
	var threads []Comment
	for rows.Next() {
		c, err := scanComment(rows, me)
		if err != nil {
			rows.Close()
			return httpx.Page[Comment]{}, err
		}
		threads = append(threads, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return httpx.Page[Comment]{}, err
	}
	page := httpx.NewPage(threads, limit, func(c Comment) any { return commentCursor{T: c.CreatedAt, ID: c.ID} })
	if len(page.Items) == 0 {
		page.Items = []Comment{}
		return page, nil
	}
	ids := make([]uuid.UUID, 0, len(page.Items))
	byID := map[uuid.UUID]int{}
	for i, t := range page.Items {
		ids = append(ids, t.ID)
		byID[t.ID] = i
	}
	rrows, err := q.Query(ctx, `SELECT `+commentColumns+` FROM files_comments c`+commentJoins+`
		WHERE c.thread_id = ANY($1) ORDER BY c.created_at, c.id`, ids)
	if err != nil {
		return httpx.Page[Comment]{}, err
	}
	defer rrows.Close()
	for rrows.Next() {
		c, err := scanComment(rrows, me)
		if err != nil {
			return httpx.Page[Comment]{}, err
		}
		if i, ok := byID[*c.ThreadID]; ok {
			page.Items[i].Replies = append(page.Items[i].Replies, c)
		}
	}
	return page, rrows.Err()
}

// commentsRoute: GET /v1/files/items/{id}/comments?status=open|resolved&cursor= (view).
func (s *Service) commentsRoute(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := idParam(r)
	if err != nil {
		return err
	}
	if _, err := s.load(ctx, s.d.Pool, me, id, access.View); err != nil {
		return err
	}
	var resolved *bool
	switch r.URL.Query().Get("status") {
	case "open":
		f := false
		resolved = &f
	case "resolved":
		t := true
		resolved = &t
	}
	p := httpx.ParsePage(r)
	page, err := s.listComments(ctx, s.d.Pool, me, id, resolved, p.Cursor, p.Limit)
	if err != nil {
		return err
	}
	httpx.OK(w, page)
	return nil
}

// NewComment is what addComment takes (REST and the agent tool).
type NewComment struct {
	Body     string         `json:"body"`
	ThreadID *uuid.UUID     `json:"thread_id"`
	Mentions []uuid.UUID    `json:"mentions"`
	Anchor   *CommentAnchor `json:"anchor"`
}

// cleanComment validates a new comment and returns its tidy body.
func cleanComment(ctx context.Context, in *NewComment) error {
	in.Body = strings.TrimSpace(in.Body)
	switch n := utf8.RuneCountInString(in.Body); {
	case n == 0:
		return httpx.BadRequest("Write something first.")
	case n > maxCommentChars:
		return httpx.BadRequest("Keep comments under 10,000 characters.")
	}
	if len(in.Mentions) > maxMentions {
		return httpx.BadRequest("Mention at most 20 people in one comment.")
	}
	if in.ThreadID != nil && in.Anchor != nil {
		return httpx.BadRequest("Replies don't take an anchor; the thread has one.")
	}
	if !in.Anchor.valid() {
		return httpx.BadRequest("An anchor is a page (1 or more) or seconds (0 or more), not both.")
	}
	return moderation.Check(ctx, in.Body)
}

// addComment posts a comment or reply as me (comment level on the item) and
// notifies mentioned people who can open it, and the thread's participants.
// It returns the comment and the mentioned people who weren't notified
// because they can't open the item.
func (s *Service) addComment(ctx context.Context, me, itemID uuid.UUID, in NewComment) (*Comment, []uuid.UUID, error) {
	if err := cleanComment(ctx, &in); err != nil {
		return nil, nil, err
	}
	var out Comment
	var it *Item
	var notifyIDs, skipped []uuid.UUID
	var threadAuthor *uuid.UUID
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var err error
		if it, err = s.loadLive(ctx, tx, me, itemID, access.Comment); err != nil {
			return err
		}
		if in.ThreadID != nil {
			var resolved *time.Time
			err := tx.QueryRow(ctx, `SELECT author_id, resolved_at FROM files_comments WHERE id = $1 AND item_id = $2 AND thread_id IS NULL`,
				*in.ThreadID, itemID).Scan(&threadAuthor, &resolved)
			if db.IsNoRows(err) {
				return httpx.NotFound("That thread is gone.")
			}
			if err != nil {
				return err
			}
			if resolved != nil { // replying reopens it
				if _, err := tx.Exec(ctx, `UPDATE files_comments SET resolved_at = NULL, resolved_by = NULL WHERE id = $1`, *in.ThreadID); err != nil {
					return err
				}
			}
		}
		mentions := []uuid.UUID{}
		seen := map[uuid.UUID]bool{me: true}
		for _, u := range in.Mentions {
			if seen[u] {
				continue
			}
			seen[u] = true
			lvl, err := access.LevelOf(ctx, tx, u, Kind, itemID)
			if err != nil {
				return err
			}
			if lvl >= access.View {
				mentions = append(mentions, u)
			} else {
				skipped = append(skipped, u)
			}
		}
		var anchor any
		if in.Anchor != nil {
			b, _ := json.Marshal(in.Anchor)
			anchor = b
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO files_comments (org_id, item_id, thread_id, author_id, body, mentions, anchor)
			VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`, it.OrgID, itemID, in.ThreadID, me, in.Body, mentions, anchor).Scan(&id); err != nil {
			return err
		}
		s.logActivity(ctx, tx, it.OrgID, itemID, it.ParentID, me, "commented", map[string]any{"comment_id": id})
		c, err := scanComment(tx.QueryRow(ctx, `SELECT `+commentColumns+` FROM files_comments c`+commentJoins+` WHERE c.id = $1`, id), me)
		if err != nil {
			return err
		}
		out = c
		notifyIDs = mentions
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	s.afterComment(ctx, me, it, &out, notifyIDs, threadAuthor)
	if skipped == nil {
		skipped = []uuid.UUID{}
	}
	return &out, skipped, nil
}

// afterComment publishes the change and sends notifications (best effort).
func (s *Service) afterComment(ctx context.Context, me uuid.UUID, it *Item, c *Comment, mentions []uuid.UUID, threadAuthor *uuid.UUID) {
	s.publishComments(ctx, it.ID)
	if s.d.Notify == nil {
		return
	}
	author := "Someone"
	if c.Author != nil && c.Author.Name != "" {
		author = c.Author.Name
	}
	excerpt := c.Body
	if utf8.RuneCountInString(excerpt) > 140 {
		excerpt = string([]rune(excerpt)[:139]) + "…"
	}
	link := itemLink(it.ID) + "?comment=" + c.ID.String()
	data := map[string]any{"kind": Kind, "id": it.ID.String(), "comment_id": c.ID.String()}
	sent := map[uuid.UUID]bool{me: true}
	var batch []notify.Notification
	for _, u := range mentions {
		sent[u] = true
		batch = append(batch, notify.Notification{UserID: u, Kind: KindCommentMention, Title: author + " mentioned you on " + it.Name, Body: excerpt, Link: link, Data: data})
	}
	if c.ThreadID != nil {
		// Everyone in the thread who can still open the item hears about a reply.
		rows, err := s.d.Pool.Query(ctx, `SELECT DISTINCT author_id FROM files_comments WHERE (id = $1 OR thread_id = $1) AND author_id IS NOT NULL`, *c.ThreadID)
		if err == nil {
			var ids []uuid.UUID
			for rows.Next() {
				var u uuid.UUID
				if rows.Scan(&u) == nil {
					ids = append(ids, u)
				}
			}
			rows.Close()
			for _, u := range ids {
				if sent[u] {
					continue
				}
				if lvl, err := access.LevelOf(ctx, s.d.Pool, u, Kind, it.ID); err == nil && lvl >= access.View {
					sent[u] = true
					batch = append(batch, notify.Notification{UserID: u, Kind: KindCommentReply, Title: author + " replied on " + it.Name, Body: excerpt, Link: link, Data: data})
				}
			}
		}
	} else if it.OwnerID != nil && !sent[*it.OwnerID] {
		// A new thread on something in your My drive.
		if lvl, err := access.LevelOf(ctx, s.d.Pool, *it.OwnerID, Kind, it.ID); err == nil && lvl >= access.View {
			batch = append(batch, notify.Notification{UserID: *it.OwnerID, Kind: KindCommentReply, Title: author + " commented on " + it.Name, Body: excerpt, Link: link, Data: data})
		}
	}
	if len(batch) == 0 {
		return
	}
	bg.Go(ctx, "files.comment_notify", func(ctx context.Context) error {
		for _, n := range batch {
			if err := s.d.Notify.Send(ctx, n); err != nil {
				slog.WarnContext(ctx, "files: comment notification", "err", err)
			}
		}
		return nil
	})
}

func (s *Service) publishComments(ctx context.Context, itemID uuid.UUID) {
	if s.d.Broker == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_ = s.d.Broker.Publish(ctx, realtime.Topic(topicKind, itemID), EventComments, map[string]any{"item_id": itemID})
}

// addCommentRoute: POST /v1/files/items/{id}/comments {body, thread_id?, mentions?, anchor?} (comment).
func (s *Service) addCommentRoute(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := idParam(r)
	if err != nil {
		return err
	}
	var req NewComment
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	c, skipped, err := s.addComment(ctx, me, id, req)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"comment": c, "not_notified": skipped})
	return nil
}

type commentPatch struct {
	Body     *string `json:"body"`
	Resolved *bool   `json:"resolved"`
}

// commentTarget loads a comment and the caller's level on its item.
func (s *Service) commentTarget(ctx context.Context, q db.Querier, me, commentID uuid.UUID) (*Comment, access.Level, error) {
	c, err := scanComment(q.QueryRow(ctx, `SELECT `+commentColumns+` FROM files_comments c`+commentJoins+` WHERE c.id = $1`, commentID), me)
	if db.IsNoRows(err) {
		return nil, 0, httpx.NotFound("Comment not found.")
	}
	if err != nil {
		return nil, 0, err
	}
	lvl, err := access.Require(ctx, q, me, Kind, c.ItemID, access.View)
	if err != nil {
		return nil, 0, httpx.NotFound("Comment not found.")
	}
	return &c, lvl, nil
}

// patchComment: PATCH /v1/files/comments/{commentID} {body?} (its author) or
// {resolved} (a thread: its author or anyone who can edit the item).
func (s *Service) patchComment(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	cid, err := uuid.Parse(chi.URLParam(r, "commentID"))
	if err != nil {
		return httpx.NotFound("Comment not found.")
	}
	var req commentPatch
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	var out Comment
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		c, lvl, err := s.commentTarget(ctx, tx, me, cid)
		if err != nil {
			return err
		}
		if lvl < access.Comment {
			return httpx.NewError(http.StatusForbidden, "access_level_required", "You can only read comments here.")
		}
		if req.Body != nil {
			if !c.Mine {
				return httpx.NewError(http.StatusForbidden, "not_author", "Only the person who wrote a comment can change it.")
			}
			in := NewComment{Body: *req.Body}
			if err := cleanComment(ctx, &in); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE files_comments SET body = $2, edited_at = now() WHERE id = $1`, cid, in.Body); err != nil {
				return err
			}
		}
		if req.Resolved != nil {
			if c.ThreadID != nil {
				return httpx.BadRequest("Resolve the thread, not a reply.")
			}
			if !c.Mine && lvl < access.Edit {
				return httpx.NewError(http.StatusForbidden, "access_level_required", "Only the thread's author or people who can edit this item can resolve it.")
			}
			if *req.Resolved {
				_, err = tx.Exec(ctx, `UPDATE files_comments SET resolved_at = COALESCE(resolved_at, now()), resolved_by = COALESCE(resolved_by, $2) WHERE id = $1`, cid, me)
			} else {
				_, err = tx.Exec(ctx, `UPDATE files_comments SET resolved_at = NULL, resolved_by = NULL WHERE id = $1`, cid)
			}
			if err != nil {
				return err
			}
		}
		out, err = scanComment(tx.QueryRow(ctx, `SELECT `+commentColumns+` FROM files_comments c`+commentJoins+` WHERE c.id = $1`, cid), me)
		return err
	})
	if err != nil {
		return err
	}
	s.publishComments(ctx, out.ItemID)
	httpx.OK(w, out)
	return nil
}

// deleteComment: DELETE /v1/files/comments/{commentID} (its author, or a
// manager of the item). Deleting a thread removes its replies.
func (s *Service) deleteComment(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	cid, err := uuid.Parse(chi.URLParam(r, "commentID"))
	if err != nil {
		return httpx.NotFound("Comment not found.")
	}
	var itemID uuid.UUID
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		c, lvl, err := s.commentTarget(ctx, tx, me, cid)
		if err != nil {
			return err
		}
		if !(c.Mine && lvl >= access.Comment) && lvl < access.Manage {
			return httpx.NewError(http.StatusForbidden, "not_author", "Only the person who wrote a comment, or someone who manages this item, can delete it.")
		}
		itemID = c.ItemID
		_, err = tx.Exec(ctx, `DELETE FROM files_comments WHERE id = $1`, cid)
		return err
	})
	if err != nil {
		return err
	}
	s.publishComments(ctx, itemID)
	httpx.NoContent(w)
	return nil
}


// KindComment is the moderation target for a file comment.
const KindComment = "files.comment"

type commentTarget struct{ s *Service }

// Snapshot: only for people who can open the comment's item.
func (t commentTarget) Snapshot(ctx context.Context, rawID string) (moderation.Snapshot, error) {
	id, err := uuid.Parse(rawID)
	if err != nil {
		return moderation.Snapshot{}, httpx.ErrNotFound
	}
	me := auth.FromContext(ctx)
	if me == nil {
		return moderation.Snapshot{}, httpx.ErrNotFound
	}
	c, _, err := t.s.commentTarget(ctx, t.s.d.Pool, me.UserID, id)
	if err != nil {
		return moderation.Snapshot{}, httpx.ErrNotFound
	}
	snap := moderation.Snapshot{Text: c.Body, Link: itemLink(c.ItemID) + "?comment=" + c.ID.String()}
	if c.Author != nil {
		snap.AuthorID = &c.Author.ID
	}
	return snap, nil
}

// Remove deletes the comment (and, for a thread, its replies).
func (t commentTarget) Remove(ctx context.Context, tx pgx.Tx, rawID string) error {
	id, err := uuid.Parse(rawID)
	if err != nil {
		return httpx.ErrNotFound
	}
	var item uuid.UUID
	err = tx.QueryRow(ctx, `DELETE FROM files_comments WHERE id = $1 RETURNING item_id`, id).Scan(&item)
	if db.IsNoRows(err) {
		return httpx.ErrNotFound
	}
	if err == nil {
		t.s.publishComments(ctx, item)
	}
	return err
}

const (
	schemaListComments = `{"type":"object","properties":{
		"item_id":{"type":"string","format":"uuid"},
		"status":{"type":"string","enum":["all","open","resolved"],"description":"Default open."},
		"cursor":{"type":"string"}},"required":["item_id"],"additionalProperties":false}`
	schemaComment = `{"type":"object","properties":{
		"item_id":{"type":"string","format":"uuid","description":"The file or folder."},
		"body":{"type":"string","description":"The comment (plain text, up to 10,000 characters)."},
		"thread_id":{"type":"string","format":"uuid","description":"Reply in this thread instead of starting one."},
		"mentions":{"type":"array","items":{"type":"string","format":"uuid"},"maxItems":20,"description":"User ids to notify; only people who can open the item are notified."},
		"page":{"type":"integer","minimum":1,"description":"Pin a new thread to this PDF page."},
		"seconds":{"type":"number","minimum":0,"description":"Pin a new thread to this moment of a video or recording."}},
		"required":["item_id","body"],"additionalProperties":false}`
)

type commentIn struct {
	ItemID   uuid.UUID   `json:"item_id"`
	Body     string      `json:"body"`
	ThreadID *uuid.UUID  `json:"thread_id"`
	Mentions []uuid.UUID `json:"mentions"`
	Page     *int        `json:"page"`
	Seconds  *float64    `json:"seconds"`
}

func (in commentIn) toNew() NewComment {
	nc := NewComment{Body: in.Body, ThreadID: in.ThreadID, Mentions: in.Mentions}
	if in.Page != nil || in.Seconds != nil {
		nc.Anchor = &CommentAnchor{Page: in.Page, Seconds: in.Seconds}
	}
	return nc
}

// commentTools: read an item's comments, and comment as the person (a
// single reviewable write with an exact preview).
func (s *Service) commentTools() []app.Tool {
	return []app.Tool{
		{
			Name:        "files_list_comments",
			Description: "List the comment threads on a file or folder (with replies, who wrote them, anchors to a PDF page or video moment, and whether they're resolved).",
			InputSchema: json.RawMessage(schemaListComments),
			ReadOnly:    true,
			Target:      targetOf("item_id"),
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					ItemID uuid.UUID `json:"item_id"`
					Status string    `json:"status"`
					Cursor string    `json:"cursor"`
				}
				if err := decode(input, &in); err != nil {
					return nil, err
				}
				if _, err := s.load(ctx, s.d.Pool, caller.UserID, in.ItemID, access.View); err != nil {
					return nil, err
				}
				var resolved *bool
				switch in.Status {
				case "", "open":
					f := false
					resolved = &f
				case "resolved":
					t := true
					resolved = &t
				}
				page, err := s.listComments(ctx, s.d.Pool, caller.UserID, in.ItemID, resolved, in.Cursor, 50)
				if err != nil {
					return nil, err
				}
				return map[string]any{"threads": page.Items, "next_cursor": page.NextCursor}, nil
			},
		},
		{
			Name:        "files_comment",
			Description: "Comment on a file or folder as the user, or reply in a thread (replying reopens a resolved thread). Mentioned people who can open the item are notified.",
			InputSchema: json.RawMessage(schemaComment),
			ReadOnly:    false,
			Target:      targetOf("item_id"),
			Preview: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
				var in commentIn
				if err := decode(input, &in); err != nil {
					return app.ToolPreview{}, err
				}
				nc := in.toNew()
				if err := cleanComment(ctx, &nc); err != nil {
					return app.ToolPreview{}, err
				}
				it, err := s.loadLive(ctx, s.d.Pool, caller.UserID, in.ItemID, access.View)
				if err != nil {
					return app.ToolPreview{}, err
				}
				where := "Comment on “" + it.Name + "”"
				if in.ThreadID != nil {
					where = "Reply in a thread on “" + it.Name + "”"
				} else if nc.Anchor != nil && nc.Anchor.Page != nil {
					where += " (page " + strconv.Itoa(*nc.Anchor.Page) + ")"
				} else if nc.Anchor != nil && nc.Anchor.Seconds != nil {
					where += " (at " + strconv.FormatFloat(*nc.Anchor.Seconds, 'f', -1, 64) + " s)"
				}
				return app.ToolPreview{Summary: where, After: nc.Body, Link: itemLink(it.ID)}, nil
			},
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in commentIn
				if err := decode(input, &in); err != nil {
					return nil, err
				}
				c, skipped, err := s.addComment(ctx, caller.UserID, in.ItemID, in.toNew())
				if err != nil {
					return nil, err
				}
				return map[string]any{"comment": c, "not_notified": skipped}, nil
			},
		},
	}
}
