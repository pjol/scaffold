package events

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
)

// Search implements app.SearchProvider: events the caller can see under the
// rules of GET /events (visibility, plus cancelled events only for their
// host and people who answered), matching the title, details or location,
// past or upcoming. Upcoming events rank first, nearest first. For an agent
// call each event must also be readable at the caller's agent scope.
func (s *Service) Search(ctx context.Context, caller *auth.Identity, query string, limit int) ([]app.SearchResult, error) {
	q := strings.TrimSpace(query)
	like := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q) + "%"
	rows, err := s.d.Pool.Query(ctx, `
		SELECT e.id, e.title, e.location_text, e.starts_at, e.ends_at, e.status, e.timezone
		FROM events e
		WHERE `+visibleSQL+`
		  AND (e.status = 'scheduled' OR e.host_id = $1
		       OR EXISTS (SELECT 1 FROM event_rsvps r WHERE r.event_id = e.id AND r.user_id = $1))
		  AND (e.title ILIKE $2 ESCAPE '\' OR e.details ILIKE $2 ESCAPE '\' OR e.location_text ILIKE $2 ESCAPE '\')
		ORDER BY (e.ends_at > now()) DESC, abs(extract(epoch FROM e.starts_at - now())), e.id
		LIMIT $3`, caller.UserID, like, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type hit struct {
		id                   uuid.UUID
		title, place, status string
		zone                 string
		starts, ends         time.Time
	}
	var hits []hit
	for rows.Next() {
		var h hit
		if err := rows.Scan(&h.id, &h.title, &h.place, &h.starts, &h.ends, &h.status, &h.zone); err != nil {
			return nil, err
		}
		hits = append(hits, h)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	ids := make([]string, len(hits))
	for i, h := range hits {
		ids[i] = h.id.String()
	}
	readable, err := app.AgentReadable(ctx, s.d.Modules, caller, agentFeature, agentKind, ids)
	if err != nil {
		return nil, err
	}
	lq := strings.ToLower(q)
	out := []app.SearchResult{}
	for _, h := range hits {
		if !readable[h.id.String()] {
			continue
		}
		score := 0.5
		switch lt := strings.ToLower(h.title); {
		case lt == lq:
			score = 0.9
		case strings.HasPrefix(lt, lq):
			score = 0.8
		case strings.Contains(lt, lq):
			score = 0.7
		}
		if h.ends.Before(time.Now()) {
			score -= 0.1 // past events rank under upcoming ones with the same match
		}
		loc, err := time.LoadLocation(h.zone)
		if err != nil {
			loc = time.UTC
		}
		snippet := h.starts.In(loc).Format("Mon Jan 2, 2006 15:04 MST") // in the event's own zone
		if h.status == "cancelled" {
			snippet = "Cancelled · " + snippet
		}
		if h.place != "" {
			snippet += " · " + h.place
		}
		out = append(out, app.SearchResult{
			Kind: "events.event", ID: h.id.String(), Title: h.title, Snippet: snippet,
			Link: "/events/" + h.id.String(), Icon: "calendar-days", Score: score, At: h.starts,
		})
	}
	return out, nil
}
