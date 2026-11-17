package oracle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
)

// Source is a price source the scheduler polls. Register one with
// RegisterSource; feeds name it in their source rows (oracle_feed_sources.source).
type Source interface {
	// Name is the registered name feeds use ("http").
	Name() string
	// Budget is how hard the source may be called (its published limits, with margin).
	Budget() Budget
	// Tasks groups the source rows that name this source into fetch units: one
	// per row, or one batch endpoint for many. Called again whenever feeds change.
	Tasks(refs []Ref) []Task
	// Fetch runs one task. Return *HTTPError for HTTP failures so a 429's
	// Retry-After is honoured.
	Fetch(ctx context.Context, t Task) ([]Print, error)
}

// HistorySource is implemented by sources that can backfill daily reference
// prices (charts only; never used for the live price).
type HistorySource interface {
	// HasHistory reports whether this source row can backfill.
	HasHistory(ref Ref) bool
	// History returns daily closes in [from, to] (UTC days).
	History(ctx context.Context, ref Ref, from, to time.Time) ([]DailyPrice, error)
}

// Budget limits one source. The effective gap between requests is the larger
// of MinGap and Per/Quota; a task is due every Cadence, but never more often
// than the gap allows across all tasks.
type Budget struct {
	MinGap  time.Duration // between any two requests
	Cadence time.Duration // default interval per task
	Quota   int           // requests allowed per Per (0 = no quota)
	Per     time.Duration
}

func (b Budget) gap() time.Duration {
	g := b.MinGap
	if b.Quota > 0 && b.Per > 0 {
		if q := b.Per / time.Duration(b.Quota); q > g {
			g = q
		}
	}
	if g <= 0 {
		g = time.Second
	}
	return g
}

// Ref is one source row as a source sees it.
type Ref struct {
	ID      uuid.UUID
	FeedID  uuid.UUID
	FeedKey string
	Venue   string
	Ref     string
	Params  json.RawMessage
}

// Task is one fetch unit. Key must be stable across restarts (its last fetch
// time is persisted so a restart doesn't re-burst the source).
type Task struct {
	Key     string
	Cadence time.Duration // 0 = the source's Budget().Cadence
	// Lane is the rate-limit domain (an API host): each lane gets its own
	// Budget and its own 429 backoff. "" = one lane for the whole source.
	Lane string
	Refs []Ref
}

// Print is one observation a source reports.
type Print struct {
	FeedID uuid.UUID
	Venue  string
	Kind   string    // sale | market | listing
	Price  float64   // in the feed's quote currency, before premium normalisation
	Qty    float64   // units traded (sale) or trades summarised (market); 0 = unknown
	At     time.Time // when it happened (not when it was fetched)
	ID     string    // stable id for dedupe (a trade id), unique per feed, source and venue; empty = derived from venue, kind, price and time
	Note   string    // short detail shown as the first reason; never sent to the decision model
}

// DailyPrice is one day of reference history.
type DailyPrice struct {
	Day       time.Time // UTC midnight
	Close     float64
	Low, High float64 // 0 = unknown
}

var (
	registryMu sync.Mutex
	registry   = map[string]func(d *app.Deps) Source{}
)

// RegisterSource adds a source factory under name. Call it from an init()
// (the app's own sources) before the module is constructed; New builds every
// registered source once.
func RegisterSource(name string, factory func(d *app.Deps) Source) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[name] = factory
}

func registeredSources(d *app.Deps) map[string]Source {
	registryMu.Lock()
	defer registryMu.Unlock()
	out := map[string]Source{}
	for name, fn := range registry {
		if src := fn(d); src != nil {
			out[name] = src
		}
	}
	return out
}

// HTTPError carries the status and Retry-After of a failed request.
type HTTPError struct {
	Status     int
	RetryAfter time.Duration
	Body       string
}

func (e *HTTPError) Error() string {
	if e.Status == http.StatusTooManyRequests {
		return "rate limited (HTTP 429)"
	}
	return fmt.Sprintf("HTTP %d", e.Status)
}

