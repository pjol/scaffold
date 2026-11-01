// Package orgs adds organizations: memberships with roles (owner, admin,
// member), email invites and shareable invite links, joining through a
// verified email domain, and the org-scoped guard other features use
// (RoleOf, Require). Every org always has at least one owner.
package orgs

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/jobs"
	"scaffoldapp/backend/internal/notifications"
	"scaffoldapp/backend/internal/notify"
	"scaffoldapp/backend/internal/realtime"
	"scaffoldapp/backend/internal/storage"
)

// Roles, lowest to highest.
const (
	RoleMember = "member"
	RoleAdmin  = "admin"
	RoleOwner  = "owner"
)

// Notification kinds.
const (
	KindInvite      = "orgs.invite"       // you were invited (existing account with the invited email)
	KindRole        = "orgs.role"         // your role changed, or you were removed
	KindJoinRequest = "orgs.join_request" // admins: someone asked to join; requester: the answer
)

// Realtime events on orgs:<id> (members subscribe), and on user:<id> for the
// person whose memberships changed.
const (
	EventUpdated           = "orgs.updated"
	EventDeleted           = "orgs.deleted"
	EventMemberJoined      = "orgs.member_joined"
	EventMemberUpdated     = "orgs.member_updated"
	EventMemberRemoved     = "orgs.member_removed"
	EventMembershipChanged = "orgs.membership_changed" // user topic: refetch your org list
)

// AvatarPurpose is the public upload purpose for org pictures.
const AvatarPurpose = "org_avatar"

const (
	inviteTTL         = 7 * 24 * time.Hour
	invitesPerHour    = 20
	maxPendingInvites = 50
	maxActiveLinks    = 20
	resendCooldown    = time.Minute
	createsPerHour    = 10
)

type Service struct {
	d             *app.Deps
	resolver      *net.Resolver   // DNS for domain verification
	publicDomains map[string]bool // email providers no org may claim
	// autoJoinLimited is autoJoinWork behind its rate limit: autoJoin only
	// reaches it when there is something to join.
	autoJoinLimited http.Handler
}

// pool backs Require, which has no Service to hang off.
var pool atomic.Pointer[pgxpool.Pool]

func New(d *app.Deps) *Service {
	pool.Store(d.Pool)
	storage.RegisterPurpose(storage.Purpose{Name: AvatarPurpose, Public: true, Types: storage.ImageTypes, MaxBytes: 5 << 20})
	notifications.RegisterKind(notifications.Kind{ID: KindInvite, Label: NounTitle() + " invites", Push: true})
	notifications.RegisterKind(notifications.Kind{ID: KindRole, Label: NounTitle() + " roles", Push: true})
	notifications.RegisterKind(notifications.Kind{ID: KindJoinRequest, Label: "Join requests", Push: true})

	resolverAddr := config.String("ORGS_DNS_RESOLVER", "")
	if resolverAddr != "" {
		if _, _, err := net.SplitHostPort(resolverAddr); err != nil {
			config.Problem("ORGS_DNS_RESOLVER must be host:port (got %q)", resolverAddr)
			resolverAddr = ""
		}
	}
	public := map[string]bool{}
	for _, d := range publicEmailDomains {
		public[d] = true
	}
	for _, d := range config.List("ORGS_PUBLIC_EMAIL_DOMAINS", "") {
		public[strings.TrimPrefix(strings.ToLower(d), "@")] = true
	}
	s := &Service{d: d, resolver: newResolver(resolverAddr), publicDomains: public}
	s.autoJoinLimited = auth.RateLimit(autoJoinsPerHour, time.Hour)(httpx.Handler(s.autoJoinWork))
	return s
}

func (s *Service) Name() string { return "orgs" }

func (s *Service) MountPublic(r chi.Router) {
	r.With(publicPreviewLimit).Get("/orgs/invites/{token}", httpx.Handler(s.previewInvite))
}

