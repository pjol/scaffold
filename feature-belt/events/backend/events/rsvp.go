package events

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/notify"
)

type rsvpReq struct {
	Status string `json:"status"` // going | maybe | declined
}

// lockedEvent is an event row locked FOR UPDATE for an RSVP change. The lock
// serialises every RSVP on the event, so capacity can't be overrun by two
// people taking the last spot at once.
type lockedEvent struct {
	host                      uuid.UUID
	title, status, visibility string
	endsAt                    time.Time
	capacity                  *int
}

func lockEvent(ctx context.Context, tx pgx.Tx, id uuid.UUID) (lockedEvent, error) {
	var e lockedEvent
	err := tx.QueryRow(ctx, `
		SELECT host_id, title, status, visibility, ends_at, capacity FROM events WHERE id = $1 FOR UPDATE`, id).
		Scan(&e.host, &e.title, &e.status, &e.visibility, &e.endsAt, &e.capacity)
	if db.IsNoRows(err) {
		return e, errNotFound
	}
	return e, err
}

func (e lockedEvent) open() error {
	if e.status == "cancelled" {
		return httpx.Conflict("This event was cancelled.")
	}
	if !e.endsAt.After(time.Now()) {
		return httpx.Conflict("This event has ended.")
	}
	return nil
}

// rsvpHandler sets the caller's RSVP. Going on a full event joins the
// waitlist (keeping an existing place in it); leaving a going spot promotes
// the first person waiting.
func (s *Service) rsvpHandler(w http.ResponseWriter, r *http.Request) error {
	caller := auth.MustFromContext(r.Context())
	ctx := r.Context()
	eid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req rsvpReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	switch req.Status {
	case RSVPGoing, RSVPMaybe, RSVPDeclined:
	default:
		return httpx.BadRequest("Status must be going, maybe or declined.")
	}
	var (
		ev       lockedEvent
		prev     string
		status   string
		promoted []uuid.UUID
	)
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var err error
		if ev, err = lockEvent(ctx, tx, eid); err != nil {
			return err
		}
		a, err := loadAccess(ctx, tx, eid, ev.host, caller.UserID)
		if err != nil {
			return err
		}
		if !a.canView(ev.visibility) {
			return errNotFound
		}
		if err := ev.open(); err != nil {
			return err
		}
		prev, status = a.rsvp, req.Status
		if status == RSVPGoing && prev != RSVPGoing && ev.capacity != nil {
			var going int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM event_rsvps WHERE event_id = $1 AND status = 'going'`, eid).Scan(&going); err != nil {
				return err
			}
			if going >= *ev.capacity {
				status = RSVPWaitlisted
			}
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO event_rsvps (event_id, user_id, status, waitlisted_at)
			VALUES ($1, $2, $3, CASE WHEN $3 = 'waitlisted' THEN now() END)
			ON CONFLICT (event_id, user_id) DO UPDATE SET
			  status = EXCLUDED.status,
			  waitlisted_at = CASE WHEN EXCLUDED.status <> 'waitlisted' THEN NULL
			                       WHEN event_rsvps.status = 'waitlisted' THEN event_rsvps.waitlisted_at
			                       ELSE now() END,
			  updated_at = now()`, eid, caller.UserID, status); err != nil {
			return err
		}
		if prev == RSVPGoing && status != RSVPGoing {
			promoted, err = promoteWaitlist(ctx, tx, eid)
		}
		return err
	})
	if err != nil {
		return err
	}
	s.afterRSVP(ctx, caller, eid, ev, prev, status, promoted)
	e, _, err := s.loadEvent(ctx, eid, caller.UserID)
	if err != nil {
		return err
	}
	httpx.OK(w, e)
	return nil
}

func (s *Service) unrsvpHandler(w http.ResponseWriter, r *http.Request) error {
	caller := auth.MustFromContext(r.Context())
	ctx := r.Context()
	eid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var (
		ev       lockedEvent
		prev     string
		promoted []uuid.UUID
	)
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var err error
		if ev, err = lockEvent(ctx, tx, eid); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `
			DELETE FROM event_rsvps WHERE event_id = $1 AND user_id = $2 RETURNING status`, eid, caller.UserID).Scan(&prev); err != nil {
			if db.IsNoRows(err) {
				return nil
			}
			return err
		}
		if prev == RSVPGoing && ev.open() == nil {
			promoted, err = promoteWaitlist(ctx, tx, eid)
		}
		return err
	})
	if err != nil {
		return err
	}
	if prev != "" {
		s.afterRSVP(ctx, caller, eid, ev, prev, "", promoted)
	}
	httpx.NoContent(w)
	return nil
}

func (s *Service) afterRSVP(ctx context.Context, caller *auth.Identity, eid uuid.UUID, ev lockedEvent, prev, status string, promoted []uuid.UUID) {
	s.publishCounts(ctx, eid)
	name := caller.DisplayName
	if name == "" {
		name = "Someone"
	}
	s.afterCommit(ctx, "events.notify_rsvp", func(ctx context.Context) {
		s.notifyPromoted(ctx, eid, ev.title, promoted)
		if status == RSVPGoing && prev != RSVPGoing {
			s.notifyAll(ctx, caller.UserID, []uuid.UUID{ev.host}, notify.Notification{
				Kind: NotifyRSVP, Title: name + " is going to " + ev.title, Body: "Open the event to see who's coming.",
				Link: eventLink(eid), Data: map[string]any{"event_id": eid},
			})
		}
	})
}

