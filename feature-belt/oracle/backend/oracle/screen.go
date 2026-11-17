package oracle

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// statModel names the statistical screen in Observation.Model.
const statModel = "stat-v1"

// features are what stat-v1 measures about one print relative to the feed's
// recent history and the current published price.
type features struct {
	RefN       int     // reference observations available
	RefMedian  float64 // median of the reference set (or the published price when thin)
	RobustZ    float64 // |p − median| / (1.4826 · MAD), MAD floored at 2.5% of median
	DevOracle  float64 // |p − price| / price
	Divergence float64 // |p − other venues' median| / that median; 0 when no other venue
	OtherSrcN  int
	BurstN     int  // same-direction prints (> 2% off) within the burst window, including this one
	WalkUp     bool // those prints are monotonic in the direction of deviation (walk-up / walk-down)
	QtyAnomaly bool
	Thin       bool
	Future     bool
	AgeDays    float64
	Direction  int  // +1 above reference, −1 below
	SeedAnchor bool // the feed isn't live yet: deviation is measured against the seed
}

// verdictFor maps a manipulation score to a verdict and the factor applied to
// the base weight.
func verdictFor(score float64) (string, float64) {
	switch {
	case score > rejectAbove:
		return VerdictRejected, 0
	case score >= acceptBelow:
		return VerdictDownweighted, 1 - score
	default:
		return VerdictAccepted, 1
	}
}

func sigmoid(x float64) float64 { return 1 / (1 + math.Exp(-x)) }

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

// measure computes the features of a candidate print (already normalised by
// its venue premium) against the feed's state. oracle is the current
// published price; before the feed is live it is the seed, a loose anchor,
// or 0 when the feed has none.
func measure(f *feed, venue, kind string, px, qty float64, observed, now time.Time, oracle float64) features {
	var ft features
	p := f.p
	ft.SeedAnchor = !f.live

	// Reference set: non-rejected, non-superseded prints in the reference
	// window, most recent 60.
	var ref []float64
	var refQty []float64
	for i := len(f.recs) - 1; i >= 0 && len(ref) < 60; i-- {
		r := f.recs[i]
		if r.rejected() || r.superseded || now.Sub(r.observed) > p.RefWindow {
			continue
		}
		ref = append(ref, r.px)
		if !r.aggregate() && r.qty > 0 {
			refQty = append(refQty, r.qty)
		}
	}
	ft.RefN = len(ref)
	ft.Thin = ft.RefN < 5
	m := oracle
	if ft.RefN >= 3 {
		m = median(ref)
	}
	ft.RefMedian = m
	devs := make([]float64, 0, len(ref))
	for _, x := range ref {
		devs = append(devs, math.Abs(x-m))
	}
	mad := 0.0
	if len(devs) >= 3 {
		mad = median(devs)
	}
	if floor := 0.025 * m; mad < floor {
		mad = floor
	}
	if m > 0 && (ft.RefN >= 3 || !ft.SeedAnchor) {
		ft.RobustZ = math.Abs(px-m) / (1.4826 * mad)
	}
	if oracle > 0 {
		ft.DevOracle = math.Abs(px-oracle) / oracle
	}
	if px >= m {
		ft.Direction = 1
	} else {
		ft.Direction = -1
	}

	// Cross-venue divergence: compare with the latest non-rejected value from
	// every other venue.
	var others []float64
	for v, r := range f.latest {
		if v == venue || r.rejected() || now.Sub(r.observed) > p.RefWindow {
			continue
		}
		others = append(others, r.px)
	}
	ft.OtherSrcN = len(others)
	if len(others) > 0 {
		if om := median(others); om > 0 {
			ft.Divergence = math.Abs(px-om) / om
		}
	}

	// Same-direction burst / walk-up among sales in the burst window.
	if kind == KindSale && m > 0 {
		type pt struct {
			t  time.Time
			px float64
		}
		var pts []pt
		for _, r := range f.recs {
			if r.aggregate() || r.observed.Before(observed.Add(-p.BurstWindow)) || r.observed.After(observed) {
				continue
			}
			d := (r.px - m) / m
			if math.Abs(d) > 0.02 && (d > 0) == (ft.Direction > 0) {
				pts = append(pts, pt{r.observed, r.px})
			}
		}
		if d := (px - m) / m; math.Abs(d) > 0.02 {
			pts = append(pts, pt{observed, px})
		}
		ft.BurstN = len(pts)
		if len(pts) >= 3 {
			sort.Slice(pts, func(i, j int) bool { return pts[i].t.Before(pts[j].t) })
			mono := true
			for i := 1; i < len(pts); i++ {
				if float64(ft.Direction)*(pts[i].px-pts[i-1].px) <= 0 {
					mono = false
					break
				}
			}
			ft.WalkUp = mono
		}
	}

	// Quantity anomaly: a bulk print far above the usual lot size.
	if kind == KindSale && qty >= 3 {
		typical := 1.0
		if len(refQty) >= 3 {
			typical = median(refQty)
		}
		ft.QtyAnomaly = qty >= 3*typical
	}

	ft.Future = observed.After(now.Add(10 * time.Minute))
	ft.AgeDays = now.Sub(observed).Hours() / 24
	return ft
}

