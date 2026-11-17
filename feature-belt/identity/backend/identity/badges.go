package identity

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/bg"
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/notify"
	"scaffoldapp/backend/internal/realtime"
)

// Verification badges, beside ID verification: four kinds with distinct
// marks, highest first. A person's badge is the highest active one they hold.
//   - official: the app's own team and accounts; admins grant it.
//   - org: a business or organization; staff approve an application.
//   - notable: a public figure or creator; staff approve an application.
//   - paid: a paid plan's perk; granted and ended by an entitlement through
//     SetPaidBadge (stitching wires payments to it). Identity isn't checked.
const (
	BadgeOfficial = "official"
	BadgeOrg      = "org"
	BadgeNotable  = "notable"
	BadgePaid     = "paid"
)

// badgeKinds in precedence order.
var badgeKinds = []string{BadgeOfficial, BadgeOrg, BadgeNotable, BadgePaid}

// rankSQL orders badge rows highest first.
const rankSQL = `CASE kind WHEN 'official' THEN 0 WHEN 'org' THEN 1 WHEN 'notable' THEN 2 ELSE 3 END`

// activeSQL is an identity_badges row that shows now.
const activeSQL = `revoked_at IS NULL AND (expires_at IS NULL OR expires_at > now())`

// Badge sources (identity_badges.source). A paid badge staff removed is
// held (SourceStaffRevoked): billing can't bring it back until an admin lifts
// the removal (RestorePaid).
const (
	SourceReview       = "review"
	SourceAdmin        = "admin"
	SourceBilling      = "billing"
	SourceStaffRevoked = "staff_revoked"
)

// Category is a kind of reviewed badge people apply for
// (IDENTITY_BADGE_CATEGORIES): its key, the badge kind it grants and its label.
type Category struct {
	Key   string `json:"key"`
	Kind  string `json:"kind"` // org | notable
	Label string `json:"label"`
}

// KindInfo names a badge kind (IDENTITY_BADGE_LABELS).
type KindInfo struct {
	Kind  string `json:"kind"`
	Label string `json:"label"`
}

type badgeSettings struct {
	labels      map[string]string
	categories  []Category
	ttlDays     int    // reviewed badges: default expiry
	paidKey     string // payments entitlement that grants the paid badge
	paidMinDays int    // account age before the paid badge shows
	paidNeedsID bool   // paid badge only for ID-verified people
}

const (
	defaultBadgeLabels     = "official=Official,org=Organization,notable=Notable,paid=Supporter"
	defaultBadgeCategories = "business:org:Business,organization:org:Organization,public_figure:notable:Public figure,creator:notable:Creator"
	maxBadgeTTLDays        = 730
)

var categoryKey = regexp.MustCompile(`^[a-z][a-z0-9_]{1,40}$`)

// parseBadgeLabels reads kind=Label pairs over the defaults.
func parseBadgeLabels(raw string) (map[string]string, error) {
	out := map[string]string{}
	for _, part := range strings.Split(defaultBadgeLabels+","+raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kind, label, ok := strings.Cut(part, "=")
		kind, label = strings.TrimSpace(kind), strings.TrimSpace(label)
		if !ok || !isKind(kind) || label == "" || utf8.RuneCountInString(label) > 40 {
			return nil, fmt.Errorf("%q is not kind=Label (kinds: official, org, notable, paid; labels up to 40 characters)", part)
		}
		out[kind] = label
	}
	return out, nil
}

// parseBadgeCategories reads key:kind:Label entries.
func parseBadgeCategories(raw string) ([]Category, error) {
	var out []Category
	seen := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		f := strings.SplitN(part, ":", 3)
		if len(f) != 3 {
			return nil, fmt.Errorf("%q is not key:kind:Label", part)
		}
		c := Category{Key: strings.TrimSpace(f[0]), Kind: strings.TrimSpace(f[1]), Label: strings.TrimSpace(f[2])}
		switch {
		case !categoryKey.MatchString(c.Key):
			return nil, fmt.Errorf("category key %q must be lowercase letters, digits and _", c.Key)
		case c.Kind != BadgeOrg && c.Kind != BadgeNotable:
			return nil, fmt.Errorf("category %q: kind must be org or notable", c.Key)
		case c.Label == "" || utf8.RuneCountInString(c.Label) > 60:
			return nil, fmt.Errorf("category %q: label must be 1 to 60 characters", c.Key)
		case seen[c.Key]:
			return nil, fmt.Errorf("category %q appears twice", c.Key)
		}
		seen[c.Key] = true
		out = append(out, c)
	}
	return out, nil
}

