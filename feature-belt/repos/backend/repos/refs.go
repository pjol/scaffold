package repos

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/bg"
	"scaffoldapp/backend/internal/db"
)

// After any ref moves (a push, an in-app commit, a merge, a branch made or
// deleted): pull requests follow their branches, approvals reset when new
// commits arrive (if the base's rule says so), a pull request whose head
// branch was deleted closes, one whose commits reached its base by other
// means is marked merged, and open pages hear about it.

// afterPush keeps only the updates that actually landed.
func (s *Service) afterPush(ctx context.Context, repo *Repo, actor *auth.Identity, accepted []refUpdate) {
	var landed []refUpdate
	for _, u := range accepted {
		cur, err := s.git.RefValue(ctx, repo.ID, u.Ref)
		if err != nil {
			slog.WarnContext(ctx, "repos: reading a pushed ref failed", "repo_id", repo.ID, "err", err)
			continue
		}
		if (u.deleting() && cur == "") || cur == u.New {
			landed = append(landed, u)
		}
	}
	if len(landed) > 0 {
		s.afterRefUpdates(ctx, repo, actor, landed, "push")
	}
}

func (s *Service) afterRefUpdates(ctx context.Context, repo *Repo, actor *auth.Identity, updates []refUpdate, via string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
	defer cancel()
	if _, err := s.d.Pool.Exec(ctx, `UPDATE repos_repos SET pushed_at = now() WHERE id = $1`, repo.ID); err != nil {
		slog.WarnContext(ctx, "repos: pushed_at update failed", "repo_id", repo.ID, "err", err)
	}
	var actorID *uuid.UUID
	if actor != nil {
		actorID = &actor.UserID
	}
	rules, err := s.protections(ctx, s.d.Pool, repo.ID)
	if err != nil {
		slog.WarnContext(ctx, "repos: reading rules failed", "repo_id", repo.ID, "err", err)
	}
	changed := map[int]bool{}
	for _, u := range updates {
		branch, ok := u.branch()
		if !ok {
			continue
		}
		if err := s.followHead(ctx, repo, rules, branch, u, actorID, changed); err != nil {
			slog.WarnContext(ctx, "repos: updating pull requests failed", "repo_id", repo.ID, "branch", branch, "err", err)
		}
		if err := s.followBase(ctx, repo, branch, u, actorID, changed); err != nil {
			slog.WarnContext(ctx, "repos: updating pull requests failed", "repo_id", repo.ID, "branch", branch, "err", err)
		}
	}
	for n := range changed {
		s.pullChanged(ctx, repo.ID, n)
	}
	s.publish(ctx, repo.ID, EventRefsChanged, map[string]any{"via": via})
	if via != "push" { // receive-pack runs gc --auto itself
		s.gcSoon(ctx, repo.ID)
	}
	s.updateSizeSoon(ctx, repo.ID)
}

// openPull is the part of a pull request ref changes look at.
type openPull struct {
	ID         uuid.UUID
	Number     int
	HeadSHA    string
	BaseBranch string
	HeadBranch string
}

func (s *Service) openPullsBy(ctx context.Context, repoID uuid.UUID, col, branch string) ([]openPull, error) {
	rows, err := s.d.Pool.Query(ctx, `SELECT id, number, head_sha, base_branch, head_branch FROM repos_pulls
		WHERE repo_id = $1 AND state = 'open' AND `+col+` = $2`, repoID, branch)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[openPull])
}

