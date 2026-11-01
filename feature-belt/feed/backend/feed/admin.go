package feed

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/audit"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/moderation"
)

// adminListPosts lists and searches every post, removed ones included.
// Filters: q (text, or a post id), author_id, kind, status (visible|removed).
func (s *Service) adminListPosts(w http.ResponseWriter, r *http.Request) error {
	staff := auth.MustFromContext(r.Context())
	p := httpx.ParsePage(r)
	var c cursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	q := r.URL.Query()
	text := strings.TrimSpace(q.Get("q"))
	var idQuery *uuid.UUID
	if id, err := uuid.Parse(text); err == nil {
		idQuery, text = &id, ""
	}
	var author *uuid.UUID
	if a := q.Get("author_id"); a != "" {
		id, err := uuid.Parse(a)
		if err != nil {
			return httpx.BadRequest("Invalid author_id.")
		}
		author = &id
	}
	kind, status := q.Get("kind"), q.Get("status")
	if kind != "" && kind != KindPost && kind != KindAnnouncement {
		return httpx.BadRequest("Kind must be post or announcement.")
	}
	if status != "" && status != "visible" && status != "removed" {
		return httpx.BadRequest("Status must be visible or removed.")
	}
	rows, err := s.d.Pool.Query(r.Context(), `
		SELECT `+postCols+`
		FROM feed_posts p JOIN users u ON u.id = p.author_id
		WHERE ($1::text = '' OR p.search @@ to_tsquery('simple', $1))
		  AND ($2::uuid IS NULL OR p.id = $2)
		  AND ($3::uuid IS NULL OR p.author_id = $3)
		  AND ($4::text = '' OR p.kind = $4)
		  AND ($5::text = '' OR ($5 = 'removed') = (p.removed_at IS NOT NULL))
		  AND ($6::timestamptz IS NULL OR (p.created_at, p.id) < ($6, $7::uuid))
		ORDER BY p.created_at DESC, p.id DESC LIMIT $8`,
		searchQuery(text), idQuery, author, kind, status, nullTime(c.T), c.ID, p.Limit+1)
	if err != nil {
		return err
	}
	posts, err := scanPosts(rows)
	if err != nil {
		return err
	}
	page := httpx.NewPage(posts, p.Limit, func(p Post) any { return cursor{p.CreatedAt, p.ID} })
	if err := s.hydratePosts(r.Context(), staff.UserID, page.Items); err != nil {
		return err
	}
	httpx.OK(w, page)
	return nil
}

func (s *Service) adminGetPost(w http.ResponseWriter, r *http.Request) error {
	staff := auth.MustFromContext(r.Context())
	pid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	p, err := s.loadPost(r.Context(), pid, staff.UserID, nil)
	if err != nil {
		return err
	}
	httpx.OK(w, p)
	return nil
}

type removeReq struct {
	Reason string `json:"reason"`
}

// adminRemovePost hides a post from everyone. The row stays for the
// moderation record; the author's account deletion still removes it.
func (s *Service) adminRemovePost(w http.ResponseWriter, r *http.Request) error {
	staff := auth.MustFromContext(r.Context())
	pid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req removeReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	var audience string
	err = db.WithTx(r.Context(), s.d.Pool, func(tx pgx.Tx) error {
		aud, ok, err := removePost(r.Context(), tx, pid, &staff.UserID)
		if err != nil {
			return err
		}
		if !ok {
			return httpx.NotFound("That post isn't available.")
		}
		audience = aud
		return audit.Log(r.Context(), tx, staff.UserID, "feed.post.remove", "feed_post", pid.String(),
			map[string]any{"reason": strings.TrimSpace(req.Reason)})
	})
	if err != nil {
		return err
	}
	s.publish(r.Context(), audience, EventPostUpdated, map[string]any{"post_id": pid, "deleted": true})
	httpx.NoContent(w)
	return nil
}

func (s *Service) adminRemoveComment(w http.ResponseWriter, r *http.Request) error {
	staff := auth.MustFromContext(r.Context())
	cid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req removeReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	var postID uuid.UUID
	var audience string
	err = db.WithTx(r.Context(), s.d.Pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(r.Context(), `
			SELECT c.post_id, p.audience FROM feed_comments c JOIN feed_posts p ON p.id = c.post_id
			WHERE c.id = $1`, cid).Scan(&postID, &audience); err != nil {
			if db.IsNoRows(err) {
				return httpx.NotFound("That comment isn't available.")
			}
			return err
		}
		ok, err := removeComment(r.Context(), tx, cid, &staff.UserID)
		if err != nil {
			return err
		}
		if !ok {
			return httpx.NotFound("That comment isn't available.")
		}
		return audit.Log(r.Context(), tx, staff.UserID, "feed.comment.remove", "feed_comment", cid.String(),
			map[string]any{"post_id": postID, "reason": strings.TrimSpace(req.Reason)})
	})
	if err != nil {
		return err
	}
	s.publish(r.Context(), audience, EventComment, map[string]any{
		"post_id": postID, "comment_id": cid, "comment_count": s.commentCount(r.Context(), postID), "deleted": true,
	})
	httpx.NoContent(w)
	return nil
}

type pinReq struct {
	PinnedUntil *time.Time `json:"pinned_until"` // null unpins
}

