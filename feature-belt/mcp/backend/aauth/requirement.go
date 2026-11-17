package aauth

import (
	"errors"
	"fmt"
	"strings"

	"github.com/dunglas/httpsfv"
)

// The requirement values this document and its extensions define (protocol,
// Requirement Values).
const (
	RequirementAgentToken    = "agent-token"
	RequirementPersonToken   = "person-token"
	RequirementAuthToken     = "auth-token"
	RequirementInteraction   = "interaction"
	RequirementApproval      = "approval"
	RequirementClarification = "clarification"
	RequirementClaims        = "claims"
)

// The `reason` parameter values of AAuth Budgets.
const (
	ReasonBudgetExhausted    = "budget-exhausted"
	ReasonInsufficientBudget = "insufficient-budget"
)

// Requirement is an AAuth-Requirement field. Parameters a recipient does not model
// are ignored, as "Recipients MUST ignore unknown parameters".
type Requirement struct {
	// Value is the `requirement` Token.
	Value string
	// ResourceToken is the `resource-token` parameter of requirement=auth-token.
	ResourceToken string
	// URL and Code are the parameters of requirement=interaction.
	URL  string
	Code string
	// Reason is the Budgets `reason` Token parameter.
	Reason string
}

// Field serializes the requirement as an RFC 9651 Dictionary, in strict form, with no
// space after `;`. It refuses the combinations the spec forbids: interaction needs an
// https `url` with no query or fragment and a `code`; auth-token needs a resource-token.
func (q Requirement) Field() (string, error) {
	return q.field(IdentifierPolicy{})
}

// FieldWithPolicy is Field under an identifier policy, for development loopback URLs.
func (q Requirement) FieldWithPolicy(p IdentifierPolicy) (string, error) { return q.field(p) }

// Validate applies the rules on a requirement's parameters. ParseRequirement doesn't, so that an
// unrecognized requirement stays readable; call this on one you are about to act on, such as an
// access server's interaction requirement the PS will relay.
func (q Requirement) Validate(policy IdentifierPolicy) error {
	if q.Value == "" {
		return errors.New("aauth: requirement has no value")
	}
	switch q.Value {
	case RequirementAuthToken:
		if q.ResourceToken == "" {
			return errors.New("aauth: requirement=auth-token needs a resource-token")
		}
	case RequirementInteraction:
		// "MUST use the https scheme and MUST NOT contain query or fragment components."
		if err := policy.ValidateEndpointURL(q.URL); err != nil {
			return fmt.Errorf("aauth: requirement=interaction url: %w", err)
		}
		if q.Code == "" {
			return errors.New("aauth: requirement=interaction needs a code")
		}
	}
	return nil
}

func (q Requirement) field(policy IdentifierPolicy) (string, error) {
	if err := q.Validate(policy); err != nil {
		return "", err
	}
	params := httpsfv.NewParams()
	switch q.Value {
	case RequirementAuthToken:
		params.Add("resource-token", q.ResourceToken)
	case RequirementInteraction:
		params.Add("url", q.URL)
		params.Add("code", q.Code)
	}
	if q.Reason != "" {
		params.Add("reason", httpsfv.Token(q.Reason))
	}
	dict := httpsfv.NewDictionary()
	dict.Add("requirement", httpsfv.Item{Value: httpsfv.Token(q.Value), Params: params})
	return httpsfv.Marshal(dict)
}

