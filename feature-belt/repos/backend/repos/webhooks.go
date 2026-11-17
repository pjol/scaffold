package repos

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/bg"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/safehttp"
)

// Outbound webhooks: a repository's managers point URLs at events (push,
// pull requests, reviews, issues, comments, releases, statuses). Each
// delivery is a JSON POST through safehttp (no private addresses outside
// development, no redirects, 10 s, small responses), signed with the
// hook's secret (HMAC-SHA256 of the body in X-Webhook-Signature-256 and
// X-Hub-Signature-256), logged with its response, and can be redelivered.

// WebhookEvents are the events a hook can subscribe to.
var WebhookEvents = []string{"push", "pull_request", "pull_request_review", "issues", "issue_comment", "release", "status"}

const (
	maxWebhooksPerRepo  = 20
	webhookTimeout      = 10 * time.Second
	webhookResponseKeep = 4 << 10
	webhookMaxPayload   = 256 << 10
)

// Webhook is a configured hook (its secret is shown only when created).
type Webhook struct {
	ID             uuid.UUID  `json:"id"`
	URL            string     `json:"url"`
	Events         []string   `json:"events"`
	Active         bool       `json:"active"`
	LastStatus     *int       `json:"last_status"`
	LastDeliveryAt *time.Time `json:"last_delivery_at"`
	CreatedAt      time.Time  `json:"created_at"`
	Secret         string     `json:"secret,omitempty"`
}

const webhookCols = `id, url, events, active, last_status, last_delivery_at, created_at`

func scanWebhook(row pgx.Row) (*Webhook, error) {
	var h Webhook
	err := row.Scan(&h.ID, &h.URL, &h.Events, &h.Active, &h.LastStatus, &h.LastDeliveryAt, &h.CreatedAt)
	return &h, err
}

// webhookRepo is manage level, a person only.
func (s *Service) webhookRepo(r *http.Request) (*Repo, error) {
	if _, ok := auth.AgentFrom(r.Context()); ok {
		return nil, errAgentForbidden
	}
	return s.repoFromRoute(r, levelManage)
}

func (s *Service) listWebhooks(w http.ResponseWriter, r *http.Request) error {
	repo, err := s.webhookRepo(r)
	if err != nil {
		return err
	}
	rows, err := s.d.Pool.Query(r.Context(), `SELECT `+webhookCols+` FROM repos_webhooks WHERE repo_id = $1 ORDER BY created_at, id`, repo.ID)
	if err != nil {
		return err
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Webhook, error) { h, err := scanWebhook(r); return *h, err })
	if err != nil {
		return err
	}
	if list == nil {
		list = []Webhook{}
	}
	httpx.OK(w, map[string]any{"items": list, "events": WebhookEvents})
	return nil
}

type webhookReq struct {
	URL    *string  `json:"url"`
	Events []string `json:"events"`
	Active *bool    `json:"active"`
}

func (s *Service) cleanWebhook(req webhookReq, h *Webhook) error {
	dev := s.d.Cfg == nil || s.d.Cfg.IsDevelopment()
	if req.URL != nil {
		u := strings.TrimSpace(*req.URL)
		if len(u) < 8 || len(u) > 2000 {
			return httpx.BadRequest("Enter the URL that receives deliveries (https://…).")
		}
		if _, err := safehttp.CheckURL(u, dev, dev); err != nil {
			return httpx.NewError(http.StatusBadRequest, "url_refused", "That URL can't receive webhooks: use a public https:// address.")
		}
		h.URL = u
	}
	if req.Events != nil {
		known := map[string]bool{}
		for _, e := range WebhookEvents {
			known[e] = true
		}
		seen := map[string]bool{}
		h.Events = []string{}
		for _, e := range req.Events {
			if !known[e] {
				return httpx.BadRequest("events are some of: " + strings.Join(WebhookEvents, ", ") + ".")
			}
			if !seen[e] {
				seen[e] = true
				h.Events = append(h.Events, e)
			}
		}
		if len(h.Events) == 0 {
			return httpx.BadRequest("Choose at least one event.")
		}
	}
	if req.Active != nil {
		h.Active = *req.Active
	}
	return nil
}

