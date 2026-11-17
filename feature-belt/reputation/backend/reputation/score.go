package reputation

import (
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/shopspring/decimal"
)

// rep-v1 scoring: pure functions over one person's day series, so the formula
// is golden-tested and the explainer shows the same numbers the score is
// made of.

// Version names the scoring model (Reputation.version). Change it with the
// formula constants below.
const Version = "rep-v1"

// Statuses. Only rated people have a score.
const (
	StatusRated       = "rated"
	StatusUnrated     = "unrated"      // requirements not met yet (Reputation.requirements says which)
	StatusUnderReview = "under_review" // an open integrity flag: score hidden until staff decide
)

// Component keys.
const (
	ComponentPerformance = "performance"
	ComponentRisk        = "risk"
	ComponentConsistency = "consistency"
	ComponentCommunity   = "community"
	ComponentTenure      = "tenure"
)

// Requirement keys.
const (
	RequirementAge        = "account_age"
	RequirementActiveDays = "active_days"
	RequirementActivity   = "activity"
	RequirementVolume     = "volume"
)

// Formula constants of rep-v1. Changing one is a new Version.
const (
	Window           = 90 // days the score looks back
	ShortWindow      = 30 // the recent window blended into performance
	Shrinkage        = 20.0
	minStd           = 0.002 // std floor in the Sharpe-like ratio
	mddScale         = 0.6   // a 60% drawdown scores 0 on risk
	incidentPenalty  = 10.0  // risk points per incident in the window
	strikePenalty    = 15.0  // community points per strike in the window
	consistencyWks   = 13
	minActiveWeeks   = 4
	tenureFullDays   = 180 // tenure: 70 points at 180 days old
	tenureFullActive = 60  // tenure: 30 points at 60 active days
)

// Weight is one component's share of the score in points. A component
// without data is unscored and its points go to the others: each scored
// component's effective weight is its points over the scored total.
type Weight struct {
	Key, Label string
	Points     float64
}

// Tier names a score band; a score gets the first tier (highest Min first)
// it reaches.
type Tier struct {
	Key string
	Min int
}

// Model is what an app tunes: weights, tiers and the rating requirements.
// DefaultModel is rep-v1 as published; change it from an init() in
// stitching code (New copies it, then applies REPUTATION_MIN_VOLUME and
// REPUTATION_RETURN_FLOOR).
type Model struct {
	Weights       []Weight
	Tiers         []Tier
	MinAgeDays    int             // days since first activity
	MinActiveDays int             // active days in the 90-day window
	MinActivity   int             // activity count in the window
	MinVolume     decimal.Decimal // volume in the window
	// ReturnFloor: a day's return divides by max(open, ReturnFloor), so a
	// nearly empty account can't post huge percentages.
	ReturnFloor decimal.Decimal
	// Labels are the requirement labels people read, and "incidents", the
	// plural word for incidents in explanations ("liquidations").
	Labels map[string]string
}

// DefaultModel is rep-v1.
var DefaultModel = Model{
	Weights: []Weight{
		{ComponentPerformance, "Performance", 40},
		{ComponentRisk, "Risk", 20},
		{ComponentConsistency, "Consistency", 15},
		{ComponentCommunity, "Community", 10},
		{ComponentTenure, "Tenure", 5},
	},
	Tiers:         []Tier{{"elite", 75}, {"strong", 60}, {"steady", 40}, {"building", 0}},
	MinAgeDays:    14,
	MinActiveDays: 14,
	MinActivity:   20,
	MinVolume:     decimal.NewFromInt(500),
	ReturnFloor:   decimal.NewFromInt(100),
	Labels: map[string]string{
		RequirementAge:        "Days since first activity",
		RequirementActiveDays: "Active days in 90",
		RequirementActivity:   "Activity in 90 days",
		RequirementVolume:     "Volume in 90 days",
		"incidents":           "incidents",
	},
}

func (m Model) clone() Model {
	out := m
	out.Weights = append([]Weight(nil), m.Weights...)
	out.Tiers = append([]Tier(nil), m.Tiers...)
	out.Labels = make(map[string]string, len(m.Labels))
	for k, v := range m.Labels {
		out.Labels[k] = v
	}
	return out
}

