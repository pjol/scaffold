package repos

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/notify"
)

// Issues: per-repository, numbered with pull requests (#12 is one or the
// other). View reads; comment opens issues, comments and reacts; edit
// triages (labels, assignees, milestones, closing anyone's, lock, pin);
// the author edits and closes their own; manage deletes.

const (
	maxAssignees    = 10
	maxIssueLabels  = 20
	maxPinnedIssues = 3
	issuesPerHour   = 120
)

// Label is a repository label (issues and pull requests).
type Label struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Color       string    `json:"color"`
	Description string    `json:"description"`
}

// MilestoneRef is a milestone shown on an issue or pull request.
type MilestoneRef struct {
	ID    uuid.UUID `json:"id"`
	Title string    `json:"title"`
	DueOn *string   `json:"due_on"` // YYYY-MM-DD
	State string    `json:"state"`
}

// Reaction is one emoji's count on a subject, and whether the caller reacted.
type Reaction struct {
	Emoji string `json:"emoji"`
	Count int    `json:"count"`
	Me    bool   `json:"me"`
}

// MentionRef resolves a <@uuid> token for display.
type MentionRef struct {
	ID          uuid.UUID `json:"id"`
	DisplayName string    `json:"display_name"`
}

// LinkedPull is a pull request that refers to or closes an issue.
type LinkedPull struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	State  string `json:"state"`
	Draft  bool   `json:"draft"`
	Closes bool   `json:"closes"`
}

// Issue is an issue (list rows leave body, reactions and links empty).
type Issue struct {
	ID           uuid.UUID     `json:"id"`
	RepoID       uuid.UUID     `json:"repo_id"`
	Number       int           `json:"number"`
	Title        string        `json:"title"`
	Body         string        `json:"body"`
	State        string        `json:"state"`        // open | closed
	StateReason  *string       `json:"state_reason"` // completed | not_planned (closed only)
	Author       *UserRef      `json:"author"`
	ClosedBy     *UserRef      `json:"closed_by"`
	ClosedAt     *time.Time    `json:"closed_at"`
	Milestone    *MilestoneRef `json:"milestone"`
	Labels       []Label       `json:"labels"`
	Assignees    []UserRef     `json:"assignees"`
	Locked       bool          `json:"locked"`
	Pinned       bool          `json:"pinned"`
	CommentCount int           `json:"comment_count"`
	LinkedPulls  int           `json:"linked_pulls"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
	// Detail only.
	Reactions  []Reaction   `json:"reactions,omitempty"`
	Pulls      []LinkedPull `json:"pulls,omitempty"`
	Mentions   []MentionRef `json:"mentions,omitempty"`
	CanEdit    bool         `json:"can_edit"`    // title, body, close and reopen
	CanTriage  bool         `json:"can_triage"`  // labels, assignees, milestone, lock, pin, anyone's state
	CanComment bool         `json:"can_comment"` // locked issues: edit level only
	CanDelete  bool         `json:"can_delete"`
}

const issueSelect = `
	SELECT i.id, i.repo_id, i.number, i.title, i.body, i.state, i.state_reason,
	       i.author_id, a.display_name, a.avatar_url, a.status,
	       i.closed_by, cb.display_name, cb.avatar_url, cb.status, i.closed_at,
	       m.id, m.title, to_char(m.due_on, 'YYYY-MM-DD'), m.state,
	       i.locked, i.pinned_at IS NOT NULL, i.comment_count, i.created_at, i.updated_at,
	       (SELECT count(*) FROM repos_references x WHERE x.repo_id = i.repo_id AND x.target_number = i.number AND x.source_type = 'pull')::int
	FROM repos_issues i
	LEFT JOIN users a ON a.id = i.author_id
	LEFT JOIN users cb ON cb.id = i.closed_by
	LEFT JOIN repos_milestones m ON m.id = i.milestone_id`

func scanIssue(row pgx.Row) (*Issue, error) {
	var is Issue
	var aID, cID, mID *uuid.UUID
	var aName, aAv, aSt, cName, cAv, cSt, mTitle, mState *string
	var mDue *string
	err := row.Scan(&is.ID, &is.RepoID, &is.Number, &is.Title, &is.Body, &is.State, &is.StateReason,
		&aID, &aName, &aAv, &aSt, &cID, &cName, &cAv, &cSt, &is.ClosedAt,
		&mID, &mTitle, &mDue, &mState, &is.Locked, &is.Pinned, &is.CommentCount, &is.CreatedAt, &is.UpdatedAt, &is.LinkedPulls)
	if err != nil {
		return nil, err
	}
	is.Author, is.ClosedBy = userRef(aID, aName, aAv, aSt), userRef(cID, cName, cAv, cSt)
	if mID != nil {
		is.Milestone = &MilestoneRef{ID: *mID, Title: deref(mTitle), DueOn: mDue, State: deref(mState)}
	}
	is.Labels, is.Assignees = []Label{}, []UserRef{}
	return &is, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

var errIssueNotFound = httpx.NotFound("Issue not found.")

func (s *Service) loadIssue(ctx context.Context, q db.Querier, repoID uuid.UUID, number int) (*Issue, error) {
	is, err := scanIssue(q.QueryRow(ctx, issueSelect+` WHERE i.repo_id = $1 AND i.number = $2`, repoID, number))
	if db.IsNoRows(err) {
		// The number may be a pull request's: say so, so links can follow.
		var isPull bool
		_ = q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM repos_pulls WHERE repo_id = $1 AND number = $2)`, repoID, number).Scan(&isPull)
		if isPull {
			e := httpx.NewError(http.StatusNotFound, "is_pull_request", fmt.Sprintf("#%d is a pull request.", number))
			e.Details = map[string]any{"number": number}
			return nil, e
		}
		return nil, errIssueNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := s.fillIssueLists(ctx, q, []*Issue{is}); err != nil {
		return nil, err
	}
	return is, nil
}

