package crm

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
)

// Meetings and booking links come from the app's calendar through two seams
// the product's stitching wires (the CRM never imports a calendar feature):
//
//	crm.SetMeetingSource(fn)  events on the person's calendars with given guests
//	crm.SetBookingLinks(fn)   the person's booking links, for "Insert booking link"
//
// Unwired, the routes answer available: false and screens hide both actions.

// MeetingCandidate is an event on the caller's calendars with a contact as a guest.
type MeetingCandidate struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Start     time.Time `json:"start"`
	End       time.Time `json:"end"`
	Attendees []string  `json:"attendees"` // emails
	Location  string    `json:"location,omitempty"`
	Source    string    `json:"source,omitempty"` // e.g. "Google Calendar · sam@acme.com"; "" for the app's calendar
}

// MeetingSource lists events on caller's own calendars (in orgID's context)
// between from and to that have any of emails as a guest. It runs as the
// caller: only events the person can see.
type MeetingSource func(ctx context.Context, caller *auth.Identity, orgID uuid.UUID, emails []string, from, to time.Time) ([]MeetingCandidate, error)

// BookingLink is a page where people book time with the caller.
type BookingLink struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

// BookingLinks lists the caller's booking links that anyone outside the org
// can open (in orgID's context).
type BookingLinks func(ctx context.Context, caller *auth.Identity, orgID uuid.UUID) ([]BookingLink, error)

var (
	seamMu        sync.RWMutex
	meetingSource MeetingSource
	bookingLinks  BookingLinks
)

// SetMeetingSource wires "Log meeting" to a calendar (call from stitching).
func SetMeetingSource(fn MeetingSource) {
	seamMu.Lock()
	defer seamMu.Unlock()
	meetingSource = fn
}

// SetBookingLinks wires "Insert booking link" to a calendar (call from stitching).
func SetBookingLinks(fn BookingLinks) {
	seamMu.Lock()
	defer seamMu.Unlock()
	bookingLinks = fn
}

func currentMeetingSource() MeetingSource {
	seamMu.RLock()
	defer seamMu.RUnlock()
	return meetingSource
}

func currentBookingLinks() BookingLinks {
	seamMu.RLock()
	defer seamMu.RUnlock()
	return bookingLinks
}

func (s *Service) bookingLinksFor(ctx context.Context, userID, orgID uuid.UUID) ([]BookingLink, error) {
	fn := currentBookingLinks()
	if fn == nil {
		return nil, nil
	}
	links, err := fn(ctx, &auth.Identity{UserID: userID}, orgID)
	if err != nil {
		return nil, err
	}
	out := []BookingLink{}
	for _, l := range links {
		if strings.HasPrefix(l.URL, "https://") || strings.HasPrefix(l.URL, "http://") {
			out = append(out, l)
		}
	}
	return out, nil
}

// getBookingLinks: GET /v1/crm/mail/booking-links?org_id= (member + crm.access).
func (s *Service) getBookingLinks(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	orgID, err := uuid.Parse(r.URL.Query().Get("org_id"))
	if err != nil {
		return httpx.BadRequest("org_id is required.")
	}
	if err := s.requireOrgCRM(ctx, orgID, me(r)); err != nil {
		return err
	}
	if currentBookingLinks() == nil {
		httpx.OK(w, map[string]any{"available": false, "items": []BookingLink{}})
		return nil
	}
	links, err := s.bookingLinksFor(ctx, me(r), orgID)
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"available": true, "items": links})
	return nil
}

// meetingWindow is how far back and ahead "Log meeting" looks.
const (
	meetingsBack  = 30 * 24 * time.Hour
	meetingsAhead = 24 * time.Hour
)

func (s *Service) contactMeetings(ctx context.Context, r *http.Request, contactID uuid.UUID, min access.Level) (*Contact, uuid.UUID, []MeetingCandidate, bool, error) {
	userID := me(r)
	orgID, _, err := s.requireRecord(ctx, s.d.Pool, userID, objContact, contactID, min)
	if err != nil {
		return nil, uuid.Nil, nil, false, err
	}
	rec, err := s.getOne(ctx, s.d.Pool, userID, objContact, contactID)
	if err != nil {
		return nil, uuid.Nil, nil, false, err
	}
	c := rec.(*Contact)
	fn := currentMeetingSource()
	emails := contactEmails(c)
	if fn == nil || len(emails) == 0 {
		return c, orgID, []MeetingCandidate{}, fn != nil, nil
	}
	now := time.Now()
	list, err := fn(ctx, auth.MustFromContext(ctx), orgID, emails, now.Add(-meetingsBack), now.Add(meetingsAhead))
	if err != nil {
		return nil, uuid.Nil, nil, true, err
	}
	if list == nil {
		list = []MeetingCandidate{}
	}
	return c, orgID, list, true, nil
}

// meetingCandidates: GET /v1/crm/contacts/{id}/meetings (view on the contact):
// the caller's events of the last 30 days (and the next day) with the contact.
func (s *Service) meetingCandidates(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	contactID, err := idParam(r, "id")
	if err != nil {
		return err
	}
	_, _, list, available, err := s.contactMeetings(ctx, r, contactID, access.View)
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"available": available, "items": list})
	return nil
}

type logMeetingReq struct {
	EventID  string     `json:"event_id"`
	Body     string     `json:"body"`
	Outcome  *string    `json:"outcome"`
	DealID   *uuid.UUID `json:"deal_id"`
	TicketID *uuid.UUID `json:"ticket_id"`
}

// logMeeting: POST /v1/crm/contacts/{id}/meetings/log {event_id, body?, outcome?}
// (comment on the contact): logs one of the candidates, read again from the
// calendar, as a meeting on the timeline.
func (s *Service) logMeeting(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	contactID, err := idParam(r, "id")
	if err != nil {
		return err
	}
	var req logMeetingReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	_, orgID, list, available, err := s.contactMeetings(ctx, r, contactID, access.Comment)
	if err != nil {
		return err
	}
	if !available {
		return httpx.NewError(http.StatusNotFound, "meetings_unavailable", "Meetings from calendars aren't set up here. Log the meeting by hand.")
	}
	var ev *MeetingCandidate
	for i := range list {
		if list[i].ID == req.EventID {
			ev = &list[i]
		}
	}
	if ev == nil {
		return httpx.NotFound("That event isn't on your calendars with this contact.")
	}
	subject := clip(ev.Title, 200)
	body := strings.TrimSpace(req.Body)
	if body == "" && len(ev.Attendees) > 0 {
		body = "Guests: " + strings.Join(ev.Attendees, ", ")
	}
	mins := int(ev.End.Sub(ev.Start).Minutes())
	if mins < 0 || mins > 1440 {
		mins = 0
	}
	start := ev.Start
	also, err := s.mailLinks(ctx, me(r), orgID, contactID, req.DealID, req.TicketID)
	if err != nil {
		return err
	}
	a, err := s.logActivity(ctx, me(r), objContact, contactID, activityReq{Type: "meeting", Subject: &subject, Body: &body,
		OccurredAt: &start, DurationMinutes: &mins, Outcome: req.Outcome, AlsoOn: also})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, a)
	return nil
}
