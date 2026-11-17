package docs

import (
	"context"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
)

// Reaction is one emoji on a comment: how many people chose it and whether
// the caller did (people blocked either way aren't counted).
type Reaction struct {
	Emoji string `json:"emoji"`
	Count int    `json:"count"`
	Mine  bool   `json:"mine"`
}

const maxReactionsPerComment = 20

// attachReactions fills Reactions on threads and their replies.
func (s *Service) attachReactions(ctx context.Context, me uuid.UUID, blocked []uuid.UUID, threads []Comment) error {
	var ids []uuid.UUID
	for _, t := range threads {
		ids = append(ids, t.ID)
		for _, r := range t.Replies {
			ids = append(ids, r.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT comment_id, emoji, count(*)::int, bool_or(user_id = $2) FROM docs_comment_reactions
		WHERE comment_id = ANY ($1) AND user_id <> ALL ($3)
		GROUP BY comment_id, emoji ORDER BY min(created_at), emoji`, ids, me, blocked)
	if err != nil {
		return err
	}
	defer rows.Close()
	by := map[uuid.UUID][]Reaction{}
	for rows.Next() {
		var id uuid.UUID
		var r Reaction
		if err := rows.Scan(&id, &r.Emoji, &r.Count, &r.Mine); err != nil {
			return err
		}
		by[id] = append(by[id], r)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range threads {
		if r, ok := by[threads[i].ID]; ok {
			threads[i].Reactions = r
		}
		for j := range threads[i].Replies {
			if r, ok := by[threads[i].Replies[j].ID]; ok {
				threads[i].Replies[j].Reactions = r
			}
		}
	}
	return nil
}

func cleanEmoji(raw string) (string, error) {
	e := strings.TrimSpace(raw)
	if e == "" || utf8.RuneCountInString(e) > 8 || len(e) > 32 || strings.ContainsAny(e, "<>\"'\n ") {
		return "", httpx.BadRequest("Choose one emoji.")
	}
	if _, _, ok := startsWithEmoji(e); !ok {
		return "", httpx.BadRequest("Choose one emoji.")
	}
	return e, nil
}

type reactionReq struct {
	Emoji string `json:"emoji"`
}

// react: PUT /docs/comments/{commentID}/reactions {emoji} (comment level):
// add the caller's reaction (idempotent).
func (s *Service) react(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "commentID")
	if err != nil {
		return err
	}
	var req reactionReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	emoji, err := cleanEmoji(req.Emoji)
	if err != nil {
		return err
	}
	c, p, _, err := s.commentFor(ctx, me, id, access.Comment)
	if err != nil {
		return err
	}
	if c.Deleted {
		return httpx.NotFound("Comment not found.")
	}
	var distinct int
	if err := s.d.Pool.QueryRow(ctx, `SELECT count(DISTINCT emoji) FROM docs_comment_reactions WHERE comment_id = $1 AND emoji <> $2`, id, emoji).Scan(&distinct); err != nil {
		return err
	}
	if distinct >= maxReactionsPerComment {
		return httpx.Conflict("This comment has as many different reactions as it can hold.")
	}
	if _, err := s.d.Pool.Exec(ctx, `INSERT INTO docs_comment_reactions (comment_id, user_id, emoji) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, id, me, emoji); err != nil {
		return err
	}
	s.publish(ctx, pageTopic(p.ID), EventComments, map[string]any{"page_id": p.ID, "thread_id": threadOf(c)})
	httpx.NoContent(w)
	return nil
}

// unreact: DELETE /docs/comments/{commentID}/reactions?emoji= (view): remove
// the caller's own reaction.
func (s *Service) unreact(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "commentID")
	if err != nil {
		return err
	}
	emoji, err := cleanEmoji(r.URL.Query().Get("emoji"))
	if err != nil {
		return err
	}
	c, p, _, err := s.commentFor(ctx, me, id, access.View)
	if err != nil {
		return err
	}
	if _, err := s.d.Pool.Exec(ctx, `DELETE FROM docs_comment_reactions WHERE comment_id = $1 AND user_id = $2 AND emoji = $3`, id, me, emoji); err != nil {
		return err
	}
	s.publish(ctx, pageTopic(p.ID), EventComments, map[string]any{"page_id": p.ID, "thread_id": threadOf(c)})
	httpx.NoContent(w)
	return nil
}
