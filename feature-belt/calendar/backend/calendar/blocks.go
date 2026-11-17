package calendar

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/bg"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/notify"
)

// Event types.
const (
	TypeDefault     = "default"
	TypeOutOfOffice = "out_of_office"
	TypeFocus       = "focus"
)

// Auto-decline modes for out of office and focus time.
const (
	DeclineNone = "none" // nothing is declined
	DeclineNew  = "new"  // invitations that arrive while it's on the calendar
	DeclineAll  = "all"  // also invitations already accepted or pending
)

const maxDeclineMessage = 500

// FocusHook learns that focus time started for userID and lasts until
// until: the app's stitching turns on Do Not Disturb (e.g. over the
// presence feature) for that long. It runs in the background, once per
// occurrence, within a minute of the start.
type FocusHook func(ctx context.Context, userID uuid.UUID, until time.Time)

var focusHook atomic.Pointer[FocusHook]

// SetFocusHook wires focus time to the stitching (e.g. presence's Do Not
// Disturb). Without it focus time still declines invitations.
func SetFocusHook(fn FocusHook) {
	if fn == nil {
		focusHook.Store(nil)
		return
	}
	focusHook.Store(&fn)
}

// FocusUntil reports whether userID is in focus time now and until when
// (pull alternative to the hook, e.g. for a status line).
func FocusUntil(ctx context.Context, q db.Querier, userID uuid.UUID) (time.Time, bool) {
	s := calendarService.Load()
	if s == nil {
		return time.Time{}, false
	}
	now := time.Now().UTC()
	occ, err := s.blocksFor(ctx, q, userID, now, now.Add(time.Second))
	if err != nil {
		return time.Time{}, false
	}
	for _, b := range occ {
		if b.row.EventType == TypeFocus {
			return b.end, true
		}
	}
	return time.Time{}, false
}

// block is one occurrence of someone's out-of-office or focus event.
type block struct {
	row        eventRow
	start, end time.Time
}

// blocksFor expands userID's out-of-office and focus events overlapping
// [from, to).
func (s *Service) blocksFor(ctx context.Context, q db.Querier, userID uuid.UUID, from, to time.Time) ([]block, error) {
	rows, err := q.Query(ctx, `
		SELECT `+eventCols+` FROM calendar_events e JOIN users u ON u.id = e.owner_id
		WHERE e.owner_id = $1 AND e.event_type <> 'default' AND e.recurring_event_id IS NULL AND e.status = 'confirmed'
		  AND e.starts_at < $3
		  AND ((e.rrule IS NULL AND e.ends_at > $2) OR (e.rrule IS NOT NULL AND (e.series_until IS NULL OR e.series_until > $2)))
		LIMIT 500`, userID, from, to)
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (viewerRow, error) {
		var v viewerRow
		err := row.Scan(v.dest()...)
		v.isOwner = true
		return v, err
	})
	if err != nil || len(list) == 0 {
		return nil, err
	}
	byID := map[uuid.UUID]eventRow{}
	for _, v := range list {
		byID[v.ID] = v.eventRow
	}
	occ, err := s.expand(ctx, q, list, from, to, 500)
	if err != nil {
		return nil, err
	}
	out := make([]block, 0, len(occ))
	for _, o := range occ {
		out = append(out, block{row: byID[o.EventID], start: o.StartsAt, end: o.EndsAt})
	}
	return out, nil
}

// declineReason is the out-of-office or focus block that declines an
// invitation from start to end for userID, if any.
func (s *Service) declineReason(ctx context.Context, q db.Querier, userID uuid.UUID, start, end time.Time) (*block, error) {
	blocks, err := s.blocksFor(ctx, q, userID, start, end)
	if err != nil {
		return nil, err
	}
	return decliningBlock(blocks, start, end), nil
}

