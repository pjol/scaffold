package calls

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/db"
)

// UserRef is a person as calls shows them. A deleted account reads "Deleted user".
type UserRef struct {
	ID          uuid.UUID `json:"id"`
	DisplayName string    `json:"display_name"`
	AvatarURL   *string   `json:"avatar_url"`
	Deleted     bool      `json:"deleted"`
}

func userRef(id uuid.UUID, name string, avatar *string, status string) UserRef {
	if status == "deleted" {
		return UserRef{ID: id, DisplayName: "Deleted user", Deleted: true}
	}
	if strings.TrimSpace(name) == "" {
		name = "Member"
	}
	return UserRef{ID: id, DisplayName: name, AvatarURL: avatar}
}

// Source links a meeting to the record another feature created it for
// (a calendar event, a channel). Kind is that feature's name.
type Source struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// Meeting is a meeting link and its settings.
type Meeting struct {
	ID           uuid.UUID  `json:"id"`
	Code         string     `json:"code"`
	JoinURL      string     `json:"join_url"`
	Kind         string     `json:"kind"` // meeting | call
	Title        string     `json:"title"`
	Host         UserRef    `json:"host"`
	OrgID        *uuid.UUID `json:"org_id"`
	OrgName      *string    `json:"org_name"`
	Access       string     `json:"access"` // org | invited | open
	LobbyEnabled bool       `json:"lobby_enabled"`
	Locked       bool       `json:"locked"`
	StartsAt     *time.Time `json:"starts_at"`
	EndsAt       *time.Time `json:"ends_at"`
	Source       *Source    `json:"source"`
	CanceledAt   *time.Time `json:"canceled_at"`
	LiveSince    *time.Time `json:"live_since"` // set while someone is in the call
	CreatedAt    time.Time  `json:"created_at"`
}

// Invitee is someone invited by account; role cohost shares host controls.
type Invitee struct {
	User UserRef `json:"user"`
	Role string  `json:"role"` // invitee | cohost
}

// MeetingDetail is a meeting from the point of view of someone allowed in it.
type MeetingDetail struct {
	Meeting
	Role     string    `json:"role"` // the caller's: host | cohost | invitee | member
	Invitees []Invitee `json:"invitees"`
}

// JoinInfo answers "can I join this?" for the pre-join screen. Title and
// host show to people who may join or knock; Meeting only to those who may
// join without asking.
type JoinInfo struct {
	MeetingID uuid.UUID      `json:"meeting_id"`
	Code      string         `json:"code"`
	Kind      string         `json:"kind"`
	Mode      string         `json:"mode"`   // host | cohost | direct | knock | denied
	Reason    *string        `json:"reason"` // denied: org_only | invited_only | locked | removed | canceled
	Title     *string        `json:"title"`
	HostName  *string        `json:"host_name"`
	Live      bool           `json:"live"`
	Meeting   *MeetingDetail `json:"meeting"`
	Lobby     *LobbyEntry    `json:"lobby"` // the caller's open request to join
}

// PublicMeeting is what a guest without an account sees. Title and host show
// only for open meetings.
type PublicMeeting struct {
	Code          string  `json:"code"`
	Title         *string `json:"title"`
	HostName      *string `json:"host_name"`
	GuestsAllowed bool    `json:"guests_allowed"`
	LobbyEnabled  bool    `json:"lobby_enabled"`
	Live          bool    `json:"live"`
	Canceled      bool    `json:"canceled"`
	Locked        bool    `json:"locked"`
}

// JoinToken is a short-lived SFU credential for one room.
type JoinToken struct {
	Token     string    `json:"token"`
	URL       string    `json:"url"`
	Room      string    `json:"room"`
	Identity  string    `json:"identity"`
	ExpiresAt time.Time `json:"expires_at"`
	Role      string    `json:"role"` // host | cohost | invitee | member | guest
	MeetingID uuid.UUID `json:"meeting_id"`
}

// LobbyEntry is one request to join.
type LobbyEntry struct {
	ID        uuid.UUID  `json:"id"`
	MeetingID uuid.UUID  `json:"meeting_id"`
	User      *UserRef   `json:"user"` // nil for guests
	Name      string     `json:"name"` // what hosts see
	Guest     bool       `json:"guest"`
	Status    string     `json:"status"` // waiting | admitted | denied | canceled | expired | removed
	CreatedAt time.Time  `json:"created_at"`
	DecidedAt *time.Time `json:"decided_at"`
}

