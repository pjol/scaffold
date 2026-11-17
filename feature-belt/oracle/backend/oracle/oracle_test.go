package oracle

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
)

// screenInto screens one print with stat-v1 exactly as screenOne does
// (premium normalisation, measure, score) and records it.
func screenInto(f *feed, venue, kind string, raw, qty float64, at, now time.Time) *rec {
	prem, _ := f.premiumFor(venue, true)
	px := raw / (1 + prem)
	ft := measure(f, venue, kind, px, qty, at, now, f.price)
	sc, reasons := score(ft, kind, f.p.StaleAfter)
	verdict, factor := verdictFor(sc)
	r := &rec{
		obs:    Observation{Venue: venue, Kind: kind, Verdict: verdict, Score: sc, Reasons: reasons, Model: statModel, ObservedAt: at, Premium: prem},
		dedupe: fmt.Sprintf("%s:%d:%f", venue, at.UnixNano(), raw), px: px, raw: raw, qty: qty, observed: at,
		baseW: kindWeight(kind, qty) * factor,
	}
	f.learnPremium(venue, raw, at)
	f.add(r)
	return r
}

// liquid returns a live intraday feed around 100 with 30 sales over 6 hours
// and a 7-day market median.
func liquid(now time.Time) *feed {
	f := newFeed(uuid.New(), "TEST", 100, Profiles["intraday"])
	start := now.Add(-6 * time.Hour)
	noise := []float64{0, 1.5, -1.2, 0.8, -0.5, 1.9, -1.8, 0.3, -0.9, 1.1}
	screenInto(f, "agg", KindMarket, 100, 20, start, start)
	for i := 0; i < 30; i++ {
		at := start.Add(time.Duration(i) * 12 * time.Minute)
		screenInto(f, "trades", KindSale, 100+noise[i%len(noise)], 1, at, at)
		f.recompute(at)
	}
	f.recompute(now)
	f.price = f.target
	return f
}

func TestSingleOutlierDoesNotMovePrice(t *testing.T) {
	now := time.Now()
	f := liquid(now)
	before := f.target
	if !f.live || math.Abs(before-100) > 1.5 {
		t.Fatalf("setup: live=%v target=%.2f", f.live, before)
	}
	r := screenInto(f, "trades", KindSale, 300, 1, now, now)
	if r.obs.Verdict != VerdictRejected {
		t.Fatalf("3× print verdict = %s (score %.2f, %v)", r.obs.Verdict, r.obs.Score, r.obs.Reasons)
	}
	f.recompute(now)
	if math.Abs(f.target-before) > 0.01 {
		t.Fatalf("rejected outlier moved target %.2f → %.2f", before, f.target)
	}
	// A large but plausible print (+15%) moves the median by at most one rank.
	r = screenInto(f, "trades", KindSale, 115, 1, now.Add(time.Minute), now.Add(time.Minute))
	f.recompute(now.Add(time.Minute))
	if move := math.Abs(f.target-before) / before; move > 0.01 {
		t.Fatalf("single +15%% print moved target by %.2f%% (verdict %s)", 100*move, r.obs.Verdict)
	}
	// And the published price can't jump: one 3 s tick moves at most 15 bps/min × 0.05 min.
	f.target = 130
	p0 := f.price
	f.slew(3 * time.Second)
	if (f.price-p0)/p0 > 15.0/1e4*0.05+1e-12 {
		t.Fatalf("slew exceeded: %.5f → %.5f", p0, f.price)
	}
}

func TestWalkUpBurstFlagged(t *testing.T) {
	now := time.Now()
	f := liquid(now)
	before := f.target
	prints := []float64{103, 106, 110, 115, 121, 128}
	var verdicts []string
	for i, px := range prints {
		at := now.Add(time.Duration(i) * 3 * time.Minute)
		r := screenInto(f, "trades", KindSale, px, 1, at, at)
		verdicts = append(verdicts, r.obs.Verdict)
	}
	for i := 2; i < len(prints); i++ {
		if verdicts[i] == VerdictAccepted {
			t.Fatalf("walk-up print %d (%.0f) accepted: %v", i, prints[i], verdicts)
		}
	}
	for i := 3; i < len(prints); i++ {
		if verdicts[i] != VerdictRejected {
			t.Fatalf("walk-up print %d (%.0f) not rejected: %v", i, prints[i], verdicts)
		}
	}
	f.recompute(now.Add(20 * time.Minute))
	if f.target > before*1.025 {
		t.Fatalf("walk-up moved target %.2f → %.2f", before, f.target)
	}
}

