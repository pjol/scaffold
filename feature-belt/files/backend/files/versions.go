package files

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/storage"
)

// newVersion is an object that becomes a file's current version.
type newVersion struct {
	key      string
	size     int64
	mime     string
	textLike bool
	content  *string // text indexed for search (nil: none)
	note     string
}

// reserve holds the org's quota lock for the transaction and refuses add
// bytes that don't fit.
func (s *Service) reserve(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, add int64) error {
	if s.cfg.quotaBytes == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('files.quota:' || $1::text, 0))`, orgID); err != nil {
		return err
	}
	used, err := s.usedBytes(ctx, tx, orgID)
	if err != nil {
		return err
	}
	if used+add > s.cfg.quotaBytes {
		return errQuota()
	}
	return nil
}

// addVersion makes v the file's current version and keeps the newest 100.
func (s *Service) addVersion(ctx context.Context, tx pgx.Tx, itemID, orgID, by uuid.UUID, v newVersion) error {
	// Lock the item so concurrent uploads number their versions one after another.
	if _, err := tx.Exec(ctx, `SELECT 1 FROM files_items WHERE id = $1 FOR UPDATE`, itemID); err != nil {
		return err
	}
	var number int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max(number), 0) + 1 FROM files_versions WHERE item_id = $1`, itemID).Scan(&number); err != nil {
		return err
	}
	var vid uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO files_versions (item_id, org_id, number, storage_key, size, mime_type, text_like, uploaded_by, note)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
		itemID, orgID, number, v.key, v.size, v.mime, v.textLike, nullID(by), v.note).Scan(&vid); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM files_versions WHERE item_id = $1 AND id <> $2 AND number <= $3`, itemID, vid, number-maxVersions); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		UPDATE files_items SET version_id = $2, mime_type = $3, size = $4, text_like = $5, content_text = $6,
		       version_count = (SELECT count(*) FROM files_versions WHERE item_id = $1), updated_at = now(), updated_by = $7
		WHERE id = $1`, itemID, vid, v.mime, v.size, v.textLike, v.content, nullID(by))
	return err
}

// indexText reads the start of a small text file for search.
func (s *Service) indexText(ctx context.Context, key string, size int64, textLike bool) *string {
	if !textLike || size > maxIndexedFile {
		return nil
	}
	b, err := s.d.Storage.ReadPrefix(ctx, key, maxIndexedText)
	if err != nil {
		slog.WarnContext(ctx, "files: index text", "err", err)
		return nil
	}
	b, _ = truncateUTF8(b, maxIndexedText)
	t := string(b)
	return &t
}

// queueDeletion drops objects written for a transaction that failed.
func (s *Service) queueDeletion(ctx context.Context, keys ...string) {
	for _, k := range keys {
		if _, err := s.d.Pool.Exec(context.WithoutCancel(ctx), `INSERT INTO files_object_deletions (storage_key) VALUES ($1) ON CONFLICT DO NOTHING`, k); err != nil {
			slog.WarnContext(ctx, "files: queue object deletion", "err", err)
		}
	}
}

// ---- versions --------------------------------------------------------------------

// listVersions: GET /v1/files/items/{id}/versions (view) → {items}, newest first.
func (s *Service) listVersions(w http.ResponseWriter, r *http.Request) error {
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
	if it.Type != TypeFile {
		return errNotFile
	}
	list, err := s.versions(ctx, s.d.Pool, id)
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": list})
	return nil
}

func (s *Service) versions(ctx context.Context, q db.Querier, itemID uuid.UUID) ([]Version, error) {
	rows, err := q.Query(ctx, `
		SELECT v.id, v.number, v.size, v.mime_type, v.uploaded_by, u.display_name, u.avatar_url, v.note, v.id = i.version_id, v.created_at
		FROM files_versions v JOIN files_items i ON i.id = v.item_id LEFT JOIN users u ON u.id = v.uploaded_by
		WHERE v.item_id = $1 ORDER BY v.number DESC LIMIT $2`, itemID, maxVersions)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Version{}
	for rows.Next() {
		var v Version
		var by *uuid.UUID
		var name, avatar *string
		if err := rows.Scan(&v.ID, &v.Number, &v.Size, &v.MimeType, &by, &name, &avatar, &v.Note, &v.Current, &v.CreatedAt); err != nil {
			return nil, err
		}
		v.UploadedBy = person(by, name, avatar)
		out = append(out, v)
	}
	return out, rows.Err()
}

