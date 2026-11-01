package access

import (
	"context"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/audit"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/bg"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
)

// Permission is a named org capability a feature registers ("crm.export").
// Org admins change which roles hold it and add teams and people.
type Permission struct {
	// Key is "<feature>.<capability>", lowercase.
	Key string `json:"key"`
	// Title is the row label on the permissions screen: "Export records".
	Title string `json:"title"`
	// Info is the tooltip: what holding it allows.
	Info string `json:"info"`
	// DefaultRoles hold it until an admin changes it (owner, admin, member).
	DefaultRoles []string `json:"default_roles"`
}

var (
	permsMu  sync.RWMutex
	perms    = map[string]Permission{}
	permKey  = regexp.MustCompile(`^[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*$`)
	orgRoles = []string{orgs.RoleOwner, orgs.RoleAdmin, orgs.RoleMember}
)

// RegisterPermission adds a permission (call it from the feature's New). It
// panics on an invalid key or role (a programming error).
func RegisterPermission(p Permission) {
	if !permKey.MatchString(p.Key) {
		panic("access: permission key must look like feature.capability: " + p.Key)
	}
	for _, r := range p.DefaultRoles {
		if !slices.Contains(orgRoles, r) {
			panic("access: permission " + p.Key + " has an unknown default role " + r)
		}
	}
	if p.Title == "" {
		p.Title = p.Key
	}
	p.DefaultRoles = normRoles(p.DefaultRoles)
	permsMu.Lock()
	perms[p.Key] = p
	permsMu.Unlock()
}

// PermissionOf returns a registered permission.
func PermissionOf(key string) (Permission, bool) {
	permsMu.RLock()
	defer permsMu.RUnlock()
	p, ok := perms[key]
	return p, ok
}