// fillIssueLists loads labels and assignees for a page of issues.
func (s *Service) fillIssueLists(ctx context.Context, q db.Querier, list []*Issue) error {
	if len(list) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(list))
	byID := map[uuid.UUID]*Issue{}
	for i, is := range list {
		ids[i], byID[is.ID] = is.ID, is
	}
	rows, err := q.Query(ctx, `SELECT il.issue_id, l.id, l.name, l.color, l.description FROM repos_issue_labels il
		JOIN repos_labels l ON l.id = il.label_id WHERE il.issue_id = ANY($1) ORDER BY lower(l.name), l.id`, ids)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id uuid.UUID
		var l Label
		if err := rows.Scan(&id, &l.ID, &l.Name, &l.Color, &l.Description); err != nil {
			rows.Close()
			return err
		}
		byID[id].Labels = append(byID[id].Labels, l)
	}
	rows.Close()
	rows, err = q.Query(ctx, `SELECT x.issue_id, `+userRefCols+` FROM repos_issue_assignees x JOIN users u ON u.id = x.user_id
		WHERE x.issue_id = ANY($1) ORDER BY x.created_at, u.id`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var u UserRef
		if err := rows.Scan(&id, &u.ID, &u.DisplayName, &u.AvatarURL); err != nil {
			return err
		}
		byID[id].Assignees = append(byID[id].Assignees, u)
	}
	return rows.Err()
}

func issueNumber(r *http.Request) (int, error) {
	n, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil || n < 1 {
		return 0, errIssueNotFound
	}
	return n, nil
}

// issueFromRoute checks the repository level and loads {number}.
func (s *Service) issueFromRoute(r *http.Request, min access.Level) (*Repo, *Issue, access.Level, error) {
	repo, lvl, err := s.repoLevelFromRoute(r, min)
	if err != nil {
		return nil, nil, lvl, err
	}
	n, err := issueNumber(r)
	if err != nil {
		return nil, nil, lvl, err
	}
	is, err := s.loadIssue(r.Context(), s.d.Pool, repo.ID, n)
	if err != nil {
		return nil, nil, lvl, err
	}
	return repo, is, lvl, nil
}

func canEditIssue(me *auth.Identity, is *Issue, lvl access.Level) bool {
	return lvl >= access.Edit || (is.Author != nil && is.Author.ID == me.UserID && lvl >= access.View)
}

func (s *Service) issueFlags(me *auth.Identity, is *Issue, lvl access.Level) {
	is.CanEdit = canEditIssue(me, is, lvl)
	is.CanTriage = lvl >= access.Edit
	is.CanComment = lvl >= access.Comment && (!is.Locked || lvl >= access.Edit)
	is.CanDelete = lvl >= access.Manage
}

