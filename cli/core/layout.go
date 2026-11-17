package core

import (
	"path/filepath"
	"strings"
)

// Layout says where an adopted project (one not made by `scaffold new`) keeps what
// the template keeps elsewhere, and which of its surfaces predate the foundation.
// It lives in scaffold.json under "layout"; the zero value is the template's layout.
//
//	"layout": {
//	  "packages": "mobile/packages",   // tokens, ui-native, shared live inside mobile's own workspace
//	  "custom":   ["backend", "web"],  // surfaces built outside the template
//	  "hand_env": ["backend", "web"]   // .env.example files written by hand
//	}
type Layout struct {
	// Packages is the directory holding the workspace packages (tokens, ui-native,
	// ui-web, shared). Default "packages". A project whose web app predates the
	// foundation keeps them inside the mobile app's own pnpm workspace instead.
	Packages string `json:"packages,omitempty"`
	// Custom lists surfaces that exist but don't follow the template's shape (their
	// own backend or web). `add` installs nothing into them and lists what to port by
	// hand; `doctor` skips their anchors, migration ledger and house-rule scan.
	Custom []string `json:"custom,omitempty"`
	// HandEnv lists tiers whose .env.example is written by hand: `env --write` leaves
	// it alone, and `doctor` still checks that it names every declared variable.
	HandEnv []string `json:"hand_env,omitempty"`
}

// isCustom reports whether a surface exists in the project but predates the template.
func (p *Project) isCustom(surface string) bool {
	return p.Layout != nil && contains(p.Layout.Custom, surface)
}

// isHandEnv reports whether a tier's .env.example is maintained by hand.
func (p *Project) isHandEnv(tier string) bool {
	return p.Layout != nil && contains(p.Layout.HandEnv, tier)
}

// packagesDir is the project-relative directory that holds the workspace packages.
func (p *Project) packagesDir() string {
	if p.Layout == nil || strings.Trim(p.Layout.Packages, "/") == "" {
		return "packages"
	}
	return strings.Trim(filepath.ToSlash(p.Layout.Packages), "/")
}

// physical maps a template-relative path ("packages/shared/src/index.ts") to where
// this project keeps it ("mobile/packages/shared/src/index.ts"). Every other path is
// the same in both.
func (p *Project) physical(rel string) string {
	rel = filepath.ToSlash(rel)
	dir := p.packagesDir()
	if dir == "packages" {
		return rel
	}
	if rel == "packages" {
		return dir
	}
	if rest, ok := strings.CutPrefix(rel, "packages/"); ok {
		return dir + "/" + rest
	}
	return rel
}

// customSurfaceOf returns the custom surface a template-relative path belongs to, or "".
func (p *Project) customSurfaceOf(rel string) string {
	switch s := surfaceOf(rel); s {
	case "backend", "web", "mobile":
		if p.HasSurface(s) && p.isCustom(s) {
			return s
		}
	}
	return ""
}

// layoutSummary describes a non-default layout for doctor, or "" for the template's.
func (p *Project) layoutSummary() string {
	if p.Layout == nil {
		return ""
	}
	var parts []string
	if d := p.packagesDir(); d != "packages" {
		parts = append(parts, "packages in "+d+"/")
	}
	if len(p.Layout.Custom) > 0 {
		parts = append(parts, "custom surfaces "+strings.Join(p.Layout.Custom, ", "))
	}
	if len(p.Layout.HandEnv) > 0 {
		parts = append(parts, "hand-written .env.example for "+strings.Join(p.Layout.HandEnv, ", "))
	}
	return strings.Join(parts, " · ")
}
