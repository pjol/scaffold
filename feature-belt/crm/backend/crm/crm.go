// Package crm is a HubSpot-style CRM inside an org: companies, contacts and
// deals with owners, teams, tags and custom properties; pipelines with stages
// and stage history; activities (notes, calls, emails, meetings, tasks) on a
// timeline with mentions; saved list views, a deals board, a dashboard,
// duplicates and merge, CSV import and export.
//
// Every record is an access resource, and every list, count, total, export,
// search and agent tool reads through the same visible-records query
// (query.go), so nobody — and no agent acting for them — sees a record, or a
// total that includes it, unless access lets them open it.
package crm

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/jobs"
	"scaffoldapp/backend/internal/notifications"
	"scaffoldapp/backend/internal/notify"
	"scaffoldapp/backend/internal/orgs"
	"scaffoldapp/backend/internal/realtime"
)

// Org permissions (access.RegisterPermission in New).
const (
	PermAccess         = "crm.access"
	PermManageSettings = "crm.manage_settings"
	PermExport         = "crm.export"
	PermImport         = "crm.import"
	PermDelete         = "crm.delete"
)

// Access kinds. Records live under their team's crm.team resource, or the
// org's crm.workspace resource when they have no team.
const (
	KindWorkspace = "crm.workspace" // id = org id
	KindTeam      = "crm.team"      // id = access team id
	KindCompany   = "crm.company"
	KindContact   = "crm.contact"
	KindDeal      = "crm.deal"
)

// Notification kinds.
const (
	NotifyMention      = "crm.mention"
	NotifyTaskAssigned = "crm.task_assigned"
	NotifyTaskReminder = "crm.task_reminder"
	NotifyAssigned     = "crm.assigned"
)

// Realtime events. crm:<record id> carries record and timeline changes to
// anyone who can view the record; crm:<pipeline id> board changes; crm:<org
// id> settings, pipelines, properties, tags and shared views.
const (
	EventRecordChanged   = "crm.record_changed"   // {type, id}
	EventRecordDeleted   = "crm.record_deleted"   // {type, id}
	EventTimelineChanged = "crm.timeline_changed" // {type, id}
	EventBoardChanged    = "crm.board_changed"    // {pipeline_id}
	EventSettingsChanged = "crm.settings_changed" // {org_id, what}
)

const (
	writesPerHour   = 2000 // record and activity writes per person
	bulkPerHour     = 60
	mergesPerHour   = 60
	importsPerHour  = 10
	previewsPerHour = 60
	exportsPerHour  = 20
	settingsPerHour = 300
	maxBulkIDs      = 200
	maxNoteChars    = 20000
)

// Service is the crm module.
type Service struct {
	d *app.Deps
	// booted remembers orgs whose settings row, workspace resource and
	// default pipeline exist (bootstrap), so the check runs once per process.
	booted sync.Map
}

