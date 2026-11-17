package repos

import (
	"context"
	"errors"
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

// Pull requests: a branch proposed for merging into another branch of the
// same repository. Edit level opens, merges and closes them (the author can
// always edit, close and reopen their own); comment level comments.

var errPullExists = errors.New("repos: an open pull request exists for this pair")

// UserRef is a person shown beside activity (deleted accounts show as such).
type UserRef struct {
	ID          uuid.UUID `json:"id"`
	DisplayName string    `json:"display_name"`
	AvatarURL   *string   `json:"avatar_url"`
}

// Pull is a pull request.
type Pull struct {
	ID              uuid.UUID     `json:"id"`
	RepoID          uuid.UUID     `json:"repo_id"`
	Number          int           `json:"number"`
	Title           string        `json:"title"`
	Body            string        `json:"body"`
	State           string        `json:"state"` // open | closed | merged
	HeadBranch      string        `json:"head_branch"`
	BaseBranch      string        `json:"base_branch"`
	HeadSHA         string        `json:"head_sha"`
	BaseSHA         string        `json:"base_sha"`
	Author          *UserRef      `json:"author"`
	MergeMethod     *string       `json:"merge_method"`
	MergeCommitSHA  *string       `json:"merge_commit_sha"`
	MergedBy        *UserRef      `json:"merged_by"`
	MergedAt        *time.Time    `json:"merged_at"`
	ClosedBy        *UserRef      `json:"closed_by"`
	ClosedAt        *time.Time    `json:"closed_at"`
	CommentCount    int           `json:"comment_count"`
	Approvals       int           `json:"approvals"`
	ChangesRequests int           `json:"changes_requests"`
	Reviewers       []Reviewer    `json:"reviewers"`
	Draft           bool          `json:"draft"`
	Labels          []Label       `json:"labels"`
	Assignees       []UserRef     `json:"assignees"`
	Milestone       *MilestoneRef `json:"milestone"`
	AutoMerge       *AutoMerge    `json:"auto_merge"`
	ChecksState     string        `json:"checks_state"` // success | failure | pending | "" (the head's statuses)
	CreatedAt       time.Time     `json:"created_at"`
	UpdatedAt       time.Time     `json:"updated_at"`
	// Detail only.
	CanEdit       bool         `json:"can_edit"` // title, description, close and reopen
	CanTriage     bool         `json:"can_triage"`
	ClosingIssues []IssueLink  `json:"closing_issues,omitempty"`
	Reactions     []Reaction   `json:"reactions,omitempty"`
	Mentions      []MentionRef `json:"mentions,omitempty"`
}

// AutoMerge says a pull request merges by itself once it can.
type AutoMerge struct {
	Method    string    `json:"method"`
	EnabledBy *UserRef  `json:"enabled_by"`
	EnabledAt time.Time `json:"enabled_at"`
}

// IssueLink is an issue a pull request closes when it merges.
type IssueLink struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	State  string `json:"state"`
}

// Reviewer is someone asked to review, or who reviewed, with their latest
// decision ("" when they haven't reviewed yet).
type Reviewer struct {
	User      UserRef `json:"user"`
	Requested bool    `json:"requested"`
	State     string  `json:"state"` // approved | changes_requested | commented | ""
	Stale     bool    `json:"stale"` // approved an older head (dismissed)
}

const userRefCols = `u.id, CASE WHEN u.status = 'deleted' THEN 'Deleted user' ELSE u.display_name END, CASE WHEN u.status = 'deleted' THEN NULL ELSE u.avatar_url END`

const pullSelect = `
	SELECT p.id, p.repo_id, p.number, p.title, p.body, p.state, p.head_branch, p.base_branch, p.head_sha, p.base_sha,
	       p.author_id, a.display_name, a.avatar_url, a.status,
	       p.merge_method, p.merge_commit_sha, p.merged_by, mb.display_name, mb.avatar_url, mb.status, p.merged_at,
	       p.closed_by, cb.display_name, cb.avatar_url, cb.status, p.closed_at,
	       p.comment_count, p.created_at, p.updated_at,
	       (SELECT count(*) FROM (SELECT DISTINCT ON (author_id) state, dismissed_at FROM repos_reviews
	          WHERE pull_id = p.id AND state IN ('approved', 'changes_requested') ORDER BY author_id, created_at DESC) l
	        WHERE l.state = 'approved' AND l.dismissed_at IS NULL)::int,
	       (SELECT count(*) FROM (SELECT DISTINCT ON (author_id) state FROM repos_reviews
	          WHERE pull_id = p.id AND state IN ('approved', 'changes_requested') ORDER BY author_id, created_at DESC) l
	        WHERE l.state = 'changes_requested')::int,
	       p.draft, ms.id, ms.title, to_char(ms.due_on, 'YYYY-MM-DD'), ms.state,
	       p.auto_merge_method, p.auto_merge_by, am.display_name, am.avatar_url, am.status, p.auto_merge_at,
	       (SELECT array_agg(st.state) FROM repos_statuses st WHERE st.repo_id = p.repo_id AND st.sha = p.head_sha)
	FROM repos_pulls p
	LEFT JOIN users a ON a.id = p.author_id
	LEFT JOIN users mb ON mb.id = p.merged_by
	LEFT JOIN users cb ON cb.id = p.closed_by
	LEFT JOIN users am ON am.id = p.auto_merge_by
	LEFT JOIN repos_milestones ms ON ms.id = p.milestone_id`

func userRef(id *uuid.UUID, name, avatar *string, status *string) *UserRef {
	if id == nil {
		return nil
	}
	u := &UserRef{ID: *id, DisplayName: "Someone"}
	if name != nil {
		u.DisplayName = *name
	}
	if status != nil && *status == "deleted" {
		u.DisplayName = "Deleted user"
		return u
	}
	u.AvatarURL = avatar
	return u
}

