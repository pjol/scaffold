package docs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/bg"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// Op is one block operation. create needs id (client-minted), type,
// parent_id (null: top level), position and props; update patches props
// (and may change type: "turn into"); move sets parent_id and position;
// delete removes the block and everything nested in it.
type Op struct {
	Op       string                     `json:"op"`
	ID       uuid.UUID                  `json:"id"`
	Type     string                     `json:"type,omitempty"`
	ParentID *uuid.UUID                 `json:"parent_id,omitempty"`
	Position string                     `json:"position,omitempty"`
	Props    map[string]json.RawMessage `json:"props,omitempty"`
	// BaseText (update only) is the block text the edit started from; when the
	// block's text changed since, the two edits are merged (mergeSpans).
	BaseText json.RawMessage `json:"base_text,omitempty"`
}

// Rejected is an operation the server skipped because the block changed
// under it (deleted by someone else, an id taken).
type Rejected struct {
	ID      uuid.UUID `json:"id"`
	Op      string    `json:"op"`
	Code    string    `json:"code"`
	Message string    `json:"message"`
}

// OpsResult is the outcome of a batch: the page version after it, the final
// state of every block it touched and the ids it deleted. The same shape
// goes to the page's viewers as docs.ops (plus page_id, client_id, actor_id).
type OpsResult struct {
	PageID   uuid.UUID   `json:"page_id"`
	Version  int64       `json:"version"`
	ClientID string      `json:"client_id"`
	ActorID  uuid.UUID   `json:"actor_id"`
	Blocks   []Block     `json:"blocks"`
	Deleted  []uuid.UUID `json:"deleted"`
	Rejected []Rejected  `json:"rejected"`
}

type opsReq struct {
	ClientID string `json:"client_id"`
	Ops      []Op   `json:"ops"`
}

const maxOpsPerBatch = 500

// applyOps: POST /docs/pages/{id}/ops (edit, not locked or trashed).
func (s *Service) applyOps(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req opsReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if len(req.Ops) == 0 || len(req.Ops) > maxOpsPerBatch {
		return httpx.BadRequest(fmt.Sprintf("Send 1 to %d operations at a time.", maxOpsPerBatch))
	}
	if utf8.RuneCountInString(req.ClientID) > 64 {
		return httpx.BadRequest("client_id is too long.")
	}
	p, err := requirePage(ctx, s.d.Pool, me, id, access.Edit)
	if err != nil {
		return err
	}
	if err := editable(p, true); err != nil {
		return err
	}
	res, err := s.apply(ctx, me, p, req.Ops, req.ClientID)
	if err != nil {
		return err
	}
	httpx.OK(w, res)
	return nil
}

// batch carries one apply call's state.
type batch struct {
	s        *Service
	ctx      context.Context
	tx       pgx.Tx
	me       uuid.UUID
	page     *Page
	version  int64
	pc       propCtx
	changed  map[uuid.UUID]bool
	deleted  []uuid.UUID
	rejected []Rejected
	count    int                  // blocks on the page
	mentions map[uuid.UUID]string // newly mentioned person → block plain text
}

