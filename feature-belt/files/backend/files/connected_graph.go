package files

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"scaffoldapp/backend/internal/httpx"
)

// graphDrive is the Microsoft Graph adapter: the person's OneDrive and the
// document libraries of SharePoint sites they follow. Refs are
// "<drive id>~<item id>" ("<drive id>~root" for a library's top), since
// Graph addresses items within a drive.
type graphDrive struct {
	p          platform
	base       string // …/v1.0
	oneDrive   bool   // files.microsoft.onedrive granted
	sharePoint bool   // files.microsoft.sharepoint granted
	mu         sync.Mutex
	driveNames map[string]string
}

func newGraphDrive(info *connInfo, client *http.Client, wrap func(error) error) *graphDrive {
	base := info.Endpoints["graph"]
	if base == "" {
		base = info.Endpoints["api"]
	}
	if base == "" {
		base = "https://graph.microsoft.com/v1.0"
	}
	g := &graphDrive{p: platform{client: client, name: "OneDrive", wrap: wrap}, base: strings.TrimRight(base, "/"), driveNames: map[string]string{}}
	for _, c := range info.read {
		g.oneDrive = g.oneDrive || c == CapOneDrive
		g.sharePoint = g.sharePoint || c == CapSharePoint
	}
	if !g.oneDrive {
		g.p.name = "SharePoint"
	}
	return g
}

func (g *graphDrive) provider() string { return providerMicrosoft }

func (g *graphDrive) product(ref string) string {
	d, _ := splitGraphRef(ref)
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.driveNames["kind:"+d] == "library" || !g.oneDrive {
		return productSharePoint
	}
	return productOneDrive
}

func (g *graphDrive) externalID(ref string) string { return strings.Replace(ref, "~", "/", 1) }

func splitGraphRef(ref string) (drive, item string) {
	drive, item, ok := strings.Cut(ref, "~")
	if !ok || drive == "" || item == "" {
		return "", ""
	}
	return drive, item
}

func (g *graphDrive) itemPath(ref string) (string, error) {
	d, i := splitGraphRef(ref)
	if d == "" {
		return "", httpx.NotFound("Item not found.")
	}
	if i == "root" {
		return "/drives/" + url.PathEscape(d) + "/root", nil
	}
	return "/drives/" + url.PathEscape(d) + "/items/" + url.PathEscape(i), nil
}

type gUser struct {
	User *struct {
		DisplayName string `json:"displayName"`
	} `json:"user"`
	Group *struct {
		DisplayName string `json:"displayName"`
	} `json:"group"`
}

func (u *gUser) name() string {
	switch {
	case u == nil:
		return ""
	case u.User != nil:
		return u.User.DisplayName
	case u.Group != nil:
		return u.Group.DisplayName
	}
	return ""
}

type graphItem struct {
	ID                   string `json:"id"`
	Name                 string `json:"name"`
	Size                 int64  `json:"size"`
	WebURL               string `json:"webUrl"`
	ETag                 string `json:"eTag"`
	CreatedDateTime      string `json:"createdDateTime"`
	LastModifiedDateTime string `json:"lastModifiedDateTime"`
	File                 *struct {
		MimeType string `json:"mimeType"`
	} `json:"file"`
	Folder  *struct{} `json:"folder"`
	Package *struct {
		Type string `json:"type"`
	} `json:"package"`
	Root            *struct{} `json:"root"`
	ParentReference *struct {
		DriveID   string `json:"driveId"`
		DriveType string `json:"driveType"`
		ID        string `json:"id"`
		Path      string `json:"path"`
	} `json:"parentReference"`
	LastModifiedBy *gUser `json:"lastModifiedBy"`
	CreatedBy      *gUser `json:"createdBy"`
	Shared         *struct {
		Scope string `json:"scope"`
	} `json:"shared"`
	DownloadURL string `json:"@microsoft.graph.downloadUrl"`
	Thumbnails  []struct {
		Medium *struct {
			URL string `json:"url"`
		} `json:"medium"`
	} `json:"thumbnails"`
	RemoteItem *graphItem `json:"remoteItem"`
}