func scanPull(row pgx.Row) (*Pull, error) {
	var p Pull
	var aID, mID, cID *uuid.UUID
	var aName, aAv, aSt, mName, mAv, mSt, cName, cAv, cSt *string
	var msID, amID *uuid.UUID
	var msTitle, msState, amMethod, amName, amAv, amSt *string
	var msDue *string
	var amAt *time.Time
	var states []string
	err := row.Scan(&p.ID, &p.RepoID, &p.Number, &p.Title, &p.Body, &p.State, &p.HeadBranch, &p.BaseBranch, &p.HeadSHA, &p.BaseSHA,
		&aID, &aName, &aAv, &aSt, &p.MergeMethod, &p.MergeCommitSHA, &mID, &mName, &mAv, &mSt, &p.MergedAt,
		&cID, &cName, &cAv, &cSt, &p.ClosedAt, &p.CommentCount, &p.CreatedAt, &p.UpdatedAt, &p.Approvals, &p.ChangesRequests,
		&p.Draft, &msID, &msTitle, &msDue, &msState, &amMethod, &amID, &amName, &amAv, &amSt, &amAt, &states)
	if err != nil {
		return nil, err
	}
	p.Author, p.MergedBy, p.ClosedBy = userRef(aID, aName, aAv, aSt), userRef(mID, mName, mAv, mSt), userRef(cID, cName, cAv, cSt)
	p.Reviewers, p.Labels, p.Assignees = []Reviewer{}, []Label{}, []UserRef{}
	if msID != nil {
		p.Milestone = &MilestoneRef{ID: *msID, Title: deref(msTitle), DueOn: msDue, State: deref(msState)}
	}
	if amMethod != nil && amAt != nil {
		p.AutoMerge = &AutoMerge{Method: *amMethod, EnabledBy: userRef(amID, amName, amAv, amSt), EnabledAt: *amAt}
	}
	p.ChecksState = CombinedState(states)
	return &p, nil
}