func TestSlowTrendFollowedWithinSlew(t *testing.T) {
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	f := newFeed(uuid.New(), "TREND", 100, Profiles["daily"])
	limit := f.p.MaxMoveBpsPerMin / 1e4
	step := 5 * time.Minute
	rejected := 0
	noise := []float64{0.01, -0.012, 0.004, -0.006, 0.015, -0.009}
	nextSale, nextMarket := start, start
	k := 0
	for now := start; now.Before(start.Add(15 * 24 * time.Hour)); now = now.Add(step) {
		fair := 100 * math.Pow(1.01, now.Sub(start).Hours()/24) // +1%/day
		if !now.Before(nextMarket) {
			screenInto(f, "daily", KindMarket, math.Round(fair*100)/100, 0, now, now)
			nextMarket = nextMarket.Add(24 * time.Hour)
		}
		if !now.Before(nextSale) {
			r := screenInto(f, "trades", KindSale, math.Round(fair*(1+noise[k%len(noise)])*100)/100, 1, now, now)
			if r.obs.Verdict == VerdictRejected {
				rejected++
			}
			k++
			nextSale = nextSale.Add(4 * time.Hour)
		}
		p0 := f.price
		f.recompute(now)
		wasLive := f.live
		f.slew(step)
		if wasLive && p0 > 0 && math.Abs(f.price-p0)/p0 > limit*step.Minutes()+1e-9 && now.After(start) {
			t.Fatalf("slew limit exceeded at %s: %.4f → %.4f", now, p0, f.price)
		}
	}
	fair := 100 * math.Pow(1.01, 15)
	if rejected > 0 {
		t.Fatalf("%d genuine trend prints rejected", rejected)
	}
	if dev := math.Abs(f.price-fair) / fair; dev > 0.03 {
		t.Fatalf("price %.2f didn't follow the trend to %.2f (%.1f%%)", f.price, fair, 100*dev)
	}
}

func TestFirstTargetNeedsMoreThanOnePrint(t *testing.T) {
	now := time.Now()
	f := newFeed(uuid.New(), "X", 100, Profiles["daily"])
	screenInto(f, "trades", KindSale, 120, 1, now, now)
	f.recompute(now)
	if f.live {
		t.Fatalf("one sale established a live price")
	}
	screenInto(f, "trades", KindSale, 121, 1, now.Add(time.Minute), now.Add(time.Minute))
	f.recompute(now.Add(time.Minute))
	if !f.live || math.Abs(f.price-120.5) > 1 {
		t.Fatalf("two sales: live=%v price=%.2f", f.live, f.price)
	}
}

func TestNoSeedFirstPrintsAccepted(t *testing.T) {
	now := time.Now()
	f := newFeed(uuid.New(), "NEW", 0, Profiles["realtime"])
	r := screenInto(f, "a", KindMarket, 64000, 0, now, now)
	if r.obs.Verdict != VerdictAccepted {
		t.Fatalf("first print on a seedless feed: %s %v", r.obs.Verdict, r.obs.Reasons)
	}
	f.recompute(now)
	if !f.live || f.price != 64000 {
		t.Fatalf("seedless feed: live=%v price=%.2f", f.live, f.price)
	}
	if f.status(now) != StatusLive || f.status(now.Add(11*time.Minute)) != StatusStale {
		t.Fatalf("status: %s then %s", f.status(now), f.status(now.Add(11*time.Minute)))
	}
}

func TestLearnedPremiumNormalisesVenue(t *testing.T) {
	now := time.Now()
	f := newFeed(uuid.New(), "PREM", 100, Profiles["intraday"])
	start := now.Add(-4 * time.Hour)
	// Two cash venues and "retail", which runs 10% above them; all report
	// every 10 minutes while the market trends up 2% over the run.
	var last *rec
	for i := 0; i < 24; i++ {
		at := start.Add(time.Duration(i) * 10 * time.Minute)
		lvl := 100 * (1 + 0.02*float64(i)/24)
		screenInto(f, "cash-a", KindMarket, lvl, 0, at, at)
		screenInto(f, "cash-b", KindMarket, lvl*1.001, 0, at.Add(30*time.Second), at.Add(30*time.Second))
		last = screenInto(f, "retail", KindMarket, lvl*1.1, 0, at.Add(time.Minute), at.Add(time.Minute))
		f.recompute(at.Add(time.Minute))
	}
	p, learned := f.premiumFor("retail", true)
	if !learned || math.Abs(p-0.10) > 0.005 {
		t.Fatalf("learned premium = %.4f (learned %v), want ≈0.10", p, learned)
	}
	for _, v := range []string{"cash-a", "cash-b"} {
		if p, _ := f.premiumFor(v, true); p != 0 {
			t.Fatalf("%s learned %.4f, want 0", v, p)
		}
	}
	lvl := 100 * (1 + 0.02*23/24)
	if math.Abs(last.px-lvl) > 0.6 || last.obs.Verdict != VerdictAccepted {
		t.Fatalf("normalised retail print = %.2f (%s), want ≈%.2f accepted", last.px, last.obs.Verdict, lvl)
	}

	// With an anchor (a fixed premium, here 0), two venues are enough and the
	// whole gap goes to the learning venue.
	g := newFeed(uuid.New(), "ANCHOR", 100, Profiles["intraday"])
	zero := 0.0
	g.refs = []sourceRef{{Venue: "cash", Premium: &zero}, {Venue: "retail"}}
	for i := 0; i < 12; i++ {
		at := start.Add(time.Duration(i) * 10 * time.Minute)
		screenInto(g, "cash", KindMarket, 100, 0, at, at)
		screenInto(g, "retail", KindMarket, 108, 0, at.Add(time.Minute), at.Add(time.Minute))
	}
	if p, learned := g.premiumFor("retail", true); !learned || math.Abs(p-0.08) > 0.001 {
		t.Fatalf("anchored premium = %.4f (learned %v), want 0.08", p, learned)
	}
	if p, learned := g.premiumFor("cash", true); p != 0 || learned {
		t.Fatalf("anchor premium = %.4f learned=%v", p, learned)
	}
}

