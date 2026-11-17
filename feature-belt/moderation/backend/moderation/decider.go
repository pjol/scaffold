package moderation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

// The optional second stage of Screen: a decision model answers yes/no
// questions about a state of short strings (the decisions format: OpenRouter's
// /api/alpha/decisions serving decision models such as TypeSafe's Jev). It
// can only escalate the rules' decision, and every failure leaves the rules'
// decision in place (fail open).

// Question is one yes/no ("noul") question in the decisions format.
type Question struct {
	Type         string            `json:"type"`
	Instructions string            `json:"instructions"`
	Criteria     map[string]string `json:"criteria"`
}

// noul is a yes/no question. Write it literally and positively, with criteria
// that extend the instruction (decision models read them literally).
func noul(instructions, whenTrue, whenFalse string) Question {
	return Question{Type: "noul", Instructions: instructions, Criteria: map[string]string{"true": whenTrue, "false": whenFalse}}
}

// Answers is a decision model's reply.
type Answers struct {
	Model     string             // the snapshot that answered
	P         map[string]float64 // P(yes) per question, clamped to [0, 1]
	CostUSD   decimal.Decimal
	LatencyMs int64
}

// Decider answers every question about one state. An error means "couldn't
// tell": Screen records ModelStatusOf(err) and keeps the rules' decision.
// Return a *DecideError to report why.
type Decider interface {
	Decide(ctx context.Context, state map[string]string, questions map[string]Question) (Answers, error)
}

// DeciderHealth is optional: a Decider that implements it shows its state in
// Admin → Moderation.
type DeciderHealth interface {
	Health() DecisionModelHealth
}

// DeciderResetter is optional: a Decider that turned itself off (a refused
// key, no credit) and can be turned back on from Admin → Moderation.
type DeciderResetter interface {
	Reset()
}

// Model statuses, recorded on every decision (moderation_decisions.model_status).
// Everything but ModelOK leaves the rules' decision in place.
const (
	ModelOK          = "ok"
	ModelOff         = "off"          // no decision model configured
	ModelSkipped     = "skipped"      // the rules already remove the item
	ModelUnavailable = "unavailable"  // turned off after a 401/402/403 until reset or restart
	ModelRateLimited = "rate_limited" // 429/529, a Retry-After pause, or no request slot within the timeout
	ModelTimeout     = "timeout"
	ModelCapReached  = "cap_reached" // the daily cost cap is spent
	ModelError       = "error"       // other HTTP errors, malformed answers
)

// DecideError is a failed decision with its status.
type DecideError struct {
	Status string
	HTTP   int
	Err    error
}

func (e *DecideError) Error() string {
	if e.Err != nil {
		return "decision model " + e.Status + ": " + e.Err.Error()
	}
	return "decision model " + e.Status
}

func (e *DecideError) Unwrap() error { return e.Err }

// ModelStatusOf is err's status: ModelOK for nil, ModelError for untyped
// errors, ModelTimeout for deadlines.
func ModelStatusOf(err error) string {
	if err == nil {
		return ModelOK
	}
	var e *DecideError
	if errors.As(err, &e) {
		return e.Status
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ModelTimeout
	}
	return ModelError
}

func decideFail(status string, code int, err error) error {
	return &DecideError{Status: status, HTTP: code, Err: err}
}

// DecisionModelHealth is the decision model's state for Admin → Moderation.
type DecisionModelHealth struct {
	Configured  bool       `json:"configured"`
	Model       string     `json:"model"`
	Status      string     `json:"status"` // ok | off | unavailable | rate_limited | cap_reached
	Note        string     `json:"note"`
	Calls       int64      `json:"calls"`
	Failures    int64      `json:"failures"`
	CostUSD     string     `json:"cost_usd"`      // since the server started
	DayCostUSD  string     `json:"day_cost_usd"`  // this UTC day
	DailyCapUSD string     `json:"daily_cap_usd"` // "0" = no cap
	PausedUntil *time.Time `json:"paused_until"`
}

var (
	deciderMu sync.RWMutex
	decider   Decider
)

// UseDecider installs the decision model Screen asks, replacing the HTTP one
// configured by MODERATION_DECISION_URL; nil turns the model off. Call it
// from an init() in internal/api (stitching) or a test.
func UseDecider(d Decider) {
	deciderMu.Lock()
	defer deciderMu.Unlock()
	decider = d
}