// issueDetail adds reactions, linked pull requests and mention names.
func (s *Service) issueDetail(ctx context.Context, me *auth.Identity, is *Issue, lvl access.Level) (*Issue, error) {
	s.issueFlags(me, is, lvl)
	rx, err := s.reactionsFor(ctx, "issue", []uuid.UUID{is.ID}, me.UserID)
	if err != nil {
		return nil, err
	}
	is.Reactions = rx[is.ID]
	if is.Reactions == nil {
		is.Reactions = []Reaction{}
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT p.number, p.title, p.state, p.draft, bool_or(x.closes) FROM repos_references x
		JOIN repos_pulls p ON p.repo_id = x.repo_id AND p.number = x.source_key::int
		WHERE x.repo_id = $1 AND x.target_number = $2 AND x.source_type = 'pull'
		GROUP BY p.number, p.title, p.state, p.draft ORDER BY p.number`, is.RepoID, is.Number)
	if err != nil {
		return nil, err
	}
	is.Pulls, err = pgx.CollectRows(rows, pgx.RowToStructByPos[LinkedPull])
	if err != nil {
		return nil, err
	}
	if is.Pulls == nil {
		is.Pulls = []LinkedPull{}
	}
	if is.Mentions, err = s.mentionRefs(ctx, is.Body); err != nil {
		return nil, err
	}
	return is, nil
}

// ---- list ----------------------------------------------------------------------

type issueCursor struct {
	T  time.Time `json:"t"`
	N  int       `json:"n"`
	ID uuid.UUID `json:"id"`
}

// sqlArgs numbers placeholders while a query is built.
type sqlArgs struct{ list []any }

func (a *sqlArgs) add(v any) string {
	a.list = append(a.list, v)
	return "$" + strconv.Itoa(len(a.list))
}

// peopleMatching resolves a search value (@me, an email, its local part or a
// name without spaces) to org members.
func (s *Service) peopleMatching(ctx context.Context, orgID uuid.UUID, me uuid.UUID, v string) ([]uuid.UUID, error) {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "@me" {
		return []uuid.UUID{me}, nil
	}
	v = strings.TrimPrefix(v, "@")
	rows, err := s.d.Pool.Query(ctx, `
		SELECT u.id FROM org_members m JOIN users u ON u.id = m.user_id
		WHERE m.org_id = $1 AND (lower(u.email) = $2 OR split_part(lower(u.email), '@', 1) = $2
		   OR lower(replace(u.display_name, ' ', '')) = replace($2, ' ', '') OR lower(u.display_name) = $2)
		LIMIT 20`, orgID, v)
	if err != nil {
		return nil, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if ids == nil {
		ids = []uuid.UUID{}
	}
	return ids, err
}

// issueFilter turns the search syntax into SQL conditions on alias i
// (issues) or p (pull requests: kind "pull").
func (s *Service) issueFilter(ctx context.Context, repo *Repo, me uuid.UUID, q IssueQuery, kind string, a *sqlArgs) ([]string, error) {
	t := "i"
	labelJoin, assignTable, fk := "repos_issue_labels", "repos_issue_assignees", "issue_id"
	if kind == "pull" {
		t, labelJoin, assignTable, fk = "p", "repos_pull_labels", "repos_pull_assignees", "pull_id"
	}
	var where []string
	for _, l := range q.Labels {
		where = append(where, fmt.Sprintf(`EXISTS (SELECT 1 FROM %s xl JOIN repos_labels l ON l.id = xl.label_id WHERE xl.%s = %s.id AND lower(l.name) = %s)`, labelJoin, fk, t, a.add(l)))
	}
	for _, l := range q.NotLabels {
		where = append(where, fmt.Sprintf(`NOT EXISTS (SELECT 1 FROM %s xl JOIN repos_labels l ON l.id = xl.label_id WHERE xl.%s = %s.id AND lower(l.name) = %s)`, labelJoin, fk, t, a.add(l)))
	}
	if q.NoLabel {
		where = append(where, fmt.Sprintf(`NOT EXISTS (SELECT 1 FROM %s xl WHERE xl.%s = %s.id)`, labelJoin, fk, t))
	}
	if q.NoAssignee || q.Assignee == "none" {
		where = append(where, fmt.Sprintf(`NOT EXISTS (SELECT 1 FROM %s xa WHERE xa.%s = %s.id)`, assignTable, fk, t))
	} else if q.Assignee != "" {
		ids, err := s.peopleMatching(ctx, repo.OrgID, me, q.Assignee)
		if err != nil {
			return nil, err
		}
		where = append(where, fmt.Sprintf(`EXISTS (SELECT 1 FROM %s xa WHERE xa.%s = %s.id AND xa.user_id = ANY(%s))`, assignTable, fk, t, a.add(ids)))
	}
	if q.Author != "" {
		ids, err := s.peopleMatching(ctx, repo.OrgID, me, q.Author)
		if err != nil {
			return nil, err
		}
		where = append(where, fmt.Sprintf(`%s.author_id = ANY(%s)`, t, a.add(ids)))
	}
	if q.Mentions != "" {
		ids, err := s.peopleMatching(ctx, repo.OrgID, me, q.Mentions)
		if err != nil {
			return nil, err
		}
		pats := make([]string, len(ids))
		for i, id := range ids {
			pats[i] = "%<@" + id.String() + ">%"
		}
		if kind == "pull" {
			where = append(where, fmt.Sprintf(`(p.body LIKE ANY(%[1]s) OR EXISTS (SELECT 1 FROM repos_comments c WHERE c.pull_id = p.id AND c.deleted_at IS NULL AND c.body LIKE ANY(%[1]s)))`, a.add(pats)))
		} else {
			where = append(where, fmt.Sprintf(`(i.body LIKE ANY(%[1]s) OR EXISTS (SELECT 1 FROM repos_issue_comments c WHERE c.issue_id = i.id AND c.deleted_at IS NULL AND c.body LIKE ANY(%[1]s)))`, a.add(pats)))
		}
	}
	if q.NoMilestone || q.Milestone == "none" {
		where = append(where, t+`.milestone_id IS NULL`)
	} else if q.Milestone != "" {
		where = append(where, fmt.Sprintf(`%s.milestone_id IN (SELECT id FROM repos_milestones WHERE repo_id = %s AND lower(title) = %s)`, t, a.add(repo.ID), a.add(q.Milestone)))
	}
	if q.Text != "" {
		text := strings.ToLower(strings.TrimSpace(q.Text))
		num := strings.TrimPrefix(text, "#")
		where = append(where, fmt.Sprintf(`(lower(%[1]s.title) LIKE '%%' || %[2]s || '%%' OR lower(%[1]s.body) LIKE '%%' || %[2]s || '%%' OR %[1]s.number::text = %[3]s)`,
			t, a.add(likeEscape(text)), a.add(num)))
	}
	if kind != "pull" {
		if q.Reason != "" {
			where = append(where, `i.state_reason = `+a.add(q.Reason))
		}
		if q.Locked != nil {
			where = append(where, `i.locked = `+a.add(*q.Locked))
		}
		if q.Pinned {
			where = append(where, `i.pinned_at IS NOT NULL`)
		}
	} else {
		if q.Draft != nil {
			where = append(where, `p.draft = `+a.add(*q.Draft))
		}
		if q.ReviewRequest != "" {
			ids, err := s.peopleMatching(ctx, repo.OrgID, me, q.ReviewRequest)
			if err != nil {
				return nil, err
			}
			where = append(where, fmt.Sprintf(`EXISTS (SELECT 1 FROM repos_review_requests rr WHERE rr.pull_id = p.id AND rr.user_id = ANY(%s))`, a.add(ids)))
		}
	}
	return where, nil
}

// orderAndCursor returns ORDER BY and the keyset condition for a sort.
func orderAndCursor(t, sort string, c issueCursor, a *sqlArgs) (string, string) {
	col, dir, cmp := t+".created_at", "DESC", "<"
	switch sort {
	case "created-asc":
		dir, cmp = "ASC", ">"
	case "updated":
		col = t + ".updated_at"
	case "updated-asc":
		col, dir, cmp = t+".updated_at", "ASC", ">"
	case "comments":
		col = t + ".comment_count"
	}
	order := fmt.Sprintf(" ORDER BY %s %s, %s.id %s", col, dir, t, dir)
	if c.ID == uuid.Nil {
		return order, ""
	}
	var v any = c.T
	if sort == "comments" {
		v = c.N
	}
	return order, fmt.Sprintf("(%s, %s.id) %s (%s, %s)", col, t, cmp, a.add(v), a.add(c.ID))
}

func issueCursorOf(sort string) func(Issue) any {
	return func(is Issue) any {
		c := issueCursor{T: is.CreatedAt, ID: is.ID, N: is.CommentCount}
		if strings.HasPrefix(sort, "updated") {
			c.T = is.UpdatedAt
		}
		return c
	}
}

// IssueCounts are the open and closed totals for a query without its state.
type IssueCounts struct {
	Open   int `json:"open"`
	Closed int `json:"closed"`
}

// searchIssues is the one filtered list: REST and agent tools.
func (s *Service) searchIssues(ctx context.Context, repo *Repo, me uuid.UUID, raw string, cursorStr string, limit int) ([]Issue, IssueQuery, IssueCounts, error) {
	q := ParseIssueQuery(raw)
	var counts IssueCounts
	var c issueCursor
	if err := httpx.DecodeCursor(cursorStr, &c); err != nil {
		return nil, q, counts, err
	}
	a := &sqlArgs{}
	base := []string{"i.repo_id = " + a.add(repo.ID)}
	filters, err := s.issueFilter(ctx, repo, me, q, "issue", a)
	if err != nil {
		return nil, q, counts, err
	}
	base = append(base, filters...)
	// Totals by state (the tabs), without the state filter.
	if err := s.d.Pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE i.state = 'open'), count(*) FILTER (WHERE i.state = 'closed')
		FROM repos_issues i WHERE `+strings.Join(base, " AND "), a.list...).Scan(&counts.Open, &counts.Closed); err != nil {
		return nil, q, counts, err
	}
	where := append([]string{}, base...)
	switch q.State {
	case "open", "closed":
		where = append(where, "i.state = "+a.add(q.State))
	case "merged":
		return []Issue{}, q, counts, nil
	}
	order, keyset := orderAndCursor("i", q.Sort, c, a)
	if keyset != "" {
		where = append(where, keyset)
	}
	rows, err := s.d.Pool.Query(ctx, issueSelect+` WHERE `+strings.Join(where, " AND ")+order+` LIMIT `+a.add(limit), a.list...)
	if err != nil {
		return nil, q, counts, err
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (*Issue, error) { return scanIssue(r) })
	if err != nil {
		return nil, q, counts, err
	}
	if err := s.fillIssueLists(ctx, s.d.Pool, list); err != nil {
		return nil, q, counts, err
	}
	out := make([]Issue, len(list))
	for i, is := range list {
		is.Body = ""
		out[i] = *is
	}
	return out, q, counts, nil
}

