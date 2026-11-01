// Package access is the one permission model content features share: teams
// inside an org, a registry of shareable resources (features register kinds
// and write a row per resource), grants to people, teams and the whole org
// that inherit along parent links, public links, and org permissions with
// role defaults and team/person overrides.
//
// The effective level lives in one SQL function, access_level(user, kind, id),
// which Go (LevelOf, Require) and feature list queries both call, so the REST
// API, agent tools and search can't disagree. See FEATURE.md → Extending.
package access

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
	"github.com/jackc/pgx/v5/pgxpool"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/notifications"
	"scaffoldapp/backend/internal/notify"
	"scaffoldapp/backend/internal/orgs"
	"scaffoldapp/backend/internal/realtime"
	"scaffoldapp/backend/internal/storage"
)

// KindShared is the notification someone gets when a resource is shared with
// them or a team they're on (never for whole-org access).
const KindShared = "access.shared"

// Realtime events. access:<resource id> carries resource events to anyone who
// can view it; access:<org id> carries team and permission events to the org's
// members; user:<id> gets access.changed when a share touches that person.
const (
	EventChanged            = "access.changed"              // {kind, id, org_id}
	EventTeamsChanged       = "access.teams_changed"        // {org_id, team_id}
	EventTeamMembersChanged = "access.team_members_changed" // {org_id, team_id, user_id?}
	EventPermissionsChanged = "access.permissions_changed"  // {org_id, key}
)

// TeamAvatarPurpose is the public upload purpose for team pictures.
const TeamAvatarPurpose = "access_team_avatar"

// Built-in permissions (registered in New).
const (
	PermSharePublic = "access.share_public"
	PermManageTeams = "access.manage_teams"
)

const (
	maxDepth            = 32  // rows walked up a parent chain (the resource + 31 ancestors)
	maxGrantsPerRequest = 50  // subjects in one share request
	maxGrantsPerRes     = 500 // direct grants on one resource
	maxActiveLinks      = 20  // active public links on one resource
	maxTeamsPerOrg      = 500
	maxTeamSize         = 1000
	notifyTeamCap       = 200 // team shares notify at most this many members
	expandCap           = 1000

	shareWritesPerHour = 300
	grantAddsPerHour   = 120
	linksPerHour       = 30
	teamWritesPerHour  = 240
	permWritesPerHour  = 120
)

// Service is the access module.
type Service struct {
	d *app.Deps
}

// pool backs the package-level helpers that run without a Service
// (RequirePermission, Changed).
var pool atomic.Pointer[pgxpool.Pool]

var (
	broker   atomic.Value // realtime.Broker
	notifier atomic.Value // notify.Sender
)

// New registers the built-in permissions, the notification kind and the team
// picture purpose.
func New(d *app.Deps) *Service {
	pool.Store(d.Pool)
	setWebBase(d.Cfg)
	broker.Store(brokerBox{d.Broker})
	notifier.Store(senderBox{d.Notify})
	storage.RegisterPurpose(storage.Purpose{Name: TeamAvatarPurpose, Public: true, Types: storage.ImageTypes, MaxBytes: 5 << 20})
	notifications.RegisterKind(notifications.Kind{ID: KindShared, Label: "Shared with you", Push: true})
	notifications.RegisterKind(notifications.Kind{ID: KindTeam, Label: "Added to a team", Push: true})
	RegisterPermission(Permission{
		Key: PermSharePublic, Title: "Share with a public link",
		Info:         "Make links that open an item without an account. Turning this off stops every link this person or role made.",
		DefaultRoles: []string{orgs.RoleOwner, orgs.RoleAdmin, orgs.RoleMember},
	})
	RegisterPermission(Permission{
		Key: PermManageTeams, Title: "Manage teams",
		Info:         "Create, rename and delete teams and change anyone's team membership. Team leads manage their own team's members without it.",
		DefaultRoles: []string{orgs.RoleOwner, orgs.RoleAdmin},
	})
	httpx.RedactPathPrefix("/v1/access/links/")
	orgs.OnMemberRemoved(memberRemoved)
	orgs.OnRoleChanged(roleChanged)
	return &Service{d: d}
}

func (s *Service) Name() string { return "access" }

// publicLinkLimit caps anonymous link lookups per IP.
var publicLinkLimit = httprate.LimitBy(30, time.Minute, httpx.KeyByClientIP,
	httprate.WithLimitHandler(httpx.RateLimitedHandler))

func (s *Service) MountPublic(r chi.Router) {
	r.With(publicLinkLimit).Get("/access/links/{token}", httpx.Handler(s.resolveLinkRoute))
}

