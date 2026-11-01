// Package calendar is a personal calendar with booking links: events with
// time zones, recurrence (RRULE subset, edits and deletes for one, the
// following or all occurrences), attendees from the user's organizations
// and by email (iCalendar invites, RSVP in-app or by a tokenized link),
// reminders, busy time from external calendars subscribed by secret iCal
// URL, a personal iCal feed, colleagues' free/busy, and booking links
// (internal = members of the link's org, external = anyone) with race-safe
// bookings, a manage link for the booker, and optional video rooms through
// the Conferencing seam.
//
// Nobody sees another person's event details through this package except
// attendees of that event. Colleagues get free/busy only (titles when the
// owner marked an event "details"), and booking pages show free slots,
// never events.
package calendar

import (
	"context"
	"encoding/base64"
	"log/slog"
	"net/http"
	"strings"
	"time"
	_ "time/tzdata" // IANA zones on hosts without a zoneinfo database

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/httprate"
	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/jobs"
	"scaffoldapp/backend/internal/notifications"
	"scaffoldapp/backend/internal/realtime"
)

// Notification kinds this module sends to platform users.
const (
	NotifyInvite    = "calendar.invite"
	NotifyUpdated   = "calendar.updated"
	NotifyCancelled = "calendar.cancelled"
	NotifyResponse  = "calendar.response"
	NotifyReminder  = "calendar.reminder"
	NotifyBooking   = "calendar.booking"
)

// EventChanged is published on the user:<id> topic of everyone whose
// calendar changed; clients refetch. Payload {scope}: events, bookings,
// links or subscriptions.
const EventChanged = "calendar.changed"

const (
	maxTitle        = 200
	maxDescription  = 8000
	maxLocation     = 500
	maxAttendees    = 100
	maxReminders    = 5
	maxReminderMins = 4 * 7 * 24 * 60
	maxWindow       = 62 * 24 * time.Hour // longest range a calendar query covers
	maxOccurrences  = 2000
	maxEventLength  = 14 * 24 * time.Hour
	maxLinks        = 50
	maxSubs         = 20
)

// devSecretKey is the published development key (scaffold.json dev value).
const devSecretKey = "calendar-dev-key-not-a-secret-32"

type Service struct {
	d            *app.Deps
	secretKey    []byte        // AES-256 key for subscription URLs; nil = subscriptions off
	allowPrivate bool          // development: subscription URLs may point at this machine
	subInterval  time.Duration // how often each subscription is re-fetched
	subMaxBytes  int64
	fetcher      *http.Client
}

func New(d *app.Deps) *Service {
	s := &Service{
		d:            d,
		subInterval:  config.Duration("CALENDAR_SUBSCRIPTION_INTERVAL", 30*time.Minute),
		subMaxBytes:  int64(config.Int("CALENDAR_SUBSCRIPTION_MAX_MB", 5)) << 20,
		allowPrivate: config.Bool("CALENDAR_ALLOW_PRIVATE_URLS", false),
	}
	if s.subInterval < 5*time.Minute || s.subInterval > 24*time.Hour {
		config.Problem("CALENDAR_SUBSCRIPTION_INTERVAL must be between 5m and 24h (got %s)", s.subInterval)
	}
	if s.subMaxBytes < 1<<20 || s.subMaxBytes > 50<<20 {
		config.Problem("CALENDAR_SUBSCRIPTION_MAX_MB must be between 1 and 50")
	}
	if s.allowPrivate && !d.Cfg.IsDevelopment() {
		config.Problem("CALENDAR_ALLOW_PRIVATE_URLS is only allowed with APP_ENV=development")
		s.allowPrivate = false
	}
	if raw := config.RequiredOutsideDev("CALENDAR_SECRET_KEY", ""); raw != "" {
		key, err := base64.StdEncoding.DecodeString(raw)
		switch {
		case err != nil || len(key) != 32:
			config.Problem("CALENDAR_SECRET_KEY must be 32 random bytes, base64-encoded (openssl rand -base64 32)")
		case string(key) == devSecretKey && !d.Cfg.IsDevelopment():
			config.Problem("CALENDAR_SECRET_KEY is the development value; generate one with openssl rand -base64 32")
		default:
			s.secretKey = key
		}
	}
	s.fetcher = newSafeClient(15*time.Second, func() bool { return s.allowPrivate })

	// Secret tokens travel in these paths: keep them out of request logs.
	httpx.RedactPathPrefix("/v1/calendar/feed/", "/v1/calendar/manage/", "/v1/calendar/rsvp/")

	// An agent reading the inbox sees event notifications only while it may
	// read the event (bookings follow the feature's level).
	eventTarget := notifications.DataTarget(KindEvent, "event_id")
	notifications.RegisterKind(notifications.Kind{ID: NotifyInvite, Label: "Calendar invitations", Push: true, AgentTarget: eventTarget})
	notifications.RegisterKind(notifications.Kind{ID: NotifyUpdated, Label: "Changes to calendar events", Push: true, AgentTarget: eventTarget})
	notifications.RegisterKind(notifications.Kind{ID: NotifyCancelled, Label: "Cancelled calendar events", Push: true, AgentTarget: eventTarget})
	notifications.RegisterKind(notifications.Kind{ID: NotifyResponse, Label: "Replies to your invitations", Push: false, AgentTarget: eventTarget})
	notifications.RegisterKind(notifications.Kind{ID: NotifyReminder, Label: "Calendar reminders", Push: true, AgentTarget: eventTarget})
	notifications.RegisterKind(notifications.Kind{ID: NotifyBooking, Label: "Bookings", Push: true})
	return s
}

