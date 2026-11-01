package connectors

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/audit"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// Staff manage the catalog in the admin console. Support reads it; admins
// create, edit and delete, each change audited. Client secrets are sealed
// and never returned (has_client_secret says whether one is set).

// AdminConnector is a catalog entry as staff see it.
type AdminConnector struct {
	ID              uuid.UUID `json:"id"`
	Slug            string    `json:"slug"`
	Name            string    `json:"name"`
	Description     string    `json:"description"`
	IconURL         string    `json:"icon_url"`
	ServerURL       string    `json:"server_url"`
	AuthType        string    `json:"auth_type"`
	OAuthClientID   string    `json:"oauth_client_id"`
	HasClientSecret bool      `json:"has_client_secret"`
	OAuthScopes     string    `json:"oauth_scopes"`
	WritePolicy     string    `json:"write_policy"`
	Enabled         bool      `json:"enabled"`
	Connections     int       `json:"connections"`
	NeedsReauth     int       `json:"needs_reauth"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

const adminCols = connectorCols + `,
	(SELECT count(*) FROM connector_connections x WHERE x.connector_id = c.id)::int,
	(SELECT count(*) FROM connector_connections x WHERE x.connector_id = c.id AND x.status = 'needs_reauth')::int`

func scanAdmin(row pgx.Row) (*AdminConnector, error) {
	var c connectorRow
	var a AdminConnector
	err := row.Scan(&c.ID, &c.OwnerID, &c.Slug, &c.Name, &c.Description, &c.IconURL, &c.ServerURL, &c.AuthType,
		&c.OAuthClientID, &c.OAuthClientSecretEnc, &c.OAuthScopes, &c.WritePolicy, &c.Enabled, &c.CreatedAt, &c.UpdatedAt,
		&a.Connections, &a.NeedsReauth)
	if err != nil {
		return nil, err
	}
	a.ID, a.Slug, a.Name, a.Description, a.IconURL, a.ServerURL = c.ID, c.Slug, c.Name, c.Description, c.IconURL, c.ServerURL
	a.AuthType, a.OAuthClientID, a.HasClientSecret, a.OAuthScopes = c.AuthType, c.OAuthClientID, len(c.OAuthClientSecretEnc) > 0, c.OAuthScopes
	a.WritePolicy, a.Enabled, a.CreatedAt, a.UpdatedAt = c.WritePolicy, c.Enabled, c.CreatedAt, c.UpdatedAt
	return &a, nil
}

type cursor struct {
	T  time.Time `json:"t"`
	ID uuid.UUID `json:"id"`
}

func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// adminList is GET /v1/admin/connectors: catalog entries, newest first.
func (s *Service) adminList(w http.ResponseWriter, r *http.Request) error {
	p := httpx.ParsePage(r)
	var cur cursor
	if err := httpx.DecodeCursor(p.Cursor, &cur); err != nil {
		return err
	}
	rows, err := s.d.Pool.Query(r.Context(), `SELECT `+adminCols+` FROM connectors c
		WHERE c.owner_id IS NULL AND ($1::timestamptz IS NULL OR (c.created_at, c.id) < ($1, $2))
		ORDER BY c.created_at DESC, c.id DESC LIMIT $3`, nullTime(cur.T), cur.ID, p.Limit+1)
	if err != nil {
		return err
	}
	defer rows.Close()
	list := []AdminConnector{}
	for rows.Next() {
		a, err := scanAdmin(rows)
		if err != nil {
			return err
		}
		list = append(list, *a)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(list, p.Limit, func(a AdminConnector) any { return cursor{a.CreatedAt, a.ID} }))
	return nil
}

func (s *Service) adminLoad(ctx context.Context, q db.Querier, id uuid.UUID) (*AdminConnector, error) {
	a, err := scanAdmin(q.QueryRow(ctx, `SELECT `+adminCols+` FROM connectors c WHERE c.id = $1 AND c.owner_id IS NULL`, id))
	if isNoRows(err) {
		return nil, httpx.NotFound("That connector doesn't exist.")
	}
	return a, err
}

func (s *Service) adminGet(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	a, err := s.adminLoad(r.Context(), s.d.Pool, id)
	if err != nil {
		return err
	}
	httpx.OK(w, a)
	return nil
}

// connectorInput is the create/update body; nil fields are left alone on
// update. oauth_client_secret is write-only: send a value to set it,
// clear_client_secret to remove it.
type connectorInput struct {
	Name              *string `json:"name"`
	Slug              *string `json:"slug"`
	Description       *string `json:"description"`
	IconURL           *string `json:"icon_url"`
	ServerURL         *string `json:"server_url"`
	AuthType          *string `json:"auth_type"`
	OAuthClientID     *string `json:"oauth_client_id"`
	OAuthClientSecret *string `json:"oauth_client_secret"`
	ClearClientSecret bool    `json:"clear_client_secret"`
	OAuthScopes       *string `json:"oauth_scopes"`
	WritePolicy       *string `json:"write_policy"`
	Enabled           *bool   `json:"enabled"`
}

// validated is an input merged onto the current values.
type validated struct {
	Name, Slug, Description, IconURL, ServerURL, AuthType string
	OAuthClientID, OAuthScopes, WritePolicy               string
	Enabled                                               bool
}

var (
	slugRE  = regexp.MustCompile(`^[a-z][a-z0-9]{1,19}$`)
	scopeRE = regexp.MustCompile(`^[\x21\x23-\x5B\x5D-\x7E]+$`) // RFC 6749 scope-token
)

// validateInput checks in (merged onto cur when updating).
func (s *Service) validateInput(in connectorInput, cur *validated) (validated, error) {
	v := validated{AuthType: "oauth", WritePolicy: "off", Enabled: true}
	if cur != nil {
		v = *cur
	}
	set := func(dst *string, src *string) {
		if src != nil {
			*dst = strings.TrimSpace(*src)
		}
	}
	set(&v.Name, in.Name)
	set(&v.Slug, in.Slug)
	set(&v.Description, in.Description)
	set(&v.IconURL, in.IconURL)
	set(&v.ServerURL, in.ServerURL)
	set(&v.AuthType, in.AuthType)
	set(&v.OAuthClientID, in.OAuthClientID)
	set(&v.OAuthScopes, in.OAuthScopes)
	set(&v.WritePolicy, in.WritePolicy)
	if in.Enabled != nil {
		v.Enabled = *in.Enabled
	}

	v.Name = strings.Join(strings.FieldsFunc(v.Name, unicode.IsSpace), " ")
	if n := len([]rune(v.Name)); n == 0 || n > 60 {
		return v, httpx.BadRequest("Give the connector a name of up to 60 characters.")
	}
	if len([]rune(v.Description)) > 300 {
		return v, httpx.BadRequest("Descriptions can be up to 300 characters.")
	}
	if v.IconURL != "" {
		u, err := url.Parse(v.IconURL)
		if err != nil || u.Scheme != "https" || u.Host == "" || len(v.IconURL) > 500 {
			return v, httpx.BadRequest("The icon must be an https image URL.")
		}
	}
	if v.ServerURL == "" || len(v.ServerURL) > 500 {
		return v, httpx.BadRequest("Enter the server's MCP URL.")
	}
	u, err := s.guard.checkURL(v.ServerURL)
	if err != nil {
		if isBlocked(err) {
			return v, httpx.NewError(http.StatusBadRequest, "address_blocked", "That URL isn't allowed: use the server's public https address.")
		}
		return v, httpx.BadRequest("Enter a full URL, like https://mcp.example.com/mcp.")
	}
	v.ServerURL = u.String()
	if v.AuthType != "oauth" && v.AuthType != "bearer" {
		return v, httpx.BadRequest("Sign-in must be oauth or bearer.")
	}
	switch v.WritePolicy {
	case "disabled", "off", "on":
	default:
		return v, httpx.BadRequest("Changes must be disabled, off or on.")
	}
	if len(v.OAuthClientID) > 300 {
		return v, httpx.BadRequest("The client ID is too long.")
	}
	if in.OAuthClientSecret != nil && len(*in.OAuthClientSecret) > 500 {
		return v, httpx.BadRequest("The client secret is too long.")
	}
	if len(v.OAuthScopes) > 500 {
		return v, httpx.BadRequest("Scopes can be up to 500 characters.")
	}
	for _, sc := range strings.Fields(v.OAuthScopes) {
		if !scopeRE.MatchString(sc) {
			return v, httpx.BadRequest("Scopes are space-separated words without quotes or backslashes.")
		}
	}
	v.OAuthScopes = strings.Join(strings.Fields(v.OAuthScopes), " ")
	return v, nil
}

// deriveSlug turns a name into a tool namespace: lowercase letters and
// digits, starting with a letter, at most 20 characters.
func deriveSlug(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9' && b.Len() > 0) {
			b.WriteRune(r)
		}
		if b.Len() == 20 {
			break
		}
	}
	slug := b.String()
	if len(slug) < 2 {
		slug = "server" + slug
	}
	return slug
}

func (s *Service) adminCreate(w http.ResponseWriter, r *http.Request) error {
	staff := auth.MustFromContext(r.Context())
	var in connectorInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	v, err := s.validateInput(in, nil)
	if err != nil {
		return err
	}
	id := uuid.New()
	err = db.WithTx(r.Context(), s.d.Pool, func(tx pgx.Tx) error {
		if v.Slug == "" {
			slug, err := s.freeSlug(r.Context(), tx, deriveSlug(v.Name), nil)
			if err != nil {
				return err
			}
			v.Slug = slug
		}
		if !slugRE.MatchString(v.Slug) {
			return httpx.BadRequest("The slug is 2 to 20 lowercase letters and digits, starting with a letter. Tool names start with it.")
		}
		var secret []byte
		if in.OAuthClientSecret != nil && *in.OAuthClientSecret != "" {
			secret = s.seal.seal(*in.OAuthClientSecret, aadConnector(id))
		}
		_, err := tx.Exec(r.Context(), `
			INSERT INTO connectors (id, slug, name, description, icon_url, server_url, auth_type, oauth_client_id,
			  oauth_client_secret_enc, oauth_scopes, write_policy, enabled, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
			id, v.Slug, v.Name, v.Description, v.IconURL, v.ServerURL, v.AuthType, v.OAuthClientID, secret,
			v.OAuthScopes, v.WritePolicy, v.Enabled, staff.UserID)
		if db.IsUniqueViolation(err, "connectors_catalog_slug") {
			return httpx.NewError(http.StatusConflict, "slug_taken", "Another connector uses that slug. Pick another.")
		}
		if err != nil {
			return err
		}
		return audit.Log(r.Context(), tx, staff.UserID, "connectors.connector.create", "connector", id.String(), auditData(v, secret != nil))
	})
	if err != nil {
		return err
	}
	a, err := s.adminLoad(r.Context(), s.d.Pool, id)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, a)
	return nil
}

