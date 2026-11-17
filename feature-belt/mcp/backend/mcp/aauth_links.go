package mcp

import (
	"context"
	"crypto/subtle"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/aauth"
	"scaffoldapp/backend/internal/audit"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/legal"
)

// The person's side of AAuth: the link page (/connect/aauth), where a
// signed-in person claims a code an agent was given and links the person
// server's identifier for them to their account, and the linked person
// servers in Settings → Connected apps, with staff's view across people.

var (
	errInvalidCode = httpx.NewError(http.StatusGone, "invalid_code",
		"That code isn't valid or was already used. Ask the agent for a new one.")
	errLinkExpired = httpx.NewError(http.StatusGone, "request_expired",
		"This request expired. Ask the agent to try again.")
	errBadCallback = httpx.NewError(http.StatusBadRequest, "invalid_callback",
		"The return address must be an https URL, or http on this computer.")
)

// AAuthServer is a person server as a page shows it: its verified address,
// and the name it gives itself ("" when none).
type AAuthServer struct {
	Issuer string `json:"issuer"`
	Host   string `json:"host"`
	Name   string `json:"name"`
}

// AAuthInteraction is a link request as the person deciding it sees it.
type AAuthInteraction struct {
	ID           string      `json:"id"`
	PersonServer AAuthServer `json:"person_server"`
	// Email is the verified email the person server asserted for its account;
	// EmailMatch compares it with the signed-in account's: match, mismatch,
	// or none when it asserted none.
	Email      *string   `json:"email"`
	EmailMatch string    `json:"email_match"`
	ReturnHost *string   `json:"return_host"`
	Status     string    `json:"status"` // interacting, linked or denied
	ExpiresAt  time.Time `json:"expires_at"`
}