func (s *Service) Name() string { return "calendar" }

// Public routes. Booking routes accept an optional bearer token: internal
// links need a signed-in org member, and a signed-in booker gets the event
// on their own calendar. Feed, manage and RSVP routes are opened by their
// URL token alone. None takes an AI agent's credential.
func (s *Service) MountPublic(r chi.Router) {
	r = r.With(refuseAgentCredential)
	opt := r.With(s.optionalAuth)
	byIP := func(n int, window time.Duration) func(http.Handler) http.Handler {
		return httprate.LimitBy(n, window, httpx.KeyByClientIP, httprate.WithLimitHandler(httpx.RateLimitedHandler))
	}
	bySlug := httprate.LimitBy(60, time.Hour, func(r *http.Request) (string, error) {
		return "calendar-link:" + strings.ToLower(chi.URLParam(r, "slug")), nil
	}, httprate.WithLimitHandler(httpx.RateLimitedHandler))

	r.With(byIP(120, time.Minute)).Get("/calendar/feed/{token}.ics", httpx.Handler(s.feedICS))

	opt.With(byIP(60, time.Minute)).Get("/calendar/book/{slug}", httpx.Handler(s.publicLink))
	opt.With(byIP(60, time.Minute)).Get("/calendar/book/{slug}/slots", httpx.Handler(s.publicSlots))
	opt.With(byIP(10, time.Hour), bySlug).Post("/calendar/book/{slug}", httpx.Handler(s.bookHandler))

	r.With(byIP(60, time.Minute)).Get("/calendar/manage/{token}", httpx.Handler(s.manageGet))
	r.With(byIP(60, time.Minute)).Get("/calendar/manage/{token}.ics", httpx.Handler(s.manageICS))
	r.With(byIP(60, time.Minute)).Get("/calendar/manage/{token}/slots", httpx.Handler(s.manageSlots))
	r.With(byIP(20, time.Hour)).Post("/calendar/manage/{token}/cancel", httpx.Handler(s.manageCancel))
	r.With(byIP(20, time.Hour)).Post("/calendar/manage/{token}/reschedule", httpx.Handler(s.manageReschedule))

	r.With(byIP(60, time.Minute)).Get("/calendar/rsvp/{token}", httpx.Handler(s.rsvpTokenGet))
	r.With(byIP(30, time.Hour)).Post("/calendar/rsvp/{token}", httpx.Handler(s.rsvpTokenPost))
}

