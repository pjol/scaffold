package access

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/audit"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/notify"
	"scaffoldapp/backend/internal/orgs"
)

// Subject is who a grant is for.
type Subject struct {
	Type        string    `json:"type"` // user | team | org
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Handle      *string   `json:"handle"`       // teams
	AvatarURL   *string   `json:"avatar_url"`   // people and teams
	Email       *string   `json:"email"`        // people, shown to org admins only
	MemberCount *int      `json:"member_count"` // teams and the org
}

// Source is a resource access comes from (a parent). Title and Link are
// null when the caller can't open it.
type Source struct {
	Kind  string    `json:"kind"`
	ID    uuid.UUID `json:"id"`
	Label string    `json:"label"`
	Title *string   `json:"title"`
	Link  *string   `json:"link"`
}

// Grant is one entry in a resource's access list.
type Grant struct {
	ID      uuid.UUID `json:"id"`
	Subject Subject   `json:"subject"`
	Level   Level     `json:"level"`
	// InheritedFrom is null for grants set on this resource; otherwise the
	// ancestor they come from (change them there).
	InheritedFrom *Source   `json:"inherited_from"`
	CreatedAt     time.Time `json:"created_at"`
}

// General is the resource's org-wide access: restricted, or everyone in the
// org at a level (set here or inherited).
type General struct {
	Access string `json:"access"` // restricted | org
	Level  *Level `json:"level"`  // the effective org-wide level when access is org
	// OwnLevel is the org-wide level set on this resource itself (null: none).
	OwnLevel      *Level  `json:"own_level"`
	InheritedFrom *Source `json:"inherited_from"`
}

// Person is a user shown by name.
type Person struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	AvatarURL *string   `json:"avatar_url"`
}

// PublicLinks says whether the caller may make public links here.
type PublicLinks struct {
	Supported bool `json:"supported"` // the kind has public links
	// Allowed: the org lets the caller make them (access.share_public and the
	// kind's LinkPermission).
	Allowed bool    `json:"allowed"`
	Levels  []Level `json:"levels"`
	// MissingPermission is the org permission the caller lacks to make one
	// (null when Allowed), so the dialog can say which to ask for.
	MissingPermission *PermissionRef `json:"missing_permission"`
	// Inherited: a public link on an ancestor also opens this resource.
	Inherited *Source `json:"inherited"`
}

// PermissionRef names an org permission.
type PermissionRef struct {
	Key   string `json:"key"`
	Title string `json:"title"`
}

// Sharing is GET /v1/access/resources/{kind}/{id}. Everyone who can view
// sees their level, the owner, general access and where access comes from;
// grants and links are filled only for people who can manage it.
type Sharing struct {
	Kind          string      `json:"kind"`
	ID            uuid.UUID   `json:"id"`
	OrgID         *uuid.UUID  `json:"org_id"`
	Label         string      `json:"label"`
	Title         string      `json:"title"`
	Link          string      `json:"link"`
	MyLevel       Level       `json:"my_level"`
	IsOwner       bool        `json:"is_owner"`
	CanManage     bool        `json:"can_manage"`
	AdminOverride bool        `json:"admin_override"` // you manage it only as an org admin
	Owner         *Person     `json:"owner"`
	Inherits      bool        `json:"inherits"`
	Parent        *Source     `json:"parent"`
	General       General     `json:"general"`
	Grants        []Grant     `json:"grants"`
	Links         []LinkInfo  `json:"links"`
	Levels        []Level     `json:"levels"`
	PublicLinks   PublicLinks `json:"public_links"`
	// CanCreateLink: the caller (a person, not an agent) manages it and may
	// make a public link to it now.
	CanCreateLink bool `json:"can_create_link"`
}

// target is a resource a request acts on, with the caller's standing.
type target struct {
	kind     Kind
	ref      Ref
	res      *Resource
	me       uuid.UUID
	level    Level // the caller's level (capped for agent calls)
	human    Level // the user's own level
	orgRole  string
	override bool // manage comes only from the org-admin rule
}

func refParams(r *http.Request) (Kind, Ref, error) {
	k, ok := KindOf(chi.URLParam(r, "kind"))
	if !ok {
		return Kind{}, Ref{}, httpx.NotFound("Not found.")
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return Kind{}, Ref{}, notFound(k.Name)
	}
	return k, Ref{Kind: k.Name, ID: id}, nil
}

// load resolves the target for the caller: 404 without view; with min
// Manage, 403 for viewers.
func (s *Service) load(ctx context.Context, q db.Querier, r *http.Request, min Level) (*target, error) {
	k, ref, err := refParams(r)
	if err != nil {
		return nil, err
	}
	return s.loadTarget(ctx, q, auth.MustFromContext(ctx).UserID, k, ref, min)
}

// loadTarget is load for callers without a request (agent tools).
func (s *Service) loadTarget(ctx context.Context, q db.Querier, me uuid.UUID, k Kind, ref Ref, min Level) (*target, error) {
	level, err := Require(ctx, q, me, ref.Kind, ref.ID, min)
	if err != nil {
		return nil, err
	}
	res, err := Lookup(ctx, q, ref)
	if err != nil {
		return nil, err
	}
	role, err := orgRole(ctx, q, res.OrgID, me)
	if err != nil {
		return nil, err
	}
	t := &target{kind: k, ref: ref, res: res, me: me, level: level, human: level, orgRole: role}
	if ViaAgent(ctx) {
		// The agent's level is capped; what it may read about sharing follows
		// its user (an agent of a manager sees the access list, never changes it).
		if t.human, err = levelOf(ctx, q, me, ref.Kind, ref.ID, false); err != nil {
			return nil, err
		}
	}
	if level == Manage && !t.isOwner() && orgs.RoleAtLeast(role, orgs.RoleAdmin) {
		var real int
		if err := q.QueryRow(ctx, `SELECT access_level($1, $2, $3, p_admin_rule => false)`, me, ref.Kind, ref.ID).Scan(&real); err != nil {
			return nil, err
		}
		t.override = Level(real) < Manage
	}
	return t, nil
}

