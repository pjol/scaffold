package calendar

import (
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
)

// SearchHit is an event found by the calendar's search box: its next
// occurrence (or its last, when it's over).
type SearchHit struct {
	EventID    uuid.UUID  `json:"event_id"`
	Title      string     `json:"title"`
	Location   string     `json:"location"`
	StartsAt   time.Time  `json:"starts_at"`
	EndsAt     time.Time  `json:"ends_at"`
	AllDay     bool       `json:"all_day"`
	Timezone   string     `json:"timezone"`
	Recurring  bool       `json:"recurring"`
	CalendarID *uuid.UUID `json:"calendar_id"`
	Occurrence *time.Time `json:"occurrence_start"`
}

// searchHandler finds events by title, place or description among the
// caller's own, their invitations, and events on calendars shared with
// them with details (free/busy-only calendars never match).
func (s *Service) searchHandler(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id := auth.MustFromContext(ctx)
	q, _ := cleanText(r.URL.Query().Get("q"), 100)
	if len([]rune(q)) < 2 {
		httpx.OK(w, map[string]any{"items": []SearchHit{}})
		return nil
	}
	readable, err := s.agentReadable(ctx, id)
	if err != nil {
		return err
	}
	like := likeEscape(q)
	rows, err := s.d.Pool.Query(ctx, `
		SELECT `+eventCols+` FROM calendar_events e JOIN users u ON u.id = e.owner_id
		LEFT JOIN calendar_attendees a ON a.event_id = e.id AND a.user_id = $1
		LEFT JOIN access_visible($1, 'calendar.calendar', $4) v ON v.id = e.calendar_id
		WHERE e.recurring_event_id IS NULL AND e.status = 'confirmed'
		  AND (e.owner_id = $1 OR a.id IS NOT NULL OR v.level >= 2)
		  AND (e.title ILIKE '%' || $2 || '%' OR e.location ILIKE '%' || $2 || '%' OR e.description ILIKE '%' || $2 || '%')
		ORDER BY (e.title ILIKE $2 || '%') DESC, abs(extract(epoch FROM e.starts_at - now())), e.id LIMIT $3`,
		id.UserID, like, 50, access.ViaAgent(ctx))
	if err != nil {
		return err
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (eventRow, error) { return scanEvent(row) })
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	out := []SearchHit{}
	for _, e := range list {
		if !readable(KindEvent, e.ID) {
			continue
		}
		h := SearchHit{EventID: e.ID, Title: e.Title, Location: e.Location, StartsAt: e.StartsAt, EndsAt: e.EndsAt, AllDay: e.AllDay,
			Timezone: e.Timezone, Recurring: e.RRule != nil, CalendarID: e.CalendarID}
		if rule, ok := e.rule(); ok {
			dur := e.duration()
			next := rule.Expand(e.StartsAt, e.loc(), now.Add(-dur), now.Add(2*366*24*time.Hour), 1, nil)
			if len(next) == 0 {
				if last, ok := rule.Last(e.StartsAt, e.loc()); ok {
					next = []time.Time{last}
				}
			}
			if len(next) > 0 {
				st := next[0]
				h.StartsAt, h.EndsAt, h.Occurrence = st, st.Add(dur), &st
			}
		}
		out = append(out, h)
		if len(out) >= 20 {
			break
		}
	}
	httpx.OK(w, map[string]any{"items": out})
	return nil
}
