package aauth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Every code in the spec's tables, with the status those tables give it.
func TestErrorCodeTables(t *testing.T) {
	for code, status := range map[ErrorCode]int{
		// Token Endpoint Error Codes.
		"invalid_request": 400, "invalid_resource_token": 400, "expired_resource_token": 400,
		"revoked_resource_token": 400, "invalid_presented_token": 400, "expired_presented_token": 400,
		"revoked_presented_token": 400, "invalid_upstream_token": 400, "expired_upstream_token": 400,
		"revoked_upstream_token": 400, "invalid_subagent_token": 400, "expired_subagent_token": 400,
		"revoked_subagent_token": 400, "clock_skew": 400, "user_unreachable": 403, "as_unreachable": 502,
		"server_error": 500,
		// Polling Error Codes.
		"denied": 403, "abandoned": 403, "expired": 408, "revoked": 403, "invalid_code": 410, "slow_down": 429,
		// Mission Endpoint Errors.
		"mission_not_found": 404, "mission_terminated": 403,
		// Interaction Endpoint Errors.
		"interaction_unavailable": 424,
		// Authorization Endpoint Error Responses.
		"invalid_scope": 400, "invalid_account": 400,
		// Revocation errors.
		"unsupported_iss": 403, "rate_limited": 429,
		// Supervision Errors.
		"unknown_sub": 404, "unknown_mission": 404, "unknown_pending": 404,
		// Budgets Errors.
		"invalid_budget": 400,
	} {
		if got := code.Status(); got != status {
			t.Errorf("%s: %d, want %d", code, got, status)
		}
	}
	if ErrorCode("not-a-code").Status() != 500 {
		t.Error("an unknown code reads as a server error")
	}
}

// protocol, Error Response Format, and the spec's two examples.
func TestWriteProblem(t *testing.T) {
	w := httptest.NewRecorder()
	WriteProblem(w, NewProblem(CodeExpiredResourceToken, "The resource token expired."))
	if w.Code != 400 || w.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("%d %v", w.Code, w.Header())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["error"] != "expired_resource_token" || body["detail"] != "The resource token expired." {
		t.Errorf("%v", body)
	}

	// No detail: the member is absent.
	w = httptest.NewRecorder()
	WriteProblem(w, NewProblem(CodeDenied, ""))
	if w.Body.String() != `{"error":"denied"}` {
		t.Errorf("%s", w.Body)
	}

	// protocol, Mission Status Errors.
	w = httptest.NewRecorder()
	WriteProblem(w, MissionTerminatedProblem(TerminationExpired))
	body = nil
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if w.Code != 403 || body["error"] != "mission_terminated" || body["mission_status"] != "terminated" || body["termination_reason"] != "expired" {
		t.Errorf("%d %v", w.Code, body)
	}
	w = httptest.NewRecorder()
	WriteProblem(w, MissionTerminatedProblem(""))
	body = nil
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if _, ok := body["termination_reason"]; ok {
		t.Errorf("%v", body)
	}

	// rate_limited: "Retry-After is REQUIRED".
	w = httptest.NewRecorder()
	WriteProblem(w, NewProblem(CodeRateLimited, "").WithRetryAfter(30))
	if w.Code != 429 || w.Header().Get("Retry-After") != "30" {
		t.Errorf("%d %v", w.Code, w.Header())
	}

	// budgets, Declining with Guidance.
	w = httptest.NewRecorder()
	WriteProblem(w, NewProblem(CodeDenied, "").With("suggested_budget", Budget{Amount: 5, Unit: "USD", Decimals: 2}))
	if w.Body.String() != `{"error":"denied","suggested_budget":{"amount":5,"unit":"USD","decimals":2}}` {
		t.Errorf("%s", w.Body)
	}

	// Extra can't override the code.
	w = httptest.NewRecorder()
	WriteProblem(w, NewProblem(CodeDenied, "").With("error", "other"))
	if w.Body.String() != `{"error":"denied"}` {
		t.Errorf("%s", w.Body)
	}

	// An AS's error relayed with its own status (protocol, Auth Token Delivery).
	w = httptest.NewRecorder()
	WriteProblem(w, &Problem{Code: "as_specific", Status: 451})
	if w.Code != 451 {
		t.Errorf("%d", w.Code)
	}

	// A body that can't serialize answers server_error rather than failing silently.
	w = httptest.NewRecorder()
	WriteProblem(w, NewProblem(CodeDenied, "").With("x", make(chan int)))
	if w.Code != 500 || w.Body.String() != `{"error":"server_error"}` {
		t.Errorf("%d %s", w.Code, w.Body)
	}
	if got := NewProblem(CodeDenied, "no").Error(); got != "denied: no" {
		t.Error(got)
	}
	if got := NewProblem(CodeDenied, "").Error(); got != "denied" {
		t.Error(got)
	}
}

// "A 403 response ... MUST NOT include a Signature-Error, Accept-Signature-Scheme, or
// Accept-Signature-Alg header."
func TestForbiddenCarriesNoSignatureHeaders(t *testing.T) {
	w := httptest.NewRecorder()
	h := w.Header()
	h.Set(HeaderSignatureError, "error=invalid_signature")
	h.Set(HeaderAcceptSignatureScheme, "jwt")
	h.Set(HeaderAcceptSignatureAlg, "Ed25519")
	WriteProblem(w, NewProblem(CodeDenied, ""))
	if w.Code != http.StatusForbidden {
		t.Fatalf("%d", w.Code)
	}
	for _, k := range []string{HeaderSignatureError, HeaderAcceptSignatureScheme, HeaderAcceptSignatureAlg} {
		if w.Header().Get(k) != "" {
			t.Errorf("%s present on a 403", k)
		}
	}
}

// protocol, Interaction Callback Errors.
func TestPollingErrorForCallback(t *testing.T) {
	for in, want := range map[string]ErrorCode{
		"access_denied":           CodeDenied,
		"user_abandoned":          CodeAbandoned,
		"interaction_expired":     CodeExpired,
		"server_error":            CodeServerError,
		"temporarily_unavailable": CodeServerError,
		"something_new":           CodeServerError,
	} {
		if got := PollingErrorForCallback(in); got != want {
			t.Errorf("%s: %s", in, got)
		}
	}
}
