package access

import (
	"context"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/text/unicode/norm"

	"scaffoldapp/backend/internal/audit"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/notify"
	"scaffoldapp/backend/internal/orgs"
	"scaffoldapp/backend/internal/realtime"
	"scaffoldapp/backend/internal/storage"
)

// Team roles.
const (
	TeamLead   = "lead"
	TeamMember = "member"
)

// KindTeam is the notification for being added to a team.
const KindTeam = "access.team"

// Team is a team as an org member sees it.
type Team struct {
	ID          uuid.UUID `json:"id"`
	OrgID       uuid.UUID `json:"org_id"`
	Name        string    `json:"name"`
	Handle      string    `json:"handle"`
	Description string    `json:"description"`
	AvatarURL   *string   `json:"avatar_url"`
	MemberCount int       `json:"member_count"`
	// MyRole is the caller's role on the team: lead, member, or null.
	MyRole *string `json:"my_role"`
	// CanManage: rename, change the picture, delete (access.manage_teams).
	CanManage bool `json:"can_manage"`
	// CanManageMembers: add, remove, make lead (manage_teams or a lead).
	CanManageMembers bool      `json:"can_manage_members"`
	CreatedAt        time.Time `json:"created_at"`
}

// TeamMemberRow is one person on a team. Email is shown to org admins only,
// as in the orgs member list.
type TeamMemberRow struct {
	UserID      uuid.UUID `json:"user_id"`
	DisplayName string    `json:"display_name"`
	Email       *string   `json:"email"`
	AvatarURL   *string   `json:"avatar_url"`
	Role        string    `json:"role"`
	JoinedAt    time.Time `json:"joined_at"`
}

// TeamRef is the short form other features use (mentions, pickers).
type TeamRef struct {
	ID     uuid.UUID `json:"id"`
	OrgID  uuid.UUID `json:"org_id"`
	Name   string    `json:"name"`
	Handle string    `json:"handle"`
}

