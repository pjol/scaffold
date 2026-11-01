// Package chat is direct and group messaging: conversations, members with
// roles and read pointers, messages with attachments, replies, reactions,
// edits and tombstones, realtime delivery and typing, unread counts, and the
// moderation hooks (blocks, the pre-post filter, reports, staff removal).
//
// Rules that hold everywhere in this package:
//   - Membership is an active chat_members row (left_at IS NULL). Anything a
//     non-member asks about answers "not found", so membership can't be probed.
//   - Ids a client sends are scoped in SQL to the conversation the caller is
//     a member of (reply targets, cursors, attachments, members).
//   - Ordering is (created_at, id) for messages and (last_message_at, id) for
//     the inbox; pagination is keyset in both directions.
//   - Notifications and realtime events go out after the transaction commits
//     and never fail the request.
package chat

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/jobs"
	"scaffoldapp/backend/internal/moderation"
	"scaffoldapp/backend/internal/notifications"
	"scaffoldapp/backend/internal/storage"
)

const (
	// PurposeAttachment is the private upload purpose for message files.
	PurposeAttachment = "chat_attachment"
	// PurposeAvatar is the public upload purpose for group pictures.
	PurposeAvatar = "chat_avatar"

	topicKind  = "chat"         // realtime topics chat:<conversation id>
	notifyKind = "chat.message" // notifications kind
	targetKind = "chat.message" // moderation target kind

	// Realtime event types. Conversation topic: message, message_updated,
	// read, typing, member. User topic: inbox.
	EventMessage        = "chat.message"
	EventMessageUpdated = "chat.message_updated"
	EventRead           = "chat.read"
	EventTyping         = "chat.typing"
	EventMember         = "chat.member"
	EventInbox          = "chat.inbox"

	maxAttachments      = 10
	maxReactionsPerUser = 12 // distinct emoji one person may put on one message
	attachmentURLTTL    = time.Hour
)

// mutedForever is stored in muted_until for "until I unmute".
var mutedForever = time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)

// attachmentTypes are the content types chat accepts: photos, video, audio
// and documents (storage.DocumentTypes).
var attachmentTypes = append([]string{
	"image/jpeg", "image/png", "image/heic", "image/webp", "image/gif",
	"video/mp4", "video/quicktime", "audio/mp4", "audio/mpeg",
}, storage.DocumentTypes...)

