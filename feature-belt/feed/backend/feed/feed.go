// Package feed is the social feed: posts with media and links, one level of
// comments, reactions, @mentions, staff announcements pinned to the top for a
// bounded time, and a per-user "seen" marker for the unread dot.
//
// Who reads and posts where is decided by a Scope (scope.go). The default
// shows every post to every signed-in user; stitching swaps it to scope the
// feed by org or group without touching this package's queries.
package feed

import (
	"context"
	"log/slog"
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
	"scaffoldapp/backend/internal/storage"
)

// Post kinds.
const (
	KindPost         = "post"
	KindAnnouncement = "announcement"
)

// Notification kinds this module sends.
const (
	NotifyComment      = "feed.comment"
	NotifyReply        = "feed.reply"
	NotifyMention      = "feed.mention"
	NotifyReaction     = "feed.reaction"
	NotifyAnnouncement = "feed.announcement"
)

// Realtime event types, published on the audience topic (AudienceTopic).
const (
	EventPost        = "feed.post"
	EventPostUpdated = "feed.post_updated"
	EventComment     = "feed.comment"
	EventReaction    = "feed.reaction"
)

// Storage purposes for post attachments, both public-read under unguessable
// keys: MediaPurpose for photos and video, FilePurpose for documents (PDF,
// Office, text, zip). They are separate so each keeps its own type list and
// size limit, and the key prefix alone says how to render an attachment.
const (
	MediaPurpose = "feed_media"
	FilePurpose  = "feed_file"
)

const (
	maxMediaPerPost = 10
	maxMentions     = 20
	maxPinned       = 3                   // active announcements shown above the feed
	maxPinDuration  = 90 * 24 * time.Hour // furthest pinned_until accepted
	maxLinkLen      = 2048
	previewLen      = 120 // notification body preview, in characters
)

// Reactions is the fixed reaction set. One per user per target; changeable.
var Reactions = []string{"like", "love", "laugh", "wow", "sad"}

var mediaTypes = []string{"image/jpeg", "image/png", "image/heic", "image/webp", "image/gif", "video/mp4", "video/quicktime"}

type Service struct {
	d     *app.Deps
	cfg   settings
	scope Scope
}

type settings struct {
	postMax    int // characters
	commentMax int // characters
}

func New(d *app.Deps) *Service {
	s := &Service{d: d, cfg: settings{
		postMax:    positive("FEED_POST_MAX_CHARS", 5000),
		commentMax: positive("FEED_COMMENT_MAX_CHARS", 2000),
	}}
	s.scope = buildScope(d)
	mediaMB := positive("FEED_MEDIA_MAX_MB", 50)
	fileMB := positive("FEED_FILE_MAX_MB", 25)
	storage.RegisterPurpose(storage.Purpose{Name: MediaPurpose, Public: true, Types: mediaTypes, MaxBytes: int64(mediaMB) << 20})
	storage.RegisterPurpose(storage.Purpose{Name: FilePurpose, Public: true, Types: storage.DocumentTypes, MaxBytes: int64(fileMB) << 20})

	notifications.RegisterKind(notifications.Kind{ID: NotifyComment, Label: "Comments on your posts", Push: true, AgentTarget: postAgentTarget})
	notifications.RegisterKind(notifications.Kind{ID: NotifyReply, Label: "Replies to your comments", Push: true, AgentTarget: postAgentTarget})
	notifications.RegisterKind(notifications.Kind{ID: NotifyMention, Label: "Mentions", Push: true, AgentTarget: postAgentTarget})
	notifications.RegisterKind(notifications.Kind{ID: NotifyReaction, Label: "Reactions", Push: false, AgentTarget: postAgentTarget})
	notifications.RegisterKind(notifications.Kind{ID: NotifyAnnouncement, Label: "Announcements", Push: true, AgentTarget: postAgentTarget})

	moderation.RegisterTarget("feed.post", postTarget{s})
	moderation.RegisterTarget("feed.comment", commentTarget{s})
	return s
}

