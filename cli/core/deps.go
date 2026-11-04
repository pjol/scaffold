package core

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var npmPackageFiles = map[string]string{
	"web":       "web/package.json",
	"mobile":    "mobile/package.json",
	"shared":    "packages/shared/package.json",
	"ui-web":    "packages/ui-web/package.json",
	"ui-native": "packages/ui-native/package.json",
}

// addNpmDeps inserts missing dependencies right after the "dependencies"
// line of a workspace package.json (text insertion keeps the file's
// formatting). pnpm install sorts and resolves them.
func (p *Project) addNpmDeps(root, pkg string, deps map[string]string, dryRun bool) ([]string, error) {
	rel, ok := npmPackageFiles[pkg]
	if !ok {
		return nil, fmt.Errorf("unknown workspace package %q in deps", pkg)
	}
	if !p.wants(rel) {
		return nil, nil
	}
	path := filepath.Join(root, rel)
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(b), "\n")
	at := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == `"dependencies": {` {
			at = i
			break
		}
	}
	if at < 0 || at+1 >= len(lines) {
		return nil, fmt.Errorf("%s has no \"dependencies\" block; add %v by hand", rel, deps)
	}
	indent := lines[at+1][:len(lines[at+1])-len(strings.TrimLeft(lines[at+1], " \t"))]
	var added, insert []string
	for name, ver := range deps {
		if strings.Contains(string(b), `"`+name+`":`) {
			continue
		}
		insert = append(insert, fmt.Sprintf(`%s"%s": "%s",`, indent, name, ver))
		added = append(added, pkg+":"+name+"@"+ver)
	}
	if len(insert) == 0 || dryRun {
		return added, nil
	}
	out := append([]string{}, lines[:at+1]...)
	out = append(out, insert...)
	out = append(out, lines[at+1:]...)
	return added, os.WriteFile(path, []byte(strings.Join(out, "\n")), 0o644)
}

// goModTidy resolves Go dependencies the installed features import.
func goModTidy(ctx context.Context, dir string) error {
	if _, err := exec.LookPath("go"); err != nil {
		return fmt.Errorf("go not on PATH")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute) // a cold module cache with many features takes minutes
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "mod", "tidy")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		tail := strings.TrimSpace(string(out))
		if len(tail) > 400 {
			tail = tail[len(tail)-400:]
		}
		return fmt.Errorf("%v: %s", err, tail)
	}
	return nil
}
