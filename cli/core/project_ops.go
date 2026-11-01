package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var slugRe = regexp.MustCompile(`^[a-z][a-z0-9]{1,30}$`)

// ---- new -------------------------------------------------------------------

type NewInput struct {
	Dir      string   `json:"dir" arg:"0" jsonschema:"directory to create (must not exist or be empty)"`
	Name     string   `json:"name" jsonschema:"display name, e.g. Dale"`
	Slug     string   `json:"slug,omitempty" jsonschema:"lowercase letters and digits; used for db names, npm scope, url scheme (default: from name)"`
	Module   string   `json:"module,omitempty" jsonschema:"Go module path for the backend (default: <slug>/backend)"`
	BundleID string   `json:"bundle_id,omitempty" jsonschema:"iOS bundle id / Android package (default: com.olearylabs.<slug>)"`
	Surfaces []string `json:"surfaces,omitempty" jsonschema:"subset of backend,web,mobile (default: all three; backend is always included)"`
	Features []string `json:"features,omitempty" jsonschema:"feature-belt modules to install, e.g. chat,feed"`
	DryRun   bool     `json:"dry_run,omitempty" jsonschema:"report what would be written without writing"`
}

type NewOutput struct {
	Dir    string     `json:"dir"`
	Files  int        `json:"files"`
	Add    *AddOutput `json:"add,omitempty"`
	Next   []string   `json:"next"`
	DryRun bool       `json:"dry_run,omitempty"`
}

func (o NewOutput) Text() string {
	var b strings.Builder
	verb := "Created"
	if o.DryRun {
		verb = "Would create"
	}
	fmt.Fprintf(&b, "%s %s (%d template files)\n", verb, o.Dir, o.Files)
	if o.Add != nil {
		b.WriteString(o.Add.Text())
	}
	b.WriteString("\nNext:\n")
	for _, n := range o.Next {
		fmt.Fprintf(&b, "  %s\n", n)
	}
	return b.String()
}

func (k *Kit) New(ctx context.Context, in NewInput) (NewOutput, error) {
	if in.Dir == "" || in.Name == "" {
		return NewOutput{}, fmt.Errorf("new needs a directory and --name")
	}
	slug := in.Slug
	if slug == "" {
		slug = strings.Map(func(r rune) rune {
			switch {
			case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
				return r
			case r >= 'A' && r <= 'Z':
				return r + 32
			}
			return -1
		}, in.Name)
	}
	if !slugRe.MatchString(slug) {
		return NewOutput{}, fmt.Errorf("slug %q must match %s; pass --slug", slug, slugRe)
	}
	dir, err := filepath.Abs(in.Dir)
	if err != nil {
		return NewOutput{}, err
	}
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		return NewOutput{}, fmt.Errorf("%s is not empty", dir)
	}

	var p Project
	if err := readJSON(filepath.Join(k.templateDir(), "scaffold.json"), &p); err != nil {
		return NewOutput{}, err
	}
	p.Scaffold = ContractVersion
	p.Name, p.Slug = in.Name, slug
	p.Module = orDefault(in.Module, slug+"/backend")
	p.BundleID = orDefault(in.BundleID, "com.olearylabs."+slug)
	p.Surfaces = []string{"backend"}
	surfaces := in.Surfaces
	if len(surfaces) == 0 {
		surfaces = []string{"web", "mobile"}
	}
	for _, s := range surfaces {
		switch s {
		case "backend":
		case "web", "mobile":
			p.Surfaces = append(p.Surfaces, s)
		default:
			return NewOutput{}, fmt.Errorf("unknown surface %q (backend, web, mobile)", s)
		}
	}
	p.Features = map[string]string{}
	core := p.Env
	p.Env = nil
	p.MergeEnv(core)

	files, err := p.copyTree(k.templateDir(), dir, ".", in.DryRun, false)
	if err != nil {
		return NewOutput{}, err
	}
	out := NewOutput{Dir: dir, Files: len(files), DryRun: in.DryRun}
	if !in.DryRun {
		if err := p.Save(dir); err != nil {
			return out, err
		}
	}
	if len(in.Features) > 0 {
		if in.DryRun {
			// Nothing was written, so plan against the in-memory project.
			res, err := k.Resolve(in.Features, p.Features)
			if err != nil {
				return out, err
			}
			add := &AddOutput{DryRun: true}
			for _, f := range res {
				add.Installed = append(add.Installed, f.Name+"@"+f.Version)
			}
			out.Add = add
		} else {
			add, err := k.Add(ctx, AddInput{Dir: dir, Features: in.Features})
			if err != nil {
				return out, err
			}
			out.Add = &add
		}
	}
	if !in.DryRun {
		if _, err := k.Env(ctx, EnvInput{Dir: dir, Write: true}); err != nil {
			return out, err
		}
	}
	out.Next = []string{
		"cd " + in.Dir,
		"read CLAUDE.md, then ENV.md (fill the secrets it lists in .env.dev)",
		"scripts/dev-up/dev-up.sh",
		"run the design process in concrete/design-process.md before building screens",
	}
	return out, nil
}

