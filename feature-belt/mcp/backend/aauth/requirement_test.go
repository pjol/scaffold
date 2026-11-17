package aauth

import (
	"strings"
	"testing"
)

// protocol, Requirement Responses: the spec's examples, as written, parse.
func TestRequirementSpecExamples(t *testing.T) {
	q, err := ParseRequirement([]string{`requirement=auth-token; resource-token="eyJ..."`})
	if err != nil {
		t.Fatal(err)
	}
	if q.Value != RequirementAuthToken || q.ResourceToken != "eyJ..." {
		t.Fatalf("%+v", q)
	}
	q, err = ParseRequirement([]string{`requirement=interaction; url="https://example.com/interact"; code="A1B2-C3D4"`})
	if err != nil {
		t.Fatal(err)
	}
	if q.Value != RequirementInteraction || q.URL != "https://example.com/interact" || q.Code != "A1B2-C3D4" {
		t.Fatalf("%+v", q)
	}
	for _, simple := range []string{"person-token", "agent-token", "approval", "clarification", "claims"} {
		q, err := ParseRequirement([]string{"requirement=" + simple})
		if err != nil || q.Value != simple {
			t.Errorf("%s: %+v %v", simple, q, err)
		}
	}
	// budgets, The reason Parameter.
	q, err = ParseRequirement([]string{`requirement=auth-token; resource-token="eyJ..."; reason=budget-exhausted`})
	if err != nil || q.Reason != ReasonBudgetExhausted {
		t.Fatalf("%+v %v", q, err)
	}
}

