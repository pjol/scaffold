package channels

// Scheduled messages: written now, sent later by the channels.send_scheduled
// job as the person, through the same checks as a send (access, posting
// rules, blocks, the filter). A message that can't be sent stays as failed
// with the reason, and the person is notified.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

const (
	maxScheduledPerUser = 100
	minScheduleLead     = 30 * time.Second
)

// Scheduled is a message waiting to be sent.
type Scheduled struct {
	ID                uuid.UUID      `json:"id"`
	Channel           BriefChan      `json:"channel"`
	ThreadRootID      *uuid.UUID     `json:"thread_root_id"`
	AlsoSendToChannel bool           `json:"also_send_to_channel"`
	Body              string         `json:"body"`
	Mentions          []UserRef      `json:"mentions"`
	Attachments       []attachmentIn `json:"attachments"`
	SendAt            time.Time      `json:"send_at"`
	Status            string         `json:"status"` // scheduled | failed
	Error             string         `json:"error"`  // why it failed
	CreatedAt         time.Time      `json:"created_at"`
}

type scheduleReq struct {
	Body              string         `json:"body"`
	ThreadRootID      *uuid.UUID     `json:"thread_root_id"`
	AlsoSendToChannel bool           `json:"also_send_to_channel"`
	Attachments       []attachmentIn `json:"attachments"`
	SendAt            *time.Time     `json:"send_at"` // or
	When              string         `json:"when"`    // "tomorrow at 9am", read in tz
	TZ                string         `json:"tz"`      // IANA; defaults to the person's zone
}

// resolveSendAt reads send_at or when, after now and within a year.
func resolveSendAt(at *time.Time, when string, now time.Time, loc *time.Location) (time.Time, error) {
	var t time.Time
	switch {
	case at != nil:
		t = *at
	case strings.TrimSpace(when) != "":
		var err error
		if t, err = parseWhen(when, now, loc); err != nil {
			return time.Time{}, whenError(err)
		}
	default:
		return time.Time{}, httpx.BadRequest("Choose when to send it.")
	}
	if t.Before(now.Add(minScheduleLead)) {
		return time.Time{}, httpx.BadRequest("Choose a time at least a minute from now.")
	}
	if t.Sub(now) > maxAhead {
		return time.Time{}, httpx.BadRequest("Choose a time within a year.")
	}
	return t.UTC(), nil
}

func whenError(err error) error {
	switch {
	case errors.Is(err, errWhenPast):
		return httpx.BadRequest("That time has passed. Choose a later one.")
	case errors.Is(err, errWhenFar):
		return httpx.BadRequest("Choose a time within a year.")
	}
	return httpx.BadRequest(`Couldn't read that time. Try "in 20 minutes", "tomorrow at 9am" or "friday".`)
}