func (t *target) isOwner() bool { return t.res.OwnerID != nil && *t.res.OwnerID == t.me }

// manage runs fn in a transaction that holds the resource row and re-checks
// manage there, then audits admin overrides.
func (s *Service) manage(ctx context.Context, r *http.Request, action string, fn func(tx pgx.Tx, t *target) (map[string]any, error)) (*target, error) {
	if ViaAgent(ctx) {
		return nil, ErrAgentForbidden
	}
	var t *target
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		k, ref, err := refParams(r)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT 1 FROM access_resources WHERE kind = $1 AND id = $2 FOR UPDATE`, k.Name, ref.ID); err != nil {
			return err
		}
		if t, err = s.load(ctx, tx, r, Manage); err != nil {
			return err
		}
		data, err := fn(tx, t)
		if err != nil {
			return err
		}
		if !t.override {
			return nil
		}
		if data == nil {
			data = map[string]any{}
		}
		data["org_id"] = t.res.OrgID
		return audit.Log(ctx, tx, t.me, "access.share."+action, t.ref.Kind, t.ref.ID.String(), data)
	})
	return t, err
}

// ---- reading -----------------------------------------------------------------

// getSharing: GET /v1/access/resources/{kind}/{id}.
func (s *Service) getSharing(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	t, err := s.load(ctx, s.d.Pool, r, View)
	if err != nil {
		return err
	}
	sh, err := s.sharing(ctx, t)
	if err != nil {
		return err
	}
	httpx.OK(w, sh)
	return nil
}

type chainRow struct {
	Kind  string
	ID    uuid.UUID
	Depth int
}

// chain is the resource and the ancestors it inherits from, nearest first.
func chain(ctx context.Context, q db.Querier, ref Ref) ([]chainRow, error) {
	rows, err := q.Query(ctx, `
		WITH RECURSIVE c (kind, id, parent_kind, parent_id, inherits, depth) AS (
			SELECT kind, id, parent_kind, parent_id, inherits, 0 FROM access_resources WHERE kind = $1 AND id = $2
			UNION ALL
			SELECT r.kind, r.id, r.parent_kind, r.parent_id, r.inherits, c.depth + 1
			FROM c JOIN access_resources r ON r.kind = c.parent_kind AND r.id = c.parent_id
			WHERE c.inherits AND c.depth < $3
		)
		SELECT kind, id, depth FROM c ORDER BY depth`, ref.Kind, ref.ID, maxDepth-1)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[chainRow])
}

// source describes an ancestor; its title and link only when the caller can
// view it.
func (s *Service) source(ctx context.Context, me uuid.UUID, ref Ref, cache map[Ref]*Source) (*Source, error) {
	if src, ok := cache[ref]; ok {
		return src, nil
	}
	src := &Source{Kind: ref.Kind, ID: ref.ID, Label: label(ref.Kind)}
	if k, ok := KindOf(ref.Kind); ok {
		l, err := LevelOf(ctx, s.d.Pool, me, ref.Kind, ref.ID)
		if err != nil {
			return nil, err
		}
		if l >= View {
			title, err := k.Title(ctx, s.d.Pool, ref.ID)
			if err != nil {
				slog.WarnContext(ctx, "access: kind title", "kind", ref.Kind, "err", err)
			} else {
				link := k.Link(ref.ID)
				src.Title, src.Link = &title, &link
			}
		}
	}
	cache[ref] = src
	return src, nil
}

func (s *Service) sharing(ctx context.Context, t *target) (*Sharing, error) {
	title, err := t.kind.Title(ctx, s.d.Pool, t.ref.ID)
	if err != nil {
		return nil, fmt.Errorf("access: %s title: %w", t.ref.Kind, err)
	}
	sh := &Sharing{
		Kind: t.ref.Kind, ID: t.ref.ID, OrgID: t.res.OrgID, Label: t.kind.Label, Title: title, Link: t.kind.Link(t.ref.ID),
		MyLevel: t.level, IsOwner: t.isOwner(), CanManage: t.level >= Manage, AdminOverride: t.override,
		Inherits: t.res.Inherits, Grants: []Grant{}, Links: []LinkInfo{}, Levels: t.kind.Levels,
		General: General{Access: "restricted"},
	}
	cache := map[Ref]*Source{}
	if t.res.OwnerID != nil {
		var p Person
		if err := s.d.Pool.QueryRow(ctx, `SELECT id, display_name, avatar_url FROM users WHERE id = $1 AND status <> 'deleted'`,
			*t.res.OwnerID).Scan(&p.ID, &p.Name, &p.AvatarURL); err == nil {
			sh.Owner = &p
		} else if !db.IsNoRows(err) {
			return nil, err
		}
	}
	if t.res.Parent != nil {
		if sh.Parent, err = s.source(ctx, t.me, *t.res.Parent, cache); err != nil {
			return nil, err
		}
	}
	links, err := chain(ctx, s.d.Pool, t.ref)
	if err != nil {
		return nil, err
	}
	kinds, ids := make([]string, len(links)), make([]uuid.UUID, len(links))
	depth := map[Ref]int{}
	for i, c := range links {
		kinds[i], ids[i] = c.Kind, c.ID
		depth[Ref{c.Kind, c.ID}] = c.Depth
	}

	showEmail := orgs.RoleAtLeast(t.orgRole, orgs.RoleAdmin)
	rows, err := s.d.Pool.Query(ctx, `
		SELECT g.id, g.resource_kind, g.resource_id, g.subject_type, g.subject_id, g.level, g.created_at,
		       COALESCE(u.display_name, tm.name, o.name, ''), tm.handle, u.avatar_url, tm.avatar_key, u.email,
		       CASE g.subject_type
		         WHEN 'team' THEN (SELECT count(*) FROM access_team_members x WHERE x.team_id = g.subject_id)::int
		         WHEN 'org' THEN (SELECT count(*) FROM org_members x WHERE x.org_id = g.subject_id)::int END
		FROM access_grants g
		JOIN unnest($1::text[], $2::uuid[]) AS c (k, i) ON c.k = g.resource_kind AND c.i = g.resource_id
		LEFT JOIN users u ON g.subject_type = 'user' AND u.id = g.subject_id
		LEFT JOIN access_teams tm ON g.subject_type = 'team' AND tm.id = g.subject_id
		LEFT JOIN orgs o ON g.subject_type = 'org' AND o.id = g.subject_id
		ORDER BY g.level DESC, g.created_at, g.id`, kinds, ids)
	if err != nil {
		return nil, err
	}
	type grantRow struct {
		g   Grant
		src Ref
		key *string
	}
	var all []grantRow
	for rows.Next() {
		var gr grantRow
		var lvl int16
		if err := rows.Scan(&gr.g.ID, &gr.src.Kind, &gr.src.ID, &gr.g.Subject.Type, &gr.g.Subject.ID, &lvl, &gr.g.CreatedAt,
			&gr.g.Subject.Name, &gr.g.Subject.Handle, &gr.g.Subject.AvatarURL, &gr.key, &gr.g.Subject.Email, &gr.g.Subject.MemberCount); err != nil {
			rows.Close()
			return nil, err
		}
		gr.g.Level = Level(lvl)
		if !showEmail {
			gr.g.Subject.Email = nil
		}
		if gr.key != nil {
			gr.g.Subject.AvatarURL = s.publicURL(gr.key)
		}
		all = append(all, gr)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// General access: the org grant on this resource, and the highest one
	// inherited.
	for _, gr := range all {
		if gr.g.Subject.Type != "org" {
			continue
		}
		lvl := gr.g.Level
		if depth[gr.src] == 0 {
			sh.General.OwnLevel = &lvl
		}
		if sh.General.Level == nil || lvl > *sh.General.Level {
			sh.General.Access, sh.General.Level = "org", &lvl
			sh.General.InheritedFrom = nil
			if depth[gr.src] > 0 {
				if sh.General.InheritedFrom, err = s.source(ctx, t.me, gr.src, cache); err != nil {
					return nil, err
				}
			}
		}
	}

	sh.PublicLinks = PublicLinks{Supported: t.kind.PublicPath != nil, Levels: []Level{View}}
	if t.kind.PublicComment {
		sh.PublicLinks.Levels = append(sh.PublicLinks.Levels, Comment)
	}
	if t.human < Manage {
		return sh, nil
	}

	for _, gr := range all {
		if gr.g.Subject.Type == "org" {
			continue
		}
		if depth[gr.src] > 0 {
			if gr.g.InheritedFrom, err = s.source(ctx, t.me, gr.src, cache); err != nil {
				return nil, err
			}
		}
		sh.Grants = append(sh.Grants, gr.g)
	}
	if sh.PublicLinks.Supported {
		missing, err := missingLinkPermission(ctx, s.d.Pool, t.kind, t.res.OrgID, t.me)
		if err != nil {
			return nil, err
		}
		sh.PublicLinks.Allowed = missing == ""
		if !sh.PublicLinks.Allowed {
			title := missing
			if p, ok := PermissionOf(missing); ok {
				title = p.Title
			}
			sh.PublicLinks.MissingPermission = &PermissionRef{Key: missing, Title: title}
		}
		sh.CanCreateLink = sh.PublicLinks.Allowed && t.level >= Manage
		if sh.Links, err = s.activeLinks(ctx, t.ref); err != nil {
			return nil, err
		}
		if len(links) > 1 {
			var src Ref
			err := s.d.Pool.QueryRow(ctx, `
				SELECT l.resource_kind, l.resource_id FROM access_links l
				JOIN unnest($1::text[], $2::uuid[]) AS c (k, i) ON c.k = l.resource_kind AND c.i = l.resource_id
				WHERE l.revoked_at IS NULL AND (l.expires_at IS NULL OR l.expires_at > now())
				  AND NOT (l.resource_kind = $3 AND l.resource_id = $4)
				LIMIT 1`, kinds, ids, t.ref.Kind, t.ref.ID).Scan(&src.Kind, &src.ID)
			if err == nil {
				if sh.PublicLinks.Inherited, err = s.source(ctx, t.me, src, cache); err != nil {
					return nil, err
				}
			} else if !db.IsNoRows(err) {
				return nil, err
			}
		}
	}
	return sh, nil
}

// ---- general access and inheritance ------------------------------------------

type generalReq struct {
	Access string `json:"access"` // restricted | org
	Level  *Level `json:"level"`
}

// setGeneral: PUT /v1/access/resources/{kind}/{id}/general {access, level}.
func (s *Service) setGeneral(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	var req generalReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.Access != "restricted" && req.Access != "org" {
		return httpx.BadRequest("access must be restricted or org.")
	}
	var shrank bool
	t, err := s.manage(ctx, r, "general", func(tx pgx.Tx, t *target) (map[string]any, error) {
		if t.res.OrgID == nil {
			return nil, httpx.BadRequest("Personal items can't be shared with " + orgs.NounA() + ".")
		}
		var before *int16
		err := tx.QueryRow(ctx, `SELECT level FROM access_grants WHERE resource_kind = $1 AND resource_id = $2 AND subject_type = 'org'`,
			t.ref.Kind, t.ref.ID).Scan(&before)
		if err != nil && !db.IsNoRows(err) {
			return nil, err
		}
		if req.Access == "restricted" {
			_, err = tx.Exec(ctx, `DELETE FROM access_grants WHERE resource_kind = $1 AND resource_id = $2 AND subject_type = 'org'`,
				t.ref.Kind, t.ref.ID)
			shrank = before != nil
			return map[string]any{"access": "restricted"}, err
		}
		if req.Level == nil || *req.Level < View || *req.Level > Edit || !t.kind.allows(*req.Level) {
			return nil, httpx.BadRequest("Choose view, comment or edit for everyone in the " + orgs.Noun() + ".")
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO access_grants (resource_kind, resource_id, subject_type, subject_id, level, granted_by)
			VALUES ($1, $2, 'org', $3, $4, $5)
			ON CONFLICT (resource_kind, resource_id, subject_type, subject_id)
			DO UPDATE SET level = EXCLUDED.level, granted_by = EXCLUDED.granted_by, updated_at = now()`,
			t.ref.Kind, t.ref.ID, *t.res.OrgID, int(*req.Level), t.me)
		shrank = before != nil && Level(*before) > *req.Level
		return map[string]any{"access": "org", "level": req.Level.String()}, err
	})
	if err != nil {
		return err
	}
	return s.afterChange(w, r, t, shrank, nil, nil)
}