// restoreVersion: POST /v1/files/items/{id}/versions/{versionID}/restore
// (edit) — a copy of that version becomes the newest.
func (s *Service) restoreVersion(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := idParam(r)
	if err != nil {
		return err
	}
	vid, err := uuid.Parse(chi.URLParam(r, "versionID"))
	if err != nil {
		return httpx.NotFound("Version not found.")
	}
	it, err := s.loadLive(ctx, s.d.Pool, me, id, access.Edit)
	if err != nil {
		return err
	}
	if it.Type != TypeFile {
		return errNotFile
	}
	if it.VersionID != nil && *it.VersionID == vid {
		httpx.OK(w, it)
		return nil
	}
	var v struct {
		number   int
		key, mt  string
		size     int64
		textLike bool
	}
	err = s.d.Pool.QueryRow(ctx, `SELECT number, storage_key, mime_type, size, text_like FROM files_versions WHERE id = $1 AND item_id = $2`,
		vid, id).Scan(&v.number, &v.key, &v.mt, &v.size, &v.textLike)
	if db.IsNoRows(err) {
		return httpx.NotFound("Version not found.")
	}
	if err != nil {
		return err
	}
	key, err := storage.NewKey(Purpose, it.OrgID, v.mt)
	if err != nil {
		return err
	}
	if err := s.d.Storage.Copy(ctx, v.key, key); err != nil {
		return err
	}
	var out *Item
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if err := s.reserve(ctx, tx, it.OrgID, v.size); err != nil {
			return err
		}
		nv := newVersion{key: key, size: v.size, mime: v.mt, textLike: v.textLike, content: s.indexText(ctx, key, v.size, v.textLike),
			note: fmt.Sprintf("Restored from version %d", v.number)}
		if err := s.addVersion(ctx, tx, id, it.OrgID, me, nv); err != nil {
			return err
		}
		s.logActivity(ctx, tx, it.OrgID, id, it.ParentID, me, "version_restored", map[string]any{"number": v.number})
		out, err = s.getItem(ctx, tx, me, id)
		return err
	})
	if err != nil {
		s.queueDeletion(ctx, key)
		return err
	}
	s.touchRecent(ctx, me, out, "edited")
	s.changed(ctx, out, "version")
	httpx.OK(w, out)
	return nil
}

// ---- text files ----------------------------------------------------------------------

// createText writes a new text file (UI "new text file", empty uploads and
// the agent tool).
func (s *Service) createText(ctx context.Context, me, orgID uuid.UUID, parentID *uuid.UUID, name, text, declared string, rename bool) (*Item, error) {
	clean, err := cleanName(name)
	if err != nil {
		return nil, err
	}
	if len(text) > maxTextEdit {
		return nil, httpx.BadRequest("Text files made here can be up to 1 MB. Upload larger files instead.")
	}
	body := []byte(text)
	mt, textLike, err := classify(declared, clean, body)
	if err != nil {
		return nil, err
	}
	if !textLike {
		return nil, httpx.BadRequest("That text isn't valid UTF-8.")
	}
	if orgID == uuid.Nil && parentID != nil {
		// The agent tool names only the folder; objects are keyed by its org.
		err := s.d.Pool.QueryRow(ctx, `SELECT org_id FROM files_items WHERE id = $1`, *parentID).Scan(&orgID)
		if db.IsNoRows(err) {
			return nil, errItemNotFound
		}
		if err != nil {
			return nil, err
		}
	}
	key, err := storage.NewKey(Purpose, orgID, mt)
	if err != nil {
		return nil, err
	}
	if err := s.d.Storage.Put(ctx, key, mt, body); err != nil {
		return nil, err
	}
	var out *Item
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		pl, err := s.placeFor(ctx, tx, me, orgID, parentID)
		if err != nil {
			return err
		}
		if err := s.reserve(ctx, tx, pl.orgID, int64(len(body))); err != nil {
			return err
		}
		id, err := s.insertItem(ctx, tx, pl, TypeFile, clean, me, rename)
		if err != nil {
			return err
		}
		content, _ := truncateUTF8(body, maxIndexedText)
		ct := string(content)
		if err := s.addVersion(ctx, tx, id, pl.orgID, me, newVersion{key: key, size: int64(len(body)), mime: mt, textLike: true, content: &ct}); err != nil {
			return err
		}
		s.logActivity(ctx, tx, pl.orgID, id, pl.parentID(), me, "created", map[string]any{"type": TypeFile})
		out, err = s.getItem(ctx, tx, me, id)
		return err
	})
	if err != nil {
		s.queueDeletion(ctx, key)
		return nil, err
	}
	s.touchRecent(ctx, me, out, "edited")
	s.changed(ctx, out, "created")
	return out, nil
}

