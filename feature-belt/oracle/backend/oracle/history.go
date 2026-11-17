package oracle

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Daily history: one close per feed per UTC day in oracle_history. Days the
// oracle published itself carry source "oracle" (written by the publisher);
// earlier days can be backfilled from a source's history (HistorySource) or
// imported by staff, and never overwrite the oracle's own days. History is
// for charts only; it never moves the live price.

// DailyBar is one day of a feed's chart.
type DailyBar struct {
	Day    string `json:"day"` // YYYY-MM-DD (UTC)
	Open   string `json:"open"`
	High   string `json:"high"`
	Low    string `json:"low"`
	Close  string `json:"close"`
	Source string `json:"source"` // oracle, or the source the backfill came from
}

// histPt is one reference close on one day.
type histPt struct {
	Day       time.Time // UTC midnight
	Close     float64
	Low, High float64 // 0 = unknown
	Source    string
}

const historyDays = 365

func utcDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// mergeHist upserts points into a sorted series; it reports whether anything changed.
func mergeHist(existing []histPt, add []histPt) ([]histPt, bool) {
	byDay := make(map[time.Time]histPt, len(existing)+len(add))
	for _, p := range existing {
		byDay[p.Day] = p
	}
	changed := false
	for _, p := range add {
		if old, ok := byDay[p.Day]; !ok || old.Close != p.Close || old.Source != p.Source || old.Low != p.Low || old.High != p.High {
			changed = true
		}
		byDay[p.Day] = p
	}
	out := make([]histPt, 0, len(byDay))
	for _, p := range byDay {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Day.Before(out[j].Day) })
	return out, changed
}

// screenHistory drops closes that are wild outliers against their neighbours
// (> 5× or < 1/5 of the median of up to 7 points either side).
func screenHistory(pts []histPt) (kept, dropped []histPt) {
	for i, p := range pts {
		var nb []float64
		for j := max(0, i-7); j <= min(len(pts)-1, i+7); j++ {
			if j != i && pts[j].Close > 0 {
				nb = append(nb, pts[j].Close)
			}
		}
		if p.Close <= 0 || math.IsNaN(p.Close) {
			dropped = append(dropped, p)
			continue
		}
		if len(nb) >= 3 {
			m := median(nb)
			if p.Close > 5*m || p.Close < m/5 {
				dropped = append(dropped, p)
				continue
			}
		}
		kept = append(kept, p)
	}
	return kept, dropped
}

// Backfill smoothing. Raw closes stay stored as fetched; only the chart is
// smoothed, so these can be re-tuned without refetching.
const (
	smoothMedianDays = 2.0   // centred rolling median over ±2 days kills one-off spikes
	smoothSpanDays   = 7.0   // EMA span in calendar days (α = 2/(span+1) per day)
	bandVolMult      = 0.5   // High/Low band = 0.5 × robust daily volatility …
	bandCap          = 0.015 // … capped at 1.5% of price each side
	seamTolerance    = 0.02  // the last smoothed close must sit within 2% of the anchor …
	seamEaseDays     = 5.0   // … otherwise the final 5 days are eased onto it
)

