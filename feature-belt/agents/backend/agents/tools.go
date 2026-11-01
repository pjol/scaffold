package agents

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
)

// Tools gives agents read-only views of their own scope and suggestions. No
// tool changes a scope or decides a suggestion; the person does that in the
// app. Suggestion tools see only suggestions made by the calling agent.
func (s *Service) Tools() []app.Tool {
	return []app.Tool{
		{
			Name: "agents_my_scopes",
			Description: "Show what the person lets AI agents do for them: the default level, each feature's level, and items with their own level. " +
				"Levels: off (agents can't see it), read (read only), suggest (changes wait for the person to accept them), edit (changes run). " +
				"Call it before planning changes so you know which ones will run and which become suggestions.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
			ReadOnly:    true,
			Handler:     s.toolMyScopes,
		},
		{
			Name: "agents_get_suggestion",
			Description: "Check a suggestion you made (a change that waited for the person's review): pending, accepted, rejected, failed or expired, " +
				"with the error when it failed and the id of the item an accepted call returned (read it with the feature's own tools). " +
				"Pass the suggestion_id the suggested call returned.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"suggestion_id":{"type":"string","format":"uuid"}},"required":["suggestion_id"],"additionalProperties":false}`),
			ReadOnly:    true,
			Handler:     s.toolGetSuggestion,
		},
		{
			Name:        "agents_list_suggestions",
			Description: "List your recent suggestions to the person, newest first, optionally only one status (pending, accepted, rejected, failed, expired).",
			InputSchema: json.RawMessage(`{"type":"object","properties":{
				"status":{"type":"string","enum":["pending","accepted","rejected","failed","expired"]},
				"limit":{"type":"integer","minimum":1,"maximum":50}
			},"additionalProperties":false}`),
			ReadOnly: true,
			Handler:  s.toolListSuggestions,
		},
	}
}

type toolScopeItem struct {
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
	Level Level  `json:"level"`
}

