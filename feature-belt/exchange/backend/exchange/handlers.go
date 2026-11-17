package exchange

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/exchange/engine"
	"scaffoldapp/backend/internal/httpx"
)

// apiErr maps engine errors onto the API error envelope.
func apiErr(err error) error {
	var ee *engine.Error
	if errors.As(err, &ee) {
		return httpx.NewError(ee.Status, ee.Code, ee.Message)
	}
	return err
}

func symbolParam(r *http.Request) string { return strings.ToUpper(chi.URLParam(r, "symbol")) }

func intQuery(r *http.Request, name string, def, lo, hi int) int {
	v, err := strconv.Atoi(r.URL.Query().Get(name))
	if err != nil {
		return def
	}
	return max(lo, min(v, hi))
}

// Config is GET /v1/exchange/config.
type Config struct {
	QuoteCurrency       string   `json:"quote_currency"`
	QuoteDecimals       int      `json:"quote_decimals"`
	FaucetAmount        *string  `json:"faucet_amount"` // null: no faucet
	FaucetCooldownHours float64  `json:"faucet_cooldown_hours"`
	SimEnabled          bool     `json:"sim_enabled"`
	Intervals           []string `json:"intervals"`
	FundingIntervalMins int      `json:"funding_interval_minutes"`
}

func (s *Service) config() Config {
	c := Config{QuoteCurrency: s.quoteCurrency, QuoteDecimals: int(s.params.QuoteDecimals), SimEnabled: s.simEnabled,
		Intervals: engine.IntervalNames, FundingIntervalMins: int(s.params.FundingInterval.Minutes())}
	if s.params.FaucetAmount.IsPositive() {
		a := s.params.FaucetAmount.StringFixed(s.params.QuoteDecimals)
		c.FaucetAmount = &a
		c.FaucetCooldownHours = s.params.FaucetCooldown.Hours()
	}
	return c
}

func (s *Service) configHandler(w http.ResponseWriter, _ *http.Request) error {
	return okJSON(w, s.config())
}

// ---- market data ----

func filterMarkets(ms []engine.MarketView, category, query string) []engine.MarketView {
	q := strings.ToLower(strings.TrimSpace(query))
	out := []engine.MarketView{}
	for _, m := range ms {
		if category != "" && !strings.EqualFold(m.Category, category) {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(m.Symbol+" "+m.Name+" "+m.Category), q) {
			continue
		}
		out = append(out, m)
	}
	return out
}

func (s *Service) marketsHandler(w http.ResponseWriter, r *http.Request) error {
	eng, err := s.running()
	if err != nil {
		return err
	}
	items := filterMarkets(eng.Markets(), r.URL.Query().Get("category"), r.URL.Query().Get("q"))
	return okJSON(w, map[string]any{"items": items})
}

func (s *Service) marketHandler(w http.ResponseWriter, r *http.Request) error {
	eng, err := s.running()
	if err != nil {
		return err
	}
	m, ok := eng.Market(symbolParam(r))
	if !ok {
		return httpx.NotFound("Unknown market.")
	}
	return okJSON(w, m)
}

func (s *Service) bookHandler(w http.ResponseWriter, r *http.Request) error {
	eng, err := s.running()
	if err != nil {
		return err
	}
	b, ok := eng.Book(symbolParam(r), intQuery(r, "depth", 20, 1, 100))
	if !ok {
		return httpx.NotFound("Unknown market.")
	}
	return okJSON(w, b)
}

func (s *Service) tradesHandler(w http.ResponseWriter, r *http.Request) error {
	eng, err := s.running()
	if err != nil {
		return err
	}
	t, ok := eng.Trades(symbolParam(r), intQuery(r, "limit", 50, 1, 200))
	if !ok {
		return httpx.NotFound("Unknown market.")
	}
	return okJSON(w, map[string]any{"items": t})
}

