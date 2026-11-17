package calls

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/audit"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
)

// The room's shared state lives in server-owned room metadata, so every
// participant (guests too, who have no realtime connection) sees the same
// recording notice, spotlight, rules and breakout rooms, and nobody in the
// room can change it. Changes that need a refetch (polls, questions) and
// one-off messages (a broadcast, "please unmute") go out as server-sent
// data messages on SignalTopic.

// SignalTopic is the data-channel topic of server-sent signals.
const SignalTopic = "calls.signal"

// Signal types (signal.Type).
const (
	SignalPolls      = "polls"      // polls changed: refetch the room
	SignalQuestions  = "questions"  // questions changed: refetch the room
	SignalBroadcast  = "broadcast"  // a host's message to every room {text, from}
	SignalAskUnmute  = "ask_unmute" // a host asks you to unmute {from}
	SignalHelp       = "help"       // someone in a breakout room asks a host for help {from, room_id}
	SignalAssignment = "breakout"   // breakout assignments changed: refetch your room
)

type signal struct {
	Type   string     `json:"type"`
	Text   string     `json:"text,omitempty"`
	From   string     `json:"from,omitempty"`
	RoomID *uuid.UUID `json:"room_id,omitempty"`
}

// RoomMeta is the room metadata clients read (parse it, never trust it for
// access: the server checks every action).
type RoomMeta struct {
	V            int            `json:"v"`
	MeetingID    uuid.UUID      `json:"meeting_id"`
	Recording    *RoomRecording `json:"recording"`    // being recorded now
	Transcribing bool           `json:"transcribing"` // the transcript is being saved
	Captions     bool           `json:"captions"`     // the workspace allows browser captions
	Transcripts  bool           `json:"transcripts"`  // hosts may save the transcript
	CanRecord    bool           `json:"can_record"`   // the server and workspace allow recording
	Spotlight    string         `json:"spotlight"`    // identity everyone sees large ("" = none)
	Rules        RoomRules      `json:"rules"`
	Breakout     *RoomBreakout  `json:"breakout"` // the open breakout rooms (in a breakout room: Room is yours)
}

// RoomRules mirror MeetingRules for people in the room.
type RoomRules struct {
	ScreenShare bool `json:"screen_share"`
	Chat        bool `json:"chat"`
	Reactions   bool `json:"reactions"`
	Unmute      bool `json:"unmute"`
	MuteOnEntry bool `json:"mute_on_entry"`
	SaveChat    bool `json:"save_chat"`
}

// RoomRecording is the recording in progress.
type RoomRecording struct {
	ID        uuid.UUID `json:"id"`
	Status    string    `json:"status"`
	StartedAt time.Time `json:"started_at"`
}

// RoomBreakout is the open set of breakout rooms.
type RoomBreakout struct {
	ID          uuid.UUID     `json:"id"`
	Status      string        `json:"status"` // open | closing
	EndsAt      *time.Time    `json:"ends_at"`
	ClosingAt   *time.Time    `json:"closing_at"`
	AllowChoose bool          `json:"allow_choose"`
	Rooms       []BreakoutRef `json:"rooms"`
	Room        *BreakoutRef  `json:"room,omitempty"` // set in a breakout room's own metadata
}

// BreakoutRef names one breakout room.
type BreakoutRef struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// OrgCallSettings is what a workspace allows in its calls.
type OrgCallSettings struct {
	OrgID       uuid.UUID `json:"org_id"`
	Captions    bool      `json:"captions"`
	Transcripts bool      `json:"transcripts"`
	Recording   bool      `json:"recording"`
	CanManage   bool      `json:"can_manage"` // the caller is a workspace owner or admin
	// RecordingAvailable: this server can record (egress and storage set up).
	RecordingAvailable bool `json:"recording_available"`
}

// orgSettings reads a workspace's call settings (defaults when unset;
// meetings in no workspace get no captions or transcripts).
func orgSettings(ctx context.Context, q db.Querier, orgID *uuid.UUID) (OrgCallSettings, error) {
	if orgID == nil {
		return OrgCallSettings{Recording: true}, nil
	}
	out := OrgCallSettings{OrgID: *orgID, Recording: true}
	err := q.QueryRow(ctx, `SELECT captions, transcripts, recording FROM calls_org_settings WHERE org_id = $1`, *orgID).
		Scan(&out.Captions, &out.Transcripts, &out.Recording)
	if db.IsNoRows(err) {
		return out, nil
	}
	return out, err
}

