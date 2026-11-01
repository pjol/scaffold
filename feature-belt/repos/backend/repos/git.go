package repos

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// The git engine. Every repository is a bare repository on disk under
// REPOS_DIR, named <repo id>.git. All work goes through the git CLI (2.38 or
// newer, for merge-tree --write-tree): user input travels only as arguments
// or stdin, never through a shell; revisions are resolved to object ids
// before use and paths are validated; every process has a deadline.

// ZeroOID is git's "no object" id (ref creation and deletion).
const ZeroOID = "0000000000000000000000000000000000000000"

// emptyTree is the id of the tree with no entries.
const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// minGitMajor, minGitMinor: merge-tree --write-tree arrived in 2.38.
const (
	minGitMajor = 2
	minGitMinor = 38
)

var (
	// ErrNotFound: the revision, path or object doesn't exist.
	ErrNotFound = errors.New("repos: not found")
	// ErrRefMoved: update-ref's old-value check failed (someone else wrote first).
	ErrRefMoved = errors.New("repos: ref moved")
	// ErrTooLarge: output passed the caller's cap.
	ErrTooLarge = errors.New("repos: too large")
)

// GitError is a git process that failed.
type GitError struct {
	Args   []string
	Code   int
	Stderr string
}

func (e *GitError) Error() string {
	return fmt.Sprintf("git %s: exit %d: %s", strings.Join(e.Args, " "), e.Code, strings.TrimSpace(e.Stderr))
}

// Git runs the git CLI against the bare repositories under Root.
type Git struct {
	Bin     string        // git executable
	Root    string        // REPOS_DIR, absolute
	Timeout time.Duration // per process
	home    string        // an empty HOME, so no user config leaks in
}

// NewGit prepares the engine: the repositories directory and an empty home.
func NewGit(bin, root string, timeout time.Duration) (*Git, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o750); err != nil {
		return nil, err
	}
	home := filepath.Join(abs, ".home")
	if err := os.MkdirAll(home, 0o750); err != nil {
		return nil, err
	}
	if bin == "" {
		bin = "git"
	}
	if p, err := exec.LookPath(bin); err == nil {
		bin = p
	}
	return &Git{Bin: bin, Root: abs, Timeout: timeout, home: home}, nil
}

// Dir is the bare repository's directory.
func (g *Git) Dir(id uuid.UUID) string { return filepath.Join(g.Root, id.String()+".git") }

// baseEnv isolates git from the host's configuration and terminal.
func (g *Git) baseEnv() []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + g.home,
		"XDG_CONFIG_HOME=" + g.home,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_ATTR_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_OPTIONAL_LOCKS=0",
		"LC_ALL=C",
	}
}

type runOpts struct {
	stdin  io.Reader
	env    []string
	max    int64 // stdout cap in bytes (0: 64 MiB)
	okCode []int // exit codes that aren't failures (merge-tree's 1, grep's 1)
}

// run executes git with args in repository id's directory (uuid.Nil: no
// repository) and returns stdout and the exit code.
func (g *Git) run(ctx context.Context, id uuid.UUID, o runOpts, args ...string) ([]byte, int, error) {
	ctx, cancel := context.WithTimeout(ctx, g.Timeout)
	defer cancel()
	full := append([]string{"-c", "core.quotePath=false"}, args...)
	if id != uuid.Nil {
		full = append([]string{"--git-dir=" + g.Dir(id)}, full...)
	}
	cmd := exec.CommandContext(ctx, g.Bin, full...)
	cmd.Env = append(g.baseEnv(), o.env...)
	cmd.Stdin = o.stdin
	cmd.WaitDelay = 2 * time.Second
	max := o.max
	if max <= 0 {
		max = 64 << 20
	}
	out := &capped{max: max}
	var stderr capped
	stderr.max = 32 << 10
	cmd.Stdout, cmd.Stderr = out, &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			if ctx.Err() != nil {
				return nil, -1, fmt.Errorf("git %s: %w", args[0], ctx.Err())
			}
			return nil, -1, err
		}
		code = exit.ExitCode()
		if out.over {
			return out.buf.Bytes(), code, ErrTooLarge
		}
		for _, ok := range o.okCode {
			if code == ok {
				return out.buf.Bytes(), code, nil
			}
		}
		if ctx.Err() != nil {
			return nil, code, fmt.Errorf("git %s: %w", args[0], ctx.Err())
		}
		return nil, code, &GitError{Args: args, Code: code, Stderr: stderr.buf.String()}
	}
	if out.over {
		return out.buf.Bytes(), code, ErrTooLarge
	}
	return out.buf.Bytes(), code, nil
}

