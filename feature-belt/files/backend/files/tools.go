package files

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
)

// Agent tools run as the caller through the same queries and access checks
// as the REST routes, with access.ViaAgent(ctx) so the caller's agent scope
// caps every row (an item whose scope is Off is invisible, with everything
// under it). Writes are single, reviewable changes with a Target and an exact
// Preview; there are no delete or sharing tools.

// toolItem is the compact item shape tools return.
type toolItem struct {
	ID         uuid.UUID  `json:"id"`
	Type       string     `json:"type"`
	Name       string     `json:"name"`
	ParentID   *uuid.UUID `json:"parent_id"`
	MimeType   string     `json:"mime_type,omitempty"`
	Size       int64      `json:"size,omitempty"`
	TextLike   bool       `json:"text,omitempty"`
	VersionID  *uuid.UUID `json:"version_id,omitempty"`
	UpdatedAt  time.Time  `json:"updated_at"`
	YourAccess string     `json:"your_access"`
	Link       string     `json:"link"`
	Snippet    string     `json:"snippet,omitempty"`
}

func toTool(it *Item) toolItem {
	return toolItem{ID: it.ID, Type: it.Type, Name: it.Name, ParentID: it.ParentID, MimeType: it.MimeType, Size: it.Size,
		TextLike: it.TextLike, VersionID: it.VersionID, UpdatedAt: it.UpdatedAt, YourAccess: it.MyLevel.String(), Link: it.Link}
}

func decode(input json.RawMessage, dst any) error {
	if len(input) == 0 {
		return nil
	}
	if err := json.Unmarshal(input, dst); err != nil {
		return httpx.BadRequest("Invalid input: " + err.Error())
	}
	return nil
}

// targetOf reads one id field from a tool's input for its Target. The kind
// comes back even without an id (agents ask with {} which kinds a tool acts
// on).
func targetOf(field string) func(json.RawMessage) (string, string) {
	return func(input json.RawMessage) (string, string) {
		var m map[string]any
		_ = json.Unmarshal(input, &m)
		v, _ := m[field].(string)
		return Kind, v
	}
}

// resolveOrg picks the org a tool works in: the given one (if the caller is
// a member) or their only org.
func (s *Service) resolveOrg(ctx context.Context, me uuid.UUID, orgID *uuid.UUID) (uuid.UUID, error) {
	rows, err := s.d.Pool.Query(ctx, `
		SELECT o.id, o.name FROM org_members m JOIN orgs o ON o.id = m.org_id WHERE m.user_id = $1 ORDER BY o.name`, me)
	if err != nil {
		return uuid.Nil, err
	}
	defer rows.Close()
	var names []string
	var only uuid.UUID
	count := 0
	for rows.Next() {
		var id uuid.UUID
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return uuid.Nil, err
		}
		if orgID != nil && id == *orgID {
			return id, nil
		}
		names = append(names, fmt.Sprintf("%s (%s)", name, id))
		only = id
		count++
	}
	if err := rows.Err(); err != nil {
		return uuid.Nil, err
	}
	switch {
	case orgID != nil || count == 0:
		return uuid.Nil, httpx.NotFound(orgs.NounTitle() + " not found.")
	case count == 1:
		return only, nil
	}
	return uuid.Nil, httpx.BadRequest("Pass org_id. The user belongs to: " + strings.Join(names, ", ") + ".")
}

func (s *Service) folderLabel(ctx context.Context, q db.Querier, me uuid.UUID, id *uuid.UUID) string {
	if id == nil {
		return "My drive"
	}
	var name string
	if err := q.QueryRow(ctx, `SELECT name FROM files_items WHERE id = $1`, *id).Scan(&name); err != nil {
		return "a folder"
	}
	return "“" + name + "”"
}

