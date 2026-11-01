package events

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/bg"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/notify"
)

// Person is a user as other users see them.
type Person struct {
	ID          uuid.UUID `json:"id"`
	DisplayName string    `json:"display_name"`
	AvatarURL   *string   `json:"avatar_url"`
}

// MyRSVP is the caller's own RSVP.
type MyRSVP struct {
	Status           string `json:"status"` // going | maybe | declined | waitlisted
	WaitlistPosition *int   `json:"waitlist_position"`
}

// Event is an event as a signed-in user sees it.
type Event struct {
	ID            uuid.UUID  `json:"id"`
	Title         string     `json:"title"`
	Details       string     `json:"details"`
	StartsAt      time.Time  `json:"starts_at"`
	EndsAt        time.Time  `json:"ends_at"`
	Timezone      string     `json:"timezone"` // IANA zone for display; times are UTC
	LocationText  string     `json:"location_text"`
	Lat           *float64   `json:"lat"`
	Lng           *float64   `json:"lng"`
	OnlineURL     *string    `json:"online_url"`
	CoverKey      *string    `json:"cover_key"`
	CoverURL      *string    `json:"cover_url"`
	Capacity      *int       `json:"capacity"`
	Visibility    string     `json:"visibility"` // public | members | invite
	Status        string     `json:"status"`     // scheduled | cancelled
	Host          Person     `json:"host"`
	CoHosts       []Person   `json:"co_hosts"`
	GoingCount    int        `json:"going_count"`
	MaybeCount    int        `json:"maybe_count"`
	WaitlistCount int        `json:"waitlist_count"`
	IsFull        bool       `json:"is_full"`
	MyRSVP        *MyRSVP    `json:"my_rsvp"`
	HostRole      *string    `json:"host_role"`             // creator | cohost | null
	InviteCode    *string    `json:"invite_code,omitempty"` // hosts only
	CancelledAt   *time.Time `json:"cancelled_at"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`

	sequence int
}

const eventCols = `e.id, e.title, e.details, e.starts_at, e.ends_at, e.timezone, e.location_text, e.lat, e.lng,
	e.online_url, e.cover_key, e.capacity, e.visibility, e.status, e.invite_code, e.sequence, e.cancelled_at,
	e.created_at, e.updated_at, e.host_id, u.display_name, u.avatar_url`

func scanEvents(rows pgx.Rows) ([]Event, error) {
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.Title, &e.Details, &e.StartsAt, &e.EndsAt, &e.Timezone, &e.LocationText,
			&e.Lat, &e.Lng, &e.OnlineURL, &e.CoverKey, &e.Capacity, &e.Visibility, &e.Status, &e.InviteCode,
			&e.sequence, &e.CancelledAt, &e.CreatedAt, &e.UpdatedAt, &e.Host.ID, &e.Host.DisplayName, &e.Host.AvatarURL); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// visibleSQL is the list filter for "events $1 may see" (alias e).
const visibleSQL = `(e.visibility IN ('public', 'members') OR e.host_id = $1
	OR EXISTS (SELECT 1 FROM event_cohosts h WHERE h.event_id = e.id AND h.user_id = $1)
	OR EXISTS (SELECT 1 FROM event_invites i WHERE i.event_id = e.id AND i.user_id = $1)
	OR EXISTS (SELECT 1 FROM event_rsvps r WHERE r.event_id = e.id AND r.user_id = $1))`

// access is a user's relation to one event.
type access struct {
	creator, cohost, invited bool
	rsvp                     string // "" when none
}

func (a access) host() bool { return a.creator || a.cohost }

func (a access) attendee() bool {
	return a.rsvp == RSVPGoing || a.rsvp == RSVPMaybe || a.rsvp == RSVPWaitlisted
}

func (a access) canView(visibility string) bool {
	return visibility != VisibilityInvite || a.host() || a.invited || a.rsvp != ""
}

