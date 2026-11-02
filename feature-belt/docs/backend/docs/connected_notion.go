package docs

// The Notion adapter (API version 2022-06-28): search, pages, block
// children, databases, comments and users, read and written with the
// person's own token. Notion only shows an integration the pages the person
// shared with it (on the consent screen, or later from a page's ••• →
// Connections), so the tree's roots are those pages.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/httpx"
)

const (
	notionVersion       = "2022-06-28"
	notionPageSize      = 100
	maxNotionBlocks     = 1500 // blocks loaded for one page; more is Partial
	maxNotionRequests   = 80   // children requests while loading one page
	maxNotionRootsPages = 3    // search pages crawled to find the tree's roots
	maxNotionRichText   = 100  // rich text objects Notion accepts in one array
	maxNotionTextChars  = 2000 // characters in one text object
)

type notion struct {
	conn *docsConn
	p    *platform
	base string
	me   uuid.UUID
	// shallow loads only a page's first top-level blocks (previews).
	shallow bool
}

func newNotion(conn *docsConn, p *platform, me uuid.UUID) *notion {
	p.headers = map[string]string{"Notion-Version": notionVersion}
	return &notion{conn: conn, p: p, base: endpoint(conn, "api", "https://api.notion.com/v1"), me: me}
}

// ---- wire types ---------------------------------------------------------------------------

type nList struct {
	Results    []json.RawMessage `json:"results"`
	NextCursor *string           `json:"next_cursor"`
	HasMore    bool              `json:"has_more"`
}

type nParent struct {
	Type       string `json:"type"`
	PageID     string `json:"page_id"`
	DatabaseID string `json:"database_id"`
	BlockID    string `json:"block_id"`
	Workspace  bool   `json:"workspace"`
}

func (p nParent) id() string {
	switch p.Type {
	case "page_id":
		return p.PageID
	case "database_id":
		return p.DatabaseID
	case "block_id":
		return p.BlockID
	}
	return ""
}

type nIcon struct {
	Type  string `json:"type"`
	Emoji string `json:"emoji"`
}

type nUser struct {
	Object    string  `json:"object"`
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	AvatarURL *string `json:"avatar_url"`
}

type nRich struct {
	Type string `json:"type"`
	Text *struct {
		Content string `json:"content"`
		Link    *struct {
			URL string `json:"url"`
		} `json:"link"`
	} `json:"text,omitempty"`
	Mention *struct {
		Type     string               `json:"type"`
		User     *nUser               `json:"user,omitempty"`
		Page     *struct{ ID string } `json:"page,omitempty"`
		Database *struct{ ID string } `json:"database,omitempty"`
		Date     *struct {
			Start string  `json:"start"`
			End   *string `json:"end"`
		} `json:"date,omitempty"`
	} `json:"mention,omitempty"`
	Equation *struct {
		Expression string `json:"expression"`
	} `json:"equation,omitempty"`
	Annotations struct {
		Bold          bool   `json:"bold"`
		Italic        bool   `json:"italic"`
		Strikethrough bool   `json:"strikethrough"`
		Underline     bool   `json:"underline"`
		Code          bool   `json:"code"`
		Color         string `json:"color"`
	} `json:"annotations"`
	PlainText string  `json:"plain_text"`
	Href      *string `json:"href"`
}

// nPage is a page or a database (object tells which).
type nPage struct {
	Object         string                     `json:"object"`
	ID             string                     `json:"id"`
	CreatedTime    time.Time                  `json:"created_time"`
	LastEditedTime time.Time                  `json:"last_edited_time"`
	Archived       bool                       `json:"archived"`
	InTrash        bool                       `json:"in_trash"`
	Icon           *nIcon                     `json:"icon"`
	Parent         nParent                    `json:"parent"`
	URL            string                     `json:"url"`
	Title          []nRich                    `json:"title"` // databases
	Properties     map[string]json.RawMessage `json:"properties"`
}

type nBlock struct {
	ID             string                     `json:"id"`
	Type           string                     `json:"type"`
	Parent         nParent                    `json:"parent"`
	HasChildren    bool                       `json:"has_children"`
	Archived       bool                       `json:"archived"`
	InTrash        bool                       `json:"in_trash"`
	LastEditedTime string                     `json:"last_edited_time"`
	CreatedTime    time.Time                  `json:"created_time"`
	Raw            map[string]json.RawMessage `json:"-"`
}

func (b *nBlock) UnmarshalJSON(data []byte) error {
	type plain nBlock
	if err := json.Unmarshal(data, (*plain)(b)); err != nil {
		return err
	}
	return json.Unmarshal(data, &b.Raw)
}

// body is the type-specific payload of a block ({rich_text, checked…}).
type nBody struct {
	RichText []nRich `json:"rich_text"`
	Checked  bool    `json:"checked"`
	Icon     *nIcon  `json:"icon"`
	Language string  `json:"language"`
	Caption  []nRich `json:"caption"`
	URL      string  `json:"url"`
	Title    string  `json:"title"`
	Name     string  `json:"name"`
	Type     string  `json:"type"`
	External *struct {
		URL string `json:"url"`
	} `json:"external"`
	File *struct {
		URL string `json:"url"`
	} `json:"file"`
	Expression      string    `json:"expression"`
	PageID          string    `json:"page_id"`
	DatabaseID      string    `json:"database_id"`
	HasColumnHeader bool      `json:"has_column_header"`
	HasRowHeader    bool      `json:"has_row_header"`
	Cells           [][]nRich `json:"cells"`
	IsToggleable    bool      `json:"is_toggleable"`
	SyncedFrom      *nParent  `json:"synced_from"`
}

func (b *nBlock) body() nBody {
	var out nBody
	if raw, ok := b.Raw[b.Type]; ok {
		_ = json.Unmarshal(raw, &out)
	}
	return out
}

// ---- small helpers ---------------------------------------------------------------------------

func (n *notion) url(path string, q url.Values) string {
	u := n.base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return u
}

// call runs one request, retrying once when Notion reports a conflicting
// transaction (409 conflict_error is transient on its side).
func (n *notion) call(ctx context.Context, method, path string, q url.Values, in, out any) error {
	err := n.p.do(ctx, method, n.url(path, q), in, out)
	if err != nil && isHTTPStatus(err, http.StatusConflict) && method != http.MethodGet {
		time.Sleep(300 * time.Millisecond)
		err = n.p.do(ctx, method, n.url(path, q), in, out)
	}
	return err
}

func (n *notion) pageURL(ref string) string {
	return "https://www.notion.so/" + strings.ReplaceAll(ref, "-", "")
}

func richPlain(rt []nRich) string {
	var b strings.Builder
	for _, r := range rt {
		b.WriteString(r.PlainText)
	}
	return b.String()
}

func emojiOf(ic *nIcon) *string {
	if ic != nil && ic.Type == "emoji" && ic.Emoji != "" {
		e := ic.Emoji
		return &e
	}
	return nil
}

// titleOf is a page's title (its title property) or a database's.
func (pg *nPage) titleOf() string {
	if pg.Object == "database" {
		return richPlain(pg.Title)
	}
	for _, raw := range pg.Properties {
		var p struct {
			Type  string  `json:"type"`
			Title []nRich `json:"title"`
		}
		if json.Unmarshal(raw, &p) == nil && p.Type == "title" {
			return richPlain(p.Title)
		}
	}
	return ""
}

func (n *notion) item(pg *nPage) ExtItem {
	kind := "page"
	if pg.Object == "database" {
		kind = "database"
	}
	it := ExtItem{ID: pg.ID, ConnectionID: n.conn.ID, Kind: kind, Title: pg.titleOf(), Icon: emojiOf(pg.Icon), HasChildren: kind == "page",
		UpdatedAt: pg.LastEditedTime, URL: pg.URL, Link: ConnectedLink(n.conn.ID, pg.ID)}
	if it.URL == "" {
		it.URL = n.pageURL(pg.ID)
	}
	if pid := pg.Parent.id(); pid != "" {
		it.ParentRef = &pid
	}
	it.Source = n.conn.source(it.URL)
	return it
}

// ---- rich text both ways -------------------------------------------------------------------------

