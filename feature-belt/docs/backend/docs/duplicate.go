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

// dupBudget bounds a copy and says where it goes: org is the destination
// workspace (uuid.Nil: the source's), and token, when set, copies only what
// that public link opens instead of what the caller can open.
type dupBudget struct {
	pages, blocks int
	org           uuid.UUID
	token         string
}

var errTooBig = httpx.NewError(http.StatusConflict, "too_large", "That page has too many subpages or blocks to copy at once. Copy a smaller part.")

// copyTree copies page src (and the live subpages and rows the caller can
// open) under parent. The copies are the caller's.
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
	org := p.OrgID
	if budget.org != uuid.Nil {
		org = budget.org
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO docs_pages (id, org_id, parent_id, kind, is_row, inline, title, icon, position, full_width, props, schema, version, created_by, updated_by, font, small_text, is_template)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, '{}', NULL, 1, $11, $11, $12, $13, $14)`,
		newID, org, parent, p.Kind, p.IsRow, p.Inline, p.Title, p.Icon, pos, p.FullWidth, me, p.Font, p.SmallText,
		p.IsRow && p.IsTemplate); err != nil { // a database's row templates stay row templates
		return uuid.Nil, err
	}
	res := access.NewResource{Kind: Kind, ID: newID, OrgID: org, OwnerID: me}
	if parent != nil {
		res.Parent = &access.Ref{Kind: Kind, ID: *parent}
	}
	if err := access.CreateResource(ctx, tx, res); err != nil {
		return uuid.Nil, err
	}
	if err := s.copyContent(ctx, tx, me, p, newID, budget); err != nil {
		return uuid.Nil, err
	}
	return newID, nil
}

// copyInto fills an existing page dst (just created, same kind) with page
// src's content: chrome, a database's schema and views, files, the
// subpages and rows the caller can open, and blocks (budget counted).
func (s *Service) copyInto(ctx context.Context, tx pgx.Tx, me, src, dst uuid.UUID, budget *dupBudget) error {
	p, err := loadPage(ctx, tx, src, false)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE docs_pages SET full_width = $2, font = $3, small_text = $4 WHERE id = $1`, dst, p.FullWidth, p.Font, p.SmallText); err != nil {
		return err
	}
	if p.Kind == "database" {
		if _, err := tx.Exec(ctx, `DELETE FROM docs_views WHERE database_id = $1`, dst); err != nil {
			return err
		}
	}
	return s.copyContent(ctx, tx, me, p, dst, budget)
}

