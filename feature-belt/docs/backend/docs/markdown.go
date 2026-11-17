package docs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
)

// ---- export ----------------------------------------------------------------------

// mdOptions controls Markdown rendering. withIDs puts "<!-- block:<id> -->"
// before every block (agent tools edit blocks by id).
type mdOptions struct {
	names   mdNames
	withIDs bool
	// pageLink is the app path of a linked page (default /docs/<id>;
	// connected pages link to /docs/c/<connection>/<id>).
	pageLink func(id string) string
	// synced returns a synced copy's original content (document order) when
	// the reader can open it; ok false renders a note instead.
	synced func(sourceID string) (blocks []Block, ok bool)
}

// blocksToMarkdown renders blocks (document order, as loadBlocks returns
// them) as Markdown.
func blocksToMarkdown(blocks []Block, opt mdOptions) string {
	kids := map[uuid.UUID][]Block{}
	ids := map[uuid.UUID]bool{}
	for _, b := range blocks {
		ids[b.ID] = true
	}
	var roots []Block
	for _, b := range blocks {
		if b.ParentID != nil && ids[*b.ParentID] {
			kids[*b.ParentID] = append(kids[*b.ParentID], b)
		} else {
			roots = append(roots, b)
		}
	}
	var out strings.Builder
	var render func(list []Block, indent string, depth int)
	render = func(list []Block, indent string, depth int) {
		number := 0
		for i, b := range list {
			if b.Type == "numbered_list" {
				number++
			} else {
				number = 0
			}
			if opt.withIDs {
				out.WriteString(indent + "<!-- block:" + b.ID.String() + " -->\n")
			}
			text := spansToMarkdown(textOf(b.Props), opt.names)
			line := func(prefix, body string) {
				out.WriteString(indent + prefix + strings.ReplaceAll(body, "\n", "\n"+indent+strings.Repeat(" ", utf8.RuneCountInString(prefix))) + "\n")
			}
			fileSrc := func() string {
				if u := propString(b.Props, "url"); u != "" && propString(b.Props, "file_id") == "" {
					return u
				}
				return "/docs/files/" + propString(b.Props, "file_id")
			}
			childIndent := indent + "  "
			nested := true // render children after the block's own line
			switch b.Type {
			case "heading_1", "heading_2", "heading_3", "heading_4":
				n, _ := strconv.Atoi(b.Type[len(b.Type)-1:])
				if propBool(b.Props, "toggleable") {
					out.WriteString(indent + "<details>\n" + indent + "<summary>" + strings.Repeat("#", n) + " " + strings.ReplaceAll(text, "\n", " ") + "</summary>\n\n")
					if depth < 32 {
						render(kids[b.ID], indent, depth+1)
					}
					out.WriteString(indent + "</details>\n")
					nested = false
				} else {
					line(strings.Repeat("#", n)+" ", text)
				}
			case "bulleted_list":
				line("- ", text)
			case "numbered_list":
				line(strconv.Itoa(number)+". ", text)
				childIndent = indent + strings.Repeat(" ", len(strconv.Itoa(number))+2)
			case "to_do":
				mark := " "
				if propBool(b.Props, "checked") {
					mark = "x"
				}
				line("- ["+mark+"] ", text)
			case "toggle":
				out.WriteString(indent + "<details>\n" + indent + "<summary>" + text + "</summary>\n\n")
				if depth < 32 {
					render(kids[b.ID], indent, depth+1)
				}
				out.WriteString(indent + "</details>\n")
				nested = false
			case "quote":
				line("> ", strings.ReplaceAll(text, "\n", "\n> "))
			case "callout":
				line("> ", propString(b.Props, "icon")+" "+strings.ReplaceAll(text, "\n", "\n> "))
			case "code":
				lang := propString(b.Props, "language")
				if lang == "plain" {
					lang = ""
				}
				code := propString(b.Props, "code")
				fence := "```"
				for strings.Contains(code, fence) {
					fence += "`"
				}
				out.WriteString(indent + fence + lang + "\n" + code + "\n" + indent + fence + "\n")
				if c := plainOf(spansOf(b.Props, "caption")); c != "" && opt.withIDs {
					out.WriteString(indent + "(Caption: " + mdEscaper.Replace(c) + ")\n")
				}
			case "equation":
				out.WriteString(indent + "$$\n" + indent + strings.ReplaceAll(propString(b.Props, "expression"), "\n", "\n"+indent) + "\n" + indent + "$$\n")
			case "divider":
				out.WriteString(indent + "---\n")
			case "table_of_contents":
				out.WriteString(indent + "[TOC]\n")
			case "breadcrumb":
				out.WriteString(indent + "<!-- breadcrumb -->\n")
			case "image":
				alt := propString(b.Props, "alt")
				if alt == "" {
					alt = plainOf(spansOf(b.Props, "caption"))
				}
				if alt == "" {
					alt = propString(b.Props, "name")
				}
				out.WriteString(indent + "![" + mdEscaper.Replace(alt) + "](" + fileSrc() + ")\n")
			case "file", "video", "audio", "pdf":
				label := propString(b.Props, "name")
				if label == "" {
					label = DisplayBlockType(b.Type)
				}
				prefix := ""
				if opt.withIDs && b.Type != "file" {
					prefix = DisplayBlockType(b.Type) + ": "
				}
				out.WriteString(indent + prefix + "[" + mdEscaper.Replace(label) + "](" + fileSrc() + ")\n")
			case "bookmark":
				title := propString(b.Props, "title")
				if title == "" {
					title = propString(b.Props, "url")
				}
				out.WriteString(indent + "[" + mdEscaper.Replace(title) + "](" + propString(b.Props, "url") + ")\n")
				if d := propString(b.Props, "description"); d != "" && opt.withIDs {
					out.WriteString(indent + "(" + mdEscaper.Replace(d) + ")\n")
				}
			case "embed":
				out.WriteString(indent + propString(b.Props, "url") + "\n")
			case "page", "database", "link_to_page":
				key := "page_id"
				if b.Type == "database" {
					key = "database_id"
				}
				id := propString(b.Props, key)
				title := "Untitled"
				if opt.names.page != nil {
					if t := opt.names.page(id); t != "" {
						title = t
					}
				}
				suffix := ""
				if b.Type == "database" && opt.withIDs {
					suffix = " (database: read it with docs_query_database)"
				}
				link := "/docs/" + id
				if opt.pageLink != nil {
					link = opt.pageLink(id)
				}
				out.WriteString(indent + "[" + mdEscaper.Replace(title) + "](" + link + ")" + suffix + "\n")
			case "connected_page":
				title := propString(b.Props, "title")
				if title == "" {
					title = "Untitled"
				}
				suffix := ""
				if opt.withIDs {
					suffix = " (" + platformLabel(propString(b.Props, "provider")) + " page " + propString(b.Props, "ref") + ": read it with docs_get_page and that source)"
				}
				out.WriteString(indent + "[" + mdEscaper.Replace(title) + "](" + propString(b.Props, "url") + ")" + suffix + "\n")
			case "table":
				raw, _ := json.Marshal(b.Props["rows"])
				var rows [][][]Span
				_ = json.Unmarshal(raw, &rows)
				for ri, row := range rows {
					cells := make([]string, len(row))
					for ci, cell := range row {
						cells[ci] = strings.ReplaceAll(strings.ReplaceAll(spansToMarkdown(cell, opt.names), "|", `\|`), "\n", " ")
					}
					out.WriteString(indent + "| " + strings.Join(cells, " | ") + " |\n")
					if ri == 0 {
						seps := make([]string, len(row))
						for i := range seps {
							seps[i] = "---"
						}
						out.WriteString(indent + "| " + strings.Join(seps, " | ") + " |\n")
					}
				}
			case "button":
				out.WriteString(indent + "[" + mdEscaper.Replace(propString(b.Props, "label")) + "] (button)\n")
			case "column_list":
				out.WriteString(indent + "<columns>\n")
				for _, col := range kids[b.ID] {
					attr := ""
					if w, ok := col.Props["width"].(float64); ok && w > 0 {
						attr = ` width="` + strconv.FormatFloat(w, 'f', -1, 64) + `"`
					}
					if opt.withIDs {
						out.WriteString(indent + "<!-- block:" + col.ID.String() + " -->\n")
					}
					out.WriteString(indent + "<column" + attr + ">\n\n")
					if depth < 32 {
						render(kids[col.ID], indent, depth+2)
					}
					out.WriteString("\n" + indent + "</column>\n")
				}
				out.WriteString(indent + "</columns>\n")
				nested = false
			case "column":
				nested = true // a stray column renders its content
				childIndent = indent
			case "synced_block":
				childIndent, nested = indent, true
				if src := propString(b.Props, "source_id"); src != "" {
					nested = false
					var content []Block
					ok := false
					if opt.synced != nil {
						content, ok = opt.synced(src)
					}
					if !ok {
						out.WriteString(indent + "(Synced content you can't open.)\n")
					} else if len(content) > 0 && depth < 32 {
						inner := opt
						inner.synced = nil // a copy shows its original's blocks, never another copy
						for _, l := range strings.Split(strings.TrimRight(blocksToMarkdown(content, inner), "\n"), "\n") {
							out.WriteString(indent + l + "\n")
						}
					}
				}
			default:
				line("", text)
			}
			if nested && depth < 32 {
				render(kids[b.ID], childIndent, depth+1)
			}
			// Blank line between blocks, except inside runs of list items.
			if i+1 < len(list) && !(isListType(b.Type) && isListType(list[i+1].Type)) {
				out.WriteString("\n")
			}
		}
	}
	render(roots, "", 0)
	return strings.TrimRight(out.String(), "\n") + "\n"
}