// notionSpans converts rich text to spans, recording the people and pages
// it mentions in refs.
func notionSpans(rt []nRich, refs *Refs) []Span {
	out := make([]Span, 0, len(rt))
	for _, r := range rt {
		s := Span{Text: r.PlainText, Bold: r.Annotations.Bold, Italic: r.Annotations.Italic, Underline: r.Annotations.Underline,
			Strike: r.Annotations.Strikethrough, Code: r.Annotations.Code}
		switch r.Type {
		case "text":
			if r.Text != nil {
				s.Text = r.Text.Content
				if r.Text.Link != nil {
					s.Link = r.Text.Link.URL
				}
			}
		case "equation":
			if r.Equation != nil {
				s.Text, s.Code = r.Equation.Expression, true
			}
		case "mention":
			m := r.Mention
			switch {
			case m == nil:
			case m.Type == "user" && m.User != nil && m.User.ID != "":
				s.Mention = &Mention{Type: "user", ID: m.User.ID}
				if refs != nil {
					name := m.User.Name
					if name == "" {
						name = strings.TrimPrefix(r.PlainText, "@")
					}
					refs.Users[m.User.ID] = RefUser{ID: platformUUID(m.User.ID), Name: name, AvatarURL: m.User.AvatarURL}
				}
			case (m.Type == "page" && m.Page != nil) || (m.Type == "database" && m.Database != nil):
				id, kind := "", "page"
				if m.Page != nil {
					id = m.Page.ID
				} else {
					id, kind = m.Database.ID, "database"
				}
				s.Mention = &Mention{Type: "page", ID: id}
				if refs != nil {
					t := r.PlainText
					refs.Pages[platformUUID(id).String()] = RefPage{ID: platformUUID(id), Title: &t, Kind: kind}
				}
			case m.Type == "date" && m.Date != nil && len(m.Date.Start) >= 10:
				s.Mention = &Mention{Type: "date", Date: m.Date.Start[:10]}
			default:
				if r.Href != nil {
					s.Link = *r.Href
				}
			}
		}
		out = append(out, s)
	}
	clean, _ := sanitizeSpans(out, spanRules{})
	return clean
}

// notionRich converts spans to rich text. Mentions of people Notion knows
// (known) stay mentions; others are plain text.
func notionRich(spans []Span, known map[string]bool) ([]map[string]any, error) {
	out := []map[string]any{}
	ann := func(s Span) map[string]any {
		return map[string]any{"bold": s.Bold, "italic": s.Italic, "strikethrough": s.Strike, "underline": s.Underline, "code": s.Code, "color": "default"}
	}
	for _, s := range spans {
		if m := s.Mention; m != nil {
			switch {
			case m.Type == "user" && known[m.ID]:
				out = append(out, map[string]any{"type": "mention", "mention": map[string]any{"type": "user", "user": map[string]any{"id": m.ID}}, "annotations": ann(s)})
				continue
			case m.Type == "page":
				out = append(out, map[string]any{"type": "mention", "mention": map[string]any{"type": "page", "page": map[string]any{"id": m.ID}}, "annotations": ann(s)})
				continue
			case m.Type == "date" && validDate(m.Date):
				out = append(out, map[string]any{"type": "mention", "mention": map[string]any{"type": "date", "date": map[string]any{"start": m.Date}}, "annotations": ann(s)})
				continue
			}
		}
		text := []rune(s.Text)
		for len(text) > 0 {
			n := min(len(text), maxNotionTextChars)
			t := map[string]any{"content": string(text[:n])}
			if s.Link != "" {
				t["link"] = map[string]any{"url": s.Link}
			}
			out = append(out, map[string]any{"type": "text", "text": t, "annotations": ann(s)})
			text = text[n:]
		}
	}
	if len(out) > maxNotionRichText {
		return nil, httpx.NewError(http.StatusBadRequest, "too_much_formatting", "Notion takes up to 100 differently formatted pieces of text in one block. Simplify the formatting, or edit it in Notion.")
	}
	return out, nil
}

// ---- block conversion ---------------------------------------------------------------------------

var notionToDocs = map[string]string{
	"paragraph": "paragraph", "heading_1": "heading_1", "heading_2": "heading_2", "heading_3": "heading_3",
	"bulleted_list_item": "bulleted_list", "numbered_list_item": "numbered_list", "to_do": "to_do", "toggle": "toggle",
	"quote": "quote", "callout": "callout", "code": "code", "divider": "divider",
}

var docsToNotion = map[string]string{
	"paragraph": "paragraph", "heading_1": "heading_1", "heading_2": "heading_2", "heading_3": "heading_3",
	"bulleted_list": "bulleted_list_item", "numbered_list": "numbered_list_item", "to_do": "to_do", "toggle": "toggle",
	"quote": "quote", "callout": "callout", "code": "code", "divider": "divider",
}

// Notion's code language names where they differ from Docs'.
var notionLanguages = map[string]string{"plain text": "plain", "c++": "cpp", "c#": "csharp", "shell": "bash", "objective-c": "plain"}

func docsLanguage(l string) string {
	l = strings.ToLower(l)
	if v, ok := notionLanguages[l]; ok {
		return v
	}
	if languageRE.MatchString(l) {
		return l
	}
	return "plain"
}

func notionLanguage(l string) string {
	for k, v := range notionLanguages {
		if v == l && k != "shell" && k != "objective-c" {
			return k
		}
	}
	if l == "" || l == "plain" {
		return "plain text"
	}
	return l
}

// convert maps one Notion block onto a Docs block. readOnly marks blocks
// Docs shows but doesn't change (media, embeds, sub-pages, tables,
// equations, anything unsupported).
func convertNotionBlock(b *nBlock, refs *Refs) (typ string, props map[string]any, readOnly bool) {
	body := b.body()
	if t, ok := notionToDocs[b.Type]; ok {
		props = map[string]any{}
		switch t {
		case "code":
			props["code"] = richPlain(body.RichText)
			props["language"] = docsLanguage(body.Language)
		case "divider":
		default:
			props["text"] = notionSpans(body.RichText, refs)
			if t == "to_do" {
				props["checked"] = body.Checked
			}
			if t == "callout" {
				icon := "💡"
				if e := emojiOf(body.Icon); e != nil {
					icon = *e
				}
				props["icon"] = icon
			}
		}
		return t, props, false
	}
	fileURL := func() string {
		if body.External != nil {
			return body.External.URL
		}
		if body.File != nil {
			return body.File.URL
		}
		return ""
	}
	switch b.Type {
	case "image":
		return "image", map[string]any{"url": fileURL(), "caption": notionSpans(body.Caption, refs), "width": 100, "name": body.Name}, true
	case "file", "pdf", "video", "audio":
		name := body.Name
		if name == "" {
			name = strings.ToUpper(b.Type[:1]) + b.Type[1:]
		}
		return "file", map[string]any{"url": fileURL(), "name": name, "caption": notionSpans(body.Caption, refs), "size": 0}, true
	case "bookmark", "embed", "link_preview":
		return "bookmark", map[string]any{"url": body.URL, "title": "", "caption": notionSpans(body.Caption, refs)}, true
	case "child_page", "child_database":
		kind := "page"
		if b.Type == "child_database" {
			kind = "database"
		}
		t := body.Title
		refs.Pages[platformUUID(b.ID).String()] = RefPage{ID: platformUUID(b.ID), Title: &t, Kind: kind}
		return "page", map[string]any{"page_id": platformUUID(b.ID).String()}, true
	case "link_to_page":
		target := body.PageID
		kind := "page"
		if target == "" {
			target, kind = body.DatabaseID, "database"
		}
		if _, ok := refs.Pages[platformUUID(target).String()]; !ok {
			t := "Linked page"
			refs.Pages[platformUUID(target).String()] = RefPage{ID: platformUUID(target), Title: &t, Kind: kind}
		}
		return "page", map[string]any{"page_id": platformUUID(target).String()}, true
	case "equation":
		return "code", map[string]any{"code": body.Expression, "language": "latex"}, true
	}
	return "paragraph", map[string]any{"text": []Span{{Text: "This Notion block (" + strings.ReplaceAll(b.Type, "_", " ") + ") isn't shown here.", Italic: true}}}, true
}

// notionPayload is the type body Notion takes for a Docs block.
func notionPayload(typ string, props map[string]any, known map[string]bool) (string, map[string]any, error) {
	nt, ok := docsToNotion[typ]
	if !ok {
		return "", nil, errUnsupported("Notion", "Adding this kind of block")
	}
	body := map[string]any{}
	switch typ {
	case "divider":
	case "code":
		code, _ := props["code"].(string)
		lang, _ := props["language"].(string)
		rt, err := notionRich(textSpans(code), nil)
		if err != nil {
			return "", nil, err
		}
		body["rich_text"], body["language"] = rt, notionLanguage(lang)
	default:
		rt, err := notionRich(spansValue(props["text"]), known)
		if err != nil {
			return "", nil, err
		}
		body["rich_text"] = rt
		if typ == "to_do" {
			c, _ := props["checked"].(bool)
			body["checked"] = c
		}
		if typ == "callout" {
			icon, _ := props["icon"].(string)
			if icon == "" {
				icon = "💡"
			}
			body["icon"] = map[string]any{"type": "emoji", "emoji": icon}
		}
	}
	return nt, body, nil
}

