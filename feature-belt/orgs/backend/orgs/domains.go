package orgs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/audit"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// Domain policies: what a verified domain lets people with an email on it do.
const (
	PolicyAutoJoin = "auto_join" // join as a member when they open the app
	PolicyRequest  = "request"   // ask to join; an admin approves
)

// Event on orgs:<id> when the org's domains change (admins refetch).
const EventDomainsChanged = "orgs.domains_changed"

const (
	maxDomains          = 20
	domainAddsPerHour   = 20
	verifyChecksPerHour = 30
	dnsCheckCooldown    = 15 * time.Second
	dnsTimeout          = 5 * time.Second
	// verifyLabel names the TXT record (_scaffoldapp-verify.<domain>) and
	// prefixes its value (scaffoldapp-verify=<token>).
	verifyLabel = "scaffoldapp-verify"
)

// publicEmailDomains are free mailbox providers: anyone can get an address
// there, so no org may claim them. ORGS_PUBLIC_EMAIL_DOMAINS adds more.
var publicEmailDomains = []string{
	"gmail.com", "googlemail.com", "outlook.com", "hotmail.com", "hotmail.co.uk", "hotmail.fr", "hotmail.de",
	"live.com", "live.co.uk", "msn.com", "passport.com", "icloud.com", "me.com", "mac.com",
	"yahoo.com", "yahoo.co.uk", "yahoo.co.jp", "yahoo.fr", "yahoo.de", "yahoo.ca", "yahoo.com.au", "ymail.com",
	"rocketmail.com", "aol.com", "aim.com", "proton.me", "protonmail.com", "protonmail.ch", "pm.me",
	"tutanota.com", "tutanota.de", "tuta.io", "tuta.com", "gmx.com", "gmx.net", "gmx.de", "gmx.at", "web.de",
	"mail.com", "email.com", "zoho.com", "zohomail.com", "yandex.com", "yandex.ru", "ya.ru", "mail.ru", "inbox.ru",
	"bk.ru", "list.ru", "rambler.ru", "qq.com", "foxmail.com", "163.com", "126.com", "yeah.net", "sina.com",
	"naver.com", "daum.net", "hanmail.net", "fastmail.com", "fastmail.fm", "hey.com", "duck.com", "hushmail.com",
	"mailbox.org", "posteo.de", "posteo.net", "disroot.org", "riseup.net", "comcast.net", "verizon.net", "att.net",
	"sbcglobal.net", "bellsouth.net", "cox.net", "charter.net", "earthlink.net", "btinternet.com", "virginmedia.com",
	"sky.com", "talktalk.net", "orange.fr", "wanadoo.fr", "free.fr", "laposte.net", "sfr.fr", "libero.it",
	"virgilio.it", "tiscali.it", "t-online.de", "freenet.de", "seznam.cz", "wp.pl", "o2.pl", "interia.pl",
	"rediffmail.com", "uol.com.br", "bol.com.br", "terra.com.br", "shaw.ca", "rogers.com", "bigpond.com",
	"optusnet.com.au", "xtra.co.nz", "mailinator.com", "guerrillamail.com", "10minutemail.com", "temp-mail.org",
}

// reservedTLDs never receive real mail (RFC 2606, 6761, 6762). "test" is
// allowed in development, where the dev identities live on <slug>.test.
var reservedTLDs = map[string]bool{"test": true, "example": true, "invalid": true, "localhost": true, "local": true, "internal": true, "arpa": true, "onion": true}

// lookupTXT is the DNS call, swapped in tests.
var lookupTXT = func(ctx context.Context, r *net.Resolver, name string) ([]string, error) {
	return r.LookupTXT(ctx, name)
}

// newResolver uses ORGS_DNS_RESOLVER (host:port) when set, else the system
// resolver.
func newResolver(addr string) *net.Resolver {
	if addr == "" {
		return net.DefaultResolver
	}
	return &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, addr)
	}}
}

// Domain is an email domain an org claims, as its admins see it.
type Domain struct {
	ID             uuid.UUID  `json:"id"`
	OrgID          uuid.UUID  `json:"org_id"`
	Domain         string     `json:"domain"`
	Status         string     `json:"status"` // verified | pending | taken (another org verified it through DNS)
	Policy         string     `json:"policy"` // auto_join | request
	Role           string     `json:"role"`   // what people join as: member
	VerifyMethod   *string    `json:"verify_method"`
	VerifiedAt     *time.Time `json:"verified_at"`
	VerifiedByName *string    `json:"verified_by_name"`
	TXTName        string     `json:"txt_name"`  // _scaffoldapp-verify.example.com
	TXTValue       string     `json:"txt_value"` // scaffoldapp-verify=<token>
	LastCheckedAt  *time.Time `json:"last_checked_at"`
	LastCheckError *string    `json:"last_check_error"`
	AddedByName    *string    `json:"added_by_name"`
	CreatedAt      time.Time  `json:"created_at"`
}

