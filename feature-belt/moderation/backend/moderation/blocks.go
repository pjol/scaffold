package moderation

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/realtime"
)

// BlockedUser is one person the caller blocked.
type BlockedUser struct {
	UserID      uuid.UUID `json:"user_id"`
	DisplayName string    `json:"display_name"`
	AvatarURL   *string   `json:"avatar_url"`
	CreatedAt   time.Time `json:"created_at"`
}

type blockCursor struct {
	T  time.Time `json:"t"`
	ID uuid.UUID `json:"id"`
}

// listBlocks: GET /v1/moderation/blocks, newest first. Only blocks the
// caller made; who blocked the caller is never shown.
func (s *Service) listBlocks(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context()).UserID
	p := httpx.ParsePage(r)
	var c blockCursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	list, err := blockedUsers(r.Context(), s.d.Pool, me, nullTime(c.T), c.ID, p.Limit+1)
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(list, p.Limit, func(b BlockedUser) any { return blockCursor{b.CreatedAt, b.UserID} }))
	return nil
}

func blockedUsers(ctx context.Context, q db.Querier, me uuid.UUID, before *time.Time, beforeID uuid.UUID, limit int) ([]BlockedUser, error) {
	rows, err := q.Query(ctx, `
		SELECT b.blocked_id,
		       CASE WHEN u.status = 'deleted' THEN 'Deleted user' ELSE u.display_name END,
		       u.avatar_url, b.created_at
		FROM moderation_blocks b JOIN users u ON u.id = b.blocked_id
		WHERE b.blocker_id = $1 AND ($2::timestamptz IS NULL OR (b.created_at, b.blocked_id) < ($2, $3))
		ORDER BY b.created_at DESC, b.blocked_id DESC LIMIT $4`, me, before, beforeID, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[BlockedUser])
}

// maxBlockedIDs caps GET /v1/moderation/blocks/ids.
const maxBlockedIDs = 5000

// blockedIDs: GET /v1/moderation/blocks/ids, the ids of everyone the caller
// blocked (never who blocked them), so clients can show block state and drop
// realtime events from those people without paging the list.
func (s *Service) blockedIDs(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context()).UserID
	rows, err := s.d.Pool.Query(r.Context(), `SELECT blocked_id FROM moderation_blocks WHERE blocker_id = $1
		ORDER BY created_at DESC LIMIT $2`, me, maxBlockedIDs)
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return err
	}
	if ids == nil {
		ids = []uuid.UUID{}
	}
	httpx.OK(w, map[string]any{"user_ids": ids})
	return nil
}

type blockReq struct {
	UserID uuid.UUID `json:"user_id"`
}

// block: POST /v1/moderation/blocks {user_id}. Idempotent.
func (s *Service) block(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	var req blockReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.UserID == uuid.Nil {
		return httpx.BadRequest("user_id is required.")
	}
	if req.UserID == me {
		return httpx.BadRequest("You can't block yourself.")
	}
	b, err := Block(ctx, s.d.Pool, me, req.UserID)
	if err != nil {
		return err
	}
	s.publish(ctx, realtime.UserTopic(me), EventBlocksChanged, map[string]any{"user_id": req.UserID, "blocked": true})
	httpx.JSON(w, http.StatusCreated, b)
	return nil
}

// Block records that blocker blocked blocked (idempotent) and returns the
// row. It is exported for stitching code (e.g. "block and report" in one
// tap) and the agent tool.
func Block(ctx context.Context, q db.Querier, blocker, blocked uuid.UUID) (BlockedUser, error) {
	var b BlockedUser
	err := q.QueryRow(ctx, `
		WITH target AS (SELECT id, display_name, avatar_url FROM users WHERE id = $2 AND status <> 'deleted'),
		     ins AS (
		       INSERT INTO moderation_blocks (blocker_id, blocked_id)
		       SELECT $1, id FROM target
		       ON CONFLICT (blocker_id, blocked_id) DO UPDATE SET blocker_id = EXCLUDED.blocker_id
		       RETURNING blocked_id, created_at)
		SELECT ins.blocked_id, target.display_name, target.avatar_url, ins.created_at
		FROM ins JOIN target ON target.id = ins.blocked_id`, blocker, blocked,
	).Scan(&b.UserID, &b.DisplayName, &b.AvatarURL, &b.CreatedAt)
	if db.IsNoRows(err) {
		return b, httpx.NotFound("That person doesn't have an account.")
	}
	return b, err
}

// unblock: DELETE /v1/moderation/blocks/{user_id}. Idempotent.
func (s *Service) unblock(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	other, err := httpx.URLParamUUID(r, "user_id")
	if err != nil {
		return err
	}
	tag, err := s.d.Pool.Exec(ctx, `DELETE FROM moderation_blocks WHERE blocker_id = $1 AND blocked_id = $2`, me, other)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		s.publish(ctx, realtime.UserTopic(me), EventBlocksChanged, map[string]any{"user_id": other, "blocked": false})
	}
	httpx.NoContent(w)
	return nil
}
