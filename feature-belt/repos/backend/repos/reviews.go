package repos

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/notify"
)

// Reviews and comments. Comment level comments (conversation and lines)
// and leaves "comment" reviews; approving or requesting changes takes edit
// level, and only those decisions count toward branch protection. Line
// comments sit on one side of the diff ("new" for added and context lines,
// "old" for removed ones) at the head commit they were written against.

// Comment is a conversation or line comment.
type Comment struct {
	ID         uuid.UUID  `json:"id"`
	Author     *UserRef   `json:"author"`
	Body       string     `json:"body"`
	Path       *string    `json:"path"`
	Line       *int       `json:"line"`
	Side       *string    `json:"side"`
	CommitSHA  *string    `json:"commit_sha"`
	ReplyTo    *uuid.UUID `json:"reply_to"`
	ReviewID   *uuid.UUID `json:"review_id"`
	ResolvedAt *time.Time `json:"resolved_at"`
	ResolvedBy *UserRef   `json:"resolved_by"`
	EditedAt   *time.Time `json:"edited_at"`
	Deleted    bool       `json:"deleted"`
	CreatedAt  time.Time  `json:"created_at"`
	CanEdit    bool       `json:"can_edit"`
}

const commentSelect = `
	SELECT c.id, c.author_id, a.display_name, a.avatar_url, a.status, c.body, c.path, c.line, c.side, c.commit_sha,
	       c.reply_to, c.review_id, c.resolved_at, c.resolved_by, rb.display_name, rb.avatar_url, rb.status,
	       c.edited_at, c.deleted_at IS NOT NULL, c.created_at
	FROM repos_comments c
	LEFT JOIN users a ON a.id = c.author_id
	LEFT JOIN users rb ON rb.id = c.resolved_by`

func scanComment(row pgx.Row, me uuid.UUID) (*Comment, error) {
	var c Comment
	var aID, rID *uuid.UUID
	var aName, aAv, aSt, rName, rAv, rSt *string
	if err := row.Scan(&c.ID, &aID, &aName, &aAv, &aSt, &c.Body, &c.Path, &c.Line, &c.Side, &c.CommitSHA,
		&c.ReplyTo, &c.ReviewID, &c.ResolvedAt, &rID, &rName, &rAv, &rSt, &c.EditedAt, &c.Deleted, &c.CreatedAt); err != nil {
		return nil, err
	}
	c.Author, c.ResolvedBy = userRef(aID, aName, aAv, aSt), userRef(rID, rName, rAv, rSt)
	if c.Deleted {
		c.Body = ""
	}
	c.CanEdit = !c.Deleted && aID != nil && *aID == me
	return &c, nil
}