func (s *Service) MountUser(r chi.Router) {
	r = r.With(refuseAgentWrites)
	active := r.With(auth.RequireActive)
	member, admin := orgs.Require(orgs.RoleMember), orgs.Require(orgs.RoleAdmin)
	shareWrite := active.With(auth.RateLimit(shareWritesPerHour, time.Hour))

	// Directory for pickers.
	r.With(member).Get("/access/orgs/{orgID}/directory", httpx.Handler(s.directory))

	// Teams.
	r.With(member).Get("/access/orgs/{orgID}/teams", httpx.Handler(s.listTeams))
	active.With(member, RequirePermission(PermManageTeams), auth.RateLimit(teamWritesPerHour, time.Hour)).
		Post("/access/orgs/{orgID}/teams", httpx.Handler(s.createTeam))
	r.Get("/access/teams/{teamID}", httpx.Handler(s.getTeam))
	teamWrite := active.With(auth.RateLimit(teamWritesPerHour, time.Hour))
	teamWrite.Patch("/access/teams/{teamID}", httpx.Handler(s.updateTeam))
	teamWrite.Delete("/access/teams/{teamID}", httpx.Handler(s.deleteTeam))
	r.Get("/access/teams/{teamID}/members", httpx.Handler(s.listTeamMembers))
	teamWrite.Post("/access/teams/{teamID}/members", httpx.Handler(s.addTeamMembers))
	teamWrite.Patch("/access/teams/{teamID}/members/{userID}", httpx.Handler(s.updateTeamMember))
	r.With(auth.RateLimit(teamWritesPerHour, time.Hour)).Delete("/access/teams/{teamID}/members/{userID}", httpx.Handler(s.removeTeamMember))

	// Permissions.
	r.With(member).Get("/access/orgs/{orgID}/my-permissions", httpx.Handler(s.myPermissions))
	r.With(admin).Get("/access/orgs/{orgID}/permissions", httpx.Handler(s.listPermissions))
	permWrite := active.With(admin, auth.RateLimit(permWritesPerHour, time.Hour))
	permWrite.Put("/access/orgs/{orgID}/permissions/{key}", httpx.Handler(s.setPermission))
	permWrite.Delete("/access/orgs/{orgID}/permissions/{key}", httpx.Handler(s.resetPermission))

	// Sharing, over every registered kind.
	r.Get("/access/resources/{kind}/{id}", httpx.Handler(s.getSharing))
	shareWrite.Put("/access/resources/{kind}/{id}/general", httpx.Handler(s.setGeneral))
	shareWrite.Put("/access/resources/{kind}/{id}/inherit", httpx.Handler(s.setInherit))
	active.With(auth.RateLimit(grantAddsPerHour, time.Hour)).Post("/access/resources/{kind}/{id}/grants", httpx.Handler(s.addGrants))
	shareWrite.Patch("/access/resources/{kind}/{id}/grants/{grantID}", httpx.Handler(s.updateGrant))
	r.With(auth.RateLimit(shareWritesPerHour, time.Hour)).Delete("/access/resources/{kind}/{id}/grants/{grantID}", httpx.Handler(s.removeGrant))
	shareWrite.Put("/access/resources/{kind}/{id}/owner", httpx.Handler(s.transferOwner))
	active.With(auth.RateLimit(linksPerHour, time.Hour)).Post("/access/resources/{kind}/{id}/links", httpx.Handler(s.createLink))
	shareWrite.Delete("/access/resources/{kind}/{id}/links/{linkID}", httpx.Handler(s.revokeLink))
}

// refuseAgentWrites answers 403 agent_forbidden to every access write made
// as an AI agent: sharing, grants, links, teams and permissions.
func refuseAgentWrites(next http.Handler) http.Handler {
	guarded := refuseAgents(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}
		guarded.ServeHTTP(w, r)
	})
}

// AuthorizeTopic owns access:<uuid>. An org id admits its members (team and
// permission events); a resource id admits anyone who can view it.
func (s *Service) AuthorizeTopic(ctx context.Context, userID uuid.UUID, _ bool, topic string) (bool, bool) {
	rest, ok := strings.CutPrefix(topic, "access:")
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
	// A resource id: anyone who can view it (levelOf applies kind permissions).
	rows, err := s.d.Pool.Query(ctx, `SELECT kind FROM access_resources WHERE id = $1`, id)
	if err != nil {
		return true, false
	}
	kinds, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return true, false
	}
	for _, k := range kinds {
		if l, err := levelOf(ctx, s.d.Pool, userID, k, id, false); err == nil && l >= View {
			return true, true
		}
	}
	return true, false
}

// ---- side effects ------------------------------------------------------------

// brokerBox and senderBox give atomic.Value one concrete type to hold.
type (
	brokerBox struct{ realtime.Broker }
	senderBox struct{ notify.Sender }
)

func publish(ctx context.Context, topic, event string, data any) {
	b, _ := broker.Load().(brokerBox)
	if b.Broker == nil {
		return
	}
	if err := b.Publish(ctx, topic, event, data); err != nil {
		slog.WarnContext(ctx, "access: realtime publish failed", "event", event, "err", err)
	}
}

func send(ctx context.Context, n notify.Notification) {
	ns, _ := notifier.Load().(senderBox)
	if ns.Sender == nil {
		return
	}
	if err := ns.Send(ctx, n); err != nil {
		slog.WarnContext(ctx, "access: notify failed", "kind", n.Kind, "user_id", n.UserID, "err", err)
	}
}

func orgTopic(orgID uuid.UUID) string { return realtime.Topic("access", orgID) }

func resourceTopic(id uuid.UUID) string { return realtime.Topic("access", id) }

// resourceChanged tells open share dialogs and the affected people.
func resourceChanged(ctx context.Context, ref Ref, orgID *uuid.UUID, users []uuid.UUID) {
	data := map[string]any{"kind": ref.Kind, "id": ref.ID, "org_id": orgID}
	publish(ctx, resourceTopic(ref.ID), EventChanged, data)
	for _, u := range users {
		publish(ctx, realtime.UserTopic(u), EventChanged, data)
	}
}

type cursor struct {
	T  time.Time `json:"t"`
	ID uuid.UUID `json:"id"`
}

func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
