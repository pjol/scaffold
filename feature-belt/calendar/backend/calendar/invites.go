package calendar

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/mail"
	"scaffoldapp/backend/internal/notify"
)

// ---- tokens ------------------------------------------------------------------

// tokenFor returns the secret token for an RSVP or manage link. With
// CALENDAR_SECRET_KEY it is an HMAC of the row id, so every email carries
// the same working link; without one (development only) it is random and
// each new email rotates it. Only sha256(token) is stored.
func (s *Service) tokenFor(kind string, id uuid.UUID) (token string, stable bool) {
	if s.secretKey == nil {
		return randomToken(32), false
	}
	m := hmac.New(sha256.New, s.secretKey)
	m.Write([]byte("calendar:" + kind + ":"))
	m.Write(id[:])
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil)), true
}

// rsvpLink returns a working RSVP link for an external attendee, rotating
// the stored hash when tokens aren't stable.
func (s *Service) rsvpLink(ctx context.Context, q db.Querier, attendeeID uuid.UUID) (string, error) {
	token, stable := s.tokenFor("rsvp", attendeeID)
	if !stable {
		if _, err := q.Exec(ctx, `UPDATE calendar_attendees SET token_hash = $2 WHERE id = $1`, attendeeID, hashToken(token)); err != nil {
			return "", err
		}
	}
	return s.webURL("/calendar/rsvp/" + token), nil
}

// ---- series state -------------------------------------------------------------

// fullAttendee is an attendee with the address mail goes to.
type fullAttendee struct {
	ID       uuid.UUID
	UserID   *uuid.UUID
	Name     string
	Email    string // external: typed address; platform: account email
	Response string
}

func (a fullAttendee) external() bool { return a.UserID == nil }

func loadFullAttendees(ctx context.Context, q db.Querier, eventID uuid.UUID) ([]fullAttendee, error) {
	rows, err := q.Query(ctx, `
		SELECT a.id, a.user_id, COALESCE(u.display_name, a.name), COALESCE(u.email, a.email), a.response
		FROM calendar_attendees a LEFT JOIN users u ON u.id = a.user_id
		WHERE a.event_id = $1 AND (u.id IS NULL OR u.status IN ('active', 'suspended'))
		ORDER BY a.created_at, a.id`, eventID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (fullAttendee, error) {
		var a fullAttendee
		return a, row.Scan(&a.ID, &a.UserID, &a.Name, &a.Email, &a.Response)
	})
}

