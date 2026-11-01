package repos

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"scaffoldapp/backend/internal/httpx"
)

// Reading a repository: refs, trees, files, history, diffs, blame and code
// search. Every route takes ?ref= (a branch, tag or commit id; default: the
// default branch), resolves it once to a commit id and reads at that id.

const (
	maxImageBytes   = 5 << 20
	maxPathsListed  = 50000
	maxFinderHits   = 50
	maxGrepMatches  = 200
	maxGrepPerFile  = 20
	maxLogPage      = 50
	maxCompare      = 250
	maxBlameBytes   = 1 << 20
	treeCommitsWalk = 2000
)

var errEmptyRepo = httpx.NewError(http.StatusNotFound, "repo_empty", "This repository has no commits yet.")

// resolved is a ref the client named, resolved to a commit.
type resolved struct {
	Ref string // what to show: a branch or tag name, or the commit id
	SHA string
}

func (s *Service) resolve(ctx context.Context, repo *Repo, ref string) (resolved, error) {
	if ref == "" {
		ref = repo.DefaultBranch
	}
	sha, err := s.git.ResolveCommit(ctx, repo.ID, ref)
	if errors.Is(err, ErrNotFound) {
		if ref == repo.DefaultBranch {
			if refs, rerr := s.git.Refs(ctx, repo.ID); rerr == nil && len(refs) == 0 {
				return resolved{}, errEmptyRepo
			}
		}
		return resolved{}, httpx.NotFound("There's no branch, tag or commit named " + ref + ".")
	}
	if err != nil {
		return resolved{}, err
	}
	return resolved{Ref: ref, SHA: sha}, nil
}

func queryPath(r *http.Request) (string, error) {
	p, err := CleanPath(r.URL.Query().Get("path"))
	if err != nil {
		return "", httpx.BadRequest("That path isn't valid.")
	}
	return p, nil
}

// ---- refs ----------------------------------------------------------------------

// RefView is a branch or tag with what clients show beside it.
type RefView struct {
	Ref
	Default   bool `json:"default"`
	Protected bool `json:"protected"`
	Ahead     *int `json:"ahead"`  // branches: commits not on the default branch
	Behind    *int `json:"behind"` // branches: default-branch commits it lacks
	OpenPull  *int `json:"open_pull"`
}

func (s *Service) listRefs(w http.ResponseWriter, r *http.Request) error {
	repo, err := s.repoFromRoute(r, levelView)
	if err != nil {
		return err
	}
	ctx := r.Context()
	refs, err := s.git.Refs(ctx, repo.ID)
	if err != nil {
		return err
	}
	rules, err := s.protections(ctx, s.d.Pool, repo.ID)
	if err != nil {
		return err
	}
	pulls := map[string]int{}
	rows, err := s.d.Pool.Query(ctx, `SELECT head_branch, number FROM repos_pulls WHERE repo_id = $1 AND state = 'open'`, repo.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var b string
		var n int
		if err := rows.Scan(&b, &n); err != nil {
			rows.Close()
			return err
		}
		pulls[b] = n
	}
	rows.Close()
	defaultSHA := ""
	for _, ref := range refs {
		if ref.Kind == "branch" && ref.Name == repo.DefaultBranch {
			defaultSHA = ref.SHA
		}
	}
	withCounts := r.URL.Query().Get("counts") == "true"
	out := make([]RefView, 0, len(refs))
	for i, ref := range refs {
		v := RefView{Ref: ref}
		if ref.Kind == "branch" {
			v.Default = ref.Name == repo.DefaultBranch
			v.Protected = v.Default || protectionFor(rules, ref.Name) != nil
			if n, ok := pulls[ref.Name]; ok {
				v.OpenPull = &n
			}
			// Ahead/behind costs two rev-lists a branch: the first 100 only.
			if withCounts && defaultSHA != "" && !v.Default && i < 100 {
				ahead, err1 := s.git.CountRange(ctx, repo.ID, defaultSHA, ref.SHA)
				behind, err2 := s.git.CountRange(ctx, repo.ID, ref.SHA, defaultSHA)
				if err1 == nil && err2 == nil {
					v.Ahead, v.Behind = &ahead, &behind
				}
			}
		}
		out = append(out, v)
	}
	httpx.OK(w, map[string]any{"items": out, "default_branch": repo.DefaultBranch})
	return nil
}

// ---- tree ----------------------------------------------------------------------

