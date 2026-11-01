package wallets

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// siweMessage is a Sign-In with Ethereum message (EIP-4361). The server
// builds it for the challenge; clients may also build their own with a SIWE
// library, so verification parses whatever was signed and checks every field.
type siweMessage struct {
	Scheme         string
	Domain         string
	Address        string
	Statement      string
	URI            string
	Version        string
	ChainID        int64
	Nonce          string
	IssuedAt       time.Time
	ExpirationTime *time.Time
	NotBefore      *time.Time
	RequestID      string
	Resources      []string
}

const siwePreamble = " wants you to sign in with your Ethereum account:"

// String renders the message in the exact EIP-4361 layout.
func (m siweMessage) String() string {
	var b strings.Builder
	if m.Scheme != "" {
		b.WriteString(m.Scheme + "://")
	}
	b.WriteString(m.Domain + siwePreamble + "\n")
	b.WriteString(m.Address + "\n\n")
	if m.Statement != "" {
		b.WriteString(m.Statement + "\n")
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "URI: %s\nVersion: %s\nChain ID: %d\nNonce: %s\nIssued At: %s",
		m.URI, m.Version, m.ChainID, m.Nonce, m.IssuedAt.UTC().Format(time.RFC3339))
	if m.ExpirationTime != nil {
		b.WriteString("\nExpiration Time: " + m.ExpirationTime.UTC().Format(time.RFC3339))
	}
	if m.NotBefore != nil {
		b.WriteString("\nNot Before: " + m.NotBefore.UTC().Format(time.RFC3339))
	}
	if m.RequestID != "" {
		b.WriteString("\nRequest ID: " + m.RequestID)
	}
	if len(m.Resources) > 0 {
		b.WriteString("\nResources:")
		for _, r := range m.Resources {
			b.WriteString("\n- " + r)
		}
	}
	return b.String()
}

var errSIWEFormat = errors.New("not a Sign-In with Ethereum message")

// parseSIWE parses an EIP-4361 message. It accepts the optional scheme
// prefix, CRLF line endings, and both blank-line layouts libraries emit when
// there is no statement.
func parseSIWE(raw string) (*siweMessage, error) {
	if len(raw) > 8<<10 {
		return nil, errSIWEFormat
	}
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	if len(lines) < 7 {
		return nil, errSIWEFormat
	}
	m := &siweMessage{}
	head, ok := strings.CutSuffix(lines[0], siwePreamble)
	if !ok || head == "" {
		return nil, errSIWEFormat
	}
	if scheme, rest, found := strings.Cut(head, "://"); found {
		m.Scheme, head = scheme, rest
	}
	m.Domain = head
	m.Address = lines[1]
	i := 2
	if lines[i] != "" {
		return nil, errSIWEFormat
	}
	i++
	if i < len(lines) && lines[i] != "" && !strings.HasPrefix(lines[i], "URI: ") {
		m.Statement = lines[i]
		i++
	}
	for i < len(lines) && lines[i] == "" {
		i++
	}
	fields := map[string]string{}
	order := []string{"URI", "Version", "Chain ID", "Nonce", "Issued At", "Expiration Time", "Not Before", "Request ID"}
	pos := 0
	for ; i < len(lines); i++ {
		line := lines[i]
		if line == "Resources:" {
			for i++; i < len(lines); i++ {
				r, ok := strings.CutPrefix(lines[i], "- ")
				if !ok {
					return nil, errSIWEFormat
				}
				m.Resources = append(m.Resources, r)
			}
			break
		}
		key, val, found := strings.Cut(line, ": ")
		if !found {
			return nil, errSIWEFormat
		}
		// Fields appear in the spec's order, each at most once.
		for pos < len(order) && order[pos] != key {
			pos++
		}
		if pos == len(order) {
			return nil, fmt.Errorf("unexpected or repeated field %q", key)
		}
		fields[key] = val
		pos++
	}
	for _, req := range []string{"URI", "Version", "Chain ID", "Nonce", "Issued At"} {
		if fields[req] == "" {
			return nil, fmt.Errorf("missing %s", req)
		}
	}
	m.URI, m.Version, m.Nonce, m.RequestID = fields["URI"], fields["Version"], fields["Nonce"], fields["Request ID"]
	id, err := strconv.ParseInt(fields["Chain ID"], 10, 64)
	if err != nil || id <= 0 {
		return nil, errors.New("invalid Chain ID")
	}
	m.ChainID = id
	if m.IssuedAt, err = time.Parse(time.RFC3339Nano, fields["Issued At"]); err != nil {
		return nil, errors.New("invalid Issued At")
	}
	if v := fields["Expiration Time"]; v != "" {
		t, err := time.Parse(time.RFC3339Nano, v)
		if err != nil {
			return nil, errors.New("invalid Expiration Time")
		}
		m.ExpirationTime = &t
	}
	if v := fields["Not Before"]; v != "" {
		t, err := time.Parse(time.RFC3339Nano, v)
		if err != nil {
			return nil, errors.New("invalid Not Before")
		}
		m.NotBefore = &t
	}
	return m, nil
}

// siweExpect is what the server requires of a signed message.
type siweExpect struct {
	Domain  string
	URI     string
	Address Address
	ChainID int64
	Nonce   string
}

// clockSkew tolerates small client clock differences on Issued At / Not Before.
const clockSkew = time.Minute

// verify checks every field against the challenge. It returns a short
// reason suitable for the error details.
func (m *siweMessage) verify(want siweExpect, now time.Time) error {
	if !strings.EqualFold(m.Domain, want.Domain) {
		return fmt.Errorf("domain %q does not match %q", m.Domain, want.Domain)
	}
	if m.Scheme != "" && m.Scheme != "https" && m.Scheme != "http" {
		return fmt.Errorf("scheme %q is not allowed", m.Scheme)
	}
	if strings.TrimRight(m.URI, "/") != strings.TrimRight(want.URI, "/") {
		return fmt.Errorf("URI %q does not match %q", m.URI, want.URI)
	}
	if m.Version != "1" {
		return fmt.Errorf("version %q is not 1", m.Version)
	}
	a, err := ParseAddress(m.Address)
	if err != nil || a != want.Address {
		return errors.New("address does not match the challenge")
	}
	if m.ChainID != want.ChainID {
		return fmt.Errorf("chain id %d does not match %d", m.ChainID, want.ChainID)
	}
	if m.Nonce != want.Nonce {
		return errors.New("nonce does not match")
	}
	if m.IssuedAt.After(now.Add(clockSkew)) {
		return errors.New("issued in the future")
	}
	if m.ExpirationTime != nil && !now.Before(*m.ExpirationTime) {
		return errors.New("message expired")
	}
	if m.NotBefore != nil && now.Add(clockSkew).Before(*m.NotBefore) {
		return errors.New("message not valid yet")
	}
	return nil
}
