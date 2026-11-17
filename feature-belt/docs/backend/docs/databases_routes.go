package docs

import (
	"context"
	"encoding/json"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/httprate"
	"github.com/google/uuid"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/jobs"
	"scaffoldapp/backend/internal/notifications"
	"scaffoldapp/backend/internal/notify"
	"scaffoldapp/backend/internal/orgs"
)

const (
	// KindForm is the access resource kind of a form view (parent: its
	// database), so a form can be shared in the workspace or by public link.
	KindForm = "docs.form"

	// NotifyAutomation is the notification kind of automations' notify
	// steps. Row date reminders use NotifyReminder (reminders.go).
	NotifyAutomation = "docs.automation"
)

// initDatabases registers the form kind and the notification kinds.
func (s *Service) initDatabases() {
	access.RegisterKind(access.Kind{
		Name:  KindForm,
		Label: "form",
		Title: func(ctx context.Context, q db.Querier, id uuid.UUID) (string, error) {
			var name, title string
			err := q.QueryRow(ctx, `SELECT v.name, d.title FROM docs_views v JOIN docs_pages d ON d.id = v.database_id WHERE v.id = $1`, id).Scan(&name, &title)
			if err != nil {
				return "", err
			}
			if f := s.formTitle(ctx, q, id); f != "" {
				return f, nil
			}
			return displayTitle(title) + " · " + name, nil
		},
		Link:           func(id uuid.UUID) string { return "/docs/forms/" + id.String() },
		PublicPath:     func(token string) string { return "/p/form/" + token },
		LinkPermission: PermPublish,
	})
	onRow := notifications.DataTarget(Kind, "page_id")
	notifications.RegisterKind(notifications.Kind{ID: NotifyAutomation, Label: "Database automations", Push: true, AgentTarget: onRow})
}

// publicFormLimit caps anonymous form reads and submissions per IP.
var (
	publicFormRead   = httprate.LimitBy(120, time.Minute, httpx.KeyByClientIP, httprate.WithLimitHandler(httpx.RateLimitedHandler))
	publicFormSubmit = httprate.LimitBy(10, time.Minute, httpx.KeyByClientIP, httprate.WithLimitHandler(httpx.RateLimitedHandler))
)

// mountDatabasesPublic: public forms (no account; agent credentials refused).
func (s *Service) mountDatabasesPublic(r chi.Router) {
	p := r.With(refuseAgentCredential)
	p.With(publicFormRead).Get("/docs/public/forms/{token}", httpx.Handler(s.publicForm))
	p.With(publicFormSubmit).Post("/docs/public/forms/{token}", httpx.Handler(s.publicFormSubmit))
}

func (s *Service) mountDatabases(r chi.Router) {
	h := httpx.Handler
	member := orgs.Require(orgs.RoleMember)
	w := r.With(auth.RequireActive)

	w.With(member, auth.RateLimit(300, time.Hour)).Post("/docs/orgs/{orgID}/databases", h(s.createDatabase))

	// Rows and values.
	r.Get("/docs/databases/{id}/rows", h(s.listRows))
	r.With(auth.RateLimit(120, time.Minute)).Get("/docs/databases/{id}/chart", h(s.chart))
	w.With(auth.RateLimit(300, time.Minute)).Post("/docs/databases/{id}/rows", h(s.createRow))
	w.With(auth.RateLimit(600, time.Minute)).Patch("/docs/pages/{id}/properties", h(s.setRowValues))
	w.With(auth.RateLimit(120, time.Minute)).Post("/docs/pages/{id}/buttons/{propID}", h(s.pressButton))

	// Properties and views.
	w.With(auth.RateLimit(120, time.Minute)).Put("/docs/databases/{id}/schema", h(s.putSchema))
	w.With(auth.RateLimit(120, time.Minute)).Post("/docs/databases/{id}/formula", h(s.checkFormula))
	w.With(auth.RateLimit(120, time.Minute)).Post("/docs/databases/{id}/views", h(s.createView))
	w.With(auth.RateLimit(300, time.Minute)).Patch("/docs/views/{viewID}", h(s.updateView))
	w.With(auth.RateLimit(120, time.Minute)).Delete("/docs/views/{viewID}", h(s.deleteView))
	w.With(auth.RateLimit(120, time.Minute)).Post("/docs/pages/{id}/linked-views", h(s.createLinkedView))
	r.Get("/docs/pages/{id}/blocks/{blockID}/database", h(s.getBlockDatabase))

	// Templates.
	r.Get("/docs/databases/{id}/templates", h(s.listRowTemplates))
	w.With(auth.RateLimit(60, time.Minute)).Post("/docs/databases/{id}/templates", h(s.createRowTemplate))
	w.With(auth.RateLimit(60, time.Minute)).Patch("/docs/templates/{id}", h(s.updateRowTemplate))

	// Forms.
	r.Get("/docs/forms/{viewID}", h(s.getForm))
	w.With(auth.RateLimit(30, time.Minute)).Post("/docs/forms/{viewID}/submit", h(s.submitForm))

	// Automations.
	r.Get("/docs/databases/{id}/automations", h(s.listAutomations))
	w.With(auth.RateLimit(60, time.Hour)).Post("/docs/databases/{id}/automations", h(s.createAutomation))
	w.With(auth.RateLimit(120, time.Hour)).Patch("/docs/automations/{automationID}", h(s.updateAutomation))
	w.With(auth.RateLimit(60, time.Hour)).Delete("/docs/automations/{automationID}", h(s.deleteAutomation))
	r.Get("/docs/automations/{automationID}/runs", h(s.automationRuns))

	// CSV.
	r.With(auth.RateLimit(30, time.Minute)).Get("/docs/databases/{id}/csv", h(s.exportCSV))
	w.With(auth.RateLimit(60, time.Hour)).Post("/docs/databases/{id}/csv/preview", h(s.previewCSV))
	w.With(auth.RateLimit(20, time.Hour)).Post("/docs/databases/{id}/csv/import", h(s.importCSV))
}