func (g *Git) output(ctx context.Context, id uuid.UUID, args ...string) ([]byte, error) {
	b, _, err := g.run(ctx, id, runOpts{}, args...)
	return b, err
}

// capped keeps the first max bytes and notes that more came.
type capped struct {
	buf  bytes.Buffer
	max  int64
	over bool
}

func (c *capped) Write(p []byte) (int, error) {
	room := c.max - int64(c.buf.Len())
	if room <= 0 {
		c.over = true
		return 0, ErrTooLarge // makes git stop writing (SIGPIPE/EPIPE)
	}
	if int64(len(p)) > room {
		c.buf.Write(p[:room])
		c.over = true
		return int(room), ErrTooLarge
	}
	return c.buf.Write(p)
}

// ---- boot ----------------------------------------------------------------------

var versionRe = regexp.MustCompile(`git version (\d+)\.(\d+)(?:\.(\d+))?`)

// Version returns the git version ("2.39.5") and whether it is new enough.
func (g *Git) Version(ctx context.Context) (string, bool, error) {
	out, err := g.output(ctx, uuid.Nil, "version")
	if err != nil {
		return "", false, err
	}
	m := versionRe.FindStringSubmatch(string(out))
	if m == nil {
		return strings.TrimSpace(string(out)), false, nil
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	v := m[1] + "." + m[2]
	if m[3] != "" {
		v += "." + m[3]
	}
	return v, major > minGitMajor || (major == minGitMajor && minor >= minGitMinor), nil
}

// ---- repositories --------------------------------------------------------------

// Init creates an empty bare repository whose HEAD names defaultBranch.
func (g *Git) Init(ctx context.Context, id uuid.UUID, defaultBranch string) error {
	dir := g.Dir(id)
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("repos: %s already exists", dir)
	}
	if _, err := g.output(ctx, uuid.Nil, "init", "--quiet", "--bare", "--initial-branch="+defaultBranch, dir); err != nil {
		return err
	}
	for _, kv := range [][2]string{
		{"receive.fsckObjects", "true"}, // refuse malformed objects on push
		{"receive.denyDeleteCurrent", "true"},
		{"uploadpack.allowFilter", "true"}, // partial clones
		{"core.logAllRefUpdates", "true"},  // reflogs: a record of every ref change
		{"gc.auto", "6700"},
	} {
		if _, err := g.output(ctx, id, "config", kv[0], kv[1]); err != nil {
			return err
		}
	}
	// Bare repositories get sample hooks and a description; none are used.
	_ = os.RemoveAll(filepath.Join(dir, "hooks"))
	_ = os.Remove(filepath.Join(dir, "description"))
	return nil
}

// Remove deletes a repository from disk.
func (g *Git) Remove(id uuid.UUID) error { return os.RemoveAll(g.Dir(id)) }

// Exists reports whether the repository is on disk.
func (g *Git) Exists(id uuid.UUID) bool {
	st, err := os.Stat(filepath.Join(g.Dir(id), "HEAD"))
	return err == nil && !st.IsDir()
}

// SetHead points HEAD at a branch (the default branch).
func (g *Git) SetHead(ctx context.Context, id uuid.UUID, branch string) error {
	_, err := g.output(ctx, id, "symbolic-ref", "HEAD", "refs/heads/"+branch)
	return err
}

