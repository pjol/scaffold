package repos

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"scaffoldapp/backend/internal/httpx"
)

// gitlabSource maps GitLab's REST API (v4) onto RepoSource. The API base
// comes from the connection, so self-managed instances (also under a path,
// https://git.example.com/gitlab/api/v4) work the same as gitlab.com.
// Merge requests are this app's pull requests; a project's numeric id is
// never needed, its URL-encoded path works everywhere.
type gitlabSource struct{ *remoteRepo }

// ---- GitLab shapes ---------------------------------------------------------------

type glUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
}

type glProject struct {
	ID                int64      `json:"id"`
	Name              string     `json:"name"`
	Path              string     `json:"path"`
	PathWithNamespace string     `json:"path_with_namespace"`
	Description       *string    `json:"description"`
	DefaultBranch     string     `json:"default_branch"`
	WebURL            string     `json:"web_url"`
	HTTPURL           string     `json:"http_url_to_repo"`
	LastActivityAt    *time.Time `json:"last_activity_at"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         *time.Time `json:"updated_at"`
	Visibility        string     `json:"visibility"`
	Archived          bool       `json:"archived"`
	EmptyRepo         bool       `json:"empty_repo"`
	Namespace         struct {
		FullPath string `json:"full_path"`
	} `json:"namespace"`
	Permissions *struct {
		ProjectAccess *struct {
			AccessLevel int `json:"access_level"`
		} `json:"project_access"`
		GroupAccess *struct {
			AccessLevel int `json:"access_level"`
		} `json:"group_access"`
	} `json:"permissions"`
	MergeMethod            string `json:"merge_method"`
	SquashOption           string `json:"squash_option"`
	RemoveSourceBranch     bool   `json:"remove_source_branch_after_merge"`
	OnlyAllowMergeIfPassed bool   `json:"only_allow_merge_if_pipeline_succeeds"`
	Statistics             *struct {
		RepositorySize int64 `json:"repository_size"`
	} `json:"statistics"`
}

type glCommit struct {
	ID             string    `json:"id"`
	Title          string    `json:"title"`
	Message        string    `json:"message"`
	AuthorName     string    `json:"author_name"`
	AuthorEmail    string    `json:"author_email"`
	AuthoredDate   time.Time `json:"authored_date"`
	CommitterName  string    `json:"committer_name"`
	CommitterEmail string    `json:"committer_email"`
	CommittedDate  time.Time `json:"committed_date"`
	ParentIDs      []string  `json:"parent_ids"`
	WebURL         string    `json:"web_url"`
}

type glDiff struct {
	OldPath     string `json:"old_path"`
	NewPath     string `json:"new_path"`
	AMode       string `json:"a_mode"`
	BMode       string `json:"b_mode"`
	NewFile     bool   `json:"new_file"`
	RenamedFile bool   `json:"renamed_file"`
	DeletedFile bool   `json:"deleted_file"`
	Diff        string `json:"diff"`
	TooLarge    bool   `json:"too_large"`
	Collapsed   bool   `json:"collapsed"`
}

type glMR struct {
	ID           int64      `json:"id"`
	IID          int        `json:"iid"`
	Title        string     `json:"title"`
	Description  *string    `json:"description"`
	State        string     `json:"state"`
	SourceBranch string     `json:"source_branch"`
	TargetBranch string     `json:"target_branch"`
	SHA          string     `json:"sha"`
	Author       *glUser    `json:"author"`
	Reviewers    []glUser   `json:"reviewers"`
	MergedBy     *glUser    `json:"merged_by"`
	MergeUser    *glUser    `json:"merge_user"`
	MergedAt     *time.Time `json:"merged_at"`
	ClosedBy     *glUser    `json:"closed_by"`
	ClosedAt     *time.Time `json:"closed_at"`
	MergeSHA     *string    `json:"merge_commit_sha"`
	SquashSHA    *string    `json:"squash_commit_sha"`
	Notes        int        `json:"user_notes_count"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	WebURL       string     `json:"web_url"`
	Draft        bool       `json:"draft"`
	WorkInProg   bool       `json:"work_in_progress"`
	MergeStatus  string     `json:"merge_status"`
	Detailed     string     `json:"detailed_merge_status"`
	HasConflicts bool       `json:"has_conflicts"`
	Squash       bool       `json:"squash"`
	ForceRemove  *bool      `json:"force_remove_source_branch"`
	DiffRefs     *struct {
		BaseSHA  string `json:"base_sha"`
		HeadSHA  string `json:"head_sha"`
		StartSHA string `json:"start_sha"`
	} `json:"diff_refs"`
	HeadPipeline *struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
		WebURL string `json:"web_url"`
	} `json:"head_pipeline"`
}

