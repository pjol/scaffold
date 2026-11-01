package mcp

import (
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/audit"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// Connected apps: what a person has authorized over OAuth, one row per
// client however many times (or devices) they connected it, and staff's
// per-grant view.

// Connection is one connected app as its owner sees it.
type Connection struct {
	ID          uuid.UUID  `json:"id"` // the client; revoking it ends every grant the person gave it
	Name        string     `json:"name"`
	Kind        string     `json:"kind"`
	Host        *string    `json:"host"`
	Scopes      []string   `json:"scopes"` // union over its active grants
	Grants      int        `json:"grants"` // active authorizations (one per device or reconnect)
	ConnectedAt time.Time  `json:"connected_at"`
	FirstUsedAt *time.Time `json:"first_used_at"`
	LastUsedAt  *time.Time `json:"last_used_at"`
	clientID    string
}

// activeGrant is the SQL condition for a grant that can still mint tokens.
const activeGrant = `g.revoked_at IS NULL AND g.expires_at > now()`

// listConnections is GET /v1/mcp/connections: the caller's connected apps,
// most recently connected first.
func (s *Service) listConnections(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	p := httpx.ParsePage(r)
	var c cursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	rows, err := s.d.Pool.Query(r.Context(), `
		WITH apps AS (
			SELECT c.id, c.client_name, c.kind, c.client_id,
			       CASE WHEN bool_or('write' = ANY (g.scopes)) THEN ARRAY['read', 'write'] ELSE ARRAY['read'] END AS scopes,
			       count(*)::int AS grants, max(g.created_at) AS connected_at,
			       min(g.first_used_at) AS first_used_at, max(g.last_used_at) AS last_used_at
			FROM mcp_oauth_grants g
			JOIN mcp_oauth_clients c ON c.id = g.client_ref
			WHERE g.user_id = $1 AND `+activeGrant+`
			GROUP BY c.id
		)
		SELECT id, client_name, kind, client_id, scopes, grants, connected_at, first_used_at, last_used_at FROM apps
		WHERE ($2::timestamptz IS NULL OR (connected_at, id) < ($2, $3))
		ORDER BY connected_at DESC, id DESC LIMIT $4`, id.UserID, nullTime(c.T), c.ID, p.Limit+1)
	if err != nil {
		return err
	}
	defer rows.Close()
	list := []Connection{}
	for rows.Next() {
		var x Connection
		if err := rows.Scan(&x.ID, &x.Name, &x.Kind, &x.clientID, &x.Scopes, &x.Grants, &x.ConnectedAt, &x.FirstUsedAt, &x.LastUsedAt); err != nil {
			return err
		}
		x.Host = clientInfo(x.clientID, x.Kind, x.Name).Host
		list = append(list, x)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(list, p.Limit, func(x Connection) any { return cursor{x.ConnectedAt, x.ID} }))
	return nil
}

// revokeConnection is DELETE /v1/mcp/connections/{id}: ends every grant the
// caller gave that client, and with them every token. The client must
// connect again to get access back.
func (s *Service) revokeConnection(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	clientRef, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var n int
	err = db.WithTx(r.Context(), s.d.Pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `SELECT g.id FROM mcp_oauth_grants g WHERE g.user_id = $1 AND g.client_ref = $2 AND g.revoked_at IS NULL`,
			id.UserID, clientRef)
		if err != nil {
			return err
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		if err != nil {
			return err
		}
		for _, g := range ids {
			if err := revokeGrant(r.Context(), tx, g, "user", nil); err != nil {
				return err
			}
		}
		n = len(ids)
		return nil
	})
	if err != nil {
		return err
	}
	if n == 0 {
		return httpx.ErrNotFound
	}
	httpx.NoContent(w)
	return nil
}

// ---- admin -----------------------------------------------------------------

