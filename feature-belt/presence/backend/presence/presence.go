// Package presence tells people who is online, away or offline, and lets each
// person set a custom status (emoji, text, when it clears), Do Not Disturb
// until a time, and "appear away".
//
// Rules that hold everywhere in this package:
//   - State comes from client heartbeats stored in Postgres, never from one
//     instance's sockets, so every API instance computes the same answer. A
//     device that sent a heartbeat within PRESENCE_OFFLINE_TTL keeps its person
//     online; away when every such device reports idle or the person chose
//     "appear away"; offline otherwise.
//   - Who may see whom is the app's VisibilityPolicy (default: any signed-in
//     user). Reads, the realtime topic authorizer and the agent tool all ask it.
//   - Changes publish presence.updated on presence:<audience> topics (and the
//     person's own user topic). The sweeper announces what no request causes:
//     a device going quiet, a status or Do Not Disturb running out.
package presence

import (
	"context"
	"log/slog"
	"strings"
	"sync/atomic"
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
	"scaffoldapp/backend/internal/realtime"
)

const (
	topicKind = "presence"

	// EventUpdated carries a Presence on presence:<audience> and on the
	// person's own user topic.
	EventUpdated = "presence.updated"

	StatusOnline  = "online"
	StatusAway    = "away"
	StatusOffline = "offline"

	maxBatch     = 100 // ids per GET /presence
	maxAudiences = 10  // topics one person's presence goes to (the hub caps topics per socket at 20)
)

// Everyone is the audience of the default policy: every signed-in user
// subscribes to presence:00000000-0000-0000-0000-000000000000.
var Everyone = uuid.Nil

type Service struct {
	d   *app.Deps
	cfg settings
}

type settings struct {
	offlineTTL time.Duration
	heartbeat  time.Duration
	idleAfter  time.Duration
}

// svc backs the package-level functions other features call (IsDND, Lookup).
var svc atomic.Pointer[Service]

func New(d *app.Deps) *Service {
	s := &Service{d: d, cfg: settings{
		offlineTTL: config.Duration("PRESENCE_OFFLINE_TTL", 2*time.Minute),
		heartbeat:  config.Duration("PRESENCE_HEARTBEAT_INTERVAL", 45*time.Second),
		idleAfter:  config.Duration("PRESENCE_IDLE_AFTER", 10*time.Minute),
	}}
	if s.cfg.heartbeat < 10*time.Second {
		config.Problem("PRESENCE_HEARTBEAT_INTERVAL must be at least 10s (got %s)", s.cfg.heartbeat)
		s.cfg.heartbeat = 45 * time.Second
	}
	if s.cfg.offlineTTL < 2*s.cfg.heartbeat {
		config.Problem("PRESENCE_OFFLINE_TTL (%s) must be at least twice PRESENCE_HEARTBEAT_INTERVAL (%s), so one late heartbeat doesn't read as offline",
			s.cfg.offlineTTL, s.cfg.heartbeat)
		s.cfg.offlineTTL = 3 * s.cfg.heartbeat
	}
	if s.cfg.idleAfter < time.Minute {
		config.Problem("PRESENCE_IDLE_AFTER must be at least 1m (got %s)", s.cfg.idleAfter)
		s.cfg.idleAfter = 10 * time.Minute
	}
	svc.Store(s)
	return s
}

func (s *Service) Name() string { return "presence" }

func (s *Service) MountUser(r chi.Router) {
	h := httpx.Handler
	r.Get("/presence", h(s.list))
	r.Get("/presence/me", h(s.me))
	// Heartbeats keep a reading (even suspended) person online.
	r.With(auth.RateLimit(60, time.Minute)).Post("/presence/heartbeat", h(s.heartbeatRoute))
	r.With(auth.RateLimit(60, time.Minute)).Post("/presence/offline", h(s.offline))
	r.With(auth.RateLimit(30, time.Minute)).Put("/presence/me/away", h(s.setAway))
	r.With(auth.RateLimit(30, time.Minute)).Put("/presence/me/dnd", h(s.setDND))
	r.With(auth.RateLimit(30, time.Minute)).Delete("/presence/me/dnd", h(s.clearDND))
	r.With(auth.RateLimit(30, time.Minute)).Delete("/presence/me/status", h(s.clearStatus))
	// A custom status is something other people read: active accounts only.
	r.With(auth.RequireActive, auth.RateLimit(30, time.Minute)).Put("/presence/me/status", h(s.setStatus))
}

