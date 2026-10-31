package events

import (
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/audit"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// AdminEvent is an event as staff see it in the console list.
type AdminEvent struct {
	ID            uuid.UUID  `json:"id"`
	Title         string     `json:"title"`
	StartsAt      time.Time  `json:"starts_at"`
	EndsAt        time.Time  `json:"ends_at"`
	Timezone      string     `json:"timezone"`
	Visibility    string     `json:"visibility"`
	Status        string     `json:"status"`
	Capacity      *int       `json:"capacity"`
	Host          Person     `json:"host"`
	GoingCount    int        `json:"going_count"`
	WaitlistCount int        `json:"waitlist_count"`
	CancelledAt   *time.Time `json:"cancelled_at"`
	CreatedAt     time.Time  `json:"created_at"`
}

// adminList lists every event, newest start first. Filters: q (title or an
// event id), host_id, status.
func (s *Service) adminList(w http.ResponseWriter, r *http.Request) error {
	p := httpx.ParsePage(r)
	var c cursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	q := r.URL.Query()
	text := strings.TrimSpace(q.Get("q"))
	var host *uuid.UUID
	if h := q.Get("host_id"); h != "" {
		id, err := uuid.Parse(h)
		if err != nil {
			return httpx.BadRequest("Invalid host_id.")
		}
		host = &id
	}
	status := q.Get("status")
	if status != "" && status != "scheduled" && status != "cancelled" {
		return httpx.BadRequest("Status must be scheduled or cancelled.")
	}
	rows, err := s.d.Pool.Query(r.Context(), `
		SELECT e.id, e.title, e.starts_at, e.ends_at, e.timezone, e.visibility, e.status, e.capacity,
		       u.id, u.display_name, u.avatar_url,
		       (SELECT count(*) FROM event_rsvps x WHERE x.event_id = e.id AND x.status = 'going'),
		       (SELECT count(*) FROM event_rsvps x WHERE x.event_id = e.id AND x.status = 'waitlisted'),
		       e.cancelled_at, e.created_at
		FROM events e JOIN users u ON u.id = e.host_id
		WHERE ($1::text = '' OR e.title ILIKE '%' || $1 || '%' OR e.id::text = $1)
		  AND ($2::uuid IS NULL OR e.host_id = $2)
		  AND ($3::text = '' OR e.status = $3)
		  AND ($4::timestamptz IS NULL OR (e.starts_at, e.id) < ($4, $5::uuid))
		ORDER BY e.starts_at DESC, e.id DESC LIMIT $6`,
		likeEscape(text), host, status, nullTime(c.T), c.ID, p.Limit+1)
	if err != nil {
		return err
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (AdminEvent, error) {
		var e AdminEvent
		err := row.Scan(&e.ID, &e.Title, &e.StartsAt, &e.EndsAt, &e.Timezone, &e.Visibility, &e.Status, &e.Capacity,
			&e.Host.ID, &e.Host.DisplayName, &e.Host.AvatarURL, &e.GoingCount, &e.WaitlistCount, &e.CancelledAt, &e.CreatedAt)
		return e, err
	})
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(list, p.Limit, func(e AdminEvent) any { return cursor{e.StartsAt, e.ID} }))
	return nil
}

type adminCancelReq struct {
	Reason string `json:"reason"`
}

// adminCancel cancels any scheduled event (audited); attendees are told.
func (s *Service) adminCancel(w http.ResponseWriter, r *http.Request) error {
	staff := auth.MustFromContext(r.Context())
	ctx := r.Context()
	eid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req adminCancelReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	var title string
	var users []uuid.UUID
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var err error
		title, users, err = cancelEvent(ctx, tx, eid, nil)
		if db.IsNoRows(err) {
			return httpx.NotFound("No scheduled event with that id.")
		}
		if err != nil {
			return err
		}
		return audit.Log(ctx, tx, staff.UserID, "events.event.cancel", "event", eid.String(),
			map[string]any{"reason": strings.TrimSpace(req.Reason)})
	})
	if err != nil {
		return err
	}
	s.afterCancel(ctx, staff.UserID, eid, title, "This event was cancelled.", users)
	httpx.NoContent(w)
	return nil
}

func likeEscape(q string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q)
}