// ParseRequirement parses an AAuth-Requirement field. A field that is not a Dictionary,
// has no `requirement` member, or whose member isn't a Token is an error. A requirement
// value this package doesn't know parses fine: "An agent that does not recognize the
// requirement value MUST NOT treat the response as satisfiable", which is the caller's
// to act on.
func ParseRequirement(fieldValues []string) (Requirement, error) {
	dict, err := httpsfv.UnmarshalDictionary(fieldValues)
	if err != nil {
		return Requirement{}, fmt.Errorf("parse AAuth-Requirement: %w", err)
	}
	member, ok := dict.Get("requirement")
	if !ok {
		return Requirement{}, errors.New("parse AAuth-Requirement: no requirement member")
	}
	item, ok := member.(httpsfv.Item)
	if !ok {
		return Requirement{}, errors.New("parse AAuth-Requirement: requirement is not an Item")
	}
	token, ok := item.Value.(httpsfv.Token)
	if !ok {
		return Requirement{}, errors.New("parse AAuth-Requirement: requirement is not a Token")
	}
	q := Requirement{Value: string(token)}
	readString := func(name string, dst *string) {
		if v, ok := item.Params.Get(name); ok {
			if s, ok := v.(string); ok {
				*dst = s
			}
		}
	}
	readString("resource-token", &q.ResourceToken)
	readString("url", &q.URL)
	readString("code", &q.Code)
	if v, ok := item.Params.Get("reason"); ok {
		if t, ok := v.(httpsfv.Token); ok {
			q.Reason = string(t)
		}
	}
	return q, nil
}

// The capability values this document and Budgets define.
const (
	CapabilityInteraction   = "interaction"
	CapabilityClarification = "clarification"
	CapabilityPayment       = "payment"
	CapabilityBudget        = "budget"
)

// CapabilitiesField serializes an AAuth-Capabilities List of Tokens.
func CapabilitiesField(capabilities []string) (string, error) {
	list := httpsfv.List{}
	for _, c := range capabilities {
		list = append(list, httpsfv.NewItem(httpsfv.Token(c)))
	}
	return httpsfv.Marshal(list)
}

// ParseCapabilities reads an AAuth-Capabilities field. It returns every Token, with
// parameters dropped, and leaves ignoring unrecognized values to the comparison. A
// malformed field reads as absent: "When the header is absent, recipients MUST NOT
// assume any capabilities."
func ParseCapabilities(fieldValues []string) []string {
	if len(fieldValues) == 0 {
		return nil
	}
	list, err := httpsfv.UnmarshalList(fieldValues)
	if err != nil {
		return nil
	}
	var out []string
	for _, m := range list {
		if item, ok := m.(httpsfv.Item); ok {
			if t, ok := item.Value.(httpsfv.Token); ok {
				out = append(out, string(t))
			}
		}
	}
	return out
}

// HasCapability reports whether capabilities contains want.
func HasCapability(capabilities []string, want string) bool {
	for _, c := range capabilities {
		if c == want {
			return true
		}
	}
	return false
}

// ValidateAccessToken applies the AAuth-Access rule: the value "is a token68. Recipients
// MUST reject empty values, values containing embedded whitespace or control characters".
func ValidateAccessToken(value string) error {
	if value == "" {
		return errors.New("aauth: session token is empty")
	}
	end := strings.TrimRight(value, "=")
	if end == "" {
		return errors.New("aauth: session token is not a token68")
	}
	for i := 0; i < len(end); i++ {
		c := end[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case strings.IndexByte("-._~+/", c) >= 0:
		default:
			return errors.New("aauth: session token is not a token68")
		}
	}
	return nil
}

// ParseAAuthAuthorization reads a session token from an Authorization field whose
// scheme is AAuth. ok is false for any other scheme. A malformed value or "more than one
// credential" is an error.
func ParseAAuthAuthorization(fieldValues []string) (token string, ok bool, err error) {
	if len(fieldValues) == 0 {
		return "", false, nil
	}
	if len(fieldValues) > 1 {
		return "", false, errors.New("aauth: more than one Authorization field")
	}
	scheme, rest, _ := strings.Cut(strings.TrimSpace(fieldValues[0]), " ")
	if !strings.EqualFold(scheme, "AAuth") {
		return "", false, nil
	}
	rest = strings.TrimLeft(rest, " ")
	if err := ValidateAccessToken(rest); err != nil {
		return "", true, err
	}
	return rest, true, nil
}
