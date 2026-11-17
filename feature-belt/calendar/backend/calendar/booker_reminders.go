package calendar

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/mail"
)

// sendBookerReminders emails bookers before their bookings, at each of the
// link's offsets, once per booking per offset. A booking made after an
// offset's moment skips it; a reminder more than 30 minutes late is
// dropped rather than sent stale.
func (s *Service) sendBookerReminders(ctx context.Context) error {
	rows, err := s.d.Pool.Query(ctx, `
		SELECT b.id, r.mins FROM calendar_bookings b
		JOIN calendar_booking_links l ON l.id = b.link_id
		CROSS JOIN LATERAL unnest(l.booker_reminders) AS r(mins)
		WHERE b.status = 'confirmed' AND b.starts_at > now()
		  AND b.starts_at - make_interval(mins => r.mins) <= now()
		  AND b.starts_at - make_interval(mins => r.mins) > now() - interval '30 minutes'
		  AND b.created_at < b.starts_at - make_interval(mins => r.mins)
		  AND NOT EXISTS (SELECT 1 FROM calendar_booking_reminders_sent x WHERE x.booking_id = b.id AND x.offset_minutes = r.mins)
		ORDER BY b.starts_at LIMIT 200`)
	if err != nil {
		return err
	}
	type due struct {
		id   uuid.UUID
		mins int
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (due, error) {
		var d due
		return d, row.Scan(&d.id, &d.mins)
	})
	if err != nil {
		return err
	}
	sent := 0
	for _, d := range list {
		err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
			tag, err := tx.Exec(ctx, `INSERT INTO calendar_booking_reminders_sent (booking_id, offset_minutes) VALUES ($1, $2) ON CONFLICT DO NOTHING`, d.id, d.mins)
			if err != nil || tag.RowsAffected() == 0 {
				return err
			}
			b, err := scanBooking(tx.QueryRow(ctx, `SELECT `+bookingCols+` FROM calendar_bookings b WHERE b.id = $1`, d.id))
			if err != nil {
				return err
			}
			l, host, err := s.bookingContext(ctx, tx, b)
			if err != nil {
				return err
			}
			hostContact, err := ownerContact(ctx, tx, b.HostID)
			if err != nil {
				return err
			}
			when := formatWhen(b.StartsAt, b.EndsAt, false, zoneOr(b.BookerTimezone, time.UTC))
			paras := []string{"Your booking with " + host.DisplayName + " starts in " + untilText(time.Until(b.StartsAt)) + ".", "When: " + when}
			if where := bookingLocation(b); where != "" {
				paras = append(paras, "Where: "+where)
			}
			var links []mailLink
			if b.ConferenceURL != nil {
				links = append(links, mailLink{"Join the video call", *b.ConferenceURL})
			}
			if token, stable := s.tokenFor("manage", b.ID); stable && l.ID != uuid.Nil {
				links = append(links, mailLink{"Reschedule or cancel", s.webURL("/book/manage/" + token)})
			}
			text, html := s.mailBody(paras, links)
			sent++
			return enqueueMail(ctx, tx, mail.Message{To: b.BookerEmail, Subject: "Reminder: " + b.Title + " with " + host.DisplayName + " @ " + when,
				Text: text, HTML: html, ReplyTo: hostContact.Email})
		})
		if err != nil {
			return err
		}
	}
	if sent > 0 {
		kickOutbox()
	}
	return nil
}