func (gi graphItem) item() extItem {
	if gi.RemoteItem != nil && gi.RemoteItem.ID != "" {
		r := *gi.RemoteItem
		if r.Name == "" {
			r.Name = gi.Name
		}
		return r.item()
	}
	it := extItem{Name: gi.Name, Size: gi.Size, WebURL: gi.WebURL, Version: gi.ETag, Folder: gi.Folder != nil, DownloadURL: gi.DownloadURL,
		Shared: gi.Shared != nil, CanEdit: true, ModifiedBy: gi.LastModifiedBy.name(), CreatedBy: gi.CreatedBy.name()}
	drive := ""
	if gi.ParentReference != nil {
		drive = gi.ParentReference.DriveID
	}
	it.DriveRef = drive + "~root"
	if gi.Root != nil {
		it.Ref, it.Root, it.Folder = drive+"~root", true, true
	} else {
		it.Ref = drive + "~" + gi.ID
		if pr := gi.ParentReference; pr != nil && pr.ID != "" {
			if strings.HasSuffix(pr.Path, "/root:") || pr.Path == "/drive/root:" {
				it.ParentRef = drive + "~root"
			} else {
				it.ParentRef = drive + "~" + pr.ID
			}
		}
	}
	if gi.File != nil {
		it.MimeType = gi.File.MimeType
	}
	if gi.Package != nil {
		// OneNote notebooks and similar open only on the platform.
		it.Folder, it.Native = false, "OneNote notebook"
		if gi.Package.Type != "oneNote" {
			it.Native = "Package"
		}
	}
	if it.Folder {
		it.MimeType = ""
	}
	it.Created, _ = time.Parse(time.RFC3339, gi.CreatedDateTime)
	it.Modified, _ = time.Parse(time.RFC3339, gi.LastModifiedDateTime)
	if len(gi.Thumbnails) > 0 && gi.Thumbnails[0].Medium != nil {
		it.ThumbURL = gi.Thumbnails[0].Medium.URL
	}
	return it
}

// item converts and remembers which drives are SharePoint libraries (the
// product rows are badged with).
func (g *graphDrive) item(gi graphItem) extItem {
	src := gi
	if gi.RemoteItem != nil && gi.RemoteItem.ID != "" {
		src = *gi.RemoteItem
	}
	if pr := src.ParentReference; pr != nil && pr.DriveID != "" && pr.DriveType == "documentLibrary" {
		g.mu.Lock()
		g.driveNames["kind:"+pr.DriveID] = "library"
		g.mu.Unlock()
	}
	return gi.item()
}

type graphList struct {
	Value    []graphItem `json:"value"`
	NextLink string      `json:"@odata.nextLink"`
}

func (g *graphDrive) driveName(ctx context.Context, drive string) string {
	g.mu.Lock()
	n, ok := g.driveNames[drive]
	g.mu.Unlock()
	if ok {
		return n
	}
	var d struct {
		Name      string `json:"name"`
		DriveType string `json:"driveType"`
	}
	name := "OneDrive"
	if _, err := g.p.json(ctx, http.MethodGet, g.base+"/drives/"+url.PathEscape(drive)+"?$select=name,driveType", nil, &d, nil); err == nil {
		if d.DriveType == "documentLibrary" {
			name = d.Name
			g.mu.Lock()
			g.driveNames["kind:"+drive] = "library"
			g.mu.Unlock()
		}
	}
	g.mu.Lock()
	g.driveNames[drive] = name
	g.mu.Unlock()
	return name
}

