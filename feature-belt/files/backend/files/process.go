package files

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/storage"
)

// The background pass over new versions (job files.process, every 15 s,
// once per tick across instances): a thumbnail for images and office files
// that carry a preview picture, and the text of PDFs and office documents
// for search and for agents. Text files are indexed at upload already.
// Videos get their poster from the uploading browser (putThumbnail).

const (
	processBatch  = 6
	processBudget = 10 * time.Second
)

type pendingVersion struct {
	id, itemID, orgID uuid.UUID
	key, mt, name     string
	size              int64
	textLike          bool
	current           bool
}

// processPending works through versions waiting for their pass, oldest first.
func (s *Service) processPending(ctx context.Context) error {
	deadline := time.Now().Add(processBudget)
	rows, err := s.d.Pool.Query(ctx, `
		SELECT v.id, v.item_id, v.org_id, v.storage_key, v.mime_type, i.name, v.size, v.text_like, i.version_id = v.id
		FROM files_versions v JOIN files_items i ON i.id = v.item_id
		WHERE v.processed_at IS NULL ORDER BY v.created_at LIMIT $1`, processBatch)
	if err != nil {
		return err
	}
	var todo []pendingVersion
	for rows.Next() {
		var p pendingVersion
		var current *bool
		if err := rows.Scan(&p.id, &p.itemID, &p.orgID, &p.key, &p.mt, &p.name, &p.size, &p.textLike, &current); err != nil {
			rows.Close()
			return err
		}
		p.current = current != nil && *current
		todo = append(todo, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, p := range todo {
		if time.Now().After(deadline) {
			break
		}
		s.processVersion(ctx, p)
	}
	return nil
}

// processVersion makes what a version needs and marks it done, whatever
// happens (a failure is logged, never retried forever).
func (s *Service) processVersion(ctx context.Context, p pendingVersion) {
	kind := previewKind(TypeFile, p.mt, p.name, p.textLike)
	format := extractFormat(p.name, p.mt)
	wantThumb := kind == "image" || format == "docx" || format == "xlsx" || format == "pptx" || format == "odf"
	wantText := format != "" // PDFs written as plain ASCII are text-like too; their text is still what the reader shows
	var thumbKey *string
	var text string
	if (wantThumb || wantText) && p.size > 0 && p.size <= maxThumbSource {
		data, err := s.readObject(ctx, p.key, maxThumbSource)
		if err != nil {
			slog.WarnContext(ctx, "files: read for processing", "version", p.id, "err", err)
		} else {
			if wantThumb {
				src := data
				if kind != "image" {
					src = embeddedThumbnail(data)
				}
				if src != nil {
					if k, err := s.storeThumb(ctx, p.orgID, src); err == nil {
						thumbKey = &k
					} else if !errors.Is(err, errNotImage) {
						slog.WarnContext(ctx, "files: thumbnail", "version", p.id, "err", err)
					}
				}
			}
			if wantText {
				if t, err := extractText(p.name, p.mt, data); err == nil {
					text = t
				} else if !errors.Is(err, errNoText) {
					slog.InfoContext(ctx, "files: extract text", "version", p.id, "err", err)
				}
			}
		}
	}
	tag, err := s.d.Pool.Exec(ctx, `UPDATE files_versions SET thumb_key = COALESCE($2, thumb_key), processed_at = now() WHERE id = $1 AND processed_at IS NULL`, p.id, thumbKey)
	if err != nil || tag.RowsAffected() == 0 {
		if thumbKey != nil {
			s.queueDeletion(ctx, *thumbKey)
		}
		if err != nil {
			slog.WarnContext(ctx, "files: mark processed", "version", p.id, "err", err)
		}
		return
	}
	if text != "" {
		if _, err := s.d.Pool.Exec(ctx, `UPDATE files_items SET content_text = $2, content_source = 'extracted' WHERE id = $1 AND version_id = $3`,
			p.itemID, text, p.id); err != nil {
			slog.WarnContext(ctx, "files: store extracted text", "item", p.itemID, "err", err)
		}
	}
	if thumbKey != nil && p.current {
		s.changedByID(ctx, p.itemID, "thumbnail")
	}
}

// readObject reads a whole object, refusing more than limit bytes.
func (s *Service) readObject(ctx context.Context, key string, limit int64) ([]byte, error) {
	rc, err := s.d.Storage.Open(ctx, key)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, errors.New("files: object larger than the processing limit")
	}
	return b, nil
}

// storeThumb scales src and writes it as files/<org>/<uuid>.jpg|png.
func (s *Service) storeThumb(ctx context.Context, orgID uuid.UUID, src []byte) (string, error) {
	thumb, ct, err := makeThumb(src)
	if err != nil {
		return "", err
	}
	key, err := storage.NewKey(Purpose, orgID, ct)
	if err != nil {
		return "", err
	}
	if err := s.d.Storage.Put(ctx, key, ct, thumb); err != nil {
		return "", err
	}
	return key, nil
}

// changedByID publishes files.changed for an item read fresh (no caller).
func (s *Service) changedByID(ctx context.Context, id uuid.UUID, action string) {
	it, err := s.getItem(ctx, s.d.Pool, uuid.Nil, id)
	if err == nil {
		s.changed(ctx, it, action)
	}
}

// putThumbnail: PUT /v1/files/items/{id}/thumbnail {image: base64} (edit; ≤
// 700 KB): a video's poster frame (or a PDF's first page) the uploading
// browser drew. It is decoded and re-encoded like any thumbnail and set on
// the current version.
func (s *Service) putThumbnail(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := idParam(r)
	if err != nil {
		return err
	}
	it, err := s.loadLive(ctx, s.d.Pool, me, id, access.Edit)
	if err != nil {
		return err
	}
	if it.Type != TypeFile || it.VersionID == nil {
		return errNotFile
	}
	if k := previewKind(TypeFile, it.MimeType, it.Name, it.TextLike); k != "video" && k != "pdf" {
		return httpx.BadRequest("Only videos and PDFs take a picture from the browser; images get theirs from the server.")
	}
	var req struct {
		Image string `json:"image"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	raw := req.Image
	if i := strings.Index(raw, ";base64,"); strings.HasPrefix(raw, "data:") && i > 0 {
		raw = raw[i+len(";base64,"):]
	}
	body, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(body) == 0 {
		return httpx.BadRequest("image must be a base64-encoded JPEG or PNG.")
	}
	if len(body) > maxPosterUpload {
		return httpx.NewError(http.StatusRequestEntityTooLarge, "too_large", "The picture is too large (700 KB at most).")
	}
	key, err := s.storeThumb(ctx, it.OrgID, body)
	if errors.Is(err, errNotImage) {
		return httpx.NewError(http.StatusUnprocessableEntity, "not_image", "That isn't a picture this server can read.")
	}
	if err != nil {
		return err
	}
	var old *string
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT thumb_key FROM files_versions WHERE id = $1 FOR UPDATE`, *it.VersionID).Scan(&old); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE files_versions SET thumb_key = $2 WHERE id = $1`, *it.VersionID, key)
		return err
	})
	if err != nil {
		s.queueDeletion(ctx, key)
		return err
	}
	if old != nil {
		s.queueDeletion(ctx, *old)
	}
	s.changed(ctx, it, "thumbnail")
	httpx.NoContent(w)
	return nil
}

// thumbFor is the presigned inline URL of a thumbnail key.
func (s *Service) thumbFor(ctx context.Context, key string) (string, error) {
	ct := "image/jpeg"
	if strings.HasSuffix(key, ".png") {
		ct = "image/png"
	}
	return s.d.Storage.PresignGetWith(ctx, key, urlTTL, storage.GetOptions{ContentType: ct, Disposition: "inline"})
}

// isRasterSmall reports whether an image is small enough to stand in for its
// own thumbnail until the server makes one.
func isRasterSmall(size int64) bool { return size > 0 && size <= 3<<20 }

var errNoExtractedText = httpx.NewError(http.StatusUnsupportedMediaType, "not_text",
	"There's no text to read in this file. Text files, PDFs and Word, Excel, PowerPoint and OpenDocument files have text.")

// extractedText is a document's text as indexed (the background pass), read
// now when the pass hasn't reached it yet.
func (s *Service) extractedText(ctx context.Context, it *Item) (string, error) {
	if it.Type != TypeFile || it.VersionID == nil || extractFormat(it.Name, it.MimeType) == "" {
		return "", errNoExtractedText
	}
	var text *string
	var source string
	if err := s.d.Pool.QueryRow(ctx, `SELECT content_text, content_source FROM files_items WHERE id = $1`, it.ID).Scan(&text, &source); err != nil {
		return "", err
	}
	if text != nil && source == "extracted" {
		return *text, nil
	}
	if it.Size > maxThumbSource {
		return "", errNoExtractedText
	}
	var key string
	if err := s.d.Pool.QueryRow(ctx, `SELECT storage_key FROM files_versions WHERE id = $1`, *it.VersionID).Scan(&key); err != nil {
		return "", err
	}
	data, err := s.readObject(ctx, key, maxThumbSource)
	if err != nil {
		return "", err
	}
	t, err := extractText(it.Name, it.MimeType, data)
	if err != nil {
		return "", errNoExtractedText
	}
	return t, nil
}
