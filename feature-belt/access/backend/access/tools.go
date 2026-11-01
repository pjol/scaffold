package access

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
)

// Tools exposes read-only views with the REST routes' rules: teams and
// permissions of orgs the caller belongs to, and a resource's sharing (its
// access list only for people who manage it).
func (s *Service) Tools() []app.Tool {
	return []app.Tool{
		{
			Name:        "access_list_my_teams",
			Description: "List the teams the user is on, with their role (lead or member). Pass org_id to limit to one " + orgs.Noun() + ".",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"org_id":{"type":"string","format":"uuid"}},"additionalProperties":false}`),
			ReadOnly:    true,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					OrgID *uuid.UUID `json:"org_id"`
				}
				if err := decodeInput(input, &in); err != nil {
					return nil, err
				}
				if in.OrgID != nil {
					if _, err := memberRole(ctx, s, *in.OrgID, caller.UserID); err != nil {
						return nil, err
					}
				}
				rows, err := s.d.Pool.Query(ctx, teamSelect+`
					JOIN access_team_members mine ON mine.team_id = t.id AND mine.user_id = $1
					WHERE ($2::uuid IS NULL OR t.org_id = $2)
					ORDER BY t.org_id, lower(t.name), t.id LIMIT 200`, caller.UserID, in.OrgID)
				if err != nil {
					return nil, err
				}
				return s.scanTeams(rows)
			},
		},
		{
			Name:        "access_list_team_members",
			Description: "List the members of a team in one of the user's " + orgs.Nouns() + ", leads first. Emails are included only for " + orgs.Noun() + " admins.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"team_id":{"type":"string","format":"uuid"}},"required":["team_id"],"additionalProperties":false}`),
			ReadOnly:    true,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					TeamID uuid.UUID `json:"team_id"`
				}
				if err := decodeInput(input, &in); err != nil || in.TeamID == uuid.Nil {
					return nil, httpx.BadRequest("team_id must be a team id.")
				}
				t, role, err := s.teamFor(ctx, s.d.Pool, in.TeamID, caller.UserID)
				if err != nil {
					return nil, err
				}
				members, err := s.teamMembers(ctx, s.d.Pool, t.ID, orgs.RoleAtLeast(role, orgs.RoleAdmin), nil, 0, uuid.Nil, 200)
				if err != nil {
					return nil, err
				}
				return map[string]any{"team": t, "members": members}, nil
			},
		},
		{
			Name:        "access_my_permissions",
			Description: "List which " + orgs.Noun() + " permissions the user holds in one " + orgs.Noun() + " (for example whether they may make public links).",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"org_id":{"type":"string","format":"uuid"}},"required":["org_id"],"additionalProperties":false}`),
			ReadOnly:    true,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					OrgID uuid.UUID `json:"org_id"`
				}
				if err := decodeInput(input, &in); err != nil || in.OrgID == uuid.Nil {
					return nil, httpx.BadRequest("org_id must be " + orgs.NounA() + " id.")
				}
				if _, err := memberRole(ctx, s, in.OrgID, caller.UserID); err != nil {
					return nil, err
				}
				type row struct {
					Key     string `json:"key"`
					Title   string `json:"title"`
					Allowed bool   `json:"allowed"`
				}
				out := []row{}
				for _, p := range allPermissions() {
					ok, err := Allowed(ctx, s.d.Pool, in.OrgID, caller.UserID, p.Key)
					if err != nil {
						return nil, err
					}
					out = append(out, row{Key: p.Key, Title: p.Title, Allowed: ok})
				}
				return out, nil
			},
		},
		{
			Name: "access_get_sharing",
			Description: "Show how a resource is shared: the user's own level, its owner, general access and the parent it inherits from. " +
				"For people who can manage it, also every person and team with access and its public links. kind is a registered kind such as docs.page.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"kind":{"type":"string"},"id":{"type":"string","format":"uuid"}},"required":["kind","id"],"additionalProperties":false}`),
			ReadOnly:    true,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					Kind string    `json:"kind"`
					ID   uuid.UUID `json:"id"`
				}
				if err := decodeInput(input, &in); err != nil {
					return nil, err
				}
				k, ok := KindOf(strings.TrimSpace(in.Kind))
				if !ok || in.ID == uuid.Nil {
					return nil, httpx.NotFound("Not found.")
				}
				t, err := s.loadTarget(ctx, s.d.Pool, caller.UserID, k, Ref{Kind: k.Name, ID: in.ID}, View)
				if err != nil {
					return nil, err
				}
				return s.sharing(ctx, t)
			},
		},
	}
}

func decodeInput(input json.RawMessage, dst any) error {
	if len(input) == 0 {
		return nil
	}
	if err := json.Unmarshal(input, dst); err != nil {
		return httpx.BadRequest("Invalid input: " + err.Error())
	}
	return nil
}

// memberRole returns the caller's org role; non-members get the same 404 as
// the REST guard.
func memberRole(ctx context.Context, s *Service, orgID, userID uuid.UUID) (string, error) {
	role, err := orgs.RoleOf(ctx, s.d.Pool, orgID, userID)
	if err != nil {
		return "", err
	}
	if role == "" {
		return "", httpx.NotFound(orgs.NounTitle() + " not found.")
	}
	return role, nil
}

// Search finds teams by name or handle in the caller's orgs (app.SearchProvider),
// in one org when the search is scoped (app.SearchOrg). Context is the org's name.
func (s *Service) Search(ctx context.Context, caller *auth.Identity, query string, limit int) ([]app.SearchResult, error) {
	q := strings.TrimPrefix(strings.TrimSpace(query), "@")
	if q == "" || limit <= 0 {
		return []app.SearchResult{}, nil
	}
	var scope *uuid.UUID
	if id, ok := app.SearchOrg(ctx); ok {
		scope = &id
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT t.id, t.name, t.handle, t.description, t.updated_at, t.org_id, o.name,
		       CASE WHEN t.handle = lower($2) OR lower(t.name) = lower($2) THEN 1.0
		            WHEN t.handle ILIKE $3 || '%' OR t.name ILIKE $3 || '%' THEN 0.8 ELSE 0.5 END
		FROM access_teams t JOIN org_members m ON m.org_id = t.org_id AND m.user_id = $1
		JOIN orgs o ON o.id = t.org_id
		WHERE (t.name ILIKE '%' || $3 || '%' OR t.handle ILIKE $3 || '%') AND ($5::uuid IS NULL OR t.org_id = $5)
		ORDER BY 8 DESC, lower(t.name), t.id LIMIT $4`, caller.UserID, q, likeEscape(q), limit, scope)
	if err != nil {
		return nil, err
	}
	out := []app.SearchResult{}
	for rows.Next() {
		var (
			id, orgID                   uuid.UUID
			name, handle, desc, orgName string
			r                           app.SearchResult
		)
		if err := rows.Scan(&id, &name, &handle, &desc, &r.At, &orgID, &orgName, &r.Score); err != nil {
			rows.Close()
			return nil, err
		}
		r.Kind, r.ID, r.Title, r.Snippet, r.Link, r.Icon = "access.team", id.String(), name+" (@"+handle+")", desc, "/settings/teams/"+id.String(), "users"
		r.OrgID, r.Context = orgID.String(), app.SearchContext(orgName)
		out = append(out, r)
	}
	return out, rows.Err()
}