// GetJSON fetches url with client and decodes the JSON body into v. A non-2xx
// answer is an *HTTPError. Bodies are capped at 8 MiB.
func GetJSON(ctx context.Context, client *http.Client, url string, headers map[string]string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "scaffoldapp-oracle/1.0")
	for k, val := range headers {
		req.Header.Set(k, val)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		he := &HTTPError{Status: resp.StatusCode, Body: truncate(string(body), 200)}
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if n, err := strconv.Atoi(strings.TrimSpace(ra)); err == nil {
				he.RetryAfter = time.Duration(n) * time.Second
			} else if t, err := http.ParseTime(ra); err == nil {
				he.RetryAfter = time.Until(t)
			}
		}
		return he
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ---- limiter ----

// limiter enforces a source's gap between requests and the source-wide
// exponential backoff after errors (with jitter, honouring 429 Retry-After).
// Live polling has priority over history backfill.
type limiter struct {
	minGap time.Duration

	mu           sync.Mutex
	next         time.Time
	lowNext      time.Time // earliest next low-priority (history) request
	highWaiting  int       // live requests queued: low priority yields to them
	fails        int
	backoffUntil time.Time
	lastErr      string
	lastErrAt    time.Time
	lastOK       time.Time
	rateLimited  bool
}

func (l *limiter) wait(ctx context.Context) error {
	l.mu.Lock()
	l.highWaiting++
	l.mu.Unlock()
	defer func() {
		l.mu.Lock()
		l.highWaiting--
		l.mu.Unlock()
	}()
	for {
		l.mu.Lock()
		now := time.Now()
		at := l.next
		if l.backoffUntil.After(at) {
			at = l.backoffUntil
		}
		if !at.After(now) {
			l.next = now.Add(l.minGap)
			l.mu.Unlock()
			return nil
		}
		l.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Until(at)):
		}
	}
}

// waitLow takes a slot at low priority: only when no live request is queued,
// after the source's own gap and backoff, and at most once per gap.
func (l *limiter) waitLow(ctx context.Context, gap time.Duration) error {
	for {
		l.mu.Lock()
		now := time.Now()
		at := l.next
		for _, t := range []time.Time{l.backoffUntil, l.lowNext} {
			if t.After(at) {
				at = t
			}
		}
		if l.highWaiting == 0 && !at.After(now) {
			l.next = now.Add(l.minGap)
			l.lowNext = now.Add(gap)
			l.mu.Unlock()
			return nil
		}
		l.mu.Unlock()
		d := min(max(time.Until(at), 250*time.Millisecond), 2*time.Second)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(d):
		}
	}
}

func (l *limiter) success() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.fails = 0
	l.rateLimited = false
	l.lastOK = time.Now()
}

// backoff is the wait after the n-th consecutive failure: 30 s, 1 m, 2 m …
// capped at 32 m, with ±20% jitter.
func backoff(n int) time.Duration {
	d := 30 * time.Second << min(max(n, 1)-1, 6)
	return time.Duration(float64(d) * (0.8 + 0.4*rand.Float64()))
}

func isRateLimited(err error) (*HTTPError, bool) {
	var he *HTTPError
	if errors.As(err, &he) && he.Status == http.StatusTooManyRequests {
		return he, true
	}
	return nil, false
}

// failure records a failed request. A rate limit (429) backs off the whole
// lane, honouring Retry-After; any other error only its task (the runner).
func (l *limiter) failure(err error) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lastErr = truncate(err.Error(), 200)
	l.lastErrAt = time.Now()
	he, limited := isRateLimited(err)
	if !limited {
		return 0
	}
	l.fails++
	l.rateLimited = true
	ra := he.RetryAfter
	if ra <= 0 {
		ra = 5 * time.Minute
	}
	d := max(backoff(l.fails), ra)
	l.backoffUntil = time.Now().Add(d)
	return d
}

// state summarises the lane for its health row: ok unless rate limited.
func (l *limiter) state(now time.Time) (ok bool, note string, lastOK, lastErrAt time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.rateLimited {
		wait := max(l.backoffUntil.Sub(now).Round(time.Second), 0)
		return false, fmt.Sprintf("rate limited; retrying in %s", wait), l.lastOK, l.lastErrAt
	}
	return true, "", l.lastOK, l.lastErrAt
}

