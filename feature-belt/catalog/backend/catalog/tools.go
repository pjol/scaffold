package catalog

import (
	"context"
	"encoding/json"
	"strings"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
)

const maxToolNames = 100

// Tools: read-only catalog access for agents (the mcp and assistant
// features, and agent-web's WebMCP).
func (s *Service) Tools() []app.Tool {
	return []app.Tool{
		{
			Name:        "catalog_search",
			Description: "Search the reference catalog by name or alias. Best matches first: exact name, names starting with the query, later words, names containing it, then aliases. Returns up to 50 items with key, name, category, subtitle, attributes and any reference price (a decimal string in its currency).",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","minLength":2,"description":"Name or part of one"},"category":{"type":"string","description":"Only this category, exactly as listed"},"include_retired":{"type":"boolean","description":"Include items the source no longer lists"},"limit":{"type":"integer","minimum":1,"maximum":50}},"required":["query"],"additionalProperties":false}`),
			ReadOnly:    true,
			Handler: func(ctx context.Context, _ *auth.Identity, in json.RawMessage) (any, error) {
				var a struct {
					Query          string `json:"query"`
					Category       string `json:"category"`
					IncludeRetired bool   `json:"include_retired"`
					Limit          int    `json:"limit"`
				}
				if err := json.Unmarshal(in, &a); err != nil {
					return nil, httpx.BadRequest("Invalid input.")
				}
				x, err := s.loadedIndex()
				if err != nil {
					return nil, err
				}
				hits := x.search(a.Query, Filter{Category: strings.TrimSpace(a.Category), Retired: a.IncludeRetired}, a.Limit)
				items := make([]Item, 0, len(hits))
				for _, h := range hits {
					items = append(items, x.item(h.i))
				}
				return map[string]any{"items": items}, nil
			},
		},
		{
			Name:        "catalog_resolve",
			Description: "Resolve names (as people type or paste them) to catalog items: exact name, then alias, then a close spelling (match \"fuzzy\": confirm it with the person). Unresolved names come back with reason unknown or ambiguous and up to 5 candidates.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"names":{"type":"array","items":{"type":"string"},"minItems":1,"maxItems":100}},"required":["names"],"additionalProperties":false}`),
			ReadOnly:    true,
			Handler: func(ctx context.Context, _ *auth.Identity, in json.RawMessage) (any, error) {
				var a struct {
					Names []string `json:"names"`
				}
				if err := json.Unmarshal(in, &a); err != nil || len(a.Names) == 0 || len(a.Names) > maxToolNames {
					return nil, httpx.BadRequest("Send 1 to 100 names.")
				}
				x, err := s.loadedIndex()
				if err != nil {
					return nil, err
				}
				return map[string]any{"results": s.resolveAll(x, a.Names)}, nil
			},
		},
		{
			Name:        "catalog_get_item",
			Description: "Get one catalog item by key: name, aliases, category, subtitle, image URL, attributes, reference price and whether it is retired.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"key":{"type":"string","description":"The item's key"}},"required":["key"],"additionalProperties":false}`),
			ReadOnly:    true,
			Handler: func(ctx context.Context, _ *auth.Identity, in json.RawMessage) (any, error) {
				var a struct {
					Key string `json:"key"`
				}
				if err := json.Unmarshal(in, &a); err != nil || strings.TrimSpace(a.Key) == "" {
					return nil, httpx.BadRequest("key is required.")
				}
				x, err := s.loadedIndex()
				if err != nil {
					return nil, err
				}
				i, ok := x.byKey[strings.TrimSpace(a.Key)]
				if !ok {
					return nil, httpx.NotFound("No item with that key.")
				}
				return x.item(i), nil
			},
		},
	}
}

// GlobalSearch is the catalog's app.SearchProvider. It is a module of its own
// (feature_catalog.go registers it beside the Service) because
// Service.Search is the catalog's Go API, with another signature.
type GlobalSearch struct{ s *Service }

func NewGlobalSearch(s *Service) *GlobalSearch { return &GlobalSearch{s: s} }

func (g *GlobalSearch) Name() string { return "catalog" }

var rankScore = [...]float64{rankExact: 0.9, rankPrefix: 0.8, rankWord: 0.7, rankSubstring: 0.6, rankAlias: 0.55, rankAliasSub: 0.45}

// Search implements app.SearchProvider: active items by name, then alias,
// ranked as Service.Search ranks them. The catalog is reference data every
// signed-in person may read.
func (g *GlobalSearch) Search(ctx context.Context, _ *auth.Identity, query string, limit int) ([]app.SearchResult, error) {
	x, err := g.s.loadedIndex()
	if err != nil {
		return nil, err
	}
	hits := x.search(query, Filter{}, limit)
	out := make([]app.SearchResult, 0, len(hits))
	for _, h := range hits {
		r := x.recs[h.i]
		out = append(out, app.SearchResult{
			Kind: "catalog.item", ID: r.key, Title: r.name, Snippet: r.subtitle, Context: r.category,
			Link: "/catalog/" + r.key, Icon: "notebook", Score: rankScore[h.rank], At: r.updatedAt,
		})
	}
	return out, nil
}
