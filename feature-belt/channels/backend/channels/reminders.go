package channels

// Reminders: "remind me about this message" (the message also goes to
// Later) and "/remind me to … tomorrow" for free text. The
// channels.fire_reminders job notifies the person when one is due; a
// reminder about a message they can no longer read fires without its text.

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/notify"
	"scaffoldapp/backend/internal/orgs"
)

const maxRemindersPerUser = 500

// Reminder is one of the caller's reminders.
type Reminder struct {
	ID          uuid.UUID  `json:"id"`
	Text        string     `json:"text"`
	Message     *Message   `json:"message"` // null for text reminders and messages you can't read any more
	Channel     *BriefChan `json:"channel"`
	RemindAt    time.Time  `json:"remind_at"`
	FiredAt     *time.Time `json:"fired_at"`
	CompletedAt *time.Time `json:"completed_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

type reminderReq struct {
	MessageID *uuid.UUID `json:"message_id"` // remind me about this message, or
	Text      string     `json:"text"`       // what to remember
	Command   string     `json:"command"`    // or "/remind" text: "me to call Sam tomorrow at 3pm"
	At        *time.Time `json:"at"`         // when, or
	When      string     `json:"when"`       // "in 20 minutes", "tomorrow", "next week"
	TZ        string     `json:"tz"`         // IANA; defaults to the person's zone
}

// POST /channels/orgs/{orgID}/reminders — 201 with the reminder.
func (s *Service) createReminder(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	m, _ := orgs.FromContext(ctx)
	var req reminderReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	rem, err := s.remind(ctx, me, m.OrgID, req)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, rem)
	return nil
}

// remind creates a reminder as me in orgID (the agent tool uses it too).
func (s *Service) remind(ctx context.Context, me, orgID uuid.UUID, req reminderReq) (*Reminder, error) {
	loc := zoneFor(ctx, s.d.Pool, me, orgID, req.TZ)
	now := time.Now()
	text := strings.TrimSpace(req.Text)
	var at time.Time
	switch {
	case strings.TrimSpace(req.Command) != "":
		what, t, err := remindCommand(req.Command, now, loc)
		if err != nil {
			if err == errWhenUnknown {
				return nil, httpx.BadRequest(`Couldn't read that. Try "/remind me to call Sam tomorrow at 3pm".`)
			}
			return nil, whenError(err)
		}
		text, at = what, t
	default:
		t, err := resolveSendAt(req.At, req.When, now, loc)
		if err != nil {
			return nil, err
		}
		at = t
	}
	if utf8.RuneCountInString(text) > 1000 {
		return nil, httpx.BadRequest("Reminders can be up to 1000 characters.")
	}
	if req.MessageID == nil && text == "" {
		return nil, httpx.BadRequest("Say what to remind you about.")
	}
	if err := checkText(ctx, text); err != nil {
		return nil, err
	}
	if req.MessageID != nil {
		msg, err := s.loadMessage(ctx, s.d.Pool, *req.MessageID, me)
		if err != nil {
			return nil, err
		}
		if msg.OrgID != orgID {
			return nil, errMessageNotFound
		}
		if msg.Kind == msgSystem || msg.Deleted {
			return nil, httpx.Conflict("You can't set a reminder on this message.")
		}
	}
	var id uuid.UUID
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM channel_reminders WHERE user_id = $1 AND completed_at IS NULL`, me).Scan(&n); err != nil {
			return err
		}
		if n >= maxRemindersPerUser {
			return httpx.BadRequest("You have 500 open reminders, the most you can have. Mark some done first.")
		}
		if req.MessageID != nil {
			// One open reminder per message: a new time replaces the old one.
			if _, err := tx.Exec(ctx, `DELETE FROM channel_reminders WHERE user_id = $1 AND message_id = $2 AND completed_at IS NULL`, me, *req.MessageID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO channel_saved (user_id, message_id) VALUES ($1, $2)
				ON CONFLICT (user_id, message_id) DO UPDATE SET completed_at = NULL, updated_at = now()`, me, *req.MessageID); err != nil {
				return err
			}
		}
		return tx.QueryRow(ctx, `
			INSERT INTO channel_reminders (user_id, org_id, message_id, text, remind_at) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
			me, orgID, req.MessageID, text, at).Scan(&id)
	})
	if err != nil {
		return nil, err
	}
	s.toUser(ctx, me, EventSidebar, SidebarEvent{OrgID: orgID, Reason: "later"})
	return s.reminderOne(ctx, me, id)
}

func (s *Service) reminderOne(ctx context.Context, me, id uuid.UUID) (*Reminder, error) {
	list, err := s.reminders(ctx, me, nil, &id, false, true, cursor{}, 1)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, httpx.NotFound("Reminder not found.")
	}
	return &list[0], nil
}

// reminders reads the caller's reminders, soonest first (done: most
// recently completed first). Message text only where they can still read it.
func (s *Service) reminders(ctx context.Context, me uuid.UUID, orgID, id *uuid.UUID, done, all bool, c cursor, limit int) ([]Reminder, error) {
	order := `r.remind_at, r.id`
	cmp := `>`
	if done {
		order, cmp = `r.completed_at DESC, r.id DESC`, `<`
	}
	keyCol := `r.remind_at`
	if done {
		keyCol = `r.completed_at`
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT r.id, r.text, r.message_id, m.channel_id, r.remind_at, r.fired_at, r.completed_at, r.created_at
		FROM channel_reminders r LEFT JOIN channel_messages m ON m.id = r.message_id
		WHERE r.user_id = $1 AND ($2::uuid IS NULL OR r.org_id = $2) AND ($3::uuid IS NULL OR r.id = $3)
		  AND ($4 OR (r.completed_at IS NOT NULL) = $5)
		  AND EXISTS (SELECT 1 FROM org_members om WHERE om.org_id = r.org_id AND om.user_id = $1)
		  AND ($6::timestamptz IS NULL OR (`+keyCol+`, r.id) `+cmp+` ($6, $7))
		ORDER BY `+order+` LIMIT $8`, me, orgID, id, all, done, nullTime(c.T), c.ID, limit)
	if err != nil {
		return nil, err
	}
	type row struct {
		Reminder
		msgID, channelID *uuid.UUID
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
		var x row
		err := r.Scan(&x.ID, &x.Text, &x.msgID, &x.channelID, &x.RemindAt, &x.FiredAt, &x.CompletedAt, &x.CreatedAt)
		return x, err
	})
	if err != nil {
		return nil, err
	}
	msgIDs, chans := []uuid.UUID{}, []uuid.UUID{}
	readable := map[uuid.UUID]bool{}
	for _, x := range list {
		if x.msgID == nil {
			continue
		}
		if _, err := s.loadMessage(ctx, s.d.Pool, *x.msgID, me); err == nil {
			readable[*x.msgID] = true
			msgIDs = append(msgIDs, *x.msgID)
			chans = append(chans, *x.channelID)
		}
	}
	msgs, err := s.messagesByID(ctx, s.d.Pool, msgIDs, me, false)
	if err != nil {
		return nil, err
	}
	refs, err := s.channelRefs(ctx, me, chans)
	if err != nil {
		return nil, err
	}
	out := make([]Reminder, 0, len(list))
	for _, x := range list {
		rem := x.Reminder
		if x.msgID != nil && readable[*x.msgID] {
			rem.Message = msgs[*x.msgID]
			ref := refs[*x.channelID]
			rem.Channel = &ref
		}
		out = append(out, rem)
	}
	return out, nil
}

