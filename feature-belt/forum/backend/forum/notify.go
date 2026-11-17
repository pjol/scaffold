package forum

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/bg"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/moderation"
	"scaffoldapp/backend/internal/notify"
)

// publish sends a realtime nudge on the post's topic. Payloads carry ids and
// scores, never text: clients refetch through the API, which applies blocks
// and moderation. Best-effort.
func (s *Service) publish(ctx context.Context, postID uuid.UUID, event string, data any) {
	if err := s.d.Broker.Publish(ctx, PostTopic(postID), event, data); err != nil {
		slog.WarnContext(ctx, "forum: realtime publish failed", "event", event, "err", err)
	}
}

// notifyUser sends n unless the recipient is the actor or either blocked the other.
func (s *Service) notifyUser(ctx context.Context, actor uuid.UUID, n notify.Notification) {
	if n.UserID == actor || n.UserID == uuid.Nil {
		return
	}
	blocked, err := moderation.IsBlocked(ctx, s.d.Pool, actor, n.UserID)
	if err != nil {
		slog.WarnContext(ctx, "forum: block check before notify", "kind", n.Kind, "err", err)
		return
	}
	if blocked {
		return
	}
	if err := s.d.Notify.Send(ctx, n); err != nil {
		slog.WarnContext(ctx, "forum: notify failed", "kind", n.Kind, "err", err)
	}
}

func preview(s string) string {
	s = Excerpt(s)
	if r := []rune(s); len(r) > 140 {
		s = string(r[:139]) + "…"
	}
	return s
}

func actorName(name string) string {
	if strings.TrimSpace(name) == "" {
		return "Someone"
	}
	return name
}

// afterVisiblePost tells mentioned people about a post that became visible.
func (s *Service) afterVisiblePost(ctx context.Context, p postRow, mentions []uuid.UUID) {
	if len(mentions) == 0 {
		return
	}
	bg.Go(ctx, "forum.post_mentions", func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()
		data := map[string]any{"post_id": p.ID.String()}
		for _, u := range mentions {
			s.notifyUser(ctx, p.AuthorID, notify.Notification{UserID: u, Kind: NotifyMention,
				Title: actorName(p.AuthorName) + " mentioned you in " + p.Community.Title, Body: p.Title, Link: postPath(p.ID), Data: data})
		}
		return nil
	})
}

// afterVisibleComment announces a comment that became visible and tells the
// parent's author (reply) or the post's author (comment) and mentioned people.
func (s *Service) afterVisibleComment(ctx context.Context, p postRow, c commentRow, parent *commentRow, mentions []uuid.UUID, count int) {
	s.publish(ctx, p.ID, EventComment, map[string]any{"post_id": p.ID, "comment_id": c.ID, "parent_id": c.ParentID, "comment_count": count})
	bg.Go(ctx, "forum.comment_notify", func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()
		link := postPath(p.ID) + "?comment=" + c.ID.String()
		data := map[string]any{"post_id": p.ID.String(), "comment_id": c.ID.String()}
		body := preview(c.Body)
		told := map[uuid.UUID]bool{c.AuthorID: true}
		if parent != nil && !told[parent.AuthorID] {
			told[parent.AuthorID] = true
			s.notifyUser(ctx, c.AuthorID, notify.Notification{UserID: parent.AuthorID, Kind: NotifyReply,
				Title: actorName(c.AuthorName) + " replied to your comment", Body: body, Link: link, Data: data})
		}
		if parent == nil && p.Status != StatusDeleted && !told[p.AuthorID] {
			told[p.AuthorID] = true
			s.notifyUser(ctx, c.AuthorID, notify.Notification{UserID: p.AuthorID, Kind: NotifyComment,
				Title: actorName(c.AuthorName) + " commented on " + p.Title, Body: body, Link: link, Data: data})
		}
		for _, u := range mentions {
			if told[u] {
				continue
			}
			told[u] = true
			s.notifyUser(ctx, c.AuthorID, notify.Notification{UserID: u, Kind: NotifyMention,
				Title: actorName(c.AuthorName) + " mentioned you in a comment", Body: body, Link: link, Data: data})
		}
		return nil
	})
}

// ---- mentions ----

