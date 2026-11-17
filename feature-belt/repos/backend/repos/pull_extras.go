package repos

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/bg"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// Pull request depth: drafts, labels and assignees, auto-merge, updating
// the branch, files viewed, applying suggested changes, code owners and
// rebase merges.

// ---- drafts --------------------------------------------------------------------

// setDraft is POST /pulls/{n}/ready (draft=false) or /pulls/{n}/draft: the
// author or edit level.
func (s *Service) setDraft(draft bool) func(http.ResponseWriter, *http.Request) error {
	return func(w http.ResponseWriter, r *http.Request) error {
		me := auth.MustFromContext(r.Context())
		repo, p, lvl, err := s.pullFromRoute(r, levelView)
		if err != nil {
			return err
		}
		if !canEditPull(me, p, lvl) {
			return httpx.Forbidden("Only the author and people who can edit the repository change this.")
		}
		if p.State != "open" {
			return httpx.Conflict("This pull request is " + p.State + ".")
		}
		if p.Draft == draft {
			return httpx.Conflict(map[bool]string{true: "This pull request is a draft already.", false: "This pull request is ready for review already."}[draft])
		}
		var requested []uuid.UUID
		err = db.WithTx(r.Context(), s.d.Pool, func(tx pgx.Tx) error {
			if _, err := tx.Exec(r.Context(), `UPDATE repos_pulls SET draft = $2, updated_at = now() WHERE id = $1`, p.ID, draft); err != nil {
				return err
			}
			if err := addEvent(r.Context(), tx, p.ID, &me.UserID, map[bool]string{true: "converted_to_draft", false: "ready_for_review"}[draft], nil); err != nil {
				return err
			}
			if draft {
				return nil
			}
			owners, err := s.codeOwnerReviewers(r.Context(), repo, p.BaseSHA, p.HeadSHA, me.UserID)
			if err != nil || len(owners) == 0 {
				return nil
			}
			var author uuid.UUID
			if p.Author != nil {
				author = p.Author.ID
			}
			requested, err = s.addReviewRequests(r.Context(), tx, repo, p.ID, me.UserID, author, owners)
			return err
		})
		if err != nil {
			return err
		}
		s.notifyReviewRequested(r.Context(), me, repo, p, requested)
		s.pullChanged(r.Context(), repo.ID, p.Number)
		p, err = s.loadPull(r.Context(), s.d.Pool, repo.ID, p.Number)
		if err != nil {
			return err
		}
		s.emitWebhook(r.Context(), repo, me, "pull_request", map[bool]string{true: "converted_to_draft", false: "ready_for_review"}[draft], map[string]any{"pull_request": s.pullHookView(repo, p)})
		p, err = s.pullDetail(r.Context(), me, p, lvl)
		if err != nil {
			return err
		}
		httpx.OK(w, p)
		return nil
	}
}

// ---- labels and assignees ------------------------------------------------------

func (s *Service) setPullLabels(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	repo, p, lvl, err := s.pullFromRoute(r, levelEdit)
	if err != nil {
		return err
	}
	var req idsReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if err := s.applyLabels(r.Context(), me, repo, "pull", p.ID, p.Number, req.IDs); err != nil {
		return err
	}
	return s.respondPull(w, r, me, repo, p.Number, lvl)
}

func (s *Service) setPullAssignees(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	repo, p, lvl, err := s.pullFromRoute(r, levelEdit)
	if err != nil {
		return err
	}
	var req idsReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if err := s.applyAssignees(r.Context(), me, repo, "pull", p.ID, p.Number, p.Title, req.IDs); err != nil {
		return err
	}
	return s.respondPull(w, r, me, repo, p.Number, lvl)
}

