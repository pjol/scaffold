package docs

// Agent tools on connected Notion pages and Google Docs. Reads are the docs
// tools with a `source` (a connection id from docs_list_sources); writes
// are separate tools per platform with Source set (the assistant asks the
// person first and treats what comes back as third-party content), Target
// kind docs.external with id "<connection id>:<platform id>", an exact
// Preview, and the connection's own agent scope checked in both Preview
// (at least suggest) and Handler (edit).

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
)

// sourceSchema is the `source` property the read tools take.
const sourceSchema = `"source":{"type":"string","description":"A connection id from docs_list_sources to read that Notion workspace or Google account instead of the app's own pages (page and database ids are then that platform's ids)."}`

// withSources gives the docs read tools a `source` parameter: with it they
// read the caller's connected platform; without it, the app's own pages.
func (s *Service) withSources(tools []app.Tool) []app.Tool {
	for i, t := range tools {
		orig := t.Handler
		switch t.Name {
		case "docs_search_pages":
			t.Description += " Results from connected Notion and Google Docs accounts are included (each with its source); pass source \"app\" for the app's pages only, or a connection id for one account."
			t.InputSchema = json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","minLength":1},"org_id":{"type":"string","format":"uuid"},"limit":{"type":"integer","minimum":1,"maximum":50},` + sourceSchema + `},"required":["query"],"additionalProperties":false}`)
			t.Handler = func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				src := sourceOfInput(input)
				var out []map[string]any
				if src == "" || src == "app" {
					res, err := orig(ctx, caller, withoutSource(input))
					if err != nil {
						return nil, err
					}
					out, _ = res.([]map[string]any)
				}
				if src != "app" {
					var in struct {
						Query string `json:"query"`
						Limit int    `json:"limit"`
					}
					_ = json.Unmarshal(input, &in)
					more, err := s.toolSearchConnected(ctx, caller, src, in.Query, in.Limit)
					if err != nil && src != "" {
						return nil, err
					}
					out = append(out, more...)
				}
				if out == nil {
					out = []map[string]any{}
				}
				return out, nil
			}
		case "docs_get_page":
			t.Description += " With source, page_id is a Notion page or database id or a Google document id; the Markdown carries block ids for docs_notion_* and docs_google_* tools."
			t.InputSchema = json.RawMessage(`{"type":"object","properties":{"page_id":{"type":"string","minLength":1,"maxLength":128},` + sourceSchema + `},"required":["page_id"],"additionalProperties":false}`)
			t.Handler = func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				if src := sourceOfInput(input); src != "" && src != "app" {
					var in struct {
						PageID string `json:"page_id"`
					}
					_ = json.Unmarshal(input, &in)
					return s.toolGetConnected(ctx, caller, src, in.PageID)
				}
				return orig(ctx, caller, withoutSource(input))
			}
		case "docs_list_children":
			t.Description += " With source, lists a Notion page's subpages and databases (no page_id: the pages shared with the app; Google: recent documents)."
			t.InputSchema = json.RawMessage(`{"type":"object","properties":{"page_id":{"type":"string","maxLength":128},` + sourceSchema + `},"additionalProperties":false}`)
			t.Handler = func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					PageID string `json:"page_id"`
					Source string `json:"source"`
				}
				_ = json.Unmarshal(input, &in)
				if in.Source != "" && in.Source != "app" {
					return s.toolListConnected(ctx, caller, in.Source, in.PageID)
				}
				if in.PageID == "" {
					return nil, httpx.BadRequest("page_id is required for the app's own pages.")
				}
				return orig(ctx, caller, withoutSource(input))
			}
		case "docs_query_database":
			t.Description += " With source, database_id is a Notion database id; filters and sorts use its property names and option names."
			var schema map[string]any
			_ = json.Unmarshal(t.InputSchema, &schema)
			if props, ok := schema["properties"].(map[string]any); ok {
				props["database_id"] = map[string]any{"type": "string", "minLength": 1, "maxLength": 128}
				props["source"] = map[string]any{"type": "string", "description": "A connection id from docs_list_sources (a Notion workspace)."}
			}
			t.InputSchema, _ = json.Marshal(schema)
			t.Handler = func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				if src := sourceOfInput(input); src != "" && src != "app" {
					return s.toolQueryConnected(ctx, caller, src, input)
				}
				return orig(ctx, caller, withoutSource(input))
			}
		default:
			continue
		}
		tools[i] = t
	}
	return tools
}

func sourceOfInput(input json.RawMessage) string {
	var in struct {
		Source string `json:"source"`
	}
	_ = json.Unmarshal(input, &in)
	return strings.TrimSpace(in.Source)
}

// withoutSource drops `source` before the app's own handler decodes the
// input strictly.
func withoutSource(input json.RawMessage) json.RawMessage {
	var m map[string]json.RawMessage
	if json.Unmarshal(input, &m) != nil {
		return input
	}
	delete(m, "source")
	out, _ := json.Marshal(m)
	return out
}

