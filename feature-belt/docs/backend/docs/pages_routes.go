package docs

import (
	"context"
	"time"

	"github.com/go-chi/chi/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/jobs"
	"scaffoldapp/backend/internal/notifications"
	"scaffoldapp/backend/internal/orgs"
	"scaffoldapp/backend/internal/storage"
)

// Pages and the workspace around them (docs 1.2.0): templates, imports,
// exports, history diffs and updates, following, analytics, verification,
// reactions, publish options, quick find and teamspaces. Wired from docs.go
// with one line per hook.

// initPages registers the import upload purpose and the edit notification.
func (s *Service) initPages() {
	s.cfg.importMax = int64(atLeast("DOCS_IMPORT_MAX_MB", config.Int("DOCS_IMPORT_MAX_MB", 50), 1)) << 20
	storage.RegisterPurpose(storage.Purpose{Name: ImportPurpose, Types: importTypes, MaxBytes: s.cfg.importMax})
	storage.RegisterPurpose(storage.Purpose{Name: ExportPurpose, AnyType: true, MaxBytes: 1}) // server-written only
	notifications.RegisterKind(notifications.Kind{ID: NotifyUpdate, Label: "Edits to pages you follow", Push: false, Email: true,
		AgentTarget: notifications.DataTarget(Kind, "page_id")})
}

// mountPages adds the signed-in routes (agent credentials never reach them:
// the core auth middleware refuses them outside /mcp).
func (s *Service) mountPages(r chi.Router) {
	h := httpx.Handler
	member := orgs.Require(orgs.RoleMember)
	w := r.With(auth.RequireActive)

	// Templates.
	r.With(member).Get("/docs/orgs/{orgID}/templates", h(s.listTemplates))
	r.With(member).Get("/docs/orgs/{orgID}/templates/{templateID}/preview", h(s.previewTemplate))
	w.With(member, auth.RateLimit(120, time.Hour)).Post("/docs/orgs/{orgID}/templates/{templateID}/use", h(s.useTemplate))
	w.With(auth.RateLimit(30, time.Hour)).Post("/docs/pages/{id}/template", h(s.saveAsTemplate))
	w.With(auth.RateLimit(60, time.Hour)).Delete("/docs/pages/{id}/template", h(s.stopTemplate))

	// Imports and exports.
	w.With(member, auth.RateLimit(30, time.Hour)).Post("/docs/orgs/{orgID}/imports", h(s.startImport))
	r.With(member).Get("/docs/orgs/{orgID}/imports", h(s.listImports))
	r.With(auth.RateLimit(240, time.Minute)).Get("/docs/imports/{importID}", h(s.getImport))
	r.With(auth.RateLimit(20, time.Minute)).Get("/docs/pages/{id}/export/{format}", h(s.export))

	// History: diffs and the updates feed.
	r.With(auth.RateLimit(120, time.Minute)).Get("/docs/history/{snapshotID}/diff", h(s.diffSnapshot))
	r.With(auth.RateLimit(120, time.Minute)).Get("/docs/pages/{id}/updates", h(s.updates))

	// Following, analytics, verification.
	r.With(auth.RateLimit(120, time.Minute)).Put("/docs/pages/{id}/follow", h(s.follow))
	r.With(auth.RateLimit(120, time.Minute)).Delete("/docs/pages/{id}/follow", h(s.unfollow))
	r.With(auth.RateLimit(60, time.Minute)).Get("/docs/pages/{id}/analytics", h(s.analytics))
	w.With(auth.RateLimit(60, time.Minute)).Put("/docs/pages/{id}/verification", h(s.verifyPage))
	w.With(auth.RateLimit(60, time.Minute)).Delete("/docs/pages/{id}/verification", h(s.unverifyPage))

	// Comment reactions.
	w.With(auth.RateLimit(120, time.Minute)).Put("/docs/comments/{commentID}/reactions", h(s.react))
	r.With(auth.RateLimit(120, time.Minute)).Delete("/docs/comments/{commentID}/reactions", h(s.unreact))

	// Publishing options and copying a published page.
	r.Get("/docs/pages/{id}/publish", h(s.getPublishSettings))
	w.With(auth.RateLimit(60, time.Minute)).Patch("/docs/pages/{id}/publish", h(s.putPublishSettings))
	w.With(auth.RateLimit(20, time.Hour)).Post("/docs/public/{token}/duplicate", h(s.duplicatePublic))

	// Quick find and teamspaces.
	r.With(member, auth.RateLimit(120, time.Minute)).Get("/docs/orgs/{orgID}/find", h(s.find))
	r.With(member).Put("/docs/orgs/{orgID}/teamspaces/{teamID}/hidden", h(s.hideTeamspace))
	r.With(member).Delete("/docs/orgs/{orgID}/teamspaces/{teamID}/hidden", h(s.showTeamspace))
}

// mountPagesPublic adds the public routes (behind refuseAgentCredential and
// the per-IP limit of MountPublic).
func (s *Service) mountPagesPublic(p chi.Router) {
	p.Get("/docs/public/{token}/meta", httpx.Handler(s.publicMeta))
}

// startPages runs import housekeeping.
func (s *Service) startPages(ctx context.Context) {
	go jobs.Every(ctx, s.d.Pool, "docs.imports", 5*time.Minute, s.cleanupImports)
}

// cleanupPages (with docs.cleanup) drops page views older than a year and
// downloaded exports.
func (s *Service) cleanupPages(ctx context.Context) error {
	if _, err := s.d.Pool.Exec(ctx, `DELETE FROM docs_page_views WHERE day < current_date - 366`); err != nil {
		return err
	}
	return s.cleanupExports(ctx)
}