// CandlePage is GET /v1/exchange/markets/{symbol}/candles: candles oldest
// first; next_before pages further back (pass it as ?before=), null at the
// start of the history.
type CandlePage struct {
	Items      []engine.Candle `json:"items"`
	NextBefore *int64          `json:"next_before"`
}

type marketPrecision struct {
	id           string
	pxDec, szDec int32
}

func (s *Service) precision(ctx context.Context, symbol string) (marketPrecision, error) {
	var p marketPrecision
	var tick string
	err := s.d.Pool.QueryRow(ctx, `SELECT id::text, tick_size::text, sz_decimals FROM exchange_markets WHERE symbol = $1`, symbol).
		Scan(&p.id, &tick, &p.szDec)
	if err != nil {
		return p, httpx.NotFound("Unknown market.")
	}
	t := dec(tick)
	p.pxDec = 0
	if t.Exponent() < 0 {
		p.pxDec = -t.Exponent()
	}
	return p, nil
}

func (p marketPrecision) view(c engine.CandleRec, backfill bool) engine.Candle {
	v := c.V.StringFixed(p.szDec)
	if backfill {
		v = "0"
	}
	return engine.Candle{T: c.T, O: c.O.StringFixed(p.pxDec), H: c.H.StringFixed(p.pxDec), L: c.L.StringFixed(p.pxDec),
		C: c.C.StringFixed(p.pxDec), V: v, Backfill: backfill}
}

// Candles returns limit candles of interval that open before beforeMs (0 =
// the newest, including the open bucket), oldest first. 1d pages continue
// into the price feed's daily history before the market's first live day.
func (s *Service) Candles(ctx context.Context, symbol, interval string, beforeMs int64, limit int) (CandlePage, error) {
	if interval == "" {
		interval = "1m"
	}
	ivMs, ok := engine.Intervals[interval]
	if !ok {
		return CandlePage{}, httpx.BadRequest("interval must be one of 1m, 5m, 15m, 1h, 4h, 1d.")
	}
	limit = max(1, min(limit, 1000))
	p, err := s.precision(ctx, strings.ToUpper(symbol))
	if err != nil {
		return CandlePage{}, err
	}
	live := beforeMs <= 0
	if live {
		beforeMs = (time.Now().UnixMilli()/ivMs + 1) * ivMs
	} else {
		beforeMs = beforeMs / ivMs * ivMs
	}
	hist, err := s.store.Candles(ctx, p.id, strings.ToUpper(symbol), ivMs, beforeMs, limit)
	if err != nil {
		return CandlePage{}, err
	}
	if eng := s.engine(); live && eng != nil {
		if cur, ok := eng.CurrentCandle(strings.ToUpper(symbol), ivMs); ok {
			hist = engine.MergeCandles(hist, cur)
		}
	}
	if len(hist) > limit {
		hist = hist[len(hist)-limit:]
	}
	page := CandlePage{Items: make([]engine.Candle, 0, len(hist))}
	earliest, hasLive, err := s.store.EarliestCandle(ctx, p.id)
	if err != nil {
		return CandlePage{}, err
	}
	// The query scanned [from, beforeMs); the next page ends where this one starts.
	from := (beforeMs/ivMs - int64(limit)) * ivMs
	next := from
	if len(hist) >= limit {
		next = hist[0].T
	}
	more := hasLive && earliest < next
	if interval == "1d" && len(hist) < limit && !(hasLive && earliest < from) {
		// at the start of live history: continue into the feed's daily history
		bound := beforeMs
		if len(hist) > 0 {
			bound = hist[0].T
		}
		need := limit - len(hist)
		bars, err := s.store.Backfill(ctx, p.id, bound, need)
		if err != nil {
			return CandlePage{}, err
		}
		for _, b := range bars {
			page.Items = append(page.Items, p.view(b, true))
		}
		more = len(bars) == need
		if len(bars) > 0 {
			next = bars[0].T
		}
	}
	for _, c := range hist {
		page.Items = append(page.Items, p.view(c, false))
	}
	if more {
		page.NextBefore = &next
	}
	return page, nil
}