// Start runs the sweeper: devices that went quiet, statuses and Do Not
// Disturb that ran out. One instance at a time (jobs.Every).
func (s *Service) Start(ctx context.Context) {
	go jobs.Every(ctx, s.d.Pool, "presence.sweep", 15*time.Second, s.sweep)
}

// AuthorizeTopic admits a viewer to presence:<audience> when the policy puts
// them in that audience.
func (s *Service) AuthorizeTopic(ctx context.Context, userID uuid.UUID, _ bool, topic string) (handled, allowed bool) {
	rest, ok := strings.CutPrefix(topic, topicKind+":")
	if !ok {
		return false, false
	}
	id, err := uuid.Parse(rest)
	if err != nil {
		return true, false
	}
	auds, err := audiences(ctx, s.d.Pool, userID)
	if err != nil {
		slog.WarnContext(ctx, "presence: audiences", "err", err)
		return true, false
	}
	for _, a := range auds {
		if a == id {
			return true, true
		}
	}
	return true, false
}

// ---- visibility policy --------------------------------------------------------

// VisibilityPolicy decides who sees whose presence and custom status.
type VisibilityPolicy interface {
	// Audiences are the groups userID belongs to. Their presence is published
	// to presence:<audience> for each one, and they may subscribe to each one.
	// Return at most a handful (the first 10 are used).
	Audiences(ctx context.Context, q db.Querier, userID uuid.UUID) ([]uuid.UUID, error)
	// Visible returns the subset of subjects viewer may see.
	Visible(ctx context.Context, q db.Querier, viewer uuid.UUID, subjects []uuid.UUID) ([]uuid.UUID, error)
}

var policy atomic.Value // VisibilityPolicy

// SetVisibilityPolicy installs the app's rule (call it from stitching code).
// The default lets any signed-in user see anyone with an active account.
// Apps with organizations use
// SharedGroupPolicy("org_members", "org_id", "user_id"): people who share an org.
func SetVisibilityPolicy(p VisibilityPolicy) {
	if p != nil {
		policy.Store(&p)
	}
}

func currentPolicy() VisibilityPolicy {
	if p, ok := policy.Load().(*VisibilityPolicy); ok && p != nil {
		return *p
	}
	return signedIn{}
}

// signedIn is the default policy: one audience, every active account visible.
type signedIn struct{}

func (signedIn) Audiences(context.Context, db.Querier, uuid.UUID) ([]uuid.UUID, error) {
	return []uuid.UUID{Everyone}, nil
}

