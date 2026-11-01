package calendar

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/db"
)

// SeedDev gives the dev identities a believable week: zones, Sam's
// standup and 1:1 series, meetings with colleagues and an external guest,
// an all-day offsite, a few events for Jordan and Priya, and two booking
// links for Sam (internal for the climbing club, external intro calls)
// with one booking each. users are admin, sam, jordan, priya. It runs once
// (marked by Sam's intro link) and logs the external booking's manage link.
func (s *Service) SeedDev(ctx context.Context, users []uuid.UUID) error {
	if len(users) < 4 {
		return nil
	}
	// Orgs seed after calendar in name order; make sure the clubs exist.
	for _, m := range s.d.Modules {
		if m.Name() == "orgs" {
			if sd, ok := m.(app.DevSeeder); ok {
				if err := sd.SeedDev(ctx, users); err != nil {
					return err
				}
			}
		}
	}
	var done bool
	if err := s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM calendar_booking_links WHERE slug = 'sam-intro-call')`).Scan(&done); err != nil || done {
		return err
	}
	var orgID *uuid.UUID
	var oid uuid.UUID
	if err := s.d.Pool.QueryRow(ctx, `SELECT id FROM orgs WHERE slug = 'riverside-climbing'`).Scan(&oid); err == nil {
		orgID = &oid
	}
	admin, sam, jordan, priya := users[0], users[1], users[2], users[3]
	la, _ := time.LoadLocation("America/Los_Angeles")
	ny, _ := time.LoadLocation("America/New_York")
	london, _ := time.LoadLocation("Europe/London")

	now := time.Now().In(la)
	monday := time.Date(now.Year(), now.Month(), now.Day()-(int(now.Weekday())+6)%7, 0, 0, 0, 0, la)
	at := func(loc *time.Location, day, hour, minute int) time.Time {
		d := monday.AddDate(0, 0, day)
		return time.Date(d.Year(), d.Month(), d.Day(), hour, minute, 0, 0, loc).UTC()
	}
	hours := func() []byte {
		b, _ := json.Marshal(defaultWorkingHours())
		return b
	}()
	var manageURL string

	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		for _, z := range []struct {
			user uuid.UUID
			tz   string
		}{{admin, "UTC"}, {sam, "America/Los_Angeles"}, {jordan, "America/New_York"}, {priya, "Europe/London"}} {
			if _, err := tx.Exec(ctx, `INSERT INTO calendar_settings (user_id, timezone, working_hours) VALUES ($1, $2, $3)
				ON CONFLICT (user_id) DO NOTHING`, z.user, z.tz, hours); err != nil {
				return err
			}
		}
		type ev struct {
			owner      uuid.UUID
			title      string
			loc        *time.Location
			start, end time.Time
			allDay     bool
			rrule      string
			location   string
			desc       string
			color      string
			visibility string
			guests     []resolvedAttendee
			responses  []string
		}
		uid := func(u uuid.UUID) *uuid.UUID { return &u }
		events := []ev{
			{owner: sam, title: "Daily standup", loc: la, start: at(la, 0, 9, 30), end: at(la, 0, 9, 45), rrule: "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR",
				guests: []resolvedAttendee{{userID: uid(jordan)}, {userID: uid(priya)}}, responses: []string{ResponseAccepted, ResponseAccepted}},
			{owner: sam, title: "Roadmap review", loc: la, start: at(la, 0, 11, 0), end: at(la, 0, 12, 0), location: "Room 3B",
				desc: "Q1 priorities and the hiring plan.", guests: []resolvedAttendee{{userID: uid(jordan)}, {email: "alex@northwind.example", name: "Alex Chen"}},
				responses: []string{ResponseAccepted, ResponseNeedsAction}},
			{owner: sam, title: "Lunch with Priya", loc: la, start: at(la, 1, 12, 30), end: at(la, 1, 13, 30), location: "Tartine, 18th St",
				color: "success", guests: []resolvedAttendee{{userID: uid(priya)}}, responses: []string{ResponseAccepted}},
			{owner: sam, title: "Jordan / Sam 1:1", loc: la, start: at(la, 2, 15, 0), end: at(la, 2, 15, 30), rrule: "FREQ=WEEKLY;INTERVAL=1",
				guests: []resolvedAttendee{{userID: uid(jordan)}}, responses: []string{ResponseAccepted}},
			{owner: sam, title: "Design critique", loc: la, start: at(la, 3, 10, 0), end: at(la, 3, 11, 30), color: "accent",
				desc: "Bring the onboarding flow mocks.", guests: []resolvedAttendee{{userID: uid(priya)}}, responses: []string{ResponseTentative}},
			{owner: sam, title: "Team offsite", loc: la, start: at(la, 4, 0, 0), end: at(la, 5, 0, 0), allDay: true, location: "Mission Cliffs", color: "warning"},
			{owner: sam, title: "Climbing gym", loc: la, start: at(la, 4, 17, 30), end: at(la, 4, 19, 0), color: "info", visibility: "details"},
			{owner: jordan, title: "Customer call: Northwind", loc: ny, start: at(ny, 1, 10, 0), end: at(ny, 1, 11, 0), location: "Phone"},
			{owner: jordan, title: "Hiring sync", loc: ny, start: at(ny, 3, 14, 0), end: at(ny, 3, 15, 0),
				guests: []resolvedAttendee{{userID: uid(sam)}}, responses: []string{ResponseNeedsAction}},
			{owner: priya, title: "Book club planning", loc: london, start: at(london, 0, 18, 0), end: at(london, 0, 19, 0),
				guests: []resolvedAttendee{{userID: uid(sam)}}, responses: []string{ResponseDeclined}},
		}
		for _, e := range events {
			row := eventRow{ID: uuid.New(), OwnerID: e.owner, Title: e.title, Description: e.desc, Location: e.location,
				StartsAt: e.start, EndsAt: e.end, Timezone: e.loc.String(), AllDay: e.allDay, Status: "confirmed",
				Visibility: "busy", ShowAs: "busy", Color: "primary", Reminders: []int32{10}}
			if e.color != "" {
				row.Color = e.color
			}
			if e.visibility != "" {
				row.Visibility = e.visibility
			}
			if e.allDay {
				row.Reminders = []int32{}
			}
			if e.rrule != "" {
				r, err := ParseRule(e.rrule, e.loc)
				if err != nil {
					return err
				}
				str := r.String(false, e.loc)
				row.RRule, row.SeriesUntil = &str, seriesUntil(r, row.StartsAt, e.loc, row.duration())
			}
			if err := insertEvent(ctx, tx, row); err != nil {
				return err
			}
			for i, g := range e.guests {
				if _, err := s.insertAttendees(ctx, tx, row.ID, []resolvedAttendee{g}, e.responses[i]); err != nil {
					return err
				}
			}
		}

		// Booking links: one internal (club members only), one external.
		type link struct {
			slug, title, desc, audience, locType, locValue string
			org                                            *uuid.UUID
			durations                                      []int
			questions                                      []Question
		}
		links := []link{
			{slug: "sam-intro-call", title: "Intro call", desc: "A short call to see if we can help.", audience: "external",
				locType: LocationPhone, durations: []int{15, 30},
				questions: []Question{{ID: "topic", Label: "What would you like to talk about?", Type: "long_text", Required: true, Options: []string{}}}},
		}
		if orgID != nil {
			links = append(links, link{slug: "sam-office-hours", title: "Club office hours", desc: "Route setting, gear and membership questions.",
				audience: "internal", org: orgID, locType: LocationInPerson, locValue: "Front desk, Riverside gym", durations: []int{30}, questions: []Question{}})
		}
		linkIDs := map[string]uuid.UUID{}
		for _, l := range links {
			q, _ := json.Marshal(l.questions)
			var id uuid.UUID
			if err := tx.QueryRow(ctx, `
				INSERT INTO calendar_booking_links (owner_id, org_id, audience, slug, title, description, durations, location_type,
				  location_value, timezone, weekly_hours, questions, buffer_before, buffer_after, min_notice, max_days_ahead, slot_interval)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'America/Los_Angeles', $10, $11, 0, 10, 120, 60, 30) RETURNING id`,
				sam, l.org, l.audience, l.slug, l.title, l.desc, toInt32(l.durations), l.locType, l.locValue, hours, q).Scan(&id); err != nil {
				return err
			}
			linkIDs[l.slug] = id
		}

		// One booking on each, next week.
		type booking struct {
			slug, name, email, phone, title string
			booker                          *uuid.UUID
			start                           time.Time
			mins                            int
			answers                         []Answer
		}
		bookings := []booking{{slug: "sam-intro-call", name: "Casey Morgan", email: "casey@example.com", phone: "+1 415 555 0142",
			title: "Intro call", start: at(la, 9, 11, 0), mins: 30,
			answers: []Answer{{QuestionID: "topic", Label: "What would you like to talk about?", Value: "Moving our team scheduling off spreadsheets."}}}}
		if orgID != nil {
			bookings = append(bookings, booking{slug: "sam-office-hours", name: "Jordan Lee", email: "jordan@scaffoldapp.test",
				title: "Club office hours", booker: uid(jordan), start: at(la, 8, 10, 0), mins: 30, answers: []Answer{}})
		}
		for _, b := range bookings {
			id := uuid.New()
			row := bookingRow{ID: id, HostID: sam, Title: b.title, BookerName: b.name, BookerEmail: b.email, StartsAt: b.start,
				EndsAt: b.start.Add(time.Duration(b.mins) * time.Minute), Answers: b.answers, LocationType: LocationPhone, LocationValue: b.phone}
			if b.booker != nil {
				row.LocationType, row.LocationValue = LocationInPerson, "Front desk, Riverside gym"
			}
			ev := eventRow{ID: uuid.New(), OwnerID: sam, Title: bookingTitle(b.title, "Sam Rivera", b.name), StartsAt: row.StartsAt,
				EndsAt: row.EndsAt, Timezone: "America/Los_Angeles", Status: "confirmed", Visibility: "busy", ShowAs: "busy",
				Color: "primary", Location: bookingLocation(row), Description: bookingNotes(row, b.phone), Reminders: []int32{10}}
			if err := insertEvent(ctx, tx, ev); err != nil {
				return err
			}
			att := resolvedAttendee{userID: b.booker}
			if b.booker == nil {
				att = resolvedAttendee{email: b.email, name: b.name}
			}
			if _, err := s.insertAttendees(ctx, tx, ev.ID, []resolvedAttendee{att}, ResponseAccepted); err != nil {
				return err
			}
			token, _ := s.tokenFor("manage", id)
			answers, _ := json.Marshal(b.answers)
			if _, err := tx.Exec(ctx, `
				INSERT INTO calendar_bookings (id, link_id, host_id, event_id, title, booker_user_id, booker_name, booker_email,
				  booker_timezone, starts_at, ends_at, block, location_type, location_value, answers, manage_token_hash)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'America/Los_Angeles', $9, $10, tstzrange($9::timestamptz, $10::timestamptz + interval '10 minutes'),
				  $11, $12, $13, $14)`,
				id, linkIDs[b.slug], sam, ev.ID, b.title, b.booker, b.name, b.email, row.StartsAt, row.EndsAt,
				row.LocationType, row.LocationValue, answers, hashToken(token)); err != nil {
				return err
			}
			if b.booker == nil {
				manageURL = s.webURL("/book/manage/" + token)
			}
		}
		return nil
	})
	if err == nil && manageURL != "" {
		slog.Info("calendar: dev booking manage link for casey@example.com", "url", manageURL)
	}
	return err
}
