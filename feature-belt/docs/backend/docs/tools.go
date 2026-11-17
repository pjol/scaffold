package docs

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// Tools exposes pages to agents acting for the caller, with the REST
// routes' checks: every page id goes through access.Require with the call's
// context (so an agent gets at most the person's level, capped by their
// agent scope) and every list uses the same access-filtered queries. Writes
// are single, reviewable changes with Target and Preview; there are no
// delete, sharing or publishing tools.
// Tools are the docs agent tools: the app's own pages (reads also take a
// connected source) and the connected-platform writes (connected_tools.go).
func (s *Service) Tools() []app.Tool {
	return append(append(s.withSources(s.appTools()), s.connectedTools()...), s.pageTools()...)
}

func (s *Service) appTools() []app.Tool {
	pageTarget := func(field string) func(json.RawMessage) (string, string) {
		return func(input json.RawMessage) (string, string) {
			var m map[string]any
			_ = json.Unmarshal(input, &m)
			id, _ := m[field].(string)
			return Kind, id
		}
	}
	return append(s.databaseTools(), []app.Tool{
		{
			Name:        "docs_search_pages",
			Description: "Search the pages and databases the user can open by title and text. Returns ids, titles, a snippet and links.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","minLength":1},"org_id":{"type":"string","format":"uuid"},"limit":{"type":"integer","minimum":1,"maximum":50}},"required":["query"],"additionalProperties":false}`),
			ReadOnly:    true,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					Query string     `json:"query"`
					OrgID *uuid.UUID `json:"org_id"`
					Limit int        `json:"limit"`
				}
				if err := decodeInput(input, &in); err != nil {
					return nil, err
				}
				if in.Limit <= 0 || in.Limit > 50 {
					in.Limit = 20
				}
				hits, err := s.searchPages(ctx, caller.UserID, in.Query, searchOpts{orgID: in.OrgID, minLevel: access.View, limit: in.Limit})
				if err != nil {
					return nil, err
				}
				out := make([]map[string]any, 0, len(hits))
				for _, h := range hits {
					out = append(out, map[string]any{"id": h.ID, "title": h.Title, "kind": h.Kind, "is_row": h.IsRow, "snippet": h.Snippet,
						"link": "/docs/" + h.ID.String(), "updated_at": h.UpdatedAt, "my_level": h.MyLevel})
				}
				return out, nil
			},
		},
		{
			Name: "docs_get_page",
			Description: "Read a page as Markdown. Every block is preceded by <!-- block:<id> --> so you can edit it with docs_update_block or insert after it with docs_insert_blocks. " +
				"A database row also lists its property values; a database lists its first 200 rows.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"page_id":{"type":"string","format":"uuid"}},"required":["page_id"],"additionalProperties":false}`),
			ReadOnly:    true,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					PageID uuid.UUID `json:"page_id"`
				}
				if err := decodeInput(input, &in); err != nil {
					return nil, err
				}
				p, err := requirePage(ctx, s.d.Pool, caller.UserID, in.PageID, access.View)
				if err != nil {
					return nil, err
				}
				md, err := s.pageAsMarkdown(ctx, caller.UserID, p, true)
				if err != nil {
					return nil, err
				}
				return map[string]any{"id": p.ID, "title": displayTitle(p.Title), "kind": p.Kind, "is_row": p.IsRow, "parent_id": p.ParentID,
					"my_level": p.MyLevel, "locked": p.Locked, "in_trash": p.DeletedAt != nil, "link": "/docs/" + p.ID.String(), "markdown": md}, nil
			},
		},
		{
			Name:        "docs_list_children",
			Description: "List the subpages of a page the user can open (database rows: use docs_query_database).",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"page_id":{"type":"string","format":"uuid"}},"required":["page_id"],"additionalProperties":false}`),
			ReadOnly:    true,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					PageID uuid.UUID `json:"page_id"`
				}
				if err := decodeInput(input, &in); err != nil {
					return nil, err
				}
				if _, err := requirePage(ctx, s.d.Pool, caller.UserID, in.PageID, access.View); err != nil {
					return nil, err
				}
				list, err := s.childPages(ctx, s.d.Pool, caller.UserID, in.PageID)
				if err != nil {
					return nil, err
				}
				out := make([]map[string]any, 0, len(list))
				for _, c := range list {
					out = append(out, map[string]any{"id": c.ID, "title": displayTitle(c.Title), "kind": c.Kind, "has_children": c.HasChildren, "link": "/docs/" + c.ID.String()})
				}
				return out, nil
			},
		},
		{
			Name:        "docs_create_page",
			Description: "Create a page inside a page the user can edit, with an optional Markdown body. Returns the new page's id and link.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"parent_id":{"type":"string","format":"uuid"},"title":{"type":"string","maxLength":300},"markdown":{"type":"string","maxLength":200000}},"required":["parent_id","title"],"additionalProperties":false}`),
			Target:      pageTarget("parent_id"),
			Preview: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
				var in struct {
					ParentID uuid.UUID `json:"parent_id"`
					Title    string    `json:"title"`
					Markdown string    `json:"markdown"`
				}
				if err := decodeInput(input, &in); err != nil {
					return app.ToolPreview{}, err
				}
				parent, err := requirePage(ctx, s.d.Pool, caller.UserID, in.ParentID, access.View)
				if err != nil {
					return app.ToolPreview{}, err
				}
				// What createFromMarkdown will store: title, icon and blocks.
				title, icon, blocks := splitMarkdownPage(in.Title, in.Markdown)
				if title, err = cleanTitle(title); err != nil {
					return app.ToolPreview{}, err
				}
				heading := displayTitle(title)
				if icon != nil {
					heading = *icon + " " + heading
				}
				body := ""
				if len(blocks) > 0 {
					body = previewMarkdown(blocks)
				}
				return app.ToolPreview{
					Summary: fmt.Sprintf("Create the page “%s” in “%s”", heading, displayTitle(parent.Title)),
					After:   "# " + heading + "\n\n" + body,
					Link:    "/docs/" + parent.ID.String(),
				}, nil
			},
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					ParentID uuid.UUID `json:"parent_id"`
					Title    string    `json:"title"`
					Markdown string    `json:"markdown"`
				}
				if err := decodeInput(input, &in); err != nil {
					return nil, err
				}
				parent, err := requirePage(ctx, s.d.Pool, caller.UserID, in.ParentID, access.Edit)
				if err != nil {
					return nil, err
				}
				p, err := s.createFromMarkdown(ctx, caller.UserID, parent.OrgID, &parent.ID, in.Title, in.Markdown, false)
				if err != nil {
					return nil, err
				}
				return map[string]any{"id": p.ID, "title": displayTitle(p.Title), "link": "/docs/" + p.ID.String()}, nil
			},
		},
		{
			Name: "docs_update_block",
			Description: "Replace the text of one block (a paragraph, heading, list item, to-do, quote, callout, toggle, code block or equation) on a page. " +
				"text is inline Markdown (**bold**, _italic_, `code`, [link](https://…), @[Name](user:<id>)); for a code block it is the code. " +
				"Pass expected_text (the block's current text as you read it) so the change is refused if someone edited it since.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"page_id":{"type":"string","format":"uuid"},"block_id":{"type":"string","format":"uuid"},"text":{"type":"string","maxLength":20000},"expected_text":{"type":"string"},"checked":{"type":"boolean"}},"required":["page_id","block_id","text"],"additionalProperties":false}`),
			Target:      pageTarget("page_id"),
			Preview: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
				pl, err := s.planBlockUpdate(ctx, caller, input, access.View)
				if err != nil {
					return app.ToolPreview{}, err
				}
				return app.ToolPreview{Summary: fmt.Sprintf("Edit a %s in “%s”", blockNoun(pl.block.Type), displayTitle(pl.page.Title)),
					Before: pl.before, After: pl.after, Link: "/docs/" + pl.page.ID.String()}, nil
			},
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				pl, err := s.planBlockUpdate(ctx, caller, input, access.Edit)
				if err != nil {
					return nil, err
				}
				if err := editable(pl.page, true); err != nil {
					return nil, err
				}
				res, err := s.apply(ctx, caller.UserID, pl.page, []Op{pl.op}, "agent")
				if err != nil {
					return nil, err
				}
				if len(res.Rejected) > 0 {
					return nil, httpx.Conflict(res.Rejected[0].Message)
				}
				return map[string]any{"block_id": pl.block.ID, "version": res.Version, "text": pl.after}, nil
			},
		},
		{
			Name: "docs_insert_blocks",
			Description: "Insert Markdown as new blocks on a page the user can edit, right after the block after_block_id (same nesting level), or at the end of the page when it is omitted. " +
				"Supports headings (# to ####), lists, to-dos (- [ ]), quotes, callouts (> 💡 text), code fences, $$ equations $$ (inline $x^2$), tables, dividers, " +
				"toggles (<details><summary>Title</summary> … </details>; a summary starting with # makes a toggle heading), columns (<columns><column> … </column><column> … </column></columns>), " +
				"[TOC] for a table of contents, a line with only a YouTube, Vimeo, Loom, Figma, Google Docs/Sheets/Slides/Maps, Miro, CodePen, Gist, Spotify… link for an embed, " +
				"<span color=\"red\">coloured text</span> (red_background for a highlight) and paragraphs.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"page_id":{"type":"string","format":"uuid"},"after_block_id":{"type":"string","format":"uuid"},"markdown":{"type":"string","minLength":1,"maxLength":200000}},"required":["page_id","markdown"],"additionalProperties":false}`),
			Target:      pageTarget("page_id"),
			Preview: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
				pl, err := s.planInsert(ctx, caller, input, access.View)
				if err != nil {
					return app.ToolPreview{}, err
				}
				where := "at the end"
				if pl.afterText != "" {
					where = "after “" + snippet(pl.afterText, 60) + "”"
				}
				return app.ToolPreview{Summary: fmt.Sprintf("Add %d blocks to “%s” %s", countBlocks(pl.blocks), displayTitle(pl.page.Title), where),
					After: previewMarkdown(pl.blocks), Link: "/docs/" + pl.page.ID.String()}, nil
			},
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				pl, err := s.planInsert(ctx, caller, input, access.Edit)
				if err != nil {
					return nil, err
				}
				if err := editable(pl.page, true); err != nil {
					return nil, err
				}
				ops := blockOps(pl.blocks, pl.parent, pl.after, pl.before)
				if len(ops) > maxOpsPerBatch {
					return nil, httpx.BadRequest(fmt.Sprintf("Insert up to %d blocks at a time.", maxOpsPerBatch))
				}
				res, err := s.apply(ctx, caller.UserID, pl.page, ops, "agent")
				if err != nil {
					return nil, err
				}
				ids := make([]uuid.UUID, 0, len(ops))
				for _, op := range ops {
					ids = append(ids, op.ID)
				}
				return map[string]any{"block_ids": ids, "version": res.Version}, nil
			},
		},
		{
			Name:        "docs_add_comment",
			Description: "Comment on a page the user can comment on, optionally on one block. text is inline Markdown; mention people as @[Name](user:<id>) to notify them.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"page_id":{"type":"string","format":"uuid"},"block_id":{"type":"string","format":"uuid"},"text":{"type":"string","minLength":1,"maxLength":4000}},"required":["page_id","text"],"additionalProperties":false}`),
			Target:      pageTarget("page_id"),
			Preview: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
				var in struct {
					PageID  uuid.UUID  `json:"page_id"`
					BlockID *uuid.UUID `json:"block_id"`
					Text    string     `json:"text"`
				}
				if err := decodeInput(input, &in); err != nil {
					return app.ToolPreview{}, err
				}
				p, err := requirePage(ctx, s.d.Pool, caller.UserID, in.PageID, access.View)
				if err != nil {
					return app.ToolPreview{}, err
				}
				summary := fmt.Sprintf("Comment on “%s”", displayTitle(p.Title))
				if in.BlockID != nil {
					// The block the comment attaches to, as the handler checks it.
					var plain string
					err := s.d.Pool.QueryRow(ctx, `SELECT plain FROM docs_blocks WHERE id = $1 AND page_id = $2`, *in.BlockID, p.ID).Scan(&plain)
					if db.IsNoRows(err) {
						return app.ToolPreview{}, httpx.NotFound("That block was deleted.")
					}
					if err != nil {
						return app.ToolPreview{}, err
					}
					if plain == "" {
						plain = "a block"
					}
					summary += " on “" + snippet(plain, 60) + "”"
				}
				return app.ToolPreview{Summary: summary,
					After: spansToMarkdown(markdownToSpans(in.Text), mdNames{}), Link: "/docs/" + p.ID.String()}, nil
			},
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					PageID  uuid.UUID  `json:"page_id"`
					BlockID *uuid.UUID `json:"block_id"`
					Text    string     `json:"text"`
				}
				if err := decodeInput(input, &in); err != nil {
					return nil, err
				}
				c, err := s.addComment(ctx, caller.UserID, in.PageID, commentReq{BlockID: in.BlockID, Body: markdownToSpans(in.Text)})
				if err != nil {
					return nil, err
				}
				return map[string]any{"comment_id": c.ID, "link": "/docs/" + in.PageID.String() + "?comment=" + c.ID.String()}, nil
			},
		},
	}...)
}

