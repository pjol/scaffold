package files

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
)

// Agent tools on connected drives. Reads run as the person through their own
// connection and are capped by the files scope (GateTool) and the
// connection's scope (app.ConnectionScope), checked here per call and per
// row. Writes are separate tools per platform with Source set, so the
// assistant asks before each one and frames the output as third-party; their
// Target is files.external "<connection id>:<platform id>", and the
// connection's scope is checked in Preview (suggest) and Handler (edit).

// connTool is the compact shape tools return for a connected item.
type connTool struct {
	ConnectionID string         `json:"connection_id"`
	Ref          string         `json:"ref"`
	Type         string         `json:"type"`
	Name         string         `json:"name"`
	ParentRef    *string        `json:"parent_ref,omitempty"`
	MimeType     string         `json:"mime_type,omitempty"`
	Kind         string         `json:"kind,omitempty"`
	Size         int64          `json:"size,omitempty"`
	TextLike     bool           `json:"text,omitempty"`
	VersionID    *string        `json:"version_id,omitempty"`
	UpdatedAt    time.Time      `json:"updated_at"`
	YourAccess   string         `json:"your_access"`
	Link         string         `json:"link"`
	Source       *app.SourceRef `json:"source"`
}

func toConnTool(c *ConnectedItem) connTool {
	return connTool{ConnectionID: c.ConnectionID, Ref: c.Ref, Type: c.Type, Name: c.Name, ParentRef: c.ParentRef, MimeType: c.MimeType, Kind: c.KindLabel,
		Size: c.Size, TextLike: c.TextLike, VersionID: c.VersionID, UpdatedAt: c.UpdatedAt, YourAccess: c.MyLevel, Link: c.Link, Source: c.Source}
}

// agentSession opens a connection for a tool and checks the connection's
// scope for (ref) at need.
func (s *Service) agentSession(ctx context.Context, caller *auth.Identity, connID, ref string, write bool, need app.AgentLevel) (*connSession, error) {
	if connID == "" {
		return nil, httpx.BadRequest("Pass connection_id (from files_connected_list).")
	}
	cs, err := s.session(ctx, caller.UserID, uuid.Nil, connID, write)
	if err != nil {
		return nil, err
	}
	pid := ""
	if ref != "" {
		pid = cs.ad.externalID(ref)
	}
	if err := s.agentAllows(ctx, caller, connID, pid, need); err != nil {
		return nil, err
	}
	return cs, nil
}

// readableRows drops rows the connection's agent scope hides (people see all).
func (s *Service) readableRows(ctx context.Context, caller *auth.Identity, cs *connSession, items []*ConnectedItem) ([]*ConnectedItem, error) {
	if len(items) == 0 {
		return items, nil
	}
	ids := make([]string, len(items))
	for i, it := range items {
		ids[i] = externalTarget(cs.info.ID, cs.ad.externalID(it.Ref))
	}
	ok, err := app.AgentReadable(ctx, s.d.Modules, caller, app.ConnectionScope(cs.info.ID), KindExternal, ids)
	if err != nil {
		return nil, err
	}
	out := items[:0]
	for i, it := range items {
		if ok[ids[i]] {
			out = append(out, it)
		}
	}
	return out, nil
}

// searchConnected runs each connection's own search at once, each with its
// own deadline; slow or failing accounts are skipped (reported in missing).
func (s *Service) searchConnected(ctx context.Context, caller *auth.Identity, org uuid.UUID, query, only string, limit int) ([]*ConnectedItem, []string, error) {
	conns, err := s.connectedFor(ctx, caller.UserID, org)
	if err != nil || len(conns) == 0 {
		return nil, nil, err
	}
	type result struct {
		items []*ConnectedItem
		miss  string
	}
	results := make([]result, len(conns))
	var wg sync.WaitGroup
	for i, c := range conns {
		if c.Status != "active" || (only != "" && only != c.ID) {
			continue
		}
		wg.Add(1)
		go func(i int, c *connInfo) {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, connectedSearchTimeout)
			defer cancel()
			cs, err := s.session(cctx, caller.UserID, org, c.ID, false)
			if err == nil {
				err = s.agentAllows(cctx, caller, c.ID, "", app.AgentRead)
			}
			if err != nil {
				results[i].miss = c.label()
				return
			}
			page, err := cs.ad.search(cctx, truncate(query, 200), "", limit)
			if err != nil {
				results[i].miss = c.label()
				return
			}
			items := cs.page(page).Items
			if items, err = s.readableRows(cctx, caller, cs, items); err != nil {
				results[i].miss = c.label()
				return
			}
			results[i].items = items
		}(i, c)
	}
	wg.Wait()
	var all []*ConnectedItem
	var missing []string
	for _, r := range results {
		all = append(all, r.items...)
		if r.miss != "" {
			missing = append(missing, r.miss)
		}
	}
	return all, missing, nil
}