// GC packs loose objects when git thinks it's time (cheap when it isn't).
func (g *Git) GC(ctx context.Context, id uuid.UUID) error {
	_, err := g.output(ctx, id, "gc", "--auto", "--quiet")
	return err
}

// SizeBytes is the repository's size on disk (packs and loose objects).
func (g *Git) SizeBytes(ctx context.Context, id uuid.UUID) (int64, error) {
	out, err := g.output(ctx, id, "count-objects", "-v")
	if err != nil {
		return 0, err
	}
	var kib int64
	for _, line := range strings.Split(string(out), "\n") {
		k, v, ok := strings.Cut(line, ": ")
		if ok && (k == "size" || k == "size-pack") {
			n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			kib += n
		}
	}
	return kib << 10, nil
}

// ---- names ---------------------------------------------------------------------

// CheckRefFormat validates a branch or tag name with git check-ref-format
// (plus our own rules: no leading dash, no "HEAD", length).
func (g *Git) CheckRefFormat(ctx context.Context, kind, name string) error {
	if name == "" || len(name) > 200 || strings.HasPrefix(name, "-") || name == "HEAD" ||
		strings.HasPrefix(name, "refs/") || !utf8.ValidString(name) || strings.ContainsAny(name, "\x00\n\r") {
		return errBadRefName
	}
	_, err := g.output(ctx, uuid.Nil, "check-ref-format", "refs/"+kind+"/"+name)
	if err != nil {
		var ge *GitError
		if errors.As(err, &ge) {
			return errBadRefName
		}
		return err
	}
	return nil
}

var errBadRefName = errors.New("repos: invalid ref name")

var hexOID = regexp.MustCompile(`^[0-9a-f]{40}$|^[0-9a-f]{64}$`)

// IsOID reports whether s is a full object id.
func IsOID(s string) bool { return hexOID.MatchString(s) }

// CleanPath validates a repository path from a client: relative, slash
// separated, no empty, "." or ".." parts, no NUL or newline, no .git part.
// "" is the root.
func CleanPath(p string) (string, error) {
	p = strings.Trim(p, "/")
	if p == "" {
		return "", nil
	}
	if len(p) > 4096 || !utf8.ValidString(p) || strings.ContainsAny(p, "\x00\n\r\\") {
		return "", errBadPath
	}
	for _, part := range strings.Split(p, "/") {
		if part == "" || part == "." || part == ".." || len(part) > 255 || strings.EqualFold(part, ".git") {
			return "", errBadPath
		}
	}
	return p, nil
}

var errBadPath = errors.New("repos: invalid path")

// ---- reading -------------------------------------------------------------------

// ResolveCommit turns a branch, tag or commit id into a commit id.
func (g *Git) ResolveCommit(ctx context.Context, id uuid.UUID, rev string) (string, error) {
	if rev == "" || strings.HasPrefix(rev, "-") || strings.ContainsAny(rev, "\x00\n\r ") || len(rev) > 255 {
		return "", ErrNotFound
	}
	if !IsOID(rev) && !strings.HasPrefix(rev, "refs/") {
		// A bare name: a branch first, then a tag (never a revision expression).
		for _, full := range []string{"refs/heads/" + rev, "refs/tags/" + rev} {
			if sha, err := g.peel(ctx, id, full); err == nil {
				return sha, nil
			} else if !errors.Is(err, ErrNotFound) {
				return "", err
			}
		}
		if len(rev) >= 7 && len(rev) < 40 && isHex(rev) {
			return g.peel(ctx, id, rev) // abbreviated commit id
		}
		return "", ErrNotFound
	}
	return g.peel(ctx, id, rev)
}

func (g *Git) peel(ctx context.Context, id uuid.UUID, rev string) (string, error) {
	out, code, err := g.run(ctx, id, runOpts{okCode: []int{1, 128}}, "rev-parse", "--verify", "--quiet", "--end-of-options", rev+"^{commit}")
	if err != nil {
		return "", err
	}
	sha := strings.TrimSpace(string(out))
	if code != 0 || !IsOID(sha) {
		return "", ErrNotFound
	}
	return sha, nil
}

