package repos

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"scaffoldapp/backend/internal/httpx"
)

// githubSource maps GitHub's REST API (v3, 2022-11-28) onto RepoSource.
// Endpoints come from the connection, so GitHub Enterprise Server works
// the same way.
type githubSource struct{ *remoteRepo }

// ---- GitHub shapes ---------------------------------------------------------------

type ghUser struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
}

type ghRepo struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	FullName    string     `json:"full_name"`
	Owner       ghUser     `json:"owner"`
	Private     bool       `json:"private"`
	Description *string    `json:"description"`
	Default     string     `json:"default_branch"`
	HTMLURL     string     `json:"html_url"`
	CloneURL    string     `json:"clone_url"`
	PushedAt    *time.Time `json:"pushed_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	Size        int64      `json:"size"` // KB
	Archived    bool       `json:"archived"`
	Permissions *struct {
		Admin    bool `json:"admin"`
		Maintain bool `json:"maintain"`
		Push     bool `json:"push"`
		Triage   bool `json:"triage"`
		Pull     bool `json:"pull"`
	} `json:"permissions"`
	AllowMergeCommit    *bool `json:"allow_merge_commit"`
	AllowSquashMerge    *bool `json:"allow_squash_merge"`
	AllowRebaseMerge    *bool `json:"allow_rebase_merge"`
	DeleteBranchOnMerge bool  `json:"delete_branch_on_merge"`
}

type ghSig struct {
	Name  string    `json:"name"`
	Email string    `json:"email"`
	Date  time.Time `json:"date"`
}

type ghFile struct {
	Filename         string `json:"filename"`
	PreviousFilename string `json:"previous_filename"`
	Status           string `json:"status"`
	Additions        int    `json:"additions"`
	Deletions        int    `json:"deletions"`
	Patch            string `json:"patch"`
}

type ghCommit struct {
	SHA    string `json:"sha"`
	Commit struct {
		Author    ghSig  `json:"author"`
		Committer ghSig  `json:"committer"`
		Message   string `json:"message"`
	} `json:"commit"`
	Parents []struct {
		SHA string `json:"sha"`
	} `json:"parents"`
	Files []ghFile `json:"files"`
}

type ghPull struct {
	ID       int64      `json:"id"`
	Number   int        `json:"number"`
	Title    string     `json:"title"`
	Body     *string    `json:"body"`
	State    string     `json:"state"`
	Draft    bool       `json:"draft"`
	Merged   bool       `json:"merged"`
	MergedAt *time.Time `json:"merged_at"`
	MergedBy *ghUser    `json:"merged_by"`
	ClosedAt *time.Time `json:"closed_at"`
	MergeSHA *string    `json:"merge_commit_sha"`
	User     *ghUser    `json:"user"`
	Head     struct {
		Ref  string `json:"ref"`
		SHA  string `json:"sha"`
		Repo *struct {
			FullName string `json:"full_name"`
		} `json:"repo"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"base"`
	RequestedReviewers []ghUser  `json:"requested_reviewers"`
	Comments           int       `json:"comments"`
	ReviewComments     int       `json:"review_comments"`
	Mergeable          *bool     `json:"mergeable"`
	MergeableState     string    `json:"mergeable_state"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	HTMLURL            string    `json:"html_url"`
}

type ghReview struct {
	ID          int64     `json:"id"`
	User        *ghUser   `json:"user"`
	Body        string    `json:"body"`
	State       string    `json:"state"`
	CommitID    string    `json:"commit_id"`
	SubmittedAt time.Time `json:"submitted_at"`
}