// GET /channels/orgs/{orgID}/reminders?state=open|done&cursor=
func (s *Service) listReminders(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	m, _ := orgs.FromContext(ctx)
	p := httpx.ParsePage(r)
	var c cursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	done := r.URL.Query().Get("state") == "done"
	list, err := s.reminders(ctx, me, &m.OrgID, nil, done, false, c, p.Limit+1)
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(list, p.Limit, func(x Reminder) any {
		if done && x.CompletedAt != nil {
			return cursor{*x.CompletedAt, x.ID}
		}
		return cursor{x.RemindAt, x.ID}
	}))
	return nil
}

type reminderUpdate struct {
	At   *time.Time `json:"at"`
	When string     `json:"when"`
	TZ   string     `json:"tz"`
	Done *bool      `json:"done"`
}

// PATCH /channels/reminders/{id} {at? | when?, done?} — reschedule (it fires
// again) or mark done / not done.
func (s *Service) updateReminder(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req reminderUpdate
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	var orgID uuid.UUID
	if err := s.d.Pool.QueryRow(ctx, `SELECT org_id FROM channel_reminders WHERE id = $1 AND user_id = $2`, id, me).Scan(&orgID); err != nil {
		if db.IsNoRows(err) {
			return httpx.NotFound("Reminder not found.")
		}
		return err
	}
	var at *time.Time
	if req.At != nil || strings.TrimSpace(req.When) != "" {
		t, err := resolveSendAt(req.At, req.When, time.Now(), zoneFor(ctx, s.d.Pool, me, orgID, req.TZ))
		if err != nil {
			return err
		}
		at = &t
	}
	if _, err := s.d.Pool.Exec(ctx, `
		UPDATE channel_reminders SET
		  remind_at = COALESCE($3, remind_at),
		  fired_at = CASE WHEN $3::timestamptz IS NOT NULL THEN NULL ELSE fired_at END,
		  completed_at = CASE WHEN $4::boolean IS NULL THEN (CASE WHEN $3::timestamptz IS NOT NULL THEN NULL ELSE completed_at END)
		                      WHEN $4 THEN COALESCE(completed_at, now()) ELSE NULL END,
		  updated_at = now()
		WHERE id = $1 AND user_id = $2`, id, me, at, req.Done); err != nil {
		return err
	}
	s.toUser(ctx, me, EventSidebar, SidebarEvent{OrgID: orgID, Reason: "later"})
	rem, err := s.reminderOne(ctx, me, id)
	if err != nil {
		return err
	}
	httpx.OK(w, rem)
	return nil
}