// DisplayBlockType names a block type in copy ("heading_2" → "Heading 2").
func DisplayBlockType(t string) string {
	switch t {
	case "pdf":
		return "PDF"
	case "to_do":
		return "To-do"
	case "table_of_contents":
		return "Table of contents"
	}
	s := strings.ReplaceAll(t, "_", " ")
	return strings.ToUpper(s[:1]) + s[1:]
}

func isListType(t string) bool {
	return t == "bulleted_list" || t == "numbered_list" || t == "to_do"
}

// namesFor resolves mention names for Markdown: people by name, pages by
// title when me can open them (refs from resolveRefs).
func namesFor(refs Refs) mdNames {
	return mdNames{
		user: func(id string) string {
			if u, ok := refs.Users[id]; ok {
				return u.Name
			}
			return ""
		},
		page: func(id string) string {
			if p, ok := refs.Pages[id]; ok && p.Title != nil {
				return *p.Title
			}
			return ""
		},
	}
}

var fileNameRE = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// exportMarkdown: GET /docs/pages/{id}/export (view): the page as a
// Markdown file (a database exports its rows as a table).
func (s *Service) exportMarkdown(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	p, err := requirePage(ctx, s.d.Pool, me, id, access.View)
	if err != nil {
		return err
	}
	md, err := s.pageAsMarkdown(ctx, me, p, false)
	if err != nil {
		return err
	}
	name := strings.Trim(fileNameRE.ReplaceAllString(displayTitle(p.Title), "-"), "-")
	if name == "" {
		name = "page"
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`.md"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(md))
	return nil
}

// pageAsMarkdown renders a page (title, icon, blocks; a database's first 200
// rows) for export and agent tools.
func (s *Service) pageAsMarkdown(ctx context.Context, me uuid.UUID, p *Page, withIDs bool) (string, error) {
	detail, err := s.pageDetail(ctx, me, p, true)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	title := displayTitle(p.Title)
	if p.Icon != nil {
		title = *p.Icon + " " + title
	}
	out.WriteString("# " + title + "\n\n")
	if p.IsRow && detail.Database != nil {
		for _, prop := range detail.Database.Schema {
			if prop.Type == "title" {
				continue
			}
			if v := formatValue(prop, p.Props[prop.ID], detail.Refs); v != "" {
				out.WriteString("- **" + mdEscaper.Replace(prop.Name) + ":** " + mdEscaper.Replace(v) + "\n")
			}
		}
		out.WriteString("\n")
	}
	if len(detail.Blocks) > 0 {
		out.WriteString(blocksToMarkdown(detail.Blocks, mdOptions{names: namesFor(detail.Refs), withIDs: withIDs, synced: s.syncedResolver(ctx, me, p.ID)}))
	}
	if p.Kind == "database" {
		rows, err := s.queryRows(ctx, rowQuery{db: p, limit: 200, me: me, agent: access.ViaAgent(ctx)})
		if err != nil {
			return "", err
		}
		out.WriteString("\n" + rowsTable(p.Schema, rows) + "\n")
	}
	return out.String(), nil
}

// rowsTable renders rows as a Markdown table.
func rowsTable(schema []Property, rows *RowPage) string {
	var b strings.Builder
	head := make([]string, len(schema))
	seps := make([]string, len(schema))
	for i, p := range schema {
		head[i], seps[i] = mdEscaper.Replace(p.Name), "---"
	}
	b.WriteString("| " + strings.Join(head, " | ") + " |\n| " + strings.Join(seps, " | ") + " |\n")
	for _, r := range rows.Items {
		cells := make([]string, len(schema))
		for i, p := range schema {
			v := displayTitle(r.Title)
			if p.Type != "title" {
				v = formatValue(p, r.Props[p.ID], rows.Refs)
			}
			cells[i] = strings.ReplaceAll(mdEscaper.Replace(v), "|", `\|`)
		}
		b.WriteString("| " + strings.Join(cells, " | ") + " |\n")
	}
	if rows.NextCursor != nil {
		b.WriteString("\n(First 200 rows.)\n")
	}
	return b.String()
}

// formatValue renders a row value as text (option names, people's names).
func formatValue(p Property, v any, refs Refs) string {
	return valueText(p, v, refs) // every property type (csv.go)
}

// ---- import ----------------------------------------------------------------------

// mdBlock is a parsed Markdown block.
type mdBlock struct {
	typ      string
	props    map[string]any
	children []*mdBlock
}

var (
	headingRE = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
	listRE    = regexp.MustCompile(`^(\s*)([-*+]|\d{1,9}[.)])\s+(\[( |x|X)\]\s+)?(.*)$`)
	fenceRE   = regexp.MustCompile("^(\\s*)(```+|~~~+)\\s*([A-Za-z0-9+#.\\-]*)\\s*$")
	dividerRE = regexp.MustCompile(`^\s*([-*_])(\s*[-*_]){2,}\s*$`)
	tableSep  = regexp.MustCompile(`^\s*\|?\s*:?-{1,}:?\s*(\|\s*:?-{1,}:?\s*)*\|?\s*$`)
	imageRE   = regexp.MustCompile(`^!\[([^\]]*)\]\(([^)\s]+)\)$`)
)

func textBlock(typ, md string) *mdBlock {
	return &mdBlock{typ: typ, props: map[string]any{"text": markdownToSpans(md)}}
}

// startsWithEmoji reports a leading pictographic character (a callout icon).
func startsWithEmoji(s string) (string, string, bool) {
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError || r < 0x2190 || unicode.IsLetter(r) || unicode.IsDigit(r) {
		return "", s, false
	}
	icon := s[:size]
	rest := s[size:]
	for len(rest) > 0 {
		r2, n := utf8.DecodeRuneInString(rest)
		if r2 == 0xFE0F || r2 == 0x200D || (r2 >= 0x1F3FB && r2 <= 0x1F3FF) {
			icon += rest[:n]
			rest = rest[n:]
			continue
		}
		break
	}
	return icon, strings.TrimLeft(rest, " "), true
}

// markdownToBlocks parses Markdown into blocks: headings 1–4, lists (nested
// by indentation), to-dos, quotes and callouts (a quote opening with an
// emoji), fenced code, $$ equations, dividers, tables, <details> toggles
// (a "# …" summary makes a toggle heading), <columns>/<column> layouts,
// [TOC], <!-- breadcrumb -->, images as bookmarks, a lone link to an
// allowlisted provider as an embed, and paragraphs (consecutive lines keep
// their line breaks).
func markdownToBlocks(md string, limit int) []*mdBlock {
	lines := strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n")
	var out []*mdBlock
	for i := 0; i < len(lines) && countBlocks(out) < limit; {
		part, next, _ := parseLines(lines, i, limit-countBlocks(out), 0)
		out = append(out, part...)
		if next <= i {
			next = i + 1
		}
		i = next
		if i < len(lines) && closerOf(lines[i]) != "" {
			i++ // a stray closing tag at the top level
		}
	}
	return out
}

var (
	summaryRE     = regexp.MustCompile(`^\s*<summary>(.*)</summary>\s*$`)
	summaryHeadRE = regexp.MustCompile(`^(#{1,4})\s+(.*)$`)
	columnRE      = regexp.MustCompile(`^\s*<column(?:\s+width="([0-9.]{1,6})")?\s*>\s*$`)
	tocRE         = regexp.MustCompile(`(?i)^(\[toc\]|\[\[_toc_\]\]|<!--\s*toc\s*-->)$`)
	breadcrumbRE  = regexp.MustCompile(`(?i)^<!--\s*breadcrumb\s*-->$`)
	bareURLRE     = regexp.MustCompile(`^<?(https?://[^\s<>]+)>?$`)
)

// closerOf names the closing tag a line is ("" for none): nested parses stop
// at it and the opener consumes it.
func closerOf(line string) string {
	switch strings.TrimSpace(line) {
	case "</details>":
		return "details"
	case "</column>":
		return "column"
	case "</columns>":
		return "columns"
	}
	return ""
}

// parseLines parses blocks from start until a closing tag (left unconsumed:
// next points at it) or the end.
func parseLines(lines []string, start, limit, depth int) (out []*mdBlock, next int, closer string) {
	type stackItem struct {
		indent int
		block  *mdBlock
	}
	var stack []stackItem
	count := 0
	add := func(b *mdBlock, indent int, listy bool) {
		count += 1 + countBlocks(b.children)
		if !listy {
			stack = nil
			out = append(out, b)
			return
		}
		for len(stack) > 0 && stack[len(stack)-1].indent >= indent {
			stack = stack[:len(stack)-1]
		}
		if len(stack) > 0 && len(stack) < maxBlockDepth-1 {
			parent := stack[len(stack)-1].block
			parent.children = append(parent.children, b)
		} else {
			out = append(out, b)
		}
		stack = append(stack, stackItem{indent, b})
	}
	// nestedUntil parses children until the closer and consumes it.
	nestedUntil := func(i int, want string) ([]*mdBlock, int) {
		if depth >= maxBlockDepth-1 {
			return nil, i
		}
		kids, j, c := parseLines(lines, i, limit-count, depth+1)
		if c == want {
			j++
		}
		return kids, j
	}
	i := start
	for i < len(lines) && count < limit {
		raw := lines[i]
		line := strings.TrimRight(raw, " \t")
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "":
			i++
		case closerOf(trimmed) != "":
			return out, i, closerOf(trimmed)
		case strings.HasPrefix(trimmed, "<details"):
			summary := ""
			i++
			if i < len(lines) {
				if m := summaryRE.FindStringSubmatch(lines[i]); m != nil {
					summary = m[1]
					i++
				}
			}
			var b *mdBlock
			if h := summaryHeadRE.FindStringSubmatch(strings.TrimSpace(summary)); h != nil {
				b = textBlock("heading_"+strconv.Itoa(len(h[1])), h[2])
				b.props["toggleable"] = true
			} else {
				b = textBlock("toggle", summary)
			}
			b.children, i = nestedUntil(i, "details")
			add(b, 0, false)
		case trimmed == "<columns>":
			list := &mdBlock{typ: "column_list", props: map[string]any{}}
			i++
			for i < len(lines) && count < limit {
				t := strings.TrimSpace(lines[i])
				if t == "" {
					i++
					continue
				}
				if t == "</columns>" {
					i++
					break
				}
				m := columnRE.FindStringSubmatch(t)
				if m == nil || len(list.children) >= maxColumns {
					break // not a column: the list ends here
				}
				col := &mdBlock{typ: "column", props: map[string]any{}}
				if w, err := strconv.ParseFloat(m[1], 64); err == nil && w > 0 && w <= 1 {
					col.props["width"] = w
				}
				col.children, i = nestedUntil(i+1, "column")
				list.children = append(list.children, col)
			}
			switch len(list.children) {
			case 0:
			case 1: // one column is just its content
				for _, k := range list.children[0].children {
					add(k, 0, false)
				}
			default:
				add(list, 0, false)
			}
		case fenceRE.MatchString(line):
			m := fenceRE.FindStringSubmatch(line)
			fence := m[2]
			var code []string
			i++
			for i < len(lines) && strings.TrimSpace(lines[i]) != fence {
				code = append(code, lines[i])
				i++
			}
			i++
			lang := strings.ToLower(m[3])
			if lang == "" {
				lang = "plain"
			}
			add(&mdBlock{typ: "code", props: map[string]any{"code": strings.Join(code, "\n"), "language": lang}}, 0, false)
		case trimmed == "$$":
			var expr []string
			i++
			for i < len(lines) && strings.TrimSpace(lines[i]) != "$$" {
				expr = append(expr, strings.TrimSpace(lines[i]))
				i++
			}
			i++
			add(&mdBlock{typ: "equation", props: map[string]any{"expression": strings.Join(expr, "\n")}}, 0, false)
		case len(trimmed) > 4 && strings.HasPrefix(trimmed, "$$") && strings.HasSuffix(trimmed, "$$"):
			add(&mdBlock{typ: "equation", props: map[string]any{"expression": strings.TrimSpace(trimmed[2 : len(trimmed)-2])}}, 0, false)
			i++
		case tocRE.MatchString(trimmed):
			add(&mdBlock{typ: "table_of_contents", props: map[string]any{}}, 0, false)
			i++
		case breadcrumbRE.MatchString(trimmed):
			add(&mdBlock{typ: "breadcrumb", props: map[string]any{}}, 0, false)
			i++
		case dividerRE.MatchString(line) && !listRE.MatchString(line+" x"):
			add(&mdBlock{typ: "divider", props: map[string]any{}}, 0, false)
			i++
		case headingRE.MatchString(trimmed):
			m := headingRE.FindStringSubmatch(trimmed)
			level := min(len(m[1]), 4)
			add(textBlock("heading_"+strconv.Itoa(level), strings.TrimRight(m[2], "# ")), 0, false)
			i++
		case listRE.MatchString(line):
			m := listRE.FindStringSubmatch(line)
			indent := len(strings.ReplaceAll(m[1], "\t", "    "))
			typ := "bulleted_list"
			props := map[string]any{}
			if m[3] != "" {
				typ = "to_do"
				props["checked"] = strings.EqualFold(m[4], "x")
			} else if m[2][0] >= '0' && m[2][0] <= '9' {
				typ = "numbered_list"
			}
			b := textBlock(typ, m[5])
			for k, v := range props {
				b.props[k] = v
			}
			add(b, indent, true)
			i++
		case strings.HasPrefix(trimmed, ">"):
			var parts []string
			for i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), ">") {
				parts = append(parts, strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(lines[i]), ">"), " "))
				i++
			}
			text := strings.Join(parts, "\n")
			if icon, rest, ok := startsWithEmoji(text); ok {
				b := textBlock("callout", rest)
				b.props["icon"] = icon
				add(b, 0, false)
			} else {
				add(textBlock("quote", text), 0, false)
			}
		case strings.HasPrefix(trimmed, "|") && i+1 < len(lines) && tableSep.MatchString(lines[i+1]):
			var rows [][][]Span
			for i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), "|") {
				if !tableSep.MatchString(lines[i]) && len(rows) < maxTableRows {
					rows = append(rows, tableCells(lines[i]))
				}
				i++
			}
			add(&mdBlock{typ: "table", props: map[string]any{"rows": rows, "header_row": true}}, 0, false)
		case imageRE.MatchString(trimmed):
			m := imageRE.FindStringSubmatch(trimmed)
			if link := safeLink(m[2]); link != "" && !strings.HasPrefix(link, "mailto:") {
				add(&mdBlock{typ: "bookmark", props: map[string]any{"url": link, "title": m[1]}}, 0, false)
			} else if m[1] != "" {
				add(textBlock("paragraph", m[1]), 0, false)
			}
			i++
		case bareURLRE.MatchString(trimmed) && embedLine(trimmed) != nil:
			add(embedLine(trimmed), 0, false)
			i++
		default:
			var para []string
			for i < len(lines) {
				l := strings.TrimRight(lines[i], " \t")
				t := strings.TrimSpace(l)
				if t == "" || (len(para) > 0 && startsBlock(l, lines, i)) {
					break
				}
				para = append(para, strings.TrimSuffix(strings.TrimLeft(l, " \t"), "\\"))
				i++
			}
			add(textBlock("paragraph", strings.Join(para, "\n")), 0, false)
		}
	}
	return out, i, ""
}

