package repos

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/notify"
)

// Cross-references and mentions. "#12" in an issue, a pull request (its
// description and comments) or a commit message links to issue or pull
// request 12 of the same repository and shows on its timeline; a closing
// keyword in a pull request's description closes the issue when the pull
// request merges into the default branch, and in a commit message when the
// commit reaches the default branch. <@uuid> mentions notify people who can
// view the repository.

// recordRefs stores the references written in a source (issue or pull
// request number n) and adds "referenced" to each target's timeline once.
// For a pull request's description (closingSource), the closing keywords
// are recomputed from the current text.
func (s *Service) recordRefs(ctx context.Context, repo *Repo, me *auth.Identity, sourceType string, n int, title, text string, closingSource bool) {
	refs := ParseRefs(text)
	key := strconv.Itoa(n)
	var actor *uuid.UUID
	if me != nil {
		actor = &me.UserID
	}
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if closingSource {
			if _, err := tx.Exec(ctx, `UPDATE repos_references SET closes = false WHERE repo_id = $1 AND source_type = $2 AND source_key = $3`,
				repo.ID, sourceType, key); err != nil {
				return err
			}
		}
		for _, r := range refs {
			if r.Number == n {
				continue
			}
			closes := closingSource && r.Closes
			if err := s.addRef(ctx, tx, repo, r.Number, sourceType, key, closes, actor, map[string]any{"source": sourceType, "number": n, "title": title}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		slog.WarnContext(ctx, "repos: recording references failed", "repo_id", repo.ID, "err", err)
	}
}

// addRef records one reference; the first time, the target's timeline
// shows it. Unknown numbers are skipped.
func (s *Service) addRef(ctx context.Context, tx pgx.Tx, repo *Repo, target int, sourceType, key string, closes bool, actor *uuid.UUID, data map[string]any) error {
	kind, id, err := itemByNumber(ctx, tx, repo.ID, target)
	if err != nil || id == uuid.Nil {
		return err
	}
	var inserted bool
	if err := tx.QueryRow(ctx, `
		INSERT INTO repos_references (repo_id, target_number, source_type, source_key, closes, actor_id) VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT ON CONSTRAINT repos_references_key DO UPDATE SET closes = repos_references.closes OR EXCLUDED.closes
		RETURNING (xmax = 0)`, repo.ID, target, sourceType, key, closes, actor).Scan(&inserted); err != nil {
		return err
	}
	if !inserted {
		return nil
	}
	data["closes"] = closes
	if kind == "pull" {
		return addEvent(ctx, tx, id, actor, "referenced", data)
	}
	return addIssueEvent(ctx, tx, id, actor, "referenced", data)
}

// itemByNumber finds issue or pull request #n ("" when there's none).
func itemByNumber(ctx context.Context, q db.Querier, repoID uuid.UUID, n int) (string, uuid.UUID, error) {
	var kind string
	var id uuid.UUID
	err := q.QueryRow(ctx, `SELECT 'issue', id FROM repos_issues WHERE repo_id = $1 AND number = $2
		UNION ALL SELECT 'pull', id FROM repos_pulls WHERE repo_id = $1 AND number = $2 LIMIT 1`, repoID, n).Scan(&kind, &id)
	if db.IsNoRows(err) {
		return "", uuid.Nil, nil
	}
	return kind, id, err
}

// closeIssuesForPull closes the issues a merged pull request's description
// closes ("Fixes #12"), when it merged into the default branch.
func (s *Service) closeIssuesForPull(ctx context.Context, repo *Repo, actor *auth.Identity, p *Pull) {
	if p.BaseBranch != repo.DefaultBranch {
		return
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT target_number FROM repos_references WHERE repo_id = $1 AND source_type = 'pull' AND source_key = $2 AND closes`,
		repo.ID, strconv.Itoa(p.Number))
	if err != nil {
		slog.WarnContext(ctx, "repos: reading closing references failed", "err", err)
		return
	}
	nums, err := pgx.CollectRows(rows, pgx.RowTo[int])
	if err != nil {
		return
	}
	for _, n := range nums {
		s.closeIssueBy(ctx, repo, actor, n, map[string]any{"reason": "completed", "pull": p.Number, "title": p.Title})
	}
}

// closeIssueBy closes an open issue as completed with a reason in its event.
func (s *Service) closeIssueBy(ctx context.Context, repo *Repo, actor *auth.Identity, number int, data map[string]any) {
	var actorID *uuid.UUID
	if actor != nil {
		actorID = &actor.UserID
	}
	var closed bool
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var id uuid.UUID
		err := tx.QueryRow(ctx, `UPDATE repos_issues SET state = 'closed', state_reason = 'completed', closed_by = $3, closed_at = now(), updated_at = now()
			WHERE repo_id = $1 AND number = $2 AND state = 'open' RETURNING id`, repo.ID, number, actorID).Scan(&id)
		if db.IsNoRows(err) {
			return nil
		}
		if err != nil {
			return err
		}
		closed = true
		return addIssueEvent(ctx, tx, id, actorID, "closed", data)
	})
	if err != nil {
		slog.WarnContext(ctx, "repos: closing an issue failed", "repo_id", repo.ID, "number", number, "err", err)
		return
	}
	if closed {
		s.issueChanged(ctx, repo.ID, number)
		if is, err := s.loadIssue(ctx, s.d.Pool, repo.ID, number); err == nil {
			s.emitWebhook(ctx, repo, actor, "issues", "closed", map[string]any{"issue": s.issueHookView(repo, is)})
		}
	}
}

// scanCommitRefs reads the messages of commits new on a branch: references
// are recorded, and closing keywords close issues once the commits are on
// the default branch.
func (s *Service) scanCommitRefs(ctx context.Context, repo *Repo, actor *auth.Identity, branch string, u refUpdate) {
	if u.deleting() {
		return
	}
	base := u.Old
	if u.creating() {
		// A new branch: only commits not on the default branch yet.
		def, _ := s.git.RefValue(ctx, repo.ID, "refs/heads/"+repo.DefaultBranch)
		if def == "" || def == u.New {
			return
		}
		base = def
	}
	commits, err := s.git.RangeCommits(ctx, repo.ID, base, u.New, 100)
	if err != nil {
		return
	}
	var actorID *uuid.UUID
	if actor != nil {
		actorID = &actor.UserID
	}
	onDefault := branch == repo.DefaultBranch
	for _, c := range commits {
		msg := c.Subject + "\n" + c.Body
		refs := ParseRefs(msg)
		if len(refs) == 0 {
			continue
		}
		for _, r := range refs {
			err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
				return s.addRef(ctx, tx, repo, r.Number, "commit", c.SHA, r.Closes && onDefault, actorID,
					map[string]any{"source": "commit", "sha": c.SHA, "message": c.Subject})
			})
			if err != nil {
				slog.WarnContext(ctx, "repos: commit reference failed", "err", err)
				continue
			}
			if r.Closes && onDefault {
				s.closeIssueBy(ctx, repo, actor, r.Number, map[string]any{"reason": "completed", "commit": c.SHA, "message": c.Subject})
			}
		}
	}
}

// ---- mentions ------------------------------------------------------------------

// mentionRefs resolves the <@uuid> tokens in text to names.
func (s *Service) mentionRefs(ctx context.Context, text string) ([]MentionRef, error) {
	ids := ParseMentions(text)
	out := []MentionRef{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT id, CASE WHEN status = 'deleted' THEN 'Deleted user' ELSE display_name END FROM users WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[MentionRef])
	if list == nil {
		list = out
	}
	return list, err
}

// plainBody writes mentions as @names (notification previews).
func (s *Service) plainBody(ctx context.Context, body string) string {
	refs, err := s.mentionRefs(ctx, body)
	if err != nil {
		return body
	}
	for _, m := range refs {
		body = strings.ReplaceAll(body, "<@"+m.ID.String()+">", "@"+m.DisplayName)
	}
	return mentionRe.ReplaceAllString(body, "@someone")
}

// newMentions keeps only the mention tokens in after that weren't in before
// (an edit notifies only people it adds).
func newMentions(before, after string) string {
	old := map[uuid.UUID]bool{}
	for _, id := range ParseMentions(before) {
		old[id] = true
	}
	var b strings.Builder
	for _, id := range ParseMentions(after) {
		if !old[id] {
			b.WriteString("<@" + id.String() + "> ")
		}
	}
	return b.String()
}

// notifyMentions tells the people mentioned in text who can view the
// repository, and returns who was told.
func (s *Service) notifyMentions(ctx context.Context, me *auth.Identity, repo *Repo, text, where, link string, skip map[uuid.UUID]bool) map[uuid.UUID]bool {
	told := map[uuid.UUID]bool{}
	ids := ParseMentions(text)
	if len(ids) > 50 {
		ids = ids[:50]
	}
	for _, id := range ids {
		if id == me.UserID || skip[id] {
			continue
		}
		lctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		lvl, err := access.LevelOf(lctx, s.d.Pool, id, RepoKind, repo.ID)
		cancel()
		if err != nil || lvl < access.View {
			continue
		}
		told[id] = true
		s.notifyIfVisible(ctx, repo, notify.Notification{
			UserID: id, Kind: KindMentioned,
			Title: fmt.Sprintf("%s mentioned you in %s", s.person(me).Name, where),
			Body:  repo.Name, Link: link,
		})
	}
	return told
}
