package files

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
)

// mountConnected adds the connected-drive routes (called from MountUser).
func (s *Service) mountConnected(r chi.Router) {
	h := httpx.Handler
	w := r.With(auth.RequireActive)
	read := r.With(auth.RateLimit(600, time.Minute))
	read.Get("/files/sources", h(s.sourcesRoute))
	read.Get("/files/connected/{connID}/drives", h(s.connDrivesRoute))
	read.Get("/files/connected/{connID}/search", h(s.connSearchRoute))
	read.Get("/files/connected/{connID}/items/{ref}", h(s.connItemRoute))
	read.Get("/files/connected/{connID}/items/{ref}/children", h(s.connChildrenRoute))
	read.Get("/files/connected/{connID}/items/{ref}/url", h(s.connURLRoute))
	read.Get("/files/connected/{connID}/items/{ref}/text", h(s.connTextRoute))
	read.Get("/files/connected/{connID}/items/{ref}/sharing", h(s.connSharingRoute))
	w.With(auth.RateLimit(600, time.Hour)).Post("/files/connected/{connID}/folders", h(s.connCreateFolderRoute))
	w.With(auth.RateLimit(600, time.Hour)).Patch("/files/connected/{connID}/items/{ref}", h(s.connRenameRoute))
	w.With(auth.RateLimit(600, time.Hour)).Post("/files/connected/{connID}/items/{ref}/move", h(s.connMoveRoute))
	w.With(auth.RateLimit(600, time.Hour)).Post("/files/connected/{connID}/items/{ref}/trash", h(s.connTrashRoute))
	w.With(auth.RateLimit(2000, time.Hour)).Post("/files/connected/{connID}/uploads", h(s.connUploadRoute))
	w.With(auth.RateLimit(300, time.Hour)).Post("/files/connected/{connID}/items/{ref}/copy-to-files", h(s.connCopyToFilesRoute))
	w.With(orgs.Require(orgs.RoleMember), auth.RateLimit(600, time.Hour)).Post("/files/orgs/{orgID}/shortcuts", h(s.createShortcutRoute))
	r.With(auth.RateLimit(300, time.Minute)).Get("/files/items/{id}/shortcut", h(s.resolveShortcutRoute))
	w.With(auth.RateLimit(300, time.Hour)).Post("/files/items/{id}/save-to", h(s.saveToConnectedRoute))
}

// connRequest is what every connected route reads first.
func (s *Service) connRequest(r *http.Request, write bool) (*connSession, string, error) {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	org, err := s.orgOf(ctx, r, me)
	if err != nil {
		return nil, "", err
	}
	ref, err := url.PathUnescape(chi.URLParam(r, "ref"))
	if err != nil {
		return nil, "", httpx.NotFound("Item not found.")
	}
	cs, err := s.session(ctx, me, org, chi.URLParam(r, "connID"), write)
	if err != nil {
		return nil, "", err
	}
	return cs, ref, nil
}