// smoothBackfill turns backfilled closes into chart bars: Close = calendar-time
// EMA (span 7 days) of a ±2-day rolling median; Open = previous smoothed
// close; High/Low = max/min(Open, Close) widened by half the robust trailing
// 7-day volatility, capped at 1.5%. When anchor > 0 (the first oracle close,
// or the live price) and the last smoothed close is more than 2% from it, the
// final 5 days are eased onto it. No days are invented.
func smoothBackfill(raw []histPt, anchor float64, decimals int32) []DailyBar {
	n := len(raw)
	if n == 0 {
		return nil
	}
	days := func(a, b time.Time) float64 { return b.Sub(a).Hours() / 24 }
	med := make([]float64, n)
	for i := range raw {
		var w []float64
		for j := i; j >= 0 && days(raw[j].Day, raw[i].Day) <= smoothMedianDays; j-- {
			w = append(w, raw[j].Close)
		}
		for j := i + 1; j < n && days(raw[i].Day, raw[j].Day) <= smoothMedianDays; j++ {
			w = append(w, raw[j].Close)
		}
		med[i] = median(w)
	}
	a := 2 / (smoothSpanDays + 1)
	ema := make([]float64, n)
	ema[0] = med[0]
	for i := 1; i < n; i++ {
		alpha := 1 - math.Pow(1-a, math.Max(days(raw[i-1].Day, raw[i].Day), 0))
		ema[i] = ema[i-1] + alpha*(med[i]-ema[i-1])
	}
	last := n - 1
	if anchor > 0 && ema[last] > 0 {
		if gap := anchor/ema[last] - 1; math.Abs(gap) > seamTolerance {
			for j := last; j >= 0; j-- {
				d := days(raw[j].Day, raw[last].Day)
				if d >= seamEaseDays {
					break
				}
				ema[j] *= 1 + (1-d/seamEaseDays)*gap
			}
		}
	}
	rets := make([]float64, n)
	for i := 1; i < n; i++ {
		dt := math.Max(days(raw[i-1].Day, raw[i].Day), 1)
		rets[i] = math.Log(raw[i].Close/raw[i-1].Close) / math.Sqrt(dt)
	}
	bandAt := func(i int) float64 {
		var rs []float64
		for j := i; j >= 1 && days(raw[j].Day, raw[i].Day) < 7; j-- {
			rs = append(rs, rets[j])
		}
		if len(rs) < 3 {
			rs = rs[:0]
			for j := i; j >= 1 && len(rs) < 5; j-- {
				rs = append(rs, rets[j])
			}
		}
		if len(rs) < 2 {
			return 0
		}
		m := median(rs)
		dev := make([]float64, len(rs))
		for k, r := range rs {
			dev[k] = math.Abs(r - m)
		}
		return math.Min(bandCap, bandVolMult*1.4826*median(dev))
	}
	bars := make([]DailyBar, 0, n)
	for i, p := range raw {
		open := ema[i]
		if i > 0 {
			open = ema[i-1]
		}
		cl := ema[i]
		b := bandAt(i)
		bars = append(bars, DailyBar{
			Day: p.Day.Format("2006-01-02"), Source: p.Source,
			Open: fmtPrice(open, decimals), Close: fmtPrice(cl, decimals),
			High: fmtPrice(math.Max(open, cl)*(1+b), decimals), Low: fmtPrice(math.Min(open, cl)*(1-b), decimals),
		})
	}
	return bars
}

// ownBars are the oracle's own days: open = previous close, high/low the
// day's published extremes.
func ownBars(pts []histPt, prevClose float64, decimals int32) []DailyBar {
	bars := make([]DailyBar, 0, len(pts))
	prev := prevClose
	for _, p := range pts {
		open := p.Close
		if prev > 0 {
			open = prev
		}
		hi, lo := math.Max(open, p.Close), math.Min(open, p.Close)
		if p.High > 0 {
			hi = math.Max(hi, p.High)
		}
		if p.Low > 0 {
			lo = math.Min(lo, p.Low)
		}
		bars = append(bars, DailyBar{Day: p.Day.Format("2006-01-02"), Source: p.Source,
			Open: fmtPrice(open, decimals), High: fmtPrice(hi, decimals), Low: fmtPrice(lo, decimals), Close: fmtPrice(p.Close, decimals)})
		prev = p.Close
	}
	return bars
}

// buildBars splits a series at the first oracle day: the backfill before it
// is smoothed and eased onto it (or onto live when the oracle has no day yet).
func buildBars(pts []histPt, live float64, decimals int32) []DailyBar {
	split := len(pts)
	for i, p := range pts {
		if p.Source == "oracle" {
			split = i
			break
		}
	}
	backfill, own := pts[:split], pts[split:]
	anchor := live
	if len(own) > 0 {
		anchor = own[0].Close
	}
	bars := smoothBackfill(backfill, anchor, decimals)
	prev := 0.0
	if len(backfill) > 0 {
		prev = anchor
		if c, err := strconv.ParseFloat(bars[len(bars)-1].Close, 64); err == nil {
			prev = c
		}
	}
	return append(bars, ownBars(own, prev, decimals)...)
}

// basketPoints prices a basket on every day any part has a point:
// Σ quantity × part close, carrying each part's last close across its gaps.
// A day before some part's first point is left out (missing, never
// invented). A day is "oracle" only when every part's point is.
func basketPoints(qty []float64, series [][]histPt) []histPt {
	daySet := map[time.Time]bool{}
	for _, s := range series {
		for _, p := range s {
			daySet[p.Day] = true
		}
	}
	days := make([]time.Time, 0, len(daySet))
	for d := range daySet {
		days = append(days, d)
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Before(days[j]) })
	idx := make([]int, len(series))
	last := make([]histPt, len(series))
	var out []histPt
	for _, d := range days {
		total, ok, own := 0.0, true, true
		for c, s := range series {
			for idx[c] < len(s) && !s[idx[c]].Day.After(d) {
				last[c] = s[idx[c]]
				idx[c]++
			}
			if last[c].Close <= 0 {
				ok = false
				continue
			}
			total += qty[c] * last[c].Close
			own = own && last[c].Source == "oracle"
		}
		if ok {
			src := "basket"
			if own {
				src = "oracle"
			}
			out = append(out, histPt{Day: d, Close: total, Source: src})
		}
	}
	return out
}

