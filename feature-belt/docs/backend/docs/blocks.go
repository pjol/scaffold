package docs

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// Block is one row of a page's content. Props depend on Type (see
// normalizeProps); the JSON matches DocsBlock in shared/types.ts.
type Block struct {
	ID        uuid.UUID      `json:"id"`
	PageID    uuid.UUID      `json:"page_id"`
	ParentID  *uuid.UUID     `json:"parent_id"`
	Type      string         `json:"type"`
	Props     map[string]any `json:"props"`
	Position  string         `json:"position"`
	Version   int64          `json:"version"`
	CreatedBy *uuid.UUID     `json:"created_by"`
	UpdatedBy *uuid.UUID     `json:"updated_by"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// Block types. Text types carry rich text in props.text.
var (
	textTypes = []string{"paragraph", "heading_1", "heading_2", "heading_3", "bulleted_list", "numbered_list", "to_do", "toggle", "quote", "callout"}
	allTypes  = append(slices.Clone(textTypes), "code", "divider", "image", "file", "bookmark", "page", "table", "database")
	// nestable types may hold child blocks (indent with Tab).
	nestable = []string{"paragraph", "bulleted_list", "numbered_list", "to_do", "toggle", "quote", "callout"}
)

const (
	maxCodeChars   = 100000
	maxTableRows   = 100
	maxTableCols   = 20
	maxBlockDepth  = 12
	maxBlocks      = 5000 // blocks on one page
	maxTitleChars  = 300
	maxCaptionChar = 2000
)

var languageRE = regexp.MustCompile(`^[a-z0-9+#.\-]{1,32}$`)

// propCtx is what normalizeProps needs to check references: the page the
// block lives on and lookups the caller prepared for the batch.
type propCtx struct {
	ctx    context.Context
	q      db.Querier
	pageID uuid.UUID
	orgID  uuid.UUID
	rules  spanRules
}

// normalized is a block's cleaned props plus what was derived from them.
type normalized struct {
	props map[string]any
	plain string
	users []uuid.UUID // people mentioned
	pages []uuid.UUID // pages mentioned or linked
}

func badProps(typ, msg string) error {
	return httpx.BadRequest(fmt.Sprintf("A %s block %s", strings.ReplaceAll(typ, "_", " "), msg))
}

func decodeSpans(raw json.RawMessage) ([]Span, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return []Span{}, nil
	}
	var spans []Span
	if err := json.Unmarshal(raw, &spans); err != nil {
		return nil, err
	}
	if len(spans) > maxSpans {
		return nil, fmt.Errorf("too many spans")
	}
	return spans, nil
}

// normalizeProps validates raw props for typ and returns them cleaned, with
// only the keys the type uses (others are dropped, so "turn into" keeps the
// text and sheds the rest).
func normalizeProps(pc propCtx, typ string, raw map[string]json.RawMessage) (normalized, error) {
	n := normalized{props: map[string]any{}}
	str := func(key string) string {
		var s string
		if v, ok := raw[key]; ok {
			_ = json.Unmarshal(v, &s)
		}
		return s
	}
	richField := func(key string, limit int) ([]Span, error) {
		spans, err := decodeSpans(raw[key])
		if err != nil {
			return nil, badProps(typ, "has text it can't read.")
		}
		clean, count := sanitizeSpans(spans, pc.rules)
		if count > limit {
			return nil, badProps(typ, fmt.Sprintf("can hold up to %d characters.", limit))
		}
		u, p := mentionIDs(clean)
		n.users, n.pages = append(n.users, u...), append(n.pages, p...)
		return clean, nil
	}

	if slices.Contains(textTypes, typ) {
		text, err := richField("text", maxBlockChars)
		if err != nil {
			return n, err
		}
		n.props["text"] = text
		n.plain = plainOf(text)
	}
	switch typ {
	case "to_do":
		var checked bool
		if v, ok := raw["checked"]; ok {
			_ = json.Unmarshal(v, &checked)
		}
		n.props["checked"] = checked
	case "callout":
		icon := strings.TrimSpace(str("icon"))
		if icon == "" || utf8.RuneCountInString(icon) > 8 || len(icon) > 32 {
			icon = "💡"
		}
		n.props["icon"] = icon
	case "code":
		code := strings.ReplaceAll(str("code"), "\r\n", "\n")
		if utf8.RuneCountInString(code) > maxCodeChars {
			return n, badProps(typ, fmt.Sprintf("can hold up to %d characters.", maxCodeChars))
		}
		lang := strings.ToLower(strings.TrimSpace(str("language")))
		if !languageRE.MatchString(lang) {
			lang = "plain"
		}
		n.props["code"], n.props["language"] = code, lang
		n.plain = code
	case "image", "file":
		fileID, err := uuid.Parse(str("file_id"))
		if err != nil {
			return n, badProps(typ, "needs an uploaded file.")
		}
		var name, contentType string
		var size int64
		err = pc.q.QueryRow(pc.ctx, `SELECT name, content_type, size FROM docs_files WHERE id = $1 AND page_id = $2`, fileID, pc.pageID).
			Scan(&name, &contentType, &size)
		if db.IsNoRows(err) {
			return n, badProps(typ, "refers to a file that isn't on this page.")
		}
		if err != nil {
			return n, err
		}
		if typ == "image" && !strings.HasPrefix(contentType, "image/") {
			return n, badProps(typ, "needs an image file.")
		}
		caption, err := richField("caption", maxCaptionChar)
		if err != nil {
			return n, err
		}
		n.props["file_id"], n.props["name"], n.props["content_type"], n.props["size"], n.props["caption"] = fileID, name, contentType, size, caption
		if typ == "image" {
			var width int
			if v, ok := raw["width"]; ok {
				_ = json.Unmarshal(v, &width)
			}
			if width < 10 || width > 100 {
				width = 100
			}
			n.props["width"] = width
		}
		n.plain = strings.TrimSpace(name + " " + plainOf(caption))
	case "bookmark":
		link := safeLink(str("url"))
		if link == "" || strings.HasPrefix(link, "mailto:") {
			return n, badProps(typ, "needs a link starting with http:// or https://.")
		}
		title := strings.TrimSpace(cleanText(str("title")))
		if utf8.RuneCountInString(title) > maxTitleChars {
			title = string([]rune(title)[:maxTitleChars])
		}
		caption, err := richField("caption", maxCaptionChar)
		if err != nil {
			return n, err
		}
		n.props["url"], n.props["title"], n.props["caption"] = link, title, caption
		n.plain = strings.TrimSpace(title + " " + link + " " + plainOf(caption))
	case "page", "database":
		key := "page_id"
		if typ == "database" {
			key = "database_id"
		}
		id, err := uuid.Parse(str(key))
		if err != nil {
			return n, badProps(typ, "needs a page.")
		}
		var kind string
		err = pc.q.QueryRow(pc.ctx, `SELECT kind FROM docs_pages WHERE id = $1 AND org_id = $2`, id, pc.orgID).Scan(&kind)
		if db.IsNoRows(err) || (err == nil && typ == "database" && kind != "database") || id == pc.pageID {
			return n, badProps(typ, "refers to a page that isn't in this workspace.")
		}
		if err != nil {
			return n, err
		}
		n.props[key] = id
		n.pages = append(n.pages, id)
	case "table":
		var rows [][]json.RawMessage
		if v, ok := raw["rows"]; ok {
			if err := json.Unmarshal(v, &rows); err != nil {
				return n, badProps(typ, "has cells it can't read.")
			}
		}
		if len(rows) == 0 {
			rows = [][]json.RawMessage{{nil, nil}, {nil, nil}}
		}
		if len(rows) > maxTableRows {
			return n, badProps(typ, fmt.Sprintf("can have up to %d rows.", maxTableRows))
		}
		cols := 0
		for _, r := range rows {
			cols = max(cols, len(r))
		}
		if cols == 0 || cols > maxTableCols {
			return n, badProps(typ, fmt.Sprintf("needs 1 to %d columns.", maxTableCols))
		}
		out := make([][][]Span, len(rows))
		var plain []string
		total := 0
		for i, r := range rows {
			out[i] = make([][]Span, cols)
			for j := range cols {
				var cell []Span
				if j < len(r) {
					spans, err := decodeSpans(r[j])
					if err != nil {
						return n, badProps(typ, "has cells it can't read.")
					}
					var c int
					cell, c = sanitizeSpans(spans, pc.rules)
					total += c
					u, p := mentionIDs(cell)
					n.users, n.pages = append(n.users, u...), append(n.pages, p...)
				}
				if cell == nil {
					cell = []Span{}
				}
				out[i][j] = cell
				if t := plainOf(cell); t != "" {
					plain = append(plain, t)
				}
			}
		}
		if total > maxBlockChars {
			return n, badProps(typ, fmt.Sprintf("can hold up to %d characters.", maxBlockChars))
		}
		var hr, hc bool
		if v, ok := raw["header_row"]; ok {
			_ = json.Unmarshal(v, &hr)
		}
		if v, ok := raw["header_col"]; ok {
			_ = json.Unmarshal(v, &hc)
		}
		n.props["rows"], n.props["header_row"], n.props["header_col"] = out, hr, hc
		n.plain = strings.Join(plain, " ")
	case "divider":
	}
	return n, nil
}

// propsMap turns stored props JSON into the raw map normalizeProps reads.
func propsMap(b []byte) map[string]json.RawMessage {
	m := map[string]json.RawMessage{}
	_ = json.Unmarshal(b, &m)
	return m
}

// textOf reads a block's rich text from stored props.
func textOf(props map[string]any) []Span {
	b, _ := json.Marshal(props["text"])
	spans, _ := decodeSpans(b)
	return spans
}

// spansOf reads any rich-text field of stored props.
func spansOf(props map[string]any, key string) []Span {
	b, _ := json.Marshal(props[key])
	spans, _ := decodeSpans(b)
	return spans
}

func propString(props map[string]any, key string) string {
	s, _ := props[key].(string)
	return s
}

func propBool(props map[string]any, key string) bool {
	b, _ := props[key].(bool)
	return b
}

const blockCols = `b.id, b.page_id, b.parent_id, b.type, b.props, b.position, b.version, b.created_by, b.updated_by, b.created_at, b.updated_at`

func scanBlock(row interface{ Scan(...any) error }) (Block, error) {
	var b Block
	var props []byte
	err := row.Scan(&b.ID, &b.PageID, &b.ParentID, &b.Type, &props, &b.Position, &b.Version, &b.CreatedBy, &b.UpdatedBy, &b.CreatedAt, &b.UpdatedAt)
	if err == nil {
		b.Props = map[string]any{}
		_ = json.Unmarshal(props, &b.Props)
	}
	return b, err
}

// loadBlocks returns every block of a page in document order (parents before
// children, siblings by position).
func loadBlocks(ctx context.Context, q db.Querier, pageID uuid.UUID) ([]Block, error) {
	rows, err := q.Query(ctx, `SELECT `+blockCols+` FROM docs_blocks b WHERE b.page_id = $1 ORDER BY b.position, b.id`, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var all []Block
	for rows.Next() {
		b, err := scanBlock(rows)
		if err != nil {
			return nil, err
		}
		all = append(all, b)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return orderTree(all), nil
}

// orderTree sorts blocks depth-first: each block followed by its children.
// Input must already be sorted by (position, id). Orphans (a parent that no
// longer exists) are appended at the top level.
func orderTree(all []Block) []Block {
	kids := map[uuid.UUID][]Block{}
	ids := map[uuid.UUID]bool{}
	for _, b := range all {
		ids[b.ID] = true
	}
	var roots []Block
	for _, b := range all {
		if b.ParentID != nil && ids[*b.ParentID] {
			kids[*b.ParentID] = append(kids[*b.ParentID], b)
		} else {
			roots = append(roots, b)
		}
	}
	out := make([]Block, 0, len(all))
	var walk func(list []Block, depth int)
	walk = func(list []Block, depth int) {
		for _, b := range list {
			out = append(out, b)
			if depth < 64 {
				walk(kids[b.ID], depth+1)
			}
		}
	}
	walk(roots, 0)
	if out == nil {
		out = []Block{}
	}
	return out
}
