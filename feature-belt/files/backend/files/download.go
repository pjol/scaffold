package files

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/storage"
)

// FileURL is a short-lived download or preview link.
type FileURL struct {
	URL         string    `json:"url"`
	ExpiresAt   time.Time `json:"expires_at"`
	Disposition string    `json:"disposition"` // inline | attachment
	ContentType string    `json:"content_type"`
	Filename    string    `json:"filename"`
}

// signedURL presigns a version with the serving rules (servePolicy).
func (s *Service) signedURL(ctx context.Context, it *Item, versionID *uuid.UUID, wantInline bool) (*FileURL, error) {
	if it.Type != TypeFile || it.VersionID == nil {
		return nil, errNotFile
	}
	vid := *it.VersionID
	if versionID != nil {
		vid = *versionID
	}
	var key, mt string
	var textLike bool
	err := s.d.Pool.QueryRow(ctx, `SELECT storage_key, mime_type, text_like FROM files_versions WHERE id = $1 AND item_id = $2`,
		vid, it.ID).Scan(&key, &mt, &textLike)
	if db.IsNoRows(err) {
		return nil, httpx.NotFound("Version not found.")
	}
	if err != nil {
		return nil, err
	}
	ct, cd := servePolicy(mt, it.Name, textLike, wantInline)
	u, err := s.d.Storage.PresignGetWith(ctx, key, urlTTL, storage.GetOptions{ContentType: ct, Disposition: cd})
	if err != nil {
		return nil, err
	}
	disp, _, _ := mime.ParseMediaType(cd)
	return &FileURL{URL: u, ExpiresAt: time.Now().Add(urlTTL), Disposition: disp, ContentType: ct, Filename: it.Name}, nil
}

func optionalVersion(r *http.Request) (*uuid.UUID, error) {
	v := r.URL.Query().Get("version")
	if v == "" {
		return nil, nil
	}
	id, err := uuid.Parse(v)
	if err != nil {
		return nil, httpx.NotFound("Version not found.")
	}
	return &id, nil
}

// urlRoute: GET /v1/files/items/{id}/url?version=&disposition=inline|attachment (view).
func (s *Service) urlRoute(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := idParam(r)
	if err != nil {
		return err
	}
	vid, err := optionalVersion(r)
	if err != nil {
		return err
	}
	it, err := s.load(ctx, s.d.Pool, me, id, access.View)
	if err != nil {
		return err
	}
	u, err := s.signedURL(ctx, it, vid, r.URL.Query().Get("disposition") == "inline")
	if err != nil {
		return err
	}
	s.touchRecent(ctx, me, it, "viewed")
	httpx.OK(w, u)
	return nil
}

// TextContent is a text file's content for previews.
type TextContent struct {
	Text      string    `json:"text"`
	Truncated bool      `json:"truncated"` // only the first 1 MB
	Size      int64     `json:"size"`
	VersionID uuid.UUID `json:"version_id"`
}