// New registers the access kinds, org permissions and notification kinds.
func New(d *app.Deps) *Service {
	s := &Service{d: d}
	// Every CRM kind needs PermAccess: without it access's sharing endpoint,
	// tools and LevelOf treat CRM resources as missing.
	access.RegisterKind(access.Kind{Name: KindWorkspace, Label: "CRM", AdminsManage: true, Permission: PermAccess,
		Title: func(ctx context.Context, q db.Querier, id uuid.UUID) (string, error) {
			var name string
			err := q.QueryRow(ctx, `SELECT name FROM orgs WHERE id = $1`, id).Scan(&name)
			return "Every CRM record in " + name, err
		},
		Link: func(uuid.UUID) string { return "/crm" },
	})
	access.RegisterKind(access.Kind{Name: KindTeam, Label: "team's CRM records", AdminsManage: true, Permission: PermAccess,
		Title: func(ctx context.Context, q db.Querier, id uuid.UUID) (string, error) {
			var name string
			err := q.QueryRow(ctx, `SELECT name FROM access_teams WHERE id = $1`, id).Scan(&name)
			if db.IsNoRows(err) {
				return "Records of a deleted team", nil
			}
			return name + " team's CRM records", err
		},
		Link: func(id uuid.UUID) string { return "/crm/deals?team=" + id.String() },
	})
	for _, o := range objectTypes {
		o := o
		access.RegisterKind(access.Kind{Name: o.kind(), Label: o.label(), AdminsManage: true, Permission: PermAccess,
			Title: func(ctx context.Context, q db.Querier, id uuid.UUID) (string, error) {
				return recordTitle(ctx, q, o, id)
			},
			Link: func(id uuid.UUID) string { return o.link(id) },
		})
	}
	member := []string{orgs.RoleOwner, orgs.RoleAdmin, orgs.RoleMember}
	admins := []string{orgs.RoleOwner, orgs.RoleAdmin}
	access.RegisterPermission(access.Permission{Key: PermAccess, Title: "Use the CRM", DefaultRoles: member,
		Info: "See the CRM at all: companies, contacts, deals, tasks and the dashboard. Which records each person sees still follows record access and sharing."})
	access.RegisterPermission(access.Permission{Key: PermManageSettings, Title: "Change CRM settings", DefaultRoles: admins,
		Info: "Edit pipelines and stages, custom properties, tags, record access and the default currency."})
	access.RegisterPermission(access.Permission{Key: PermExport, Title: "Export CRM records", DefaultRoles: admins,
		Info: "Download a list as CSV. Exports contain only the records the person can see."})
	access.RegisterPermission(access.Permission{Key: PermImport, Title: "Import CRM records", DefaultRoles: admins,
		Info: "Create and update companies, contacts and deals from a CSV file."})
	access.RegisterPermission(access.Permission{Key: PermDelete, Title: "Delete and merge CRM records", DefaultRoles: admins,
		Info: "Delete records (one at a time or in bulk) and merge duplicates. Needs edit access to each record too."})
	notifications.RegisterKind(notifications.Kind{ID: NotifyMention, Label: "CRM mentions", Push: true, AgentTarget: recordNotifyTarget})
	notifications.RegisterKind(notifications.Kind{ID: NotifyTaskAssigned, Label: "CRM tasks assigned to you", Push: true, AgentTarget: recordNotifyTarget})
	notifications.RegisterKind(notifications.Kind{ID: NotifyTaskReminder, Label: "CRM task reminders", Push: true, AgentTarget: recordNotifyTarget})
	notifications.RegisterKind(notifications.Kind{ID: NotifyAssigned, Label: "CRM records assigned to you", Push: true, AgentTarget: recordNotifyTarget})
	access.OnChange(s.onAccessChange)
	access.OnPermissionChange(s.permissionChanged)
	orgs.OnMemberRemoved(s.memberRemoved)
	return s
}

func (s *Service) Name() string { return "crm" }

// recordNotifyTarget is the record a CRM notification is about (its data's
// type and id), so an AI agent reading the person's inbox sees it only while
// it may read that record. Notifications without one (bulk assignments)
// follow the CRM's level as a whole.
func recordNotifyTarget(data map[string]any) (string, string) {
	t, _ := data["type"].(string)
	id, _ := data["id"].(string)
	for _, o := range objectTypes {
		if string(o) == t {
			return o.kind(), id
		}
	}
	return "", ""
}