// ---- reads ---------------------------------------------------------------------------

// toolConn opens one of the caller's connections for a tool after checking
// the connection's agent scope for the item.
func (s *Service) toolConn(ctx context.Context, caller *auth.Identity, connID, ref string, need app.AgentLevel, write bool) (*connSession, error) {
	if !validConnID(connID) {
		return nil, httpx.BadRequest("source must be a connection id from docs_list_sources.")
	}
	if err := s.agentAllows(ctx, caller, connID, ref, need); err != nil {
		return nil, err
	}
	return s.open(ctx, caller.UserID, uuid.Nil, connID, write)
}

func validConnID(id string) bool {
	if id == "" || len(id) > 100 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}

func (s *Service) toolListSources(ctx context.Context, caller *auth.Identity, _ json.RawMessage) (any, error) {
	conns, err := s.connectedFor(ctx, caller.UserID, uuid.Nil)
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for _, c := range conns {
		ok, err := app.AgentAllows(ctx, s.d.Modules, caller, app.ConnectionScope(c.ID), KindExternal, c.ID, app.AgentRead)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		out = append(out, map[string]any{"source": c.ID, "platform": platformLabel(c.Provider), "account": c.Account, "status": c.Status, "can_write": c.writeCap != ""})
	}
	return out, nil
}

func (s *Service) toolSearchConnected(ctx context.Context, caller *auth.Identity, src, q string, limit int) ([]map[string]any, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	conns, err := s.connectedFor(ctx, caller.UserID, uuid.Nil)
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	for _, c := range conns {
		if (src != "" && c.ID != src) || c.Status != "active" {
			continue
		}
		hits, err := s.searchConnection(ctx, caller, c, q, limit)
		if err != nil {
			if src != "" {
				return nil, err
			}
			continue
		}
		for _, h := range hits {
			out = append(out, map[string]any{"id": h.ID, "title": h.Title, "kind": h.Kind, "source": c.ID, "platform": platformLabel(c.Provider),
				"account": c.Account, "link": h.Link, "url": h.URL, "updated_at": h.UpdatedAt})
		}
	}
	return out, nil
}

// searchConnection searches one connection within the search time box,
// dropping what the connection's agent scope hides.
func (s *Service) searchConnection(ctx context.Context, caller *auth.Identity, c *docsConn, q string, limit int) ([]ExtItem, error) {
	if ok, err := app.AgentAllows(ctx, s.d.Modules, caller, app.ConnectionScope(c.ID), KindExternal, c.ID, app.AgentRead); err != nil || !ok {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, connSearchLimit)
	defer cancel()
	cs, err := s.open(ctx, caller.UserID, uuid.Nil, c.ID, false)
	if err != nil {
		return nil, err
	}
	list, err := cs.ad.search(ctx, q, "", limit)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(list.Items))
	for i, it := range list.Items {
		ids[i] = externalTarget(c.ID, it.ID)
	}
	readable, err := app.AgentReadable(ctx, s.d.Modules, caller, app.ConnectionScope(c.ID), KindExternal, ids)
	if err != nil {
		return nil, err
	}
	var out []ExtItem
	for i, it := range list.Items {
		if readable[ids[i]] {
			out = append(out, it)
		}
	}
	return out, nil
}

func (s *Service) toolGetConnected(ctx context.Context, caller *auth.Identity, src, ref string) (any, error) {
	if !validRef(ref) {
		return nil, httpx.BadRequest("page_id must be the platform's id for the page.")
	}
	cs, err := s.toolConn(ctx, caller, src, ref, app.AgentRead, false)
	if err != nil {
		return nil, err
	}
	doc, err := cs.ad.load(ctx, ref)
	if err != nil {
		return nil, err
	}
	var md strings.Builder
	title := displayTitle(doc.title)
	if doc.icon != nil {
		title = *doc.icon + " " + title
	}
	md.WriteString("# " + title + "\n\n")
	if doc.isRow && doc.db != nil {
		for _, p := range doc.db.Schema {
			if p.Type == "title" {
				continue
			}
			if v := formatValue(p, doc.props[p.ID], doc.refs); v != "" {
				md.WriteString("- **" + mdEscaper.Replace(p.Name) + ":** " + mdEscaper.Replace(v) + "\n")
			}
		}
		md.WriteString("\n")
	}
	blocks := make([]Block, 0, len(doc.blocks))
	readOnly := []string{}
	for _, b := range doc.blocks {
		blocks = append(blocks, b.Block)
		if b.readOnly {
			readOnly = append(readOnly, b.ID.String())
		}
	}
	connID := cs.conn.ID
	md.WriteString(blocksToMarkdown(blocks, mdOptions{names: namesFor(doc.refs), withIDs: true, pageLink: func(id string) string { return ConnectedLink(connID, id) }}))
	if doc.kind == "database" {
		rows, err := cs.ad.rows(ctx, ref, extRowQuery{limit: 100})
		if err == nil {
			md.WriteString("\n" + rowsTable(doc.schema, rows) + "\n")
		}
	}
	canEdit := doc.canEdit && cs.conn.writeCap != ""
	out := map[string]any{"id": doc.ref, "title": displayTitle(doc.title), "kind": doc.kind, "is_row": doc.isRow, "source": connID,
		"platform": platformLabel(cs.conn.Provider), "account": cs.conn.Account, "link": ConnectedLink(connID, doc.ref), "url": doc.url,
		"can_edit": canEdit, "read_only_blocks": readOnly, "partial": doc.partial, "markdown": md.String()}
	if doc.dbRef != "" {
		out["database_id"] = doc.dbRef
	}
	return out, nil
}

