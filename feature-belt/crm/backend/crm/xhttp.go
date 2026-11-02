package crm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/httpx"
)

// Platform requests: JSON in and out with the person's client, a time limit,
// a size cap, and the platform's failures turned into errors clients act on
// (reconnect, slow down, fix a field).

const (
	xRequestTimeout = 20 * time.Second
	xMaxBody        = 16 << 20
)

// platformError is how one platform reports a failed request: a message and
// the fields it names.
type platformError func(status int, body []byte) (msg string, fields map[string]string)

// xreq is one request to a platform API.
type xreq struct {
	hc     *http.Client
	label  string // HubSpot, Salesforce
	method string
	url    string
	query  url.Values
	body   any
	header http.Header
	parse  platformError
	// limited reports a rate limit sent with another status (Salesforce
	// answers 403 REQUEST_LIMIT_EXCEEDED).
	limited func(status int, body []byte) bool
}

// do sends the request and decodes a JSON answer into out (nil: ignore it).
func (q xreq) do(ctx context.Context, out any) error {
	ctx, cancel := context.WithTimeout(ctx, xRequestTimeout)
	defer cancel()
	u := q.url
	if len(q.query) > 0 {
		if strings.Contains(u, "?") {
			u += "&" + q.query.Encode()
		} else {
			u += "?" + q.query.Encode()
		}
	}
	var rd io.Reader
	if q.body != nil {
		b, err := json.Marshal(q.body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, q.method, u, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if q.body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range q.header {
		req.Header[k] = v
	}
	res, err := q.hc.Do(req)
	if err != nil {
		if app.ConnectionErrorCode(err) != "" {
			return passConnErr(err)
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return httpx.NewError(http.StatusGatewayTimeout, app.ConnectionUnavailable, q.label+" didn't answer in time. Try again.")
		}
		if errors.Is(err, context.Canceled) {
			return err
		}
		return httpx.NewError(http.StatusBadGateway, app.ConnectionUnavailable, q.label+" couldn't be reached. Try again shortly.")
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, xMaxBody+1))
	if err != nil {
		return httpx.NewError(http.StatusBadGateway, app.ConnectionUnavailable, q.label+" stopped answering. Try again.")
	}
	if len(body) > xMaxBody {
		return httpx.NewError(http.StatusBadGateway, app.ConnectionTooLarge, q.label+" sent more than the CRM reads at once.")
	}
	if res.StatusCode >= 300 {
		return q.fail(res, body)
	}
	if out == nil || len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return httpx.NewError(http.StatusBadGateway, app.ConnectionUnavailable, q.label+" sent an answer the CRM couldn't read.")
	}
	return nil
}

// fail maps a platform error response.
func (q xreq) fail(res *http.Response, body []byte) error {
	msg, fields := "", map[string]string(nil)
	if q.parse != nil {
		msg, fields = q.parse(res.StatusCode, body)
	}
	if q.limited != nil && q.limited(res.StatusCode, body) {
		return httpx.NewError(http.StatusTooManyRequests, app.ConnectionRateLimited, q.label+" is limiting requests from this account. Try again later.")
	}
	switch {
	case res.StatusCode == http.StatusUnauthorized:
		return httpx.NewError(http.StatusConflict, app.ConnectionNeedsAuth, q.label+" stopped accepting this connection. Reconnect it in Settings → Connections.")
	case res.StatusCode == http.StatusTooManyRequests:
		e := httpx.NewError(http.StatusTooManyRequests, app.ConnectionRateLimited, q.label+" is limiting requests from this account. Try again in a minute.")
		if ra := res.Header.Get("Retry-After"); ra != "" {
			if n, err := strconv.Atoi(ra); err == nil {
				e.Details = map[string]any{"retry_after": n}
			}
		}
		return e
	case res.StatusCode == http.StatusForbidden:
		m := q.label + " doesn't let this account do that."
		if msg != "" {
			m += " " + msg
		}
		return httpx.NewError(http.StatusForbidden, "platform_forbidden", m)
	case res.StatusCode == http.StatusNotFound:
		return httpx.NotFound("Not found in " + q.label + ", or this account can't see it.")
	case res.StatusCode >= 500:
		return httpx.NewError(http.StatusBadGateway, app.ConnectionUnavailable, q.label+" had a problem answering. Try again shortly.")
	}
	if msg == "" {
		msg = fmt.Sprintf("%s refused the change (%d).", q.label, res.StatusCode)
	}
	e := httpx.NewError(http.StatusUnprocessableEntity, "platform_validation", q.label+": "+msg)
	if len(fields) > 0 {
		e.Details = map[string]any{"fields": fields}
	}
	return e
}

