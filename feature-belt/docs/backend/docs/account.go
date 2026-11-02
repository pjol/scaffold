package docs

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
)

// DeleteAccount removes the person's private pages (top-level pages they
// own where nothing in the tree is shared), clears their comments' text,
// their favorites, visits and presence, the files they uploaded (core
// deletes the objects under their storage prefix after the commit) and
// their name from attribution columns. Shared pages stay with their org:
// with the owner gone, org owners and admins manage them.
func (s *Service) DeleteAccount(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	// Connected docs keep only per-person memory (editing sessions, caches).
	forgetConnectedUser(userID)
	rows, err := tx.Query(ctx, `
		WITH RECURSIVE roots AS (
			SELECT p.id FROM docs_pages p JOIN access_resources ar ON ar.kind = 'docs.page' AND ar.id = p.id
			WHERE p.parent_id IS NULL AND ar.owner_id = $1
		), tree (root, id, depth) AS (
			SELECT id, id, 0 FROM roots
			UNION ALL
			SELECT t.root, c.id, t.depth + 1 FROM tree t JOIN docs_pages c ON c.parent_id = t.id WHERE t.depth < 64
		)
		SELECT root FROM tree GROUP BY root
		HAVING bool_and(NOT EXISTS (SELECT 1 FROM access_grants g WHERE g.resource_kind = 'docs.page' AND g.resource_id = tree.id))`, userID)
	if err != nil {
		return err
	}
	private, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return err
	}
	for _, id := range private {
		if err := access.DeleteResource(ctx, tx, access.Ref{Kind: Kind, ID: id}); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM docs_pages WHERE id = ANY ($1)`, private); err != nil {
		return err
	}
	stmts := []string{
		`UPDATE docs_comments SET body = '[]', plain = '', deleted_at = COALESCE(deleted_at, now()), updated_at = now() WHERE author_id = $1`,
		`UPDATE docs_comments SET resolved_by = NULL WHERE resolved_by = $1`,
		`DELETE FROM docs_favorites WHERE user_id = $1`,
		`DELETE FROM docs_visits WHERE user_id = $1`,
		`DELETE FROM docs_presence WHERE user_id = $1`,
		`DELETE FROM docs_files WHERE uploaded_by = $1`,
		`UPDATE docs_pages SET created_by = NULL WHERE created_by = $1`,
		`UPDATE docs_pages SET updated_by = NULL WHERE updated_by = $1`,
		`UPDATE docs_pages SET deleted_by = NULL WHERE deleted_by = $1`,
		`UPDATE docs_blocks SET created_by = NULL WHERE created_by = $1`,
		`UPDATE docs_blocks SET updated_by = NULL WHERE updated_by = $1`,
		`UPDATE docs_views SET created_by = NULL WHERE created_by = $1`,
		`UPDATE docs_snapshots SET editors = array_remove(editors, $1) WHERE $1 = ANY (editors)`,
	}
	for _, q := range stmts {
		if _, err := tx.Exec(ctx, q, userID); err != nil {
			return err
		}
	}
	return nil
}

type exportPage struct {
	ID        uuid.UUID `json:"id"`
	OrgID     uuid.UUID `json:"org_id"`
	Title     string    `json:"title"`
	Kind      string    `json:"kind"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Markdown  string    `json:"markdown"`
}

type exportComment struct {
	ID        uuid.UUID `json:"id"`
	PageID    uuid.UUID `json:"page_id"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

// ExportAccount returns the pages the person created (with their content as
// Markdown, up to 500), their comments and their favorites.
func (s *Service) ExportAccount(ctx context.Context, userID uuid.UUID) (any, error) {
	rows, err := s.d.Pool.Query(ctx, `SELECT `+pageCols+` FROM docs_pages p WHERE p.created_by = $1 ORDER BY p.created_at LIMIT 500`, userID)
	if err != nil {
		return nil, err
	}
	var pages []*Page
	for rows.Next() {
		p, err := scanPage(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		pages = append(pages, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := struct {
		Pages     []exportPage    `json:"pages"`
		Comments  []exportComment `json:"comments"`
		Favorites []uuid.UUID     `json:"favorites"`
	}{Pages: []exportPage{}, Comments: []exportComment{}, Favorites: []uuid.UUID{}}
	for _, p := range pages {
		blocks, err := loadBlocks(ctx, s.d.Pool, p.ID)
		if err != nil {
			return nil, err
		}
		var users, refs []uuid.UUID
		for _, b := range blocks {
			u, pg := blockRefs(b)
			users, refs = append(users, u...), append(refs, pg...)
		}
		r, err := s.resolveRefs(ctx, userID, true, users, refs)
		if err != nil {
			return nil, err
		}
		md := ""
		if len(blocks) > 0 {
			md = blocksToMarkdown(blocks, mdOptions{names: namesFor(r)})
		}
		out.Pages = append(out.Pages, exportPage{ID: p.ID, OrgID: p.OrgID, Title: p.Title, Kind: p.Kind, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt, Markdown: md})
	}
	crow, err := s.d.Pool.Query(ctx, `SELECT id, page_id, plain, created_at FROM docs_comments WHERE author_id = $1 AND deleted_at IS NULL ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	for crow.Next() {
		var c exportComment
		if err := crow.Scan(&c.ID, &c.PageID, &c.Text, &c.CreatedAt); err != nil {
			crow.Close()
			return nil, err
		}
		out.Comments = append(out.Comments, c)
	}
	crow.Close()
	if err := crow.Err(); err != nil {
		return nil, err
	}
	frow, err := s.d.Pool.Query(ctx, `SELECT page_id FROM docs_favorites WHERE user_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	if out.Favorites, err = pgx.CollectRows(frow, pgx.RowTo[uuid.UUID]); err != nil {
		return nil, err
	}
	if out.Favorites == nil {
		out.Favorites = []uuid.UUID{}
	}
	return out, nil
}
