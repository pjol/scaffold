package docs

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// deleteAccountPages (from DeleteAccount) removes what docs 1.2.0 keeps per
// person: import runs, follows, page views, reactions, teamspace choices,
// and their name on verifications and publish settings.
func (s *Service) deleteAccountPages(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	for _, q := range []string{
		`DELETE FROM docs_imports WHERE user_id = $1`,
		`DELETE FROM docs_exports WHERE user_id = $1`,
		`DELETE FROM docs_follows WHERE user_id = $1`,
		`DELETE FROM docs_page_views WHERE user_id = $1`,
		`DELETE FROM docs_comment_reactions WHERE user_id = $1`,
		`DELETE FROM docs_teamspace_prefs WHERE user_id = $1`,
		`UPDATE docs_pages SET verified_by = NULL, verified_at = NULL, verified_until = NULL WHERE verified_by = $1`,
		`UPDATE docs_publish_settings SET updated_by = NULL WHERE updated_by = $1`,
	} {
		if _, err := tx.Exec(ctx, q, userID); err != nil {
			return err
		}
	}
	return nil
}

type exportFollow struct {
	PageID    uuid.UUID `json:"page_id"`
	Following bool      `json:"following"`
}

type exportView struct {
	PageID uuid.UUID `json:"page_id"`
	Day    string    `json:"day"`
	Views  int       `json:"views"`
}

type exportReaction struct {
	CommentID uuid.UUID `json:"comment_id"`
	Emoji     string    `json:"emoji"`
	CreatedAt time.Time `json:"created_at"`
}

type exportImport struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// exportAccountPages (from ExportAccount) lists the person's follows, page
// views, reactions, import runs and hidden teamspaces.
func (s *Service) exportAccountPages(ctx context.Context, userID uuid.UUID) (any, error) {
	out := struct {
		Follows          []exportFollow   `json:"follows"`
		PageViews        []exportView     `json:"page_views"`
		Reactions        []exportReaction `json:"reactions"`
		Imports          []exportImport   `json:"imports"`
		HiddenTeamspaces []uuid.UUID      `json:"hidden_teamspaces"`
	}{Follows: []exportFollow{}, PageViews: []exportView{}, Reactions: []exportReaction{}, Imports: []exportImport{}, HiddenTeamspaces: []uuid.UUID{}}
	collect := func(sql string, scan func(pgx.Rows) error) error {
		rows, err := s.d.Pool.Query(ctx, sql, userID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			if err := scan(rows); err != nil {
				return err
			}
		}
		return rows.Err()
	}
	if err := collect(`SELECT page_id, following FROM docs_follows WHERE user_id = $1 ORDER BY created_at`, func(r pgx.Rows) error {
		var f exportFollow
		err := r.Scan(&f.PageID, &f.Following)
		out.Follows = append(out.Follows, f)
		return err
	}); err != nil {
		return nil, err
	}
	if err := collect(`SELECT page_id, to_char(day, 'YYYY-MM-DD'), views FROM docs_page_views WHERE user_id = $1 ORDER BY day, page_id`, func(r pgx.Rows) error {
		var v exportView
		err := r.Scan(&v.PageID, &v.Day, &v.Views)
		out.PageViews = append(out.PageViews, v)
		return err
	}); err != nil {
		return nil, err
	}
	if err := collect(`SELECT comment_id, emoji, created_at FROM docs_comment_reactions WHERE user_id = $1 ORDER BY created_at`, func(r pgx.Rows) error {
		var x exportReaction
		err := r.Scan(&x.CommentID, &x.Emoji, &x.CreatedAt)
		out.Reactions = append(out.Reactions, x)
		return err
	}); err != nil {
		return nil, err
	}
	if err := collect(`SELECT id, name, status, created_at FROM docs_imports WHERE user_id = $1 ORDER BY created_at`, func(r pgx.Rows) error {
		var x exportImport
		err := r.Scan(&x.ID, &x.Name, &x.Status, &x.CreatedAt)
		out.Imports = append(out.Imports, x)
		return err
	}); err != nil {
		return nil, err
	}
	if err := collect(`SELECT team_id FROM docs_teamspace_prefs WHERE user_id = $1 AND hidden`, func(r pgx.Rows) error {
		var id uuid.UUID
		err := r.Scan(&id)
		out.HiddenTeamspaces = append(out.HiddenTeamspaces, id)
		return err
	}); err != nil {
		return nil, err
	}
	return out, nil
}
