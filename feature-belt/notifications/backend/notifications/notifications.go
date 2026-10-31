// Package notifications is the notifications feature: an in-app inbox with
// an unread count, Expo push to registered devices, email, and per-kind
// channel preferences. It replaces Deps.Notify (see Sender), so modules keep
// calling d.Notify.Send and declare their kinds with RegisterKind.
package notifications

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/jobs"
	"scaffoldapp/backend/internal/legal"
)

// maxMarkReadIDs bounds an explicit mark-read batch.
const maxMarkReadIDs = 500

type Service struct {
	d         *app.Deps
	sender    *Sender
	retention time.Duration
}

// New builds the module. It shares the Sender installed on d.Notify by
// RegisterDeps, or builds one when another provider replaced it.
func New(d *app.Deps) *Service {
	sender, ok := d.Notify.(*Sender)
	if !ok {
		sender = NewSender(d)
	}
	// Push registration, sign-out and clearing the badge must work on the
	// consent screen.
	legal.Exempt("/v1/devices", "/v1/notifications/read")
	retention := config.Duration("NOTIFICATIONS_RETENTION", 2160*time.Hour)
	if retention < time.Hour {
		config.Problem("NOTIFICATIONS_RETENTION must be at least 1h (got %s)", retention)
		retention = 2160 * time.Hour
	}
	return &Service{d: d, sender: sender, retention: retention}
}

func (s *Service) Name() string { return "notifications" }

func (s *Service) MountUser(r chi.Router) {
	r.With(auth.RateLimit(30, time.Hour)).Post("/devices", httpx.Handler(s.registerDevice))
	r.Delete("/devices/{token}", httpx.Handler(s.deleteDevice))
	r.Get("/notifications", httpx.Handler(s.listInbox))
	r.Get("/notifications/unread-count", httpx.Handler(s.getUnreadCount))
	r.With(auth.RateLimit(120, time.Minute)).Post("/notifications/read", httpx.Handler(s.markRead))
	r.Get("/notifications/preferences", httpx.Handler(s.getPreferences))
	r.With(auth.RateLimit(60, time.Minute)).Put("/notifications/preferences", httpx.Handler(s.putPreferences))
}

func (s *Service) MountAdmin(r chi.Router) {
	r.Get("/notifications/stats", httpx.Handler(s.adminStats))
	r.With(auth.RateLimit(20, time.Minute)).Post("/notifications/test", httpx.Handler(s.adminSendTest))
}

func (s *Service) Start(ctx context.Context) {
	go jobs.Every(ctx, s.d.Pool, "notifications.receipts", 5*time.Minute, s.checkReceipts)
	go jobs.Every(ctx, s.d.Pool, "notifications.prune", time.Hour, s.prune)
}

// ---- DTOs ------------------------------------------------------------------

// Item is one inbox row.
type Item struct {
	ID        uuid.UUID      `json:"id"`
	Kind      string         `json:"kind"`
	Title     string         `json:"title"`
	Body      string         `json:"body"`
	Link      string         `json:"link"`
	Data      map[string]any `json:"data"`
	ReadAt    *time.Time     `json:"read_at"`
	CreatedAt time.Time      `json:"created_at"`
}

// Event is the realtime "notification" payload: the item plus the fields of
// core's notify.Notification and the new unread count.
type Event struct {
	Item
	UserID      uuid.UUID `json:"user_id"`
	UnreadCount int       `json:"unread_count"`
}

// UnreadCount is the notifications.unread event and the mark-read response.
type UnreadCount struct {
	UnreadCount int `json:"unread_count"`
}

// InboxPage is GET /v1/notifications.
type InboxPage struct {
	httpx.Page[Item]
	UnreadCount int `json:"unread_count"`
}

// Preference is one kind's channels for the caller.
type Preference struct {
	Kind  string `json:"kind"`
	Group string `json:"group"` // module prefix of kind, for grouping rows
	Label string `json:"label"`
	Push  bool   `json:"push"`
	Email bool   `json:"email"`
}

// Preferences is GET/PUT /v1/notifications/preferences.
type Preferences struct {
	Items []Preference `json:"items"`
}

// ---- devices ---------------------------------------------------------------

type registerDeviceReq struct {
	Token    string `json:"token"`
	Platform string `json:"platform"`
}

// registerDevice upserts a push token for the caller. A token bound to
// another account moves to the caller (the device signed in as someone
// else). Each user keeps at most maxDevices tokens.
func (s *Service) registerDevice(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	userID := auth.MustFromContext(ctx).UserID
	var req registerDeviceReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	req.Token = strings.TrimSpace(req.Token)
	if !expoTokenRe.MatchString(req.Token) {
		return httpx.BadRequest("That isn't an Expo push token.")
	}
	switch req.Platform {
	case "ios", "android", "web":
	default:
		return httpx.BadRequest("Platform must be ios, android or web.")
	}
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO notification_devices (token, user_id, platform) VALUES ($1, $2, $3)
			ON CONFLICT (token) DO UPDATE SET user_id = EXCLUDED.user_id, platform = EXCLUDED.platform,
			  last_seen_at = now()`, req.Token, userID, req.Platform); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			DELETE FROM notification_devices WHERE token IN (
			  SELECT token FROM notification_devices WHERE user_id = $1
			  ORDER BY last_seen_at DESC, token OFFSET $2)`, userID, maxDevices)
		return err
	})
	if err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

