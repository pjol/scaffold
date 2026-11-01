package orgs

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/httprate"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	mailer "scaffoldapp/backend/internal/mail"
	"scaffoldapp/backend/internal/notify"
)

// Tokens are random 32-byte values; only their sha256 is stored. Email
// invites start "oi_", shareable links "ol_". Both open
// PUBLIC_WEB_URL/invites/<token>.
const (
	prefixInvite = "oi_"
	prefixLink   = "ol_"
)

func newToken(prefix string) (token string, hash []byte) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	token = prefix + base64.RawURLEncoding.EncodeToString(b[:])
	return token, hashToken(token)
}

func hashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

// publicPreviewLimit caps anonymous invite lookups per IP.
var publicPreviewLimit = httprate.LimitBy(30, time.Minute, httpx.KeyByClientIP,
	httprate.WithLimitHandler(httpx.RateLimitedHandler))

func (s *Service) inviteURL(token string) string {
	base := s.d.Cfg.PublicWebURL
	if base == "" && len(s.d.Cfg.CORSOrigins) > 0 {
		base = s.d.Cfg.CORSOrigins[0] // development without PUBLIC_WEB_URL
	}
	return strings.TrimRight(base, "/") + "/invites/" + token
}

// OrgRef is the public face of an org on an invite.
type OrgRef struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	AvatarURL *string   `json:"avatar_url"`
}

// Invite is a pending (or past) email invite as org admins see it.
type Invite struct {
	ID            uuid.UUID `json:"id"`
	OrgID         uuid.UUID `json:"org_id"`
	Email         string    `json:"email"`
	Role          string    `json:"role"`
	Status        string    `json:"status"` // pending | accepted | declined | revoked | expired
	InvitedByName *string   `json:"invited_by_name"`
	SentCount     int       `json:"sent_count"`
	LastSentAt    time.Time `json:"last_sent_at"`
	ExpiresAt     time.Time `json:"expires_at"`
	CreatedAt     time.Time `json:"created_at"`
}

const inviteSelect = `
	SELECT i.id, i.org_id, i.email, i.role,
	       CASE WHEN i.status = 'pending' AND i.expires_at < now() THEN 'expired' ELSE i.status END,
	       NULLIF(u.display_name, ''), i.sent_count, i.last_sent_at, i.expires_at, i.created_at
	FROM org_invites i LEFT JOIN users u ON u.id = i.invited_by`

// listInvites: GET /v1/orgs/{orgID}/invites?status=pending|all (admin+).
func (s *Service) listInvites(w http.ResponseWriter, r *http.Request) error {
	m := mustMembership(r)
	p := httpx.ParsePage(r)
	var c cursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	all := r.URL.Query().Get("status") == "all"
	rows, err := s.d.Pool.Query(r.Context(), inviteSelect+`
		WHERE i.org_id = $1 AND ($2 OR (i.status = 'pending' AND i.expires_at > now()))
		  AND ($3::timestamptz IS NULL OR (i.created_at, i.id) < ($3, $4))
		ORDER BY i.created_at DESC, i.id DESC LIMIT $5`, m.OrgID, all, nullTime(c.T), c.ID, p.Limit+1)
	if err != nil {
		return err
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Invite])
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(list, p.Limit, func(i Invite) any { return cursor{i.CreatedAt, i.ID} }))
	return nil
}

type createInviteReq struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

