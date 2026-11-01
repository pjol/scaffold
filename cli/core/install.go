package core

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Template tokens. The template and every feature are real, buildable code
// that uses these literals; installing replaces them.
const (
	tokModule   = "scaffoldapp/backend"     // Go module path
	tokBundleID = "com.example.scaffoldapp" // iOS bundle id / Android package
	tokName     = "Scaffold App"            // display name
	tokUpper    = "SCAFFOLDAPP"             // upper-case slug
	tokSlug     = "scaffoldapp"             // slug: db names, npm scope, url scheme
)

// skipNames are never copied out of the template or a feature.
var skipNames = map[string]bool{
	"node_modules": true, ".next": true, ".expo": true, "dist": true, "build": true,
	".turbo": true, "ios": true, "android": true, "logs": true, "tmp": true,
	".DS_Store": true, ".env": true, ".env.local": true, ".env.dev": true,
	".data": true, ".git": true, "uploads": true, ".dev-up-backup": true,
}

func (p *Project) replacer() *strings.Replacer {
	return strings.NewReplacer(
		tokModule, p.Module,
		tokBundleID, p.BundleID,
		tokName, p.Name,
		tokUpper, strings.ToUpper(p.Slug),
		tokSlug, p.Slug,
	)
}

// copyTree copies src into dst (project-relative dstRel under root), applying
// token replacement to text file contents and paths. Paths the project's
// surfaces exclude are skipped. It returns project-relative written paths.
func (p *Project) copyTree(src, root, dstRel string, dryRun bool, overwrite bool) ([]string, error) {
	r := p.replacer()
	var written []string
	info, err := os.Stat(src)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		rel := r.Replace(filepath.ToSlash(dstRel))
		if !p.wants(rel) {
			return nil, nil
		}
		if err := p.copyFile(src, filepath.Join(root, rel), r, dryRun, overwrite); err != nil {
			return nil, err
		}
		return []string{rel}, nil
	}
	err = filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if skipNames[d.Name()] && path != src {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), ".tsbuildinfo") {
			return nil
		}
		relSrc, _ := filepath.Rel(src, path)
		rel := filepath.ToSlash(filepath.Join(dstRel, relSrc))
		rel = r.Replace(rel)
		if rel != "." && !p.wants(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if err := p.copyFile(path, filepath.Join(root, rel), r, dryRun, overwrite); err != nil {
			return err
		}
		written = append(written, rel)
		return nil
	})
	return written, err
}

func (p *Project) copyFile(src, dst string, r *strings.Replacer, dryRun, overwrite bool) error {
	if !overwrite {
		if _, err := os.Stat(dst); err == nil {
			return fmt.Errorf("%s already exists (pass force to overwrite)", dst)
		}
	}
	if dryRun {
		return nil
	}
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if isText(b) {
		b = []byte(r.Replace(string(b)))
	}
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, b, info.Mode().Perm())
}

func isText(b []byte) bool {
	n := len(b)
	if n > 8000 {
		n = 8000
	}
	return !bytes.Contains(b[:n], []byte{0})
}

// applyWire inserts w.Insert on its own line directly above the anchor line,
// at the anchor's indentation. It is a no-op when the line already exists.
// It reports whether the file changed.
func (p *Project) applyWire(root string, w Wire, dryRun bool) (bool, error) {
	r := p.replacer()
	rel := r.Replace(w.File)
	if !p.wants(rel) {
		return false, nil
	}
	path := filepath.Join(root, rel)
	b, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("wire %s: %w", rel, err)
	}
	insert := r.Replace(w.Insert)
	lines := strings.Split(string(b), "\n")
	for _, l := range lines {
		if strings.TrimSpace(l) == strings.TrimSpace(insert) {
			return false, nil
		}
	}
	for i, l := range lines {
		if !isAnchorLine(l, w.Anchor) {
			continue
		}
		indent := l[:len(l)-len(strings.TrimLeft(l, " \t"))]
		var add []string
		for _, il := range strings.Split(insert, "\n") {
			add = append(add, indent+il)
		}
		out := append([]string{}, lines[:i]...)
		out = append(out, add...)
		out = append(out, lines[i:]...)
		if dryRun {
			return true, nil
		}
		return true, os.WriteFile(path, []byte(strings.Join(out, "\n")), 0o644)
	}
	return false, fmt.Errorf("wire %s: anchor scaffold:%s not found; add the line by hand: %s", rel, w.Anchor, insert)
}

// isAnchorLine reports whether line is the anchor comment itself: the
// marker must open its comment (`// scaffold:x`, `{/* scaffold:x */}`,
// `<!-- scaffold:x -->`, `# scaffold:x`), so prose or doc comments that
// mention an anchor never match.
func isAnchorLine(line, anchor string) bool {
	t := strings.TrimSpace(line)
	for _, open := range []string{"//", "{/*", "/*", "<!--", "#"} {
		if rest, ok := strings.CutPrefix(t, open); ok {
			rest = strings.TrimSpace(rest)
			if name, ok := strings.CutPrefix(rest, "scaffold:"); ok {
				end := strings.IndexFunc(name, func(r rune) bool {
					return !(r == '-' || r == '_' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
				})
				if end < 0 {
					end = len(name)
				}
				return name[:end] == anchor
			}
			return false
		}
	}
	return false
}