type inheritReq struct {
	Inherits *bool `json:"inherits"`
}

// setInherit: PUT /v1/access/resources/{kind}/{id}/inherit {inherits}.
// Restricting keeps the caller's manage (they get a grant if they'd lose it).
func (s *Service) setInherit(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	var req inheritReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.Inherits == nil {
		return httpx.BadRequest("Send inherits: true or false.")
	}
	var shrank bool
	t, err := s.manage(ctx, r, "inherit", func(tx pgx.Tx, t *target) (map[string]any, error) {
		if t.res.Parent == nil {
			return nil, httpx.BadRequest("It has no parent to inherit from.")
		}
		if t.res.Inherits == *req.Inherits {
			return nil, nil
		}
		if _, err := tx.Exec(ctx, `UPDATE access_resources SET inherits = $3, updated_at = now() WHERE kind = $1 AND id = $2`,
			t.ref.Kind, t.ref.ID, *req.Inherits); err != nil {
			return nil, err
		}
		if !*req.Inherits {
			shrank = true
			var after int
			if err := tx.QueryRow(ctx, `SELECT access_level($1, $2, $3)`, t.me, t.ref.Kind, t.ref.ID).Scan(&after); err != nil {
				return nil, err
			}
			if Level(after) < Manage {
				if _, err := tx.Exec(ctx, `
					INSERT INTO access_grants (resource_kind, resource_id, subject_type, subject_id, level, granted_by)
					VALUES ($1, $2, 'user', $3, 4, $3)
					ON CONFLICT (resource_kind, resource_id, subject_type, subject_id) DO UPDATE SET level = 4, updated_at = now()`,
					t.ref.Kind, t.ref.ID, t.me); err != nil {
					return nil, err
				}
			}
		}
		return map[string]any{"inherits": *req.Inherits}, nil
	})
	if err != nil {
		return err
	}
	return s.afterChange(w, r, t, shrank, nil, nil)
}