// A domain another org holds through an admin's email is still "pending"
// here: its DNS record (proof of the domain itself) moves it to this org.
const domainSelect = `
	SELECT d.id, d.org_id, d.domain,
	       CASE WHEN d.verified_at IS NOT NULL THEN 'verified'
	            WHEN EXISTS (SELECT 1 FROM org_domains x WHERE x.domain = d.domain AND x.verified_at IS NOT NULL
	                         AND x.verify_method = 'dns') THEN 'taken'
	            ELSE 'pending' END,
	       d.policy, d.role, d.verify_method, d.verified_at, NULLIF(vb.display_name, ''), d.verify_token,
	       d.last_checked_at, d.last_check_error, NULLIF(ab.display_name, ''), d.created_at
	FROM org_domains d LEFT JOIN users vb ON vb.id = d.verified_by LEFT JOIN users ab ON ab.id = d.added_by`

func scanDomains(rows pgx.Rows) ([]Domain, error) {
	defer rows.Close()
	out := []Domain{}
	for rows.Next() {
		var (
			d     Domain
			token string
		)
		if err := rows.Scan(&d.ID, &d.OrgID, &d.Domain, &d.Status, &d.Policy, &d.Role, &d.VerifyMethod, &d.VerifiedAt,
			&d.VerifiedByName, &token, &d.LastCheckedAt, &d.LastCheckError, &d.AddedByName, &d.CreatedAt); err != nil {
			return nil, err
		}
		d.TXTName, d.TXTValue = txtName(d.Domain), verifyLabel+"="+token
		out = append(out, d)
	}
	return out, rows.Err()
}

func txtName(domain string) string { return "_" + verifyLabel + "." + domain }

func (s *Service) loadDomain(ctx context.Context, q db.Querier, orgID, id uuid.UUID) (*Domain, error) {
	rows, err := q.Query(ctx, domainSelect+` WHERE d.id = $1 AND d.org_id = $2`, id, orgID)
	if err != nil {
		return nil, err
	}
	list, err := scanDomains(rows)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, errDomainNotFound
	}
	return &list[0], nil
}

var errDomainNotFound = httpx.NotFound("Domain not found.")

// ---- validation ----------------------------------------------------------------

// cleanDomain turns what an admin typed ("@Acme.com", "https://acme.com/")
// into a lowercase ASCII domain, or explains what's wrong.
func cleanDomain(raw string) (string, error) {
	d := strings.ToLower(strings.TrimSpace(raw))
	d = strings.TrimPrefix(strings.TrimPrefix(d, "https://"), "http://")
	if i := strings.IndexAny(d, "/?#"); i >= 0 {
		d = d[:i]
	}
	if i := strings.LastIndex(d, "@"); i >= 0 {
		d = d[i+1:]
	}
	d = strings.TrimSuffix(d, ".")
	if d == "" || len(d) > 253 {
		return "", httpx.BadRequest("Enter a domain like example.com.")
	}
	labels := strings.Split(d, ".")
	if len(labels) < 2 {
		return "", httpx.BadRequest("Enter a domain like example.com.")
	}
	for _, l := range labels {
		if l == "" || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
			return "", httpx.BadRequest("Enter a domain like example.com.")
		}
		for _, c := range l {
			if c > 127 {
				return "", httpx.BadRequest("Enter the domain in ASCII. International domains use their xn-- form.")
			}
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return "", httpx.BadRequest("Enter a domain like example.com.")
			}
		}
	}
	if strings.Trim(labels[len(labels)-1], "0123456789") == "" {
		return "", httpx.BadRequest("Enter a domain name, not an IP address.")
	}
	return d, nil
}

// claimable refuses public email providers and domains that never get mail.
func (s *Service) claimable(domain string) error {
	if s.publicDomains[domain] {
		return httpx.BadRequest(domain + " is a public email provider. Add your " + Noun() + "'s own domain.")
	}
	tld := domain[strings.LastIndex(domain, ".")+1:]
	if reservedTLDs[tld] && !(tld == "test" && s.d.Cfg.IsDevelopment()) {
		return httpx.BadRequest("That domain can't receive email. Add your " + Noun() + "'s own domain.")
	}
	return nil
}

