package calls

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/orgs"
)

// SeedDev gives the dev identities meetings and history: an upcoming
// org-only sync Sam hosts (Jordan co-hosts), a design review Jordan hosts,
// Sam's open office-hours link (guests knock), two past calls with
// attendance (one with a guest) and missed calls for Sam and Priya. Codes
// and SFU ids are fixed, so re-running only refreshes the schedule. users
// are admin, sam, jordan, priya (auth.DevIdentities order).
func (s *Service) SeedDev(ctx context.Context, users []uuid.UUID) error {
	if len(users) < 4 {
		return nil
	}
	sam, jordan, priya := users[1], users[2], users[3]
	// The orgs seed runs after this one (modules seed in name order) and is
	// idempotent, so run it first for the org the sync belongs to.
	for _, m := range s.d.Modules {
		if o, ok := m.(*orgs.Service); ok {
			if err := o.SeedDev(ctx, users); err != nil {
				return err
			}
		}
	}
	var orgID *uuid.UUID
	err := s.d.Pool.QueryRow(ctx, `SELECT org_id FROM org_members WHERE user_id = $1 AND role = 'owner' ORDER BY created_at, org_id LIMIT 1`, sam).Scan(&orgID)
	if err != nil && !db.IsNoRows(err) {
		return err
	}
	now := time.Now().UTC().Truncate(time.Minute)
	day := func(d int, h int) time.Time {
		t := time.Date(now.Year(), now.Month(), now.Day(), h, 0, 0, 0, time.UTC)
		return t.AddDate(0, 0, d)
	}
	type seedMeeting struct {
		code, title, kind, access string
		host                      uuid.UUID
		org                       *uuid.UUID
		start, end                *time.Time
		invitees                  map[uuid.UUID]string
	}
	at := func(t time.Time) *time.Time { return &t }
	syncAccess := AccessInvited
	if orgID != nil {
		syncAccess = AccessOrg
	}
	meetings := []seedMeeting{
		{"wky-sync-sam", "Weekly sync", KindMeeting, syncAccess, sam, orgID, at(day(1, 15)), at(day(1, 15).Add(30 * time.Minute)),
			map[uuid.UUID]string{jordan: RoleCohost, priya: RoleInvitee}},
		{"dsg-revw-jor", "Design review", KindMeeting, AccessInvited, jordan, nil, at(day(3, 17)), at(day(3, 18)),
			map[uuid.UUID]string{sam: RoleInvitee, priya: RoleInvitee}},
		{"opn-hour-sam", "Office hours", KindMeeting, AccessOpen, sam, nil, nil, nil, nil},
		{"pln-call-sam", "", KindCall, AccessInvited, sam, nil, nil, nil, map[uuid.UUID]string{jordan: RoleInvitee}},
		{"msd-call-pri", "", KindCall, AccessInvited, priya, nil, nil, nil, map[uuid.UUID]string{sam: RoleInvitee, jordan: RoleInvitee}},
	}
	return db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		ids := map[string]uuid.UUID{}
		for _, m := range meetings {
			var id uuid.UUID
			if err := tx.QueryRow(ctx, `
				INSERT INTO calls_meetings (code, kind, title, host_id, org_id, access, starts_at, ends_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
				ON CONFLICT (code) DO UPDATE SET starts_at = EXCLUDED.starts_at, ends_at = EXCLUDED.ends_at, canceled_at = NULL
				RETURNING id`, m.code, m.kind, m.title, m.host, m.org, m.access, m.start, m.end).Scan(&id); err != nil {
				return err
			}
			ids[m.code] = id
			for uid, role := range m.invitees {
				if _, err := tx.Exec(ctx, `INSERT INTO calls_invitees (meeting_id, user_id, role, invited_by) VALUES ($1, $2, $3, $4)
					ON CONFLICT (meeting_id, user_id) DO NOTHING`, id, uid, role, m.host); err != nil {
					return err
				}
			}
		}
		// Past calls: a 34-minute call Sam and Jordan had two days ago, and
		// office hours yesterday where a guest joined for 12 minutes.
		type stint struct {
			sid      string
			identity string
			user     *uuid.UUID
			name     string
			from, to time.Duration
		}
		past := []struct {
			code, roomSID string
			start         time.Time
			length        time.Duration
			people        []stint
		}{
			{"pln-call-sam", "seed-planning", day(-2, 16), 34 * time.Minute, []stint{
				{"seed-planning-sam", userIdentity(sam), &sam, "", 0, 34 * time.Minute},
				{"seed-planning-jordan", userIdentity(jordan), &jordan, "", time.Minute, 34 * time.Minute},
			}},
			{"opn-hour-sam", "seed-office", day(-1, 18), 25 * time.Minute, []stint{
				{"seed-office-sam", userIdentity(sam), &sam, "", 0, 25 * time.Minute},
				{"seed-office-priya", userIdentity(priya), &priya, "", 2 * time.Minute, 20 * time.Minute},
				{"seed-office-guest", "g:" + uuid.NewSHA1(uuid.NameSpaceOID, []byte("calls-seed-guest")).String(), nil, "Alex Morgan", 8 * time.Minute, 20 * time.Minute},
			}},
		}
		for _, p := range past {
			var sessionID uuid.UUID
			err := tx.QueryRow(ctx, `
				INSERT INTO calls_sessions (meeting_id, room_sid, started_at, ended_at, end_reason) VALUES ($1, $2, $3, $4, 'empty')
				ON CONFLICT (room_sid) DO UPDATE SET room_sid = EXCLUDED.room_sid RETURNING id`,
				ids[p.code], p.roomSID, p.start, p.start.Add(p.length)).Scan(&sessionID)
			if err != nil {
				return err
			}
			for _, st := range p.people {
				if _, err := tx.Exec(ctx, `
					INSERT INTO calls_participants (session_id, identity, user_id, name, participant_sid, joined_at, left_at)
					VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT (participant_sid) WHERE participant_sid IS NOT NULL DO NOTHING`,
					sessionID, st.identity, st.user, st.name, st.sid, p.start.Add(st.from), p.start.Add(st.to)); err != nil {
					return err
				}
			}
		}
		// Missed calls: Priya rang Sam and Jordan 3 hours ago; Sam missed it, Jordan declined.
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM calls_rings WHERE meeting_id = $1)`, ids["msd-call-pri"]).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			rangAt := now.Add(-3 * time.Hour)
			for _, u := range []struct {
				id     uuid.UUID
				status string
			}{{sam, "missed"}, {jordan, "declined"}} {
				if _, err := tx.Exec(ctx, `INSERT INTO calls_rings (meeting_id, caller_id, user_id, status, expires_at, created_at, updated_at)
					VALUES ($1, $2, $3, $4, $5, $5, $5)`, ids["msd-call-pri"], priya, u.id, u.status, rangAt); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