type glNote struct {
	ID         int64      `json:"id"`
	Type       *string    `json:"type"` // DiffNote | DiscussionNote | null
	Body       string     `json:"body"`
	Author     *glUser    `json:"author"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	System     bool       `json:"system"`
	Resolvable bool       `json:"resolvable"`
	Resolved   bool       `json:"resolved"`
	ResolvedBy *glUser    `json:"resolved_by"`
	ResolvedAt *time.Time `json:"resolved_at"`
	Position   *struct {
		BaseSHA  string `json:"base_sha"`
		StartSHA string `json:"start_sha"`
		HeadSHA  string `json:"head_sha"`
		OldPath  string `json:"old_path"`
		NewPath  string `json:"new_path"`
		OldLine  *int   `json:"old_line"`
		NewLine  *int   `json:"new_line"`
	} `json:"position"`
}

type glDiscussion struct {
	ID             string   `json:"id"`
	IndividualNote bool     `json:"individual_note"`
	Notes          []glNote `json:"notes"`
}

// ---- helpers ---------------------------------------------------------------------

func (g *gitlabSource) projURL(sub string) string {
	u := g.api + "/projects/" + url.PathEscape(g.id.Path)
	if sub != "" {
		u += "/" + sub
	}
	return u
}

func (g *gitlabSource) user(u *glUser) *XUser {
	if u == nil || u.Username == "" {
		return nil
	}
	return &XUser{ID: "gitlab:" + strconv.FormatInt(u.ID, 10), DisplayName: orDefault(u.Name, u.Username), Login: u.Username}
}

func (g *gitlabSource) isViewer(u *glUser) bool {
	return u != nil && strconv.FormatInt(u.ID, 10) == g.viewer.ID
}

func glToCommit(c glCommit) Commit {
	subject, body := splitMessage(c.Message)
	if subject == "" {
		subject = c.Title
	}
	out := Commit{SHA: c.ID, Parents: c.ParentIDs, Subject: subject, Body: body,
		Author:    Signature{Name: c.AuthorName, Email: c.AuthorEmail, At: c.AuthoredDate},
		Committer: Signature{Name: c.CommitterName, Email: c.CommitterEmail, At: c.CommittedDate}}
	if out.Parents == nil {
		out.Parents = []string{}
	}
	return out
}

func glPatchFiles(diffs []glDiff) []patchFile {
	out := make([]patchFile, 0, len(diffs))
	for _, d := range diffs {
		pf := patchFile{Path: d.NewPath, OldPath: d.OldPath, OldMode: d.AMode, NewMode: d.BMode, Status: "modified", Patch: d.Diff, TooLarge: d.TooLarge || d.Collapsed}
		switch {
		case d.NewFile:
			pf.Status = "added"
		case d.DeletedFile:
			pf.Status = "deleted"
		case d.RenamedFile:
			pf.Status = "renamed"
		case d.AMode != d.BMode && d.AMode != "" && d.BMode != "":
			pf.Status = "type_changed"
		}
		if strings.HasPrefix(pf.Patch, "Binary files") {
			pf.Patch = ""
		}
		out = append(out, pf)
	}
	return out
}

func (g *gitlabSource) project(ctx context.Context) (*glProject, error) {
	key := g.cacheKey("m:project")
	if v, ok := g.s.rcache.get(key); ok {
		return v.(*glProject), nil
	}
	var p glProject
	if _, err := g.get(ctx, g.projURL("?statistics=false"), &p); err != nil {
		return nil, err
	}
	g.s.rcache.set(key, &p, 30*time.Second)
	return &p, nil
}

func glAccess(p *glProject) int {
	lvl := 0
	if p.Permissions != nil {
		if p.Permissions.ProjectAccess != nil {
			lvl = p.Permissions.ProjectAccess.AccessLevel
		}
		if p.Permissions.GroupAccess != nil && p.Permissions.GroupAccess.AccessLevel > lvl {
			lvl = p.Permissions.GroupAccess.AccessLevel
		}
	}
	return lvl
}

// level maps GitLab roles: Maintainer/Owner manage, Developer edit,
// Reporter comment, Guest view.
func (g *gitlabSource) level(p *glProject) string {
	a := glAccess(p)
	return levelFrom(a >= 40, a >= 30, a >= 20, g.canWrite)
}

// ---- repository ------------------------------------------------------------------

func (g *gitlabSource) Info(ctx context.Context) (*XRepoView, error) {
	p, err := g.project(ctx)
	if err != nil {
		return nil, err
	}
	lvl := g.level(p)
	v := &XRepoView{ID: g.id.String(), Name: p.Name, Slug: p.Path, FullName: p.PathWithNamespace, DefaultBranch: p.DefaultBranch,
		CloneURL: p.HTTPURL, Link: g.link(""), MyLevel: lvl, CanPush: levelAtLeast(lvl, "edit"),
		AllowMergeCommit: true, AllowSquash: p.SquashOption != "never", DeleteBranchOnMerge: p.RemoveSourceBranch,
		PushedAt: p.LastActivityAt, CreatedAt: p.CreatedAt, Private: p.Visibility != "public", Archived: p.Archived, Empty: p.EmptyRepo,
		WebURL: p.WebURL, Source: g.source(p.WebURL),
		Supports: Supports{Upload: true, Blame: true, ResolveThreads: true, Checks: true, CodeSearchRefs: true, TreeCommits: true, Import: true}}
	if p.UpdatedAt != nil {
		v.UpdatedAt = *p.UpdatedAt
	} else if p.LastActivityAt != nil {
		v.UpdatedAt = *p.LastActivityAt
	}
	if p.Description != nil {
		v.Description = *p.Description
	}
	if p.SquashOption == "always" {
		v.AllowMergeCommit = false
	}
	if p.Statistics != nil {
		v.SizeBytes = p.Statistics.RepositorySize
	}
	if !g.canWrite {
		v.GrantWriteURL = g.s.grantWriteURL(ctx, g.userID, ProviderGitLab, g.link(""))
	}
	if h, err := g.get(ctx, g.projURL("merge_requests?state=opened&per_page=1"), nil); err == nil {
		if n := lastPage(h); n >= 0 {
			v.OpenPulls = n
		}
	}
	return v, nil
}

func (g *gitlabSource) Refs(ctx context.Context, counts bool) ([]RefView, string, error) {
	p, err := g.project(ctx)
	if err != nil {
		return nil, "", err
	}
	type glBranch struct {
		Name      string   `json:"name"`
		Commit    glCommit `json:"commit"`
		Protected bool     `json:"protected"`
		Default   bool     `json:"default"`
	}
	var branches []glBranch
	for page := 1; page > 0 && page <= 3; {
		var list []glBranch
		h, err := g.get(ctx, g.projURL(fmt.Sprintf("repository/branches?per_page=100&page=%d", page)), &list)
		if err != nil {
			return nil, "", err
		}
		branches = append(branches, list...)
		page = nextPage(h)
	}
	var tags []glBranch
	if _, err := g.get(ctx, g.projURL("repository/tags?per_page=100"), &tags); err != nil {
		return nil, "", err
	}
	out := make([]RefView, 0, len(branches)+len(tags))
	for _, b := range branches {
		c := glToCommit(b.Commit)
		out = append(out, RefView{Ref: Ref{Name: b.Name, Kind: "branch", SHA: b.Commit.ID, Subject: c.Subject, Author: c.Author.Name, UpdatedAt: c.Committer.At},
			Default: b.Default || b.Name == p.DefaultBranch, Protected: b.Protected})
	}
	for _, t := range tags {
		c := glToCommit(t.Commit)
		out = append(out, RefView{Ref: Ref{Name: t.Name, Kind: "tag", SHA: t.Commit.ID, Subject: c.Subject, Author: c.Author.Name, UpdatedAt: c.Committer.At}})
	}
	if counts {
		open := map[string]int{}
		var mrs []glMR
		if _, err := g.get(ctx, g.projURL("merge_requests?state=opened&per_page=100"), &mrs); err == nil {
			for _, m := range mrs {
				open[m.SourceBranch] = m.IID
			}
		}
		n := min(len(branches), 20)
		parallel(6, n, func(i int) {
			ref := &out[i]
			if num, ok := open[ref.Name]; ok {
				ref.OpenPull = ptr(num)
			}
			if ref.Default {
				return
			}
			count := func(from, to string) (int, bool) {
				var cmp struct {
					Commits []glCommit `json:"commits"`
				}
				_, err := g.get(ctx, g.projURL("repository/compare?straight=false&from="+url.QueryEscape(from)+"&to="+url.QueryEscape(to)), &cmp)
				return len(cmp.Commits), err == nil
			}
			a, ok1 := count(p.DefaultBranch, ref.Name)
			b, ok2 := count(ref.Name, p.DefaultBranch)
			if ok1 && ok2 {
				ref.Ahead, ref.Behind = ptr(a), ptr(b)
			}
		})
	}
	return out, p.DefaultBranch, nil
}

func (g *gitlabSource) Resolve(ctx context.Context, ref string) (resolved, error) {
	if ref == "" {
		p, err := g.project(ctx)
		if err != nil {
			return resolved{}, err
		}
		if p.EmptyRepo || p.DefaultBranch == "" {
			return resolved{}, errEmptyRepo
		}
		ref = p.DefaultBranch
	}
	if IsOID(ref) {
		return resolved{Ref: ref, SHA: strings.ToLower(ref)}, nil
	}
	key := g.cacheKey("m:ref:" + ref)
	if v, ok := g.s.rcache.get(key); ok {
		return resolved{Ref: ref, SHA: v.(string)}, nil
	}
	var c glCommit
	if _, err := g.get(ctx, g.projURL("repository/commits/"+url.PathEscape(ref)), &c); err != nil {
		if platformStatus(err) == http.StatusNotFound {
			return resolved{}, httpx.NotFound("There's no branch, tag or commit named " + ref + ".")
		}
		return resolved{}, err
	}
	g.s.rcache.set(key, c.ID, 10*time.Second)
	return resolved{Ref: ref, SHA: c.ID}, nil
}

type glTreeEntry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"` // tree | blob | commit
	Path string `json:"path"`
	Mode string `json:"mode"`
}

func (e glTreeEntry) entry() TreeEntry {
	typ := e.Type
	if typ == "blob" && e.Mode == "120000" {
		typ = "symlink"
	}
	mode := e.Mode
	if mode == "" {
		mode = entryMode(typ)
	}
	return TreeEntry{Name: e.Name, Path: e.Path, Type: typ, Mode: mode, OID: e.ID, Size: -1}
}

func (g *gitlabSource) treePage(ctx context.Context, at resolved, dir string, recursive bool, page int) ([]glTreeEntry, int, error) {
	u := g.projURL(fmt.Sprintf("repository/tree?per_page=100&page=%d&ref=%s", page, url.QueryEscape(at.SHA)))
	if dir != "" {
		u += "&path=" + url.QueryEscape(dir)
	}
	if recursive {
		u += "&recursive=true"
	}
	var list []glTreeEntry
	h, err := g.get(ctx, u, &list)
	return list, nextPage(h), err
}