// emailDomain is the lowercase domain of a verified account email, or ""
// when it is a public provider (those never match a claim).
func (s *Service) emailDomain(email string) string {
	i := strings.LastIndex(email, "@")
	if i < 0 {
		return ""
	}
	d := strings.TrimSuffix(strings.ToLower(email[i+1:]), ".")
	if s.publicDomains[d] {
		return ""
	}
	return d
}

func newVerifyToken() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	return hex.EncodeToString(b[:])
}

// checkTXT looks for the verification record. The error is user-facing.
func (s *Service) checkTXT(ctx context.Context, domain, token string) error {
	ctx, cancel := context.WithTimeout(ctx, dnsTimeout)
	defer cancel()
	name := txtName(domain)
	want := verifyLabel + "=" + token
	records, err := lookupTXT(ctx, s.resolver, name+".")
	var dnsErr *net.DNSError
	switch {
	case err == nil:
	case errors.As(err, &dnsErr) && dnsErr.IsNotFound:
		records = nil
	case errors.Is(err, context.DeadlineExceeded) || errors.As(err, &dnsErr) && dnsErr.IsTimeout:
		return errors.New("The DNS lookup timed out. Try again in a minute.")
	default:
		slog.WarnContext(ctx, "orgs: dns lookup failed", "domain", domain, "err", err)
		return errors.New("The DNS lookup failed. Try again in a minute.")
	}
	for _, r := range records {
		if strings.EqualFold(strings.Trim(strings.TrimSpace(r), `"`), want) {
			return nil
		}
	}
	if len(records) == 0 {
		return fmt.Errorf("No TXT record at %s yet. New records can take up to an hour to appear.", name)
	}
	return fmt.Errorf("The TXT record at %s doesn't match. Set its value to %s.", name, want)
}

// ---- routes ------------------------------------------------------------------

// listDomains: GET /v1/orgs/{orgID}/domains (admin+), oldest first. An org
// has at most maxDomains, so there is one page.
func (s *Service) listDomains(w http.ResponseWriter, r *http.Request) error {
	m := mustMembership(r)
	rows, err := s.d.Pool.Query(r.Context(), domainSelect+` WHERE d.org_id = $1 ORDER BY d.created_at, d.id`, m.OrgID)
	if err != nil {
		return err
	}
	list, err := scanDomains(rows)
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.Page[Domain]{Items: list})
	return nil
}

type addDomainReq struct {
	Domain string  `json:"domain"`
	Policy *string `json:"policy"`
}

func validPolicy(p string) bool { return p == PolicyAutoJoin || p == PolicyRequest }

