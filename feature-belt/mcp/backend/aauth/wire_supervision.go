package aauth

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Envelope holds the members every supervision request body carries (supervision, Envelope).
// SDI is required on a supervision request, ID on an event and on a report.
//
// Capabilities is an additive extension (docs/ARCHITECTURE.md section 13): the draft's envelope
// has no capabilities, and the SS needs them to tell whether an agent can answer a
// clarification. A conforming SS ignores it.
type Envelope struct {
	SDI          string   `json:"sdi,omitempty"`
	ID           string   `json:"id,omitempty"`
	Iat          int64    `json:"iat"`
	Sub          string   `json:"sub"`
	Agent        string   `json:"agent"`
	MissionS256  string   `json:"mission_s256,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
}

// Validate checks the members every kind needs.
func (e *Envelope) validate() error {
	if e.Iat <= 0 {
		return invalidRequest("iat is required")
	}
	if e.Sub == "" {
		return invalidRequest("sub is required")
	}
	if e.Agent == "" {
		return invalidRequest("agent is required")
	}
	if e.MissionS256 != "" && !validMissionS256(e.MissionS256) {
		return invalidRequest("mission_s256 is malformed")
	}
	return nil
}

func (e *Envelope) validateRequest() error {
	if e.SDI == "" {
		return invalidRequest("sdi is required")
	}
	return e.validate()
}

func (e *Envelope) validateReport() error {
	if e.ID == "" {
		return invalidRequest("id is required")
	}
	return e.validate()
}

// The `action` values of a connect request.
const (
	ConnectActionConnect    = "connect"
	ConnectActionDisconnect = "disconnect"
)

// ConnectRequest is the body of a POST to connect_endpoint. It has no agent or sdi: it concerns
// the person.
type ConnectRequest struct {
	Action string `json:"action"`
	ID     string `json:"id"`
	Iat    int64  `json:"iat"`
	Sub    string `json:"sub"`
	Tenant string `json:"tenant,omitempty"`
}

// Validate checks required members. The result is a *Problem.
func (q *ConnectRequest) Validate() error {
	if q.Action != ConnectActionConnect && q.Action != ConnectActionDisconnect {
		return invalidRequest("action must be connect or disconnect")
	}
	if q.ID == "" || q.Iat <= 0 || q.Sub == "" {
		return invalidRequest("id, iat and sub are required")
	}
	return nil
}

// MissionProposalSupervision is a POST to the SS's mission_endpoint.
type MissionProposalSupervision struct {
	Envelope
	AgentToken  string   `json:"agent_token"`
	Description string   `json:"description"`
	Tools       []Tool   `json:"tools,omitempty"`
	Resources   []string `json:"resources,omitempty"`
}

// Validate checks required members. The result is a *Problem.
func (q *MissionProposalSupervision) Validate() error {
	if err := q.validateRequest(); err != nil {
		return err
	}
	if q.AgentToken == "" {
		return invalidRequest("agent_token is required")
	}
	if q.Description == "" {
		return invalidRequest("description is required")
	}
	return nil
}

// MissionActionSupervision is a POST to the SS's {mission_endpoint}/{mission_s256}. Action
// decides which members apply: `update` is a supervision request with Description, `completion`
// a supervision request with Summary, `approved` a report with S256 and Mission, and `terminated`
// a report with Reason.
type MissionActionSupervision struct {
	Action string `json:"action"`
	Envelope
	Description string `json:"description,omitempty"` // update
	Summary     string `json:"summary,omitempty"`     // completion
	S256        string `json:"s256,omitempty"`        // approved
	Mission     string `json:"mission,omitempty"`     // approved
	Reason      string `json:"reason,omitempty"`      // terminated
}

// Validate applies "an SS MUST reject a missing or unrecognized action with invalid_request" and
// each action's required members. The result is a *Problem.
func (q *MissionActionSupervision) Validate() error {
	switch q.Action {
	case MissionActionUpdate:
		if err := q.validateRequest(); err != nil {
			return err
		}
		if q.Description == "" {
			return invalidRequest("description is required")
		}
	case MissionActionCompletion:
		if err := q.validateRequest(); err != nil {
			return err
		}
		if q.Summary == "" {
			return invalidRequest("summary is required")
		}
	case MissionActionApproved:
		if err := q.validateReport(); err != nil {
			return err
		}
		if q.S256 == "" || q.Mission == "" {
			return invalidRequest("s256 and mission are required")
		}
	case MissionActionTerminated:
		if err := q.validateReport(); err != nil {
			return err
		}
		if q.Reason == "" {
			return invalidRequest("reason is required")
		}
	default:
		return invalidRequest("action is missing or unrecognized")
	}
	return nil
}

// IsReport reports whether the action is a report answered with 201, rather than a supervision
// request answered with a decision.
func (q *MissionActionSupervision) IsReport() bool {
	return q.Action == MissionActionApproved || q.Action == MissionActionTerminated
}

// The `act` values of the decision endpoint.
const (
	ActPersonToken = "person_token"
	ActAuthToken   = "auth_token"
	ActPermission  = "permission"
)

// R3Forwarded carries what the PS read from an R3 document or per-call proposal to the SS, as an
// additive extension (docs/ARCHITECTURE.md section 13). Only the PS and the issuer may fetch R3
// documents, so without this the SS never sees them. `result` is AAuth issue 257; `display` and
// `parameters` are the resource-asserted facts the review reads. Recipients ignore unknown
// members, so a conforming SS is unaffected.
type R3Forwarded struct {
	URI        string          `json:"uri"`
	S256       string          `json:"s256"`
	Display    *R3Display      `json:"display,omitempty"`
	Parameters json.RawMessage `json:"parameters,omitempty"`
	Result     json.RawMessage `json:"result,omitempty"`
}

// UsageForwarded carries what the PS read at a resource's usage_endpoint to the SS, as an additive
// extension: Budgets puts reading the counters with the PS and its issuer, and the SS is neither, so
// without this it decides without them. Person is the query by `sub`, Mission the query by
// `mission_s256`. Recipients ignore unknown members.
type UsageForwarded struct {
	Person  *UsageResponse `json:"person,omitempty"`
	Mission *UsageResponse `json:"mission,omitempty"`
}

// DecisionRequest is a POST to the SS's decision_endpoint, with Act naming the kind. Fields not
// used by the kind are empty.
type DecisionRequest struct {
	Act string `json:"act"`
	Envelope

	// person_token and auth_token.
	AgentToken    string `json:"agent_token,omitempty"`
	UpstreamToken string `json:"upstream_token,omitempty"`
	SubagentToken string `json:"subagent_token,omitempty"`
	// Platform and Device are what the agent said in its token request, as it sent them: "The PS forwards the tokens and body the agent
	// presented, so the SS sees what the PS saw." Supervision's member lists leave them out, and the protocol counts them as agent-asserted.
	Platform string `json:"platform,omitempty"`
	Device   string `json:"device,omitempty"`
	// Prompt is an additive extension: the agent's `prompt`, as it sent it. "specifying whether the PS prompts the user for reauthentication and consent": login, consent and select_account reach the person whoever supervises,
	// so an SS's gates answer ask.
	Prompt string `json:"prompt,omitempty"`

	// person_token.
	Resource string `json:"resource,omitempty"`

	// auth_token.
	ResourceToken  string `json:"resource_token,omitempty"`
	PresentedToken string `json:"presented_token,omitempty"`
	Justification  string `json:"justification,omitempty"`
	// R3 is an additive extension: see R3Forwarded.
	R3 *R3Forwarded `json:"r3,omitempty"`
	// Usage is an additive extension: see UsageForwarded.
	Usage *UsageForwarded `json:"usage,omitempty"`

	// permission.
	Action      string          `json:"action,omitempty"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// Validate applies each act's required members, and unrecognized `act` is invalid_request. The
// result is a *Problem.
func (q *DecisionRequest) Validate() error {
	if err := q.validateRequest(); err != nil {
		return err
	}
	switch q.Act {
	case ActPersonToken:
		if q.AgentToken == "" || q.Resource == "" {
			return invalidRequest("agent_token and resource are required")
		}
	case ActAuthToken:
		if q.AgentToken == "" || q.ResourceToken == "" || q.PresentedToken == "" {
			return invalidRequest("agent_token, resource_token and presented_token are required")
		}
	case ActPermission:
		if q.Action == "" {
			return invalidRequest("action is required")
		}
	default:
		return invalidRequest("act is missing or unrecognized")
	}
	return nil
}

// The `decision` values (supervision, Supervision Decisions). clarify is not a decision: it
// leaves the sdi open and arrives as a 202.
const (
	DecisionAllow   = "allow"
	DecisionDeny    = "deny"
	DecisionAsk     = "ask"
	DecisionClarify = "clarify"
)

// ApplyOption is a reach the SS offers the person for their answer.
type ApplyOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// The `apply` values whose meaning is fixed, offered when an ask carries no apply_options.
const (
	ApplyOnce    = "once"
	ApplyMission = "mission"
	ApplyAlways  = "always"
)

// MissionRefinement holds the members the PS writes into a mission blob, as an allow carries them
// directly and an ask carries them under `proposed`.
type MissionRefinement struct {
	Description       string   `json:"description,omitempty"`
	ApprovedTools     []Tool   `json:"approved_tools,omitempty"`
	ApprovedResources []string `json:"approved_resources,omitempty"`
	ExpiresAt         string   `json:"expires_at,omitempty"`
}

// Decision is the 200 body from the SS: allow, deny or ask.
//
// Budget is an additive extension (docs/ARCHITECTURE.md section 11 and 13): the draft has no
// budget narrowing, Budgets puts sizing with the PS, and the PS may not decide. Recipients ignore
// unknown members. On an allow it is the allocation, on an ask what the SS suggests, and on a deny
// the figure the SS would accept.
//
// R3Granted and R3PerCall are additive too: R3 has the issuer decide "which operations to grant
// outright and which to make per-call", and for a supervised agent that judgment is the SS's.
type Decision struct {
	SDI      string `json:"sdi"`
	Decision string `json:"decision"`

	// allow.
	Scope     string        `json:"scope,omitempty"`
	Exp       int64         `json:"exp,omitempty"`
	Budget    *Budget       `json:"budget,omitempty"`
	R3Granted *R3Operations `json:"r3_granted,omitempty"`
	R3PerCall *R3Operations `json:"r3_per_call,omitempty"`
	MissionRefinement

	// deny.
	Reason string `json:"reason,omitempty"`

	// ask.
	Summary        string             `json:"summary,omitempty"`
	Recommendation string             `json:"recommendation,omitempty"`
	Proposed       *MissionRefinement `json:"proposed,omitempty"`
	ApplyOptions   []ApplyOption      `json:"apply_options,omitempty"`
	// HighRisk is an additive extension: the SS says the act matters enough that a PS
	// holding interruptions back shouldn't hold this one. Recipients ignore unknown members.
	HighRisk bool `json:"high_risk,omitempty"`
	// Manipulation is an additive extension: the review found text in this request trying to influence it rather than describe the work, whether the agent wrote it or a
	// resource did. A surface can say so where the summary doesn't fit. Recipients ignore unknown members.
	Manipulation bool `json:"manipulation,omitempty"`
	// Stance, Said and SaidIDs are additive extensions on an ask: how the supervisor's own message to the person is drawn. Stance is allow,
	// unsure, deny or none (DecisionStance*). Said is its lines to the person, picked by code from a fixed sentence set, in the second person:
	// never the agent's words, a resource's, or a paraphrase of either. SaidIDs names each line's sentence, in the same order, so a PS can
	// word it again. Recipients ignore unknown members, and a PS ignores a stance it doesn't know.
	Stance  string   `json:"stance,omitempty"`
	Said    []string `json:"said,omitempty"`
	SaidIDs []string `json:"said_ids,omitempty"`
}

// The stance values of an ask.
const (
	DecisionStanceAllow  = "allow"
	DecisionStanceUnsure = "unsure"
	DecisionStanceDeny   = "deny"
	DecisionStanceNone   = "none"
)

// ErrMalformedDecision reports a response the PS must treat as the SS being unavailable: "the
// response is malformed".
var ErrMalformedDecision = errors.New("malformed supervision decision")

// ParseDecision reads a 200 body and applies "The response MUST echo the request's sdi. The PS MUST
// verify it matches." A failure means the SS is unavailable for this act.
func ParseDecision(body []byte, sdi string) (*Decision, error) {
	var d Decision
	if err := json.Unmarshal(body, &d); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformedDecision, err)
	}
	if d.SDI != sdi {
		return nil, fmt.Errorf("%w: sdi does not match", ErrMalformedDecision)
	}
	switch d.Decision {
	case DecisionAllow, DecisionDeny, DecisionAsk:
	default:
		return nil, fmt.Errorf("%w: decision %q", ErrMalformedDecision, d.Decision)
	}
	if d.Recommendation != "" && d.Recommendation != DecisionAllow && d.Recommendation != DecisionDeny {
		return nil, fmt.Errorf("%w: recommendation %q", ErrMalformedDecision, d.Recommendation)
	}
	return &d, nil
}