func (signedIn) Visible(ctx context.Context, q db.Querier, _ uuid.UUID, subjects []uuid.UUID) ([]uuid.UUID, error) {
	rows, err := q.Query(ctx, `SELECT id FROM users WHERE id = ANY($1) AND status IN ('active', 'suspended')`, subjects)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

// SharedGroupPolicy shows a person's presence to everyone who shares a group
// with them in a membership table: table has one row per (group, user), e.g.
// SharedGroupPolicy("org_members", "org_id", "user_id"). Each group is an
// audience. The names are SQL identifiers chosen in code, never user input.
func SharedGroupPolicy(table, groupColumn, userColumn string) VisibilityPolicy {
	return sharedGroup{
		table: pgx.Identifier{table}.Sanitize(),
		group: pgx.Identifier{groupColumn}.Sanitize(),
		user:  pgx.Identifier{userColumn}.Sanitize(),
	}
}

type sharedGroup struct{ table, group, user string }

func (p sharedGroup) Audiences(ctx context.Context, q db.Querier, userID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := q.Query(ctx, `SELECT DISTINCT `+p.group+` FROM `+p.table+` WHERE `+p.user+` = $1 LIMIT $2`, userID, maxAudiences)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

func (p sharedGroup) Visible(ctx context.Context, q db.Querier, viewer uuid.UUID, subjects []uuid.UUID) ([]uuid.UUID, error) {
	rows, err := q.Query(ctx, `
		SELECT u.id FROM users u
		WHERE u.id = ANY($2) AND u.status IN ('active', 'suspended')
		  AND (u.id = $1 OR EXISTS (
		        SELECT 1 FROM `+p.table+` a JOIN `+p.table+` b ON b.`+p.group+` = a.`+p.group+`
		        WHERE a.`+p.user+` = $1 AND b.`+p.user+` = u.id))`, viewer, subjects)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

func audiences(ctx context.Context, q db.Querier, userID uuid.UUID) ([]uuid.UUID, error) {
	auds, err := currentPolicy().Audiences(ctx, q, userID)
	if len(auds) > maxAudiences {
		auds = auds[:maxAudiences]
	}
	if auds == nil {
		auds = []uuid.UUID{}
	}
	return auds, err
}

// ---- custom status text check -------------------------------------------------

var textCheck atomic.Value // func(context.Context, string) error

// SetTextCheck runs custom status text through the app's content filter
// before it is saved (stitch: presence.SetTextCheck(moderation.Check)). Return
// an *httpx.Error to refuse with a message the person can act on.
func SetTextCheck(fn func(ctx context.Context, text string) error) {
	if fn != nil {
		textCheck.Store(fn)
	}
}

func checkText(ctx context.Context, text string) error {
	if fn, ok := textCheck.Load().(func(context.Context, string) error); ok && text != "" {
		return fn(ctx, text)
	}
	return nil
}

// ---- for other features --------------------------------------------------------

// IsDND reports whether userID has Do Not Disturb on right now. Features that
// notify people (channels, calendars) skip or hold pushes while it is on.
func IsDND(ctx context.Context, q db.Querier, userID uuid.UUID) (bool, error) {
	var on bool
	err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM presence_status WHERE user_id = $1 AND dnd_until > now())`, userID).Scan(&on)
	return on, err
}

// DNDUntil returns when userID's Do Not Disturb ends, or nil when it is off.
func DNDUntil(ctx context.Context, q db.Querier, userID uuid.UUID) (*time.Time, error) {
	var until *time.Time
	err := q.QueryRow(ctx, `SELECT dnd_until FROM presence_status WHERE user_id = $1 AND dnd_until > now()`, userID).Scan(&until)
	if db.IsNoRows(err) {
		return nil, nil
	}
	return until, err
}

// Lookup returns the presence of the ids viewer may see (others are left
// out), for features that embed presence in their own responses.
func Lookup(ctx context.Context, q db.Querier, viewer uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]Presence, error) {
	s := svc.Load()
	if s == nil {
		return map[uuid.UUID]Presence{}, nil
	}
	list, err := s.visiblePresence(ctx, q, viewer, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]Presence, len(list))
	for _, p := range list {
		out[p.UserID] = p
	}
	return out, nil
}

// CanSee reports whether viewer may see subject's presence under the policy.
func CanSee(ctx context.Context, q db.Querier, viewer, subject uuid.UUID) (bool, error) {
	ids, err := currentPolicy().Visible(ctx, q, viewer, []uuid.UUID{subject})
	return len(ids) == 1 && err == nil, err
}

// ---- computing and announcing --------------------------------------------------

// statusSQL computes each id's presence; $1 ids, $2 the offline TTL in seconds.
const statusSQL = `
	SELECT u.id,
	       CASE
	         WHEN NOT EXISTS (SELECT 1 FROM presence_devices d
	                          WHERE d.user_id = u.id AND d.last_seen_at > now() - make_interval(secs => $2)) THEN 'offline'
	         WHEN COALESCE(ps.away, false) OR NOT EXISTS (SELECT 1 FROM presence_devices d
	                          WHERE d.user_id = u.id AND d.active AND d.last_seen_at > now() - make_interval(secs => $2)) THEN 'away'
	         ELSE 'online'
	       END,
	       COALESCE(ps.emoji, ''), COALESCE(ps.text, ''), COALESCE(ps.clear_after, 'never'), ps.expires_at,
	       ps.dnd_until, COALESCE(ps.away, false)
	FROM unnest($1::uuid[]) AS x(id)
	JOIN users u ON u.id = x.id AND u.status IN ('active', 'suspended')
	LEFT JOIN presence_status ps ON ps.user_id = u.id`

type row struct {
	Presence
	away bool
}

// compute reads presence for ids (no visibility check).
func (s *Service) compute(ctx context.Context, q db.Querier, ids []uuid.UUID) ([]row, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := q.Query(ctx, statusSQL, ids, s.cfg.offlineTTL.Seconds())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	now := time.Now()
	var out []row
	for rows.Next() {
		var (
			r                       row
			emoji, text, clearAfter string
			expires, dnd            *time.Time
		)
		if err := rows.Scan(&r.UserID, &r.Status, &emoji, &text, &clearAfter, &expires, &dnd, &r.away); err != nil {
			return nil, err
		}
		if (emoji != "" || text != "") && (expires == nil || expires.After(now)) {
			r.Custom = &CustomStatus{Emoji: emoji, Text: text, ClearAfter: clearAfter, ExpiresAt: expires}
		}
		if dnd != nil && dnd.After(now) {
			r.DNDUntil = dnd
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// visiblePresence is compute filtered by the policy.
func (s *Service) visiblePresence(ctx context.Context, q db.Querier, viewer uuid.UUID, ids []uuid.UUID) ([]Presence, error) {
	ids = dedupe(ids)
	if len(ids) == 0 {
		return []Presence{}, nil
	}
	visible, err := currentPolicy().Visible(ctx, q, viewer, ids)
	if err != nil {
		return nil, err
	}
	rows, err := s.compute(ctx, q, visible)
	if err != nil {
		return nil, err
	}
	out := make([]Presence, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Presence)
	}
	return out, nil
}

// reconcile recomputes each person's presence and publishes it when the
// computed state differs from the one last announced, or always with force
// (their status text or Do Not Disturb changed). The conditional UPDATE makes
// one instance announce each transition. It returns what it computed.
func (s *Service) reconcile(ctx context.Context, ids []uuid.UUID, force bool) []row {
	ctx = context.WithoutCancel(ctx)
	rows, err := s.compute(ctx, s.d.Pool, ids)
	if err != nil {
		slog.WarnContext(ctx, "presence: compute", "err", err)
		return nil
	}
	for _, r := range rows {
		tag, err := s.d.Pool.Exec(ctx, `
			UPDATE presence_status SET announced = $2 WHERE user_id = $1 AND announced <> $2`, r.UserID, r.Status)
		if err != nil {
			slog.WarnContext(ctx, "presence: record announced state", "user_id", r.UserID, "err", err)
			continue
		}
		if tag.RowsAffected() > 0 || force {
			s.publish(ctx, r.Presence)
		}
	}
	return rows
}

func (s *Service) publish(ctx context.Context, p Presence) {
	auds, err := audiences(ctx, s.d.Pool, p.UserID)
	if err != nil {
		slog.WarnContext(ctx, "presence: audiences", "user_id", p.UserID, "err", err)
	}
	for _, a := range auds {
		if err := s.d.Broker.Publish(ctx, realtime.Topic(topicKind, a), EventUpdated, p); err != nil {
			slog.WarnContext(ctx, "presence: publish", "err", err)
		}
	}
	if err := s.d.Broker.Publish(ctx, realtime.UserTopic(p.UserID), EventUpdated, p); err != nil {
		slog.WarnContext(ctx, "presence: publish to self", "err", err)
	}
}

// sweep announces what no request causes: devices going quiet, statuses and
// Do Not Disturb running out. It also forgets devices silent for a day.
func (s *Service) sweep(ctx context.Context) error {
	collect := func(sql string) ([]uuid.UUID, error) {
		rows, err := s.d.Pool.Query(ctx, sql)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	}
	expired, err := collect(`
		UPDATE presence_status SET emoji = '', text = '', clear_after = 'never', expires_at = NULL, updated_at = now()
		WHERE expires_at <= now() RETURNING user_id`)
	if err != nil {
		return err
	}
	dndOver, err := collect(`
		UPDATE presence_status SET dnd_until = NULL, updated_at = now() WHERE dnd_until <= now() RETURNING user_id`)
	if err != nil {
		return err
	}
	s.reconcile(ctx, dedupe(append(expired, dndOver...)), true)

	live, err := collect(`SELECT user_id FROM presence_status WHERE announced <> 'offline'`)
	if err != nil {
		return err
	}
	for start := 0; start < len(live); start += 500 {
		s.reconcile(ctx, live[start:min(start+500, len(live))], false)
	}
	_, err = s.d.Pool.Exec(ctx, `DELETE FROM presence_devices WHERE last_seen_at < now() - interval '1 day'`)
	return err
}

func dedupe(ids []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]bool, len(ids))
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if id != uuid.Nil && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}
