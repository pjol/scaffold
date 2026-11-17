package docs

import (
	"encoding/json"
	"reflect"
	"strings"

	"github.com/google/uuid"
)

// DiffEntry is one block in a version comparison, in document order: the
// newer version's blocks, with removed blocks placed where they were.
type DiffEntry struct {
	Block  Block  `json:"block"`
	Depth  int    `json:"depth"`
	Change string `json:"change"` // "" (same), added, removed, changed
	// BeforeText is the block's text in the older version (changed only).
	BeforeText string `json:"before_text,omitempty"`
}

// DiffSummary counts what changed between two versions.
type DiffSummary struct {
	Added        int  `json:"added"`
	Removed      int  `json:"removed"`
	Changed      int  `json:"changed"`
	TitleChanged bool `json:"title_changed"`
}

func (d DiffSummary) Empty() bool {
	return d.Added == 0 && d.Removed == 0 && d.Changed == 0 && !d.TitleChanged
}

// Text is a short sentence for notifications and the updates feed.
func (d DiffSummary) Text() string {
	var parts []string
	plural := func(n int, one, many string) string {
		if n == 1 {
			return "1 " + one
		}
		return itoa(n) + " " + many
	}
	if d.TitleChanged {
		parts = append(parts, "renamed it")
	}
	if d.Added > 0 {
		parts = append(parts, "added "+plural(d.Added, "block", "blocks"))
	}
	if d.Changed > 0 {
		parts = append(parts, "edited "+plural(d.Changed, "block", "blocks"))
	}
	if d.Removed > 0 {
		parts = append(parts, "removed "+plural(d.Removed, "block", "blocks"))
	}
	if len(parts) == 0 {
		return "No changes to the text"
	}
	s := strings.Join(parts, ", ")
	return strings.ToUpper(s[:1]) + s[1:]
}

// blockText is a block's text for comparisons and "before" lines.
func blockText(typ string, props map[string]any) string {
	switch typ {
	case "code":
		return propString(props, "code")
	case "image", "file":
		return strings.TrimSpace(propString(props, "name") + " " + plainOf(spansOf(props, "caption")))
	case "bookmark":
		return strings.TrimSpace(propString(props, "title") + " " + propString(props, "url"))
	case "table":
		raw, _ := json.Marshal(props["rows"])
		var rows [][][]Span
		_ = json.Unmarshal(raw, &rows)
		var lines []string
		for _, row := range rows {
			cells := make([]string, len(row))
			for i, c := range row {
				cells[i] = plainOf(c)
			}
			lines = append(lines, strings.Join(cells, " | "))
		}
		return strings.Join(lines, "\n")
	}
	if _, ok := props["text"]; ok {
		return plainOf(textOf(props))
	}
	return ""
}

// sameBlock compares what a reader sees: type and props (JSON-normalised).
func sameBlock(a, b snapBlock) bool {
	if a.Type != b.Type {
		return false
	}
	return reflect.DeepEqual(normJSON(a.Props), normJSON(b.Props))
}

func normJSON(v map[string]any) any {
	raw, _ := json.Marshal(v)
	var out any
	_ = json.Unmarshal(raw, &out)
	return out
}

// docOrder returns blocks in document order (parents before children,
// siblings by position) with each block's depth.
func docOrder(blocks []snapBlock) ([]snapBlock, map[uuid.UUID]int) {
	ids := map[uuid.UUID]bool{}
	for _, b := range blocks {
		ids[b.ID] = true
	}
	kids := map[uuid.UUID][]snapBlock{}
	var roots []snapBlock
	for _, b := range blocks {
		if b.ParentID != nil && ids[*b.ParentID] {
			kids[*b.ParentID] = append(kids[*b.ParentID], b)
		} else {
			roots = append(roots, b)
		}
	}
	sortSnap := func(list []snapBlock) {
		for i := 1; i < len(list); i++ {
			for j := i; j > 0 && (list[j].Position < list[j-1].Position || (list[j].Position == list[j-1].Position && list[j].ID.String() < list[j-1].ID.String())); j-- {
				list[j], list[j-1] = list[j-1], list[j]
			}
		}
	}
	out := make([]snapBlock, 0, len(blocks))
	depth := map[uuid.UUID]int{}
	var walk func(list []snapBlock, d int)
	walk = func(list []snapBlock, d int) {
		sortSnap(list)
		for _, b := range list {
			if _, seen := depth[b.ID]; seen || d > 64 {
				continue
			}
			depth[b.ID] = d
			out = append(out, b)
			walk(kids[b.ID], d+1)
		}
	}
	walk(roots, 0)
	return out, depth
}

// diffBlocks compares an older and a newer version block by block (blocks
// keep their ids across versions): added, removed and changed blocks, in
// the newer version's order with removed blocks after the block that came
// before them in the older one.
func diffBlocks(older, newer []snapBlock) ([]DiffEntry, DiffSummary) {
	oldOrder, oldDepth := docOrder(older)
	newOrder, newDepth := docOrder(newer)
	oldByID := map[uuid.UUID]snapBlock{}
	for _, b := range oldOrder {
		oldByID[b.ID] = b
	}
	inNew := map[uuid.UUID]bool{}
	for _, b := range newOrder {
		inNew[b.ID] = true
	}
	// Removed blocks, grouped under the nearest earlier block that survived
	// (uuid.Nil: before everything).
	removedAfter := map[uuid.UUID][]snapBlock{}
	anchor := uuid.Nil
	for _, b := range oldOrder {
		if inNew[b.ID] {
			anchor = b.ID
			continue
		}
		removedAfter[anchor] = append(removedAfter[anchor], b)
	}
	var sum DiffSummary
	out := make([]DiffEntry, 0, len(newOrder)+len(older))
	emitRemoved := func(after uuid.UUID) {
		for _, b := range removedAfter[after] {
			out = append(out, DiffEntry{Block: b.block(), Depth: oldDepth[b.ID], Change: "removed"})
			sum.Removed++
		}
	}
	emitRemoved(uuid.Nil)
	for _, b := range newOrder {
		e := DiffEntry{Block: b.block(), Depth: newDepth[b.ID]}
		if ob, ok := oldByID[b.ID]; !ok {
			e.Change = "added"
			sum.Added++
		} else if !sameBlock(ob, b) {
			e.Change = "changed"
			e.BeforeText = blockText(ob.Type, ob.Props)
			sum.Changed++
		}
		out = append(out, e)
		emitRemoved(b.ID)
	}
	return out, sum
}

func (b snapBlock) block() Block {
	return Block{ID: b.ID, ParentID: b.ParentID, Type: b.Type, Props: b.Props, Position: b.Position}
}
