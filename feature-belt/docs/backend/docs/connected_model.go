package docs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/httpx"
)

// ExtItem is a connected page or database in trees, lists and search
// (DocsConnectedItem in shared/types.ts).
type ExtItem struct {
	ID           string         `json:"id"` // the platform id (ref)
	ConnectionID string         `json:"connection_id"`
	Kind         string         `json:"kind"` // page | database
	Title        string         `json:"title"`
	Icon         *string        `json:"icon"`
	HasChildren  bool           `json:"has_children"`
	ParentRef    *string        `json:"parent_ref"`
	UpdatedAt    time.Time      `json:"updated_at"`
	Snippet      string         `json:"snippet,omitempty"`
	URL          string         `json:"url"`  // the page on the platform
	Link         string         `json:"link"` // the app path that opens it here
	Source       *app.SourceRef `json:"source"`
}

// ExtList is one page of connected items.
type ExtList struct {
	Items      []ExtItem `json:"items"`
	NextCursor *string   `json:"next_cursor"`
}

// ExtComment is a comment on a connected page (DocsConnectedComment).
type ExtComment struct {
	ID        string       `json:"id"`
	ThreadID  string       `json:"thread_id"`
	BlockID   *string      `json:"block_id"`
	Quote     *string      `json:"quote"`
	Body      []Span       `json:"body"`
	Author    *ExtPerson   `json:"author"`
	CreatedAt time.Time    `json:"created_at"`
	Resolved  bool         `json:"resolved"`
	Replies   []ExtComment `json:"replies"`
}

// ExtPerson is someone on a platform, shown by name.
type ExtPerson struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	AvatarURL *string `json:"avatar_url"`
}

// ConnectedPageDetail is GET /v1/docs/connected/{connID}/pages/{ref}: the
// PageDetail shape (so the editor, block views and database views render
// it) plus where it comes from and the editing session.
type ConnectedPageDetail struct {
	PageDetail
	Ref          string          `json:"ref"`
	ConnectionID string          `json:"connection_id"`
	SessionID    string          `json:"session_id"`
	Source       *app.SourceRef  `json:"source"`
	Connection   ConnectedSource `json:"connection"`
	URL          string          `json:"url"`
	// CanEdit: the account may change it on the platform and the connection
	// allows changes from here.
	CanEdit bool `json:"can_edit"`
	// ReadOnlyBlocks can't be changed here ("Edit in Notion").
	ReadOnlyBlocks []uuid.UUID `json:"read_only_blocks"`
	// Partial: the page has more blocks than Docs shows; open it on the platform.
	Partial     bool   `json:"partial"`
	DatabaseRef string `json:"database_ref,omitempty"` // a row's database
}

// ---- adapters -------------------------------------------------------------------------

// docsAdapter is one platform's documents API, built per request from the
// person's signing client and the connection's endpoints.
type docsAdapter interface {
	// tree lists a level of the browse tree: the roots (parent "", section
	// "" or "recent"/"shared" for Google) or a page's subpages.
	tree(ctx context.Context, parent, section, cursor string) (ExtList, error)
	search(ctx context.Context, q, cursor string, limit int) (ExtList, error)
	// load reads a page (blocks, refs) or a database (schema, views).
	load(ctx context.Context, ref string) (*extDoc, error)
	// apply runs block operations from the editor against the platform.
	apply(ctx context.Context, sess *extSession, ops []Op) (*OpsResult, error)
	// sync re-reads the page for an open session.
	sync(ctx context.Context, sess *extSession) (*SyncResult, error)
	rows(ctx context.Context, dbRef string, q extRowQuery) (*RowPage, error)
	setRow(ctx context.Context, rowRef string, title *string, values map[string]json.RawMessage) (*Row, error)
	createRow(ctx context.Context, dbRef, title string, values map[string]json.RawMessage) (*Row, error)
	createPage(ctx context.Context, parentRef, title string, blocks []*mdBlock) (ExtItem, error)
	comments(ctx context.Context, ref string) ([]ExtComment, error)
	addComment(ctx context.Context, ref string, body []Span, threadID, blockID string) (*ExtComment, error)
	rename(ctx context.Context, ref, title string) error
	pageURL(ref string) string
}