// afterChange publishes, fires change hooks when access may have shrunk, and
// answers with the fresh sharing state.
func (s *Service) afterChange(w http.ResponseWriter, r *http.Request, t *target, shrank bool, users []uuid.UUID, notifyUsers []uuid.UUID) error {
	ctx := r.Context()
	bg := context.WithoutCancel(ctx)
	resourceChanged(bg, t.ref, t.res.OrgID, notifyUsers)
	if shrank {
		fireChanged(bg, []Ref{t.ref}, users)
	}
	fresh, err := s.load(ctx, s.d.Pool, r, View)
	if err != nil {
		// The caller just removed their own access.
		if e, ok := err.(*httpx.Error); ok && e.Status == http.StatusNotFound {
			httpx.NoContent(w)
			return nil
		}
		return err
	}
	sh, err := s.sharing(ctx, fresh)
	if err != nil {
		return err
	}
	httpx.OK(w, sh)
	return nil
}

// ---- grants ------------------------------------------------------------------

// SubjectRef names a person or team in a share request (AddGrant also takes
// the org: general access).
type SubjectRef struct {
	Type string    `json:"type"` // user | team (| org in AddGrant)
	ID   uuid.UUID `json:"id"`
}

type addGrantsReq struct {
	Subjects []SubjectRef `json:"subjects"`
	Level    Level        `json:"level"`
	Notify   *bool        `json:"notify"` // default true
}

