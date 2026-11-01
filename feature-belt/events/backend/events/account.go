package events

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/notify"
)

// DeleteAccount removes the user's RSVPs (promoting waitlists where they
// held a spot), co-host roles, invites, reminders and calendar feed. Events
// they created that haven't ended are cancelled and attendees told (queued
// in event_outbox, sent after the deletion commits); every event they
// created is anonymised: the title and times stay so attendees' history
// keeps its shape, the details, place, links and cover go.
func (s *Service) DeleteAccount(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	// Cancel first, so no one is promoted into an event that is going away.
	rows, err := tx.Query(ctx, `
		UPDATE events SET status = 'cancelled', cancelled_at = now(), sequence = sequence + 1, updated_at = now()
		WHERE host_id = $1 AND status = 'scheduled' AND ends_at > now() RETURNING id, title`, userID)
	if err != nil {
		return err
	}
	type cancelled struct {
		id    uuid.UUID
		title string
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (cancelled, error) {
		var c cancelled
		return c, row.Scan(&c.id, &c.title)
	})
	if err != nil {
		return err
	}
	for _, c := range list {
		users, err := attendeeIDs(ctx, tx, c.id)
		if err != nil {
			return err
		}
		var others []uuid.UUID
		for _, u := range users {
			if u != userID {
				others = append(others, u)
			}
		}
		if err := enqueue(ctx, tx, others, notify.Notification{
			Kind: NotifyCancelled, Title: c.title + " is cancelled", Body: "The host cancelled this event.",
			Link: eventLink(c.id), Data: map[string]any{"event_id": c.id},
		}); err != nil {
			return err
		}
	}

	rows, err = tx.Query(ctx, `
		DELETE FROM event_rsvps WHERE user_id = $1 AND status = 'going' RETURNING event_id`, userID)
	if err != nil {
		return err
	}
	freed, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return err
	}
	for _, eid := range freed {
		var title string
		if err := tx.QueryRow(ctx, `SELECT title FROM events WHERE id = $1 FOR UPDATE`, eid).Scan(&title); err != nil {
			return err
		}
		promoted, err := promoteWaitlist(ctx, tx, eid)
		if err != nil {
			return err
		}
		if err := enqueue(ctx, tx, promoted, notify.Notification{
			Kind: NotifyPromoted, Title: "You're going to " + title, Body: "A spot opened up and you moved off the waitlist.",
			Link: eventLink(eid), Data: map[string]any{"event_id": eid},
		}); err != nil {
			return err
		}
	}

	stmts := []string{
		`DELETE FROM event_rsvps WHERE user_id = $1`,
		`DELETE FROM event_cohosts WHERE user_id = $1`,
		`DELETE FROM event_invites WHERE user_id = $1`,
		`DELETE FROM event_reminders_sent WHERE user_id = $1`,
		`DELETE FROM event_calendar_feeds WHERE user_id = $1`,
		`DELETE FROM event_outbox WHERE user_id = $1`,
		`UPDATE events SET details = '', location_text = '', lat = NULL, lng = NULL, online_url = NULL,
		   cover_key = NULL, invite_code = NULL, updated_at = now()
		 WHERE host_id = $1`,
		// Covers they uploaded to events others host are deleted from storage.
		`UPDATE events SET cover_key = NULL, updated_at = now()
		 WHERE cover_key LIKE 'event_cover/' || $1::text || '/%'`,
	}
	for _, q := range stmts {
		if _, err := tx.Exec(ctx, q, userID); err != nil {
			return err
		}
	}
	return nil
}

type exportEvent struct {
	ID           uuid.UUID  `json:"id"`
	Title        string     `json:"title"`
	Details      string     `json:"details"`
	StartsAt     time.Time  `json:"starts_at"`
	EndsAt       time.Time  `json:"ends_at"`
	Timezone     string     `json:"timezone"`
	LocationText string     `json:"location_text"`
	Lat          *float64   `json:"lat"`
	Lng          *float64   `json:"lng"`
	OnlineURL    *string    `json:"online_url"`
	Capacity     *int       `json:"capacity"`
	Visibility   string     `json:"visibility"`
	Status       string     `json:"status"`
	CreatedAt    time.Time  `json:"created_at"`
	CancelledAt  *time.Time `json:"cancelled_at"`
}

