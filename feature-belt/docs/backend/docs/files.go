package docs

import (
	"context"
	"errors"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/storage"
)

// fileTypes are what pages accept: photos and the usual documents.
var fileTypes = append([]string{"image/jpeg", "image/png", "image/webp", "image/gif", "image/heic"}, storage.DocumentTypes...)

// extTypes maps a storage key's extension (issued by storage.PresignPut for
// the validated type) back to the content type.
var extTypes = map[string]string{
	"jpg": "image/jpeg", "png": "image/png", "webp": "image/webp", "gif": "image/gif", "heic": "image/heic",
	"pdf": "application/pdf", "txt": "text/plain", "csv": "text/csv", "zip": "application/zip",
	"docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	"xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	"pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
}

const fileURLTTL = time.Hour

// File is an upload on a page. URL is a short-lived download link.
type File struct {
	ID          uuid.UUID  `json:"id"`
	PageID      uuid.UUID  `json:"page_id"`
	Name        string     `json:"name"`
	ContentType string     `json:"content_type"`
	Size        int64      `json:"size"`
	URL         string     `json:"url,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

type registerFileReq struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	Size int64  `json:"size"` // ignored: the stored object's size counts
}

// registerFile: POST /docs/pages/{id}/files (edit) {key, name, size}: attach
// a finished upload (purpose docs_file) to the page. Image and file blocks
// and covers then refer to the returned id.
func (s *Service) registerFile(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req registerFileReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	p, err := requirePage(ctx, s.d.Pool, me, id, access.Edit)
	if err != nil {
		return err
	}
	if err := editable(p, false); err != nil {
		return err
	}
	if !storage.KeyOwnedBy(req.Key, FilePurpose, me) {
		return httpx.BadRequest("Upload the file first.")
	}
	ct, ok := extTypes[strings.TrimPrefix(path.Ext(req.Key), ".")]
	if !ok {
		return httpx.BadRequest("That kind of file can't go on a page.")
	}
	name := strings.TrimSpace(cleanText(path.Base(strings.ReplaceAll(req.Name, `\`, "/"))))
	if name == "" || name == "." || name == "/" {
		name = "file" + path.Ext(req.Key)
	}
	if utf8.RuneCountInString(name) > 200 {
		name = string([]rune(name)[:200])
	}
	if s.d.Storage == nil {
		return errStorageUnavailable
	}
	// The stored object, not the size the client claims.
	info, err := s.d.Storage.Stat(ctx, req.Key)
	if errors.Is(err, storage.ErrObjectNotFound) {
		return httpx.BadRequest("Upload the file first.")
	}
	if err != nil {
		return err
	}
	if info.Size > s.cfg.fileMax {
		return httpx.BadRequest("That file is too large.")
	}
	f := File{PageID: id, Name: name, ContentType: ct, Size: info.Size}
	if err := s.d.Pool.QueryRow(ctx, `
		INSERT INTO docs_files (org_id, page_id, key, name, content_type, size, uploaded_by) VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		p.OrgID, id, req.Key, name, ct, req.Size, me).Scan(&f.ID); err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, f)
	return nil
}

// fileURL: GET /docs/files/{fileID} (view on its page): a download link.
func (s *Service) fileURL(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	fileID, err := httpx.URLParamUUID(r, "fileID")
	if err != nil {
		return err
	}
	f, key, err := s.loadFile(ctx, fileID)
	if err != nil {
		return err
	}
	if _, err := access.Require(ctx, s.d.Pool, me, Kind, f.PageID, access.View); err != nil {
		return httpx.NotFound("File not found.")
	}
	if err := s.sign(ctx, f, key); err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "private, max-age=600")
	httpx.OK(w, f)
	return nil
}

func (s *Service) loadFile(ctx context.Context, id uuid.UUID) (*File, string, error) {
	var f File
	var key string
	err := s.d.Pool.QueryRow(ctx, `SELECT id, page_id, name, content_type, size, key FROM docs_files WHERE id = $1`, id).
		Scan(&f.ID, &f.PageID, &f.Name, &f.ContentType, &f.Size, &key)
	if db.IsNoRows(err) {
		return nil, "", httpx.NotFound("File not found.")
	}
	return &f, key, err
}

var errStorageUnavailable = httpx.NewError(http.StatusServiceUnavailable, "storage_unavailable", "Files can't be opened right now. Try again later.")

// sign presigns a download that opens what the browser can show and saves
// the rest under the file's name (not the storage key's).
func (s *Service) sign(ctx context.Context, f *File, key string) error {
	if s.d.Storage == nil {
		return errStorageUnavailable
	}
	url, err := s.d.Storage.PresignGetWith(ctx, key, fileURLTTL, storage.GetOptions{
		ContentType: f.ContentType, Disposition: mime.FormatMediaType("inline", map[string]string{"filename": f.Name}),
	})
	if err != nil {
		return err
	}
	exp := time.Now().Add(fileURLTTL)
	f.URL, f.ExpiresAt = url, &exp
	return nil
}
