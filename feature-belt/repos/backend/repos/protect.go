package repos

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// Branch protection. A rule matches a branch by exact name or a glob
// (path.Match: "release/*" doesn't cross a slash). The default branch gets a
// rule when the repository is made: changes land through pull requests, no
// force-push, no deletion. Rules apply to git pushes (the pre-receive check),
// in-app commits, branch deletion and merges alike. The default branch can
// never be deleted, protected or not.

// Protection is one rule.
type Protection struct {
	ID                    uuid.UUID `json:"id"`
	Pattern               string    `json:"pattern"`
	RequirePullRequest    bool      `json:"require_pull_request"`
	RequiredApprovals     int       `json:"required_approvals"`
	DismissStaleApprovals bool      `json:"dismiss_stale_approvals"`
	AllowForcePush        bool      `json:"allow_force_push"`
	AllowDeletion         bool      `json:"allow_deletion"`
	CreatedAt             time.Time `json:"created_at"`
}

const protectionCols = `id, pattern, require_pull_request, required_approvals, dismiss_stale_approvals, allow_force_push, allow_deletion, created_at`

func (s *Service) protections(ctx context.Context, q db.Querier, repoID uuid.UUID) ([]Protection, error) {
	rows, err := q.Query(ctx, `SELECT `+protectionCols+` FROM repos_protections WHERE repo_id = $1 ORDER BY created_at, id`, repoID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Protection])
}

// protectionFor returns the rule that applies to branch: an exact match
// wins over a glob; among globs, the strictest values combine.
func protectionFor(rules []Protection, branch string) *Protection {
	var out *Protection
	for i := range rules {
		r := rules[i]
		if r.Pattern == branch {
			return &r
		}
		if ok, _ := path.Match(r.Pattern, branch); ok {
			if out == nil {
				out = &r
				continue
			}
			out.RequirePullRequest = out.RequirePullRequest || r.RequirePullRequest
			out.RequiredApprovals = max(out.RequiredApprovals, r.RequiredApprovals)
			out.DismissStaleApprovals = out.DismissStaleApprovals || r.DismissStaleApprovals
			out.AllowForcePush = out.AllowForcePush && r.AllowForcePush
			out.AllowDeletion = out.AllowDeletion && r.AllowDeletion
		}
	}
	return out
}

// refUpdate is one ref change: a push command or an in-app write.
type refUpdate struct {
	Old, New, Ref string
}

func (u refUpdate) branch() (string, bool) { return strings.CutPrefix(u.Ref, "refs/heads/") }
func (u refUpdate) deleting() bool         { return u.New == ZeroOID }
func (u refUpdate) creating() bool         { return u.Old == ZeroOID }

// checkPush decides whether a push's ref updates may land. env carries
// the push's quarantine so ancestry checks see the incoming objects. It
// returns the reasons for every refused ref (empty: allowed).
func (s *Service) checkPush(ctx context.Context, repo *Repo, updates []refUpdate, env []string) ([]string, error) {
	rules, err := s.protections(ctx, s.d.Pool, repo.ID)
	if err != nil {
		return nil, err
	}
	// The first push to an empty repository may create its default branch.
	empty, err := s.git.HasBranches(ctx, repo.ID)
	if err != nil {
		return nil, err
	}
	empty = !empty
	var denied []string
	for _, u := range updates {
		if b, ok := u.branch(); ok && empty && u.creating() && b == repo.DefaultBranch {
			continue
		}
		if msg, err := s.checkUpdate(ctx, repo, rules, u, env); err != nil {
			return nil, err
		} else if msg != "" {
			denied = append(denied, msg)
		}
	}
	return denied, nil
}