// sourcesRoute: GET /v1/files/sources?org_id= → FeatureSources.
func (s *Service) sourcesRoute(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	org, err := s.orgOf(ctx, r, me)
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

// connDrivesRoute: GET …/connected/{connID}/drives?cursor= → the places to
// browse (My Drive, Shared with me, shared drives; OneDrive, libraries).
func (s *Service) connDrivesRoute(w http.ResponseWriter, r *http.Request) error {
	cs, _, err := s.connRequest(r, false)
	if err != nil {
		return err
	}
	list, next, err := cs.ad.drives(r.Context(), r.URL.Query().Get("cursor"))
	if err != nil {
		return err
	}
	for i := range list {
		list[i].Link = ConnectedLink(cs.info.ID, list[i].Ref)
	}
	out := struct {
		Items      []ConnectedDrive `json:"items"`
		NextCursor *string          `json:"next_cursor"`
		Source     ConnectedSource  `json:"source"`
	}{Items: orEmpty(list), Source: cs.sourceView()}
	if next != "" {
		out.NextCursor = &next
	}
	httpx.OK(w, out)
	return nil
}

// connItemRoute: GET …/items/{ref} → the item, its folders and the source.
func (s *Service) connItemRoute(w http.ResponseWriter, r *http.Request) error {
	cs, ref, err := s.connRequest(r, false)
	if err != nil {
		return err
	}
	ctx := r.Context()
	it, err := cs.getItem(ctx, ref)
	if err != nil {
		return err
	}
	crumbs, err := cs.ad.path(ctx, it)
	if err != nil {
		return err
	}
	httpx.OK(w, ConnectedDetail{Item: cs.toConnected(it), Path: cs.crumbs(crumbs), Source: cs.sourceView()})
	return nil
}

// connChildrenRoute: GET …/items/{ref}/children?sort=&dir=&thumbs=1&cursor=
// → a page in the platform's own pagination.
func (s *Service) connChildrenRoute(w http.ResponseWriter, r *http.Request) error {
	cs, ref, err := s.connRequest(r, false)
	if err != nil {
		return err
	}
	q := r.URL.Query()
	page, err := cs.ad.list(r.Context(), ref, q.Get("sort"), q.Get("dir") == "desc", q.Get("thumbs") == "1", q.Get("cursor"))
	if err != nil {
		return err
	}
	httpx.OK(w, cs.page(page))
	return nil
}

func (cs *connSession) page(p extPage) httpx.Page[*ConnectedItem] {
	out := httpx.Page[*ConnectedItem]{Items: make([]*ConnectedItem, 0, len(p.Items))}
	for _, it := range p.Items {
		cs.remember(it.Ref, it)
		out.Items = append(out.Items, cs.toConnected(it))
	}
	if p.Next != "" {
		n := p.Next
		out.NextCursor = &n
	}
	return out
}

// connSearchRoute: GET …/connected/{connID}/search?q=&cursor= → the
// platform's own search, as the person.
func (s *Service) connSearchRoute(w http.ResponseWriter, r *http.Request) error {
	cs, _, err := s.connRequest(r, false)
	if err != nil {
		return err
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		httpx.OK(w, httpx.Page[*ConnectedItem]{Items: []*ConnectedItem{}})
		return nil
	}
	page, err := cs.ad.search(r.Context(), truncate(q, 200), r.URL.Query().Get("cursor"), 30)
	if err != nil {
		return err
	}
	httpx.OK(w, cs.page(page))
	return nil
}

// connSharingRoute: GET …/items/{ref}/sharing → the platform's sharing
// summary, read-only (manage it on the platform).
func (s *Service) connSharingRoute(w http.ResponseWriter, r *http.Request) error {
	cs, ref, err := s.connRequest(r, false)
	if err != nil {
		return err
	}
	it, err := cs.getItem(r.Context(), ref)
	if err != nil {
		return err
	}
	sh, err := cs.ad.sharing(r.Context(), it)
	if err != nil {
		return err
	}
	httpx.OK(w, sh)
	return nil
}

// ConnectedText is a connected text file's content for previews.
type ConnectedText struct {
	Text      string `json:"text"`
	Truncated bool   `json:"truncated"`
	Size      int64  `json:"size"`
	VersionID string `json:"version_id"`
}

// readConnectedText reads up to limit bytes of a text file.
func (cs *connSession) readText(ctx context.Context, it extItem, limit int) ([]byte, bool, error) {
	if it.Folder {
		return nil, false, errNotFile
	}
	textLike := it.Native == "" && (isTextual(baseType(it.MimeType)) || codeTypes[extOf(it.Name)] != "")
	if !textLike {
		return nil, false, errNotText
	}
	resp, err := cs.ad.open(ctx, it, "", "")
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, int64(limit)+1))
	if err != nil {
		return nil, false, err
	}
	b, truncated := truncateUTF8(b, limit)
	if !looksText(b) {
		return nil, false, errNotText
	}
	return b, truncated, nil
}

// connTextRoute: GET …/items/{ref}/text → the first 1 MB of a text file.
func (s *Service) connTextRoute(w http.ResponseWriter, r *http.Request) error {
	cs, ref, err := s.connRequest(r, false)
	if err != nil {
		return err
	}
	it, err := cs.getItem(r.Context(), ref)
	if err != nil {
		return err
	}
	b, truncated, err := cs.readText(r.Context(), it, maxTextPreview)
	if err != nil {
		return err
	}
	httpx.OK(w, ConnectedText{Text: string(b), Truncated: truncated, Size: it.Size, VersionID: it.Version})
	return nil
}

// ---- previews and downloads: short-lived proxied links ----------------------------