func (s *Service) comments(ctx context.Context, q db.Querier, pullID, me uuid.UUID, where string) ([]Comment, error) {
	rows, err := q.Query(ctx, commentSelect+` WHERE c.pull_id = $1 `+where+` ORDER BY c.created_at, c.id`, pullID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Comment{}
	for rows.Next() {
		c, err := scanComment(rows, me)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// ---- timeline ------------------------------------------------------------------

// TimelineItem is one entry of a pull request's conversation.
type TimelineItem struct {
	Type    string          `json:"type"` // comment | review | thread | event
	At      time.Time       `json:"at"`
	Comment *Comment        `json:"comment,omitempty"`
	Review  *Review         `json:"review,omitempty"`
	Event   *Event          `json:"event,omitempty"`
	Thread  *ThreadSummary  `json:"thread,omitempty"`
	Threads []ThreadSummary `json:"threads,omitempty"` // a review's line comments
}

// Review is a submitted review.
type Review struct {
	ID          uuid.UUID  `json:"id"`
	Author      *UserRef   `json:"author"`
	State       string     `json:"state"`
	Body        string     `json:"body"`
	CommitSHA   string     `json:"commit_sha"`
	DismissedAt *time.Time `json:"dismissed_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

// Event is a state change.
type Event struct {
	ID        uuid.UUID      `json:"id"`
	Kind      string         `json:"kind"`
	Actor     *UserRef       `json:"actor"`
	Data      map[string]any `json:"data"`
	CreatedAt time.Time      `json:"created_at"`
}

// ThreadSummary points the timeline at a line thread.
type ThreadSummary struct {
	ID      uuid.UUID `json:"id"`
	Path    string    `json:"path"`
	Line    int       `json:"line"`
	Side    string    `json:"side"`
	Body    string    `json:"body"`
	Replies int       `json:"replies"`
	Author  *UserRef  `json:"author"`
}

func (s *Service) pullTimeline(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	_, p, _, err := s.pullFromRoute(r, levelView)
	if err != nil {
		return err
	}
	ctx := r.Context()
	items := []TimelineItem{}
	conv, err := s.comments(ctx, s.d.Pool, p.ID, me.UserID, `AND c.path IS NULL`)
	if err != nil {
		return err
	}
	for i := range conv {
		items = append(items, TimelineItem{Type: "comment", At: conv[i].CreatedAt, Comment: &conv[i]})
	}
	roots, err := s.comments(ctx, s.d.Pool, p.ID, me.UserID, `AND c.path IS NOT NULL AND c.reply_to IS NULL`)
	if err != nil {
		return err
	}
	replies := map[uuid.UUID]int{}
	rows, err := s.d.Pool.Query(ctx, `SELECT reply_to, count(*) FROM repos_comments WHERE pull_id = $1 AND reply_to IS NOT NULL AND deleted_at IS NULL GROUP BY reply_to`, p.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id uuid.UUID
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			rows.Close()
			return err
		}
		replies[id] = n
	}
	rows.Close()
	byReview := map[uuid.UUID][]ThreadSummary{}
	for _, c := range roots {
		ts := ThreadSummary{ID: c.ID, Path: *c.Path, Line: *c.Line, Side: *c.Side, Body: c.Body, Replies: replies[c.ID], Author: c.Author}
		if c.ReviewID != nil {
			byReview[*c.ReviewID] = append(byReview[*c.ReviewID], ts)
			continue
		}
		items = append(items, TimelineItem{Type: "thread", At: c.CreatedAt, Thread: &ts})
	}
	rows, err = s.d.Pool.Query(ctx, `
		SELECT r.id, r.author_id, a.display_name, a.avatar_url, a.status, r.state, r.body, r.commit_sha, r.dismissed_at, r.created_at
		FROM repos_reviews r LEFT JOIN users a ON a.id = r.author_id WHERE r.pull_id = $1 ORDER BY r.created_at, r.id`, p.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var rv Review
		var aID *uuid.UUID
		var aName, aAv, aSt *string
		if err := rows.Scan(&rv.ID, &aID, &aName, &aAv, &aSt, &rv.State, &rv.Body, &rv.CommitSHA, &rv.DismissedAt, &rv.CreatedAt); err != nil {
			rows.Close()
			return err
		}
		rv.Author = userRef(aID, aName, aAv, aSt)
		items = append(items, TimelineItem{Type: "review", At: rv.CreatedAt, Review: &rv, Threads: byReview[rv.ID]})
	}
	rows.Close()
	rows, err = s.d.Pool.Query(ctx, `
		SELECT e.id, e.kind, e.actor_id, a.display_name, a.avatar_url, a.status, e.data, e.created_at
		FROM repos_pull_events e LEFT JOIN users a ON a.id = e.actor_id WHERE e.pull_id = $1 ORDER BY e.created_at, e.id`, p.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var ev Event
		var aID *uuid.UUID
		var aName, aAv, aSt *string
		if err := rows.Scan(&ev.ID, &ev.Kind, &aID, &aName, &aAv, &aSt, &ev.Data, &ev.CreatedAt); err != nil {
			rows.Close()
			return err
		}
		ev.Actor = userRef(aID, aName, aAv, aSt)
		items = append(items, TimelineItem{Type: "event", At: ev.CreatedAt, Event: &ev})
	}
	rows.Close()
	sort.SliceStable(items, func(i, j int) bool { return items[i].At.Before(items[j].At) })
	httpx.OK(w, map[string]any{"items": items})
	return nil
}

// Thread is a line comment with its replies.
type Thread struct {
	Root     Comment   `json:"root"`
	Replies  []Comment `json:"replies"`
	Outdated bool      `json:"outdated"` // the file changed since the comment
}

// pullThreads is GET /pulls/{n}/threads: every line thread, for the diff.
func (s *Service) pullThreads(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	repo, p, _, err := s.pullFromRoute(r, levelView)
	if err != nil {
		return err
	}
	ctx := r.Context()
	all, err := s.comments(ctx, s.d.Pool, p.ID, me.UserID, `AND c.path IS NOT NULL`)
	if err != nil {
		return err
	}
	threads := []*Thread{}
	byID := map[uuid.UUID]*Thread{}
	for _, c := range all {
		if c.ReplyTo == nil {
			t := &Thread{Root: c, Replies: []Comment{}}
			threads = append(threads, t)
			byID[c.ID] = t
		} else if t := byID[*c.ReplyTo]; t != nil {
			t.Replies = append(t.Replies, c)
		}
	}
	// A thread is outdated when its file differs between the commit it was
	// written against and the current head (its line numbers may not hold).
	blobAt := map[string]string{}
	blob := func(sha, p string) string {
		k := sha + "\x00" + p
		if v, ok := blobAt[k]; ok {
			return v
		}
		e, err := s.git.Entry(ctx, repo.ID, sha, p)
		v := ""
		if err == nil {
			v = e.OID
		}
		blobAt[k] = v
		return v
	}
	for _, t := range threads {
		if t.Root.CommitSHA != nil && *t.Root.CommitSHA != p.HeadSHA && t.Root.Path != nil {
			t.Outdated = blob(*t.Root.CommitSHA, *t.Root.Path) != blob(p.HeadSHA, *t.Root.Path)
		}
	}
	httpx.OK(w, map[string]any{"items": threads, "head_sha": p.HeadSHA})
	return nil
}

// ---- comments ------------------------------------------------------------------

// CommentInput is a new comment.
type CommentInput struct {
	Body    string     `json:"body"`
	Path    string     `json:"path"`
	Line    int        `json:"line"`
	Side    string     `json:"side"` // new (default) | old
	ReplyTo *uuid.UUID `json:"reply_to"`
}

func cleanBody(b string) (string, error) {
	b = strings.TrimSpace(strings.ReplaceAll(b, "\r\n", "\n"))
	if n := utf8.RuneCountInString(b); n == 0 || n > 65536 {
		return "", httpx.BadRequest("Write a comment (up to 65,536 characters).")
	}
	return b, nil
}

func (s *Service) addComment(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	repo, p, _, err := s.pullFromRoute(r, levelComment)
	if err != nil {
		return err
	}
	var in CommentInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	c, err := s.createComment(r.Context(), me, repo, p, in, nil)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, c)
	return nil
}

// createComment writes one comment (in tx when reviewID is set).
func (s *Service) createComment(ctx context.Context, me *auth.Identity, repo *Repo, p *Pull, in CommentInput, review *reviewCtx) (*Comment, error) {
	body, err := cleanBody(in.Body)
	if err != nil {
		return nil, err
	}
	var path *string
	var line *int
	var side *string
	commit := p.HeadSHA
	if in.ReplyTo != nil {
		var rp *string
		var rl *int
		var rs, rc *string
		var root *uuid.UUID
		err := s.d.Pool.QueryRow(ctx, `SELECT path, line, side, commit_sha, reply_to FROM repos_comments WHERE id = $1 AND pull_id = $2 AND deleted_at IS NULL`,
			*in.ReplyTo, p.ID).Scan(&rp, &rl, &rs, &rc, &root)
		if db.IsNoRows(err) {
			return nil, httpx.NotFound("That comment doesn't exist anymore.")
		}
		if err != nil {
			return nil, err
		}
		if root != nil {
			in.ReplyTo = root // reply to the thread, not to a reply
		}
		path, line, side = rp, rl, rs
		if rc != nil {
			commit = *rc
		}
	} else if in.Path != "" {
		clean, err := CleanPath(in.Path)
		if err != nil || clean == "" {
			return nil, httpx.BadRequest("That file path isn't valid.")
		}
		if in.Line < 1 {
			return nil, httpx.BadRequest("Choose a line to comment on.")
		}
		sd := in.Side
		if sd == "" {
			sd = "new"
		}
		if sd != "new" && sd != "old" {
			return nil, httpx.BadRequest("side is new or old.")
		}
		path, line, side = &clean, &in.Line, &sd
	}
	var reviewID *uuid.UUID
	q := db.Querier(s.d.Pool)
	if review != nil {
		reviewID, q = &review.id, review.tx
	}
	var id uuid.UUID
	write := func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO repos_comments (pull_id, review_id, author_id, body, path, line, side, commit_sha, reply_to)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
			p.ID, reviewID, me.UserID, body, path, line, side, commit, in.ReplyTo).Scan(&id); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE repos_pulls SET comment_count = comment_count + 1, updated_at = now() WHERE id = $1`, p.ID)
		return err
	}
	if review != nil {
		if err := write(review.tx); err != nil {
			return nil, err
		}
	} else if err := db.WithTx(ctx, s.d.Pool, write); err != nil {
		return nil, err
	}
	c, err := scanComment(q.QueryRow(ctx, commentSelect+` WHERE c.id = $1`, id), me.UserID)
	if err != nil {
		return nil, err
	}
	if review == nil {
		s.pullChanged(ctx, repo.ID, p.Number)
		s.notifyComment(ctx, me, repo, p, c)
	}
	return c, nil
}

type editCommentReq struct {
	Body string `json:"body"`
}

func (s *Service) editComment(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	repo, p, _, err := s.pullFromRoute(r, levelComment)
	if err != nil {
		return err
	}
	cid, err := httpx.URLParamUUID(r, "commentID")
	if err != nil {
		return err
	}
	var req editCommentReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	body, err := cleanBody(req.Body)
	if err != nil {
		return err
	}
	tag, err := s.d.Pool.Exec(r.Context(), `UPDATE repos_comments SET body = $4, edited_at = now(), updated_at = now()
		WHERE id = $1 AND pull_id = $2 AND author_id = $3 AND deleted_at IS NULL`, cid, p.ID, me.UserID, body)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.NotFound("You can edit only your own comments.")
	}
	c, err := scanComment(s.d.Pool.QueryRow(r.Context(), commentSelect+` WHERE c.id = $1`, cid), me.UserID)
	if err != nil {
		return err
	}
	s.pullChanged(r.Context(), repo.ID, p.Number)
	httpx.OK(w, c)
	return nil
}