// embedLine is an embed block for a line that is only a link to an
// allowlisted provider.
func embedLine(t string) *mdBlock {
	m := bareURLRE.FindStringSubmatch(t)
	if m == nil {
		return nil
	}
	e, ok := embedFor(m[1])
	if !ok {
		return nil
	}
	return &mdBlock{typ: "embed", props: map[string]any{"url": e.URL}}
}

func startsBlock(l string, lines []string, i int) bool {
	t := strings.TrimSpace(l)
	return headingRE.MatchString(t) || listRE.MatchString(l) || fenceRE.MatchString(l) || strings.HasPrefix(t, ">") ||
		strings.HasPrefix(t, "<details") || closerOf(t) != "" || t == "<columns>" || t == "$$" || tocRE.MatchString(t) ||
		(dividerRE.MatchString(l) && !listRE.MatchString(l+" x")) ||
		(strings.HasPrefix(t, "|") && i+1 < len(lines) && tableSep.MatchString(lines[i+1]))
}

func tableCells(line string) [][]Span {
	t := strings.TrimSpace(line)
	t = strings.TrimSuffix(strings.TrimPrefix(t, "|"), "|")
	var cells [][]Span
	var cur strings.Builder
	for i := 0; i < len(t); i++ {
		switch {
		case t[i] == '\\' && i+1 < len(t) && t[i+1] == '|':
			cur.WriteByte('|')
			i++
		case t[i] == '|':
			cells = append(cells, markdownToSpans(strings.TrimSpace(cur.String())))
			cur.Reset()
		default:
			cur.WriteByte(t[i])
		}
	}
	cells = append(cells, markdownToSpans(strings.TrimSpace(cur.String())))
	if len(cells) > maxTableCols {
		cells = cells[:maxTableCols]
	}
	return cells
}

