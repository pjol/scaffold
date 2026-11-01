package calls

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/orgs"
)

// Join modes (JoinInfo.Mode).
const (
	ModeHost   = "host"
	ModeCohost = "cohost"
	ModeDirect = "direct" // joins without asking
	ModeKnock  = "knock"  // may ask to join; a host admits or denies
	ModeDenied = "denied"
)

// Reasons a person can't join (JoinInfo.Reason).
const (
	ReasonOrgOnly     = "org_only"
	ReasonInvitedOnly = "invited_only"
	ReasonLocked      = "locked"
	ReasonRemoved     = "removed"
	ReasonCanceled    = "canceled"
	ReasonSignIn      = "sign_in" // guests: the meeting needs an account
)

type access struct {
	Mode   string
	Role   string // host | cohost | invitee | member when the person may join
	Reason string // when denied
}

func (a access) canJoin() bool {
	return a.Mode == ModeHost || a.Mode == ModeCohost || a.Mode == ModeDirect
}

func (a access) isHost() bool { return a.Mode == ModeHost || a.Mode == ModeCohost }

// accessFor decides what userID may do with meeting m. It is the single
// access rule for people with accounts:
//
//   - the host always joins (even when locked);
//   - removed from the live session: denied for that session (co-hosts too);
//   - co-hosts join (even when locked);
//   - invitees join directly; so do members of the meeting's organization
//     (org and open meetings), and anyone with an open link while its lobby
//     is off, and anyone a host admitted in the last 12 hours;
//   - a locked room lets in only those already in this session (reconnects);
//   - otherwise invited and open meetings with the lobby on take a knock,
//     and org meetings refuse.
func (s *Service) accessFor(ctx context.Context, q db.Querier, m *meetingRow, userID uuid.UUID) (access, error) {
	if m.CanceledAt != nil {
		return access{Mode: ModeDenied, Reason: ReasonCanceled}, nil
	}
	if m.HostID == userID {
		return access{Mode: ModeHost, Role: RoleHost}, nil
	}
	var inviteRole *string
	var removed, attended, admitted bool
	err := q.QueryRow(ctx, `
		SELECT
		  (SELECT role FROM calls_invitees WHERE meeting_id = $1 AND user_id = $2),
		  ($3::uuid IS NOT NULL AND EXISTS (SELECT 1 FROM calls_removals WHERE session_id = $3 AND identity = $4)),
		  ($3::uuid IS NOT NULL AND EXISTS (SELECT 1 FROM calls_participants WHERE session_id = $3 AND user_id = $2)),
		  EXISTS (SELECT 1 FROM calls_lobby WHERE meeting_id = $1 AND user_id = $2 AND status = 'admitted'
		          AND decided_at > now() - make_interval(secs => $5))`,
		m.ID, userID, m.LiveSession, userIdentity(userID), admittedTTL.Seconds(),
	).Scan(&inviteRole, &removed, &attended, &admitted)
	if err != nil {
		return access{}, err
	}
	if removed {
		return access{Mode: ModeDenied, Reason: ReasonRemoved}, nil
	}
	if inviteRole != nil && *inviteRole == RoleCohost {
		return access{Mode: ModeCohost, Role: RoleCohost}, nil
	}
	role := ""
	switch {
	case inviteRole != nil:
		role = RoleInvitee
	case admitted:
		role = RoleMember
	case m.OrgID != nil && (m.Access == AccessOrg || m.Access == AccessOpen):
		orgRole, err := orgs.RoleOf(ctx, q, *m.OrgID, userID)
		if err != nil {
			return access{}, err
		}
		if orgRole != "" {
			role = RoleMember
		}
	}
	if role == "" && m.Access == AccessOpen && !m.LobbyEnabled {
		role = RoleMember
	}
	if m.Locked && !attended && !admitted {
		return access{Mode: ModeDenied, Reason: ReasonLocked}, nil
	}
	if role != "" {
		return access{Mode: ModeDirect, Role: role}, nil
	}
	if m.Access != AccessOrg && m.LobbyEnabled {
		return access{Mode: ModeKnock}, nil
	}
	if m.Access == AccessOrg {
		return access{Mode: ModeDenied, Reason: ReasonOrgOnly}, nil
	}
	return access{Mode: ModeDenied, Reason: ReasonInvitedOnly}, nil
}

// guestAccess is the rule for people without an account: only open
// meetings, never while locked; with the lobby on they knock.
func guestAccess(m *meetingRow) access {
	switch {
	case m.CanceledAt != nil:
		return access{Mode: ModeDenied, Reason: ReasonCanceled}
	case m.Access != AccessOpen:
		return access{Mode: ModeDenied, Reason: ReasonSignIn}
	case m.Locked:
		return access{Mode: ModeDenied, Reason: ReasonLocked}
	case !m.LobbyEnabled:
		return access{Mode: ModeDirect, Role: RoleGuest}
	}
	return access{Mode: ModeKnock}
}