func (s *Service) MountUser(r chi.Router) {
	active := r.With(auth.RequireActive)
	r.Get("/orgs", httpx.Handler(s.listMine))
	active.With(auth.RateLimit(createsPerHour, time.Hour)).Post("/orgs", httpx.Handler(s.createOrg))
	r.Get("/orgs/my-invites", httpx.Handler(s.listMyInvites))
	active.Post("/orgs/my-invites/{id}/accept", httpx.Handler(s.acceptMyInvite))
	r.Post("/orgs/my-invites/{id}/decline", httpx.Handler(s.declineMyInvite))
	active.Post("/orgs/invites/{token}/accept", httpx.Handler(s.acceptToken))

	// Joining through a verified email domain (domains.go, joining.go).
	r.Get("/orgs/joinable", httpx.Handler(s.listJoinable))
	active.Post("/orgs/auto-join", httpx.Handler(s.autoJoin)) // rate-limited only when it joins (autoJoinLimited)
	active.With(auth.RateLimit(domainJoinsPerHour, time.Hour)).Post("/orgs/{orgID}/join", httpx.Handler(s.joinByDomain))
	active.With(auth.RateLimit(joinRequestsPerHour, time.Hour)).Post("/orgs/{orgID}/join-requests", httpx.Handler(s.createJoinRequest))
	r.Delete("/orgs/my-join-requests/{id}", httpx.Handler(s.cancelMyJoinRequest))

	member, admin, owner := Require(RoleMember), Require(RoleAdmin), Require(RoleOwner)
	r.With(member).Get("/orgs/{orgID}", httpx.Handler(s.getOrg))
	active.With(admin).Patch("/orgs/{orgID}", httpx.Handler(s.updateOrg))
	active.With(owner).Delete("/orgs/{orgID}", httpx.Handler(s.deleteOrg))

	r.With(member).Get("/orgs/{orgID}/members", httpx.Handler(s.listMembers))
	active.With(admin).Patch("/orgs/{orgID}/members/{userID}", httpx.Handler(s.updateMember))
	r.With(member).Delete("/orgs/{orgID}/members/{userID}", httpx.Handler(s.removeMember))
	active.With(owner).Post("/orgs/{orgID}/transfer", httpx.Handler(s.transferOwnership))

	r.With(admin).Get("/orgs/{orgID}/invites", httpx.Handler(s.listInvites))
	active.With(admin, auth.RateLimit(invitesPerHour, time.Hour)).Post("/orgs/{orgID}/invites", httpx.Handler(s.createInvite))
	active.With(admin, auth.RateLimit(invitesPerHour, time.Hour)).Post("/orgs/{orgID}/invites/{inviteID}/resend", httpx.Handler(s.resendInvite))
	active.With(admin).Delete("/orgs/{orgID}/invites/{inviteID}", httpx.Handler(s.revokeInvite))

	r.With(admin).Get("/orgs/{orgID}/links", httpx.Handler(s.listLinks))
	active.With(admin, auth.RateLimit(invitesPerHour, time.Hour)).Post("/orgs/{orgID}/links", httpx.Handler(s.createLink))
	active.With(admin).Delete("/orgs/{orgID}/links/{linkID}", httpx.Handler(s.revokeLink))

	r.With(admin).Get("/orgs/{orgID}/domains", httpx.Handler(s.listDomains))
	active.With(admin, auth.RateLimit(domainAddsPerHour, time.Hour)).Post("/orgs/{orgID}/domains", httpx.Handler(s.addDomain))
	active.With(admin, auth.RateLimit(verifyChecksPerHour, time.Hour)).Post("/orgs/{orgID}/domains/{domainID}/verify", httpx.Handler(s.verifyDomain))
	active.With(admin).Patch("/orgs/{orgID}/domains/{domainID}", httpx.Handler(s.updateDomain))
	active.With(admin).Delete("/orgs/{orgID}/domains/{domainID}", httpx.Handler(s.removeDomain))

	r.With(admin).Get("/orgs/{orgID}/join-requests", httpx.Handler(s.listJoinRequests))
	active.With(admin).Post("/orgs/{orgID}/join-requests/{requestID}/approve", httpx.Handler(s.approveJoinRequest))
	active.With(admin).Post("/orgs/{orgID}/join-requests/{requestID}/deny", httpx.Handler(s.denyJoinRequest))
}

func (s *Service) MountAdmin(r chi.Router) {
	r.Get("/orgs", httpx.Handler(s.adminListOrgs))
	r.Get("/orgs/{orgID}", httpx.Handler(s.adminGetOrg))
	r.With(auth.RequireAdminRole("admin")).Delete("/orgs/{orgID}/domains/{domainID}", httpx.Handler(s.adminReleaseDomain))
}

func (s *Service) Start(ctx context.Context) {
	go jobs.Every(ctx, s.d.Pool, "orgs.expire_invites", time.Hour, func(ctx context.Context) error {
		_, err := s.d.Pool.Exec(ctx, `UPDATE org_invites SET status = 'expired', updated_at = now()
			WHERE status = 'pending' AND expires_at < now()`)
		return err
	})
}