func loadAccess(ctx context.Context, q db.Querier, eventID, hostID, userID uuid.UUID) (access, error) {
	a := access{creator: hostID == userID}
	err := q.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM event_cohosts WHERE event_id = $1 AND user_id = $2),
		       EXISTS (SELECT 1 FROM event_invites WHERE event_id = $1 AND user_id = $2),
		       COALESCE((SELECT status FROM event_rsvps WHERE event_id = $1 AND user_id = $2), '')`,
		eventID, userID).Scan(&a.cohost, &a.invited, &a.rsvp)
	return a, err
}

var errNotFound = httpx.NotFound("That event isn't available.")

// loadEvent returns one event the viewer can see, hydrated, plus their access.
func (s *Service) loadEvent(ctx context.Context, id, viewerID uuid.UUID) (Event, access, error) {
	rows, err := s.d.Pool.Query(ctx, `
		SELECT `+eventCols+` FROM events e JOIN users u ON u.id = e.host_id WHERE e.id = $1`, id)
	if err != nil {
		return Event{}, access{}, err
	}
	list, err := scanEvents(rows)
	if err != nil {
		return Event{}, access{}, err
	}
	if len(list) == 0 {
		return Event{}, access{}, errNotFound
	}
	a, err := loadAccess(ctx, s.d.Pool, id, list[0].Host.ID, viewerID)
	if err != nil {
		return Event{}, access{}, err
	}
	if !a.canView(list[0].Visibility) {
		return Event{}, access{}, errNotFound
	}
	if err := s.hydrate(ctx, viewerID, list); err != nil {
		return Event{}, access{}, err
	}
	return list[0], a, nil
}

// hydrate fills counts, co-hosts, the viewer's RSVP and host-only fields for
// a page of events in a fixed number of queries.
func (s *Service) hydrate(ctx context.Context, viewerID uuid.UUID, list []Event) error {
	if len(list) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(list))
	idx := make(map[uuid.UUID]int, len(list))
	for i := range list {
		ids[i] = list[i].ID
		idx[list[i].ID] = i
		list[i].CoHosts = []Person{}
		if list[i].CoverKey != nil {
			u := s.d.Storage.PublicURL(*list[i].CoverKey)
			list[i].CoverURL = &u
		}
		if list[i].Host.ID == viewerID {
			r := "creator"
			list[i].HostRole = &r
		}
	}

	rows, err := s.d.Pool.Query(ctx, `
		SELECT event_id, status, count(*) FROM event_rsvps WHERE event_id = ANY($1) GROUP BY event_id, status`, ids)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id uuid.UUID
		var status string
		var n int
		if err := rows.Scan(&id, &status, &n); err != nil {
			rows.Close()
			return err
		}
		e := &list[idx[id]]
		switch status {
		case RSVPGoing:
			e.GoingCount = n
		case RSVPMaybe:
			e.MaybeCount = n
		case RSVPWaitlisted:
			e.WaitlistCount = n
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	rows, err = s.d.Pool.Query(ctx, `
		SELECT h.event_id, u.id, u.display_name, u.avatar_url
		FROM event_cohosts h JOIN users u ON u.id = h.user_id
		WHERE h.event_id = ANY($1) ORDER BY h.event_id, h.created_at`, ids)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id uuid.UUID
		var p Person
		if err := rows.Scan(&id, &p.ID, &p.DisplayName, &p.AvatarURL); err != nil {
			rows.Close()
			return err
		}
		e := &list[idx[id]]
		e.CoHosts = append(e.CoHosts, p)
		if p.ID == viewerID && e.HostRole == nil {
			r := "cohost"
			e.HostRole = &r
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	rows, err = s.d.Pool.Query(ctx, `
		SELECT r.event_id, r.status,
		       CASE WHEN r.status = 'waitlisted' THEN (
		         SELECT count(*) FROM event_rsvps w
		         WHERE w.event_id = r.event_id AND w.status = 'waitlisted'
		           AND (w.waitlisted_at, w.user_id) <= (r.waitlisted_at, r.user_id)) END
		FROM event_rsvps r WHERE r.event_id = ANY($1) AND r.user_id = $2`, ids, viewerID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id uuid.UUID
		var m MyRSVP
		var pos *int
		if err := rows.Scan(&id, &m.Status, &pos); err != nil {
			rows.Close()
			return err
		}
		m.WaitlistPosition = pos
		list[idx[id]].MyRSVP = &m
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for i := range list {
		e := &list[i]
		e.IsFull = e.Capacity != nil && e.GoingCount >= *e.Capacity
		if e.HostRole == nil {
			e.InviteCode = nil
		}
	}
	return nil
}

// ---- side effects ----------------------------------------------------------

// publishCounts sends the event's RSVP counts on its topic. Best-effort.
func (s *Service) publishCounts(ctx context.Context, id uuid.UUID) {
	var going, maybe, waitlist int
	var capacity *int
	if err := s.d.Pool.QueryRow(ctx, `
		SELECT (SELECT capacity FROM events WHERE id = $1),
		       count(*) FILTER (WHERE status = 'going'), count(*) FILTER (WHERE status = 'maybe'),
		       count(*) FILTER (WHERE status = 'waitlisted')
		FROM event_rsvps WHERE event_id = $1`, id).Scan(&capacity, &going, &maybe, &waitlist); err != nil {
		slog.WarnContext(ctx, "events: counts for realtime", "err", err)
		return
	}
	s.publish(ctx, id, EventRSVP, map[string]any{
		"event_id": id, "going_count": going, "maybe_count": maybe, "waitlist_count": waitlist,
		"is_full": capacity != nil && going >= *capacity,
	})
}

func (s *Service) publish(ctx context.Context, id uuid.UUID, event string, data any) {
	if err := s.d.Broker.Publish(ctx, "events:"+id.String(), event, data); err != nil {
		slog.WarnContext(ctx, "events: realtime publish failed", "event", event, "err", err)
	}
}

// afterCommit runs fn in the background (bg.Go), detached from the request
// (notifications, cleanup). name is "events.<what>" for the logs.
func (s *Service) afterCommit(ctx context.Context, name string, fn func(ctx context.Context)) {
	bg.Go(ctx, name, func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		fn(ctx)
		return nil
	})
}

// notifyAll sends n to each user except the actor. Best-effort.
func (s *Service) notifyAll(ctx context.Context, actor uuid.UUID, users []uuid.UUID, n notify.Notification) {
	for _, u := range users {
		if u == actor || u == uuid.Nil {
			continue
		}
		m := n
		m.UserID = u
		if err := s.d.Notify.Send(ctx, m); err != nil {
			slog.WarnContext(ctx, "events: notify failed", "kind", n.Kind, "err", err)
		}
	}
}

func eventLink(id uuid.UUID) string { return "/events/" + id.String() }

// attendeeIDs returns users with a going, maybe or waitlisted RSVP.
func attendeeIDs(ctx context.Context, q db.Querier, eventID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := q.Query(ctx, `
		SELECT user_id FROM event_rsvps WHERE event_id = $1 AND status IN ('going', 'maybe', 'waitlisted')`, eventID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

// whenText renders a start time in the event's zone: "Sat, Nov 1 at 7:00 PM".
func whenText(t time.Time, zone string) string {
	loc, err := time.LoadLocation(zone)
	if err != nil {
		loc = time.UTC
	}
	return t.In(loc).Format("Mon, Jan 2 at 3:04 PM") + " (" + zoneLabel(t.In(loc)) + ")"
}

func zoneLabel(t time.Time) string {
	name, _ := t.Zone()
	return name
}