func (s *Service) toolListConnected(ctx context.Context, caller *auth.Identity, src, parent string) (any, error) {
	if parent != "" && !validRef(parent) {
		return nil, httpx.BadRequest("page_id must be the platform's id for the page.")
	}
	cs, err := s.toolConn(ctx, caller, src, parent, app.AgentRead, false)
	if err != nil {
		return nil, err
	}
	list, err := cs.ad.tree(ctx, parent, "", "")
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(list.Items))
	for i, it := range list.Items {
		ids[i] = externalTarget(cs.conn.ID, it.ID)
	}
	readable, err := app.AgentReadable(ctx, s.d.Modules, caller, app.ConnectionScope(cs.conn.ID), KindExternal, ids)
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for i, it := range list.Items {
		if readable[ids[i]] {
			out = append(out, map[string]any{"id": it.ID, "title": it.Title, "kind": it.Kind, "source": cs.conn.ID, "link": it.Link, "url": it.URL})
		}
	}
	return out, nil
}

// namedProperty finds a connected database property by name or id.
func namedProperty(schema []Property, key string) (Property, bool) {
	for _, p := range schema {
		if p.ID == key {
			return p, true
		}
	}
	for _, p := range schema {
		if strings.EqualFold(p.Name, key) {
			return p, true
		}
	}
	return Property{}, false
}

// optionValue turns option names into ids for select, status and
// multi-select values (ids pass through).
func optionValue(p Property, v json.RawMessage) json.RawMessage {
	byName := func(s string) string {
		for _, o := range p.Options {
			if o.ID == s {
				return s
			}
		}
		for _, o := range p.Options {
			if strings.EqualFold(o.Name, s) {
				return o.ID
			}
		}
		return s
	}
	switch p.Type {
	case "select", "status":
		var s string
		if json.Unmarshal(v, &s) == nil {
			out, _ := json.Marshal(byName(s))
			return out
		}
	case "multi_select":
		var list []string
		if json.Unmarshal(v, &list) == nil {
			for i := range list {
				list[i] = byName(list[i])
			}
			out, _ := json.Marshal(list)
			return out
		}
		var s string
		if json.Unmarshal(v, &s) == nil {
			out, _ := json.Marshal([]string{byName(s)})
			return out
		}
	case "date":
		var s string
		if json.Unmarshal(v, &s) == nil {
			out, _ := json.Marshal(map[string]string{"start": relativeDate(s)})
			return out
		}
	}
	return v
}

