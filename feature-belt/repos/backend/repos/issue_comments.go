package repos

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/notify"
)

// Issue conversation: comments with reactions, and the timeline of events
// (labels, assignees, milestones, references, closing and reopening).

// IssueComment is a comment on an issue.
type IssueComment struct {
	ID        uuid.UUID  `json:"id"`
	Author    *UserRef   `json:"author"`
	Body      string     `json:"body"`
	EditedAt  *time.Time `json:"edited_at"`
	Deleted   bool       `json:"deleted"`
	CreatedAt time.Time  `json:"created_at"`
	Reactions []Reaction `json:"reactions"`
	CanEdit   bool       `json:"can_edit"`
	CanDelete bool       `json:"can_delete"`
}

const issueCommentSelect = `
	SELECT c.id, c.author_id, a.display_name, a.avatar_url, a.status, c.body, c.edited_at, c.deleted_at IS NOT NULL, c.created_at
	FROM repos_issue_comments c LEFT JOIN users a ON a.id = c.author_id`

func scanIssueComment(row pgx.Row, me uuid.UUID, lvl access.Level) (*IssueComment, error) {
	var c IssueComment
	var aID *uuid.UUID
	var aName, aAv, aSt *string
	if err := row.Scan(&c.ID, &aID, &aName, &aAv, &aSt, &c.Body, &c.EditedAt, &c.Deleted, &c.CreatedAt); err != nil {
		return nil, err
	}
	c.Author = userRef(aID, aName, aAv, aSt)
	if c.Deleted {
		c.Body = ""
	}
	mine := aID != nil && *aID == me
	c.CanEdit = !c.Deleted && mine
	c.CanDelete = !c.Deleted && (mine || lvl >= access.Manage)
	c.Reactions = []Reaction{}
	return &c, nil
}

// IssueTimelineItem is a comment or an event, in order.
type IssueTimelineItem struct {
	Type    string        `json:"type"` // comment | event
	At      time.Time     `json:"at"`
	Comment *IssueComment `json:"comment,omitempty"`
	Event   *Event        `json:"event,omitempty"`
}

// issueTimeline is GET /issues/{n}/timeline: every comment and event, with
// the names of mentioned people and of people events refer to.
func (s *Service) issueTimeline(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	_, is, lvl, err := s.issueFromRoute(r, levelView)
	if err != nil {
		return err
	}
	items, mentions, err := s.issueTimelineItems(r.Context(), me, is, lvl)
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": items, "mentions": mentions})
	return nil
}

func (s *Service) issueTimelineItems(ctx context.Context, me *auth.Identity, is *Issue, lvl access.Level) ([]IssueTimelineItem, []MentionRef, error) {
	rows, err := s.d.Pool.Query(ctx, issueCommentSelect+` WHERE c.issue_id = $1 ORDER BY c.created_at, c.id`, is.ID)
	if err != nil {
		return nil, nil, err
	}
	comments, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (*IssueComment, error) { return scanIssueComment(r, me.UserID, lvl) })
	if err != nil {
		return nil, nil, err
	}
	ids := make([]uuid.UUID, len(comments))
	var bodies []string
	for i, c := range comments {
		ids[i] = c.ID
		bodies = append(bodies, c.Body)
	}
	rx, err := s.reactionsFor(ctx, "issue_comment", ids, me.UserID)
	if err != nil {
		return nil, nil, err
	}
	items := []IssueTimelineItem{}
	for _, c := range comments {
		if r := rx[c.ID]; r != nil {
			c.Reactions = r
		}
		items = append(items, IssueTimelineItem{Type: "comment", At: c.CreatedAt, Comment: c})
	}
	rows, err = s.d.Pool.Query(ctx, `
		SELECT e.id, e.kind, e.actor_id, a.display_name, a.avatar_url, a.status, e.data, e.created_at
		FROM repos_issue_events e LEFT JOIN users a ON a.id = e.actor_id WHERE e.issue_id = $1 ORDER BY e.created_at, e.id`, is.ID)
	if err != nil {
		return nil, nil, err
	}
	people := map[uuid.UUID]bool{}
	for rows.Next() {
		var ev Event
		var aID *uuid.UUID
		var aName, aAv, aSt *string
		if err := rows.Scan(&ev.ID, &ev.Kind, &aID, &aName, &aAv, &aSt, &ev.Data, &ev.CreatedAt); err != nil {
			rows.Close()
			return nil, nil, err
		}
		ev.Actor = userRef(aID, aName, aAv, aSt)
		if v, ok := ev.Data["user_id"].(string); ok {
			if id, err := uuid.Parse(v); err == nil {
				people[id] = true
			}
		}
		items = append(items, IssueTimelineItem{Type: "event", At: ev.CreatedAt, Event: &ev})
	}
	rows.Close()
	sort.SliceStable(items, func(i, j int) bool { return items[i].At.Before(items[j].At) })
	text := strings.Join(bodies, "\n")
	for id := range people {
		text += " <@" + id.String() + ">"
	}
	mentions, err := s.mentionRefs(ctx, text)
	if err != nil {
		return nil, nil, err
	}
	return items, mentions, nil
}

// ---- comments ------------------------------------------------------------------

type issueCommentReq struct {
	Body string `json:"body"`
}

func (s *Service) addIssueCommentRoute(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	repo, is, lvl, err := s.issueFromRoute(r, levelComment)
	if err != nil {
		return err
	}
	var req issueCommentReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	c, err := s.addIssueComment(r.Context(), me, repo, is, lvl, req.Body)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, c)
	return nil
}

func (s *Service) addIssueComment(ctx context.Context, me *auth.Identity, repo *Repo, is *Issue, lvl access.Level, raw string) (*IssueComment, error) {
	if is.Locked && lvl < access.Edit {
		return nil, httpx.NewError(http.StatusForbidden, "locked", "This conversation is locked. Only people who can edit the repository comment.")
	}
	body, err := cleanBody(raw)
	if err != nil {
		return nil, err
	}
	var id uuid.UUID
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO repos_issue_comments (issue_id, author_id, body) VALUES ($1, $2, $3) RETURNING id`,
			is.ID, me.UserID, body).Scan(&id); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE repos_issues SET comment_count = comment_count + 1, updated_at = now() WHERE id = $1`, is.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	c, err := scanIssueComment(s.d.Pool.QueryRow(ctx, issueCommentSelect+` WHERE c.id = $1`, id), me.UserID, lvl)
	if err != nil {
		return nil, err
	}
	s.recordRefs(ctx, repo, me, "issue", is.Number, is.Title, body, false)
	mentioned := s.notifyMentions(ctx, me, repo, body, fmt.Sprintf("#%d: %s", is.Number, is.Title), s.issueLink(repo, is.Number), nil)
	s.notifyIssueComment(ctx, me, repo, is, body, mentioned)
	s.issueChanged(ctx, repo.ID, is.Number)
	s.emitWebhook(ctx, repo, me, "issue_comment", "created", map[string]any{"issue": s.issueHookView(repo, is), "comment": map[string]any{"id": c.ID, "body": body, "created_at": c.CreatedAt}})
	return c, nil
}

