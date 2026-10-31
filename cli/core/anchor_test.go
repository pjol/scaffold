package core

import "testing"

func TestIsAnchorLine(t *testing.T) {
	cases := []struct {
		line, anchor string
		want         bool
	}{
		{"// scaffold:imports", "imports", true},
		{"  // scaffold:nav", "nav", true},
		{"      {/* scaffold:tabs */}", "tabs", true},
		{"<!-- scaffold:features -->", "features", true},
		{"// scaffold:admin-nav", "admin-nav", true},
		{"// scaffold:admin-nav", "admin", false},
		{" *   // scaffold:imports    import { x } from \"./x\";", "imports", false},
		{" * `<Tabs.Screen>` above the scaffold:tabs anchor", "tabs", false},
		{"const x = 1 // scaffold:nav", "nav", false},
	}
	for _, c := range cases {
		if got := isAnchorLine(c.line, c.anchor); got != c.want {
			t.Errorf("isAnchorLine(%q, %q) = %v, want %v", c.line, c.anchor, got, c.want)
		}
	}
}
