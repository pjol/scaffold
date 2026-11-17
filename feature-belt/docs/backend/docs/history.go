package docs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/bg"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/moderation"
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

// snapshot saves the page as it is now, inside q's transaction, and returns
// who changed it since the previous copy.
func snapshot(ctx context.Context, q db.Querier, pageID uuid.UUID, reason string) ([]uuid.UUID, error) {
	var title string
	var icon *string
	var version int64
	if err := q.QueryRow(ctx, `SELECT title, icon, version FROM docs_pages WHERE id = $1`, pageID).Scan(&title, &icon, &version); err != nil {
		return nil, err
	}
	blocks, err := loadBlocks(ctx, q, pageID)
	if err != nil {
		return nil, err
	}
	out := make([]snapBlock, len(blocks))
	for i, b := range blocks {
		out[i] = snapBlock{ID: b.ID, ParentID: b.ParentID, Type: b.Type, Props: b.Props, Position: b.Position}
	}
	raw, _ := json.Marshal(out)
	var editors []uuid.UUID
	err = q.QueryRow(ctx, `
		INSERT INTO docs_snapshots (page_id, version, title, icon, blocks, editors, reason)
		VALUES ($1, $2, $3, $4, $5, COALESCE((
		    SELECT array_agg(DISTINCT e) FROM (
		        SELECT b.updated_by AS e FROM docs_blocks b WHERE b.page_id = $1 AND b.updated_by IS NOT NULL
		          AND b.version > COALESCE((SELECT max(s.version) FROM docs_snapshots s WHERE s.page_id = $1), 0)
		        UNION SELECT p.updated_by FROM docs_pages p WHERE p.id = $1 AND p.updated_by IS NOT NULL) x), '{}'), $6)
		RETURNING editors`,
		pageID, version, title, icon, raw, reason).Scan(&editors)
	return editors, err
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
		editors, err := snapshot(ctx, s.d.Pool, id, "auto")
		if err != nil {
			slogWarn(ctx, "docs: snapshot", err)
			continue
		}
		if len(editors) == 0 {
			continue
		}
		summary := ""
		if sum, err := s.lastChange(ctx, id); err == nil {
			if sum.Empty() {
				continue // touched but nothing a reader would notice
			}
			summary = sum.Text()
		}
		pageID, eds := id, editors
		bg.Go(ctx, "docs.notify_edits", func(ctx context.Context) error {
			s.notifyEdits(ctx, pageID, eds, summary)
			return nil
		})
	}
	return nil
}

