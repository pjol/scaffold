package calls

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/notify"
	"scaffoldapp/backend/internal/orgs"
	"scaffoldapp/backend/internal/realtime"
)

// ---- create ------------------------------------------------------------------

type createMeetingReq struct {
	Title        string      `json:"title"`
	OrgID        *uuid.UUID  `json:"org_id"`
	Access       string      `json:"access"`
	LobbyEnabled *bool       `json:"lobby_enabled"`
	StartsAt     *time.Time  `json:"starts_at"`
	EndsAt       *time.Time  `json:"ends_at"`
	InviteeIDs   []uuid.UUID `json:"invitee_ids"`
	CohostIDs    []uuid.UUID `json:"cohost_ids"`
	// TimeZone is the creator's IANA zone (their device's): invitations are
	// worded in it for people whose own zone isn't known (SetUserZone).
	TimeZone string `json:"time_zone"`
}

// createMeeting: POST /calls/meetings. Instant ("New meeting") or scheduled.
func (s *Service) createMeeting(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	var req createMeetingReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.OrgID != nil {
		role, err := orgs.RoleOf(ctx, s.d.Pool, *req.OrgID, me.UserID)
		if err != nil {
			return err
		}
		if role == "" {
			return httpx.NotFound(orgs.NounTitle() + " not found.")
		}
	}
	people := append(slices.Clone(req.InviteeIDs), req.CohostIDs...)
	if err := s.checkInvitable(ctx, s.d.Pool, me.UserID, people); err != nil {
		return err
	}
	in := MeetingInput{
		HostID: me.UserID, OrgID: req.OrgID, Title: req.Title, Access: req.Access, LobbyEnabled: req.LobbyEnabled,
		StartsAt: req.StartsAt, EndsAt: req.EndsAt, InviteeIDs: req.InviteeIDs, CohostIDs: req.CohostIDs,
	}
	var m Meeting
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var err error
		m, err = createMeeting(ctx, tx, in)
		return err
	})
	if err != nil {
		return err
	}
	s.notifyInvited(ctx, m, me, people, requestZone(req.TimeZone))
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

// notifyInvited tells newly invited people about a meeting (best-effort),
// with its time in each person's zone (else fallback, the inviter's).
func (s *Service) notifyInvited(ctx context.Context, m Meeting, by *auth.Identity, ids []uuid.UUID, fallback *time.Location) {
	for _, id := range ids {
		if id == by.UserID {
			continue
		}
		body := inviteText(by.DisplayName, m, zoneFor(ctx, id, fallback))
		s.send(ctx, notify.Notification{
			UserID: id, Kind: KindNotifyInvite, Title: "Meeting invitation", Body: body,
			Link: "/meet/" + m.Code, Data: map[string]any{"meeting_id": m.ID, "code": m.Code},
		})
	}
}

// inviteText is an invitation's body: "Sam invited you to Weekly sync on
// Mon, Nov 2, 2026 · 7:00 AM – 7:30 AM PST.", the time in loc.
func inviteText(inviter string, m Meeting, loc *time.Location) string {
	title := m.Title
	if title == "" {
		title = "a meeting"
	}
	if m.StartsAt == nil {
		return inviter + " invited you to " + title + "."
	}
	return inviter + " invited you to " + title + " on " + whenText(*m.StartsAt, m.EndsAt, loc) + "."
}

// ---- read ------------------------------------------------------------------------

// getMeeting: GET /calls/meetings/{id} (an id or a code) → JoinInfo.
func (s *Service) getMeeting(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	m, err := loadMeetingRef(ctx, s.d.Pool, chi.URLParam(r, "id"))
	if err != nil {
		return err
	}
	info, err := s.joinInfo(ctx, m, me.UserID)
	if err != nil {
		return err
	}
	httpx.OK(w, info)
	return nil
}

func (s *Service) joinInfo(ctx context.Context, m *meetingRow, userID uuid.UUID) (JoinInfo, error) {
	a, err := s.accessFor(ctx, s.d.Pool, m, userID)
	if err != nil {
		return JoinInfo{}, err
	}
	info := JoinInfo{MeetingID: m.ID, Code: m.Code, Kind: m.Kind, Mode: a.Mode, Live: m.LiveNow}
	if a.Reason != "" {
		info.Reason = &a.Reason
	}
	if a.canJoin() || a.Mode == ModeKnock {
		title := m.Title
		host := userRef(m.HostID, m.HostName, m.HostAvatar, m.HostStatus).DisplayName
		info.Title, info.HostName = &title, &host
	}
	if a.canJoin() {
		d, err := s.detail(ctx, s.d.Pool, m, a.Role)
		if err != nil {
			return JoinInfo{}, err
		}
		info.Meeting = &d
	}
	if a.Mode == ModeKnock || a.Mode == ModeDenied {
		e, err := latestKnock(ctx, s.d.Pool, m.ID, userID)
		if err != nil {
			return JoinInfo{}, err
		}
		info.Lobby = e
	}
	return info, nil
}