// TeamByHandle finds a team by its handle in an org ("design" for @design).
// It returns nil, nil when there is none.
func TeamByHandle(ctx context.Context, q db.Querier, orgID uuid.UUID, handle string) (*TeamRef, error) {
	var t TeamRef
	err := q.QueryRow(ctx, `SELECT id, org_id, name, handle FROM access_teams WHERE org_id = $1 AND handle = $2`,
		orgID, strings.ToLower(strings.TrimPrefix(strings.TrimSpace(handle), "@"))).Scan(&t.ID, &t.OrgID, &t.Name, &t.Handle)
	if db.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// TeamMemberIDs lists a team's members (to notify a mentioned team).
func TeamMemberIDs(ctx context.Context, q db.Querier, teamID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := q.Query(ctx, `SELECT user_id FROM access_team_members WHERE team_id = $1 ORDER BY created_at, user_id`, teamID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

// TeamsOf lists the teams userID is on in orgID.
func TeamsOf(ctx context.Context, q db.Querier, orgID, userID uuid.UUID) ([]TeamRef, error) {
	rows, err := q.Query(ctx, `
		SELECT t.id, t.org_id, t.name, t.handle FROM access_team_members tm JOIN access_teams t ON t.id = tm.team_id
		WHERE t.org_id = $1 AND tm.user_id = $2 ORDER BY lower(t.name), t.id`, orgID, userID)
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[TeamRef])
	if list == nil {
		list = []TeamRef{}
	}
	return list, err
}

// ---- loading and authorization -----------------------------------------------

const teamSelect = `
	SELECT t.id, t.org_id, t.name, t.handle, t.description, t.avatar_key,
	       (SELECT count(*) FROM access_team_members x WHERE x.team_id = t.id)::int,
	       (SELECT tm.role FROM access_team_members tm WHERE tm.team_id = t.id AND tm.user_id = $1),
	       t.created_at
	FROM access_teams t`

func (s *Service) scanTeams(rows pgx.Rows) ([]Team, error) {
	defer rows.Close()
	out := []Team{}
	for rows.Next() {
		var t Team
		var key *string
		if err := rows.Scan(&t.ID, &t.OrgID, &t.Name, &t.Handle, &t.Description, &key, &t.MemberCount, &t.MyRole, &t.CreatedAt); err != nil {
			return nil, err
		}
		t.AvatarURL = s.publicURL(key)
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Service) publicURL(key *string) *string {
	if key == nil || *key == "" {
		return nil
	}
	u := s.d.Storage.PublicURL(*key)
	return &u
}

// teamFor loads a team for a member of its org (404 otherwise) and fills the
// caller's abilities.
func (s *Service) teamFor(ctx context.Context, q db.Querier, teamID, userID uuid.UUID) (*Team, string, error) {
	rows, err := q.Query(ctx, teamSelect+` WHERE t.id = $2`, userID, teamID)
	if err != nil {
		return nil, "", err
	}
	list, err := s.scanTeams(rows)
	if err != nil {
		return nil, "", err
	}
	if len(list) == 0 {
		return nil, "", errTeamNotFound
	}
	t := list[0]
	role, err := orgs.RoleOf(ctx, q, t.OrgID, userID)
	if err != nil {
		return nil, "", err
	}
	if role == "" {
		return nil, "", errTeamNotFound
	}
	if t.CanManage, err = Allowed(ctx, q, t.OrgID, userID, PermManageTeams); err != nil {
		return nil, "", err
	}
	t.CanManageMembers = t.CanManage || (t.MyRole != nil && *t.MyRole == TeamLead)
	return &t, role, nil
}

var errTeamNotFound = httpx.NotFound("Team not found.")

func teamIDParam(r *http.Request) (uuid.UUID, error) {
	id, err := httpx.URLParamUUID(r, "teamID")
	if err != nil {
		return uuid.Nil, errTeamNotFound
	}
	return id, nil
}

// ---- routes ------------------------------------------------------------------

// listTeams: GET /v1/access/orgs/{orgID}/teams?q=&mine=true&cursor= (members),
// by name.
func (s *Service) listTeams(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	m, _ := orgs.FromContext(ctx)
	p := httpx.ParsePage(r)
	var c struct {
		N  string    `json:"n"`
		ID uuid.UUID `json:"id"`
	}
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	mine := r.URL.Query().Get("mine") == "true"
	rows, err := s.d.Pool.Query(ctx, teamSelect+`
		WHERE t.org_id = $2
		  AND ($3 = '' OR t.name ILIKE '%' || $3 || '%' OR t.handle ILIKE $3 || '%')
		  AND (NOT $4 OR EXISTS (SELECT 1 FROM access_team_members tm WHERE tm.team_id = t.id AND tm.user_id = $1))
		  AND ($5::uuid IS NULL OR (lower(t.name), t.id) > ($6, $5))
		ORDER BY lower(t.name), t.id LIMIT $7`,
		me, m.OrgID, likeEscape(q), mine, nullUUID(c.ID), c.N, p.Limit+1)
	if err != nil {
		return err
	}
	list, err := s.scanTeams(rows)
	if err != nil {
		return err
	}
	canManage, err := Allowed(ctx, s.d.Pool, m.OrgID, me, PermManageTeams)
	if err != nil {
		return err
	}
	for i := range list {
		list[i].CanManage = canManage
		list[i].CanManageMembers = canManage || (list[i].MyRole != nil && *list[i].MyRole == TeamLead)
	}
	page := httpx.NewPage(list, p.Limit, func(t Team) any { return map[string]any{"n": strings.ToLower(t.Name), "id": t.ID} })
	httpx.OK(w, map[string]any{"items": page.Items, "next_cursor": page.NextCursor, "can_create": canManage})
	return nil
}

type teamReq struct {
	Name        *string `json:"name"`
	Handle      *string `json:"handle"`
	Description *string `json:"description"`
	AvatarKey   *string `json:"avatar_key"` // "" removes the picture
}

// createTeam: POST /v1/access/orgs/{orgID}/teams (access.manage_teams). The
// creator joins as lead. Audited.
func (s *Service) createTeam(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	m, _ := orgs.FromContext(ctx)
	var req teamReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.Name == nil {
		return httpx.BadRequest("Name the team.")
	}
	name, err := cleanTeamName(*req.Name)
	if err != nil {
		return err
	}
	handle := handleFrom(name)
	explicit := req.Handle != nil && strings.TrimSpace(*req.Handle) != ""
	if explicit {
		if handle, err = cleanHandle(*req.Handle); err != nil {
			return err
		}
	}
	desc := ""
	if req.Description != nil {
		if desc, err = cleanDescription(*req.Description); err != nil {
			return err
		}
	}
	if req.AvatarKey != nil && *req.AvatarKey != "" && !storage.KeyOwnedBy(*req.AvatarKey, TeamAvatarPurpose, me) {
		return httpx.BadRequest("Upload the picture first (purpose access_team_avatar), then send its key.")
	}
	var teamID uuid.UUID
	for attempt := 0; ; attempt++ {
		candidate := handle
		if attempt > 0 {
			candidate = trimHandle(handle, 27) + "-" + randomSuffix()
		}
		err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
			var n int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM access_teams WHERE org_id = $1`, m.OrgID).Scan(&n); err != nil {
				return err
			}
			if n >= maxTeamsPerOrg {
				return httpx.Conflict("This " + orgs.Noun() + " has the most teams it can have. Delete one first.")
			}
			if err := tx.QueryRow(ctx, `
				INSERT INTO access_teams (org_id, name, handle, description, avatar_key, created_by)
				VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6) RETURNING id`,
				m.OrgID, name, candidate, desc, deref(req.AvatarKey), me).Scan(&teamID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO access_team_members (team_id, user_id, role, added_by) VALUES ($1, $2, 'lead', $2)`,
				teamID, me); err != nil {
				return err
			}
			return audit.Log(ctx, tx, me, "access.team.create", "access_team", teamID.String(),
				map[string]any{"org_id": m.OrgID, "name": name, "handle": candidate})
		})
		if !db.IsUniqueViolation(err, "access_teams_handle_key") {
			break
		}
		if explicit {
			return httpx.Conflict("@" + handle + " is taken in this " + orgs.Noun() + ". Try another handle.")
		}
		if attempt >= 4 {
			return httpx.Conflict("Couldn't find a free handle. Choose one.")
		}
	}
	if err != nil {
		return err
	}
	bg := context.WithoutCancel(ctx)
	publish(bg, orgTopic(m.OrgID), EventTeamsChanged, map[string]any{"org_id": m.OrgID, "team_id": teamID})
	publish(bg, realtime.UserTopic(me), EventTeamMembersChanged, map[string]any{"org_id": m.OrgID, "team_id": teamID, "user_id": me})
	t, _, err := s.teamFor(ctx, s.d.Pool, teamID, me)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, t)
	return nil
}