// checkUpdate returns why one update is refused, or "".
func (s *Service) checkUpdate(ctx context.Context, repo *Repo, rules []Protection, u refUpdate, env []string) (string, error) {
	branch, isBranch := u.branch()
	if !isBranch {
		if strings.HasPrefix(u.Ref, "refs/tags/") {
			return "", nil
		}
		return fmt.Sprintf("%s: only branches (refs/heads/) and tags (refs/tags/) can be pushed.", u.Ref), nil
	}
	if u.deleting() && branch == repo.DefaultBranch {
		return fmt.Sprintf("%s is the default branch and can't be deleted. Choose another default branch in the repository's settings first.", branch), nil
	}
	rule := protectionFor(rules, branch)
	if rule == nil {
		return "", nil
	}
	switch {
	case u.deleting():
		if !rule.AllowDeletion {
			return fmt.Sprintf("%s is protected and can't be deleted.", branch), nil
		}
		return "", nil
	case rule.RequirePullRequest:
		return fmt.Sprintf("%s is protected: changes land through pull requests. Push another branch and open one.", branch), nil
	case !u.creating() && !rule.AllowForcePush:
		ok, err := s.git.IsAncestor(ctx, repo.ID, u.Old, u.New, env...)
		if err != nil {
			return "", err
		}
		if !ok {
			return fmt.Sprintf("%s is protected: force-pushes aren't allowed. Pull first, then push again.", branch), nil
		}
	}
	return "", nil
}

// errProtected is an in-app write refused by a rule.
func errProtected(msg string) *httpx.Error {
	return httpx.NewError(http.StatusForbidden, "branch_protected", msg)
}

// ---- routes --------------------------------------------------------------------

type protectionReq struct {
	Pattern               *string `json:"pattern"`
	RequirePullRequest    *bool   `json:"require_pull_request"`
	RequiredApprovals     *int    `json:"required_approvals"`
	DismissStaleApprovals *bool   `json:"dismiss_stale_approvals"`
	AllowForcePush        *bool   `json:"allow_force_push"`
	AllowDeletion         *bool   `json:"allow_deletion"`
}

func (s *Service) listProtections(w http.ResponseWriter, r *http.Request) error {
	repo, err := s.repoFromRoute(r, levelView)
	if err != nil {
		return err
	}
	rules, err := s.protections(r.Context(), s.d.Pool, repo.ID)
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": rules})
	return nil
}

const maxProtections = 50

func (s *Service) createProtection(w http.ResponseWriter, r *http.Request) error {
	repo, err := s.repoFromRoute(r, levelManage)
	if err != nil {
		return err
	}
	var req protectionReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.Pattern == nil {
		return httpx.BadRequest("Name the branch or pattern to protect.")
	}
	pattern, err := s.cleanPattern(r.Context(), *req.Pattern)
	if err != nil {
		return err
	}
	p := Protection{Pattern: pattern, RequirePullRequest: true, DismissStaleApprovals: true}
	applyProtection(&p, req)
	if p.RequiredApprovals < 0 || p.RequiredApprovals > 10 {
		return httpx.BadRequest("Required approvals must be 0 to 10.")
	}
	var out Protection
	err = db.WithTx(r.Context(), s.d.Pool, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(r.Context(), `SELECT count(*) FROM repos_protections WHERE repo_id = $1`, repo.ID).Scan(&n); err != nil {
			return err
		}
		if n >= maxProtections {
			return httpx.Conflict("This repository has 50 protection rules. Remove one first.")
		}
		rows, err := tx.Query(r.Context(), `
			INSERT INTO repos_protections (repo_id, pattern, require_pull_request, required_approvals, dismiss_stale_approvals, allow_force_push, allow_deletion)
			VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING `+protectionCols,
			repo.ID, p.Pattern, p.RequirePullRequest, p.RequiredApprovals, p.DismissStaleApprovals, p.AllowForcePush, p.AllowDeletion)
		if err != nil {
			return err
		}
		out, err = pgx.CollectExactlyOneRow(rows, pgx.RowToStructByPos[Protection])
		if db.IsUniqueViolation(err, "repos_protections_pattern_key") {
			return httpx.Conflict("A rule for " + pattern + " already exists.")
		}
		return err
	})
	if err != nil {
		return err
	}
	s.repoChanged(r.Context(), repo.ID)
	httpx.JSON(w, http.StatusCreated, out)
	return nil
}

