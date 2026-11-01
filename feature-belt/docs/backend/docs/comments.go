package docs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/bg"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/moderation"
	"scaffoldapp/backend/internal/notify"
)

// Comment is a thread root (with Replies) or a reply.
type Comment struct {
	ID          uuid.UUID  `json:"id"`
	PageID      uuid.UUID  `json:"page_id"`
	ThreadID    *uuid.UUID `json:"thread_id"`
	BlockID     *uuid.UUID `json:"block_id"`
	Quote       *string    `json:"quote"`
	AnchorStart *int       `json:"anchor_start"`
	AnchorEnd   *int       `json:"anchor_end"`
	Body        []Span     `json:"body"`
	Author      *RefUser   `json:"author"` // nil once the account is deleted
	ResolvedAt  *time.Time `json:"resolved_at"`
	ResolvedBy  *RefUser   `json:"resolved_by"`
	EditedAt    *time.Time `json:"edited_at"`
	Deleted     bool       `json:"deleted"`
	CreatedAt   time.Time  `json:"created_at"`
	Replies     []Comment  `json:"replies"`
}

const maxCommentChars = 4000

const commentSelect = `
	SELECT c.id, c.page_id, c.thread_id, c.block_id, c.quote, c.anchor_start, c.anchor_end,
	       CASE WHEN c.deleted_at IS NULL THEN c.body ELSE '[]'::jsonb END,
	       c.author_id, au.display_name, au.avatar_url, au.status,
	       c.resolved_at, c.resolved_by, ru.display_name, c.edited_at, c.deleted_at IS NOT NULL, c.created_at
	FROM docs_comments c
	LEFT JOIN users au ON au.id = c.author_id
	LEFT JOIN users ru ON ru.id = c.resolved_by`

func scanComment(row pgx.Row) (Comment, error) {
	var c Comment
	var body []byte
	var authorID, resolvedBy *uuid.UUID
	var authorName, avatar, status, resolverName *string
	err := row.Scan(&c.ID, &c.PageID, &c.ThreadID, &c.BlockID, &c.Quote, &c.AnchorStart, &c.AnchorEnd, &body,
		&authorID, &authorName, &avatar, &status, &c.ResolvedAt, &resolvedBy, &resolverName, &c.EditedAt, &c.Deleted, &c.CreatedAt)
	if err != nil {
		return c, err
	}
	c.Body, _ = decodeSpans(body)
	if authorID != nil && authorName != nil && (status == nil || *status != "deleted") {
		c.Author = &RefUser{ID: *authorID, Name: *authorName, AvatarURL: avatar}
	}
	if resolvedBy != nil && resolverName != nil {
		c.ResolvedBy = &RefUser{ID: *resolvedBy, Name: *resolverName}
	}
	c.Replies = []Comment{}
	return c, nil
}

// listComments: GET /docs/pages/{id}/comments?status=open|resolved|all
// (view): threads with their replies, oldest first. Comments removed by
// moderation and those by people blocked either way are left out.
func (s *Service) listComments(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	if _, err := access.Require(ctx, s.d.Pool, me, Kind, id, access.View); err != nil {
		return err
	}
	status := r.URL.Query().Get("status")
	list, err := s.threads(ctx, me, id, status)
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": list})
	return nil
}

