package calls

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// Breakout rooms: a host splits the call into up to 50 rooms, each its own
// SFU room named by the room's id. People are assigned automatically
// (evenly, at random), by hand, or choose a room themselves; a timer and a
// host's "Close rooms" start a countdown, after which the rooms end and
// everyone returns to the main room. Hosts can visit any room. Time spent
// in a room counts toward the same session's attendance.

const (
	maxBreakoutRooms = 50
	breakoutGrace    = 30 * time.Second // countdown before rooms close
	maxBreakoutMins  = 240
)

// BreakoutPerson is someone assigned to (or present in) a room.
type BreakoutPerson struct {
	Identity string     `json:"identity"`
	Name     string     `json:"name"`
	UserID   *uuid.UUID `json:"user_id"`
	Present  bool       `json:"present"` // in that room now
}

// BreakoutRoom is one room with its people (hosts' view).
type BreakoutRoom struct {
	BreakoutRef
	Assigned []BreakoutPerson `json:"assigned"`
	Present  []BreakoutPerson `json:"present"` // in the room now (assigned or not)
}

// Breakout is the open set of rooms, for hosts.
type Breakout struct {
	ID          uuid.UUID        `json:"id"`
	Status      string           `json:"status"` // open | closing
	AllowChoose bool             `json:"allow_choose"`
	EndsAt      *time.Time       `json:"ends_at"`
	ClosingAt   *time.Time       `json:"closing_at"`
	Rooms       []BreakoutRoom   `json:"rooms"`
	MainRoom    []BreakoutPerson `json:"main_room"` // people in the main room now
}

// MyBreakout is the open set of rooms as one participant sees it.
type MyBreakout struct {
	ID          uuid.UUID     `json:"id"`
	Status      string        `json:"status"`
	AllowChoose bool          `json:"allow_choose"`
	EndsAt      *time.Time    `json:"ends_at"`
	ClosingAt   *time.Time    `json:"closing_at"`
	Room        *BreakoutRef  `json:"room"` // your room (nil: unassigned)
	Rooms       []BreakoutRef `json:"rooms"`
}

// assignEvenly spreads people over n rooms in a random order so room sizes
// differ by at most one. shuffle is rand.Shuffle (tests pass their own).
func assignEvenly(people []string, n int, shuffle func(int, func(i, j int))) map[string]int {
	out := map[string]int{}
	if n <= 0 {
		return out
	}
	order := slices.Clone(people)
	shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
	for i, p := range order {
		out[p] = i % n
	}
	return out
}

// openBreakout returns a session's open set of rooms (nil when none).
func openBreakout(ctx context.Context, q db.Querier, sessionID uuid.UUID) (*RoomBreakout, error) {
	var b RoomBreakout
	err := q.QueryRow(ctx, `SELECT id, status, ends_at, closing_at, allow_choose FROM calls_breakouts WHERE session_id = $1 AND status <> 'closed'`, sessionID).
		Scan(&b.ID, &b.Status, &b.EndsAt, &b.ClosingAt, &b.AllowChoose)
	if db.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `SELECT id, name FROM calls_breakout_rooms WHERE breakout_id = $1 ORDER BY position`, b.ID)
	if err != nil {
		return nil, err
	}
	b.Rooms, err = pgx.CollectRows(rows, pgx.RowToStructByPos[BreakoutRef])
	return &b, err
}

// myBreakout is the actor's view of the open rooms.
func (s *Service) myBreakout(ctx context.Context, a *actor) (*MyBreakout, error) {
	if a.m.LiveSession == nil {
		return nil, nil
	}
	b, err := openBreakout(ctx, s.d.Pool, *a.m.LiveSession)
	if err != nil || b == nil {
		return nil, err
	}
	out := &MyBreakout{ID: b.ID, Status: b.Status, AllowChoose: b.AllowChoose, EndsAt: b.EndsAt, ClosingAt: b.ClosingAt, Rooms: b.Rooms}
	var roomID uuid.UUID
	err = s.d.Pool.QueryRow(ctx, `SELECT room_id FROM calls_breakout_assignments WHERE breakout_id = $1 AND identity = $2`, b.ID, a.identity).Scan(&roomID)
	if err != nil && !db.IsNoRows(err) {
		return nil, err
	}
	for _, r := range b.Rooms {
		if r.ID == roomID {
			ref := r
			out.Room = &ref
		}
	}
	return out, nil
}