// ClarifyBody is the body of the SS's 202 with requirement=clarification, "with decision: clarify in
// the body beside status: pending, so the four responses read alike".
type ClarifyBody struct {
	SDI           string   `json:"sdi"`
	Status        string   `json:"status"`
	Decision      string   `json:"decision"`
	Clarification string   `json:"clarification"`
	Timeout       int      `json:"timeout,omitempty"`
	Options       []string `json:"options,omitempty"`
}

// The `event` names of the event endpoint.
const (
	EventPersonTokenIssued    = "person_token.issued"
	EventAuthTokenIssued      = "auth_token.issued"
	EventDecisionResolved     = "decision.resolved"
	EventAuditRecorded        = "audit.recorded"
	EventInteractionRequested = "interaction.requested"
	EventAgentBound           = "agent.bound"
	EventRevocationReceived   = "revocation.received"
)

// PersonTokenIssuedEvent reports a person token issued under the decision sdi.
type PersonTokenIssuedEvent struct {
	Event string `json:"event"`
	Envelope
	Jti string `json:"jti"`
	Aud string `json:"aud"`
	Exp int64  `json:"exp"`
}

// AuthTokenIssuedEvent reports an auth token issued by the PS, or by an AS on the PS's federation.
type AuthTokenIssuedEvent struct {
	Event string `json:"event"`
	Envelope
	Jti    string `json:"jti"`
	Aud    string `json:"aud"`
	Scope  string `json:"scope,omitempty"`
	Exp    int64  `json:"exp"`
	Issuer string `json:"issuer"`
}

