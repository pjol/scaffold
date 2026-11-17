package calendar

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
)

// Access kinds this module registers.
const (
	KindCalendar = "calendar.calendar"
	KindRoom     = "calendar.room"
)

// PermManageRooms lets people add, change and remove a workspace's rooms
// and resources.
const PermManageRooms = "calendar.manage_rooms"

const maxCalendars = 30

func registerAccessKinds() {
	access.RegisterKind(access.Kind{
		Name: KindCalendar, Label: "calendar",
		Title: func(ctx context.Context, q db.Querier, id uuid.UUID) (string, error) {
			var n string
			err := q.QueryRow(ctx, `SELECT name FROM calendar_calendars WHERE id = $1`, id).Scan(&n)
			return n, err
		},
		Link: func(id uuid.UUID) string { return "/calendar?add_calendar=" + id.String() },
	})
	access.RegisterKind(access.Kind{
		Name: KindRoom, Label: "room", AdminsManage: true,
		Levels: []access.Level{access.View, access.Edit, access.Manage},
		Title: func(ctx context.Context, q db.Querier, id uuid.UUID) (string, error) {
			var n string
			err := q.QueryRow(ctx, `SELECT name FROM calendar_rooms WHERE id = $1`, id).Scan(&n)
			return n, err
		},
		Link: func(uuid.UUID) string { return "/calendar/rooms" },
	})
	access.RegisterPermission(access.Permission{Key: PermManageRooms, Title: "Manage rooms and resources",
		Info: "Add, change and remove the rooms and resources people book with events.", DefaultRoles: []string{"owner", "admin"}})
}

// CalendarRef names a calendar in an event's detail.
type CalendarRef struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Color     string    `json:"color"`
	IsPrimary bool      `json:"is_primary"`
	Owner     *Person   `json:"owner"`
}

// Calendar is one calendar as the viewer sees it.
type Calendar struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	// Color is the viewer's colour for it (their own choice, else the calendar's).
	Color      string     `json:"color"`
	OwnColor   string     `json:"own_color"`
	IsPrimary  bool       `json:"is_primary"`
	Owner      *Person    `json:"owner"`
	OrgID      *uuid.UUID `json:"org_id"`
	OrgName    *string    `json:"org_name"`
	TeamID     *uuid.UUID `json:"team_id"`
	TeamName   *string    `json:"team_name"`
	CountsBusy bool       `json:"counts_busy"`
	// Level: view (free/busy), comment (details), edit (changes), manage.
	Level  access.Level `json:"level"`
	Mine   bool         `json:"mine"`
	Hidden bool         `json:"hidden"`
	Added  bool         `json:"added"`
}

const calendarCols = `c.id, c.name, c.description, c.color, COALESCE(p.color, c.color), c.is_primary, c.owner_id, u.display_name,
	u.avatar_url, c.org_id, o.name, c.team_id, t.name, c.counts_busy, COALESCE(p.hidden, false),
	COALESCE(p.added, c.owner_id = $1 OR EXISTS (SELECT 1 FROM access_team_members tm WHERE tm.team_id = c.team_id AND tm.user_id = $1))`

const calendarFrom = ` FROM calendar_calendars c
	LEFT JOIN calendar_calendar_prefs p ON p.calendar_id = c.id AND p.user_id = $1
	LEFT JOIN users u ON u.id = c.owner_id
	LEFT JOIN orgs o ON o.id = c.org_id
	LEFT JOIN access_teams t ON t.id = c.team_id `