// deleteComment: your own, or anyone's with manage (moderation).
func (s *Service) deleteComment(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	repo, p, lvl, err := s.pullFromRoute(r, levelComment)
	if err != nil {
		return err
	}
	cid, err := httpx.URLParamUUID(r, "commentID")
	if err != nil {
		return err
	}
	err = db.WithTx(r.Context(), s.d.Pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(r.Context(), `UPDATE repos_comments SET deleted_at = now(), body = '', updated_at = now()
			WHERE id = $1 AND pull_id = $2 AND deleted_at IS NULL AND (author_id = $3 OR $4)`, cid, p.ID, me.UserID, lvl >= access.Manage)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.NotFound("You can delete only your own comments.")
		}
		_, err = tx.Exec(r.Context(), `UPDATE repos_pulls SET comment_count = greatest(comment_count - 1, 0) WHERE id = $1`, p.ID)
		return err
	})
	if err != nil {
		return err
	}
	s.pullChanged(r.Context(), repo.ID, p.Number)
	httpx.NoContent(w)
	return nil
}

type resolveReq struct {
	Resolved bool `json:"resolved"`
}

// resolveThread: the pull request's author, the thread's author, or edit.
func (s *Service) resolveThread(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	repo, p, lvl, err := s.pullFromRoute(r, levelComment)
	if err != nil {
		return err
	}
	cid, err := httpx.URLParamUUID(r, "commentID")
	if err != nil {
		return err
	}
	var req resolveReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	var author *uuid.UUID
	err = s.d.Pool.QueryRow(r.Context(), `SELECT author_id FROM repos_comments WHERE id = $1 AND pull_id = $2 AND path IS NOT NULL AND reply_to IS NULL`,
		cid, p.ID).Scan(&author)
	if db.IsNoRows(err) {
		return httpx.NotFound("Thread not found.")
	}
	if err != nil {
		return err
	}
	isAuthor := (author != nil && *author == me.UserID) || (p.Author != nil && p.Author.ID == me.UserID)
	if !isAuthor && lvl < access.Edit {
		return httpx.Forbidden("Only the people in the conversation and those who can edit the repository resolve threads.")
	}
	if req.Resolved {
		_, err = s.d.Pool.Exec(r.Context(), `UPDATE repos_comments SET resolved_at = now(), resolved_by = $2 WHERE id = $1 AND resolved_at IS NULL`, cid, me.UserID)
	} else {
		_, err = s.d.Pool.Exec(r.Context(), `UPDATE repos_comments SET resolved_at = NULL, resolved_by = NULL WHERE id = $1`, cid)
	}
	if err != nil {
		return err
	}
	s.pullChanged(r.Context(), repo.ID, p.Number)
	httpx.NoContent(w)
	return nil
}