func (g *gitlabSource) Tree(ctx context.Context, at resolved, dir string) ([]TreeEntry, error) {
	var out []TreeEntry
	for page := 1; page > 0 && page <= 10; {
		list, next, err := g.treePage(ctx, at, dir, false, page)
		if err != nil {
			if platformStatus(err) == http.StatusNotFound {
				return nil, httpx.NotFound("There's no folder at " + dir + " on " + at.Ref + ".")
			}
			return nil, err
		}
		for _, e := range list {
			out = append(out, e.entry())
		}
		page = next
	}
	if out == nil && dir != "" {
		return nil, httpx.NotFound("There's no folder at " + dir + " on " + at.Ref + ".")
	}
	if out == nil {
		out = []TreeEntry{}
	}
	sortEntries(out)
	return out, nil
}

func (g *gitlabSource) TreeCommits(ctx context.Context, at resolved, dir string, names []string) (map[string]Commit, error) {
	out := map[string]Commit{}
	if len(names) > 40 {
		names = names[:40]
	}
	results := make([]*Commit, len(names))
	parallel(6, len(names), func(i int) {
		p := names[i]
		if dir != "" {
			p = dir + "/" + names[i]
		}
		key := g.cacheKey("tc:" + at.SHA + ":" + p)
		if v, ok := g.s.rcache.get(key); ok {
			c := v.(Commit)
			results[i] = &c
			return
		}
		var list []glCommit
		if _, err := g.get(ctx, g.projURL("repository/commits?per_page=1&ref_name="+url.QueryEscape(at.SHA)+"&path="+url.QueryEscape(p)), &list); err == nil && len(list) > 0 {
			c := glToCommit(list[0])
			g.s.rcache.set(key, c, immutableTTL)
			results[i] = &c
		}
	})
	for i, c := range results {
		if c != nil {
			out[names[i]] = *c
		}
	}
	return out, nil
}

func (g *gitlabSource) fileURL(p, suffix, ref string) string {
	return g.projURL("repository/files/"+url.PathEscape(p)+suffix) + "?ref=" + url.QueryEscape(ref)
}

// File reads the size with HEAD first, so a large file isn't downloaded.
func (g *gitlabSource) File(ctx context.Context, at resolved, p string, max int64) (TreeEntry, []byte, error) {
	h, err := g.do(ctx, http.MethodHead, g.fileURL(p, "", at.SHA), nil, nil, reqOpts{raw: new([]byte)})
	if err != nil {
		if platformStatus(err) == http.StatusNotFound {
			// A folder answers 404 too.
			if list, _, terr := g.treePage(ctx, at, p, false, 1); terr == nil && len(list) > 0 {
				return TreeEntry{}, nil, httpx.NewError(http.StatusConflict, "is_folder", p+" is a folder.")
			}
			return TreeEntry{}, nil, httpx.NotFound("There's no file at " + p + " on " + at.Ref + ".")
		}
		return TreeEntry{}, nil, err
	}
	size, _ := strconv.ParseInt(h.Get("X-Gitlab-Size"), 10, 64)
	e := TreeEntry{Name: baseName(p), Path: p, Type: "blob", Mode: "100644", OID: h.Get("X-Gitlab-Blob-Id"), Size: size}
	if size > max {
		return e, nil, nil
	}
	var data []byte
	if _, err := g.do(ctx, http.MethodGet, g.fileURL(p, "/raw", at.SHA), nil, nil, reqOpts{accept: "*/*", raw: &data}); err != nil {
		return e, nil, err
	}
	return e, data, nil
}

// lastCommitOf is the last commit touching p as of ref (GitLab's
// optimistic-lock token for that file).
func (g *gitlabSource) lastCommitOf(ctx context.Context, p, ref string) (string, bool, error) {
	h, err := g.do(ctx, http.MethodHead, g.fileURL(p, "", ref), nil, nil, reqOpts{raw: new([]byte)})
	if err != nil {
		if platformStatus(err) == http.StatusNotFound {
			return "", false, nil
		}
		return "", false, err
	}
	return h.Get("X-Gitlab-Last-Commit-Id"), true, nil
}

func (g *gitlabSource) Blame(ctx context.Context, at resolved, p string) ([]BlameRange, error) {
	var list []struct {
		Commit glCommit `json:"commit"`
		Lines  []string `json:"lines"`
	}
	if _, err := g.get(ctx, g.fileURL(p, "/blame", at.SHA), &list); err != nil {
		return nil, err
	}
	out := []BlameRange{}
	line := 1
	for _, b := range list {
		c := glToCommit(b.Commit)
		out = append(out, BlameRange{SHA: c.SHA, Start: line, Lines: len(b.Lines), Author: c.Author.Name, At: c.Author.At.Unix(), Subject: c.Subject})
		line += len(b.Lines)
	}
	return out, nil
}

func (g *gitlabSource) Paths(ctx context.Context, at resolved) ([]string, bool, error) {
	key := g.cacheKey("paths:" + at.SHA)
	type cached struct {
		paths     []string
		truncated bool
	}
	if v, ok := g.s.rcache.get(key); ok {
		c := v.(cached)
		return c.paths, c.truncated, nil
	}
	var paths []string
	truncated := false
	for page := 1; page > 0; {
		if page > 30 {
			truncated = true
			break
		}
		list, next, err := g.treePage(ctx, at, "", true, page)
		if err != nil {
			return nil, false, err
		}
		for _, e := range list {
			if e.Type == "blob" {
				paths = append(paths, e.Path)
			}
		}
		page = next
	}
	g.s.rcache.set(key, cached{paths, truncated}, immutableTTL)
	return paths, truncated, nil
}

func (g *gitlabSource) SearchCode(ctx context.Context, q string, at resolved) ([]GrepMatch, bool, error) {
	var list []struct {
		Path      string `json:"path"`
		Data      string `json:"data"`
		Startline int    `json:"startline"`
	}
	ref := at.Ref
	if ref == "" {
		ref = at.SHA
	}
	if _, err := g.get(ctx, g.projURL("search?scope=blobs&per_page=50&search="+url.QueryEscape(q)+"&ref="+url.QueryEscape(ref)), &list); err != nil {
		return nil, false, err
	}
	var out []GrepMatch
	for _, b := range list {
		for _, m := range grepLines(b.Path, strings.TrimRight(b.Data, "\n"), q, maxGrepPerFile) {
			m.Line += max(b.Startline, 1) - 1
			out = append(out, m)
		}
	}
	truncated := len(list) >= 50
	if len(out) > maxGrepMatches {
		out, truncated = out[:maxGrepMatches], true
	}
	return out, truncated, nil
}

func (g *gitlabSource) Log(ctx context.Context, at resolved, p string, page, limit int) ([]Commit, bool, error) {
	u := g.projURL(fmt.Sprintf("repository/commits?per_page=%d&page=%d&ref_name=%s", limit, page, url.QueryEscape(at.SHA)))
	if p != "" {
		u += "&path=" + url.QueryEscape(p)
	}
	var list []glCommit
	h, err := g.get(ctx, u, &list)
	if err != nil {
		return nil, false, err
	}
	out := make([]Commit, 0, len(list))
	for _, c := range list {
		out = append(out, glToCommit(c))
	}
	return out, nextPage(h) > 0, nil
}

func (g *gitlabSource) diffs(ctx context.Context, base string, pages int) ([]glDiff, bool, error) {
	pages = pages * 100 / remotePerPage
	var all []glDiff
	for page := 1; page > 0; {
		if page > pages {
			return all, true, nil
		}
		var list []glDiff
		sep := "?"
		if strings.Contains(base, "?") {
			sep = "&"
		}
		h, err := g.get(ctx, base+fmt.Sprintf("%sper_page=%d&page=%d", sep, remotePerPage, page), &list)
		if err != nil {
			return nil, false, err
		}
		all = append(all, list...)
		page = nextPage(h)
	}
	return all, false, nil
}

func (g *gitlabSource) Commit(ctx context.Context, sha, p string) (*CommitView, error) {
	var c glCommit
	if _, err := g.get(ctx, g.projURL("repository/commits/"+url.PathEscape(sha)), &c); err != nil {
		if platformStatus(err) == http.StatusNotFound {
			return nil, httpx.NotFound("There's no commit " + sha + ".")
		}
		return nil, err
	}
	diffs, truncated, err := g.diffs(ctx, g.projURL("repository/commits/"+url.PathEscape(c.ID)+"/diff"), 5)
	if err != nil {
		return nil, err
	}
	return &CommitView{Commit: glToCommit(c), Diff: buildDiff(glPatchFiles(diffs), truncated, p)}, nil
}

