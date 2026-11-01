package orgs

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/notify"
	"scaffoldapp/backend/internal/realtime"
)

// Joining through a verified email domain. Who qualifies is always worked out
// from the caller's verified account email; a client never names a domain.

// Event on orgs:<id> when its pending join requests change (admins refetch).
const EventJoinRequestsChanged = "orgs.join_requests_changed"

const (
	autoJoinsPerHour     = 60
	domainJoinsPerHour   = 20
	joinRequestsPerHour  = 10
	denyCooldown         = 7 * 24 * time.Hour // no new request after a denial
	requestNotifyWindow  = 24 * time.Hour     // admins hear once per person per day
	maxJoinRequestNotify = 50                 // admins notified per request
)

// JoinableOrg is an org the caller's email domain lets them join or ask to
// join, or one they already asked.
type JoinableOrg struct {
	Org         OrgRef     `json:"org"`
	MemberCount int        `json:"member_count"`
	Domain      string     `json:"domain"`
	Action      string     `json:"action"`     // join | request | requested
	RequestID   *uuid.UUID `json:"request_id"` // the pending request when action = requested
	RequestedAt *time.Time `json:"requested_at"`
}

// listJoinable: GET /v1/orgs/joinable. Empty for suspended accounts and for
// public email providers.
func (s *Service) listJoinable(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	list, err := s.joinable(r.Context(), me)
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.Page[JoinableOrg]{Items: list})
	return nil
}