func auditData(v validated, secretSet bool) map[string]any {
	return map[string]any{"name": v.Name, "slug": v.Slug, "server_url": v.ServerURL, "auth_type": v.AuthType,
		"oauth_client_id": v.OAuthClientID, "client_secret_set": secretSet, "oauth_scopes": v.OAuthScopes,
		"write_policy": v.WritePolicy, "enabled": v.Enabled}
}

// adminUpdate is PATCH /v1/admin/connectors/{id}. The slug is fixed after
// creation (tool names and users' tool switches depend on it). A new server
// URL or sign-in type asks every connected user to connect again.
func (s *Service) adminUpdate(w http.ResponseWriter, r *http.Request) error {
	staff := auth.MustFromContext(r.Context())
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var in connectorInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	var reauth bool
	err = db.WithTx(r.Context(), s.d.Pool, func(tx pgx.Tx) error {
		cur, err := scanConnector(tx.QueryRow(r.Context(), `SELECT `+connectorCols+` FROM connectors c
			WHERE c.id = $1 AND c.owner_id IS NULL FOR UPDATE`, id))
		if isNoRows(err) {
			return httpx.NotFound("That connector doesn't exist.")
		}
		if err != nil {
			return err
		}
		if in.Slug != nil && strings.TrimSpace(*in.Slug) != cur.Slug {
			return httpx.BadRequest("The slug can't change after creation. Create a new connector instead.")
		}
		before := validated{Name: cur.Name, Slug: cur.Slug, Description: cur.Description, IconURL: cur.IconURL, ServerURL: cur.ServerURL,
			AuthType: cur.AuthType, OAuthClientID: cur.OAuthClientID, OAuthScopes: cur.OAuthScopes, WritePolicy: cur.WritePolicy, Enabled: cur.Enabled}
		v, err := s.validateInput(in, &before)
		if err != nil {
			return err
		}
		secret := cur.OAuthClientSecretEnc
		switch {
		case in.ClearClientSecret:
			secret = nil
		case in.OAuthClientSecret != nil && *in.OAuthClientSecret != "":
			secret = s.seal.seal(*in.OAuthClientSecret, aadConnector(id))
		}
		if _, err := tx.Exec(r.Context(), `
			UPDATE connectors SET name = $2, description = $3, icon_url = $4, server_url = $5, auth_type = $6,
			  oauth_client_id = $7, oauth_client_secret_enc = $8, oauth_scopes = $9, write_policy = $10, enabled = $11, updated_at = now()
			WHERE id = $1`, id, v.Name, v.Description, v.IconURL, v.ServerURL, v.AuthType, v.OAuthClientID, secret,
			v.OAuthScopes, v.WritePolicy, v.Enabled); err != nil {
			return err
		}
		reauth = v.ServerURL != before.ServerURL || v.AuthType != before.AuthType
		if reauth {
			if _, err := tx.Exec(r.Context(), `UPDATE connector_connections SET status = 'needs_reauth',
				last_error = 'The server address changed. Connect again.', last_error_at = now(), updated_at = now()
				WHERE connector_id = $1`, id); err != nil {
				return err
			}
		}
		if v.WritePolicy == "disabled" && before.WritePolicy != "disabled" {
			if _, err := tx.Exec(r.Context(), `UPDATE connector_connections SET writes_enabled = false, updated_at = now()
				WHERE connector_id = $1`, id); err != nil {
				return err
			}
		}
		data := auditData(v, len(secret) > 0)
		data["before"] = auditData(before, len(cur.OAuthClientSecretEnc) > 0)
		return audit.Log(r.Context(), tx, staff.UserID, "connectors.connector.update", "connector", id.String(), data)
	})
	if err != nil {
		return err
	}
	if reauth {
		s.forgetConnector(r.Context(), id)
	}
	a, err := s.adminLoad(r.Context(), s.d.Pool, id)
	if err != nil {
		return err
	}
	httpx.OK(w, a)
	return nil
}

