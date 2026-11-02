package repos

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"scaffoldapp/backend/internal/httpx"
)

// Helpers both adapters share: commit messages, patches, parallel reads.

// splitMessage cuts a commit message into subject and body like git log.
func splitMessage(msg string) (string, string) {
	msg = strings.ReplaceAll(msg, "\r\n", "\n")
	subject, body, _ := strings.Cut(strings.TrimLeft(msg, "\n"), "\n")
	return strings.TrimSpace(subject), strings.TrimSpace(body)
}

// patchFile is one file of a platform diff before it becomes a DiffFile.
type patchFile struct {
	Path, OldPath    string
	Status           string // added | modified | deleted | renamed | copied | type_changed
	OldMode, NewMode string
	Additions        int
	Deletions        int
	Patch            string // hunks only ("@@ … @@" onwards); "" for binary or cut
	TooLarge         bool   // the platform cut it
}

// buildDiff turns platform file patches into the engine's Diff.
func buildDiff(files []patchFile, filesTruncated bool, onlyPath string) *Diff {
	d := &Diff{Files: []DiffFile{}, FilesTruncated: filesTruncated}
	for _, f := range files {
		if onlyPath != "" && f.Path != onlyPath && f.OldPath != onlyPath {
			continue
		}
		df := DiffFile{Path: f.Path, OldPath: f.OldPath, Status: f.Status, OldMode: f.OldMode, NewMode: f.NewMode, Hunks: []DiffHunk{}}
		if df.OldPath == "" {
			df.OldPath = df.Path
		}
		if df.Status == "" {
			df.Status = "modified"
		}
		switch {
		case f.Patch != "" && len(f.Patch) <= defaultDiffLimits.MaxFileBytes:
			df.Hunks = parseHunks([]byte(f.Patch))
			if f.Additions == 0 && f.Deletions == 0 {
				for _, h := range df.Hunks {
					for _, l := range h.Lines {
						switch l.Type {
						case "add":
							df.Additions++
						case "del":
							df.Deletions++
						}
					}
				}
				f.Additions, f.Deletions = df.Additions, df.Deletions
			}
		case f.Patch != "" || f.TooLarge:
			df.Truncated = true
		case f.Additions+f.Deletions > 0:
			df.Truncated = true // the platform left the patch out (too large)
		case f.Status != "renamed": // no patch and no line counts: binary (or empty)
			df.Binary = true
		}
		df.Additions, df.Deletions = f.Additions, f.Deletions
		d.Additions += df.Additions
		d.Deletions += df.Deletions
		d.Files = append(d.Files, df)
	}
	return d
}

// parallel runs fn for each index with at most n at once.
func parallel(n, count int, fn func(i int)) {
	if count == 0 {
		return
	}
	sem := make(chan struct{}, n)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			fn(i)
		}(i)
	}
	wg.Wait()
}

// grepLines finds lines containing q (case-insensitive) in text.
func grepLines(p, text, q string, max int) []GrepMatch {
	lq := strings.ToLower(q)
	var out []GrepMatch
	for i, line := range strings.Split(text, "\n") {
		if strings.Contains(strings.ToLower(line), lq) {
			out = append(out, GrepMatch{Path: p, Line: i + 1, Text: truncateUTF8(line, 300)})
			if len(out) >= max {
				break
			}
		}
	}
	return out
}

func decodeB64(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(strings.NewReplacer("\n", "", "\r", "").Replace(s))
}

// entryMode is git's mode for a tree entry type.
func entryMode(typ string) string {
	switch typ {
	case "tree":
		return "040000"
	case "symlink":
		return "120000"
	case "commit":
		return "160000"
	}
	return "100644"
}

func baseName(p string) string { return path.Base(p) }

// sortCommitsNewestFirst orders commits by committer time, newest first.
func sortCommitsOldestFirst(list []Commit) {
	sort.SliceStable(list, func(i, j int) bool { return list[i].Committer.At.Before(list[j].Committer.At) })
}

