package chat

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
)

// Tools exposes read-only chat tools to agents, always as the caller.
func (s *Service) Tools() []app.Tool {
	return []app.Tool{
		{
			Name:        "chat_list_conversations",
			Description: "List the caller's chat conversations, most recent activity first, with unread counts and the latest message.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{
				"limit":{"type":"integer","minimum":1,"maximum":50,"description":"How many conversations (default 20)."},
				"archived":{"type":"boolean","description":"List archived conversations instead."}
			},"additionalProperties":false}`),
			ReadOnly: true,
			Handler:  s.toolListConversations,
		},
		{
			Name:        "chat_search_messages",
			Description: "Search the text of messages in the caller's own conversations, newest first.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{
				"query":{"type":"string","minLength":2,"description":"Text to find."},
				"conversation_id":{"type":"string","format":"uuid","description":"Only this conversation."},
				"limit":{"type":"integer","minimum":1,"maximum":50,"description":"How many messages (default 20)."}
			},"required":["query"],"additionalProperties":false}`),
			ReadOnly: true,
			Handler:  s.toolSearchMessages,
		},
	}
}

type toolMessage struct {
	ID          uuid.UUID `json:"id"`
	From        string    `json:"from"`
	Text        string    `json:"text"`
	Attachments int       `json:"attachments"`
	Deleted     bool      `json:"deleted,omitempty"`
	SentAt      time.Time `json:"sent_at"`
}

func toToolMessage(m *Message) *toolMessage {
	if m == nil {
		return nil
	}
	text := m.Body
	if m.Event != nil {
		text = "[" + *m.Event + "]"
	}
	return &toolMessage{ID: m.ID, From: m.Sender.DisplayName, Text: text, Attachments: len(m.Attachments), Deleted: m.Deleted, SentAt: m.CreatedAt}
}

func toolLimit(n int) int {
	if n <= 0 {
		return 20
	}
	return min(n, 50)
}

func (s *Service) toolListConversations(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
	var in struct {
		Limit    int  `json:"limit"`
		Archived bool `json:"archived"`
	}
	if len(input) > 0 {
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, httpx.BadRequest("Invalid input: " + err.Error())
		}
	}
	convs, err := s.inbox(ctx, caller.UserID, in.Archived, cursor{}, toolLimit(in.Limit))
	if err != nil {
		return nil, err
	}
	type item struct {
		ID            uuid.UUID    `json:"id"`
		Kind          string       `json:"kind"`
		Title         string       `json:"title"`
		Members       int          `json:"members"`
		Unread        int          `json:"unread"`
		Muted         bool         `json:"muted"`
		LastMessage   *toolMessage `json:"last_message"`
		LastMessageAt time.Time    `json:"last_message_at"`
	}
	out := make([]item, 0, len(convs))
	for _, c := range convs {
		out = append(out, item{ID: c.ID, Kind: c.Kind, Title: c.Title, Members: c.MemberCount, Unread: c.Unread,
			Muted: c.Muted, LastMessage: toToolMessage(c.LastMessage), LastMessageAt: c.LastMessageAt})
	}
	return map[string]any{"conversations": out}, nil
}

func (s *Service) toolSearchMessages(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
	var in struct {
		Query          string     `json:"query"`
		ConversationID *uuid.UUID `json:"conversation_id"`
		Limit          int        `json:"limit"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, httpx.BadRequest("Invalid input: " + err.Error())
	}
	hits, err := s.searchMessages(ctx, caller.UserID, in.Query, in.ConversationID, cursor{}, toolLimit(in.Limit))
	if err != nil {
		return nil, err
	}
	type item struct {
		Conversation ConversationRef `json:"conversation"`
		Message      *toolMessage    `json:"message"`
	}
	out := make([]item, 0, len(hits))
	for _, h := range hits {
		out = append(out, item{Conversation: h.Conversation, Message: toToolMessage(h.Message)})
	}
	return map[string]any{"messages": out}, nil
}
