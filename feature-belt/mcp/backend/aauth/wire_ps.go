package aauth

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

func invalidRequest(format string, args ...any) error {
	return NewProblem(CodeInvalidRequest, fmt.Sprintf(format, args...))
}

// The platform values of the AAuth Platform Value Registry.
const (
	PlatformWeb        = "web"
	PlatformMobile     = "mobile"
	PlatformDesktop    = "desktop"
	PlatformWorkload   = "workload"
	PlatformSelfHosted = "self-hosted"
)

// TokenRequestCommon holds the OPTIONAL parameters the two PS token endpoints share
// (protocol, Auth Token Request). Everything here except the tokens is agent-asserted.
type TokenRequestCommon struct {
	UpstreamToken string   `json:"upstream_token,omitempty"`
	SubagentToken string   `json:"subagent_token,omitempty"`
	Justification string   `json:"justification,omitempty"`
	LoginHint     string   `json:"login_hint,omitempty"`
	Tenant        string   `json:"tenant,omitempty"`
	DomainHint    string   `json:"domain_hint,omitempty"`
	Prompt        string   `json:"prompt,omitempty"`
	Platform      string   `json:"platform,omitempty"`
	Device        string   `json:"device,omitempty"`
	Capabilities  []string `json:"capabilities,omitempty"`
}

func (c *TokenRequestCommon) validate() error {
	switch c.Platform {
	case "", PlatformWeb, PlatformMobile, PlatformDesktop, PlatformWorkload, PlatformSelfHosted:
	default:
		return invalidRequest("platform %q is not in the registry", c.Platform)
	}
	if c.Device != "" {
		if !utf8.ValidString(c.Device) || utf8.RuneCountInString(c.Device) > 64 {
			return invalidRequest("device must be at most 64 UTF-8 characters")
		}
		for _, r := range c.Device {
			if !unicode.IsPrint(r) {
				return invalidRequest("device must contain printable characters only")
			}
		}
	}
	return nil
}

// PersonTokenRequest is the body of a POST to person_token_endpoint.
type PersonTokenRequest struct {
	Resource    string `json:"resource"`
	MissionS256 string `json:"mission_s256,omitempty"`
	TokenRequestCommon
}

// Validate applies "invalid_request covers a missing or malformed resource or
// mission_s256" and the parameter rules. The result is a *Problem.
func (q *PersonTokenRequest) Validate(p IdentifierPolicy) error {
	if q.Resource == "" {
		return invalidRequest("resource is required")
	}
	if err := p.ValidateServerIdentifier(q.Resource); err != nil {
		return invalidRequest("resource is not a server identifier")
	}
	if q.MissionS256 != "" && !validMissionS256(q.MissionS256) {
		return invalidRequest("mission_s256 is malformed")
	}
	if q.MissionS256 != "" && q.UpstreamToken != "" {
		return invalidRequest("mission_s256 is not sent with upstream_token")
	}
	return q.validate()
}

// PersonTokenResponse is the 200 body from person_token_endpoint.
type PersonTokenResponse struct {
	PersonToken string `json:"person_token"`
	ExpiresIn   int64  `json:"expires_in"`
}

// AuthTokenRequest is the body of a POST to auth_token_endpoint.
type AuthTokenRequest struct {
	ResourceToken  string `json:"resource_token"`
	PresentedToken string `json:"presented_token"`
	TokenRequestCommon
}

// Validate checks required members. The result is a *Problem.
func (q *AuthTokenRequest) Validate() error {
	if q.ResourceToken == "" {
		return invalidRequest("resource_token is required")
	}
	if q.PresentedToken == "" {
		return invalidRequest("presented_token is required")
	}
	return q.validate()
}

// AuthTokenResponse is the 200 body from an auth token endpoint.
type AuthTokenResponse struct {
	AuthToken string `json:"auth_token"`
	ExpiresIn int64  `json:"expires_in"`
}

// ASTokenRequest is the PS-to-AS token request (protocol, PS-to-AS Token Request; budgets,
// PS-to-AS Token Request Extensions).
type ASTokenRequest struct {
	ResourceToken  string  `json:"resource_token"`
	AgentToken     string  `json:"agent_token"`
	PresentedToken string  `json:"presented_token"`
	SubagentToken  string  `json:"subagent_token,omitempty"`
	UpstreamToken  string  `json:"upstream_token,omitempty"`
	Budget         *Budget `json:"budget,omitempty"`
}

