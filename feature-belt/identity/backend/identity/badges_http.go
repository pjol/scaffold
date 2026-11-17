package identity

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/audit"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// ---- the person's badges -----------------------------------------------------------

// PaidStatus is the paid badge as its (would-be) holder sees it.
type PaidStatus struct {
	Entitlement  string        `json:"entitlement"` // the payments entitlement that grants it (IDENTITY_PAID_ENTITLEMENT)
	Active       bool          `json:"active"`
	ExpiresAt    *time.Time    `json:"expires_at"` // renews or ends then
	Held         bool          `json:"held"`       // staff removed it: billing can't bring it back
	Eligible     bool          `json:"eligible"`
	Requirements []Requirement `json:"requirements"`
}

// BadgesStatus is GET /v1/identity/badges.
type BadgesStatus struct {
	Badges       []OwnBadge    `json:"badges"` // active, highest first
	Paid         PaidStatus    `json:"paid"`
	Applications []Application `json:"applications"` // newest first (last 50)
	Domains      []Domain      `json:"domains"`
	Categories   []Category    `json:"categories"`
	Kinds        []KindInfo    `json:"kinds"` // highest first
}

func (s *Service) badgesStatus(ctx context.Context, q db.Querier, userID uuid.UUID) (BadgesStatus, error) {
	var st BadgesStatus
	var err error
	if st.Badges, err = activeBadges(ctx, q, userID); err != nil {
		return st, err
	}
	if st.Applications, err = applicationsOf(ctx, q, userID); err != nil {
		return st, err
	}
	if st.Domains, err = domainsOf(ctx, q, userID); err != nil {
		return st, err
	}
	st.Paid.Entitlement = s.badges.paidKey
	if st.Paid.Eligible, st.Paid.Requirements, err = paidEligibility(ctx, q, userID, true); err != nil {
		return st, err
	}
	var source string
	var expires *time.Time
	var active bool
	err = q.QueryRow(ctx, `SELECT source, expires_at, `+activeSQL+` FROM identity_badges WHERE user_id = $1 AND kind = 'paid'`, userID).
		Scan(&source, &expires, &active)
	switch {
	case err == nil:
		st.Paid.Held = source == SourceStaffRevoked
		st.Paid.Active = active
		if active {
			st.Paid.ExpiresAt = expires
		}
	case !db.IsNoRows(err):
		return st, err
	}
	st.Categories, st.Kinds = s.badges.categories, s.badges.kinds()
	return st, nil
}

func (s *Service) getBadges(w http.ResponseWriter, r *http.Request) error {
	st, err := s.badgesStatus(r.Context(), s.d.Pool, auth.MustFromContext(r.Context()).UserID)
	if err != nil {
		return err
	}
	httpx.OK(w, st)
	return nil
}

func (s *Service) postApplication(w http.ResponseWriter, r *http.Request) error {
	var in ApplicationInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	a, err := s.apply(r.Context(), auth.MustFromContext(r.Context()).UserID, in)
	if err != nil {
		return err
	}
	httpx.OK(w, a)
	return nil
}

func (s *Service) postResubmit(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var in ApplicationInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	a, err := s.resubmit(r.Context(), auth.MustFromContext(r.Context()).UserID, id, in)
	if err != nil {
		return err
	}
	httpx.OK(w, a)
	return nil
}

func (s *Service) postWithdraw(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	a, err := s.withdraw(r.Context(), auth.MustFromContext(r.Context()).UserID, id)
	if err != nil {
		return err
	}
	httpx.OK(w, a)
	return nil
}

func (s *Service) postDomain(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Domain string `json:"domain"`
	}
	if err := httpx.Decode(r, &body); err != nil {
		return err
	}
	d, err := s.addDomain(r.Context(), auth.MustFromContext(r.Context()).UserID, body.Domain)
	if err != nil {
		return err
	}
	httpx.OK(w, d)
	return nil
}

func (s *Service) postDomainCheck(w http.ResponseWriter, r *http.Request) error {
	d, err := s.checkDomain(r.Context(), auth.MustFromContext(r.Context()).UserID, chi.URLParam(r, "domain"))
	if err != nil {
		return err
	}
	httpx.OK(w, d)
	return nil
}