func (s *Service) updateProtection(w http.ResponseWriter, r *http.Request) error {
	repo, err := s.repoFromRoute(r, levelManage)
	if err != nil {
		return err
	}
	pid, err := httpx.URLParamUUID(r, "protectionID")
	if err != nil {
		return err
	}
	var req protectionReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	var out Protection
	err = db.WithTx(r.Context(), s.d.Pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `SELECT `+protectionCols+` FROM repos_protections WHERE id = $1 AND repo_id = $2 FOR UPDATE`, pid, repo.ID)
		if err != nil {
			return err
		}
		p, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByPos[Protection])
		if db.IsNoRows(err) {
			return httpx.NotFound("Rule not found.")
		}
		if err != nil {
			return err
		}
		if req.Pattern != nil {
			if p.Pattern, err = s.cleanPattern(r.Context(), *req.Pattern); err != nil {
				return err
			}
		}
		applyProtection(&p, req)
		if p.RequiredApprovals < 0 || p.RequiredApprovals > 10 {
			return httpx.BadRequest("Required approvals must be 0 to 10.")
		}
		rows, err = tx.Query(r.Context(), `
			UPDATE repos_protections SET pattern = $2, require_pull_request = $3, required_approvals = $4,
			       dismiss_stale_approvals = $5, allow_force_push = $6, allow_deletion = $7, updated_at = now()
			WHERE id = $1 RETURNING `+protectionCols,
			pid, p.Pattern, p.RequirePullRequest, p.RequiredApprovals, p.DismissStaleApprovals, p.AllowForcePush, p.AllowDeletion)
		if err != nil {
			return err
		}
		out, err = pgx.CollectExactlyOneRow(rows, pgx.RowToStructByPos[Protection])
		if db.IsUniqueViolation(err, "repos_protections_pattern_key") {
			return httpx.Conflict("A rule for " + p.Pattern + " already exists.")
		}
		return err
	})
	if err != nil {
		return err
	}
	s.repoChanged(r.Context(), repo.ID)
	httpx.OK(w, out)
	return nil
}

func (s *Service) deleteProtection(w http.ResponseWriter, r *http.Request) error {
	repo, err := s.repoFromRoute(r, levelManage)
	if err != nil {
		return err
	}
	pid, err := httpx.URLParamUUID(r, "protectionID")
	if err != nil {
		return err
	}
	tag, err := s.d.Pool.Exec(r.Context(), `DELETE FROM repos_protections WHERE id = $1 AND repo_id = $2`, pid, repo.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.NotFound("Rule not found.")
	}
	s.repoChanged(r.Context(), repo.ID)
	httpx.NoContent(w)
	return nil
}

func applyProtection(p *Protection, req protectionReq) {
	if req.RequirePullRequest != nil {
		p.RequirePullRequest = *req.RequirePullRequest
	}
	if req.RequiredApprovals != nil {
		p.RequiredApprovals = *req.RequiredApprovals
	}
	if req.DismissStaleApprovals != nil {
		p.DismissStaleApprovals = *req.DismissStaleApprovals
	}
	if req.AllowForcePush != nil {
		p.AllowForcePush = *req.AllowForcePush
	}
	if req.AllowDeletion != nil {
		p.AllowDeletion = *req.AllowDeletion
	}
}

// cleanPattern accepts a branch name or a glob over branch names.
func (s *Service) cleanPattern(ctx context.Context, raw string) (string, error) {
	p := strings.TrimSpace(raw)
	if p == "" || len(p) > 200 {
		return "", httpx.BadRequest("Name the branch or pattern to protect.")
	}
	if _, err := path.Match(p, ""); err != nil {
		return "", httpx.BadRequest("That pattern isn't valid. Use a branch name, or * and ? as wildcards (release/*).")
	}
	// Check the name with its wildcards swapped for a letter.
	probe := strings.NewReplacer("*", "x", "?", "x", "[", "x", "]", "x").Replace(p)
	if err := s.git.CheckRefFormat(ctx, "heads", probe); err != nil {
		return "", httpx.BadRequest("That isn't a valid branch name or pattern.")
	}
	return p, nil
}
