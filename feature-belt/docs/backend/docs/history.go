package docs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// Snapshot is a saved copy of a page. Blocks are filled only when one
// snapshot is fetched.
type Snapshot struct {
	ID        uuid.UUID `json:"id"`
	PageID    uuid.UUID `json:"page_id"`
	Version   int64     `json:"version"`
	Title     string    `json:"title"`
	Icon      *string   `json:"icon"`
	Editors   []RefUser `json:"editors"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
	Blocks    []Block   `json:"blocks,omitempty"`
}

// snapBlock is a block as a snapshot stores it.
type snapBlock struct {
	ID       uuid.UUID      `json:"id"`
	ParentID *uuid.UUID     `json:"parent_id"`
	Type     string         `json:"type"`
	Props    map[string]any `json:"props"`
	Position string         `json:"position"`
}

// snapshot saves the page as it is now, inside q's transaction.
func snapshot(ctx context.Context, q db.Querier, pageID uuid.UUID, reason string) error {
	var title string
	var icon *string
	var version int64
	if err := q.QueryRow(ctx, `SELECT title, icon, version FROM docs_pages WHERE id = $1`, pageID).Scan(&title, &icon, &version); err != nil {
		return err
	}
	blocks, err := loadBlocks(ctx, q, pageID)
	if err != nil {
		return err
	}
	out := make([]snapBlock, len(blocks))
	for i, b := range blocks {
		out[i] = snapBlock{ID: b.ID, ParentID: b.ParentID, Type: b.Type, Props: b.Props, Position: b.Position}
	}
	raw, _ := json.Marshal(out)
	_, err = q.Exec(ctx, `
		INSERT INTO docs_snapshots (page_id, version, title, icon, blocks, editors, reason)
		VALUES ($1, $2, $3, $4, $5, COALESCE((
		    SELECT array_agg(DISTINCT e) FROM (
		        SELECT b.updated_by AS e FROM docs_blocks b WHERE b.page_id = $1 AND b.updated_by IS NOT NULL
		          AND b.version > COALESCE((SELECT max(s.version) FROM docs_snapshots s WHERE s.page_id = $1), 0)
		        UNION SELECT p.updated_by FROM docs_pages p WHERE p.id = $1 AND p.updated_by IS NOT NULL) x), '{}'), $6)`,
		pageID, version, title, icon, raw, reason)
	return err
}

// takeSnapshots (every minute, one instance) copies pages that changed
// since their last copy once they have been idle for 2 minutes, or every
// DOCS_SNAPSHOT_INTERVAL while someone keeps editing.
func (s *Service) takeSnapshots(ctx context.Context) error {
	rows, err := s.d.Pool.Query(ctx, `
		SELECT p.id FROM docs_pages p
		LEFT JOIN LATERAL (SELECT created_at FROM docs_snapshots s WHERE s.page_id = p.id ORDER BY s.created_at DESC LIMIT 1) last ON true
		WHERE p.deleted_at IS NULL AND p.kind = 'page'
		  AND p.updated_at > COALESCE(last.created_at, '-infinity'::timestamptz)
		  AND (p.updated_at < now() - interval '2 minutes' OR COALESCE(last.created_at, p.created_at) < now() - $1::interval)
		ORDER BY p.updated_at LIMIT 100`, fmt.Sprintf("%d seconds", int(s.cfg.snapshotInterval.Seconds())))
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := snapshot(ctx, s.d.Pool, id, "auto"); err != nil {
			slogWarn(ctx, "docs: snapshot", err)
		}
	}
	return nil
}

// cleanup trims history, tombstones, presence and the trash.
func (s *Service) cleanup(ctx context.Context) error {
	if _, err := s.d.Pool.Exec(ctx, `
		DELETE FROM docs_snapshots s WHERE s.created_at < now() - $1::interval
		  AND s.id <> (SELECT x.id FROM docs_snapshots x WHERE x.page_id = s.page_id ORDER BY x.created_at DESC LIMIT 1)`,
		fmt.Sprintf("%d days", s.cfg.historyDays)); err != nil {
		return err
	}
	if _, err := s.d.Pool.Exec(ctx, `
		WITH gone AS (DELETE FROM docs_block_tombstones WHERE created_at < now() - interval '7 days' RETURNING page_id, version)
		UPDATE docs_pages p SET tomb_floor = g.v FROM (SELECT page_id, max(version) AS v FROM gone GROUP BY page_id) g
		WHERE p.id = g.page_id AND p.tomb_floor < g.v`); err != nil {
		return err
	}
	if _, err := s.d.Pool.Exec(ctx, `DELETE FROM docs_presence WHERE seen_at < now() - interval '5 minutes'`); err != nil {
		return err
	}
	for range 20 {
		rows, err := s.d.Pool.Query(ctx, `SELECT id FROM docs_pages WHERE trash_root_id = id AND deleted_at < now() - $1::interval LIMIT 50`,
			fmt.Sprintf("%d days", s.cfg.trashDays))
		if err != nil {
			return err
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		if err != nil || len(ids) == 0 {
			return err
		}
		keys, err := s.purge(ctx, ids)
		if err != nil {
			return err
		}
		s.deleteObjects(ctx, keys)
	}
	return nil
}

type historyCursor struct {
	T  time.Time `json:"t"`
	ID uuid.UUID `json:"id"`
}

// listHistory: GET /docs/pages/{id}/history?cursor= (view): newest first.
func (s *Service) listHistory(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	if _, err := access.Require(ctx, s.d.Pool, me, Kind, id, access.View); err != nil {
		return err
	}
	pp := httpx.ParsePage(r)
	var c historyCursor
	if err := httpx.DecodeCursor(pp.Cursor, &c); err != nil {
		return err
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT s.id, s.page_id, s.version, s.title, s.icon, s.reason, s.created_at,
		       COALESCE((SELECT jsonb_agg(jsonb_build_object('id', u.id, 'name', u.display_name, 'avatar_url', u.avatar_url))
		                 FROM users u WHERE u.id = ANY (s.editors) AND u.status <> 'deleted'), '[]')
		FROM docs_snapshots s
		WHERE s.page_id = $1 AND ($2::timestamptz IS NULL OR (s.created_at, s.id) < ($2, $3))
		ORDER BY s.created_at DESC, s.id DESC LIMIT $4`, id, nullTime(c.T), c.ID, pp.Limit+1)
	if err != nil {
		return err
	}
	defer rows.Close()
	list := []Snapshot{}
	for rows.Next() {
		var sn Snapshot
		var editors []byte
		if err := rows.Scan(&sn.ID, &sn.PageID, &sn.Version, &sn.Title, &sn.Icon, &sn.Reason, &sn.CreatedAt, &editors); err != nil {
			return err
		}
		_ = json.Unmarshal(editors, &sn.Editors)
		sn.Title = displayTitle(sn.Title)
		list = append(list, sn)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(list, pp.Limit, func(sn Snapshot) any { return historyCursor{T: sn.CreatedAt, ID: sn.ID} }))
	return nil
}

