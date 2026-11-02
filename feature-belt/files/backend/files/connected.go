package files

// Connected drives: Google Drive (My Drive, Shared with me, shared drives),
// OneDrive and SharePoint document libraries, browsed, previewed and changed
// live with the person's own connection (app.Connections, the connectors
// feature). Nothing from a platform is stored here except the shortcuts
// people pin into a folder (files_links: a reference, never content).
//
// Rules that hold for every connected route and tool:
//   - A connection is used only by the person it belongs to: every call goes
//     through Connections.Client with the caller's id, so they reach exactly
//     what that account reaches on the platform.
//   - Reads need the provider's read capability, changes its write
//     capability (asked for separately, incrementally).
//   - Agents are capped by the files scope and the connection's own scope
//     (app.ConnectionScope); writes are separate tools with Source set.
//   - Platform errors are passed on honestly (no access, name taken, changed
//     since, rate limited, reconnect), never as a 401 (that signs people out).

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
)

// KindExternal is the agent-scope kind of an item on a connected platform;
// its id is "<connection id>:<platform item id>".
const KindExternal = "files.external"

// Capabilities files declares (app.CapabilityProvider). Read and write are
// separate so people grant changes only when they want them.
const (
	CapGoogleDrive          = "files.google.drive"
	CapGoogleDriveWrite     = "files.google.drive.write"
	CapOneDrive             = "files.microsoft.onedrive"
	CapOneDriveWrite        = "files.microsoft.onedrive.write"
	CapSharePoint           = "files.microsoft.sharepoint"
	CapSharePointWrite      = "files.microsoft.sharepoint.write"
	providerGoogle          = "google"
	providerMicrosoft       = "microsoft"
	productDrive            = "drive"
	productOneDrive         = "onedrive"
	productSharePoint       = "sharepoint"
	connectedPageSize       = 100
	connectedSearchTimeout  = 3 * time.Second
	connectedMetaTTL        = 30 * time.Second
	connectedProxyTTL       = 15 * time.Minute
	maxConnectedImport      = 10 << 20 // bytes buffered for an export of unknown size
	maxConnectedSmallUpload = 4 << 20  // server-side writes up to this use one request
)

// Capabilities implements app.CapabilityProvider with the narrowest scopes
// that work: Google needs drive.readonly to list and read every file the
// account can open (drive.file only reaches files this app created) and drive
// to change them; Microsoft needs Files.Read.All to open items others share
// (shortcuts) and Sites.Read.All to list followed SharePoint sites.
func (s *Service) Capabilities() []app.Capability {
	return []app.Capability{
		{Key: CapGoogleDrive, Module: "files", Provider: providerGoogle, Label: "Google Drive in Files",
			Scopes: []string{"https://www.googleapis.com/auth/drive.readonly"}},
		{Key: CapGoogleDriveWrite, Module: "files", Provider: providerGoogle, Label: "Change Google Drive files from Files", Write: true,
			Scopes: []string{"https://www.googleapis.com/auth/drive"}},
		{Key: CapOneDrive, Module: "files", Provider: providerMicrosoft, Label: "OneDrive in Files",
			Scopes: []string{"Files.Read.All"}},
		{Key: CapOneDriveWrite, Module: "files", Provider: providerMicrosoft, Label: "Change OneDrive files from Files", Write: true,
			Scopes: []string{"Files.ReadWrite.All"}},
		{Key: CapSharePoint, Module: "files", Provider: providerMicrosoft, Label: "SharePoint document libraries in Files",
			Scopes: []string{"Sites.Read.All", "Files.Read.All"}},
		{Key: CapSharePointWrite, Module: "files", Provider: providerMicrosoft, Label: "Change SharePoint files from Files", Write: true,
			Scopes: []string{"Sites.Read.All", "Files.ReadWrite.All"}},
	}
}

var (
	readCaps  = []string{CapGoogleDrive, CapOneDrive, CapSharePoint}
	writeCaps = map[string]string{CapGoogleDrive: CapGoogleDriveWrite, CapOneDrive: CapOneDriveWrite, CapSharePoint: CapSharePointWrite}
)

// productOfCap is the product a read capability opens.
func productOfCap(c string) string {
	switch c {
	case CapOneDrive:
		return productOneDrive
	case CapSharePoint:
		return productSharePoint
	}
	return productDrive
}

