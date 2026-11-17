package baskets

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/oracle"
)

// basketRow is one baskets row with its creator's display name.
type basketRow struct {
	ID            uuid.UUID
	Symbol        string
	Hash          string
	Canonical     string
	Name          string
	Description   string
	CreatedBy     *uuid.UUID
	Source        string
	ValueAtCreate decimal.Decimal
	MaxShare      decimal.Decimal
	EndsOn        *time.Time
	Status        string
	StatusAt      time.Time
	Hidden        bool
	Entries       int
	OpenReports   int
	VolumeRecent  decimal.Decimal
	OpenInterest  decimal.Decimal
	Holders       int
	Popularity    decimal.Decimal
	CreatedAt     time.Time
	Creator       string
}

const basketColumns = `b.id, b.symbol, b.hash, b.canonical, b.name, b.description, b.created_by, b.source, b.value_at_create::text,
 b.max_share::text, b.ends_on, b.status, b.status_at, b.hidden, b.entries, b.open_reports, b.volume_recent::text,
 b.open_interest::text, b.holders, b.popularity::text, b.created_at,
 CASE WHEN u.id IS NULL OR u.status = 'deleted' THEN 'Deleted user' WHEN u.display_name = '' THEN 'Member' ELSE u.display_name END`

const basketFrom = ` FROM baskets b LEFT JOIN users u ON u.id = b.created_by`

func scanBasket(row interface{ Scan(...any) error }) (basketRow, error) {
	var b basketRow
	var value, share, vol, oi, pop string
	err := row.Scan(&b.ID, &b.Symbol, &b.Hash, &b.Canonical, &b.Name, &b.Description, &b.CreatedBy, &b.Source, &value, &share,
		&b.EndsOn, &b.Status, &b.StatusAt, &b.Hidden, &b.Entries, &b.OpenReports, &vol, &oi, &b.Holders, &pop, &b.CreatedAt, &b.Creator)
	if err != nil {
		return b, err
	}
	b.ValueAtCreate, b.MaxShare, b.VolumeRecent, b.OpenInterest, b.Popularity = dec(value), dec(share), dec(vol), dec(oi), dec(pop)
	return b, nil
}

func dec(s string) decimal.Decimal {
	d, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Zero
	}
	return d
}

var errNoBasket = httpx.NotFound("No such basket.")

// basketBy loads one basket by id, symbol or hash; forUpdate locks the row.
func basketByLock(ctx context.Context, q db.Querier, col, val string, forUpdate bool) (basketRow, error) {
	switch col {
	case "id", "symbol", "hash":
	default:
		return basketRow{}, fmt.Errorf("baskets: basketBy %q", col)
	}
	if col == "symbol" {
		val = strings.ToUpper(strings.TrimSpace(val))
	}
	if col == "id" {
		if _, err := uuid.Parse(val); err != nil {
			return basketRow{}, errNoBasket
		}
	}
	sql := `SELECT ` + basketColumns + basketFrom + ` WHERE b.` + col + ` = $1`
	if forUpdate {
		sql += ` FOR UPDATE OF b`
	}
	b, err := scanBasket(q.QueryRow(ctx, sql, val))
	if db.IsNoRows(err) {
		return b, errNoBasket
	}
	return b, err
}

func basketBy(ctx context.Context, q db.Querier, col, val string) (basketRow, error) {
	return basketByLock(ctx, q, col, val, false)
}

