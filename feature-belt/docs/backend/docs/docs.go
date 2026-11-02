// Package docs is Notion-style pages and databases inside an org: a tree of
// pages made of blocks, edited through block operations that are applied
// server-side and broadcast live, with presence, comments, history, trash,
// Markdown import/export and publishing to the web.
//
// Rules that hold everywhere in this package:
//   - Every page (databases and their rows included) is an access resource of
//     kind "docs.page" whose access parent is its parent page, written in the
//     same transaction as the page row. access_level() is the only permission
//     logic: routes call access.Require, lists filter with access_level or
//     JOIN access_visible, always passing access.ViaAgent(ctx), so REST, agent
//     tools and search can't disagree.
//   - Block edits run in one transaction per batch that locks the page row,
//     bumps docs_pages.version once and stamps every touched block with it;
//     clients reconcile with GET …/sync?since=<version>.
//   - Notifications and realtime events go out after commit and never fail
//     the request.
package docs

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/httprate"
	"github.com/google/uuid"

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
	// Kind is the access resource kind of every page.
	Kind = "docs.page"
	// FilePurpose is the private upload purpose for images, files and covers.
	FilePurpose = "docs_file"

	// Org permissions (access.RegisterPermission).
	PermCreateShared = "docs.create_shared"
	PermPublish      = "docs.publish"

	// Notification kinds.
	NotifyMention = "docs.mention"
	NotifyComment = "docs.comment"

	// Moderation targets.
	TargetPage    = "docs.page"
	TargetComment = "docs.comment"

	topicKind = "docs" // docs:<page id> (viewers), docs:<org id> (members: sidebar changes)

	// Realtime events on docs:<page id> unless noted.
	EventOps      = "docs.ops"      // {page_id, version, client_id, actor_id, ops}
	EventPage     = "docs.page"     // {page}: title, icon, cover, lock, width, row values
	EventPresence = "docs.presence" // {page_id, session_id, user, block_id, left}
	EventComments = "docs.comments" // {page_id, thread_id}
	EventReload   = "docs.reload"   // {page_id, version}: history restored, reload
	EventRows     = "docs.rows"     // {database_id}: rows added, changed or removed
	EventSchema   = "docs.schema"   // {database_id}: properties or views changed
	EventTree     = "docs.tree"     // on docs:<org id>: {org_id}; pages created, moved, renamed or trashed
)

// Service is the docs module.
type Service struct {
	d   *app.Deps
	cfg settings
}

type settings struct {
	fileMax          int64
	trashDays        int
	historyDays      int
	snapshotInterval time.Duration
}

// New registers the page kind, permissions, notification kinds, moderation
// targets and the upload purpose.
func New(d *app.Deps) *Service {
	s := &Service{d: d, cfg: settings{
		fileMax:          int64(atLeast("DOCS_FILE_MAX_MB", config.Int("DOCS_FILE_MAX_MB", 25), 1)) << 20,
		trashDays:        atLeast("DOCS_TRASH_DAYS", config.Int("DOCS_TRASH_DAYS", 30), 1),
		historyDays:      atLeast("DOCS_HISTORY_DAYS", config.Int("DOCS_HISTORY_DAYS", 90), 1),
		snapshotInterval: config.Duration("DOCS_SNAPSHOT_INTERVAL", 10*time.Minute),
	}}
	if s.cfg.snapshotInterval < time.Minute {
		config.Problem("DOCS_SNAPSHOT_INTERVAL must be at least 1m (got %s)", s.cfg.snapshotInterval)
		s.cfg.snapshotInterval = 10 * time.Minute
	}
	storage.RegisterPurpose(storage.Purpose{Name: FilePurpose, Types: fileTypes, MaxBytes: s.cfg.fileMax})
	access.RegisterKind(access.Kind{
		Name:       Kind,
		Label:      "page",
		Title:      func(ctx context.Context, q db.Querier, id uuid.UUID) (string, error) { return pageTitle(ctx, q, id) },
		Link:       func(id uuid.UUID) string { return "/docs/" + id.String() },
		PublicPath: func(token string) string { return "/p/" + token },
		// Publishing needs docs.publish besides access.share_public; access
		// refuses the link and stops it opening while the creator lacks it.
		LinkPermission: PermPublish,
	})
	access.RegisterPermission(access.Permission{
		Key: PermCreateShared, Title: "Add pages for everyone",
		Info:         "Create top-level pages everyone in the " + orgs.Noun() + " can open. Anyone can still make private pages and pages inside ones they can edit.",
		DefaultRoles: []string{orgs.RoleOwner, orgs.RoleAdmin, orgs.RoleMember},
	})
	access.RegisterPermission(access.Permission{
		Key: PermPublish, Title: "Publish pages to the web",
		Info:         "Publish a page with a public link anyone can read without an account. Taking this away stops the pages this person or role published.",
		DefaultRoles: []string{orgs.RoleOwner, orgs.RoleAdmin, orgs.RoleMember},
	})
	// An agent reading the inbox sees these only while it may read the page.
	onPage := notifications.DataTarget(Kind, "page_id")
	notifications.RegisterKind(notifications.Kind{ID: NotifyMention, Label: "Mentions in pages", Push: true, Email: true, AgentTarget: onPage})
	notifications.RegisterKind(notifications.Kind{ID: NotifyComment, Label: "Comments on pages", Push: true, AgentTarget: onPage})
	moderation.RegisterTarget(TargetPage, pageTarget{s})
	moderation.RegisterTarget(TargetComment, commentTarget{s})
	access.OnChange(func(ctx context.Context, ref access.Ref, users []uuid.UUID) {
		if ref.Kind != Kind {
			return
		}
		topic := realtime.Topic(topicKind, ref.ID)
		if users == nil {
			_ = realtime.RevokeTopic(ctx, d.Broker, topic)
			return
		}
		for _, u := range users {
			_ = realtime.Revoke(ctx, d.Broker, u, topic)
		}
	})
	httpx.RedactPathPrefix("/v1/docs/public/")
	return s
}

