package files

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/bg"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/notify"
	"scaffoldapp/backend/internal/storage"
)

// File requests: an upload-only link into a folder for people without an
// account (clients, candidates, vendors). They see the title, the
// instructions and the limits, never the folder or what's in it. Gated like
// public links: making one needs edit on the folder, access.share_public and
// files.file_requests, and the link works only while its creator still has
// all three (checked live). Agent credentials are refused; agents can't make
// requests.

const (
	PermFileRequests  = "files.file_requests"
	KindRequestUpload = "files.request_upload"

	requestPrefix      = "fr_"
	requestUploadTTL   = 24 * time.Hour
	maxRequestsPerPage = 100
)

var errRequestGone = httpx.NewError(http.StatusNotFound, "request_not_found", "This file request doesn't take files anymore. Ask the person who sent it for a new link.")

// FileRequest is a request as the people who can edit its folder see it.
type FileRequest struct {
	ID            uuid.UUID  `json:"id"`
	OrgID         uuid.UUID  `json:"org_id"`
	FolderID      uuid.UUID  `json:"folder_id"`
	FolderName    string     `json:"folder_name"`
	Title         string     `json:"title"`
	Instructions  string     `json:"instructions"`
	ExpiresAt     *time.Time `json:"expires_at"`
	MaxFiles      *int       `json:"max_files"`
	MaxFileBytes  *int64     `json:"max_file_bytes"`
	RequireEmail  bool       `json:"require_email"`
	FilesReceived int        `json:"files_received"`
	ClosedAt      *time.Time `json:"closed_at"`
	CreatedBy     *Person    `json:"created_by"`
	CreatedAt     time.Time  `json:"created_at"`
	Open          bool       `json:"open"`            // taking files now
	URL           string     `json:"url,omitempty"`   // only when created
	Token         string     `json:"token,omitempty"` // only when created
}

const requestColumns = `r.id, r.org_id, r.folder_id, f.name, r.title, r.instructions, r.expires_at, r.max_files, r.max_file_bytes,
	r.require_email, r.files_received, r.closed_at, r.created_by, u.display_name, u.avatar_url, r.created_at`

const requestJoins = ` JOIN files_items f ON f.id = r.folder_id LEFT JOIN users u ON u.id = r.created_by`

func scanRequest(row pgx.Row) (*FileRequest, error) {
	var fr FileRequest
	var by *uuid.UUID
	var name, avatar *string
	if err := row.Scan(&fr.ID, &fr.OrgID, &fr.FolderID, &fr.FolderName, &fr.Title, &fr.Instructions, &fr.ExpiresAt, &fr.MaxFiles, &fr.MaxFileBytes,
		&fr.RequireEmail, &fr.FilesReceived, &fr.ClosedAt, &by, &name, &avatar, &fr.CreatedAt); err != nil {
		return nil, err
	}
	fr.CreatedBy = person(by, name, avatar)
	fr.Open = requestOpen(&fr, time.Now())
	return &fr, nil
}

// requestOpen applies the request's own limits (not its creator's standing).
func requestOpen(fr *FileRequest, now time.Time) bool {
	if fr.ClosedAt != nil || fr.ExpiresAt != nil && !now.Before(*fr.ExpiresAt) || fr.CreatedBy == nil {
		return false
	}
	return fr.MaxFiles == nil || fr.FilesReceived < *fr.MaxFiles
}

func newRequestToken() (string, []byte) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	t := requestPrefix + base64.RawURLEncoding.EncodeToString(b[:])
	return t, hashRequestToken(t)
}

func hashRequestToken(t string) []byte {
	h := sha256.Sum256([]byte(t))
	return h[:]
}

func requestPath(token string) string { return "/f/request/" + token }

type createRequestReq struct {
	Title        string     `json:"title"`
	Instructions string     `json:"instructions"`
	ExpiresAt    *time.Time `json:"expires_at"`
	MaxFiles     *int       `json:"max_files"`
	MaxFileMB    *int64     `json:"max_file_mb"`
	RequireEmail *bool      `json:"require_email"`
}