func (s *Service) joinable(ctx context.Context, me *auth.Identity) ([]JoinableOrg, error) {
	out := []JoinableOrg{}
	if !me.Active() {
		return out, nil
	}
	// Orgs with a verified domain matching the caller's email, plus any org
	// they already asked (its domain may since have gone).
	rows, err := s.d.Pool.Query(ctx, `
		WITH c AS (
		  SELECT org_id FROM org_domains WHERE domain = $1 AND verified_at IS NOT NULL
		  UNION
		  SELECT org_id FROM org_join_requests WHERE user_id = $2 AND status = 'pending'
		)
		SELECT o.id, o.name, o.slug, o.avatar_key,
		       (SELECT count(*) FROM org_members x WHERE x.org_id = o.id)::int,
		       COALESCE(d.domain, jr.domain), COALESCE(d.policy, ''), COALESCE(dep.reason, ''), jr.id, jr.created_at
		FROM c JOIN orgs o ON o.id = c.org_id
		LEFT JOIN org_domains d ON d.org_id = o.id AND d.domain = $1 AND d.verified_at IS NOT NULL
		LEFT JOIN org_join_requests jr ON jr.org_id = o.id AND jr.user_id = $2 AND jr.status = 'pending'
		LEFT JOIN org_departures dep ON dep.org_id = o.id AND dep.user_id = $2
		WHERE NOT EXISTS (SELECT 1 FROM org_members m WHERE m.org_id = o.id AND m.user_id = $2)
		  AND NOT EXISTS (SELECT 1 FROM org_join_requests x WHERE x.org_id = o.id AND x.user_id = $2
		                  AND x.status = 'denied' AND x.decided_at > now() - make_interval(secs => $3))
		ORDER BY o.name, o.id LIMIT 50`, s.emailDomain(me.Email), me.UserID, denyCooldown.Seconds())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			j              JoinableOrg
			avatar         *string
			policy, reason string
		)
		if err := rows.Scan(&j.Org.ID, &j.Org.Name, &j.Org.Slug, &avatar, &j.MemberCount, &j.Domain, &policy, &reason,
			&j.RequestID, &j.RequestedAt); err != nil {
			return nil, err
		}
		j.Org.AvatarURL = s.avatarURL(avatar)
		switch {
		case j.RequestID != nil:
			j.Action = "requested"
		case policy == PolicyAutoJoin && reason != "removed":
			j.Action = "join"
		default:
			j.Action = "request"
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// AutoJoined is POST /v1/orgs/auto-join.
type AutoJoined struct {
	Joined []Org `json:"joined"`
}

// autoJoin: POST /v1/orgs/auto-join (active, consented). Joins every org
// whose verified auto_join domain matches the caller's email, except ones
// they left or were removed from. The web and mobile apps call it once per
// session when signed in; it is idempotent.
//
// Joining people without a click needs the domain proven through DNS: an
// admin's own mailbox on a domain (email verification) proves that mailbox,
// not the domain, so it lets people join from "Organizations you can join"
// but never enrols them silently. Development accepts either (.test can't be
// published in DNS).
//
// A call with nothing to join is one indexed read and answers {joined: []}
// at once; only calls that would join something count toward
// autoJoinsPerHour, so a legitimate repeat is never refused.
func (s *Service) autoJoin(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	domain := s.emailDomain(me.Email)
	if domain == "" || !me.Active() {
		httpx.OK(w, AutoJoined{Joined: []Org{}})
		return nil
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT d.org_id FROM org_domains d
		WHERE d.domain = $1 AND d.verified_at IS NOT NULL AND d.policy = 'auto_join'
		  AND (d.verify_method = 'dns' OR $4)
		  AND NOT EXISTS (SELECT 1 FROM org_members m WHERE m.org_id = d.org_id AND m.user_id = $2)
		  AND NOT EXISTS (SELECT 1 FROM org_departures x WHERE x.org_id = d.org_id AND x.user_id = $2)
		  AND NOT EXISTS (SELECT 1 FROM org_join_requests x WHERE x.org_id = d.org_id AND x.user_id = $2
		                  AND x.status = 'denied' AND x.decided_at > now() - make_interval(secs => $3))`,
		domain, me.UserID, denyCooldown.Seconds(), s.d.Cfg.IsDevelopment())
	if err != nil {
		return err
	}
	candidates, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		httpx.OK(w, AutoJoined{Joined: []Org{}})
		return nil
	}
	s.autoJoinLimited.ServeHTTP(w, r.WithContext(context.WithValue(ctx, autoJoinCandidatesKey{}, candidates)))
	return nil
}

type autoJoinCandidatesKey struct{}

// autoJoinWork joins the candidates autoJoin found, behind
// auth.RateLimit(autoJoinsPerHour) (s.autoJoinLimited).
func (s *Service) autoJoinWork(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	domain := s.emailDomain(me.Email)
	candidates, _ := ctx.Value(autoJoinCandidatesKey{}).([]uuid.UUID)
	out := AutoJoined{Joined: []Org{}}
	for _, orgID := range candidates {
		var joined bool
		err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
			if _, err := lockOrg(ctx, tx, orgID); err != nil {
				return err
			}
			// Re-check under the lock: the domain or its policy may have changed.
			var ok bool
			if err := tx.QueryRow(ctx, `
				SELECT EXISTS (SELECT 1 FROM org_domains WHERE org_id = $1 AND domain = $2 AND verified_at IS NOT NULL AND policy = 'auto_join'
				               AND (verify_method = 'dns' OR $4))
				   AND NOT EXISTS (SELECT 1 FROM org_departures WHERE org_id = $1 AND user_id = $3)`,
				orgID, domain, me.UserID, s.d.Cfg.IsDevelopment()).Scan(&ok); err != nil || !ok {
				return err
			}
			var err error
			if joined, err = joinMember(ctx, tx, orgID, me.UserID, RoleMember); err != nil {
				return err
			}
			return closeRequest(ctx, tx, orgID, me.UserID, "approved", nil)
		})
		if errors.Is(err, errOrgNotFound) {
			continue // org deleted meanwhile
		}
		if err != nil {
			return err
		}
		if !joined {
			continue
		}
		s.membershipChanged(context.WithoutCancel(ctx), orgID, me.UserID, EventMemberJoined, map[string]any{"role": RoleMember})
		o, err := s.loadOrg(ctx, orgID, me.UserID)
		if err != nil {
			return err
		}
		out.Joined = append(out.Joined, *o)
	}
	httpx.OK(w, out)
	return nil
}

// domainAccess is how the caller may enter an org through their email domain.
type domainAccess struct {
	domain, policy, departure string
	pendingID                 *uuid.UUID
	deniedAt                  *time.Time
}

// accessFor reads the caller's domain route into orgID, or errOrgNotFound
// when their email domain has none (so org ids can't be probed).
func (s *Service) accessFor(ctx context.Context, q db.Querier, orgID uuid.UUID, me *auth.Identity) (*domainAccess, error) {
	domain := s.emailDomain(me.Email)
	if domain == "" {
		return nil, errOrgNotFound
	}
	a := domainAccess{domain: domain}
	err := q.QueryRow(ctx, `
		SELECT d.policy, COALESCE(dep.reason, ''),
		       (SELECT id FROM org_join_requests WHERE org_id = $1 AND user_id = $3 AND status = 'pending'),
		       (SELECT max(decided_at) FROM org_join_requests WHERE org_id = $1 AND user_id = $3 AND status = 'denied'
		          AND decided_at > now() - make_interval(secs => $4))
		FROM org_domains d LEFT JOIN org_departures dep ON dep.org_id = d.org_id AND dep.user_id = $3
		WHERE d.org_id = $1 AND d.domain = $2 AND d.verified_at IS NOT NULL`,
		orgID, domain, me.UserID, denyCooldown.Seconds()).Scan(&a.policy, &a.departure, &a.pendingID, &a.deniedAt)
	if db.IsNoRows(err) {
		return nil, errOrgNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func deniedError(at time.Time) error {
	return httpx.NewError(http.StatusTooManyRequests, "request_denied",
		"An admin declined your request. You can ask again after "+at.Add(denyCooldown).UTC().Format("January 2")+".")
}

// joinByDomain: POST /v1/orgs/{orgID}/join (active). For an auto_join
// domain. People an admin removed must ask instead.
func (s *Service) joinByDomain(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	orgID, err := uuid.Parse(chi.URLParam(r, "orgID"))
	if err != nil {
		return errOrgNotFound
	}
	// Callers without a domain route get 404 before any lock is taken.
	if _, err := s.accessFor(ctx, s.d.Pool, orgID, me); err != nil {
		return err
	}
	var joined bool
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if _, err := lockOrg(ctx, tx, orgID); err != nil {
			return err
		}
		a, err := s.accessFor(ctx, tx, orgID, me)
		if err != nil {
			return err
		}
		role, err := RoleOf(ctx, tx, orgID, me.UserID)
		if err != nil || role != "" {
			return err
		}
		if a.deniedAt != nil {
			return deniedError(*a.deniedAt)
		}
		if a.policy != PolicyAutoJoin || a.departure == "removed" {
			return httpx.NewError(http.StatusForbidden, "approval_required", "An admin approves new members of this "+Noun()+". Ask to join instead.")
		}
		if joined, err = joinMember(ctx, tx, orgID, me.UserID, RoleMember); err != nil {
			return err
		}
		return closeRequest(ctx, tx, orgID, me.UserID, "approved", nil)
	})
	if err != nil {
		return err
	}
	return s.respondJoined(w, r, orgID, me.UserID, joined)
}

// MyJoinRequest is the caller's own request.
type MyJoinRequest struct {
	ID        uuid.UUID `json:"id"`
	Org       OrgRef    `json:"org"`
	Domain    string    `json:"domain"`
	Status    string    `json:"status"` // pending
	CreatedAt time.Time `json:"created_at"`
}

// createJoinRequest: POST /v1/orgs/{orgID}/join-requests (active, 10/h).
// Asking again while a request is pending returns it. Admins are notified
// at most once a day per person.
func (s *Service) createJoinRequest(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	orgID, err := uuid.Parse(chi.URLParam(r, "orgID"))
	if err != nil {
		return errOrgNotFound
	}
	var (
		req        MyJoinRequest
		created    bool
		tellAdmins bool
		orgName    string
	)
	if _, err := s.accessFor(ctx, s.d.Pool, orgID, me); err != nil {
		return err
	}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if orgName, err = lockOrg(ctx, tx, orgID); err != nil {
			return err
		}
		a, err := s.accessFor(ctx, tx, orgID, me)
		if err != nil {
			return err
		}
		role, err := RoleOf(ctx, tx, orgID, me.UserID)
		if err != nil {
			return err
		}
		if role != "" {
			return httpx.Conflict("You're already a member.")
		}
		if a.pendingID == nil {
			if a.deniedAt != nil {
				return deniedError(*a.deniedAt)
			}
			if a.policy == PolicyAutoJoin && a.departure != "removed" {
				return httpx.NewError(http.StatusConflict, "join_directly", "You can join this "+Noun()+" without asking.")
			}
			if err := tx.QueryRow(ctx, `
				SELECT NOT EXISTS (SELECT 1 FROM org_join_requests WHERE org_id = $1 AND user_id = $2
				                   AND created_at > now() - make_interval(secs => $3))`,
				orgID, me.UserID, requestNotifyWindow.Seconds()).Scan(&tellAdmins); err != nil {
				return err
			}
			var id uuid.UUID
			if err := tx.QueryRow(ctx, `INSERT INTO org_join_requests (org_id, user_id, domain) VALUES ($1, $2, $3) RETURNING id`,
				orgID, me.UserID, a.domain).Scan(&id); err != nil {
				return err
			}
			a.pendingID, created = &id, true
		}
		var avatar *string
		if err := tx.QueryRow(ctx, `
			SELECT j.id, o.id, o.name, o.slug, o.avatar_key, j.domain, j.status, j.created_at
			FROM org_join_requests j JOIN orgs o ON o.id = j.org_id WHERE j.id = $1`, *a.pendingID).
			Scan(&req.ID, &req.Org.ID, &req.Org.Name, &req.Org.Slug, &avatar, &req.Domain, &req.Status, &req.CreatedAt); err != nil {
			return err
		}
		req.Org.AvatarURL = s.avatarURL(avatar)
		return nil
	})
	if err != nil {
		return err
	}
	if !created {
		httpx.OK(w, req)
		return nil
	}
	bg := context.WithoutCancel(ctx)
	s.publish(bg, orgTopic(orgID), EventJoinRequestsChanged, map[string]any{"org_id": orgID})
	s.publish(bg, realtime.UserTopic(me.UserID), EventMembershipChanged, map[string]any{"org_id": orgID})
	if tellAdmins {
		s.notifyAdminsOfRequest(bg, orgID, orgName, me, req.ID)
	}
	httpx.JSON(w, http.StatusCreated, req)
	return nil
}

func (s *Service) notifyAdminsOfRequest(ctx context.Context, orgID uuid.UUID, orgName string, me *auth.Identity, requestID uuid.UUID) {
	rows, err := s.d.Pool.Query(ctx, `
		SELECT m.user_id FROM org_members m JOIN users u ON u.id = m.user_id
		WHERE m.org_id = $1 AND m.role IN ('owner', 'admin') AND u.status = 'active'
		ORDER BY m.role = 'owner' DESC, m.created_at LIMIT $2`, orgID, maxJoinRequestNotify)
	if err != nil {
		slog.WarnContext(ctx, "orgs: find admins for join request", "org_id", orgID, "err", err)
		return
	}
	admins, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		slog.WarnContext(ctx, "orgs: find admins for join request", "org_id", orgID, "err", err)
		return
	}
	who := me.DisplayName
	if who == "" {
		who = me.Email
	}
	for _, a := range admins {
		s.send(ctx, notify.Notification{UserID: a, Kind: KindJoinRequest, Title: who + " asked to join " + orgName, Body: me.Email,
			Link: "/settings/orgs/" + orgID.String() + "?tab=members",
			Data: map[string]any{"org_id": orgID, "request_id": requestID}})
	}
}

// cancelMyJoinRequest: DELETE /v1/orgs/my-join-requests/{id}.
func (s *Service) cancelMyJoinRequest(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var orgID uuid.UUID
	err = s.d.Pool.QueryRow(r.Context(), `
		UPDATE org_join_requests SET status = 'cancelled', decided_at = now(), updated_at = now()
		WHERE id = $1 AND user_id = $2 AND status = 'pending' RETURNING org_id`, id, me.UserID).Scan(&orgID)
	if db.IsNoRows(err) {
		return httpx.NotFound("Request not found.")
	}
	if err != nil {
		return err
	}
	bg := context.WithoutCancel(r.Context())
	s.publish(bg, orgTopic(orgID), EventJoinRequestsChanged, map[string]any{"org_id": orgID})
	s.publish(bg, realtime.UserTopic(me.UserID), EventMembershipChanged, map[string]any{"org_id": orgID})
	httpx.NoContent(w)
	return nil
}

// JoinRequest is a pending request as org admins see it.
type JoinRequest struct {
	ID          uuid.UUID `json:"id"`
	OrgID       uuid.UUID `json:"org_id"`
	UserID      uuid.UUID `json:"user_id"`
	DisplayName string    `json:"display_name"`
	Email       string    `json:"email"`
	AvatarURL   *string   `json:"avatar_url"`
	Domain      string    `json:"domain"`
	CreatedAt   time.Time `json:"created_at"`
}

const joinRequestSelect = `
	SELECT j.id, j.org_id, j.user_id, u.display_name, u.email, u.avatar_url, j.domain, j.created_at
	FROM org_join_requests j JOIN users u ON u.id = j.user_id`

// listJoinRequests: GET /v1/orgs/{orgID}/join-requests (admin+), pending,
// oldest first.
func (s *Service) listJoinRequests(w http.ResponseWriter, r *http.Request) error {
	m := mustMembership(r)
	p := httpx.ParsePage(r)
	var c cursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	rows, err := s.d.Pool.Query(r.Context(), joinRequestSelect+`
		WHERE j.org_id = $1 AND j.status = 'pending' AND ($2::timestamptz IS NULL OR (j.created_at, j.id) > ($2, $3))
		ORDER BY j.created_at, j.id LIMIT $4`, m.OrgID, nullTime(c.T), c.ID, p.Limit+1)
	if err != nil {
		return err
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[JoinRequest])
	if err != nil {
		return err
	}
	if list == nil {
		list = []JoinRequest{}
	}
	httpx.OK(w, httpx.NewPage(list, p.Limit, func(j JoinRequest) any { return cursor{j.CreatedAt, j.ID} }))
	return nil
}

// approveJoinRequest: POST /v1/orgs/{orgID}/join-requests/{requestID}/approve
// (admin+). The person joins with the domain's role (member). Accounts that
// aren't active can't be added.
func (s *Service) approveJoinRequest(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	m := mustMembership(r)
	id, err := httpx.URLParamUUID(r, "requestID")
	if err != nil {
		return err
	}
	var (
		target  uuid.UUID
		orgName string
		joined  bool
	)
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if orgName, err = lockOrg(ctx, tx, m.OrgID); err != nil {
			return err
		}
		var status, userStatus string
		err := tx.QueryRow(ctx, `SELECT j.user_id, j.status, u.status FROM org_join_requests j JOIN users u ON u.id = j.user_id
			WHERE j.id = $1 AND j.org_id = $2 FOR UPDATE OF j`, id, m.OrgID).Scan(&target, &status, &userStatus)
		if db.IsNoRows(err) {
			return errRequestNotFound
		}
		if err != nil {
			return err
		}
		if status != "pending" {
			return httpx.Conflict("That request was already " + status + ".")
		}
		if userStatus != "active" {
			return httpx.Conflict("That account is " + userStatus + ", so it can't join.")
		}
		if joined, err = joinMember(ctx, tx, m.OrgID, target, RoleMember); err != nil {
			return err
		}
		return closeRequest(ctx, tx, m.OrgID, target, "approved", &me)
	})
	if err != nil {
		return err
	}
	bg := context.WithoutCancel(ctx)
	s.publish(bg, orgTopic(m.OrgID), EventJoinRequestsChanged, map[string]any{"org_id": m.OrgID})
	if joined {
		s.membershipChanged(bg, m.OrgID, target, EventMemberJoined, map[string]any{"role": RoleMember})
		s.send(bg, notify.Notification{UserID: target, Kind: KindJoinRequest, Title: "You joined " + orgName,
			Body: "An admin approved your request.", Link: "/settings/orgs/" + m.OrgID.String(), Data: map[string]any{"org_id": m.OrgID}})
	}
	mem, err := s.loadMember(ctx, s.d.Pool, m.OrgID, target, true)
	if err != nil {
		return err
	}
	httpx.OK(w, mem)
	return nil
}

var errRequestNotFound = httpx.NotFound("Request not found.")

// denyJoinRequest: POST /v1/orgs/{orgID}/join-requests/{requestID}/deny
// (admin+). They can ask again after 7 days.
func (s *Service) denyJoinRequest(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	m := mustMembership(r)
	id, err := httpx.URLParamUUID(r, "requestID")
	if err != nil {
		return err
	}
	var (
		target  uuid.UUID
		orgName string
	)
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if orgName, err = lockOrg(ctx, tx, m.OrgID); err != nil {
			return err
		}
		var status string
		err := tx.QueryRow(ctx, `SELECT user_id, status FROM org_join_requests WHERE id = $1 AND org_id = $2 FOR UPDATE`,
			id, m.OrgID).Scan(&target, &status)
		if db.IsNoRows(err) {
			return errRequestNotFound
		}
		if err != nil {
			return err
		}
		if status != "pending" {
			return httpx.Conflict("That request was already " + status + ".")
		}
		_, err = tx.Exec(ctx, `UPDATE org_join_requests SET status = 'denied', decided_by = $2, decided_at = now(), updated_at = now()
			WHERE id = $1`, id, me)
		return err
	})
	if err != nil {
		return err
	}
	bg := context.WithoutCancel(ctx)
	s.publish(bg, orgTopic(m.OrgID), EventJoinRequestsChanged, map[string]any{"org_id": m.OrgID})
	s.publish(bg, realtime.UserTopic(target), EventMembershipChanged, map[string]any{"org_id": m.OrgID})
	s.send(bg, notify.Notification{UserID: target, Kind: KindJoinRequest, Title: "Your request to join " + orgName + " was declined",
		Link: "/settings/orgs", Data: map[string]any{"org_id": m.OrgID}})
	httpx.NoContent(w)
	return nil
}

// closeRequest settles userID's pending request to orgID, if any.
func closeRequest(ctx context.Context, tx pgx.Tx, orgID, userID uuid.UUID, status string, decidedBy *uuid.UUID) error {
	_, err := tx.Exec(ctx, `UPDATE org_join_requests SET status = $3, decided_by = $4, decided_at = now(), updated_at = now()
		WHERE org_id = $1 AND user_id = $2 AND status = 'pending'`, orgID, userID, status, decidedBy)
	return err
}

// recordDeparture remembers that userID left orgID (or was removed), so
// domain auto-join leaves them out.
func recordDeparture(ctx context.Context, tx pgx.Tx, orgID, userID uuid.UUID, removed bool) error {
	reason := "left"
	if removed {
		reason = "removed"
	}
	_, err := tx.Exec(ctx, `INSERT INTO org_departures (org_id, user_id, reason) VALUES ($1, $2, $3)
		ON CONFLICT (org_id, user_id) DO UPDATE SET reason = EXCLUDED.reason, created_at = now()`, orgID, userID, reason)
	return err
}