// AuthorizationRequest is the body of a POST to a resource's authorization_endpoint, with
// the R3 and Budgets parameters. The testbed resource reads it.
type AuthorizationRequest struct {
	Scope        string        `json:"scope,omitempty"`
	Account      string        `json:"account,omitempty"`
	R3Operations *R3Operations `json:"r3_operations,omitempty"`
	Budget       *Budget       `json:"budget,omitempty"`
}

// ResourceTokenResponse is an authorization endpoint's answer with a resource token.
type ResourceTokenResponse struct {
	ResourceToken string `json:"resource_token"`
}

// ClarificationBody is the body of a 202 with requirement=clarification.
type ClarificationBody struct {
	Status        string   `json:"status"`
	Clarification string   `json:"clarification"`
	Timeout       int      `json:"timeout,omitempty"`
	Options       []string `json:"options,omitempty"`
}

// ClaimsRequiredBody is the body of a 202 with requirement=claims.
type ClaimsRequiredBody struct {
	Status         string   `json:"status"`
	RequiredClaims []string `json:"required_claims"`
}

// The `action` values of a POST to a pending URL.
const (
	PendingActionClarificationResponse = "clarification_response"
	PendingActionUpdatedRequest        = "updated_request"
)

// PendingAction is the body of a POST to a pending URL (protocol, Agent Response to
// Clarification).
type PendingAction struct {
	Action                string `json:"action"`
	ClarificationResponse string `json:"clarification_response,omitempty"`
	ResourceToken         string `json:"resource_token,omitempty"`
	PresentedToken        string `json:"presented_token,omitempty"`
	Justification         string `json:"justification,omitempty"`
}

// Validate applies "A server MUST reject a POST with a missing or unrecognized action with
// 400 Bad Request", and "presented_token is REQUIRED" on updated_request. The result is a
// *Problem.
func (a *PendingAction) Validate() error {
	switch a.Action {
	case PendingActionClarificationResponse:
		if a.ClarificationResponse == "" {
			return invalidRequest("clarification_response is required")
		}
	case PendingActionUpdatedRequest:
		if a.ResourceToken == "" || a.PresentedToken == "" {
			return invalidRequest("resource_token and presented_token are required")
		}
	default:
		return invalidRequest("action is missing or unrecognized")
	}
	return nil
}

// The mission states and termination reasons (protocol, Mission Management). The reason
// set is open: "a recipient that does not recognize a reason MUST retain the terminated
// state and treat the reason as an opaque audit value."
const (
	MissionStatusActive     = "active"
	MissionStatusTerminated = "terminated"

	TerminationCompleted      = "completed"
	TerminationRevoked        = "revoked"
	TerminationExpired        = "expired"
	TerminationSuperseded     = "superseded"
	TerminationAdministrative = "administrative"
)

// Tool is an entry of `tools` and `approved_tools`.
type Tool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// MissionProposal is the body of a POST to mission_endpoint.
type MissionProposal struct {
	Description string   `json:"description"`
	Tools       []Tool   `json:"tools,omitempty"`
	Resources   []string `json:"resources,omitempty"`
}

// Validate checks required members and that resources are server identifiers. The result
// is a *Problem.
func (m *MissionProposal) Validate(p IdentifierPolicy) error {
	if m.Description == "" {
		return invalidRequest("description is required")
	}
	for _, r := range m.Resources {
		if err := p.ValidateServerIdentifier(r); err != nil {
			return invalidRequest("resources must be server identifiers")
		}
	}
	return nil
}

// MissionApproval is the 200 body when a mission is approved. Capabilities "are neither part
// of the blob nor covered by the digest".
type MissionApproval struct {
	S256         string            `json:"s256"`
	Mission      string            `json:"mission"`
	Capabilities []string          `json:"capabilities,omitempty"`
	PersonTokens map[string]string `json:"person_tokens,omitempty"`
}

// MissionBlobInput is what a mission blob is built from.
type MissionBlobInput struct {
	Agent             string
	ApprovedAt        time.Time
	ExpiresAt         time.Time // zero means none
	Description       string
	ApprovedTools     []Tool
	ApprovedResources []string
	// Extra holds additional members: "A PS MAY include additional members".
	Extra map[string]json.RawMessage
}

