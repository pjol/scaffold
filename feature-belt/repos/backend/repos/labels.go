package repos

import (
	"context"
	"errors"
	"net/http"
	"path"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// Labels and milestones (edit level manages them; everyone who can view
// reads them), and issue/pull request templates read from the repository.

var labelColors = map[string]bool{"neutral": true, "primary": true, "accent": true, "success": true, "warning": true, "danger": true, "info": true}

// defaultLabels are what a new repository starts with.
var defaultLabels = []Label{
	{Name: "bug", Color: "danger", Description: "Something isn't working"},
	{Name: "documentation", Color: "info", Description: "Improvements or additions to documentation"},
	{Name: "duplicate", Color: "neutral", Description: "This issue or pull request already exists"},
	{Name: "enhancement", Color: "primary", Description: "New feature or request"},
	{Name: "good first issue", Color: "accent", Description: "Good for newcomers"},
	{Name: "help wanted", Color: "success", Description: "Extra attention is needed"},
	{Name: "question", Color: "warning", Description: "Further information is requested"},
	{Name: "wontfix", Color: "neutral", Description: "This will not be worked on"},
}

func seedDefaultLabels(ctx context.Context, q db.Querier, repoID uuid.UUID) error {
	for _, l := range defaultLabels {
		if _, err := q.Exec(ctx, `INSERT INTO repos_labels (repo_id, name, color, description) VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`,
			repoID, l.Name, l.Color, l.Description); err != nil {
			return err
		}
	}
	return nil
}

// LabelRow is a label with how many open issues and pull requests use it.
type LabelRow struct {
	Label
	OpenIssues int `json:"open_issues"`
	OpenPulls  int `json:"open_pulls"`
}

func (s *Service) listLabels(w http.ResponseWriter, r *http.Request) error {
	repo, err := s.repoFromRoute(r, levelView)
	if err != nil {
		return err
	}
	rows, err := s.d.Pool.Query(r.Context(), `
		SELECT l.id, l.name, l.color, l.description,
		       (SELECT count(*) FROM repos_issue_labels x JOIN repos_issues i ON i.id = x.issue_id WHERE x.label_id = l.id AND i.state = 'open')::int,
		       (SELECT count(*) FROM repos_pull_labels x JOIN repos_pulls p ON p.id = x.pull_id WHERE x.label_id = l.id AND p.state = 'open')::int
		FROM repos_labels l WHERE l.repo_id = $1 ORDER BY lower(l.name), l.id`, repo.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	out := []LabelRow{}
	for rows.Next() {
		var l LabelRow
		if err := rows.Scan(&l.ID, &l.Name, &l.Color, &l.Description, &l.OpenIssues, &l.OpenPulls); err != nil {
			return err
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": out})
	return nil
}

type labelReq struct {
	Name        *string `json:"name"`
	Color       *string `json:"color"`
	Description *string `json:"description"`
}

func cleanLabel(l *Label, req labelReq) error {
	if req.Name != nil {
		l.Name = strings.Join(strings.Fields(*req.Name), " ")
		if n := utf8.RuneCountInString(l.Name); n == 0 || n > 50 {
			return httpx.BadRequest("Name the label (up to 50 characters).")
		}
		if strings.ContainsAny(l.Name, ",\"") {
			return httpx.BadRequest("A label name can't contain commas or quotes.")
		}
	}
	if req.Color != nil {
		if !labelColors[*req.Color] {
			return httpx.BadRequest("color is neutral, primary, accent, success, warning, danger or info.")
		}
		l.Color = *req.Color
	}
	if req.Description != nil {
		l.Description = strings.TrimSpace(*req.Description)
		if utf8.RuneCountInString(l.Description) > 100 {
			return httpx.BadRequest("Keep the description under 100 characters.")
		}
	}
	return nil
}

func (s *Service) createLabel(w http.ResponseWriter, r *http.Request) error {
	repo, err := s.repoFromRoute(r, levelEdit)
	if err != nil {
		return err
	}
	var req labelReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.Name == nil {
		return httpx.BadRequest("Name the label.")
	}
	l := Label{Color: "neutral"}
	if err := cleanLabel(&l, req); err != nil {
		return err
	}
	var n int
	if err := s.d.Pool.QueryRow(r.Context(), `SELECT count(*) FROM repos_labels WHERE repo_id = $1`, repo.ID).Scan(&n); err != nil {
		return err
	}
	if n >= 200 {
		return httpx.Conflict("A repository holds up to 200 labels.")
	}
	err = s.d.Pool.QueryRow(r.Context(), `INSERT INTO repos_labels (repo_id, name, color, description) VALUES ($1, $2, $3, $4) RETURNING id`,
		repo.ID, l.Name, l.Color, l.Description).Scan(&l.ID)
	if db.IsUniqueViolation(err, "repos_labels_name_key") {
		return httpx.Conflict("A label named " + l.Name + " exists already.")
	}
	if err != nil {
		return err
	}
	s.repoChanged(r.Context(), repo.ID)
	httpx.JSON(w, http.StatusCreated, l)
	return nil
}

func (s *Service) updateLabel(w http.ResponseWriter, r *http.Request) error {
	repo, err := s.repoFromRoute(r, levelEdit)
	if err != nil {
		return err
	}
	id, err := httpx.URLParamUUID(r, "labelID")
	if err != nil {
		return httpx.NotFound("Label not found.")
	}
	var req labelReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	var l Label
	if err := s.d.Pool.QueryRow(r.Context(), `SELECT id, name, color, description FROM repos_labels WHERE id = $1 AND repo_id = $2`, id, repo.ID).
		Scan(&l.ID, &l.Name, &l.Color, &l.Description); db.IsNoRows(err) {
		return httpx.NotFound("Label not found.")
	} else if err != nil {
		return err
	}
	if err := cleanLabel(&l, req); err != nil {
		return err
	}
	_, err = s.d.Pool.Exec(r.Context(), `UPDATE repos_labels SET name = $2, color = $3, description = $4, updated_at = now() WHERE id = $1`, l.ID, l.Name, l.Color, l.Description)
	if db.IsUniqueViolation(err, "repos_labels_name_key") {
		return httpx.Conflict("A label named " + l.Name + " exists already.")
	}
	if err != nil {
		return err
	}
	s.repoChanged(r.Context(), repo.ID)
	httpx.OK(w, l)
	return nil
}

func (s *Service) deleteLabel(w http.ResponseWriter, r *http.Request) error {
	repo, err := s.repoFromRoute(r, levelEdit)
	if err != nil {
		return err
	}
	id, err := httpx.URLParamUUID(r, "labelID")
	if err != nil {
		return httpx.NotFound("Label not found.")
	}
	tag, err := s.d.Pool.Exec(r.Context(), `DELETE FROM repos_labels WHERE id = $1 AND repo_id = $2`, id, repo.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.NotFound("Label not found.")
	}
	s.repoChanged(r.Context(), repo.ID)
	httpx.NoContent(w)
	return nil
}

// labelIDsByName finds this repository's labels by name (case-insensitive);
// unknown names are skipped.
func (s *Service) labelIDsByName(ctx context.Context, q db.Querier, repoID uuid.UUID, names []string) ([]uuid.UUID, error) {
	if len(names) == 0 {
		return nil, nil
	}
	lower := make([]string, len(names))
	for i, n := range names {
		lower[i] = strings.ToLower(strings.TrimSpace(n))
	}
	rows, err := q.Query(ctx, `SELECT id FROM repos_labels WHERE repo_id = $1 AND lower(name) = ANY($2)`, repoID, lower)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

// ---- milestones ----------------------------------------------------------------

// Milestone is a milestone with its progress.
type Milestone struct {
	ID          uuid.UUID  `json:"id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	DueOn       *string    `json:"due_on"` // YYYY-MM-DD
	State       string     `json:"state"`
	ClosedAt    *time.Time `json:"closed_at"`
	OpenItems   int        `json:"open_items"`   // open issues and pull requests
	ClosedItems int        `json:"closed_items"` // closed issues, closed and merged pull requests
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

const milestoneSelect = `
	SELECT m.id, m.title, m.description, to_char(m.due_on, 'YYYY-MM-DD'), m.state, m.closed_at,
	       ((SELECT count(*) FROM repos_issues i WHERE i.milestone_id = m.id AND i.state = 'open')
	        + (SELECT count(*) FROM repos_pulls p WHERE p.milestone_id = m.id AND p.state = 'open'))::int,
	       ((SELECT count(*) FROM repos_issues i WHERE i.milestone_id = m.id AND i.state = 'closed')
	        + (SELECT count(*) FROM repos_pulls p WHERE p.milestone_id = m.id AND p.state <> 'open'))::int,
	       m.created_at, m.updated_at
	FROM repos_milestones m`

func (s *Service) listMilestones(w http.ResponseWriter, r *http.Request) error {
	repo, err := s.repoFromRoute(r, levelView)
	if err != nil {
		return err
	}
	state := r.URL.Query().Get("state")
	if state == "" {
		state = "open"
	}
	if state != "open" && state != "closed" && state != "all" {
		return httpx.BadRequest("state is open, closed or all.")
	}
	rows, err := s.d.Pool.Query(r.Context(), milestoneSelect+` WHERE m.repo_id = $1 AND ($2 = 'all' OR m.state = $2)
		ORDER BY m.due_on NULLS LAST, lower(m.title), m.id LIMIT 200`, repo.ID, state)
	if err != nil {
		return err
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Milestone])
	if err != nil {
		return err
	}
	if list == nil {
		list = []Milestone{}
	}
	httpx.OK(w, map[string]any{"items": list})
	return nil
}

func (s *Service) getMilestone(w http.ResponseWriter, r *http.Request) error {
	repo, err := s.repoFromRoute(r, levelView)
	if err != nil {
		return err
	}
	id, err := httpx.URLParamUUID(r, "milestoneID")
	if err != nil {
		return httpx.NotFound("Milestone not found.")
	}
	m, err := s.loadMilestone(r.Context(), repo.ID, id)
	if err != nil {
		return err
	}
	httpx.OK(w, m)
	return nil
}

func (s *Service) loadMilestone(ctx context.Context, repoID, id uuid.UUID) (*Milestone, error) {
	rows, err := s.d.Pool.Query(ctx, milestoneSelect+` WHERE m.id = $1 AND m.repo_id = $2`, id, repoID)
	if err != nil {
		return nil, err
	}
	m, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByPos[Milestone])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.NotFound("Milestone not found.")
	}
	return &m, err
}

type milestoneReq struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
	DueOn       *string `json:"due_on"` // YYYY-MM-DD, "" clears
	State       *string `json:"state"`  // open | closed
}

func (s *Service) saveMilestone(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	repo, err := s.repoFromRoute(r, levelEdit)
	if err != nil {
		return err
	}
	var req milestoneReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	var id uuid.UUID
	creating := r.Method == http.MethodPost
	var title, desc, state string
	var due *time.Time
	if creating {
		state = "open"
		if req.Title == nil {
			return httpx.BadRequest("Give the milestone a title.")
		}
	} else {
		if id, err = httpx.URLParamUUID(r, "milestoneID"); err != nil {
			return httpx.NotFound("Milestone not found.")
		}
		m, err := s.loadMilestone(r.Context(), repo.ID, id)
		if err != nil {
			return err
		}
		title, desc, state = m.Title, m.Description, m.State
		if m.DueOn != nil {
			t, _ := time.Parse("2006-01-02", *m.DueOn)
			due = &t
		}
	}
	if req.Title != nil {
		title = strings.Join(strings.Fields(*req.Title), " ")
		if n := utf8.RuneCountInString(title); n == 0 || n > 100 {
			return httpx.BadRequest("Give the milestone a title (up to 100 characters).")
		}
	}
	if req.Description != nil {
		desc = strings.TrimSpace(*req.Description)
		if utf8.RuneCountInString(desc) > 4000 {
			return httpx.BadRequest("Keep the description under 4,000 characters.")
		}
	}
	if req.DueOn != nil {
		due = nil
		if *req.DueOn != "" {
			t, err := time.Parse("2006-01-02", *req.DueOn)
			if err != nil {
				return httpx.BadRequest("due_on is a date (YYYY-MM-DD).")
			}
			due = &t
		}
	}
	if req.State != nil {
		if *req.State != "open" && *req.State != "closed" {
			return httpx.BadRequest("state is open or closed.")
		}
		state = *req.State
	}
	var dueDate *string
	if due != nil {
		d := due.Format("2006-01-02")
		dueDate = &d
	}
	if creating {
		err = s.d.Pool.QueryRow(r.Context(), `INSERT INTO repos_milestones (repo_id, title, description, due_on, created_by) VALUES ($1, $2, $3, $4::date, $5) RETURNING id`,
			repo.ID, title, desc, dueDate, me.UserID).Scan(&id)
	} else {
		_, err = s.d.Pool.Exec(r.Context(), `UPDATE repos_milestones SET title = $2, description = $3, due_on = $4::date, state = $5,
			closed_at = CASE WHEN $5 = 'closed' THEN COALESCE(closed_at, now()) END, updated_at = now() WHERE id = $1`, id, title, desc, dueDate, state)
	}
	if db.IsUniqueViolation(err, "repos_milestones_title_key") {
		return httpx.Conflict("A milestone named " + title + " exists already.")
	}
	if err != nil {
		return err
	}
	m, err := s.loadMilestone(r.Context(), repo.ID, id)
	if err != nil {
		return err
	}
	s.repoChanged(r.Context(), repo.ID)
	status := http.StatusOK
	if creating {
		status = http.StatusCreated
	}
	httpx.JSON(w, status, m)
	return nil
}

func (s *Service) deleteMilestone(w http.ResponseWriter, r *http.Request) error {
	repo, err := s.repoFromRoute(r, levelEdit)
	if err != nil {
		return err
	}
	id, err := httpx.URLParamUUID(r, "milestoneID")
	if err != nil {
		return httpx.NotFound("Milestone not found.")
	}
	tag, err := s.d.Pool.Exec(r.Context(), `DELETE FROM repos_milestones WHERE id = $1 AND repo_id = $2`, id, repo.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.NotFound("Milestone not found.")
	}
	s.repoChanged(r.Context(), repo.ID)
	httpx.NoContent(w)
	return nil
}

func (s *Service) checkMilestone(ctx context.Context, q db.Querier, repoID, id uuid.UUID) error {
	var ok bool
	if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM repos_milestones WHERE id = $1 AND repo_id = $2)`, id, repoID).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return httpx.BadRequest("Choose a milestone from this repository.")
	}
	return nil
}

// ---- templates -----------------------------------------------------------------

// Template is an issue or pull request template from the repository's
// default branch (.github/ISSUE_TEMPLATE/*.md with front matter, or
// pull_request_template.md).
type Template struct {
	File      string   `json:"file"`
	Name      string   `json:"name"`
	About     string   `json:"about"`
	Title     string   `json:"title"`
	Body      string   `json:"body"`
	Labels    []string `json:"labels"`
	Assignees []string `json:"assignees"`
}

func (s *Service) listTemplates(w http.ResponseWriter, r *http.Request) error {
	repo, err := s.repoFromRoute(r, levelView)
	if err != nil {
		return err
	}
	kind := r.URL.Query().Get("kind")
	if kind != "issue" && kind != "pull" {
		return httpx.BadRequest("kind is issue or pull.")
	}
	list, err := s.templates(r.Context(), repo, kind)
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": list})
	return nil
}

// templates reads the templates on the default branch (none for an empty repository).
func (s *Service) templates(ctx context.Context, repo *Repo, kind string) ([]Template, error) {
	out := []Template{}
	head, err := s.git.RefValue(ctx, repo.ID, "refs/heads/"+repo.DefaultBranch)
	if err != nil || head == "" {
		return out, err
	}
	read := func(p string) (string, bool) {
		e, err := s.git.Entry(ctx, repo.ID, head, p)
		if err != nil || e.Type != "blob" || e.Size > 64<<10 {
			return "", false
		}
		b, _, err := s.git.ReadBlob(ctx, repo.ID, e.OID, 64<<10)
		if err != nil {
			return "", false
		}
		return string(b), true
	}
	if kind == "pull" {
		for _, dir := range []string{".github", "", "docs"} {
			entries, err := s.git.ListTree(ctx, repo.ID, head, dir)
			if err != nil {
				continue
			}
			for _, e := range entries {
				if e.Type == "blob" && strings.EqualFold(path.Base(e.Path), "pull_request_template.md") {
					if src, ok := read(e.Path); ok {
						_, body := parseFrontMatter(src)
						return []Template{{File: e.Path, Name: "Pull request", Body: body, Labels: []string{}, Assignees: []string{}}}, nil
					}
				}
			}
		}
		return out, nil
	}
	entries, err := s.git.ListTree(ctx, repo.ID, head, ".github/ISSUE_TEMPLATE")
	if err != nil {
		return out, nil // no folder
	}
	for _, e := range entries {
		if e.Type != "blob" || !strings.HasSuffix(strings.ToLower(e.Path), ".md") || len(out) >= 20 {
			continue
		}
		src, ok := read(e.Path)
		if !ok {
			continue
		}
		fm, body := parseFrontMatter(src)
		t := Template{File: path.Base(e.Path), Name: fm["name"], About: fm["about"], Title: fm["title"], Body: body,
			Labels: splitList(fm["labels"]), Assignees: splitList(fm["assignees"])}
		if t.Name == "" {
			t.Name = strings.TrimSuffix(path.Base(e.Path), path.Ext(e.Path))
		}
		out = append(out, t)
	}
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

func (s *Service) findTemplate(ctx context.Context, repo *Repo, kind, file string) (*Template, error) {
	list, err := s.templates(ctx, repo, kind)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].File == file {
			return &list[i], nil
		}
	}
	return nil, httpx.BadRequest("That template doesn't exist on the default branch anymore.")
}

// templateAssignees maps a template's assignees (emails of workspace people
// who can view the repository) to ids; others are skipped.
func (s *Service) templateAssignees(ctx context.Context, repo *Repo, emails []string) ([]uuid.UUID, error) {
	if len(emails) == 0 {
		return nil, nil
	}
	lower := make([]string, len(emails))
	for i, e := range emails {
		lower[i] = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(e), "@"))
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT u.id FROM org_members m JOIN users u ON u.id = m.user_id
		WHERE m.org_id = $1 AND lower(u.email) = ANY($2) AND access_level(u.id, $3, $4) >= 1`, repo.OrgID, lower, RepoKind, repo.ID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

// parseFrontMatter reads a leading "---" block of `key: value` lines.
func parseFrontMatter(src string) (map[string]string, string) {
	fm := map[string]string{}
	src = strings.ReplaceAll(src, "\r\n", "\n")
	if !strings.HasPrefix(src, "---\n") {
		return fm, src
	}
	rest := src[4:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return fm, src
	}
	for _, line := range strings.Split(rest[:end], "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		v = strings.Trim(v, `"'`)
		fm[strings.ToLower(strings.TrimSpace(k))] = v
	}
	body := rest[end+4:]
	body = strings.TrimPrefix(strings.TrimPrefix(body, "-"), "\n")
	return fm, strings.TrimLeft(body, "\n")
}

func splitList(v string) []string {
	v = strings.Trim(strings.TrimSpace(v), "[]")
	out := []string{}
	for _, p := range strings.Split(v, ",") {
		if p = strings.Trim(strings.TrimSpace(p), `"'`); p != "" {
			out = append(out, p)
		}
	}
	return out
}