func (s *Service) deleteDomain(w http.ResponseWriter, r *http.Request) error {
	if err := s.removeDomain(r.Context(), auth.MustFromContext(r.Context()).UserID, chi.URLParam(r, "domain")); err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

// ---- staff: applications -----------------------------------------------------------

// BadgeUser identifies a person to staff.
type BadgeUser struct {
	ID          uuid.UUID `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
}

// AdminApplication is an application with its applicant.
type AdminApplication struct {
	Application
	User       BadgeUser  `json:"user"`
	ReviewedBy *uuid.UUID `json:"reviewed_by"`
}

// AdminApplicationDetail adds what a reviewer weighs: ID verification and
// the badges the person already holds.
type AdminApplicationDetail struct {
	AdminApplication
	IdentityVerified bool       `json:"identity_verified"`
	Badges           []OwnBadge `json:"badges"`
	CategoryKind     string     `json:"category_kind"` // the badge an approval grants by default
}

const adminAppCols = `a.id, a.user_id, a.category, a.display_label, a.status, a.evidence, a.note, a.reviewer_note, a.submitted_at,
	a.decided_at, a.created_at, u.email, u.display_name, a.reviewed_by`

func scanAdminApp(row pgx.Row) (AdminApplication, error) {
	var a AdminApplication
	var raw []byte
	err := row.Scan(&a.ID, &a.userID, &a.Category, &a.DisplayLabel, &a.Status, &raw, &a.Note, &a.ReviewerNote,
		&a.SubmittedAt, &a.DecidedAt, &a.CreatedAt, &a.User.Email, &a.User.DisplayName, &a.ReviewedBy)
	if err != nil {
		return a, err
	}
	a.User.ID = a.userID
	a.Evidence = []Evidence{}
	return a, json.Unmarshal(raw, &a.Evidence)
}

type appCursor struct {
	T  time.Time `json:"t"`
	ID uuid.UUID `json:"id"`
}

// adminApplications is GET /v1/admin/identity/badge-applications?status=.
// The default is the review queue (submitted, oldest first); decided
// statuses list newest first.
func (s *Service) adminApplications(w http.ResponseWriter, r *http.Request) error {
	status := r.URL.Query().Get("status")
	switch status {
	case "":
		status = AppSubmitted
	case AppSubmitted, AppNeedsInfo, AppApproved, AppRejected, AppWithdrawn:
	default:
		return httpx.BadRequest("status must be submitted, needs_info, approved, rejected or withdrawn.")
	}
	p := httpx.ParsePage(r)
	var c appCursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	order, cmp := "DESC", "<"
	if status == AppSubmitted || status == AppNeedsInfo {
		order, cmp = "ASC", ">"
	}
	var after *time.Time
	if !c.T.IsZero() {
		after = &c.T
	}
	rows, err := s.d.Pool.Query(r.Context(), `SELECT `+adminAppCols+`
		FROM identity_badge_applications a JOIN users u ON u.id = a.user_id
		WHERE a.status = $1 AND ($2::timestamptz IS NULL OR (a.submitted_at, a.id) `+cmp+` ($2, $3))
		ORDER BY a.submitted_at `+order+`, a.id `+order+` LIMIT $4`, status, after, c.ID, p.Limit+1)
	if err != nil {
		return err
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (AdminApplication, error) { return scanAdminApp(row) })
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(list, p.Limit, func(a AdminApplication) any { return appCursor{a.SubmittedAt, a.ID} }))
	return nil
}

func (s *Service) loadAdminApplication(ctx context.Context, q db.Querier, id uuid.UUID) (AdminApplicationDetail, error) {
	var out AdminApplicationDetail
	a, err := scanAdminApp(q.QueryRow(ctx, `SELECT `+adminAppCols+`
		FROM identity_badge_applications a JOIN users u ON u.id = a.user_id WHERE a.id = $1`, id))
	if db.IsNoRows(err) {
		return out, errNoApplication
	}
	if err != nil {
		return out, err
	}
	out.AdminApplication = a
	if out.IdentityVerified, err = IsVerified(ctx, q, a.userID); err != nil {
		return out, err
	}
	if out.Badges, err = activeBadges(ctx, q, a.userID); err != nil {
		return out, err
	}
	if c, ok := s.badges.category(a.Category); ok {
		out.CategoryKind = c.Kind
	}
	return out, nil
}

func (s *Service) adminApplication(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	out, err := s.loadAdminApplication(r.Context(), s.d.Pool, id)
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

// Decision is POST /v1/admin/identity/badge-applications/{id}/decision.
type Decision struct {
	Decision      string `json:"decision"`        // approve | needs_info | reject
	Kind          string `json:"kind"`            // approve: org | notable (default: the category's)
	Label         string `json:"label"`           // approve: what the badge says (default: the application's name)
	ExpiresInDays int    `json:"expires_in_days"` // approve: 1–730 (default IDENTITY_BADGE_TTL_DAYS)
	Note          string `json:"note"`            // the applicant sees it; required to ask for more or reject
}

// decideApplication applies a staff decision with an audit row; staff can't decide
// their own application.
func (s *Service) decideApplication(ctx context.Context, staff uuid.UUID, id uuid.UUID, d Decision) (AdminApplicationDetail, error) {
	d.Note = strings.TrimSpace(d.Note)
	if utf8.RuneCountInString(d.Note) > 1000 {
		return AdminApplicationDetail{}, httpx.BadRequest("The note is at most 1,000 characters.")
	}
	var userID uuid.UUID
	var status string
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		a, err := scanApp(tx.QueryRow(ctx, `SELECT `+appCols+` FROM identity_badge_applications WHERE id = $1 FOR UPDATE`, id))
		if db.IsNoRows(err) {
			return errNoApplication
		}
		if err != nil {
			return err
		}
		if a.userID == staff {
			return httpx.Forbidden("Another staff member has to review your own application.")
		}
		if a.Status != AppSubmitted && a.Status != AppNeedsInfo {
			return errNotOpen
		}
		userID = a.userID
		data := map[string]any{"user_id": a.userID, "category": a.Category, "from": a.Status}
		switch d.Decision {
		case "approve":
			status = AppApproved
			kind := d.Kind
			if kind == "" {
				if c, ok := s.badges.category(a.Category); ok {
					kind = c.Kind
				}
			}
			if kind != BadgeOrg && kind != BadgeNotable {
				return httpx.BadRequest("Reviewed badges are org or notable.")
			}
			label := strings.TrimSpace(d.Label)
			if label == "" {
				label = a.DisplayLabel
			}
			if utf8.RuneCountInString(label) > 60 {
				return httpx.BadRequest("The badge label is at most 60 characters.")
			}
			days := d.ExpiresInDays
			if days == 0 {
				days = s.badges.ttlDays
			}
			if days < 1 || days > maxBadgeTTLDays {
				return httpx.BadRequest("Badges expire in 1 to 730 days.")
			}
			exp := time.Now().UTC().AddDate(0, 0, days)
			cat := a.Category
			if _, err := grant(ctx, tx, grantInput{userID: a.userID, kind: kind, label: label, category: &cat,
				source: SourceReview, application: &a.ID, by: &staff, expires: &exp}); err != nil {
				return err
			}
			data["kind"], data["label"], data["expires_at"] = kind, label, exp
		case "needs_info", "reject":
			status = map[string]string{"needs_info": AppNeedsInfo, "reject": AppRejected}[d.Decision]
			if utf8.RuneCountInString(d.Note) < 3 {
				return httpx.BadRequest("Say why in the note (3 characters or more). The applicant sees it.")
			}
		default:
			return httpx.BadRequest("decision is approve, needs_info or reject.")
		}
		if d.Note != "" {
			data["note"] = d.Note
		}
		if _, err := tx.Exec(ctx, `UPDATE identity_badge_applications SET status = $2, reviewer_note = $3, reviewed_by = $4,
			decided_at = CASE WHEN $2 = 'needs_info' THEN NULL ELSE now() END, updated_at = now() WHERE id = $1`,
			id, status, d.Note, staff); err != nil {
			return err
		}
		return audit.Log(ctx, tx, staff, "identity.badge_application."+d.Decision, "identity_badge_application", id.String(), data)
	})
	if err != nil {
		return AdminApplicationDetail{}, err
	}
	title := map[string]string{
		AppApproved:  "Your badge application was approved",
		AppNeedsInfo: "Your badge application needs more information",
		AppRejected:  "Your badge application wasn't approved",
	}[status]
	s.notifyBadge(ctx, userID, title, d.Note, "", status)
	s.badgesChanged(ctx, userID)
	return s.loadAdminApplication(ctx, s.d.Pool, id)
}

func (s *Service) adminDecide(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var d Decision
	if err := httpx.Decode(r, &d); err != nil {
		return err
	}
	out, err := s.decideApplication(context.WithoutCancel(r.Context()), auth.MustFromContext(r.Context()).UserID, id, d)
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

// ---- staff: grants and removals ----------------------------------------------------

// AdminBadge is a badge row as staff see it, ended ones included.
type AdminBadge struct {
	OwnBadge
	User          BadgeUser  `json:"user"`
	Active        bool       `json:"active"`
	Held          bool       `json:"held"` // a paid badge staff removed
	GrantedBy     *uuid.UUID `json:"granted_by"`
	RevokedAt     *time.Time `json:"revoked_at"`
	RevokedReason string     `json:"revoked_reason"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

const adminBadgeCols = `b.kind, b.label, b.granted_at, b.category, b.source, b.expires_at, b.user_id, u.email, u.display_name,
	b.revoked_at IS NULL AND (b.expires_at IS NULL OR b.expires_at > now()), b.granted_by, b.revoked_at, b.revoked_reason, b.updated_at`

func scanAdminBadge(row pgx.Row) (AdminBadge, error) {
	var b AdminBadge
	var kind, label string
	var since time.Time
	err := row.Scan(&kind, &label, &since, &b.Category, &b.Source, &b.ExpiresAt, &b.User.ID, &b.User.Email, &b.User.DisplayName,
		&b.Active, &b.GrantedBy, &b.RevokedAt, &b.RevokedReason, &b.UpdatedAt)
	b.HeldBadge = heldBadge(kind, label, since)
	b.Held = b.Source == SourceStaffRevoked
	if b.Held {
		b.Source = SourceBilling
	}
	return b, err
}

type badgeCursor struct {
	T    time.Time `json:"t"`
	User uuid.UUID `json:"u"`
	Kind string    `json:"k"`
}

// adminBadges is GET /v1/admin/identity/badges?kind=&state=active|ended|held&user_id=,
// most recently changed first.
func (s *Service) adminBadges(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	kind, state := q.Get("kind"), q.Get("state")
	if kind != "" && !isKind(kind) {
		return httpx.BadRequest("kind must be official, org, notable or paid.")
	}
	switch state {
	case "":
		state = "active"
	case "active", "ended", "held":
	default:
		return httpx.BadRequest("state must be active, ended or held.")
	}
	var user *uuid.UUID
	if raw := q.Get("user_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return httpx.BadRequest("user_id must be a uuid.")
		}
		user = &id
	}
	p := httpx.ParsePage(r)
	var c badgeCursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	var after *time.Time
	if !c.T.IsZero() {
		after = &c.T
	}
	where := map[string]string{
		"active": `b.revoked_at IS NULL AND (b.expires_at IS NULL OR b.expires_at > now())`,
		"ended":  `NOT (b.revoked_at IS NULL AND (b.expires_at IS NULL OR b.expires_at > now())) AND b.source <> 'staff_revoked'`,
		"held":   `b.source = 'staff_revoked'`,
	}[state]
	rows, err := s.d.Pool.Query(r.Context(), `SELECT `+adminBadgeCols+`
		FROM identity_badges b JOIN users u ON u.id = b.user_id
		WHERE `+where+` AND ($1 = '' OR b.kind = $1) AND ($2::uuid IS NULL OR b.user_id = $2)
		  AND ($3::timestamptz IS NULL OR (b.updated_at, b.user_id, b.kind) < ($3, $4, $5))
		ORDER BY b.updated_at DESC, b.user_id DESC, b.kind DESC LIMIT $6`, kind, user, after, c.User, c.Kind, p.Limit+1)
	if err != nil {
		return err
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (AdminBadge, error) { return scanAdminBadge(row) })
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(list, p.Limit, func(b AdminBadge) any { return badgeCursor{b.UpdatedAt, b.User.ID, b.Kind} }))
	return nil
}

