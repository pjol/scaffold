package channels

// Incoming webhooks: an integration posts into one channel by POSTing
// {"text": "…"} to a secret URL. Owners and admins create them
// (ActManageIntegrations); the URL is shown once and only its hash is kept.
// Messages show the integration's name and go through the content filter;
// @channel and @here are refused. Each hook is rate-limited.

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/moderation"
	"scaffoldapp/backend/internal/orgs"
)

const (
	webhookPath      = "/channels/hooks/"
	webhookPerMinute = 30
	maxWebhooks      = 20 // per channel
	webhookPrefix    = "whk_"
)

// Webhook is an incoming webhook as admins see it (never its secret).
type Webhook struct {
	ID         uuid.UUID  `json:"id"`
	ChannelID  uuid.UUID  `json:"channel_id"`
	Name       string     `json:"name"`
	CreatedBy  *UserRef   `json:"created_by"`
	LastUsedAt *time.Time `json:"last_used_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

// NewWebhook is the create response: the URL, shown once.
type NewWebhook struct {
	Webhook
	URL string `json:"url"`
}

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// webhookChannel loads a channel the caller may manage integrations for.
func (s *Service) webhookChannel(r *http.Request, channelID uuid.UUID) (*access, error) {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	a, err := s.loadAccess(ctx, s.d.Pool, channelID, me)
	if err != nil {
		return nil, err
	}
	if a.direct() {
		return nil, httpx.BadRequest("Webhooks post into channels, not direct messages.")
	}
	ok, err := allowed(ctx, s.d.Pool, PermissionRequest{Action: ActManageIntegrations, OrgID: a.OrgID, UserID: me, OrgRole: a.OrgRole})
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, httpx.Forbidden("Only the " + orgs.Noun() + "'s owners and admins manage webhooks.")
	}
	return a, nil
}

// GET /channels/channels/{id}/webhooks
func (s *Service) listWebhooks(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	if _, err := s.webhookChannel(r, id); err != nil {
		return err
	}
	rows, err := s.d.Pool.Query(r.Context(), `
		SELECT h.id, h.channel_id, h.name, h.last_used_at, h.created_at, h.created_by, u.display_name, u.avatar_url, u.status
		FROM channel_webhooks h LEFT JOIN users u ON u.id = h.created_by
		WHERE h.channel_id = $1 ORDER BY h.created_at, h.id`, id)
	if err != nil {
		return err
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Webhook, error) {
		var h Webhook
		var by *uuid.UUID
		var name, avatar, status *string
		if err := row.Scan(&h.ID, &h.ChannelID, &h.Name, &h.LastUsedAt, &h.CreatedAt, &by, &name, &avatar, &status); err != nil {
			return h, err
		}
		if by != nil {
			u := userRef(*by, deref(name), avatar, deref(status))
			h.CreatedBy = &u
		}
		return h, nil
	})
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": list})
	return nil
}

type webhookReq struct {
	Name string `json:"name"`
}

// POST /channels/channels/{id}/webhooks {name} — 201 with the URL (shown once); audited.
func (s *Service) createWebhook(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req webhookReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	a, err := s.webhookChannel(r, id)
	if err != nil {
		return err
	}
	if a.Archived {
		return errArchived
	}
	name := strings.Join(strings.Fields(req.Name), " ")
	if name == "" || utf8.RuneCountInString(name) > 80 {
		return httpx.BadRequest("Name the integration (1 to 80 characters).")
	}
	if err := checkText(ctx, name); err != nil {
		return err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return err
	}
	token := webhookPrefix + base64.RawURLEncoding.EncodeToString(raw)
	var out NewWebhook
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM channel_webhooks WHERE channel_id = $1`, id).Scan(&n); err != nil {
			return err
		}
		if n >= maxWebhooks {
			return httpx.BadRequest("A channel can have up to 20 webhooks.")
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO channel_webhooks (channel_id, name, token_hash, created_by) VALUES ($1, $2, $3, $4) RETURNING id, created_at`,
			id, name, hashToken(token), me).Scan(&out.ID, &out.CreatedAt); err != nil {
			return err
		}
		return auditLog(ctx, tx, me, "channels.webhook.create", "channels.channel", id.String(), map[string]any{"webhook_id": out.ID, "name": name})
	})
	if err != nil {
		return err
	}
	out.ChannelID, out.Name = id, name
	out.URL = strings.TrimRight(s.d.Cfg.PublicAPIURL, "/") + "/v1" + webhookPath + token
	httpx.JSON(w, http.StatusCreated, out)
	return nil
}

// DELETE /channels/webhooks/{id} — the URL stops working; audited.
func (s *Service) deleteWebhook(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var channelID uuid.UUID
	if err := s.d.Pool.QueryRow(ctx, `SELECT channel_id FROM channel_webhooks WHERE id = $1`, id).Scan(&channelID); err != nil {
		if db.IsNoRows(err) {
			return httpx.NotFound("Webhook not found.")
		}
		return err
	}
	if _, err := s.webhookChannel(r, channelID); err != nil {
		if he, ok := err.(*httpx.Error); ok && he.Status == http.StatusNotFound {
			return httpx.NotFound("Webhook not found.")
		}
		return err
	}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM channel_webhooks WHERE id = $1`, id); err != nil {
			return err
		}
		return auditLog(ctx, tx, me, "channels.webhook.delete", "channels.channel", channelID.String(), map[string]any{"webhook_id": id})
	})
	if err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

