package calendar

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/mail"
	"scaffoldapp/backend/internal/notify"
)

// Every outward side effect (invite and cancellation email, notification,
// conference cancellation) is queued in calendar_outbox inside the same
// transaction as the change it reports, then sent by drainOutbox after the
// commit, with retries. A change that rolls back sends nothing; a change
// that commits is reported even if the process restarts.
//
// Sending never holds a transaction: a batch is claimed in one short
// statement (FOR UPDATE SKIP LOCKED, a fresh claim id, next_attempt_at
// pushed out by outboxLease), sent with no connection held, and each
// result is recorded by id and claim. A sender that crashes mid-batch
// leaves its rows to come due again when the lease ends (at least once);
// a sender whose lease ran out records nothing over the newer claim.

const maxOutboxAttempts = 8

var kick = make(chan struct{}, 1)

// kickOutbox asks the outbox loop to drain now (non-blocking).
func kickOutbox() {
	select {
	case kick <- struct{}{}:
	default:
	}
}

func enqueue(ctx context.Context, q db.Querier, kind string, payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `INSERT INTO calendar_outbox (kind, payload) VALUES ($1, $2)`, kind, b)
	return err
}

func enqueueMail(ctx context.Context, q db.Querier, m mail.Message) error {
	if m.To == "" {
		return nil
	}
	return enqueue(ctx, q, "mail", m)
}

func enqueueNotify(ctx context.Context, q db.Querier, n notify.Notification) error {
	if n.UserID == uuid.Nil {
		return nil
	}
	return enqueue(ctx, q, "notify", n)
}

