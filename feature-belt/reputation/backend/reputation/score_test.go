package reputation

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/config"
)

var now = time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)

func dd(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func uid(s string) uuid.UUID { return uuid.NewSHA1(uuid.NameSpaceOID, []byte(s)) }

// series builds n consecutive days ending today with the given returns
// (cycled), each active with one action and 100 of volume.
func series(n int, rets ...float64) []Day {
	var out []Day
	start := dayStart(now).AddDate(0, 0, -(n - 1))
	eq := 1000.0
	for i := 0; i < n; i++ {
		r := rets[i%len(rets)]
		open := eq
		eq *= 1 + r
		out = append(out, Day{Day: start.AddDate(0, 0, i), Open: decimal.NewFromFloat(open), Close: decimal.NewFromFloat(eq),
			Flows: decimal.Zero, Ret: r, Active: true, Activity: 1, Volume: dd("100"), Source: SourcePushed})
	}
	return out
}

func comp(t *testing.T, r Reputation, key string) Component {
	t.Helper()
	for _, c := range r.Components {
		if c.Key == key {
			return c
		}
	}
	t.Fatalf("no %s component", key)
	return Component{}
}

func near(t *testing.T, what string, got, want, tol float64) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Fatalf("%s = %v, want %v ± %v", what, got, want, tol)
	}
}

// Golden: 30 active days alternating +1% / −0.5%, active for 200 days, no
// community signals.
func TestGoldenScore(t *testing.T) {
	in := Input{Now: now, Started: now.AddDate(0, 0, -200), Days: series(30, 0.01, -0.005)}
	r := DefaultModel.Compute(in)
	// performance: mean 0.0025, sample std 0.0076282 → S 0.32773; S* = S·30/50 = 0.19664 → 50 + 250·S* = 99.16
	p := comp(t, r, ComponentPerformance)
	near(t, "performance", p.Value, 99.2, 0.06)
	if p.Metrics["sharpe_90d"] != "0.328" || p.Metrics["shrunk_90d"] != "0.197" || !p.Scored {
		t.Fatalf("performance metrics %v", p.Metrics)
	}
	// risk: the worst fall is one −0.5% day → 100·(1 − 0.005/0.6) = 99.17
	near(t, "risk", comp(t, r, ComponentRisk).Value, 99.2, 0.06)
	// consistency: 30 days span 5 week buckets, all positive → (5+2)/(5+4) = 77.8
	c := comp(t, r, ComponentConsistency)
	near(t, "consistency", c.Value, 77.8, 0.06)
	if c.Metrics["active_weeks_13w"] != "5" {
		t.Fatalf("weeks %v", c.Metrics)
	}
	// tenure: 70·200/180 + 30·30/60 = 92.8
	near(t, "tenure", comp(t, r, ComponentTenure).Value, 92.8, 0.06)
	// community unscored: its 10 points are spread over the other 80
	if comp(t, r, ComponentCommunity).Scored {
		t.Fatal("community scored without signals")
	}
	wants := map[string]string{ComponentPerformance: "0.50", ComponentRisk: "0.25", ComponentConsistency: "0.19",
		ComponentTenure: "0.06", ComponentCommunity: "0.00"}
	for k, w := range wants {
		if got := comp(t, r, k).Weight; got != w {
			t.Fatalf("%s weight %s, want %s", k, got, w)
		}
	}
	// points: .5·99.2 = 49.6, .25·99.2 = 24.8, .1875·77.8 = 14.6, .0625·92.8 = 5.8
	if comp(t, r, ComponentPerformance).Points != 49.6 || comp(t, r, ComponentConsistency).Points != 14.6 {
		t.Fatalf("points %+v", r.Components)
	}
	// score = 49.6 + 24.8 + 14.5875 + 5.8 = 94.8 → 95, elite
	if r.Status != StatusRated || r.Score == nil || *r.Score != 95 || *r.Tier != "elite" || r.Version != Version {
		t.Fatalf("score %+v", r)
	}
	st := r.Stats
	if st.ActiveDays90d != 30 || st.Activity30d != 30 || st.Volume30d != "3000.00" || len(st.Equity) != 30 {
		t.Fatalf("stats %+v", st)
	}
	// compound return: 1.01^15 × 0.995^15 − 1 = 7.69%
	if st.Return90dPct != "7.69" || st.MaxDrawdown90dPct != "0.50" {
		t.Fatalf("returns %s / %s", st.Return90dPct, st.MaxDrawdown90dPct)
	}
	if st.Equity[0].Day != dayStart(now).AddDate(0, 0, -29).Format("2006-01-02") || st.Equity[0].Index != "101.00" {
		t.Fatalf("equity %+v", st.Equity[0])
	}
}

