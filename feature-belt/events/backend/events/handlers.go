package events

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/notify"
	"scaffoldapp/backend/internal/storage"
)

type cursor struct {
	T  time.Time `json:"t"`
	ID uuid.UUID `json:"id"`
}

type listFilter struct {
	when     string // upcoming | past
	from, to *time.Time
	mine     string // "" | hosting | attending
	limit    int
	cursor   string
}

// listEvents lists events the viewer can see. Upcoming (not yet ended) runs
// soonest first; past runs most recent first; a from/to window runs in start
// order and overrides when. Keyset on (starts_at, id). Cancelled events show
// only to their hosts and people who RSVP'd.
func (s *Service) listEvents(ctx context.Context, viewerID uuid.UUID, f listFilter) (httpx.Page[Event], error) {
	var c cursor
	if err := httpx.DecodeCursor(f.cursor, &c); err != nil {
		return httpx.Page[Event]{}, err
	}
	asc := f.when != "past" || f.from != nil
	order, cmp := "ASC", ">"
	if !asc {
		order, cmp = "DESC", "<"
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT `+eventCols+`
		FROM events e JOIN users u ON u.id = e.host_id
		WHERE `+visibleSQL+`
		  AND (e.status = 'scheduled' OR e.host_id = $1
		       OR EXISTS (SELECT 1 FROM event_rsvps r WHERE r.event_id = e.id AND r.user_id = $1))
		  AND CASE WHEN $2::timestamptz IS NOT NULL THEN e.starts_at < $3 AND e.ends_at > $2
		           WHEN $4::text = 'past' THEN e.ends_at <= now()
		           ELSE e.ends_at > now() END
		  AND ($5::text = '' OR ($5 = 'hosting' AND (e.host_id = $1
		         OR EXISTS (SELECT 1 FROM event_cohosts h WHERE h.event_id = e.id AND h.user_id = $1)))
		       OR ($5 = 'attending' AND EXISTS (SELECT 1 FROM event_rsvps r WHERE r.event_id = e.id
		         AND r.user_id = $1 AND r.status IN ('going', 'maybe', 'waitlisted'))))
		  AND ($6::timestamptz IS NULL OR (e.starts_at, e.id) `+cmp+` ($6, $7::uuid))
		ORDER BY e.starts_at `+order+`, e.id `+order+` LIMIT $8`,
		viewerID, f.from, f.to, f.when, f.mine, nullTime(c.T), c.ID, f.limit+1)
	if err != nil {
		return httpx.Page[Event]{}, err
	}
	list, err := scanEvents(rows)
	if err != nil {
		return httpx.Page[Event]{}, err
	}
	page := httpx.NewPage(list, f.limit, func(e Event) any { return cursor{e.StartsAt, e.ID} })
	if err := s.hydrate(ctx, viewerID, page.Items); err != nil {
		return httpx.Page[Event]{}, err
	}
	return page, nil
}

func (s *Service) listHandler(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	q := r.URL.Query()
	p := httpx.ParsePage(r)
	f := listFilter{when: q.Get("when"), mine: q.Get("filter"), limit: p.Limit, cursor: p.Cursor}
	switch f.when {
	case "", "upcoming", "past":
	default:
		return httpx.BadRequest("when must be upcoming or past.")
	}
	switch f.mine {
	case "", "all":
		f.mine = ""
	case "hosting", "attending":
	default:
		return httpx.BadRequest("filter must be all, hosting or attending.")
	}
	if q.Get("from") != "" || q.Get("to") != "" {
		from, err1 := time.Parse(time.RFC3339, q.Get("from"))
		to, err2 := time.Parse(time.RFC3339, q.Get("to"))
		if err1 != nil || err2 != nil || !to.After(from) || to.Sub(from) > maxWindow {
			return httpx.BadRequest("from and to must be RFC 3339 times, to after from, at most 100 days apart.")
		}
		f.from, f.to = &from, &to
	}
	page, err := s.listEvents(r.Context(), id.UserID, f)
	if err != nil {
		return err
	}
	httpx.OK(w, page)
	return nil
}

func (s *Service) getHandler(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	eid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	e, _, err := s.loadEvent(r.Context(), eid, id.UserID)
	if err != nil {
		return err
	}
	httpx.OK(w, e)
	return nil
}

// ---- create / update -------------------------------------------------------

// eventReq is the full set of editable fields; PUT replaces all of them.
type eventReq struct {
	Title        string    `json:"title"`
	Details      string    `json:"details"`
	StartsAt     time.Time `json:"starts_at"`
	EndsAt       time.Time `json:"ends_at"`
	Timezone     string    `json:"timezone"`
	LocationText string    `json:"location_text"`
	Lat          *float64  `json:"lat"`
	Lng          *float64  `json:"lng"`
	OnlineURL    *string   `json:"online_url"`
	CoverKey     *string   `json:"cover_key"`
	Capacity     *int      `json:"capacity"`
	Visibility   string    `json:"visibility"`
}

// validate normalises req. currentCover is the cover already on the event
// (accepted whoever uploaded it).
func (req *eventReq) validate(caller uuid.UUID, currentCover *string) error {
	req.Title = strings.TrimSpace(req.Title)
	req.Details = strings.TrimSpace(req.Details)
	req.LocationText = strings.TrimSpace(req.LocationText)
	if n := utf8.RuneCountInString(req.Title); n == 0 || n > maxTitle {
		return httpx.BadRequest("Title must be 1 to 200 characters.")
	}
	if utf8.RuneCountInString(req.Details) > maxDetails {
		return httpx.BadRequest("Details must be under 5000 characters.")
	}
	if utf8.RuneCountInString(req.LocationText) > maxLocation {
		return httpx.BadRequest("Location must be under 300 characters.")
	}
	if req.StartsAt.IsZero() || req.EndsAt.IsZero() {
		return httpx.BadRequest("Set a start and end time.")
	}
	req.StartsAt, req.EndsAt = req.StartsAt.UTC(), req.EndsAt.UTC()
	if !req.EndsAt.After(req.StartsAt) {
		return httpx.BadRequest("End time must be after the start time.")
	}
	if req.EndsAt.Sub(req.StartsAt) > maxDuration {
		return httpx.BadRequest("Events can last up to 31 days.")
	}
	if req.Timezone == "" {
		req.Timezone = "UTC"
	}
	if _, err := time.LoadLocation(req.Timezone); err != nil || len(req.Timezone) > 64 {
		return httpx.BadRequest("Timezone must be an IANA zone such as America/New_York.")
	}
	if (req.Lat == nil) != (req.Lng == nil) {
		return httpx.BadRequest("Send both lat and lng, or neither.")
	}
	if req.Lat != nil && (*req.Lat < -90 || *req.Lat > 90 || *req.Lng < -180 || *req.Lng > 180) {
		return httpx.BadRequest("Coordinates are out of range.")
	}
	if req.OnlineURL != nil {
		v := strings.TrimSpace(*req.OnlineURL)
		if v == "" {
			req.OnlineURL = nil
		} else {
			u, err := url.Parse(v)
			if err != nil || len(v) > maxURL || u.Host == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
				return httpx.BadRequest("Enter a full link starting with https://.")
			}
			req.OnlineURL = &v
		}
	}
	if req.CoverKey != nil {
		k := strings.TrimSpace(*req.CoverKey)
		switch {
		case k == "":
			req.CoverKey = nil
		case currentCover != nil && k == *currentCover:
		case !storage.KeyOwnedBy(k, CoverPurpose, caller):
			return httpx.BadRequest("Upload the cover image first, then save the event.")
		default:
			req.CoverKey = &k
		}
	}
	if req.Capacity != nil && (*req.Capacity < 1 || *req.Capacity > maxCapacity) {
		return httpx.BadRequest("Capacity must be between 1 and 100000, or empty for no limit.")
	}
	switch req.Visibility {
	case "":
		req.Visibility = VisibilityMembers
	case VisibilityPublic, VisibilityMembers, VisibilityInvite:
	default:
		return httpx.BadRequest("Visibility must be public, members or invite.")
	}
	return nil
}

func (s *Service) createHandler(w http.ResponseWriter, r *http.Request) error {
	caller := auth.MustFromContext(r.Context())
	var req eventReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if err := req.validate(caller.UserID, nil); err != nil {
		return err
	}
	if req.StartsAt.Before(time.Now().Add(-5 * time.Minute)) {
		return httpx.BadRequest("Pick a start time in the future.")
	}
	var id uuid.UUID
	err := s.d.Pool.QueryRow(r.Context(), `
		INSERT INTO events (host_id, title, details, starts_at, ends_at, timezone, location_text, lat, lng,
		                    online_url, cover_key, capacity, visibility)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13) RETURNING id`,
		caller.UserID, req.Title, req.Details, req.StartsAt, req.EndsAt, req.Timezone, req.LocationText,
		req.Lat, req.Lng, req.OnlineURL, req.CoverKey, req.Capacity, req.Visibility).Scan(&id)
	if err != nil {
		return err
	}
	e, _, err := s.loadEvent(r.Context(), id, caller.UserID)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, e)
	return nil
}

// updateHandler replaces an event's editable fields. Hosts (creator and
// co-hosts) only; visibility included, so no attendee can widen it.
func (s *Service) updateHandler(w http.ResponseWriter, r *http.Request) error {
	caller := auth.MustFromContext(r.Context())
	ctx := r.Context()
	eid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req eventReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	var (
		promoted []uuid.UUID
		notifyTo []uuid.UUID
		oldCover *string
		changed  bool
		title    string
	)
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var cur struct {
			host               uuid.UUID
			status, visibility string
			starts, ends       time.Time
			location           string
			lat, lng           *float64
			online, cover      *string
			capacity           *int
		}
		if err := tx.QueryRow(ctx, `
			SELECT host_id, status, visibility, starts_at, ends_at, location_text, lat, lng, online_url, cover_key, capacity
			FROM events WHERE id = $1 FOR UPDATE`, eid).Scan(&cur.host, &cur.status, &cur.visibility, &cur.starts,
			&cur.ends, &cur.location, &cur.lat, &cur.lng, &cur.online, &cur.cover, &cur.capacity); err != nil {
			if db.IsNoRows(err) {
				return errNotFound
			}
			return err
		}
		a, err := loadAccess(ctx, tx, eid, cur.host, caller.UserID)
		if err != nil {
			return err
		}
		if !a.host() {
			if a.canView(cur.visibility) {
				return httpx.Forbidden("Only hosts can edit this event.")
			}
			return errNotFound
		}
		if cur.status == "cancelled" {
			return httpx.Conflict("This event was cancelled.")
		}
		if err := req.validate(caller.UserID, cur.cover); err != nil {
			return err
		}
		if !req.StartsAt.Equal(cur.starts) && req.StartsAt.Before(time.Now().Add(-5*time.Minute)) {
			return httpx.BadRequest("Pick a start time in the future.")
		}
		changed = !req.StartsAt.Equal(cur.starts) || !req.EndsAt.Equal(cur.ends) || req.LocationText != cur.location ||
			!sameStr(req.OnlineURL, cur.online) || !sameFloat(req.Lat, cur.lat) || !sameFloat(req.Lng, cur.lng)
		if cur.cover != nil && !sameStr(req.CoverKey, cur.cover) {
			oldCover = cur.cover
		}
		if err := tx.QueryRow(ctx, `
			UPDATE events SET title = $2, details = $3, starts_at = $4, ends_at = $5, timezone = $6,
			  location_text = $7, lat = $8, lng = $9, online_url = $10, cover_key = $11, capacity = $12,
			  visibility = $13, sequence = sequence + 1, updated_at = now()
			WHERE id = $1 RETURNING title`, eid, req.Title, req.Details, req.StartsAt, req.EndsAt, req.Timezone,
			req.LocationText, req.Lat, req.Lng, req.OnlineURL, req.CoverKey, req.Capacity, req.Visibility).Scan(&title); err != nil {
			return err
		}
		if !req.StartsAt.Equal(cur.starts) {
			// Reminders follow the new time.
			if _, err := tx.Exec(ctx, `DELETE FROM event_reminders_sent WHERE event_id = $1`, eid); err != nil {
				return err
			}
		}
		if promoted, err = promoteWaitlist(ctx, tx, eid); err != nil {
			return err
		}
		if changed {
			if notifyTo, err = attendeeIDs(ctx, tx, eid); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if oldCover != nil {
		key := *oldCover
		s.afterCommit(ctx, func(ctx context.Context) { _ = s.d.Storage.Delete(ctx, key) })
	}
	e, _, err := s.loadEvent(ctx, eid, caller.UserID)
	if err != nil {
		return err
	}
	s.publish(ctx, eid, EventUpdated, map[string]any{"event_id": eid, "status": e.Status})
	s.publishCounts(ctx, eid)
	when := whenText(e.StartsAt, e.Timezone)
	s.afterCommit(ctx, func(ctx context.Context) {
		s.notifyPromoted(ctx, eid, title, promoted)
		s.notifyAll(ctx, caller.UserID, notifyTo, notify.Notification{
			Kind: NotifyUpdated, Title: title + " changed", Body: "Now " + when + ".",
			Link: eventLink(eid), Data: map[string]any{"event_id": eid},
		})
	})
	httpx.OK(w, e)
	return nil
}

func sameStr(a, b *string) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

func sameFloat(a, b *float64) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

// ---- cancel ----------------------------------------------------------------

// cancelEvent cancels a scheduled event inside tx and returns its title and
// the attendees to tell. byCreator, when set, limits it to that creator.
func cancelEvent(ctx context.Context, tx pgx.Tx, id uuid.UUID, byCreator *uuid.UUID) (string, []uuid.UUID, error) {
	var title string
	err := tx.QueryRow(ctx, `
		UPDATE events SET status = 'cancelled', cancelled_at = now(), sequence = sequence + 1, updated_at = now()
		WHERE id = $1 AND status = 'scheduled' AND ($2::uuid IS NULL OR host_id = $2) RETURNING title`,
		id, byCreator).Scan(&title)
	if err != nil {
		return "", nil, err
	}
	users, err := attendeeIDs(ctx, tx, id)
	return title, users, err
}

func (s *Service) cancelHandler(w http.ResponseWriter, r *http.Request) error {
	caller := auth.MustFromContext(r.Context())
	ctx := r.Context()
	eid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var title string
	var users []uuid.UUID
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var err error
		title, users, err = cancelEvent(ctx, tx, eid, &caller.UserID)
		if db.IsNoRows(err) {
			e, a, lerr := s.loadEvent(ctx, eid, caller.UserID)
			switch {
			case lerr != nil:
				return lerr
			case !a.creator:
				return httpx.Forbidden("Only the event's creator can cancel it.")
			case e.Status == "cancelled":
				return httpx.Conflict("This event is already cancelled.")
			}
			return errNotFound
		}
		return err
	})
	if err != nil {
		return err
	}
	s.afterCancel(ctx, caller.UserID, eid, title, "The host cancelled this event.", users)
	e, _, err := s.loadEvent(ctx, eid, caller.UserID)
	if err != nil {
		return err
	}
	httpx.OK(w, e)
	return nil
}

func (s *Service) afterCancel(ctx context.Context, actor, eid uuid.UUID, title, body string, users []uuid.UUID) {
	s.publish(ctx, eid, EventUpdated, map[string]any{"event_id": eid, "status": "cancelled"})
	s.afterCommit(ctx, func(ctx context.Context) {
		s.notifyAll(ctx, actor, users, notify.Notification{
			Kind: NotifyCancelled, Title: title + " is cancelled", Body: body,
			Link: eventLink(eid), Data: map[string]any{"event_id": eid},
		})
	})
}

func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