// connURLRoute: GET …/items/{ref}/url?disposition=inline|attachment → a
// 15-minute link that streams the file through the API with the person's
// connection. Inline previews of native documents and Office files are PDF
// renderings; downloads keep the name and type (native documents export as
// Office files).
func (s *Service) connURLRoute(w http.ResponseWriter, r *http.Request) error {
	cs, ref, err := s.connRequest(r, false)
	if err != nil {
		return err
	}
	ctx := r.Context()
	me := cs.me
	org, _ := s.orgOf(ctx, r, me)
	it, err := cs.getItem(ctx, ref)
	if err != nil {
		return err
	}
	if it.Folder {
		return errNotFile
	}
	inline := r.URL.Query().Get("disposition") == "inline"
	ci := cs.toConnected(it)
	format, ct, filename, disp := "", "", it.Name, "attachment"
	switch {
	case inline && ci.Preview == "pdf" && baseType(it.MimeType) != "application/pdf":
		format, ct, disp = "pdf", "application/pdf", "inline"
		filename = strings.TrimSuffix(it.Name, "."+extOf(it.Name)) + ".pdf"
	case it.Native != "":
		if it.NativeExport == "" {
			return httpx.NewError(http.StatusUnsupportedMediaType, "not_exportable", it.Native+" files open only on the platform. Use Open in "+ProductLabel(ci.Source.Product)+".")
		}
		ct = it.NativeExport
		if !strings.HasSuffix(strings.ToLower(filename), "."+it.NativeExt) {
			filename += "." + it.NativeExt
		}
	default:
		var cd string
		ct, cd = servePolicy(baseType(it.MimeType), it.Name, ci.TextLike, inline)
		disp, _, _ = mime.ParseMediaType(cd)
	}
	cd := mime.FormatMediaType(disp, map[string]string{"filename": filename})
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return err
	}
	token := base64.RawURLEncoding.EncodeToString(raw[:])
	hash := sha256.Sum256([]byte(token))
	exp := time.Now().Add(connectedProxyTTL)
	var orgArg *uuid.UUID
	if org != uuid.Nil {
		orgArg = &org
	}
	if _, err := s.d.Pool.Exec(ctx, `
		INSERT INTO files_connected_downloads (token_hash, user_id, org_id, connection_id, ref, format, content_type, disposition, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`, hash[:], me, orgArg, cs.info.ID, it.Ref, format, ct, cd, exp); err != nil {
		return err
	}
	base := strings.TrimRight(s.d.Cfg.PublicAPIURL, "/")
	httpx.OK(w, FileURL{URL: base + "/files-connected/" + token, ExpiresAt: exp, Disposition: disp, ContentType: ct, Filename: filename})
	return nil
}

// serveConnected: GET /files-connected/{token} streams a connected file
// with the type and disposition chosen when the link was made, re-reading
// the item with the person's connection (their access is checked live).
func (s *Service) serveConnected(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	hash := sha256.Sum256([]byte(chi.URLParam(r, "token")))
	var (
		me                      uuid.UUID
		org                     *uuid.UUID
		connID, ref, format, ct string
		disposition             string
	)
	err := s.d.Pool.QueryRow(ctx, `
		SELECT d.user_id, d.org_id, d.connection_id, d.ref, d.format, d.content_type, d.disposition
		FROM files_connected_downloads d JOIN users u ON u.id = d.user_id
		WHERE d.token_hash = $1 AND d.expires_at > now() AND u.status IN ('active', 'suspended')`, hash[:]).
		Scan(&me, &org, &connID, &ref, &format, &ct, &disposition)
	if err != nil {
		if !db.IsNoRows(err) {
			slog.ErrorContext(ctx, "files: connected link lookup", "err", err)
		}
		httpx.Fail(w, r, httpx.NewError(http.StatusNotFound, "link_expired", "This link expired. Open the file again."))
		return
	}
	orgID := uuid.Nil
	if org != nil {
		orgID = *org
	}
	cs, err := s.session(ctx, me, orgID, connID, false)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	it, err := cs.getItem(ctx, ref)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	resp, err := cs.ad.open(ctx, it, format, r.Header.Get("Range"))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	defer resp.Body.Close()
	hd := w.Header()
	hd.Set("Content-Type", ct)
	hd.Set("Content-Disposition", disposition)
	hd.Set("X-Content-Type-Options", "nosniff")
	hd.Set("Cache-Control", "private, no-store")
	hd.Set("X-Robots-Tag", "noindex")
	// Inline previews are framed by the web app (PDF): allow its origins only.
	ancestors := "frame-ancestors 'none'"
	if strings.HasPrefix(disposition, "inline") {
		hd.Del("X-Frame-Options")
		ancestors = "frame-ancestors 'self' " + strings.Join(s.d.Cfg.WebOrigins(), " ")
	}
	if ct == "application/pdf" {
		hd.Set("Content-Security-Policy", ancestors)
	} else {
		hd.Set("Content-Security-Policy", "sandbox; default-src 'none'; img-src 'self' data:; media-src 'self'; style-src 'unsafe-inline'; "+ancestors)
	}
	for _, k := range []string{"Content-Length", "Content-Range", "Accept-Ranges"} {
		if v := resp.Header.Get(k); v != "" {
			hd.Set(k, v)
		}
	}
	status := http.StatusOK
	if resp.StatusCode == http.StatusPartialContent {
		status = http.StatusPartialContent
	}
	w.WriteHeader(status)
	if _, err := io.Copy(w, resp.Body); err != nil && ctx.Err() == nil {
		slog.WarnContext(ctx, "files: connected stream ended early", "err", err)
	}
}