func (s *Service) MountUser(r chi.Router) {
	write := r.With(auth.RequireActive)

	r.Get("/calendar/settings", httpx.Handler(s.getSettings))
	write.With(auth.RateLimit(60, time.Hour)).Put("/calendar/settings", httpx.Handler(s.putSettings))

	r.Get("/calendar/events", httpx.Handler(s.listHandler))
	write.With(auth.RateLimit(120, time.Hour)).Post("/calendar/events", httpx.Handler(s.createHandler))
	r.Get("/calendar/events/{id}", httpx.Handler(s.getHandler))
	r.Get("/calendar/events/{id}.ics", httpx.Handler(s.eventICS))
	write.With(auth.RateLimit(300, time.Hour)).Patch("/calendar/events/{id}", httpx.Handler(s.updateHandler))
	write.With(auth.RateLimit(300, time.Hour)).Delete("/calendar/events/{id}", httpx.Handler(s.deleteHandler))
	write.With(auth.RateLimit(60, time.Minute)).Put("/calendar/events/{id}/rsvp", httpx.Handler(s.rsvpHandler))

	r.With(auth.RateLimit(120, time.Minute)).Get("/calendar/people", httpx.Handler(s.peopleHandler))
	r.With(auth.RateLimit(120, time.Minute)).Get("/calendar/freebusy", httpx.Handler(s.freeBusyHandler))
	r.With(auth.RateLimit(60, time.Minute)).Get("/calendar/find-time", httpx.Handler(s.findTimeHandler))

	r.Get("/calendar/subscriptions", httpx.Handler(s.listSubscriptions))
	write.With(auth.RateLimit(20, time.Hour)).Post("/calendar/subscriptions", httpx.Handler(s.createSubscription))
	write.Patch("/calendar/subscriptions/{id}", httpx.Handler(s.updateSubscription))
	r.Delete("/calendar/subscriptions/{id}", httpx.Handler(s.deleteSubscription))
	write.With(auth.RateLimit(12, time.Hour)).Post("/calendar/subscriptions/{id}/refresh", httpx.Handler(s.refreshSubscription))

	r.Get("/calendar/feed", httpx.Handler(s.getFeed))
	r.With(auth.RateLimit(10, time.Hour)).Post("/calendar/feed", httpx.Handler(s.rotateFeed))
	r.Delete("/calendar/feed", httpx.Handler(s.deleteFeed))

	r.Get("/calendar/links", httpx.Handler(s.listLinks))
	write.With(auth.RateLimit(30, time.Hour)).Post("/calendar/links", httpx.Handler(s.createLink))
	r.With(auth.RateLimit(240, time.Hour)).Post("/calendar/links/preview", httpx.Handler(s.previewLink))
	r.Get("/calendar/links/{id}", httpx.Handler(s.getLink))
	write.With(auth.RateLimit(240, time.Hour)).Patch("/calendar/links/{id}", httpx.Handler(s.updateLink))
	r.Delete("/calendar/links/{id}", httpx.Handler(s.deleteLink))

	r.Get("/calendar/bookings", httpx.Handler(s.listBookings))
	r.Get("/calendar/bookings/{id}", httpx.Handler(s.getBooking))
	write.With(auth.RateLimit(60, time.Hour)).Post("/calendar/bookings/{id}/cancel", httpx.Handler(s.hostCancelBooking))
}

// Start runs reminders, subscription refreshes and the outbox.
func (s *Service) Start(ctx context.Context) {
	go jobs.Every(ctx, s.d.Pool, "calendar.reminders", time.Minute, s.sendReminders)
	go jobs.Every(ctx, s.d.Pool, "calendar.subscriptions", time.Minute, s.refreshDue)
	go jobs.Every(ctx, s.d.Pool, "calendar.outbox", 15*time.Second, s.drainOutbox)
	go s.outboxLoop(ctx)
	go jobs.Every(ctx, s.d.Pool, "calendar.prune", time.Hour, s.prune)
}

// errAgentCredential answers a public calendar route called with an AI
// agent's credential: agents reach calendars only through their tools.
var errAgentCredential = httpx.NewError(http.StatusUnauthorized, "agent_credential",
	"This credential is for AI agents. It works only at the agent endpoint.")

// refuseAgentCredential stops agent credentials (agt_…) at the public
// routes, whose tokens would otherwise open a calendar outside the agent's
// scope.
func refuseAgentCredential(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok && auth.IsAgentCredential(strings.TrimSpace(tok)) {
			httpx.Fail(w, r, errAgentCredential)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// optionalAuth resolves the caller when a bearer token is present (and
// refuses a bad one); without one the request continues signed out.
func (s *Service) optionalAuth(next http.Handler) http.Handler {
	authed := s.d.Auth.Middleware(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			next.ServeHTTP(w, r)
			return
		}
		authed.ServeHTTP(w, r)
	})
}

// changed tells each user's clients to refetch a part of their calendar.
func (s *Service) changed(ctx context.Context, scope string, users ...uuid.UUID) {
	seen := map[uuid.UUID]bool{}
	for _, u := range users {
		if u == uuid.Nil || seen[u] {
			continue
		}
		seen[u] = true
		if err := s.d.Broker.Publish(ctx, realtime.UserTopic(u), EventChanged, map[string]string{"scope": scope}); err != nil {
			slog.WarnContext(ctx, "calendar: realtime publish failed", "err", err)
		}
	}
}

// webURL is PUBLIC_WEB_URL + path, or "" when the web URL isn't set.
func (s *Service) webURL(path string) string {
	base := strings.TrimRight(s.d.Cfg.PublicWebURL, "/")
	if base == "" {
		return ""
	}
	return base + path
}

func (s *Service) apiURL(path string) string {
	return strings.TrimRight(s.d.Cfg.PublicAPIURL, "/") + path
}

// eventLink is the app path of an event (web and mobile).
func eventLink(id uuid.UUID) string { return "/calendar/event/" + id.String() }
