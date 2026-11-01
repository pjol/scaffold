// Package channels is a Slack-style workspace per organization: public and
// private channels (chat or forum layout), direct and group messages,
// threads, mentions (@person, @here, @channel, @group), reactions, pins,
// saved items, attachments, read pointers with unread and mention counts,
// per-channel notification preferences, typing, search, and the moderation
// hooks (blocks, the pre-post filter, reports, staff removal).
//
// Rules that hold everywhere in this package:
//   - Every read and write first loads the caller's access (access.go): an
//     org member, and a channel member unless the channel is public. Anything
//     else answers "not found", so ids can't be probed. A person who left the
//     org loses access at once; their memberships are pruned within a minute.
//   - Ids a client sends are scoped in SQL to the channel the caller can see
//     (thread roots, cursors, attachments, members).
//   - Who may create, archive, edit and @channel is decided in policy.go and
//     nowhere else; the app can replace it with SetPermissionCheck.
//   - Ordering is (created_at, id) for timelines and threads, (activity_at,
//     id) for forum topics; pagination is keyset.
//   - Notifications and realtime events go out after the transaction commits
//     and never fail the request.
package channels

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/jobs"
	"scaffoldapp/backend/internal/moderation"
	"scaffoldapp/backend/internal/notifications"
	"scaffoldapp/backend/internal/notify"
	"scaffoldapp/backend/internal/orgs"
	"scaffoldapp/backend/internal/presence"
	"scaffoldapp/backend/internal/storage"
)

const (
	// PurposeAttachment is the private upload purpose for message files.
	PurposeAttachment = "channel_attachment"

	topicKind  = "channels"         // realtime topics channels:<channel id>
	targetKind = "channels.message" // moderation target kind

	// Notification kinds.
	KindMention = "channels.mention" // mentions and direct messages
	KindMessage = "channels.message" // every message, in channels set to "all"
	KindThread  = "channels.thread"  // replies in threads you follow

	// Realtime events. Channel topic: message, message_updated, typing,
	// channel (details, membership). User topic: sidebar, thread.
	EventMessage        = "channels.message"
	EventMessageUpdated = "channels.message_updated"
	EventTyping         = "channels.typing"
	EventChannel        = "channels.channel"
	EventSidebar        = "channels.sidebar"
	EventThread         = "channels.thread"

	maxAttachments      = 10
	maxReactionsPerUser = 20
	maxGroupDM          = 9 // people in a group DM, the caller included
	attachmentURLTTL    = time.Hour
)

type Service struct {
	d   *app.Deps
	cfg settings

	// deletedMembership holds, between an account's deletion transaction and
	// its AccountDeleted hook, the channels whose topics to revoke.
	deletedMembership sync.Map // uuid.UUID → []uuid.UUID
}

type settings struct {
	maxChars        int
	editWindow      time.Duration // 0 = no limit
	perMinute       int
	attachmentMax   int64
	defaultChannels []string
	mentionAllMax   int
}

// svc backs the package-level functions other features call (PostBotMessage).
var svc atomic.Pointer[Service]

func New(d *app.Deps) *Service {
	s := &Service{d: d, cfg: settings{
		maxChars:        atLeast("CHANNELS_MESSAGE_MAX_CHARS", config.Int("CHANNELS_MESSAGE_MAX_CHARS", 8000), 100),
		editWindow:      config.Duration("CHANNELS_EDIT_WINDOW", 0),
		perMinute:       atLeast("CHANNELS_MESSAGES_PER_MINUTE", config.Int("CHANNELS_MESSAGES_PER_MINUTE", 60), 1),
		attachmentMax:   int64(atLeast("CHANNELS_ATTACHMENT_MAX_MB", config.Int("CHANNELS_ATTACHMENT_MAX_MB", 50), 1)) << 20,
		defaultChannels: config.List("CHANNELS_DEFAULT_CHANNELS", "general"),
		mentionAllMax:   atLeast("CHANNELS_MENTION_ALL_MAX_MEMBERS", config.Int("CHANNELS_MENTION_ALL_MAX_MEMBERS", 50), 2),
	}}
	if s.cfg.editWindow < 0 {
		config.Problem("CHANNELS_EDIT_WINDOW must be 0 (no limit) or a positive duration (got %s)", s.cfg.editWindow)
		s.cfg.editWindow = 0
	}
	for i, name := range s.cfg.defaultChannels {
		clean, err := cleanName(name)
		if err != nil {
			config.Problem("CHANNELS_DEFAULT_CHANNELS: %q isn't a valid channel name (lowercase letters, digits, - and _)", name)
			continue
		}
		s.cfg.defaultChannels[i] = clean
	}
	mentionAllMax.Store(int64(s.cfg.mentionAllMax))
	storage.RegisterPurpose(storage.Purpose{Name: PurposeAttachment, Types: attachmentTypes, MaxBytes: s.cfg.attachmentMax})
	notifications.RegisterKind(notifications.Kind{ID: KindMention, Label: "Mentions and direct messages", Push: true, AgentTarget: notifications.DataTarget(agentKind, "channel_id")})
	notifications.RegisterKind(notifications.Kind{ID: KindMessage, Label: "Channel messages", Push: true, AgentTarget: notifications.DataTarget(agentKind, "channel_id")})
	notifications.RegisterKind(notifications.Kind{ID: KindThread, Label: "Thread replies", Push: true, AgentTarget: notifications.DataTarget(agentKind, "channel_id")})
	moderation.RegisterTarget(targetKind, messageTarget{s})
	orgs.OnMemberRemoved(s.memberRemoved)
	// Do Not Disturb (presence) holds push and email for every notification,
	// channels' and other features' alike; the inbox still gets them.
	notifications.AddDeliveryFilter("presence.dnd", func(ctx context.Context, n notify.Notification) (notifications.Hold, error) {
		on, err := presence.IsDND(ctx, d.Pool, n.UserID)
		return notifications.Hold{Push: on, Email: on}, err
	})
	svc.Store(s)
	return s
}

