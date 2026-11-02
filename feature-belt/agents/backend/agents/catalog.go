package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
)

// indexedTool is a tool with the agent-scope key it is gated under.
type indexedTool struct {
	app.Tool
	feature string // Tool.Feature, else the providing module's Name()
}

// toolIndex is every ToolProvider tool by name, built on first use
// (Deps.Modules is filled after every module is constructed). It also learns
// which resource kinds each feature's tools act on (toolKinds).
func (s *Service) toolIndex() map[string]indexedTool {
	s.toolsOnce.Do(func() {
		s.tools = map[string]indexedTool{}
		s.toolKinds = map[string]map[string]bool{}
		defer func() {
			for _, t := range s.tools {
				if k := targetKind(t.Tool); k != "" && kindFeature(k) != t.feature {
					if s.toolKinds[t.feature] == nil {
						s.toolKinds[t.feature] = map[string]bool{}
					}
					s.toolKinds[t.feature][k] = true
				}
			}
		}()
		for _, m := range s.d.Modules {
			tp, ok := m.(app.ToolProvider)
			if !ok {
				continue
			}
			for _, t := range tp.Tools() {
				if t.Handler == nil {
					continue
				}
				if _, dup := s.tools[t.Name]; dup {
					continue
				}
				f := t.Feature
				if f == "" {
					f = m.Name()
				}
				s.tools[t.Name] = indexedTool{Tool: t, feature: f}
			}
		}
	})
	return s.tools
}

// targetKind asks a tool's Target which kind it acts on, with an empty input
// (a Target returns its kind even without an id). Panics count as none.
func targetKind(t app.Tool) (kind string) {
	if t.Target == nil {
		return ""
	}
	defer func() {
		if r := recover(); r != nil {
			slog.Warn("agents: tool target panicked on empty input", "tool", t.Name, "panic", fmt.Sprint(r))
			kind = ""
		}
	}()
	kind, _ = t.Target(json.RawMessage(`{}`))
	kind = strings.TrimSpace(kind)
	if !kindRE.MatchString(kind) {
		return ""
	}
	return kind
}

var (
	extraKindsMu sync.RWMutex
	extraKinds   = map[string][]string{}
)

// SetFeatureKinds declares resource kinds a feature's tools act on besides
// its own "<feature>.*" kinds, for tools whose Target can't name its kind
// without an id. Items of those kinds with their own level then count for
// the feature (FeatureLevel's max lists its tools; its ⋮ menu may set them).
// Call it from stitching code before requests are served.
func SetFeatureKinds(feature string, kinds ...string) {
	extraKindsMu.Lock()
	defer extraKindsMu.Unlock()
	for _, k := range kinds {
		if kindRE.MatchString(k) && kindFeature(k) != feature {
			extraKinds[feature] = append(extraKinds[feature], k)
		}
	}
}