// maxLevelGap is how far backfilled history may sit from the live price
// before the series is taken for a different product and dropped.
const maxLevelGap = 0.25

// recentLevel is the median close of the last 7 days of a series (falling
// back to the latest point within 30 days).
func recentLevel(pts []histPt, now time.Time) (float64, bool) {
	to := utcDay(now)
	var xs []float64
	for _, p := range pts {
		if !p.Day.Before(to.AddDate(0, 0, -7)) && !p.Day.After(to) {
			xs = append(xs, p.Close)
		}
	}
	if len(xs) > 0 {
		return median(xs), true
	}
	for i := len(pts) - 1; i >= 0; i-- {
		if !pts[i].Day.After(to) {
			if to.Sub(pts[i].Day) <= 30*24*time.Hour {
				return pts[i].Close, true
			}
			break
		}
	}
	return 0, false
}

func levelMismatch(pts []histPt, live float64, now time.Time) string {
	if live <= 0 {
		return "" // nothing to compare with yet
	}
	lvl, ok := recentLevel(pts, now)
	if !ok {
		return "no point in the last 30 days to check against the live price"
	}
	if gap := math.Abs(lvl/live - 1); gap > maxLevelGap {
		return fmt.Sprintf("recent history %.4g is %.0f%% from the live price %.4g", lvl, 100*gap, live)
	}
	return ""
}

// loadHistory reads a feed's stored points from day from on.
func (s *Service) loadHistory(ctx context.Context, feedID uuid.UUID, from time.Time) ([]histPt, error) {
	rows, err := s.d.Pool.Query(ctx, `SELECT day, close::float8, coalesce(low, 0)::float8, coalesce(high, 0)::float8, source
 FROM oracle_history WHERE feed_id = $1 AND day >= $2 ORDER BY day`, feedID, from)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []histPt
	for rows.Next() {
		var p histPt
		if err := rows.Scan(&p.Day, &p.Close, &p.Low, &p.High, &p.Source); err != nil {
			return nil, err
		}
		p.Day = utcDay(p.Day)
		out = append(out, p)
	}
	return out, rows.Err()
}

// storeHistory screens backfilled points for one feed, checks their level
// against the live price and stores them (never over the oracle's own days).
// It writes directly (any instance) and returns how many of add it stored.
func (s *Service) storeHistory(ctx context.Context, feedID uuid.UUID, source string, add []DailyPrice, live float64) (int, error) {
	now := time.Now()
	from := utcDay(now).AddDate(0, 0, -historyDays-7)
	existing, err := s.loadHistory(ctx, feedID, from)
	if err != nil {
		return 0, err
	}
	var own, prior []histPt
	ownDay := map[time.Time]bool{}
	for _, p := range existing {
		if p.Source == "oracle" {
			own = append(own, p)
			ownDay[p.Day] = true
		} else {
			prior = append(prior, p)
		}
	}
	var in []histPt
	inDay := map[time.Time]bool{}
	for _, d := range add {
		day := utcDay(d.Day)
		if day.Before(from) || day.After(utcDay(now)) || d.Close <= 0 || ownDay[day] {
			continue
		}
		in = append(in, histPt{Day: day, Close: d.Close, Low: d.Low, High: d.High, Source: source})
		inDay[day] = true
	}
	merged, changed := mergeHist(prior, in)
	kept, dropped := screenHistory(merged)
	for _, d := range dropped {
		s.log.Info("oracle: history outlier dropped", "feed", feedID, "day", d.Day.Format("2006-01-02"), "close", d.Close, "source", d.Source)
	}
	if why := levelMismatch(append(append([]histPt(nil), kept...), own...), live, now); why != "" {
		return 0, fmt.Errorf("history doesn't match the live price: %s", why)
	}
	if !changed {
		return 0, nil
	}
	b := &pgx.Batch{}
	for _, p := range kept {
		b.Queue(`INSERT INTO oracle_history (feed_id, day, close, low, high, source) VALUES ($1,$2,$3,$4,$5,$6)
 ON CONFLICT (feed_id, day) DO UPDATE SET close=EXCLUDED.close, low=EXCLUDED.low, high=EXCLUDED.high, source=EXCLUDED.source
 WHERE oracle_history.source <> 'oracle'`, feedID, p.Day, p.Close, nullPrice(p.Low), nullPrice(p.High), p.Source)
	}
	if err := s.d.Pool.SendBatch(ctx, b).Close(); err != nil {
		return 0, err
	}
	n := 0
	for _, p := range kept {
		if inDay[p.Day] {
			n++
		}
	}
	return n, nil
}

