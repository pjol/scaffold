package calls

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// deletedLive holds, between an account's deletion transaction and its
// AccountDeleted hook, the rooms to act on: meetings the person hosted that
// are live (ended) and live rooms they may still be in (removed from).
var deletedLive sync.Map // uuid.UUID → deletedRooms

type deletedRooms struct {
	hosted []uuid.UUID
	joined []uuid.UUID
}

// DeleteAccount runs inside the account-deletion transaction:
//   - meetings they host that nobody ever used are deleted; the rest are
//     canceled (the links stop working) and keep their title for the other
//     people's history;
//   - their invitations, knocks and the rings they received are deleted;
//   - attendance and rings they made stay for the other people's history,
//     shown as "Deleted user" (the users row no longer identifies anyone).
func (s *Service) DeleteAccount(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	var rooms deletedRooms
	rows, err := tx.Query(ctx, `SELECT m.id FROM calls_meetings m JOIN calls_sessions ss ON ss.meeting_id = m.id AND ss.ended_at IS NULL
		WHERE m.host_id = $1`, userID)
	if err != nil {
		return err
	}
	if rooms.hosted, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID]); err != nil {
		return err
	}
	rows, err = tx.Query(ctx, `SELECT DISTINCT ss.meeting_id FROM calls_participants p JOIN calls_sessions ss ON ss.id = p.session_id
		WHERE p.user_id = $1 AND p.left_at IS NULL AND ss.ended_at IS NULL`, userID)
	if err != nil {
		return err
	}
	if rooms.joined, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID]); err != nil {
		return err
	}
	for _, q := range []string{
		`DELETE FROM calls_meetings m WHERE m.host_id = $1 AND NOT EXISTS (SELECT 1 FROM calls_sessions ss WHERE ss.meeting_id = m.id)`,
		`UPDATE calls_meetings SET canceled_at = now(), updated_at = now() WHERE host_id = $1 AND canceled_at IS NULL`,
		`DELETE FROM calls_invitees WHERE user_id = $1`,
		`UPDATE calls_invitees SET invited_by = NULL WHERE invited_by = $1`,
		`DELETE FROM calls_lobby WHERE user_id = $1`,
		`UPDATE calls_lobby SET decided_by = NULL WHERE decided_by = $1`,
		`DELETE FROM calls_rings WHERE user_id = $1`,
		`UPDATE calls_rings SET status = 'missed', updated_at = now() WHERE caller_id = $1 AND status = 'ringing'`,
		`UPDATE calls_participants SET left_at = now() WHERE user_id = $1 AND left_at IS NULL`,
		`UPDATE calls_removals SET removed_by = NULL WHERE removed_by = $1`,
		`UPDATE calls_sessions SET ended_by = NULL WHERE ended_by = $1`,
	} {
		if _, err := tx.Exec(ctx, q, userID); err != nil {
			return err
		}
	}
	if len(rooms.hosted)+len(rooms.joined) > 0 {
		deletedLive.Store(userID, rooms)
	}
	return nil
}

// AccountDeleted runs after the deletion commits: live rooms the person
// hosted end, and they are removed from any room they were still in.
func (s *Service) AccountDeleted(ctx context.Context, userID uuid.UUID) error {
	v, ok := deletedLive.LoadAndDelete(userID)
	if !ok || s.media == nil {
		return nil
	}
	rooms := v.(deletedRooms)
	for _, id := range rooms.hosted {
		if err := s.media.EndRoom(ctx, id.String()); err != nil {
			slog.WarnContext(ctx, "calls: ending a deleted host's room failed", "meeting_id", id, "err", err)
		}
	}
	for _, id := range rooms.joined {
		if err := s.media.RemoveParticipant(ctx, id.String(), userIdentity(userID)); err != nil && err != errMediaNotFound {
			slog.WarnContext(ctx, "calls: removing a deleted account from a room failed", "meeting_id", id, "err", err)
		}
	}
	return nil
}