// grantReq is POST /v1/admin/identity/badges. kind paid lifts a staff
// removal instead (paid badges come from a plan).
type grantReq struct {
	UserID        *uuid.UUID `json:"user_id"`
	Email         string     `json:"email"`
	Kind          string     `json:"kind"`
	Label         string     `json:"label"`
	ExpiresInDays int        `json:"expires_in_days"` // 0 = no expiry
}

func (s *Service) adminGrant(w http.ResponseWriter, r *http.Request) error {
	ctx := context.WithoutCancel(r.Context())
	staff := auth.MustFromContext(r.Context()).UserID
	var req grantReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if !isKind(req.Kind) {
		return httpx.BadRequest("kind must be official, org, notable or paid.")
	}
	label := strings.TrimSpace(req.Label)
	if utf8.RuneCountInString(label) > 60 {
		return httpx.BadRequest("The badge label is at most 60 characters.")
	}
	if req.ExpiresInDays < 0 || req.ExpiresInDays > maxBadgeTTLDays {
		return httpx.BadRequest("Badges expire in 1 to 730 days, or never (0).")
	}
	var userID uuid.UUID
	var paid PaidBadgeChange
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var status string
		err := tx.QueryRow(ctx, `SELECT id, status FROM users WHERE (id = $1 OR ($1::uuid IS NULL AND lower(email) = lower($2)))
			AND status <> 'deleted' ORDER BY created_at LIMIT 1`, req.UserID, strings.TrimSpace(req.Email)).Scan(&userID, &status)
		if db.IsNoRows(err) {
			return httpx.NotFound("No account with that id or email.")
		}
		if err != nil {
			return err
		}
		if userID == staff {
			return httpx.Forbidden("Another admin has to grant you a badge.")
		}
		if req.Kind == BadgePaid {
			lifted, ch, err := restorePaid(ctx, tx, userID)
			if err != nil {
				return err
			}
			if !lifted {
				return httpx.Conflict("Paid badges come from a plan. This person has no staff removal to lift.")
			}
			paid = ch
			return audit.Log(ctx, tx, staff, "identity.badge.restore", "user", userID.String(), map[string]any{"kind": BadgePaid})
		}
		var exp *time.Time
		if req.ExpiresInDays > 0 {
			t := time.Now().UTC().AddDate(0, 0, req.ExpiresInDays)
			exp = &t
		}
		if _, err := grant(ctx, tx, grantInput{userID: userID, kind: req.Kind, label: label, source: SourceAdmin,
			by: &staff, expires: exp}); err != nil {
			return err
		}
		return audit.Log(ctx, tx, staff, "identity.badge.grant", "user", userID.String(),
			map[string]any{"kind": req.Kind, "label": label, "expires_at": exp})
	})
	if err != nil {
		return err
	}
	if req.Kind == BadgePaid {
		_ = AnnouncePaidBadge(ctx, paid)
	} else {
		name := strings.ToLower(s.badges.labels[req.Kind])
		s.notifyBadge(ctx, userID, "You have a new badge", "Staff gave you the "+name+" badge.", req.Kind, "granted")
	}
	s.badgesChanged(ctx, userID)
	return s.writeAdminBadge(w, r, userID, req.Kind)
}

