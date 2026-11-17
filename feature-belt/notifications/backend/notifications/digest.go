package notifications

// Email digest: an opt-in daily or weekly email of unread notifications at
// a local hour. It is held like any email (delivery filters: Do Not Disturb
// and quiet hours) and retried every 30 minutes until they end, for up to
// 12 hours after its slot.

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/mail"
	"scaffoldapp/backend/internal/notify"
)

const (
	digestKind      = "notifications.digest" // what delivery filters see
	digestRetry     = 30 * time.Minute
	digestGiveUp    = 12 * time.Hour
	digestMaxItems  = 25
	digestBatch     = 100
	digestAppName   = "Scaffold App"
	digestMaxZoneLn = 64
)

// DigestSettings is GET/PUT /notifications/digest.
type DigestSettings struct {
	Cadence    string     `json:"cadence"`   // off | daily | weekly
	Hour       int        `json:"hour"`      // local hour, 0–23
	Weekday    int        `json:"weekday"`   // weekly: 0 = Sunday
	TimeZone   string     `json:"time_zone"` // IANA
	NextAt     *time.Time `json:"next_at"`
	LastSentAt *time.Time `json:"last_sent_at"`
}

func (s *Service) digestSettings(ctx context.Context, userID uuid.UUID) (DigestSettings, error) {
	out := DigestSettings{Cadence: "off", Hour: 8, Weekday: 1}
	err := s.d.Pool.QueryRow(ctx, `SELECT cadence, hour, weekday, time_zone, next_at, last_sent_at FROM notification_digests WHERE user_id = $1`, userID).
		Scan(&out.Cadence, &out.Hour, &out.Weekday, &out.TimeZone, &out.NextAt, &out.LastSentAt)
	if db.IsNoRows(err) {
		return out, nil
	}
	return out, err
}