func atLeast(name string, v, minimum int) int {
	if v < minimum {
		config.Problem("%s must be at least %d (got %d)", name, minimum, v)
		return minimum
	}
	return v
}

func (s *Service) Name() string { return "channels" }

func (s *Service) MountUser(r chi.Router) {
	h := httpx.Handler
	member := orgs.Require(orgs.RoleMember)
	w := r.With(auth.RequireActive)

	r.Get("/channels/config", h(s.config))
	r.Get("/channels/unread", h(s.unreadTotals))

	// Workspace (org-scoped): sidebar, browse, people, groups, threads, later, search.
	r.With(member).Get("/channels/orgs/{orgID}/workspace", h(s.workspace))
	r.With(member).Get("/channels/orgs/{orgID}/browse", h(s.browse))
	r.With(member, auth.RateLimit(120, time.Minute)).Get("/channels/orgs/{orgID}/people", h(s.people))
	r.With(member, auth.RateLimit(120, time.Minute)).Get("/channels/orgs/{orgID}/groups", h(s.groups))
	r.With(member).Get("/channels/orgs/{orgID}/threads", h(s.threads))
	r.With(member).Get("/channels/orgs/{orgID}/later", h(s.later))
	r.With(member, auth.RateLimit(60, time.Minute)).Get("/channels/orgs/{orgID}/search", h(s.search))
	w.With(member, auth.RateLimit(20, time.Hour)).Post("/channels/orgs/{orgID}/channels", h(s.createChannel))
	w.With(member, auth.RateLimit(60, time.Hour)).Post("/channels/orgs/{orgID}/dms", h(s.openDM))

	// One channel.
	r.Get("/channels/channels/{id}", h(s.getChannel))
	r.Get("/channels/channels/{id}/members", h(s.listMembers))
	r.Get("/channels/channels/{id}/messages", h(s.listMessages))
	r.Get("/channels/channels/{id}/topics", h(s.listTopics))
	r.Get("/channels/channels/{id}/pins", h(s.listPins))
	r.Get("/channels/channels/{id}/files", h(s.listFiles))
	// The reader's own state: allowed while suspended.
	r.Post("/channels/channels/{id}/read", h(s.markRead))
	r.Put("/channels/channels/{id}/prefs", h(s.setPrefs))
	r.Delete("/channels/channels/{id}/membership", h(s.leave))
	// Anything other people see: active accounts only.
	w.With(auth.RateLimit(30, time.Minute)).Patch("/channels/channels/{id}", h(s.updateChannel))
	w.With(auth.RateLimit(30, time.Minute)).Post("/channels/channels/{id}/archive", h(s.archive))
	w.With(auth.RateLimit(30, time.Minute)).Delete("/channels/channels/{id}/archive", h(s.unarchive))
	w.With(auth.RateLimit(60, time.Minute)).Post("/channels/channels/{id}/join", h(s.join))
	w.With(auth.RateLimit(30, time.Minute)).Post("/channels/channels/{id}/members", h(s.addMembers))
	w.With(auth.RateLimit(60, time.Minute)).Delete("/channels/channels/{id}/members/{userID}", h(s.removeMember))
	w.With(auth.RateLimit(s.cfg.perMinute, time.Minute)).Post("/channels/channels/{id}/messages", h(s.sendMessage))
	w.With(auth.RateLimit(40, time.Minute)).Post("/channels/channels/{id}/typing", h(s.typing))
	w.With(auth.RateLimit(10, time.Minute)).Post("/channels/channels/{id}/call", h(s.startCall))

	// One message.
	r.Get("/channels/messages/{id}", h(s.getMessage))
	r.Get("/channels/messages/{id}/thread", h(s.getThread))
	r.Post("/channels/messages/{id}/thread/read", h(s.markThreadRead))
	r.Put("/channels/messages/{id}/follow", h(s.follow))
	r.Delete("/channels/messages/{id}/follow", h(s.unfollow))
	r.With(auth.RateLimit(120, time.Minute)).Put("/channels/messages/{id}/later", h(s.saveLater))
	r.Delete("/channels/messages/{id}/later", h(s.unsaveLater))
	w.With(auth.RateLimit(30, time.Minute)).Patch("/channels/messages/{id}", h(s.editMessage))
	w.With(auth.RateLimit(60, time.Minute)).Delete("/channels/messages/{id}", h(s.deleteMessage))
	w.With(auth.RateLimit(60, time.Minute)).Post("/channels/messages/{id}/reactions", h(s.addReaction))
	w.With(auth.RateLimit(60, time.Minute)).Delete("/channels/messages/{id}/reactions", h(s.removeReaction))
	w.With(auth.RateLimit(30, time.Minute)).Put("/channels/messages/{id}/pin", h(s.pin))
	w.With(auth.RateLimit(30, time.Minute)).Delete("/channels/messages/{id}/pin", h(s.unpin))
	r.Get("/channels/attachments/{id}", h(s.attachmentURL))
}