func (s *Service) candlesHandler(w http.ResponseWriter, r *http.Request) error {
	before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	page, err := s.Candles(r.Context(), symbolParam(r), r.URL.Query().Get("interval"), before, intQuery(r, "limit", 300, 1, 1000))
	if err != nil {
		return err
	}
	return okJSON(w, page)
}

// sparkCache keeps sparklines for 30 s.
type sparkCache struct {
	mu sync.Mutex
	m  map[string]sparkEntry
}

type sparkEntry struct {
	at   time.Time
	data map[string][]string
}

func (s *Service) sparklinesHandler(w http.ResponseWriter, r *http.Request) error {
	iv := r.URL.Query().Get("interval")
	if iv == "" {
		iv = "1h"
	}
	if _, ok := engine.Intervals[iv]; !ok {
		return httpx.BadRequest("interval must be one of 1m, 5m, 15m, 1h, 4h, 1d.")
	}
	points := intQuery(r, "points", 24, 2, 200)
	key := iv + "|" + strconv.Itoa(points)
	s.sp.mu.Lock()
	if s.sp.m == nil {
		s.sp.m = map[string]sparkEntry{}
	}
	e, ok := s.sp.m[key]
	s.sp.mu.Unlock()
	if !ok || time.Since(e.at) > 30*time.Second {
		rows, err := s.d.Pool.Query(r.Context(), `SELECT symbol FROM exchange_markets ORDER BY sort, symbol`)
		if err != nil {
			return err
		}
		var syms []string
		for rows.Next() {
			var sym string
			if err := rows.Scan(&sym); err != nil {
				rows.Close()
				return err
			}
			syms = append(syms, sym)
		}
		rows.Close()
		data := make(map[string][]string, len(syms))
		for _, sym := range syms {
			page, err := s.Candles(r.Context(), sym, iv, 0, points)
			if err != nil {
				return err
			}
			closes := make([]string, len(page.Items))
			for i, c := range page.Items {
				closes[i] = c.C
			}
			data[sym] = closes
		}
		e = sparkEntry{at: time.Now(), data: data}
		s.sp.mu.Lock()
		s.sp.m[key] = e
		s.sp.mu.Unlock()
	}
	w.Header().Set("Cache-Control", "public, max-age=30")
	return okJSON(w, map[string]any{"sparklines": e.data})
}

// ---- account ----

// userAccount returns the engine and the caller's account id, creating the
// account on first use.
func (s *Service) userAccount(r *http.Request) (*engine.Engine, string, error) {
	eng, err := s.running()
	if err != nil {
		return nil, "", err
	}
	id := auth.MustFromContext(r.Context())
	acct, _, err := eng.EnsureUser(id.UserID.String())
	if err != nil {
		return nil, "", apiErr(err)
	}
	return eng, acct, nil
}

// storedAccount returns the caller's account id from the database ("" if none).
func (s *Service) storedAccount(ctx context.Context) (string, error) {
	id := auth.MustFromContext(ctx)
	var acct string
	err := s.d.Pool.QueryRow(ctx, `SELECT id::text FROM exchange_accounts WHERE user_id = $1`, id.UserID).Scan(&acct)
	if err != nil && !db.IsNoRows(err) {
		return "", err
	}
	return acct, nil
}

func (s *Service) accountHandler(w http.ResponseWriter, r *http.Request) error {
	eng, acct, err := s.userAccount(r)
	if err != nil {
		return err
	}
	sum, err := eng.Summary(acct)
	if err != nil {
		return apiErr(err)
	}
	return okJSON(w, sum)
}

func pageCursor(r *http.Request) (histCursor, int, error) {
	pp := httpx.ParsePage(r)
	var c histCursor
	if err := httpx.DecodeCursor(pp.Cursor, &c); err != nil {
		return c, 0, err
	}
	return c, pp.Limit, nil
}

