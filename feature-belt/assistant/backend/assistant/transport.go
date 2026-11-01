package assistant

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"scaffoldapp/backend/internal/httpx"
)

// Shared plumbing for the provider clients: one HTTP client, retries that
// only happen before any output streamed, an idle timeout on streams, a
// spec-compliant SSE reader, and the mapping of provider failures onto safe
// API errors.

const (
	// maxRetries is how many times a call is retried (429, 5xx, overloads,
	// connection errors), and only while nothing has been streamed yet, so
	// text is never duplicated.
	maxRetries = 3
	// maxRetryWait caps one backoff sleep; a longer Retry-After means the
	// provider is out for longer than a user will wait.
	maxRetryWait = 20 * time.Second
	// streamHeaderTimeout bounds the wait for a stream's response headers.
	streamHeaderTimeout = 90 * time.Second
	// streamIdleTimeout cancels a stream that sends nothing (not even a ping)
	// for this long.
	streamIdleTimeout = 120 * time.Second
	maxErrorBody      = 1 << 20
	maxSSELine        = 32 << 20
)

func newHTTPClient() *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.MaxIdleConnsPerHost = 32
	tr.DialContext = (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	tr.TLSHandshakeTimeout = 10 * time.Second
	return &http.Client{Transport: tr}
}

// ---- errors ----------------------------------------------------------------

// providerError is a failure reported by (or while talking to) a provider.
// It never leaves the package: toAPIError turns it into a safe *httpx.Error.
type providerError struct {
	Status     int    // HTTP status; 0 for transport and in-stream errors
	Type       string // provider error type (invalid_request_error, overloaded_error…)
	Code       string // provider error code (OpenAI)
	Message    string // provider message: may echo request content, so log only, redacted
	Transport  bool
	RetryAfter time.Duration
}

func (e *providerError) Error() string {
	switch {
	case e.Transport:
		return "provider transport: " + redact(e.Message)
	case e.Status > 0:
		return fmt.Sprintf("provider http %d %s/%s: %s", e.Status, e.Type, e.Code, redact(e.Message))
	}
	return fmt.Sprintf("provider stream %s/%s: %s", e.Type, e.Code, redact(e.Message))
}

func (e *providerError) retryable() bool {
	switch e.Code {
	case "insufficient_quota", "billing_hard_limit_reached":
		return false
	}
	switch {
	case e.Transport:
		return true
	case e.Status == http.StatusTooManyRequests, e.Status == http.StatusRequestTimeout,
		e.Status == http.StatusConflict, e.Status >= 500:
		return true
	case e.Status == 0:
		switch e.Type {
		case "overloaded_error", "api_error", "rate_limit_error":
			return true
		}
		switch e.Code {
		case "server_error", "rate_limit_exceeded", "server_is_overloaded", "slow_down", "timeout":
			return true
		}
	}
	return false
}

// keyRE matches API-key-shaped strings (providers echo masked keys in 401s).
var keyRE = regexp.MustCompile(`(?i)\b(sk|sess|rk)-[A-Za-z0-9_\-*]{4,}`)

func redact(s string) string {
	s = keyRE.ReplaceAllString(s, "[redacted-key]")
	if len(s) > 600 {
		s = s[:600] + "…"
	}
	return s
}