func (m Model) label(key string) string {
	if l := m.Labels[key]; l != "" {
		return l
	}
	if l := DefaultModel.Labels[key]; l != "" {
		return l
	}
	return key
}

// TierOf maps a score to its tier.
func (m Model) TierOf(score int) string {
	for _, t := range m.Tiers {
		if score >= t.Min {
			return t.Key
		}
	}
	if len(m.Tiers) > 0 {
		return m.Tiers[len(m.Tiers)-1].Key
	}
	return ""
}

// Day is one day of a person's series: value at the open and close of the
// UTC day, the flows in between (money put in minus taken out: never a
// return), and that day's activity.
type Day struct {
	Day         time.Time // UTC midnight
	Open, Close decimal.Decimal
	Flows       decimal.Decimal
	Ret         float64 // (close − open − flows) / max(open, floor); set by the loaders
	Active      bool    // activity that day, or open exposure in a snapshot
	Activity    int
	Volume      decimal.Decimal
	Incidents   int
	Source      string // SourceSnapshots | SourcePushed
}

// Day sources.
const (
	SourceSnapshots = "snapshots" // rolled up from the Source's snapshots
	SourcePushed    = "pushed"    // RecordDay (never overwritten by the rollup)
)

// Community is one person's community standing over the 90-day window
// (SetCommunitySignals).
type Community struct {
	Karma   int  // net votes received
	Strikes int  // moderation strikes
	Posted  bool // has posted at all: the component is unscored until then
}

// Input is everything one person's score is computed from.
type Input struct {
	Now         time.Time
	Started     time.Time  // first activity (else account creation)
	Days        []Day      // ascending; only the last Window days count
	Community   *Community // nil: the app has no community signals
	UnderReview bool       // an open (or recently confirmed) integrity flag
}

// Component is one part of the score as the explainer shows it.
type Component struct {
	Key     string            `json:"key"`
	Label   string            `json:"label"`
	Weight  string            `json:"weight"` // effective weight after redistribution, "0.50"
	Value   float64           `json:"value"`  // 0–100, one decimal
	Points  float64           `json:"points"` // value × effective weight, one decimal
	Scored  bool              `json:"scored"`
	Detail  string            `json:"detail"`
	Metrics map[string]string `json:"metrics"` // raw inputs, e.g. {"sharpe_90d": "0.328"}
}

// Requirement is one rating requirement with progress.
type Requirement struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Met      bool   `json:"met"`
	Progress string `json:"progress"` // "9/14"
}

// EquityPoint is one day of the time-weighted index (100 at the start of the window).
type EquityPoint struct {
	Day   string `json:"day"` // YYYY-MM-DD
	Index string `json:"index"`
}

// Stats are the person's numbers: time-weighted, with flows excluded.
type Stats struct {
	Return30dPct      string        `json:"return_30d_pct"`
	Return90dPct      string        `json:"return_90d_pct"`
	Gain30d           string        `json:"gain_30d"` // value change minus flows
	MaxDrawdown90dPct string        `json:"max_drawdown_90d_pct"`
	Volume30d         string        `json:"volume_30d"`
	Activity30d       int           `json:"activity_30d"`
	ActiveDays90d     int           `json:"active_days_90d"`
	Incidents90d      int           `json:"incidents_90d"`
	Equity            []EquityPoint `json:"equity"` // last 90 days
}

// Reputation is a computed score with everything it is made of.
type Reputation struct {
	UserID       string        `json:"user_id"`
	Score        *int          `json:"score"`
	Status       string        `json:"status"`
	Tier         *string       `json:"tier"`
	Version      string        `json:"version"`
	ComputedAt   *time.Time    `json:"computed_at"`
	Components   []Component   `json:"components"`
	Requirements []Requirement `json:"requirements"`
	Stats        Stats         `json:"stats"`
}

