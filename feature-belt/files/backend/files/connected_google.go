package files

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"scaffoldapp/backend/internal/httpx"
)

// googleDrive is the Drive v3 adapter (My Drive, Shared with me, shared
// drives). Refs are Drive file ids, plus "root" (My Drive) and
// "shared-with-me" (a list, not a folder). Every call passes
// supportsAllDrives so shared drives work.
type googleDrive struct {
	p      platform
	base   string // …/drive/v3
	upload string // …/upload/drive/v3
}

const (
	googleFolder     = "application/vnd.google-apps.folder"
	googleShortcut   = "application/vnd.google-apps.shortcut"
	googleRoot       = "root"
	googleSharedWith = "shared-with-me"
	googleFields     = "id,name,mimeType,size,createdTime,modifiedTime,parents,driveId,webViewLink,thumbnailLink,starred,shared,ownedByMe,version," +
		"lastModifyingUser(displayName),owners(displayName),capabilities(canEdit,canRename,canTrash,canAddChildren,canMoveItemWithinDrive)," +
		"shortcutDetails(targetId,targetMimeType)"
)

// googleNative are Google's own document types: previewed as PDF exports,
// downloaded and copied as Office files, edited only in Google.
var googleNative = map[string]struct{ label, export, ext string }{
	"application/vnd.google-apps.document":     {"Google Docs", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "docx"},
	"application/vnd.google-apps.spreadsheet":  {"Google Sheets", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "xlsx"},
	"application/vnd.google-apps.presentation": {"Google Slides", "application/vnd.openxmlformats-officedocument.presentationml.presentation", "pptx"},
	"application/vnd.google-apps.drawing":      {"Google Drawings", "image/png", "png"},
	"application/vnd.google-apps.form":         {"Google Forms", "", ""},
	"application/vnd.google-apps.site":         {"Google Sites", "", ""},
	"application/vnd.google-apps.map":          {"Google My Maps", "", ""},
	"application/vnd.google-apps.jam":          {"Jamboard", "", ""},
	"application/vnd.google-apps.script":       {"Apps Script", "", ""},
	"application/vnd.google-apps.vid":          {"Google Vids", "", ""},
}

func newGoogleDrive(info *connInfo, client *http.Client, wrap func(error) error) *googleDrive {
	base, upload := info.Endpoints["drive"], info.Endpoints["drive_upload"]
	if api := strings.TrimRight(info.Endpoints["api"], "/"); base == "" && api != "" {
		base = api + "/drive/v3"
	}
	if base == "" {
		base = "https://www.googleapis.com/drive/v3"
	}
	base = strings.TrimRight(base, "/")
	if upload == "" {
		if i := strings.LastIndex(base, "/drive/"); i >= 0 {
			upload = base[:i] + "/upload" + base[i:]
		} else {
			upload = base + "/upload"
		}
	}
	return &googleDrive{p: platform{client: client, name: "Google Drive", wrap: wrap}, base: base, upload: strings.TrimRight(upload, "/")}
}

func (g *googleDrive) provider() string           { return providerGoogle }
func (g *googleDrive) product(string) string      { return productDrive }
func (g *googleDrive) externalID(r string) string { return r }

type gFile struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	MimeType          string   `json:"mimeType"`
	Size              string   `json:"size"`
	CreatedTime       string   `json:"createdTime"`
	ModifiedTime      string   `json:"modifiedTime"`
	Parents           []string `json:"parents"`
	DriveID           string   `json:"driveId"`
	WebViewLink       string   `json:"webViewLink"`
	ThumbnailLink     string   `json:"thumbnailLink"`
	Starred           bool     `json:"starred"`
	Shared            bool     `json:"shared"`
	OwnedByMe         bool     `json:"ownedByMe"`
	Version           string   `json:"version"`
	LastModifyingUser *struct {
		DisplayName string `json:"displayName"`
	} `json:"lastModifyingUser"`
	Owners []struct {
		DisplayName string `json:"displayName"`
	} `json:"owners"`
	Capabilities *struct {
		CanEdit                bool `json:"canEdit"`
		CanRename              bool `json:"canRename"`
		CanTrash               bool `json:"canTrash"`
		CanAddChildren         bool `json:"canAddChildren"`
		CanMoveItemWithinDrive bool `json:"canMoveItemWithinDrive"`
	} `json:"capabilities"`
	ShortcutDetails *struct {
		TargetID       string `json:"targetId"`
		TargetMimeType string `json:"targetMimeType"`
	} `json:"shortcutDetails"`
}