func atLeast(name string, v, minimum int) int {
	if v < minimum {
		config.Problem("%s must be at least %d (got %d)", name, minimum, v)
		return minimum
	}
	return v
}

func (s *Service) Name() string { return "docs" }

// publicLimit caps anonymous reads of published pages per IP.
var publicLimit = httprate.LimitBy(120, time.Minute, httpx.KeyByClientIP, httprate.WithLimitHandler(httpx.RateLimitedHandler))

func (s *Service) MountPublic(r chi.Router) {
	p := r.With(refuseAgentCredential, publicLimit)
	p.Get("/docs/public/{token}", httpx.Handler(s.publicPage))
	p.Get("/docs/public/{token}/pages/{id}", httpx.Handler(s.publicPage))
	p.Get("/docs/public/{token}/files/{fileID}", httpx.Handler(s.publicFile))
	p.Get("/docs/public/{token}/databases/{id}/rows", httpx.Handler(s.publicRows))
}

var errAgentCredential = httpx.NewError(http.StatusUnauthorized, "agent_credential", "This credential is for AI agents. It works only at the agent endpoint.")

// refuseAgentCredential: routes outside the user routes never accept a
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

func (s *Service) MountUser(r chi.Router) {
	h := httpx.Handler
	member := orgs.Require(orgs.RoleMember)
	w := r.With(auth.RequireActive)

	// Workspace lists.
	r.With(member).Get("/docs/orgs/{orgID}/sidebar", h(s.sidebar))
	r.With(member).Get("/docs/orgs/{orgID}/recent", h(s.recent))
	r.With(member).Get("/docs/orgs/{orgID}/trash", h(s.trash))
	r.With(member, auth.RateLimit(120, time.Minute)).Get("/docs/orgs/{orgID}/search", h(s.searchRoute))
	w.With(member, auth.RateLimit(300, time.Hour)).Post("/docs/orgs/{orgID}/pages", h(s.createPage))
	w.With(member, auth.RateLimit(30, time.Hour)).Post("/docs/orgs/{orgID}/import", h(s.importMarkdown))

	// Pages.
	r.Get("/docs/pages/{id}", h(s.getPage))
	r.Get("/docs/pages/{id}/children", h(s.children))
	r.Get("/docs/pages/{id}/sync", h(s.sync))
	r.Get("/docs/pages/{id}/backlinks", h(s.backlinks))
	r.With(auth.RateLimit(60, time.Minute)).Get("/docs/pages/{id}/export", h(s.exportMarkdown))
	w.With(auth.RateLimit(900, time.Minute)).Post("/docs/pages/{id}/ops", h(s.applyOps))
	w.With(auth.RateLimit(300, time.Minute)).Patch("/docs/pages/{id}", h(s.updatePage))
	w.With(auth.RateLimit(120, time.Minute)).Post("/docs/pages/{id}/move", h(s.movePage))
	w.With(auth.RateLimit(60, time.Hour)).Post("/docs/pages/{id}/duplicate", h(s.duplicatePage))
	w.With(auth.RateLimit(120, time.Minute)).Delete("/docs/pages/{id}", h(s.trashPage))
	w.With(auth.RateLimit(120, time.Minute)).Post("/docs/pages/{id}/restore", h(s.restorePage))
	w.With(auth.RateLimit(60, time.Minute)).Delete("/docs/pages/{id}/permanent", h(s.deletePermanently))
	r.Put("/docs/pages/{id}/favorite", h(s.favorite))
	r.Delete("/docs/pages/{id}/favorite", h(s.unfavorite))
	w.With(auth.RateLimit(240, time.Hour)).Post("/docs/pages/{id}/files", h(s.registerFile))
	r.Get("/docs/files/{fileID}", h(s.fileURL))

	// Presence (a viewer's own state: allowed while suspended).
	r.Get("/docs/pages/{id}/presence", h(s.listPresence))
	r.With(auth.RateLimit(240, time.Minute)).Post("/docs/pages/{id}/presence", h(s.beat))
	r.With(auth.RateLimit(240, time.Minute)).Delete("/docs/pages/{id}/presence/{session}", h(s.leave))

	// Comments.
	r.Get("/docs/pages/{id}/comments", h(s.listComments))
	w.With(auth.RateLimit(60, time.Minute)).Post("/docs/pages/{id}/comments", h(s.createComment))
	w.With(auth.RateLimit(60, time.Minute)).Patch("/docs/comments/{commentID}", h(s.editComment))
	w.With(auth.RateLimit(60, time.Minute)).Delete("/docs/comments/{commentID}", h(s.deleteComment))
	w.With(auth.RateLimit(120, time.Minute)).Post("/docs/comments/{commentID}/resolve", h(s.resolveComment))
	w.With(auth.RateLimit(120, time.Minute)).Delete("/docs/comments/{commentID}/resolve", h(s.reopenComment))

	// History.
	r.Get("/docs/pages/{id}/history", h(s.listHistory))
	r.Get("/docs/history/{snapshotID}", h(s.getSnapshot))
	w.With(auth.RateLimit(30, time.Minute)).Post("/docs/history/{snapshotID}/restore", h(s.restoreSnapshot))

	// Databases.
	r.Get("/docs/databases/{id}/rows", h(s.listRows))
	w.With(auth.RateLimit(300, time.Minute)).Post("/docs/databases/{id}/rows", h(s.createRow))
	w.With(auth.RateLimit(600, time.Minute)).Patch("/docs/pages/{id}/properties", h(s.setRowValues))
	w.With(auth.RateLimit(120, time.Minute)).Put("/docs/databases/{id}/schema", h(s.putSchema))
	w.With(auth.RateLimit(120, time.Minute)).Post("/docs/databases/{id}/views", h(s.createView))
	w.With(auth.RateLimit(300, time.Minute)).Patch("/docs/views/{viewID}", h(s.updateView))
	w.With(auth.RateLimit(120, time.Minute)).Delete("/docs/views/{viewID}", h(s.deleteView))

	// Connected Notion and Google Docs (connected_routes.go).
	s.mountConnected(r)
}