// GuestKnock is a guest's new lobby entry with the secret key they keep to
// poll it and to join once admitted. The key is shown only here.
type GuestKnock struct {
	LobbyEntry
	Key string `json:"key"`
}

// Ring is an instant call ringing one person.
type Ring struct {
	ID        uuid.UUID `json:"id"`
	MeetingID uuid.UUID `json:"meeting_id"`
	Code      string    `json:"code"`
	Title     string    `json:"title"`
	Caller    UserRef   `json:"caller"`
	Callee    UserRef   `json:"callee"`
	Others    []UserRef `json:"others"` // everyone else rung with this person
	Status    string    `json:"status"` // ringing | accepted | declined | missed
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// RingEnded tells a callee's devices to stop ringing.
type RingEnded struct {
	RingID    uuid.UUID `json:"ring_id"`
	MeetingID uuid.UUID `json:"meeting_id"`
	Status    string    `json:"status"`
}

// MeetingRef is the part of a meeting history rows show.
type MeetingRef struct {
	ID    uuid.UUID `json:"id"`
	Code  string    `json:"code"`
	Title string    `json:"title"`
	Kind  string    `json:"kind"`
}

// Person is someone who was in a call: an account or a guest by name.
type Person struct {
	User  *UserRef `json:"user"`
	Name  string   `json:"name"`
	Guest bool     `json:"guest"`
}

// HistoryItem is a call you were in, or one you missed.
type HistoryItem struct {
	ID               uuid.UUID  `json:"id"`   // session id (call) or ring id (missed)
	Type             string     `json:"type"` // call | missed
	Meeting          MeetingRef `json:"meeting"`
	StartedAt        time.Time  `json:"started_at"`
	EndedAt          *time.Time `json:"ended_at"`
	DurationSeconds  int        `json:"duration_seconds"`
	Live             bool       `json:"live"`
	Participants     []Person   `json:"participants"` // up to 6
	ParticipantCount int        `json:"participant_count"`
	Caller           *UserRef   `json:"caller"`      // missed: who called
	RingStatus       *string    `json:"ring_status"` // missed: missed | declined
}

// Attendance is one person's time in a call.
type Attendance struct {
	Person
	Identity      string     `json:"identity"`
	FirstJoinedAt time.Time  `json:"first_joined_at"`
	LastLeftAt    *time.Time `json:"last_left_at"` // nil while still in the call
	Seconds       int        `json:"seconds"`
	Joins         int        `json:"joins"`
}

// CallDetail is one past or live call.
type CallDetail struct {
	ID              uuid.UUID    `json:"id"`
	Meeting         MeetingRef   `json:"meeting"`
	Host            UserRef      `json:"host"`
	StartedAt       time.Time    `json:"started_at"`
	EndedAt         *time.Time   `json:"ended_at"`
	DurationSeconds int          `json:"duration_seconds"`
	Live            bool         `json:"live"`
	Participants    []Attendance `json:"participants"`
	CanJoin         bool         `json:"can_join"`
}

// ---- meeting rows --------------------------------------------------------------

// meetingRow is a calls_meetings row with the host and org joined.
type meetingRow struct {
	ID           uuid.UUID
	Code         string
	Kind         string
	Title        string
	HostID       uuid.UUID
	HostName     string
	HostAvatar   *string
	HostStatus   string
	OrgID        *uuid.UUID
	OrgName      *string
	Access       string
	LobbyEnabled bool
	Locked       bool
	StartsAt     *time.Time
	EndsAt       *time.Time
	SourceKind   *string
	SourceID     *string
	CanceledAt   *time.Time
	CreatedAt    time.Time
	LiveSession  *uuid.UUID // the open session (a room was opened for it)
	LiveSince    *time.Time
	LiveNow      bool // someone is in the room right now
}

const meetingSelect = `
	SELECT m.id, m.code, m.kind, m.title, m.host_id, u.display_name, u.avatar_url, u.status,
	       m.org_id, o.name, m.access, m.lobby_enabled, m.locked, m.starts_at, m.ends_at,
	       m.source_kind, m.source_id, m.canceled_at, m.created_at, ls.id, ls.started_at,
	       ls.id IS NOT NULL AND EXISTS (SELECT 1 FROM calls_participants p WHERE p.session_id = ls.id AND p.left_at IS NULL)
	FROM calls_meetings m
	JOIN users u ON u.id = m.host_id
	LEFT JOIN orgs o ON o.id = m.org_id
	LEFT JOIN calls_sessions ls ON ls.meeting_id = m.id AND ls.ended_at IS NULL`

func scanMeetings(rows pgx.Rows) ([]meetingRow, error) {
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[meetingRow])
	if list == nil {
		list = []meetingRow{}
	}
	return list, err
}

