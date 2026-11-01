package connectors

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/config"
)

func key(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	return k
}

func TestSealRoundTripAndBinding(t *testing.T) {
	s, err := newSealer(key(t))
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	sealed := s.seal("secret-token", aadConnection(id, "access"))
	if bytes.Contains(sealed, []byte("secret-token")) {
		t.Fatal("ciphertext contains the plaintext")
	}
	got, err := s.open(sealed, aadConnection(id, "access"))
	if err != nil || got != "secret-token" {
		t.Fatalf("open = %q, %v", got, err)
	}
	// Moved to another column or row: refused.
	if _, err := s.open(sealed, aadConnection(id, "refresh")); err == nil {
		t.Fatal("opened with another column's additional data")
	}
	if _, err := s.open(sealed, aadConnection(uuid.New(), "access")); err == nil {
		t.Fatal("opened with another row's additional data")
	}
	sealed[len(sealed)-1] ^= 1
	if _, err := s.open(sealed, aadConnection(id, "access")); err == nil {
		t.Fatal("opened a tampered value")
	}
	if s.seal("", "x") != nil {
		t.Fatal("empty plaintext should seal to nil")
	}
	if v, err := s.open(nil, "x"); v != "" || err != nil {
		t.Fatal("nil should open to empty")
	}
}

func TestKeyRotation(t *testing.T) {
	oldKey, newKey := key(t), key(t)
	old, _ := newSealer(oldKey)
	sealed := old.seal("tok", "a")
	rotated, err := newSealer(newKey, oldKey)
	if err != nil {
		t.Fatal(err)
	}
	if v, err := rotated.open(sealed, "a"); err != nil || v != "tok" {
		t.Fatalf("previous key didn't open: %q %v", v, err)
	}
	fresh := rotated.seal("tok2", "a")
	if _, err := old.open(fresh, "a"); err == nil {
		t.Fatal("new values must use the new key")
	}
}

func TestParseKey(t *testing.T) {
	k := key(t)
	for _, raw := range []string{base64.StdEncoding.EncodeToString(k), base64.RawURLEncoding.EncodeToString(k), hex.EncodeToString(k)} {
		got, err := parseKey(raw)
		if err != nil || !bytes.Equal(got, k) {
			t.Errorf("parseKey(%q) = %v", raw, err)
		}
	}
	for _, bad := range []string{"", "short", base64.StdEncoding.EncodeToString(k[:16])} {
		if _, err := parseKey(bad); err == nil {
			t.Errorf("parseKey(%q) accepted", bad)
		}
	}
}

var toolNameRE = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func TestExposedName(t *testing.T) {
	cases := map[string]string{
		"list_issues":  "linear__list_issues",
		"create-issue": "linear__create-issue",
	}
	for in, want := range cases {
		if got := exposedName("linear", in); got != want {
			t.Errorf("exposedName(%q) = %q, want %q", in, got, want)
		}
	}
	long1 := "a_very_long_tool_name_that_goes_on_and_on_past_the_limit_of_providers_one"
	long2 := "a_very_long_tool_name_that_goes_on_and_on_past_the_limit_of_providers_two"
	n1, n2 := exposedName("averylongslugname20", long1), exposedName("averylongslugname20", long2)
	for _, n := range []string{n1, n2, exposedName("linear", "files.read"), exposedName("linear", "検索"), exposedName("x1", "")} {
		if !toolNameRE.MatchString(n) {
			t.Errorf("%q is not a valid tool name", n)
		}
	}
	if n1 == n2 {
		t.Errorf("two long names collapsed into %q", n1)
	}
	if exposedName("linear", "files.read") == exposedName("linear", "files_read") {
		t.Error("files.read and files_read must stay distinct")
	}
}

func TestConfigRefusedOutsideDevelopment(t *testing.T) {
	t.Setenv("CONNECTORS_ENCRYPTION_KEY", "")
	t.Setenv("CONNECTORS_ALLOW_PRIVATE_NETWORKS", "true")
	s := New(&app.Deps{Cfg: &config.Config{Env: "production", PublicAPIURL: "https://api.example.com"}})
	err := config.Check()
	if err == nil || !strings.Contains(err.Error(), "CONNECTORS_ENCRYPTION_KEY is required") || !strings.Contains(err.Error(), "CONNECTORS_ALLOW_PRIVATE_NETWORKS") {
		t.Fatalf("config.Check() = %v", err)
	}
	if s.allowPrivate || s.guard.allowHTTP {
		t.Fatal("production must not allow private networks or http")
	}
	if !s.canUseMetadataDocument() {
		t.Fatal("a public https API should offer the client metadata document")
	}
}