// getTeam: GET /v1/access/teams/{teamID} (org members).
func (s *Service) getTeam(w http.ResponseWriter, r *http.Request) error {
	teamID, err := teamIDParam(r)
	if err != nil {
		return err
	}
	t, _, err := s.teamFor(r.Context(), s.d.Pool, teamID, auth.MustFromContext(r.Context()).UserID)
	if err != nil {
		return err
	}
	httpx.OK(w, t)
	return nil
}

// updateTeam: PATCH /v1/access/teams/{teamID} {name?, handle?, description?,
// avatar_key?}. Name, handle and picture need access.manage_teams; leads may
// change the description. Audited when done through manage_teams.
func (s *Service) updateTeam(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	teamID, err := teamIDParam(r)
	if err != nil {
		return err
	}
	t, _, err := s.teamFor(ctx, s.d.Pool, teamID, me)
	if err != nil {
		return err
	}
	var req teamReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	identity := req.Name != nil || req.Handle != nil || req.AvatarKey != nil
	if identity && !t.CanManage {
		return permissionRequired(PermManageTeams)
	}
	if !t.CanManageMembers {
		return permissionRequired(PermManageTeams)
	}
	if req.Name != nil {
		n, err := cleanTeamName(*req.Name)
		if err != nil {
			return err
		}
		req.Name = &n
	}
	if req.Handle != nil {
		h, err := cleanHandle(*req.Handle)
		if err != nil {
			return err
		}
		req.Handle = &h
	}
	if req.Description != nil {
		d, err := cleanDescription(*req.Description)
		if err != nil {
			return err
		}
		req.Description = &d
	}
	if req.AvatarKey != nil && *req.AvatarKey != "" && !storage.KeyOwnedBy(*req.AvatarKey, TeamAvatarPurpose, me) {
		return httpx.BadRequest("Upload the picture first (purpose access_team_avatar), then send its key.")
	}
	var oldKey *string
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT avatar_key FROM access_teams WHERE id = $1 FOR UPDATE`, teamID).Scan(&oldKey); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE access_teams SET name = COALESCE($2, name), handle = COALESCE($3, handle),
			  description = COALESCE($4, description),
			  avatar_key = CASE WHEN $5::text IS NULL THEN avatar_key ELSE NULLIF($5, '') END,
			  updated_at = now()
			WHERE id = $1`, teamID, req.Name, req.Handle, req.Description, req.AvatarKey); err != nil {
			return err
		}
		if !t.CanManage {
			return nil
		}
		return audit.Log(ctx, tx, me, "access.team.update", "access_team", teamID.String(), map[string]any{
			"org_id": t.OrgID, "name": req.Name, "handle": req.Handle, "description_changed": req.Description != nil,
			"avatar_changed": req.AvatarKey != nil,
		})
	})
	if db.IsUniqueViolation(err, "access_teams_handle_key") {
		return httpx.Conflict("That handle is taken in this " + orgs.Noun() + ". Try another.")
	}
	if err != nil {
		return err
	}
	if req.AvatarKey != nil && oldKey != nil && *oldKey != *req.AvatarKey {
		if err := s.d.Storage.Delete(context.WithoutCancel(ctx), *oldKey); err != nil {
			slog.WarnContext(ctx, "access: delete old team picture", "team_id", teamID, "err", err)
		}
	}
	publish(context.WithoutCancel(ctx), orgTopic(t.OrgID), EventTeamsChanged, map[string]any{"org_id": t.OrgID, "team_id": teamID})
	t, _, err = s.teamFor(ctx, s.d.Pool, teamID, me)
	if err != nil {
		return err
	}
	httpx.OK(w, t)
	return nil
}