func (s *Service) respondPull(w http.ResponseWriter, r *http.Request, me *auth.Identity, repo *Repo, number int, lvl access.Level) error {
	p, err := s.loadPull(r.Context(), s.d.Pool, repo.ID, number)
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

// ---- auto-merge ----------------------------------------------------------------

// enableAutoMerge is PUT /pulls/{n}/auto-merge {method}: edit level, a
// person. The pull request merges as them once every requirement passes
// (approvals, checks, code owners, no conflicts).
func (s *Service) enableAutoMerge(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	if _, ok := auth.AgentFrom(r.Context()); ok {
		return errAgentForbidden
	}
	repo, p, lvl, err := s.pullFromRoute(r, levelEdit)
	if err != nil {
		return err
	}
	var req struct {
		Method string `json:"method"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if !repo.AllowAutoMerge {
		return httpx.Forbidden("Auto-merge is turned off in this repository's settings.")
	}
	if p.State != "open" {
		return httpx.Conflict("This pull request is " + p.State + ".")
	}
	m, _, err := s.mergeability(r.Context(), me, repo, p, lvl)
	if err != nil {
		return err
	}
	if !slicesContains(m.Methods, req.Method) {
		return httpx.BadRequest("This repository merges with: " + strings.Join(m.Methods, ", ") + ".")
	}
	if len(m.Conflicts) > 0 {
		return httpx.Conflict("Resolve the conflicts first; auto-merge waits for reviews and checks, not conflicts.")
	}
	err = db.WithTx(r.Context(), s.d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), `UPDATE repos_pulls SET auto_merge_method = $2, auto_merge_by = $3, auto_merge_at = now(), updated_at = now() WHERE id = $1`,
			p.ID, req.Method, me.UserID); err != nil {
			return err
		}
		return addEvent(r.Context(), tx, p.ID, &me.UserID, "auto_merge_enabled", map[string]any{"method": req.Method})
	})
	if err != nil {
		return err
	}
	s.pullChanged(r.Context(), repo.ID, p.Number)
	s.tryAutoMerge(context.WithoutCancel(r.Context()), repo, p.Number)
	return s.respondPull(w, r, me, repo, p.Number, lvl)
}

func (s *Service) disableAutoMerge(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	repo, p, lvl, err := s.pullFromRoute(r, levelView)
	if err != nil {
		return err
	}
	if !canEditPull(me, p, lvl) {
		return httpx.Forbidden("Only the author and people who can edit the repository change this.")
	}
	if p.AutoMerge == nil {
		return s.respondPull(w, r, me, repo, p.Number, lvl)
	}
	err = db.WithTx(r.Context(), s.d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), `UPDATE repos_pulls SET auto_merge_method = NULL, auto_merge_by = NULL, auto_merge_at = NULL WHERE id = $1`, p.ID); err != nil {
			return err
		}
		return addEvent(r.Context(), tx, p.ID, &me.UserID, "auto_merge_disabled", nil)
	})
	if err != nil {
		return err
	}
	s.pullChanged(r.Context(), repo.ID, p.Number)
	return s.respondPull(w, r, me, repo, p.Number, lvl)
}

// tryAutoMerge merges a pull request with auto-merge on when it can,
// as the person who turned it on (their own current level, never an
// agent's). Runs in the background; a failure leaves it waiting.
func (s *Service) tryAutoMerge(ctx context.Context, repo *Repo, number int) {
	bg.Go(ctx, "repos.auto_merge", func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()
		p, err := s.loadPull(ctx, s.d.Pool, repo.ID, number)
		if err != nil || p.State != "open" || p.AutoMerge == nil || p.AutoMerge.EnabledBy == nil {
			return nil
		}
		who, err := s.identityOf(ctx, p.AutoMerge.EnabledBy.ID)
		if err != nil || who == nil || who.Status != "active" {
			return nil
		}
		lvl, err := access.LevelOf(ctx, s.d.Pool, who.UserID, RepoKind, repo.ID)
		if err != nil || lvl < access.Edit {
			return nil
		}
		m, _, err := s.mergeability(ctx, who, repo, p, lvl)
		if err != nil || !m.Mergeable {
			return err
		}
		fresh, err := s.loadRepo(ctx, s.d.Pool, repo.ID)
		if err != nil {
			return nil
		}
		_, err = s.doMerge(ctx, who, fresh, p, lvl, mergeReq{Method: p.AutoMerge.Method, ExpectedHead: m.HeadSHA, DeleteBranch: fresh.DeleteBranchOnMerge})
		var he *httpx.Error
		if errors.As(err, &he) {
			slog.InfoContext(ctx, "repos: auto-merge waits", "repo_id", repo.ID, "number", number, "code", he.Code)
			return nil
		}
		return err
	})
}

// autoMergeAll retries every pull request with auto-merge on in a
// repository (after a review, a push or a status).
func (s *Service) autoMergeAll(ctx context.Context, repo *Repo) {
	rows, err := s.d.Pool.Query(ctx, `SELECT number FROM repos_pulls WHERE repo_id = $1 AND state = 'open' AND auto_merge_method IS NOT NULL LIMIT 20`, repo.ID)
	if err != nil {
		return
	}
	nums, _ := pgx.CollectRows(rows, pgx.RowTo[int])
	for _, n := range nums {
		s.tryAutoMerge(ctx, repo, n)
	}
}

// identityOf loads a person as an Identity (for work done on their behalf).
func (s *Service) identityOf(ctx context.Context, id uuid.UUID) (*auth.Identity, error) {
	who := &auth.Identity{UserID: id}
	var role *string
	err := s.d.Pool.QueryRow(ctx, `SELECT auth_subject, email, display_name, admin_role, status FROM users WHERE id = $1`, id).
		Scan(&who.Subject, &who.Email, &who.DisplayName, &role, &who.Status)
	if db.IsNoRows(err) {
		return nil, nil
	}
	if role != nil {
		who.AdminRole = *role
	}
	return who, err
}

// ---- update branch -------------------------------------------------------------

// updateBranch is POST /pulls/{n}/update-branch {expected_head}: merges the
// base into the head branch (a merge commit on the head), edit level, when
// the head isn't protected and the merge is clean.
func (s *Service) updateBranch(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	repo, p, lvl, err := s.pullFromRoute(r, levelEdit)
	if err != nil {
		return err
	}
	var req struct {
		ExpectedHead string `json:"expected_head"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	ctx := r.Context()
	var newHead string
	err = s.withRepoLock(ctx, repo.ID, func(tx pgx.Tx) error {
		m, res, err := s.mergeability(ctx, me, repo, p, lvl)
		if err != nil {
			return err
		}
		if !m.CanUpdateBranch {
			switch {
			case m.Behind == 0:
				return httpx.Conflict(p.HeadBranch + " already has every commit of " + p.BaseBranch + ".")
			case len(m.Conflicts) > 0:
				return httpx.Conflict("Updating would conflict in " + strings.Join(m.Conflicts, ", ") + ". Merge locally and push.")
			default:
				return errProtected(p.HeadBranch + " is protected, so it can't be updated from here.")
			}
		}
		if req.ExpectedHead != "" && req.ExpectedHead != m.HeadSHA {
			return httpx.NewError(http.StatusConflict, "head_moved", p.HeadBranch+" has new commits since you looked.")
		}
		who := s.person(me)
		sha, err := s.git.CommitTree(ctx, repo.ID, res.Tree, []string{m.HeadSHA, m.BaseSHA},
			fmt.Sprintf("Merge branch '%s' into %s", p.BaseBranch, p.HeadBranch), who, who)
		if err != nil {
			return err
		}
		if err := s.git.UpdateRef(ctx, repo.ID, "refs/heads/"+p.HeadBranch, sha, m.HeadSHA, "update branch"); err != nil {
			if errors.Is(err, ErrRefMoved) {
				return httpx.NewError(http.StatusConflict, "head_moved", p.HeadBranch+" just changed. Try again.")
			}
			return err
		}
		newHead = sha
		return addEvent(ctx, tx, p.ID, &me.UserID, "branch_updated", map[string]any{"base": p.BaseBranch, "commit": sha})
	})
	if err != nil {
		return err
	}
	s.afterRefUpdates(context.WithoutCancel(ctx), repo, me, []refUpdate{{Old: p.HeadSHA, New: newHead, Ref: "refs/heads/" + p.HeadBranch}}, "update_branch")
	return s.respondPull(w, r, me, repo, p.Number, lvl)
}

// ---- files viewed --------------------------------------------------------------

// viewedFiles is GET /pulls/{n}/viewed: the caller's files marked viewed
// that haven't changed since.
func (s *Service) viewedFiles(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	repo, p, _, err := s.pullFromRoute(r, levelView)
	if err != nil {
		return err
	}
	rows, err := s.d.Pool.Query(r.Context(), `SELECT path, blob_oid FROM repos_viewed_files WHERE pull_id = $1 AND user_id = $2`, p.ID, me.UserID)
	if err != nil {
		return err
	}
	type pb struct{ Path, OID string }
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[pb])
	if err != nil {
		return err
	}
	out := []string{}
	for _, v := range list {
		if s.blobAtHead(r.Context(), repo, p, v.Path) == v.OID {
			out = append(out, v.Path)
		}
	}
	sort.Strings(out)
	httpx.OK(w, map[string]any{"items": out})
	return nil
}

func (s *Service) blobAtHead(ctx context.Context, repo *Repo, p *Pull, path string) string {
	e, err := s.git.Entry(ctx, repo.ID, p.HeadSHA, path)
	if err != nil {
		return ""
	}
	return e.OID
}

// setViewed is PUT (viewed) or DELETE /pulls/{n}/viewed {path}.
func (s *Service) setViewed(viewed bool) func(http.ResponseWriter, *http.Request) error {
	return func(w http.ResponseWriter, r *http.Request) error {
		me := auth.MustFromContext(r.Context())
		repo, p, _, err := s.pullFromRoute(r, levelView)
		if err != nil {
			return err
		}
		var req struct {
			Path string `json:"path"`
		}
		if err := httpx.Decode(r, &req); err != nil {
			return err
		}
		clean, err := CleanPath(req.Path)
		if err != nil || clean == "" {
			return httpx.BadRequest("That file path isn't valid.")
		}
		if viewed {
			_, err = s.d.Pool.Exec(r.Context(), `INSERT INTO repos_viewed_files (pull_id, user_id, path, blob_oid) VALUES ($1, $2, $3, $4)
				ON CONFLICT (pull_id, user_id, path) DO UPDATE SET blob_oid = EXCLUDED.blob_oid, created_at = now()`,
				p.ID, me.UserID, clean, s.blobAtHead(r.Context(), repo, p, clean))
		} else {
			_, err = s.d.Pool.Exec(r.Context(), `DELETE FROM repos_viewed_files WHERE pull_id = $1 AND user_id = $2 AND path = $3`, p.ID, me.UserID, clean)
		}
		if err != nil {
			return err
		}
		httpx.NoContent(w)
		return nil
	}
}

// ---- suggested changes ---------------------------------------------------------

type applySuggestionsReq struct {
	CommentIDs   []uuid.UUID `json:"comment_ids"`
	Message      string      `json:"message"`
	ExpectedHead string      `json:"expected_head"`
}

// applySuggestions is POST /pulls/{n}/suggestions: commits the ```suggestion
// blocks of line comments to the head branch in one commit (edit level, the
// head branch must accept commits). A suggestion whose file changed since
// it was written is refused as outdated.
func (s *Service) applySuggestions(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	repo, p, lvl, err := s.pullFromRoute(r, levelEdit)
	if err != nil {
		return err
	}
	var req applySuggestionsReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	res, err := s.applySuggestionsAs(r.Context(), me, repo, p, req, false)
	if err != nil {
		return err
	}
	_ = lvl
	httpx.JSON(w, http.StatusCreated, res)
	return nil
}

func (s *Service) applySuggestionsAs(ctx context.Context, me *auth.Identity, repo *Repo, p *Pull, req applySuggestionsReq, agentOnly bool) (*CommitResult, error) {
	ids := uniqueIDs(req.CommentIDs)
	if len(ids) == 0 || len(ids) > 50 {
		return nil, httpx.BadRequest("Choose 1 to 50 suggestions to apply.")
	}
	if p.State != "open" {
		return nil, httpx.Conflict("This pull request is " + p.State + ".")
	}
	head, err := s.git.RefValue(ctx, repo.ID, "refs/heads/"+p.HeadBranch)
	if err != nil {
		return nil, err
	}
	if head == "" {
		return nil, httpx.Conflict("The branch " + p.HeadBranch + " doesn't exist anymore.")
	}
	if req.ExpectedHead != "" && req.ExpectedHead != head {
		return nil, httpx.NewError(http.StatusConflict, "head_moved", p.HeadBranch+" has new commits since you looked. Reload and try again.")
	}
	type sug struct {
		id         uuid.UUID
		path       string
		start, end int
		repl       string
		commit     string
		author     string
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT c.id, c.path, COALESCE(c.start_line, c.line), c.line, c.side, c.body, c.commit_sha, c.suggestion_applied_sha, COALESCE(u.display_name, '')
		FROM repos_comments c LEFT JOIN users u ON u.id = c.author_id LEFT JOIN repos_reviews rv ON rv.id = c.review_id
		WHERE c.pull_id = $1 AND c.id = ANY($2) AND c.deleted_at IS NULL AND (rv.state IS DISTINCT FROM 'pending' OR c.author_id = $3)`, p.ID, ids, me.UserID)
	if err != nil {
		return nil, err
	}
	var list []sug
	for rows.Next() {
		var x sug
		var path, side, commit, applied *string
		var start, end *int
		var body string
		if err := rows.Scan(&x.id, &path, &start, &end, &side, &body, &commit, &applied, &x.author); err != nil {
			rows.Close()
			return nil, err
		}
		repl, ok := ParseSuggestion(body)
		switch {
		case path == nil || end == nil:
			rows.Close()
			return nil, httpx.BadRequest("Only line comments carry suggestions.")
		case !ok:
			rows.Close()
			return nil, httpx.BadRequest("A chosen comment has no suggestion block.")
		case applied != nil:
			rows.Close()
			return nil, httpx.Conflict("A chosen suggestion was applied already.")
		case side != nil && *side != "new":
			rows.Close()
			return nil, httpx.BadRequest("Suggestions apply to the new side of the diff.")
		}
		x.path, x.start, x.end, x.repl, x.commit = *path, *start, *end, repl, deref(commit)
		list = append(list, x)
	}
	rows.Close()
	if len(list) != len(ids) {
		return nil, httpx.NotFound("A chosen comment doesn't exist anymore.")
	}
	// Group by file; apply bottom-up so line numbers hold; refuse overlaps.
	byPath := map[string][]sug{}
	for _, x := range list {
		if x.commit != head && s.blobAt(ctx, repo, x.commit, x.path) != s.blobAt(ctx, repo, head, x.path) {
			return nil, httpx.NewError(http.StatusConflict, "suggestion_outdated", x.path+" changed since the suggestion was written. Apply the change by hand.")
		}
		byPath[x.path] = append(byPath[x.path], x)
	}
	var changes []ChangeInput
	authors := map[string]bool{}
	for path, group := range byPath {
		sort.Slice(group, func(i, j int) bool { return group[i].start > group[j].start })
		for i := 1; i < len(group); i++ {
			if group[i].end >= group[i-1].start {
				return nil, httpx.Conflict("Two chosen suggestions change the same lines of " + path + ". Apply them one at a time.")
			}
		}
		e, err := s.git.Entry(ctx, repo.ID, head, path)
		if err != nil {
			return nil, httpx.NotFound(path + " isn't on " + p.HeadBranch + " anymore.")
		}
		data, _, err := s.git.ReadBlob(ctx, repo.ID, e.OID, s.maxEditBytes)
		if err != nil {
			return nil, err
		}
		if isBinary(data) {
			return nil, httpx.BadRequest(path + " isn't a text file.")
		}
		text := string(data)
		for _, x := range group {
			out, ok := ApplySuggestion(text, x.start, x.end, x.repl)
			if !ok {
				return nil, httpx.NewError(http.StatusConflict, "suggestion_outdated", "The lines of a suggestion on "+path+" aren't there anymore.")
			}
			text = out
			if x.author != "" {
				authors[x.author] = true
			}
		}
		content := text
		changes = append(changes, ChangeInput{Op: "put", Path: path, Content: &content})
	}
	msg := strings.TrimSpace(req.Message)
	if msg == "" {
		msg = "Apply suggestions from code review"
		if len(list) == 1 {
			msg = "Apply suggestion from code review"
		}
	}
	var names []string
	for n := range authors {
		names = append(names, n)
	}
	sort.Strings(names)
	if len(names) > 0 {
		msg += "\n\nSuggested by " + strings.Join(names, ", ") + "."
	}
	res, err := s.commit(ctx, me, repo, CommitInput{Branch: p.HeadBranch, ExpectedHead: head, Message: msg, Changes: changes}, agentOnly)
	if err != nil {
		return nil, err
	}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE repos_comments SET suggestion_applied_sha = $2, resolved_at = COALESCE(resolved_at, now()), resolved_by = COALESCE(resolved_by, $3)
			WHERE id = ANY($1)`, ids, res.Commit.SHA, me.UserID); err != nil {
			return err
		}
		return addEvent(ctx, tx, p.ID, &me.UserID, "suggestions_applied", map[string]any{"count": len(ids), "commit": res.Commit.SHA})
	})
	if err != nil {
		return nil, err
	}
	s.pullChanged(ctx, repo.ID, p.Number)
	return res, nil
}