// ---- reviews -------------------------------------------------------------------

type reviewCtx struct {
	id uuid.UUID
	tx pgx.Tx
}

// ReviewInput submits a review with optional line comments.
type ReviewInput struct {
	State    string         `json:"state"` // approve | request_changes | comment
	Body     string         `json:"body"`
	Comments []CommentInput `json:"comments"`
}

func (s *Service) submitReview(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	repo, p, lvl, err := s.pullFromRoute(r, levelComment)
	if err != nil {
		return err
	}
	var in ReviewInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	state := map[string]string{"approve": "approved", "request_changes": "changes_requested", "comment": "commented"}[in.State]
	switch {
	case state == "":
		return httpx.BadRequest("A review approves, requests changes or comments.")
	case p.State != "open":
		return httpx.Conflict("This pull request is " + p.State + ".")
	case state != "commented" && lvl < access.Edit:
		return httpx.Forbidden("Approving and requesting changes needs edit access to the repository. You can still comment.")
	case state != "commented" && p.Author != nil && p.Author.ID == me.UserID:
		return httpx.Forbidden("You can't approve or request changes on your own pull request.")
	case len(in.Comments) > maxCommentsPerReview:
		return httpx.BadRequest(fmt.Sprintf("A review holds up to %d line comments.", maxCommentsPerReview))
	}
	body := strings.TrimSpace(in.Body)
	if state == "changes_requested" && body == "" && len(in.Comments) == 0 {
		return httpx.BadRequest("Say what should change.")
	}
	if state == "commented" && body == "" && len(in.Comments) == 0 {
		return httpx.BadRequest("Write a comment or add line comments.")
	}
	if utf8.RuneCountInString(body) > 65536 {
		return httpx.BadRequest("The review is too long.")
	}
	err = db.WithTx(r.Context(), s.d.Pool, func(tx pgx.Tx) error {
		var id uuid.UUID
		if err := tx.QueryRow(r.Context(), `INSERT INTO repos_reviews (pull_id, author_id, state, body, commit_sha)
			VALUES ($1, $2, $3, $4, $5) RETURNING id`, p.ID, me.UserID, state, body, p.HeadSHA).Scan(&id); err != nil {
			return err
		}
		rc := &reviewCtx{id: id, tx: tx}
		for _, c := range in.Comments {
			if c.ReplyTo == nil && c.Path == "" {
				return httpx.BadRequest("Line comments in a review need a file and a line.")
			}
			if _, err := s.createComment(r.Context(), me, repo, p, c, rc); err != nil {
				return err
			}
		}
		_, err := tx.Exec(r.Context(), `UPDATE repos_pulls SET updated_at = now() WHERE id = $1`, p.ID)
		return err
	})
	if err != nil {
		return err
	}
	s.pullChanged(r.Context(), repo.ID, p.Number)
	if p.Author != nil && p.Author.ID != me.UserID {
		verb := map[string]string{"approved": "approved", "changes_requested": "requested changes on", "commented": "reviewed"}[state]
		s.notifyIfVisible(r.Context(), repo, notify.Notification{
			UserID: p.Author.ID, Kind: KindPullReview,
			Title: fmt.Sprintf("%s %s #%d: %s", s.person(me).Name, verb, p.Number, p.Title),
			Body:  truncateUTF8(body, 200),
			Link:  s.pullLink(repo, p.Number),
		})
	}
	p, err = s.loadPull(r.Context(), s.d.Pool, repo.ID, p.Number)
	if err != nil {
		return err
	}
	p, err = s.pullDetail(r.Context(), me, p, lvl)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, p)
	return nil
}