var errUnsupported = func(platform, what string) error {
	return httpx.NewError(http.StatusBadRequest, "unsupported", what+" isn't available for "+platform+" here. Do it in "+platform+".")
}

// extRowQuery is a database query in Docs' terms (property ids, filter ops).
type extRowQuery struct {
	filters []Filter
	sorts   []Sort
	search  string
	cursor  string
	limit   int
}

// extDoc is a loaded connected page or database.
type extDoc struct {
	ref, kind, title, url string
	icon                  *string
	pageID                uuid.UUID
	created, edited       time.Time
	canEdit               bool // the platform account may change it
	blocks                []*extBlock
	refs                  Refs
	schema                []Property
	views                 []View
	isRow                 bool
	props                 map[string]any
	db                    *DatabaseInfo
	dbRef                 string
	revision              string      // Google: the revision the blocks describe
	elemIDs               []uuid.UUID // Google: the block id of each body element (uuid.Nil: none)
	partial               bool
}

// extBlock is one block as the editor sees it plus where it lives on the
// platform.
type extBlock struct {
	Block
	platformID     string // Notion block id (differs from Block.ID for blocks made here)
	platformParent string // Notion: the real parent (page, column or block)
	platformType   string // Notion block type
	readOnly       bool
	edited         string // Notion last_edited_time as last seen
	hasKids        bool   // Notion: it has children on the platform
	synced         bool   // Notion: shown from a synced block (read-only, like its siblings)
}

// ---- editing sessions -------------------------------------------------------------------

// extSession is the server side of one open copy of a connected page: the
// blocks as that editor sees them, their platform ids, and (Google) the
// document revision they describe. Block operations translate through it,
// so the editor's ids stay stable while the platform assigns its own.
// Sessions live in memory for two hours after their last use (per person;
// another instance or a restart asks the editor to reload).
type extSession struct {
	mu         sync.Mutex
	id         string
	user       uuid.UUID
	connID     string
	ref        string
	pageID     uuid.UUID
	version    int64
	blocks     map[uuid.UUID]*extBlock
	byPlatform map[string]uuid.UUID
	revision   string
	elemIDs    []uuid.UUID
	readOnly   bool
	users      map[string]bool // platform people seen on the page (mentions survive edits)
	used       time.Time
}

const (
	extSessionTTL     = 2 * time.Hour
	maxExtSessions    = 5000
	maxSessionsByUser = 30
)

var extSessions = struct {
	sync.Mutex
	m map[string]*extSession
}{m: map[string]*extSession{}}

func newSessionID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// startSession records a loaded page for one editor.
func startSession(user uuid.UUID, connID string, doc *extDoc, readOnly bool) *extSession {
	sess := &extSession{id: newSessionID(), user: user, connID: connID, ref: doc.ref, pageID: doc.pageID, readOnly: readOnly, used: time.Now()}
	sess.reset(doc)
	extSessions.Lock()
	defer extSessions.Unlock()
	now := time.Now()
	var mine []*extSession
	for k, v := range extSessions.m {
		if now.Sub(v.used) > extSessionTTL {
			delete(extSessions.m, k)
			continue
		}
		if v.user == user {
			mine = append(mine, v)
		}
	}
	if len(mine) >= maxSessionsByUser {
		sort.Slice(mine, func(i, j int) bool { return mine[i].used.Before(mine[j].used) })
		for _, v := range mine[:len(mine)-maxSessionsByUser+1] {
			delete(extSessions.m, v.id)
		}
	}
	if len(extSessions.m) >= maxExtSessions {
		var oldest *extSession
		for _, v := range extSessions.m {
			if oldest == nil || v.used.Before(oldest.used) {
				oldest = v
			}
		}
		delete(extSessions.m, oldest.id)
	}
	extSessions.m[sess.id] = sess
	return sess
}

var errSessionExpired = httpx.NewError(http.StatusConflict, "connected_session_expired", "This page was open too long to save changes. Reload it to keep editing.")

