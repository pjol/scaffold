package notifications

// Inbox depth: unread again, snooze, groups by resource, filters by kind,
// and muting a resource from the inbox.

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/bg"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/notify"
	"scaffoldapp/backend/internal/realtime"
)

const (
	maxSnooze    = 30 * 24 * time.Hour
	maxResources = 50 // per group request
	threadSep    = "/thread:"
)

// ---- resources -------------------------------------------------------------

var resourceRE = regexp.MustCompile(`^[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*:[A-Za-z0-9_.:-]{1,100}(/thread:[A-Za-z0-9_.:-]{1,100})?$`)

// resourceOf names what a notification is about: the kind's Group, else
// its AgentTarget, else data {"kind", "id"}; refined to a thread by data
// "thread_id" or a link that opens one (?thread=<id>). "" when unknown.
func resourceOf(ctx context.Context, kind string, data map[string]any, link string) (out string) {
	k, _ := lookupKind(kind)
	var rk, id string
	func() {
		defer func() {
			if r := recover(); r != nil {
				slog.ErrorContext(ctx, "notifications: group function panicked", "kind", kind, "panic", fmt.Sprint(r))
				rk, id = "", ""
			}
		}()
		switch {
		case k.Group != nil:
			rk, id = k.Group(data)
		case k.AgentTarget != nil:
			rk, id = k.AgentTarget(data)
		default:
			rk, _ = data["kind"].(string)
			id, _ = data["id"].(string)
		}
	}()
	rk, id = strings.TrimSpace(rk), strings.TrimSpace(id)
	if rk == "" || id == "" {
		return ""
	}
	out = rk + ":" + id
	thread, _ := data["thread_id"].(string)
	if thread == "" && strings.Contains(link, "thread=") {
		if u, err := url.Parse(link); err == nil {
			thread = u.Query().Get("thread")
		}
	}
	if thread = strings.TrimSpace(thread); thread != "" {
		out += threadSep + thread
	}
	if !resourceRE.MatchString(out) {
		return ""
	}
	return out
}

// resourceKind is "channels.channel" for "channels.channel:<id>/thread:<t>".
func resourceKind(resource string) string {
	k, _, _ := strings.Cut(resource, ":")
	return k
}

// resourceParent is the resource without its thread, or "" for none.
func resourceParent(resource string) string {
	if p, _, ok := strings.Cut(resource, threadSep); ok {
		return p
	}
	return ""
}

// ---- mute seam -------------------------------------------------------------

// MuteHandler keeps the mute state of one resource kind in its own feature
// (a channel's own mute setting), so muting from the inbox and from the
// feature are the same switch. SetMuted checks that userID may see the
// resource; ids are the part after "<kind>:" (a thread keeps its suffix).
type MuteHandler interface {
	SetMuted(ctx context.Context, userID uuid.UUID, id string, muted bool) error
}

var (
	muteMu       sync.RWMutex
	muteKinds    = map[string]bool{}
	muteHandlers = map[string]MuteHandler{}
)

// AllowMute lets people mute notifications about one resource kind from the
// inbox ("Mute #general"); this feature stores the mute and drops new
// notifications about it (a muted resource mutes its threads). Call it from
// the owning feature's New or the app's stitching.
func AllowMute(resourceKind string) {
	muteMu.Lock()
	muteKinds[strings.TrimSpace(resourceKind)] = true
	muteMu.Unlock()
}

// RegisterMuteHandler sends "Mute" from the inbox to the feature that owns
// the resource kind instead of storing it here; that feature then sends
// nothing about muted resources itself.
func RegisterMuteHandler(resourceKind string, h MuteHandler) {
	if h == nil {
		return
	}
	muteMu.Lock()
	muteHandlers[strings.TrimSpace(resourceKind)] = h
	muteMu.Unlock()
}

func muteAllowed(resource string) bool {
	if resource == "" {
		return false
	}
	k := resourceKind(resource)
	muteMu.RLock()
	defer muteMu.RUnlock()
	return muteKinds[k] || muteHandlers[k] != nil
}

func muteHandler(resource string) MuteHandler {
	muteMu.RLock()
	defer muteMu.RUnlock()
	return muteHandlers[resourceKind(resource)]
}