func (g *gitlabSource) Compare(ctx context.Context, base, head, p string) (*Comparison, error) {
	pr, err := g.project(ctx)
	if err != nil {
		return nil, err
	}
	if base == "" {
		base = pr.DefaultBranch
	}
	var cmp struct {
		Commit  *glCommit  `json:"commit"`
		Commits []glCommit `json:"commits"`
		Diffs   []glDiff   `json:"diffs"`
	}
	if _, err := g.get(ctx, g.projURL("repository/compare?straight=false&from="+url.QueryEscape(base)+"&to="+url.QueryEscape(head)), &cmp); err != nil {
		if platformStatus(err) == http.StatusNotFound {
			return nil, httpx.NotFound("Both branches must exist to compare them.")
		}
		return nil, err
	}
	out := &Comparison{Base: base, Head: head, Ahead: len(cmp.Commits), Commits: []Commit{}, Conflicts: []string{},
		Diff: buildDiff(glPatchFiles(cmp.Diffs), false, p)}
	for _, c := range cmp.Commits {
		out.Commits = append(out.Commits, glToCommit(c))
	}
	sortCommitsOldestFirst(out.Commits)
	if b, err := g.Resolve(ctx, base); err == nil {
		out.BaseSHA = b.SHA
	}
	if h, err := g.Resolve(ctx, head); err == nil {
		out.HeadSHA = h.SHA
	}
	var back struct {
		Commits []glCommit `json:"commits"`
	}
	if _, err := g.get(ctx, g.projURL("repository/compare?straight=false&from="+url.QueryEscape(head)+"&to="+url.QueryEscape(base)), &back); err == nil {
		out.Behind = len(back.Commits)
	}
	if mrs, _, err := g.Pulls(ctx, pullQuery{State: "open", Head: head, Base: base, Page: 1, Limit: 1}); err == nil && len(mrs) > 0 {
		out.OpenPull = &PullLink{Number: mrs[0].Number, Title: mrs[0].Title}
	}
	return out, nil
}

func (g *gitlabSource) Checks(ctx context.Context, sha string) (*Checks, error) {
	var list []struct {
		Name         string  `json:"name"`
		Status       string  `json:"status"`
		Description  *string `json:"description"`
		TargetURL    *string `json:"target_url"`
		AllowFailure bool    `json:"allow_failure"`
	}
	if _, err := g.get(ctx, g.projURL("repository/commits/"+url.PathEscape(sha)+"/statuses?per_page=100"), &list); err != nil && platformStatus(err) != http.StatusNotFound {
		return nil, err
	}
	out := &Checks{SHA: sha, Items: []Check{}}
	for _, st := range list {
		c := Check{Name: st.Name, AllowFailure: st.AllowFailure}
		c.Status = map[string]string{"success": "success", "failed": "failure", "running": "in_progress", "pending": "queued", "created": "queued",
			"waiting_for_resource": "queued", "preparing": "queued", "scheduled": "queued", "canceled": "cancelled", "skipped": "skipped", "manual": "neutral"}[st.Status]
		if c.Status == "" {
			c.Status = "neutral"
		}
		if st.Description != nil {
			c.Description = *st.Description
		}
		if st.TargetURL != nil {
			c.URL = *st.TargetURL
		}
		out.Items = append(out.Items, c)
	}
	out.State = overallState(out.Items)
	return out, nil
}

// ---- writing ---------------------------------------------------------------------

// CommitChanges writes one commit through the commits API. Each touched
// file carries last_commit_id as of the commit the edit started from, so
// GitLab refuses it if that file changed on the branch since.
func (g *gitlabSource) CommitChanges(ctx context.Context, in remoteCommit) (*CommitResult, error) {
	p, err := g.project(ctx)
	if err != nil {
		return nil, err
	}
	if err := checkCommitInput(&in.CommitInput, p.DefaultBranch); err != nil {
		return nil, err
	}
	base := in.ExpectedHead
	if base == "" {
		at, err := g.Resolve(ctx, in.Branch)
		if err != nil {
			return nil, err
		}
		base = at.SHA
	}
	at := resolved{Ref: base, SHA: base}
	actions := []map[string]any{}
	for _, c := range in.Changes {
		a := map[string]any{"file_path": c.Path}
		switch c.Op {
		case "delete":
			last, ok, err := g.lastCommitOf(ctx, c.Path, base)
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, httpx.NotFound(c.Path + " doesn't exist on " + in.Branch + ".")
			}
			a["action"], a["last_commit_id"] = "delete", last
		case "move":
			last, ok, err := g.lastCommitOf(ctx, c.From, base)
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, httpx.NotFound(c.From + " doesn't exist on " + in.Branch + ".")
			}
			a["action"], a["previous_path"], a["last_commit_id"] = "move", c.From, last
		default:
			last, existed, err := g.lastCommitOf(ctx, c.Path, base)
			if err != nil {
				return nil, err
			}
			var old []byte
			if existed && c.Op == "replace_lines" {
				if _, old, err = g.File(ctx, at, c.Path, 50<<20); err != nil {
					return nil, err
				}
			}
			data, err := changeContent(c, old, existed)
			if err != nil {
				return nil, err
			}
			a["action"] = "create"
			if existed {
				a["action"], a["last_commit_id"] = "update", last
			}
			a["content"], a["encoding"] = base64.StdEncoding.EncodeToString(data), "base64"
			if c.Executable != nil {
				a["execute_filemode"] = *c.Executable
			}
		}
		actions = append(actions, a)
	}
	body := map[string]any{"branch": in.Branch, "commit_message": agentTrailer(in.Message, in.agent), "actions": actions}
	target := in.Branch
	if in.NewBranch != "" {
		target = in.NewBranch
		body["branch"], body["start_sha"] = target, base
	}
	var c glCommit
	if _, err := g.send(ctx, http.MethodPost, g.projURL("repository/commits"), body, &c); err != nil {
		return nil, g.writeErr(err, target)
	}
	g.forgetMutable()
	return &CommitResult{Commit: glToCommit(c), Branch: target, SuggestPull: target != p.DefaultBranch}, nil
}

func (g *gitlabSource) writeErr(err error, branch string) error {
	pe := asPlatform(err)
	if pe == nil {
		return err
	}
	msg := strings.ToLower(pe.Message)
	switch {
	case pe.Status == http.StatusForbidden && (strings.Contains(msg, "push") || strings.Contains(msg, "protected")):
		return errBranchProtected("GitLab", branch)
	case strings.Contains(msg, "changed since") || strings.Contains(msg, "has changed") || strings.Contains(msg, "last_commit_id"):
		return errHeadMoved(branch)
	case strings.Contains(msg, "already exists") && strings.Contains(msg, "branch"):
		return httpx.NewError(http.StatusConflict, "branch_exists", "A branch named "+branch+" already exists.")
	}
	return err
}

func (g *gitlabSource) CreateBranch(ctx context.Context, name, from string) (*RefView, error) {
	if !validBranchName(name) {
		return nil, httpx.BadRequest(name + " isn't a valid branch name.")
	}
	at, err := g.Resolve(ctx, from)
	if err != nil {
		return nil, err
	}
	if _, err := g.send(ctx, http.MethodPost, g.projURL("repository/branches"), map[string]string{"branch": name, "ref": at.SHA}, nil); err != nil {
		if pe := asPlatform(err); pe != nil && pe.Status == http.StatusBadRequest {
			return nil, httpx.NewError(http.StatusConflict, "branch_exists", "A branch named "+name+" already exists.")
		}
		return nil, err
	}
	g.forgetMutable()
	return &RefView{Ref: Ref{Name: name, Kind: "branch", SHA: at.SHA}}, nil
}

