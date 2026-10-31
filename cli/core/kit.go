// Package core holds every scaffold operation. Interfaces (CLI, MCP, HTTP)
// are thin adapters over Kit.Operations(); add behaviour here, never in an
// adapter.
package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Kit is the scaffold toolset bound to one scaffold home directory (the
// directory holding SKILL.md, concrete/, feature-belt/ and template/).
type Kit struct {
	Home string
}

// FindHome resolves the scaffold home: explicit flag, then $SCAFFOLD_HOME,
// then the executable's ancestors, then the working directory's ancestors.
func FindHome(explicit string) (string, error) {
	var candidates []string
	if explicit != "" {
		candidates = append(candidates, explicit)
	}
	if env := os.Getenv("SCAFFOLD_HOME"); env != "" {
		candidates = append(candidates, env)
	}
	if exe, err := os.Executable(); err == nil {
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			candidates = append(candidates, ancestors(filepath.Dir(real))...)
		}
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, ancestors(wd)...)
	}
	for _, c := range candidates {
		if isHome(c) {
			abs, _ := filepath.Abs(c)
			return abs, nil
		}
	}
	return "", errors.New("scaffold home not found: pass --home, set SCAFFOLD_HOME, or run from inside the scaffold directory")
}

func ancestors(dir string) []string {
	var out []string
	for {
		out = append(out, dir)
		parent := filepath.Dir(dir)
		if parent == dir {
			return out
		}
		dir = parent
	}
}

func isHome(dir string) bool {
	for _, p := range []string{"SKILL.md", "feature-belt", "template", "concrete"} {
		if _, err := os.Stat(filepath.Join(dir, p)); err != nil {
			return false
		}
	}
	return true
}

func (k *Kit) beltDir() string     { return filepath.Join(k.Home, "feature-belt") }
func (k *Kit) templateDir() string { return filepath.Join(k.Home, "template") }

// Catalog loads every feature manifest in the belt, sorted by name.
func (k *Kit) Catalog() (map[string]*Feature, []string, error) {
	entries, err := os.ReadDir(k.beltDir())
	if err != nil {
		return nil, nil, err
	}
	byName := map[string]*Feature{}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(k.beltDir(), e.Name())
		path := filepath.Join(dir, "feature.json")
		if _, err := os.Stat(path); err != nil {
			continue
		}
		var f Feature
		if err := readJSON(path, &f); err != nil {
			return nil, nil, err
		}
		if f.Name != e.Name() {
			return nil, nil, fmt.Errorf("%s: name %q does not match its directory", path, f.Name)
		}
		f.Dir = dir
		byName[f.Name] = &f
		names = append(names, f.Name)
	}
	sort.Strings(names)
	return byName, names, nil
}

// Resolve expands requested features with their requirements, in install
// order (requirements first). Already-installed features are skipped.
func (k *Kit) Resolve(want []string, installed map[string]string) ([]*Feature, error) {
	cat, _, err := k.Catalog()
	if err != nil {
		return nil, err
	}
	var order []*Feature
	state := map[string]int{} // 0 unseen, 1 visiting, 2 done
	var visit func(name, from string) error
	visit = func(name, from string) error {
		f, ok := cat[name]
		if !ok {
			if from != "" {
				return fmt.Errorf("feature %q requires unknown feature %q", from, name)
			}
			return fmt.Errorf("unknown feature %q (see `scaffold list`)", name)
		}
		switch state[name] {
		case 1:
			return fmt.Errorf("feature requirement cycle at %q", name)
		case 2:
			return nil
		}
		state[name] = 1
		for _, r := range f.Requires {
			if err := visit(r, name); err != nil {
				return err
			}
		}
		state[name] = 2
		if _, done := installed[name]; !done {
			order = append(order, f)
		}
		return nil
	}
	for _, n := range want {
		if err := visit(n, ""); err != nil {
			return nil, err
		}
	}
	return order, nil
}