func currentDecider() Decider {
	deciderMu.RLock()
	defer deciderMu.RUnlock()
	return decider
}

// ---- the HTTP decision model ----

// httpDecider speaks the decisions wire format: POST {model, state,
// questions} → {model, answers: {name: {noul}}, usage: {cost}}. It spaces
// requests to maxRPS (and gives up when no slot frees within the timeout),
// stops for the day at the cost cap, pauses on 429/529 for Retry-After, and
// turns itself off on 401/402/403 until reset or restart.
type httpDecider struct {
	url, key, model string
	timeout         time.Duration
	gap             time.Duration
	dailyCap        decimal.Decimal // zero = no cap
	client          *http.Client
	now             func() time.Time

	rateMu sync.Mutex
	nextAt time.Time

	mu          sync.Mutex
	disabled    string
	pausedUntil time.Time
	day         string
	dayCost     decimal.Decimal
	cost        decimal.Decimal
	calls       int64
	failures    int64
	lastErr     string
	lastWarn    time.Time
}

// newHTTPDecider builds the decisions-format client.
func newHTTPDecider(url, key, model string, maxRPS int, timeout time.Duration, dailyCapUSD decimal.Decimal) *httpDecider {
	if maxRPS <= 0 {
		maxRPS = 5
	}
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	return &httpDecider{url: url, key: key, model: model, timeout: timeout, gap: time.Second / time.Duration(maxRPS),
		dailyCap: dailyCapUSD, client: &http.Client{Timeout: timeout + time.Second}, now: time.Now}
}

func dayOf(t time.Time) string { return t.UTC().Format("2006-01-02") }

// ready is the status a call would fail with right now, or ModelOK.
func (h *httpDecider) ready() string {
	now := h.now()
	h.mu.Lock()
	defer h.mu.Unlock()
	switch {
	case h.disabled != "":
		return ModelUnavailable
	case now.Before(h.pausedUntil):
		return ModelRateLimited
	case h.dailyCap.IsPositive() && h.day == dayOf(now) && h.dayCost.GreaterThanOrEqual(h.dailyCap):
		return ModelCapReached
	}
	return ModelOK
}