func (g *graphDrive) drives(ctx context.Context, _ string) ([]ConnectedDrive, string, error) {
	var out []ConnectedDrive
	if g.oneDrive {
		var d struct {
			ID string `json:"id"`
		}
		if _, err := g.p.json(ctx, http.MethodGet, g.base+"/me/drive?$select=id,name,driveType", nil, &d, nil); err != nil {
			return nil, "", err
		}
		out = append(out, ConnectedDrive{Ref: d.ID + "~root", Name: "OneDrive", Kind: "onedrive"})
	}
	if !g.sharePoint {
		return out, "", nil
	}
	type site struct {
		ID          string `json:"id"`
		DisplayName string `json:"displayName"`
	}
	var sites []site
	next := g.base + "/me/followedSites?$select=id,displayName,webUrl"
	for page := 0; next != "" && page < 3 && len(sites) < 50; page++ {
		var res struct {
			Value    []site `json:"value"`
			NextLink string `json:"@odata.nextLink"`
		}
		if _, err := g.p.json(ctx, http.MethodGet, next, nil, &res, nil); err != nil {
			return nil, "", err
		}
		sites = append(sites, res.Value...)
		next = g.sameHost(res.NextLink)
	}
	if len(sites) > 50 {
		sites = sites[:50]
	}
	libs := make([][]ConnectedDrive, len(sites))
	errs := make([]error, len(sites))
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for i, st := range sites {
		wg.Add(1)
		go func(i int, st site) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			var res struct {
				Value []struct {
					ID        string `json:"id"`
					Name      string `json:"name"`
					DriveType string `json:"driveType"`
				} `json:"value"`
			}
			if _, err := g.p.json(ctx, http.MethodGet, g.base+"/sites/"+url.PathEscape(st.ID)+"/drives?$select=id,name,driveType,webUrl", nil, &res, nil); err != nil {
				errs[i] = err
				return
			}
			for _, d := range res.Value {
				if d.DriveType != "" && d.DriveType != "documentLibrary" {
					continue
				}
				libs[i] = append(libs[i], ConnectedDrive{Ref: d.ID + "~root", Name: d.Name, Kind: "library", Site: st.DisplayName})
				g.mu.Lock()
				g.driveNames[d.ID], g.driveNames["kind:"+d.ID] = d.Name, "library"
				g.mu.Unlock()
			}
		}(i, st)
	}
	wg.Wait()
	for i := range sites {
		if errs[i] != nil && !isStatus(errs[i], http.StatusForbidden) && !isStatus(errs[i], http.StatusNotFound) {
			return nil, "", errs[i]
		}
		out = append(out, libs[i]...)
	}
	return out, "", nil
}

// sameHost keeps an @odata.nextLink only when it points at this API, so a
// client-supplied cursor can't send the person's token anywhere else.
func (g *graphDrive) sameHost(link string) string {
	if link == "" || !strings.HasPrefix(link, g.base+"/") {
		return ""
	}
	return link
}

func (g *graphDrive) get(ctx context.Context, ref string) (extItem, error) {
	p, err := g.itemPath(ref)
	if err != nil {
		return extItem{}, err
	}
	var gi graphItem
	if _, err := g.p.json(ctx, http.MethodGet, g.base+p, nil, &gi, nil); err != nil {
		return extItem{}, err
	}
	it := g.item(gi)
	if it.Root {
		d, _ := splitGraphRef(ref)
		if it.Ref == "~root" {
			it.Ref, it.DriveRef = d+"~root", d+"~root"
		}
		it.Name = g.driveName(ctx, d)
	}
	return it, nil
}

func (g *graphDrive) list(ctx context.Context, folder, sortBy string, desc, thumbs bool, cursor string) (extPage, error) {
	u := g.sameHost(cursor)
	if u == "" {
		if cursor != "" {
			return extPage{}, httpx.BadRequest("Invalid cursor.")
		}
		p, err := g.itemPath(folder)
		if err != nil {
			return extPage{}, err
		}
		q := url.Values{"$top": {strconv.Itoa(connectedPageSize)}}
		if thumbs {
			q.Set("$expand", "thumbnails")
		}
		order := "name"
		switch sortBy {
		case "updated":
			order = "lastModifiedDateTime"
		case "size":
			order = "size"
		}
		if desc {
			order += " desc"
		}
		q.Set("$orderby", order)
		u = g.base + p + "/children?" + q.Encode()
	}
	var res graphList
	if _, err := g.p.json(ctx, http.MethodGet, u, nil, &res, nil); err != nil {
		if !isStatus(err, http.StatusBadRequest) || cursor != "" {
			return extPage{}, err
		}
		// Some libraries can't sort children: list them in Graph's order.
		pu, _ := url.Parse(u)
		q := pu.Query()
		q.Del("$orderby")
		pu.RawQuery = q.Encode()
		if _, err := g.p.json(ctx, http.MethodGet, pu.String(), nil, &res, nil); err != nil {
			return extPage{}, err
		}
	}
	out := extPage{Next: g.sameHost(res.NextLink)}
	for _, gi := range res.Value {
		out.Items = append(out.Items, g.item(gi))
	}
	// Folders first within the page, as the app lists them.
	sort.SliceStable(out.Items, func(i, j int) bool { return out.Items[i].Folder && !out.Items[j].Folder })
	return out, nil
}

func (g *graphDrive) path(ctx context.Context, it extItem) ([]extCrumb, error) {
	if it.Root {
		return nil, nil
	}
	var up []extCrumb
	parent := it.ParentRef
	for depth := 0; parent != "" && depth < 16; depth++ {
		p, err := g.get(ctx, parent)
		if err != nil {
			if isStatus(err, http.StatusNotFound) || isStatus(err, http.StatusForbidden) {
				break
			}
			return nil, err
		}
		up = append(up, extCrumb{Ref: p.Ref, Name: p.Name})
		if p.Root {
			break
		}
		parent = p.ParentRef
	}
	for i, j := 0, len(up)-1; i < j; i, j = i+1, j-1 {
		up[i], up[j] = up[j], up[i]
	}
	return up, nil
}