func isHex(s string) bool {
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// RefValue reads one full ref ("refs/heads/main"): its object id, or ""
// when it doesn't exist.
func (g *Git) RefValue(ctx context.Context, id uuid.UUID, ref string) (string, error) {
	out, code, err := g.run(ctx, id, runOpts{okCode: []int{1, 128}}, "rev-parse", "--verify", "--quiet", "--end-of-options", ref)
	if err != nil {
		return "", err
	}
	if code != 0 {
		return "", nil
	}
	return strings.TrimSpace(string(out)), nil
}

// Ref is a branch or tag.
type Ref struct {
	Name      string    `json:"name"` // "main", "v1.0"
	Kind      string    `json:"kind"` // branch | tag
	SHA       string    `json:"sha"`  // commit (tags peeled)
	Subject   string    `json:"subject"`
	Author    string    `json:"author"`
	UpdatedAt time.Time `json:"updated_at"` // committer date of the commit
}

// Refs lists branches and tags with their commit's subject, newest first.
func (g *Git) Refs(ctx context.Context, id uuid.UUID) ([]Ref, error) {
	out, err := g.output(ctx, id, "for-each-ref", "--sort=-committerdate",
		"--format=%(refname)%00%(objectname)%00%(*objectname)%00%(subject)%00%(*subject)%00%(authorname)%00%(*authorname)%00%(committerdate:unix)%00%(*committerdate:unix)",
		"refs/heads/", "refs/tags/")
	if err != nil {
		return nil, err
	}
	var refs []Ref
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		f := strings.Split(line, "\x00")
		if len(f) < 9 {
			continue
		}
		r := Ref{SHA: f[1], Subject: f[3], Author: f[5]}
		ts := f[7]
		if f[2] != "" { // annotated tag: use the commit it points at
			r.SHA, r.Subject, r.Author, ts = f[2], f[4], f[6], f[8]
		}
		if n, err := strconv.ParseInt(ts, 10, 64); err == nil {
			r.UpdatedAt = time.Unix(n, 0).UTC()
		}
		switch {
		case strings.HasPrefix(f[0], "refs/heads/"):
			r.Kind, r.Name = "branch", strings.TrimPrefix(f[0], "refs/heads/")
		case strings.HasPrefix(f[0], "refs/tags/"):
			r.Kind, r.Name = "tag", strings.TrimPrefix(f[0], "refs/tags/")
		default:
			continue
		}
		refs = append(refs, r)
	}
	return refs, nil
}

// HasBranches reports whether the repository has any branch yet.
func (g *Git) HasBranches(ctx context.Context, id uuid.UUID) (bool, error) {
	out, err := g.output(ctx, id, "for-each-ref", "--count=1", "--format=%(refname)", "refs/heads/")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) != "", nil
}

// TreeEntry is one row of a directory listing.
type TreeEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"` // blob | tree | commit (submodule) | symlink
	Mode string `json:"mode"`
	OID  string `json:"oid"`
	Size int64  `json:"size"` // blobs only; -1 otherwise
}

// ListTree lists the directory at dir ("" for the root) in commit sha.
func (g *Git) ListTree(ctx context.Context, id uuid.UUID, sha, dir string) ([]TreeEntry, error) {
	treeish := sha + "^{tree}"
	if dir != "" {
		treeish = sha + ":" + dir
	}
	if t, err := g.objectType(ctx, id, treeish); err != nil {
		return nil, err
	} else if t != "tree" {
		return nil, ErrNotFound
	}
	out, err := g.output(ctx, id, "ls-tree", "-z", "--long", "--end-of-options", treeish)
	if err != nil {
		return nil, err
	}
	entries := parseLsTree(out, dir)
	sortEntries(entries)
	return entries, nil
}

