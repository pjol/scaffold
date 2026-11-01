package calls

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
)

// The exported seams other features and stitching code use. They run the
// same validation as the REST routes; they don't check who is calling (the
// caller feature has done that) and send no notifications (the caller sends
// its own invitations), except StartCall, which rings.

// MeetingInput describes a meeting to create.
type MeetingInput struct {
	HostID       uuid.UUID
	OrgID        *uuid.UUID // required for AccessOrg
	Title        string
	StartsAt     *time.Time // nil = an unscheduled link
	EndsAt       *time.Time
	Access       string // AccessOrg | AccessInvited | AccessOpen ("" = AccessOpen)
	LobbyEnabled *bool  // nil = on
	InviteeIDs   []uuid.UUID
	CohostIDs    []uuid.UUID
	Source       *Source // the record this meeting belongs to (unique while active)

	kind string // KindCall for instant calls
}

// MeetingUpdate changes a meeting; nil fields stay as they are.
type MeetingUpdate struct {
	Title        *string
	Access       *string
	LobbyEnabled *bool
	Locked       *bool
	StartsAt     *time.Time
	EndsAt       *time.Time
	ClearTimes   bool         // drop the schedule (an unscheduled link)
	InviteeIDs   *[]uuid.UUID // replace the invitees (co-hosts stay)
}

// CallOptions configure StartCall.
type CallOptions struct {
	OrgID  *uuid.UUID
	Title  string
	Source *Source
}

var errNotBuilt = errors.New("calls: module not constructed (is feature_calls.go installed?)")

// CreateMeeting creates a meeting in q (e.g. inside a calendar booking's
// transaction) and returns it with its code and join URL.
func CreateMeeting(ctx context.Context, q db.Querier, in MeetingInput) (Meeting, error) {
	if current.Load() == nil {
		return Meeting{}, errNotBuilt
	}
	return createMeeting(ctx, q, in)
}

// UpdateMeeting changes a meeting's title, schedule, access or invitees.
// Live subscriptions to the meeting are re-checked when it returns, so
// people it leaves out stop receiving events; inside a transaction that
// re-check may run before the commit, so prefer calling it outside one.
func UpdateMeeting(ctx context.Context, q db.Querier, meetingID uuid.UUID, u MeetingUpdate) (Meeting, error) {
	s := current.Load()
	if s == nil {
		return Meeting{}, errNotBuilt
	}
	m, err := updateMeeting(ctx, q, meetingID, u)
	if err == nil {
		s.revokeMeeting(ctx, meetingID)
	}
	return m, err
}

// CancelMeeting cancels a meeting: its link stops working and pending knocks
// and rings end. A live room is closed by the module's worker within a
// minute (this may run inside the caller's transaction; live subscriptions
// are re-checked when it returns, as for UpdateMeeting).
func CancelMeeting(ctx context.Context, q db.Querier, meetingID uuid.UUID) error {
	if err := cancelMeeting(ctx, q, meetingID); err != nil {
		return err
	}
	if s := current.Load(); s != nil {
		s.revokeMeeting(ctx, meetingID)
	}
	return nil
}

// GetMeeting returns a meeting by id.
func GetMeeting(ctx context.Context, q db.Querier, meetingID uuid.UUID) (Meeting, error) {
	s := current.Load()
	if s == nil {
		return Meeting{}, errNotBuilt
	}
	m, err := loadMeeting(ctx, q, meetingID)
	if err != nil {
		return Meeting{}, err
	}
	return s.toMeeting(m), nil
}

// MeetingBySource returns the active (not canceled) meeting created for a
// source record, or a not-found *httpx.Error.
func MeetingBySource(ctx context.Context, q db.Querier, kind, id string) (Meeting, error) {
	s := current.Load()
	if s == nil {
		return Meeting{}, errNotBuilt
	}
	var mid uuid.UUID
	err := q.QueryRow(ctx, `SELECT id FROM calls_meetings WHERE source_kind = $1 AND source_id = $2 AND canceled_at IS NULL`, kind, id).Scan(&mid)
	if db.IsNoRows(err) {
		return Meeting{}, errMeetingNotFound
	}
	if err != nil {
		return Meeting{}, err
	}
	return GetMeeting(ctx, q, mid)
}

// CanJoin reports whether userID may join a meeting without asking (the
// rule the REST routes apply), for features that show a "Join" button.
func CanJoin(ctx context.Context, q db.Querier, meetingID, userID uuid.UUID) (bool, error) {
	s := current.Load()
	if s == nil {
		return false, errNotBuilt
	}
	m, err := loadMeeting(ctx, q, meetingID)
	if err != nil {
		return false, err
	}
	a, err := s.accessFor(ctx, q, m, userID)
	return err == nil && a.canJoin(), err
}