func (f gFile) item() extItem {
	it := extItem{Ref: f.ID, Name: f.Name, MimeType: f.MimeType, Folder: f.MimeType == googleFolder, WebURL: f.WebViewLink,
		Starred: f.Starred, Shared: f.Shared, Version: f.Version, DriveRef: f.DriveID, OwnedByMe: f.OwnedByMe}
	if f.ShortcutDetails != nil && f.ShortcutDetails.TargetID != "" {
		// A Drive shortcut opens what it points at.
		it.Ref, it.MimeType = f.ShortcutDetails.TargetID, f.ShortcutDetails.TargetMimeType
		it.Folder = it.MimeType == googleFolder
	}
	if len(f.Parents) > 0 {
		it.ParentRef = f.Parents[0]
	}
	if it.DriveRef == "" {
		it.DriveRef = googleRoot
	}
	it.Size, _ = strconv.ParseInt(f.Size, 10, 64)
	it.Created, _ = time.Parse(time.RFC3339, f.CreatedTime)
	it.Modified, _ = time.Parse(time.RFC3339, f.ModifiedTime)
	if f.LastModifyingUser != nil {
		it.ModifiedBy = f.LastModifyingUser.DisplayName
	}
	if len(f.Owners) > 0 {
		it.CreatedBy = f.Owners[0].DisplayName
	}
	if f.Capabilities != nil {
		it.CanEdit = f.Capabilities.CanEdit
	}
	if n, ok := googleNative[it.MimeType]; ok {
		it.Native, it.NativeExport, it.NativeExt = n.label, n.export, n.ext
	}
	if f.ThumbnailLink != "" && strings.HasPrefix(f.ThumbnailLink, "https://") {
		it.ThumbURL = f.ThumbnailLink
	}
	// The top of a shared drive is a folder whose id is the drive's.
	it.Root = f.DriveID != "" && f.ID == f.DriveID
	return it
}

func (g *googleDrive) url(path string, q url.Values) string {
	if q == nil {
		q = url.Values{}
	}
	q.Set("supportsAllDrives", "true")
	return g.base + path + "?" + q.Encode()
}

