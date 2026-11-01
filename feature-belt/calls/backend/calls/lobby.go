package calls

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// The lobby: people who may not join directly ask ("knock"), hosts and
// co-hosts admit or deny. Guests without an account hold a secret key from
// their knock and poll with it; people with accounts hear on their user
// topic and poll too. Each poll is a check-in (seen_at): someone who stops
// checking in for lobbyLease, or whose page closes (guests send a beacon to
// /leave), is set aside (away_at) and hosts stop seeing them until they
// check in again. An admitted entry lets its holder (re)join for 12 hours
// unless a host removes them or the session ends.

const maxWaiting = 100 // open knocks per meeting

// lobbyPresent is the SQL condition (alias l) for a knock hosts see and may
// admit: waiting, under lobbyWaitTTL old, and its holder still checking in.
var lobbyPresent = `l.status = 'waiting' AND l.away_at IS NULL
	AND l.seen_at > now() - make_interval(secs => ` + lobbyLeaseSecs + `)
	AND l.created_at > now() - make_interval(secs => ` + lobbyWaitSecs + `)`

const lobbySelect = `
	SELECT l.id, l.meeting_id, l.user_id, u.display_name, u.avatar_url, u.status, l.guest_name, l.status, l.created_at, l.decided_at
	FROM calls_lobby l LEFT JOIN users u ON u.id = l.user_id`

