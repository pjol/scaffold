package social

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestValidHandle(t *testing.T) {
	for h, want := range map[string]bool{
		"sam": true, "sam_rivera": true, "s4m": true, "abcdefghijklmnopqrst": true,
		"sa": false, "abcdefghijklmnopqrstu": false, "4sam": false, "_sam": false, "sam-r": false, "Sam": false, "sam.r": false, "": false,
	} {
		if got := ValidHandle(h); got != want {
			t.Errorf("ValidHandle(%q) = %v, want %v", h, got, want)
		}
	}
	if NormalizeHandle("  @Sam_Rivera ") != "sam_rivera" {
		t.Error("NormalizeHandle")
	}
}

func TestCheckHandle(t *testing.T) {
	cases := []struct {
		h     string
		extra []string
		staff bool
		code  string // "" = allowed
	}{
		{"sam_rivera", nil, false, ""},
		{"admin", nil, false, "handle_reserved"},
		{"settings", nil, false, "handle_reserved"},
		{"supp0rt_team", nil, false, "handle_reserved"},      // lookalike word
		{"the_moderatr", nil, false, "handle_reserved"},      // edit distance 1 on a word
		{"adm1n", nil, false, "handle_reserved"},             // leetspeak
		{"supp0rt_team", nil, true, ""},                      // staff may
		{"admin", nil, true, "handle_reserved"},              // but never an exact reserved word
		{"acme", []string{"Acme"}, false, "handle_reserved"}, // SOCIAL_RESERVED_HANDLES
		{"supportive", nil, false, ""},                       // three edits from "support"
		{"suport_desk", nil, false, "handle_reserved"},       // one edit from "support"
		{"stuff", nil, false, ""},                            // short names match only exactly
		{"st4ff", nil, false, "handle_reserved"},
		{"admit", nil, false, ""},
		{"9lives", nil, false, "handle_invalid"},
	}
	for _, c := range cases {
		e := checkHandle(c.h, c.extra, c.staff)
		got := ""
		if e != nil {
			got = e.Code
		}
		if got != c.code {
			t.Errorf("checkHandle(%q, staff=%v) = %q, want %q", c.h, c.staff, got, c.code)
		}
	}
}

func TestHandleBaseAndCandidates(t *testing.T) {
	for in, want := range map[string]string{
		"Sam Rivera":             "sam_rivera",
		"  Zoë   O'Brien ":       "zoe_o_brien",
		"42 Priya":               "priya",
		"Ø":                      "",
		"Bartholomew Montgomery": "bartholomew_mo",
		"李":                      "",
	} {
		if got := handleBase(in); got != want {
			t.Errorf("handleBase(%q) = %q, want %q", in, got, want)
		}
	}
	id := uuid.MustParse("0b6c1d2e-3f40-4a5b-8c6d-7e8f90a1b2c3")
	for _, c := range [][2]string{{"Sam Rivera", "sam@x.test"}, {"", "jo.lee@x.test"}, {"", ""}, {"Admin", "admin@x.test"}, {"李", "李@x.test"}} {
		cands := handleCandidates(c[0], c[1], id)
		if len(cands) < 8 {
			t.Fatalf("handleCandidates(%q, %q): %d candidates", c[0], c[1], len(cands))
		}
		for _, h := range cands {
			if !ValidHandle(h) {
				t.Errorf("handleCandidates(%q, %q) produced invalid %q", c[0], c[1], h)
			}
			if reservedExact(h) {
				t.Errorf("handleCandidates(%q, %q) produced reserved %q", c[0], c[1], h)
			}
		}
		if last := cands[len(cands)-1]; last != "user_0b6c1d2e3f40" {
			t.Errorf("fallback = %q", last)
		}
	}
	if got := handleCandidates("Sam Rivera", "", id)[0]; got != "sam_rivera" {
		t.Errorf("first candidate = %q", got)
	}
	if got := handleCandidates("Admin", "admin@x.test", id)[0]; !strings.HasPrefix(got, "user") {
		t.Errorf("a protected base falls back to user…: %q", got)
	}
	if got := handleCandidates("Avery Admin", "avery@x.test", id)[0]; got != "avery" {
		t.Errorf("a protected word falls back to the first name: %q", got)
	}
}