func hasEntry(ctx context.Context, q db.Querier, basketID, userID uuid.UUID) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM basket_entries WHERE basket_id = $1 AND user_id = $2)`, basketID, userID).Scan(&ok)
	return ok, err
}

// ---- DTOs ----

// Summary is a basket in lists.
type Summary struct {
	ID            uuid.UUID `json:"id"`
	Symbol        string    `json:"symbol"`
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	Status        string    `json:"status"`
	Hidden        bool      `json:"hidden"`
	Flagged       bool      `json:"flagged"` // enough open reports to leave discovery until staff review
	Creator       string    `json:"creator"`
	IsMine        bool      `json:"is_mine"` // the caller created it
	Entries       int       `json:"entries"`
	LineCount     int       `json:"line_count"`
	TopLines      []string  `json:"top_lines"` // up to 3 market names, largest share at creation first
	ValueAtCreate string    `json:"value_at_create"`
	VolumeRecent  string    `json:"volume_recent"`
	OpenInterest  string    `json:"open_interest"`
	Holders       int       `json:"holders"`
	EndsOn        *string   `json:"ends_on"`
	CreatedAt     time.Time `json:"created_at"`
}

// MarketInfo is the basket's exchange market as listed.
type MarketInfo struct {
	Status string `json:"status"` // active | reduce_only | halted
	Spec
}

// Detail is GET /v1/baskets/{symbol}.
type Detail struct {
	Summary
	Lines       []BasketLine `json:"lines"`        // with current prices and weights
	Value       string       `json:"value"`        // Σ current prices over the priced lines
	Price       *string      `json:"price"`        // the basket feed's published price
	PriceStatus string       `json:"price_status"` // the oracle's: live, stale, seed, off
	Market      *MarketInfo  `json:"market"`
	Entered     bool         `json:"entered"`  // the caller registered it
	Reported    bool         `json:"reported"` // the caller reported it (open)
}

// AdminBasket is a basket in Admin → Baskets.
type AdminBasket struct {
	Summary
	OpenReports int      `json:"open_reports"`
	Source      string   `json:"source"`
	Reasons     []string `json:"report_reasons"` // reasons of the open reports
}

func (s *Service) summary(b basketRow, caller uuid.UUID) Summary {
	out := Summary{ID: b.ID, Symbol: b.Symbol, Name: b.Name, Description: b.Description, Status: b.Status, Hidden: b.Hidden,
		Flagged: b.OpenReports >= s.cfg.reportsToFlag, Creator: b.Creator, IsMine: b.CreatedBy != nil && *b.CreatedBy == caller,
		Entries: b.Entries, TopLines: []string{}, ValueAtCreate: b.ValueAtCreate.StringFixed(2), VolumeRecent: b.VolumeRecent.StringFixed(2),
		OpenInterest: b.OpenInterest.StringFixed(2), Holders: b.Holders, CreatedAt: b.CreatedAt}
	if b.EndsOn != nil {
		out.EndsOn = strPtr(b.EndsOn.Format(time.DateOnly))
	}
	return out
}

// fillLines adds line counts and top lines to summaries (one query per page).
func fillLines(ctx context.Context, q db.Querier, items []*Summary) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(items))
	by := make(map[uuid.UUID]*Summary, len(items))
	for i, it := range items {
		ids[i], by[it.ID] = it.ID, it
	}
	rows, err := q.Query(ctx, `SELECT basket_id, name FROM basket_lines WHERE basket_id = ANY($1)
 ORDER BY basket_id, quantity * price_at_create DESC, market`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return err
		}
		it := by[id]
		it.LineCount++
		if len(it.TopLines) < 3 {
			it.TopLines = append(it.TopLines, name)
		}
	}
	return rows.Err()
}

// ---- lists ----

type listCursor struct {
	P  string    `json:"p,omitempty"`
	T  time.Time `json:"t"`
	ID uuid.UUID `json:"id"`
}

type listQuery struct {
	sort   string // new | popular
	status string // live | archived | all
	q      string
	filter string // admin: all | reported | hidden
	admin  bool
	limit  int
	after  *listCursor
}

func likePattern(q string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(strings.TrimSpace(q)) + "%"
}

// list reads one page of baskets (limit+1 rows) in discovery or admin order.
func (s *Service) list(ctx context.Context, lq listQuery) ([]basketRow, error) {
	var where []string
	args := []any{}
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if !lq.admin {
		where = append(where, "NOT b.hidden", "b.open_reports < "+arg(s.cfg.reportsToFlag))
	}
	switch lq.status {
	case "live":
		where = append(where, "b.status IN ('active', 'reduce_only')")
	case StatusArchived:
		where = append(where, "b.status = 'archived'")
	}
	switch lq.filter {
	case "reported":
		where = append(where, "b.open_reports > 0")
	case "hidden":
		where = append(where, "b.hidden")
	}
	if strings.TrimSpace(lq.q) != "" {
		p := arg(likePattern(lq.q))
		where = append(where, "(b.name ILIKE "+p+" OR b.symbol ILIKE "+p+")")
	}
	order := "b.created_at DESC, b.id DESC"
	if lq.sort == "popular" {
		order = "b.popularity DESC, b.created_at DESC, b.id DESC"
		if lq.after != nil {
			where = append(where, "(b.popularity, b.created_at, b.id) < ("+arg(lq.after.P)+"::numeric, "+arg(lq.after.T)+", "+arg(lq.after.ID)+")")
		}
	} else if lq.after != nil {
		where = append(where, "(b.created_at, b.id) < ("+arg(lq.after.T)+", "+arg(lq.after.ID)+")")
	}
	sql := `SELECT ` + basketColumns + basketFrom
	if len(where) > 0 {
		sql += ` WHERE ` + strings.Join(where, " AND ")
	}
	sql += ` ORDER BY ` + order + ` LIMIT ` + arg(lq.limit+1)
	rows, err := s.d.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []basketRow
	for rows.Next() {
		b, err := scanBasket(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

type mineRow struct {
	basketRow
	EnteredAt time.Time
	EntryID   uuid.UUID
}

// mine reads the baskets a person registered, newest entry first.
func (s *Service) mine(ctx context.Context, user uuid.UUID, limit int, after *listCursor) ([]mineRow, error) {
	args := []any{user, limit + 1}
	cond := ""
	if after != nil {
		args = append(args, after.T, after.ID)
		cond = ` AND (e.created_at, e.id) < ($3, $4)`
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT `+basketColumns+`, e.created_at, e.id`+basketFrom+
		` JOIN basket_entries e ON e.basket_id = b.id AND e.user_id = $1 WHERE true`+cond+` ORDER BY e.created_at DESC, e.id DESC LIMIT $2`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []mineRow
	for rows.Next() {
		var m mineRow
		var value, share, vol, oi, pop string
		b := &m.basketRow
		if err := rows.Scan(&b.ID, &b.Symbol, &b.Hash, &b.Canonical, &b.Name, &b.Description, &b.CreatedBy, &b.Source, &value, &share,
			&b.EndsOn, &b.Status, &b.StatusAt, &b.Hidden, &b.Entries, &b.OpenReports, &vol, &oi, &b.Holders, &pop, &b.CreatedAt, &b.Creator,
			&m.EnteredAt, &m.EntryID); err != nil {
			return nil, err
		}
		b.ValueAtCreate, b.MaxShare, b.VolumeRecent, b.OpenInterest, b.Popularity = dec(value), dec(share), dec(vol), dec(oi), dec(pop)
		out = append(out, m)
	}
	return out, rows.Err()
}

