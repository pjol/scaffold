package oracle

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/safehttp"
)

// The "http" source polls a public JSON endpoint per source row: Ref is the
// URL, Params say where the numbers are.
//
//	{
//	  "price_path": "data.amount",   required: dot path, array indexes as numbers (result.XXBTZUSD.c.0)
//	  "time_path": "timestamp",      optional: RFC 3339, or unix seconds or milliseconds; default = fetch time
//	  "qty_path": "volume",          optional
//	  "kind": "market",              sale | market | listing (default market)
//	  "every": "60s",                optional poll interval (at least 10s)
//	  "history_url": "https://…?start={from}&end={to}",  optional daily backfill; {from}/{to} are YYYY-MM-DD,
//	                                 {from_unix}/{to_unix} unix seconds
//	  "history_path": "data",        array of points in the history response ("" = the root)
//	  "history_day_path": "date",    per point: a date, RFC 3339 time or unix seconds/milliseconds
//	  "history_close_path": "close", per point
//	  "history_low_path": "low", "history_high_path": "high"  optional
//	}
//
// Every URL is fetched through safehttp (no private addresses, https only
// outside development). Endpoints that need credentials get a source of their
// own (RegisterSource), with the key in env, never in a source row.
const httpSourceName = "http"

func init() {
	RegisterSource(httpSourceName, func(d *app.Deps) Source {
		dev := d.Cfg != nil && d.Cfg.IsDevelopment()
		s := &httpSource{
			client: safehttp.Client(safehttp.Options{Timeout: 15 * time.Second, MaxBody: 4 << 20, MaxRedirects: 3, AllowPrivate: dev, AllowHTTP: dev}),
			minGap: config.Duration("ORACLE_HTTP_MIN_GAP", time.Second),
			every:  config.Duration("ORACLE_HTTP_CADENCE", time.Minute),
			dev:    dev,
		}
		if s.minGap < 100*time.Millisecond {
			config.Problem("ORACLE_HTTP_MIN_GAP must be at least 100ms (got %s)", s.minGap)
		}
		if s.every < 10*time.Second {
			config.Problem("ORACLE_HTTP_CADENCE must be at least 10s (got %s)", s.every)
		}
		return s
	})
}

type httpSource struct {
	client *http.Client
	minGap time.Duration
	every  time.Duration
	dev    bool
}

type httpParams struct {
	PricePath        string `json:"price_path"`
	TimePath         string `json:"time_path,omitempty"`
	QtyPath          string `json:"qty_path,omitempty"`
	Kind             string `json:"kind,omitempty"`
	Every            string `json:"every,omitempty"`
	HistoryURL       string `json:"history_url,omitempty"`
	HistoryPath      string `json:"history_path,omitempty"`
	HistoryDayPath   string `json:"history_day_path,omitempty"`
	HistoryClosePath string `json:"history_close_path,omitempty"`
	HistoryLowPath   string `json:"history_low_path,omitempty"`
	HistoryHighPath  string `json:"history_high_path,omitempty"`
}

func (h *httpSource) Name() string { return httpSourceName }

func (h *httpSource) Budget() Budget { return Budget{MinGap: h.minGap, Cadence: h.every} }

func parseHTTPParams(raw json.RawMessage) (httpParams, error) {
	var p httpParams
	if len(raw) > 0 && string(raw) != "null" {
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&p); err != nil {
			return p, fmt.Errorf("params: %v", err)
		}
	}
	if p.Kind == "" {
		p.Kind = KindMarket
	}
	return p, nil
}

// ValidateRef checks a source row before it is saved.
func (h *httpSource) ValidateRef(ref Ref) error {
	if _, err := safehttp.CheckURL(ref.Ref, h.dev, h.dev); err != nil {
		return fmt.Errorf("the URL can't be fetched: %v", err)
	}
	p, err := parseHTTPParams(ref.Params)
	if err != nil {
		return err
	}
	if strings.TrimSpace(p.PricePath) == "" {
		return fmt.Errorf("params.price_path is required")
	}
	if !validKind(p.Kind) {
		return fmt.Errorf("params.kind must be sale, market or listing")
	}
	if p.Every != "" {
		d, err := time.ParseDuration(p.Every)
		if err != nil || d < 10*time.Second {
			return fmt.Errorf("params.every must be a duration of at least 10s")
		}
	}
	if p.HistoryURL != "" {
		if _, err := safehttp.CheckURL(historyURL(p.HistoryURL, time.Now(), time.Now()), h.dev, h.dev); err != nil {
			return fmt.Errorf("params.history_url can't be fetched: %v", err)
		}
		if p.HistoryDayPath == "" || p.HistoryClosePath == "" {
			return fmt.Errorf("params.history_day_path and history_close_path are required with history_url")
		}
	}
	return nil
}

// Tasks: one per source row, at its own interval; each API host is a lane
// with its own budget.
func (h *httpSource) Tasks(refs []Ref) []Task {
	out := make([]Task, 0, len(refs))
	for _, r := range refs {
		p, err := parseHTTPParams(r.Params)
		if err != nil || p.PricePath == "" {
			continue
		}
		var every time.Duration
		if d, err := time.ParseDuration(p.Every); err == nil && d >= 10*time.Second {
			every = d
		}
		out = append(out, Task{Key: "ref:" + r.ID.String(), Cadence: every, Lane: sourceHost(r.Ref), Refs: []Ref{r}})
	}
	return out
}