// spansValue reads []Span from a props value (typed, or decoded JSON).
func spansValue(v any) []Span {
	switch t := v.(type) {
	case []Span:
		return t
	case nil:
		return nil
	default:
		b, _ := json.Marshal(t)
		var out []Span
		_ = json.Unmarshal(b, &out)
		return out
	}
}

// ---- tree and search ---------------------------------------------------------------------------

type rootsEntry struct {
	items []ExtItem
	exp   time.Time
}

// notionRoots caches each person's tree roots per connection for a minute
// (finding them crawls search).
var notionRoots = struct {
	sync.Mutex
	m map[string]rootsEntry
}{m: map[string]rootsEntry{}}

// forgetRoots drops the cached roots after a change that can alter them.
func (n *notion) forgetRoots() {
	notionRoots.Lock()
	delete(notionRoots.m, n.me.String()+"|"+n.conn.ID)
	notionRoots.Unlock()
}

func (n *notion) tree(ctx context.Context, parent, _ string, cursor string) (ExtList, error) {
	if parent == "" {
		return n.roots(ctx)
	}
	q := url.Values{"page_size": {strconv.Itoa(notionPageSize)}}
	if cursor != "" {
		q.Set("start_cursor", cursor)
	}
	var list nList
	if err := n.call(ctx, http.MethodGet, "/blocks/"+url.PathEscape(parent)+"/children", q, nil, &list); err != nil {
		return ExtList{}, err
	}
	out := ExtList{Items: []ExtItem{}}
	for _, raw := range list.Results {
		var b nBlock
		if json.Unmarshal(raw, &b) != nil || (b.Type != "child_page" && b.Type != "child_database") {
			continue
		}
		kind := "page"
		if b.Type == "child_database" {
			kind = "database"
		}
		p := parent
		it := ExtItem{ID: b.ID, ConnectionID: n.conn.ID, Kind: kind, Title: b.body().Title, HasChildren: kind == "page", ParentRef: &p,
			UpdatedAt: parseNotionTime(b.LastEditedTime), URL: n.pageURL(b.ID), Link: ConnectedLink(n.conn.ID, b.ID)}
		it.Source = n.conn.source(it.URL)
		out.Items = append(out.Items, it)
	}
	if list.HasMore && list.NextCursor != nil {
		out.NextCursor = list.NextCursor
	}
	return out, nil
}

func parseNotionTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

// roots are the shared pages and databases whose parent the integration
// can't open (or the workspace): what the person picked when connecting.
func (n *notion) roots(ctx context.Context) (ExtList, error) {
	key := n.me.String() + "|" + n.conn.ID
	notionRoots.Lock()
	e, ok := notionRoots.m[key]
	notionRoots.Unlock()
	if ok && time.Now().Before(e.exp) {
		return ExtList{Items: e.items}, nil
	}
	var all []*nPage
	cursor := ""
	for range maxNotionRootsPages {
		body := map[string]any{"page_size": notionPageSize, "sort": map[string]any{"direction": "descending", "timestamp": "last_edited_time"}}
		if cursor != "" {
			body["start_cursor"] = cursor
		}
		var list nList
		if err := n.call(ctx, http.MethodPost, "/search", nil, body, &list); err != nil {
			return ExtList{}, err
		}
		for _, raw := range list.Results {
			var pg nPage
			if json.Unmarshal(raw, &pg) == nil && !pg.Archived && !pg.InTrash {
				all = append(all, &pg)
			}
		}
		if !list.HasMore || list.NextCursor == nil {
			break
		}
		cursor = *list.NextCursor
	}
	seen := map[string]bool{}
	for _, pg := range all {
		seen[strings.ReplaceAll(pg.ID, "-", "")] = true
	}
	items := []ExtItem{}
	for _, pg := range all {
		pid := strings.ReplaceAll(pg.Parent.id(), "-", "")
		if pg.Parent.Type == "workspace" || pid == "" || !seen[pid] {
			if pg.Parent.Type == "database_id" && seen[pid] {
				continue
			}
			items = append(items, n.item(pg))
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return strings.ToLower(items[i].Title) < strings.ToLower(items[j].Title) })
	notionRoots.Lock()
	if len(notionRoots.m) > 5000 {
		notionRoots.m = map[string]rootsEntry{}
	}
	notionRoots.m[key] = rootsEntry{items: items, exp: time.Now().Add(time.Minute)}
	notionRoots.Unlock()
	return ExtList{Items: items}, nil
}

func (n *notion) search(ctx context.Context, q, cursor string, limit int) (ExtList, error) {
	if limit <= 0 || limit > notionPageSize {
		limit = 25
	}
	body := map[string]any{"query": q, "page_size": limit}
	if cursor != "" {
		body["start_cursor"] = cursor
	}
	var list nList
	if err := n.call(ctx, http.MethodPost, "/search", nil, body, &list); err != nil {
		return ExtList{}, err
	}
	out := ExtList{Items: []ExtItem{}}
	for _, raw := range list.Results {
		var pg nPage
		if json.Unmarshal(raw, &pg) == nil && !pg.Archived && !pg.InTrash {
			out.Items = append(out.Items, n.item(&pg))
		}
	}
	if list.HasMore && list.NextCursor != nil {
		out.NextCursor = list.NextCursor
	}
	return out, nil
}

// ---- loading pages ---------------------------------------------------------------------------

func (n *notion) getPage(ctx context.Context, ref string) (*nPage, error) {
	var pg nPage
	if err := n.call(ctx, http.MethodGet, "/pages/"+url.PathEscape(ref), nil, nil, &pg); err != nil {
		return nil, err
	}
	return &pg, nil
}

func (n *notion) getDatabase(ctx context.Context, ref string) (*nPage, error) {
	var db nPage
	if err := n.call(ctx, http.MethodGet, "/databases/"+url.PathEscape(ref), nil, nil, &db); err != nil {
		return nil, err
	}
	return &db, nil
}

type notionLoad struct {
	blocks   []*extBlock
	refs     Refs
	requests int
	partial  bool
	pageID   uuid.UUID
}

func (n *notion) load(ctx context.Context, ref string) (*extDoc, error) {
	pg, err := n.getPage(ctx, ref)
	if err != nil {
		if !isHTTPStatus(err, http.StatusNotFound) && !isHTTPStatus(err, http.StatusBadRequest) {
			return nil, err
		}
		db, derr := n.getDatabase(ctx, ref)
		if derr != nil {
			return nil, err
		}
		pg = db
	}
	if pg.Archived || pg.InTrash {
		return nil, httpx.NewError(http.StatusNotFound, "not_found", "This page is in Notion's trash.")
	}
	doc := &extDoc{ref: pg.ID, kind: "page", title: pg.titleOf(), icon: emojiOf(pg.Icon), url: pg.URL, pageID: platformUUID(pg.ID),
		created: pg.CreatedTime, edited: pg.LastEditedTime, canEdit: true, refs: Refs{Users: map[string]RefUser{}, Pages: map[string]RefPage{}}}
	if doc.url == "" {
		doc.url = n.pageURL(pg.ID)
	}
	if pg.Object == "database" {
		doc.kind = "database"
		doc.schema = notionSchema(pg.Properties)
		doc.views = notionViews(pg.ID, doc.schema)
		doc.blocks = []*extBlock{}
		return doc, nil
	}
	if pg.Parent.Type == "database_id" {
		if db, err := n.getDatabase(ctx, pg.Parent.DatabaseID); err == nil {
			schema := notionSchema(db.Properties)
			doc.isRow, doc.dbRef = true, db.ID
			t := db.titleOf()
			doc.db = &DatabaseInfo{ID: platformUUID(db.ID), Title: &t, Icon: emojiOf(db.Icon), Schema: schema}
			_, doc.props = notionValues(pg.Properties, schema, &doc.refs)
		}
	}
	st := &notionLoad{refs: doc.refs, pageID: doc.pageID}
	if err := n.collect(ctx, pg.ID, nil, 0, false, st); err != nil {
		return nil, err
	}
	assignPositions(st.blocks)
	doc.blocks, doc.partial = st.blocks, st.partial
	return doc, nil
}

