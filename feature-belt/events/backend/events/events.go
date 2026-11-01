// Package events is events with a time and place: hosts and co-hosts, RSVPs
// (going, maybe, declined) with a waitlist that promotes automatically when
// a spot frees, invite-only events, reminders, cancellation notices, and
// calendar export (single-event .ics and a per-user secret feed).
//
// Attendee lists are never public: only hosts and people who RSVP'd see
// them, and the public routes carry no attendee data at all.
package events

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"time"
	_ "time/tzdata" // IANA zones on hosts without a zoneinfo database

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/jobs"
	"scaffoldapp/backend/internal/notifications"
	"scaffoldapp/backend/internal/storage"
)

// Notification kinds this module sends.
const (
	NotifyInvite    = "events.invite"
	NotifyReminder  = "events.reminder"
	NotifyCancelled = "events.cancelled"
	NotifyUpdated   = "events.updated"
	NotifyPromoted  = "events.promoted"
	NotifyRSVP      = "events.rsvp"
)

// Realtime events on the events:<event id> topic. Payloads carry counts and
// status only, never attendee identities.
const (
	EventUpdated = "events.updated"
	EventRSVP    = "events.rsvp"
)

// CoverPurpose is the storage purpose for cover images (public-read).
const CoverPurpose = "event_cover"

// Visibility values. Members means any signed-in user; invite means hosts,
// invitees and people who already RSVP'd.
const (
	VisibilityPublic  = "public"
	VisibilityMembers = "members"
	VisibilityInvite  = "invite"
)

// RSVP statuses. Waitlisted is set by the server when a going RSVP finds the
// event full; clients only send going, maybe or declined.
const (
	RSVPGoing      = "going"
	RSVPMaybe      = "maybe"
	RSVPDeclined   = "declined"
	RSVPWaitlisted = "waitlisted"
)

const (
	maxTitle       = 200
	maxDetails     = 5000
	maxLocation    = 300
	maxURL         = 2048
	maxDuration    = 31 * 24 * time.Hour
	maxCapacity    = 100000
	maxCoHosts     = 10
	maxInvitesCall = 100
	maxWindow      = 100 * 24 * time.Hour // longest from/to calendar window
	feedPast       = 30 * 24 * time.Hour  // calendar feed keeps ended events this long
	feedMax        = 500
)

type Service struct {
	d       *app.Deps
	offsets []time.Duration // reminder offsets, ascending
}

func New(d *app.Deps) *Service {
	s := &Service{d: d, offsets: parseOffsets(config.List("EVENTS_REMINDER_OFFSETS", "24h,1h"))}
	// The calendar feed token is a credential in the path: keep it out of request logs.
	httpx.RedactPathPrefix("/v1/events/feed/")
	storage.RegisterPurpose(storage.Purpose{Name: CoverPurpose, Public: true, Types: storage.ImageTypes})

	notifications.RegisterKind(notifications.Kind{ID: NotifyInvite, Label: "Event invitations", Push: true})
	notifications.RegisterKind(notifications.Kind{ID: NotifyReminder, Label: "Event reminders", Push: true})
	notifications.RegisterKind(notifications.Kind{ID: NotifyCancelled, Label: "Cancelled events", Push: true})
	notifications.RegisterKind(notifications.Kind{ID: NotifyUpdated, Label: "Event changes", Push: true})
	notifications.RegisterKind(notifications.Kind{ID: NotifyPromoted, Label: "Spots from the waitlist", Push: true})
	notifications.RegisterKind(notifications.Kind{ID: NotifyRSVP, Label: "RSVPs to your events", Push: false})
	return s
}

func (s *Service) Name() string { return "events" }

func (s *Service) MountPublic(r chi.Router) {
	r.Get("/events/public", httpx.Handler(s.publicList))
	r.Get("/events/public/{id}", httpx.Handler(s.publicGet))
	r.Get("/events/public/{id}.ics", httpx.Handler(s.publicICS))
	r.Get("/events/feed/{token}.ics", httpx.Handler(s.calendarFeed))
}