// IssuePage is a page of issues with the state totals and ignored qualifiers.
type IssuePage struct {
	httpx.Page[Issue]
	Counts  IssueCounts `json:"counts"`
	Ignored []string    `json:"ignored"`
}

// listIssues is GET /repos/{repoID}/issues?q=&cursor=&limit=.
func (s *Service) listIssues(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	repo, err := s.repoFromRoute(r, levelView)
	if err != nil {
		return err
	}
	page := httpx.ParsePage(r)
	list, q, counts, err := s.searchIssues(r.Context(), repo, me.UserID, r.URL.Query().Get("q"), page.Cursor, page.Limit+1)
	if err != nil {
		return err
	}
	ignored := q.Unknown
	if ignored == nil {
		ignored = []string{}
	}
	httpx.OK(w, IssuePage{Page: httpx.NewPage(list, page.Limit, issueCursorOf(q.Sort)), Counts: counts, Ignored: ignored})
	return nil
}

func (s *Service) getIssue(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	_, is, lvl, err := s.issueFromRoute(r, levelView)
	if err != nil {
		return err
	}
	is, err = s.issueDetail(r.Context(), me, is, lvl)
	if err != nil {
		return err
	}
	httpx.OK(w, is)
	return nil
}

// ---- create --------------------------------------------------------------------

// IssueInput opens an issue. Labels, assignees and a milestone need edit
// level; a template's labels and assignees apply for anyone.
type IssueInput struct {
	Title       string      `json:"title"`
	Body        string      `json:"body"`
	Labels      []uuid.UUID `json:"labels"`
	Assignees   []uuid.UUID `json:"assignees"`
	MilestoneID *uuid.UUID  `json:"milestone_id"`
	Template    string      `json:"template"` // a file name from GET /templates?kind=issue
}

func cleanIssueTitle(t string) (string, error) {
	t = strings.Join(strings.Fields(t), " ")
	if n := utf8.RuneCountInString(t); n == 0 || n > 256 {
		return "", httpx.BadRequest("Give the issue a title (up to 256 characters).")
	}
	return t, nil
}

func cleanLongText(b, what string) (string, error) {
	b = strings.TrimSpace(strings.ReplaceAll(b, "\r\n", "\n"))
	if utf8.RuneCountInString(b) > 65536 {
		return "", httpx.BadRequest("The " + what + " is too long (up to 65,536 characters).")
	}
	return b, nil
}