// MissionBlob is a decoded mission blob. Timestamps stay strings so it reads a blob exactly
// as the PS wrote it. "a reader MUST ignore members it does not recognize"; they are kept in
// Extra.
type MissionBlob struct {
	Agent             string                     `json:"agent"`
	ApprovedAt        string                     `json:"approved_at"`
	ExpiresAt         string                     `json:"expires_at,omitempty"`
	Description       string                     `json:"description"`
	ApprovedTools     []Tool                     `json:"approved_tools,omitempty"`
	ApprovedResources []string                   `json:"approved_resources,omitempty"`
	Extra             map[string]json.RawMessage `json:"-"`
}

// BuiltMission is the output of BuildMissionBlob. Persist Bytes exactly: "The PS MUST compute
// s256 over the exact bytes it persists as the mission blob, MUST return those same bytes as
// mission, and MUST serve them wherever it later exposes the mission for audit."
type BuiltMission struct {
	Bytes   []byte // the blob, to persist as bytea
	S256    string // the mission identifier
	Encoded string // the `mission` member: Bytes in unpadded base64url
}

// BuildMissionBlob serializes a mission blob and computes its identifier.
func BuildMissionBlob(in MissionBlobInput, p IdentifierPolicy) (*BuiltMission, error) {
	if err := p.ValidateAgentIdentifier(in.Agent); err != nil {
		return nil, fmt.Errorf("agent: %w", err)
	}
	if in.ApprovedAt.IsZero() {
		return nil, errors.New("aauth: a mission blob needs approved_at")
	}
	if in.Description == "" {
		return nil, errors.New("aauth: a mission blob needs a description")
	}
	blob := MissionBlob{
		Agent:             in.Agent,
		ApprovedAt:        FormatISOTime(in.ApprovedAt),
		Description:       in.Description,
		ApprovedTools:     in.ApprovedTools,
		ApprovedResources: in.ApprovedResources,
		Extra:             in.Extra,
	}
	if !in.ExpiresAt.IsZero() {
		blob.ExpiresAt = FormatISOTime(in.ExpiresAt)
	}
	raw, err := json.Marshal(blob)
	if err != nil {
		return nil, err
	}
	return &BuiltMission{Bytes: raw, S256: S256(raw), Encoded: Base64URL(raw)}, nil
}

// MarshalJSON includes Extra. Modeled members win over a same-named Extra entry.
func (m MissionBlob) MarshalJSON() ([]byte, error) {
	type plain MissionBlob
	return marshalWithExtra(plain(m), m.Extra)
}

// UnmarshalJSON collects unknown members into Extra.
func (m *MissionBlob) UnmarshalJSON(b []byte) error {
	type plain MissionBlob
	var p plain
	extra, err := unmarshalWithExtra(b, &p)
	if err != nil {
		return err
	}
	*m = MissionBlob(p)
	m.Extra = extra
	return nil
}

// ParseMissionBlob reads a blob, checking its required members.
func ParseMissionBlob(raw []byte) (*MissionBlob, error) {
	var m MissionBlob
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("parse mission blob: %w", err)
	}
	if m.Agent == "" || m.ApprovedAt == "" || m.Description == "" {
		return nil, errors.New("parse mission blob: agent, approved_at and description are required")
	}
	return &m, nil
}

// DecodeMission decodes a `mission` member and checks it against s256, the check an agent or
// an SS makes: "verify that s256 is the digest of mission".
func DecodeMission(encoded, s256 string) ([]byte, error) {
	raw, err := b64url.DecodeString(encoded)
	if err != nil {
		return nil, errors.New("mission is not unpadded base64url")
	}
	if S256(raw) != s256 {
		return nil, errors.New("s256 is not the digest of mission")
	}
	return raw, nil
}

// ExpiresTime returns the blob's expires_at. ok is false when it has none.
func (m *MissionBlob) ExpiresTime() (t time.Time, ok bool, err error) {
	if m.ExpiresAt == "" {
		return time.Time{}, false, nil
	}
	t, err = ParseISOTime(m.ExpiresAt)
	return t, err == nil, err
}