func (s *Service) ordersHandler(w http.ResponseWriter, r *http.Request) error {
	status := r.URL.Query().Get("status")
	switch status {
	case "", engine.StatusOpen, engine.StatusFilled, engine.StatusCancelled:
	default:
		return httpx.BadRequest("status must be open, filled or cancelled.")
	}
	c, limit, err := pageCursor(r)
	if err != nil {
		return err
	}
	acct, err := s.storedAccount(r.Context())
	if err != nil || acct == "" {
		return emptyPage[engine.OrderView](w, err)
	}
	recs, err := s.store.Orders(r.Context(), acct, status, c, limit+1)
	if err != nil {
		return err
	}
	views := make([]engine.OrderView, len(recs))
	for i, o := range recs {
		views[i] = s.orderView(o)
	}
	return okJSON(w, httpx.NewPage(views, limit, func(v engine.OrderView) any { return histCursor{T: v.CreatedAt, ID: v.ID} }))
}

func (s *Service) orderView(o *engine.Order) engine.OrderView {
	if eng := s.engine(); eng != nil {
		return eng.OrderViewOf(o)
	}
	v := engine.OrderView{ID: o.ID, Symbol: o.Symbol, Side: o.Side, Type: o.Type, Sz: o.Sz.String(), FilledSz: o.Filled.String(),
		Status: o.Status, ReduceOnly: o.ReduceOnly, PostOnly: o.PostOnly, Leverage: o.Leverage, Liquidation: o.Liquidation,
		Source: o.Source, CreatedAt: ms(o.CreatedMs), UpdatedAt: ms(o.UpdatedMs)}
	if o.Type == engine.TypeLimit {
		px := o.Px.String()
		v.Px = &px
	}
	return v
}

func emptyPage[T any](w http.ResponseWriter, err error) error {
	if err != nil {
		return err
	}
	return okJSON(w, httpx.Page[T]{Items: []T{}})
}

func (s *Service) fillsHandler(w http.ResponseWriter, r *http.Request) error {
	c, limit, err := pageCursor(r)
	if err != nil {
		return err
	}
	acct, err := s.storedAccount(r.Context())
	if err != nil || acct == "" {
		return emptyPage[engine.Fill](w, err)
	}
	recs, err := s.store.Fills(r.Context(), acct, c, limit+1)
	if err != nil {
		return err
	}
	views := make([]engine.Fill, len(recs))
	for i, f := range recs {
		if eng := s.engine(); eng != nil {
			views[i] = eng.FillViewOf(f)
		} else {
			views[i] = engine.Fill{ID: f.ID, OrderID: f.OrderID, Symbol: f.Symbol, Side: f.Side, Px: f.Px.String(), Sz: f.Sz.String(),
				Fee: f.Fee.String(), RealizedPnl: f.RealizedPnl.String(), Liquidity: f.Liquidity, Liquidation: f.Liquidation, CreatedAt: ms(f.Ts)}
		}
	}
	return okJSON(w, httpx.NewPage(views, limit, func(v engine.Fill) any { return histCursor{T: v.CreatedAt, ID: v.ID} }))
}

// fundingRow is a funding payment with its id (the page cursor needs it).
type fundingRow struct {
	ID string `json:"id"`
	engine.FundingPayment
}

func (s *Service) fundingHandler(w http.ResponseWriter, r *http.Request) error {
	c, limit, err := pageCursor(r)
	if err != nil {
		return err
	}
	acct, err := s.storedAccount(r.Context())
	if err != nil || acct == "" {
		return emptyPage[fundingRow](w, err)
	}
	recs, err := s.store.Funding(r.Context(), acct, c, limit+1)
	if err != nil {
		return err
	}
	views := make([]fundingRow, len(recs))
	for i, f := range recs {
		if eng := s.engine(); eng != nil {
			views[i] = fundingRow{ID: f.ID, FundingPayment: eng.FundingViewOf(f)}
		} else {
			views[i] = fundingRow{ID: f.ID, FundingPayment: engine.FundingPayment{Symbol: f.Symbol, Rate: f.Rate.String(), Sz: f.Sz.String(),
				Payment: f.Payment.String(), CreatedAt: ms(f.Ts)}}
		}
	}
	return okJSON(w, httpx.NewPage(views, limit, func(v fundingRow) any { return histCursor{T: v.CreatedAt, ID: v.ID} }))
}