// connectedSearchResults adds connected hits to global search (Context
// "Google Drive · sam@acme.com", Source set, no OrgID: they belong to the
// person, not a workspace).
func (s *Service) connectedSearchResults(ctx context.Context, caller *auth.Identity, query string, limit int) []app.SearchResult {
	org := uuid.Nil
	if id, ok := app.SearchOrg(ctx); ok {
		org = id
	}
	items, _, err := s.searchConnected(ctx, caller, org, query, "", min(limit, 10))
	if err != nil {
		return nil
	}
	out := make([]app.SearchResult, 0, len(items))
	for i, it := range items {
		icon := "file"
		switch {
		case it.Type == TypeFolder:
			icon = "folder"
		case it.Preview == "image":
			icon = "image"
		case it.Preview == "video":
			icon = "video"
		case it.Preview == "pdf" || it.Preview == "text" || it.Preview == "markdown" || it.Preview == "office":
			icon = "file-text"
		}
		out = append(out, app.SearchResult{Kind: KindExternal, ID: it.ID, Title: it.Name, Link: it.Link, Icon: icon,
			Score: 0.45 - float64(i%10)*0.01, At: it.UpdatedAt, Context: fmtProduct(it.Source.Product, it.Source.Account), Source: it.Source})
	}
	return out
}

const (
	schemaConnList = `{"type":"object","properties":{
		"connection_id":{"type":"string","description":"A connected account from a call without it. Leave out to list the user's connected accounts."},
		"folder_ref":{"type":"string","description":"A folder's ref (from this tool). Leave out to list the account's drives."},
		"cursor":{"type":"string"}},"additionalProperties":false}`
	schemaConnItem = `{"type":"object","properties":{"connection_id":{"type":"string"},"ref":{"type":"string"}},"required":["connection_id","ref"],"additionalProperties":false}`
	schemaConnRead = `{"type":"object","properties":{
		"connection_id":{"type":"string"},"ref":{"type":"string"},
		"start_line":{"type":"integer","minimum":1},"end_line":{"type":"integer","minimum":1}},
		"required":["connection_id","ref"],"additionalProperties":false}`
	schemaConnCreate = `{"type":"object","properties":{
		"connection_id":{"type":"string"},
		"folder_ref":{"type":"string","description":"The folder's ref. Leave out for the top of My Drive / OneDrive."},
		"name":{"type":"string","description":"File name with extension, e.g. notes.md."},
		"text":{"type":"string","description":"The whole content (UTF-8, up to 1 MB)."}},
		"required":["connection_id","name","text"],"additionalProperties":false}`
	schemaConnReplace = `{"type":"object","properties":{
		"connection_id":{"type":"string"},"ref":{"type":"string"},
		"start_line":{"type":"integer","minimum":1},"end_line":{"type":"integer","minimum":0,"description":"Inclusive. start_line - 1 inserts before start_line."},
		"text":{"type":"string","description":"The new lines. Empty removes the range."},
		"base_version_id":{"type":"string","description":"The version_id you read. Refused if the file changed since."}},
		"required":["connection_id","ref","start_line","end_line","text"],"additionalProperties":false}`
	schemaConnRename = `{"type":"object","properties":{"connection_id":{"type":"string"},"ref":{"type":"string"},"name":{"type":"string"}},"required":["connection_id","ref","name"],"additionalProperties":false}`
	schemaConnMove   = `{"type":"object","properties":{"connection_id":{"type":"string"},"ref":{"type":"string"},"folder_ref":{"type":"string","description":"A folder in the same drive."}},"required":["connection_id","ref","folder_ref"],"additionalProperties":false}`
)

type connIn struct {
	ConnectionID  string `json:"connection_id"`
	Ref           string `json:"ref"`
	FolderRef     string `json:"folder_ref"`
	Cursor        string `json:"cursor"`
	Name          string `json:"name"`
	Text          string `json:"text"`
	StartLine     int    `json:"start_line"`
	EndLine       int    `json:"end_line"`
	BaseVersionID string `json:"base_version_id"`
}

