package search

import (
	"context"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
)

// maxSaved is how many saved searches one person keeps.
const maxSaved = 50

// Saved is a named search a person runs again (operators included).
type Saved struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Query     string    `json:"query"`
	Kinds     string    `json:"kinds"`  // comma list, as GET /search takes it; "" = all
	Source    string    `json:"source"` // as GET /search takes it; "" = all
	CreatedAt time.Time `json:"created_at"`
}

const savedCols = `id, name, query, kinds, source, created_at`

func (s *Service) savedFor(ctx context.Context, userID uuid.UUID) ([]Saved, error) {
	rows, err := s.d.Pool.Query(ctx, `SELECT `+savedCols+` FROM search_saved WHERE user_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2`, userID, maxSaved)
	if err != nil {
		return nil, err
	}
	items, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Saved])
	if items == nil {
		items = []Saved{}
	}
	return items, err
}

// GET /search/saved — newest first.
func (s *Service) listSaved(w http.ResponseWriter, r *http.Request) error {
	items, err := s.savedFor(r.Context(), auth.MustFromContext(r.Context()).UserID)
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": items})
	return nil
}

type savedReq struct {
	Name   string `json:"name"`
	Query  string `json:"query"`
	Kinds  string `json:"kinds"`
	Source string `json:"source"`
}

// POST /search/saved {name, query, kinds?, source?} → 201
func (s *Service) createSaved(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	var req savedReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	q, err := NormalizeQuery(req.Query)
	if err != nil {
		return err
	}
	name := strings.Join(strings.Fields(req.Name), " ")
	if name == "" {
		name = q
	}
	if utf8.RuneCountInString(name) > 80 {
		return httpx.BadRequest("Names can be up to 80 characters.")
	}
	kinds, err := parseKinds(req.Kinds)
	if err != nil {
		return err
	}
	source, err := parseSource(req.Source)
	if err != nil {
		return err
	}
	var out Saved
	err = s.d.Pool.QueryRow(ctx, `
		INSERT INTO search_saved (user_id, name, query, kinds, source)
		SELECT $1, $2, $3, $4, $5 WHERE (SELECT count(*) FROM search_saved WHERE user_id = $1) < $6
		RETURNING `+savedCols, me, name, q, strings.Join(kinds, ","), source, maxSaved).
		Scan(&out.ID, &out.Name, &out.Query, &out.Kinds, &out.Source, &out.CreatedAt)
	if err == pgx.ErrNoRows {
		return httpx.NewError(http.StatusConflict, "saved_search_limit", "You have 50 saved searches. Remove one first.")
	}
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, out)
	return nil
}

// DELETE /search/saved/{id} — yours only.
func (s *Service) deleteSaved(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	tag, err := s.d.Pool.Exec(r.Context(), `DELETE FROM search_saved WHERE id = $1 AND user_id = $2`, id, auth.MustFromContext(r.Context()).UserID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.NotFound("That saved search doesn't exist.")
	}
	httpx.NoContent(w)
	return nil
}

// DeleteAccount removes the person's saved searches.
func (s *Service) DeleteAccount(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	_, err := tx.Exec(ctx, `DELETE FROM search_saved WHERE user_id = $1`, userID)
	return err
}

// ExportAccount returns the person's saved searches.
func (s *Service) ExportAccount(ctx context.Context, userID uuid.UUID) (any, error) {
	items, err := s.savedFor(ctx, userID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"saved_searches": items}, nil
}