// addGrants: POST /v1/access/resources/{kind}/{id}/grants {subjects, level,
// notify?}. Sets the level for each person or team (adding or changing) and
// notifies the newly added.
func (s *Service) addGrants(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	var req addGrantsReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if len(req.Subjects) == 0 || len(req.Subjects) > maxGrantsPerRequest {
		return httpx.BadRequest(fmt.Sprintf("Share with 1 to %d people or teams at a time.", maxGrantsPerRequest))
	}
	var (
		users, teams []uuid.UUID
		newUsers     []uuid.UUID // people to notify
		affected     []uuid.UUID // people whose access changed
		lowered      []uuid.UUID
		shrank       bool
		before       map[uuid.UUID]Level
	)
	for _, sub := range req.Subjects {
		switch sub.Type {
		case "user":
			users = append(users, sub.ID)
		case "team":
			teams = append(teams, sub.ID)
		case "org":
			return httpx.BadRequest("Use general access to share with everyone in the " + orgs.Noun() + ".")
		default:
			return httpx.BadRequest("subjects[].type must be user or team.")
		}
	}
	users, teams = dedupe(users), dedupe(teams)
	t, err := s.manage(ctx, r, "grant", func(tx pgx.Tx, t *target) (map[string]any, error) {
		if req.Level < View || req.Level > Manage || !t.kind.allows(req.Level) {
			return nil, httpx.BadRequest("That access level isn't available here.")
		}
		if err := checkSubjects(ctx, tx, t.res.OrgID, users, teams); err != nil {
			return nil, err
		}
		if t.res.OwnerID != nil {
			users = without(users, *t.res.OwnerID) // the owner already has full control
		}
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM access_grants WHERE resource_kind = $1 AND resource_id = $2`,
			t.ref.Kind, t.ref.ID).Scan(&count); err != nil {
			return nil, err
		}
		if count+len(users)+len(teams) > maxGrantsPerRes {
			return nil, httpx.Conflict("This item is shared with as many people and teams as it can be. Use a team or general access.")
		}
		// Levels before the change, so only people who gain are notified.
		var err error
		if before, err = levelsBefore(ctx, tx, t.ref, users, teams); err != nil {
			return nil, err
		}
		rows, err := tx.Query(ctx, `
			WITH prev AS (
				SELECT subject_type, subject_id, level FROM access_grants WHERE resource_kind = $1 AND resource_id = $2
			), ins AS (
				INSERT INTO access_grants (resource_kind, resource_id, subject_type, subject_id, level, granted_by)
				SELECT $1::text, $2::uuid, 'user', unnest($3::uuid[]), $5::smallint, $6::uuid
				UNION ALL SELECT $1::text, $2::uuid, 'team', unnest($4::uuid[]), $5::smallint, $6::uuid
				ON CONFLICT (resource_kind, resource_id, subject_type, subject_id)
				DO UPDATE SET level = EXCLUDED.level, granted_by = EXCLUDED.granted_by, updated_at = now()
				RETURNING subject_type, subject_id
			)
			SELECT ins.subject_type, ins.subject_id, prev.level FROM ins
			LEFT JOIN prev ON prev.subject_type = ins.subject_type AND prev.subject_id = ins.subject_id`,
			t.ref.Kind, t.ref.ID, users, teams, int(req.Level), t.me)
		if err != nil {
			return nil, err
		}
		type res struct {
			Type string
			ID   uuid.UUID
			Prev *int16
		}
		changed, err := pgx.CollectRows(rows, pgx.RowToStructByPos[res])
		if err != nil {
			return nil, err
		}
		var candidates []uuid.UUID // newly added people, to notify if their level went up
		for _, c := range changed {
			ids := []uuid.UUID{c.ID}
			if c.Type == "team" {
				if ids, err = TeamMemberIDs(ctx, tx, c.ID); err != nil {
					return nil, err
				}
			}
			affected = append(affected, ids...)
			switch {
			case c.Prev == nil:
				if c.Type == "user" || len(ids) <= notifyTeamCap {
					candidates = append(candidates, ids...)
				}
			case Level(*c.Prev) > req.Level:
				shrank = true
				lowered = append(lowered, ids...)
			}
		}
		candidates = without(dedupe(candidates), t.me)
		for _, u := range candidates {
			if before[u] < req.Level {
				newUsers = append(newUsers, u)
			}
		}
		return map[string]any{"user_ids": users, "team_ids": teams, "level": req.Level.String()}, nil
	})
	if err != nil {
		return err
	}
	if req.Notify == nil || *req.Notify {
		s.notifyShared(context.WithoutCancel(ctx), t, newUsers, req.Level)
	}
	return s.afterChange(w, r, t, shrank, dedupe(lowered), dedupe(affected))
}

// AddGrant gives subject at least level on ref, in the feature's transaction,
// for a feature's own flows: the creator's manage on something otherwise only
// org admins manage, an import, a seed. An existing grant to the subject is
// raised, never lowered. Access only grows, so nothing is needed after commit.
// See SetGrant for the subjects and checks.
func AddGrant(ctx context.Context, q db.Querier, ref Ref, subject SubjectRef, level Level, by uuid.UUID) error {
	return putGrant(ctx, q, ref, subject, level, by, true)
}

// SetGrant sets subject's grant on ref to exactly level (adding, raising or
// lowering it), in the feature's transaction: a feature's structural sharing
// (a team's edit on its records, a workspace's general access). subject is a
// person or team of the resource's org, or the org itself ({Type: "org",
// ID: orgID}: general access, up to Edit); the owner already has full
// control, so a grant to them is skipped. by may be uuid.Nil (the system).
//
// It applies the share dialog's checks (the kind offers the level, people are
// active members, teams belong to the org, at most 500 grants) and answers
// their 400/409 errors, but not the caller's own level: Require(…, Manage)
// first when a person asked for it. It doesn't notify, audit or publish. If
// it may have lowered a level, call Changed(ctx, ref, nil) after commit.
func SetGrant(ctx context.Context, q db.Querier, ref Ref, subject SubjectRef, level Level, by uuid.UUID) error {
	return putGrant(ctx, q, ref, subject, level, by, false)
}

// RemoveGrant deletes subject's grant on ref (a person, team or the org: org
// removes general access), in the feature's transaction; no grant is not an
// error. Inherited access and the owner's are untouched. After commit call
// Changed(ctx, ref, nil) so features drop subscriptions people lost.
func RemoveGrant(ctx context.Context, q db.Querier, ref Ref, subject SubjectRef) error {
	if _, ok := KindOf(ref.Kind); !ok {
		return fmt.Errorf("%w: %s", ErrUnknownKind, ref.Kind)
	}
	if subject.Type != "user" && subject.Type != "team" && subject.Type != "org" {
		return httpx.BadRequest("The subject type must be user, team or org.")
	}
	_, err := q.Exec(ctx, `DELETE FROM access_grants WHERE resource_kind = $1 AND resource_id = $2 AND subject_type = $3 AND subject_id = $4`,
		ref.Kind, ref.ID, subject.Type, subject.ID)
	return err
}

// SetInherits sets whether ref inherits its parent's access (false:
// restricted, "only people added here"), in the feature's transaction, for a
// feature's own flows (an import that keeps a folder's restriction). Unlike
// the inherit route it adds no grant: give the people who must keep access
// one (AddGrant) first. On an item without a parent the flag takes effect
// when it moves under one. It doesn't notify, audit or publish; when it
// restricts, call Changed(ctx, ref, nil) after commit.
func SetInherits(ctx context.Context, q db.Querier, ref Ref, inherits bool) error {
	if _, ok := KindOf(ref.Kind); !ok {
		return fmt.Errorf("%w: %s", ErrUnknownKind, ref.Kind)
	}
	res, err := Lookup(ctx, q, ref)
	if err != nil {
		return err
	}
	if res.Inherits == inherits {
		return nil
	}
	_, err = q.Exec(ctx, `UPDATE access_resources SET inherits = $3, updated_at = now() WHERE kind = $1 AND id = $2`,
		ref.Kind, ref.ID, inherits)
	return err
}

// putGrant is AddGrant (raiseOnly) and SetGrant.
func putGrant(ctx context.Context, q db.Querier, ref Ref, subject SubjectRef, level Level, by uuid.UUID, raiseOnly bool) error {
	k, ok := KindOf(ref.Kind)
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownKind, ref.Kind)
	}
	if _, err := q.Exec(ctx, `SELECT 1 FROM access_resources WHERE kind = $1 AND id = $2 FOR UPDATE`, ref.Kind, ref.ID); err != nil {
		return err
	}
	res, err := Lookup(ctx, q, ref)
	if err != nil {
		return err
	}
	if level < View || level > Manage || !k.allows(level) {
		return httpx.BadRequest("That access level isn't available here.")
	}
	switch subject.Type {
	case "user":
		if res.OwnerID != nil && *res.OwnerID == subject.ID {
			return nil
		}
		err = checkSubjects(ctx, q, res.OrgID, []uuid.UUID{subject.ID}, nil)
	case "team":
		err = checkSubjects(ctx, q, res.OrgID, nil, []uuid.UUID{subject.ID})
	case "org":
		switch {
		case res.OrgID == nil:
			err = httpx.BadRequest("Personal items can't be shared with " + orgs.NounA() + ".")
		case subject.ID != *res.OrgID:
			err = httpx.BadRequest("General access is for the item's own " + orgs.Noun() + ".")
		case level > Edit:
			err = httpx.BadRequest("Choose view, comment or edit for everyone in the " + orgs.Noun() + ".")
		}
	default:
		err = httpx.BadRequest("The subject type must be user, team or org.")
	}
	if err != nil {
		return err
	}
	if subject.Type != "org" {
		var count int
		var has bool
		if err := q.QueryRow(ctx, `
			SELECT count(*), COALESCE(bool_or(subject_type = $3 AND subject_id = $4), false)
			FROM access_grants WHERE resource_kind = $1 AND resource_id = $2`,
			ref.Kind, ref.ID, subject.Type, subject.ID).Scan(&count, &has); err != nil {
			return err
		}
		if !has && count >= maxGrantsPerRes {
			return httpx.Conflict("This item is shared with as many people and teams as it can be. Use a team or general access.")
		}
	}
	_, err = q.Exec(ctx, `
		INSERT INTO access_grants (resource_kind, resource_id, subject_type, subject_id, level, granted_by)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (resource_kind, resource_id, subject_type, subject_id)
		DO UPDATE SET level = EXCLUDED.level, granted_by = EXCLUDED.granted_by, updated_at = now()
		WHERE access_grants.level <> EXCLUDED.level AND (NOT $7 OR access_grants.level < EXCLUDED.level)`,
		ref.Kind, ref.ID, subject.Type, subject.ID, int(level), nullUUID(by), raiseOnly)
	return err
}

// levelsBefore returns the current level of the people named and the members
// of the teams named (capped like team notifications).
func levelsBefore(ctx context.Context, q db.Querier, ref Ref, users, teams []uuid.UUID) (map[uuid.UUID]Level, error) {
	rows, err := q.Query(ctx, `
		WITH people AS (
			SELECT unnest($3::uuid[]) AS user_id
			UNION
			SELECT tm.user_id FROM access_team_members tm WHERE tm.team_id = ANY ($4)
			  AND (SELECT count(*) FROM access_team_members x WHERE x.team_id = tm.team_id) <= $5
		)
		SELECT user_id, access_level(user_id, $1, $2) FROM people`, ref.Kind, ref.ID, users, teams, notifyTeamCap)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]Level{}
	for rows.Next() {
		var u uuid.UUID
		var l int
		if err := rows.Scan(&u, &l); err != nil {
			return nil, err
		}
		out[u] = Level(l)
	}
	return out, rows.Err()
}

// checkSubjects: people must be active members of the resource's org (any
// active account for personal resources, org nil); teams must belong to the
// org.
func checkSubjects(ctx context.Context, q db.Querier, org *uuid.UUID, users, teams []uuid.UUID) error {
	if len(users) > 0 {
		var n int
		var err error
		if org != nil {
			err = q.QueryRow(ctx, `
				SELECT count(*) FROM org_members m JOIN users u ON u.id = m.user_id
				WHERE m.org_id = $1 AND m.user_id = ANY ($2) AND u.status = 'active'`, *org, users).Scan(&n)
		} else {
			err = q.QueryRow(ctx, `SELECT count(*) FROM users WHERE id = ANY ($1) AND status = 'active'`, users).Scan(&n)
		}
		if err != nil {
			return err
		}
		if n != len(users) {
			return httpx.BadRequest("You can share only with active members of this " + orgs.Noun() + ".")
		}
	}
	if len(teams) > 0 {
		if org == nil {
			return httpx.BadRequest("Personal items can't be shared with teams.")
		}
		var n int
		if err := q.QueryRow(ctx, `SELECT count(*) FROM access_teams WHERE org_id = $1 AND id = ANY ($2)`, *org, teams).Scan(&n); err != nil {
			return err
		}
		if n != len(teams) {
			return httpx.BadRequest("Choose teams from this " + orgs.Noun() + ".")
		}
	}
	return nil
}

func (s *Service) notifyShared(ctx context.Context, t *target, users []uuid.UUID, level Level) {
	users = s.mayReach(ctx, t, users)
	if len(users) == 0 {
		return
	}
	title, err := t.kind.Title(ctx, s.d.Pool, t.ref.ID)
	if err != nil || title == "" {
		title = "Untitled " + t.kind.Label
	}
	var actor string
	_ = s.d.Pool.QueryRow(ctx, `SELECT display_name FROM users WHERE id = $1`, t.me).Scan(&actor)
	if actor == "" {
		actor = "Someone"
	}
	for _, u := range users {
		send(ctx, notify.Notification{
			UserID: u, Kind: KindShared,
			Title: actor + " shared “" + title + "” with you",
			Body:  "You can " + verb(level) + " this " + t.kind.Label + ".",
			Link:  t.kind.Link(t.ref.ID),
			Data:  map[string]any{"kind": t.ref.Kind, "id": t.ref.ID, "level": level.String()},
		})
	}
}

// mayReach drops the people a kind's Permission keeps out of the resource's
// org (a share must not tell them its title).
func (s *Service) mayReach(ctx context.Context, t *target, users []uuid.UUID) []uuid.UUID {
	if t.kind.Permission == "" || t.res.OrgID == nil {
		return users
	}
	out := users[:0:0]
	for _, u := range users {
		if ok, err := Allowed(ctx, s.d.Pool, *t.res.OrgID, u, t.kind.Permission); err == nil && ok {
			out = append(out, u)
		}
	}
	return out
}

type updateGrantReq struct {
	Level Level `json:"level"`
}

// updateGrant: PATCH /v1/access/resources/{kind}/{id}/grants/{grantID} {level}.
func (s *Service) updateGrant(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	grantID, err := httpx.URLParamUUID(r, "grantID")
	if err != nil {
		return err
	}
	var req updateGrantReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	var users []uuid.UUID
	var shrank bool
	t, err := s.manage(ctx, r, "grant_update", func(tx pgx.Tx, t *target) (map[string]any, error) {
		if req.Level < View || req.Level > Manage || !t.kind.allows(req.Level) {
			return nil, httpx.BadRequest("That access level isn't available here.")
		}
		g, err := grantOn(ctx, tx, t.ref, grantID)
		if err != nil {
			return nil, err
		}
		if g.Type == "org" {
			return nil, httpx.BadRequest("Change general access instead.")
		}
		if _, err := tx.Exec(ctx, `UPDATE access_grants SET level = $2, granted_by = $3, updated_at = now() WHERE id = $1`,
			grantID, int(req.Level), t.me); err != nil {
			return nil, err
		}
		if users, err = subjectUsers(ctx, tx, g.Type, g.ID); err != nil {
			return nil, err
		}
		shrank = Level(g.Level) > req.Level
		return map[string]any{"grant_id": grantID, "subject_type": g.Type, "subject_id": g.ID, "level": req.Level.String()}, nil
	})
	if err != nil {
		return err
	}
	return s.afterChange(w, r, t, shrank, users, users)
}

// removeGrant: DELETE /v1/access/resources/{kind}/{id}/grants/{grantID}.
// Managers remove anyone; anyone may remove their own grant.
func (s *Service) removeGrant(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	grantID, err := httpx.URLParamUUID(r, "grantID")
	if err != nil {
		return err
	}
	me := auth.MustFromContext(ctx).UserID
	// Your own grant: no manage needed.
	k, ref, err := refParams(r)
	if err != nil {
		return err
	}
	if _, err := Require(ctx, s.d.Pool, me, ref.Kind, ref.ID, View); err != nil {
		return err
	}
	g, err := grantOn(ctx, s.d.Pool, ref, grantID)
	if err != nil {
		return err
	}
	if g.Type == "user" && g.ID == me {
		if _, err := s.d.Pool.Exec(ctx, `DELETE FROM access_grants WHERE id = $1`, grantID); err != nil {
			return err
		}
		res, err := Lookup(ctx, s.d.Pool, ref)
		if err != nil {
			return err
		}
		t := &target{kind: k, ref: ref, res: res, me: me}
		return s.afterChange(w, r, t, true, []uuid.UUID{me}, []uuid.UUID{me})
	}
	var users []uuid.UUID
	t, err := s.manage(ctx, r, "grant_remove", func(tx pgx.Tx, t *target) (map[string]any, error) {
		g, err := grantOn(ctx, tx, t.ref, grantID)
		if err != nil {
			return nil, err
		}
		if g.Type == "org" {
			return nil, httpx.BadRequest("Change general access instead.")
		}
		if users, err = subjectUsers(ctx, tx, g.Type, g.ID); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM access_grants WHERE id = $1`, grantID); err != nil {
			return nil, err
		}
		return map[string]any{"grant_id": grantID, "subject_type": g.Type, "subject_id": g.ID}, nil
	})
	if err != nil {
		return err
	}
	return s.afterChange(w, r, t, true, users, users)
}

