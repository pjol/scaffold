package calendar

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// ---- personal iCal feed -------------------------------------------------------

func (s *Service) feedURL(token string) string {
	return s.apiURL("/v1/calendar/feed/" + token + ".ics")
}

// getFeed says whether the caller has a feed. The URL is shown once, when
// created; only its hash is stored.
func (s *Service) getFeed(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	var created time.Time
	err := s.d.Pool.QueryRow(r.Context(), `SELECT created_at FROM calendar_feeds WHERE user_id = $1`, id.UserID).Scan(&created)
	if db.IsNoRows(err) {
		httpx.OK(w, map[string]any{"active": false, "created_at": nil})
		return nil
	}
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"active": true, "created_at": created})
	return nil
}

// rotateFeed makes a new secret feed URL; any earlier one stops working.
func (s *Service) rotateFeed(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	token := randomToken(32)
	var created time.Time
	if err := s.d.Pool.QueryRow(r.Context(), `
		INSERT INTO calendar_feeds (user_id, token_hash) VALUES ($1, $2)
		ON CONFLICT (user_id) DO UPDATE SET token_hash = EXCLUDED.token_hash, created_at = now()
		RETURNING created_at`, id.UserID, hashToken(token)).Scan(&created); err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"url": s.feedURL(token), "created_at": created})
	return nil
}

