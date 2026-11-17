package calendar

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
)

// DeleteAccount removes the user's calendar. Future bookings they host are
// cancelled and bookers emailed ("the host's account was closed"); future
// bookings they made are cancelled and the host told. Guests of their
// upcoming events get a cancellation (email with METHOD:CANCEL, or in-app).
// Then their events, booking links, bookings as host, subscriptions, feed,
// settings and invitations are deleted; bookings they made on others'
// links stay with the host, anonymised. Messages wait in the outbox and go
// out after the deletion commits; video rooms are cancelled the same way.
func (s *Service) DeleteAccount(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	rows, err := tx.Query(ctx, `SELECT id, host_id = $1 FROM calendar_bookings
		WHERE (host_id = $1 OR booker_user_id = $1) AND status = 'confirmed' AND ends_at > now()`, userID)
	if err != nil {
		return err
	}
	type pending struct {
		id     uuid.UUID
		asHost bool
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (pending, error) {
		var p pending
		return p, row.Scan(&p.id, &p.asHost)
	})
	if err != nil {
		return err
	}
	for _, p := range list {
		by, reason := "booker", "Their account was closed."
		if p.asHost {
			by, reason = "system", "The host's account was closed."
		}
		if _, err := s.cancelBookingTx(ctx, tx, p.id, by, reason); err != nil {
			return err
		}
	}

	// Tell guests of upcoming events, then delete every event the user owns.
	brows, err := tx.Query(ctx, `SELECT `+eventCols+` FROM calendar_events e JOIN users u ON u.id = e.owner_id
		WHERE e.owner_id = $1 AND e.recurring_event_id IS NULL AND e.status = 'confirmed'
		  AND ((e.rrule IS NULL AND e.ends_at > now()) OR (e.rrule IS NOT NULL AND (e.series_until IS NULL OR e.series_until > now())))`, userID)
	if err != nil {
		return err
	}
	var upcoming []eventRow
	for brows.Next() {
		e, err := scanEvent(brows)
		if err != nil {
			brows.Close()
			return err
		}
		upcoming = append(upcoming, e)
	}
	brows.Close()
	for _, e := range upcoming {
		atts, err := loadFullAttendees(ctx, tx, e.ID)
		if err != nil {
			return err
		}
		var exceptions []eventRow
		if e.RRule != nil {
			if exceptions, err = loadExceptions(ctx, tx, e.ID); err != nil {
				return err
			}
		}
		if err := s.notifyAttendees(ctx, tx, e, exceptions, atts, inviteCancelled); err != nil {
			return err
		}
	}
	crows, err := tx.Query(ctx, `DELETE FROM calendar_events WHERE owner_id = $1 RETURNING conference_id`, userID)
	if err != nil {
		return err
	}
	confs, err := pgx.CollectRows(crows, pgx.RowTo[*string])
	if err != nil {
		return err
	}
	for _, c := range confs {
		if err := enqueueConferenceCancel(ctx, tx, c); err != nil {
			return err
		}
	}

	// Calendars: personal ones go (their events went above); team calendars
	// stay with the team, ownerless (its workspace's admins manage them).
	prows, err := tx.Query(ctx, `SELECT id, team_id IS NOT NULL FROM calendar_calendars WHERE owner_id = $1`, userID)
	if err != nil {
		return err
	}
	type cal struct {
		id   uuid.UUID
		team bool
	}
	cals, err := pgx.CollectRows(prows, func(row pgx.CollectableRow) (cal, error) {
		var c cal
		return c, row.Scan(&c.id, &c.team)
	})
	if err != nil {
		return err
	}
	for _, c := range cals {
		ref := access.Ref{Kind: KindCalendar, ID: c.id}
		if c.team {
			if err := access.SetOwner(ctx, tx, ref, uuid.Nil); err != nil && !errors.Is(err, access.ErrNotRegistered) {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE calendar_calendars SET owner_id = NULL, updated_at = now() WHERE id = $1`, c.id); err != nil {
				return err
			}
			continue
		}
		if err := access.DeleteResource(ctx, tx, ref); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM calendar_calendars WHERE id = $1`, c.id); err != nil {
			return err
		}
	}
	// Team links they made pass to another active member of the team
	// (leads first); links nobody can take are deleted below.
	if _, err := tx.Exec(ctx, `
		UPDATE calendar_booking_links l SET updated_at = now(),
		  owner_id = (SELECT tm.user_id FROM access_team_members tm JOIN users u ON u.id = tm.user_id
		              WHERE tm.team_id = l.team_id AND tm.user_id <> $1 AND u.status = 'active'
		              ORDER BY (tm.role = 'lead') DESC, tm.created_at LIMIT 1)
		WHERE l.owner_id = $1 AND l.team_id IS NOT NULL
		  AND EXISTS (SELECT 1 FROM access_team_members tm JOIN users u ON u.id = tm.user_id
		              WHERE tm.team_id = l.team_id AND tm.user_id <> $1 AND u.status = 'active')`, userID); err != nil {
		return err
	}

	for _, q := range []string{
		`DELETE FROM calendar_link_hosts WHERE user_id = $1`,
		`DELETE FROM calendar_booking_hosts WHERE user_id = $1`,
		`DELETE FROM calendar_calendar_prefs WHERE user_id = $1`,
		`UPDATE calendar_rooms SET created_by = NULL WHERE created_by = $1`,
		`UPDATE calendar_event_rooms SET added_by = NULL WHERE added_by = $1`,
		`DELETE FROM calendar_bookings WHERE host_id = $1`,
		`UPDATE calendar_bookings SET booker_user_id = NULL, booker_name = 'Deleted user', booker_email = '',
		   answers = '[]', updated_at = now() WHERE booker_user_id = $1`,
		`DELETE FROM calendar_booking_links WHERE owner_id = $1`,
		`DELETE FROM calendar_attendees WHERE user_id = $1`,
		`DELETE FROM calendar_reminders_sent WHERE user_id = $1`,
		`DELETE FROM calendar_busy_blocks WHERE user_id = $1`,
		`DELETE FROM calendar_subscriptions WHERE user_id = $1`,
		`DELETE FROM calendar_feeds WHERE user_id = $1`,
		`DELETE FROM calendar_settings WHERE user_id = $1`,
		`DELETE FROM calendar_connected_calendars WHERE user_id = $1`,
		`DELETE FROM calendar_connected_conferences WHERE user_id = $1`,
	} {
		if _, err := tx.Exec(ctx, q, userID); err != nil {
			return err
		}
	}
	return nil
}

// AccountDeleted wakes the outbox so cancellations go out promptly and
// drops the person's cached connected calendars. Copies of their bookings
// on their own connected calendars stay there (their connection is gone).
func (s *Service) AccountDeleted(ctx context.Context, userID uuid.UUID) error {
	cache.forgetUser(userID)
	kickOutbox()
	return nil
}

// ExportAccount returns the user's calendar data (subscription addresses
// as their host only: the full address is a credential).
func (s *Service) ExportAccount(ctx context.Context, userID uuid.UUID) (any, error) {
	st, err := s.loadSettings(ctx, s.d.Pool, userID)
	if err != nil {
		return nil, err
	}
	type exportEvent struct {
		ID          uuid.UUID  `json:"id"`
		Title       string     `json:"title"`
		Description string     `json:"description"`
		Location    string     `json:"location"`
		StartsAt    time.Time  `json:"starts_at"`
		EndsAt      time.Time  `json:"ends_at"`
		Timezone    string     `json:"timezone"`
		AllDay      bool       `json:"all_day"`
		RRule       *string    `json:"rrule"`
		SeriesID    *uuid.UUID `json:"series_id"`
		Status      string     `json:"status"`
		Attendees   []Attendee `json:"attendees"`
		CreatedAt   time.Time  `json:"created_at"`
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT `+eventCols+` FROM calendar_events e JOIN users u ON u.id = e.owner_id
		WHERE e.owner_id = $1 ORDER BY e.starts_at LIMIT 10000`, userID)
	if err != nil {
		return nil, err
	}
	var owned []eventRow
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		owned = append(owned, e)
	}
	rows.Close()
	events := []exportEvent{}
	for _, e := range owned {
		atts := []Attendee{}
		if e.RecurringEventID == nil {
			if atts, err = listAttendees(ctx, s.d.Pool, e.ID); err != nil {
				return nil, err
			}
		}
		events = append(events, exportEvent{ID: e.ID, Title: e.Title, Description: e.Description, Location: e.Location,
			StartsAt: e.StartsAt, EndsAt: e.EndsAt, Timezone: e.Timezone, AllDay: e.AllDay, RRule: e.RRule,
			SeriesID: e.RecurringEventID, Status: e.Status, Attendees: atts, CreatedAt: e.CreatedAt})
	}

	type invitation struct {
		EventID   uuid.UUID `json:"event_id"`
		Title     string    `json:"title"`
		Organizer string    `json:"organizer"`
		StartsAt  time.Time `json:"starts_at"`
		Response  string    `json:"response"`
	}
	irows, err := s.d.Pool.Query(ctx, `SELECT e.id, e.title, u.display_name, e.starts_at, a.response
		FROM calendar_attendees a JOIN calendar_events e ON e.id = a.event_id JOIN users u ON u.id = e.owner_id
		WHERE a.user_id = $1 ORDER BY e.starts_at LIMIT 10000`, userID)
	if err != nil {
		return nil, err
	}
	invitations, err := pgx.CollectRows(irows, func(row pgx.CollectableRow) (invitation, error) {
		var i invitation
		return i, row.Scan(&i.EventID, &i.Title, &i.Organizer, &i.StartsAt, &i.Response)
	})
	if err != nil {
		return nil, err
	}

	lrows, err := s.d.Pool.Query(ctx, `SELECT `+linkCols+linkFrom+`WHERE l.owner_id = $1 ORDER BY l.created_at`, userID)
	if err != nil {
		return nil, err
	}
	links := []BookingLink{}
	for lrows.Next() {
		l, err := s.scanLink(lrows)
		if err != nil {
			lrows.Close()
			return nil, err
		}
		links = append(links, l)
	}
	lrows.Close()

	bookings := func(where string) ([]Booking, error) {
		rows, err := s.d.Pool.Query(ctx, `SELECT `+bookingCols+` FROM calendar_bookings b WHERE `+where+` ORDER BY b.starts_at LIMIT 10000`, userID)
		if err != nil {
			return nil, err
		}
		var list []bookingRow
		for rows.Next() {
			b, err := scanBooking(rows)
			if err != nil {
				rows.Close()
				return nil, err
			}
			list = append(list, b)
		}
		rows.Close()
		out := []Booking{}
		for _, b := range list {
			v, err := s.bookingView(ctx, b, userID)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	}
	hosted, err := bookings("b.host_id = $1")
	if err != nil {
		return nil, err
	}
	made, err := bookings("b.booker_user_id = $1")
	if err != nil {
		return nil, err
	}

	type exportSub struct {
		Name      string    `json:"name"`
		Host      string    `json:"host"`
		CreatedAt time.Time `json:"created_at"`
	}
	srows, err := s.d.Pool.Query(ctx, `SELECT name, url_host, created_at FROM calendar_subscriptions WHERE user_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	subs, err := pgx.CollectRows(srows, func(row pgx.CollectableRow) (exportSub, error) {
		var x exportSub
		return x, row.Scan(&x.Name, &x.Host, &x.CreatedAt)
	})
	if err != nil {
		return nil, err
	}
	var feedCreated *time.Time
	_ = s.d.Pool.QueryRow(ctx, `SELECT created_at FROM calendar_feeds WHERE user_id = $1`, userID).Scan(&feedCreated)

	// Choices per connected calendar and rooms made for connected events
	// (references only: the events live on the platform).
	type exportConnected struct {
		ConnectionID string  `json:"connection_id"`
		CalendarID   string  `json:"calendar_id"`
		Color        *string `json:"color"`
		Hidden       *bool   `json:"hidden"`
		Busy         *bool   `json:"busy"`
	}
	connected := []exportConnected{}
	crows, err := s.d.Pool.Query(ctx, `SELECT connection_id, calendar_id, color, hidden, busy FROM calendar_connected_calendars WHERE user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	for crows.Next() {
		var c exportConnected
		if err := crows.Scan(&c.ConnectionID, &c.CalendarID, &c.Color, &c.Hidden, &c.Busy); err != nil {
			crows.Close()
			return nil, err
		}
		connected = append(connected, c)
	}
	crows.Close()
	type exportRoom struct {
		ConnectionID string    `json:"connection_id"`
		CalendarID   string    `json:"calendar_id"`
		EventID      string    `json:"event_id"`
		Provider     string    `json:"provider"`
		CreatedAt    time.Time `json:"created_at"`
	}
	rooms := []exportRoom{}
	rrows, err := s.d.Pool.Query(ctx, `SELECT connection_id, calendar_id, event_id, provider, created_at FROM calendar_connected_conferences WHERE user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	for rrows.Next() {
		var r exportRoom
		if err := rrows.Scan(&r.ConnectionID, &r.CalendarID, &r.EventID, &r.Provider, &r.CreatedAt); err != nil {
			rrows.Close()
			return nil, err
		}
		rooms = append(rooms, r)
	}
	rrows.Close()

	type exportCalendar struct {
		ID          uuid.UUID `json:"id"`
		Name        string    `json:"name"`
		Description string    `json:"description"`
		IsPrimary   bool      `json:"is_primary"`
		CreatedAt   time.Time `json:"created_at"`
	}
	calRows, err := s.d.Pool.Query(ctx, `SELECT id, name, description, is_primary, created_at FROM calendar_calendars WHERE owner_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	calendars, err := pgx.CollectRows(calRows, func(row pgx.CollectableRow) (exportCalendar, error) {
		var c exportCalendar
		return c, row.Scan(&c.ID, &c.Name, &c.Description, &c.IsPrimary, &c.CreatedAt)
	})
	if err != nil {
		return nil, err
	}
	type exportPref struct {
		CalendarID uuid.UUID `json:"calendar_id"`
		Color      *string   `json:"color"`
		Hidden     bool      `json:"hidden"`
		Added      bool      `json:"added"`
	}
	prefRows, err := s.d.Pool.Query(ctx, `SELECT calendar_id, color, hidden, added FROM calendar_calendar_prefs WHERE user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	prefs, err := pgx.CollectRows(prefRows, func(row pgx.CollectableRow) (exportPref, error) {
		var p exportPref
		return p, row.Scan(&p.CalendarID, &p.Color, &p.Hidden, &p.Added)
	})
	if err != nil {
		return nil, err
	}
	type exportHost struct {
		LinkID   uuid.UUID `json:"link_id"`
		Title    string    `json:"title"`
		Priority int       `json:"priority"`
		Weight   int       `json:"weight"`
	}
	hostRows, err := s.d.Pool.Query(ctx, `SELECT h.link_id, l.title, h.priority, h.weight FROM calendar_link_hosts h
		JOIN calendar_booking_links l ON l.id = h.link_id WHERE h.user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	hosting, err := pgx.CollectRows(hostRows, func(row pgx.CollectableRow) (exportHost, error) {
		var h exportHost
		return h, row.Scan(&h.LinkID, &h.Title, &h.Priority, &h.Weight)
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"calendars":           calendars,
		"calendar_choices":    prefs,
		"team_links_hosted":   hosting,
		"settings":            st,
		"events":              events,
		"invitations":         invitations,
		"booking_links":       links,
		"bookings_as_host":    hosted,
		"bookings_as_booker":  made,
		"subscriptions":       subs,
		"feed_created_at":     feedCreated,
		"connected_calendars": connected,
		"connected_rooms":     rooms,
	}, nil
}