func (g *googleDrive) drives(ctx context.Context, cursor string) ([]ConnectedDrive, string, error) {
	var out []ConnectedDrive
	if cursor == "" {
		out = append(out, ConnectedDrive{Ref: googleRoot, Name: "My Drive", Kind: "my_drive"},
			ConnectedDrive{Ref: googleSharedWith, Name: "Shared with me", Kind: "shared_with_me"})
	}
	q := url.Values{"pageSize": {"100"}, "fields": {"nextPageToken,drives(id,name)"}}
	if cursor != "" {
		q.Set("pageToken", cursor)
	}
	var res struct {
		NextPageToken string `json:"nextPageToken"`
		Drives        []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"drives"`
	}
	if _, err := g.p.json(ctx, http.MethodGet, g.base+"/drives?"+q.Encode(), nil, &res, nil); err != nil {
		return nil, "", err
	}
	for _, d := range res.Drives {
		out = append(out, ConnectedDrive{Ref: d.ID, Name: d.Name, Kind: "shared_drive"})
	}
	return out, res.NextPageToken, nil
}

func (g *googleDrive) get(ctx context.Context, ref string) (extItem, error) {
	if ref == googleSharedWith {
		return extItem{Ref: googleSharedWith, Name: "Shared with me", Folder: true, MimeType: googleFolder, Root: true, DriveRef: googleSharedWith,
			WebURL: "https://drive.google.com/drive/shared-with-me"}, nil
	}
	var f gFile
	if _, err := g.p.json(ctx, http.MethodGet, g.url("/files/"+url.PathEscape(ref), url.Values{"fields": {googleFields}}), nil, &f, nil); err != nil {
		return extItem{}, err
	}
	it := f.item()
	if ref == googleRoot || (len(f.Parents) == 0 && f.DriveID == "" && f.OwnedByMe && f.MimeType == googleFolder && f.Name == "My Drive") {
		it.Ref, it.Root, it.Name, it.DriveRef = googleRoot, true, "My Drive", googleRoot
	}
	return it, nil
}

// orderBy maps the app's sort to Drive's (folders first, like the app).
func googleOrder(sort string, desc bool) string {
	field := "name_natural"
	switch sort {
	case "updated":
		field = "modifiedTime"
	case "size":
		field = "quotaBytesUsed"
	}
	if desc {
		field += " desc"
	}
	return "folder," + field
}

func (g *googleDrive) list(ctx context.Context, folder, sort string, desc, _ bool, cursor string) (extPage, error) {
	q := url.Values{"pageSize": {strconv.Itoa(connectedPageSize)}, "fields": {"nextPageToken,files(" + googleFields + ")"},
		"orderBy": {googleOrder(sort, desc)}, "includeItemsFromAllDrives": {"true"}}
	switch folder {
	case googleSharedWith:
		q.Set("q", "sharedWithMe = true and trashed = false")
		q.Set("corpora", "user")
	case googleRoot:
		q.Set("q", "'root' in parents and trashed = false")
		q.Set("corpora", "user")
	default:
		q.Set("q", "'"+driveQuote(folder)+"' in parents and trashed = false")
		q.Set("corpora", "allDrives")
	}
	if cursor != "" {
		q.Set("pageToken", cursor)
	}
	return g.files(ctx, q)
}

func (g *googleDrive) files(ctx context.Context, q url.Values) (extPage, error) {
	var res struct {
		NextPageToken string  `json:"nextPageToken"`
		Files         []gFile `json:"files"`
	}
	if _, err := g.p.json(ctx, http.MethodGet, g.url("/files", q), nil, &res, nil); err != nil {
		return extPage{}, err
	}
	out := extPage{Items: make([]extItem, 0, len(res.Files)), Next: res.NextPageToken}
	for _, f := range res.Files {
		out.Items = append(out.Items, f.item())
	}
	return out, nil
}

// driveQuote escapes a value inside a Drive query string literal.
func driveQuote(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `'`, `\'`)
}

func (g *googleDrive) path(ctx context.Context, it extItem) ([]extCrumb, error) {
	if it.Root {
		return nil, nil
	}
	var up []extCrumb
	parent := it.ParentRef
	for depth := 0; parent != "" && depth < 16; depth++ {
		p, err := g.get(ctx, parent)
		if err != nil {
			if isStatus(err, http.StatusNotFound) || isStatus(err, http.StatusForbidden) {
				break // a folder above that this account can't open
			}
			return nil, err
		}
		up = append(up, extCrumb{Ref: p.Ref, Name: p.Name})
		if p.Root {
			break
		}
		parent = p.ParentRef
		if parent == "" && !p.OwnedByMe && p.DriveRef == googleRoot {
			up = append(up, extCrumb{Ref: googleSharedWith, Name: "Shared with me"})
		}
	}
	if len(up) == 0 && !it.OwnedByMe && it.DriveRef == googleRoot {
		up = append(up, extCrumb{Ref: googleSharedWith, Name: "Shared with me"})
	}
	for i, j := 0, len(up)-1; i < j; i, j = i+1, j-1 {
		up[i], up[j] = up[j], up[i]
	}
	return up, nil
}

func (g *googleDrive) search(ctx context.Context, query, cursor string, limit int) (extPage, error) {
	qv := driveQuote(query)
	q := url.Values{"pageSize": {strconv.Itoa(limit)}, "fields": {"nextPageToken,files(" + googleFields + ")"},
		"q":       {"(name contains '" + qv + "' or fullText contains '" + qv + "') and trashed = false"},
		"corpora": {"allDrives"}, "includeItemsFromAllDrives": {"true"}}
	if cursor != "" {
		q.Set("pageToken", cursor)
	}
	return g.files(ctx, q)
}

func (g *googleDrive) open(ctx context.Context, it extItem, format, rangeHeader string) (*http.Response, error) {
	var u string
	switch {
	case it.Native != "" && format == "pdf":
		u = g.url("/files/"+url.PathEscape(it.Ref)+"/export", url.Values{"mimeType": {"application/pdf"}})
	case it.Native != "":
		if it.NativeExport == "" {
			return nil, httpx.NewError(http.StatusUnsupportedMediaType, "not_exportable", it.Native+" files can only be opened in Google.")
		}
		u = g.url("/files/"+url.PathEscape(it.Ref)+"/export", url.Values{"mimeType": {it.NativeExport}})
		rangeHeader = ""
	case format == "pdf":
		return nil, httpx.NewError(http.StatusUnsupportedMediaType, "no_preview", "Google Drive can't show this file as a PDF.")
	default:
		u = g.url("/files/"+url.PathEscape(it.Ref), url.Values{"alt": {"media"}})
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if rangeHeader != "" && it.Native == "" {
		req.Header.Set("Range", rangeHeader)
	}
	return g.p.send(req)
}

func (g *googleDrive) sharing(ctx context.Context, it extItem) (ConnectedSharing, error) {
	out := ConnectedSharing{People: []ConnectedSharePe{}, ManageURL: it.WebURL}
	if it.Root {
		return out, nil
	}
	var res struct {
		Permissions []struct {
			Type         string `json:"type"` // user group domain anyone
			Role         string `json:"role"` // owner organizer fileOrganizer writer commenter reader
			EmailAddress string `json:"emailAddress"`
			DisplayName  string `json:"displayName"`
			Domain       string `json:"domain"`
		} `json:"permissions"`
	}
	q := url.Values{"fields": {"permissions(type,role,emailAddress,displayName,domain)"}, "pageSize": {"100"}}
	if _, err := g.p.json(ctx, http.MethodGet, g.url("/files/"+url.PathEscape(it.Ref)+"/permissions", q), nil, &res, nil); err != nil {
		if isStatus(err, http.StatusForbidden) || isStatus(err, http.StatusNotFound) {
			// Only people who can share see the list; say who owns it.
			out.Owner = it.CreatedBy
			return out, nil
		}
		return out, err
	}
	out.Available = true
	role := func(r string) string {
		switch r {
		case "owner":
			return "owner"
		case "organizer", "fileOrganizer", "writer":
			return "edit"
		case "commenter":
			return "comment"
		}
		return "view"
	}
	for _, pm := range res.Permissions {
		switch pm.Type {
		case "anyone":
			out.Link, out.LinkRole = "anyone", role(pm.Role)
		case "domain":
			out.Link, out.LinkRole, out.Domain = "domain", role(pm.Role), pm.Domain
		default:
			name := pm.DisplayName
			if name == "" {
				name = pm.EmailAddress
			}
			if pm.Role == "owner" {
				out.Owner = name
			}
			out.People = append(out.People, ConnectedSharePe{Name: name, Role: role(pm.Role), Kind: pm.Type})
		}
	}
	return out, nil
}

func (g *googleDrive) createFolder(ctx context.Context, parent, name string) (extItem, error) {
	if parent == googleSharedWith {
		return extItem{}, httpx.BadRequest("Choose a folder in My Drive or a shared drive.")
	}
	var f gFile
	body := map[string]any{"name": name, "mimeType": googleFolder, "parents": []string{parent}}
	if _, err := g.p.json(ctx, http.MethodPost, g.url("/files", url.Values{"fields": {googleFields}}), body, &f, nil); err != nil {
		return extItem{}, err
	}
	return f.item(), nil
}

func (g *googleDrive) rename(ctx context.Context, it extItem, name string) (extItem, error) {
	var f gFile
	if _, err := g.p.json(ctx, http.MethodPatch, g.url("/files/"+url.PathEscape(it.Ref), url.Values{"fields": {googleFields}}), map[string]any{"name": name}, &f, nil); err != nil {
		return extItem{}, err
	}
	return f.item(), nil
}

func (g *googleDrive) move(ctx context.Context, it extItem, dest extItem) (extItem, error) {
	q := url.Values{"fields": {googleFields}, "addParents": {dest.Ref}}
	if it.ParentRef != "" {
		q.Set("removeParents", it.ParentRef)
	}
	var f gFile
	if _, err := g.p.json(ctx, http.MethodPatch, g.url("/files/"+url.PathEscape(it.Ref), q), map[string]any{}, &f, nil); err != nil {
		return extItem{}, err
	}
	return f.item(), nil
}

func (g *googleDrive) trash(ctx context.Context, it extItem) error {
	_, err := g.p.json(ctx, http.MethodPatch, g.url("/files/"+url.PathEscape(it.Ref), url.Values{"fields": {"id"}}), map[string]any{"trashed": true}, nil, nil)
	return err
}

func (g *googleDrive) rootRef(context.Context) (string, error) { return googleRoot, nil }

// session opens a resumable upload session: POST for a new file, PATCH for a
// new version. The session URL carries its own authorisation, so browsers
// send bytes to it directly (CORS for origin).
func (g *googleDrive) session(ctx context.Context, parent string, replace *extItem, name, mime string, size int64, origin string) (string, error) {
	q := url.Values{"uploadType": {"resumable"}, "supportsAllDrives": {"true"}, "fields": {googleFields}}
	method, u := http.MethodPost, g.upload+"/files?"
	meta := map[string]any{"name": name}
	if replace != nil {
		method, u = http.MethodPatch, g.upload+"/files/"+url.PathEscape(replace.Ref)+"?"
		meta = map[string]any{}
	} else {
		if parent == googleSharedWith {
			return "", httpx.BadRequest("Choose a folder in My Drive or a shared drive.")
		}
		meta["parents"] = []string{parent}
		if mime != "" {
			meta["mimeType"] = mime
		}
	}
	headers := map[string]string{"X-Upload-Content-Length": strconv.FormatInt(size, 10)}
	if mime != "" {
		headers["X-Upload-Content-Type"] = mime
	}
	if origin != "" {
		headers["Origin"] = origin
	}
	resp, err := g.p.json(ctx, method, u+q.Encode(), meta, nil, headers)
	if err != nil {
		return "", err
	}
	loc := resp.Header.Get("Location")
	if loc == "" {
		return "", httpx.NewError(http.StatusBadGateway, "connection_unavailable", "Google Drive didn't start the upload. Try again.")
	}
	return loc, nil
}

func (g *googleDrive) startUpload(ctx context.Context, parent string, replace *extItem, name, mime string, size int64, origin string) (extUpload, error) {
	loc, err := g.session(ctx, parent, replace, name, mime, size, origin)
	if err != nil {
		return extUpload{}, err
	}
	// Chunks are multiples of 256 KiB; a session lasts a week.
	return extUpload{URL: loc, ChunkSize: 32 * 256 << 10, Headers: map[string]string{}, ExpiresAt: time.Now().Add(7 * 24 * time.Hour)}, nil
}

func (g *googleDrive) write(ctx context.Context, parent string, replace *extItem, name, mime string, size int64, body readerAt) (extItem, error) {
	if replace == nil {
		// Drive allows two files with one name; Files doesn't make one.
		q := url.Values{"pageSize": {"1"}, "fields": {"files(id)"}, "includeItemsFromAllDrives": {"true"}, "corpora": {"allDrives"},
			"q": {"'" + driveQuote(parent) + "' in parents and name = '" + driveQuote(name) + "' and trashed = false"}}
		if page, err := g.files(ctx, q); err != nil {
			return extItem{}, err
		} else if len(page.Items) > 0 {
			return extItem{}, httpx.NewError(http.StatusConflict, "name_taken", "Something in this Google Drive folder already has that name. Choose another name.")
		}
	} else if replace.Version != "" {
		// Drive has no preconditions: compare versions just before writing.
		cur, err := g.get(ctx, replace.Ref)
		if err != nil {
			return extItem{}, err
		}
		if cur.Version != replace.Version {
			return extItem{}, httpx.NewError(http.StatusConflict, "target_changed", "It changed in Google Drive since you opened it. Refresh and try again.")
		}
	}
	loc, err := g.session(ctx, parent, replace, name, mime, size, "")
	if err != nil {
		return extItem{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, loc, io.NopCloser(body))
	if err != nil {
		return extItem{}, err
	}
	req.ContentLength = size
	if size == 0 {
		req.Body = http.NoBody
	}
	if mime != "" {
		req.Header.Set("Content-Type", mime)
	}
	resp, err := g.p.send(req)
	if err != nil {
		return extItem{}, err
	}
	defer resp.Body.Close()
	var f gFile
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err := jsonUnmarshal(b, &f); err != nil || f.ID == "" {
		return extItem{}, fmt.Errorf("files: drive upload answered %d without a file", resp.StatusCode)
	}
	if f.Name == "" || f.Capabilities == nil {
		// The session answered with the default fields: read the rest.
		return g.get(ctx, f.ID)
	}
	return f.item(), nil
}
