package calendar

// The read seam for link cards: what a viewer may see of an event without
// opening it, for other features' previews (channels' unfurlers) through
// the app's stitching.

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// LinkedEvent is one event (an occurrence, for a series) as one viewer may
// see it from a link. Busy viewers (the event's calendar is shared with
// them at free/busy only) get Busy set, Title "Busy", the times, and a Path
// to that day in their own calendar (the event page doesn't open for them);
// every other field is empty.
type LinkedEvent struct {
	ID         uuid.UUID  `json:"id"`         // the single event or series master
	Occurrence *time.Time `json:"occurrence"` // a series: the occurrence's start (its original start, when moved)
	Busy       bool       `json:"busy"`
	Title      string     `json:"title"`
	StartsAt   time.Time  `json:"starts_at"`
	EndsAt     time.Time  `json:"ends_at"`
	AllDay     bool       `json:"all_day"`
	When       string     `json:"when"`   // "Mon, Nov 9, 2026 · 10:00 AM – 11:00 AM PST" in the viewer's zone; all-day: the event's dates
	Repeat     string     `json:"repeat"` // a series: "Every week on Monday"
	Location   string     `json:"location"`
	Organizer  string     `json:"organizer"`  // the owner's display name
	Snippet    string     `json:"snippet"`    // the description's first line, ≤ 140 characters
	EventType  string     `json:"event_type"` // default | out_of_office | focus
	Path       string     `json:"path"`       // /calendar/event/<id>, with ?occurrence= for a series; Busy: /calendar?view=day&date=<day>
	UpdatedAt  *time.Time `json:"updated_at"`
}

// ErrEventNotFound is EventRef's answer when the event (or occurrence)
// doesn't exist, was cancelled or deleted, or the viewer can't see it at
// all; they look the same.
var ErrEventNotFound = errEventNotFound

// EventRef reads an event as viewer with the checks of GET
// /calendar/events/{id} (?occurrence=): the owner, a guest, or someone its
// calendar is shared with at "all details" or more sees it; someone it's
// shared with at free/busy gets a Busy event (times only, as their calendar
// grid shows it); anyone else, a cancelled event or occurrence, and an
// occurrence outside the series get ErrEventNotFound. When ctx is an agent
// call the calendar level is capped by the agent scope (access.ViaAgent)
// and the event must be readable under it (calendar.event), as for the
// calendar_get_event tool. Nothing is recorded.
func EventRef(ctx context.Context, q db.Querier, viewer *auth.Identity, eventID uuid.UUID, occurrence *time.Time) (*LinkedEvent, error) {
	if viewer == nil || viewer.UserID == uuid.Nil || eventID == uuid.Nil {
		return nil, ErrEventNotFound
	}
	s := calendarService.Load()
	if s == nil {
		return nil, errors.New("calendar: not initialised")
	}
	r, err := locateEvent(ctx, q, eventID, occurrence)
	var he *httpx.Error
	if errors.As(err, &he) && he.Status == 404 {
		return nil, ErrEventNotFound
	}
	if err != nil {
		return nil, err
	}
	if r.base.Status != "confirmed" || (r.exception != nil && r.exception.Status == "cancelled") {
		return nil, ErrEventNotFound
	}
	acc, err := loadAccess(ctx, q, r.base, viewer.UserID)
	if err != nil {
		return nil, err
	}
	busy := false
	switch {
	case acc.canView():
	case acc.level >= access.View:
		busy = true
	default:
		return nil, ErrEventNotFound
	}
	readable, err := s.agentReadable(ctx, viewer)
	if err != nil {
		return nil, err
	}
	if !readable(KindEvent, r.base.ID) {
		return nil, ErrEventNotFound
	}

	inst := r.instance()
	loc := inst.loc()
	if !inst.AllDay {
		loc = s.zoneFor(ctx, q, viewer.UserID, loc)
	}
	out := &LinkedEvent{
		ID: r.base.ID, Busy: busy, Title: "Busy", StartsAt: inst.StartsAt, EndsAt: inst.EndsAt, AllDay: inst.AllDay,
		When: formatWhen(inst.StartsAt, inst.EndsAt, inst.AllDay, loc), EventType: TypeDefault, Path: eventLink(r.base.ID),
	}
	if r.occ != nil {
		o := r.occ.UTC()
		out.Occurrence = &o
		out.Path += "?occurrence=" + url.QueryEscape(o.Format(time.RFC3339))
	}
	if busy {
		out.Path = "/calendar?view=day&date=" + localDate(inst.StartsAt, loc)
		return out, nil
	}
	updated := inst.UpdatedAt
	out.Title, out.Location, out.Organizer, out.EventType, out.UpdatedAt = inst.Title, inst.Location, r.base.OwnerName, inst.EventType, &updated
	out.Snippet = firstLine(inst.Description, 140)
	if rule, ok := r.base.rule(); ok {
		out.Repeat = describeRule(rule, r.base.loc())
	}
	return out, nil
}

// EventFromURL reads an event link's id and occurrence
// ("/calendar/event/<id>", "?occurrence=<RFC 3339>"). Other calendar
// screens, connected events and a malformed occurrence answer false.
func EventFromURL(u *url.URL) (uuid.UUID, *time.Time, bool) {
	if u == nil {
		return uuid.Nil, nil, false
	}
	rest, ok := strings.CutPrefix(u.Path, "/calendar/event/")
	if !ok {
		return uuid.Nil, nil, false
	}
	rest = strings.TrimSuffix(rest, "/")
	if strings.Contains(rest, "/") {
		return uuid.Nil, nil, false
	}
	id, err := uuid.Parse(rest)
	if err != nil {
		return uuid.Nil, nil, false
	}
	raw := u.Query().Get("occurrence")
	if raw == "" {
		return id, nil, true
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return uuid.Nil, nil, false
	}
	t = t.UTC()
	return id, &t, true
}

// firstLine is the first non-blank line of text, cut to max characters.
func firstLine(text string, max int) string {
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.Join(strings.Fields(line), " ")
		if line == "" {
			continue
		}
		if utf8.RuneCountInString(line) > max {
			line = string([]rune(line)[:max-1]) + "…"
		}
		return line
	}
	return ""
}