// isMuted: the resource or its parent (a thread's channel) is muted here.
func (s *Sender) isMuted(ctx context.Context, userID uuid.UUID, resource string) (bool, error) {
	if muteHandler(resource) != nil {
		return false, nil // the owning feature stops sending itself
	}
	var muted bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM notification_mutes WHERE user_id = $1 AND resource = ANY($2))`,
		userID, []string{resource, resourceParent(resource)}).Scan(&muted)
	return muted, err
}

// Mute is one muted resource as listed in settings.
type Mute struct {
	Resource  string    `json:"resource"`
	Label     string    `json:"label"`
	CreatedAt time.Time `json:"created_at"`
}

// GET /notifications/mutes
func (s *Service) listMutes(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context()).UserID
	rows, err := s.d.Pool.Query(r.Context(), `SELECT resource, label, created_at FROM notification_mutes WHERE user_id = $1 ORDER BY created_at DESC LIMIT 500`, me)
	if err != nil {
		return err
	}
	items, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Mute])
	if err != nil {
		return err
	}
	if items == nil {
		items = []Mute{}
	}
	httpx.OK(w, map[string]any{"items": items})
	return nil
}

type muteReq struct {
	Resource string `json:"resource"`
	Label    string `json:"label"`
}

// POST /notifications/mutes {resource, label} — for a resource you were
// notified about whose kind allows muting. Its unread notifications turn
// read and nothing new arrives about it.
func (s *Service) mute(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	var req muteReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	req.Resource = strings.TrimSpace(req.Resource)
	if !resourceRE.MatchString(req.Resource) || !muteAllowed(req.Resource) {
		return httpx.BadRequest("This can't be muted from the inbox.")
	}
	label := truncate(strings.Join(strings.Fields(req.Label), " "), 200)
	// Only something you were notified about (ownership of the client id).
	var known bool
	if err := s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM notifications WHERE user_id = $1 AND (resource = $2 OR resource LIKE $2 || '/thread:%'))`,
		me, req.Resource).Scan(&known); err != nil {
		return err
	}
	if !known {
		return httpx.NotFound("You have no notifications about that.")
	}
	if h := muteHandler(req.Resource); h != nil {
		_, id, _ := strings.Cut(req.Resource, ":")
		if err := h.SetMuted(ctx, me, id, true); err != nil {
			return err
		}
	} else if _, err := s.d.Pool.Exec(ctx, `
		INSERT INTO notification_mutes (user_id, resource, label) VALUES ($1, $2, $3)
		ON CONFLICT (user_id, resource) DO UPDATE SET label = EXCLUDED.label`, me, req.Resource, label); err != nil {
		return err
	}
	ids, err := s.idsForResources(ctx, me, []string{req.Resource}, true)
	if err != nil {
		return err
	}
	n, err := unreadCount(ctx, s.d.Pool, me)
	if len(ids) > 0 && err == nil {
		n, err = s.markReadFor(ctx, me, markReadReq{IDs: ids})
	}
	if err != nil {
		return err
	}
	httpx.OK(w, UnreadCount{UnreadCount: n})
	return nil
}