// lastChange compares a page's newest saved version with the one before it.
func (s *Service) lastChange(ctx context.Context, pageID uuid.UUID) (DiffSummary, error) {
	rows, err := s.d.Pool.Query(ctx, `SELECT title, blocks FROM docs_snapshots WHERE page_id = $1 ORDER BY created_at DESC, id DESC LIMIT 2`, pageID)
	if err != nil {
		return DiffSummary{}, err
	}
	defer rows.Close()
	var titles []string
	var lists [][]snapBlock
	for rows.Next() {
		var t string
		var raw []byte
		if err := rows.Scan(&t, &raw); err != nil {
			return DiffSummary{}, err
		}
		var bl []snapBlock
		_ = json.Unmarshal(raw, &bl)
		titles, lists = append(titles, t), append(lists, bl)
	}
	if err := rows.Err(); err != nil {
		return DiffSummary{}, err
	}
	if len(lists) == 0 {
		return DiffSummary{}, nil
	}
	if len(lists) == 1 {
		_, sum := diffBlocks(nil, lists[0])
		return sum, nil
	}
	_, sum := diffBlocks(lists[1], lists[0])
	sum.TitleChanged = titles[0] != titles[1]
	return sum, nil
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
	if err := s.cleanupPages(ctx); err != nil {
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
		if _, err := snapshot(ctx, tx, p.ID, "before_restore"); err != nil {
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

// ---- diff and updates ------------------------------------------------------------

// VersionDiff is GET /docs/history/{snapshotID}/diff.
type VersionDiff struct {
	Against   string      `json:"against"` // previous | current
	FromTitle string      `json:"from_title"`
	ToTitle   string      `json:"to_title"`
	FromAt    *time.Time  `json:"from_at"` // nil: before the first saved version
	ToAt      *time.Time  `json:"to_at"`   // nil: the page now
	Entries   []DiffEntry `json:"entries"`
	Summary   DiffSummary `json:"summary"`
	Refs      Refs        `json:"refs"`
}

// diffSnapshot: GET /docs/history/{snapshotID}/diff?against=previous|current
// (view): what this version changed since the one before it, or what has
// changed since this version (current).
func (s *Service) diffSnapshot(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "snapshotID")
	if err != nil {
		return err
	}
	against := r.URL.Query().Get("against")
	if against == "" {
		against = "previous"
	}
	if against != "previous" && against != "current" {
		return httpx.BadRequest("against must be previous or current.")
	}
	out, err := s.versionDiff(ctx, me, id, against)
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

func (s *Service) versionDiff(ctx context.Context, me, id uuid.UUID, against string) (*VersionDiff, error) {
	sn, blocks, p, err := s.loadSnapshot(ctx, me, id, access.View)
	if err != nil {
		return nil, err
	}
	out := &VersionDiff{Against: against}
	var older, newer []snapBlock
	if against == "current" {
		cur, err := loadBlocks(ctx, s.d.Pool, p.ID)
		if err != nil {
			return nil, err
		}
		older = blocks
		newer = make([]snapBlock, len(cur))
		for i, b := range cur {
			newer[i] = snapBlock{ID: b.ID, ParentID: b.ParentID, Type: b.Type, Props: b.Props, Position: b.Position}
		}
		at := sn.CreatedAt
		out.FromAt, out.FromTitle, out.ToTitle = &at, displayTitle(sn.Title), displayTitle(p.Title)
	} else {
		var prevTitle string
		var prevAt time.Time
		var raw []byte
		err := s.d.Pool.QueryRow(ctx, `SELECT title, created_at, blocks FROM docs_snapshots
			WHERE page_id = $1 AND (created_at, id) < ($2, $3) ORDER BY created_at DESC, id DESC LIMIT 1`, p.ID, sn.CreatedAt, sn.ID).
			Scan(&prevTitle, &prevAt, &raw)
		switch {
		case db.IsNoRows(err):
			out.FromTitle = displayTitle(sn.Title)
		case err != nil:
			return nil, err
		default:
			_ = json.Unmarshal(raw, &older)
			out.FromAt, out.FromTitle = &prevAt, displayTitle(prevTitle)
		}
		newer = blocks
		at := sn.CreatedAt
		out.ToAt, out.ToTitle = &at, displayTitle(sn.Title)
	}
	out.Entries, out.Summary = diffBlocks(older, newer)
	out.Summary.TitleChanged = out.FromAt != nil && out.FromTitle != out.ToTitle
	var users, pages []uuid.UUID
	for i := range out.Entries {
		out.Entries[i].Block.PageID = p.ID
		u, pg := blockRefs(out.Entries[i].Block)
		users, pages = append(users, u...), append(pages, pg...)
	}
	if out.Refs, err = s.resolveRefs(ctx, me, true, users, pages); err != nil {
		return nil, err
	}
	return out, nil
}

// Update is one entry of a page's Updates feed: a saved version (who edited,
// what changed) or a new comment thread.
type Update struct {
	Type       string       `json:"type"` // edit | comment
	At         time.Time    `json:"at"`
	People     []RefUser    `json:"people"`
	SnapshotID *uuid.UUID   `json:"snapshot_id,omitempty"`
	Summary    *DiffSummary `json:"summary,omitempty"`
	Text       string       `json:"text"` // the summary sentence, or the comment's opening
	ThreadID   *uuid.UUID   `json:"thread_id,omitempty"`
	Quote      *string      `json:"quote,omitempty"`
}

type updatesCursor struct {
	T time.Time `json:"t"`
}

// updates: GET /docs/pages/{id}/updates?cursor= (view): who changed what and
// when, newest first: saved versions with what each changed, and comment
// threads (hidden ones and those of people blocked either way left out).
func (s *Service) updates(w http.ResponseWriter, r *http.Request) error {
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
	var c updatesCursor
	if err := httpx.DecodeCursor(pp.Cursor, &c); err != nil {
		return err
	}
	list, err := s.pageUpdates(ctx, me, id, nullTime(c.T), pp.Limit+1)
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(list, pp.Limit, func(u Update) any { return updatesCursor{T: u.At} }))
	return nil
}

// pageUpdates merges saved versions and comment threads older than before
// (nil: now), newest first, at most limit.
func (s *Service) pageUpdates(ctx context.Context, me, pageID uuid.UUID, before *time.Time, limit int) ([]Update, error) {
	// One extra version, so the oldest listed still has the one before it.
	rows, err := s.d.Pool.Query(ctx, `
		SELECT s.id, s.title, s.created_at, s.blocks, s.reason,
		       COALESCE((SELECT jsonb_agg(jsonb_build_object('id', u.id, 'name', u.display_name, 'avatar_url', u.avatar_url))
		                 FROM users u WHERE u.id = ANY (s.editors) AND u.status <> 'deleted'), '[]')
		FROM docs_snapshots s
		WHERE s.page_id = $1 AND ($2::timestamptz IS NULL OR s.created_at < $2)
		ORDER BY s.created_at DESC, s.id DESC LIMIT $3`, pageID, before, limit+1)
	if err != nil {
		return nil, err
	}
	type ver struct {
		id     uuid.UUID
		title  string
		at     time.Time
		blocks []snapBlock
		reason string
		people []RefUser
	}
	var vers []ver
	for rows.Next() {
		var v ver
		var raw, people []byte
		if err := rows.Scan(&v.id, &v.title, &v.at, &raw, &v.reason, &people); err != nil {
			rows.Close()
			return nil, err
		}
		_ = json.Unmarshal(raw, &v.blocks)
		_ = json.Unmarshal(people, &v.people)
		if v.people == nil {
			v.people = []RefUser{}
		}
		vers = append(vers, v)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := []Update{}
	for i, v := range vers {
		if i == limit {
			break
		}
		var older []snapBlock
		olderTitle := v.title
		hasOlder := i+1 < len(vers)
		if hasOlder {
			older, olderTitle = vers[i+1].blocks, vers[i+1].title
		}
		_, sum := diffBlocks(older, v.blocks)
		sum.TitleChanged = hasOlder && olderTitle != v.title
		if v.reason == "before_restore" || (hasOlder && sum.Empty()) {
			continue
		}
		sid := v.id
		text := sum.Text()
		if !hasOlder && i+1 == len(vers) && len(vers) <= limit {
			text = "Created the page"
		}
		out = append(out, Update{Type: "edit", At: v.at, People: v.people, SnapshotID: &sid, Summary: &sum, Text: text})
	}
	blocked, err := moderation.BlockedIDs(ctx, s.d.Pool, me)
	if err != nil {
		return nil, err
	}
	rows, err = s.d.Pool.Query(ctx, `
		SELECT c.id, c.created_at, c.plain, c.quote, u.id, u.display_name, u.avatar_url
		FROM docs_comments c LEFT JOIN users u ON u.id = c.author_id AND u.status <> 'deleted'
		WHERE c.page_id = $1 AND c.thread_id IS NULL AND c.hidden_at IS NULL AND c.deleted_at IS NULL
		  AND (c.author_id IS NULL OR c.author_id <> ALL ($2)) AND ($3::timestamptz IS NULL OR c.created_at < $3)
		ORDER BY c.created_at DESC, c.id DESC LIMIT $4`, pageID, blocked, before, limit)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var u Update
		var tid uuid.UUID
		var plain string
		var uid *uuid.UUID
		var name, avatar *string
		if err := rows.Scan(&tid, &u.At, &plain, &u.Quote, &uid, &name, &avatar); err != nil {
			rows.Close()
			return nil, err
		}
		u.Type, u.ThreadID, u.Text, u.People = "comment", &tid, snippet(plain, 140), []RefUser{}
		if uid != nil && name != nil {
			u.People = append(u.People, RefUser{ID: *uid, Name: *name, AvatarURL: avatar})
		}
		out = append(out, u)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	slices.SortStableFunc(out, func(a, b Update) int { return b.At.Compare(a.At) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