// ---- update ------------------------------------------------------------------------

type updateMeetingReq struct {
	Title        *string    `json:"title"`
	Access       *string    `json:"access"`
	LobbyEnabled *bool      `json:"lobby_enabled"`
	Locked       *bool      `json:"locked"`
	StartsAt     *time.Time `json:"starts_at"`
	EndsAt       *time.Time `json:"ends_at"`
	ClearTimes   bool       `json:"clear_times"` // make it an unscheduled link
}

// updateMeeting: PATCH /calls/meetings/{id} (hosts). Settings and schedule.
func (s *Service) updateMeeting(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	m, a, err := s.meetingAsHost(r, me)
	if err != nil {
		return err
	}
	var req updateMeetingReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if m.CanceledAt != nil {
		return errMeetingCanceled
	}
	// A meeting another feature made (a calendar event's room) takes its
	// title and schedule from there; hosts still run the room (who can join,
	// the lobby, the lock).
	if m.SourceKind != nil && (req.Title != nil || req.StartsAt != nil || req.EndsAt != nil || req.ClearTimes) {
		return errSourced(m, "Change its title and time")
	}
	upd := MeetingUpdate{Title: req.Title, Access: req.Access, LobbyEnabled: req.LobbyEnabled, Locked: req.Locked,
		StartsAt: req.StartsAt, EndsAt: req.EndsAt, ClearTimes: req.ClearTimes}
	var admitted []LobbyEntry
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if _, err := updateMeeting(ctx, tx, m.ID, upd); err != nil {
			return err
		}
		// Opening the link (open, lobby off) lets everyone waiting in.
		after, err := loadMeeting(ctx, tx, m.ID)
		if err != nil {
			return err
		}
		if after.Access == AccessOpen && !after.LobbyEnabled && !after.Locked {
			admitted, err = decideWaiting(ctx, tx, m.ID, nil, "admitted", me.UserID)
		}
		return err
	})
	if err != nil {
		return err
	}
	row, err := loadMeeting(ctx, s.d.Pool, m.ID)
	if err != nil {
		return err
	}
	d, err := s.detail(ctx, s.d.Pool, row, a.Role)
	if err != nil {
		return err
	}
	s.publish(ctx, meetingTopic(m.ID), EventMeetingUpdated, d.Meeting)
	s.lobbyDecided(ctx, row, admitted)
	if req.Access != nil || req.Locked != nil || req.LobbyEnabled != nil {
		s.revokeMeeting(ctx, m.ID) // people the new setting keeps out stop hearing
	}
	httpx.OK(w, d)
	return nil
}

