package connectors

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/webhooks"
)

// Webhook ingress for platforms (optional for features; polling with the
// person's Client is the default). POST /webhooks/connectors/{provider}
// verifies the platform's signature — refusing when its secret is unset —
// answers the platform's handshake where it has one (Zoom URL validation,
// Microsoft Graph validationToken, Notion verification), drops replays
// (webhooks.Once on the delivery id), and hands the event to every module
// implementing app.ConnectionWebhookHandler for that provider, with the
// connections whose account the event names when it can tell.

const maxWebhookBody = 1 << 20

func (s *Service) MountWebhooks(r chi.Router) {
	r.Post("/connectors/{provider}", s.webhook)
}

func (s *Service) webhook(w http.ResponseWriter, r *http.Request) {
	p := s.providers[chi.URLParam(r, "provider")]
	if p == nil || p.Webhook == nil {
		http.NotFound(w, r)
		return
	}
	env := providerEnv()[p.ID]
	secret := env["webhook"]
	if p.Webhook.Verify == "hubspot_v3" {
		secret = p.clientSecret
	}
	// Microsoft Graph validates a subscription with ?validationToken= (echo it as text).
	if p.Webhook.Verify == "microsoft_state" {
		if vt := r.URL.Query().Get("validationToken"); vt != "" {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = io.WriteString(w, truncate(vt, 1024))
			return
		}
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody+1))
	if err != nil || len(body) > maxWebhookBody {
		http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
		return
	}
	if secret == "" {
		slog.WarnContext(r.Context(), "connectors: webhook refused; secret unset", "provider", p.ID)
		http.Error(w, "webhooks are not set up", http.StatusServiceUnavailable)
		return
	}
	// Handshakes that carry no event.
	switch p.Webhook.Verify {
	case "zoom_hmac":
		var probe struct {
			Event   string `json:"event"`
			Payload struct {
				PlainToken string `json:"plainToken"`
			} `json:"payload"`
		}
		if json.Unmarshal(body, &probe) == nil && probe.Event == "endpoint.url_validation" && probe.Payload.PlainToken != "" {
			if !verifyZoom(secret, r.Header, body) {
				http.Error(w, "bad signature", http.StatusUnauthorized)
				return
			}
			mac := hmac.New(sha256.New, []byte(secret))
			mac.Write([]byte(probe.Payload.PlainToken))
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"plainToken": probe.Payload.PlainToken, "encryptedToken": hex.EncodeToString(mac.Sum(nil))})
			return
		}
	case "notion_hmac":
		var probe struct {
			VerificationToken string `json:"verification_token"`
		}
		if json.Unmarshal(body, &probe) == nil && probe.VerificationToken != "" && r.Header.Get("X-Notion-Signature") == "" {
			// Notion sends the token once when a subscription is created; an
			// operator copies it from the log into NOTION_WEBHOOK_SECRET.
			slog.InfoContext(r.Context(), "connectors: Notion webhook verification received; set NOTION_WEBHOOK_SECRET to the token shown in Notion's integration settings")
			w.WriteHeader(http.StatusOK)
			return
		}
	}
	if !verifyWebhook(p.Webhook.Verify, secret, r, body) {
		slog.WarnContext(r.Context(), "connectors: webhook signature refused", "provider", p.ID)
		http.Error(w, "bad signature", http.StatusUnauthorized)
		return
	}
	ev := app.ConnectionEvent{Provider: p.ID, Header: r.Header.Clone(), Body: json.RawMessage(body)}
	ev.Type, ev.DeliveryID = webhookMeta(p.ID, r.Header, body)
	ev.Header.Del("Authorization")
	if account := webhookAccount(p.ID, body); account != "" {
		ev.Connections = s.connectionsForAccount(r.Context(), p.ID, account)
	}
	var handlers []app.ConnectionWebhook
	for _, m := range s.d.Modules {
		if h, ok := m.(app.ConnectionWebhookHandler); ok {
			for _, cw := range h.ConnectionWebhooks() {
				if cw.Provider == p.ID && cw.Handle != nil {
					handlers = append(handlers, cw)
				}
			}
		}
	}
	if len(handlers) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	id := ev.DeliveryID
	if id == "" {
		sum := sha256.Sum256(body)
		id = hex.EncodeToString(sum[:16])
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 20*time.Second)
	defer cancel()
	dup, err := webhooks.Once(ctx, s.d.Pool, "connectors:"+p.ID, id, func(pgx.Tx) error {
		for _, h := range handlers {
			if err := h.Handle(ctx, ev); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		slog.ErrorContext(ctx, "connectors: webhook handler failed", "provider", p.ID, "type", ev.Type, "err", err)
		http.Error(w, "handler failed", http.StatusInternalServerError)
		return
	}
	if dup {
		slog.DebugContext(ctx, "connectors: webhook replay skipped", "provider", p.ID, "delivery", id)
	}
	w.WriteHeader(http.StatusNoContent)
}

func verifyWebhook(kind, secret string, r *http.Request, body []byte) bool {
	h := r.Header
	switch kind {
	case "github_hmac": // X-Hub-Signature-256: sha256=<hex HMAC of the body>
		return hmacHexEqual(secret, body, strings.TrimPrefix(h.Get("X-Hub-Signature-256"), "sha256="))
	case "gitlab_token": // X-Gitlab-Token: the secret token set on the hook
		got := h.Get("X-Gitlab-Token")
		return got != "" && subtle.ConstantTimeCompare([]byte(got), []byte(secret)) == 1
	case "notion_hmac": // X-Notion-Signature: sha256=<hex HMAC of the body>
		return hmacHexEqual(secret, body, strings.TrimPrefix(h.Get("X-Notion-Signature"), "sha256="))
	case "zoom_hmac":
		return verifyZoom(secret, h, body)
	case "hubspot_v3": // base64 HMAC(client secret, method + URI + body + timestamp), 5 minutes
		ts, err := strconv.ParseInt(h.Get("X-HubSpot-Request-Timestamp"), 10, 64)
		if err != nil || absDur(time.Since(time.UnixMilli(ts))) > 5*time.Minute {
			return false
		}
		uri := "https://" + r.Host + r.URL.RequestURI()
		if fwd := h.Get("X-Forwarded-Host"); fwd != "" {
			uri = "https://" + fwd + r.URL.RequestURI()
		}
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(r.Method + uri + string(body) + h.Get("X-HubSpot-Request-Timestamp")))
		want := base64.StdEncoding.EncodeToString(mac.Sum(nil))
		got := h.Get("X-HubSpot-Signature-v3")
		return got != "" && hmac.Equal([]byte(got), []byte(want))
	case "google_channel": // X-Goog-Channel-Token: the token set when the watch was created
		got := h.Get("X-Goog-Channel-Token")
		return got != "" && subtle.ConstantTimeCompare([]byte(got), []byte(secret)) == 1
	case "microsoft_state": // every notification carries the clientState set on the subscription
		var payload struct {
			Value []struct {
				ClientState string `json:"clientState"`
			} `json:"value"`
		}
		if json.Unmarshal(body, &payload) != nil || len(payload.Value) == 0 {
			return false
		}
		for _, v := range payload.Value {
			if subtle.ConstantTimeCompare([]byte(v.ClientState), []byte(secret)) != 1 {
				return false
			}
		}
		return true
	}
	return false
}

