package calendar

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/audit"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/jobs"
	"scaffoldapp/backend/internal/orgs"
)

// Room is a bookable room or resource in a workspace.
type Room struct {
	ID          uuid.UUID `json:"id"`
	OrgID       uuid.UUID `json:"org_id"`
	Kind        string    `json:"kind"` // room | resource
	Name        string    `json:"name"`
	Building    string    `json:"building"`
	Floor       string    `json:"floor"`
	Capacity    *int      `json:"capacity"`
	Features    []string  `json:"features"`
	Description string    `json:"description"`
	Active      bool      `json:"active"`
	CanBook     bool      `json:"can_book"`
	CanManage   bool      `json:"can_manage"`
	// Busy is filled when the list asks for a window (times only).
	Busy []Slot `json:"busy"`
	// Free: free for the whole asked window (from/to).
	Free *bool `json:"free"`
}

// EventRoom is a room booked by an event.
type EventRoom struct {
	ID       uuid.UUID `json:"id"`
	Name     string    `json:"name"`
	Kind     string    `json:"kind"`
	Building string    `json:"building"`
	Capacity *int      `json:"capacity"`
}

const roomCols = `r.id, r.org_id, r.kind, r.name, r.building, r.floor, r.capacity, r.features, r.description, r.active`

func scanRoom(row pgx.Row, extra ...any) (Room, error) {
	var m Room
	err := row.Scan(append([]any{&m.ID, &m.OrgID, &m.Kind, &m.Name, &m.Building, &m.Floor, &m.Capacity, &m.Features, &m.Description, &m.Active}, extra...)...)
	if m.Features == nil {
		m.Features = []string{}
	}
	m.Busy = []Slot{}
	return m, err
}

func eventRooms(ctx context.Context, q db.Querier, eventID uuid.UUID) ([]EventRoom, error) {
	rows, err := q.Query(ctx, `SELECT r.id, r.name, r.kind, r.building, r.capacity FROM calendar_event_rooms er
		JOIN calendar_rooms r ON r.id = er.room_id WHERE er.event_id = $1 ORDER BY lower(r.name), r.id`, eventID)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (EventRoom, error) {
		var r EventRoom
		return r, row.Scan(&r.ID, &r.Name, &r.Kind, &r.Building, &r.Capacity)
	})
	if out == nil {
		out = []EventRoom{}
	}
	return out, err
}

var (
	errRoomNotFound = httpx.NotFound("That room doesn't exist or you can't see it.")
	errRoomBusy     = httpx.NewError(http.StatusConflict, "room_busy", "That room is already booked then.")
)

// roomBusy expands the events booking roomID over [from, to), leaving out
// exclude (the event being saved). Times only.
func (s *Service) roomBusy(ctx context.Context, q db.Querier, roomID uuid.UUID, from, to time.Time, exclude uuid.UUID) ([]Slot, error) {
	rows, err := q.Query(ctx, `
		SELECT `+eventCols+` FROM calendar_event_rooms er
		JOIN calendar_events e ON e.id = er.event_id
		JOIN users u ON u.id = e.owner_id
		WHERE er.room_id = $1 AND e.id <> $4 AND e.recurring_event_id IS NULL AND e.status = 'confirmed'
		  AND e.starts_at < $3
		  AND ((e.rrule IS NULL AND e.ends_at > $2) OR (e.rrule IS NOT NULL AND (e.series_until IS NULL OR e.series_until > $2)))
		LIMIT 2000`, roomID, from, to, exclude)
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (viewerRow, error) {
		var v viewerRow
		return v, row.Scan(v.dest()...)
	})
	if err != nil {
		return nil, err
	}
	occ, err := s.expand(ctx, q, list, from, to, 5000)
	if err != nil {
		return nil, err
	}
	out := make([]Slot, 0, len(occ))
	for _, o := range occ {
		out = append(out, Slot{StartsAt: o.StartsAt, EndsAt: o.EndsAt})
	}
	return out, nil
}

// roomConflict is the first time an event's occurrences (spans) overlap
// the room's other bookings.
func roomConflict(spans, busy []Slot) *Slot {
	for _, sp := range spans {
		for _, b := range busy {
			if sp.StartsAt.Before(b.EndsAt) && sp.EndsAt.After(b.StartsAt) {
				c := sp
				return &c
			}
		}
	}
	return nil
}