func decodeInput(input json.RawMessage, dst any) error {
	if len(input) == 0 {
		return nil
	}
	if err := json.Unmarshal(input, dst); err != nil {
		return httpx.BadRequest("Invalid input: " + err.Error())
	}
	return nil
}

func blockNoun(t string) string {
	switch t {
	case "heading_1", "heading_2", "heading_3", "heading_4":
		return "heading"
	case "bulleted_list", "numbered_list":
		return "list item"
	case "to_do":
		return "to-do"
	case "code":
		return "code block"
	}
	return strings.ReplaceAll(t, "_", " ")
}

// previewMarkdown renders parsed blocks back to Markdown: exactly what the
// insert will store, for the suggestion a person reviews.
func previewMarkdown(list []*mdBlock) string {
	var blocks []Block
	var walk func(l []*mdBlock, parent *uuid.UUID)
	walk = func(l []*mdBlock, parent *uuid.UUID) {
		for _, b := range l {
			id := uuid.New()
			raw, _ := json.Marshal(b.props)
			props := map[string]any{}
			_ = json.Unmarshal(raw, &props)
			blocks = append(blocks, Block{ID: id, ParentID: parent, Type: b.typ, Props: props})
			if holdsChildren(b.typ, props) {
				pid := id
				walk(b.children, &pid)
			}
		}
	}
	walk(list, nil)
	return blocksToMarkdown(blocks, mdOptions{})
}