func (s *Service) deleteFeed(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	if _, err := s.d.Pool.Exec(r.Context(), `DELETE FROM calendar_feeds WHERE user_id = $1`, id.UserID); err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

// feedICS serves the owner's calendar to other calendar apps: events they
// own and invitations they haven't declined, from 60 days ago, with
// recurring series as RRULEs. The token in the path is the credential.
func (s *Service) feedICS(w http.ResponseWriter, r *http.Request) error {
	token := chi.URLParam(r, "token")
	if token == "" || len(token) > 128 {
		return httpx.ErrNotFound
	}
	var userID uuid.UUID
	err := s.d.Pool.QueryRow(r.Context(), `
		SELECT f.user_id FROM calendar_feeds f JOIN users u ON u.id = f.user_id
		WHERE f.token_hash = $1 AND u.status IN ('active', 'suspended')`, hashToken(token)).Scan(&userID)
	if db.IsNoRows(err) {
		return httpx.ErrNotFound
	}
	if err != nil {
		return err
	}
	events, err := s.feedEvents(r.Context(), userID)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Cache-Control", "private, max-age=300")
	_, _ = w.Write(renderICS("PUBLISH", appName+" calendar", events))
	return nil
}

func (s *Service) feedEvents(ctx context.Context, userID uuid.UUID) ([]icsEvent, error) {
	since := time.Now().Add(-60 * 24 * time.Hour)
	rows, err := s.d.Pool.Query(ctx, `
		SELECT `+eventCols+` FROM calendar_events e JOIN users u ON u.id = e.owner_id
		LEFT JOIN calendar_attendees a ON a.event_id = e.id AND a.user_id = $1
		WHERE e.recurring_event_id IS NULL AND e.status = 'confirmed'
		  AND (e.owner_id = $1 OR (a.id IS NOT NULL AND a.response <> 'declined'))
		  AND ((e.rrule IS NULL AND e.ends_at > $2) OR (e.rrule IS NOT NULL AND (e.series_until IS NULL OR e.series_until > $2)))
		ORDER BY e.starts_at LIMIT 2000`, userID, since)
	if err != nil {
		return nil, err
	}
	var bases []eventRow
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		bases = append(bases, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var out []icsEvent
	for _, b := range bases {
		var exceptions []eventRow
		if b.RRule != nil {
			if exceptions, err = loadExceptions(ctx, s.d.Pool, b.ID); err != nil {
				return nil, err
			}
		}
		org, err := ownerContact(ctx, s.d.Pool, b.OwnerID)
		if err != nil {
			return nil, err
		}
		atts, err := loadFullAttendees(ctx, s.d.Pool, b.ID)
		if err != nil {
			return nil, err
		}
		people := make([]icsPerson, 0, len(atts))
		for _, a := range atts {
			people = append(people, icsPerson{Name: a.Name, Email: a.Email, Response: a.Response})
		}
		out = append(out, seriesVEvents(b, exceptions, &org, people)...)
	}
	return out, nil
}

// ---- external guests' RSVP ------------------------------------------------------

// GuestInvitation is what an external guest's RSVP page shows.
type GuestInvitation struct {
	Title         string     `json:"title"`
	Organizer     string     `json:"organizer"`
	StartsAt      time.Time  `json:"starts_at"`
	EndsAt        time.Time  `json:"ends_at"`
	AllDay        bool       `json:"all_day"`
	StartDate     *string    `json:"start_date"`
	EndDate       *string    `json:"end_date"`
	Timezone      string     `json:"timezone"`
	Repeats       string     `json:"repeats"`
	Location      string     `json:"location"`
	ConferenceURL *string    `json:"conference_url"`
	Description   string     `json:"description"`
	Name          string     `json:"name"`
	Response      string     `json:"response"`
	RespondedAt   *time.Time `json:"responded_at"`
}

func (s *Service) guestByToken(ctx context.Context, token string) (GuestInvitation, uuid.UUID, eventRow, string, error) {
	var gi GuestInvitation
	if token == "" || len(token) > 128 {
		return gi, uuid.Nil, eventRow{}, "", errInviteGone
	}
	var attendeeID, eventID uuid.UUID
	err := s.d.Pool.QueryRow(ctx, `SELECT id, event_id, name, response, responded_at FROM calendar_attendees
		WHERE token_hash = $1 AND user_id IS NULL`, hashToken(token)).Scan(&attendeeID, &eventID, &gi.Name, &gi.Response, &gi.RespondedAt)
	if db.IsNoRows(err) {
		return gi, uuid.Nil, eventRow{}, "", errInviteGone
	}
	if err != nil {
		return gi, uuid.Nil, eventRow{}, "", err
	}
	base, err := loadEventRow(ctx, s.d.Pool, eventID)
	if err != nil {
		return gi, uuid.Nil, eventRow{}, "", errInviteGone
	}
	loc := base.loc()
	gi.Title, gi.Organizer, gi.StartsAt, gi.EndsAt, gi.AllDay, gi.Timezone = base.Title, base.OwnerName, base.StartsAt, base.EndsAt, base.AllDay, base.Timezone
	gi.Location, gi.ConferenceURL, gi.Description = base.Location, base.ConferenceURL, base.Description
	if base.AllDay {
		gi.StartDate, gi.EndDate = allDayDates(base.StartsAt, base.EndsAt, loc)
	}
	if rule, ok := base.rule(); ok {
		gi.Repeats = describeRule(rule, loc)
		// Show the next occurrence of a series rather than its first.
		if next := rule.Expand(base.StartsAt, loc, time.Now(), time.Now().AddDate(2, 0, 0), 1, nil); len(next) == 1 {
			gi.StartsAt, gi.EndsAt = next[0], next[0].Add(base.duration())
		}
	}
	return gi, attendeeID, base, gi.Response, nil
}

var errInviteGone = httpx.NotFound("This invitation isn't available any more.")

func (s *Service) rsvpTokenGet(w http.ResponseWriter, r *http.Request) error {
	gi, _, _, _, err := s.guestByToken(r.Context(), chi.URLParam(r, "token"))
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "private, no-store")
	httpx.OK(w, gi)
	return nil
}

func (s *Service) rsvpTokenPost(w http.ResponseWriter, r *http.Request) error {
	token := chi.URLParam(r, "token")
	var in struct {
		Response string `json:"response"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	in.Response = strings.TrimSpace(in.Response)
	if in.Response != ResponseAccepted && in.Response != ResponseTentative && in.Response != ResponseDeclined {
		return httpx.BadRequest("response must be accepted, tentative or declined.")
	}
	gi, attendeeID, base, prev, err := s.guestByToken(r.Context(), token)
	if err != nil {
		return err
	}
	who := gi.Name
	if who == "" {
		who = "A guest"
	}
	if err := s.setResponse(r.Context(), base, attendeeID, prev, in.Response, who); err != nil {
		return err
	}
	kickOutbox()
	s.changed(r.Context(), "events", base.OwnerID)
	gi, _, _, _, err = s.guestByToken(r.Context(), token)
	if err != nil {
		return err
	}
	httpx.OK(w, gi)
	return nil
}