// roomMeta builds a meeting's room metadata from the database.
func (s *Service) roomMeta(ctx context.Context, q db.Querier, m *meetingRow) (RoomMeta, error) {
	org, err := orgSettings(ctx, q, m.OrgID)
	if err != nil {
		return RoomMeta{}, err
	}
	r := m.Rules
	meta := RoomMeta{V: 1, MeetingID: m.ID, Captions: org.Captions, Transcripts: org.Transcripts,
		CanRecord: org.Recording && s.recordingAvailable(),
		Rules: RoomRules{ScreenShare: r.AllowScreenShare, Chat: r.AllowChat, Reactions: r.AllowReactions, Unmute: r.AllowUnmute,
			MuteOnEntry: r.MuteOnEntry, SaveChat: r.SaveChat}}
	var rec RoomRecording
	err = q.QueryRow(ctx, `SELECT id, status, started_at FROM calls_recordings WHERE meeting_id = $1 AND status IN ('starting', 'recording')`, m.ID).
		Scan(&rec.ID, &rec.Status, &rec.StartedAt)
	if err == nil {
		meta.Recording = &rec
	} else if !db.IsNoRows(err) {
		return RoomMeta{}, err
	}
	if m.LiveSession == nil {
		return meta, nil
	}
	var spotlight *string
	if err := q.QueryRow(ctx, `SELECT spotlight, transcribing FROM calls_sessions WHERE id = $1`, *m.LiveSession).Scan(&spotlight, &meta.Transcribing); err != nil && !db.IsNoRows(err) {
		return RoomMeta{}, err
	}
	meta.Spotlight = deref(spotlight)
	meta.Transcribing = meta.Transcribing && org.Transcripts
	b, err := openBreakout(ctx, q, *m.LiveSession)
	if err != nil {
		return RoomMeta{}, err
	}
	meta.Breakout = b
	return meta, nil
}

func (m RoomMeta) json() string {
	b, _ := json.Marshal(m)
	return string(b)
}

// syncRoom pushes a meeting's room metadata to its live room and open
// breakout rooms (best-effort: clients also refetch on reconnect).
func (s *Service) syncRoom(ctx context.Context, meetingID uuid.UUID) {
	rc, ok := s.media.(RoomController)
	if !ok {
		return
	}
	m, err := loadMeeting(ctx, s.d.Pool, meetingID)
	if err != nil || m.LiveSession == nil {
		return
	}
	meta, err := s.roomMeta(ctx, s.d.Pool, m)
	if err != nil {
		slog.WarnContext(ctx, "calls: room state", "meeting_id", meetingID, "err", err)
		return
	}
	dctx, cancel := detached(ctx)
	defer cancel()
	if err := rc.UpdateRoomMetadata(dctx, meetingID.String(), meta.json()); err != nil && err != errMediaNotFound {
		slog.WarnContext(ctx, "calls: room metadata", "meeting_id", meetingID, "err", err)
	}
	if meta.Breakout != nil {
		for _, room := range meta.Breakout.Rooms {
			bm := breakoutMeta(meta, room)
			if err := rc.UpdateRoomMetadata(dctx, room.ID.String(), bm.json()); err != nil && err != errMediaNotFound {
				slog.WarnContext(ctx, "calls: breakout room metadata", "room_id", room.ID, "err", err)
			}
		}
	}
}

// breakoutMeta is a breakout room's own metadata: its room set, and no
// recording or transcript (the recording is of the main room; lines from
// breakout rooms would interleave in one transcript).
func breakoutMeta(meta RoomMeta, room BreakoutRef) RoomMeta {
	bm := meta
	if meta.Breakout != nil {
		b := *meta.Breakout
		b.Room = &room
		bm.Breakout = &b
	}
	bm.Recording, bm.Transcribing = nil, false
	return bm
}