// verifyZoom checks x-zm-signature: v0=<hex HMAC("v0:"+timestamp+":"+body)>.
func verifyZoom(secret string, h http.Header, body []byte) bool {
	ts := h.Get("x-zm-request-timestamp")
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || absDur(time.Since(time.Unix(sec, 0))) > 5*time.Minute {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("v0:" + ts + ":" + string(body)))
	want := "v0=" + hex.EncodeToString(mac.Sum(nil))
	got := h.Get("x-zm-signature")
	return got != "" && hmac.Equal([]byte(got), []byte(want))
}

func hmacHexEqual(secret string, body []byte, gotHex string) bool {
	got, err := hex.DecodeString(gotHex)
	if err != nil || len(got) == 0 {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(got, mac.Sum(nil))
}

func absDur(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// webhookMeta reads the event name and delivery id each platform sends.
func webhookMeta(provider string, h http.Header, body []byte) (typ, delivery string) {
	var m map[string]any
	_ = json.Unmarshal(body, &m)
	switch provider {
	case "github":
		return h.Get("X-GitHub-Event"), h.Get("X-GitHub-Delivery")
	case "gitlab":
		return h.Get("X-Gitlab-Event"), firstNonEmpty(h.Get("X-Gitlab-Webhook-UUID"), h.Get("X-Gitlab-Event-UUID"))
	case "google":
		return h.Get("X-Goog-Resource-State"), h.Get("X-Goog-Channel-Id") + ":" + h.Get("X-Goog-Message-Number")
	case "zoom":
		return jsonString(m, "event"), ""
	case "notion":
		return jsonString(m, "type"), jsonString(m, "id")
	case "microsoft":
		return "change_notification", ""
	case "hubspot":
		return "", ""
	}
	return "", ""
}

// webhookAccount is the platform account an event is about, when the
// payload says (matched against connections' account_id).
func webhookAccount(provider string, body []byte) string {
	var m map[string]any
	if json.Unmarshal(body, &m) != nil {
		return ""
	}
	switch provider {
	case "github":
		return jsonPath(m, "sender.id")
	case "gitlab":
		return jsonPath(m, "user_id")
	case "notion":
		return jsonPath(m, "workspace_id")
	}
	return ""
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

func (s *Service) connectionsForAccount(ctx context.Context, provider, accountID string) []app.ConnectionOwner {
	rows, err := s.d.Pool.Query(ctx, `SELECT id, user_id FROM connector_platform_connections
		WHERE provider = $1 AND account_id = $2 AND status = 'active' LIMIT 100`, provider, accountID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []app.ConnectionOwner
	for rows.Next() {
		var id, uid uuid.UUID
		if rows.Scan(&id, &uid) == nil {
			out = append(out, app.ConnectionOwner{ConnectionID: id.String(), UserID: uid})
		}
	}
	return out
}