// ---- changes on the platform (write capability) -----------------------------------

type connFolderReq struct {
	ParentRef string `json:"parent_ref"`
	Name      string `json:"name"`
}

// connCreateFolderRoute: POST …/connected/{connID}/folders {parent_ref, name}.
func (s *Service) connCreateFolderRoute(w http.ResponseWriter, r *http.Request) error {
	cs, _, err := s.connRequest(r, true)
	if err != nil {
		return err
	}
	var req connFolderReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	name, err := cleanName(req.Name)
	if err != nil {
		return err
	}
	parent := req.ParentRef
	if parent == "" {
		if parent, err = cs.ad.rootRef(r.Context()); err != nil {
			return err
		}
	}
	it, err := cs.ad.createFolder(r.Context(), parent, name)
	if err != nil {
		return err
	}
	cs.remember(it.Ref, it)
	httpx.JSON(w, http.StatusCreated, cs.toConnected(it))
	return nil
}

// connRenameRoute: PATCH …/items/{ref} {name}.
func (s *Service) connRenameRoute(w http.ResponseWriter, r *http.Request) error {
	cs, ref, err := s.connRequest(r, true)
	if err != nil {
		return err
	}
	var req renameReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	name, err := cleanName(req.Name)
	if err != nil {
		return err
	}
	ctx := r.Context()
	it, err := cs.ad.get(ctx, ref)
	if err != nil {
		return err
	}
	if it.Root {
		return httpx.BadRequest("Rename drives in " + ProductLabel(cs.ad.product(ref)) + ".")
	}
	out, err := cs.ad.rename(ctx, it, name)
	if err != nil {
		return err
	}
	cs.forget(ref, out.Ref)
	httpx.OK(w, cs.toConnected(out))
	return nil
}

type connMoveReq struct {
	ParentRef string `json:"parent_ref"`
}

// connMoveRoute: POST …/items/{ref}/move {parent_ref}: within the same drive.
func (s *Service) connMoveRoute(w http.ResponseWriter, r *http.Request) error {
	cs, ref, err := s.connRequest(r, true)
	if err != nil {
		return err
	}
	var req connMoveReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	out, err := cs.moveItem(r.Context(), ref, req.ParentRef)
	if err != nil {
		return err
	}
	httpx.OK(w, cs.toConnected(out))
	return nil
}

func (cs *connSession) moveItem(ctx context.Context, ref, parentRef string) (extItem, error) {
	it, err := cs.ad.get(ctx, ref)
	if err != nil {
		return extItem{}, err
	}
	if it.Root {
		return extItem{}, httpx.BadRequest("Drives can't be moved.")
	}
	dest, err := cs.ad.get(ctx, parentRef)
	if err != nil {
		return extItem{}, err
	}
	if !dest.Folder || dest.Ref == googleSharedWith {
		return extItem{}, errNotFolder
	}
	if dest.Ref == it.Ref {
		return extItem{}, httpx.BadRequest("A folder can't go inside itself.")
	}
	if dest.DriveRef != it.DriveRef {
		return extItem{}, httpx.NewError(http.StatusBadRequest, "cross_drive", "Move it within the same drive here. Moving between drives changes who owns it: do that in "+ProductLabel(cs.ad.product(ref))+".")
	}
	out, err := cs.ad.move(ctx, it, dest)
	if err != nil {
		return extItem{}, err
	}
	cs.forget(ref, it.ParentRef, dest.Ref, out.Ref)
	return out, nil
}

// connTrashRoute: POST …/items/{ref}/trash → the platform's trash (Drive)
// or recycle bin (OneDrive, SharePoint), restorable there.
func (s *Service) connTrashRoute(w http.ResponseWriter, r *http.Request) error {
	cs, ref, err := s.connRequest(r, true)
	if err != nil {
		return err
	}
	it, err := cs.ad.get(r.Context(), ref)
	if err != nil {
		return err
	}
	if it.Root {
		return httpx.BadRequest("Drives can't be trashed from here.")
	}
	if err := cs.ad.trash(r.Context(), it); err != nil {
		return err
	}
	cs.forget(ref, it.ParentRef)
	httpx.NoContent(w)
	return nil
}