// promoteWaitlist moves people from the waitlist to going, earliest first,
// while there is room (all of them when there is no capacity). The event row
// must be locked by the caller's transaction. Returns the promoted users.
func promoteWaitlist(ctx context.Context, tx pgx.Tx, eid uuid.UUID) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `
		WITH ev AS (
		  SELECT capacity FROM events WHERE id = $1 AND status = 'scheduled' AND ends_at > now()
		), room AS (
		  SELECT CASE WHEN ev.capacity IS NULL THEN NULL
		              ELSE GREATEST(ev.capacity - (SELECT count(*) FROM event_rsvps
		                                           WHERE event_id = $1 AND status = 'going'), 0) END AS n
		  FROM ev
		), next AS (
		  SELECT r.user_id FROM event_rsvps r, room
		  WHERE r.event_id = $1 AND r.status = 'waitlisted'
		  ORDER BY r.waitlisted_at, r.user_id
		  LIMIT (SELECT n FROM room)
		)
		UPDATE event_rsvps SET status = 'going', waitlisted_at = NULL, updated_at = now()
		WHERE event_id = $1 AND user_id IN (SELECT user_id FROM next)
		RETURNING user_id`, eid)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

func (s *Service) notifyPromoted(ctx context.Context, eid uuid.UUID, title string, users []uuid.UUID) {
	s.notifyAll(ctx, uuid.Nil, users, notify.Notification{
		Kind: NotifyPromoted, Title: "You're going to " + title, Body: "A spot opened up and you moved off the waitlist.",
		Link: eventLink(eid), Data: map[string]any{"event_id": eid},
	})
}

// ---- attendees -------------------------------------------------------------

// Attendee is one RSVP in an attendee list.
type Attendee struct {
	User             Person    `json:"user"`
	Status           string    `json:"status"`
	WaitlistPosition *int      `json:"waitlist_position"`
	RespondedAt      time.Time `json:"responded_at"`
}

// attendeesHandler lists RSVPs by status (default going). Hosts see every
// status; attendees (going, maybe, waitlisted) see all but declined;
// everyone else gets 403.
func (s *Service) attendeesHandler(w http.ResponseWriter, r *http.Request) error {
	caller := auth.MustFromContext(r.Context())
	ctx := r.Context()
	eid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	status := r.URL.Query().Get("status")
	if status == "" {
		status = RSVPGoing
	}
	switch status {
	case RSVPGoing, RSVPMaybe, RSVPWaitlisted, RSVPDeclined:
	default:
		return httpx.BadRequest("Status must be going, maybe, waitlisted or declined.")
	}
	var host uuid.UUID
	var visibility string
	if err := s.d.Pool.QueryRow(ctx, `SELECT host_id, visibility FROM events WHERE id = $1`, eid).Scan(&host, &visibility); err != nil {
		if db.IsNoRows(err) {
			return errNotFound
		}
		return err
	}
	a, err := loadAccess(ctx, s.d.Pool, eid, host, caller.UserID)
	if err != nil {
		return err
	}
	switch {
	case !a.canView(visibility):
		return errNotFound
	case a.host():
	case a.attendee() && status != RSVPDeclined:
	default:
		return httpx.Forbidden("Only hosts and people going can see who's coming.")
	}
	p := httpx.ParsePage(r)
	var c cursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	// Waitlist runs in queue order; the rest in the order people answered.
	rows, err := s.d.Pool.Query(ctx, `
		SELECT id, display_name, avatar_url, status, pos, t FROM (
		  SELECT u.id, u.display_name, u.avatar_url, r.status,
		         CASE WHEN r.status = 'waitlisted' THEN row_number() OVER (ORDER BY r.waitlisted_at, r.user_id) END AS pos,
		         CASE WHEN r.status = 'waitlisted' THEN r.waitlisted_at ELSE r.created_at END AS t
		  FROM event_rsvps r JOIN users u ON u.id = r.user_id
		  WHERE r.event_id = $1 AND r.status = $2
		) x
		WHERE ($3::timestamptz IS NULL OR (x.t, x.id) > ($3, $4::uuid))
		ORDER BY x.t, x.id LIMIT $5`, eid, status, nullTime(c.T), c.ID, p.Limit+1)
	if err != nil {
		return err
	}
	list := []Attendee{}
	for rows.Next() {
		var at Attendee
		if err := rows.Scan(&at.User.ID, &at.User.DisplayName, &at.User.AvatarURL, &at.Status, &at.WaitlistPosition, &at.RespondedAt); err != nil {
			rows.Close()
			return err
		}
		list = append(list, at)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(list, p.Limit, func(a Attendee) any { return cursor{a.RespondedAt, a.User.ID} }))
	return nil
}
