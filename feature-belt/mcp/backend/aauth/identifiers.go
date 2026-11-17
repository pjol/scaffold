package aauth

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// ErrInvalidIdentifier is wrapped by every identifier and URL validation error.
var ErrInvalidIdentifier = errors.New("invalid identifier")

// IdentifierPolicy decides which identifiers are acceptable. The zero value is
// the production rule set from the spec.
type IdentifierPolicy struct {
	// AllowInsecureLoopback also accepts http and https issuers on localhost,
	// 127.0.0.1 and [::1], with a port. Development only: config.Validate
	// refuses it elsewhere (docs/ARCHITECTURE.md section 3).
	AllowInsecureLoopback bool
}

func idErr(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidIdentifier, fmt.Sprintf(format, args...))
}

// ValidateServerIdentifier applies the production rules of protocol section
// "Server Identifiers": https, scheme and host only, no trailing slash,
// lowercase, A-labels for internationalized names.
func ValidateServerIdentifier(id string) error {
	return IdentifierPolicy{}.ValidateServerIdentifier(id)
}

// ValidateServerIdentifier applies the policy's rules to a server identifier.
func (p IdentifierPolicy) ValidateServerIdentifier(id string) error {
	if id == "" {
		return idErr("server identifier is empty")
	}
	scheme, rest, ok := strings.Cut(id, "://")
	if !ok {
		return idErr("server identifier %q has no scheme", id)
	}
	if p.AllowInsecureLoopback && (scheme == "http" || scheme == "https") && isLoopbackHostPort(rest) {
		return nil
	}
	if scheme != "https" {
		return idErr("server identifier %q must use https", id)
	}
	if err := validDomainName(rest); err != nil {
		return idErr("server identifier %q: %v", id, err)
	}
	return nil
}

// SameServerIdentifier compares two server identifiers: "Implementations MUST
// perform exact string comparison on server identifiers."
func SameServerIdentifier(a, b string) bool { return a == b }

// ServerHost returns the part of a valid server identifier after the scheme.
func ServerHost(id string) string {
	_, rest, ok := strings.Cut(id, "://")
	if !ok {
		return ""
	}
	return rest
}

// validDomainName checks a name against the host rules of a server identifier:
// lowercase LDH labels, ACE form for internationalized names. A name whose last
// label is numeric is refused: it would be read as an IP address (2130706433,
// 0x7f.0.0.1) rather than as a domain.
func validDomainName(host string) error {
	if host == "" {
		return errors.New("host is empty")
	}
	if len(host) > 253 {
		return errors.New("host is longer than 253 characters")
	}
	labels := strings.Split(host, ".")
	for _, l := range labels {
		if l == "" {
			return errors.New("host has an empty label or trailing dot")
		}
		if len(l) > 63 {
			return errors.New("host has a label longer than 63 characters")
		}
		if l[0] == '-' || l[len(l)-1] == '-' {
			return errors.New("host label starts or ends with a hyphen")
		}
		for i := 0; i < len(l); i++ {
			c := l[i]
			switch {
			case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-':
			case c >= 'A' && c <= 'Z':
				return errors.New("host must be lowercase")
			case c == ':':
				return errors.New("identifier must not contain a port")
			case c == '/':
				return errors.New("identifier must not contain a path or trailing slash")
			case c == '?':
				return errors.New("identifier must not contain a query")
			case c == '#':
				return errors.New("identifier must not contain a fragment")
			case c == '@':
				return errors.New("identifier must not contain credentials")
			case c >= 0x80:
				return errors.New("internationalized names must use the ASCII-compatible form")
			default:
				return fmt.Errorf("host contains %q", c)
			}
		}
	}
	last := labels[len(labels)-1]
	if _, err := strconv.ParseUint(last, 10, 64); err == nil || strings.HasPrefix(last, "0x") {
		return errors.New("host must be a domain name, not an IP address")
	}
	return nil
}

// isLoopbackHostPort reports whether s is a loopback host with an optional
// port and nothing else.
func isLoopbackHostPort(s string) bool {
	host, port := s, ""
	if h, p, err := net.SplitHostPort(s); err == nil {
		host, port = h, p
	} else if strings.Contains(s, ":") && !strings.HasPrefix(s, "[") {
		return false
	} else {
		host = strings.Trim(host, "[]")
	}
	if port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return false
		}
	}
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// AgentIdentifier is a parsed `aauth:local@domain`.
type AgentIdentifier struct {
	Local  string
	Domain string
}

// String returns the identifier's exact form.
func (a AgentIdentifier) String() string { return "aauth:" + a.Local + "@" + a.Domain }

const maxAgentLocal = 255

var agentLocalRE = regexp.MustCompile(`^[A-Za-z0-9._+\-]+$`)

// ParseAgentIdentifier parses and validates an agent identifier under the
// production rules.
func ParseAgentIdentifier(id string) (AgentIdentifier, error) {
	return IdentifierPolicy{}.ParseAgentIdentifier(id)
}

// ValidateAgentIdentifier validates an agent identifier under the production
// rules (protocol, Agent Identifiers): its form and its character set. It reads nothing
// into a `+`, since "verifiers rely on parent_agent, not on parsing the local part". The
// naming rules an agent provider follows when it mints one are ValidateTopLevelAgentIdentifier
// and ValidateSubAgentIdentifier.
func ValidateAgentIdentifier(id string) error {
	_, err := ParseAgentIdentifier(id)
	return err
}

