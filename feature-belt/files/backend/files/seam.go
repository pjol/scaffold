package files

// The seam other features build on (the repos feature first): register an
// item type, create and read drive items of it, and read a folder's files.
// Items of every type are access resources of kind files.item with their
// folder as parent, so sharing and agent scopes inherit from the folder.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// ItemType is a kind of drive item another feature owns. Register it from
// that feature's New. Its items show in folder listings, search, Recent and
// Starred with Icon and Title, take the shared ⋮ menu, move, rename, star,
// trash and share like files, and open Link(id).
type ItemType struct {
	// Type is lowercase letters, digits and underscores: "repo".
	Type string
	// Title is the singular name in copy: "Repository".
	Title string
	// Icon is a lucide icon name: "git-branch".
	Icon string
	// Link returns the app path that opens an item: "/repos/" + id.
	Link func(itemID uuid.UUID) string
	// CreatePath is the app path of the type's create flow; "{folder}" is
	// replaced with the folder's id ("" at the top of My drive):
	// "/repos/new?folder={folder}". Empty: not offered under New.
	CreatePath string
	// ConvertPath is where a folder's ⋮ menu "Turn into <title>" goes, with
	// "{folder}" replaced: "/repos/new?from={folder}". Empty: not offered.
	ConvertPath string
	// OnDelete runs in the purge transaction before the item's row goes
	// (required): delete what the feature keeps for it.
	OnDelete func(ctx context.Context, tx pgx.Tx, itemID uuid.UUID) error
	// OnTrash and OnRestore run in the transaction that trashes or restores
	// the item (optional).
	OnTrash   func(ctx context.Context, tx pgx.Tx, itemID uuid.UUID) error
	OnRestore func(ctx context.Context, tx pgx.Tx, itemID uuid.UUID) error
}

// TypeSummary is a registered type as clients see it (GET /v1/files/types).
type TypeSummary struct {
	Type         string `json:"type"`
	Title        string `json:"title"`
	Icon         string `json:"icon"`
	CreatePath   string `json:"create_path"`
	CreateLabel  string `json:"create_label"`
	ConvertPath  string `json:"convert_path"`
	ConvertLabel string `json:"convert_label"`
}

var (
	typesMu   sync.RWMutex
	itemTypes = map[string]ItemType{}
	typeName  = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
)

// RegisterItemType adds an item type. It panics on an invalid or built-in
// type, or a missing Title, Link or OnDelete (programming errors).
func RegisterItemType(t ItemType) {
	if !typeName.MatchString(t.Type) || t.Type == TypeFolder || t.Type == TypeFile {
		panic("files: invalid item type " + t.Type)
	}
	if t.Title == "" || t.Link == nil || t.OnDelete == nil {
		panic("files: item type " + t.Type + " needs Title, Link and OnDelete")
	}
	if t.Icon == "" {
		t.Icon = "file"
	}
	typesMu.Lock()
	itemTypes[t.Type] = t
	typesMu.Unlock()
}

// ItemTypeOf returns a registered type.
func ItemTypeOf(typ string) (ItemType, bool) {
	typesMu.RLock()
	defer typesMu.RUnlock()
	t, ok := itemTypes[typ]
	return t, ok
}

func typeSummaries() []TypeSummary {
	typesMu.RLock()
	out := make([]TypeSummary, 0, len(itemTypes))
	for _, t := range itemTypes {
		title := strings.ToLower(t.Title)
		out = append(out, TypeSummary{Type: t.Type, Title: t.Title, Icon: t.Icon,
			CreatePath: t.CreatePath, CreateLabel: "New " + title, ConvertPath: t.ConvertPath, ConvertLabel: "Turn into " + title})
	}
	typesMu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Type < out[j].Type })
	return out
}

// itemLink is the app path that opens an item: the type's Link for
// registered types, /files/<id> for folders and files.
func itemLink(id uuid.UUID) string { return "/files/" + id.String() }

// ItemLink is the app path that opens a folder or file.
func ItemLink(id uuid.UUID) string { return itemLink(id) }

// NewItem is a drive entry another feature creates.
type NewItem struct {
	OrgID uuid.UUID
	// ParentID is the folder (nil: the top of CreatedBy's My drive). CreatedBy
	// needs edit on it.
	ParentID *uuid.UUID
	// Type is "folder" or a registered type.
	Type      string
	Name      string
	CreatedBy uuid.UUID
	// AutoRename picks "Name (1)" when the name is taken; otherwise a taken
	// name answers 409 name_taken.
	AutoRename bool
}

// CreateItem creates a drive entry of a registered type (or a folder) in the
// caller's transaction, with its access resource under the folder. It checks
// CreatedBy's org membership and edit on the folder (capped by the agent
// scope when ctx is an agent call) and answers httpx errors: 404 for a folder
// they can't see, 403 without edit, 409 for a taken name.
func CreateItem(ctx context.Context, tx pgx.Tx, n NewItem) (Item, error) {
	s := svc.Load()
	if s == nil {
		return Item{}, errors.New("files: not initialised")
	}
	if n.Type != TypeFolder {
		if _, ok := ItemTypeOf(n.Type); !ok {
			return Item{}, fmt.Errorf("files: item type %q not registered", n.Type)
		}
	}
	pl, err := s.placeFor(ctx, tx, n.CreatedBy, n.OrgID, n.ParentID)
	if err != nil {
		return Item{}, err
	}
	id, err := s.insertItem(ctx, tx, pl, n.Type, n.Name, n.CreatedBy, n.AutoRename)
	if err != nil {
		return Item{}, err
	}
	s.logActivity(ctx, tx, n.OrgID, id, pl.parentID(), n.CreatedBy, "created", map[string]any{"type": n.Type})
	it, err := s.getItem(ctx, tx, n.CreatedBy, id)
	if err != nil {
		return Item{}, err
	}
	return *it, nil
}

