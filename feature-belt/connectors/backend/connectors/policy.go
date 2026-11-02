package connectors

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/audit"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
)

// Workspace policy (enterprise IT): an org's owners and admins allow or deny
// platforms, deny single capabilities, and decide whether capabilities that
// change data on a platform are allowed. Nothing is stored until they
// decide; no row means allowed. The policy applies live: For marks a
// connection blocked, Client refuses it, and the settings screen shows why.
// Which org applies: the one a feature names (For's orgID,
// app.WithConnectionOrg for Client); without one, every org the person
// belongs to (the strictest wins).

type policyRow struct {
	Provider    string
	Allowed     bool
	AllowWrites bool
	Denied      []string
	UpdatedAt   time.Time
}

type orgPolicy struct {
	OrgID   uuid.UUID
	OrgName string
	Rows    map[string]policyRow
}

// policySet is the policies that apply to one request.
type policySet struct {
	s    *Service
	orgs []orgPolicy
}

func (op orgPolicy) row(provider string) (policyRow, bool) {
	if r, ok := op.Rows[provider]; ok {
		return r, true
	}
	r, ok := op.Rows["*"]
	return r, ok
}

func (op orgPolicy) denied(provider, key string) bool {
	if r, ok := op.Rows[provider]; ok && slices.Contains(r.Denied, key) {
		return true
	}
	r, ok := op.Rows["*"]
	return ok && slices.Contains(r.Denied, key)
}

// blocks returns why the policy refuses the platform (c nil) or the
// capability, or "".
func (ps *policySet) blocks(provider string, c *app.Capability) string {
	if ps == nil {
		return ""
	}
	name := provider
	if p := ps.s.providers[provider]; p != nil {
		name = p.Name
	}
	for _, op := range ps.orgs {
		r, ok := op.row(provider)
		who := op.OrgName
		if who == "" {
			who = "Your workspace"
		}
		if ok && !r.Allowed {
			return who + " doesn't allow " + name + " connections."
		}
		if c == nil {
			continue
		}
		if op.denied(provider, c.Key) {
			return who + " doesn't allow " + c.Label + "."
		}
		if c.Write && ok && !r.AllowWrites {
			return who + " doesn't allow changes through " + name + "."
		}
	}
	return ""
}

// platformPolicy is the effective decision for a platform (the strictest
// across the orgs that apply).
func (ps *policySet) platformPolicy(provider string) PlatformPolicy {
	out := PlatformPolicy{Allowed: true, AllowWrites: true, DeniedCapabilities: []string{}}
	if ps == nil {
		return out
	}
	for _, op := range ps.orgs {
		if r, ok := op.row(provider); ok {
			out.Allowed = out.Allowed && r.Allowed
			out.AllowWrites = out.AllowWrites && r.AllowWrites
		}
		if r, ok := op.Rows[provider]; ok {
			out.Explicit = true
			out.DeniedCapabilities = unionScopes(out.DeniedCapabilities, r.Denied)
		}
		if r, ok := op.Rows["*"]; ok {
			out.DeniedCapabilities = unionScopes(out.DeniedCapabilities, r.Denied)
		}
	}
	out.DeniedCapabilities = nonNil(out.DeniedCapabilities)
	return out
}