func (s *Service) loadSnapshot(ctx context.Context, me, id uuid.UUID, min access.Level) (*Snapshot, []snapBlock, *Page, error) {
	var sn Snapshot
	var raw []byte
	err := s.d.Pool.QueryRow(ctx, `SELECT id, page_id, version, title, icon, reason, created_at, blocks FROM docs_snapshots WHERE id = $1`, id).
		Scan(&sn.ID, &sn.PageID, &sn.Version, &sn.Title, &sn.Icon, &sn.Reason, &sn.CreatedAt, &raw)
	if db.IsNoRows(err) {
		return nil, nil, nil, httpx.NotFound("That version isn't saved any more.")
	}
	if err != nil {
		return nil, nil, nil, err
	}
	p, err := requirePage(ctx, s.d.Pool, me, sn.PageID, min)
	if err != nil {
		return nil, nil, nil, err
	}
	var blocks []snapBlock
	_ = json.Unmarshal(raw, &blocks)
	sn.Editors = []RefUser{}
	return &sn, blocks, p, nil
}

// getSnapshot: GET /docs/history/{snapshotID} (view on the page).
func (s *Service) getSnapshot(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "snapshotID")
	if err != nil {
		return err
	}
	sn, blocks, _, err := s.loadSnapshot(ctx, me, id, access.View)
	if err != nil {
		return err
	}
	sn.Blocks = make([]Block, len(blocks))
	var users, pages []uuid.UUID
	for i, b := range blocks {
		sn.Blocks[i] = Block{ID: b.ID, PageID: sn.PageID, ParentID: b.ParentID, Type: b.Type, Props: b.Props, Position: b.Position, Version: sn.Version, CreatedAt: sn.CreatedAt, UpdatedAt: sn.CreatedAt}
		u, p := blockRefs(sn.Blocks[i])
		users, pages = append(users, u...), append(pages, p...)
	}
	refs, err := s.resolveRefs(ctx, me, true, users, pages)
	if err != nil {
		return err
	}
	sn.Title = displayTitle(sn.Title)
	httpx.OK(w, map[string]any{"snapshot": sn, "refs": refs})
	return nil
}