type connUploadReq struct {
	ParentRef   string `json:"parent_ref"`
	ItemRef     string `json:"item_ref"` // a new version of this file instead
	Name        string `json:"name"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
}

// ConnectedUploadTicket is a resumable session on the platform: the client
// PUTs chunks (Content-Range) to UploadURL directly, then reads the item
// (ref = RefPrefix + the id in the last answer).
type ConnectedUploadTicket struct {
	extUpload
	ConnectionID string         `json:"connection_id"`
	Provider     string         `json:"provider"`
	Item         *ConnectedItem `json:"item,omitempty"` // set for empty files (no session needed)
}

// connUploadRoute: POST …/connected/{connID}/uploads.
func (s *Service) connUploadRoute(w http.ResponseWriter, r *http.Request) error {
	cs, _, err := s.connRequest(r, true)
	if err != nil {
		return err
	}
	ctx := r.Context()
	var req connUploadReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.Size < 0 || req.Size > 250<<30 {
		return httpx.BadRequest("Choose a smaller file.")
	}
	ct := baseType(req.ContentType)
	var replace *extItem
	name := req.Name
	parent := req.ParentRef
	if req.ItemRef != "" {
		it, err := cs.ad.get(ctx, req.ItemRef)
		if err != nil {
			return err
		}
		if it.Folder || it.Native != "" {
			return httpx.BadRequest("Only files get new versions. Edit " + it.Native + " files on the platform.")
		}
		replace, name = &it, it.Name
	} else {
		if name, err = cleanName(name); err != nil {
			return err
		}
		if parent == "" {
			if parent, err = cs.ad.rootRef(ctx); err != nil {
				return err
			}
		}
	}
	out := ConnectedUploadTicket{ConnectionID: cs.info.ID, Provider: cs.info.Provider}
	if req.Size == 0 {
		it, err := cs.ad.write(ctx, parent, replace, name, ct, 0, strings.NewReader(""))
		if err != nil {
			return err
		}
		out.Item = cs.toConnected(it)
		httpx.JSON(w, http.StatusCreated, out)
		return nil
	}
	up, err := cs.ad.startUpload(ctx, parent, replace, name, ct, req.Size, strings.TrimRight(s.d.Cfg.PublicWebURL, "/"))
	if err != nil {
		return err
	}
	out.extUpload = up
	if replace != nil {
		cs.forget(replace.Ref)
	}
	httpx.JSON(w, http.StatusCreated, out)
	return nil
}

// ---- bridges ----------------------------------------------------------------------

type copyToFilesReq struct {
	OrgID    uuid.UUID  `json:"org_id"`
	ParentID *uuid.UUID `json:"parent_id"` // a Files folder (null: the top of your My drive)
}

// connCopyToFilesRoute: POST …/items/{ref}/copy-to-files {org_id,
// parent_id?} → a new Files file (app content from now on), stored through
// the drive's own storage path, quota and versions.
func (s *Service) connCopyToFilesRoute(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	var req copyToFilesReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if role, err := orgs.RoleOf(ctx, s.d.Pool, req.OrgID, me); err != nil {
		return err
	} else if role == "" {
		return httpx.NotFound(orgs.NounTitle() + " not found.")
	}
	ref, err := url.PathUnescape(chi.URLParam(r, "ref"))
	if err != nil {
		return httpx.NotFound("Item not found.")
	}
	cs, err := s.session(ctx, me, req.OrgID, chi.URLParam(r, "connID"), false)
	if err != nil {
		return err
	}
	it, err := cs.ad.get(ctx, ref)
	if err != nil {
		return err
	}
	out, err := s.copyIntoFiles(ctx, cs, it, req.OrgID, req.ParentID)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, out)
	return nil
}

// copyIntoFiles streams a connected file into a Files folder.
func (s *Service) copyIntoFiles(ctx context.Context, cs *connSession, it extItem, orgID uuid.UUID, parentID *uuid.UUID) (*Item, error) {
	if it.Folder {
		return nil, httpx.BadRequest("Copy files one at a time: folders stay on the platform.")
	}
	name, declared, size := it.Name, it.MimeType, it.Size
	if it.Native != "" {
		if it.NativeExport == "" {
			return nil, httpx.NewError(http.StatusUnsupportedMediaType, "not_exportable", it.Native+" files can't be copied out of the platform.")
		}
		declared, size = it.NativeExport, -1 // exports have no size until they're made
		if !strings.HasSuffix(strings.ToLower(name), "."+it.NativeExt) {
			name += "." + it.NativeExt
		}
	}
	if size > s.cfg.maxUpload {
		return nil, httpx.NewError(http.StatusRequestEntityTooLarge, "file_too_large", "That file is over "+formatMB(s.cfg.maxUpload)+". Choose a smaller one.")
	}
	// Fail fast on the folder and the quota before moving any bytes.
	if _, err := s.placeFor(ctx, s.d.Pool, cs.me, orgID, parentID); err != nil {
		return nil, err
	}
	if s.cfg.quotaBytes > 0 && size > 0 {
		used, err := s.usedBytes(ctx, s.d.Pool, orgID)
		if err != nil {
			return nil, err
		}
		if used+size > s.cfg.quotaBytes {
			return nil, errQuota()
		}
	}
	resp, err := cs.ad.open(ctx, it, "", "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	from := ProductLabel(cs.ad.product(it.Ref))
	return s.importStream(ctx, cs.me, orgID, parentID, name, declared, size, resp.Body, from)
}

// importStream stores bytes as a new Files file: known sizes stream to a
// presigned upload (the drive's storage path), unknown ones are read whole
// (exports, capped). The object is checked like any upload.
func (s *Service) importStream(ctx context.Context, me, orgID uuid.UUID, parentID *uuid.UUID, rawName, declared string, size int64, body io.Reader, from string) (*Item, error) {
	name, err := cleanName(rawName)
	if err != nil {
		return nil, err
	}
	ct := baseType(declared)
	if ct == "" || ct == "application/octet-stream" {
		ct = "application/octet-stream"
	}
	if size < 0 {
		b, err := io.ReadAll(io.LimitReader(body, min(s.cfg.maxUpload, 50<<20)+1))
		if err != nil {
			return nil, err
		}
		if int64(len(b)) > min(s.cfg.maxUpload, 50<<20) {
			return nil, httpx.NewError(http.StatusRequestEntityTooLarge, "file_too_large", "That export is too large to copy.")
		}
		size = int64(len(b))
		body = strings.NewReader(string(b))
	}
	var key string
	if size == 0 {
		key, err = s.putSmall(ctx, orgID, ct, nil)
	} else {
		key, err = s.putStream(ctx, orgID, ct, size, body)
	}
	if err != nil {
		return nil, err
	}
	var out *Item
	err = func() error {
		var head []byte
		if size > 0 {
			info, err := s.d.Storage.Stat(ctx, key)
			if err != nil {
				return err
			}
			if info.Size != size {
				return httpx.NewError(http.StatusBadGateway, "size_mismatch", from+" sent a different size than it announced. Try again.")
			}
			if head, err = s.d.Storage.ReadPrefix(ctx, key, 8192); err != nil {
				return err
			}
		}
		mt, textLike, err := classify(ct, name, head)
		if err != nil {
			return err
		}
		v := newVersion{key: key, size: size, mime: mt, textLike: textLike, content: s.indexText(ctx, key, size, textLike)}
		return db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
			pl, err := s.placeFor(ctx, tx, me, orgID, parentID)
			if err != nil {
				return err
			}
			if err := s.reserve(ctx, tx, pl.orgID, size); err != nil {
				return err
			}
			id, err := s.insertItem(ctx, tx, pl, TypeFile, name, me, true)
			if err != nil {
				return err
			}
			if err := s.addVersion(ctx, tx, id, pl.orgID, me, v); err != nil {
				return err
			}
			s.logActivity(ctx, tx, pl.orgID, id, pl.parentID(), me, "uploaded", map[string]any{"size": size, "from": from})
			out, err = s.getItem(ctx, tx, me, id)
			return err
		})
	}()
	if err != nil {
		s.queueDeletion(ctx, key)
		return nil, err
	}
	s.touchRecent(ctx, me, out, "edited")
	s.changed(ctx, out, "created")
	return out, nil
}

func (s *Service) putSmall(ctx context.Context, orgID uuid.UUID, ct string, b []byte) (string, error) {
	key, err := newObjectKey(orgID, ct)
	if err != nil {
		return "", err
	}
	return key, s.d.Storage.Put(ctx, key, ct, b)
}

// putStream sends size bytes to a presigned upload of the files purpose.
func (s *Service) putStream(ctx context.Context, orgID uuid.UUID, ct string, size int64, body io.Reader) (string, error) {
	up, err := s.d.Storage.PresignPut(ctx, Purpose, orgID, ct, size)
	if err != nil {
		return "", httpx.BadRequest(err.Error())
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, up.UploadURL, io.LimitReader(body, size))
	if err != nil {
		return "", err
	}
	req.ContentLength = size
	for k, v := range up.Headers {
		req.Header.Set(k, v)
	}
	resp, err := storageClient.Do(req)
	if err != nil {
		s.queueDeletion(ctx, up.Key)
		return "", httpx.NewError(http.StatusBadGateway, "copy_failed", "Couldn't copy the file. Try again.")
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode >= 300 {
		s.queueDeletion(ctx, up.Key)
		return "", httpx.NewError(http.StatusBadGateway, "copy_failed", "Couldn't store the copy. Try again.")
	}
	return up.Key, nil
}

var storageClient = &http.Client{Timeout: 30 * time.Minute}

type shortcutReq struct {
	ParentID     *uuid.UUID `json:"parent_id"`
	ConnectionID string     `json:"connection_id"`
	Ref          string     `json:"ref"`
}

// createShortcutRoute: POST /v1/files/orgs/{orgID}/shortcuts {parent_id?,
// connection_id, ref} → a Files item of type link pointing at a connected
// item. Only the reference is stored; colleagues open it with their own
// connection.
func (s *Service) createShortcutRoute(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	orgID := orgParam(r)
	var req shortcutReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	cs, err := s.session(ctx, me, orgID, req.ConnectionID, false)
	if err != nil {
		return err
	}
	it, err := cs.ad.get(ctx, req.Ref)
	if err != nil {
		return err
	}
	if it.Ref == googleSharedWith {
		return httpx.BadRequest("Choose a file or folder.")
	}
	out, err := s.createShortcut(ctx, cs, it, orgID, req.ParentID)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, out)
	return nil
}

func (s *Service) createShortcut(ctx context.Context, cs *connSession, it extItem, orgID uuid.UUID, parentID *uuid.UUID) (*Item, error) {
	var out *Item
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		pl, err := s.placeFor(ctx, tx, cs.me, orgID, parentID)
		if err != nil {
			return err
		}
		id, err := s.insertItem(ctx, tx, pl, TypeLink, it.Name, cs.me, true)
		if err != nil {
			return err
		}
		mt := it.MimeType
		if it.Folder {
			mt = ""
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO files_links (item_id, provider, product, ref, mime_type, folder, web_url, kind_label)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			id, cs.info.Provider, cs.ad.product(it.Ref), it.Ref, mt, it.Folder, it.WebURL, it.Native); err != nil {
			return err
		}
		s.logActivity(ctx, tx, pl.orgID, id, pl.parentID(), cs.me, "created", map[string]any{"type": TypeLink, "provider": cs.info.Provider})
		out, err = s.getItem(ctx, tx, cs.me, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.changed(ctx, out, "created")
	return out, nil
}

// ShortcutTarget is what a shortcut opens for the caller, checked live with
// their own connection.
type ShortcutTarget struct {
	Status       string `json:"status"` // ok | not_connected | no_access | needs_reauth | blocked | unavailable
	Provider     string `json:"provider"`
	Product      string `json:"product"`
	ProductLabel string `json:"product_label"`
	Name         string `json:"name"`
	MimeType     string `json:"mime_type"`
	Folder       bool   `json:"folder"`
	KindLabel    string `json:"kind_label,omitempty"`
	ConnectionID string `json:"connection_id,omitempty"`
	Account      string `json:"account,omitempty"`
	Ref          string `json:"ref,omitempty"`
	Link         string `json:"link,omitempty"`        // app path when status is ok
	ConnectURL   string `json:"connect_url,omitempty"` // not_connected, needs_reauth
	WebURL       string `json:"web_url,omitempty"`     // no_access: ask for access on the platform
	Message      string `json:"message"`
}

type linkRow struct {
	provider, product, ref, mime, webURL, kindLabel string
	folder                                          bool
}

// resolveShortcutRoute: GET /v1/files/items/{id}/shortcut (view).
func (s *Service) resolveShortcutRoute(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := idParam(r)
	if err != nil {
		return err
	}
	it, err := s.load(ctx, s.d.Pool, me, id, access.View)
	if err != nil {
		return err
	}
	out, err := s.resolveShortcut(ctx, me, it)
	if err != nil {
		return err
	}
	s.touchRecent(ctx, me, it, "viewed")
	httpx.OK(w, out)
	return nil
}

func (s *Service) linkOf(ctx context.Context, q db.Querier, id uuid.UUID) (linkRow, error) {
	var l linkRow
	err := q.QueryRow(ctx, `SELECT provider, product, ref, mime_type, web_url, kind_label, folder FROM files_links WHERE item_id = $1`, id).
		Scan(&l.provider, &l.product, &l.ref, &l.mime, &l.webURL, &l.kindLabel, &l.folder)
	if db.IsNoRows(err) {
		return l, httpx.BadRequest("That isn't a shortcut.")
	}
	return l, err
}

// resolveShortcut tries each of the caller's own connections to the
// shortcut's platform; the pinner's connection is never used.
func (s *Service) resolveShortcut(ctx context.Context, me uuid.UUID, it *Item) (*ShortcutTarget, error) {
	l, err := s.linkOf(ctx, s.d.Pool, it.ID)
	if err != nil {
		return nil, err
	}
	label := ProductLabel(l.product)
	out := &ShortcutTarget{Provider: l.provider, Product: l.product, ProductLabel: label, Name: it.Name, MimeType: l.mime, Folder: l.folder, KindLabel: l.kindLabel}
	capability := CapGoogleDrive
	if l.provider == providerMicrosoft {
		capability = map[bool]string{true: CapSharePoint, false: CapOneDrive}[l.product == productSharePoint]
	}
	conns, err := s.connectedFor(ctx, me, it.OrgID)
	if err != nil {
		return nil, err
	}
	var noAccess, blocked, unavailable bool
	var reauth *connInfo
	for _, c := range conns {
		if c.Provider != l.provider {
			continue
		}
		switch c.Status {
		case "blocked":
			blocked = true
			continue
		case "needs_reauth":
			reauth = c
			continue
		}
		cs, err := s.session(ctx, me, it.OrgID, c.ID, false)
		if err != nil {
			reauth = c
			continue
		}
		target, err := cs.ad.get(ctx, l.ref)
		switch {
		case err == nil:
			out.Status, out.ConnectionID, out.Account, out.Ref = "ok", c.ID, c.Account, target.Ref
			out.Link = ConnectedLink(c.ID, target.Ref)
			out.Message = "Opens with " + c.label() + "."
			return out, nil
		case isStatus(err, http.StatusNotFound) || isStatus(err, http.StatusForbidden):
			noAccess = true
			out.Account = c.Account
		case isStatus(err, http.StatusConflict):
			reauth = c
		default:
			unavailable = true
		}
	}
	switch {
	case noAccess:
		out.Status, out.WebURL = "no_access", l.webURL
		out.Message = "Your " + label + " account (" + out.Account + ") can't open this. Ask for access in " + label + "."
	case reauth != nil:
		out.Status, out.ConnectURL, out.Account = "needs_reauth", connectURL(capability, itemLink(it.ID)), reauth.Account
		out.Message = "Reconnect " + reauth.label() + " to open this."
	case unavailable:
		out.Status = "unavailable"
		out.Message = label + " isn't answering. Try again shortly."
	case blocked:
		out.Status = "blocked"
		out.Message = "Your workspace doesn't allow " + label + " here."
	default:
		out.Status = "not_connected"
		out.ConnectURL = connectURL(capability, itemLink(it.ID))
		if g := s.grantURL(ctx, me, it.OrgID, capability); strings.Contains(g, "?") {
			out.ConnectURL = g
		}
		out.Message = "Connect " + label + " to open this."
	}
	return out, nil
}

type saveToReq struct {
	ConnectionID string `json:"connection_id"`
	ParentRef    string `json:"parent_ref"` // the platform folder ("": My Drive / OneDrive)
}

// saveToConnectedRoute: POST /v1/files/items/{id}/save-to {connection_id,
// parent_ref?} → the file saved to the person's Google Drive or OneDrive (a
// copy there; the Files file doesn't change).
func (s *Service) saveToConnectedRoute(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := idParam(r)
	if err != nil {
		return err
	}
	var req saveToReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	it, err := s.loadLive(ctx, s.d.Pool, me, id, access.View)
	if err != nil {
		return err
	}
	cs, err := s.session(ctx, me, it.OrgID, req.ConnectionID, true)
	if err != nil {
		return err
	}
	out, err := s.saveToConnected(ctx, cs, it, req.ParentRef)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, cs.toConnected(out))
	return nil
}

func (s *Service) saveToConnected(ctx context.Context, cs *connSession, it *Item, parent string) (extItem, error) {
	if it.Type != TypeFile || it.VersionID == nil {
		return extItem{}, errNotFile
	}
	var err error
	if parent == "" {
		if parent, err = cs.ad.rootRef(ctx); err != nil {
			return extItem{}, err
		}
	}
	key, err := s.versionKey(ctx, s.d.Pool, it.ID, *it.VersionID)
	if err != nil {
		return extItem{}, err
	}
	name := it.Name
	for n := 0; ; n++ {
		rc, err := s.d.Storage.Open(ctx, key)
		if err != nil {
			return extItem{}, err
		}
		out, err := cs.ad.write(ctx, parent, nil, name, it.MimeType, it.Size, rc)
		rc.Close()
		if err == nil {
			cs.forget(parent)
			return out, nil
		}
		if !isCode(err, "name_taken") || n >= 5 {
			return extItem{}, err
		}
		name = numberedName(it.Name, n+1, true)
	}
}

func isCode(err error, code string) bool {
	var he *httpx.Error
	return asHTTPError(err, &he) && he.Code == code
}