// followHead moves pull requests whose head branch changed.
func (s *Service) followHead(ctx context.Context, repo *Repo, rules []Protection, branch string, u refUpdate, actor *uuid.UUID, changed map[int]bool) error {
	pulls, err := s.openPullsBy(ctx, repo.ID, "head_branch", branch)
	if err != nil {
		return err
	}
	for _, p := range pulls {
		if u.deleting() {
			err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
				if _, err := tx.Exec(ctx, `UPDATE repos_pulls SET state = 'closed', closed_by = $2, closed_at = now(), updated_at = now()
					WHERE id = $1 AND state = 'open'`, p.ID, actor); err != nil {
					return err
				}
				return addEvent(ctx, tx, p.ID, actor, "branch_deleted", map[string]any{"branch": branch})
			})
			if err != nil {
				return err
			}
			changed[p.Number] = true
			continue
		}
		if p.HeadSHA == u.New {
			continue
		}
		kind, data := "pushed", map[string]any{"before": p.HeadSHA, "after": u.New}
		if ok, err := s.git.IsAncestor(ctx, repo.ID, p.HeadSHA, u.New); err == nil && ok {
			if n, err := s.git.CountRange(ctx, repo.ID, p.HeadSHA, u.New); err == nil {
				data["commits"] = n
			}
		} else {
			kind = "force_pushed"
		}
		dismiss := false
		if rule := protectionFor(rules, p.BaseBranch); rule != nil && rule.DismissStaleApprovals {
			dismiss = true
		}
		err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `UPDATE repos_pulls SET head_sha = $2, updated_at = now() WHERE id = $1`, p.ID, u.New); err != nil {
				return err
			}
			if err := addEvent(ctx, tx, p.ID, actor, kind, data); err != nil {
				return err
			}
			if !dismiss {
				return nil
			}
			tag, err := tx.Exec(ctx, `UPDATE repos_reviews SET dismissed_at = now()
				WHERE pull_id = $1 AND state = 'approved' AND dismissed_at IS NULL`, p.ID)
			if err != nil || tag.RowsAffected() == 0 {
				return err
			}
			return addEvent(ctx, tx, p.ID, nil, "approvals_dismissed", map[string]any{"count": tag.RowsAffected()})
		})
		if err != nil {
			return err
		}
		if err := s.git.ForceRef(ctx, repo.ID, pullRef(p.Number), u.New); err != nil {
			slog.WarnContext(ctx, "repos: pull ref update failed", "repo_id", repo.ID, "number", p.Number, "err", err)
		}
		changed[p.Number] = true
	}
	return nil
}

// followBase marks pull requests merged when their head reached the base
// outside the app (a local merge pushed), and tells open pages that
// mergeability may have changed.
func (s *Service) followBase(ctx context.Context, repo *Repo, branch string, u refUpdate, actor *uuid.UUID, changed map[int]bool) error {
	pulls, err := s.openPullsBy(ctx, repo.ID, "base_branch", branch)
	if err != nil {
		return err
	}
	for _, p := range pulls {
		changed[p.Number] = true
		if u.deleting() {
			continue
		}
		ok, err := s.git.IsAncestor(ctx, repo.ID, p.HeadSHA, u.New)
		if err != nil || !ok {
			continue
		}
		err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
			tag, err := tx.Exec(ctx, `UPDATE repos_pulls SET state = 'merged', merged_by = $2, merged_at = now(), merge_commit_sha = $3,
				base_sha = $3, updated_at = now() WHERE id = $1 AND state = 'open'`, p.ID, actor, u.New)
			if err != nil || tag.RowsAffected() == 0 {
				return err
			}
			return addEvent(ctx, tx, p.ID, actor, "merged", map[string]any{"commit": u.New, "outside_app": true})
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func pullRef(number int) string { return "refs/pull/" + strconv.Itoa(number) + "/head" }

func addEvent(ctx context.Context, q db.Querier, pullID uuid.UUID, actor *uuid.UUID, kind string, data map[string]any) error {
	if data == nil {
		data = map[string]any{}
	}
	_, err := q.Exec(ctx, `INSERT INTO repos_pull_events (pull_id, actor_id, kind, data, created_at) VALUES ($1, $2, $3, $4, clock_timestamp())`, pullID, actor, kind, data)
	return err
}

// updateSizeSoon recomputes a repository's size on disk in the background.
func (s *Service) updateSizeSoon(ctx context.Context, repoID uuid.UUID) {
	bg.Go(ctx, "repos.update_size", func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		n, err := s.git.SizeBytes(ctx, repoID)
		if err != nil {
			return err
		}
		_, err = s.d.Pool.Exec(ctx, `UPDATE repos_repos SET size_bytes = $2 WHERE id = $1`, repoID, n)
		return err
	})
}
