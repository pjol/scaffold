package channels

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/moderation"
	"scaffoldapp/backend/internal/orgs"
)

// Tools exposes channels to agents, always as the caller and through the same
// access checks as the REST routes (loadAccess, the org membership, blocks).
func (s *Service) Tools() []app.Tool {
	return []app.Tool{
		{
			Name:        "channels_list_mine",
			Description: "List the channels and direct messages the caller is in, per " + orgs.Noun() + ", with unread and mention counts.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{
				"org_id":{"type":"string","format":"uuid","description":"Only this ` + orgs.Noun() + `."}
			},"additionalProperties":false}`),
			ReadOnly: true,
			Handler:  s.toolListMine,
		},
		{
			Name:        "channels_read_history",
			Description: "Read the latest messages of a channel or direct message the caller can see, oldest first. Give channel_id, or org_id with a channel name.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{
				"channel_id":{"type":"string","format":"uuid"},
				"org_id":{"type":"string","format":"uuid","description":"With channel_name."},
				"channel_name":{"type":"string","description":"A channel's name without #, with org_id."},
				"before":{"type":"string","format":"uuid","description":"Messages older than this message id."},
				"limit":{"type":"integer","minimum":1,"maximum":100,"description":"How many messages (default 30)."}
			},"additionalProperties":false}`),
			ReadOnly: true,
			Handler:  s.toolReadHistory,
		},
		{
			Name:        "channels_read_thread",
			Description: "Read a thread: its first message and replies, oldest first.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{
				"message_id":{"type":"string","format":"uuid","description":"The thread's first message, or any reply in it."},
				"limit":{"type":"integer","minimum":1,"maximum":100,"description":"How many replies (default 50)."}
			},"required":["message_id"],"additionalProperties":false}`),
			ReadOnly: true,
			Handler:  s.toolReadThread,
		},
		{
			Name:        "channels_search_messages",
			Description: "Search messages the caller can read, newest first. The query may include in:#channel, from:@name, from:me, has:file and is:thread.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{
				"query":{"type":"string","minLength":2},
				"org_id":{"type":"string","format":"uuid","description":"Only this ` + orgs.Noun() + `."},
				"channel_id":{"type":"string","format":"uuid","description":"Only this channel."},
				"limit":{"type":"integer","minimum":1,"maximum":50,"description":"How many messages (default 20)."}
			},"required":["query"],"additionalProperties":false}`),
			ReadOnly: true,
			Handler:  s.toolSearch,
		},
		{
			Name: "channels_post_message",
			Description: "Post a message as the caller in a channel or direct message they belong to, or reply in a thread. Markdown works; " +
				"mention a person with <@user_id>. Everyone in the channel sees it.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{
				"channel_id":{"type":"string","format":"uuid"},
				"text":{"type":"string","minLength":1,"maxLength":8000},
				"thread_root_id":{"type":"string","format":"uuid","description":"Reply in this thread."},
				"also_send_to_channel":{"type":"boolean","description":"With thread_root_id: also show the reply in the channel."},
				"title":{"type":"string","maxLength":200,"description":"Forum channels: the new post's title."}
			},"required":["channel_id","text"],"additionalProperties":false}`),
			ReadOnly: false,
			Handler:  s.toolPost,
			Target:   postTarget,
			Preview:  s.postPreview,
		},
	}
}

// toolMessage is a message as agents read it: markdown with mentions as names.
type toolMessage struct {
	ID           uuid.UUID  `json:"id"`
	From         string     `json:"from"`
	Title        string     `json:"title,omitempty"`
	Text         string     `json:"text"`
	Attachments  int        `json:"attachments,omitempty"`
	Replies      int        `json:"replies,omitempty"`
	ThreadRootID *uuid.UUID `json:"thread_root_id,omitempty"`
	Deleted      bool       `json:"deleted,omitempty"`
	SentAt       time.Time  `json:"sent_at"`
}

func toToolMessage(m *Message) toolMessage {
	from := deref(m.BotName)
	if m.Sender != nil {
		from = m.Sender.DisplayName
	}
	text := plainMentions(m.Body, m.Mentions)
	if m.Event != nil {
		text = "[" + *m.Event + "]"
	}
	return toolMessage{ID: m.ID, From: from, Title: deref(m.Title), Text: text, Attachments: len(m.Attachments), Replies: m.ReplyCount,
		ThreadRootID: m.ThreadRootID, Deleted: m.Deleted, SentAt: m.CreatedAt}
}

// plainMentions keeps markdown but writes mention tokens as @names.
func plainMentions(body string, mentioned []UserRef) string {
	names := map[string]string{}
	for _, u := range mentioned {
		names[u.ID.String()] = "@" + u.DisplayName
	}
	body = userTokenRe.ReplaceAllStringFunc(body, func(tok string) string {
		if n, ok := names[userTokenRe.FindStringSubmatch(tok)[1]]; ok {
			return n
		}
		return "@someone"
	})
	body = specialTokenRe.ReplaceAllString(body, "@$1")
	return groupTokenRe.ReplaceAllString(body, "@$2")
}

func decodeTool[T any](input json.RawMessage) (T, error) {
	var in T
	if len(input) > 0 {
		if err := json.Unmarshal(input, &in); err != nil {
			return in, httpx.BadRequest("Invalid input: " + err.Error())
		}
	}
	return in, nil
}

func clampLimit(n, def, maximum int) int {
	if n <= 0 {
		return def
	}
	return min(n, maximum)
}

func (s *Service) toolListMine(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
	in, err := decodeTool[struct {
		OrgID *uuid.UUID `json:"org_id"`
	}](input)
	if err != nil {
		return nil, err
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT o.id, o.name, m.role FROM org_members m JOIN orgs o ON o.id = m.org_id
		WHERE m.user_id = $1 AND ($2::uuid IS NULL OR o.id = $2) ORDER BY m.created_at, o.id`, caller.UserID, in.OrgID)
	if err != nil {
		return nil, err
	}
	type orgRow struct {
		ID         uuid.UUID
		Name, Role string
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[orgRow])
	if err != nil {
		return nil, err
	}
	if in.OrgID != nil && len(list) == 0 {
		return nil, httpx.NotFound(orgs.NounTitle() + " not found.")
	}
	type item struct {
		ID       uuid.UUID `json:"id"`
		Name     string    `json:"name"`
		Kind     string    `json:"kind"`
		Layout   string    `json:"layout"`
		Topic    string    `json:"topic,omitempty"`
		Unread   int       `json:"unread"`
		Mentions int       `json:"mentions"`
		Muted    bool      `json:"muted,omitempty"`
		Starred  bool      `json:"starred,omitempty"`
	}
	type org struct {
		ID       uuid.UUID `json:"id"`
		Name     string    `json:"name"`
		Role     string    `json:"role"`
		Channels []item    `json:"channels"`
	}
	reader, err := s.agentReader(ctx, caller)
	if err != nil {
		return nil, err
	}
	out := []org{}
	for _, o := range list {
		if err := s.ensureDefaults(ctx, o.ID, caller.UserID); err != nil {
			return nil, err
		}
		chans, err := s.sidebar(ctx, caller.UserID, o.ID, nil)
		if err != nil {
			return nil, err
		}
		items := make([]item, 0, len(chans))
		for _, c := range chans {
			if ok, err := reader.can(c.ID); err != nil || !ok {
				if err != nil {
					return nil, err
				}
				continue
			}
			name := c.Name
			if c.Kind == kindPublic || c.Kind == kindPrivate {
				name = "#" + name
			}
			items = append(items, item{ID: c.ID, Name: name, Kind: c.Kind, Layout: c.Layout, Topic: c.Topic, Unread: c.Unread, Mentions: c.Mentions, Muted: c.Muted, Starred: c.Starred})
		}
		out = append(out, org{ID: o.ID, Name: o.Name, Role: o.Role, Channels: items})
	}
	return map[string]any{"organizations": out}, nil
}

