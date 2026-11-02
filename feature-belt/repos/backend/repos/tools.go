package repos

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// Agent tools. Each runs as the caller through the same checks as the REST
// routes (repoFor → access.Require on repos.repository, capped by the
// person's AI agent scope when ctx is an agent call). Writes target the one
// repository they change and describe the exact change in Preview, so with
// scope "suggest" a commit becomes a diff the person accepts or rejects.
// Merging is a person's decision: there is no merge tool, and agents never
// touch credentials, settings or protection.

const (
	maxToolFileBytes = 256 << 10
	maxToolTreeRows  = 2000
	maxToolPatch     = 200 << 10
	toolWorkflow     = "Workflow for changing code: (1) read with repos_read_tree, repos_read_file and repos_search_code; (2) repos_commit_files to a new or existing feature branch (never the default or a protected branch), optionally opening a pull request in the same call; (3) or repos_open_pull_request afterwards. People review and merge pull requests themselves."
)

var repoIDProp = `"repo_id":{"type":"string","description":"The repository's id from repos_list: a UUID for this app's own, or a connected GitHub/GitLab id (github~…, gitlab~…)."}`

func schema(props, required string) json.RawMessage {
	req := ""
	if required != "" {
		req = `,"required":[` + required + `]`
	}
	return json.RawMessage(`{"type":"object","properties":{` + props + `}` + req + `,"additionalProperties":false}`)
}

// repoTarget reads repo_id; with no id it still names the kind (agents
// learns the feature's kinds by calling Target with {}).
func repoTarget(input json.RawMessage) (string, string) {
	var in struct {
		RepoID string `json:"repo_id"`
	}
	_ = json.Unmarshal(input, &in)
	if _, err := uuid.Parse(in.RepoID); err != nil {
		return RepoKind, ""
	}
	return RepoKind, in.RepoID
}

func decodeTool(input json.RawMessage, dst any) error {
	if len(bytes.TrimSpace(input)) == 0 {
		input = json.RawMessage(`{}`)
	}
	dec := json.NewDecoder(bytes.NewReader(input))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return httpx.BadRequest("Invalid arguments: " + err.Error())
	}
	return nil
}

// toolRepo loads repo_id at min level for the caller (agent-capped).
func (s *Service) toolRepo(ctx context.Context, caller *auth.Identity, id uuid.UUID, min access.Level) (*Repo, access.Level, error) {
	if s.git == nil || s.gitProblem != "" {
		return nil, access.None, httpx.NewError(503, "git_unavailable", "Repositories are unavailable on this server.")
	}
	if id == uuid.Nil {
		return nil, access.None, httpx.BadRequest("repo_id is required (find it with repos_list).")
	}
	return s.repoFor(ctx, caller.UserID, id, min)
}

// Tools lists the repos tools: the read tools also take connected
// repositories (withConnected), and each connected platform adds its own
// write tools (externalTools).
func (s *Service) Tools() []app.Tool {
	connected := map[string]func(context.Context, *auth.Identity, RepoSource, *remoteRepo, json.RawMessage) (any, error){
		"repos_read_tree": s.xToolTree, "repos_read_file": s.xToolFile, "repos_search_code": s.xToolSearch, "repos_log": s.xToolLog,
		"repos_diff": s.xToolDiff, "repos_list_pull_requests": s.xToolPulls, "repos_read_pull_request": s.xToolPull,
	}
	tools := s.appTools()
	for i, t := range tools {
		if x, ok := connected[t.Name]; ok {
			tools[i] = s.withConnected(t, x)
		}
	}
	if s.connections() == nil {
		return tools
	}
	return append(append(tools, s.externalTools(ProviderGitHub)...), s.externalTools(ProviderGitLab)...)
}

