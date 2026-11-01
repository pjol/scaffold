package presence

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
)

// CustomStatus is what a person says about themselves ("🌴 Out until Monday").
type CustomStatus struct {
	Emoji      string     `json:"emoji"`
	Text       string     `json:"text"`
	ClearAfter string     `json:"clear_after"` // 30m | 1h | 4h | today | week | never
	ExpiresAt  *time.Time `json:"expires_at"`  // null = never
}

// Presence is one person's state as others see it.
type Presence struct {
	UserID   uuid.UUID     `json:"user_id"`
	Status   string        `json:"status"` // online | away | offline
	Custom   *CustomStatus `json:"custom_status"`
	DNDUntil *time.Time    `json:"dnd_until"` // Do Not Disturb until; null = off
}

// Mine is the caller's own presence with the settings clients need.
type Mine struct {
	Presence
	Away             bool        `json:"away"`      // "appear away" is on
	Audiences        []uuid.UUID `json:"audiences"` // subscribe to presence:<id> for each
	HeartbeatSeconds int         `json:"heartbeat_seconds"`
	IdleAfterSeconds int         `json:"idle_after_seconds"`
}

// GET /presence?user_ids=<id>,<id> — the ones the caller may see (≤ 100).
func (s *Service) list(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context()).UserID
	var ids []uuid.UUID
	for _, part := range strings.Split(r.URL.Query().Get("user_ids"), ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := uuid.Parse(part)
		if err != nil {
			return httpx.BadRequest("user_ids must be comma-separated ids.")
		}
		ids = append(ids, id)
	}
	if len(ids) > maxBatch {
		return httpx.BadRequest("Ask for up to 100 people at a time.")
	}
	items, err := s.visiblePresence(r.Context(), s.d.Pool, me, ids)
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": items})
	return nil
}

func (s *Service) mine(ctx context.Context, userID uuid.UUID) (*Mine, error) {
	rows, err := s.compute(ctx, s.d.Pool, []uuid.UUID{userID})
	if err != nil {
		return nil, err
	}
	m := &Mine{
		Presence:         Presence{UserID: userID, Status: StatusOffline},
		HeartbeatSeconds: int(s.cfg.heartbeat.Seconds()),
		IdleAfterSeconds: int(s.cfg.idleAfter.Seconds()),
	}
	if len(rows) == 1 {
		m.Presence, m.Away = rows[0].Presence, rows[0].away
	}
	if m.Audiences, err = audiences(ctx, s.d.Pool, userID); err != nil {
		return nil, err
	}
	return m, nil
}

func (s *Service) respondMine(w http.ResponseWriter, r *http.Request) error {
	m, err := s.mine(r.Context(), auth.MustFromContext(r.Context()).UserID)
	if err != nil {
		return err
	}
	httpx.OK(w, m)
	return nil
}

// GET /presence/me
func (s *Service) me(w http.ResponseWriter, r *http.Request) error { return s.respondMine(w, r) }

// ---- heartbeats ----------------------------------------------------------------

var deviceIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

type heartbeatReq struct {
	DeviceID string `json:"device_id"`
	Platform string `json:"platform"` // web | ios | android
	Active   *bool  `json:"active"`   // false = idle on this device; default true
}

// HeartbeatResult tells the client its state and how often to beat.
type HeartbeatResult struct {
	Status           string `json:"status"`
	HeartbeatSeconds int    `json:"heartbeat_seconds"`
	IdleAfterSeconds int    `json:"idle_after_seconds"`
}

// POST /presence/heartbeat — clients send one every heartbeat_seconds while
// visible or foregrounded, and at once when they go idle or come back.
func (s *Service) heartbeatRoute(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	var req heartbeatReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if !deviceIDPattern.MatchString(req.DeviceID) {
		return httpx.BadRequest("device_id must be 8 to 64 letters, digits, dashes or underscores.")
	}
	switch req.Platform {
	case "":
		req.Platform = "web"
	case "web", "ios", "android":
	default:
		return httpx.BadRequest("platform must be web, ios or android.")
	}
	active := req.Active == nil || *req.Active
	if _, err := s.d.Pool.Exec(ctx, `
		WITH st AS (INSERT INTO presence_status (user_id) VALUES ($1) ON CONFLICT (user_id) DO NOTHING)
		INSERT INTO presence_devices (user_id, device_id, platform, active, last_seen_at)
		VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (user_id, device_id) DO UPDATE SET platform = EXCLUDED.platform, active = EXCLUDED.active, last_seen_at = now()`,
		me, req.DeviceID, req.Platform, active); err != nil {
		return err
	}
	status := StatusOffline
	if rows := s.reconcile(ctx, []uuid.UUID{me}, false); len(rows) == 1 {
		status = rows[0].Status
	}
	httpx.OK(w, HeartbeatResult{Status: status, HeartbeatSeconds: int(s.cfg.heartbeat.Seconds()), IdleAfterSeconds: int(s.cfg.idleAfter.Seconds())})
	return nil
}

type offlineReq struct {
	DeviceID string `json:"device_id"`
}

// POST /presence/offline — a tab closing or the app signing out. Without it
// the device goes offline after PRESENCE_OFFLINE_TTL anyway.
func (s *Service) offline(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context()).UserID
	var req offlineReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if !deviceIDPattern.MatchString(req.DeviceID) {
		return httpx.BadRequest("device_id must be 8 to 64 letters, digits, dashes or underscores.")
	}
	if _, err := s.d.Pool.Exec(r.Context(), `DELETE FROM presence_devices WHERE user_id = $1 AND device_id = $2`, me, req.DeviceID); err != nil {
		return err
	}
	s.reconcile(r.Context(), []uuid.UUID{me}, false)
	httpx.NoContent(w)
	return nil
}