func (s *Service) toolReadHistory(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
	in, err := decodeTool[struct {
		ChannelID   *uuid.UUID `json:"channel_id"`
		OrgID       *uuid.UUID `json:"org_id"`
		ChannelName string     `json:"channel_name"`
		Before      *uuid.UUID `json:"before"`
		Limit       int        `json:"limit"`
	}](input)
	if err != nil {
		return nil, err
	}
	channelID := uuid.Nil
	switch {
	case in.ChannelID != nil:
		channelID = *in.ChannelID
	case in.OrgID != nil && in.ChannelName != "":
		if role, err := orgs.RoleOf(ctx, s.d.Pool, *in.OrgID, caller.UserID); err != nil || role == "" {
			if err == nil {
				err = errChannelNotFound
			}
			return nil, err
		}
		if channelID, err = s.channelByName(ctx, caller.UserID, *in.OrgID, in.ChannelName); err != nil {
			return nil, err
		}
	default:
		return nil, httpx.BadRequest("Give channel_id, or org_id with channel_name.")
	}
	a, err := s.loadAccess(ctx, s.d.Pool, channelID, caller.UserID)
	if err != nil {
		return nil, err
	}
	if err := s.agentRequire(ctx, caller, channelID); err != nil {
		return nil, err
	}
	blocked, err := moderation.BlockedIDs(ctx, s.d.Pool, caller.UserID)
	if err != nil {
		return nil, err
	}
	page, err := s.pageMessages(ctx, s.d.Pool, window{channelID: channelID, blocked: blocked, before: in.Before, limit: clampLimit(in.Limit, 30, 100)}, caller.UserID)
	if err != nil {
		return nil, err
	}
	refs, err := s.channelRefs(ctx, caller.UserID, []uuid.UUID{channelID})
	if err != nil {
		return nil, err
	}
	msgs := make([]toolMessage, 0, len(page.Items))
	for _, m := range page.Items {
		msgs = append(msgs, toToolMessage(m))
	}
	return map[string]any{"channel": refs[channelID], "member": a.Member, "messages": msgs, "has_older": page.HasOlder}, nil
}