// createInvite: POST /v1/orgs/{orgID}/invites {email, role} (admin+). Emails
// an accept link; an existing account with that email also gets an in-app
// notification. Ownership is never invited; transfer it instead.
func (s *Service) createInvite(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	m := mustMembership(r)
	var req createInviteReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if addr, err := mail.ParseAddress(email); err != nil || addr.Address != email || len(email) > 254 {
		return httpx.BadRequest("Enter a valid email address.")
	}
	if req.Role == "" {
		req.Role = RoleMember
	}
	if req.Role != RoleMember && req.Role != RoleAdmin {
		return httpx.BadRequest("role must be member or admin.")
	}
	token, hash := newToken(prefixInvite)
	var (
		inv     Invite
		orgName string
	)
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var err error
		if orgName, err = lockOrg(ctx, tx, m.OrgID); err != nil {
			return err
		}
		var isMember bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM org_members om JOIN users u ON u.id = om.user_id
			               WHERE om.org_id = $1 AND lower(u.email) = $2)`, m.OrgID, email).Scan(&isMember); err != nil {
			return err
		}
		if isMember {
			return httpx.Conflict("They're already a member.")
		}
		// A lapsed invite to the same address frees the slot.
		if _, err := tx.Exec(ctx, `UPDATE org_invites SET status = 'expired', updated_at = now()
			WHERE org_id = $1 AND email = $2 AND status = 'pending' AND expires_at < now()`, m.OrgID, email); err != nil {
			return err
		}
		var pending int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM org_invites WHERE org_id = $1 AND status = 'pending'`,
			m.OrgID).Scan(&pending); err != nil {
			return err
		}
		if pending >= maxPendingInvites {
			return httpx.Conflict(fmt.Sprintf("%d invites are waiting. Revoke some before sending more.", maxPendingInvites))
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO org_invites (org_id, email, role, token_hash, invited_by, expires_at)
			VALUES ($1, $2, $3, $4, $5, now() + make_interval(secs => $6)) RETURNING id`,
			m.OrgID, email, req.Role, hash, me.UserID, inviteTTL.Seconds()).Scan(&id); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, inviteSelect+` WHERE i.id = $1`, id)
		if err != nil {
			return err
		}
		inv, err = pgx.CollectExactlyOneRow(rows, pgx.RowToStructByPos[Invite])
		return err
	})
	if db.IsUniqueViolation(err, "org_invites_one_pending_idx") {
		return httpx.Conflict("That email already has a pending invite. Resend it instead.")
	}
	if err != nil {
		return err
	}
	s.deliverInvite(context.WithoutCancel(ctx), inv, token, orgName, me.DisplayName)
	httpx.JSON(w, http.StatusCreated, inv)
	return nil
}

// resendInvite: POST /v1/orgs/{orgID}/invites/{inviteID}/resend. Issues a
// new link (the old one stops working) and restarts the 7 days.
func (s *Service) resendInvite(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	m := mustMembership(r)
	id, err := httpx.URLParamUUID(r, "inviteID")
	if err != nil {
		return err
	}
	token, hash := newToken(prefixInvite)
	var (
		inv     Invite
		orgName string
	)
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if orgName, err = lockOrg(ctx, tx, m.OrgID); err != nil {
			return err
		}
		var (
			status   string
			lastSent time.Time
		)
		err := tx.QueryRow(ctx, `SELECT status, last_sent_at FROM org_invites WHERE id = $1 AND org_id = $2 FOR UPDATE`,
			id, m.OrgID).Scan(&status, &lastSent)
		if db.IsNoRows(err) {
			return httpx.NotFound("Invite not found.")
		}
		if err != nil {
			return err
		}
		if status != "pending" && status != "expired" {
			return httpx.Conflict("That invite was already " + status + ".")
		}
		if time.Since(lastSent) < resendCooldown {
			return httpx.NewError(http.StatusTooManyRequests, "rate_limited", "That invite was just sent. Wait a minute before resending.")
		}
		if _, err := tx.Exec(ctx, `
			UPDATE org_invites SET token_hash = $2, status = 'pending', expires_at = now() + make_interval(secs => $3),
			  sent_count = sent_count + 1, last_sent_at = now(), updated_at = now()
			WHERE id = $1`, id, hash, inviteTTL.Seconds()); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, inviteSelect+` WHERE i.id = $1`, id)
		if err != nil {
			return err
		}
		inv, err = pgx.CollectExactlyOneRow(rows, pgx.RowToStructByPos[Invite])
		return err
	})
	if db.IsUniqueViolation(err, "org_invites_one_pending_idx") {
		return httpx.Conflict("That email already has a newer pending invite.")
	}
	if err != nil {
		return err
	}
	s.deliverInvite(context.WithoutCancel(ctx), inv, token, orgName, me.DisplayName)
	httpx.OK(w, inv)
	return nil
}

// deliverInvite emails the link and notifies an existing account with that
// email. Best-effort: failures are logged and the invite stays valid.
func (s *Service) deliverInvite(ctx context.Context, inv Invite, token, orgName, inviter string) {
	url := s.inviteURL(token)
	who := inviter
	if who == "" {
		who = "Someone"
	}
	role := "a member"
	if inv.Role == RoleAdmin {
		role = "an admin"
	}
	expires := inv.ExpiresAt.UTC().Format("January 2, 2006")
	subject := fmt.Sprintf("%s invited you to %s on Scaffold App", who, orgName)
	text := fmt.Sprintf("%s invited you to join %s on Scaffold App as %s.\n\nAccept the invite: %s\n\nThe link works until %s. Sign in with %s to accept.\n",
		who, orgName, role, url, expires, inv.Email)
	body := fmt.Sprintf(`<p>%s invited you to join <strong>%s</strong> on Scaffold App as %s.</p>`+
		`<p><a href="%s" style="display:inline-block;background:#1a1a1a;color:#ffffff;padding:10px 18px;border-radius:8px;text-decoration:none">Accept invite</a></p>`+
		`<p style="color:#6b6b6b;font-size:13px">The link works until %s. Sign in with %s to accept.</p>`,
		html.EscapeString(who), html.EscapeString(orgName), role, html.EscapeString(url), expires, html.EscapeString(inv.Email))
	if err := s.d.Mail.Send(ctx, mailer.Message{To: inv.Email, Subject: subject, Text: text, HTML: mailer.Layout("Scaffold App", body)}); err != nil {
		slog.WarnContext(ctx, "orgs: invite email failed", "invite_id", inv.ID, "err", err)
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT id FROM users WHERE lower(email) = $1 AND status IN ('active', 'suspended')`, inv.Email)
	if err != nil {
		slog.WarnContext(ctx, "orgs: find invitee", "invite_id", inv.ID, "err", err)
		return
	}
	users, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		slog.WarnContext(ctx, "orgs: find invitee", "invite_id", inv.ID, "err", err)
		return
	}
	for _, u := range users {
		s.send(ctx, notify.Notification{UserID: u, Kind: KindInvite, Title: who + " invited you to " + orgName,
			Body: "Join as " + role + ".", Link: "/settings/orgs",
			Data: map[string]any{"invite_id": inv.ID, "org_id": inv.OrgID}})
	}
}

