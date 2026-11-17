package identity

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// Reviewed badges (org, notable): a person applies in a category with
// evidence; staff approve (with an expiry), ask for more, or reject.

// Application statuses. submitted and needs_info are open: one open
// application per person and category.
const (
	AppSubmitted = "submitted"  // waiting for staff
	AppNeedsInfo = "needs_info" // staff asked for more; the applicant resubmits
	AppApproved  = "approved"
	AppRejected  = "rejected"
	AppWithdrawn = "withdrawn"
)

var openStatuses = []string{AppSubmitted, AppNeedsInfo}

// Evidence kinds.
const (
	EvidenceURL     = "url"     // a public page that shows who they are; a reviewer opens it
	EvidenceDomain  = "domain"  // a domain proved with a DNS TXT record (verified by the app)
	EvidenceAccount = "account" // an account on another platform (verified through LinkedAccounts)
	EvidenceText    = "text"    // anything else a reviewer should know
)

// Evidence is one item of an application. Verified is computed by the app.
type Evidence struct {
	Kind     string `json:"kind"`
	Ref      string `json:"ref"`
	Verified bool   `json:"verified"`
}

// Application is a badge application as its applicant sees it.
type Application struct {
	ID           uuid.UUID  `json:"id"`
	Category     string     `json:"category"`
	DisplayLabel string     `json:"display_label"`
	Status       string     `json:"status"`
	Evidence     []Evidence `json:"evidence"`
	Note         string     `json:"note"`
	ReviewerNote string     `json:"reviewer_note"`
	SubmittedAt  time.Time  `json:"submitted_at"`
	DecidedAt    *time.Time `json:"decided_at"`
	CreatedAt    time.Time  `json:"created_at"`

	userID uuid.UUID
}

const appCols = `id, user_id, category, display_label, status, evidence, note, reviewer_note, submitted_at, decided_at, created_at`

func scanApp(row pgx.Row) (Application, error) {
	var a Application
	var raw []byte
	if err := row.Scan(&a.ID, &a.userID, &a.Category, &a.DisplayLabel, &a.Status, &raw, &a.Note, &a.ReviewerNote,
		&a.SubmittedAt, &a.DecidedAt, &a.CreatedAt); err != nil {
		return a, err
	}
	a.Evidence = []Evidence{}
	if err := json.Unmarshal(raw, &a.Evidence); err != nil {
		return a, err
	}
	return a, nil
}

var (
	errApplicationOpen  = httpx.NewError(http.StatusConflict, "application_open", "You already have an open application in this category.")
	errApplicationLimit = httpx.NewError(http.StatusTooManyRequests, "application_limit", "You can send 3 applications every 30 days.")
	errNotOpen          = httpx.NewError(http.StatusConflict, "application_closed", "This application was already decided or withdrawn.")
	errNoApplication    = httpx.NotFound("Application not found.")
)

// ApplicationInput is the body of a new application (and a resubmission).
type ApplicationInput struct {
	Category     string     `json:"category"`
	DisplayLabel string     `json:"display_label"`
	Evidence     []Evidence `json:"evidence"`
	Note         string     `json:"note"`
}

// clean validates the input against the configured categories.
func (in *ApplicationInput) clean(cfg *badgeSettings) error {
	c, ok := cfg.category(in.Category)
	if !ok {
		return httpx.BadRequest("Pick one of the listed categories.")
	}
	in.DisplayLabel = strings.TrimSpace(in.DisplayLabel)
	if in.DisplayLabel == "" {
		in.DisplayLabel = c.Label
	}
	if utf8.RuneCountInString(in.DisplayLabel) > 60 {
		return httpx.BadRequest("The name to show is at most 60 characters.")
	}
	in.Note = strings.TrimSpace(in.Note)
	if utf8.RuneCountInString(in.Note) > 2000 {
		return httpx.BadRequest("The note is at most 2,000 characters.")
	}
	if len(in.Evidence) == 0 || len(in.Evidence) > 10 {
		return httpx.BadRequest("Add 1 to 10 pieces of evidence.")
	}
	return nil
}

