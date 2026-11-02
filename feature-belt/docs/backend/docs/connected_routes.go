package docs

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
)

// mountConnected adds the connected-docs routes (signed in; each opens the
// caller's own connection).
func (s *Service) mountConnected(r chi.Router) {
	h := httpx.Handler
	w := r.With(auth.RequireActive)
	r.Get("/docs/sources", h(s.sourcesRoute))
	r.Get("/docs/connected/resolve", h(s.resolveRoute))
	r.With(auth.RateLimit(240, time.Minute)).Get("/docs/connected/{connID}/tree", h(s.treeRoute))
	r.With(auth.RateLimit(120, time.Minute)).Get("/docs/connected/{connID}/search", h(s.connSearchRoute))
	r.With(auth.RateLimit(240, time.Minute)).Get("/docs/connected/{connID}/pages/{ref}", h(s.connPageRoute))
	r.With(auth.RateLimit(120, time.Minute)).Get("/docs/connected/{connID}/pages/{ref}/sync", h(s.connSyncRoute))
	w.With(auth.RateLimit(600, time.Minute)).Post("/docs/connected/{connID}/pages/{ref}/ops", h(s.connOpsRoute))
	w.With(auth.RateLimit(120, time.Minute)).Patch("/docs/connected/{connID}/pages/{ref}", h(s.connRenameRoute))
	w.With(auth.RateLimit(60, time.Hour)).Post("/docs/connected/{connID}/pages", h(s.connCreatePageRoute))
	r.With(auth.RateLimit(240, time.Minute)).Get("/docs/connected/{connID}/databases/{ref}/rows", h(s.connRowsRoute))
	w.With(auth.RateLimit(300, time.Minute)).Post("/docs/connected/{connID}/databases/{ref}/rows", h(s.connCreateRowRoute))
	w.With(auth.RateLimit(600, time.Minute)).Patch("/docs/connected/{connID}/rows/{ref}", h(s.connSetRowRoute))
	r.With(auth.RateLimit(120, time.Minute)).Get("/docs/connected/{connID}/pages/{ref}/comments", h(s.connCommentsRoute))
	w.With(auth.RateLimit(60, time.Minute)).Post("/docs/connected/{connID}/pages/{ref}/comments", h(s.connAddCommentRoute))
	w.With(auth.RateLimit(30, time.Hour)).Post("/docs/connected/{connID}/pages/{ref}/copy", h(s.connCopyRoute))
}

// connRequest reads the caller, the optional org and the connection id,
// and opens the connection (write: with its write capability).
func (s *Service) connRequest(r *http.Request, write bool) (*connSession, string, error) {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	org, err := s.orgParam(ctx, r.URL.Query().Get("org_id"), me)
	if err != nil {
		return nil, "", err
	}
	cs, err := s.open(ctx, me, org, chi.URLParam(r, "connID"), write)
	if err != nil {
		return nil, "", err
	}
	ref := chi.URLParam(r, "ref")
	if ref != "" && !validRef(ref) {
		return nil, "", httpx.NotFound("Page not found.")
	}
	return cs, ref, nil
}