// GET /notifications/digest
func (s *Service) getDigest(w http.ResponseWriter, r *http.Request) error {
	out, err := s.digestSettings(r.Context(), auth.MustFromContext(r.Context()).UserID)
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

// PUT /notifications/digest {cadence, hour, weekday, time_zone}
func (s *Service) putDigest(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	var req DigestSettings
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	switch req.Cadence {
	case "off":
		if _, err := s.d.Pool.Exec(ctx, `DELETE FROM notification_digests WHERE user_id = $1`, me); err != nil {
			return err
		}
		return s.getDigest(w, r)
	case "daily", "weekly":
	default:
		return httpx.BadRequest("cadence must be off, daily or weekly.")
	}
	if req.Hour < 0 || req.Hour > 23 || req.Weekday < 0 || req.Weekday > 6 {
		return httpx.BadRequest("hour is 0 to 23 and weekday 0 (Sunday) to 6.")
	}
	if req.TimeZone == "" || req.TimeZone == "Local" || len(req.TimeZone) > digestMaxZoneLn {
		return httpx.BadRequest("Send time_zone: your IANA time zone, like Europe/Berlin.")
	}
	loc, err := time.LoadLocation(req.TimeZone)
	if err != nil {
		return httpx.BadRequest("Unknown time zone " + req.TimeZone + ".")
	}
	next := nextDigestSlot(time.Now(), req.Cadence, req.Hour, time.Weekday(req.Weekday), loc)
	if _, err := s.d.Pool.Exec(ctx, `
		INSERT INTO notification_digests (user_id, cadence, hour, weekday, time_zone, next_at) VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (user_id) DO UPDATE SET cadence = EXCLUDED.cadence, hour = EXCLUDED.hour, weekday = EXCLUDED.weekday,
		  time_zone = EXCLUDED.time_zone, next_at = EXCLUDED.next_at, updated_at = now()`,
		me, req.Cadence, req.Hour, req.Weekday, req.TimeZone, next); err != nil {
		return err
	}
	return s.getDigest(w, r)
}

// nextDigestSlot is the first slot strictly after now: hour:00 local, every
// day or on weekday.
func nextDigestSlot(now time.Time, cadence string, hour int, weekday time.Weekday, loc *time.Location) time.Time {
	l := now.In(loc)
	t := time.Date(l.Year(), l.Month(), l.Day(), hour, 0, 0, 0, loc)
	if cadence == "weekly" {
		t = t.AddDate(0, 0, (int(weekday)-int(t.Weekday())+7)%7)
		if !t.After(now) {
			t = t.AddDate(0, 0, 7)
		}
		return t
	}
	if !t.After(now) {
		t = t.AddDate(0, 0, 1)
	}
	return t
}

// period is how far back a digest looks the first time.
func digestPeriod(cadence string) time.Duration {
	if cadence == "weekly" {
		return 7 * 24 * time.Hour
	}
	return 24 * time.Hour
}

type dueDigest struct {
	user      uuid.UUID
	cadence   string
	hour      int
	weekday   int
	zone      string
	lastSent  *time.Time
	email     string
	createdAt time.Time
}

// sendDigests sends every digest whose slot came, unless delivery filters
// hold email for that person right now (then it retries).
func (s *Service) sendDigests(ctx context.Context) error {
	rows, err := s.d.Pool.Query(ctx, `
		SELECT d.user_id, d.cadence, d.hour, d.weekday, d.time_zone, d.last_sent_at, u.email, d.created_at
		FROM notification_digests d JOIN users u ON u.id = d.user_id AND u.status = 'active'
		WHERE d.next_at <= now() ORDER BY d.next_at LIMIT $1`, digestBatch)
	if err != nil {
		return err
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (dueDigest, error) {
		var d dueDigest
		err := row.Scan(&d.user, &d.cadence, &d.hour, &d.weekday, &d.zone, &d.lastSent, &d.email, &d.createdAt)
		return d, err
	})
	if err != nil {
		return err
	}
	for _, d := range list {
		if err := s.sendDigest(ctx, d); err != nil {
			slog.WarnContext(ctx, "notifications: digest", "user_id", d.user, "err", err)
		}
	}
	return nil
}

func (s *Service) sendDigest(ctx context.Context, d dueDigest) error {
	now := time.Now()
	loc, err := time.LoadLocation(d.zone)
	if err != nil {
		loc = time.UTC
	}
	next := nextDigestSlot(now, d.cadence, d.hour, time.Weekday(d.weekday), loc)
	reschedule := func(at time.Time, sent bool) error {
		_, err := s.d.Pool.Exec(ctx, `UPDATE notification_digests SET next_at = $2,
			last_sent_at = CASE WHEN $3 THEN now() ELSE last_sent_at END, updated_at = now() WHERE user_id = $1`, d.user, at, sent)
		return err
	}
	if d.email == "" {
		return reschedule(next, false)
	}
	// Held by Do Not Disturb or quiet hours: try again soon, until 12 hours
	// after the slot it belongs to; then wait for the next slot.
	if hasFilters() {
		_, mailOn := applyFilters(ctx, notify.Notification{UserID: d.user, Kind: digestKind, Title: "Digest"}, false, true)
		if !mailOn {
			slot := next.Add(-digestPeriod(d.cadence))
			if now.Sub(slot) < digestGiveUp {
				return reschedule(now.Add(digestRetry), false)
			}
			return reschedule(next, false)
		}
	}
	since := now.Add(-digestPeriod(d.cadence))
	if d.lastSent != nil && d.lastSent.After(since) {
		since = *d.lastSent
	}
	var total int
	if err := s.d.Pool.QueryRow(ctx, `SELECT count(*) FROM notifications
		WHERE user_id = $1 AND read_at IS NULL AND (snoozed_until IS NULL OR snoozed_until <= now()) AND sort_at > $2`,
		d.user, since).Scan(&total); err != nil {
		return err
	}
	if total == 0 {
		return reschedule(next, false)
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT `+itemCols+` FROM notifications
		WHERE user_id = $1 AND read_at IS NULL AND (snoozed_until IS NULL OR snoozed_until <= now()) AND sort_at > $2
		ORDER BY sort_at DESC, id DESC LIMIT $3`, d.user, since, digestMaxItems)
	if err != nil {
		return err
	}
	items, err := pgx.CollectRows(rows, scanItem)
	if err != nil {
		return err
	}
	msg := s.digestMessage(d, items, total)
	if err := s.d.Mail.Send(ctx, msg); err != nil {
		return reschedule(now.Add(digestRetry), false)
	}
	return reschedule(next, true)
}

// digestMessage lists the unread notifications, newest first, grouped by
// feature, each with its link.
func (s *Service) digestMessage(d dueDigest, items []Item, total int) mail.Message {
	web := s.sender.webURL
	subject := fmt.Sprintf("%d unread notifications", total)
	if total == 1 {
		subject = "1 unread notification"
	}
	var text, body strings.Builder
	text.WriteString(subject + "\n")
	body.WriteString(`<p style="margin:0 0 16px">` + html.EscapeString(subject) + `</p>`)
	group := ""
	for _, it := range items {
		if g := kindGroup(it.Kind); g != group {
			group = g
			label := strings.ToUpper(group[:1]) + strings.ReplaceAll(group[1:], "_", " ")
			text.WriteString("\n" + label + "\n")
			body.WriteString(`<p style="margin:16px 0 4px;font-weight:600">` + html.EscapeString(label) + `</p>`)
		}
		link := s.sender.absoluteLink(it.Link)
		text.WriteString("- " + it.Title)
		if it.Body != "" {
			text.WriteString(": " + truncate(it.Body, 140))
		}
		if link != "" {
			text.WriteString("\n  " + link)
		}
		text.WriteString("\n")
		title := html.EscapeString(it.Title)
		if link != "" {
			title = `<a href="` + html.EscapeString(link) + `" style="color:#1a1a1a">` + title + `</a>`
		}
		body.WriteString(`<p style="margin:0 0 8px">` + title)
		if it.Body != "" {
			body.WriteString(`<br><span style="color:#5c5c5c">` + html.EscapeString(truncate(it.Body, 140)) + `</span>`)
		}
		body.WriteString(`</p>`)
	}
	if total > len(items) {
		more := fmt.Sprintf("And %d more.", total-len(items))
		text.WriteString("\n" + more + "\n")
		body.WriteString(`<p style="margin:16px 0 0">` + more + `</p>`)
	}
	if web != "" {
		fmt.Fprintf(&text, "\nOpen your inbox: %s/notifications\nChange or stop this digest: %s/settings/notifications\n", web, web)
		body.WriteString(`<p style="margin:24px 0 0;color:#5c5c5c;font-size:13px"><a href="` + html.EscapeString(web) + `/notifications">Open your inbox</a> · <a href="` +
			html.EscapeString(web) + `/settings/notifications">Change or stop this digest</a></p>`)
	}
	return mail.Message{To: d.email, Subject: subject, Text: text.String(), HTML: mail.Layout(digestAppName, body.String())}
}