// LinkedAccounts verifies account evidence: whether ref ("github:octocat")
// names an account the person has connected and proved they own. Stitching
// sets it (e.g. from the connectors feature); without it account evidence is
// shown to reviewers unverified.
type LinkedAccounts interface {
	VerifiedLink(ctx context.Context, userID uuid.UUID, ref string) (bool, error)
}

var linkedAccounts atomic.Pointer[LinkedAccounts]

// SetLinkedAccounts installs the linked-account check (nil removes it).
func SetLinkedAccounts(l LinkedAccounts) {
	if l == nil {
		linkedAccounts.Store(nil)
		return
	}
	linkedAccounts.Store(&l)
}

// checkEvidence validates each item and computes verified.
func checkEvidence(ctx context.Context, q db.Querier, userID uuid.UUID, in []Evidence) ([]Evidence, error) {
	domains, err := verifiedDomains(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	out := make([]Evidence, 0, len(in))
	for _, e := range in {
		e.Ref = strings.TrimSpace(e.Ref)
		if e.Ref == "" || utf8.RuneCountInString(e.Ref) > 500 {
			return nil, httpx.BadRequest("Each piece of evidence needs a reference of at most 500 characters.")
		}
		e.Verified = false
		switch e.Kind {
		case EvidenceURL:
			u, err := url.Parse(e.Ref)
			if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
				return nil, httpx.BadRequest("Links must be web addresses starting with https://.")
			}
		case EvidenceDomain:
			e.Ref = normalizeDomain(e.Ref)
			e.Verified = slices.Contains(domains, e.Ref)
		case EvidenceAccount:
			if l := linkedAccounts.Load(); l != nil {
				ok, err := (*l).VerifiedLink(ctx, userID, e.Ref)
				if err != nil {
					return nil, err
				}
				e.Verified = ok
			}
		case EvidenceText:
		default:
			return nil, httpx.BadRequest("Evidence kinds are url, domain, account and text.")
		}
		out = append(out, e)
	}
	return out, nil
}

// apply files an application (3 per 30 days, one open per category).
func (s *Service) apply(ctx context.Context, userID uuid.UUID, in ApplicationInput) (Application, error) {
	if err := in.clean(&s.badges); err != nil {
		return Application{}, err
	}
	ev, err := checkEvidence(ctx, s.d.Pool, userID, in.Evidence)
	if err != nil {
		return Application{}, err
	}
	raw, err := json.Marshal(ev)
	if err != nil {
		return Application{}, err
	}
	var a Application
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('identity-apply:' || $1::text, 0))`, userID); err != nil {
			return err
		}
		var recent int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM identity_badge_applications
			WHERE user_id = $1 AND created_at > now() - interval '30 days'`, userID).Scan(&recent); err != nil {
			return err
		}
		if recent >= 3 {
			return errApplicationLimit
		}
		a, err = scanApp(tx.QueryRow(ctx, `INSERT INTO identity_badge_applications (user_id, category, display_label, evidence, note)
			VALUES ($1, $2, $3, $4, $5) RETURNING `+appCols, userID, in.Category, in.DisplayLabel, raw, in.Note))
		if db.IsUniqueViolation(err) {
			return errApplicationOpen
		}
		return err
	})
	if err != nil {
		return Application{}, err
	}
	s.announceApplication(ctx, a.ID)
	return a, nil
}

// ownApplication locks one of userID's applications.
func ownApplication(ctx context.Context, tx pgx.Tx, userID, id uuid.UUID) (Application, error) {
	a, err := scanApp(tx.QueryRow(ctx, `SELECT `+appCols+` FROM identity_badge_applications WHERE id = $1 FOR UPDATE`, id))
	if db.IsNoRows(err) || (err == nil && a.userID != userID) {
		return a, errNoApplication
	}
	return a, err
}

