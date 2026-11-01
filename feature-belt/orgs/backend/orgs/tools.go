package orgs

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
)

// Tools exposes read-only views of the caller's orgs. Invites are not a
// tool: they email people outside the app. Each tool applies the same checks
// as its REST route.
func (s *Service) Tools() []app.Tool {
	orgInput := json.RawMessage(`{"type":"object","properties":{"org_id":{"type":"string","format":"uuid"}},"required":["org_id"],"additionalProperties":false}`)
	return []app.Tool{
		{
			Name:        "orgs_list_mine",
			Description: "List the " + Nouns() + " the user belongs to, with their role in each.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
			ReadOnly:    true,
			Handler: func(ctx context.Context, caller *auth.Identity, _ json.RawMessage) (any, error) {
				rows, err := s.d.Pool.Query(ctx, orgSelect+` WHERE m.user_id = $1 ORDER BY m.created_at, m.org_id LIMIT 100`, caller.UserID)
				if err != nil {
					return nil, err
				}
				return s.scanOrgs(rows)
			},
		},
		{
			Name:        "orgs_list_members",
			Description: "List the members of one of the user's " + Nouns() + " and their roles. Emails are included only for admins and owners.",
			InputSchema: orgInput,
			ReadOnly:    true,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				orgID, role, err := s.toolOrg(ctx, caller, input)
				if err != nil {
					return nil, err
				}
				rows, err := s.d.Pool.Query(ctx, memberSelect+`
					WHERE m.org_id = $1 ORDER BY CASE m.role WHEN 'owner' THEN 1 WHEN 'admin' THEN 2 ELSE 3 END, m.created_at LIMIT 200`, orgID)
				if err != nil {
					return nil, err
				}
				return scanMembers(rows, RoleAtLeast(role, RoleAdmin))
			},
		},
		{
			Name:        "orgs_list_pending_invites",
			Description: "List pending email invites for " + NounA() + " the user administers.",
			InputSchema: orgInput,
			ReadOnly:    true,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				orgID, role, err := s.toolOrg(ctx, caller, input)
				if err != nil {
					return nil, err
				}
				if !RoleAtLeast(role, RoleAdmin) {
					return nil, httpx.Forbidden("Only admins of this " + Noun() + " can see its invites.")
				}
				rows, err := s.d.Pool.Query(ctx, inviteSelect+`
					WHERE i.org_id = $1 AND i.status = 'pending' AND i.expires_at > now() ORDER BY i.created_at DESC LIMIT 100`, orgID)
				if err != nil {
					return nil, err
				}
				list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Invite])
				if list == nil {
					list = []Invite{}
				}
				return list, err
			},
		},
		{
			Name:        "orgs_list_joinable",
			Description: "List " + Nouns() + " the user can join, or has asked to join, because their verified email domain matches one the " + Noun() + " verified. action is join, request or requested.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
			ReadOnly:    true,
			Handler: func(ctx context.Context, caller *auth.Identity, _ json.RawMessage) (any, error) {
				return s.joinable(ctx, caller)
			},
		},
		{
			Name:        "orgs_list_domains",
			Description: "List the email domains " + NounA() + " the user administers has claimed, with verification status and join policy (auto_join or request).",
			InputSchema: orgInput,
			ReadOnly:    true,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				orgID, role, err := s.toolOrg(ctx, caller, input)
				if err != nil {
					return nil, err
				}
				if !RoleAtLeast(role, RoleAdmin) {
					return nil, httpx.Forbidden("Only admins of this " + Noun() + " can see its domains.")
				}
				rows, err := s.d.Pool.Query(ctx, domainSelect+` WHERE d.org_id = $1 ORDER BY d.created_at, d.id`, orgID)
				if err != nil {
					return nil, err
				}
				return scanDomains(rows)
			},
		},
		{
			Name:        "orgs_list_join_requests",
			Description: "List pending requests to join " + NounA() + " the user administers, oldest first, with each person's name and email.",
			InputSchema: orgInput,
			ReadOnly:    true,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				orgID, role, err := s.toolOrg(ctx, caller, input)
				if err != nil {
					return nil, err
				}
				if !RoleAtLeast(role, RoleAdmin) {
					return nil, httpx.Forbidden("Only admins of this " + Noun() + " can see its join requests.")
				}
				rows, err := s.d.Pool.Query(ctx, joinRequestSelect+`
					WHERE j.org_id = $1 AND j.status = 'pending' ORDER BY j.created_at, j.id LIMIT 100`, orgID)
				if err != nil {
					return nil, err
				}
				list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[JoinRequest])
				if list == nil {
					list = []JoinRequest{}
				}
				return list, err
			},
		},
	}
}

// toolOrg parses {org_id} and checks the caller is a member.
func (s *Service) toolOrg(ctx context.Context, caller *auth.Identity, input json.RawMessage) (uuid.UUID, string, error) {
	var in struct {
		OrgID uuid.UUID `json:"org_id"`
	}
	if err := json.Unmarshal(input, &in); err != nil || in.OrgID == uuid.Nil {
		return uuid.Nil, "", httpx.BadRequest("org_id must be " + NounA() + " id.")
	}
	role, err := RoleOf(ctx, s.d.Pool, in.OrgID, caller.UserID)
	if err != nil {
		return uuid.Nil, "", err
	}
	if role == "" {
		return uuid.Nil, "", errOrgNotFound
	}
	return in.OrgID, role, nil
}
