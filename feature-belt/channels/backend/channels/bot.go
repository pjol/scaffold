package channels

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
)

// BotMessage is a message the app posts into a channel on behalf of a feature
// (a calendar reminder, a deploy notice, a CRM update).
type BotMessage struct {
	// BotName is shown as the author ("Calendar"). 1–80 characters.
	BotName string
	// Text is markdown and may carry mention tokens: <@user-uuid>, <@here>,
	// <@channel>. Mentioned members are notified like any mention.
	Text string
	// ThreadRootID replies in that thread instead of the timeline.
	ThreadRootID *uuid.UUID
}

// ErrUnavailable means the channels module isn't constructed (it is always
// constructed once installed; this guards calls during tests and tools).
var ErrUnavailable = errors.New("channels: module not running")

// PostBotMessage posts msg into channelID as the app and announces it like
// any message: live delivery, unread and mention counts, notifications. It
// refuses archived channels. There is no caller, so no permission check:
// call it only for content your feature generated, never to relay one
// user's text into a channel they can't post in.
func PostBotMessage(ctx context.Context, channelID uuid.UUID, msg BotMessage) (uuid.UUID, error) {
	s := svc.Load()
	if s == nil {
		return uuid.Nil, ErrUnavailable
	}
	name := strings.Join(strings.Fields(msg.BotName), " ")
	text := strings.TrimSpace(msg.Text)
	switch {
	case name == "" || utf8.RuneCountInString(name) > 80:
		return uuid.Nil, httpx.BadRequest("BotName must be 1 to 80 characters.")
	case text == "" || utf8.RuneCountInString(text) > s.cfg.maxChars:
		return uuid.Nil, httpx.BadRequest("Text must be 1 to CHANNELS_MESSAGE_MAX_CHARS characters.")
	}
	a := &access{ChannelID: channelID}
	var archived bool
	err := s.d.Pool.QueryRow(ctx, `SELECT org_id, kind, layout, COALESCE(name, ''), archived_at IS NOT NULL FROM channels WHERE id = $1`, channelID).
		Scan(&a.OrgID, &a.Kind, &a.Layout, &a.Name, &archived)
	if db.IsNoRows(err) {
		return uuid.Nil, errChannelNotFound
	}
	if err != nil {
		return uuid.Nil, err
	}
	if archived {
		return uuid.Nil, errArchived
	}
	ments, err := s.resolveMentions(ctx, s.d.Pool, a, nil, text)
	if err != nil {
		return uuid.Nil, err
	}
	var id uuid.UUID
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if msg.ThreadRootID != nil {
			var ok bool
			if err := tx.QueryRow(ctx, `
				SELECT EXISTS (SELECT 1 FROM channel_messages WHERE id = $1 AND channel_id = $2 AND thread_root_id IS NULL AND kind <> 'system' FOR UPDATE)`,
				*msg.ThreadRootID, channelID).Scan(&ok); err != nil {
				return err
			}
			if !ok {
				return errThreadScope
			}
		}
		var at time.Time
		if err := tx.QueryRow(ctx, `
			INSERT INTO channel_messages (channel_id, kind, bot_name, body, thread_root_id) VALUES ($1, 'bot', $2, $3, $4)
			RETURNING id, created_at`, channelID, name, text, msg.ThreadRootID).Scan(&id, &at); err != nil {
			return err
		}
		if err := saveMentions(ctx, tx, id, ments); err != nil {
			return err
		}
		return afterInsert(ctx, tx, channelID, id, at, nil, msg.ThreadRootID, false, nil, ments)
	})
	if err != nil {
		return uuid.Nil, err
	}
	if m, err := s.message(ctx, s.d.Pool, id, uuid.Nil); err == nil {
		s.announceNew(ctx, a, m, ments)
	}
	return id, nil
}

// DefaultChannel returns the org's first default channel (#general unless
// CHANNELS_DEFAULT_CHANNELS says otherwise), creating the org's default set
// if nobody has opened its workspace yet. Use it as the target for
// org-wide announcements.
func DefaultChannel(ctx context.Context, orgID uuid.UUID) (uuid.UUID, error) {
	s := svc.Load()
	if s == nil {
		return uuid.Nil, ErrUnavailable
	}
	var id uuid.UUID
	find := func() error {
		return s.d.Pool.QueryRow(ctx, `
			SELECT id FROM channels WHERE org_id = $1 AND is_default AND archived_at IS NULL
			ORDER BY array_position($2::text[], name) NULLS LAST, created_at LIMIT 1`, orgID, s.cfg.defaultChannels).Scan(&id)
	}
	err := find()
	if db.IsNoRows(err) {
		var owner uuid.UUID
		if err := s.d.Pool.QueryRow(ctx, `SELECT user_id FROM org_members WHERE org_id = $1 ORDER BY created_at LIMIT 1`, orgID).Scan(&owner); err != nil {
			if db.IsNoRows(err) {
				return uuid.Nil, httpx.NotFound(orgs.NounTitle() + " not found.")
			}
			return uuid.Nil, err
		}
		if err := s.ensureDefaults(ctx, orgID, owner); err != nil {
			return uuid.Nil, err
		}
		err = find()
	}
	if db.IsNoRows(err) {
		return uuid.Nil, errChannelNotFound
	}
	return id, err
}