func scanCalendar(row pgx.Row, viewer uuid.UUID, lvl *int) (Calendar, error) {
	var c Calendar
	var ownerID *uuid.UUID
	var ownerName *string
	var ownerAvatar *string
	dest := []any{&c.ID, &c.Name, &c.Description, &c.OwnColor, &c.Color, &c.IsPrimary, &ownerID, &ownerName, &ownerAvatar,
		&c.OrgID, &c.OrgName, &c.TeamID, &c.TeamName, &c.CountsBusy, &c.Hidden, &c.Added}
	var level int
	if lvl != nil {
		dest = append(dest, &level)
	}
	if err := row.Scan(dest...); err != nil {
		return c, err
	}
	if ownerID != nil {
		c.Owner = &Person{ID: *ownerID, DisplayName: ptrStr(ownerName), AvatarURL: ownerAvatar}
		c.Mine = *ownerID == viewer
	}
	if lvl != nil {
		*lvl = level
		c.Level = access.Level(level)
	}
	return c, nil
}

// primaryCalendarID is userID's primary calendar, nil before it exists.
func primaryCalendarID(ctx context.Context, q db.Querier, userID uuid.UUID) (*uuid.UUID, error) {
	var id uuid.UUID
	err := q.QueryRow(ctx, `SELECT id FROM calendar_calendars WHERE owner_id = $1 AND is_primary`, userID).Scan(&id)
	if db.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &id, nil
}