// adminDelete removes a catalog entry and every connection to it (tokens
// queued for revocation first).
func (s *Service) adminDelete(w http.ResponseWriter, r *http.Request) error {
	staff := auth.MustFromContext(r.Context())
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var connIDs []uuid.UUID
	err = db.WithTx(r.Context(), s.d.Pool, func(tx pgx.Tx) error {
		cur, err := scanConnector(tx.QueryRow(r.Context(), `SELECT `+connectorCols+` FROM connectors c
			WHERE c.id = $1 AND c.owner_id IS NULL FOR UPDATE`, id))
		if isNoRows(err) {
			return httpx.NotFound("That connector doesn't exist.")
		}
		if err != nil {
			return err
		}
		ids, err := s.queueConnectorRevocations(r.Context(), tx, id)
		if err != nil {
			return err
		}
		connIDs = ids
		if _, err := tx.Exec(r.Context(), `DELETE FROM connectors WHERE id = $1`, id); err != nil {
			return err
		}
		return audit.Log(r.Context(), tx, staff.UserID, "connectors.connector.delete", "connector", id.String(),
			map[string]any{"name": cur.Name, "slug": cur.Slug, "server_url": cur.ServerURL, "connections": len(ids)})
	})
	if err != nil {
		return err
	}
	for _, c := range connIDs {
		s.tools.forget(c)
	}
	s.kickRevocations(r.Context())
	httpx.NoContent(w)
	return nil
}

