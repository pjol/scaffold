package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ---- list ------------------------------------------------------------------

type ListInput struct {
	Dir string `json:"dir,omitempty" jsonschema:"project directory; when given, marks installed features"`
}

type FeatureSummary struct {
	Name      string   `json:"name"`
	Title     string   `json:"title"`
	Version   string   `json:"version"`
	Summary   string   `json:"summary"`
	Requires  []string `json:"requires,omitempty"`
	Surfaces  []string `json:"surfaces"`
	Installed string   `json:"installed,omitempty"`
}

type ListOutput struct {
	Features []FeatureSummary `json:"features"`
}

func (o ListOutput) Text() string {
	var b strings.Builder
	for _, f := range o.Features {
		mark := "  "
		if f.Installed != "" {
			mark = "✓ "
		}
		fmt.Fprintf(&b, "%s%-14s %s\n", mark, f.Name, f.Summary)
		extra := []string{"v" + f.Version, strings.Join(f.Surfaces, "+")}
		if len(f.Requires) > 0 {
			extra = append(extra, "needs "+strings.Join(f.Requires, ", "))
		}
		fmt.Fprintf(&b, "  %-14s %s\n", "", strings.Join(extra, " · "))
	}
	return b.String()
}

func (k *Kit) List(_ context.Context, in ListInput) (ListOutput, error) {
	cat, names, err := k.Catalog()
	if err != nil {
		return ListOutput{}, err
	}
	installed := map[string]string{}
	if in.Dir != "" {
		p, err := LoadProject(in.Dir)
		if err != nil {
			return ListOutput{}, err
		}
		installed = p.Features
	}
	out := ListOutput{Features: []FeatureSummary{}}
	for _, n := range names {
		f := cat[n]
		out.Features = append(out.Features, FeatureSummary{
			Name: f.Name, Title: f.Title, Version: f.Version, Summary: f.Summary,
			Requires: f.Requires, Surfaces: f.Surfaces, Installed: installed[n],
		})
	}
	return out, nil
}

// ---- show ------------------------------------------------------------------

type ShowInput struct {
	Feature string `json:"feature" arg:"0" jsonschema:"feature name, e.g. chat"`
}

type ShowOutput struct {
	Manifest *Feature `json:"manifest"`
	Spec     string   `json:"spec"` // FEATURE.md
	Path     string   `json:"path"`
}

func (o ShowOutput) Text() string { return o.Spec }

func (k *Kit) Show(_ context.Context, in ShowInput) (ShowOutput, error) {
	cat, _, err := k.Catalog()
	if err != nil {
		return ShowOutput{}, err
	}
	f, ok := cat[in.Feature]
	if !ok {
		return ShowOutput{}, fmt.Errorf("unknown feature %q (see `scaffold list`)", in.Feature)
	}
	path := filepath.Join(f.Dir, "FEATURE.md")
	b, err := os.ReadFile(path)
	if err != nil {
		return ShowOutput{}, err
	}
	return ShowOutput{Manifest: f, Spec: string(b), Path: path}, nil
}

// ---- spec ------------------------------------------------------------------

type SpecInput struct {
	Path string `json:"path,omitempty" arg:"0" jsonschema:"document path without .md, e.g. concrete/design or feature-belt/chat/FEATURE; empty lists all"`
}

type SpecOutput struct {
	Path    string   `json:"path,omitempty"`
	Content string   `json:"content,omitempty"`
	Docs    []string `json:"docs,omitempty"`
}

func (o SpecOutput) Text() string {
	if o.Content != "" {
		return o.Content
	}
	return strings.Join(o.Docs, "\n") + "\n"
}

func (k *Kit) Spec(_ context.Context, in SpecInput) (SpecOutput, error) {
	if in.Path == "" {
		var docs []string
		err := filepath.WalkDir(k.Home, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && (d.Name() == "template" || d.Name() == "cli" || strings.HasPrefix(d.Name(), ".")) && p != k.Home {
				return filepath.SkipDir
			}
			if !d.IsDir() && strings.HasSuffix(p, ".md") {
				rel, _ := filepath.Rel(k.Home, p)
				docs = append(docs, strings.TrimSuffix(filepath.ToSlash(rel), ".md"))
			}
			return nil
		})
		sort.Strings(docs)
		return SpecOutput{Docs: docs}, err
	}
	clean := filepath.Clean(strings.TrimSuffix(in.Path, ".md")) + ".md"
	if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		return SpecOutput{}, fmt.Errorf("spec path must stay inside the scaffold home")
	}
	full := filepath.Join(k.Home, clean)
	if fi, err := os.Stat(full); err != nil || fi.IsDir() {
		// allow "concrete" to mean concrete/SKILL.md
		alt := filepath.Join(k.Home, strings.TrimSuffix(clean, ".md"), "SKILL.md")
		if _, err2 := os.Stat(alt); err2 == nil {
			full = alt
		} else {
			return SpecOutput{}, fmt.Errorf("no spec at %s (run `scaffold spec` to list)", clean)
		}
	}
	b, err := os.ReadFile(full)
	if err != nil {
		return SpecOutput{}, err
	}
	rel, _ := filepath.Rel(k.Home, full)
	return SpecOutput{Path: filepath.ToSlash(rel), Content: string(b)}, nil
}
