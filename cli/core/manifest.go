package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ContractVersion is the version of the project/feature manifest format.
const ContractVersion = "1"

// EnvVar is one environment variable. The same shape is used by the
// template's scaffold.json (core vars), by feature.json (feature vars) and by
// a generated project's scaffold.json (everything the project needs).
type EnvVar struct {
	Name     string `json:"name"`
	Tier     string `json:"tier"`               // backend | web | mobile | dev
	Group    string `json:"group,omitempty"`    // core, auth, storage, or a feature name
	Required string `json:"required,omitempty"` // always | prod | optional (default optional)
	Default  string `json:"default,omitempty"`  // value the code uses when unset
	Dev      string `json:"dev,omitempty"`      // value dev-up writes for local development
	Secret   bool   `json:"secret,omitempty"`
	Doc      string `json:"doc"`
	Source   string `json:"source,omitempty"` // where to obtain the value
}

// FileMap copies a directory or file from a feature into a project.
type FileMap struct {
	Src string `json:"src"` // relative to the feature directory
	Dst string `json:"dst"` // relative to the project root
}

// Wire inserts one line above an anchor comment (`scaffold:<anchor>`) in a
// project file. Inserting is idempotent.
type Wire struct {
	File   string `json:"file"`
	Anchor string `json:"anchor"`
	Insert string `json:"insert"`
}

// Feature is a feature-belt module manifest (feature.json).
type Feature struct {
	Name           string    `json:"name"`
	Title          string    `json:"title"`
	Version        string    `json:"version"`
	Summary        string    `json:"summary"`
	Requires       []string  `json:"requires,omitempty"`
	Surfaces       []string  `json:"surfaces"`
	MigrationBlock int       `json:"migration_block,omitempty"`
	Env            []EnvVar  `json:"env,omitempty"`
	Files          []FileMap `json:"files"`
	Wire           []Wire    `json:"wire,omitempty"`
	Stitch         []string  `json:"stitch,omitempty"`
	// Deps are npm dependencies the feature's client code needs, keyed by
	// workspace package: web, mobile, shared, ui-web, ui-native.
	Deps map[string]map[string]string `json:"deps,omitempty"`

	Dir string `json:"-"` // absolute path of the feature directory
}

// Project is a generated project's manifest (scaffold.json at its root).
type Project struct {
	Scaffold string            `json:"scaffold"`
	Name     string            `json:"name"`
	Slug     string            `json:"slug"`
	Module   string            `json:"module"`
	BundleID string            `json:"bundle_id"`
	Surfaces []string          `json:"surfaces"`
	Features map[string]string `json:"features"`
	Env      []EnvVar          `json:"env"`
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// LoadProject reads scaffold.json from a project root.
func LoadProject(dir string) (*Project, error) {
	var p Project
	if err := readJSON(filepath.Join(dir, "scaffold.json"), &p); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%s has no scaffold.json; run this inside a project made by `scaffold new`", dir)
		}
		return nil, err
	}
	if p.Features == nil {
		p.Features = map[string]string{}
	}
	return &p, nil
}

// Save writes scaffold.json back to the project root.
func (p *Project) Save(dir string) error {
	sort.SliceStable(p.Env, func(i, j int) bool { return envLess(p.Env[i], p.Env[j]) })
	return writeJSON(filepath.Join(dir, "scaffold.json"), p)
}

// HasSurface reports whether the project includes backend, web or mobile.
func (p *Project) HasSurface(s string) bool {
	for _, x := range p.Surfaces {
		if x == s {
			return true
		}
	}
	return false
}

// surfaceOf maps a project-relative path to the surface that owns it.
// The empty string means "always present".
func surfaceOf(rel string) string {
	rel = filepath.ToSlash(rel)
	switch {
	case strings.HasPrefix(rel, "backend/"):
		return "backend"
	case strings.HasPrefix(rel, "web/"):
		return "web"
	case strings.HasPrefix(rel, "mobile/"), strings.HasPrefix(rel, "packages/ui-native/"):
		return "mobile"
	case strings.HasPrefix(rel, "packages/ui-web/"):
		return "web"
	case strings.HasPrefix(rel, "packages/"):
		return "client" // web or mobile
	}
	return ""
}

// wants reports whether a project-relative path belongs in the project.
func (p *Project) wants(rel string) bool {
	switch s := surfaceOf(rel); s {
	case "":
		return true
	case "client":
		return p.HasSurface("web") || p.HasSurface("mobile")
	default:
		return p.HasSurface(s)
	}
}

func (p *Project) wantsTier(tier string) bool {
	switch tier {
	case "backend", "web", "mobile":
		return p.HasSurface(tier)
	}
	return true
}

// MergeEnv adds or replaces env vars by name, dropping tiers the project
// lacks. Template literals in values and docs become the project's names.
func (p *Project) MergeEnv(vars []EnvVar) (added []string) {
	r := p.replacer()
	idx := map[string]int{}
	for i, v := range p.Env {
		idx[v.Name+"|"+v.Tier] = i
	}
	for _, v := range vars {
		if !p.wantsTier(v.Tier) {
			continue
		}
		v.Default, v.Dev, v.Doc, v.Source = r.Replace(v.Default), r.Replace(v.Dev), r.Replace(v.Doc), r.Replace(v.Source)
		k := v.Name + "|" + v.Tier
		if i, ok := idx[k]; ok {
			p.Env[i] = v
			continue
		}
		idx[k] = len(p.Env)
		p.Env = append(p.Env, v)
		added = append(added, v.Name)
	}
	return added
}

var tierOrder = map[string]int{"backend": 0, "web": 1, "mobile": 2, "dev": 3}

func envLess(a, b EnvVar) bool {
	if tierOrder[a.Tier] != tierOrder[b.Tier] {
		return tierOrder[a.Tier] < tierOrder[b.Tier]
	}
	ga, gb := groupRank(a.Group), groupRank(b.Group)
	if ga != gb {
		return ga < gb
	}
	if a.Group != b.Group {
		return a.Group < b.Group
	}
	return a.Name < b.Name
}

// Core groups sort first, in this order; feature groups follow alphabetically.
var coreGroups = []string{"core", "database", "auth", "storage", "email", "push", "observability", "dev"}

func groupRank(g string) int {
	for i, c := range coreGroups {
		if g == c {
			return i
		}
	}
	return len(coreGroups)
}
