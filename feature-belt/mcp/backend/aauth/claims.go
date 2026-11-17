package aauth

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Token lifetime limits from the spec.
const (
	// MaxPersonTokenLifetime and MaxAuthTokenLifetime are MUSTs: "exp minus iat
	// MUST NOT exceed the one-hour ceiling of a person token or an auth token".
	MaxPersonTokenLifetime = time.Hour
	MaxAuthTokenLifetime   = time.Hour
	// MaxAgentTokenLifetime and MaxResourceTokenLifetime are SHOULD NOTs. A
	// verifier accepts longer tokens from other parties; TokenIssuer refuses to
	// create them.
	MaxAgentTokenLifetime    = 24 * time.Hour
	MaxResourceTokenLifetime = 5 * time.Minute
)

// Claims are the members common to all four token types (protocol, Common JWT
// Claims).
type Claims struct {
	Iss string `json:"iss"`
	Dwk string `json:"dwk"`
	Jti string `json:"jti"`
	Iat int64  `json:"iat"`
	Exp int64  `json:"exp"`
}

func (c *Claims) common() *Claims { return c }

type claimsHolder interface{ common() *Claims }

// Confirmation is the `cnf` claim (RFC 7800). Decoding never fails on a bad
// `jwk` member, because the spec gives that its own outcome: "reject it as
// invalid key material" rather than as a malformed token.
type Confirmation struct {
	JWK JWK `json:"jwk"`

	parseErr error
}

// ConfirmKey builds the `cnf` claim for key.
func ConfirmKey(key JWK) *Confirmation {
	key.hasPrivate = false
	return &Confirmation{JWK: key}
}

// UnmarshalJSON reads `cnf`, keeping any problem with the key for verification.
func (c *Confirmation) UnmarshalJSON(b []byte) error {
	var aux struct {
		JWK json.RawMessage `json:"jwk"`
	}
	if err := json.Unmarshal(b, &aux); err != nil {
		c.parseErr = err
		return nil
	}
	if aux.JWK == nil {
		return nil
	}
	k, err := ParseJWK(aux.JWK)
	if err != nil {
		c.parseErr = err
		return nil
	}
	c.JWK = k
	return nil
}

// Budget is the budget object of AAuth Budgets: an integer in a declared scale.
type Budget struct {
	Amount   int64  `json:"amount"`
	Unit     string `json:"unit"`
	Decimals int    `json:"decimals"`
}

// MaxBudgetAmount is the largest amount any carrier can represent: "MUST NOT
// issue a budget whose amount exceeds 999,999,999,999,999" (budgets, Range Limits).
const MaxBudgetAmount int64 = 999_999_999_999_999

// MaxBudgetDecimals is the largest scale accepted. The spec says "A non-negative integer" and names no top, and ERC-20's own common figure is 18, so nothing real goes past it. Past it a scale stops being a number anyone
// computes with: 10^19 doesn't fit an int64, and a scale an attacker chose is an exponent every party that works out a figure has to raise.
const MaxBudgetDecimals = 18