// readText reads a text file's version (current when versionID is nil), up
// to limit bytes.
func (s *Service) readText(ctx context.Context, it *Item, versionID *uuid.UUID, limit int) (text []byte, truncated bool, err error) {
	if it.Type != TypeFile || it.VersionID == nil {
		return nil, false, errNotFile
	}
	vid := *it.VersionID
	if versionID != nil {
		vid = *versionID
	}
	var key string
	var textLike bool
	err = s.d.Pool.QueryRow(ctx, `SELECT storage_key, text_like FROM files_versions WHERE id = $1 AND item_id = $2`, vid, it.ID).Scan(&key, &textLike)
	if db.IsNoRows(err) {
		return nil, false, httpx.NotFound("Version not found.")
	}
	if err != nil {
		return nil, false, err
	}
	if !textLike {
		return nil, false, errNotText
	}
	b, err := s.d.Storage.ReadPrefix(ctx, key, int64(limit)+1)
	if err != nil {
		return nil, false, err
	}
	b, truncated = truncateUTF8(b, limit)
	return b, truncated, nil
}

// lineEdit is a planned replacement of a line range in a text file.
type lineEdit struct {
	item          *Item
	doc           textDoc
	before, after []string
	start, end    int
}

// planLineEdit loads a text file the caller can edit and computes the edit
// without writing (the tool's Preview and handler share it). With a base
// version the file must still be at it.
func (s *Service) planLineEdit(ctx context.Context, me, itemID uuid.UUID, start, end int, text string, base *uuid.UUID, min access.Level) (*lineEdit, error) {
	it, err := s.loadLive(ctx, s.d.Pool, me, itemID, min)
	if err != nil {
		return nil, err
	}
	if it.Type != TypeFile {
		return nil, errNotFile
	}
	if !it.TextLike {
		return nil, errNotText
	}
	if it.Size > maxTextEdit {
		return nil, httpx.BadRequest("Only text files up to 1 MB can be edited line by line.")
	}
	if base != nil && (it.VersionID == nil || *it.VersionID != *base) {
		return nil, httpx.NewError(http.StatusConflict, "target_changed", "The file changed since that version. Read it again.")
	}
	key, err := s.versionKey(ctx, s.d.Pool, it.ID, *it.VersionID)
	if err != nil {
		return nil, err
	}
	rc, err := s.d.Storage.Open(ctx, key)
	if err != nil {
		return nil, err
	}
	b, err := io.ReadAll(io.LimitReader(rc, maxTextEdit+1))
	rc.Close()
	if err != nil {
		return nil, err
	}
	doc, before, after, err := parseText(b).replaceLines(start, end, text)
	if err != nil {
		return nil, err
	}
	return &lineEdit{item: it, doc: doc, before: before, after: after, start: start, end: end}, nil
}