// findSession returns the caller's session for the page, or an error the
// editor answers by reloading.
func findSession(id string, user uuid.UUID, connID, ref string) (*extSession, error) {
	extSessions.Lock()
	defer extSessions.Unlock()
	s, ok := extSessions.m[id]
	if !ok || s.user != user || s.connID != connID || s.ref != ref || time.Since(s.used) > extSessionTTL {
		return nil, errSessionExpired
	}
	s.used = time.Now()
	return s, nil
}

// reset replaces the session's view with a freshly loaded document.
func (s *extSession) reset(doc *extDoc) {
	s.blocks = map[uuid.UUID]*extBlock{}
	s.byPlatform = map[string]uuid.UUID{}
	s.users = map[string]bool{}
	for _, b := range doc.blocks {
		s.blocks[b.ID] = b
		if b.platformID != "" {
			s.byPlatform[b.platformID] = b.ID
		}
	}
	for id := range doc.refs.Users {
		s.users[id] = true
	}
	s.revision = doc.revision
	s.elemIDs = append([]uuid.UUID(nil), doc.elemIDs...)
}

// children returns parent's blocks (nil: the top level) in position order.
func (s *extSession) children(parent *uuid.UUID) []*extBlock {
	var out []*extBlock
	for _, b := range s.blocks {
		if (parent == nil && b.ParentID == nil) || (parent != nil && b.ParentID != nil && *b.ParentID == *parent) {
			out = append(out, b)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Position != out[j].Position {
			return out[i].Position < out[j].Position
		}
		return out[i].ID.String() < out[j].ID.String()
	})
	return out
}

// before returns the sibling under parent that a block at position follows.
func (s *extSession) before(parent *uuid.UUID, position string, self uuid.UUID) *extBlock {
	var prev *extBlock
	for _, b := range s.children(parent) {
		if b.ID == self {
			continue
		}
		if b.Position < position {
			prev = b
		}
	}
	return prev
}

// subtree is id and everything nested in it.
func (s *extSession) subtree(id uuid.UUID) []uuid.UUID {
	out := []uuid.UUID{id}
	for i := 0; i < len(out) && i < 10000; i++ {
		for _, b := range s.blocks {
			if b.ParentID != nil && *b.ParentID == out[i] {
				out = append(out, b.ID)
			}
		}
	}
	return out
}

// docOrder is every block in document order (parents before children).
func (s *extSession) docOrder() []*extBlock {
	var out []*extBlock
	var walk func(parent *uuid.UUID, depth int)
	walk = func(parent *uuid.UUID, depth int) {
		if depth > maxBlockDepth+4 {
			return
		}
		for _, b := range s.children(parent) {
			out = append(out, b)
			id := b.ID
			walk(&id, depth+1)
		}
	}
	walk(nil, 0)
	return out
}

func (s *extSession) remove(id uuid.UUID) []uuid.UUID {
	ids := s.subtree(id)
	for _, x := range ids {
		if b := s.blocks[x]; b != nil && b.platformID != "" {
			delete(s.byPlatform, b.platformID)
		}
		delete(s.blocks, x)
	}
	return ids
}

// result starts an ops result for the session's next version.
func (s *extSession) result(clientID string, actor uuid.UUID) *OpsResult {
	s.version++
	return &OpsResult{PageID: s.pageID, Version: s.version, ClientID: clientID, ActorID: actor, Blocks: []Block{}, Deleted: []uuid.UUID{}, Rejected: []Rejected{}}
}

func (s *extSession) touched(res *OpsResult, b *extBlock) {
	b.Version = res.Version
	b.UpdatedAt = time.Now().UTC()
	for i := range res.Blocks {
		if res.Blocks[i].ID == b.ID {
			res.Blocks[i] = b.Block
			return
		}
	}
	res.Blocks = append(res.Blocks, b.Block)
}

// syncResult is the whole page as the session sees it.
func (s *extSession) syncResult(page *Page) *SyncResult {
	out := &SyncResult{Page: page, Version: s.version, Full: true, Blocks: []Block{}, Deleted: []uuid.UUID{}}
	for _, b := range s.docOrder() {
		out.Blocks = append(out.Blocks, b.Block)
	}
	return out
}

