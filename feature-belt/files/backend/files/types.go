package files

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
)

// Built-in item types. Others come from RegisterItemType.
const (
	TypeFolder = "folder"
	TypeFile   = "file"
)

// Drives an item can live in.
const (
	DriveMy     = "my"     // a person's My drive (owner_id is that person)
	DriveShared = "shared" // an org-owned shared folder tree (no owner)
)

// Person is a user shown by name. Name is "" for a deleted account.
type Person struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	AvatarURL *string   `json:"avatar_url"`
}

// TypeInfo describes a registered item type on items of that type.
type TypeInfo struct {
	Type  string `json:"type"`
	Title string `json:"title"`
	Icon  string `json:"icon"`
}

// Item is one drive entry as the caller sees it. JSON matches
// packages/shared/src/files/types.ts (FilesItem).
type Item struct {
	ID       uuid.UUID  `json:"id"`
	OrgID    uuid.UUID  `json:"org_id"`
	ParentID *uuid.UUID `json:"parent_id"`
	Type     string     `json:"type"` // folder | file | a registered type
	Name     string     `json:"name"`
	Drive    string     `json:"drive"`    // my | shared
	OwnerID  *uuid.UUID `json:"owner_id"` // whose My drive; null for org-owned items
	// Files: the current version.
	MimeType     string     `json:"mime_type"`
	Size         int64      `json:"size"`
	TextLike     bool       `json:"text_like"`
	VersionID    *uuid.UUID `json:"version_id"`
	VersionCount int        `json:"version_count"`
	// Preview is how clients show it: folder, image, pdf, video, audio,
	// text, markdown, code, csv, svg, office, other, or the registered type.
	Preview   string    `json:"preview"`
	CreatedBy *Person   `json:"created_by"`
	UpdatedBy *Person   `json:"updated_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	// The caller's standing.
	MyLevel access.Level `json:"my_level"`
	Starred bool         `json:"starred"`
	// Shared: someone besides the owner has access here (a grant, general
	// access or a working public link on this item itself).
	Shared         bool       `json:"shared"`
	PublicLink     bool       `json:"public_link"`
	PublicDisabled bool       `json:"public_disabled"`
	TrashedAt      *time.Time `json:"trashed_at"`
	TrashedBy      *Person    `json:"trashed_by"` // null in the trash: a deleted account's private item
	TypeInfo       *TypeInfo  `json:"type_info"`
	Link           string     `json:"link"`      // app path that opens it
	ThumbURL       *string    `json:"thumb_url"` // images, when the list asked for thumbs
}

// IsFolder reports whether the item holds other items.
func (it *Item) IsFolder() bool { return it.Type == TypeFolder }

// Crumb is one ancestor in an item's path.
type Crumb struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	Type string    `json:"type"`
}

// ItemDetail is GET /v1/files/items/{id}: the item, the part of its path the
// caller can open (top first), and where that path starts: my (My drive),
// shared (Shared folders) or shared_with_me.
type ItemDetail struct {
	Item *Item   `json:"item"`
	Path []Crumb `json:"path"`
	Root string  `json:"root"`
}

// Version is one stored version of a file.
type Version struct {
	ID         uuid.UUID `json:"id"`
	Number     int       `json:"number"`
	Size       int64     `json:"size"`
	MimeType   string    `json:"mime_type"`
	UploadedBy *Person   `json:"uploaded_by"`
	Note       string    `json:"note"`
	Current    bool      `json:"current"`
	CreatedAt  time.Time `json:"created_at"`
}

// Activity is one event on an item (or, for a folder, on a child).
type Activity struct {
	ID        uuid.UUID      `json:"id"`
	ItemID    uuid.UUID      `json:"item_id"`
	ItemName  string         `json:"item_name"`
	Action    string         `json:"action"` // created uploaded version version_restored edited renamed moved copied trashed restored shared link_created
	Actor     *Person        `json:"actor"`
	Data      map[string]any `json:"data"`
	CreatedAt time.Time      `json:"created_at"`
}

// DriveInfo is GET /v1/files/orgs/{orgID}/drive.
type DriveInfo struct {
	OrgID           uuid.UUID     `json:"org_id"`
	UsedBytes       int64         `json:"used_bytes"`
	QuotaBytes      int64         `json:"quota_bytes"` // 0: no quota
	MaxUploadBytes  int64         `json:"max_upload_bytes"`
	TrashDays       int           `json:"trash_days"`
	CanCreateShared bool          `json:"can_create_shared"`
	Types           []TypeSummary `json:"types"`
}

// Errors shared across handlers.
var (
	errItemNotFound    = httpx.NotFound("Item not found.")
	errNotFolder       = httpx.BadRequest("That isn't a folder.")
	errNotFile         = httpx.BadRequest("That isn't a file.")
	errTrashed         = httpx.NewError(http.StatusConflict, "item_trashed", "This item is in the trash. Restore it first.")
	errNotTrashed      = httpx.NewError(http.StatusConflict, "item_not_trashed", "Move it to the trash first.")
	errNameTaken       = httpx.NewError(http.StatusConflict, "name_taken", "Something with that name is already there. Choose another name.")
	errNotText         = httpx.NewError(http.StatusUnsupportedMediaType, "not_text", "This file isn't text, so it can't be shown here. Download it instead.")
	errCrossDrive      = httpx.NewError(http.StatusForbidden, "manage_required", "Only people who manage this item can move it to another drive.")
	errAgentCredential = httpx.NewError(http.StatusUnauthorized, "agent_credential", "This credential is for AI agents. It works only at the agent endpoint.")
)

func errQuota() error {
	return httpx.NewError(http.StatusInsufficientStorage, "quota_exceeded",
		"Your "+orgs.Noun()+"'s storage is full. Delete files or empty the trash, then try again.")
}