const (
	schemaSearch = `{"type":"object","properties":{
		"query":{"type":"string","description":"Words in the name, or in the text of small text files."},
		"org_id":{"type":"string","format":"uuid","description":"Limit to one organization."},
		"limit":{"type":"integer","minimum":1,"maximum":50}},"required":["query"],"additionalProperties":false}`
	schemaList = `{"type":"object","properties":{
		"folder_id":{"type":"string","format":"uuid","description":"List this folder. Leave out to list a section."},
		"section":{"type":"string","enum":["my","shared","shared_with_me","recent","starred"],"description":"Without folder_id: my (My drive, default), shared (shared folders), shared_with_me, recent or starred."},
		"org_id":{"type":"string","format":"uuid","description":"The organization, for sections (needed when the user has several)."},
		"cursor":{"type":"string"}},"additionalProperties":false}`
	schemaItem = `{"type":"object","properties":{"item_id":{"type":"string","format":"uuid"}},"required":["item_id"],"additionalProperties":false}`
	schemaRead = `{"type":"object","properties":{
		"item_id":{"type":"string","format":"uuid"},
		"start_line":{"type":"integer","minimum":1,"description":"First line to return (default 1)."},
		"end_line":{"type":"integer","minimum":1,"description":"Last line to return (default: as much as fits in 256 KB)."}},
		"required":["item_id"],"additionalProperties":false}`
	schemaCreate = `{"type":"object","properties":{
		"name":{"type":"string","description":"File name with extension, e.g. notes.md."},
		"text":{"type":"string","description":"The whole content (UTF-8, up to 1 MB)."},
		"folder_id":{"type":"string","format":"uuid","description":"The folder. Leave out for the top of the user's My drive."},
		"org_id":{"type":"string","format":"uuid","description":"Without folder_id: the organization (needed when the user has several)."}},
		"required":["name","text"],"additionalProperties":false}`
	schemaReplace = `{"type":"object","properties":{
		"item_id":{"type":"string","format":"uuid"},
		"start_line":{"type":"integer","minimum":1,"description":"First line to replace (1-based)."},
		"end_line":{"type":"integer","minimum":0,"description":"Last line to replace, inclusive. Use start_line - 1 to insert before start_line without replacing (start_line may be one past the last line to append)."},
		"text":{"type":"string","description":"The new lines. Empty removes the range. One trailing newline is ignored."},
		"base_version_id":{"type":"string","format":"uuid","description":"The version_id you read. The edit is refused if the file changed since."}},
		"required":["item_id","start_line","end_line","text"],"additionalProperties":false}`
	schemaRename = `{"type":"object","properties":{"item_id":{"type":"string","format":"uuid"},"name":{"type":"string"}},"required":["item_id","name"],"additionalProperties":false}`
	schemaMove   = `{"type":"object","properties":{
		"item_id":{"type":"string","format":"uuid"},
		"folder_id":{"type":["string","null"],"format":"uuid","description":"The destination folder; null for the top of the user's My drive."}},
		"required":["item_id","folder_id"],"additionalProperties":false}`
)

type replaceIn struct {
	ItemID        uuid.UUID  `json:"item_id"`
	StartLine     int        `json:"start_line"`
	EndLine       int        `json:"end_line"`
	Text          string     `json:"text"`
	BaseVersionID *uuid.UUID `json:"base_version_id"`
}

type createIn struct {
	Name     string     `json:"name"`
	Text     string     `json:"text"`
	FolderID *uuid.UUID `json:"folder_id"`
	OrgID    *uuid.UUID `json:"org_id"`
}

type renameIn struct {
	ItemID uuid.UUID `json:"item_id"`
	Name   string    `json:"name"`
}

type moveIn struct {
	ItemID   uuid.UUID  `json:"item_id"`
	FolderID *uuid.UUID `json:"folder_id"`
}