// ProductLabel is how people name a product: Google Drive, OneDrive, SharePoint.
func ProductLabel(product string) string {
	switch product {
	case productOneDrive:
		return "OneDrive"
	case productSharePoint:
		return "SharePoint"
	}
	return "Google Drive"
}

// ConnectedSource is one connected account in Files (sources.FeatureSource
// plus whether changes are allowed and what this account offers).
type ConnectedSource struct {
	ID         string   `json:"id"`
	Type       string   `json:"type"` // app | connected
	Provider   string   `json:"provider,omitempty"`
	Product    string   `json:"product,omitempty"`
	Products   []string `json:"products,omitempty"` // drive | onedrive, sharepoint
	Account    string   `json:"account,omitempty"`
	Label      string   `json:"label"`
	Status     string   `json:"status,omitempty"` // active | needs_reauth | blocked
	Capability string   `json:"capability,omitempty"`
	FixURL     string   `json:"fix_url,omitempty"`
	CanWrite   bool     `json:"can_write"`
	WriteURL   string   `json:"write_url,omitempty"` // grants changes, when CanWrite is false
}

// ConnectedSources is GET /v1/files/sources (sources.FeatureSources).
type ConnectedSources struct {
	Sources []ConnectedSource     `json:"sources"`
	Offers  []app.ConnectionOffer `json:"offers"`
}

// extPerson is someone on a platform, shown by name (FilesPerson's shape).
type extPerson struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	AvatarURL *string `json:"avatar_url"`
}

// ShortcutInfo describes what a shortcut item (type link) points at.
type ShortcutInfo struct {
	Provider string `json:"provider"`
	Product  string `json:"product"`
	MimeType string `json:"mime_type"`
	Folder   bool   `json:"folder"`
}

// ConnectedItem is a file or folder on a connected platform in the FilesItem
// shape, so clients reuse rows, previews and details, plus where it lives.
type ConnectedItem struct {
	ID             string        `json:"id"` // "<connection id>:<ref>"
	OrgID          string        `json:"org_id"`
	ParentID       *string       `json:"parent_id"`
	Type           string        `json:"type"` // folder | file
	Name           string        `json:"name"`
	Drive          string        `json:"drive"` // connected
	OwnerID        *string       `json:"owner_id"`
	MimeType       string        `json:"mime_type"`
	Size           int64         `json:"size"`
	TextLike       bool          `json:"text_like"`
	VersionID      *string       `json:"version_id"`
	VersionCount   int           `json:"version_count"`
	Preview        string        `json:"preview"`
	CreatedBy      *extPerson    `json:"created_by"`
	UpdatedBy      *extPerson    `json:"updated_by"`
	CreatedAt      time.Time     `json:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at"`
	MyLevel        string        `json:"my_level"` // view | edit
	Starred        bool          `json:"starred"`
	Shared         bool          `json:"shared"`
	PublicLink     bool          `json:"public_link"`
	PublicDisabled bool          `json:"public_disabled"`
	TrashedAt      *time.Time    `json:"trashed_at"`
	TrashedBy      *extPerson    `json:"trashed_by"`
	TypeInfo       *TypeInfo     `json:"type_info"`
	Link           string        `json:"link"`
	ThumbURL       *string       `json:"thumb_url"`
	Shortcut       *ShortcutInfo `json:"shortcut"`

	Source       *app.SourceRef `json:"source"`
	ConnectionID string         `json:"connection_id"`
	Ref          string         `json:"ref"`
	ParentRef    *string        `json:"parent_ref"`
	DriveRef     string         `json:"drive_ref"`            // the drive it's in (moves stay inside it)
	KindLabel    string         `json:"kind_label,omitempty"` // "Google Docs", "Word document"
	Native       bool           `json:"native"`               // a platform document: read-only preview, edited on the platform
	WebURL       string         `json:"web_url"`
	CanEdit      bool           `json:"can_edit"`  // the account may change it on the platform
	CanWrite     bool           `json:"can_write"` // the connection allows changes from here
}

// ConnectedCrumb is one folder above a connected item.
type ConnectedCrumb struct {
	Ref  string `json:"ref"`
	Name string `json:"name"`
	Link string `json:"link"`
}

// ConnectedDetail is GET /v1/files/connected/{connID}/items/{ref}.
type ConnectedDetail struct {
	Item   *ConnectedItem   `json:"item"`
	Path   []ConnectedCrumb `json:"path"` // top first, the drive's root included
	Source ConnectedSource  `json:"source"`
}