func (s *Service) appTools() []app.Tool {
	return []app.Tool{
		{
			Name: "repos_list",
			Description: "List git repositories the user can open, most recently changed first, with their default branch: this app's own and, from connected GitHub and GitLab accounts, theirs (rows carry source). " +
				"source narrows it: \"app\", \"connected\", \"github\", \"gitlab\" or a connection id. Filter by name with query. " + toolWorkflow,
			InputSchema: schema(`"query":{"type":"string"},"org_id":{"type":"string","format":"uuid"},"source":{"type":"string"},"limit":{"type":"integer","minimum":1,"maximum":50}`, ""),
			ReadOnly:    true,
			Target:      func(json.RawMessage) (string, string) { return RepoKind, "" },
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					Query  string     `json:"query"`
					OrgID  *uuid.UUID `json:"org_id"`
					Source string     `json:"source"`
					Limit  int        `json:"limit"`
				}
				if err := decodeTool(input, &in); err != nil {
					return nil, err
				}
				if in.Limit <= 0 || in.Limit > 50 {
					in.Limit = 25
				}
				type row struct {
					ID            string     `json:"id"`
					Name          string     `json:"name"`
					FullName      string     `json:"full_name,omitempty"`
					Description   string     `json:"description"`
					DefaultBranch string     `json:"default_branch"`
					OpenPulls     int        `json:"open_pulls,omitempty"`
					MyLevel       string     `json:"my_level"`
					PushedAt      *time.Time `json:"pushed_at"`
					Link          string     `json:"link"`
					Source        any        `json:"source,omitempty"`
				}
				out := []row{}
				if in.Source == "" || in.Source == "app" {
					if s.git != nil && s.gitProblem == "" {
						list, err := s.visibleRepos(ctx, caller.UserID, in.OrgID, in.Query, nil, in.Limit)
						if err != nil {
							return nil, err
						}
						for _, r := range list {
							out = append(out, row{ID: r.ID.String(), Name: r.Name, Description: r.Description, DefaultBranch: r.DefaultBranch, OpenPulls: r.OpenPulls,
								MyLevel: r.MyLevel, PushedAt: r.PushedAt, Link: "/repos/" + r.ID.String()})
						}
					}
				}
				if in.Source != "app" {
					list, err := s.connectedRepoRows(ctx, caller, in.Source, in.Query, in.Limit)
					if err != nil {
						return nil, err
					}
					for _, r := range list {
						out = append(out, row{ID: r.ID, Name: r.Name, FullName: r.FullName, Description: r.Description, DefaultBranch: r.DefaultBranch,
							MyLevel: r.MyLevel, PushedAt: r.PushedAt, Link: "/repos/" + r.ID, Source: r.Source})
					}
				}
				return map[string]any{"repositories": out}, nil
			},
		},
		{
			Name:        "repos_read_tree",
			Description: "List a folder of a repository at a branch, tag or commit (default: the default branch). recursive=true lists every file path under the folder (up to 2,000). Also returns the branches.",
			InputSchema: schema(repoIDProp+`,"ref":{"type":"string"},"path":{"type":"string","description":"Folder path; empty for the root."},"recursive":{"type":"boolean"}`, `"repo_id"`),
			ReadOnly:    true,
			Target:      repoTarget,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					RepoID    uuid.UUID `json:"repo_id"`
					Ref       string    `json:"ref"`
					Path      string    `json:"path"`
					Recursive bool      `json:"recursive"`
				}
				if err := decodeTool(input, &in); err != nil {
					return nil, err
				}
				repo, _, err := s.toolRepo(ctx, caller, in.RepoID, levelView)
				if err != nil {
					return nil, err
				}
				at, err := s.resolve(ctx, repo, in.Ref)
				if err != nil {
					return nil, err
				}
				dir, err := CleanPath(in.Path)
				if err != nil {
					return nil, httpx.BadRequest("That path isn't valid.")
				}
				refs, _ := s.git.Refs(ctx, repo.ID)
				branches := []string{}
				for _, r := range refs {
					if r.Kind == "branch" {
						branches = append(branches, r.Name)
					}
				}
				out := map[string]any{"ref": at.Ref, "commit": at.SHA, "path": dir, "default_branch": repo.DefaultBranch, "branches": branches}
				if in.Recursive {
					paths, truncated, err := s.git.ListPaths(ctx, repo.ID, at.SHA, maxPathsListed)
					if err != nil {
						return nil, err
					}
					var under []string
					for _, p := range paths {
						if dir == "" || strings.HasPrefix(p, dir+"/") {
							under = append(under, p)
							if len(under) == maxToolTreeRows {
								truncated = true
								break
							}
						}
					}
					out["files"], out["truncated"] = under, truncated
					return out, nil
				}
				entries, err := s.git.ListTree(ctx, repo.ID, at.SHA, dir)
				if err != nil {
					return nil, httpx.NotFound("There's no folder at " + dir + ".")
				}
				type row struct {
					Path string `json:"path"`
					Type string `json:"type"`
					Size int64  `json:"size,omitempty"`
				}
				rows := make([]row, 0, len(entries))
				for _, e := range entries {
					kind := map[string]string{"tree": "folder", "blob": "file", "symlink": "symlink", "commit": "submodule"}[e.Type]
					rows = append(rows, row{Path: e.Path, Type: kind, Size: max(e.Size, 0)})
				}
				out["entries"] = rows
				return out, nil
			},
		},
		{
			Name:        "repos_read_file",
			Description: "Read a text file from a repository at a branch, tag or commit, with line numbers. Use start_line and end_line for part of a long file. The commit returned is what to pass as expected_head when changing it.",
			InputSchema: schema(repoIDProp+`,"ref":{"type":"string"},"path":{"type":"string"},"start_line":{"type":"integer","minimum":1},"end_line":{"type":"integer","minimum":1}`, `"repo_id","path"`),
			ReadOnly:    true,
			Target:      repoTarget,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					RepoID    uuid.UUID `json:"repo_id"`
					Ref       string    `json:"ref"`
					Path      string    `json:"path"`
					StartLine int       `json:"start_line"`
					EndLine   int       `json:"end_line"`
				}
				if err := decodeTool(input, &in); err != nil {
					return nil, err
				}
				repo, _, err := s.toolRepo(ctx, caller, in.RepoID, levelView)
				if err != nil {
					return nil, err
				}
				at, err := s.resolve(ctx, repo, in.Ref)
				if err != nil {
					return nil, err
				}
				p, err := CleanPath(in.Path)
				if err != nil || p == "" {
					return nil, httpx.BadRequest("Name the file.")
				}
				e, err := s.git.Entry(ctx, repo.ID, at.SHA, p)
				if err != nil || e.Type != "blob" {
					return nil, httpx.NotFound("There's no file at " + p + " on " + at.Ref + ".")
				}
				data, cut, err := s.git.ReadBlob(ctx, repo.ID, e.OID, maxToolFileBytes)
				if err != nil {
					return nil, err
				}
				if isBinary(data) {
					return map[string]any{"path": p, "ref": at.Ref, "commit": at.SHA, "binary": true, "size": e.Size}, nil
				}
				lines := strings.SplitAfter(string(data), "\n")
				if n := len(lines); n > 0 && lines[n-1] == "" {
					lines = lines[:n-1]
				}
				start, end := max(in.StartLine, 1), len(lines)
				if in.EndLine > 0 && in.EndLine < end {
					end = in.EndLine
				}
				var b strings.Builder
				for i := start; i <= end && i <= len(lines); i++ {
					b.WriteString(strconv.Itoa(i))
					b.WriteString("\t")
					b.WriteString(strings.TrimSuffix(lines[i-1], "\n"))
					b.WriteString("\n")
				}
				return map[string]any{"path": p, "ref": at.Ref, "commit": at.SHA, "size": e.Size, "total_lines": len(lines),
					"truncated": cut, "start_line": start, "end_line": min(end, len(lines)), "content": b.String()}, nil
			},
		},
		{
			Name:        "repos_search_code",
			Description: "Search a repository's files for a fixed string (case-insensitive) at a branch, tag or commit. Returns up to 100 matching lines with paths and line numbers.",
			InputSchema: schema(repoIDProp+`,"query":{"type":"string","minLength":2,"maxLength":200},"ref":{"type":"string"}`, `"repo_id","query"`),
			ReadOnly:    true,
			Target:      repoTarget,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					RepoID uuid.UUID `json:"repo_id"`
					Query  string    `json:"query"`
					Ref    string    `json:"ref"`
				}
				if err := decodeTool(input, &in); err != nil {
					return nil, err
				}
				q := strings.TrimSpace(in.Query)
				if len([]rune(q)) < 2 || len([]rune(q)) > 200 {
					return nil, httpx.BadRequest("Search for 2 to 200 characters.")
				}
				repo, _, err := s.toolRepo(ctx, caller, in.RepoID, levelView)
				if err != nil {
					return nil, err
				}
				at, err := s.resolve(ctx, repo, in.Ref)
				if err != nil {
					return nil, err
				}
				matches, truncated, err := s.git.Grep(ctx, repo.ID, at.SHA, q, 100, 10)
				if err != nil {
					return nil, err
				}
				return map[string]any{"ref": at.Ref, "commit": at.SHA, "matches": matches, "truncated": truncated}, nil
			},
		},
		{
			Name:        "repos_log",
			Description: "List recent commits of a branch, tag or commit (default: the default branch), optionally only those touching a path. Up to 50.",
			InputSchema: schema(repoIDProp+`,"ref":{"type":"string"},"path":{"type":"string"},"limit":{"type":"integer","minimum":1,"maximum":50}`, `"repo_id"`),
			ReadOnly:    true,
			Target:      repoTarget,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					RepoID uuid.UUID `json:"repo_id"`
					Ref    string    `json:"ref"`
					Path   string    `json:"path"`
					Limit  int       `json:"limit"`
				}
				if err := decodeTool(input, &in); err != nil {
					return nil, err
				}
				repo, _, err := s.toolRepo(ctx, caller, in.RepoID, levelView)
				if err != nil {
					return nil, err
				}
				at, err := s.resolve(ctx, repo, in.Ref)
				if err != nil {
					return nil, err
				}
				p, err := CleanPath(in.Path)
				if err != nil {
					return nil, httpx.BadRequest("That path isn't valid.")
				}
				if in.Limit <= 0 || in.Limit > 50 {
					in.Limit = 20
				}
				list, err := s.git.Log(ctx, repo.ID, at.SHA, p, 0, in.Limit)
				if err != nil {
					return nil, err
				}
				return map[string]any{"ref": at.Ref, "commits": list}, nil
			},
		},
		{
			Name:        "repos_diff",
			Description: "Show a change as unified diffs: one commit (commit), or what merging head into base would bring (base...head, default base: the default branch). Long diffs are cut; pass path for one file.",
			InputSchema: schema(repoIDProp+`,"commit":{"type":"string"},"base":{"type":"string"},"head":{"type":"string"},"path":{"type":"string"}`, `"repo_id"`),
			ReadOnly:    true,
			Target:      repoTarget,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					RepoID uuid.UUID `json:"repo_id"`
					Commit string    `json:"commit"`
					Base   string    `json:"base"`
					Head   string    `json:"head"`
					Path   string    `json:"path"`
				}
				if err := decodeTool(input, &in); err != nil {
					return nil, err
				}
				repo, _, err := s.toolRepo(ctx, caller, in.RepoID, levelView)
				if err != nil {
					return nil, err
				}
				p, err := CleanPath(in.Path)
				if err != nil {
					return nil, httpx.BadRequest("That path isn't valid.")
				}
				var paths []string
				if p != "" {
					paths = []string{p}
				}
				if in.Commit != "" {
					at, err := s.resolve(ctx, repo, in.Commit)
					if err != nil {
						return nil, err
					}
					c, err := s.git.GetCommit(ctx, repo.ID, at.SHA)
					if err != nil {
						return nil, err
					}
					parent := ""
					if len(c.Parents) > 0 {
						parent = c.Parents[0]
					}
					d, err := s.git.DiffCommits(ctx, repo.ID, parent, c.SHA, defaultDiffLimits, paths...)
					if err != nil {
						return nil, err
					}
					return map[string]any{"commit": c, "files": patchText(d)}, nil
				}
				if in.Head == "" {
					return nil, httpx.BadRequest("Pass commit, or head (and optionally base).")
				}
				cmp, err := s.compareRefs(ctx, repo, in.Base, in.Head, p)
				if err != nil {
					return nil, err
				}
				return map[string]any{"base": cmp.Base, "head": cmp.Head, "ahead": cmp.Ahead, "behind": cmp.Behind,
					"mergeable": cmp.Mergeable, "conflicts": cmp.Conflicts, "commits": cmp.Commits, "files": patchText(cmp.Diff)}, nil
			},
		},
		{
			Name:        "repos_list_pull_requests",
			Description: "List a repository's pull requests: open (default), closed, merged or all, most recently updated first.",
			InputSchema: schema(repoIDProp+`,"state":{"type":"string","enum":["open","closed","merged","all"]}`, `"repo_id"`),
			ReadOnly:    true,
			Target:      repoTarget,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					RepoID uuid.UUID `json:"repo_id"`
					State  string    `json:"state"`
				}
				if err := decodeTool(input, &in); err != nil {
					return nil, err
				}
				repo, _, err := s.toolRepo(ctx, caller, in.RepoID, levelView)
				if err != nil {
					return nil, err
				}
				states := map[string][]string{"": {"open"}, "open": {"open"}, "closed": {"closed", "merged"}, "merged": {"merged"}, "all": {"open", "closed", "merged"}}[in.State]
				if states == nil {
					return nil, httpx.BadRequest("state is open, closed, merged or all.")
				}
				rows, err := s.d.Pool.Query(ctx, pullSelect+` WHERE p.repo_id = $1 AND p.state = ANY($2) ORDER BY p.updated_at DESC, p.id DESC LIMIT 50`, repo.ID, states)
				if err != nil {
					return nil, err
				}
				list, err := collectPulls(rows)
				if err != nil {
					return nil, err
				}
				type row struct {
					Number    int       `json:"number"`
					Title     string    `json:"title"`
					State     string    `json:"state"`
					Head      string    `json:"head"`
					Base      string    `json:"base"`
					Author    string    `json:"author"`
					Approvals int       `json:"approvals"`
					Comments  int       `json:"comments"`
					Updated   string    `json:"updated_at"`
					Link      string    `json:"link"`
					ID        uuid.UUID `json:"id"`
				}
				out := make([]row, 0, len(list))
				for _, p := range list {
					author := ""
					if p.Author != nil {
						author = p.Author.DisplayName
					}
					out = append(out, row{Number: p.Number, Title: p.Title, State: p.State, Head: p.HeadBranch, Base: p.BaseBranch, Author: author,
						Approvals: p.Approvals, Comments: p.CommentCount, Updated: p.UpdatedAt.Format("2006-01-02T15:04:05Z"), Link: s.pullLink(repo, p.Number), ID: p.ID})
				}
				return map[string]any{"pull_requests": out}, nil
			},
		},
		{
			Name:        "repos_read_pull_request",
			Description: "Read a pull request: description, state, whether it can merge and why not, reviews, the conversation and line comments, and the changed files (with diffs when include_diff is true).",
			InputSchema: schema(repoIDProp+`,"number":{"type":"integer","minimum":1},"include_diff":{"type":"boolean"}`, `"repo_id","number"`),
			ReadOnly:    true,
			Target:      repoTarget,
			Handler:     s.toolReadPull,
		},
		{
			Name: "repos_create_branch",
			Description: "Create a branch in a repository from another branch, tag or commit (default: the default branch). Names matching a protection rule are refused. " +
				"You usually don't need this: repos_commit_files creates its branch.",
			InputSchema: schema(repoIDProp+`,"name":{"type":"string"},"from":{"type":"string"}`, `"repo_id","name"`),
			Target:      repoTarget,
			Preview: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
				var in struct {
					RepoID uuid.UUID `json:"repo_id"`
					Name   string    `json:"name"`
					From   string    `json:"from"`
				}
				if err := decodeTool(input, &in); err != nil {
					return app.ToolPreview{}, err
				}
				repo, _, err := s.toolRepo(ctx, caller, in.RepoID, levelView)
				if err != nil {
					return app.ToolPreview{}, err
				}
				from := in.From
				if from == "" {
					from = repo.DefaultBranch
				}
				return app.ToolPreview{Summary: fmt.Sprintf("Create branch %s from %s in %s", in.Name, from, repo.Name), Link: "/repos/" + repo.ID.String() + "/branches"}, nil
			},
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					RepoID uuid.UUID `json:"repo_id"`
					Name   string    `json:"name"`
					From   string    `json:"from"`
				}
				if err := decodeTool(input, &in); err != nil {
					return nil, err
				}
				repo, _, err := s.toolRepo(ctx, caller, in.RepoID, levelEdit)
				if err != nil {
					return nil, err
				}
				return s.makeBranch(ctx, caller, repo, in.Name, in.From, true)
			},
		},
		{
			Name: "repos_commit_files",
			Description: "Commit file changes to a feature branch as one commit; never to the default or a protected branch. If the branch doesn't exist it is created from create_from (default: the default branch). " +
				"Each change: {op:\"put\", path, content} writes a whole file; {op:\"replace_lines\", path, start_line, end_line, content} replaces lines start..end of an existing file (end = start-1 inserts); {op:\"delete\", path}; {op:\"move\", from, path}. " +
				"Pass expected_head (the commit you read) so a branch that moved since is refused instead of overwritten. Set open_pull_request to open one into base (default: the default branch) in the same step. " +
				"When the person's AI agent scope is Suggest, this becomes a suggestion they review as a diff.",
			InputSchema: schema(repoIDProp+`,"branch":{"type":"string","description":"Feature branch to commit to, e.g. agent/fix-login."},`+
				`"create_from":{"type":"string"},"expected_head":{"type":"string"},"message":{"type":"string"},`+
				`"changes":{"type":"array","minItems":1,"maxItems":100,"items":{"type":"object","properties":{"op":{"type":"string","enum":["put","replace_lines","delete","move"]},"path":{"type":"string"},"from":{"type":"string"},"content":{"type":"string"},"start_line":{"type":"integer"},"end_line":{"type":"integer"}},"required":["op","path"],"additionalProperties":false}},`+
				`"open_pull_request":{"type":"object","properties":{"title":{"type":"string"},"body":{"type":"string"},"base":{"type":"string"}},"required":["title"],"additionalProperties":false}`,
				`"repo_id","branch","message","changes"`),
			Target:  repoTarget,
			Preview: s.previewCommit,
			Handler: s.toolCommit,
		},
		{
			Name:        "repos_open_pull_request",
			Description: "Open a pull request proposing a branch (head) for merging into base (default: the default branch), with a title and a description of what changed and why.",
			InputSchema: schema(repoIDProp+`,"head":{"type":"string"},"base":{"type":"string"},"title":{"type":"string"},"body":{"type":"string"}`, `"repo_id","head","title"`),
			Target:      repoTarget,
			Preview: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
				var in struct {
					RepoID uuid.UUID `json:"repo_id"`
					Head   string    `json:"head"`
					Base   string    `json:"base"`
					Title  string    `json:"title"`
					Body   string    `json:"body"`
				}
				if err := decodeTool(input, &in); err != nil {
					return app.ToolPreview{}, err
				}
				repo, _, err := s.toolRepo(ctx, caller, in.RepoID, levelView)
				if err != nil {
					return app.ToolPreview{}, err
				}
				base := in.Base
				if base == "" {
					base = repo.DefaultBranch
				}
				cmp, err := s.compareRefs(ctx, repo, base, in.Head, "")
				if err != nil {
					return app.ToolPreview{}, err
				}
				return app.ToolPreview{Summary: fmt.Sprintf("Open pull request “%s”: %s → %s in %s", in.Title, in.Head, base, repo.Name),
					After: in.Body + "\n\n" + diffStat(cmp.Diff), Link: "/repos/" + repo.ID.String() + "/compare?base=" + base + "&head=" + in.Head}, nil
			},
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					RepoID uuid.UUID `json:"repo_id"`
					Head   string    `json:"head"`
					Base   string    `json:"base"`
					Title  string    `json:"title"`
					Body   string    `json:"body"`
				}
				if err := decodeTool(input, &in); err != nil {
					return nil, err
				}
				repo, _, err := s.toolRepo(ctx, caller, in.RepoID, levelEdit)
				if err != nil {
					return nil, err
				}
				p, err := s.createPull(ctx, caller, repo, OpenPullInput{Title: in.Title, Body: in.Body, Head: in.Head, Base: in.Base})
				if err != nil {
					return nil, err
				}
				return map[string]any{"number": p.Number, "link": s.pullLink(repo, p.Number), "state": p.State}, nil
			},
		},
		{
			Name:        "repos_comment_on_pull_request",
			Description: "Comment on a pull request: in the conversation, or on one line of a changed file (path and line on the new side; side \"old\" for a removed line), or as a reply (reply_to a comment id).",
			InputSchema: schema(repoIDProp+`,"number":{"type":"integer","minimum":1},"body":{"type":"string"},"path":{"type":"string"},"line":{"type":"integer","minimum":1},"side":{"type":"string","enum":["new","old"]},"reply_to":{"type":"string","format":"uuid"}`, `"repo_id","number","body"`),
			Target:      repoTarget,
			Preview: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
				var in toolCommentIn
				if err := decodeTool(input, &in); err != nil {
					return app.ToolPreview{}, err
				}
				repo, _, err := s.toolRepo(ctx, caller, in.RepoID, levelView)
				if err != nil {
					return app.ToolPreview{}, err
				}
				p, err := s.loadPull(ctx, s.d.Pool, repo.ID, in.Number)
				if err != nil {
					return app.ToolPreview{}, err
				}
				where := ""
				if in.Path != "" {
					where = fmt.Sprintf(" on %s line %d", in.Path, in.Line)
				}
				return app.ToolPreview{Summary: fmt.Sprintf("Comment%s in #%d “%s”", where, p.Number, p.Title), After: in.Body, Link: s.pullLink(repo, p.Number)}, nil
			},
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in toolCommentIn
				if err := decodeTool(input, &in); err != nil {
					return nil, err
				}
				repo, _, err := s.toolRepo(ctx, caller, in.RepoID, levelComment)
				if err != nil {
					return nil, err
				}
				p, err := s.loadPull(ctx, s.d.Pool, repo.ID, in.Number)
				if err != nil {
					return nil, err
				}
				c, err := s.createComment(ctx, caller, repo, p, CommentInput{Body: in.Body, Path: in.Path, Line: in.Line, Side: in.Side, ReplyTo: in.ReplyTo}, nil)
				if err != nil {
					return nil, err
				}
				return map[string]any{"comment_id": c.ID, "link": s.pullLink(repo, p.Number)}, nil
			},
		},
	}
}

