// Package calls is video meetings and instant calls on an SFU (LiveKit by
// default, behind the Media interface): meetings with a stable Meet-style
// code and link, access by organization, invitation or link (guests knock
// and wait in a lobby), host controls, instant calls that ring people's
// devices, attendance history from SFU webhooks, and the seams calendars and
// channels use to create meetings and start calls.
//
// Rules that hold everywhere in this package:
//   - Who may join is decided by one function, accessFor (people) and
//     guestAccess (guests). REST routes, the realtime authorizer, agent tools
//     and search all go through it.
//   - The SFU only ever sees room names (meeting ids) and identities
//     ("u:<user id>", "g:<lobby id>"); it never decides access.
//   - Ids a client sends are scoped in SQL to a meeting the caller may act on.
//   - Notifications and realtime events go out after the transaction commits
//     and never fail the request.
package calls

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/httprate"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/jobs"
	"scaffoldapp/backend/internal/notifications"
	"scaffoldapp/backend/internal/notify"
	"scaffoldapp/backend/internal/orgs"
	"scaffoldapp/backend/internal/realtime"
)

// Access modes.
const (
	AccessOrg     = "org"     // members of the meeting's organization and invitees
	AccessInvited = "invited" // invitees; other signed-in people may ask to join
	AccessOpen    = "open"    // anyone with the link; outsiders and guests knock while the lobby is on
)

// Meeting kinds.
const (
	KindMeeting = "meeting"
	KindCall    = "call" // an instant call that rang people
)

// Roles a person has in a meeting (Meeting.Role, token metadata).
const (
	RoleHost    = "host"
	RoleCohost  = "cohost"
	RoleInvitee = "invitee"
	RoleMember  = "member" // allowed without an invitation (org member, admitted, open link)
	RoleGuest   = "guest"
)

// Notification kinds.
const (
	KindNotifyInvite = "calls.invite" // you were invited to a scheduled meeting
	KindNotifyMissed = "calls.missed" // you missed an instant call
)

// Realtime events. Meeting topic calls:<id>: meeting_updated, ring_updated.
// User topics: ring, ring_ended (callees); lobby (hosts and co-hosts);
// lobby_decided (the person who knocked).
const (
	EventMeetingUpdated = "calls.meeting_updated"
	EventRingUpdated    = "calls.ring_updated"
	EventRing           = "calls.ring"
	EventRingEnded      = "calls.ring_ended"
	EventLobby          = "calls.lobby"
	EventLobbyDecided   = "calls.lobby_decided"
)

const (
	topicKind = "calls"

	maxRingTargets   = 16  // people one instant call rings
	maxInvitees      = 200 // invitees on one meeting
	maxTitleChars    = 200
	maxGuestName     = 60
	lobbyWaitTTL     = 15 * time.Minute // a knock nobody answers expires
	lobbyLease       = 45 * time.Second // someone waiting who stops checking in (page closed) is set aside
	lobbyCheckIn     = 10 * time.Second // how often hosts hear who left the lobby
	admittedTTL      = 12 * time.Hour   // an admitted person may reconnect this long
	emptyRoomTimeout = 2 * time.Minute  // a room nobody joined closes
	departureTimeout = 30 * time.Second
	reconcileAfter   = 2 * time.Minute // sessions this old are checked against the SFU
)

// The lobby windows as SQL second literals, so every query uses the same numbers.
var (
	lobbyLeaseSecs = strconv.Itoa(int(lobbyLease / time.Second))
	lobbyWaitSecs  = strconv.Itoa(int(lobbyWaitTTL / time.Second))
)

type settings struct {
	tokenTTL        time.Duration
	ringTimeout     time.Duration
	maxParticipants int
}

type Service struct {
	d     *app.Deps
	cfg   settings
	media Media // nil when unconfigured: call routes answer 503
}

var (
	current       atomic.Pointer[Service] // the module, for the exported seams
	mediaOverride Media
)

// SetMedia installs another SFU implementation. Call it from an init() in
// stitching code (e.g. internal/api/feature_calls.go) before the API is
// built; it replaces LiveKit and the LIVEKIT_* settings are ignored.
func SetMedia(m Media) { mediaOverride = m }