// copyContent copies p's row values and schema, files (new rows, same
// objects), cover and custom icon, views, visible children and blocks onto
// newID.
func (s *Service) copyContent(ctx context.Context, tx pgx.Tx, me uuid.UUID, p *Page, newID uuid.UUID, budget *dupBudget) error {
	src := p.ID
	props, _ := json.Marshal(p.Props)
	var schema []byte
	if p.Schema != nil {
		schema = jsonOrNil(p.Schema)
	}
	if _, err := tx.Exec(ctx, `UPDATE docs_pages SET props = $2, schema = COALESCE($3, schema) WHERE id = $1`, newID, props, schema); err != nil {
		return err
	}
	// Files get their own rows on the copy (same objects).
	fileMap := map[string]string{}
	rows, err := tx.Query(ctx, `SELECT id FROM docs_files WHERE page_id = $1`, src)
	if err != nil {
		return err
	}
	fileIDs, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return err
	}
	for _, f := range fileIDs {
		var nf uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO docs_files (org_id, page_id, key, name, content_type, size, uploaded_by)
			SELECT (SELECT org_id FROM docs_pages WHERE id = $2), $2, key, name, content_type, size, uploaded_by FROM docs_files WHERE id = $1 RETURNING id`, f, newID).Scan(&nf); err != nil {
			return err
		}
		fileMap[f.String()] = nf.String()
	}
	if p.IsRow && p.ParentID != nil {
		// Fresh unique IDs, files values pointing at the copies, mirrors (databases.go).
		if err := s.afterRowCopy(ctx, tx, newID, fileMap); err != nil {
			return err
		}
	}
	if p.Cover != nil {
		c := *p.Cover
		if c.FileID != nil {
			if nf, ok := fileMap[c.FileID.String()]; ok {
				id := uuid.MustParse(nf)
				c.FileID = &id
			} else {
				c.FileID = nil
				c.Color = "neutral"
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE docs_pages SET cover = $2 WHERE id = $1`, newID, jsonOrNil(c)); err != nil {
			return err
		}
	}
	if p.CustomIcon != nil {
		ci := *p.CustomIcon
		keep := true
		if ci.FileID != nil {
			if nf, ok := fileMap[ci.FileID.String()]; ok {
				id := uuid.MustParse(nf)
				ci.FileID = &id
			} else {
				keep = false
			}
		}
		if keep {
			if _, err := tx.Exec(ctx, `UPDATE docs_pages SET custom_icon = $2, icon = NULL WHERE id = $1`, newID, jsonOrNil(ci)); err != nil {
				return err
			}
		}
	}
	if p.Kind == "database" {
		if _, err := tx.Exec(ctx, `INSERT INTO docs_views (database_id, name, type, position, config, created_by)
			SELECT $2, name, type, position, config, $3 FROM docs_views WHERE database_id = $1`, src, newID, me); err != nil {
			return err
		}
	}
	// Subpages and rows first, so blocks pointing at them can be re-aimed.
	// Only those the caller can open: a restricted subpage stays behind.
	pageMap := map[string]string{}
	if budget.token != "" {
		rows, err = tx.Query(ctx, `SELECT id, position FROM docs_pages WHERE parent_id = $1 AND deleted_at IS NULL AND public_blocked_at IS NULL
			ORDER BY position, id`, src)
	} else {
		rows, err = tx.Query(ctx, `SELECT id, position FROM docs_pages WHERE parent_id = $1 AND deleted_at IS NULL
			AND access_level($2, 'docs.page', id, $3) >= 1 ORDER BY position, id`, src, me, access.ViaAgent(ctx))
	}
	if err != nil {
		return err
	}
	type kid struct {
		ID       uuid.UUID
		Position string
	}
	kids, err := pgx.CollectRows(rows, pgx.RowToStructByPos[kid])
	if err != nil {
		return err
	}
	for _, k := range kids {
		if budget.token != "" {
			if lvl, err := access.LinkLevel(ctx, tx, budget.token, Kind, k.ID); err != nil || lvl < access.View {
				continue
			}
		}
		pid := newID
		cid, err := s.copyTree(ctx, tx, me, k.ID, &pid, k.Position, budget)
		if err != nil {
			return err
		}
		pageMap[k.ID.String()] = cid.String()
	}
	blocks, err := loadBlocks(ctx, tx, src)
	if err != nil {
		return err
	}
	budget.blocks -= len(blocks)
	if budget.blocks < 0 {
		return errTooBig
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
		skip := false
		for key, m := range map[string]map[string]string{"file_id": fileMap, "page_id": pageMap, "database_id": pageMap} {
			if v := propString(props, key); v != "" {
				if nv, ok := m[v]; ok {
					props[key] = nv
				} else if (b.Type == "page" || b.Type == "database") && key != "file_id" {
					// A subpage left behind (restricted or trashed) keeps no link in the copy.
					if kidOf(ctx, tx, v, src) {
						skip = true
					}
				}
			}
		}
		if skip {
			continue
		}
		raw, _ := json.Marshal(props)
		plain := ""
		_ = tx.QueryRow(ctx, `SELECT plain FROM docs_blocks WHERE id = $1`, b.ID).Scan(&plain)
		if _, err := tx.Exec(ctx, `
			INSERT INTO docs_blocks (id, page_id, parent_id, type, props, position, plain, version, created_by, updated_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, 1, $8, $8)`, blockMap[b.ID], newID, parentID, b.Type, raw, b.Position, plain, me); err != nil {
			return err
		}
		_, pages := blockRefs(Block{Type: b.Type, Props: props})
		org := p.OrgID
		if budget.org != uuid.Nil {
			org = budget.org
		}
		if err := relinkBlock(ctx, tx, newID, org, blockMap[b.ID], pages); err != nil {
			return err
		}
	}
	return nil
}

// kidOf reports whether page id is a child of parent (a sub-page block of
// a page that wasn't copied).
func kidOf(ctx context.Context, tx pgx.Tx, id string, parent uuid.UUID) bool {
	var ok bool
	_ = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM docs_pages WHERE id = $1::uuid AND parent_id = $2)`, id, parent).Scan(&ok)
	return ok
}