type toolCommentIn struct {
	RepoID  uuid.UUID  `json:"repo_id"`
	Number  int        `json:"number"`
	Body    string     `json:"body"`
	Path    string     `json:"path"`
	Line    int        `json:"line"`
	Side    string     `json:"side"`
	ReplyTo *uuid.UUID `json:"reply_to"`
}

// toolCommitIn is repos_commit_files' input.
type toolCommitIn struct {
	RepoID       uuid.UUID     `json:"repo_id"`
	Branch       string        `json:"branch"`
	CreateFrom   string        `json:"create_from"`
	ExpectedHead string        `json:"expected_head"`
	Message      string        `json:"message"`
	Changes      []ChangeInput `json:"changes"`
	OpenPull     *struct {
		Title string `json:"title"`
		Body  string `json:"body"`
		Base  string `json:"base"`
	} `json:"open_pull_request"`
}

// commitInput maps the tool's branch fields onto a CommitInput: an
// existing branch is committed to; a missing one is created from
// create_from by the same commit.
func (s *Service) commitInput(ctx context.Context, repo *Repo, in toolCommitIn) (CommitInput, string, error) {
	branch := strings.TrimSpace(in.Branch)
	if branch == "" {
		return CommitInput{}, "", httpx.BadRequest("Name the feature branch to commit to.")
	}
	head, err := s.git.RefValue(ctx, repo.ID, "refs/heads/"+branch)
	if err != nil {
		return CommitInput{}, "", err
	}
	if head != "" {
		return CommitInput{Branch: branch, ExpectedHead: in.ExpectedHead, Message: in.Message, Changes: in.Changes}, head, nil
	}
	from := strings.TrimSpace(in.CreateFrom)
	if from == "" {
		from = repo.DefaultBranch
	}
	start, err := s.git.RefValue(ctx, repo.ID, "refs/heads/"+from)
	if err != nil {
		return CommitInput{}, "", err
	}
	if start == "" || s.git.CheckRefFormat(ctx, "heads", from) != nil {
		return CommitInput{}, "", httpx.NotFound("create_from must name an existing branch; there's no branch named " + from + ".")
	}
	return CommitInput{Branch: from, NewBranch: branch, Message: in.Message, Changes: in.Changes}, start, nil
}

