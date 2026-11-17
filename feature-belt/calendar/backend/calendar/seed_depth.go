package calendar

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/db"
)

// seedDepth adds 1.3.0's pieces to the dev data, once (marked by the
// climbing club's Boulder room): everyone's primary calendar, a Coaching
// team calendar with a session on it, Jordan sharing his calendar with
// Sam (details) and Priya's at free/busy, three rooms and a projector,
// Sam's weekly focus time and a day out of office, and two team booking
// links (Route setters round robin, Coaching collective).
func (s *Service) seedDepth(ctx context.Context, users []uuid.UUID) error {
	for _, m := range s.d.Modules {
		if m.Name() == "access" {
			if sd, ok := m.(app.DevSeeder); ok {
				if err := sd.SeedDev(ctx, users); err != nil {
					return err
				}
			}
		}
	}
	var orgID uuid.UUID
	if err := s.d.Pool.QueryRow(ctx, `SELECT id FROM orgs WHERE slug = 'riverside-climbing'`).Scan(&orgID); err != nil {
		if db.IsNoRows(err) {
			return nil
		}
		return err
	}
	var done bool
	if err := s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM calendar_rooms WHERE org_id = $1 AND name = 'Boulder room')`, orgID).Scan(&done); err != nil || done {
		return err
	}
	admin, sam, jordan, priya := users[0], users[1], users[2], users[3]
	cals := map[uuid.UUID]uuid.UUID{}
	for _, u := range []uuid.UUID{admin, sam, jordan, priya} {
		id, err := s.ensurePrimary(ctx, u, orgID)
		if err != nil {
			return err
		}
		cals[u] = id
	}
	team := func(handle string) uuid.UUID {
		var id uuid.UUID
		_ = s.d.Pool.QueryRow(ctx, `SELECT id FROM access_teams WHERE org_id = $1 AND handle = $2`, orgID, handle).Scan(&id)
		return id
	}
	coaching, setters := team("coaching"), team("route-setters")
	la, _ := time.LoadLocation("America/Los_Angeles")
	now := time.Now().In(la)
	monday := time.Date(now.Year(), now.Month(), now.Day()-(int(now.Weekday())+6)%7, 0, 0, 0, 0, la)
	at := func(day, hour, minute int) time.Time {
		d := monday.AddDate(0, 0, day)
		return time.Date(d.Year(), d.Month(), d.Day(), hour, minute, 0, 0, la).UTC()
	}
	return db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		// Jordan shares his calendar with Sam (details) and Sam adds it.
		if err := access.AddGrant(ctx, tx, access.Ref{Kind: KindCalendar, ID: cals[jordan]}, access.SubjectRef{Type: "user", ID: sam}, access.Comment, jordan); err != nil {
			return err
		}
		for _, c := range []uuid.UUID{cals[jordan], cals[priya]} {
			if _, err := tx.Exec(ctx, `INSERT INTO calendar_calendar_prefs (user_id, calendar_id, added) VALUES ($1, $2, true) ON CONFLICT DO NOTHING`, sam, c); err != nil {
				return err
			}
		}
		if coaching != uuid.Nil {
			calID := uuid.New()
			if _, err := tx.Exec(ctx, `INSERT INTO calendar_calendars (id, owner_id, org_id, team_id, name, description, color, counts_busy)
				VALUES ($1, $2, $3, $4, 'Coaching', 'Youth team practice and technique sessions.', 'success', false)`, calID, sam, orgID, coaching); err != nil {
				return err
			}
			ref := access.Ref{Kind: KindCalendar, ID: calID}
			if err := access.CreateResource(ctx, tx, access.NewResource{Kind: KindCalendar, ID: calID, OrgID: orgID, OwnerID: sam}); err != nil {
				return err
			}
			if err := access.AddGrant(ctx, tx, ref, access.SubjectRef{Type: "team", ID: coaching}, access.Edit, sam); err != nil {
				return err
			}
			str := "FREQ=WEEKLY;BYDAY=TU"
			if err := insertEvent(ctx, tx, eventRow{ID: uuid.New(), OwnerID: jordan, CalendarID: &calID, Title: "Technique session",
				StartsAt: at(1, 18, 0), EndsAt: at(1, 19, 30), Timezone: "America/Los_Angeles", RRule: &str, Status: "confirmed",
				Visibility: "busy", ShowAs: "busy", Color: "success", Location: "Main wall"}); err != nil {
				return err
			}
		}
		// Sam: focus time Tuesday and Thursday mornings, out of office next Friday.
		focus := "FREQ=WEEKLY;BYDAY=TU,TH"
		if err := insertEvent(ctx, tx, eventRow{ID: uuid.New(), OwnerID: sam, CalendarID: &[]uuid.UUID{cals[sam]}[0], Title: "Focus time",
			StartsAt: at(1, 9, 0), EndsAt: at(1, 11, 0), Timezone: "America/Los_Angeles", RRule: &focus, Status: "confirmed",
			Visibility: "details", ShowAs: "busy", Color: "info", EventType: TypeFocus, AutoDecline: DeclineNew}); err != nil {
			return err
		}
		fri := monday.AddDate(0, 0, 11)
		if err := insertEvent(ctx, tx, eventRow{ID: uuid.New(), OwnerID: sam, CalendarID: &[]uuid.UUID{cals[sam]}[0], Title: "Out of office",
			StartsAt: time.Date(fri.Year(), fri.Month(), fri.Day(), 0, 0, 0, 0, la).UTC(), EndsAt: time.Date(fri.Year(), fri.Month(), fri.Day()+1, 0, 0, 0, 0, la).UTC(),
			Timezone: "America/Los_Angeles", AllDay: true, Status: "confirmed", Visibility: "details", ShowAs: "busy", Color: "neutral",
			EventType: TypeOutOfOffice, AutoDecline: DeclineNew, DeclineMessage: "At the regional competition; back Monday."}); err != nil {
			return err
		}
		// Rooms and a resource.
		for _, r := range []struct {
			kind, name, building, floor string
			capacity                    int
			features                    []string
		}{
			{"room", "Boulder room", "Main hall", "1", 8, []string{"TV", "Whiteboard"}},
			{"room", "Board room", "Main hall", "2", 12, []string{"Video conferencing", "Whiteboard"}},
			{"room", "Quiet room", "Annex", "1", 2, []string{}},
			{"resource", "Projector", "Main hall", "", 0, []string{"HDMI"}},
		} {
			id := uuid.New()
			var capacity *int
			if r.capacity > 0 {
				capacity = &r.capacity
			}
			if _, err := tx.Exec(ctx, `INSERT INTO calendar_rooms (id, org_id, kind, name, building, floor, capacity, features, created_by)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`, id, orgID, r.kind, r.name, r.building, r.floor, capacity, r.features, admin); err != nil {
				return err
			}
			if err := access.CreateResource(ctx, tx, access.NewResource{Kind: KindRoom, ID: id, OrgID: orgID, General: access.Edit}); err != nil {
				return err
			}
		}
		// Team booking links.
		hours, _ := json.Marshal(defaultWorkingHours())
		for _, l := range []struct {
			kind, slug, title string
			team              uuid.UUID
			owner             uuid.UUID
			hosts             []uuid.UUID
			duration          int
		}{
			{LinkRoundRobin, "route-setting-intro", "Route setting intro", setters, jordan, []uuid.UUID{jordan, priya}, 30},
			{LinkCollective, "coaching-assessment", "Coaching assessment", coaching, sam, []uuid.UUID{sam, jordan}, 45},
		} {
			if l.team == uuid.Nil {
				continue
			}
			var id uuid.UUID
			if err := tx.QueryRow(ctx, `INSERT INTO calendar_booking_links (owner_id, org_id, audience, slug, title, description, durations,
				location_type, location_value, timezone, weekly_hours, kind, team_id, booker_reminders, allow_guests)
				VALUES ($1, $2, 'external', $3, $4, $5, $6, 'in_person', 'Riverside Climbing, front desk', 'America/Los_Angeles', $7, $8, $9, '{1440,60}', true)
				RETURNING id`, l.owner, orgID, l.slug, l.title, "Meet the team for "+l.title+".", []int32{int32(l.duration)}, hours, l.kind, l.team).Scan(&id); err != nil {
				return err
			}
			for i, h := range l.hosts {
				if _, err := tx.Exec(ctx, `INSERT INTO calendar_link_hosts (link_id, user_id, priority) SELECT $1, $2, $3
					WHERE EXISTS (SELECT 1 FROM access_team_members WHERE team_id = $4 AND user_id = $2)`, id, h, 2+i%2, l.team); err != nil {
					return err
				}
			}
		}
		// Settings: where people work.
		locs, _ := json.Marshal([]WorkLocation{{Day: 1, Type: "office", Label: "Main hall"}, {Day: 2, Type: "office", Label: "Main hall"},
			{Day: 3, Type: "home"}, {Day: 4, Type: "office", Label: "Main hall"}, {Day: 5, Type: "home"}})
		_, err := tx.Exec(ctx, `UPDATE calendar_settings SET work_locations = $2, world_clocks = '{Europe/London,Asia/Tokyo}' WHERE user_id = $1`, sam, locs)
		return err
	})
}