// fieldValueError is a value the CRM refused before sending it.
func fieldValueError(label, msg string) *httpx.Error {
	e := httpx.NewError(http.StatusUnprocessableEntity, "invalid_value", msg)
	e.Details = map[string]any{"field": label}
	return e
}

// ---- small conversions -----------------------------------------------------------

func parseFloat(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	return f, err == nil
}

func parseInt(s string) (int, bool) {
	f, ok := parseFloat(s)
	return int(f), ok
}

// parseTime reads ISO timestamps and epoch milliseconds (HubSpot uses both).
func parseTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	if ms, err := strconv.ParseInt(s, 10, 64); err == nil {
		return time.UnixMilli(ms).UTC(), true
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.000-0700", "2006-01-02T15:04:05.000Z0700", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

func timePtr(s string) *time.Time {
	if t, ok := parseTime(s); ok {
		return &t
	}
	return nil
}

// dateOnly is YYYY-MM-DD of a date or timestamp value.
func dateOnly(s string) *string {
	if s == "" {
		return nil
	}
	if len(s) >= 10 && s[4] == '-' && s[7] == '-' {
		d := s[:10]
		return &d
	}
	if t, ok := parseTime(s); ok {
		d := t.Format("2006-01-02")
		return &d
	}
	return nil
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func nonEmpty(list ...string) []string {
	out := []string{}
	for _, s := range list {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// domainOf turns a website into a bare domain (example.com).
func domainOf(site string) *string {
	site = strings.TrimSpace(strings.ToLower(site))
	if site == "" {
		return nil
	}
	if d, err := cleanDomain(site); err == nil {
		return &d
	}
	return &site
}

// jsonValue decodes a raw field value for a platform write.
func jsonValue(raw json.RawMessage) (any, error) {
	var v any
	if len(raw) == 0 {
		return nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

func asText(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case json.Number:
		return t.String()
	case bool:
		return strconv.FormatBool(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func asStrings(v any) []string {
	switch t := v.(type) {
	case []any:
		out := make([]string, 0, len(t))
		for _, x := range t {
			out = append(out, asText(x))
		}
		return out
	case []string:
		return t
	case string:
		if t == "" {
			return []string{}
		}
		return []string{t}
	}
	return nil
}

// timeBounds is the start of today and of this week (Monday) in loc.
func timeBounds(loc *time.Location) (dayStart, dayEnd, weekStart, monthStart, monthEnd time.Time) {
	now := time.Now().In(loc)
	dayStart = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	dayEnd = dayStart.AddDate(0, 0, 1)
	wd := (int(now.Weekday()) + 6) % 7
	weekStart = dayStart.AddDate(0, 0, -wd)
	monthStart = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
	monthEnd = monthStart.AddDate(0, 1, 0)
	return
}

// taskBucket says where an open or done task falls.
func taskBucket(a *XActivity, loc *time.Location) string {
	if a.DoneAt != nil {
		return "done"
	}
	dayStart, dayEnd, _, _, _ := timeBounds(loc)
	switch {
	case a.DueAt == nil:
		return "upcoming"
	case a.DueAt.Before(dayStart):
		return "overdue"
	case a.DueAt.Before(dayEnd):
		return "today"
	}
	return "upcoming"
}

// offsetCursor pages an in-memory list.
type offsetCursor struct {
	O int `json:"o"`
}

func readOffset(cursor string) int {
	if cursor == "" {
		return 0
	}
	var c offsetCursor
	if httpx.DecodeCursor(cursor, &c) != nil || c.O < 0 {
		return 0
	}
	return c.O
}