func readBadgeSettings(dev bool) badgeSettings {
	b := badgeSettings{
		ttlDays:     config.Int("IDENTITY_BADGE_TTL_DAYS", 365),
		paidKey:     config.String("IDENTITY_PAID_ENTITLEMENT", "supporter"),
		paidNeedsID: config.Bool("IDENTITY_PAID_REQUIRES_VERIFIED", false),
	}
	minDays := 30
	if dev {
		minDays = 0 // dev accounts are minutes old
	}
	b.paidMinDays = config.Int("IDENTITY_PAID_MIN_ACCOUNT_DAYS", minDays)
	var err error
	if b.labels, err = parseBadgeLabels(config.String("IDENTITY_BADGE_LABELS", "")); err != nil {
		config.Problem("IDENTITY_BADGE_LABELS: %v", err)
		b.labels, _ = parseBadgeLabels("")
	}
	if b.categories, err = parseBadgeCategories(config.String("IDENTITY_BADGE_CATEGORIES", defaultBadgeCategories)); err != nil {
		config.Problem("IDENTITY_BADGE_CATEGORIES: %v", err)
		b.categories, _ = parseBadgeCategories(defaultBadgeCategories)
	}
	if b.ttlDays < 1 || b.ttlDays > maxBadgeTTLDays {
		config.Problem("IDENTITY_BADGE_TTL_DAYS must be 1 to %d (got %d)", maxBadgeTTLDays, b.ttlDays)
		b.ttlDays = 365
	}
	if b.paidMinDays < 0 {
		config.Problem("IDENTITY_PAID_MIN_ACCOUNT_DAYS must not be negative (got %d)", b.paidMinDays)
		b.paidMinDays = 0
	}
	if !regexp.MustCompile(`^[a-z][a-z0-9_]{1,62}$`).MatchString(b.paidKey) {
		config.Problem("IDENTITY_PAID_ENTITLEMENT must be an entitlement key like supporter (got %q)", b.paidKey)
		b.paidKey = "supporter"
	}
	return b
}

func isKind(k string) bool {
	for _, x := range badgeKinds {
		if x == k {
			return true
		}
	}
	return false
}

func (b *badgeSettings) category(key string) (Category, bool) {
	for _, c := range b.categories {
		if c.Key == key {
			return c, true
		}
	}
	return Category{}, false
}

func (b *badgeSettings) kinds() []KindInfo {
	out := make([]KindInfo, 0, len(badgeKinds))
	for _, k := range badgeKinds {
		out = append(out, KindInfo{Kind: k, Label: b.labels[k]})
	}
	return out
}

// badgeConfig is the installed module's badge settings (defaults when the
// module isn't constructed, as in tests of other packages).
func badgeConfig() *badgeSettings {
	if s := installed.Load(); s != nil {
		return &s.badges
	}
	b := badgeSettings{ttlDays: 365, paidKey: "supporter", paidMinDays: 30}
	b.labels, _ = parseBadgeLabels("")
	b.categories, _ = parseBadgeCategories(defaultBadgeCategories)
	return &b
}

// PaidEntitlement is the payments entitlement key that grants the paid badge
// (IDENTITY_PAID_ENTITLEMENT, default supporter).
func PaidEntitlement() string { return badgeConfig().paidKey }

// ---- readers -----------------------------------------------------------------------

// HeldBadge is a badge someone holds, as anyone may see it.
type HeldBadge struct {
	Kind      string    `json:"kind"`       // official | org | notable | paid
	Label     string    `json:"label"`      // what the badge says ("Riverside Bikes"); the kind's label when none
	KindLabel string    `json:"kind_label"` // the kind's name (IDENTITY_BADGE_LABELS)
	Since     time.Time `json:"since"`      // start of the current unbroken hold
}

func heldBadge(kind, label string, since time.Time) HeldBadge {
	kl := badgeConfig().labels[kind]
	if label == "" {
		label = kl
	}
	return HeldBadge{Kind: kind, Label: label, KindLabel: kl, Since: since}
}