// FormatISOTime formats an ISO 8601 timestamp in UTC to the second, as in the spec's examples.
func FormatISOTime(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05Z") }

// ParseISOTime reads an ISO 8601 timestamp with an offset or Z.
func ParseISOTime(s string) (time.Time, error) { return time.Parse(time.RFC3339, s) }

// S256 returns the unpadded base64url SHA-256 digest of b, the form of `s256` and `r3_s256`.
func S256(b []byte) string {
	sum := sha256.Sum256(b)
	return Base64URL(sum[:])
}

// ValidMissionS256 reports whether s has the form of a mission identifier, for the path
// segment of a mission URL. A bad one is invalid_request.
func ValidMissionS256(s string) bool { return validMissionS256(s) }

// The `action` values of a POST to a mission's own URL at the PS. The supervision draft adds
// approved and terminated at the SS.
const (
	MissionActionUpdate     = "update"
	MissionActionCompletion = "completion"
	MissionActionApproved   = "approved"
	MissionActionTerminated = "terminated"
)

// MissionActionRequest is the body of a POST to {mission_endpoint}/{mission_s256}.
type MissionActionRequest struct {
	Action      string `json:"action"`
	Description string `json:"description,omitempty"` // update
	Summary     string `json:"summary,omitempty"`     // completion
}

// Validate applies "a PS MUST reject a request with a missing or unrecognized action with 400
// Bad Request" and each action's required member. The result is a *Problem.
func (q *MissionActionRequest) Validate() error {
	switch q.Action {
	case MissionActionUpdate:
		if q.Description == "" {
			return invalidRequest("description is required")
		}
	case MissionActionCompletion:
		if q.Summary == "" {
			return invalidRequest("summary is required")
		}
	default:
		return invalidRequest("action is missing or unrecognized")
	}
	return nil
}

// MissionUpdateResponse answers an accepted update with the digest of its persisted bytes.
type MissionUpdateResponse struct {
	S256 string `json:"s256"`
}

// The `permission` values of a permission response.
const (
	PermissionGranted = "granted"
	PermissionDenied  = "denied"
)

// PermissionRequest is the body of a POST to permission_endpoint.
type PermissionRequest struct {
	Action      string          `json:"action"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	MissionS256 string          `json:"mission_s256,omitempty"`
}

// Validate checks required members. The result is a *Problem.
func (q *PermissionRequest) Validate() error {
	if q.Action == "" {
		return invalidRequest("action is required")
	}
	if q.MissionS256 != "" && !validMissionS256(q.MissionS256) {
		return invalidRequest("mission_s256 is malformed")
	}
	return validateJSONObject("parameters", q.Parameters)
}

// PermissionResponse is the 200 body from permission_endpoint.
type PermissionResponse struct {
	Permission string `json:"permission"`
	Reason     string `json:"reason,omitempty"`
}

// AuditRequest is the body of a POST to audit_endpoint: "It requires a mission".
type AuditRequest struct {
	MissionS256 string          `json:"mission_s256"`
	Action      string          `json:"action"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Result      json.RawMessage `json:"result,omitempty"`
}

// Validate checks required members. The result is a *Problem.
func (q *AuditRequest) Validate() error {
	if !validMissionS256(q.MissionS256) {
		return invalidRequest("mission_s256 is required")
	}
	if q.Action == "" {
		return invalidRequest("action is required")
	}
	if err := validateJSONObject("parameters", q.Parameters); err != nil {
		return err
	}
	return validateJSONObject("result", q.Result)
}

func validateJSONObject(name string, raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	if t := bytes.TrimSpace(raw); len(t) == 0 || t[0] != '{' {
		return invalidRequest("%s must be a JSON object", name)
	}
	return nil
}

// The `type` values of an interaction request.
const (
	InteractionTypeInteraction = "interaction"
	InteractionTypePayment     = "payment"
	InteractionTypeQuestion    = "question"
)

// InteractionRequest is the body of a POST to interaction_endpoint.
type InteractionRequest struct {
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	URL         string `json:"url,omitempty"`
	Code        string `json:"code,omitempty"`
	MaxWait     int    `json:"max_wait,omitempty"`
	Question    string `json:"question,omitempty"`
	MissionS256 string `json:"mission_s256,omitempty"`
}

// Validate checks `type` and the members each type uses. A relayed URL takes the rule of an
// interaction URL: https with no query or fragment. The result is a *Problem.
func (q *InteractionRequest) Validate(p IdentifierPolicy) error {
	if q.URL != "" {
		if err := p.ValidateEndpointURL(q.URL); err != nil {
			return invalidRequest("url must be https with no query or fragment")
		}
	}
	// "MUST validate all values before processing." A code goes in a URL the person is sent to, and "Every symbol is URL-safe".
	if q.Code != "" && !relayCodeRE.MatchString(q.Code) {
		return invalidRequest("code must be symbols and hyphens only")
	}
	switch q.Type {
	case InteractionTypeInteraction, InteractionTypePayment:
	case InteractionTypeQuestion:
		if q.Question == "" {
			return invalidRequest("question is required")
		}
	default:
		return invalidRequest("type must be interaction, payment or question")
	}
	if q.MissionS256 != "" && !validMissionS256(q.MissionS256) {
		return invalidRequest("mission_s256 is malformed")
	}
	return nil
}

// relayCodeRE is what a relayed code may be. A resource's code is the resource's to generate, so this is wider than the Crockford set a server must emit: it holds the URL-safe part, and the hyphen a code may be grouped with.
var relayCodeRE = regexp.MustCompile(`^[0-9A-Za-z-]{1,128}$`)

// InteractionAnswer is the 200 body for a question.
type InteractionAnswer struct {
	Answer string `json:"answer"`
}

// RevocationRequest is the body of a POST to a revocation_endpoint. "The issuer is not a
// request parameter. The recipient takes it from the identity it verified on the signature".
type RevocationRequest struct {
	Jti string `json:"jti"`
	Exp int64  `json:"exp"`
}

// Validate applies "Malformed JSON, or a missing or malformed jti or exp". The result is a
// *Problem.
func (q *RevocationRequest) Validate() error {
	if q.Jti == "" {
		return invalidRequest("jti is required")
	}
	if q.Exp <= 0 {
		return invalidRequest("exp is required")
	}
	return nil
}

// DownstreamRevocation is one recipient's outcome in a revocation response. Error is empty when
// the revocation there succeeded.
type DownstreamRevocation struct {
	Recipient string `json:"recipient"`
	Error     string `json:"error,omitempty"`
}

// RevocationResponse is the JSON body of a 200 from a revocation endpoint.
type RevocationResponse struct {
	Downstream []DownstreamRevocation `json:"downstream,omitempty"`
}

// ParseRevocationResponse applies the caller's rule for a 200: "A caller reads an absent body as
// the revocation recorded with nothing reported, and a non-empty body it cannot parse as
// revocation_unavailable." ok is false in the second case.
func ParseRevocationResponse(body []byte) (resp RevocationResponse, ok bool) {
	if len(bytes.TrimSpace(body)) == 0 {
		return RevocationResponse{}, true
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal(body, &raw) != nil {
		return RevocationResponse{}, false
	}
	if json.Unmarshal(body, &resp) != nil {
		return RevocationResponse{}, false
	}
	return resp, true
}

// R3Display is the `display` section of an R3 document: resource-asserted content.
type R3Display struct {
	Summary      string `json:"summary"`
	Implications string `json:"implications,omitempty"`
	DataAccessed string `json:"data_accessed,omitempty"`
	Irreversible string `json:"irreversible,omitempty"`
	Detail       string `json:"detail,omitempty"` // per-call proposals
}

// R3Document is an R3 document or per-call proposal. Parameters and Result are present only on
// a per-call proposal. Hash the served bytes rather than this struct: "verifiers hash the bytes
// received over the wire, not a normalized form".
type R3Document struct {
	Vocabulary string            `json:"vocabulary"`
	Operations []json.RawMessage `json:"operations"`
	Account    string            `json:"account,omitempty"`
	Display    *R3Display        `json:"display,omitempty"`
	Parameters json.RawMessage   `json:"parameters,omitempty"`
	Result     json.RawMessage   `json:"result,omitempty"`
}

// ParseR3Document checks raw against r3S256 and decodes it: "If the hashes do not match, the
// issuer MUST reject the resource token."
func ParseR3Document(raw []byte, r3S256 string) (*R3Document, error) {
	if S256(raw) != r3S256 {
		return nil, errors.New("aauth: R3 document does not match r3_s256")
	}
	var d R3Document
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("aauth: parse R3 document: %w", err)
	}
	if d.Vocabulary == "" || d.Operations == nil {
		return nil, errors.New("aauth: R3 document needs vocabulary and operations")
	}
	return &d, nil
}

// CoversOperations reports whether every operation in ops appears in the document, under the
// same vocabulary. Operations compare by JSON value, so member order doesn't matter.
func (d *R3Document) CoversOperations(ops *R3Operations) bool {
	if ops.Vocabulary != d.Vocabulary {
		return false
	}
	have := map[string]bool{}
	for _, o := range d.Operations {
		have[canonicalJSON(o)] = true
	}
	for _, o := range ops.Operations {
		if !have[canonicalJSON(o)] {
			return false
		}
	}
	return true
}

func canonicalJSON(raw json.RawMessage) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return string(raw)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return string(raw)
	}
	return string(b)
}