// slot reserves the next request slot, or reports false when it would not
// start within the timeout (the item keeps the rules' decision).
func (h *httpDecider) slot(ctx context.Context) bool {
	h.rateMu.Lock()
	now := time.Now()
	at := h.nextAt
	if at.Before(now) {
		at = now
	}
	wait := at.Sub(now)
	if wait > h.timeout/2 {
		h.rateMu.Unlock()
		return false
	}
	h.nextAt = at.Add(h.gap)
	h.rateMu.Unlock()
	if wait <= 0 {
		return true
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func (h *httpDecider) Decide(ctx context.Context, state map[string]string, questions map[string]Question) (Answers, error) {
	if st := h.ready(); st != ModelOK {
		return Answers{}, decideFail(st, 0, nil)
	}
	if !h.slot(ctx) {
		return Answers{}, h.failed(decideFail(ModelRateLimited, 0, errors.New("no request slot within the timeout")))
	}
	body := map[string]any{"state": state, "questions": questions}
	if h.model != "" {
		body["model"] = h.model
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return Answers{}, decideFail(ModelError, 0, err)
	}
	ctx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.url, bytes.NewReader(buf))
	if err != nil {
		return Answers{}, decideFail(ModelError, 0, err)
	}
	req.Header.Set("Authorization", "Bearer "+h.key)
	req.Header.Set("Content-Type", "application/json")
	start := time.Now()
	resp, err := h.client.Do(req)
	if err != nil {
		st := ModelError
		var ne net.Error
		if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
			st = ModelTimeout
		}
		return Answers{}, h.failed(decideFail(st, 0, err))
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch code := resp.StatusCode; {
	case code == http.StatusUnauthorized || code == http.StatusForbidden || code == http.StatusPaymentRequired:
		why := "MODERATION_DECISION_API_KEY was refused"
		if code == http.StatusPaymentRequired {
			why = "the decision service is out of credit"
		}
		h.mu.Lock()
		first := h.disabled == ""
		h.disabled = why
		h.mu.Unlock()
		if first {
			slog.Error("moderation: decision model turned off until reset or restart; rules only", "reason", why, "status", code)
		}
		return Answers{}, h.failed(decideFail(ModelUnavailable, code, fmt.Errorf("HTTP %d", code)))
	case code == http.StatusTooManyRequests || code == 529:
		ra := retryAfter(resp.Header.Get("Retry-After"), h.now())
		h.mu.Lock()
		h.pausedUntil = h.now().Add(ra)
		h.mu.Unlock()
		return Answers{}, h.failed(decideFail(ModelRateLimited, code, fmt.Errorf("HTTP %d, pausing %s", code, ra)))
	case code != http.StatusOK:
		return Answers{}, h.failed(decideFail(ModelError, code, fmt.Errorf("HTTP %d: %s", code, truncate(string(raw), 200))))
	}
	var out struct {
		Model   string `json:"model"`
		Answers map[string]struct {
			Noul *float64 `json:"noul"`
		} `json:"answers"`
		Usage struct {
			Cost json.Number `json:"cost"`
		} `json:"usage"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&out); err != nil {
		return Answers{}, h.failed(decideFail(ModelError, code200, fmt.Errorf("decode: %w", err)))
	}
	a := Answers{Model: out.Model, P: map[string]float64{}, LatencyMs: time.Since(start).Milliseconds()}
	for name := range questions {
		ans, ok := out.Answers[name]
		if !ok || ans.Noul == nil {
			return Answers{}, h.failed(decideFail(ModelError, code200, fmt.Errorf("missing answer %q", name)))
		}
		a.P[name] = clamp01(*ans.Noul)
	}
	if a.Model == "" {
		a.Model = h.model
	}
	if out.Usage.Cost != "" {
		if c, err := decimal.NewFromString(out.Usage.Cost.String()); err == nil && !c.IsNegative() {
			a.CostUSD = c
		}
	}
	now := h.now()
	h.mu.Lock()
	if d := dayOf(now); h.day != d {
		h.day, h.dayCost = d, decimal.Zero
	}
	h.dayCost = h.dayCost.Add(a.CostUSD)
	h.cost = h.cost.Add(a.CostUSD)
	h.calls++
	h.mu.Unlock()
	return a, nil
}

const code200 = http.StatusOK

// failed counts a failure and logs it at most every 10 minutes.
func (h *httpDecider) failed(err error) error {
	h.mu.Lock()
	h.failures++
	h.lastErr = truncate(err.Error(), 200)
	warn := time.Since(h.lastWarn) > 10*time.Minute
	if warn {
		h.lastWarn = time.Now()
	}
	h.mu.Unlock()
	if warn {
		slog.Warn("moderation: decision model failed; the rules' decision stands", "status", ModelStatusOf(err), "err", err)
	}
	return err
}

// Health implements DeciderHealth.
func (h *httpDecider) Health() DecisionModelHealth {
	st := h.ready()
	now := h.now()
	h.mu.Lock()
	defer h.mu.Unlock()
	out := DecisionModelHealth{Configured: true, Model: h.model, Status: st, Calls: h.calls, Failures: h.failures,
		CostUSD: h.cost.StringFixed(6), DayCostUSD: decimal.Zero.StringFixed(6), DailyCapUSD: h.dailyCap.String()}
	if h.day == dayOf(now) {
		out.DayCostUSD = h.dayCost.StringFixed(6)
	}
	if now.Before(h.pausedUntil) {
		p := h.pausedUntil
		out.PausedUntil = &p
	}
	switch {
	case h.disabled != "":
		out.Note = h.disabled
	case st == ModelCapReached:
		out.Note = "The daily cost cap is spent; rules only until midnight UTC."
	case h.lastErr != "":
		out.Note = "Last error: " + h.lastErr
	}
	return out
}

// Reset implements DeciderResetter: back on after the key or credit was fixed.
func (h *httpDecider) Reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.disabled, h.pausedUntil, h.lastErr = "", time.Time{}, ""
}

// retryAfter parses Retry-After (seconds or an HTTP date); default 5 s, at
// most 10 minutes.
func retryAfter(v string, now time.Time) time.Duration {
	v = strings.TrimSpace(v)
	d := 5 * time.Second
	if n, err := strconv.Atoi(v); err == nil && n > 0 {
		d = time.Duration(n) * time.Second
	} else if t, err := http.ParseTime(v); err == nil && t.After(now) {
		d = t.Sub(now)
	}
	return min(d, 10*time.Minute)
}

func clamp01(x float64) float64 {
	if math.IsNaN(x) {
		return 0
	}
	return math.Max(0, math.Min(1, x))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