func (s *Service) toolCommit(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
	var in toolCommitIn
	if err := decodeTool(input, &in); err != nil {
		return nil, err
	}
	repo, _, err := s.toolRepo(ctx, caller, in.RepoID, levelEdit)
	if err != nil {
		return nil, err
	}
	ci, _, err := s.commitInput(ctx, repo, in)
	if err != nil {
		return nil, err
	}
	res, err := s.commit(ctx, caller, repo, ci, true)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"commit": res.Commit.SHA, "branch": res.Branch, "link": "/repos/" + repo.ID.String() + "/commits/" + res.Commit.SHA}
	if in.OpenPull != nil {
		base := in.OpenPull.Base
		if base == "" {
			base = repo.DefaultBranch
		}
		var existing int
		err := s.d.Pool.QueryRow(ctx, `SELECT number FROM repos_pulls WHERE repo_id = $1 AND head_branch = $2 AND base_branch = $3 AND state = 'open'`,
			repo.ID, res.Branch, base).Scan(&existing)
		if err == nil {
			out["pull_request"] = map[string]any{"number": existing, "link": s.pullLink(repo, existing), "already_open": true}
			return out, nil
		}
		p, err := s.createPull(ctx, caller, repo, OpenPullInput{Title: in.OpenPull.Title, Body: in.OpenPull.Body, Head: res.Branch, Base: base})
		if err != nil {
			out["pull_request_error"] = err.Error()
			return out, nil
		}
		out["pull_request"] = map[string]any{"number": p.Number, "link": s.pullLink(repo, p.Number)}
	}
	return out, nil
}