// Tools implements app.ToolProvider.
func (s *Service) Tools() []app.Tool {
	return []app.Tool{
		{
			Name:        "files_search",
			Description: "Search the user's drives for files and folders by name, or by the text inside small text files. Returns only items the user can open.",
			InputSchema: orgWorded(schemaSearch),
			ReadOnly:    true,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					Query string     `json:"query"`
					OrgID *uuid.UUID `json:"org_id"`
					Limit int        `json:"limit"`
				}
				if err := decode(input, &in); err != nil {
					return nil, err
				}
				if in.Limit <= 0 || in.Limit > 50 {
					in.Limit = 20
				}
				page, err := s.searchItems(ctx, s.d.Pool, caller.UserID, access.ViaAgent(ctx), in.OrgID, in.Query, "", in.Limit)
				if err != nil {
					return nil, err
				}
				out := make([]toolItem, 0, len(page.Items))
				for _, h := range page.Items {
					t := toTool(h.Item)
					t.Snippet = h.Snippet
					out = append(out, t)
				}
				return map[string]any{"items": out}, nil
			},
		},
		{
			Name:        "files_list_folder",
			Description: "List what a folder holds (folders first, by name), or a drive section: My drive, shared folders, shared with the user, recent or starred.",
			InputSchema: orgWorded(schemaList),
			ReadOnly:    true,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					FolderID *uuid.UUID `json:"folder_id"`
					Section  string     `json:"section"`
					OrgID    *uuid.UUID `json:"org_id"`
					Cursor   string     `json:"cursor"`
				}
				if err := decode(input, &in); err != nil {
					return nil, err
				}
				viaAgent := access.ViaAgent(ctx)
				o := listOpts{Sort: "name", Cursor: in.Cursor, Limit: 100}
				var page httpx.Page[*Item]
				out := map[string]any{}
				if in.FolderID != nil {
					folder, err := s.loadLive(ctx, s.d.Pool, caller.UserID, *in.FolderID, access.View)
					if err != nil {
						return nil, err
					}
					if !folder.IsFolder() {
						return nil, errNotFolder
					}
					if page, err = s.children(ctx, s.d.Pool, caller.UserID, viaAgent, folder.ID, o); err != nil {
						return nil, err
					}
					out["folder"] = toTool(folder)
				} else {
					section := in.Section
					if section == "" {
						section = SectionMy
					}
					if section == SectionTrash {
						return nil, httpx.BadRequest("section must be my, shared, shared_with_me, recent or starred.")
					}
					orgID, err := s.resolveOrg(ctx, caller.UserID, in.OrgID)
					if err != nil {
						return nil, err
					}
					if page, err = s.section(ctx, s.d.Pool, caller.UserID, viaAgent, orgID, section, o); err != nil {
						return nil, err
					}
					out["section"], out["org_id"] = section, orgID
				}
				items := make([]toolItem, 0, len(page.Items))
				for _, it := range page.Items {
					items = append(items, toTool(it))
				}
				out["items"], out["next_cursor"] = items, page.NextCursor
				return out, nil
			},
		},
		{
			Name:        "files_get_item",
			Description: "Get a file or folder's details: type, size, current version, the user's access, its folder path and how many versions it has.",
			InputSchema: json.RawMessage(schemaItem),
			ReadOnly:    true,
			Target:      targetOf("item_id"),
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					ItemID uuid.UUID `json:"item_id"`
				}
				if err := decode(input, &in); err != nil {
					return nil, err
				}
				it, err := s.load(ctx, s.d.Pool, caller.UserID, in.ItemID, access.View)
				if err != nil {
					return nil, err
				}
				path, root, err := s.path(ctx, s.d.Pool, caller.UserID, access.ViaAgent(ctx), it)
				if err != nil {
					return nil, err
				}
				names := make([]string, 0, len(path))
				for _, c := range path {
					names = append(names, c.Name)
				}
				return map[string]any{"item": toTool(it), "path": strings.Join(names, "/"), "starts_in": root,
					"version_count": it.VersionCount, "trashed": it.TrashedAt != nil, "preview": it.Preview}, nil
			},
		},
		{
			Name: "files_read_text",
			Description: "Read a text file (code, markdown, CSV, notes) with line numbers, up to 256 KB at a time. " +
				"Use start_line and end_line to read further, and pass version_id to files_replace_lines as base_version_id.",
			InputSchema: json.RawMessage(schemaRead),
			ReadOnly:    true,
			Target:      targetOf("item_id"),
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					ItemID    uuid.UUID `json:"item_id"`
					StartLine int       `json:"start_line"`
					EndLine   int       `json:"end_line"`
				}
				if err := decode(input, &in); err != nil {
					return nil, err
				}
				it, err := s.loadLive(ctx, s.d.Pool, caller.UserID, in.ItemID, access.View)
				if err != nil {
					return nil, err
				}
				if !it.TextLike {
					return nil, errNotText
				}
				b, truncated, err := s.readText(ctx, it, nil, maxTextEdit)
				if err != nil {
					return nil, err
				}
				doc := parseText(b)
				start := max(in.StartLine, 1)
				end := len(doc.lines)
				if in.EndLine > 0 && in.EndLine < end {
					end = in.EndLine
				}
				text := doc.numbered(start, end)
				if len(text) > maxAgentRead {
					cut, _ := truncateUTF8([]byte(text), maxAgentRead)
					text = string(cut)
					if i := strings.LastIndexByte(text, '\n'); i > 0 {
						text = text[:i+1]
					}
					end = start + strings.Count(text, "\n") - 1
					truncated = true
				}
				return map[string]any{"name": it.Name, "version_id": it.VersionID, "total_lines": len(doc.lines),
					"start_line": start, "end_line": end, "more": truncated || end < len(doc.lines), "text": text}, nil
			},
		},
		{
			Name:        "files_create_text_file",
			Description: "Create a new text file (up to 1 MB) in a folder the user can edit, or at the top of their My drive. A taken name answers an error.",
			InputSchema: orgWorded(schemaCreate),
			ReadOnly:    false,
			Target:      targetOf("folder_id"),
			Preview: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
				var in createIn
				if err := decode(input, &in); err != nil {
					return app.ToolPreview{}, err
				}
				if in.FolderID != nil {
					if _, err := s.loadLive(ctx, s.d.Pool, caller.UserID, *in.FolderID, access.View); err != nil {
						return app.ToolPreview{}, err
					}
				}
				pv := app.ToolPreview{Summary: "Create “" + strings.TrimSpace(in.Name) + "” in " + s.folderLabel(ctx, s.d.Pool, caller.UserID, in.FolderID), After: in.Text}
				if in.FolderID != nil {
					pv.Link = itemLink(*in.FolderID)
				}
				return pv, nil
			},
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in createIn
				if err := decode(input, &in); err != nil {
					return nil, err
				}
				orgID := uuid.Nil
				if in.FolderID == nil {
					var err error
					if orgID, err = s.resolveOrg(ctx, caller.UserID, in.OrgID); err != nil {
						return nil, err
					}
				}
				it, err := s.createText(ctx, caller.UserID, orgID, in.FolderID, in.Name, in.Text, "", false)
				if err != nil {
					return nil, err
				}
				return map[string]any{"item": toTool(it)}, nil
			},
		},
		{
			Name: "files_replace_lines",
			Description: "Replace one range of lines in a text file the user can edit, saving a new version. Read the file first with files_read_text " +
				"and pass its version_id as base_version_id; line numbers are 1-based and inclusive.",
			InputSchema: json.RawMessage(schemaReplace),
			ReadOnly:    false,
			Target:      targetOf("item_id"),
			Preview: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
				var in replaceIn
				if err := decode(input, &in); err != nil {
					return app.ToolPreview{}, err
				}
				e, err := s.planLineEdit(ctx, caller.UserID, in.ItemID, in.StartLine, in.EndLine, in.Text, in.BaseVersionID, access.View)
				if err != nil {
					return app.ToolPreview{}, err
				}
				return replacePreview(e), nil
			},
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in replaceIn
				if err := decode(input, &in); err != nil {
					return nil, err
				}
				e, err := s.planLineEdit(ctx, caller.UserID, in.ItemID, in.StartLine, in.EndLine, in.Text, in.BaseVersionID, access.Edit)
				if err != nil {
					return nil, err
				}
				it, err := s.applyLineEdit(ctx, caller.UserID, e)
				if err != nil {
					return nil, err
				}
				return map[string]any{"item": toTool(it), "replaced_lines": len(e.before), "inserted_lines": len(e.after),
					"total_lines": len(e.doc.lines)}, nil
			},
		},
		{
			Name:        "files_rename",
			Description: "Rename a file or folder the user can edit. A name another item in the same folder has answers an error.",
			InputSchema: json.RawMessage(schemaRename),
			ReadOnly:    false,
			Target:      targetOf("item_id"),
			Preview: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
				var in renameIn
				if err := decode(input, &in); err != nil {
					return app.ToolPreview{}, err
				}
				it, err := s.loadLive(ctx, s.d.Pool, caller.UserID, in.ItemID, access.View)
				if err != nil {
					return app.ToolPreview{}, err
				}
				name, err := cleanName(in.Name)
				if err != nil {
					return app.ToolPreview{}, err
				}
				return app.ToolPreview{Summary: "Rename “" + it.Name + "” to “" + name + "”", Before: it.Name, After: name, Link: it.Link}, nil
			},
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in renameIn
				if err := decode(input, &in); err != nil {
					return nil, err
				}
				var out *Item
				err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
					it, err := s.load(ctx, tx, caller.UserID, in.ItemID, access.Edit)
					if err != nil {
						return err
					}
					out, err = s.rename(ctx, tx, caller.UserID, it, in.Name)
					return err
				})
				if err != nil {
					return nil, err
				}
				s.changed(ctx, out, "renamed")
				return map[string]any{"item": toTool(out)}, nil
			},
		},
		{
			Name: "files_move",
			Description: "Move a file or folder into another folder in the same drive (the user needs edit on both). " +
				"Agents can't make moves that change who can open the item: into another drive, or into or out of a folder shared differently.",
			InputSchema: json.RawMessage(schemaMove),
			ReadOnly:    false,
			Target:      targetOf("item_id"),
			Preview: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
				var in moveIn
				if err := decode(input, &in); err != nil {
					return app.ToolPreview{}, err
				}
				it, err := s.loadLive(ctx, s.d.Pool, caller.UserID, in.ItemID, access.View)
				if err != nil {
					return app.ToolPreview{}, err
				}
				var dest *Item
				if in.FolderID != nil {
					if dest, err = s.loadLive(ctx, s.d.Pool, caller.UserID, *in.FolderID, access.View); err != nil {
						return app.ToolPreview{}, err
					}
				}
				// An agent can't even suggest a move that shares or unshares
				// the item (the refusal its Edit-level call would get).
				if err := s.agentMove(ctx, s.d.Pool, caller.UserID, it, dest); err != nil {
					return app.ToolPreview{}, err
				}
				from := s.pathText(ctx, caller.UserID, it.ParentID)
				to := s.pathText(ctx, caller.UserID, in.FolderID)
				return app.ToolPreview{Summary: "Move “" + it.Name + "” to " + s.folderLabel(ctx, s.d.Pool, caller.UserID, in.FolderID),
					Before: from + it.Name, After: to + it.Name, Link: it.Link}, nil
			},
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in moveIn
				if err := decode(input, &in); err != nil {
					return nil, err
				}
				before, after, err := s.moveOne(ctx, caller.UserID, in.ItemID, in.FolderID)
				if err != nil {
					return nil, err
				}
				s.afterMove(ctx, before, after)
				return map[string]any{"item": toTool(after)}, nil
			},
		},
	}
}

