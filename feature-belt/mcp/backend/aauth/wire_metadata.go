package aauth

import (
	"context"
	"errors"
	"fmt"
)

// The access_mode values of resource metadata (protocol registry, plus R3's per-call).
const (
	AccessModeAgentToken   = "agent-token"
	AccessModePersonToken  = "person-token"
	AccessModeSessionToken = "session-token"
	AccessModeAuthToken    = "auth-token"
	AccessModePerCall      = "per-call"
)

// CommonMetadata holds "the following fields defined identically across all four
// metadata documents" (protocol, Metadata Documents).
type CommonMetadata struct {
	Issuer              string   `json:"issuer"`
	JWKSURI             string   `json:"jwks_uri,omitempty"`
	AcceptSignatureAlgs []string `json:"accept_signature_algs,omitempty"`
	Name                string   `json:"name,omitempty"`
	Description         string   `json:"description,omitempty"`
	LogoURI             string   `json:"logo_uri,omitempty"`
	LogoDarkURI         string   `json:"logo_dark_uri,omitempty"`
	DocumentationURI    string   `json:"documentation_uri,omitempty"`
	TOSURI              string   `json:"tos_uri,omitempty"`
	PolicyURI           string   `json:"policy_uri,omitempty"`
}

func (m *CommonMetadata) validate(p IdentifierPolicy, jwksRequired bool) error {
	if err := p.ValidateServerIdentifier(m.Issuer); err != nil {
		return fmt.Errorf("issuer: %w", err)
	}
	if m.JWKSURI == "" && jwksRequired {
		return errors.New("jwks_uri is required")
	}
	for name, v := range map[string]string{
		"jwks_uri":          m.JWKSURI,
		"logo_uri":          m.LogoURI,
		"logo_dark_uri":     m.LogoDarkURI,
		"documentation_uri": m.DocumentationURI,
		"tos_uri":           m.TOSURI,
		"policy_uri":        m.PolicyURI,
	} {
		if v != "" {
			if err := p.ValidateHTTPSURL(v); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
		}
	}
	for _, alg := range m.AcceptSignatureAlgs {
		// A server MUST NOT list none or a symmetric algorithm; EdDSA is forbidden.
		if alg == "none" || alg == "EdDSA" || (len(alg) >= 2 && alg[:2] == "HS") {
			return fmt.Errorf("accept_signature_algs must not list %s", alg)
		}
	}
	return nil
}