// parseHTTPError reads an error body. Anthropic sends
// {"type":"error","error":{"type","message"}}; OpenAI sends
// {"error":{"message","type","param","code"}}.
func parseHTTPError(status int, header http.Header, body []byte) *providerError {
	e := &providerError{Status: status, RetryAfter: retryAfter(header)}
	var env struct {
		Error struct {
			Type    string          `json:"type"`
			Message string          `json:"message"`
			Code    json.RawMessage `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &env) == nil && env.Error.Message != "" {
		e.Type, e.Message, e.Code = env.Error.Type, env.Error.Message, rawCode(env.Error.Code)
	} else {
		e.Message = strings.TrimSpace(string(body))
	}
	return e
}

func rawCode(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	if len(raw) > 0 && string(raw) != "null" {
		return string(raw)
	}
	return ""
}

func retryAfter(h http.Header) time.Duration {
	if h == nil {
		return 0
	}
	if v := h.Get("Retry-After-Ms"); v != "" {
		if ms, err := strconv.ParseFloat(v, 64); err == nil && ms > 0 {
			return time.Duration(ms * float64(time.Millisecond))
		}
	}
	v := strings.TrimSpace(h.Get("Retry-After"))
	if v == "" {
		return 0
	}
	if secs, err := strconv.ParseFloat(v, 64); err == nil && secs > 0 {
		return time.Duration(secs * float64(time.Second))
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

func transportError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return &providerError{Transport: true, Message: err.Error()}
}

// API error codes the assistant returns for provider failures.
const (
	codeUnavailable  = "assistant_unavailable" // 503: busy, down, misconfigured
	codeProviderFail = "assistant_error"       // 502: the provider rejected or failed the request
)

const (
	msgBusy        = "The assistant is busy. Try again in a moment."
	msgUnavailable = "The assistant isn't available right now. Try again shortly."
	msgMisconfig   = "The assistant isn't set up correctly on this server."
	msgBadRequest  = "The assistant couldn't process that request."
	msgTooLong     = "This conversation is too long for the model. Start a new conversation."
	msgFailed      = "The assistant ran into a problem. Try again."
)

// toAPIError maps a provider failure onto a safe *httpx.Error. Provider
// messages are never shown: they can quote request content or key fragments.
func toAPIError(e *providerError) *httpx.Error {
	var out *httpx.Error
	lower := strings.ToLower(e.Message)
	switch {
	case e.Transport:
		out = httpx.NewError(http.StatusServiceUnavailable, codeUnavailable, msgUnavailable)
	case e.Status == http.StatusUnauthorized, e.Status == http.StatusForbidden, e.Status == http.StatusPaymentRequired,
		e.Status == http.StatusNotFound, e.Code == "insufficient_quota", e.Code == "billing_hard_limit_reached",
		e.Code == "model_not_found", e.Type == "authentication_error", e.Type == "permission_error",
		e.Type == "billing_error", e.Type == "not_found_error":
		out = httpx.NewError(http.StatusServiceUnavailable, codeUnavailable, msgMisconfig)
	case e.Status == http.StatusTooManyRequests, e.Status == 529, e.Type == "rate_limit_error",
		e.Type == "overloaded_error", e.Code == "rate_limit_exceeded", e.Code == "server_is_overloaded", e.Code == "slow_down":
		out = httpx.NewError(http.StatusServiceUnavailable, codeUnavailable, msgBusy)
	case e.Status >= 500, e.Type == "api_error", e.Code == "server_error":
		out = httpx.NewError(http.StatusServiceUnavailable, codeUnavailable, msgUnavailable)
	case e.Code == "context_length_exceeded", e.Status == http.StatusRequestEntityTooLarge,
		strings.Contains(lower, "context window"), strings.Contains(lower, "prompt is too long"):
		out = httpx.NewError(http.StatusBadGateway, codeProviderFail, msgTooLong)
	case e.Status == http.StatusBadRequest, e.Status == http.StatusUnprocessableEntity:
		out = httpx.NewError(http.StatusBadGateway, codeProviderFail, msgBadRequest)
	default:
		out = httpx.NewError(http.StatusBadGateway, codeProviderFail, msgFailed)
	}
	// In-stream failures are usually transient model-side problems.
	retry := e.retryable() || (e.Status == 0 && !e.Transport)
	out.Details = map[string]any{"retryable": retry}
	return out
}

// isRetryable reports whether an error is worth offering "try again" for.
func isRetryable(err error) bool {
	var apiErr *httpx.Error
	if errors.As(err, &apiErr) {
		if r, ok := apiErr.Details["retryable"].(bool); ok {
			return r
		}
		return apiErr.Code == codeUnavailable
	}
	return errors.Is(err, context.DeadlineExceeded)
}

// ---- retries ---------------------------------------------------------------

type retrier struct {
	sleep  func(ctx context.Context, d time.Duration) error
	jitter func() float64
}

func newRetrier() retrier { return retrier{sleep: sleepCtx, jitter: rand.Float64} }

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// do runs attempt until it succeeds, fails permanently, has delivered output
// (a retry would duplicate it), or the retries are spent. Provider failures
// come back as *httpx.Error.
func (rt retrier) do(ctx context.Context, provider, model string, attempt func() (delivered bool, err error)) error {
	for n := 0; ; n++ {
		delivered, err := attempt()
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var pe *providerError
		if !errors.As(err, &pe) {
			return err
		}
		wait := rt.backoff(n, pe.RetryAfter)
		if delivered || !pe.retryable() || n >= maxRetries || wait > maxRetryWait {
			slog.WarnContext(ctx, "assistant: provider call failed", "provider", provider, "model", model,
				"attempts", n+1, "delivered", delivered, "err", pe.Error())
			return toAPIError(pe)
		}
		slog.InfoContext(ctx, "assistant: provider call retrying", "provider", provider, "model", model,
			"attempt", n+1, "wait_ms", wait.Milliseconds(), "err", pe.Error())
		if err := rt.sleep(ctx, wait); err != nil {
			return err
		}
	}
}

// backoff is jittered exponential (0.5 s, 1 s, 2 s… ×[0.5, 1.5)), never
// shorter than the provider's Retry-After.
func (rt retrier) backoff(attempt int, after time.Duration) time.Duration {
	d := time.Duration(float64(500*time.Millisecond) * float64(int(1)<<attempt) * (0.5 + rt.jitter()))
	return max(d, after)
}

// ---- streaming -------------------------------------------------------------

// openStream POSTs body and returns the response once headers arrive (or a
// providerError for a non-200). The caller wraps the body in newIdleReader.
func openStream(ctx context.Context, client *http.Client, req *http.Request, cancel context.CancelFunc) (*http.Response, error) {
	headerTimer := time.AfterFunc(streamHeaderTimeout, cancel)
	res, err := client.Do(req)
	headerTimer.Stop()
	if err != nil {
		return nil, transportError(ctx, err)
	}
	if res.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(res.Body, maxErrorBody))
		res.Body.Close()
		return nil, parseHTTPError(res.StatusCode, res.Header, raw)
	}
	return res, nil
}

// postJSON sends a non-streaming request and returns the 2xx body.
func postJSON(ctx context.Context, client *http.Client, req *http.Request) ([]byte, error) {
	res, err := client.Do(req)
	if err != nil {
		return nil, transportError(ctx, err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 64<<20))
	if err != nil {
		return nil, transportError(ctx, err)
	}
	if res.StatusCode/100 != 2 {
		return nil, parseHTTPError(res.StatusCode, res.Header, raw[:min(len(raw), maxErrorBody)])
	}
	return raw, nil
}

// idleReader cancels a stream that goes silent for longer than idle.
type idleReader struct {
	r     io.Reader
	timer *time.Timer
	idle  time.Duration
	once  sync.Once
}

func newIdleReader(r io.Reader, idle time.Duration, onIdle func()) *idleReader {
	return &idleReader{r: r, idle: idle, timer: time.AfterFunc(idle, onIdle)}
}

func (ir *idleReader) Read(p []byte) (int, error) {
	n, err := ir.r.Read(p)
	if n > 0 {
		ir.timer.Reset(ir.idle)
	}
	return n, err
}

func (ir *idleReader) stop() { ir.once.Do(func() { ir.timer.Stop() }) }

// sseFrame is one dispatched server-sent event.
type sseFrame struct {
	Event string
	Data  string
}

// sseReader parses text/event-stream incrementally: `event:` and multi-line
// `data:` fields, comment lines skipped, LF or CRLF endings, a frame
// dispatched on each blank line.
type sseReader struct{ r *bufio.Reader }

func newSSEReader(r io.Reader) *sseReader { return &sseReader{r: bufio.NewReaderSize(r, 64<<10)} }

var errLineTooLong = errors.New("sse: line too long")

// Next returns the next frame, io.EOF at a clean end, io.ErrUnexpectedEOF
// mid-frame.
func (s *sseReader) Next() (sseFrame, error) {
	var (
		f       sseFrame
		data    strings.Builder
		hasData bool
		partial bool
	)
	for {
		line, err := s.readLine()
		if err != nil {
			if errors.Is(err, io.EOF) {
				if partial {
					return sseFrame{}, io.ErrUnexpectedEOF
				}
				return sseFrame{}, io.EOF
			}
			return sseFrame{}, err
		}
		if line == "" {
			if hasData {
				f.Data = data.String()
				return f, nil
			}
			f, partial = sseFrame{}, false
			continue
		}
		if line[0] == ':' {
			continue
		}
		partial = true
		field, value, found := strings.Cut(line, ":")
		if found {
			value = strings.TrimPrefix(value, " ")
		}
		switch field {
		case "event":
			f.Event = value
		case "data":
			if hasData {
				data.WriteByte('\n')
			}
			data.WriteString(value)
			hasData = true
		}
	}
}

func (s *sseReader) readLine() (string, error) {
	var b []byte
	for {
		chunk, err := s.r.ReadSlice('\n')
		if len(b)+len(chunk) > maxSSELine {
			return "", errLineTooLong
		}
		b = append(b, chunk...)
		switch {
		case err == nil:
			return strings.TrimSuffix(strings.TrimSuffix(string(b), "\n"), "\r"), nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF):
			if len(b) == 0 {
				return "", io.EOF
			}
			return "", io.ErrUnexpectedEOF
		default:
			return "", fmt.Errorf("sse: read: %w", err)
		}
	}
}