func (g *gitlabSource) DeleteBranch(ctx context.Context, name string) error {
	p, err := g.project(ctx)
	if err != nil {
		return err
	}
	if name == p.DefaultBranch {
		return httpx.NewError(http.StatusForbidden, "branch_protected", "The default branch can't be deleted.")
	}
	if _, err := g.send(ctx, http.MethodDelete, g.projURL("repository/branches/"+url.PathEscape(name)), nil, nil); err != nil {
		return g.writeErr(err, name)
	}
	g.forgetMutable()
	return nil
}

// ---- merge requests --------------------------------------------------------------

type glApprovals struct {
	ApprovalsRequired int `json:"approvals_required"`
	ApprovalsLeft     int `json:"approvals_left"`
	ApprovedBy        []struct {
		User glUser `json:"user"`
	} `json:"approved_by"`
}

func (g *gitlabSource) toPull(m glMR, lvl string, appr *glApprovals) XPull {
	out := XPull{ID: "gitlab:" + strconv.FormatInt(m.ID, 10), RepoID: g.id.String(), Number: m.IID, Title: m.Title, HeadBranch: m.SourceBranch, BaseBranch: m.TargetBranch,
		HeadSHA: m.SHA, Author: g.user(m.Author), MergedAt: m.MergedAt, ClosedAt: m.ClosedAt, ClosedBy: g.user(m.ClosedBy), CommentCount: m.Notes,
		Reviewers: []XReviewer{}, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt, Draft: m.Draft || m.WorkInProg, WebURL: m.WebURL, IsAuthor: g.isViewer(m.Author)}
	out.Source = g.source(m.WebURL)
	if m.Description != nil {
		out.Body = *m.Description
	}
	if m.DiffRefs != nil {
		out.BaseSHA = m.DiffRefs.BaseSHA
		if m.DiffRefs.HeadSHA != "" {
			out.HeadSHA = m.DiffRefs.HeadSHA
		}
	}
	switch m.State {
	case "merged":
		out.State = "merged"
		out.MergedBy = g.user(m.MergedBy)
		if out.MergedBy == nil {
			out.MergedBy = g.user(m.MergeUser)
		}
		out.MergeCommitSHA = m.MergeSHA
		if out.MergeCommitSHA == nil {
			out.MergeCommitSHA = m.SquashSHA
		}
	case "closed":
		out.State = "closed"
	default:
		out.State = "open"
	}
	out.CanEdit = g.canWrite && (levelAtLeast(lvl, "edit") || out.IsAuthor)
	approved := map[int64]bool{}
	if appr != nil {
		for _, a := range appr.ApprovedBy {
			approved[a.User.ID] = true
		}
		out.Approvals = len(appr.ApprovedBy)
	}
	seen := map[int64]bool{}
	for _, u := range m.Reviewers {
		u := u
		rv := XReviewer{User: *g.user(&u), Requested: true}
		if approved[u.ID] {
			rv.State = "approved"
		}
		seen[u.ID] = true
		out.Reviewers = append(out.Reviewers, rv)
	}
	if appr != nil {
		for _, a := range appr.ApprovedBy {
			a := a
			if !seen[a.User.ID] {
				out.Reviewers = append(out.Reviewers, XReviewer{User: *g.user(&a.User), State: "approved"})
			}
		}
	}
	return out
}

func (g *gitlabSource) Pulls(ctx context.Context, q pullQuery) ([]XPull, bool, error) {
	p, err := g.project(ctx)
	if err != nil {
		return nil, false, err
	}
	lvl := g.level(p)
	limit := q.Limit
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	state := map[string]string{"": "opened", "open": "opened", "closed": "all", "merged": "merged", "all": "all"}[q.State]
	if state == "" {
		return nil, false, httpx.BadRequest("state is open, closed, merged or all.")
	}
	u := g.projURL(fmt.Sprintf("merge_requests?state=%s&order_by=updated_at&sort=desc&per_page=%d&page=%d", state, limit, max(q.Page, 1)))
	if q.Q != "" {
		u += "&search=" + url.QueryEscape(q.Q) + "&in=title"
	}
	if q.Author {
		u += "&author_username=" + url.QueryEscape(g.viewer.Login)
	}
	if q.ReviewRequired {
		u += "&reviewer_username=" + url.QueryEscape(g.viewer.Login)
	}
	if q.Head != "" {
		u += "&source_branch=" + url.QueryEscape(q.Head)
	}
	if q.Base != "" {
		u += "&target_branch=" + url.QueryEscape(q.Base)
	}
	var list []glMR
	h, err := g.get(ctx, u, &list)
	if err != nil {
		return nil, false, err
	}
	out := make([]XPull, 0, len(list))
	for _, m := range list {
		x := g.toPull(m, lvl, nil)
		if q.State == "closed" && x.State == "open" { // "closed" here means closed or merged
			continue
		}
		out = append(out, x)
	}
	return out, nextPage(h) > 0, nil
}

func (g *gitlabSource) rawMR(ctx context.Context, n int) (*glMR, error) {
	var m glMR
	if _, err := g.get(ctx, g.projURL(fmt.Sprintf("merge_requests/%d", n)), &m); err != nil {
		if platformStatus(err) == http.StatusNotFound {
			return nil, httpx.NotFound(fmt.Sprintf("There's no merge request !%d.", n))
		}
		return nil, err
	}
	return &m, nil
}

func (g *gitlabSource) approvals(ctx context.Context, n int) (*glApprovals, error) {
	var a glApprovals
	if _, err := g.get(ctx, g.projURL(fmt.Sprintf("merge_requests/%d/approvals", n)), &a); err != nil {
		if platformStatus(err) == http.StatusNotFound || platformStatus(err) == http.StatusForbidden {
			return &glApprovals{}, nil // approvals aren't available on this tier
		}
		return nil, err
	}
	return &a, nil
}

func (g *gitlabSource) Pull(ctx context.Context, n int) (*XPull, error) {
	p, err := g.project(ctx)
	if err != nil {
		return nil, err
	}
	m, err := g.rawMR(ctx, n)
	if err != nil {
		return nil, err
	}
	a, err := g.approvals(ctx, n)
	if err != nil {
		return nil, err
	}
	x := g.toPull(*m, g.level(p), a)
	return &x, nil
}

func (g *gitlabSource) OpenPull(ctx context.Context, in OpenPullInput) (*XPull, error) {
	p, err := g.project(ctx)
	if err != nil {
		return nil, err
	}
	title, err := cleanTitle(in.Title)
	if err != nil {
		return nil, err
	}
	base := in.Base
	if base == "" {
		base = p.DefaultBranch
	}
	var m glMR
	if _, err := g.send(ctx, http.MethodPost, g.projURL("merge_requests"), map[string]any{"source_branch": in.Head, "target_branch": base, "title": title, "description": in.Body}, &m); err != nil {
		if pe := asPlatform(err); pe != nil && pe.Status == http.StatusConflict {
			return nil, httpx.NewError(http.StatusConflict, "pull_exists", "GitLab: "+orDefault(pe.Message, "a merge request for this branch is already open."))
		}
		return nil, err
	}
	g.forgetMutable()
	x := g.toPull(m, g.level(p), nil)
	return &x, nil
}

func (g *gitlabSource) UpdatePull(ctx context.Context, n int, in updatePullReq) (*XPull, error) {
	body := map[string]string{}
	if in.Title != nil {
		t, err := cleanTitle(*in.Title)
		if err != nil {
			return nil, err
		}
		body["title"] = t
	}
	if in.Body != nil {
		body["description"] = *in.Body
	}
	if in.Base != nil {
		body["target_branch"] = *in.Base
	}
	if _, err := g.send(ctx, http.MethodPut, g.projURL(fmt.Sprintf("merge_requests/%d", n)), body, nil); err != nil {
		return nil, err
	}
	return g.Pull(ctx, n)
}

func (g *gitlabSource) SetPullOpen(ctx context.Context, n int, open bool) (*XPull, error) {
	ev := "close"
	if open {
		ev = "reopen"
	}
	if _, err := g.send(ctx, http.MethodPut, g.projURL(fmt.Sprintf("merge_requests/%d", n)), map[string]string{"state_event": ev}, nil); err != nil {
		return nil, err
	}
	g.forgetMutable()
	return g.Pull(ctx, n)
}

