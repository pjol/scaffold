package files

// The read seam for link cards: what a viewer may see of a drive item
// without opening it, for other features' previews (channels' unfurlers,
// an assistant's context line) through the app's stitching.

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
)

// LinkedItem is a drive item as one viewer may see it from a link.
type LinkedItem struct {
	ID        uuid.UUID    `json:"id"`
	OrgID     uuid.UUID    `json:"org_id"`
	Type      string       `json:"type"` // folder | file | link | a registered type (repo)
	Name      string       `json:"name"`
	Preview   string       `json:"preview"`   // how clients show it, as Item.Preview: folder, image, pdf, text…
	MimeType  string       `json:"mime_type"` // files only
	Size      int64        `json:"size"`      // files only: bytes of the current version
	Icon      string       `json:"icon"`      // a lucide name, the one search results use
	Context   string       `json:"context"`   // org · folder when the viewer can open the folder, else the drive section
	Snippet   string       `json:"snippet"`   // the first line of a text file or extracted document text, ≤ 140 characters; often ""
	Path      string       `json:"path"`      // app path that opens it: /files/<id>, or a registered type's Link
	Level     access.Level `json:"level"`     // the viewer's level, agent-capped
	UpdatedAt time.Time    `json:"updated_at"`
}

// ErrItemNotFound is ItemRef's answer when the item doesn't exist, is in
// the trash, or the viewer can't open it; the three look the same.
var ErrItemNotFound = errItemNotFound

// ItemRef reads a drive item as viewer with the check of GET
// /files/items/{id}: access_level on files.item (sharing, inheritance from
// folders, workspace membership), capped by the viewer's AI agent scope when
// ctx is an agent call (access.ViaAgent). Trashed items are not found.
// Nothing is recorded (no Recent entry, no view). The folder is named only
// when the viewer can open it too.
func ItemRef(ctx context.Context, q db.Querier, viewer *auth.Identity, itemID uuid.UUID) (*LinkedItem, error) {
	if viewer == nil || viewer.UserID == uuid.Nil || itemID == uuid.Nil {
		return nil, ErrItemNotFound
	}
	lvl, err := access.LevelOf(ctx, q, viewer.UserID, Kind, itemID)
	if err != nil {
		return nil, err
	}
	if lvl < access.View {
		return nil, ErrItemNotFound
	}
	var (
		it        Item
		trashedAt *time.Time
		orgName   string
		folder    *string
		folderLvl int
		text      *string
	)
	err = q.QueryRow(ctx, `
		SELECT i.id, i.org_id, i.parent_id, i.type, i.name, i.drive, i.owner_id, i.mime_type, i.size, i.text_like,
		       i.updated_at, i.trashed_at, o.name, p.name,
		       CASE WHEN p.id IS NULL THEN 0 ELSE access_level($1, 'files.item', p.id, $2) END,
		       left(i.content_text, 2000)
		FROM files_items i
		JOIN orgs o ON o.id = i.org_id
		LEFT JOIN files_items p ON p.id = i.parent_id
		WHERE i.id = $3`, viewer.UserID, access.ViaAgent(ctx), itemID).
		Scan(&it.ID, &it.OrgID, &it.ParentID, &it.Type, &it.Name, &it.Drive, &it.OwnerID, &it.MimeType, &it.Size, &it.TextLike,
			&it.UpdatedAt, &trashedAt, &orgName, &folder, &folderLvl, &text)
	if db.IsNoRows(err) {
		return nil, ErrItemNotFound
	}
	if err != nil {
		return nil, err
	}
	if trashedAt != nil {
		return nil, ErrItemNotFound
	}
	finishItem(&it)
	out := &LinkedItem{
		ID: it.ID, OrgID: it.OrgID, Type: it.Type, Name: it.Name, Preview: it.Preview,
		Icon: searchIcon(&it), Path: it.Link, Level: lvl, UpdatedAt: it.UpdatedAt,
	}
	if it.Type == TypeFile {
		out.MimeType, out.Size = it.MimeType, it.Size
	}
	name := ""
	if folder != nil {
		name = *folder
	}
	out.Context = app.SearchContext(orgName, placeOf(&it, viewer.UserID, name, folderLvl >= int(access.View)))
	if text != nil && it.Type == TypeFile {
		out.Snippet = firstLine(*text, 140)
	}
	return out, nil
}

// ItemIDFromPath reads the item id from an app path an item link opens
// ("/files/<id>", with or without a trailing slash). Drive sections
// (/files/recent, /files/trash…) and connected drives (/files/c/…) answer
// false.
func ItemIDFromPath(path string) (uuid.UUID, bool) {
	rest, ok := strings.CutPrefix(path, "/files/")
	if !ok {
		return uuid.Nil, false
	}
	rest = strings.TrimSuffix(rest, "/")
	if strings.Contains(rest, "/") {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(rest)
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

// placeOf says where an item lives for me: the folder's name when they can
// open the folder, else the drive section the drive shows it under (My
// drive, Shared folders, Shared with me). Search and link cards use it.
func placeOf(it *Item, me uuid.UUID, folder string, canOpenFolder bool) string {
	switch {
	case it.ParentID != nil && canOpenFolder:
		return folder
	case it.ParentID == nil && it.Drive == DriveShared:
		return "Shared folders"
	case it.ParentID == nil && it.OwnerID != nil && *it.OwnerID == me:
		return "My drive"
	}
	return "Shared with me"
}

// searchIcon is the lucide icon search results and link cards show for an
// item.
func searchIcon(it *Item) string {
	switch {
	case it.IsFolder():
		return "folder"
	case it.TypeInfo != nil:
		return it.TypeInfo.Icon
	case it.Preview == "image":
		return "image"
	case it.Preview == "video":
		return "video"
	case it.Preview == "text" || it.Preview == "markdown" || it.Preview == "pdf" || it.Preview == "office":
		return "file-text"
	}
	return "file"
}

// firstLine is the first non-blank line of text, cut to max characters.
func firstLine(text string, max int) string {
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.Join(strings.Fields(line), " ")
		if line == "" {
			continue
		}
		if utf8.RuneCountInString(line) > max {
			line = string([]rune(line)[:max-1]) + "…"
		}
		return line
	}
	return ""
}