// The `answer` values of decision.resolved, and the `supervisor` value.
const (
	AnswerAllow           = "allow"
	AnswerDeny            = "deny"
	AnswerAbandoned       = "abandoned"
	SupervisorUnavailable = "unavailable"
)

// The `from` values of a transcript entry.
const (
	TranscriptFromPerson = "person"
	TranscriptFromAgent  = "agent"
)

// TranscriptEntry is one turn of the clarification chat between the person and the agent.
type TranscriptEntry struct {
	From string `json:"from"`
	Text string `json:"text"`
}

// DecisionResolvedEvent reports what the person decided after an ask, or when the SS was unavailable.
type DecisionResolvedEvent struct {
	Event string `json:"event"`
	Envelope
	Answer         string             `json:"answer"`
	Apply          string             `json:"apply,omitempty"`
	Recommendation string             `json:"recommendation,omitempty"`
	Proposed       *MissionRefinement `json:"proposed,omitempty"`
	Transcript     []TranscriptEntry  `json:"transcript,omitempty"`
	Supervisor     string             `json:"supervisor,omitempty"`
	Reason         string             `json:"reason,omitempty"`
}

// AuditRecordedEvent reports what the agent logged at the PS's audit endpoint, as it sent it.
type AuditRecordedEvent struct {
	Event string `json:"event"`
	Envelope
	Action      string          `json:"action"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Result      json.RawMessage `json:"result,omitempty"`
}

// InteractionRequestedEvent reports a relayed interaction. "The code of a relayed interaction is not
// forwarded", so there is no such field.
type InteractionRequestedEvent struct {
	Event string `json:"event"`
	Envelope
	Type string `json:"type"`
	URL  string `json:"url,omitempty"`
}

// AgentBoundEvent reports that the PS bound the agent to the person on first approval.
type AgentBoundEvent struct {
	Event string `json:"event"`
	Envelope
	AgentIss string `json:"agent_iss"`
	AgentSub string `json:"agent_sub"`
}

// RevocationReceivedEvent reports a revocation the PS received.
type RevocationReceivedEvent struct {
	Event string `json:"event"`
	Envelope
	TokenType string `json:"token_type"`
	Iss       string `json:"iss"`
	Jti       string `json:"jti"`
	Exp       int64  `json:"exp"`
}

// ParseEvent decodes an event body to its typed struct, one of the *Event types above by value of
// its `event` member, and checks the envelope. An unrecognized event is invalid_request. The result is
// a *Problem on failure.
func ParseEvent(body []byte) (any, error) {
	var head struct {
		Event string `json:"event"`
	}
	if err := json.Unmarshal(body, &head); err != nil {
		return nil, invalidRequest("body is not a JSON object")
	}
	var out any
	var env *Envelope
	switch head.Event {
	case EventPersonTokenIssued:
		e := &PersonTokenIssuedEvent{}
		out, env = e, &e.Envelope
	case EventAuthTokenIssued:
		e := &AuthTokenIssuedEvent{}
		out, env = e, &e.Envelope
	case EventDecisionResolved:
		e := &DecisionResolvedEvent{}
		out, env = e, &e.Envelope
	case EventAuditRecorded:
		e := &AuditRecordedEvent{}
		out, env = e, &e.Envelope
	case EventInteractionRequested:
		e := &InteractionRequestedEvent{}
		out, env = e, &e.Envelope
	case EventAgentBound:
		e := &AgentBoundEvent{}
		out, env = e, &e.Envelope
	case EventRevocationReceived:
		e := &RevocationReceivedEvent{}
		out, env = e, &e.Envelope
	default:
		return nil, invalidRequest("event is missing or unrecognized")
	}
	if err := json.Unmarshal(body, out); err != nil {
		return nil, invalidRequest("event body is malformed")
	}
	if err := env.validateReport(); err != nil {
		return nil, err
	}
	if e, ok := out.(*DecisionResolvedEvent); ok {
		switch e.Answer {
		case AnswerAllow, AnswerDeny, AnswerAbandoned:
		default:
			return nil, invalidRequest("answer must be allow, deny or abandoned")
		}
		if e.SDI == "" {
			return nil, invalidRequest("sdi is required")
		}
	}
	return out, nil
}