// cancelMeeting: DELETE /calls/meetings/{id} (the host). The link stops
// working, a live room ends, and invitees of an upcoming meeting hear.
func (s *Service) cancelMeeting(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	m, _, err := s.meetingAsHost(r, me)
	if err != nil {
		return err
	}
	if m.HostID != me.UserID {
		return errNotOwner
	}
	if m.CanceledAt != nil {
		httpx.NoContent(w)
		return nil
	}
	if m.SourceKind != nil {
		return errSourced(m, "Cancel it")
	}
	var ids []uuid.UUID
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if err := cancelMeeting(ctx, tx, m.ID); err != nil {
			return err
		}
		if m.StartsAt != nil && (m.EndsAt == nil || m.EndsAt.After(time.Now())) {
			rows, err := tx.Query(ctx, `SELECT user_id FROM calls_invitees WHERE meeting_id = $1`, m.ID)
			if err != nil {
				return err
			}
			ids, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.afterCancel(ctx, m)
	s.revokeMeeting(ctx, m.ID)
	for _, id := range ids {
		title := m.Title
		if title == "" {
			title = "A meeting"
		}
		s.send(ctx, notify.Notification{UserID: id, Kind: KindNotifyInvite, Title: "Meeting canceled",
			Body: title + " was canceled.", Link: "/calls", Data: map[string]any{"meeting_id": m.ID}})
	}
	httpx.NoContent(w)
	return nil
}

// afterCancel ends a live room of a canceled meeting and tells subscribers.
func (s *Service) afterCancel(ctx context.Context, m *meetingRow) {
	s.publish(ctx, meetingTopic(m.ID), EventMeetingUpdated, map[string]any{"id": m.ID, "canceled": true})
	if m.LiveSession != nil && s.media != nil {
		dctx, cancel := detached(ctx)
		defer cancel()
		if err := s.media.EndRoom(dctx, m.ID.String()); err != nil {
			slog.WarnContext(ctx, "calls: ending a canceled meeting's room failed", "meeting_id", m.ID, "err", err)
		}
	}
}

// ---- invitees ----------------------------------------------------------------------

type inviteesReq struct {
	UserIDs  []uuid.UUID `json:"user_ids"`
	Role     string      `json:"role"`      // invitee (default) | cohost
	TimeZone string      `json:"time_zone"` // the inviter's IANA zone (see createMeetingReq)
}

// addInvitees: POST /calls/meetings/{id}/invitees (hosts; co-hosts only by the host).
func (s *Service) addInvitees(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	m, a, err := s.meetingAsHost(r, me)
	if err != nil {
		return err
	}
	var req inviteesReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.Role == "" {
		req.Role = RoleInvitee
	}
	if req.Role != RoleInvitee && req.Role != RoleCohost {
		return httpx.BadRequest("role must be invitee or cohost.")
	}
	if req.Role == RoleCohost && a.Mode != ModeHost {
		return errNotOwner
	}
	if len(req.UserIDs) == 0 {
		return httpx.BadRequest("Choose at least 1 person.")
	}
	if m.CanceledAt != nil {
		return errMeetingCanceled
	}
	if m.SourceKind != nil && req.Role == RoleInvitee {
		return errSourced(m, "Invite people")
	}
	if err := s.checkInvitable(ctx, s.d.Pool, me.UserID, req.UserIDs); err != nil {
		return err
	}
	var added []uuid.UUID
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM calls_invitees WHERE meeting_id = $1`, m.ID).Scan(&n); err != nil {
			return err
		}
		if n+len(req.UserIDs) > maxInvitees {
			return httpx.BadRequest(fmt.Sprintf("A meeting can have at most %d invitees.", maxInvitees))
		}
		rows, err := tx.Query(ctx, `
			INSERT INTO calls_invitees (meeting_id, user_id, role, invited_by)
			SELECT $1, u, $3, $4 FROM unnest($2::uuid[]) AS u WHERE u <> (SELECT host_id FROM calls_meetings WHERE id = $1)
			ON CONFLICT (meeting_id, user_id) DO UPDATE SET role = CASE WHEN $3 = 'cohost' THEN 'cohost' ELSE calls_invitees.role END,
			  updated_at = now()
			RETURNING user_id, (xmax = 0)`, m.ID, req.UserIDs, req.Role, me.UserID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			var inserted bool
			if err := rows.Scan(&id, &inserted); err != nil {
				return err
			}
			if inserted {
				added = append(added, id)
			}
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}
	s.notifyInvited(ctx, s.toMeeting(m), me, added, requestZone(req.TimeZone))
	if req.Role == RoleCohost {
		for _, id := range req.UserIDs {
			s.syncRole(ctx, m, id, RoleCohost)
		}
	}
	return s.respondDetail(ctx, w, m.ID, a.Role)
}

// setInviteeRole: PATCH /calls/meetings/{id}/invitees/{userID} {role} (the host).
func (s *Service) setInviteeRole(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	m, a, err := s.meetingAsHost(r, me)
	if err != nil {
		return err
	}
	if a.Mode != ModeHost {
		return errNotOwner
	}
	userID, err := httpx.URLParamUUID(r, "userID")
	if err != nil {
		return err
	}
	var req struct {
		Role string `json:"role"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.Role != RoleInvitee && req.Role != RoleCohost {
		return httpx.BadRequest("role must be invitee or cohost.")
	}
	if userID == m.HostID {
		return httpx.BadRequest("The host is already in charge.")
	}
	// Making someone in the call a co-host invites them if they weren't.
	if err := s.checkInvitable(ctx, s.d.Pool, me.UserID, []uuid.UUID{userID}); err != nil {
		return err
	}
	if _, err := s.d.Pool.Exec(ctx, `
		INSERT INTO calls_invitees (meeting_id, user_id, role, invited_by) VALUES ($1, $2, $3, $4)
		ON CONFLICT (meeting_id, user_id) DO UPDATE SET role = EXCLUDED.role, updated_at = now()`,
		m.ID, userID, req.Role, me.UserID); err != nil {
		return err
	}
	newRole := req.Role
	if newRole == RoleInvitee {
		newRole = RoleMember
	}
	s.syncRole(ctx, m, userID, newRole)
	return s.respondDetail(ctx, w, m.ID, a.Role)
}

// removeInvitee: DELETE /calls/meetings/{id}/invitees/{userID}. Hosts remove
// invitees (only the host removes co-hosts); anyone may remove themselves.
func (s *Service) removeInvitee(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	userID, err := httpx.URLParamUUID(r, "userID")
	if err != nil {
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
	var role string
	err = s.d.Pool.QueryRow(ctx, `SELECT role FROM calls_invitees WHERE meeting_id = $1 AND user_id = $2`, m.ID, userID).Scan(&role)
	if db.IsNoRows(err) {
		if !a.canJoin() {
			return errMeetingNotFound
		}
		httpx.NoContent(w)
		return nil
	}
	if err != nil {
		return err
	}
	switch {
	case userID == me.UserID:
	case a.Mode == ModeHost:
	case a.Mode == ModeCohost && role == RoleInvitee:
	case a.canJoin() || a.Mode == ModeCohost:
		return errNotHost
	default:
		return errMeetingNotFound
	}
	if userID != me.UserID && m.SourceKind != nil {
		return errSourced(m, "Remove people")
	}
	if _, err := s.d.Pool.Exec(ctx, `DELETE FROM calls_invitees WHERE meeting_id = $1 AND user_id = $2`, m.ID, userID); err != nil {
		return err
	}
	if role == RoleCohost {
		s.syncRole(ctx, m, userID, RoleMember)
	}
	s.revokeMeeting(ctx, m.ID, userID)
	httpx.NoContent(w)
	return nil
}

func (s *Service) respondDetail(ctx context.Context, w http.ResponseWriter, id uuid.UUID, role string) error {
	row, err := loadMeeting(ctx, s.d.Pool, id)
	if err != nil {
		return err
	}
	d, err := s.detail(ctx, s.d.Pool, row, role)
	if err != nil {
		return err
	}
	s.publish(ctx, meetingTopic(id), EventMeetingUpdated, d.Meeting)
	httpx.OK(w, d)
	return nil
}

// ---- join ------------------------------------------------------------------------

// participantMetadata is the server-owned JSON each participant carries in
// the room. Clients read roles and avatars from it; participants can't edit it.
type participantMetadata struct {
	Role      string     `json:"role"`
	UserID    *uuid.UUID `json:"user_id,omitempty"`
	AvatarURL *string    `json:"avatar_url,omitempty"`
}

func (p participantMetadata) String() string {
	b, _ := json.Marshal(p)
	return string(b)
}

// join: POST /calls/meetings/{id}/join → JoinToken for people who may join
// without asking (incl. those a host admitted).
func (s *Service) join(w http.ResponseWriter, r *http.Request) error {
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
		return joinRefusal(a)
	}
	var avatar *string
	var name string
	if err := s.d.Pool.QueryRow(ctx, `SELECT display_name, avatar_url FROM users WHERE id = $1`, me.UserID).Scan(&name, &avatar); err != nil {
		return err
	}
	tok, err := s.mint(ctx, m, userIdentity(me.UserID), name, participantMetadata{Role: a.Role, UserID: &me.UserID, AvatarURL: avatar})
	if err != nil {
		return err
	}
	tok.Role = a.Role
	s.answered(ctx, m.ID, me.UserID)
	httpx.OK(w, tok)
	return nil
}

// joinRefusal is the error for someone who may not join directly.
func joinRefusal(a access) error {
	if a.Mode == ModeKnock {
		return httpx.NewError(http.StatusForbidden, "calls_knock_required", "Ask to join. A host lets you in.")
	}
	if a.Reason == ReasonCanceled {
		return errMeetingCanceled
	}
	e := httpx.NewError(http.StatusForbidden, "calls_not_allowed", reasonMessage(a.Reason))
	e.Details = map[string]any{"reason": a.Reason}
	return e
}

// mint opens the room's session and issues a token for identity.
func (s *Service) mint(ctx context.Context, m *meetingRow, identity, name string, meta participantMetadata) (JoinToken, error) {
	dctx, cancel := detached(ctx)
	defer cancel()
	if _, err := ensureSession(dctx, s.d.Pool, m.ID); err != nil {
		return JoinToken{}, err
	}
	room := m.ID.String()
	if err := s.media.EnsureRoom(dctx, room, RoomOptions{MaxParticipants: s.cfg.maxParticipants, EmptyTimeout: emptyRoomTimeout, DepartureTimeout: departureTimeout}); err != nil {
		slog.ErrorContext(ctx, "calls: media server unreachable", "provider", s.media.Name(), "err", err)
		return JoinToken{}, errMediaUnavailable
	}
	tok, exp, err := s.media.Token(Grant{Room: room, Identity: identity, Name: name, Metadata: meta.String(), TTL: s.cfg.tokenTTL})
	if err != nil {
		return JoinToken{}, err
	}
	return JoinToken{Token: tok, URL: s.media.ClientURL(), Room: room, Identity: identity, ExpiresAt: exp, Role: meta.Role, MeetingID: m.ID}, nil
}

// syncRole updates a live participant's role in the room (best-effort).
func (s *Service) syncRole(ctx context.Context, m *meetingRow, userID uuid.UUID, role string) {
	if s.media == nil || m.LiveSession == nil {
		return
	}
	var avatar *string
	_ = s.d.Pool.QueryRow(ctx, `SELECT avatar_url FROM users WHERE id = $1`, userID).Scan(&avatar)
	meta := participantMetadata{Role: role, UserID: &userID, AvatarURL: avatar}
	dctx, cancel := detached(ctx)
	defer cancel()
	if err := s.media.UpdateParticipant(dctx, m.ID.String(), userIdentity(userID), meta.String(), nil); err != nil && err != errMediaNotFound {
		slog.WarnContext(ctx, "calls: role sync failed", "meeting_id", m.ID, "err", err)
	}
}

// ---- helpers ------------------------------------------------------------------------

// meetingParam loads {id} (a meeting id).
func (s *Service) meetingParam(r *http.Request) (*meetingRow, error) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return nil, errMeetingNotFound
	}
	return loadMeeting(r.Context(), s.d.Pool, id)
}