// BadgeOf returns the person's highest active badge, nil when they hold none.
// Unknown ids read as none.
func BadgeOf(ctx context.Context, q db.Querier, userID uuid.UUID) (*HeldBadge, error) {
	var kind, label string
	var since time.Time
	err := q.QueryRow(ctx, `SELECT kind, label, granted_at FROM identity_badges
		WHERE user_id = $1 AND `+activeSQL+` ORDER BY `+rankSQL+` LIMIT 1`, userID).Scan(&kind, &label, &since)
	if db.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	b := heldBadge(kind, label, since)
	return &b, nil
}

// BadgeSet returns the highest active badge of each of ids that holds one
// (for badges on lists; missing ids hold none).
func BadgeSet(ctx context.Context, q db.Querier, ids []uuid.UUID) (map[uuid.UUID]HeldBadge, error) {
	out := make(map[uuid.UUID]HeldBadge, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := q.Query(ctx, `SELECT DISTINCT ON (user_id) user_id, kind, label, granted_at FROM identity_badges
		WHERE user_id = ANY($1) AND `+activeSQL+` ORDER BY user_id, `+rankSQL, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var kind, label string
		var since time.Time
		if err := rows.Scan(&id, &kind, &label, &since); err != nil {
			return nil, err
		}
		out[id] = heldBadge(kind, label, since)
	}
	return out, rows.Err()
}

// OwnBadge is a badge as its holder (and staff) see it.
type OwnBadge struct {
	HeldBadge
	Category  *string    `json:"category"`
	Source    string     `json:"source"` // review | admin | billing
	ExpiresAt *time.Time `json:"expires_at"`
}

func activeBadges(ctx context.Context, q db.Querier, userID uuid.UUID) ([]OwnBadge, error) {
	rows, err := q.Query(ctx, `SELECT kind, label, granted_at, category, source, expires_at FROM identity_badges
		WHERE user_id = $1 AND `+activeSQL+` ORDER BY `+rankSQL, userID)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (OwnBadge, error) {
		var b OwnBadge
		var kind, label string
		var since time.Time
		err := r.Scan(&kind, &label, &since, &b.Category, &b.Source, &b.ExpiresAt)
		b.HeldBadge = heldBadge(kind, label, since)
		return b, err
	})
	if out == nil {
		out = []OwnBadge{}
	}
	return out, err
}

// ---- grants ------------------------------------------------------------------------

// ErrStaffRevoked: billing can't bring back a paid badge staff removed; an
// admin lifts the removal first (Admin → Identity → Badges, RestorePaid).
var ErrStaffRevoked = errors.New("identity: the paid badge was removed by staff")

type grantInput struct {
	userID      uuid.UUID
	kind        string
	label       string // "" = the kind's label at read time
	category    *string
	source      string
	application *uuid.UUID
	by          *uuid.UUID
	expires     *time.Time
}

// badgeRow is the locked state of one identity_badges row.
type badgeRow struct {
	exists     bool
	active     bool
	source     string
	reason     string
	paidActive bool
	expires    *time.Time
}

func lockBadge(ctx context.Context, q db.Querier, userID uuid.UUID, kind string) (badgeRow, error) {
	var b badgeRow
	err := q.QueryRow(ctx, `SELECT `+activeSQL+`, source, revoked_reason, paid_active, expires_at FROM identity_badges
		WHERE user_id = $1 AND kind = $2 FOR UPDATE`, userID, kind).Scan(&b.active, &b.source, &b.reason, &b.paidActive, &b.expires)
	if db.IsNoRows(err) {
		return badgeRow{}, nil
	}
	b.exists = err == nil
	return b, err
}

