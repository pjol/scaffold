// Package files is an org-scoped drive: folders, files with versions,
// previews, public links, trash and a storage quota, plus item types other
// features register (the repos feature's repositories).
//
// Rules that hold everywhere in this package:
//   - Every item is an access resource of kind files.item whose parent is its
//     folder. Permission questions go to access (Require, access_level,
//     access_visible); nothing here computes a level itself.
//   - REST routes, agent tools and global search call the same query
//     functions (query.go), passing access.ViaAgent(ctx), so an agent acting
//     for a person reaches exactly what that person can, capped by their
//     agent scope.
//   - An item lives in one person's My drive (drive "my", owner = that
//     person) or in an org-owned shared folder (drive "shared", no owner).
//     Moving between trees needs manage.
//   - Objects are keyed by the org (files/<org id>/…), so org content
//     survives its uploader's account deletion; a trigger queues an object for
//     deletion whenever its version row goes.
//   - Realtime events and activity side effects never fail the request.
package files

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/httprate"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/jobs"
	"scaffoldapp/backend/internal/moderation"
	"scaffoldapp/backend/internal/notifications"
	"scaffoldapp/backend/internal/orgs"
	"scaffoldapp/backend/internal/realtime"
	"scaffoldapp/backend/internal/storage"
)

const (
	// Kind is the access resource kind of every drive item (folders, files
	// and registered item types).
	Kind = "files.item"
	// Purpose is the private upload purpose; keys are files/<org id>/….
	Purpose = "files"
	// PermCreateShared lets a member create top-level shared folders.
	PermCreateShared = "files.create_shared_folders"
	// EventChanged goes to files:<folder id> (or the org / the owner for top
	// level items) and files:<item id>: {org_id, item_id, parent_id, action}.
	EventChanged = "files.changed"
	// KindPublicDisabled tells an item's owner (or its uploader) staff turned
	// its public links off.
	KindPublicDisabled = "files.public_disabled"

	topicKind = "files"

	urlTTL          = 15 * time.Minute
	uploadTTL       = 24 * time.Hour
	maxVersions     = 100
	maxTextPreview  = 1 << 20   // bytes the text endpoint returns
	maxAgentRead    = 256 << 10 // bytes the read tool returns
	maxTextEdit     = 1 << 20   // largest file the line edit tool rewrites
	maxIndexedFile  = 2 << 20   // files up to this size get their text indexed
	maxIndexedText  = 100 << 10 // bytes of text indexed for search
	maxBatch        = 200
	maxCopyItems    = 500
	maxArchiveItems = 2000
	maxArchiveBytes = 4 << 30
)

// Service is the files module.
type Service struct {
	d   *app.Deps
	cfg settings
}

type settings struct {
	quotaBytes   int64 // 0: no quota
	maxUpload    int64
	trashDays    int
	keepDays     int // a deleted account's private items stay this long (0: deleted with the account)
	publicPerMin int
}

// svc backs the package-level seam functions (CreateItem, GetItem, …) that
// other features call without a Service.
var svc atomic.Pointer[Service]

