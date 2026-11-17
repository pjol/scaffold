package docs

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
)

// pageTools are the agent tools for templates and history. They run as the
// caller with access.ViaAgent in every query (requirePage, the template and
// history queries); the write sets Target and an exact Preview. There are no
// tools to publish, share, follow for someone or export files.
func (s *Service) pageTools() []app.Tool {
	parentTarget := func(input json.RawMessage) (string, string) {
		var m map[string]any
		_ = json.Unmarshal(input, &m)
		id, _ := m["parent_id"].(string)
		return Kind, id
	}
	pageTarget := func(input json.RawMessage) (string, string) {
		var m map[string]any
		_ = json.Unmarshal(input, &m)
		id, _ := m["page_id"].(string)
		return Kind, id
	}
	type useIn struct {
		TemplateID string    `json:"template_id"`
		ParentID   uuid.UUID `json:"parent_id"`
		Title      string    `json:"title"`
	}
	// planUse checks the template and parent the way the handler will and
	// describes the page it would make.
	planUse := func(ctx context.Context, caller *auth.Identity, input json.RawMessage, min access.Level) (*Page, string, string, error) {
		var in useIn
		if err := decodeInput(input, &in); err != nil {
			return nil, "", "", err
		}
		parent, err := requirePage(ctx, s.d.Pool, caller.UserID, in.ParentID, min)
		if err != nil {
			return nil, "", "", err
		}
		name, outline := "", ""
		if key, ok := strings.CutPrefix(in.TemplateID, "builtin:"); ok {
			t, ok := builtinByKey(key)
			if !ok {
				return nil, "", "", httpx.NotFound("Template not found.")
			}
			name = t.Icon + " " + t.Name
			if t.Database != nil {
				outline = "A database with " + itoa(len(t.Database.Props)) + " properties, " + itoa(len(t.Database.Views)) + " views and " + itoa(len(t.Database.Rows)) + " example rows."
			} else {
				outline = previewMarkdown(templateBlocks(t.Markdown))
			}
		} else {
			id, err := uuid.Parse(in.TemplateID)
			if err != nil {
				return nil, "", "", httpx.NotFound("Template not found.")
			}
			src, err := requirePage(ctx, s.d.Pool, caller.UserID, id, access.View)
			if err != nil {
				return nil, "", "", err
			}
			if !src.IsTemplate || src.ParentID != nil || src.OrgID != parent.OrgID {
				return nil, "", "", httpx.NotFound("Template not found.")
			}
			name = displayTitle(src.Title)
			if src.Icon != nil {
				name = *src.Icon + " " + name
			}
			md, err := s.pageAsMarkdown(ctx, caller.UserID, src, false)
			if err != nil {
				return nil, "", "", err
			}
			outline = md
		}
		title := name
		if strings.TrimSpace(in.Title) != "" {
			t, err := cleanTitle(in.Title)
			if err != nil {
				return nil, "", "", err
			}
			title = t
		}
		return parent, fmt.Sprintf("Create “%s” from the template “%s” in “%s”", title, name, displayTitle(parent.Title)), outline, nil
	}
	return []app.Tool{
		{
			Name:        "docs_list_templates",
			Description: "List the page templates the user can use in a workspace: built-in ones (id builtin:<key>) and workspace templates (id = the template page). Use one with docs_create_from_template.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"org_id":{"type":"string","format":"uuid"}},"required":["org_id"],"additionalProperties":false}`),
			ReadOnly:    true,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					OrgID uuid.UUID `json:"org_id"`
				}
				if err := decodeInput(input, &in); err != nil {
					return nil, err
				}
				if role, err := orgs.RoleOf(ctx, s.d.Pool, in.OrgID, caller.UserID); err != nil {
					return nil, err
				} else if role == "" {
					return nil, httpx.NotFound("Not a member of that " + orgs.Noun() + ".")
				}
				t, err := s.templates(ctx, caller.UserID, in.OrgID)
				if err != nil {
					return nil, err
				}
				out := make([]map[string]any, 0, len(t.Builtin)+len(t.Workspace))
				for _, x := range append(t.Builtin, t.Workspace...) {
					out = append(out, map[string]any{"id": x.ID, "name": x.Name, "category": x.Category, "kind": x.Kind, "about": x.Info, "source": x.Source})
				}
				return out, nil
			},
		},
		{
			Name:        "docs_create_from_template",
			Description: "Create a page from a template (docs_list_templates) inside a page the user can edit. Returns the new page's id and link.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"template_id":{"type":"string","minLength":1},"parent_id":{"type":"string","format":"uuid"},"title":{"type":"string","maxLength":300}},"required":["template_id","parent_id"],"additionalProperties":false}`),
			Target:      parentTarget,
			Preview: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
				parent, summary, outline, err := planUse(ctx, caller, input, access.View)
				if err != nil {
					return app.ToolPreview{}, err
				}
				return app.ToolPreview{Summary: summary, After: outline, Link: "/docs/" + parent.ID.String()}, nil
			},
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				parent, _, _, err := planUse(ctx, caller, input, access.Edit)
				if err != nil {
					return nil, err
				}
				var in useIn
				_ = decodeInput(input, &in)
				p, err := s.createFromTemplate(ctx, caller.UserID, parent.OrgID, in.TemplateID, useTemplateReq{ParentID: &parent.ID, Title: in.Title})
				if err != nil {
					return nil, err
				}
				return map[string]any{"id": p.ID, "title": displayTitle(p.Title), "kind": p.Kind, "link": "/docs/" + p.ID.String()}, nil
			},
		},
		{
			Name: "docs_page_history",
			Description: "Read a page's updates, newest first: saved versions (who edited, what changed: blocks added, edited, removed, renamed) and new comment threads. " +
				"Pass version_id to get one version's changes block by block (the text before and after).",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"page_id":{"type":"string","format":"uuid"},"version_id":{"type":"string","format":"uuid"},"limit":{"type":"integer","minimum":1,"maximum":50},"before":{"type":"string","format":"date-time"}},"required":["page_id"],"additionalProperties":false}`),
			ReadOnly:    true,
			Target:      pageTarget,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					PageID    uuid.UUID  `json:"page_id"`
					VersionID *uuid.UUID `json:"version_id"`
					Limit     int        `json:"limit"`
					Before    *time.Time `json:"before"`
				}
				if err := decodeInput(input, &in); err != nil {
					return nil, err
				}
				p, err := requirePage(ctx, s.d.Pool, caller.UserID, in.PageID, access.View)
				if err != nil {
					return nil, err
				}
				if in.VersionID != nil {
					d, err := s.versionDiff(ctx, caller.UserID, *in.VersionID, "previous")
					if err != nil {
						return nil, err
					}
					var page uuid.UUID
					if err := s.d.Pool.QueryRow(ctx, `SELECT page_id FROM docs_snapshots WHERE id = $1`, *in.VersionID).Scan(&page); err != nil || page != p.ID {
						return nil, httpx.NotFound("That version isn't one of this page's.")
					}
					changes := []map[string]any{}
					for _, e := range d.Entries {
						if e.Change == "" {
							continue
						}
						c := map[string]any{"change": e.Change, "type": e.Block.Type, "block_id": e.Block.ID, "text": blockText(e.Block.Type, e.Block.Props)}
						if e.Change == "changed" {
							c["before"] = e.BeforeText
						}
						changes = append(changes, c)
					}
					return map[string]any{"version_id": in.VersionID, "summary": d.Summary.Text(), "title_before": d.FromTitle, "title_after": d.ToTitle, "changes": changes}, nil
				}
				limit := in.Limit
				if limit <= 0 || limit > 50 {
					limit = 20
				}
				list, err := s.pageUpdates(ctx, caller.UserID, p.ID, in.Before, limit)
				if err != nil {
					return nil, err
				}
				out := make([]map[string]any, 0, len(list))
				for _, u := range list {
					names := make([]string, 0, len(u.People))
					for _, x := range u.People {
						names = append(names, x.Name)
					}
					item := map[string]any{"type": u.Type, "at": u.At, "people": names, "text": u.Text}
					if u.SnapshotID != nil {
						item["version_id"] = u.SnapshotID
					}
					if u.ThreadID != nil {
						item["thread_id"] = u.ThreadID
					}
					out = append(out, item)
				}
				return map[string]any{"page_id": p.ID, "title": displayTitle(p.Title), "updates": out}, nil
			},
		},
	}
}
