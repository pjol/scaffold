package events

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// icsEvent is what a VEVENT needs.
type icsEvent struct {
	id                    uuid.UUID
	title, details, place string
	lat, lng              *float64
	online                *string
	status                string
	startsAt, endsAt      time.Time
	updatedAt             time.Time
	sequence              int
}

func toICS(e Event) icsEvent {
	return icsEvent{id: e.ID, title: e.Title, details: e.Details, place: e.LocationText, lat: e.Lat, lng: e.Lng,
		online: e.OnlineURL, status: e.Status, startsAt: e.StartsAt, endsAt: e.EndsAt, updatedAt: e.UpdatedAt, sequence: e.sequence}
}

// writeICS renders a VCALENDAR (RFC 5545): UTC times, escaped text, lines
// folded at 75 octets, CRLF endings.
func (s *Service) writeICS(w http.ResponseWriter, name string, events []icsEvent, download bool) {
	var b strings.Builder
	line := func(l string) { foldLine(&b, l) }
	line("BEGIN:VCALENDAR")
	line("VERSION:2.0")
	line("PRODID:-//Scaffold App//Events//EN")
	line("CALSCALE:GREGORIAN")
	line("METHOD:PUBLISH")
	line("X-WR-CALNAME:" + escapeText(name))
	now := time.Now().UTC().Format("20060102T150405Z")
	for _, e := range events {
		line("BEGIN:VEVENT")
		line("UID:" + e.id.String() + "@events.scaffoldapp")
		line("DTSTAMP:" + now)
		line("LAST-MODIFIED:" + e.updatedAt.UTC().Format("20060102T150405Z"))
		line("SEQUENCE:" + strconv.Itoa(e.sequence))
		line("DTSTART:" + e.startsAt.UTC().Format("20060102T150405Z"))
		line("DTEND:" + e.endsAt.UTC().Format("20060102T150405Z"))
		line("SUMMARY:" + escapeText(e.title))
		desc := e.details
		if link := s.webLink(e.id); link != "" {
			desc = strings.TrimSpace(desc + "\n\n" + link)
			line("URL:" + link)
		}
		if e.online != nil {
			desc = strings.TrimSpace(desc + "\n\nJoin online: " + *e.online)
		}
		if desc != "" {
			line("DESCRIPTION:" + escapeText(desc))
		}
		place := e.place
		if place == "" && e.online != nil {
			place = *e.online
		}
		if place != "" {
			line("LOCATION:" + escapeText(place))
		}
		if e.lat != nil && e.lng != nil {
			line("GEO:" + strconv.FormatFloat(*e.lat, 'f', 6, 64) + ";" + strconv.FormatFloat(*e.lng, 'f', 6, 64))
		}
		if e.status == "cancelled" {
			line("STATUS:CANCELLED")
		} else {
			line("STATUS:CONFIRMED")
		}
		line("END:VEVENT")
	}
	line("END:VCALENDAR")
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Cache-Control", "private, max-age=300")
	if download {
		w.Header().Set("Content-Disposition", `attachment; filename="event.ics"`)
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(b.String()))
}

func (s *Service) webLink(id uuid.UUID) string {
	base := strings.TrimRight(s.d.Cfg.PublicWebURL, "/")
	if base == "" {
		return ""
	}
	return base + eventLink(id)
}

func escapeText(v string) string {
	r := strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\r\n", `\n`, "\n", `\n`, "\r", `\n`)
	return r.Replace(v)
}

// foldLine writes l with CRLF, folding at 75 octets on rune boundaries.
func foldLine(b *strings.Builder, l string) {
	limit := 75
	for len(l) > limit {
		cut := limit
		for cut > 0 && !utf8.RuneStart(l[cut]) {
			cut--
		}
		b.WriteString(l[:cut])
		b.WriteString("\r\n ")
		l = l[cut:]
		limit = 74 // the leading space counts
	}
	b.WriteString(l)
	b.WriteString("\r\n")
}

