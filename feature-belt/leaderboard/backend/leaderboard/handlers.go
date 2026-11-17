package leaderboard

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
)

func okJSON(w http.ResponseWriter, v any) error {
	httpx.OK(w, v)
	return nil
}

// BoardInfo is a board as clients see it.
type BoardInfo struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Format    string     `json:"format"`
	Currency  string     `json:"currency,omitempty"`
	Ascending bool       `json:"ascending"`
	Ranked    int        `json:"ranked"`     // people with a rank
	UpdatedAt *time.Time `json:"updated_at"` // latest score change
}

// Entry is one row of a board.
type Entry struct {
	Rank      int     `json:"rank"`
	Name      string  `json:"name"`       // display name or alias, by the person's choice
	AvatarURL *string `json:"avatar_url"` // only when they show their name
	Score     string  `json:"score"`
	IsMe      bool    `json:"is_me"`
}

// Standing is the caller's place on a board.
type Standing struct {
	Board      string  `json:"board"`
	Rank       *int    `json:"rank"`  // null: not ranked (no score, or hidden)
	Score      *string `json:"score"` // null: no score
	Ranked     int     `json:"ranked"`
	Visibility string  `json:"visibility"`
}

func (s *Service) boardInfos(ctx context.Context) ([]BoardInfo, error) {
	all := s.all()
	out := make([]BoardInfo, 0, len(all))
	for _, b := range all {
		info := BoardInfo{ID: b.ID, Title: b.Title, Format: b.Format, Currency: b.Currency, Ascending: b.Ascending}
		if err := s.d.Pool.QueryRow(ctx, `SELECT count(rank), max(updated_at) FROM leaderboard_entries WHERE board = $1`, b.ID).
			Scan(&info.Ranked, &info.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, info)
	}
	return out, nil
}

func (s *Service) boardsHandler(w http.ResponseWriter, r *http.Request) error {
	items, err := s.boardInfos(r.Context())
	if err != nil {
		return err
	}
	return okJSON(w, map[string]any{"items": items})
}

type entryCursor struct {
	Rank int       `json:"r"`
	User uuid.UUID `json:"u"`
}

func (s *Service) entries(ctx context.Context, board string, me uuid.UUID, c entryCursor, limit int) ([]Entry, []entryCursor, error) {
	rows, err := s.d.Pool.Query(ctx, `
		SELECT e.rank, e.score::text, e.user_id, COALESCE(p.visibility, $5), COALESCE(p.alias, ''), u.display_name, u.avatar_url
		FROM leaderboard_entries e
		JOIN users u ON u.id = e.user_id
		LEFT JOIN leaderboard_profiles p ON p.user_id = e.user_id
		WHERE e.board = $1 AND e.rank IS NOT NULL AND (e.rank, e.user_id) > ($2, $3)
		ORDER BY e.rank, e.user_id LIMIT $4`, board, c.Rank, c.User, limit, s.defaultVisibility)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var out []Entry
	var cursors []entryCursor
	for rows.Next() {
		var e Entry
		var uid uuid.UUID
		var vis, alias, name string
		var avatar *string
		if err := rows.Scan(&e.Rank, &e.Score, &uid, &vis, &alias, &name, &avatar); err != nil {
			return nil, nil, err
		}
		e.Name = alias
		if vis == ShowName && strings.TrimSpace(name) != "" {
			e.Name, e.AvatarURL = name, avatar
		}
		if e.Name == "" {
			e.Name = "Anonymous"
		}
		e.IsMe = uid == me
		out = append(out, e)
		cursors = append(cursors, entryCursor{Rank: e.Rank, User: uid})
	}
	return out, cursors, rows.Err()
}

func (s *Service) boardHandler(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "board")
	if _, ok := s.board(id); !ok || !validBoardID(id) {
		return httpx.NotFound("No such leaderboard.")
	}
	pp := httpx.ParsePage(r)
	var c entryCursor
	if err := httpx.DecodeCursor(pp.Cursor, &c); err != nil {
		return err
	}
	me := auth.MustFromContext(r.Context()).UserID
	items, cursors, err := s.entries(r.Context(), id, me, c, pp.Limit+1)
	if err != nil {
		return err
	}
	page := httpx.Page[Entry]{Items: items}
	if page.Items == nil {
		page.Items = []Entry{}
	}
	if len(items) > pp.Limit {
		page.Items = items[:pp.Limit]
		page.NextCursor = httpx.EncodeCursor(cursors[pp.Limit-1])
	}
	return okJSON(w, page)
}

func (s *Service) standing(ctx context.Context, board string, userID uuid.UUID) (Standing, error) {
	st := Standing{Board: board}
	p, err := s.profile(ctx, userID)
	if err != nil {
		return st, err
	}
	st.Visibility = p.Visibility
	err = s.d.Pool.QueryRow(ctx, `SELECT rank, score::text FROM leaderboard_entries WHERE board = $1 AND user_id = $2`, board, userID).
		Scan(&st.Rank, &st.Score)
	if err != nil && err != pgx.ErrNoRows {
		return st, err
	}
	if err := s.d.Pool.QueryRow(ctx, `SELECT count(rank) FROM leaderboard_entries WHERE board = $1`, board).Scan(&st.Ranked); err != nil {
		return st, err
	}
	return st, nil
}