func (s *Service) createIssueRoute(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	repo, lvl, err := s.repoLevelFromRoute(r, levelComment)
	if err != nil {
		return err
	}
	var in IssueInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	is, err := s.createIssue(r.Context(), me, repo, lvl, in)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, is)
	return nil
}

// nextNumber takes the repository's next issue/pull request number.
func nextNumber(ctx context.Context, tx pgx.Tx, repoID uuid.UUID) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `UPDATE repos_repos SET next_pull_number = next_pull_number + 1 WHERE id = $1 RETURNING next_pull_number - 1`, repoID).Scan(&n)
	return n, err
}

func (s *Service) createIssue(ctx context.Context, me *auth.Identity, repo *Repo, lvl access.Level, in IssueInput) (*Issue, error) {
	title, err := cleanIssueTitle(in.Title)
	if err != nil {
		return nil, err
	}
	body, err := cleanLongText(in.Body, "description")
	if err != nil {
		return nil, err
	}
	if (len(in.Labels) > 0 || len(in.Assignees) > 0 || in.MilestoneID != nil) && lvl < access.Edit {
		return nil, httpx.Forbidden("Labels, assignees and milestones need edit access to the repository. Open the issue without them.")
	}
	labels, assignees := in.Labels, in.Assignees
	if in.Template != "" {
		tpl, err := s.findTemplate(ctx, repo, "issue", in.Template)
		if err != nil {
			return nil, err
		}
		if tpl != nil {
			ids, err := s.labelIDsByName(ctx, s.d.Pool, repo.ID, tpl.Labels)
			if err != nil {
				return nil, err
			}
			labels = append(labels, ids...)
			who, err := s.templateAssignees(ctx, repo, tpl.Assignees)
			if err != nil {
				return nil, err
			}
			assignees = append(assignees, who...)
		}
	}
	var number int
	var issueID uuid.UUID
	var assigned []uuid.UUID
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var err error
		if number, err = nextNumber(ctx, tx, repo.ID); err != nil {
			return err
		}
		if in.MilestoneID != nil {
			if err := s.checkMilestone(ctx, tx, repo.ID, *in.MilestoneID); err != nil {
				return err
			}
		}
		if err := tx.QueryRow(ctx, `INSERT INTO repos_issues (repo_id, number, title, body, author_id, milestone_id)
			VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`, repo.ID, number, title, body, me.UserID, in.MilestoneID).Scan(&issueID); err != nil {
			return err
		}
		if err := s.setLabels(ctx, tx, repo.ID, "issue", issueID, labels, nil, false); err != nil {
			return err
		}
		assigned, err = s.setAssignees(ctx, tx, repo, "issue", issueID, assignees, me.UserID, false)
		return err
	})
	if err != nil {
		return nil, err
	}
	is, err := s.loadIssue(ctx, s.d.Pool, repo.ID, number)
	if err != nil {
		return nil, err
	}
	s.recordRefs(ctx, repo, me, "issue", number, title, body, false)
	s.notifyAssigned(ctx, me, repo, "issue", number, title, assigned)
	s.notifyMentions(ctx, me, repo, body, fmt.Sprintf("#%d: %s", number, title), s.issueLink(repo, number), nil)
	s.issueChanged(ctx, repo.ID, number)
	s.emitWebhook(ctx, repo, me, "issues", "opened", map[string]any{"issue": s.issueHookView(repo, is)})
	return s.issueDetail(ctx, me, is, lvl)
}

func (s *Service) issueLink(repo *Repo, number int) string {
	return fmt.Sprintf("/repos/%s/issues/%d", repo.ID, number)
}

func (s *Service) issueChanged(ctx context.Context, repoID uuid.UUID, number int) {
	s.publish(ctx, repoID, EventIssueChanged, map[string]any{"number": number})
}

// ---- update --------------------------------------------------------------------

type updateIssueReq struct {
	Title       *string `json:"title"`
	Body        *string `json:"body"`
	State       *string `json:"state"`        // open | closed
	StateReason *string `json:"state_reason"` // completed (default) | not_planned
	MilestoneID *string `json:"milestone_id"` // "" clears
	Locked      *bool   `json:"locked"`
	Pinned      *bool   `json:"pinned"`
}

func (s *Service) updateIssueRoute(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	repo, is, lvl, err := s.issueFromRoute(r, levelView)
	if err != nil {
		return err
	}
	var req updateIssueReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	is, err = s.updateIssue(r.Context(), me, repo, is, lvl, req)
	if err != nil {
		return err
	}
	httpx.OK(w, is)
	return nil
}

func (s *Service) updateIssue(ctx context.Context, me *auth.Identity, repo *Repo, is *Issue, lvl access.Level, req updateIssueReq) (*Issue, error) {
	edit := canEditIssue(me, is, lvl)
	triage := lvl >= access.Edit
	if (req.Title != nil || req.Body != nil || req.State != nil) && !edit {
		return nil, httpx.Forbidden("Only the author and people who can edit the repository change this issue.")
	}
	if (req.MilestoneID != nil || req.Locked != nil || req.Pinned != nil) && !triage {
		return nil, httpx.Forbidden("Milestones, locking and pinning need edit access to the repository.")
	}
	title, body := is.Title, is.Body
	var err error
	if req.Title != nil {
		if title, err = cleanIssueTitle(*req.Title); err != nil {
			return nil, err
		}
	}
	if req.Body != nil {
		if body, err = cleanLongText(*req.Body, "description"); err != nil {
			return nil, err
		}
	}
	state, reason := is.State, deref(is.StateReason)
	if req.State != nil {
		switch *req.State {
		case "open":
			state, reason = "open", ""
		case "closed":
			state, reason = "closed", "completed"
			if req.StateReason != nil {
				if *req.StateReason != "completed" && *req.StateReason != "not_planned" {
					return nil, httpx.BadRequest("state_reason is completed or not_planned.")
				}
				reason = *req.StateReason
			}
		default:
			return nil, httpx.BadRequest("state is open or closed.")
		}
	}
	var events []issueEvent
	action := ""
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if title != is.Title {
			events = append(events, issueEvent{"renamed", map[string]any{"from": is.Title, "to": title}})
			action = "edited"
		}
		if body != is.Body && action == "" {
			action = "edited"
		}
		if state != is.State || (state == "closed" && reason != deref(is.StateReason)) {
			if state == "closed" {
				events = append(events, issueEvent{"closed", map[string]any{"reason": reason}})
				action = "closed"
			} else {
				events = append(events, issueEvent{"reopened", nil})
				action = "reopened"
			}
		}
		var rsn *string
		if reason != "" {
			rsn = &reason
		}
		if _, err := tx.Exec(ctx, `UPDATE repos_issues SET title = $2, body = $3, state = $4, state_reason = $5,
			closed_by = CASE WHEN $4 = 'closed' THEN COALESCE(CASE WHEN state = 'closed' THEN closed_by END, $6) END,
			closed_at = CASE WHEN $4 = 'closed' THEN COALESCE(CASE WHEN state = 'closed' THEN closed_at END, now()) END,
			updated_at = now() WHERE id = $1`, is.ID, title, body, state, rsn, me.UserID); err != nil {
			return err
		}
		if req.MilestoneID != nil {
			var mid *uuid.UUID
			if *req.MilestoneID != "" {
				id, err := uuid.Parse(*req.MilestoneID)
				if err != nil {
					return httpx.BadRequest("milestone_id must be a milestone's id, or empty to clear it.")
				}
				if err := s.checkMilestone(ctx, tx, repo.ID, id); err != nil {
					return err
				}
				mid = &id
			}
			old := (*uuid.UUID)(nil)
			if is.Milestone != nil {
				old = &is.Milestone.ID
			}
			if !sameUUID(old, mid) {
				if _, err := tx.Exec(ctx, `UPDATE repos_issues SET milestone_id = $2, updated_at = now() WHERE id = $1`, is.ID, mid); err != nil {
					return err
				}
				if mid != nil {
					var t string
					_ = tx.QueryRow(ctx, `SELECT title FROM repos_milestones WHERE id = $1`, *mid).Scan(&t)
					events = append(events, issueEvent{"milestoned", map[string]any{"title": t}})
				} else {
					events = append(events, issueEvent{"demilestoned", map[string]any{"title": is.Milestone.Title}})
				}
				if action == "" {
					action = "milestoned"
				}
			}
		}
		if req.Locked != nil && *req.Locked != is.Locked {
			if _, err := tx.Exec(ctx, `UPDATE repos_issues SET locked = $2, updated_at = now() WHERE id = $1`, is.ID, *req.Locked); err != nil {
				return err
			}
			events = append(events, issueEvent{map[bool]string{true: "locked", false: "unlocked"}[*req.Locked], nil})
			if action == "" {
				action = map[bool]string{true: "locked", false: "unlocked"}[*req.Locked]
			}
		}
		if req.Pinned != nil && *req.Pinned != is.Pinned {
			if *req.Pinned {
				var n int
				if err := tx.QueryRow(ctx, `SELECT count(*) FROM repos_issues WHERE repo_id = $1 AND pinned_at IS NOT NULL`, repo.ID).Scan(&n); err != nil {
					return err
				}
				if n >= maxPinnedIssues {
					return httpx.Conflict(fmt.Sprintf("Up to %d issues can be pinned. Unpin one first.", maxPinnedIssues))
				}
				_, err = tx.Exec(ctx, `UPDATE repos_issues SET pinned_at = now() WHERE id = $1`, is.ID)
			} else {
				_, err = tx.Exec(ctx, `UPDATE repos_issues SET pinned_at = NULL WHERE id = $1`, is.ID)
			}
			if err != nil {
				return err
			}
			events = append(events, issueEvent{map[bool]string{true: "pinned", false: "unpinned"}[*req.Pinned], nil})
			if action == "" {
				action = map[bool]string{true: "pinned", false: "unpinned"}[*req.Pinned]
			}
		}
		for _, e := range events {
			if err := addIssueEvent(ctx, tx, is.ID, &me.UserID, e.kind, e.data); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if body != is.Body || title != is.Title {
		s.recordRefs(ctx, repo, me, "issue", is.Number, title, body, false)
		if body != is.Body {
			s.notifyMentions(ctx, me, repo, newMentions(is.Body, body), fmt.Sprintf("#%d: %s", is.Number, title), s.issueLink(repo, is.Number), nil)
		}
	}
	if state != is.State && is.Author != nil && is.Author.ID != me.UserID {
		s.notifyIfVisible(ctx, repo, notify.Notification{
			UserID: is.Author.ID, Kind: KindIssueActivity,
			Title: fmt.Sprintf("%s %s #%d: %s", s.person(me).Name, map[string]string{"open": "reopened", "closed": "closed"}[state], is.Number, title),
			Body:  repo.Name, Link: s.issueLink(repo, is.Number),
		})
	}
	s.issueChanged(ctx, repo.ID, is.Number)
	out, err := s.loadIssue(ctx, s.d.Pool, repo.ID, is.Number)
	if err != nil {
		return nil, err
	}
	if action != "" {
		s.emitWebhook(ctx, repo, me, "issues", action, map[string]any{"issue": s.issueHookView(repo, out)})
	}
	return s.issueDetail(ctx, me, out, lvl)
}

type issueEvent struct {
	kind string
	data map[string]any
}

func sameUUID(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func addIssueEvent(ctx context.Context, q db.Querier, issueID uuid.UUID, actor *uuid.UUID, kind string, data map[string]any) error {
	if data == nil {
		data = map[string]any{}
	}
	_, err := q.Exec(ctx, `INSERT INTO repos_issue_events (issue_id, actor_id, kind, data, created_at) VALUES ($1, $2, $3, $4, clock_timestamp())`, issueID, actor, kind, data)
	return err
}

// deleteIssue is DELETE /issues/{n}: manage level, a person only. The number stays used.
func (s *Service) deleteIssue(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	if _, ok := auth.AgentFrom(r.Context()); ok {
		return errAgentForbidden
	}
	repo, is, _, err := s.issueFromRoute(r, levelManage)
	if err != nil {
		return err
	}
	err = db.WithTx(r.Context(), s.d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), `DELETE FROM repos_reactions WHERE (subject_type = 'issue' AND subject_id = $1)
			OR (subject_type = 'issue_comment' AND subject_id IN (SELECT id FROM repos_issue_comments WHERE issue_id = $1))`, is.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `DELETE FROM repos_references WHERE repo_id = $1 AND ((source_type = 'issue' AND source_key = $2) OR target_number = $3)`,
			repo.ID, strconv.Itoa(is.Number), is.Number); err != nil {
			return err
		}
		_, err := tx.Exec(r.Context(), `DELETE FROM repos_issues WHERE id = $1`, is.ID)
		return err
	})
	if err != nil {
		return err
	}
	s.issueChanged(r.Context(), repo.ID, is.Number)
	s.emitWebhook(r.Context(), repo, me, "issues", "deleted", map[string]any{"issue": s.issueHookView(repo, is)})
	httpx.NoContent(w)
	return nil
}