// policiesFor loads the policies for a request: orgID's, or (strict and no
// org) those of every org the person belongs to.
func (s *Service) policiesFor(ctx context.Context, userID, orgID uuid.UUID, strict bool) (*policySet, error) {
	ps := &policySet{s: s}
	var rows pgx.Rows
	var err error
	switch {
	case orgID != uuid.Nil:
		rows, err = s.d.Pool.Query(ctx, `SELECT o.id, o.name, p.provider, p.allowed, p.allow_writes, p.denied_capabilities, p.updated_at
			FROM orgs o LEFT JOIN connector_policies p ON p.org_id = o.id WHERE o.id = $1`, orgID)
	case strict:
		rows, err = s.d.Pool.Query(ctx, `SELECT o.id, o.name, p.provider, p.allowed, p.allow_writes, p.denied_capabilities, p.updated_at
			FROM org_members m JOIN orgs o ON o.id = m.org_id JOIN connector_policies p ON p.org_id = o.id
			WHERE m.user_id = $1`, userID)
	default:
		return ps, nil
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byOrg := map[uuid.UUID]int{}
	for rows.Next() {
		var oid uuid.UUID
		var name string
		var provider *string
		var allowed, writes *bool
		var denied []string
		var at *time.Time
		if err := rows.Scan(&oid, &name, &provider, &allowed, &writes, &denied, &at); err != nil {
			return nil, err
		}
		i, ok := byOrg[oid]
		if !ok {
			i = len(ps.orgs)
			byOrg[oid] = i
			ps.orgs = append(ps.orgs, orgPolicy{OrgID: oid, OrgName: name, Rows: map[string]policyRow{}})
		}
		if provider != nil {
			r := policyRow{Provider: *provider, Allowed: *allowed, AllowWrites: *writes, Denied: denied}
			if at != nil {
				r.UpdatedAt = *at
			}
			ps.orgs[i].Rows[*provider] = r
		}
	}
	return ps, rows.Err()
}

// checkPolicy refuses connecting or turning on c when orgID's policy (or,
// without one, any of the person's orgs') doesn't allow it.
func (s *Service) checkPolicy(ctx context.Context, userID, orgID uuid.UUID, p *provider, c *app.Capability) error {
	ps, err := s.policiesFor(ctx, userID, orgID, true)
	if err != nil {
		return err
	}
	if reason := ps.blocks(p.ID, c); reason != "" {
		return httpx.NewError(http.StatusForbidden, app.ConnectionBlocked, reason)
	}
	return nil
}

// ---- workspace policy routes -------------------------------------------------------

// PolicyPlatform is one platform in the policy screen.
type PolicyPlatform struct {
	ID           string               `json:"id"`
	Name         string               `json:"name"`
	Available    bool                 `json:"available"` // set up on this server (sign-in or personal tokens)
	Policy       PlatformPolicy       `json:"policy"`
	Capabilities []PlatformCapability `json:"capabilities"`
	Connections  int                  `json:"connections"` // members' connected accounts (a count only)
}

// PolicyView is GET /v1/connectors/orgs/{orgID}/policy.
type PolicyView struct {
	OrgID     string           `json:"org_id"`
	CanEdit   bool             `json:"can_edit"`
	Default   PlatformPolicy   `json:"default"`
	Platforms []PolicyPlatform `json:"platforms"`
}

func (s *Service) getPolicy(w http.ResponseWriter, r *http.Request) error {
	m, _ := orgs.FromContext(r.Context())
	v, err := s.policyView(r.Context(), m.OrgID, orgs.RoleAtLeast(m.Role, orgs.RoleAdmin))
	if err != nil {
		return err
	}
	httpx.OK(w, v)
	return nil
}

func (s *Service) policyView(ctx context.Context, orgID uuid.UUID, canEdit bool) (*PolicyView, error) {
	ps, err := s.policiesFor(ctx, uuid.Nil, orgID, false)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	rows, err := s.d.Pool.Query(ctx, `SELECT pc.provider, count(*) FROM connector_platform_connections pc
		JOIN org_members m ON m.user_id = pc.user_id AND m.org_id = $1 GROUP BY pc.provider`, orgID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var p string
		var n int
		if err := rows.Scan(&p, &n); err != nil {
			rows.Close()
			return nil, err
		}
		counts[p] = n
	}
	rows.Close()
	out := &PolicyView{OrgID: orgID.String(), CanEdit: canEdit, Default: PlatformPolicy{Allowed: true, AllowWrites: true, DeniedCapabilities: []string{}}}
	if len(ps.orgs) > 0 {
		if d, ok := ps.orgs[0].Rows["*"]; ok {
			out.Default = PlatformPolicy{Allowed: d.Allowed, AllowWrites: d.AllowWrites, DeniedCapabilities: nonNil(d.Denied), Explicit: true}
		}
	}
	caps := s.capabilities()
	for _, id := range s.providerOrder {
		p := s.providers[id]
		pp := PolicyPlatform{ID: p.ID, Name: p.Name, Available: p.available(), Policy: ps.platformPolicy(p.ID),
			Capabilities: []PlatformCapability{}, Connections: counts[p.ID]}
		for _, c := range caps {
			if c.Provider == p.ID {
				pp.Capabilities = append(pp.Capabilities, s.capabilityDTO(p, c, ps))
			}
		}
		out.Platforms = append(out.Platforms, pp)
	}
	return out, nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

type policyReq struct {
	Allowed            *bool     `json:"allowed"`
	AllowWrites        *bool     `json:"allow_writes"`
	DeniedCapabilities *[]string `json:"denied_capabilities"`
}

// putPolicy is PUT /v1/connectors/orgs/{orgID}/policy/{provider} (owners
// and admins; provider "*" is the default for platforms without a row).
func (s *Service) putPolicy(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id := auth.MustFromContext(ctx)
	m, _ := orgs.FromContext(ctx)
	provider := chi.URLParam(r, "provider")
	if provider != "*" && s.providers[provider] == nil {
		return httpx.NotFound("That platform isn't available.")
	}
	var req policyReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	known := map[string]bool{}
	for _, c := range s.capabilities() {
		if provider == "*" || c.Provider == provider {
			known[c.Key] = true
		}
	}
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		cur := policyRow{Allowed: true, AllowWrites: true}
		var denied []string
		err := tx.QueryRow(ctx, `SELECT allowed, allow_writes, denied_capabilities FROM connector_policies
			WHERE org_id = $1 AND provider = $2 FOR UPDATE`, m.OrgID, provider).Scan(&cur.Allowed, &cur.AllowWrites, &denied)
		if err != nil && !isNoRows(err) {
			return err
		}
		cur.Denied = denied
		if req.Allowed != nil {
			cur.Allowed = *req.Allowed
		}
		if req.AllowWrites != nil {
			cur.AllowWrites = *req.AllowWrites
		}
		if req.DeniedCapabilities != nil {
			cur.Denied = []string{}
			for _, k := range *req.DeniedCapabilities {
				k = strings.TrimSpace(k)
				if k == "" || slices.Contains(cur.Denied, k) {
					continue
				}
				if !known[k] {
					return httpx.BadRequest("Unknown capability: " + truncate(k, 80) + ".")
				}
				cur.Denied = append(cur.Denied, k)
			}
		}
		if cur.Denied == nil {
			cur.Denied = []string{}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO connector_policies (org_id, provider, allowed, allow_writes, denied_capabilities, updated_by)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (org_id, provider) DO UPDATE SET allowed = EXCLUDED.allowed, allow_writes = EXCLUDED.allow_writes,
			  denied_capabilities = EXCLUDED.denied_capabilities, updated_by = EXCLUDED.updated_by, updated_at = now()`,
			m.OrgID, provider, cur.Allowed, cur.AllowWrites, cur.Denied, id.UserID); err != nil {
			return err
		}
		return audit.Log(ctx, tx, id.UserID, "connectors.policy.update", "org", m.OrgID.String(), map[string]any{
			"provider": provider, "allowed": cur.Allowed, "allow_writes": cur.AllowWrites, "denied_capabilities": cur.Denied})
	})
	if err != nil {
		return err
	}
	v, err := s.policyView(ctx, m.OrgID, true)
	if err != nil {
		return err
	}
	httpx.OK(w, v)
	return nil
}

// deletePolicy is DELETE /v1/connectors/orgs/{orgID}/policy/{provider}:
// back to the default.
func (s *Service) deletePolicy(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id := auth.MustFromContext(ctx)
	m, _ := orgs.FromContext(ctx)
	provider := chi.URLParam(r, "provider")
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM connector_policies WHERE org_id = $1 AND provider = $2`, m.OrgID, provider)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		return audit.Log(ctx, tx, id.UserID, "connectors.policy.reset", "org", m.OrgID.String(), map[string]any{"provider": provider})
	})
	if err != nil {
		return err
	}
	v, err := s.policyView(ctx, m.OrgID, true)
	if err != nil {
		return err
	}
	httpx.OK(w, v)
	return nil
}