// decliningBlock is the first block that declines an invitation from start
// to end: it overlaps and its auto-decline is on.
func decliningBlock(blocks []block, start, end time.Time) *block {
	for i := range blocks {
		if blocks[i].row.AutoDecline != DeclineNone && blocks[i].row.AutoDecline != "" && blocks[i].start.Before(end) && blocks[i].end.After(start) {
			return &blocks[i]
		}
	}
	return nil
}

// declineText is what the organizer reads: the person's message, else why.
func declineText(b block) string {
	if b.row.DeclineMessage != "" {
		return b.row.DeclineMessage
	}
	if b.row.EventType == TypeFocus {
		return "Declined automatically: in focus time."
	}
	return "Declined automatically: out of office."
}

// autoDeclineNew declines freshly added invitations (one-off events) of
// people who are out of office or in focus time then, with their message,
// and tells the organizer. Series invitations are left for the person
// (replies are series-wide).
func (s *Service) autoDeclineNew(ctx context.Context, q db.Querier, eventID uuid.UUID, added []fullAttendee) error {
	var ownerID uuid.UUID
	var start, end time.Time
	var title, tz string
	var allDay, recurring bool
	if err := q.QueryRow(ctx, `SELECT owner_id, starts_at, ends_at, title, timezone, all_day, rrule IS NOT NULL FROM calendar_events WHERE id = $1`, eventID).
		Scan(&ownerID, &start, &end, &title, &tz, &allDay, &recurring); err != nil {
		return err
	}
	if recurring {
		return nil
	}
	for i, a := range added {
		if a.UserID == nil || *a.UserID == ownerID || a.Response != ResponseNeedsAction {
			continue
		}
		b, err := s.declineReason(ctx, q, *a.UserID, start, end)
		if err != nil {
			return err
		}
		if b == nil {
			continue
		}
		note := declineText(*b)
		if _, err := q.Exec(ctx, `UPDATE calendar_attendees SET response = 'declined', responded_at = now(), note = $2, auto_declined = true
			WHERE id = $1`, a.ID, note); err != nil {
			return err
		}
		added[i].Response = ResponseDeclined
		if err := enqueueNotify(ctx, q, notify.Notification{
			UserID: ownerID, Kind: NotifyResponse, Title: a.Name + " declined " + title, Body: note,
			Link: eventLink(eventID), Data: map[string]any{"event_id": eventID},
		}); err != nil {
			return err
		}
	}
	return nil
}