// connectedReadTools are the read tools for connected drives.
func (s *Service) connectedReadTools() []app.Tool {
	return []app.Tool{
		{
			Name: "files_connected_list",
			Description: "Browse the user's connected drives (Google Drive, OneDrive, SharePoint): without connection_id, their connected accounts; " +
				"with it, that account's drives; with folder_ref, what the folder holds. Content comes live from the platform as the user.",
			InputSchema: json.RawMessage(schemaConnList),
			ReadOnly:    true,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in connIn
				if err := decode(input, &in); err != nil {
					return nil, err
				}
				if in.ConnectionID == "" {
					conns, err := s.connectedFor(ctx, caller.UserID, uuid.Nil)
					if err != nil {
						return nil, err
					}
					list := []map[string]any{}
					for _, c := range conns {
						if s.agentAllows(ctx, caller, c.ID, "", app.AgentRead) != nil {
							continue
						}
						list = append(list, map[string]any{"connection_id": c.ID, "label": c.label(), "provider": c.Provider, "product": c.product(),
							"account": c.Account, "status": c.Status, "can_change": c.writeCap != ""})
					}
					return map[string]any{"connections": list}, nil
				}
				cs, err := s.agentSession(ctx, caller, in.ConnectionID, "", false, app.AgentRead)
				if err != nil {
					return nil, err
				}
				if in.FolderRef == "" {
					drives, next, err := cs.ad.drives(ctx, in.Cursor)
					if err != nil {
						return nil, err
					}
					return map[string]any{"drives": orEmpty(drives), "next_cursor": next, "source": cs.info.source(cs.info.product(), "")}, nil
				}
				if err := s.agentAllows(ctx, caller, cs.info.ID, cs.ad.externalID(in.FolderRef), app.AgentRead); err != nil {
					return nil, err
				}
				page, err := cs.ad.list(ctx, in.FolderRef, "name", false, false, in.Cursor)
				if err != nil {
					return nil, err
				}
				rows, err := s.readableRows(ctx, caller, cs, cs.page(page).Items)
				if err != nil {
					return nil, err
				}
				items := make([]connTool, 0, len(rows))
				for _, r := range rows {
					items = append(items, toConnTool(r))
				}
				return map[string]any{"items": items, "next_cursor": page.Next}, nil
			},
		},
		{
			Name:        "files_connected_get_item",
			Description: "Get a connected file or folder's details: kind, size, version, its folders, the user's access and the platform's sharing summary.",
			InputSchema: json.RawMessage(schemaConnItem),
			ReadOnly:    true,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in connIn
				if err := decode(input, &in); err != nil {
					return nil, err
				}
				cs, err := s.agentSession(ctx, caller, in.ConnectionID, in.Ref, false, app.AgentRead)
				if err != nil {
					return nil, err
				}
				it, err := cs.getItem(ctx, in.Ref)
				if err != nil {
					return nil, err
				}
				crumbs, err := cs.ad.path(ctx, it)
				if err != nil {
					return nil, err
				}
				names := make([]string, 0, len(crumbs))
				for _, c := range crumbs {
					names = append(names, c.Name)
				}
				sh, _ := cs.ad.sharing(ctx, it)
				return map[string]any{"item": toConnTool(cs.toConnected(it)), "path": strings.Join(names, "/"), "sharing": sh}, nil
			},
		},
		{
			Name: "files_connected_read_text",
			Description: "Read a connected text file (plain text, code, Markdown, CSV) with line numbers, up to 256 KB at a time. " +
				"Pass version_id to a replace_lines tool as base_version_id.",
			InputSchema: json.RawMessage(schemaConnRead),
			ReadOnly:    true,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in connIn
				if err := decode(input, &in); err != nil {
					return nil, err
				}
				cs, err := s.agentSession(ctx, caller, in.ConnectionID, in.Ref, false, app.AgentRead)
				if err != nil {
					return nil, err
				}
				it, err := cs.ad.get(ctx, in.Ref)
				if err != nil {
					return nil, err
				}
				b, truncated, err := cs.readText(ctx, it, maxTextEdit)
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
				return map[string]any{"name": it.Name, "version_id": it.Version, "total_lines": len(doc.lines), "start_line": start, "end_line": end,
					"more": truncated || end < len(doc.lines), "text": text, "source": cs.info.source(cs.ad.product(it.Ref), it.WebURL)}, nil
			},
		},
	}
}

