package repos

// The read seam for link cards: what a viewer may see of a pull request or
// an issue without opening it, for other features' previews (channels'
// unfurlers) through the app's stitching.

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
)

// What a LinkedIssue is.
const (
	IssueKindPull  = "pull"
	IssueKindIssue = "issue"
)

// LinkedIssue is #Number of a repository as one viewer may see it from a
// link: a pull request or an issue. They share numbers per repository (as
// on GitHub), so a link to either path finds whichever #Number is.
type LinkedIssue struct {
	RepoID      uuid.UUID    `json:"repo_id"`
	OrgID       uuid.UUID    `json:"org_id"`
	Number      int          `json:"number"`
	Kind        string       `json:"kind"` // pull | issue
	Title       string       `json:"title"`
	State       string       `json:"state"`        // pull: open | merged | closed; issue: open | closed
	Draft       bool         `json:"draft"`        // a pull request marked draft
	StateReason string       `json:"state_reason"` // a closed issue: completed | not_planned
	RepoName    string       `json:"repo_name"`
	Context     string       `json:"context"` // org · repository, as search shows it
	Path        string       `json:"path"`    // /repos/<id>/pulls/<n> or /repos/<id>/issues/<n>, by what it is
	Level       access.Level `json:"level"`   // the viewer's level on the repository, agent-capped
	UpdatedAt   time.Time    `json:"updated_at"`
}

// ErrIssueNotFound is IssueRef's answer when the repository or the number
// doesn't exist, the repository is in the trash, or the viewer can't open
// it; they look the same.
var ErrIssueNotFound = errIssueNotFound

// IssueRef reads #number of a repository as viewer with the check of GET
// /repos/{repoID}/pulls/{number} and /issues/{number}: access_level on the
// repository (repos.repository, inherited from its drive folder), capped by
// the viewer's AI agent scope when ctx is an agent call (access.ViaAgent),
// and refusing repositories in the trash. Only the app's own repositories:
// connected ones (github~…, gitlab~…) aren't uuids and never get here.
// Nothing is recorded.
func IssueRef(ctx context.Context, q db.Querier, viewer *auth.Identity, repoID uuid.UUID, number int) (*LinkedIssue, error) {
	if viewer == nil || viewer.UserID == uuid.Nil || repoID == uuid.Nil || number < 1 {
		return nil, ErrIssueNotFound
	}
	lvl, err := access.LevelOf(ctx, q, viewer.UserID, RepoKind, repoID)
	if err != nil {
		return nil, err
	}
	if lvl < levelView {
		return nil, ErrIssueNotFound
	}
	out := &LinkedIssue{RepoID: repoID, Number: number, Level: lvl}
	var orgName string
	var trashed bool
	err = q.QueryRow(ctx, `
		SELECT r.org_id, i.name, o.name, i.trashed_at IS NOT NULL
		FROM repos_repos r JOIN files_items i ON i.id = r.id JOIN orgs o ON o.id = r.org_id
		WHERE r.id = $1`, repoID).Scan(&out.OrgID, &out.RepoName, &orgName, &trashed)
	if db.IsNoRows(err) || (err == nil && trashed) {
		return nil, ErrIssueNotFound
	}
	if err != nil {
		return nil, err
	}
	out.Context = app.SearchContext(orgName, out.RepoName)
	err = q.QueryRow(ctx, `SELECT title, state, draft, updated_at FROM repos_pulls WHERE repo_id = $1 AND number = $2`, repoID, number).
		Scan(&out.Title, &out.State, &out.Draft, &out.UpdatedAt)
	if err == nil {
		out.Kind, out.Path = IssueKindPull, fmt.Sprintf("/repos/%s/pulls/%d", repoID, number)
		return out, nil
	}
	if !db.IsNoRows(err) {
		return nil, err
	}
	err = q.QueryRow(ctx, `SELECT title, state, COALESCE(state_reason, ''), updated_at FROM repos_issues WHERE repo_id = $1 AND number = $2`, repoID, number).
		Scan(&out.Title, &out.State, &out.StateReason, &out.UpdatedAt)
	if db.IsNoRows(err) {
		return nil, ErrIssueNotFound
	}
	if err != nil {
		return nil, err
	}
	out.Kind, out.Path = IssueKindIssue, fmt.Sprintf("/repos/%s/issues/%d", repoID, number)
	return out, nil
}

// IssueFromPath reads the repository and number from an app path a pull
// request or issue link opens ("/repos/<id>/pulls/<n>",
// "/repos/<id>/issues/<n>", with or without a trailing slash). Other
// repository screens and connected repositories answer false.
func IssueFromPath(path string) (uuid.UUID, int, bool) {
	rest, ok := strings.CutPrefix(path, "/repos/")
	if !ok {
		return uuid.Nil, 0, false
	}
	parts := strings.Split(strings.TrimSuffix(rest, "/"), "/")
	if len(parts) != 3 || (parts[1] != "pulls" && parts[1] != "issues") {
		return uuid.Nil, 0, false
	}
	id, err := uuid.Parse(parts[0])
	if err != nil {
		return uuid.Nil, 0, false
	}
	n, err := strconv.Atoi(parts[2])
	if err != nil || n < 1 || strconv.Itoa(n) != parts[2] {
		return uuid.Nil, 0, false
	}
	return id, n, true
}