// UsageRequest is the body of a signed POST to a resource's usage_endpoint (budgets, Usage
// Request). At most one of Sub, Tenant and MissionS256 is set.
type UsageRequest struct {
	Sub         string   `json:"sub,omitempty"`
	Tenant      string   `json:"tenant,omitempty"`
	MissionS256 string   `json:"mission_s256,omitempty"`
	JKTs        []string `json:"jkts,omitempty"`
	PS          string   `json:"ps,omitempty"`
}

// Validate applies "A request MUST carry a scope key or jkts... At most one scope key may
// appear." The result is a *Problem.
func (q *UsageRequest) Validate() error {
	keys := 0
	for _, s := range []string{q.Sub, q.Tenant, q.MissionS256} {
		if s != "" {
			keys++
		}
	}
	if keys > 1 {
		return invalidRequest("at most one scope key may appear")
	}
	if keys == 0 && len(q.JKTs) == 0 {
		return invalidRequest("a scope key or jkts is required")
	}
	return nil
}

// UsageCounters are the calendar counters of a usage response. Only all_time is required.
type UsageCounters struct {
	Day     *int64 `json:"day,omitempty"`
	Week    *int64 `json:"week,omitempty"`
	Month   *int64 `json:"month,omitempty"`
	Year    *int64 `json:"year,omitempty"`
	AllTime int64  `json:"all_time"`
}

