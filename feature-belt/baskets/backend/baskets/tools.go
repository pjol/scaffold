package baskets

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
)

// KindBasket is the agent-scope resource kind of a basket.
const KindBasket = "baskets.basket"

const inputSchema = `"text": {"type": "string", "maxLength": 20000, "description": "One market per line: \"2 Gold\", \"Silver x3\", \"BTC-USD, 1\" or CSV with a header. Quantity defaults to 1."},
					"picks": {"type": "array", "maxItems": 250, "items": {"type": "object", "properties": {"symbol": {"type": "string"}, "quantity": {"type": "integer", "minimum": 1, "maximum": 10000}}, "required": ["symbol", "quantity"], "additionalProperties": false}}`

// Tools: preview, list and read baskets; create as a write agent scopes gate
// (Suggest shows the preview first). Creating a market is not a trade.
func (s *Service) Tools() []app.Tool {
	return []app.Tool{
		{
			Name:        "baskets_preview",
			Description: "Check a basket without creating it: how each line resolved to a market, warnings, the basket's value and largest share, blockers in words, the market spec it would get, and whether the same basket exists.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					` + inputSchema + `
				},
				"additionalProperties": false
			}`),
			ReadOnly: true,
			Handler:  s.toolPreview,
		},
		{
			Name:        "baskets_list",
			Description: "List baskets: new or popular (people's volume and entries), live or archived, optionally matching text; or only the caller's (mine).",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"sort": {"type": "string", "enum": ["new", "popular", "mine"]},
					"status": {"type": "string", "enum": ["live", "archived", "all"]},
					"query": {"type": "string", "maxLength": 100},
					"limit": {"type": "integer", "minimum": 1, "maximum": 50}
				},
				"additionalProperties": false
			}`),
			ReadOnly: true,
			Handler:  s.toolList,
		},
		{
			Name:        "baskets_get",
			Description: "One basket by symbol: its lines with current prices and weights, value, the published basket price, its market's status and spec, entries and creator.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {"symbol": {"type": "string"}},
				"required": ["symbol"],
				"additionalProperties": false
			}`),
			ReadOnly: true,
			Handler:  s.toolGet,
		},
		{
			Name:        "baskets_create",
			Description: "Create a basket market from a list of markets, or join the existing basket with the same lines (reopening it if archived). Lists a market people can trade; it doesn't trade. Preview first.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					` + inputSchema + `,
					"name": {"type": "string", "maxLength": 48, "description": "3–48 characters; generated from the largest lines when empty."},
					"description": {"type": "string", "maxLength": 500},
					"ends_on": {"type": "string", "description": "Optional end date, YYYY-MM-DD."}
				},
				"additionalProperties": false
			}`),
			ReadOnly: false,
			Handler:  s.toolCreate,
			// A new basket has no id yet: the kind alone (the feature's level decides).
			Target:  func(json.RawMessage) (string, string) { return KindBasket, "" },
			Preview: s.previewCreate,
		},
	}
}

func decodeTool(raw json.RawMessage, dst any) error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return httpx.BadRequest("Invalid input: " + err.Error())
	}
	return nil
}

func (s *Service) toolPreview(ctx context.Context, caller *auth.Identity, raw json.RawMessage) (any, error) {
	var in Input
	if err := decodeTool(raw, &in); err != nil {
		return nil, err
	}
	return s.Preview(ctx, caller.UserID, in)
}

func (s *Service) toolList(ctx context.Context, caller *auth.Identity, raw json.RawMessage) (any, error) {
	var in struct {
		Sort   string `json:"sort"`
		Status string `json:"status"`
		Query  string `json:"query"`
		Limit  int    `json:"limit"`
	}
	if err := decodeTool(raw, &in); err != nil {
		return nil, err
	}
	limit := in.Limit
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	if in.Sort == "mine" {
		rows, err := s.mine(ctx, caller.UserID, limit, nil)
		if err != nil {
			return nil, err
		}
		out := make([]Summary, len(rows))
		ptrs := make([]*Summary, len(rows))
		for i, m := range rows {
			out[i] = s.summary(m.basketRow, caller.UserID)
			ptrs[i] = &out[i]
		}
		if len(out) > limit {
			out, ptrs = out[:limit], ptrs[:limit]
		}
		return map[string]any{"items": out}, fillLines(ctx, s.d.Pool, ptrs)
	}
	lq := listQuery{sort: in.Sort, status: in.Status, q: in.Query, limit: limit}
	if lq.sort != "popular" {
		lq.sort = "new"
	}
	switch lq.status {
	case "live", StatusArchived, "all":
	default:
		lq.status = "live"
	}
	rows, err := s.list(ctx, lq)
	if err != nil {
		return nil, err
	}
	pg, err := s.page(ctx, rows, caller.UserID, limit, lq.sort)
	if err != nil {
		return nil, err
	}
	return map[string]any{"items": pg.Items, "more": pg.NextCursor != nil}, nil
}

func (s *Service) toolGet(ctx context.Context, caller *auth.Identity, raw json.RawMessage) (any, error) {
	var in struct {
		Symbol string `json:"symbol"`
	}
	if err := decodeTool(raw, &in); err != nil {
		return nil, err
	}
	b, err := basketBy(ctx, s.d.Pool, "symbol", in.Symbol)
	if err != nil {
		return nil, err
	}
	return s.detail(ctx, b, caller.UserID)
}

func (s *Service) toolCreate(ctx context.Context, caller *auth.Identity, raw json.RawMessage) (any, error) {
	if !caller.Active() {
		return nil, httpx.ErrSuspended
	}
	var req CreateRequest
	if err := decodeTool(raw, &req); err != nil {
		return nil, err
	}
	return s.Create(ctx, caller.UserID, "agent", req)
}

// previewCreate is what a person reviews when their agent scope is Suggest.
func (s *Service) previewCreate(ctx context.Context, caller *auth.Identity, raw json.RawMessage) (app.ToolPreview, error) {
	var req CreateRequest
	if err := decodeTool(raw, &req); err != nil {
		return app.ToolPreview{}, err
	}
	p, err := s.Preview(ctx, caller.UserID, req.Input)
	if err != nil {
		return app.ToolPreview{}, err
	}
	var lines []string
	for _, l := range p.Basket {
		lines = append(lines, fmt.Sprintf("%d × %s (%s)", l.Quantity, l.Name, l.Symbol))
	}
	name := strings.TrimSpace(req.Name)
	sum := "Create a basket"
	switch {
	case p.Existing != nil:
		sum = fmt.Sprintf("Join %s (%s)", p.Existing.Name, p.Existing.Symbol)
		if p.Existing.Status == StatusArchived {
			sum = fmt.Sprintf("Reopen %s (%s)", p.Existing.Name, p.Existing.Symbol)
		}
	case name != "":
		sum = fmt.Sprintf("Create the basket “%s”", name)
	}
	after := strings.Join(lines, "\n")
	if p.Value != "" {
		after += "\nValue " + p.Value
	}
	if len(p.Blockers) > 0 {
		after += "\nBlocked: " + strings.Join(p.Blockers, " ")
	}
	link := "/baskets/new"
	if p.Symbol != nil && p.Existing != nil {
		link = "/baskets/" + *p.Symbol
	}
	return app.ToolPreview{Summary: sum, After: after, Link: link}, nil
}
