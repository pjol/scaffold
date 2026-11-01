package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/bg"
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/mail"
	"scaffoldapp/backend/internal/notify"
	"scaffoldapp/backend/internal/realtime"
)

// EventUnread is published on the user topic with {"unread_count": n}
// whenever the caller's unread count may have changed.
const EventUnread = "notifications.unread"

// Limits applied to what callers send.
const (
	maxTitleRunes = 200
	maxBodyRunes  = 2000
	maxDevices    = 10 // per user; the least recently seen are pruned
	pushTimeout   = 45 * time.Second
	pushParallel  = 8 // concurrent push deliveries per instance
)

// Sender replaces Deps.Notify (api.RegisterDeps). Send stores the inbox row,
// publishes it over realtime, then pushes and emails in the background as
// the kind and the user's preferences allow. Only the inbox insert can fail
// the call; everything after it is logged.
type Sender struct {
	pool   *pgxpool.Pool
	broker realtime.Broker
	mail   mail.Mailer
	webURL string
	expo   *expoClient
	sem    chan struct{}
}

// NewSender builds the sender from Deps and the feature's settings.
func NewSender(d *app.Deps) *Sender {
	return &Sender{
		pool:   d.Pool,
		broker: d.Broker,
		mail:   d.Mail,
		webURL: strings.TrimRight(d.Cfg.PublicWebURL, "/"),
		expo: newExpoClient(
			config.String("EXPO_ACCESS_TOKEN", ""),
			config.String("PUSH_SINK_DIR", ""),
			d.Cfg.IsDevelopment(),
		),
		sem: make(chan struct{}, pushParallel),
	}
}

// PushMode reports how pushes go out: expo, sink or log.
func (s *Sender) PushMode() string { return s.expo.mode }

// Send implements notify.Sender.
func (s *Sender) Send(ctx context.Context, n notify.Notification) error {
	_, err := s.deliver(ctx, n, deliverOpts{})
	return err
}

type deliverOpts struct {
	forcePush bool // ignore the push preference (staff test sends)
	noEmail   bool
}

// delivery is what deliver did, for the admin test action.
type delivery struct {
	Item    Item
	Devices int
	Push    bool
	Email   bool
}

// errRecipientGone means the user is deleted; nothing was stored.
var errRecipientGone = errors.New("notifications: recipient deleted")