func (s *Service) toolQueryConnected(ctx context.Context, caller *auth.Identity, src string, input json.RawMessage) (any, error) {
	var in struct {
		DatabaseID string   `json:"database_id"`
		Filters    []Filter `json:"filters"`
		Sorts      []Sort   `json:"sorts"`
		Limit      int      `json:"limit"`
		Cursor     string   `json:"cursor"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, httpx.BadRequest("The input doesn't match the tool's schema.")
	}
	if !validRef(in.DatabaseID) {
		return nil, httpx.BadRequest("database_id must be the Notion database's id.")
	}
	cs, err := s.toolConn(ctx, caller, src, in.DatabaseID, app.AgentRead, false)
	if err != nil {
		return nil, err
	}
	doc, err := cs.ad.load(ctx, in.DatabaseID)
	if err != nil {
		return nil, err
	}
	if doc.kind != "database" {
		return nil, httpx.BadRequest("That isn't a database. Read it with docs_get_page.")
	}
	rq := extRowQuery{cursor: in.Cursor, limit: in.Limit}
	for _, f := range in.Filters {
		p, ok := namedProperty(doc.schema, f.Property)
		if !ok {
			return nil, httpx.BadRequest("No property named " + f.Property + ".")
		}
		f.Property, f.Value = p.ID, optionValue(p, f.Value)
		if p.Type == "date" {
			var d map[string]string
			if json.Unmarshal(f.Value, &d) == nil {
				f.Value, _ = json.Marshal(d["start"])
			}
		}
		rq.filters = append(rq.filters, f)
	}
	for _, so := range in.Sorts {
		if p, ok := namedProperty(doc.schema, so.Property); ok {
			so.Property = p.ID
		}
		rq.sorts = append(rq.sorts, so)
	}
	rows, err := cs.ad.rows(ctx, in.DatabaseID, rq)
	if err != nil {
		return nil, err
	}
	items := []map[string]any{}
	for _, r := range rows.Items {
		vals := map[string]any{}
		for _, p := range doc.schema {
			if p.Type == "title" {
				continue
			}
			vals[p.Name] = formatValue(p, r.Props[p.ID], rows.Refs)
		}
		items = append(items, map[string]any{"id": r.Ref, "title": r.Title, "values": vals, "url": r.URL, "link": ConnectedLink(cs.conn.ID, r.Ref)})
	}
	return map[string]any{"rows": items, "next_cursor": rows.NextCursor, "source": cs.conn.ID}, nil
}

// ---- writes ---------------------------------------------------------------------------

// ephemeral is an editing session for one tool call (not kept).
func ephemeral(cs *connSession, doc *extDoc) *extSession {
	sess := &extSession{id: "tool", user: cs.me, connID: cs.conn.ID, ref: doc.ref, pageID: doc.pageID, used: time.Now()}
	sess.reset(doc)
	return sess
}

// toolFailure turns rejected operations into a tool error.
func toolFailure(res *OpsResult) error {
	if len(res.Rejected) == 0 {
		return nil
	}
	r := res.Rejected[0]
	status := http.StatusBadRequest
	if r.Code == "changed_on_platform" || r.Code == "not_found" {
		status = http.StatusConflict
	}
	return httpx.NewError(status, r.Code, r.Message)
}

type extInput struct {
	Source       string          `json:"source"`
	PageID       string          `json:"page_id"`
	DocumentID   string          `json:"document_id"`
	BlockID      string          `json:"block_id"`
	AfterBlockID string          `json:"after_block_id"`
	Text         string          `json:"text"`
	ExpectedText *string         `json:"expected_text"`
	Checked      *bool           `json:"checked"`
	Markdown     string          `json:"markdown"`
	RowID        string          `json:"row_id"`
	Property     string          `json:"property"`
	Value        json.RawMessage `json:"value"`
	ParentID     string          `json:"parent_id"`
	Title        string          `json:"title"`
	ThreadID     string          `json:"thread_id"`
}

func (in extInput) page() string {
	if in.DocumentID != "" {
		return in.DocumentID
	}
	return in.PageID
}

func decodeExt(input json.RawMessage) (extInput, error) {
	var in extInput
	if err := decodeInput(input, &in); err != nil {
		return in, err
	}
	return in, nil
}

// extTarget reads a write tool's target: kind docs.external, id
// "<source>:<platform id>" (field names the id; the kind is set even for
// an empty input).
func extTarget(field string) func(json.RawMessage) (string, string) {
	return func(input json.RawMessage) (string, string) {
		var m map[string]any
		_ = json.Unmarshal(input, &m)
		src, _ := m["source"].(string)
		id, _ := m[field].(string)
		if src == "" || id == "" {
			return KindExternal, ""
		}
		return KindExternal, externalTarget(src, id)
	}
}

// updatePlan is a block edit worked out against the platform's current page.
type updatePlan struct {
	cs            *connSession
	doc           *extDoc
	sess          *extSession
	op            Op
	before, after string
	title         string
}

func (s *Service) planExtUpdate(ctx context.Context, caller *auth.Identity, input json.RawMessage, preview bool, provider string) (*updatePlan, error) {
	in, err := decodeExt(input)
	if err != nil {
		return nil, err
	}
	ref := in.page()
	if !validRef(ref) {
		return nil, httpx.BadRequest("Pass the page id docs_get_page returned.")
	}
	need := app.AgentEdit
	if preview {
		need = app.AgentSuggest
	}
	cs, err := s.toolConn(ctx, caller, in.Source, ref, need, true)
	if err != nil {
		return nil, err
	}
	if cs.conn.Provider != provider {
		return nil, httpx.BadRequest("That source isn't a " + platformLabel(provider) + " account.")
	}
	doc, err := cs.ad.load(ctx, ref)
	if err != nil {
		return nil, err
	}
	if !doc.canEdit {
		return nil, httpx.NewError(http.StatusForbidden, "connected_forbidden", "This account can't edit that page in "+platformLabel(provider)+".")
	}
	sess := ephemeral(cs, doc)
	bid, err := uuid.Parse(in.BlockID)
	if err != nil {
		return nil, httpx.BadRequest("Pass a block id from docs_get_page.")
	}
	b := sess.blocks[bid]
	if b == nil {
		return nil, httpx.NotFound("That block isn't on the page anymore. Read the page again.")
	}
	if b.readOnly {
		return nil, httpx.BadRequest("That block can't be changed from here. The person can edit it in " + platformLabel(provider) + ".")
	}
	pl := &updatePlan{cs: cs, doc: doc, sess: sess, op: Op{Op: "update", ID: bid, Props: map[string]json.RawMessage{}}, title: displayTitle(doc.title)}
	names := namesFor(doc.refs)
	switch {
	case b.Type == "code":
		cur, _ := b.Props["code"].(string)
		if in.ExpectedText != nil && *in.ExpectedText != cur {
			return nil, httpx.Conflict("That code block changed since you read it. Read the page again.")
		}
		raw, _ := json.Marshal(in.Text)
		pl.op.Props["code"] = raw
		pl.before, pl.after = cur, in.Text
	case isTextBlock(b.Type):
		cur := spansValue(b.Props["text"])
		exp := ""
		if in.ExpectedText != nil {
			exp = strings.TrimSpace(*in.ExpectedText)
		} else if provider == providerGoogle {
			return nil, httpx.BadRequest("Pass expected_text (the block's text as you read it): Google Docs paragraphs have no lasting ids.")
		}
		if in.ExpectedText != nil && exp != strings.TrimSpace(plainOf(cur)) && exp != strings.TrimSpace(spansToMarkdown(cur, names)) {
			return nil, httpx.Conflict("That block changed since you read it. Read the page again.")
		}
		next := markdownToSpans(in.Text)
		raw, _ := json.Marshal(next)
		pl.op.Props["text"] = raw
		pl.before, pl.after = spansToMarkdown(cur, names), spansToMarkdown(next, names)
		if b.Type == "to_do" && in.Checked != nil {
			rawc, _ := json.Marshal(*in.Checked)
			pl.op.Props["checked"] = rawc
			mark := func(v bool) string {
				if v {
					return "[x] "
				}
				return "[ ] "
			}
			was, _ := b.Props["checked"].(bool)
			pl.before, pl.after = mark(was)+pl.before, mark(*in.Checked)+pl.after
		}
	default:
		return nil, httpx.BadRequest("That block has no text to change.")
	}
	return pl, nil
}

type insertExtPlan struct {
	cs        *connSession
	sess      *extSession
	ops       []Op
	blocks    []*mdBlock
	title     string
	afterText string
}

func (s *Service) planExtInsert(ctx context.Context, caller *auth.Identity, input json.RawMessage, preview bool, provider string) (*insertExtPlan, error) {
	in, err := decodeExt(input)
	if err != nil {
		return nil, err
	}
	ref := in.page()
	if !validRef(ref) {
		return nil, httpx.BadRequest("Pass the page id docs_get_page returned.")
	}
	need := app.AgentEdit
	if preview {
		need = app.AgentSuggest
	}
	cs, err := s.toolConn(ctx, caller, in.Source, ref, need, true)
	if err != nil {
		return nil, err
	}
	if cs.conn.Provider != provider {
		return nil, httpx.BadRequest("That source isn't a " + platformLabel(provider) + " account.")
	}
	blocks := markdownToBlocks(cleanText(in.Markdown), 100)
	if len(blocks) == 0 {
		return nil, httpx.BadRequest("Write some Markdown to insert.")
	}
	doc, err := cs.ad.load(ctx, ref)
	if err != nil {
		return nil, err
	}
	if !doc.canEdit || doc.kind != "page" {
		return nil, httpx.NewError(http.StatusForbidden, "connected_forbidden", "This account can't add to that page in "+platformLabel(provider)+".")
	}
	sess := ephemeral(cs, doc)
	pl := &insertExtPlan{cs: cs, sess: sess, blocks: blocks, title: displayTitle(doc.title), afterText: "the end"}
	var parent *uuid.UUID
	after, before := "", ""
	if in.AfterBlockID != "" {
		id, err := uuid.Parse(in.AfterBlockID)
		b := sess.blocks[id]
		if err != nil || b == nil {
			return nil, httpx.NotFound("That block isn't on the page anymore. Read the page again.")
		}
		parent, after = b.ParentID, b.Position
		for _, sib := range sess.children(b.ParentID) {
			if sib.Position > b.Position {
				before = sib.Position
				break
			}
		}
		pl.afterText = "“" + snippet(plainOf(spansValue(b.Props["text"])), 60) + "”"
	} else if kids := sess.children(nil); len(kids) > 0 {
		after = kids[len(kids)-1].Position
	}
	pl.ops = blockOps(blocks, parent, after, before)
	return pl, nil
}

func (s *Service) connectedTools() []app.Tool {
	tools := []app.Tool{{
		Name:        "docs_list_sources",
		Description: "List the person's connected Notion workspaces and Google accounts Docs can read (source ids for the docs tools' source parameter), and whether changes are allowed.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		ReadOnly:    true,
		Handler:     s.toolListSources,
	}}
	for _, pv := range []string{providerNotion, providerGoogle} {
		provider := pv
		label := platformLabel(provider)
		prefix := "docs_" + provider + "_"
		pageField, pageNoun := "page_id", "page"
		if provider == providerGoogle {
			pageField, pageNoun = "document_id", "document"
		}
		srcProp := `"source":{"type":"string","description":"The ` + label + ` connection id (docs_list_sources)."}`
		pageProp := `"` + pageField + `":{"type":"string","minLength":1,"maxLength":128}`
		expected := `"expected_text":{"type":"string","description":"The block's current text as you read it; the change is refused if it changed since."}`
		required := `["source","` + pageField + `","block_id","text"]`
		if provider == providerGoogle {
			required = `["source","` + pageField + `","block_id","text","expected_text"]`
		}
		tools = append(tools,
			app.Tool{
				Name: prefix + "update_block",
				Description: "Replace the text of one block on a connected " + label + " " + pageNoun + " (ids from docs_get_page with source). text is inline Markdown (**bold**, _italic_, `code`, [link](https://…)); for a code block, the code. " +
					"Pass expected_text so the change is refused if someone edited the block since.",
				InputSchema: json.RawMessage(`{"type":"object","properties":{` + srcProp + `,` + pageProp + `,"block_id":{"type":"string","format":"uuid"},"text":{"type":"string","maxLength":20000},` + expected + `,"checked":{"type":"boolean"}},"required":` + required + `,"additionalProperties":false}`),
				Source:      label,
				Target:      extTarget(pageField),
				Preview: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
					pl, err := s.planExtUpdate(ctx, caller, input, true, provider)
					if err != nil {
						return app.ToolPreview{}, err
					}
					return app.ToolPreview{Summary: fmt.Sprintf("Edit a block in “%s” in %s", pl.title, label), Before: pl.before, After: pl.after, Link: ConnectedLink(pl.cs.conn.ID, pl.doc.ref)}, nil
				},
				Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
					pl, err := s.planExtUpdate(ctx, caller, input, false, provider)
					if err != nil {
						return nil, err
					}
					res, err := pl.cs.ad.apply(ctx, pl.sess, []Op{pl.op})
					if err != nil {
						return nil, err
					}
					if err := toolFailure(res); err != nil {
						return nil, err
					}
					return map[string]any{"updated": pl.op.ID, "source": pl.cs.conn.ID, "link": ConnectedLink(pl.cs.conn.ID, pl.doc.ref)}, nil
				},
			},
			app.Tool{
				Name:        prefix + "insert_blocks",
				Description: "Add content (Markdown: paragraphs, headings, lists, to-dos, quotes, code) to a connected " + label + " " + pageNoun + ", after a block (after_block_id) or at the end.",
				InputSchema: json.RawMessage(`{"type":"object","properties":{` + srcProp + `,` + pageProp + `,"after_block_id":{"type":"string","format":"uuid"},"markdown":{"type":"string","minLength":1,"maxLength":50000}},"required":["source","` + pageField + `","markdown"],"additionalProperties":false}`),
				Source:      label,
				Target:      extTarget(pageField),
				Preview: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
					pl, err := s.planExtInsert(ctx, caller, input, true, provider)
					if err != nil {
						return app.ToolPreview{}, err
					}
					return app.ToolPreview{Summary: fmt.Sprintf("Add %d blocks to “%s” in %s after %s", countBlocks(pl.blocks), pl.title, label, pl.afterText),
						After: previewMarkdown(pl.blocks), Link: ConnectedLink(pl.cs.conn.ID, pl.sess.ref)}, nil
				},
				Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
					pl, err := s.planExtInsert(ctx, caller, input, false, provider)
					if err != nil {
						return nil, err
					}
					res, err := pl.cs.ad.apply(ctx, pl.sess, pl.ops)
					if err != nil {
						return nil, err
					}
					if len(res.Blocks) == 0 {
						if err := toolFailure(res); err != nil {
							return nil, err
						}
					}
					ids := make([]uuid.UUID, 0, len(res.Blocks))
					for _, b := range res.Blocks {
						ids = append(ids, b.ID)
					}
					out := map[string]any{"inserted": ids, "source": pl.cs.conn.ID, "link": ConnectedLink(pl.cs.conn.ID, pl.sess.ref)}
					if len(res.Rejected) > 0 {
						out["skipped"] = res.Rejected
					}
					return out, nil
				},
			},
			app.Tool{
				Name:        prefix + "add_comment",
				Description: "Comment on a connected " + label + " " + pageNoun + ", or reply to a comment thread (thread_id).",
				InputSchema: json.RawMessage(`{"type":"object","properties":{` + srcProp + `,` + pageProp + `,"text":{"type":"string","minLength":1,"maxLength":2000},"thread_id":{"type":"string","maxLength":128}},"required":["source","` + pageField + `","text"],"additionalProperties":false}`),
				Source:      label,
				Target:      extTarget(pageField),
				Preview: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
					in, err := decodeExt(input)
					if err != nil {
						return app.ToolPreview{}, err
					}
					cs, title, err := s.extTitle(ctx, caller, in.Source, in.page(), app.AgentSuggest, provider)
					if err != nil {
						return app.ToolPreview{}, err
					}
					verb := "Comment on"
					if in.ThreadID != "" {
						verb = "Reply to a comment on"
					}
					return app.ToolPreview{Summary: fmt.Sprintf("%s “%s” in %s", verb, title, label), After: strings.TrimSpace(in.Text), Link: ConnectedLink(cs.conn.ID, in.page())}, nil
				},
				Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
					in, err := decodeExt(input)
					if err != nil {
						return nil, err
					}
					if !validRef(in.page()) {
						return nil, httpx.BadRequest("Pass the page id docs_get_page returned.")
					}
					cs, err := s.toolConn(ctx, caller, in.Source, in.page(), app.AgentEdit, true)
					if err != nil {
						return nil, err
					}
					if cs.conn.Provider != provider {
						return nil, httpx.BadRequest("That source isn't a " + label + " account.")
					}
					c, err := cs.ad.addComment(ctx, in.page(), textSpans(strings.TrimSpace(in.Text)), in.ThreadID, "")
					if err != nil {
						return nil, err
					}
					return map[string]any{"comment_id": c.ID, "thread_id": c.ThreadID, "source": cs.conn.ID}, nil
				},
			},
		)
	}
	// Notion: properties and new pages.
	tools = append(tools,
		app.Tool{
			Name:        "docs_notion_set_property",
			Description: "Set one property of a row in a connected Notion database (row_id from docs_query_database with source). property is its name; select and status take an option name, multi_select a list of names, date YYYY-MM-DD, checkbox true/false.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"source":{"type":"string"},"row_id":{"type":"string","minLength":1,"maxLength":128},"property":{"type":"string","minLength":1},"value":{}},"required":["source","row_id","property","value"],"additionalProperties":false}`),
			Source:      "Notion",
			Target:      extTarget("row_id"),
			Preview: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
				pl, err := s.planNotionProperty(ctx, caller, input, true)
				if err != nil {
					return app.ToolPreview{}, err
				}
				return app.ToolPreview{Summary: fmt.Sprintf("Set %s of “%s” in Notion", pl.prop.Name, pl.title), Before: pl.before, After: pl.after, Link: ConnectedLink(pl.cs.conn.ID, pl.row)}, nil
			},
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				pl, err := s.planNotionProperty(ctx, caller, input, false)
				if err != nil {
					return nil, err
				}
				var title *string
				vals := map[string]json.RawMessage{}
				if pl.prop.Type == "title" {
					var t string
					_ = json.Unmarshal(pl.raw, &t)
					title = &t
				} else {
					vals[pl.prop.ID] = pl.raw
				}
				row, err := pl.cs.ad.setRow(ctx, pl.row, title, vals)
				if err != nil {
					return nil, err
				}
				return map[string]any{"id": row.Ref, "title": row.Title, "source": pl.cs.conn.ID}, nil
			},
		},
		app.Tool{
			Name:        "docs_notion_create_page",
			Description: "Create a page inside a connected Notion page (parent_id), with an optional Markdown body. Returns the new page's id and link.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"source":{"type":"string"},"parent_id":{"type":"string","minLength":1,"maxLength":128},"title":{"type":"string","maxLength":300},"markdown":{"type":"string","maxLength":50000}},"required":["source","parent_id","title"],"additionalProperties":false}`),
			Source:      "Notion",
			Target:      extTarget("parent_id"),
			Preview: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
				in, err := decodeExt(input)
				if err != nil {
					return app.ToolPreview{}, err
				}
				cs, parent, err := s.extTitle(ctx, caller, in.Source, in.ParentID, app.AgentSuggest, providerNotion)
				if err != nil {
					return app.ToolPreview{}, err
				}
				title, err := cleanTitle(in.Title)
				if err != nil {
					return app.ToolPreview{}, err
				}
				body := previewMarkdown(markdownToBlocks(cleanText(in.Markdown), 100))
				return app.ToolPreview{Summary: fmt.Sprintf("Create the page “%s” in “%s” in Notion", displayTitle(title), parent), After: "# " + displayTitle(title) + "\n\n" + body, Link: ConnectedLink(cs.conn.ID, in.ParentID)}, nil
			},
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				in, err := decodeExt(input)
				if err != nil {
					return nil, err
				}
				if !validRef(in.ParentID) {
					return nil, httpx.BadRequest("parent_id must be a Notion page id.")
				}
				title, err := cleanTitle(in.Title)
				if err != nil {
					return nil, err
				}
				cs, err := s.toolConn(ctx, caller, in.Source, in.ParentID, app.AgentEdit, true)
				if err != nil {
					return nil, err
				}
				if cs.conn.Provider != providerNotion {
					return nil, httpx.BadRequest("That source isn't a Notion account.")
				}
				it, err := cs.ad.createPage(ctx, in.ParentID, title, markdownToBlocks(cleanText(in.Markdown), 100))
				if err != nil {
					return nil, err
				}
				return map[string]any{"id": it.ID, "title": it.Title, "link": it.Link, "url": it.URL, "source": cs.conn.ID}, nil
			},
		},
	)
	return tools
}