// requestPermissions reports the org permission user lacks to run a file
// request in org ("" when none).
func requestPermissions(ctx context.Context, q db.Querier, orgID, user uuid.UUID) (string, error) {
	for _, key := range []string{access.PermSharePublic, PermFileRequests} {
		ok, err := access.Allowed(ctx, q, orgID, user, key)
		if err != nil {
			return "", err
		}
		if !ok {
			return key, nil
		}
	}
	return "", nil
}

// createRequest: POST /v1/files/items/{id}/requests (edit on the folder;
// access.share_public and files.file_requests; people only).
func (s *Service) createRequest(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	if access.ViaAgent(ctx) {
		return httpx.NewError(http.StatusForbidden, "agent_forbidden", "AI agents can't make file requests.")
	}
	id, err := idParam(r)
	if err != nil {
		return err
	}
	var req createRequestReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	req.Title = strings.TrimSpace(req.Title)
	req.Instructions = strings.TrimSpace(req.Instructions)
	switch {
	case req.Title == "" || utf8.RuneCountInString(req.Title) > 200:
		return httpx.BadRequest("Give the request a title of up to 200 characters.")
	case utf8.RuneCountInString(req.Instructions) > 2000:
		return httpx.BadRequest("Keep the instructions under 2,000 characters.")
	case req.ExpiresAt != nil && !req.ExpiresAt.After(time.Now().Add(time.Minute)):
		return httpx.BadRequest("Choose an end date in the future.")
	case req.MaxFiles != nil && (*req.MaxFiles < 1 || *req.MaxFiles > 10000):
		return httpx.BadRequest("Allow between 1 and 10,000 files.")
	case req.MaxFileMB != nil && (*req.MaxFileMB < 1 || *req.MaxFileMB<<20 > s.cfg.maxUpload):
		return httpx.BadRequest("The size limit must be between 1 MB and " + formatMB(s.cfg.maxUpload) + ".")
	}
	var maxBytes *int64
	if req.MaxFileMB != nil {
		b := *req.MaxFileMB << 20
		maxBytes = &b
	}
	requireEmail := req.RequireEmail == nil || *req.RequireEmail
	var out *FileRequest
	token, hash := newRequestToken()
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		folder, err := s.loadLive(ctx, tx, me, id, access.Edit)
		if err != nil {
			return err
		}
		if !folder.IsFolder() {
			return errNotFolder
		}
		if missing, err := requestPermissions(ctx, tx, folder.OrgID, me); err != nil {
			return err
		} else if missing != "" {
			p, _ := access.PermissionOf(missing)
			return permissionRequired(missing, p.Title)
		}
		if folder.PublicDisabled {
			return errPublicDisabled
		}
		var rid uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO files_requests (org_id, folder_id, created_by, title, instructions, token_hash, expires_at, max_files, max_file_bytes, require_email)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id`,
			folder.OrgID, folder.ID, me, req.Title, req.Instructions, hash, req.ExpiresAt, req.MaxFiles, maxBytes, requireEmail).Scan(&rid); err != nil {
			return err
		}
		out, err = scanRequest(tx.QueryRow(ctx, `SELECT `+requestColumns+` FROM files_requests r`+requestJoins+` WHERE r.id = $1`, rid))
		return err
	})
	if err != nil {
		return err
	}
	out.Token, out.URL = token, strings.TrimRight(s.d.Cfg.PublicWebURL, "/")+requestPath(token)
	httpx.JSON(w, http.StatusCreated, out)
	return nil
}

// folderRequests: GET /v1/files/items/{id}/requests (edit on the folder).
func (s *Service) folderRequests(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := idParam(r)
	if err != nil {
		return err
	}
	if _, err := s.load(ctx, s.d.Pool, me, id, access.Edit); err != nil {
		return err
	}
	return s.requestList(w, r, `r.folder_id = $1`, id)
}

// myRequests: GET /v1/files/orgs/{orgID}/requests (member): the requests you made.
func (s *Service) myRequests(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	return s.requestList(w, r, `r.org_id = $1 AND r.created_by = $2`, orgParam(r), me)
}

type requestCursor struct {
	T  time.Time `json:"t"`
	ID uuid.UUID `json:"id"`
}

func (s *Service) requestList(w http.ResponseWriter, r *http.Request, where string, args ...any) error {
	ctx := r.Context()
	p := httpx.ParsePage(r)
	limit := p.Limit
	if limit <= 0 || limit > maxRequestsPerPage {
		limit = 50
	}
	sql := `SELECT ` + requestColumns + ` FROM files_requests r` + requestJoins + ` WHERE ` + where + ` AND f.trashed_at IS NULL`
	if p.Cursor != "" {
		var c requestCursor
		if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
			return err
		}
		args = append(args, c.T, c.ID)
		sql += ` AND (r.created_at, r.id) < ($` + itoa(int64(len(args)-1)) + `, $` + itoa(int64(len(args))) + `)`
	}
	args = append(args, limit+1)
	sql += ` ORDER BY r.created_at DESC, r.id DESC LIMIT $` + itoa(int64(len(args)))
	rows, err := s.d.Pool.Query(ctx, sql, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	var list []*FileRequest
	for rows.Next() {
		fr, err := scanRequest(rows)
		if err != nil {
			return err
		}
		list = append(list, fr)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	page := httpx.NewPage(list, limit, func(fr *FileRequest) any { return requestCursor{T: fr.CreatedAt, ID: fr.ID} })
	if page.Items == nil {
		page.Items = []*FileRequest{}
	}
	httpx.OK(w, page)
	return nil
}

// loadRequest reads a request for someone who can edit its folder.
func (s *Service) loadRequest(ctx context.Context, q db.Querier, me uuid.UUID, rid uuid.UUID, min access.Level) (*FileRequest, error) {
	fr, err := scanRequest(q.QueryRow(ctx, `SELECT `+requestColumns+` FROM files_requests r`+requestJoins+` WHERE r.id = $1`, rid))
	if db.IsNoRows(err) {
		return nil, httpx.NotFound("File request not found.")
	}
	if err != nil {
		return nil, err
	}
	mine := fr.CreatedBy != nil && fr.CreatedBy.ID == me
	need := min
	if mine {
		need = access.Edit
	}
	if _, err := access.Require(ctx, q, me, Kind, fr.FolderID, need); err != nil {
		return nil, httpx.NotFound("File request not found.")
	}
	return fr, nil
}

// closeRequest: POST /v1/files/requests/{requestID}/close (its creator, or a
// manager of the folder).
func (s *Service) closeRequest(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	rid, err := uuid.Parse(chi.URLParam(r, "requestID"))
	if err != nil {
		return httpx.NotFound("File request not found.")
	}
	fr, err := s.loadRequest(ctx, s.d.Pool, me, rid, access.Manage)
	if err != nil {
		return err
	}
	if _, err := s.d.Pool.Exec(ctx, `UPDATE files_requests SET closed_at = COALESCE(closed_at, now()), closed_by = COALESCE(closed_by, $2) WHERE id = $1`, rid, me); err != nil {
		return err
	}
	fr, err = s.loadRequest(ctx, s.d.Pool, me, fr.ID, access.Manage)
	if err != nil {
		return err
	}
	httpx.OK(w, fr)
	return nil
}

// RequestUpload is a file someone sent through a request.
type RequestUpload struct {
	ID            uuid.UUID  `json:"id"`
	Name          string     `json:"name"`
	Size          int64      `json:"size"`
	UploaderName  string     `json:"uploader_name"`
	UploaderEmail string     `json:"uploader_email"`
	ItemID        *uuid.UUID `json:"item_id"` // null once the file was deleted
	CompletedAt   time.Time  `json:"completed_at"`
}

// requestUploads: GET /v1/files/requests/{requestID}/uploads (edit on the folder) — who sent what.
func (s *Service) requestUploads(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	rid, err := uuid.Parse(chi.URLParam(r, "requestID"))
	if err != nil {
		return httpx.NotFound("File request not found.")
	}
	if _, err := s.loadRequest(ctx, s.d.Pool, me, rid, access.Edit); err != nil {
		return err
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT id, name, size, uploader_name, uploader_email, item_id, completed_at FROM files_request_uploads
		WHERE request_id = $1 AND completed_at IS NOT NULL ORDER BY completed_at DESC LIMIT 500`, rid)
	if err != nil {
		return err
	}
	defer rows.Close()
	out := []RequestUpload{}
	for rows.Next() {
		var u RequestUpload
		if err := rows.Scan(&u.ID, &u.Name, &u.Size, &u.UploaderName, &u.UploaderEmail, &u.ItemID, &u.CompletedAt); err != nil {
			return err
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": out})
	return nil
}

// ---- public side ---------------------------------------------------------------------

// PublicRequest is what someone with a request link sees.
type PublicRequest struct {
	Title        string     `json:"title"`
	Instructions string     `json:"instructions"`
	RequestedBy  string     `json:"requested_by"`
	OrgName      string     `json:"org_name"`
	ExpiresAt    *time.Time `json:"expires_at"`
	MaxFileBytes int64      `json:"max_file_bytes"`
	FilesLeft    *int       `json:"files_left"` // null: no limit
	RequireEmail bool       `json:"require_email"`
}

// liveRequest resolves a token to an open request whose creator may still
// run it, or errRequestGone.
func (s *Service) liveRequest(ctx context.Context, q db.Querier, token string, forUpdate bool) (*FileRequest, error) {
	if !strings.HasPrefix(token, requestPrefix) || len(token) != len(requestPrefix)+43 {
		return nil, errRequestGone
	}
	sql := `SELECT ` + requestColumns + ` FROM files_requests r` + requestJoins + ` WHERE r.token_hash = $1 AND f.trashed_at IS NULL AND f.public_disabled_at IS NULL`
	if forUpdate {
		sql += ` FOR UPDATE OF r`
	}
	fr, err := scanRequest(q.QueryRow(ctx, sql, hashRequestToken(token)))
	if db.IsNoRows(err) {
		return nil, errRequestGone
	}
	if err != nil {
		return nil, err
	}
	if !fr.Open {
		return nil, errRequestGone
	}
	creator := fr.CreatedBy.ID
	if lvl, err := access.LevelOf(ctx, q, creator, Kind, fr.FolderID); err != nil {
		return nil, err
	} else if lvl < access.Edit {
		return nil, errRequestGone
	}
	if missing, err := requestPermissions(ctx, q, fr.OrgID, creator); err != nil {
		return nil, err
	} else if missing != "" {
		return nil, errRequestGone
	}
	return fr, nil
}

func (s *Service) requestFileLimit(fr *FileRequest) int64 {
	if fr.MaxFileBytes != nil && *fr.MaxFileBytes < s.cfg.maxUpload {
		return *fr.MaxFileBytes
	}
	return s.cfg.maxUpload
}

// publicRequest: GET /v1/files/requests/public/{token} (public).
func (s *Service) publicRequest(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	fr, err := s.liveRequest(ctx, s.d.Pool, chi.URLParam(r, "token"), false)
	if err != nil {
		return err
	}
	var org string
	_ = s.d.Pool.QueryRow(ctx, `SELECT name FROM orgs WHERE id = $1`, fr.OrgID).Scan(&org)
	out := PublicRequest{Title: fr.Title, Instructions: fr.Instructions, RequestedBy: fr.CreatedBy.Name, OrgName: org, ExpiresAt: fr.ExpiresAt,
		MaxFileBytes: s.requestFileLimit(fr), RequireEmail: fr.RequireEmail}
	if fr.MaxFiles != nil {
		left := *fr.MaxFiles - fr.FilesReceived
		out.FilesLeft = &left
	}
	httpx.OK(w, out)
	return nil
}

type requestUploadReq struct {
	Name          string `json:"name"`
	ContentType   string `json:"content_type"`
	Size          int64  `json:"size"`
	UploaderName  string `json:"uploader_name"`
	UploaderEmail string `json:"uploader_email"`
}

// validUploader checks the sender's name and email.
func validUploader(name, email string, requireEmail bool) (string, string, error) {
	name = strings.TrimSpace(name)
	email = strings.TrimSpace(email)
	if name == "" || utf8.RuneCountInString(name) > 100 {
		return "", "", httpx.BadRequest("Enter your name (up to 100 characters).")
	}
	if email == "" && requireEmail {
		return "", "", httpx.BadRequest("Enter your email address.")
	}
	if email != "" {
		a, err := mail.ParseAddress(email)
		if err != nil || a.Address != email || len(email) > 254 {
			return "", "", httpx.BadRequest("Enter a valid email address.")
		}
	}
	return name, email, nil
}

// publicRequestUpload: POST /v1/files/requests/public/{token}/uploads (public) → a presigned PUT.
func (s *Service) publicRequestUpload(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	var req requestUploadReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	fr, err := s.liveRequest(ctx, s.d.Pool, chi.URLParam(r, "token"), false)
	if err != nil {
		return err
	}
	name, email, err := validUploader(req.UploaderName, req.UploaderEmail, fr.RequireEmail)
	if err != nil {
		return err
	}
	file, err := cleanName(req.Name)
	if err != nil {
		return err
	}
	if req.Size <= 0 {
		return httpx.BadRequest("That file is empty.")
	}
	if limit := s.requestFileLimit(fr); req.Size > limit {
		return httpx.NewError(http.StatusRequestEntityTooLarge, "file_too_large", "That file is over "+formatMB(limit)+". Choose a smaller one.")
	}
	if s.cfg.quotaBytes > 0 {
		used, err := s.usedBytes(ctx, s.d.Pool, fr.OrgID)
		if err != nil {
			return err
		}
		if used+req.Size > s.cfg.quotaBytes {
			return httpx.NewError(http.StatusInsufficientStorage, "quota_exceeded", "This folder can't take more files right now. Tell the person who asked for them.")
		}
	}
	ct := baseType(req.ContentType)
	if ct == "" {
		ct = "application/octet-stream"
	}
	up, err := s.d.Storage.PresignPut(ctx, Purpose, fr.OrgID, ct, req.Size)
	if err != nil {
		return httpx.BadRequest(err.Error())
	}
	t := UploadTicket{UploadURL: up.UploadURL, Headers: up.Headers, ExpiresAt: time.Now().Add(15 * time.Minute)}
	if err := s.d.Pool.QueryRow(ctx, `
		INSERT INTO files_request_uploads (request_id, storage_key, name, content_type, size, uploader_name, uploader_email, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now() + make_interval(secs => $8)) RETURNING id`,
		fr.ID, up.Key, file, ct, req.Size, name, email, requestUploadTTL.Seconds()).Scan(&t.UploadID); err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, t)
	return nil
}