func countBlocks(list []*mdBlock) int {
	n := len(list)
	for _, b := range list {
		n += countBlocks(b.children)
	}
	return n
}

// blockOps turns parsed blocks into create operations under parent, placed
// between after and before (positions), children nested.
func blockOps(list []*mdBlock, parent *uuid.UUID, after, before string) []Op {
	var ops []Op
	keys := keysAfter(after, before, len(list))
	for i, b := range list {
		id := uuid.New()
		raw := map[string]json.RawMessage{}
		for k, v := range b.props {
			enc, _ := json.Marshal(v)
			raw[k] = enc
		}
		ops = append(ops, Op{Op: "create", ID: id, Type: b.typ, ParentID: parent, Position: keys[i], Props: raw})
		if len(b.children) > 0 {
			if !holdsChildren(b.typ, b.props) {
				continue
			}
			pid := id
			ops = append(ops, blockOps(b.children, &pid, "", "")...)
		}
	}
	return ops
}

func isNestable(t string) bool {
	for _, n := range nestable {
		if n == t {
			return true
		}
	}
	return false
}

const maxImportBytes = 1 << 20

type importReq struct {
	ParentID *uuid.UUID `json:"parent_id"`
	Title    string     `json:"title"`
	Markdown string     `json:"markdown"`
	Shared   bool       `json:"shared"`
}