// deleteDevice unregisters one of the caller's tokens (sign-out). Idempotent.
func (s *Service) deleteDevice(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	token, err := url.PathUnescape(chi.URLParam(r, "token"))
	if err != nil || token == "" {
		return httpx.BadRequest("Invalid token.")
	}
	if _, err := s.d.Pool.Exec(ctx, `DELETE FROM notification_devices WHERE token = $1 AND user_id = $2`,
		token, auth.MustFromContext(ctx).UserID); err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

// ---- inbox -----------------------------------------------------------------

type cursor struct {
	T  time.Time `json:"t"`
	ID uuid.UUID `json:"id"`
}

const itemCols = `id, kind, title, body, link, data, read_at, created_at`

func scanItem(row pgx.CollectableRow) (Item, error) {
	var it Item
	err := row.Scan(&it.ID, &it.Kind, &it.Title, &it.Body, &it.Link, &it.Data, &it.ReadAt, &it.CreatedAt)
	if it.Data == nil {
		it.Data = map[string]any{}
	}
	return it, err
}

// listInbox is the caller's inbox, newest first; ?unread=true for unread only.
func (s *Service) listInbox(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	userID := auth.MustFromContext(ctx).UserID
	p := httpx.ParsePage(r)
	var c cursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	unreadOnly := r.URL.Query().Get("unread") == "true"
	items, err := s.inbox(ctx, userID, unreadOnly, c, p.Limit+1)
	if err != nil {
		return err
	}
	out := InboxPage{Page: httpx.NewPage(items, p.Limit, func(it Item) any { return cursor{it.CreatedAt, it.ID} })}
	if out.UnreadCount, err = unreadCount(ctx, s.d.Pool, userID); err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

func (s *Service) inbox(ctx context.Context, userID uuid.UUID, unreadOnly bool, c cursor, limit int) ([]Item, error) {
	rows, err := s.d.Pool.Query(ctx, `
		SELECT `+itemCols+` FROM notifications
		WHERE user_id = $1 AND (NOT $2 OR read_at IS NULL)
		  AND ($3::timestamptz IS NULL OR (created_at, id) < ($3, $4))
		ORDER BY created_at DESC, id DESC LIMIT $5`,
		userID, unreadOnly, nullTime(c.T), c.ID, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanItem)
}

func (s *Service) getUnreadCount(w http.ResponseWriter, r *http.Request) error {
	n, err := unreadCount(r.Context(), s.d.Pool, auth.MustFromContext(r.Context()).UserID)
	if err != nil {
		return err
	}
	httpx.OK(w, UnreadCount{UnreadCount: n})
	return nil
}

type markReadReq struct {
	IDs []uuid.UUID `json:"ids"`
	All bool        `json:"all"`
}

// markRead marks ids (or every unread row with all=true) read and returns
// the new unread count, also published as notifications.unread.
func (s *Service) markRead(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	userID := auth.MustFromContext(ctx).UserID
	var req markReadReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	n, err := s.markReadFor(ctx, userID, req)
	if err != nil {
		return err
	}
	httpx.OK(w, UnreadCount{UnreadCount: n})
	return nil
}

func (s *Service) markReadFor(ctx context.Context, userID uuid.UUID, req markReadReq) (int, error) {
	switch {
	case req.All && len(req.IDs) > 0:
		return 0, httpx.BadRequest("Send ids or all, not both.")
	case !req.All && len(req.IDs) == 0:
		return 0, httpx.BadRequest("Send ids or all.")
	case len(req.IDs) > maxMarkReadIDs:
		return 0, httpx.BadRequest("Mark at most 500 at a time.")
	}
	var err error
	if req.All {
		_, err = s.d.Pool.Exec(ctx, `UPDATE notifications SET read_at = now() WHERE user_id = $1 AND read_at IS NULL`, userID)
	} else {
		_, err = s.d.Pool.Exec(ctx, `UPDATE notifications SET read_at = now()
			WHERE user_id = $1 AND id = ANY($2) AND read_at IS NULL`, userID, req.IDs)
	}
	if err != nil {
		return 0, err
	}
	n, err := unreadCount(ctx, s.d.Pool, userID)
	if err != nil {
		return 0, err
	}
	s.sender.publishUnread(ctx, userID, n)
	return n, nil
}

// ---- preferences -----------------------------------------------------------

func (s *Service) preferences(ctx context.Context, userID uuid.UUID) ([]Preference, error) {
	saved := map[string][2]bool{}
	rows, err := s.d.Pool.Query(ctx, `SELECT kind, push, email FROM notification_preferences WHERE user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var kind string
		var push, email bool
		if err := rows.Scan(&kind, &push, &email); err != nil {
			rows.Close()
			return nil, err
		}
		saved[kind] = [2]bool{push, email}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := []Preference{}
	for _, k := range registeredKinds() {
		p := Preference{Kind: k.ID, Group: kindGroup(k.ID), Label: k.Label, Push: k.Push, Email: k.Email}
		if v, ok := saved[k.ID]; ok {
			p.Push, p.Email = v[0], v[1]
		}
		out = append(out, p)
	}
	return out, nil
}

func (s *Service) getPreferences(w http.ResponseWriter, r *http.Request) error {
	items, err := s.preferences(r.Context(), auth.MustFromContext(r.Context()).UserID)
	if err != nil {
		return err
	}
	httpx.OK(w, Preferences{Items: items})
	return nil
}

type putPreferencesReq struct {
	Items []struct {
		Kind  string `json:"kind"`
		Push  bool   `json:"push"`
		Email bool   `json:"email"`
	} `json:"items"`
}

// putPreferences saves the channels for each listed kind (others keep their
// value) and returns the full list.
func (s *Service) putPreferences(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	userID := auth.MustFromContext(ctx).UserID
	var req putPreferencesReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if len(req.Items) == 0 {
		return httpx.BadRequest("Send at least one preference.")
	}
	if len(req.Items) > 200 {
		return httpx.BadRequest("Too many preferences.")
	}
	for _, it := range req.Items {
		if _, ok := lookupKind(it.Kind); !ok {
			return httpx.BadRequest("Unknown notification kind: " + it.Kind)
		}
	}
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		for _, it := range req.Items {
			if _, err := tx.Exec(ctx, `
				INSERT INTO notification_preferences (user_id, kind, push, email) VALUES ($1, $2, $3, $4)
				ON CONFLICT (user_id, kind) DO UPDATE SET push = EXCLUDED.push, email = EXCLUDED.email, updated_at = now()`,
				userID, it.Kind, it.Push, it.Email); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return s.getPreferences(w, r)
}

// ---- jobs ------------------------------------------------------------------

// checkReceipts asks Expo how accepted pushes ended (receipts are ready
// after ~15 minutes and kept for 24 hours) and removes tokens reported as
// DeviceNotRegistered. Every ticket is checked once; unanswered tickets older
// than a day are dropped.
func (s *Service) checkReceipts(ctx context.Context) error {
	if s.sender.expo.mode != modeExpo {
		return nil
	}
	if _, err := s.d.Pool.Exec(ctx, `DELETE FROM notification_push_tickets WHERE created_at < now() - interval '24 hours'`); err != nil {
		return err
	}
	for range 20 { // at most 20 × 300 tickets per run
		rows, err := s.d.Pool.Query(ctx, `
			SELECT ticket_id, token FROM notification_push_tickets
			WHERE created_at < now() - interval '15 minutes'
			ORDER BY created_at LIMIT $1`, expoReceiptIDs)
		if err != nil {
			return err
		}
		tokens := map[string]string{}
		var ids []string
		for rows.Next() {
			var id, tok string
			if err := rows.Scan(&id, &tok); err != nil {
				rows.Close()
				return err
			}
			tokens[id] = tok
			ids = append(ids, id)
		}
		rows.Close()
		if len(ids) == 0 {
			return nil
		}
		receipts, err := s.sender.expo.receipts(ctx, ids)
		if err != nil {
			return err
		}
		var done []string
		for _, id := range ids {
			rc, ok := receipts[id]
			if !ok {
				continue // not ready; retried until it ages out
			}
			done = append(done, id)
			if rc.Status == "error" {
				if rc.Details.Error == "DeviceNotRegistered" {
					removeDevice(ctx, s.d.Pool, tokens[id])
				} else {
					slog.WarnContext(ctx, "notifications: push receipt error", "ticket_id", id, "error", rc.Details.Error)
				}
			}
		}
		if len(done) == 0 {
			return nil
		}
		if _, err := s.d.Pool.Exec(ctx, `DELETE FROM notification_push_tickets WHERE ticket_id = ANY($1)`, done); err != nil {
			return err
		}
		if len(ids) < expoReceiptIDs {
			return nil
		}
	}
	return nil
}

// prune deletes read inbox rows older than NOTIFICATIONS_RETENTION, in
// batches so one run never holds a long lock.
func (s *Service) prune(ctx context.Context) error {
	cutoff := time.Now().Add(-s.retention)
	for {
		tag, err := s.d.Pool.Exec(ctx, `
			DELETE FROM notifications WHERE id IN (
			  SELECT id FROM notifications WHERE read_at IS NOT NULL AND created_at < $1 LIMIT 5000)`, cutoff)
		if err != nil {
			return err
		}
		if tag.RowsAffected() < 5000 {
			return nil
		}
	}
}

func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