// ensurePrimary returns userID's primary calendar, making it on first use:
// named after them, in orgHint (when they belong to it) else their oldest
// workspace (colleagues there see its free/busy, as they always could),
// else personal. Events made before it existed are filed on it.
func (s *Service) ensurePrimary(ctx context.Context, userID, orgHint uuid.UUID) (uuid.UUID, error) {
	if id, err := primaryCalendarID(ctx, s.d.Pool, userID); err != nil || id != nil {
		if id == nil {
			return uuid.Nil, err
		}
		return *id, err
	}
	var name string
	var org *uuid.UUID
	if err := s.d.Pool.QueryRow(ctx, `
		SELECT u.display_name,
		       COALESCE((SELECT m.org_id FROM org_members m WHERE m.user_id = u.id AND m.org_id = $2),
		                (SELECT m.org_id FROM org_members m WHERE m.user_id = u.id ORDER BY m.created_at, m.org_id LIMIT 1))
		FROM users u WHERE u.id = $1`, userID, orgHint).Scan(&name, &org); err != nil {
		return uuid.Nil, err
	}
	if name == "" {
		name = "Calendar"
	}
	id := uuid.New()
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `INSERT INTO calendar_calendars (id, owner_id, org_id, name, is_primary) VALUES ($1, $2, $3, $4, true)
			ON CONFLICT (owner_id) WHERE is_primary DO NOTHING`, id, userID, org, name)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return errRaced
		}
		nr := access.NewResource{Kind: KindCalendar, ID: id, OwnerID: userID}
		if org != nil {
			nr.OrgID, nr.General = *org, access.View
		}
		if err := access.CreateResource(ctx, tx, nr); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE calendar_events SET calendar_id = $2 WHERE owner_id = $1 AND calendar_id IS NULL`, userID, id)
		return err
	})
	if err == errRaced {
		got, err := primaryCalendarID(ctx, s.d.Pool, userID)
		if err != nil || got == nil {
			return uuid.Nil, err
		}
		return *got, nil
	}
	return id, err
}

var errRaced = httpx.Conflict("Try again.")

// calendarLevel is viewer's level on a calendar (access's answer, capped
// for agents), None when it doesn't exist or they can't see it.
func calendarLevel(ctx context.Context, q db.Querier, viewer, calID uuid.UUID) (access.Level, error) {
	return access.LevelOf(ctx, q, viewer, KindCalendar, calID)
}

func calendarRefOf(ctx context.Context, q db.Querier, id uuid.UUID) (CalendarRef, error) {
	var c CalendarRef
	var ownerID *uuid.UUID
	var ownerName, ownerAvatar *string
	err := q.QueryRow(ctx, `SELECT c.id, c.name, c.color, c.is_primary, c.owner_id, u.display_name, u.avatar_url
		FROM calendar_calendars c LEFT JOIN users u ON u.id = c.owner_id WHERE c.id = $1`, id).
		Scan(&c.ID, &c.Name, &c.Color, &c.IsPrimary, &ownerID, &ownerName, &ownerAvatar)
	if ownerID != nil {
		c.Owner = &Person{ID: *ownerID, DisplayName: ptrStr(ownerName), AvatarURL: ownerAvatar}
	}
	return c, err
}

var errCalendarNotFound = httpx.NotFound("That calendar doesn't exist or isn't shared with you.")

// loadCalendar reads one calendar the viewer can see (level ≥ view).
func (s *Service) loadCalendar(ctx context.Context, q db.Querier, viewer, id uuid.UUID) (Calendar, error) {
	var lvl int
	c, err := scanCalendar(q.QueryRow(ctx, `SELECT `+calendarCols+`, access_level($1, 'calendar.calendar', c.id, $3)`+calendarFrom+
		`WHERE c.id = $2`, viewer, id, access.ViaAgent(ctx)), viewer, &lvl)
	if db.IsNoRows(err) || (err == nil && lvl < 1) {
		return c, errCalendarNotFound
	}
	return c, err
}

// listCalendars: the viewer's own calendars and the ones they added
// (mine), then calendars shared with them they haven't added (others).
func (s *Service) listCalendars(ctx context.Context, viewer uuid.UUID) (mine, others []Calendar, err error) {
	via := access.ViaAgent(ctx)
	rows, err := s.d.Pool.Query(ctx, `SELECT `+calendarCols+`, v.level`+calendarFrom+`
		JOIN access_visible($1, 'calendar.calendar', $2) v ON v.id = c.id
		ORDER BY (c.owner_id = $1) DESC, c.is_primary DESC, lower(c.name), c.id LIMIT 300`, viewer, via)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	mine, others = []Calendar{}, []Calendar{}
	for rows.Next() {
		var lvl int
		c, err := scanCalendar(rows, viewer, &lvl)
		if err != nil {
			return nil, nil, err
		}
		if lvl < 1 {
			continue
		}
		// A team calendar is in its members' lists until they remove it.
		if c.Mine || c.Added {
			c.Added = true
			mine = append(mine, c)
		} else {
			others = append(others, c)
		}
	}
	return mine, others, rows.Err()
}

// listedCalendarIDs are the calendars in viewer's list with their level
// (the same rule as listCalendars), for the range query.
func (s *Service) listedCalendars(ctx context.Context, viewer uuid.UUID) (map[uuid.UUID]access.Level, error) {
	mine, _, err := s.listCalendars(ctx, viewer)
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID]access.Level{}
	for _, c := range mine {
		out[c.ID] = c.Level
	}
	return out, nil
}

// sharedOccurrences expands events on the calendars in viewer's list that
// viewer neither owns nor is invited to (a teammate's events on a team
// calendar, a colleague's calendar they added). Calendars shared at
// free/busy only come back redacted.
func (s *Service) sharedOccurrences(ctx context.Context, viewer uuid.UUID, from, to time.Time, limit int) ([]Occurrence, error) {
	levels, err := s.listedCalendars(ctx, viewer)
	if err != nil || len(levels) == 0 {
		return []Occurrence{}, err
	}
	ids := make([]uuid.UUID, 0, len(levels))
	for id := range levels {
		ids = append(ids, id)
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT `+eventCols+`,
		       (SELECT count(*) FROM calendar_attendees x WHERE x.event_id = e.id)::int,
		       (SELECT b.id FROM calendar_bookings b WHERE b.event_id = e.id AND b.status = 'confirmed' LIMIT 1),
		       (SELECT count(*) FROM calendar_event_rooms r WHERE r.event_id = e.id)::int
		FROM calendar_events e
		JOIN users u ON u.id = e.owner_id
		WHERE e.calendar_id = ANY($2) AND e.recurring_event_id IS NULL AND e.status = 'confirmed' AND e.owner_id <> $1
		  AND NOT EXISTS (SELECT 1 FROM calendar_attendees a WHERE a.event_id = e.id AND a.user_id = $1)
		  AND e.starts_at < $4
		  AND ((e.rrule IS NULL AND e.ends_at > $3) OR (e.rrule IS NOT NULL AND (e.series_until IS NULL OR e.series_until > $3)))
		ORDER BY e.starts_at, e.id LIMIT 5000`, viewer, ids, from, to)
	if err != nil {
		return nil, err
	}
	var list []viewerRow
	for rows.Next() {
		var v viewerRow
		if err := rows.Scan(append(v.dest(), &v.attendeeCount, &v.bookingID, &v.roomCount)...); err != nil {
			rows.Close()
			return nil, err
		}
		v.viewCalendar = v.CalendarID
		v.redact = levels[*v.CalendarID] < access.Comment
		list = append(list, v)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return []Occurrence{}, nil
	}
	return s.expand(ctx, s.d.Pool, list, from, to, limit)
}

