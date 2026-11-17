package core

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ---- adopt -----------------------------------------------------------------

type AdoptInput struct {
	Dir      string   `json:"dir,omitempty" arg:"0" jsonschema:"root of the existing project (default: current directory)"`
	Name     string   `json:"name" jsonschema:"display name, e.g. MOX"`
	Slug     string   `json:"slug,omitempty" jsonschema:"lowercase letters and digits: npm scope, url scheme (default: from name)"`
	Module   string   `json:"module,omitempty" jsonschema:"Go module path of the backend (default: read from backend/go.mod)"`
	BundleID string   `json:"bundle_id,omitempty" jsonschema:"iOS bundle id / Android package (default: com.olearylabs.<slug>)"`
	Surfaces []string `json:"surfaces,omitempty" jsonschema:"surfaces the project has (default: whichever of backend/, web/, mobile/ exist)"`
	Custom   []string `json:"custom,omitempty" jsonschema:"surfaces built outside the template (default: detected: a surface without the template's registry or app package)"`
	Packages string   `json:"packages,omitempty" jsonschema:"directory of the workspace packages (default: mobile/packages when it exists, else packages)"`
	HandEnv  []string `json:"hand_env,omitempty" jsonschema:"tiers whose .env.example is written by hand (default: every custom surface that has one)"`
	Force    bool     `json:"force,omitempty" jsonschema:"replace an existing scaffold.json (its features and env are kept)"`
	DryRun   bool     `json:"dry_run,omitempty" jsonschema:"report what would be written without writing"`
}

type AdoptOutput struct {
	Dir     string   `json:"dir"`
	Project *Project `json:"project"`
	Written []string `json:"written,omitempty"`
	Next    []string `json:"next"`
	DryRun  bool     `json:"dry_run,omitempty"`
}

func (o AdoptOutput) Text() string {
	var b strings.Builder
	verb := "Adopted"
	if o.DryRun {
		verb = "Would adopt"
	}
	p := o.Project
	fmt.Fprintf(&b, "%s %s as %s (%s) · surfaces %s\n", verb, o.Dir, p.Name, p.Slug, strings.Join(p.Surfaces, ", "))
	if s := p.layoutSummary(); s != "" {
		fmt.Fprintf(&b, "  layout: %s\n", s)
	}
	for _, w := range o.Written {
		fmt.Fprintf(&b, "  wrote %s\n", w)
	}
	b.WriteString("\nNext:\n")
	for _, n := range o.Next {
		fmt.Fprintf(&b, "  %s\n", n)
	}
	return b.String()
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// goModule reads the module path from a go.mod.
func goModule(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(sc.Text()), "module "); ok {
			return strings.Trim(strings.TrimSpace(rest), `"`)
		}
	}
	return ""
}

// templateShaped reports whether a surface already follows the template (it has the
// files `scaffold add` wires into), so features can install into it.
func templateShaped(dir, surface string) bool {
	switch surface {
	case "backend":
		// The module registry features register through (`api.RegisterModule`).
		b, err := os.ReadFile(filepath.Join(dir, "backend", "internal", "api", "registry.go"))
		return err == nil && strings.Contains(string(b), "func RegisterModule")
	case "web":
		return exists(filepath.Join(dir, "web", "src", "features", "registry.tsx"))
	case "mobile":
		return exists(filepath.Join(dir, "mobile", "src", "features", "registry.tsx"))
	}
	return false
}

