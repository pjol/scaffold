package repos

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
)

// Agent tools on connected repositories. Reads are the repos_* read tools
// themselves: give them a connected repo_id (from repos_list) and they read
// the platform as the person, filtered by repos' agent scope and the
// connection's (both, at read). Changes on a platform are separate tools,
// one set per platform (Source "GitHub" / "GitLab", so assistants treat
// output as outside content and ask before each call), with Target kind
// repos.external and id "<connection id>:<owner/repo>", an exact Preview,
// and the connection's scope checked in Preview (suggest) and Handler
// (edit). No merge tool: merging stays a person's decision.

// toolRepoID reads repo_id without decoding the rest.
func toolRepoID(input json.RawMessage) string {
	var in struct {
		RepoID string `json:"repo_id"`
	}
	_ = json.Unmarshal(input, &in)
	return strings.TrimSpace(in.RepoID)
}

// withConnected lets a read tool take a connected repo_id: the target
// becomes the connected repository and x answers instead of the engine.
func (s *Service) withConnected(t app.Tool, x func(ctx context.Context, caller *auth.Identity, src RepoSource, rr *remoteRepo, input json.RawMessage) (any, error)) app.Tool {
	local, localTarget := t.Handler, t.Target
	t.Target = func(input json.RawMessage) (string, string) {
		if id, ok := parseExtID(toolRepoID(input)); ok {
			return ExternalKind, id.target()
		}
		if localTarget == nil {
			return RepoKind, ""
		}
		return localTarget(input)
	}
	t.Handler = func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
		raw := toolRepoID(input)
		if !isExternalID(raw) {
			return local(ctx, caller, input)
		}
		id, ok := parseExtID(raw)
		if !ok {
			return nil, httpx.BadRequest("That repo_id isn't valid. Use an id from repos_list.")
		}
		src, rr, err := s.openRemote(ctx, caller.UserID, id, false)
		if err != nil {
			return nil, err
		}
		out, err := x(ctx, caller, src, rr, input)
		return out, s.remoteErr(err, rr)
	}
	return t
}

// decodeLoose decodes a connected tool's input (repo_id is a string there).
func decodeLoose(input json.RawMessage, dst any) error {
	if err := json.Unmarshal(input, dst); err != nil {
		return httpx.BadRequest("Invalid arguments: " + err.Error())
	}
	return nil
}

