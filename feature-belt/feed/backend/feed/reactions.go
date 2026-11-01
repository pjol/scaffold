package feed

import (
	"context"
	"net/http"
	"slices"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/notify"
)

type reactReq struct {
	Reaction string `json:"reaction"` // like | love | laugh | wow | sad
}

func decodeReaction(r *http.Request) (string, error) {
	var req reactReq
	if err := httpx.Decode(r, &req); err != nil {
		return "", err
	}
	if !slices.Contains(Reactions, req.Reaction) {
		return "", httpx.BadRequest("Reaction must be like, love, laugh, wow or sad.")
	}
	return req.Reaction, nil
}

// reactionSummary counts a target's reactions. table is feed_post_reactions
// or feed_comment_reactions, col post_id or comment_id (fixed strings).
func (s *Service) reactionSummary(ctx context.Context, table, col string, id, viewerID uuid.UUID) (map[string]int, int, *string, error) {
	rows, err := s.d.Pool.Query(ctx, `
		SELECT reaction, count(*), bool_or(user_id = $2) FROM `+table+` WHERE `+col+` = $1 GROUP BY reaction`, id, viewerID)
	if err != nil {
		return nil, 0, nil, err
	}
	defer rows.Close()
	counts := map[string]int{}
	total := 0
	var mine *string
	for rows.Next() {
		var reaction string
		var n int
		var me bool
		if err := rows.Scan(&reaction, &n, &me); err != nil {
			return nil, 0, nil, err
		}
		counts[reaction] = n
		total += n
		if me {
			r := reaction
			mine = &r
		}
	}
	return counts, total, mine, rows.Err()
}

// respondReaction answers the caller (with my_reaction) and broadcasts the
// counts (without it).
func (s *Service) respondReaction(w http.ResponseWriter, r *http.Request, kind, table, col string, target, postID uuid.UUID, audience string) error {
	caller := auth.MustFromContext(r.Context())
	counts, total, mine, err := s.reactionSummary(r.Context(), table, col, target, caller.UserID)
	if err != nil {
		return err
	}
	sum := ReactionSummary{TargetKind: kind, TargetID: target, PostID: postID, Reactions: counts, ReactionCount: total}
	s.publish(r.Context(), audience, EventReaction, sum)
	sum.MyReaction = mine
	httpx.OK(w, sum)
	return nil
}

// ---- posts -----------------------------------------------------------------

func (s *Service) reactPostHandler(w http.ResponseWriter, r *http.Request) error {
	caller := auth.MustFromContext(r.Context())
	ctx := r.Context()
	pid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	reaction, err := decodeReaction(r)
	if err != nil {
		return err
	}
	v, err := s.viewerFor(ctx, caller.UserID)
	if err != nil {
		return err
	}
	post, err := s.visiblePost(ctx, s.d.Pool, v, pid)
	if err != nil {
		return err
	}
	var inserted bool
	if err := s.d.Pool.QueryRow(ctx, `
		INSERT INTO feed_post_reactions (post_id, user_id, reaction) VALUES ($1, $2, $3)
		ON CONFLICT (post_id, user_id) DO UPDATE SET reaction = EXCLUDED.reaction, updated_at = now()
		RETURNING (xmax = 0)`, pid, caller.UserID, reaction).Scan(&inserted); err != nil {
		return err
	}
	if inserted {
		name := actorName(caller.DisplayName)
		s.afterCommit(ctx, "feed.notify_reaction", func(ctx context.Context) {
			var body string
			_ = s.d.Pool.QueryRow(ctx, `SELECT body FROM feed_posts WHERE id = $1`, pid).Scan(&body)
			s.notifyUser(ctx, caller.UserID, notify.Notification{
				UserID: post.authorID, Kind: NotifyReaction, Title: name + " reacted to your post",
				Body: preview(body), Link: "/feed/posts/" + pid.String(), Data: map[string]any{"post_id": pid, "reaction": reaction},
			})
		})
	}
	return s.respondReaction(w, r, "post", "feed_post_reactions", "post_id", pid, pid, post.audience)
}