func (s *Service) adminRevoke(w http.ResponseWriter, r *http.Request) error {
	ctx := context.WithoutCancel(r.Context())
	staff := auth.MustFromContext(r.Context())
	userID, err := httpx.URLParamUUID(r, "user_id")
	if err != nil {
		return err
	}
	kind := chi.URLParam(r, "kind")
	if !isKind(kind) {
		return httpx.NotFound("No such badge kind.")
	}
	if kind == BadgeOfficial && !staff.IsAdmin() {
		return httpx.Forbidden("Only admins remove official badges.")
	}
	var body struct {
		Reason string `json:"reason"`
	}
	if err := httpx.Decode(r, &body); err != nil {
		return err
	}
	reason := strings.TrimSpace(body.Reason)
	if n := utf8.RuneCountInString(reason); n < 3 || n > 500 {
		return httpx.BadRequest("Give a reason of 3 to 500 characters. The person sees it.")
	}
	var ended bool
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var rerr error
		if ended, rerr = revokeBadge(ctx, tx, userID, kind, reason, staff.UserID); rerr != nil {
			return rerr
		}
		return audit.Log(ctx, tx, staff.UserID, "identity.badge.revoke", "user", userID.String(),
			map[string]any{"kind": kind, "reason": reason, "was_active": ended})
	})
	if err != nil {
		return err
	}
	if ended {
		s.notifyBadge(ctx, userID, "Your "+strings.ToLower(s.badges.labels[kind])+" badge was removed", reason, kind, "revoked")
		s.badgesChanged(ctx, userID)
	}
	if kind == BadgePaid {
		runPaidRevokedHooks(ctx, userID)
	}
	return s.writeAdminBadge(w, r, userID, kind)
}

func (s *Service) writeAdminBadge(w http.ResponseWriter, r *http.Request, userID uuid.UUID, kind string) error {
	b, err := scanAdminBadge(s.d.Pool.QueryRow(r.Context(), `SELECT `+adminBadgeCols+`
		FROM identity_badges b JOIN users u ON u.id = b.user_id WHERE b.user_id = $1 AND b.kind = $2`, userID, kind))
	if err != nil {
		return err
	}
	httpx.OK(w, b)
	return nil
}
