package calendar

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
)

// coMembers returns which of ids share at least one organization with
// callerID (accounts that are active or suspended).
func coMembers(ctx context.Context, q db.Querier, callerID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	out := map[uuid.UUID]bool{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := q.Query(ctx, `
		SELECT DISTINCT b.user_id FROM org_members a
		JOIN org_members b ON b.org_id = a.org_id
		JOIN users u ON u.id = b.user_id
		WHERE a.user_id = $1 AND b.user_id = ANY($2) AND u.status IN ('active', 'suspended')`, callerID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// peopleHandler searches people the caller shares an organization with,
// by name (or exact email), or with ?ids= returns those of the given ids
// who are such people (a prefilled guest list); others are left out, the
// same as a search would. Emails are never returned: organizations show
// member emails to their admins only.
func (s *Service) peopleHandler(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	if raw := r.URL.Query().Get("ids"); raw != "" {
		ids, err := parseUserIDs(raw, 50)
		if err != nil {
			return err
		}
		people, err := s.peopleByID(r.Context(), id.UserID, ids)
		if err != nil {
			return err
		}
		httpx.OK(w, map[string]any{"items": people})
		return nil
	}
	q, _ := cleanText(r.URL.Query().Get("q"), 100)
	people, err := s.searchPeople(r.Context(), id.UserID, q, 20)
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": people})
	return nil
}

// peopleByID returns the active co-members among ids, by name.
func (s *Service) peopleByID(ctx context.Context, callerID uuid.UUID, ids []uuid.UUID) ([]Person, error) {
	rows, err := s.d.Pool.Query(ctx, `
		SELECT DISTINCT u.id, u.display_name, u.avatar_url FROM users u
		JOIN org_members b ON b.user_id = u.id
		JOIN org_members a ON a.org_id = b.org_id AND a.user_id = $1
		WHERE u.id = ANY($2) AND u.id <> $1 AND u.status = 'active'
		ORDER BY u.display_name, u.id`, callerID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Person{}
	for rows.Next() {
		var p Person
		if err := rows.Scan(&p.ID, &p.DisplayName, &p.AvatarURL); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Service) searchPeople(ctx context.Context, callerID uuid.UUID, q string, limit int) ([]Person, error) {
	rows, err := s.d.Pool.Query(ctx, `
		SELECT DISTINCT u.id, u.display_name, u.avatar_url FROM users u
		JOIN org_members b ON b.user_id = u.id
		JOIN org_members a ON a.org_id = b.org_id AND a.user_id = $1
		WHERE u.id <> $1 AND u.status = 'active'
		  AND ($2 = '' OR u.display_name ILIKE '%' || $3 || '%' OR lower(u.email) = lower($2))
		ORDER BY u.display_name, u.id LIMIT $4`, callerID, q, likeEscape(q), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Person{}
	for rows.Next() {
		var p Person
		if err := rows.Scan(&p.ID, &p.DisplayName, &p.AvatarURL); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// FreeBusy is one person's busy time in a window.
type FreeBusy struct {
	User Person      `json:"user"`
	Busy []BusyBlock `json:"busy"`
}

// BusyBlock is busy time; Title only when the owner shared details.
type BusyBlock struct {
	StartsAt time.Time `json:"starts_at"`
	EndsAt   time.Time `json:"ends_at"`
	Title    *string   `json:"title"`
}

func parseUserIDs(raw string, max int) ([]uuid.UUID, error) {
	var out []uuid.UUID
	for v := range strings.SplitSeq(raw, ",") {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		id, err := uuid.Parse(v)
		if err != nil {
			return nil, httpx.BadRequest("user_ids must be a comma-separated list of ids.")
		}
		out = append(out, id)
	}
	if len(out) > max {
		return nil, httpx.BadRequest("Up to " + strconv.Itoa(max) + " people at a time.")
	}
	return out, nil
}

func errNotColleague() error {
	return httpx.Forbidden("You can see free/busy only for people in your " + orgs.Nouns() + ".")
}

// freeBusy returns busy time for each user (the caller or co-members).
func (s *Service) freeBusy(ctx context.Context, callerID uuid.UUID, ids []uuid.UUID, from, to time.Time) ([]FreeBusy, error) {
	ok, err := coMembers(ctx, s.d.Pool, callerID, ids)
	if err != nil {
		return nil, err
	}
	out := []FreeBusy{}
	for _, uid := range ids {
		if uid != callerID && !ok[uid] {
			return nil, errNotColleague()
		}
		var p Person
		if err := s.d.Pool.QueryRow(ctx, `SELECT id, display_name, avatar_url FROM users WHERE id = $1`, uid).
			Scan(&p.ID, &p.DisplayName, &p.AvatarURL); err != nil {
			return nil, err
		}
		items, err := s.busyFor(ctx, s.d.Pool, uid, from, to, uuid.Nil)
		if err != nil {
			return nil, err
		}
		fb := FreeBusy{User: p, Busy: []BusyBlock{}}
		for _, it := range items {
			b := BusyBlock{StartsAt: it.Start, EndsAt: it.End}
			if it.Title != "" {
				t := it.Title
				b.Title = &t
			}
			if b.EndsAt.After(from) && b.StartsAt.Before(to) {
				fb.Busy = append(fb.Busy, b)
			}
		}
		out = append(out, fb)
	}
	return out, nil
}

func (s *Service) freeBusyHandler(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	q := r.URL.Query()
	ids, err := parseUserIDs(q.Get("user_ids"), 20)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return httpx.BadRequest("Pass user_ids.")
	}
	from, to, err := parseWindow(q.Get("from"), q.Get("to"), 31*24*time.Hour)
	if err != nil {
		return err
	}
	out, err := s.freeBusy(r.Context(), id.UserID, ids, from, to)
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": out})
	return nil
}

// FreeTime is a find-time answer: free starts in the caller's working hours.
type FreeTime struct {
	Timezone string `json:"timezone"`
	Slots    []Slot `json:"slots"`
}

// findTime finds times when the caller and the given colleagues are all
// free, inside the caller's working hours.
func (s *Service) findTime(ctx context.Context, callerID uuid.UUID, ids []uuid.UUID, from, to time.Time, dur time.Duration, limit int) (FreeTime, error) {
	ok, err := coMembers(ctx, s.d.Pool, callerID, ids)
	if err != nil {
		return FreeTime{}, err
	}
	st, err := s.loadSettings(ctx, s.d.Pool, callerID)
	if err != nil {
		return FreeTime{}, err
	}
	var busy []busyItem
	for _, uid := range append([]uuid.UUID{callerID}, ids...) {
		if uid != callerID && !ok[uid] {
			return FreeTime{}, errNotColleague()
		}
		items, err := s.busyFor(ctx, s.d.Pool, uid, from, to, uuid.Nil)
		if err != nil {
			return FreeTime{}, err
		}
		for _, it := range items {
			it.BlockStart, it.BlockEnd = it.Start, it.End
			busy = append(busy, it)
		}
	}
	loc := zoneOr(st.Timezone, time.UTC)
	return FreeTime{Timezone: loc.String(), Slots: freeTimes(loc, st.WorkingHours, from, to, dur, busy, limit)}, nil
}

func (s *Service) findTimeHandler(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	q := r.URL.Query()
	ids, err := parseUserIDs(q.Get("user_ids"), 20)
	if err != nil {
		return err
	}
	from, to, err := parseWindow(q.Get("from"), q.Get("to"), 31*24*time.Hour)
	if err != nil {
		return err
	}
	mins, err := strconv.Atoi(q.Get("duration"))
	if err != nil || mins < 5 || mins > 8*60 {
		return httpx.BadRequest("duration is minutes, 5 to 480.")
	}
	out, err := s.findTime(r.Context(), id.UserID, ids, from, to, time.Duration(mins)*time.Minute, 50)
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}