// ConnectedDrive is a place to browse in one connection: My Drive, Shared
// with me, a shared drive, OneDrive, a SharePoint library.
type ConnectedDrive struct {
	Ref  string `json:"ref"`
	Name string `json:"name"`
	Kind string `json:"kind"` // my_drive | shared_with_me | shared_drive | onedrive | library
	Site string `json:"site,omitempty"`
	Link string `json:"link"`
}

// ConnectedSharing is the platform's sharing summary, read-only.
type ConnectedSharing struct {
	Available bool               `json:"available"` // false: the platform doesn't show this account the sharing
	Owner     string             `json:"owner,omitempty"`
	Link      string             `json:"link,omitempty"` // anyone | organization | domain | ""
	LinkRole  string             `json:"link_role,omitempty"`
	Domain    string             `json:"domain,omitempty"`
	People    []ConnectedSharePe `json:"people"`
	ManageURL string             `json:"manage_url,omitempty"`
}

// ConnectedSharePe is one person or group with access on the platform.
type ConnectedSharePe struct {
	Name string `json:"name"`
	Role string `json:"role"` // owner | edit | comment | view
	Kind string `json:"kind"` // user | group
}

// ---- the platform adapters ------------------------------------------------------

// extItem is a platform item as both adapters describe it.
type extItem struct {
	Ref, ParentRef, DriveRef string
	Name, MimeType           string
	Folder                   bool
	Size                     int64
	Created, Modified        time.Time
	ModifiedBy, CreatedBy    string
	WebURL                   string
	Native                   string // the platform's document type label ("Google Docs"); "" for ordinary files
	NativeExport             string // export type for downloads of a native document ("" = none)
	NativeExt                string
	CanEdit                  bool
	Starred, Shared          bool
	Version                  string // compares content versions (Drive version, Graph eTag)
	ThumbURL                 string
	DownloadURL              string // a short-lived pre-authenticated link (Graph)
	Root                     bool   // a drive's top folder
	OwnedByMe                bool
}

type extPage struct {
	Items []extItem
	Next  string
}

type extCrumb struct{ Ref, Name string }

type extUpload struct {
	URL       string            `json:"upload_url"`
	ChunkSize int64             `json:"chunk_size"`
	Headers   map[string]string `json:"headers"`
	RefPrefix string            `json:"ref_prefix"`
	ExpiresAt time.Time         `json:"expires_at"`
}

// driveAdapter is one platform's file API, built per request from the
// person's signing client and the connection's endpoints.
type driveAdapter interface {
	provider() string
	product(ref string) string
	drives(ctx context.Context, cursor string) ([]ConnectedDrive, string, error)
	get(ctx context.Context, ref string) (extItem, error)
	list(ctx context.Context, folder, sort string, desc, thumbs bool, cursor string) (extPage, error)
	path(ctx context.Context, it extItem) ([]extCrumb, error)
	search(ctx context.Context, q, cursor string, limit int) (extPage, error)
	// open streams content: the file, or (format "pdf") a PDF rendering of a
	// document; export downloads a native document in NativeExport.
	open(ctx context.Context, it extItem, format, rangeHeader string) (*http.Response, error)
	sharing(ctx context.Context, it extItem) (ConnectedSharing, error)
	createFolder(ctx context.Context, parent, name string) (extItem, error)
	rename(ctx context.Context, it extItem, name string) (extItem, error)
	move(ctx context.Context, it extItem, parent extItem) (extItem, error)
	trash(ctx context.Context, it extItem) error
	// startUpload opens a resumable session the browser sends bytes to
	// (replace: a new version of that item).
	startUpload(ctx context.Context, parent string, replace *extItem, name, mime string, size int64, origin string) (extUpload, error)
	// write stores bytes from the server: a new file in parent, or the
	// content of replace (ifVersion: refuse when it changed since).
	write(ctx context.Context, parent string, replace *extItem, name, mime string, size int64, body readerAt) (extItem, error)
	// rootRef is where new files go when no folder is chosen.
	rootRef(ctx context.Context) (string, error)
	// externalID is the platform item id part of an agent-scope id.
	externalID(ref string) string
}

// readerAt is the body a server-side write sends: the whole of a small file,
// or a stream read once in order.
type readerAt interface {
	Read(p []byte) (int, error)
}