// replacePreview shows exactly the lines a replacement removes and adds.
func replacePreview(e *lineEdit) app.ToolPreview {
	var summary string
	switch {
	case e.end < e.start:
		summary = fmt.Sprintf("Insert %d line(s) before line %d of “%s”", len(e.after), e.start, e.item.Name)
	case e.end == e.start:
		summary = fmt.Sprintf("Replace line %d of “%s”", e.start, e.item.Name)
	default:
		summary = fmt.Sprintf("Replace lines %d–%d of “%s”", e.start, e.end, e.item.Name)
	}
	return app.ToolPreview{Summary: summary, Before: strings.Join(e.before, "\n"), After: strings.Join(e.after, "\n"), Link: e.item.Link}
}

// pathText is "Projects/Plans/" for a folder (with a trailing slash), or
// "My drive/" at the top.
func (s *Service) pathText(ctx context.Context, me uuid.UUID, folderID *uuid.UUID) string {
	if folderID == nil {
		return "My drive/"
	}
	it, err := s.getItem(ctx, s.d.Pool, me, *folderID)
	if err != nil {
		return ""
	}
	crumbs, _, err := s.path(ctx, s.d.Pool, me, access.ViaAgent(ctx), it)
	if err != nil {
		return ""
	}
	var b strings.Builder
	for _, c := range crumbs {
		b.WriteString(c.Name + "/")
	}
	b.WriteString(it.Name + "/")
	return b.String()
}