func (s *Service) Name() string { return "feed" }

func (s *Service) MountUser(r chi.Router) {
	write := r.With(auth.RequireActive)

	r.Get("/feed/posts", httpx.Handler(s.listPostsHandler))
	r.Get("/feed/pinned", httpx.Handler(s.pinnedHandler))
	r.Get("/feed/posts/{id}", httpx.Handler(s.getPostHandler))
	write.With(auth.RateLimit(10, time.Minute)).Post("/feed/posts", httpx.Handler(s.createPostHandler))
	write.With(auth.RateLimit(30, time.Minute)).Patch("/feed/posts/{id}", httpx.Handler(s.updatePostHandler))
	write.Delete("/feed/posts/{id}", httpx.Handler(s.deletePostHandler))

	r.Get("/feed/posts/{id}/comments", httpx.Handler(s.listCommentsHandler))
	write.With(auth.RateLimit(30, time.Minute)).Post("/feed/posts/{id}/comments", httpx.Handler(s.createCommentHandler))
	write.With(auth.RateLimit(30, time.Minute)).Patch("/feed/comments/{id}", httpx.Handler(s.updateCommentHandler))
	write.Delete("/feed/comments/{id}", httpx.Handler(s.deleteCommentHandler))

	react := write.With(auth.RateLimit(120, time.Minute))
	react.Put("/feed/posts/{id}/reaction", httpx.Handler(s.reactPostHandler))
	react.Delete("/feed/posts/{id}/reaction", httpx.Handler(s.unreactPostHandler))
	react.Put("/feed/comments/{id}/reaction", httpx.Handler(s.reactCommentHandler))
	react.Delete("/feed/comments/{id}/reaction", httpx.Handler(s.unreactCommentHandler))

	r.Get("/feed/unread", httpx.Handler(s.unreadHandler))
	r.Post("/feed/seen", httpx.Handler(s.seenHandler))
	r.With(auth.RateLimit(60, time.Minute)).Get("/feed/people", httpx.Handler(s.peopleHandler))
}

func (s *Service) MountAdmin(r chi.Router) {
	r.Get("/feed/posts", httpx.Handler(s.adminListPosts))
	r.Get("/feed/posts/{id}", httpx.Handler(s.adminGetPost))
	r.Patch("/feed/posts/{id}", httpx.Handler(s.adminPinPost))
	r.Delete("/feed/posts/{id}", httpx.Handler(s.adminRemovePost))
	r.Delete("/feed/comments/{id}", httpx.Handler(s.adminRemoveComment))
	r.With(auth.RateLimit(10, time.Minute)).Post("/feed/announcements", httpx.Handler(s.adminCreateAnnouncement))
}

// Start runs the media cleanup loop: objects of deleted posts and of media
// dropped in an edit are queued in feed_media_trash inside the transaction
// and removed here, so a rolled-back delete never loses a file.
func (s *Service) Start(ctx context.Context) {
	go jobs.Every(ctx, s.d.Pool, "feed.media_trash", 10*time.Minute, s.emptyTrash)
}

// AuthorizeTopic allows feed:<uuid> topics for audiences the user reads:
// feed:<nil uuid> is the "all" audience.
func (s *Service) AuthorizeTopic(ctx context.Context, userID uuid.UUID, _ bool, topic string) (bool, bool) {
	id, ok := topicID(topic)
	if !ok {
		return false, false
	}
	auds, err := s.scope.Audiences(ctx, userID)
	if err != nil {
		slog.WarnContext(ctx, "feed: audiences for topic", "err", err)
		return true, false
	}
	for _, a := range auds {
		if aid, ok := audienceID(a); ok && aid == id {
			return true, true
		}
	}
	return true, false
}

// positive reads a positive integer setting; zero or less fails the boot.
func positive(key string, def int) int {
	v := config.Int(key, def)
	if v <= 0 {
		config.Problem("%s must be a positive number (got %d)", key, v)
		return def
	}
	return v
}