// Adopt writes scaffold.json for a project that wasn't made by `scaffold new`: its
// surfaces, which of them follow the template (the rest are custom and never written
// into), where its workspace packages live, and which env examples are hand-written.
// It adds the root docs doctor expects when missing (AGENTS.md, ENV.md), never
// touching existing files other than scaffold.json, and regenerates ENV.md's table.
func (k *Kit) Adopt(ctx context.Context, in AdoptInput) (AdoptOutput, error) {
	if in.Name == "" {
		return AdoptOutput{}, fmt.Errorf("adopt needs --name")
	}
	dir, err := filepath.Abs(orDefault(in.Dir, "."))
	if err != nil {
		return AdoptOutput{}, err
	}
	if !exists(dir) {
		return AdoptOutput{}, fmt.Errorf("%s does not exist", dir)
	}
	manifest := filepath.Join(dir, "scaffold.json")
	var prev *Project
	if exists(manifest) {
		if !in.Force {
			return AdoptOutput{}, fmt.Errorf("%s already has scaffold.json; pass force to replace it (features and env are kept)", dir)
		}
		prev, _ = LoadProject(dir)
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
		return AdoptOutput{}, fmt.Errorf("slug %q must match %s; pass --slug", slug, slugRe)
	}

	surfaces := in.Surfaces
	if len(surfaces) == 0 {
		for _, s := range []string{"backend", "web", "mobile"} {
			if exists(filepath.Join(dir, s)) {
				surfaces = append(surfaces, s)
			}
		}
	}
	for _, s := range surfaces {
		if s != "backend" && s != "web" && s != "mobile" {
			return AdoptOutput{}, fmt.Errorf("unknown surface %q (backend, web, mobile)", s)
		}
	}
	custom := in.Custom
	if len(custom) == 0 {
		for _, s := range surfaces {
			if !templateShaped(dir, s) {
				custom = append(custom, s)
			}
		}
	}
	packages := in.Packages
	if packages == "" && exists(filepath.Join(dir, "mobile", "packages")) {
		packages = "mobile/packages"
	}
	hand := in.HandEnv
	if len(hand) == 0 {
		for _, s := range custom {
			if exists(filepath.Join(dir, envExampleFiles[s])) {
				hand = append(hand, s)
			}
		}
	}

	module := in.Module
	if module == "" {
		module = goModule(filepath.Join(dir, "backend", "go.mod"))
	}
	p := &Project{
		Scaffold: ContractVersion,
		Name:     in.Name,
		Slug:     slug,
		Module:   module,
		BundleID: orDefault(in.BundleID, "com.olearylabs."+slug),
		Surfaces: surfaces,
		Features: map[string]string{},
		Env:      []EnvVar{},
	}
	if prev != nil {
		p.Features, p.Env = prev.Features, prev.Env
		if p.Features == nil {
			p.Features = map[string]string{}
		}
		if p.Env == nil {
			p.Env = []EnvVar{}
		}
	}
	if packages != "" && packages != "packages" || len(custom) > 0 || len(hand) > 0 {
		sort.Strings(custom)
		sort.Strings(hand)
		p.Layout = &Layout{Packages: packages, Custom: custom, HandEnv: hand}
	}

	out := AdoptOutput{Dir: dir, Project: p, DryRun: in.DryRun}
	docs := map[string]string{
		"AGENTS.md": "# AGENTS.md\n\nRead `CLAUDE.md` in full before working in this repo; it is the agent guide.\n",
		"ENV.md":    "# Environment\n\nEvery variable the project reads. The table is generated from `scaffold.json` by `scaffold env --write`; edit `scaffold.json`, not the table.\n\n" + envBegin + "\n" + envEnd + "\n",
	}
	names := []string{"AGENTS.md", "ENV.md"}
	if !in.DryRun {
		if err := p.Save(dir); err != nil {
			return out, err
		}
	}
	out.Written = append(out.Written, "scaffold.json")
	for _, name := range names {
		path := filepath.Join(dir, name)
		if exists(path) {
			continue
		}
		if !in.DryRun {
			if err := os.WriteFile(path, []byte(docs[name]), 0o644); err != nil {
				return out, err
			}
		}
		out.Written = append(out.Written, name)
	}
	if !in.DryRun {
		env, err := k.Env(ctx, EnvInput{Dir: dir, Write: true})
		if err != nil {
			return out, err
		}
		for _, w := range env.Written {
			if !contains(out.Written, w) {
				out.Written = append(out.Written, w)
			}
		}
		if len(env.Undocumented) > 0 {
			out.Next = append(out.Next, fmt.Sprintf("declare the %d variables the code reads in scaffold.json → env (`scaffold env` lists them), then `scaffold env --write`", len(env.Undocumented)))
		}
	}
	if b, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md")); err != nil || !strings.Contains(string(b), "<!-- scaffold:features -->") {
		out.Next = append(out.Next, "add a `<!-- scaffold:features -->` line to CLAUDE.md's installed-features list (`scaffold add` lists each feature above it)")
	}
	for _, s := range surfaces {
		if !contains(custom, s) && !templateShaped(dir, s) {
			out.Next = append(out.Next, fmt.Sprintf("%s is marked template-shaped but has no registry yet: bring it over from `scaffold new <tmp> --surfaces %s` (concrete/%s.md)", s, s, s))
		}
	}
	out.Next = append(out.Next, "scaffold doctor --dir "+in.Dir)
	return out, nil
}
