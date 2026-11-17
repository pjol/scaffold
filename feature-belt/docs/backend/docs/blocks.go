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

	"scaffoldapp/backend/internal/access"
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
	textTypes = []string{"paragraph", "heading_1", "heading_2", "heading_3", "heading_4", "bulleted_list", "numbered_list", "to_do", "toggle", "quote", "callout"}
	allTypes  = append(slices.Clone(textTypes), "code", "divider", "image", "file", "bookmark", "page", "table", "database", "connected_page",
		"column_list", "column", "synced_block", "table_of_contents", "breadcrumb", "equation", "button", "link_to_page", "video", "audio", "pdf", "embed")
	// nestable types may hold child blocks (indent with Tab). Headings hold
	// children only when toggleable; a synced copy holds none (holdsChildren).
	nestable = []string{"paragraph", "bulleted_list", "numbered_list", "to_do", "toggle", "quote", "callout", "column_list", "column", "synced_block"}
	// structural types keep their shape: they never turn into other types.
	structural = []string{"column_list", "column", "synced_block"}
	// fileTypes of blocks that show an upload, and the content types each takes.
	fileBlockTypes = map[string]string{"image": "image/", "video": "video/", "audio": "audio/", "pdf": "application/pdf", "file": ""}
)

const (
	maxCodeChars     = 100000
	maxTableRows     = 100
	maxTableCols     = 20
	maxBlockDepth    = 12
	maxBlocks        = 5000 // blocks on one page
	maxTitleChars    = 300
	maxCaptionChar   = 2000
	maxColumns       = 5
	maxEquationChars = 10000
	maxButtonBlocks  = 50
	maxButtonActions = 5
)

var languageRE = regexp.MustCompile(`^[a-z0-9+#.\-]{1,32}$`)

// swatchHues are the block and text colours (Notion's set); a colour is a
// hue for text or "<hue>_background" for a highlight. "" is the default.
var swatchHues = []string{"gray", "brown", "orange", "yellow", "green", "blue", "purple", "pink", "red"}

func validColor(c string) bool {
	return c == "" || slices.Contains(swatchHues, strings.TrimSuffix(c, "_background"))
}

