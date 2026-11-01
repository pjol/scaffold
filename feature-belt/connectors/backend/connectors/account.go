package connectors

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ---- account deletion and export -------------------------------------------

// DeleteAccount deletes the user's connections, flows in progress and own
// servers inside the deletion transaction. Their tokens move to the
// revocation queue first; AccountDeleted asks the providers right after the
// commit and the worker retries what fails.
func (s *Service) DeleteAccount(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	rows, err := tx.Query(ctx, `SELECT `+connectionCols+` FROM connector_connections cc WHERE cc.user_id = $1 FOR UPDATE`, userID)
	if err != nil {
		return err
	}
	var conns []*connectionRow
	for rows.Next() {
		cc, err := scanConnection(rows)
		if err != nil {
			rows.Close()
			return err
		}
		conns = append(conns, cc)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, cc := range conns {
		if err := s.enqueueConnectionRevocations(ctx, tx, cc); err != nil {
			return err
		}
		s.tools.forget(cc.ID)
	}
	flows, err := tx.Query(ctx, `SELECT `+flowCols+`, access_token_enc, refresh_token_enc FROM connector_oauth_flows
		WHERE user_id = $1 AND status = 'authorized' FOR UPDATE`, userID)
	if err != nil {
		return err
	}
	type issued struct {
		f               flowRow
		access, refresh []byte
	}
	var pending []issued
	for flows.Next() {
		var it issued
		f := &it.f
		if err := flows.Scan(&f.ID, &f.UserID, &f.ConnectorID, &f.Client, &f.ExpiresAt, &f.CodeVerifierEnc, &f.RedirectURI,
			&f.Resource, &f.Issuer, &f.IssSupported, &f.TokenEndpoint, &f.RevocationEndpoint, &f.ClientID,
			&f.ClientSecretEnc, &f.TokenAuthMethod, &f.Scopes, &it.access, &it.refresh); err != nil {
			flows.Close()
			return err
		}
		pending = append(pending, it)
	}
	flows.Close()
	if err := flows.Err(); err != nil {
		return err
	}
	for _, it := range pending {
		client, err := it.f.client(s)
		if err != nil {
			continue
		}
		access, _ := s.seal.open(it.access, aadFlow(it.f.ID, "access"))
		refresh, _ := s.seal.open(it.refresh, aadFlow(it.f.ID, "refresh"))
		uid := userID
		if err := s.enqueueRevocation(ctx, tx, &uid, it.f.RevocationEndpoint, client, refresh, "refresh_token"); err != nil {
			return err
		}
		if err := s.enqueueRevocation(ctx, tx, &uid, it.f.RevocationEndpoint, client, access, "access_token"); err != nil {
			return err
		}
	}
	for _, q := range []string{
		`DELETE FROM connector_oauth_flows WHERE user_id = $1`,
		`DELETE FROM connector_connections WHERE user_id = $1`,
		`DELETE FROM connectors WHERE owner_id = $1`,
	} {
		if _, err := tx.Exec(ctx, q, userID); err != nil {
			return err
		}
	}
	return nil
}

// AccountDeleted revokes the deleted user's tokens at their providers now
// (best effort; the revocation worker retries what fails).
func (s *Service) AccountDeleted(ctx context.Context, userID uuid.UUID) error {
	done, pending := s.processUserRevocations(ctx, userID)
	slog.InfoContext(ctx, "connectors: account tokens revoked", "user_id", userID, "revoked", done, "pending", pending)
	return nil
}

type exportConnection struct {
	Connector     string     `json:"connector"`
	ServerURL     string     `json:"server_url"`
	Status        string     `json:"status"`
	Scopes        []string   `json:"scopes"`
	WritesEnabled bool       `json:"writes_enabled"`
	DisabledTools []string   `json:"disabled_tools"`
	ConnectedAt   time.Time  `json:"connected_at"`
	LastUsedAt    *time.Time `json:"last_used_at"`
}

type exportServer struct {
	Name      string    `json:"name"`
	ServerURL string    `json:"server_url"`
	AuthType  string    `json:"auth_type"`
	CreatedAt time.Time `json:"created_at"`
}

// ExportAccount lists the user's connections and own servers: metadata
// only, never tokens or secrets.
func (s *Service) ExportAccount(ctx context.Context, userID uuid.UUID) (any, error) {
	out := struct {
		Connections []exportConnection `json:"connections"`
		Servers     []exportServer     `json:"servers"`
	}{Connections: []exportConnection{}, Servers: []exportServer{}}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT c.name, c.server_url, cc.status, cc.scopes, cc.writes_enabled, cc.disabled_tools, cc.connected_at, cc.last_used_at
		FROM connector_connections cc JOIN connectors c ON c.id = cc.connector_id
		WHERE cc.user_id = $1 ORDER BY cc.connected_at, cc.id`, userID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var e exportConnection
		var scopes string
		if err := rows.Scan(&e.Connector, &e.ServerURL, &e.Status, &scopes, &e.WritesEnabled, &e.DisabledTools, &e.ConnectedAt, &e.LastUsedAt); err != nil {
			rows.Close()
			return nil, err
		}
		e.Scopes = splitScopes(scopes)
		if e.DisabledTools == nil {
			e.DisabledTools = []string{}
		}
		out.Connections = append(out.Connections, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = s.d.Pool.Query(ctx, `SELECT name, server_url, auth_type, created_at FROM connectors
		WHERE owner_id = $1 ORDER BY created_at, id`, userID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var e exportServer
		if err := rows.Scan(&e.Name, &e.ServerURL, &e.AuthType, &e.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		out.Servers = append(out.Servers, e)
	}
	rows.Close()
	return out, rows.Err()
}

// ---- development seed ------------------------------------------------------

// SeedDev adds a small catalog of public remote MCP servers that use OAuth
// with dynamic client registration, so the settings screen has something
// real to connect to. No connections are seeded: connecting is the flow to
// exercise, and a fake token would be fake data.
func (s *Service) SeedDev(ctx context.Context, users []uuid.UUID) error {
	var admin *uuid.UUID
	if len(users) > 0 {
		admin = &users[0]
	}
	entries := []struct{ slug, name, desc, url, auth, policy string }{
		{"linear", "Linear", "Issues, projects and cycles in your Linear workspace.", "https://mcp.linear.app/mcp", "oauth", "off"},
		{"notion", "Notion", "Pages and databases you can open in Notion.", "https://mcp.notion.com/mcp", "oauth", "off"},
		{"sentry", "Sentry", "Errors and issues from your Sentry organizations.", "https://mcp.sentry.dev/mcp", "oauth", "disabled"},
		{"github", "GitHub", "Repositories, issues and pull requests, with a personal access token.", "https://api.githubcopilot.com/mcp/", "bearer", "off"},
	}
	for _, e := range entries {
		if _, err := s.d.Pool.Exec(ctx, `
			INSERT INTO connectors (slug, name, description, server_url, auth_type, write_policy, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (slug) WHERE owner_id IS NULL DO NOTHING`,
			e.slug, e.name, e.desc, e.url, e.auth, e.policy, admin); err != nil {
			return err
		}
	}
	return nil
}