func (s *Sender) deliver(ctx context.Context, n notify.Notification, opts deliverOpts) (*delivery, error) {
	n.Kind = strings.TrimSpace(n.Kind)
	n.Title = truncate(strings.TrimSpace(n.Title), maxTitleRunes)
	n.Body = truncate(strings.TrimSpace(n.Body), maxBodyRunes)
	if n.UserID == uuid.Nil || n.Kind == "" || n.Title == "" {
		return nil, fmt.Errorf("notifications: user, kind and title are required (kind %q)", n.Kind)
	}
	if n.Data == nil {
		n.Data = map[string]any{}
	}
	data, err := json.Marshal(n.Data)
	if err != nil {
		return nil, fmt.Errorf("notifications: data: %w", err)
	}

	// Inbox row (always). Deleted users get nothing.
	item := Item{Kind: n.Kind, Title: n.Title, Body: n.Body, Link: n.Link, Data: n.Data}
	var email string
	err = s.pool.QueryRow(ctx, `
		WITH u AS (SELECT id, email FROM users WHERE id = $1 AND status <> 'deleted')
		INSERT INTO notifications (user_id, kind, title, body, link, data)
		SELECT u.id, $2, $3, $4, $5, $6 FROM u
		RETURNING id, created_at, (SELECT email FROM u)`,
		n.UserID, n.Kind, n.Title, n.Body, n.Link, data,
	).Scan(&item.ID, &item.CreatedAt, &email)
	if db.IsNoRows(err) {
		slog.DebugContext(ctx, "notifications: recipient deleted, dropped", "kind", n.Kind, "user_id", n.UserID)
		return nil, errRecipientGone
	}
	if err != nil {
		return nil, err
	}

	// Realtime: the item (a superset of core's notify.Notification) and the
	// new unread count.
	unread, err := unreadCount(ctx, s.pool, n.UserID)
	if err != nil {
		slog.WarnContext(ctx, "notifications: unread count", "err", err)
	}
	s.publish(ctx, n.UserID, realtime.EventNotification, Event{Item: item, UserID: n.UserID, UnreadCount: unread})
	s.publishUnread(ctx, n.UserID, unread)

	// Channels.
	kind, _ := lookupKind(n.Kind)
	push, mailOn := kind.Push, kind.Email
	if n.Kind == kindTest {
		push, mailOn = true, false
	} else {
		var p, e bool
		err := s.pool.QueryRow(ctx, `SELECT push, email FROM notification_preferences WHERE user_id = $1 AND kind = $2`,
			n.UserID, n.Kind).Scan(&p, &e)
		switch {
		case err == nil:
			push, mailOn = p, e
		case !db.IsNoRows(err):
			slog.WarnContext(ctx, "notifications: preferences", "err", err)
		}
	}
	if opts.forcePush {
		push = true
	}
	if opts.noEmail {
		mailOn = false
	}

	out := &delivery{Item: item, Push: push, Email: mailOn && email != ""}
	if push {
		var count int
		if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM notification_devices WHERE user_id = $1 AND platform <> 'web'`,
			n.UserID).Scan(&count); err != nil {
			slog.WarnContext(ctx, "notifications: count devices", "err", err)
		}
		out.Devices = count
	}
	pushOn := push && out.Devices > 0
	if !pushOn && !out.Email {
		return out, nil
	}
	// Push, email and delivery filters (AddDeliveryFilter) run in the
	// background (bg.Go), so Send never waits on them and shutdown waits for
	// them. Staff test sends skip filters, as they skip preferences.
	sendPush := func(ctx context.Context) error { s.push(ctx, n.UserID, item, unread); return nil }
	sendEmail := func(ctx context.Context) error { s.email(ctx, email, kind, item); return nil }
	if opts.forcePush || !hasFilters() {
		if pushOn {
			bg.Go(ctx, "notifications.push", sendPush)
		}
		if out.Email {
			bg.Go(ctx, "notifications.email", sendEmail)
		}
		return out, nil
	}
	bg.Go(ctx, "notifications.deliver", func(ctx context.Context) error {
		pushOn, mailOn := applyFilters(ctx, n, pushOn, out.Email)
		if mailOn {
			bg.Go(ctx, "notifications.email", sendEmail)
		}
		if pushOn {
			return sendPush(ctx)
		}
		return nil
	})
	return out, nil
}

// push sends item to every iOS/Android device of userID through Expo in
// batches of 100. A failed batch or token is logged and the rest still go
// out. Tickets are stored for the receipts job; DeviceNotRegistered tokens
// are removed at once.
func (s *Sender) push(ctx context.Context, userID uuid.UUID, item Item, unread int) {
	s.sem <- struct{}{}
	defer func() { <-s.sem }()
	ctx, cancel := context.WithTimeout(ctx, pushTimeout)
	defer cancel()

	rows, err := s.pool.Query(ctx, `SELECT token FROM notification_devices
		WHERE user_id = $1 AND platform <> 'web' ORDER BY last_seen_at DESC LIMIT $2`, userID, maxDevices)
	if err != nil {
		slog.WarnContext(ctx, "notifications: load devices", "err", err)
		return
	}
	var tokens []string
	for rows.Next() {
		var t string
		if rows.Scan(&t) == nil {
			tokens = append(tokens, t)
		}
	}
	rows.Close()
	if len(tokens) == 0 {
		return
	}

	data := map[string]any{}
	for k, v := range item.Data {
		data[k] = v
	}
	data["notification_id"] = item.ID.String()
	data["kind"] = item.Kind
	if item.Link != "" {
		data["link"] = item.Link
	}
	badge := unread
	msgs := make([]expoMessage, len(tokens))
	for i, tok := range tokens {
		msgs[i] = expoMessage{To: tok, Title: item.Title, Body: item.Body, Data: data, Sound: "default", Badge: &badge, Priority: "high"}
	}

	var sent, failed, pruned int
	for _, o := range s.expo.sendAll(ctx, item.Kind+"-"+item.ID.String(), msgs) {
		switch {
		case o.Dead:
			pruned++
			removeDevice(ctx, s.pool, o.Token)
		case o.Err != "":
			failed++
			slog.WarnContext(ctx, "notifications: push failed", "notification_id", item.ID, "err", o.Err)
		default:
			sent++
			if o.TicketID == "" {
				continue
			}
			if _, err := s.pool.Exec(ctx, `INSERT INTO notification_push_tickets (ticket_id, token, user_id)
				VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, o.TicketID, o.Token, userID); err != nil {
				slog.WarnContext(ctx, "notifications: store ticket", "err", err)
			}
		}
	}
	slog.InfoContext(ctx, "push", "mode", s.expo.mode, "notification_id", item.ID, "kind", item.Kind,
		"user_id", userID, "devices", len(tokens), "sent", sent, "failed", failed, "pruned", pruned)
}

// email sends the notification as a plain-text message.
func (s *Sender) email(ctx context.Context, to string, kind Kind, item Item) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var b strings.Builder
	b.WriteString(item.Title)
	if item.Body != "" {
		b.WriteString("\n\n" + item.Body)
	}
	if link := s.absoluteLink(item.Link); link != "" {
		b.WriteString("\n\nOpen: " + link)
	}
	if s.webURL != "" {
		fmt.Fprintf(&b, "\n\n—\nEmails for %s are on. Turn them off: %s/settings/notifications", kind.Label, s.webURL)
	}
	if err := s.mail.Send(ctx, mail.Message{To: to, Subject: item.Title, Text: b.String()}); err != nil {
		slog.WarnContext(ctx, "notifications: email failed", "notification_id", item.ID, "kind", item.Kind, "err", err)
	}
}

func (s *Sender) absoluteLink(link string) string {
	switch {
	case link == "":
		return ""
	case strings.HasPrefix(link, "https://"), strings.HasPrefix(link, "http://"):
		return link
	case strings.HasPrefix(link, "/") && s.webURL != "":
		return s.webURL + link
	}
	return ""
}

func (s *Sender) publish(ctx context.Context, userID uuid.UUID, event string, data any) {
	if err := s.broker.Publish(ctx, realtime.UserTopic(userID), event, data); err != nil {
		slog.WarnContext(ctx, "notifications: realtime publish failed", "event", event, "err", err)
	}
}

func (s *Sender) publishUnread(ctx context.Context, userID uuid.UUID, unread int) {
	s.publish(ctx, userID, EventUnread, UnreadCount{UnreadCount: unread})
}

func unreadCount(ctx context.Context, q db.Querier, userID uuid.UUID) (int, error) {
	var n int
	err := q.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE user_id = $1 AND read_at IS NULL`, userID).Scan(&n)
	return n, err
}

func removeDevice(ctx context.Context, q db.Querier, token string) {
	if _, err := q.Exec(ctx, `DELETE FROM notification_devices WHERE token = $1`, token); err != nil {
		slog.WarnContext(ctx, "notifications: remove device", "err", err)
	}
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