// New reads the settings and registers the access kind, upload purpose,
// permission, moderation target and notification kind.
func New(d *app.Deps) *Service {
	s := &Service{d: d, cfg: settings{
		quotaBytes:   int64(atLeast("FILES_ORG_QUOTA_GB", config.Int("FILES_ORG_QUOTA_GB", 100), 0)) << 30,
		maxUpload:    int64(atLeast("FILES_MAX_UPLOAD_MB", config.Int("FILES_MAX_UPLOAD_MB", 250), 1)) << 20,
		trashDays:    atLeast("FILES_TRASH_DAYS", config.Int("FILES_TRASH_DAYS", 30), 1),
		keepDays:     atLeast("FILES_DELETED_ACCOUNT_KEEP_DAYS", config.Int("FILES_DELETED_ACCOUNT_KEEP_DAYS", 30), 0),
		publicPerMin: atLeast("FILES_PUBLIC_DOWNLOADS_PER_MIN", config.Int("FILES_PUBLIC_DOWNLOADS_PER_MIN", 30), 1),
	}}
	svc.Store(s)
	storage.RegisterPurpose(storage.Purpose{Name: Purpose, AnyType: true, MaxBytes: s.cfg.maxUpload})
	access.RegisterKind(access.Kind{
		Name:  Kind,
		Label: "item",
		Title: func(ctx context.Context, q db.Querier, id uuid.UUID) (string, error) {
			var name string
			err := q.QueryRow(ctx, `SELECT name FROM files_items WHERE id = $1`, id).Scan(&name)
			return name, err
		},
		Link:       itemLink,
		PublicPath: func(token string) string { return "/f/" + token },
	})
	access.RegisterPermission(access.Permission{
		Key: PermCreateShared, Title: "Create shared folders",
		Info:         "Make top-level folders the " + orgs.Noun() + " owns, for a team or everyone. Anyone can share folders from their own drive.",
		DefaultRoles: []string{orgs.RoleOwner, orgs.RoleAdmin},
	})
	access.OnChange(s.accessChanged)
	moderation.RegisterTarget(Kind, itemTarget{s})
	notifications.RegisterKind(notifications.Kind{ID: KindPublicDisabled, Label: "Public links turned off by staff", Push: true, Email: true})
	httpx.RedactPathPrefix("/v1/files/public/", "/files-archive/", "/files-connected/")
	RegisterItemType(ItemType{Type: TypeLink, Title: "Shortcut", Icon: "link", Link: itemLink,
		OnDelete: func(context.Context, pgx.Tx, uuid.UUID) error { return nil }}) // files_links goes with the row
	return s
}

func atLeast(name string, v, minimum int) int {
	if v < minimum {
		config.Problem("%s must be at least %d (got %d)", name, minimum, v)
		return minimum
	}
	return v
}

func (s *Service) Name() string { return "files" }

func (s *Service) MountUser(r chi.Router) {
	h := httpx.Handler
	member := orgs.Require(orgs.RoleMember)
	w := r.With(auth.RequireActive)

	r.Get("/files/types", h(s.listTypes))
	r.With(member).Get("/files/orgs/{orgID}/drive", h(s.driveInfo))
	r.With(member).Get("/files/orgs/{orgID}/items", h(s.listSection))
	r.With(member, auth.RateLimit(120, time.Minute)).Get("/files/orgs/{orgID}/search", h(s.searchRoute))
	w.With(member, auth.RateLimit(600, time.Hour)).Post("/files/orgs/{orgID}/folders", h(s.createFolder))
	w.With(member, auth.RateLimit(600, time.Hour)).Post("/files/orgs/{orgID}/files", h(s.createTextFile))
	w.With(member, auth.RateLimit(2000, time.Hour)).Post("/files/orgs/{orgID}/uploads", h(s.startUpload))
	w.Post("/files/uploads/{uploadID}/complete", h(s.completeUpload))
	r.Delete("/files/uploads/{uploadID}", h(s.cancelUpload))
	w.With(member, auth.RateLimit(60, time.Hour)).Post("/files/orgs/{orgID}/archives", h(s.createArchive))
	w.With(member, auth.RateLimit(30, time.Hour)).Delete("/files/orgs/{orgID}/trash", h(s.emptyTrash))
	w.With(auth.RateLimit(120, time.Minute)).Post("/files/batch", h(s.batch))

	r.Get("/files/items/{id}", h(s.getItemRoute))
	r.Get("/files/items/{id}/children", h(s.childrenRoute))
	r.Get("/files/items/{id}/url", h(s.urlRoute))
	r.With(auth.RateLimit(120, time.Minute)).Get("/files/items/{id}/text", h(s.textRoute))
	r.Get("/files/items/{id}/versions", h(s.listVersions))
	r.Get("/files/items/{id}/activity", h(s.activityRoute))
	r.Put("/files/items/{id}/star", h(s.star))
	r.Delete("/files/items/{id}/star", h(s.unstar))
	w.With(auth.RateLimit(600, time.Hour)).Patch("/files/items/{id}", h(s.renameRoute))
	w.With(auth.RateLimit(600, time.Hour)).Post("/files/items/{id}/move", h(s.moveRoute))
	w.With(auth.RateLimit(300, time.Hour)).Post("/files/items/{id}/copy", h(s.copyRoute))
	w.Post("/files/items/{id}/trash", h(s.trashRoute))
	w.Post("/files/items/{id}/restore", h(s.restoreRoute))
	w.Delete("/files/items/{id}", h(s.deleteRoute))
	w.With(auth.RateLimit(300, time.Hour)).Post("/files/items/{id}/versions/{versionID}/restore", h(s.restoreVersion))
	s.mountConnected(r)
}

