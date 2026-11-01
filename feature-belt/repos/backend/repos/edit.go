package repos

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/bg"
	"scaffoldapp/backend/internal/httpx"
)

// Editing in the app: a set of file changes becomes one commit on a branch
// (or on a new branch), written without a worktree. The editor sends the
// commit it started from; if the branch moved since, the commit is refused
// with 409 head_moved and nothing is lost on either side.

const maxChanges = 500

// ChangeInput is one file change from a client or an agent.
type ChangeInput struct {
	Op       string  `json:"op"`       // put | delete | move | replace_lines
	Path     string  `json:"path"`     // the file (move: the new path)
	From     string  `json:"from"`     // move: the old path
	Content  *string `json:"content"`  // put: the whole file; replace_lines: the new lines
	Encoding string  `json:"encoding"` // put: utf-8 (default) or base64 (uploads)
	// replace_lines: replace lines start..end (1-based, inclusive) of an
	// existing text file; end = start - 1 inserts before start.
	StartLine  int   `json:"start_line"`
	EndLine    int   `json:"end_line"`
	Executable *bool `json:"executable"`
}

// CommitInput is a commit to make.
type CommitInput struct {
	Branch       string        `json:"branch"`
	ExpectedHead string        `json:"expected_head"` // the commit the edit started from ("" for an empty repository)
	NewBranch    string        `json:"new_branch"`    // commit to this new branch, started from Branch instead
	Message      string        `json:"message"`
	Changes      []ChangeInput `json:"changes"`
}

// CommitResult is what a commit made.
type CommitResult struct {
	Commit Commit `json:"commit"`
	Branch string `json:"branch"`
	// Compare suggests opening a pull request (new or unprotected branches
	// that differ from the default branch).
	SuggestPull bool `json:"suggest_pull"`
}

// commitChanges is POST /v1/repos/{repoID}/commits (edit level).
func (s *Service) commitChanges(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	repo, err := s.repoFromRoute(r, levelEdit)
	if err != nil {
		return err
	}
	var in CommitInput
	// Uploads travel base64 in JSON, so the body may exceed httpx.Decode's 1 MiB.
	body := http.MaxBytesReader(w, r.Body, s.maxCommit*4/3+(1<<20))
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return httpx.NewError(http.StatusRequestEntityTooLarge, "too_large", fmt.Sprintf("This commit is larger than %s. Commit fewer or smaller files at a time.", formatBytes(s.maxCommit)))
		}
		if errors.Is(err, io.EOF) {
			return httpx.BadRequest("Describe the commit.")
		}
		return httpx.BadRequest("Invalid JSON: " + err.Error())
	}
	res, err := s.commit(r.Context(), me, repo, in, false)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, res)
	return nil
}