// meetingAsHost loads {id} and requires the caller to be its host or a
// co-host. People who can't join get 404; others 403.
func (s *Service) meetingAsHost(r *http.Request, me *auth.Identity) (*meetingRow, access, error) {
	m, err := s.meetingParam(r)
	if err != nil {
		return nil, access{}, err
	}
	if m.CanceledAt != nil {
		if m.HostID == me.UserID {
			return m, access{Mode: ModeHost, Role: RoleHost}, nil
		}
		return nil, access{}, errMeetingCanceled
	}
	a, err := s.accessFor(r.Context(), s.d.Pool, m, me.UserID)
	if err != nil {
		return nil, access{}, err
	}
	switch {
	case a.isHost():
		return m, a, nil
	case a.canJoin() || a.Mode == ModeKnock:
		return nil, access{}, errNotHost
	}
	return nil, access{}, errMeetingNotFound
}

// errSourced refuses a change to a meeting another feature manages (its
// source: a calendar event's room, a channel's call). The UI offers "Open
// in …" instead; the API says the same.
func errSourced(m *meetingRow, action string) error {
	src := Source{Kind: *m.SourceKind}
	if m.SourceID != nil {
		src.ID = *m.SourceID
	}
	where := "the place that created it"
	switch feature, _, _ := strings.Cut(src.Kind, "."); feature {
	case "calendar":
		where = "its calendar event"
	case "channels":
		where = "its channel"
	case "":
	default:
		where = "its " + feature + " item"
	}
	e := httpx.NewError(http.StatusConflict, "calls_managed_by_source",
		"This meeting belongs to "+where+". "+action+" there.")
	e.Details = map[string]any{"source": src}
	return e
}

// revokeMeeting re-checks live subscriptions to calls:<id> after a change
// that may keep people out (users, or everyone subscribed when none), so a
// person who can no longer join stops receiving the meeting's events.
func (s *Service) revokeMeeting(ctx context.Context, meetingID uuid.UUID, users ...uuid.UUID) {
	if s.d.Broker == nil {
		return
	}
	topic := meetingTopic(meetingID)
	if len(users) == 0 {
		if err := realtime.RevokeTopic(ctx, s.d.Broker, topic); err != nil {
			slog.WarnContext(ctx, "calls: realtime revoke failed", "meeting_id", meetingID, "err", err)
		}
		return
	}
	for _, u := range users {
		if err := realtime.Revoke(ctx, s.d.Broker, u, topic); err != nil {
			slog.WarnContext(ctx, "calls: realtime revoke failed", "meeting_id", meetingID, "err", err)
		}
	}
}