func New(d *app.Deps) *Service {
	s := &Service{d: d, cfg: settings{
		tokenTTL:        between("CALLS_TOKEN_TTL", config.Duration("CALLS_TOKEN_TTL", 10*time.Minute), time.Minute, 6*time.Hour),
		ringTimeout:     between("CALLS_RING_TIMEOUT", config.Duration("CALLS_RING_TIMEOUT", 45*time.Second), 10*time.Second, 5*time.Minute),
		maxParticipants: config.Int("CALLS_MAX_PARTICIPANTS", 100),
	}}
	if s.cfg.maxParticipants < 2 || s.cfg.maxParticipants > 1000 {
		config.Problem("CALLS_MAX_PARTICIPANTS must be between 2 and 1000 (got %d)", s.cfg.maxParticipants)
		s.cfg.maxParticipants = 100
	}
	if mediaOverride != nil {
		s.media = mediaOverride
	} else {
		s.media = liveKitFromEnv(d.Cfg.IsDevelopment())
	}
	// An agent reading the inbox sees these only while it may read the meeting.
	meetingTarget := notifications.DataTarget(AgentKindMeeting, "meeting_id")
	notifications.RegisterKind(notifications.Kind{ID: KindNotifyInvite, Label: "Meeting invitations", Push: true, Email: true, AgentTarget: meetingTarget})
	notifications.RegisterKind(notifications.Kind{ID: KindNotifyMissed, Label: "Missed calls", Push: true, AgentTarget: meetingTarget})
	orgs.OnMemberRemoved(s.memberRemoved)
	current.Store(s)
	slog.Info("calls: media", "provider", s.mediaName(), "configured", s.media != nil)
	return s
}

// liveKitFromEnv reads LIVEKIT_*. All empty = unconfigured (routes answer
// 503); a partial or unsafe setting fails the boot.
func liveKitFromEnv(development bool) Media {
	url := config.String("LIVEKIT_URL", "")
	key := config.String("LIVEKIT_API_KEY", "")
	secret := config.String("LIVEKIT_API_SECRET", "")
	apiURL := config.String("LIVEKIT_API_URL", "")
	if url == "" && key == "" && secret == "" {
		return nil
	}
	ok := true
	if url == "" || key == "" || secret == "" {
		config.Problem("LIVEKIT_URL, LIVEKIT_API_KEY and LIVEKIT_API_SECRET must be set together (calls)")
		ok = false
	}
	if url != "" && !strings.HasPrefix(url, "ws://") && !strings.HasPrefix(url, "wss://") {
		config.Problem("LIVEKIT_URL must start with wss:// (or ws:// in development), got %q", url)
		ok = false
	}
	if apiURL == "" {
		apiURL = liveKitAPIURL(url)
	} else if !strings.HasPrefix(apiURL, "http://") && !strings.HasPrefix(apiURL, "https://") {
		config.Problem("LIVEKIT_API_URL must be an http(s) URL, got %q", apiURL)
		ok = false
	}
	if !development {
		if strings.HasPrefix(url, "ws://") || config.IsLocalURL(url) {
			config.Problem("LIVEKIT_URL must be a public wss:// URL outside development (got %q)", url)
			ok = false
		}
		if len(secret) < 32 {
			config.Problem("LIVEKIT_API_SECRET must be at least 32 characters outside development")
			ok = false
		}
	}
	if !ok {
		return nil
	}
	return newLiveKit(url, apiURL, key, secret)
}

func between(name string, v, lo, hi time.Duration) time.Duration {
	if v < lo || v > hi {
		config.Problem("%s must be between %s and %s (got %s)", name, lo, hi, v)
		if v < lo {
			return lo
		}
		return hi
	}
	return v
}

func (s *Service) Name() string { return "calls" }

func (s *Service) mediaName() string {
	if s.media == nil {
		return "none"
	}
	return s.media.Name()
}

// Per-IP limits for the anonymous guest routes.
var (
	previewLimit = httprate.LimitBy(60, time.Minute, httpx.KeyByClientIP, httprate.WithLimitHandler(httpx.RateLimitedHandler))
	knockLimit   = httprate.LimitBy(10, time.Minute, httpx.KeyByClientIP, httprate.WithLimitHandler(httpx.RateLimitedHandler))
	pollLimit    = httprate.LimitBy(120, time.Minute, httpx.KeyByClientIP, httprate.WithLimitHandler(httpx.RateLimitedHandler))
	guestLimit   = httprate.LimitBy(30, time.Minute, httpx.KeyByClientIP, httprate.WithLimitHandler(httpx.RateLimitedHandler))
)