// commit validates and writes a commit. agentOnly refuses protected
// branches outright (agent tools never commit to them).
func (s *Service) commit(ctx context.Context, me *auth.Identity, repo *Repo, in CommitInput, agentOnly bool) (*CommitResult, error) {
	in.Branch = strings.TrimSpace(in.Branch)
	in.NewBranch = strings.TrimSpace(in.NewBranch)
	if in.Branch == "" {
		in.Branch = repo.DefaultBranch
	}
	message := strings.TrimSpace(strings.ReplaceAll(in.Message, "\r\n", "\n"))
	if message == "" || utf8.RuneCountInString(message) > 10000 {
		return nil, httpx.BadRequest("Write a commit message (up to 10,000 characters).")
	}
	if len(in.Changes) == 0 || len(in.Changes) > maxChanges {
		return nil, httpx.BadRequest(fmt.Sprintf("A commit changes 1 to %d files.", maxChanges))
	}
	if in.ExpectedHead != "" && !IsOID(in.ExpectedHead) {
		return nil, httpx.BadRequest("expected_head must be a full commit id.")
	}
	target := in.Branch
	if in.NewBranch != "" {
		target = in.NewBranch
	}
	if err := s.git.CheckRefFormat(ctx, "heads", target); err != nil {
		return nil, httpx.BadRequest(target + " isn't a valid branch name.")
	}
	if agent, ok := auth.AgentFrom(ctx); ok {
		message += "\n\nMade by " + agentName(agent) + " (AI agent) for " + s.person(me).Name + "."
	}

	var res *CommitResult
	var update refUpdate
	err := s.withRepoLock(ctx, repo.ID, func(tx pgx.Tx) error {
		ref := "refs/heads/" + in.Branch
		head, err := s.git.RefValue(ctx, repo.ID, ref)
		if err != nil {
			return err
		}
		if head == "" && in.Branch != repo.DefaultBranch {
			return httpx.NotFound("There's no branch named " + in.Branch + ".")
		}
		if head == "" && in.NewBranch != "" {
			return httpx.BadRequest("Make the first commit on " + repo.DefaultBranch + " before branching.")
		}
		// A new branch starts where the edit started, even if the source moved since
		// (that's how an edit refused with head_moved still lands).
		if in.NewBranch != "" && in.ExpectedHead != "" && head != in.ExpectedHead {
			if ok, err := s.git.IsAncestor(ctx, repo.ID, in.ExpectedHead, head); err == nil && ok {
				head = in.ExpectedHead
			}
		}
		if in.ExpectedHead != "" && head != in.ExpectedHead {
			e := httpx.NewError(http.StatusConflict, "head_moved",
				in.Branch+" changed since you started editing. Commit your changes to a new branch, or copy them, reload and apply them again.")
			e.Details = map[string]any{"head": head}
			return e
		}
		old := head
		if in.NewBranch != "" {
			if v, err := s.git.RefValue(ctx, repo.ID, "refs/heads/"+in.NewBranch); err != nil {
				return err
			} else if v != "" {
				return httpx.Conflict("A branch named " + in.NewBranch + " already exists. Choose another name.")
			}
			old = ZeroOID
		} else if old == "" {
			old = ZeroOID
		}
		rules, err := s.protections(ctx, tx, repo.ID)
		if err != nil {
			return err
		}
		// The first commit of an empty repository creates its default branch.
		first := head == "" && in.NewBranch == "" && target == repo.DefaultBranch
		if rule := protectionFor(rules, target); rule != nil && ((rule.RequirePullRequest && !first) || agentOnly) {
			return errProtected(target + " is protected: changes land through pull requests. Commit to a new branch and open one.")
		}
		if agentOnly && target == repo.DefaultBranch {
			return errProtected(target + " is the default branch. Commit to a new branch and open a pull request.")
		}

		baseTree := ""
		if head != "" {
			if baseTree, err = s.git.TreeOf(ctx, repo.ID, head); err != nil {
				return err
			}
		}
		changes, err := s.resolveChanges(ctx, repo, head, in.Changes)
		if err != nil {
			return err
		}
		tree, err := s.git.ApplyChanges(ctx, repo.ID, baseTree, changes)
		if err != nil {
			return changeErr(err)
		}
		if tree == baseTree {
			return httpx.NewError(http.StatusUnprocessableEntity, "no_changes", "Nothing changed: the files already look like this.")
		}
		var parents []string
		if head != "" {
			parents = []string{head}
		}
		who := s.person(me)
		sha, err := s.git.CommitTree(ctx, repo.ID, tree, parents, message, who, who)
		if err != nil {
			return err
		}
		if err := s.git.UpdateRef(ctx, repo.ID, "refs/heads/"+target, sha, old, "commit in app: "+firstLine(message)); err != nil {
			if errors.Is(err, ErrRefMoved) {
				return httpx.NewError(http.StatusConflict, "head_moved", target+" changed while saving. Reload and try again.")
			}
			return err
		}
		update = refUpdate{Old: old, New: sha, Ref: "refs/heads/" + target}
		c, err := s.git.GetCommit(ctx, repo.ID, sha)
		if err != nil {
			return err
		}
		res = &CommitResult{Commit: c, Branch: target, SuggestPull: target != repo.DefaultBranch}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.afterRefUpdates(context.WithoutCancel(ctx), repo, me, []refUpdate{update}, "commit")
	return res, nil
}

// resolveChanges turns client changes into engine changes, reading the
// files that moves and line replacements need from head.
func (s *Service) resolveChanges(ctx context.Context, repo *Repo, head string, in []ChangeInput) ([]Change, error) {
	var out []Change
	var total int64
	seen := map[string]bool{}
	claim := func(p string) error {
		if seen[p] {
			return httpx.BadRequest(p + " is changed twice in one commit.")
		}
		seen[p] = true
		return nil
	}
	for _, c := range in {
		p, err := CleanPath(c.Path)
		if err != nil || p == "" {
			return nil, httpx.BadRequest(fmt.Sprintf("%q isn't a valid file path.", c.Path))
		}
		if err := claim(p); err != nil {
			return nil, err
		}
		mode := ""
		if c.Executable != nil {
			mode = map[bool]string{true: "100755", false: "100644"}[*c.Executable]
		}
		switch c.Op {
		case "put":
			if c.Content == nil {
				return nil, httpx.BadRequest("Send the content of " + p + ".")
			}
			data := []byte(*c.Content)
			if c.Encoding == "base64" {
				if data, err = base64.StdEncoding.DecodeString(*c.Content); err != nil {
					return nil, httpx.BadRequest(p + " isn't valid base64.")
				}
			} else if c.Encoding != "" && c.Encoding != "utf-8" {
				return nil, httpx.BadRequest("Encoding must be utf-8 or base64.")
			}
			total += int64(len(data))
			out = append(out, Change{Op: "put", Path: p, Content: data, Mode: mode})
		case "delete":
			out = append(out, Change{Op: "delete", Path: p})
		case "move":
			from, err := CleanPath(c.From)
			if err != nil || from == "" || head == "" {
				return nil, httpx.BadRequest("Name the file to move.")
			}
			if err := claim(from); err != nil {
				return nil, err
			}
			e, err := s.git.Entry(ctx, repo.ID, head, from)
			if errors.Is(err, ErrNotFound) || (err == nil && e.Type != "blob" && e.Type != "symlink") {
				return nil, httpx.NotFound("There's no file at " + from + ".")
			}
			if err != nil {
				return nil, err
			}
			if mode == "" {
				mode = e.Mode
			}
			out = append(out, Change{Op: "delete", Path: from}, Change{Op: "put", Path: p, OID: e.OID, Mode: mode})
		case "replace_lines":
			if head == "" || c.Content == nil {
				return nil, httpx.BadRequest("replace_lines needs an existing file and content.")
			}
			e, err := s.git.Entry(ctx, repo.ID, head, p)
			if errors.Is(err, ErrNotFound) || (err == nil && e.Type != "blob") {
				return nil, httpx.NotFound("There's no file at " + p + ".")
			}
			if err != nil {
				return nil, err
			}
			if e.Size > s.maxEditBytes {
				return nil, httpx.NewError(http.StatusUnprocessableEntity, "too_large", p+" is too large to edit here.")
			}
			data, _, err := s.git.ReadBlob(ctx, repo.ID, e.OID, s.maxEditBytes)
			if err != nil {
				return nil, err
			}
			if isBinary(data) {
				return nil, httpx.NewError(http.StatusUnprocessableEntity, "binary_file", p+" is a binary file.")
			}
			next, err := replaceLines(string(data), c.StartLine, c.EndLine, *c.Content)
			if err != nil {
				return nil, err
			}
			total += int64(len(next))
			out = append(out, Change{Op: "put", Path: p, Content: []byte(next), Mode: mode})
		default:
			return nil, httpx.BadRequest("A change's op is put, delete, move or replace_lines.")
		}
		if total > s.maxCommit {
			return nil, httpx.NewError(http.StatusRequestEntityTooLarge, "too_large", fmt.Sprintf("This commit is larger than %s.", formatBytes(s.maxCommit)))
		}
	}
	return out, nil
}

// replaceLines swaps lines start..end (1-based, inclusive) of text for
// repl. end = start-1 inserts before line start.
func replaceLines(text string, start, end int, repl string) (string, error) {
	lines := strings.SplitAfter(text, "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	if start < 1 || end < start-1 || end > len(lines) || start > len(lines)+1 {
		return "", httpx.BadRequest(fmt.Sprintf("Lines %d–%d are outside the file (it has %d lines).", start, end, len(lines)))
	}
	if repl != "" && !strings.HasSuffix(repl, "\n") && (end < len(lines) || strings.HasSuffix(text, "\n")) {
		repl += "\n"
	}
	return strings.Join(lines[:start-1], "") + repl + strings.Join(lines[end:], ""), nil
}

func changeErr(err error) error {
	var ce *ChangeError
	if errors.As(err, &ce) {
		return httpx.NewError(http.StatusUnprocessableEntity, "invalid_change", ce.Path+" "+ce.Msg+".")
	}
	return err
}

func firstLine(s string) string {
	l, _, _ := strings.Cut(s, "\n")
	return l
}

func agentName(a auth.Agent) string {
	if a.Client != "" {
		return a.Client
	}
	return "an AI agent"
}

// ---- branches ------------------------------------------------------------------

type createBranchReq struct {
	Name string `json:"name"`
	From string `json:"from"` // branch, tag or commit; default: the default branch
}

// createBranch is POST /v1/repos/{repoID}/branches (edit level).
func (s *Service) createBranch(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	repo, err := s.repoFromRoute(r, levelEdit)
	if err != nil {
		return err
	}
	var req createBranchReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	ref, err := s.makeBranch(r.Context(), me, repo, req.Name, req.From, false)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, ref)
	return nil
}

func (s *Service) makeBranch(ctx context.Context, me *auth.Identity, repo *Repo, name, from string, agentOnly bool) (*Ref, error) {
	name = strings.TrimSpace(name)
	if err := s.git.CheckRefFormat(ctx, "heads", name); err != nil {
		return nil, httpx.BadRequest("That isn't a valid branch name. Use letters, numbers, dashes and slashes (feature/login).")
	}
	at, err := s.resolve(ctx, repo, from)
	if err != nil {
		return nil, err
	}
	err = s.withRepoLock(ctx, repo.ID, func(tx pgx.Tx) error {
		rules, err := s.protections(ctx, tx, repo.ID)
		if err != nil {
			return err
		}
		if rule := protectionFor(rules, name); rule != nil && (rule.RequirePullRequest || agentOnly) {
			return errProtected(name + " matches a protection rule, so it can't be created directly.")
		}
		if err := s.git.UpdateRef(ctx, repo.ID, "refs/heads/"+name, at.SHA, ZeroOID, "branch created in app"); err != nil {
			if errors.Is(err, ErrRefMoved) {
				return httpx.Conflict("A branch named " + name + " already exists.")
			}
			var ge *GitError
			if errors.As(err, &ge) {
				return httpx.Conflict(name + " can't be created next to an existing branch with the same prefix.")
			}
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.afterRefUpdates(context.WithoutCancel(ctx), repo, me, []refUpdate{{Old: ZeroOID, New: at.SHA, Ref: "refs/heads/" + name}}, "branch")
	c, err := s.git.GetCommit(ctx, repo.ID, at.SHA)
	if err != nil {
		return nil, err
	}
	return &Ref{Name: name, Kind: "branch", SHA: at.SHA, Subject: c.Subject, Author: c.Author.Name, UpdatedAt: c.Committer.At}, nil
}

// deleteBranch is DELETE /v1/repos/{repoID}/branches?name= (edit level).
func (s *Service) deleteBranch(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	repo, err := s.repoFromRoute(r, levelEdit)
	if err != nil {
		return err
	}
	name := r.URL.Query().Get("name")
	if err := s.removeBranch(r.Context(), me, repo, name); err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

func (s *Service) removeBranch(ctx context.Context, me *auth.Identity, repo *Repo, name string) error {
	if name == repo.DefaultBranch {
		return errProtected(name + " is the default branch and can't be deleted.")
	}
	var update refUpdate
	err := s.withRepoLock(ctx, repo.ID, func(tx pgx.Tx) error {
		sha, err := s.git.RefValue(ctx, repo.ID, "refs/heads/"+name)
		if err != nil {
			return err
		}
		if sha == "" || s.git.CheckRefFormat(ctx, "heads", name) != nil {
			return httpx.NotFound("There's no branch named " + name + ".")
		}
		rules, err := s.protections(ctx, tx, repo.ID)
		if err != nil {
			return err
		}
		if rule := protectionFor(rules, name); rule != nil && !rule.AllowDeletion {
			return errProtected(name + " is protected and can't be deleted.")
		}
		if err := s.git.DeleteRef(ctx, repo.ID, "refs/heads/"+name, sha, "branch deleted in app"); err != nil {
			if errors.Is(err, ErrRefMoved) {
				return httpx.Conflict(name + " just changed. Reload and try again.")
			}
			return err
		}
		update = refUpdate{Old: sha, New: ZeroOID, Ref: "refs/heads/" + name}
		return nil
	})
	if err != nil {
		return err
	}
	s.afterRefUpdates(context.WithoutCancel(ctx), repo, me, []refUpdate{update}, "branch")
	return nil
}

// gcSoon packs loose objects in the background after in-app writes.
func (s *Service) gcSoon(ctx context.Context, repoID uuid.UUID) {
	bg.Go(ctx, "repos.gc", func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		if err := s.git.GC(ctx, repoID); err != nil {
			slog.WarnContext(ctx, "repos: gc failed", "repo_id", repoID, "err", err)
		}
		return nil
	})
}