type exportRSVP struct {
	EventID     uuid.UUID `json:"event_id"`
	EventTitle  string    `json:"event_title"`
	StartsAt    time.Time `json:"starts_at"`
	Status      string    `json:"status"`
	RespondedAt time.Time `json:"responded_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type exportRole struct {
	EventID    uuid.UUID `json:"event_id"`
	EventTitle string    `json:"event_title"`
	Since      time.Time `json:"since"`
}

// ExportAccount returns events the user created, their RSVPs, co-host roles
// and invites.
func (s *Service) ExportAccount(ctx context.Context, userID uuid.UUID) (any, error) {
	rows, err := s.d.Pool.Query(ctx, `
		SELECT id, title, details, starts_at, ends_at, timezone, location_text, lat, lng, online_url, capacity,
		       visibility, status, created_at, cancelled_at
		FROM events WHERE host_id = $1 ORDER BY starts_at`, userID)
	if err != nil {
		return nil, err
	}
	hosted, err := pgx.CollectRows(rows, pgx.RowToStructByPos[exportEvent])
	if err != nil {
		return nil, err
	}
	rows, err = s.d.Pool.Query(ctx, `
		SELECT r.event_id, e.title, e.starts_at, r.status, r.created_at, r.updated_at
		FROM event_rsvps r JOIN events e ON e.id = r.event_id WHERE r.user_id = $1 ORDER BY e.starts_at`, userID)
	if err != nil {
		return nil, err
	}
	rsvps, err := pgx.CollectRows(rows, pgx.RowToStructByPos[exportRSVP])
	if err != nil {
		return nil, err
	}
	rows, err = s.d.Pool.Query(ctx, `
		SELECT h.event_id, e.title, h.created_at FROM event_cohosts h JOIN events e ON e.id = h.event_id
		WHERE h.user_id = $1 ORDER BY h.created_at`, userID)
	if err != nil {
		return nil, err
	}
	cohosting, err := pgx.CollectRows(rows, pgx.RowToStructByPos[exportRole])
	if err != nil {
		return nil, err
	}
	rows, err = s.d.Pool.Query(ctx, `
		SELECT i.event_id, e.title, i.created_at FROM event_invites i JOIN events e ON e.id = i.event_id
		WHERE i.user_id = $1 ORDER BY i.created_at`, userID)
	if err != nil {
		return nil, err
	}
	invites, err := pgx.CollectRows(rows, pgx.RowToStructByPos[exportRole])
	if err != nil {
		return nil, err
	}
	var feed *time.Time
	_ = s.d.Pool.QueryRow(ctx, `SELECT created_at FROM event_calendar_feeds WHERE user_id = $1`, userID).Scan(&feed)
	return map[string]any{
		"hosted": hosted, "rsvps": rsvps, "cohosting": cohosting, "invites": invites,
		"calendar_feed_created_at": feed,
	}, nil
}

// ---- dev seed --------------------------------------------------------------

var seedNS = uuid.MustParse("0d7c2a4e-8b1f-4e0a-9c55-3a6f1b2e7d90")

func seedID(name string) uuid.UUID { return uuid.NewSHA1(seedNS, []byte("events:"+name)) }

// SeedDev adds three upcoming events, one of them full with a waitlist.
// users: admin, sam, jordan, priya.
func (s *Service) SeedDev(ctx context.Context, users []uuid.UUID) error {
	if len(users) < 4 {
		return nil
	}
	admin, sam, jordan, priya := users[0], users[1], users[2], users[3]
	// The cleanup is on the next Saturday at least two days out.
	toSaturday := (int(time.Saturday) - int(time.Now().Weekday()) + 7) % 7
	if toSaturday < 2 {
		toSaturday += 7
	}
	at := func(zone string, days, hour int) time.Time {
		loc, err := time.LoadLocation(zone)
		if err != nil {
			loc = time.UTC
		}
		d := time.Now().In(loc).AddDate(0, 0, days)
		return time.Date(d.Year(), d.Month(), d.Day(), hour, 0, 0, 0, loc).UTC()
	}
	f := func(v float64) *float64 { return &v }
	i := func(v int) *int { return &v }
	str := func(v string) *string { return &v }

	type ev struct {
		name, title, details, zone, place, visibility string
		host                                          uuid.UUID
		days, hour, hours                             int
		lat, lng                                      *float64
		online                                        *string
		capacity                                      *int
	}
	events := []ev{
		{"cleanup", "Saturday park cleanup", "We meet at the 19th Street entrance. Gloves and bags provided.",
			"America/Los_Angeles", "Dolores Park, 19th St entrance", VisibilityMembers, sam, toSaturday, 10, 2,
			f(37.7596), f(-122.4269), nil, i(2)},
		{"potluck", "Community potluck", "Bring a dish to share. Plates and drinks are covered.",
			"America/Los_Angeles", "Mission Community Center", VisibilityPublic, priya, 6, 18, 3,
			f(37.7599), f(-122.4148), nil, nil},
		{"reading", "Online reading group", "Chapter 5 this week.",
			"Europe/London", "", VisibilityMembers, jordan, 2, 19, 1,
			nil, nil, str("https://meet.example.com/reading-group"), nil},
	}
	for _, e := range events {
		start := at(e.zone, e.days, e.hour)
		if _, err := s.d.Pool.Exec(ctx, `
			INSERT INTO events (id, host_id, title, details, starts_at, ends_at, timezone, location_text, lat, lng,
			                    online_url, capacity, visibility)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
			ON CONFLICT (id) DO NOTHING`,
			seedID(e.name), e.host, e.title, e.details, start, start.Add(time.Duration(e.hours)*time.Hour), e.zone,
			e.place, e.lat, e.lng, e.online, e.capacity, e.visibility); err != nil {
			return err
		}
	}
	if _, err := s.d.Pool.Exec(ctx, `
		INSERT INTO event_cohosts (event_id, user_id, added_by) VALUES ($1, $2, $3)
		ON CONFLICT DO NOTHING`, seedID("potluck"), jordan, priya); err != nil {
		return err
	}
	type rsvp struct {
		event  string
		user   uuid.UUID
		status string
		ago    time.Duration
	}
	for _, r := range []rsvp{
		{"cleanup", jordan, RSVPGoing, 30 * time.Hour},
		{"cleanup", priya, RSVPGoing, 26 * time.Hour},
		{"cleanup", admin, RSVPWaitlisted, 20 * time.Hour},
		{"potluck", sam, RSVPGoing, 10 * time.Hour},
		{"potluck", jordan, RSVPMaybe, 8 * time.Hour},
		{"reading", sam, RSVPGoing, 12 * time.Hour},
		{"reading", priya, RSVPDeclined, 6 * time.Hour},
	} {
		created := time.Now().Add(-r.ago).UTC()
		var waitlisted *time.Time
		if r.status == RSVPWaitlisted {
			waitlisted = &created
		}
		if _, err := s.d.Pool.Exec(ctx, `
			INSERT INTO event_rsvps (event_id, user_id, status, waitlisted_at, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $5) ON CONFLICT (event_id, user_id) DO NOTHING`,
			seedID(r.event), r.user, r.status, waitlisted, created); err != nil {
			return err
		}
	}
	return nil
}