// AdminConnection is one grant as staff see it.
type AdminConnection struct {
	ID           uuid.UUID  `json:"id"`
	UserID       uuid.UUID  `json:"user_id"`
	UserEmail    string     `json:"user_email"`
	ClientName   string     `json:"client_name"`
	ClientKind   string     `json:"client_kind"`
	ClientHost   *string    `json:"client_host"`
	Scopes       []string   `json:"scopes"`
	CreatedAt    time.Time  `json:"created_at"`
	FirstUsedAt  *time.Time `json:"first_used_at"`
	LastUsedAt   *time.Time `json:"last_used_at"`
	ExpiresAt    time.Time  `json:"expires_at"`
	RevokedAt    *time.Time `json:"revoked_at"`
	RevokeReason *string    `json:"revoke_reason"`
}

// adminListConnections is GET /v1/admin/mcp/connections?user_id=&status=active|revoked.
func (s *Service) adminListConnections(w http.ResponseWriter, r *http.Request) error {
	p := httpx.ParsePage(r)
	var c cursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	q := r.URL.Query()
	var userID *uuid.UUID
	if v := q.Get("user_id"); v != "" {
		u, err := uuid.Parse(v)
		if err != nil {
			return httpx.BadRequest("Invalid user_id.")
		}
		userID = &u
	}
	status := q.Get("status")
	if !slices.Contains([]string{"", "active", "revoked"}, status) {
		return httpx.BadRequest("Status must be active or revoked.")
	}
	rows, err := s.d.Pool.Query(r.Context(), `
		SELECT g.id, g.user_id, u.email, c.client_name, c.kind, c.client_id, g.scopes, g.created_at,
		       g.first_used_at, g.last_used_at, g.expires_at, g.revoked_at, g.revoke_reason
		FROM mcp_oauth_grants g
		JOIN mcp_oauth_clients c ON c.id = g.client_ref
		JOIN users u ON u.id = g.user_id
		WHERE ($1::uuid IS NULL OR g.user_id = $1)
		  AND ($2 = '' OR ($2 = 'active') = (`+activeGrant+`))
		  AND ($3::timestamptz IS NULL OR (g.created_at, g.id) < ($3, $4))
		ORDER BY g.created_at DESC, g.id DESC LIMIT $5`, userID, status, nullTime(c.T), c.ID, p.Limit+1)
	if err != nil {
		return err
	}
	defer rows.Close()
	list := []AdminConnection{}
	for rows.Next() {
		var x AdminConnection
		var clientID string
		if err := rows.Scan(&x.ID, &x.UserID, &x.UserEmail, &x.ClientName, &x.ClientKind, &clientID, &x.Scopes, &x.CreatedAt,
			&x.FirstUsedAt, &x.LastUsedAt, &x.ExpiresAt, &x.RevokedAt, &x.RevokeReason); err != nil {
			return err
		}
		x.ClientHost = clientInfo(clientID, x.ClientKind, x.ClientName).Host
		list = append(list, x)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(list, p.Limit, func(x AdminConnection) any { return cursor{x.CreatedAt, x.ID} }))
	return nil
}

// adminRevokeConnection is DELETE /v1/admin/mcp/connections/{id}: ends one
// grant, audited as mcp.oauth.revoke.
func (s *Service) adminRevokeConnection(w http.ResponseWriter, r *http.Request) error {
	staff := auth.MustFromContext(r.Context())
	grantID, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	err = db.WithTx(r.Context(), s.d.Pool, func(tx pgx.Tx) error {
		var owner uuid.UUID
		var name string
		err := tx.QueryRow(r.Context(), `
			SELECT g.user_id, c.client_name FROM mcp_oauth_grants g JOIN mcp_oauth_clients c ON c.id = g.client_ref
			WHERE g.id = $1 AND g.revoked_at IS NULL FOR UPDATE OF g`, grantID).Scan(&owner, &name)
		if db.IsNoRows(err) {
			return httpx.ErrNotFound
		}
		if err != nil {
			return err
		}
		if err := revokeGrant(r.Context(), tx, grantID, "staff", &staff.UserID); err != nil {
			return err
		}
		return audit.Log(r.Context(), tx, staff.UserID, "mcp.oauth.revoke", "mcp_oauth_grant", grantID.String(),
			map[string]any{"user_id": owner, "client_name": name})
	})
	if err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}