// grant gives the person a badge or renews it, clearing any revocation; the
// hold's start moves only when the badge wasn't showing. A billing grant
// never undoes a staff removal (ErrStaffRevoked). wasActive reports whether
// it was showing before.
func grant(ctx context.Context, q db.Querier, g grantInput) (wasActive bool, err error) {
	prev, err := lockBadge(ctx, q, g.userID, g.kind)
	if err != nil {
		return false, err
	}
	if prev.exists && prev.source == SourceStaffRevoked && g.source == SourceBilling {
		return false, ErrStaffRevoked
	}
	_, err = q.Exec(ctx, `
		INSERT INTO identity_badges (user_id, kind, label, category, source, application_id, granted_by, expires_at, paid_active)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (user_id, kind) DO UPDATE SET label = EXCLUDED.label, category = EXCLUDED.category,
		  source = EXCLUDED.source, application_id = EXCLUDED.application_id, granted_by = EXCLUDED.granted_by,
		  granted_at = CASE WHEN $10 THEN identity_badges.granted_at ELSE now() END,
		  expires_at = EXCLUDED.expires_at, revoked_at = NULL, revoked_by = NULL, revoked_reason = '',
		  paid_active = EXCLUDED.paid_active, updated_at = now()`,
		g.userID, g.kind, g.label, g.category, g.source, g.application, g.by, g.expires, g.kind == BadgePaid, prev.active)
	return prev.active, err
}

// ---- the paid badge ----------------------------------------------------------------

// Outcomes of SetPaidBadge.
const (
	PaidGranted    = "granted"    // the badge started showing
	PaidRenewed    = "renewed"    // its end moved
	PaidEnded      = "ended"      // it stopped showing (the entitlement ended)
	PaidHeld       = "held"       // the entitlement is active but staff removed the badge: stop the renewal
	PaidIneligible = "ineligible" // the entitlement is active but the person isn't eligible (yet)
	PaidUnchanged  = "unchanged"
)

// PaidBadgeChange says what SetPaidBadge did. Hand it to AnnouncePaidBadge
// once the transaction commits.
type PaidBadgeChange struct {
	UserID  uuid.UUID
	Outcome string
}

// SetPaidBadge grants, renews or ends the paid badge as the entitlement that
// pays for it changes: active with until (zero = no end) while it holds,
// inactive when it ends. Call it inside the transaction that changed the
// entitlement (a payments.OnEntitlementChange hook); it is idempotent. Policy
// outcomes are not errors:
//   - a badge staff removed stays removed (PaidHeld; the entitlement state is
//     kept, so RestorePaid can bring the badge back),
//   - a person who isn't eligible (account younger than
//     IDENTITY_PAID_MIN_ACCOUNT_DAYS, suspended, or not ID-verified with
//     IDENTITY_PAID_REQUIRES_VERIFIED) doesn't get it (PaidIneligible); the
//     housekeeping job grants it when they become eligible while it's paid for.
func SetPaidBadge(ctx context.Context, q db.Querier, userID uuid.UUID, active bool, until time.Time) (PaidBadgeChange, error) {
	ch := PaidBadgeChange{UserID: userID, Outcome: PaidUnchanged}
	var exp *time.Time
	if !until.IsZero() {
		u := until.UTC()
		exp = &u
		if !u.After(time.Now()) {
			active = false
		}
	}
	var status string
	if err := q.QueryRow(ctx, `SELECT status FROM users WHERE id = $1`, userID).Scan(&status); err != nil {
		if db.IsNoRows(err) {
			return ch, nil
		}
		return ch, err
	}
	if status == "deleted" {
		return ch, nil
	}
	prev, err := lockBadge(ctx, q, userID, BadgePaid)
	if err != nil {
		return ch, err
	}
	if prev.exists && prev.source == SourceStaffRevoked {
		// Held: remember what billing says, show nothing.
		if _, err := q.Exec(ctx, `UPDATE identity_badges SET paid_active = $2, expires_at = $3, updated_at = now()
			WHERE user_id = $1 AND kind = 'paid'`, userID, active, exp); err != nil {
			return ch, err
		}
		if active {
			ch.Outcome = PaidHeld
		}
		return ch, nil
	}
	if !active {
		if !prev.exists {
			return ch, nil
		}
		if _, err := q.Exec(ctx, `UPDATE identity_badges SET paid_active = false,
			revoked_at = CASE WHEN `+activeSQL+` THEN now() ELSE revoked_at END,
			revoked_reason = CASE WHEN `+activeSQL+` THEN 'ended' ELSE revoked_reason END, updated_at = now()
			WHERE user_id = $1 AND kind = 'paid'`, userID); err != nil {
			return ch, err
		}
		if prev.active {
			ch.Outcome = PaidEnded
		}
		return ch, nil
	}
	ok, _, err := paidEligibility(ctx, q, userID, false)
	if err != nil {
		return ch, err
	}
	if !ok {
		// Paid for but withheld: a revoked row the housekeeping job revisits.
		if _, err := q.Exec(ctx, `
			INSERT INTO identity_badges (user_id, kind, label, source, expires_at, paid_active, revoked_at, revoked_reason)
			VALUES ($1, 'paid', '', 'billing', $2, true, now(), 'not_eligible')
			ON CONFLICT (user_id, kind) DO UPDATE SET source = 'billing', expires_at = EXCLUDED.expires_at, paid_active = true,
			  revoked_at = COALESCE(identity_badges.revoked_at, now()), revoked_reason = 'not_eligible', updated_at = now()`,
			userID, exp); err != nil {
			return ch, err
		}
		ch.Outcome = PaidIneligible
		if prev.active {
			ch.Outcome = PaidEnded
		}
		return ch, nil
	}
	was, err := grant(ctx, q, grantInput{userID: userID, kind: BadgePaid, source: SourceBilling, expires: exp})
	if err != nil {
		return ch, err
	}
	switch {
	case !was:
		ch.Outcome = PaidGranted
	case !sameTime(prev.expires, exp):
		ch.Outcome = PaidRenewed
	}
	return ch, nil
}

