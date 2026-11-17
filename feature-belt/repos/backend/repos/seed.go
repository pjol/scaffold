package repos

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
)

// SeedDev makes one small, real repository in the first org's first shared
// folder (the team folder the files seed makes), through the same engine
// the app uses: commits on main and on a feature branch, and an open pull
// request with a review request and a line comment. users are admin, sam,
// jordan, priya. It runs the files seed first and skips when the
// repository already exists.
func (s *Service) SeedDev(ctx context.Context, users []uuid.UUID) error {
	if len(users) < 4 || s.git == nil || s.gitProblem != "" {
		return nil
	}
	for _, m := range s.d.Modules {
		if seeder, ok := m.(app.DevSeeder); ok && m.Name() == "files" {
			if err := seeder.SeedDev(ctx, users); err != nil {
				return fmt.Errorf("files seed: %w", err)
			}
		}
	}
	var orgID uuid.UUID
	err := s.d.Pool.QueryRow(ctx, `SELECT id FROM orgs WHERE slug = 'riverside-climbing'`).Scan(&orgID)
	if db.IsNoRows(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var exists bool
	if err := s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM repos_repos WHERE org_id = $1 AND slug = 'route-board')`, orgID).Scan(&exists); err != nil || exists {
		return err
	}
	sam, err := s.seedIdentity(ctx, users[1])
	if err != nil {
		return err
	}
	jordan, err := s.seedIdentity(ctx, users[2])
	if err != nil {
		return err
	}
	// The team folder: the route setters' folder from the files seed, else
	// the org's first shared folder sam can edit.
	var folderID *uuid.UUID
	var fid uuid.UUID
	err = s.d.Pool.QueryRow(ctx, `
		SELECT i.id FROM files_items i
		WHERE i.org_id = $1 AND i.drive = 'shared' AND i.type = 'folder' AND i.trashed_at IS NULL
		  AND access_level($2, $3, i.id) >= 3
		ORDER BY (i.name = 'Route setting') DESC, i.parent_id NULLS FIRST, lower(i.name), i.id LIMIT 1`, orgID, sam.UserID, itemKind).Scan(&fid)
	if err == nil {
		folderID = &fid
	} else if !db.IsNoRows(err) {
		return err
	}

	var repo *Repo
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var err error
		repo, err = s.newRepo(ctx, tx, sam, orgID, folderID, "route-board", "The wall board: current routes, grades and setters.", "main")
		return err
	})
	if err != nil {
		return err
	}
	day := func(n int) time.Time { return time.Now().Add(-time.Duration(n) * 24 * time.Hour) }
	write := func(who *auth.Identity, at time.Time, branch, newBranch, message string, files map[string]string) error {
		head, _ := s.git.RefValue(ctx, repo.ID, "refs/heads/"+branch)
		changes := []ChangeInput{}
		for p, c := range files {
			c := c
			changes = append(changes, ChangeInput{Op: "put", Path: p, Content: &c})
		}
		s.seedNow = at
		_, err := s.commit(ctx, who, repo, CommitInput{Branch: branch, NewBranch: newBranch, ExpectedHead: head, Message: message, Changes: changes}, false)
		s.seedNow = time.Time{}
		return err
	}
	// main: two commits by sam. The protection rule allows no direct
	// commits, so the seed lifts it while it writes main.
	if _, err := s.d.Pool.Exec(ctx, `UPDATE repos_protections SET require_pull_request = false WHERE repo_id = $1`, repo.ID); err != nil {
		return err
	}
	steps := []struct {
		who      *auth.Identity
		at       time.Time
		branch   string
		newB     string
		msg      string
		contents map[string]string
	}{
		{sam, day(9), "main", "", "Start the route board", map[string]string{
			"README.md":                            seedReadme,
			"package.json":                         seedPackage,
			".gitignore":                           "node_modules/\ndist/\n",
			"src/routes.ts":                        seedRoutes,
			"src/index.ts":                         seedIndex,
			"src/grades.ts":                        seedGrades,
			".github/ISSUE_TEMPLATE/bug_report.md": seedBugTemplate,
			".github/ISSUE_TEMPLATE/route_request.md": seedRouteTemplate,
			".github/pull_request_template.md":        seedPullTemplate,
			".github/CODEOWNERS":                      "# Route board owners\n*            " + sam.Email + "\nsrc/grades.ts " + jordan.Email + "\n",
		}},
		{sam, day(6), "main", "", "Filter routes by wall", map[string]string{"src/index.ts": seedIndex2}},
		{jordan, day(3), "main", "feature/grade-colours", "Colour routes by grade band", map[string]string{"src/grades.ts": seedGrades2}},
		{jordan, day(2), "feature/grade-colours", "", "Show the colour beside each route", map[string]string{"src/index.ts": seedIndex3}},
	}
	for _, st := range steps {
		if err := write(st.who, st.at, st.branch, st.newB, st.msg, st.contents); err != nil {
			return fmt.Errorf("seed commit %q: %w", st.msg, err)
		}
	}
	if _, err := s.d.Pool.Exec(ctx, `UPDATE repos_protections SET require_pull_request = true WHERE repo_id = $1`, repo.ID); err != nil {
		return err
	}
	if _, err := s.makeBranch(ctx, sam, repo, "docs/setting-schedule", "main", false); err != nil {
		return err
	}
	p, err := s.createPull(ctx, jordan, repo, OpenPullInput{
		Title:     "Colour routes by grade band",
		Body:      "Setters asked for the board to match the tape colours on the wall.\n\n- Grades map to five bands\n- Each route shows its band's colour\n\nChecked against the March reset sheet.",
		Head:      "feature/grade-colours",
		Reviewers: []uuid.UUID{sam.UserID},
	})
	if err != nil {
		return err
	}
	pull, err := s.loadPull(ctx, s.d.Pool, repo.ID, p.Number)
	if err != nil {
		return err
	}
	if _, err := s.createComment(ctx, sam, repo, pull, CommentInput{Body: "Should V8 and up share the black band? The comp wall uses black for everything over V7.", Path: "src/grades.ts", Line: 12, Side: "new"}, nil); err != nil {
		return err
	}
	if _, err = s.createComment(ctx, jordan, repo, pull, CommentInput{Body: "Ready for a look. The colours come from the tape we already stock."}, nil); err != nil {
		return err
	}
	return s.seedTracker(ctx, repo, sam, jordan, users[3], pull)
}

// seedTracker adds issues, a milestone, a suggestion, statuses and a release.
func (s *Service) seedTracker(ctx context.Context, repo *Repo, sam, jordan *auth.Identity, priyaID uuid.UUID, pull *Pull) error {
	priya, err := s.seedIdentity(ctx, priyaID)
	if err != nil {
		return err
	}
	labels := map[string]uuid.UUID{}
	rows, err := s.d.Pool.Query(ctx, `SELECT name, id FROM repos_labels WHERE repo_id = $1`, repo.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var n string
		var id uuid.UUID
		if err := rows.Scan(&n, &id); err != nil {
			rows.Close()
			return err
		}
		labels[n] = id
	}
	rows.Close()
	var milestone uuid.UUID
	if err := s.d.Pool.QueryRow(ctx, `INSERT INTO repos_milestones (repo_id, title, description, due_on, created_by)
		VALUES ($1, 'Spring reset', 'Everything the board needs before the spring reset.', (now() + interval '14 days')::date, $2) RETURNING id`,
		repo.ID, sam.UserID).Scan(&milestone); err != nil {
		return err
	}
	bug, err := s.createIssue(ctx, priya, repo, access.Comment, IssueInput{
		Title: "Board shows retired routes after a reset",
		Body:  "After Sunday's reset the board still lists Long reach on the overhang.\n\n**Steps**\n1. Open the board\n2. Filter by overhang\n\nExpected: only routes on the wall now.",
	})
	if err != nil {
		return err
	}
	if err := s.applyLabels(ctx, sam, repo, "issue", bug.ID, bug.Number, []uuid.UUID{labels["bug"]}); err != nil {
		return err
	}
	if err := s.applyAssignees(ctx, sam, repo, "issue", bug.ID, bug.Number, bug.Title, []uuid.UUID{jordan.UserID}); err != nil {
		return err
	}
	mid := milestone.String()
	if _, err := s.updateIssue(ctx, sam, repo, bug, access.Manage, updateIssueReq{MilestoneID: &mid}); err != nil {
		return err
	}
	if bug, err = s.loadIssue(ctx, s.d.Pool, repo.ID, bug.Number); err != nil {
		return err
	}
	if _, err := s.addIssueComment(ctx, sam, repo, bug, access.Manage, "<@"+jordan.UserID.String()+"> can the reset crew mark routes retired in src/routes.ts? A setOn date alone doesn't tell us."); err != nil {
		return err
	}
	print, err := s.createIssue(ctx, sam, repo, access.Manage, IssueInput{
		Title: "Print view for the front desk", Body: "A one-page list by wall, big enough to read from the desk.", Labels: []uuid.UUID{labels["enhancement"]}, MilestoneID: &milestone,
	})
	if err != nil {
		return err
	}
	_ = print
	typo, err := s.createIssue(ctx, jordan, repo, access.Edit, IssueInput{Title: "README run steps miss npm install", Labels: []uuid.UUID{labels["documentation"]}})
	if err != nil {
		return err
	}
	closed, reason := "closed", "completed"
	if _, err := s.updateIssue(ctx, jordan, repo, typo, access.Edit, updateIssueReq{State: &closed, StateReason: &reason}); err != nil {
		return err
	}
	// The pull request closes the bug when it merges.
	body := pull.Body + fmt.Sprintf("\n\nCloses #%d", bug.Number)
	if _, err := s.d.Pool.Exec(ctx, `UPDATE repos_pulls SET body = $2 WHERE id = $1`, pull.ID, body); err != nil {
		return err
	}
	s.recordRefs(ctx, repo, jordan, "pull", pull.Number, pull.Title, body, true)
	if _, err := s.createComment(ctx, priya, repo, pull, CommentInput{Path: "src/grades.ts", Line: 12, StartLine: 11, Side: "new",
		Body: "The comp wall tapes everything over V7 black, so this matches. A note helps the next setter:\n```suggestion\n  if (grade <= 7) return \"red\";\n  return \"black\"; // V8 and up: the comp wall's black tape\n```"}, nil); err != nil {
		return err
	}
	// CI: a token, two statuses on the head, and main requires "build".
	_, hash, prefix := newSecret(ciPrefix)
	var tokenID uuid.UUID
	if err := s.d.Pool.QueryRow(ctx, `INSERT INTO repos_ci_tokens (repo_id, name, prefix, secret_hash, created_by) VALUES ($1, 'Wall CI', $2, $3, $4) RETURNING id`,
		repo.ID, prefix, hash, sam.UserID).Scan(&tokenID); err != nil {
		return err
	}
	for _, st := range []struct{ ctx, state, desc string }{{"build", "success", "Built in 41s"}, {"test", "pending", "Running 12 tests"}} {
		if _, err := s.d.Pool.Exec(ctx, `INSERT INTO repos_statuses (repo_id, sha, context, state, description, token_id) VALUES ($1, $2, $3, $4, $5, $6)`,
			repo.ID, pull.HeadSHA, st.ctx, st.state, st.desc, tokenID); err != nil {
			return err
		}
	}
	if _, err := s.d.Pool.Exec(ctx, `UPDATE repos_protections SET required_checks = '{build}' WHERE repo_id = $1 AND pattern = 'main'`, repo.ID); err != nil {
		return err
	}
	// A release of main.
	main, err := s.git.RefValue(ctx, repo.ID, "refs/heads/main")
	if err != nil || main == "" {
		return err
	}
	if err := s.ensureTag(ctx, repo, "v0.1.0", main); err != nil {
		return err
	}
	_, err = s.d.Pool.Exec(ctx, `INSERT INTO repos_releases (repo_id, tag, name, body, target_sha, author_id, published_at)
		VALUES ($1, 'v0.1.0', 'First board', $2, $3, $4, now() - interval '5 days')`, repo.ID,
		"The board by the front desk, filtered by wall.\n\n* Lists every route with grade and wall\n* `npm run dev <wall>` filters by wall", main, sam.UserID)
	return err
}