// parseLsTree reads `ls-tree -z [--long]` output.
func parseLsTree(out []byte, dir string) []TreeEntry {
	var entries []TreeEntry
	for _, rec := range bytes.Split(out, []byte{0}) {
		if len(rec) == 0 {
			continue
		}
		meta, name, ok := bytes.Cut(rec, []byte{'\t'})
		if !ok {
			continue
		}
		f := strings.Fields(string(meta))
		if len(f) < 3 {
			continue
		}
		e := TreeEntry{Mode: f[0], Type: f[1], OID: f[2], Name: string(name), Size: -1}
		if len(f) >= 4 && f[3] != "-" {
			e.Size, _ = strconv.ParseInt(f[3], 10, 64)
		}
		if e.Mode == "120000" {
			e.Type = "symlink"
		}
		e.Path = e.Name
		if dir != "" {
			e.Path = dir + "/" + e.Name
		}
		entries = append(entries, e)
	}
	return entries
}

// sortEntries puts directories first, then names case-insensitively.
func sortEntries(entries []TreeEntry) {
	rank := func(t string) int {
		if t == "tree" {
			return 0
		}
		return 1
	}
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0; j-- {
			a, b := entries[j-1], entries[j]
			if rank(a.Type) < rank(b.Type) || (rank(a.Type) == rank(b.Type) && strings.ToLower(a.Name) <= strings.ToLower(b.Name)) {
				break
			}
			entries[j-1], entries[j] = b, a
		}
	}
}

func (g *Git) objectType(ctx context.Context, id uuid.UUID, obj string) (string, error) {
	out, code, err := g.run(ctx, id, runOpts{okCode: []int{1, 128}}, "cat-file", "-t", "--end-of-options", obj)
	if err != nil {
		return "", err
	}
	if code != 0 {
		return "", ErrNotFound
	}
	return strings.TrimSpace(string(out)), nil
}

// Entry looks up one path in a commit (a file, directory, link or submodule).
func (g *Git) Entry(ctx context.Context, id uuid.UUID, sha, p string) (TreeEntry, error) {
	if p == "" {
		return TreeEntry{Type: "tree", Mode: "040000", Size: -1}, nil
	}
	out, err := g.output(ctx, id, "ls-tree", "-z", "--long", "--full-tree", "--end-of-options", sha, "--", p)
	if err != nil {
		return TreeEntry{}, err
	}
	entries := parseLsTree(out, "")
	for _, e := range entries {
		if e.Name == p {
			e.Path = p
			e.Name = path.Base(p)
			return e, nil
		}
	}
	return TreeEntry{}, ErrNotFound
}

// ReadBlob returns up to max bytes of a blob by object id and whether it
// was cut short.
func (g *Git) ReadBlob(ctx context.Context, id uuid.UUID, oid string, max int64) ([]byte, bool, error) {
	if !IsOID(oid) {
		return nil, false, ErrNotFound
	}
	out, _, err := g.run(ctx, id, runOpts{max: max}, "cat-file", "blob", oid)
	if errors.Is(err, ErrTooLarge) {
		return out, true, nil
	}
	return out, false, err
}

// StreamBlob writes a whole blob to w (raw downloads).
func (g *Git) StreamBlob(ctx context.Context, id uuid.UUID, oid string, w io.Writer) error {
	if !IsOID(oid) {
		return ErrNotFound
	}
	ctx, cancel := context.WithTimeout(ctx, g.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, g.Bin, "--git-dir="+g.Dir(id), "cat-file", "blob", oid)
	cmd.Env = g.baseEnv()
	cmd.Stdout = w
	cmd.WaitDelay = 2 * time.Second
	return cmd.Run()
}