// budgetUnitRE is what a unit may be: an identifier, such as an ISO 4217 code or "tokens". The spec establishes no registry, so this says only that it isn't a sentence, since it's read out in the title a person decides under.
var budgetUnitRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,31}$`)

// Validate checks the object's members.
func (b Budget) Validate() error {
	if b.Amount < 0 || b.Amount > MaxBudgetAmount {
		return fmt.Errorf("budget amount %d is out of range", b.Amount)
	}
	if b.Unit == "" {
		return errors.New("budget has no unit")
	}
	if !budgetUnitRE.MatchString(b.Unit) {
		return errors.New("budget unit isn't an identifier")
	}
	if b.Decimals < 0 {
		return fmt.Errorf("budget decimals %d is negative", b.Decimals)
	}
	if b.Decimals > MaxBudgetDecimals {
		return fmt.Errorf("budget decimals %d is out of range", b.Decimals)
	}
	return nil
}

// BudgetConsumed is the `budget_consumed` consumption record of a resource token.
type BudgetConsumed struct {
	Jti      string `json:"jti"`
	Consumed int64  `json:"consumed"`
}

// R3Operations is the `r3_granted`, `r3_per_call` and `r3_operations` shape:
// a vocabulary URI and vocabulary-specific operations. Operations stay raw
// because each vocabulary defines their structure.
type R3Operations struct {
	Vocabulary string            `json:"vocabulary"`
	Operations []json.RawMessage `json:"operations"`
}

// ResourceInteraction is the `interaction` claim of a resource token, present
// when the resource requires its own user-facing flow.
type ResourceInteraction struct {
	URL  string `json:"url"`
	Code string `json:"code"`
}

// AgentClaims is the payload of an agent token.
type AgentClaims struct {
	Claims
	Sub         string        `json:"sub"`
	Cnf         *Confirmation `json:"cnf,omitempty"`
	PS          string        `json:"ps,omitempty"`
	ParentAgent string        `json:"parent_agent,omitempty"`
	// Extra holds claims this package does not model. "PSes and ASes MUST
	// ignore unrecognized claims"; they are kept so posture claims can be read
	// by name and survive a round trip.
	Extra map[string]json.RawMessage `json:"-"`
}

// PersonClaims is the payload of a person token. There is no scope or account
// field: "A person token MUST NOT contain scope or account."
type PersonClaims struct {
	Claims
	Aud         string                     `json:"aud"`
	Sub         string                     `json:"sub"`
	Cnf         *Confirmation              `json:"cnf,omitempty"`
	MissionS256 string                     `json:"mission_s256,omitempty"`
	Tenant      string                     `json:"tenant,omitempty"`
	Extra       map[string]json.RawMessage `json:"-"`
}

// ResourceClaims is the payload of a resource token, with the R3 and Budgets
// extensions.
type ResourceClaims struct {
	Claims
	Aud          string               `json:"aud"`
	PS           string               `json:"ps"`
	Sub          string               `json:"sub"`
	PresentedJti string               `json:"presented_jti"`
	AgentJKT     string               `json:"agent_jkt"`
	Scope        string               `json:"scope,omitempty"`
	Account      string               `json:"account,omitempty"`
	LoginHint    string               `json:"login_hint,omitempty"`
	MissionS256  string               `json:"mission_s256,omitempty"`
	Tenant       string               `json:"tenant,omitempty"`
	Interaction  *ResourceInteraction `json:"interaction,omitempty"`
	// R3 (draft-hardt-aauth-r3, Resource Token Extensions): both or neither.
	R3URI  string `json:"r3_uri,omitempty"`
	R3S256 string `json:"r3_s256,omitempty"`
	// Budgets (draft-hardt-aauth-budgets, Resource Token Extensions).
	Budget         *Budget                    `json:"budget,omitempty"`
	BudgetConsumed *BudgetConsumed            `json:"budget_consumed,omitempty"`
	Extra          map[string]json.RawMessage `json:"-"`
}

// AuthClaims is the payload of an auth token, with the R3 and Budgets extensions.
type AuthClaims struct {
	Claims
	Aud         string        `json:"aud"`
	PS          string        `json:"ps"`
	Sub         string        `json:"sub"`
	Cnf         *Confirmation `json:"cnf,omitempty"`
	Scope       string        `json:"scope,omitempty"`
	Account     string        `json:"account,omitempty"`
	MissionS256 string        `json:"mission_s256,omitempty"`
	Tenant      string        `json:"tenant,omitempty"`
	// R3 (draft-hardt-aauth-r3, Auth Token Extensions).
	R3URI     string        `json:"r3_uri,omitempty"`
	R3S256    string        `json:"r3_s256,omitempty"`
	R3Granted *R3Operations `json:"r3_granted,omitempty"`
	R3PerCall *R3Operations `json:"r3_per_call,omitempty"`
	// Budgets (draft-hardt-aauth-budgets, Auth Token Extensions).
	Budget *Budget `json:"budget,omitempty"`
	// Extra holds further claims, typically identity claims (email, name) when
	// identity scopes were granted.
	Extra map[string]json.RawMessage `json:"-"`
}

// MarshalJSON includes Extra. Modeled members win over a same-named Extra entry.
func (c AgentClaims) MarshalJSON() ([]byte, error) {
	type plain AgentClaims
	return marshalWithExtra(plain(c), c.Extra)
}

// UnmarshalJSON collects unknown claims into Extra.
func (c *AgentClaims) UnmarshalJSON(b []byte) error {
	type plain AgentClaims
	var p plain
	extra, err := unmarshalWithExtra(b, &p)
	if err != nil {
		return err
	}
	*c = AgentClaims(p)
	c.Extra = extra
	return nil
}

// MarshalJSON includes Extra. Modeled members win over a same-named Extra entry.
func (c PersonClaims) MarshalJSON() ([]byte, error) {
	type plain PersonClaims
	return marshalWithExtra(plain(c), c.Extra)
}

// UnmarshalJSON collects unknown claims into Extra.
func (c *PersonClaims) UnmarshalJSON(b []byte) error {
	type plain PersonClaims
	var p plain
	extra, err := unmarshalWithExtra(b, &p)
	if err != nil {
		return err
	}
	*c = PersonClaims(p)
	c.Extra = extra
	return nil
}

// MarshalJSON includes Extra. Modeled members win over a same-named Extra entry.
func (c ResourceClaims) MarshalJSON() ([]byte, error) {
	type plain ResourceClaims
	return marshalWithExtra(plain(c), c.Extra)
}

// UnmarshalJSON collects unknown claims into Extra.
func (c *ResourceClaims) UnmarshalJSON(b []byte) error {
	type plain ResourceClaims
	var p plain
	extra, err := unmarshalWithExtra(b, &p)
	if err != nil {
		return err
	}
	*c = ResourceClaims(p)
	c.Extra = extra
	return nil
}

// MarshalJSON includes Extra. Modeled members win over a same-named Extra entry.
func (c AuthClaims) MarshalJSON() ([]byte, error) {
	type plain AuthClaims
	return marshalWithExtra(plain(c), c.Extra)
}

// UnmarshalJSON collects unknown claims into Extra.
func (c *AuthClaims) UnmarshalJSON(b []byte) error {
	type plain AuthClaims
	var p plain
	extra, err := unmarshalWithExtra(b, &p)
	if err != nil {
		return err
	}
	*c = AuthClaims(p)
	c.Extra = extra
	return nil
}

// marshalWithExtra serializes known, then appends Extra members it does not
// already contain, in sorted order, so output is deterministic.
func marshalWithExtra(known any, extra map[string]json.RawMessage) ([]byte, error) {
	b, err := json.Marshal(known)
	if err != nil {
		return nil, err
	}
	if len(extra) == 0 {
		return b, nil
	}
	names := jsonFieldNames(reflect.TypeOf(known))
	keys := make([]string, 0, len(extra))
	for k := range extra {
		if !names[k] {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return b, nil
	}
	sortStrings(keys)
	var out bytes.Buffer
	out.Write(b[:len(b)-1]) // drop the closing brace
	first := len(b) == 2    // known serialized to {}
	for _, k := range keys {
		if !first {
			out.WriteByte(',')
		}
		first = false
		name, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		out.Write(name)
		out.WriteByte(':')
		out.Write(extra[k])
	}
	out.WriteByte('}')
	return out.Bytes(), nil
}

// unmarshalWithExtra decodes b into known and returns the members it does not
// model. iat and exp may be fractional per RFC 7519's NumericDate; they are
// truncated to whole seconds, which never extends a token's life.
func unmarshalWithExtra(b []byte, known any) (map[string]json.RawMessage, error) {
	var all map[string]json.RawMessage
	if err := json.Unmarshal(b, &all); err != nil {
		return nil, err
	}
	if all == nil {
		return nil, errors.New("not a JSON object")
	}
	changed := false
	for _, name := range []string{"iat", "exp"} {
		if raw, ok := all[name]; ok && bytes.ContainsAny(raw, ".eE") {
			var f float64
			if err := json.Unmarshal(raw, &f); err != nil {
				return nil, err
			}
			all[name] = json.RawMessage(fmt.Sprintf("%d", int64(f)))
			changed = true
		}
	}
	if changed {
		rebuilt, err := json.Marshal(all)
		if err != nil {
			return nil, err
		}
		b = rebuilt
	}
	if err := json.Unmarshal(b, known); err != nil {
		return nil, err
	}
	names := jsonFieldNames(reflect.TypeOf(known))
	var extra map[string]json.RawMessage
	for k, v := range all {
		if !names[k] {
			if extra == nil {
				extra = map[string]json.RawMessage{}
			}
			extra[k] = v
		}
	}
	return extra, nil
}

var jsonNamesCache sync.Map // reflect.Type -> map[string]bool

// jsonFieldNames lists the JSON member names of a struct type, descending into
// embedded structs the way encoding/json does.
func jsonFieldNames(t reflect.Type) map[string]bool {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if v, ok := jsonNamesCache.Load(t); ok {
		return v.(map[string]bool)
	}
	names := map[string]bool{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if tag == "-" {
			continue
		}
		if f.Anonymous && tag == "" && f.Type.Kind() == reflect.Struct {
			for k := range jsonFieldNames(f.Type) {
				names[k] = true
			}
			continue
		}
		if !f.IsExported() {
			continue
		}
		if tag == "" {
			tag = f.Name
		}
		names[tag] = true
	}
	jsonNamesCache.Store(t, names)
	return names
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

// TokenIssuer signs tokens after checking the claims the spec requires. Every
// method fills `dwk` (except Auth, whose dwk depends on the issuer's role),
// validates the required members and the lifetime, and returns the compact JWT.
// Claims must carry Iss, Jti, Iat and Exp; use NewJTI for the jti.
type TokenIssuer struct {
	Key    Signer
	Policy IdentifierPolicy
}

func (ti TokenIssuer) checkCommon(c *Claims, maxLife time.Duration) error {
	if ti.Key == nil {
		return errors.New("aauth: TokenIssuer has no key")
	}
	if err := ti.Policy.ValidateServerIdentifier(c.Iss); err != nil {
		return fmt.Errorf("iss: %w", err)
	}
	if c.Jti == "" {
		return errors.New("aauth: token has no jti")
	}
	if c.Iat <= 0 {
		return errors.New("aauth: token has no iat")
	}
	if c.Exp <= c.Iat {
		return errors.New("aauth: token exp is not after iat")
	}
	if c.Exp-c.Iat > int64(maxLife/time.Second) {
		return fmt.Errorf("aauth: token lifetime exceeds %s", maxLife)
	}
	return nil
}

func checkCnf(c *Confirmation) error {
	if c == nil {
		return errors.New("aauth: token has no cnf.jwk")
	}
	if err := c.JWK.Validate(); err != nil {
		return fmt.Errorf("cnf.jwk: %w", err)
	}
	return nil
}

func validMissionS256(s string) bool {
	if len(s) != 43 {
		return false
	}
	b, err := b64url.DecodeString(s)
	return err == nil && len(b) == 32
}

// Agent signs an agent token (`typ: aa-agent+jwt`, `dwk: aauth-agent.json`).
func (ti TokenIssuer) Agent(c AgentClaims) (string, error) {
	c.Dwk = DwkAgent
	if err := ti.checkCommon(&c.Claims, MaxAgentTokenLifetime); err != nil {
		return "", err
	}
	if c.ParentAgent == "" {
		if err := ti.Policy.ValidateTopLevelAgentIdentifier(c.Sub); err != nil {
			return "", fmt.Errorf("sub: %w", err)
		}
	} else if err := ti.Policy.ValidateSubAgentIdentifier(c.Sub, c.ParentAgent); err != nil {
		return "", fmt.Errorf("sub: %w", err)
	}
	if !ti.Policy.AgentBelongsToIssuer(c.Sub, c.Iss) {
		return "", fmt.Errorf("aauth: sub %q is not under issuer %q", c.Sub, c.Iss)
	}
	if err := checkCnf(c.Cnf); err != nil {
		return "", err
	}
	if c.PS != "" {
		if err := ti.Policy.ValidateServerIdentifier(c.PS); err != nil {
			return "", fmt.Errorf("ps: %w", err)
		}
	}
	return SignJWT(ti.Key, TypAgent, c)
}

// Person signs a person token (`typ: aa-person+jwt`, `dwk: aauth-person.json`).
func (ti TokenIssuer) Person(c PersonClaims) (string, error) {
	c.Dwk = DwkPerson
	if err := ti.checkCommon(&c.Claims, MaxPersonTokenLifetime); err != nil {
		return "", err
	}
	if err := ti.Policy.ValidateServerIdentifier(c.Aud); err != nil {
		return "", fmt.Errorf("aud: %w", err)
	}
	if c.Sub == "" {
		return "", errors.New("aauth: person token has no sub")
	}
	if err := checkCnf(c.Cnf); err != nil {
		return "", err
	}
	if c.MissionS256 != "" && !validMissionS256(c.MissionS256) {
		return "", errors.New("aauth: mission_s256 is malformed")
	}
	for _, forbidden := range []string{"scope", "account"} {
		if _, ok := c.Extra[forbidden]; ok {
			return "", fmt.Errorf("aauth: a person token must not contain %s", forbidden)
		}
	}
	return SignJWT(ti.Key, TypPerson, c)
}

// Resource signs a resource token (`typ: aa-resource+jwt`,
// `dwk: aauth-resource.json`). Used by the testbed resource.
func (ti TokenIssuer) Resource(c ResourceClaims) (string, error) {
	c.Dwk = DwkResource
	if err := ti.checkCommon(&c.Claims, MaxResourceTokenLifetime); err != nil {
		return "", err
	}
	if err := ti.Policy.ValidateServerIdentifier(c.Aud); err != nil {
		return "", fmt.Errorf("aud: %w", err)
	}
	if err := ti.Policy.ValidateServerIdentifier(c.PS); err != nil {
		return "", fmt.Errorf("ps: %w", err)
	}
	if c.Sub == "" || c.PresentedJti == "" || c.AgentJKT == "" {
		return "", errors.New("aauth: resource token needs sub, presented_jti and agent_jkt")
	}
	if err := checkResourceExtensions(&c); err != nil {
		return "", err
	}
	return SignJWT(ti.Key, TypResource, c)
}

// Auth signs an auth token (`typ: aa-auth+jwt`). c.Dwk must be DwkPerson when a
// PS issues it, DwkAccess when an AS does; a PS-issued token has `ps` equal to
// `iss`.
func (ti TokenIssuer) Auth(c AuthClaims) (string, error) {
	if c.Dwk != DwkPerson && c.Dwk != DwkAccess {
		return "", fmt.Errorf("aauth: auth token dwk must be %s or %s", DwkPerson, DwkAccess)
	}
	if err := ti.checkCommon(&c.Claims, MaxAuthTokenLifetime); err != nil {
		return "", err
	}
	if err := ti.Policy.ValidateServerIdentifier(c.Aud); err != nil {
		return "", fmt.Errorf("aud: %w", err)
	}
	if err := ti.Policy.ValidateServerIdentifier(c.PS); err != nil {
		return "", fmt.Errorf("ps: %w", err)
	}
	if c.Dwk == DwkPerson && c.PS != c.Iss {
		return "", errors.New("aauth: a PS-issued auth token has ps equal to iss")
	}
	if c.Sub == "" {
		return "", errors.New("aauth: auth token has no sub")
	}
	if err := checkCnf(c.Cnf); err != nil {
		return "", err
	}
	if err := checkAuthExtensions(&c); err != nil {
		return "", err
	}
	return SignJWT(ti.Key, TypAuth, c)
}

// errMalformedBudget marks a resource token whose budget object fails its rules, which
// has its own error code.
var errMalformedBudget = errors.New("budget is malformed")

// checkResourceExtensions applies the structural rules of a resource token's
// optional claims, for issuing and verifying alike.
func checkResourceExtensions(c *ResourceClaims) error {
	if c.MissionS256 != "" && !validMissionS256(c.MissionS256) {
		return errors.New("mission_s256 is malformed")
	}
	if (c.R3URI == "") != (c.R3S256 == "") {
		return errors.New("r3_uri and r3_s256 must appear together")
	}
	if c.Budget != nil {
		if err := c.Budget.Validate(); err != nil {
			return fmt.Errorf("%w: %v", errMalformedBudget, err)
		}
	}
	if c.BudgetConsumed != nil {
		// "A resource MUST NOT include budget_consumed unless budget is present
		// in the same token."
		if c.Budget == nil {
			return errors.New("budget_consumed requires budget")
		}
		if c.BudgetConsumed.Consumed < 0 || c.BudgetConsumed.Jti == "" {
			return errors.New("budget_consumed is malformed")
		}
	}
	if c.Interaction != nil {
		if c.Interaction.URL == "" || c.Interaction.Code == "" {
			return errors.New("interaction claim needs url and code")
		}
	}
	return nil
}

// checkAuthExtensions applies the structural rules of an auth token's optional
// claims, for issuing and verifying alike.
func checkAuthExtensions(c *AuthClaims) error {
	if c.MissionS256 != "" && !validMissionS256(c.MissionS256) {
		return errors.New("mission_s256 is malformed")
	}
	anyR3 := c.R3URI != "" || c.R3S256 != "" || c.R3Granted != nil || c.R3PerCall != nil
	if anyR3 && (c.R3URI == "" || c.R3S256 == "" || c.R3Granted == nil) {
		return errors.New("r3_uri, r3_s256 and r3_granted are all required for R3")
	}
	if c.Budget != nil {
		if err := c.Budget.Validate(); err != nil {
			return err
		}
	}
	return nil
}