func enqueueConferenceCancel(ctx context.Context, q db.Querier, id *string) error {
	if id == nil || *id == "" {
		return nil
	}
	// Rooms can be shared by the two halves of a split series: only cancel
	// when nothing references the room any more.
	var used bool
	if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM calendar_events WHERE conference_id = $1)
		OR EXISTS (SELECT 1 FROM calendar_bookings WHERE conference_id = $1 AND status = 'confirmed')`, *id).Scan(&used); err != nil {
		return err
	}
	if used {
		return nil
	}
	return enqueue(ctx, q, "conference_cancel", map[string]string{"id": *id})
}

// outboxLoop drains whenever a request kicks it (the 15-second job is the
// safety net for restarts and retries).
func (s *Service) outboxLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-kick:
			if err := s.drainOutbox(ctx); err != nil && ctx.Err() == nil {
				slog.WarnContext(ctx, "calendar: outbox drain failed", "err", err)
			}
		}
	}
}

const (
	outboxBatch    = 10
	outboxSendTime = 30 * time.Second // one delivery's deadline
	// outboxLease covers a whole batch at the delivery deadline, twice over.
	outboxLease = 2 * outboxBatch * outboxSendTime
)

// drainOutbox sends due items in batches until none are due (at most 20
// batches a run). Concurrent drains claim disjoint rows.
func (s *Service) drainOutbox(ctx context.Context) error {
	for range 20 {
		n, err := s.drainBatch(ctx)
		if err != nil || n < outboxBatch {
			return err
		}
	}
	return nil
}

type outboxItem struct {
	id       int64
	kind     string
	payload  []byte
	attempts int
}

// drainBatch claims up to outboxBatch due rows, sends them and records
// each result.
func (s *Service) drainBatch(ctx context.Context) (int, error) {
	claim := uuid.New()
	rows, err := s.d.Pool.Query(ctx, `
		UPDATE calendar_outbox o SET claim = $1, next_attempt_at = now() + make_interval(secs => $2)
		FROM (SELECT id FROM calendar_outbox WHERE next_attempt_at <= now() ORDER BY id LIMIT $3 FOR UPDATE SKIP LOCKED) due
		WHERE o.id = due.id
		RETURNING o.id, o.kind, o.payload, o.attempts`, claim, outboxLease.Seconds(), outboxBatch)
	if err != nil {
		return 0, err
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (outboxItem, error) {
		var it outboxItem
		return it, row.Scan(&it.id, &it.kind, &it.payload, &it.attempts)
	})
	if err != nil {
		return 0, err
	}
	slices.SortFunc(items, func(a, b outboxItem) int { return cmp.Compare(a.id, b.id) })
	for _, it := range items {
		if ctx.Err() != nil {
			return len(items), ctx.Err() // unsent rows come due when the lease ends
		}
		dctx, cancel := context.WithTimeout(ctx, outboxSendTime)
		sendErr := s.deliver(dctx, it.kind, it.payload)
		cancel()
		if ctx.Err() != nil {
			return len(items), ctx.Err() // shutting down: not a failed attempt
		}
		if err := s.recordOutbox(ctx, it, claim, sendErr); err != nil {
			return len(items), err
		}
	}
	return len(items), nil
}

// recordOutbox stores one delivery's result: sent (or out of attempts)
// rows are deleted, failures wait a backoff. Only the holder of the claim
// writes, so a result is recorded once.
func (s *Service) recordOutbox(ctx context.Context, it outboxItem, claim uuid.UUID, sendErr error) error {
	if sendErr == nil || it.attempts+1 >= maxOutboxAttempts {
		if sendErr != nil {
			slog.ErrorContext(ctx, "calendar: outbox item dropped", "id", it.id, "kind", it.kind, "err", sendErr)
		}
		_, err := s.d.Pool.Exec(ctx, `DELETE FROM calendar_outbox WHERE id = $1 AND claim = $2`, it.id, claim)
		return err
	}
	backoff := time.Minute << it.attempts
	slog.WarnContext(ctx, "calendar: outbox send failed, will retry", "id", it.id, "kind", it.kind, "err", sendErr)
	_, err := s.d.Pool.Exec(ctx, `UPDATE calendar_outbox SET attempts = attempts + 1, claim = NULL,
		next_attempt_at = now() + make_interval(secs => $3) WHERE id = $1 AND claim = $2`, it.id, claim, backoff.Seconds())
	return err
}

func (s *Service) deliver(ctx context.Context, kind string, payload []byte) error {
	switch kind {
	case "mail":
		var m mail.Message
		if err := json.Unmarshal(payload, &m); err != nil {
			return nil // unreadable: drop
		}
		return s.d.Mail.Send(ctx, m)
	case "notify":
		var n notify.Notification
		if err := json.Unmarshal(payload, &n); err != nil {
			return nil
		}
		return s.d.Notify.Send(ctx, n)
	case "conference_cancel":
		var p struct{ ID string }
		if err := json.Unmarshal(payload, &p); err != nil || p.ID == "" {
			return nil
		}
		c := conferencingProvider()
		if c == nil {
			return nil
		}
		return c.Cancel(ctx, p.ID)
	}
	return nil
}

// ---- wording -----------------------------------------------------------------

// formatWhen is "Mon, Nov 2, 2026 · 10:00 AM – 10:30 AM EST", or for
// all-day "Mon, Nov 2, 2026" / "Mon, Nov 2 – Wed, Nov 4, 2026".
func formatWhen(start, end time.Time, allDay bool, loc *time.Location) string {
	ls, le := start.In(loc), end.In(loc)
	if allDay {
		last := le.Add(-time.Second)
		if ls.Format(time.DateOnly) == last.Format(time.DateOnly) {
			return ls.Format("Mon, Jan 2, 2006") + " (all day)"
		}
		return ls.Format("Mon, Jan 2") + " – " + last.Format("Mon, Jan 2, 2006") + " (all day)"
	}
	zone := ls.Format("MST")
	if strings.ContainsAny(zone, "+-") {
		zone = "UTC" + zone
	}
	if ls.Format(time.DateOnly) == le.Format(time.DateOnly) {
		return ls.Format("Mon, Jan 2, 2006 · 3:04 PM") + " – " + le.Format("3:04 PM") + " " + zone
	}
	return ls.Format("Mon, Jan 2, 2006, 3:04 PM") + " – " + le.Format("Mon, Jan 2, 3:04 PM") + " " + zone
}

var dayNames = [...]string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}

// describeRule says how a series repeats: "Every week on Monday and
// Wednesday, until Dec 31, 2026".
func describeRule(r Rule, loc *time.Location) string {
	unit := map[string]string{"DAILY": "day", "WEEKLY": "week", "MONTHLY": "month", "YEARLY": "year"}[r.Freq]
	s := "Every " + unit
	if r.Interval > 1 {
		s = fmt.Sprintf("Every %d %ss", r.Interval, unit)
	}
	if len(r.ByDay) > 0 {
		var days []string
		for _, d := range r.ByDay {
			name := dayNames[d.Day]
			switch {
			case d.N == -1:
				name = "the last " + name
			case d.N > 0:
				name = "the " + ordinal(d.N) + " " + name
			}
			days = append(days, name)
		}
		s += " on " + joinAnd(days)
	}
	if len(r.ByMonthDay) > 0 {
		var days []string
		for _, d := range r.ByMonthDay {
			if d == -1 {
				days = append(days, "the last day")
			} else {
				days = append(days, "day "+fmt.Sprint(d))
			}
		}
		s += " on " + joinAnd(days)
	}
	switch {
	case r.Count > 0:
		s += fmt.Sprintf(", %d times", r.Count)
	case r.Until != nil:
		s += ", until " + r.Until.In(loc).Format("Jan 2, 2006")
	}
	return s
}

func ordinal(n int) string {
	switch n {
	case 1:
		return "first"
	case 2:
		return "second"
	case 3:
		return "third"
	case 4:
		return "fourth"
	case 5:
		return "fifth"
	}
	return fmt.Sprintf("%dth", n)
}

func joinAnd(v []string) string {
	switch len(v) {
	case 0:
		return ""
	case 1:
		return v[0]
	}
	return strings.Join(v[:len(v)-1], ", ") + " and " + v[len(v)-1]
}

// mailBody is a message's plain text and HTML (the house layout) from
// paragraphs and labelled links.
type mailLink struct{ Label, URL string }

func (s *Service) mailBody(paras []string, links []mailLink) (string, string) {
	var t, h strings.Builder
	for _, p := range paras {
		if p == "" {
			continue
		}
		t.WriteString(p + "\n\n")
		h.WriteString(`<p style="margin:0 0 16px">` + strings.ReplaceAll(html.EscapeString(p), "\n", "<br>") + `</p>`)
	}
	for _, l := range links {
		if l.URL == "" {
			continue
		}
		t.WriteString(l.Label + ": " + l.URL + "\n")
		h.WriteString(`<p style="margin:0 0 8px"><a href="` + html.EscapeString(l.URL) + `" style="color:#1a1a1a;font-weight:600">` +
			html.EscapeString(l.Label) + `</a></p>`)
	}
	return strings.TrimSpace(t.String()) + "\n", mail.Layout(appName, h.String())
}

// appName is the product name in mail (the CLI replaces the token).
const appName = "Scaffold App"

func icsAttachment(method string, body []byte) mail.Attachment {
	name := "invite.ics"
	if method == "CANCEL" {
		name = "cancel.ics"
	}
	return mail.Attachment{Filename: name, ContentType: "text/calendar; charset=utf-8; method=" + method, Content: body}
}