func validateEndpoints(p IdentifierPolicy, endpoints map[string]string, required ...string) error {
	for _, name := range required {
		if endpoints[name] == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	for name, v := range endpoints {
		if v != "" {
			if err := p.ValidateEndpointURL(v); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
		}
	}
	return nil
}

// AgentProviderMetadata is /.well-known/aauth-agent.json.
type AgentProviderMetadata struct {
	CommonMetadata
	CallbackEndpoint         string `json:"callback_endpoint,omitempty"`
	EventEndpoint            string `json:"event_endpoint,omitempty"`
	LocalhostCallbackAllowed bool   `json:"localhost_callback_allowed,omitempty"`
}

// Validate checks required members and URL rules.
func (m *AgentProviderMetadata) Validate(p IdentifierPolicy) error {
	if err := m.validate(p, true); err != nil {
		return err
	}
	if m.EventEndpoint != "" {
		if err := p.ValidateHTTPSURL(m.EventEndpoint); err != nil {
			return fmt.Errorf("event_endpoint: %w", err)
		}
	}
	return validateEndpoints(p, map[string]string{"callback_endpoint": m.CallbackEndpoint})
}

// PersonServerMetadata is /.well-known/aauth-person.json. "The four REQUIRED fields
// (issuer, jwks_uri, auth_token_endpoint, person_token_endpoint) are the whole of what a
// conformant PS publishes."
type PersonServerMetadata struct {
	CommonMetadata
	AuthTokenEndpoint      string   `json:"auth_token_endpoint"`
	PersonTokenEndpoint    string   `json:"person_token_endpoint"`
	MissionEndpoint        string   `json:"mission_endpoint,omitempty"`
	PermissionEndpoint     string   `json:"permission_endpoint,omitempty"`
	AuditEndpoint          string   `json:"audit_endpoint,omitempty"`
	InteractionEndpoint    string   `json:"interaction_endpoint,omitempty"`
	MissionControlEndpoint string   `json:"mission_control_endpoint,omitempty"`
	RevocationEndpoint     string   `json:"revocation_endpoint,omitempty"`
	ScopesSupported        []string `json:"scopes_supported,omitempty"`
	ClaimsSupported        []string `json:"claims_supported,omitempty"`
}

// Validate checks required members and URL rules.
func (m *PersonServerMetadata) Validate(p IdentifierPolicy) error {
	if err := m.validate(p, true); err != nil {
		return err
	}
	if err := validateEndpoints(p, map[string]string{
		"auth_token_endpoint":   m.AuthTokenEndpoint,
		"person_token_endpoint": m.PersonTokenEndpoint,
		"mission_endpoint":      m.MissionEndpoint,
	}, "auth_token_endpoint", "person_token_endpoint"); err != nil {
		return err
	}
	for name, v := range map[string]string{
		"permission_endpoint":      m.PermissionEndpoint,
		"audit_endpoint":           m.AuditEndpoint,
		"interaction_endpoint":     m.InteractionEndpoint,
		"mission_control_endpoint": m.MissionControlEndpoint,
		"revocation_endpoint":      m.RevocationEndpoint,
	} {
		if v != "" {
			if err := p.ValidateHTTPSURL(v); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
		}
	}
	return nil
}

// AccessServerMetadata is /.well-known/aauth-access.json.
type AccessServerMetadata struct {
	CommonMetadata
	AuthTokenEndpoint  string `json:"auth_token_endpoint"`
	RevocationEndpoint string `json:"revocation_endpoint,omitempty"`
}

// Validate checks required members and URL rules.
func (m *AccessServerMetadata) Validate(p IdentifierPolicy) error {
	if err := m.validate(p, true); err != nil {
		return err
	}
	if m.RevocationEndpoint != "" {
		if err := p.ValidateHTTPSURL(m.RevocationEndpoint); err != nil {
			return fmt.Errorf("revocation_endpoint: %w", err)
		}
	}
	return validateEndpoints(p, map[string]string{"auth_token_endpoint": m.AuthTokenEndpoint}, "auth_token_endpoint")
}

// BudgetUnit declares a unit a resource denominates budgets in (budgets, Resource
// Metadata Extensions). Max is RECOMMENDED, so it is a pointer: zero is a real value.
type BudgetUnit struct {
	Unit        string `json:"unit"`
	Decimals    int    `json:"decimals"`
	Max         *int64 `json:"max,omitempty"`
	Description string `json:"description,omitempty"`
}

// ResourceMetadata is /.well-known/aauth-resource.json, with the R3 and Budgets fields.
// jwks_uri is optional here: "A resource that only verifies agent signatures has no keys
// to publish and MAY omit it".
type ResourceMetadata struct {
	CommonMetadata
	AccessMode                    string            `json:"access_mode,omitempty"`
	AuthorizationEndpoint         string            `json:"authorization_endpoint,omitempty"`
	ScopeDescriptions             map[string]string `json:"scope_descriptions,omitempty"`
	SignatureWindow               int               `json:"signature_window,omitempty"`
	AdditionalSignatureComponents []string          `json:"additional_signature_components,omitempty"`
	RevocationEndpoint            string            `json:"revocation_endpoint,omitempty"`
	R3Vocabularies                map[string]string `json:"r3_vocabularies,omitempty"`
	BudgetUnits                   []BudgetUnit      `json:"budget_units,omitempty"`
	UsageEndpoint                 string            `json:"usage_endpoint,omitempty"`
}

// Validate checks required members and URL rules. An unrecognized access_mode is fine:
// "An agent that does not recognize a value proceeds as with no declaration."
func (m *ResourceMetadata) Validate(p IdentifierPolicy) error {
	if err := m.validate(p, false); err != nil {
		return err
	}
	if m.RevocationEndpoint != "" {
		if err := p.ValidateHTTPSURL(m.RevocationEndpoint); err != nil {
			return fmt.Errorf("revocation_endpoint: %w", err)
		}
	}
	return validateEndpoints(p, map[string]string{
		"authorization_endpoint": m.AuthorizationEndpoint,
		"usage_endpoint":         m.UsageEndpoint,
	})
}

// SupervisorMetadata is /.well-known/aauth-supervisor.json (supervision, Supervisor
// Metadata). "An SS that publishes [jwks_uri] signs its decisions."
type SupervisorMetadata struct {
	CommonMetadata
	ConnectEndpoint  string `json:"connect_endpoint"`
	MissionEndpoint  string `json:"mission_endpoint"`
	DecisionEndpoint string `json:"decision_endpoint"`
	EventEndpoint    string `json:"event_endpoint"`
}

// Validate checks required members and URL rules. Whether an endpoint is "on the
// issuer's origin or one the SS controls" can't be judged from the document alone.
func (m *SupervisorMetadata) Validate(p IdentifierPolicy) error {
	if err := m.validate(p, false); err != nil {
		return err
	}
	for name, v := range map[string]string{
		"connect_endpoint":  m.ConnectEndpoint,
		"mission_endpoint":  m.MissionEndpoint,
		"decision_endpoint": m.DecisionEndpoint,
		"event_endpoint":    m.EventEndpoint,
	} {
		if v == "" {
			return fmt.Errorf("%s is required", name)
		}
		if err := p.ValidateHTTPSURL(v); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

func decodeValidated(doc *MetadataDocument, dst any, validate func() error) error {
	if err := doc.Decode(dst); err != nil {
		return fmt.Errorf("metadata %s: %w", doc.Issuer, err)
	}
	if err := validate(); err != nil {
		return fmt.Errorf("metadata %s: %w", doc.Issuer, err)
	}
	return nil
}

// AgentProvider fetches and validates an agent provider's metadata.
func (d *Discovery) AgentProvider(ctx context.Context, id string) (*AgentProviderMetadata, error) {
	doc, err := d.Metadata(ctx, id, DwkAgent)
	if err != nil {
		return nil, err
	}
	m := &AgentProviderMetadata{}
	return m, decodeValidated(doc, m, func() error { return m.Validate(d.cfg.Policy) })
}

// PersonServer fetches and validates a person server's metadata.
func (d *Discovery) PersonServer(ctx context.Context, id string) (*PersonServerMetadata, error) {
	doc, err := d.Metadata(ctx, id, DwkPerson)
	if err != nil {
		return nil, err
	}
	m := &PersonServerMetadata{}
	return m, decodeValidated(doc, m, func() error { return m.Validate(d.cfg.Policy) })
}

// AccessServer fetches and validates an access server's metadata.
func (d *Discovery) AccessServer(ctx context.Context, id string) (*AccessServerMetadata, error) {
	doc, err := d.Metadata(ctx, id, DwkAccess)
	if err != nil {
		return nil, err
	}
	m := &AccessServerMetadata{}
	return m, decodeValidated(doc, m, func() error { return m.Validate(d.cfg.Policy) })
}

// Resource fetches and validates a resource's metadata.
func (d *Discovery) Resource(ctx context.Context, id string) (*ResourceMetadata, error) {
	doc, err := d.Metadata(ctx, id, DwkResource)
	if err != nil {
		return nil, err
	}
	m := &ResourceMetadata{}
	return m, decodeValidated(doc, m, func() error { return m.Validate(d.cfg.Policy) })
}

// Supervisor fetches and validates a supervision server's metadata.
func (d *Discovery) Supervisor(ctx context.Context, id string) (*SupervisorMetadata, error) {
	doc, err := d.Metadata(ctx, id, DwkSupervisor)
	if err != nil {
		return nil, err
	}
	m := &SupervisorMetadata{}
	return m, decodeValidated(doc, m, func() error { return m.Validate(d.cfg.Policy) })
}
