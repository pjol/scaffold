package aauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// The deferred response `status` values (protocol, Pending Response).
const (
	PendingStatusPending     = "pending"
	PendingStatusInteracting = "interacting"
)

// Pending describes a 202 Accepted deferred response.
type Pending struct {
	// Location is the pending URL, "on the same origin as the responding server". Use a
	// path, or an absolute URL on the server's origin.
	Location string
	// RetryAfter is "Seconds the agent SHOULD wait before polling. 0 means retry
	// immediately."
	RetryAfter int
	// Status is the body's status. Empty means pending.
	Status string
	// Requirement, when set, is sent as AAuth-Requirement.
	Requirement *Requirement
	// Policy is the identifier policy for a requirement's interaction URL.
	Policy IdentifierPolicy
	// Body holds further members: `clarification` and `timeout`, `required_claims`, or
	// the Supervision `sdi` and `decision`. A value given as `status` is ignored.
	Body map[string]any
}

// WritePending writes the deferred response: 202, Location, Retry-After,
// Cache-Control: no-store, and a JSON body with status. It returns an error, with
// nothing written, when a required header can't be produced.
func WritePending(w http.ResponseWriter, p Pending) error {
	if p.Location == "" {
		return errors.New("aauth: a deferred response needs a Location")
	}
	if p.RetryAfter < 0 {
		return errors.New("aauth: Retry-After must not be negative")
	}
	var requirement string
	if p.Requirement != nil {
		s, err := p.Requirement.FieldWithPolicy(p.Policy)
		if err != nil {
			return err
		}
		requirement = s
	}
	status := p.Status
	if status == "" {
		status = PendingStatusPending
	}
	body := map[string]any{}
	for k, v := range p.Body {
		body[k] = v
	}
	body["status"] = status
	b, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("aauth: deferred response body: %w", err)
	}
	h := w.Header()
	h.Set("Location", p.Location)
	h.Set("Retry-After", strconv.Itoa(p.RetryAfter))
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Type", ContentTypeJSON)
	if requirement != "" {
		h.Set(HeaderAAuthRequirement, requirement)
	}
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write(b)
	return nil
}

// WriteGone answers a pending URL after its terminal response: "the pending URL MUST
// return 410 Gone".
func WriteGone(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusGone)
}

// ValidatePendingLocation checks a Location against origin: "MUST be on the same origin
// as the responding server". A path is always same-origin; one starting `//` is not a
// path but a network reference to another host.
func ValidatePendingLocation(origin, location string) error {
	if location == "" {
		return errors.New("aauth: pending Location is empty")
	}
	if strings.HasPrefix(location, "/") && !strings.HasPrefix(location, "//") {
		return nil
	}
	u, err := url.Parse(location)
	if err != nil {
		return fmt.Errorf("aauth: pending Location: %w", err)
	}
	if u.Scheme == "" && u.Host == "" {
		return nil
	}
	if u.Scheme+"://"+u.Host != origin {
		return fmt.Errorf("aauth: pending Location %q is not on origin %q", location, origin)
	}
	return nil
}

// ParsePreferWait reads the `wait` preference from Prefer fields (RFC 7240). ok is false
// when there is none or it isn't a non-negative integer. Preference names are
// case-insensitive, and a first occurrence wins, as RFC 7240 section 2 says recipients
// should treat duplicates. A server applies its own cap to the result.
func ParsePreferWait(fieldValues []string) (seconds int, ok bool) {
	for _, field := range fieldValues {
		for _, pref := range splitOutsideQuotes(field, ',') {
			head, _, _ := strings.Cut(pref, ";")
			name, value, hasValue := strings.Cut(head, "=")
			if !strings.EqualFold(strings.TrimSpace(name), "wait") {
				continue
			}
			if !hasValue {
				return 0, false
			}
			value = strings.Trim(strings.TrimSpace(value), `"`)
			n, err := strconv.Atoi(value)
			if err != nil || n < 0 {
				return 0, false
			}
			return n, true
		}
	}
	return 0, false
}

// PreferenceAppliedWait returns the Preference-Applied field value for a wait the server
// honoured.
func PreferenceAppliedWait(seconds int) string { return "wait=" + strconv.Itoa(seconds) }

func splitOutsideQuotes(s string, sep byte) []string {
	var parts []string
	inQuotes, start := false, 0
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '"':
			inQuotes = !inQuotes
		case s[i] == sep && !inQuotes:
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	return append(parts, s[start:])
}
