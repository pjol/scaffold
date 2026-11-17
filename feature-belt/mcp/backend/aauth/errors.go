package aauth

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// ErrorCode is the value of the `error` member of an AAuth problem+json body.
type ErrorCode string

// Token endpoint error codes (protocol, Token Endpoint Error Codes).
const (
	CodeInvalidRequest        ErrorCode = "invalid_request"
	CodeInvalidResourceToken  ErrorCode = "invalid_resource_token"
	CodeExpiredResourceToken  ErrorCode = "expired_resource_token"
	CodeRevokedResourceToken  ErrorCode = "revoked_resource_token"
	CodeInvalidPresentedToken ErrorCode = "invalid_presented_token"
	CodeExpiredPresentedToken ErrorCode = "expired_presented_token"
	CodeRevokedPresentedToken ErrorCode = "revoked_presented_token"
	CodeInvalidUpstreamToken  ErrorCode = "invalid_upstream_token"
	CodeExpiredUpstreamToken  ErrorCode = "expired_upstream_token"
	CodeRevokedUpstreamToken  ErrorCode = "revoked_upstream_token"
	CodeInvalidSubagentToken  ErrorCode = "invalid_subagent_token"
	CodeExpiredSubagentToken  ErrorCode = "expired_subagent_token"
	CodeRevokedSubagentToken  ErrorCode = "revoked_subagent_token"
	CodeClockSkew             ErrorCode = "clock_skew"
	CodeUserUnreachable       ErrorCode = "user_unreachable"
	CodeASUnreachable         ErrorCode = "as_unreachable"
	CodeServerError           ErrorCode = "server_error"
)

// Polling error codes (protocol, Polling Error Codes).
const (
	CodeDenied      ErrorCode = "denied"
	CodeAbandoned   ErrorCode = "abandoned"
	CodeExpired     ErrorCode = "expired"
	CodeRevoked     ErrorCode = "revoked"
	CodeInvalidCode ErrorCode = "invalid_code"
	CodeSlowDown    ErrorCode = "slow_down"
)

// Mission endpoint and mission status error codes.
const (
	CodeMissionNotFound   ErrorCode = "mission_not_found"
	CodeMissionTerminated ErrorCode = "mission_terminated"
)

// Interaction endpoint error code.
const CodeInteractionUnavailable ErrorCode = "interaction_unavailable"

// Authorization endpoint error codes.
const (
	CodeInvalidScope   ErrorCode = "invalid_scope"
	CodeInvalidAccount ErrorCode = "invalid_account"
)

// Revocation error codes. CodeInvalidRequest and CodeServerError also apply.
const (
	CodeUnsupportedIss ErrorCode = "unsupported_iss"
	CodeRateLimited    ErrorCode = "rate_limited"
)

// Supervision error codes (supervision, Errors). CodeInvalidRequest,
// CodeUnsupportedIss, CodeRateLimited and CodeServerError also apply.
const (
	CodeUnknownSub     ErrorCode = "unknown_sub"
	CodeUnknownMission ErrorCode = "unknown_mission"
	CodeUnknownPending ErrorCode = "unknown_pending"
)

// Budgets error code.
const CodeInvalidBudget ErrorCode = "invalid_budget"

// Downstream revocation outcomes (protocol, Revocation Response). They appear
// in a DownstreamRevocation, never as a problem's `error`.
const (
	RevocationUnsupported = "revocation_unsupported"
	RevocationUnavailable = "revocation_unavailable"
)

// Interaction callback error codes (protocol, Interaction Callback Errors).
// They travel in a query parameter, never in a problem body.
const (
	CallbackAccessDenied           = "access_denied"
	CallbackUserAbandoned          = "user_abandoned"
	CallbackServerError            = "server_error"
	CallbackTemporarilyUnavailable = "temporarily_unavailable"
	CallbackInteractionExpired     = "interaction_expired"
)

var errorStatus = map[ErrorCode]int{
	CodeInvalidRequest:         http.StatusBadRequest,
	CodeInvalidResourceToken:   http.StatusBadRequest,
	CodeExpiredResourceToken:   http.StatusBadRequest,
	CodeRevokedResourceToken:   http.StatusBadRequest,
	CodeInvalidPresentedToken:  http.StatusBadRequest,
	CodeExpiredPresentedToken:  http.StatusBadRequest,
	CodeRevokedPresentedToken:  http.StatusBadRequest,
	CodeInvalidUpstreamToken:   http.StatusBadRequest,
	CodeExpiredUpstreamToken:   http.StatusBadRequest,
	CodeRevokedUpstreamToken:   http.StatusBadRequest,
	CodeInvalidSubagentToken:   http.StatusBadRequest,
	CodeExpiredSubagentToken:   http.StatusBadRequest,
	CodeRevokedSubagentToken:   http.StatusBadRequest,
	CodeClockSkew:              http.StatusBadRequest,
	CodeUserUnreachable:        http.StatusForbidden,
	CodeASUnreachable:          http.StatusBadGateway,
	CodeServerError:            http.StatusInternalServerError,
	CodeDenied:                 http.StatusForbidden,
	CodeAbandoned:              http.StatusForbidden,
	CodeExpired:                http.StatusRequestTimeout,
	CodeRevoked:                http.StatusForbidden,
	CodeInvalidCode:            http.StatusGone,
	CodeSlowDown:               http.StatusTooManyRequests,
	CodeMissionNotFound:        http.StatusNotFound,
	CodeMissionTerminated:      http.StatusForbidden,
	CodeInteractionUnavailable: http.StatusFailedDependency,
	CodeInvalidScope:           http.StatusBadRequest,
	CodeInvalidAccount:         http.StatusBadRequest,
	CodeUnsupportedIss:         http.StatusForbidden,
	CodeRateLimited:            http.StatusTooManyRequests,
	CodeUnknownSub:             http.StatusNotFound,
	CodeUnknownMission:         http.StatusNotFound,
	CodeUnknownPending:         http.StatusNotFound,
	CodeInvalidBudget:          http.StatusBadRequest,
}