func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

// Requirement is one condition for the paid badge.
type Requirement struct {
	Key    string `json:"key"` // account_age | good_standing | identity_verified | not_removed
	Label  string `json:"label"`
	Met    bool   `json:"met"`
	Detail string `json:"detail"`
}

// paidEligibility checks the paid badge's conditions. withHold adds the staff
// removal (shown to the person; SetPaidBadge handles a hold itself).
func paidEligibility(ctx context.Context, q db.Querier, userID uuid.UUID, withHold bool) (bool, []Requirement, error) {
	cfg := badgeConfig()
	var created time.Time
	var status string
	var held bool
	if err := q.QueryRow(ctx, `SELECT u.created_at, u.status,
		  EXISTS (SELECT 1 FROM identity_badges b WHERE b.user_id = u.id AND b.kind = 'paid' AND b.source = 'staff_revoked')
		FROM users u WHERE u.id = $1`, userID).Scan(&created, &status, &held); err != nil {
		return false, nil, err
	}
	age := int(time.Since(created) / (24 * time.Hour))
	reqs := []Requirement{
		{Key: "account_age", Label: "Account age", Met: age >= cfg.paidMinDays,
			Detail: fmt.Sprintf("%d of %d days", min(age, cfg.paidMinDays), cfg.paidMinDays)},
		{Key: "good_standing", Label: "Good standing", Met: status == "active",
			Detail: map[bool]string{true: "Your account is in good standing", false: "Your account is restricted"}[status == "active"]},
	}
	if cfg.paidNeedsID {
		ok, err := IsVerified(ctx, q, userID)
		if err != nil {
			return false, nil, err
		}
		reqs = append(reqs, Requirement{Key: "identity_verified", Label: "Verified identity", Met: ok,
			Detail: map[bool]string{true: "Your identity is verified", false: "Verify your identity first"}[ok]})
	}
	if withHold && held {
		reqs = append(reqs, Requirement{Key: "not_removed", Label: "Not removed by staff", Met: false,
			Detail: "Staff removed your " + strings.ToLower(cfg.labels[BadgePaid]) + " badge. Contact support."})
	}
	ok := true
	for _, r := range reqs {
		ok = ok && r.Met
	}
	return ok, reqs, nil
}

// ErrPaidNotEligible is the 422 for buying the paid badge's plan without
// being eligible (a payments.GuardCheckout in stitching returns it).
func errPaidNotEligible(reqs []Requirement) error {
	var unmet []string
	for _, r := range reqs {
		if !r.Met {
			unmet = append(unmet, r.Detail)
		}
	}
	e := httpx.NewError(http.StatusUnprocessableEntity, "not_eligible",
		"You can't get this badge yet: "+strings.Join(unmet, "; ")+".")
	e.Details = map[string]any{"requirements": reqs}
	return e
}