func (s *Service) xToolTree(ctx context.Context, _ *auth.Identity, src RepoSource, rr *remoteRepo, input json.RawMessage) (any, error) {
	var in struct {
		Ref       string `json:"ref"`
		Path      string `json:"path"`
		Recursive bool   `json:"recursive"`
	}
	if err := decodeLoose(input, &in); err != nil {
		return nil, err
	}
	at, err := src.Resolve(ctx, in.Ref)
	if err != nil {
		return nil, err
	}
	dir, err := CleanPath(in.Path)
	if err != nil {
		return nil, httpx.BadRequest("That path isn't valid.")
	}
	refs, def, _ := src.Refs(ctx, false)
	branches := []string{}
	for _, r := range refs {
		if r.Kind == "branch" {
			branches = append(branches, r.Name)
		}
	}
	out := map[string]any{"ref": at.Ref, "commit": at.SHA, "path": dir, "default_branch": def, "branches": branches, "source": rr.source("")}
	if in.Recursive {
		paths, truncated, err := src.Paths(ctx, at)
		if err != nil {
			return nil, err
		}
		under := []string{}
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
	entries, err := src.Tree(ctx, at, dir)
	if err != nil {
		return nil, err
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
}

func (s *Service) xToolFile(ctx context.Context, _ *auth.Identity, src RepoSource, rr *remoteRepo, input json.RawMessage) (any, error) {
	var in struct {
		Ref       string `json:"ref"`
		Path      string `json:"path"`
		StartLine int    `json:"start_line"`
		EndLine   int    `json:"end_line"`
	}
	if err := decodeLoose(input, &in); err != nil {
		return nil, err
	}
	at, err := src.Resolve(ctx, in.Ref)
	if err != nil {
		return nil, err
	}
	p, err := CleanPath(in.Path)
	if err != nil || p == "" {
		return nil, httpx.BadRequest("Name the file.")
	}
	e, data, err := src.File(ctx, at, p, maxToolFileBytes)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return map[string]any{"path": p, "ref": at.Ref, "commit": at.SHA, "size": e.Size, "too_large": true, "source": rr.source("")}, nil
	}
	if isBinary(data) {
		return map[string]any{"path": p, "ref": at.Ref, "commit": at.SHA, "binary": true, "size": e.Size, "source": rr.source("")}, nil
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
		b.WriteString(strconv.Itoa(i) + "\t" + strings.TrimSuffix(lines[i-1], "\n") + "\n")
	}
	return map[string]any{"path": p, "ref": at.Ref, "commit": at.SHA, "size": e.Size, "total_lines": len(lines), "start_line": start,
		"end_line": min(end, len(lines)), "content": b.String(), "source": rr.source("")}, nil
}

func (s *Service) xToolSearch(ctx context.Context, _ *auth.Identity, src RepoSource, rr *remoteRepo, input json.RawMessage) (any, error) {
	var in struct {
		Query string `json:"query"`
		Ref   string `json:"ref"`
	}
	if err := decodeLoose(input, &in); err != nil {
		return nil, err
	}
	q := strings.TrimSpace(in.Query)
	if len([]rune(q)) < 2 || len([]rune(q)) > 200 {
		return nil, httpx.BadRequest("Search for 2 to 200 characters.")
	}
	at, err := src.Resolve(ctx, in.Ref)
	if err != nil {
		return nil, err
	}
	matches, truncated, err := src.SearchCode(ctx, q, at)
	if err != nil {
		return nil, err
	}
	if len(matches) > 100 {
		matches, truncated = matches[:100], true
	}
	return map[string]any{"ref": at.Ref, "commit": at.SHA, "matches": matches, "truncated": truncated, "source": rr.source("")}, nil
}

func (s *Service) xToolLog(ctx context.Context, _ *auth.Identity, src RepoSource, rr *remoteRepo, input json.RawMessage) (any, error) {
	var in struct {
		Ref   string `json:"ref"`
		Path  string `json:"path"`
		Limit int    `json:"limit"`
	}
	if err := decodeLoose(input, &in); err != nil {
		return nil, err
	}
	at, err := src.Resolve(ctx, in.Ref)
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
	list, _, err := src.Log(ctx, at, p, 1, in.Limit)
	if err != nil {
		return nil, err
	}
	return map[string]any{"ref": at.Ref, "commits": list, "source": rr.source("")}, nil
}

func (s *Service) xToolDiff(ctx context.Context, _ *auth.Identity, src RepoSource, rr *remoteRepo, input json.RawMessage) (any, error) {
	var in struct {
		Commit, Base, Head, Path string
	}
	if err := decodeLoose(input, &in); err != nil {
		return nil, err
	}
	p, err := CleanPath(in.Path)
	if err != nil {
		return nil, httpx.BadRequest("That path isn't valid.")
	}
	if in.Commit != "" {
		v, err := src.Commit(ctx, in.Commit, p)
		if err != nil {
			return nil, err
		}
		return map[string]any{"commit": v.Commit, "files": patchText(v.Diff), "source": rr.source("")}, nil
	}
	if in.Head == "" {
		return nil, httpx.BadRequest("Pass commit, or head (and optionally base).")
	}
	cmp, err := src.Compare(ctx, in.Base, in.Head, p)
	if err != nil {
		return nil, err
	}
	return map[string]any{"base": cmp.Base, "head": cmp.Head, "ahead": cmp.Ahead, "behind": cmp.Behind, "commits": cmp.Commits,
		"files": patchText(cmp.Diff), "source": rr.source("")}, nil
}

func (s *Service) xToolPulls(ctx context.Context, _ *auth.Identity, src RepoSource, rr *remoteRepo, input json.RawMessage) (any, error) {
	var in struct {
		State string `json:"state"`
	}
	if err := decodeLoose(input, &in); err != nil {
		return nil, err
	}
	list, _, err := src.Pulls(ctx, pullQuery{State: in.State, Page: 1, Limit: 50})
	if err != nil {
		return nil, err
	}
	type row struct {
		Number    int    `json:"number"`
		Title     string `json:"title"`
		State     string `json:"state"`
		Head      string `json:"head,omitempty"`
		Base      string `json:"base,omitempty"`
		Author    string `json:"author"`
		Approvals int    `json:"approvals"`
		Comments  int    `json:"comments"`
		Updated   string `json:"updated_at"`
		Link      string `json:"link"`
	}
	out := make([]row, 0, len(list))
	for _, p := range list {
		author := ""
		if p.Author != nil {
			author = p.Author.Login
		}
		out = append(out, row{Number: p.Number, Title: p.Title, State: p.State, Head: p.HeadBranch, Base: p.BaseBranch, Author: author,
			Approvals: p.Approvals, Comments: p.CommentCount, Updated: p.UpdatedAt.Format("2006-01-02T15:04:05Z"), Link: rr.link(fmt.Sprintf("pulls/%d", p.Number))})
	}
	return map[string]any{"pull_requests": out, "source": rr.source("")}, nil
}

func (s *Service) xToolPull(ctx context.Context, _ *auth.Identity, src RepoSource, rr *remoteRepo, input json.RawMessage) (any, error) {
	var in struct {
		Number      int  `json:"number"`
		IncludeDiff bool `json:"include_diff"`
	}
	if err := decodeLoose(input, &in); err != nil {
		return nil, err
	}
	p, err := src.Pull(ctx, in.Number)
	if err != nil {
		return nil, err
	}
	m, err := src.Mergeability(ctx, in.Number)
	if err != nil {
		return nil, err
	}
	timeline, err := src.Timeline(ctx, in.Number)
	if err != nil {
		return nil, err
	}
	threads, _, err := src.Threads(ctx, in.Number)
	if err != nil {
		return nil, err
	}
	type note struct {
		ID     string `json:"id"`
		Author string `json:"author"`
		Body   string `json:"body"`
		Where  string `json:"where,omitempty"`
		Reply  bool   `json:"reply,omitempty"`
	}
	who := func(u *XUser) string {
		if u == nil {
			return ""
		}
		return u.Login
	}
	notes := []note{}
	for _, it := range timeline {
		switch {
		case it.Comment != nil:
			notes = append(notes, note{ID: it.Comment.ID, Author: who(it.Comment.Author), Body: it.Comment.Body, Reply: it.Comment.ReplyTo != nil})
		case it.Review != nil && it.Review.Body != "":
			notes = append(notes, note{ID: it.Review.ID, Author: who(it.Review.Author), Body: "[" + it.Review.State + "] " + it.Review.Body})
		}
	}
	for _, t := range threads {
		for i, c := range append([]XComment{t.Root}, t.Replies...) {
			n := note{ID: c.ID, Author: who(c.Author), Body: c.Body, Reply: i > 0}
			if c.Path != nil && c.Line != nil {
				n.Where = fmt.Sprintf("%s:%d (%s)", *c.Path, *c.Line, *c.Side)
			}
			notes = append(notes, n)
		}
	}
	d, err := src.PullDiff(ctx, in.Number, "")
	if err != nil {
		return nil, err
	}
	checks, _ := src.Checks(ctx, p.HeadSHA)
	out := map[string]any{"number": p.Number, "title": p.Title, "body": p.Body, "state": p.State, "head": p.HeadBranch, "base": p.BaseBranch,
		"head_commit": p.HeadSHA, "author": who(p.Author), "reviewers": p.Reviewers, "merge": m, "comments": notes, "checks": checks,
		"link": rr.link(fmt.Sprintf("pulls/%d", p.Number)), "changed_files": diffStat(d.Diff), "source": p.Source}
	if in.IncludeDiff {
		out["files"] = patchText(d.Diff)
	}
	return out, nil
}

// connectedRepoRows lists connected repositories for repos_list: each
// active connection's most recent (or matching) ones, agent-filtered.
func (s *Service) connectedRepoRows(ctx context.Context, caller *auth.Identity, source, query string, limit int) ([]XRepoSummary, error) {
	conns := s.connections()
	if conns == nil {
		return nil, nil
	}
	list, err := s.connectedSources(ctx, caller.UserID, uuid.Nil)
	if err != nil {
		return nil, err
	}
	var out []XRepoSummary
	for _, src := range list {
		if src.Status != "active" || (source != "" && source != "connected" && source != src.Provider && source != src.ID) {
			continue
		}
		lister, _, err := s.openAccount(ctx, caller.UserID, src.ID)
		if err != nil {
			continue
		}
		rows, _, err := lister.listRepos(ctx, query, "recent", "", 1)
		if err != nil {
			continue
		}
		if len(rows) > limit {
			rows = rows[:limit]
		}
		out = append(out, rows...)
	}
	return s.agentFilterSummaries(ctx, out)
}

// ---- writes on a platform --------------------------------------------------------

// externalTarget reads repo_id for a platform write tool; with no id it
// still names the kind (agents learns kinds by calling Target with {}).
func externalTarget(provider string) func(json.RawMessage) (string, string) {
	return func(input json.RawMessage) (string, string) {
		if id, ok := parseExtID(toolRepoID(input)); ok && id.Provider == provider {
			return ExternalKind, id.target()
		}
		return ExternalKind, ""
	}
}

// toolExternal parses repo_id for a platform tool and checks the
// connection's agent scope: need is AgentSuggest in Preview (an agent at
// read can't even suggest) and AgentEdit in Handler (an accepted
// suggestion runs as the person, which passes).
func (s *Service) toolExternal(ctx context.Context, caller *auth.Identity, provider string, input json.RawMessage, need app.AgentLevel) (extID, error) {
	id, ok := parseExtID(toolRepoID(input))
	if !ok || id.Provider != provider {
		return extID{}, httpx.BadRequest("repo_id must be a " + platformNames[provider] + " repository id from repos_list.")
	}
	for _, feature := range []string{app.ConnectionScope(id.ConnID), "repos"} {
		ok, err := app.AgentAllows(ctx, s.d.Modules, caller, feature, ExternalKind, id.target(), need)
		if err != nil {
			return extID{}, err
		}
		if !ok {
			verb := "change"
			if need == app.AgentSuggest {
				verb = "suggest changes to"
			}
			return extID{}, httpx.NewError(http.StatusForbidden, "agent_scope_read_only",
				"The person lets AI agents only read "+platformNames[provider]+" ("+id.Path+"), not "+verb+" it. They can change that in Settings → AI agents.")
		}
	}
	return id, nil
}

// protectedOrDefault refuses the default branch and protected branches:
// agents only ever commit to feature branches.
func protectedOrDefault(ctx context.Context, src RepoSource, branch string) error {
	refs, def, err := src.Refs(ctx, false)
	if err != nil {
		return err
	}
	if branch == def {
		return httpx.NewError(http.StatusForbidden, "branch_protected", branch+" is the default branch. Commit to a feature branch and open a pull request.")
	}
	for _, r := range refs {
		if r.Kind == "branch" && r.Name == branch && r.Protected {
			return httpx.NewError(http.StatusForbidden, "branch_protected", branch+" is protected. Commit to a feature branch and open a pull request.")
		}
	}
	return nil
}

func branchExists(ctx context.Context, src RepoSource, branch string) (string, bool) {
	refs, _, err := src.Refs(ctx, false)
	if err != nil {
		return "", false
	}
	for _, r := range refs {
		if r.Kind == "branch" && r.Name == branch {
			return r.SHA, true
		}
	}
	return "", false
}

type xToolCommitIn struct {
	RepoID       string        `json:"repo_id"`
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

// xCommitPlan maps the tool's branch fields: an existing feature branch
// is committed to; a missing one is created from create_from.
func xCommitPlan(ctx context.Context, src RepoSource, in xToolCommitIn) (CommitInput, string, error) {
	branch := strings.TrimSpace(in.Branch)
	if branch == "" {
		return CommitInput{}, "", httpx.BadRequest("Name the feature branch to commit to.")
	}
	if err := protectedOrDefault(ctx, src, branch); err != nil {
		return CommitInput{}, "", err
	}
	if head, ok := branchExists(ctx, src, branch); ok {
		return CommitInput{Branch: branch, ExpectedHead: in.ExpectedHead, Message: in.Message, Changes: in.Changes}, head, nil
	}
	from := strings.TrimSpace(in.CreateFrom)
	_, def, _ := src.Refs(ctx, false)
	if from == "" {
		from = def
	}
	start, ok := branchExists(ctx, src, from)
	if !ok {
		return CommitInput{}, "", httpx.NotFound("create_from must name an existing branch; there's no branch named " + from + ".")
	}
	head := start
	if in.ExpectedHead != "" {
		head = in.ExpectedHead
	}
	return CommitInput{Branch: from, ExpectedHead: head, NewBranch: branch, Message: in.Message, Changes: in.Changes}, head, nil
}

func (s *Service) externalTools(provider string) []app.Tool {
	name := platformNames[provider]
	pr := "pull request"
	if provider == ProviderGitLab {
		pr = "merge request"
	}
	idProp := `"repo_id":{"type":"string","description":"The ` + name + ` repository's id from repos_list (` + provider + `~…)."}`
	prefix := "repos_" + provider + "_"
	target := externalTarget(provider)
	open := func(ctx context.Context, caller *auth.Identity, input json.RawMessage, write bool) (RepoSource, *remoteRepo, extID, error) {
		need := app.AgentSuggest
		if write {
			need = app.AgentEdit
		}
		id, err := s.toolExternal(ctx, caller, provider, input, need)
		if err != nil {
			return nil, nil, id, err
		}
		src, rr, err := s.openRemote(ctx, caller.UserID, id, write)
		return src, rr, id, err
	}
	return []app.Tool{
		{
			Name:        prefix + "create_branch",
			Description: "Create a branch in a " + name + " repository from another branch, tag or commit (default: the default branch). You usually don't need this: " + prefix + "commit_files creates its branch.",
			InputSchema: schema(idProp+`,"name":{"type":"string"},"from":{"type":"string"}`, `"repo_id","name"`),
			Source:      name,
			Target:      target,
			Preview: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
				var in struct{ Name, From string }
				if err := decodeLoose(input, &in); err != nil {
					return app.ToolPreview{}, err
				}
				src, rr, id, err := open(ctx, caller, input, false)
				if err != nil {
					return app.ToolPreview{}, err
				}
				from := in.From
				if from == "" {
					_, from, _ = src.Refs(ctx, false)
				}
				at, err := src.Resolve(ctx, from)
				if err != nil {
					return app.ToolPreview{}, s.remoteErr(err, rr)
				}
				return app.ToolPreview{Summary: fmt.Sprintf("Create branch %s from %s (%s) in %s on %s", in.Name, from, reposShort(at.SHA), id.Path, name), Link: rr.link("branches")}, nil
			},
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct{ Name, From string }
				if err := decodeLoose(input, &in); err != nil {
					return nil, err
				}
				src, rr, _, err := open(ctx, caller, input, true)
				if err != nil {
					return nil, err
				}
				ref, err := src.CreateBranch(ctx, strings.TrimSpace(in.Name), strings.TrimSpace(in.From))
				if err != nil {
					return nil, s.remoteErr(err, rr)
				}
				return map[string]any{"branch": ref.Name, "commit": ref.SHA, "link": rr.link("branches"), "source": rr.source("")}, nil
			},
		},
		{
			Name: prefix + "commit_files",
			Description: "Commit file changes to a feature branch of a " + name + " repository as one commit; never to the default or a protected branch. If the branch doesn't exist it is created from create_from (default: the default branch). " +
				"Changes: {op:\"put\", path, content}, {op:\"replace_lines\", path, start_line, end_line, content}, {op:\"delete\", path}, {op:\"move\", from, path}. Pass expected_head (the commit you read) so a file changed since is refused instead of overwritten. " +
				"Set open_pull_request to open a " + pr + " into base in the same step. With the person's scope at Suggest this becomes a diff they review.",
			InputSchema: schema(idProp+`,"branch":{"type":"string","description":"Feature branch, e.g. agent/fix-login."},"create_from":{"type":"string"},"expected_head":{"type":"string"},"message":{"type":"string"},`+
				`"changes":{"type":"array","minItems":1,"maxItems":100,"items":{"type":"object","properties":{"op":{"type":"string","enum":["put","replace_lines","delete","move"]},"path":{"type":"string"},"from":{"type":"string"},"content":{"type":"string"},"start_line":{"type":"integer"},"end_line":{"type":"integer"}},"required":["op","path"],"additionalProperties":false}},`+
				`"open_pull_request":{"type":"object","properties":{"title":{"type":"string"},"body":{"type":"string"},"base":{"type":"string"}},"required":["title"],"additionalProperties":false}`,
				`"repo_id","branch","message","changes"`),
			Source: name,
			Target: target,
			Preview: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
				var in xToolCommitIn
				if err := decodeLoose(input, &in); err != nil {
					return app.ToolPreview{}, err
				}
				src, rr, id, err := open(ctx, caller, input, false)
				if err != nil {
					return app.ToolPreview{}, err
				}
				pv, err := s.previewExternalCommit(ctx, src, rr, id, in, pr)
				return pv, s.remoteErr(err, rr)
			},
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in xToolCommitIn
				if err := decodeLoose(input, &in); err != nil {
					return nil, err
				}
				src, rr, _, err := open(ctx, caller, input, true)
				if err != nil {
					return nil, err
				}
				ci, _, err := xCommitPlan(ctx, src, in)
				if err != nil {
					return nil, s.remoteErr(err, rr)
				}
				res, err := src.CommitChanges(ctx, remoteCommit{CommitInput: ci, agent: s.agentLine(ctx, caller)})
				if err != nil {
					return nil, s.remoteErr(err, rr)
				}
				out := map[string]any{"commit": res.Commit.SHA, "branch": res.Branch, "link": rr.link("commits/" + res.Commit.SHA), "source": rr.source("")}
				if in.OpenPull != nil {
					base := in.OpenPull.Base
					if base == "" {
						_, base, _ = src.Refs(ctx, false)
					}
					if open, _, err := src.Pulls(ctx, pullQuery{State: "open", Head: res.Branch, Base: base, Page: 1, Limit: 1}); err == nil && len(open) > 0 {
						out["pull_request"] = map[string]any{"number": open[0].Number, "link": rr.link(fmt.Sprintf("pulls/%d", open[0].Number)), "already_open": true}
						return out, nil
					}
					p, err := src.OpenPull(ctx, OpenPullInput{Title: in.OpenPull.Title, Body: in.OpenPull.Body, Head: res.Branch, Base: base})
					if err != nil {
						out["pull_request_error"] = s.remoteErr(err, rr).Error()
						return out, nil
					}
					out["pull_request"] = map[string]any{"number": p.Number, "link": rr.link(fmt.Sprintf("pulls/%d", p.Number)), "web_url": p.WebURL}
				}
				return out, nil
			},
		},
		{
			Name:        prefix + "open_pull_request",
			Description: "Open a " + pr + " in a " + name + " repository proposing head for merging into base (default: the default branch), with a title and a description of what changed and why.",
			InputSchema: schema(idProp+`,"head":{"type":"string"},"base":{"type":"string"},"title":{"type":"string"},"body":{"type":"string"}`, `"repo_id","head","title"`),
			Source:      name,
			Target:      target,
			Preview: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
				var in struct{ Head, Base, Title, Body string }
				if err := decodeLoose(input, &in); err != nil {
					return app.ToolPreview{}, err
				}
				src, rr, id, err := open(ctx, caller, input, false)
				if err != nil {
					return app.ToolPreview{}, err
				}
				base := in.Base
				if base == "" {
					_, base, _ = src.Refs(ctx, false)
				}
				cmp, err := src.Compare(ctx, base, in.Head, "")
				if err != nil {
					return app.ToolPreview{}, s.remoteErr(err, rr)
				}
				return app.ToolPreview{Summary: fmt.Sprintf("Open %s “%s”: %s → %s in %s on %s", pr, in.Title, in.Head, base, id.Path, name),
					After: in.Title + "\n\n" + in.Body + "\n\n" + diffStat(cmp.Diff), Link: rr.link("compare?base=" + base + "&head=" + in.Head)}, nil
			},
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct{ Head, Base, Title, Body string }
				if err := decodeLoose(input, &in); err != nil {
					return nil, err
				}
				src, rr, _, err := open(ctx, caller, input, true)
				if err != nil {
					return nil, err
				}
				p, err := src.OpenPull(ctx, OpenPullInput{Title: in.Title, Body: in.Body, Head: in.Head, Base: in.Base})
				if err != nil {
					return nil, s.remoteErr(err, rr)
				}
				return map[string]any{"number": p.Number, "state": p.State, "link": rr.link(fmt.Sprintf("pulls/%d", p.Number)), "web_url": p.WebURL, "source": p.Source}, nil
			},
		},
		{
			Name:        prefix + "comment_on_pull_request",
			Description: "Comment on a " + name + " " + pr + ": in the conversation, on one line of a changed file (path and line on the new side; side \"old\" for a removed line), or as a reply (reply_to a comment id from the read tools).",
			InputSchema: schema(idProp+`,"number":{"type":"integer","minimum":1},"body":{"type":"string"},"path":{"type":"string"},"line":{"type":"integer","minimum":1},"side":{"type":"string","enum":["new","old"]},"reply_to":{"type":"string"}`, `"repo_id","number","body"`),
			Source:      name,
			Target:      target,
			Preview: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
				var in struct {
					Number int `json:"number"`
					xCommentInput
				}
				if err := decodeLoose(input, &in); err != nil {
					return app.ToolPreview{}, err
				}
				src, rr, id, err := open(ctx, caller, input, false)
				if err != nil {
					return app.ToolPreview{}, err
				}
				p, err := src.Pull(ctx, in.Number)
				if err != nil {
					return app.ToolPreview{}, s.remoteErr(err, rr)
				}
				where := ""
				if in.Path != "" {
					where = fmt.Sprintf(" on %s line %d", in.Path, in.Line)
				}
				return app.ToolPreview{Summary: fmt.Sprintf("Comment%s in #%d “%s” (%s on %s)", where, p.Number, p.Title, id.Path, name), After: in.Body, Link: rr.link(fmt.Sprintf("pulls/%d", p.Number))}, nil
			},
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					Number int `json:"number"`
					xCommentInput
				}
				if err := decodeLoose(input, &in); err != nil {
					return nil, err
				}
				src, rr, _, err := open(ctx, caller, input, true)
				if err != nil {
					return nil, err
				}
				c, err := src.Comment(ctx, in.Number, in.xCommentInput)
				if err != nil {
					return nil, s.remoteErr(err, rr)
				}
				return map[string]any{"comment_id": c.ID, "link": rr.link(fmt.Sprintf("pulls/%d", in.Number)), "source": rr.source("")}, nil
			},
		},
	}
}