func (s *Service) MountAdmin(r chi.Router) {
	r.Get("/channels/channels/{id}", httpx.Handler(s.adminChannel))
	r.Delete("/channels/messages/{id}", httpx.Handler(s.adminRemoveMessage))
}

// Start runs the storage clean-up for deleted rows and the backstop prune of
// memberships of people who left an org (memberRemoved does it at once).
func (s *Service) Start(ctx context.Context) {
	go jobs.Every(ctx, s.d.Pool, "channels.delete_objects", time.Minute, s.deleteQueuedObjects)
	go jobs.Every(ctx, s.d.Pool, "channels.prune_departed", 15*time.Minute, s.pruneDeparted)
}

// AuthorizeTopic admits a viewer to channels:<id> when they may read the
// channel: org members for public channels, members otherwise.
func (s *Service) AuthorizeTopic(ctx context.Context, userID uuid.UUID, _ bool, topic string) (handled, allowed bool) {
	rest, ok := strings.CutPrefix(topic, topicKind+":")
	if !ok {
		return false, false
	}
	id, err := uuid.Parse(rest)
	if err != nil {
		return true, false
	}
	_, err = s.loadAccess(ctx, s.d.Pool, id, userID)
	return true, err == nil
}

// ---- config ----------------------------------------------------------------------

// Config is GET /channels/config: limits and which integrations are wired.
type Config struct {
	MaxChars           int   `json:"max_chars"`
	EditWindowSeconds  int   `json:"edit_window_seconds"` // 0 = no limit
	AttachmentMaxBytes int64 `json:"attachment_max_bytes"`
	MaxAttachments     int   `json:"max_attachments"`
	MaxGroupDM         int   `json:"max_group_dm"`
	CallsEnabled       bool  `json:"calls_enabled"`
	GroupsEnabled      bool  `json:"groups_enabled"`
}

func (s *Service) configValue() Config {
	return Config{
		MaxChars: s.cfg.maxChars, EditWindowSeconds: int(s.cfg.editWindow.Seconds()),
		AttachmentMaxBytes: s.cfg.attachmentMax, MaxAttachments: maxAttachments, MaxGroupDM: maxGroupDM,
		CallsEnabled: currentCallStarter() != nil, GroupsEnabled: currentGroupResolver() != nil,
	}
}

func (s *Service) config(w http.ResponseWriter, _ *http.Request) error {
	httpx.OK(w, s.configValue())
	return nil
}

// ---- errors ----------------------------------------------------------------------

var (
	errChannelNotFound = httpx.NotFound("Channel not found.")
	errMessageNotFound = httpx.NotFound("Message not found.")
	errNotMember       = httpx.NewError(http.StatusForbidden, "not_a_member", "Join the channel to do that.")
	errArchived        = httpx.NewError(http.StatusConflict, "channel_archived", "This channel is archived. Unarchive it first.")
	errBlocked         = httpx.NewError(http.StatusForbidden, "blocked", "You can't message this person.")
	errCursorScope     = httpx.BadRequest("That message isn't in this channel.")
	errThreadScope     = httpx.BadRequest("That thread isn't in this channel.")
)