// ---- update one block ------------------------------------------------------------

type blockPlan struct {
	page          *Page
	block         *Block
	op            Op
	before, after string
}

// planBlockUpdate validates an update and computes the exact before/after
// text. Preview and Handler share it, so the reviewed change is the stored one.
func (s *Service) planBlockUpdate(ctx context.Context, caller *auth.Identity, input json.RawMessage, min access.Level) (*blockPlan, error) {
	var in struct {
		PageID       uuid.UUID `json:"page_id"`
		BlockID      uuid.UUID `json:"block_id"`
		Text         string    `json:"text"`
		ExpectedText *string   `json:"expected_text"`
		Checked      *bool     `json:"checked"`
	}
	if err := decodeInput(input, &in); err != nil {
		return nil, err
	}
	p, err := requirePage(ctx, s.d.Pool, caller.UserID, in.PageID, min)
	if err != nil {
		return nil, err
	}
	blk, err := scanBlock(s.d.Pool.QueryRow(ctx, `SELECT `+blockCols+` FROM docs_blocks b WHERE b.id = $1 AND b.page_id = $2`, in.BlockID, in.PageID))
	if db.IsNoRows(err) {
		return nil, httpx.NotFound("That block isn't on this page.")
	}
	if err != nil {
		return nil, err
	}
	pl := &blockPlan{page: p, block: &blk, op: Op{Op: "update", ID: blk.ID, Props: map[string]json.RawMessage{}}}
	refs, err := s.resolveRefs(ctx, caller.UserID, true, firstOf(blockRefs(blk)), nil)
	if err != nil {
		return nil, err
	}
	names := namesFor(refs)
	switch {
	case blk.Type == "equation":
		cur := propString(blk.Props, "expression")
		if in.ExpectedText != nil && strings.TrimSpace(*in.ExpectedText) != strings.TrimSpace(cur) {
			return nil, httpx.Conflict("That equation changed since you read it. Read the page again.")
		}
		raw, _ := json.Marshal(strings.TrimSpace(in.Text))
		pl.op.Props["expression"] = raw
		pl.before, pl.after = cur, strings.TrimSpace(strings.ReplaceAll(in.Text, "\r\n", "\n"))
	case blk.Type == "code":
		cur := propString(blk.Props, "code")
		if in.ExpectedText != nil && *in.ExpectedText != cur {
			return nil, httpx.Conflict("That code block changed since you read it. Read the page again.")
		}
		raw, _ := json.Marshal(in.Text)
		pl.op.Props["code"] = raw
		pl.before, pl.after = cur, strings.ReplaceAll(in.Text, "\r\n", "\n")
	case isTextType(blk.Type):
		cur := textOf(blk.Props)
		if in.ExpectedText != nil && strings.TrimSpace(*in.ExpectedText) != strings.TrimSpace(plainOf(cur)) &&
			strings.TrimSpace(*in.ExpectedText) != strings.TrimSpace(spansToMarkdown(cur, names)) {
			return nil, httpx.Conflict("That block changed since you read it. Read the page again.")
		}
		next := markdownToSpans(in.Text)
		raw, _ := json.Marshal(next)
		pl.op.Props["text"] = raw
		newUsers, _ := mentionIDs(next)
		nrefs, err := s.resolveRefs(ctx, caller.UserID, true, newUsers, nil)
		if err != nil {
			return nil, err
		}
		pl.before, pl.after = spansToMarkdown(cur, names), spansToMarkdown(next, namesFor(nrefs))
		if blk.Type == "to_do" && in.Checked != nil {
			rawc, _ := json.Marshal(*in.Checked)
			pl.op.Props["checked"] = rawc
			mark := func(b bool) string {
				if b {
					return "[x] "
				}
				return "[ ] "
			}
			pl.before, pl.after = mark(propBool(blk.Props, "checked"))+pl.before, mark(*in.Checked)+pl.after
		}
	default:
		return nil, httpx.BadRequest("That block has no text to change. Use docs_insert_blocks to add content.")
	}
	return pl, nil
}