// B-08: a valid RFC 9651 Dictionary, with unknown parameters and members ignored.
func TestRequirementParsing(t *testing.T) {
	q, err := ParseRequirement([]string{`other=1, requirement=approval;future="x";n=2`})
	if err != nil || q.Value != RequirementApproval {
		t.Fatalf("%+v %v", q, err)
	}
	// A `;` inside a quoted value belongs to the value.
	q, err = ParseRequirement([]string{`requirement=interaction;url="https://a.example/x;y";code="ABCD-EFGH"`})
	if err != nil || q.URL != "https://a.example/x;y" {
		t.Fatalf("%+v %v", q, err)
	}
	// An unknown value parses; satisfying it is the caller's question.
	q, err = ParseRequirement([]string{`requirement=future-thing`})
	if err != nil || q.Value != "future-thing" {
		t.Fatalf("%+v %v", q, err)
	}
	for _, bad := range []string{
		`requirement="auth-token"`, // a String, not a Token
		`requirement=1`,
		`requirement=(a b)`,
		`requirement`,
		`other=1`,
		``,
		`requirement=auth-token; resource-token=`,
		`(((`,
	} {
		if _, err := ParseRequirement([]string{bad}); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
}

func TestRequirementField(t *testing.T) {
	cases := []struct {
		q    Requirement
		want string
	}{
		{Requirement{Value: RequirementPersonToken}, `requirement=person-token`},
		{Requirement{Value: RequirementApproval}, `requirement=approval`},
		{Requirement{Value: RequirementClarification}, `requirement=clarification`},
		{Requirement{Value: RequirementClaims}, `requirement=claims`},
		{Requirement{Value: RequirementAuthToken, ResourceToken: "eyJ"}, `requirement=auth-token;resource-token="eyJ"`},
		{Requirement{Value: RequirementAuthToken, ResourceToken: "eyJ", Reason: ReasonInsufficientBudget}, `requirement=auth-token;resource-token="eyJ";reason=insufficient-budget`},
		{Requirement{Value: RequirementInteraction, URL: "https://example.com/interact", Code: "A1B2-C3D4"}, `requirement=interaction;url="https://example.com/interact";code="A1B2-C3D4"`},
	}
	for _, c := range cases {
		got, err := c.q.Field()
		if err != nil {
			t.Errorf("%+v: %v", c.q, err)
			continue
		}
		if got != c.want {
			t.Errorf("got %s, want %s", got, c.want)
		}
		back, err := ParseRequirement([]string{got})
		if err != nil || back != c.q {
			t.Errorf("round trip %s: %+v %v", got, back, err)
		}
	}
	for name, q := range map[string]Requirement{
		"no value":             {},
		"auth-token no token":  {Value: RequirementAuthToken},
		"interaction no code":  {Value: RequirementInteraction, URL: "https://a.example/i"},
		"interaction no url":   {Value: RequirementInteraction, Code: "ABCD"},
		"interaction http":     {Value: RequirementInteraction, URL: "http://a.example/i", Code: "ABCD"},
		"interaction query":    {Value: RequirementInteraction, URL: "https://a.example/i?x=1", Code: "ABCD"},
		"interaction fragment": {Value: RequirementInteraction, URL: "https://a.example/i#f", Code: "ABCD"},
	} {
		if _, err := q.Field(); err == nil {
			t.Errorf("%s: serialized", name)
		}
	}
	// A parsed requirement with no url reads, and Validate is what refuses it before acting.
	parsed, err := ParseRequirement([]string{`requirement=interaction;code="A1B2-C3D4"`})
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Validate(IdentifierPolicy{}) == nil {
		t.Error("an interaction with no url validated")
	}
	// Loopback interaction URLs need the dev policy.
	dev := IdentifierPolicy{AllowInsecureLoopback: true}
	q := Requirement{Value: RequirementInteraction, URL: "http://localhost:3000/i", Code: "ABCD"}
	if _, err := q.FieldWithPolicy(dev); err != nil {
		t.Error(err)
	}
	if _, err := q.Field(); err == nil {
		t.Error("loopback interaction URL accepted in production")
	}
}

// protocol, AAuth-Capabilities Request Header.
func TestCapabilities(t *testing.T) {
	got, err := CapabilitiesField([]string{CapabilityInteraction, CapabilityClarification, CapabilityPayment})
	if err != nil {
		t.Fatal(err)
	}
	if got != "interaction, clarification, payment" {
		t.Fatalf("got %s", got)
	}
	parsed := ParseCapabilities([]string{"interaction, clarification, payment"})
	if strings.Join(parsed, " ") != "interaction clarification payment" {
		t.Fatalf("%v", parsed)
	}
	// Parameters are ignored, as are values that are not Tokens.
	parsed = ParseCapabilities([]string{`interaction;future=1, "string", 7, future-capability`})
	if strings.Join(parsed, " ") != "interaction future-capability" {
		t.Fatalf("%v", parsed)
	}
	if !HasCapability(parsed, CapabilityInteraction) || HasCapability(parsed, CapabilityPayment) {
		t.Fatalf("%v", parsed)
	}
	// "When the header is absent, recipients MUST NOT assume any capabilities."
	if ParseCapabilities(nil) != nil || ParseCapabilities([]string{"((("}) != nil {
		t.Fatal("assumed capabilities")
	}
}

// protocol, AAuth-Access Response Header.
func TestAccessToken(t *testing.T) {
	for _, ok := range []string{"wrapped-session-token-value", "abc123==", "a.b_c~d+e/f"} {
		if err := ValidateAccessToken(ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "a b", "a\tb", "a\nb", "a\x01b", "====", "a=b", "a,b"} {
		if err := ValidateAccessToken(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	tok, ok, err := ParseAAuthAuthorization([]string{"AAuth wrapped-session-token-value"})
	if err != nil || !ok || tok != "wrapped-session-token-value" {
		t.Fatalf("%s %v %v", tok, ok, err)
	}
	if _, ok, err := ParseAAuthAuthorization([]string{"Bearer abc"}); ok || err != nil {
		t.Errorf("other scheme: %v %v", ok, err)
	}
	if _, ok, err := ParseAAuthAuthorization(nil); ok || err != nil {
		t.Errorf("absent: %v %v", ok, err)
	}
	if _, _, err := ParseAAuthAuthorization([]string{"AAuth a", "AAuth b"}); err == nil {
		t.Error("two credentials accepted")
	}
	if _, _, err := ParseAAuthAuthorization([]string{"AAuth a b"}); err == nil {
		t.Error("embedded whitespace accepted")
	}
	if _, _, err := ParseAAuthAuthorization([]string{"AAuth"}); err == nil {
		t.Error("empty credential accepted")
	}
}