// resubmit answers a needs_info request with new evidence and note.
func (s *Service) resubmit(ctx context.Context, userID, id uuid.UUID, in ApplicationInput) (Application, error) {
	var a Application
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		cur, err := ownApplication(ctx, tx, userID, id)
		if err != nil {
			return err
		}
		if cur.Status != AppNeedsInfo {
			return httpx.NewError(http.StatusConflict, "application_not_waiting", "Only an application staff asked about can be sent again.")
		}
		in.Category = cur.Category
		if err := in.clean(&s.badges); err != nil {
			return err
		}
		ev, err := checkEvidence(ctx, tx, userID, in.Evidence)
		if err != nil {
			return err
		}
		raw, err := json.Marshal(ev)
		if err != nil {
			return err
		}
		a, err = scanApp(tx.QueryRow(ctx, `UPDATE identity_badge_applications SET display_label = $2, evidence = $3, note = $4,
			status = 'submitted', submitted_at = now(), updated_at = now() WHERE id = $1 RETURNING `+appCols,
			id, in.DisplayLabel, raw, in.Note))
		return err
	})
	if err != nil {
		return Application{}, err
	}
	s.announceApplication(ctx, a.ID)
	return a, nil
}

// withdraw closes the applicant's own open application.
func (s *Service) withdraw(ctx context.Context, userID, id uuid.UUID) (Application, error) {
	var a Application
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		cur, err := ownApplication(ctx, tx, userID, id)
		if err != nil {
			return err
		}
		if !slices.Contains(openStatuses, cur.Status) {
			return errNotOpen
		}
		a, err = scanApp(tx.QueryRow(ctx, `UPDATE identity_badge_applications SET status = 'withdrawn', decided_at = now(),
			updated_at = now() WHERE id = $1 RETURNING `+appCols, id))
		return err
	})
	return a, err
}

func applicationsOf(ctx context.Context, q db.Querier, userID uuid.UUID) ([]Application, error) {
	rows, err := q.Query(ctx, `SELECT `+appCols+` FROM identity_badge_applications WHERE user_id = $1
		ORDER BY created_at DESC, id DESC LIMIT 50`, userID)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Application, error) { return scanApp(r) })
	if out == nil {
		out = []Application{}
	}
	return out, err
}

// ---- domain proofs -----------------------------------------------------------------

// The TXT record that proves a domain: _scaffoldapp-verify.<domain> (or the
// domain itself) holding scaffoldapp-verify=<token>.
const verifyTag = "scaffoldapp-verify"

// Resolver looks up TXT records (net.DefaultResolver; a fake in tests).
type Resolver interface {
	LookupTXT(ctx context.Context, name string) ([]string, error)
}

// domainLookupTimeout bounds one check (both names).
const domainLookupTimeout = 5 * time.Second

// Domain is a domain proof as its owner sees it, with the record to add.
type Domain struct {
	Domain     string     `json:"domain"`
	TxtName    string     `json:"txt_name"`
	TxtValue   string     `json:"txt_value"`
	Verified   bool       `json:"verified"`
	VerifiedAt *time.Time `json:"verified_at"`
	CheckedAt  *time.Time `json:"checked_at"`
	LastError  string     `json:"last_error"`
}

func domainView(domain, token string, verifiedAt, checkedAt *time.Time, lastErr string) Domain {
	d := Domain{Domain: domain, TxtName: "_" + verifyTag + "." + domain, TxtValue: verifyTag + "=" + token,
		Verified: verifiedAt != nil, VerifiedAt: verifiedAt, CheckedAt: checkedAt}
	if verifiedAt == nil {
		d.LastError = lastErr
	}
	return d
}