type grantRef struct {
	Type  string
	ID    uuid.UUID
	Level int16
}

func grantOn(ctx context.Context, q db.Querier, ref Ref, grantID uuid.UUID) (*grantRef, error) {
	var g grantRef
	err := q.QueryRow(ctx, `SELECT subject_type, subject_id, level FROM access_grants
		WHERE id = $1 AND resource_kind = $2 AND resource_id = $3`, grantID, ref.Kind, ref.ID).Scan(&g.Type, &g.ID, &g.Level)
	if db.IsNoRows(err) {
		return nil, httpx.NotFound("That person or team doesn't have access here.")
	}
	if err != nil {
		return nil, err
	}
	return &g, nil
}

func subjectUsers(ctx context.Context, q db.Querier, typ string, id uuid.UUID) ([]uuid.UUID, error) {
	switch typ {
	case "user":
		return []uuid.UUID{id}, nil
	case "team":
		return TeamMemberIDs(ctx, q, id)
	}
	return nil, nil
}

func without(ids []uuid.UUID, drop uuid.UUID) []uuid.UUID {
	out := ids[:0:0]
	for _, id := range ids {
		if id != drop {
			out = append(out, id)
		}
	}
	return out
}

// ---- ownership ---------------------------------------------------------------

type ownerReq struct {
	UserID uuid.UUID `json:"user_id"`
}