// deleteTeam: DELETE /v1/access/teams/{teamID} (access.manage_teams). Its
// memberships, grants and permission overrides go with it. Audited.
func (s *Service) deleteTeam(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	teamID, err := teamIDParam(r)
	if err != nil {
		return err
	}
	t, _, err := s.teamFor(ctx, s.d.Pool, teamID, me)
	if err != nil {
		return err
	}
	if !t.CanManage {
		return permissionRequired(PermManageTeams)
	}
	var (
		members []uuid.UUID
		refs    []Ref
		key     *string
	)
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT avatar_key FROM access_teams WHERE id = $1 FOR UPDATE`, teamID).Scan(&key); err != nil {
			return err
		}
		var err error
		if members, err = TeamMemberIDs(ctx, tx, teamID); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `DELETE FROM access_grants WHERE subject_type = 'team' AND subject_id = $1
			RETURNING resource_kind, resource_id`, teamID)
		if err != nil {
			return err
		}
		if refs, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Ref]); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM access_permission_subjects WHERE subject_type = 'team' AND subject_id = $1`, teamID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM access_teams WHERE id = $1`, teamID); err != nil {
			return err
		}
		return audit.Log(ctx, tx, me, "access.team.delete", "access_team", teamID.String(), map[string]any{
			"org_id": t.OrgID, "name": t.Name, "handle": t.Handle, "members": len(members), "grants": len(refs),
		})
	})
	if err != nil {
		return err
	}
	bg := context.WithoutCancel(ctx)
	if key != nil {
		if err := s.d.Storage.Delete(bg, *key); err != nil {
			slog.WarnContext(ctx, "access: delete team picture", "team_id", teamID, "err", err)
		}
	}
	publish(bg, orgTopic(t.OrgID), EventTeamsChanged, map[string]any{"org_id": t.OrgID, "team_id": teamID})
	for _, u := range members {
		publish(bg, realtime.UserTopic(u), EventTeamMembersChanged, map[string]any{"org_id": t.OrgID, "team_id": teamID, "user_id": u})
	}
	for _, ref := range refs {
		resourceChanged(bg, ref, &t.OrgID, members)
	}
	if len(members) > 0 {
		fireChanged(bg, refs, members)
		firePermissionChange(bg, t.OrgID, "", members)
	}
	httpx.NoContent(w)
	return nil
}

// listTeamMembers: GET /v1/access/teams/{teamID}/members (org members): leads
// first, then by join date.
func (s *Service) listTeamMembers(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	teamID, err := teamIDParam(r)
	if err != nil {
		return err
	}
	t, role, err := s.teamFor(ctx, s.d.Pool, teamID, me)
	if err != nil {
		return err
	}
	p := httpx.ParsePage(r)
	var c struct {
		R  int       `json:"r"`
		T  time.Time `json:"t"`
		ID uuid.UUID `json:"id"`
	}
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	list, err := s.teamMembers(ctx, s.d.Pool, t.ID, orgs.RoleAtLeast(role, orgs.RoleAdmin), &c.T, c.R, c.ID, p.Limit+1)
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(list, p.Limit, func(m TeamMemberRow) any {
		r := 2
		if m.Role == TeamLead {
			r = 1
		}
		return map[string]any{"r": r, "t": m.JoinedAt, "id": m.UserID}
	}))
	return nil
}

func (s *Service) teamMembers(ctx context.Context, q db.Querier, teamID uuid.UUID, showEmail bool, after *time.Time, afterRank int, afterID uuid.UUID, limit int) ([]TeamMemberRow, error) {
	const rankSQL = `CASE tm.role WHEN 'lead' THEN 1 ELSE 2 END`
	var t *time.Time
	if after != nil && !after.IsZero() {
		t = after
	}
	rows, err := q.Query(ctx, `
		SELECT tm.user_id, u.display_name, u.email, u.avatar_url, tm.role, tm.created_at
		FROM access_team_members tm JOIN users u ON u.id = tm.user_id
		WHERE tm.team_id = $1 AND ($2::timestamptz IS NULL OR (`+rankSQL+`, tm.created_at, tm.user_id) > ($3, $2, $4))
		ORDER BY `+rankSQL+`, tm.created_at, tm.user_id LIMIT $5`, teamID, t, afterRank, afterID, limit)
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[TeamMemberRow])
	if err != nil {
		return nil, err
	}
	if list == nil {
		list = []TeamMemberRow{}
	}
	if !showEmail {
		for i := range list {
			list[i].Email = nil
		}
	}
	return list, nil
}

type addMembersReq struct {
	UserIDs []uuid.UUID `json:"user_ids"`
	Role    string      `json:"role"`
}

// addTeamMembers: POST /v1/access/teams/{teamID}/members {user_ids, role}
// (manage_teams or a lead). Only org members; people already on the team keep
// their role. Notifies the people added.
func (s *Service) addTeamMembers(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	teamID, err := teamIDParam(r)
	if err != nil {
		return err
	}
	t, _, err := s.teamFor(ctx, s.d.Pool, teamID, me)
	if err != nil {
		return err
	}
	if !t.CanManageMembers {
		return httpx.Forbidden("Only team leads and people who manage teams can add members.")
	}
	var req addMembersReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.Role == "" {
		req.Role = TeamMember
	}
	if req.Role != TeamMember && req.Role != TeamLead {
		return httpx.BadRequest("role must be lead or member.")
	}
	ids := dedupe(req.UserIDs)
	if len(ids) == 0 || len(ids) > 100 {
		return httpx.BadRequest("Add 1 to 100 people at a time.")
	}
	var added []uuid.UUID
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT 1 FROM access_teams WHERE id = $1 FOR UPDATE`, teamID); err != nil {
			return err
		}
		var members int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM org_members WHERE org_id = $1 AND user_id = ANY ($2)`, t.OrgID, ids).Scan(&members); err != nil {
			return err
		}
		if members != len(ids) {
			return httpx.BadRequest("Only members of this " + orgs.Noun() + " can join its teams.")
		}
		var size int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM access_team_members WHERE team_id = $1`, teamID).Scan(&size); err != nil {
			return err
		}
		if size+len(ids) > maxTeamSize {
			return httpx.Conflict("A team can have up to 1,000 members.")
		}
		rows, err := tx.Query(ctx, `
			INSERT INTO access_team_members (team_id, user_id, role, added_by)
			SELECT $1, unnest($2::uuid[]), $3, $4
			ON CONFLICT (team_id, user_id) DO NOTHING RETURNING user_id`, teamID, ids, req.Role, me)
		if err != nil {
			return err
		}
		if added, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID]); err != nil {
			return err
		}
		if !t.overrideByManager() || len(added) == 0 {
			return nil
		}
		return audit.Log(ctx, tx, me, "access.team.members.add", "access_team", teamID.String(),
			map[string]any{"org_id": t.OrgID, "user_ids": added, "role": req.Role})
	})
	if err != nil {
		return err
	}
	bg := context.WithoutCancel(ctx)
	s.teamMembershipChanged(bg, t, added)
	for _, u := range added {
		if u == me {
			continue
		}
		send(bg, notify.Notification{UserID: u, Kind: KindTeam, Title: "You were added to @" + t.Handle,
			Body: t.Name, Link: "/settings/teams/" + t.ID.String(), Data: map[string]any{"team_id": t.ID, "org_id": t.OrgID}})
	}
	list, err := s.teamMembers(ctx, s.d.Pool, teamID, true, nil, 0, uuid.Nil, maxTeamSize)
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"added": len(added), "items": filterMembers(list, added)})
	return nil
}

