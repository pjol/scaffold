package oracle

import (
	"fmt"
	"math"
	"time"
)

// Params are the aggregation knobs of one feed (oracle_feeds columns).
type Params struct {
	// HalfLife of the exponential recency weight on every observation.
	HalfLife time.Duration
	// StaleAfter: no accepted observation within this window → status stale.
	StaleAfter time.Duration
	// MaxMoveBpsPerMin caps how fast the published price slews toward its target.
	MaxMoveBpsPerMin float64
	// Lookback bounds which observations count toward the target at all.
	Lookback time.Duration
	// RefWindow bounds the history the screen compares a print against.
	RefWindow time.Duration
	// BurstWindow is the window for same-direction burst and walk-up detection.
	BurstWindow time.Duration
}

// Profiles are the starting points for a new feed's Params. "daily" suits
// sources that reprice once a day plus a trickle of sales: a 24 h half-life
// and a 6 bps/min slew (3.6%/h) follow real moves within hours while a forced
// print can only nudge the price. "intraday" suits continuously traded
// markets: 2 h half-life, 15 bps/min (9%/h), stale after 45 min. "realtime"
// suits liquid quotes polled every minute or so: 10 min half-life,
// 60 bps/min, stale after 10 min.
var Profiles = map[string]Params{
	"daily":    {HalfLife: 24 * time.Hour, StaleAfter: 48 * time.Hour, MaxMoveBpsPerMin: 6, Lookback: 120 * 24 * time.Hour, RefWindow: 30 * 24 * time.Hour, BurstWindow: 6 * time.Hour},
	"intraday": {HalfLife: 2 * time.Hour, StaleAfter: 45 * time.Minute, MaxMoveBpsPerMin: 15, Lookback: 14 * 24 * time.Hour, RefWindow: 3 * 24 * time.Hour, BurstWindow: time.Hour},
	"realtime": {HalfLife: 10 * time.Minute, StaleAfter: 10 * time.Minute, MaxMoveBpsPerMin: 60, Lookback: 2 * 24 * time.Hour, RefWindow: 6 * time.Hour, BurstWindow: 15 * time.Minute},
}

// Validate reports the first out-of-range knob.
func (p Params) Validate() error {
	switch {
	case p.HalfLife < time.Minute || p.HalfLife > 90*24*time.Hour:
		return fmt.Errorf("half_life must be between 1m and 90d")
	case p.StaleAfter < time.Minute || p.StaleAfter > 30*24*time.Hour:
		return fmt.Errorf("stale_after must be between 1m and 30d")
	case p.MaxMoveBpsPerMin <= 0 || p.MaxMoveBpsPerMin > 10000 || math.IsNaN(p.MaxMoveBpsPerMin):
		return fmt.Errorf("max_move_bps_per_min must be above 0 and at most 10000")
	case p.Lookback < p.StaleAfter || p.Lookback > 365*24*time.Hour:
		return fmt.Errorf("lookback must be at least stale_after and at most 365d")
	case p.RefWindow < time.Minute || p.RefWindow > p.Lookback:
		return fmt.Errorf("ref_window must be between 1m and lookback")
	case p.BurstWindow < time.Minute || p.BurstWindow > p.RefWindow:
		return fmt.Errorf("burst_window must be between 1m and ref_window")
	}
	return nil
}

// Screening thresholds (stat-v1).
const (
	acceptBelow = 0.35 // score < acceptBelow → accepted at full weight
	rejectAbove = 0.70 // score > rejectAbove → rejected; between → weight × (1 − score)

	printCapFrac = 0.12 // no single sale may carry more than 12% of the target's total weight
)

// Kinds of observation.
const (
	KindSale    = "sale"    // one completed trade
	KindMarket  = "market"  // an aggregate over many trades (a daily market price, a 7-day median, a spot quote)
	KindListing = "listing" // an asking price, not a trade
)

func validKind(k string) bool { return k == KindSale || k == KindMarket || k == KindListing }

// kindWeight is how much one observation counts in the weighted median
// before recency decay, screening and the venue's own weight. A sale is the
// unit; an aggregate counts as several sales because it summarises many
// (with a count, more the more it covers); a listing is only an ask.
func kindWeight(kind string, qty float64) float64 {
	switch kind {
	case KindMarket:
		if qty > 0 {
			return 1 + math.Min(qty/5, 4)
		}
		return 3
	case KindListing:
		return 0.3
	}
	return 1
}

// Venue premiums: a venue whose prices run consistently above or below the
// others (a market paid in store credit, a retail venue) is normalised by
// its premium before screening. A premium is fixed on the feed's source row
// or learned from the venue's own prints against the other venues.
const (
	premiumSamples    = 32    // log-ratios kept per venue
	premiumMinSamples = 8     // before a learned premium applies
	premiumFloor      = 0.005 // smaller learned premiums are treated as none
	premiumCap        = 0.5   // |premium| never exceeds 50%
)