func (s *Service) loadPull(ctx context.Context, q db.Querier, repoID uuid.UUID, number int) (*Pull, error) {
	p, err := scanPull(q.QueryRow(ctx, pullSelect+` WHERE p.repo_id = $1 AND p.number = $2`, repoID, number))
	if db.IsNoRows(err) {
		var isIssue bool
		_ = q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM repos_issues WHERE repo_id = $1 AND number = $2)`, repoID, number).Scan(&isIssue)
		if isIssue {
			e := httpx.NewError(http.StatusNotFound, "is_issue", fmt.Sprintf("#%d is an issue.", number))
			e.Details = map[string]any{"number": number}
			return nil, e
		}
		return nil, httpx.NotFound("Pull request not found.")
	}
	if err != nil {
		return nil, err
	}
	if err := fillPullLists(ctx, q, []*Pull{p}); err != nil {
		return nil, err
	}
	return p, nil
}

// fillPullLists loads labels and assignees for a page of pull requests.
func fillPullLists(ctx context.Context, q db.Querier, list []*Pull) error {
	if len(list) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(list))
	byID := map[uuid.UUID]*Pull{}
	for i, p := range list {
		ids[i], byID[p.ID] = p.ID, p
	}
	rows, err := q.Query(ctx, `SELECT x.pull_id, l.id, l.name, l.color, l.description FROM repos_pull_labels x
		JOIN repos_labels l ON l.id = x.label_id WHERE x.pull_id = ANY($1) ORDER BY lower(l.name), l.id`, ids)
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
	rows, err = q.Query(ctx, `SELECT x.pull_id, `+userRefCols+` FROM repos_pull_assignees x JOIN users u ON u.id = x.user_id
		WHERE x.pull_id = ANY($1) ORDER BY x.created_at, u.id`, ids)
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

// pullFromRoute checks the repository level and loads {number}.
func (s *Service) pullFromRoute(r *http.Request, min access.Level) (*Repo, *Pull, access.Level, error) {
	repo, lvl, err := s.repoLevelFromRoute(r, min)
	if err != nil {
		return nil, nil, lvl, err
	}
	n, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil || n < 1 {
		return nil, nil, lvl, httpx.NotFound("Pull request not found.")
	}
	p, err := s.loadPull(r.Context(), s.d.Pool, repo.ID, n)
	if err != nil {
		return nil, nil, lvl, err
	}
	return repo, p, lvl, nil
}

func (s *Service) reviewers(ctx context.Context, q db.Querier, pullID uuid.UUID) ([]Reviewer, error) {
	rows, err := q.Query(ctx, `
		WITH latest AS (
			SELECT DISTINCT ON (author_id) author_id, state, dismissed_at FROM repos_reviews
			WHERE pull_id = $1 AND author_id IS NOT NULL AND state <> 'pending' ORDER BY author_id, (state <> 'commented') DESC, created_at DESC
		), people AS (
			SELECT user_id AS id FROM repos_review_requests WHERE pull_id = $1
			UNION SELECT author_id FROM latest
		)
		SELECT `+userRefCols+`, EXISTS (SELECT 1 FROM repos_review_requests rr WHERE rr.pull_id = $1 AND rr.user_id = u.id),
		       COALESCE(l.state, ''), l.dismissed_at IS NOT NULL
		FROM people JOIN users u ON u.id = people.id LEFT JOIN latest l ON l.author_id = u.id
		ORDER BY lower(u.display_name), u.id`, pullID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Reviewer{}
	for rows.Next() {
		var rv Reviewer
		if err := rows.Scan(&rv.User.ID, &rv.User.DisplayName, &rv.User.AvatarURL, &rv.Requested, &rv.State, &rv.Stale); err != nil {
			return nil, err
		}
		out = append(out, rv)
	}
	return out, rows.Err()
}

func canEditPull(me *auth.Identity, p *Pull, lvl access.Level) bool {
	return lvl >= access.Edit || (p.Author != nil && p.Author.ID == me.UserID && lvl >= access.View)
}

// ---- list ----------------------------------------------------------------------

func (s *Service) listPulls(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	repo, err := s.repoFromRoute(r, levelView)
	if err != nil {
		return err
	}
	q := r.URL.Query()
	query := ParseIssueQuery(q.Get("q"))
	state := q.Get("state")
	if state == "" {
		state = query.State
	}
	var states []string
	switch state {
	case "", "open":
		states = []string{"open"}
		if q.Get("state") == "" && query.State == "" && q.Get("q") != "" {
			states = []string{"open", "closed", "merged"}
		}
	case "closed":
		states = []string{"closed", "merged"}
	case "merged":
		states = []string{"merged"}
	case "all":
		states = []string{"open", "closed", "merged"}
	default:
		return httpx.BadRequest("state is open, closed, merged or all.")
	}
	if q.Get("author") == "me" {
		query.Author = "@me"
	}
	if q.Get("review_requested") == "me" {
		query.ReviewRequest = "@me"
	}
	page := httpx.ParsePage(r)
	var c issueCursor
	if err := httpx.DecodeCursor(page.Cursor, &c); err != nil {
		return err
	}
	if query.Sort == "" {
		query.Sort = "updated"
	}
	a := &sqlArgs{}
	where := []string{"p.repo_id = " + a.add(repo.ID)}
	filters, err := s.issueFilter(r.Context(), repo, me.UserID, query, "pull", a)
	if err != nil {
		return err
	}
	where = append(where, filters...)
	var counts IssueCounts
	if err := s.d.Pool.QueryRow(r.Context(), `SELECT count(*) FILTER (WHERE p.state = 'open'), count(*) FILTER (WHERE p.state <> 'open')
		FROM repos_pulls p WHERE `+strings.Join(where, " AND "), a.list...).Scan(&counts.Open, &counts.Closed); err != nil {
		return err
	}
	where = append(where, "p.state = ANY("+a.add(states)+")")
	order, keyset := orderAndCursor("p", query.Sort, c, a)
	if keyset != "" {
		where = append(where, keyset)
	}
	rows, err := s.d.Pool.Query(r.Context(), pullSelect+` WHERE `+strings.Join(where, " AND ")+order+` LIMIT `+a.add(page.Limit+1), a.list...)
	if err != nil {
		return err
	}
	list, err := collectPulls(rows)
	if err != nil {
		return err
	}
	ptrs := make([]*Pull, len(list))
	for i := range list {
		ptrs[i] = &list[i]
	}
	if err := fillPullLists(r.Context(), s.d.Pool, ptrs); err != nil {
		return err
	}
	sortKey := query.Sort
	ignored := query.Unknown
	if ignored == nil {
		ignored = []string{}
	}
	pg := httpx.NewPage(list, page.Limit, func(p Pull) any {
		c := issueCursor{T: p.CreatedAt, ID: p.ID, N: p.CommentCount}
		if strings.HasPrefix(sortKey, "updated") {
			c.T = p.UpdatedAt
		}
		return c
	})
	httpx.OK(w, map[string]any{"items": pg.Items, "next_cursor": pg.NextCursor, "counts": counts, "ignored": ignored})
	return nil
}

func collectPulls(rows pgx.Rows) ([]Pull, error) {
	defer rows.Close()
	out := []Pull{}
	for rows.Next() {
		p, err := scanPull(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// ---- open ----------------------------------------------------------------------

// OpenPullInput opens a pull request.
type OpenPullInput struct {
	Title       string      `json:"title"`
	Body        string      `json:"body"`
	Head        string      `json:"head"`
	Base        string      `json:"base"` // default: the default branch
	Reviewers   []uuid.UUID `json:"reviewers"`
	Draft       bool        `json:"draft"`
	Labels      []uuid.UUID `json:"labels"`
	Assignees   []uuid.UUID `json:"assignees"`
	MilestoneID *uuid.UUID  `json:"milestone_id"`
}

func (s *Service) openPull(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	repo, err := s.repoFromRoute(r, levelEdit)
	if err != nil {
		return err
	}
	var in OpenPullInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	p, err := s.createPull(r.Context(), me, repo, in)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, p)
	return nil
}

func cleanTitle(t string) (string, error) {
	t = strings.Join(strings.Fields(t), " ")
	if n := utf8.RuneCountInString(t); n == 0 || n > 256 {
		return "", httpx.BadRequest("Give the pull request a title (up to 256 characters).")
	}
	return t, nil
}

func (s *Service) createPull(ctx context.Context, me *auth.Identity, repo *Repo, in OpenPullInput) (*Pull, error) {
	title, err := cleanTitle(in.Title)
	if err != nil {
		return nil, err
	}
	body := strings.TrimSpace(in.Body)
	if utf8.RuneCountInString(body) > 65536 {
		return nil, httpx.BadRequest("The description is too long.")
	}
	head, base := strings.TrimSpace(in.Head), strings.TrimSpace(in.Base)
	if base == "" {
		base = repo.DefaultBranch
	}
	if head == "" || head == base {
		return nil, httpx.BadRequest("Choose a branch to merge that differs from the base.")
	}
	headSHA, err := s.git.RefValue(ctx, repo.ID, "refs/heads/"+head)
	if err != nil {
		return nil, err
	}
	baseSHA, err := s.git.RefValue(ctx, repo.ID, "refs/heads/"+base)
	if err != nil {
		return nil, err
	}
	if headSHA == "" || s.git.CheckRefFormat(ctx, "heads", head) != nil {
		return nil, httpx.NotFound("There's no branch named " + head + ".")
	}
	if baseSHA == "" || s.git.CheckRefFormat(ctx, "heads", base) != nil {
		return nil, httpx.NotFound("There's no branch named " + base + ".")
	}
	if mb, err := s.git.MergeBase(ctx, repo.ID, baseSHA, headSHA); err != nil {
		return nil, err
	} else if mb == "" {
		return nil, httpx.NewError(http.StatusUnprocessableEntity, "unrelated_histories", base+" and "+head+" have no history in common.")
	}
	if ahead, err := s.git.CountRange(ctx, repo.ID, baseSHA, headSHA); err != nil {
		return nil, err
	} else if ahead == 0 {
		return nil, httpx.NewError(http.StatusUnprocessableEntity, "nothing_to_merge", head+" has no commits that aren't on "+base+" already.")
	}
	if len(in.Reviewers) > maxReviewRequests {
		return nil, httpx.BadRequest(fmt.Sprintf("Ask up to %d reviewers.", maxReviewRequests))
	}
	var number int
	var pullID uuid.UUID
	var requested, assigned []uuid.UUID
	reviewers := in.Reviewers
	if !in.Draft {
		// Code owners of the changed files are asked too.
		if owners, err := s.codeOwnerReviewers(ctx, repo, baseSHA, headSHA, me.UserID); err == nil {
			reviewers = append(reviewers, owners...)
		}
		if len(reviewers) > maxReviewRequests {
			reviewers = reviewers[:maxReviewRequests]
		}
	}
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
		err = tx.QueryRow(ctx, `
			INSERT INTO repos_pulls (repo_id, number, title, body, author_id, head_branch, base_branch, head_sha, base_sha, draft, milestone_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) RETURNING id`,
			repo.ID, number, title, body, me.UserID, head, base, headSHA, baseSHA, in.Draft, in.MilestoneID).Scan(&pullID)
		if db.IsUniqueViolation(err, "repos_pulls_open_pair_key") {
			return errPullExists
		}
		if err != nil {
			return err
		}
		if err := addEvent(ctx, tx, pullID, &me.UserID, "opened", map[string]any{"draft": in.Draft}); err != nil {
			return err
		}
		if err := s.setLabels(ctx, tx, repo.ID, "pull", pullID, in.Labels, nil, false); err != nil {
			return err
		}
		if assigned, err = s.setAssignees(ctx, tx, repo, "pull", pullID, in.Assignees, me.UserID, false); err != nil {
			return err
		}
		requested, err = s.addReviewRequests(ctx, tx, repo, pullID, me.UserID, me.UserID, reviewers)
		return err
	})
	if errors.Is(err, errPullExists) {
		var existing int
		_ = s.d.Pool.QueryRow(ctx, `SELECT number FROM repos_pulls WHERE repo_id = $1 AND head_branch = $2 AND base_branch = $3 AND state = 'open'`,
			repo.ID, head, base).Scan(&existing)
		e := httpx.NewError(http.StatusConflict, "pull_exists", fmt.Sprintf("Pull request #%d already proposes %s into %s.", existing, head, base))
		e.Details = map[string]any{"number": existing}
		return nil, e
	}
	if err != nil {
		return nil, err
	}
	if err := s.git.ForceRef(ctx, repo.ID, pullRef(number), headSHA); err != nil {
		return nil, err
	}
	p, err := s.loadPull(ctx, s.d.Pool, repo.ID, number)
	if err != nil {
		return nil, err
	}
	s.notifyReviewRequested(ctx, me, repo, p, requested)
	s.notifyAssigned(ctx, me, repo, "pull", number, title, assigned)
	s.recordRefs(ctx, repo, me, "pull", number, title, body, true)
	s.notifyMentions(ctx, me, repo, body, fmt.Sprintf("#%d: %s", number, title), s.pullLink(repo, number), nil)
	s.pullChanged(ctx, repo.ID, number)
	s.emitWebhook(ctx, repo, me, "pull_request", "opened", map[string]any{"pull_request": s.pullHookView(repo, p)})
	return s.pullDetail(ctx, me, p, access.Edit)
}

func (s *Service) pullDetail(ctx context.Context, me *auth.Identity, p *Pull, lvl access.Level) (*Pull, error) {
	rv, err := s.reviewers(ctx, s.d.Pool, p.ID)
	if err != nil {
		return nil, err
	}
	p.Reviewers = rv
	p.CanEdit = canEditPull(me, p, lvl)
	p.CanTriage = lvl >= access.Edit
	rows, err := s.d.Pool.Query(ctx, `SELECT i.number, i.title, i.state FROM repos_references x
		JOIN repos_issues i ON i.repo_id = x.repo_id AND i.number = x.target_number
		WHERE x.repo_id = $1 AND x.source_type = 'pull' AND x.source_key = $2 AND x.closes ORDER BY i.number`, p.RepoID, strconv.Itoa(p.Number))
	if err != nil {
		return nil, err
	}
	if p.ClosingIssues, err = pgx.CollectRows(rows, pgx.RowToStructByPos[IssueLink]); err != nil {
		return nil, err
	}
	if p.ClosingIssues == nil {
		p.ClosingIssues = []IssueLink{}
	}
	rx, err := s.reactionsFor(ctx, "pull", []uuid.UUID{p.ID}, me.UserID)
	if err != nil {
		return nil, err
	}
	if p.Reactions = rx[p.ID]; p.Reactions == nil {
		p.Reactions = []Reaction{}
	}
	if p.Mentions, err = s.mentionRefs(ctx, p.Body); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Service) getPull(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	_, p, lvl, err := s.pullFromRoute(r, levelView)
	if err != nil {
		return err
	}
	p, err = s.pullDetail(r.Context(), me, p, lvl)
	if err != nil {
		return err
	}
	httpx.OK(w, p)
	return nil
}

// ---- edit, close, reopen -------------------------------------------------------

type updatePullReq struct {
	Title       *string `json:"title"`
	Body        *string `json:"body"`
	Base        *string `json:"base"`
	MilestoneID *string `json:"milestone_id"` // "" clears; edit level
}

func (s *Service) updatePull(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	repo, p, lvl, err := s.pullFromRoute(r, levelView)
	if err != nil {
		return err
	}
	if !canEditPull(me, p, lvl) {
		return httpx.Forbidden("Only the author and people who can edit the repository can change this pull request.")
	}
	var req updatePullReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	title, body, base := p.Title, p.Body, p.BaseBranch
	if req.Title != nil {
		if title, err = cleanTitle(*req.Title); err != nil {
			return err
		}
	}
	if req.Body != nil {
		body = strings.TrimSpace(*req.Body)
		if utf8.RuneCountInString(body) > 65536 {
			return httpx.BadRequest("The description is too long.")
		}
	}
	if req.Base != nil && *req.Base != base {
		if p.State != "open" {
			return httpx.Conflict("Only open pull requests can change their base.")
		}
		base = strings.TrimSpace(*req.Base)
		if base == p.HeadBranch {
			return httpx.BadRequest("The base must differ from the branch being merged.")
		}
		if sha, err := s.git.RefValue(r.Context(), repo.ID, "refs/heads/"+base); err != nil {
			return err
		} else if sha == "" {
			return httpx.NotFound("There's no branch named " + base + ".")
		}
	}
	if req.MilestoneID != nil && lvl < access.Edit {
		return httpx.Forbidden("Milestones need edit access to the repository.")
	}
	err = db.WithTx(r.Context(), s.d.Pool, func(tx pgx.Tx) error {
		if req.MilestoneID != nil {
			var mid *uuid.UUID
			if *req.MilestoneID != "" {
				id, err := uuid.Parse(*req.MilestoneID)
				if err != nil {
					return httpx.BadRequest("milestone_id must be a milestone's id, or empty to clear it.")
				}
				if err := s.checkMilestone(r.Context(), tx, repo.ID, id); err != nil {
					return err
				}
				mid = &id
			}
			var old *uuid.UUID
			if p.Milestone != nil {
				old = &p.Milestone.ID
			}
			if !sameUUID(old, mid) {
				if _, err := tx.Exec(r.Context(), `UPDATE repos_pulls SET milestone_id = $2 WHERE id = $1`, p.ID, mid); err != nil {
					return err
				}
				if mid != nil {
					var t string
					_ = tx.QueryRow(r.Context(), `SELECT title FROM repos_milestones WHERE id = $1`, *mid).Scan(&t)
					if err := addEvent(r.Context(), tx, p.ID, &me.UserID, "milestoned", map[string]any{"title": t}); err != nil {
						return err
					}
				} else if err := addEvent(r.Context(), tx, p.ID, &me.UserID, "demilestoned", map[string]any{"title": p.Milestone.Title}); err != nil {
					return err
				}
			}
		}
		_, err := tx.Exec(r.Context(), `UPDATE repos_pulls SET title = $2, body = $3, base_branch = $4, updated_at = now() WHERE id = $1`,
			p.ID, title, body, base)
		if db.IsUniqueViolation(err, "repos_pulls_open_pair_key") {
			return httpx.Conflict("Another open pull request already proposes " + p.HeadBranch + " into " + base + ".")
		}
		if err != nil {
			return err
		}
		if title != p.Title {
			if err := addEvent(r.Context(), tx, p.ID, &me.UserID, "retitled", map[string]any{"from": p.Title, "to": title}); err != nil {
				return err
			}
		}
		if base != p.BaseBranch {
			return addEvent(r.Context(), tx, p.ID, &me.UserID, "base_changed", map[string]any{"from": p.BaseBranch, "to": base})
		}
		return nil
	})
	if err != nil {
		return err
	}
	if body != p.Body || title != p.Title {
		s.recordRefs(r.Context(), repo, me, "pull", p.Number, title, body, true)
		if body != p.Body {
			s.notifyMentions(r.Context(), me, repo, newMentions(p.Body, body), fmt.Sprintf("#%d: %s", p.Number, title), s.pullLink(repo, p.Number), nil)
		}
	}
	s.pullChanged(r.Context(), repo.ID, p.Number)
	p, err = s.loadPull(r.Context(), s.d.Pool, repo.ID, p.Number)
	if err != nil {
		return err
	}
	s.emitWebhook(r.Context(), repo, me, "pull_request", "edited", map[string]any{"pull_request": s.pullHookView(repo, p)})
	p, err = s.pullDetail(r.Context(), me, p, lvl)
	if err != nil {
		return err
	}
	httpx.OK(w, p)
	return nil
}

func (s *Service) closePull(w http.ResponseWriter, r *http.Request) error {
	return s.setPullState(w, r, "closed")
}

func (s *Service) reopenPull(w http.ResponseWriter, r *http.Request) error {
	return s.setPullState(w, r, "open")
}

func (s *Service) setPullState(w http.ResponseWriter, r *http.Request, to string) error {
	me := auth.MustFromContext(r.Context())
	repo, p, lvl, err := s.pullFromRoute(r, levelView)
	if err != nil {
		return err
	}
	if !canEditPull(me, p, lvl) {
		return httpx.Forbidden("Only the author and people who can edit the repository can close or reopen it.")
	}
	switch {
	case p.State == "merged":
		return httpx.Conflict("This pull request was merged.")
	case p.State == to:
		return httpx.Conflict("This pull request is already " + to + ".")
	}
	var head string
	if to == "open" {
		if head, err = s.git.RefValue(r.Context(), repo.ID, "refs/heads/"+p.HeadBranch); err != nil {
			return err
		} else if head == "" {
			return httpx.Conflict("The branch " + p.HeadBranch + " was deleted. Push it again to reopen this pull request.")
		}
	}
	baseSHA, _ := s.git.RefValue(r.Context(), repo.ID, "refs/heads/"+p.BaseBranch)
	err = db.WithTx(r.Context(), s.d.Pool, func(tx pgx.Tx) error {
		var err error
		if to == "closed" {
			_, err = tx.Exec(r.Context(), `UPDATE repos_pulls SET state = 'closed', closed_by = $2, closed_at = now(),
				base_sha = COALESCE(NULLIF($3, ''), base_sha), updated_at = now() WHERE id = $1`, p.ID, me.UserID, baseSHA)
		} else {
			_, err = tx.Exec(r.Context(), `UPDATE repos_pulls SET state = 'open', closed_by = NULL, closed_at = NULL, head_sha = $2,
				updated_at = now() WHERE id = $1`, p.ID, head)
			if db.IsUniqueViolation(err, "repos_pulls_open_pair_key") {
				return httpx.Conflict("Another open pull request already proposes " + p.HeadBranch + " into " + p.BaseBranch + ".")
			}
		}
		if err != nil {
			return err
		}
		return addEvent(r.Context(), tx, p.ID, &me.UserID, map[string]string{"closed": "closed", "open": "reopened"}[to], nil)
	})
	if err != nil {
		return err
	}
	if head != "" {
		_ = s.git.ForceRef(r.Context(), repo.ID, pullRef(p.Number), head)
	}
	s.pullChanged(r.Context(), repo.ID, p.Number)
	p, err = s.loadPull(r.Context(), s.d.Pool, repo.ID, p.Number)
	if err != nil {
		return err
	}
	s.emitWebhook(r.Context(), repo, me, "pull_request", map[string]string{"closed": "closed", "open": "reopened"}[to], map[string]any{"pull_request": s.pullHookView(repo, p)})
	p, err = s.pullDetail(r.Context(), me, p, lvl)
	if err != nil {
		return err
	}
	httpx.OK(w, p)
	return nil
}

// ---- commits and diff ----------------------------------------------------------

// pullRange returns the commits a pull request compares: the base side (the
// base branch now while open, as recorded once closed or merged) and head.
func (s *Service) pullRange(ctx context.Context, repo *Repo, p *Pull) (base, head, mergeBase string, err error) {
	head = p.HeadSHA
	base = p.BaseSHA
	if p.State == "open" {
		if cur, err := s.git.RefValue(ctx, repo.ID, "refs/heads/"+p.BaseBranch); err != nil {
			return "", "", "", err
		} else if cur != "" {
			base = cur
		}
	}
	mergeBase, err = s.git.MergeBase(ctx, repo.ID, base, head)
	if err != nil {
		return "", "", "", err
	}
	if mergeBase == "" {
		mergeBase = base
	}
	return base, head, mergeBase, nil
}

func (s *Service) pullCommits(w http.ResponseWriter, r *http.Request) error {
	repo, p, _, err := s.pullFromRoute(r, levelView)
	if err != nil {
		return err
	}
	base, head, _, err := s.pullRange(r.Context(), repo, p)
	if err != nil {
		return err
	}
	if p.State == "merged" && p.MergeMethod != nil && *p.MergeMethod == "merge" {
		base = p.BaseSHA
	}
	list, err := s.git.RangeCommits(r.Context(), repo.ID, base, head, maxCompare)
	if err != nil {
		return err
	}
	if list == nil {
		list = []Commit{}
	}
	httpx.OK(w, map[string]any{"items": list})
	return nil
}

// pullDiff is GET /pulls/{n}/diff?path=&commit=: the whole change (merge
// base → head), one file's full patch, or one commit's change.
func (s *Service) pullDiff(w http.ResponseWriter, r *http.Request) error {
	repo, p, _, err := s.pullFromRoute(r, levelView)
	if err != nil {
		return err
	}
	ctx := r.Context()
	_, head, mb, err := s.pullRange(ctx, repo, p)
	if err != nil {
		return err
	}
	onePath, err := queryPath(r)
	if err != nil {
		return err
	}
	var paths []string
	if onePath != "" {
		paths = []string{onePath}
	}
	from, to := mb, head
	if c := strings.ToLower(r.URL.Query().Get("commit")); c != "" {
		if !IsOID(c) {
			return httpx.BadRequest("commit must be a full commit id.")
		}
		if ok, err := s.git.IsAncestor(ctx, repo.ID, c, head); err != nil || !ok {
			return httpx.NotFound("That commit isn't part of this pull request.")
		}
		commit, err := s.git.GetCommit(ctx, repo.ID, c)
		if err != nil {
			return err
		}
		from, to = "", c
		if len(commit.Parents) > 0 {
			from = commit.Parents[0]
		}
	}
	lim := defaultDiffLimits
	if onePath != "" {
		lim.MaxFileBytes = 4 << 20
	}
	lim.IgnoreWhitespace = r.URL.Query().Get("ignore_ws") == "1"
	d, err := s.git.DiffCommits(ctx, repo.ID, from, to, lim, paths...)
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"base": from, "head": to, "diff": d})
	return nil
}

// ---- mergeability --------------------------------------------------------------

// Mergeability says whether a pull request can merge and why not.
type Mergeability struct {
	State             string   `json:"state"` // clean | conflicts | blocked | up_to_date | missing_branch | merged | closed | draft
	Mergeable         bool     `json:"mergeable"`
	CanMerge          bool     `json:"can_merge"` // the caller may merge it now
	Conflicts         []string `json:"conflicts"`
	Blockers          []string `json:"blockers"`
	Methods           []string `json:"methods"`
	Approvals         int      `json:"approvals"`
	RequiredApprovals int      `json:"required_approvals"`
	HeadSHA           string   `json:"head_sha"`
	BaseSHA           string   `json:"base_sha"`
	DeleteBranch      bool     `json:"delete_branch"` // the default for "delete the branch after merging"
	CanDeleteBranch   bool     `json:"can_delete_branch"`
	Behind            int      `json:"behind"`            // base commits the head doesn't have
	CanUpdateBranch   bool     `json:"can_update_branch"` // merge the base into the head from the app
	Checks            Checks   `json:"checks"`
	RequiredChecks    []string `json:"required_checks"`
	CodeOwners        []string `json:"code_owners"` // owners still to approve, when the rule requires them
	CanAutoMerge      bool     `json:"can_auto_merge"`
	RebaseBlocked     string   `json:"rebase_blocked"` // why rebasing isn't possible ("" when it is)
}

func (s *Service) mergeability(ctx context.Context, me *auth.Identity, repo *Repo, p *Pull, lvl access.Level) (*Mergeability, MergeResult, error) {
	m := &Mergeability{Conflicts: []string{}, Blockers: []string{}, Methods: []string{}, HeadSHA: p.HeadSHA, DeleteBranch: repo.DeleteBranchOnMerge,
		RequiredChecks: []string{}, CodeOwners: []string{}, Checks: Checks{SHA: p.HeadSHA, State: "none", Items: []Check{}}}
	if repo.AllowMergeCommit {
		m.Methods = append(m.Methods, "merge")
	}
	if repo.AllowSquash {
		m.Methods = append(m.Methods, "squash")
	}
	if repo.AllowRebase {
		m.Methods = append(m.Methods, "rebase")
	}
	var res MergeResult
	switch p.State {
	case "merged", "closed":
		m.State = p.State
		return m, res, nil
	}
	head, err := s.git.RefValue(ctx, repo.ID, "refs/heads/"+p.HeadBranch)
	if err != nil {
		return nil, res, err
	}
	base, err := s.git.RefValue(ctx, repo.ID, "refs/heads/"+p.BaseBranch)
	if err != nil {
		return nil, res, err
	}
	if head == "" || base == "" {
		m.State = "missing_branch"
		missing := p.HeadBranch
		if base == "" {
			missing = p.BaseBranch
		}
		m.Blockers = append(m.Blockers, "The branch "+missing+" doesn't exist anymore.")
		return m, res, nil
	}
	m.HeadSHA, m.BaseSHA = head, base
	if ok, err := s.git.IsAncestor(ctx, repo.ID, head, base); err != nil {
		return nil, res, err
	} else if ok {
		m.State = "up_to_date"
		m.Blockers = append(m.Blockers, p.BaseBranch+" already has every commit of "+p.HeadBranch+".")
		return m, res, nil
	}
	if res, err = s.git.MergeTrees(ctx, repo.ID, base, head); err != nil {
		if errors.Is(err, errUnrelated) {
			m.State = "conflicts"
			m.Blockers = append(m.Blockers, "The branches have no history in common.")
			return m, res, nil
		}
		return nil, res, err
	}
	m.Conflicts = res.Conflicts
	if n, err := s.git.CountRange(ctx, repo.ID, head, base); err == nil {
		m.Behind = n
	}
	rules, err := s.protections(ctx, s.d.Pool, repo.ID)
	if err != nil {
		return nil, res, err
	}
	rule := protectionFor(rules, p.BaseBranch)
	approvals, changers, approvers, err := s.countDecisions(ctx, repo, p)
	if err != nil {
		return nil, res, err
	}
	m.Approvals = approvals
	if p.Draft {
		m.Blockers = append(m.Blockers, "This pull request is a draft. Mark it ready for review to merge it.")
	}
	if rule != nil {
		m.RequiredApprovals = rule.RequiredApprovals
		if approvals < rule.RequiredApprovals {
			need := rule.RequiredApprovals - approvals
			m.Blockers = append(m.Blockers, fmt.Sprintf("Needs %d more %s from people who can edit the repository.", need, plural(need, "approval", "approvals")))
		}
		if rule.RequireCodeOwnerReview {
			waiting, err := s.codeOwnersWaiting(ctx, repo, base, head, approvers)
			if err != nil {
				return nil, res, err
			}
			m.CodeOwners = waiting
			if len(waiting) > 0 {
				m.Blockers = append(m.Blockers, "Waiting on review from code owners: "+strings.Join(waiting, ", ")+".")
			}
		}
		m.RequiredChecks = append(m.RequiredChecks, rule.RequiredChecks...)
	}
	statuses, err := s.statusesFor(ctx, repo.ID, head)
	if err != nil {
		return nil, res, err
	}
	m.Checks = statusChecks(head, statuses, m.RequiredChecks)
	if len(m.RequiredChecks) > 0 {
		latest := map[string]string{}
		for _, st := range statuses {
			latest[st.Context] = st.State
		}
		missing, failing, pending := MissingChecks(m.RequiredChecks, latest)
		if len(failing) > 0 {
			m.Blockers = append(m.Blockers, "Required checks failed: "+strings.Join(failing, ", ")+".")
		}
		if len(pending)+len(missing) > 0 {
			m.Blockers = append(m.Blockers, "Waiting on required checks: "+strings.Join(append(pending, missing...), ", ")+".")
		}
	}
	for _, name := range changers {
		m.Blockers = append(m.Blockers, name+" requested changes.")
	}
	if len(m.Methods) == 0 {
		m.Blockers = append(m.Blockers, "Every merge method is turned off in the repository's settings.")
	}
	switch {
	case !res.Clean:
		m.State = "conflicts"
	case p.Draft:
		m.State = "draft"
	case len(m.Blockers) > 0:
		m.State = "blocked"
	default:
		m.State, m.Mergeable = "clean", true
	}
	m.CanMerge = m.Mergeable && lvl >= access.Edit
	if r := protectionFor(rules, p.HeadBranch); p.HeadBranch != repo.DefaultBranch && (r == nil || r.AllowDeletion) && lvl >= access.Edit {
		m.CanDeleteBranch = true
	}
	if hr := protectionFor(rules, p.HeadBranch); m.Behind > 0 && res.Clean && lvl >= access.Edit && (hr == nil || !hr.RequirePullRequest) {
		m.CanUpdateBranch = true
	}
	m.CanAutoMerge = repo.AllowAutoMerge && lvl >= access.Edit && !m.Mergeable && res.Clean && p.State == "open"
	if slicesContains(m.Methods, "rebase") {
		if reason, err := s.rebaseBlocked(ctx, repo, base, head); err != nil {
			return nil, res, err
		} else {
			m.RebaseBlocked = reason
		}
	}
	return m, res, nil
}

// countDecisions counts current approvals and outstanding change requests
// from people who can still edit the repository (not the author), and
// returns the approvers.
func (s *Service) countDecisions(ctx context.Context, repo *Repo, p *Pull) (int, []string, []uuid.UUID, error) {
	var author uuid.UUID
	if p.Author != nil {
		author = p.Author.ID
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT l.author_id, l.state, u.display_name FROM (
			SELECT DISTINCT ON (author_id) author_id, state, dismissed_at FROM repos_reviews
			WHERE pull_id = $1 AND state IN ('approved', 'changes_requested') AND author_id IS NOT NULL ORDER BY author_id, created_at DESC
		) l JOIN users u ON u.id = l.author_id AND u.status IN ('active', 'suspended')
		WHERE l.author_id <> $2 AND l.dismissed_at IS NULL AND access_level(l.author_id, $3, $4) >= 3`,
		p.ID, author, RepoKind, repo.ID)
	if err != nil {
		return 0, nil, nil, err
	}
	defer rows.Close()
	approvals := 0
	var changers []string
	var approvers []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		var state, name string
		if err := rows.Scan(&id, &state, &name); err != nil {
			return 0, nil, nil, err
		}
		if state == "approved" {
			approvals++
			approvers = append(approvers, id)
		} else {
			changers = append(changers, name)
		}
	}
	return approvals, changers, approvers, rows.Err()
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func (s *Service) pullMergeability(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	repo, p, lvl, err := s.pullFromRoute(r, levelView)
	if err != nil {
		return err
	}
	m, _, err := s.mergeability(r.Context(), me, repo, p, lvl)
	if err != nil {
		return err
	}
	httpx.OK(w, m)
	return nil
}