// previewCommit shows exactly what the commit would change: every touched
// file's text before and after, under a header per file.
func (s *Service) previewCommit(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
	var in toolCommitIn
	if err := decodeTool(input, &in); err != nil {
		return app.ToolPreview{}, err
	}
	repo, _, err := s.toolRepo(ctx, caller, in.RepoID, levelView)
	if err != nil {
		return app.ToolPreview{}, err
	}
	ci, head, err := s.commitInput(ctx, repo, in)
	if err != nil {
		return app.ToolPreview{}, err
	}
	if in.ExpectedHead != "" && head != in.ExpectedHead && ci.NewBranch == "" {
		return app.ToolPreview{}, httpx.NewError(409, "head_moved", in.Branch+" changed since the agent read it.")
	}
	changes, err := s.resolveChanges(ctx, repo, head, ci.Changes)
	if err != nil {
		return app.ToolPreview{}, err
	}
	var before, after strings.Builder
	added, changed, removed := 0, 0, 0
	read := func(p string) (string, bool) {
		e, err := s.git.Entry(ctx, repo.ID, head, p)
		if err != nil || e.Type != "blob" {
			return "", false
		}
		data, _, err := s.git.ReadBlob(ctx, repo.ID, e.OID, s.maxEditBytes)
		if err != nil {
			return "", false
		}
		if isBinary(data) {
			return "(binary file)\n", true
		}
		return string(data), true
	}
	for _, c := range changes {
		old, existed := read(c.Path)
		header := "=== " + c.Path + " ===\n"
		before.WriteString(header)
		after.WriteString(header)
		if existed {
			before.WriteString(old)
		} else {
			before.WriteString("(new file)\n")
		}
		switch {
		case c.Op == "delete":
			after.WriteString("(deleted)\n")
			removed++
		case c.OID != "":
			data, _, _ := s.git.ReadBlob(ctx, repo.ID, c.OID, s.maxEditBytes)
			after.Write(data)
			added++
		default:
			if isBinary(c.Content) {
				after.WriteString("(binary file)\n")
			} else {
				after.Write(c.Content)
			}
			if existed {
				changed++
			} else {
				added++
			}
		}
		before.WriteString("\n")
		after.WriteString("\n")
	}
	target := ci.Branch
	if ci.NewBranch != "" {
		target = "new branch " + ci.NewBranch + " (from " + ci.Branch + ")"
	}
	summary := fmt.Sprintf("Commit “%s” to %s in %s: %s", firstLine(strings.TrimSpace(in.Message)), target, repo.Name, countPhrase(added, changed, removed))
	if in.OpenPull != nil {
		// The pull request is text posted as the person: show it in full, or
		// that the one already open is reused (what the handler does).
		base := in.OpenPull.Base
		if base == "" {
			base = repo.DefaultBranch
		}
		head := ci.Branch
		if ci.NewBranch != "" {
			head = ci.NewBranch
		}
		header := "=== pull request " + head + " → " + base + " ===\n"
		before.WriteString(header)
		after.WriteString(header)
		var open int
		err := s.d.Pool.QueryRow(ctx, `SELECT number FROM repos_pulls WHERE repo_id = $1 AND head_branch = $2 AND base_branch = $3 AND state = 'open'`,
			repo.ID, head, base).Scan(&open)
		switch {
		case err == nil:
			fmt.Fprintf(&before, "#%d is open\n", open)
			fmt.Fprintf(&after, "#%d is open (the commit adds to it)\n", open)
			summary += fmt.Sprintf(", adding to pull request #%d", open)
		case db.IsNoRows(err):
			before.WriteString("(none)\n")
			after.WriteString(strings.TrimSpace(in.OpenPull.Title) + "\n\n" + strings.TrimSpace(in.OpenPull.Body) + "\n")
			summary += fmt.Sprintf(", and open a pull request into %s", base)
		default:
			return app.ToolPreview{}, err
		}
	}
	return app.ToolPreview{Summary: summary, Before: before.String(), After: after.String(), Link: "/repos/" + repo.ID.String()}, nil
}