// connectedWriteTools are one set of write tools per platform.
func (s *Service) connectedWriteTools() []app.Tool {
	var out []app.Tool
	for _, pf := range []struct{ provider, prefix, source, product string }{
		{providerGoogle, "files_google_drive_", "Google Drive", "Google Drive"},
		{providerMicrosoft, "files_onedrive_", "OneDrive and SharePoint", "OneDrive or SharePoint"},
	} {
		pf := pf
		external := func(ref string) string {
			if pf.provider == providerMicrosoft {
				return strings.Replace(ref, "~", "/", 1)
			}
			return ref
		}
		target := func(field string) func(json.RawMessage) (string, string) {
			return func(input json.RawMessage) (string, string) {
				var m map[string]any
				_ = json.Unmarshal(input, &m)
				conn, _ := m["connection_id"].(string)
				ref, _ := m[field].(string)
				if conn == "" {
					return KindExternal, ""
				}
				if ref == "" {
					ref = "root"
				}
				return KindExternal, externalTarget(conn, external(ref))
			}
		}
		// open checks the platform and the connection's scope (suggest in
		// Preview, edit in Handler; a suggestion the person accepts runs as
		// them, without the agent marker).
		open := func(ctx context.Context, caller *auth.Identity, in connIn, ref string, write bool, need app.AgentLevel) (*connSession, error) {
			cs, err := s.agentSession(ctx, caller, in.ConnectionID, ref, write, need)
			if err != nil {
				return nil, err
			}
			if cs.info.Provider != pf.provider {
				return nil, httpx.BadRequest(fmt.Sprintf("That connection isn't %s. Use the %s* tools for it.", pf.product, toolPrefix(cs.info.Provider)))
			}
			return cs, nil
		}
		where := func(ctx context.Context, cs *connSession, ref string) string {
			if ref == "" {
				return cs.info.label()
			}
			it, err := cs.getItem(ctx, ref)
			if err != nil {
				return cs.info.label()
			}
			return cs.info.label() + " › " + it.Name
		}
		out = append(out,
			app.Tool{
				Name:        pf.prefix + "create_text_file",
				Description: "Create a new text file (up to 1 MB) in a folder of the user's connected " + pf.product + ". A taken name answers an error.",
				InputSchema: json.RawMessage(schemaConnCreate),
				Source:      pf.source,
				Target:      target("folder_ref"),
				Preview: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
					var in connIn
					if err := decode(input, &in); err != nil {
						return app.ToolPreview{}, err
					}
					cs, err := open(ctx, caller, in, in.FolderRef, false, app.AgentSuggest)
					if err != nil {
						return app.ToolPreview{}, err
					}
					name, err := cleanName(in.Name)
					if err != nil {
						return app.ToolPreview{}, err
					}
					pv := app.ToolPreview{Summary: "Create “" + name + "” in " + where(ctx, cs, in.FolderRef), After: in.Text}
					if in.FolderRef != "" {
						pv.Link = ConnectedLink(cs.info.ID, in.FolderRef)
					}
					return pv, nil
				},
				Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
					var in connIn
					if err := decode(input, &in); err != nil {
						return nil, err
					}
					cs, err := open(ctx, caller, in, in.FolderRef, true, app.AgentEdit)
					if err != nil {
						return nil, err
					}
					name, err := cleanName(in.Name)
					if err != nil {
						return nil, err
					}
					if len(in.Text) > maxTextEdit {
						return nil, httpx.BadRequest("Text files made here can be up to 1 MB.")
					}
					mt, textLike, err := classify("", name, []byte(in.Text))
					if err != nil || !textLike {
						return nil, httpx.BadRequest("That text isn't valid UTF-8.")
					}
					parent := in.FolderRef
					if parent == "" {
						if parent, err = cs.ad.rootRef(ctx); err != nil {
							return nil, err
						}
					}
					it, err := cs.ad.write(ctx, parent, nil, name, mt, int64(len(in.Text)), strings.NewReader(in.Text))
					if err != nil {
						return nil, err
					}
					cs.forget(parent)
					return map[string]any{"item": toConnTool(cs.toConnected(it))}, nil
				},
			},
			app.Tool{
				Name: pf.prefix + "replace_lines",
				Description: "Replace one range of lines in a plain-text file (text, code, Markdown, CSV) in the user's connected " + pf.product +
					", saving a new version there. Read it first with files_connected_read_text and pass its version_id as base_version_id.",
				InputSchema: json.RawMessage(schemaConnReplace),
				Source:      pf.source,
				Target:      target("ref"),
				Preview: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
					var in connIn
					if err := decode(input, &in); err != nil {
						return app.ToolPreview{}, err
					}
					cs, err := open(ctx, caller, in, in.Ref, false, app.AgentSuggest)
					if err != nil {
						return app.ToolPreview{}, err
					}
					e, err := s.planConnectedEdit(ctx, cs, in)
					if err != nil {
						return app.ToolPreview{}, err
					}
					pv := replacePreview(e.lineEdit)
					pv.Summary += " in " + cs.info.label()
					pv.Link = ConnectedLink(cs.info.ID, in.Ref)
					return pv, nil
				},
				Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
					var in connIn
					if err := decode(input, &in); err != nil {
						return nil, err
					}
					cs, err := open(ctx, caller, in, in.Ref, true, app.AgentEdit)
					if err != nil {
						return nil, err
					}
					e, err := s.planConnectedEdit(ctx, cs, in)
					if err != nil {
						return nil, err
					}
					body := e.doc.bytes()
					it, err := cs.ad.write(ctx, "", &e.ext, e.ext.Name, e.ext.MimeType, int64(len(body)), strings.NewReader(string(body)))
					if err != nil {
						return nil, err
					}
					cs.forget(in.Ref, it.Ref)
					return map[string]any{"item": toConnTool(cs.toConnected(it)), "replaced_lines": len(e.before), "inserted_lines": len(e.after),
						"total_lines": len(e.doc.lines)}, nil
				},
			},
			app.Tool{
				Name:        pf.prefix + "rename",
				Description: "Rename a file or folder in the user's connected " + pf.product + ".",
				InputSchema: json.RawMessage(schemaConnRename),
				Source:      pf.source,
				Target:      target("ref"),
				Preview: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
					var in connIn
					if err := decode(input, &in); err != nil {
						return app.ToolPreview{}, err
					}
					cs, err := open(ctx, caller, in, in.Ref, false, app.AgentSuggest)
					if err != nil {
						return app.ToolPreview{}, err
					}
					it, err := cs.ad.get(ctx, in.Ref)
					if err != nil {
						return app.ToolPreview{}, err
					}
					name, err := cleanName(in.Name)
					if err != nil {
						return app.ToolPreview{}, err
					}
					return app.ToolPreview{Summary: "Rename “" + it.Name + "” to “" + name + "” in " + cs.info.label(), Before: it.Name, After: name,
						Link: ConnectedLink(cs.info.ID, it.Ref)}, nil
				},
				Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
					var in connIn
					if err := decode(input, &in); err != nil {
						return nil, err
					}
					cs, err := open(ctx, caller, in, in.Ref, true, app.AgentEdit)
					if err != nil {
						return nil, err
					}
					name, err := cleanName(in.Name)
					if err != nil {
						return nil, err
					}
					it, err := cs.ad.get(ctx, in.Ref)
					if err != nil {
						return nil, err
					}
					if it.Root {
						return nil, httpx.BadRequest("Drives can't be renamed here.")
					}
					outIt, err := cs.ad.rename(ctx, it, name)
					if err != nil {
						return nil, err
					}
					cs.forget(in.Ref)
					return map[string]any{"item": toConnTool(cs.toConnected(outIt))}, nil
				},
			},
			app.Tool{
				Name:        pf.prefix + "move",
				Description: "Move a file or folder into another folder of the same drive in the user's connected " + pf.product + ".",
				InputSchema: json.RawMessage(schemaConnMove),
				Source:      pf.source,
				Target:      target("ref"),
				Preview: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
					var in connIn
					if err := decode(input, &in); err != nil {
						return app.ToolPreview{}, err
					}
					cs, err := open(ctx, caller, in, in.Ref, false, app.AgentSuggest)
					if err != nil {
						return app.ToolPreview{}, err
					}
					if err := s.agentAllows(ctx, caller, cs.info.ID, cs.ad.externalID(in.FolderRef), app.AgentSuggest); err != nil {
						return app.ToolPreview{}, err
					}
					it, err := cs.ad.get(ctx, in.Ref)
					if err != nil {
						return app.ToolPreview{}, err
					}
					dest, err := cs.ad.get(ctx, in.FolderRef)
					if err != nil {
						return app.ToolPreview{}, err
					}
					if !dest.Folder || dest.DriveRef != it.DriveRef {
						return app.ToolPreview{}, httpx.BadRequest("Choose a folder in the same drive.")
					}
					from := s.connPathText(ctx, cs, it)
					to := s.connPathText(ctx, cs, extItem{ParentRef: dest.Ref, Root: false}) // dest's path + dest
					return app.ToolPreview{Summary: "Move “" + it.Name + "” to “" + dest.Name + "” in " + cs.info.label(),
						Before: from + it.Name, After: to + it.Name, Link: ConnectedLink(cs.info.ID, it.Ref)}, nil
				},
				Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
					var in connIn
					if err := decode(input, &in); err != nil {
						return nil, err
					}
					cs, err := open(ctx, caller, in, in.Ref, true, app.AgentEdit)
					if err != nil {
						return nil, err
					}
					if err := s.agentAllows(ctx, caller, cs.info.ID, cs.ad.externalID(in.FolderRef), app.AgentEdit); err != nil {
						return nil, err
					}
					it, err := cs.moveItem(ctx, in.Ref, in.FolderRef)
					if err != nil {
						return nil, err
					}
					return map[string]any{"item": toConnTool(cs.toConnected(it))}, nil
				},
			},
		)
	}
	return out
}