// ListPaths lists every file path in a commit (for the file finder and
// search), at most max.
func (g *Git) ListPaths(ctx context.Context, id uuid.UUID, sha string, max int) ([]string, bool, error) {
	out, _, err := g.run(ctx, id, runOpts{max: int64(max) * 300}, "ls-tree", "-r", "-z", "--name-only", "--full-tree", "--end-of-options", sha)
	truncated := errors.Is(err, ErrTooLarge)
	if err != nil && !truncated {
		return nil, false, err
	}
	var paths []string
	for _, p := range bytes.Split(out, []byte{0}) {
		if len(p) == 0 {
			continue
		}
		if len(paths) == max {
			truncated = true
			break
		}
		paths = append(paths, string(p))
	}
	if truncated && len(paths) > 0 && err != nil {
		paths = paths[:len(paths)-1] // the last one may be cut mid-name
	}
	return paths, truncated, nil
}

// ---- history -------------------------------------------------------------------

// Signature is a commit's author or committer as recorded in git.
type Signature struct {
	Name  string    `json:"name"`
	Email string    `json:"email"`
	At    time.Time `json:"at"`
}

// Commit is one commit's metadata.
type Commit struct {
	SHA       string    `json:"sha"`
	Parents   []string  `json:"parents"`
	Author    Signature `json:"author"`
	Committer Signature `json:"committer"`
	Subject   string    `json:"subject"`
	Body      string    `json:"body"`
}

const commitFormat = "--format=%H%x00%P%x00%an%x00%ae%x00%at%x00%cn%x00%ce%x00%ct%x00%s%x00%b%x1e"

func parseCommits(out []byte) []Commit {
	var list []Commit
	for _, rec := range bytes.Split(out, []byte{0x1e}) {
		rec = bytes.TrimLeft(rec, "\n")
		if len(rec) == 0 {
			continue
		}
		f := strings.SplitN(string(rec), "\x00", 10)
		if len(f) < 10 {
			continue
		}
		c := Commit{SHA: f[0], Parents: strings.Fields(f[1]), Subject: f[8], Body: strings.TrimSpace(f[9])}
		c.Author = Signature{Name: f[2], Email: f[3], At: unixTime(f[4])}
		c.Committer = Signature{Name: f[5], Email: f[6], At: unixTime(f[7])}
		if c.Parents == nil {
			c.Parents = []string{}
		}
		list = append(list, c)
	}
	return list
}

func unixTime(s string) time.Time {
	n, _ := strconv.ParseInt(s, 10, 64)
	return time.Unix(n, 0).UTC()
}

// Log lists commits reachable from sha (optionally touching p), newest
// first, skipping skip. sha pins the walk, so pages stay stable.
func (g *Git) Log(ctx context.Context, id uuid.UUID, sha, p string, skip, limit int) ([]Commit, error) {
	args := []string{"log", commitFormat, "--skip=" + strconv.Itoa(skip), "--max-count=" + strconv.Itoa(limit), "--end-of-options", sha}
	if p != "" {
		args = append(args, "--", p)
	}
	out, err := g.output(ctx, id, args...)
	if err != nil {
		return nil, err
	}
	return parseCommits(out), nil
}

// RangeCommits lists commits in head that aren't in base (base..head),
// oldest first, at most max.
func (g *Git) RangeCommits(ctx context.Context, id uuid.UUID, base, head string, max int) ([]Commit, error) {
	args := []string{"log", commitFormat, "--reverse", "--max-count=" + strconv.Itoa(max), "--end-of-options", head}
	if base != "" && base != ZeroOID {
		args = append(args, "^"+base)
	}
	out, err := g.output(ctx, id, args...)
	if err != nil {
		return nil, err
	}
	return parseCommits(out), nil
}

// CountRange counts commits in head that aren't in base.
func (g *Git) CountRange(ctx context.Context, id uuid.UUID, base, head string) (int, error) {
	args := []string{"rev-list", "--count", "--end-of-options", head}
	if base != "" && base != ZeroOID {
		args = append(args, "^"+base)
	}
	out, err := g.output(ctx, id, args...)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(out)))
}