func (s *Service) seedIdentity(ctx context.Context, id uuid.UUID) (*auth.Identity, error) {
	me := &auth.Identity{UserID: id}
	err := s.d.Pool.QueryRow(ctx, `SELECT email, display_name, status FROM users WHERE id = $1`, id).Scan(&me.Email, &me.DisplayName, &me.Status)
	return me, err
}

const seedReadme = `# Route board

The board by the front desk: every route on the walls with its grade, colour and setter.

## Run it

` + "```sh\nnpm install\nnpm run dev\n```" + `

Routes live in ` + "`src/routes.ts`" + `; the reset crew edits that file after each reset.
`

const seedPackage = `{
  "name": "route-board",
  "private": true,
  "type": "module",
  "scripts": {
    "dev": "tsx watch src/index.ts",
    "build": "tsc -p ."
  },
  "devDependencies": {
    "tsx": "^4.19.2",
    "typescript": "^5.7.2"
  }
}
`

const seedRoutes = `export interface Route {
  id: string;
  name: string;
  wall: "cave" | "slab" | "overhang" | "comp";
  grade: number; // V scale
  setter: string;
  setOn: string; // ISO date
}

export const routes: Route[] = [
  { id: "r1", name: "Morning coffee", wall: "slab", grade: 1, setter: "Jordan", setOn: "2026-10-03" },
  { id: "r2", name: "Pinch me", wall: "cave", grade: 4, setter: "Priya", setOn: "2026-10-03" },
  { id: "r3", name: "Long reach", wall: "overhang", grade: 6, setter: "Sam", setOn: "2026-10-10" },
  { id: "r4", name: "Comp warm-up", wall: "comp", grade: 3, setter: "Jordan", setOn: "2026-10-17" },
  { id: "r5", name: "The roof", wall: "overhang", grade: 8, setter: "Sam", setOn: "2026-10-17" },
];
`