// hostBreakout is the hosts' view, with who is where right now.
func (s *Service) hostBreakout(ctx context.Context, m *meetingRow) (*Breakout, error) {
	if m.LiveSession == nil {
		return nil, nil
	}
	b, err := openBreakout(ctx, s.d.Pool, *m.LiveSession)
	if err != nil || b == nil {
		return nil, err
	}
	out := &Breakout{ID: b.ID, Status: b.Status, AllowChoose: b.AllowChoose, EndsAt: b.EndsAt, ClosingAt: b.ClosingAt, Rooms: []BreakoutRoom{}, MainRoom: []BreakoutPerson{}}
	rows, err := s.d.Pool.Query(ctx, `SELECT room_id, identity, name, user_id FROM calls_breakout_assignments WHERE breakout_id = $1 ORDER BY name, identity`, b.ID)
	if err != nil {
		return nil, err
	}
	assigned := map[uuid.UUID][]BreakoutPerson{}
	for rows.Next() {
		var room uuid.UUID
		var p BreakoutPerson
		if err := rows.Scan(&room, &p.Identity, &p.Name, &p.UserID); err != nil {
			rows.Close()
			return nil, err
		}
		assigned[room] = append(assigned[room], p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	dctx, cancel := detached(ctx)
	defer cancel()
	present := func(room string) []BreakoutPerson {
		list := []BreakoutPerson{}
		if s.media == nil {
			return list
		}
		people, err := s.media.ListParticipants(dctx, room)
		if err != nil {
			slog.WarnContext(ctx, "calls: list breakout participants", "room", room, "err", err)
			return list
		}
		for _, p := range people {
			guest, id, ok := parseIdentity(p.Identity)
			if !ok {
				continue
			}
			bp := BreakoutPerson{Identity: p.Identity, Name: p.Name, Present: true}
			if !guest {
				uid := id
				bp.UserID = &uid
			}
			list = append(list, bp)
		}
		return list
	}
	for _, r := range b.Rooms {
		here := present(r.ID.String())
		as := assigned[r.ID]
		for i := range as {
			as[i].Present = slices.ContainsFunc(here, func(p BreakoutPerson) bool { return p.Identity == as[i].Identity })
		}
		if as == nil {
			as = []BreakoutPerson{}
		}
		out.Rooms = append(out.Rooms, BreakoutRoom{BreakoutRef: r, Assigned: as, Present: here})
	}
	out.MainRoom = present(m.ID.String())
	return out, nil
}

// getBreakouts: GET /calls/meetings/{id}/breakouts (hosts) → Breakout or null.
func (s *Service) getBreakouts(w http.ResponseWriter, r *http.Request, a *actor) error {
	if err := a.requireHost(); err != nil {
		return err
	}
	b, err := s.hostBreakout(r.Context(), a.m)
	if err != nil {
		return err
	}
	httpx.OK(w, b)
	return nil
}

type breakoutReq struct {
	Rooms       []string          `json:"rooms"`       // room names; or
	Count       int               `json:"count"`       // N rooms named "Room 1…N"
	Assign      string            `json:"assign"`      // auto | manual | choose
	Assignments map[string]int    `json:"assignments"` // manual: identity → room index
	Minutes     int               `json:"minutes"`     // 0 = no timer
	Names       map[string]string `json:"names"`       // identity → name shown to hosts (optional)
}

// createBreakouts: POST /calls/meetings/{id}/breakouts (hosts, live).
func (s *Service) createBreakouts(w http.ResponseWriter, r *http.Request, a *actor) error {
	ctx := r.Context()
	if err := a.requireHost(); err != nil {
		return err
	}
	if err := a.requireLive(); err != nil {
		return err
	}
	if err := s.requireMedia(); err != nil {
		return err
	}
	if _, ok := s.media.(RoomController); !ok {
		return errNoRoomControl
	}
	var req breakoutReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	names := []string{}
	for _, n := range req.Rooms {
		if n = strings.Join(strings.Fields(n), " "); n != "" {
			if len([]rune(n)) > 60 {
				return httpx.BadRequest("Room names can be at most 60 characters.")
			}
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		for i := 1; i <= req.Count; i++ {
			names = append(names, fmt.Sprintf("Room %d", i))
		}
	}
	if len(names) < 1 || len(names) > maxBreakoutRooms {
		return httpx.BadRequest(fmt.Sprintf("Make 1 to %d rooms.", maxBreakoutRooms))
	}
	if req.Minutes < 0 || req.Minutes > maxBreakoutMins {
		return httpx.BadRequest("The timer can be at most 4 hours.")
	}
	if req.Assign == "" {
		req.Assign = "auto"
	}
	if req.Assign != "auto" && req.Assign != "manual" && req.Assign != "choose" {
		return httpx.BadRequest("assign must be auto, manual or choose.")
	}
	// Who is in the main room now (auto assigns them; manual checks against them).
	dctx, cancel := detached(ctx)
	defer cancel()
	people, err := s.media.ListParticipants(dctx, a.m.ID.String())
	if err != nil {
		return s.mediaErr(ctx, err)
	}
	hosts, err := hostIDs(ctx, s.d.Pool, a.m)
	if err != nil {
		return err
	}
	isHost := map[string]bool{}
	for _, h := range hosts {
		isHost[userIdentity(h)] = true
	}
	inRoom := map[string]string{}
	var everyone []string
	for _, p := range people {
		if _, _, ok := parseIdentity(p.Identity); !ok || isHost[p.Identity] {
			continue
		}
		inRoom[p.Identity] = p.Name
		everyone = append(everyone, p.Identity)
	}
	assign := map[string]int{}
	switch req.Assign {
	case "auto":
		assign = assignEvenly(everyone, len(names), rand.Shuffle)
	case "manual":
		for id, idx := range req.Assignments {
			if idx < 0 || idx >= len(names) {
				return httpx.BadRequest("Each person needs one of the rooms.")
			}
			if _, _, ok := parseIdentity(id); !ok {
				return httpx.BadRequest("Unknown participant.")
			}
			assign[id] = idx
		}
	}
	var until *time.Time
	if req.Minutes > 0 {
		t := time.Now().Add(time.Duration(req.Minutes) * time.Minute).UTC()
		until = &t
	}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var id uuid.UUID
		err := tx.QueryRow(ctx, `INSERT INTO calls_breakouts (meeting_id, session_id, allow_choose, ends_at, created_by) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
			a.m.ID, *a.m.LiveSession, req.Assign == "choose", until, a.userID).Scan(&id)
		if db.IsUniqueViolation(err, "calls_breakouts_live_idx") {
			return httpx.Conflict("Breakout rooms are already open. Close them first.")
		}
		if err != nil {
			return err
		}
		roomIDs := make([]uuid.UUID, len(names))
		for i, n := range names {
			if err := tx.QueryRow(ctx, `INSERT INTO calls_breakout_rooms (breakout_id, name, position) VALUES ($1, $2, $3) RETURNING id`, id, n, i).Scan(&roomIDs[i]); err != nil {
				return err
			}
		}
		for identity, idx := range assign {
			name := inRoom[identity]
			if name == "" {
				name = req.Names[identity]
			}
			if err := upsertAssignment(ctx, tx, id, identity, roomIDs[idx], name); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.syncRoom(ctx, a.m.ID)
	s.signalRoom(ctx, a.m, signal{Type: SignalAssignment})
	b, err := s.hostBreakout(ctx, a.m)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, b)
	return nil
}

func upsertAssignment(ctx context.Context, q db.Querier, breakoutID uuid.UUID, identity string, roomID uuid.UUID, name string) error {
	guest, id, ok := parseIdentity(identity)
	if !ok {
		return httpx.BadRequest("Unknown participant.")
	}
	var uid *uuid.UUID
	if !guest {
		uid = &id
		if name == "" {
			_ = q.QueryRow(ctx, `SELECT display_name FROM users WHERE id = $1`, id).Scan(&name)
		}
	} else if name == "" {
		_ = q.QueryRow(ctx, `SELECT COALESCE(guest_name, '') FROM calls_lobby WHERE id = $1`, id).Scan(&name)
	}
	_, err := q.Exec(ctx, `INSERT INTO calls_breakout_assignments (breakout_id, identity, room_id, user_id, name) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (breakout_id, identity) DO UPDATE SET room_id = $3, name = CASE WHEN $5 = '' THEN calls_breakout_assignments.name ELSE $5 END`,
		breakoutID, identity, roomID, uid, name)
	return err
}

// patchBreakouts: PATCH /calls/meetings/{id}/breakouts (hosts)
// {assignments: {identity: room id | null}, minutes?, allow_choose?}.
// minutes restarts the timer from now (0 removes it).
func (s *Service) patchBreakouts(w http.ResponseWriter, r *http.Request, a *actor) error {
	ctx := r.Context()
	if err := a.requireHost(); err != nil {
		return err
	}
	if err := a.requireLive(); err != nil {
		return err
	}
	var req struct {
		Assignments map[string]*uuid.UUID `json:"assignments"`
		Minutes     *int                  `json:"minutes"`
		AllowChoose *bool                 `json:"allow_choose"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	b, err := openBreakout(ctx, s.d.Pool, *a.m.LiveSession)
	if err != nil {
		return err
	}
	if b == nil || b.Status != "open" {
		return httpx.NotFound("No breakout rooms are open.")
	}
	moved := []string{}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		for identity, room := range req.Assignments {
			if room == nil {
				if _, err := tx.Exec(ctx, `DELETE FROM calls_breakout_assignments WHERE breakout_id = $1 AND identity = $2`, b.ID, identity); err != nil {
					return err
				}
			} else {
				if !slices.ContainsFunc(b.Rooms, func(x BreakoutRef) bool { return x.ID == *room }) {
					return httpx.BadRequest("That room isn't one of these breakout rooms.")
				}
				if err := upsertAssignment(ctx, tx, b.ID, identity, *room, ""); err != nil {
					return err
				}
			}
			moved = append(moved, identity)
		}
		if req.Minutes != nil {
			if *req.Minutes < 0 || *req.Minutes > maxBreakoutMins {
				return httpx.BadRequest("The timer can be at most 4 hours.")
			}
			var until *time.Time
			if *req.Minutes > 0 {
				t := time.Now().Add(time.Duration(*req.Minutes) * time.Minute).UTC()
				until = &t
			}
			if _, err := tx.Exec(ctx, `UPDATE calls_breakouts SET ends_at = $2 WHERE id = $1`, b.ID, until); err != nil {
				return err
			}
		}
		if req.AllowChoose != nil {
			if _, err := tx.Exec(ctx, `UPDATE calls_breakouts SET allow_choose = $2 WHERE id = $1`, b.ID, *req.AllowChoose); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if req.Minutes != nil || req.AllowChoose != nil {
		s.syncRoom(ctx, a.m.ID)
	}
	if len(moved) > 0 {
		s.signalRoom(ctx, a.m, signal{Type: SignalAssignment}, moved...)
	}
	out, err := s.hostBreakout(ctx, a.m)
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

// broadcastBreakouts: POST /calls/meetings/{id}/breakouts/broadcast {text}
// (hosts): a message shown in every room.
func (s *Service) broadcastBreakouts(w http.ResponseWriter, r *http.Request, a *actor) error {
	if err := a.requireHost(); err != nil {
		return err
	}
	if err := a.requireLive(); err != nil {
		return err
	}
	var req struct {
		Text string `json:"text"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	text, ok := cleanText(req.Text, 500)
	if !ok {
		return httpx.BadRequest("Write a message.")
	}
	s.signalRoom(r.Context(), a.m, signal{Type: SignalBroadcast, Text: text, From: a.name})
	httpx.NoContent(w)
	return nil
}

// closeBreakouts: POST /calls/meetings/{id}/breakouts/close {now?} (hosts):
// everyone gets a 30-second countdown (none with now), then the rooms end
// and people return to the main room.
func (s *Service) closeBreakouts(w http.ResponseWriter, r *http.Request, a *actor) error {
	ctx := r.Context()
	if err := a.requireHost(); err != nil {
		return err
	}
	if err := a.requireLive(); err != nil {
		return err
	}
	var req struct {
		Now bool `json:"now"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	grace := breakoutGrace
	if req.Now {
		grace = 0
	}
	tag, err := s.d.Pool.Exec(ctx, `UPDATE calls_breakouts SET status = 'closing', closing_at = now() + make_interval(secs => $2)
		WHERE session_id = $1 AND status = 'open'`, *a.m.LiveSession, grace.Seconds())
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 && !req.Now {
		return httpx.NotFound("No breakout rooms are open.")
	}
	if req.Now {
		if err := s.endBreakouts(ctx, *a.m.LiveSession); err != nil {
			return err
		}
	}
	s.syncRoom(ctx, a.m.ID)
	httpx.NoContent(w)
	return nil
}

// endBreakouts closes a session's rooms and ends them at the SFU.
func (s *Service) endBreakouts(ctx context.Context, sessionID uuid.UUID) error {
	b, err := openBreakout(ctx, s.d.Pool, sessionID)
	if err != nil || b == nil {
		return err
	}
	if _, err := s.d.Pool.Exec(ctx, `UPDATE calls_breakouts SET status = 'closed', closed_at = now() WHERE id = $1`, b.ID); err != nil {
		return err
	}
	if s.media == nil {
		return nil
	}
	dctx, cancel := detached(ctx)
	defer cancel()
	for _, room := range b.Rooms {
		if err := s.media.EndRoom(dctx, room.ID.String()); err != nil {
			slog.WarnContext(ctx, "calls: ending a breakout room failed", "room_id", room.ID, "err", err)
		}
	}
	return nil
}

// breakoutTargets lists the SFU rooms of a session's open breakout (for
// removing someone everywhere).
func breakoutRooms(ctx context.Context, q db.Querier, sessionID *uuid.UUID) []string {
	if sessionID == nil {
		return nil
	}
	b, err := openBreakout(ctx, q, *sessionID)
	if err != nil || b == nil {
		return nil
	}
	out := make([]string, len(b.Rooms))
	for i, r := range b.Rooms {
		out[i] = r.ID.String()
	}
	return out
}

// joinBreakout: POST …/breakouts/join {room_id?} (anyone in the call) →
// JoinToken for a breakout room: your assigned room, any room when people
// choose (it becomes yours), any room for hosts.
func (s *Service) joinBreakout(w http.ResponseWriter, r *http.Request, a *actor) error {
	ctx := r.Context()
	if err := a.requireLive(); err != nil {
		return err
	}
	if err := s.requireMedia(); err != nil {
		return err
	}
	var req struct {
		RoomID *uuid.UUID `json:"room_id"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	b, err := openBreakout(ctx, s.d.Pool, *a.m.LiveSession)
	if err != nil {
		return err
	}
	if b == nil || b.Status != "open" {
		return httpx.NewError(http.StatusConflict, "calls_breakouts_closed", "Breakout rooms are closed.")
	}
	var assigned uuid.UUID
	if err := s.d.Pool.QueryRow(ctx, `SELECT room_id FROM calls_breakout_assignments WHERE breakout_id = $1 AND identity = $2`, b.ID, a.identity).Scan(&assigned); err != nil && !db.IsNoRows(err) {
		return err
	}
	target := assigned
	if req.RoomID != nil {
		target = *req.RoomID
	}
	if target == uuid.Nil {
		return httpx.NewError(http.StatusConflict, "calls_breakout_unassigned", "A host hasn't given you a room yet.")
	}
	if !slices.ContainsFunc(b.Rooms, func(x BreakoutRef) bool { return x.ID == target }) {
		return httpx.NotFound("That breakout room isn't open.")
	}
	if target != assigned && !a.host {
		if !b.AllowChoose {
			return httpx.Forbidden("A host assigns the rooms in this call.")
		}
		if err := upsertAssignment(ctx, s.d.Pool, b.ID, a.identity, target, a.name); err != nil {
			return err
		}
	}
	meta := participantMetadata{Role: a.role, UserID: a.userID, AvatarURL: a.avatar}
	tok, err := s.mintFor(ctx, a.m, target.String(), a.identity, a.name, meta)
	if err != nil {
		return err
	}
	httpx.OK(w, tok)
	return nil
}

// breakoutHelp: POST …/breakouts/help (anyone in a breakout room): the
// hosts get "<name> asks for help" with a button to visit the room.
func (s *Service) breakoutHelp(w http.ResponseWriter, r *http.Request, a *actor) error {
	ctx := r.Context()
	if err := a.requireLive(); err != nil {
		return err
	}
	b, err := openBreakout(ctx, s.d.Pool, *a.m.LiveSession)
	if err != nil {
		return err
	}
	if b == nil {
		return httpx.NotFound("No breakout rooms are open.")
	}
	var room uuid.UUID
	if err := s.d.Pool.QueryRow(ctx, `SELECT room_id FROM calls_breakout_assignments WHERE breakout_id = $1 AND identity = $2`, b.ID, a.identity).Scan(&room); err != nil {
		if db.IsNoRows(err) {
			return httpx.NewError(http.StatusConflict, "calls_breakout_unassigned", "You aren't in a breakout room.")
		}
		return err
	}
	hosts, err := hostIDs(ctx, s.d.Pool, a.m)
	if err != nil {
		return err
	}
	ids := make([]string, len(hosts))
	for i, h := range hosts {
		ids[i] = userIdentity(h)
	}
	s.signalRoom(ctx, a.m, signal{Type: SignalHelp, From: a.name, RoomID: &room}, ids...)
	httpx.NoContent(w)
	return nil
}

// sweepBreakouts (job): a timer that ran out starts the countdown; a
// countdown that ran out ends the rooms.
func (s *Service) sweepBreakouts(ctx context.Context) error {
	rows, err := s.d.Pool.Query(ctx, `UPDATE calls_breakouts SET status = 'closing', closing_at = now() + make_interval(secs => $1)
		WHERE status = 'open' AND ends_at < now() RETURNING meeting_id`, breakoutGrace.Seconds())
	if err != nil {
		return err
	}
	timed, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return err
	}
	for _, id := range timed {
		s.syncRoom(ctx, id)
	}
	type due struct {
		SessionID uuid.UUID
		MeetingID uuid.UUID
	}
	rows, err = s.d.Pool.Query(ctx, `SELECT session_id, meeting_id FROM calls_breakouts WHERE status = 'closing' AND closing_at < now() LIMIT 100`)
	if err != nil {
		return err
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[due])
	if err != nil {
		return err
	}
	for _, d := range list {
		if err := s.endBreakouts(ctx, d.SessionID); err != nil {
			return err
		}
		s.syncRoom(ctx, d.MeetingID)
	}
	return nil
}

// breakoutRoomOf maps an SFU room name that is a breakout room to its
// meeting and session (ok false for anything else).
func breakoutRoomOf(ctx context.Context, q db.Querier, roomID uuid.UUID) (meetingID, sessionID uuid.UUID, ok bool, err error) {
	err = q.QueryRow(ctx, `SELECT b.meeting_id, b.session_id FROM calls_breakout_rooms r JOIN calls_breakouts b ON b.id = r.breakout_id WHERE r.id = $1`, roomID).
		Scan(&meetingID, &sessionID)
	if db.IsNoRows(err) {
		return uuid.Nil, uuid.Nil, false, nil
	}
	return meetingID, sessionID, err == nil, err
}

// applyBreakoutEvent records attendance in a breakout room against the
// main session (room started/finished events change nothing).
func applyBreakoutEvent(ctx context.Context, tx pgx.Tx, roomID, sessionID uuid.UUID, ev MediaEvent) error {
	switch ev.Kind {
	case EventParticipantJoined:
		guest, id, ok := parseIdentity(ev.Identity)
		if !ok {
			return nil
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
		_, err := tx.Exec(ctx, `
			INSERT INTO calls_participants (session_id, identity, user_id, name, participant_sid, joined_at, breakout_room_id)
			SELECT $1, $2, $3, $4, $5, $6, $7 WHERE EXISTS (SELECT 1 FROM calls_sessions WHERE id = $1 AND ended_at IS NULL)
			ON CONFLICT (participant_sid) WHERE participant_sid IS NOT NULL DO NOTHING`,
			sessionID, ev.Identity, userID, name, sid, ev.At, roomID)
		return err
	case EventParticipantLeft:
		_, err := tx.Exec(ctx, `UPDATE calls_participants SET left_at = GREATEST($2, joined_at) WHERE participant_sid = $1 AND left_at IS NULL`, ev.ParticipantSID, ev.At)
		return err
	}
	return nil
}