func scanLobby(rows pgx.Rows) ([]LobbyEntry, error) {
	defer rows.Close()
	out := []LobbyEntry{}
	for rows.Next() {
		var e LobbyEntry
		var uid *uuid.UUID
		var uname, ustatus, guestName *string
		var avatar *string
		if err := rows.Scan(&e.ID, &e.MeetingID, &uid, &uname, &avatar, &ustatus, &guestName, &e.Status, &e.CreatedAt, &e.DecidedAt); err != nil {
			return nil, err
		}
		if uid != nil {
			ref := userRef(*uid, deref(uname), avatar, deref(ustatus))
			e.User, e.Name = &ref, ref.DisplayName
		} else {
			e.Guest, e.Name = true, deref(guestName)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func loadLobby(ctx context.Context, q db.Querier, where string, args ...any) ([]LobbyEntry, error) {
	rows, err := q.Query(ctx, lobbySelect+" "+where, args...)
	if err != nil {
		return nil, err
	}
	return scanLobby(rows)
}

// latestKnock is userID's most recent request to join a meeting (12 hours).
func latestKnock(ctx context.Context, q db.Querier, meetingID, userID uuid.UUID) (*LobbyEntry, error) {
	list, err := loadLobby(ctx, q, `WHERE l.meeting_id = $1 AND l.user_id = $2 AND l.created_at > now() - make_interval(secs => $3)
		ORDER BY l.created_at DESC LIMIT 1`, meetingID, userID, admittedTTL.Seconds())
	if err != nil || len(list) == 0 {
		return nil, err
	}
	return &list[0], nil
}

// hostIDs are the people who run a meeting: the host and co-hosts.
func hostIDs(ctx context.Context, q db.Querier, m *meetingRow) ([]uuid.UUID, error) {
	rows, err := q.Query(ctx, `SELECT user_id FROM calls_invitees WHERE meeting_id = $1 AND role = 'cohost'`, m.ID)
	if err != nil {
		return nil, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	return append(ids, m.HostID), err
}

// lobbyEvent is EventLobby on hosts' user topics.
type lobbyEvent struct {
	MeetingID uuid.UUID  `json:"meeting_id"`
	Code      string     `json:"code"`
	Title     string     `json:"title"`
	Entry     LobbyEntry `json:"entry"`
	Reason    string     `json:"reason"` // knock | decided | canceled | left (stopped checking in) | expired
}

func (s *Service) tellHosts(ctx context.Context, m *meetingRow, e LobbyEntry, reason string) {
	ids, err := hostIDs(ctx, s.d.Pool, m)
	if err != nil {
		return
	}
	ev := lobbyEvent{MeetingID: m.ID, Code: m.Code, Title: m.Title, Entry: e, Reason: reason}
	for _, id := range ids {
		s.publishUser(ctx, id, EventLobby, ev)
	}
}

// lobbyDecided tells each person a host decided about, and the other hosts.
func (s *Service) lobbyDecided(ctx context.Context, m *meetingRow, entries []LobbyEntry) {
	for _, e := range entries {
		if e.User != nil {
			s.publishUser(ctx, e.User.ID, EventLobbyDecided, e)
		}
		s.tellHosts(ctx, m, e, "decided")
	}
}

// expireStale closes a meeting's knocks nobody answered in lobbyWaitTTL
// (hosts' lists already leave them out; sweepLobby tells hosts in time).
func expireStale(ctx context.Context, q db.Querier, meetingID uuid.UUID) error {
	_, err := q.Exec(ctx, `UPDATE calls_lobby SET status = 'expired', updated_at = now()
		WHERE meeting_id = $1 AND status = 'waiting' AND created_at <= now() - make_interval(secs => $2)`, meetingID, lobbyWaitTTL.Seconds())
	return err
}

// sweepLobby (job): sets aside people who stopped checking in and expires
// knocks nobody answered, and tells each meeting's hosts so their lists and
// counts drop them at once.
func (s *Service) sweepLobby(ctx context.Context) error {
	type gone struct {
		ID, MeetingID uuid.UUID
		Reason        string
	}
	var out []gone
	for _, sql := range []string{
		`UPDATE calls_lobby SET away_at = now(), updated_at = now()
			WHERE status = 'waiting' AND away_at IS NULL AND seen_at <= now() - make_interval(secs => ` + lobbyLeaseSecs + `)
			RETURNING id, meeting_id, 'left'`,
		`UPDATE calls_lobby SET status = 'expired', updated_at = now()
			WHERE status = 'waiting' AND created_at <= now() - make_interval(secs => ` + lobbyWaitSecs + `)
			RETURNING id, meeting_id, CASE WHEN away_at IS NULL THEN 'expired' ELSE '' END`,
	} {
		rows, err := s.d.Pool.Query(ctx, sql)
		if err != nil {
			return err
		}
		list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[gone])
		if err != nil {
			return err
		}
		out = append(out, list...)
	}
	meetings := map[uuid.UUID]*meetingRow{}
	for _, g := range out {
		if g.Reason == "" { // already set aside: hosts were told then
			continue
		}
		m, ok := meetings[g.MeetingID]
		if !ok {
			var err error
			if m, err = loadMeeting(ctx, s.d.Pool, g.MeetingID); err != nil {
				m = nil
			}
			meetings[g.MeetingID] = m
		}
		if m == nil {
			continue
		}
		list, err := loadLobby(ctx, s.d.Pool, `WHERE l.id = $1`, g.ID)
		if err != nil || len(list) == 0 {
			continue
		}
		s.tellHosts(ctx, m, list[0], g.Reason)
	}
	return nil
}

// checkIn records that a knock's holder is still there (owner is the SQL
// condition on l, with $2 its argument): a waiting knock set aside comes
// back (hosts hear it again), one past lobbyWaitTTL expires. Knocks in any
// other state are left alone. It returns the knock as it is now.
func (s *Service) checkIn(ctx context.Context, id uuid.UUID, owner string, ownerArg any) (LobbyEntry, error) {
	var wasAway bool
	var status string
	err := s.d.Pool.QueryRow(ctx, `
		WITH cur AS (SELECT l.id, l.away_at FROM calls_lobby l WHERE l.id = $1 AND `+owner+` AND l.status = 'waiting' FOR UPDATE)
		UPDATE calls_lobby l SET seen_at = now(), away_at = NULL, updated_at = now(),
			status = CASE WHEN l.created_at <= now() - make_interval(secs => `+lobbyWaitSecs+`) THEN 'expired' ELSE 'waiting' END
		FROM cur WHERE l.id = cur.id
		RETURNING cur.away_at IS NOT NULL, l.status`, id, ownerArg).Scan(&wasAway, &status)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return LobbyEntry{}, err
	}
	list, err := loadLobby(ctx, s.d.Pool, `WHERE l.id = $1 AND `+owner, id, ownerArg)
	if err != nil {
		return LobbyEntry{}, err
	}
	if len(list) == 0 {
		return LobbyEntry{}, errLobbyNotFound
	}
	e := list[0]
	switch {
	case status == "waiting" && wasAway:
		s.tellLobbyHosts(ctx, e, "knock")
	case status == "expired" && !wasAway:
		s.tellLobbyHosts(ctx, e, "expired")
	}
	return e, nil
}

// tellLobbyHosts is tellHosts for an entry whose meeting isn't loaded.
func (s *Service) tellLobbyHosts(ctx context.Context, e LobbyEntry, reason string) {
	if m, err := loadMeeting(ctx, s.d.Pool, e.MeetingID); err == nil {
		s.tellHosts(ctx, m, e, reason)
	}
}

// ---- people with accounts --------------------------------------------------------

// knock: POST /calls/meetings/{id}/knock.
func (s *Service) knock(w http.ResponseWriter, r *http.Request) error {
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
	if a.canJoin() {
		return httpx.NewError(http.StatusConflict, "calls_no_knock_needed", "You can join without asking.")
	}
	if a.Mode != ModeKnock {
		return joinRefusal(a)
	}
	var entry LobbyEntry
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if err := checkLobbyRoom(ctx, tx, m.ID); err != nil {
			return err
		}
		// Knocking again while still waiting is a check-in.
		if _, err := tx.Exec(ctx, `INSERT INTO calls_lobby (meeting_id, user_id) VALUES ($1, $2)
			ON CONFLICT (meeting_id, user_id) WHERE status = 'waiting' AND user_id IS NOT NULL
			DO UPDATE SET seen_at = now(), away_at = NULL, updated_at = now()`, m.ID, me.UserID); err != nil {
			return err
		}
		list, err := loadLobby(ctx, tx, `WHERE l.meeting_id = $1 AND l.user_id = $2 AND l.status = 'waiting'`, m.ID, me.UserID)
		if err != nil {
			return err
		}
		if len(list) == 0 {
			return errLobbyNotFound
		}
		entry = list[0]
		return nil
	})
	if err != nil {
		return err
	}
	s.tellHosts(ctx, m, entry, "knock")
	httpx.JSON(w, http.StatusCreated, entry)
	return nil
}

