package files

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/storage"
)

// Uploads go straight to storage: POST …/uploads returns a presigned PUT
// for an object under the org, the client PUTs the bytes, then POST
// …/uploads/{id}/complete checks the object (size, type against its bytes)
// and creates the file or its new version.

type startUploadReq struct {
	ParentID    *uuid.UUID `json:"parent_id"` // the folder (null: the top of your My drive)
	ItemID      *uuid.UUID `json:"item_id"`   // upload a new version of this file instead
	Name        string     `json:"name"`
	ContentType string     `json:"content_type"`
	Size        int64      `json:"size"`
}

// UploadTicket is what the client needs to send the bytes.
type UploadTicket struct {
	UploadID  uuid.UUID         `json:"upload_id"`
	UploadURL string            `json:"upload_url"`
	Headers   map[string]string `json:"headers"`
	ExpiresAt time.Time         `json:"expires_at"`
}

// startUpload: POST /v1/files/orgs/{orgID}/uploads.
func (s *Service) startUpload(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	orgID := orgParam(r)
	var req startUploadReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.Size <= 0 {
		return httpx.BadRequest("That file is empty. Create an empty file instead.")
	}
	if req.Size > s.cfg.maxUpload {
		return httpx.NewError(http.StatusRequestEntityTooLarge, "file_too_large",
			"That file is over "+formatMB(s.cfg.maxUpload)+". Choose a smaller one.")
	}
	ct := baseType(req.ContentType)
	if ct == "" {
		ct = "application/octet-stream"
	}
	name := req.Name
	var parentID *uuid.UUID
	if req.ItemID != nil {
		it, err := s.loadLive(ctx, s.d.Pool, me, *req.ItemID, access.Edit)
		if err != nil {
			return err
		}
		if it.Type != TypeFile {
			return errNotFile
		}
		if it.OrgID != orgID {
			return errItemNotFound
		}
		name = it.Name
	} else {
		clean, err := cleanName(name)
		if err != nil {
			return err
		}
		name = clean
		pl, err := s.placeFor(ctx, s.d.Pool, me, orgID, req.ParentID)
		if err != nil {
			return err
		}
		parentID = pl.parentID()
	}
	if s.cfg.quotaBytes > 0 {
		used, err := s.usedBytes(ctx, s.d.Pool, orgID)
		if err != nil {
			return err
		}
		if used+req.Size > s.cfg.quotaBytes {
			return errQuota()
		}
	}
	up, err := s.d.Storage.PresignPut(ctx, Purpose, orgID, ct, req.Size)
	if err != nil {
		return httpx.BadRequest(err.Error())
	}
	t := UploadTicket{UploadURL: up.UploadURL, Headers: up.Headers, ExpiresAt: time.Now().Add(15 * time.Minute)}
	if err := s.d.Pool.QueryRow(ctx, `
		INSERT INTO files_uploads (org_id, user_id, parent_id, item_id, name, content_type, size, storage_key, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now() + make_interval(secs => $9)) RETURNING id`,
		orgID, me, parentID, req.ItemID, name, ct, req.Size, up.Key, uploadTTL.Seconds()).Scan(&t.UploadID); err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, t)
	return nil
}