// ExportAccount returns the meetings the person hosts, their invitations,
// their attendance and the calls that rang them or that they made.
func (s *Service) ExportAccount(ctx context.Context, userID uuid.UUID) (any, error) {
	type hosted struct {
		Code       string     `json:"code"`
		Title      string     `json:"title"`
		Kind       string     `json:"kind"`
		Access     string     `json:"access"`
		StartsAt   *time.Time `json:"starts_at"`
		EndsAt     *time.Time `json:"ends_at"`
		CanceledAt *time.Time `json:"canceled_at"`
		CreatedAt  time.Time  `json:"created_at"`
	}
	type invitation struct {
		Code      string    `json:"code"`
		Title     string    `json:"title"`
		Role      string    `json:"role"`
		InvitedAt time.Time `json:"invited_at"`
	}
	type attendance struct {
		Code     string     `json:"code"`
		Title    string     `json:"title"`
		JoinedAt time.Time  `json:"joined_at"`
		LeftAt   *time.Time `json:"left_at"`
	}
	type ring struct {
		Code      string    `json:"code"`
		Direction string    `json:"direction"` // received | made
		Status    string    `json:"status"`
		At        time.Time `json:"at"`
	}
	collect := func(sql string, fn func(pgx.Rows) error) error {
		rows, err := s.d.Pool.Query(ctx, sql, userID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			if err := fn(rows); err != nil {
				return err
			}
		}
		return rows.Err()
	}
	out := struct {
		Hosted      []hosted     `json:"meetings_hosted"`
		Invitations []invitation `json:"invitations"`
		Attendance  []attendance `json:"attendance"`
		Rings       []ring       `json:"rings"`
	}{[]hosted{}, []invitation{}, []attendance{}, []ring{}}
	if err := collect(`SELECT code, title, kind, access, starts_at, ends_at, canceled_at, created_at FROM calls_meetings
		WHERE host_id = $1 ORDER BY created_at`, func(r pgx.Rows) error {
		var h hosted
		err := r.Scan(&h.Code, &h.Title, &h.Kind, &h.Access, &h.StartsAt, &h.EndsAt, &h.CanceledAt, &h.CreatedAt)
		out.Hosted = append(out.Hosted, h)
		return err
	}); err != nil {
		return nil, err
	}
	if err := collect(`SELECT m.code, m.title, i.role, i.created_at FROM calls_invitees i JOIN calls_meetings m ON m.id = i.meeting_id
		WHERE i.user_id = $1 ORDER BY i.created_at`, func(r pgx.Rows) error {
		var v invitation
		err := r.Scan(&v.Code, &v.Title, &v.Role, &v.InvitedAt)
		out.Invitations = append(out.Invitations, v)
		return err
	}); err != nil {
		return nil, err
	}
	if err := collect(`SELECT m.code, m.title, p.joined_at, p.left_at FROM calls_participants p
		JOIN calls_sessions ss ON ss.id = p.session_id JOIN calls_meetings m ON m.id = ss.meeting_id
		WHERE p.user_id = $1 ORDER BY p.joined_at`, func(r pgx.Rows) error {
		var a attendance
		err := r.Scan(&a.Code, &a.Title, &a.JoinedAt, &a.LeftAt)
		out.Attendance = append(out.Attendance, a)
		return err
	}); err != nil {
		return nil, err
	}
	if err := collect(`SELECT m.code, CASE WHEN r.user_id = $1 THEN 'received' ELSE 'made' END, r.status, r.created_at
		FROM calls_rings r JOIN calls_meetings m ON m.id = r.meeting_id
		WHERE r.user_id = $1 OR r.caller_id = $1 ORDER BY r.created_at`, func(r pgx.Rows) error {
		var v ring
		err := r.Scan(&v.Code, &v.Direction, &v.Status, &v.At)
		out.Rings = append(out.Rings, v)
		return err
	}); err != nil {
		return nil, err
	}
	return out, nil
}