// textRoute: GET /v1/files/items/{id}/text?version= (view; text files).
func (s *Service) textRoute(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := idParam(r)
	if err != nil {
		return err
	}
	vid, err := optionalVersion(r)
	if err != nil {
		return err
	}
	it, err := s.load(ctx, s.d.Pool, me, id, access.View)
	if err != nil {
		return err
	}
	out, err := s.textContent(ctx, it, vid)
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

func (s *Service) textContent(ctx context.Context, it *Item, vid *uuid.UUID) (*TextContent, error) {
	b, truncated, err := s.readText(ctx, it, vid, maxTextPreview)
	if err != nil {
		return nil, err
	}
	out := &TextContent{Text: string(b), Truncated: truncated, Size: it.Size}
	if vid != nil {
		out.VersionID = *vid
	} else if it.VersionID != nil {
		out.VersionID = *it.VersionID
	}
	return out, nil
}

// ---- activity ---------------------------------------------------------------------

type activityCursor struct {
	T  time.Time `json:"t"`
	ID uuid.UUID `json:"id"`
}

// activityRoute: GET /v1/files/items/{id}/activity?cursor= (view). A folder
// also shows events on its direct children the caller can see; managers also
// see sharing changes and public links.
func (s *Service) activityRoute(w http.ResponseWriter, r *http.Request) error {
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
	p := httpx.ParsePage(r)
	page, err := s.activity(ctx, s.d.Pool, me, it, p.Cursor, p.Limit)
	if err != nil {
		return err
	}
	httpx.OK(w, page)
	return nil
}

func (s *Service) activity(ctx context.Context, q db.Querier, me uuid.UUID, it *Item, cursor string, limit int) (httpx.Page[Activity], error) {
	args := []any{it.ID, me, it.MyLevel >= access.Manage, access.ViaAgent(ctx)}
	sql := `
		SELECT x.id, x.item_id, x.item_name, x.action, x.actor_id, u.display_name, u.avatar_url, x.data, x.created_at FROM (
			SELECT a.id, a.item_id, i.name AS item_name, a.action, a.actor_id, a.data, a.created_at
			FROM files_activity a JOIN files_items i ON i.id = a.item_id
			WHERE a.item_id = $1
			   OR (a.folder_id = $1 AND a.item_id <> $1 AND access_level($2, 'files.item', a.item_id, $4) >= 1)
			UNION ALL
			SELECT g.id, g.resource_id, i.name, 'shared', g.granted_by,
			       jsonb_build_object('subject_type', g.subject_type, 'level', g.level,
			                          'subject_name', COALESCE(su.display_name, t.name, o.name, '')),
			       g.updated_at
			FROM access_grants g JOIN files_items i ON i.id = g.resource_id
			LEFT JOIN users su ON g.subject_type = 'user' AND su.id = g.subject_id
			LEFT JOIN access_teams t ON g.subject_type = 'team' AND t.id = g.subject_id
			LEFT JOIN orgs o ON g.subject_type = 'org' AND o.id = g.subject_id
			WHERE $3 AND g.resource_kind = 'files.item' AND g.resource_id = $1
			  AND NOT (g.subject_type = 'user' AND g.subject_id = g.granted_by)
			UNION ALL
			SELECT k.id, k.resource_id, i.name, 'link_created', k.created_by,
			       jsonb_build_object('expires_at', k.expires_at, 'revoked', k.revoked_at IS NOT NULL),
			       k.created_at
			FROM access_links k JOIN files_items i ON i.id = k.resource_id
			WHERE $3 AND k.resource_kind = 'files.item' AND k.resource_id = $1
		) x LEFT JOIN users u ON u.id = x.actor_id`
	if cursor != "" {
		var c activityCursor
		if err := httpx.DecodeCursor(cursor, &c); err != nil {
			return httpx.Page[Activity]{}, err
		}
		args = append(args, c.T, c.ID)
		sql += ` WHERE (x.created_at, x.id) < ($5, $6)`
	}
	args = append(args, limit+1)
	sql += ` ORDER BY x.created_at DESC, x.id DESC LIMIT $` + itoa(int64(len(args)))
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return httpx.Page[Activity]{}, err
	}
	defer rows.Close()
	var list []Activity
	for rows.Next() {
		var a Activity
		var actor *uuid.UUID
		var name, avatar *string
		var data []byte
		if err := rows.Scan(&a.ID, &a.ItemID, &a.ItemName, &a.Action, &actor, &name, &avatar, &data, &a.CreatedAt); err != nil {
			return httpx.Page[Activity]{}, err
		}
		a.Actor = person(actor, name, avatar)
		a.Data = map[string]any{}
		_ = json.Unmarshal(data, &a.Data)
		list = append(list, a)
	}
	if err := rows.Err(); err != nil {
		return httpx.Page[Activity]{}, err
	}
	return httpx.NewPage(list, limit, func(a Activity) any { return activityCursor{T: a.CreatedAt, ID: a.ID} }), nil
}

// ---- zip downloads -------------------------------------------------------------------

type archiveReq struct {
	IDs  []uuid.UUID `json:"ids"`
	Name string      `json:"name"`
}

// Archive is a zip download link (valid for 5 minutes).
type Archive struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

// createArchive: POST /v1/files/orgs/{orgID}/archives {ids, name?} — a
// short-lived link that streams the items (folders with what's in them the
// caller can see) as one zip. Access is checked again when it streams.
func (s *Service) createArchive(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	orgID := orgParam(r)
	var req archiveReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if len(req.IDs) == 0 || len(req.IDs) > maxBatch {
		return httpx.BadRequest("Choose between 1 and 200 items.")
	}
	name := "Download"
	for i, id := range req.IDs {
		it, err := s.loadLive(ctx, s.d.Pool, me, id, access.View)
		if err != nil {
			return err
		}
		if it.OrgID != orgID {
			return errItemNotFound
		}
		if i == 0 && len(req.IDs) == 1 {
			name = strings.TrimSuffix(it.Name, path.Ext(it.Name))
		}
	}
	if n, err := cleanName(req.Name); err == nil {
		name = n
	}
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return err
	}
	token := base64.RawURLEncoding.EncodeToString(raw[:])
	hash := sha256.Sum256([]byte(token))
	exp := time.Now().Add(5 * time.Minute)
	if _, err := s.d.Pool.Exec(ctx, `
		INSERT INTO files_archives (token_hash, user_id, org_id, item_ids, name, expires_at) VALUES ($1, $2, $3, $4, $5, $6)`,
		hash[:], me, orgID, req.IDs, name, exp); err != nil {
		return err
	}
	base := strings.TrimRight(s.d.Cfg.PublicAPIURL, "/")
	httpx.JSON(w, http.StatusCreated, Archive{URL: base + "/files-archive/" + token, ExpiresAt: exp})
	return nil
}