func (s *Service) MountUser(r chi.Router) {
	org := r.With(orgs.Require(orgs.RoleMember), access.RequirePermission(PermAccess), s.bootstrapMiddleware)
	write := r.With(auth.RequireActive, auth.RateLimit(writesPerHour, time.Hour))
	orgWrite := org.With(auth.RequireActive, auth.RateLimit(writesPerHour, time.Hour))
	settings := org.With(auth.RequireActive, access.RequirePermission(PermManageSettings), auth.RateLimit(settingsPerHour, time.Hour))

	// Settings, pipelines, properties, tags.
	org.Get("/crm/orgs/{orgID}/settings", httpx.Handler(s.getSettings))
	settings.Put("/crm/orgs/{orgID}/settings", httpx.Handler(s.putSettings))
	org.Get("/crm/orgs/{orgID}/pipelines", httpx.Handler(s.xPipelines(s.listPipelines)))
	settings.Post("/crm/orgs/{orgID}/pipelines", httpx.Handler(s.createPipeline))
	settings.Patch("/crm/orgs/{orgID}/pipelines/{pipelineID}", httpx.Handler(s.updatePipeline))
	settings.Delete("/crm/orgs/{orgID}/pipelines/{pipelineID}", httpx.Handler(s.deletePipeline))
	org.Get("/crm/orgs/{orgID}/properties", httpx.Handler(s.xProperties(s.listProperties)))
	settings.Post("/crm/orgs/{orgID}/properties", httpx.Handler(s.createProperty))
	settings.Patch("/crm/orgs/{orgID}/properties/{propertyID}", httpx.Handler(s.updateProperty))
	settings.Delete("/crm/orgs/{orgID}/properties/{propertyID}", httpx.Handler(s.deleteProperty))
	org.Get("/crm/orgs/{orgID}/tags", httpx.Handler(s.listTags))
	orgWrite.Post("/crm/orgs/{orgID}/tags", httpx.Handler(s.createTag))
	settings.Patch("/crm/orgs/{orgID}/tags/{tagID}", httpx.Handler(s.updateTag))
	settings.Delete("/crm/orgs/{orgID}/tags/{tagID}", httpx.Handler(s.deleteTag))

	// Saved views.
	org.Get("/crm/orgs/{orgID}/views", httpx.Handler(s.listViews))
	orgWrite.Post("/crm/orgs/{orgID}/views", httpx.Handler(s.createView))
	orgWrite.Patch("/crm/orgs/{orgID}/views/{viewID}", httpx.Handler(s.updateView))
	orgWrite.Delete("/crm/orgs/{orgID}/views/{viewID}", httpx.Handler(s.deleteView))

	// Records.
	for _, o := range objectTypes {
		o := o
		org.Get("/crm/orgs/{orgID}/"+o.path(), httpx.Handler(s.xListRecords(o, s.listRecords(o))))
		orgWrite.Post("/crm/orgs/{orgID}/"+o.path(), httpx.Handler(s.xCreateRecord(o, s.createRecord(o))))
		r.Get("/crm/"+o.path()+"/{id}", httpx.Handler(s.xGetRecord(o, s.getRecord(o))))
		write.Patch("/crm/"+o.path()+"/{id}", httpx.Handler(s.xUpdateRecord(o, s.updateRecord(o))))
		write.Delete("/crm/"+o.path()+"/{id}", httpx.Handler(s.xNotHere("id", "Delete connected records on their platform.", s.deleteRecord(o))))
		r.Get("/crm/"+o.path()+"/{id}/timeline", httpx.Handler(s.xTimeline(o, s.timeline(o))))
		write.Post("/crm/"+o.path()+"/{id}/activities", httpx.Handler(s.xCreateActivity(o, s.createActivity(o))))
	}
	write.Post("/crm/deals/{id}/stage", httpx.Handler(s.xMoveStage(s.moveStage)))
	r.Get("/crm/deals/{id}/stages", httpx.Handler(s.xStageHistory(s.stageHistory)))

	// Associations.
	notHere := "Change a connected record's links on its platform."
	write.Post("/crm/contacts/{id}/companies", httpx.Handler(s.xNotHere("id", notHere, s.addContactCompany)))
	write.Put("/crm/contacts/{id}/companies/{companyID}/primary", httpx.Handler(s.xNotHere("id", notHere, s.setPrimaryCompany)))
	write.Delete("/crm/contacts/{id}/companies/{companyID}", httpx.Handler(s.xNotHere("id", notHere, s.removeContactCompany)))
	write.Post("/crm/deals/{id}/contacts", httpx.Handler(s.xNotHere("id", notHere, s.addDealContact)))
	write.Delete("/crm/deals/{id}/contacts/{contactID}", httpx.Handler(s.xNotHere("id", notHere, s.removeDealContact)))

	// Activities and tasks.
	write.Patch("/crm/activities/{activityID}", httpx.Handler(s.xUpdateActivity(s.updateActivity)))
	write.Delete("/crm/activities/{activityID}", httpx.Handler(s.xNotHere("activityID", "Delete connected activities on their platform.", s.deleteActivity)))
	org.Get("/crm/orgs/{orgID}/tasks", httpx.Handler(s.xTasks(s.listTasks)))
	org.Get("/crm/orgs/{orgID}/tasks/summary", httpx.Handler(s.xTaskSummary(s.taskSummary)))

	// Board, dashboard, search, duplicates.
	org.Get("/crm/orgs/{orgID}/board", httpx.Handler(s.xBoard(s.board)))
	org.Get("/crm/orgs/{orgID}/dashboard", httpx.Handler(s.xDashboard(s.dashboard)))
	org.Get("/crm/orgs/{orgID}/search", httpx.Handler(s.xSearch(s.searchRoute)))

	// Connected CRMs: sources, platform owners, import, company links.
	r.Get("/crm/sources", httpx.Handler(s.getSources))
	org.Get("/crm/orgs/{orgID}/sources/{sourceID}/owners", httpx.Handler(s.sourceOwners))
	org.With(auth.RequireActive, access.RequirePermission(PermImport), auth.RateLimit(importsPerHour*3, time.Hour)).
		Post("/crm/orgs/{orgID}/sources/{sourceID}/import", httpx.Handler(s.importFromSource))
	r.Get("/crm/companies/{id}/links", httpx.Handler(s.listLinks))
	write.Post("/crm/companies/{id}/links", httpx.Handler(s.createLink))
	write.Delete("/crm/companies/{id}/links/{linkID}", httpx.Handler(s.deleteLink))
	org.Get("/crm/orgs/{orgID}/duplicates", httpx.Handler(s.duplicates))

	// Bulk, merge, import, export.
	org.With(auth.RequireActive, auth.RateLimit(bulkPerHour, time.Hour)).Post("/crm/orgs/{orgID}/bulk", httpx.Handler(s.bulk))
	org.With(auth.RequireActive, access.RequirePermission(PermDelete), auth.RateLimit(mergesPerHour, time.Hour)).
		Post("/crm/orgs/{orgID}/merge", httpx.Handler(s.merge))
	importer := org.With(auth.RequireActive, access.RequirePermission(PermImport))
	importer.With(auth.RateLimit(previewsPerHour, time.Hour)).Post("/crm/orgs/{orgID}/imports/inspect", httpx.Handler(s.inspectImport))
	importer.With(auth.RateLimit(previewsPerHour, time.Hour)).Post("/crm/orgs/{orgID}/imports/preview", httpx.Handler(s.previewImport))
	importer.With(auth.RateLimit(importsPerHour, time.Hour)).Post("/crm/orgs/{orgID}/imports", httpx.Handler(s.runImport))
	org.With(access.RequirePermission(PermImport)).Get("/crm/orgs/{orgID}/imports", httpx.Handler(s.listImports))
	org.With(access.RequirePermission(PermImport)).Get("/crm/orgs/{orgID}/imports/{importID}", httpx.Handler(s.getImport))
	org.With(access.RequirePermission(PermExport), auth.RateLimit(exportsPerHour, time.Hour)).
		Get("/crm/orgs/{orgID}/export", httpx.Handler(s.export))
}