// calendarWatchers are people who show calendar calID (added it, or its
// team's members): realtime refreshes after its events change.
func (s *Service) calendarWatchers(ctx context.Context, calID *uuid.UUID) []uuid.UUID {
	if calID == nil {
		return nil
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT user_id FROM calendar_calendar_prefs WHERE calendar_id = $1 AND added
		UNION
		SELECT tm.user_id FROM calendar_calendars c JOIN access_team_members tm ON tm.team_id = c.team_id WHERE c.id = $1
		LIMIT 500`, *calID)
	if err != nil {
		return nil
	}
	ids, _ := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	return ids
}

// nonBusyCalendars are userID's own calendars whose events don't count as
// busy time (team calendars by default, or turned off).
func nonBusyCalendars(ctx context.Context, q db.Querier, userID uuid.UUID) (map[uuid.UUID]bool, error) {
	rows, err := q.Query(ctx, `SELECT c.id FROM calendar_calendars c WHERE NOT c.counts_busy
		AND (c.owner_id = $1 OR EXISTS (SELECT 1 FROM calendar_events e WHERE e.calendar_id = c.id AND e.owner_id = $1))`, userID)
	if err != nil {
		return nil, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	out := map[uuid.UUID]bool{}
	for _, id := range ids {
		out[id] = true
	}
	return out, err
}

// ---- handlers -----------------------------------------------------------------

func (s *Service) listCalendarsHandler(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	hint, _ := uuid.Parse(r.URL.Query().Get("org_id"))
	if _, err := s.ensurePrimary(r.Context(), id.UserID, hint); err != nil {
		return err
	}
	mine, others, err := s.listCalendars(r.Context(), id.UserID)
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": mine, "shared": others})
	return nil
}

type calendarInput struct {
	Name        *string    `json:"name"`
	Description *string    `json:"description"`
	Color       *string    `json:"color"`
	OrgID       *uuid.UUID `json:"org_id"`
	TeamID      *uuid.UUID `json:"team_id"`
	CountsBusy  *bool      `json:"counts_busy"`
}

func (in calendarInput) apply(c *Calendar) error {
	if in.Name != nil {
		n, ok := cleanText(*in.Name, 100)
		if n == "" || !ok {
			return httpx.BadRequest("Name the calendar (up to 100 characters).")
		}
		c.Name = n
	}
	if in.Description != nil {
		d, ok := cleanText(*in.Description, 1000)
		if !ok {
			return httpx.BadRequest("Descriptions are up to 1000 characters.")
		}
		c.Description = d
	}
	if in.Color != nil {
		if !validColor(*in.Color) {
			return httpx.BadRequest("Pick one of the calendar colors.")
		}
		c.OwnColor = *in.Color
	}
	if in.CountsBusy != nil {
		c.CountsBusy = *in.CountsBusy
	}
	return nil
}

// createCalendarHandler makes a calendar in a workspace: the caller's own
// (Work, Personal…), or a team's (team_id: the team may make changes; its
// events don't count as anyone's busy time unless turned on).
func (s *Service) createCalendarHandler(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id := auth.MustFromContext(ctx)
	var in calendarInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if in.OrgID == nil {
		return httpx.BadRequest("Pick the " + orgs.Noun() + " the calendar belongs to.")
	}
	var member bool
	if err := s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM org_members WHERE org_id = $1 AND user_id = $2)`, *in.OrgID, id.UserID).Scan(&member); err != nil {
		return err
	}
	if !member {
		return httpx.BadRequest("You can only use " + orgs.Nouns() + " you belong to.")
	}
	c := Calendar{OwnColor: "accent", CountsBusy: true}
	if in.TeamID != nil {
		var inTeam bool
		if err := s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM access_teams t JOIN access_team_members m ON m.team_id = t.id
			WHERE t.id = $1 AND t.org_id = $2 AND m.user_id = $3)`, *in.TeamID, *in.OrgID, id.UserID).Scan(&inTeam); err != nil {
			return err
		}
		if !inTeam {
			return httpx.BadRequest("Pick a team you're on.")
		}
		c.CountsBusy = false
	}
	if in.Name == nil {
		return httpx.BadRequest("Name the calendar (up to 100 characters).")
	}
	if err := in.apply(&c); err != nil {
		return err
	}
	var n int
	if err := s.d.Pool.QueryRow(ctx, `SELECT count(*) FROM calendar_calendars WHERE owner_id = $1`, id.UserID).Scan(&n); err != nil {
		return err
	}
	if n >= maxCalendars {
		return httpx.Conflict("You have 30 calendars. Delete one to add another.")
	}
	calID := uuid.New()
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO calendar_calendars (id, owner_id, org_id, team_id, name, description, color, counts_busy)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, calID, id.UserID, *in.OrgID, in.TeamID, c.Name, c.Description, c.OwnColor, c.CountsBusy); err != nil {
			return err
		}
		ref := access.Ref{Kind: KindCalendar, ID: calID}
		if err := access.CreateResource(ctx, tx, access.NewResource{Kind: KindCalendar, ID: calID, OrgID: *in.OrgID, OwnerID: id.UserID}); err != nil {
			return err
		}
		if in.TeamID != nil {
			return access.AddGrant(ctx, tx, ref, access.SubjectRef{Type: "team", ID: *in.TeamID}, access.Edit, id.UserID)
		}
		return nil
	})
	if err != nil {
		return err
	}
	out, err := s.loadCalendar(ctx, s.d.Pool, id.UserID, calID)
	if err != nil {
		return err
	}
	s.changed(ctx, "calendars", append(s.calendarWatchers(ctx, &calID), id.UserID)...)
	httpx.JSON(w, http.StatusCreated, out)
	return nil
}

// updateCalendarHandler changes name, description, colour and whether its
// events count as busy (manage).
func (s *Service) updateCalendarHandler(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id := auth.MustFromContext(ctx)
	calID, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var in calendarInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if in.OrgID != nil || in.TeamID != nil {
		return httpx.BadRequest("A calendar's " + orgs.Noun() + " and team can't change.")
	}
	if _, err := access.Require(ctx, s.d.Pool, id.UserID, KindCalendar, calID, access.Manage); err != nil {
		return err
	}
	c, err := s.loadCalendar(ctx, s.d.Pool, id.UserID, calID)
	if err != nil {
		return err
	}
	if err := in.apply(&c); err != nil {
		return err
	}
	if _, err := s.d.Pool.Exec(ctx, `UPDATE calendar_calendars SET name = $2, description = $3, color = $4, counts_busy = $5, updated_at = now() WHERE id = $1`,
		calID, c.Name, c.Description, c.OwnColor, c.CountsBusy); err != nil {
		return err
	}
	out, err := s.loadCalendar(ctx, s.d.Pool, id.UserID, calID)
	if err != nil {
		return err
	}
	s.changed(ctx, "calendars", append(s.calendarWatchers(ctx, &calID), id.UserID)...)
	httpx.OK(w, out)
	return nil
}

// calendarPrefsHandler saves the viewer's own view of a calendar: colour,
// hidden, and whether a shared calendar is in their list.
func (s *Service) calendarPrefsHandler(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id := auth.MustFromContext(ctx)
	calID, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var in struct {
		Color  json.RawMessage `json:"color"`
		Hidden *bool           `json:"hidden"`
		Added  *bool           `json:"added"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	c, err := s.loadCalendar(ctx, s.d.Pool, id.UserID, calID)
	if err != nil {
		return err
	}
	var color *string
	if c.Color != c.OwnColor {
		v := c.Color
		color = &v
	}
	if len(in.Color) > 0 {
		color = nil
		if string(in.Color) != "null" {
			var v string
			if json.Unmarshal(in.Color, &v) != nil || !validColor(v) {
				return httpx.BadRequest("Pick one of the calendar colors.")
			}
			color = &v
		}
	}
	hidden, added := c.Hidden, c.Added
	if in.Hidden != nil {
		hidden = *in.Hidden
	}
	if in.Added != nil {
		if c.Mine && !*in.Added {
			return httpx.BadRequest("Your own calendars stay in your list. Hide it instead.")
		}
		added = *in.Added
	}
	if _, err := s.d.Pool.Exec(ctx, `
		INSERT INTO calendar_calendar_prefs (user_id, calendar_id, color, hidden, added) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (user_id, calendar_id) DO UPDATE SET color = EXCLUDED.color, hidden = EXCLUDED.hidden, added = EXCLUDED.added, updated_at = now()`,
		id.UserID, calID, color, hidden, added); err != nil {
		return err
	}
	out, err := s.loadCalendar(ctx, s.d.Pool, id.UserID, calID)
	if err != nil {
		return err
	}
	s.changed(ctx, "calendars", id.UserID)
	httpx.OK(w, out)
	return nil
}

