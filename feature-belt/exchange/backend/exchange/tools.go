package exchange

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/exchange/engine"
	"scaffoldapp/backend/internal/httpx"
)

// Tools exposes the exchange to agents, run as the caller: market data and
// the caller's own account to read; placing and cancelling orders and closing
// positions as writes, which agent scopes gate (Suggest shows the order for
// approval first). There is no tool that moves collateral.
func (s *Service) Tools() []app.Tool {
	return []app.Tool{
		{
			Name:        "exchange_list_markets",
			Description: "List the exchange's perpetual markets with mark price, index price, 24h change and volume, funding rate (per hour) and open interest. Optionally filter by text or category.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"query": {"type": "string", "description": "Matches symbol, name or category."},
					"category": {"type": "string"}
				},
				"additionalProperties": false
			}`),
			ReadOnly: true,
			Handler:  s.toolMarkets,
		},
		{
			Name:        "exchange_get_market",
			Description: "One market by symbol: parameters (max leverage, tick size, fees, minimum order), live stats, the top 10 book levels a side and the 10 latest trades.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {"symbol": {"type": "string"}},
				"required": ["symbol"],
				"additionalProperties": false
			}`),
			ReadOnly: true,
			Handler:  s.toolMarket,
		},
		{
			Name:        "exchange_get_candles",
			Description: "Price candles (open, high, low, close, volume) for a market, oldest first. Interval 1m, 5m, 15m, 1h, 4h or 1d; t is the bucket's open time in unix milliseconds.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"symbol": {"type": "string"},
					"interval": {"type": "string", "enum": ["1m", "5m", "15m", "1h", "4h", "1d"]},
					"limit": {"type": "integer", "minimum": 1, "maximum": 200}
				},
				"required": ["symbol"],
				"additionalProperties": false
			}`),
			ReadOnly: true,
			Handler:  s.toolCandles,
		},
		{
			Name:        "exchange_get_account",
			Description: "The user's trading account: balance, account value, PnL, margin used, free collateral, open positions (with liquidation prices) and open orders. Amounts are in the quote currency.",
			InputSchema: json.RawMessage(`{"type": "object", "properties": {}, "additionalProperties": false}`),
			ReadOnly:    true,
			Handler:     s.toolAccount,
		},
		{
			Name:        "exchange_list_fills",
			Description: "The user's most recent fills (executed trades), newest first.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {"limit": {"type": "integer", "minimum": 1, "maximum": 50}},
				"additionalProperties": false
			}`),
			ReadOnly: true,
			Handler:  s.toolFills,
		},
		{
			Name: "exchange_place_order",
			Description: "Place an order for the user. type market fills now against the book within slippage_bps of the mark (default 500); " +
				"type limit rests at px. Give the size in base units (sz) or in the quote currency (sz_quote). reduce_only orders only shrink a position; " +
				"post_only limits never take liquidity. Leverage applies to the whole position in that market (1 to the market's max).",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"symbol": {"type": "string"},
					"side": {"type": "string", "enum": ["buy", "sell"]},
					"type": {"type": "string", "enum": ["market", "limit"]},
					"sz": {"type": "string", "description": "Size in base units, a decimal string."},
					"sz_quote": {"type": "string", "description": "Size in the quote currency, a decimal string (instead of sz)."},
					"px": {"type": "string", "description": "Limit price, a decimal string (limit orders)."},
					"leverage": {"type": "integer", "minimum": 1},
					"reduce_only": {"type": "boolean"},
					"post_only": {"type": "boolean"},
					"slippage_bps": {"type": "integer", "minimum": 1, "maximum": 5000}
				},
				"required": ["symbol", "side", "type"],
				"additionalProperties": false
			}`),
			Handler: s.toolPlace,
			Target:  s.marketTarget,
			Preview: s.previewPlace,
		},
		{
			Name:        "exchange_cancel_order",
			Description: "Cancel one of the user's open orders by id.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {"order_id": {"type": "string", "format": "uuid"}},
				"required": ["order_id"],
				"additionalProperties": false
			}`),
			Handler: s.toolCancel,
			Target: func(raw json.RawMessage) (string, string) {
				var in struct {
					OrderID string `json:"order_id"`
				}
				_ = json.Unmarshal(raw, &in)
				return "exchange.order", in.OrderID
			},
			Preview: func(_ context.Context, _ *auth.Identity, raw json.RawMessage) (app.ToolPreview, error) {
				var in struct {
					OrderID string `json:"order_id"`
				}
				_ = json.Unmarshal(raw, &in)
				return app.ToolPreview{Summary: "Cancel order " + in.OrderID, Link: "/portfolio"}, nil
			},
		},
		{
			Name:        "exchange_close_position",
			Description: "Close the user's position in a market at market price (all of it, or sz base units of it).",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {"symbol": {"type": "string"}, "sz": {"type": "string"}},
				"required": ["symbol"],
				"additionalProperties": false
			}`),
			Handler: s.toolClose,
			Target:  s.marketTarget,
			Preview: func(_ context.Context, _ *auth.Identity, raw json.RawMessage) (app.ToolPreview, error) {
				var in struct {
					Symbol, Sz string
				}
				_ = json.Unmarshal(raw, &in)
				what := "all of"
				if in.Sz != "" {
					what = in.Sz + " of"
				}
				sym := strings.ToUpper(in.Symbol)
				return app.ToolPreview{Summary: fmt.Sprintf("Close %s the %s position at market", what, sym), Link: "/markets/" + sym}, nil
			},
		},
	}
}

func decodeInput(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return httpx.BadRequest("Invalid input: " + err.Error())
	}
	return nil
}

// marketTarget names the market a write acts on (kind exchange.market, id the market id).
func (s *Service) marketTarget(raw json.RawMessage) (string, string) {
	var in struct {
		Symbol string `json:"symbol"`
	}
	_ = json.Unmarshal(raw, &in)
	id, err := marketIDBySymbol(context.Background(), s.d.Pool, in.Symbol)
	if err != nil {
		return "exchange.market", strings.ToUpper(in.Symbol)
	}
	return "exchange.market", id
}

func (s *Service) toolMarkets(_ context.Context, _ *auth.Identity, raw json.RawMessage) (any, error) {
	var in struct{ Query, Category string }
	if err := decodeInput(raw, &in); err != nil {
		return nil, err
	}
	eng, err := s.running()
	if err != nil {
		return nil, err
	}
	all := eng.Markets()
	return map[string]any{"quote_currency": s.quoteCurrency, "categories": sortedCategories(all),
		"markets": filterMarkets(all, in.Category, in.Query)}, nil
}

func (s *Service) toolMarket(_ context.Context, _ *auth.Identity, raw json.RawMessage) (any, error) {
	var in struct{ Symbol string }
	if err := decodeInput(raw, &in); err != nil {
		return nil, err
	}
	eng, err := s.running()
	if err != nil {
		return nil, err
	}
	sym := strings.ToUpper(strings.TrimSpace(in.Symbol))
	m, ok := eng.Market(sym)
	if !ok {
		return nil, httpx.NotFound("Unknown market " + sym + ".")
	}
	book, _ := eng.Book(sym, 10)
	trades, _ := eng.Trades(sym, 10)
	return map[string]any{"market": m, "book": book, "trades": trades, "quote_currency": s.quoteCurrency}, nil
}

func (s *Service) toolCandles(ctx context.Context, _ *auth.Identity, raw json.RawMessage) (any, error) {
	var in struct {
		Symbol, Interval string
		Limit            int
	}
	if err := decodeInput(raw, &in); err != nil {
		return nil, err
	}
	if in.Limit <= 0 || in.Limit > 200 {
		in.Limit = 60
	}
	if in.Interval == "" {
		in.Interval = "1h"
	}
	page, err := s.Candles(ctx, in.Symbol, in.Interval, 0, in.Limit)
	if err != nil {
		return nil, err
	}
	return map[string]any{"symbol": strings.ToUpper(in.Symbol), "interval": in.Interval, "candles": page.Items}, nil
}

func (s *Service) callerAccount(caller *auth.Identity) (*engine.Engine, string, error) {
	eng, err := s.running()
	if err != nil {
		return nil, "", err
	}
	acct, _, err := eng.EnsureUser(caller.UserID.String())
	if err != nil {
		return nil, "", apiErr(err)
	}
	return eng, acct, nil
}

func (s *Service) toolAccount(_ context.Context, caller *auth.Identity, _ json.RawMessage) (any, error) {
	eng, acct, err := s.callerAccount(caller)
	if err != nil {
		return nil, err
	}
	sum, err := eng.Summary(acct)
	if err != nil {
		return nil, apiErr(err)
	}
	return map[string]any{"quote_currency": s.quoteCurrency, "account": sum}, nil
}

func (s *Service) toolFills(ctx context.Context, caller *auth.Identity, raw json.RawMessage) (any, error) {
	var in struct{ Limit int }
	if err := decodeInput(raw, &in); err != nil {
		return nil, err
	}
	if in.Limit <= 0 || in.Limit > 50 {
		in.Limit = 20
	}
	var acct string
	err := s.d.Pool.QueryRow(ctx, `SELECT id::text FROM exchange_accounts WHERE user_id = $1`, caller.UserID).Scan(&acct)
	if err != nil {
		return map[string]any{"fills": []engine.Fill{}}, nil
	}
	recs, err := s.store.Fills(ctx, acct, histCursor{}, in.Limit)
	if err != nil {
		return nil, err
	}
	out := make([]engine.Fill, 0, len(recs))
	for _, f := range recs {
		out = append(out, engine.Fill{ID: f.ID, OrderID: f.OrderID, Symbol: f.Symbol, Side: f.Side, Px: f.Px.String(), Sz: f.Sz.String(),
			Fee: f.Fee.String(), RealizedPnl: f.RealizedPnl.String(), Liquidity: f.Liquidity, Liquidation: f.Liquidation,
			CreatedAt: time.UnixMilli(f.Ts).UTC()})
	}
	return map[string]any{"fills": out}, nil
}

func (s *Service) toolPlace(_ context.Context, caller *auth.Identity, raw json.RawMessage) (any, error) {
	var req engine.PlaceOrder
	if err := decodeInput(raw, &req); err != nil {
		return nil, err
	}
	eng, acct, err := s.callerAccount(caller)
	if err != nil {
		return nil, err
	}
	o, fills, err := eng.PlaceOrderWith(acct, req, engine.PlaceOpts{Source: engine.SourceAgent})
	if err != nil {
		return nil, apiErr(err)
	}
	if fills == nil {
		fills = []engine.Fill{}
	}
	return OrderResult{Order: o, Fills: fills}, nil
}

func (s *Service) previewPlace(_ context.Context, _ *auth.Identity, raw json.RawMessage) (app.ToolPreview, error) {
	var req engine.PlaceOrder
	_ = json.Unmarshal(raw, &req)
	sym := strings.ToUpper(req.Symbol)
	size := req.Sz + " " + sym
	if req.Sz == "" {
		size = req.SzQuote + " " + s.quoteCurrency + " of " + sym
	}
	verb := "Buy"
	if req.Side == engine.Sell {
		verb = "Sell"
	}
	where := "at market"
	if req.Type == engine.TypeLimit {
		where = "at " + req.Px + " (limit)"
	}
	sum := fmt.Sprintf("%s %s %s", verb, size, where)
	if req.Leverage > 0 {
		sum += fmt.Sprintf(", %d× leverage", req.Leverage)
	}
	if req.ReduceOnly {
		sum += ", reduce only"
	}
	return app.ToolPreview{Summary: sum, Link: "/markets/" + sym}, nil
}

func (s *Service) toolCancel(_ context.Context, caller *auth.Identity, raw json.RawMessage) (any, error) {
	var in struct {
		OrderID string `json:"order_id"`
	}
	if err := decodeInput(raw, &in); err != nil {
		return nil, err
	}
	eng, acct, err := s.callerAccount(caller)
	if err != nil {
		return nil, err
	}
	o, err := eng.Cancel(acct, in.OrderID)
	if err != nil {
		return nil, apiErr(err)
	}
	return map[string]any{"order": o}, nil
}

func (s *Service) toolClose(_ context.Context, caller *auth.Identity, raw json.RawMessage) (any, error) {
	var in struct{ Symbol, Sz string }
	if err := decodeInput(raw, &in); err != nil {
		return nil, err
	}
	eng, acct, err := s.callerAccount(caller)
	if err != nil {
		return nil, err
	}
	o, fills, err := eng.ClosePositionWith(acct, in.Symbol, in.Sz, engine.PlaceOpts{Source: engine.SourceAgent})
	if err != nil {
		return nil, apiErr(err)
	}
	if fills == nil {
		fills = []engine.Fill{}
	}
	return OrderResult{Order: o, Fills: fills}, nil
}
