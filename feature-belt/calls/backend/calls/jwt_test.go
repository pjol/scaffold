package calls

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHS256RoundTrip(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	now := time.Now()
	tok, err := signHS256(secret, liveKitClaims{registeredClaims: registeredClaims{Issuer: "key", Subject: "u:1", ExpiresAt: now.Add(time.Minute).Unix()}, Name: "Sam"})
	if err != nil {
		t.Fatal(err)
	}
	var got liveKitClaims
	if err := verifyHS256(secret, tok, now, &got); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if got.Issuer != "key" || got.Subject != "u:1" || got.Name != "Sam" {
		t.Fatalf("claims = %+v", got)
	}
}

func TestHS256Rejects(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	now := time.Now()
	valid, _ := signHS256(secret, registeredClaims{Issuer: "key", ExpiresAt: now.Add(time.Minute).Unix()})
	expired, _ := signHS256(secret, registeredClaims{Issuer: "key", ExpiresAt: now.Add(-time.Hour).Unix()})
	future, _ := signHS256(secret, registeredClaims{Issuer: "key", ExpiresAt: now.Add(2 * time.Hour).Unix(), NotBefore: now.Add(time.Hour).Unix()})
	noExp, _ := signHS256(secret, registeredClaims{Issuer: "key"})
	parts := strings.Split(valid, ".")
	none := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`)) + "." + parts[1] + "."
	rs := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`)) + "." + parts[1] + "." + parts[2]
	tampered := parts[0] + "." + base64.RawURLEncoding.EncodeToString([]byte(`{"iss":"other","exp":9999999999}`)) + "." + parts[2]
	cases := map[string]struct {
		secret []byte
		token  string
		want   error
	}{
		"wrong secret": {[]byte("another-secret-another-secret-xx"), valid, errTokenSignature},
		"empty secret": {nil, valid, errTokenSignature},
		"expired":      {secret, expired, errTokenExpired},
		"not yet":      {secret, future, errTokenExpired},
		"no exp":       {secret, noExp, errTokenExpired},
		"alg none":     {secret, none, errTokenMalformed},
		"alg RS256":    {secret, rs, errTokenAlg},
		"tampered":     {secret, tampered, errTokenSignature},
		"garbage":      {secret, "a.b", errTokenMalformed},
	}
	for name, c := range cases {
		if err := verifyHS256(c.secret, c.token, now, nil); err != c.want {
			t.Errorf("%s: err = %v, want %v", name, err, c.want)
		}
	}
}

func TestLiveKitWebhookVerification(t *testing.T) {
	lk := newLiveKit("ws://127.0.0.1:7880", "http://127.0.0.1:7880", "APIkey", "0123456789abcdef0123456789abcdef")
	body := []byte(`{"event":"participant_joined","id":"EV_1","createdAt":"1793479917","room":{"sid":"RM_1","name":"room"},"participant":{"sid":"PA_1","identity":"u:x","name":"Sam"}}`)
	sum := sha256.Sum256(body)
	sign := func(key string, hash string) string {
		tok, _ := signHS256(lk.secret, liveKitClaims{registeredClaims: registeredClaims{Issuer: key, ExpiresAt: time.Now().Add(time.Minute).Unix()}, Sha256: hash})
		return tok
	}
	good := sign("APIkey", base64.StdEncoding.EncodeToString(sum[:]))
	r := httptest.NewRequest("POST", "/webhooks/livekit", nil)
	r.Header.Set("Authorization", good)
	ev, err := lk.ParseWebhook(r, body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if ev.Kind != EventParticipantJoined || ev.ID != "EV_1" || ev.Room != "room" || ev.RoomSID != "RM_1" || ev.Identity != "u:x" || ev.ParticipantSID != "PA_1" || ev.At.Unix() != 1793479917 {
		t.Fatalf("event = %+v", ev)
	}
	for name, auth := range map[string]string{
		"missing":      "",
		"other key":    sign("other", base64.StdEncoding.EncodeToString(sum[:])),
		"body changed": sign("APIkey", base64.StdEncoding.EncodeToString(make([]byte, 32))),
	} {
		r := httptest.NewRequest("POST", "/webhooks/livekit", nil)
		r.Header.Set("Authorization", auth)
		if _, err := lk.ParseWebhook(r, body); err != errWebhookAuth {
			t.Errorf("%s: err = %v, want errWebhookAuth", name, err)
		}
	}
}

func TestCodes(t *testing.T) {
	for range 200 {
		c := newCode()
		if n, ok := normalizeCode(c); !ok || n != c {
			t.Fatalf("code %q did not round-trip (%q)", c, n)
		}
	}
	for in, want := range map[string]string{
		"ABC-DEFG-HIJ": "abc-defg-hij",
		"abcdefghij":   "abc-defg-hij",
		"https://app.example.com/meet/abc-defg-hij": "abc-defg-hij",
	} {
		if got, ok := normalizeCode(in); !ok || got != want {
			t.Errorf("normalizeCode(%q) = %q, %v", in, got, ok)
		}
	}
	for _, bad := range []string{"", "abc-defg-hi", "abc-defg-hi1", "abc_defg_hij"} {
		if _, ok := normalizeCode(bad); ok {
			t.Errorf("normalizeCode(%q) accepted", bad)
		}
	}
}
