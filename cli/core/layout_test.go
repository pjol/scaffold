package core

import "testing"

func TestLayoutPhysicalAndWants(t *testing.T) {
	p := &Project{Surfaces: []string{"backend", "web", "mobile"}, Layout: &Layout{Packages: "mobile/packages", Custom: []string{"backend", "web"}, HandEnv: []string{"backend"}}}
	cases := map[string]string{
		"packages/shared/src/index.ts":  "mobile/packages/shared/src/index.ts",
		"packages":                      "mobile/packages",
		"mobile/app/(tabs)/_layout.tsx": "mobile/app/(tabs)/_layout.tsx",
		"packagesX/a":                   "packagesX/a",
	}
	for in, want := range cases {
		if got := p.physical(in); got != want {
			t.Errorf("physical(%q) = %q, want %q", in, got, want)
		}
	}
	wants := map[string]bool{
		"backend/internal/chat/chat.go":   false, // custom
		"web/src/features/chat/x.tsx":     false, // custom
		"mobile/src/features/chat/x.tsx":  true,
		"packages/shared/src/chat/api.ts": true, // mobile still consumes shared
		"packages/ui-web/src/x.tsx":       false,
		"packages/ui-native/src/x.tsx":    true,
		"docs/features/chat.md":           true,
	}
	for in, want := range wants {
		if got := p.wants(in); got != want {
			t.Errorf("wants(%q) = %v, want %v", in, got, want)
		}
	}
	if p.wantsTier("backend") || !p.wantsTier("mobile") {
		t.Errorf("wantsTier: custom backend must be skipped, mobile kept")
	}
	if !p.isHandEnv("backend") || p.isHandEnv("mobile") {
		t.Errorf("isHandEnv wrong")
	}
	plain := &Project{Surfaces: []string{"backend", "mobile"}}
	if plain.physical("packages/shared/x") != "packages/shared/x" || !plain.wants("backend/x") || plain.layoutSummary() != "" {
		t.Errorf("a project without layout must behave like the template")
	}
}
