package repos

import (
	"bytes"
	"context"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// DiffFile is one changed file between two commits. Patch holds unified
// hunks unless the file is binary, too large, or past the diff's caps
// (then Truncated is set and the client asks for that file alone).
type DiffFile struct {
	Path      string     `json:"path"`
	OldPath   string     `json:"old_path"` // differs from Path on renames and copies
	Status    string     `json:"status"`   // added | modified | deleted | renamed | copied | type_changed
	OldMode   string     `json:"old_mode"`
	NewMode   string     `json:"new_mode"`
	Additions int        `json:"additions"`
	Deletions int        `json:"deletions"`
	Binary    bool       `json:"binary"`
	Truncated bool       `json:"truncated"`
	Hunks     []DiffHunk `json:"hunks"`
}

// DiffHunk is one @@ section.
type DiffHunk struct {
	Header   string     `json:"header"` // the text after the second @@ (function context)
	OldStart int        `json:"old_start"`
	OldLines int        `json:"old_lines"`
	NewStart int        `json:"new_start"`
	NewLines int        `json:"new_lines"`
	Lines    []DiffLine `json:"lines"`
}

// DiffLine is one line of a hunk: context, add or del, with its numbers on
// each side (0 where it has none).
type DiffLine struct {
	Type string `json:"type"` // context | add | del | note ("\ No newline at end of file")
	Old  int    `json:"old"`
	New  int    `json:"new"`
	Text string `json:"text"`
}

// Diff is a set of changed files with totals.
type Diff struct {
	Files     []DiffFile `json:"files"`
	Additions int        `json:"additions"`
	Deletions int        `json:"deletions"`
	// FilesTruncated: more files changed than the cap; Files lists the first ones.
	FilesTruncated bool `json:"files_truncated"`
}

// DiffLimits bounds a diff so a huge change can't exhaust memory or the page.
type DiffLimits struct {
	MaxFiles     int   // files listed
	MaxFileBytes int   // patch text per file before it's truncated
	MaxBytes     int64 // patch text in total
}

var defaultDiffLimits = DiffLimits{MaxFiles: 300, MaxFileBytes: 400 << 10, MaxBytes: 6 << 20}

// DiffCommits diffs from → to (from "" or ZeroOID: against the empty tree).
// paths limits it to some files (one file's full patch).
func (g *Git) DiffCommits(ctx context.Context, id uuid.UUID, from, to string, lim DiffLimits, paths ...string) (*Diff, error) {
	if from == "" || from == ZeroOID {
		from = emptyTree
	}
	common := []string{"--no-color", "--find-renames", "--no-ext-diff", "--no-textconv", "--end-of-options", from, to, "--"}
	common = append(common, paths...)

	// 1. The file list with stats; -z keeps any name intact.
	stat, err := g.output(ctx, id, append([]string{"diff", "--raw", "--numstat", "-z"}, common...)...)
	if err != nil {
		return nil, err
	}
	files := parseRawNumstat(stat)
	d := &Diff{Files: []DiffFile{}}
	for _, f := range files {
		d.Additions += f.Additions
		d.Deletions += f.Deletions
	}
	if lim.MaxFiles > 0 && len(files) > lim.MaxFiles {
		files = files[:lim.MaxFiles]
		d.FilesTruncated = true
	}

	// 2. The patches, in the same order; blocks start at "diff --git".
	out, _, err := g.run(ctx, id, runOpts{max: lim.MaxBytes}, append([]string{"diff", "--patch", "--full-index"}, common...)...)
	cut := err == ErrTooLarge
	if err != nil && !cut {
		return nil, err
	}
	blocks := splitPatch(out)
	for i := range files {
		f := &files[i]
		if i >= len(blocks) || (cut && i == len(blocks)-1) {
			f.Truncated = !f.Binary
			continue
		}
		if f.Binary {
			continue
		}
		if lim.MaxFileBytes > 0 && len(blocks[i]) > lim.MaxFileBytes {
			f.Truncated = true
			continue
		}
		f.Hunks = parseHunks(blocks[i])
	}
	d.Files = files
	return d, nil
}

// parseRawNumstat reads `diff --raw --numstat -z`: first every raw record
// (":old new oldsha newsha status\0path\0[path2\0]"), then every numstat
// record ("add\tdel\tpath\0" or "add\tdel\t\0old\0new\0" for renames).
func parseRawNumstat(out []byte) []DiffFile {
	toks := bytes.Split(out, []byte{0})
	var files []DiffFile
	i := 0
	for i < len(toks) && len(toks[i]) > 0 && toks[i][0] == ':' {
		f := strings.Fields(string(toks[i][1:]))
		if len(f) < 5 || i+1 >= len(toks) {
			break
		}
		df := DiffFile{OldMode: f[0], NewMode: f[1], Hunks: []DiffHunk{}}
		code := f[4][:1]
		df.OldPath = string(toks[i+1])
		df.Path = df.OldPath
		i += 2
		if code == "R" || code == "C" {
			if i < len(toks) {
				df.Path = string(toks[i])
			}
			i++
		}
		df.Status = map[string]string{"A": "added", "M": "modified", "D": "deleted", "R": "renamed", "C": "copied", "T": "type_changed"}[code]
		if df.Status == "" {
			df.Status = "modified"
		}
		files = append(files, df)
	}
	// Numstat records follow in the same order.
	for n := 0; n < len(files) && i < len(toks); n++ {
		f := strings.SplitN(string(toks[i]), "\t", 3)
		i++
		if len(f) < 3 {
			break
		}
		if f[2] == "" { // rename: two more tokens
			i += 2
		}
		if f[0] == "-" {
			files[n].Binary = true
			continue
		}
		files[n].Additions, _ = strconv.Atoi(f[0])
		files[n].Deletions, _ = strconv.Atoi(f[1])
	}
	return files
}

// splitPatch cuts patch output into one block per file.
func splitPatch(out []byte) [][]byte {
	var blocks [][]byte
	start := -1
	for i := 0; i < len(out); {
		end := bytes.IndexByte(out[i:], '\n')
		lineEnd := len(out)
		if end >= 0 {
			lineEnd = i + end + 1
		}
		if bytes.HasPrefix(out[i:lineEnd], []byte("diff --git ")) {
			if start >= 0 {
				blocks = append(blocks, out[start:i])
			}
			start = i
		}
		i = lineEnd
	}
	if start >= 0 {
		blocks = append(blocks, out[start:])
	}
	return blocks
}

// parseHunks reads the @@ sections of one file's patch block.
func parseHunks(block []byte) []DiffHunk {
	hunks := []DiffHunk{}
	var h *DiffHunk
	oldN, newN := 0, 0
	for _, raw := range strings.Split(string(block), "\n") {
		if strings.HasPrefix(raw, "@@ ") {
			hunks = append(hunks, DiffHunk{Lines: []DiffLine{}})
			h = &hunks[len(hunks)-1]
			h.OldStart, h.OldLines, h.NewStart, h.NewLines, h.Header = parseHunkHeader(raw)
			oldN, newN = h.OldStart, h.NewStart
			continue
		}
		if h == nil || raw == "" {
			continue
		}
		switch raw[0] {
		case ' ':
			h.Lines = append(h.Lines, DiffLine{Type: "context", Old: oldN, New: newN, Text: raw[1:]})
			oldN++
			newN++
		case '+':
			h.Lines = append(h.Lines, DiffLine{Type: "add", New: newN, Text: raw[1:]})
			newN++
		case '-':
			h.Lines = append(h.Lines, DiffLine{Type: "del", Old: oldN, Text: raw[1:]})
			oldN++
		case '\\':
			h.Lines = append(h.Lines, DiffLine{Type: "note", Text: strings.TrimSpace(raw[1:])})
		}
	}
	return hunks
}

// parseHunkHeader reads "@@ -a,b +c,d @@ context".
func parseHunkHeader(s string) (oldStart, oldLines, newStart, newLines int, ctx string) {
	rest := strings.TrimPrefix(s, "@@ ")
	ranges, after, _ := strings.Cut(rest, " @@")
	ctx = strings.TrimSpace(after)
	parts := strings.Fields(ranges)
	parse := func(p string) (int, int) {
		p = strings.TrimLeft(p, "-+")
		a, b, ok := strings.Cut(p, ",")
		start, _ := strconv.Atoi(a)
		n := 1
		if ok {
			n, _ = strconv.Atoi(b)
		}
		return start, n
	}
	if len(parts) >= 2 {
		oldStart, oldLines = parse(parts[0])
		newStart, newLines = parse(parts[1])
	}
	return
}