// ---- labels and assignees on issues and pull requests --------------------------

type idsReq struct {
	IDs []uuid.UUID `json:"ids"`
}

// setIssueLabels is PUT /issues/{n}/labels {ids}: edit level.
func (s *Service) setIssueLabels(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	repo, is, lvl, err := s.issueFromRoute(r, levelEdit)
	if err != nil {
		return err
	}
	var req idsReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if err := s.applyLabels(r.Context(), me, repo, "issue", is.ID, is.Number, req.IDs); err != nil {
		return err
	}
	out, err := s.loadIssue(r.Context(), s.d.Pool, repo.ID, is.Number)
	if err != nil {
		return err
	}
	out, err = s.issueDetail(r.Context(), me, out, lvl)
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

// applyLabels replaces the labels on an issue or pull request with events.
func (s *Service) applyLabels(ctx context.Context, me *auth.Identity, repo *Repo, kind string, id uuid.UUID, number int, ids []uuid.UUID) error {
	if len(ids) > maxIssueLabels {
		return httpx.BadRequest(fmt.Sprintf("Use up to %d labels.", maxIssueLabels))
	}
	var changed bool
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		return s.setLabels(ctx, tx, repo.ID, kind, id, ids, &me.UserID, true, &changed)
	})
	if err != nil {
		return err
	}
	if changed {
		s.itemChanged(ctx, repo, kind, number)
		s.emitItemHook(ctx, repo, me, kind, number, "labeled")
	}
	return nil
}

