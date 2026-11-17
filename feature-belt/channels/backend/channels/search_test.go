package channels

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/safehttp"
)

func TestParseSearchOperators(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Skip("no zoneinfo")
	}
	now := time.Date(2026, 11, 4, 15, 0, 0, 0, loc) // a Wednesday
	day := func(m time.Month, d int) time.Time { return time.Date(2026, m, d, 0, 0, 0, 0, loc) }
	q := parseSearch("deploy in:#Ops from:@sam with:@priya to:me has:link has:pin has:reaction has:file is:saved is:thread after:2026-10-01 before:2026-11-01", now, loc)
	if q.Text != "deploy" || q.InChannel != "ops" || q.From != "sam" || q.With != "priya" || !q.ToMe || !q.HasLink || !q.HasPin || !q.HasReaction || !q.HasFile || !q.IsSaved || !q.IsThread {
		t.Fatalf("flags: %+v", q)
	}
	if !q.After.Equal(day(10, 2)) || !q.Before.Equal(day(11, 1)) {
		t.Fatalf("range = %v .. %v", q.After, q.Before)
	}
	on := parseSearch("on:yesterday", now, loc)
	if on.Text != "" || !on.After.Equal(day(11, 3)) || !on.Before.Equal(day(11, 4)) {
		t.Fatalf("on:yesterday = %+v", on)
	}
	week := parseSearch("standup during:week", now, loc)
	if !week.After.Equal(day(11, 2)) || !week.Before.Equal(day(11, 9)) || week.Text != "standup" {
		t.Fatalf("during:week = %v .. %v (%q)", week.After, week.Before, week.Text)
	}
	month := parseSearch("during:september", now, loc)
	if !month.After.Equal(day(9, 1)) || !month.Before.Equal(day(10, 1)) {
		t.Fatalf("during:september = %v .. %v", month.After, month.Before)
	}
	dec := parseSearch("during:december", now, loc) // December hasn't come yet: last year's
	if dec.After.Year() != 2025 {
		t.Fatalf("during:december = %v", dec.After)
	}
	ym := parseSearch("during:2026-02", now, loc)
	if !ym.After.Equal(day(2, 1)) || !ym.Before.Equal(day(3, 1)) {
		t.Fatalf("during:2026-02 = %v .. %v", ym.After, ym.Before)
	}
	// Two date filters narrow to their overlap.
	both := parseSearch("during:month on:2026-11-03", now, loc)
	if !both.After.Equal(day(11, 3)) || !both.Before.Equal(day(11, 4)) {
		t.Fatalf("overlap = %v .. %v", both.After, both.Before)
	}
	// Unknown values stay text; words with colons in the middle aren't filters.
	odd := parseSearch("before:someday has:nothing ratio:3 to:you", now, loc)
	if odd.Text != "before:someday has:nothing ratio:3 to:you" || odd.filtered() {
		t.Fatalf("odd = %+v", odd)
	}
	if !parseSearch("from:me", now, loc).filtered() {
		t.Fatal("from:me alone should count as a filter")
	}
}

func TestResolveSendAt(t *testing.T) {
	now := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	if _, err := resolveSendAt(nil, "", now, time.UTC); err == nil {
		t.Fatal("no time accepted")
	}
	soon := now.Add(10 * time.Second)
	if _, err := resolveSendAt(&soon, "", now, time.UTC); err == nil {
		t.Fatal("10 seconds ahead accepted")
	}
	far := now.AddDate(2, 0, 0)
	if _, err := resolveSendAt(&far, "", now, time.UTC); err == nil {
		t.Fatal("2 years ahead accepted")
	}
	got, err := resolveSendAt(nil, "tomorrow at 9am", now, time.UTC)
	if err != nil || !got.Equal(time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("tomorrow at 9am = %v, %v", got, err)
	}
	if _, err := resolveSendAt(nil, "whenever", now, time.UTC); err == nil {
		t.Fatal("nonsense accepted")
	}
}

// The preview fetcher goes through safehttp: a server on a private address
// is refused outside development, and read in development.
func TestFetchPreviewUsesSafeHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<html><head><meta property="og:title" content="Internal dashboard"><meta property="og:site_name" content="Intranet"></head></html>`)
	}))
	defer srv.Close()
	newSvc := func(dev bool) *Service {
		s := &Service{d: &app.Deps{Cfg: &config.Config{}}, devFetch: dev}
		s.pageClient = safehttp.Client(safehttp.Options{Timeout: 3 * time.Second, MaxBody: -1, MaxRedirects: 2, AllowPrivate: dev, AllowHTTP: dev})
		s.imageClient = safehttp.Client(safehttp.Options{Timeout: 3 * time.Second, MaxBody: 1 << 20, AllowPrivate: dev, AllowHTTP: dev})
		return s
	}
	ctx := context.Background()
	if got := newSvc(false).fetchPreview(ctx, srv.URL+"/page"); got.status != "error" {
		t.Fatalf("private address fetched outside development: %+v", got)
	}
	got := newSvc(true).fetchPreview(ctx, srv.URL+"/page")
	if got.status != "ok" || got.meta.Title != "Internal dashboard" || got.meta.SiteName != "Intranet" {
		t.Fatalf("development fetch = %+v", got)
	}
	// The metadata server address (link-local) is refused even in development.
	if got := newSvc(true).fetchPreview(ctx, "http://169.254.169.254/latest/meta-data/"); got.status != "error" {
		t.Fatalf("metadata address fetched: %+v", got)
	}
}