// propCtx is what normalizeProps needs to check references: the page the
// block lives on and lookups the caller prepared for the batch.
type propCtx struct {
	ctx    context.Context
	q      db.Querier
	pageID uuid.UUID
	orgID  uuid.UUID
	rules  spanRules
	// me is who makes the change (uuid.Nil: a restore, which re-checks
	// nothing about the person); prev is the block's props before an update.
	me   uuid.UUID
	prev map[string]any
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

// holdsChildren reports whether a block of typ with props may hold blocks.
func holdsChildren(typ string, props map[string]any) bool {
	switch typ {
	case "heading_1", "heading_2", "heading_3", "heading_4":
		return propBool(props, "toggleable")
	case "synced_block":
		return propString(props, "source_id") == ""
	}
	return slices.Contains(nestable, typ)
}

func clip(s string, n int) string {
	s = strings.TrimSpace(cleanText(s))
	if utf8.RuneCountInString(s) > n {
		s = string([]rune(s)[:n])
	}
	return s
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
	boolean := func(key string) bool {
		var b bool
		if v, ok := raw[key]; ok {
			_ = json.Unmarshal(v, &b)
		}
		return b
	}
	integer := func(key string) (int, bool) {
		var f float64
		v, ok := raw[key]
		if !ok || json.Unmarshal(v, &f) != nil {
			return 0, false
		}
		return int(f), true
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
	caption := func() error {
		c, err := richField("caption", maxCaptionChar)
		if err != nil {
			return err
		}
		n.props["caption"] = c
		return nil
	}
	pageRef := func(key string, wantDatabase bool) error {
		id, err := uuid.Parse(str(key))
		if err != nil {
			return badProps(typ, "needs a page.")
		}
		var kind string
		err = pc.q.QueryRow(pc.ctx, `SELECT kind FROM docs_pages WHERE id = $1 AND org_id = $2`, id, pc.orgID).Scan(&kind)
		if db.IsNoRows(err) || (err == nil && wantDatabase && kind != "database") || id == pc.pageID {
			return badProps(typ, "refers to a page that isn't in this workspace.")
		}
		if err != nil {
			return err
		}
		n.props[key] = id
		n.pages = append(n.pages, id)
		return nil
	}

	// Every block may carry a colour (text or background).
	if c := str("color"); c != "" {
		if !validColor(c) {
			return n, badProps(typ, "has a colour it doesn't know.")
		}
		n.props["color"] = c
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
	case "heading_1", "heading_2", "heading_3", "heading_4":
		if boolean("toggleable") {
			n.props["toggleable"] = true
		}
	case "to_do":
		n.props["checked"] = boolean("checked")
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
		if boolean("wrap") {
			n.props["wrap"] = true
		}
		if err := caption(); err != nil {
			return n, err
		}
		n.plain = strings.TrimSpace(code + " " + plainOf(spansOf(n.props, "caption")))
	case "image", "file", "video", "audio", "pdf":
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
		if want := fileBlockTypes[typ]; want != "" && !strings.HasPrefix(contentType, want) {
			return n, badProps(typ, "needs "+map[string]string{"image": "an image", "video": "a video", "audio": "an audio file", "pdf": "a PDF"}[typ]+".")
		}
		if err := caption(); err != nil {
			return n, err
		}
		n.props["file_id"], n.props["name"], n.props["content_type"], n.props["size"] = fileID, name, contentType, size
		if typ == "image" || typ == "video" {
			width, _ := integer("width")
			if width < 10 || width > 100 {
				width = 100
			}
			n.props["width"] = width
			if a := str("align"); a == "left" || a == "right" {
				n.props["align"] = a
			}
		}
		if typ == "image" {
			if alt := clip(str("alt"), 1000); alt != "" {
				n.props["alt"] = alt
			}
		}
		if typ == "pdf" {
			if h, ok := integer("height"); ok && h >= 240 && h <= 2000 {
				n.props["height"] = h
			}
		}
		n.plain = strings.TrimSpace(name + " " + plainOf(spansOf(n.props, "caption")))
	case "bookmark":
		link := safeLink(str("url"))
		if link == "" || strings.HasPrefix(link, "mailto:") {
			return n, badProps(typ, "needs a link starting with http:// or https://.")
		}
		title := clip(str("title"), maxTitleChars)
		desc := clip(str("description"), 500)
		site := clip(str("site_name"), 200)
		if err := caption(); err != nil {
			return n, err
		}
		n.props["url"], n.props["title"] = link, title
		if desc != "" {
			n.props["description"] = desc
		}
		if site != "" {
			n.props["site_name"] = site
		}
		if pid, err := uuid.Parse(str("preview_id")); err == nil {
			var ok bool
			if err := pc.q.QueryRow(pc.ctx, `SELECT EXISTS (SELECT 1 FROM docs_link_previews WHERE id = $1)`, pid).Scan(&ok); err != nil {
				return n, err
			}
			if ok {
				n.props["preview_id"] = pid
			}
		}
		n.plain = strings.TrimSpace(title + " " + desc + " " + link + " " + plainOf(spansOf(n.props, "caption")))
	case "embed":
		e, ok := embedFor(str("url"))
		if !ok {
			return n, badProps(typ, "needs a link from a site that can be shown here. Add other links as bookmarks.")
		}
		if err := caption(); err != nil {
			return n, err
		}
		n.props["url"], n.props["provider"], n.props["src"] = e.URL, e.Provider, e.Src
		if h, ok := integer("height"); ok && h >= 120 && h <= 1600 {
			n.props["height"] = h
		}
		if width, ok := integer("width"); ok && width >= 10 && width < 100 {
			n.props["width"] = width
		}
		if a := str("align"); a == "left" || a == "right" {
			n.props["align"] = a
		}
		n.plain = strings.TrimSpace(embedName(e.Provider) + " " + e.URL + " " + plainOf(spansOf(n.props, "caption")))
	case "page", "link_to_page":
		if err := pageRef("page_id", false); err != nil {
			return n, err
		}
	case "database":
		if err := pageRef("database_id", true); err != nil {
			return n, err
		}
	case "equation":
		expr := strings.TrimSpace(strings.ReplaceAll(cleanText(str("expression")), "\r\n", "\n"))
		if utf8.RuneCountInString(expr) > maxEquationChars {
			return n, badProps(typ, fmt.Sprintf("can hold up to %d characters.", maxEquationChars))
		}
		n.props["expression"] = expr
		n.plain = expr
	case "table_of_contents", "breadcrumb", "divider", "column_list":
	case "column":
		var w float64
		if v, ok := raw["width"]; ok {
			_ = json.Unmarshal(v, &w)
		}
		if w > 0.04 && w <= 1 {
			n.props["width"] = float64(int(w*1000)) / 1000
		}
	case "synced_block":
		if err := pc.syncedSource(str("source_id"), n.props); err != nil {
			return n, err
		}
	case "button":
		if err := pc.buttonProps(raw, &n); err != nil {
			return n, err
		}
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
		n.props["rows"], n.props["header_row"], n.props["header_col"] = out, boolean("header_row"), boolean("header_col")
		n.plain = strings.Join(plain, " ")
	case "connected_page":
		// A Notion page or Google Doc shown live to each viewer through their
		// own connection (connected_routes.go resolveRoute).
		provider := str("provider")
		if provider != providerNotion && provider != providerGoogle {
			return n, badProps(typ, "needs a Notion page or a Google document.")
		}
		ref, account := str("ref"), str("account_id")
		if !validRef(ref) || len(account) > 128 {
			return n, badProps(typ, "needs a Notion page or a Google document.")
		}
		title := clip(str("title"), maxTitleChars)
		link := safeLink(str("url"))
		if !strings.HasPrefix(link, "https://") {
			link = ""
		}
		n.props["provider"], n.props["ref"], n.props["account_id"], n.props["title"], n.props["url"] = provider, ref, account, title, link
		n.plain = title
	}
	return n, nil
}

// syncedSource checks a synced copy's original: a synced block in the same
// workspace that is itself an original. The person making a copy must be
// able to open the original's page (a restore re-checks nothing). The copy
// stores where the original lives; each viewer then opens that page with
// their own access, so a copy never shows what its viewer can't open.
func (pc propCtx) syncedSource(raw string, props map[string]any) error {
	if raw == "" {
		return nil
	}
	src, err := uuid.Parse(raw)
	if err != nil {
		return badProps("synced_block", "refers to a block it can't find.")
	}
	var pageID uuid.UUID
	var srcType string
	var srcProps []byte
	err = pc.q.QueryRow(pc.ctx, `
		SELECT b.page_id, b.type, b.props FROM docs_blocks b JOIN docs_pages p ON p.id = b.page_id
		WHERE b.id = $1 AND p.org_id = $2 AND p.deleted_at IS NULL`, src, pc.orgID).Scan(&pageID, &srcType, &srcProps)
	if db.IsNoRows(err) {
		return badProps("synced_block", "refers to a block it can't find.")
	}
	if err != nil {
		return err
	}
	if srcType != "synced_block" || propString(propsAny(srcProps), "source_id") != "" {
		return badProps("synced_block", "must copy an original synced block.")
	}
	if pc.me != uuid.Nil && propString(pc.prev, "source_id") != src.String() {
		if _, err := access.Require(pc.ctx, pc.q, pc.me, Kind, pageID, access.View); err != nil {
			return badProps("synced_block", "refers to a block it can't find.")
		}
	}
	props["source_id"], props["source_page_id"] = src, pageID
	return nil
}

func propsAny(b []byte) map[string]any {
	m := map[string]any{}
	_ = json.Unmarshal(b, &m)
	return m
}

// BlockButtonAction is one thing a button does when clicked: insert its blocks
// below it, open a link, or add a row to a database with preset values.
type BlockButtonAction struct {
	Type       string             `json:"type"` // insert | link | add_row
	Blocks     []BlockButtonBlock `json:"blocks,omitempty"`
	URL        string             `json:"url,omitempty"`
	DatabaseID *uuid.UUID         `json:"database_id,omitempty"`
	Title      string             `json:"title,omitempty"` // add_row: the new row's title
	Values     json.RawMessage    `json:"values,omitempty"`
}

// BlockButtonBlock is a block a button inserts (a template, not on the page).
type BlockButtonBlock struct {
	Type     string             `json:"type"`
	Props    map[string]any     `json:"props"`
	Children []BlockButtonBlock `json:"children"`
}

// buttonTemplateTypes are what a button may insert.
var buttonTemplateTypes = append(slices.Clone(textTypes), "code", "divider", "equation", "table", "table_of_contents")

func (pc propCtx) buttonProps(raw map[string]json.RawMessage, n *normalized) error {
	var label string
	_ = json.Unmarshal(raw["label"], &label)
	label = clip(label, 100)
	if label == "" {
		label = "Button"
	}
	var in []struct {
		Type       string            `json:"type"`
		Blocks     []json.RawMessage `json:"blocks"`
		URL        string            `json:"url"`
		DatabaseID string            `json:"database_id"`
		Title      string            `json:"title"`
		Values     json.RawMessage   `json:"values"`
	}
	if v, ok := raw["actions"]; ok && json.Unmarshal(v, &in) != nil {
		return badProps("button", "has actions it can't read.")
	}
	if len(in) > maxButtonActions {
		return badProps("button", fmt.Sprintf("can do up to %d things.", maxButtonActions))
	}
	actions := []BlockButtonAction{}
	count := 0
	for _, a := range in {
		switch a.Type {
		case "insert":
			blocks, err := pc.buttonBlocks(a.Blocks, 0, &count, n)
			if err != nil {
				return err
			}
			actions = append(actions, BlockButtonAction{Type: "insert", Blocks: blocks})
		case "link":
			link := safeLink(a.URL)
			if link == "" {
				return badProps("button", "needs a link starting with http://, https:// or mailto:.")
			}
			actions = append(actions, BlockButtonAction{Type: "link", URL: link})
		case "add_row":
			id, err := uuid.Parse(a.DatabaseID)
			if err != nil {
				return badProps("button", "needs a database to add rows to.")
			}
			var kind string
			err = pc.q.QueryRow(pc.ctx, `SELECT kind FROM docs_pages WHERE id = $1 AND org_id = $2 AND NOT is_row`, id, pc.orgID).Scan(&kind)
			if db.IsNoRows(err) || (err == nil && kind != "database") {
				return badProps("button", "needs a database in this workspace.")
			}
			if err != nil {
				return err
			}
			values := a.Values
			if len(values) == 0 || string(values) == "null" {
				values = json.RawMessage(`{}`)
			}
			var check map[string]json.RawMessage
			if len(values) > 8192 || json.Unmarshal(values, &check) != nil {
				return badProps("button", "has preset values it can't read.")
			}
			actions = append(actions, BlockButtonAction{Type: "add_row", DatabaseID: &id, Title: clip(a.Title, maxTitleChars), Values: values})
			n.pages = append(n.pages, id)
		default:
			return badProps("button", "has an action it doesn't know.")
		}
	}
	n.props["label"], n.props["actions"] = label, actions
	n.plain = label
	return nil
}

func (pc propCtx) buttonBlocks(raws []json.RawMessage, depth int, count *int, n *normalized) ([]BlockButtonBlock, error) {
	out := []BlockButtonBlock{}
	for _, r := range raws {
		var d struct {
			Type     string                     `json:"type"`
			Props    map[string]json.RawMessage `json:"props"`
			Children []json.RawMessage          `json:"children"`
		}
		if json.Unmarshal(r, &d) != nil || !slices.Contains(buttonTemplateTypes, d.Type) {
			return nil, badProps("button", "can insert text, lists, headings, toggles, callouts, quotes, code, equations and tables.")
		}
		*count++
		if *count > maxButtonBlocks || depth > 4 {
			return nil, badProps("button", fmt.Sprintf("can insert up to %d blocks.", maxButtonBlocks))
		}
		inner := pc
		inner.prev = nil
		bn, err := normalizeProps(inner, d.Type, d.Props)
		if err != nil {
			return nil, err
		}
		n.users = append(n.users, bn.users...)
		blk := BlockButtonBlock{Type: d.Type, Props: bn.props, Children: []BlockButtonBlock{}}
		if len(d.Children) > 0 && holdsChildren(d.Type, bn.props) {
			if blk.Children, err = pc.buttonBlocks(d.Children, depth+1, count, n); err != nil {
				return nil, err
			}
		}
		out = append(out, blk)
	}
	return out, nil
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