func (s *Service) createWebhook(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	repo, err := s.webhookRepo(r)
	if err != nil {
		return err
	}
	var req webhookReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.URL == nil {
		return httpx.BadRequest("Enter the URL that receives deliveries.")
	}
	h := &Webhook{Events: []string{"push"}, Active: true}
	if err := s.cleanWebhook(req, h); err != nil {
		return err
	}
	var n int
	if err := s.d.Pool.QueryRow(r.Context(), `SELECT count(*) FROM repos_webhooks WHERE repo_id = $1`, repo.ID).Scan(&n); err != nil {
		return err
	}
	if n >= maxWebhooksPerRepo {
		return httpx.Conflict(fmt.Sprintf("A repository holds up to %d webhooks.", maxWebhooksPerRepo))
	}
	secret := newWebhookSecret()
	created, err := scanWebhook(s.d.Pool.QueryRow(r.Context(), `INSERT INTO repos_webhooks (repo_id, url, secret, events, active, created_by)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING `+webhookCols, repo.ID, h.URL, secret, h.Events, h.Active, me.UserID))
	if err != nil {
		return err
	}
	created.Secret = secret
	if created.Active {
		s.deliverTo(r.Context(), repo, me, created.ID, "ping", "", map[string]any{"zen": "Deliveries to this URL work.", "hook_id": created.ID, "events": created.Events})
	}
	httpx.JSON(w, http.StatusCreated, created)
	return nil
}

func newWebhookSecret() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return "whsec_" + hex.EncodeToString(b)
}

func (s *Service) hookFromRoute(r *http.Request) (*Repo, *Webhook, error) {
	repo, err := s.webhookRepo(r)
	if err != nil {
		return nil, nil, err
	}
	id, err := httpx.URLParamUUID(r, "hookID")
	if err != nil {
		return nil, nil, httpx.NotFound("Webhook not found.")
	}
	h, err := scanWebhook(s.d.Pool.QueryRow(r.Context(), `SELECT `+webhookCols+` FROM repos_webhooks WHERE id = $1 AND repo_id = $2`, id, repo.ID))
	if db.IsNoRows(err) {
		return nil, nil, httpx.NotFound("Webhook not found.")
	}
	return repo, h, err
}

func (s *Service) updateWebhook(w http.ResponseWriter, r *http.Request) error {
	_, h, err := s.hookFromRoute(r)
	if err != nil {
		return err
	}
	var req webhookReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if err := s.cleanWebhook(req, h); err != nil {
		return err
	}
	out, err := scanWebhook(s.d.Pool.QueryRow(r.Context(), `UPDATE repos_webhooks SET url = $2, events = $3, active = $4, updated_at = now()
		WHERE id = $1 RETURNING `+webhookCols, h.ID, h.URL, h.Events, h.Active))
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

func (s *Service) deleteWebhook(w http.ResponseWriter, r *http.Request) error {
	_, h, err := s.hookFromRoute(r)
	if err != nil {
		return err
	}
	if _, err := s.d.Pool.Exec(r.Context(), `DELETE FROM repos_webhooks WHERE id = $1`, h.ID); err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

func (s *Service) pingWebhook(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	repo, h, err := s.hookFromRoute(r)
	if err != nil {
		return err
	}
	s.deliverTo(r.Context(), repo, me, h.ID, "ping", "", map[string]any{"zen": "Deliveries to this URL work.", "hook_id": h.ID, "events": h.Events})
	httpx.JSON(w, http.StatusAccepted, map[string]any{"queued": true})
	return nil
}

// WebhookDelivery is one attempt (payload and response in detail only).
type WebhookDelivery struct {
	ID           uuid.UUID  `json:"id"`
	Event        string     `json:"event"`
	Action       string     `json:"action"`
	StatusCode   *int       `json:"status_code"`
	Error        string     `json:"error"`
	DurationMS   int        `json:"duration_ms"`
	RedeliveryOf *uuid.UUID `json:"redelivery_of"`
	CreatedAt    time.Time  `json:"created_at"`
	Payload      string     `json:"payload,omitempty"`
	Response     string     `json:"response,omitempty"`
}

func (s *Service) listDeliveries(w http.ResponseWriter, r *http.Request) error {
	_, h, err := s.hookFromRoute(r)
	if err != nil {
		return err
	}
	page := httpx.ParsePage(r)
	var c cursor
	if err := httpx.DecodeCursor(page.Cursor, &c); err != nil {
		return err
	}
	rows, err := s.d.Pool.Query(r.Context(), `SELECT id, event, action, status_code, error, duration_ms, redelivery_of, created_at, '', ''
		FROM repos_webhook_deliveries WHERE webhook_id = $1 AND ($2::timestamptz IS NULL OR (created_at, id) < ($2, $3))
		ORDER BY created_at DESC, id DESC LIMIT $4`, h.ID, nullTime(c.T), c.ID, page.Limit+1)
	if err != nil {
		return err
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[WebhookDelivery])
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(list, page.Limit, func(d WebhookDelivery) any { return cursor{T: d.CreatedAt, ID: d.ID} }))
	return nil
}

func (s *Service) getDelivery(w http.ResponseWriter, r *http.Request) error {
	_, h, err := s.hookFromRoute(r)
	if err != nil {
		return err
	}
	id, err := httpx.URLParamUUID(r, "deliveryID")
	if err != nil {
		return httpx.NotFound("Delivery not found.")
	}
	rows, err := s.d.Pool.Query(r.Context(), `SELECT id, event, action, status_code, error, duration_ms, redelivery_of, created_at, payload, response_body
		FROM repos_webhook_deliveries WHERE id = $1 AND webhook_id = $2`, id, h.ID)
	if err != nil {
		return err
	}
	d, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByPos[WebhookDelivery])
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.NotFound("Delivery not found.")
	}
	if err != nil {
		return err
	}
	httpx.OK(w, d)
	return nil
}

