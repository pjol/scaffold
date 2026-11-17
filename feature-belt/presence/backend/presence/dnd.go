package presence

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// maxDND is the longest Do Not Disturb anyone (or any feature) can set.
const maxDND = 31 * 24 * time.Hour

// SetDNDUntil turns Do Not Disturb on for userID until until, for features
// that start it on the person's behalf (calendar focus time, a meeting). It
// never shortens a longer Do Not Disturb already on (one the person set
// themselves keeps its end), and it announces the change exactly like
// PUT /presence/me/dnd. until must be in the future and within 31 days.
//
// q takes the write; pass the pool, or call it after your transaction
// commits, because the announcement goes out at once.
func SetDNDUntil(ctx context.Context, q db.Querier, userID uuid.UUID, until time.Time) error {
	now := time.Now()
	if !until.After(now) {
		return httpx.BadRequest("Pick a time in the future.")
	}
	if until.Sub(now) > maxDND {
		return httpx.BadRequest("Do Not Disturb can last up to 31 days.")
	}
	tag, err := q.Exec(ctx, `
		INSERT INTO presence_status (user_id, dnd_until) VALUES ($1, $2)
		ON CONFLICT (user_id) DO UPDATE SET dnd_until = EXCLUDED.dnd_until, updated_at = now()
		WHERE presence_status.dnd_until IS NULL OR presence_status.dnd_until < EXCLUDED.dnd_until`,
		userID, until.UTC())
	if err != nil || tag.RowsAffected() == 0 {
		return err // a longer Do Not Disturb is already on: nothing changed, nothing to announce
	}
	if s := svc.Load(); s != nil {
		s.announce(ctx, q, userID)
	}
	return nil
}

// announce publishes userID's presence as q sees it now: what every write
// of status, Do Not Disturb or away does after it lands.
func (s *Service) announce(ctx context.Context, q db.Querier, userID uuid.UUID) {
	ctx = context.WithoutCancel(ctx)
	rows, err := s.compute(ctx, q, []uuid.UUID{userID})
	if err != nil {
		slog.WarnContext(ctx, "presence: compute", "err", err)
		return
	}
	for _, r := range rows {
		s.publish(ctx, r.Presence)
	}
}

// ---- Do Not Disturb schedule ---------------------------------------------------

// loadSchedule reads userID's schedule; nil when they have none.
func loadSchedule(ctx context.Context, q db.Querier, userID uuid.UUID) (*Schedule, *time.Time, error) {
	var (
		sc   Schedule
		days []byte
		skip *time.Time
	)
	err := q.QueryRow(ctx, `SELECT enabled, time_zone, days, skip_until FROM presence_dnd_schedules WHERE user_id = $1`, userID).
		Scan(&sc.Enabled, &sc.TimeZone, &days, &skip)
	if db.IsNoRows(err) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if err := json.Unmarshal(days, &sc.Days); err != nil {
		return nil, nil, err
	}
	return &sc, skip, nil
}

// scheduleTick is a schedule's state now: the end of the quiet hours it is
// in (nil outside them or while skipped) and when it next changes.
func scheduleTick(q *quiet, skip *time.Time, now time.Time) (*time.Time, time.Time) {
	next := now.Add(24 * time.Hour)
	var end *time.Time
	if q != nil {
		if e, ok := scheduledUntil(q, skip, now); ok {
			end = &e
		}
		next = q.nextChange(now)
	}
	if skip != nil && skip.After(now) && skip.Before(next) {
		next = *skip
	}
	return end, next
}

// refreshSchedule records userID's schedule state after a change, so the
// sweeper doesn't announce what the change itself announces.
func refreshSchedule(ctx context.Context, q db.Querier, userID uuid.UUID) error {
	var (
		zone string
		days []byte
		skip *time.Time
	)
	err := q.QueryRow(ctx, `SELECT time_zone, days, skip_until FROM presence_dnd_schedules WHERE user_id = $1 AND enabled`, userID).
		Scan(&zone, &days, &skip)
	if db.IsNoRows(err) {
		return nil
	}
	if err != nil {
		return err
	}
	end, next := scheduleTick(parseStored(zone, days), skip, time.Now())
	_, err = q.Exec(ctx, `UPDATE presence_dnd_schedules SET announced_end = $2, next_check_at = $3 WHERE user_id = $1`, userID, end, next)
	return err
}

// sweepSchedules finds schedules whose quiet hours started or ended since
// they were last looked at and returns the people whose state changed.
func (s *Service) sweepSchedules(ctx context.Context) ([]uuid.UUID, error) {
	rows, err := s.d.Pool.Query(ctx, `
		SELECT user_id, time_zone, days, skip_until, announced_end FROM presence_dnd_schedules
		WHERE enabled AND next_check_at <= now() ORDER BY next_check_at LIMIT 1000`)
	if err != nil {
		return nil, err
	}
	type due struct {
		id              uuid.UUID
		zone            string
		days            []byte
		skip, announced *time.Time
	}
	var list []due
	for rows.Next() {
		var d due
		if err := rows.Scan(&d.id, &d.zone, &d.days, &d.skip, &d.announced); err != nil {
			rows.Close()
			return nil, err
		}
		list = append(list, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	now := time.Now()
	var changed []uuid.UUID
	for _, d := range list {
		end, next := scheduleTick(parseStored(d.zone, d.days), d.skip, now)
		if !sameTime(end, d.announced) {
			changed = append(changed, d.id)
		}
		if _, err := s.d.Pool.Exec(ctx, `
			UPDATE presence_dnd_schedules SET announced_end = $2, next_check_at = $3,
			  skip_until = CASE WHEN skip_until <= now() THEN NULL ELSE skip_until END
			WHERE user_id = $1`, d.id, end, next); err != nil {
			return nil, err
		}
	}
	return changed, nil
}

func sameTime(a, b *time.Time) bool {
	switch {
	case a == nil || b == nil:
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

// PUT /presence/me/schedule {enabled, time_zone, days}
func (s *Service) setSchedule(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	var req Schedule
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	q, err := req.parse()
	if err != nil {
		return err
	}
	if req.Enabled && q.empty() {
		return httpx.BadRequest("Choose quiet hours for at least one day.")
	}
	days, err := json.Marshal(req.Days)
	if err != nil {
		return err
	}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO presence_dnd_schedules (user_id, enabled, time_zone, days) VALUES ($1, $2, $3, $4)
			ON CONFLICT (user_id) DO UPDATE SET enabled = EXCLUDED.enabled, time_zone = EXCLUDED.time_zone,
			  days = EXCLUDED.days, skip_until = NULL, updated_at = now()`, me, req.Enabled, req.TimeZone, days); err != nil {
			return err
		}
		return refreshSchedule(ctx, tx, me)
	})
	if err != nil {
		return err
	}
	s.reconcile(ctx, []uuid.UUID{me}, true)
	return s.respondMine(w, r)
}

// DELETE /presence/me/schedule
func (s *Service) clearSchedule(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context()).UserID
	if _, err := s.d.Pool.Exec(r.Context(), `DELETE FROM presence_dnd_schedules WHERE user_id = $1`, me); err != nil {
		return err
	}
	s.reconcile(r.Context(), []uuid.UUID{me}, true)
	return s.respondMine(w, r)
}

func (q *quiet) empty() bool {
	for _, d := range q.days {
		if d != nil {
			return false
		}
	}
	return true
}