func allPermissions() []Permission {
	permsMu.RLock()
	out := make([]Permission, 0, len(perms))
	for _, p := range perms {
		out = append(out, p)
	}
	permsMu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// normRoles returns roles in rank order (owner, admin, member), deduplicated.
func normRoles(roles []string) []string {
	out := []string{}
	for _, r := range orgRoles {
		if slices.Contains(roles, r) {
			out = append(out, r)
		}
	}
	return out
}

// Allowed reports whether userID holds the permission in orgID: a member
// whose role holds it, who is on a team that holds it, or who holds it by
// name. Non-members and unregistered keys are never allowed.
func Allowed(ctx context.Context, q db.Querier, orgID, userID uuid.UUID, key string) (bool, error) {
	p, ok := PermissionOf(key)
	if !ok {
		return false, nil
	}
	var allowed bool
	err := q.QueryRow(ctx, `SELECT access_permission_allowed($1, $2, $3, $4)`, orgID, userID, key, p.DefaultRoles).Scan(&allowed)
	return allowed, err
}

// RequirePermission guards a route with an {orgID} URL param: non-members
// get 404, members without the permission 403 permission_required. Mount
// behind auth (UserRoutes).
func RequirePermission(key string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := auth.FromContext(r.Context())
			if id == nil {
				httpx.Fail(w, r, httpx.ErrUnauthenticated)
				return
			}
			orgID, err := uuid.Parse(chi.URLParam(r, "orgID"))
			if err != nil {
				httpx.Fail(w, r, httpx.NotFound(orgs.NounTitle()+" not found."))
				return
			}
			p := pool.Load()
			role, err := orgs.RoleOf(r.Context(), p, orgID, id.UserID)
			if err != nil {
				httpx.Fail(w, r, err)
				return
			}
			if role == "" {
				httpx.Fail(w, r, httpx.NotFound(orgs.NounTitle()+" not found."))
				return
			}
			ok, err := Allowed(r.Context(), p, orgID, id.UserID, key)
			if err != nil {
				httpx.Fail(w, r, err)
				return
			}
			if !ok {
				httpx.Fail(w, r, permissionRequired(key))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func permissionRequired(key string) *httpx.Error {
	title := key
	if p, ok := PermissionOf(key); ok {
		title = p.Title
	}
	e := httpx.NewError(http.StatusForbidden, "permission_required",
		"You don't have the \""+title+"\" permission in this "+orgs.Noun()+". Ask an admin.")
	e.Details = map[string]any{"permission": key}
	return e
}

// ---- routes ------------------------------------------------------------------

// myPermissions: GET /v1/access/orgs/{orgID}/my-permissions → {permissions: {key: bool},
// items: [{key, title, info, allowed}]}.
func (s *Service) myPermissions(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context()).UserID
	m, _ := orgs.FromContext(r.Context())
	list := allPermissions()
	keys := make([]string, len(list))
	defaults := make([]string, len(list)) // roles as a comma list per key (pgx can't send ragged arrays)
	for i, p := range list {
		keys[i] = p.Key
		defaults[i] = strings.Join(p.DefaultRoles, ",")
	}
	rows, err := s.d.Pool.Query(r.Context(), `
		SELECT k, access_permission_allowed($1, $2, k, CASE WHEN d = '' THEN '{}'::text[] ELSE string_to_array(d, ',') END)
		FROM unnest($3::text[], $4::text[]) AS t (k, d)`, m.OrgID, me, keys, defaults)
	if err != nil {
		return err
	}
	type item struct {
		Key     string `json:"key"`
		Title   string `json:"title"`
		Info    string `json:"info"`
		Allowed bool   `json:"allowed"`
	}
	out := map[string]bool{}
	items := []item{}
	for rows.Next() {
		var k string
		var ok bool
		if err := rows.Scan(&k, &ok); err != nil {
			rows.Close()
			return err
		}
		out[k] = ok
		p, _ := PermissionOf(k)
		items = append(items, item{Key: k, Title: p.Title, Info: p.Info, Allowed: ok})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Key < items[j].Key })
	httpx.OK(w, map[string]any{"org_id": m.OrgID, "permissions": out, "items": items})
	return nil
}

// PolicySubject is a team or person named on a permission.
type PolicySubject struct {
	Type      string     `json:"type"` // team | user
	ID        uuid.UUID  `json:"id"`
	Name      string     `json:"name"`
	Handle    *string    `json:"handle"`
	AvatarURL *string    `json:"avatar_url"`
	AddedAt   *time.Time `json:"added_at,omitempty"`
}

// PermissionPolicy is one row of the permissions matrix.
type PermissionPolicy struct {
	Permission
	Roles      []string        `json:"roles"`
	Teams      []PolicySubject `json:"teams"`
	Users      []PolicySubject `json:"users"`
	Customized bool            `json:"customized"`
	UpdatedAt  *time.Time      `json:"updated_at"`
}

// listPermissions: GET /v1/access/orgs/{orgID}/permissions (org admins): every
// registered permission with the org's roles, teams and people.
func (s *Service) listPermissions(w http.ResponseWriter, r *http.Request) error {
	m, _ := orgs.FromContext(r.Context())
	list, err := s.loadPolicies(r.Context(), s.d.Pool, m.OrgID, "")
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": list})
	return nil
}

func (s *Service) loadPolicies(ctx context.Context, q db.Querier, orgID uuid.UUID, only string) ([]PermissionPolicy, error) {
	type row struct {
		Key       string
		Roles     []string
		UpdatedAt time.Time
	}
	rows, err := q.Query(ctx, `SELECT key, roles, updated_at FROM access_permission_policies WHERE org_id = $1`, orgID)
	if err != nil {
		return nil, err
	}
	saved, err := pgx.CollectRows(rows, pgx.RowToStructByPos[row])
	if err != nil {
		return nil, err
	}
	rows, err = q.Query(ctx, `
		SELECT s.key, s.subject_type, s.subject_id,
		       COALESCE(t.name, u.display_name, ''), t.handle, CASE WHEN s.subject_type = 'user' THEN u.avatar_url END, s.created_at
		FROM access_permission_subjects s
		LEFT JOIN access_teams t ON s.subject_type = 'team' AND t.id = s.subject_id
		LEFT JOIN users u ON s.subject_type = 'user' AND u.id = s.subject_id
		WHERE s.org_id = $1 AND (t.id IS NOT NULL OR u.id IS NOT NULL)
		ORDER BY lower(COALESCE(t.name, u.display_name, '')), s.subject_id`, orgID)
	if err != nil {
		return nil, err
	}
	subjects := map[string][]PolicySubject{}
	for rows.Next() {
		var key string
		var ps PolicySubject
		var at time.Time
		if err := rows.Scan(&key, &ps.Type, &ps.ID, &ps.Name, &ps.Handle, &ps.AvatarURL, &at); err != nil {
			rows.Close()
			return nil, err
		}
		ps.AddedAt = &at
		subjects[key] = append(subjects[key], ps)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := []PermissionPolicy{}
	for _, p := range allPermissions() {
		if only != "" && p.Key != only {
			continue
		}
		pp := PermissionPolicy{Permission: p, Roles: p.DefaultRoles, Teams: []PolicySubject{}, Users: []PolicySubject{}}
		for _, sr := range saved {
			if sr.Key == p.Key {
				at := sr.UpdatedAt
				pp.Roles, pp.Customized, pp.UpdatedAt = normRoles(sr.Roles), true, &at
			}
		}
		for _, sub := range subjects[p.Key] {
			if sub.Type == "team" {
				pp.Teams = append(pp.Teams, sub)
			} else {
				pp.Users = append(pp.Users, sub)
			}
		}
		out = append(out, pp)
	}
	return out, nil
}

type setPermissionReq struct {
	Roles   []string    `json:"roles"`
	TeamIDs []uuid.UUID `json:"team_ids"`
	UserIDs []uuid.UUID `json:"user_ids"`
}

// setPermission: PUT /v1/access/orgs/{orgID}/permissions/{key} (org admins)
// {roles, team_ids, user_ids}: replaces who holds it. Audited.
func (s *Service) setPermission(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	m, _ := orgs.FromContext(ctx)
	key := chi.URLParam(r, "key")
	p, ok := PermissionOf(key)
	if !ok {
		return httpx.NotFound("Permission not found.")
	}
	var req setPermissionReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	for _, role := range req.Roles {
		if !slices.Contains(orgRoles, role) {
			return httpx.BadRequest("roles must be owner, admin or member.")
		}
	}
	roles := normRoles(req.Roles)
	teams, users := dedupe(req.TeamIDs), dedupe(req.UserIDs)
	if len(teams)+len(users) > 200 {
		return httpx.BadRequest("Name at most 200 teams and people. Use a role or a team for larger groups.")
	}
	var before []PermissionPolicy
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var err error
		if before, err = s.loadPolicies(ctx, tx, m.OrgID, key); err != nil {
			return err
		}
		if len(teams) > 0 {
			var n int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM access_teams WHERE org_id = $1 AND id = ANY ($2)`, m.OrgID, teams).Scan(&n); err != nil {
				return err
			}
			if n != len(teams) {
				return httpx.BadRequest("Choose teams from this " + orgs.Noun() + ".")
			}
		}
		if len(users) > 0 {
			var n int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM org_members WHERE org_id = $1 AND user_id = ANY ($2)`, m.OrgID, users).Scan(&n); err != nil {
				return err
			}
			if n != len(users) {
				return httpx.BadRequest("Choose people who are members of this " + orgs.Noun() + ".")
			}
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO access_permission_policies (org_id, key, roles, updated_by) VALUES ($1, $2, $3, $4)
			ON CONFLICT (org_id, key) DO UPDATE SET roles = EXCLUDED.roles, updated_by = EXCLUDED.updated_by, updated_at = now()`,
			m.OrgID, key, roles, me); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM access_permission_subjects WHERE org_id = $1 AND key = $2`, m.OrgID, key); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO access_permission_subjects (org_id, key, subject_type, subject_id)
			SELECT $1::uuid, $2::text, 'team', unnest($3::uuid[])
			UNION ALL SELECT $1::uuid, $2::text, 'user', unnest($4::uuid[])`, m.OrgID, key, teams, users); err != nil {
			return err
		}
		return audit.Log(ctx, tx, me, "access.permission.set", "org", m.OrgID.String(), map[string]any{
			"key": key, "roles": roles, "team_ids": teams, "user_ids": users, "before": policySummary(before),
		})
	})
	if err != nil {
		return err
	}
	publish(context.WithoutCancel(ctx), orgTopic(m.OrgID), EventPermissionsChanged, map[string]any{"org_id": m.OrgID, "key": key})
	firePermissionChange(ctx, m.OrgID, key, nil)
	list, err := s.loadPolicies(ctx, s.d.Pool, m.OrgID, p.Key)
	if err != nil {
		return err
	}
	httpx.OK(w, list[0])
	return nil
}

// resetPermission: DELETE /v1/access/orgs/{orgID}/permissions/{key} (org
// admins): back to the registered defaults. Audited.
func (s *Service) resetPermission(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	m, _ := orgs.FromContext(ctx)
	key := chi.URLParam(r, "key")
	if _, ok := PermissionOf(key); !ok {
		return httpx.NotFound("Permission not found.")
	}
	reset := false
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		before, err := s.loadPolicies(ctx, tx, m.OrgID, key)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `DELETE FROM access_permission_policies WHERE org_id = $1 AND key = $2`, m.OrgID, key)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		reset = true
		return audit.Log(ctx, tx, me, "access.permission.reset", "org", m.OrgID.String(), map[string]any{
			"key": key, "before": policySummary(before),
		})
	})
	if err != nil {
		return err
	}
	publish(context.WithoutCancel(ctx), orgTopic(m.OrgID), EventPermissionsChanged, map[string]any{"org_id": m.OrgID, "key": key})
	if reset {
		firePermissionChange(ctx, m.OrgID, key, nil)
	}
	list, err := s.loadPolicies(ctx, s.d.Pool, m.OrgID, key)
	if err != nil {
		return err
	}
	httpx.OK(w, list[0])
	return nil
}

// ---- permission-change hooks ---------------------------------------------------

// PermissionChangeFunc learns that users may have lost an org permission:
// key is the permission ("crm.access"), "" for any permission; users nil
// means anyone in the org. It fires after the change commits, in the
// background with a 30-second context (best-effort; a panic is logged), when
// an admin sets or resets a permission (key, users nil), when people leave a
// team or it is deleted (key "", the people who were on it), and when
// someone's org role changes (key "", that person). Leaving the org is
// orgs.OnMemberRemoved's. Re-check with Allowed and drop what the permission
// gave (realtime.Revoke per user, realtime.RevokeTopic when users is nil).
type PermissionChangeFunc func(ctx context.Context, orgID uuid.UUID, key string, users []uuid.UUID)

var (
	permChangeMu    sync.RWMutex
	permChangeHooks []PermissionChangeFunc
)

// OnPermissionChange registers fn (call it from the feature's New; hooks are
// read when a change happens, so construction order never matters).
func OnPermissionChange(fn PermissionChangeFunc) {
	if fn == nil {
		panic("access: OnPermissionChange needs a function")
	}
	permChangeMu.Lock()
	permChangeHooks = append(permChangeHooks, fn)
	permChangeMu.Unlock()
}

// firePermissionChange runs the hooks off the request. Call it only after the
// change committed.
func firePermissionChange(ctx context.Context, orgID uuid.UUID, key string, users []uuid.UUID) {
	permChangeMu.RLock()
	hooks := append([]PermissionChangeFunc(nil), permChangeHooks...)
	permChangeMu.RUnlock()
	if len(hooks) == 0 || (users != nil && len(users) == 0) {
		return
	}
	bg.Go(ctx, "access.permission_change", func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		for _, fn := range hooks {
			func() {
				defer func() {
					if p := recover(); p != nil {
						slog.ErrorContext(ctx, "access: permission-change hook panicked", "org_id", orgID, "key", key, "panic", p)
					}
				}()
				fn(ctx, orgID, key, users)
			}()
		}
		return nil
	})
}

// roleChanged is access's orgs.OnRoleChanged hook: a permission held by a
// role can go with it.
func roleChanged(ctx context.Context, orgID, userID uuid.UUID, _, _ string) {
	firePermissionChange(ctx, orgID, "", []uuid.UUID{userID})
}

func policySummary(list []PermissionPolicy) map[string]any {
	if len(list) == 0 {
		return nil
	}
	p := list[0]
	teams, users := []uuid.UUID{}, []uuid.UUID{}
	for _, t := range p.Teams {
		teams = append(teams, t.ID)
	}
	for _, u := range p.Users {
		users = append(users, u.ID)
	}
	return map[string]any{"roles": p.Roles, "team_ids": teams, "user_ids": users, "customized": p.Customized}
}

func dedupe(ids []uuid.UUID) []uuid.UUID {
	seen := map[uuid.UUID]bool{}
	out := []uuid.UUID{}
	for _, id := range ids {
		if id != uuid.Nil && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}