// signalRoom sends a signal to everyone in the meeting's room and its
// open breakout rooms (identities: only them).
func (s *Service) signalRoom(ctx context.Context, m *meetingRow, sig signal, identities ...string) {
	rc, ok := s.media.(RoomController)
	if !ok || m.LiveSession == nil {
		return
	}
	payload, _ := json.Marshal(sig)
	rooms := []string{m.ID.String()}
	if b, err := openBreakout(ctx, s.d.Pool, *m.LiveSession); err == nil && b != nil {
		for _, r := range b.Rooms {
			rooms = append(rooms, r.ID.String())
		}
	}
	dctx, cancel := detached(ctx)
	defer cancel()
	for _, room := range rooms {
		if err := rc.SendData(dctx, room, SignalTopic, payload, identities); err != nil {
			slog.WarnContext(ctx, "calls: room signal", "meeting_id", m.ID, "type", sig.Type, "err", err)
		}
	}
}

// publishSources is what identity may publish under the meeting's rules.
func publishSources(m *meetingRow, host bool, identity string, speakers []string) []string {
	if host {
		return allSources
	}
	out := []string{SourceCamera}
	if m.Rules.AllowUnmute || slices.Contains(speakers, identity) {
		out = append(out, SourceMicrophone)
	}
	if m.Rules.AllowScreenShare {
		out = append(out, SourceScreenShare, SourceScreenShareAudio)
	}
	return out
}

func sessionSpeakers(ctx context.Context, q db.Querier, sessionID *uuid.UUID) ([]string, error) {
	if sessionID == nil {
		return nil, nil
	}
	var out []string
	err := q.QueryRow(ctx, `SELECT speakers FROM calls_sessions WHERE id = $1`, *sessionID).Scan(&out)
	if db.IsNoRows(err) {
		return nil, nil
	}
	return out, err
}

// applyPermissions re-applies the rules to everyone in the room who isn't
// a host (presenting and unmuting are enforced by the media server).
func (s *Service) applyPermissions(ctx context.Context, meetingID uuid.UUID, only ...string) {
	rc, ok := s.media.(RoomController)
	if !ok {
		return
	}
	m, err := loadMeeting(ctx, s.d.Pool, meetingID)
	if err != nil || m.LiveSession == nil {
		return
	}
	hosts, err := hostIDs(ctx, s.d.Pool, m)
	if err != nil {
		return
	}
	isHost := map[string]bool{}
	for _, id := range hosts {
		isHost[userIdentity(id)] = true
	}
	speakers, err := sessionSpeakers(ctx, s.d.Pool, m.LiveSession)
	if err != nil {
		return
	}
	dctx, cancel := detached(ctx)
	defer cancel()
	rooms := []string{m.ID.String()}
	if b, err := openBreakout(ctx, s.d.Pool, *m.LiveSession); err == nil && b != nil {
		for _, r := range b.Rooms {
			rooms = append(rooms, r.ID.String())
		}
	}
	for _, room := range rooms {
		people, err := s.media.ListParticipants(dctx, room)
		if err != nil {
			slog.WarnContext(ctx, "calls: list participants for permissions", "meeting_id", m.ID, "err", err)
			continue
		}
		for _, p := range people {
			if isHost[p.Identity] || (len(only) > 0 && !slices.Contains(only, p.Identity)) {
				continue
			}
			if _, _, ok := parseIdentity(p.Identity); !ok {
				continue // a recorder or another server participant
			}
			if err := rc.UpdatePermissions(dctx, room, p.Identity, publishSources(m, false, p.Identity, speakers)); err != nil && err != errMediaNotFound {
				slog.WarnContext(ctx, "calls: permissions", "meeting_id", m.ID, "err", err)
			}
		}
	}
}

// ---- host controls ------------------------------------------------------------

// rulesReq is the part of PATCH /calls/meetings/{id} that sets the room's rules.
type rulesReq struct {
	AllowScreenShare  *bool   `json:"allow_screen_share"`
	AllowChat         *bool   `json:"allow_chat"`
	AllowReactions    *bool   `json:"allow_reactions"`
	AllowUnmute       *bool   `json:"allow_unmute"`
	MuteOnEntry       *bool   `json:"mute_on_entry"`
	SaveChat          *bool   `json:"save_chat"`
	ModerateQuestions *bool   `json:"moderate_questions"`
	RecordsAccess     *string `json:"records_access"`
}