// HistoryBars returns a feed's daily bars for the last days (≤ 365), oldest
// first, ending today.
func (s *Service) HistoryBars(ctx context.Context, f FeedInfo, days int, live float64) ([]DailyBar, error) {
	days = max(1, min(days, historyDays))
	from := utcDay(time.Now()).AddDate(0, 0, -days)
	if f.Kind == FeedSingle {
		pts, err := s.loadHistory(ctx, f.ID, from.AddDate(0, 0, -7))
		if err != nil {
			return nil, err
		}
		return trimBars(buildBars(pts, live, f.Decimals), from), nil
	}
	parts, err := loadParts(ctx, s.d.Pool, &f.ID)
	if err != nil {
		return nil, err
	}
	qty := make([]float64, len(parts))
	series := make([][]histPt, len(parts))
	for i, p := range parts {
		qty[i] = p.Quantity
		if series[i], err = s.loadHistory(ctx, p.FeedID, from.AddDate(0, 0, -30)); err != nil {
			return nil, err
		}
	}
	return trimBars(buildBars(basketPoints(qty, series), live, f.Decimals), from), nil
}

func trimBars(bars []DailyBar, from time.Time) []DailyBar {
	cut := from.Format("2006-01-02")
	i := 0
	for i < len(bars) && bars[i].Day < cut {
		i++
	}
	out := bars[i:]
	if out == nil {
		out = []DailyBar{}
	}
	return out
}

// ---- backfill runner ----

type historyJob struct {
	feedID uuid.UUID
	key    string
	ref    Ref
	src    HistorySource
	name   string
	lim    *limiter
}

// runHistory backfills a year at low priority (live polling always wins the
// shared limiter), then refreshes the last days once a day per feed. One
// goroutine per source.
func (s *Service) runHistory(ctx context.Context) {
	s.mu.RLock()
	groups := map[string][]historyJob{}
	for _, f := range s.order {
		if !f.enabled || f.kind != FeedSingle {
			continue
		}
		for _, r := range f.refs {
			src, ok := s.sources[r.Source]
			if !ok || s.cfg.disabled[r.Source] {
				continue
			}
			hs, ok := src.(HistorySource)
			ref := Ref{ID: r.ID, FeedID: f.id, FeedKey: f.key, Venue: r.Venue, Ref: r.Ref, Params: r.Params}
			if !ok || !hs.HasHistory(ref) {
				continue
			}
			groups[r.Source] = append(groups[r.Source], historyJob{feedID: f.id, key: f.key, ref: ref, src: hs, name: r.Source, lim: s.limiters[laneKey(r.Source, s.refLane[r.ID])]})
			break // one history source per feed: the first that has one
		}
	}
	s.mu.RUnlock()
	for _, jobs := range groups {
		go s.runHistoryGroup(ctx, jobs)
	}
}

func (s *Service) runHistoryGroup(ctx context.Context, jobs []historyJob) {
	for {
		next := time.Now().Add(time.Hour)
		for _, j := range jobs {
			key := "history:" + j.ref.ID.String()
			last, ok := s.lastFetch(j.name, key)
			if ok && time.Since(last) < 24*time.Hour {
				next = minTime(next, last.Add(24*time.Hour))
				continue
			}
			lim := j.lim
			if lim == nil {
				lim = &limiter{minGap: 5 * time.Second}
			}
			if err := lim.waitLow(ctx, 5*time.Second); err != nil {
				return
			}
			to := utcDay(time.Now())
			from := to.AddDate(0, 0, -historyDays)
			if ok { // refresh: the last 10 days
				from = to.AddDate(0, 0, -10)
			}
			fctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			pts, err := j.src.History(fctx, j.ref, from, to)
			cancel()
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				s.log.Warn("oracle: history fetch failed", "feed", j.key, "source", j.name, "err", err)
				next = minTime(next, time.Now().Add(30*time.Minute))
				continue
			}
			s.recordFetch(j.name, key, time.Now())
			s.mu.RLock()
			live := 0.0
			if f := s.feeds[j.feedID]; f != nil && f.live {
				live = f.price
			}
			s.mu.RUnlock()
			n, err := s.storeHistory(ctx, j.feedID, j.name, pts, live)
			if err != nil {
				s.log.Warn("oracle: history not stored", "feed", j.key, "source", j.name, "err", err)
				continue
			}
			if n > 0 {
				s.emitHistory(ctx, j.feedID)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(max(time.Until(next), time.Minute)):
		}
	}
}

func minTime(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}