// ParseAgentIdentifier parses and validates an agent identifier under the
// policy. The local part keeps its case: "MUST NOT case-fold the local part".
func (p IdentifierPolicy) ParseAgentIdentifier(id string) (AgentIdentifier, error) {
	rest, ok := strings.CutPrefix(id, "aauth:")
	if !ok {
		return AgentIdentifier{}, idErr("agent identifier %q must use the aauth scheme", id)
	}
	local, domain, ok := strings.Cut(rest, "@")
	if !ok || strings.Contains(domain, "@") {
		return AgentIdentifier{}, idErr("agent identifier %q must have the form aauth:local@domain", id)
	}
	if local == "" {
		return AgentIdentifier{}, idErr("agent identifier %q has an empty local part", id)
	}
	if len(local) > maxAgentLocal {
		return AgentIdentifier{}, idErr("agent identifier local part exceeds %d characters", maxAgentLocal)
	}
	if !agentLocalRE.MatchString(local) {
		return AgentIdentifier{}, idErr("agent identifier %q has an invalid local part", id)
	}
	if p.AllowInsecureLoopback && isLoopbackHostPort(domain) {
		return AgentIdentifier{Local: local, Domain: domain}, nil
	}
	if err := validDomainName(domain); err != nil {
		return AgentIdentifier{}, idErr("agent identifier %q: %v", id, err)
	}
	return AgentIdentifier{Local: local, Domain: domain}, nil
}

// ValidateAgentIdentifier validates an agent identifier under the policy.
func (p IdentifierPolicy) ValidateAgentIdentifier(id string) error {
	_, err := p.ParseAgentIdentifier(id)
	return err
}

// ValidateTopLevelAgentIdentifier additionally applies "A top-level agent's
// local part MUST NOT contain +".
func (p IdentifierPolicy) ValidateTopLevelAgentIdentifier(id string) error {
	a, err := p.ParseAgentIdentifier(id)
	if err != nil {
		return err
	}
	if strings.Contains(a.Local, "+") {
		return idErr("top-level agent identifier %q must not contain '+'", id)
	}
	return nil
}

// ValidateSubAgentIdentifier applies the naming rule an agent provider follows:
// the local part is the parent's local part, `+`, and a non-empty discriminator,
// under the same domain. Verifiers must not use this for protocol decisions;
// the parent_agent claim is authoritative (protocol, Agent Identifiers).
func (p IdentifierPolicy) ValidateSubAgentIdentifier(id, parent string) error {
	a, err := p.ParseAgentIdentifier(id)
	if err != nil {
		return err
	}
	if err := p.ValidateTopLevelAgentIdentifier(parent); err != nil {
		return err
	}
	pa, _ := p.ParseAgentIdentifier(parent)
	disc, ok := strings.CutPrefix(a.Local, pa.Local+"+")
	if !ok || disc == "" || strings.Contains(disc, "+") || a.Domain != pa.Domain {
		return idErr("%q is not named as a sub-agent of %q", id, parent)
	}
	return nil
}

// SameAgentIdentifier compares agent identifiers: "MUST perform exact string
// comparison on agent identifiers (case-sensitive)".
func SameAgentIdentifier(a, b string) bool { return a == b }

// AgentBelongsToIssuer reports whether the identifier's domain is the host of
// iss: "aauth:local@domain, where domain is the AP's domain", and "An AP is
// named in both things it issues". Under a loopback policy an identifier may
// name the host with or without the port.
func (p IdentifierPolicy) AgentBelongsToIssuer(agentID, iss string) bool {
	a, err := p.ParseAgentIdentifier(agentID)
	if err != nil {
		return false
	}
	host := ServerHost(iss)
	if a.Domain == host {
		return true
	}
	if p.AllowInsecureLoopback && isLoopbackHostPort(host) {
		if h, _, err := net.SplitHostPort(host); err == nil {
			return a.Domain == h
		}
	}
	return false
}

// ValidateEndpointURL applies the rule for auth_token_endpoint and its
// siblings: https, with no query and no fragment (protocol, Endpoint and Other
// URLs).
func ValidateEndpointURL(raw string) error {
	return IdentifierPolicy{}.ValidateEndpointURL(raw)
}

// ValidateEndpointURL applies the endpoint URL rule under the policy.
func (p IdentifierPolicy) ValidateEndpointURL(raw string) error {
	u, err := p.parseHTTPS(raw)
	if err != nil {
		return err
	}
	if u.RawQuery != "" || u.ForceQuery || strings.Contains(raw, "?") {
		return idErr("endpoint URL %q must not contain a query", raw)
	}
	if u.Fragment != "" || strings.Contains(raw, "#") {
		return idErr("endpoint URL %q must not contain a fragment", raw)
	}
	return nil
}

// ValidateHTTPSURL applies the rule for jwks_uri, tos_uri and the like: https.
func ValidateHTTPSURL(raw string) error {
	return IdentifierPolicy{}.ValidateHTTPSURL(raw)
}

// ValidateHTTPSURL applies the https URL rule under the policy.
func (p IdentifierPolicy) ValidateHTTPSURL(raw string) error {
	_, err := p.parseHTTPS(raw)
	return err
}

func (p IdentifierPolicy) parseHTTPS(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil, idErr("%q is not an absolute URL", raw)
	}
	if u.User != nil {
		return nil, idErr("URL %q must not contain credentials", raw)
	}
	if u.Scheme == "https" {
		return u, nil
	}
	if p.AllowInsecureLoopback && u.Scheme == "http" && isLoopbackHostPort(u.Host) {
		return u, nil
	}
	return nil, idErr("URL %q must use https", raw)
}