// GetItem returns an item as userID sees it, or 404 when they can't view it
// (agent-capped when ctx is an agent call). Trashed items are returned with
// TrashedAt set.
func GetItem(ctx context.Context, q db.Querier, userID, itemID uuid.UUID) (Item, error) {
	s := svc.Load()
	if s == nil {
		return Item{}, errors.New("files: not initialised")
	}
	it, err := s.load(ctx, q, userID, itemID, access.View)
	if err != nil {
		return Item{}, err
	}
	return *it, nil
}

// RenameItem renames an item userID can edit (409 name_taken on a clash).
func RenameItem(ctx context.Context, tx pgx.Tx, userID, itemID uuid.UUID, name string) (Item, error) {
	s := svc.Load()
	if s == nil {
		return Item{}, errors.New("files: not initialised")
	}
	it, err := s.load(ctx, tx, userID, itemID, access.Edit)
	if err != nil {
		return Item{}, err
	}
	out, err := s.rename(ctx, tx, userID, it, name)
	if err != nil {
		return Item{}, err
	}
	return *out, nil
}

// TrashItem moves an item userID can edit, and everything under it, to the
// trash (purged after FILES_TRASH_DAYS).
func TrashItem(ctx context.Context, tx pgx.Tx, userID, itemID uuid.UUID) error {
	s := svc.Load()
	if s == nil {
		return errors.New("files: not initialised")
	}
	it, err := s.load(ctx, tx, userID, itemID, access.Edit)
	if err != nil {
		return err
	}
	return s.trash(ctx, tx, userID, it)
}

// Entry is one item under a walked folder.
type Entry struct {
	ID        uuid.UUID
	ParentID  uuid.UUID
	Type      string
	Name      string
	Path      string // relative to the walked folder, "/"-separated: "docs/plan.md"
	Size      int64
	MimeType  string
	VersionID *uuid.UUID
	UpdatedAt time.Time
}

// maxWalk bounds WalkFolder.
const maxWalk = 50_000

// WalkFolder visits everything under a folder userID can view, parents
// before children, sorted by path. Subfolders they can't view are skipped
// with everything under them; trashed items are skipped. It answers 404 for
// a folder they can't view, and refuses folders over 50,000 items.
func WalkFolder(ctx context.Context, q db.Querier, userID, folderID uuid.UUID, fn func(Entry) error) error {
	s := svc.Load()
	if s == nil {
		return errors.New("files: not initialised")
	}
	folder, err := s.load(ctx, q, userID, folderID, access.View)
	if err != nil {
		return err
	}
	if !folder.IsFolder() {
		return errNotFolder
	}
	entries, err := s.walk(ctx, q, userID, access.ViaAgent(ctx), folderID)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := fn(e); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) walk(ctx context.Context, q db.Querier, userID uuid.UUID, viaAgent bool, folderID uuid.UUID) ([]Entry, error) {
	rows, err := q.Query(ctx, `
		WITH RECURSIVE t AS (
			SELECT i.id, i.parent_id, i.type, i.name, i.name AS path, 1 AS depth, i.size, i.mime_type, i.version_id, i.updated_at
			FROM files_items i
			WHERE i.parent_id = $3 AND i.trashed_at IS NULL AND access_level($1, 'files.item', i.id, $2) >= 1
			UNION ALL
			SELECT c.id, c.parent_id, c.type, c.name, t.path || '/' || c.name, t.depth + 1, c.size, c.mime_type, c.version_id, c.updated_at
			FROM t JOIN files_items c ON c.parent_id = t.id
			WHERE t.type = 'folder' AND t.depth < 64 AND c.trashed_at IS NULL AND access_level($1, 'files.item', c.id, $2) >= 1
		)
		SELECT id, parent_id, type, name, path, size, mime_type, version_id, updated_at FROM t ORDER BY path LIMIT $4`,
		userID, viaAgent, folderID, maxWalk+1)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Entry])
	if err != nil {
		return nil, err
	}
	if len(out) > maxWalk {
		return nil, httpx.NewError(http.StatusRequestEntityTooLarge, "folder_too_large", "This folder has more than 50,000 items. Choose a smaller folder.")
	}
	return out, nil
}

// OpenFile streams the current version of a file userID can view (404
// otherwise). The caller closes the reader.
func OpenFile(ctx context.Context, userID, itemID uuid.UUID) (io.ReadCloser, Item, error) {
	s := svc.Load()
	if s == nil {
		return nil, Item{}, errors.New("files: not initialised")
	}
	it, err := s.load(ctx, s.d.Pool, userID, itemID, access.View)
	if err != nil {
		return nil, Item{}, err
	}
	if it.Type != TypeFile || it.VersionID == nil {
		return nil, Item{}, errNotFile
	}
	key, err := s.versionKey(ctx, s.d.Pool, it.ID, *it.VersionID)
	if err != nil {
		return nil, Item{}, err
	}
	rc, err := s.d.Storage.Open(ctx, key)
	if err != nil {
		return nil, Item{}, err
	}
	return rc, *it, nil
}