// attachmentExt maps a storage key's extension to its type and kind. Keys are
// issued by storage.PresignPut, so the extension is the validated type.
var attachmentExt = map[string]struct{ contentType, kind string }{
	"jpg": {"image/jpeg", "image"}, "png": {"image/png", "image"}, "heic": {"image/heic", "image"},
	"webp": {"image/webp", "image"}, "gif": {"image/gif", "image"},
	"mp4": {"video/mp4", "video"}, "mov": {"video/quicktime", "video"},
	"m4a": {"audio/mp4", "audio"}, "mp3": {"audio/mpeg", "audio"},
	"pdf": {"application/pdf", "file"}, "txt": {"text/plain", "file"}, "csv": {"text/csv", "file"},
	"zip":  {"application/zip", "file"},
	"docx": {"application/vnd.openxmlformats-officedocument.wordprocessingml.document", "file"},
	"xlsx": {"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "file"},
	"pptx": {"application/vnd.openxmlformats-officedocument.presentationml.presentation", "file"},
}

type Service struct {
	d   *app.Deps
	cfg settings

	// deletedMembership holds, between an account's deletion transaction and
	// its AccountDeleted hook, the conversations whose topics to revoke.
	deletedMembership sync.Map // uuid.UUID → []uuid.UUID
}

type settings struct {
	maxChars      int
	groupMax      int
	editWindow    time.Duration // 0 turns editing off
	newPerDay     int
	perMinute     int
	attachmentMax int64
}

func New(d *app.Deps) *Service {
	s := &Service{d: d, cfg: settings{
		maxChars:      atLeast("CHAT_MESSAGE_MAX_CHARS", config.Int("CHAT_MESSAGE_MAX_CHARS", 4000), 1),
		groupMax:      atLeast("CHAT_GROUP_MAX_MEMBERS", config.Int("CHAT_GROUP_MAX_MEMBERS", 50), 3),
		editWindow:    config.Duration("CHAT_EDIT_WINDOW", 5*time.Minute),
		newPerDay:     atLeast("CHAT_NEW_CONVERSATIONS_PER_DAY", config.Int("CHAT_NEW_CONVERSATIONS_PER_DAY", 20), 1),
		perMinute:     atLeast("CHAT_MESSAGES_PER_MINUTE", config.Int("CHAT_MESSAGES_PER_MINUTE", 60), 1),
		attachmentMax: int64(atLeast("CHAT_ATTACHMENT_MAX_MB", config.Int("CHAT_ATTACHMENT_MAX_MB", 25), 1)) << 20,
	}}
	if s.cfg.editWindow < 0 {
		config.Problem("CHAT_EDIT_WINDOW must be 0 (editing off) or a positive duration (got %s)", s.cfg.editWindow)
		s.cfg.editWindow = 0
	}
	storage.RegisterPurpose(storage.Purpose{Name: PurposeAttachment, Types: attachmentTypes, MaxBytes: s.cfg.attachmentMax})
	storage.RegisterPurpose(storage.Purpose{Name: PurposeAvatar, Public: true, Types: storage.ImageTypes, MaxBytes: 5 << 20})
	notifications.RegisterKind(notifications.Kind{ID: notifyKind, Label: "Messages", Push: true})
	moderation.RegisterTarget(targetKind, messageTarget{s})
	return s
}

// atLeast records a config problem when v is below minimum.
func atLeast(name string, v, minimum int) int {
	if v < minimum {
		config.Problem("%s must be at least %d (got %d)", name, minimum, v)
		return minimum
	}
	return v
}

func (s *Service) Name() string { return "chat" }

func (s *Service) MountUser(r chi.Router) {
	h := httpx.Handler
	r.Get("/chat/conversations", h(s.listConversations))
	r.Get("/chat/conversations/{id}", h(s.getConversation))
	r.Get("/chat/conversations/{id}/messages", h(s.listMessages))
	r.Get("/chat/unread", h(s.unread))
	r.With(auth.RateLimit(60, time.Minute)).Get("/chat/search", h(s.search))
	r.With(auth.RateLimit(60, time.Minute)).Get("/chat/people", h(s.people))
	r.Get("/chat/attachments/{id}", h(s.attachmentURL))

	// The reader's own state: allowed while suspended.
	r.Post("/chat/conversations/{id}/read", h(s.markRead))
	r.Put("/chat/conversations/{id}/mute", h(s.mute))
	r.Delete("/chat/conversations/{id}/mute", h(s.unmute))
	r.Post("/chat/conversations/{id}/archive", h(s.archive))
	r.Delete("/chat/conversations/{id}/archive", h(s.unarchive))
	r.Delete("/chat/conversations/{id}", h(s.leaveConversation))

	// Anything other people see: active accounts only.
	w := r.With(auth.RequireActive)
	w.With(auth.RateLimit(30, time.Minute)).Post("/chat/conversations", h(s.createConversation))
	w.With(auth.RateLimit(30, time.Minute)).Patch("/chat/conversations/{id}", h(s.updateConversation))
	w.With(auth.RateLimit(30, time.Minute)).Post("/chat/conversations/{id}/members", h(s.addMembers))
	w.Patch("/chat/conversations/{id}/members/{user_id}", h(s.setMemberRole))
	w.Delete("/chat/conversations/{id}/members/{user_id}", h(s.removeMember))
	w.With(auth.RateLimit(s.cfg.perMinute, time.Minute)).Post("/chat/conversations/{id}/messages", h(s.sendMessage))
	w.With(auth.RateLimit(40, time.Minute)).Post("/chat/conversations/{id}/typing", h(s.typing))
	w.With(auth.RateLimit(30, time.Minute)).Patch("/chat/messages/{id}", h(s.editMessage))
	w.With(auth.RateLimit(60, time.Minute)).Delete("/chat/messages/{id}", h(s.deleteMessage))
	w.With(auth.RateLimit(60, time.Minute)).Post("/chat/messages/{id}/reactions", h(s.addReaction))
	w.With(auth.RateLimit(60, time.Minute)).Delete("/chat/messages/{id}/reactions", h(s.removeReaction))
}

func (s *Service) MountAdmin(r chi.Router) {
	r.Get("/chat/conversations/{id}", httpx.Handler(s.adminConversation))
	r.Delete("/chat/messages/{id}", httpx.Handler(s.adminRemoveMessage))
}

// Start runs the storage clean-up for deleted rows.
func (s *Service) Start(ctx context.Context) {
	go jobs.Every(ctx, s.d.Pool, "chat.delete_objects", time.Minute, s.deleteQueuedObjects)
}

// AuthorizeTopic admits members to chat:<conversation id>.
func (s *Service) AuthorizeTopic(ctx context.Context, userID uuid.UUID, _ bool, topic string) (handled, allowed bool) {
	rest, ok := strings.CutPrefix(topic, topicKind+":")
	if !ok {
		return false, false
	}
	id, err := uuid.Parse(rest)
	if err != nil {
		return true, false
	}
	var member bool
	err = s.d.Pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM chat_members WHERE conversation_id = $1 AND user_id = $2 AND left_at IS NULL)`,
		id, userID).Scan(&member)
	return true, err == nil && member
}

// ---- contact policy ----------------------------------------------------------

// ContactPolicy decides whether from may start a direct conversation with, or
// add to a group, the user to. Return an *httpx.Error to refuse (403 with a
// message the person can act on). Blocks are checked before it runs.
type ContactPolicy func(ctx context.Context, q db.Querier, from, to uuid.UUID) error

var contactPolicy ContactPolicy

// SetContactPolicy installs the app's rule for who may message whom (call it
// from stitching code, e.g. "only people in a shared organization"). The
// default allows any active user.
func SetContactPolicy(p ContactPolicy) { contactPolicy = p }

// ---- errors ----------------------------------------------------------------------

var (
	errConversationNotFound = httpx.NotFound("Conversation not found.")
	errMessageNotFound      = httpx.NotFound("Message not found.")
	errPersonNotFound       = httpx.NotFound("That person isn't available.")
	errBlocked              = httpx.NewError(http.StatusForbidden, "blocked", "You can't message this person.")
	errRecipientGone        = httpx.NewError(http.StatusConflict, "recipient_unavailable", "This person can't receive messages.")
	errNotGroupAdmin        = httpx.Forbidden("Only the group's owner or admins can do that.")
	errNotGroup             = httpx.BadRequest("That only works in group conversations.")
	errReplyScope           = httpx.BadRequest("The message you replied to isn't in this conversation.")
	errCursorScope          = httpx.BadRequest("That message isn't in this conversation.")
)