// TreeView is a directory at a commit, with its README.
type TreeView struct {
	Ref        string      `json:"ref"`
	SHA        string      `json:"sha"`
	Path       string      `json:"path"`
	Entries    []TreeEntry `json:"entries"`
	Readme     *BlobView   `json:"readme"`
	LastCommit *Commit     `json:"last_commit"`
}

func (s *Service) getTree(w http.ResponseWriter, r *http.Request) error {
	repo, err := s.repoFromRoute(r, levelView)
	if err != nil {
		return err
	}
	ctx := r.Context()
	at, err := s.resolve(ctx, repo, r.URL.Query().Get("ref"))
	if err != nil {
		return err
	}
	dir, err := queryPath(r)
	if err != nil {
		return err
	}
	entries, err := s.git.ListTree(ctx, repo.ID, at.SHA, dir)
	if errors.Is(err, ErrNotFound) {
		return httpx.NotFound("There's no folder at " + dir + " on " + at.Ref + ".")
	}
	if err != nil {
		return err
	}
	if entries == nil {
		entries = []TreeEntry{}
	}
	tv := TreeView{Ref: at.Ref, SHA: at.SHA, Path: dir, Entries: entries}
	if log, err := s.git.Log(ctx, repo.ID, at.SHA, dir, 0, 1); err == nil && len(log) == 1 {
		tv.LastCommit = &log[0]
	}
	for _, e := range entries {
		if e.Type == "blob" && isReadme(e.Name) {
			if b, err := s.readBlob(ctx, repo, at, e); err == nil {
				tv.Readme = b
			}
			break
		}
	}
	httpx.OK(w, tv)
	return nil
}

func isReadme(name string) bool {
	n := strings.ToLower(name)
	return n == "readme.md" || n == "readme" || n == "readme.markdown" || n == "readme.txt"
}

// getTreeCommits is GET /tree/commits?ref=&path=: the newest commit for each
// entry of a directory (filled in after the listing renders).
func (s *Service) getTreeCommits(w http.ResponseWriter, r *http.Request) error {
	repo, err := s.repoFromRoute(r, levelView)
	if err != nil {
		return err
	}
	ctx := r.Context()
	at, err := s.resolve(ctx, repo, r.URL.Query().Get("ref"))
	if err != nil {
		return err
	}
	dir, err := queryPath(r)
	if err != nil {
		return err
	}
	entries, err := s.git.ListTree(ctx, repo.ID, at.SHA, dir)
	if errors.Is(err, ErrNotFound) {
		return httpx.NotFound("There's no folder at " + dir + ".")
	}
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name)
	}
	if len(names) > 500 {
		names = names[:500]
	}
	found, err := s.git.LastCommits(ctx, repo.ID, at.SHA, dir, names, treeCommitsWalk)
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"sha": at.SHA, "commits": found})
	return nil
}

// ---- files ---------------------------------------------------------------------

// BlobView is a file at a commit. Content is UTF-8 text, or base64 for
// images; empty for other binary files and files over the size limit.
type BlobView struct {
	Path      string `json:"path"`
	Name      string `json:"name"`
	SHA       string `json:"sha"` // the commit
	Ref       string `json:"ref"`
	OID       string `json:"oid"`
	Mode      string `json:"mode"`
	Size      int64  `json:"size"`
	Binary    bool   `json:"binary"`
	Image     bool   `json:"image"`
	TooLarge  bool   `json:"too_large"`
	Editable  bool   `json:"editable"` // text, under the edit limit, a regular file
	Encoding  string `json:"encoding"` // utf-8 | base64 | ""
	Content   string `json:"content"`
	Language  string `json:"language"` // highlighter hint from the name
	Lines     int    `json:"lines"`
	Symlink   bool   `json:"symlink"`
	Submodule bool   `json:"submodule"`
}

var imageTypes = map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp", ".ico": "image/x-icon", ".bmp": "image/bmp"}