func (g *graphDrive) search(ctx context.Context, query, cursor string, limit int) (extPage, error) {
	if g.sharePoint {
		from, _ := strconv.Atoi(cursor)
		body := map[string]any{"requests": []map[string]any{{
			"entityTypes": []string{"driveItem"}, "query": map[string]any{"queryString": query}, "from": from, "size": limit,
		}}}
		var res struct {
			Value []struct {
				HitsContainers []struct {
					Hits []struct {
						Resource graphItem `json:"resource"`
					} `json:"hits"`
					MoreResultsAvailable bool `json:"moreResultsAvailable"`
				} `json:"hitsContainers"`
			} `json:"value"`
		}
		if _, err := g.p.json(ctx, http.MethodPost, g.base+"/search/query", body, &res, nil); err != nil {
			return extPage{}, err
		}
		var out extPage
		more := false
		for _, v := range res.Value {
			for _, hc := range v.HitsContainers {
				for _, h := range hc.Hits {
					if h.Resource.ID == "" {
						continue
					}
					out.Items = append(out.Items, g.item(h.Resource))
				}
				more = more || hc.MoreResultsAvailable
			}
		}
		if more {
			out.Next = strconv.Itoa(from + limit)
		}
		return out, nil
	}
	u := g.sameHost(cursor)
	if u == "" {
		escaped := strings.ReplaceAll(query, "'", "''")
		u = g.base + "/me/drive/root/search(q='" + url.PathEscape(escaped) + "')?$top=" + strconv.Itoa(limit)
	}
	var res graphList
	if _, err := g.p.json(ctx, http.MethodGet, u, nil, &res, nil); err != nil {
		return extPage{}, err
	}
	out := extPage{Next: g.sameHost(res.NextLink)}
	for _, gi := range res.Value {
		out.Items = append(out.Items, g.item(gi))
	}
	return out, nil
}

