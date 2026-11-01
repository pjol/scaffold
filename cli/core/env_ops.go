package core

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// envExampleFiles maps a tier to the example file `env --write` generates.
// The real files sit beside them (backend/.env, web/.env.local, mobile/.env,
// .env.dev) and are gitignored; dev-up creates them from these.
var envExampleFiles = map[string]string{
	"backend": "backend/.env.example",
	"web":     "web/.env.example",
	"mobile":  "mobile/.env.example",
	"dev":     ".env.dev.example",
}

const envBegin, envEnd = "<!-- scaffold:env:begin -->", "<!-- scaffold:env:end -->"

type EnvInput struct {
	Dir   string `json:"dir,omitempty" jsonschema:"project root (default: current directory)"`
	Write bool   `json:"write,omitempty" jsonschema:"regenerate the ENV.md table and the .env.example files from scaffold.json"`
}

type EnvOutput struct {
	Vars         []EnvVar `json:"vars"`
	Undocumented []string `json:"undocumented,omitempty"` // read in code, missing from scaffold.json
	Written      []string `json:"written,omitempty"`
}

func (o EnvOutput) Text() string {
	var b strings.Builder
	tier := ""
	for _, v := range o.Vars {
		if v.Tier != tier {
			tier = v.Tier
			fmt.Fprintf(&b, "\n[%s]\n", tier)
		}
		req := v.Required
		if req == "" {
			req = "optional"
		}
		fmt.Fprintf(&b, "  %-36s %-8s %s\n", v.Name, req, v.Doc)
	}
	if len(o.Undocumented) > 0 {
		fmt.Fprintf(&b, "\nRead in code but not in scaffold.json (add them, then `scaffold env --write`):\n")
		for _, u := range o.Undocumented {
			fmt.Fprintf(&b, "  %s\n", u)
		}
	}
	for _, w := range o.Written {
		fmt.Fprintf(&b, "wrote %s\n", w)
	}
	return b.String()
}

func (k *Kit) Env(_ context.Context, in EnvInput) (EnvOutput, error) {
	dir, err := filepath.Abs(orDefault(in.Dir, "."))
	if err != nil {
		return EnvOutput{}, err
	}
	p, err := LoadProject(dir)
	if err != nil {
		return EnvOutput{}, err
	}
	sort.SliceStable(p.Env, func(i, j int) bool { return envLess(p.Env[i], p.Env[j]) })
	out := EnvOutput{Vars: p.Env}
	used, err := scanEnvUsage(dir)
	if err != nil {
		return out, err
	}
	known := map[string]bool{}
	for _, v := range p.Env {
		known[v.Name] = true
	}
	for name, where := range used {
		if !known[name] && !ambientEnv[name] {
			out.Undocumented = append(out.Undocumented, name+"  ("+where+")")
		}
	}
	sort.Strings(out.Undocumented)
	if !in.Write {
		return out, nil
	}
	written, err := writeEnvDocs(dir, p)
	out.Written = written
	return out, err
}

// ambientEnv are variables set by tools or the OS, not by the project.
var ambientEnv = map[string]bool{
	"NODE_ENV": true, "HOME": true, "PATH": true, "USER": true, "SHELL": true, "TERM": true,
	"TMPDIR": true, "CI": true, "NEXT_RUNTIME": true, "EXPO_OS": true, "PWD": true,
	"EAS_BUILD": true, "EAS_BUILD_PROFILE": true, "GOOS": true, "GOARCH": true, "TZ": true,
	"NO_COLOR": true, "NVM_DIR": true, "ANDROID_HOME": true, "ANDROID_SDK_ROOT": true,
	"PLAYWRIGHT_MODULE": true, "PLAYWRIGHT_BROWSERS_PATH": true, "SCAFFOLD_HOME": true,
}

var envUsePatterns = []*regexp.Regexp{
	regexp.MustCompile(`\b(?:Getenv|LookupEnv|String|Required|RequiredOutsideDev|Int|Bool|Duration|List)\(\s*"([A-Z][A-Z0-9_]+)"`), // Go (os + config helpers)
	regexp.MustCompile(`process\.env\.([A-Z][A-Z0-9_]+)`),                                                                          // TS
	regexp.MustCompile(`process\.env\[\s*["']([A-Z][A-Z0-9_]+)["']\s*\]`),                                                          // TS
	regexp.MustCompile(`\$\{([A-Z][A-Z0-9_]+):[-=]`),                                                                               // shell defaults
}

func scanEnvUsage(dir string) (map[string]string, error) {
	used := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipNames[d.Name()] || strings.HasPrefix(d.Name(), ".") && path != dir {
				return filepath.SkipDir
			}
			return nil
		}
		ext := filepath.Ext(path)
		switch ext {
		case ".go", ".ts", ".tsx", ".js", ".mjs":
		default:
			return nil
		}
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		for _, re := range envUsePatterns {
			for _, m := range re.FindAllSubmatch(b, -1) {
				name := string(m[1])
				if strings.HasSuffix(name, "_") { // a prefix in prose, e.g. process.env.NEXT_PUBLIC_*
					continue
				}
				if _, ok := used[name]; !ok {
					used[name] = filepath.ToSlash(rel)
				}
			}
		}
		return nil
	})
	return used, err
}

