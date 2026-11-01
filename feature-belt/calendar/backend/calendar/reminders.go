package calendar

import (
	"context"
	"log/slog"
	"math"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/notify"
)

// reminderGrace is how late a reminder may still go out (worker restarts,
// a slow tick). Older due times are skipped rather than sent stale.
const reminderGrace = 10 * time.Minute

// sendReminders delivers each event reminder once per person per
// occurrence: the organizer and platform guests who haven't declined. A
// reminder whose time had passed before the event was created isn't sent. Rows in calendar_reminders_sent (inserted before sending,
// ON CONFLICT DO NOTHING) make it at-most-once across instances.
func (s *Service) sendReminders(ctx context.Context) error {
	now := time.Now().UTC()
	rows, err := s.d.Pool.Query(ctx, `
		SELECT `+eventCols+` FROM calendar_events e JOIN users u ON u.id = e.owner_id
		WHERE e.recurring_event_id IS NULL AND e.status = 'confirmed' AND cardinality(e.reminders) > 0
		  AND e.starts_at <= $1::timestamptz + make_interval(mins => (SELECT max(r) FROM unnest(e.reminders) r))
		  AND ((e.rrule IS NULL AND e.starts_at > $1)
		       OR (e.rrule IS NOT NULL AND (e.series_until IS NULL OR e.series_until > $1)))
		LIMIT 2000`, now)
	if err != nil {
		return err
	}
	var bases []eventRow
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			rows.Close()
			return err
		}
		bases = append(bases, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	// Exceptions carry their own reminders and times; include edited
	// occurrences of series whose master has none.
	exRows, err := s.d.Pool.Query(ctx, `
		SELECT `+eventCols+` FROM calendar_events e JOIN users u ON u.id = e.owner_id
		WHERE e.recurring_event_id IS NOT NULL AND e.status = 'confirmed' AND cardinality(e.reminders) > 0
		  AND e.starts_at > $1
		  AND e.starts_at <= $1::timestamptz + make_interval(mins => (SELECT max(r) FROM unnest(e.reminders) r))`, now)
	if err != nil {
		return err
	}
	var edited []eventRow
	for exRows.Next() {
		e, err := scanEvent(exRows)
		if err != nil {
			exRows.Close()
			return err
		}
		edited = append(edited, e)
	}
	exRows.Close()

	type due struct {
		row      eventRow  // fields for the message
		seriesID uuid.UUID // event or master id (key + attendees)
		occ      time.Time // occurrence key
		offset   int
	}
	var list []due
	consider := func(row eventRow, seriesID uuid.UUID, occ time.Time) {
		for _, m := range row.reminders() {
			at := row.StartsAt.Add(-time.Duration(m) * time.Minute)
			if at.After(now) || at.Before(now.Add(-reminderGrace)) || at.Before(row.CreatedAt.Add(-time.Minute)) {
				continue
			}
			if !row.StartsAt.After(now.Add(-time.Minute)) {
				continue
			}
			list = append(list, due{row: row, seriesID: seriesID, occ: occ, offset: m})
		}
	}
	for _, b := range bases {
		if b.RRule == nil {
			consider(b, b.ID, b.StartsAt)
			continue
		}
		rule, ok := b.rule()
		if !ok {
			continue
		}
		maxOff := 0
		for _, m := range b.reminders() {
			maxOff = max(maxOff, m)
		}
		exceptions, err := loadExceptions(ctx, s.d.Pool, b.ID)
		if err != nil {
			return err
		}
		skip := map[int64]bool{}
		for _, x := range exceptions {
			skip[x.OriginalStart.Unix()] = true
		}
		for _, st := range rule.Expand(b.StartsAt, b.loc(), now, now.Add(time.Duration(maxOff)*time.Minute+time.Minute), 50, skip) {
			inst := b
			inst.StartsAt, inst.EndsAt = st, st.Add(b.duration())
			consider(inst, b.ID, st)
		}
	}
	for _, x := range edited {
		consider(x, *x.RecurringEventID, *x.OriginalStart)
	}

	sent := 0
	for _, d := range list {
		recipients, err := s.reminderRecipients(ctx, d.seriesID, d.row.OwnerID)
		if err != nil {
			return err
		}
		rows, err := s.d.Pool.Query(ctx, `
			INSERT INTO calendar_reminders_sent (event_id, occurrence_start, user_id, offset_minutes)
			SELECT $1, $2, u, $3 FROM unnest($4::uuid[]) AS u
			ON CONFLICT DO NOTHING RETURNING user_id`, d.seriesID, d.occ, d.offset, recipients)
		if err != nil {
			return err
		}
		users, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		if err != nil {
			return err
		}
		for _, u := range users {
			title := d.row.Title + " starts in " + untilText(d.row.StartsAt.Sub(now))
			if d.row.AllDay {
				title = d.row.Title + " is coming up"
			}
			body := s.whenFor(ctx, s.d.Pool, u, d.row.StartsAt, d.row.EndsAt, d.row.AllDay, d.row.loc())
			if d.row.Location != "" {
				body += " · " + d.row.Location
			}
			if err := s.d.Notify.Send(ctx, notify.Notification{
				UserID: u, Kind: NotifyReminder, Title: title, Body: body, Link: eventLink(d.seriesID),
				Data: map[string]any{"event_id": d.seriesID, "occurrence_start": d.occ},
			}); err != nil {
				slog.WarnContext(ctx, "calendar: reminder notify failed", "err", err)
			}
			sent++
		}
	}
	if sent > 0 {
		slog.InfoContext(ctx, "calendar: reminders sent", "count", sent)
	}
	return nil
}

// reminderRecipients is the organizer plus platform guests who haven't
// declined, all with usable accounts.
func (s *Service) reminderRecipients(ctx context.Context, eventID, ownerID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.d.Pool.Query(ctx, `
		SELECT u.id FROM users u WHERE u.id = $2 AND u.status IN ('active', 'suspended')
		UNION
		SELECT a.user_id FROM calendar_attendees a JOIN users u ON u.id = a.user_id
		WHERE a.event_id = $1 AND a.response <> 'declined' AND u.status IN ('active', 'suspended')`, eventID, ownerID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

// untilText rounds how far off a start is: "10 minutes", "1 hour", "2 days".
func untilText(d time.Duration) string {
	plural := func(n int, unit string) string {
		if n == 1 {
			return "1 " + unit
		}
		return strconv.Itoa(n) + " " + unit + "s"
	}
	switch mins := int(math.Ceil(d.Minutes())); {
	case mins < 1:
		return "less than a minute"
	case mins < 60:
		return plural(mins, "minute")
	case mins < 48*60:
		return plural(int(d.Round(time.Hour)/time.Hour), "hour")
	}
	return plural(int(d.Round(24*time.Hour)/(24*time.Hour)), "day")
}

// prune drops bookkeeping nobody reads any more.
func (s *Service) prune(ctx context.Context) error {
	if _, err := s.d.Pool.Exec(ctx, `DELETE FROM calendar_reminders_sent WHERE created_at < now() - interval '3 days'`); err != nil {
		return err
	}
	_, err := s.d.Pool.Exec(ctx, `DELETE FROM calendar_busy_blocks WHERE upper(span) < now() - interval '30 days'`)
	return err
}