func (g *gitlabSource) PullCommits(ctx context.Context, n int) ([]Commit, error) {
	var list []glCommit
	if _, err := g.get(ctx, g.projURL(fmt.Sprintf("merge_requests/%d/commits?per_page=100", n)), &list); err != nil {
		return nil, err
	}
	out := make([]Commit, 0, len(list))
	for _, c := range list {
		out = append(out, glToCommit(c))
	}
	sortCommitsOldestFirst(out)
	return out, nil
}

func (g *gitlabSource) PullDiff(ctx context.Context, n int, p string) (*XPullDiff, error) {
	m, err := g.rawMR(ctx, n)
	if err != nil {
		return nil, err
	}
	diffs, truncated, err := g.diffs(ctx, g.projURL(fmt.Sprintf("merge_requests/%d/diffs", n)), 10)
	if err != nil {
		return nil, err
	}
	out := &XPullDiff{Head: m.SHA, Diff: buildDiff(glPatchFiles(diffs), truncated, p)}
	if m.DiffRefs != nil {
		out.Base, out.Head = m.DiffRefs.BaseSHA, m.DiffRefs.HeadSHA
	}
	return out, nil
}

func (g *gitlabSource) discussions(ctx context.Context, n int) ([]glDiscussion, error) {
	var all []glDiscussion
	for page := 1; page > 0 && page <= 5; {
		var list []glDiscussion
		h, err := g.get(ctx, g.projURL(fmt.Sprintf("merge_requests/%d/discussions?per_page=100&page=%d", n, page)), &list)
		if err != nil {
			return nil, err
		}
		all = append(all, list...)
		page = nextPage(h)
	}
	return all, nil
}

// Comment ids carry the discussion: "<discussion id>_<note id>".
func glCommentID(disc string, note int64) string { return disc + "_" + strconv.FormatInt(note, 10) }

func splitGLCommentID(id string) (string, string, bool) {
	d, n, ok := strings.Cut(id, "_")
	if !ok || d == "" {
		return "", "", false
	}
	if _, err := strconv.ParseInt(n, 10, 64); err != nil {
		return "", "", false
	}
	return d, n, true
}

func (g *gitlabSource) note(disc string, n glNote, root string) XComment {
	x := XComment{ID: glCommentID(disc, n.ID), Author: g.user(n.Author), Body: n.Body, CreatedAt: n.CreatedAt, CanEdit: g.canWrite && g.isViewer(n.Author)}
	if n.Position != nil {
		path, side, line := n.Position.NewPath, "new", n.Position.NewLine
		if line == nil {
			path, side, line = n.Position.OldPath, "old", n.Position.OldLine
		}
		x.Path, x.Side, x.Line, x.CommitSHA = ptr(path), ptr(side), line, ptr(n.Position.HeadSHA)
	}
	if root != "" && root != x.ID {
		x.ReplyTo = ptr(root)
	}
	if n.Resolved {
		at := n.UpdatedAt
		if n.ResolvedAt != nil {
			at = *n.ResolvedAt
		}
		x.ResolvedAt, x.ResolvedBy = ptr(at), g.user(n.ResolvedBy)
	}
	if n.UpdatedAt.After(n.CreatedAt.Add(time.Second)) && !n.Resolved {
		x.EditedAt = ptr(n.UpdatedAt)
	}
	return x
}

func (g *gitlabSource) Threads(ctx context.Context, n int) ([]XThread, string, error) {
	m, err := g.rawMR(ctx, n)
	if err != nil {
		return nil, "", err
	}
	list, err := g.discussions(ctx, n)
	if err != nil {
		return nil, "", err
	}
	head := m.SHA
	if m.DiffRefs != nil {
		head = m.DiffRefs.HeadSHA
	}
	out := []XThread{}
	for _, d := range list {
		if len(d.Notes) == 0 || d.Notes[0].Position == nil || d.Notes[0].System {
			continue
		}
		rootID := glCommentID(d.ID, d.Notes[0].ID)
		t := XThread{Root: g.note(d.ID, d.Notes[0], ""), Replies: []XComment{}, Outdated: d.Notes[0].Position.HeadSHA != head}
		for _, nt := range d.Notes[1:] {
			t.Replies = append(t.Replies, g.note(d.ID, nt, rootID))
		}
		out = append(out, t)
	}
	return out, head, nil
}

var (
	glAddedCommits = regexp.MustCompile(`^added (\d+) (new )?commits?`)
	glTitleChange  = regexp.MustCompile(`^changed title from \*\*(.*)\*\* to \*\*(.*)\*\*`)
	glTargetChange = regexp.MustCompile("^changed target branch from `(.*)` to `(.*)`")
)

func (g *gitlabSource) Timeline(ctx context.Context, n int) ([]XTimelineItem, error) {
	m, err := g.rawMR(ctx, n)
	if err != nil {
		return nil, err
	}
	list, err := g.discussions(ctx, n)
	if err != nil {
		return nil, err
	}
	items := []XTimelineItem{{Type: "event", At: m.CreatedAt, Event: &XEvent{ID: "opened", Kind: "opened", Actor: g.user(m.Author), Data: map[string]any{}, CreatedAt: m.CreatedAt}}}
	for _, d := range list {
		if len(d.Notes) == 0 {
			continue
		}
		first := d.Notes[0]
		switch {
		case first.System:
			for _, nt := range d.Notes {
				if it, ok := g.systemItem(nt, m); ok {
					items = append(items, it)
				}
			}
		case first.Position != nil:
			root := g.note(d.ID, first, "")
			ts := XThreadSummary{ID: root.ID, Path: *root.Path, Side: *root.Side, Body: first.Body, Replies: len(d.Notes) - 1, Author: root.Author}
			if root.Line != nil {
				ts.Line = *root.Line
			}
			items = append(items, XTimelineItem{Type: "thread", At: first.CreatedAt, Thread: &ts})
		default:
			rootID := glCommentID(d.ID, first.ID)
			for i, nt := range d.Notes {
				root := rootID
				if i == 0 {
					root = ""
				}
				x := g.note(d.ID, nt, root)
				items = append(items, XTimelineItem{Type: "comment", At: nt.CreatedAt, Comment: &x})
			}
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].At.Before(items[j].At) })
	return items, nil
}

// systemItem reads one of GitLab's system notes as a timeline item.
func (g *gitlabSource) systemItem(nt glNote, m *glMR) (XTimelineItem, bool) {
	body := strings.TrimSpace(nt.Body)
	ev := &XEvent{ID: "gitlab:" + strconv.FormatInt(nt.ID, 10), Actor: g.user(nt.Author), Data: map[string]any{}, CreatedAt: nt.CreatedAt}
	switch {
	case body == "approved this merge request":
		return XTimelineItem{Type: "review", At: nt.CreatedAt, Review: &XReview{ID: ev.ID, Author: ev.Actor, State: "approved", CommitSHA: m.SHA, CreatedAt: nt.CreatedAt}}, true
	case strings.HasPrefix(body, "requested review from"):
		ev.Kind = "review_requested"
	case strings.HasPrefix(body, "removed review request"):
		ev.Kind = "review_request_removed"
	case glAddedCommits.MatchString(body):
		ev.Kind = "pushed"
		num, _ := strconv.Atoi(glAddedCommits.FindStringSubmatch(body)[1])
		ev.Data["commits"] = num
		ev.Data["after"] = m.SHA
	case body == "closed":
		ev.Kind = "closed"
	case body == "reopened":
		ev.Kind = "reopened"
	case strings.HasPrefix(body, "merged"):
		ev.Kind = "merged"
		ev.Data["method"] = "merge"
		if m.MergeSHA != nil {
			ev.Data["commit"] = *m.MergeSHA
		}
	case glTitleChange.MatchString(body):
		sm := glTitleChange.FindStringSubmatch(body)
		ev.Kind, ev.Data["from"], ev.Data["to"] = "retitled", sm[1], sm[2]
	case glTargetChange.MatchString(body):
		sm := glTargetChange.FindStringSubmatch(body)
		ev.Kind, ev.Data["from"], ev.Data["to"] = "base_changed", sm[1], sm[2]
	case strings.HasPrefix(body, "deleted the source branch") || strings.HasPrefix(body, "deleted source branch"):
		ev.Kind, ev.Data["branch"], ev.Data["after_merge"] = "branch_deleted", m.SourceBranch, m.State == "merged"
	default:
		return XTimelineItem{}, false
	}
	return XTimelineItem{Type: "event", At: nt.CreatedAt, Event: ev}, true
}