// AuthorizeTopic lets members subscribe to orgs:<id>.
func (s *Service) AuthorizeTopic(ctx context.Context, userID uuid.UUID, _ bool, topic string) (bool, bool) {
	rest, ok := strings.CutPrefix(topic, "orgs:")
	if !ok {
		return false, false
	}
	orgID, err := uuid.Parse(rest)
	if err != nil {
		return true, false
	}
	role, err := RoleOf(ctx, s.d.Pool, orgID, userID)
	return true, err == nil && role != ""
}

// ---- roles and the org guard -------------------------------------------------

var rank = map[string]int{RoleMember: 1, RoleAdmin: 2, RoleOwner: 3}

// RoleAtLeast reports whether role is minRole or higher. "" (not a member)
// is never enough.
func RoleAtLeast(role, minRole string) bool { return rank[role] > 0 && rank[role] >= rank[minRole] }

// RoleOf returns userID's role in orgID: owner, admin, member, or "" when
// they are not a member (with a nil error).
func RoleOf(ctx context.Context, q db.Querier, orgID, userID uuid.UUID) (string, error) {
	var role string
	err := q.QueryRow(ctx, `SELECT role FROM org_members WHERE org_id = $1 AND user_id = $2`, orgID, userID).Scan(&role)
	if db.IsNoRows(err) {
		return "", nil
	}
	return role, err
}

// Membership is the caller's place in the org a route is scoped to.
type Membership struct {
	OrgID uuid.UUID
	Role  string
}

type ctxKey struct{}

// FromContext returns the membership Require resolved for this request.
func FromContext(ctx context.Context) (Membership, bool) {
	m, ok := ctx.Value(ctxKey{}).(Membership)
	return m, ok
}

var errOrgNotFound = httpx.NotFound("Organization not found.")

// refreshSentinels rewords the errors callers compare with errors.Is when
// SetNoun changes the word.
func refreshSentinels() {
	errOrgNotFound.Message = NounTitle() + " not found."
	errDomainTaken.Message = "That domain belongs to another " + Noun() + "."
}

// Require guards a route with an {orgID} URL param: the caller must be a
// member with at least minRole (member, admin or owner). Non-members get 404
// so org ids can't be probed; members without the role get 403. The
// membership is available to the handler through FromContext. Mount behind
// auth (UserRoutes).
func Require(minRole string) func(http.Handler) http.Handler {
	if rank[minRole] == 0 {
		panic("orgs: Require needs member, admin or owner")
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := auth.FromContext(r.Context())
			if id == nil {
				httpx.Fail(w, r, httpx.ErrUnauthenticated)
				return
			}
			orgID, err := uuid.Parse(chi.URLParam(r, "orgID"))
			if err != nil {
				httpx.Fail(w, r, errOrgNotFound)
				return
			}
			role, err := RoleOf(r.Context(), pool.Load(), orgID, id.UserID)
			switch {
			case err != nil:
				httpx.Fail(w, r, err)
				return
			case role == "":
				httpx.Fail(w, r, errOrgNotFound)
				return
			case !RoleAtLeast(role, minRole):
				httpx.Fail(w, r, httpx.Forbidden("Only "+minRole+"s of this "+Noun()+" can do that."))
				return
			}
			ctx := context.WithValue(r.Context(), ctxKey{}, Membership{OrgID: orgID, Role: role})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func mustMembership(r *http.Request) Membership {
	m, ok := FromContext(r.Context())
	if !ok {
		panic("orgs: handler not behind Require")
	}
	return m
}

// ---- side effects ------------------------------------------------------------

func (s *Service) send(ctx context.Context, n notify.Notification) {
	if err := s.d.Notify.Send(ctx, n); err != nil {
		slog.WarnContext(ctx, "orgs: notify failed", "kind", n.Kind, "user_id", n.UserID, "err", err)
	}
}

func (s *Service) publish(ctx context.Context, topic, event string, data any) {
	if err := s.d.Broker.Publish(ctx, topic, event, data); err != nil {
		slog.WarnContext(ctx, "orgs: realtime publish failed", "event", event, "err", err)
	}
}

func orgTopic(id uuid.UUID) string { return realtime.Topic("orgs", id) }

// membershipChanged tells the org's members and the affected person.
func (s *Service) membershipChanged(ctx context.Context, orgID, userID uuid.UUID, event string, data map[string]any) {
	data["org_id"], data["user_id"] = orgID, userID
	s.publish(ctx, orgTopic(orgID), event, data)
	s.publish(ctx, realtime.UserTopic(userID), EventMembershipChanged, map[string]any{"org_id": orgID})
	if event == EventMemberRemoved {
		if err := realtime.Revoke(ctx, s.d.Broker, userID, orgTopic(orgID)); err != nil {
			slog.WarnContext(ctx, "orgs: realtime revoke failed", "err", err)
		}
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