// Search implements app.SearchProvider with the same query as GET
// /files/orgs/{orgID}/search, over every org the caller is in (one org when
// the search is scoped: app.SearchOrg). Context is the org, then the folder
// the item is in when the caller can open it, else its drive section.
func (s *Service) Search(ctx context.Context, caller *auth.Identity, query string, limit int) ([]app.SearchResult, error) {
	var scope *uuid.UUID
	if id, ok := app.SearchOrg(ctx); ok {
		scope = &id
	}
	page, err := s.searchItems(ctx, s.d.Pool, caller.UserID, access.ViaAgent(ctx), scope, query, "", limit)
	if err != nil {
		return nil, err
	}
	places, err := s.searchPlaces(ctx, caller.UserID, page.Items)
	if err != nil {
		return nil, err
	}
	out := make([]app.SearchResult, 0, len(page.Items))
	for _, h := range page.Items {
		it := h.Item
		icon := "file"
		switch {
		case it.IsFolder():
			icon = "folder"
		case it.TypeInfo != nil:
			icon = it.TypeInfo.Icon
		case it.Preview == "image":
			icon = "image"
		case it.Preview == "video":
			icon = "video"
		case it.Preview == "text" || it.Preview == "markdown" || it.Preview == "pdf" || it.Preview == "office":
			icon = "file-text"
		}
		out = append(out, app.SearchResult{Kind: Kind, ID: it.ID.String(), Title: it.Name, Snippet: h.Snippet,
			Link: it.Link, Icon: icon, Score: h.Score, At: it.UpdatedAt, OrgID: it.OrgID.String(), Context: places[it.ID]})
	}
	return out, nil
}