// ---- connections -------------------------------------------------------------------

// connInfo is one of the caller's connections as files sees it.
type connInfo struct {
	app.Connection
	read     []string // granted read capabilities, in readCaps order
	writeCap string   // a granted write capability ("" = read only)
}

func (c *connInfo) product() string {
	if len(c.read) == 0 {
		return productDrive
	}
	return productOfCap(c.read[0])
}

func (c *connInfo) label() string {
	return ProductLabel(c.product()) + " · " + c.Account
}

func (c *connInfo) source(product, url string) *app.SourceRef {
	return app.ConnectedSource(c.Connection, product, url)
}

func (s *Service) connections() app.Connections { return app.FindConnections(s.d.Modules) }

// connectedFor lists the caller's connections that grant any files
// capability in org (uuid.Nil: no workspace context), in a stable order.
func (s *Service) connectedFor(ctx context.Context, me, org uuid.UUID) ([]*connInfo, error) {
	cx := s.connections()
	if cx == nil {
		return nil, nil
	}
	byID := map[string]*connInfo{}
	var order []string
	for _, c := range readCaps {
		list, err := cx.For(ctx, me, org, c)
		if err != nil {
			return nil, err
		}
		for _, cn := range list {
			ci := byID[cn.ID]
			if ci == nil {
				ci = &connInfo{Connection: cn}
				byID[cn.ID] = ci
				order = append(order, cn.ID)
			}
			ci.read = append(ci.read, c)
		}
	}
	for _, rc := range readCaps {
		list, err := cx.For(ctx, me, org, writeCaps[rc])
		if err != nil {
			return nil, err
		}
		for _, cn := range list {
			if ci := byID[cn.ID]; ci != nil && ci.writeCap == "" && cn.Status == "active" {
				ci.writeCap = writeCaps[rc]
			}
		}
	}
	out := make([]*connInfo, 0, len(order))
	for _, id := range order {
		out = append(out, byID[id])
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Provider != out[j].Provider {
			return out[i].Provider < out[j].Provider
		}
		return strings.ToLower(out[i].Account) < strings.ToLower(out[j].Account)
	})
	return out, nil
}

// connSession is an authenticated adapter for one request.
type connSession struct {
	info       *connInfo
	ad         driveAdapter
	me         uuid.UUID
	s          *Service
	capability string
}

var (
	errNoConnections  = httpx.NewError(http.StatusServiceUnavailable, app.ConnectionUnavailable, "Connected accounts aren't available in this app.")
	errConnectionGone = httpx.NewError(http.StatusNotFound, app.ConnectionNotFound, "That connected account isn't available. Connect it again in Settings → Connections.")
)

// session opens connID for the caller: the read capability, or the write
// one (write). It refuses a blocked or read-only connection with a fix link.
func (s *Service) session(ctx context.Context, me, org uuid.UUID, connID string, write bool) (*connSession, error) {
	cx := s.connections()
	if cx == nil {
		return nil, errNoConnections
	}
	all, err := s.connectedFor(ctx, me, org)
	if err != nil {
		return nil, err
	}
	var info *connInfo
	for _, c := range all {
		if c.ID == connID {
			info = c
		}
	}
	if info == nil {
		return nil, errConnectionGone
	}
	switch info.Status {
	case "blocked":
		return nil, httpx.NewError(http.StatusForbidden, app.ConnectionBlocked, "Your workspace doesn't allow "+ProductLabel(info.product())+" here.")
	case "needs_reauth":
		return nil, s.needsAuth(info, info.read[0])
	}
	capability := info.read[0]
	if write {
		if info.writeCap == "" {
			e := httpx.NewError(http.StatusConflict, app.ConnectionScopeMissing,
				"Allow changes to "+ProductLabel(info.product())+" first: this connection can only read.")
			e.Details = map[string]any{"fix_url": s.grantURL(ctx, me, org, writeCaps[info.read[0]]), "capability": writeCaps[info.read[0]], "connection_id": info.ID}
			return nil, e
		}
		capability = info.writeCap
	}
	if org != uuid.Nil {
		ctx = app.WithConnectionOrg(ctx, org)
	}
	client, cn, err := cx.Client(ctx, me, connID, capability)
	if err != nil {
		return nil, s.connError(info, capability, err)
	}
	info.Connection.Endpoints = cn.Endpoints
	wrap := func(err error) error { return s.connError(info, capability, err) }
	ad, err := newAdapter(info, client, wrap)
	if err != nil {
		return nil, err
	}
	return &connSession{info: info, ad: ad, me: me, s: s, capability: capability}, nil
}