// ---- merge ---------------------------------------------------------------------

type mergeReq struct {
	Method       string `json:"method"`        // merge | squash | rebase
	Title        string `json:"title"`         // commit subject (optional)
	Message      string `json:"message"`       // commit body (optional)
	ExpectedHead string `json:"expected_head"` // the head the person reviewed
	DeleteBranch bool   `json:"delete_branch"`
}

// mergePull is POST /pulls/{n}/merge: edit level, a person only (agents
// have no merge tool and this route refuses agent calls).
func (s *Service) mergePull(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	if _, ok := auth.AgentFrom(r.Context()); ok {
		return errAgentForbidden
	}
	repo, p, lvl, err := s.pullFromRoute(r, levelEdit)
	if err != nil {
		return err
	}
	var req mergeReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.ExpectedHead != "" && !IsOID(req.ExpectedHead) {
		return httpx.BadRequest("expected_head must be a full commit id.")
	}
	p, err = s.doMerge(r.Context(), me, repo, p, lvl, req)
	if err != nil {
		return err
	}
	httpx.OK(w, p)
	return nil
}

// doMerge merges a pull request as me (a person; auto-merge runs it as the
// person who turned it on).
func (s *Service) doMerge(ctx context.Context, me *auth.Identity, repo *Repo, p *Pull, lvl access.Level, req mergeReq) (*Pull, error) {
	var merged string
	err := s.withRepoLock(ctx, repo.ID, func(tx pgx.Tx) error {
		// Re-read inside the lock: nobody merges the same request twice.
		cur, err := s.loadPull(ctx, tx, repo.ID, p.Number)
		if err != nil {
			return err
		}
		m, res, err := s.mergeability(ctx, me, repo, cur, lvl)
		if err != nil {
			return err
		}
		if !slicesContains(m.Methods, req.Method) {
			return httpx.BadRequest("This repository merges with: " + strings.Join(m.Methods, ", ") + ".")
		}
		if req.ExpectedHead != "" && m.HeadSHA != req.ExpectedHead {
			e := httpx.NewError(http.StatusConflict, "head_moved", cur.HeadBranch+" has new commits since you looked. Review them before merging.")
			e.Details = map[string]any{"head": m.HeadSHA}
			return e
		}
		if !m.Mergeable {
			msg := "This pull request can't merge yet."
			if len(m.Conflicts) > 0 {
				msg = "This pull request has conflicts: " + strings.Join(m.Conflicts, ", ") + ". Resolve them on the branch first."
			} else if len(m.Blockers) > 0 {
				msg = strings.Join(m.Blockers, " ")
			}
			e := httpx.NewError(http.StatusConflict, "not_mergeable", msg)
			e.Details = map[string]any{"state": m.State, "conflicts": m.Conflicts, "blockers": m.Blockers}
			return e
		}
		title := strings.TrimSpace(req.Title)
		body := strings.TrimSpace(req.Message)
		committer := s.person(me)
		var sha string
		switch req.Method {
		case "rebase":
			if m.RebaseBlocked != "" {
				return httpx.NewError(http.StatusConflict, "not_mergeable", m.RebaseBlocked)
			}
			sha, err = s.rebaseOnto(ctx, repo, m.BaseSHA, m.HeadSHA, committer)
			if err != nil {
				return err
			}
		case "squash":
			if title == "" {
				title = fmt.Sprintf("%s (#%d)", cur.Title, cur.Number)
			}
			if body == "" {
				commits, err := s.git.RangeCommits(ctx, repo.ID, m.BaseSHA, m.HeadSHA, 100)
				if err != nil {
					return err
				}
				var lines []string
				for _, c := range commits {
					lines = append(lines, "* "+c.Subject)
				}
				body = strings.Join(lines, "\n")
			}
			author := committer
			if cur.Author != nil {
				if a, err := s.authorPerson(ctx, cur.Author.ID); err == nil {
					author = a
				}
			}
			if sha, err = s.git.CommitTree(ctx, repo.ID, res.Tree, []string{m.BaseSHA}, title+"\n\n"+body, author, committer); err != nil {
				return err
			}
		default:
			if title == "" {
				title = fmt.Sprintf("Merge pull request #%d from %s", cur.Number, cur.HeadBranch)
			}
			if body == "" {
				body = cur.Title
			}
			if sha, err = s.git.CommitTree(ctx, repo.ID, res.Tree, []string{m.BaseSHA, m.HeadSHA}, title+"\n\n"+body, committer, committer); err != nil {
				return err
			}
		}
		if err := s.git.UpdateRef(ctx, repo.ID, "refs/heads/"+cur.BaseBranch, sha, m.BaseSHA, fmt.Sprintf("merge pull request #%d", cur.Number)); err != nil {
			if errors.Is(err, ErrRefMoved) {
				return httpx.NewError(http.StatusConflict, "base_moved", cur.BaseBranch+" just changed. Check the pull request again and merge.")
			}
			return err
		}
		merged = sha
		if _, err := tx.Exec(ctx, `UPDATE repos_pulls SET state = 'merged', merge_method = $2, merge_commit_sha = $3, merged_by = $4,
			merged_at = now(), base_sha = $5, head_sha = $6, auto_merge_method = NULL, auto_merge_by = NULL, auto_merge_at = NULL,
			updated_at = now() WHERE id = $1`,
			cur.ID, req.Method, sha, me.UserID, m.BaseSHA, m.HeadSHA); err != nil {
			return err
		}
		return addEvent(ctx, tx, cur.ID, &me.UserID, "merged", map[string]any{"commit": sha, "method": req.Method})
	})
	if err != nil {
		return nil, err
	}
	_ = s.git.ForceRef(ctx, repo.ID, pullRef(p.Number), p.HeadSHA)
	s.afterRefUpdates(context.WithoutCancel(ctx), repo, me, []refUpdate{{Old: p.BaseSHA, New: merged, Ref: "refs/heads/" + p.BaseBranch}}, "merge")
	if req.DeleteBranch {
		if err := s.removeBranch(ctx, me, repo, p.HeadBranch); err == nil {
			_ = addEvent(ctx, s.d.Pool, p.ID, &me.UserID, "branch_deleted", map[string]any{"branch": p.HeadBranch, "after_merge": true})
		}
	}
	p, err = s.loadPull(ctx, s.d.Pool, repo.ID, p.Number)
	if err != nil {
		return nil, err
	}
	s.closeIssuesForPull(ctx, repo, me, p)
	s.notifyMerged(ctx, me, repo, p)
	s.pullChanged(ctx, repo.ID, p.Number)
	s.emitWebhook(ctx, repo, me, "pull_request", "closed", map[string]any{"pull_request": s.pullHookView(repo, p)})
	return s.pullDetail(ctx, me, p, lvl)
}