// UsageResponse is the 200 body from a usage_endpoint.
type UsageResponse struct {
	AsOf        int64            `json:"as_of"`
	Aud         string           `json:"aud"`
	Unit        string           `json:"unit"`
	Decimals    int              `json:"decimals"`
	Sub         string           `json:"sub,omitempty"`
	Tenant      string           `json:"tenant,omitempty"`
	MissionS256 string           `json:"mission_s256,omitempty"`
	Usage       *UsageCounters   `json:"usage,omitempty"`
	JKTs        map[string]int64 `json:"jkts,omitempty"`
}

// FormatBudgetAmount renders an amount for a person: "$5.00" for USD with enough digits,
// otherwise the plain figure and unit. "A PS that puts a budget to the person for consent MUST
// present the amount as a human-readable figure in its unit". It computes in integers, never
// floats.
func FormatBudgetAmount(b Budget) string {
	// Budget.Validate holds this for anything from a token. A figure from anywhere else is only ever shown, so it is shown as it stands, with no scale worked out.
	if b.Decimals < 0 || b.Decimals > MaxBudgetDecimals {
		return fmt.Sprintf("%d %s", b.Amount, b.Unit)
	}
	scale := int64(1)
	for i := 0; i < b.Decimals; i++ {
		scale *= 10
	}
	whole, frac := b.Amount/scale, b.Amount%scale
	digits := ""
	if b.Decimals > 0 {
		digits = fmt.Sprintf("%0*d", b.Decimals, frac)
		digits = strings.TrimRight(digits, "0")
	}
	symbol, money := "", false
	switch b.Unit {
	case "USD":
		symbol, money = "$", true
	case "EUR":
		symbol, money = "€", true
	case "GBP":
		symbol, money = "£", true
	}
	if money {
		for len(digits) < 2 {
			digits += "0"
		}
		return fmt.Sprintf("%s%d.%s", symbol, whole, digits)
	}
	if digits != "" {
		return fmt.Sprintf("%d.%s %s", whole, digits, b.Unit)
	}
	return fmt.Sprintf("%d %s", whole, b.Unit)
}