func dayStart(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// window returns the days within the last n days (today included).
func window(days []Day, now time.Time, n int) []Day {
	from := dayStart(now).AddDate(0, 0, -(n - 1))
	i := 0
	for i < len(days) && days[i].Day.Before(from) {
		i++
	}
	return days[i:]
}

func activeRets(days []Day) []float64 {
	var out []float64
	for _, d := range days {
		if d.Active {
			out = append(out, d.Ret)
		}
	}
	return out
}

func meanStd(xs []float64) (mean, std float64) {
	if len(xs) == 0 {
		return 0, 0
	}
	for _, x := range xs {
		mean += x
	}
	mean /= float64(len(xs))
	if len(xs) < 2 {
		return mean, 0
	}
	var ss float64
	for _, x := range xs {
		ss += (x - mean) * (x - mean)
	}
	return mean, math.Sqrt(ss / float64(len(xs)-1))
}

// Sharpe is the Sharpe-like S = mean(r)/max(std(r), 0.002) over active-day
// returns and its shrunk S* = S·n/(n+20), so few days count for less.
func Sharpe(rets []float64) (s, shrunk float64) {
	if len(rets) < 2 {
		return 0, 0
	}
	m, sd := meanStd(rets)
	s = m / math.Max(sd, minStd)
	n := float64(len(rets))
	return s, s * n / (n + Shrinkage)
}

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }

// MaxDrawdown is the largest peak-to-trough fall of the time-weighted index
// Π(1+r): flows never enter it, so a deposit can't erase a loss.
func MaxDrawdown(days []Day) float64 {
	idx, peak, mdd := 1.0, 1.0, 0.0
	for _, d := range days {
		idx *= 1 + d.Ret
		if idx < 0 {
			idx = 0
		}
		peak = math.Max(peak, idx)
		if peak > 0 {
			mdd = math.Max(mdd, (peak-idx)/peak)
		}
	}
	return mdd
}

// Compound is Π(1+r) − 1.
func Compound(days []Day) float64 {
	idx := 1.0
	for _, d := range days {
		idx *= 1 + d.Ret
	}
	return idx - 1
}

// DayReturn is (close − open − flows) / max(open, floor).
func DayReturn(open, close, flows, floor decimal.Decimal) float64 {
	r, _ := close.Sub(open).Sub(flows).Div(decimal.Max(open, floor)).Float64()
	return r
}

