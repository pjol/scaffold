package presence

import (
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
)

// maxPresets is how many status presets one person keeps.
const maxPresets = 10

// StatusPreset is a status a person saved to set again in one tap. The app's
// defaults ("In a meeting", "Commuting", …) live in the clients.
type StatusPreset struct {
	ID         uuid.UUID `json:"id"`
	Emoji      string    `json:"emoji"`
	Text       string    `json:"text"`
	ClearAfter string    `json:"clear_after"`
	CreatedAt  time.Time `json:"created_at"`
}

const presetCols = `id, emoji, text, clear_after, created_at`

// GET /presence/me/presets — oldest first.
func (s *Service) listPresets(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context()).UserID
	rows, err := s.d.Pool.Query(r.Context(), `SELECT `+presetCols+` FROM presence_status_presets WHERE user_id = $1 ORDER BY created_at, id`, me)
	if err != nil {
		return err
	}
	items, err := pgx.CollectRows(rows, pgx.RowToStructByPos[StatusPreset])
	if err != nil {
		return err
	}
	if items == nil {
		items = []StatusPreset{}
	}
	httpx.OK(w, map[string]any{"items": items})
	return nil
}

type presetReq struct {
	Emoji      string `json:"emoji"`
	Text       string `json:"text"`
	ClearAfter string `json:"clear_after"`
}

// POST /presence/me/presets {emoji, text, clear_after} → 201
func (s *Service) createPreset(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	var req presetReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	emoji, text, err := normalizeStatus(req.Emoji, req.Text)
	if err != nil {
		return err
	}
	switch req.ClearAfter {
	case "":
		req.ClearAfter = "never"
	case "30m", "1h", "4h", "today", "week", "never":
	default:
		return httpx.BadRequest("clear_after must be 30m, 1h, 4h, today, week or never.")
	}
	if err := checkText(ctx, text); err != nil {
		return err
	}
	var p StatusPreset
	err = s.d.Pool.QueryRow(ctx, `
		WITH mine AS (SELECT count(*) AS n, bool_or(emoji = $2 AND text = $3) AS dup FROM presence_status_presets WHERE user_id = $1)
		INSERT INTO presence_status_presets (user_id, emoji, text, clear_after)
		SELECT $1, $2, $3, $4 FROM mine WHERE mine.n < $5 AND NOT COALESCE(mine.dup, false)
		RETURNING `+presetCols, me, emoji, text, req.ClearAfter, maxPresets).
		Scan(&p.ID, &p.Emoji, &p.Text, &p.ClearAfter, &p.CreatedAt)
	if err == pgx.ErrNoRows {
		return httpx.NewError(http.StatusConflict, "preset_refused", "You already saved this status, or you have 10 saved. Remove one first.")
	}
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, p)
	return nil
}

// DELETE /presence/me/presets/{id}
func (s *Service) deletePreset(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context()).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	tag, err := s.d.Pool.Exec(r.Context(), `DELETE FROM presence_status_presets WHERE id = $1 AND user_id = $2`, id, me)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.NotFound("That saved status doesn't exist.")
	}
	httpx.NoContent(w)
	return nil
}
