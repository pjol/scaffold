package aauth

import (
	"errors"
	"testing"
)

// RFC 9530 appendices B.1, B.2 and B.3, and the digests in RFC 9421's test messages.
func TestContentDigestRFCExamples(t *testing.T) {
	cases := []struct {
		name    string
		content string
		field   string
	}{
		{"RFC 9530 B.1", "{\"hello\": \"world\"}\n", "sha-256=:RK/0qy18MlBSVnWgjwz6lZEWjP/lF5HF9bvEF8FabDg=:"},
		{"RFC 9530 B.2 empty content", "", "sha-256=:47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU=:"},
		{"RFC 9530 B.3 partial content", "\"world\"}\n", "sha-256=:jjcgBDWNAtbYUXI37CVG3gRuGOAjaaDRGpIUFsdyepQ=:"},
		{"RFC 9421 B.2 test-request", `{"hello": "world"}`, rfcTestRequestDigest},
		{"RFC 9421 B.2 test-response", `{"message": "good dog"}`, rfcTestResponseDigest},
		{"RFC 9421 section 2.4", `{"busy": true, "message": "Your call is very important to us"}`, rfcBusyDigest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := VerifyContentDigest([]string{c.field}, []byte(c.content)); err != nil {
				t.Fatal(err)
			}
			if err := VerifyContentDigest([]string{c.field}, []byte(c.content+"x")); !errors.Is(err, ErrDigestMismatch) {
				t.Fatalf("tampered content: %v", err)
			}
		})
	}
	if got := ContentDigest([]byte("{\"hello\": \"world\"}\n")); got != "sha-256=:RK/0qy18MlBSVnWgjwz6lZEWjP/lF5HF9bvEF8FabDg=:" {
		t.Fatalf("ContentDigest = %s", got)
	}
}

func TestContentDigestField(t *testing.T) {
	body := []byte(`{"hello": "world"}`)
	sha256Field := ContentDigest(body)
	// Every supported algorithm present must match, and unknown ones are ignored.
	if err := VerifyContentDigest([]string{sha256Field + ", " + rfcTestRequestDigest}, body); err != nil {
		t.Error(err)
	}
	if err := VerifyContentDigest([]string{"unixsum=:AAAA:, " + sha256Field}, body); err != nil {
		t.Error(err)
	}
	// One good and one bad is a mismatch: an attacker can't add a passing digest beside a bad one.
	if err := VerifyContentDigest([]string{sha256Field + ", sha-512=:AAAA:"}, body); !errors.Is(err, ErrDigestMismatch) {
		t.Errorf("mixed: %v", err)
	}
	if err := VerifyContentDigest([]string{"md5=:AAAA:"}, body); !errors.Is(err, ErrDigestUnsupported) {
		t.Errorf("unsupported: %v", err)
	}
	if err := VerifyContentDigest(nil, body); !errors.Is(err, ErrDigestUnsupported) {
		t.Errorf("absent: %v", err)
	}
	for _, bad := range []string{`sha-256="not bytes"`, "sha-256=1", "sha-256=(a b)", "((("} {
		if err := VerifyContentDigest([]string{bad}, body); !errors.Is(err, ErrDigestMalformed) {
			t.Errorf("%s: %v", bad, err)
		}
	}
}
