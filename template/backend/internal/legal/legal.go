// Package legal holds the current Terms and Privacy versions and the consent
// gate. The documents ship in packages/shared/src/legal under the same
// version strings; bump both together (concrete/compliance.md). A bump makes
// every user's consent_required true until they accept again.
package legal

import (
	"net/http"
	"strings"
	"sync"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
)

const (
	TermsVersion   = "2026-01-01"
	PrivacyVersion = "2026-01-01"
)

// Current reports whether the accepted versions are the current ones.
func Current(termsAccepted, privacyAccepted string) bool {
	return termsAccepted == TermsVersion && privacyAccepted == PrivacyVersion
}

// ErrConsentRequired is the 403 every gated call returns until the caller
// accepts the current versions (POST /v1/me/consents).
func ErrConsentRequired() *httpx.Error {
	e := httpx.NewError(http.StatusForbidden, "consent_required",
		"Accept the latest Terms of Service and Privacy Policy to continue.")
	e.Details = map[string]any{"terms_version": TermsVersion, "privacy_version": PrivacyVersion}
	return e
}

var (
	mu     sync.RWMutex
	exempt = []string{"/v1/me/consents", "/v1/uploads"} // uploads re-check per purpose
)

// Exempt lets mutations on paths with these prefixes through the gate. Use it
// in a module's New for calls that must work on the consent screen: push
// registration, sign-out housekeeping, contacting support, deleting the
// account.
func Exempt(prefixes ...string) {
	mu.Lock()
	exempt = append(exempt, prefixes...)
	mu.Unlock()
}

func isExempt(path string) bool {
	mu.RLock()
	defer mu.RUnlock()
	for _, p := range exempt {
		if path == p || strings.HasPrefix(path, p+"/") {
			return true
		}
	}
	return false
}

// RequireConsent gates mutations (anything but GET/HEAD/OPTIONS) on the
// authenticated API until the caller has accepted the current versions.
// Reads are never gated: clients need GET /v1/me to render the consent
// screen. Mount after auth middleware.
func RequireConsent(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := auth.FromContext(r.Context())
		switch {
		case id == nil, Current(id.TermsVersion, id.PrivacyVersion),
			r.Method == http.MethodGet, r.Method == http.MethodHead, r.Method == http.MethodOptions,
			r.Method == http.MethodDelete && r.URL.Path == "/v1/me",
			isExempt(strings.TrimSuffix(r.URL.Path, "/")):
			next.ServeHTTP(w, r)
			return
		}
		httpx.Fail(w, r, ErrConsentRequired())
	})
}