// ---- building documents --------------------------------------------------------------------

// assignPositions gives every sibling group fractional positions in order.
func assignPositions(blocks []*extBlock) {
	groups := map[string][]*extBlock{}
	var order []string
	for _, b := range blocks {
		k := ""
		if b.ParentID != nil {
			k = b.ParentID.String()
		}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], b)
	}
	for _, k := range order {
		g := groups[k]
		keys := keysAfter("", "", len(g))
		for i, b := range g {
			b.Position = keys[i]
		}
	}
}

// newPage is the Page a connected document presents to the editor.
func newExtPage(doc *extDoc, org uuid.UUID, version int64, canEdit bool) *Page {
	p := &Page{ID: doc.pageID, OrgID: org, Kind: doc.kind, IsRow: doc.isRow, Title: doc.title, Icon: doc.icon,
		Position: "a0", Props: doc.props, Schema: doc.schema, Version: version, CreatedAt: doc.created, UpdatedAt: doc.edited, MyLevel: access.View}
	if p.Props == nil {
		p.Props = map[string]any{}
	}
	if canEdit {
		p.MyLevel = access.Edit
	}
	return p
}

// textBlockTypes are the editor's text block types (rich text in props.text).
func isTextBlock(t string) bool {
	for _, x := range textTypes {
		if x == t {
			return true
		}
	}
	return false
}

// incomingSpans reads and cleans an edit's text; mentions of people the
// platform doesn't know (app members picked in the editor) become plain text.
func incomingSpans(raw json.RawMessage, known map[string]bool) ([]Span, error) {
	spans, err := decodeSpans(raw)
	if err != nil {
		return nil, httpx.BadRequest("That text can't be read.")
	}
	clean, count := sanitizeSpans(spans, spanRules{validUser: func(id uuid.UUID) bool { return known[id.String()] }})
	if count > maxBlockChars {
		return nil, httpx.BadRequest("A block can hold up to 20000 characters.")
	}
	return clean, nil
}

// reject records an operation the platform adapter skipped.
func reject(res *OpsResult, op Op, code, msg string) {
	res.Rejected = append(res.Rejected, Rejected{ID: op.ID, Op: op.Op, Code: code, Message: msg})
}

// rejectAll refuses every remaining operation with err's code and message.
func rejectAll(res *OpsResult, ops []Op, err error) {
	code, msg := "platform_error", err.Error()
	if he, ok := err.(*httpx.Error); ok {
		code, msg = he.Code, he.Message
	}
	for _, op := range ops {
		reject(res, op, code, msg)
	}
}

// namespace for the ids Docs gives Google document parts.
var googleNS = uuid.MustParse("6f1c1a52-3c55-4f0e-9f2b-9b6b2f0b6c11")

func googleID(parts ...string) uuid.UUID {
	k := ""
	for i, p := range parts {
		if i > 0 {
			k += "\x00"
		}
		k += p
	}
	return uuid.NewSHA1(googleNS, []byte(k))
}

// platformUUID parses a platform's UUID id (Notion), or derives a stable
// one from any other id.
func platformUUID(id string) uuid.UUID {
	if u, err := uuid.Parse(id); err == nil {
		return u
	}
	return googleID("ref", id)
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// forgetConnectedUser drops a person's editing sessions and cached platform
// data (account deletion).
func forgetConnectedUser(user uuid.UUID) {
	extSessions.Lock()
	for k, v := range extSessions.m {
		if v.user == user {
			delete(extSessions.m, k)
		}
	}
	extSessions.Unlock()
	prefix := user.String() + "|"
	notionRoots.Lock()
	for k := range notionRoots.m {
		if strings.HasPrefix(k, prefix) {
			delete(notionRoots.m, k)
		}
	}
	notionRoots.Unlock()
	notionUsers.Lock()
	for k := range notionUsers.m {
		if strings.HasPrefix(k, prefix) {
			delete(notionUsers.m, k)
		}
	}
	notionUsers.Unlock()
}
