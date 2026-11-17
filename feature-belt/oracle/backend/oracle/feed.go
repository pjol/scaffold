package oracle

import (
	"encoding/json"
	"math"
	"sort"
	"time"

	"github.com/google/uuid"
)

// Feed kinds.
const (
	FeedSingle = "single" // priced from its own sources
	FeedBasket = "basket" // Σ quantity × part price
)

// Status of a feed.
const (
	StatusLive  = "live"  // an accepted observation within the staleness window
	StatusStale = "stale" // none within the window: consumers should stop relying on it
	StatusSeed  = "seed"  // no source has established a price yet; price is the seed (or none)
	StatusOff   = "off"   // disabled by staff
)

// Verdicts.
const (
	VerdictAccepted     = "accepted"
	VerdictDownweighted = "downweighted"
	VerdictRejected     = "rejected"
)

const maxRecsPerFeed = 400

// sourceRef is one row of oracle_feed_sources: where a single's prints come from.
type sourceRef struct {
	ID      uuid.UUID
	Source  string // registered source name (http, manual, …)
	Venue   string // label compared across venues (coinbase, kraken, store)
	Ref     string // the source's id for the feed (a URL, a product id)
	Params  json.RawMessage
	Weight  float64  // multiplies kindWeight; 1 when unset
	Premium *float64 // fixed venue premium; nil = learned
}

type part struct {
	qty float64
	f   *feed
}

// feed is one priced thing. Singles run their own screened, slew-limited
// pipeline; baskets sum their parts.
type feed struct {
	id       uuid.UUID
	key      string
	name     string
	category string
	quote    string
	decimals int32
	kind     string
	listed   bool
	enabled  bool
	seed     float64 // 0 = no seed: the first live target sets the price
	p        Params

	refs    []sourceRef // singles
	parts   []part      // baskets
	baskets []*feed     // baskets this single belongs to

	// pipeline (singles)
	price        float64 // published, slew-limited; 0 until known
	target       float64
	live         bool
	lastAccepted time.Time
	recs         []*rec // sorted by observed time
	seen         map[string]struct{}
	latest       map[string]*rec          // latest record per venue (any verdict)
	premiums     map[string]*premiumState // learned venue premiums
}

func newFeed(id uuid.UUID, key string, seed float64, p Params) *feed {
	return &feed{
		id: id, key: key, name: key, kind: FeedSingle, decimals: 2, quote: "USD", listed: true, enabled: true,
		seed: seed, p: p, price: seed, target: seed,
		seen: map[string]struct{}{}, latest: map[string]*rec{}, premiums: map[string]*premiumState{},
	}
}

// rec is a screened observation as the pipeline keeps it.
type rec struct {
	obs      Observation
	dedupe   string
	px       float64 // normalised by the venue premium
	raw      float64
	qty      float64
	observed time.Time
	baseW    float64 // kind × venue weight × screening factor (0 when rejected)
	// pending records await the decision model: they inform screening of later
	// prints but don't count toward the target until finalised.
	pending bool
	// superseded aggregates (an older market/listing value from the same venue)
	// stay in history but no longer count toward the target.
	superseded bool
}

func (r *rec) rejected() bool  { return r.obs.Verdict == VerdictRejected }
func (r *rec) aggregate() bool { return r.obs.Kind != KindSale }

func (f *feed) status(now time.Time) string {
	if !f.enabled {
		return StatusOff
	}
	if !f.live {
		return StatusSeed
	}
	if now.Sub(f.lastAccepted) > f.p.StaleAfter {
		return StatusStale
	}
	return StatusLive
}

func (f *feed) refFor(venue string) *sourceRef {
	for i := range f.refs {
		if f.refs[i].Venue == venue {
			return &f.refs[i]
		}
	}
	return nil
}