// toolMyScopes reports the scope. Items set to off are only counted: their
// titles and ids would tell the agent about things it may not see.
func (s *Service) toolMyScopes(ctx context.Context, caller *auth.Identity, _ json.RawMessage) (any, error) {
	st, err := s.state(ctx, s.d.Pool, caller.UserID)
	if err != nil {
		return nil, err
	}
	own, _, err := s.featureOverrides(ctx, caller.UserID)
	if err != nil {
		return nil, err
	}
	type feat struct {
		Feature string `json:"feature"`
		Label   string `json:"label"`
		Level   Level  `json:"level"`
		Own     bool   `json:"own_setting"`
	}
	features := []feat{}
	for _, e := range s.featureCatalog() {
		f := feat{Feature: e.Feature, Label: e.Label, Level: st.Default}
		if o, ok := own[e.Feature]; ok {
			f.Level, f.Own = o.level, true
		}
		features = append(features, f)
	}
	for f, o := range own {
		if isService(f) {
			label := o.label
			if label == "" {
				label = featureLabel(f)
			}
			features = append(features, feat{Feature: f, Label: label, Level: o.level, Own: true})
		}
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT kind, resource_id, level, title FROM agents_resource_scopes
		WHERE user_id = $1 AND level <> 'off' ORDER BY created_at DESC, id DESC LIMIT 100`, caller.UserID)
	if err != nil {
		return nil, err
	}
	items := []toolScopeItem{}
	for rows.Next() {
		var it toolScopeItem
		if err := rows.Scan(&it.Kind, &it.ID, &it.Level, &it.Title); err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range items {
		k, ok := access.KindOf(items[i].Kind)
		if !ok {
			continue
		}
		items[i].Title = ""
		if rid, err := uuid.Parse(items[i].ID); err == nil {
			if lvl, err := access.LevelOf(ctx, s.d.Pool, caller.UserID, items[i].Kind, rid); err == nil && lvl >= access.View {
				if t, err := k.Title(ctx, s.d.Pool, rid); err == nil {
					items[i].Title = truncate(t, 120)
				}
			}
		}
	}
	var off int
	if err := s.d.Pool.QueryRow(ctx, `SELECT count(*) FROM agents_resource_scopes WHERE user_id = $1 AND level = 'off'`, caller.UserID).Scan(&off); err != nil {
		return nil, err
	}
	return map[string]any{
		"default_level":   st.Default,
		"features":        features,
		"items":           items,
		"items_off_count": off,
		"rules":           "Changes to sharing, permissions, membership and credentials are never allowed. You never get more access than the person has.",
	}, nil
}

// toolSuggestion is a suggestion as its agent reads it. It never carries the
// accepted call's output: that ran as the person, with their access, and can
// name things past the agent's scope (related titles, other people's rows).
// The agent gets the outcome and the ids to look things up with its own
// reads, which its scope gates.
type toolSuggestion struct {
	ID        uuid.UUID        `json:"id"`
	Status    string           `json:"status"`
	Tool      string           `json:"tool"`
	Summary   string           `json:"summary"`
	Target    *Target          `json:"target,omitempty"`
	ResultID  string           `json:"result_id,omitempty"` // the item the accepted call returned (often one it created)
	Error     *SuggestionError `json:"error,omitempty"`
	CreatedAt string           `json:"created_at"`
	DecidedAt string           `json:"decided_at,omitempty"`
	ExpiresAt string           `json:"expires_at,omitempty"`
}

func (s *Service) toToolSuggestion(sg *Suggestion) toolSuggestion {
	out := toolSuggestion{ID: sg.ID, Status: sg.Status, Tool: sg.Tool, Summary: sg.Summary, Target: sg.Target,
		Error: sg.Error, CreatedAt: sg.CreatedAt.UTC().Format("2006-01-02T15:04:05Z")}
	if sg.Status == StatusAccepted {
		kind := ""
		if sg.Target != nil {
			kind = sg.Target.Kind
		} else if t, ok := s.toolIndex()[sg.Tool]; ok {
			kind = targetKind(t.Tool) // a create names no item, but its kind
		}
		out.ResultID = resultID(sg.Result, kind)
	}
	if sg.DecidedAt != nil {
		out.DecidedAt = sg.DecidedAt.UTC().Format("2006-01-02T15:04:05Z")
	}
	if sg.Status == StatusPending {
		out.ExpiresAt = sg.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z")
	}
	return out
}

// resultID is the id of the item a tool's JSON object output describes: its
// top-level "id", else "<thing>_id" for the tool's kind "<feature>.<thing>"
// ("event_id" for calendar.event).
func resultID(result json.RawMessage, kind string) string {
	var v map[string]any
	if len(result) == 0 || json.Unmarshal(result, &v) != nil {
		return ""
	}
	keys := []string{"id"}
	if _, thing, ok := strings.Cut(kind, "."); ok {
		keys = append(keys, thing+"_id")
	}
	for _, k := range keys {
		switch id := v[k].(type) {
		case string:
			return truncate(id, 100)
		case float64:
			return strconv.FormatFloat(id, 'f', -1, 64)
		}
	}
	return ""
}

func (s *Service) toolGetSuggestion(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
	var in struct {
		SuggestionID string `json:"suggestion_id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, httpx.BadRequest("suggestion_id must be a suggestion id.")
	}
	id, err := uuid.Parse(strings.TrimSpace(in.SuggestionID))
	if err != nil {
		return nil, errNotFound("Suggestion")
	}
	agent, _ := auth.AgentFrom(ctx)
	sg, err := scanSuggestion(s.d.Pool.QueryRow(ctx, `SELECT `+suggestionCols+` FROM agents_suggestions
		WHERE id = $1 AND user_id = $2 AND via = $3 AND client_key = $4`, id, caller.UserID, clientVia(agent), clientKey(agent)))
	if err != nil {
		return nil, errNotFound("Suggestion")
	}
	return s.toToolSuggestion(sg), nil
}

func (s *Service) toolListSuggestions(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
	var in struct {
		Status string `json:"status"`
		Limit  int    `json:"limit"`
	}
	if len(input) > 0 {
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, httpx.BadRequest("Invalid input: " + err.Error())
		}
	}
	if in.Status != "" && !validStatus[in.Status] {
		return nil, httpx.BadRequest("status must be pending, accepted, rejected, failed or expired.")
	}
	if in.Limit <= 0 || in.Limit > 50 {
		in.Limit = 20
	}
	agent, _ := auth.AgentFrom(ctx)
	rows, err := s.d.Pool.Query(ctx, `
		SELECT `+suggestionCols+` FROM agents_suggestions
		WHERE user_id = $1 AND via = $2 AND client_key = $3
		  AND ($4 = '' OR CASE WHEN status = 'pending' AND expires_at <= now() THEN 'expired'
		                       WHEN status = 'accepting' THEN 'pending' ELSE status END = $4)
		ORDER BY created_at DESC, id DESC LIMIT $5`, caller.UserID, clientVia(agent), clientKey(agent), in.Status, in.Limit)
	if err != nil {
		return nil, err
	}
	list, err := collectSuggestions(rows)
	if err != nil {
		return nil, err
	}
	out := make([]toolSuggestion, 0, len(list))
	for i := range list {
		out = append(out, s.toToolSuggestion(&list[i]))
	}
	return map[string]any{"items": out}, nil
}