func (s *Service) blobAt(ctx context.Context, repo *Repo, sha, path string) string {
	if sha == "" {
		return ""
	}
	e, err := s.git.Entry(ctx, repo.ID, sha, path)
	if err != nil {
		return ""
	}
	return e.OID
}

// ---- code owners ---------------------------------------------------------------

// codeOwnersAt reads CODEOWNERS from a commit (nil when there's none).
func (s *Service) codeOwnersAt(ctx context.Context, repo *Repo, sha string) []CodeOwnersRule {
	for _, p := range codeOwnersPaths {
		e, err := s.git.Entry(ctx, repo.ID, sha, p)
		if err != nil || e.Type != "blob" || e.Size > 256<<10 {
			continue
		}
		data, _, err := s.git.ReadBlob(ctx, repo.ID, e.OID, 256<<10)
		if err != nil {
			continue
		}
		return ParseCodeOwners(string(data))
	}
	return nil
}

// ownerSet maps CODEOWNERS owners to people: an email to the workspace
// member with it, @team or @org/team to an access team's members. Owners
// who can't view the repository are left out.
type ownerSet struct {
	users map[string][]uuid.UUID // owner as written → people
}

func (s *Service) resolveOwners(ctx context.Context, repo *Repo, owners []string) (*ownerSet, error) {
	set := &ownerSet{users: map[string][]uuid.UUID{}}
	for _, o := range owners {
		if _, done := set.users[o]; done {
			continue
		}
		var ids []uuid.UUID
		if strings.HasPrefix(o, "@") {
			handle := strings.TrimPrefix(o, "@")
			if i := strings.LastIndex(handle, "/"); i >= 0 {
				handle = handle[i+1:]
			}
			team, err := access.TeamByHandle(ctx, s.d.Pool, repo.OrgID, handle)
			if err != nil {
				return nil, err
			}
			if team != nil {
				if ids, err = access.TeamMemberIDs(ctx, s.d.Pool, team.ID); err != nil {
					return nil, err
				}
			}
		} else {
			rows, err := s.d.Pool.Query(ctx, `SELECT u.id FROM org_members m JOIN users u ON u.id = m.user_id
				WHERE m.org_id = $1 AND lower(u.email) = lower($2) AND u.status = 'active'`, repo.OrgID, o)
			if err != nil {
				return nil, err
			}
			if ids, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID]); err != nil {
				return nil, err
			}
		}
		var visible []uuid.UUID
		for _, id := range ids {
			lvl, err := access.LevelOf(context.Background(), s.d.Pool, id, RepoKind, repo.ID)
			if err == nil && lvl >= access.View {
				visible = append(visible, id)
			}
		}
		set.users[o] = visible
	}
	return set, nil
}