func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

// ---- add -------------------------------------------------------------------

type AddInput struct {
	Dir      string   `json:"dir,omitempty" jsonschema:"project root (default: current directory)"`
	Features []string `json:"features" arg:"rest" jsonschema:"feature names; requirements are added automatically"`
	DryRun   bool     `json:"dry_run,omitempty" jsonschema:"report what would change without writing"`
	Force    bool     `json:"force,omitempty" jsonschema:"overwrite files that already exist"`
}

type StitchList struct {
	Feature string   `json:"feature"`
	Spec    string   `json:"spec"`
	Steps   []string `json:"steps"`
}

type AddOutput struct {
	Installed []string     `json:"installed"`
	Files     []string     `json:"files,omitempty"`
	Wired     []string     `json:"wired,omitempty"`
	Env       []string     `json:"env,omitempty"`
	NpmDeps   []string     `json:"npm_deps,omitempty"`
	Stitch    []StitchList `json:"stitch,omitempty"`
	Warnings  []string     `json:"warnings,omitempty"`
	Next      []string     `json:"next,omitempty"`
	DryRun    bool         `json:"dry_run,omitempty"`
}

func (o AddOutput) Text() string {
	var b strings.Builder
	if len(o.Installed) == 0 {
		b.WriteString("Nothing to install: every requested feature is already in the project.\n")
		return b.String()
	}
	verb := "Installed"
	if o.DryRun {
		verb = "Would install"
	}
	fmt.Fprintf(&b, "%s: %s\n", verb, strings.Join(o.Installed, ", "))
	fmt.Fprintf(&b, "  %d files, %d wiring lines, %d env vars, %d npm dependencies\n", len(o.Files), len(o.Wired), len(o.Env), len(o.NpmDeps))
	for _, w := range o.Warnings {
		fmt.Fprintf(&b, "  ! %s\n", w)
	}
	for _, n := range o.Next {
		fmt.Fprintf(&b, "  → %s\n", n)
	}
	for _, s := range o.Stitch {
		fmt.Fprintf(&b, "\nStitch %s (spec: %s):\n", s.Feature, s.Spec)
		for i, st := range s.Steps {
			fmt.Fprintf(&b, "  %d. %s\n", i+1, st)
		}
	}
	return b.String()
}

