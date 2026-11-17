// Package forum is a small Reddit-like forum: communities (staff-made, or
// made by the app for its own objects with EnsureCommunity), text and link
// posts, threaded comments (depth ≤ 8), ±1 votes ranked hot, new and top,
// karma, @mentions, pins and locks by moderators. Every post, comment and
// edit goes through moderation.Screen; held items stay visible to their
// author (and staff) until the moderation queue approves or removes them.
package forum

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/moderation"
	"scaffoldapp/backend/internal/notifications"
)

// Moderation target kinds (reports, screening, the staff queue).
const (
	TargetPost    = "forum.post"
	TargetComment = "forum.comment"
)

// Notification kinds this module sends.
const (
	NotifyComment = "forum.comment" // a top-level comment on your post
	NotifyReply   = "forum.reply"   // a reply to your comment
	NotifyMention = "forum.mention"
)

// Realtime events on a post's topic (PostTopic).
const (
	EventComment     = "forum.comment"      // {post_id, comment_id, parent_id, comment_count}
	EventVote        = "forum.vote"         // {post_id, target_kind, target_id, score}
	EventStatus      = "forum.status"       // {post_id, target_kind, target_id, status}
	EventPostUpdated = "forum.post_updated" // {post_id}
)

// Content statuses (moderation's, plus deleted by the author).
const (
	StatusVisible = moderation.ContentVisible
	StatusHeld    = moderation.ContentHeld
	StatusRemoved = moderation.ContentRemoved
	StatusDeleted = "deleted"
)

// Limits.
const (
	TitleMin, TitleMax = 3, 300
	URLMax             = 2048
	MaxDepth           = 8   // replies to a comment at this depth attach beside it
	MaxTopComments     = 200 // top-level comments in one page of the tree
	MaxChildren        = 50  // replies shown per comment; the rest count in more_replies
	MentionsMax        = 10
	newAccount         = 3 * 24 * time.Hour
)

type Service struct {
	d   *app.Deps
	cfg settings
}

type settings struct {
	postMax     int
	commentMax  int
	voteMinAge  time.Duration // accounts younger can't vote
	karmaMinAge time.Duration // votes from accounts younger don't count for karma
}

func New(d *app.Deps) *Service {
	s := &Service{d: d, cfg: settings{
		postMax:     positive("FORUM_POST_MAX_CHARS", 10000),
		commentMax:  positive("FORUM_COMMENT_MAX_CHARS", 5000),
		voteMinAge:  nonNegative("FORUM_VOTE_MIN_AGE", 0),
		karmaMinAge: nonNegative("FORUM_KARMA_MIN_AGE", 7*24*time.Hour),
	}}
	target := notifications.DataTarget(TargetPost, "post_id")
	notifications.RegisterKind(notifications.Kind{ID: NotifyComment, Label: "Comments on your forum posts", Push: true, AgentTarget: target})
	notifications.RegisterKind(notifications.Kind{ID: NotifyReply, Label: "Replies to your forum comments", Push: true, AgentTarget: target})
	notifications.RegisterKind(notifications.Kind{ID: NotifyMention, Label: "Forum mentions", Push: true, AgentTarget: target})
	moderation.RegisterTarget(TargetPost, postTarget{s})
	moderation.RegisterTarget(TargetComment, commentTarget{s})
	return s
}

func (s *Service) Name() string { return "forum" }

func (s *Service) MountUser(r chi.Router) {
	write := r.With(auth.RequireActive)

	r.Get("/forum/communities", httpx.Handler(s.listCommunitiesHandler))
	r.Get("/forum/communities/{slug}", httpx.Handler(s.getCommunityHandler))

	r.Get("/forum/posts", httpx.Handler(s.listPostsHandler))
	r.Get("/forum/posts/{id}", httpx.Handler(s.getPostHandler))
	write.With(auth.RateLimit(10, time.Minute)).Post("/forum/posts", httpx.Handler(s.createPostHandler))
	write.With(auth.RateLimit(30, time.Minute)).Patch("/forum/posts/{id}", httpx.Handler(s.editPostHandler))
	write.Delete("/forum/posts/{id}", httpx.Handler(s.deletePostHandler))
	write.With(auth.RateLimit(60, time.Minute)).Post("/forum/posts/{id}/moderate", httpx.Handler(s.moderatePostHandler))

	r.Get("/forum/posts/{id}/comments", httpx.Handler(s.listCommentsHandler))
	write.With(auth.RateLimit(20, time.Minute)).Post("/forum/posts/{id}/comments", httpx.Handler(s.createCommentHandler))
	r.Get("/forum/comments/{id}/replies", httpx.Handler(s.repliesHandler))
	write.With(auth.RateLimit(30, time.Minute)).Patch("/forum/comments/{id}", httpx.Handler(s.editCommentHandler))
	write.Delete("/forum/comments/{id}", httpx.Handler(s.deleteCommentHandler))
	write.With(auth.RateLimit(60, time.Minute)).Post("/forum/comments/{id}/moderate", httpx.Handler(s.moderateCommentHandler))

	write.With(auth.RateLimit(120, time.Minute)).Put("/forum/votes", httpx.Handler(s.voteHandler))
	r.With(auth.RateLimit(60, time.Minute)).Get("/forum/people", httpx.Handler(s.peopleHandler))
	r.Get("/forum/karma", httpx.Handler(s.karmaHandler))
}