// StartCall starts an instant call from callerID that rings calleeIDs
// (1–16 people the call policy allows) and returns the meeting to open
// (/meet/<code>). It runs its own transaction and rings after commit.
func StartCall(ctx context.Context, callerID uuid.UUID, calleeIDs []uuid.UUID, o CallOptions) (Meeting, error) {
	s := current.Load()
	if s == nil {
		return Meeting{}, errNotBuilt
	}
	return s.startCallFor(ctx, callerID, calleeIDs, o)
}

// ---- call policy ------------------------------------------------------------------

// CallPolicy decides whether from may ring or invite to. Return an
// *httpx.Error to refuse with a message the person can act on.
type CallPolicy func(ctx context.Context, q db.Querier, from, to uuid.UUID) error

var callPolicy CallPolicy = shareAnOrg

// SetCallPolicy replaces the rule for who may ring or invite whom (call it
// from stitching code). The default allows people who share an organization.
func SetCallPolicy(p CallPolicy) {
	if p != nil {
		callPolicy = p
	}
}

func shareAnOrg(ctx context.Context, q db.Querier, from, to uuid.UUID) error {
	var ok bool
	err := q.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM org_members a JOIN org_members b ON b.org_id = a.org_id
		               WHERE a.user_id = $1 AND b.user_id = $2)`, from, to).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return httpx.Forbidden("You can only call or invite people in your " + orgs.Nouns() + ".")
	}
	return nil
}

// checkInvitable validates people from wants to invite or ring: active
// accounts the call policy allows. The caller themselves is skipped.
func (s *Service) checkInvitable(ctx context.Context, q db.Querier, from uuid.UUID, ids []uuid.UUID) error {
	ids = uniqueIDs(ids, from)
	if len(ids) == 0 {
		return nil
	}
	if len(ids) > maxInvitees {
		return httpx.BadRequest(fmt.Sprintf("Invite at most %d people.", maxInvitees))
	}
	rows, err := q.Query(ctx, `SELECT id FROM users WHERE id = ANY($1) AND status = 'active'`, ids)
	if err != nil {
		return err
	}
	found, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return err
	}
	if len(found) != len(ids) {
		return httpx.NotFound("Someone you chose isn't available.")
	}
	for _, id := range ids {
		if err := callPolicy(ctx, q, from, id); err != nil {
			return err
		}
	}
	return nil
}

func uniqueIDs(ids []uuid.UUID, skip uuid.UUID) []uuid.UUID {
	seen := map[uuid.UUID]bool{skip: true, uuid.Nil: true}
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// ---- implementations ------------------------------------------------------------------

func validAccess(a string) bool { return a == AccessOrg || a == AccessInvited || a == AccessOpen }

func checkTimes(start, end *time.Time) error {
	if end != nil && start == nil {
		return httpx.BadRequest("Set a start time before an end time.")
	}
	if start != nil && end != nil {
		if !end.After(*start) {
			return httpx.BadRequest("The meeting must end after it starts.")
		}
		if end.Sub(*start) > 24*time.Hour {
			return httpx.BadRequest("A meeting can last at most 24 hours.")
		}
	}
	return nil
}

func createMeeting(ctx context.Context, q db.Querier, in MeetingInput) (Meeting, error) {
	s := current.Load()
	if in.HostID == uuid.Nil {
		return Meeting{}, httpx.BadRequest("A meeting needs a host.")
	}
	if in.Access == "" {
		in.Access = AccessOpen
	}
	if !validAccess(in.Access) {
		return Meeting{}, httpx.BadRequest("access must be org, invited or open.")
	}
	if in.Access == AccessOrg && in.OrgID == nil {
		return Meeting{}, httpx.BadRequest("Choose " + orgs.NounA() + " for " + orgs.NounA() + "-only meeting.")
	}
	if err := checkTimes(in.StartsAt, in.EndsAt); err != nil {
		return Meeting{}, err
	}
	if in.Source != nil && (strings.TrimSpace(in.Source.Kind) == "" || strings.TrimSpace(in.Source.ID) == "") {
		return Meeting{}, httpx.BadRequest("A source needs a kind and an id.")
	}
	kind := in.kind
	if kind == "" {
		kind = KindMeeting
	}
	lobby := true
	if in.LobbyEnabled != nil {
		lobby = *in.LobbyEnabled
	}
	cohosts := uniqueIDs(in.CohostIDs, in.HostID)
	invited := uniqueIDs(append(in.InviteeIDs, cohosts...), in.HostID)
	if len(invited) > maxInvitees {
		return Meeting{}, httpx.BadRequest(fmt.Sprintf("A meeting can have at most %d invitees.", maxInvitees))
	}
	var srcKind, srcID *string
	if in.Source != nil {
		srcKind, srcID = &in.Source.Kind, &in.Source.ID
	}
	var id uuid.UUID
	for attempt := 0; ; attempt++ {
		err := q.QueryRow(ctx, `
			INSERT INTO calls_meetings (code, kind, title, host_id, org_id, access, lobby_enabled, starts_at, ends_at, source_kind, source_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) RETURNING id`,
			newCode(), kind, trimTitle(in.Title), in.HostID, in.OrgID, in.Access, lobby, in.StartsAt, in.EndsAt, srcKind, srcID,
		).Scan(&id)
		if db.IsUniqueViolation(err, "calls_meetings_code_key") && attempt < 5 {
			continue
		}
		if db.IsUniqueViolation(err, "calls_meetings_source_idx") {
			return Meeting{}, httpx.Conflict("That record already has a meeting.")
		}
		if err != nil {
			return Meeting{}, err
		}
		break
	}
	if len(invited) > 0 {
		isCohost := map[uuid.UUID]bool{}
		for _, c := range cohosts {
			isCohost[c] = true
		}
		roles := make([]string, len(invited))
		for i, u := range invited {
			roles[i] = RoleInvitee
			if isCohost[u] {
				roles[i] = RoleCohost
			}
		}
		if _, err := q.Exec(ctx, `
			INSERT INTO calls_invitees (meeting_id, user_id, role, invited_by)
			SELECT $1, u, r, $4 FROM unnest($2::uuid[], $3::text[]) AS t(u, r)`, id, invited, roles, in.HostID); err != nil {
			return Meeting{}, err
		}
	}
	m, err := loadMeeting(ctx, q, id)
	if err != nil {
		return Meeting{}, err
	}
	return s.toMeeting(m), nil
}

func updateMeeting(ctx context.Context, q db.Querier, id uuid.UUID, u MeetingUpdate) (Meeting, error) {
	s := current.Load()
	m, err := loadMeeting(ctx, q, id)
	if err != nil {
		return Meeting{}, err
	}
	if m.CanceledAt != nil {
		return Meeting{}, errMeetingCanceled
	}
	title, access, lobby, locked, start, end := m.Title, m.Access, m.LobbyEnabled, m.Locked, m.StartsAt, m.EndsAt
	if u.Title != nil {
		title = trimTitle(*u.Title)
	}
	if u.Access != nil {
		if !validAccess(*u.Access) {
			return Meeting{}, httpx.BadRequest("access must be org, invited or open.")
		}
		access = *u.Access
	}
	if access == AccessOrg && m.OrgID == nil {
		return Meeting{}, httpx.BadRequest("Only meetings in " + orgs.NounA() + " can be " + orgs.Noun() + "-only.")
	}
	if u.LobbyEnabled != nil {
		lobby = *u.LobbyEnabled
	}
	if u.Locked != nil {
		locked = *u.Locked
	}
	if u.ClearTimes {
		start, end = nil, nil
	}
	if u.StartsAt != nil {
		start = u.StartsAt
	}
	if u.EndsAt != nil {
		end = u.EndsAt
	}
	if err := checkTimes(start, end); err != nil {
		return Meeting{}, err
	}
	if _, err := q.Exec(ctx, `
		UPDATE calls_meetings SET title = $2, access = $3, lobby_enabled = $4, locked = $5, starts_at = $6, ends_at = $7, updated_at = now()
		WHERE id = $1`, id, title, access, lobby, locked, start, end); err != nil {
		return Meeting{}, err
	}
	if u.InviteeIDs != nil {
		ids := uniqueIDs(*u.InviteeIDs, m.HostID)
		if len(ids) > maxInvitees {
			return Meeting{}, httpx.BadRequest(fmt.Sprintf("A meeting can have at most %d invitees.", maxInvitees))
		}
		if _, err := q.Exec(ctx, `DELETE FROM calls_invitees WHERE meeting_id = $1 AND role = 'invitee' AND NOT (user_id = ANY($2))`, id, ids); err != nil {
			return Meeting{}, err
		}
		if _, err := q.Exec(ctx, `
			INSERT INTO calls_invitees (meeting_id, user_id, role, invited_by)
			SELECT $1, u, 'invitee', $3 FROM unnest($2::uuid[]) AS u
			ON CONFLICT (meeting_id, user_id) DO NOTHING`, id, ids, m.HostID); err != nil {
			return Meeting{}, err
		}
	}
	m, err = loadMeeting(ctx, q, id)
	if err != nil {
		return Meeting{}, err
	}
	return s.toMeeting(m), nil
}

func cancelMeeting(ctx context.Context, q db.Querier, id uuid.UUID) error {
	tag, err := q.Exec(ctx, `UPDATE calls_meetings SET canceled_at = now(), updated_at = now() WHERE id = $1 AND canceled_at IS NULL`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		var exists bool
		if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM calls_meetings WHERE id = $1)`, id).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return errMeetingNotFound
		}
		return nil
	}
	if _, err := q.Exec(ctx, `UPDATE calls_lobby SET status = 'expired', updated_at = now() WHERE meeting_id = $1 AND status IN ('waiting', 'admitted')`, id); err != nil {
		return err
	}
	_, err = q.Exec(ctx, `UPDATE calls_rings SET status = 'missed', updated_at = now() WHERE meeting_id = $1 AND status = 'ringing'`, id)
	return err
}
