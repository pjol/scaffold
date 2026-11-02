package calendar

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// Booking links can add each booking to one of the host's connected
// calendars (BookingLink.AddTo). The app's own event stays the record (the
// booker's invitation, reminders, the manage link); the platform copy is
// written with the host's connection, carries a native meeting link (Google
// Meet, Teams) when the link asks for one, follows reschedules and is
// deleted on cancel (through the outbox). Connected views leave the copy out
// (mirrorKeys), so the host sees the booking once. The copy has no guests:
// the app emails the booker, and Outlook can't add guests without emailing
// them.

// bookingCopy is a link's connected calendar, opened for one booking.
type bookingCopy struct {
	sess   *calSession
	calID  string
	native bool // the meeting link comes from the copy
}

// prepareBookingCopy opens the link's connected calendar. When it can't be
// opened, the booking goes on in the app's calendar only; a native meeting
// then falls back to the app's own rooms (l is changed for this booking),
// or fails when there are none.
func (s *Service) prepareBookingCopy(ctx context.Context, l *BookingLink) (bookingCopy, error) {
	if l.AddTo == nil {
		return bookingCopy{}, nil
	}
	org := uuid.Nil
	if l.OrgID != nil {
		org = *l.OrgID
	}
	opts, sess, err := s.conferenceOptionsFor(ctx, l.ownerID, org, l.AddTo.ConnectionID, l.AddTo.CalendarID)
	if err != nil {
		slog.WarnContext(ctx, "calendar: booking copy unavailable", "err", err)
		needsNative := l.LocationType == LocationVideo && (l.ConferenceProvider == ProviderGoogleMeet || l.ConferenceProvider == ProviderTeams)
		if needsNative {
			if _, ok := seamOption(ctx, l.ownerID, org, l.ConferenceProvider, ptrStr(l.ConferenceConnectionID)); ok {
				return bookingCopy{}, nil // the wired conferencing makes this provider too
			}
			if !VideoAvailable() {
				return bookingCopy{}, httpx.NewError(http.StatusServiceUnavailable, "video_unavailable", "The host's video calls aren't available right now. Try again later.")
			}
			l.ConferenceProvider, l.ConferenceConnectionID = ProviderApp, nil
		}
		return bookingCopy{}, nil
	}
	c := bookingCopy{sess: sess, calID: l.AddTo.CalendarID}
	if l.LocationType == LocationVideo {
		for _, o := range opts {
			if o.ID == l.ConferenceProvider && o.Native {
				c.native = true
			}
		}
	}
	return c, nil
}

// writeBookingCopy adds the booking to the connected calendar and records it
// on b (and a native meeting's link). Without a native meeting a failure is
// logged and the booking goes on; with one, it's returned.
func (s *Service) writeBookingCopy(ctx context.Context, l BookingLink, b *bookingRow, c bookingCopy, title, notes string) error {
	w := extWrite{Title: &title, Description: &notes, TimeSet: true, Start: b.StartsAt, End: b.EndsAt, Timezone: l.Timezone}
	busy := "busy"
	w.ShowAs = &busy
	if place := bookingLocation(*b); place != "" {
		w.Location = &place
	}
	if c.native {
		w.Meeting = l.ConferenceProvider
	} else if b.ConferenceURL != nil {
		w.JoinURL, w.JoinLabel = *b.ConferenceURL, locationLabel(l)
	}
	e, err := c.sess.ad.create(ctx, c.calID, w)
	if err != nil {
		err = s.connError(c.sess.conn, c.sess.conn.writeCap, err)
		if c.native {
			return err
		}
		slog.WarnContext(ctx, "calendar: writing a booking copy failed", "err", err)
		return nil
	}
	if c.native {
		if e.ConferenceURL == "" {
			_ = c.sess.ad.remove(context.WithoutCancel(ctx), c.calID, e.ID, false)
			return httpx.NewError(http.StatusBadGateway, "video_failed", productLabel(c.sess.conn.Provider)+" didn't add a meeting link. Try again.")
		}
		u := e.ConferenceURL
		b.ConferenceURL, b.ConferenceID = &u, nil
	}
	connID, calID, eventID := c.sess.conn.ID, c.calID, e.ID
	b.ExtConnectionID, b.ExtCalendarID, b.ExtEventID = &connID, &calID, &eventID
	cache.stale(b.HostID, connID, calID)
	return nil
}

// dropBookingCopy deletes a copy written for a booking that then failed.
func (s *Service) dropBookingCopy(ctx context.Context, c bookingCopy, b bookingRow) {
	if c.sess == nil || b.ExtEventID == nil {
		return
	}
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := c.sess.ad.remove(cctx, c.calID, *b.ExtEventID, false); err != nil {
		slog.WarnContext(ctx, "calendar: removing a booking copy failed", "err", err)
	}
}

// enqueueCopyMove queues moving a booking's platform copy (in the change's
// transaction).
func enqueueCopyMove(ctx context.Context, q db.Querier, b bookingRow, start, end time.Time, tz string) error {
	if b.ExtEventID == nil || b.ExtConnectionID == nil || b.ExtCalendarID == nil {
		return nil
	}
	return enqueueConnectedOp(ctx, q, connectedOp{UserID: b.HostID, ConnectionID: *b.ExtConnectionID, CalendarID: *b.ExtCalendarID,
		EventID: *b.ExtEventID, Op: "move", StartsAt: start, EndsAt: end, Timezone: tz})
}
