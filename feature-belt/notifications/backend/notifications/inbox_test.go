package notifications

import (
	"context"
	"testing"
	"time"
)

func TestResourceOf(t *testing.T) {
	ctx := context.Background()
	RegisterKind(Kind{ID: "rtest.message", Label: "Messages", AgentTarget: DataTarget("rtest.channel", "channel_id")})
	RegisterKind(Kind{ID: "rtest.grouped", Label: "Grouped", AgentTarget: DataTarget("rtest.channel", "channel_id"),
		Group: func(d map[string]any) (string, string) { id, _ := d["page_id"].(string); return "rtest.page", id }})
	cases := []struct {
		kind string
		data map[string]any
		link string
		want string
	}{
		{"rtest.message", map[string]any{"channel_id": "c1"}, "/x/c1?message=m", "rtest.channel:c1"},
		{"rtest.message", map[string]any{"channel_id": "c1"}, "/x/c1?thread=t9&message=m", "rtest.channel:c1/thread:t9"},
		{"rtest.message", map[string]any{"channel_id": "c1", "thread_id": "t2"}, "", "rtest.channel:c1/thread:t2"},
		{"rtest.grouped", map[string]any{"channel_id": "c1", "page_id": "p1"}, "", "rtest.page:p1"},
		{"rtest.unregistered", map[string]any{"kind": "docs.page", "id": "abc"}, "", "docs.page:abc"},
		{"rtest.unregistered", map[string]any{}, "", ""},
		{"rtest.message", map[string]any{"channel_id": "bad id with spaces"}, "", ""},
	}
	for _, c := range cases {
		if got := resourceOf(ctx, c.kind, c.data, c.link); got != c.want {
			t.Errorf("resourceOf(%s, %v, %q) = %q, want %q", c.kind, c.data, c.link, got, c.want)
		}
	}
	if resourceParent("rtest.channel:c1/thread:t9") != "rtest.channel:c1" || resourceParent("rtest.channel:c1") != "" {
		t.Error("resourceParent")
	}
	if muteAllowed("rtest.channel:c1") {
		t.Error("not allowed before AllowMute")
	}
	AllowMute("rtest.channel")
	if !muteAllowed("rtest.channel:c1/thread:t9") || muteAllowed("") {
		t.Error("AllowMute")
	}
}

func TestNextDigestSlot(t *testing.T) {
	berlin, _ := time.LoadLocation("Europe/Berlin")
	now := time.Date(2026, 11, 5, 9, 30, 0, 0, berlin) // Thursday
	if got := nextDigestSlot(now, "daily", 8, 0, berlin); !got.Equal(time.Date(2026, 11, 6, 8, 0, 0, 0, berlin)) {
		t.Errorf("daily past hour: %v", got)
	}
	if got := nextDigestSlot(now, "daily", 17, 0, berlin); !got.Equal(time.Date(2026, 11, 5, 17, 0, 0, 0, berlin)) {
		t.Errorf("daily later today: %v", got)
	}
	if got := nextDigestSlot(now, "weekly", 8, time.Monday, berlin); !got.Equal(time.Date(2026, 11, 9, 8, 0, 0, 0, berlin)) {
		t.Errorf("weekly monday: %v", got)
	}
	if got := nextDigestSlot(now, "weekly", 8, time.Thursday, berlin); !got.Equal(time.Date(2026, 11, 12, 8, 0, 0, 0, berlin)) {
		t.Errorf("weekly same day past hour: %v", got)
	}
}