func reservedExact(h string) bool {
	for _, r := range reservedHandles {
		if h == r {
			return true
		}
	}
	return false
}

func TestVisibleMatrix(t *testing.T) {
	cases := []struct {
		vis  string
		v    Viewer
		want bool
	}{
		{VisibilityPrivate, Viewer{Self: true}, true},
		{VisibilityPublic, Viewer{Self: true, Blocked: true}, true},
		{VisibilityPublic, Viewer{}, true},
		{VisibilityPublic, Viewer{Anonymous: true}, true},
		{VisibilityPublic, Viewer{Blocked: true}, false},
		{VisibilityFollowers, Viewer{}, false},
		{VisibilityFollowers, Viewer{Follower: true}, true},
		{VisibilityFollowers, Viewer{Follower: true, Blocked: true}, false},
		{VisibilityFollowers, Viewer{Anonymous: true, Follower: true}, false},
		{VisibilityPrivate, Viewer{Follower: true}, false},
		{"bogus", Viewer{Follower: true}, false},
	}
	for _, c := range cases {
		if got := Visible(c.vis, c.v); got != c.want {
			t.Errorf("Visible(%q, %+v) = %v, want %v", c.vis, c.v, got, c.want)
		}
	}
}

func TestPrivacy(t *testing.T) {
	RegisterSection("test_posts", "Posts", VisibilityFollowers)
	p := parsePrivacy([]byte(`{"activity":"private","connections":"nonsense","test_posts":"public"}`))
	if p.of(SectionActivity) != VisibilityPrivate || p.of(SectionConnections) != VisibilityPublic || p.of("test_posts") != VisibilityPublic {
		t.Errorf("parsePrivacy: %v", p)
	}
	if p.of("unregistered") != VisibilityPrivate {
		t.Error("unknown sections read as private")
	}
	if parsePrivacy(nil).of("test_posts") != VisibilityFollowers {
		t.Error("missing keys read as the default")
	}
	if _, err := p.apply(map[string]string{"nope": "public"}); err == nil {
		t.Error("apply accepted an unknown section")
	}
	if _, err := p.apply(map[string]string{"activity": "friends"}); err == nil {
		t.Error("apply accepted an invalid visibility")
	}
	q, err := p.apply(map[string]string{"activity": "followers"})
	if err != nil || q.of(SectionActivity) != VisibilityFollowers || p.of(SectionActivity) != VisibilityPrivate {
		t.Errorf("apply: %v %v (original must not change)", q, err)
	}

	// A follower sees public and followers sections, a stranger public only,
	// nobody across a block, the owner everything (nil = no filter).
	follower := visibleSections(q, Viewer{Follower: true})
	stranger := visibleSections(q, Viewer{})
	if !contains(follower, SectionActivity) || contains(stranger, SectionActivity) || !contains(stranger, SectionConnections) {
		t.Errorf("visibleSections: follower %v, stranger %v", follower, stranger)
	}
	if got := visibleSections(q, Viewer{Blocked: true, Follower: true}); len(got) != 0 {
		t.Errorf("blocked sees %v", got)
	}
	if visibleSections(q, Viewer{Self: true}) != nil {
		t.Error("the owner is unfiltered")
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func TestCleanLinks(t *testing.T) {
	got, err := cleanLinks([]Link{{URL: " example.com/me ", Label: "  My   site "}, {URL: ""}, {URL: "http://blog.example.org"}})
	if err != nil || len(got) != 2 || got[0].URL != "https://example.com/me" || got[0].Label != "My site" {
		t.Fatalf("cleanLinks = %v, %v", got, err)
	}
	for _, bad := range []string{"javascript:alert(1)", "https://user:pw@example.com", "ftp://example.com", "https://localhost", "mailto:a@b.c"} {
		if _, err := cleanLinks([]Link{{URL: bad}}); err == nil {
			t.Errorf("cleanLinks accepted %q", bad)
		}
	}
	if _, err := cleanLinks(make([]Link, MaxLinks+1)); err == nil {
		t.Error("too many links accepted")
	}
	if _, err := cleanLinks([]Link{{URL: "https://example.com", Label: strings.Repeat("x", 41)}}); err == nil {
		t.Error("long label accepted")
	}
}

func TestRender(t *testing.T) {
	RegisterKind(Kind{ID: "test.note", Label: "Notes"})
	RegisterKind(Kind{ID: "test.panics", Render: func(map[string]any, *Ref) (Rendered, bool) { panic("boom") }})
	actor, subject, blocked := uuid.New(), uuid.New(), uuid.New()
	refs := map[uuid.UUID]Ref{
		actor:   {UserID: actor, Handle: "sam", DisplayName: "Sam"},
		subject: {UserID: subject, Handle: "jo", DisplayName: "Jo Lee"},
		blocked: {UserID: blocked, Handle: "kim", DisplayName: "Kim"},
	}
	at := time.Now()
	row := func(kind string, sub *uuid.UUID, data string) activityRow {
		return activityRow{id: uuid.New(), actor: actor, kind: kind, subject: sub, data: []byte(data), createdAt: at}
	}
	hidden := map[uuid.UUID]bool{blocked: true}

	it, ok := renderOne(row("test.note", nil, `{"title":"Wrote a note","summary":"Hello","link":"/notes/1"}`), refs, hidden)
	if !ok || it.Title != "Wrote a note" || it.Summary != "Hello" || it.Link != "/notes/1" || it.Actor.Handle != "sam" {
		t.Errorf("default render: %+v %v", it, ok)
	}
	if it, ok := renderOne(row("test.note", nil, `{}`), refs, hidden); !ok || it.Title != "Notes" {
		t.Errorf("no title falls back to the label: %+v", it)
	}
	if _, ok := renderOne(row("test.unregistered", nil, `{"title":"x"}`), refs, hidden); ok {
		t.Error("unregistered kinds are left out")
	}
	if _, ok := renderOne(row("test.panics", nil, `{}`), refs, hidden); ok {
		t.Error("a panicking Render drops the item")
	}
	if it, ok := renderOne(row(KindFollowed, &subject, `{}`), refs, hidden); !ok || it.Title != "Followed Jo Lee" || it.Link != "/u/jo" || it.Subject == nil {
		t.Errorf("followed: %+v %v", it, ok)
	}
	if _, ok := renderOne(row(KindFollowed, &blocked, `{}`), refs, hidden); ok {
		t.Error("items about someone a block separates from the viewer are left out")
	}
	gone := uuid.New()
	refs[gone] = Ref{UserID: gone, DisplayName: "Deleted user"}
	if _, ok := renderOne(row(KindFollowed, &gone, `{}`), refs, hidden); ok {
		t.Error("items about deleted accounts are left out")
	}
}

func TestHelpers(t *testing.T) {
	for n, want := range map[int]string{0: "0", 999: "999", 2000: "2,000", 1234567: "1,234,567", -1500: "-1,500"} {
		if got := grouped(n); got != want {
			t.Errorf("grouped(%d) = %q", n, got)
		}
	}
	if likeEscape(`sam_r%\`) != `sam\_r\%\\` {
		t.Errorf("likeEscape = %q", likeEscape(`sam_r%\`))
	}
	if interval(90*time.Minute) != "5400 seconds" {
		t.Error("interval")
	}
	if !validKey("test_posts") || validKey("Posts") || validKey("") {
		t.Error("validKey")
	}
	a, b := uuid.New(), uuid.New()
	if got := uniqueIDs([]uuid.UUID{a, uuid.Nil, a, b}); len(got) != 2 {
		t.Errorf("uniqueIDs = %v", got)
	}
}