// Public email and hosting providers can't prove an organization.
var publicDomains = map[string]bool{
	"gmail.com": true, "googlemail.com": true, "outlook.com": true, "hotmail.com": true, "live.com": true, "msn.com": true,
	"yahoo.com": true, "ymail.com": true, "icloud.com": true, "me.com": true, "mac.com": true, "aol.com": true,
	"proton.me": true, "protonmail.com": true, "gmx.com": true, "gmx.de": true, "mail.com": true, "yandex.com": true,
	"yandex.ru": true, "zoho.com": true, "hey.com": true, "fastmail.com": true, "qq.com": true, "163.com": true,
	"github.io": true, "vercel.app": true, "netlify.app": true, "pages.dev": true, "wordpress.com": true, "blogspot.com": true,
	"herokuapp.com": true, "web.app": true, "firebaseapp.com": true, "substack.com": true, "medium.com": true,
}

func normalizeDomain(d string) string {
	d = strings.ToLower(strings.TrimSpace(d))
	d = strings.TrimPrefix(strings.TrimPrefix(d, "https://"), "http://")
	if i := strings.IndexAny(d, "/?#:"); i >= 0 {
		d = d[:i]
	}
	return strings.TrimSuffix(strings.TrimPrefix(d, "www."), ".")
}

func validDomain(d string) bool {
	if len(d) < 4 || len(d) > 253 || !strings.Contains(d, ".") || net.ParseIP(d) != nil {
		return false
	}
	for _, label := range strings.Split(d, ".") {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
				return false
			}
		}
	}
	return true
}

func publicDomain(d string) bool {
	for ; strings.Contains(d, "."); d = d[strings.IndexByte(d, '.')+1:] {
		if publicDomains[d] {
			return true
		}
	}
	return false
}

// addDomain starts a proof; adding a domain again keeps its token.
func (s *Service) addDomain(ctx context.Context, userID uuid.UUID, raw string) (Domain, error) {
	domain := normalizeDomain(raw)
	if !validDomain(domain) {
		return Domain{}, httpx.BadRequest("That isn't a domain name. Enter it like example.com.")
	}
	if publicDomain(domain) {
		return Domain{}, httpx.BadRequest("Public email and hosting domains can't prove an organization. Use your own domain.")
	}
	var d Domain
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('identity-domains:' || $1::text, 0))`, userID); err != nil {
			return err
		}
		var n int
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT count(*), COALESCE(bool_or(domain = $2), false) FROM identity_domains WHERE user_id = $1`,
			userID, domain).Scan(&n, &exists); err != nil {
			return err
		}
		if n >= 10 && !exists {
			return httpx.BadRequest("You can prove at most 10 domains. Remove one first.")
		}
		var token, lastErr string
		var verifiedAt, checkedAt *time.Time
		if err := tx.QueryRow(ctx, `INSERT INTO identity_domains (user_id, domain, token) VALUES ($1, $2, $3)
			ON CONFLICT (user_id, domain) DO UPDATE SET domain = EXCLUDED.domain
			RETURNING token, verified_at, checked_at, last_error`, userID, domain, strings.ReplaceAll(uuid.NewString(), "-", "")).
			Scan(&token, &verifiedAt, &checkedAt, &lastErr); err != nil {
			return err
		}
		d = domainView(domain, token, verifiedAt, checkedAt, lastErr)
		return nil
	})
	return d, err
}

// findTXT looks for verifyTag=token at _verifyTag.<domain>, then at the
// domain itself; detail says what was found when it isn't there.
func findTXT(ctx context.Context, r Resolver, domain, token string) (bool, string) {
	want := verifyTag + "=" + token
	name := "_" + verifyTag + "." + domain
	var firstErr error
	sawOther := false
	for _, n := range []string{name, domain} {
		recs, err := r.LookupTXT(ctx, n)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		for _, rec := range recs {
			v := strings.TrimSpace(strings.Trim(rec, `"`))
			if v == want {
				return true, ""
			}
			if strings.HasPrefix(v, verifyTag+"=") {
				sawOther = true
			}
		}
	}
	switch {
	case sawOther:
		return false, "Found a " + verifyTag + " record, but not this one. Copy the value again."
	case firstErr != nil && !notFound(firstErr):
		return false, "The DNS lookup for " + name + " failed (" + lookupError(firstErr) + "). Try again in a minute."
	default:
		return false, "No TXT record at " + name + " yet. DNS changes can take a while to appear."
	}
}