// deleteCalendarHandler deletes a calendar that isn't primary, with its
// events (manage): guests of upcoming events get cancellations.
func (s *Service) deleteCalendarHandler(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id := auth.MustFromContext(ctx)
	calID, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	if _, err := access.Require(ctx, s.d.Pool, id.UserID, KindCalendar, calID, access.Manage); err != nil {
		return err
	}
	var primary bool
	if err := s.d.Pool.QueryRow(ctx, `SELECT is_primary FROM calendar_calendars WHERE id = $1`, calID).Scan(&primary); err != nil {
		return err
	}
	if primary {
		return httpx.BadRequest("Your primary calendar can't be deleted.")
	}
	watchers := s.calendarWatchers(ctx, &calID)
	var users []uuid.UUID
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT 1 FROM calendar_calendars WHERE id = $1 FOR UPDATE`, calID); err != nil {
			return err
		}
		// Booked meetings move to their host's primary calendar rather than vanish.
		if _, err := tx.Exec(ctx, `UPDATE calendar_events e SET calendar_id = (SELECT c.id FROM calendar_calendars c WHERE c.owner_id = e.owner_id AND c.is_primary)
			WHERE e.calendar_id = $1 AND EXISTS (SELECT 1 FROM calendar_bookings b WHERE b.event_id = e.id AND b.status = 'confirmed')`, calID); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT `+eventCols+` FROM calendar_events e JOIN users u ON u.id = e.owner_id
			WHERE e.calendar_id = $1 AND e.recurring_event_id IS NULL AND e.status = 'confirmed'
			  AND (e.ends_at > now() OR e.rrule IS NOT NULL AND (e.series_until IS NULL OR e.series_until > now()))
			  AND EXISTS (SELECT 1 FROM calendar_attendees a WHERE a.event_id = e.id) LIMIT 500`, calID)
		if err != nil {
			return err
		}
		evs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (eventRow, error) { return scanEvent(row) })
		if err != nil {
			return err
		}
		for _, e := range evs {
			atts, err := loadFullAttendees(ctx, tx, e.ID)
			if err != nil {
				return err
			}
			users = append(users, platformIDs(atts)...)
			exceptions, err := loadExceptions(ctx, tx, e.ID)
			if err != nil {
				return err
			}
			if err := s.notifyAttendees(ctx, tx, e, exceptions, atts, inviteCancelled); err != nil {
				return err
			}
			if err := enqueueConferenceCancel(ctx, tx, e.ConferenceID); err != nil {
				return err
			}
		}
		if err := access.DeleteResource(ctx, tx, access.Ref{Kind: KindCalendar, ID: calID}); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `DELETE FROM calendar_calendars WHERE id = $1`, calID)
		return err
	})
	if err != nil {
		return err
	}
	kickOutbox()
	access.Changed(ctx, access.Ref{Kind: KindCalendar, ID: calID}, nil)
	s.changed(ctx, "calendars", append(watchers, id.UserID)...)
	s.changed(ctx, "events", append(append(watchers, users...), id.UserID)...)
	httpx.NoContent(w)
	return nil
}