// extTitle opens a connection for a write's Preview or Handler and reads
// the page's title.
func (s *Service) extTitle(ctx context.Context, caller *auth.Identity, src, ref string, need app.AgentLevel, provider string) (*connSession, string, error) {
	if !validRef(ref) {
		return nil, "", httpx.BadRequest("Pass the page id docs_get_page returned.")
	}
	cs, err := s.toolConn(ctx, caller, src, ref, need, true)
	if err != nil {
		return nil, "", err
	}
	if cs.conn.Provider != provider {
		return nil, "", httpx.BadRequest("That source isn't a " + platformLabel(provider) + " account.")
	}
	if n, ok := cs.ad.(*notion); ok {
		n.shallow = true
	}
	doc, err := cs.ad.load(ctx, ref)
	if err != nil {
		return nil, "", err
	}
	return cs, displayTitle(doc.title), nil
}

type notionPropPlan struct {
	cs            *connSession
	row, title    string
	prop          Property
	raw           json.RawMessage
	before, after string
}

func (s *Service) planNotionProperty(ctx context.Context, caller *auth.Identity, input json.RawMessage, preview bool) (*notionPropPlan, error) {
	in, err := decodeExt(input)
	if err != nil {
		return nil, err
	}
	if !validRef(in.RowID) {
		return nil, httpx.BadRequest("row_id must be a Notion page id from docs_query_database.")
	}
	need := app.AgentEdit
	if preview {
		need = app.AgentSuggest
	}
	cs, err := s.toolConn(ctx, caller, in.Source, in.RowID, need, true)
	if err != nil {
		return nil, err
	}
	n, ok := cs.ad.(*notion)
	if !ok {
		return nil, httpx.BadRequest("That source isn't a Notion account.")
	}
	n.shallow = true
	doc, err := n.load(ctx, in.RowID)
	if err != nil {
		return nil, err
	}
	if !doc.isRow || doc.db == nil {
		return nil, httpx.BadRequest("That Notion page isn't a database row.")
	}
	p, ok := namedProperty(doc.db.Schema, in.Property)
	if !ok {
		return nil, httpx.BadRequest("No property named " + in.Property + " in that database.")
	}
	raw := optionValue(p, in.Value)
	if p.Type != "title" {
		if _, err := notionValue(p, raw); err != nil {
			return nil, err
		}
	}
	pl := &notionPropPlan{cs: cs, row: doc.ref, title: displayTitle(doc.title), prop: p, raw: raw}
	var next any
	_ = json.Unmarshal(raw, &next)
	if p.Type == "title" {
		pl.before, pl.after = doc.title, fmt.Sprint(next)
	} else {
		pl.before, pl.after = formatValue(p, doc.props[p.ID], doc.refs), formatValue(p, next, doc.refs)
	}
	return pl, nil
}