func (r rulesReq) any() bool {
	return r.AllowScreenShare != nil || r.AllowChat != nil || r.AllowReactions != nil || r.AllowUnmute != nil ||
		r.MuteOnEntry != nil || r.SaveChat != nil || r.ModerateQuestions != nil || r.RecordsAccess != nil
}

// updateRules writes the rules inside the PATCH transaction.
func updateRules(ctx context.Context, tx pgx.Tx, m *meetingRow, r rulesReq) error {
	if r.RecordsAccess != nil {
		switch *r.RecordsAccess {
		case "participants", "invited":
		case "org":
			if m.OrgID == nil {
				return httpx.BadRequest("Only meetings in " + orgs.NounA() + " can share records with the whole " + orgs.Noun() + ".")
			}
		default:
			return httpx.BadRequest("records_access must be participants, invited or org.")
		}
	}
	_, err := tx.Exec(ctx, `UPDATE calls_meetings SET
		allow_screen_share = COALESCE($2, allow_screen_share), allow_chat = COALESCE($3, allow_chat),
		allow_reactions = COALESCE($4, allow_reactions), allow_unmute = COALESCE($5, allow_unmute),
		mute_on_entry = COALESCE($6, mute_on_entry), save_chat = COALESCE($7, save_chat),
		moderate_questions = COALESCE($8, moderate_questions), records_access = COALESCE($9, records_access), updated_at = now()
		WHERE id = $1`, m.ID, r.AllowScreenShare, r.AllowChat, r.AllowReactions, r.AllowUnmute, r.MuteOnEntry, r.SaveChat,
		r.ModerateQuestions, r.RecordsAccess)
	return err
}