func newAdapter(info *connInfo, client *http.Client, wrap func(error) error) (driveAdapter, error) {
	switch info.Provider {
	case providerGoogle:
		return newGoogleDrive(info, client, wrap), nil
	case providerMicrosoft:
		return newGraphDrive(info, client, wrap), nil
	}
	return nil, httpx.BadRequest("Files can't open " + info.Provider + " accounts.")
}

func (s *Service) needsAuth(info *connInfo, capability string) error {
	e := httpx.NewError(http.StatusConflict, app.ConnectionNeedsAuth, "Reconnect "+info.label()+" to continue.")
	e.Details = map[string]any{"fix_url": connectURL(capability, ""), "capability": capability, "connection_id": info.ID}
	return e
}

// connError passes a Connections failure on with its code (never a 401:
// that signs people out) and the link that fixes it.
func (s *Service) connError(info *connInfo, capability string, err error) error {
	var he *httpx.Error
	if !errors.As(err, &he) {
		return err
	}
	out := *he
	if out.Status == http.StatusUnauthorized {
		out.Status, out.Code = http.StatusConflict, app.ConnectionNeedsAuth
	}
	switch out.Code {
	case app.ConnectionNeedsAuth, app.ConnectionScopeMissing:
		d := map[string]any{"fix_url": connectURL(capability, ""), "capability": capability, "connection_id": info.ID}
		for k, v := range he.Details {
			d[k] = v
		}
		out.Details = d
	}
	return &out
}

// connectURL is the connections screen for a capability (TS:
// connectionFixPath), coming back to returnTo.
func connectURL(capability, returnTo string) string {
	q := url.Values{}
	if capability != "" {
		q.Set("capability", capability)
	}
	if strings.HasPrefix(returnTo, "/") && !strings.HasPrefix(returnTo, "//") {
		q.Set("return_to", returnTo)
	}
	if len(q) == 0 {
		return "/settings/connections"
	}
	return "/settings/connections?" + q.Encode()
}

// grantURL is where a person grants capability: the connectors feature's
// offer when it has one, else the connections screen.
func (s *Service) grantURL(ctx context.Context, me, org uuid.UUID, capability string) string {
	if cx := s.connections(); cx != nil {
		if offers, err := cx.Offers(ctx, me, org, s.Name()); err == nil {
			for _, o := range offers {
				if o.Capability == capability && o.ConnectURL != "" {
					return o.ConnectURL
				}
			}
		}
	}
	return connectURL(capability, "")
}

// sources builds GET /v1/files/sources: the app's own drive, then each
// connected account, and the capabilities still on offer.
func (s *Service) sources(ctx context.Context, me, org uuid.UUID) (ConnectedSources, error) {
	out := ConnectedSources{Sources: []ConnectedSource{{ID: "app", Type: "app", Label: "Files", CanWrite: true}}, Offers: []app.ConnectionOffer{}}
	cx := s.connections()
	if cx == nil {
		return out, nil
	}
	conns, err := s.connectedFor(ctx, me, org)
	if err != nil {
		return out, err
	}
	offers, err := cx.Offers(ctx, me, org, s.Name())
	if err != nil {
		return out, err
	}
	for _, c := range conns {
		src := ConnectedSource{ID: c.ID, Type: "connected", Provider: c.Provider, Product: c.product(), Account: c.Account,
			Label: c.label(), Status: c.Status, Capability: c.read[0], CanWrite: c.writeCap != ""}
		for _, rc := range c.read {
			src.Products = append(src.Products, productOfCap(rc))
		}
		if c.Status != "active" {
			src.FixURL = connectURL(c.read[0], "")
		}
		if !src.CanWrite {
			src.WriteURL = connectURL(writeCaps[c.read[0]], "")
			for _, o := range offers {
				if o.Capability == writeCaps[c.read[0]] && o.ConnectURL != "" {
					src.WriteURL = o.ConnectURL
				}
			}
		}
		out.Sources = append(out.Sources, src)
	}
	// Offer what the person could still connect for browsing; changes are
	// offered per account (WriteURL).
	for _, o := range offers {
		if strings.HasPrefix(o.Capability, "files.") && !strings.HasSuffix(o.Capability, ".write") {
			out.Offers = append(out.Offers, o)
		}
	}
	return out, nil
}