// ---- global search ---------------------------------------------------------------------------

// searchConnected adds hits from each of the caller's active connections
// (time-boxed per connection, in parallel) to global search.
func (s *Service) searchConnected(ctx context.Context, caller *auth.Identity, query string, limit int) []app.SearchResult {
	org, _ := app.SearchOrg(ctx)
	conns, err := s.connectedFor(ctx, caller.UserID, org)
	if err != nil || len(conns) == 0 {
		return nil
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	var out []app.SearchResult
	for _, c := range conns {
		if c.Status != "active" {
			continue
		}
		wg.Add(1)
		go func(c *docsConn) {
			defer wg.Done()
			hits, err := s.searchConnection(ctx, caller, c, query, limit)
			if err != nil {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			for i, h := range hits {
				icon := "file-text"
				if h.Kind == "database" {
					icon = "table"
				}
				title := h.Title
				if h.Icon != nil {
					title = *h.Icon + " " + title
				}
				r := app.SearchResult{Kind: KindExternal, ID: externalTarget(c.ID, h.ID), Title: displayTitle(title), Snippet: h.Snippet, Link: h.Link,
					Icon: icon, Score: 0.6 - float64(i)*0.02, At: h.UpdatedAt, Context: app.SearchContext(platformLabel(c.Provider), c.Account), Source: h.Source}
				if org != uuid.Nil {
					r.OrgID = org.String()
				}
				out = append(out, r)
			}
		}(c)
	}
	wg.Wait()
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}