// setLabels replaces labels (ids must be this repository's); with events,
// it records labeled/unlabeled.
func (s *Service) setLabels(ctx context.Context, tx pgx.Tx, repoID uuid.UUID, kind string, id uuid.UUID, ids []uuid.UUID, actor *uuid.UUID, events bool, changed ...*bool) error {
	table, fk := "repos_issue_labels", "issue_id"
	if kind == "pull" {
		table, fk = "repos_pull_labels", "pull_id"
	}
	ids = uniqueIDs(ids)
	if len(ids) > 0 {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM repos_labels WHERE repo_id = $1 AND id = ANY($2)`, repoID, ids).Scan(&n); err != nil {
			return err
		}
		if n != len(ids) {
			return httpx.BadRequest("Choose labels from this repository.")
		}
	}
	rows, err := tx.Query(ctx, `DELETE FROM `+table+` x USING repos_labels l WHERE x.`+fk+` = $1 AND l.id = x.label_id AND NOT (x.label_id = ANY($2)) RETURNING l.name, l.color`, id, ids)
	if err != nil {
		return err
	}
	type nl struct{ Name, Color string }
	removed, err := pgx.CollectRows(rows, pgx.RowToStructByPos[nl])
	if err != nil {
		return err
	}
	rows, err = tx.Query(ctx, `INSERT INTO `+table+` (`+fk+`, label_id) SELECT $1, l.id FROM repos_labels l WHERE l.id = ANY($2)
		ON CONFLICT DO NOTHING RETURNING (SELECT name FROM repos_labels WHERE id = label_id), (SELECT color FROM repos_labels WHERE id = label_id)`, id, ids)
	if err != nil {
		return err
	}
	added, err := pgx.CollectRows(rows, pgx.RowToStructByPos[nl])
	if err != nil {
		return err
	}
	if len(changed) > 0 && changed[0] != nil {
		*changed[0] = len(added)+len(removed) > 0
	}
	if !events {
		return nil
	}
	for _, l := range added {
		if err := addItemEvent(ctx, tx, kind, id, actor, "labeled", map[string]any{"name": l.Name, "color": l.Color}); err != nil {
			return err
		}
	}
	for _, l := range removed {
		if err := addItemEvent(ctx, tx, kind, id, actor, "unlabeled", map[string]any{"name": l.Name, "color": l.Color}); err != nil {
			return err
		}
	}
	if len(added)+len(removed) > 0 {
		t := "repos_issues"
		if kind == "pull" {
			t = "repos_pulls"
		}
		_, err = tx.Exec(ctx, `UPDATE `+t+` SET updated_at = now() WHERE id = $1`, id)
	}
	return err
}

func addItemEvent(ctx context.Context, q db.Querier, kind string, id uuid.UUID, actor *uuid.UUID, ev string, data map[string]any) error {
	if kind == "pull" {
		return addEvent(ctx, q, id, actor, ev, data)
	}
	return addIssueEvent(ctx, q, id, actor, ev, data)
}

func uniqueIDs(ids []uuid.UUID) []uuid.UUID {
	seen := map[uuid.UUID]bool{}
	out := []uuid.UUID{}
	for _, id := range ids {
		if id != uuid.Nil && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// setIssueAssignees is PUT /issues/{n}/assignees {ids}: edit level.
func (s *Service) setIssueAssignees(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	repo, is, lvl, err := s.issueFromRoute(r, levelEdit)
	if err != nil {
		return err
	}
	var req idsReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if err := s.applyAssignees(r.Context(), me, repo, "issue", is.ID, is.Number, is.Title, req.IDs); err != nil {
		return err
	}
	out, err := s.loadIssue(r.Context(), s.d.Pool, repo.ID, is.Number)
	if err != nil {
		return err
	}
	out, err = s.issueDetail(r.Context(), me, out, lvl)
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

func (s *Service) applyAssignees(ctx context.Context, me *auth.Identity, repo *Repo, kind string, id uuid.UUID, number int, title string, ids []uuid.UUID) error {
	var added []uuid.UUID
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var err error
		added, err = s.setAssignees(ctx, tx, repo, kind, id, ids, me.UserID, true)
		return err
	})
	if err != nil {
		return err
	}
	s.notifyAssigned(ctx, me, repo, kind, number, title, added)
	s.itemChanged(ctx, repo, kind, number)
	s.emitItemHook(ctx, repo, me, kind, number, "assigned")
	return nil
}

// setAssignees replaces assignees (people who can view the repository) and
// returns the newly assigned.
func (s *Service) setAssignees(ctx context.Context, tx pgx.Tx, repo *Repo, kind string, id uuid.UUID, ids []uuid.UUID, by uuid.UUID, events bool) ([]uuid.UUID, error) {
	table, fk := "repos_issue_assignees", "issue_id"
	if kind == "pull" {
		table, fk = "repos_pull_assignees", "pull_id"
	}
	ids = uniqueIDs(ids)
	if len(ids) > maxAssignees {
		return nil, httpx.BadRequest(fmt.Sprintf("Assign up to %d people.", maxAssignees))
	}
	for _, uid := range ids {
		// The assignee's own level, not capped by the caller's agent scope.
		lvl, err := access.LevelOf(context.Background(), tx, uid, RepoKind, repo.ID)
		if err != nil {
			return nil, err
		}
		if lvl < access.View {
			return nil, httpx.BadRequest("Everyone you assign must be able to open the repository. Share it with them first.")
		}
	}
	rows, err := tx.Query(ctx, `DELETE FROM `+table+` WHERE `+fk+` = $1 AND NOT (user_id = ANY($2)) RETURNING user_id`, id, ids)
	if err != nil {
		return nil, err
	}
	removed, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, err
	}
	rows, err = tx.Query(ctx, `INSERT INTO `+table+` (`+fk+`, user_id, assigned_by) SELECT $1, u, $3 FROM unnest($2::uuid[]) u
		ON CONFLICT DO NOTHING RETURNING user_id`, id, ids, by)
	if err != nil {
		return nil, err
	}
	added, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, err
	}
	if events {
		for _, u := range added {
			if err := addItemEvent(ctx, tx, kind, id, &by, "assigned", map[string]any{"user_id": u}); err != nil {
				return nil, err
			}
		}
		for _, u := range removed {
			if err := addItemEvent(ctx, tx, kind, id, &by, "unassigned", map[string]any{"user_id": u}); err != nil {
				return nil, err
			}
		}
	}
	return added, nil
}

// itemChanged publishes the change for an issue or a pull request.
func (s *Service) itemChanged(ctx context.Context, repo *Repo, kind string, number int) {
	if kind == "pull" {
		s.pullChanged(ctx, repo.ID, number)
		return
	}
	s.issueChanged(ctx, repo.ID, number)
}

func (s *Service) notifyAssigned(ctx context.Context, me *auth.Identity, repo *Repo, kind string, number int, title string, ids []uuid.UUID) {
	link := s.issueLink(repo, number)
	if kind == "pull" {
		link = s.pullLink(repo, number)
	}
	for _, id := range ids {
		if id == me.UserID {
			continue
		}
		s.notifyIfVisible(ctx, repo, notify.Notification{
			UserID: id, Kind: KindIssueAssigned,
			Title: fmt.Sprintf("%s assigned you #%d: %s", s.person(me).Name, number, title),
			Body:  repo.Name, Link: link,
		})
	}
}
