package calendar

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

// proposeHandler lets a guest propose a new time for a one-off event (with
// an optional note); the organizer is told and may accept it, which moves
// the event for everyone. A guest has one proposal at a time; DELETE
// withdraws it.
func (s *Service) proposeHandler(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id := auth.MustFromContext(ctx)
	eid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var in struct {
		StartsAt time.Time `json:"starts_at"`
		EndsAt   time.Time `json:"ends_at"`
		Note     string    `json:"note"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if err := s.propose(ctx, id, eid, in.StartsAt, in.EndsAt, in.Note); err != nil {
		return err
	}
	return s.writeDetail(w, ctx, id.UserID, eid)
}

// propose records caller's proposed time on event eid and tells the organizer.
func (s *Service) propose(ctx context.Context, caller *auth.Identity, eid uuid.UUID, startsAt, endsAt time.Time, rawNote string) error {
	note, ok := cleanText(rawNote, 500)
	if !ok {
		return httpx.BadRequest("Notes are up to 500 characters.")
	}
	start, end := startsAt.UTC().Truncate(time.Minute), endsAt.UTC().Truncate(time.Minute)
	if !end.After(start) || end.Sub(start) > maxEventLength {
		return httpx.BadRequest("Propose a start before the end, up to 14 days long.")
	}
	if start.Before(time.Now().Add(-time.Minute)) {
		return httpx.BadRequest("Propose a time in the future.")
	}
	res, err := s.resolve(ctx, s.d.Pool, caller.UserID, eid, nil)
	if err != nil {
		return err
	}
	if res.acc.attendee == nil {
		return httpx.BadRequest("Only guests propose new times. Change the event instead.")
	}
	if res.base.RRule != nil {
		return httpx.BadRequest("New times can be proposed for one-off events. Reply with a note for a series.")
	}
	if res.base.AllDay {
		return httpx.BadRequest("All-day events can't take a proposed time.")
	}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE calendar_attendees SET proposed_start = $2, proposed_end = $3, proposed_at = now(), note = $4
			WHERE id = $1`, res.acc.attendee.ID, start, end, note); err != nil {
			return err
		}
		body := s.whenFor(ctx, tx, res.base.OwnerID, start, end, false, res.base.loc())
		if note != "" {
			body += " · “" + note + "”"
		}
		return enqueueNotify(ctx, tx, notify.Notification{
			UserID: res.base.OwnerID, Kind: NotifyResponse, Title: caller.DisplayName + " proposed a new time for " + res.base.Title,
			Body: body, Link: eventLink(res.base.ID), Data: map[string]any{"event_id": res.base.ID},
		})
	})
	if err != nil {
		return err
	}
	kickOutbox()
	s.changed(ctx, "events", caller.UserID, res.base.OwnerID)
	return nil
}

// withdrawProposalHandler takes back the caller's proposal.
func (s *Service) withdrawProposalHandler(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id := auth.MustFromContext(ctx)
	eid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	res, err := s.resolve(ctx, s.d.Pool, id.UserID, eid, nil)
	if err != nil {
		return err
	}
	if res.acc.attendee == nil {
		return httpx.BadRequest("Only guests propose new times.")
	}
	if _, err := s.d.Pool.Exec(ctx, `UPDATE calendar_attendees SET proposed_start = NULL, proposed_end = NULL, proposed_at = NULL WHERE id = $1`,
		res.acc.attendee.ID); err != nil {
		return err
	}
	s.changed(ctx, "events", id.UserID, res.base.OwnerID)
	return s.writeDetail(w, ctx, id.UserID, eid)
}

// answerProposalHandler: the organizer (or someone who may change the
// event) accepts a guest's proposal, moving the event and telling every
// guest, or declines it, telling that guest. Other proposals are cleared
// when one is accepted.
func (s *Service) answerProposalHandler(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id := auth.MustFromContext(ctx)
	eid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	attendeeID, err := httpx.URLParamUUID(r, "attendee")
	if err != nil {
		return err
	}
	var in struct {
		Accept bool `json:"accept"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	res, err := s.resolve(ctx, s.d.Pool, id.UserID, eid, nil)
	if err != nil {
		return err
	}
	if !res.acc.canEdit() {
		return errNotOrganizer
	}
	var start, end *time.Time
	var guest *uuid.UUID
	err = s.d.Pool.QueryRow(ctx, `SELECT proposed_start, proposed_end, user_id FROM calendar_attendees WHERE id = $1 AND event_id = $2`,
		attendeeID, res.base.ID).Scan(&start, &end, &guest)
	if db.IsNoRows(err) || (err == nil && start == nil) {
		return httpx.NotFound("That proposal was withdrawn or already answered.")
	}
	if err != nil {
		return err
	}
	if !in.Accept {
		err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `UPDATE calendar_attendees SET proposed_start = NULL, proposed_end = NULL, proposed_at = NULL WHERE id = $1`, attendeeID); err != nil {
				return err
			}
			if guest == nil {
				return nil
			}
			return enqueueNotify(ctx, tx, notify.Notification{
				UserID: *guest, Kind: NotifyUpdated, Title: "Your proposed time for " + res.base.Title + " wasn't taken",
				Body: s.whenFor(ctx, tx, *guest, res.base.StartsAt, res.base.EndsAt, false, res.base.loc()) + " stays",
				Link: eventLink(res.base.ID), Data: map[string]any{"event_id": res.base.ID},
			})
		})
		if err != nil {
			return err
		}
		kickOutbox()
		s.changed(ctx, "events", id.UserID, uuidOr(guest))
		return s.writeDetail(w, ctx, id.UserID, eid)
	}
	tz := res.base.Timezone
	allDay := false
	if _, err := s.updateEvent(ctx, id, res.base.ID, eventInput{StartsAt: start, EndsAt: end, Timezone: &tz, AllDay: &allDay}); err != nil {
		return err
	}
	if _, err := s.d.Pool.Exec(ctx, `UPDATE calendar_attendees SET proposed_start = NULL, proposed_end = NULL, proposed_at = NULL
		WHERE event_id = $1`, res.base.ID); err != nil {
		return err
	}
	return s.writeDetail(w, ctx, id.UserID, eid)
}

// writeDetail answers with an event's detail for viewer.
func (s *Service) writeDetail(w http.ResponseWriter, ctx context.Context, viewer, eid uuid.UUID) error {
	res, err := s.resolve(ctx, s.d.Pool, viewer, eid, nil)
	if err != nil {
		return err
	}
	d, err := s.detail(ctx, s.d.Pool, viewer, res)
	if err != nil {
		return err
	}
	httpx.OK(w, d)
	return nil
}