// Shrinkage: the same daily Sharpe counts for less over fewer days.
func TestShrinkage(t *testing.T) {
	few, many := []float64{0.01, -0.005, 0.01, -0.005}, []float64{}
	for i := 0; i < 30; i++ {
		many = append(many, 0.01, -0.005)
	}
	s1, k1 := Sharpe(few)
	s2, k2 := Sharpe(many)
	near(t, "shrink 4", k1, s1*4/24, 1e-9)
	near(t, "shrink 60", k2, s2*60/80, 1e-9)
	if k1 >= k2 {
		t.Fatalf("4 days shrunk to %v, 60 days to %v", k1, k2)
	}
	if s, k := Sharpe([]float64{0.05}); s != 0 || k != 0 {
		t.Fatal("one day has no Sharpe")
	}
	// a flat series is floored at std 0.002, not divided by zero
	s, _ := Sharpe([]float64{0.001, 0.001, 0.001})
	near(t, "floored", s, 0.5, 1e-9)
}

func TestTiersAndCommunityRedistribution(t *testing.T) {
	for score, tier := range map[int]string{0: "building", 39: "building", 40: "steady", 59: "steady", 60: "strong",
		74: "strong", 75: "elite", 100: "elite"} {
		if DefaultModel.TierOf(score) != tier {
			t.Fatalf("TierOf(%d) = %s", score, DefaultModel.TierOf(score))
		}
	}
	in := Input{Now: now, Started: now.AddDate(0, 0, -30), Days: series(30, 0.002, -0.003),
		Community: &Community{Karma: 150, Strikes: 1, Posted: true}}
	r := DefaultModel.Compute(in)
	// community: min(100, 50 + 10·log2(1 + 15)) − 15 = 75
	c := comp(t, r, ComponentCommunity)
	if !c.Scored {
		t.Fatal("community unscored")
	}
	near(t, "community", c.Value, 75, 1e-9)
	// everything scored: weights over all 90 points
	if comp(t, r, ComponentPerformance).Weight != "0.44" || comp(t, r, ComponentCommunity).Weight != "0.11" {
		t.Fatalf("weights %+v", r.Components)
	}
	in.Community.Strikes = 9 // floor at 0
	near(t, "community floor", comp(t, DefaultModel.Compute(in), ComponentCommunity).Value, 0, 1e-9)
	in.Community = &Community{Karma: 40} // signals, but never posted
	if comp(t, DefaultModel.Compute(in), ComponentCommunity).Scored {
		t.Fatal("community scored before a first post")
	}
}

// The gate: requirements with progress; an open integrity flag hides the score.
func TestGateAndReview(t *testing.T) {
	young := Input{Now: now, Started: now.AddDate(0, 0, -3), Days: series(5, 0.01)}
	r := DefaultModel.Compute(young)
	if r.Status != StatusUnrated || r.Score != nil || r.Tier != nil {
		t.Fatalf("young account %+v", r)
	}
	want := map[string]string{RequirementAge: "3/14", RequirementActiveDays: "5/14", RequirementActivity: "5/20", RequirementVolume: "500/500"}
	for _, q := range r.Requirements {
		if want[q.Key] != q.Progress {
			t.Fatalf("requirement %s progress %q, want %q", q.Key, q.Progress, want[q.Key])
		}
		if q.Key == RequirementVolume && !q.Met || q.Key != RequirementVolume && q.Met {
			t.Fatalf("requirement %+v", q)
		}
	}
	if len(r.Components) != 5 {
		t.Fatal("unrated people still see their components")
	}
	// a model with a higher bar (REPUTATION_MIN_VOLUME)
	m := DefaultModel.clone()
	m.MinVolume = dd("501")
	for _, q := range m.Compute(young).Requirements {
		if q.Key == RequirementVolume && (q.Met || q.Progress != "500/501") {
			t.Fatalf("volume requirement %+v", q)
		}
	}
	good := Input{Now: now, Started: now.AddDate(0, 0, -200), Days: series(30, 0.01, -0.005), UnderReview: true}
	rv := DefaultModel.Compute(good)
	if rv.Status != StatusUnderReview || rv.Score != nil || rv.Tier != nil || len(rv.Components) != 5 {
		t.Fatalf("under review %+v", rv)
	}
	empty := DefaultModel.Compute(Input{Now: now})
	if empty.Status != StatusUnrated || len(empty.Stats.Equity) != 0 || comp(t, empty, ComponentRisk).Scored {
		t.Fatalf("no history %+v", empty)
	}
}

