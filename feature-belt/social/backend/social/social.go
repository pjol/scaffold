// Package social is public profiles, the follow graph and the activity
// feed. Every person gets a handle (unique, case-insensitive, changeable
// with a cooldown; the old one redirects for a while), a bio and a few
// links next to the display name and photo core keeps. People follow each
// other (follows you, followers and following lists, counts), choose who
// sees each part of their profile (public, followers, only me; modules add
// parts with RegisterSection), and read what the people they follow did
// (modules record it with Record; kinds say how to show it).
//
// Blocks are the moderation feature's: a block either way refuses a follow,
// hides the person everywhere social shows people, and the follows between
// them are removed (see graph.go: repair on read plus a daily sweep).
package social

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/jobs"
	"scaffoldapp/backend/internal/moderation"
	"scaffoldapp/backend/internal/notifications"
)

// KindFollowNotice is the notification sent to a person someone followed.
const KindFollowNotice = "social.follow"

// KindFollowed is the built-in activity kind "<actor> followed <subject>".
const KindFollowed = "social.followed"

// MaxLinks is how many links a profile carries.
const MaxLinks = 5

type Service struct {
	d *app.Deps

	handleCooldown time.Duration // SOCIAL_HANDLE_COOLDOWN
	handleRedirect time.Duration // SOCIAL_HANDLE_REDIRECT
	reserved       []string      // SOCIAL_RESERVED_HANDLES
	maxFollowing   int           // SOCIAL_MAX_FOLLOWING
	bioMax         int           // SOCIAL_BIO_MAX_CHARS
	retention      time.Duration // SOCIAL_ACTIVITY_RETENTION
}

func New(d *app.Deps) *Service {
	s := &Service{
		d:              d,
		handleCooldown: config.Duration("SOCIAL_HANDLE_COOLDOWN", 7*24*time.Hour),
		handleRedirect: config.Duration("SOCIAL_HANDLE_REDIRECT", 30*24*time.Hour),
		reserved:       config.List("SOCIAL_RESERVED_HANDLES", ""),
		maxFollowing:   config.Int("SOCIAL_MAX_FOLLOWING", 2000),
		bioMax:         config.Int("SOCIAL_BIO_MAX_CHARS", 280),
		retention:      config.Duration("SOCIAL_ACTIVITY_RETENTION", 90*24*time.Hour),
	}
	if s.handleCooldown < 0 {
		config.Problem("SOCIAL_HANDLE_COOLDOWN must be 0 or more (got %s)", s.handleCooldown)
		s.handleCooldown = 7 * 24 * time.Hour
	}
	if s.handleRedirect < 0 {
		config.Problem("SOCIAL_HANDLE_REDIRECT must be 0 or more (got %s)", s.handleRedirect)
		s.handleRedirect = 30 * 24 * time.Hour
	}
	for _, h := range s.reserved {
		if !ValidHandle(NormalizeHandle(h)) {
			config.Problem("SOCIAL_RESERVED_HANDLES: %q isn't a handle (%d–%d of a–z, 0–9 and _, starting with a letter)", h, HandleMin, HandleMax)
		}
	}
	if s.maxFollowing < 1 || s.maxFollowing > 100_000 {
		config.Problem("SOCIAL_MAX_FOLLOWING must be between 1 and 100000 (got %d)", s.maxFollowing)
		s.maxFollowing = 2000
	}
	if s.bioMax < 0 || s.bioMax > 5000 {
		config.Problem("SOCIAL_BIO_MAX_CHARS must be between 0 and 5000 (got %d)", s.bioMax)
		s.bioMax = 280
	}
	if s.retention < 24*time.Hour {
		config.Problem("SOCIAL_ACTIVITY_RETENTION must be at least 24h (got %s)", s.retention)
		s.retention = 90 * 24 * time.Hour
	}

	notifications.RegisterKind(notifications.Kind{
		ID: KindFollowNotice, Label: "New followers", Push: true,
		// "Sam and 3 others followed you": one inbox group per person followed.
		Group: func(data map[string]any) (string, string) {
			id, _ := data["followee_id"].(string)
			return "social.followers", id
		},
	})
	moderation.RegisterTarget(TargetProfile, profileTarget{s})
	return s
}

func (s *Service) Name() string { return "social" }

func (s *Service) MountUser(r chi.Router) {
	follow := auth.RateLimit(100, time.Hour)        // follow and unfollow share it
	followDay := auth.RateLimit(1000, 24*time.Hour) // and a daily ceiling
	search := auth.RateLimit(60, time.Minute)

	r.Get("/social/me", httpx.Handler(s.getMe))
	r.With(auth.RequireActive, auth.RateLimit(30, time.Hour)).Patch("/social/me", httpx.Handler(s.patchMe))
	r.With(auth.RequireActive, auth.RateLimit(10, time.Hour)).Put("/social/me/handle", httpx.Handler(s.putHandle))
	r.With(search).Get("/social/handles/{handle}", httpx.Handler(s.getHandleCheck))

	r.Get("/social/profiles/{handle}", httpx.Handler(s.getProfile))
	r.Get("/social/profiles/{handle}/followers", httpx.Handler(s.getFollowers))
	r.Get("/social/profiles/{handle}/following", httpx.Handler(s.getFollowing))
	r.Get("/social/profiles/{handle}/activity", httpx.Handler(s.getProfileActivity))

	r.With(auth.RequireActive, follow, followDay).Put("/social/follows/{user_id}", httpx.Handler(s.putFollow))
	r.With(auth.RequireActive, follow).Delete("/social/follows/{user_id}", httpx.Handler(s.deleteFollow))

	r.Get("/social/feed", httpx.Handler(s.getFeed))
	r.With(search).Get("/social/people", httpx.Handler(s.getPeople))
	r.Get("/social/suggestions", httpx.Handler(s.getSuggestions))
}

// Start runs the background jobs, each once across instances:
// social.profiles gives new accounts a profile, social.blocks removes
// follows a block now separates, social.retention prunes old activity,
// expired handle holds and follow-notice stamps.
func (s *Service) Start(ctx context.Context) {
	go jobs.Every(ctx, s.d.Pool, "social.profiles", time.Minute, s.backfillProfiles)
	go jobs.Every(ctx, s.d.Pool, "social.blocks", time.Minute, func(ctx context.Context) error {
		n, err := sweepBlocked(ctx, s.d.Pool, time.Now())
		if n > 0 {
			slog.InfoContext(ctx, "social: follows removed after blocks", "follows", n)
		}
		return err
	})
	go jobs.Every(ctx, s.d.Pool, "social.retention", time.Hour, s.prune)
}

// staff reports support or admin (they may take handles resembling
// protected names).
func staff(id *auth.Identity) bool { return id != nil && id.IsSupport() }

// reservedFor is the extra reserved list, normalised.
func (s *Service) reservedFor() []string {
	out := make([]string, 0, len(s.reserved))
	for _, r := range s.reserved {
		out = append(out, strings.ToLower(strings.TrimSpace(r)))
	}
	return out
}
