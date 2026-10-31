package events

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/notify"
)

// sendReminders sends each going/maybe RSVP one reminder per offset. The
// smallest due offset wins: an RSVP made 30 minutes before the start gets no
// "starts in 24 hours" reminder, and a reminder is never sent for an offset
// whose window had already begun when the person RSVP'd. Rows in
// event_reminders_sent (inserted before sending, ON CONFLICT DO NOTHING)
// make it at-most-once across instances and restarts.
func (s *Service) sendReminders(ctx context.Context) error {
	for _, off := range s.offsets {
		mins := int(off / time.Minute)
		rows, err := s.d.Pool.Query(ctx, `
			WITH due AS (
			  SELECT r.event_id, r.user_id FROM event_rsvps r JOIN events e ON e.id = r.event_id
			  WHERE e.status = 'scheduled' AND e.starts_at > now()
			    AND e.starts_at <= now() + make_interval(mins => $1)
			    AND r.status IN ('going', 'maybe')
			    AND r.created_at < e.starts_at - make_interval(mins => $1)
			    AND NOT EXISTS (SELECT 1 FROM event_reminders_sent x
			                    WHERE x.event_id = r.event_id AND x.user_id = r.user_id AND x.offset_minutes <= $1)
			  ORDER BY e.starts_at LIMIT 500
			)
			INSERT INTO event_reminders_sent (event_id, user_id, offset_minutes)
			SELECT event_id, user_id, $1 FROM due
			ON CONFLICT DO NOTHING
			RETURNING event_id, user_id`, mins)
		if err != nil {
			return err
		}
		type due struct{ event, user uuid.UUID }
		list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (due, error) {
			var d due
			return d, row.Scan(&d.event, &d.user)
		})
		if err != nil {
			return err
		}
		byEvent := map[uuid.UUID][]uuid.UUID{}
		for _, d := range list {
			byEvent[d.event] = append(byEvent[d.event], d.user)
		}
		for eid, users := range byEvent {
			var title, zone, place string
			var starts time.Time
			if err := s.d.Pool.QueryRow(ctx, `SELECT title, timezone, location_text, starts_at FROM events WHERE id = $1`, eid).
				Scan(&title, &zone, &place, &starts); err != nil {
				return err
			}
			body := whenText(starts, zone)
			if place != "" {
				body += " · " + place
			}
			s.notifyAll(ctx, uuid.Nil, users, notify.Notification{
				Kind: NotifyReminder, Title: title + " starts in " + untilText(time.Until(starts)), Body: body,
				Link: eventLink(eid), Data: map[string]any{"event_id": eid},
			})
		}
		if len(list) > 0 {
			slog.InfoContext(ctx, "events: reminders sent", "offset", off.String(), "count", len(list))
		}
	}
	return nil
}

// untilText says how far off a start is, rounded: "50 minutes", "1 hour",
// "23 hours", "3 days".
func untilText(d time.Duration) string {
	plural := func(n int, unit string) string {
		if n == 1 {
			return "1 " + unit
		}
		return strconv.Itoa(n) + " " + unit + "s"
	}
	switch mins := int(d.Round(time.Minute) / time.Minute); {
	case mins < 1:
		return "1 minute"
	case mins < 60:
		return plural(mins, "minute")
	case mins < 48*60:
		return plural(int(d.Round(time.Hour)/time.Hour), "hour")
	}
	return plural(int(d.Round(24*time.Hour)/(24*time.Hour)), "day")
}

// enqueue stores notifications inside tx; drainOutbox sends them after the
// transaction commits. Used where the code runs inside someone else's
// transaction (account deletion) and can't know whether it will commit.
func enqueue(ctx context.Context, tx pgx.Tx, users []uuid.UUID, n notify.Notification) error {
	if len(users) == 0 {
		return nil
	}
	data, err := json.Marshal(n.Data)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO event_outbox (user_id, kind, title, body, link, data)
		SELECT u, $2, $3, $4, $5, $6 FROM unnest($1::uuid[]) AS u`, users, n.Kind, n.Title, n.Body, n.Link, data)
	return err
}

func (s *Service) drainOutbox(ctx context.Context) error {
	for {
		rows, err := s.d.Pool.Query(ctx, `
			DELETE FROM event_outbox WHERE id IN (
			  SELECT id FROM event_outbox ORDER BY id LIMIT 200 FOR UPDATE SKIP LOCKED)
			RETURNING user_id, kind, title, body, link, data`)
		if err != nil {
			return err
		}
		list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (notify.Notification, error) {
			var n notify.Notification
			var data []byte
			if err := row.Scan(&n.UserID, &n.Kind, &n.Title, &n.Body, &n.Link, &data); err != nil {
				return n, err
			}
			_ = json.Unmarshal(data, &n.Data)
			return n, nil
		})
		if err != nil {
			return err
		}
		for _, n := range list {
			if err := s.d.Notify.Send(ctx, n); err != nil {
				slog.WarnContext(ctx, "events: notify failed", "kind", n.Kind, "err", err)
			}
		}
		if len(list) < 200 {
			return nil
		}
	}
}