// add inserts a screened record, keeping recs sorted and bounded. A
// non-pending record is resolved immediately.
func (f *feed) add(r *rec) {
	f.seen[r.dedupe] = struct{}{}
	i := sort.Search(len(f.recs), func(i int) bool { return f.recs[i].observed.After(r.observed) })
	f.recs = append(f.recs, nil)
	copy(f.recs[i+1:], f.recs[i:])
	f.recs[i] = r
	if len(f.recs) > maxRecsPerFeed {
		drop := f.recs[:len(f.recs)-maxRecsPerFeed]
		for _, d := range drop {
			delete(f.seen, d.dedupe)
		}
		f.recs = append([]*rec(nil), f.recs[len(f.recs)-maxRecsPerFeed:]...)
	}
	if l := f.latest[r.obs.Venue]; l == nil || !l.observed.After(r.observed) {
		f.latest[r.obs.Venue] = r
	}
	if !r.pending {
		f.resolve(r)
	}
}

// resolve makes a record count: it supersedes older aggregates from the same
// venue and kind and refreshes the staleness clock.
func (f *feed) resolve(r *rec) {
	r.pending = false
	if r.aggregate() && !r.rejected() {
		for _, o := range f.recs {
			if o != r && o.obs.Venue == r.obs.Venue && o.obs.Kind == r.obs.Kind && !o.superseded && !o.pending && !o.observed.After(r.observed) {
				o.superseded = true
			}
		}
	}
	if !r.rejected() && r.observed.After(f.lastAccepted) {
		f.lastAccepted = r.observed
	}
}

// weighted is one value with its effective weight in the target.
type weighted struct {
	px, w float64
	sale  bool
}

// targetInputs returns the observations that count toward the target at now,
// with recency decay, screening factor and the per-print cap applied.
func (f *feed) targetInputs(now time.Time) []weighted {
	var out []weighted
	hl := f.p.HalfLife.Hours()
	for _, r := range f.recs {
		if r.rejected() || r.superseded || r.pending || r.baseW <= 0 {
			continue
		}
		age := now.Sub(r.observed)
		if age > f.p.Lookback {
			continue
		}
		if age < 0 {
			age = 0
		}
		w := r.baseW * math.Exp(-math.Ln2*age.Hours()/hl)
		if w <= 0 || math.IsNaN(w) {
			continue
		}
		out = append(out, weighted{px: r.px, w: w, sale: !r.aggregate()})
	}
	capPrints(out, printCapFrac)
	return out
}

// capPrints limits every sale to frac of the total weight. Capping shrinks the
// total, so it iterates; with fewer than 1/frac prints the sales converge
// toward equal weight, which is the intent: no single print dominates.
func capPrints(ws []weighted, frac float64) {
	for iter := 0; iter < 50; iter++ {
		total := 0.0
		for _, x := range ws {
			total += x.w
		}
		limit := frac * total
		changed := false
		for i := range ws {
			if ws[i].sale && ws[i].w > limit*(1+1e-9) {
				ws[i].w = limit
				changed = true
			}
		}
		if !changed {
			return
		}
	}
}

// weightedMedian of the inputs; ok is false when there are none.
func weightedMedian(ws []weighted) (float64, bool) {
	if len(ws) == 0 {
		return 0, false
	}
	s := append([]weighted(nil), ws...)
	sort.Slice(s, func(i, j int) bool { return s[i].px < s[j].px })
	total := 0.0
	for _, x := range s {
		total += x.w
	}
	if total <= 0 {
		return 0, false
	}
	half := total / 2
	acc := 0.0
	for i, x := range s {
		acc += x.w
		if acc > half*(1+1e-12) {
			return x.px, true
		}
		if math.Abs(acc-half) <= half*1e-12 && i+1 < len(s) {
			return (x.px + s[i+1].px) / 2, true // exactly at the midpoint: average the neighbours
		}
	}
	return s[len(s)-1].px, true
}

// hasFreshEvidence reports whether a live target may be established: some
// non-rejected observation inside the staleness window, and either an
// aggregate or at least two sales (one print never sets the price).
func (f *feed) hasFreshEvidence(now time.Time) bool {
	sales, aggs := 0, 0
	fresh := false
	for _, r := range f.recs {
		if r.rejected() || r.superseded || r.pending || now.Sub(r.observed) > f.p.Lookback {
			continue
		}
		if now.Sub(r.observed) <= f.p.StaleAfter {
			fresh = true
		}
		if r.aggregate() {
			aggs++
		} else {
			sales++
		}
	}
	return fresh && (aggs > 0 || sales >= 2)
}