func (s *Service) MountPublic(r chi.Router) {
	h := httpx.Handler
	browse := httprate.LimitBy(120, time.Minute, httpx.KeyByClientIP, httprate.WithLimitHandler(httpx.RateLimitedHandler))
	fetch := httprate.LimitBy(s.cfg.publicPerMin, time.Minute, httpx.KeyByClientIP, httprate.WithLimitHandler(httpx.RateLimitedHandler))
	pub := r.With(refuseAgentCredential, noStore)
	pub.With(browse).Get("/files/public/{token}", h(s.publicRoot))
	pub.With(browse).Get("/files/public/{token}/items/{id}", h(s.publicItem))
	pub.With(browse).Get("/files/public/{token}/items/{id}/children", h(s.publicChildren))
	pub.With(fetch).Get("/files/public/{token}/items/{id}/url", h(s.publicURL))
	pub.With(fetch).Get("/files/public/{token}/items/{id}/text", h(s.publicText))
}

// MountRoot serves zip downloads: long responses outside the request
// timeout, authorized by a short-lived token minted by a signed-in route.
func (s *Service) MountRoot(r chi.Router) {
	r.With(refuseAgentCredential).Get("/files-archive/{token}", s.serveArchive)
	r.With(refuseAgentCredential).Get("/files-connected/{token}", s.serveConnected)
}

func (s *Service) MountAdmin(r chi.Router) {
	h := httpx.Handler
	r.Get("/files/public", h(s.adminPublicList))
	r.Get("/files/items/{id}", h(s.adminItem))
	r.Post("/files/items/{id}/disable", h(s.adminDisable))
	r.Post("/files/items/{id}/enable", h(s.adminEnable))
}

// Start runs the clean-up jobs: object deletion, trash purge, stale uploads.
func (s *Service) Start(ctx context.Context) {
	go jobs.Every(ctx, s.d.Pool, "files.delete_objects", time.Minute, s.deleteQueuedObjects)
	go jobs.Every(ctx, s.d.Pool, "files.purge_trash", 10*time.Minute, s.purgeExpired)
	go jobs.Every(ctx, s.d.Pool, "files.cleanup", time.Hour, s.cleanup)
}

// AuthorizeTopic owns files:<uuid>: an org id admits its members (top-level
// shared folders), an item id anyone who can view the item.
func (s *Service) AuthorizeTopic(ctx context.Context, userID uuid.UUID, _ bool, topic string) (bool, bool) {
	rest, ok := strings.CutPrefix(topic, topicKind+":")
	if !ok {
		return false, false
	}
	id, err := uuid.Parse(rest)
	if err != nil {
		return true, false
	}
	level, err := access.LevelOf(ctx, s.d.Pool, userID, Kind, id)
	if err != nil {
		return true, false
	}
	if level >= access.View {
		return true, true
	}
	role, err := orgs.RoleOf(ctx, s.d.Pool, id, userID)
	return true, err == nil && role != ""
}

