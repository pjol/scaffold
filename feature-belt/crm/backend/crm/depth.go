package crm

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/httprate"
	"github.com/google/uuid"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
)

// mountDepth mounts the 1.3 routes (email, meetings, reports, workflows,
// forms, scoring). org: members with crm.access; orgWrite adds the write
// limit; write: record routes with the write limit.
func (s *Service) mountDepth(r, org, orgWrite, write chi.Router) {
	h := httpx.Handler
	settings := org.With(auth.RequireActive, access.RequirePermission(PermManageSettings), auth.RateLimit(settingsPerHour, time.Hour))

	// Email through connected mailboxes, templates, drafts, meetings.
	sends := r.With(auth.RequireActive, auth.RateLimit(mailSendsPerHour, time.Hour))
	logs := r.With(auth.RequireActive, auth.RateLimit(mailLogsPerHour, time.Hour))
	r.Get("/crm/mail/accounts", h(s.getMailAccounts))
	r.Get("/crm/mail/booking-links", h(s.getBookingLinks))
	write.Post("/crm/mail/render", h(s.renderMail))
	sends.Post("/crm/contacts/{id}/mail", h(s.sendMail))
	r.With(auth.RateLimit(mailLogsPerHour, time.Hour)).Get("/crm/contacts/{id}/mail/recent", h(s.recentMail))
	logs.Post("/crm/contacts/{id}/mail/log", h(s.logMail))
	write.Post("/crm/contacts/{id}/mail/drafts", h(s.createDraft))
	write.Patch("/crm/mail/drafts/{draftID}", h(s.updateDraft))
	write.Delete("/crm/mail/drafts/{draftID}", h(s.deleteDraft))
	org.Get("/crm/orgs/{orgID}/mail/drafts", h(s.listDrafts))
	org.Get("/crm/orgs/{orgID}/mail/templates", h(s.listTemplates))
	orgWrite.Post("/crm/orgs/{orgID}/mail/templates", h(s.createTemplate))
	orgWrite.Patch("/crm/orgs/{orgID}/mail/templates/{templateID}", h(s.updateTemplate))
	orgWrite.Delete("/crm/orgs/{orgID}/mail/templates/{templateID}", h(s.deleteTemplate))
	r.Get("/crm/contacts/{id}/meetings", h(s.meetingCandidates))
	write.Post("/crm/contacts/{id}/meetings/log", h(s.logMeeting))

	// Reports, dashboards, forecast, goals.
	org.With(auth.RateLimit(reportsPerHour, time.Hour)).Post("/crm/orgs/{orgID}/reports/run", h(s.runReportRoute))
	org.Get("/crm/orgs/{orgID}/dashboards", h(s.listDashboards))
	orgWrite.Post("/crm/orgs/{orgID}/dashboards", h(s.createDashboard))
	r.With(auth.RateLimit(reportsPerHour, time.Hour)).Get("/crm/dashboards/{dashboardID}", h(s.getDashboard))
	write.Patch("/crm/dashboards/{dashboardID}", h(s.updateDashboard))
	write.Delete("/crm/dashboards/{dashboardID}", h(s.deleteDashboard))
	write.Post("/crm/dashboards/{dashboardID}/reports", h(s.createReport))
	write.Patch("/crm/dashboards/{dashboardID}/reports/{reportID}", h(s.updateReport))
	write.Delete("/crm/dashboards/{dashboardID}/reports/{reportID}", h(s.deleteReport))
	org.With(auth.RateLimit(reportsPerHour, time.Hour)).Get("/crm/orgs/{orgID}/forecast", h(s.getForecast))
	org.Get("/crm/orgs/{orgID}/goals", h(s.listGoals))
	settings.Post("/crm/orgs/{orgID}/goals", h(s.createGoal))
	settings.Patch("/crm/orgs/{orgID}/goals/{goalID}", h(s.updateGoal))
	settings.Delete("/crm/orgs/{orgID}/goals/{goalID}", h(s.deleteGoal))

	// Lead scoring.
	org.Get("/crm/orgs/{orgID}/score-rules", h(s.listScoreRules))
	settings.Post("/crm/orgs/{orgID}/score-rules", h(s.saveScoreRule))
	settings.Patch("/crm/orgs/{orgID}/score-rules/{ruleID}", h(s.saveScoreRule))
	settings.Delete("/crm/orgs/{orgID}/score-rules/{ruleID}", h(s.deleteScoreRule))

	// Workflows.
	flows := org.With(auth.RequireActive, access.RequirePermission(PermWorkflows), auth.RateLimit(settingsPerHour, time.Hour))
	org.Get("/crm/orgs/{orgID}/workflows", h(s.listWorkflows))
	org.Get("/crm/orgs/{orgID}/workflows/{workflowID}", h(s.getWorkflow))
	org.With(access.RequirePermission(PermWorkflows)).Get("/crm/orgs/{orgID}/workflows/{workflowID}/runs", h(s.listWorkflowRuns))
	flows.Post("/crm/orgs/{orgID}/workflows", h(s.createWorkflow))
	flows.Patch("/crm/orgs/{orgID}/workflows/{workflowID}", h(s.updateWorkflow))
	flows.Delete("/crm/orgs/{orgID}/workflows/{workflowID}", h(s.deleteWorkflow))

	// Forms.
	forms := org.With(auth.RequireActive, access.RequirePermission(PermForms), auth.RateLimit(settingsPerHour, time.Hour))
	org.Get("/crm/orgs/{orgID}/forms", h(s.listForms))
	org.Get("/crm/orgs/{orgID}/forms/{formID}", h(s.getForm))
	org.With(access.RequirePermission(PermForms)).Get("/crm/orgs/{orgID}/forms/{formID}/submissions", h(s.listSubmissions))
	forms.Post("/crm/orgs/{orgID}/forms", h(s.createForm))
	forms.Patch("/crm/orgs/{orgID}/forms/{formID}", h(s.updateForm))
	forms.Delete("/crm/orgs/{orgID}/forms/{formID}", h(s.deleteForm))
	forms.Post("/crm/orgs/{orgID}/forms/{formID}/publish", h(s.publishForm(true)))
	forms.Post("/crm/orgs/{orgID}/forms/{formID}/unpublish", h(s.publishForm(false)))
}

