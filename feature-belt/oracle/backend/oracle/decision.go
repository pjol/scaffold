package oracle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DecisionModel is an optional second opinion on every live print after the
// statistical screen. It can only raise risk: the score becomes
// max(stat, Manipulation), and Include < 0.35 rejects the print. Any error,
// timeout or pause fails open to the stat-v1 verdict. The state it sees is
// built from the screen's own features in words; no text a source supplied
// ever reaches it.
type DecisionModel interface {
	Name() string
	Decide(ctx context.Context, state map[string]string) (Decision, error)
}

// Decision is a model's answer for one print.
type Decision struct {
	Manipulation float64 // P(the print was made to move the price), 0..1
	Include      float64 // P(the print belongs in the price), 0..1
	Model        string  // the model (snapshot) that answered, recorded on the observation
	CostUSD      float64
}

// decisionExcludeBelow: a print the model says not to include (P < 0.35) is
// rejected whatever its score.
const decisionExcludeBelow = 0.35

var (
	decisionMu     sync.Mutex
	decisionCustom DecisionModel
)

// UseDecisionModel installs a decision model in place of the HTTP one
// configured by ORACLE_DECISION_URL (call from an init()).
func UseDecisionModel(m DecisionModel) {
	decisionMu.Lock()
	defer decisionMu.Unlock()
	decisionCustom = m
}

// combineDecision applies stat-as-floor: the model can raise risk, never
// lower it, and a confident "don't include" excludes the print.
func combineDecision(stat float64, a Decision) (score float64, verdict string, factor float64, reason string) {
	score = math.Max(stat, clamp01(a.Manipulation))
	verdict, factor = verdictFor(score)
	if clamp01(a.Include) < decisionExcludeBelow {
		verdict, factor = VerdictRejected, 0
	}
	action := "include"
	if verdict == VerdictRejected {
		action = "exclude"
	}
	return score, verdict, factor, fmt.Sprintf("model: %.2f manipulative, %s", clamp01(a.Manipulation), action)
}

func clamp01(x float64) float64 {
	if math.IsNaN(x) {
		return 0
	}
	return math.Max(0, math.Min(1, x))
}

// ---- the HTTP decision model ----

// httpDecision speaks the decisions wire format (a state of short strings and
// yes/no questions answered with a probability, "noul"): the format of
// OpenRouter's decisions endpoint (/api/alpha/decisions) for decision models
// such as TypeSafe's Jev. Point ORACLE_DECISION_URL at any service that
// answers it.
type httpDecision struct {
	url, key, model string
	timeout         time.Duration
	client          *http.Client
	gap             time.Duration

	rateMu sync.Mutex
	nextAt time.Time

	mu          sync.Mutex
	disabled    string    // why it was turned off (bad key, no credit)
	pausedUntil time.Time // 429 / 529 Retry-After
	lastWarn    time.Time
	lastErr     string
	calls       int64
	failures    int64
	costUSD     float64
	warn        func(msg string, args ...any)
}

const (
	qManipulative = "manipulative"
	qInclude      = "include"
)

// Questions are literal and positive, with criteria that extend the
// instruction (decision models read them literally).
var decisionQuestions = map[string]any{
	qManipulative: map[string]any{
		"type":         "noul",
		"instructions": "Was this price print made to manipulate the published price?",
		"criteria": map[string]string{
			"true":  "The print looks engineered to move the price: for example a wash trade, one of a run of rising or falling prints in a short time, a different or mislabelled item reported as this one, or a price far from every other venue with no market-wide reason.",
			"false": "The print looks like ordinary trading: in line with recent prices and other venues, or a single plausible move.",
		},
	},
	qInclude: map[string]any{
		"type":         "noul",
		"instructions": "Should this print be included in the published price?",
		"criteria": map[string]string{
			"true":  "The print is a trustworthy signal of the item's market price.",
			"false": "The print should be left out of the price because it looks manipulative, mislabelled, erroneous or far out of line with the evidence.",
		},
	},
}

func newHTTPDecision(url, key, model string, maxRPS int, timeout time.Duration) *httpDecision {
	if url == "" || key == "" {
		return nil
	}
	return &httpDecision{
		url: url, key: key, model: model, timeout: timeout,
		client: &http.Client{Timeout: timeout + time.Second},
		gap:    time.Second / time.Duration(max(1, maxRPS)),
	}
}

func (h *httpDecision) Name() string {
	if h.model != "" {
		return h.model
	}
	return "decision-model"
}