// ---- custom status, Do Not Disturb, away ----------------------------------------

type statusReq struct {
	Emoji      string     `json:"emoji"`
	Text       string     `json:"text"`
	ClearAfter string     `json:"clear_after"`
	ExpiresAt  *time.Time `json:"expires_at"` // required for today and week (the client's local end of day / week)
}

var fixedClear = map[string]time.Duration{"30m": 30 * time.Minute, "1h": time.Hour, "4h": 4 * time.Hour}

// PUT /presence/me/status
func (s *Service) setStatus(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	var req statusReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	emoji := strings.TrimSpace(req.Emoji)
	text := strings.Join(strings.Fields(req.Text), " ")
	switch {
	case emoji == "" && text == "":
		return httpx.BadRequest("Write a status or choose an emoji.")
	case emoji != "" && !validEmoji(emoji):
		return httpx.BadRequest("Choose an emoji.")
	case utf8.RuneCountInString(text) > 100:
		return httpx.BadRequest("Statuses can be up to 100 characters.")
	}
	now := time.Now()
	var expires *time.Time
	switch req.ClearAfter {
	case "never", "":
		req.ClearAfter = "never"
	case "30m", "1h", "4h":
		t := now.Add(fixedClear[req.ClearAfter]).UTC()
		expires = &t
	case "today", "week":
		limit := 25 * time.Hour
		if req.ClearAfter == "week" {
			limit = 8 * 24 * time.Hour
		}
		if req.ExpiresAt == nil || !req.ExpiresAt.After(now) || req.ExpiresAt.Sub(now) > limit {
			return httpx.BadRequest("Send expires_at: the end of today or of this week in your time zone.")
		}
		t := req.ExpiresAt.UTC()
		expires = &t
	default:
		return httpx.BadRequest("clear_after must be 30m, 1h, 4h, today, week or never.")
	}
	if err := checkText(ctx, text); err != nil {
		return err
	}
	if _, err := s.d.Pool.Exec(ctx, `
		INSERT INTO presence_status (user_id, emoji, text, clear_after, expires_at) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (user_id) DO UPDATE SET emoji = EXCLUDED.emoji, text = EXCLUDED.text,
		  clear_after = EXCLUDED.clear_after, expires_at = EXCLUDED.expires_at, updated_at = now()`,
		me, emoji, text, req.ClearAfter, expires); err != nil {
		return err
	}
	s.reconcile(ctx, []uuid.UUID{me}, true)
	return s.respondMine(w, r)
}

// DELETE /presence/me/status
func (s *Service) clearStatus(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context()).UserID
	if _, err := s.d.Pool.Exec(r.Context(), `
		UPDATE presence_status SET emoji = '', text = '', clear_after = 'never', expires_at = NULL, updated_at = now()
		WHERE user_id = $1`, me); err != nil {
		return err
	}
	s.reconcile(r.Context(), []uuid.UUID{me}, true)
	return s.respondMine(w, r)
}

type dndReq struct {
	Until *time.Time `json:"until"`
}

// PUT /presence/me/dnd {until} — Do Not Disturb until then (≤ 31 days).
func (s *Service) setDND(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context()).UserID
	var req dndReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	now := time.Now()
	if req.Until == nil || !req.Until.After(now) {
		return httpx.BadRequest("Pick a time in the future.")
	}
	if req.Until.Sub(now) > 31*24*time.Hour {
		return httpx.BadRequest("Do Not Disturb can last up to 31 days.")
	}
	if _, err := s.d.Pool.Exec(r.Context(), `
		INSERT INTO presence_status (user_id, dnd_until) VALUES ($1, $2)
		ON CONFLICT (user_id) DO UPDATE SET dnd_until = EXCLUDED.dnd_until, updated_at = now()`, me, req.Until.UTC()); err != nil {
		return err
	}
	s.reconcile(r.Context(), []uuid.UUID{me}, true)
	return s.respondMine(w, r)
}

// DELETE /presence/me/dnd
func (s *Service) clearDND(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context()).UserID
	if _, err := s.d.Pool.Exec(r.Context(), `UPDATE presence_status SET dnd_until = NULL, updated_at = now() WHERE user_id = $1`, me); err != nil {
		return err
	}
	s.reconcile(r.Context(), []uuid.UUID{me}, true)
	return s.respondMine(w, r)
}

type awayReq struct {
	Away *bool `json:"away"`
}

// PUT /presence/me/away {away} — "appear away" while connected.
func (s *Service) setAway(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context()).UserID
	var req awayReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.Away == nil {
		return httpx.BadRequest("Send away: true or false.")
	}
	if _, err := s.d.Pool.Exec(r.Context(), `
		INSERT INTO presence_status (user_id, away) VALUES ($1, $2)
		ON CONFLICT (user_id) DO UPDATE SET away = EXCLUDED.away, updated_at = now()`, me, *req.Away); err != nil {
		return err
	}
	s.reconcile(r.Context(), []uuid.UUID{me}, true)
	return s.respondMine(w, r)
}

// validEmoji accepts free-form emoji (sequences, keycaps, flags) and refuses words.
func validEmoji(e string) bool {
	if e == "" || utf8.RuneCountInString(e) > 16 || len(e) > 64 {
		return false
	}
	symbol := false
	for _, r := range e {
		if unicode.IsLetter(r) || unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
		symbol = symbol || r > 0x7f
	}
	return symbol
}
