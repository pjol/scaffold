package calendar

// Read seams for other features through the app's stitching: a person's
// meetings with given guests (a CRM's "Log meeting") and the booking pages
// people outside the workspace can open (a CRM email's booking link).

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/auth"
)

// GuestMeeting is an event on someone's own calendars with given guests.
type GuestMeeting struct {
	// ID is stable for the occurrence: the app's events "<event id>" (a
	// series: "<event id>@<occurrence start, RFC 3339>"); connected events
	// their ConnectedEvent.EventID.
	ID       string
	Title    string
	StartsAt time.Time
	EndsAt   time.Time
	AllDay   bool
	Location string
	Guests   []string // everyone else on it by email: guests and the organizer
	Link     string   // app path that opens it in the calendar
	Source   string   // connected: "Google Calendar · sam@acme.com"; "" for the app's own
}

// MeetingsWith lists caller's events overlapping [from, to) (at most 62
// days) that have any of emails as a guest or organizer: events on their
// own calendar (organized or invited, declined ones left out) and on their
// visible connected Google and Outlook calendars in orgID's context (live,
// through the per-person cache; a platform that doesn't answer is skipped).
// Only what the caller sees on those calendars, never a calendar merely
// shared with them; agent calls drop events their scope hides. Without the
// calendar module it returns none.
func MeetingsWith(ctx context.Context, caller *auth.Identity, orgID uuid.UUID, emails []string, from, to time.Time) ([]GuestMeeting, error) {
	s := calendarService.Load()
	if s == nil || caller == nil || caller.UserID == uuid.Nil {
		return nil, nil
	}
	want := map[string]bool{}
	for _, e := range emails {
		if e = strings.ToLower(strings.TrimSpace(e)); e != "" {
			want[e] = true
		}
	}
	if len(want) == 0 || !to.After(from) {
		return []GuestMeeting{}, nil
	}
	if to.Sub(from) > maxWindow {
		to = from.Add(maxWindow)
	}
	readable, err := s.agentReadable(ctx, caller)
	if err != nil {
		return nil, err
	}
	out := []GuestMeeting{}

	own, err := s.occurrences(ctx, s.d.Pool, caller.UserID, from, to, maxOccurrences)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(own))
	for _, o := range own {
		ids = append(ids, o.EventID)
	}
	people, err := s.eventPeople(ctx, caller.UserID, ids)
	if err != nil {
		return nil, err
	}
	for _, o := range own {
		if (o.MyResponse != nil && *o.MyResponse == ResponseDeclined) || o.EventType != TypeDefault || !readable(KindEvent, o.EventID) {
			continue
		}
		guests := people[o.EventID]
		if !anyIn(guests, want) {
			continue
		}
		m := GuestMeeting{ID: o.EventID.String(), Title: o.Title, StartsAt: o.StartsAt, EndsAt: o.EndsAt, AllDay: o.AllDay,
			Location: o.Location, Guests: guests, Link: eventLink(o.EventID)}
		if o.OccurrenceStart != nil {
			occ := o.OccurrenceStart.UTC().Format(time.RFC3339)
			m.ID += "@" + occ
			m.Link += "?occurrence=" + occ
		}
		out = append(out, m)
	}

	var mu sync.Mutex
	platform := map[string]extEvent{}
	res, err := s.connectedRangeWith(ctx, caller, orgID, from, to, "", func(ref string, e extEvent) {
		mu.Lock()
		platform[ref] = e
		mu.Unlock()
	})
	if err != nil {
		return nil, err
	}
	for _, c := range res.Items {
		e, ok := platform[c.EventID]
		if !ok || e.Status == "cancelled" || (c.MyResponse != nil && *c.MyResponse == ResponseDeclined) {
			continue
		}
		guests := platformGuests(e)
		if !anyIn(guests, want) {
			continue
		}
		m := GuestMeeting{ID: c.EventID, Title: c.Title, StartsAt: c.StartsAt, EndsAt: c.EndsAt, AllDay: c.AllDay, Location: c.Location, Guests: guests,
			Link: "/calendar?view=day&date=" + c.StartsAt.UTC().Format(time.DateOnly)}
		if c.StartDate != nil {
			m.Link = "/calendar?view=day&date=" + *c.StartDate
		}
		if c.Source != nil {
			m.Source = productLabel(c.Source.Provider) + " · " + c.Source.Account
		}
		out = append(out, m)
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].StartsAt.Before(out[b].StartsAt) })
	return out, nil
}