// notifyIssueComment tells the author, assignees and earlier commenters
// (not the people a mention already told).
func (s *Service) notifyIssueComment(ctx context.Context, me *auth.Identity, repo *Repo, is *Issue, body string, skip map[uuid.UUID]bool) {
	ids := map[uuid.UUID]bool{}
	if is.Author != nil {
		ids[is.Author.ID] = true
	}
	for _, a := range is.Assignees {
		ids[a.ID] = true
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT DISTINCT author_id FROM repos_issue_comments WHERE issue_id = $1 AND author_id IS NOT NULL LIMIT 50`, is.ID)
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
	plain := s.plainBody(ctx, body)
	for id := range ids {
		if skip[id] {
			continue
		}
		s.notifyIfVisible(ctx, repo, notify.Notification{
			UserID: id, Kind: KindIssueActivity,
			Title: fmt.Sprintf("%s commented on #%d: %s", s.person(me).Name, is.Number, is.Title),
			Body:  truncateUTF8(plain, 200), Link: s.issueLink(repo, is.Number),
		})
	}
}

func (s *Service) issueCommentFromRoute(r *http.Request, min access.Level) (*Repo, *Issue, access.Level, uuid.UUID, error) {
	repo, is, lvl, err := s.issueFromRoute(r, min)
	if err != nil {
		return nil, nil, lvl, uuid.Nil, err
	}
	cid, err := httpx.URLParamUUID(r, "commentID")
	if err != nil {
		return nil, nil, lvl, uuid.Nil, httpx.NotFound("Comment not found.")
	}
	return repo, is, lvl, cid, nil
}

func (s *Service) editIssueComment(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	repo, is, lvl, cid, err := s.issueCommentFromRoute(r, levelComment)
	if err != nil {
		return err
	}
	var req issueCommentReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	body, err := cleanBody(req.Body)
	if err != nil {
		return err
	}
	var old string
	if err := s.d.Pool.QueryRow(r.Context(), `SELECT body FROM repos_issue_comments WHERE id = $1 AND issue_id = $2`, cid, is.ID).Scan(&old); db.IsNoRows(err) {
		return httpx.NotFound("Comment not found.")
	} else if err != nil {
		return err
	}
	tag, err := s.d.Pool.Exec(r.Context(), `UPDATE repos_issue_comments SET body = $4, edited_at = now(), updated_at = now()
		WHERE id = $1 AND issue_id = $2 AND author_id = $3 AND deleted_at IS NULL`, cid, is.ID, me.UserID, body)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.NotFound("You can edit only your own comments.")
	}
	s.recordRefs(r.Context(), repo, me, "issue", is.Number, is.Title, body, false)
	s.notifyMentions(r.Context(), me, repo, newMentions(old, body), fmt.Sprintf("#%d: %s", is.Number, is.Title), s.issueLink(repo, is.Number), nil)
	c, err := scanIssueComment(s.d.Pool.QueryRow(r.Context(), issueCommentSelect+` WHERE c.id = $1`, cid), me.UserID, lvl)
	if err != nil {
		return err
	}
	s.issueChanged(r.Context(), repo.ID, is.Number)
	httpx.OK(w, c)
	return nil
}

// deleteIssueComment: your own, or anyone's with manage.
func (s *Service) deleteIssueComment(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	repo, is, lvl, cid, err := s.issueCommentFromRoute(r, levelComment)
	if err != nil {
		return err
	}
	err = db.WithTx(r.Context(), s.d.Pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(r.Context(), `UPDATE repos_issue_comments SET deleted_at = now(), body = '', updated_at = now()
			WHERE id = $1 AND issue_id = $2 AND deleted_at IS NULL AND (author_id = $3 OR $4)`, cid, is.ID, me.UserID, lvl >= access.Manage)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.NotFound("You can delete only your own comments.")
		}
		if _, err := tx.Exec(r.Context(), `DELETE FROM repos_reactions WHERE subject_type = 'issue_comment' AND subject_id = $1`, cid); err != nil {
			return err
		}
		_, err = tx.Exec(r.Context(), `UPDATE repos_issues SET comment_count = greatest(comment_count - 1, 0) WHERE id = $1`, is.ID)
		return err
	})
	if err != nil {
		return err
	}
	s.issueChanged(r.Context(), repo.ID, is.Number)
	httpx.NoContent(w)
	return nil
}

// ---- reactions -----------------------------------------------------------------

var reactionEmoji = map[string]bool{"+1": true, "-1": true, "laugh": true, "hooray": true, "confused": true, "heart": true, "rocket": true, "eyes": true}

type reactionReq struct {
	SubjectType string    `json:"subject_type"` // issue | issue_comment | pull | pull_comment
	SubjectID   uuid.UUID `json:"subject_id"`
	Emoji       string    `json:"emoji"`
}

// react is PUT (add) or DELETE (remove) /repos/{repoID}/reactions: comment
// level, on something in this repository the caller can see.
func (s *Service) react(add bool) func(http.ResponseWriter, *http.Request) error {
	return func(w http.ResponseWriter, r *http.Request) error {
		me := auth.MustFromContext(r.Context())
		repo, err := s.repoFromRoute(r, levelComment)
		if err != nil {
			return err
		}
		var req reactionReq
		if err := httpx.Decode(r, &req); err != nil {
			return err
		}
		if !reactionEmoji[req.Emoji] {
			return httpx.BadRequest("emoji is one of +1, -1, laugh, hooray, confused, heart, rocket, eyes.")
		}
		var q string
		switch req.SubjectType {
		case "issue":
			q = `SELECT number FROM repos_issues WHERE id = $1 AND repo_id = $2`
		case "issue_comment":
			q = `SELECT i.number FROM repos_issue_comments c JOIN repos_issues i ON i.id = c.issue_id WHERE c.id = $1 AND i.repo_id = $2 AND c.deleted_at IS NULL`
		case "pull":
			q = `SELECT number FROM repos_pulls WHERE id = $1 AND repo_id = $2`
		case "pull_comment":
			q = `SELECT p.number FROM repos_comments c JOIN repos_pulls p ON p.id = c.pull_id
				LEFT JOIN repos_reviews rv ON rv.id = c.review_id
				WHERE c.id = $1 AND p.repo_id = $2 AND c.deleted_at IS NULL AND (rv.state IS DISTINCT FROM 'pending' OR c.author_id = $3)`
		default:
			return httpx.BadRequest("subject_type is issue, issue_comment, pull or pull_comment.")
		}
		args := []any{req.SubjectID, repo.ID}
		if req.SubjectType == "pull_comment" {
			args = append(args, me.UserID)
		}
		var number int
		if err := s.d.Pool.QueryRow(r.Context(), q, args...).Scan(&number); db.IsNoRows(err) {
			return httpx.NotFound("That isn't in this repository anymore.")
		} else if err != nil {
			return err
		}
		if add {
			_, err = s.d.Pool.Exec(r.Context(), `INSERT INTO repos_reactions (subject_type, subject_id, repo_id, user_id, emoji) VALUES ($1, $2, $3, $4, $5)
				ON CONFLICT DO NOTHING`, req.SubjectType, req.SubjectID, repo.ID, me.UserID, req.Emoji)
		} else {
			_, err = s.d.Pool.Exec(r.Context(), `DELETE FROM repos_reactions WHERE subject_type = $1 AND subject_id = $2 AND user_id = $3 AND emoji = $4`,
				req.SubjectType, req.SubjectID, me.UserID, req.Emoji)
		}
		if err != nil {
			return err
		}
		if strings.HasPrefix(req.SubjectType, "pull") {
			s.pullChanged(r.Context(), repo.ID, number)
		} else {
			s.issueChanged(r.Context(), repo.ID, number)
		}
		rx, err := s.reactionsFor(r.Context(), req.SubjectType, []uuid.UUID{req.SubjectID}, me.UserID)
		if err != nil {
			return err
		}
		list := rx[req.SubjectID]
		if list == nil {
			list = []Reaction{}
		}
		httpx.OK(w, map[string]any{"items": list})
		return nil
	}
}

var emojiOrder = map[string]int{"+1": 0, "-1": 1, "laugh": 2, "hooray": 3, "confused": 4, "heart": 5, "rocket": 6, "eyes": 7}

// reactionsFor summarises reactions on subjects of one type.
func (s *Service) reactionsFor(ctx context.Context, typ string, ids []uuid.UUID, me uuid.UUID) (map[uuid.UUID][]Reaction, error) {
	out := map[uuid.UUID][]Reaction{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT subject_id, emoji, count(*)::int, bool_or(user_id = $3) FROM repos_reactions
		WHERE subject_type = $1 AND subject_id = ANY($2) GROUP BY subject_id, emoji`, typ, ids, me)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var rx Reaction
		if err := rows.Scan(&id, &rx.Emoji, &rx.Count, &rx.Me); err != nil {
			return nil, err
		}
		out[id] = append(out[id], rx)
	}
	for _, list := range out {
		sort.Slice(list, func(i, j int) bool { return emojiOrder[list[i].Emoji] < emojiOrder[list[j].Emoji] })
	}
	return out, rows.Err()
}