// startDatabases runs reminders, repeating templates, scheduled
// automations and the clean-up of linked views and run logs.
func (s *Service) startDatabases(ctx context.Context) {
	go jobs.Every(ctx, s.d.Pool, "docs.row_reminders", time.Minute, s.sendRowReminders)
	go jobs.Every(ctx, s.d.Pool, "docs.repeating_templates", time.Minute, s.runRepeatingTemplates)
	go jobs.Every(ctx, s.d.Pool, "docs.automation_schedules", time.Minute, s.runScheduledAutomations)
	go jobs.Every(ctx, s.d.Pool, "docs.databases_cleanup", time.Hour, func(ctx context.Context) error {
		if err := s.sweepLinkedViews(ctx); err != nil {
			return err
		}
		_, err := s.d.Pool.Exec(ctx, `DELETE FROM docs_automation_runs WHERE created_at < now() - interval '30 days'`)
		return err
	})
}

// sendRowReminders notifies people whose date reminders are due, if they
// can still open the row.
func (s *Service) sendRowReminders(ctx context.Context) error {
	type due struct {
		RowID  uuid.UUID
		Prop   string
		UserID uuid.UUID
		Label  string
	}
	rows, err := s.d.Pool.Query(ctx, `DELETE FROM docs_row_reminders WHERE (row_id, property_id) IN (
		SELECT row_id, property_id FROM docs_row_reminders WHERE fire_at <= now() ORDER BY fire_at LIMIT 200 FOR UPDATE SKIP LOCKED)
		RETURNING row_id, property_id, user_id, label`)
	if err != nil {
		return err
	}
	var list []due
	for rows.Next() {
		var d due
		if err := rows.Scan(&d.RowID, &d.Prop, &d.UserID, &d.Label); err != nil {
			rows.Close()
			return err
		}
		list = append(list, d)
	}
	rows.Close()
	for _, d := range list {
		if !s.canView(ctx, d.UserID, d.RowID) {
			continue
		}
		var title, dbTitle string
		var schemaRaw []byte
		if err := s.d.Pool.QueryRow(ctx, `SELECT r.title, d.title, d.schema FROM docs_pages r JOIN docs_pages d ON d.id = r.parent_id WHERE r.id = $1 AND r.deleted_at IS NULL`, d.RowID).Scan(&title, &dbTitle, &schemaRaw); err != nil {
			continue
		}
		prop := ""
		var schema []Property
		if json.Unmarshal(schemaRaw, &schema) == nil {
			if p, ok := findProp(schema, d.Prop); ok {
				prop = p.Name
			}
		}
		body := prop + ": " + d.Label
		s.send(ctx, notify.Notification{UserID: d.UserID, Kind: NotifyReminder, Title: displayTitle(title), Body: body,
			Link: "/docs/" + d.RowID.String(), Data: map[string]any{"page_id": d.RowID, "database": displayTitle(dbTitle)}})
	}
	return nil
}