func (s *Service) MountAdmin(r chi.Router) {
	r.Get("/forum/communities", httpx.Handler(s.adminListCommunities))
	r.Post("/forum/communities", httpx.Handler(s.adminCreateCommunity))
	r.Patch("/forum/communities/{id}", httpx.Handler(s.adminUpdateCommunity))
	r.Post("/forum/communities/{id}/moderators", httpx.Handler(s.adminAddModerator))
	r.Delete("/forum/communities/{id}/moderators/{user_id}", httpx.Handler(s.adminRemoveModerator))
}

// PostTopic is a post's realtime topic: new comments, votes, status changes.
func PostTopic(postID uuid.UUID) string { return "forum:" + postID.String() }

// AuthorizeTopic allows forum:<post> to anyone who may open the post.
func (s *Service) AuthorizeTopic(ctx context.Context, userID uuid.UUID, isAdmin bool, topic string) (bool, bool) {
	const prefix = "forum:"
	if len(topic) <= len(prefix) || topic[:len(prefix)] != prefix {
		return false, false
	}
	id, err := uuid.Parse(topic[len(prefix):])
	if err != nil {
		return true, false
	}
	p, err := s.loadPost(ctx, s.d.Pool, id, false)
	if err != nil {
		return true, false
	}
	v, err := s.viewerFor(ctx, userID, isAdmin)
	if err != nil {
		slog.WarnContext(ctx, "forum: viewer for topic", "err", err)
		return true, false
	}
	return true, v.canSeePost(p)
}

// viewer is who is reading: their blocks both ways and whether they are staff.
type viewer struct {
	id         uuid.UUID
	staff      bool
	blocked    []uuid.UUID
	blockedSet map[uuid.UUID]bool
}

func (s *Service) viewerOf(ctx context.Context, caller *auth.Identity) (viewer, error) {
	return s.viewerFor(ctx, caller.UserID, caller.IsSupport())
}

func (s *Service) viewerFor(ctx context.Context, userID uuid.UUID, staff bool) (viewer, error) {
	blocked, err := moderation.BlockedIDs(ctx, s.d.Pool, userID)
	if err != nil {
		return viewer{}, err
	}
	set := make(map[uuid.UUID]bool, len(blocked))
	for _, b := range blocked {
		set[b] = true
	}
	return viewer{id: userID, staff: staff, blocked: blocked, blockedSet: set}, nil
}

func (v viewer) blocks(id uuid.UUID) bool { return v.blockedSet[id] }

// canSeePost: visible and deleted posts (a placeholder) are open to everyone
// the author hasn't blocked (either way); held and removed ones only to the
// author and staff.
func (v viewer) canSeePost(p postRow) bool {
	if v.blocks(p.AuthorID) && p.Status != StatusDeleted && p.AuthorID != v.id {
		return false
	}
	switch p.Status {
	case StatusVisible, StatusDeleted:
		return true
	}
	return v.id == p.AuthorID || v.staff
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

func nonNegative(key string, def time.Duration) time.Duration {
	v := config.Duration(key, def)
	if v < 0 {
		config.Problem("%s must be zero or more (got %s)", key, v)
		return def
	}
	return v
}

// Errors clients branch on.
var (
	errCommunityLocked = httpx.NewError(http.StatusForbidden, "community_locked", "This community is locked. Only its moderators can post.")
	errPostLocked      = httpx.NewError(http.StatusForbidden, "post_locked", "This post is locked, so it takes no new comments.")
	errBlocked         = httpx.NewError(http.StatusForbidden, "blocked", "You can't interact with this person.")
	errVoteOwn         = httpx.NewError(http.StatusForbidden, "vote_restricted", "You can't vote on your own posts or comments.")
)

func rateLimited(msg string) *httpx.Error {
	return httpx.NewError(http.StatusTooManyRequests, "rate_limited", msg)
}
