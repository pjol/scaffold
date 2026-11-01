package calls

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/notify"
	"scaffoldapp/backend/internal/orgs"
)

// Instant calls: a meeting of kind "call" (invited access) whose invitees'
// devices ring over realtime until they accept, decline, or the ring times
// out (CALLS_RING_TIMEOUT) and becomes a missed call with a notification.

type startCallReq struct {
	UserIDs []uuid.UUID `json:"user_ids"`
	OrgID   *uuid.UUID  `json:"org_id"`
	Title   string      `json:"title"`
}

// startCall: POST /calls/calls {user_ids, org_id?, title?} → MeetingDetail.
func (s *Service) startCall(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	var req startCallReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if err := s.requireMedia(); err != nil {
		return err
	}
	m, err := s.startCallFor(ctx, me.UserID, req.UserIDs, CallOptions{OrgID: req.OrgID, Title: req.Title})
	if err != nil {
		return err
	}
	row, err := loadMeeting(ctx, s.d.Pool, m.ID)
	if err != nil {
		return err
	}
	d, err := s.detail(ctx, s.d.Pool, row, RoleHost)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, d)
	return nil
}

func (s *Service) startCallFor(ctx context.Context, caller uuid.UUID, callees []uuid.UUID, o CallOptions) (Meeting, error) {
	callees = uniqueIDs(callees, caller)
	if len(callees) == 0 {
		return Meeting{}, httpx.BadRequest("Choose who to call.")
	}
	if len(callees) > maxRingTargets {
		return Meeting{}, httpx.BadRequest(fmt.Sprintf("Call at most %d people at once.", maxRingTargets))
	}
	if o.OrgID != nil {
		role, err := orgs.RoleOf(ctx, s.d.Pool, *o.OrgID, caller)
		if err != nil {
			return Meeting{}, err
		}
		if role == "" {
			return Meeting{}, httpx.NotFound(orgs.NounTitle() + " not found.")
		}
	}
	if err := s.checkInvitable(ctx, s.d.Pool, caller, callees); err != nil {
		return Meeting{}, err
	}
	var m Meeting
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var err error
		m, err = createMeeting(ctx, tx, MeetingInput{
			// Invited people only, no knocking: hosts add people by ringing them.
			HostID: caller, OrgID: o.OrgID, Title: o.Title, Access: AccessInvited, LobbyEnabled: ptr(false), InviteeIDs: callees, Source: o.Source, kind: KindCall,
		})
		if err != nil {
			return err
		}
		return s.insertRings(ctx, tx, m.ID, caller, callees)
	})
	if err != nil {
		return Meeting{}, err
	}
	s.publishRings(ctx, m.ID)
	return m, nil
}

func (s *Service) insertRings(ctx context.Context, q db.Querier, meetingID, caller uuid.UUID, users []uuid.UUID) error {
	_, err := q.Exec(ctx, `
		INSERT INTO calls_rings (meeting_id, caller_id, user_id, expires_at)
		SELECT $1, $2, u, now() + make_interval(secs => $4) FROM unnest($3::uuid[]) AS u
		ON CONFLICT (meeting_id, user_id) WHERE status = 'ringing' DO NOTHING`,
		meetingID, caller, users, s.cfg.ringTimeout.Seconds())
	return err
}

const ringSelect = `
	SELECT r.id, r.meeting_id, m.code, m.title, r.caller_id, cu.display_name, cu.avatar_url, cu.status,
	       r.user_id, uu.display_name, uu.avatar_url, uu.status, r.status, r.expires_at, r.created_at
	FROM calls_rings r
	JOIN calls_meetings m ON m.id = r.meeting_id
	JOIN users cu ON cu.id = r.caller_id
	JOIN users uu ON uu.id = r.user_id`

