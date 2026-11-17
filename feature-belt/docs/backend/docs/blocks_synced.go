package docs

import (
	"context"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/access"
)

// syncedResolver returns the original content of synced copies for
// Markdown (export, agent tools): the original's nested blocks when me can
// open its page (agents capped by their scope through access.ViaAgent in
// ctx), else not ok. Results are cached per call.
func (s *Service) syncedResolver(ctx context.Context, me, pageID uuid.UUID) func(string) ([]Block, bool) {
	type entry struct {
		blocks []Block
		ok     bool
	}
	cache := map[string]entry{}
	pages := map[uuid.UUID][]Block{}
	return func(src string) ([]Block, bool) {
		if e, hit := cache[src]; hit {
			return e.blocks, e.ok
		}
		e := entry{}
		defer func() { cache[src] = e }()
		id, err := uuid.Parse(src)
		if err != nil || len(cache) > 50 {
			return nil, false
		}
		var srcPage uuid.UUID
		if err := s.d.Pool.QueryRow(ctx, `SELECT page_id FROM docs_blocks WHERE id = $1 AND type = 'synced_block'`, id).Scan(&srcPage); err != nil {
			return nil, false
		}
		if srcPage != pageID {
			if _, err := access.Require(ctx, s.d.Pool, me, Kind, srcPage, access.View); err != nil {
				return nil, false
			}
		}
		all, loaded := pages[srcPage]
		if !loaded {
			if all, err = loadBlocks(ctx, s.d.Pool, srcPage); err != nil {
				return nil, false
			}
			pages[srcPage] = all
		}
		e.blocks, e.ok = subtree(all, id), true
		return e.blocks, e.ok
	}
}

// subtree is the blocks nested under root (document order, root excluded),
// re-rooted so root's children are top level.
func subtree(all []Block, root uuid.UUID) []Block {
	in := map[uuid.UUID]bool{root: true}
	var out []Block
	for _, b := range all { // document order: parents come first
		if b.ParentID != nil && in[*b.ParentID] {
			in[b.ID] = true
			if *b.ParentID == root {
				b.ParentID = nil
			}
			out = append(out, b)
		}
	}
	return out
}