// Start runs trash and history clean-up and the snapshot taker.
func (s *Service) Start(ctx context.Context) {
	go jobs.Every(ctx, s.d.Pool, "docs.snapshots", time.Minute, s.takeSnapshots)
	go jobs.Every(ctx, s.d.Pool, "docs.cleanup", 15*time.Minute, s.cleanup)
}

// AuthorizeTopic owns docs:<uuid>. An org id admits its members (sidebar
// change pings carry no titles); a page id admits anyone who can view it.
func (s *Service) AuthorizeTopic(ctx context.Context, userID uuid.UUID, _ bool, topic string) (bool, bool) {
	rest, ok := strings.CutPrefix(topic, topicKind+":")
	if !ok {
		return false, false
	}
	id, err := uuid.Parse(rest)
	if err != nil {
		return true, false
	}
	role, err := orgs.RoleOf(ctx, s.d.Pool, id, userID)
	if err != nil {
		return true, false
	}
	if role != "" {
		return true, true
	}
	lvl, err := access.LevelOf(ctx, s.d.Pool, userID, Kind, id)
	return true, err == nil && lvl >= access.View
}

// ---- side effects ----------------------------------------------------------------

func (s *Service) publish(ctx context.Context, topic, event string, data any) {
	if s.d.Broker == nil {
		return
	}
	if err := s.d.Broker.Publish(context.WithoutCancel(ctx), topic, event, data); err != nil {
		slog.WarnContext(ctx, "docs: realtime publish failed", "event", event, "err", err)
	}
}

func pageTopic(id uuid.UUID) string { return realtime.Topic(topicKind, id) }

// treeChanged pings the org's members so open sidebars refetch.
func (s *Service) treeChanged(ctx context.Context, orgID uuid.UUID) {
	s.publish(ctx, realtime.Topic(topicKind, orgID), EventTree, map[string]any{"org_id": orgID})
}

// ---- errors ----------------------------------------------------------------------

var (
	errPageNotFound = httpx.NotFound("Page not found.")
	errLocked       = httpx.NewError(http.StatusConflict, "page_locked", "This page is locked. Choose Allow editing in its menu to change it.")
	errInTrash      = httpx.NewError(http.StatusConflict, "page_in_trash", "This page is in the trash. Restore it to edit.")
	errNotDatabase  = httpx.BadRequest("That page isn't a database.")
)