// GetCommit reads one commit.
func (g *Git) GetCommit(ctx context.Context, id uuid.UUID, sha string) (Commit, error) {
	out, err := g.output(ctx, id, "log", commitFormat, "--max-count=1", "--end-of-options", sha)
	if err != nil {
		return Commit{}, err
	}
	list := parseCommits(out)
	if len(list) == 0 {
		return Commit{}, ErrNotFound
	}
	return list[0], nil
}

// LastCommits finds the newest commit touching each entry of dir, walking
// at most maxCommits commits from sha. Entries it doesn't reach are absent.
func (g *Git) LastCommits(ctx context.Context, id uuid.UUID, sha, dir string, names []string, maxCommits int) (map[string]Commit, error) {
	want := make(map[string]bool, len(names))
	for _, n := range names {
		want[n] = true
	}
	// Fields split by 0x1f and the header ends with 0x1d, so NUL separates
	// only the header and the paths: <header>\x1d\0\n<path>\0<path>\0<header>…
	args := []string{"--git-dir=" + g.Dir(id), "-c", "core.quotePath=false", "log",
		"--format=%H%x1f%an%x1f%ae%x1f%at%x1f%s%x1d", "--name-only", "-z",
		"--max-count=" + strconv.Itoa(maxCommits), "--end-of-options", sha}
	if dir != "" {
		args = append(args, "--", dir)
	}
	ctx, cancel := context.WithTimeout(ctx, g.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, g.Bin, args...)
	cmd.Env = g.baseEnv()
	cmd.WaitDelay = 2 * time.Second
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	found := map[string]Commit{}
	prefix := ""
	if dir != "" {
		prefix = dir + "/"
	}
	var cur Commit
	r := bufio.NewReaderSize(stdout, 64<<10)
	for len(found) < len(want) {
		tok, err := r.ReadString(0)
		tok = strings.TrimLeft(strings.TrimSuffix(tok, "\x00"), "\n")
		if tok != "" {
			if h, ok := strings.CutSuffix(tok, "\x1d"); ok && len(h) > 41 && h[40] == 0x1f && isHex(h[:40]) {
				f := strings.SplitN(h, "\x1f", 5)
				if len(f) == 5 {
					cur = Commit{SHA: f[0], Subject: f[4], Author: Signature{Name: f[1], Email: f[2], At: unixTime(f[3])}, Parents: []string{}}
				}
			} else if cur.SHA != "" && strings.HasPrefix(tok, prefix) {
				name, _, _ := strings.Cut(strings.TrimPrefix(tok, prefix), "/")
				if _, seen := found[name]; want[name] && !seen {
					found[name] = cur
				}
			}
		}
		if err != nil {
			break
		}
	}
	return found, nil
}

// MergeBase is the best common ancestor of a and b ("" when unrelated).
func (g *Git) MergeBase(ctx context.Context, id uuid.UUID, a, b string) (string, error) {
	out, code, err := g.run(ctx, id, runOpts{okCode: []int{1}}, "merge-base", "--end-of-options", a, b)
	if err != nil {
		return "", err
	}
	if code != 0 {
		return "", nil
	}
	return strings.TrimSpace(string(out)), nil
}

// IsAncestor reports whether a is an ancestor of (or equal to) b. env adds
// object directories (a push's quarantine).
func (g *Git) IsAncestor(ctx context.Context, id uuid.UUID, a, b string, env ...string) (bool, error) {
	_, code, err := g.run(ctx, id, runOpts{okCode: []int{1}, env: env}, "merge-base", "--is-ancestor", "--end-of-options", a, b)
	if err != nil {
		return false, err
	}
	return code == 0, nil
}

// ---- blame ---------------------------------------------------------------------

// BlameRange is a run of lines last changed by one commit.
type BlameRange struct {
	SHA     string `json:"sha"`
	Start   int    `json:"start"` // 1-based line in the file
	Lines   int    `json:"lines"`
	Author  string `json:"author"`
	At      int64  `json:"at"` // unix seconds
	Subject string `json:"subject"`
}