// overrideByManager: the change happens through manage_teams, not as the
// team's lead (audited as an admin override).
func (t *Team) overrideByManager() bool {
	return t.CanManage && (t.MyRole == nil || *t.MyRole != TeamLead)
}

func filterMembers(list []TeamMemberRow, ids []uuid.UUID) []TeamMemberRow {
	out := []TeamMemberRow{}
	for _, m := range list {
		for _, id := range ids {
			if m.UserID == id {
				out = append(out, m)
			}
		}
	}
	return out
}

type teamMemberReq struct {
	Role string `json:"role"`
}

// updateTeamMember: PATCH /v1/access/teams/{teamID}/members/{userID} {role}
// (manage_teams or a lead).
func (s *Service) updateTeamMember(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	teamID, err := teamIDParam(r)
	if err != nil {
		return err
	}
	target, err := httpx.URLParamUUID(r, "userID")
	if err != nil {
		return err
	}
	t, _, err := s.teamFor(ctx, s.d.Pool, teamID, me)
	if err != nil {
		return err
	}
	if !t.CanManageMembers {
		return httpx.Forbidden("Only team leads and people who manage teams can change roles.")
	}
	var req teamMemberReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.Role != TeamMember && req.Role != TeamLead {
		return httpx.BadRequest("role must be lead or member.")
	}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE access_team_members SET role = $3, updated_at = now() WHERE team_id = $1 AND user_id = $2`,
			teamID, target, req.Role)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.NotFound("They aren't on this team.")
		}
		if !t.overrideByManager() {
			return nil
		}
		return audit.Log(ctx, tx, me, "access.team.members.role", "access_team", teamID.String(),
			map[string]any{"org_id": t.OrgID, "user_id": target, "role": req.Role})
	})
	if err != nil {
		return err
	}
	s.teamMembershipChanged(context.WithoutCancel(ctx), t, []uuid.UUID{target})
	list, err := s.teamMembers(ctx, s.d.Pool, teamID, true, nil, 0, uuid.Nil, maxTeamSize)
	if err != nil {
		return err
	}
	if m := filterMembers(list, []uuid.UUID{target}); len(m) == 1 {
		httpx.OK(w, m[0])
		return nil
	}
	return httpx.NotFound("They aren't on this team.")
}

// removeTeamMember: DELETE /v1/access/teams/{teamID}/members/{userID}. Your
// own id leaves; others need manage_teams or to be a lead. Access the team
// gave them ends at once.
func (s *Service) removeTeamMember(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	teamID, err := teamIDParam(r)
	if err != nil {
		return err
	}
	target, err := httpx.URLParamUUID(r, "userID")
	if err != nil {
		return err
	}
	t, _, err := s.teamFor(ctx, s.d.Pool, teamID, me)
	if err != nil {
		return err
	}
	if target != me && !t.CanManageMembers {
		return httpx.Forbidden("Only team leads and people who manage teams can remove members.")
	}
	var refs []Ref
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM access_team_members WHERE team_id = $1 AND user_id = $2`, teamID, target)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.NotFound("They aren't on this team.")
		}
		rows, err := tx.Query(ctx, `SELECT resource_kind, resource_id FROM access_grants WHERE subject_type = 'team' AND subject_id = $1 LIMIT $2`,
			teamID, expandCap)
		if err != nil {
			return err
		}
		if refs, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Ref]); err != nil {
			return err
		}
		if target == me || !t.overrideByManager() {
			return nil
		}
		return audit.Log(ctx, tx, me, "access.team.members.remove", "access_team", teamID.String(),
			map[string]any{"org_id": t.OrgID, "user_id": target})
	})
	if err != nil {
		return err
	}
	bg := context.WithoutCancel(ctx)
	s.teamMembershipChanged(bg, t, []uuid.UUID{target})
	for _, ref := range refs {
		resourceChanged(bg, ref, &t.OrgID, []uuid.UUID{target})
	}
	fireChanged(bg, refs, []uuid.UUID{target})
	firePermissionChange(bg, t.OrgID, "", []uuid.UUID{target})
	httpx.NoContent(w)
	return nil
}