// Start runs the task reminder job (each reminder goes out once) and marks
// imports a restart interrupted as failed.
func (s *Service) Start(ctx context.Context) {
	go jobs.Every(ctx, s.d.Pool, "crm.task_reminders", time.Minute, s.sendReminders)
	go jobs.Every(ctx, s.d.Pool, "crm.stale_imports", 10*time.Minute, s.failStaleImports)
}

// AuthorizeTopic owns crm:<uuid>: an org id admits members with crm.access;
// a pipeline id members of its org with crm.access; a record id anyone who
// can view the record and holds crm.access.
func (s *Service) AuthorizeTopic(ctx context.Context, userID uuid.UUID, _ bool, topic string) (bool, bool) {
	rest, ok := strings.CutPrefix(topic, "crm:")
	if !ok {
		return false, false
	}
	id, err := uuid.Parse(rest)
	if err != nil {
		return true, false
	}
	var orgID uuid.UUID
	err = s.d.Pool.QueryRow(ctx, `
		SELECT id FROM orgs WHERE id = $1
		UNION ALL SELECT org_id FROM crm_pipelines WHERE id = $1
		LIMIT 1`, id).Scan(&orgID)
	if err == nil {
		allowed, err := access.Allowed(ctx, s.d.Pool, orgID, userID, PermAccess)
		return true, err == nil && allowed
	}
	if !db.IsNoRows(err) {
		return true, false
	}
	for _, o := range objectTypes {
		if lvl, err := access.LevelOf(ctx, s.d.Pool, userID, o.kind(), id); err == nil && lvl >= access.View {
			allowed, err := s.recordOrgAllowed(ctx, o, id, userID)
			return true, err == nil && allowed
		}
	}
	return true, false
}

// onAccessChange drops record subscriptions people may have lost.
func (s *Service) onAccessChange(ctx context.Context, ref access.Ref, users []uuid.UUID) {
	if _, ok := objectByKind(ref.Kind); !ok {
		return
	}
	topic := realtime.Topic("crm", ref.ID)
	if users == nil {
		_ = realtime.RevokeTopic(ctx, s.d.Broker, topic)
		return
	}
	for _, u := range users {
		_ = realtime.Revoke(ctx, s.d.Broker, u, topic)
	}
}

// accessLostRecordCap bounds the record topics re-checked when people may
// have lost crm.access (most recently updated first: the ones open in clients).
const accessLostRecordCap = 1000