// eventPeople are the emails of everyone on each event besides me: its
// organizer and guests (an invited colleague by their account's email).
func (s *Service) eventPeople(ctx context.Context, me uuid.UUID, ids []uuid.UUID) (map[uuid.UUID][]string, error) {
	out := map[uuid.UUID][]string{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT e.id, u.email FROM calendar_events e JOIN users u ON u.id = e.owner_id
		WHERE e.id = ANY($1) AND e.owner_id <> $2
		UNION ALL
		SELECT a.event_id, COALESCE(NULLIF(a.email, ''), u.email) FROM calendar_attendees a LEFT JOIN users u ON u.id = a.user_id
		WHERE a.event_id = ANY($1) AND a.user_id IS DISTINCT FROM $2`, ids, me)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[uuid.UUID]map[string]bool{}
	for rows.Next() {
		var id uuid.UUID
		var email *string
		if err := rows.Scan(&id, &email); err != nil {
			return nil, err
		}
		if email == nil || *email == "" {
			continue
		}
		key := strings.ToLower(*email)
		if seen[id] == nil {
			seen[id] = map[string]bool{}
		}
		if !seen[id][key] {
			seen[id][key] = true
			out[id] = append(out[id], *email)
		}
	}
	return out, rows.Err()
}

// platformGuests are a platform event's guests and organizer by email,
// leaving out the account itself and rooms.
func platformGuests(e extEvent) []string {
	var out []string
	seen := map[string]bool{}
	add := func(a extAttendee) {
		key := strings.ToLower(strings.TrimSpace(a.Email))
		if key == "" || a.Self || a.Resource || seen[key] {
			return
		}
		seen[key] = true
		out = append(out, a.Email)
	}
	if !e.OrganizerSelf {
		add(e.Organizer)
	}
	for _, a := range e.Attendees {
		add(a)
	}
	return out
}

func anyIn(emails []string, want map[string]bool) bool {
	for _, e := range emails {
		if want[strings.ToLower(e)] {
			return true
		}
	}
	return false
}

// BookingPage is a booking page anyone outside the workspace can open.
type BookingPage struct {
	ID    uuid.UUID
	Title string
	URL   string // absolute, on PUBLIC_WEB_URL
}

// PublicBookingLinks lists caller's active booking links whose audience is
// external (anyone can book; members-only links are never listed): their
// own links, and orgID's team links they made or host. A single-use link
// that was used is left out; agent calls drop links their scope hides.
// Empty without PUBLIC_WEB_URL (there is no address to give out).
func PublicBookingLinks(ctx context.Context, caller *auth.Identity, orgID uuid.UUID) ([]BookingPage, error) {
	s := calendarService.Load()
	if s == nil || caller == nil || caller.UserID == uuid.Nil {
		return nil, nil
	}
	if s.webURL("/") == "" {
		return []BookingPage{}, nil
	}
	readable, err := s.agentReadable(ctx, caller)
	if err != nil {
		return nil, err
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT l.id, l.title, l.slug FROM calendar_booking_links l
		WHERE l.deleted_at IS NULL AND l.active AND l.audience = 'external' AND NOT (l.single_use AND l.used_at IS NOT NULL)
		  AND ((l.team_id IS NULL AND l.owner_id = $1)
		    OR (l.team_id IS NOT NULL AND l.org_id = $2
		        AND (l.owner_id = $1 OR EXISTS (SELECT 1 FROM calendar_link_hosts h WHERE h.link_id = l.id AND h.user_id = $1 AND h.active))))
		ORDER BY l.created_at DESC, l.id DESC LIMIT 50`, caller.UserID, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BookingPage{}
	for rows.Next() {
		var l BookingPage
		var slug string
		if err := rows.Scan(&l.ID, &l.Title, &slug); err != nil {
			return nil, err
		}
		if !readable(KindBookingLink, l.ID) {
			continue
		}
		l.URL = s.webURL("/book/" + slug)
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