// CheckPaidEligible returns nil when the person may hold the paid badge, else
// a 422 not_eligible listing what's missing (also when staff removed it).
// Stitching calls it from a payments.GuardCheckout for the plans that grant
// PaidEntitlement, so nobody pays for a badge they can't have.
func CheckPaidEligible(ctx context.Context, q db.Querier, userID uuid.UUID) error {
	ok, reqs, err := paidEligibility(ctx, q, userID, true)
	if err != nil || ok {
		return err
	}
	return errPaidNotEligible(reqs)
}

// restorePaid lifts a staff removal of the paid badge: billing may grant it
// again, and it shows now if the entitlement billing last reported still
// holds and the person is eligible. lifted is false when there was no removal.
func restorePaid(ctx context.Context, tx pgx.Tx, userID uuid.UUID) (lifted bool, ch PaidBadgeChange, err error) {
	ch = PaidBadgeChange{UserID: userID, Outcome: PaidUnchanged}
	var paid bool
	var exp *time.Time
	err = tx.QueryRow(ctx, `UPDATE identity_badges SET source = 'billing', revoked_reason = 'ended', updated_at = now()
		WHERE user_id = $1 AND kind = 'paid' AND source = 'staff_revoked' RETURNING paid_active, expires_at`, userID).Scan(&paid, &exp)
	if db.IsNoRows(err) {
		return false, ch, nil
	}
	if err != nil {
		return false, ch, err
	}
	if !paid || (exp != nil && !exp.After(time.Now())) {
		return true, ch, nil
	}
	var until time.Time
	if exp != nil {
		until = *exp
	}
	ch, err = SetPaidBadge(ctx, tx, userID, true, until)
	return true, ch, err
}

// AnnouncePaidBadge tells the person what SetPaidBadge did (a notification
// for granted and ended, and a live update of their badges). Call it after
// the transaction commits (payments.AfterCommit in stitching).
func AnnouncePaidBadge(ctx context.Context, ch PaidBadgeChange) error {
	s := installed.Load()
	if s == nil {
		return nil
	}
	label := s.badges.labels[BadgePaid]
	switch ch.Outcome {
	case PaidGranted:
		s.notifyBadge(ctx, ch.UserID, "Your "+label+" badge is live", "Thanks for your support. It shows next to your name.", BadgePaid, "granted")
	case PaidEnded:
		s.notifyBadge(ctx, ch.UserID, "Your "+label+" badge ended", "The plan that pays for it ended. Get it again any time.", BadgePaid, "ended")
	case PaidUnchanged:
		return nil
	}
	s.badgesChanged(ctx, ch.UserID)
	return nil
}

// PaidRevokedHook runs after staff remove a person's paid badge (after the
// transaction commits): stitching stops the subscription renewing.
type PaidRevokedHook func(ctx context.Context, userID uuid.UUID) error

var paidRevokedHooks struct {
	sync.RWMutex
	list []PaidRevokedHook
}

// OnPaidBadgeRevoked registers a hook for staff removals of the paid badge.
func OnPaidBadgeRevoked(h PaidRevokedHook) {
	paidRevokedHooks.Lock()
	defer paidRevokedHooks.Unlock()
	paidRevokedHooks.list = append(paidRevokedHooks.list, h)
}

func runPaidRevokedHooks(ctx context.Context, userID uuid.UUID) {
	paidRevokedHooks.RLock()
	hooks := append([]PaidRevokedHook(nil), paidRevokedHooks.list...)
	paidRevokedHooks.RUnlock()
	for _, h := range hooks {
		bg.Go(ctx, "identity.paid_revoked", func(ctx context.Context) error { return h(ctx, userID) })
	}
}

// ---- staff revocation --------------------------------------------------------------

// revokeBadge ends a badge for a reason. A paid badge is also held, even one
// that had already ended, so billing can't re-grant it. ended reports a badge
// that was showing.
func revokeBadge(ctx context.Context, tx pgx.Tx, userID uuid.UUID, kind, reason string, by uuid.UUID) (ended bool, err error) {
	prev, err := lockBadge(ctx, tx, userID, kind)
	if err != nil {
		return false, err
	}
	if !prev.exists || (!prev.active && kind != BadgePaid) {
		return false, httpx.NotFound("This person has no such badge.")
	}
	if kind == BadgePaid && prev.source == SourceStaffRevoked {
		return false, httpx.Conflict("This badge is already removed.")
	}
	_, err = tx.Exec(ctx, `UPDATE identity_badges SET
		  revoked_at = COALESCE(revoked_at, now()), revoked_by = $3, revoked_reason = $4,
		  source = CASE WHEN kind = 'paid' THEN 'staff_revoked' ELSE source END, updated_at = now()
		WHERE user_id = $1 AND kind = $2`, userID, kind, by, reason)
	return prev.active, err
}