func (s *Service) toolReadThread(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
	in, err := decodeTool[struct {
		MessageID uuid.UUID `json:"message_id"`
		Limit     int       `json:"limit"`
	}](input)
	if err != nil {
		return nil, err
	}
	m, err := s.loadMessage(ctx, s.d.Pool, in.MessageID, caller.UserID)
	if err != nil {
		return nil, err
	}
	if err := s.agentRequire(ctx, caller, m.ChannelID); err != nil {
		return nil, errMessageNotFound
	}
	rootID := in.MessageID
	if m.ThreadRootID != nil {
		rootID = *m.ThreadRootID
	}
	blocked, err := moderation.BlockedIDs(ctx, s.d.Pool, caller.UserID)
	if err != nil {
		return nil, err
	}
	root, err := s.message(ctx, s.d.Pool, rootID, caller.UserID)
	if err != nil {
		return nil, err
	}
	if root.senderID != nil && uuidSet(blocked)[*root.senderID] {
		return nil, errMessageNotFound
	}
	page, err := s.pageMessages(ctx, s.d.Pool, window{channelID: m.ChannelID, rootID: &rootID, blocked: blocked, limit: clampLimit(in.Limit, 50, 100)}, caller.UserID)
	if err != nil {
		return nil, err
	}
	refs, err := s.channelRefs(ctx, caller.UserID, []uuid.UUID{m.ChannelID})
	if err != nil {
		return nil, err
	}
	replies := make([]toolMessage, 0, len(page.Items))
	for _, r := range page.Items {
		replies = append(replies, toToolMessage(r))
	}
	return map[string]any{"channel": refs[m.ChannelID], "root": toToolMessage(root), "replies": replies, "has_older": page.HasOlder}, nil
}

func (s *Service) toolSearch(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
	in, err := decodeTool[struct {
		Query     string     `json:"query"`
		OrgID     *uuid.UUID `json:"org_id"`
		ChannelID *uuid.UUID `json:"channel_id"`
		Limit     int        `json:"limit"`
	}](input)
	if err != nil {
		return nil, err
	}
	if in.OrgID != nil {
		if role, err := orgs.RoleOf(ctx, s.d.Pool, *in.OrgID, caller.UserID); err != nil || role == "" {
			if err == nil {
				err = httpx.NotFound(orgs.NounTitle() + " not found.")
			}
			return nil, err
		}
	}
	hits, err := s.searchMessages(ctx, caller.UserID, in.OrgID, in.ChannelID, in.Query, cursor{}, clampLimit(in.Limit, 20, 50))
	if err != nil {
		return nil, err
	}
	type item struct {
		Channel BriefChan   `json:"channel"`
		Message toolMessage `json:"message"`
	}
	if hits, err = s.agentFilterHits(ctx, caller, hits); err != nil {
		return nil, err
	}
	out := make([]item, 0, len(hits))
	for _, h := range hits {
		out = append(out, item{Channel: h.Channel, Message: toToolMessage(h.Message)})
	}
	return map[string]any{"messages": out}, nil
}