func (s *Service) MountPublic(r chi.Router) {
	h := httpx.Handler
	r.With(previewLimit).Get("/calls/public/meetings/{code}", h(s.publicMeeting))
	r.With(knockLimit).Post("/calls/public/meetings/{code}/knock", h(s.guestKnock))
	r.With(pollLimit).Post("/calls/public/lobby/{id}/status", h(s.guestStatus))
	r.With(pollLimit).Post("/calls/public/lobby/{id}/cancel", h(s.guestCancel))
	r.With(pollLimit).Post("/calls/public/lobby/{id}/leave", h(s.guestLeave))
	r.With(guestLimit).Post("/calls/public/lobby/{id}/token", h(s.guestToken))
	r.With(guestLimit).Post("/calls/public/lobby/{id}/hand", h(s.guestHand))
}

func (s *Service) MountUser(r chi.Router) {
	h := httpx.Handler
	r.Get("/calls/history", h(s.listHistory))
	r.Get("/calls/history/{id}", h(s.getHistory))
	r.Get("/calls/upcoming", h(s.listUpcoming))
	r.Get("/calls/links", h(s.listLinks))
	r.Get("/calls/rings", h(s.listRings))
	r.With(auth.RateLimit(60, time.Minute)).Get("/calls/people", h(s.people))
	r.Get("/calls/meetings/{id}", h(s.getMeeting))
	r.Get("/calls/meetings/{id}/lobby", h(s.listLobby))
	r.Get("/calls/meetings/{id}/rings", h(s.listMeetingRings))
	r.Get("/calls/lobby/{id}", h(s.getMyKnock))
	// The callee's own state: allowed while suspended.
	r.Delete("/calls/lobby/{id}", h(s.cancelMyKnock))
	r.Post("/calls/rings/{id}/decline", h(s.declineRing))
	r.Post("/calls/rings/{id}/accept", h(s.acceptRing))

	w := r.With(auth.RequireActive)
	w.With(auth.RateLimit(30, time.Hour)).Post("/calls/meetings", h(s.createMeeting))
	w.With(auth.RateLimit(60, time.Minute)).Patch("/calls/meetings/{id}", h(s.updateMeeting))
	w.Delete("/calls/meetings/{id}", h(s.cancelMeeting))
	w.With(auth.RateLimit(30, time.Minute)).Post("/calls/meetings/{id}/invitees", h(s.addInvitees))
	w.Patch("/calls/meetings/{id}/invitees/{userID}", h(s.setInviteeRole))
	w.Delete("/calls/meetings/{id}/invitees/{userID}", h(s.removeInvitee))
	w.With(auth.RateLimit(60, time.Minute)).Post("/calls/meetings/{id}/join", h(s.join))
	w.With(auth.RateLimit(10, time.Minute)).Post("/calls/meetings/{id}/knock", h(s.knock))
	w.With(auth.RateLimit(120, time.Minute)).Post("/calls/meetings/{id}/lobby/admit", h(s.admit))
	w.With(auth.RateLimit(120, time.Minute)).Post("/calls/meetings/{id}/lobby/deny", h(s.deny))
	w.With(auth.RateLimit(60, time.Minute)).Post("/calls/meetings/{id}/mute", h(s.mute))
	w.With(auth.RateLimit(60, time.Minute)).Post("/calls/meetings/{id}/remove", h(s.remove))
	w.With(auth.RateLimit(30, time.Minute)).Post("/calls/meetings/{id}/end", h(s.end))
	w.With(auth.RateLimit(30, time.Minute)).Post("/calls/meetings/{id}/hand", h(s.hand))
	w.With(auth.RateLimit(30, 10*time.Minute)).Post("/calls/meetings/{id}/ring", h(s.ringAgain))
	r.Delete("/calls/meetings/{id}/ring", h(s.stopRinging))
	w.With(auth.RateLimit(30, time.Hour)).Post("/calls/calls", h(s.startCall))
}

func (s *Service) MountWebhooks(r chi.Router) {
	r.Post("/livekit", s.webhook)
}

// Start runs ring timeouts (missed calls), the lobby sweep and the SFU reconciliation.
func (s *Service) Start(ctx context.Context) {
	go jobs.Every(ctx, s.d.Pool, "calls.rings", 5*time.Second, s.expireRings)
	go jobs.Every(ctx, s.d.Pool, "calls.lobby", lobbyCheckIn, s.sweepLobby)
	go jobs.Every(ctx, s.d.Pool, "calls.reconcile", time.Minute, s.reconcile)
}

