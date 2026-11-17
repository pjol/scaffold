package leaderboard

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"scaffoldapp/backend/internal/db"
)

// refresh reloads scheduled boards that are due, gives new people an alias
// and recomputes every board's ranks. One board or person failing doesn't
// hold up the rest; the errors are returned together.
func (s *Service) refresh(ctx context.Context) error {
	var errs []error
	for _, b := range s.all() {
		if b.Scores != nil && s.due(b) {
			scores, err := b.Scores(ctx)
			if err != nil {
				slog.WarnContext(ctx, "leaderboard: scores failed", "board", b.ID, "err", err)
			} else if err := s.replace(ctx, b.ID, scores); err != nil {
				errs = append(errs, fmt.Errorf("board %s: %w", b.ID, err))
			}
		}
	}
	// Ranking doesn't need profiles (people without one get the default
	// visibility), so it goes ahead even if this fails.
	if err := s.ensureProfiles(ctx); err != nil {
		errs = append(errs, fmt.Errorf("profiles: %w", err))
	}
	for _, b := range s.all() {
		if err := s.rank(ctx, b); err != nil {
			errs = append(errs, fmt.Errorf("rank %s: %w", b.ID, err))
		}
	}
	return errors.Join(errs...)
}

func (s *Service) due(b Board) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if time.Since(s.lastRun[b.ID]) < b.Refresh {
		return false
	}
	s.lastRun[b.ID] = time.Now()
	return true
}

// replace makes scores the board's entire contents (people who can't be
// ranked, such as banned accounts, are left out).
func (s *Service) replace(ctx context.Context, board string, scores map[uuid.UUID]decimal.Decimal) error {
	ids := make([]uuid.UUID, 0, len(scores))
	vals := make([]string, 0, len(scores))
	for id, v := range scores {
		ids = append(ids, id)
		vals = append(vals, v.String())
	}
	return db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM leaderboard_entries WHERE board = $1 AND NOT (user_id = ANY ($2::uuid[]))`, board, ids); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO leaderboard_entries (board, user_id, score)
			SELECT $1, x.user_id, x.score::numeric
			FROM unnest($2::uuid[], $3::text[]) AS x(user_id, score)
			JOIN users u ON u.id = x.user_id AND u.status IN ('active', 'suspended')
			ON CONFLICT (board, user_id) DO UPDATE SET score = EXCLUDED.score, updated_at = now()
			WHERE leaderboard_entries.score IS DISTINCT FROM EXCLUDED.score`, board, ids, vals)
		return err
	})
}

// rank numbers the board's visible entries (ties share a rank); hidden people
// and accounts that can't be ranked get no rank.
func (s *Service) rank(ctx context.Context, b Board) error {
	dir := "DESC"
	if b.Ascending {
		dir = "ASC"
	}
	return db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			WITH ranked AS (
			  SELECT e.user_id, rank() OVER (ORDER BY e.score `+dir+`) AS r
			  FROM leaderboard_entries e
			  JOIN users u ON u.id = e.user_id AND u.status IN ('active', 'suspended')
			  LEFT JOIN leaderboard_profiles p ON p.user_id = e.user_id
			  WHERE e.board = $1 AND COALESCE(p.visibility, $2) <> 'hidden')
			UPDATE leaderboard_entries e SET rank = ranked.r
			FROM ranked WHERE e.board = $1 AND e.user_id = ranked.user_id AND e.rank IS DISTINCT FROM ranked.r`,
			b.ID, s.defaultVisibility); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			UPDATE leaderboard_entries e SET rank = NULL
			WHERE e.board = $1 AND e.rank IS NOT NULL AND (
			  COALESCE((SELECT p.visibility FROM leaderboard_profiles p WHERE p.user_id = e.user_id), $2) = 'hidden'
			  OR NOT EXISTS (SELECT 1 FROM users u WHERE u.id = e.user_id AND u.status IN ('active', 'suspended')))`,
			b.ID, s.defaultVisibility)
		return err
	})
}

