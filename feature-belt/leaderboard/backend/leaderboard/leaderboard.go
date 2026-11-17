// Package leaderboard ranks people by scores the app provides: one or more
// boards (profit and loss, points, streaks), each a list of (user, score)
// refreshed on a schedule or pushed as scores change. People appear under a
// generated alias by default and choose to show their name or hide.
//
// Boards come from two places:
//   - RegisterBoard, from the app's stitching code;
//   - any installed module implementing Source (found in Deps.Modules, so the
//     two features never import each other).
package leaderboard

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/jobs"
)

// Board formats.
const (
	FormatNumber  = "number"
	FormatMoney   = "money"
	FormatPercent = "percent"
)

// Visibility of a person on boards.
const (
	ShowAlias = "alias" // a generated alias (the default)
	ShowName  = "name"  // their display name and photo
	Hidden    = "hidden"
)

// Board is one ranking.
type Board struct {
	ID        string // lowercase a–z, 0–9, '-' and '_' (URL segment)
	Title     string
	Format    string // FormatNumber | FormatMoney | FormatPercent
	Currency  string // FormatMoney: the currency code shown with scores
	Ascending bool   // lower scores rank first (fastest time)
	// Scores returns the whole board (every ranked person), called every
	// Refresh. Nil for push-only boards fed by SetScore.
	Scores  func(ctx context.Context) (map[uuid.UUID]decimal.Decimal, error)
	Refresh time.Duration // default 1 minute
}

// Score is one entry pushed with SetScore.
type Score struct {
	UserID uuid.UUID
	Value  decimal.Decimal
}

// Source is a module that provides a board. The leaderboard finds it among
// the installed modules; implementing it needs no import of this package.
// unit is "" (a plain number), "%" (a percentage) or a currency code (money).
type Source interface {
	LeaderboardBoard() (id, title, unit string)
	LeaderboardScores(ctx context.Context) (map[uuid.UUID]decimal.Decimal, error)
}

var (
	regMu  sync.RWMutex
	boards = map[string]Board{}
	order  []string
)

// RegisterBoard adds a board (call from stitching code at init). A later
// registration with the same id replaces the earlier one.
func RegisterBoard(b Board) {
	regMu.Lock()
	defer regMu.Unlock()
	if b.Format == "" {
		b.Format = FormatNumber
	}
	if b.Refresh <= 0 {
		b.Refresh = time.Minute
	}
	if _, ok := boards[b.ID]; !ok {
		order = append(order, b.ID)
	}
	boards[b.ID] = b
}

// SetScore records one person's score on a push-only board (or overrides it
// on a scheduled one until the next refresh). Ranks update within the
// refresh interval.
func SetScore(ctx context.Context, q db.Querier, board string, userID uuid.UUID, value decimal.Decimal) error {
	_, err := q.Exec(ctx, `INSERT INTO leaderboard_entries (board, user_id, score) VALUES ($1, $2, $3)
		ON CONFLICT (board, user_id) DO UPDATE SET score = EXCLUDED.score, updated_at = now()`, board, userID, value.String())
	return err
}

// RemoveScore takes a person off a board.
func RemoveScore(ctx context.Context, q db.Querier, board string, userID uuid.UUID) error {
	_, err := q.Exec(ctx, `DELETE FROM leaderboard_entries WHERE board = $1 AND user_id = $2`, board, userID)
	return err
}

type Service struct {
	d                 *app.Deps
	defaultVisibility string

	mu      sync.Mutex
	lastRun map[string]time.Time
}

func New(d *app.Deps) *Service {
	v := config.String("LEADERBOARD_DEFAULT_VISIBILITY", ShowAlias)
	if v != ShowAlias && v != ShowName && v != Hidden {
		config.Problem("LEADERBOARD_DEFAULT_VISIBILITY must be alias, name or hidden (got %q)", v)
		v = ShowAlias
	}
	return &Service{d: d, defaultVisibility: v, lastRun: map[string]time.Time{}}
}

func (s *Service) Name() string { return "leaderboard" }

func (s *Service) MountUser(r chi.Router) {
	r.Get("/leaderboard/boards", httpx.Handler(s.boardsHandler))
	r.Get("/leaderboard/boards/{board}", httpx.Handler(s.boardHandler))
	r.Get("/leaderboard/boards/{board}/me", httpx.Handler(s.meHandler))
	r.Get("/leaderboard/profile", httpx.Handler(s.profileHandler))
	r.With(auth.RequireActive, auth.RateLimit(30, time.Hour)).Put("/leaderboard/profile", httpx.Handler(s.updateProfileHandler))
}

// Start refreshes scheduled boards and ranks every 15 seconds (once across instances).
func (s *Service) Start(ctx context.Context) {
	go jobs.Every(ctx, s.d.Pool, "leaderboard.refresh", 15*time.Second, s.refresh)
}

// all returns every board: registered ones in order, then module sources.
func (s *Service) all() []Board {
	regMu.RLock()
	out := make([]Board, 0, len(order))
	seen := map[string]bool{}
	for _, id := range order {
		out = append(out, boards[id])
		seen[id] = true
	}
	regMu.RUnlock()
	for _, m := range s.d.Modules {
		src, ok := m.(Source)
		if !ok {
			continue
		}
		id, title, unit := src.LeaderboardBoard()
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		b := Board{ID: id, Title: title, Format: FormatNumber, Scores: src.LeaderboardScores, Refresh: time.Minute}
		switch unit {
		case "":
		case "%":
			b.Format = FormatPercent
		default:
			b.Format, b.Currency = FormatMoney, unit
		}
		out = append(out, b)
	}
	return out
}

func (s *Service) board(id string) (Board, bool) {
	for _, b := range s.all() {
		if b.ID == id {
			return b, true
		}
	}
	return Board{}, false
}

func validBoardID(id string) bool {
	if id == "" || len(id) > 40 {
		return false
	}
	return strings.Trim(id, "abcdefghijklmnopqrstuvwxyz0123456789-_") == ""
}