// serveArchive: GET /files-archive/{token} streams the zip. The caller's
// access is re-checked per item; a folder includes only what they can see.
func (s *Service) serveArchive(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token := chi.URLParam(r, "token")
	hash := sha256.Sum256([]byte(token))
	var me, orgID uuid.UUID
	var ids []uuid.UUID
	var name string
	err := s.d.Pool.QueryRow(ctx, `
		SELECT a.user_id, a.org_id, a.item_ids, a.name FROM files_archives a JOIN users u ON u.id = a.user_id
		WHERE a.token_hash = $1 AND a.expires_at > now() AND u.status IN ('active', 'suspended')`, hash[:]).Scan(&me, &orgID, &ids, &name)
	if err != nil {
		if !db.IsNoRows(err) {
			slog.ErrorContext(ctx, "files: archive lookup", "err", err)
		}
		httpx.Fail(w, r, httpx.NewError(http.StatusNotFound, "archive_expired", "This download link expired. Start the download again."))
		return
	}
	type file struct {
		path string
		item *Item
		key  string
	}
	var files []file
	var dirs []string
	var total int64
	used := map[string]bool{}
	unique := func(p string) string {
		if !used[strings.ToLower(p)] {
			used[strings.ToLower(p)] = true
			return p
		}
		for n := 1; ; n++ {
			c := numberedName(p, n, true)
			if !used[strings.ToLower(c)] {
				used[strings.ToLower(c)] = true
				return c
			}
		}
	}
	for _, id := range ids {
		it, err := s.loadLive(ctx, s.d.Pool, me, id, access.View)
		if err != nil || it.OrgID != orgID {
			continue
		}
		switch it.Type {
		case TypeFile:
			files = append(files, file{path: unique(it.Name), item: it})
		case TypeFolder:
			root := unique(it.Name)
			dirs = append(dirs, root+"/")
			entries, err := s.walk(ctx, s.d.Pool, me, false, it.ID)
			if err != nil {
				httpx.Fail(w, r, err)
				return
			}
			for _, e := range entries {
				switch e.Type {
				case TypeFolder:
					dirs = append(dirs, root+"/"+e.Path+"/")
				case TypeFile:
					files = append(files, file{path: root + "/" + e.Path, item: &Item{ID: e.ID, Type: TypeFile, Name: e.Name, VersionID: e.VersionID, Size: e.Size, UpdatedAt: e.UpdatedAt}})
				}
			}
		}
		if len(files) > maxArchiveItems {
			httpx.Fail(w, r, httpx.BadRequest("Zip downloads hold up to 2,000 files. Choose fewer."))
			return
		}
	}
	for i := range files {
		total += files[i].item.Size
		if files[i].item.VersionID == nil {
			continue
		}
		key, err := s.versionKey(ctx, s.d.Pool, files[i].item.ID, *files[i].item.VersionID)
		if err != nil {
			continue
		}
		files[i].key = key
	}
	if total > maxArchiveBytes {
		httpx.Fail(w, r, httpx.BadRequest("Zip downloads hold up to 4 GB. Choose fewer items."))
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name + ".zip"}))
	w.Header().Set("Cache-Control", "no-store")
	zw := zip.NewWriter(w)
	buf := make([]byte, 256<<10)
	for _, d := range dirs {
		if _, err := zw.Create(d); err != nil {
			return
		}
	}
	for _, f := range files {
		if f.key == "" {
			continue
		}
		hdr := &zip.FileHeader{Name: f.path, Method: zip.Deflate, Modified: f.item.UpdatedAt}
		dst, err := zw.CreateHeader(hdr)
		if err != nil {
			return
		}
		rc, err := s.d.Storage.Open(ctx, f.key)
		if err != nil {
			slog.WarnContext(ctx, "files: archive open", "item_id", f.item.ID, "err", err)
			continue
		}
		_, err = io.CopyBuffer(dst, rc, buf)
		rc.Close()
		if err != nil {
			return // the client went away
		}
	}
	_ = zw.Close()
}