func (s *Service) teamMembershipChanged(ctx context.Context, t *Team, users []uuid.UUID) {
	if len(users) == 0 {
		return
	}
	publish(ctx, orgTopic(t.OrgID), EventTeamMembersChanged, map[string]any{"org_id": t.OrgID, "team_id": t.ID})
	for _, u := range users {
		publish(ctx, realtime.UserTopic(u), EventTeamMembersChanged, map[string]any{"org_id": t.OrgID, "team_id": t.ID, "user_id": u})
	}
}

// ---- validation --------------------------------------------------------------

func cleanTeamName(raw string) (string, error) {
	name := strings.Join(strings.Fields(raw), " ")
	if n := utf8.RuneCountInString(name); n == 0 || n > 60 {
		return "", httpx.BadRequest("Team name must be 1 to 60 characters.")
	}
	return name, nil
}

func cleanDescription(raw string) (string, error) {
	d := strings.TrimSpace(raw)
	if utf8.RuneCountInString(d) > 280 {
		return "", httpx.BadRequest("Keep the description to 280 characters.")
	}
	return d, nil
}

var handlePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,30}[a-z0-9])?$`)

// reservedHandles are mention keywords features may give a meaning.
var reservedHandles = map[string]bool{"all": true, "everyone": true, "here": true, "channel": true, "team": true, "admins": true}

func cleanHandle(raw string) (string, error) {
	h := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(raw), "@"))
	if len(h) < 2 || !handlePattern.MatchString(h) || strings.Contains(h, "--") {
		return "", httpx.BadRequest("Handle must be 2 to 32 lowercase letters, numbers or single hyphens.")
	}
	if reservedHandles[h] {
		return "", httpx.Conflict("@" + h + " is reserved. Try another handle.")
	}
	return h, nil
}

// handleFrom turns a name into a handle: "Design & Brand" → "design-brand".
func handleFrom(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range norm.NFKD.String(strings.ToLower(name)) {
		switch {
		case unicode.Is(unicode.Mn, r):
		case r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	h := trimHandle(b.String(), 32)
	if len(h) < 2 || reservedHandles[h] {
		h = strings.Trim("team-"+h, "-")
		if len(h) < 2 {
			h = "team-" + randomSuffix()
		}
	}
	return h
}

func trimHandle(s string, n int) string {
	if len(s) > n {
		s = s[:n]
	}
	return strings.Trim(s, "-")
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// likeEscape escapes LIKE wildcards in user input.
func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
