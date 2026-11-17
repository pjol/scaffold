package leaderboard

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"scaffoldapp/backend/internal/app"
)

type source struct{}

func (source) Name() string                               { return "src" }
func (source) LeaderboardBoard() (string, string, string) { return "pnl", "Profit and loss", "USD" }
func (source) LeaderboardScores(context.Context) (map[uuid.UUID]decimal.Decimal, error) {
	return map[uuid.UUID]decimal.Decimal{}, nil
}

func TestBoardsFromRegistryAndModules(t *testing.T) {
	RegisterBoard(Board{ID: "points", Title: "Points"})
	s := &Service{d: &app.Deps{Modules: []app.Module{source{}}}, lastRun: map[string]time.Time{}}
	all := s.all()
	if len(all) != 2 || all[0].ID != "points" || all[0].Format != FormatNumber || all[0].Refresh != time.Minute {
		t.Fatalf("registered board first, with defaults: %+v", all)
	}
	if b := all[1]; b.ID != "pnl" || b.Format != FormatMoney || b.Currency != "USD" || b.Scores == nil {
		t.Fatalf("module source board: %+v", b)
	}
	if _, ok := s.board("pnl"); !ok {
		t.Fatal("board lookup")
	}
}

func TestValidBoardID(t *testing.T) {
	for id, want := range map[string]bool{"pnl": true, "weekly_points": true, "a-b": true, "": false, "Bad": false, "x/y": false} {
		if got := validBoardID(id); got != want {
			t.Errorf("validBoardID(%q) = %v", id, got)
		}
	}
}

func TestAlias(t *testing.T) {
	random := regexp.MustCompile(`^[a-z]+-[a-z]+-[1-9][0-9]{3}$`)
	for range 50 {
		if a := newAlias(); !random.MatchString(a) {
			t.Fatalf("alias %q", a)
		}
	}
	id := uuid.MustParse("0192f5c4-7e1a-7b3d-9c2e-5a6b7c8d9e0f")
	for n := range randomAliasTries {
		if a := aliasFor(id, n); !random.MatchString(a) {
			t.Fatalf("try %d: alias %q", n, a)
		}
	}
	if a := aliasFor(id, randomAliasTries); !regexp.MustCompile(`^[a-z]+-[a-z]+-7c8d9e0f$`).MatchString(a) {
		t.Fatalf("short fallback %q", a)
	}
	if a := aliasFor(id, randomAliasTries+1); !regexp.MustCompile(`^[a-z]+-[a-z]+-0192f5c47e1a7b3d9c2e5a6b7c8d9e0f$`).MatchString(a) {
		t.Fatalf("full fallback %q", a)
	}
}