func reposShort(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// previewExternalCommit shows exactly what the commit would change: every
// touched file before and after, read from the platform at the base.
func (s *Service) previewExternalCommit(ctx context.Context, src RepoSource, rr *remoteRepo, id extID, in xToolCommitIn, pr string) (app.ToolPreview, error) {
	ci, head, err := xCommitPlan(ctx, src, in)
	if err != nil {
		return app.ToolPreview{}, err
	}
	if err := checkCommitInput(&ci, ci.Branch); err != nil {
		return app.ToolPreview{}, err
	}
	at := resolved{Ref: head, SHA: head}
	var before, after strings.Builder
	added, changed, removed := 0, 0, 0
	read := func(p string) ([]byte, bool) {
		e, data, err := src.File(ctx, at, p, s.maxEditBytes)
		if err != nil || e.Type != "blob" {
			return nil, false
		}
		if data == nil {
			return []byte("(file too large to show)\n"), true
		}
		return data, true
	}
	for _, c := range ci.Changes {
		header := "=== " + c.Path + " ===\n"
		before.WriteString(header)
		after.WriteString(header)
		old, existed := read(c.Path)
		if c.Op == "move" {
			old, existed = read(c.From)
			before.Reset()
			before.WriteString("=== " + c.From + " → " + c.Path + " ===\n")
		}
		switch {
		case existed && isBinary(old):
			before.WriteString("(binary file)\n")
		case existed:
			before.Write(old)
		default:
			before.WriteString("(new file)\n")
		}
		switch c.Op {
		case "delete":
			after.WriteString("(deleted)\n")
			removed++
		case "move":
			after.Write(old)
			changed++
		default:
			data, err := changeContent(c, old, existed)
			if err != nil {
				return app.ToolPreview{}, err
			}
			if isBinary(data) {
				after.WriteString("(binary file)\n")
			} else {
				after.Write(data)
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
	summary := fmt.Sprintf("Commit “%s” to %s in %s on %s: %s", firstLine(strings.TrimSpace(in.Message)), target, id.Path, rr.platform, countPhrase(added, changed, removed))
	if in.OpenPull != nil {
		base := in.OpenPull.Base
		if base == "" {
			_, base, _ = src.Refs(ctx, false)
		}
		headBranch := ci.Branch
		if ci.NewBranch != "" {
			headBranch = ci.NewBranch
		}
		header := "=== " + pr + " " + headBranch + " → " + base + " ===\n"
		before.WriteString(header)
		after.WriteString(header)
		if open, _, err := src.Pulls(ctx, pullQuery{State: "open", Head: headBranch, Base: base, Page: 1, Limit: 1}); err == nil && len(open) > 0 {
			fmt.Fprintf(&before, "#%d is open\n", open[0].Number)
			fmt.Fprintf(&after, "#%d is open (the commit adds to it)\n", open[0].Number)
			summary += fmt.Sprintf(", adding to %s #%d", pr, open[0].Number)
		} else {
			before.WriteString("(none)\n")
			after.WriteString(strings.TrimSpace(in.OpenPull.Title) + "\n\n" + strings.TrimSpace(in.OpenPull.Body) + "\n")
			summary += ", and open a " + pr + " into " + base
		}
	}
	return app.ToolPreview{Summary: summary, Before: before.String(), After: after.String(), Link: rr.link("")}, nil
}