func (s *Service) meHandler(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "board")
	if _, ok := s.board(id); !ok {
		return httpx.NotFound("No such leaderboard.")
	}
	st, err := s.standing(r.Context(), id, auth.MustFromContext(r.Context()).UserID)
	if err != nil {
		return err
	}
	return okJSON(w, st)
}

// ProfileView is GET /v1/leaderboard/profile.
type ProfileView struct {
	Profile
	DisplayName string `json:"display_name"`
}

func (s *Service) profileView(ctx context.Context, id *auth.Identity) (ProfileView, error) {
	p, err := s.profile(ctx, id.UserID)
	if err != nil {
		return ProfileView{}, err
	}
	var name string
	_ = s.d.Pool.QueryRow(ctx, `SELECT display_name FROM users WHERE id = $1`, id.UserID).Scan(&name)
	return ProfileView{Profile: p, DisplayName: name}, nil
}

func (s *Service) profileHandler(w http.ResponseWriter, r *http.Request) error {
	v, err := s.profileView(r.Context(), auth.MustFromContext(r.Context()))
	if err != nil {
		return err
	}
	return okJSON(w, v)
}

func (s *Service) updateProfileHandler(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Visibility string `json:"visibility"`
	}
	if err := httpx.Decode(r, &body); err != nil {
		return err
	}
	if body.Visibility != ShowAlias && body.Visibility != ShowName && body.Visibility != Hidden {
		return httpx.BadRequest("visibility must be alias, name or hidden.")
	}
	id := auth.MustFromContext(r.Context())
	if _, err := s.profile(r.Context(), id.UserID); err != nil {
		return err
	}
	if _, err := s.d.Pool.Exec(r.Context(), `UPDATE leaderboard_profiles SET visibility = $2, updated_at = now() WHERE user_id = $1`,
		id.UserID, body.Visibility); err != nil {
		return err
	}
	if body.Visibility == Hidden { // off the boards now; others' ranks close up on the next refresh
		if _, err := s.d.Pool.Exec(r.Context(), `UPDATE leaderboard_entries SET rank = NULL WHERE user_id = $1`, id.UserID); err != nil {
			return err
		}
	}
	v, err := s.profileView(r.Context(), id)
	if err != nil {
		return err
	}
	return okJSON(w, v)
}

// ---- agent tools ----

// Tools exposes boards to agents (read-only).
func (s *Service) Tools() []app.Tool {
	return []app.Tool{{
		Name:        "leaderboard_get",
		Description: "List the app's leaderboards, or read one: the top entries (rank, name, score) and the user's own rank and score.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"board": {"type": "string", "description": "Board id; omit to list the boards."},
				"limit": {"type": "integer", "minimum": 1, "maximum": 50}
			},
			"additionalProperties": false
		}`),
		ReadOnly: true,
		Handler: func(ctx context.Context, caller *auth.Identity, raw json.RawMessage) (any, error) {
			var in struct {
				Board string `json:"board"`
				Limit int    `json:"limit"`
			}
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, &in); err != nil {
					return nil, httpx.BadRequest("Invalid input: " + err.Error())
				}
			}
			if in.Board == "" {
				items, err := s.boardInfos(ctx)
				return map[string]any{"boards": items}, err
			}
			b, ok := s.board(in.Board)
			if !ok {
				return nil, httpx.NotFound("No such leaderboard.")
			}
			if in.Limit <= 0 || in.Limit > 50 {
				in.Limit = 10
			}
			top, _, err := s.entries(ctx, b.ID, caller.UserID, entryCursor{}, in.Limit)
			if err != nil {
				return nil, err
			}
			me, err := s.standing(ctx, b.ID, caller.UserID)
			if err != nil {
				return nil, err
			}
			return map[string]any{"board": BoardInfo{ID: b.ID, Title: b.Title, Format: b.Format, Currency: b.Currency, Ascending: b.Ascending},
				"top": top, "me": me}, nil
		},
	}}
}

// ---- account deletion and export ----

// DeleteAccount removes the person's scores and profile.
func (s *Service) DeleteAccount(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	if _, err := tx.Exec(ctx, `DELETE FROM leaderboard_entries WHERE user_id = $1`, userID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `DELETE FROM leaderboard_profiles WHERE user_id = $1`, userID)
	return err
}

// ExportAccount returns the person's profile and scores.
func (s *Service) ExportAccount(ctx context.Context, userID uuid.UUID) (any, error) {
	out := map[string]any{"profile": nil}
	var p Profile
	err := s.d.Pool.QueryRow(ctx, `SELECT visibility, alias FROM leaderboard_profiles WHERE user_id = $1`, userID).Scan(&p.Visibility, &p.Alias)
	if err == nil {
		out["profile"] = p
	} else if err != pgx.ErrNoRows {
		return nil, err
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT board, score::text, rank, updated_at FROM leaderboard_entries WHERE user_id = $1 ORDER BY board`, userID)
	if err != nil {
		return nil, err
	}
	type entry struct {
		Board     string    `json:"board"`
		Score     string    `json:"score"`
		Rank      *int      `json:"rank"`
		UpdatedAt time.Time `json:"updated_at"`
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[entry])
	if err != nil {
		return nil, err
	}
	if list == nil {
		list = []entry{}
	}
	out["scores"] = list
	return out, nil
}
