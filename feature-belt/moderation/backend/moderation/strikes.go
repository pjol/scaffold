package moderation

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// Strikes: a staff removal (Remove, or "Also remove the content") or a ban
// adds one to the author. Strikes count for StrikeWindow; reaching a step of
// the ladder (MODERATION_STRIKE_LIMITS, default 3 → 24 h, 5 → 7 days) stops
// the person posting for that long from their latest strike.

// StrikeWindow is how long a strike counts.
const StrikeWindow = 90 * 24 * time.Hour

type strikeStep struct {
	Strikes  int
	Restrict time.Duration
}

var defaultLadder = []strikeStep{{3, 24 * time.Hour}, {5, 7 * 24 * time.Hour}}

// parseLadder reads "3:24h,5:168h": strikes, then how long posting stops.
func parseLadder(s string) ([]strikeStep, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "off" {
		return nil, nil
	}
	var out []strikeStep
	for _, part := range strings.Split(s, ",") {
		n, d, ok := strings.Cut(strings.TrimSpace(part), ":")
		if !ok {
			return nil, fmt.Errorf("%q is not strikes:duration", part)
		}
		k, err := strconv.Atoi(strings.TrimSpace(n))
		if err != nil || k < 1 {
			return nil, fmt.Errorf("%q: strikes must be a positive number", part)
		}
		dur, err := time.ParseDuration(strings.TrimSpace(d))
		if err != nil || dur <= 0 {
			return nil, fmt.Errorf("%q: the duration must be positive, like 24h", part)
		}
		out = append(out, strikeStep{k, dur})
	}
	slices.SortFunc(out, func(a, b strikeStep) int { return a.Strikes - b.Strikes })
	for i := 1; i < len(out); i++ {
		if out[i].Strikes == out[i-1].Strikes {
			return nil, fmt.Errorf("%d strikes appears twice", out[i].Strikes)
		}
	}
	return out, nil
}

// Strikes counts userID's strikes given since (the zero time counts every
// strike). Revoked strikes don't count. The reputation feature's community
// seam reads it.
func Strikes(ctx context.Context, q db.Querier, userID uuid.UUID, since time.Time) (int, error) {
	var n int
	err := q.QueryRow(ctx, `SELECT count(*) FROM moderation_strikes
		WHERE user_id = $1 AND revoked_at IS NULL AND created_at >= $2`, userID, since).Scan(&n)
	return n, err
}

// RestrictedUntil is when userID may post again, or the zero time when they
// may post now.
func RestrictedUntil(ctx context.Context, q db.Querier, userID uuid.UUID) (time.Time, error) {
	ladder := currentScreening().ladder
	if len(ladder) == 0 {
		return time.Time{}, nil
	}
	var (
		n      int
		latest *time.Time
	)
	if err := q.QueryRow(ctx, `SELECT count(*), max(created_at) FROM moderation_strikes
		WHERE user_id = $1 AND revoked_at IS NULL AND created_at > $2`, userID, time.Now().Add(-StrikeWindow)).Scan(&n, &latest); err != nil {
		return time.Time{}, err
	}
	return restrictionAfter(ladder, n, latest, time.Now()), nil
}

// restrictionAfter applies the highest step reached from the latest strike.
func restrictionAfter(ladder []strikeStep, n int, latest *time.Time, now time.Time) time.Time {
	if latest == nil {
		return time.Time{}
	}
	var d time.Duration
	for _, s := range ladder {
		if n >= s.Strikes {
			d = s.Restrict
		}
	}
	if until := latest.Add(d); d > 0 && until.After(now) {
		return until
	}
	return time.Time{}
}

// CheckPosting refuses new posts from someone the strike ladder restricts:
// a *httpx.Error 403 "posting_restricted" with details.until. Screen calls it
// for new items; call it yourself before other writes you want restricted.
func CheckPosting(ctx context.Context, q db.Querier, userID uuid.UUID) error {
	until, err := RestrictedUntil(ctx, q, userID)
	if err != nil || until.IsZero() {
		return err
	}
	e := httpx.NewError(http.StatusForbidden, "posting_restricted",
		"You can't post until "+until.UTC().Format("Jan 2, 15:04")+" UTC because of repeated removals. Settings → Safety shows why.")
	e.Details = map[string]any{"until": until.UTC()}
	return e
}

// addStrike records a strike in the staff transaction.
func addStrike(ctx context.Context, tx pgx.Tx, userID uuid.UUID, caseID uuid.UUID, action, category string, staffID uuid.UUID) error {
	_, err := tx.Exec(ctx, `INSERT INTO moderation_strikes (user_id, case_id, action, category, created_by)
		VALUES ($1, $2, $3, $4, $5)`, userID, caseID, action, category, staffID)
	return err
}

// ---- the person's own standing: GET /v1/moderation/standing ----

// Strike is one strike as its owner sees it.
type Strike struct {
	ID        uuid.UUID `json:"id"`
	Action    string    `json:"action"`   // remove | ban
	Category  string    `json:"category"` // spam, scam, harassment, … or "" when staff gave none
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// StrikeStep is one step of the ladder.
type StrikeStep struct {
	Strikes int `json:"strikes"`
	Hours   int `json:"hours"`
}

// Standing is GET /v1/moderation/standing.
type Standing struct {
	Strikes         []Strike     `json:"strikes"` // counting now, newest first
	RestrictedUntil *time.Time   `json:"restricted_until"`
	Ladder          []StrikeStep `json:"ladder"`
	WindowDays      int          `json:"window_days"`
}

func loadStanding(ctx context.Context, q db.Querier, userID uuid.UUID) (Standing, error) {
	out := Standing{Strikes: []Strike{}, Ladder: []StrikeStep{}, WindowDays: int(StrikeWindow.Hours() / 24)}
	for _, s := range currentScreening().ladder {
		out.Ladder = append(out.Ladder, StrikeStep{Strikes: s.Strikes, Hours: int(s.Restrict.Hours())})
	}
	rows, err := q.Query(ctx, `SELECT id, action, COALESCE(category, ''), created_at FROM moderation_strikes
		WHERE user_id = $1 AND revoked_at IS NULL AND created_at > $2 ORDER BY created_at DESC, id DESC LIMIT 100`,
		userID, time.Now().Add(-StrikeWindow))
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var s Strike
		if err := rows.Scan(&s.ID, &s.Action, &s.Category, &s.CreatedAt); err != nil {
			return out, err
		}
		s.ExpiresAt = s.CreatedAt.Add(StrikeWindow)
		out.Strikes = append(out.Strikes, s)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	until, err := RestrictedUntil(ctx, q, userID)
	if err != nil {
		return out, err
	}
	if !until.IsZero() {
		out.RestrictedUntil = &until
	}
	return out, nil
}

func (s *Service) standing(w http.ResponseWriter, r *http.Request) error {
	out, err := loadStanding(r.Context(), s.d.Pool, auth.MustFromContext(r.Context()).UserID)
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}