// score combines features into a manipulation probability with a fixed
// logistic. Coefficients are hand-set so that: a print within ~5% of history
// scores ≈ 0.05; a lone 25% outlier lands in the downweight band; ≥ 35% off
// is rejected; a monotonic walk-up is downweighted from its 3rd print and
// rejected from its 4th.
func score(ft features, kind string, staleAfter time.Duration) (float64, []string) {
	x := -3.0
	var reasons []string
	if ft.RobustZ > 2.5 {
		x += 0.6 * (ft.RobustZ - 2.5)
		side := "above"
		if ft.Direction < 0 {
			side = "below"
		}
		reasons = append(reasons, fmt.Sprintf("%.1f MAD %s recent median", ft.RobustZ, side))
	}
	free := 0.05
	what := "published price"
	if ft.SeedAnchor {
		free, what = 0.30, "seed price"
	}
	if ft.DevOracle > free {
		x += 10 * (ft.DevOracle - free)
		reasons = append(reasons, fmt.Sprintf("%.0f%% from %s", 100*ft.DevOracle, what))
	}
	if ft.Divergence > 0.10 {
		x += 5 * (ft.Divergence - 0.10)
		reasons = append(reasons, fmt.Sprintf("%.0f%% off other venues", 100*ft.Divergence))
	}
	if ft.BurstN >= 3 {
		x += 0.8 * float64(ft.BurstN-2)
		reasons = append(reasons, fmt.Sprintf("%d same-direction prints in a short window", ft.BurstN))
	}
	if ft.WalkUp {
		x += 1.3
		if ft.Direction > 0 {
			reasons = append(reasons, "monotonic walk-up pattern")
		} else {
			reasons = append(reasons, "monotonic walk-down pattern")
		}
	}
	if ft.QtyAnomaly {
		x += 0.8
		reasons = append(reasons, "unusually large quantity")
	}
	if ft.Thin {
		x += 0.4
		reasons = append(reasons, fmt.Sprintf("thin history (%d reference prints)", ft.RefN))
	}
	if ft.Future {
		x += 2.0
		reasons = append(reasons, "timestamp in the future")
	}
	if kind == KindSale && staleAfter > 0 && ft.AgeDays*24 > 4*staleAfter.Hours() {
		x += 0.3
		reasons = append(reasons, fmt.Sprintf("print is %.0f days old", ft.AgeDays))
	}
	if kind == KindListing {
		x += 0.5
		reasons = append(reasons, "listing, not a sale")
	}
	return sigmoid(x), reasons
}