// Status returns the HTTP status the spec's tables assign to the code. An
// unknown code is 500, the status a recipient assumes for what it cannot read.
func (c ErrorCode) Status() int {
	if s, ok := errorStatus[c]; ok {
		return s
	}
	return http.StatusInternalServerError
}

// PollingErrorForCallback maps an interaction callback error to the polling
// error the PS returns to the agent (protocol, Interaction Callback Errors). An
// unknown value maps to server_error: "MUST NOT treat the pending request as
// completable".
func PollingErrorForCallback(callbackError string) ErrorCode {
	switch callbackError {
	case CallbackAccessDenied:
		return CodeDenied
	case CallbackUserAbandoned:
		return CodeAbandoned
	case CallbackInteractionExpired:
		return CodeExpired
	default:
		return CodeServerError
	}
}

// Problem is an RFC 9457 problem+json response in the AAuth format: the `error`
// member is what receivers act on, `detail` is for people. It implements error
// so a handler can return it.
type Problem struct {
	Code   ErrorCode
	Detail string
	// Status overrides Code.Status() when non-zero. Needed only when relaying
	// an access server's error with its own status (protocol, Auth Token
	// Delivery).
	Status int
	// Extra holds additional members, such as mission_status,
	// termination_reason or suggested_budget.
	Extra map[string]any

	retryAfter *int
}

// NewProblem returns a Problem with the code's standard status.
func NewProblem(code ErrorCode, detail string) *Problem {
	return &Problem{Code: code, Detail: detail}
}

// Error implements error.
func (p *Problem) Error() string {
	if p.Detail != "" {
		return string(p.Code) + ": " + p.Detail
	}
	return string(p.Code)
}

// With adds an extension member and returns p.
func (p *Problem) With(member string, value any) *Problem {
	if p.Extra == nil {
		p.Extra = map[string]any{}
	}
	p.Extra[member] = value
	return p
}

// WithRetryAfter sets the Retry-After header, REQUIRED on rate_limited.
func (p *Problem) WithRetryAfter(seconds int) *Problem {
	p.retryAfter = &seconds
	return p
}

// HTTPStatus returns the status this problem is written with.
func (p *Problem) HTTPStatus() int {
	if p.Status != 0 {
		return p.Status
	}
	return p.Code.Status()
}

// MissionTerminatedProblem builds the mission status error (protocol, Mission
// Status Errors). reason may be empty: termination_reason is OPTIONAL.
func MissionTerminatedProblem(reason string) *Problem {
	p := NewProblem(CodeMissionTerminated, "").With("mission_status", MissionStatusTerminated)
	if reason != "" {
		p.With("termination_reason", reason)
	}
	return p
}

// WriteProblem writes the problem as application/problem+json.
//
// A 403 denies access after the signature verified, and "such a response MUST
// NOT include a Signature-Error, Accept-Signature-Scheme, or
// Accept-Signature-Alg header" (protocol, Verification), so those headers are
// removed here whatever a middleware set earlier.
func WriteProblem(w http.ResponseWriter, p *Problem) {
	status := p.HTTPStatus()
	body := map[string]any{"error": p.Code}
	if p.Detail != "" {
		body["detail"] = p.Detail
	}
	for k, v := range p.Extra {
		if k != "error" && k != "detail" {
			body[k] = v
		}
	}
	b, err := json.Marshal(body)
	if err != nil {
		b = []byte(`{"error":"server_error"}`)
		status = http.StatusInternalServerError
	}
	h := w.Header()
	if status == http.StatusForbidden {
		h.Del(HeaderSignatureError)
		h.Del(HeaderAcceptSignatureScheme)
		h.Del(HeaderAcceptSignatureAlg)
	}
	h.Set("Content-Type", ContentTypeProblem)
	if p.retryAfter != nil {
		h.Set("Retry-After", strconv.Itoa(*p.retryAfter))
	}
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

// Header and media type names used across the package.
const (
	HeaderSignature             = "Signature"
	HeaderSignatureInput        = "Signature-Input"
	HeaderSignatureKey          = "Signature-Key"
	HeaderSignatureError        = "Signature-Error"
	HeaderAcceptSignature       = "Accept-Signature"
	HeaderAcceptSignatureScheme = "Accept-Signature-Scheme"
	HeaderAcceptSignatureAlg    = "Accept-Signature-Alg"
	HeaderContentDigest         = "Content-Digest"
	HeaderAAuthRequirement      = "AAuth-Requirement"
	HeaderAAuthCapabilities     = "AAuth-Capabilities"
	HeaderAAuthAccess           = "AAuth-Access"
	HeaderPrefer                = "Prefer"
	HeaderPreferenceApplied     = "Preference-Applied"

	ContentTypeJSON    = "application/json"
	ContentTypeProblem = "application/problem+json"
)