// DELETE /notifications/mutes?resource=
func (s *Service) unmute(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	resource := strings.TrimSpace(r.URL.Query().Get("resource"))
	if !resourceRE.MatchString(resource) {
		return httpx.BadRequest("Send resource.")
	}
	if h := muteHandler(resource); h != nil {
		_, id, _ := strings.Cut(resource, ":")
		if err := h.SetMuted(ctx, me, id, false); err != nil {
			return err
		}
	}
	if _, err := s.d.Pool.Exec(ctx, `DELETE FROM notification_mutes WHERE user_id = $1 AND resource = $2`, me, resource); err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

// ---- unread, snooze --------------------------------------------------------

// idsForResources lists the caller's notifications about resources (and
// their threads), unread ones only when unreadOnly.
func (s *Service) idsForResources(ctx context.Context, userID uuid.UUID, resources []string, unreadOnly bool) ([]uuid.UUID, error) {
	if len(resources) > maxResources {
		return nil, httpx.BadRequest("Send at most 50 resources.")
	}
	for _, res := range resources {
		if !resourceRE.MatchString(res) && !isIDKey(res) {
			return nil, httpx.BadRequest("Unknown resource " + res + ".")
		}
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT id FROM notifications
		WHERE user_id = $1 AND (NOT $3 OR read_at IS NULL)
		  AND (resource = ANY($2) OR id::text = ANY($2) OR EXISTS (SELECT 1 FROM unnest($2::text[]) g WHERE resource LIKE g || '/thread:%'))
		LIMIT 2000`, userID, resources, unreadOnly)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

// isIDKey: a group of one notification is keyed by its id.
func isIDKey(s string) bool {
	_, err := uuid.Parse(s)
	return err == nil
}

type idsReq struct {
	IDs       []uuid.UUID `json:"ids"`
	Resources []string    `json:"resources"`
}

func (s *Service) resolveIDs(ctx context.Context, userID uuid.UUID, req idsReq, unreadOnly bool) ([]uuid.UUID, error) {
	switch {
	case len(req.IDs) > 0 && len(req.Resources) > 0:
		return nil, httpx.BadRequest("Send ids or resources, not both.")
	case len(req.Resources) > 0:
		return s.idsForResources(ctx, userID, req.Resources, unreadOnly)
	case len(req.IDs) == 0:
		return nil, httpx.BadRequest("Send ids or resources.")
	case len(req.IDs) > maxMarkReadIDs:
		return nil, httpx.BadRequest("Send at most 500 at a time.")
	}
	return req.IDs, nil
}

// POST /notifications/unread {ids | resources} — read ones turn unread.
func (s *Service) markUnread(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	var req idsReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	ids, err := s.resolveIDs(ctx, me, req, false)
	if err != nil {
		return err
	}
	if _, err := s.d.Pool.Exec(ctx, `UPDATE notifications SET read_at = NULL WHERE user_id = $1 AND id = ANY($2) AND read_at IS NOT NULL`, me, ids); err != nil {
		return err
	}
	return s.respondUnread(ctx, w, me)
}

func (s *Service) respondUnread(ctx context.Context, w http.ResponseWriter, userID uuid.UUID) error {
	n, err := unreadCount(ctx, s.d.Pool, userID)
	if err != nil {
		return err
	}
	s.sender.publishUnread(ctx, userID, n)
	httpx.OK(w, UnreadCount{UnreadCount: n})
	return nil
}

type snoozeReq struct {
	idsReq
	Until *time.Time `json:"until"`
}

// POST /notifications/snooze {ids | resources, until} — hidden (and out of
// the unread count) until then; they come back unread at the top.
func (s *Service) snooze(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	var req snoozeReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	now := time.Now()
	if req.Until == nil || !req.Until.After(now.Add(time.Minute)) {
		return httpx.BadRequest("Pick a time in the future.")
	}
	if req.Until.Sub(now) > maxSnooze {
		return httpx.BadRequest("Snooze for up to 30 days.")
	}
	ids, err := s.resolveIDs(ctx, me, req.idsReq, false)
	if err != nil {
		return err
	}
	if _, err := s.d.Pool.Exec(ctx, `UPDATE notifications SET snoozed_until = $3 WHERE user_id = $1 AND id = ANY($2)`, me, ids, req.Until.UTC()); err != nil {
		return err
	}
	return s.respondUnread(ctx, w, me)
}

// POST /notifications/unsnooze {ids | resources} — back in the inbox now.
func (s *Service) unsnooze(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	var req idsReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	ids, err := s.resolveIDs(ctx, me, req, false)
	if err != nil {
		return err
	}
	if _, err := s.d.Pool.Exec(ctx, `UPDATE notifications SET snoozed_until = NULL, sort_at = now()
		WHERE user_id = $1 AND id = ANY($2) AND snoozed_until IS NOT NULL`, me, ids); err != nil {
		return err
	}
	return s.respondUnread(ctx, w, me)
}

// wakeSnoozed brings snoozed notifications back when their time comes:
// unread, at the top of the inbox, announced live, and pushed again as the
// kind, the person's preferences and the delivery filters allow (a snooze
// is "remind me").
func (s *Service) wakeSnoozed(ctx context.Context) error {
	rows, err := s.d.Pool.Query(ctx, `
		UPDATE notifications SET snoozed_until = NULL, read_at = NULL, sort_at = now()
		WHERE id IN (SELECT id FROM notifications WHERE snoozed_until <= now() ORDER BY snoozed_until LIMIT 500)
		RETURNING user_id, `+itemCols)
	if err != nil {
		return err
	}
	type woke struct {
		user uuid.UUID
		item Item
	}
	var list []woke
	for rows.Next() {
		var w woke
		if err := rows.Scan(&w.user, &w.item.ID, &w.item.Kind, &w.item.Title, &w.item.Body, &w.item.Link, &w.item.Data,
			&w.item.ReadAt, &w.item.CreatedAt, &w.item.SortAt, &w.item.SnoozedUntil, &w.item.Resource); err != nil {
			rows.Close()
			return err
		}
		if w.item.Data == nil {
			w.item.Data = map[string]any{}
		}
		w.item.Mutable = muteAllowed(w.item.Resource)
		list = append(list, w)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, w := range list {
		s.sender.redeliver(ctx, w.user, w.item)
	}
	return nil
}

// redeliver announces a woken notification and pushes it again (never email).
func (s *Sender) redeliver(ctx context.Context, userID uuid.UUID, item Item) {
	unread, err := unreadCount(ctx, s.pool, userID)
	if err != nil {
		slog.WarnContext(ctx, "notifications: unread count", "err", err)
	}
	s.publish(ctx, userID, realtime.EventNotification, Event{Item: item, UserID: userID, UnreadCount: unread})
	s.publishUnread(ctx, userID, unread)
	kind, _ := lookupKind(item.Kind)
	push := kind.Push
	var p, e bool
	switch err := s.pool.QueryRow(ctx, `SELECT push, email FROM notification_preferences WHERE user_id = $1 AND kind = $2`, userID, item.Kind).Scan(&p, &e); {
	case err == nil:
		push = p
	case !db.IsNoRows(err):
		slog.WarnContext(ctx, "notifications: preferences", "err", err)
	}
	if !push {
		return
	}
	n := notify.Notification{UserID: userID, Kind: item.Kind, Title: item.Title, Body: item.Body, Link: item.Link, Data: item.Data}
	bgPush := func(ctx context.Context) error {
		if hasFilters() {
			if ok, _ := applyFilters(ctx, n, true, false); !ok {
				return nil
			}
		}
		s.push(ctx, userID, item, unread)
		return nil
	}
	bg.Go(ctx, "notifications.snooze_push", bgPush)
}

// ---- groups and kinds ------------------------------------------------------

// Group is notifications about one resource, newest first: the latest one,
// how many and how many unread. A notification about no resource is a group
// of one keyed by its id.
type Group struct {
	Key    string `json:"key"`
	Count  int    `json:"count"`
	Unread int    `json:"unread"`
	Latest Item   `json:"latest"`
}

type groupCursor struct {
	T   time.Time `json:"t"`
	Key string    `json:"k"`
}

// GroupsPage is GET /notifications/groups.
type GroupsPage struct {
	httpx.Page[Group]
	UnreadCount int `json:"unread_count"`
}

// GET /notifications/groups?unread=&snoozed=&kind=&cursor= — the inbox
// grouped by thread or resource (GitHub-style), newest group first.
func (s *Service) listGroups(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	p := httpx.ParsePage(r)
	var c groupCursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	f, err := parseInboxFilter(r)
	if err != nil {
		return err
	}
	groups, err := s.groups(ctx, me, f, c, p.Limit+1)
	if err != nil {
		return err
	}
	out := GroupsPage{Page: httpx.NewPage(groups, p.Limit, func(g Group) any { return groupCursor{g.Latest.SortAt, g.Key} })}
	if out.UnreadCount, err = unreadCount(ctx, s.d.Pool, me); err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

func (s *Service) groups(ctx context.Context, userID uuid.UUID, f inboxFilter, c groupCursor, limit int) ([]Group, error) {
	rows, err := s.d.Pool.Query(ctx, `
		WITH m AS (
		  SELECT *, CASE WHEN resource = '' THEN id::text ELSE resource END AS gkey FROM notifications WHERE `+inboxWhere+`
		), g AS (
		  SELECT gkey, max(sort_at) AS at, count(*) AS n, count(*) FILTER (WHERE read_at IS NULL) AS unread FROM m GROUP BY gkey
		)
		SELECT g.gkey, g.n, g.unread, l.id, l.kind, l.title, l.body, l.link, l.data, l.read_at, l.created_at, l.sort_at, l.snoozed_until, l.resource
		FROM g
		JOIN LATERAL (SELECT * FROM m WHERE m.gkey = g.gkey ORDER BY m.sort_at DESC, m.id DESC LIMIT 1) l ON true
		WHERE ($5::timestamptz IS NULL OR (g.at, g.gkey) < ($5, $6))
		ORDER BY g.at DESC, g.gkey DESC LIMIT $7`,
		userID, f.UnreadOnly, f.Snoozed, f.Kind, nullTime(c.T), c.Key, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Group, error) {
		var g Group
		it := &g.Latest
		err := row.Scan(&g.Key, &g.Count, &g.Unread, &it.ID, &it.Kind, &it.Title, &it.Body, &it.Link, &it.Data, &it.ReadAt,
			&it.CreatedAt, &it.SortAt, &it.SnoozedUntil, &it.Resource)
		if it.Data == nil {
			it.Data = map[string]any{}
		}
		it.Mutable = muteAllowed(it.Resource)
		return g, err
	})
}

// InboxKindCount is one filter chip: a feature's notifications in the inbox.
type InboxKindCount struct {
	Group  string `json:"group"` // feature prefix ("channels")
	Count  int    `json:"count"`
	Unread int    `json:"unread"`
}

// GET /notifications/kinds?snoozed= — per-feature counts for filter chips.
func (s *Service) listKinds(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	snoozed := r.URL.Query().Get("snoozed") == "true"
	rows, err := s.d.Pool.Query(ctx, `
		SELECT split_part(kind, '.', 1) AS g, count(*), count(*) FILTER (WHERE read_at IS NULL)
		FROM notifications WHERE `+inboxWhere+`
		GROUP BY g ORDER BY g`, me, false, snoozed, "")
	if err != nil {
		return err
	}
	items, err := pgx.CollectRows(rows, pgx.RowToStructByPos[InboxKindCount])
	if err != nil {
		return err
	}
	if items == nil {
		items = []InboxKindCount{}
	}
	var snoozedCount int
	if err := s.d.Pool.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE user_id = $1 AND snoozed_until > now()`, me).Scan(&snoozedCount); err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": items, "snoozed": snoozedCount})
	return nil
}