// ---- staff: platform setup ------------------------------------------------------------

// PlatformSetup is one platform's setup on this server, for staff.
type PlatformSetup struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	OAuth        bool     `json:"oauth"`
	Missing      []string `json:"missing"` // env vars to set for sign-in
	Tokens       bool     `json:"tokens"`  // personal tokens work without setup
	BaseURL      string   `json:"base_url,omitempty"`
	RedirectURI  string   `json:"redirect_uri"`
	RegisterURL  string   `json:"register_url"`
	Scopes       []string `json:"scopes"` // every scope installed features may ask for
	Capabilities int      `json:"capabilities"`
	Connections  int      `json:"connections"`
	Revocation   bool     `json:"revocation"`
	WebhookPath  string   `json:"webhook_path,omitempty"`
	WebhookReady bool     `json:"webhook_ready"`
	DevOverride  string   `json:"dev_override,omitempty"`
}

// adminPlatforms is GET /v1/admin/connectors/platforms (support and up).
func (s *Service) adminPlatforms(w http.ResponseWriter, r *http.Request) error {
	counts := map[string]int{}
	rows, err := s.d.Pool.Query(r.Context(), `SELECT provider, count(*) FROM connector_platform_connections GROUP BY provider`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var p string
		var n int
		if err := rows.Scan(&p, &n); err != nil {
			rows.Close()
			return err
		}
		counts[p] = n
	}
	rows.Close()
	caps := s.capabilities()
	env := providerEnv()
	out := make([]PlatformSetup, 0, len(s.providerOrder))
	for _, id := range s.providerOrder {
		p := s.providers[id]
		ps := PlatformSetup{ID: p.ID, Name: p.Name, OAuth: p.oauthReady(), Missing: []string{}, Tokens: p.Token != nil,
			RedirectURI: s.redirectURI(), RegisterURL: p.page(p.Docs), Scopes: unionScopes(p.BaseScopes),
			Connections: counts[p.ID], Revocation: p.RevokeStyle != ""}
		if p.clientID == "" {
			ps.Missing = append(ps.Missing, p.EnvClientID)
		}
		if p.clientSecret == "" {
			ps.Missing = append(ps.Missing, p.EnvClientSecret)
		}
		if p.SelfManaged {
			ps.BaseURL = p.vars["base"]
		}
		for _, c := range caps {
			if c.Provider == p.ID {
				ps.Capabilities++
				ps.Scopes = unionScopes(ps.Scopes, c.Scopes)
			}
		}
		if ps.Scopes == nil {
			ps.Scopes = []string{}
		}
		if p.Webhook != nil {
			ps.WebhookPath = "/webhooks/connectors/" + p.ID
			ps.WebhookReady = env[p.ID]["webhook"] != "" || (p.Webhook.Verify == "hubspot_v3" && p.clientSecret != "")
		}
		if p.override != nil {
			ps.DevOverride = p.override.String()
		}
		out = append(out, ps)
	}
	httpx.OK(w, map[string]any{"items": out, "next_cursor": nil})
	return nil
}