// checkScheduled validates a scheduled message the way a send would, now.
func (s *Service) checkScheduled(ctx context.Context, me uuid.UUID, a *access, req scheduleReq) error {
	body := strings.TrimSpace(req.Body)
	switch {
	case body == "" && len(req.Attachments) == 0:
		return httpx.BadRequest("Write a message or attach a file.")
	case utf8.RuneCountInString(body) > s.cfg.maxChars:
		return httpx.BadRequest(fmt.Sprintf("Messages can be up to %d characters.", s.cfg.maxChars))
	case len(req.Attachments) > maxAttachments:
		return httpx.BadRequest(fmt.Sprintf("Attach up to %d files per message.", maxAttachments))
	case req.AlsoSendToChannel && req.ThreadRootID == nil:
		return httpx.BadRequest("Only thread replies can also be sent to the channel.")
	case a.Layout == layoutForum && req.ThreadRootID == nil:
		return httpx.BadRequest("Schedule replies in forum posts; new posts are sent at once.")
	}
	if _, err := s.parseAttachments(me, req.Attachments); err != nil {
		return err
	}
	if err := checkText(ctx, body); err != nil {
		return err
	}
	if err := a.requireWritable(); err != nil {
		return err
	}
	if err := s.requirePost(ctx, s.d.Pool, a, req.ThreadRootID != nil); err != nil {
		return err
	}
	if req.ThreadRootID != nil {
		var ok bool
		if err := s.d.Pool.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM channel_messages WHERE id = $1 AND channel_id = $2 AND thread_root_id IS NULL AND kind <> 'system' AND deleted_at IS NULL)`,
			*req.ThreadRootID, a.ChannelID).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return errThreadScope
		}
	}
	return nil
}

// POST /channels/channels/{id}/scheduled — 201 with the scheduled message.
func (s *Service) createScheduled(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	channelID, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req scheduleReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	item, err := s.schedule(ctx, me, channelID, req)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, item)
	return nil
}

// schedule stores a scheduled message as me (the agent tool uses it too).
func (s *Service) schedule(ctx context.Context, me, channelID uuid.UUID, req scheduleReq) (*Scheduled, error) {
	a, err := s.loadAccess(ctx, s.d.Pool, channelID, me)
	if err != nil {
		return nil, err
	}
	at, err := resolveSendAt(req.SendAt, req.When, time.Now(), zoneFor(ctx, s.d.Pool, me, a.OrgID, req.TZ))
	if err != nil {
		return nil, err
	}
	if err := s.checkScheduled(ctx, me, a, req); err != nil {
		return nil, err
	}
	atts, err := json.Marshal(nonNilAttachments(req.Attachments))
	if err != nil {
		return nil, err
	}
	var id uuid.UUID
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM channel_scheduled WHERE user_id = $1`, me).Scan(&n); err != nil {
			return err
		}
		if n >= maxScheduledPerUser {
			return httpx.BadRequest("You have 100 scheduled messages, the most you can have. Send or delete some first.")
		}
		return tx.QueryRow(ctx, `
			INSERT INTO channel_scheduled (user_id, channel_id, thread_root_id, also_in_channel, body, attachments, send_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
			me, channelID, req.ThreadRootID, req.AlsoSendToChannel, strings.TrimSpace(req.Body), atts, at).Scan(&id)
	})
	if err != nil {
		return nil, err
	}
	s.clearDraft(ctx, me, a.OrgID, channelID, req.ThreadRootID)
	s.toUser(ctx, me, EventSidebar, SidebarEvent{OrgID: a.OrgID, ChannelID: channelID, Reason: "scheduled"})
	return s.scheduledOne(ctx, me, id)
}

func nonNilAttachments(in []attachmentIn) []attachmentIn {
	if in == nil {
		return []attachmentIn{}
	}
	return in
}

// scheduledList reads the caller's scheduled messages in conversations they
// can still read, soonest first.
func (s *Service) scheduledList(ctx context.Context, me uuid.UUID, orgID, id *uuid.UUID, c cursor, limit int) ([]Scheduled, error) {
	rows, err := s.d.Pool.Query(ctx, `
		SELECT x.id, x.channel_id, x.thread_root_id, x.also_in_channel, x.body, x.attachments, x.send_at, x.status, x.error, x.created_at
		FROM channel_scheduled x JOIN channels c ON c.id = x.channel_id
		WHERE x.user_id = $1 AND `+readableSQL+`
		  AND ($2::uuid IS NULL OR c.org_id = $2) AND ($3::uuid IS NULL OR x.id = $3)
		  AND ($4::timestamptz IS NULL OR (x.send_at, x.id) > ($4, $5))
		ORDER BY x.send_at, x.id LIMIT $6`, me, orgID, id, nullTime(c.T), c.ID, limit)
	if err != nil {
		return nil, err
	}
	type row struct {
		Scheduled
		channel uuid.UUID
		atts    []byte
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
		var x row
		err := r.Scan(&x.ID, &x.channel, &x.ThreadRootID, &x.AlsoSendToChannel, &x.Body, &x.atts, &x.SendAt, &x.Status, &x.Error, &x.CreatedAt)
		return x, err
	})
	if err != nil {
		return nil, err
	}
	chans, mentioned := []uuid.UUID{}, []uuid.UUID{}
	for _, x := range list {
		chans = append(chans, x.channel)
		mentioned = append(mentioned, mentionedUserIDs(x.Body)...)
	}
	refs, err := s.channelRefs(ctx, me, chans)
	if err != nil {
		return nil, err
	}
	users, err := loadUsers(ctx, s.d.Pool, dedupe(mentioned))
	if err != nil {
		return nil, err
	}
	out := make([]Scheduled, 0, len(list))
	for _, x := range list {
		item := x.Scheduled
		item.Channel, item.Mentions, item.Attachments = refs[x.channel], []UserRef{}, []attachmentIn{}
		if err := json.Unmarshal(x.atts, &item.Attachments); err != nil {
			return nil, err
		}
		for _, id := range mentionedUserIDs(item.Body) {
			if u, ok := users[id]; ok {
				item.Mentions = append(item.Mentions, u)
			}
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *Service) scheduledOne(ctx context.Context, me, id uuid.UUID) (*Scheduled, error) {
	list, err := s.scheduledList(ctx, me, nil, &id, cursor{}, 1)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, httpx.NotFound("Scheduled message not found.")
	}
	return &list[0], nil
}

// GET /channels/orgs/{orgID}/scheduled?cursor= — soonest first.
func (s *Service) listScheduled(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	m, _ := orgs.FromContext(ctx)
	p := httpx.ParsePage(r)
	var c cursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	list, err := s.scheduledList(ctx, me, &m.OrgID, nil, c, p.Limit+1)
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(list, p.Limit, func(x Scheduled) any { return cursor{x.SendAt, x.ID} }))
	return nil
}

type scheduleUpdate struct {
	Body   *string    `json:"body"`
	SendAt *time.Time `json:"send_at"`
	When   string     `json:"when"`
	TZ     string     `json:"tz"`
}

// PATCH /channels/scheduled/{id} {body?, send_at? | when?} — edit or
// reschedule (a failed one goes back to scheduled).
func (s *Service) updateScheduled(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req scheduleUpdate
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	cur, err := s.scheduledOne(ctx, me, id)
	if err != nil {
		return err
	}
	a, err := s.loadAccess(ctx, s.d.Pool, cur.Channel.ID, me)
	if err != nil {
		return err
	}
	next := scheduleReq{Body: cur.Body, ThreadRootID: cur.ThreadRootID, AlsoSendToChannel: cur.AlsoSendToChannel, Attachments: cur.Attachments}
	if req.Body != nil {
		next.Body = *req.Body
	}
	sendAt := cur.SendAt
	if req.SendAt != nil || strings.TrimSpace(req.When) != "" {
		if sendAt, err = resolveSendAt(req.SendAt, req.When, time.Now(), zoneFor(ctx, s.d.Pool, me, a.OrgID, req.TZ)); err != nil {
			return err
		}
	} else if cur.Status == "failed" || sendAt.Before(time.Now().Add(minScheduleLead)) {
		return httpx.BadRequest("Choose a new time to send it.")
	}
	if err := s.checkScheduled(ctx, me, a, next); err != nil {
		return err
	}
	if _, err := s.d.Pool.Exec(ctx, `
		UPDATE channel_scheduled SET body = $3, send_at = $4, status = 'scheduled', error = '', updated_at = now()
		WHERE id = $1 AND user_id = $2`, id, me, strings.TrimSpace(next.Body), sendAt); err != nil {
		return err
	}
	s.toUser(ctx, me, EventSidebar, SidebarEvent{OrgID: a.OrgID, ChannelID: a.ChannelID, Reason: "scheduled"})
	item, err := s.scheduledOne(ctx, me, id)
	if err != nil {
		return err
	}
	httpx.OK(w, item)
	return nil
}

// DELETE /channels/scheduled/{id} — cancel; its uploads are deleted.
func (s *Service) deleteScheduled(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var orgID, channelID uuid.UUID
	var atts []byte
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			DELETE FROM channel_scheduled x USING channels c WHERE c.id = x.channel_id AND x.id = $1 AND x.user_id = $2
			RETURNING c.org_id, c.id, x.attachments`, id, me).Scan(&orgID, &channelID, &atts)
		if db.IsNoRows(err) {
			return httpx.NotFound("Scheduled message not found.")
		}
		if err != nil {
			return err
		}
		return queueUploads(ctx, tx, atts)
	})
	if err != nil {
		return err
	}
	s.kickObjectCleanup(ctx)
	s.toUser(ctx, me, EventSidebar, SidebarEvent{OrgID: orgID, ChannelID: channelID, Reason: "scheduled"})
	httpx.NoContent(w)
	return nil
}