func (s *Service) unreactPostHandler(w http.ResponseWriter, r *http.Request) error {
	caller := auth.MustFromContext(r.Context())
	ctx := r.Context()
	pid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	v, err := s.viewerFor(ctx, caller.UserID)
	if err != nil {
		return err
	}
	post, err := s.visiblePost(ctx, s.d.Pool, v, pid)
	if err != nil {
		return err
	}
	if _, err := s.d.Pool.Exec(ctx, `DELETE FROM feed_post_reactions WHERE post_id = $1 AND user_id = $2`, pid, caller.UserID); err != nil {
		return err
	}
	return s.respondReaction(w, r, "post", "feed_post_reactions", "post_id", pid, pid, post.audience)
}

// ---- comments --------------------------------------------------------------

type commentRef struct {
	postID, authorID uuid.UUID
	body, audience   string
}

func (s *Service) visibleComment(ctx context.Context, v viewer, id uuid.UUID) (commentRef, error) {
	var ref commentRef
	var postAuthor uuid.UUID
	err := s.d.Pool.QueryRow(ctx, `
		SELECT c.post_id, c.author_id, c.body, p.author_id, p.audience
		FROM feed_comments c JOIN feed_posts p ON p.id = c.post_id
		WHERE c.id = $1 AND c.deleted_at IS NULL AND c.removed_at IS NULL AND p.removed_at IS NULL`, id).
		Scan(&ref.postID, &ref.authorID, &ref.body, &postAuthor, &ref.audience)
	if db.IsNoRows(err) || (err == nil && (!v.reads(ref.audience) || v.blocks(postAuthor) || v.blocks(ref.authorID))) {
		return commentRef{}, httpx.NotFound("That comment isn't available.")
	}
	return ref, err
}

func (s *Service) reactCommentHandler(w http.ResponseWriter, r *http.Request) error {
	caller := auth.MustFromContext(r.Context())
	ctx := r.Context()
	cid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	reaction, err := decodeReaction(r)
	if err != nil {
		return err
	}
	v, err := s.viewerFor(ctx, caller.UserID)
	if err != nil {
		return err
	}
	c, err := s.visibleComment(ctx, v, cid)
	if err != nil {
		return err
	}
	var inserted bool
	if err := s.d.Pool.QueryRow(ctx, `
		INSERT INTO feed_comment_reactions (comment_id, user_id, reaction) VALUES ($1, $2, $3)
		ON CONFLICT (comment_id, user_id) DO UPDATE SET reaction = EXCLUDED.reaction, updated_at = now()
		RETURNING (xmax = 0)`, cid, caller.UserID, reaction).Scan(&inserted); err != nil {
		return err
	}
	if inserted {
		name := actorName(caller.DisplayName)
		s.afterCommit(ctx, "feed.notify_reaction", func(ctx context.Context) {
			s.notifyUser(ctx, caller.UserID, notify.Notification{
				UserID: c.authorID, Kind: NotifyReaction, Title: name + " reacted to your comment",
				Body: preview(c.body), Link: "/feed/posts/" + c.postID.String(),
				Data: map[string]any{"post_id": c.postID, "comment_id": cid, "reaction": reaction},
			})
		})
	}
	return s.respondReaction(w, r, "comment", "feed_comment_reactions", "comment_id", cid, c.postID, c.audience)
}

func (s *Service) unreactCommentHandler(w http.ResponseWriter, r *http.Request) error {
	caller := auth.MustFromContext(r.Context())
	ctx := r.Context()
	cid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	v, err := s.viewerFor(ctx, caller.UserID)
	if err != nil {
		return err
	}
	c, err := s.visibleComment(ctx, v, cid)
	if err != nil {
		return err
	}
	if _, err := s.d.Pool.Exec(ctx, `DELETE FROM feed_comment_reactions WHERE comment_id = $1 AND user_id = $2`, cid, caller.UserID); err != nil {
		return err
	}
	return s.respondReaction(w, r, "comment", "feed_comment_reactions", "comment_id", cid, c.postID, c.audience)
}