func notFound(err error) bool {
	var de *net.DNSError
	return errors.As(err, &de) && de.IsNotFound
}

func lookupError(err error) string {
	var de *net.DNSError
	switch {
	case errors.As(err, &de) && de.IsTimeout, errors.Is(err, context.DeadlineExceeded):
		return "timed out"
	case errors.As(err, &de):
		return "server error"
	}
	return "lookup error"
}

// checkDomain looks the record up (10 s apart per domain) and records the result.
func (s *Service) checkDomain(ctx context.Context, userID uuid.UUID, raw string) (Domain, error) {
	domain := normalizeDomain(raw)
	var token string
	var checkedAt *time.Time
	err := s.d.Pool.QueryRow(ctx, `SELECT token, checked_at FROM identity_domains WHERE user_id = $1 AND domain = $2`,
		userID, domain).Scan(&token, &checkedAt)
	if db.IsNoRows(err) {
		return Domain{}, httpx.NotFound("Add the domain first.")
	}
	if err != nil {
		return Domain{}, err
	}
	if checkedAt != nil && time.Since(*checkedAt) < 10*time.Second {
		return Domain{}, httpx.NewError(http.StatusTooManyRequests, "rate_limited", "Wait a few seconds before checking again.")
	}
	lctx, cancel := context.WithTimeout(ctx, domainLookupTimeout)
	found, detail := findTXT(lctx, s.resolver, domain, token)
	cancel()
	var verifiedAt *time.Time
	var lastErr string
	if err := s.d.Pool.QueryRow(ctx, `UPDATE identity_domains SET checked_at = now(), last_error = $3,
		  verified_at = CASE WHEN $4 THEN COALESCE(verified_at, now()) ELSE verified_at END
		WHERE user_id = $1 AND domain = $2 RETURNING verified_at, checked_at, last_error`,
		userID, domain, detail, found).Scan(&verifiedAt, &checkedAt, &lastErr); err != nil {
		return Domain{}, err
	}
	if verifiedAt == nil {
		return Domain{}, httpx.NewError(http.StatusUnprocessableEntity, "domain_unverified", detail)
	}
	return domainView(domain, token, verifiedAt, checkedAt, lastErr), nil
}

func (s *Service) removeDomain(ctx context.Context, userID uuid.UUID, raw string) error {
	tag, err := s.d.Pool.Exec(ctx, `DELETE FROM identity_domains WHERE user_id = $1 AND domain = $2`, userID, normalizeDomain(raw))
	if err == nil && tag.RowsAffected() == 0 {
		return httpx.NotFound("Domain not found.")
	}
	return err
}

func domainsOf(ctx context.Context, q db.Querier, userID uuid.UUID) ([]Domain, error) {
	rows, err := q.Query(ctx, `SELECT domain, token, verified_at, checked_at, last_error FROM identity_domains
		WHERE user_id = $1 ORDER BY domain`, userID)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Domain, error) {
		var domain, token, lastErr string
		var verifiedAt, checkedAt *time.Time
		err := r.Scan(&domain, &token, &verifiedAt, &checkedAt, &lastErr)
		return domainView(domain, token, verifiedAt, checkedAt, lastErr), err
	})
	if out == nil {
		out = []Domain{}
	}
	return out, err
}

func verifiedDomains(ctx context.Context, q db.Querier, userID uuid.UUID) ([]string, error) {
	rows, err := q.Query(ctx, `SELECT domain FROM identity_domains WHERE user_id = $1 AND verified_at IS NOT NULL`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}
