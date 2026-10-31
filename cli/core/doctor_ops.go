package core

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type DoctorInput struct {
	Dir string `json:"dir,omitempty" jsonschema:"project root (default: current directory)"`
}

type Check struct {
	Level   string `json:"level"` // ok | warn | error
	Area    string `json:"area"`
	Message string `json:"message"`
}

type DoctorOutput struct {
	Checks []Check `json:"checks"`
	Errors int     `json:"errors"`
	Warns  int     `json:"warnings"`
}

func (o DoctorOutput) Text() string {
	var b strings.Builder
	icon := map[string]string{"ok": "✓", "warn": "!", "error": "✗"}
	for _, c := range o.Checks {
		fmt.Fprintf(&b, "%s %-10s %s\n", icon[c.Level], c.Area, c.Message)
	}
	fmt.Fprintf(&b, "\n%d errors, %d warnings\n", o.Errors, o.Warns)
	return b.String()
}

func (o *DoctorOutput) add(level, area, format string, args ...any) {
	o.Checks = append(o.Checks, Check{Level: level, Area: area, Message: fmt.Sprintf(format, args...)})
	switch level {
	case "error":
		o.Errors++
	case "warn":
		o.Warns++
	}
}

var migName = regexp.MustCompile(`^(\d{4})_([a-z0-9]+)_[a-z0-9_]+\.sql$`)

func (k *Kit) Doctor(ctx context.Context, in DoctorInput) (DoctorOutput, error) {
	var out DoctorOutput
	dir, err := filepath.Abs(orDefault(in.Dir, "."))
	if err != nil {
		return out, err
	}
	p, err := LoadProject(dir)
	if err != nil {
		out.add("error", "manifest", "%v", err)
		return out, nil
	}
	if p.Scaffold != ContractVersion {
		out.add("warn", "manifest", "scaffold.json contract %q, toolset speaks %q", p.Scaffold, ContractVersion)
	} else {
		out.add("ok", "manifest", "%s (%s) · surfaces %s", p.Name, p.Slug, strings.Join(p.Surfaces, ", "))
	}

	cat, _, err := k.Catalog()
	if err != nil {
		return out, err
	}
	k.checkFeatures(&out, p, cat)
	checkMigrations(&out, dir, p, cat)
	k.checkAnchors(&out, dir, p, cat)
	k.checkDocs(&out, dir)

	env, err := k.Env(ctx, EnvInput{Dir: dir})
	if err != nil {
		return out, err
	}
	if len(env.Undocumented) > 0 {
		out.add("warn", "env", "%d variables read in code are missing from scaffold.json: %s", len(env.Undocumented), strings.Join(env.Undocumented, "; "))
	} else {
		out.add("ok", "env", "%d variables documented", len(env.Vars))
	}
	checkEnvExamples(&out, dir, p)
	checkSharedExports(&out, dir, p)
	checkHouseRules(&out, dir)
	return out, nil
}

func (k *Kit) checkFeatures(out *DoctorOutput, p *Project, cat map[string]*Feature) {
	names := make([]string, 0, len(p.Features))
	for n := range p.Features {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		f, ok := cat[n]
		switch {
		case !ok:
			out.add("warn", "features", "%s is installed but no longer in the belt", n)
		case f.Version != p.Features[n]:
			out.add("warn", "features", "%s %s installed; belt has %s (diff feature-belt/%s against the project and port changes by hand)", n, p.Features[n], f.Version, n)
		default:
			for _, r := range f.Requires {
				if _, ok := p.Features[r]; !ok {
					out.add("error", "features", "%s requires %s, which is not installed (`scaffold add %s`)", n, r, r)
				}
			}
		}
	}
	if len(names) > 0 {
		out.add("ok", "features", "installed: %s", strings.Join(names, ", "))
	}
}