type ghReviewComment struct {
	ID           int64     `json:"id"`
	ReviewID     *int64    `json:"pull_request_review_id"`
	InReplyTo    *int64    `json:"in_reply_to_id"`
	Path         string    `json:"path"`
	Line         *int      `json:"line"`
	OriginalLine *int      `json:"original_line"`
	Side         string    `json:"side"`
	CommitID     string    `json:"commit_id"`
	Body         string    `json:"body"`
	User         *ghUser   `json:"user"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type ghIssueComment struct {
	ID        int64     `json:"id"`
	Body      string    `json:"body"`
	User      *ghUser   `json:"user"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ---- helpers ---------------------------------------------------------------------

func (g *githubSource) repoURL(sub string) string {
	u := g.api + "/repos/" + encodePath(g.id.Path)
	if sub != "" {
		u += "/" + sub
	}
	return u
}

func (g *githubSource) user(u *ghUser) *XUser {
	if u == nil || u.Login == "" {
		return nil
	}
	return &XUser{ID: "github:" + strconv.FormatInt(u.ID, 10), DisplayName: u.Login, Login: u.Login}
}

func (g *githubSource) isViewer(u *ghUser) bool {
	return u != nil && strconv.FormatInt(u.ID, 10) == g.viewer.ID
}

func ghToCommit(c ghCommit) Commit {
	subject, body := splitMessage(c.Commit.Message)
	out := Commit{SHA: c.SHA, Parents: []string{}, Subject: subject, Body: body,
		Author:    Signature{Name: c.Commit.Author.Name, Email: c.Commit.Author.Email, At: c.Commit.Author.Date},
		Committer: Signature{Name: c.Commit.Committer.Name, Email: c.Commit.Committer.Email, At: c.Commit.Committer.Date}}
	for _, p := range c.Parents {
		out.Parents = append(out.Parents, p.SHA)
	}
	return out
}

func ghPatchFiles(files []ghFile) []patchFile {
	out := make([]patchFile, 0, len(files))
	for _, f := range files {
		status := map[string]string{"added": "added", "removed": "deleted", "modified": "modified", "renamed": "renamed", "copied": "copied", "changed": "type_changed", "unchanged": "modified"}[f.Status]
		pf := patchFile{Path: f.Filename, OldPath: f.PreviousFilename, Status: status, Additions: f.Additions, Deletions: f.Deletions, Patch: f.Patch}
		if pf.OldPath == "" {
			pf.OldPath = pf.Path
		}
		out = append(out, pf)
	}
	return out
}

// repo reads the repository (cached briefly).
func (g *githubSource) repo(ctx context.Context) (*ghRepo, error) {
	key := g.cacheKey("m:repo")
	if v, ok := g.s.rcache.get(key); ok {
		return v.(*ghRepo), nil
	}
	var r ghRepo
	if _, err := g.get(ctx, g.repoURL(""), &r); err != nil {
		return nil, err
	}
	g.s.rcache.set(key, &r, 30*time.Second)
	return &r, nil
}

func (g *githubSource) level(r *ghRepo) string {
	if r.Permissions == nil {
		return "view"
	}
	p := r.Permissions
	return levelFrom(p.Admin, p.Push || p.Maintain, p.Triage || p.Pull, g.canWrite)
}

func boolOr(b *bool, d bool) bool {
	if b == nil {
		return d
	}
	return *b
}

// ---- repository ------------------------------------------------------------------

func (g *githubSource) Info(ctx context.Context) (*XRepoView, error) {
	r, err := g.repo(ctx)
	if err != nil {
		return nil, err
	}
	lvl := g.level(r)
	v := &XRepoView{ID: g.id.String(), Name: r.Name, Slug: r.Name, FullName: r.FullName, DefaultBranch: r.Default,
		CloneURL: r.CloneURL, Link: g.link(""), MyLevel: lvl, CanPush: levelAtLeast(lvl, "edit"), CanManage: false,
		AllowMergeCommit: boolOr(r.AllowMergeCommit, true), AllowSquash: boolOr(r.AllowSquashMerge, true), AllowRebase: boolOr(r.AllowRebaseMerge, true),
		DeleteBranchOnMerge: r.DeleteBranchOnMerge, SizeBytes: r.Size * 1024, PushedAt: r.PushedAt, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
		Private: r.Private, Archived: r.Archived, WebURL: r.HTMLURL, Source: g.source(r.HTMLURL),
		Supports: Supports{Upload: true, RequestChanges: true, Checks: true, TreeCommits: true, Import: true}}
	if r.Description != nil {
		v.Description = *r.Description
	}
	if !g.canWrite {
		v.GrantWriteURL = g.s.grantWriteURL(ctx, g.userID, ProviderGitHub, g.link(""))
	}
	h, err := g.get(ctx, g.repoURL("pulls?state=open&per_page=1"), nil)
	if err == nil {
		if n := lastPage(h); n > 0 {
			v.OpenPulls = n
		} else {
			var one []ghPull
			if _, err := g.get(ctx, g.repoURL("pulls?state=open&per_page=1"), &one); err == nil {
				v.OpenPulls = len(one)
			}
		}
	}
	return v, nil
}

func (g *githubSource) Refs(ctx context.Context, counts bool) ([]RefView, string, error) {
	r, err := g.repo(ctx)
	if err != nil {
		return nil, "", err
	}
	type ghBranch struct {
		Name   string `json:"name"`
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
		Protected bool `json:"protected"`
	}
	var branches []ghBranch
	for page := 1; page <= 3; page++ {
		var list []ghBranch
		h, err := g.get(ctx, g.repoURL(fmt.Sprintf("branches?per_page=100&page=%d", page)), &list)
		if err != nil {
			return nil, "", err
		}
		branches = append(branches, list...)
		if nextPage(h) == 0 {
			break
		}
	}
	var tags []ghBranch
	if _, err := g.get(ctx, g.repoURL("tags?per_page=100"), &tags); err != nil {
		return nil, "", err
	}
	out := make([]RefView, 0, len(branches)+len(tags))
	for _, b := range branches {
		out = append(out, RefView{Ref: Ref{Name: b.Name, Kind: "branch", SHA: b.Commit.SHA}, Default: b.Name == r.Default, Protected: b.Protected})
	}
	for _, t := range tags {
		out = append(out, RefView{Ref: Ref{Name: t.Name, Kind: "tag", SHA: t.Commit.SHA}})
	}
	if counts {
		open := map[string]int{}
		var pulls []ghPull
		if _, err := g.get(ctx, g.repoURL("pulls?state=open&per_page=100"), &pulls); err == nil {
			for _, p := range pulls {
				open[p.Head.Ref] = p.Number
			}
		}
		defaultSHA := ""
		for _, ref := range out {
			if ref.Default {
				defaultSHA = ref.SHA
			}
		}
		n := min(len(branches), 30)
		parallel(6, n, func(i int) {
			ref := &out[i]
			if c, err := g.commitInfo(ctx, ref.SHA); err == nil {
				ref.Subject, ref.Author, ref.UpdatedAt = c.Subject, c.Author.Name, c.Committer.At
			}
			if num, ok := open[ref.Name]; ok {
				ref.OpenPull = ptr(num)
			}
			if !ref.Default && defaultSHA != "" {
				var cmp struct {
					AheadBy  int `json:"ahead_by"`
					BehindBy int `json:"behind_by"`
				}
				if _, err := g.get(ctx, g.repoURL("compare/"+url.PathEscape(defaultSHA)+"..."+url.PathEscape(ref.SHA)+"?per_page=1"), &cmp); err == nil {
					ref.Ahead, ref.Behind = ptr(cmp.AheadBy), ptr(cmp.BehindBy)
				}
			}
		})
	}
	return out, r.Default, nil
}

// commitInfo reads one commit (cached: commits don't change).
func (g *githubSource) commitInfo(ctx context.Context, sha string) (Commit, error) {
	key := g.cacheKey("c:" + sha)
	if v, ok := g.s.rcache.get(key); ok {
		return v.(Commit), nil
	}
	var c ghCommit
	if _, err := g.get(ctx, g.repoURL("commits/"+url.PathEscape(sha)), &c); err != nil {
		return Commit{}, err
	}
	out := ghToCommit(c)
	g.s.rcache.set(key, out, immutableTTL)
	return out, nil
}

func (g *githubSource) Resolve(ctx context.Context, ref string) (resolved, error) {
	if ref == "" {
		r, err := g.repo(ctx)
		if err != nil {
			return resolved{}, err
		}
		ref = r.Default
	}
	if IsOID(ref) {
		return resolved{Ref: ref, SHA: strings.ToLower(ref)}, nil
	}
	key := g.cacheKey("m:ref:" + ref)
	if v, ok := g.s.rcache.get(key); ok {
		return resolved{Ref: ref, SHA: v.(string)}, nil
	}
	var raw []byte
	_, err := g.do(ctx, http.MethodGet, g.repoURL("commits/"+url.PathEscape(ref)), nil, nil, reqOpts{accept: "application/vnd.github.sha", raw: &raw})
	if s := platformStatus(err); s == http.StatusNotFound || s == http.StatusUnprocessableEntity {
		return resolved{}, httpx.NotFound("There's no branch, tag or commit named " + ref + ".")
	}
	if s := platformStatus(err); s == http.StatusConflict {
		return resolved{}, errEmptyRepo
	}
	if err != nil {
		return resolved{}, err
	}
	sha := strings.TrimSpace(string(raw))
	if !IsOID(sha) { // an answer in JSON after all
		var c ghCommit
		if _, err := g.get(ctx, g.repoURL("commits/"+url.PathEscape(ref)), &c); err != nil {
			return resolved{}, err
		}
		sha = c.SHA
	}
	g.s.rcache.set(key, sha, 10*time.Second)
	return resolved{Ref: ref, SHA: sha}, nil
}

type ghContent struct {
	Type     string `json:"type"` // file | dir | symlink | submodule
	Name     string `json:"name"`
	Path     string `json:"path"`
	SHA      string `json:"sha"`
	Size     int64  `json:"size"`
	Encoding string `json:"encoding"`
	Content  string `json:"content"`
	Target   string `json:"target"`
}

func (c ghContent) entry() TreeEntry {
	typ := map[string]string{"file": "blob", "dir": "tree", "symlink": "symlink", "submodule": "commit"}[c.Type]
	if typ == "" {
		typ = "blob"
	}
	size := c.Size
	if typ != "blob" {
		size = -1
	}
	return TreeEntry{Name: c.Name, Path: c.Path, Type: typ, Mode: entryMode(typ), OID: c.SHA, Size: size}
}

func (g *githubSource) contentsURL(p, ref string) string {
	u := g.repoURL("contents")
	if p != "" {
		u += "/" + encodePath(p)
	}
	return u + "?ref=" + url.QueryEscape(ref)
}

func (g *githubSource) Tree(ctx context.Context, at resolved, dir string) ([]TreeEntry, error) {
	var raw []byte
	if _, err := g.do(ctx, http.MethodGet, g.contentsURL(dir, at.SHA), nil, nil, reqOpts{raw: &raw}); err != nil {
		if platformStatus(err) == http.StatusNotFound {
			return nil, httpx.NotFound("There's no folder at " + dir + " on " + at.Ref + ".")
		}
		return nil, err
	}
	var list []ghContent
	if err := jsonUnmarshal(raw, &list); err != nil {
		return nil, httpx.NewError(http.StatusConflict, "is_file", dir+" is a file.")
	}
	out := make([]TreeEntry, 0, len(list))
	for _, c := range list {
		out = append(out, c.entry())
	}
	sortEntries(out)
	return out, nil
}

func (g *githubSource) TreeCommits(ctx context.Context, at resolved, dir string, names []string) (map[string]Commit, error) {
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
		var list []ghCommit
		if _, err := g.get(ctx, g.repoURL("commits?per_page=1&sha="+url.QueryEscape(at.SHA)+"&path="+url.QueryEscape(p)), &list); err == nil && len(list) > 0 {
			c := ghToCommit(list[0])
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

func (g *githubSource) File(ctx context.Context, at resolved, p string, max int64) (TreeEntry, []byte, error) {
	var raw []byte
	if _, err := g.do(ctx, http.MethodGet, g.contentsURL(p, at.SHA), nil, nil, reqOpts{raw: &raw}); err != nil {
		if platformStatus(err) == http.StatusNotFound {
			return TreeEntry{}, nil, httpx.NotFound("There's no file at " + p + " on " + at.Ref + ".")
		}
		return TreeEntry{}, nil, err
	}
	var c ghContent
	if err := jsonUnmarshal(raw, &c); err != nil || c.Type == "" {
		return TreeEntry{}, nil, httpx.NewError(http.StatusConflict, "is_folder", p+" is a folder.")
	}
	e := c.entry()
	if e.Type != "blob" && e.Type != "symlink" {
		return e, nil, nil
	}
	if e.Type == "symlink" {
		return e, []byte(c.Target), nil
	}
	if c.Size > max {
		return e, nil, nil
	}
	if c.Encoding == "base64" && (c.Content != "" || c.Size == 0) {
		data, err := decodeB64(c.Content)
		return e, data, err
	}
	// 1–100 MB files come without content: read them raw.
	var data []byte
	if _, err := g.do(ctx, http.MethodGet, g.contentsURL(p, at.SHA), nil, nil, reqOpts{accept: "application/vnd.github.raw", raw: &data}); err != nil {
		return e, nil, err
	}
	return e, data, nil
}

func (g *githubSource) Blame(context.Context, resolved, string) ([]BlameRange, error) {
	return nil, errNotSupported("GitHub", "blame")
}

func (g *githubSource) Paths(ctx context.Context, at resolved) ([]string, bool, error) {
	key := g.cacheKey("paths:" + at.SHA)
	type cached struct {
		paths     []string
		truncated bool
	}
	if v, ok := g.s.rcache.get(key); ok {
		c := v.(cached)
		return c.paths, c.truncated, nil
	}
	var commit struct {
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	if _, err := g.get(ctx, g.repoURL("git/commits/"+url.PathEscape(at.SHA)), &commit); err != nil {
		return nil, false, err
	}
	var tree struct {
		Tree []struct {
			Path string `json:"path"`
			Type string `json:"type"`
		} `json:"tree"`
		Truncated bool `json:"truncated"`
	}
	if _, err := g.get(ctx, g.repoURL("git/trees/"+url.PathEscape(commit.Tree.SHA)+"?recursive=1"), &tree); err != nil {
		return nil, false, err
	}
	paths := make([]string, 0, len(tree.Tree))
	for _, t := range tree.Tree {
		if t.Type == "blob" {
			paths = append(paths, t.Path)
		}
	}
	g.s.rcache.set(key, cached{paths, tree.Truncated}, immutableTTL)
	return paths, tree.Truncated, nil
}

// SearchCode uses GitHub code search (the default branch only), then
// reads the matching files to give exact line numbers.
func (g *githubSource) SearchCode(ctx context.Context, q string, at resolved) ([]GrepMatch, bool, error) {
	var res struct {
		Total int `json:"total_count"`
		Items []struct {
			Path string `json:"path"`
			SHA  string `json:"sha"`
		} `json:"items"`
		Incomplete bool `json:"incomplete_results"`
	}
	query := q + " repo:" + g.id.Path
	if _, err := g.do(ctx, http.MethodGet, g.api+"/search/code?per_page=20&q="+url.QueryEscape(query), nil, &res, reqOpts{accept: "application/vnd.github.text-match+json"}); err != nil {
		return nil, false, err
	}
	r, err := g.repo(ctx)
	if err != nil {
		return nil, false, err
	}
	head, err := g.Resolve(ctx, r.Default)
	if err != nil {
		return nil, false, err
	}
	per := make([][]GrepMatch, len(res.Items))
	parallel(6, len(res.Items), func(i int) {
		_, data, err := g.File(ctx, head, res.Items[i].Path, 1<<20)
		if err == nil && data != nil && !isBinary(data) {
			per[i] = grepLines(res.Items[i].Path, string(data), q, maxGrepPerFile)
		}
	})
	var out []GrepMatch
	for _, m := range per {
		out = append(out, m...)
	}
	if len(out) > maxGrepMatches {
		out = out[:maxGrepMatches]
	}
	return out, res.Incomplete || res.Total > len(res.Items), nil
}

func (g *githubSource) Log(ctx context.Context, at resolved, p string, page, limit int) ([]Commit, bool, error) {
	u := g.repoURL(fmt.Sprintf("commits?per_page=%d&page=%d&sha=%s", limit, page, url.QueryEscape(at.SHA)))
	if p != "" {
		u += "&path=" + url.QueryEscape(p)
	}
	var list []ghCommit
	h, err := g.get(ctx, u, &list)
	if err != nil {
		return nil, false, err
	}
	out := make([]Commit, 0, len(list))
	for _, c := range list {
		out = append(out, ghToCommit(c))
	}
	return out, nextPage(h) > 0, nil
}

func (g *githubSource) Commit(ctx context.Context, sha, p string) (*CommitView, error) {
	var c ghCommit
	if _, err := g.get(ctx, g.repoURL("commits/"+url.PathEscape(sha)), &c); err != nil {
		if s := platformStatus(err); s == http.StatusNotFound || s == http.StatusUnprocessableEntity {
			return nil, httpx.NotFound("There's no commit " + sha + ".")
		}
		return nil, err
	}
	return &CommitView{Commit: ghToCommit(c), Diff: buildDiff(ghPatchFiles(c.Files), len(c.Files) >= 300, p)}, nil
}

func (g *githubSource) Compare(ctx context.Context, base, head, p string) (*Comparison, error) {
	if base == "" {
		r, err := g.repo(ctx)
		if err != nil {
			return nil, err
		}
		base = r.Default
	}
	var cmp struct {
		AheadBy   int        `json:"ahead_by"`
		BehindBy  int        `json:"behind_by"`
		MergeBase ghCommit   `json:"merge_base_commit"`
		BaseC     ghCommit   `json:"base_commit"`
		Commits   []ghCommit `json:"commits"`
		Files     []ghFile   `json:"files"`
	}
	if _, err := g.get(ctx, g.repoURL("compare/"+url.PathEscape(base)+"..."+url.PathEscape(head)), &cmp); err != nil {
		if platformStatus(err) == http.StatusNotFound {
			return nil, httpx.NotFound("Both branches must exist to compare them.")
		}
		return nil, err
	}
	out := &Comparison{Base: base, Head: head, BaseSHA: cmp.BaseC.SHA, MergeBase: cmp.MergeBase.SHA, Ahead: cmp.AheadBy, Behind: cmp.BehindBy,
		Commits: []Commit{}, Conflicts: []string{}, Diff: buildDiff(ghPatchFiles(cmp.Files), len(cmp.Files) >= 300, p)}
	for _, c := range cmp.Commits {
		out.Commits = append(out.Commits, ghToCommit(c))
	}
	if n := len(cmp.Commits); n > 0 {
		out.HeadSHA = cmp.Commits[n-1].SHA
	} else if at, err := g.Resolve(ctx, head); err == nil {
		out.HeadSHA = at.SHA
	}
	if pulls, _, err := g.Pulls(ctx, pullQuery{State: "open", Head: head, Base: base, Page: 1, Limit: 1}); err == nil && len(pulls) > 0 {
		out.OpenPull = &PullLink{Number: pulls[0].Number, Title: pulls[0].Title}
	}
	return out, nil
}

func (g *githubSource) Checks(ctx context.Context, sha string) (*Checks, error) {
	var runs struct {
		CheckRuns []struct {
			Name       string  `json:"name"`
			Status     string  `json:"status"`
			Conclusion *string `json:"conclusion"`
			HTMLURL    string  `json:"html_url"`
			DetailsURL string  `json:"details_url"`
			Output     struct {
				Title *string `json:"title"`
			} `json:"output"`
		} `json:"check_runs"`
	}
	if _, err := g.get(ctx, g.repoURL("commits/"+url.PathEscape(sha)+"/check-runs?per_page=100"), &runs); err != nil && platformStatus(err) != http.StatusNotFound {
		return nil, err
	}
	var status struct {
		Statuses []struct {
			Context     string  `json:"context"`
			State       string  `json:"state"`
			Description *string `json:"description"`
			TargetURL   *string `json:"target_url"`
		} `json:"statuses"`
	}
	if _, err := g.get(ctx, g.repoURL("commits/"+url.PathEscape(sha)+"/status"), &status); err != nil && platformStatus(err) != http.StatusNotFound {
		return nil, err
	}
	out := &Checks{SHA: sha, Items: []Check{}}
	for _, r := range runs.CheckRuns {
		c := Check{Name: r.Name, URL: r.HTMLURL}
		if c.URL == "" {
			c.URL = r.DetailsURL
		}
		if r.Output.Title != nil {
			c.Description = *r.Output.Title
		}
		switch {
		case r.Status != "completed":
			c.Status = map[string]string{"queued": "queued", "in_progress": "in_progress"}[r.Status]
			if c.Status == "" {
				c.Status = "queued"
			}
		case r.Conclusion == nil:
			c.Status = "neutral"
		default:
			c.Status = map[string]string{"success": "success", "failure": "failure", "timed_out": "failure", "action_required": "failure",
				"cancelled": "cancelled", "skipped": "skipped", "neutral": "neutral", "stale": "neutral"}[*r.Conclusion]
		}
		out.Items = append(out.Items, c)
	}
	for _, st := range status.Statuses {
		c := Check{Name: st.Context, Status: map[string]string{"success": "success", "pending": "in_progress", "failure": "failure", "error": "failure"}[st.State]}
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

func (g *githubSource) branchHead(ctx context.Context, branch string) (string, error) {
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if _, err := g.get(ctx, g.repoURL("git/ref/heads/"+encodePath(branch)), &ref); err != nil {
		if platformStatus(err) == http.StatusNotFound {
			return "", nil
		}
		return "", err
	}
	return ref.Object.SHA, nil
}

// CommitChanges writes one commit: a single file through the contents API
// (conflicts detected on the file's blob as of the commit the edit started
// from), several files through the git data API (blobs → tree → commit →
// a fast-forward-only ref update from that commit).
func (g *githubSource) CommitChanges(ctx context.Context, in remoteCommit) (*CommitResult, error) {
	r, err := g.repo(ctx)
	if err != nil {
		return nil, err
	}
	if err := checkCommitInput(&in.CommitInput, r.Default); err != nil {
		return nil, err
	}
	message := agentTrailer(in.Message, in.agent)
	head, err := g.branchHead(ctx, in.Branch)
	if err != nil {
		return nil, err
	}
	if head == "" {
		return nil, httpx.NotFound("There's no branch named " + in.Branch + ".")
	}
	base := head
	if in.ExpectedHead != "" {
		base = in.ExpectedHead
	}
	target := in.Branch
	if in.NewBranch != "" {
		target = in.NewBranch
		if _, err := g.send(ctx, http.MethodPost, g.repoURL("git/refs"), map[string]string{"ref": "refs/heads/" + target, "sha": base}, nil); err != nil {
			if platformStatus(err) == http.StatusUnprocessableEntity {
				return nil, httpx.NewError(http.StatusConflict, "branch_exists", "A branch named "+target+" already exists.")
			}
			return nil, err
		}
	}
	at := resolved{Ref: base, SHA: base}
	var commit Commit
	single := len(in.Changes) == 1 && in.Changes[0].Op != "move"
	if single {
		commit, err = g.commitOne(ctx, at, target, message, in.Changes[0])
	} else {
		if in.NewBranch == "" && in.ExpectedHead != "" && head != in.ExpectedHead {
			return nil, errHeadMoved(in.Branch)
		}
		commit, err = g.commitMany(ctx, at, target, message, in.Changes)
	}
	if err != nil {
		if in.NewBranch != "" { // don't leave an empty branch behind
			_, _ = g.send(ctx, http.MethodDelete, g.repoURL("git/refs/heads/"+encodePath(target)), nil, nil)
		}
		return nil, g.writeErr(err, target)
	}
	g.forgetMutable()
	return &CommitResult{Commit: commit, Branch: target, SuggestPull: target != r.Default}, nil
}

func (g *githubSource) writeErr(err error, branch string) error {
	pe := asPlatform(err)
	if pe == nil {
		return err
	}
	msg := strings.ToLower(pe.Message)
	switch {
	case strings.Contains(msg, "protected") || strings.Contains(msg, "rule violation"):
		return errBranchProtected("GitHub", branch)
	case pe.Status == http.StatusConflict || strings.Contains(msg, "fast forward") || strings.Contains(msg, "wasn't supplied") || strings.Contains(msg, "does not match"):
		return errHeadMoved(branch)
	}
	return err
}

func (g *githubSource) commitOne(ctx context.Context, at resolved, branch, message string, c ChangeInput) (Commit, error) {
	e, old, err := g.File(ctx, at, c.Path, 50<<20)
	existed := err == nil && e.Type == "blob"
	if err != nil && !isNotFound(err) {
		return Commit{}, err
	}
	var res struct {
		Commit struct {
			SHA       string `json:"sha"`
			Message   string `json:"message"`
			Author    ghSig  `json:"author"`
			Committer ghSig  `json:"committer"`
			Parents   []struct {
				SHA string `json:"sha"`
			} `json:"parents"`
		} `json:"commit"`
	}
	if c.Op == "delete" {
		if !existed {
			return Commit{}, httpx.NotFound(c.Path + " doesn't exist on " + branch + ".")
		}
		if _, err := g.send(ctx, http.MethodDelete, g.repoURL("contents/"+encodePath(c.Path)), map[string]string{"message": message, "sha": e.OID, "branch": branch}, &res); err != nil {
			return Commit{}, err
		}
	} else {
		data, err := changeContent(c, old, existed)
		if err != nil {
			return Commit{}, err
		}
		body := map[string]string{"message": message, "content": base64.StdEncoding.EncodeToString(data), "branch": branch}
		if existed {
			body["sha"] = e.OID
		}
		if _, err := g.send(ctx, http.MethodPut, g.repoURL("contents/"+encodePath(c.Path)), body, &res); err != nil {
			return Commit{}, err
		}
	}
	subject, bodyText := splitMessage(res.Commit.Message)
	out := Commit{SHA: res.Commit.SHA, Parents: []string{}, Subject: subject, Body: bodyText,
		Author:    Signature{Name: res.Commit.Author.Name, Email: res.Commit.Author.Email, At: res.Commit.Author.Date},
		Committer: Signature{Name: res.Commit.Committer.Name, Email: res.Commit.Committer.Email, At: res.Commit.Committer.Date}}
	for _, p := range res.Commit.Parents {
		out.Parents = append(out.Parents, p.SHA)
	}
	return out, nil
}

func (g *githubSource) commitMany(ctx context.Context, at resolved, branch, message string, changes []ChangeInput) (Commit, error) {
	var parent struct {
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	if _, err := g.get(ctx, g.repoURL("git/commits/"+url.PathEscape(at.SHA)), &parent); err != nil {
		return Commit{}, err
	}
	type entry struct {
		Path string  `json:"path"`
		Mode string  `json:"mode"`
		Type string  `json:"type"`
		SHA  *string `json:"sha"`
	}
	var entries []entry
	blob := func(data []byte) (string, error) {
		var b struct {
			SHA string `json:"sha"`
		}
		_, err := g.send(ctx, http.MethodPost, g.repoURL("git/blobs"), map[string]string{"content": base64.StdEncoding.EncodeToString(data), "encoding": "base64"}, &b)
		return b.SHA, err
	}
	for _, c := range changes {
		mode := "100644"
		if c.Executable != nil && *c.Executable {
			mode = "100755"
		}
		switch c.Op {
		case "delete":
			entries = append(entries, entry{Path: c.Path, Mode: "100644", Type: "blob"})
		case "move":
			e, _, err := g.File(ctx, at, c.From, 0)
			if err != nil {
				return Commit{}, err
			}
			entries = append(entries, entry{Path: c.From, Mode: e.Mode, Type: "blob"}, entry{Path: c.Path, Mode: e.Mode, Type: "blob", SHA: ptr(e.OID)})
		default:
			e, old, err := g.File(ctx, at, c.Path, 50<<20)
			existed := err == nil && e.Type == "blob"
			if err != nil && !isNotFound(err) {
				return Commit{}, err
			}
			data, err := changeContent(c, old, existed)
			if err != nil {
				return Commit{}, err
			}
			sha, err := blob(data)
			if err != nil {
				return Commit{}, err
			}
			if existed && c.Executable == nil {
				mode = e.Mode
			}
			entries = append(entries, entry{Path: c.Path, Mode: mode, Type: "blob", SHA: ptr(sha)})
		}
	}
	var tree struct {
		SHA string `json:"sha"`
	}
	if _, err := g.send(ctx, http.MethodPost, g.repoURL("git/trees"), map[string]any{"base_tree": parent.Tree.SHA, "tree": entries}, &tree); err != nil {
		return Commit{}, err
	}
	var made struct {
		SHA       string `json:"sha"`
		Message   string `json:"message"`
		Author    ghSig  `json:"author"`
		Committer ghSig  `json:"committer"`
	}
	if _, err := g.send(ctx, http.MethodPost, g.repoURL("git/commits"), map[string]any{"message": message, "tree": tree.SHA, "parents": []string{at.SHA}}, &made); err != nil {
		return Commit{}, err
	}
	if _, err := g.send(ctx, http.MethodPatch, g.repoURL("git/refs/heads/"+encodePath(branch)), map[string]any{"sha": made.SHA, "force": false}, nil); err != nil {
		return Commit{}, err
	}
	subject, body := splitMessage(made.Message)
	return Commit{SHA: made.SHA, Parents: []string{at.SHA}, Subject: subject, Body: body,
		Author:    Signature{Name: made.Author.Name, Email: made.Author.Email, At: made.Author.Date},
		Committer: Signature{Name: made.Committer.Name, Email: made.Committer.Email, At: made.Committer.Date}}, nil
}

func isNotFound(err error) bool {
	if e, ok := err.(*httpx.Error); ok {
		return e.Status == http.StatusNotFound
	}
	return platformStatus(err) == http.StatusNotFound
}

func (g *githubSource) CreateBranch(ctx context.Context, name, from string) (*RefView, error) {
	if !validBranchName(name) {
		return nil, httpx.BadRequest(name + " isn't a valid branch name.")
	}
	at, err := g.Resolve(ctx, from)
	if err != nil {
		return nil, err
	}
	if _, err := g.send(ctx, http.MethodPost, g.repoURL("git/refs"), map[string]string{"ref": "refs/heads/" + name, "sha": at.SHA}, nil); err != nil {
		if platformStatus(err) == http.StatusUnprocessableEntity {
			return nil, httpx.NewError(http.StatusConflict, "branch_exists", "A branch named "+name+" already exists.")
		}
		return nil, err
	}
	g.forgetMutable()
	return &RefView{Ref: Ref{Name: name, Kind: "branch", SHA: at.SHA}}, nil
}

func (g *githubSource) DeleteBranch(ctx context.Context, name string) error {
	r, err := g.repo(ctx)
	if err != nil {
		return err
	}
	if name == r.Default {
		return httpx.NewError(http.StatusForbidden, "branch_protected", "The default branch can't be deleted.")
	}
	if _, err := g.send(ctx, http.MethodDelete, g.repoURL("git/refs/heads/"+encodePath(name)), nil, nil); err != nil {
		return g.writeErr(err, name)
	}
	g.forgetMutable()
	return nil
}

// ---- pull requests ---------------------------------------------------------------

func (g *githubSource) toPull(p ghPull, lvl string, reviews []ghReview) XPull {
	out := XPull{ID: "github:" + strconv.FormatInt(p.ID, 10), RepoID: g.id.String(), Number: p.Number, Title: p.Title, State: p.State,
		HeadBranch: p.Head.Ref, BaseBranch: p.Base.Ref, HeadSHA: p.Head.SHA, BaseSHA: p.Base.SHA, Author: g.user(p.User),
		MergedAt: p.MergedAt, ClosedAt: p.ClosedAt, MergedBy: g.user(p.MergedBy), CommentCount: p.Comments + p.ReviewComments,
		Reviewers: []XReviewer{}, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt, Draft: p.Draft, WebURL: p.HTMLURL, IsAuthor: g.isViewer(p.User)}
	out.Source = g.source(p.HTMLURL)
	if p.Body != nil {
		out.Body = *p.Body
	}
	if p.Merged || p.MergedAt != nil {
		out.State = "merged"
		out.MergeCommitSHA = p.MergeSHA
	}
	if out.State == "closed" && p.ClosedAt != nil {
		out.ClosedAt = p.ClosedAt
	}
	out.CanEdit = g.canWrite && (levelAtLeast(lvl, "edit") || out.IsAuthor)
	// Reviewers: requested ones, then everyone's latest decision.
	latest := map[int64]ghReview{}
	order := []int64{}
	for _, rv := range reviews {
		if rv.User == nil || rv.State == "PENDING" {
			continue
		}
		prev, seen := latest[rv.User.ID]
		if !seen {
			order = append(order, rv.User.ID)
		}
		// COMMENTED doesn't replace an earlier decision.
		if !seen || rv.State != "COMMENTED" || prev.State == "COMMENTED" {
			latest[rv.User.ID] = rv
		}
	}
	requested := map[int64]bool{}
	for _, u := range p.RequestedReviewers {
		u := u
		requested[u.ID] = true
		rv := XReviewer{User: *g.user(&u), Requested: true}
		if l, ok := latest[u.ID]; ok {
			rv.State = ghReviewState(l.State)
		}
		out.Reviewers = append(out.Reviewers, rv)
	}
	for _, id := range order {
		l := latest[id]
		switch l.State {
		case "APPROVED":
			out.Approvals++
		case "CHANGES_REQUESTED":
			out.ChangesRequests++
		}
		if requested[id] {
			continue
		}
		out.Reviewers = append(out.Reviewers, XReviewer{User: *g.user(l.User), State: ghReviewState(l.State), Stale: l.State == "DISMISSED"})
	}
	return out
}

func ghReviewState(s string) string {
	return map[string]string{"APPROVED": "approved", "CHANGES_REQUESTED": "changes_requested", "COMMENTED": "commented", "DISMISSED": "approved"}[s]
}

func (g *githubSource) Pulls(ctx context.Context, q pullQuery) ([]XPull, bool, error) {
	r, err := g.repo(ctx)
	if err != nil {
		return nil, false, err
	}
	lvl := g.level(r)
	limit := q.Limit
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	page := max(q.Page, 1)
	if q.Q != "" || q.Author || q.ReviewRequired {
		// Search answers text and people filters.
		terms := []string{"repo:" + g.id.Path, "is:pr"}
		switch q.State {
		case "", "open":
			terms = append(terms, "is:open")
		case "closed":
			terms = append(terms, "is:closed")
		case "merged":
			terms = append(terms, "is:merged")
		}
		if q.Author {
			terms = append(terms, "author:"+g.viewer.Login)
		}
		if q.ReviewRequired {
			terms = append(terms, "review-requested:"+g.viewer.Login)
		}
		if q.Q != "" {
			terms = append(terms, q.Q, "in:title")
		}
		var res struct {
			Items []struct {
				ghPull
				PullRequest *struct {
					MergedAt *time.Time `json:"merged_at"`
				} `json:"pull_request"`
			} `json:"items"`
		}
		h, err := g.get(ctx, g.api+fmt.Sprintf("/search/issues?sort=updated&order=desc&per_page=%d&page=%d&q=%s", limit, page, url.QueryEscape(strings.Join(terms, " "))), &res)
		if err != nil {
			return nil, false, err
		}
		out := make([]XPull, 0, len(res.Items))
		for _, it := range res.Items {
			p := it.ghPull
			if it.PullRequest != nil && it.PullRequest.MergedAt != nil {
				p.MergedAt = it.PullRequest.MergedAt
			}
			out = append(out, g.toPull(p, lvl, nil))
		}
		return out, nextPage(h) > 0, nil
	}
	state := map[string]string{"": "open", "open": "open", "closed": "closed", "merged": "closed", "all": "all"}[q.State]
	if state == "" {
		return nil, false, httpx.BadRequest("state is open, closed, merged or all.")
	}
	u := g.repoURL(fmt.Sprintf("pulls?state=%s&sort=updated&direction=desc&per_page=%d&page=%d", state, limit, page))
	if q.Head != "" {
		u += "&head=" + url.QueryEscape(strings.SplitN(g.id.Path, "/", 2)[0]+":"+q.Head)
	}
	if q.Base != "" {
		u += "&base=" + url.QueryEscape(q.Base)
	}
	var list []ghPull
	h, err := g.get(ctx, u, &list)
	if err != nil {
		return nil, false, err
	}
	out := make([]XPull, 0, len(list))
	for _, p := range list {
		x := g.toPull(p, lvl, nil)
		if q.State == "merged" && x.State != "merged" {
			continue
		}
		out = append(out, x)
	}
	return out, nextPage(h) > 0, nil
}

func (g *githubSource) rawPull(ctx context.Context, n int) (*ghPull, error) {
	var p ghPull
	if _, err := g.get(ctx, g.repoURL(fmt.Sprintf("pulls/%d", n)), &p); err != nil {
		if platformStatus(err) == http.StatusNotFound {
			return nil, httpx.NotFound(fmt.Sprintf("There's no pull request #%d.", n))
		}
		return nil, err
	}
	return &p, nil
}

func (g *githubSource) reviews(ctx context.Context, n int) ([]ghReview, error) {
	var list []ghReview
	_, err := g.get(ctx, g.repoURL(fmt.Sprintf("pulls/%d/reviews?per_page=100", n)), &list)
	return list, err
}

func (g *githubSource) Pull(ctx context.Context, n int) (*XPull, error) {
	r, err := g.repo(ctx)
	if err != nil {
		return nil, err
	}
	p, err := g.rawPull(ctx, n)
	if err != nil {
		return nil, err
	}
	reviews, err := g.reviews(ctx, n)
	if err != nil {
		return nil, err
	}
	x := g.toPull(*p, g.level(r), reviews)
	return &x, nil
}

func (g *githubSource) OpenPull(ctx context.Context, in OpenPullInput) (*XPull, error) {
	r, err := g.repo(ctx)
	if err != nil {
		return nil, err
	}
	title, err := cleanTitle(in.Title)
	if err != nil {
		return nil, err
	}
	base := in.Base
	if base == "" {
		base = r.Default
	}
	var p ghPull
	if _, err := g.send(ctx, http.MethodPost, g.repoURL("pulls"), map[string]string{"title": title, "body": in.Body, "head": in.Head, "base": base}, &p); err != nil {
		return nil, err
	}
	g.forgetMutable()
	x := g.toPull(p, g.level(r), nil)
	return &x, nil
}

func (g *githubSource) UpdatePull(ctx context.Context, n int, in updatePullReq) (*XPull, error) {
	body := map[string]string{}
	if in.Title != nil {
		t, err := cleanTitle(*in.Title)
		if err != nil {
			return nil, err
		}
		body["title"] = t
	}
	if in.Body != nil {
		body["body"] = *in.Body
	}
	if in.Base != nil {
		body["base"] = *in.Base
	}
	if _, err := g.send(ctx, http.MethodPatch, g.repoURL(fmt.Sprintf("pulls/%d", n)), body, nil); err != nil {
		return nil, err
	}
	return g.Pull(ctx, n)
}

func (g *githubSource) SetPullOpen(ctx context.Context, n int, open bool) (*XPull, error) {
	state := "closed"
	if open {
		state = "open"
	}
	if _, err := g.send(ctx, http.MethodPatch, g.repoURL(fmt.Sprintf("pulls/%d", n)), map[string]string{"state": state}, nil); err != nil {
		return nil, err
	}
	g.forgetMutable()
	return g.Pull(ctx, n)
}

func (g *githubSource) PullCommits(ctx context.Context, n int) ([]Commit, error) {
	var list []ghCommit
	if _, err := g.get(ctx, g.repoURL(fmt.Sprintf("pulls/%d/commits?per_page=100", n)), &list); err != nil {
		return nil, err
	}
	out := make([]Commit, 0, len(list))
	for _, c := range list {
		out = append(out, ghToCommit(c))
	}
	return out, nil
}

// PullDiff reads the changed files page by page (GitHub sends 30 by
// default and up to 3,000 in all).
func (g *githubSource) PullDiff(ctx context.Context, n int, p string) (*XPullDiff, error) {
	pr, err := g.rawPull(ctx, n)
	if err != nil {
		return nil, err
	}
	var files []ghFile
	truncated := false
	for page := 1; page > 0; {
		if page > 3000/remotePerPage {
			truncated = true
			break
		}
		var list []ghFile
		h, err := g.get(ctx, g.repoURL(fmt.Sprintf("pulls/%d/files?per_page=%d&page=%d", n, remotePerPage, page)), &list)
		if err != nil {
			return nil, err
		}
		files = append(files, list...)
		page = nextPage(h)
	}
	return &XPullDiff{Base: pr.Base.SHA, Head: pr.Head.SHA, Diff: buildDiff(ghPatchFiles(files), truncated, p)}, nil
}

func (g *githubSource) reviewComments(ctx context.Context, n int) ([]ghReviewComment, error) {
	var all []ghReviewComment
	for page := 1; page > 0 && page <= 5; {
		var list []ghReviewComment
		h, err := g.get(ctx, g.repoURL(fmt.Sprintf("pulls/%d/comments?per_page=100&page=%d", n, page)), &list)
		if err != nil {
			return nil, err
		}
		all = append(all, list...)
		page = nextPage(h)
	}
	return all, nil
}

func (g *githubSource) reviewComment(c ghReviewComment, lvl string) XComment {
	side := "new"
	if c.Side == "LEFT" {
		side = "old"
	}
	line := c.Line
	if line == nil {
		line = c.OriginalLine
	}
	x := XComment{ID: "r" + strconv.FormatInt(c.ID, 10), Author: g.user(c.User), Body: c.Body, Path: ptr(c.Path), Line: line, Side: ptr(side),
		CommitSHA: ptr(c.CommitID), CreatedAt: c.CreatedAt, CanEdit: g.canWrite && g.isViewer(c.User)}
	if c.InReplyTo != nil {
		x.ReplyTo = ptr("r" + strconv.FormatInt(*c.InReplyTo, 10))
	}
	if c.ReviewID != nil {
		x.ReviewID = ptr("github:" + strconv.FormatInt(*c.ReviewID, 10))
	}
	if c.UpdatedAt.After(c.CreatedAt.Add(time.Second)) {
		x.EditedAt = ptr(c.UpdatedAt)
	}
	return x
}

func (g *githubSource) Threads(ctx context.Context, n int) ([]XThread, string, error) {
	r, err := g.repo(ctx)
	if err != nil {
		return nil, "", err
	}
	pr, err := g.rawPull(ctx, n)
	if err != nil {
		return nil, "", err
	}
	list, err := g.reviewComments(ctx, n)
	if err != nil {
		return nil, "", err
	}
	lvl := g.level(r)
	byRoot := map[int64]*XThread{}
	var order []int64
	for _, c := range list {
		if c.InReplyTo == nil {
			byRoot[c.ID] = &XThread{Root: g.reviewComment(c, lvl), Replies: []XComment{}, Outdated: c.Line == nil}
			order = append(order, c.ID)
		}
	}
	for _, c := range list {
		if c.InReplyTo != nil {
			if t, ok := byRoot[*c.InReplyTo]; ok {
				t.Replies = append(t.Replies, g.reviewComment(c, lvl))
			}
		}
	}
	out := make([]XThread, 0, len(order))
	for _, id := range order {
		out = append(out, *byRoot[id])
	}
	return out, pr.Head.SHA, nil
}

func (g *githubSource) Timeline(ctx context.Context, n int) ([]XTimelineItem, error) {
	r, err := g.repo(ctx)
	if err != nil {
		return nil, err
	}
	lvl := g.level(r)
	pr, err := g.rawPull(ctx, n)
	if err != nil {
		return nil, err
	}
	var comments []ghIssueComment
	if _, err := g.get(ctx, g.repoURL(fmt.Sprintf("issues/%d/comments?per_page=100", n)), &comments); err != nil {
		return nil, err
	}
	reviews, err := g.reviews(ctx, n)
	if err != nil {
		return nil, err
	}
	rcs, err := g.reviewComments(ctx, n)
	if err != nil {
		return nil, err
	}
	var events []struct {
		ID        int64     `json:"id"`
		Event     string    `json:"event"`
		Actor     *ghUser   `json:"actor"`
		CommitID  *string   `json:"commit_id"`
		CreatedAt time.Time `json:"created_at"`
		Rename    *struct {
			From string `json:"from"`
			To   string `json:"to"`
		} `json:"rename"`
	}
	if _, err := g.get(ctx, g.repoURL(fmt.Sprintf("issues/%d/events?per_page=100", n)), &events); err != nil {
		return nil, err
	}
	items := []XTimelineItem{{Type: "event", At: pr.CreatedAt, Event: &XEvent{ID: "opened", Kind: "opened", Actor: g.user(pr.User), Data: map[string]any{}, CreatedAt: pr.CreatedAt}}}
	for _, c := range comments {
		x := XComment{ID: "i" + strconv.FormatInt(c.ID, 10), Author: g.user(c.User), Body: c.Body, CreatedAt: c.CreatedAt, CanEdit: g.canWrite && g.isViewer(c.User)}
		if c.UpdatedAt.After(c.CreatedAt.Add(time.Second)) {
			x.EditedAt = ptr(c.UpdatedAt)
		}
		items = append(items, XTimelineItem{Type: "comment", At: c.CreatedAt, Comment: &x})
	}
	// Line comments: roots under their review, the rest on their own.
	threadsOf := map[int64][]XThreadSummary{}
	replies := map[int64]int{}
	for _, c := range rcs {
		if c.InReplyTo != nil {
			replies[*c.InReplyTo]++
		}
	}
	for _, c := range rcs {
		if c.InReplyTo != nil {
			continue
		}
		x := g.reviewComment(c, lvl)
		ts := XThreadSummary{ID: x.ID, Path: c.Path, Side: *x.Side, Body: c.Body, Replies: replies[c.ID], Author: x.Author}
		if x.Line != nil {
			ts.Line = *x.Line
		}
		if c.ReviewID != nil {
			threadsOf[*c.ReviewID] = append(threadsOf[*c.ReviewID], ts)
		} else {
			items = append(items, XTimelineItem{Type: "thread", At: c.CreatedAt, Thread: &ts})
		}
	}
	reviewIDs := map[int64]bool{}
	for _, rv := range reviews {
		if rv.State == "PENDING" {
			continue
		}
		reviewIDs[rv.ID] = true
		state := ghReviewState(rv.State)
		x := XReview{ID: "github:" + strconv.FormatInt(rv.ID, 10), Author: g.user(rv.User), State: state, Body: rv.Body, CommitSHA: rv.CommitID, CreatedAt: rv.SubmittedAt}
		if rv.State == "DISMISSED" {
			x.DismissedAt = ptr(rv.SubmittedAt)
		}
		// A reply posts a review of its own that only holds the reply.
		if rv.State == "COMMENTED" && rv.Body == "" && len(threadsOf[rv.ID]) == 0 {
			continue
		}
		items = append(items, XTimelineItem{Type: "review", At: rv.SubmittedAt, Review: &x, Threads: threadsOf[rv.ID]})
	}
	for id, ts := range threadsOf {
		if !reviewIDs[id] {
			for i := range ts {
				items = append(items, XTimelineItem{Type: "thread", At: pr.CreatedAt, Thread: &ts[i]})
			}
		}
	}
	for _, e := range events {
		ev := &XEvent{ID: "github:" + strconv.FormatInt(e.ID, 10), Actor: g.user(e.Actor), Data: map[string]any{}, CreatedAt: e.CreatedAt}
		switch e.Event {
		case "closed":
			if pr.MergedAt != nil && e.CreatedAt.Sub(*pr.MergedAt).Abs() < 5*time.Second {
				continue // the merge says it
			}
			ev.Kind = "closed"
		case "reopened":
			ev.Kind = "reopened"
		case "merged":
			ev.Kind = "merged"
			if e.CommitID != nil {
				ev.Data["commit"] = *e.CommitID
			} else if pr.MergeSHA != nil {
				ev.Data["commit"] = *pr.MergeSHA
			}
			ev.Data["method"] = "merge"
		case "review_requested":
			ev.Kind = "review_requested"
		case "review_request_removed":
			ev.Kind = "review_request_removed"
		case "renamed":
			ev.Kind = "retitled"
			if e.Rename != nil {
				ev.Data["from"], ev.Data["to"] = e.Rename.From, e.Rename.To
			}
		case "base_ref_changed":
			ev.Kind = "base_changed"
		case "head_ref_deleted":
			ev.Kind = "branch_deleted"
			ev.Data["branch"] = pr.Head.Ref
			ev.Data["after_merge"] = pr.MergedAt != nil
		case "head_ref_force_pushed":
			ev.Kind = "force_pushed"
		default:
			continue
		}
		items = append(items, XTimelineItem{Type: "event", At: e.CreatedAt, Event: ev})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].At.Before(items[j].At) })
	return items, nil
}

func (g *githubSource) Mergeability(ctx context.Context, n int) (*Mergeability, error) {
	r, err := g.repo(ctx)
	if err != nil {
		return nil, err
	}
	lvl := g.level(r)
	pr, err := g.rawPull(ctx, n)
	if err != nil {
		return nil, err
	}
	reviews, _ := g.reviews(ctx, n)
	x := g.toPull(*pr, lvl, reviews)
	m := &Mergeability{Conflicts: []string{}, Blockers: []string{}, Methods: []string{}, Approvals: x.Approvals, HeadSHA: pr.Head.SHA, BaseSHA: pr.Base.SHA,
		DeleteBranch: r.DeleteBranchOnMerge}
	if boolOr(r.AllowMergeCommit, true) {
		m.Methods = append(m.Methods, "merge")
	}
	if boolOr(r.AllowSquashMerge, true) {
		m.Methods = append(m.Methods, "squash")
	}
	if boolOr(r.AllowRebaseMerge, true) {
		m.Methods = append(m.Methods, "rebase")
	}
	sameRepo := pr.Head.Repo != nil && strings.EqualFold(pr.Head.Repo.FullName, r.FullName)
	m.CanDeleteBranch = g.canWrite && levelAtLeast(lvl, "edit") && sameRepo
	switch {
	case x.State == "merged":
		m.State = "merged"
	case x.State == "closed":
		m.State = "closed"
	case pr.Mergeable == nil || pr.MergeableState == "unknown":
		m.State = "blocked"
		m.Blockers = append(m.Blockers, "GitHub is still checking whether this can merge. Refresh in a moment.")
	case pr.MergeableState == "dirty" || !*pr.Mergeable:
		m.State = "conflicts"
		m.Blockers = append(m.Blockers, "This branch has conflicts with "+pr.Base.Ref+". Resolve them on GitHub or locally.")
	case pr.MergeableState == "blocked":
		m.State = "blocked"
		m.Blockers = append(m.Blockers, "Branch protection on "+pr.Base.Ref+" blocks merging: it needs approvals or passing required checks.")
	case pr.MergeableState == "behind":
		m.State = "blocked"
		m.Blockers = append(m.Blockers, pr.Head.Ref+" is out of date with "+pr.Base.Ref+" and protection requires it to be up to date.")
	case pr.MergeableState == "draft" || pr.Draft:
		m.State = "blocked"
		m.Blockers = append(m.Blockers, "This is a draft. Mark it ready for review on GitHub to merge.")
	default: // clean, unstable (non-required checks failing), has_hooks
		m.State = "clean"
		m.Mergeable = true
	}
	if !g.canWrite {
		m.Blockers = append(m.Blockers, "Turn on changes for GitHub in Settings → Connections to merge from here.")
	} else if !levelAtLeast(lvl, "edit") {
		m.Blockers = append(m.Blockers, "This GitHub account can't merge in "+r.FullName+".")
	}
	m.CanMerge = m.Mergeable && g.canWrite && levelAtLeast(lvl, "edit") && len(m.Methods) > 0
	return m, nil
}

func (g *githubSource) Merge(ctx context.Context, n int, in mergeReq) (*XPull, error) {
	pr, err := g.rawPull(ctx, n)
	if err != nil {
		return nil, err
	}
	method := in.Method
	if method == "" {
		method = "merge"
	}
	body := map[string]string{"merge_method": method}
	if in.ExpectedHead != "" {
		body["sha"] = in.ExpectedHead
	}
	if t := strings.TrimSpace(in.Title); t != "" {
		body["commit_title"] = t
	}
	if m := strings.TrimSpace(in.Message); m != "" {
		body["commit_message"] = m
	}
	if _, err := g.send(ctx, http.MethodPut, g.repoURL(fmt.Sprintf("pulls/%d/merge", n)), body, nil); err != nil {
		switch pe := asPlatform(err); {
		case pe != nil && pe.Status == http.StatusMethodNotAllowed:
			return nil, httpx.NewError(http.StatusConflict, "not_mergeable", "GitHub won't merge this yet: "+orDefault(pe.Message, "it isn't mergeable."))
		case pe != nil && pe.Status == http.StatusConflict:
			return nil, httpx.NewError(http.StatusConflict, "head_moved", "New commits arrived on "+pr.Head.Ref+". Review them, then merge.")
		}
		return nil, err
	}
	if in.DeleteBranch && pr.Head.Repo != nil && strings.EqualFold(pr.Head.Repo.FullName, g.id.Path) {
		_, _ = g.send(ctx, http.MethodDelete, g.repoURL("git/refs/heads/"+encodePath(pr.Head.Ref)), nil, nil)
	}
	g.forgetMutable()
	return g.Pull(ctx, n)
}

func (g *githubSource) Comment(ctx context.Context, n int, in xCommentInput) (*XComment, error) {
	body, err := cleanBody(in.Body)
	if err != nil {
		return nil, err
	}
	r, err := g.repo(ctx)
	if err != nil {
		return nil, err
	}
	lvl := g.level(r)
	switch {
	case strings.HasPrefix(in.ReplyTo, "r"):
		var c ghReviewComment
		if _, err := g.send(ctx, http.MethodPost, g.repoURL(fmt.Sprintf("pulls/%d/comments/%s/replies", n, strings.TrimPrefix(in.ReplyTo, "r"))), map[string]string{"body": body}, &c); err != nil {
			return nil, err
		}
		x := g.reviewComment(c, lvl)
		return &x, nil
	case in.Path != "":
		pr, err := g.rawPull(ctx, n)
		if err != nil {
			return nil, err
		}
		side := "RIGHT"
		if in.Side == "old" {
			side = "LEFT"
		}
		var c ghReviewComment
		if _, err := g.send(ctx, http.MethodPost, g.repoURL(fmt.Sprintf("pulls/%d/comments", n)),
			map[string]any{"body": body, "commit_id": pr.Head.SHA, "path": in.Path, "line": in.Line, "side": side}, &c); err != nil {
			return nil, err
		}
		x := g.reviewComment(c, lvl)
		return &x, nil
	default:
		var c ghIssueComment
		if _, err := g.send(ctx, http.MethodPost, g.repoURL(fmt.Sprintf("issues/%d/comments", n)), map[string]string{"body": body}, &c); err != nil {
			return nil, err
		}
		return &XComment{ID: "i" + strconv.FormatInt(c.ID, 10), Author: g.user(c.User), Body: c.Body, CreatedAt: c.CreatedAt, CanEdit: true}, nil
	}
}

func ghCommentPath(id string) (string, bool) {
	num := id[min(1, len(id)):]
	if _, err := strconv.ParseInt(num, 10, 64); err != nil {
		return "", false
	}
	switch {
	case strings.HasPrefix(id, "i"):
		return "issues/comments/" + num, true
	case strings.HasPrefix(id, "r"):
		return "pulls/comments/" + num, true
	}
	return "", false
}

func (g *githubSource) EditComment(ctx context.Context, n int, id, body string) (*XComment, error) {
	body, err := cleanBody(body)
	if err != nil {
		return nil, err
	}
	p, ok := ghCommentPath(id)
	if !ok {
		return nil, httpx.NotFound("That comment doesn't exist.")
	}
	var c ghReviewComment
	if _, err := g.send(ctx, http.MethodPatch, g.repoURL(p), map[string]string{"body": body}, &c); err != nil {
		return nil, err
	}
	return &XComment{ID: id, Author: g.user(c.User), Body: c.Body, CreatedAt: c.CreatedAt, EditedAt: ptr(c.UpdatedAt), CanEdit: true}, nil
}

func (g *githubSource) DeleteComment(ctx context.Context, n int, id string) error {
	p, ok := ghCommentPath(id)
	if !ok {
		return httpx.NotFound("That comment doesn't exist.")
	}
	_, err := g.send(ctx, http.MethodDelete, g.repoURL(p), nil, nil)
	return err
}

func (g *githubSource) ResolveThread(context.Context, int, string, bool) error {
	return errNotSupported("GitHub", "resolving threads (REST)")
}

func (g *githubSource) Review(ctx context.Context, n int, in xReviewInput) error {
	pr, err := g.rawPull(ctx, n)
	if err != nil {
		return err
	}
	event := map[string]string{"approve": "APPROVE", "request_changes": "REQUEST_CHANGES", "comment": "COMMENT"}[in.State]
	if event == "" {
		return httpx.BadRequest("state is approve, request_changes or comment.")
	}
	body := strings.TrimSpace(in.Body)
	if event != "APPROVE" && body == "" && len(in.Comments) == 0 {
		return httpx.BadRequest("Write what to change or comment on.")
	}
	comments := []map[string]any{}
	for _, c := range in.Comments {
		b, err := cleanBody(c.Body)
		if err != nil {
			return err
		}
		side := "RIGHT"
		if c.Side == "old" {
			side = "LEFT"
		}
		comments = append(comments, map[string]any{"path": c.Path, "line": c.Line, "side": side, "body": b})
	}
	req := map[string]any{"event": event, "commit_id": pr.Head.SHA, "body": body}
	if len(comments) > 0 {
		req["comments"] = comments
	}
	if _, err := g.send(ctx, http.MethodPost, g.repoURL(fmt.Sprintf("pulls/%d/reviews", n)), req, nil); err != nil {
		return err
	}
	g.forgetMutable()
	return nil
}

// ---- listing (connectedLister) -------------------------------------------------

func (g *githubSource) summary(r ghRepo, canWrite bool) XRepoSummary {
	id, _ := newExtID(ProviderGitHub, g.conn.ID, r.FullName)
	lvl := "view"
	if r.Permissions != nil {
		p := r.Permissions
		lvl = levelFrom(p.Admin, p.Push || p.Maintain, p.Triage || p.Pull, canWrite)
	}
	s := XRepoSummary{ID: id.String(), Name: r.Name, Slug: r.Name, FullName: r.FullName, Owner: r.Owner.Login, DefaultBranch: r.Default,
		MyLevel: lvl, Private: r.Private, PushedAt: r.PushedAt, UpdatedAt: r.UpdatedAt, Source: g.source(r.HTMLURL)}
	if r.Description != nil {
		s.Description = *r.Description
	}
	return s
}

func (g *githubSource) listRepos(ctx context.Context, q, sortBy, owner string, page int) ([]XRepoSummary, int, error) {
	canWrite := g.s.writeGranted(ctx, g.userID, g.conn)
	sortParam, dir := "pushed", "desc"
	if sortBy == "name" {
		sortParam, dir = "full_name", "asc"
	}
	if q == "" && owner == "" {
		var list []ghRepo
		h, err := g.get(ctx, g.api+fmt.Sprintf("/user/repos?affiliation=owner,collaborator,organization_member&sort=%s&direction=%s&per_page=%d&page=%d", sortParam, dir, connectedPerPage, page), &list)
		if err != nil {
			return nil, 0, err
		}
		out := make([]XRepoSummary, 0, len(list))
		for _, r := range list {
			out = append(out, g.summary(r, canWrite))
		}
		return out, nextPage(h), nil
	}
	// Text or owner filters: scan the first pages (cached) and filter.
	key := "list:" + g.userID.String() + ":" + g.conn.ID + ":" + sortParam
	var all []ghRepo
	if v, ok := g.s.rcache.get(key); ok {
		all = v.([]ghRepo)
	} else {
		for p := 1; p > 0 && p <= connectedScanMax; {
			var list []ghRepo
			h, err := g.get(ctx, g.api+fmt.Sprintf("/user/repos?affiliation=owner,collaborator,organization_member&sort=%s&direction=%s&per_page=100&page=%d", sortParam, dir, p), &list)
			if err != nil {
				return nil, 0, err
			}
			all = append(all, list...)
			p = nextPage(h)
		}
		g.s.rcache.set(key, all, remoteListTTL)
	}
	lq := strings.ToLower(q)
	var match []XRepoSummary
	for _, r := range all {
		if owner != "" && !strings.EqualFold(r.Owner.Login, owner) {
			continue
		}
		desc := ""
		if r.Description != nil {
			desc = *r.Description
		}
		if lq != "" && !strings.Contains(strings.ToLower(r.FullName), lq) && !strings.Contains(strings.ToLower(desc), lq) {
			continue
		}
		match = append(match, g.summary(r, canWrite))
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

func (g *githubSource) owners(ctx context.Context) ([]string, error) {
	key := "owners:" + g.userID.String() + ":" + g.conn.ID
	if v, ok := g.s.rcache.get(key); ok {
		return v.([]string), nil
	}
	out := []string{g.viewer.Login}
	var orgs []ghUser
	if _, err := g.get(ctx, g.api+"/user/orgs?per_page=100", &orgs); err != nil {
		return out, err
	}
	for _, o := range orgs {
		out = append(out, o.Login)
	}
	g.s.rcache.set(key, out, viewerTTL)
	return out, nil
}