// ensureProfiles gives everyone on a board a profile with an alias.
func (s *Service) ensureProfiles(ctx context.Context) error {
	rows, err := s.d.Pool.Query(ctx, `SELECT DISTINCT e.user_id FROM leaderboard_entries e
		LEFT JOIN leaderboard_profiles p ON p.user_id = e.user_id WHERE p.user_id IS NULL LIMIT 1000`)
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := s.profile(ctx, id); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			slog.WarnContext(ctx, "leaderboard: profile failed", "user", id, "err", err)
		}
	}
	return nil
}

var (
	aliasAdjectives = []string{"amber", "brisk", "calm", "clever", "coral", "dusky", "eager", "early", "fair", "gentle",
		"golden", "hazel", "ivory", "jolly", "keen", "lively", "lucky", "misty", "nimble", "olive", "polar", "quiet",
		"rapid", "rosy", "silver", "steady", "sunny", "swift", "tidy", "vivid"}
	aliasNouns = []string{"otter", "falcon", "maple", "comet", "harbor", "meadow", "cedar", "fox", "heron", "lynx", "orca",
		"panda", "quail", "raven", "sparrow", "tiger", "walrus", "badger", "bison", "crane", "dolphin", "ember", "finch",
		"gecko", "ibis", "koala", "lark", "marten", "newt", "willow"}
)

// Generated aliases are adjective-noun-NNNN: 30 × 30 × 9,000 = 8.1 million.
// When random picks keep colliding, the suffix comes from the person's id
// instead (see aliasFor), so creating a profile never runs out of names.
const (
	randomAliasTries = 4
	aliasTries       = randomAliasTries + 2 // then 8 hex digits of the id, then all 32
)

func pick(words []string) string { return words[rand.IntN(len(words))] }

func newAlias() string {
	return fmt.Sprintf("%s-%s-%d", pick(aliasAdjectives), pick(aliasNouns), 1000+rand.IntN(9000))
}

// aliasFor is the alias to try on attempt n for a person: random first, then
// 8 hex digits of their id, then all 32. A hex suffix is never 4 digits long,
// so it can't meet a random alias, and the full id is unique to the person.
func aliasFor(userID uuid.UUID, n int) string {
	switch h := hex.EncodeToString(userID[:]); {
	case n < randomAliasTries:
		return newAlias()
	case n == randomAliasTries:
		return fmt.Sprintf("%s-%s-%s", pick(aliasAdjectives), pick(aliasNouns), h[len(h)-8:])
	default:
		return fmt.Sprintf("%s-%s-%s", pick(aliasAdjectives), pick(aliasNouns), h)
	}
}

// Profile is how a person appears on boards.
type Profile struct {
	Visibility string `json:"visibility"`
	Alias      string `json:"alias"`
}

// profile returns a person's profile, creating it (with a fresh alias and the
// default visibility) on first use.
func (s *Service) profile(ctx context.Context, userID uuid.UUID) (Profile, error) {
	var p Profile
	for n := 0; ; n++ {
		err := s.d.Pool.QueryRow(ctx, `SELECT visibility, alias FROM leaderboard_profiles WHERE user_id = $1`, userID).Scan(&p.Visibility, &p.Alias)
		if err == nil {
			return p, nil
		}
		if !db.IsNoRows(err) {
			return p, err
		}
		if n == aliasTries { // the unique fallback was inserted and the row still isn't there
			return p, fmt.Errorf("leaderboard: could not create a profile for %s", userID)
		}
		// ON CONFLICT covers a taken alias and a profile created concurrently;
		// the next read tells which.
		_, err = s.d.Pool.Exec(ctx, `INSERT INTO leaderboard_profiles (user_id, visibility, alias) VALUES ($1, $2, $3)
			ON CONFLICT DO NOTHING`, userID, s.defaultVisibility, aliasFor(userID, n))
		if err != nil {
			return p, err
		}
	}
}