// open follows Graph's redirect to a short-lived pre-authenticated link
// (never sending the person's token there).
func (g *graphDrive) open(ctx context.Context, it extItem, format, rangeHeader string) (*http.Response, error) {
	if it.Native != "" {
		return nil, httpx.NewError(http.StatusUnsupportedMediaType, "not_exportable", it.Native+" files open only in Microsoft 365.")
	}
	p, err := g.itemPath(it.Ref)
	if err != nil {
		return nil, err
	}
	u := g.base + p + "/content"
	if format == "pdf" {
		u += "?format=pdf"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	c := *g.p.client
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	pc := g.p
	pc.client = &c
	resp, err := pc.send(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 300 {
		return resp, nil
	}
	resp.Body.Close()
	loc := resp.Header.Get("Location")
	if !g.allowedDownload(loc) {
		return nil, httpx.NewError(http.StatusBadGateway, "connection_unavailable", g.p.name+" didn't send the file. Try again.")
	}
	dl, err := http.NewRequestWithContext(ctx, http.MethodGet, loc, nil)
	if err != nil {
		return nil, err
	}
	if rangeHeader != "" {
		dl.Header.Set("Range", rangeHeader)
	}
	plain := platform{client: downloadClient, name: g.p.name, wrap: g.p.wrap}
	return plain.send(dl)
}

// downloadClient fetches Graph's pre-authenticated links and sends upload
// chunks: those URLs carry their own authorisation and must not get the
// person's token.
var downloadClient = &http.Client{Timeout: 0, CheckRedirect: func(req *http.Request, via []*http.Request) error {
	if len(via) >= 5 {
		return errors.New("too many redirects")
	}
	return nil
}}

// allowedDownload accepts https links (and http only when the API itself
// is plain http: a local stand-in in development).
func (g *graphDrive) allowedDownload(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	return u.Scheme == "https" || (u.Scheme == "http" && strings.HasPrefix(g.base, "http://"))
}

func (g *graphDrive) sharing(ctx context.Context, it extItem) (ConnectedSharing, error) {
	out := ConnectedSharing{People: []ConnectedSharePe{}, ManageURL: it.WebURL}
	if it.Root {
		return out, nil
	}
	p, err := g.itemPath(it.Ref)
	if err != nil {
		return out, err
	}
	var res struct {
		Value []struct {
			Roles []string `json:"roles"`
			Link  *struct {
				Scope string `json:"scope"`
				Type  string `json:"type"`
			} `json:"link"`
			GrantedToV2           *gUser  `json:"grantedToV2"`
			GrantedToIdentitiesV2 []gUser `json:"grantedToIdentitiesV2"`
		} `json:"value"`
	}
	if _, err := g.p.json(ctx, http.MethodGet, g.base+p+"/permissions", nil, &res, nil); err != nil {
		if isStatus(err, http.StatusForbidden) || isStatus(err, http.StatusNotFound) {
			return out, nil
		}
		return out, err
	}
	out.Available = true
	role := func(roles []string) string {
		for _, r := range roles {
			switch r {
			case "owner":
				return "owner"
			case "write":
				return "edit"
			}
		}
		return "view"
	}
	for _, pm := range res.Value {
		r := role(pm.Roles)
		if pm.Link != nil {
			lr := "view"
			if pm.Link.Type == "edit" {
				lr = "edit"
			}
			switch pm.Link.Scope {
			case "anonymous":
				out.Link, out.LinkRole = "anyone", lr
			case "organization":
				if out.Link != "anyone" {
					out.Link, out.LinkRole = "organization", lr
				}
			}
		}
		people := append([]gUser{}, pm.GrantedToIdentitiesV2...)
		if pm.GrantedToV2 != nil {
			people = append(people, *pm.GrantedToV2)
		}
		for _, u := range people {
			if n := u.name(); n != "" {
				kind := "user"
				if u.Group != nil {
					kind = "group"
				}
				if r == "owner" {
					out.Owner = n
				}
				out.People = append(out.People, ConnectedSharePe{Name: n, Role: r, Kind: kind})
			}
		}
	}
	return out, nil
}

func (g *graphDrive) createFolder(ctx context.Context, parent, name string) (extItem, error) {
	p, err := g.itemPath(parent)
	if err != nil {
		return extItem{}, err
	}
	var gi graphItem
	body := map[string]any{"name": name, "folder": map[string]any{}, "@microsoft.graph.conflictBehavior": "fail"}
	if _, err := g.p.json(ctx, http.MethodPost, g.base+p+"/children", body, &gi, nil); err != nil {
		return extItem{}, err
	}
	return g.item(gi), nil
}

func (g *graphDrive) rename(ctx context.Context, it extItem, name string) (extItem, error) {
	p, err := g.itemPath(it.Ref)
	if err != nil {
		return extItem{}, err
	}
	var gi graphItem
	if _, err := g.p.json(ctx, http.MethodPatch, g.base+p, map[string]any{"name": name}, &gi, nil); err != nil {
		return extItem{}, err
	}
	return g.item(gi), nil
}

func (g *graphDrive) move(ctx context.Context, it extItem, dest extItem) (extItem, error) {
	p, err := g.itemPath(it.Ref)
	if err != nil {
		return extItem{}, err
	}
	_, destID := splitGraphRef(dest.Ref)
	if destID == "root" {
		var root struct {
			ID string `json:"id"`
		}
		rp, _ := g.itemPath(dest.Ref)
		if _, err := g.p.json(ctx, http.MethodGet, g.base+rp+"?$select=id", nil, &root, nil); err != nil {
			return extItem{}, err
		}
		destID = root.ID
	}
	var gi graphItem
	if _, err := g.p.json(ctx, http.MethodPatch, g.base+p, map[string]any{"parentReference": map[string]any{"id": destID}}, &gi, nil); err != nil {
		return extItem{}, err
	}
	return g.item(gi), nil
}

func (g *graphDrive) trash(ctx context.Context, it extItem) error {
	p, err := g.itemPath(it.Ref)
	if err != nil {
		return err
	}
	_, err = g.p.json(ctx, http.MethodDelete, g.base+p, nil, nil, nil)
	return err
}

func (g *graphDrive) rootRef(ctx context.Context) (string, error) {
	if !g.oneDrive {
		return "", httpx.BadRequest("Choose a SharePoint folder to save into.")
	}
	var d struct {
		ID string `json:"id"`
	}
	if _, err := g.p.json(ctx, http.MethodGet, g.base+"/me/drive?$select=id", nil, &d, nil); err != nil {
		return "", err
	}
	return d.ID + "~root", nil
}

// childPath addresses a new child by name: …/items/{id}:/{name}:.
func (g *graphDrive) childPath(parent, name string) (string, error) {
	p, err := g.itemPath(parent)
	if err != nil {
		return "", err
	}
	return p + ":/" + url.PathEscape(name) + ":", nil
}

func (g *graphDrive) uploadSession(ctx context.Context, parent string, replace *extItem, name, conflict string) (string, string, error) {
	var p string
	var err error
	if replace != nil {
		p, err = g.itemPath(replace.Ref)
		conflict = "replace"
	} else {
		p, err = g.childPath(parent, name)
	}
	if err != nil {
		return "", "", err
	}
	var res struct {
		UploadURL string `json:"uploadUrl"`
	}
	body := map[string]any{"item": map[string]any{"@microsoft.graph.conflictBehavior": conflict}}
	if _, err := g.p.json(ctx, http.MethodPost, g.base+p+"/createUploadSession", body, &res, nil); err != nil {
		return "", "", err
	}
	if !g.allowedDownload(res.UploadURL) {
		return "", "", httpx.NewError(http.StatusBadGateway, "connection_unavailable", g.p.name+" didn't start the upload. Try again.")
	}
	d := parent
	if replace != nil {
		d = replace.Ref
	}
	drive, _ := splitGraphRef(d)
	return res.UploadURL, drive, nil
}

func (g *graphDrive) startUpload(ctx context.Context, parent string, replace *extItem, name, _ string, _ int64, _ string) (extUpload, error) {
	u, drive, err := g.uploadSession(ctx, parent, replace, name, "rename")
	if err != nil {
		return extUpload{}, err
	}
	// Chunks are multiples of 320 KiB (Graph's rule); 10 MiB each.
	return extUpload{URL: u, ChunkSize: 32 * 320 << 10, Headers: map[string]string{}, RefPrefix: drive + "~", ExpiresAt: time.Now().Add(24 * time.Hour)}, nil
}

func (g *graphDrive) write(ctx context.Context, parent string, replace *extItem, name, mime string, size int64, body readerAt) (extItem, error) {
	if size <= maxConnectedSmallUpload {
		var p string
		var err error
		headers := map[string]string{}
		if replace != nil {
			p, err = g.itemPath(replace.Ref)
			if replace.Version != "" {
				headers["If-Match"] = replace.Version
			}
		} else {
			p, err = g.childPath(parent, name)
		}
		if err != nil {
			return extItem{}, err
		}
		u := g.base + p + "/content"
		if replace == nil {
			u += "?@microsoft.graph.conflictBehavior=fail"
		}
		b, err := io.ReadAll(io.LimitReader(body, maxConnectedSmallUpload+1))
		if err != nil {
			return extItem{}, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, u, bytes.NewReader(b))
		if err != nil {
			return extItem{}, err
		}
		if mime == "" {
			mime = "application/octet-stream"
		}
		req.Header.Set("Content-Type", mime)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := g.p.send(req)
		if err != nil {
			return extItem{}, err
		}
		defer resp.Body.Close()
		var gi graphItem
		if err := jsonDecode(resp.Body, &gi); err != nil {
			return extItem{}, err
		}
		return g.item(gi), nil
	}
	u, _, err := g.uploadSession(ctx, parent, replace, name, "fail")
	if err != nil {
		return extItem{}, err
	}
	chunk := int64(32 * 320 << 10)
	buf := make([]byte, chunk)
	plain := platform{client: downloadClient, name: g.p.name, wrap: g.p.wrap}
	for off := int64(0); off < size; {
		n, err := io.ReadFull(body, buf[:min(chunk, size-off)])
		if err != nil {
			return extItem{}, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, u, bytes.NewReader(buf[:n]))
		if err != nil {
			return extItem{}, err
		}
		req.ContentLength = int64(n)
		req.Header.Set("Content-Range", "bytes "+strconv.FormatInt(off, 10)+"-"+strconv.FormatInt(off+int64(n)-1, 10)+"/"+strconv.FormatInt(size, 10))
		resp, err := plain.send(req)
		if err != nil {
			return extItem{}, err
		}
		off += int64(n)
		if off >= size {
			defer resp.Body.Close()
			var gi graphItem
			if err := jsonDecode(resp.Body, &gi); err != nil {
				return extItem{}, err
			}
			return g.item(gi), nil
		}
		resp.Body.Close()
	}
	return extItem{}, httpx.BadRequest("That file is empty.")
}