func (h *httpSource) Fetch(ctx context.Context, t Task) ([]Print, error) {
	var out []Print
	for _, r := range t.Refs {
		p, err := parseHTTPParams(r.Params)
		if err != nil {
			return nil, err
		}
		var doc any
		if err := GetJSON(ctx, h.client, r.Ref, nil, &doc); err != nil {
			return nil, err
		}
		price, err := jsonNumber(doc, p.PricePath)
		if err != nil {
			return nil, fmt.Errorf("price_path %q: %w", p.PricePath, err)
		}
		if price <= 0 || math.IsNaN(price) || math.IsInf(price, 0) {
			return nil, fmt.Errorf("price_path %q: not a positive number", p.PricePath)
		}
		at := time.Now()
		if p.TimePath != "" {
			v, err := jsonAt(doc, p.TimePath)
			if err != nil {
				return nil, fmt.Errorf("time_path %q: %w", p.TimePath, err)
			}
			if at, err = parseTime(v); err != nil {
				return nil, fmt.Errorf("time_path %q: %w", p.TimePath, err)
			}
		}
		var qty float64
		if p.QtyPath != "" {
			if q, err := jsonNumber(doc, p.QtyPath); err == nil && q > 0 {
				qty = q
			}
		}
		out = append(out, Print{FeedID: r.FeedID, Venue: r.Venue, Kind: p.Kind, Price: price, Qty: qty, At: at})
	}
	return out, nil
}

func (h *httpSource) HasHistory(ref Ref) bool {
	p, err := parseHTTPParams(ref.Params)
	return err == nil && p.HistoryURL != "" && p.HistoryDayPath != "" && p.HistoryClosePath != ""
}

func historyURL(tmpl string, from, to time.Time) string {
	return strings.NewReplacer(
		"{from}", from.UTC().Format("2006-01-02"), "{to}", to.UTC().Format("2006-01-02"),
		"{from_unix}", strconv.FormatInt(from.Unix(), 10), "{to_unix}", strconv.FormatInt(to.Unix(), 10),
	).Replace(tmpl)
}

func (h *httpSource) History(ctx context.Context, ref Ref, from, to time.Time) ([]DailyPrice, error) {
	p, err := parseHTTPParams(ref.Params)
	if err != nil {
		return nil, err
	}
	var doc any
	if err := GetJSON(ctx, h.client, historyURL(p.HistoryURL, from, to), nil, &doc); err != nil {
		return nil, err
	}
	arr, err := jsonAt(doc, p.HistoryPath)
	if err != nil {
		return nil, fmt.Errorf("history_path %q: %w", p.HistoryPath, err)
	}
	items, ok := arr.([]any)
	if !ok {
		return nil, fmt.Errorf("history_path %q: not an array", p.HistoryPath)
	}
	var out []DailyPrice
	for _, it := range items {
		dv, err := jsonAt(it, p.HistoryDayPath)
		if err != nil {
			continue
		}
		day, err := parseTime(dv)
		if err != nil {
			continue
		}
		c, err := jsonNumber(it, p.HistoryClosePath)
		if err != nil || c <= 0 {
			continue
		}
		dp := DailyPrice{Day: utcDay(day), Close: c}
		if p.HistoryLowPath != "" {
			dp.Low, _ = jsonNumber(it, p.HistoryLowPath)
		}
		if p.HistoryHighPath != "" {
			dp.High, _ = jsonNumber(it, p.HistoryHighPath)
		}
		out = append(out, dp)
	}
	return out, nil
}

// jsonAt walks a dot path ("result.XXBTZUSD.c.0"); "" is the root.
func jsonAt(doc any, path string) (any, error) {
	cur := doc
	if strings.TrimSpace(path) == "" {
		return cur, nil
	}
	for _, seg := range strings.Split(path, ".") {
		switch v := cur.(type) {
		case map[string]any:
			next, ok := v[seg]
			if !ok {
				return nil, fmt.Errorf("no %q", seg)
			}
			cur = next
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil || i < 0 || i >= len(v) {
				return nil, fmt.Errorf("no index %q", seg)
			}
			cur = v[i]
		default:
			return nil, fmt.Errorf("can't step into %q", seg)
		}
	}
	return cur, nil
}

// jsonNumber reads a number or a numeric string at path.
func jsonNumber(doc any, path string) (float64, error) {
	v, err := jsonAt(doc, path)
	if err != nil {
		return 0, err
	}
	switch x := v.(type) {
	case float64:
		return x, nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		if err != nil {
			return 0, fmt.Errorf("%q is not a number", truncate(x, 40))
		}
		return f, nil
	}
	return 0, fmt.Errorf("not a number")
}

// parseTime reads RFC 3339, a date, or unix seconds / milliseconds (number or string).
func parseTime(v any) (time.Time, error) {
	var n float64
	switch x := v.(type) {
	case float64:
		n = x
	case string:
		s := strings.TrimSpace(x)
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return t, nil
		}
		if t, err := time.Parse("2006-01-02", s); err == nil {
			return t, nil
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return time.Time{}, fmt.Errorf("%q is not a time", truncate(s, 40))
		}
		n = f
	default:
		return time.Time{}, fmt.Errorf("not a time")
	}
	if n <= 0 {
		return time.Time{}, fmt.Errorf("not a time")
	}
	if n > 1e12 { // milliseconds
		return time.UnixMilli(int64(n)), nil
	}
	sec, frac := math.Modf(n)
	return time.Unix(int64(sec), int64(frac*1e9)), nil
}

// sourceHost is the host of a URL ref, for default venue names.
func sourceHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(u.Hostname(), "www.")
}