func writeEnvDocs(dir string, p *Project) ([]string, error) {
	var written []string

	// ENV.md: regenerate the table between markers, keep the prose around it.
	mdPath := filepath.Join(dir, "ENV.md")
	md, _ := os.ReadFile(mdPath)
	table := envTable(p)
	content := string(md)
	if i, j := strings.Index(content, envBegin), strings.Index(content, envEnd); i >= 0 && j > i {
		content = content[:i] + envBegin + "\n" + table + envEnd + content[j+len(envEnd):]
	} else {
		if content == "" {
			content = "# Environment\n\n"
		}
		content += "\n" + envBegin + "\n" + table + envEnd + "\n"
	}
	if err := os.WriteFile(mdPath, []byte(content), 0o644); err != nil {
		return written, err
	}
	written = append(written, "ENV.md")

	// .env.example per tier.
	byTier := map[string][]EnvVar{}
	for _, v := range p.Env {
		byTier[v.Tier] = append(byTier[v.Tier], v)
	}
	for tier, rel := range envExampleFiles {
		vars := byTier[tier]
		if tier == "dev" {
			vars = append(append([]EnvVar{}, vars...), devOverrides(p)...)
			sort.SliceStable(vars, func(i, j int) bool { return envLess(vars[i], vars[j]) })
		}
		if len(vars) == 0 || !p.wants(rel) {
			continue
		}
		var b strings.Builder
		fmt.Fprintf(&b, "# Generated by `scaffold env --write` from scaffold.json. Edit scaffold.json, not this file.\n")
		if tier == "dev" {
			b.WriteString(devHeader)
		} else {
			fmt.Fprintf(&b, "# dev-up writes the real file from scaffold.json and .env.dev; ENV.md explains each variable.\n")
		}
		group := ""
		for _, v := range vars {
			if v.Group != group {
				group = v.Group
				fmt.Fprintf(&b, "\n# --- %s ---\n", group)
			}
			flags := []string{}
			if v.Required == "always" || v.Required == "prod" {
				flags = append(flags, "required:"+v.Required)
			}
			if v.Secret {
				flags = append(flags, "secret")
			}
			note := v.Doc
			if len(flags) > 0 && tier != "dev" {
				note += " [" + strings.Join(flags, ", ") + "]"
			}
			val := v.Dev
			if val == "" && !v.Secret {
				val = v.Default
			}
			if tier == "dev" {
				val = "" // .env.dev holds only what the developer chooses to set
			}
			fmt.Fprintf(&b, "# %s\n%s=%s\n", note, v.Name, val)
		}
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, rel)), 0o755); err != nil {
			return written, err
		}
		if err := os.WriteFile(filepath.Join(dir, rel), []byte(b.String()), 0o644); err != nil {
			return written, err
		}
		written = append(written, rel)
	}
	return written, nil
}

const devHeader = `# .env.dev is read only by scripts/dev-up (copied from this file on first run) and is
# never sourced: scripts/dev-up/env.mjs parses it and writes each value into the runtime
# file of every tier that declares the name (backend/.env, web/.env.local, mobile/.env).
# Every key is optional in development; blank means unset (dev fallbacks apply). X here
# also fills NEXT_PUBLIC_X / EXPO_PUBLIC_X. Flags win over this file; PORT, DATABASE_URL
# and APP_ENV always belong to dev-up. Below: dev-up settings, then the provider keys
# you may paste to use real services locally (test-mode keys only).
`

// devOverrides lists, once per name, the provider-backed variables a developer
// may paste into .env.dev: secrets and anything with a source, minus client
// copies (the unprefixed name fans out) and what dev-up owns.
func devOverrides(p *Project) []EnvVar {
	owned := map[string]bool{"DATABASE_URL": true, "PORT": true, "APP_ENV": true}
	seen := map[string]bool{}
	for _, v := range p.Env {
		if v.Tier == "dev" {
			seen[v.Name] = true
		}
	}
	var out []EnvVar
	for _, v := range p.Env {
		name := v.Name
		if strings.HasPrefix(name, "NEXT_PUBLIC_") || strings.HasPrefix(name, "EXPO_PUBLIC_") || owned[name] || seen[name] {
			continue
		}
		if !v.Secret && v.Source == "" {
			continue
		}
		seen[name] = true
		out = append(out, EnvVar{Name: name, Tier: "dev", Group: v.Group, Doc: v.Doc})
	}
	sort.SliceStable(out, func(i, j int) bool { return envLess(out[i], out[j]) })
	return out
}

func envTable(p *Project) string {
	var b strings.Builder
	tier := ""
	for _, v := range p.Env {
		if v.Tier != tier {
			tier = v.Tier
			fmt.Fprintf(&b, "\n### %s — `%s`\n\n", tierTitle[tier], envRealFile[tier])
			b.WriteString("| Variable | Group | Required | Default | Dev value | What it is |\n|---|---|---|---|---|---|\n")
		}
		req := orDefault(v.Required, "optional")
		def := cell(v.Default)
		dev := cell(v.Dev)
		if v.Secret {
			dev = "secret"
			if v.Dev != "" {
				dev = "`" + v.Dev + "` (dev only)"
			}
		}
		doc := v.Doc
		if v.Source != "" {
			doc += " Source: " + v.Source + "."
		}
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s | %s | %s |\n", v.Name, v.Group, req, def, dev, strings.ReplaceAll(doc, "|", "\\|"))
	}
	b.WriteString("\n")
	return b.String()
}

func cell(v string) string {
	if v == "" {
		return "—"
	}
	return "`" + strings.ReplaceAll(v, "|", "\\|") + "`"
}

var tierTitle = map[string]string{"backend": "Backend", "web": "Web", "mobile": "Mobile", "dev": "Dev tooling"}
var envRealFile = map[string]string{"backend": "backend/.env", "web": "web/.env.local", "mobile": "mobile/.env", "dev": ".env.dev"}