// checkLobbyRoom expires a meeting's unanswered knocks and refuses a new one
// when too many people are waiting (inside the knock's transaction).
func checkLobbyRoom(ctx context.Context, tx pgx.Tx, meetingID uuid.UUID) error {
	if err := expireStale(ctx, tx, meetingID); err != nil {
		return err
	}
	var waiting int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM calls_lobby l WHERE l.meeting_id = $1 AND `+lobbyPresent, meetingID).Scan(&waiting); err != nil {
		return err
	}
	if waiting >= maxWaiting {
		return httpx.NewError(http.StatusTooManyRequests, "calls_lobby_full", "Too many people are waiting. Try again in a few minutes.")
	}
	return nil
}

// myKnock loads {id} owned by the caller.
func (s *Service) myKnock(r *http.Request, me *auth.Identity) (LobbyEntry, error) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return LobbyEntry{}, errLobbyNotFound
	}
	list, err := loadLobby(r.Context(), s.d.Pool, `WHERE l.id = $1 AND l.user_id = $2`, id, me.UserID)
	if err != nil {
		return LobbyEntry{}, err
	}
	if len(list) == 0 {
		return LobbyEntry{}, errLobbyNotFound
	}
	return list[0], nil
}

// getMyKnock: GET /calls/lobby/{id}. Polling it while waiting is the check-in.
func (s *Service) getMyKnock(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return errLobbyNotFound
	}
	e, err := s.checkIn(ctx, id, `l.user_id = $2`, me.UserID)
	if err != nil {
		return err
	}
	httpx.OK(w, e)
	return nil
}

// cancelMyKnock: DELETE /calls/lobby/{id}.
func (s *Service) cancelMyKnock(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	e, err := s.myKnock(r, auth.MustFromContext(ctx))
	if err != nil {
		return err
	}
	return s.cancelKnock(ctx, w, e)
}

func (s *Service) cancelKnock(ctx context.Context, w http.ResponseWriter, e LobbyEntry) error {
	tag, err := s.d.Pool.Exec(ctx, `UPDATE calls_lobby SET status = 'canceled', updated_at = now() WHERE id = $1 AND status = 'waiting'`, e.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		if m, err := loadMeeting(ctx, s.d.Pool, e.MeetingID); err == nil {
			e.Status = "canceled"
			s.tellHosts(ctx, m, e, "canceled")
		}
	}
	httpx.NoContent(w)
	return nil
}

// ---- hosts -------------------------------------------------------------------------

// listLobby: GET /calls/meetings/{id}/lobby (hosts) → people waiting, oldest first.
func (s *Service) listLobby(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	m, _, err := s.meetingAsHost(r, auth.MustFromContext(ctx))
	if err != nil {
		return err
	}
	list, err := loadLobby(ctx, s.d.Pool, `WHERE l.meeting_id = $1 AND `+lobbyPresent+` ORDER BY l.created_at, l.id LIMIT $2`, m.ID, maxWaiting)
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.Page[LobbyEntry]{Items: list})
	return nil
}

type decideReq struct {
	IDs []uuid.UUID `json:"ids"` // empty with all=true decides everyone waiting
	All bool        `json:"all"`
}

func (s *Service) admit(w http.ResponseWriter, r *http.Request) error {
	return s.decide(w, r, "admitted")
}

func (s *Service) deny(w http.ResponseWriter, r *http.Request) error {
	return s.decide(w, r, "denied")
}

// decide: POST /calls/meetings/{id}/lobby/admit|deny {ids} or {all:true}.
func (s *Service) decide(w http.ResponseWriter, r *http.Request, status string) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	m, _, err := s.meetingAsHost(r, me)
	if err != nil {
		return err
	}
	var req decideReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if len(req.IDs) == 0 && !req.All {
		return httpx.BadRequest("Choose who to let in.")
	}
	if len(req.IDs) > maxWaiting {
		return httpx.BadRequest("Too many people at once.")
	}
	var ids []uuid.UUID
	if !req.All {
		ids = req.IDs
	}
	entries, err := decideWaiting(ctx, s.d.Pool, m.ID, ids, status, me.UserID)
	if err != nil {
		return err
	}
	s.lobbyDecided(ctx, m, entries)
	httpx.OK(w, httpx.Page[LobbyEntry]{Items: entries})
	return nil
}

// decideWaiting moves waiting entries (ids, or all when ids is nil) to
// status. Only people still there are decided: "Admit all" never lets in
// someone who closed the page.
func decideWaiting(ctx context.Context, q db.Querier, meetingID uuid.UUID, ids []uuid.UUID, status string, by uuid.UUID) ([]LobbyEntry, error) {
	rows, err := q.Query(ctx, `
		UPDATE calls_lobby l SET status = $3, decided_by = $4, decided_at = now(), updated_at = now()
		WHERE l.meeting_id = $1 AND `+lobbyPresent+` AND ($2::uuid[] IS NULL OR l.id = ANY($2))
		RETURNING l.id`, meetingID, ids, status, by)
	if err != nil {
		return nil, err
	}
	changed, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil || len(changed) == 0 {
		return []LobbyEntry{}, err
	}
	return loadLobby(ctx, q, `WHERE l.id = ANY($1) ORDER BY l.created_at`, changed)
}

// ---- guests ---------------------------------------------------------------------------

// publicMeeting: GET /calls/public/meetings/{code}.
func (s *Service) publicMeeting(w http.ResponseWriter, r *http.Request) error {
	m, err := s.publicCode(r)
	if err != nil {
		return err
	}
	a := guestAccess(m)
	out := PublicMeeting{
		Code: m.Code, GuestsAllowed: a.Mode == ModeDirect || a.Mode == ModeKnock, LobbyEnabled: m.LobbyEnabled,
		Live: m.LiveNow, Canceled: m.CanceledAt != nil, Locked: m.Locked,
	}
	if m.Access == AccessOpen && m.CanceledAt == nil {
		title := m.Title
		host := userRef(m.HostID, m.HostName, m.HostAvatar, m.HostStatus).DisplayName
		out.Title, out.HostName = &title, &host
	}
	httpx.OK(w, out)
	return nil
}

func (s *Service) publicCode(r *http.Request) (*meetingRow, error) {
	code, ok := normalizeCode(chi.URLParam(r, "code"))
	if !ok {
		return nil, errMeetingNotFound
	}
	return loadMeetingRef(r.Context(), s.d.Pool, code)
}

// guestName validates a guest's name: 1–60 printable characters.
func guestName(in string) (string, error) {
	name := strings.Join(strings.Fields(in), " ")
	if name == "" {
		return "", httpx.BadRequest("Enter your name.")
	}
	if len([]rune(name)) > maxGuestName {
		return "", httpx.BadRequest("Use at most 60 characters for your name.")
	}
	for _, c := range name {
		if !unicode.IsPrint(c) {
			return "", httpx.BadRequest("Use letters, numbers and punctuation in your name.")
		}
	}
	return name, nil
}

// guestKnock: POST /calls/public/meetings/{code}/knock {name} → GuestKnock.
// With the lobby off the entry is admitted at once.
func (s *Service) guestKnock(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	m, err := s.publicCode(r)
	if err != nil {
		return err
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	name, err := guestName(req.Name)
	if err != nil {
		return err
	}
	a := guestAccess(m)
	if a.Mode != ModeKnock && a.Mode != ModeDirect {
		return joinRefusal(a)
	}
	status := "waiting"
	if a.Mode == ModeDirect {
		status = "admitted"
	}
	key, hash := newGuestKey()
	var entry LobbyEntry
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if err := checkLobbyRoom(ctx, tx, m.ID); err != nil {
			return err
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO calls_lobby (meeting_id, guest_name, key_hash, status, decided_at)
			VALUES ($1, $2, $3, $4, CASE WHEN $4 = 'admitted' THEN now() END) RETURNING id`,
			m.ID, name, hash, status).Scan(&id); err != nil {
			return err
		}
		list, err := loadLobby(ctx, tx, `WHERE l.id = $1`, id)
		if err != nil || len(list) == 0 {
			return err
		}
		entry = list[0]
		return nil
	})
	if err != nil {
		return err
	}
	if status == "waiting" {
		s.tellHosts(ctx, m, entry, "knock")
	}
	httpx.JSON(w, http.StatusCreated, GuestKnock{LobbyEntry: entry, Key: key})
	return nil
}