// applyLineEdit writes a planned edit as a new version, if the file hasn't
// changed since it was planned.
func (s *Service) applyLineEdit(ctx context.Context, me uuid.UUID, e *lineEdit) (*Item, error) {
	it := e.item
	body := e.doc.bytes()
	if len(body) > maxTextEdit {
		return nil, httpx.BadRequest("The edited file would be over 1 MB.")
	}
	key, err := storage.NewKey(Purpose, it.OrgID, it.MimeType)
	if err != nil {
		return nil, err
	}
	if err := s.d.Storage.Put(ctx, key, it.MimeType, body); err != nil {
		return nil, err
	}
	var out *Item
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var current *uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT version_id FROM files_items WHERE id = $1 FOR UPDATE`, it.ID).Scan(&current); err != nil {
			return err
		}
		if !eqID(current, it.VersionID) {
			return httpx.NewError(http.StatusConflict, "target_changed", "The file changed while editing. Read it again.")
		}
		if err := s.reserve(ctx, tx, it.OrgID, int64(len(body))); err != nil {
			return err
		}
		content, _ := truncateUTF8(body, maxIndexedText)
		ct := string(content)
		note := lineRangeNote(e.start, e.end)
		if a, ok := auth.AgentFrom(ctx); ok {
			note += " by " + a.Client
		}
		if err := s.addVersion(ctx, tx, it.ID, it.OrgID, me, newVersion{key: key, size: int64(len(body)), mime: it.MimeType, textLike: true, content: &ct, note: note}); err != nil {
			return err
		}
		s.logActivity(ctx, tx, it.OrgID, it.ID, it.ParentID, me, "edited", map[string]any{
			"start_line": e.start, "end_line": e.end, "removed": len(e.before), "added": len(e.after)})
		out, err = s.getItem(ctx, tx, me, it.ID)
		return err
	})
	if err != nil {
		s.queueDeletion(ctx, key)
		return nil, err
	}
	s.touchRecent(ctx, me, out, "edited")
	s.changed(ctx, out, "version")
	return out, nil
}

func lineRangeNote(start, end int) string {
	switch {
	case end < start:
		return fmt.Sprintf("Inserted before line %d", start)
	case end == start:
		return fmt.Sprintf("Edited line %d", start)
	}
	return fmt.Sprintf("Edited lines %d–%d", start, end)
}

// ---- copy ------------------------------------------------------------------------

// copyItem copies a file, or a folder with everything in it the caller can
// see (up to 500 items), into a folder they can edit (nil: their My drive).
// Objects are copied first; a failed transaction queues them for deletion.
func (s *Service) copyItem(ctx context.Context, me uuid.UUID, src *Item, dest *uuid.UUID, name string) (*Item, error) {
	if src.Type != TypeFile && src.Type != TypeFolder {
		title := src.Type
		if src.TypeInfo != nil {
			title = src.TypeInfo.Title
		}
		return nil, httpx.BadRequest(title + " items can't be copied.")
	}
	if strings.TrimSpace(name) == "" {
		name = src.Name
	}
	var entries []Entry
	if src.IsFolder() {
		var err error
		if entries, err = s.walk(ctx, s.d.Pool, me, access.ViaAgent(ctx), src.ID); err != nil {
			return nil, err
		}
		if len(entries)+1 > maxCopyItems {
			return nil, httpx.BadRequest("Folders with more than 500 items can't be copied at once.")
		}
		if dest != nil {
			for _, e := range entries {
				if e.ID == *dest {
					return nil, httpx.BadRequest("A folder can't be copied into itself.")
				}
			}
			if *dest == src.ID {
				return nil, httpx.BadRequest("A folder can't be copied into itself.")
			}
		}
	}
	// Copy every file's current object.
	type fileCopy struct {
		key, mt  string
		size     int64
		textLike bool
		content  *string
	}
	copies := map[uuid.UUID]fileCopy{}
	var newKeys []string
	var total int64
	copyOne := func(id uuid.UUID) error {
		var c fileCopy
		var srcKey string
		err := s.d.Pool.QueryRow(ctx, `
			SELECT v.storage_key, v.mime_type, v.size, v.text_like, i.content_text FROM files_items i JOIN files_versions v ON v.id = i.version_id
			WHERE i.id = $1`, id).Scan(&srcKey, &c.mt, &c.size, &c.textLike, &c.content)
		if db.IsNoRows(err) {
			return nil // a file without a version (never happens for uploads); skip it
		}
		if err != nil {
			return err
		}
		if c.key, err = storage.NewKey(Purpose, src.OrgID, c.mt); err != nil {
			return err
		}
		if err := s.d.Storage.Copy(ctx, srcKey, c.key); err != nil {
			return err
		}
		newKeys = append(newKeys, c.key)
		total += c.size
		copies[id] = c
		return nil
	}
	fail := func(err error) (*Item, error) {
		s.queueDeletion(ctx, newKeys...)
		return nil, err
	}
	if src.Type == TypeFile {
		if err := copyOne(src.ID); err != nil {
			return fail(err)
		}
	}
	for _, e := range entries {
		if e.Type == TypeFile {
			if err := copyOne(e.ID); err != nil {
				return fail(err)
			}
		}
	}
	var out *Item
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		pl, err := s.placeFor(ctx, tx, me, src.OrgID, dest)
		if err != nil {
			return err
		}
		if err := s.reserve(ctx, tx, src.OrgID, total); err != nil {
			return err
		}
		insert := func(pl *placement, old uuid.UUID, typ, name string, rename bool) (uuid.UUID, error) {
			id, err := s.insertItem(ctx, tx, pl, typ, name, me, rename)
			if err != nil {
				return uuid.Nil, err
			}
			if c, ok := copies[old]; ok {
				if err := s.addVersion(ctx, tx, id, src.OrgID, me, newVersion{key: c.key, size: c.size, mime: c.mt, textLike: c.textLike, content: c.content}); err != nil {
					return uuid.Nil, err
				}
			}
			return id, nil
		}
		rootID, err := insert(pl, src.ID, src.Type, name, true)
		if err != nil {
			return err
		}
		newIDs := map[uuid.UUID]*Item{src.ID: {ID: rootID, OrgID: pl.orgID, Drive: pl.drive, OwnerID: pl.owner}}
		for _, e := range entries {
			if e.Type != TypeFile && e.Type != TypeFolder {
				continue // registered types aren't copied
			}
			parent, ok := newIDs[e.ParentID]
			if !ok {
				continue
			}
			child := &placement{orgID: pl.orgID, parent: parent, drive: pl.drive, owner: pl.owner}
			id, err := insert(child, e.ID, e.Type, e.Name, true)
			if err != nil {
				return err
			}
			newIDs[e.ID] = &Item{ID: id, OrgID: pl.orgID, Drive: pl.drive, OwnerID: pl.owner}
		}
		s.logActivity(ctx, tx, pl.orgID, rootID, pl.parentID(), me, "copied", map[string]any{"from_id": src.ID, "from_name": src.Name})
		out, err = s.getItem(ctx, tx, me, rootID)
		return err
	})
	if err != nil {
		return fail(err)
	}
	return out, nil
}