// queueUploads sends a cancelled message's uploads to the clean-up queue
// (unless something else holds them).
func queueUploads(ctx context.Context, tx pgx.Tx, raw []byte) error {
	var atts []attachmentIn
	if err := json.Unmarshal(raw, &atts); err != nil || len(atts) == 0 {
		return err
	}
	keys := make([]string, 0, len(atts))
	for _, a := range atts {
		keys = append(keys, a.Key)
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO channel_deleted_objects (storage_key)
		SELECT k FROM unnest($1::text[]) k
		WHERE NOT EXISTS (SELECT 1 FROM channel_attachments a WHERE a.storage_key = k)
		ON CONFLICT (storage_key) DO NOTHING`, keys)
	return err
}

// POST /channels/scheduled/{id}/send — send it now; returns the message.
func (s *Service) sendScheduledNow(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	if _, err := s.scheduledOne(ctx, me, id); err != nil {
		return err
	}
	msg, err := s.sendScheduled(ctx, id)
	if err != nil {
		return err
	}
	httpx.OK(w, msg)
	return nil
}

// sendScheduled posts one scheduled message as its author and removes it;
// on failure it is marked failed with the reason.
func (s *Service) sendScheduled(ctx context.Context, id uuid.UUID) (*Message, error) {
	var x struct {
		user, channel uuid.UUID
		root          *uuid.UUID
		also          bool
		body          string
		atts          []byte
		status        string
	}
	err := s.d.Pool.QueryRow(ctx, `
		SELECT user_id, channel_id, thread_root_id, also_in_channel, body, attachments, status FROM channel_scheduled WHERE id = $1`, id).
		Scan(&x.user, &x.channel, &x.root, &x.also, &x.body, &x.atts, &x.status)
	if db.IsNoRows(err) {
		return nil, httpx.NotFound("Scheduled message not found.")
	}
	if err != nil {
		return nil, err
	}
	var atts []attachmentIn
	if err := json.Unmarshal(x.atts, &atts); err != nil {
		return nil, err
	}
	var status string
	if err := s.d.Pool.QueryRow(ctx, `SELECT status FROM users WHERE id = $1`, x.user).Scan(&status); err != nil {
		return nil, err
	}
	var msg *Message
	if status != "active" {
		err = httpx.ErrSuspended
	} else {
		// The client id makes a retry after a crash return the message already sent.
		msg, _, err = s.post(ctx, x.user, x.channel, sendReq{ClientID: "sched_" + id.String(), Body: x.body, ThreadRootID: x.root,
			AlsoSendToChannel: x.also, Attachments: atts, keepDraft: true})
	}
	if err != nil {
		reason := "Couldn't send it."
		var he *httpx.Error
		if errors.As(err, &he) {
			reason = he.Message
		} else {
			slog.WarnContext(ctx, "channels: send scheduled", "scheduled_id", id, "err", err)
		}
		if _, uerr := s.d.Pool.Exec(ctx, `UPDATE channel_scheduled SET status = 'failed', error = $2, updated_at = now() WHERE id = $1`,
			id, truncate(reason, 300)); uerr != nil {
			return nil, uerr
		}
		if x.status == "scheduled" {
			s.notifyScheduledFailed(ctx, x.user, x.channel, reason)
		}
		return nil, err
	}
	var orgID uuid.UUID
	if err := s.d.Pool.QueryRow(ctx, `DELETE FROM channel_scheduled x USING channels c WHERE c.id = x.channel_id AND x.id = $1 RETURNING c.org_id`, id).
		Scan(&orgID); err != nil && !db.IsNoRows(err) {
		return nil, err
	}
	s.toUser(ctx, x.user, EventSidebar, SidebarEvent{OrgID: orgID, ChannelID: x.channel, Reason: "scheduled"})
	return msg, nil
}

func (s *Service) notifyScheduledFailed(ctx context.Context, userID, channelID uuid.UUID, reason string) {
	refs, err := s.channelRefs(ctx, userID, []uuid.UUID{channelID})
	if err != nil {
		return
	}
	c := refs[channelID]
	where := c.Name
	if c.Kind == kindPublic || c.Kind == kindPrivate {
		where = "#" + where
	}
	if err := s.d.Notify.Send(ctx, notify.Notification{
		UserID: userID, Kind: KindScheduled, Title: "Scheduled message not sent", Body: where + ": " + reason, Link: "/channels/drafts?tab=scheduled",
		Data: map[string]any{"org_id": c.OrgID.String(), "channel_id": channelID.String()},
	}); err != nil {
		slog.WarnContext(ctx, "channels: notify scheduled failure", "err", err)
	}
}

// sendDueScheduled is the job: sends every scheduled message that is due.
func (s *Service) sendDueScheduled(ctx context.Context) error {
	rows, err := s.d.Pool.Query(ctx, `
		SELECT id FROM channel_scheduled WHERE status = 'scheduled' AND send_at <= now() ORDER BY send_at, id LIMIT 200`)
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return err
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_, _ = s.sendScheduled(ctx, id) // failures are recorded on the row
	}
	return nil
}