func (s *Service) redeliver(w http.ResponseWriter, r *http.Request) error {
	repo, h, err := s.hookFromRoute(r)
	if err != nil {
		return err
	}
	id, err := httpx.URLParamUUID(r, "deliveryID")
	if err != nil {
		return httpx.NotFound("Delivery not found.")
	}
	var event, action, payload string
	if err := s.d.Pool.QueryRow(r.Context(), `SELECT event, action, payload FROM repos_webhook_deliveries WHERE id = $1 AND webhook_id = $2`, id, h.ID).
		Scan(&event, &action, &payload); db.IsNoRows(err) {
		return httpx.NotFound("Delivery not found.")
	} else if err != nil {
		return err
	}
	if err := s.queueDelivery(r.Context(), repo.ID, h.ID, event, action, []byte(payload), &id); err != nil {
		return err
	}
	httpx.JSON(w, http.StatusAccepted, map[string]any{"queued": true})
	return nil
}

// ---- emitting ------------------------------------------------------------------

// emitWebhook queues event for every active hook of the repository that
// subscribes to it. Best-effort: failures are logged, never returned.
func (s *Service) emitWebhook(ctx context.Context, repo *Repo, actor *auth.Identity, event, action string, payload map[string]any) {
	rows, err := s.d.Pool.Query(ctx, `SELECT id FROM repos_webhooks WHERE repo_id = $1 AND active AND $2 = ANY(events)`, repo.ID, event)
	if err != nil {
		slog.WarnContext(ctx, "repos: reading webhooks failed", "err", err)
		return
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil || len(ids) == 0 {
		return
	}
	for _, id := range ids {
		s.deliverTo(ctx, repo, actor, id, event, action, payload)
	}
}

// deliverTo builds the body for one hook and queues it.
func (s *Service) deliverTo(ctx context.Context, repo *Repo, actor *auth.Identity, hookID uuid.UUID, event, action string, payload map[string]any) {
	body := map[string]any{"event": event, "repository": s.repoHookView(repo)}
	if action != "" {
		body["action"] = action
	}
	if actor != nil {
		body["sender"] = map[string]any{"id": actor.UserID, "display_name": s.person(actor).Name}
	}
	for k, v := range payload {
		body[k] = v
	}
	b, err := json.Marshal(body)
	if err != nil {
		slog.WarnContext(ctx, "repos: webhook payload failed", "err", err)
		return
	}
	if len(b) > webhookMaxPayload {
		b, _ = json.Marshal(map[string]any{"event": event, "action": action, "repository": s.repoHookView(repo), "truncated": true})
	}
	if err := s.queueDelivery(ctx, repo.ID, hookID, event, action, b, nil); err != nil {
		slog.WarnContext(ctx, "repos: queueing a webhook failed", "err", err)
	}
}

// queueDelivery records the delivery and sends it in the background.
func (s *Service) queueDelivery(ctx context.Context, repoID, hookID uuid.UUID, event, action string, payload []byte, redeliveryOf *uuid.UUID) error {
	var id uuid.UUID
	if err := s.d.Pool.QueryRow(ctx, `INSERT INTO repos_webhook_deliveries (webhook_id, event, action, payload, redelivery_of) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		hookID, event, action, string(payload), redeliveryOf).Scan(&id); err != nil {
		return err
	}
	bg.Go(ctx, "repos.webhook", func(ctx context.Context) error { return s.send1(ctx, hookID, id, event, payload) })
	return nil
}

// SignWebhook is the signature header value for a body: sha256=<hex HMAC>.
func SignWebhook(secret string, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(body)
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}

func (s *Service) send1(ctx context.Context, hookID, deliveryID uuid.UUID, event string, payload []byte) error {
	var url, secret string
	var active bool
	if err := s.d.Pool.QueryRow(ctx, `SELECT url, secret, active FROM repos_webhooks WHERE id = $1`, hookID).Scan(&url, &secret, &active); err != nil {
		if db.IsNoRows(err) {
			return nil // deleted meanwhile
		}
		return err
	}
	dev := s.d.Cfg == nil || s.d.Cfg.IsDevelopment()
	client := safehttp.Client(safehttp.Options{Timeout: webhookTimeout, MaxBody: 64 << 10, MaxRedirects: 0, AllowPrivate: dev, AllowHTTP: dev})
	ctx, cancel := context.WithTimeout(ctx, webhookTimeout+2*time.Second)
	defer cancel()
	start := time.Now()
	var status *int
	var respBody, errMsg string
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err == nil {
		sig := SignWebhook(secret, payload)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "Scaffold App-Webhooks/1")
		req.Header.Set("X-Webhook-Event", event)
		req.Header.Set("X-Webhook-Delivery", deliveryID.String())
		req.Header.Set("X-Webhook-Signature-256", sig)
		req.Header.Set("X-Hub-Signature-256", sig)
		var resp *http.Response
		resp, err = client.Do(req)
		if err == nil {
			code := resp.StatusCode
			status = &code
			b, rerr := io.ReadAll(io.LimitReader(resp.Body, webhookResponseKeep))
			_ = resp.Body.Close()
			if rerr == nil || errors.Is(rerr, safehttp.ErrTooLarge) {
				respBody = string(b)
			}
			if !utf8.ValidString(respBody) {
				respBody = "(binary response)"
			}
		}
	}
	if err != nil {
		switch {
		case safehttp.IsBlocked(err):
			errMsg = "The URL points at a private or local address, which isn't allowed."
		case errors.Is(err, context.DeadlineExceeded):
			errMsg = "The receiver didn't answer within 10 seconds."
		default:
			errMsg = "The delivery failed: " + truncateUTF8(err.Error(), 300)
		}
	}
	dur := int(time.Since(start) / time.Millisecond)
	if _, err := s.d.Pool.Exec(context.WithoutCancel(ctx), `UPDATE repos_webhook_deliveries SET status_code = $2, response_body = $3, error = $4, duration_ms = $5 WHERE id = $1`,
		deliveryID, status, respBody, errMsg, dur); err != nil {
		return err
	}
	_, err = s.d.Pool.Exec(context.WithoutCancel(ctx), `UPDATE repos_webhooks SET last_status = $2, last_delivery_at = now() WHERE id = $1`, hookID, statusOrZero(status))
	return err
}

func statusOrZero(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// pruneDeliveries keeps each hook's latest 200 deliveries, at most 30 days old.
func (s *Service) pruneDeliveries(ctx context.Context) error {
	_, err := s.d.Pool.Exec(ctx, `
		DELETE FROM repos_webhook_deliveries d WHERE d.created_at < now() - interval '30 days'
		   OR d.id IN (SELECT id FROM (SELECT id, row_number() OVER (PARTITION BY webhook_id ORDER BY created_at DESC, id DESC) AS rn
		               FROM repos_webhook_deliveries) x WHERE x.rn > 200)`)
	return err
}

// ---- payload views -------------------------------------------------------------

func (s *Service) webURL(p string) string {
	if s.d.Cfg == nil {
		return p
	}
	return strings.TrimRight(s.d.Cfg.PublicWebURL, "/") + p
}

func (s *Service) repoHookView(repo *Repo) map[string]any {
	return map[string]any{"id": repo.ID, "name": repo.Name, "slug": repo.Slug, "full_name": repo.OrgSlug + "/" + repo.Slug,
		"default_branch": repo.DefaultBranch, "clone_url": s.cloneURL(repo), "html_url": s.webURL("/repos/" + repo.ID.String())}
}

func (s *Service) issueHookView(repo *Repo, is *Issue) map[string]any {
	labels := make([]string, 0, len(is.Labels))
	for _, l := range is.Labels {
		labels = append(labels, l.Name)
	}
	v := map[string]any{"number": is.Number, "title": is.Title, "body": is.Body, "state": is.State, "state_reason": is.StateReason,
		"labels": labels, "created_at": is.CreatedAt, "updated_at": is.UpdatedAt, "closed_at": is.ClosedAt, "html_url": s.webURL(s.issueLink(repo, is.Number))}
	if is.Author != nil {
		v["author"] = map[string]any{"id": is.Author.ID, "display_name": is.Author.DisplayName}
	}
	return v
}

func (s *Service) pullHookView(repo *Repo, p *Pull) map[string]any {
	v := map[string]any{"number": p.Number, "title": p.Title, "body": p.Body, "state": p.State, "draft": p.Draft,
		"head": map[string]any{"ref": p.HeadBranch, "sha": p.HeadSHA}, "base": map[string]any{"ref": p.BaseBranch, "sha": p.BaseSHA},
		"merge_commit_sha": p.MergeCommitSHA, "merged": p.State == "merged", "created_at": p.CreatedAt, "updated_at": p.UpdatedAt,
		"html_url": s.webURL(s.pullLink(repo, p.Number))}
	if p.Author != nil {
		v["author"] = map[string]any{"id": p.Author.ID, "display_name": p.Author.DisplayName}
	}
	return v
}

// emitItemHook sends the issue or pull request event for a triage change.
func (s *Service) emitItemHook(ctx context.Context, repo *Repo, actor *auth.Identity, kind string, number int, action string) {
	if kind == "pull" {
		if p, err := s.loadPull(ctx, s.d.Pool, repo.ID, number); err == nil {
			s.emitWebhook(ctx, repo, actor, "pull_request", action, map[string]any{"pull_request": s.pullHookView(repo, p)})
		}
		return
	}
	if is, err := s.loadIssue(ctx, s.d.Pool, repo.ID, number); err == nil {
		s.emitWebhook(ctx, repo, actor, "issues", action, map[string]any{"issue": s.issueHookView(repo, is)})
	}
}

// emitPush sends one push event per updated ref.
func (s *Service) emitPush(ctx context.Context, repo *Repo, actor *auth.Identity, u refUpdate) {
	payload := map[string]any{"ref": u.Ref, "before": u.Old, "after": u.New, "created": u.creating(), "deleted": u.deleting()}
	commits := []map[string]any{}
	if !u.deleting() {
		base := u.Old
		if u.creating() {
			base = ""
		}
		list, err := s.git.RangeCommits(ctx, repo.ID, base, u.New, 20)
		if err == nil {
			for _, c := range list {
				commits = append(commits, map[string]any{"id": c.SHA, "message": strings.TrimSpace(c.Subject + "\n\n" + c.Body),
					"timestamp": c.Author.At, "author": map[string]any{"name": c.Author.Name, "email": c.Author.Email},
					"url": s.webURL(fmt.Sprintf("/repos/%s/commits/%s", repo.ID, c.SHA))})
			}
		}
	}
	payload["commits"] = commits
	s.emitWebhook(ctx, repo, actor, "push", "", payload)
}