func (s *Service) readBlob(ctx context.Context, repo *Repo, at resolved, e TreeEntry) (*BlobView, error) {
	b := &BlobView{Path: e.Path, Name: path.Base(e.Path), SHA: at.SHA, Ref: at.Ref, OID: e.OID, Mode: e.Mode, Size: e.Size,
		Language: languageOf(e.Path), Symlink: e.Type == "symlink", Submodule: e.Type == "commit"}
	if b.Submodule {
		return b, nil
	}
	ext := strings.ToLower(path.Ext(e.Path))
	if _, ok := imageTypes[ext]; ok && ext != ".svg" {
		b.Image, b.Binary = true, true
		if e.Size > maxImageBytes {
			b.TooLarge = true
			return b, nil
		}
		data, _, err := s.git.ReadBlob(ctx, repo.ID, e.OID, maxImageBytes)
		if err != nil {
			return nil, err
		}
		b.Encoding, b.Content = "base64", base64.StdEncoding.EncodeToString(data)
		return b, nil
	}
	if e.Size > s.maxEditBytes {
		b.TooLarge = true
		head, _, err := s.git.ReadBlob(ctx, repo.ID, e.OID, 8000)
		if err != nil {
			return nil, err
		}
		b.Binary = isBinary(head)
		return b, nil
	}
	data, _, err := s.git.ReadBlob(ctx, repo.ID, e.OID, s.maxEditBytes)
	if err != nil {
		return nil, err
	}
	if isBinary(data) {
		b.Binary = true
		return b, nil
	}
	b.Encoding, b.Content = "utf-8", string(data)
	b.Lines = bytes.Count(data, []byte{'\n'})
	if len(data) > 0 && data[len(data)-1] != '\n' {
		b.Lines++
	}
	b.Editable = !b.Symlink
	return b, nil
}

// isBinary uses git's rule (a NUL in the first 8000 bytes) plus UTF-8.
func isBinary(b []byte) bool {
	head := b
	if len(head) > 8000 {
		head = head[:8000]
	}
	if bytes.IndexByte(head, 0) >= 0 {
		return true
	}
	return !utf8.Valid(trimPartialRune(b))
}

func trimPartialRune(b []byte) []byte {
	for i := 0; i < 3 && len(b) > 0; i++ {
		if utf8.Valid(b) {
			return b
		}
		b = b[:len(b)-1]
	}
	return b
}

func (s *Service) getBlob(w http.ResponseWriter, r *http.Request) error {
	repo, err := s.repoFromRoute(r, levelView)
	if err != nil {
		return err
	}
	ctx := r.Context()
	at, err := s.resolve(ctx, repo, r.URL.Query().Get("ref"))
	if err != nil {
		return err
	}
	p, err := queryPath(r)
	if err != nil || p == "" {
		return httpx.BadRequest("Name the file to open.")
	}
	e, err := s.git.Entry(ctx, repo.ID, at.SHA, p)
	if errors.Is(err, ErrNotFound) {
		return httpx.NotFound("There's no file at " + p + " on " + at.Ref + ".")
	}
	if err != nil {
		return err
	}
	if e.Type == "tree" {
		return httpx.NewError(http.StatusConflict, "is_folder", p+" is a folder.")
	}
	b, err := s.readBlob(ctx, repo, at, e)
	if err != nil {
		return err
	}
	httpx.OK(w, b)
	return nil
}