type guestKeyReq struct {
	Key string `json:"key"`
}

// guestOwner is the SQL condition for a guest's own entry ($2: the key's hash).
const guestOwner = `l.user_id IS NULL AND l.key_hash = $2`

// guestRef reads {id} and the guest's key from the request (a malformed one = not found).
func guestRef(r *http.Request) (uuid.UUID, []byte, error) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return uuid.Nil, nil, errLobbyNotFound
	}
	var req guestKeyReq
	if err := httpx.Decode(r, &req); err != nil {
		return uuid.Nil, nil, err
	}
	if !strings.HasPrefix(req.Key, "cgk_") {
		return uuid.Nil, nil, errLobbyNotFound
	}
	return id, hashKey(req.Key), nil
}

// guestEntry loads {id} with the guest's key (wrong key = not found).
func (s *Service) guestEntry(r *http.Request) (LobbyEntry, error) {
	id, hash, err := guestRef(r)
	if err != nil {
		return LobbyEntry{}, err
	}
	list, err := loadLobby(r.Context(), s.d.Pool, `WHERE l.id = $1 AND `+guestOwner, id, hash)
	if err != nil {
		return LobbyEntry{}, err
	}
	if len(list) == 0 {
		return LobbyEntry{}, errLobbyNotFound
	}
	return list[0], nil
}