// ---- converting adapter items --------------------------------------------------------

// itemRefID is the client id of a connected item.
func itemRefID(connID, ref string) string { return connID + ":" + ref }

// ConnectedLink is the app path that opens a connected item or folder.
func ConnectedLink(connID, ref string) string {
	return "/files/c/" + url.PathEscape(connID) + "/" + url.PathEscape(ref)
}

// toConnected shapes a platform item as a FilesItem with its source.
func (cs *connSession) toConnected(it extItem) *ConnectedItem {
	connID := cs.info.ID
	canWrite := cs.info.writeCap != ""
	out := &ConnectedItem{
		ID: itemRefID(connID, it.Ref), Type: TypeFile, Name: it.Name, Drive: "connected", MimeType: it.MimeType, Size: it.Size,
		CreatedAt: it.Created, UpdatedAt: it.Modified, Starred: it.Starred, Shared: it.Shared, Link: ConnectedLink(connID, it.Ref),
		Source: cs.info.source(cs.ad.product(it.Ref), it.WebURL), ConnectionID: connID, Ref: it.Ref, DriveRef: it.DriveRef, WebURL: it.WebURL,
		CanEdit: it.CanEdit, CanWrite: canWrite, MyLevel: "view", KindLabel: it.Native, Native: it.Native != "",
	}
	if it.Folder {
		out.Type = TypeFolder
	}
	if it.CanEdit && canWrite && !out.Native {
		out.MyLevel = "edit"
	}
	if it.ParentRef != "" {
		p := it.ParentRef
		pid := itemRefID(connID, p)
		out.ParentRef, out.ParentID = &p, &pid
	}
	if it.Version != "" {
		v := it.Version
		out.VersionID = &v
	}
	if it.ModifiedBy != "" {
		out.UpdatedBy = &extPerson{Name: it.ModifiedBy}
	}
	if it.CreatedBy != "" {
		out.CreatedBy = &extPerson{Name: it.CreatedBy}
	}
	if it.ThumbURL != "" {
		t := it.ThumbURL
		out.ThumbURL = &t
	}
	out.TextLike = !it.Folder && it.Native == "" && (isTextual(baseType(it.MimeType)) || codeTypes[extOf(it.Name)] != "")
	out.Preview = connectedPreview(cs.ad, it, out.TextLike)
	if out.KindLabel == "" && out.Preview == "pdf" && officeTypes[baseType(it.MimeType)] {
		out.KindLabel = officeLabel(it.Name)
	}
	return out
}

// connectedPreview is how clients show a connected item. Native documents
// and Office files the platform converts preview as PDF.
func connectedPreview(ad driveAdapter, it extItem, textLike bool) string {
	if it.Folder {
		return TypeFolder
	}
	if it.Native != "" {
		if it.NativeExport != "" {
			return "pdf"
		}
		return "other"
	}
	mt := baseType(it.MimeType)
	if officeTypes[mt] && ad.provider() == providerMicrosoft && mt != "application/x-iwork-keynote-sffkey" &&
		mt != "application/x-iwork-pages-sffpages" && mt != "application/x-iwork-numbers-sffnumbers" {
		return "pdf" // Graph converts Office files (?format=pdf)
	}
	return previewKind(TypeFile, mt, it.Name, textLike)
}

func officeLabel(name string) string {
	switch extOf(name) {
	case "doc", "docx", "odt", "rtf":
		return "Word document"
	case "xls", "xlsx", "ods":
		return "Excel workbook"
	case "ppt", "pptx", "odp":
		return "PowerPoint presentation"
	}
	return "Document"
}

func (cs *connSession) crumbs(list []extCrumb) []ConnectedCrumb {
	out := make([]ConnectedCrumb, 0, len(list))
	for _, c := range list {
		out = append(out, ConnectedCrumb{Ref: c.Ref, Name: c.Name, Link: ConnectedLink(cs.info.ID, c.Ref)})
	}
	return out
}