// addDomain: POST /v1/orgs/{orgID}/domains {domain, policy?} (admin+). When
// the admin's own verified email is on the domain it is verified at once;
// otherwise it waits for the DNS record. Another org's DNS-verified domain is
// refused; one another org holds only through an admin's email is added
// pending, and its DNS record moves it here (verifyDomain).
func (s *Service) addDomain(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	m := mustMembership(r)
	var req addDomainReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	domain, err := cleanDomain(req.Domain)
	if err != nil {
		return err
	}
	if err := s.claimable(domain); err != nil {
		return err
	}
	policy := PolicyRequest
	if req.Policy != nil {
		if !validPolicy(*req.Policy) {
			return httpx.BadRequest("policy must be auto_join or request.")
		}
		policy = *req.Policy
	}
	byEmail := s.emailDomain(me.Email) == domain
	var id uuid.UUID
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if _, err := lockOrg(ctx, tx, m.OrgID); err != nil {
			return err
		}
		var count int
		var taken, held bool
		if err := tx.QueryRow(ctx, `
			SELECT (SELECT count(*) FROM org_domains WHERE org_id = $1)::int,
			       EXISTS (SELECT 1 FROM org_domains WHERE domain = $2 AND verified_at IS NOT NULL AND org_id <> $1 AND verify_method = 'dns'),
			       EXISTS (SELECT 1 FROM org_domains WHERE domain = $2 AND verified_at IS NOT NULL AND org_id <> $1)`,
			m.OrgID, domain).Scan(&count, &taken, &held); err != nil {
			return err
		}
		if taken {
			return errDomainTaken
		}
		if held {
			byEmail = false // an email is no better proof than theirs; DNS decides
		}
		if count >= maxDomains {
			return httpx.Conflict(fmt.Sprintf("%s can have %d domains. Remove one first.", capital(NounA()), maxDomains))
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO org_domains (org_id, domain, policy, verify_token, added_by, verified_at, verify_method, verified_by)
			VALUES ($1, $2, $3, $4, $5, CASE WHEN $6 THEN now() END, CASE WHEN $6 THEN 'email' END, CASE WHEN $6 THEN $5::uuid END)
			RETURNING id`, m.OrgID, domain, policy, newVerifyToken(), me.UserID, byEmail).Scan(&id); err != nil {
			return err
		}
		return audit.Log(ctx, tx, me.UserID, "orgs.domain.add", "org_domain", id.String(),
			map[string]any{"org_id": m.OrgID, "domain": domain, "policy": policy, "verified": byEmail})
	})
	switch {
	case db.IsUniqueViolation(err, "org_domains_org_id_domain_key"):
		return httpx.Conflict("That domain is already on this " + Noun() + ".")
	case db.IsUniqueViolation(err, "org_domains_verified_idx"):
		return errDomainTaken
	case err != nil:
		return err
	}
	d, err := s.loadDomain(ctx, s.d.Pool, m.OrgID, id)
	if err != nil {
		return err
	}
	s.publish(context.WithoutCancel(ctx), orgTopic(m.OrgID), EventDomainsChanged, map[string]any{"org_id": m.OrgID})
	httpx.JSON(w, http.StatusCreated, d)
	return nil
}

var errDomainTaken = httpx.Conflict("That domain belongs to another organization.")

// verifyDomain: POST /v1/orgs/{orgID}/domains/{domainID}/verify (admin+).
// Verifies through the caller's own email when it is on the domain, else by
// looking up the TXT record (at most once per 15 seconds per domain). A
// failed check answers 422 verification_failed and is kept on the domain.
//
// The TXT record proves the domain; an email proves one mailbox on it. So
// the DNS check also upgrades a domain verified by email (auto-join on
// opening the app needs it), and moves a domain that another org holds only
// through an admin's email to this org (theirs goes back to unverified,
// audited as orgs.domain.superseded).
func (s *Service) verifyDomain(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	m := mustMembership(r)
	id, err := httpx.URLParamUUID(r, "domainID")
	if err != nil {
		return err
	}
	cur, err := s.loadDomain(ctx, s.d.Pool, m.OrgID, id)
	if err != nil {
		return err
	}
	upgrade := cur.Status == "verified" && cur.VerifyMethod != nil && *cur.VerifyMethod == "email"
	if cur.Status == "verified" && !upgrade {
		httpx.OK(w, cur)
		return nil
	}
	if cur.Status == "taken" {
		return errDomainTaken
	}
	var heldElsewhere bool
	if err := s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM org_domains WHERE domain = $1 AND org_id <> $2 AND verified_at IS NOT NULL)`,
		cur.Domain, m.OrgID).Scan(&heldElsewhere); err != nil {
		return err
	}
	method := "email"
	if upgrade || heldElsewhere || s.emailDomain(me.Email) != cur.Domain {
		method = "dns"
		// Claim the check slot atomically so parallel clicks make one lookup.
		tag, err := s.d.Pool.Exec(ctx, `UPDATE org_domains SET last_checked_at = now(), updated_at = now()
			WHERE id = $1 AND (last_checked_at IS NULL OR last_checked_at < now() - make_interval(secs => $2))`,
			id, dnsCheckCooldown.Seconds())
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.NewError(http.StatusTooManyRequests, "rate_limited", "That domain was checked a moment ago. Wait 15 seconds and check again.")
		}
		token := strings.TrimPrefix(cur.TXTValue, verifyLabel+"=")
		if checkErr := s.checkTXT(ctx, cur.Domain, token); checkErr != nil {
			if _, err := s.d.Pool.Exec(context.WithoutCancel(ctx), `UPDATE org_domains SET last_check_error = $2 WHERE id = $1`,
				id, checkErr.Error()); err != nil {
				return err
			}
			s.publish(context.WithoutCancel(ctx), orgTopic(m.OrgID), EventDomainsChanged, map[string]any{"org_id": m.OrgID})
			return httpx.NewError(http.StatusUnprocessableEntity, "verification_failed", checkErr.Error())
		}
	}
	var superseded []uuid.UUID // orgs whose email-only claim this DNS proof replaced
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if _, err := lockOrg(ctx, tx, m.OrgID); err != nil {
			return err
		}
		if method == "dns" {
			rows, err := tx.Query(ctx, `
				UPDATE org_domains SET verified_at = NULL, verify_method = NULL, verified_by = NULL, updated_at = now()
				WHERE domain = $1 AND org_id <> $2 AND verified_at IS NOT NULL AND verify_method = 'email'
				RETURNING id, org_id`, cur.Domain, m.OrgID)
			if err != nil {
				return err
			}
			type lost struct{ ID, OrgID uuid.UUID }
			list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[lost])
			if err != nil {
				return err
			}
			for _, l := range list {
				superseded = append(superseded, l.OrgID)
				if err := audit.Log(ctx, tx, me.UserID, "orgs.domain.superseded", "org_domain", l.ID.String(),
					map[string]any{"org_id": l.OrgID, "domain": cur.Domain, "by_org_id": m.OrgID}); err != nil {
					return err
				}
			}
		}
		tag, err := tx.Exec(ctx, `
			UPDATE org_domains SET verified_at = now(), verify_method = $3, verified_by = $4, last_check_error = NULL, updated_at = now()
			WHERE id = $1 AND org_id = $2 AND (verified_at IS NULL OR ($3 = 'dns' AND verify_method = 'email'))`, id, m.OrgID, method, me.UserID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return nil // verified meanwhile, or removed (loadDomain below answers)
		}
		return audit.Log(ctx, tx, me.UserID, "orgs.domain.verify", "org_domain", id.String(),
			map[string]any{"org_id": m.OrgID, "domain": cur.Domain, "method": method})
	})
	if db.IsUniqueViolation(err, "org_domains_verified_idx") {
		return httpx.Conflict("Another " + Noun() + " verified that domain first.")
	}
	if err != nil {
		return err
	}
	d, err := s.loadDomain(ctx, s.d.Pool, m.OrgID, id)
	if err != nil {
		return err
	}
	bg := context.WithoutCancel(ctx)
	s.publish(bg, orgTopic(m.OrgID), EventDomainsChanged, map[string]any{"org_id": m.OrgID})
	for _, o := range superseded {
		s.publish(bg, orgTopic(o), EventDomainsChanged, map[string]any{"org_id": o})
	}
	httpx.OK(w, d)
	return nil
}