// foreignKinds are the kinds of other features that feature's tools act on:
// learned from its tools' Targets plus SetFeatureKinds, sorted.
func (s *Service) foreignKinds(feature string) []string {
	s.toolIndex()
	set := map[string]bool{}
	for k := range s.toolKinds[feature] {
		set[k] = true
	}
	extraKindsMu.RLock()
	for _, k := range extraKinds[feature] {
		set[k] = true
	}
	extraKindsMu.RUnlock()
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// featureOwnsKind: kind is "<feature>.*" or one its tools act on.
func (s *Service) featureOwnsKind(feature, kind string) bool {
	if kindFeature(kind) == feature {
		return true
	}
	for _, k := range s.foreignKinds(feature) {
		if k == kind {
			return true
		}
	}
	return false
}

// dynamicTools asks every DynamicToolProvider for the caller's tools (the
// connectors feature's external services). A slow or failing provider costs
// only its own tools.
func (s *Service) dynamicTools(ctx context.Context, caller *auth.Identity) []indexedTool {
	var out []indexedTool
	for _, m := range s.d.Modules {
		dp, ok := m.(app.DynamicToolProvider)
		if !ok {
			continue
		}
		dctx, cancel := context.WithTimeout(ctx, dynamicTimeout)
		tools, err := dp.ToolsFor(dctx, caller)
		cancel()
		if err != nil {
			slog.WarnContext(ctx, "agents: dynamic tools unavailable", "module", m.Name(), "err", err)
			continue
		}
		for _, t := range tools {
			if t.Handler == nil {
				continue
			}
			f := t.Feature
			if f == "" {
				f = m.Name()
			}
			out = append(out, indexedTool{Tool: t, feature: f})
		}
	}
	return out
}

// findTool finds a tool by name for caller: the app's own first, then the
// caller's external tools. ok is false when it is gone.
func (s *Service) findTool(ctx context.Context, caller *auth.Identity, name string) (indexedTool, bool) {
	if t, ok := s.toolIndex()[name]; ok {
		return t, true
	}
	for _, t := range s.dynamicTools(ctx, caller) {
		if t.Name == name {
			return t, true
		}
	}
	return indexedTool{}, false
}

// installed reports whether a module with this name is in the app.
func (s *Service) installed(name string) bool {
	for _, m := range s.d.Modules {
		if m.Name() == name {
			return true
		}
	}
	return false
}

// knownFeature: a module of this app, or an external service when the
// connectors feature is installed.
func (s *Service) knownFeature(f string) bool {
	if isService(f) {
		return s.installed("connectors")
	}
	return s.installed(f)
}

// catalogEntry is one scope key with its tools.
type catalogEntry struct {
	Feature    string `json:"feature"`
	Label      string `json:"label"`
	ReadTools  int    `json:"read_tools"`
	WriteTools int    `json:"write_tools"`
	Platform   string `json:"platform,omitempty"` // a connected platform account: its provider id
}

// featureCatalog lists the app's features that give agents tools, by label.
// This feature's own (read-only, about the scope itself) is left out.
func (s *Service) featureCatalog() []catalogEntry {
	byFeature := map[string]*catalogEntry{}
	for _, t := range s.toolIndex() {
		if t.feature == s.Name() || isService(t.feature) {
			continue
		}
		e := byFeature[t.feature]
		if e == nil {
			e = &catalogEntry{Feature: t.feature, Label: featureLabel(t.feature)}
			byFeature[t.feature] = e
		}
		if t.ReadOnly {
			e.ReadTools++
		} else {
			e.WriteTools++
		}
	}
	out := make([]catalogEntry, 0, len(byFeature))
	for _, e := range byFeature {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Label) < strings.ToLower(out[j].Label) })
	return out
}

// serviceCatalog lists the caller's connected external services (dynamic
// tools grouped by scope key, labelled with Tool.Source) and connected
// platform accounts ("Google · sam@acme.com").
func (s *Service) serviceCatalog(ctx context.Context, caller *auth.Identity) []catalogEntry {
	byFeature := map[string]*catalogEntry{}
	for _, t := range s.dynamicTools(ctx, caller) {
		if !isService(t.feature) {
			continue
		}
		e := byFeature[t.feature]
		if e == nil {
			label := strings.TrimSpace(t.Source)
			if label == "" {
				label = featureLabel(t.feature)
			}
			e = &catalogEntry{Feature: t.feature, Label: truncate(label, 60)}
			byFeature[t.feature] = e
		}
		if t.ReadOnly {
			e.ReadTools++
		} else {
			e.WriteTools++
		}
	}
	out := make([]catalogEntry, 0, len(byFeature))
	for _, e := range byFeature {
		out = append(out, *e)
	}
	// Connected platform accounts (app.ConnectionDirectory): every feature's
	// tools that act on that account are capped by its level.
	if dir := app.FindConnectionDirectory(s.d.Modules); dir != nil && caller != nil {
		conns, err := dir.All(ctx, caller.UserID)
		if err != nil {
			slog.WarnContext(ctx, "agents: connected accounts unavailable", "err", err)
		}
		for _, c := range conns {
			key := app.ConnectionScope(c.ID)
			if byFeature[key] != nil {
				continue
			}
			out = append(out, catalogEntry{Feature: key, Label: truncate(app.ConnectionLabel(c), 60), Platform: c.Provider})
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Label) < strings.ToLower(out[j].Label) })
	return out
}

// toolTitle turns a tool name into a short action: calendar_create_event →
// "Create event" (the feature prefix dropped).
func toolTitle(feature, name string) string {
	n := strings.TrimPrefix(name, feature+"_")
	if i := strings.Index(n, "__"); i >= 0 { // connector tools: <slug>__<tool>
		n = n[i+2:]
	}
	n = strings.TrimSpace(strings.NewReplacer("_", " ", "-", " ").Replace(n))
	if n == "" {
		return name
	}
	return strings.ToUpper(n[:1]) + n[1:]
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