func (cs *connSession) sourceView() ConnectedSource {
	c := cs.info
	out := ConnectedSource{ID: c.ID, Type: "connected", Provider: c.Provider, Product: c.product(), Account: c.Account, Label: c.label(),
		Status: c.Status, Capability: c.read[0], CanWrite: c.writeCap != ""}
	if !out.CanWrite {
		out.WriteURL = connectURL(writeCaps[c.read[0]], "")
	}
	return out
}

// ---- a short per-person cache of item metadata ---------------------------------------

type metaEntry struct {
	it  extItem
	exp time.Time
}

var metaCache = struct {
	sync.Mutex
	m map[string]metaEntry
}{m: map[string]metaEntry{}}

func metaKey(me uuid.UUID, connID, ref string) string { return me.String() + "|" + connID + "|" + ref }

// getItem reads an item through the cache (30 s, per person and connection;
// never shared between people).
func (cs *connSession) getItem(ctx context.Context, ref string) (extItem, error) {
	k := metaKey(cs.me, cs.info.ID, ref)
	metaCache.Lock()
	e, ok := metaCache.m[k]
	metaCache.Unlock()
	if ok && time.Now().Before(e.exp) {
		return e.it, nil
	}
	it, err := cs.ad.get(ctx, ref)
	if err != nil {
		return extItem{}, err
	}
	cs.remember(ref, it)
	return it, nil
}

func (cs *connSession) remember(ref string, it extItem) {
	metaCache.Lock()
	defer metaCache.Unlock()
	if len(metaCache.m) > 20_000 {
		metaCache.m = map[string]metaEntry{}
	}
	exp := time.Now().Add(connectedMetaTTL)
	metaCache.m[metaKey(cs.me, cs.info.ID, ref)] = metaEntry{it: it, exp: exp}
	if it.Ref != ref {
		metaCache.m[metaKey(cs.me, cs.info.ID, it.Ref)] = metaEntry{it: it, exp: exp}
	}
}

func (cs *connSession) forget(refs ...string) {
	metaCache.Lock()
	defer metaCache.Unlock()
	for _, r := range refs {
		delete(metaCache.m, metaKey(cs.me, cs.info.ID, r))
	}
}

// ---- agent scope ---------------------------------------------------------------------

// externalTarget is the agent-scope id of a connected item:
// "<connection id>:<platform id>", within the agents feature's id rules
// ([A-Za-z0-9_.:/-], up to 200; OneDrive ids' "!" becomes ".").
func externalTarget(connID, platformID string) string {
	id := connID + ":" + strings.ReplaceAll(platformID, "!", ".")
	if len(id) > 200 {
		id = id[:200]
	}
	return id
}

// agentAllows checks the connection's own agent scope for one item (always
// true for people and without an agent policy).
func (s *Service) agentAllows(ctx context.Context, caller *auth.Identity, connID, platformID string, need app.AgentLevel) error {
	ok, err := app.AgentAllows(ctx, s.d.Modules, caller, app.ConnectionScope(connID), KindExternal, externalTarget(connID, platformID), need)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	switch need {
	case app.AgentRead:
		return httpx.NewError(http.StatusForbidden, "agent_scope_off", "The person hasn't given AI agents access to this connected account. They can change it in Settings → AI agents.")
	case app.AgentSuggest:
		return httpx.NewError(http.StatusForbidden, "agent_scope_read_only", "The person lets AI agents read this connected account but not change it. They can allow suggestions or edits in Settings → AI agents.")
	}
	return httpx.NewError(http.StatusForbidden, "agent_scope_suggest_only", "The person lets AI agents only suggest changes in this connected account. Ask them to make the change, or to allow edits in Settings → AI agents.")
}

// orgOf reads ?org_id= (optional) and checks the caller is a member.
func (s *Service) orgOf(ctx context.Context, r *http.Request, me uuid.UUID) (uuid.UUID, error) {
	raw := r.URL.Query().Get("org_id")
	if raw == "" {
		return uuid.Nil, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, httpx.NotFound(orgs.NounTitle() + " not found.")
	}
	role, err := orgs.RoleOf(ctx, s.d.Pool, id, me)
	if err != nil {
		return uuid.Nil, err
	}
	if role == "" {
		return uuid.Nil, httpx.NotFound(orgs.NounTitle() + " not found.")
	}
	return id, nil
}

// fmtProduct is "Google Drive · sam@acme.com" for search context and copy.
func fmtProduct(product, account string) string {
	return fmt.Sprintf("%s · %s", ProductLabel(product), account)
}