// webhookLimiter allows webhookPerMinute posts per hook per minute.
type webhookLimiter struct {
	mu   sync.Mutex
	hits map[uuid.UUID][]time.Time
}

func (l *webhookLimiter) allow(id uuid.UUID, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.hits == nil {
		l.hits = map[uuid.UUID][]time.Time{}
	}
	cut := now.Add(-time.Minute)
	kept := l.hits[id][:0]
	for _, t := range l.hits[id] {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= webhookPerMinute {
		l.hits[id] = kept
		return false
	}
	l.hits[id] = append(kept, now)
	if len(l.hits) > 10000 { // forget idle hooks
		for k, v := range l.hits {
			if len(v) == 0 || v[len(v)-1].Before(cut) {
				delete(l.hits, k)
			}
		}
	}
	return true
}

type webhookPost struct {
	Text string `json:"text"`
}

// POST /v1/channels/hooks/{token} {text} — public; the token is the credential.
func (s *Service) receiveWebhook(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	if h := r.Header.Get("Authorization"); h != "" && auth.IsAgentCredential(strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))) {
		return httpx.NewError(http.StatusUnauthorized, "agent_credential", "Agent credentials work only at the agent endpoint.")
	}
	token := chi.URLParam(r, "token")
	notFound := httpx.NotFound("Webhook not found.")
	if !strings.HasPrefix(token, webhookPrefix) || len(token) > 80 {
		return notFound
	}
	var req webhookPost
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	var id, channelID uuid.UUID
	var name string
	err := s.d.Pool.QueryRow(ctx, `SELECT id, channel_id, name FROM channel_webhooks WHERE token_hash = $1`, hashToken(token)).Scan(&id, &channelID, &name)
	if db.IsNoRows(err) {
		return notFound
	}
	if err != nil {
		return err
	}
	if !s.hookLimit.allow(id, time.Now()) {
		return httpx.NewError(http.StatusTooManyRequests, "rate_limited", fmt.Sprintf("This webhook can post %d messages a minute.", webhookPerMinute))
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return httpx.BadRequest("Send {\"text\": \"…\"}.")
	}
	if specialTokenRe.MatchString(text) {
		return httpx.BadRequest("Webhooks can't use @channel or @here.")
	}
	if err := moderation.Check(ctx, text); err != nil {
		return err
	}
	msgID, err := s.postBot(ctx, channelID, BotMessage{BotName: name, Text: text}, &id)
	if err != nil {
		return err
	}
	if _, err := s.d.Pool.Exec(ctx, `UPDATE channel_webhooks SET last_used_at = now() WHERE id = $1`, id); err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"ok": true, "message_id": msgID})
	return nil
}