// claimInteraction is POST /v1/mcp/aauth/interactions {code, callback?}: the
// person arrives with a code. A code is single use and binds the request to
// whoever claims it; "the code alone MUST NOT authorize the decision", so the
// page then asks them to decide. A wrong code counts against every open
// request sharing its first four symbols, and the fifth ends them.
func (s *Service) claimInteraction(w http.ResponseWriter, r *http.Request) error {
	if s.aauth == nil {
		return httpx.ErrNotFound
	}
	caller := auth.MustFromContext(r.Context())
	var in struct {
		Code     string `json:"code"`
		Callback string `json:"callback"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	code, err := aauth.NormalizeInteractionCode(in.Code)
	if err != nil || len(code) < aauth.MinInteractionCodeSymbols {
		return errInvalidCode
	}
	callback, err := checkCallback(in.Callback)
	if err != nil {
		return err
	}
	ctx := r.Context()
	var id string
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, code_hash FROM mcp_aauth_pending
			WHERE code_prefix = $1 AND status = 'pending' AND expires_at > now() FOR UPDATE`, code[:4])
		if err != nil {
			return err
		}
		want := codeHash(code)
		for rows.Next() {
			var rowID string
			var hash []byte
			if err := rows.Scan(&rowID, &hash); err != nil {
				rows.Close()
				return err
			}
			if subtle.ConstantTimeCompare(hash, want) == 1 {
				id = rowID
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if id == "" {
			_, err := tx.Exec(ctx, `
				UPDATE mcp_aauth_pending SET failures = failures + 1,
				       status = CASE WHEN failures + 1 >= $2 THEN 'failed' ELSE status END
				WHERE code_prefix = $1 AND status = 'pending'`, code[:4], aauthCodeFailure)
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE mcp_aauth_pending SET status = 'interacting', viewer_id = $2, callback = $3 WHERE id = $1`,
			id, caller.UserID, nullString(callback))
		return err
	})
	if err != nil {
		return err
	}
	if id == "" {
		return errInvalidCode
	}
	view, err := s.interaction(ctx, caller, id)
	if err != nil {
		return err
	}
	httpx.OK(w, view)
	return nil
}

// describeInteraction is GET /v1/mcp/aauth/interactions/{id}: the request
// the caller claimed, for a reload of the page.
func (s *Service) describeInteraction(w http.ResponseWriter, r *http.Request) error {
	if s.aauth == nil {
		return httpx.ErrNotFound
	}
	view, err := s.interaction(r.Context(), auth.MustFromContext(r.Context()), chi.URLParam(r, "id"))
	if err != nil {
		return err
	}
	httpx.OK(w, view)
	return nil
}

func (s *Service) interaction(ctx context.Context, caller *auth.Identity, id string) (*AAuthInteraction, error) {
	var ps, status string
	var email, callback *string
	var viewer *uuid.UUID
	var expires time.Time
	err := s.d.Pool.QueryRow(ctx, `
		SELECT ps_iss, email, callback, status, viewer_id, expires_at FROM mcp_aauth_pending WHERE id = $1`, id,
	).Scan(&ps, &email, &callback, &status, &viewer, &expires)
	if db.IsNoRows(err) || (err == nil && (viewer == nil || *viewer != caller.UserID)) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if status == "interacting" && !time.Now().Before(expires) {
		return nil, errLinkExpired
	}
	view := &AAuthInteraction{
		ID:           id,
		PersonServer: AAuthServer{Issuer: ps, Host: aauth.ServerHost(ps), Name: s.aauth.psName(ctx, ps)},
		Email:        email,
		EmailMatch:   emailMatch(email, caller.Email),
		Status:       status,
		ExpiresAt:    expires,
	}
	if callback != nil {
		if u, err := url.Parse(*callback); err == nil {
			view.ReturnHost = &u.Host
		}
	}
	return view, nil
}

func emailMatch(asserted *string, account string) string {
	switch {
	case asserted == nil:
		return "none"
	case strings.EqualFold(strings.TrimSpace(*asserted), strings.TrimSpace(account)):
		return "match"
	}
	return "mismatch"
}

// decideInteraction is POST /v1/mcp/aauth/interactions/{id}/decision {allow}:
// links (iss, sub) to the caller, or declines. An identity links to one
// account at a time. The answer says where to send the browser: the
// callback, with error=access_denied on a refusal, or nowhere.
func (s *Service) decideInteraction(w http.ResponseWriter, r *http.Request) error {
	if s.aauth == nil {
		return httpx.ErrNotFound
	}
	ctx := r.Context()
	caller := auth.MustFromContext(ctx)
	id := chi.URLParam(r, "id")
	var in struct {
		Allow bool `json:"allow"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if in.Allow {
		if caller.Status != "active" {
			return errInactive
		}
		if !legal.Current(caller.TermsVersion, caller.PrivacyVersion) {
			return legal.ErrConsentRequired()
		}
	}
	var ps string
	if err := s.d.Pool.QueryRow(ctx, `SELECT ps_iss FROM mcp_aauth_pending WHERE id = $1`, id).Scan(&ps); db.IsNoRows(err) {
		return httpx.ErrNotFound
	} else if err != nil {
		return err
	}
	name := s.aauth.psName(ctx, ps) // outside the transaction: a fetch, usually cached
	var status string
	var redirect *string
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var sub, state string
		var tenant, email, callback *string
		var viewer *uuid.UUID
		var expires time.Time
		err := tx.QueryRow(ctx, `
			SELECT sub, tenant, email, callback, status, viewer_id, expires_at FROM mcp_aauth_pending WHERE id = $1 FOR UPDATE`, id,
		).Scan(&sub, &tenant, &email, &callback, &state, &viewer, &expires)
		if err != nil {
			return err
		}
		if viewer == nil || *viewer != caller.UserID {
			return httpx.ErrNotFound
		}
		if state != "interacting" {
			return httpx.NewError(http.StatusConflict, "already_decided", "This request was already answered.")
		}
		if !time.Now().Before(expires) {
			return errLinkExpired
		}
		if !in.Allow {
			status, redirect = "denied", returnURL(callback, false)
			_, err := tx.Exec(ctx, `UPDATE mcp_aauth_pending SET status = 'denied' WHERE id = $1`, id)
			return err
		}
		// Two requests for one identity can be decided at once: the unique
		// index settles it, and the loser sees the winner's owner.
		if _, err := tx.Exec(ctx, `
			INSERT INTO mcp_aauth_links (user_id, ps_iss, sub, ps_name, email, tenant) VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (ps_iss, sub) WHERE revoked_at IS NULL DO NOTHING`,
			caller.UserID, ps, sub, name, email, tenant); err != nil {
			return err
		}
		var owner uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT user_id FROM mcp_aauth_links WHERE ps_iss = $1 AND sub = $2 AND revoked_at IS NULL`,
			ps, sub).Scan(&owner); err != nil {
			return err
		}
		if owner != caller.UserID {
			return httpx.NewError(http.StatusConflict, "already_linked",
				"This person server account is linked to another account here. Its owner can unlink it in Settings → Connected apps.")
		}
		// The agent has another stretch to collect the held call.
		status, redirect = "linked", returnURL(callback, true)
		_, err = tx.Exec(ctx, `UPDATE mcp_aauth_pending SET status = 'linked', expires_at = now() + make_interval(secs => $2) WHERE id = $1`,
			id, aauthPendingTTL.Seconds())
		return err
	})
	if db.IsNoRows(err) {
		return httpx.ErrNotFound
	}
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"status": status, "redirect_url": redirect})
	return nil
}

// checkCallback accepts where the person returns after deciding ("A
// resource's interaction endpoint MUST support the ?code=...&callback=...
// pattern"): https, or http on loopback for an agent on this computer, with
// no credentials or fragment. "" is no callback.
func checkCallback(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 2048 || u.Host == "" || u.User != nil || u.Fragment != "" {
		return "", errBadCallback
	}
	switch {
	case u.Scheme == "https":
	case u.Scheme == "http" && isLoopbackHost(u.Hostname()):
	default:
		return "", errBadCallback
	}
	return u.String(), nil
}

// returnURL is the callback to send the browser to, with error=access_denied
// on a refusal ("the server MUST redirect to the callback URL with an error
// query parameter").
func returnURL(callback *string, allowed bool) *string {
	if callback == nil {
		return nil
	}
	if allowed {
		return callback
	}
	u := withParams(*callback, map[string]string{"error": aauth.CallbackAccessDenied})
	return &u
}

// ---- linked person servers -------------------------------------------------

// AAuthLink is a linked person server as its owner sees it.
type AAuthLink struct {
	ID           uuid.UUID   `json:"id"`
	PersonServer AAuthServer `json:"person_server"`
	Email        *string     `json:"email"`
	// Agents is how many agents (signing keys) used the link in the last 30 days.
	Agents     int        `json:"agents"`
	LinkedAt   time.Time  `json:"linked_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

const linkAgents = `(SELECT count(*)::int FROM mcp_aauth_agents a WHERE a.link_id = l.id AND a.last_seen_at > now() - interval '30 days')`

// listLinks is GET /v1/mcp/aauth/links: the caller's linked person servers,
// most recently linked first.
func (s *Service) listLinks(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	p := httpx.ParsePage(r)
	var c cursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	rows, err := s.d.Pool.Query(r.Context(), `
		SELECT l.id, l.ps_iss, l.ps_name, l.email, `+linkAgents+`, l.created_at, l.last_used_at
		FROM mcp_aauth_links l
		WHERE l.user_id = $1 AND l.revoked_at IS NULL
		  AND ($2::timestamptz IS NULL OR (l.created_at, l.id) < ($2, $3))
		ORDER BY l.created_at DESC, l.id DESC LIMIT $4`, id.UserID, nullTime(c.T), c.ID, p.Limit+1)
	if err != nil {
		return err
	}
	defer rows.Close()
	list := []AAuthLink{}
	for rows.Next() {
		var x AAuthLink
		if err := rows.Scan(&x.ID, &x.PersonServer.Issuer, &x.PersonServer.Name, &x.Email, &x.Agents, &x.LinkedAt, &x.LastUsedAt); err != nil {
			return err
		}
		x.PersonServer.Host = aauth.ServerHost(x.PersonServer.Issuer)
		list = append(list, x)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(list, p.Limit, func(x AAuthLink) any { return cursor{x.LinkedAt, x.ID} }))
	return nil
}

// unlink is DELETE /v1/mcp/aauth/links/{id}: the person server's agents stop
// acting as the caller at once; linking again needs the page.
func (s *Service) unlink(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	linkID, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	tag, err := s.d.Pool.Exec(r.Context(), `
		UPDATE mcp_aauth_links SET revoked_at = now(), revoked_by = $2, revoke_reason = 'user'
		WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`, linkID, id.UserID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.ErrNotFound
	}
	httpx.NoContent(w)
	return nil
}

// AdminAAuthLink is a link as staff see it.
type AdminAAuthLink struct {
	AAuthLink
	UserID       uuid.UUID  `json:"user_id"`
	UserEmail    string     `json:"user_email"`
	RevokedAt    *time.Time `json:"revoked_at"`
	RevokeReason *string    `json:"revoke_reason"`
}

// adminListLinks is GET /v1/admin/mcp/aauth/links?user_id=&status=active|revoked.
func (s *Service) adminListLinks(w http.ResponseWriter, r *http.Request) error {
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
		SELECT l.id, l.ps_iss, l.ps_name, l.email, `+linkAgents+`, l.created_at, l.last_used_at,
		       l.user_id, u.email, l.revoked_at, l.revoke_reason
		FROM mcp_aauth_links l JOIN users u ON u.id = l.user_id
		WHERE ($1::uuid IS NULL OR l.user_id = $1)
		  AND ($2 = '' OR ($2 = 'active') = (l.revoked_at IS NULL))
		  AND ($3::timestamptz IS NULL OR (l.created_at, l.id) < ($3, $4))
		ORDER BY l.created_at DESC, l.id DESC LIMIT $5`, userID, status, nullTime(c.T), c.ID, p.Limit+1)
	if err != nil {
		return err
	}
	defer rows.Close()
	list := []AdminAAuthLink{}
	for rows.Next() {
		var x AdminAAuthLink
		if err := rows.Scan(&x.ID, &x.PersonServer.Issuer, &x.PersonServer.Name, &x.Email, &x.Agents, &x.LinkedAt, &x.LastUsedAt,
			&x.UserID, &x.UserEmail, &x.RevokedAt, &x.RevokeReason); err != nil {
			return err
		}
		x.PersonServer.Host = aauth.ServerHost(x.PersonServer.Issuer)
		list = append(list, x)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(list, p.Limit, func(x AdminAAuthLink) any { return cursor{x.LinkedAt, x.ID} }))
	return nil
}

// adminUnlink is DELETE /v1/admin/mcp/aauth/links/{id}, audited as
// mcp.aauth.unlink.
func (s *Service) adminUnlink(w http.ResponseWriter, r *http.Request) error {
	staff := auth.MustFromContext(r.Context())
	linkID, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	err = db.WithTx(r.Context(), s.d.Pool, func(tx pgx.Tx) error {
		var owner uuid.UUID
		var ps string
		err := tx.QueryRow(r.Context(), `
			UPDATE mcp_aauth_links SET revoked_at = now(), revoked_by = $2, revoke_reason = 'staff'
			WHERE id = $1 AND revoked_at IS NULL RETURNING user_id, ps_iss`, linkID, staff.UserID).Scan(&owner, &ps)
		if db.IsNoRows(err) {
			return httpx.ErrNotFound
		}
		if err != nil {
			return err
		}
		return audit.Log(r.Context(), tx, staff.UserID, "mcp.aauth.unlink", "mcp_aauth_link", linkID.String(),
			map[string]any{"user_id": owner, "person_server": ps})
	})
	if err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

// exportedLink is a linked person server in the data export.
type exportedLink struct {
	PersonServer string     `json:"person_server"`
	Name         string     `json:"name"`
	Email        *string    `json:"email"`
	Tenant       *string    `json:"tenant"`
	LinkedAt     time.Time  `json:"linked_at"`
	LastUsedAt   *time.Time `json:"last_used_at"`
	RevokedAt    *time.Time `json:"revoked_at"`
	RevokeReason *string    `json:"revoke_reason"`
}