// transferOwner: PUT /v1/access/resources/{kind}/{id}/owner {user_id}. The
// owner, or an org owner/admin who manages it, hands it to a member; the
// previous owner keeps manage.
func (s *Service) transferOwner(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	var req ownerReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.UserID == uuid.Nil {
		return httpx.BadRequest("Choose who should own it.")
	}
	t, err := s.manage(ctx, r, "owner", func(tx pgx.Tx, t *target) (map[string]any, error) {
		if !t.isOwner() && !orgs.RoleAtLeast(t.orgRole, orgs.RoleAdmin) {
			return nil, httpx.Forbidden("Only the owner or " + orgs.NounA() + " admin can change the owner.")
		}
		if t.res.OwnerID != nil && *t.res.OwnerID == req.UserID {
			return nil, nil
		}
		if err := checkSubjects(ctx, tx, t.res.OrgID, []uuid.UUID{req.UserID}, nil); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE access_resources SET owner_id = $3, updated_at = now() WHERE kind = $1 AND id = $2`,
			t.ref.Kind, t.ref.ID, req.UserID); err != nil {
			return nil, err
		}
		// The new owner needs no grant; the previous one keeps manage.
		if _, err := tx.Exec(ctx, `DELETE FROM access_grants WHERE resource_kind = $1 AND resource_id = $2 AND subject_type = 'user' AND subject_id = $3`,
			t.ref.Kind, t.ref.ID, req.UserID); err != nil {
			return nil, err
		}
		if prev := t.res.OwnerID; prev != nil {
			var still bool
			if t.res.OrgID == nil {
				still = true
			} else if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM org_members WHERE org_id = $1 AND user_id = $2)`,
				*t.res.OrgID, *prev).Scan(&still); err != nil {
				return nil, err
			}
			if still {
				if _, err := tx.Exec(ctx, `
					INSERT INTO access_grants (resource_kind, resource_id, subject_type, subject_id, level, granted_by)
					VALUES ($1, $2, 'user', $3, 4, $4)
					ON CONFLICT (resource_kind, resource_id, subject_type, subject_id) DO UPDATE SET level = 4, updated_at = now()`,
					t.ref.Kind, t.ref.ID, *prev, t.me); err != nil {
					return nil, err
				}
			}
		}
		return map[string]any{"from": t.res.OwnerID, "to": req.UserID}, nil
	})
	if err != nil {
		return err
	}
	if req.UserID != t.me && len(s.mayReach(ctx, t, []uuid.UUID{req.UserID})) == 1 {
		bg := context.WithoutCancel(ctx)
		title, err := t.kind.Title(bg, s.d.Pool, t.ref.ID)
		if err != nil || title == "" {
			title = "Untitled " + t.kind.Label
		}
		send(bg, notify.Notification{UserID: req.UserID, Kind: KindShared, Title: "You now own “" + title + "”",
			Link: t.kind.Link(t.ref.ID), Data: map[string]any{"kind": t.ref.Kind, "id": t.ref.ID, "level": Manage.String()}})
	}
	return s.afterChange(w, r, t, false, nil, []uuid.UUID{req.UserID})
}
