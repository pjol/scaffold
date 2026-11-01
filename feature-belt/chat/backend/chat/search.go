package chat

import (
	"context"
	"strings"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
)

// Search implements app.SearchProvider: text messages in the caller's own
// conversations, by exactly the rules of GET /chat/search (membership,
// cleared history, deleted messages and blocked senders excluded). For an
// agent call the conversation must also be readable at the caller's agent
// scope. Title is who sent it; Context is "Chat · <conversation>".
func (s *Service) Search(ctx context.Context, caller *auth.Identity, query string, limit int) ([]app.SearchResult, error) {
	hits, err := s.searchMessages(ctx, caller.UserID, query, nil, cursor{}, limit)
	if err != nil {
		return nil, err
	}
	if hits, err = s.agentHits(ctx, caller, hits); err != nil {
		return nil, err
	}
	q := strings.ToLower(strings.TrimSpace(query))
	out := make([]app.SearchResult, 0, len(hits))
	for _, h := range hits {
		conv := h.Conversation.ID
		where := h.Conversation.Title // the group, or the other person
		if where == "" {
			where = "Conversation"
		}
		title := h.Message.Sender.DisplayName
		if title == "" {
			title = where
		}
		score := 0.5
		if strings.HasPrefix(strings.ToLower(h.Message.Body), q) {
			score = 0.7
		}
		out = append(out, app.SearchResult{
			Kind:    "chat.message",
			ID:      h.Message.ID.String(),
			Title:   title,
			Snippet: snippetAround(h.Message.Body, q),
			Link:    "/chat/" + conv.String() + "?message=" + h.Message.ID.String(),
			Icon:    "message-square",
			Score:   score,
			At:      h.Message.CreatedAt,
			Context: app.SearchContext("Chat", where),
		})
	}
	return out, nil
}

// snippetAround returns up to 140 characters of body around the first match
// of q (lower-case), with an ellipsis where it was cut.
func snippetAround(body, q string) string {
	r := []rune(strings.Join(strings.Fields(body), " "))
	if len(r) <= 140 {
		return string(r)
	}
	at := 0
	if qr := []rune(q); len(qr) > 0 {
		low := []rune(strings.ToLower(string(r)))
		for i := 0; i+len(qr) <= len(low) && len(low) == len(r); i++ {
			if string(low[i:i+len(qr)]) == q {
				at = i
				break
			}
		}
	}
	start := max(0, at-40)
	end := min(start+140, len(r))
	out := string(r[start:end])
	if start > 0 {
		out = "…" + out
	}
	if end < len(r) {
		out += "…"
	}
	return out
}