// ---- scheduler ----

// runSource drives one lane of a source: picks the most overdue task, waits
// for the lane's limiter, fetches, and schedules the next run (cadence on
// success, a growing per-task backoff on failure; a 429 backs off the whole
// lane). Due times start from the persisted last-fetch times.
func (s *Service) runSource(ctx context.Context, src Source, lane string, lim *limiter, tasks []Task) {
	if len(tasks) == 0 {
		return
	}
	b := src.Budget()
	gap := b.gap()
	cadence := func(t Task) time.Duration {
		c := t.Cadence
		if c <= 0 {
			c = b.Cadence
		}
		// Never ask for more than the budget allows across the lane's tasks.
		if floor := time.Duration(len(tasks)) * gap; c < floor {
			c = floor
		}
		return max(c, time.Second)
	}
	due := make(map[string]time.Time, len(tasks))
	fails := make(map[string]int, len(tasks))
	polled := make(map[string]bool, len(tasks)) // task completed a poll in this leadership
	for _, t := range tasks {
		if last, ok := s.lastFetch(src.Name(), t.Key); ok {
			due[t.Key] = last.Add(cadence(t))
		}
	}
	for {
		var pick *Task
		var at time.Time
		for i := range tasks {
			d := due[tasks[i].Key]
			if pick == nil || d.Before(at) {
				pick, at = &tasks[i], d
			}
		}
		if w := time.Until(at); w > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(w):
			}
		}
		if err := lim.wait(ctx); err != nil {
			return
		}
		fctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		prints, err := safeFetch(fctx, src, *pick)
		cancel()
		if ctx.Err() != nil {
			return
		}
		now := time.Now()
		if err != nil {
			fails[pick.Key]++
			d := max(lim.failure(err), backoff(fails[pick.Key]))
			due[pick.Key] = now.Add(d)
			s.log.Warn("oracle: source error", "source", src.Name(), "lane", lane, "task", pick.Key, "err", err, "retry_in", d.Round(time.Second))
			s.persistRefHealth(*pick, err, fails[pick.Key], d)
		} else {
			fails[pick.Key] = 0
			lim.success()
			due[pick.Key] = now.Add(cadence(*pick))
			s.recordFetch(src.Name(), pick.Key, now)
			s.persistRefHealth(*pick, nil, 0, 0)
			// The first poll of a task replays prints from before this
			// leadership (boot backfill): those never go to the decision model.
			raws := make([]rawPrint, 0, len(prints))
			for _, p := range prints {
				raws = append(raws, rawPrint{Print: p, Source: src.Name(), Live: polled[pick.Key]})
			}
			polled[pick.Key] = true
			if len(raws) > 0 {
				s.ingest(ctx, raws)
			}
		}
		s.persistHealth(src.Name(), lane, lim)
	}
}

func safeFetch(ctx context.Context, src Source, t Task) (out []Print, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("source panicked: %v", r)
		}
	}()
	return src.Fetch(ctx, t)
}

// refsBySource groups the source rows of enabled single feeds by source name.
func (s *Service) refsBySource() map[string][]Ref {
	out := map[string][]Ref{}
	for _, f := range s.order {
		if !f.enabled || f.kind != FeedSingle {
			continue
		}
		for _, r := range f.refs {
			out[r.Source] = append(out[r.Source], Ref{ID: r.ID, FeedID: f.id, FeedKey: f.key, Venue: r.Venue, Ref: r.Ref, Params: r.Params})
		}
	}
	for _, refs := range out {
		sort.Slice(refs, func(i, j int) bool { return refs[i].ID.String() < refs[j].ID.String() })
	}
	return out
}

func laneKey(source, lane string) string { return source + "|" + lane }

func (s *Service) limiterFor(name, lane string, b Budget) *limiter {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := laneKey(name, lane)
	lim := s.limiters[k]
	if lim == nil {
		lim = &limiter{minGap: b.gap(), lastOK: s.restoredOK[k]}
		s.limiters[k] = lim
	}
	lim.mu.Lock()
	lim.minGap = b.gap()
	lim.mu.Unlock()
	return lim
}