// getRaw streams a file as a download (never rendered by the browser).
func (s *Service) getRaw(w http.ResponseWriter, r *http.Request) error {
	repo, err := s.repoFromRoute(r, levelView)
	if err != nil {
		return err
	}
	ctx := r.Context()
	at, err := s.resolve(ctx, repo, r.URL.Query().Get("ref"))
	if err != nil {
		return err
	}
	p, err := queryPath(r)
	if err != nil || p == "" {
		return httpx.BadRequest("Name the file to download.")
	}
	e, err := s.git.Entry(ctx, repo.ID, at.SHA, p)
	if errors.Is(err, ErrNotFound) || (err == nil && e.Type != "blob") {
		return httpx.NotFound("There's no file at " + p + ".")
	}
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+strings.ReplaceAll(path.Base(p), `"`, "")+`"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox")
	if e.Size >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(e.Size, 10))
	}
	return s.git.StreamBlob(ctx, repo.ID, e.OID, w)
}

// getDownload is GET /download?ref=&path=: a file as base64 JSON, for
// clients that save it through their own download flow (a browser can't
// send the session header with a plain link). Up to REPOS_MAX_COMMIT_BYTES.
func (s *Service) getDownload(w http.ResponseWriter, r *http.Request) error {
	repo, err := s.repoFromRoute(r, levelView)
	if err != nil {
		return err
	}
	ctx := r.Context()
	at, err := s.resolve(ctx, repo, r.URL.Query().Get("ref"))
	if err != nil {
		return err
	}
	p, err := queryPath(r)
	if err != nil || p == "" {
		return httpx.BadRequest("Name the file to download.")
	}
	e, err := s.git.Entry(ctx, repo.ID, at.SHA, p)
	if errors.Is(err, ErrNotFound) || (err == nil && e.Type != "blob") {
		return httpx.NotFound("There's no file at " + p + ".")
	}
	if err != nil {
		return err
	}
	if e.Size > s.maxCommit {
		return httpx.NewError(http.StatusUnprocessableEntity, "too_large", "This file is larger than "+formatBytes(s.maxCommit)+". Clone the repository to get it.")
	}
	data, _, err := s.git.ReadBlob(ctx, repo.ID, e.OID, s.maxCommit)
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"name": path.Base(p), "size": len(data), "content_base64": base64.StdEncoding.EncodeToString(data)})
	return nil
}

func (s *Service) getBlame(w http.ResponseWriter, r *http.Request) error {
	repo, err := s.repoFromRoute(r, levelView)
	if err != nil {
		return err
	}
	ctx := r.Context()
	at, err := s.resolve(ctx, repo, r.URL.Query().Get("ref"))
	if err != nil {
		return err
	}
	p, err := queryPath(r)
	if err != nil || p == "" {
		return httpx.BadRequest("Name the file.")
	}
	e, err := s.git.Entry(ctx, repo.ID, at.SHA, p)
	if errors.Is(err, ErrNotFound) || (err == nil && e.Type != "blob") {
		return httpx.NotFound("There's no file at " + p + ".")
	}
	if err != nil {
		return err
	}
	if e.Size > maxBlameBytes {
		return httpx.NewError(http.StatusUnprocessableEntity, "too_large", "This file is too large for blame here. Run git blame locally.")
	}
	ranges, err := s.git.Blame(ctx, repo.ID, at.SHA, p)
	if err != nil {
		return err
	}
	if ranges == nil {
		ranges = []BlameRange{}
	}
	httpx.OK(w, map[string]any{"sha": at.SHA, "path": p, "ranges": ranges})
	return nil
}

// ---- finding -------------------------------------------------------------------

// findPaths is the file finder: paths matching every character of q in
// order, best first (a match in the file name beats one in the folders).
func (s *Service) findPaths(w http.ResponseWriter, r *http.Request) error {
	repo, err := s.repoFromRoute(r, levelView)
	if err != nil {
		return err
	}
	ctx := r.Context()
	at, err := s.resolve(ctx, repo, r.URL.Query().Get("ref"))
	if err != nil {
		return err
	}
	paths, truncated, err := s.git.ListPaths(ctx, repo.ID, at.SHA, maxPathsListed)
	if err != nil {
		return err
	}
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	hits := rankPaths(paths, q, maxFinderHits)
	httpx.OK(w, map[string]any{"sha": at.SHA, "items": hits, "truncated": truncated})
	return nil
}

func rankPaths(paths []string, q string, limit int) []string {
	type hit struct {
		p     string
		score int
	}
	var hits []hit
	for _, p := range paths {
		if q == "" {
			hits = append(hits, hit{p, len(p)})
			if len(hits) >= limit {
				break
			}
			continue
		}
		lp := strings.ToLower(p)
		score, ok := fuzzyScore(lp, q)
		if !ok {
			continue
		}
		if strings.Contains(strings.ToLower(path.Base(p)), q) {
			score -= 1000
		}
		hits = append(hits, hit{p, score})
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score < hits[j].score })
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.p)
	}
	return out
}

// fuzzyScore: q's characters appear in order in s; lower is better (span
// of the match plus length).
func fuzzyScore(s, q string) (int, bool) {
	if strings.Contains(s, q) {
		return len(s), true
	}
	first, j := -1, 0
	for i := 0; i < len(s) && j < len(q); i++ {
		if s[i] == q[j] {
			if first < 0 {
				first = i
			}
			j++
			if j == len(q) {
				return (i-first)*4 + len(s) + 100, true
			}
		}
	}
	return 0, false
}

// searchCode is GET /search?q=&ref=: lines containing q (case-insensitive),
// at most 200, 20 per file.
func (s *Service) searchCode(w http.ResponseWriter, r *http.Request) error {
	repo, err := s.repoFromRoute(r, levelView)
	if err != nil {
		return err
	}
	ctx := r.Context()
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if n := utf8.RuneCountInString(q); n < 2 || n > 200 {
		return httpx.BadRequest("Search for 2 to 200 characters.")
	}
	at, err := s.resolve(ctx, repo, r.URL.Query().Get("ref"))
	if err != nil {
		return err
	}
	matches, truncated, err := s.git.Grep(ctx, repo.ID, at.SHA, q, maxGrepMatches, maxGrepPerFile)
	if err != nil {
		return err
	}
	if matches == nil {
		matches = []GrepMatch{}
	}
	httpx.OK(w, map[string]any{"sha": at.SHA, "ref": at.Ref, "items": matches, "truncated": truncated})
	return nil
}

// ---- history -------------------------------------------------------------------

type logCursor struct {
	SHA  string `json:"sha"`
	Skip int    `json:"skip"`
}

// listCommits is GET /commits?ref=&path=&cursor=. The cursor pins the walk
// to the commit the first page started from, so pages don't shift when the
// branch moves.
func (s *Service) listCommits(w http.ResponseWriter, r *http.Request) error {
	repo, err := s.repoFromRoute(r, levelView)
	if err != nil {
		return err
	}
	ctx := r.Context()
	p, err := queryPath(r)
	if err != nil {
		return err
	}
	page := httpx.ParsePage(r)
	limit := min(page.Limit, maxLogPage)
	var c logCursor
	if err := httpx.DecodeCursor(page.Cursor, &c); err != nil {
		return err
	}
	sha := c.SHA
	if sha == "" {
		at, err := s.resolve(ctx, repo, r.URL.Query().Get("ref"))
		if err != nil {
			return err
		}
		sha = at.SHA
	} else if !IsOID(sha) || c.Skip < 0 {
		return httpx.BadRequest("Invalid cursor.")
	}
	list, err := s.git.Log(ctx, repo.ID, sha, p, c.Skip, limit+1)
	if err != nil {
		return err
	}
	if list == nil {
		list = []Commit{}
	}
	out := httpx.Page[Commit]{Items: list}
	if len(list) > limit {
		out.Items = list[:limit]
		out.NextCursor = httpx.EncodeCursor(logCursor{SHA: sha, Skip: c.Skip + limit})
	}
	httpx.OK(w, out)
	return nil
}

// CommitView is one commit with its diff against its first parent.
type CommitView struct {
	Commit
	Diff *Diff `json:"diff"`
}

func (s *Service) getCommit(w http.ResponseWriter, r *http.Request) error {
	repo, err := s.repoFromRoute(r, levelView)
	if err != nil {
		return err
	}
	ctx := r.Context()
	at, err := s.resolve(ctx, repo, strings.ToLower(chi.URLParam(r, "sha")))
	if err != nil {
		return err
	}
	c, err := s.git.GetCommit(ctx, repo.ID, at.SHA)
	if err != nil {
		return err
	}
	parent := ""
	if len(c.Parents) > 0 {
		parent = c.Parents[0]
	}
	p, err := queryPath(r)
	if err != nil {
		return err
	}
	var paths []string
	if p != "" {
		paths = []string{p}
	}
	d, err := s.git.DiffCommits(ctx, repo.ID, parent, c.SHA, defaultDiffLimits, paths...)
	if err != nil {
		return err
	}
	httpx.OK(w, CommitView{Commit: c, Diff: d})
	return nil
}

// Comparison is base...head: what merging head into base would bring.
type Comparison struct {
	Base      string    `json:"base"`
	Head      string    `json:"head"`
	BaseSHA   string    `json:"base_sha"`
	HeadSHA   string    `json:"head_sha"`
	MergeBase string    `json:"merge_base"`
	Ahead     int       `json:"ahead"`  // head commits not in base
	Behind    int       `json:"behind"` // base commits not in head
	Commits   []Commit  `json:"commits"`
	Diff      *Diff     `json:"diff"`
	Mergeable *bool     `json:"mergeable"`
	Conflicts []string  `json:"conflicts"`
	OpenPull  *PullLink `json:"open_pull"`
}

// PullLink points at a pull request.
type PullLink struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
}

func (s *Service) compare(w http.ResponseWriter, r *http.Request) error {
	repo, err := s.repoFromRoute(r, levelView)
	if err != nil {
		return err
	}
	q := r.URL.Query()
	p, err := queryPath(r)
	if err != nil {
		return err
	}
	cmp, err := s.compareRefs(r.Context(), repo, q.Get("base"), q.Get("head"), p)
	if err != nil {
		return err
	}
	httpx.OK(w, cmp)
	return nil
}

func (s *Service) compareRefs(ctx context.Context, repo *Repo, base, head, onePath string) (*Comparison, error) {
	if head == "" {
		return nil, httpx.BadRequest("Choose a branch to compare.")
	}
	b, err := s.resolve(ctx, repo, base)
	if err != nil {
		return nil, err
	}
	h, err := s.resolve(ctx, repo, head)
	if err != nil {
		return nil, err
	}
	mb, err := s.git.MergeBase(ctx, repo.ID, b.SHA, h.SHA)
	if err != nil {
		return nil, err
	}
	cmp := &Comparison{Base: b.Ref, Head: h.Ref, BaseSHA: b.SHA, HeadSHA: h.SHA, MergeBase: mb, Commits: []Commit{}, Conflicts: []string{}}
	if mb == "" {
		return cmp, httpx.NewError(http.StatusUnprocessableEntity, "unrelated_histories", b.Ref+" and "+h.Ref+" have no history in common.")
	}
	if cmp.Ahead, err = s.git.CountRange(ctx, repo.ID, b.SHA, h.SHA); err != nil {
		return nil, err
	}
	if cmp.Behind, err = s.git.CountRange(ctx, repo.ID, h.SHA, b.SHA); err != nil {
		return nil, err
	}
	if cmp.Commits, err = s.git.RangeCommits(ctx, repo.ID, b.SHA, h.SHA, maxCompare); err != nil {
		return nil, err
	}
	if cmp.Commits == nil {
		cmp.Commits = []Commit{}
	}
	var paths []string
	if onePath != "" {
		paths = []string{onePath}
	}
	if cmp.Diff, err = s.git.DiffCommits(ctx, repo.ID, mb, h.SHA, defaultDiffLimits, paths...); err != nil {
		return nil, err
	}
	if cmp.Ahead > 0 {
		res, err := s.git.MergeTrees(ctx, repo.ID, b.SHA, h.SHA)
		if err != nil {
			return nil, err
		}
		cmp.Mergeable, cmp.Conflicts = &res.Clean, res.Conflicts
	}
	if bName, hName := b.Ref, h.Ref; bName != "" && hName != "" {
		var pl PullLink
		err := s.d.Pool.QueryRow(ctx, `SELECT number, title FROM repos_pulls
			WHERE repo_id = $1 AND head_branch = $2 AND base_branch = $3 AND state = 'open'`, repo.ID, hName, bName).Scan(&pl.Number, &pl.Title)
		if err == nil {
			cmp.OpenPull = &pl
		}
	}
	return cmp, nil
}

// ---- helpers -------------------------------------------------------------------

// languageOf maps a file name to a highlighter language id.
func languageOf(p string) string {
	base := strings.ToLower(path.Base(p))
	switch base {
	case "dockerfile", "containerfile":
		return "dockerfile"
	case "makefile", "gnumakefile":
		return "makefile"
	case "go.mod", "go.sum":
		return "go"
	}
	ext := strings.TrimPrefix(path.Ext(base), ".")
	if l, ok := languages[ext]; ok {
		return l
	}
	return "plaintext"
}

var languages = map[string]string{
	"go": "go", "js": "javascript", "mjs": "javascript", "cjs": "javascript", "jsx": "javascript",
	"ts": "typescript", "tsx": "typescript", "mts": "typescript", "cts": "typescript",
	"py": "python", "rb": "ruby", "rs": "rust", "java": "java", "kt": "kotlin", "kts": "kotlin", "swift": "swift",
	"c": "c", "h": "c", "cc": "cpp", "cpp": "cpp", "cxx": "cpp", "hpp": "cpp", "hh": "cpp", "cs": "csharp",
	"php": "php", "sh": "bash", "bash": "bash", "zsh": "bash", "ps1": "powershell",
	"json": "json", "jsonc": "json", "yml": "yaml", "yaml": "yaml", "toml": "ini", "ini": "ini", "cfg": "ini",
	"md": "markdown", "markdown": "markdown", "html": "xml", "htm": "xml", "xml": "xml", "svg": "xml", "vue": "xml",
	"css": "css", "scss": "scss", "less": "less", "sql": "sql", "graphql": "graphql", "gql": "graphql",
	"dart": "dart", "lua": "lua", "r": "r", "scala": "scala", "ex": "elixir", "exs": "elixir", "erl": "erlang",
	"hs": "haskell", "clj": "clojure", "proto": "protobuf", "tf": "ini", "diff": "diff", "patch": "diff",
}