func (s *Service) toolPost(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
	in, err := decodeTool[postInput](input)
	if err != nil {
		return nil, err
	}
	if caller.Status != "active" {
		return nil, httpx.ErrSuspended
	}
	if err := s.agentRequire(ctx, caller, in.ChannelID); err != nil {
		return nil, err
	}
	// The same per-person rate as POST /channels/channels/{id}/messages.
	var recent int
	if err := s.d.Pool.QueryRow(ctx, `SELECT count(*) FROM channel_messages WHERE sender_id = $1 AND created_at > now() - interval '1 minute'`,
		caller.UserID).Scan(&recent); err != nil {
		return nil, err
	}
	if recent >= s.cfg.perMinute {
		return nil, httpx.NewError(http.StatusTooManyRequests, "rate_limited", "Too many messages. Try again in a minute.")
	}
	msg, _, err := s.post(ctx, caller.UserID, in.ChannelID, sendReq{Body: in.Text, ThreadRootID: in.ThreadRootID, AlsoSendToChannel: in.AlsoSendToChannel, Title: in.Title})
	if err != nil {
		return nil, err
	}
	return map[string]any{"message": toToolMessage(msg), "link": "/channels/" + msg.ChannelID.String() + "?message=" + msg.ID.String()}, nil
}

type postInput struct {
	ChannelID         uuid.UUID  `json:"channel_id"`
	Text              string     `json:"text"`
	ThreadRootID      *uuid.UUID `json:"thread_root_id"`
	AlsoSendToChannel bool       `json:"also_send_to_channel"`
	Title             string     `json:"title"`
}

// postTarget names the channel a post acts on, so a per-channel agent scope applies.
func postTarget(input json.RawMessage) (kind, id string) {
	var in postInput
	if json.Unmarshal(input, &in) != nil || in.ChannelID == uuid.Nil {
		return agentKind, ""
	}
	return agentKind, in.ChannelID.String()
}

// postPreview is what the user reviews when their agent scope is "suggest":
// "Post in #design" / "Reply in a thread in #design" / "Message Jordan Lee",
// with the text.
func (s *Service) postPreview(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
	in, err := decodeTool[postInput](input)
	if err != nil {
		return app.ToolPreview{}, err
	}
	a, err := s.loadAccess(ctx, s.d.Pool, in.ChannelID, caller.UserID)
	if err != nil {
		return app.ToolPreview{}, err
	}
	refs, err := s.channelRefs(ctx, caller.UserID, []uuid.UUID{in.ChannelID})
	if err != nil {
		return app.ToolPreview{}, err
	}
	where := "#" + refs[in.ChannelID].Name
	summary := "Post in " + where
	switch {
	case a.direct() && in.ThreadRootID != nil:
		summary = "Reply in a thread with " + refs[in.ChannelID].Name
	case a.direct():
		summary = "Message " + refs[in.ChannelID].Name
	case in.ThreadRootID != nil && in.AlsoSendToChannel:
		summary = "Reply in a thread and post in " + where
	case in.ThreadRootID != nil:
		summary = "Reply in a thread in " + where
	case in.Title != "":
		summary = "Post “" + truncate(in.Title, 60) + "” in " + where
	}
	link := "/channels/" + in.ChannelID.String()
	if in.ThreadRootID != nil {
		link += "?thread=" + in.ThreadRootID.String()
	}
	return app.ToolPreview{Summary: summary, After: in.Text, Link: link}, nil
}

// agentRequire refuses (not found) a channel the caller's agent may not read.
func (s *Service) agentRequire(ctx context.Context, caller *auth.Identity, channelID uuid.UUID) error {
	r, err := s.agentReader(ctx, caller)
	if err != nil {
		return err
	}
	return r.require(channelID)
}

// agentFilterHits drops search hits in channels the caller's agent may not read.
func (s *Service) agentFilterHits(ctx context.Context, caller *auth.Identity, hits []SearchHit) ([]SearchHit, error) {
	r, err := s.agentReader(ctx, caller)
	if err != nil {
		return nil, err
	}
	out := hits[:0]
	for _, h := range hits {
		ok, err := r.can(h.Channel.ID)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, h)
		}
	}
	return out, nil
}
