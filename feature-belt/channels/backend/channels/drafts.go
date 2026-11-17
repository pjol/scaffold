package channels

// Drafts follow the person between devices: one per conversation and thread,
// saved as they type (clients debounce), deleted when sent or emptied. The
// Drafts & sent view lists drafts, scheduled messages and what they sent.

import (
	"context"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
)

// Draft is unsent text in one conversation or thread.
type Draft struct {
	ID           uuid.UUID  `json:"id"`
	Channel      BriefChan  `json:"channel"`
	ThreadRootID *uuid.UUID `json:"thread_root_id"`
	Body         string     `json:"body"`     // markdown with mention tokens
	Mentions     []UserRef  `json:"mentions"` // people the body's <@id> tokens name
	UpdatedAt    time.Time  `json:"updated_at"`
}

type draftReq struct {
	Body         string     `json:"body"`
	ThreadRootID *uuid.UUID `json:"thread_root_id"`
}

func threadParam(r *http.Request) (*uuid.UUID, error) {
	v := r.URL.Query().Get("thread_root_id")
	if v == "" {
		return nil, nil
	}
	id, err := uuid.Parse(v)
	if err != nil {
		return nil, httpx.BadRequest("Invalid thread_root_id.")
	}
	return &id, nil
}

// draftAccess checks the caller may read the channel and, for a thread, that
// the root is in it.
func (s *Service) draftAccess(ctx context.Context, me, channelID uuid.UUID, rootID *uuid.UUID) (*access, error) {
	a, err := s.loadAccess(ctx, s.d.Pool, channelID, me)
	if err != nil {
		return nil, err
	}
	if rootID != nil {
		var ok bool
		if err := s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM channel_messages WHERE id = $1 AND channel_id = $2 AND thread_root_id IS NULL)`,
			*rootID, channelID).Scan(&ok); err != nil {
			return nil, err
		}
		if !ok {
			return nil, errThreadScope
		}
	}
	return a, nil
}

// PUT /channels/channels/{id}/draft {body, thread_root_id?} — an empty body
// deletes the draft. 204.
func (s *Service) saveDraft(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req draftReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	body := strings.TrimRight(req.Body, " \t\n")
	if utf8.RuneCountInString(body) > s.cfg.maxChars {
		return httpx.BadRequest("Drafts can be as long as a message, no longer.")
	}
	a, err := s.draftAccess(ctx, me, id, req.ThreadRootID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(body) == "" {
		_, err = s.d.Pool.Exec(ctx, `
			DELETE FROM channel_drafts WHERE user_id = $1 AND channel_id = $2 AND thread_root_id IS NOT DISTINCT FROM $3`, me, id, req.ThreadRootID)
	} else {
		_, err = s.d.Pool.Exec(ctx, `
			INSERT INTO channel_drafts (user_id, channel_id, thread_root_id, body) VALUES ($1, $2, $3, $4)
			ON CONFLICT (user_id, channel_id, COALESCE(thread_root_id, '00000000-0000-0000-0000-000000000000'::uuid))
			DO UPDATE SET body = EXCLUDED.body, updated_at = now()`, me, id, req.ThreadRootID, body)
	}
	if err != nil {
		return err
	}
	s.toUser(ctx, me, EventSidebar, SidebarEvent{OrgID: a.OrgID, ChannelID: id, Reason: "draft"})
	httpx.NoContent(w)
	return nil
}

// GET /channels/channels/{id}/draft?thread_root_id= — 204 when there is none.
func (s *Service) getDraft(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	root, err := threadParam(r)
	if err != nil {
		return err
	}
	if _, err := s.draftAccess(ctx, me, id, root); err != nil {
		return err
	}
	list, err := s.drafts(ctx, me, nil, &id, root, cursor{}, 1)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		httpx.NoContent(w)
		return nil
	}
	httpx.OK(w, list[0])
	return nil
}

// DELETE /channels/channels/{id}/draft?thread_root_id=
func (s *Service) deleteDraft(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	root, err := threadParam(r)
	if err != nil {
		return err
	}
	var orgID uuid.UUID
	err = s.d.Pool.QueryRow(ctx, `
		DELETE FROM channel_drafts d USING channels c WHERE c.id = d.channel_id AND d.user_id = $1 AND d.channel_id = $2
		  AND d.thread_root_id IS NOT DISTINCT FROM $3 RETURNING c.org_id`, me, id, root).Scan(&orgID)
	if err != nil && !db.IsNoRows(err) {
		return err
	}
	if err == nil {
		s.toUser(ctx, me, EventSidebar, SidebarEvent{OrgID: orgID, ChannelID: id, Reason: "draft"})
	}
	httpx.NoContent(w)
	return nil
}

// drafts lists the caller's drafts in conversations they can still read,
// newest first (orgID and/or channelID narrow it).
func (s *Service) drafts(ctx context.Context, me uuid.UUID, orgID, channelID, rootID *uuid.UUID, c cursor, limit int) ([]Draft, error) {
	rows, err := s.d.Pool.Query(ctx, `
		SELECT d.id, d.channel_id, d.thread_root_id, d.body, d.updated_at
		FROM channel_drafts d JOIN channels c ON c.id = d.channel_id
		WHERE d.user_id = $1 AND `+readableSQL+`
		  AND ($2::uuid IS NULL OR c.org_id = $2)
		  AND ($3::uuid IS NULL OR (d.channel_id = $3 AND d.thread_root_id IS NOT DISTINCT FROM $4))
		  AND ($5::timestamptz IS NULL OR (d.updated_at, d.id) < ($5, $6))
		ORDER BY d.updated_at DESC, d.id DESC LIMIT $7`, me, orgID, channelID, rootID, nullTime(c.T), c.ID, limit)
	if err != nil {
		return nil, err
	}
	type row struct {
		id, channel uuid.UUID
		root        *uuid.UUID
		body        string
		at          time.Time
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
		var x row
		return x, r.Scan(&x.id, &x.channel, &x.root, &x.body, &x.at)
	})
	if err != nil {
		return nil, err
	}
	chans, mentioned := []uuid.UUID{}, []uuid.UUID{}
	for _, x := range list {
		chans = append(chans, x.channel)
		mentioned = append(mentioned, mentionedUserIDs(x.body)...)
	}
	refs, err := s.channelRefs(ctx, me, chans)
	if err != nil {
		return nil, err
	}
	users, err := loadUsers(ctx, s.d.Pool, dedupe(mentioned))
	if err != nil {
		return nil, err
	}
	out := make([]Draft, 0, len(list))
	for _, x := range list {
		d := Draft{ID: x.id, Channel: refs[x.channel], ThreadRootID: x.root, Body: x.body, Mentions: []UserRef{}, UpdatedAt: x.at}
		for _, id := range mentionedUserIDs(x.body) {
			if u, ok := users[id]; ok {
				d.Mentions = append(d.Mentions, u)
			}
		}
		out = append(out, d)
	}
	return out, nil
}

// GET /channels/orgs/{orgID}/drafts?cursor=
func (s *Service) listDrafts(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	m, _ := orgs.FromContext(ctx)
	p := httpx.ParsePage(r)
	var c cursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	list, err := s.drafts(ctx, me, &m.OrgID, nil, nil, c, p.Limit+1)
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(list, p.Limit, func(d Draft) any { return cursor{d.UpdatedAt, d.ID} }))
	return nil
}

// clearDraft drops the draft a send used (best-effort) and tells the
// person's other devices.
func (s *Service) clearDraft(ctx context.Context, me, orgID, channelID uuid.UUID, rootID *uuid.UUID) {
	tag, err := s.d.Pool.Exec(ctx, `DELETE FROM channel_drafts WHERE user_id = $1 AND channel_id = $2 AND thread_root_id IS NOT DISTINCT FROM $3`,
		me, channelID, rootID)
	if err == nil && tag.RowsAffected() > 0 {
		s.toUser(ctx, me, EventSidebar, SidebarEvent{OrgID: orgID, ChannelID: channelID, Reason: "draft"})
	}
}

// SentItem is a message the caller sent, with where it went.
type SentItem struct {
	Message *Message  `json:"message"`
	Channel BriefChan `json:"channel"`
}

// GET /channels/orgs/{orgID}/sent?cursor= — the caller's messages, newest first.
func (s *Service) listSent(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	m, _ := orgs.FromContext(ctx)
	p := httpx.ParsePage(r)
	var c cursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	rows, err := s.d.Pool.Query(ctx, messageSelect+`
		JOIN channels c ON c.id = m.channel_id
		WHERE m.sender_id = $1 AND m.kind = 'text' AND m.deleted_at IS NULL AND c.org_id = $2 AND `+readableSQL+`
		  AND ($3::timestamptz IS NULL OR (m.created_at, m.id) < ($3, $4))
		ORDER BY m.created_at DESC, m.id DESC LIMIT $5`, me, m.OrgID, nullTime(c.T), c.ID, p.Limit+1)
	if err != nil {
		return err
	}
	msgs, err := scanMessages(rows, false)
	if err != nil {
		return err
	}
	if err := s.hydrate(ctx, s.d.Pool, msgs, me, false); err != nil {
		return err
	}
	chans := make([]uuid.UUID, len(msgs))
	for i, x := range msgs {
		chans[i] = x.ChannelID
	}
	refs, err := s.channelRefs(ctx, me, chans)
	if err != nil {
		return err
	}
	items := make([]SentItem, len(msgs))
	for i, x := range msgs {
		items[i] = SentItem{Message: x, Channel: refs[x.ChannelID]}
	}
	httpx.OK(w, httpx.NewPage(items, p.Limit, func(i SentItem) any { return cursor{i.Message.CreatedAt, i.Message.ID} }))
	return nil
}
