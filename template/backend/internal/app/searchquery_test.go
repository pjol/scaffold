package app

import (
	"testing"
	"time"
)

func TestParseSearchQuery(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Berlin")
	now := time.Date(2026, 11, 5, 15, 0, 0, 0, loc)

	q := ParseSearchQuery(`launch from:@Sam in:"Q3 plan" type:page is:open before:2026-11-01 after:2026-10-01 plan`, now, loc)
	if q.Text != "launch plan" {
		t.Fatalf("text %q", q.Text)
	}
	if len(q.From) != 1 || q.From[0] != "sam" || len(q.In) != 1 || q.In[0] != "q3 plan" || q.Types[0] != "page" || q.Is[0] != "open" {
		t.Fatalf("ops %+v", q)
	}
	if !q.After.Equal(time.Date(2026, 10, 2, 0, 0, 0, 0, loc)) || !q.Before.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, loc)) {
		t.Fatalf("dates %v %v", q.After, q.Before)
	}
	for _, op := range []string{"from", "in", "type", "is", "date"} {
		if !q.Uses(op) {
			t.Errorf("Uses(%s) false", op)
		}
	}

	ch := ParseSearchQuery("deploy in:#Ops from:me has:link with:@priya to:me during:week", now, loc)
	if ch.In[0] != "ops" || ch.From[0] != "me" || ch.Has[0] != "link" || ch.With[0] != "priya" || ch.To[0] != "me" {
		t.Fatalf("channels ops %+v", ch)
	}
	if !ch.After.Equal(time.Date(2026, 11, 2, 0, 0, 0, 0, loc)) || !ch.Before.Equal(time.Date(2026, 11, 9, 0, 0, 0, 0, loc)) {
		t.Fatalf("during:week %v %v", ch.After, ch.Before)
	}

	odd := ParseSearchQuery("before:someday ratio:3 during:never", now, loc)
	if odd.Text != "before:someday ratio:3 during:never" || odd.Filtered() {
		t.Fatalf("odd %+v", odd)
	}
	dec := ParseSearchQuery("during:december", now, loc)
	if dec.After.Year() != 2025 {
		t.Fatalf("december should be last year's: %v", dec.After)
	}
	on := ParseSearchQuery("on:yesterday", now, loc)
	if !on.After.Equal(time.Date(2026, 11, 4, 0, 0, 0, 0, loc)) || !on.Before.Equal(time.Date(2026, 11, 5, 0, 0, 0, 0, loc)) {
		t.Fatalf("on:yesterday %v %v", on.After, on.Before)
	}
}