// targetCalendar checks that caller may add events to calID (edit) and
// returns who organizes them: the owner of a personal calendar (someone
// acting for them), the caller on a team calendar.
func (s *Service) targetCalendar(ctx context.Context, caller, calID uuid.UUID) (organizer uuid.UUID, err error) {
	if _, err := access.Require(ctx, s.d.Pool, caller, KindCalendar, calID, access.Edit); err != nil {
		var he *httpx.Error
		if asHTTP(err, &he) && he.Status == http.StatusForbidden {
			return uuid.Nil, httpx.Forbidden("You can see this calendar but can't add events to it.")
		}
		return uuid.Nil, err
	}
	var owner *uuid.UUID
	var team *uuid.UUID
	if err := s.d.Pool.QueryRow(ctx, `SELECT owner_id, team_id FROM calendar_calendars WHERE id = $1`, calID).Scan(&owner, &team); err != nil {
		return uuid.Nil, err
	}
	if team == nil && owner != nil {
		return *owner, nil
	}
	return caller, nil
}

// rehomeOnLeave keeps calendars personal when their owner leaves a
// workspace: their own calendars there (not team calendars) stop being
// shared through it and become personal, so the workspace's admins never
// gain them. Team calendars stay with the workspace (its admins manage
// them once the creator is gone).
func (s *Service) rehomeOnLeave(ctx context.Context, orgID, userID uuid.UUID) {
	rows, err := s.d.Pool.Query(ctx, `SELECT id FROM calendar_calendars WHERE owner_id = $1 AND org_id = $2 AND team_id IS NULL`, userID, orgID)
	if err != nil {
		slog.WarnContext(ctx, "calendar: rehome failed", "err", err)
		return
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		slog.WarnContext(ctx, "calendar: rehome failed", "err", err)
		return
	}
	for _, id := range ids {
		ref := access.Ref{Kind: KindCalendar, ID: id}
		err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
			if err := access.DeleteResource(ctx, tx, ref); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE calendar_calendars SET org_id = NULL, updated_at = now() WHERE id = $1`, id); err != nil {
				return err
			}
			return access.CreateResource(ctx, tx, access.NewResource{Kind: KindCalendar, ID: id, OwnerID: userID})
		})
		if err != nil {
			slog.WarnContext(ctx, "calendar: rehome failed", "err", err)
			continue
		}
		access.Changed(ctx, ref, nil)
	}
}

func asHTTP(err error, target **httpx.Error) bool {
	he, ok := err.(*httpx.Error)
	if ok {
		*target = he
	}
	return ok
}

// sortOccurrences orders by start, longer first on ties.
func sortOccurrences(out []Occurrence) {
	slices.SortStableFunc(out, func(a, b Occurrence) int {
		if c := a.StartsAt.Compare(b.StartsAt); c != 0 {
			return c
		}
		return b.EndsAt.Compare(a.EndsAt)
	})
}
