package connectors

import (
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// ---- rows ------------------------------------------------------------------

type connectorRow struct {
	ID                   uuid.UUID
	OwnerID              *uuid.UUID
	Slug                 string
	Name                 string
	Description          string
	IconURL              string
	ServerURL            string
	AuthType             string
	OAuthClientID        string
	OAuthClientSecretEnc []byte
	OAuthScopes          string
	WritePolicy          string
	Enabled              bool
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

const connectorCols = `c.id, c.owner_id, c.slug, c.name, c.description, c.icon_url, c.server_url, c.auth_type,
	c.oauth_client_id, c.oauth_client_secret_enc, c.oauth_scopes, c.write_policy, c.enabled, c.created_at, c.updated_at`

func (c *connectorRow) custom() bool { return c.OwnerID != nil }

// trustsAnnotations: staff vetted catalog servers, so their readOnlyHint is
// believed. A user's own server is untrusted (MCP spec: annotations from
// untrusted servers are hints only): every tool counts as a write.
func (c *connectorRow) trustsAnnotations() bool { return !c.custom() }

type connectionRow struct {
	ID                 uuid.UUID
	UserID             uuid.UUID
	ConnectorID        uuid.UUID
	Status             string
	AccessTokenEnc     []byte
	RefreshTokenEnc    []byte
	TokenType          string
	ExpiresAt          *time.Time
	Scopes             string
	Resource           string
	Issuer             string
	TokenEndpoint      string
	RevocationEndpoint string
	ClientID           string
	ClientSecretEnc    []byte
	TokenAuthMethod    string
	WritesEnabled      bool
	DisabledTools      []string
	LastError          string
	LastErrorAt        *time.Time
	LastUsedAt         *time.Time
	ConnectedAt        time.Time
}

const connectionCols = `cc.id, cc.user_id, cc.connector_id, cc.status, cc.access_token_enc, cc.refresh_token_enc,
	cc.token_type, cc.expires_at, cc.scopes, cc.resource, cc.issuer, cc.token_endpoint, cc.revocation_endpoint,
	cc.client_id, cc.client_secret_enc, cc.token_auth_method, cc.writes_enabled, cc.disabled_tools,
	cc.last_error, cc.last_error_at, cc.last_used_at, cc.connected_at`

func (cc *connectionRow) client(s *Service) (oauthClient, error) {
	secret, err := s.seal.open(cc.ClientSecretEnc, aadConnection(cc.ID, "client_secret"))
	return oauthClient{ID: cc.ClientID, Secret: secret, AuthMethod: cc.TokenAuthMethod}, err
}

// ---- DTOs ------------------------------------------------------------------

// Connector is a catalog entry as a user sees it, with their connection.
type Connector struct {
	ID          uuid.UUID `json:"id"`
	Slug        string    `json:"slug"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	IconURL     string    `json:"icon_url"`
	AuthType    string    `json:"auth_type"`    // oauth | bearer
	WritePolicy string    `json:"write_policy"` // disabled | off | on
	Custom      bool      `json:"custom"`
	ServerHost  string    `json:"server_host"`
	// Available is false when staff turned the connector off (or custom
	// servers were turned off): an existing connection stays listed so it
	// can be removed, but its tools aren't offered.
	Available  bool        `json:"available"`
	Connection *Connection `json:"connection"`
}

// Connection is the caller's connection to a connector. Tokens never leave
// the server.
type Connection struct {
	ID            uuid.UUID  `json:"id"`
	ConnectorID   uuid.UUID  `json:"connector_id"`
	Status        string     `json:"status"` // active | needs_reauth
	WritesEnabled bool       `json:"writes_enabled"`
	DisabledTools []string   `json:"disabled_tools"`
	Scopes        []string   `json:"scopes"`
	LastError     string     `json:"last_error"`
	LastErrorAt   *time.Time `json:"last_error_at"`
	LastUsedAt    *time.Time `json:"last_used_at"`
	ConnectedAt   time.Time  `json:"connected_at"`
}

func (cc *connectionRow) dto() *Connection {
	dt := cc.DisabledTools
	if dt == nil {
		dt = []string{}
	}
	return &Connection{ID: cc.ID, ConnectorID: cc.ConnectorID, Status: cc.Status, WritesEnabled: cc.WritesEnabled,
		DisabledTools: dt, Scopes: splitScopes(cc.Scopes), LastError: cc.LastError, LastErrorAt: cc.LastErrorAt,
		LastUsedAt: cc.LastUsedAt, ConnectedAt: cc.ConnectedAt}
}

func (s *Service) connectorDTO(c *connectorRow, cc *connectionRow) Connector {
	out := Connector{ID: c.ID, Slug: c.Slug, Name: c.Name, Description: c.Description, IconURL: c.IconURL,
		AuthType: c.AuthType, WritePolicy: c.WritePolicy, Custom: c.custom(), ServerHost: hostOf(c.ServerURL),
		Available: s.available(c)}
	if cc != nil {
		out.Connection = cc.dto()
	}
	return out
}

// available: the connector's tools may be offered.
func (s *Service) available(c *connectorRow) bool {
	if c.custom() {
		return s.allowCustom
	}
	return c.Enabled
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Host
}

func splitScopes(s string) []string {
	f := strings.Fields(s)
	if f == nil {
		return []string{}
	}
	return f
}

// ---- loaders -----------------------------------------------------------------

func scanConnector(row pgx.Row) (*connectorRow, error) {
	var c connectorRow
	err := row.Scan(&c.ID, &c.OwnerID, &c.Slug, &c.Name, &c.Description, &c.IconURL, &c.ServerURL, &c.AuthType,
		&c.OAuthClientID, &c.OAuthClientSecretEnc, &c.OAuthScopes, &c.WritePolicy, &c.Enabled, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func scanConnection(row pgx.Row) (*connectionRow, error) {
	var cc connectionRow
	err := row.Scan(&cc.ID, &cc.UserID, &cc.ConnectorID, &cc.Status, &cc.AccessTokenEnc, &cc.RefreshTokenEnc,
		&cc.TokenType, &cc.ExpiresAt, &cc.Scopes, &cc.Resource, &cc.Issuer, &cc.TokenEndpoint, &cc.RevocationEndpoint,
		&cc.ClientID, &cc.ClientSecretEnc, &cc.TokenAuthMethod, &cc.WritesEnabled, &cc.DisabledTools,
		&cc.LastError, &cc.LastErrorAt, &cc.LastUsedAt, &cc.ConnectedAt)
	if err != nil {
		return nil, err
	}
	return &cc, nil
}

// visibleConnector loads a connector the user may connect to: a catalog
// entry or their own server. Someone else's server is 404.
func (s *Service) visibleConnector(ctx context.Context, q db.Querier, userID, id uuid.UUID) (*connectorRow, error) {
	c, err := scanConnector(q.QueryRow(ctx, `SELECT `+connectorCols+` FROM connectors c
		WHERE c.id = $1 AND (c.owner_id IS NULL OR c.owner_id = $2)`, id, userID))
	if isNoRows(err) {
		return nil, httpx.NotFound("That connector doesn't exist.")
	}
	return c, err
}

// ownConnection loads the caller's connection with its connector (404 for
// anyone else's). lock takes the row for update.
func (s *Service) ownConnection(ctx context.Context, q db.Querier, userID, id uuid.UUID, lock bool) (*connectionRow, *connectorRow, error) {
	sql := `SELECT ` + connectionCols + `, ` + connectorCols + `
		FROM connector_connections cc JOIN connectors c ON c.id = cc.connector_id
		WHERE cc.id = $1 AND cc.user_id = $2`
	if lock {
		sql += ` FOR UPDATE OF cc`
	}
	cc, c, err := scanJoined(q.QueryRow(ctx, sql, id, userID))
	if isNoRows(err) {
		return nil, nil, httpx.NotFound("That connection doesn't exist.")
	}
	return cc, c, err
}

func scanJoined(row pgx.Row) (*connectionRow, *connectorRow, error) {
	var cc connectionRow
	var c connectorRow
	err := row.Scan(&cc.ID, &cc.UserID, &cc.ConnectorID, &cc.Status, &cc.AccessTokenEnc, &cc.RefreshTokenEnc,
		&cc.TokenType, &cc.ExpiresAt, &cc.Scopes, &cc.Resource, &cc.Issuer, &cc.TokenEndpoint, &cc.RevocationEndpoint,
		&cc.ClientID, &cc.ClientSecretEnc, &cc.TokenAuthMethod, &cc.WritesEnabled, &cc.DisabledTools,
		&cc.LastError, &cc.LastErrorAt, &cc.LastUsedAt, &cc.ConnectedAt,
		&c.ID, &c.OwnerID, &c.Slug, &c.Name, &c.Description, &c.IconURL, &c.ServerURL, &c.AuthType,
		&c.OAuthClientID, &c.OAuthClientSecretEnc, &c.OAuthScopes, &c.WritePolicy, &c.Enabled, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, nil, err
	}
	return &cc, &c, nil
}

// activeConnections are the caller's healthy connections whose connector is
// available, for ToolsFor.
func (s *Service) activeConnections(ctx context.Context, userID uuid.UUID) ([]*connectionRow, []*connectorRow, error) {
	rows, err := s.d.Pool.Query(ctx, `SELECT `+connectionCols+`, `+connectorCols+`
		FROM connector_connections cc JOIN connectors c ON c.id = cc.connector_id
		WHERE cc.user_id = $1 AND cc.status = 'active'
		  AND ((c.owner_id IS NULL AND c.enabled) OR (c.owner_id = $1 AND $2))
		ORDER BY c.owner_id NULLS FIRST, c.slug, cc.id`, userID, s.allowCustom)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var conns []*connectionRow
	var cons []*connectorRow
	for rows.Next() {
		cc, c, err := scanJoined(rows)
		if err != nil {
			return nil, nil, err
		}
		conns, cons = append(conns, cc), append(cons, c)
	}
	return conns, cons, rows.Err()
}

// markNeedsReauth flags a connection whose grant is gone; the settings
// screen asks the user to connect again.
func (s *Service) markNeedsReauth(ctx context.Context, id uuid.UUID, msg string) {
	_, _ = s.d.Pool.Exec(context.WithoutCancel(ctx), `
		UPDATE connector_connections SET status = 'needs_reauth', last_error = $2, last_error_at = now(), updated_at = now()
		WHERE id = $1`, id, msg)
	s.tools.forget(id)
}

// noteError records a passing failure (shown on the connection), and
// clears it when msg is empty.
func (s *Service) noteError(ctx context.Context, id uuid.UUID, msg string) {
	if msg == "" {
		_, _ = s.d.Pool.Exec(context.WithoutCancel(ctx), `
			UPDATE connector_connections SET last_error = '', last_error_at = NULL WHERE id = $1 AND last_error <> ''`, id)
		return
	}
	_, _ = s.d.Pool.Exec(context.WithoutCancel(ctx), `
		UPDATE connector_connections SET last_error = $2, last_error_at = now() WHERE id = $1`, id, msg)
}
