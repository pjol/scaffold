package docs

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

const (
	maxDuplicatePages  = 200
	maxDuplicateBlocks = 20000
)

// duplicatePage: POST /docs/pages/{id}/duplicate. The copy (with subpages,
// a database's views and rows, and files) goes right after the original;
// it needs edit on the parent (or, at the top level, becomes a private page
// of yours). You own the copies.
func (s *Service) duplicatePage(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	src, err := requirePage(ctx, s.d.Pool, me, id, access.View)
	if err != nil {
		return err
	}
	if src.DeletedAt != nil {
		return errInTrash
	}
	if src.ParentID != nil {
		parent, err := requirePage(ctx, s.d.Pool, me, *src.ParentID, access.Edit)
		if err != nil {
			return err
		}
		if err := editable(parent, src.IsRow); err != nil {
			return err
		}
	}
	var newID uuid.UUID
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		pos, err := siblingPosition(ctx, tx, src.OrgID, src.ParentID, &src.ID)
		if err != nil {
			return err
		}
		budget := &dupBudget{pages: maxDuplicatePages, blocks: maxDuplicateBlocks}
		newID, err = s.copyTree(ctx, tx, me, src.ID, src.ParentID, pos, budget)
		return err
	})
	if err != nil {
		return err
	}
	out, err := loadPageWithLevel(ctx, s, me, newID)
	if err != nil {
		return err
	}
	if out.IsRow && out.ParentID != nil {
		s.publish(ctx, pageTopic(*out.ParentID), EventRows, map[string]any{"database_id": *out.ParentID, "row_id": out.ID})
	} else {
		s.treeChanged(ctx, out.OrgID)
	}
	httpx.JSON(w, http.StatusCreated, out)
	return nil
}

type dupBudget struct{ pages, blocks int }

var errTooBig = httpx.NewError(http.StatusConflict, "too_large", "That page has too many subpages or blocks to copy at once. Copy a smaller part.")

// copyTree copies page src (and its live subpages and rows) under parent.
func (s *Service) copyTree(ctx context.Context, tx pgx.Tx, me, src uuid.UUID, parent *uuid.UUID, pos string, budget *dupBudget) (uuid.UUID, error) {
	budget.pages--
	if budget.pages < 0 {
		return uuid.Nil, errTooBig
	}
	p, err := loadPage(ctx, tx, src, false)
	if err != nil {
		return uuid.Nil, err
	}
	newID := uuid.New()
	schema := jsonOrNil(nil)
	if p.Schema != nil {
		schema = jsonOrNil(p.Schema)
	}
	props, _ := json.Marshal(p.Props)
	if _, err := tx.Exec(ctx, `
		INSERT INTO docs_pages (id, org_id, parent_id, kind, is_row, inline, title, icon, position, full_width, props, schema, version, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, 1, $13, $13)`,
		newID, p.OrgID, parent, p.Kind, p.IsRow, p.Inline, p.Title, p.Icon, pos, p.FullWidth, props, schema, me); err != nil {
		return uuid.Nil, err
	}
	res := access.NewResource{Kind: Kind, ID: newID, OrgID: p.OrgID, OwnerID: me}
	if parent != nil {
		res.Parent = &access.Ref{Kind: Kind, ID: *parent}
	}
	if err := access.CreateResource(ctx, tx, res); err != nil {
		return uuid.Nil, err
	}
	// Files get their own rows on the copy (same objects).
	fileMap := map[string]string{}
	rows, err := tx.Query(ctx, `SELECT id FROM docs_files WHERE page_id = $1`, src)
	if err != nil {
		return uuid.Nil, err
	}
	fileIDs, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return uuid.Nil, err
	}
	for _, f := range fileIDs {
		var nf uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO docs_files (org_id, page_id, key, name, content_type, size, uploaded_by)
			SELECT org_id, $2, key, name, content_type, size, uploaded_by FROM docs_files WHERE id = $1 RETURNING id`, f, newID).Scan(&nf); err != nil {
			return uuid.Nil, err
		}
		fileMap[f.String()] = nf.String()
	}
	if p.Cover != nil && p.Cover.FileID != nil {
		c := *p.Cover
		if nf, ok := fileMap[c.FileID.String()]; ok {
			id := uuid.MustParse(nf)
			c.FileID = &id
		} else {
			c.FileID = nil
			c.Color = "neutral"
		}
		if _, err := tx.Exec(ctx, `UPDATE docs_pages SET cover = $2 WHERE id = $1`, newID, jsonOrNil(c)); err != nil {
			return uuid.Nil, err
		}
	}
	if p.Kind == "database" {
		if _, err := tx.Exec(ctx, `INSERT INTO docs_views (database_id, name, type, position, config, created_by)
			SELECT $2, name, type, position, config, $3 FROM docs_views WHERE database_id = $1`, src, newID, me); err != nil {
			return uuid.Nil, err
		}
	}
	// Subpages and rows first, so blocks pointing at them can be re-aimed.
	pageMap := map[string]string{}
	rows, err = tx.Query(ctx, `SELECT id, position FROM docs_pages WHERE parent_id = $1 AND deleted_at IS NULL ORDER BY position, id`, src)
	if err != nil {
		return uuid.Nil, err
	}
	type kid struct {
		ID       uuid.UUID
		Position string
	}
	kids, err := pgx.CollectRows(rows, pgx.RowToStructByPos[kid])
	if err != nil {
		return uuid.Nil, err
	}
	for _, k := range kids {
		pid := newID
		cid, err := s.copyTree(ctx, tx, me, k.ID, &pid, k.Position, budget)
		if err != nil {
			return uuid.Nil, err
		}
		pageMap[k.ID.String()] = cid.String()
	}
	blocks, err := loadBlocks(ctx, tx, src)
	if err != nil {
		return uuid.Nil, err
	}
	budget.blocks -= len(blocks)
	if budget.blocks < 0 {
		return uuid.Nil, errTooBig
	}
	blockMap := map[uuid.UUID]uuid.UUID{}
	for _, b := range blocks {
		blockMap[b.ID] = uuid.New()
	}
	for _, b := range blocks {
		var parentID *uuid.UUID
		if b.ParentID != nil {
			if np, ok := blockMap[*b.ParentID]; ok {
				parentID = &np
			}
		}
		props := b.Props
		for key, m := range map[string]map[string]string{"file_id": fileMap, "page_id": pageMap, "database_id": pageMap} {
			if v := propString(props, key); v != "" {
				if nv, ok := m[v]; ok {
					props[key] = nv
				}
			}
		}
		raw, _ := json.Marshal(props)
		plain := ""
		_ = tx.QueryRow(ctx, `SELECT plain FROM docs_blocks WHERE id = $1`, b.ID).Scan(&plain)
		if _, err := tx.Exec(ctx, `
			INSERT INTO docs_blocks (id, page_id, parent_id, type, props, position, plain, version, created_by, updated_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, 1, $8, $8)`, blockMap[b.ID], newID, parentID, b.Type, raw, b.Position, plain, me); err != nil {
			return uuid.Nil, err
		}
		_, pages := blockRefs(Block{Type: b.Type, Props: props})
		if err := relinkBlock(ctx, tx, newID, p.OrgID, blockMap[b.ID], pages); err != nil {
			return uuid.Nil, err
		}
	}
	return newID, nil
}
