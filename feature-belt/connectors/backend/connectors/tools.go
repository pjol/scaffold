package connectors

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
)

// ToolsFor is app.DynamicToolProvider: the caller's remote tools, from each
// healthy connection whose connector is available, named
// <connector slug>__<tool> and labelled with the connector's name (Source).
// A tool is offered when the user hasn't switched it off, and, unless it is
// read-only, only when the user turned on changes for that connection and
// staff allow them. Read-only comes from the tool's readOnlyHint, believed
// only for catalog connectors; absent means it changes things.
//
// Every handler re-checks all of that against the database when called, as
// the caller passed in (never the user captured at listing time).
func (s *Service) ToolsFor(ctx context.Context, caller *auth.Identity) ([]app.Tool, error) {
	if caller == nil {
		return nil, nil
	}
	conns, cons, err := s.activeConnections(ctx, caller.UserID)
	if err != nil || len(conns) == 0 {
		return nil, err
	}
	// The turn waits for this: a slow server is skipped, not waited on.
	lctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	lists := make([][]remoteTool, len(conns))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i := range conns {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			tools, _, err := s.remoteTools(lctx, cons[i], conns[i], false)
			if err != nil {
				slog.WarnContext(ctx, "connectors: tools unavailable for a turn", "connection_id", conns[i].ID,
					"connector", cons[i].Slug, "err", err)
				return
			}
			lists[i] = tools
		}(i)
	}
	wg.Wait()

	var out []app.Tool
	taken := map[string]bool{}
	for i, cc := range conns {
		c := cons[i]
		for _, rt := range lists[i] {
			readOnly, offered := offeredTool(c, cc, rt)
			if !offered {
				continue
			}
			name := exposedName(c.Slug, rt.Name)
			if taken[name] {
				continue
			}
			taken[name] = true
			t := app.Tool{
				Name:        name,
				Description: toolDescription(c.Name, rt),
				InputSchema: objectSchema(rt.InputSchema),
				ReadOnly:    readOnly,
				Source:      c.Name,
				Feature:     FeatureKey(c.ID),
				Handler:     s.toolHandler(cc.ID, rt.Name),
				// Target stays nil: agent scope applies per service (Feature), not per call.
			}
			if !readOnly {
				t.Preview = toolPreview(c.Name, cc.ID, rt)
			}
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// FeatureKey is the agent-scope key of one external service
// ("connectors:<connector id>"), stable for the life of the connector, so a
// user can scope each service separately (off, read, suggest, edit). Tools
// carry it in app.Tool.Feature; the settings screens pass the same key to
// the resource menu.
func FeatureKey(connectorID uuid.UUID) string { return "connectors:" + connectorID.String() }

// toolPreview describes an external write for the suggestion a user reviews
// when their agent scope for the service is "suggest".
func toolPreview(service string, connID uuid.UUID, rt remoteTool) func(context.Context, *auth.Identity, json.RawMessage) (app.ToolPreview, error) {
	title := strings.TrimSpace(rt.Title)
	if title == "" {
		title = rt.Name
	}
	return func(_ context.Context, _ *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
		var pretty bytes.Buffer
		after := string(input)
		if json.Indent(&pretty, input, "", "  ") == nil {
			after = pretty.String()
		}
		return app.ToolPreview{Summary: service + ": " + truncate(title, 120), After: truncate(after, 4000),
			Link: "/settings/connections/" + connID.String()}, nil
	}
}

// offeredTool decides whether rt goes to the assistant for this connection.
func offeredTool(c *connectorRow, cc *connectionRow, rt remoteTool) (readOnly, offered bool) {
	readOnly = c.trustsAnnotations() && rt.ReadOnlyHint
	if slices.Contains(cc.DisabledTools, rt.Name) {
		return readOnly, false
	}
	if !readOnly && !writesAllowed(c, cc) {
		return readOnly, false
	}
	return readOnly, true
}

func writesAllowed(c *connectorRow, cc *connectionRow) bool {
	return cc.WritesEnabled && c.WritePolicy != "disabled"
}

// toolHandler calls a remote tool after re-checking, as caller, everything
// that decided the tool was offered.
func (s *Service) toolHandler(connID uuid.UUID, remoteName string) func(context.Context, *auth.Identity, json.RawMessage) (any, error) {
	return func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
		if caller == nil {
			return nil, httpx.ErrUnauthenticated
		}
		cc, c, err := s.ownConnection(ctx, s.d.Pool, caller.UserID, connID, false)
		if err != nil {
			return nil, httpx.NewError(http.StatusNotFound, "connector_disconnected", "That connection was removed.")
		}
		if cc.Status != "active" {
			return nil, remoteHTTPError(c, errNeedsReauth)
		}
		if !s.available(c) {
			return nil, httpx.NewError(http.StatusForbidden, "connector_unavailable", c.Name+" is turned off on this server.")
		}
		tools, _, err := s.remoteTools(ctx, c, cc, false)
		if err != nil {
			return nil, remoteHTTPError(c, err)
		}
		i := slices.IndexFunc(tools, func(t remoteTool) bool { return t.Name == remoteName })
		if i < 0 {
			return nil, httpx.NewError(http.StatusNotFound, "remote_tool_not_found", c.Name+" no longer offers that tool.")
		}
		if _, offered := offeredTool(c, cc, tools[i]); !offered {
			return nil, httpx.NewError(http.StatusForbidden, "tool_not_allowed",
				"That "+c.Name+" tool is switched off for this connection. The user can change it in Settings → Connections.")
		}
		return s.callRemote(ctx, c, cc, remoteName, input)
	}
}

// exposedName is "<slug>__<tool>" within the provider-safe charset
// (^[a-zA-Z0-9_-]{1,64}$). A long or unusual name keeps a hash suffix so two
// remote names never collapse into one.
func exposedName(slug, remote string) string {
	var b strings.Builder
	changed := false
	for _, r := range remote {
		if r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
			changed = true
		}
	}
	name := b.String()
	room := 64 - len(slug) - 2
	if name == "" || len(name) > room || changed {
		sum := sha256.Sum256([]byte(remote))
		suffix := "_" + hex.EncodeToString(sum[:3])
		if len(name) > room-len(suffix) {
			name = name[:max(0, room-len(suffix))]
		}
		name += suffix
	}
	return slug + "__" + name
}

// toolDescription is "[<Connector>] <the server's description>", cleaned
// of control characters and capped.
func toolDescription(source string, rt remoteTool) string {
	d := strings.TrimSpace(rt.Description)
	if d == "" {
		d = strings.TrimSpace(rt.Title)
	}
	if d == "" {
		d = rt.Name
	}
	d = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, d)
	return "[" + source + "] " + truncate(d, 1000)
}

// objectSchema makes sure a remote input schema is a JSON object schema
// with "type": "object" (some servers omit it).
func objectSchema(raw json.RawMessage) json.RawMessage {
	var m map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if dec.Decode(&m) != nil || m == nil {
		return json.RawMessage(`{"type":"object"}`)
	}
	if _, ok := m["type"]; !ok {
		m["type"] = "object"
	}
	b, err := json.Marshal(m)
	if err != nil {
		return json.RawMessage(`{"type":"object"}`)
	}
	return b
}