// recompute updates the target from the current observations.
func (f *feed) recompute(now time.Time) {
	if !f.live && !f.hasFreshEvidence(now) {
		return
	}
	t, ok := weightedMedian(f.targetInputs(now))
	if !ok || t <= 0 {
		return
	}
	if !f.live {
		// The first live target initialises the price directly.
		f.live = true
		f.price = t
	}
	f.target = t
}

// slew moves the published price toward target by at most MaxMoveBpsPerMin
// over dt.
func (f *feed) slew(dt time.Duration) {
	if !f.live || dt <= 0 {
		return
	}
	maxStep := f.price * f.p.MaxMoveBpsPerMin / 1e4 * dt.Minutes()
	d := f.target - f.price
	switch {
	case d > maxStep:
		f.price += maxStep
	case d < -maxStep:
		f.price -= maxStep
	default:
		f.price = f.target
	}
}

// ---- venue premiums ----

// premiumState is a venue's learned premium over the other venues: the
// median of its recent log price ratios against their median.
type premiumState struct {
	logs []float64 // newest last, at most premiumSamples
}

func (ps *premiumState) value() (float64, bool) {
	if ps == nil || len(ps.logs) < premiumMinSamples {
		return 0, false
	}
	p := math.Exp(median(ps.logs)) - 1
	if math.Abs(p) < premiumFloor {
		return 0, true
	}
	return math.Max(-premiumCap, math.Min(premiumCap, p)), true
}

// premiumFor is the premium a venue's prints are divided by: the fixed one on
// its source row, else the learned one (when learning is on), else 0.
func (f *feed) premiumFor(venue string, learn bool) (p float64, learned bool) {
	if ref := f.refFor(venue); ref != nil && ref.Premium != nil {
		return *ref.Premium, false
	}
	if !learn {
		return 0, false
	}
	if v, ok := f.premiums[venue].value(); ok {
		return v, v != 0
	}
	return 0, false
}

// learnPremium records one raw print of venue against the venues' current
// consensus. The consensus is cross-sectional (each venue's latest raw print
// within the staleness window), so a market-wide trend cancels out and the
// learned premiums never feed back into themselves: they depend on raw
// prints only. Venues with a fixed premium (usually 0, the reference market)
// are anchors: when there are any, the consensus is theirs alone; otherwise
// it is the median of every current venue, this one included (with two
// venues and no anchor the gap splits between them). Every print counts
// whatever its verdict: a venue that runs persistently off is normalised
// onto the others, so it contributes relative moves, never its level.
func (f *feed) learnPremium(venue string, raw float64, at time.Time) {
	if ref := f.refFor(venue); (ref != nil && ref.Premium != nil) || raw <= 0 {
		return
	}
	var anchors []float64
	all := []float64{raw}
	for v, r := range f.latest {
		if v == venue || r.rejected() || at.Sub(r.observed) > f.p.StaleAfter || r.observed.After(at.Add(time.Minute)) || r.raw <= 0 {
			continue
		}
		if ref := f.refFor(v); ref != nil && ref.Premium != nil {
			anchors = append(anchors, r.raw/(1+*ref.Premium))
			all = append(all, r.raw/(1+*ref.Premium))
			continue
		}
		all = append(all, r.raw)
	}
	var m float64
	switch {
	case len(anchors) > 0:
		m = median(anchors)
	case len(all) >= 2:
		m = median(all)
	default:
		return
	}
	if m <= 0 {
		return
	}
	ratio := raw / m
	if ratio < 0.5 || ratio > 2 {
		return // a different product or a broken print, not a premium
	}
	ps := f.premiums[venue]
	if ps == nil {
		ps = &premiumState{}
		f.premiums[venue] = ps
	}
	ps.logs = append(ps.logs, math.Log(ratio))
	if len(ps.logs) > premiumSamples {
		ps.logs = append([]float64(nil), ps.logs[len(ps.logs)-premiumSamples:]...)
	}
}