// importMarkdown: POST /docs/orgs/{orgID}/import {markdown, title?,
// parent_id?, shared?}: a new page from Markdown (a leading "# Title"
// becomes the title when none is given).
func (s *Service) importMarkdown(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	m, _ := orgs.FromContext(ctx)
	var req importReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if len(req.Markdown) > maxImportBytes {
		return httpx.BadRequest("That file is over 1 MB. Split it into smaller pages.")
	}
	p, err := s.createFromMarkdown(ctx, me, m.OrgID, req.ParentID, req.Title, req.Markdown, req.Shared)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, p)
	return nil
}

// splitMarkdownPage reads a new page's title, icon and body the way
// createFromMarkdown stores them: an empty title comes from a leading
// "# heading", and a leading emoji becomes the icon.
func splitMarkdownPage(title, md string) (string, *string, []*mdBlock) {
	md = cleanText(md)
	if strings.TrimSpace(title) == "" {
		lines := strings.SplitN(strings.TrimLeft(md, "\n"), "\n", 2)
		if m := regexp.MustCompile(`^#\s+(.+)$`).FindStringSubmatch(strings.TrimSpace(lines[0])); m != nil {
			title = m[1]
			md = ""
			if len(lines) > 1 {
				md = lines[1]
			}
		}
	}
	if utf8.RuneCountInString(title) > maxTitleChars {
		title = string([]rune(title)[:maxTitleChars])
	}
	var icon *string
	if ic, rest, ok := startsWithEmoji(strings.TrimSpace(title)); ok && rest != "" {
		icon, title = &ic, rest
	}
	return title, icon, markdownToBlocks(md, 2000)
}

func (s *Service) createFromMarkdown(ctx context.Context, me, orgID uuid.UUID, parent *uuid.UUID, title, md string, shared bool) (*Page, error) {
	title, icon, blocks := splitMarkdownPage(title, md)
	p, err := s.create(ctx, me, orgID, createPageReq{ParentID: parent, Title: title, Icon: icon, Shared: shared})
	if err != nil {
		return nil, err
	}
	if ops := blockOps(blocks, nil, "", ""); len(ops) > 0 {
		for start := 0; start < len(ops); start += maxOpsPerBatch {
			end := min(start+maxOpsPerBatch, len(ops))
			if _, err := s.apply(ctx, me, p, ops[start:end], "import"); err != nil {
				return nil, fmt.Errorf("import blocks: %w", err)
			}
		}
	}
	return loadPageWithLevel(ctx, s, me, p.ID)
}

func loadPageWithLevel(ctx context.Context, s *Service, me, id uuid.UUID) (*Page, error) {
	p, err := loadPage(ctx, s.d.Pool, id, false)
	if err != nil {
		return nil, err
	}
	p.MyLevel, err = access.LevelOf(ctx, s.d.Pool, me, Kind, id)
	return p, err
}
