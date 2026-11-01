package repos

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Server-side writes without a worktree: blobs with hash-object, trees with
// mktree (only the directories a change touches are rewritten), commits with
// commit-tree, and refs with update-ref and an old-value check, so two
// concurrent writers can't overwrite each other.

// Change is one edit to a commit's tree.
type Change struct {
	Op      string // put | delete
	Path    string
	Content []byte // put
	OID     string // put: an existing blob instead of Content (moves)
	// Mode for put: "" keeps an existing file's mode (100644 for new files);
	// "100755" makes it executable.
	Mode string
}

// ChangeError explains why a change can't apply ("a.txt is a directory").
type ChangeError struct {
	Path string
	Msg  string
}

func (e *ChangeError) Error() string { return e.Path + ": " + e.Msg }

// HashBlob stores content as a blob and returns its id.
func (g *Git) HashBlob(ctx context.Context, id uuid.UUID, content []byte) (string, error) {
	out, _, err := g.run(ctx, id, runOpts{stdin: bytes.NewReader(content)}, "hash-object", "-t", "blob", "-w", "--no-filters", "--stdin")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// treeNode is a directory being edited: its entries by name, and the
// subdirectories changes descend into.
type treeNode struct {
	entries  map[string]TreeEntry
	children map[string]*treeNode
	dirty    bool
}

func (g *Git) loadTree(ctx context.Context, id uuid.UUID, oid string) (*treeNode, error) {
	n := &treeNode{entries: map[string]TreeEntry{}, children: map[string]*treeNode{}}
	if oid == "" || oid == emptyTree {
		return n, nil
	}
	out, err := g.output(ctx, id, "ls-tree", "-z", "--end-of-options", oid)
	if err != nil {
		return nil, err
	}
	for _, e := range parseLsTree(out, "") {
		if e.Type == "symlink" {
			e.Type = "blob" // mktree wants the object type
		}
		n.entries[e.Name] = e
	}
	return n, nil
}

// ApplyChanges writes the tree that results from applying changes to
// baseTree ("" for an empty repository) and returns its id. Deleting the
// last file of a directory removes the directory.
func (g *Git) ApplyChanges(ctx context.Context, id uuid.UUID, baseTree string, changes []Change) (string, error) {
	root, err := g.loadTree(ctx, id, baseTree)
	if err != nil {
		return "", err
	}
	// Directory to descend into, loading it on first use.
	descend := func(n *treeNode, name, full string, create bool) (*treeNode, error) {
		if c, ok := n.children[name]; ok {
			return c, nil
		}
		e, exists := n.entries[name]
		switch {
		case exists && e.Type != "tree":
			return nil, &ChangeError{Path: full, Msg: "is a file, not a folder"}
		case !exists && !create:
			return nil, &ChangeError{Path: full, Msg: "doesn't exist"}
		}
		oid := ""
		if exists {
			oid = e.OID
		}
		c, err := g.loadTree(ctx, id, oid)
		if err != nil {
			return nil, err
		}
		n.children[name] = c
		return c, nil
	}
	for _, ch := range changes {
		parts := strings.Split(ch.Path, "/")
		n := root
		for i, part := range parts[:len(parts)-1] {
			n.dirty = true
			if n, err = descend(n, part, strings.Join(parts[:i+1], "/"), ch.Op == "put"); err != nil {
				return "", err
			}
		}
		n.dirty = true
		name := parts[len(parts)-1]
		existing, exists := n.entries[name]
		if _, isDir := n.children[name]; isDir || (exists && existing.Type == "tree") {
			return "", &ChangeError{Path: ch.Path, Msg: "is a folder"}
		}
		switch ch.Op {
		case "put":
			if exists && existing.Type == "commit" {
				return "", &ChangeError{Path: ch.Path, Msg: "is a submodule and can't be edited here"}
			}
			oid := ch.OID
			if oid == "" {
				var err error
				if oid, err = g.HashBlob(ctx, id, ch.Content); err != nil {
					return "", err
				}
			}
			mode := ch.Mode
			if mode == "" {
				mode = "100644"
				if exists && (existing.Mode == "100755" || existing.Mode == "120000") {
					mode = existing.Mode
				}
			}
			if mode != "100644" && mode != "100755" && mode != "120000" {
				return "", &ChangeError{Path: ch.Path, Msg: "has an unsupported file mode"}
			}
			n.entries[name] = TreeEntry{Name: name, Mode: mode, Type: "blob", OID: oid}
		case "delete":
			if !exists {
				return "", &ChangeError{Path: ch.Path, Msg: "doesn't exist"}
			}
			delete(n.entries, name)
		default:
			return "", fmt.Errorf("repos: unknown change op %q", ch.Op)
		}
	}
	oid, _, err := g.writeTree(ctx, id, root)
	if err != nil {
		return "", err
	}
	if oid == "" {
		return emptyTree, nil
	}
	return oid, nil
}

// writeTree writes dirty directories bottom-up. It returns "" for a
// directory left empty (its parent drops it).
func (g *Git) writeTree(ctx context.Context, id uuid.UUID, n *treeNode) (string, bool, error) {
	for name, c := range n.children {
		if !c.dirty {
			continue
		}
		oid, _, err := g.writeTree(ctx, id, c)
		if err != nil {
			return "", false, err
		}
		if oid == "" {
			delete(n.entries, name)
		} else {
			n.entries[name] = TreeEntry{Name: name, Mode: "040000", Type: "tree", OID: oid}
		}
	}
	if len(n.entries) == 0 {
		return "", true, nil
	}
	names := make([]string, 0, len(n.entries))
	for name := range n.entries {
		names = append(names, name)
	}
	sort.Strings(names)
	var in bytes.Buffer
	for _, name := range names {
		e := n.entries[name]
		fmt.Fprintf(&in, "%s %s %s\t%s\x00", e.Mode, e.Type, e.OID, name)
	}
	out, _, err := g.run(ctx, id, runOpts{stdin: &in}, "mktree", "-z")
	if err != nil {
		return "", false, err
	}
	return strings.TrimSpace(string(out)), false, nil
}

// TreeOf returns a commit's tree id.
func (g *Git) TreeOf(ctx context.Context, id uuid.UUID, sha string) (string, error) {
	out, err := g.output(ctx, id, "rev-parse", "--verify", "--end-of-options", sha+"^{tree}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// Person is who a commit is by, with the time to record.
type Person struct {
	Name  string
	Email string
	At    time.Time
}

func (p Person) env(role string) []string {
	at := p.At
	if at.IsZero() {
		at = time.Now()
	}
	return []string{
		"GIT_" + role + "_NAME=" + sanitizeIdent(p.Name),
		"GIT_" + role + "_EMAIL=" + sanitizeIdent(p.Email),
		"GIT_" + role + "_DATE=" + fmt.Sprintf("%d +0000", at.Unix()),
	}
}

// sanitizeIdent drops what git refuses in a name or email.
func sanitizeIdent(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '<' || r == '>' || r == '\n' || r == '\r' || r == 0 {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if s == "" {
		return "unknown"
	}
	return s
}

// CommitTree creates a commit object (it doesn't move any ref).
func (g *Git) CommitTree(ctx context.Context, id uuid.UUID, tree string, parents []string, message string, author, committer Person) (string, error) {
	args := []string{"commit-tree", "--no-gpg-sign"}
	for _, p := range parents {
		args = append(args, "-p", p)
	}
	args = append(args, "-F", "-", "--end-of-options", tree)
	env := append(author.env("AUTHOR"), committer.env("COMMITTER")...)
	out, _, err := g.run(ctx, id, runOpts{stdin: strings.NewReader(cleanMessage(message)), env: env}, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// cleanMessage normalises line endings and trims, ending in a newline.
func cleanMessage(m string) string {
	m = strings.ReplaceAll(m, "\r\n", "\n")
	m = strings.TrimSpace(m)
	return m + "\n"
}

// UpdateRef moves ref ("refs/heads/main") to newOID only if it is still at
// oldOID (ZeroOID: it must not exist yet). ErrRefMoved means someone else
// moved it first.
func (g *Git) UpdateRef(ctx context.Context, id uuid.UUID, ref, newOID, oldOID, reason string) error {
	_, err := g.output(ctx, id, "update-ref", "-m", truncateUTF8(oneLine(reason), 200), "--end-of-options", ref, newOID, oldOID)
	return g.refErr(ctx, id, ref, oldOID, err)
}

// DeleteRef removes ref only if it is still at oldOID.
func (g *Git) DeleteRef(ctx context.Context, id uuid.UUID, ref, oldOID, reason string) error {
	_, err := g.output(ctx, id, "update-ref", "-m", truncateUTF8(oneLine(reason), 200), "-d", "--end-of-options", ref, oldOID)
	return g.refErr(ctx, id, ref, oldOID, err)
}

// ForceRef sets ref without a check (the app's own refs/pull/* refs).
func (g *Git) ForceRef(ctx context.Context, id uuid.UUID, ref, newOID string) error {
	_, err := g.output(ctx, id, "update-ref", "--end-of-options", ref, newOID)
	return err
}

func (g *Git) refErr(ctx context.Context, id uuid.UUID, ref, oldOID string, err error) error {
	if err == nil {
		return nil
	}
	var ge *GitError
	if !errors.As(err, &ge) {
		return err
	}
	cur, rerr := g.RefValue(ctx, id, ref)
	if rerr == nil && cur != oldOID && !(oldOID == ZeroOID && cur == "") {
		return ErrRefMoved
	}
	return err
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// MergeResult is the outcome of a server-side merge of two commits.
type MergeResult struct {
	Tree      string   // the merged tree (with conflict markers when not Clean)
	Clean     bool     // no conflicts
	Conflicts []string // conflicted paths
}

// MergeTrees merges ours and theirs (their merge base is found by git)
// without touching any ref or worktree.
func (g *Git) MergeTrees(ctx context.Context, id uuid.UUID, ours, theirs string) (MergeResult, error) {
	out, code, err := g.run(ctx, id, runOpts{okCode: []int{1}}, "merge-tree", "--write-tree", "--name-only", "-z", "--no-messages", "--end-of-options", ours, theirs)
	if err != nil {
		var ge *GitError
		if errors.As(err, &ge) && strings.Contains(ge.Stderr, "unrelated histories") {
			return MergeResult{}, errUnrelated
		}
		return MergeResult{}, err
	}
	toks := strings.Split(strings.TrimRight(string(out), "\x00"), "\x00")
	res := MergeResult{Tree: toks[0], Clean: code == 0}
	seen := map[string]bool{}
	for _, p := range toks[1:] {
		if p != "" && !seen[p] {
			seen[p] = true
			res.Conflicts = append(res.Conflicts, p)
		}
	}
	if res.Conflicts == nil {
		res.Conflicts = []string{}
	}
	return res, nil
}

var errUnrelated = errors.New("repos: unrelated histories")

// importFile is one file of a folder imported as a first commit.
type importFile struct {
	Path    string
	Content []byte
}

// CommitFiles writes files as a new commit on top of parent ("" for the
// first commit) and returns the commit id; it doesn't move any ref.
func (g *Git) CommitFiles(ctx context.Context, id uuid.UUID, parent string, files []importFile, message string, author, committer Person) (string, error) {
	base := ""
	if parent != "" {
		t, err := g.TreeOf(ctx, id, parent)
		if err != nil {
			return "", err
		}
		base = t
	}
	changes := make([]Change, 0, len(files))
	for _, f := range files {
		changes = append(changes, Change{Op: "put", Path: f.Path, Content: f.Content})
	}
	tree, err := g.ApplyChanges(ctx, id, base, changes)
	if err != nil {
		return "", err
	}
	var parents []string
	if parent != "" {
		parents = []string{parent}
	}
	return g.CommitTree(ctx, id, tree, parents, message, author, committer)
}

// splitPath returns a path's directory and base name.
func splitPath(p string) (string, string) {
	dir, base := path.Split(p)
	return strings.TrimSuffix(dir, "/"), base
}

// ImportSource is one file streamed into an imported first commit.
type ImportSource struct {
	Path string
	Size int64 // exact size; the stream must deliver this many bytes
	Open func(ctx context.Context) (io.ReadCloser, error)
}

// FastImport writes files as the first commit of an empty repository's
// branch in one git fast-import process (no process per file), streaming
// each file. It returns the commit id.
func (g *Git) FastImport(ctx context.Context, id uuid.UUID, branch string, files []ImportSource, message string, author, committer Person, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, g.Bin, "--git-dir="+g.Dir(id), "fast-import", "--quiet", "--done")
	cmd.Env = g.baseEnv()
	cmd.WaitDelay = 2 * time.Second
	var stderr capped
	stderr.max = 32 << 10
	cmd.Stderr = &stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", err
	}
	werr := func() error {
		w := bufio.NewWriterSize(stdin, 256<<10)
		for i, f := range files {
			rc, err := f.Open(ctx)
			if err != nil {
				return fmt.Errorf("%s: %w", f.Path, err)
			}
			fmt.Fprintf(w, "blob\nmark :%d\ndata %d\n", i+1, f.Size)
			n, err := io.CopyN(w, rc, f.Size)
			_ = rc.Close()
			if err != nil {
				return fmt.Errorf("%s: read %d of %d bytes: %w", f.Path, n, f.Size, err)
			}
			w.WriteString("\n")
		}
		at := func(p Person) string {
			t := p.At
			if t.IsZero() {
				t = time.Now()
			}
			return fmt.Sprintf("%s <%s> %d +0000", sanitizeIdent(p.Name), sanitizeIdent(p.Email), t.Unix())
		}
		msg := cleanMessage(message)
		fmt.Fprintf(w, "commit refs/heads/%s\nauthor %s\ncommitter %s\ndata %d\n%s", branch, at(author), at(committer), len(msg), msg)
		for i, f := range files {
			fmt.Fprintf(w, "M 100644 :%d %s\n", i+1, quoteImportPath(f.Path))
		}
		w.WriteString("\ndone\n")
		return w.Flush()
	}()
	_ = stdin.Close()
	if err := cmd.Wait(); err != nil {
		if werr != nil {
			return "", werr
		}
		return "", &GitError{Args: []string{"fast-import"}, Code: -1, Stderr: stderr.buf.String()}
	}
	if werr != nil {
		return "", werr
	}
	return g.RefValue(ctx, id, "refs/heads/"+branch)
}

// quoteImportPath C-quotes a path for fast-import.
func quoteImportPath(p string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch {
		case c == '"' || c == '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		case c < 0x20 || c == 0x7f:
			fmt.Fprintf(&b, "\\%03o", c)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}