// eventSpans lists an event's occurrences from now for up to a year (at
// most 400), the times a room must be free.
func (s *Service) eventSpans(ctx context.Context, q db.Querier, eventID uuid.UUID) ([]Slot, error) {
	e, err := loadEventRow(ctx, q, eventID)
	if err != nil {
		return nil, err
	}
	from := time.Now().UTC().Add(-time.Hour)
	if e.StartsAt.Before(from) && e.RRule == nil {
		from = e.StartsAt
	}
	to := from.Add(366 * 24 * time.Hour)
	occ, err := s.expand(ctx, q, []viewerRow{{eventRow: e}}, from, to, 400)
	if err != nil {
		return nil, err
	}
	out := make([]Slot, 0, len(occ))
	for _, o := range occ {
		out = append(out, Slot{StartsAt: o.StartsAt, EndsAt: o.EndsAt})
	}
	return out, nil
}

// syncEventRooms makes eventID's rooms match want (nil: keep them) and
// checks that each room is free for the event's occurrences, one booking
// per room at a time (an advisory lock per room in the caller's
// transaction). New rooms need "book" (edit) on the room and the same
// workspace membership.
func (s *Service) syncEventRooms(ctx context.Context, tx pgx.Tx, callerID, eventID uuid.UUID, want *[]uuid.UUID) error {
	rows, err := tx.Query(ctx, `SELECT room_id FROM calendar_event_rooms WHERE event_id = $1`, eventID)
	if err != nil {
		return err
	}
	have, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return err
	}
	rooms := have
	if want != nil {
		rooms = slices.Clone(*want)
		slices.SortFunc(rooms, compareUUID)
		rooms = slices.Compact(rooms)
		for _, id := range rooms {
			if slices.Contains(have, id) {
				continue
			}
			lvl, err := access.LevelOf(ctx, tx, callerID, KindRoom, id)
			if err != nil {
				return err
			}
			var active bool
			if err := tx.QueryRow(ctx, `SELECT active FROM calendar_rooms WHERE id = $1 AND deleted_at IS NULL`, id).Scan(&active); db.IsNoRows(err) || lvl < access.View {
				return errRoomNotFound
			} else if err != nil {
				return err
			}
			if lvl < access.Edit || !active {
				return httpx.Forbidden("You can't book that room.")
			}
			if _, err := tx.Exec(ctx, `INSERT INTO calendar_event_rooms (event_id, room_id, added_by) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, eventID, id, callerID); err != nil {
				return err
			}
		}
		for _, id := range have {
			if !slices.Contains(rooms, id) {
				if _, err := tx.Exec(ctx, `DELETE FROM calendar_event_rooms WHERE event_id = $1 AND room_id = $2`, eventID, id); err != nil {
					return err
				}
			}
		}
	}
	if len(rooms) == 0 {
		return nil
	}
	spans, err := s.eventSpans(ctx, tx, eventID)
	if err != nil || len(spans) == 0 {
		return err
	}
	slices.SortFunc(rooms, func(a, b uuid.UUID) int { return compareUUID(a, b) })
	for _, id := range rooms {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, jobs.LockKey("calendar.room:"+id.String())); err != nil {
			return err
		}
	}
	for _, id := range rooms {
		busy, err := s.roomBusy(ctx, tx, id, spans[0].StartsAt, spans[len(spans)-1].EndsAt, eventID)
		if err != nil {
			return err
		}
		if c := roomConflict(spans, busy); c != nil {
			var name string
			_ = tx.QueryRow(ctx, `SELECT name FROM calendar_rooms WHERE id = $1`, id).Scan(&name)
			return &httpx.Error{Status: http.StatusConflict, Code: "room_busy", Message: name + " is already booked then.",
				Details: map[string]any{"room_id": id, "starts_at": c.StartsAt, "ends_at": c.EndsAt}}
		}
	}
	return nil
}

func compareUUID(a, b uuid.UUID) int {
	for i := range a {
		if a[i] != b[i] {
			return int(a[i]) - int(b[i])
		}
	}
	return 0
}

// ---- handlers -----------------------------------------------------------------

// listRoomsHandler lists a workspace's rooms the caller can see; with
// from/to each carries its busy times and whether it's free throughout.
// ?capacity= keeps rooms that seat at least that many, ?q= filters names.
func (s *Service) listRoomsHandler(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id := auth.MustFromContext(ctx)
	q := r.URL.Query()
	orgID, err := uuid.Parse(q.Get("org_id"))
	if err != nil {
		return httpx.BadRequest("Pass org_id.")
	}
	var member bool
	if err := s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM org_members WHERE org_id = $1 AND user_id = $2)`, orgID, id.UserID).Scan(&member); err != nil {
		return err
	}
	if !member {
		return httpx.NotFound("That " + orgs.Noun() + " doesn't exist.")
	}
	capacity := 0
	if v := q.Get("capacity"); v != "" {
		if capacity, err = strconv.Atoi(v); err != nil || capacity < 0 {
			return httpx.BadRequest("capacity is a number of people.")
		}
	}
	name, _ := cleanText(q.Get("q"), 100)
	var from, to time.Time
	window := q.Get("from") != ""
	if window {
		if from, to, err = parseWindow(q.Get("from"), q.Get("to"), 31*24*time.Hour); err != nil {
			return err
		}
	}
	exclude, _ := uuid.Parse(q.Get("exclude_event"))
	rows, err := s.d.Pool.Query(ctx, `SELECT `+roomCols+`, v.level FROM calendar_rooms r
		JOIN access_visible($1, 'calendar.room', $3) v ON v.id = r.id
		WHERE r.org_id = $2 AND r.deleted_at IS NULL AND ($4 = 0 OR r.capacity >= $4)
		  AND ($5 = '' OR r.name ILIKE '%' || $6 || '%' OR r.building ILIKE '%' || $6 || '%')
		ORDER BY r.active DESC, lower(r.building), lower(r.name), r.id LIMIT 200`, id.UserID, orgID, access.ViaAgent(ctx), capacity, name, likeEscape(name))
	if err != nil {
		return err
	}
	list := []Room{}
	for rows.Next() {
		var lvl int
		m, err := scanRoom(rows, &lvl)
		if err != nil {
			rows.Close()
			return err
		}
		if lvl < 1 {
			continue
		}
		m.CanBook, m.CanManage = lvl >= int(access.Edit) && m.Active, lvl >= int(access.Manage)
		list = append(list, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	canCreate, err := access.Allowed(ctx, s.d.Pool, orgID, id.UserID, PermManageRooms)
	if err != nil {
		return err
	}
	if window {
		for i := range list {
			busy, err := s.roomBusy(ctx, s.d.Pool, list[i].ID, from, to, exclude)
			if err != nil {
				return err
			}
			list[i].Busy = busy
			free := len(busy) == 0
			list[i].Free = &free
		}
	}
	_, agent := auth.AgentFrom(ctx)
	httpx.OK(w, map[string]any{"items": list, "can_create": canCreate && !agent})
	return nil
}

type roomInput struct {
	OrgID       *uuid.UUID `json:"org_id"`
	Kind        *string    `json:"kind"`
	Name        *string    `json:"name"`
	Building    *string    `json:"building"`
	Floor       *string    `json:"floor"`
	Capacity    *int       `json:"capacity"`
	Features    *[]string  `json:"features"`
	Description *string    `json:"description"`
	Active      *bool      `json:"active"`
}

func (in roomInput) apply(m *Room) error {
	if in.Kind != nil {
		if *in.Kind != "room" && *in.Kind != "resource" {
			return httpx.BadRequest("kind must be room or resource.")
		}
		m.Kind = *in.Kind
	}
	str := func(dst *string, src *string, max int, label string) error {
		if src == nil {
			return nil
		}
		v, ok := cleanText(*src, max)
		if !ok {
			return httpx.BadRequest(label + " is up to " + strconv.Itoa(max) + " characters.")
		}
		*dst = v
		return nil
	}
	if err := str(&m.Name, in.Name, 100, "The name"); err != nil {
		return err
	}
	if m.Name == "" {
		return httpx.BadRequest("Name the room.")
	}
	if err := str(&m.Building, in.Building, 100, "The building"); err != nil {
		return err
	}
	if err := str(&m.Floor, in.Floor, 40, "The floor"); err != nil {
		return err
	}
	if err := str(&m.Description, in.Description, 1000, "The description"); err != nil {
		return err
	}
	if in.Capacity != nil {
		if *in.Capacity == 0 {
			m.Capacity = nil
		} else if *in.Capacity < 1 || *in.Capacity > 10000 {
			return httpx.BadRequest("Capacity is 1 to 10,000 people.")
		} else {
			c := *in.Capacity
			m.Capacity = &c
		}
	}
	if in.Features != nil {
		if len(*in.Features) > 20 {
			return httpx.BadRequest("Up to 20 features.")
		}
		m.Features = []string{}
		for _, f := range *in.Features {
			f, ok := cleanText(f, 40)
			if !ok {
				return httpx.BadRequest("Features are up to 40 characters.")
			}
			if f != "" && !slices.Contains(m.Features, f) {
				m.Features = append(m.Features, f)
			}
		}
	}
	if in.Active != nil {
		m.Active = *in.Active
	}
	return nil
}

// createRoomHandler adds a room (calendar.manage_rooms): everyone in the
// workspace may book it until admins restrict it in its sharing.
func (s *Service) createRoomHandler(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id := auth.MustFromContext(ctx)
	var in roomInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if in.OrgID == nil {
		return httpx.BadRequest("Pass org_id.")
	}
	if ok, err := access.Allowed(ctx, s.d.Pool, *in.OrgID, id.UserID, PermManageRooms); err != nil {
		return err
	} else if !ok {
		return httpx.Forbidden("Only people who manage rooms can add them.")
	}
	m := Room{Kind: "room", Active: true, Features: []string{}, Busy: []Slot{}}
	if err := in.apply(&m); err != nil {
		return err
	}
	m.ID, m.OrgID = uuid.New(), *in.OrgID
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM calendar_rooms WHERE org_id = $1 AND deleted_at IS NULL`, m.OrgID).Scan(&n); err != nil {
			return err
		}
		if n >= 500 {
			return httpx.Conflict("This " + orgs.Noun() + " has 500 rooms and resources.")
		}
		if _, err := tx.Exec(ctx, `INSERT INTO calendar_rooms (id, org_id, kind, name, building, floor, capacity, features, description, active, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`, m.ID, m.OrgID, m.Kind, m.Name, m.Building, m.Floor, m.Capacity,
			m.Features, m.Description, m.Active, id.UserID); err != nil {
			return err
		}
		if err := access.CreateResource(ctx, tx, access.NewResource{Kind: KindRoom, ID: m.ID, OrgID: m.OrgID, General: access.Edit}); err != nil {
			return err
		}
		return audit.Log(ctx, tx, id.UserID, "calendar.room.create", "calendar_room", m.ID.String(), map[string]any{"org_id": m.OrgID, "name": m.Name})
	})
	if err != nil {
		return err
	}
	m.CanBook, m.CanManage = m.Active, true
	httpx.JSON(w, http.StatusCreated, m)
	return nil
}

// roomForManager loads a room the caller may change: manage on the room,
// or the workspace's calendar.manage_rooms.
func (s *Service) roomForManager(ctx context.Context, userID, roomID uuid.UUID) (Room, error) {
	m, err := scanRoom(s.d.Pool.QueryRow(ctx, `SELECT `+roomCols+` FROM calendar_rooms r WHERE r.id = $1 AND r.deleted_at IS NULL`, roomID))
	if db.IsNoRows(err) {
		return m, errRoomNotFound
	}
	if err != nil {
		return m, err
	}
	lvl, err := access.LevelOf(ctx, s.d.Pool, userID, KindRoom, roomID)
	if err != nil {
		return m, err
	}
	if lvl < access.View {
		return m, errRoomNotFound
	}
	if lvl < access.Manage {
		ok, err := access.Allowed(ctx, s.d.Pool, m.OrgID, userID, PermManageRooms)
		if err != nil {
			return m, err
		}
		if !ok {
			return m, httpx.Forbidden("Only people who manage rooms can change them.")
		}
	}
	m.CanBook, m.CanManage = m.Active, true
	return m, nil
}

func (s *Service) updateRoomHandler(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id := auth.MustFromContext(ctx)
	roomID, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var in roomInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if in.OrgID != nil {
		return httpx.BadRequest("A room's " + orgs.Noun() + " can't change.")
	}
	m, err := s.roomForManager(ctx, id.UserID, roomID)
	if err != nil {
		return err
	}
	if err := in.apply(&m); err != nil {
		return err
	}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE calendar_rooms SET kind = $2, name = $3, building = $4, floor = $5, capacity = $6, features = $7,
			description = $8, active = $9, updated_at = now() WHERE id = $1`, m.ID, m.Kind, m.Name, m.Building, m.Floor, m.Capacity, m.Features,
			m.Description, m.Active); err != nil {
			return err
		}
		return audit.Log(ctx, tx, id.UserID, "calendar.room.update", "calendar_room", m.ID.String(), map[string]any{"name": m.Name, "active": m.Active})
	})
	if err != nil {
		return err
	}
	httpx.OK(w, m)
	return nil
}

// deleteRoomHandler retires a room: upcoming bookings lose it and their
// organizers are told.
func (s *Service) deleteRoomHandler(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id := auth.MustFromContext(ctx)
	roomID, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	m, err := s.roomForManager(ctx, id.UserID, roomID)
	if err != nil {
		return err
	}
	var owners []uuid.UUID
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `DELETE FROM calendar_event_rooms er USING calendar_events e
			WHERE er.room_id = $1 AND e.id = er.event_id RETURNING e.owner_id`, roomID)
		if err != nil {
			return err
		}
		if owners, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID]); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE calendar_rooms SET deleted_at = now(), active = false, updated_at = now() WHERE id = $1`, roomID); err != nil {
			return err
		}
		if err := access.DeleteResource(ctx, tx, access.Ref{Kind: KindRoom, ID: roomID}); err != nil {
			return err
		}
		return audit.Log(ctx, tx, id.UserID, "calendar.room.delete", "calendar_room", roomID.String(), map[string]any{"name": m.Name})
	})
	if err != nil {
		return err
	}
	s.changed(ctx, "events", owners...)
	httpx.NoContent(w)
	return nil
}