// collect loads parent's children (paginated) into st, nesting under
// client (nil: the top level). Columns and synced blocks are flattened:
// their children join the level they sit in.
func (n *notion) collect(ctx context.Context, parent string, client *uuid.UUID, depth int, synced bool, st *notionLoad) error {
	cursor := ""
	for {
		if st.requests >= maxNotionRequests || len(st.blocks) >= maxNotionBlocks || (n.shallow && st.requests >= 1) {
			st.partial = true
			return nil
		}
		st.requests++
		q := url.Values{"page_size": {strconv.Itoa(notionPageSize)}}
		if cursor != "" {
			q.Set("start_cursor", cursor)
		}
		var list nList
		if err := n.call(ctx, http.MethodGet, "/blocks/"+url.PathEscape(parent)+"/children", q, nil, &list); err != nil {
			return err
		}
		for _, raw := range list.Results {
			var b nBlock
			if json.Unmarshal(raw, &b) != nil || b.Archived || b.InTrash {
				continue
			}
			if err := n.add(ctx, &b, parent, client, depth, synced, st); err != nil {
				return err
			}
		}
		if !list.HasMore || list.NextCursor == nil {
			return nil
		}
		cursor = *list.NextCursor
	}
}

func (n *notion) add(ctx context.Context, b *nBlock, parent string, client *uuid.UUID, depth int, synced bool, st *notionLoad) error {
	switch b.Type {
	case "column_list", "column":
		if b.HasChildren {
			return n.collect(ctx, b.ID, client, depth, synced, st)
		}
		return nil
	case "synced_block":
		if b.HasChildren {
			return n.collect(ctx, b.ID, client, depth, true, st)
		}
		return nil
	case "table_of_contents", "breadcrumb", "template":
		return nil
	}
	eb := &extBlock{platformID: b.ID, platformParent: parent, platformType: b.Type, edited: b.LastEditedTime, hasKids: b.HasChildren}
	eb.ID = platformUUID(b.ID)
	eb.PageID = st.pageID
	eb.ParentID = client
	eb.CreatedAt, eb.UpdatedAt = b.CreatedTime, parseNotionTime(b.LastEditedTime)
	if b.Type == "table" {
		rows := [][][]Span{}
		var err error
		if !n.shallow {
			rows, err = n.tableRows(ctx, b, st)
		}
		if err != nil {
			return err
		}
		body := b.body()
		eb.Type, eb.Props, eb.readOnly = "table", map[string]any{"rows": rows, "header_row": body.HasColumnHeader, "header_col": body.HasRowHeader}, true
		st.blocks = append(st.blocks, eb)
		return nil
	}
	eb.Type, eb.Props, eb.readOnly = convertNotionBlock(b, &st.refs)
	eb.readOnly = eb.readOnly || synced
	eb.synced = synced
	st.blocks = append(st.blocks, eb)
	if !b.HasChildren || b.Type == "child_page" || b.Type == "child_database" || n.shallow {
		return nil
	}
	if isNestable(eb.Type) && depth < maxBlockDepth {
		id := eb.ID
		return n.collect(ctx, b.ID, &id, depth+1, synced, st)
	}
	// Children Docs can't nest (a toggleable heading's): shown after it.
	return n.collect(ctx, b.ID, client, depth, synced, st)
}

func (n *notion) tableRows(ctx context.Context, b *nBlock, st *notionLoad) ([][][]Span, error) {
	rows := [][][]Span{}
	cursor := ""
	for len(rows) < maxTableRows {
		st.requests++
		q := url.Values{"page_size": {strconv.Itoa(notionPageSize)}}
		if cursor != "" {
			q.Set("start_cursor", cursor)
		}
		var list nList
		if err := n.call(ctx, http.MethodGet, "/blocks/"+url.PathEscape(b.ID)+"/children", q, nil, &list); err != nil {
			return nil, err
		}
		for _, raw := range list.Results {
			var r nBlock
			if json.Unmarshal(raw, &r) != nil || r.Type != "table_row" {
				continue
			}
			var cells [][]Span
			for _, c := range r.body().Cells {
				cells = append(cells, notionSpans(c, &st.refs))
			}
			rows = append(rows, cells)
		}
		if !list.HasMore || list.NextCursor == nil {
			break
		}
		cursor = *list.NextCursor
	}
	return rows, nil
}

func (n *notion) sync(ctx context.Context, sess *extSession) (*SyncResult, error) {
	doc, err := n.load(ctx, sess.ref)
	if err != nil {
		return nil, err
	}
	// Keep the editor's ids for blocks made in this session.
	for _, b := range doc.blocks {
		if id, ok := sess.byPlatform[b.platformID]; ok && id != b.ID {
			old := b.ID
			b.ID = id
			for _, c := range doc.blocks {
				if c.ParentID != nil && *c.ParentID == old {
					nid := id
					c.ParentID = &nid
				}
			}
		}
	}
	sess.reset(doc)
	sess.version++
	return sess.syncResult(newExtPage(doc, uuid.Nil, sess.version, !sess.readOnly)), nil
}

// ---- block operations ---------------------------------------------------------------------------

// coalesce merges consecutive updates of one block (typing) into one, so a
// burst of keystrokes is one platform write.
func coalesce(ops []Op) []Op {
	var out []Op
	for _, op := range ops {
		if n := len(out); n > 0 && op.Op == "update" && out[n-1].Op == "update" && out[n-1].ID == op.ID && op.Type == "" && out[n-1].Type == "" {
			prev := &out[n-1]
			merged := map[string]json.RawMessage{}
			for k, v := range prev.Props {
				merged[k] = v
			}
			for k, v := range op.Props {
				merged[k] = v
			}
			prev.Props = merged
			continue
		}
		out = append(out, op)
	}
	return out
}

func (n *notion) apply(ctx context.Context, sess *extSession, ops []Op) (*OpsResult, error) {
	res := sess.result("", n.me)
	ops = coalesce(ops)
	for i, op := range ops {
		var err error
		switch op.Op {
		case "update":
			err = n.update(ctx, sess, op, res)
		case "create":
			err = n.create(ctx, sess, op, res)
		case "delete":
			err = n.remove(ctx, sess, op, res)
		case "move":
			reject(res, op, "unsupported", "Moving blocks isn't available for Notion pages here. Move it in Notion.")
		default:
			reject(res, op, "invalid_op", "Unknown operation.")
		}
		if err != nil {
			// The platform refused: later operations may depend on this one.
			rejectAll(res, ops[i:], err)
			break
		}
	}
	return res, nil
}