// ---- review requests -----------------------------------------------------------

type reviewersReq struct {
	UserIDs []uuid.UUID `json:"user_ids"`
}

func (s *Service) requestReviewers(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	repo, p, lvl, err := s.pullFromRoute(r, levelView)
	if err != nil {
		return err
	}
	if !canEditPull(me, p, lvl) {
		return httpx.Forbidden("Only the author and people who can edit the repository ask for reviews.")
	}
	var req reviewersReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if len(req.UserIDs) == 0 || len(req.UserIDs) > maxReviewRequests {
		return httpx.BadRequest(fmt.Sprintf("Choose 1 to %d people.", maxReviewRequests))
	}
	var author uuid.UUID
	if p.Author != nil {
		author = p.Author.ID
	}
	var added []uuid.UUID
	err = db.WithTx(r.Context(), s.d.Pool, func(tx pgx.Tx) error {
		var err error
		added, err = s.addReviewRequests(r.Context(), tx, repo, p.ID, me.UserID, author, req.UserIDs)
		return err
	})
	if err != nil {
		return err
	}
	s.notifyReviewRequested(r.Context(), me, repo, p, added)
	s.pullChanged(r.Context(), repo.ID, p.Number)
	rv, err := s.reviewers(r.Context(), s.d.Pool, p.ID)
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": rv})
	return nil
}

