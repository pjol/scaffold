package events

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/httpx"
)

// PublicHost is the host as shown on public pages: a name, no account id.
type PublicHost struct {
	DisplayName string  `json:"display_name"`
	AvatarURL   *string `json:"avatar_url"`
}

// PublicEvent is a public event for signed-out readers. No attendee data:
// not who is going, not how many.
type PublicEvent struct {
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
	CoverURL     *string    `json:"cover_url"`
	Status       string     `json:"status"`
	IsFull       bool       `json:"is_full"`
	Host         PublicHost `json:"host"`
	CancelledAt  *time.Time `json:"cancelled_at"`
	UpdatedAt    time.Time  `json:"updated_at"`

	sequence int
}

const publicCols = `e.id, e.title, e.details, e.starts_at, e.ends_at, e.timezone, e.location_text, e.lat, e.lng,
	e.online_url, e.cover_key, e.status,
	(e.capacity IS NOT NULL AND e.capacity <= (SELECT count(*) FROM event_rsvps r WHERE r.event_id = e.id AND r.status = 'going')),
	u.display_name, u.avatar_url, e.cancelled_at, e.updated_at, e.sequence`

func (s *Service) queryPublic(ctx context.Context, sql string, args ...any) ([]PublicEvent, error) {
	rows, err := s.d.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PublicEvent{}
	for rows.Next() {
		var e PublicEvent
		var cover *string
		if err := rows.Scan(&e.ID, &e.Title, &e.Details, &e.StartsAt, &e.EndsAt, &e.Timezone, &e.LocationText,
			&e.Lat, &e.Lng, &e.OnlineURL, &cover, &e.Status, &e.IsFull, &e.Host.DisplayName, &e.Host.AvatarURL,
			&e.CancelledAt, &e.UpdatedAt, &e.sequence); err != nil {
			return nil, err
		}
		if cover != nil {
			u := s.d.Storage.PublicURL(*cover)
			e.CoverURL = &u
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// publicList is upcoming public events, soonest first, keyset on
// (starts_at, id).
func (s *Service) publicList(w http.ResponseWriter, r *http.Request) error {
	p := httpx.ParsePage(r)
	var c cursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	list, err := s.queryPublic(r.Context(), `
		SELECT `+publicCols+` FROM events e JOIN users u ON u.id = e.host_id
		WHERE e.visibility = 'public' AND e.status = 'scheduled' AND e.ends_at > now()
		  AND ($1::timestamptz IS NULL OR (e.starts_at, e.id) > ($1, $2::uuid))
		ORDER BY e.starts_at, e.id LIMIT $3`, nullTime(c.T), c.ID, p.Limit+1)
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(list, p.Limit, func(e PublicEvent) any { return cursor{e.StartsAt, e.ID} }))
	return nil
}

func (s *Service) loadPublic(r *http.Request) (PublicEvent, error) {
	eid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return PublicEvent{}, err
	}
	list, err := s.queryPublic(r.Context(), `
		SELECT `+publicCols+` FROM events e JOIN users u ON u.id = e.host_id
		WHERE e.id = $1 AND e.visibility = 'public'`, eid)
	if err != nil {
		return PublicEvent{}, err
	}
	if len(list) == 0 {
		return PublicEvent{}, errNotFound
	}
	return list[0], nil
}

func (s *Service) publicGet(w http.ResponseWriter, r *http.Request) error {
	e, err := s.loadPublic(r)
	if err != nil {
		return err
	}
	httpx.OK(w, e)
	return nil
}

func (s *Service) publicICS(w http.ResponseWriter, r *http.Request) error {
	e, err := s.loadPublic(r)
	if err != nil {
		return err
	}
	s.writeICS(w, e.Title, []icsEvent{{
		id: e.ID, title: e.Title, details: e.Details, place: e.LocationText, lat: e.Lat, lng: e.Lng,
		online: e.OnlineURL, status: e.Status, startsAt: e.StartsAt, endsAt: e.EndsAt, updatedAt: e.UpdatedAt, sequence: e.sequence,
	}}, true)
	return nil
}