const reportsPerHour = 1200

func chiParam(r *http.Request, name string) string { return chi.URLParam(r, name) }

// MountPublic serves the public quote page and lead forms. None takes an AI
// agent's credential; each is rate-limited by IP.
func (s *Service) MountPublic(r chi.Router) {
	r = r.With(refuseAgentCredential)
	byIP := func(n int, window time.Duration) func(http.Handler) http.Handler {
		return httprate.LimitBy(n, window, httpx.KeyByClientIP, httprate.WithLimitHandler(httpx.RateLimitedHandler))
	}
	r.With(byIP(60, time.Minute)).Get("/crm/public/quotes/{token}", httpx.Handler(s.publicQuote))
	r.With(byIP(20, time.Hour)).Post("/crm/public/quotes/{token}/accept", httpx.Handler(s.decideQuote(true)))
	r.With(byIP(20, time.Hour)).Post("/crm/public/quotes/{token}/decline", httpx.Handler(s.decideQuote(false)))
	byForm := httprate.LimitBy(300, time.Hour, func(r *http.Request) (string, error) {
		return "crm-form:" + chi.URLParam(r, "token"), nil
	}, httprate.WithLimitHandler(httpx.RateLimitedHandler))
	r.With(byIP(120, time.Minute)).Get("/crm/public/forms/{token}", httpx.Handler(s.publicForm))
	r.With(byIP(10, time.Hour), byForm).Post("/crm/public/forms/{token}", httpx.Handler(s.submitForm))
}

var errAgentCredential = httpx.NewError(http.StatusUnauthorized, "agent_credential",
	"This credential is for AI agents. It works only at the agent endpoint.")

// refuseAgentCredential stops agent credentials (agt_…) at the public routes:
// agents reach the CRM only through their tools.
func refuseAgentCredential(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok && auth.IsAgentCredential(strings.TrimSpace(tok)) {
			httpx.Fail(w, r, errAgentCredential)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// afterWrite runs after a record write commits: workflows enroll and the
// contact's lead score follows, in the background.
func (s *Service) afterWrite(ctx context.Context, orgID uuid.UUID, o objectType, id uuid.UUID, kind string, changes []change) {
	e := recordEvent{orgID: orgID, o: o, id: id, kind: kind, changes: changes}
	if o.pipelined() {
		for _, c := range changes {
			if c.Key == "stage" {
				var stage uuid.UUID
				if err := s.d.Pool.QueryRow(ctx, `SELECT stage_id FROM `+o.table()+` WHERE id = $1`, id).Scan(&stage); err == nil {
					e.stage = &stage
				}
			}
		}
		if kind == "created" {
			var stage uuid.UUID
			if err := s.d.Pool.QueryRow(ctx, `SELECT stage_id FROM `+o.table()+` WHERE id = $1`, id).Scan(&stage); err == nil {
				e.stage = &stage
			}
		}
	}
	s.background(ctx, "crm.after_write", func(ctx context.Context) error {
		if o == objContact {
			if err := s.rescoreContact(ctx, orgID, id); err != nil {
				return err
			}
		}
		return s.enroll(ctx, e)
	})
}

// EmailMeta is the mailbox metadata of an email sent from or logged out of a
// connected account.
type EmailMeta struct {
	Provider     string   `json:"provider"`
	ConnectionID string   `json:"connection_id,omitempty"`
	MessageID    string   `json:"message_id"`
	ThreadID     string   `json:"thread_id,omitempty"`
	From         string   `json:"from"`
	To           []string `json:"to"`
	CC           []string `json:"cc,omitempty"`
}