func (s *Service) MountUser(r chi.Router) {
	write := r.With(auth.RequireActive)

	r.Get("/events", httpx.Handler(s.listHandler))
	write.With(auth.RateLimit(20, time.Hour)).Post("/events", httpx.Handler(s.createHandler))
	r.Get("/events/{id}", httpx.Handler(s.getHandler))
	r.Get("/events/{id}.ics", httpx.Handler(s.eventICS))
	write.With(auth.RateLimit(60, time.Hour)).Put("/events/{id}", httpx.Handler(s.updateHandler))
	write.Post("/events/{id}/cancel", httpx.Handler(s.cancelHandler))

	rsvp := write.With(auth.RateLimit(60, time.Minute))
	rsvp.Put("/events/{id}/rsvp", httpx.Handler(s.rsvpHandler))
	rsvp.Delete("/events/{id}/rsvp", httpx.Handler(s.unrsvpHandler))
	r.Get("/events/{id}/attendees", httpx.Handler(s.attendeesHandler))

	write.Post("/events/{id}/cohosts", httpx.Handler(s.addCoHostHandler))
	write.Delete("/events/{id}/cohosts/{user_id}", httpx.Handler(s.removeCoHostHandler))

	r.Get("/events/{id}/invites", httpx.Handler(s.listInvitesHandler))
	write.With(auth.RateLimit(30, time.Hour)).Post("/events/{id}/invites", httpx.Handler(s.inviteHandler))
	write.Delete("/events/{id}/invites/{user_id}", httpx.Handler(s.uninviteHandler))
	write.Post("/events/{id}/invite-link", httpx.Handler(s.rotateInviteLinkHandler))
	write.Delete("/events/{id}/invite-link", httpx.Handler(s.disableInviteLinkHandler))
	write.With(auth.RateLimit(30, time.Minute)).Post("/events/join", httpx.Handler(s.joinHandler))

	r.Get("/events/calendar-feed", httpx.Handler(s.getFeedTokenHandler))
	r.With(auth.RateLimit(10, time.Hour)).Post("/events/calendar-feed", httpx.Handler(s.rotateFeedTokenHandler))
	r.Delete("/events/calendar-feed", httpx.Handler(s.deleteFeedTokenHandler))
}

func (s *Service) MountAdmin(r chi.Router) {
	r.Get("/events", httpx.Handler(s.adminList))
	r.Post("/events/{id}/cancel", httpx.Handler(s.adminCancel))
}

// Start runs the reminder job and drains notifications queued inside
// account-deletion transactions.
func (s *Service) Start(ctx context.Context) {
	go jobs.Every(ctx, s.d.Pool, "events.reminders", time.Minute, s.sendReminders)
	go jobs.Every(ctx, s.d.Pool, "events.outbox", 30*time.Second, s.drainOutbox)
}

// AuthorizeTopic allows events:<id> to anyone who can see the event.
func (s *Service) AuthorizeTopic(ctx context.Context, userID uuid.UUID, isAdmin bool, topic string) (bool, bool) {
	rest, ok := strings.CutPrefix(topic, "events:")
	if !ok {
		return false, false
	}
	id, err := uuid.Parse(rest)
	if err != nil {
		return true, false
	}
	var hostID uuid.UUID
	var visibility string
	if err := s.d.Pool.QueryRow(ctx, `SELECT host_id, visibility FROM events WHERE id = $1`, id).Scan(&hostID, &visibility); err != nil {
		return true, false
	}
	if isAdmin {
		return true, true
	}
	a, err := loadAccess(ctx, s.d.Pool, id, hostID, userID)
	if err != nil {
		slog.WarnContext(ctx, "events: topic access", "err", err)
		return true, false
	}
	return true, a.canView(visibility)
}

// parseOffsets reads EVENTS_REMINDER_OFFSETS: durations between 1 minute
// and 30 days, returned ascending. A bad entry fails the boot (config.Check).
func parseOffsets(list []string) []time.Duration {
	var out []time.Duration
	for _, v := range list {
		d, err := time.ParseDuration(v)
		if err != nil || d < time.Minute || d > 30*24*time.Hour {
			config.Problem("EVENTS_REMINDER_OFFSETS must be durations between 1m and 720h, like 24h,1h (got %q)", v)
			continue
		}
		if !slices.Contains(out, d) {
			out = append(out, d)
		}
	}
	slices.Sort(out)
	return out
}