// declineExisting declines userID's invitations to one-off events during
// block (auto_decline = all), each with the block's message, and tells
// their organizers. It returns the organizers to refresh.
func (s *Service) declineExisting(ctx context.Context, userID uuid.UUID, b block) ([]uuid.UUID, error) {
	var organizers []uuid.UUID
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT a.id, e.id, e.owner_id, e.title, e.starts_at, e.ends_at, e.all_day, e.timezone
			FROM calendar_attendees a JOIN calendar_events e ON e.id = a.event_id
			WHERE a.user_id = $1 AND a.response <> 'declined' AND e.owner_id <> $1 AND e.status = 'confirmed'
			  AND e.rrule IS NULL AND e.recurring_event_id IS NULL AND e.starts_at < $3 AND e.ends_at > $2
			LIMIT 200 FOR UPDATE OF a`, userID, b.start, b.end)
		if err != nil {
			return err
		}
		type hit struct {
			attendee, event, owner uuid.UUID
			title, tz              string
			start, end             time.Time
			allDay                 bool
		}
		var hits []hit
		for rows.Next() {
			var h hit
			if err := rows.Scan(&h.attendee, &h.event, &h.owner, &h.title, &h.start, &h.end, &h.allDay, &h.tz); err != nil {
				rows.Close()
				return err
			}
			hits = append(hits, h)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		var name string
		_ = tx.QueryRow(ctx, `SELECT display_name FROM users WHERE id = $1`, userID).Scan(&name)
		note := declineText(b)
		for _, h := range hits {
			if _, err := tx.Exec(ctx, `UPDATE calendar_attendees SET response = 'declined', responded_at = now(), note = $2, auto_declined = true
				WHERE id = $1`, h.attendee, note); err != nil {
				return err
			}
			organizers = append(organizers, h.owner)
			if err := enqueueNotify(ctx, tx, notify.Notification{
				UserID: h.owner, Kind: NotifyResponse, Title: name + " declined " + h.title,
				Body: s.whenFor(ctx, tx, h.owner, h.start, h.end, h.allDay, zoneOr(h.tz, time.UTC)) + " · " + note,
				Link: eventLink(h.event), Data: map[string]any{"event_id": h.event},
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil {
		kickOutbox()
	}
	return organizers, err
}

// afterBlockSaved applies an out-of-office or focus event that declines
// existing invitations: the next occurrences within 90 days.
func (s *Service) afterBlockSaved(ctx context.Context, userID, eventID uuid.UUID) {
	e, err := loadEventRow(ctx, s.d.Pool, eventID)
	if err != nil || e.EventType == TypeDefault || e.AutoDecline != DeclineAll {
		return
	}
	bg.Go(ctx, "calendar.decline_existing", func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		now := time.Now().UTC()
		blocks, err := s.blocksFor(ctx, s.d.Pool, userID, now, now.Add(90*24*time.Hour))
		if err != nil {
			return err
		}
		var touched []uuid.UUID
		for _, b := range blocks {
			if b.row.ID != eventID {
				continue
			}
			orgs, err := s.declineExisting(ctx, userID, b)
			if err != nil {
				return err
			}
			touched = append(touched, orgs...)
		}
		s.changed(ctx, "events", append(touched, userID)...)
		return nil
	})
}

// signalFocus hands focus time that just started to the focus hook, once
// per occurrence (the job runs every minute).
func (s *Service) signalFocus(ctx context.Context) error {
	hook := focusHook.Load()
	if hook == nil {
		return nil
	}
	now := time.Now().UTC()
	from, to := now.Add(-2*time.Minute), now.Add(time.Minute)
	rows, err := s.d.Pool.Query(ctx, `
		SELECT `+eventCols+` FROM calendar_events e JOIN users u ON u.id = e.owner_id
		WHERE e.event_type = 'focus' AND e.recurring_event_id IS NULL AND e.status = 'confirmed' AND u.status = 'active'
		  AND e.starts_at < $2
		  AND ((e.rrule IS NULL AND e.ends_at > $1) OR (e.rrule IS NOT NULL AND (e.series_until IS NULL OR e.series_until > $1)))
		LIMIT 5000`, from, to)
	if err != nil {
		return err
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (viewerRow, error) {
		var v viewerRow
		err := row.Scan(v.dest()...)
		v.isOwner = true
		return v, err
	})
	if err != nil || len(list) == 0 {
		return err
	}
	occ, err := s.expand(ctx, s.d.Pool, list, from, to, 5000)
	if err != nil {
		return err
	}
	owners := map[uuid.UUID]uuid.UUID{}
	for _, v := range list {
		owners[v.ID] = v.OwnerID
	}
	for _, o := range occ {
		if o.StartsAt.Before(from) || !o.StartsAt.Before(to) {
			continue
		}
		start := o.StartsAt
		if o.OccurrenceStart != nil {
			start = *o.OccurrenceStart
		}
		tag, err := s.d.Pool.Exec(ctx, `INSERT INTO calendar_focus_signals (event_id, occurrence_start) VALUES ($1, $2) ON CONFLICT DO NOTHING`, o.EventID, start)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			continue
		}
		user, until := owners[o.EventID], o.EndsAt
		bg.Go(ctx, "calendar.focus_hook", func(ctx context.Context) error {
			ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			(*hook)(ctx, user, until)
			return nil
		})
	}
	if _, err := s.d.Pool.Exec(ctx, `DELETE FROM calendar_focus_signals WHERE created_at < now() - interval '2 days'`); err != nil {
		slog.WarnContext(ctx, "calendar: focus prune failed", "err", err)
	}
	return nil
}