func loadExceptions(ctx context.Context, q db.Querier, masterID uuid.UUID) ([]eventRow, error) {
	rows, err := q.Query(ctx, `SELECT `+eventCols+` FROM calendar_events e JOIN users u ON u.id = e.owner_id
		WHERE e.recurring_event_id = $1 ORDER BY e.original_start`, masterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []eventRow
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func uid(id uuid.UUID) string { return id.String() + "@calendar.scaffoldapp" }

// seriesVEvents describes a single event, or a series master (RRULE with
// EXDATEs for deleted occurrences) and its edited occurrences.
func seriesVEvents(base eventRow, exceptions []eventRow, organizer *icsPerson, attendees []icsPerson) []icsEvent {
	loc := base.loc()
	mk := func(e eventRow) icsEvent {
		desc := e.Description
		place := e.Location
		if e.ConferenceURL != nil {
			desc = joinNonEmpty(desc, "Join the video call: "+*e.ConferenceURL)
			if place == "" {
				place = *e.ConferenceURL
			}
		}
		return icsEvent{
			UID: uid(base.ID), Sequence: base.Sequence, Start: e.StartsAt, End: e.EndsAt, AllDay: base.AllDay, Loc: loc,
			Summary: e.Title, Description: desc, Location: place, Transparent: e.ShowAs == "free",
			Organizer: organizer, Attendees: attendees, Created: e.CreatedAt, Modified: e.UpdatedAt,
		}
	}
	master := mk(base)
	if rule, ok := base.rule(); ok {
		master.Rule = rule.String(base.AllDay, loc)
	}
	out := []icsEvent{master}
	for _, x := range exceptions {
		if x.Status == "cancelled" {
			out[0].ExDates = append(out[0].ExDates, *x.OriginalStart)
			continue
		}
		ev := mk(x)
		rid := x.OriginalStart.UTC()
		ev.RecurrenceID = &rid
		out = append(out, ev)
	}
	return out
}

func joinNonEmpty(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + "\n\n" + b
}

// ownerContact is the organizer's name and address for invites.
func ownerContact(ctx context.Context, q db.Querier, ownerID uuid.UUID) (icsPerson, error) {
	var p icsPerson
	err := q.QueryRow(ctx, `SELECT display_name, email FROM users WHERE id = $1`, ownerID).Scan(&p.Name, &p.Email)
	return p, err
}

// ---- telling attendees ----------------------------------------------------------

// inviteKind is what happened to an attendee's invitation.
type inviteKind int

const (
	inviteNew inviteKind = iota
	inviteUpdated
	inviteCancelled // the event (or series) was deleted
	inviteRemoved   // this attendee was taken off the list
)

// notifyAttendees queues email (externals, with an .ics) and in-app
// notifications (platform users) describing base's current state to the
// given attendees. base must be a single event or series master; for
// inviteCancelled pass the state captured before deleting.
func (s *Service) notifyAttendees(ctx context.Context, q db.Querier, base eventRow, exceptions []eventRow, to []fullAttendee, kind inviteKind) error {
	if len(to) == 0 {
		return nil
	}
	org, err := ownerContact(ctx, q, base.OwnerID)
	if err != nil {
		return err
	}
	loc := base.loc()
	when := formatWhen(base.StartsAt, base.EndsAt, base.AllDay, loc)
	repeat := ""
	if rule, ok := base.rule(); ok {
		repeat = describeRule(rule, loc)
	}
	for _, a := range to {
		if a.UserID != nil && *a.UserID == base.OwnerID {
			continue
		}
		if !a.external() {
			if err := s.notifyPlatformAttendee(ctx, q, base, *a.UserID, org.Name, kind); err != nil {
				return err
			}
			continue
		}
		method := "REQUEST"
		subject := "Invitation: " + base.Title + " @ " + when
		lead := org.Name + " invited you to " + base.Title + "."
		switch kind {
		case inviteUpdated:
			subject = "Updated invitation: " + base.Title + " @ " + when
			lead = org.Name + " changed " + base.Title + "."
		case inviteCancelled:
			method, subject, lead = "CANCEL", "Cancelled: "+base.Title+" @ "+when, org.Name+" cancelled "+base.Title+"."
		case inviteRemoved:
			method, subject, lead = "CANCEL", "Removed: "+base.Title+" @ "+when, org.Name+" removed you from "+base.Title+"."
		}
		person := icsPerson{Name: a.Name, Email: a.Email, Response: a.Response, RSVP: method == "REQUEST"}
		events := seriesVEvents(base, exceptions, &org, []icsPerson{person})
		if method == "CANCEL" {
			events = events[:1]
			events[0].Sequence++
			events[0].Cancelled = true
		}
		paras := []string{lead, "When: " + when}
		if repeat != "" && method == "REQUEST" {
			paras = append(paras, "Repeats: "+repeat)
		}
		var links []mailLink
		if method == "REQUEST" {
			if base.Location != "" {
				paras = append(paras, "Where: "+base.Location)
			}
			if base.ConferenceURL != nil {
				links = append(links, mailLink{"Join the video call", *base.ConferenceURL})
			}
			if base.Description != "" {
				paras = append(paras, base.Description)
			}
			link, err := s.rsvpLink(ctx, q, a.ID)
			if err != nil {
				return err
			}
			if link != "" {
				links = append(links,
					mailLink{"Accept", link + "?response=accepted"},
					mailLink{"Maybe", link + "?response=tentative"},
					mailLink{"Decline", link + "?response=declined"})
			}
		}
		text, htmlBody := s.mailBody(paras, links)
		if err := enqueueMail(ctx, q, mail.Message{
			To: a.Email, Subject: subject, Text: text, HTML: htmlBody, ReplyTo: org.Email,
			Attachments: []mail.Attachment{icsAttachment(method, renderICS(method, "", events))},
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) notifyPlatformAttendee(ctx context.Context, q db.Querier, base eventRow, userID uuid.UUID, orgName string, kind inviteKind) error {
	when := s.whenFor(ctx, q, userID, base.StartsAt, base.EndsAt, base.AllDay, base.loc())
	n := notify.Notification{UserID: userID, Body: when, Link: eventLink(base.ID), Data: map[string]any{"event_id": base.ID}}
	switch kind {
	case inviteNew:
		n.Kind, n.Title = NotifyInvite, orgName+" invited you: "+base.Title
	case inviteUpdated:
		n.Kind, n.Title = NotifyUpdated, base.Title+" changed"
	case inviteCancelled:
		n.Kind, n.Title, n.Link = NotifyCancelled, base.Title+" is cancelled", "/calendar"
	case inviteRemoved:
		n.Kind, n.Title, n.Link = NotifyCancelled, "You're no longer invited to "+base.Title, "/calendar"
	}
	return enqueueNotify(ctx, q, n)
}

// tellSeries queues an update for every attendee of base (single event or
// master), read from q.
func (s *Service) tellSeries(ctx context.Context, q db.Querier, baseID uuid.UUID, kind inviteKind) error {
	base, err := loadEventRow(ctx, q, baseID)
	if err != nil {
		return err
	}
	attendees, err := loadFullAttendees(ctx, q, baseID)
	if err != nil {
		return err
	}
	var exceptions []eventRow
	if base.RRule != nil {
		if exceptions, err = loadExceptions(ctx, q, baseID); err != nil {
			return err
		}
	}
	return s.notifyAttendees(ctx, q, base, exceptions, attendees, kind)
}

// platformIDs lists attendee user ids (for realtime refreshes).
func platformIDs(list []fullAttendee) []uuid.UUID {
	var out []uuid.UUID
	for _, a := range list {
		if a.UserID != nil {
			out = append(out, *a.UserID)
		}
	}
	return out
}

// whenFor words a range for userID: timed events in their zone (else
// eventLoc, the organizer's, named in the text), all-day events as the
// event's own dates, which don't move with the reader's zone.
func (s *Service) whenFor(ctx context.Context, q db.Querier, userID uuid.UUID, start, end time.Time, allDay bool, eventLoc *time.Location) string {
	if allDay {
		return formatWhen(start, end, true, eventLoc)
	}
	return formatWhen(start, end, false, s.zoneFor(ctx, q, userID, eventLoc))
}
