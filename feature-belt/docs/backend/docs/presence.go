package docs

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// Viewer is one open copy of a page: who, and the block they are typing in.
type Viewer struct {
	SessionID string     `json:"session_id"`
	User      RefUser    `json:"user"`
	BlockID   *uuid.UUID `json:"block_id"`
	SeenAt    time.Time  `json:"seen_at"`
}

// ttlInterval: a session that hasn't beaten for this long is gone. Clients
// beat every 20 seconds and prune on the same rule.
const ttlInterval = "45 seconds"

func validSession(s string) bool {
	return len(s) >= 8 && len(s) <= 64 && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_") == ""
}

// listPresence: GET /docs/pages/{id}/presence (view).
func (s *Service) listPresence(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	if _, err := access.Require(ctx, s.d.Pool, me, Kind, id, access.View); err != nil {
		return err
	}
	list, err := s.viewers(r, id)
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": list})
	return nil
}

func (s *Service) viewers(r *http.Request, pageID uuid.UUID) ([]Viewer, error) {
	rows, err := s.d.Pool.Query(r.Context(), `
		SELECT pr.session_id, u.id, u.display_name, u.avatar_url, pr.block_id, pr.seen_at
		FROM docs_presence pr JOIN users u ON u.id = pr.user_id
		WHERE pr.page_id = $1 AND pr.seen_at > now() - $2::interval AND access_level(pr.user_id, 'docs.page', pr.page_id) >= 1
		ORDER BY pr.created_at LIMIT 50`, pageID, ttlInterval)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Viewer{}
	for rows.Next() {
		var v Viewer
		if err := rows.Scan(&v.SessionID, &v.User.ID, &v.User.Name, &v.User.AvatarURL, &v.BlockID, &v.SeenAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

type beatReq struct {
	SessionID string     `json:"session_id"`
	BlockID   *uuid.UUID `json:"block_id"` // the block being edited, if any
}

// beat: POST /docs/pages/{id}/presence (view): I'm here (and typing in
// block_id). Announced to the page's viewers when it is new or the block
// changed.
func (s *Service) beat(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id := auth.MustFromContext(ctx)
	pageID, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req beatReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if !validSession(req.SessionID) {
		return httpx.BadRequest("session_id must be 8 to 64 letters, digits, - or _.")
	}
	if _, err := access.Require(ctx, s.d.Pool, id.UserID, Kind, pageID, access.View); err != nil {
		return err
	}
	var changed bool
	err = s.d.Pool.QueryRow(ctx, `
		WITH prev AS (SELECT block_id, seen_at FROM docs_presence WHERE page_id = $1 AND session_id = $2)
		INSERT INTO docs_presence (page_id, session_id, user_id, block_id) VALUES ($1, $2, $3, $4)
		ON CONFLICT (page_id, session_id) DO UPDATE SET block_id = EXCLUDED.block_id, seen_at = now()
		WHERE docs_presence.user_id = EXCLUDED.user_id
		RETURNING NOT EXISTS (SELECT 1 FROM prev)
		       OR (SELECT block_id FROM prev) IS DISTINCT FROM $4
		       OR (SELECT seen_at FROM prev) < now() - $5::interval`,
		pageID, req.SessionID, id.UserID, req.BlockID, ttlInterval).Scan(&changed)
	if db.IsNoRows(err) {
		return httpx.Conflict("That session belongs to someone else.")
	}
	if err != nil {
		return err
	}
	if changed {
		var avatar *string
		_ = s.d.Pool.QueryRow(ctx, `SELECT avatar_url FROM users WHERE id = $1`, id.UserID).Scan(&avatar)
		s.publish(ctx, pageTopic(pageID), EventPresence, map[string]any{
			"page_id": pageID, "session_id": req.SessionID, "block_id": req.BlockID,
			"user": RefUser{ID: id.UserID, Name: id.DisplayName, AvatarURL: avatar}, "left": false,
		})
	}
	list, err := s.viewers(r, pageID)
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": list, "interval_seconds": 20})
	return nil
}

// leave: DELETE /docs/pages/{id}/presence/{session}: this copy closed.
func (s *Service) leave(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	pageID, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	session := chi.URLParam(r, "session")
	tag, err := s.d.Pool.Exec(ctx, `DELETE FROM docs_presence WHERE page_id = $1 AND session_id = $2 AND user_id = $3`, pageID, session, me)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		s.publish(ctx, pageTopic(pageID), EventPresence, map[string]any{"page_id": pageID, "session_id": session, "left": true})
	}
	httpx.NoContent(w)
	return nil
}