func checkMigrations(out *DoctorOutput, dir string, p *Project, cat map[string]*Feature) {
	if !p.HasSurface("backend") {
		return
	}
	entries, err := os.ReadDir(filepath.Join(dir, "backend", "migrations"))
	if err != nil {
		out.add("error", "migrations", "backend/migrations: %v", err)
		return
	}
	seen := map[int]string{}
	count := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		count++
		m := migName.FindStringSubmatch(e.Name())
		if m == nil {
			out.add("error", "migrations", "%s: name must be NNNN_<track>_<what>.sql", e.Name())
			continue
		}
		n, _ := strconv.Atoi(m[1])
		track := m[2]
		if prev, dup := seen[n]; dup {
			out.add("error", "migrations", "%s and %s share number %04d", prev, e.Name(), n)
		}
		seen[n] = e.Name()
		lo, hi, ok := trackRange(track, cat)
		if !ok {
			out.add("error", "migrations", "%s: unknown track %q (core, app, or an installed feature)", e.Name(), track)
			continue
		}
		if n < lo || n > hi {
			out.add("error", "migrations", "%s: track %s owns %04d–%04d", e.Name(), track, lo, hi)
		}
	}
	out.add("ok", "migrations", "%d files, numbering checked against the ledger", count)
}

// trackRange is the migration ledger: core 0001–0099, each feature its
// block of 100, app 5000–9999.
func trackRange(track string, cat map[string]*Feature) (int, int, bool) {
	switch track {
	case "core":
		return 1, 99, true
	case "app":
		return 5000, 9999, true
	}
	if f, ok := cat[track]; ok && f.MigrationBlock > 0 {
		return f.MigrationBlock, f.MigrationBlock + 99, true
	}
	return 0, 0, false
}

func (k *Kit) checkAnchors(out *DoctorOutput, dir string, p *Project, cat map[string]*Feature) {
	need := map[string]map[string]bool{} // file -> anchors
	r := p.replacer()
	for _, f := range cat {
		for _, w := range f.Wire {
			file := r.Replace(w.File)
			if !p.wants(file) {
				continue
			}
			if need[file] == nil {
				need[file] = map[string]bool{}
			}
			need[file][w.Anchor] = true
		}
	}
	missing := 0
	files := make([]string, 0, len(need))
	for f := range need {
		files = append(files, f)
	}
	sort.Strings(files)
	for _, file := range files {
		b, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			out.add("warn", "anchors", "%s missing; features that wire into it need manual stitching", file)
			missing++
			continue
		}
		for a := range need[file] {
			found := false
			for _, l := range strings.Split(string(b), "\n") {
				found = found || isAnchorLine(l, a)
			}
			if !found {
				out.add("warn", "anchors", "%s lost anchor scaffold:%s; restore the comment so `scaffold add` can wire into it", file, a)
				missing++
			}
		}
	}
	if missing == 0 {
		out.add("ok", "anchors", "%d wiring files carry their anchors", len(files))
	}
}

// checkDocs requires every root markdown file the template ships.
func (k *Kit) checkDocs(out *DoctorOutput, dir string) {
	entries, err := os.ReadDir(k.templateDir())
	if err != nil {
		return
	}
	var missing []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, e.Name())); err != nil {
			missing = append(missing, e.Name())
		}
	}
	if len(missing) > 0 {
		out.add("error", "docs", "missing root docs: %s", strings.Join(missing, ", "))
	} else {
		out.add("ok", "docs", "root doc set present")
	}
	for _, f := range []string{"README.md", "CLAUDE.md", "docs/ARCHITECTURE.md"} {
		if b, err := os.ReadFile(filepath.Join(dir, f)); err == nil && strings.Contains(strings.ToLower(string(b)), "<one line") {
			out.add("warn", "docs", "%s still has the one-line product placeholder; write what the app is", f)
		}
	}
}

func checkEnvExamples(out *DoctorOutput, dir string, p *Project) {
	stale := []string{}
	for _, v := range p.Env {
		rel := envExampleFiles[v.Tier]
		if rel == "" || !p.wants(rel) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil || !strings.Contains(string(b), "\n"+v.Name+"=") {
			stale = append(stale, v.Name)
		}
	}
	if len(stale) > 0 {
		out.add("warn", "env", ".env.example files are stale (%s); run `scaffold env --write`", strings.Join(stale, ", "))
	}
}