// publicRequestComplete: POST /v1/files/requests/public/{token}/uploads/{uploadID}/complete (public)
// → {name, size}. The file lands in the folder as the request's creator,
// with the sender recorded in its activity.
func (s *Service) publicRequestComplete(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	token := chi.URLParam(r, "token")
	uploadID, err := uuid.Parse(chi.URLParam(r, "uploadID"))
	if err != nil {
		return httpx.NotFound("Upload not found.")
	}
	var u struct {
		requestID           uuid.UUID
		key, name, ct       string
		size                int64
		uploader, email     string
	}
	err = s.d.Pool.QueryRow(ctx, `
		SELECT ru.request_id, ru.storage_key, ru.name, ru.content_type, ru.size, ru.uploader_name, ru.uploader_email
		FROM files_request_uploads ru JOIN files_requests r ON r.id = ru.request_id
		WHERE ru.id = $1 AND r.token_hash = $2 AND ru.completed_at IS NULL AND ru.expires_at > now()`, uploadID, hashRequestToken(token)).
		Scan(&u.requestID, &u.key, &u.name, &u.ct, &u.size, &u.uploader, &u.email)
	if db.IsNoRows(err) {
		return httpx.NotFound("Upload not found. Send the file again.")
	}
	if err != nil {
		return err
	}
	drop := func(e error) error {
		_, _ = s.d.Pool.Exec(ctx, `DELETE FROM files_request_uploads WHERE id = $1`, uploadID)
		return e
	}
	info, err := s.d.Storage.Stat(ctx, u.key)
	if errors.Is(err, storage.ErrObjectNotFound) {
		return httpx.NewError(http.StatusConflict, "upload_incomplete", "The file didn't finish uploading. Try again.")
	}
	if err != nil {
		return err
	}
	if info.Size != u.size {
		return drop(httpx.NewError(http.StatusUnprocessableEntity, "size_mismatch", "The uploaded file isn't the size that was announced. Send it again."))
	}
	head, err := s.d.Storage.ReadPrefix(ctx, u.key, 8192)
	if err != nil {
		return err
	}
	mt, textLike, err := classify(u.ct, u.name, head)
	if err != nil {
		return drop(err)
	}
	v := newVersion{key: u.key, size: info.Size, mime: mt, textLike: textLike, content: s.indexText(ctx, u.key, info.Size, textLike)}
	var out *Item
	var fr *FileRequest
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var err error
		if fr, err = s.liveRequest(ctx, tx, token, true); err != nil {
			return err
		}
		if fr.ID != u.requestID {
			return errRequestGone
		}
		if err := s.reserve(ctx, tx, fr.OrgID, info.Size); err != nil {
			return err
		}
		creator := fr.CreatedBy.ID
		pl, err := s.placeFor(ctx, tx, creator, fr.OrgID, &fr.FolderID)
		if err != nil {
			return err
		}
		id, err := s.insertItem(ctx, tx, pl, TypeFile, u.name, creator, true)
		if err != nil {
			return err
		}
		if err := s.addVersion(ctx, tx, id, fr.OrgID, creator, v); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE files_request_uploads SET completed_at = now(), item_id = $2 WHERE id = $1`, uploadID, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE files_requests SET files_received = files_received + 1 WHERE id = $1`, fr.ID); err != nil {
			return err
		}
		s.logActivity(ctx, tx, fr.OrgID, id, &fr.FolderID, creator, "requested_upload",
			map[string]any{"request_id": fr.ID, "request_title": fr.Title, "uploader_name": u.uploader, "uploader_email": u.email, "size": info.Size})
		out, err = s.getItem(ctx, tx, uuid.Nil, id)
		return err
	})
	if err != nil {
		var e *httpx.Error
		if errors.As(err, &e) && e.Status < 500 {
			return drop(err)
		}
		return err
	}
	s.changed(ctx, out, "created")
	if s.d.Notify != nil && fr.CreatedBy != nil {
		n := notify.Notification{UserID: fr.CreatedBy.ID, Kind: KindRequestUpload, Title: u.uploader + " sent a file for “" + fr.Title + "”",
			Body: out.Name, Link: itemLink(out.ID), Data: map[string]any{"kind": Kind, "id": out.ID.String(), "request_id": fr.ID.String()}}
		bg.Go(ctx, "files.request_notify", func(ctx context.Context) error {
			if err := s.d.Notify.Send(ctx, n); err != nil {
				slog.WarnContext(ctx, "files: request notification", "err", err)
			}
			return nil
		})
	}
	httpx.OK(w, map[string]any{"name": out.Name, "size": out.Size})
	return nil
}