func (s *Service) threads(ctx context.Context, me, pageID uuid.UUID, status string) ([]Comment, error) {
	blocked, err := moderation.BlockedIDs(ctx, s.d.Pool, me)
	if err != nil {
		return nil, err
	}
	cond := ""
	switch status {
	case "resolved":
		cond = " AND c.resolved_at IS NOT NULL"
	case "all":
	default:
		cond = " AND c.resolved_at IS NULL"
	}
	rows, err := s.d.Pool.Query(ctx, commentSelect+`
		WHERE c.page_id = $1 AND c.thread_id IS NULL AND c.hidden_at IS NULL AND (c.author_id IS NULL OR c.author_id <> ALL ($2))`+cond+`
		ORDER BY c.created_at, c.id LIMIT 300`, pageID, blocked)
	if err != nil {
		return nil, err
	}
	roots := []Comment{}
	index := map[uuid.UUID]int{}
	for rows.Next() {
		c, err := scanComment(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		index[c.ID] = len(roots)
		roots = append(roots, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(roots) == 0 {
		return roots, nil
	}
	ids := make([]uuid.UUID, 0, len(roots))
	for _, c := range roots {
		ids = append(ids, c.ID)
	}
	rows, err = s.d.Pool.Query(ctx, commentSelect+`
		WHERE c.thread_id = ANY ($1) AND c.hidden_at IS NULL AND (c.author_id IS NULL OR c.author_id <> ALL ($2))
		ORDER BY c.created_at, c.id`, ids, blocked)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		c, err := scanComment(rows)
		if err != nil {
			return nil, err
		}
		if i, ok := index[*c.ThreadID]; ok {
			roots[i].Replies = append(roots[i].Replies, c)
		}
	}
	// A deleted root with no replies left is noise.
	out := roots[:0]
	for _, c := range roots {
		if !(c.Deleted && len(c.Replies) == 0) {
			out = append(out, c)
		}
	}
	return out, rows.Err()
}

type commentReq struct {
	ThreadID    *uuid.UUID `json:"thread_id"`
	BlockID     *uuid.UUID `json:"block_id"`
	Quote       *string    `json:"quote"`
	AnchorStart *int       `json:"anchor_start"`
	AnchorEnd   *int       `json:"anchor_end"`
	Body        []Span     `json:"body"`
}

// cleanBody sanitises a comment body: people must be org members, text is
// required, short enough and passes the content filter.
func (s *Service) cleanBody(ctx context.Context, orgID uuid.UUID, body []Span) ([]Span, error) {
	raw, _ := json.Marshal(body)
	rules, err := memberRules(ctx, s.d.Pool, orgID, []Op{{Props: map[string]json.RawMessage{"text": raw}}})
	if err != nil {
		return nil, err
	}
	clean, n := sanitizeSpans(body, rules)
	if strings.TrimSpace(plainOf(clean)) == "" {
		return nil, httpx.BadRequest("Write a comment first.")
	}
	if n > maxCommentChars {
		return nil, httpx.BadRequest(fmt.Sprintf("Keep comments under %d characters.", maxCommentChars))
	}
	if err := moderation.Check(ctx, plainOf(clean)); err != nil {
		return nil, err
	}
	return clean, nil
}

// createComment: POST /docs/pages/{id}/comments (comment level). A new
// thread may anchor to a block and a quoted range of it; a reply names its
// thread. Mentioned people, the thread's people and (for a new thread) the
// page's creator are notified.
func (s *Service) createComment(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req commentReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	c, err := s.addComment(ctx, me, id, req)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, c)
	return nil
}

func (s *Service) addComment(ctx context.Context, me, pageID uuid.UUID, req commentReq) (*Comment, error) {
	p, err := requirePage(ctx, s.d.Pool, me, pageID, access.Comment)
	if err != nil {
		return nil, err
	}
	if p.DeletedAt != nil {
		return nil, errInTrash
	}
	body, err := s.cleanBody(ctx, p.OrgID, req.Body)
	if err != nil {
		return nil, err
	}
	var quote *string
	if req.ThreadID != nil {
		req.BlockID, req.AnchorStart, req.AnchorEnd = nil, nil, nil
		var ok bool
		if err := s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM docs_comments WHERE id = $1 AND page_id = $2 AND thread_id IS NULL AND hidden_at IS NULL)`,
			*req.ThreadID, pageID).Scan(&ok); err != nil {
			return nil, err
		}
		if !ok {
			return nil, httpx.NotFound("That thread isn't on this page any more.")
		}
	} else if req.BlockID != nil {
		var plain string
		err := s.d.Pool.QueryRow(ctx, `SELECT plain FROM docs_blocks WHERE id = $1 AND page_id = $2`, *req.BlockID, pageID).Scan(&plain)
		if db.IsNoRows(err) {
			return nil, httpx.NotFound("That block was deleted.")
		}
		if err != nil {
			return nil, err
		}
		if req.AnchorStart != nil && req.AnchorEnd != nil {
			runes := []rune(plain)
			a, b := *req.AnchorStart, *req.AnchorEnd
			if a < 0 || b <= a || b > len(runes) {
				return nil, httpx.BadRequest("The selected text changed. Select it again.")
			}
			q := string(runes[a:b])
			if utf8.RuneCountInString(q) > 500 {
				q = string([]rune(q)[:500])
			}
			quote = &q
		} else {
			req.AnchorStart, req.AnchorEnd = nil, nil
		}
	}
	rawBody, _ := json.Marshal(body)
	var cid uuid.UUID
	if err := s.d.Pool.QueryRow(ctx, `
		INSERT INTO docs_comments (page_id, thread_id, block_id, quote, anchor_start, anchor_end, body, plain, author_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
		pageID, req.ThreadID, req.BlockID, quote, req.AnchorStart, req.AnchorEnd, rawBody, plainOf(body), me).Scan(&cid); err != nil {
		return nil, err
	}
	c, err := scanComment(s.d.Pool.QueryRow(ctx, commentSelect+` WHERE c.id = $1`, cid))
	if err != nil {
		return nil, err
	}
	thread := cid
	if req.ThreadID != nil {
		thread = *req.ThreadID
	}
	s.publish(ctx, pageTopic(pageID), EventComments, map[string]any{"page_id": pageID, "thread_id": thread})
	bg.Go(ctx, "docs.notify_comment", func(ctx context.Context) error {
		s.notifyComment(ctx, me, p, &c, thread, body)
		return nil
	})
	return &c, nil
}

// commentFor loads a comment with the caller's level on its page.
func (s *Service) commentFor(ctx context.Context, me, id uuid.UUID, min access.Level) (*Comment, *Page, uuid.UUID, error) {
	var pageID uuid.UUID
	var author *uuid.UUID
	err := s.d.Pool.QueryRow(ctx, `SELECT page_id, author_id FROM docs_comments WHERE id = $1 AND hidden_at IS NULL`, id).Scan(&pageID, &author)
	if db.IsNoRows(err) {
		return nil, nil, uuid.Nil, httpx.NotFound("Comment not found.")
	}
	if err != nil {
		return nil, nil, uuid.Nil, err
	}
	p, err := requirePage(ctx, s.d.Pool, me, pageID, min)
	if err != nil {
		if he, ok := err.(*httpx.Error); ok && he.Status == http.StatusNotFound {
			return nil, nil, uuid.Nil, httpx.NotFound("Comment not found.")
		}
		return nil, nil, uuid.Nil, err
	}
	c, err := scanComment(s.d.Pool.QueryRow(ctx, commentSelect+` WHERE c.id = $1`, id))
	if err != nil {
		return nil, nil, uuid.Nil, err
	}
	a := uuid.Nil
	if author != nil {
		a = *author
	}
	return &c, p, a, nil
}

type editCommentReq struct {
	Body []Span `json:"body"`
}

// editComment: PATCH /docs/comments/{commentID} (its author, who can still
// comment on the page).
func (s *Service) editComment(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "commentID")
	if err != nil {
		return err
	}
	var req editCommentReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	c, p, author, err := s.commentFor(ctx, me, id, access.Comment)
	if err != nil {
		return err
	}
	if author != me || c.Deleted {
		return httpx.Forbidden("Only the person who wrote a comment can edit it.")
	}
	body, err := s.cleanBody(ctx, p.OrgID, req.Body)
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(body)
	if _, err := s.d.Pool.Exec(ctx, `UPDATE docs_comments SET body = $2, plain = $3, edited_at = now(), updated_at = now() WHERE id = $1`,
		id, raw, plainOf(body)); err != nil {
		return err
	}
	out, err := scanComment(s.d.Pool.QueryRow(ctx, commentSelect+` WHERE c.id = $1`, id))
	if err != nil {
		return err
	}
	s.publish(ctx, pageTopic(p.ID), EventComments, map[string]any{"page_id": p.ID, "thread_id": threadOf(&out)})
	httpx.OK(w, out)
	return nil
}