// reasonMessage is the user-facing copy for a denial.
func reasonMessage(reason string) string {
	switch reason {
	case ReasonOrgOnly:
		return "Only members of this meeting's " + orgs.Noun() + " can join."
	case ReasonInvitedOnly:
		return "Only people invited to this meeting can join."
	case ReasonLocked:
		return "The host locked this meeting. Nobody new can join right now."
	case ReasonRemoved:
		return "A host removed you from this call."
	case ReasonCanceled:
		return "This meeting was canceled."
	case ReasonSignIn:
		return "Sign in to join this meeting."
	}
	return "You can't join this meeting."
}

// ---- identities ---------------------------------------------------------------

func userIdentity(id uuid.UUID) string  { return "u:" + id.String() }
func guestIdentity(id uuid.UUID) string { return "g:" + id.String() }

// parseIdentity splits "u:<id>" / "g:<id>".
func parseIdentity(identity string) (guest bool, id uuid.UUID, ok bool) {
	kind, rest, found := strings.Cut(identity, ":")
	if !found || (kind != "u" && kind != "g") {
		return false, uuid.Nil, false
	}
	id, err := uuid.Parse(rest)
	if err != nil {
		return false, uuid.Nil, false
	}
	return kind == "g", id, true
}

// ---- codes and keys -------------------------------------------------------------

const codeAlphabet = "abcdefghijklmnopqrstuvwxyz"

// newCode returns a Meet-style code, xxx-xxxx-xxx (26^10 ≈ 1.4e14). Access
// rules, not the code, protect org and invited meetings; lookups by code are
// rate limited per IP.
func newCode() string {
	b := make([]byte, 0, 12)
	var buf [1]byte
	for len(b) < 12 {
		if len(b) == 3 || len(b) == 8 {
			b = append(b, '-')
			continue
		}
		if _, err := rand.Read(buf[:]); err != nil {
			panic(err) // crypto/rand never fails on supported platforms
		}
		if buf[0] >= 234 { // 234 = 9*26: reject to keep the draw uniform
			continue
		}
		b = append(b, codeAlphabet[int(buf[0])%26])
	}
	return string(b)
}

// normalizeCode accepts "ABC-DEFG-HIJ", "abcdefghij" or a full join link.
func normalizeCode(in string) (string, bool) {
	in = strings.ToLower(strings.TrimSpace(in))
	if i := strings.LastIndex(in, "/"); i >= 0 {
		in = in[i+1:]
	}
	in = strings.ReplaceAll(in, "-", "")
	if len(in) != 10 {
		return "", false
	}
	for _, c := range in {
		if c < 'a' || c > 'z' {
			return "", false
		}
	}
	return in[:3] + "-" + in[3:7] + "-" + in[7:], true
}

// newGuestKey returns a guest's lobby secret and the hash we store.
func newGuestKey() (key string, hash []byte) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	key = "cgk_" + base64.RawURLEncoding.EncodeToString(b[:])
	return key, hashKey(key)
}

func hashKey(key string) []byte {
	h := sha256.Sum256([]byte(key))
	return h[:]
}

// ensureSession returns the live session of a meeting, creating one when
// the room is about to go live (the SFU webhook fills in room_sid).
func ensureSession(ctx context.Context, q db.Querier, meetingID uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	for attempt := 0; ; attempt++ {
		err := q.QueryRow(ctx, `
		WITH ins AS (
		  INSERT INTO calls_sessions (meeting_id) VALUES ($1)
		  ON CONFLICT (meeting_id) WHERE ended_at IS NULL DO NOTHING RETURNING id)
		SELECT id FROM ins UNION ALL
		SELECT id FROM calls_sessions WHERE meeting_id = $1 AND ended_at IS NULL
		LIMIT 1`, meetingID).Scan(&id)
		// A concurrent insert can commit after this statement's snapshot:
		// neither branch sees a row, and the next attempt will.
		if db.IsNoRows(err) && attempt == 0 {
			continue
		}
		return id, err
	}
}

// trimTitle cleans a meeting title (single line, at most 200 characters).
func trimTitle(t string) string {
	t = strings.Join(strings.Fields(t), " ")
	if r := []rune(t); len(r) > maxTitleChars {
		t = string(r[:maxTitleChars])
	}
	return t
}

func nowUTC() time.Time { return time.Now().UTC() }