// searchPlaces says where each hit lives for search results: "<org> · <folder>"
// when the caller can open the folder, else "<org> · My drive", "Shared
// folders" or "Shared with me" (the same sections the drive shows).
func (s *Service) searchPlaces(ctx context.Context, me uuid.UUID, hits []SearchHit) (map[uuid.UUID]string, error) {
	out := map[uuid.UUID]string{}
	if len(hits) == 0 {
		return out, nil
	}
	ids := make([]uuid.UUID, 0, len(hits))
	for _, h := range hits {
		ids = append(ids, h.Item.ID)
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT i.id, o.name, p.name, CASE WHEN p.id IS NULL THEN 0 ELSE COALESCE(access_level($1, 'files.item', p.id, $2), 0) END
		FROM files_items i JOIN orgs o ON o.id = i.org_id LEFT JOIN files_items p ON p.id = i.parent_id
		WHERE i.id = ANY ($3)`, me, access.ViaAgent(ctx), ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := map[uuid.UUID][2]string{}
	levels := map[uuid.UUID]int{}
	for rows.Next() {
		var (
			id     uuid.UUID
			org    string
			folder *string
			level  int
		)
		if err := rows.Scan(&id, &org, &folder, &level); err != nil {
			return nil, err
		}
		f := ""
		if folder != nil {
			f = *folder
		}
		names[id] = [2]string{org, f}
		levels[id] = level
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, h := range hits {
		it := h.Item
		n := names[it.ID]
		place := n[1]
		switch {
		case it.ParentID != nil && levels[it.ID] >= 1:
		case it.ParentID == nil && it.Drive == DriveShared:
			place = "Shared folders"
		case it.ParentID == nil && it.OwnerID != nil && *it.OwnerID == me:
			place = "My drive"
		default:
			place = "Shared with me"
		}
		out[it.ID] = app.SearchContext(n[0], place)
	}
	return out, nil
}

// orgWorded is a schema with "organization" in its descriptions replaced by
// the app's word for one (orgs.SetNoun).
func orgWorded(schema string) json.RawMessage {
	return json.RawMessage(strings.ReplaceAll(schema, "organization", orgs.Noun()))
}