// ---- housekeeping ------------------------------------------------------------------

// expireBadges ends badges past their expiry and tells their holders.
func (s *Service) expireBadges(ctx context.Context) error {
	rows, err := s.d.Pool.Query(ctx, `UPDATE identity_badges SET revoked_at = expires_at, revoked_reason = 'expired', updated_at = now()
		WHERE revoked_at IS NULL AND expires_at IS NOT NULL AND expires_at <= now() RETURNING user_id, kind`)
	if err != nil {
		return err
	}
	type ended struct {
		UserID uuid.UUID
		Kind   string
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[ended])
	if err != nil {
		return err
	}
	for _, e := range list {
		label := s.badges.labels[e.Kind]
		if e.Kind == BadgePaid {
			s.notifyBadge(ctx, e.UserID, "Your "+label+" badge ended", "The plan that pays for it ended. Get it again any time.", e.Kind, "ended")
		} else {
			s.notifyBadge(ctx, e.UserID, "Your "+strings.ToLower(label)+" badge expired",
				"Badges are reviewed again from time to time. Apply again to keep it.", e.Kind, "expired")
		}
		s.badgesChanged(ctx, e.UserID)
	}
	return nil
}

// grantWithheldPaid shows paid badges that were withheld for eligibility once
// their holders become eligible, while the plan still pays for them.
func (s *Service) grantWithheldPaid(ctx context.Context) error {
	rows, err := s.d.Pool.Query(ctx, `SELECT user_id, expires_at FROM identity_badges
		WHERE kind = 'paid' AND source = 'billing' AND revoked_reason = 'not_eligible' AND paid_active
		  AND (expires_at IS NULL OR expires_at > now()) LIMIT 200`)
	if err != nil {
		return err
	}
	type withheld struct {
		UserID  uuid.UUID
		Expires *time.Time
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[withheld])
	if err != nil {
		return err
	}
	var errs []error
	for _, w := range list {
		var until time.Time
		if w.Expires != nil {
			until = *w.Expires
		}
		var ch PaidBadgeChange
		err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
			var err error
			ch, err = SetPaidBadge(ctx, tx, w.UserID, true, until)
			return err
		})
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if ch.Outcome == PaidGranted {
			_ = AnnouncePaidBadge(ctx, ch)
		}
	}
	return errors.Join(errs...)
}

// ---- side effects ------------------------------------------------------------------

// KindBadge is the notification kind for badge decisions, removals and ends.
const KindBadge = "identity.badge"

func (s *Service) notifyBadge(ctx context.Context, userID uuid.UUID, title, body, kind, event string) {
	if err := s.d.Notify.Send(ctx, notify.Notification{UserID: userID, Kind: KindBadge, Title: title, Body: body,
		Link: "/settings/identity", Data: map[string]any{"kind": kind, "event": event}}); err != nil {
		slog.WarnContext(ctx, "identity: notify badge", "user_id", userID, "err", err)
	}
}

// badgesChanged tells the person's clients to reload their badges (event
// identity.badges on their user topic).
func (s *Service) badgesChanged(ctx context.Context, userID uuid.UUID) {
	if err := s.d.Broker.Publish(context.WithoutCancel(ctx), realtime.UserTopic(userID), "identity.badges",
		map[string]string{"user_id": userID.String()}); err != nil {
		slog.WarnContext(ctx, "identity: publish badges", "user_id", userID, "err", err)
	}
}

// announceApplication tells the admin console an application awaits review.
func (s *Service) announceApplication(ctx context.Context, id uuid.UUID) {
	if err := s.d.Broker.Publish(context.WithoutCancel(ctx), realtime.AdminLiveTopic, "identity.badge_application",
		map[string]uuid.UUID{"id": id}); err != nil {
		slog.WarnContext(ctx, "identity: publish application", "application_id", id, "err", err)
	}
}