func (k *Kit) Add(ctx context.Context, in AddInput) (AddOutput, error) {
	dir := orDefault(in.Dir, ".")
	dir, err := filepath.Abs(dir)
	if err != nil {
		return AddOutput{}, err
	}
	p, err := LoadProject(dir)
	if err != nil {
		return AddOutput{}, err
	}
	if len(in.Features) == 0 {
		return AddOutput{}, fmt.Errorf("add needs at least one feature (see `scaffold list`)")
	}
	order, err := k.Resolve(in.Features, p.Features)
	if err != nil {
		return AddOutput{}, err
	}
	out := AddOutput{DryRun: in.DryRun}
	for _, f := range order {
		missing := []string{}
		for _, s := range []string{"web", "mobile"} {
			if p.HasSurface(s) && !contains(f.Surfaces, s) {
				missing = append(missing, s)
			}
		}
		if len(missing) > 0 {
			out.Warnings = append(out.Warnings, fmt.Sprintf("%s ships no %s code (by design or as a stitch: see its FEATURE.md)", f.Name, strings.Join(missing, "/")))
		}
		for _, fm := range f.Files {
			src := filepath.Join(f.Dir, fm.Src)
			if _, err := os.Stat(src); os.IsNotExist(err) {
				continue
			}
			written, err := p.copyTree(src, dir, fm.Dst, in.DryRun, in.Force)
			if err != nil {
				return out, fmt.Errorf("%s: %w", f.Name, err)
			}
			out.Files = append(out.Files, written...)
		}
		seenWire := map[string]bool{}
		for _, w := range f.Wire {
			key := w.File + "\x00" + strings.TrimSpace(w.Insert)
			if seenWire[key] {
				out.Warnings = append(out.Warnings, fmt.Sprintf("%s: two wire entries insert the same line into %s (%s); only one lands — give each its own constant", f.Name, w.File, strings.TrimSpace(w.Insert)))
			}
			seenWire[key] = true
			changed, err := p.applyWire(dir, w, in.DryRun)
			if err != nil {
				out.Warnings = append(out.Warnings, err.Error())
				continue
			}
			if changed {
				out.Wired = append(out.Wired, w.File+" ← "+strings.TrimSpace(w.Insert))
			}
		}
		// The project keeps its own copy of the spec and lists it in CLAUDE.md.
		doc := filepath.Join("docs", "features", f.Name+".md")
		if _, err := p.copyTree(filepath.Join(f.Dir, "FEATURE.md"), dir, doc, in.DryRun, true); err != nil {
			return out, fmt.Errorf("%s: %w", f.Name, err)
		}
		out.Files = append(out.Files, filepath.ToSlash(doc))
		line := fmt.Sprintf("- `%s` %s — %s (`docs/features/%s.md`)", f.Name, f.Version, f.Summary, f.Name)
		if _, err := p.applyWire(dir, Wire{File: "CLAUDE.md", Anchor: "features", Insert: line}, in.DryRun); err != nil {
			out.Warnings = append(out.Warnings, err.Error())
		}
		for pkg, deps := range f.Deps {
			added, err := p.addNpmDeps(dir, pkg, deps, in.DryRun)
			if err != nil {
				out.Warnings = append(out.Warnings, fmt.Sprintf("%s: %v", f.Name, err))
			}
			out.NpmDeps = append(out.NpmDeps, added...)
		}
		out.Env = append(out.Env, p.MergeEnv(f.Env)...)
		p.Features[f.Name] = f.Version
		out.Installed = append(out.Installed, f.Name+"@"+f.Version)
		rel, _ := filepath.Rel(k.Home, filepath.Join(f.Dir, "FEATURE.md"))
		out.Stitch = append(out.Stitch, StitchList{Feature: f.Name, Spec: filepath.ToSlash(rel), Steps: f.Stitch})
	}
	if in.DryRun {
		return out, nil
	}
	if err := p.Save(dir); err != nil {
		return out, err
	}
	if _, err := k.Env(ctx, EnvInput{Dir: dir, Write: true}); err != nil {
		return out, err
	}
	if p.HasSurface("backend") {
		if err := goModTidy(ctx, filepath.Join(dir, "backend")); err != nil {
			out.Warnings = append(out.Warnings, "go mod tidy failed; run it in backend/: "+err.Error())
		}
	}
	if len(out.NpmDeps) > 0 {
		out.Next = append(out.Next, "pnpm install (new npm dependencies)")
	}
	out.Next = append(out.Next, "restart dev-up (migrations apply on boot), then `scaffold doctor`")
	return out, nil
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