func (s *Service) ledgerHandler(w http.ResponseWriter, r *http.Request) error {
	c, limit, err := pageCursor(r)
	if err != nil {
		return err
	}
	acct, err := s.storedAccount(r.Context())
	if err != nil || acct == "" {
		return emptyPage[engine.LedgerEntry](w, err)
	}
	recs, err := s.store.Ledger(r.Context(), acct, c, limit+1, s.params.QuoteDecimals)
	if err != nil {
		return err
	}
	return okJSON(w, httpx.NewPage(recs, limit, func(v engine.LedgerEntry) any { return histCursor{T: v.CreatedAt, ID: v.ID} }))
}

// ---- trading ----

// OrderResult is the response of order placement and position close.
type OrderResult struct {
	Order engine.OrderView `json:"order"`
	Fills []engine.Fill    `json:"fills"`
}

func (s *Service) placeHandler(w http.ResponseWriter, r *http.Request) error {
	var req engine.PlaceOrder
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	eng, acct, err := s.userAccount(r)
	if err != nil {
		return err
	}
	o, fills, err := eng.PlaceOrder(acct, req)
	if err != nil {
		return apiErr(err)
	}
	if fills == nil {
		fills = []engine.Fill{}
	}
	return okJSON(w, OrderResult{Order: o, Fills: fills})
}

func (s *Service) cancelHandler(w http.ResponseWriter, r *http.Request) error {
	eng, acct, err := s.userAccount(r)
	if err != nil {
		return err
	}
	o, err := eng.Cancel(acct, chi.URLParam(r, "id"))
	if err != nil {
		return apiErr(err)
	}
	return okJSON(w, map[string]any{"order": o})
}

func (s *Service) cancelAllHandler(w http.ResponseWriter, r *http.Request) error {
	eng, acct, err := s.userAccount(r)
	if err != nil {
		return err
	}
	n, err := eng.CancelAll(acct, strings.ToUpper(r.URL.Query().Get("symbol")))
	if err != nil {
		return apiErr(err)
	}
	return okJSON(w, map[string]any{"cancelled": n})
}

func (s *Service) closeHandler(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Sz string `json:"sz"`
	}
	if err := httpx.Decode(r, &body); err != nil {
		return err
	}
	eng, acct, err := s.userAccount(r)
	if err != nil {
		return err
	}
	o, fills, err := eng.ClosePosition(acct, symbolParam(r), body.Sz)
	if err != nil {
		return apiErr(err)
	}
	if fills == nil {
		fills = []engine.Fill{}
	}
	return okJSON(w, OrderResult{Order: o, Fills: fills})
}

func (s *Service) faucetHandler(w http.ResponseWriter, r *http.Request) error {
	eng, acct, err := s.userAccount(r)
	if err != nil {
		return err
	}
	if err := eng.Faucet(acct, false); err != nil {
		return apiErr(err)
	}
	sum, err := eng.Summary(acct)
	if err != nil {
		return apiErr(err)
	}
	return okJSON(w, sum)
}

// sortedCategories lists the catalog's categories (agent tools, search).
func sortedCategories(ms []engine.MarketView) []string {
	var out []string
	for _, m := range ms {
		if m.Category != "" && !slices.Contains(out, m.Category) {
			out = append(out, m.Category)
		}
	}
	slices.Sort(out)
	return out
}

// okJSON writes 200 with v.
func okJSON(w http.ResponseWriter, v any) error {
	httpx.OK(w, v)
	return nil
}