func countPhrase(added, changed, removed int) string {
	var parts []string
	if changed > 0 {
		parts = append(parts, fmt.Sprintf("%d changed", changed))
	}
	if added > 0 {
		parts = append(parts, fmt.Sprintf("%d added", added))
	}
	if removed > 0 {
		parts = append(parts, fmt.Sprintf("%d deleted", removed))
	}
	return strings.Join(parts, ", ")
}

func (s *Service) toolReadPull(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
	var in struct {
		RepoID      uuid.UUID `json:"repo_id"`
		Number      int       `json:"number"`
		IncludeDiff bool      `json:"include_diff"`
	}
	if err := decodeTool(input, &in); err != nil {
		return nil, err
	}
	repo, lvl, err := s.toolRepo(ctx, caller, in.RepoID, levelView)
	if err != nil {
		return nil, err
	}
	p, err := s.loadPull(ctx, s.d.Pool, repo.ID, in.Number)
	if err != nil {
		return nil, err
	}
	if p, err = s.pullDetail(ctx, caller, p, lvl); err != nil {
		return nil, err
	}
	m, _, err := s.mergeability(ctx, caller, repo, p, lvl)
	if err != nil {
		return nil, err
	}
	conv, err := s.comments(ctx, s.d.Pool, p.ID, caller.UserID, `AND c.deleted_at IS NULL`)
	if err != nil {
		return nil, err
	}
	type note struct {
		ID     uuid.UUID `json:"id"`
		Author string    `json:"author"`
		Body   string    `json:"body"`
		Where  string    `json:"where,omitempty"`
		Reply  bool      `json:"reply,omitempty"`
	}
	notes := make([]note, 0, len(conv))
	for _, c := range conv {
		n := note{ID: c.ID, Body: c.Body, Reply: c.ReplyTo != nil}
		if c.Author != nil {
			n.Author = c.Author.DisplayName
		}
		if c.Path != nil {
			n.Where = fmt.Sprintf("%s:%d (%s)", *c.Path, *c.Line, *c.Side)
		}
		notes = append(notes, n)
	}
	_, head, mb, err := s.pullRange(ctx, repo, p)
	if err != nil {
		return nil, err
	}
	d, err := s.git.DiffCommits(ctx, repo.ID, mb, head, defaultDiffLimits)
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"number": p.Number, "title": p.Title, "body": p.Body, "state": p.State, "head": p.HeadBranch, "base": p.BaseBranch,
		"head_commit": p.HeadSHA, "author": p.Author, "reviewers": p.Reviewers, "merge": m, "comments": notes,
		"link": s.pullLink(repo, p.Number), "changed_files": diffStat(d),
	}
	if in.IncludeDiff {
		out["files"] = patchText(d)
	}
	return out, nil
}