func (h *httpDecision) usable(now time.Time) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.disabled == "" && !now.Before(h.pausedUntil)
}

func (h *httpDecision) waitRate(ctx context.Context) error {
	h.rateMu.Lock()
	now := time.Now()
	at := h.nextAt
	if at.Before(now) {
		at = now
	}
	h.nextAt = at.Add(h.gap)
	h.rateMu.Unlock()
	if d := time.Until(at); d > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(d):
		}
	}
	return nil
}

var errDecisionUnavailable = errors.New("decision model unavailable")

func (h *httpDecision) fail(err error) error {
	h.mu.Lock()
	h.failures++
	h.lastErr = truncate(err.Error(), 200)
	warn := h.warn != nil && time.Since(h.lastWarn) > 10*time.Minute
	if warn {
		h.lastWarn = time.Now()
	}
	h.mu.Unlock()
	if warn {
		h.warn("oracle: decision model failed; the statistical verdict stands", "err", err)
	}
	return err
}

func (h *httpDecision) Decide(ctx context.Context, state map[string]string) (Decision, error) {
	if !h.usable(time.Now()) {
		return Decision{}, errDecisionUnavailable
	}
	if err := h.waitRate(ctx); err != nil {
		return Decision{}, err
	}
	body := map[string]any{"state": state, "questions": decisionQuestions}
	if h.model != "" {
		body["model"] = h.model
	}
	buf, _ := json.Marshal(body)
	ctx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.url, bytes.NewReader(buf))
	if err != nil {
		return Decision{}, err
	}
	req.Header.Set("Authorization", "Bearer "+h.key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := h.client.Do(req)
	if err != nil {
		return Decision{}, h.fail(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusPaymentRequired || resp.StatusCode == http.StatusForbidden:
		why := "ORACLE_DECISION_API_KEY was refused"
		if resp.StatusCode == http.StatusPaymentRequired {
			why = "the decision service is out of credit"
		}
		h.mu.Lock()
		first := h.disabled == ""
		h.disabled = why
		h.mu.Unlock()
		if first && h.warn != nil {
			h.warn("oracle: decision model turned off until restart; statistical screen only", "reason", why, "status", resp.StatusCode)
		}
		return Decision{}, fmt.Errorf("decision model: HTTP %d", resp.StatusCode)
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == 529:
		ra := 5 * time.Second
		if v, err := strconv.Atoi(strings.TrimSpace(resp.Header.Get("Retry-After"))); err == nil && v > 0 {
			ra = time.Duration(v) * time.Second
		}
		h.mu.Lock()
		h.pausedUntil = time.Now().Add(ra)
		h.mu.Unlock()
		return Decision{}, h.fail(fmt.Errorf("decision model: HTTP %d, pausing %s", resp.StatusCode, ra))
	case resp.StatusCode != http.StatusOK:
		return Decision{}, h.fail(fmt.Errorf("decision model: HTTP %d: %s", resp.StatusCode, truncate(string(raw), 200)))
	}
	var out struct {
		Model   string `json:"model"`
		Answers map[string]struct {
			Noul *float64 `json:"noul"`
		} `json:"answers"`
		Usage struct {
			Cost float64 `json:"cost"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return Decision{}, h.fail(fmt.Errorf("decision model: decode: %w", err))
	}
	m, inc := out.Answers[qManipulative].Noul, out.Answers[qInclude].Noul
	if m == nil || inc == nil {
		return Decision{}, h.fail(errors.New("decision model: missing answers"))
	}
	d := Decision{Manipulation: clamp01(*m), Include: clamp01(*inc), Model: out.Model, CostUSD: out.Usage.Cost}
	if d.Model == "" {
		d.Model = h.Name()
	}
	h.mu.Lock()
	h.calls++
	h.costUSD += out.Usage.Cost
	h.mu.Unlock()
	return d, nil
}

// DecisionStatus is the decision model's state for the admin screen.
type DecisionStatus struct {
	Enabled  bool    `json:"enabled"`
	Name     string  `json:"name,omitempty"`
	Note     string  `json:"note,omitempty"`
	Calls    int64   `json:"calls"`
	Failures int64   `json:"failures"`
	CostUSD  float64 `json:"cost_usd"`
	Queued   int     `json:"queued"`
}

func (h *httpDecision) status() DecisionStatus {
	h.mu.Lock()
	defer h.mu.Unlock()
	st := DecisionStatus{Enabled: h.disabled == "", Name: h.Name(), Calls: h.calls, Failures: h.failures, CostUSD: math.Round(h.costUSD*1e6) / 1e6}
	switch {
	case h.disabled != "":
		st.Note = h.disabled
	case time.Now().Before(h.pausedUntil):
		st.Note = "paused after a rate limit"
	case h.lastErr != "":
		st.Note = "last error: " + h.lastErr
	}
	return st
}

// ---- state: the screen's own features, in words ----

var kindWords = map[string]string{
	KindSale:    "a single completed sale",
	KindMarket:  "an aggregate market price over many sales",
	KindListing: "an asking price on a listing, not a completed sale",
}

func pctWords(p float64) string {
	p = math.Round(100*p/5) * 5
	if p < 5 {
		return "less than 5%"
	}
	return fmt.Sprintf("about %.0f%%", p)
}

func ordinal(n int) string {
	suf := "th"
	switch {
	case n%100 >= 11 && n%100 <= 13:
	case n%10 == 1:
		suf = "st"
	case n%10 == 2:
		suf = "nd"
	case n%10 == 3:
		suf = "rd"
	}
	return strconv.Itoa(n) + suf
}

func windowWords(d time.Duration) string {
	if d <= time.Hour {
		return "within the last hour"
	}
	return fmt.Sprintf("within the last %d hours", int(d.Hours()))
}

// decisionState describes one print using only the feed's own name and
// category (staff-entered) and the screen's features, bucketed into words.
func decisionState(f *feed, venue, kind string, ft features) map[string]string {
	side := "above"
	if ft.Direction < 0 {
		side = "below"
	}
	st := map[string]string{
		"item":  f.name,
		"venue": venue,
		"print": kindWords[kind],
	}
	if f.category != "" {
		st["category"] = f.category
	}
	switch z := ft.RobustZ; {
	case ft.RefN < 3:
		st["price_vs_recent_prices"] = "not enough recent prices to compare"
	case z < 1.5:
		st["price_vs_recent_prices"] = "in line with recent prices"
	case z < 3:
		st["price_vs_recent_prices"] = "somewhat " + side + " recent prices"
	case z < 5:
		st["price_vs_recent_prices"] = fmt.Sprintf("well %s recent prices (about %.0f robust deviations)", side, z)
	default:
		st["price_vs_recent_prices"] = fmt.Sprintf("far %s recent prices (about %.0f robust deviations)", side, math.Min(z, 99))
	}
	ref := "the current published price"
	if !f.live {
		ref = "the seed price (no live price yet)"
	}
	switch {
	case f.price <= 0 && !f.live:
		st["price_vs_published"] = "no published price yet"
	case ft.DevOracle < 0.03:
		st["price_vs_published"] = "within 3% of " + ref
	default:
		st["price_vs_published"] = fmt.Sprintf("%s %s %s", pctWords(ft.DevOracle), side, ref)
	}
	switch {
	case ft.OtherSrcN == 0:
		st["price_vs_other_venues"] = "no other venue to compare"
	case ft.Divergence < 0.05:
		st["price_vs_other_venues"] = "in line with the other venues"
	default:
		st["price_vs_other_venues"] = pctWords(ft.Divergence) + " away from the other venues"
	}
	if kind == KindSale {
		if ft.BurstN >= 2 {
			st["same_direction_prints"] = fmt.Sprintf("%s print in the same direction %s", ordinal(ft.BurstN), windowWords(f.p.BurstWindow))
		} else {
			st["same_direction_prints"] = "none"
		}
		switch {
		case ft.WalkUp && ft.Direction > 0:
			st["steady_walk"] = "yes: each recent print was higher than the one before"
		case ft.WalkUp:
			st["steady_walk"] = "yes: each recent print was lower than the one before"
		default:
			st["steady_walk"] = "no"
		}
		if ft.QtyAnomaly {
			st["quantity"] = "unusually large lot"
		} else {
			st["quantity"] = "normal"
		}
	}
	switch n := ft.RefN; {
	case n == 0:
		st["reference_history"] = "no prior reference prints"
	case n == 1:
		st["reference_history"] = "only 1 prior reference print"
	case n < 5:
		st["reference_history"] = "only a few prior reference prints"
	case n < 20:
		st["reference_history"] = "a moderate number of reference prints"
	default:
		st["reference_history"] = "plenty of reference prints"
	}
	switch {
	case ft.Future:
		st["timing"] = "timestamped in the future"
	case ft.AgeDays*24 > 4*f.p.StaleAfter.Hours():
		st["timing"] = "an old print received late"
	default:
		st["timing"] = "recent"
	}
	return st
}
