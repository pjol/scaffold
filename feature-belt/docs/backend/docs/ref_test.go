package docs

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/auth"
)

func TestPageIDFromPath(t *testing.T) {
	id := uuid.New()
	for path, want := range map[string]bool{
		"/docs/" + id.String():              true,
		"/docs/" + id.String() + "/":        true,
		"/docs/":                            false,
		"/docs/c/conn/" + id.String():       false,
		"/docs/forms/" + id.String():        false,
		"/docs/" + id.String() + "/history": false,
		"/drive/" + id.String():             false,
		"/p/" + id.String():                 false,
		"/docs/not-a-uuid":                  false,
	} {
		got, ok := PageIDFromPath(path)
		if ok != want || (ok && got != id) {
			t.Errorf("PageIDFromPath(%q) = %v, %v; want ok=%v", path, got, ok, want)
		}
	}
}

func TestPageRefRefusesWithoutViewer(t *testing.T) {
	// No database is touched: the guard answers before any query.
	if _, err := PageRef(context.Background(), nil, nil, uuid.New()); !errors.Is(err, ErrPageNotFound) {
		t.Fatalf("nil viewer: %v", err)
	}
	if _, err := PageRef(context.Background(), nil, &auth.Identity{UserID: uuid.New()}, uuid.Nil); !errors.Is(err, ErrPageNotFound) {
		t.Fatalf("nil page id: %v", err)
	}
}