func TestCapPrintsLimitsEachSale(t *testing.T) {
	ws := []weighted{{px: 1, w: 10, sale: true}, {px: 2, w: 1, sale: true}, {px: 3, w: 1, sale: true}, {px: 4, w: 5}}
	capPrints(ws, 0.12)
	total := 0.0
	for _, w := range ws {
		total += w.w
	}
	for _, w := range ws {
		if w.sale && w.w > 0.12*total*(1+1e-6) {
			t.Fatalf("sale weight %.3f above 12%% of %.3f", w.w, total)
		}
	}
	if m, ok := weightedMedian(ws); !ok || m != 4 {
		t.Fatalf("weighted median = %v, want the aggregate's 4 once sales are capped", m)
	}
}

func TestDecisionCanOnlyRaiseRisk(t *testing.T) {
	sc, v, _, _ := combineDecision(0.1, Decision{Manipulation: 0.05, Include: 0.9})
	if sc != 0.1 || v != VerdictAccepted {
		t.Fatalf("a calmer model lowered the score: %.2f %s", sc, v)
	}
	sc, v, _, _ = combineDecision(0.1, Decision{Manipulation: 0.8, Include: 0.9})
	if sc != 0.8 || v != VerdictRejected {
		t.Fatalf("model risk not applied: %.2f %s", sc, v)
	}
	if _, v, f, _ := combineDecision(0.1, Decision{Manipulation: 0.1, Include: 0.2}); v != VerdictRejected || f != 0 {
		t.Fatalf("include < 0.35 not excluded: %s %.2f", v, f)
	}
}

func TestBasketPointsCarryAndSkip(t *testing.T) {
	d := func(n int) time.Time { return time.Date(2026, 1, n, 0, 0, 0, 0, time.UTC) }
	a := []histPt{{Day: d(1), Close: 10, Source: "oracle"}, {Day: d(3), Close: 12, Source: "oracle"}}
	b := []histPt{{Day: d(2), Close: 1, Source: "manual"}, {Day: d(3), Close: 2, Source: "oracle"}}
	pts := basketPoints([]float64{1, 10}, [][]histPt{a, b})
	if len(pts) != 2 || pts[0].Close != 20 || pts[1].Close != 32 || pts[1].Source != "oracle" || pts[0].Source != "basket" {
		t.Fatalf("basket points = %+v", pts)
	}
}

func TestBuildBarsSeamsBackfillOntoOracle(t *testing.T) {
	var pts []histPt
	for i := 0; i < 30; i++ {
		pts = append(pts, histPt{Day: time.Date(2026, 1, 1+i, 0, 0, 0, 0, time.UTC), Close: 50, Source: "manual"})
	}
	pts = append(pts, histPt{Day: time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC), Close: 60, Source: "oracle"})
	bars := buildBars(pts, 60, 2)
	if len(bars) != 31 || bars[30].Close != "60.00" || bars[29].Close != "60.00" || bars[0].Close != "50.00" {
		t.Fatalf("bars = first %+v, last two %+v %+v", bars[0], bars[29], bars[30])
	}
}

func TestJSONPaths(t *testing.T) {
	doc := map[string]any{"result": map[string]any{"X": map[string]any{"c": []any{"64000.5", "0.1"}}}, "ts": 1.7e9}
	if v, err := jsonNumber(doc, "result.X.c.0"); err != nil || v != 64000.5 {
		t.Fatalf("jsonNumber = %v %v", v, err)
	}
	if _, err := jsonNumber(doc, "result.Y"); err == nil {
		t.Fatalf("missing key not reported")
	}
	if at, err := parseTime(doc["ts"]); err != nil || at.Unix() != 1.7e9 {
		t.Fatalf("parseTime = %v %v", at, err)
	}
	if at, err := parseTime("1700000000000"); err != nil || at.Unix() != 1.7e9 {
		t.Fatalf("parseTime ms = %v %v", at, err)
	}
}

func TestBudgetGap(t *testing.T) {
	b := Budget{MinGap: time.Second, Quota: 1000, Per: 30 * 24 * time.Hour}
	if g := b.gap(); g < 43*time.Minute || g > 44*time.Minute {
		t.Fatalf("quota gap = %s, want ≈43m12s", g)
	}
	if g := (Budget{MinGap: 2 * time.Second}).gap(); g != 2*time.Second {
		t.Fatalf("gap = %s", g)
	}
}

func TestParamsValidate(t *testing.T) {
	for name, p := range Profiles {
		if err := p.Validate(); err != nil {
			t.Fatalf("profile %s: %v", name, err)
		}
	}
	bad := Profiles["daily"]
	bad.BurstWindow = 0
	if bad.Validate() == nil {
		t.Fatalf("zero burst window accepted")
	}
}