// Blame attributes each line of p at sha to the commit that last changed it.
func (g *Git) Blame(ctx context.Context, id uuid.UUID, sha, p string) ([]BlameRange, error) {
	if !IsOID(sha) { // git blame has no --end-of-options in 2.38/2.39: only ids reach it
		return nil, ErrNotFound
	}
	out, err := g.output(ctx, id, "blame", "--porcelain", sha, "--", p)
	if err != nil {
		var ge *GitError
		if errors.As(err, &ge) && strings.Contains(ge.Stderr, "no such path") {
			return nil, ErrNotFound
		}
		return nil, err
	}
	type info struct {
		author, subject string
		at              int64
	}
	infos := map[string]*info{}
	var ranges []BlameRange
	lines := strings.Split(string(out), "\n")
	for i := 0; i < len(lines); i++ {
		f := strings.Fields(lines[i])
		if len(f) < 3 || !IsOID(f[0]) {
			continue
		}
		commit := f[0]
		final, _ := strconv.Atoi(f[2])
		n := 1
		if len(f) >= 4 {
			n, _ = strconv.Atoi(f[3])
		}
		in := infos[commit]
		if in == nil {
			in = &info{}
			infos[commit] = in
		}
		// Header lines until the content line (starts with a tab).
		for i+1 < len(lines) && !strings.HasPrefix(lines[i+1], "\t") {
			i++
			k, v, _ := strings.Cut(lines[i], " ")
			switch k {
			case "author":
				in.author = v
			case "author-time":
				in.at, _ = strconv.ParseInt(v, 10, 64)
			case "summary":
				in.subject = v
			}
		}
		i++ // content line
		if len(f) >= 4 {
			ranges = append(ranges, BlameRange{SHA: commit, Start: final, Lines: n})
		}
	}
	for k := range ranges {
		in := infos[ranges[k].SHA]
		ranges[k].Author, ranges[k].At, ranges[k].Subject = in.author, in.at, in.subject
	}
	// Merge adjacent groups from the same commit.
	var merged []BlameRange
	for _, r := range ranges {
		if n := len(merged); n > 0 && merged[n-1].SHA == r.SHA && merged[n-1].Start+merged[n-1].Lines == r.Start {
			merged[n-1].Lines += r.Lines
			continue
		}
		merged = append(merged, r)
	}
	return merged, nil
}

// ---- search --------------------------------------------------------------------

// GrepMatch is one matching line.
type GrepMatch struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

// Grep finds lines containing query (fixed string, case-insensitive) in
// the files of sha, at most max matches and perFile per file. Binary files
// are skipped; long lines are cut.
func (g *Git) Grep(ctx context.Context, id uuid.UUID, sha, query string, max, perFile int) ([]GrepMatch, bool, error) {
	if !IsOID(sha) { // git grep has no --end-of-options in 2.38/2.39: only ids reach it
		return nil, false, ErrNotFound
	}
	out, _, err := g.run(ctx, id, runOpts{okCode: []int{1}, max: int64(max) * 600},
		"grep", "-n", "-I", "-z", "--fixed-strings", "--ignore-case", "--full-name",
		"--max-count="+strconv.Itoa(perFile), "-e", query, sha, "--")
	truncated := errors.Is(err, ErrTooLarge)
	if err != nil && !truncated {
		return nil, false, err
	}
	prefix := sha + ":"
	var matches []GrepMatch
	for _, line := range bytes.Split(out, []byte{'\n'}) {
		f := bytes.SplitN(line, []byte{0}, 3)
		if len(f) < 3 {
			continue
		}
		if len(matches) == max {
			truncated = true
			break
		}
		n, _ := strconv.Atoi(string(f[1]))
		text := string(f[2])
		if len(text) > 300 {
			text = truncateUTF8(text, 300)
		}
		matches = append(matches, GrepMatch{Path: strings.TrimPrefix(string(f[0]), prefix), Line: n, Text: text})
	}
	return matches, truncated, nil
}

func truncateUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