// Flows are excluded: a deposit after a blow-up doesn't erase the drawdown,
// and the return stays −90%.
func TestFlowsExcluded(t *testing.T) {
	d0 := dayStart(now).AddDate(0, 0, -2)
	at := func(day int, hour int) time.Time { return d0.AddDate(0, 0, day).Add(time.Duration(hour) * time.Hour) }
	snaps := []Snapshot{
		{At: at(0, 1), Value: dd("1000"), NetFlows: dd("1000"), Exposed: true},
		{At: at(0, 23), Value: dd("100"), NetFlows: dd("1000"), Activity: 3, Volume: dd("3000")}, // −90%
		{At: at(1, 1), Value: dd("100"), NetFlows: dd("1000"), Activity: 3, Volume: dd("3000")},
		{At: at(1, 23), Value: dd("1000"), NetFlows: dd("1900"), Activity: 3, Volume: dd("3000")}, // 900 deposited
		{At: at(2, 12), Value: dd("1000"), NetFlows: dd("1900"), Activity: 3, Volume: dd("3000")},
	}
	days := RollupDays(snaps, d0, d0.AddDate(0, 0, 2), DefaultModel.ReturnFloor)
	if len(days) != 3 {
		t.Fatalf("days %+v", days)
	}
	near(t, "day 0", days[0].Ret, -0.9, 1e-9)
	near(t, "day 1 (deposit excluded)", days[1].Ret, 0, 1e-9)
	if !days[1].Flows.Equal(dd("900")) || !days[0].Active || days[1].Active || days[0].Activity != 3 || !days[0].Volume.Equal(dd("3000")) {
		t.Fatalf("rollup %+v", days)
	}
	near(t, "max drawdown", MaxDrawdown(days), 0.9, 1e-9)
	r := DefaultModel.Compute(Input{Now: now, Started: now.AddDate(0, 0, -100), Days: days})
	if r.Stats.Return90dPct != "-90.00" || r.Stats.MaxDrawdown90dPct != "90.00" || comp(t, r, ComponentRisk).Value != 0 {
		t.Fatalf("after the deposit: return %s, mdd %s, risk %v", r.Stats.Return90dPct, r.Stats.MaxDrawdown90dPct,
			comp(t, r, ComponentRisk).Value)
	}
	// the naive value curve would show no loss at all over the two days
	if !days[1].Close.Equal(days[0].Open) {
		t.Fatal("fixture: value is back where it started")
	}
}

// Counters are differences: a recount that goes down counts as 0, a day
// without snapshots has no row, and exposure alone makes a day active.
func TestRollupCounters(t *testing.T) {
	d0 := dayStart(now).AddDate(0, 0, -3)
	at := func(day int, hour int) time.Time { return d0.AddDate(0, 0, day).Add(time.Duration(hour) * time.Hour) }
	snaps := []Snapshot{
		{At: at(0, 10), Value: dd("50"), NetFlows: dd("50"), Activity: 10, Volume: dd("800"), Incidents: 1},
		{At: at(0, 20), Value: dd("60"), NetFlows: dd("50"), Activity: 12, Volume: dd("900"), Incidents: 2},
		// day 1: no snapshot
		{At: at(2, 9), Value: dd("61"), NetFlows: dd("50"), Activity: 11, Volume: dd("850"), Incidents: 2, Exposed: true},
	}
	days := RollupDays(snaps, d0, d0.AddDate(0, 0, 3), DefaultModel.ReturnFloor)
	if len(days) != 2 || !days[1].Day.Equal(d0.AddDate(0, 0, 2)) {
		t.Fatalf("days %+v", days)
	}
	// first day opens at its first snapshot; returns divide by max(open, 100)
	if days[0].Activity != 2 || !days[0].Volume.Equal(dd("100")) || days[0].Incidents != 1 {
		t.Fatalf("day 0 %+v", days[0])
	}
	near(t, "day 0 ret", days[0].Ret, 10.0/100, 1e-9)
	if days[1].Activity != 0 || !days[1].Volume.IsZero() || !days[1].Active || !days[1].Open.Equal(dd("60")) {
		t.Fatalf("day 2 %+v", days[1])
	}
}

