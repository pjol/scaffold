package moderation

import (
	"context"
	"errors"
	"testing"

	"scaffoldapp/backend/internal/httpx"
)

func TestFilterMatchesNormalisedTerms(t *testing.T) {
	f := newFilter([]string{"badword", "very bad phrase", "ass"}, nil)
	cases := []struct {
		text string
		want bool
	}{
		{"this has a badword in it", true},
		{"BADWORD!", true},
		{"b@dw0rd", true},              // leetspeak
		{"b4dw0rd!!", true},            // leetspeak, trailing punctuation
		{"bádwörd", true},              // accents
		{"b a d w o r d", true},        // spaced out
		{"b.a.d.w.o.r.d", true},        // dotted
		{"baaadwooord", true},          // stretched
		{"bad​word", true},             // zero-width characters are dropped
		{"a very  bad   phrase", true}, // phrases match across extra spaces
		{"a very bad day", false},
		{"badwords are fine", false}, // whole words only
		{"class assignment", false},  // no substring hits
		{"as", false},                // shorter than the term
		{"what an a$$", true},
		{"call 1000 people", false},
		{"", false},
	}
	for _, c := range cases {
		if got := f.matchTerm(c.text); got != c.want {
			t.Errorf("matchTerm(%q) = %v, want %v (tokens %q)", c.text, got, c.want, tokens(c.text))
		}
	}
}

type fakeProvider struct {
	flagged bool
	err     error
}

func (fakeProvider) Name() string { return "fake" }
func (p fakeProvider) Flagged(context.Context, string) (bool, error) {
	return p.flagged, p.err
}

func TestCheck(t *testing.T) {
	defer checker.Store(nil)
	ctx := context.Background()

	checker.Store(newFilter([]string{"badword"}, nil))
	var apiErr *httpx.Error
	if err := Check(ctx, "a b4dw0rd"); !errors.As(err, &apiErr) || apiErr.Status != 422 || apiErr.Code != "content_rejected" {
		t.Fatalf("blocked term: got %v", err)
	}
	if err := Check(ctx, "hello"); err != nil {
		t.Fatalf("clean text: got %v", err)
	}

	checker.Store(newFilter(nil, fakeProvider{flagged: true}))
	if err := Check(ctx, "anything"); err == nil {
		t.Fatal("provider flagged: want rejection")
	}
	checker.Store(newFilter(nil, fakeProvider{err: errors.New("down")}))
	if err := Check(ctx, "anything"); err != nil {
		t.Fatalf("provider error must fail open, got %v", err)
	}
}
