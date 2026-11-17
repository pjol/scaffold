package calendar

import (
	"context"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// viewerRow is an event row with what the viewer's calendar needs to know.
type viewerRow struct {
	eventRow
	isOwner       bool
	response      *string
	attendeeCount int
	bookingID     *uuid.UUID
	roomCount     int
	// viewCalendar is the calendar the viewer sees it on (invitations: their
	// primary); level < Comment redacts it (a calendar shared at free/busy).
	viewCalendar *uuid.UUID
	redact       bool
}

// occurrences expands every event on userID's calendar (events they own
// and events they're invited to, declined included) that overlaps
// [from, to), sorted by start, at most limit.
func (s *Service) occurrences(ctx context.Context, q db.Querier, userID uuid.UUID, from, to time.Time, limit int) ([]Occurrence, error) {
	rows, err := q.Query(ctx, `
		SELECT `+eventCols+`, (e.owner_id = $1), a.response,
		       (SELECT count(*) FROM calendar_attendees x WHERE x.event_id = e.id)::int,
		       (SELECT b.id FROM calendar_bookings b WHERE b.event_id = e.id AND b.status = 'confirmed' LIMIT 1),
		       (SELECT count(*) FROM calendar_event_rooms r WHERE r.event_id = e.id)::int
		FROM calendar_events e
		JOIN users u ON u.id = e.owner_id
		LEFT JOIN calendar_attendees a ON a.event_id = e.id AND a.user_id = $1
		WHERE e.recurring_event_id IS NULL AND e.status = 'confirmed'
		  AND (e.owner_id = $1 OR a.id IS NOT NULL)
		  AND e.starts_at < $3
		  AND ((e.rrule IS NULL AND e.ends_at > $2) OR (e.rrule IS NOT NULL AND (e.series_until IS NULL OR e.series_until > $2)))
		ORDER BY e.starts_at, e.id LIMIT 5000`, userID, from, to)
	if err != nil {
		return nil, err
	}
	var list []viewerRow
	for rows.Next() {
		var v viewerRow
		err := rows.Scan(append(v.dest(), &v.isOwner, &v.response, &v.attendeeCount, &v.bookingID, &v.roomCount)...)
		if err != nil {
			rows.Close()
			return nil, err
		}
		list = append(list, v)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	primary, err := primaryCalendarID(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	for i := range list {
		list[i].viewCalendar = primary
		if list[i].isOwner && list[i].CalendarID != nil {
			list[i].viewCalendar = list[i].CalendarID
		}
	}
	return s.expand(ctx, q, list, from, to, limit)
}

// expand turns single events and series masters into occurrences in
// [from, to) (exceptions applied), sorted by start, at most limit.
func (s *Service) expand(ctx context.Context, q db.Querier, list []viewerRow, from, to time.Time, limit int) ([]Occurrence, error) {
	var masterIDs []uuid.UUID
	masters := map[uuid.UUID]viewerRow{}
	for _, v := range list {
		if v.RRule != nil {
			masterIDs = append(masterIDs, v.ID)
			masters[v.ID] = v
		}
	}
	exceptions := map[uuid.UUID][]eventRow{}
	if len(masterIDs) > 0 {
		erows, err := q.Query(ctx, `
			SELECT `+eventCols+` FROM calendar_events e JOIN users u ON u.id = e.owner_id
			WHERE e.recurring_event_id = ANY($1)
			  AND ((e.original_start >= $2::timestamptz - interval '15 days' AND e.original_start < $3::timestamptz + interval '15 days')
			       OR (e.starts_at < $3 AND e.ends_at > $2))`, masterIDs, from, to)
		if err != nil {
			return nil, err
		}
		for erows.Next() {
			e, err := scanEvent(erows)
			if err != nil {
				erows.Close()
				return nil, err
			}
			exceptions[*e.RecurringEventID] = append(exceptions[*e.RecurringEventID], e)
		}
		erows.Close()
		if err := erows.Err(); err != nil {
			return nil, err
		}
	}

	out := []Occurrence{}
	for _, v := range list {
		if v.RRule == nil {
			out = append(out, occurrenceOf(v, v.eventRow, nil))
			continue
		}
		rule, ok := v.rule()
		if !ok {
			continue
		}
		skip := map[int64]bool{}
		for _, x := range exceptions[v.ID] {
			skip[x.OriginalStart.Unix()] = true
		}
		dur := v.duration()
		for _, start := range rule.Expand(v.StartsAt, v.loc(), from.Add(-dur), to, maxOccurrences, skip) {
			inst := v.eventRow
			inst.StartsAt, inst.EndsAt = start, start.Add(dur)
			st := start
			out = append(out, occurrenceOf(v, inst, &st))
		}
		for _, x := range exceptions[v.ID] {
			if x.Status != "confirmed" || !x.StartsAt.Before(to) || !x.EndsAt.After(from) {
				continue
			}
			o := occurrenceOf(masters[v.ID], x, x.OriginalStart)
			o.Modified = true
			out = append(out, o)
		}
	}
	slices.SortStableFunc(out, func(a, b Occurrence) int {
		if c := a.StartsAt.Compare(b.StartsAt); c != 0 {
			return c
		}
		return b.EndsAt.Compare(a.EndsAt)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// occurrenceOf builds an occurrence from row (the instance's fields) with
// the viewer data of v (the single event or master). occ is set for series.
func occurrenceOf(v viewerRow, row eventRow, occ *time.Time) Occurrence {
	o := Occurrence{
		EventID:       v.ID,
		Recurring:     occ != nil,
		Title:         row.Title,
		Location:      row.Location,
		ConferenceURL: row.ConferenceURL,
		StartsAt:      row.StartsAt,
		EndsAt:        row.EndsAt,
		AllDay:        row.AllDay,
		Timezone:      row.Timezone,
		Color:         row.Color,
		ShowAs:        row.ShowAs,
		Visibility:    row.Visibility,
		Role:          "attendee",
		MyResponse:    v.response,
		Owner:         Person{ID: v.OwnerID, DisplayName: v.OwnerName, AvatarURL: v.OwnerAvatar},
		AttendeeCount: v.attendeeCount,
		BookingID:     v.bookingID,
		CalendarID:    v.viewCalendar,
		EventType:     row.EventType,
		RoomCount:     v.roomCount,
	}
	if o.EventType == "" {
		o.EventType = "default"
	}
	if v.redact {
		// A calendar shared at free/busy: times, and the title only when the
		// owner shows it to colleagues.
		o.Redacted, o.Location, o.ConferenceURL, o.AttendeeCount, o.BookingID, o.RoomCount = true, "", nil, 0, nil, 0
		if row.Visibility != "details" {
			o.Title = "Busy"
		}
	}
	if occ != nil {
		t := occ.UTC()
		o.OccurrenceStart = &t
	}
	switch {
	case v.isOwner:
		o.Role = "owner"
		o.MyResponse = nil
	case v.response == nil:
		o.Role = "calendar" // seen through a shared calendar
	}
	if row.AllDay {
		o.StartDate, o.EndDate = allDayDates(row.StartsAt, row.EndsAt, row.loc())
	}
	return o
}

// eventAccess is the viewer's relation to a single event or series master:
// its owner, a guest, and/or someone the event's calendar is shared with
// (level: view = free/busy, comment = details, edit = make changes).
type eventAccess struct {
	isOwner  bool
	attendee *Attendee
	level    access.Level
}

func (a eventAccess) canView() bool {
	return a.isOwner || a.attendee != nil || a.level >= access.Comment
}

// canEdit: the owner, or someone who may make changes to its calendar.
func (a eventAccess) canEdit() bool { return a.isOwner || a.level >= access.Edit }

// loadAccess finds the viewer's attendee row on the event (single or master)
// and, when they neither own nor attend it, their level on its calendar.
func loadAccess(ctx context.Context, q db.Querier, e eventRow, viewer uuid.UUID) (eventAccess, error) {
	a := eventAccess{isOwner: e.OwnerID == viewer}
	var at Attendee
	err := q.QueryRow(ctx, `
		SELECT a.id, a.user_id, a.email, COALESCE(u.display_name, a.name), u.avatar_url, a.response, a.responded_at, a.note,
		       a.proposed_start, a.proposed_end
		FROM calendar_attendees a LEFT JOIN users u ON u.id = a.user_id
		WHERE a.event_id = $1 AND a.user_id = $2`, e.ID, viewer).
		Scan(&at.ID, &at.UserID, &at.Email, &at.Name, &at.AvatarURL, &at.Response, &at.RespondedAt, &at.Note,
			&at.ProposedStart, &at.ProposedEnd)
	if err == nil {
		a.attendee = &at
	} else if !db.IsNoRows(err) {
		return a, err
	}
	if !a.isOwner && e.CalendarID != nil {
		lvl, err := calendarLevel(ctx, q, viewer, *e.CalendarID)
		if err != nil {
			return a, err
		}
		a.level = lvl
	}
	return a, nil
}

func listAttendees(ctx context.Context, q db.Querier, eventID uuid.UUID) ([]Attendee, error) {
	rows, err := q.Query(ctx, `
		SELECT a.id, a.user_id, a.email, COALESCE(u.display_name, a.name), u.avatar_url, a.response, a.responded_at, a.note,
		       a.proposed_start, a.proposed_end
		FROM calendar_attendees a LEFT JOIN users u ON u.id = a.user_id
		WHERE a.event_id = $1 ORDER BY a.created_at, a.id`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Attendee{}
	for rows.Next() {
		var a Attendee
		if err := rows.Scan(&a.ID, &a.UserID, &a.Email, &a.Name, &a.AvatarURL, &a.Response, &a.RespondedAt, &a.Note,
			&a.ProposedStart, &a.ProposedEnd); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// resolved is a request's target: the series master (or single event) the
// viewer may see, plus the occurrence (series) and its exception row.
type resolved struct {
	base      eventRow // single event or series master
	occ       *time.Time
	exception *eventRow // edited or cancelled occurrence
	acc       eventAccess
}

// instance is the occurrence's effective row (exception fields or the
// master shifted to the occurrence).
func (r resolved) instance() eventRow {
	if r.exception != nil {
		return *r.exception
	}
	inst := r.base
	if r.occ != nil {
		inst.StartsAt = *r.occ
		inst.EndsAt = r.occ.Add(r.base.duration())
	}
	return inst
}

// resolve loads id (a single event, master or exception) for viewer and,
// for series, pins the occurrence (occ, or the exception's, or the first).
// Missing, cancelled or not-visible targets are 404.
func (s *Service) resolve(ctx context.Context, q db.Querier, viewer, id uuid.UUID, occ *time.Time) (resolved, error) {
	r, err := locateEvent(ctx, q, id, occ)
	if err != nil {
		return resolved{}, err
	}
	if r.acc, err = loadAccess(ctx, q, r.base, viewer); err != nil {
		return resolved{}, err
	}
	if !r.acc.canView() || r.base.Status != "confirmed" {
		return resolved{}, errEventNotFound
	}
	if r.exception != nil && r.exception.Status == "cancelled" {
		return resolved{}, httpx.NotFound("That occurrence was deleted.")
	}
	return r, nil
}

// locateEvent finds id's series master (or single event), the occurrence
// and its exception row, without checking who may see it (resolve and
// EventRef do that).
func locateEvent(ctx context.Context, q db.Querier, id uuid.UUID, occ *time.Time) (resolved, error) {
	row, err := loadEventRow(ctx, q, id)
	if err != nil {
		return resolved{}, err
	}
	var r resolved
	if row.RecurringEventID != nil {
		master, err := loadEventRow(ctx, q, *row.RecurringEventID)
		if err != nil {
			return resolved{}, err
		}
		o := row.OriginalStart.UTC()
		r = resolved{base: master, occ: &o, exception: &row}
	} else {
		r = resolved{base: row}
		if row.RRule != nil {
			start := row.StartsAt.UTC()
			if occ != nil {
				start = occ.UTC()
			}
			r.occ = &start
			x, err := scanEvent(q.QueryRow(ctx, `SELECT `+eventCols+` FROM calendar_events e JOIN users u ON u.id = e.owner_id
				WHERE e.recurring_event_id = $1 AND e.original_start = $2`, row.ID, start))
			switch {
			case err == nil:
				r.exception = &x
			case !db.IsNoRows(err):
				return resolved{}, err
			default:
				rule, ok := row.rule()
				if !ok || !rule.Occurs(row.StartsAt, row.loc(), start) {
					return resolved{}, httpx.NotFound("That occurrence isn't part of this series.")
				}
			}
		}
	}
	return r, nil
}

// detail builds the dialog view of a resolved event for viewer.
func (s *Service) detail(ctx context.Context, q db.Querier, viewer uuid.UUID, r resolved) (EventDetail, error) {
	attendees, err := listAttendees(ctx, q, r.base.ID)
	if err != nil {
		return EventDetail{}, err
	}
	var bookingID *uuid.UUID
	var b uuid.UUID
	if err := q.QueryRow(ctx, `SELECT id FROM calendar_bookings WHERE event_id = $1 AND status = 'confirmed' LIMIT 1`, r.base.ID).Scan(&b); err == nil {
		bookingID = &b
	}
	v := viewerRow{eventRow: r.base, isOwner: r.acc.isOwner, attendeeCount: len(attendees), bookingID: bookingID, viewCalendar: r.base.CalendarID}
	rooms, err := eventRooms(ctx, q, r.base.ID)
	if err != nil {
		return EventDetail{}, err
	}
	v.roomCount = len(rooms)
	if r.acc.attendee != nil {
		if p, err := primaryCalendarID(ctx, q, viewer); err == nil && p != nil {
			v.viewCalendar = p
		}
	}
	if r.acc.attendee != nil {
		resp := r.acc.attendee.Response
		v.response = &resp
	}
	inst := r.instance()
	d := EventDetail{
		Occurrence:     occurrenceOf(v, inst, r.occ),
		Description:    inst.Description,
		Attendees:      attendees,
		Reminders:      inst.reminders(),
		CanEdit:        r.acc.canEdit(),
		CreatedAt:      inst.CreatedAt,
		UpdatedAt:      inst.UpdatedAt,
		AutoDecline:    r.base.AutoDecline,
		DeclineMessage: r.base.DeclineMessage,
		Rooms:          rooms,
	}
	if r.base.CalendarID != nil && (r.acc.isOwner || r.acc.level > access.None) {
		if c, err := calendarRefOf(ctx, q, *r.base.CalendarID); err == nil {
			d.Calendar = &c
		}
	}
	if !r.acc.isOwner && r.acc.attendee == nil && !r.acc.canEdit() {
		// Details through a shared calendar: no guest notes or proposals.
		for i := range d.Attendees {
			d.Attendees[i].Note, d.Attendees[i].ProposedStart, d.Attendees[i].ProposedEnd = "", nil, nil
		}
	}
	d.Modified = r.exception != nil
	if rule, ok := r.base.rule(); ok {
		d.Recurrence = recurrenceOf(rule, r.base.loc())
		d.RRule = rule.String(r.base.AllDay, r.base.loc())
	}
	if !r.acc.isOwner {
		d.Reminders = []int{}
	}
	if r.acc.attendee != nil && !r.acc.canEdit() {
		// Guests see their own note and proposal, not other guests'.
		for i := range d.Attendees {
			if d.Attendees[i].ID != r.acc.attendee.ID {
				d.Attendees[i].Note, d.Attendees[i].ProposedStart, d.Attendees[i].ProposedEnd = "", nil, nil
			}
		}
	}
	return d, nil
}

// parseWindow reads from/to (RFC 3339) bounded by maxWindow.
func parseWindow(fromS, toS string, maxLen time.Duration) (time.Time, time.Time, error) {
	from, err1 := time.Parse(time.RFC3339, fromS)
	to, err2 := time.Parse(time.RFC3339, toS)
	if err1 != nil || err2 != nil || !to.After(from) || to.Sub(from) > maxLen {
		return time.Time{}, time.Time{}, httpx.BadRequest("from and to must be RFC 3339 times, to after from, at most " + strconv.Itoa(int(maxLen/(24*time.Hour))) + " days apart.")
	}
	return from.UTC(), to.UTC(), nil
}