// validMentions keeps the people a post or comment names: active accounts
// other than the author, not blocked either way, whose "@Name" appears in
// the text, at most MentionsMax.
func (s *Service) validMentions(ctx context.Context, v viewer, text string, ids []uuid.UUID) ([]uuid.UUID, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	if len(ids) > MentionsMax {
		return nil, httpx.BadRequest("You can mention up to 10 people at once.")
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT id, display_name FROM users WHERE id = ANY($1) AND status = 'active'`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	lower := strings.ToLower(text)
	out := []uuid.UUID{}
	for rows.Next() {
		var (
			id   uuid.UUID
			name string
		)
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		if id == v.id || v.blocks(id) || name == "" || !strings.Contains(lower, "@"+strings.ToLower(name)) {
			continue
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func saveMentions(ctx context.Context, tx pgx.Tx, kind string, id uuid.UUID, users []uuid.UUID) error {
	if _, err := tx.Exec(ctx, `DELETE FROM forum_mentions WHERE target_kind = $1 AND target_id = $2`, kind, id); err != nil {
		return err
	}
	if len(users) == 0 {
		return nil
	}
	_, err := tx.Exec(ctx, `INSERT INTO forum_mentions (target_kind, target_id, user_id)
		SELECT $1, $2, unnest($3::uuid[]) ON CONFLICT DO NOTHING`, kind, id, users)
	return err
}

func (s *Service) mentionsOf(ctx context.Context, kind string, id uuid.UUID) ([]Author, error) {
	m, err := s.mentionsFor(ctx, kind, []uuid.UUID{id})
	if err != nil {
		return nil, err
	}
	if out := m[id]; out != nil {
		return out, nil
	}
	return []Author{}, nil
}

func (s *Service) mentionsFor(ctx context.Context, kind string, ids []uuid.UUID) (map[uuid.UUID][]Author, error) {
	out := map[uuid.UUID][]Author{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT m.target_id, u.id, u.display_name, u.avatar_url
		FROM forum_mentions m JOIN users u ON u.id = m.user_id
		WHERE m.target_kind = $1 AND m.target_id = ANY($2) AND u.status <> 'deleted'`, kind, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			t uuid.UUID
			a Author
		)
		if err := rows.Scan(&t, &a.ID, &a.DisplayName, &a.AvatarURL); err != nil {
			return nil, err
		}
		out[t] = append(out[t], a)
	}
	return out, rows.Err()
}

func (s *Service) mentionedUsers(ctx context.Context, q db.Querier, kind string, id uuid.UUID) ([]uuid.UUID, error) {
	rows, err := q.Query(ctx, `SELECT user_id FROM forum_mentions WHERE target_kind = $1 AND target_id = $2`, kind, id)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

// peopleHandler: GET /v1/forum/people?q=: mention suggestions, active people
// whose name starts with q, without anyone blocked either way.
func (s *Service) peopleHandler(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	caller := auth.MustFromContext(ctx)
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		httpx.OK(w, map[string]any{"items": []Author{}})
		return nil
	}
	v, err := s.viewerOf(ctx, caller)
	if err != nil {
		return err
	}
	esc := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(strings.ToLower(clipRunes(q, 50)))
	rows, err := s.d.Pool.Query(ctx, `
		SELECT id, display_name, avatar_url FROM users
		WHERE status = 'active' AND display_name <> '' AND id <> $1 AND NOT (id = ANY($2))
		  AND (lower(display_name) LIKE $3 || '%' OR lower(display_name) LIKE '% ' || $3 || '%')
		ORDER BY display_name, id LIMIT 8`, caller.UserID, v.blocked, esc)
	if err != nil {
		return err
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Author])
	if err != nil {
		return err
	}
	if out == nil {
		out = []Author{}
	}
	httpx.OK(w, map[string]any{"items": out})
	return nil
}

// ---- the app's own labels ----

// LabelInput is a new post or comment, for a Labeler.
type LabelInput struct {
	Kind      string // post | comment
	AuthorID  uuid.UUID
	Community Community
	Title     string
	Body      string
	URL       string
}

// Labeler adds the app's own labels to new posts and comments (a disclosure
// that the author holds what they discuss, a verified-seller tag). Labels
// are short snake_case words the clients show as badges (FORUM_LABELS in
// shared names the known ones). An error leaves the item unlabelled.
type Labeler interface {
	Labels(ctx context.Context, in LabelInput) ([]string, error)
}

var (
	labelerMu sync.RWMutex
	labeler   Labeler
)

// SetLabeler installs the app's Labeler (from an init() in internal/api).
func SetLabeler(l Labeler) {
	labelerMu.Lock()
	defer labelerMu.Unlock()
	labeler = l
}

func (s *Service) appLabels(ctx context.Context, in LabelInput) []string {
	labelerMu.RLock()
	l := labeler
	labelerMu.RUnlock()
	if l == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out, err := l.Labels(ctx, in)
	if err != nil {
		slog.WarnContext(ctx, "forum: labeler failed", "err", err)
		return nil
	}
	if len(out) > 5 {
		out = out[:5]
	}
	return out
}