// authorPerson is how a squash commit credits the pull request's author.
func (s *Service) authorPerson(ctx context.Context, userID uuid.UUID) (Person, error) {
	var name, email, status string
	if err := s.d.Pool.QueryRow(ctx, `SELECT display_name, email, status FROM users WHERE id = $1`, userID).Scan(&name, &email, &status); err != nil {
		return Person{}, err
	}
	if status == "deleted" {
		return Person{}, errors.New("deleted")
	}
	return s.person(&auth.Identity{UserID: userID, DisplayName: name, Email: email}), nil
}

func slicesContains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// ---- notifications -------------------------------------------------------------

func (s *Service) pullLink(repo *Repo, number int) string {
	return fmt.Sprintf("/repos/%s/pulls/%d", repo.ID, number)
}

func (s *Service) notifyMerged(ctx context.Context, me *auth.Identity, repo *Repo, p *Pull) {
	ids := map[uuid.UUID]bool{}
	if p.Author != nil {
		ids[p.Author.ID] = true
	}
	rv, _ := s.reviewers(ctx, s.d.Pool, p.ID)
	for _, r := range rv {
		ids[r.User.ID] = true
	}
	delete(ids, me.UserID)
	for id := range ids {
		s.notifyIfVisible(ctx, repo, notify.Notification{
			UserID: id, Kind: KindPullMerged,
			Title: fmt.Sprintf("%s merged #%d: %s", s.person(me).Name, p.Number, p.Title),
			Body:  repo.Name + " · " + p.HeadBranch + " → " + p.BaseBranch,
			Link:  s.pullLink(repo, p.Number),
		})
	}
}

// notifyIfVisible sends only to people who can still view the repository.
func (s *Service) notifyIfVisible(ctx context.Context, repo *Repo, n notify.Notification) {
	// The recipient's own level (never an agent-capped one, even when an agent's action notifies).
	lctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	lvl, err := access.LevelOf(lctx, s.d.Pool, n.UserID, RepoKind, repo.ID)
	if err != nil || lvl < access.View {
		return
	}
	if n.Data == nil {
		n.Data = map[string]any{}
	}
	n.Data["repo_id"] = repo.ID.String()
	s.send(ctx, n)
}