// spotlight: PUT /calls/meetings/{id}/spotlight {identity} (hosts; "" clears).
// Everyone sees that person large.
func (s *Service) spotlight(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	m, _, err := s.liveMeetingAsHost(r, me)
	if err != nil {
		return err
	}
	if _, ok := s.media.(RoomController); !ok {
		return errNoRoomControl
	}
	var req struct {
		Identity string `json:"identity"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	var v *string
	if req.Identity != "" {
		if _, _, ok := parseIdentity(req.Identity); !ok {
			return httpx.BadRequest("Choose who to spotlight.")
		}
		v = &req.Identity
	}
	if _, err := s.d.Pool.Exec(ctx, `UPDATE calls_sessions SET spotlight = $2 WHERE id = $1`, *m.LiveSession, v); err != nil {
		return err
	}
	s.syncRoom(ctx, m.ID)
	httpx.NoContent(w)
	return nil
}

// askUnmute: POST /calls/meetings/{id}/ask-unmute {identity} (hosts). The
// person is asked to unmute (hosts never unmute anyone); while unmuting is
// off for everyone, this also lets them.
func (s *Service) askUnmute(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	m, _, err := s.liveMeetingAsHost(r, me)
	if err != nil {
		return err
	}
	if _, ok := s.media.(RoomController); !ok {
		return errNoRoomControl
	}
	var req struct {
		Identity string `json:"identity"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if _, _, ok := parseIdentity(req.Identity); !ok {
		return httpx.BadRequest("Choose who to ask.")
	}
	if !m.Rules.AllowUnmute {
		if _, err := s.d.Pool.Exec(ctx, `UPDATE calls_sessions SET speakers = array_append(array_remove(speakers, $2), $2) WHERE id = $1`,
			*m.LiveSession, req.Identity); err != nil {
			return err
		}
		s.applyPermissions(ctx, m.ID, req.Identity)
	}
	s.signalRoom(ctx, m, signal{Type: SignalAskUnmute, From: me.DisplayName}, req.Identity)
	httpx.NoContent(w)
	return nil
}

// dropSpeakers takes people out of the "may unmute" list (a host muted
// them while unmuting is off for everyone).
func (s *Service) dropSpeakers(ctx context.Context, m *meetingRow, identities []string) {
	if m.Rules.AllowUnmute || m.LiveSession == nil || len(identities) == 0 {
		return
	}
	if _, err := s.d.Pool.Exec(ctx, `UPDATE calls_sessions SET speakers = ARRAY(SELECT unnest(speakers) EXCEPT SELECT unnest($2::text[])) WHERE id = $1`,
		*m.LiveSession, identities); err != nil {
		slog.WarnContext(ctx, "calls: drop speakers", "meeting_id", m.ID, "err", err)
		return
	}
	s.applyPermissions(ctx, m.ID, identities...)
}

// ---- workspace settings --------------------------------------------------------

func (s *Service) orgSettingsFor(r *http.Request, me *auth.Identity) (uuid.UUID, string, error) {
	orgID, err := uuid.Parse(chi.URLParam(r, "orgID"))
	if err != nil {
		return uuid.Nil, "", httpx.NotFound(orgs.NounTitle() + " not found.")
	}
	role, err := orgs.RoleOf(r.Context(), s.d.Pool, orgID, me.UserID)
	if err != nil {
		return uuid.Nil, "", err
	}
	if role == "" {
		return uuid.Nil, "", httpx.NotFound(orgs.NounTitle() + " not found.")
	}
	return orgID, role, nil
}

func canManageOrg(role string) bool { return role == "owner" || role == "admin" }

// getOrgSettings: GET /calls/orgs/{orgID}/settings (members).
func (s *Service) getOrgSettings(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	orgID, role, err := s.orgSettingsFor(r, me)
	if err != nil {
		return err
	}
	out, err := orgSettings(r.Context(), s.d.Pool, &orgID)
	if err != nil {
		return err
	}
	out.CanManage, out.RecordingAvailable = canManageOrg(role), s.recordingAvailable()
	httpx.OK(w, out)
	return nil
}

// putOrgSettings: PUT /calls/orgs/{orgID}/settings {captions?, transcripts?,
// recording?} (workspace owners and admins; audited). Live rooms in the
// workspace pick the change up.
func (s *Service) putOrgSettings(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	orgID, role, err := s.orgSettingsFor(r, me)
	if err != nil {
		return err
	}
	if !canManageOrg(role) {
		return httpx.Forbidden("Only " + orgs.Noun() + " owners and admins can change call settings.")
	}
	var req struct {
		Captions    *bool `json:"captions"`
		Transcripts *bool `json:"transcripts"`
		Recording   *bool `json:"recording"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	var out OrgCallSettings
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO calls_org_settings (org_id, captions, transcripts, recording, updated_by)
			VALUES ($1, COALESCE($2, false), COALESCE($3, false), COALESCE($4, true), $5)
			ON CONFLICT (org_id) DO UPDATE SET captions = COALESCE($2, calls_org_settings.captions),
			  transcripts = COALESCE($3, calls_org_settings.transcripts), recording = COALESCE($4, calls_org_settings.recording),
			  updated_by = $5, updated_at = now()
			RETURNING org_id, captions, transcripts, recording`, orgID, req.Captions, req.Transcripts, req.Recording, me.UserID).
			Scan(&out.OrgID, &out.Captions, &out.Transcripts, &out.Recording); err != nil {
			return err
		}
		if !out.Transcripts {
			if _, err := tx.Exec(ctx, `UPDATE calls_sessions ss SET transcribing = false FROM calls_meetings m
				WHERE m.id = ss.meeting_id AND m.org_id = $1 AND ss.ended_at IS NULL AND ss.transcribing`, orgID); err != nil {
				return err
			}
		}
		return audit.Log(ctx, tx, me.UserID, "calls.org_settings", "org", orgID.String(), out)
	})
	if err != nil {
		return err
	}
	out.CanManage, out.RecordingAvailable = true, s.recordingAvailable()
	rows, err := s.d.Pool.Query(ctx, `SELECT ss.meeting_id FROM calls_sessions ss JOIN calls_meetings m ON m.id = ss.meeting_id
		WHERE m.org_id = $1 AND ss.ended_at IS NULL LIMIT 500`, orgID)
	if err == nil {
		if ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID]); err == nil {
			for _, id := range ids {
				s.syncRoom(ctx, id)
			}
		}
	}
	httpx.OK(w, out)
	return nil
}