func firstOf(a, _ []uuid.UUID) []uuid.UUID { return a }

func isTextType(t string) bool {
	for _, x := range textTypes {
		if x == t {
			return true
		}
	}
	return false
}

// ---- insert blocks ---------------------------------------------------------------

type insertPlan struct {
	page          *Page
	blocks        []*mdBlock
	parent        *uuid.UUID
	after, before string
	afterText     string
}

func (s *Service) planInsert(ctx context.Context, caller *auth.Identity, input json.RawMessage, min access.Level) (*insertPlan, error) {
	var in struct {
		PageID       uuid.UUID  `json:"page_id"`
		AfterBlockID *uuid.UUID `json:"after_block_id"`
		Markdown     string     `json:"markdown"`
	}
	if err := decodeInput(input, &in); err != nil {
		return nil, err
	}
	p, err := requirePage(ctx, s.d.Pool, caller.UserID, in.PageID, min)
	if err != nil {
		return nil, err
	}
	blocks := markdownToBlocks(cleanText(in.Markdown), maxOpsPerBatch)
	if len(blocks) == 0 {
		return nil, httpx.BadRequest("Write some Markdown to insert.")
	}
	pl := &insertPlan{page: p, blocks: blocks}
	if in.AfterBlockID != nil {
		var pos, plain string
		var parent *uuid.UUID
		err := s.d.Pool.QueryRow(ctx, `SELECT parent_id, position, plain FROM docs_blocks WHERE id = $1 AND page_id = $2`, *in.AfterBlockID, p.ID).Scan(&parent, &pos, &plain)
		if db.IsNoRows(err) {
			return nil, httpx.NotFound("That block isn't on this page.")
		}
		if err != nil {
			return nil, err
		}
		pl.parent, pl.after, pl.afterText = parent, pos, plain
		if pl.afterText == "" {
			pl.afterText = "a block"
		}
		if err := s.d.Pool.QueryRow(ctx, `SELECT COALESCE(min(position), '') FROM docs_blocks WHERE page_id = $1 AND parent_id IS NOT DISTINCT FROM $2 AND position > $3`,
			p.ID, parent, pos).Scan(&pl.before); err != nil {
			return nil, err
		}
	} else if err := s.d.Pool.QueryRow(ctx, `SELECT COALESCE(max(position), '') FROM docs_blocks WHERE page_id = $1 AND parent_id IS NULL`, p.ID).Scan(&pl.after); err != nil {
		return nil, err
	}
	return pl, nil
}