// House rules from concrete/: tokens only, no Tailwind default palette, no
// banned copy words. These are warnings; the specs are the authority.
var (
	rawHex     = regexp.MustCompile(`#[0-9a-fA-F]{6}\b|#[0-9a-fA-F]{3}\b`)
	twPalette  = regexp.MustCompile(`\b(?:bg|text|border|ring|fill|stroke|from|to|via|outline|divide|shadow)-(?:slate|gray|zinc|neutral|stone|red|orange|amber|yellow|lime|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose)-\d{2,3}\b`)
	bannedCopy = regexp.MustCompile(`(?i)["'>][^"'<]*\b(seamless(?:ly)?|elevate|unlock|delve|curated|journey|effortless(?:ly)?|supercharge|game-?changer|cutting-edge|revolutioni[sz]e)\b`)
)

func checkHouseRules(out *DoctorOutput, dir string) {
	type hit struct{ rule, where string }
	var hits []hit
	roots := []string{"web/app", "web/src", "mobile/app", "mobile/src"}
	for _, root := range roots {
		base := filepath.Join(dir, root)
		_ = filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if skipNames[d.Name()] {
					return filepath.SkipDir
				}
				return nil
			}
			ext := filepath.Ext(path)
			if ext != ".tsx" && ext != ".ts" && ext != ".css" {
				return nil
			}
			name := d.Name()
			if strings.Contains(path, "tokens") || strings.Contains(name, "theme") || name == "globals.css" {
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			rel, _ := filepath.Rel(dir, path)
			rel = filepath.ToSlash(rel)
			s := string(b)
			if rawHex.MatchString(s) {
				hits = append(hits, hit{"raw colour (use tokens)", rel})
			}
			if twPalette.MatchString(s) {
				hits = append(hits, hit{"Tailwind default palette class (use tokens)", rel})
			}
			if ext == ".tsx" {
				if m := bannedCopy.FindStringSubmatch(s); m != nil {
					hits = append(hits, hit{"banned copy word \"" + strings.ToLower(m[1]) + "\" (concrete/writing.md)", rel})
				}
			}
			return nil
		})
	}
	if len(hits) == 0 {
		out.add("ok", "rules", "no raw colours, default palette classes or banned copy words")
		return
	}
	for _, h := range hits {
		out.add("warn", "rules", "%s: %s", h.where, h.rule)
	}
}

var exportDecl = regexp.MustCompile(`(?m)^export\s+(?:declare\s+)?(?:async\s+)?(?:function\*?|const|let|class|interface|type|enum)\s+([A-Za-z0-9_$]+)`)

// checkSharedExports finds names two installed features both export from
// packages/shared (every feature is re-exported from one index).
func checkSharedExports(out *DoctorOutput, dir string, p *Project) {
	owners := map[string][]string{}
	for name := range p.Features {
		root := filepath.Join(dir, "packages", "shared", "src", name)
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".ts") {
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			seen := map[string]bool{}
			for _, m := range exportDecl.FindAllSubmatch(b, -1) {
				n := string(m[1])
				if !seen[n] {
					seen[n] = true
					owners[n] = append(owners[n], name)
				}
			}
			return nil
		})
	}
	var clashes []string
	for n, fs := range owners {
		uniq := map[string]bool{}
		for _, f := range fs {
			uniq[f] = true
		}
		if len(uniq) > 1 {
			list := make([]string, 0, len(uniq))
			for f := range uniq {
				list = append(list, f)
			}
			sort.Strings(list)
			clashes = append(clashes, n+" ("+strings.Join(list, ", ")+")")
		}
	}
	sort.Strings(clashes)
	if len(clashes) > 0 {
		out.add("error", "shared", "features export the same name from packages/shared: %s; prefix names with the feature", strings.Join(clashes, "; "))
	} else if p.HasSurface("web") || p.HasSurface("mobile") {
		out.add("ok", "shared", "no export name clashes between features")
	}
}