func threadOf(c *Comment) uuid.UUID {
	if c.ThreadID != nil {
		return *c.ThreadID
	}
	return c.ID
}

// deleteComment: DELETE /docs/comments/{commentID} (its author, or someone
// who manages the page). The text goes; the thread keeps its shape.
func (s *Service) deleteComment(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "commentID")
	if err != nil {
		return err
	}
	c, p, author, err := s.commentFor(ctx, me, id, access.View)
	if err != nil {
		return err
	}
	if author != me && p.MyLevel < access.Manage {
		return httpx.Forbidden("Only the person who wrote a comment, or someone who manages the page, can delete it.")
	}
	if _, err := s.d.Pool.Exec(ctx, `UPDATE docs_comments SET body = '[]', plain = '', deleted_at = COALESCE(deleted_at, now()), updated_at = now() WHERE id = $1`, id); err != nil {
		return err
	}
	s.publish(ctx, pageTopic(p.ID), EventComments, map[string]any{"page_id": p.ID, "thread_id": threadOf(c)})
	httpx.NoContent(w)
	return nil
}

func (s *Service) setResolved(w http.ResponseWriter, r *http.Request, resolved bool) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "commentID")
	if err != nil {
		return err
	}
	c, p, _, err := s.commentFor(ctx, me, id, access.Comment)
	if err != nil {
		return err
	}
	if c.ThreadID != nil {
		return httpx.BadRequest("Resolve the thread, not a reply.")
	}
	if resolved {
		_, err = s.d.Pool.Exec(ctx, `UPDATE docs_comments SET resolved_at = now(), resolved_by = $2, updated_at = now() WHERE id = $1 AND resolved_at IS NULL`, id, me)
	} else {
		_, err = s.d.Pool.Exec(ctx, `UPDATE docs_comments SET resolved_at = NULL, resolved_by = NULL, updated_at = now() WHERE id = $1`, id)
	}
	if err != nil {
		return err
	}
	s.publish(ctx, pageTopic(p.ID), EventComments, map[string]any{"page_id": p.ID, "thread_id": id})
	httpx.NoContent(w)
	return nil
}