func (g *gitlabSource) Mergeability(ctx context.Context, n int) (*Mergeability, error) {
	p, err := g.project(ctx)
	if err != nil {
		return nil, err
	}
	lvl := g.level(p)
	mr, err := g.rawMR(ctx, n)
	if err != nil {
		return nil, err
	}
	a, err := g.approvals(ctx, n)
	if err != nil {
		return nil, err
	}
	x := g.toPull(*mr, lvl, a)
	m := &Mergeability{Conflicts: []string{}, Blockers: []string{}, Methods: []string{}, Approvals: x.Approvals, RequiredApprovals: a.ApprovalsRequired,
		HeadSHA: x.HeadSHA, BaseSHA: x.BaseSHA, DeleteBranch: p.RemoveSourceBranch || (mr.ForceRemove != nil && *mr.ForceRemove),
		CanDeleteBranch: g.canWrite && levelAtLeast(lvl, "edit")}
	if p.SquashOption != "always" {
		m.Methods = append(m.Methods, "merge")
	}
	if p.SquashOption != "never" {
		m.Methods = append(m.Methods, "squash")
	}
	status := mr.Detailed
	if status == "" {
		status = map[string]string{"can_be_merged": "mergeable", "cannot_be_merged": "conflict", "checking": "checking", "unchecked": "unchecked"}[mr.MergeStatus]
	}
	switch {
	case x.State == "merged":
		m.State = "merged"
	case x.State == "closed":
		m.State = "closed"
	case status == "mergeable":
		m.State, m.Mergeable = "clean", true
	case status == "conflict" || mr.HasConflicts:
		m.State = "conflicts"
		m.Blockers = append(m.Blockers, "This branch has conflicts with "+mr.TargetBranch+". Resolve them on GitLab or locally.")
	default:
		m.State = "blocked"
		msg := map[string]string{
			"not_approved":             fmt.Sprintf("It needs %d more %s.", a.ApprovalsLeft, plural(a.ApprovalsLeft, "approval", "approvals")),
			"ci_must_pass":             "The pipeline must succeed first.",
			"ci_still_running":         "The pipeline is still running.",
			"discussions_not_resolved": "Resolve every thread first.",
			"draft_status":             "This is a draft. Mark it ready on GitLab to merge.",
			"need_rebase":              "It needs a rebase onto " + mr.TargetBranch + ".",
			"blocked_status":           "Another merge request blocks it.",
			"jira_association_missing": "It needs a Jira issue key.",
			"external_status_checks":   "External status checks must pass.",
			"not_open":                 "It isn't open.",
		}[status]
		if msg == "" {
			msg = "GitLab is still checking whether this can merge. Refresh in a moment."
		}
		if status == "not_approved" && a.ApprovalsLeft == 0 {
			msg = "It needs approval."
		}
		m.Blockers = append(m.Blockers, msg)
	}
	if !g.canWrite {
		m.Blockers = append(m.Blockers, "Turn on changes for GitLab in Settings → Connections to merge from here.")
	} else if !levelAtLeast(lvl, "edit") {
		m.Blockers = append(m.Blockers, "This GitLab account can't merge in "+p.PathWithNamespace+".")
	}
	m.CanMerge = m.Mergeable && g.canWrite && levelAtLeast(lvl, "edit") && len(m.Methods) > 0
	return m, nil
}

func (g *gitlabSource) Merge(ctx context.Context, n int, in mergeReq) (*XPull, error) {
	body := map[string]any{"squash": in.Method == "squash", "should_remove_source_branch": in.DeleteBranch}
	if in.ExpectedHead != "" {
		body["sha"] = in.ExpectedHead
	}
	msg := strings.TrimSpace(strings.TrimSpace(in.Title) + "\n\n" + strings.TrimSpace(in.Message))
	if msg != "" {
		if in.Method == "squash" {
			body["squash_commit_message"] = msg
		} else {
			body["merge_commit_message"] = msg
		}
	}
	if _, err := g.send(ctx, http.MethodPut, g.projURL(fmt.Sprintf("merge_requests/%d/merge", n)), body, nil); err != nil {
		switch pe := asPlatform(err); {
		case pe == nil:
		case pe.Status == http.StatusMethodNotAllowed || pe.Status == http.StatusNotAcceptable || pe.Status == http.StatusUnprocessableEntity:
			return nil, httpx.NewError(http.StatusConflict, "not_mergeable", "GitLab won't merge this yet: "+orDefault(pe.Message, "it isn't mergeable."))
		case pe.Status == http.StatusConflict:
			return nil, httpx.NewError(http.StatusConflict, "head_moved", "New commits arrived on the source branch. Review them, then merge.")
		case pe.Status == http.StatusUnauthorized: // GitLab's answer for "not allowed to merge"
			return nil, httpx.NewError(http.StatusForbidden, "forbidden", "GitLab doesn't let this account merge here.")
		}
		return nil, err
	}
	g.forgetMutable()
	return g.Pull(ctx, n)
}

// diffLine finds where a line sits in the merge request's diff, for
// GitLab's positions: an unchanged line needs both its old and new number.
func (g *gitlabSource) diffLine(ctx context.Context, n int, p string, line int, side string) (oldLine, newLine *int, oldPath string, err error) {
	d, err := g.PullDiff(ctx, n, p)
	if err != nil {
		return nil, nil, "", err
	}
	for _, f := range d.Diff.Files {
		if f.Path != p {
			continue
		}
		for _, h := range f.Hunks {
			for _, l := range h.Lines {
				match := (side == "old" && l.Old == line && l.Type != "add") || (side != "old" && l.New == line && l.Type != "del")
				if !match {
					continue
				}
				switch l.Type {
				case "add":
					return nil, ptr(l.New), f.OldPath, nil
				case "del":
					return ptr(l.Old), nil, f.OldPath, nil
				case "context":
					return ptr(l.Old), ptr(l.New), f.OldPath, nil
				}
			}
		}
		return nil, nil, "", httpx.BadRequest(fmt.Sprintf("Line %d of %s isn't in this merge request's changes.", line, p))
	}
	return nil, nil, "", httpx.BadRequest(p + " isn't changed in this merge request.")
}

func (g *gitlabSource) Comment(ctx context.Context, n int, in xCommentInput) (*XComment, error) {
	body, err := cleanBody(in.Body)
	if err != nil {
		return nil, err
	}
	switch {
	case in.ReplyTo != "":
		disc, _, ok := splitGLCommentID(in.ReplyTo)
		if !ok {
			return nil, httpx.NotFound("That comment doesn't exist anymore.")
		}
		var nt glNote
		if _, err := g.send(ctx, http.MethodPost, g.projURL(fmt.Sprintf("merge_requests/%d/discussions/%s/notes", n, url.PathEscape(disc))), map[string]string{"body": body}, &nt); err != nil {
			return nil, err
		}
		x := g.note(disc, nt, in.ReplyTo)
		return &x, nil
	case in.Path != "":
		mr, err := g.rawMR(ctx, n)
		if err != nil {
			return nil, err
		}
		if mr.DiffRefs == nil {
			return nil, httpx.Conflict("GitLab hasn't prepared this merge request's diff yet. Try again in a moment.")
		}
		oldLine, newLine, oldPath, err := g.diffLine(ctx, n, in.Path, in.Line, in.Side)
		if err != nil {
			return nil, err
		}
		pos := map[string]any{"position_type": "text", "base_sha": mr.DiffRefs.BaseSHA, "start_sha": mr.DiffRefs.StartSHA, "head_sha": mr.DiffRefs.HeadSHA,
			"old_path": oldPath, "new_path": in.Path}
		if oldLine != nil {
			pos["old_line"] = *oldLine
		}
		if newLine != nil {
			pos["new_line"] = *newLine
		}
		var d glDiscussion
		if _, err := g.send(ctx, http.MethodPost, g.projURL(fmt.Sprintf("merge_requests/%d/discussions", n)), map[string]any{"body": body, "position": pos}, &d); err != nil {
			return nil, err
		}
		if len(d.Notes) == 0 {
			return &XComment{ID: d.ID, Body: body, CreatedAt: time.Now()}, nil
		}
		x := g.note(d.ID, d.Notes[0], "")
		return &x, nil
	default:
		var d glDiscussion
		if _, err := g.send(ctx, http.MethodPost, g.projURL(fmt.Sprintf("merge_requests/%d/discussions", n)), map[string]string{"body": body}, &d); err != nil {
			return nil, err
		}
		if len(d.Notes) == 0 {
			return &XComment{ID: d.ID, Body: body, CreatedAt: time.Now()}, nil
		}
		x := g.note(d.ID, d.Notes[0], "")
		return &x, nil
	}
}