// patchText renders a diff as unified text per file, within maxToolPatch.
func patchText(d *Diff) []map[string]any {
	out := []map[string]any{}
	if d == nil {
		return out
	}
	total := 0
	for _, f := range d.Files {
		row := map[string]any{"path": f.Path, "status": f.Status, "additions": f.Additions, "deletions": f.Deletions}
		if f.OldPath != f.Path {
			row["old_path"] = f.OldPath
		}
		switch {
		case f.Binary:
			row["binary"] = true
		case f.Truncated || total > maxToolPatch:
			row["patch_omitted"] = true
		default:
			var b strings.Builder
			for _, h := range f.Hunks {
				fmt.Fprintf(&b, "@@ -%d,%d +%d,%d @@ %s\n", h.OldStart, h.OldLines, h.NewStart, h.NewLines, h.Header)
				for _, l := range h.Lines {
					prefix := map[string]string{"context": " ", "add": "+", "del": "-", "note": "\\ "}[l.Type]
					b.WriteString(prefix + l.Text + "\n")
				}
			}
			total += b.Len()
			row["patch"] = b.String()
		}
		out = append(out, row)
	}
	return out
}

func diffStat(d *Diff) string {
	if d == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d files changed, +%d −%d\n", len(d.Files), d.Additions, d.Deletions)
	for _, f := range d.Files {
		fmt.Fprintf(&b, "%s %s (+%d −%d)\n", f.Status, f.Path, f.Additions, f.Deletions)
	}
	return b.String()
}
