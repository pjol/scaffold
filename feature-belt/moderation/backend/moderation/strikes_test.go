package moderation

import (
	"testing"
	"time"
)

func TestParseLadder(t *testing.T) {
	l, err := parseLadder("5:168h, 3:24h")
	if err != nil || len(l) != 2 || l[0].Strikes != 3 || l[1].Restrict != 168*time.Hour {
		t.Fatalf("ladder %+v %v", l, err)
	}
	if l, err := parseLadder("off"); err != nil || l != nil {
		t.Fatalf("off: %+v %v", l, err)
	}
	for _, bad := range []string{"3", "0:24h", "3:-1h", "3:soon", "3:24h,3:48h"} {
		if _, err := parseLadder(bad); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
}

func TestRestrictionAfter(t *testing.T) {
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	latest := now.Add(-2 * time.Hour)
	cases := []struct {
		n    int
		want time.Time
	}{
		{0, time.Time{}},
		{2, time.Time{}},
		{3, latest.Add(24 * time.Hour)},
		{4, latest.Add(24 * time.Hour)},
		{5, latest.Add(7 * 24 * time.Hour)},
	}
	for _, c := range cases {
		if got := restrictionAfter(defaultLadder, c.n, &latest, now); !got.Equal(c.want) {
			t.Fatalf("%d strikes: %v, want %v", c.n, got, c.want)
		}
	}
	old := now.Add(-48 * time.Hour)
	if got := restrictionAfter(defaultLadder, 3, &old, now); !got.IsZero() {
		t.Fatalf("an expired restriction still applies: %v", got)
	}
	if got := restrictionAfter(defaultLadder, 9, nil, now); !got.IsZero() {
		t.Fatal("no strikes, no restriction")
	}
}