func (s *Service) forgetConnector(ctx context.Context, connectorID uuid.UUID) {
	rows, err := s.d.Pool.Query(ctx, `SELECT id FROM connector_connections WHERE connector_id = $1`, connectorID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		if rows.Scan(&id) == nil {
			s.tools.forget(id)
		}
	}
}

type checkReq struct {
	ServerURL     string `json:"server_url"`
	AuthType      string `json:"auth_type"`
	OAuthClientID string `json:"oauth_client_id"`
}

// CheckResult is what a server's discovery found, for staff setting up a
// connector.
type CheckResult struct {
	OK bool `json:"ok"`
	// RequiresAuth is false when the server answered without a token.
	RequiresAuth bool     `json:"requires_auth"`
	Issuer       string   `json:"issuer"`
	Resource     string   `json:"resource"`
	Scopes       []string `json:"scopes"`
	// Registration: preregistered | metadata_document | dynamic |
	// needs_client_id (add a pre-registered client ID).
	Registration string `json:"registration"`
	Revocation   bool   `json:"revocation"`
	Refresh      bool   `json:"refresh"`
	ErrorCode    string `json:"error_code,omitempty"`
	Error        string `json:"error,omitempty"`
}

// adminCheck is POST /v1/admin/connectors/check: run discovery against a
// server and report what connecting will use.
func (s *Service) adminCheck(w http.ResponseWriter, r *http.Request) error {
	var req checkReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	u, err := s.guard.checkURL(req.ServerURL)
	if err != nil {
		httpx.OK(w, CheckResult{Scopes: []string{}, ErrorCode: "address_blocked", Error: "That URL isn't allowed: use the server's public https address."})
		return nil
	}
	if req.AuthType == "bearer" {
		httpx.OK(w, CheckResult{OK: true, RequiresAuth: true, Registration: "none", Resource: canonicalResource(u), Scopes: []string{}})
		return nil
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	sa, err := s.discover(ctx, u.String())
	if err != nil {
		res := CheckResult{Scopes: []string{}, ErrorCode: "discovery_failed", Error: "Couldn't check that server."}
		if de, ok := err.(*discoveryError); ok {
			res.ErrorCode, res.Error = de.Code, de.Message
		}
		httpx.OK(w, res)
		return nil
	}
	res := CheckResult{OK: true, RequiresAuth: !sa.NoAuth, Resource: sa.Resource, Scopes: sa.Scopes}
	if sa.Scopes == nil {
		res.Scopes = []string{}
	}
	if sa.NoAuth {
		res.OK, res.ErrorCode, res.Error = false, "no_sign_in", "The server answers without sign-in, so it can't act as each user."
		httpx.OK(w, res)
		return nil
	}
	res.Issuer = sa.AS.Issuer
	res.Revocation = sa.AS.RevocationEndpoint != ""
	res.Refresh = len(sa.AS.GrantTypesSupported) == 0 || containsStr(sa.AS.GrantTypesSupported, "refresh_token")
	switch {
	case strings.TrimSpace(req.OAuthClientID) != "":
		res.Registration = "preregistered"
	case sa.AS.ClientIDMetadataDocumentSupported && s.canUseMetadataDocument():
		res.Registration = "metadata_document"
	case sa.AS.RegistrationEndpoint != "":
		res.Registration = "dynamic"
	default:
		res.Registration = "needs_client_id"
	}
	httpx.OK(w, res)
	return nil
}

func containsStr(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