const seedGrades = `export function gradeLabel(grade: number): string {
  return "V" + grade;
}
`

const seedGrades2 = `export function gradeLabel(grade: number): string {
  return "V" + grade;
}

export type Band = "green" | "blue" | "yellow" | "red" | "black";

export function bandOf(grade: number): Band {
  if (grade <= 1) return "green";
  if (grade <= 3) return "blue";
  if (grade <= 5) return "yellow";
  if (grade <= 7) return "red";
  return "black";
}
`

const seedIndex = `import { gradeLabel } from "./grades";
import { routes } from "./routes";

for (const r of routes) {
  console.log(` + "`${r.name.padEnd(16)} ${gradeLabel(r.grade).padEnd(4)} ${r.wall}`" + `);
}
`

const seedIndex2 = `import { gradeLabel } from "./grades";
import { routes, type Route } from "./routes";

const wall = process.argv[2] as Route["wall"] | undefined;
const shown = wall ? routes.filter((r) => r.wall === wall) : routes;

for (const r of shown) {
  console.log(` + "`${r.name.padEnd(16)} ${gradeLabel(r.grade).padEnd(4)} ${r.wall}`" + `);
}
`

const seedIndex3 = `import { bandOf, gradeLabel } from "./grades";
import { routes, type Route } from "./routes";

const wall = process.argv[2] as Route["wall"] | undefined;
const shown = wall ? routes.filter((r) => r.wall === wall) : routes;

for (const r of shown) {
  console.log(` + "`${r.name.padEnd(16)} ${gradeLabel(r.grade).padEnd(4)} ${bandOf(r.grade).padEnd(7)} ${r.wall}`" + `);
}
`

const seedBugTemplate = `---
name: Bug report
about: Something on the board is wrong
title: ""
labels: bug
---
**What's wrong**

**Steps**
1.

**Expected**
`

const seedRouteTemplate = `---
name: Route request
about: Ask the setters for a route
labels: enhancement
---
**Wall**

**Grade range**

**Why**
`

const seedPullTemplate = `## What changed

## How to check it

- [ ] Ran it against the current reset sheet
`