// adminPinPost changes or ends an announcement's pin.
func (s *Service) adminPinPost(w http.ResponseWriter, r *http.Request) error {
	staff := auth.MustFromContext(r.Context())
	pid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req pinReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if err := validPin(req.PinnedUntil); err != nil {
		return err
	}
	var audience string
	err = db.WithTx(r.Context(), s.d.Pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(r.Context(), `
			UPDATE feed_posts SET pinned_until = $2, updated_at = now()
			WHERE id = $1 AND kind = 'announcement' AND removed_at IS NULL RETURNING audience`,
			pid, req.PinnedUntil).Scan(&audience); err != nil {
			if db.IsNoRows(err) {
				return httpx.NotFound("No announcement with that id.")
			}
			return err
		}
		return audit.Log(r.Context(), tx, staff.UserID, "feed.announcement.pin", "feed_post", pid.String(),
			map[string]any{"pinned_until": req.PinnedUntil})
	})
	if err != nil {
		return err
	}
	s.publish(r.Context(), audience, EventPostUpdated, map[string]any{"post_id": pid, "deleted": false})
	p, err := s.loadPost(r.Context(), pid, staff.UserID, nil)
	if err != nil {
		return err
	}
	httpx.OK(w, p)
	return nil
}

// adminCreateAnnouncement posts an announcement from the console. Same
// rules as POST /feed/posts with kind=announcement (audited either way).
func (s *Service) adminCreateAnnouncement(w http.ResponseWriter, r *http.Request) error {
	var req postReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	req.Kind = KindAnnouncement
	p, err := s.createPost(r.Context(), auth.MustFromContext(r.Context()), req)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, p)
	return nil
}

// removePost hides a post on staff action. ok is false when there is no
// visible post with that id. The pin is kept so a restore brings it back as
// it was (pinned queries skip removed posts).
func removePost(ctx context.Context, q db.Querier, id uuid.UUID, by *uuid.UUID) (string, bool, error) {
	var audience string
	err := q.QueryRow(ctx, `
		UPDATE feed_posts SET removed_at = now(), removed_by = $2, updated_at = now()
		WHERE id = $1 AND removed_at IS NULL RETURNING audience`, id, by).Scan(&audience)
	if db.IsNoRows(err) {
		return "", false, nil
	}
	return audience, err == nil, err
}

// ---- moderation targets ----------------------------------------------------

type postTarget struct{ s *Service }

func (t postTarget) Snapshot(ctx context.Context, id string) (moderation.Snapshot, error) {
	pid, err := uuid.Parse(id)
	if err != nil {
		return moderation.Snapshot{}, httpx.NotFound("That post isn't available.")
	}
	var author uuid.UUID
	var body string
	var link *string
	err = t.s.d.Pool.QueryRow(ctx, `SELECT author_id, body, link_url FROM feed_posts WHERE id = $1`, pid).Scan(&author, &body, &link)
	if db.IsNoRows(err) {
		return moderation.Snapshot{}, httpx.NotFound("That post isn't available.")
	}
	if err != nil {
		return moderation.Snapshot{}, err
	}
	rows, err := t.s.d.Pool.Query(ctx, `SELECT key FROM feed_media WHERE post_id = $1 ORDER BY position`, pid)
	if err != nil {
		return moderation.Snapshot{}, err
	}
	keys, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return moderation.Snapshot{}, err
	}
	if link != nil {
		body = strings.TrimSpace(body + "\n" + *link)
	}
	return moderation.Snapshot{AuthorID: &author, Text: body, MediaKeys: keys, Link: "/feed/posts/" + pid.String()}, nil
}

func (t postTarget) Remove(ctx context.Context, tx pgx.Tx, id string) error {
	pid, err := uuid.Parse(id)
	if err != nil {
		return httpx.NotFound("That post isn't available.")
	}
	_, _, err = removePost(ctx, tx, pid, nil) // already removed or deleted: nothing to do
	return err
}

// Restore undoes a removal (moderation dismisses a case it auto-hid).
func (t postTarget) Restore(ctx context.Context, tx pgx.Tx, id string) error {
	pid, err := uuid.Parse(id)
	if err != nil {
		return httpx.NotFound("That post isn't available.")
	}
	_, err = tx.Exec(ctx, `
		UPDATE feed_posts SET removed_at = NULL, removed_by = NULL, updated_at = now()
		WHERE id = $1 AND removed_at IS NOT NULL`, pid)
	return err
}

type commentTarget struct{ s *Service }

func (t commentTarget) Snapshot(ctx context.Context, id string) (moderation.Snapshot, error) {
	cid, err := uuid.Parse(id)
	if err != nil {
		return moderation.Snapshot{}, httpx.NotFound("That comment isn't available.")
	}
	var author, postID uuid.UUID
	var body string
	err = t.s.d.Pool.QueryRow(ctx, `SELECT author_id, post_id, body FROM feed_comments WHERE id = $1`, cid).Scan(&author, &postID, &body)
	if db.IsNoRows(err) {
		return moderation.Snapshot{}, httpx.NotFound("That comment isn't available.")
	}
	if err != nil {
		return moderation.Snapshot{}, err
	}
	return moderation.Snapshot{
		AuthorID: &author, Text: body, MediaKeys: []string{},
		Link: "/feed/posts/" + postID.String() + "?comment=" + cid.String(),
	}, nil
}

func (t commentTarget) Remove(ctx context.Context, tx pgx.Tx, id string) error {
	cid, err := uuid.Parse(id)
	if err != nil {
		return httpx.NotFound("That comment isn't available.")
	}
	_, err = removeComment(ctx, tx, cid, nil)
	return err
}

func (t commentTarget) Restore(ctx context.Context, tx pgx.Tx, id string) error {
	cid, err := uuid.Parse(id)
	if err != nil {
		return httpx.NotFound("That comment isn't available.")
	}
	_, err = tx.Exec(ctx, `
		UPDATE feed_comments SET removed_at = NULL, removed_by = NULL, updated_at = now()
		WHERE id = $1 AND removed_at IS NOT NULL`, cid)
	return err
}