// revokeInvite: DELETE /v1/orgs/{orgID}/invites/{inviteID} (admin+).
func (s *Service) revokeInvite(w http.ResponseWriter, r *http.Request) error {
	m := mustMembership(r)
	id, err := httpx.URLParamUUID(r, "inviteID")
	if err != nil {
		return err
	}
	tag, err := s.d.Pool.Exec(r.Context(), `UPDATE org_invites SET status = 'revoked', updated_at = now()
		WHERE id = $1 AND org_id = $2 AND status = 'pending'`, id, m.OrgID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		var exists bool
		if err := s.d.Pool.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM org_invites WHERE id = $1 AND org_id = $2)`,
			id, m.OrgID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return httpx.NotFound("Invite not found.")
		}
		return httpx.Conflict("That invite is no longer pending.")
	}
	httpx.NoContent(w)
	return nil
}

// ---- invite links ------------------------------------------------------------

// InviteLink is a shareable link. URL and Token appear only in the response
// that creates it; afterwards only the hash is kept.
type InviteLink struct {
	ID            uuid.UUID  `json:"id"`
	OrgID         uuid.UUID  `json:"org_id"`
	Role          string     `json:"role"`
	MaxUses       *int       `json:"max_uses"`
	Uses          int        `json:"uses"`
	ExpiresAt     *time.Time `json:"expires_at"`
	RevokedAt     *time.Time `json:"revoked_at"`
	CreatedByName *string    `json:"created_by_name"`
	CreatedAt     time.Time  `json:"created_at"`
	Active        bool       `json:"active"`
	URL           *string    `json:"url,omitempty"`
	Token         *string    `json:"token,omitempty"`
}

const linkSelect = `
	SELECT l.id, l.org_id, l.role, l.max_uses, l.uses, l.expires_at, l.revoked_at, NULLIF(u.display_name, ''), l.created_at,
	       l.revoked_at IS NULL AND (l.expires_at IS NULL OR l.expires_at > now()) AND (l.max_uses IS NULL OR l.uses < l.max_uses),
	       NULL::text, NULL::text
	FROM org_invite_links l LEFT JOIN users u ON u.id = l.created_by`

// listLinks: GET /v1/orgs/{orgID}/links (admin+), newest first.
func (s *Service) listLinks(w http.ResponseWriter, r *http.Request) error {
	m := mustMembership(r)
	p := httpx.ParsePage(r)
	var c cursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	rows, err := s.d.Pool.Query(r.Context(), linkSelect+`
		WHERE l.org_id = $1 AND ($2::timestamptz IS NULL OR (l.created_at, l.id) < ($2, $3))
		ORDER BY l.created_at DESC, l.id DESC LIMIT $4`, m.OrgID, nullTime(c.T), c.ID, p.Limit+1)
	if err != nil {
		return err
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[InviteLink])
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(list, p.Limit, func(l InviteLink) any { return cursor{l.CreatedAt, l.ID} }))
	return nil
}

type createLinkReq struct {
	MaxUses       *int `json:"max_uses"`        // nil = unlimited
	ExpiresInDays *int `json:"expires_in_days"` // nil = never
}

// createLink: POST /v1/orgs/{orgID}/links (admin+). Links always add
// members; promote people afterwards.
func (s *Service) createLink(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	m := mustMembership(r)
	var req createLinkReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.MaxUses != nil && (*req.MaxUses < 1 || *req.MaxUses > 10000) {
		return httpx.BadRequest("max_uses must be 1 to 10000, or left out for no limit.")
	}
	if req.ExpiresInDays != nil && (*req.ExpiresInDays < 1 || *req.ExpiresInDays > 365) {
		return httpx.BadRequest("expires_in_days must be 1 to 365, or left out for no expiry.")
	}
	token, hash := newToken(prefixLink)
	var link InviteLink
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if _, err := lockOrg(ctx, tx, m.OrgID); err != nil {
			return err
		}
		var active int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM org_invite_links WHERE org_id = $1 AND revoked_at IS NULL
			AND (expires_at IS NULL OR expires_at > now()) AND (max_uses IS NULL OR uses < max_uses)`, m.OrgID).Scan(&active); err != nil {
			return err
		}
		if active >= maxActiveLinks {
			return httpx.Conflict(fmt.Sprintf("%d links are active. Revoke one before making another.", maxActiveLinks))
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO org_invite_links (org_id, role, token_hash, max_uses, expires_at, created_by)
			VALUES ($1, 'member', $2, $3, CASE WHEN $4::int IS NULL THEN NULL ELSE now() + make_interval(days => $4) END, $5)
			RETURNING id`, m.OrgID, hash, req.MaxUses, req.ExpiresInDays, me).Scan(&id); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, linkSelect+` WHERE l.id = $1`, id)
		if err != nil {
			return err
		}
		link, err = pgx.CollectExactlyOneRow(rows, pgx.RowToStructByPos[InviteLink])
		return err
	})
	if err != nil {
		return err
	}
	url := s.inviteURL(token)
	link.URL, link.Token = &url, &token
	httpx.JSON(w, http.StatusCreated, link)
	return nil
}

// revokeLink: DELETE /v1/orgs/{orgID}/links/{linkID} (admin+). Idempotent.
func (s *Service) revokeLink(w http.ResponseWriter, r *http.Request) error {
	m := mustMembership(r)
	id, err := httpx.URLParamUUID(r, "linkID")
	if err != nil {
		return err
	}
	var found bool
	err = s.d.Pool.QueryRow(r.Context(), `
		WITH upd AS (UPDATE org_invite_links SET revoked_at = COALESCE(revoked_at, now()), updated_at = now()
		             WHERE id = $1 AND org_id = $2 RETURNING 1)
		SELECT EXISTS (SELECT 1 FROM upd)`, id, m.OrgID).Scan(&found)
	if err != nil {
		return err
	}
	if !found {
		return httpx.NotFound("Link not found.")
	}
	httpx.NoContent(w)
	return nil
}

// ---- accepting -----------------------------------------------------------------

// InvitePreview is what the accept page shows before (and after) sign-in.
type InvitePreview struct {
	Kind          string     `json:"kind"`   // email | link
	Status        string     `json:"status"` // valid | expired | revoked | used | accepted | declined
	Org           OrgRef     `json:"org"`
	Role          string     `json:"role"`
	EmailHint     *string    `json:"email_hint"` // masked address an email invite was sent to
	InvitedByName *string    `json:"invited_by_name"`
	ExpiresAt     *time.Time `json:"expires_at"`
}

// lookedUp is an invite or link found by token.
type lookedUp struct {
	preview InvitePreview
	id      uuid.UUID
	email   string
	maxUses *int
	uses    int
}

func (s *Service) lookupToken(ctx context.Context, q db.Querier, token string, lock bool) (*lookedUp, error) {
	var lu lookedUp
	var avatar *string
	forUpdate := ""
	if lock {
		forUpdate = " FOR UPDATE OF x"
	}
	var err error
	switch {
	case strings.HasPrefix(token, prefixInvite):
		lu.preview.Kind = "email"
		var status string
		var expires time.Time
		err = q.QueryRow(ctx, `
			SELECT x.id, x.email, x.role, x.status, x.expires_at, NULLIF(u.display_name, ''), o.id, o.name, o.slug, o.avatar_key
			FROM org_invites x JOIN orgs o ON o.id = x.org_id LEFT JOIN users u ON u.id = x.invited_by
			WHERE x.token_hash = $1`+forUpdate, hashToken(token)).
			Scan(&lu.id, &lu.email, &lu.preview.Role, &status, &expires, &lu.preview.InvitedByName,
				&lu.preview.Org.ID, &lu.preview.Org.Name, &lu.preview.Org.Slug, &avatar)
		if err == nil {
			hint := maskEmail(lu.email)
			lu.preview.EmailHint, lu.preview.ExpiresAt = &hint, &expires
			switch {
			case status == "pending" && time.Now().After(expires):
				lu.preview.Status = "expired"
			case status == "pending":
				lu.preview.Status = "valid"
			default:
				lu.preview.Status = status
			}
		}
	case strings.HasPrefix(token, prefixLink):
		lu.preview.Kind = "link"
		var revoked *time.Time
		err = q.QueryRow(ctx, `
			SELECT x.id, x.role, x.max_uses, x.uses, x.expires_at, x.revoked_at, NULLIF(u.display_name, ''), o.id, o.name, o.slug, o.avatar_key
			FROM org_invite_links x JOIN orgs o ON o.id = x.org_id LEFT JOIN users u ON u.id = x.created_by
			WHERE x.token_hash = $1`+forUpdate, hashToken(token)).
			Scan(&lu.id, &lu.preview.Role, &lu.maxUses, &lu.uses, &lu.preview.ExpiresAt, &revoked, &lu.preview.InvitedByName,
				&lu.preview.Org.ID, &lu.preview.Org.Name, &lu.preview.Org.Slug, &avatar)
		if err == nil {
			switch {
			case revoked != nil:
				lu.preview.Status = "revoked"
			case lu.preview.ExpiresAt != nil && time.Now().After(*lu.preview.ExpiresAt):
				lu.preview.Status = "expired"
			case lu.maxUses != nil && lu.uses >= *lu.maxUses:
				lu.preview.Status = "used"
			default:
				lu.preview.Status = "valid"
			}
		}
	default:
		return nil, errInviteNotFound
	}
	if db.IsNoRows(err) {
		return nil, errInviteNotFound
	}
	if err != nil {
		return nil, err
	}
	lu.preview.Org.AvatarURL = s.avatarURL(avatar)
	return &lu, nil
}

var errInviteNotFound = httpx.NotFound("This invite link isn't valid. Ask for a new one.")

// previewInvite: GET /v1/orgs/invites/{token} (public, 30/min per IP).
func (s *Service) previewInvite(w http.ResponseWriter, r *http.Request) error {
	lu, err := s.lookupToken(r.Context(), s.d.Pool, chi.URLParam(r, "token"), false)
	if err != nil {
		return err
	}
	httpx.OK(w, lu.preview)
	return nil
}

// acceptToken: POST /v1/orgs/invites/{token}/accept. An email invite needs
// the signed-in account's verified email to match; a link needs a free use.
// Accepting while already a member succeeds without using anything up.
func (s *Service) acceptToken(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	token := chi.URLParam(r, "token")
	var (
		orgID  uuid.UUID
		joined bool
	)
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		lu, err := s.lookupToken(ctx, tx, token, true)
		if err != nil {
			return err
		}
		orgID = lu.preview.Org.ID
		if lu.preview.Kind == "email" && !strings.EqualFold(lu.email, me.Email) {
			return httpx.Forbidden("This invite was sent to " + *lu.preview.EmailHint + ". Sign in with that email to accept.")
		}
		role, err := RoleOf(ctx, tx, orgID, me.UserID)
		if err != nil {
			return err
		}
		if role != "" {
			if lu.preview.Kind == "email" && lu.preview.Status == "valid" {
				_, err = tx.Exec(ctx, `UPDATE org_invites SET status = 'accepted', accepted_by = $2, accepted_at = now(), updated_at = now()
					WHERE id = $1`, lu.id, me.UserID)
			}
			return err
		}
		if lu.preview.Status != "valid" {
			return inviteUnavailable(lu.preview.Status)
		}
		if lu.preview.Kind == "email" {
			if _, err := tx.Exec(ctx, `UPDATE org_invites SET status = 'accepted', accepted_by = $2, accepted_at = now(), updated_at = now()
				WHERE id = $1`, lu.id, me.UserID); err != nil {
				return err
			}
		} else if _, err := tx.Exec(ctx, `UPDATE org_invite_links SET uses = uses + 1, updated_at = now() WHERE id = $1`, lu.id); err != nil {
			return err
		}
		if err := join(ctx, tx, orgID, me.UserID, lu.preview.Role); err != nil {
			return err
		}
		joined = true
		return nil
	})
	if err != nil {
		return err
	}
	return s.respondJoined(w, r, orgID, me.UserID, joined)
}

func inviteUnavailable(status string) error {
	switch status {
	case "expired":
		return httpx.NewError(http.StatusGone, "invite_expired", "This invite expired. Ask for a new one.")
	case "used":
		return httpx.NewError(http.StatusGone, "invite_used", "This link has reached its limit. Ask for a new one.")
	default:
		return httpx.NewError(http.StatusGone, "invite_unavailable", "This invite is no longer available. Ask for a new one.")
	}
}

func join(ctx context.Context, tx pgx.Tx, orgID, userID uuid.UUID, role string) error {
	_, err := tx.Exec(ctx, `INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, $3)
		ON CONFLICT (org_id, user_id) DO NOTHING`, orgID, userID, role)
	return err
}

func (s *Service) respondJoined(w http.ResponseWriter, r *http.Request, orgID, userID uuid.UUID, joined bool) error {
	o, err := s.loadOrg(r.Context(), orgID, userID)
	if err != nil {
		return err
	}
	if joined {
		s.membershipChanged(context.WithoutCancel(r.Context()), orgID, userID, EventMemberJoined, map[string]any{"role": o.Role})
	}
	httpx.OK(w, o)
	return nil
}

// MyInvite is a pending invite addressed to the caller's email.
type MyInvite struct {
	ID            uuid.UUID `json:"id"`
	Org           OrgRef    `json:"org"`
	Role          string    `json:"role"`
	InvitedByName *string   `json:"invited_by_name"`
	ExpiresAt     time.Time `json:"expires_at"`
	CreatedAt     time.Time `json:"created_at"`
}

// listMyInvites: GET /v1/orgs/my-invites, pending invites to the caller's
// verified email, newest first.
func (s *Service) listMyInvites(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	rows, err := s.d.Pool.Query(r.Context(), `
		SELECT i.id, o.id, o.name, o.slug, o.avatar_key, i.role, NULLIF(u.display_name, ''), i.expires_at, i.created_at
		FROM org_invites i JOIN orgs o ON o.id = i.org_id LEFT JOIN users u ON u.id = i.invited_by
		WHERE i.email = lower($1) AND i.status = 'pending' AND i.expires_at > now()
		  AND NOT EXISTS (SELECT 1 FROM org_members m WHERE m.org_id = i.org_id AND m.user_id = $2)
		ORDER BY i.created_at DESC, i.id DESC LIMIT 100`, me.Email, me.UserID)
	if err != nil {
		return err
	}
	defer rows.Close()
	list := []MyInvite{}
	for rows.Next() {
		var (
			v      MyInvite
			avatar *string
		)
		if err := rows.Scan(&v.ID, &v.Org.ID, &v.Org.Name, &v.Org.Slug, &avatar, &v.Role, &v.InvitedByName, &v.ExpiresAt, &v.CreatedAt); err != nil {
			return err
		}
		v.Org.AvatarURL = s.avatarURL(avatar)
		list = append(list, v)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.OK(w, httpx.Page[MyInvite]{Items: list})
	return nil
}

// acceptMyInvite: POST /v1/orgs/my-invites/{id}/accept.
func (s *Service) acceptMyInvite(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var (
		orgID  uuid.UUID
		joined bool
	)
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var (
			role, status string
			expires      time.Time
		)
		err := tx.QueryRow(ctx, `SELECT org_id, role, status, expires_at FROM org_invites
			WHERE id = $1 AND email = lower($2) FOR UPDATE`, id, me.Email).Scan(&orgID, &role, &status, &expires)
		if db.IsNoRows(err) {
			return httpx.NotFound("Invite not found.")
		}
		if err != nil {
			return err
		}
		if status == "pending" && time.Now().After(expires) {
			status = "expired"
		}
		if status != "pending" {
			return inviteUnavailable(status)
		}
		current, err := RoleOf(ctx, tx, orgID, me.UserID)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE org_invites SET status = 'accepted', accepted_by = $2, accepted_at = now(), updated_at = now()
			WHERE id = $1`, id, me.UserID); err != nil {
			return err
		}
		if current != "" {
			return nil
		}
		joined = true
		return join(ctx, tx, orgID, me.UserID, role)
	})
	if err != nil {
		return err
	}
	return s.respondJoined(w, r, orgID, me.UserID, joined)
}

// declineMyInvite: POST /v1/orgs/my-invites/{id}/decline.
func (s *Service) declineMyInvite(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	tag, err := s.d.Pool.Exec(r.Context(), `UPDATE org_invites SET status = 'declined', updated_at = now()
		WHERE id = $1 AND email = lower($2) AND status = 'pending'`, id, me.Email)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.NotFound("Invite not found.")
	}
	httpx.NoContent(w)
	return nil
}

// maskEmail shows enough of an address to recognise it: "s***@example.com".
func maskEmail(email string) string {
	local, domain, ok := strings.Cut(email, "@")
	if !ok || local == "" {
		return "***"
	}
	return string([]rune(local)[:1]) + "***@" + domain
}