// addReviewRequests asks people who can view the repository (not the
// author) and returns those newly asked.
func (s *Service) addReviewRequests(ctx context.Context, tx pgx.Tx, repo *Repo, pullID, by, author uuid.UUID, ids []uuid.UUID) ([]uuid.UUID, error) {
	var added []uuid.UUID
	seen := map[uuid.UUID]bool{}
	for _, id := range ids {
		if seen[id] || id == author {
			continue
		}
		seen[id] = true
		// The reviewer's own level, not capped by the caller's agent scope.
		lvl, err := access.LevelOf(context.Background(), tx, id, RepoKind, repo.ID)
		if err != nil {
			return nil, err
		}
		if lvl < access.View {
			return nil, httpx.BadRequest("Everyone you ask to review must be able to open the repository. Share it with them first.")
		}
		tag, err := tx.Exec(ctx, `INSERT INTO repos_review_requests (pull_id, user_id, requested_by) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, pullID, id, by)
		if err != nil {
			return nil, err
		}
		if tag.RowsAffected() == 1 {
			added = append(added, id)
			if err := addEvent(ctx, tx, pullID, &by, "review_requested", map[string]any{"user_id": id}); err != nil {
				return nil, err
			}
		}
	}
	return added, nil
}

func (s *Service) removeReviewer(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	repo, p, lvl, err := s.pullFromRoute(r, levelView)
	if err != nil {
		return err
	}
	uid, err := httpx.URLParamUUID(r, "userID")
	if err != nil {
		return err
	}
	if uid != me.UserID && !canEditPull(me, p, lvl) {
		return httpx.Forbidden("Only the author and people who can edit the repository change reviewers.")
	}
	err = db.WithTx(r.Context(), s.d.Pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(r.Context(), `DELETE FROM repos_review_requests WHERE pull_id = $1 AND user_id = $2`, p.ID, uid)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		return addEvent(r.Context(), tx, p.ID, &me.UserID, "review_request_removed", map[string]any{"user_id": uid})
	})
	if err != nil {
		return err
	}
	s.pullChanged(r.Context(), repo.ID, p.Number)
	httpx.NoContent(w)
	return nil
}

// ---- notifications -------------------------------------------------------------

func (s *Service) notifyReviewRequested(ctx context.Context, me *auth.Identity, repo *Repo, p *Pull, ids []uuid.UUID) {
	for _, id := range ids {
		s.notifyIfVisible(ctx, repo, notify.Notification{
			UserID: id, Kind: KindReviewRequested,
			Title: fmt.Sprintf("%s asked you to review #%d: %s", s.person(me).Name, p.Number, p.Title),
			Body:  repo.Name + " · " + p.HeadBranch + " → " + p.BaseBranch,
			Link:  s.pullLink(repo, p.Number),
		})
	}
}

// notifyComment tells the author and everyone else in the conversation.
func (s *Service) notifyComment(ctx context.Context, me *auth.Identity, repo *Repo, p *Pull, c *Comment) {
	ids := map[uuid.UUID]bool{}
	if p.Author != nil {
		ids[p.Author.ID] = true
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT DISTINCT author_id FROM repos_comments WHERE pull_id = $1 AND author_id IS NOT NULL
		  AND ($2::uuid IS NULL OR id = $2 OR reply_to = $2)
		LIMIT 50`, p.ID, c.ReplyTo)
	if err == nil {
		for rows.Next() {
			var id uuid.UUID
			if rows.Scan(&id) == nil {
				ids[id] = true
			}
		}
		rows.Close()
	}
	delete(ids, me.UserID)
	where := ""
	if c.Path != nil {
		where = fmt.Sprintf(" on %s line %d", *c.Path, *c.Line)
	}
	for id := range ids {
		s.notifyIfVisible(ctx, repo, notify.Notification{
			UserID: id, Kind: KindPullComment,
			Title: fmt.Sprintf("%s commented%s in #%d: %s", s.person(me).Name, where, p.Number, p.Title),
			Body:  truncateUTF8(c.Body, 200),
			Link:  s.pullLink(repo, p.Number),
		})
	}
}