// guestStatus: POST /calls/public/lobby/{id}/status {key} → LobbyEntry.
// Polling it while waiting is the guest's check-in.
func (s *Service) guestStatus(w http.ResponseWriter, r *http.Request) error {
	id, hash, err := guestRef(r)
	if err != nil {
		return err
	}
	e, err := s.checkIn(r.Context(), id, guestOwner, hash)
	if err != nil {
		return err
	}
	httpx.OK(w, e)
	return nil
}

// guestLeave: POST /calls/public/lobby/{id}/leave {key}, sent as a beacon
// when the waiting page closes (any content type, so browsers send it
// without a preflight). Hosts stop seeing the guest at once; a check-in
// from the same page (a reload) brings the knock back.
func (s *Service) guestLeave(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id, hash, err := guestRef(r)
	if err != nil {
		return err
	}
	list, err := loadLobby(ctx, s.d.Pool, `WHERE l.id = $1 AND `+guestOwner, id, hash)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		return errLobbyNotFound
	}
	tag, err := s.d.Pool.Exec(ctx, `UPDATE calls_lobby SET away_at = now(), updated_at = now(),
		seen_at = LEAST(seen_at, now() - make_interval(secs => `+lobbyLeaseSecs+`))
		WHERE id = $1 AND status = 'waiting' AND away_at IS NULL`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		s.tellLobbyHosts(ctx, list[0], "left")
	}
	httpx.NoContent(w)
	return nil
}