func (n *notion) getBlock(ctx context.Context, id string) (*nBlock, error) {
	var b nBlock
	if err := n.call(ctx, http.MethodGet, "/blocks/"+url.PathEscape(id), nil, nil, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

// sameContent reports whether two text blocks' props show the same content.
func sameContent(a, b map[string]any) bool {
	if !spansEqual(spansValue(a["text"]), spansValue(b["text"])) {
		return false
	}
	for _, k := range []string{"checked", "icon", "code", "language"} {
		if fmt.Sprint(a[k]) != fmt.Sprint(b[k]) {
			return false
		}
	}
	return true
}

var errChanged = func(platform string) (string, string) {
	return "changed_on_platform", "Someone changed this in " + platform + " since you opened the page. Reload to see their version, then make your change again."
}

func (n *notion) update(ctx context.Context, sess *extSession, op Op, res *OpsResult) error {
	eb := sess.blocks[op.ID]
	if eb == nil {
		reject(res, op, "not_found", "This block isn't on the page anymore. Reload to see the latest.")
		return nil
	}
	if eb.readOnly {
		reject(res, op, "read_only", "This block can't be changed here. Edit it in Notion.")
		return nil
	}
	fresh, err := n.getBlock(ctx, eb.platformID)
	if err != nil {
		if isHTTPStatus(err, http.StatusNotFound) {
			reject(res, op, "not_found", "This block was deleted in Notion. Reload to see the latest.")
			return nil
		}
		return err
	}
	if fresh.Archived || fresh.InTrash {
		reject(res, op, "not_found", "This block was deleted in Notion. Reload to see the latest.")
		return nil
	}
	refs := Refs{Users: map[string]RefUser{}, Pages: map[string]RefPage{}}
	_, freshProps, _ := convertNotionBlock(fresh, &refs)
	// Notion's last_edited_time is minute-granular, so the content decides:
	// anything different from what this editor last saw means someone else
	// changed it.
	if !sameContent(freshProps, eb.Props) {
		code, msg := errChanged("Notion")
		reject(res, op, code, msg)
		return nil
	}
	next := map[string]any{}
	for k, v := range eb.Props {
		next[k] = v
	}
	if raw, ok := op.Props["text"]; ok {
		spans, err := incomingSpans(raw, sess.users)
		if err != nil {
			reject(res, op, "invalid_request", err.Error())
			return nil
		}
		next["text"] = spans
	}
	for _, k := range []string{"checked", "icon", "code", "language"} {
		if raw, ok := op.Props[k]; ok {
			var v any
			_ = json.Unmarshal(raw, &v)
			if k == "language" {
				s, _ := v.(string)
				v = docsLanguage(s)
			}
			next[k] = v
		}
	}
	typ := eb.Type
	if op.Type != "" && op.Type != eb.Type {
		return n.turnInto(ctx, sess, op, eb, op.Type, next, res)
	}
	nt, body, err := notionPayload(typ, next, sess.users)
	if err != nil {
		reject(res, op, "invalid_request", err.Error())
		return nil
	}
	var updated nBlock
	if err := n.call(ctx, http.MethodPatch, "/blocks/"+url.PathEscape(eb.platformID), nil, map[string]any{nt: body}, &updated); err != nil {
		return err
	}
	_, props, _ := convertNotionBlock(&updated, &refs)
	eb.Props, eb.edited = props, updated.LastEditedTime
	sess.touched(res, eb)
	return nil
}

// turnInto changes a block's type: Notion can't, so a new block of the new
// type goes right after it and the old one is archived. The editor keeps
// its id.
func (n *notion) turnInto(ctx context.Context, sess *extSession, op Op, eb *extBlock, typ string, props map[string]any, res *OpsResult) error {
	if _, ok := docsToNotion[typ]; !ok || !isTextBlock(eb.Type) || eb.hasKids || len(sess.children(&eb.ID)) > 0 {
		reject(res, op, "unsupported", "This block can't be turned into that here. Change it in Notion.")
		return nil
	}
	if typ == "to_do" {
		if _, ok := props["checked"]; !ok {
			props["checked"] = false
		}
	}
	nt, body, err := notionPayload(typ, props, sess.users)
	if err != nil {
		reject(res, op, "invalid_request", err.Error())
		return nil
	}
	var list nList
	in := map[string]any{"children": []any{map[string]any{"object": "block", "type": nt, nt: body}}, "after": eb.platformID}
	if err := n.call(ctx, http.MethodPatch, "/blocks/"+url.PathEscape(eb.platformParent)+"/children", nil, in, &list); err != nil {
		return err
	}
	created, ok := firstCreated(list, eb.platformID)
	if !ok {
		return httpx.NewError(http.StatusBadGateway, "platform_error", "Notion didn't confirm the change. Reload to check it.")
	}
	if err := n.call(ctx, http.MethodDelete, "/blocks/"+url.PathEscape(eb.platformID), nil, nil, nil); err != nil && !isHTTPStatus(err, http.StatusNotFound) {
		return err
	}
	delete(sess.byPlatform, eb.platformID)
	refs := Refs{Users: map[string]RefUser{}, Pages: map[string]RefPage{}}
	t, p, _ := convertNotionBlock(created, &refs)
	eb.Type, eb.Props, eb.platformID, eb.platformType, eb.edited = t, p, created.ID, created.Type, created.LastEditedTime
	sess.byPlatform[created.ID] = eb.ID
	sess.touched(res, eb)
	return nil
}

// firstCreated reads the block Notion created from an append answer (its
// results list the new blocks; skip the anchor in case it's echoed).
func firstCreated(list nList, anchor string) (*nBlock, bool) {
	for _, raw := range list.Results {
		var b nBlock
		if json.Unmarshal(raw, &b) == nil && b.ID != "" && b.ID != anchor {
			return &b, true
		}
	}
	return nil, false
}

func (n *notion) create(ctx context.Context, sess *extSession, op Op, res *OpsResult) error {
	if _, ok := docsToNotion[op.Type]; !ok {
		reject(res, op, "unsupported", "Add images, files, tables and databases in Notion.")
		return nil
	}
	if sess.blocks[op.ID] != nil {
		reject(res, op, "id_taken", "That block already exists.")
		return nil
	}
	var parent *extBlock
	if op.ParentID != nil {
		parent = sess.blocks[*op.ParentID]
		if parent == nil || parent.readOnly || !isNestable(parent.Type) {
			reject(res, op, "invalid_parent", "A block can't go there on a Notion page.")
			return nil
		}
	}
	prev := sess.before(op.ParentID, op.Position, op.ID)
	if prev != nil && sess.inSynced(prev) {
		reject(res, op, "read_only", "Synced blocks can't be changed here. Edit them in Notion.")
		return nil
	}
	var target, after string
	switch {
	case prev != nil:
		target, after = prev.platformParent, prev.platformID
	case parent != nil:
		if len(sess.children(op.ParentID)) > 0 || parent.hasKids {
			reject(res, op, "unsupported", "Notion can't add a block above the first one from here. Add it below, or in Notion.")
			return nil
		}
		target = parent.platformID
	default:
		if len(sess.children(nil)) > 0 {
			reject(res, op, "unsupported", "Notion can't add a block above the first one from here. Add it below, or in Notion.")
			return nil
		}
		target = sess.ref
	}
	props := map[string]any{}
	for k, raw := range op.Props {
		if k == "text" {
			spans, err := incomingSpans(raw, sess.users)
			if err != nil {
				reject(res, op, "invalid_request", err.Error())
				return nil
			}
			props["text"] = spans
			continue
		}
		var v any
		_ = json.Unmarshal(raw, &v)
		props[k] = v
	}
	nt, body, err := notionPayload(op.Type, props, sess.users)
	if err != nil {
		reject(res, op, "invalid_request", err.Error())
		return nil
	}
	in := map[string]any{"children": []any{map[string]any{"object": "block", "type": nt, nt: body}}}
	if after != "" {
		in["after"] = after
	}
	var list nList
	if err := n.call(ctx, http.MethodPatch, "/blocks/"+url.PathEscape(target)+"/children", nil, in, &list); err != nil {
		return err
	}
	created, ok := firstCreated(list, after)
	if !ok {
		return httpx.NewError(http.StatusBadGateway, "platform_error", "Notion didn't confirm the new block. Reload to check it.")
	}
	refs := Refs{Users: map[string]RefUser{}, Pages: map[string]RefPage{}}
	t, p, _ := convertNotionBlock(created, &refs)
	eb := &extBlock{platformID: created.ID, platformParent: target, platformType: created.Type, edited: created.LastEditedTime}
	eb.ID, eb.PageID, eb.ParentID, eb.Type, eb.Props, eb.Position = op.ID, sess.pageID, op.ParentID, t, p, op.Position
	eb.CreatedAt = time.Now().UTC()
	sess.blocks[op.ID] = eb
	sess.byPlatform[created.ID] = op.ID
	if parent != nil {
		parent.hasKids = true
	}
	sess.touched(res, eb)
	return nil
}

// inSynced reports a block shown from a synced block (read-only, and so
// is the place after it).
func (s *extSession) inSynced(b *extBlock) bool { return b.synced }

func (n *notion) remove(ctx context.Context, sess *extSession, op Op, res *OpsResult) error {
	eb := sess.blocks[op.ID]
	if eb == nil {
		res.Deleted = append(res.Deleted, op.ID)
		return nil
	}
	if eb.readOnly {
		reject(res, op, "read_only", "This block can't be removed here. Remove it in Notion.")
		return nil
	}
	if err := n.call(ctx, http.MethodDelete, "/blocks/"+url.PathEscape(eb.platformID), nil, nil, nil); err != nil && !isHTTPStatus(err, http.StatusNotFound) {
		return err
	}
	res.Deleted = append(res.Deleted, sess.remove(op.ID)...)
	return nil
}

// ---- databases ---------------------------------------------------------------------------

type nProp struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Number *struct {
		Format string `json:"format"`
	} `json:"number"`
	Select      *nOptions `json:"select"`
	MultiSelect *nOptions `json:"multi_select"`
	Status      *nOptions `json:"status"`
}

type nOptions struct {
	Options []struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Color string `json:"color"`
	} `json:"options"`
	Groups []struct {
		Name      string   `json:"name"`
		OptionIDs []string `json:"option_ids"`
	} `json:"groups"`
}

var notionColors = map[string]string{"default": "neutral", "gray": "neutral", "brown": "neutral", "orange": "warning", "yellow": "warning",
	"green": "success", "blue": "info", "purple": "primary", "pink": "accent", "red": "danger"}

// Notion property types Docs edits as its own; the rest show read-only text.
var notionPropTypes = map[string]string{"title": "title", "rich_text": "text", "number": "number", "select": "select", "multi_select": "multi_select",
	"status": "status", "date": "date", "people": "person", "checkbox": "checkbox", "url": "url", "email": "email", "phone_number": "text"}

// notionSchema maps database properties onto Docs properties: title first,
// then by name (the API doesn't say Notion's column order).
func notionSchema(raw map[string]json.RawMessage) []Property {
	var out []Property
	for name, r := range raw {
		var p nProp
		if json.Unmarshal(r, &p) != nil {
			continue
		}
		if p.Name == "" {
			p.Name = name
		}
		prop := Property{ID: p.ID, Name: p.Name, Type: notionPropTypes[p.Type], Source: p.Type}
		if prop.Type == "" {
			prop.Type, prop.ReadOnly = "text", true
		}
		if p.Type == "people" {
			prop.ReadOnly = true // Notion people aren't app members
		}
		if p.Number != nil {
			switch p.Number.Format {
			case "percent", "dollar", "euro", "pound":
				prop.NumberFormat = p.Number.Format
			default:
				prop.NumberFormat = "number"
			}
		}
		opts := p.Select
		if opts == nil {
			opts = p.MultiSelect
		}
		if opts == nil {
			opts = p.Status
		}
		if opts != nil {
			group := map[string]string{}
			for i, g := range opts.Groups {
				gname := strings.ToLower(g.Name)
				k := []string{"todo", "doing", "done"}[min(i, 2)]
				switch {
				case strings.Contains(gname, "complete") || strings.Contains(gname, "done"):
					k = "done"
				case strings.Contains(gname, "progress"):
					k = "doing"
				case strings.Contains(gname, "to"):
					k = "todo"
				}
				for _, id := range g.OptionIDs {
					group[id] = k
				}
			}
			for _, o := range opts.Options {
				c := notionColors[o.Color]
				if c == "" {
					c = "neutral"
				}
				opt := Option{ID: o.ID, Name: o.Name, Color: c}
				if p.Type == "status" {
					opt.Group = group[o.ID]
					if opt.Group == "" {
						opt.Group = "todo"
					}
				}
				prop.Options = append(prop.Options, opt)
			}
		}
		out = append(out, prop)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if (out[i].Type == "title") != (out[j].Type == "title") {
			return out[i].Type == "title"
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	for i := range out {
		if out[i].Source == "title" {
			out[i].ID = "title"
		}
	}
	return out
}

// notionViews are the views Docs offers a Notion database: a table, a board
// grouped by its first status or select, a list and (with a date) a calendar.
func notionViews(dbRef string, schema []Property) []View {
	var cols []Column
	var cards []string
	group, date := "", ""
	for _, p := range schema {
		cols = append(cols, Column{Property: p.ID})
		if p.Type != "title" && len(cards) < 3 {
			cards = append(cards, p.ID)
		}
		if group == "" && (p.Type == "status" || (p.Type == "select" && !p.ReadOnly)) {
			group = p.ID
		}
		if date == "" && p.Type == "date" && !p.ReadOnly {
			date = p.ID
		}
	}
	mk := func(typ, name, pos string, cfg ViewConfig) View {
		cfg.Filters, cfg.Sorts = []Filter{}, []Sort{}
		if cfg.Columns == nil {
			cfg.Columns = []Column{}
		}
		if cfg.CardProperties == nil {
			cfg.CardProperties = []string{}
		}
		return View{ID: googleID("notion-view", dbRef, typ), DatabaseID: platformUUID(dbRef), Name: name, Type: typ, Position: pos, Config: cfg}
	}
	views := []View{mk("table", "Table", "a0", ViewConfig{Columns: cols, CardProperties: cards})}
	if group != "" {
		views = append(views, mk("board", "Board", "a1", ViewConfig{GroupBy: group, CardProperties: cards}))
	}
	views = append(views, mk("list", "List", "a2", ViewConfig{CardProperties: cards}))
	if date != "" {
		views = append(views, mk("calendar", "Calendar", "a3", ViewConfig{DateProperty: date, CardProperties: cards}))
	}
	return views
}

// notionValues maps a page's property values onto Docs values by property
// id (the title separately), recording the people in refs.
func notionValues(raw map[string]json.RawMessage, schema []Property, refs *Refs) (string, map[string]any) {
	byID := map[string]Property{}
	for _, p := range schema {
		byID[p.ID] = p
	}
	title := ""
	values := map[string]any{}
	for _, r := range raw {
		var v struct {
			ID          string                `json:"id"`
			Type        string                `json:"type"`
			Title       []nRich               `json:"title"`
			RichText    []nRich               `json:"rich_text"`
			Number      *float64              `json:"number"`
			Select      *struct{ ID string }  `json:"select"`
			Status      *struct{ ID string }  `json:"status"`
			MultiSelect []struct{ ID string } `json:"multi_select"`
			Date        *struct {
				Start string  `json:"start"`
				End   *string `json:"end"`
			} `json:"date"`
			People      []nUser                 `json:"people"`
			Checkbox    bool                    `json:"checkbox"`
			URL         *string                 `json:"url"`
			Email       *string                 `json:"email"`
			Phone       *string                 `json:"phone_number"`
			Formula     map[string]any          `json:"formula"`
			Rollup      map[string]any          `json:"rollup"`
			Relation    []map[string]any        `json:"relation"`
			CreatedTime string                  `json:"created_time"`
			EditedTime  string                  `json:"last_edited_time"`
			CreatedBy   *nUser                  `json:"created_by"`
			EditedBy    *nUser                  `json:"last_edited_by"`
			Files       []struct{ Name string } `json:"files"`
			UniqueID    *struct {
				Prefix *string `json:"prefix"`
				Number *int    `json:"number"`
			} `json:"unique_id"`
		}
		if json.Unmarshal(r, &v) != nil {
			continue
		}
		id := v.ID
		if v.Type == "title" {
			title = richPlain(v.Title)
			continue
		}
		if _, ok := byID[id]; !ok {
			continue
		}
		switch v.Type {
		case "rich_text":
			values[id] = richPlain(v.RichText)
		case "number":
			if v.Number != nil {
				values[id] = *v.Number
			} else {
				values[id] = nil
			}
		case "select":
			if v.Select != nil {
				values[id] = v.Select.ID
			} else {
				values[id] = nil
			}
		case "status":
			if v.Status != nil {
				values[id] = v.Status.ID
			} else {
				values[id] = nil
			}
		case "multi_select":
			ids := []string{}
			for _, o := range v.MultiSelect {
				ids = append(ids, o.ID)
			}
			values[id] = ids
		case "date":
			if v.Date != nil && v.Date.Start != "" {
				d := map[string]any{"start": docsDate(v.Date.Start)}
				if v.Date.End != nil && *v.Date.End != "" {
					d["end"] = docsDate(*v.Date.End)
				}
				values[id] = d
			} else {
				values[id] = nil
			}
		case "people":
			ids := []string{}
			for _, u := range v.People {
				ids = append(ids, u.ID)
				if refs != nil {
					refs.Users[u.ID] = RefUser{ID: platformUUID(u.ID), Name: nonEmpty(u.Name, "Notion user"), AvatarURL: u.AvatarURL}
				}
			}
			values[id] = ids
		case "checkbox":
			values[id] = v.Checkbox
		case "url":
			values[id] = deref(v.URL)
		case "email":
			values[id] = deref(v.Email)
		case "phone_number":
			values[id] = deref(v.Phone)
		case "formula":
			values[id] = formulaText(v.Formula)
		case "rollup":
			values[id] = formulaText(v.Rollup)
		case "relation":
			values[id] = fmt.Sprintf("%d linked", len(v.Relation))
		case "created_time":
			values[id] = docsDate(v.CreatedTime)
		case "last_edited_time":
			values[id] = docsDate(v.EditedTime)
		case "created_by", "last_edited_by":
			u := v.CreatedBy
			if v.Type == "last_edited_by" {
				u = v.EditedBy
			}
			if u != nil {
				values[id] = nonEmpty(u.Name, "Notion user")
			}
		case "files":
			var names []string
			for _, f := range v.Files {
				names = append(names, f.Name)
			}
			values[id] = strings.Join(names, ", ")
		case "unique_id":
			if v.UniqueID != nil && v.UniqueID.Number != nil {
				s := strconv.Itoa(*v.UniqueID.Number)
				if v.UniqueID.Prefix != nil && *v.UniqueID.Prefix != "" {
					s = *v.UniqueID.Prefix + "-" + s
				}
				values[id] = s
			}
		default:
			values[id] = ""
		}
	}
	return title, values
}

func formulaText(f map[string]any) string {
	if f == nil {
		return ""
	}
	t, _ := f["type"].(string)
	switch v := f[t].(type) {
	case nil:
		return ""
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		if v {
			return "Yes"
		}
		return "No"
	case map[string]any:
		if s, ok := v["start"].(string); ok {
			return docsDate(s)
		}
	case []any:
		return fmt.Sprintf("%d items", len(v))
	}
	return ""
}

// docsDate turns a Notion date or time into Docs' YYYY-MM-DD[THH:MM].
func docsDate(s string) string {
	if len(s) >= 16 && s[10] == 'T' {
		return s[:16]
	}
	if len(s) >= 10 {
		return s[:10]
	}
	return s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func nonEmpty(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

func (n *notion) row(pg *nPage, schema []Property, refs *Refs, canEdit bool) Row {
	title, values := notionValues(pg.Properties, schema, refs)
	r := Row{ID: platformUUID(pg.ID), Title: title, Icon: emojiOf(pg.Icon), Props: values, Position: "a0", CreatedAt: pg.CreatedTime, UpdatedAt: pg.LastEditedTime, MyLevel: 1}
	if canEdit {
		r.MyLevel = 3
	}
	r.URL = pg.URL
	if r.URL == "" {
		r.URL = n.pageURL(pg.ID)
	}
	r.Ref = pg.ID
	return r
}

// notionFilter translates one Docs filter into Notion's filter object
// (nil: not translatable, skipped).
func notionFilter(f Filter, schema []Property) map[string]any {
	var p Property
	ok := false
	for _, x := range schema {
		if x.ID == f.Property {
			p, ok = x, true
		}
	}
	if !ok || (p.ReadOnly && p.Source != "people") {
		return nil
	}
	nt := p.Source
	cond := func(op string, v any) map[string]any {
		return map[string]any{"property": p.ID, nt: map[string]any{op: v}}
	}
	switch f.Op {
	case "is_empty":
		return cond("is_empty", true)
	case "is_not_empty":
		return cond("is_not_empty", true)
	}
	var str string
	var num float64
	var flag bool
	hasStr := json.Unmarshal(f.Value, &str) == nil && str != ""
	hasNum := json.Unmarshal(f.Value, &num) == nil
	hasBool := json.Unmarshal(f.Value, &flag) == nil
	optionName := func(id string) string {
		for _, o := range p.Options {
			if o.ID == id {
				return o.Name
			}
		}
		return id
	}
	switch p.Type {
	case "title", "text", "url", "email":
		if !hasStr {
			return nil
		}
		ops := map[string]string{"contains": "contains", "not_contains": "does_not_contain", "is": "equals", "is_not": "does_not_equal", "starts_with": "starts_with"}
		if op, ok := ops[f.Op]; ok {
			return cond(op, str)
		}
	case "number":
		ops := map[string]string{"eq": "equals", "neq": "does_not_equal", "gt": "greater_than", "gte": "greater_than_or_equal_to", "lt": "less_than", "lte": "less_than_or_equal_to"}
		if op, ok := ops[f.Op]; ok && hasNum {
			return cond(op, num)
		}
	case "select", "status":
		if !hasStr {
			return nil
		}
		if f.Op == "is_not" {
			return cond("does_not_equal", optionName(str))
		}
		return cond("equals", optionName(str))
	case "multi_select":
		if !hasStr {
			return nil
		}
		if f.Op == "not_contains" {
			return cond("does_not_contain", optionName(str))
		}
		return cond("contains", optionName(str))
	case "person":
		if !hasStr || str == "me" {
			return nil
		}
		if f.Op == "not_contains" {
			return cond("does_not_contain", str)
		}
		return cond("contains", str)
	case "date":
		if !hasStr {
			return nil
		}
		d := relativeDate(str)
		ops := map[string]string{"is": "equals", "before": "before", "after": "after", "on_or_before": "on_or_before", "on_or_after": "on_or_after"}
		if op, ok := ops[f.Op]; ok && validDate(d) {
			return cond(op, d)
		}
	case "checkbox":
		if hasBool {
			return cond("equals", flag)
		}
	}
	return nil
}

func (n *notion) rows(ctx context.Context, dbRef string, rq extRowQuery) (*RowPage, error) {
	db, err := n.getDatabase(ctx, dbRef)
	if err != nil {
		return nil, err
	}
	schema := notionSchema(db.Properties)
	var and []any
	for _, f := range rq.filters {
		if c := notionFilter(f, schema); c != nil {
			and = append(and, c)
		}
	}
	if q := strings.TrimSpace(rq.search); q != "" {
		and = append(and, map[string]any{"property": "title", "title": map[string]any{"contains": q}})
	}
	limit := rq.limit
	if limit <= 0 || limit > notionPageSize {
		limit = notionPageSize
	}
	body := map[string]any{"page_size": limit}
	if len(and) > 0 {
		body["filter"] = map[string]any{"and": and}
	}
	var sorts []any
	for _, s := range rq.sorts {
		dir := "ascending"
		if s.Direction == "desc" {
			dir = "descending"
		}
		switch s.Property {
		case "created_time", "last_edited_time":
			sorts = append(sorts, map[string]any{"timestamp": s.Property, "direction": dir})
		default:
			for _, p := range schema {
				if p.ID == s.Property {
					sorts = append(sorts, map[string]any{"property": p.ID, "direction": dir})
				}
			}
		}
	}
	if len(sorts) > 0 {
		body["sorts"] = sorts
	}
	if rq.cursor != "" {
		body["start_cursor"] = rq.cursor
	}
	var list nList
	if err := n.call(ctx, http.MethodPost, "/databases/"+url.PathEscape(dbRef)+"/query", nil, body, &list); err != nil {
		return nil, err
	}
	out := &RowPage{Items: []Row{}, Refs: Refs{Users: map[string]RefUser{}, Pages: map[string]RefPage{}}}
	canEdit := n.conn.writeCap != ""
	keys := keysAfter("", "", len(list.Results))
	for i, raw := range list.Results {
		var pg nPage
		if json.Unmarshal(raw, &pg) != nil || pg.Archived || pg.InTrash {
			continue
		}
		r := n.row(&pg, schema, &out.Refs, canEdit)
		r.Position = keys[i]
		out.Items = append(out.Items, r)
	}
	if list.HasMore && list.NextCursor != nil {
		out.NextCursor = list.NextCursor
	}
	return out, nil
}

// notionValue is the Notion property value for a Docs value of p.
func notionValue(p Property, raw json.RawMessage) (any, error) {
	if p.ReadOnly {
		return nil, httpx.BadRequest(p.Name + " can't be changed here. Change it in Notion.")
	}
	var v any
	_ = json.Unmarshal(raw, &v)
	isNull := v == nil
	switch p.Source {
	case "rich_text", "title":
		s, _ := v.(string)
		rt, err := notionRich(textSpans(s), nil)
		if err != nil {
			return nil, err
		}
		return map[string]any{p.Source: rt}, nil
	case "number":
		if isNull {
			return map[string]any{"number": nil}, nil
		}
		f, ok := v.(float64)
		if !ok {
			return nil, httpx.BadRequest(p.Name + " needs a number.")
		}
		return map[string]any{"number": f}, nil
	case "select", "status":
		if isNull {
			return map[string]any{p.Source: nil}, nil
		}
		id, _ := v.(string)
		if !hasOption(p, id) {
			return nil, httpx.BadRequest("Pick one of " + p.Name + "'s options. Add new options in Notion.")
		}
		return map[string]any{p.Source: map[string]any{"id": id}}, nil
	case "multi_select":
		var ids []string
		_ = json.Unmarshal(raw, &ids)
		opts := []any{}
		for _, id := range ids {
			if !hasOption(p, id) {
				return nil, httpx.BadRequest("Pick from " + p.Name + "'s options. Add new options in Notion.")
			}
			opts = append(opts, map[string]any{"id": id})
		}
		return map[string]any{"multi_select": opts}, nil
	case "date":
		if isNull {
			return map[string]any{"date": nil}, nil
		}
		var d struct {
			Start string `json:"start"`
			End   string `json:"end"`
		}
		_ = json.Unmarshal(raw, &d)
		if !validDateValue(d.Start) || (d.End != "" && !validDateValue(d.End)) {
			return nil, httpx.BadRequest(p.Name + " needs a date.")
		}
		dv := map[string]any{"start": d.Start}
		if d.End != "" {
			dv["end"] = d.End
		}
		return map[string]any{"date": dv}, nil
	case "checkbox":
		b, _ := v.(bool)
		return map[string]any{"checkbox": b}, nil
	case "url", "email", "phone_number":
		s, _ := v.(string)
		if s == "" {
			return map[string]any{p.Source: nil}, nil
		}
		if p.Source == "url" && safeLink(s) == "" {
			return nil, httpx.BadRequest(p.Name + " needs a link starting with http:// or https://.")
		}
		return map[string]any{p.Source: s}, nil
	}
	return nil, httpx.BadRequest(p.Name + " can't be changed here. Change it in Notion.")
}

func (n *notion) properties(schema []Property, title *string, values map[string]json.RawMessage) (map[string]any, error) {
	out := map[string]any{}
	for id, raw := range values {
		p, ok := findProp(schema, id)
		if !ok {
			return nil, httpx.BadRequest("That property isn't in this database anymore. Reload it.")
		}
		v, err := notionValue(p, raw)
		if err != nil {
			return nil, err
		}
		out[p.ID] = v
	}
	if title != nil {
		rt, err := notionRich(textSpans(strings.TrimSpace(*title)), nil)
		if err != nil {
			return nil, err
		}
		out["title"] = map[string]any{"title": rt}
	}
	return out, nil
}

func (n *notion) setRow(ctx context.Context, rowRef string, title *string, values map[string]json.RawMessage) (*Row, error) {
	pg, err := n.getPage(ctx, rowRef)
	if err != nil {
		return nil, err
	}
	if pg.Parent.Type != "database_id" {
		return nil, httpx.BadRequest("That Notion page isn't in a database.")
	}
	db, err := n.getDatabase(ctx, pg.Parent.DatabaseID)
	if err != nil {
		return nil, err
	}
	schema := notionSchema(db.Properties)
	props, err := n.properties(schema, title, values)
	if err != nil {
		return nil, err
	}
	var updated nPage
	if err := n.call(ctx, http.MethodPatch, "/pages/"+url.PathEscape(rowRef), nil, map[string]any{"properties": props}, &updated); err != nil {
		return nil, err
	}
	refs := Refs{Users: map[string]RefUser{}, Pages: map[string]RefPage{}}
	r := n.row(&updated, schema, &refs, true)
	return &r, nil
}

func (n *notion) createRow(ctx context.Context, dbRef, title string, values map[string]json.RawMessage) (*Row, error) {
	db, err := n.getDatabase(ctx, dbRef)
	if err != nil {
		return nil, err
	}
	schema := notionSchema(db.Properties)
	props, err := n.properties(schema, &title, values)
	if err != nil {
		return nil, err
	}
	var created nPage
	in := map[string]any{"parent": map[string]any{"database_id": dbRef}, "properties": props}
	if err := n.call(ctx, http.MethodPost, "/pages", nil, in, &created); err != nil {
		return nil, err
	}
	refs := Refs{Users: map[string]RefUser{}, Pages: map[string]RefPage{}}
	r := n.row(&created, schema, &refs, true)
	return &r, nil
}

// createPage makes a page under a page, with blocks (Docs' Markdown model;
// kinds Notion can't take from here are skipped).
func (n *notion) createPage(ctx context.Context, parentRef, title string, blocks []*mdBlock) (ExtItem, error) {
	rt, err := notionRich(textSpans(strings.TrimSpace(title)), nil)
	if err != nil {
		return ExtItem{}, err
	}
	children, err := notionChildren(blocks, 0)
	if err != nil {
		return ExtItem{}, err
	}
	if len(children) > 100 {
		children = children[:100]
	}
	in := map[string]any{"parent": map[string]any{"page_id": parentRef}, "properties": map[string]any{"title": map[string]any{"title": rt}}}
	if len(children) > 0 {
		in["children"] = children
	}
	var created nPage
	if err := n.call(ctx, http.MethodPost, "/pages", nil, in, &created); err != nil {
		return ExtItem{}, err
	}
	n.forgetRoots()
	return n.item(&created), nil
}

// notionChildren converts blocks to Notion block objects (two levels deep,
// as one request allows).
func notionChildren(list []*mdBlock, depth int) ([]any, error) {
	out := []any{}
	for _, b := range list {
		nt, body, err := notionPayload(b.typ, b.props, nil)
		if err != nil {
			continue
		}
		if len(b.children) > 0 && depth < 1 && isNestable(b.typ) {
			kids, err := notionChildren(b.children, depth+1)
			if err != nil {
				return nil, err
			}
			if len(kids) > 0 {
				body["children"] = kids
			}
		}
		out = append(out, map[string]any{"object": "block", "type": nt, nt: body})
	}
	return out, nil
}

// ---- comments ---------------------------------------------------------------------------

type nComment struct {
	ID           string    `json:"id"`
	DiscussionID string    `json:"discussion_id"`
	CreatedTime  time.Time `json:"created_time"`
	CreatedBy    nUser     `json:"created_by"`
	RichText     []nRich   `json:"rich_text"`
	Parent       nParent   `json:"parent"`
}

type userEntry struct {
	person ExtPerson
	exp    time.Time
}

// notionUsers caches names of Notion people per person and connection.
var notionUsers = struct {
	sync.Mutex
	m map[string]userEntry
}{m: map[string]userEntry{}}

func (n *notion) person(ctx context.Context, u nUser) *ExtPerson {
	if u.Name != "" {
		return &ExtPerson{ID: u.ID, Name: u.Name, AvatarURL: u.AvatarURL}
	}
	key := n.me.String() + "|" + n.conn.ID + "|" + u.ID
	notionUsers.Lock()
	e, ok := notionUsers.m[key]
	notionUsers.Unlock()
	if ok && time.Now().Before(e.exp) {
		p := e.person
		return &p
	}
	var full nUser
	p := ExtPerson{ID: u.ID, Name: "Notion user"}
	if err := n.call(ctx, http.MethodGet, "/users/"+url.PathEscape(u.ID), nil, nil, &full); err == nil && full.Name != "" {
		p.Name, p.AvatarURL = full.Name, full.AvatarURL
	}
	notionUsers.Lock()
	if len(notionUsers.m) > 20000 {
		notionUsers.m = map[string]userEntry{}
	}
	notionUsers.m[key] = userEntry{person: p, exp: time.Now().Add(10 * time.Minute)}
	notionUsers.Unlock()
	return &p
}

func (n *notion) comments(ctx context.Context, ref string) ([]ExtComment, error) {
	var all []nComment
	cursor := ""
	for range 3 {
		q := url.Values{"block_id": {ref}, "page_size": {strconv.Itoa(notionPageSize)}}
		if cursor != "" {
			q.Set("start_cursor", cursor)
		}
		var list nList
		if err := n.call(ctx, http.MethodGet, "/comments", q, nil, &list); err != nil {
			return nil, err
		}
		for _, raw := range list.Results {
			var c nComment
			if json.Unmarshal(raw, &c) == nil {
				all = append(all, c)
			}
		}
		if !list.HasMore || list.NextCursor == nil {
			break
		}
		cursor = *list.NextCursor
	}
	threads := map[string]*ExtComment{}
	var order []string
	for _, c := range all {
		ec := ExtComment{ID: c.ID, ThreadID: c.DiscussionID, Body: notionSpans(c.RichText, nil), Author: n.person(ctx, c.CreatedBy), CreatedAt: c.CreatedTime, Replies: []ExtComment{}}
		if t, ok := threads[c.DiscussionID]; ok {
			t.Replies = append(t.Replies, ec)
			continue
		}
		cp := ec
		threads[c.DiscussionID] = &cp
		order = append(order, c.DiscussionID)
	}
	out := make([]ExtComment, 0, len(order))
	for _, id := range order {
		out = append(out, *threads[id])
	}
	return out, nil
}

func (n *notion) addComment(ctx context.Context, ref string, body []Span, threadID, _ string) (*ExtComment, error) {
	rt, err := notionRich(body, nil)
	if err != nil {
		return nil, err
	}
	in := map[string]any{"rich_text": rt}
	if threadID != "" {
		in["discussion_id"] = threadID
	} else {
		in["parent"] = map[string]any{"page_id": ref}
	}
	var c nComment
	if err := n.call(ctx, http.MethodPost, "/comments", nil, in, &c); err != nil {
		return nil, err
	}
	return &ExtComment{ID: c.ID, ThreadID: c.DiscussionID, Body: notionSpans(c.RichText, nil), Author: n.person(ctx, c.CreatedBy), CreatedAt: c.CreatedTime, Replies: []ExtComment{}}, nil
}