type updateDomainReq struct {
	Policy string `json:"policy"`
}

// updateDomain: PATCH /v1/orgs/{orgID}/domains/{domainID} {policy} (admin+).
func (s *Service) updateDomain(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	m := mustMembership(r)
	id, err := httpx.URLParamUUID(r, "domainID")
	if err != nil {
		return err
	}
	var req updateDomainReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if !validPolicy(req.Policy) {
		return httpx.BadRequest("policy must be auto_join or request.")
	}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var domain, old string
		err := tx.QueryRow(ctx, `SELECT domain, policy FROM org_domains WHERE id = $1 AND org_id = $2 FOR UPDATE`, id, m.OrgID).
			Scan(&domain, &old)
		if db.IsNoRows(err) {
			return errDomainNotFound
		}
		if err != nil || old == req.Policy {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE org_domains SET policy = $2, updated_at = now() WHERE id = $1`, id, req.Policy); err != nil {
			return err
		}
		return audit.Log(ctx, tx, me.UserID, "orgs.domain.update", "org_domain", id.String(),
			map[string]any{"org_id": m.OrgID, "domain": domain, "policy": map[string]string{"from": old, "to": req.Policy}})
	})
	if err != nil {
		return err
	}
	d, err := s.loadDomain(ctx, s.d.Pool, m.OrgID, id)
	if err != nil {
		return err
	}
	s.publish(context.WithoutCancel(ctx), orgTopic(m.OrgID), EventDomainsChanged, map[string]any{"org_id": m.OrgID})
	httpx.OK(w, d)
	return nil
}

// removeDomain: DELETE /v1/orgs/{orgID}/domains/{domainID} (admin+). People
// who joined through it stay; pending requests stay for admins to answer.
func (s *Service) removeDomain(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	m := mustMembership(r)
	id, err := httpx.URLParamUUID(r, "domainID")
	if err != nil {
		return err
	}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		return deleteDomain(ctx, tx, me.UserID, m.OrgID, id, "orgs.domain.remove")
	})
	if err != nil {
		return err
	}
	s.publish(context.WithoutCancel(ctx), orgTopic(m.OrgID), EventDomainsChanged, map[string]any{"org_id": m.OrgID})
	httpx.NoContent(w)
	return nil
}

func deleteDomain(ctx context.Context, tx pgx.Tx, actor, orgID, id uuid.UUID, action string) error {
	var (
		domain   string
		verified *time.Time
	)
	err := tx.QueryRow(ctx, `DELETE FROM org_domains WHERE id = $1 AND org_id = $2 RETURNING domain, verified_at`, id, orgID).
		Scan(&domain, &verified)
	if db.IsNoRows(err) {
		return errDomainNotFound
	}
	if err != nil {
		return err
	}
	return audit.Log(ctx, tx, actor, action, "org_domain", id.String(),
		map[string]any{"org_id": orgID, "domain": domain, "was_verified": verified != nil})
}