// guestCancel: POST /calls/public/lobby/{id}/cancel {key}.
func (s *Service) guestCancel(w http.ResponseWriter, r *http.Request) error {
	e, err := s.guestEntry(r)
	if err != nil {
		return err
	}
	return s.cancelKnock(r.Context(), w, e)
}

// admittedGuest checks a guest entry may (re)join now.
func (s *Service) admittedGuest(ctx context.Context, e LobbyEntry) (*meetingRow, error) {
	if e.Status != "admitted" || e.DecidedAt == nil || time.Since(*e.DecidedAt) > admittedTTL {
		switch e.Status {
		case "waiting":
			return nil, httpx.NewError(http.StatusForbidden, "calls_waiting", "A host hasn't let you in yet.")
		case "removed":
			return nil, httpx.NewError(http.StatusForbidden, "calls_not_allowed", reasonMessage(ReasonRemoved))
		case "denied":
			return nil, httpx.NewError(http.StatusForbidden, "calls_denied", "A host didn't let you in.")
		}
		return nil, httpx.NewError(http.StatusForbidden, "calls_expired", "Your request to join ended. Ask to join again.")
	}
	m, err := loadMeeting(ctx, s.d.Pool, e.MeetingID)
	if err != nil {
		return nil, err
	}
	if m.CanceledAt != nil {
		return nil, errMeetingCanceled
	}
	if m.LiveSession != nil {
		var removed bool
		if err := s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM calls_removals WHERE session_id = $1 AND identity = $2)`,
			*m.LiveSession, guestIdentity(e.ID)).Scan(&removed); err != nil {
			return nil, err
		}
		if removed {
			return nil, httpx.NewError(http.StatusForbidden, "calls_not_allowed", reasonMessage(ReasonRemoved))
		}
	}
	return m, nil
}

// guestToken: POST /calls/public/lobby/{id}/token {key} → JoinToken once admitted.
func (s *Service) guestToken(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	if err := s.requireMedia(); err != nil {
		return err
	}
	e, err := s.guestEntry(r)
	if err != nil {
		return err
	}
	m, err := s.admittedGuest(ctx, e)
	if err != nil {
		return err
	}
	tok, err := s.mint(ctx, m, guestIdentity(e.ID), e.Name, participantMetadata{Role: RoleGuest})
	if err != nil {
		return err
	}
	httpx.OK(w, tok)
	return nil
}

// guestHand: POST /calls/public/lobby/{id}/hand {key, raised}.
func (s *Service) guestHand(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	if err := s.requireMedia(); err != nil {
		return err
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return errLobbyNotFound
	}
	var req struct {
		Key    string `json:"key"`
		Raised bool   `json:"raised"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	list, err := loadLobby(ctx, s.d.Pool, `WHERE l.id = $1 AND `+guestOwner, id, hashKey(req.Key))
	if err != nil {
		return err
	}
	if len(list) == 0 {
		return errLobbyNotFound
	}
	m, err := s.admittedGuest(ctx, list[0])
	if err != nil {
		return err
	}
	if err := s.setHand(ctx, m, guestIdentity(id), req.Raised); err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}