// resolveComment / reopenComment: POST / DELETE /docs/comments/{commentID}/resolve (comment level).
func (s *Service) resolveComment(w http.ResponseWriter, r *http.Request) error {
	return s.setResolved(w, r, true)
}
func (s *Service) reopenComment(w http.ResponseWriter, r *http.Request) error {
	return s.setResolved(w, r, false)
}

// ---- notifications ---------------------------------------------------------------

func snippet(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}

func (s *Service) displayName(ctx context.Context, id uuid.UUID) string {
	var name string
	if err := s.d.Pool.QueryRow(ctx, `SELECT display_name FROM users WHERE id = $1`, id).Scan(&name); err != nil || name == "" {
		return "Someone"
	}
	return name
}

// canView is the recipient-side check before any notification: the person
// must still be able to open the page (never agent-capped).
func (s *Service) canView(ctx context.Context, userID, pageID uuid.UUID) bool {
	var lvl int
	err := s.d.Pool.QueryRow(ctx, `SELECT access_level($1, 'docs.page', $2)`, userID, pageID).Scan(&lvl)
	return err == nil && lvl >= 1
}

func (s *Service) send(ctx context.Context, n notify.Notification) {
	if s.d.Notify == nil {
		return
	}
	if err := s.d.Notify.Send(ctx, n); err != nil {
		slogWarn(ctx, "docs: notify", err)
	}
}

// notifyMentions tells people newly mentioned in a page's blocks.
func (s *Service) notifyMentions(ctx context.Context, actor, pageID uuid.UUID, mentions map[uuid.UUID]string) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	title, err := pageTitle(ctx, s.d.Pool, pageID)
	if err != nil {
		return
	}
	name := s.displayName(ctx, actor)
	for u, text := range mentions {
		if u == actor || !s.canView(ctx, u, pageID) {
			continue
		}
		if blocked, _ := moderation.IsBlocked(ctx, s.d.Pool, actor, u); blocked {
			continue
		}
		s.send(ctx, notify.Notification{
			UserID: u, Kind: NotifyMention, Title: name + " mentioned you in " + title, Body: snippet(text, 140),
			Link: "/docs/" + pageID.String(), Data: map[string]any{"page_id": pageID},
		})
	}
}

// notifyComment tells mentioned people, the thread's people and (for a new
// thread) the page's creator, once each.
func (s *Service) notifyComment(ctx context.Context, actor uuid.UUID, p *Page, c *Comment, thread uuid.UUID, body []Span) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	name := s.displayName(ctx, actor)
	title := displayTitle(p.Title)
	link := "/docs/" + p.ID.String() + "?comment=" + thread.String()
	done := map[uuid.UUID]bool{actor: true}
	deliver := func(u uuid.UUID, kind, head string) {
		if done[u] || !s.canView(ctx, u, p.ID) {
			return
		}
		if blocked, _ := moderation.IsBlocked(ctx, s.d.Pool, actor, u); blocked {
			return
		}
		done[u] = true
		s.send(ctx, notify.Notification{UserID: u, Kind: kind, Title: head, Body: snippet(plainOf(body), 140), Link: link,
			Data: map[string]any{"page_id": p.ID, "thread_id": thread}})
	}
	users, _ := mentionIDs(body)
	for _, u := range dedupeIDs(users) {
		deliver(u, NotifyMention, name+" mentioned you in a comment on "+title)
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT DISTINCT author_id FROM docs_comments WHERE (id = $1 OR thread_id = $1) AND author_id IS NOT NULL AND deleted_at IS NULL`, thread)
	if err == nil {
		people, _ := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		for _, u := range people {
			deliver(u, NotifyComment, name+" replied on "+title)
		}
	}
	if c.ThreadID == nil && p.CreatedBy != nil {
		deliver(*p.CreatedBy, NotifyComment, name+" commented on "+title)
	}
}