func TestWashPairDetector(t *testing.T) {
	a, b, c, d, e, f := uid("a"), uid("b"), uid("c"), uid("d"), uid("e"), uid("f")
	vol := map[uuid.UUID]decimal.Decimal{a: dd("10000"), b: dd("50000"), c: dd("100000"), d: dd("100000"), e: dd("100000"), f: dd("100000")}
	pairs := []PairStat{
		{A: a, B: b, Trades: 12, Volume: dd("4000")},                                      // 40% of a's volume
		{A: c, B: d, Trades: 9, Volume: dd("90000")},                                      // too few trades
		{A: e, B: f, Trades: 15, Volume: dd("1000"), GainA: dd("500"), GainB: dd("-480")}, // mirrored gains
		{A: c, B: e, Trades: 30, Volume: dd("2000"), GainA: dd("500"), GainB: dd("-100")}, // neither
	}
	flags := DetectWashPairs(pairs, vol)
	if len(flags) != 4 {
		t.Fatalf("flags %+v", flags)
	}
	by := map[uuid.UUID]IntegrityFlag{}
	for _, x := range flags {
		by[x.UserID] = x
		if x.Kind != FlagWashPair {
			t.Fatal("kind")
		}
	}
	if by[a].CounterpartyID != b || by[b].CounterpartyID != a || by[a].Metrics["share_of_user_pct"] != "40.0" ||
		by[b].Metrics["share_of_user_pct"] != "8.0" || by[a].Metrics["trades_30d"] != "12" {
		t.Fatalf("a/b %+v %+v", by[a], by[b])
	}
	if by[e].Metrics["mirrored"] != "true" || by[f].Metrics["gain_user"] != "-480.00" || by[f].Metrics["gain_counterparty"] != "500.00" {
		t.Fatalf("e/f %+v", by[f])
	}
	if _, ok := by[c]; ok {
		t.Fatal("c flagged")
	}
}

func TestVoteRingDetector(t *testing.T) {
	var votes []Vote
	add := func(voter, author string, n int) {
		for i := 0; i < n; i++ {
			votes = append(votes, Vote{VoterID: uid(voter), AuthorID: uid(author)})
		}
	}
	add("ring", "friend", 15) // 15 of 25 to one author
	add("ring", "x", 5)
	add("ring", "y", 5)
	add("few", "friend", 19) // under 20 votes
	add("spread", "p", 9)    // 30 votes, top share 40%
	add("spread", "q", 9)
	add("spread", "r", 12)
	add("self", "self", 40) // self-votes ignored
	flags := DetectVoteRings(votes)
	if len(flags) != 1 || flags[0].UserID != uid("ring") || flags[0].CounterpartyID != uid("friend") || flags[0].Kind != FlagVoteRing ||
		flags[0].Metrics["share_pct"] != "60.0" || flags[0].Metrics["upvotes_30d"] != "25" {
		t.Fatalf("flags %+v", flags)
	}
}

func TestFlagKinds(t *testing.T) {
	for k, ok := range map[string]bool{"wash_pair": true, "copy_front_run": true, "": false, "Wash": false, "a-b": false,
		strings.Repeat("a", 41): false} {
		if validKind(k) != ok {
			t.Fatalf("validKind(%q) = %v", k, !ok)
		}
	}
}

// Invalid settings are boot problems, not silent defaults.
func TestConfigProblems(t *testing.T) {
	t.Setenv("REPUTATION_RETURN_FLOOR", "0")
	t.Setenv("REPUTATION_MIN_VOLUME", "lots")
	t.Setenv("REPUTATION_SNAPSHOT_INTERVAL", "10s")
	s := New(&app.Deps{})
	err := config.Check()
	for _, name := range []string{"REPUTATION_RETURN_FLOOR", "REPUTATION_MIN_VOLUME", "REPUTATION_SNAPSHOT_INTERVAL"} {
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Fatalf("no problem for %s: %v", name, err)
		}
	}
	if !s.model.ReturnFloor.Equal(dd("100")) || !s.model.MinVolume.Equal(dd("500")) || s.interval != 15*time.Minute {
		t.Fatalf("defaults not kept: %+v %s", s.model, s.interval)
	}
}