// accessChanged drops realtime subscriptions to items people may have lost.
func (s *Service) accessChanged(ctx context.Context, ref access.Ref, users []uuid.UUID) {
	if ref.Kind != Kind || s.d.Broker == nil {
		return
	}
	topic := realtime.Topic(topicKind, ref.ID)
	if users == nil {
		_ = realtime.RevokeTopic(ctx, s.d.Broker, topic)
		return
	}
	for _, u := range users {
		_ = realtime.Revoke(ctx, s.d.Broker, u, topic)
	}
}

// changed tells open folder views about an item: its folder's topic (the
// owner's user topic or the org topic at the top level) and its own.
func (s *Service) changed(ctx context.Context, it *Item, action string) {
	if s.d.Broker == nil || it == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	data := map[string]any{"org_id": it.OrgID, "item_id": it.ID, "parent_id": it.ParentID, "action": action}
	topics := []string{realtime.Topic(topicKind, it.ID)}
	switch {
	case it.ParentID != nil:
		topics = append(topics, realtime.Topic(topicKind, *it.ParentID))
	case it.OwnerID != nil:
		topics = append(topics, realtime.UserTopic(*it.OwnerID))
	default:
		topics = append(topics, realtime.Topic(topicKind, it.OrgID))
	}
	for _, t := range topics {
		if err := s.d.Broker.Publish(ctx, t, EventChanged, data); err != nil {
			slog.WarnContext(ctx, "files: realtime publish failed", "err", err)
		}
	}
}

// refuseAgentCredential: endpoints outside the user routes never accept a
// credential minted for an AI agent, even where they need no session.
func refuseAgentCredential(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok && auth.IsAgentCredential(strings.TrimSpace(tok)) {
			httpx.Fail(w, r, errAgentCredential)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Robots-Tag", "noindex")
		next.ServeHTTP(w, r)
	})
}

// ---- background jobs -----------------------------------------------------------

// deleteQueuedObjects drains files_object_deletions (filled by triggers).
func (s *Service) deleteQueuedObjects(ctx context.Context) error {
	rows, err := s.d.Pool.Query(ctx, `
		SELECT storage_key FROM files_object_deletions WHERE attempts < 20
		ORDER BY created_at LIMIT 200`)
	if err != nil {
		return err
	}
	keys, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	for _, k := range keys {
		if err := s.d.Storage.Delete(ctx, k); err != nil {
			_, _ = s.d.Pool.Exec(ctx, `UPDATE files_object_deletions SET attempts = attempts + 1, last_error = $2 WHERE storage_key = $1`,
				k, err.Error())
			continue
		}
		_, _ = s.d.Pool.Exec(ctx, `DELETE FROM files_object_deletions WHERE storage_key = $1`, k)
	}
	return nil
}

// purgeExpired deletes trash whose time is up (FILES_TRASH_DAYS, or the
// keep period of a deleted account's private items).
func (s *Service) purgeExpired(ctx context.Context) error {
	rows, err := s.d.Pool.Query(ctx, `
		SELECT id FROM files_items WHERE trash_root = id AND purge_after < now() ORDER BY purge_after LIMIT 100`)
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error { return s.purge(ctx, tx, id) }); err != nil {
			slog.WarnContext(ctx, "files: purge failed", "item_id", id, "err", err)
		}
	}
	if len(ids) > 0 {
		slog.InfoContext(ctx, "files: purged trash", "items", len(ids))
	}
	return nil
}

// cleanup drops abandoned uploads (their objects are queued by trigger),
// finished upload rows, expired zip tokens and old recents.
func (s *Service) cleanup(ctx context.Context) error {
	for _, q := range []string{
		`DELETE FROM files_uploads WHERE completed_at IS NULL AND expires_at < now()`,
		`DELETE FROM files_uploads WHERE completed_at < now() - interval '1 day'`,
		`DELETE FROM files_archives WHERE expires_at < now()`,
		`DELETE FROM files_connected_downloads WHERE expires_at < now()`,
		`DELETE FROM files_recents WHERE at < now() - interval '90 days'`,
	} {
		if _, err := s.d.Pool.Exec(ctx, q); err != nil {
			return err
		}
	}
	return nil
}