// memberRemoved is crm's orgs.OnMemberRemoved hook. access's own hook already
// revokes the leaver's record topics (through OnChange); this drops the org
// and pipeline topics, which aren't access resources.
func (s *Service) memberRemoved(ctx context.Context, orgID, userID uuid.UUID) {
	s.revokeOrgTopics(ctx, orgID, []uuid.UUID{userID}, false)
}

// permissionChanged is crm's access.OnPermissionChange hook: people may have
// lost crm.access in orgID (an admin changed or reset it, they left a team
// that held it, their role changed; users nil: anyone in the org). Their CRM
// subscriptions are re-checked now: the org topic, every pipeline's and the
// most recently updated records'. The authorizer keeps whoever still holds it.
func (s *Service) permissionChanged(ctx context.Context, orgID uuid.UUID, key string, users []uuid.UUID) {
	if key != "" && key != PermAccess {
		return
	}
	s.revokeOrgTopics(ctx, orgID, users, true)
}

// revokeOrgTopics re-checks crm:<org> and each crm:<pipeline> in the org (and
// with records, the newest records' topics) for users, or for every
// subscriber when users is nil.
func (s *Service) revokeOrgTopics(ctx context.Context, orgID uuid.UUID, users []uuid.UUID, records bool) {
	ids := []uuid.UUID{orgID}
	q, args := `SELECT id FROM crm_pipelines WHERE org_id = $1`, []any{orgID}
	if records {
		for _, o := range objectTypes {
			q += ` UNION ALL (SELECT id FROM ` + o.table() + ` WHERE org_id = $1 ORDER BY updated_at DESC LIMIT $2)`
		}
		args = append(args, accessLostRecordCap)
	}
	rows, err := s.d.Pool.Query(ctx, q, args...)
	if err == nil {
		var more []uuid.UUID
		if more, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID]); err == nil {
			ids = append(ids, more...)
		}
	}
	if err != nil {
		slog.WarnContext(ctx, "crm: list topics to re-check", "org_id", orgID, "err", err)
	}
	for _, id := range ids {
		topic := realtime.Topic("crm", id)
		if users == nil {
			err = realtime.RevokeTopic(ctx, s.d.Broker, topic)
		} else {
			for _, u := range users {
				err = realtime.Revoke(ctx, s.d.Broker, u, topic)
			}
		}
		if err != nil {
			slog.WarnContext(ctx, "crm: realtime revoke failed", "org_id", orgID, "err", err)
		}
	}
}

// ---- side effects ------------------------------------------------------------

func (s *Service) publish(ctx context.Context, topic, event string, data any) {
	if err := s.d.Broker.Publish(context.WithoutCancel(ctx), topic, event, data); err != nil {
		slog.WarnContext(ctx, "crm: realtime publish failed", "event", event, "err", err)
	}
}

func (s *Service) recordChanged(ctx context.Context, o objectType, id uuid.UUID, timeline bool) {
	data := map[string]any{"type": o, "id": id}
	s.publish(ctx, realtime.Topic("crm", id), EventRecordChanged, data)
	if timeline {
		s.publish(ctx, realtime.Topic("crm", id), EventTimelineChanged, data)
	}
}

func (s *Service) boardChanged(ctx context.Context, pipelineIDs ...uuid.UUID) {
	seen := map[uuid.UUID]bool{}
	for _, p := range pipelineIDs {
		if p != uuid.Nil && !seen[p] {
			seen[p] = true
			s.publish(ctx, realtime.Topic("crm", p), EventBoardChanged, map[string]any{"pipeline_id": p})
		}
	}
}

func (s *Service) settingsChanged(ctx context.Context, orgID uuid.UUID, what string) {
	s.publish(ctx, realtime.Topic("crm", orgID), EventSettingsChanged, map[string]any{"org_id": orgID, "what": what})
}

func (s *Service) send(ctx context.Context, n notify.Notification) {
	if err := s.d.Notify.Send(context.WithoutCancel(ctx), n); err != nil {
		slog.WarnContext(ctx, "crm: notify failed", "kind", n.Kind, "user_id", n.UserID, "err", err)
	}
}

// agentName is the AI agent acting for the caller ("" for people), stored on
// events and activities so the timeline says "Sam (via Claude)".
func agentName(ctx context.Context) *string {
	a, ok := auth.AgentFrom(ctx)
	if !ok {
		return nil
	}
	name := strings.TrimSpace(a.Client)
	if name == "" {
		name = "AI agent"
	}
	return &name
}

// orgNounA starts a sentence with the app's word for an organization: "A workspace".
func orgNounA() string {
	a := orgs.NounA()
	return strings.ToUpper(a[:1]) + a[1:]
}