func loadRings(ctx context.Context, q db.Querier, where string, args ...any) ([]Ring, error) {
	rows, err := q.Query(ctx, ringSelect+" "+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Ring{}
	meetings := map[uuid.UUID]bool{}
	for rows.Next() {
		var r Ring
		var callerID, userID uuid.UUID
		var cName, cStatus, uName, uStatus string
		var cAvatar, uAvatar *string
		if err := rows.Scan(&r.ID, &r.MeetingID, &r.Code, &r.Title, &callerID, &cName, &cAvatar, &cStatus,
			&userID, &uName, &uAvatar, &uStatus, &r.Status, &r.ExpiresAt, &r.CreatedAt); err != nil {
			return nil, err
		}
		r.Caller = userRef(callerID, cName, cAvatar, cStatus)
		r.Callee = userRef(userID, uName, uAvatar, uStatus)
		meetings[r.MeetingID] = true
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Others: everyone else invited to the same call.
	ids := make([]uuid.UUID, 0, len(meetings))
	for id := range meetings {
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err = q.Query(ctx, `
		SELECT i.meeting_id, u.id, u.display_name, u.avatar_url, u.status
		FROM calls_invitees i JOIN users u ON u.id = i.user_id
		WHERE i.meeting_id = ANY($1) ORDER BY u.display_name, u.id`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	invited := map[uuid.UUID][]UserRef{}
	for rows.Next() {
		var mid, uid uuid.UUID
		var name, status string
		var avatar *string
		if err := rows.Scan(&mid, &uid, &name, &avatar, &status); err != nil {
			return nil, err
		}
		invited[mid] = append(invited[mid], userRef(uid, name, avatar, status))
	}
	for i := range out {
		others := []UserRef{}
		for _, u := range invited[out[i].MeetingID] {
			if u.ID != out[i].Callee.ID {
				others = append(others, u)
			}
		}
		out[i].Others = others
	}
	return out, rows.Err()
}

// publishRings rings everyone with a ringing row on a meeting and tells the
// meeting topic (the caller's screen shows who is being rung).
func (s *Service) publishRings(ctx context.Context, meetingID uuid.UUID) {
	rings, err := loadRings(ctx, s.d.Pool, `WHERE r.meeting_id = $1 AND r.status = 'ringing'`, meetingID)
	if err != nil {
		slog.WarnContext(ctx, "calls: loading rings failed", "err", err)
		return
	}
	for _, r := range rings {
		s.publishUser(ctx, r.Callee.ID, EventRing, r)
		s.publish(ctx, meetingTopic(meetingID), EventRingUpdated, r)
	}
}

// ringsEnded tells callees' devices and the meeting topic that rings ended;
// missed rings also notify.
func (s *Service) ringsEnded(ctx context.Context, ids []uuid.UUID) {
	if len(ids) == 0 {
		return
	}
	rings, err := loadRings(ctx, s.d.Pool, `WHERE r.id = ANY($1)`, ids)
	if err != nil {
		slog.WarnContext(ctx, "calls: loading rings failed", "err", err)
		return
	}
	for _, r := range rings {
		s.publishUser(ctx, r.Callee.ID, EventRingEnded, RingEnded{RingID: r.ID, MeetingID: r.MeetingID, Status: r.Status})
		s.publish(ctx, meetingTopic(r.MeetingID), EventRingUpdated, r)
		if r.Status != "missed" {
			continue
		}
		body := r.Caller.DisplayName + " called you."
		if n := len(r.Others); n > 0 {
			body = fmt.Sprintf("%s called you and %d other%s.", r.Caller.DisplayName, n, map[bool]string{true: "", false: "s"}[n == 1])
		}
		s.send(ctx, notify.Notification{UserID: r.Callee.ID, Kind: KindNotifyMissed, Title: "Missed call", Body: body,
			Link: "/calls", Data: map[string]any{"meeting_id": r.MeetingID, "code": r.Code}})
	}
}

// answered marks userID's rings for a meeting accepted (they joined).
func (s *Service) answered(ctx context.Context, meetingID, userID uuid.UUID) {
	rows, err := s.d.Pool.Query(ctx, `UPDATE calls_rings SET status = 'accepted', answered_at = now(), updated_at = now()
		WHERE meeting_id = $1 AND user_id = $2 AND status = 'ringing' RETURNING id`, meetingID, userID)
	if err != nil {
		slog.WarnContext(ctx, "calls: marking ring answered failed", "err", err)
		return
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err == nil {
		s.ringsEnded(ctx, ids)
	}
}

// missRings ends every ring still ringing on a meeting as missed.
func (s *Service) missRings(ctx context.Context, meetingID uuid.UUID) error {
	rows, err := s.d.Pool.Query(ctx, `UPDATE calls_rings SET status = 'missed', updated_at = now()
		WHERE meeting_id = $1 AND status = 'ringing' RETURNING id`, meetingID)
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return err
	}
	s.ringsEnded(ctx, ids)
	return nil
}

// expireRings (job, every 5 s) turns timed-out rings into missed calls.
func (s *Service) expireRings(ctx context.Context) error {
	rows, err := s.d.Pool.Query(ctx, `UPDATE calls_rings SET status = 'missed', updated_at = now()
		WHERE status = 'ringing' AND expires_at < now() RETURNING id`)
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return err
	}
	s.ringsEnded(ctx, ids)
	return nil
}

// ---- routes --------------------------------------------------------------------------

// listRings: GET /calls/rings → calls ringing for you now (the incoming-call
// screen restores from this after a reconnect).
func (s *Service) listRings(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	list, err := loadRings(r.Context(), s.d.Pool, `WHERE r.user_id = $1 AND r.status = 'ringing' AND r.expires_at > now()
		ORDER BY r.created_at DESC LIMIT 10`, me.UserID)
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.Page[Ring]{Items: list})
	return nil
}

// listMeetingRings: GET /calls/meetings/{id}/rings → the last 30 minutes of
// rings on a meeting, oldest first, for people in it ("Ringing Jordan…").
func (s *Service) listMeetingRings(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	m, err := s.meetingParam(r)
	if err != nil {
		return err
	}
	a, err := s.accessFor(ctx, s.d.Pool, m, me.UserID)
	if err != nil {
		return err
	}
	if !a.canJoin() {
		return errMeetingNotFound
	}
	list, err := loadRings(ctx, s.d.Pool, `WHERE r.meeting_id = $1 AND r.created_at > now() - interval '30 minutes' ORDER BY r.created_at, r.id LIMIT 100`, m.ID)
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.Page[Ring]{Items: list})
	return nil
}

// myRing updates {id} (the caller's own ringing ring) to status.
func (s *Service) myRing(w http.ResponseWriter, r *http.Request, status string) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return errRingNotFound
	}
	var meetingID uuid.UUID
	err = s.d.Pool.QueryRow(ctx, `UPDATE calls_rings SET status = $3, answered_at = now(), updated_at = now()
		WHERE id = $1 AND user_id = $2 AND status = 'ringing' RETURNING meeting_id`, id, me.UserID, status).Scan(&meetingID)
	if db.IsNoRows(err) {
		return errRingNotFound
	}
	if err != nil {
		return err
	}
	s.ringsEnded(ctx, []uuid.UUID{id})
	list, err := loadRings(ctx, s.d.Pool, `WHERE r.id = $1`, id)
	if err != nil || len(list) == 0 {
		return err
	}
	httpx.OK(w, list[0])
	return nil
}

