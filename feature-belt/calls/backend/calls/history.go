package calls

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// History is per person: calls they were in (sessions with their
// attendance) and calls that rang them and they missed or declined.

const previewPeople = 6

// listHistory: GET /calls/history?cursor= → Page[HistoryItem], newest first.
func (s *Service) listHistory(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	p := httpx.ParsePage(r)
	var c cursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	items, err := s.history(ctx, s.d.Pool, me.UserID, c, p.Limit)
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(items, p.Limit, func(h HistoryItem) any { return cursor{T: h.StartedAt, ID: h.ID} }))
	return nil
}

// history loads limit+1 items before cursor c (zero = newest).
func (s *Service) history(ctx context.Context, q db.Querier, userID uuid.UUID, c cursor, limit int) ([]HistoryItem, error) {
	var after *time.Time
	if !c.T.IsZero() {
		after = &c.T
	}
	rows, err := q.Query(ctx, `
		WITH items AS (
		  SELECT 'call'::text AS type, ss.id, ss.started_at AS at, ss.ended_at, ss.meeting_id, NULL::uuid AS caller_id, NULL::text AS ring_status
		  FROM calls_sessions ss
		  WHERE ss.id IN (SELECT p.session_id FROM calls_participants p WHERE p.user_id = $1)
		  UNION ALL
		  SELECT 'missed', r.id, r.created_at, NULL, r.meeting_id, r.caller_id, r.status
		  FROM calls_rings r WHERE r.user_id = $1 AND r.status IN ('missed', 'declined')
		)
		SELECT i.type, i.id, i.at, i.ended_at, i.caller_id, i.ring_status, m.id, m.code, m.title, m.kind
		FROM items i JOIN calls_meetings m ON m.id = i.meeting_id
		WHERE $2::timestamptz IS NULL OR (i.at, i.id) < ($2, $3)
		ORDER BY i.at DESC, i.id DESC LIMIT $4`, userID, after, c.ID, limit+1)
	if err != nil {
		return nil, err
	}
	var items []HistoryItem
	var callers []uuid.UUID
	var sessions []uuid.UUID
	callerOf := map[int]uuid.UUID{}
	func() {
		defer rows.Close()
		for rows.Next() {
			var h HistoryItem
			var callerID *uuid.UUID
			if err = rows.Scan(&h.Type, &h.ID, &h.StartedAt, &h.EndedAt, &callerID, &h.RingStatus,
				&h.Meeting.ID, &h.Meeting.Code, &h.Meeting.Title, &h.Meeting.Kind); err != nil {
				return
			}
			if h.Type == "call" {
				sessions = append(sessions, h.ID)
				h.Live = h.EndedAt == nil
				h.DurationSeconds = duration(h.StartedAt, h.EndedAt)
			} else if callerID != nil {
				callers = append(callers, *callerID)
				callerOf[len(items)] = *callerID
			}
			h.Participants = []Person{}
			items = append(items, h)
		}
		err = rows.Err()
	}()
	if err != nil {
		return nil, err
	}
	people, counts, err := sessionPeople(ctx, q, sessions)
	if err != nil {
		return nil, err
	}
	users, err := lookupUsers(ctx, q, callers)
	if err != nil {
		return nil, err
	}
	for i := range items {
		if items[i].Type == "call" {
			items[i].Participants = people[items[i].ID]
			items[i].ParticipantCount = counts[items[i].ID]
			if items[i].Participants == nil {
				items[i].Participants = []Person{}
			}
			continue
		}
		if u, ok := users[callerOf[i]]; ok {
			items[i].Caller = &u
			items[i].Participants = []Person{{User: &u, Name: u.DisplayName}}
			items[i].ParticipantCount = 1
		}
	}
	return items, nil
}

func duration(start time.Time, end *time.Time) int {
	stop := time.Now()
	if end != nil {
		stop = *end
	}
	if d := stop.Sub(start); d > 0 {
		return int(d.Seconds())
	}
	return 0
}

// sessionPeople returns up to 6 distinct people per session (first to join
// first) and the distinct count.
func sessionPeople(ctx context.Context, q db.Querier, sessions []uuid.UUID) (map[uuid.UUID][]Person, map[uuid.UUID]int, error) {
	people, counts := map[uuid.UUID][]Person{}, map[uuid.UUID]int{}
	if len(sessions) == 0 {
		return people, counts, nil
	}
	rows, err := q.Query(ctx, `
		SELECT p.session_id, p.user_id, u.display_name, u.avatar_url, u.status, max(p.name)
		FROM calls_participants p LEFT JOIN users u ON u.id = p.user_id
		WHERE p.session_id = ANY($1)
		GROUP BY p.session_id, p.identity, p.user_id, u.display_name, u.avatar_url, u.status
		ORDER BY p.session_id, min(p.joined_at)`, sessions)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var sid uuid.UUID
		var uid *uuid.UUID
		var uname, ustatus, avatar *string
		var name string
		if err := rows.Scan(&sid, &uid, &uname, &avatar, &ustatus, &name); err != nil {
			return nil, nil, err
		}
		counts[sid]++
		if len(people[sid]) < previewPeople {
			people[sid] = append(people[sid], person(uid, uname, avatar, ustatus, name))
		}
	}
	return people, counts, rows.Err()
}

func person(uid *uuid.UUID, uname, avatar, ustatus *string, guestName string) Person {
	if uid == nil {
		if guestName == "" {
			guestName = "Guest"
		}
		return Person{Name: guestName, Guest: true}
	}
	ref := userRef(*uid, deref(uname), avatar, deref(ustatus))
	return Person{User: &ref, Name: ref.DisplayName}
}