// restoreSnapshot: POST /docs/history/{snapshotID}/restore (edit). The page
// as it is now is saved first, so a restore can be undone the same way.
func (s *Service) restoreSnapshot(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "snapshotID")
	if err != nil {
		return err
	}
	sn, blocks, p, err := s.loadSnapshot(ctx, me, id, access.Edit)
	if err != nil {
		return err
	}
	if err := editable(p, true); err != nil {
		return err
	}
	var version int64
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		cur, err := loadPage(ctx, tx, p.ID, true)
		if err != nil {
			return err
		}
		if err := snapshot(ctx, tx, p.ID, "before_restore"); err != nil {
			return err
		}
		version = cur.Version + 1
		keep := make([]uuid.UUID, len(blocks))
		for i, b := range blocks {
			keep[i] = b.ID
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO docs_block_tombstones (page_id, block_id, version)
			SELECT $1, id, $3 FROM docs_blocks WHERE page_id = $1 AND NOT (id = ANY ($2))
			ON CONFLICT (page_id, block_id) DO UPDATE SET version = EXCLUDED.version`, p.ID, keep, version); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM docs_blocks WHERE page_id = $1`, p.ID); err != nil {
			return err
		}
		pc := propCtx{ctx: ctx, q: tx, pageID: p.ID, orgID: p.OrgID}
		for _, b := range blocks {
			raw, _ := json.Marshal(b.Props)
			n, err := normalizeProps(pc, b.Type, propsMap(raw))
			if err != nil {
				// A block whose file or linked page is gone comes back as a paragraph.
				n = normalized{props: map[string]any{"text": textSpans("")}}
				b.Type = "paragraph"
			}
			props, _ := json.Marshal(n.props)
			if _, err := tx.Exec(ctx, `
				INSERT INTO docs_blocks (id, page_id, parent_id, type, props, position, plain, version, created_by, updated_by)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9) ON CONFLICT (id) DO NOTHING`,
				b.ID, p.ID, b.ParentID, b.Type, props, b.Position, n.plain, version, me); err != nil {
				return err
			}
			if err := relinkBlock(ctx, tx, p.ID, p.OrgID, b.ID, n.pages); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM docs_block_tombstones WHERE page_id = $1 AND block_id = ANY ($2)`, p.ID, keep); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE docs_pages SET title = $2, icon = $3, version = $4, updated_at = now(), updated_by = $5 WHERE id = $1`,
			p.ID, sn.Title, sn.Icon, version, me)
		return err
	})
	if err != nil {
		return err
	}
	s.publish(ctx, pageTopic(p.ID), EventReload, map[string]any{"page_id": p.ID, "version": version})
	s.treeChanged(ctx, p.OrgID)
	httpx.OK(w, map[string]any{"version": version})
	return nil
}