// apply runs ops on the page in one transaction (the page row is locked, so
// batches on one page are serialised), then announces them.
func (s *Service) apply(ctx context.Context, me uuid.UUID, page *Page, ops []Op, clientID string) (*OpsResult, error) {
	res := &OpsResult{PageID: page.ID, ClientID: clientID, ActorID: me, Blocks: []Block{}, Deleted: []uuid.UUID{}, Rejected: []Rejected{}}
	var mentions map[uuid.UUID]string
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		p, err := loadPage(ctx, tx, page.ID, true)
		if err != nil {
			return err
		}
		if err := editable(p, true); err != nil {
			return err
		}
		b := &batch{s: s, ctx: ctx, tx: tx, me: me, page: p, version: p.Version + 1, changed: map[uuid.UUID]bool{}, mentions: map[uuid.UUID]string{}}
		if b.pc.rules, err = memberRules(ctx, tx, p.OrgID, ops); err != nil {
			return err
		}
		b.pc.ctx, b.pc.q, b.pc.pageID, b.pc.orgID = ctx, tx, p.ID, p.OrgID
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM docs_blocks WHERE page_id = $1`, p.ID).Scan(&b.count); err != nil {
			return err
		}
		for _, op := range ops {
			var err error
			switch op.Op {
			case "create":
				err = b.create(op)
			case "update":
				err = b.update(op)
			case "move":
				err = b.move(op)
			case "delete":
				err = b.remove(op)
			default:
				err = httpx.BadRequest("op must be create, update, move or delete.")
			}
			if err != nil {
				return err
			}
		}
		if len(b.changed) == 0 && len(b.deleted) == 0 {
			res.Version = p.Version
			if b.rejected != nil {
				res.Rejected = b.rejected
			}
			return nil
		}
		if _, err := tx.Exec(ctx, `UPDATE docs_pages SET version = $2, updated_at = now(), updated_by = $3 WHERE id = $1`, p.ID, b.version, me); err != nil {
			return err
		}
		ids := make([]uuid.UUID, 0, len(b.changed))
		for id := range b.changed {
			ids = append(ids, id)
		}
		rows, err := tx.Query(ctx, `SELECT `+blockCols+` FROM docs_blocks b WHERE b.id = ANY ($1) ORDER BY b.position, b.id`, ids)
		if err != nil {
			return err
		}
		for rows.Next() {
			blk, err := scanBlock(rows)
			if err != nil {
				rows.Close()
				return err
			}
			res.Blocks = append(res.Blocks, blk)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		res.Version = b.version
		if b.deleted != nil {
			res.Deleted = b.deleted
		}
		if b.rejected != nil {
			res.Rejected = b.rejected
		}
		mentions = b.mentions
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(res.Blocks) > 0 || len(res.Deleted) > 0 {
		pub := *res
		pub.Rejected = nil
		s.publish(ctx, pageTopic(page.ID), EventOps, pub)
		if len(mentions) > 0 {
			bg.Go(ctx, "docs.notify_mentions", func(ctx context.Context) error {
				s.notifyMentions(ctx, me, page.ID, mentions)
				return nil
			})
		}
	}
	return res, nil
}

// memberRules accepts mentions of people who are members of the org (looked
// up once for every user mentioned in the batch).
func memberRules(ctx context.Context, q db.Querier, orgID uuid.UUID, ops []Op) (spanRules, error) {
	var ids []uuid.UUID
	for _, op := range ops {
		for _, raw := range op.Props {
			ids = append(ids, userMentionsIn(raw)...)
		}
	}
	valid := map[uuid.UUID]bool{}
	if len(ids) > 0 {
		rows, err := q.Query(ctx, `SELECT user_id FROM org_members WHERE org_id = $1 AND user_id = ANY ($2)`, orgID, dedupeIDs(ids))
		if err != nil {
			return spanRules{}, err
		}
		members, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		if err != nil {
			return spanRules{}, err
		}
		for _, m := range members {
			valid[m] = true
		}
	}
	return spanRules{validUser: func(id uuid.UUID) bool { return valid[id] }}, nil
}

// userMentionsIn finds user mention ids anywhere in a props value (text,
// captions, table cells) without knowing its shape.
func userMentionsIn(raw json.RawMessage) []uuid.UUID {
	if !strings.Contains(string(raw), `"user"`) {
		return nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	var out []uuid.UUID
	var walk func(any, int)
	walk = func(x any, depth int) {
		if depth > 6 {
			return
		}
		switch t := x.(type) {
		case map[string]any:
			if t["type"] == "user" {
				if s, ok := t["id"].(string); ok {
					if id, err := uuid.Parse(s); err == nil {
						out = append(out, id)
					}
				}
			}
			for _, c := range t {
				walk(c, depth+1)
			}
		case []any:
			for _, c := range t {
				walk(c, depth+1)
			}
		}
	}
	walk(v, 0)
	return out
}

func (b *batch) reject(op Op, code, msg string) {
	b.rejected = append(b.rejected, Rejected{ID: op.ID, Op: op.Op, Code: code, Message: msg})
}

// existing reads a block of this page, locked for the rest of the batch.
func (b *batch) existing(id uuid.UUID) (*Block, bool, error) {
	blk, err := scanBlock(b.tx.QueryRow(b.ctx, `SELECT `+blockCols+` FROM docs_blocks b WHERE b.id = $1 AND b.page_id = $2 FOR UPDATE`, id, b.page.ID))
	if db.IsNoRows(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &blk, true, nil
}

// checkParent validates a new parent block: on this page, a type that holds
// children, not the block itself or inside it, and not too deep.
func (b *batch) checkParent(self uuid.UUID, parent *uuid.UUID) (ok bool, code, msg string, err error) {
	if parent == nil {
		return true, "", "", nil
	}
	if *parent == self {
		return false, "invalid_parent", "A block can't go inside itself.", nil
	}
	rows, err := b.tx.Query(b.ctx, `
		WITH RECURSIVE up (id, parent_id, type, depth) AS (
			SELECT id, parent_id, type, 1 FROM docs_blocks WHERE id = $1 AND page_id = $2
			UNION ALL
			SELECT d.id, d.parent_id, d.type, up.depth + 1 FROM up JOIN docs_blocks d ON d.id = up.parent_id WHERE up.depth < 64
		)
		SELECT id, type FROM up ORDER BY depth`, *parent, b.page.ID)
	if err != nil {
		return false, "", "", err
	}
	type anc struct {
		ID   uuid.UUID
		Type string
	}
	chain, err := pgx.CollectRows(rows, pgx.RowToStructByPos[anc])
	if err != nil {
		return false, "", "", err
	}
	if len(chain) == 0 {
		return false, "gone", "The block it goes in was deleted.", nil
	}
	if !slices.Contains(nestable, chain[0].Type) {
		return false, "invalid_parent", "That block can't hold other blocks.", nil
	}
	for _, a := range chain {
		if a.ID == self {
			return false, "invalid_parent", "A block can't go inside one of its own nested blocks.", nil
		}
	}
	if len(chain) >= maxBlockDepth {
		return false, "too_deep", fmt.Sprintf("Blocks nest up to %d levels.", maxBlockDepth), nil
	}
	return true, "", "", nil
}

func (b *batch) create(op Op) error {
	if op.ID == uuid.Nil {
		return httpx.BadRequest("Each new block needs an id.")
	}
	if !slices.Contains(allTypes, op.Type) {
		return httpx.BadRequest("Unknown block type " + strconv.Quote(op.Type) + ".")
	}
	if !validKey(op.Position) {
		return httpx.BadRequest("Each new block needs a valid position.")
	}
	if b.count >= maxBlocks {
		return httpx.NewError(http.StatusConflict, "page_full", fmt.Sprintf("A page holds up to %d blocks. Move some into a subpage.", maxBlocks))
	}
	ok, code, msg, err := b.checkParent(op.ID, op.ParentID)
	if err != nil {
		return err
	}
	if !ok {
		b.reject(op, code, msg)
		return nil
	}
	n, err := normalizeProps(b.pc, op.Type, op.Props)
	if err != nil {
		return err
	}
	props, _ := json.Marshal(n.props)
	tag, err := b.tx.Exec(b.ctx, `
		INSERT INTO docs_blocks (id, page_id, parent_id, type, props, position, plain, version, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9) ON CONFLICT (id) DO NOTHING`,
		op.ID, b.page.ID, op.ParentID, op.Type, props, op.Position, n.plain, b.version, b.me)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		b.reject(op, "exists", "A block with that id already exists.")
		return nil
	}
	b.count++
	b.changed[op.ID] = true
	b.noteMentions(nil, n)
	return b.relink(op.ID, n.pages)
}

func (b *batch) update(op Op) error {
	cur, ok, err := b.existing(op.ID)
	if err != nil {
		return err
	}
	if !ok {
		b.reject(op, "gone", "That block was deleted.")
		return nil
	}
	typ := cur.Type
	if op.Type != "" {
		if !slices.Contains(allTypes, op.Type) {
			return httpx.BadRequest("Unknown block type " + strconv.Quote(op.Type) + ".")
		}
		typ = op.Type
	}
	curRaw, _ := json.Marshal(cur.Props)
	merged := propsMap(curRaw)
	for k, v := range op.Props {
		merged[k] = v
	}
	if text, ok := op.Props["text"]; ok && len(op.BaseText) > 0 && isTextType(cur.Type) {
		base, err1 := decodeSpans(op.BaseText)
		local, err2 := decodeSpans(text)
		if err1 == nil && err2 == nil {
			now := textOf(cur.Props)
			if !spansEqual(base, now) {
				m, _ := json.Marshal(mergeSpans(base, local, now))
				merged["text"] = m
			}
		}
	}
	n, err := normalizeProps(b.pc, typ, merged)
	if err != nil {
		return err
	}
	props, _ := json.Marshal(n.props)
	if _, err := b.tx.Exec(b.ctx, `UPDATE docs_blocks SET type = $2, props = $3, plain = $4, version = $5, updated_by = $6, updated_at = now() WHERE id = $1`,
		op.ID, typ, props, n.plain, b.version, b.me); err != nil {
		return err
	}
	b.changed[op.ID] = true
	if typ != cur.Type && !slices.Contains(nestable, typ) {
		if err := b.liftChildren(cur); err != nil {
			return err
		}
	}
	oldUsers, _ := blockRefs(*cur)
	b.noteMentions(oldUsers, n)
	return b.relink(op.ID, n.pages)
}

// liftChildren moves a block's children up beside it when it turns into a
// type that can't hold them (they keep their order, right after it).
func (b *batch) liftChildren(blk *Block) error {
	rows, err := b.tx.Query(b.ctx, `SELECT id FROM docs_blocks WHERE parent_id = $1 ORDER BY position, id`, blk.ID)
	if err != nil {
		return err
	}
	kids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil || len(kids) == 0 {
		return err
	}
	var next string
	if err := b.tx.QueryRow(b.ctx, `SELECT COALESCE(min(position), '') FROM docs_blocks WHERE page_id = $1 AND parent_id IS NOT DISTINCT FROM $2 AND position > $3`,
		b.page.ID, blk.ParentID, blk.Position).Scan(&next); err != nil {
		return err
	}
	keys := keysAfter(blk.Position, next, len(kids))
	for i, k := range kids {
		if _, err := b.tx.Exec(b.ctx, `UPDATE docs_blocks SET parent_id = $2, position = $3, version = $4, updated_by = $5, updated_at = now() WHERE id = $1`,
			k, blk.ParentID, keys[i], b.version, b.me); err != nil {
			return err
		}
		b.changed[k] = true
	}
	return nil
}

func (b *batch) move(op Op) error {
	if !validKey(op.Position) {
		return httpx.BadRequest("A move needs a valid position.")
	}
	_, ok, err := b.existing(op.ID)
	if err != nil {
		return err
	}
	if !ok {
		b.reject(op, "gone", "That block was deleted.")
		return nil
	}
	ok, code, msg, err := b.checkParent(op.ID, op.ParentID)
	if err != nil {
		return err
	}
	if !ok {
		b.reject(op, code, msg)
		return nil
	}
	if _, err := b.tx.Exec(b.ctx, `UPDATE docs_blocks SET parent_id = $2, position = $3, version = $4, updated_by = $5, updated_at = now() WHERE id = $1`,
		op.ID, op.ParentID, op.Position, b.version, b.me); err != nil {
		return err
	}
	b.changed[op.ID] = true
	return nil
}

func (b *batch) remove(op Op) error {
	rows, err := b.tx.Query(b.ctx, `
		WITH RECURSIVE tree (id, depth) AS (
			SELECT id, 0 FROM docs_blocks WHERE id = $1 AND page_id = $2
			UNION ALL
			SELECT c.id, t.depth + 1 FROM tree t JOIN docs_blocks c ON c.parent_id = t.id WHERE t.depth < 64
		)
		SELECT id FROM tree`, op.ID, b.page.ID)
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil // already gone: deleting is idempotent
	}
	if _, err := b.tx.Exec(b.ctx, `DELETE FROM docs_blocks WHERE id = $1`, op.ID); err != nil {
		return err
	}
	if _, err := b.tx.Exec(b.ctx, `
		INSERT INTO docs_block_tombstones (page_id, block_id, version) SELECT $1, unnest($2::uuid[]), $3
		ON CONFLICT (page_id, block_id) DO UPDATE SET version = EXCLUDED.version`, b.page.ID, ids, b.version); err != nil {
		return err
	}
	for _, id := range ids {
		delete(b.changed, id)
	}
	b.count -= len(ids)
	b.deleted = append(b.deleted, ids...)
	return nil
}

// relink rewrites the page links a block holds (for backlinks).
func (b *batch) relink(blockID uuid.UUID, pages []uuid.UUID) error {
	return relinkBlock(b.ctx, b.tx, b.page.ID, b.page.OrgID, blockID, pages)
}

func relinkBlock(ctx context.Context, q db.Querier, pageID, orgID, blockID uuid.UUID, pages []uuid.UUID) error {
	if _, err := q.Exec(ctx, `DELETE FROM docs_links WHERE source_block_id = $1`, blockID); err != nil {
		return err
	}
	if len(pages) == 0 {
		return nil
	}
	_, err := q.Exec(ctx, `
		INSERT INTO docs_links (source_block_id, source_page_id, target_page_id)
		SELECT $1, $2, t FROM unnest($3::uuid[]) t
		WHERE t <> $2 AND EXISTS (SELECT 1 FROM docs_pages d WHERE d.id = t AND d.org_id = $4)
		ON CONFLICT DO NOTHING`, blockID, pageID, dedupeIDs(pages), orgID)
	return err
}

// noteMentions records people mentioned in n but not before.
func (b *batch) noteMentions(old []uuid.UUID, n normalized) {
	before := map[uuid.UUID]bool{}
	for _, u := range old {
		before[u] = true
	}
	for _, u := range n.users {
		if !before[u] && u != b.me && len(b.mentions) < 20 {
			b.mentions[u] = n.plain
		}
	}
}

// ---- sync ------------------------------------------------------------------------

// SyncResult is GET /docs/pages/{id}/sync?since=N: what changed after
// version N, or the whole page (full) when N is too old.
type SyncResult struct {
	Page    *Page       `json:"page"`
	Version int64       `json:"version"`
	Full    bool        `json:"full"`
	Blocks  []Block     `json:"blocks"`
	Deleted []uuid.UUID `json:"deleted"`
}

func (s *Service) sync(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	since, err := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
	if err != nil || since < 0 {
		return httpx.BadRequest("since must be a version number.")
	}
	p, err := requirePage(ctx, s.d.Pool, me, id, access.View)
	if err != nil {
		return err
	}
	out, err := s.syncSince(ctx, p, since)
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

func (s *Service) syncSince(ctx context.Context, p *Page, since int64) (*SyncResult, error) {
	out := &SyncResult{Page: p, Version: p.Version, Blocks: []Block{}, Deleted: []uuid.UUID{}}
	if since >= p.Version {
		return out, nil
	}
	var floor int64
	if err := s.d.Pool.QueryRow(ctx, `SELECT tomb_floor FROM docs_pages WHERE id = $1`, p.ID).Scan(&floor); err != nil {
		return nil, err
	}
	if since < floor {
		blocks, err := loadBlocks(ctx, s.d.Pool, p.ID)
		if err != nil {
			return nil, err
		}
		out.Full, out.Blocks = true, blocks
		return out, nil
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT `+blockCols+` FROM docs_blocks b WHERE b.page_id = $1 AND b.version > $2 ORDER BY b.position, b.id`, p.ID, since)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		blk, err := scanBlock(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out.Blocks = append(out.Blocks, blk)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = s.d.Pool.Query(ctx, `SELECT block_id FROM docs_block_tombstones WHERE page_id = $1 AND version > $2`, p.ID, since)
	if err != nil {
		return nil, err
	}
	if out.Deleted, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID]); err != nil {
		return nil, err
	}
	if out.Deleted == nil {
		out.Deleted = []uuid.UUID{}
	}
	return out, nil
}