func (s *Service) sourcesRoute(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	org, err := s.orgParam(ctx, r.URL.Query().Get("org_id"), me)
	if err != nil {
		return err
	}
	out, err := s.sources(ctx, me, org)
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

func (s *Service) treeRoute(w http.ResponseWriter, r *http.Request) error {
	cs, _, err := s.connRequest(r, false)
	if err != nil {
		return err
	}
	q := r.URL.Query()
	parent := q.Get("parent")
	if parent != "" && !validRef(parent) {
		return httpx.NotFound("Page not found.")
	}
	out, err := cs.ad.tree(r.Context(), parent, q.Get("section"), q.Get("cursor"))
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

func (s *Service) connSearchRoute(w http.ResponseWriter, r *http.Request) error {
	cs, _, err := s.connRequest(r, false)
	if err != nil {
		return err
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		return httpx.BadRequest("Type something to search for.")
	}
	if len(q) > 200 {
		q = q[:200]
	}
	out, err := cs.ad.search(r.Context(), q, r.URL.Query().Get("cursor"), 25)
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

// detail loads a connected page and starts its editing session.
func (s *Service) connDetail(ctx context.Context, cs *connSession, ref string) (*ConnectedPageDetail, error) {
	doc, err := cs.ad.load(ctx, ref)
	if err != nil {
		return nil, err
	}
	canEdit := doc.canEdit && cs.conn.writeCap != ""
	sess := startSession(cs.me, cs.conn.ID, doc, !canEdit)
	page := newExtPage(doc, cs.org, 0, canEdit)
	out := &ConnectedPageDetail{
		PageDetail: PageDetail{Page: page, Path: []Crumb{}, Blocks: []Block{}, Refs: doc.refs, Views: doc.views, Database: doc.db, CommentCounts: map[string]int{}},
		Ref:        doc.ref, ConnectionID: cs.conn.ID, SessionID: sess.id, Source: cs.conn.source(doc.url), Connection: cs.sourceView(),
		URL: doc.url, CanEdit: canEdit, ReadOnlyBlocks: []uuid.UUID{}, Partial: doc.partial, DatabaseRef: doc.dbRef,
	}
	if out.Views == nil {
		out.Views = []View{}
	}
	for _, b := range sess.docOrder() {
		out.Blocks = append(out.Blocks, b.Block)
		if b.readOnly {
			out.ReadOnlyBlocks = append(out.ReadOnlyBlocks, b.ID)
		}
	}
	return out, nil
}

func (s *Service) connPageRoute(w http.ResponseWriter, r *http.Request) error {
	cs, ref, err := s.connRequest(r, false)
	if err != nil {
		return err
	}
	out, err := s.connDetail(r.Context(), cs, ref)
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

type connOpsReq struct {
	SessionID string `json:"session_id"`
	ClientID  string `json:"client_id"`
	Ops       []Op   `json:"ops"`
}

func (s *Service) connOpsRoute(w http.ResponseWriter, r *http.Request) error {
	var req connOpsReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if len(req.Ops) == 0 || len(req.Ops) > maxOpsPerBatch {
		return httpx.BadRequest("Send 1 to 500 operations.")
	}
	cs, ref, err := s.connRequest(r, true)
	if err != nil {
		return err
	}
	sess, err := findSession(req.SessionID, cs.me, cs.conn.ID, ref)
	if err != nil {
		return err
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.readOnly {
		return httpx.NewError(http.StatusForbidden, "read_only", "This account can't change this page in "+platformLabel(cs.conn.Provider)+".")
	}
	res, err := cs.ad.apply(r.Context(), sess, req.Ops)
	if err != nil {
		return err
	}
	res.ClientID, res.ActorID = req.ClientID, cs.me
	httpx.OK(w, res)
	return nil
}

func (s *Service) connSyncRoute(w http.ResponseWriter, r *http.Request) error {
	cs, ref, err := s.connRequest(r, false)
	if err != nil {
		return err
	}
	sess, err := findSession(r.URL.Query().Get("session_id"), cs.me, cs.conn.ID, ref)
	if err != nil {
		return err
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	out, err := cs.ad.sync(r.Context(), sess)
	if err != nil {
		return err
	}
	out.Page.OrgID = cs.org
	httpx.OK(w, out)
	return nil
}

func (s *Service) connRenameRoute(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Title string `json:"title"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	title, err := cleanTitle(req.Title)
	if err != nil {
		return err
	}
	cs, ref, err := s.connRequest(r, true)
	if err != nil {
		return err
	}
	if err := cs.ad.rename(r.Context(), ref, title); err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"title": title})
	return nil
}

func (s *Service) connCreatePageRoute(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		ParentRef string `json:"parent_ref"`
		Title     string `json:"title"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	title, err := cleanTitle(req.Title)
	if err != nil {
		return err
	}
	if req.ParentRef != "" && !validRef(req.ParentRef) {
		return httpx.NotFound("Page not found.")
	}
	cs, _, err := s.connRequest(r, true)
	if err != nil {
		return err
	}
	if cs.conn.Provider == providerNotion && req.ParentRef == "" {
		return httpx.BadRequest("Choose the Notion page to add it to.")
	}
	it, err := cs.ad.createPage(r.Context(), req.ParentRef, title, nil)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, it)
	return nil
}

func (s *Service) connRowsRoute(w http.ResponseWriter, r *http.Request) error {
	cs, ref, err := s.connRequest(r, false)
	if err != nil {
		return err
	}
	q := r.URL.Query()
	rq := extRowQuery{search: q.Get("q"), cursor: q.Get("cursor")}
	if v := q.Get("filters"); v != "" {
		if err := json.Unmarshal([]byte(v), &rq.filters); err != nil {
			return httpx.BadRequest("filters must be a JSON array.")
		}
	}
	if v := q.Get("sorts"); v != "" {
		if err := json.Unmarshal([]byte(v), &rq.sorts); err != nil {
			return httpx.BadRequest("sorts must be a JSON array.")
		}
	}
	if v := q.Get("limit"); v != "" {
		rq.limit, _ = strconv.Atoi(v)
	}
	out, err := cs.ad.rows(r.Context(), ref, rq)
	if err != nil {
		return err
	}
	for i := range out.Items {
		out.Items[i].Source = cs.conn.source(out.Items[i].URL)
	}
	httpx.OK(w, out)
	return nil
}

type connRowReq struct {
	Title  *string                    `json:"title"`
	Values map[string]json.RawMessage `json:"values"`
}

func (s *Service) connCreateRowRoute(w http.ResponseWriter, r *http.Request) error {
	var req connRowReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	cs, ref, err := s.connRequest(r, true)
	if err != nil {
		return err
	}
	title := ""
	if req.Title != nil {
		title = *req.Title
	}
	row, err := cs.ad.createRow(r.Context(), ref, title, req.Values)
	if err != nil {
		return err
	}
	row.Source = cs.conn.source(row.URL)
	httpx.JSON(w, http.StatusCreated, row)
	return nil
}

func (s *Service) connSetRowRoute(w http.ResponseWriter, r *http.Request) error {
	var req connRowReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.Title == nil && len(req.Values) == 0 {
		return httpx.BadRequest("Send a title or values.")
	}
	cs, ref, err := s.connRequest(r, true)
	if err != nil {
		return err
	}
	row, err := cs.ad.setRow(r.Context(), ref, req.Title, req.Values)
	if err != nil {
		return err
	}
	row.Source = cs.conn.source(row.URL)
	httpx.OK(w, row)
	return nil
}

func (s *Service) connCommentsRoute(w http.ResponseWriter, r *http.Request) error {
	cs, ref, err := s.connRequest(r, false)
	if err != nil {
		return err
	}
	out, err := cs.ad.comments(r.Context(), ref)
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": out})
	return nil
}

func (s *Service) connAddCommentRoute(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Body     []Span `json:"body"`
		ThreadID string `json:"thread_id"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	body, n := sanitizeSpans(req.Body, spanRules{validUser: func(uuid.UUID) bool { return false }})
	if n == 0 {
		return httpx.BadRequest("Write a comment first.")
	}
	if n > 2000 {
		return httpx.BadRequest("A comment can hold up to 2000 characters.")
	}
	cs, ref, err := s.connRequest(r, true)
	if err != nil {
		return err
	}
	c, err := cs.ad.addComment(r.Context(), ref, body, req.ThreadID, "")
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, c)
	return nil
}

// ---- bridges ---------------------------------------------------------------------------

// connCopyRoute: POST …/pages/{ref}/copy {org_id, parent_id?}: a new Docs
// page with the connected page's content (structure kept; images and files
// become links to the original; subpages become connected-page links).
func (s *Service) connCopyRoute(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		OrgID    uuid.UUID  `json:"org_id"`
		ParentID *uuid.UUID `json:"parent_id"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	if _, err := s.orgParam(ctx, req.OrgID.String(), me); err != nil {
		return err
	}
	cs, err := s.open(ctx, me, req.OrgID, chi.URLParam(r, "connID"), false)
	if err != nil {
		return err
	}
	ref := chi.URLParam(r, "ref")
	if !validRef(ref) {
		return httpx.NotFound("Page not found.")
	}
	p, err := s.copyConnected(ctx, cs, me, req.OrgID, req.ParentID, ref)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, p)
	return nil
}

func (s *Service) copyConnected(ctx context.Context, cs *connSession, me, org uuid.UUID, parent *uuid.UUID, ref string) (*Page, error) {
	doc, err := cs.ad.load(ctx, ref)
	if err != nil {
		return nil, err
	}
	if doc.kind == "database" {
		return nil, httpx.BadRequest("Databases can't be copied into Docs yet. Open it here, or copy its rows in " + platformLabel(cs.conn.Provider) + ".")
	}
	if parent != nil {
		if _, err := requirePage(ctx, s.d.Pool, me, *parent, access.Edit); err != nil {
			return nil, err
		}
	}
	title, err := cleanTitle(doc.title)
	if err != nil {
		title = string([]rune(doc.title)[:maxTitleChars])
	}
	p, err := s.create(ctx, me, org, createPageReq{ParentID: parent, Title: title, Icon: doc.icon})
	if err != nil {
		return nil, err
	}
	blocks := copyBlocks(cs, doc)
	if ops := blockOps(blocks, nil, "", ""); len(ops) > 0 {
		for start := 0; start < len(ops); start += maxOpsPerBatch {
			end := min(start+maxOpsPerBatch, len(ops))
			if _, err := s.apply(ctx, me, p, ops[start:end], "copy"); err != nil {
				return nil, err
			}
		}
	}
	return loadPageWithLevel(ctx, s, me, p.ID)
}

// copyBlocks converts a connected page's blocks into Docs blocks: platform
// people and page mentions become text and links, media become links to
// the original (their platform links expire), subpages become live
// connected-page blocks.
func copyBlocks(cs *connSession, doc *extDoc) []*mdBlock {
	byParent := map[uuid.UUID][]*extBlock{}
	var roots []*extBlock
	for _, b := range doc.blocks {
		if b.ParentID == nil {
			roots = append(roots, b)
		} else {
			byParent[*b.ParentID] = append(byParent[*b.ParentID], b)
		}
	}
	fix := func(spans []Span) []Span {
		out := make([]Span, 0, len(spans))
		for _, sp := range spans {
			if m := sp.Mention; m != nil && m.Type != "date" {
				if m.Type == "page" && cs.conn.Provider == providerNotion {
					sp.Link = cs.ad.pageURL(m.ID)
				}
				if m.Type == "user" {
					if u, ok := doc.refs.Users[m.ID]; ok && !strings.HasPrefix(sp.Text, "@") {
						sp.Text = "@" + u.Name
					}
				}
				sp.Mention = nil
			}
			out = append(out, sp)
		}
		return out
	}
	var conv func(list []*extBlock, depth int) []*mdBlock
	conv = func(list []*extBlock, depth int) []*mdBlock {
		var out []*mdBlock
		for _, b := range list {
			var mb *mdBlock
			switch b.Type {
			case "image", "file":
				name, _ := b.Props["name"].(string)
				mb = &mdBlock{typ: "bookmark", props: map[string]any{"url": doc.url, "title": nonEmpty(name, "Image") + " · " + platformLabel(cs.conn.Provider), "caption": fix(spansValue(b.Props["caption"]))}}
			case "bookmark":
				u, _ := b.Props["url"].(string)
				if safeLink(u) == "" || strings.HasPrefix(safeLink(u), "mailto:") {
					mb = &mdBlock{typ: "paragraph", props: map[string]any{"text": fix(spansValue(b.Props["caption"]))}}
					break
				}
				mb = &mdBlock{typ: "bookmark", props: map[string]any{"url": u, "title": b.Props["title"], "caption": fix(spansValue(b.Props["caption"]))}}
			case "page":
				childRef, _ := b.Props["page_id"].(string)
				ref := doc.refs.Pages[childRef]
				title := "Untitled"
				if ref.Title != nil {
					title = *ref.Title
				}
				mb = &mdBlock{typ: "connected_page", props: map[string]any{"provider": cs.conn.Provider, "account_id": cs.conn.AccountID, "ref": childRef,
					"title": title, "url": cs.ad.pageURL(childRef)}}
			case "table":
				rows, _ := b.Props["rows"].([][][]Span)
				var fixed [][][]Span
				for _, row := range rows {
					var cells [][]Span
					for i, c := range row {
						if i < maxTableCols {
							cells = append(cells, fix(c))
						}
					}
					fixed = append(fixed, cells)
				}
				mb = &mdBlock{typ: "table", props: map[string]any{"rows": fixed, "header_row": b.Props["header_row"], "header_col": b.Props["header_col"]}}
			default:
				props := map[string]any{}
				for k, v := range b.Props {
					props[k] = v
				}
				if t, ok := props["text"]; ok {
					props["text"] = fix(spansValue(t))
				}
				mb = &mdBlock{typ: b.Type, props: props}
			}
			if depth < maxBlockDepth {
				mb.children = conv(byParent[b.ID], depth+1)
			}
			out = append(out, mb)
		}
		return out
	}
	return conv(roots, 0)
}

// ---- connected-page blocks ---------------------------------------------------------------------------

// ResolvedLink is GET /v1/docs/connected/resolve: what a viewer sees of a
// connected-page block, with their own connection.
type ResolvedLink struct {
	// State: ok | connect (no connection for that platform/account) |
	// reconnect | no_access (their account can't open it) | blocked.
	State        string               `json:"state"`
	ConnectionID string               `json:"connection_id,omitempty"`
	Item         *ExtItem             `json:"item,omitempty"`
	Blocks       []Block              `json:"blocks"`
	Refs         *Refs                `json:"refs,omitempty"`
	Offer        *app.ConnectionOffer `json:"offer,omitempty"`
	FixURL       string               `json:"fix_url,omitempty"`
	More         bool                 `json:"more"`
}

const maxPreviewBlocks = 12

// resolveRoute: GET /docs/connected/resolve?provider=&account_id=&ref=&org_id=.
// A connected-page block opens live for each viewer through their own
// connection to the same account (a Notion workspace) or platform (Google).
func (s *Service) resolveRoute(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	q := r.URL.Query()
	provider, ref, accountID := q.Get("provider"), q.Get("ref"), q.Get("account_id")
	if (provider != providerNotion && provider != providerGoogle) || !validRef(ref) {
		return httpx.BadRequest("That link isn't a connected page.")
	}
	org, err := s.orgParam(ctx, q.Get("org_id"), me)
	if err != nil {
		return err
	}
	out := ResolvedLink{State: "connect", Blocks: []Block{}}
	readCap := CapNotionRead
	if provider == providerGoogle {
		readCap = CapGoogleRead
	}
	conns, err := s.connectedFor(ctx, me, org)
	if err != nil {
		return err
	}
	var candidates []*docsConn
	for _, c := range conns {
		if c.Provider == provider && (provider != providerNotion || accountID == "" || c.AccountID == "" || c.AccountID == accountID) {
			candidates = append(candidates, c)
		}
	}
	if len(candidates) == 0 {
		if cx := s.connections(); cx != nil {
			if offers, err := cx.Offers(ctx, me, org, s.Name()); err == nil {
				for _, o := range offers {
					if o.Capability == readCap {
						oo := o
						out.Offer = &oo
					}
				}
			}
		}
		if out.Offer == nil {
			out.Offer = &app.ConnectionOffer{Capability: readCap, Provider: provider, Label: platformLabel(provider), ConnectURL: connectURL(readCap, "")}
		}
		httpx.OK(w, out)
		return nil
	}
	out.State = "no_access"
	for _, c := range candidates {
		switch c.Status {
		case "needs_reauth":
			out.State, out.ConnectionID, out.FixURL = "reconnect", c.ID, connectURL(c.readCap, "")
			continue
		case "blocked":
			if out.State == "no_access" {
				out.State = "blocked"
			}
			continue
		}
		cs, err := s.open(ctx, me, org, c.ID, false)
		if err != nil {
			continue
		}
		if n, ok := cs.ad.(*notion); ok {
			n.shallow = true
		}
		doc, err := cs.ad.load(ctx, ref)
		if err != nil {
			if isHTTPStatus(err, http.StatusNotFound) || isHTTPStatus(err, http.StatusForbidden) {
				out.ConnectionID = c.ID
				continue
			}
			return err
		}
		it := ExtItem{ID: doc.ref, ConnectionID: c.ID, Kind: doc.kind, Title: doc.title, Icon: doc.icon, UpdatedAt: doc.edited, URL: doc.url,
			Link: ConnectedLink(c.ID, doc.ref), Source: c.source(doc.url)}
		out.State, out.ConnectionID, out.Item, out.Refs = "ok", c.ID, &it, &doc.refs
		for _, b := range doc.blocks {
			if b.ParentID != nil {
				continue
			}
			if len(out.Blocks) >= maxPreviewBlocks {
				out.More = true
				break
			}
			out.Blocks = append(out.Blocks, b.Block)
		}
		break
	}
	httpx.OK(w, out)
	return nil
}