// ---- detail ----

type lineRow struct {
	Market, Feed, Name string
	Quantity           int
	PriceAtCreate      decimal.Decimal
}

func basketLinesOf(ctx context.Context, q db.Querier, basketID uuid.UUID) ([]lineRow, error) {
	rows, err := q.Query(ctx, `SELECT market, feed, name, quantity, price_at_create::text FROM basket_lines WHERE basket_id = $1 ORDER BY market`, basketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []lineRow
	for rows.Next() {
		var l lineRow
		var px string
		if err := rows.Scan(&l.Market, &l.Feed, &l.Name, &l.Quantity, &px); err != nil {
			return nil, err
		}
		l.PriceAtCreate = dec(px)
		out = append(out, l)
	}
	return out, rows.Err()
}

// detail builds a basket's page: lines with current prices, the basket
// feed's price, the market as listed and the caller's entry and report.
func (s *Service) detail(ctx context.Context, b basketRow, caller uuid.UUID) (Detail, error) {
	d := Detail{Summary: s.summary(b, caller), Lines: []BasketLine{}, PriceStatus: oracle.StatusSeed}
	lines, err := basketLinesOf(ctx, s.d.Pool, b.ID)
	if err != nil {
		return d, err
	}
	keys := []string{b.Symbol}
	for _, l := range lines {
		keys = append(keys, l.Feed)
	}
	prices := map[string]oracle.PricePoint{}
	if o := s.oracle(); o != nil {
		if prices, err = o.CurrentMany(ctx, keys); err != nil {
			return d, err
		}
	}
	ev := &evaluation{value: decimal.Zero}
	for _, l := range lines {
		it := bitem{symbol: l.Market, name: l.Name, feed: l.Feed, qty: l.Quantity}
		if pp, ok := prices[l.Feed]; ok {
			it.status = pp.Status
			if pp.Price > 0 && pp.Status != oracle.StatusOff {
				it.price, it.priced = decimal.NewFromFloat(pp.Price).Round(pp.Decimals), true
				ev.value = ev.value.Add(it.price.Mul(decimal.NewFromInt(int64(it.qty))))
			}
		}
		ev.items = append(ev.items, it)
	}
	d.Lines = basketLines(ev)
	if err := fillLines(ctx, s.d.Pool, []*Summary{&d.Summary}); err != nil {
		return d, err
	}
	d.Value = ev.value.StringFixed(2)
	if pp, ok := prices[b.Symbol]; ok {
		d.PriceStatus = pp.Status
		if pp.Price > 0 {
			d.Price = strPtr(decimal.NewFromFloat(pp.Price).Round(pp.Decimals).String())
		}
	}
	ix, err := s.index(ctx)
	if err != nil {
		return d, err
	}
	if m, ok := ix.baskets[b.Symbol]; ok {
		d.Market = &MarketInfo{Status: m.Status, Spec: Spec{TickSize: m.TickSize, SzDecimals: m.SzDecimals, MaxLeverage: m.MaxLeverage,
			MinNotional: m.MinNotional, OICap: m.OICap}}
	}
	if d.Entered, err = hasEntry(ctx, s.d.Pool, b.ID, caller); err != nil {
		return d, err
	}
	err = s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM basket_reports WHERE basket_id = $1 AND reporter_id = $2 AND resolved_at IS NULL)`,
		b.ID, caller).Scan(&d.Reported)
	return d, err
}

// isNotFound reports whether err is the missing-basket error.
func isNotFound(err error) bool { return errors.Is(err, errNoBasket) }