// declineRing: POST /calls/rings/{id}/decline.
func (s *Service) declineRing(w http.ResponseWriter, r *http.Request) error {
	return s.myRing(w, r, "declined")
}

// acceptRing: POST /calls/rings/{id}/accept (other devices stop ringing;
// joining also accepts).
func (s *Service) acceptRing(w http.ResponseWriter, r *http.Request) error {
	return s.myRing(w, r, "accepted")
}

// ringAgain: POST /calls/meetings/{id}/ring {user_ids?}. Anyone in the
// call may ring invitees again; hosts may ring (and so invite) new people.
// Without user_ids, rings every invitee who isn't in the call.
func (s *Service) ringAgain(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	if err := s.requireMedia(); err != nil {
		return err
	}
	m, err := s.meetingParam(r)
	if err != nil {
		return err
	}
	a, err := s.accessFor(ctx, s.d.Pool, m, me.UserID)
	if err != nil {
		return err
	}
	if !a.canJoin() {
		return errMeetingNotFound
	}
	var req struct {
		UserIDs []uuid.UUID `json:"user_ids"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	targets := uniqueIDs(req.UserIDs, me.UserID)
	if len(targets) > maxRingTargets {
		return httpx.BadRequest(fmt.Sprintf("Ring at most %d people at once.", maxRingTargets))
	}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if len(targets) == 0 {
			rows, err := tx.Query(ctx, `
				SELECT i.user_id FROM calls_invitees i
				WHERE i.meeting_id = $1 AND i.user_id <> $2
				  AND NOT EXISTS (SELECT 1 FROM calls_participants p JOIN calls_sessions ss ON ss.id = p.session_id
				                  WHERE ss.meeting_id = $1 AND ss.ended_at IS NULL AND p.user_id = i.user_id AND p.left_at IS NULL)
				LIMIT $3`, m.ID, me.UserID, maxRingTargets)
			if err != nil {
				return err
			}
			targets, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
			if err != nil {
				return err
			}
			if len(targets) == 0 {
				return httpx.BadRequest("Everyone invited is already here.")
			}
		}
		rows, err := tx.Query(ctx, `SELECT user_id FROM calls_invitees WHERE meeting_id = $1 AND user_id = ANY($2)`, m.ID, targets)
		if err != nil {
			return err
		}
		known, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		if err != nil {
			return err
		}
		isKnown := map[uuid.UUID]bool{m.HostID: true}
		for _, id := range known {
			isKnown[id] = true
		}
		var fresh []uuid.UUID
		for _, id := range targets {
			if !isKnown[id] {
				fresh = append(fresh, id)
			}
		}
		if len(fresh) > 0 {
			if !a.isHost() {
				return errNotHost
			}
			if err := s.checkInvitable(ctx, tx, me.UserID, fresh); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO calls_invitees (meeting_id, user_id, role, invited_by)
				SELECT $1, u, 'invitee', $3 FROM unnest($2::uuid[]) AS u ON CONFLICT DO NOTHING`, m.ID, fresh, me.UserID); err != nil {
				return err
			}
		}
		return s.insertRings(ctx, tx, m.ID, me.UserID, targets)
	})
	if err != nil {
		return err
	}
	s.publishRings(ctx, m.ID)
	httpx.NoContent(w)
	return nil
}

// stopRinging: DELETE /calls/meetings/{id}/ring (hosts, or whoever rang):
// rings still ringing become missed calls.
func (s *Service) stopRinging(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	m, err := s.meetingParam(r)
	if err != nil {
		return err
	}
	a, err := s.accessFor(ctx, s.d.Pool, m, me.UserID)
	if err != nil {
		return err
	}
	if !a.canJoin() {
		return errMeetingNotFound
	}
	rows, err := s.d.Pool.Query(ctx, `UPDATE calls_rings SET status = 'missed', updated_at = now()
		WHERE meeting_id = $1 AND status = 'ringing' AND ($3 OR caller_id = $2) RETURNING id`, m.ID, me.UserID, a.isHost())
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return err
	}
	s.ringsEnded(ctx, ids)
	httpx.NoContent(w)
	return nil
}