func toolPrefix(provider string) string {
	if provider == providerMicrosoft {
		return "files_onedrive_"
	}
	return "files_google_drive_"
}

// connPathText is "My Drive/Plans/" for the folder holding it.
func (s *Service) connPathText(ctx context.Context, cs *connSession, it extItem) string {
	crumbs, err := cs.ad.path(ctx, it)
	if err != nil {
		return ""
	}
	var b strings.Builder
	for _, c := range crumbs {
		b.WriteString(c.Name + "/")
	}
	return b.String()
}

type connEdit struct {
	*lineEdit
	ext extItem
}

// planConnectedEdit reads a plain-text file and plans a line replacement,
// refusing other kinds, large files and a stale base version.
func (s *Service) planConnectedEdit(ctx context.Context, cs *connSession, in connIn) (*connEdit, error) {
	it, err := cs.ad.get(ctx, in.Ref)
	if err != nil {
		return nil, err
	}
	if it.Folder || it.Native != "" {
		return nil, httpx.NewError(http.StatusUnsupportedMediaType, "not_text", "Only plain-text files can be edited by line here. Edit "+orWord(it.Native, "this file")+" on the platform.")
	}
	if it.Size > maxTextEdit {
		return nil, httpx.NewError(http.StatusRequestEntityTooLarge, "file_too_large", "Files over 1 MB can't be edited by line.")
	}
	if in.BaseVersionID != "" && it.Version != "" && in.BaseVersionID != it.Version {
		return nil, httpx.NewError(http.StatusConflict, "target_changed", "The file changed since you read it. Read it again, then retry.")
	}
	b, truncated, err := cs.readText(ctx, it, maxTextEdit)
	if err != nil {
		return nil, err
	}
	if truncated {
		return nil, httpx.NewError(http.StatusRequestEntityTooLarge, "file_too_large", "Files over 1 MB can't be edited by line.")
	}
	doc, before, after, err := parseText(b).replaceLines(in.StartLine, in.EndLine, in.Text)
	if err != nil {
		return nil, err
	}
	item := &Item{Name: it.Name, Link: ConnectedLink(cs.info.ID, it.Ref)}
	return &connEdit{lineEdit: &lineEdit{item: item, doc: doc, start: in.StartLine, end: in.EndLine, before: before, after: after}, ext: it}, nil
}

func orWord(s, def string) string {
	if s == "" {
		return def
	}
	return s + " files"
}

// connectedToolSearch is files_search's connected half.
func (s *Service) connectedToolSearch(ctx context.Context, caller *auth.Identity, query, source string, limit int) ([]connTool, []string) {
	only := ""
	if source != "" && source != "all" && source != "connected" {
		only = source
	}
	items, missing, err := s.searchConnected(ctx, caller, uuid.Nil, query, only, limit)
	if err != nil {
		return nil, nil
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].UpdatedAt.After(items[j].UpdatedAt) })
	out := make([]connTool, 0, len(items))
	for _, it := range items {
		out = append(out, toConnTool(it))
	}
	return out, missing
}