// DELETE /channels/reminders/{id}
func (s *Service) deleteReminder(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var orgID uuid.UUID
	err = s.d.Pool.QueryRow(ctx, `DELETE FROM channel_reminders WHERE id = $1 AND user_id = $2 RETURNING org_id`, id, me).Scan(&orgID)
	if db.IsNoRows(err) {
		return httpx.NotFound("Reminder not found.")
	}
	if err != nil {
		return err
	}
	s.toUser(ctx, me, EventSidebar, SidebarEvent{OrgID: orgID, Reason: "later"})
	httpx.NoContent(w)
	return nil
}

// fireReminders is the job: notifies everyone whose reminder is due.
func (s *Service) fireReminders(ctx context.Context) error {
	rows, err := s.d.Pool.Query(ctx, `
		UPDATE channel_reminders SET fired_at = now(), updated_at = now()
		WHERE id IN (SELECT id FROM channel_reminders WHERE fired_at IS NULL AND completed_at IS NULL AND remind_at <= now()
		             ORDER BY remind_at LIMIT 500)
		RETURNING id, user_id, org_id, message_id, text`)
	if err != nil {
		return err
	}
	type due struct {
		id, user, org uuid.UUID
		msg           *uuid.UUID
		text          string
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (due, error) {
		var d due
		return d, r.Scan(&d.id, &d.user, &d.org, &d.msg, &d.text)
	})
	if err != nil {
		return err
	}
	for _, d := range list {
		title, body, link := "Reminder", d.text, "/channels/later?tab=reminders"
		data := map[string]any{"org_id": d.org.String(), "reminder_id": d.id.String()}
		if d.msg != nil {
			if m, err := s.loadMessage(ctx, s.d.Pool, *d.msg, d.user); err == nil && !m.Deleted {
				refs, _ := s.channelRefs(ctx, d.user, []uuid.UUID{m.ChannelID})
				where := refs[m.ChannelID].Name
				if !m.direct() {
					where = "#" + where
				}
				title = "Reminder: message in " + where
				if msg, err := s.message(ctx, s.d.Pool, *d.msg, d.user); err == nil && body == "" {
					body = preview(msg)
				}
				link = "/channels/" + m.ChannelID.String() + "?message=" + d.msg.String()
				if m.ThreadRootID != nil {
					link = "/channels/" + m.ChannelID.String() + "?thread=" + m.ThreadRootID.String() + "&message=" + d.msg.String()
				}
				data["channel_id"] = m.ChannelID.String()
				data["message_id"] = d.msg.String()
			} else if body == "" {
				body = "About a message you can't open any more."
			}
		}
		if err := s.d.Notify.Send(ctx, notify.Notification{UserID: d.user, Kind: KindReminder, Title: title, Body: body, Link: link, Data: data}); err != nil {
			slog.WarnContext(ctx, "channels: reminder", "reminder_id", d.id, "err", err)
		}
		s.toUser(ctx, d.user, EventSidebar, SidebarEvent{OrgID: d.org, Reason: "later"})
	}
	return nil
}
