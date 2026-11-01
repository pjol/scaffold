// Package webhooks verifies inbound provider signatures and makes event
// handling exactly-once. Every webhook handler: verify the signature (refuse
// when the secret is unset), call Once with the provider's event id, and
// re-fetch current state from the provider rather than trusting the payload
// when money or permissions depend on it.
package webhooks

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"scaffoldapp/backend/internal/db"
)

// Once runs fn in a transaction unless source:eventID was already handled.
// The event is recorded in the same transaction, so a failed fn leaves it
// unrecorded and the provider's retry runs it again. duplicate reports a
// skipped replay.
func Once(ctx context.Context, pool *pgxpool.Pool, source, eventID string, fn func(tx pgx.Tx) error) (duplicate bool, err error) {
	err = db.WithTx(ctx, pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `INSERT INTO webhook_events (id, source) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			source+":"+eventID, source)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			duplicate = true
			return nil
		}
		return fn(tx)
	})
	return duplicate, err
}

// VerifySvix checks a Svix signature (Clerk, Resend and others): headers
// svix-id, svix-timestamp, svix-signature against a "whsec_…" secret, with 5
// minutes of allowed skew.
func VerifySvix(secret string, h http.Header, body []byte, now time.Time) bool {
	if secret == "" {
		return false
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(secret, "whsec_"))
	if err != nil {
		return false
	}
	id, ts, sigs := h.Get("svix-id"), h.Get("svix-timestamp"), h.Get("svix-signature")
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || id == "" {
		return false
	}
	if d := now.Sub(time.Unix(sec, 0)); d > 5*time.Minute || d < -5*time.Minute {
		return false
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(id + "." + ts + "." + string(body)))
	want := mac.Sum(nil)
	for _, part := range strings.Fields(sigs) {
		v, sig, ok := strings.Cut(part, ",")
		if !ok || v != "v1" {
			continue
		}
		if got, err := base64.StdEncoding.DecodeString(sig); err == nil && hmac.Equal(got, want) {
			return true
		}
	}
	return false
}

// VerifyHexHMAC checks a hex-encoded HMAC-SHA256 of body (the common
// "X-Signature: <hex>" scheme). An empty secret never verifies.
func VerifyHexHMAC(secret, signature string, body []byte) bool {
	if secret == "" || signature == "" {
		return false
	}
	got, err := hex.DecodeString(strings.TrimPrefix(signature, "sha256="))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(got, mac.Sum(nil))
}