// eventICS downloads one event the caller can see.
func (s *Service) eventICS(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	eid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	e, _, err := s.loadEvent(r.Context(), eid, id.UserID)
	if err != nil {
		return err
	}
	s.writeICS(w, e.Title, []icsEvent{toICS(e)}, true)
	return nil
}

// ---- personal calendar feed ------------------------------------------------

func hashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

func (s *Service) feedURL(token string) string {
	return strings.TrimRight(s.d.Cfg.PublicAPIURL, "/") + "/v1/events/feed/" + token + ".ics"
}

// getFeedTokenHandler says whether the caller has a calendar feed. The URL
// itself is shown once, when created; only its hash is stored.
func (s *Service) getFeedTokenHandler(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	var created time.Time
	err := s.d.Pool.QueryRow(r.Context(), `SELECT created_at FROM event_calendar_feeds WHERE user_id = $1`, id.UserID).Scan(&created)
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

// rotateFeedTokenHandler creates the caller's calendar feed URL, replacing
// any earlier one (which stops working).
func (s *Service) rotateFeedTokenHandler(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	token := randomToken(32)
	var created time.Time
	if err := s.d.Pool.QueryRow(r.Context(), `
		INSERT INTO event_calendar_feeds (user_id, token_hash) VALUES ($1, $2)
		ON CONFLICT (user_id) DO UPDATE SET token_hash = EXCLUDED.token_hash, created_at = now()
		RETURNING created_at`, id.UserID, hashToken(token)).Scan(&created); err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"url": s.feedURL(token), "created_at": created})
	return nil
}

func (s *Service) deleteFeedTokenHandler(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	if _, err := s.d.Pool.Exec(r.Context(), `DELETE FROM event_calendar_feeds WHERE user_id = $1`, id.UserID); err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

// calendarFeed serves a user's events (hosting, going, maybe, waitlisted)
// to calendar apps. The token in the path is the credential.
func (s *Service) calendarFeed(w http.ResponseWriter, r *http.Request) error {
	token := chi.URLParam(r, "token")
	if token == "" || len(token) > 128 {
		return httpx.ErrNotFound
	}
	var userID uuid.UUID
	err := s.d.Pool.QueryRow(r.Context(), `
		SELECT f.user_id FROM event_calendar_feeds f JOIN users u ON u.id = f.user_id
		WHERE f.token_hash = $1 AND u.status IN ('active', 'suspended')`, hashToken(token)).Scan(&userID)
	if db.IsNoRows(err) {
		return httpx.ErrNotFound
	}
	if err != nil {
		return err
	}
	list, err := s.feedEvents(r.Context(), userID)
	if err != nil {
		return err
	}
	s.writeICS(w, "Scaffold App events", list, false)
	return nil
}

func (s *Service) feedEvents(ctx context.Context, userID uuid.UUID) ([]icsEvent, error) {
	rows, err := s.d.Pool.Query(ctx, `
		SELECT e.id, e.title, e.details, e.location_text, e.lat, e.lng, e.online_url, e.status,
		       e.starts_at, e.ends_at, e.updated_at, e.sequence
		FROM events e
		WHERE e.ends_at > $2
		  AND (e.host_id = $1
		       OR EXISTS (SELECT 1 FROM event_cohosts h WHERE h.event_id = e.id AND h.user_id = $1)
		       OR EXISTS (SELECT 1 FROM event_rsvps r WHERE r.event_id = e.id AND r.user_id = $1
		                  AND r.status IN ('going', 'maybe', 'waitlisted')))
		ORDER BY e.starts_at, e.id LIMIT $3`, userID, time.Now().Add(-feedPast), feedMax)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []icsEvent
	for rows.Next() {
		var e icsEvent
		if err := rows.Scan(&e.id, &e.title, &e.details, &e.place, &e.lat, &e.lng, &e.online, &e.status,
			&e.startsAt, &e.endsAt, &e.updatedAt, &e.sequence); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