// AuthorizeTopic admits people who may join a meeting to calls:<meeting id>.
func (s *Service) AuthorizeTopic(ctx context.Context, userID uuid.UUID, _ bool, topic string) (handled, allowed bool) {
	rest, ok := strings.CutPrefix(topic, topicKind+":")
	if !ok {
		return false, false
	}
	id, err := uuid.Parse(rest)
	if err != nil {
		return true, false
	}
	m, err := loadMeeting(ctx, s.d.Pool, id)
	if err != nil {
		return true, false
	}
	a, err := s.accessFor(ctx, s.d.Pool, m, userID)
	return true, err == nil && a.canJoin()
}

// memberRemoved runs when someone leaves an org (orgs.OnMemberRemoved): the
// org's meetings they could join as a member stop sending them events
// (the topic is re-checked, so invitations they hold still count).
func (s *Service) memberRemoved(ctx context.Context, orgID, userID uuid.UUID) {
	rows, err := s.d.Pool.Query(ctx, `
		SELECT id FROM calls_meetings
		WHERE org_id = $1 AND canceled_at IS NULL AND (ends_at IS NULL OR ends_at > now() - interval '1 day')
		ORDER BY created_at DESC LIMIT 5000`, orgID)
	if err != nil {
		slog.WarnContext(ctx, "calls: list meetings for a leaver", "org_id", orgID, "err", err)
		return
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		slog.WarnContext(ctx, "calls: list meetings for a leaver", "org_id", orgID, "err", err)
		return
	}
	for _, id := range ids {
		s.revokeMeeting(ctx, id, userID)
	}
}

// ---- side effects ------------------------------------------------------------

func (s *Service) publish(ctx context.Context, topic, event string, data any) {
	if err := s.d.Broker.Publish(ctx, topic, event, data); err != nil {
		slog.WarnContext(ctx, "calls: realtime publish failed", "event", event, "err", err)
	}
}

func (s *Service) publishUser(ctx context.Context, userID uuid.UUID, event string, data any) {
	s.publish(ctx, realtime.UserTopic(userID), event, data)
}

func (s *Service) send(ctx context.Context, n notify.Notification) {
	if err := s.d.Notify.Send(ctx, n); err != nil {
		slog.WarnContext(ctx, "calls: notify failed", "kind", n.Kind, "user_id", n.UserID, "err", err)
	}
}

func meetingTopic(id uuid.UUID) string { return realtime.Topic(topicKind, id) }

// joinURL is the public link for a meeting code.
func (s *Service) joinURL(code string) string {
	base := s.d.Cfg.PublicWebURL
	if base == "" && len(s.d.Cfg.CORSOrigins) > 0 {
		base = s.d.Cfg.CORSOrigins[0] // development without PUBLIC_WEB_URL
	}
	return strings.TrimRight(base, "/") + "/meet/" + code
}

// requireMedia answers 503 when no SFU is configured.
func (s *Service) requireMedia() error {
	if s.media == nil {
		return errUnconfigured
	}
	return nil
}

// detached is a context for SFU calls and side effects that must finish
// even if the client goes away.
func detached(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
}

type cursor struct {
	T  time.Time `json:"t"`
	ID uuid.UUID `json:"id"`
}

// ---- errors ----------------------------------------------------------------------

var (
	errUnconfigured     = httpx.NewError(http.StatusServiceUnavailable, "calls_unconfigured", "Calls aren't set up on this server yet.")
	errMediaUnavailable = httpx.NewError(http.StatusServiceUnavailable, "calls_unavailable", "Couldn't reach the call server. Try again in a moment.")
	errMeetingNotFound  = httpx.NotFound("Meeting not found. Check the code or link.")
	errMeetingCanceled  = httpx.NewError(http.StatusGone, "calls_canceled", "This meeting was canceled.")
	errNotHost          = httpx.Forbidden("Only the meeting's hosts can do that.")
	errNotOwner         = httpx.Forbidden("Only the person who set up the meeting can do that.")
	errNotLive          = httpx.NewError(http.StatusConflict, "calls_not_live", "Nobody is in this call right now.")
	errLobbyNotFound    = httpx.NotFound("That request to join isn't there anymore.")
	errRingNotFound     = httpx.NotFound("That call isn't ringing anymore.")
	errCallHistoryGone  = httpx.NotFound("Call not found.")
)
