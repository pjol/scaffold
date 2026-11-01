package calls

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/webhooks"
)

// webhook: POST /webhooks/livekit. Records sessions and attendance from
// the SFU: room_started, participant_joined, participant_left,
// room_finished. Unconfigured or unsigned requests are refused; each event
// is applied once (webhooks.Once).
func (s *Service) webhook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if s.media == nil {
		httpx.Fail(w, r, errUnconfigured)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		httpx.Fail(w, r, httpx.BadRequest("Couldn't read the body."))
		return
	}
	ev, err := s.media.ParseWebhook(r, body)
	if err != nil {
		if err == errWebhookAuth {
			httpx.Fail(w, r, httpx.NewError(http.StatusUnauthorized, "invalid_signature", "Invalid signature."))
			return
		}
		httpx.Fail(w, r, httpx.BadRequest("Invalid event."))
		return
	}
	meetingID, err := uuid.Parse(ev.Room)
	if err != nil || !recorded(ev.Kind) {
		httpx.NoContent(w) // a room or event this module doesn't track
		return
	}
	if ev.ID == "" {
		ev.ID = ev.Kind + ":" + ev.RoomSID + ":" + ev.ParticipantSID + ":" + ev.At.Format(time.RFC3339Nano)
	}
	var ended bool
	dup, err := webhooks.Once(ctx, s.d.Pool, "livekit", ev.ID, func(tx pgx.Tx) error {
		var err error
		ended, err = s.apply(ctx, tx, meetingID, ev)
		return err
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if !dup {
		s.afterEvent(ctx, meetingID, ev, ended)
	}
	httpx.NoContent(w)
}

func recorded(kind string) bool {
	switch kind {
	case EventRoomStarted, EventRoomFinished, EventParticipantJoined, EventParticipantLeft:
		return true
	}
	return false
}

// apply records one event inside the webhook transaction. ended reports a
// session closed by it.
func (s *Service) apply(ctx context.Context, tx pgx.Tx, meetingID uuid.UUID, ev MediaEvent) (ended bool, err error) {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM calls_meetings WHERE id = $1)`, meetingID).Scan(&exists); err != nil || !exists {
		return false, err
	}
	switch ev.Kind {
	case EventRoomStarted:
		_, err = sessionForRoom(ctx, tx, meetingID, ev.RoomSID, ev.At)
		return false, err
	case EventParticipantJoined:
		sessionID, err := sessionForRoom(ctx, tx, meetingID, ev.RoomSID, ev.At)
		if err != nil {
			return false, err
		}
		guest, id, ok := parseIdentity(ev.Identity)
		if !ok {
			return false, nil // not one of our identities (an agent, an egress)
		}
		var userID *uuid.UUID
		name := ""
		if guest {
			name = ev.Name
		} else {
			userID = &id
		}
		var sid *string
		if ev.ParticipantSID != "" {
			sid = &ev.ParticipantSID
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO calls_participants (session_id, identity, user_id, name, participant_sid, joined_at)
			VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT (participant_sid) WHERE participant_sid IS NOT NULL DO NOTHING`,
			sessionID, ev.Identity, userID, name, sid, ev.At)
		return false, err
	case EventParticipantLeft:
		tag, err := tx.Exec(ctx, `UPDATE calls_participants SET left_at = GREATEST($2, joined_at)
			WHERE participant_sid = $1 AND left_at IS NULL`, ev.ParticipantSID, ev.At)
		if err != nil || tag.RowsAffected() > 0 {
			return false, err
		}
		// The join event was missed: close their newest open row.
		_, err = tx.Exec(ctx, `
			UPDATE calls_participants SET left_at = GREATEST($3, joined_at) WHERE id = (
			  SELECT p.id FROM calls_participants p JOIN calls_sessions ss ON ss.id = p.session_id
			  WHERE ss.meeting_id = $1 AND p.identity = $2 AND p.left_at IS NULL ORDER BY p.joined_at DESC LIMIT 1)`,
			meetingID, ev.Identity, ev.At)
		return false, err
	case EventRoomFinished:
		var sessionID uuid.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM calls_sessions WHERE meeting_id = $1 AND ended_at IS NULL AND (room_sid = $2 OR room_sid IS NULL)
			ORDER BY started_at DESC LIMIT 1`, meetingID, ev.RoomSID).Scan(&sessionID)
		if db.IsNoRows(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		_, err = closeSessionTx(ctx, tx, sessionID, ev.At, "empty", nil)
		return err == nil, err
	}
	return false, nil
}

// sessionForRoom returns the session for a room instance, opening it if
// needed. A live session left from another room instance (a missed
// room_finished) is closed first.
func sessionForRoom(ctx context.Context, tx pgx.Tx, meetingID uuid.UUID, roomSID string, at time.Time) (uuid.UUID, error) {
	var id uuid.UUID
	if roomSID != "" {
		err := tx.QueryRow(ctx, `SELECT id FROM calls_sessions WHERE room_sid = $1`, roomSID).Scan(&id)
		if err == nil {
			return id, nil
		}
		if !db.IsNoRows(err) {
			return uuid.Nil, err
		}
		var stale uuid.UUID
		err = tx.QueryRow(ctx, `SELECT id FROM calls_sessions WHERE meeting_id = $1 AND ended_at IS NULL AND room_sid IS NOT NULL`, meetingID).Scan(&stale)
		if err == nil {
			if _, err := closeSessionTx(ctx, tx, stale, at, "reconciled", nil); err != nil {
				return uuid.Nil, err
			}
		} else if !db.IsNoRows(err) {
			return uuid.Nil, err
		}
		err = tx.QueryRow(ctx, `UPDATE calls_sessions SET room_sid = $2 WHERE meeting_id = $1 AND ended_at IS NULL AND room_sid IS NULL RETURNING id`,
			meetingID, roomSID).Scan(&id)
		if err == nil {
			return id, nil
		}
		if !db.IsNoRows(err) {
			return uuid.Nil, err
		}
	}
	var sid *string
	if roomSID != "" {
		sid = &roomSID
	}
	err := tx.QueryRow(ctx, `
		WITH ins AS (
		  INSERT INTO calls_sessions (meeting_id, room_sid, started_at) VALUES ($1, $2, $3)
		  ON CONFLICT (meeting_id) WHERE ended_at IS NULL DO NOTHING RETURNING id)
		SELECT id FROM ins UNION ALL SELECT id FROM calls_sessions WHERE meeting_id = $1 AND ended_at IS NULL LIMIT 1`,
		meetingID, sid, at).Scan(&id)
	return id, err
}

// closeSessionTx ends a session in q: open attendance closes at `at`,
// admissions made during it expire; a session nobody joined is deleted.
func closeSessionTx(ctx context.Context, q db.Querier, sessionID uuid.UUID, at time.Time, reason string, by *uuid.UUID) (uuid.UUID, error) {
	var meetingID uuid.UUID
	err := q.QueryRow(ctx, `UPDATE calls_sessions SET ended_at = GREATEST($2, started_at), end_reason = $3, ended_by = $4
		WHERE id = $1 AND ended_at IS NULL RETURNING meeting_id`, sessionID, at, reason, by).Scan(&meetingID)
	if db.IsNoRows(err) {
		return uuid.Nil, nil
	}
	if err != nil {
		return uuid.Nil, err
	}
	if _, err := q.Exec(ctx, `UPDATE calls_participants SET left_at = GREATEST($2, joined_at) WHERE session_id = $1 AND left_at IS NULL`, sessionID, at); err != nil {
		return uuid.Nil, err
	}
	if _, err := q.Exec(ctx, `DELETE FROM calls_sessions s WHERE s.id = $1 AND NOT EXISTS (SELECT 1 FROM calls_participants p WHERE p.session_id = s.id)`, sessionID); err != nil {
		return uuid.Nil, err
	}
	// Admissions for this session end with it (not ones a newer session made).
	_, err = q.Exec(ctx, `UPDATE calls_lobby SET status = 'expired', updated_at = now()
		WHERE meeting_id = $1 AND ((status = 'waiting' AND created_at <= $2) OR (status = 'admitted' AND decided_at <= $2))`, meetingID, at)
	return meetingID, err
}

// afterEvent runs side effects once an event is committed.
func (s *Service) afterEvent(ctx context.Context, meetingID uuid.UUID, ev MediaEvent, ended bool) {
	switch ev.Kind {
	case EventRoomStarted:
		s.publish(ctx, meetingTopic(meetingID), EventMeetingUpdated, map[string]any{"id": meetingID, "live": true})
	case EventRoomFinished:
		if ended {
			if err := s.missRings(ctx, meetingID); err != nil {
				slog.WarnContext(ctx, "calls: ending rings failed", "err", err)
			}
		}
		s.publish(ctx, meetingTopic(meetingID), EventMeetingUpdated, map[string]any{"id": meetingID, "live": false})
	case EventParticipantJoined:
		if guest, id, ok := parseIdentity(ev.Identity); ok && !guest {
			s.answered(ctx, meetingID, id)
		}
	}
}

// reconcile (job, every minute): closes sessions the SFU no longer runs
// (missed webhooks), ends rooms of canceled meetings and expires old
// admissions (sweepLobby handles waiting knocks).
func (s *Service) reconcile(ctx context.Context) error {
	if _, err := s.d.Pool.Exec(ctx, `UPDATE calls_lobby SET status = 'expired', updated_at = now()
		WHERE status = 'admitted' AND decided_at < now() - make_interval(secs => $1)`, admittedTTL.Seconds()); err != nil {
		return err
	}
	if s.media == nil {
		return nil
	}
	type open struct {
		SessionID uuid.UUID
		MeetingID uuid.UUID
		Canceled  bool
		LastSeen  time.Time
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT ss.id, ss.meeting_id, m.canceled_at IS NOT NULL,
		       GREATEST(ss.started_at, COALESCE((SELECT max(GREATEST(p.joined_at, COALESCE(p.left_at, p.joined_at)))
		                                         FROM calls_participants p WHERE p.session_id = ss.id), ss.started_at))
		FROM calls_sessions ss JOIN calls_meetings m ON m.id = ss.meeting_id
		WHERE ss.ended_at IS NULL AND (ss.started_at < now() - make_interval(secs => $1) OR m.canceled_at IS NOT NULL)
		LIMIT 500`, reconcileAfter.Seconds())
	if err != nil {
		return err
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[open])
	if err != nil || len(list) == 0 {
		return err
	}
	names := make([]string, len(list))
	for i, o := range list {
		names[i] = o.MeetingID.String()
	}
	live, err := s.media.LiveRooms(ctx, names)
	if err != nil {
		return err
	}
	for _, o := range list {
		switch {
		case o.Canceled:
			if live[o.MeetingID.String()] {
				if err := s.media.EndRoom(ctx, o.MeetingID.String()); err != nil {
					slog.WarnContext(ctx, "calls: ending a canceled meeting's room failed", "meeting_id", o.MeetingID, "err", err)
					continue
				}
			}
			if err := s.closeSession(ctx, o.SessionID, nowUTC(), "canceled", nil); err != nil {
				return err
			}
		case !live[o.MeetingID.String()]:
			if err := s.closeSession(ctx, o.SessionID, o.LastSeen, "reconciled", nil); err != nil {
				return err
			}
			s.publish(ctx, meetingTopic(o.MeetingID), EventMeetingUpdated, map[string]any{"id": o.MeetingID, "live": false})
		}
	}
	return nil
}