func loadMeeting(ctx context.Context, q db.Querier, id uuid.UUID) (*meetingRow, error) {
	rows, err := q.Query(ctx, meetingSelect+` WHERE m.id = $1`, id)
	if err != nil {
		return nil, err
	}
	list, err := scanMeetings(rows)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, errMeetingNotFound
	}
	return &list[0], nil
}

// loadMeetingRef resolves a meeting id or a code (abc-defg-hij, any case,
// dashes optional).
func loadMeetingRef(ctx context.Context, q db.Querier, ref string) (*meetingRow, error) {
	if id, err := uuid.Parse(ref); err == nil {
		return loadMeeting(ctx, q, id)
	}
	code, ok := normalizeCode(ref)
	if !ok {
		return nil, errMeetingNotFound
	}
	rows, err := q.Query(ctx, meetingSelect+` WHERE m.code = $1`, code)
	if err != nil {
		return nil, err
	}
	list, err := scanMeetings(rows)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, errMeetingNotFound
	}
	return &list[0], nil
}

func (s *Service) toMeeting(m *meetingRow) Meeting {
	out := Meeting{
		ID: m.ID, Code: m.Code, JoinURL: s.joinURL(m.Code), Kind: m.Kind, Title: m.Title,
		Host:  userRef(m.HostID, m.HostName, m.HostAvatar, m.HostStatus),
		OrgID: m.OrgID, OrgName: m.OrgName, Access: m.Access, LobbyEnabled: m.LobbyEnabled, Locked: m.Locked,
		StartsAt: m.StartsAt, EndsAt: m.EndsAt, CanceledAt: m.CanceledAt, CreatedAt: m.CreatedAt,
	}
	if m.LiveNow {
		out.LiveSince = m.LiveSince
	}
	if m.SourceKind != nil && m.SourceID != nil {
		out.Source = &Source{Kind: *m.SourceKind, ID: *m.SourceID}
	}
	return out
}

// invitees lists a meeting's invitees, co-hosts first.
func invitees(ctx context.Context, q db.Querier, meetingID uuid.UUID) ([]Invitee, error) {
	rows, err := q.Query(ctx, `
		SELECT i.user_id, u.display_name, u.avatar_url, u.status, i.role
		FROM calls_invitees i JOIN users u ON u.id = i.user_id
		WHERE i.meeting_id = $1
		ORDER BY (i.role = 'cohost') DESC, u.display_name, i.user_id`, meetingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Invitee{}
	for rows.Next() {
		var id uuid.UUID
		var name, status, role string
		var avatar *string
		if err := rows.Scan(&id, &name, &avatar, &status, &role); err != nil {
			return nil, err
		}
		out = append(out, Invitee{User: userRef(id, name, avatar, status), Role: role})
	}
	return out, rows.Err()
}

func (s *Service) detail(ctx context.Context, q db.Querier, m *meetingRow, role string) (MeetingDetail, error) {
	inv, err := invitees(ctx, q, m.ID)
	if err != nil {
		return MeetingDetail{}, err
	}
	return MeetingDetail{Meeting: s.toMeeting(m), Role: role, Invitees: inv}, nil
}

// lookupUsers loads people by id (deleted accounts read "Deleted user").
func lookupUsers(ctx context.Context, q db.Querier, ids []uuid.UUID) (map[uuid.UUID]UserRef, error) {
	out := map[uuid.UUID]UserRef{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := q.Query(ctx, `SELECT id, display_name, avatar_url, status FROM users WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var name, status string
		var avatar *string
		if err := rows.Scan(&id, &name, &avatar, &status); err != nil {
			return nil, err
		}
		out[id] = userRef(id, name, avatar, status)
	}
	return out, rows.Err()
}