// changedPathsOwners lists, for each changed file between base and head
// (from their merge base), the owners CODEOWNERS on the base names.
func (s *Service) changedPathsOwners(ctx context.Context, repo *Repo, base, head string) (map[string][]string, error) {
	if base == "" || head == "" {
		return nil, nil
	}
	rules := s.codeOwnersAt(ctx, repo, base)
	if len(rules) == 0 {
		return nil, nil
	}
	mb, err := s.git.MergeBase(ctx, repo.ID, base, head)
	if err != nil || mb == "" {
		return nil, err
	}
	paths, err := s.git.ChangedPaths(ctx, repo.ID, mb, head, 2000)
	if err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for _, p := range paths {
		if owners := OwnersOf(rules, p); len(owners) > 0 {
			out[p] = owners
		}
	}
	return out, nil
}

// codeOwnerReviewers lists the people to ask for review: owners of the
// changed files (not the author), at most maxReviewRequests.
func (s *Service) codeOwnerReviewers(ctx context.Context, repo *Repo, base, head string, author uuid.UUID) ([]uuid.UUID, error) {
	byPath, err := s.changedPathsOwners(ctx, repo, base, head)
	if err != nil || len(byPath) == 0 {
		return nil, err
	}
	var owners []string
	for _, o := range byPath {
		owners = append(owners, o...)
	}
	set, err := s.resolveOwners(ctx, repo, owners)
	if err != nil {
		return nil, err
	}
	seen := map[uuid.UUID]bool{author: true}
	var out []uuid.UUID
	keys := make([]string, 0, len(set.users))
	for k := range set.users {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		for _, id := range set.users[k] {
			if !seen[id] && len(out) < maxReviewRequests {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	return out, nil
}

// codeOwnersWaiting lists the owners (as written) of changed files where
// nobody among the approvers owns the file yet.
func (s *Service) codeOwnersWaiting(ctx context.Context, repo *Repo, base, head string, approvers []uuid.UUID) ([]string, error) {
	byPath, err := s.changedPathsOwners(ctx, repo, base, head)
	if err != nil || len(byPath) == 0 {
		return []string{}, err
	}
	var owners []string
	for _, o := range byPath {
		owners = append(owners, o...)
	}
	set, err := s.resolveOwners(ctx, repo, owners)
	if err != nil {
		return nil, err
	}
	approved := map[uuid.UUID]bool{}
	for _, id := range approvers {
		approved[id] = true
	}
	waiting := map[string]bool{}
	for _, list := range byPath {
		ok := false
		for _, o := range list {
			for _, id := range set.users[o] {
				if approved[id] {
					ok = true
				}
			}
		}
		if !ok {
			for _, o := range list {
				waiting[o] = true
			}
		}
	}
	out := []string{}
	for o := range waiting {
		out = append(out, o)
	}
	sort.Strings(out)
	return out, nil
}

// ---- rebase --------------------------------------------------------------------

const maxRebaseCommits = 250

// rebaseBlocked says why the head's commits can't be replayed onto base:
// merge commits, too many commits, or a step that conflicts.
func (s *Service) rebaseBlocked(ctx context.Context, repo *Repo, base, head string) (string, error) {
	n, err := s.git.CountRange(ctx, repo.ID, base, head)
	if err != nil {
		return "", err
	}
	if n > maxRebaseCommits {
		return fmt.Sprintf("Rebasing replays up to %d commits; this branch has %d. Squash or merge instead.", maxRebaseCommits, n), nil
	}
	commits, err := s.git.RangeCommits(ctx, repo.ID, base, head, maxRebaseCommits)
	if err != nil {
		return "", err
	}
	for _, c := range commits {
		if len(c.Parents) > 1 {
			return "The branch has merge commits, which rebasing would drop. Squash or merge instead.", nil
		}
		res, err := s.git.MergeTrees(ctx, repo.ID, base, c.SHA)
		if err != nil {
			if errors.Is(err, errUnrelated) {
				return "The branches have no history in common.", nil
			}
			return "", err
		}
		if !res.Clean {
			return "Commit " + c.SHA[:7] + " (“" + truncateUTF8(c.Subject, 60) + "”) conflicts when replayed. Squash or merge instead.", nil
		}
	}
	return "", nil
}

// rebaseOnto replays head's commits on top of base, oldest first, each
// with its author and message and the merger as committer, and returns the
// new tip. Each replayed tree is base merged with that commit (git 2.38
// merge-tree finds their merge base itself).
func (s *Service) rebaseOnto(ctx context.Context, repo *Repo, base, head string, committer Person) (string, error) {
	commits, err := s.git.RangeCommits(ctx, repo.ID, base, head, maxRebaseCommits)
	if err != nil {
		return "", err
	}
	tip := base
	for _, c := range commits {
		res, err := s.git.MergeTrees(ctx, repo.ID, base, c.SHA)
		if err != nil {
			return "", err
		}
		if !res.Clean {
			return "", httpx.NewError(http.StatusConflict, "not_mergeable", "Commit "+c.SHA[:7]+" conflicts when replayed. Squash or merge instead.")
		}
		author := Person{Name: c.Author.Name, Email: c.Author.Email, At: c.Author.At}
		msg := c.Subject
		if c.Body != "" {
			msg += "\n\n" + c.Body
		}
		if tip, err = s.git.CommitTree(ctx, repo.ID, res.Tree, []string{tip}, msg, author, committer); err != nil {
			return "", err
		}
	}
	return tip, nil
}
