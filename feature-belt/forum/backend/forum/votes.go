package forum

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/moderation"
)

// Karma is the sum of the counted votes userID's posts and comments received
// since since (the zero time: all of them). Votes from accounts younger than
// FORUM_KARMA_MIN_AGE when they voted don't count. Other features (the
// reputation feature's community seam) read it.
func Karma(ctx context.Context, q db.Querier, userID uuid.UUID, since time.Time) (int, error) {
	var k int
	err := q.QueryRow(ctx, `SELECT COALESCE(sum(value), 0) FROM forum_votes
		WHERE author_id = $1 AND counts AND created_at >= $2`, userID, since).Scan(&k)
	return k, err
}

// viewerVotes is the viewer's vote on each of ids (missing = 0).
func (s *Service) viewerVotes(ctx context.Context, viewer uuid.UUID, kind string, ids []uuid.UUID) (map[uuid.UUID]int, error) {
	out := map[uuid.UUID]int{}
	if len(ids) == 0 || viewer == uuid.Nil {
		return out, nil
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT target_id, value FROM forum_votes WHERE user_id = $1 AND target_kind = $2 AND target_id = ANY($3)`,
		viewer, kind, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var v int
		if err := rows.Scan(&id, &v); err != nil {
			return nil, err
		}
		out[id] = v
	}
	return out, rows.Err()
}

type voteReq struct {
	TargetKind string    `json:"target_kind"` // post | comment
	TargetID   uuid.UUID `json:"target_id"`
	Value      int       `json:"value"` // -1, 0 (none), 1
}

// VoteResult is PUT /v1/forum/votes.
type VoteResult struct {
	Score      int `json:"score"`
	ViewerVote int `json:"viewer_vote"`
}

// voteHandler: PUT /v1/forum/votes. Idempotent. Not on your own content, not
// across a block, only on visible items; accounts younger than
// FORUM_VOTE_MIN_AGE can't vote, and those younger than FORUM_KARMA_MIN_AGE
// move scores but not the author's karma.
func (s *Service) voteHandler(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	caller := auth.MustFromContext(ctx)
	var req voteReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.TargetKind != "post" && req.TargetKind != "comment" {
		return httpx.BadRequest(`target_kind must be "post" or "comment".`)
	}
	if req.Value < -1 || req.Value > 1 {
		return httpx.BadRequest("value must be -1, 0 or 1.")
	}
	var created time.Time
	if err := s.d.Pool.QueryRow(ctx, `SELECT created_at FROM users WHERE id = $1`, caller.UserID).Scan(&created); err != nil {
		return err
	}
	age := time.Since(created)
	if age < s.cfg.voteMinAge {
		return httpx.NewError(http.StatusForbidden, "vote_restricted", "New accounts can vote a little later.")
	}
	counts := age >= s.cfg.karmaMinAge
	var (
		res    VoteResult
		postID uuid.UUID
	)
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var (
			author      uuid.UUID
			status      string
			score       int
			itemCreated time.Time
			labels      []string
		)
		table := "forum_posts"
		postCol := "id"
		if req.TargetKind == "comment" {
			table, postCol = "forum_comments", "post_id"
		}
		err := tx.QueryRow(ctx, `SELECT author_id, status, score, created_at, labels, `+postCol+` FROM `+table+` WHERE id = $1 FOR UPDATE`,
			req.TargetID).Scan(&author, &status, &score, &itemCreated, &labels, &postID)
		if db.IsNoRows(err) || (err == nil && status != StatusVisible) {
			return httpx.NotFound("That isn't there anymore.")
		}
		if err != nil {
			return err
		}
		if author == caller.UserID {
			return errVoteOwn
		}
		if blocked, err := moderation.IsBlocked(ctx, tx, caller.UserID, author); err != nil {
			return err
		} else if blocked {
			return errBlocked
		}
		var (
			old       int
			oldCounts bool
		)
		err = tx.QueryRow(ctx, `SELECT value, counts FROM forum_votes WHERE user_id = $1 AND target_kind = $2 AND target_id = $3`,
			caller.UserID, req.TargetKind, req.TargetID).Scan(&old, &oldCounts)
		if err != nil && !db.IsNoRows(err) {
			return err
		}
		if old == req.Value {
			res = VoteResult{Score: score, ViewerVote: old}
			return nil
		}
		if req.Value == 0 {
			_, err = tx.Exec(ctx, `DELETE FROM forum_votes WHERE user_id = $1 AND target_kind = $2 AND target_id = $3`,
				caller.UserID, req.TargetKind, req.TargetID)
		} else {
			// a changed vote keeps its first time (karma "since" counts from when it was cast)
			_, err = tx.Exec(ctx, `INSERT INTO forum_votes (user_id, target_kind, target_id, author_id, value, counts)
				VALUES ($1, $2, $3, $4, $5, $6)
				ON CONFLICT (user_id, target_kind, target_id) DO UPDATE SET value = EXCLUDED.value, counts = EXCLUDED.counts`,
				caller.UserID, req.TargetKind, req.TargetID, author, req.Value, counts)
		}
		if err != nil {
			return err
		}
		ups, downs := 0, 0
		switch old {
		case 1:
			ups--
		case -1:
			downs--
		}
		switch req.Value {
		case 1:
			ups++
		case -1:
			downs++
		}
		score += req.Value - old
		if req.TargetKind == "post" {
			_, err = tx.Exec(ctx, `UPDATE forum_posts SET ups = ups + $2, downs = downs + $3, score = $4, hot = $5 WHERE id = $1`,
				req.TargetID, ups, downs, score, Hot(score, itemCreated, hasLabel(labels, moderation.LabelOffTopic)))
		} else {
			_, err = tx.Exec(ctx, `UPDATE forum_comments SET ups = ups + $2, downs = downs + $3, score = $4 WHERE id = $1`,
				req.TargetID, ups, downs, score)
		}
		res = VoteResult{Score: score, ViewerVote: req.Value}
		return err
	})
	if err != nil {
		return err
	}
	s.publish(ctx, postID, EventVote, map[string]any{"post_id": postID, "target_kind": req.TargetKind, "target_id": req.TargetID, "score": res.Score})
	httpx.OK(w, res)
	return nil
}

// karmaHandler: GET /v1/forum/karma?user_id=: a person's karma, all time and
// over the last 90 days (yours without user_id).
func (s *Service) karmaHandler(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id := auth.MustFromContext(ctx).UserID
	if v := r.URL.Query().Get("user_id"); v != "" {
		u, err := uuid.Parse(v)
		if err != nil {
			return httpx.BadRequest("Invalid user_id.")
		}
		id = u
	}
	total, err := Karma(ctx, s.d.Pool, id, time.Time{})
	if err != nil {
		return err
	}
	recent, err := Karma(ctx, s.d.Pool, id, time.Now().Add(-90*24*time.Hour))
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"user_id": id, "karma": total, "karma_90d": recent})
	return nil
}