func formatMB(b int64) string {
	if b >= 1<<30 && b%(1<<30) == 0 {
		return itoa(b>>30) + " GB"
	}
	return itoa(b>>20) + " MB"
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

type completeUploadReq struct {
	OnConflict string `json:"on_conflict"` // rename (default) | fail: a taken name answers 409 and the upload can be completed again
}

type uploadRow struct {
	ID          uuid.UUID
	OrgID       uuid.UUID
	ParentID    *uuid.UUID
	ItemID      *uuid.UUID
	Name        string
	ContentType string
	Size        int64
	Key         string
}

// completeUpload: POST /v1/files/uploads/{uploadID}/complete → the new
// file (201) or the file with its new version (200).
func (s *Service) completeUpload(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	uploadID, err := uuid.Parse(chi.URLParam(r, "uploadID"))
	if err != nil {
		return httpx.NotFound("Upload not found.")
	}
	var req completeUploadReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	var u uploadRow
	err = s.d.Pool.QueryRow(ctx, `
		SELECT id, org_id, parent_id, item_id, name, content_type, size, storage_key FROM files_uploads
		WHERE id = $1 AND user_id = $2 AND completed_at IS NULL AND expires_at > now()`, uploadID, me).
		Scan(&u.ID, &u.OrgID, &u.ParentID, &u.ItemID, &u.Name, &u.ContentType, &u.Size, &u.Key)
	if db.IsNoRows(err) {
		return httpx.NotFound("Upload not found. Start it again.")
	}
	if err != nil {
		return err
	}
	drop := func(e error) error {
		_, _ = s.d.Pool.Exec(ctx, `DELETE FROM files_uploads WHERE id = $1`, u.ID) // the trigger queues the object
		return e
	}
	info, err := s.d.Storage.Stat(ctx, u.Key)
	if errors.Is(err, storage.ErrObjectNotFound) {
		return httpx.NewError(http.StatusConflict, "upload_incomplete", "The file didn't finish uploading. Try again.")
	}
	if err != nil {
		return err
	}
	if info.Size != u.Size {
		return drop(httpx.NewError(http.StatusUnprocessableEntity, "size_mismatch", "The uploaded file isn't the size that was announced. Upload it again."))
	}
	head, err := s.d.Storage.ReadPrefix(ctx, u.Key, 8192)
	if err != nil {
		return err
	}
	mt, textLike, err := classify(u.ContentType, u.Name, head)
	if err != nil {
		return drop(err)
	}
	v := newVersion{key: u.Key, size: info.Size, mime: mt, textLike: textLike, content: s.indexText(ctx, u.Key, info.Size, textLike)}
	var out *Item
	created := u.ItemID == nil
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if err := s.reserve(ctx, tx, u.OrgID, info.Size); err != nil {
			return err
		}
		var id uuid.UUID
		var parent *uuid.UUID
		if u.ItemID != nil {
			it, err := s.loadLive(ctx, tx, me, *u.ItemID, access.Edit)
			if err != nil {
				return err
			}
			if it.Type != TypeFile {
				return errNotFile
			}
			id, parent = it.ID, it.ParentID
			if err := s.addVersion(ctx, tx, id, u.OrgID, me, v); err != nil {
				return err
			}
			s.logActivity(ctx, tx, u.OrgID, id, parent, me, "version", map[string]any{"size": info.Size})
		} else {
			pl, err := s.placeFor(ctx, tx, me, u.OrgID, u.ParentID)
			if err != nil {
				return err
			}
			if id, err = s.insertItem(ctx, tx, pl, TypeFile, u.Name, me, req.OnConflict != "fail"); err != nil {
				return err
			}
			parent = pl.parentID()
			if err := s.addVersion(ctx, tx, id, u.OrgID, me, v); err != nil {
				return err
			}
			s.logActivity(ctx, tx, u.OrgID, id, parent, me, "uploaded", map[string]any{"size": info.Size})
		}
		if _, err := tx.Exec(ctx, `UPDATE files_uploads SET completed_at = now() WHERE id = $1`, u.ID); err != nil {
			return err
		}
		out, err = s.getItem(ctx, tx, me, id)
		return err
	})
	if err != nil {
		var e *httpx.Error
		if errors.As(err, &e) && e.Status < 500 && e != errNameTaken {
			return drop(err)
		}
		return err
	}
	s.touchRecent(ctx, me, out, "edited")
	action := "version"
	status := http.StatusOK
	if created {
		action, status = "created", http.StatusCreated
	}
	s.changed(ctx, out, action)
	httpx.JSON(w, status, out)
	return nil
}

// cancelUpload: DELETE /v1/files/uploads/{uploadID} — the object is deleted.
func (s *Service) cancelUpload(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	uploadID, err := uuid.Parse(chi.URLParam(r, "uploadID"))
	if err != nil {
		return httpx.NotFound("Upload not found.")
	}
	if _, err := s.d.Pool.Exec(ctx, `DELETE FROM files_uploads WHERE id = $1 AND user_id = $2 AND completed_at IS NULL`, uploadID, me); err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

type createFileReq struct {
	ParentID    *uuid.UUID `json:"parent_id"`
	Name        string     `json:"name"`
	Text        string     `json:"text"`
	ContentType string     `json:"content_type"`
	OnConflict  string     `json:"on_conflict"` // rename (default) | fail
}

// createTextFile: POST /v1/files/orgs/{orgID}/files {parent_id?, name, text}
// — a text file up to 1 MB written by the server (also how empty files are
// made).
func (s *Service) createTextFile(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	var req createFileReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	out, err := s.createText(ctx, me, orgParam(r), req.ParentID, req.Name, req.Text, req.ContentType, req.OnConflict != "fail")
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, out)
	return nil
}
