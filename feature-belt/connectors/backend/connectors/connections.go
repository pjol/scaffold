package connectors

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// listForUser is GET /v1/connectors: the catalog (enabled entries, plus
// turned-off ones the caller is still connected to), the caller's own
// servers, and the caller's connection to each. The catalog is small and
// staff-curated, so it is one page.
func (s *Service) listForUser(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	rows, err := s.d.Pool.Query(r.Context(), `
		SELECT `+connectorCols+`, cc.id
		FROM connectors c
		LEFT JOIN connector_connections cc ON cc.connector_id = c.id AND cc.user_id = $1
		WHERE (c.owner_id IS NULL AND (c.enabled OR cc.id IS NOT NULL)) OR c.owner_id = $1
		ORDER BY c.owner_id NULLS FIRST, lower(c.name), c.id
		LIMIT $2`, id.UserID, maxConnectorsAll)
	if err != nil {
		return err
	}
	type pair struct {
		c      *connectorRow
		connID *uuid.UUID
	}
	var list []pair
	for rows.Next() {
		var c connectorRow
		var connID *uuid.UUID
		if err := rows.Scan(&c.ID, &c.OwnerID, &c.Slug, &c.Name, &c.Description, &c.IconURL, &c.ServerURL, &c.AuthType,
			&c.OAuthClientID, &c.OAuthClientSecretEnc, &c.OAuthScopes, &c.WritePolicy, &c.Enabled, &c.CreatedAt, &c.UpdatedAt, &connID); err != nil {
			rows.Close()
			return err
		}
		list = append(list, pair{&c, connID})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	conns, err := s.userConnections(r.Context(), id.UserID)
	if err != nil {
		return err
	}
	items := make([]Connector, 0, len(list))
	for _, p := range list {
		var cc *connectionRow
		if p.connID != nil {
			cc = conns[*p.connID]
		}
		items = append(items, s.connectorDTO(p.c, cc))
	}
	httpx.OK(w, ConnectorList{Items: items, NextCursor: nil, AllowCustom: s.allowCustom})
	return nil
}

// ConnectorList is GET /v1/connectors: a single page plus whether the caller
// may add their own servers.
type ConnectorList struct {
	Items       []Connector `json:"items"`
	NextCursor  *string     `json:"next_cursor"`
	AllowCustom bool        `json:"allow_custom"`
}

func (s *Service) userConnections(ctx context.Context, userID uuid.UUID) (map[uuid.UUID]*connectionRow, error) {
	rows, err := s.d.Pool.Query(ctx, `SELECT `+connectionCols+` FROM connector_connections cc WHERE cc.user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]*connectionRow{}
	for rows.Next() {
		cc, err := scanConnection(rows)
		if err != nil {
			return nil, err
		}
		out[cc.ID] = cc
	}
	return out, rows.Err()
}

// getConnection is GET /v1/connectors/connections/{id}: the connector with
// the caller's connection.
func (s *Service) getConnection(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	connID, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	dto, err := s.connectorWithConnection(r.Context(), id.UserID, connID)
	if err != nil {
		return err
	}
	httpx.OK(w, dto)
	return nil
}

type updateConnectionReq struct {
	WritesEnabled *bool `json:"writes_enabled"`
}

// updateConnection is PATCH /v1/connectors/connections/{id}: turn changes
// (write tools) on or off for this connection.
func (s *Service) updateConnection(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	connID, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req updateConnectionReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.WritesEnabled == nil {
		return httpx.BadRequest("Send writes_enabled.")
	}
	_, c, err := s.ownConnection(r.Context(), s.d.Pool, id.UserID, connID, false)
	if err != nil {
		return err
	}
	if *req.WritesEnabled && c.WritePolicy == "disabled" {
		return httpx.NewError(http.StatusForbidden, "writes_disabled", "Changes through "+c.Name+" are turned off on this server.")
	}
	if _, err := s.d.Pool.Exec(r.Context(), `UPDATE connector_connections SET writes_enabled = $3, updated_at = now()
		WHERE id = $1 AND user_id = $2`, connID, id.UserID, *req.WritesEnabled); err != nil {
		return err
	}
	return s.getConnection(w, r)
}

type toolEnabledReq struct {
	Enabled *bool `json:"enabled"`
}

// setToolEnabled is PUT /v1/connectors/connections/{id}/tools/{tool}:
// switch one remote tool on or off (stored as the switched-off list, so a
// tool the server adds later starts on, subject to the changes switch).
func (s *Service) setToolEnabled(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	connID, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	tool, err := url.PathUnescape(chi.URLParam(r, "tool"))
	if err != nil || tool == "" || len(tool) > 128 {
		return httpx.BadRequest("Invalid tool name.")
	}
	var req toolEnabledReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.Enabled == nil {
		return httpx.BadRequest("Send enabled.")
	}
	sql := `UPDATE connector_connections SET disabled_tools = array_remove(disabled_tools, $3), updated_at = now()
		WHERE id = $1 AND user_id = $2`
	if !*req.Enabled {
		sql = `UPDATE connector_connections SET disabled_tools = array_append(array_remove(disabled_tools, $3), $3), updated_at = now()
			WHERE id = $1 AND user_id = $2 AND cardinality(disabled_tools) < 512`
	}
	tag, err := s.d.Pool.Exec(r.Context(), sql, connID, id.UserID, tool)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.NotFound("That connection doesn't exist.")
	}
	return s.getConnection(w, r)
}

// ConnectionTool is one remote tool as the settings screen lists it.
type ConnectionTool struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	// ReadOnly as this app treats it: the server's readOnlyHint for catalog
	// connectors; false for a user's own server.
	ReadOnly bool `json:"read_only"`
	// Enabled: the user hasn't switched it off.
	Enabled bool `json:"enabled"`
	// Offered: the assistant gets it right now (enabled, and read-only or
	// changes allowed).
	Offered bool `json:"offered"`
}

// ConnectionToolList is GET /v1/connectors/connections/{id}/tools.
type ConnectionToolList struct {
	Items      []ConnectionTool `json:"items"`
	NextCursor *string          `json:"next_cursor"`
	FetchedAt  time.Time        `json:"fetched_at"`
}

// connectionTools lists the server's tools for the caller's connection
// (cached; ?refresh=true asks the server again).
func (s *Service) connectionTools(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	connID, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	cc, c, err := s.ownConnection(r.Context(), s.d.Pool, id.UserID, connID, false)
	if err != nil {
		return err
	}
	if cc.Status != "active" {
		return remoteHTTPError(c, errNeedsReauth)
	}
	tools, at, err := s.remoteTools(r.Context(), c, cc, r.URL.Query().Get("refresh") == "true")
	if err != nil {
		return remoteHTTPError(c, err)
	}
	items := make([]ConnectionTool, 0, len(tools))
	for _, t := range tools {
		readOnly, offered := offeredTool(c, cc, t)
		title := t.Title
		if title == "" {
			title = t.Name
		}
		items = append(items, ConnectionTool{Name: t.Name, Title: title, Description: truncate(strings.TrimSpace(t.Description), 400),
			ReadOnly: readOnly, Enabled: !slices.Contains(cc.DisabledTools, t.Name), Offered: offered})
	}
	slices.SortFunc(items, func(a, b ConnectionTool) int {
		if a.ReadOnly != b.ReadOnly { // reads first
			if a.ReadOnly {
				return -1
			}
			return 1
		}
		return strings.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title))
	})
	httpx.OK(w, ConnectionToolList{Items: items, FetchedAt: at.UTC()})
	return nil
}

// Disconnected is the answer to DELETE /v1/connectors/connections/{id}.
// Revocation: revoked (the provider confirmed), pending (queued; retried),
// unsupported (the provider offers no revocation: remove access there).
type Disconnected struct {
	Revocation string `json:"revocation"`
}

// disconnect deletes our copy of the tokens at once, then asks the provider
// to revoke them (RFC 7009) when it can.
func (s *Service) disconnect(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	connID, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	revocable := false
	var queued []uuid.UUID
	err = db.WithTx(r.Context(), s.d.Pool, func(tx pgx.Tx) error {
		cc, _, err := s.ownConnection(r.Context(), tx, id.UserID, connID, true)
		if err != nil {
			return err
		}
		revocable = cc.RevocationEndpoint != ""
		if queued, err = s.queueConnection(r.Context(), tx, cc); err != nil {
			return err
		}
		_, err = tx.Exec(r.Context(), `DELETE FROM connector_connections WHERE id = $1`, cc.ID)
		return err
	})
	if err != nil {
		return err
	}
	s.tools.forget(connID)
	out := Disconnected{Revocation: "unsupported"}
	if revocable {
		out.Revocation = "pending"
		if s.revokeNow(context.WithoutCancel(r.Context()), queued) == 0 {
			out.Revocation = "revoked"
		}
	}
	httpx.OK(w, out)
	return nil
}

// ---- custom servers ------------------------------------------------------------

type customReq struct {
	Name      string `json:"name"`
	ServerURL string `json:"server_url"`
	AuthType  string `json:"auth_type"`
}

// createCustom is POST /v1/connectors/custom: add your own server (when
// CONNECTORS_ALLOW_CUSTOM is on). OAuth servers are checked for sign-in
// metadata first, so a wrong URL fails here rather than at connect.
func (s *Service) createCustom(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	if !s.allowCustom {
		return httpx.NewError(http.StatusForbidden, "custom_disabled", "Adding your own servers is turned off on this server.")
	}
	var req customReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	in := connectorInput{Name: &req.Name, ServerURL: &req.ServerURL, AuthType: &req.AuthType}
	v, err := s.validateInput(in, nil)
	if err != nil {
		return err
	}
	if v.AuthType == "oauth" {
		dctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		sa, err := s.discover(dctx, v.ServerURL)
		if err != nil {
			return discoveryHTTPError(err)
		}
		if sa.NoAuth {
			return httpx.NewError(http.StatusUnprocessableEntity, "no_sign_in", "That server doesn't ask for sign-in, so it can't act as you.")
		}
		if sa.AS.RegistrationEndpoint == "" && !(sa.AS.ClientIDMetadataDocumentSupported && s.canUseMetadataDocument()) {
			return httpx.NewError(http.StatusUnprocessableEntity, "connector_registration_unsupported",
				"That server only works with a client ID registered in advance. Ask an admin to add it to the catalog.")
		}
	}
	var newID uuid.UUID
	err = db.WithTx(r.Context(), s.d.Pool, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(r.Context(), `SELECT count(*) FROM connectors WHERE owner_id = $1`, id.UserID).Scan(&n); err != nil {
			return err
		}
		if n >= maxCustomPerUser {
			return httpx.NewError(http.StatusConflict, "custom_limit", "You can add up to 10 servers. Remove one first.")
		}
		slug, err := s.freeSlug(r.Context(), tx, deriveSlug(v.Name), &id.UserID)
		if err != nil {
			return err
		}
		return tx.QueryRow(r.Context(), `
			INSERT INTO connectors (owner_id, slug, name, server_url, auth_type, write_policy, created_by)
			VALUES ($1, $2, $3, $4, $5, 'off', $1) RETURNING id`,
			id.UserID, slug, v.Name, v.ServerURL, v.AuthType).Scan(&newID)
	})
	if err != nil {
		return err
	}
	c, err := s.visibleConnector(r.Context(), s.d.Pool, id.UserID, newID)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, s.connectorDTO(c, nil))
	return nil
}

// deleteCustom is DELETE /v1/connectors/custom/{id}: remove your own server
// and its connection (tokens revoked as on disconnect).
func (s *Service) deleteCustom(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	cid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var connIDs []uuid.UUID
	err = db.WithTx(r.Context(), s.d.Pool, func(tx pgx.Tx) error {
		var owner *uuid.UUID
		if err := tx.QueryRow(r.Context(), `SELECT owner_id FROM connectors WHERE id = $1 FOR UPDATE`, cid).Scan(&owner); err != nil {
			if isNoRows(err) {
				return httpx.NotFound("That server doesn't exist.")
			}
			return err
		}
		if owner == nil || *owner != id.UserID {
			return httpx.NotFound("That server doesn't exist.")
		}
		ids, err := s.queueConnectorRevocations(r.Context(), tx, cid)
		if err != nil {
			return err
		}
		connIDs = ids
		_, err = tx.Exec(r.Context(), `DELETE FROM connectors WHERE id = $1`, cid)
		return err
	})
	if err != nil {
		return err
	}
	for _, c := range connIDs {
		s.tools.forget(c)
	}
	s.processUserRevocations(context.WithoutCancel(r.Context()), id.UserID)
	httpx.NoContent(w)
	return nil
}

// queueConnectorRevocations queues the tokens of every connection to a
// connector about to be deleted and returns their ids.
func (s *Service) queueConnectorRevocations(ctx context.Context, tx pgx.Tx, connectorID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `SELECT `+connectionCols+` FROM connector_connections cc WHERE cc.connector_id = $1 FOR UPDATE`, connectorID)
	if err != nil {
		return nil, err
	}
	var list []*connectionRow
	for rows.Next() {
		cc, err := scanConnection(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		list = append(list, cc)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(list))
	for _, cc := range list {
		if err := s.enqueueConnectionRevocations(ctx, tx, cc); err != nil {
			return nil, err
		}
		ids = append(ids, cc.ID)
	}
	return ids, nil
}

// freeSlug returns base, or base with a number, that no catalog entry and
// none of owner's servers use (owner nil: catalog only).
func (s *Service) freeSlug(ctx context.Context, q db.Querier, base string, owner *uuid.UUID) (string, error) {
	for i := 0; i < 50; i++ {
		slug := base
		if i > 0 {
			suffix := itoa(i + 1)
			slug = base[:min(len(base), 20-len(suffix))] + suffix
		}
		var taken bool
		if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM connectors WHERE slug = $1 AND (owner_id IS NULL OR owner_id = $2))`,
			slug, owner).Scan(&taken); err != nil {
			return "", err
		}
		if !taken {
			return slug, nil
		}
	}
	return "", errors.New("connectors: no free slug")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