// getHistory: GET /calls/history/{id} (a session) → CallDetail. Open to
// people who were in the call and to the meeting's hosts.
func (s *Service) getHistory(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return errCallHistoryGone
	}
	out, err := s.callDetail(ctx, id, me.UserID)
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

func (s *Service) callDetail(ctx context.Context, sessionID, userID uuid.UUID) (CallDetail, error) {
	var out CallDetail
	var meetingID uuid.UUID
	var attended bool
	err := s.d.Pool.QueryRow(ctx, `
		SELECT ss.id, ss.meeting_id, ss.started_at, ss.ended_at,
		       EXISTS (SELECT 1 FROM calls_participants p WHERE p.session_id = ss.id AND p.user_id = $2)
		FROM calls_sessions ss WHERE ss.id = $1`, sessionID, userID).Scan(&out.ID, &meetingID, &out.StartedAt, &out.EndedAt, &attended)
	if db.IsNoRows(err) {
		return out, errCallHistoryGone
	}
	if err != nil {
		return out, err
	}
	m, err := loadMeeting(ctx, s.d.Pool, meetingID)
	if err != nil {
		return out, err
	}
	a, err := s.accessFor(ctx, s.d.Pool, m, userID)
	if err != nil {
		return out, err
	}
	if !attended && !a.isHost() {
		return out, errCallHistoryGone
	}
	out.Meeting = MeetingRef{ID: m.ID, Code: m.Code, Title: m.Title, Kind: m.Kind}
	out.Host = userRef(m.HostID, m.HostName, m.HostAvatar, m.HostStatus)
	out.Live = out.EndedAt == nil
	out.DurationSeconds = duration(out.StartedAt, out.EndedAt)
	out.CanJoin = a.canJoin()
	rows, err := s.d.Pool.Query(ctx, `
		SELECT p.identity, p.user_id, u.display_name, u.avatar_url, u.status, max(p.name), min(p.joined_at),
		       CASE WHEN bool_or(p.left_at IS NULL) THEN NULL ELSE max(p.left_at) END,
		       sum(EXTRACT(EPOCH FROM (COALESCE(p.left_at, now()) - p.joined_at)))::int, count(*)::int
		FROM calls_participants p LEFT JOIN users u ON u.id = p.user_id
		WHERE p.session_id = $1
		GROUP BY p.identity, p.user_id, u.display_name, u.avatar_url, u.status
		ORDER BY min(p.joined_at), p.identity`, sessionID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	out.Participants = []Attendance{}
	for rows.Next() {
		var at Attendance
		var uid *uuid.UUID
		var uname, ustatus, avatar *string
		var name string
		if err := rows.Scan(&at.Identity, &uid, &uname, &avatar, &ustatus, &name, &at.FirstJoinedAt, &at.LastLeftAt, &at.Seconds, &at.Joins); err != nil {
			return out, err
		}
		at.Person = person(uid, uname, avatar, ustatus, name)
		out.Participants = append(out.Participants, at)
	}
	return out, rows.Err()
}

// listUpcoming: GET /calls/upcoming?cursor= → scheduled meetings you host or
// are invited to that haven't ended, soonest first.
func (s *Service) listUpcoming(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	p := httpx.ParsePage(r)
	var c cursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	list, err := s.upcoming(ctx, me.UserID, c, p.Limit)
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(list, p.Limit, func(m Meeting) any { return cursor{T: *m.StartsAt, ID: m.ID} }))
	return nil
}

func (s *Service) upcoming(ctx context.Context, userID uuid.UUID, c cursor, limit int) ([]Meeting, error) {
	var after *time.Time
	if !c.T.IsZero() {
		after = &c.T
	}
	rows, err := s.d.Pool.Query(ctx, meetingSelect+`
		WHERE m.canceled_at IS NULL AND m.starts_at IS NOT NULL
		  AND COALESCE(m.ends_at, m.starts_at + interval '1 hour') > now()
		  AND (m.host_id = $1 OR EXISTS (SELECT 1 FROM calls_invitees i WHERE i.meeting_id = m.id AND i.user_id = $1))
		  AND ($2::timestamptz IS NULL OR (m.starts_at, m.id) > ($2, $3))
		ORDER BY m.starts_at, m.id LIMIT $4`, userID, after, c.ID, limit+1)
	if err != nil {
		return nil, err
	}
	return s.meetings(rows)
}

// listLinks: GET /calls/links?cursor= → unscheduled meeting links you made,
// newest first.
func (s *Service) listLinks(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	p := httpx.ParsePage(r)
	var c cursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	var after *time.Time
	if !c.T.IsZero() {
		after = &c.T
	}
	rows, err := s.d.Pool.Query(ctx, meetingSelect+`
		WHERE m.host_id = $1 AND m.kind = 'meeting' AND m.canceled_at IS NULL AND m.starts_at IS NULL
		  AND ($2::timestamptz IS NULL OR (m.created_at, m.id) < ($2, $3))
		ORDER BY m.created_at DESC, m.id DESC LIMIT $4`, me.UserID, after, c.ID, p.Limit+1)
	if err != nil {
		return err
	}
	list, err := s.meetings(rows)
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(list, p.Limit, func(m Meeting) any { return cursor{T: m.CreatedAt, ID: m.ID} }))
	return nil
}

func (s *Service) meetings(rows pgx.Rows) ([]Meeting, error) {
	list, err := scanMeetings(rows)
	if err != nil {
		return nil, err
	}
	out := make([]Meeting, len(list))
	for i := range list {
		out[i] = s.toMeeting(&list[i])
	}
	return out, nil
}