func f1(v float64) string { return strconv.FormatFloat(v, 'f', 1, 64) }
func f2(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) }
func f3(v float64) string { return strconv.FormatFloat(v, 'f', 3, 64) }
func round1(v float64) float64 {
	return math.Round(v*10) / 10
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// Compute scores one person. Every component is computed for the explainer;
// the score shows only when every requirement is met and no integrity flag
// is open.
func (m Model) Compute(in Input) Reputation {
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	at := now.UTC()
	rep := Reputation{Version: Version, ComputedAt: &at, Components: []Component{}, Requirements: []Requirement{}}
	d90 := window(in.Days, now, Window)
	d30 := window(in.Days, now, ShortWindow)
	ageDays := 0.0
	if !in.Started.IsZero() {
		ageDays = math.Max(0, now.Sub(in.Started).Hours()/24)
	}
	active90 := len(activeRets(d90))
	activity90, incidents90 := 0, 0
	vol90 := decimal.Zero
	for _, d := range d90 {
		activity90 += d.Activity
		incidents90 += d.Incidents
		vol90 = vol90.Add(d.Volume)
	}

	byKey := map[string]Component{
		ComponentPerformance: performance(d30, d90),
		ComponentRisk:        risk(d90, incidents90, m.label("incidents")),
		ComponentConsistency: consistency(d90, now),
		ComponentCommunity:   community(in.Community),
		ComponentTenure:      tenure(ageDays, active90),
	}
	comps := make([]Component, 0, len(m.Weights))
	total := 0.0
	for _, w := range m.Weights {
		c, ok := byKey[w.Key]
		if !ok {
			continue
		}
		c.Label = w.Label
		if c.Scored {
			total += w.Points
		}
		comps = append(comps, c)
	}
	sum := 0.0
	for i := range comps {
		w := 0.0
		if comps[i].Scored && total > 0 {
			w = m.Weights[weightIndex(m.Weights, comps[i].Key)].Points / total
		}
		comps[i].Weight = f2(w)
		comps[i].Value = round1(comps[i].Value)
		comps[i].Points = round1(w * comps[i].Value)
		sum += w * comps[i].Value
	}
	rep.Components = comps

	rep.Requirements = []Requirement{
		{Key: RequirementAge, Label: m.label(RequirementAge), Met: ageDays >= float64(m.MinAgeDays),
			Progress: fmt.Sprintf("%d/%d", int(ageDays), m.MinAgeDays)},
		{Key: RequirementActiveDays, Label: m.label(RequirementActiveDays), Met: active90 >= m.MinActiveDays,
			Progress: fmt.Sprintf("%d/%d", active90, m.MinActiveDays)},
		{Key: RequirementActivity, Label: m.label(RequirementActivity), Met: activity90 >= m.MinActivity,
			Progress: fmt.Sprintf("%d/%d", activity90, m.MinActivity)},
		{Key: RequirementVolume, Label: m.label(RequirementVolume), Met: vol90.GreaterThanOrEqual(m.MinVolume),
			Progress: vol90.Round(0).String() + "/" + m.MinVolume.String()},
	}
	gate := true
	for _, r := range rep.Requirements {
		gate = gate && r.Met
	}
	switch {
	case in.UnderReview:
		rep.Status = StatusUnderReview
	case !gate:
		rep.Status = StatusUnrated
	default:
		score := int(math.Round(sum))
		tier := m.TierOf(score)
		rep.Status, rep.Score, rep.Tier = StatusRated, &score, &tier
	}
	rep.Stats = stats(d30, d90, active90, incidents90)
	return rep
}

func weightIndex(ws []Weight, key string) int {
	for i, w := range ws {
		if w.Key == key {
			return i
		}
	}
	return -1
}

func performance(d30, d90 []Day) Component {
	r30, r90 := activeRets(d30), activeRets(d90)
	s30, k30 := Sharpe(r30)
	s90, k90 := Sharpe(r90)
	v30, v90 := clamp(50+250*k30, 0, 100), clamp(50+250*k90, 0, 100)
	c := Component{Key: ComponentPerformance, Scored: len(r90) >= 2, Metrics: map[string]string{
		"sharpe_30d": f3(s30), "sharpe_90d": f3(s90), "shrunk_30d": f3(k30), "shrunk_90d": f3(k90),
		"active_days_30d": strconv.Itoa(len(r30)), "active_days_90d": strconv.Itoa(len(r90)),
	}}
	switch {
	case !c.Scored:
		c.Detail = "Needs at least 2 active days."
	case len(r30) >= 2:
		c.Value = 0.4*v30 + 0.6*v90
		c.Detail = fmt.Sprintf("Sharpe-like ratio %s a day over %d active days, shrunk to %s for the sample size (90 days); 40%% of the value comes from the last 30 days.",
			f2(s90), len(r90), f2(k90))
	default:
		c.Value = v90
		c.Detail = fmt.Sprintf("Sharpe-like ratio %s a day over %d active days, shrunk to %s for the sample size (90 days).", f2(s90), len(r90), f2(k90))
	}
	return c
}

func risk(d90 []Day, incidents90 int, noun string) Component {
	mdd := MaxDrawdown(d90)
	c := Component{Key: ComponentRisk, Scored: len(d90) > 0, Metrics: map[string]string{
		"max_drawdown_90d_pct": f2(mdd * 100), "incidents_90d": strconv.Itoa(incidents90),
	}}
	if !c.Scored {
		c.Detail = "No history yet."
		return c
	}
	c.Value = math.Max(0, clamp(100*(1-mdd/mddScale), 0, 100)-incidentPenalty*float64(incidents90))
	c.Detail = fmt.Sprintf("Largest drawdown %s%% in 90 days (time-weighted, so deposits don't offset losses); %d %s.",
		f1(mdd*100), incidents90, noun)
	return c
}

func consistency(d90 []Day, now time.Time) Component {
	today := dayStart(now)
	type wk struct {
		active bool
		idx    float64
	}
	weeks := make([]wk, consistencyWks)
	for i := range weeks {
		weeks[i].idx = 1
	}
	for _, d := range d90 {
		k := int(today.Sub(d.Day).Hours() / 24 / 7)
		if k < 0 || k >= consistencyWks {
			continue
		}
		weeks[k].idx *= 1 + d.Ret
		weeks[k].active = weeks[k].active || d.Active
	}
	active, pos := 0, 0
	for _, w := range weeks {
		if w.active {
			active++
			if w.idx > 1 {
				pos++
			}
		}
	}
	c := Component{Key: ComponentConsistency, Scored: active >= minActiveWeeks, Metrics: map[string]string{
		"active_weeks_13w": strconv.Itoa(active), "positive_weeks_13w": strconv.Itoa(pos),
	}}
	if !c.Scored {
		c.Detail = fmt.Sprintf("Needs %d active weeks in the last 13 (%d so far).", minActiveWeeks, active)
		return c
	}
	c.Value = float64(pos+2) / float64(active+4) * 100
	c.Detail = fmt.Sprintf("%d of %d active weeks positive in the last 13 weeks.", pos, active)
	return c
}

func community(sig *Community) Component {
	c := Component{Key: ComponentCommunity, Metrics: map[string]string{}}
	if sig == nil {
		c.Detail = "Not part of the score in this app."
		return c
	}
	c.Metrics["karma_90d"], c.Metrics["strikes_90d"] = strconv.Itoa(sig.Karma), strconv.Itoa(sig.Strikes)
	if !sig.Posted {
		c.Detail = "Scored after a first post."
		return c
	}
	c.Scored = true
	v := math.Min(100, 50+10*math.Log2(1+float64(max(0, sig.Karma))/10)) - strikePenalty*float64(sig.Strikes)
	c.Value = math.Max(0, v)
	c.Detail = fmt.Sprintf("Karma %d and %s in 90 days.", sig.Karma, plural(sig.Strikes, "strike", "strikes"))
	return c
}

func tenure(ageDays float64, active90 int) Component {
	v := math.Min(100, 70*ageDays/tenureFullDays+30*float64(active90)/tenureFullActive)
	return Component{Key: ComponentTenure, Scored: true, Value: v,
		Detail:  fmt.Sprintf("Active for %s, with %s in the last 90.", plural(int(ageDays), "day", "days"), plural(active90, "active day", "active days")),
		Metrics: map[string]string{"age_days": strconv.Itoa(int(ageDays)), "active_days_90d": strconv.Itoa(active90)}}
}

func emptyStats() Stats {
	return Stats{Return30dPct: "0.00", Return90dPct: "0.00", Gain30d: "0.00", MaxDrawdown90dPct: "0.00", Volume30d: "0.00",
		Equity: []EquityPoint{}}
}

func stats(d30, d90 []Day, active90, incidents90 int) Stats {
	s := emptyStats()
	s.Return30dPct = f2(Compound(d30) * 100)
	s.Return90dPct = f2(Compound(d90) * 100)
	s.MaxDrawdown90dPct = f2(MaxDrawdown(d90) * 100)
	gain, vol := decimal.Zero, decimal.Zero
	for _, d := range d30 {
		gain = gain.Add(d.Close.Sub(d.Open).Sub(d.Flows))
		vol = vol.Add(d.Volume)
		s.Activity30d += d.Activity
	}
	s.Gain30d = gain.StringFixed(2)
	s.Volume30d = vol.StringFixed(2)
	s.ActiveDays90d = active90
	s.Incidents90d = incidents90
	idx := 100.0
	for _, d := range d90 {
		idx *= 1 + d.Ret
		s.Equity = append(s.Equity, EquityPoint{Day: d.Day.Format("2006-01-02"), Index: f2(math.Max(0, idx))})
	}
	return s
}