// checkCommitInput validates what both adapters commit.
func checkCommitInput(in *CommitInput, defaultBranch string) error {
	in.Branch = strings.TrimSpace(in.Branch)
	in.NewBranch = strings.TrimSpace(in.NewBranch)
	if in.Branch == "" {
		in.Branch = defaultBranch
	}
	in.Message = strings.TrimSpace(strings.ReplaceAll(in.Message, "\r\n", "\n"))
	if in.Message == "" || utf8.RuneCountInString(in.Message) > 10000 {
		return httpx.BadRequest("Write a commit message (up to 10,000 characters).")
	}
	if len(in.Changes) == 0 || len(in.Changes) > 100 {
		return httpx.BadRequest("A commit to a connected repository changes 1 to 100 files.")
	}
	if in.ExpectedHead != "" && !IsOID(in.ExpectedHead) {
		return httpx.BadRequest("expected_head must be a full commit id.")
	}
	for i := range in.Changes {
		c := &in.Changes[i]
		p, err := CleanPath(c.Path)
		if err != nil || p == "" {
			return httpx.BadRequest("Each change names a file path.")
		}
		c.Path = p
		switch c.Op {
		case "put", "replace_lines":
			if c.Content == nil {
				return httpx.BadRequest(p + ": content is required.")
			}
		case "move":
			from, err := CleanPath(c.From)
			if err != nil || from == "" {
				return httpx.BadRequest(p + ": name the file to move (from).")
			}
			c.From = from
		case "delete":
		default:
			return httpx.BadRequest(p + ": op is put, replace_lines, delete or move.")
		}
	}
	for _, b := range []string{in.Branch, in.NewBranch} {
		if b != "" && !validBranchName(b) {
			return httpx.BadRequest(b + " isn't a valid branch name.")
		}
	}
	return nil
}

// validBranchName is git's check-ref-format for heads, without git.
func validBranchName(b string) bool {
	if b == "" || len(b) > 250 || strings.HasPrefix(b, "/") || strings.HasSuffix(b, "/") || strings.HasSuffix(b, ".") ||
		strings.HasSuffix(b, ".lock") || strings.Contains(b, "..") || strings.Contains(b, "//") || strings.Contains(b, "@{") || b == "@" || strings.HasPrefix(b, "-") {
		return false
	}
	for _, r := range b {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(" ~^:?*[\\", r) {
			return false
		}
	}
	for _, seg := range strings.Split(b, "/") {
		if strings.HasPrefix(seg, ".") {
			return false
		}
	}
	return true
}

// changeContent is a put/replace change's new bytes given the old text.
func changeContent(c ChangeInput, old []byte, existed bool) ([]byte, error) {
	switch c.Op {
	case "put":
		if c.Encoding == "base64" {
			b, err := base64.StdEncoding.DecodeString(*c.Content)
			if err != nil {
				return nil, httpx.BadRequest(c.Path + ": the upload isn't valid base64.")
			}
			return b, nil
		}
		return []byte(*c.Content), nil
	case "replace_lines":
		if !existed {
			return nil, httpx.NotFound(c.Path + " doesn't exist, so its lines can't be replaced.")
		}
		out, err := replaceLines(string(old), c.StartLine, c.EndLine, *c.Content)
		if err != nil {
			return nil, httpx.BadRequest(c.Path + ": " + err.Error())
		}
		return []byte(out), nil
	}
	return nil, fmt.Errorf("changeContent: op %s", c.Op)
}

func agentTrailer(message, agent string) string {
	if agent == "" {
		return message
	}
	return message + "\n\n" + agent
}

var errHeadMoved = func(branch string) error {
	return httpx.NewError(http.StatusConflict, "head_moved", branch+" changed since you started. Reload to see the latest version, or commit to a new branch.")
}

var errBranchProtected = func(platform, branch string) error {
	return httpx.NewError(http.StatusForbidden, "branch_protected", platform+" protects "+branch+": commit to a new branch and open a pull request.")
}

func ptr[T any](v T) *T { return &v }

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// withTimeout bounds a fan-out of platform reads.
func withTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, d)
}

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }
