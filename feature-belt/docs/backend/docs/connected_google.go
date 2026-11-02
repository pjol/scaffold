package docs

// The Google Docs adapter: Drive (files.list for documents, comments) and
// the Docs API (documents.get for structure, documents.batchUpdate for
// edits), with the person's own token.
//
// Google Docs addresses text by index (UTF-16 code units from the start of
// the body), and paragraphs have no ids. Docs gives each body element a
// block id when a page loads and keeps, per editing session, which id each
// element has at the revision the session last saw. Every batch re-reads
// the document, refuses if its revision moved (someone else edited:
// "changed in Google Docs, reload"), computes the requests against a local
// model whose indices shift as each request applies, and sends them in one
// atomic batchUpdate guarded by writeControl.requiredRevisionId.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/httpx"
)

const (
	gDocMime     = "application/vnd.google-apps.document"
	gListFields  = "nextPageToken,files(id,name,modifiedTime,createdTime,webViewLink,owners(displayName),capabilities(canEdit))"
	gFileFields  = "id,name,modifiedTime,createdTime,webViewLink,trashed,capabilities(canEdit,canComment)"
	gCommentKeys = "id,content,author(displayName,photoLink),createdTime,resolved,deleted,quotedFileContent(value),replies(id,content,author(displayName,photoLink),createdTime,deleted)"
	gCodeFont    = "Roboto Mono"
)

type googleDocs struct {
	conn  *docsConn
	p     *platform
	docs  string
	drive string
}

func newGoogleDocs(conn *docsConn, p *platform) *googleDocs {
	drive := endpoint(conn, "drive", "")
	docs := endpoint(conn, "docs", "")
	if api := strings.TrimRight(conn.Endpoints["api"], "/"); api != "" {
		if drive == "" {
			drive = api + "/drive/v3"
		}
		if docs == "" {
			docs = api + "/v1"
		}
	}
	if drive == "" {
		drive = "https://www.googleapis.com/drive/v3"
	}
	if docs == "" {
		docs = "https://docs.googleapis.com/v1"
	}
	return &googleDocs{conn: conn, p: p, docs: docs, drive: drive}
}

// ---- wire types ---------------------------------------------------------------------------

type gFile struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	ModifiedTime time.Time `json:"modifiedTime"`
	CreatedTime  time.Time `json:"createdTime"`
	WebViewLink  string    `json:"webViewLink"`
	Trashed      bool      `json:"trashed"`
	Owners       []struct {
		DisplayName string `json:"displayName"`
	} `json:"owners"`
	Capabilities struct {
		CanEdit    bool `json:"canEdit"`
		CanComment bool `json:"canComment"`
	} `json:"capabilities"`
}

type gDocument struct {
	DocumentID string `json:"documentId"`
	Title      string `json:"title"`
	RevisionID string `json:"revisionId"`
	Body       struct {
		Content []gStructural `json:"content"`
	} `json:"body"`
	Lists map[string]struct {
		ListProperties struct {
			NestingLevels []struct {
				GlyphType   string `json:"glyphType"`
				GlyphSymbol string `json:"glyphSymbol"`
			} `json:"nestingLevels"`
		} `json:"listProperties"`
	} `json:"lists"`
	InlineObjects map[string]struct {
		InlineObjectProperties struct {
			EmbeddedObject struct {
				Title           string `json:"title"`
				Description     string `json:"description"`
				ImageProperties *struct {
					ContentURI string `json:"contentUri"`
				} `json:"imageProperties"`
			} `json:"embeddedObject"`
		} `json:"inlineObjectProperties"`
	} `json:"inlineObjects"`
}

type gStructural struct {
	StartIndex      int              `json:"startIndex"`
	EndIndex        int              `json:"endIndex"`
	Paragraph       *gParagraph      `json:"paragraph"`
	Table           *gTable          `json:"table"`
	SectionBreak    *json.RawMessage `json:"sectionBreak"`
	TableOfContents *json.RawMessage `json:"tableOfContents"`
}

type gParagraph struct {
	Elements       []gParElem `json:"elements"`
	ParagraphStyle struct {
		NamedStyleType string `json:"namedStyleType"`
	} `json:"paragraphStyle"`
	Bullet *struct {
		ListID       string `json:"listId"`
		NestingLevel int    `json:"nestingLevel"`
	} `json:"bullet"`
}

type gTextStyle struct {
	Bold          bool `json:"bold"`
	Italic        bool `json:"italic"`
	Underline     bool `json:"underline"`
	Strikethrough bool `json:"strikethrough"`
	Link          *struct {
		URL string `json:"url"`
	} `json:"link"`
	WeightedFontFamily *struct {
		FontFamily string `json:"fontFamily"`
	} `json:"weightedFontFamily"`
}

type gParElem struct {
	StartIndex int `json:"startIndex"`
	EndIndex   int `json:"endIndex"`
	TextRun    *struct {
		Content   string     `json:"content"`
		TextStyle gTextStyle `json:"textStyle"`
	} `json:"textRun"`
	InlineObjectElement *struct {
		InlineObjectID string `json:"inlineObjectId"`
	} `json:"inlineObjectElement"`
	HorizontalRule *json.RawMessage `json:"horizontalRule"`
	Person         *struct {
		PersonProperties struct {
			Name  string `json:"name"`
			Email string `json:"email"`
		} `json:"personProperties"`
	} `json:"person"`
	RichLink *struct {
		RichLinkProperties struct {
			Title string `json:"title"`
			URI   string `json:"uri"`
		} `json:"richLinkProperties"`
	} `json:"richLink"`
}

type gTable struct {
	TableRows []struct {
		TableCells []struct {
			Content []gStructural `json:"content"`
		} `json:"tableCells"`
	} `json:"tableRows"`
}

// ---- requests ---------------------------------------------------------------------------

func (g *googleDocs) pageURL(ref string) string {
	return "https://docs.google.com/document/d/" + ref + "/edit"
}

func (g *googleDocs) file(ctx context.Context, ref string) (*gFile, error) {
	var f gFile
	q := url.Values{"fields": {gFileFields}, "supportsAllDrives": {"true"}}
	if err := g.p.do(ctx, http.MethodGet, g.drive+"/files/"+url.PathEscape(ref)+"?"+q.Encode(), nil, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

func (g *googleDocs) document(ctx context.Context, ref string) (*gDocument, error) {
	var d gDocument
	if err := g.p.do(ctx, http.MethodGet, g.docs+"/documents/"+url.PathEscape(ref), nil, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

func (g *googleDocs) item(f *gFile) ExtItem {
	it := ExtItem{ID: f.ID, ConnectionID: g.conn.ID, Kind: "page", Title: f.Name, UpdatedAt: f.ModifiedTime, URL: f.WebViewLink, Link: ConnectedLink(g.conn.ID, f.ID)}
	if it.URL == "" {
		it.URL = g.pageURL(f.ID)
	}
	if len(f.Owners) > 0 {
		it.Snippet = f.Owners[0].DisplayName
	}
	it.Source = g.conn.source(it.URL)
	return it
}

func (g *googleDocs) list(ctx context.Context, q, orderBy, cursor string, limit int) (ExtList, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	v := url.Values{"q": {q}, "fields": {gListFields}, "pageSize": {itoa(limit)}, "supportsAllDrives": {"true"}, "includeItemsFromAllDrives": {"true"}}
	if orderBy != "" {
		v.Set("orderBy", orderBy)
	}
	if cursor != "" {
		v.Set("pageToken", cursor)
	}
	var out struct {
		NextPageToken string  `json:"nextPageToken"`
		Files         []gFile `json:"files"`
	}
	if err := g.p.do(ctx, http.MethodGet, g.drive+"/files?"+v.Encode(), nil, &out); err != nil {
		return ExtList{}, err
	}
	list := ExtList{Items: []ExtItem{}}
	for i := range out.Files {
		list.Items = append(list.Items, g.item(&out.Files[i]))
	}
	if out.NextPageToken != "" {
		t := out.NextPageToken
		list.NextCursor = &t
	}
	return list, nil
}

// driveQuote escapes a value inside a Drive query's single quotes.
func driveQuote(s string) string {
	return strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s)
}

func (g *googleDocs) tree(ctx context.Context, parent, section, cursor string) (ExtList, error) {
	if parent != "" {
		return ExtList{Items: []ExtItem{}}, nil
	}
	q := "mimeType = '" + gDocMime + "' and trashed = false"
	if section == "shared" {
		return g.list(ctx, q+" and sharedWithMe = true", "sharedWithMeTime desc", cursor, 50)
	}
	return g.list(ctx, q, "recency desc", cursor, 50)
}

func (g *googleDocs) search(ctx context.Context, q, cursor string, limit int) (ExtList, error) {
	t := driveQuote(strings.TrimSpace(q))
	return g.list(ctx, "mimeType = '"+gDocMime+"' and trashed = false and (name contains '"+t+"' or fullText contains '"+t+"')", "", cursor, limit)
}

// ---- reading documents ---------------------------------------------------------------------------

var monoFonts = map[string]bool{"courier new": true, "roboto mono": true, "source code pro": true, "consolas": true, "inconsolata": true, "fira code": true,
	"jetbrains mono": true, "ubuntu mono": true, "ibm plex mono": true, "space mono": true, "courier prime": true, "cousine": true, "pt mono": true, "menlo": true, "monaco": true}

// gElem is one body element in the local model: its index range and, for
// paragraphs, the text as Docs shows it.
type gElem struct {
	start, end int
	kind       string // paragraph | table | section | other
	spans      []Span
	editable   bool   // only text runs: Docs can rewrite it
	list       string // "" | bulleted | numbered
	named      string // namedStyleType
	level      int
	divider    bool
	images     []string // content URIs of inline images
}

func (d *gDocument) listKind(p *gParagraph) string {
	if p.Bullet == nil {
		return ""
	}
	l, ok := d.Lists[p.Bullet.ListID]
	if !ok {
		return "bulleted"
	}
	lv := l.ListProperties.NestingLevels
	if p.Bullet.NestingLevel < len(lv) {
		switch lv[p.Bullet.NestingLevel].GlyphType {
		case "DECIMAL", "ZERO_DECIMAL", "UPPER_ALPHA", "ALPHA", "UPPER_ROMAN", "ROMAN":
			return "numbered"
		}
	}
	return "bulleted"
}

// paragraphElem reads a paragraph into the model: spans (vertical tabs, the
// soft line breaks, become newlines), whether only text runs make it up,
// and its inline images.
func (d *gDocument) paragraphElem(s gStructural) gElem {
	p := s.Paragraph
	e := gElem{start: s.StartIndex, end: s.EndIndex, kind: "paragraph", editable: true, named: p.ParagraphStyle.NamedStyleType, list: d.listKind(p)}
	if p.Bullet != nil {
		e.level = p.Bullet.NestingLevel
	}
	var spans []Span
	for i, el := range p.Elements {
		switch {
		case el.TextRun != nil:
			t := el.TextRun.Content
			if i == len(p.Elements)-1 {
				t = strings.TrimSuffix(t, "\n")
			}
			t = strings.ReplaceAll(t, "\u000b", "\n")
			st := el.TextRun.TextStyle
			sp := Span{Text: t, Bold: st.Bold, Italic: st.Italic, Underline: st.Underline, Strike: st.Strikethrough}
			if st.Link != nil {
				sp.Link = st.Link.URL
			}
			if st.WeightedFontFamily != nil && monoFonts[strings.ToLower(st.WeightedFontFamily.FontFamily)] {
				sp.Code = true
			}
			spans = append(spans, sp)
		case el.HorizontalRule != nil:
			e.divider = true
			e.editable = false
		case el.InlineObjectElement != nil:
			e.editable = false
			if obj, ok := d.InlineObjects[el.InlineObjectElement.InlineObjectID]; ok && obj.InlineObjectProperties.EmbeddedObject.ImageProperties != nil {
				e.images = append(e.images, obj.InlineObjectProperties.EmbeddedObject.ImageProperties.ContentURI)
			}
		case el.Person != nil:
			e.editable = false
			spans = append(spans, Span{Text: nonEmpty(el.Person.PersonProperties.Name, el.Person.PersonProperties.Email)})
		case el.RichLink != nil:
			e.editable = false
			spans = append(spans, Span{Text: el.RichLink.RichLinkProperties.Title, Link: el.RichLink.RichLinkProperties.URI})
		default:
			e.editable = false
		}
	}
	e.spans, _ = sanitizeSpans(spans, spanRules{})
	return e
}

func (d *gDocument) model() []gElem {
	out := make([]gElem, 0, len(d.Body.Content))
	for _, s := range d.Body.Content {
		switch {
		case s.Paragraph != nil:
			out = append(out, d.paragraphElem(s))
		case s.Table != nil:
			out = append(out, gElem{start: s.StartIndex, end: s.EndIndex, kind: "table"})
		case s.SectionBreak != nil:
			out = append(out, gElem{start: s.StartIndex, end: s.EndIndex, kind: "section"})
		default:
			out = append(out, gElem{start: s.StartIndex, end: s.EndIndex, kind: "other"})
		}
	}
	return out
}

func docsTypeOf(e gElem) string {
	switch {
	case e.divider:
		return "divider"
	case e.list == "numbered":
		return "numbered_list"
	case e.list == "bulleted":
		return "bulleted_list"
	}
	switch e.named {
	case "TITLE", "HEADING_1":
		return "heading_1"
	case "SUBTITLE", "HEADING_2":
		return "heading_2"
	case "HEADING_3", "HEADING_4", "HEADING_5", "HEADING_6":
		return "heading_3"
	}
	return "paragraph"
}

func gTableCells(d *gDocument, t *gTable) [][][]Span {
	rows := [][][]Span{}
	for _, r := range t.TableRows {
		var cells [][]Span
		for _, c := range r.TableCells {
			var spans []Span
			for i, s := range c.Content {
				if s.Paragraph == nil {
					continue
				}
				if i > 0 && len(spans) > 0 {
					spans = append(spans, Span{Text: "\n"})
				}
				spans = append(spans, d.paragraphElem(s).spans...)
			}
			clean, _ := sanitizeSpans(spans, spanRules{})
			cells = append(cells, clean)
		}
		rows = append(rows, cells)
		if len(rows) >= maxTableRows {
			break
		}
	}
	return rows
}

// blocksOf builds the editor's blocks from a document, with ids from ids
// (aligned with the body; nil or too short: fresh ids). It returns the
// blocks and the id of each body element.
func (g *googleDocs) blocksOf(d *gDocument, pageID uuid.UUID, ids []uuid.UUID) ([]*extBlock, []uuid.UUID) {
	elems := d.model()
	aligned := len(ids) == len(elems)
	outIDs := make([]uuid.UUID, len(elems))
	var blocks []*extBlock
	// Nesting: the last list item seen at each level.
	stack := map[int]uuid.UUID{}
	titleShown := false
	for k, e := range elems {
		if e.kind == "section" || e.kind == "other" {
			continue
		}
		// The page shows the document's name as its title: a leading TITLE
		// paragraph saying the same isn't repeated.
		if !titleShown && e.kind == "paragraph" {
			titleShown = true
			if !aligned && e.named == "TITLE" && strings.TrimSpace(plainOf(e.spans)) == strings.TrimSpace(d.Title) {
				continue
			}
		}
		id := googleID(d.DocumentID, "e", itoa(k))
		if aligned {
			if ids[k] == uuid.Nil {
				continue // a paragraph the session hid (emptied last paragraph)
			}
			id = ids[k]
		}
		outIDs[k] = id
		mk := func(bid uuid.UUID, typ string, props map[string]any, ro bool, parent *uuid.UUID) *extBlock {
			b := &extBlock{readOnly: ro}
			b.ID, b.PageID, b.ParentID, b.Type, b.Props = bid, pageID, parent, typ, props
			b.platformID = id.String()
			blocks = append(blocks, b)
			return b
		}
		if e.kind == "table" {
			mk(id, "table", map[string]any{"rows": gTableCells(d, d.Body.Content[k].Table), "header_row": false, "header_col": false}, true, nil)
			stack = map[int]uuid.UUID{}
			continue
		}
		typ := docsTypeOf(e)
		var parent *uuid.UUID
		if e.list != "" {
			if e.level > 0 {
				if pid, ok := stack[e.level-1]; ok {
					p := pid
					parent = &p
				}
			}
			stack[e.level] = id
			for l := range stack {
				if l > e.level {
					delete(stack, l)
				}
			}
		} else {
			stack = map[int]uuid.UUID{}
		}
		hasText := plainOf(e.spans) != ""
		if typ == "divider" {
			mk(id, "divider", map[string]any{}, false, nil)
		} else if hasText || len(e.images) == 0 {
			mk(id, typ, map[string]any{"text": e.spans}, !e.editable, parent)
		}
		for j, uri := range e.images {
			iid := googleID(d.DocumentID, "i", id.String(), itoa(j))
			if !hasText && j == 0 && typ != "divider" {
				iid = id
			}
			mk(iid, "image", map[string]any{"url": uri, "caption": []Span{}, "width": 100, "name": "Image"}, true, nil)
		}
	}
	assignPositions(blocks)
	return blocks, outIDs
}

func (g *googleDocs) load(ctx context.Context, ref string) (*extDoc, error) {
	f, err := g.file(ctx, ref)
	if err != nil {
		return nil, err
	}
	if f.Trashed {
		return nil, httpx.NewError(http.StatusNotFound, "not_found", "This document is in the trash in Google Drive.")
	}
	d, err := g.document(ctx, ref)
	if err != nil {
		return nil, err
	}
	doc := &extDoc{ref: ref, kind: "page", title: nonEmpty(d.Title, f.Name), url: f.WebViewLink, pageID: googleID("doc", ref), created: f.CreatedTime,
		edited: f.ModifiedTime, canEdit: f.Capabilities.CanEdit, refs: Refs{Users: map[string]RefUser{}, Pages: map[string]RefPage{}}, revision: d.RevisionID}
	if doc.url == "" {
		doc.url = g.pageURL(ref)
	}
	doc.blocks, doc.elemIDs = g.blocksOf(d, doc.pageID, nil)
	return doc, nil
}

func (g *googleDocs) sync(ctx context.Context, sess *extSession) (*SyncResult, error) {
	d, err := g.document(ctx, sess.ref)
	if err != nil {
		return nil, err
	}
	var ids []uuid.UUID
	if d.RevisionID == sess.revision {
		ids = sess.elemIDs
	}
	blocks, elemIDs := g.blocksOf(d, sess.pageID, ids)
	doc := &extDoc{ref: sess.ref, kind: "page", title: d.Title, pageID: sess.pageID, blocks: blocks, elemIDs: elemIDs, revision: d.RevisionID,
		refs: Refs{Users: map[string]RefUser{}, Pages: map[string]RefPage{}}, edited: time.Now().UTC()}
	sess.reset(doc)
	sess.version++
	return sess.syncResult(newExtPage(doc, uuid.Nil, sess.version, !sess.readOnly)), nil
}

// ---- writing: index math ---------------------------------------------------------------------------

// u16 is the length of s in UTF-16 code units (Google's index unit).
func u16(s string) int { return len(utf16.Encode([]rune(s))) }

// gText is text as Google stores it: soft line breaks are vertical tabs.
func gText(s string) string { return strings.ReplaceAll(s, "\n", "\u000b") }

// charStyle is the formatting of one character.
type charStyle struct {
	b, i, u, s, c bool
	link          string
}

func stylesOf(spans []Span) []charStyle {
	var out []charStyle
	for _, sp := range spans {
		st := charStyle{b: sp.Bold, i: sp.Italic, u: sp.Underline, s: sp.Strike, c: sp.Code, link: sp.Link}
		for range []rune(sp.Text) {
			out = append(out, st)
		}
	}
	return out
}

func (st charStyle) json() map[string]any {
	m := map[string]any{}
	if st.b {
		m["bold"] = true
	}
	if st.i {
		m["italic"] = true
	}
	if st.u {
		m["underline"] = true
	}
	if st.s {
		m["strikethrough"] = true
	}
	if st.link != "" {
		m["link"] = map[string]any{"url": st.link}
	}
	if st.c {
		m["weightedFontFamily"] = map[string]any{"fontFamily": gCodeFont}
	}
	return m
}

const gStyleFields = "bold,italic,underline,strikethrough,link"

// textEdit returns the requests that turn a paragraph starting at start
// from old into next: one delete and one insert for the changed middle
// (common prefix and suffix untouched, so their formatting stays), then
// text-style updates for every run whose formatting differs or is new. It
// also returns the change in the paragraph's length.
func textEdit(start int, old, next []Span) ([]any, int) {
	or, nr := []rune(plainOf(old)), []rune(plainOf(next))
	p := 0
	for p < len(or) && p < len(nr) && or[p] == nr[p] {
		p++
	}
	s := 0
	for s < len(or)-p && s < len(nr)-p && or[len(or)-1-s] == nr[len(nr)-1-s] {
		s++
	}
	var reqs []any
	at := start + u16(string(or[:p]))
	if delEnd := start + u16(string(or[:len(or)-s])); delEnd > at {
		reqs = append(reqs, map[string]any{"deleteContentRange": map[string]any{"range": map[string]any{"startIndex": at, "endIndex": delEnd}}})
	}
	if mid := string(nr[p : len(nr)-s]); mid != "" {
		reqs = append(reqs, map[string]any{"insertText": map[string]any{"location": map[string]any{"index": at}, "text": gText(mid)}})
	}
	os, ns := stylesOf(old), stylesOf(next)
	// What each character of next looks like now: kept characters keep
	// theirs; inserted ones take the character before the insertion's.
	var inherited charStyle
	if p > 0 {
		inherited = os[p-1]
	} else if len(or)-s < len(os) {
		inherited = os[len(or)-s]
	}
	type cur struct {
		st  charStyle
		new bool
	}
	current := make([]cur, len(nr))
	for i := range nr {
		switch {
		case i < p:
			current[i] = cur{st: os[i]}
		case i >= len(nr)-s:
			current[i] = cur{st: os[i-len(nr)+len(or)]}
		default:
			current[i] = cur{st: inherited, new: true}
		}
	}
	for i := 0; i < len(nr); {
		if !current[i].new && current[i].st == ns[i] {
			i++
			continue
		}
		j := i + 1
		for j < len(nr) && ns[j] == ns[i] && (current[j].new || current[j].st != ns[j]) && current[j].new == current[i].new {
			j++
		}
		fields := gStyleFields
		if ns[i].c || current[i].st.c {
			fields += ",weightedFontFamily"
		}
		reqs = append(reqs, map[string]any{"updateTextStyle": map[string]any{
			"range":     map[string]any{"startIndex": start + u16(string(nr[:i])), "endIndex": start + u16(string(nr[:j]))},
			"textStyle": ns[i].json(), "fields": fields,
		}})
		i = j
	}
	return reqs, u16(string(nr)) - u16(string(or))
}

// paragraphStyle returns the requests that give the paragraph range
// [start,end) a Docs block type: its named style and its bullets. A list
// item keeps the bullets it inherited when their kind and nesting level are
// right; otherwise it gets new ones at level (leading tabs set the level
// and are removed by createParagraphBullets, so indices end where they
// started).
func paragraphStyle(start, end int, typ, hadList string, hadLevel, level int) []any {
	named := map[string]string{"heading_1": "HEADING_1", "heading_2": "HEADING_2", "heading_3": "HEADING_3"}[typ]
	if named == "" {
		named = "NORMAL_TEXT"
	}
	rng := map[string]any{"startIndex": start, "endIndex": end}
	reqs := []any{map[string]any{"updateParagraphStyle": map[string]any{"range": rng, "paragraphStyle": map[string]any{"namedStyleType": named}, "fields": "namedStyleType"}}}
	want := listKindOf(typ)
	if want == hadList && (want == "" || hadLevel == level) {
		return reqs
	}
	if hadList != "" {
		reqs = append(reqs, map[string]any{"deleteParagraphBullets": map[string]any{"range": rng}})
	}
	if want != "" {
		preset := "BULLET_DISC_CIRCLE_SQUARE"
		if want == "numbered" {
			preset = "NUMBERED_DECIMAL_ALPHA_ROMAN"
		}
		if level > 0 {
			reqs = append(reqs, map[string]any{"insertText": map[string]any{"location": map[string]any{"index": start}, "text": strings.Repeat("\t", level)}})
		}
		reqs = append(reqs, map[string]any{"createParagraphBullets": map[string]any{"range": map[string]any{"startIndex": start, "endIndex": end + level}, "bulletPreset": preset}})
	}
	return reqs
}

func listKindOf(typ string) string {
	return map[string]string{"bulleted_list": "bulleted", "numbered_list": "numbered"}[typ]
}

// listLevel is how deep a block nests among list items in the editor.
func listLevel(sess *extSession, parent *uuid.UUID) int {
	level := 0
	for i := 0; parent != nil && i < maxBlockDepth+4; i++ {
		pb := sess.blocks[*parent]
		if pb == nil {
			break
		}
		if listKindOf(pb.Type) != "" {
			level++
		}
		parent = pb.ParentID
	}
	return level
}

var googleTypes = map[string]bool{"paragraph": true, "heading_1": true, "heading_2": true, "heading_3": true, "bulleted_list": true, "numbered_list": true}

// gPlan is a batch being built: the local model, the element ids, the
// requests so far.
type gPlan struct {
	elems []gElem
	ids   []uuid.UUID
	reqs  []any
}

func (pl *gPlan) shift(after, delta int) {
	for i := after + 1; i < len(pl.elems); i++ {
		pl.elems[i].start += delta
		pl.elems[i].end += delta
	}
}

func (pl *gPlan) indexOf(id uuid.UUID) int {
	for i, x := range pl.ids {
		if x == id && id != uuid.Nil {
			return i
		}
	}
	return -1
}

func (g *googleDocs) apply(ctx context.Context, sess *extSession, ops []Op) (*OpsResult, error) {
	res := sess.result("", uuid.Nil)
	ops = coalesce(ops)
	d, err := g.document(ctx, sess.ref)
	if err != nil {
		rejectAll(res, ops, err)
		return res, nil
	}
	pl := &gPlan{elems: d.model(), ids: append([]uuid.UUID(nil), sess.elemIDs...)}
	if d.RevisionID != sess.revision || len(pl.elems) != len(pl.ids) {
		code, msg := errChanged("Google Docs")
		for _, op := range ops {
			reject(res, op, code, msg)
		}
		return res, nil
	}
	// Session changes apply as we go (later operations may refer to blocks
	// made earlier in the batch) and roll back if Google refuses the batch.
	saved := map[uuid.UUID]extBlock{}
	for id, b := range sess.blocks {
		saved[id] = *b
	}
	savedPlatform := map[string]uuid.UUID{}
	for k, v := range sess.byPlatform {
		savedPlatform[k] = v
	}
	var applied []Op
	var touched []*extBlock
	var deleted []uuid.UUID
	for _, op := range ops {
		var t []*extBlock
		var del []uuid.UUID
		var rerr *Rejected
		switch op.Op {
		case "update":
			t, rerr = g.planUpdate(sess, pl, op)
		case "create":
			t, rerr = g.planCreate(sess, pl, op)
		case "delete":
			del, rerr = g.planDelete(sess, pl, op)
		case "move":
			rerr = &Rejected{Code: "unsupported", Message: "Moving blocks isn't available for Google Docs here. Move it in Google Docs."}
		default:
			rerr = &Rejected{Code: "invalid_op", Message: "Unknown operation."}
		}
		if rerr != nil {
			reject(res, op, rerr.Code, rerr.Message)
			continue
		}
		applied = append(applied, op)
		touched = append(touched, t...)
		deleted = append(deleted, del...)
	}
	if len(pl.reqs) > 0 {
		var out struct {
			WriteControl struct {
				RequiredRevisionID string `json:"requiredRevisionId"`
			} `json:"writeControl"`
		}
		in := map[string]any{"requests": pl.reqs, "writeControl": map[string]any{"requiredRevisionId": sess.revision}}
		if err := g.p.do(ctx, http.MethodPost, g.docs+"/documents/"+url.PathEscape(sess.ref)+":batchUpdate", in, &out); err != nil {
			sess.blocks = map[uuid.UUID]*extBlock{}
			for id, b := range saved {
				bb := b
				sess.blocks[id] = &bb
			}
			sess.byPlatform = savedPlatform
			rejectAll(res, applied, err)
			return res, nil
		}
		sess.revision = out.WriteControl.RequiredRevisionID
	}
	sess.elemIDs = pl.ids
	for _, b := range touched {
		if sess.blocks[b.ID] == b {
			sess.touched(res, b)
		}
	}
	res.Deleted = append(res.Deleted, deleted...)
	return res, nil
}

func rej(code, msg string) *Rejected { return &Rejected{Code: code, Message: msg} }

func (g *googleDocs) elemOf(sess *extSession, pl *gPlan, b *extBlock) int {
	return pl.indexOf(platformUUID(b.platformID))
}

func (g *googleDocs) planUpdate(sess *extSession, pl *gPlan, op Op) ([]*extBlock, *Rejected) {
	b := sess.blocks[op.ID]
	if b == nil {
		return nil, rej("not_found", "This block isn't on the page anymore. Reload to see the latest.")
	}
	k := g.elemOf(sess, pl, b)
	if b.readOnly || k < 0 || pl.elems[k].kind != "paragraph" || !pl.elems[k].editable || b.Type == "divider" {
		return nil, rej("read_only", "This part can't be changed here. Edit it in Google Docs.")
	}
	for key := range op.Props {
		if key != "text" {
			return nil, rej("unsupported", "Google Docs only takes text and formatting changes from here.")
		}
	}
	e := &pl.elems[k]
	typ := b.Type
	if op.Type != "" && op.Type != b.Type {
		if !googleTypes[op.Type] {
			return nil, rej("unsupported", "Google Docs can't take that kind of block from here.")
		}
		typ = op.Type
	}
	next := e.spans
	if raw, ok := op.Props["text"]; ok {
		spans, err := incomingSpans(raw, nil)
		if err != nil {
			return nil, rej("invalid_request", err.Error())
		}
		next = noMentions(spans)
	}
	reqs, delta := textEdit(e.start, e.spans, next)
	pl.reqs = append(pl.reqs, reqs...)
	e.end += delta
	pl.shift(k, delta)
	e.spans = next
	if typ != b.Type {
		level := listLevel(sess, b.ParentID)
		pl.reqs = append(pl.reqs, paragraphStyle(e.start, e.end, typ, e.list, e.level, level)...)
		e.list, e.level = listKindOf(typ), level
		b.Type = typ
	}
	b.Props = map[string]any{"text": next}
	return []*extBlock{b}, nil
}

// noMentions turns mention chips into plain text (Google Docs' people and
// date chips can't be written through the API).
func noMentions(spans []Span) []Span {
	out := make([]Span, 0, len(spans))
	for _, s := range spans {
		s.Mention = nil
		out = append(out, s)
	}
	clean, _ := sanitizeSpans(out, spanRules{})
	return clean
}

// lastDescendant is the last block in document order under b (b itself
// when it has no children).
func lastDescendant(sess *extSession, b *extBlock) *extBlock {
	for range maxBlockDepth + 4 {
		kids := sess.children(&b.ID)
		if len(kids) == 0 {
			return b
		}
		b = kids[len(kids)-1]
	}
	return b
}

func (g *googleDocs) planCreate(sess *extSession, pl *gPlan, op Op) ([]*extBlock, *Rejected) {
	if !googleTypes[op.Type] {
		return nil, rej("unsupported", "Google Docs can't take that kind of block from here. Add it in Google Docs.")
	}
	if sess.blocks[op.ID] != nil {
		return nil, rej("id_taken", "That block already exists.")
	}
	var spans []Span
	if raw, ok := op.Props["text"]; ok {
		s, err := incomingSpans(raw, nil)
		if err != nil {
			return nil, rej("invalid_request", err.Error())
		}
		spans = noMentions(s)
	}
	// The block it follows in document order.
	var pred *extBlock
	if prev := sess.before(op.ParentID, op.Position, op.ID); prev != nil {
		pred = lastDescendant(sess, prev)
	} else if op.ParentID != nil {
		pred = sess.blocks[*op.ParentID]
	}
	k := -1
	if pred != nil {
		if k = g.elemOf(sess, pl, pred); k < 0 {
			return nil, rej("unsupported", "A block can't go there in Google Docs from here.")
		}
	}
	text := gText(plainOf(spans))
	var at, start, inheritList int
	inheritList = -1
	switch {
	case k >= 0 && pl.elems[k].kind == "paragraph":
		// Split after the paragraph: the new one inherits its style.
		at = pl.elems[k].end - 1
		start = pl.elems[k].end
		pl.reqs = append(pl.reqs, map[string]any{"insertText": map[string]any{"location": map[string]any{"index": at}, "text": "\n" + text}})
		inheritList = k
	default:
		// Before the next element's text (after a table, or at the top).
		next := k + 1
		for next < len(pl.elems) && (pl.elems[next].kind != "paragraph" || (pl.ids[next] == uuid.Nil && pl.elems[next].named == "TITLE")) {
			next++
		}
		if next >= len(pl.elems) {
			return nil, rej("unsupported", "A block can't go there in Google Docs from here.")
		}
		at, start = pl.elems[next].start, pl.elems[next].start
		pl.reqs = append(pl.reqs, map[string]any{"insertText": map[string]any{"location": map[string]any{"index": at}, "text": text + "\n"}})
		inheritList = next
		k = next - 1
	}
	length := u16(text) + 1
	hadList, hadLevel := pl.elems[inheritList].list, pl.elems[inheritList].level
	level := listLevel(sess, op.ParentID)
	end := start + length
	// Clear inherited formatting, then apply the block's own.
	if u16(text) > 0 {
		pl.reqs = append(pl.reqs, map[string]any{"updateTextStyle": map[string]any{"range": map[string]any{"startIndex": start, "endIndex": end - 1}, "textStyle": map[string]any{}, "fields": gStyleFields + ",weightedFontFamily"}})
		r := []rune(plainOf(spans))
		st := stylesOf(spans)
		for i := 0; i < len(r); {
			j := i + 1
			for j < len(r) && st[j] == st[i] {
				j++
			}
			if st[i] != (charStyle{}) {
				pl.reqs = append(pl.reqs, map[string]any{"updateTextStyle": map[string]any{
					"range": map[string]any{"startIndex": start + u16(string(r[:i])), "endIndex": start + u16(string(r[:j]))}, "textStyle": st[i].json(), "fields": gStyleFields + ",weightedFontFamily"}})
			}
			i = j
		}
	}
	pl.reqs = append(pl.reqs, paragraphStyle(start, end, op.Type, hadList, hadLevel, level)...)
	// The model: a new paragraph after element k.
	ne := gElem{start: start, end: end, kind: "paragraph", editable: true, spans: spans, list: listKindOf(op.Type), level: level}
	pl.elems = append(pl.elems[:k+1], append([]gElem{ne}, pl.elems[k+1:]...)...)
	pl.ids = append(pl.ids[:k+1], append([]uuid.UUID{op.ID}, pl.ids[k+1:]...)...)
	pl.shift(k+1, length)
	b := &extBlock{platformID: op.ID.String()}
	b.ID, b.PageID, b.ParentID, b.Type, b.Position = op.ID, sess.pageID, op.ParentID, op.Type, op.Position
	b.Props = map[string]any{"text": spans}
	b.CreatedAt = time.Now().UTC()
	sess.blocks[op.ID] = b
	sess.byPlatform[b.platformID] = op.ID
	return []*extBlock{b}, nil
}

func (g *googleDocs) planDelete(sess *extSession, pl *gPlan, op Op) ([]uuid.UUID, *Rejected) {
	b := sess.blocks[op.ID]
	if b == nil {
		return []uuid.UUID{op.ID}, nil
	}
	ids := sess.subtree(op.ID)
	// Every block in the subtree must be a removable paragraph.
	var ks []int
	for _, id := range ids {
		x := sess.blocks[id]
		k := g.elemOf(sess, pl, x)
		if k < 0 || pl.elems[k].kind != "paragraph" || (x.readOnly && x.Type != "divider") || (!pl.elems[k].editable && !pl.elems[k].divider) {
			return nil, rej("read_only", "This part can't be removed here. Remove it in Google Docs.")
		}
		ks = append(ks, k)
	}
	// Last element first, so earlier indices stay put.
	for i := 0; i < len(ks); i++ {
		for j := i + 1; j < len(ks); j++ {
			if ks[j] > ks[i] {
				ks[i], ks[j] = ks[j], ks[i]
			}
		}
	}
	for _, k := range ks {
		e := pl.elems[k]
		if k == len(pl.elems)-1 {
			// The body's last paragraph keeps its newline: empty it and hide it.
			if e.end-1 > e.start {
				pl.reqs = append(pl.reqs, map[string]any{"deleteContentRange": map[string]any{"range": map[string]any{"startIndex": e.start, "endIndex": e.end - 1}}})
			}
			if e.list != "" {
				pl.reqs = append(pl.reqs, map[string]any{"deleteParagraphBullets": map[string]any{"range": map[string]any{"startIndex": e.start, "endIndex": e.start + 1}}})
			}
			delta := (e.start + 1) - e.end
			pl.elems[k].end = e.start + 1
			pl.elems[k].spans, pl.elems[k].list = nil, ""
			pl.shift(k, delta)
			pl.ids[k] = uuid.Nil
			continue
		}
		pl.reqs = append(pl.reqs, map[string]any{"deleteContentRange": map[string]any{"range": map[string]any{"startIndex": e.start, "endIndex": e.end}}})
		pl.shift(k, e.start-e.end)
		pl.elems = append(pl.elems[:k], pl.elems[k+1:]...)
		pl.ids = append(pl.ids[:k], pl.ids[k+1:]...)
	}
	return sess.remove(op.ID), nil
}

// ---- other writes ---------------------------------------------------------------------------

func (g *googleDocs) rows(context.Context, string, extRowQuery) (*RowPage, error) {
	return nil, errUnsupported("Google Docs", "A database")
}

func (g *googleDocs) setRow(context.Context, string, *string, map[string]json.RawMessage) (*Row, error) {
	return nil, errUnsupported("Google Docs", "Setting a property")
}

func (g *googleDocs) createRow(context.Context, string, string, map[string]json.RawMessage) (*Row, error) {
	return nil, errUnsupported("Google Docs", "Adding a row")
}

// createPage makes a new Google Doc named title (in My Drive; parentRef is
// a Drive folder id when given) and writes blocks into it.
func (g *googleDocs) createPage(ctx context.Context, parentRef, title string, blocks []*mdBlock) (ExtItem, error) {
	meta := map[string]any{"name": nonEmpty(strings.TrimSpace(title), "Untitled document"), "mimeType": gDocMime}
	if parentRef != "" {
		meta["parents"] = []string{parentRef}
	}
	var f gFile
	q := url.Values{"fields": {gFileFields}, "supportsAllDrives": {"true"}}
	if err := g.p.do(ctx, http.MethodPost, g.drive+"/files?"+q.Encode(), meta, &f); err != nil {
		return ExtItem{}, err
	}
	var reqs []any
	at := 1
	for _, b := range flattenMD(blocks) {
		typ := b.typ
		if !googleTypes[typ] {
			typ = "paragraph"
		}
		text := gText(plainOf(spansValue(b.props["text"])))
		if b.typ == "code" {
			code, _ := b.props["code"].(string)
			text = gText(code)
		}
		reqs = append(reqs, map[string]any{"insertText": map[string]any{"location": map[string]any{"index": at}, "text": text + "\n"}})
		reqs = append(reqs, paragraphStyle(at, at+u16(text)+1, typ, "", 0, 0)...)
		at += u16(text) + 1
	}
	if len(reqs) > 0 {
		if err := g.p.do(ctx, http.MethodPost, g.docs+"/documents/"+url.PathEscape(f.ID)+":batchUpdate", map[string]any{"requests": reqs}, nil); err != nil {
			return ExtItem{}, err
		}
	}
	return g.item(&f), nil
}

func flattenMD(list []*mdBlock) []*mdBlock {
	var out []*mdBlock
	for _, b := range list {
		out = append(out, b)
		out = append(out, flattenMD(b.children)...)
	}
	return out
}

type gComment struct {
	ID          string    `json:"id"`
	Content     string    `json:"content"`
	CreatedTime time.Time `json:"createdTime"`
	Resolved    bool      `json:"resolved"`
	Deleted     bool      `json:"deleted"`
	Author      struct {
		DisplayName string `json:"displayName"`
		PhotoLink   string `json:"photoLink"`
	} `json:"author"`
	QuotedFileContent *struct {
		Value string `json:"value"`
	} `json:"quotedFileContent"`
	Replies []gComment `json:"replies"`
}

func (c gComment) ext(thread string) ExtComment {
	out := ExtComment{ID: c.ID, ThreadID: thread, Body: textSpans(c.Content), CreatedAt: c.CreatedTime, Resolved: c.Resolved, Replies: []ExtComment{},
		Author: &ExtPerson{Name: nonEmpty(c.Author.DisplayName, "Google user"), AvatarURL: strPtr(c.Author.PhotoLink)}}
	if c.QuotedFileContent != nil && c.QuotedFileContent.Value != "" {
		q := c.QuotedFileContent.Value
		out.Quote = &q
	}
	for _, r := range c.Replies {
		if !r.Deleted {
			out.Replies = append(out.Replies, r.ext(thread))
		}
	}
	return out
}

func (g *googleDocs) comments(ctx context.Context, ref string) ([]ExtComment, error) {
	out := []ExtComment{}
	token := ""
	for range 3 {
		q := url.Values{"fields": {"nextPageToken,comments(" + gCommentKeys + ")"}, "pageSize": {"100"}, "includeDeleted": {"false"}}
		if token != "" {
			q.Set("pageToken", token)
		}
		var list struct {
			NextPageToken string     `json:"nextPageToken"`
			Comments      []gComment `json:"comments"`
		}
		if err := g.p.do(ctx, http.MethodGet, g.drive+"/files/"+url.PathEscape(ref)+"/comments?"+q.Encode(), nil, &list); err != nil {
			return nil, err
		}
		for _, c := range list.Comments {
			if !c.Deleted {
				out = append(out, c.ext(c.ID))
			}
		}
		if list.NextPageToken == "" {
			break
		}
		token = list.NextPageToken
	}
	return out, nil
}

func (g *googleDocs) addComment(ctx context.Context, ref string, body []Span, threadID, _ string) (*ExtComment, error) {
	content := strings.TrimSpace(plainOf(body))
	if content == "" {
		return nil, httpx.BadRequest("Write a comment first.")
	}
	q := url.Values{"fields": {gCommentKeys}}
	var c gComment
	if threadID != "" {
		if err := g.p.do(ctx, http.MethodPost, g.drive+"/files/"+url.PathEscape(ref)+"/comments/"+url.PathEscape(threadID)+"/replies?"+url.Values{"fields": {"id,content,author(displayName,photoLink),createdTime"}}.Encode(),
			map[string]any{"content": content}, &c); err != nil {
			return nil, err
		}
		out := c.ext(threadID)
		return &out, nil
	}
	if err := g.p.do(ctx, http.MethodPost, g.drive+"/files/"+url.PathEscape(ref)+"/comments?"+q.Encode(), map[string]any{"content": content}, &c); err != nil {
		return nil, err
	}
	out := c.ext(c.ID)
	return &out, nil
}

func (g *googleDocs) rename(ctx context.Context, ref, title string) error {
	q := url.Values{"supportsAllDrives": {"true"}, "fields": {"id"}}
	return g.p.do(ctx, http.MethodPatch, g.drive+"/files/"+url.PathEscape(ref)+"?"+q.Encode(), map[string]any{"name": title}, nil)
}

func (n *notion) rename(ctx context.Context, ref, title string) error {
	rt, err := notionRich(textSpans(title), nil)
	if err != nil {
		return err
	}
	n.forgetRoots()
	return n.call(ctx, http.MethodPatch, "/pages/"+url.PathEscape(ref), nil, map[string]any{"properties": map[string]any{"title": map[string]any{"title": rt}}}, nil)
}