func (g *gitlabSource) EditComment(ctx context.Context, n int, id, body string) (*XComment, error) {
	body, err := cleanBody(body)
	if err != nil {
		return nil, err
	}
	disc, note, ok := splitGLCommentID(id)
	if !ok {
		return nil, httpx.NotFound("That comment doesn't exist.")
	}
	var nt glNote
	if _, err := g.send(ctx, http.MethodPut, g.projURL(fmt.Sprintf("merge_requests/%d/notes/%s", n, note)), map[string]string{"body": body}, &nt); err != nil {
		return nil, err
	}
	x := g.note(disc, nt, "")
	return &x, nil
}

func (g *gitlabSource) DeleteComment(ctx context.Context, n int, id string) error {
	_, note, ok := splitGLCommentID(id)
	if !ok {
		return httpx.NotFound("That comment doesn't exist.")
	}
	_, err := g.send(ctx, http.MethodDelete, g.projURL(fmt.Sprintf("merge_requests/%d/notes/%s", n, note)), nil, nil)
	return err
}

func (g *gitlabSource) ResolveThread(ctx context.Context, n int, id string, resolved bool) error {
	disc, _, ok := splitGLCommentID(id)
	if !ok {
		return httpx.NotFound("That thread doesn't exist.")
	}
	_, err := g.send(ctx, http.MethodPut, g.projURL(fmt.Sprintf("merge_requests/%d/discussions/%s?resolved=%t", n, url.PathEscape(disc), resolved)), nil, nil)
	return err
}

func (g *gitlabSource) Review(ctx context.Context, n int, in xReviewInput) error {
	switch in.State {
	case "approve":
		mr, err := g.rawMR(ctx, n)
		if err != nil {
			return err
		}
		if _, err := g.send(ctx, http.MethodPost, g.projURL(fmt.Sprintf("merge_requests/%d/approve", n)), map[string]string{"sha": mr.SHA}, nil); err != nil {
			if pe := asPlatform(err); pe != nil && pe.Status == http.StatusUnauthorized {
				return httpx.NewError(http.StatusForbidden, "cannot_approve", "GitLab didn't let this account approve: it's your own merge request, you approved already, or you aren't an eligible approver.")
			}
			if pe := asPlatform(err); pe != nil && pe.Status == http.StatusConflict {
				return httpx.NewError(http.StatusConflict, "head_moved", "New commits arrived. Review them, then approve.")
			}
			return err
		}
	case "request_changes":
		return errNotSupported("GitLab", "\"request changes\" reviews")
	case "comment":
		if strings.TrimSpace(in.Body) == "" && len(in.Comments) == 0 {
			return httpx.BadRequest("Write a comment.")
		}
	default:
		return httpx.BadRequest("state is approve or comment.")
	}
	if strings.TrimSpace(in.Body) != "" {
		if _, err := g.Comment(ctx, n, xCommentInput{Body: in.Body}); err != nil {
			return err
		}
	}
	for _, c := range in.Comments {
		if _, err := g.Comment(ctx, n, c); err != nil {
			return err
		}
	}
	g.forgetMutable()
	return nil
}

// ---- listing (connectedLister) -------------------------------------------------

func (g *gitlabSource) summary(p glProject, canWrite bool) XRepoSummary {
	id, _ := newExtID(ProviderGitLab, g.conn.ID, p.PathWithNamespace)
	a := glAccess(&p)
	s := XRepoSummary{ID: id.String(), Name: p.Name, Slug: p.Path, FullName: p.PathWithNamespace, Owner: p.Namespace.FullPath, DefaultBranch: p.DefaultBranch,
		MyLevel: levelFrom(a >= 40, a >= 30, a >= 20, canWrite), Private: p.Visibility != "public", PushedAt: p.LastActivityAt, Source: g.source(p.WebURL)}
	if p.LastActivityAt != nil {
		s.UpdatedAt = *p.LastActivityAt
	}
	if p.Description != nil {
		s.Description = *p.Description
	}
	return s
}

func (g *gitlabSource) listRepos(ctx context.Context, q, sortBy, owner string, page int) ([]XRepoSummary, int, error) {
	canWrite := g.s.writeGranted(ctx, g.userID, g.conn)
	order, dir := "last_activity_at", "desc"
	if sortBy == "name" {
		order, dir = "name", "asc"
	}
	if owner == "" {
		u := g.api + fmt.Sprintf("/projects?membership=true&simple=false&order_by=%s&sort=%s&per_page=%d&page=%d", order, dir, connectedPerPage, page)
		if q != "" {
			u += "&search=" + url.QueryEscape(q)
		}
		var list []glProject
		h, err := g.get(ctx, u, &list)
		if err != nil {
			return nil, 0, err
		}
		out := make([]XRepoSummary, 0, len(list))
		for _, p := range list {
			out = append(out, g.summary(p, canWrite))
		}
		return out, nextPage(h), nil
	}
	key := "list:" + g.userID.String() + ":" + g.conn.ID + ":" + order + ":" + q
	var all []glProject
	if v, ok := g.s.rcache.get(key); ok {
		all = v.([]glProject)
	} else {
		for p := 1; p > 0 && p <= connectedScanMax; {
			u := g.api + fmt.Sprintf("/projects?membership=true&simple=false&order_by=%s&sort=%s&per_page=100&page=%d", order, dir, p)
			if q != "" {
				u += "&search=" + url.QueryEscape(q)
			}
			var list []glProject
			h, err := g.get(ctx, u, &list)
			if err != nil {
				return nil, 0, err
			}
			all = append(all, list...)
			p = nextPage(h)
		}
		g.s.rcache.set(key, all, remoteListTTL)
	}
	var match []XRepoSummary
	for _, p := range all {
		ns := p.Namespace.FullPath
		if !strings.EqualFold(ns, owner) && !strings.HasPrefix(strings.ToLower(ns), strings.ToLower(owner)+"/") {
			continue
		}
		match = append(match, g.summary(p, canWrite))
	}
	start := (page - 1) * connectedPerPage
	if start >= len(match) {
		return []XRepoSummary{}, 0, nil
	}
	end := min(start+connectedPerPage, len(match))
	next := 0
	if end < len(match) {
		next = page + 1
	}
	return match[start:end], next, nil
}

func (g *gitlabSource) owners(ctx context.Context) ([]string, error) {
	key := "owners:" + g.userID.String() + ":" + g.conn.ID
	if v, ok := g.s.rcache.get(key); ok {
		return v.([]string), nil
	}
	out := []string{g.viewer.Login}
	var groups []struct {
		FullPath string `json:"full_path"`
	}
	if _, err := g.get(ctx, g.api+"/groups?min_access_level=10&per_page=100", &groups); err != nil {
		return out, err
	}
	for _, gr := range groups {
		out = append(out, gr.FullPath)
	}
	g.s.rcache.set(key, out, viewerTTL)
	return out, nil
}
