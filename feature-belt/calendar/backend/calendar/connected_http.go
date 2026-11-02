package calendar

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/httpx"
)

// platform does JSON requests against one platform with the person's signing
// client and turns failures into errors people can act on.
type platform struct {
	client  *http.Client
	name    string // "Google Calendar", "Outlook Calendar"
	headers map[string]string
}

// errSyncGone is a platform's "start over" answer to a sync or delta token
// (Google 410 Gone, Graph 410 / syncStateNotFound).
var errSyncGone = errors.New("calendar: sync token no longer valid")

// errPlatformGone is a 404/410 for an item (deleted on the platform).
var errPlatformGone = httpx.NotFound("That event doesn't exist any more, or this account can't see it.")

const maxPlatformBody = 8 << 20

// call sends a request (body JSON-encoded when non-nil) and decodes a JSON
// answer into out (when non-nil). rawURL is absolute.
func (p platform) call(ctx context.Context, method, rawURL string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, rawURL, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range p.headers {
		req.Header.Set(k, v)
	}
	res, err := p.client.Do(req)
	if err != nil {
		var he *httpx.Error
		if errors.As(err, &he) {
			return he
		}
		if ctx.Err() != nil {
			return httpx.NewError(http.StatusGatewayTimeout, app.ConnectionUnavailable, p.name+" didn't answer in time. Try again.")
		}
		return httpx.NewError(http.StatusBadGateway, app.ConnectionUnavailable, "Couldn't reach "+p.name+". Try again.")
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, maxPlatformBody+1))
	if err != nil {
		return httpx.NewError(http.StatusBadGateway, app.ConnectionUnavailable, "Couldn't read "+p.name+"'s answer. Try again.")
	}
	if len(data) > maxPlatformBody {
		return httpx.NewError(http.StatusBadGateway, app.ConnectionTooLarge, p.name+" sent more than this app reads at once.")
	}
	if res.StatusCode >= 300 {
		return p.statusError(res.StatusCode, data)
	}
	if out == nil || len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return httpx.NewError(http.StatusBadGateway, app.ConnectionUnavailable, p.name+" sent an answer this app couldn't read.")
	}
	return nil
}

// platformErr is the error body both platforms send: Google
// {"error":{"code","message","errors":[{"reason"}],"status"}}, Graph
// {"error":{"code","message"}}.
type platformErr struct {
	Error struct {
		Code    json.RawMessage `json:"code"`
		Message string          `json:"message"`
		Status  string          `json:"status"`
		Errors  []struct {
			Reason string `json:"reason"`
		} `json:"errors"`
	} `json:"error"`
}

func (p platform) statusError(status int, data []byte) error {
	var pe platformErr
	_ = json.Unmarshal(data, &pe)
	code := strings.Trim(string(pe.Error.Code), `"`)
	reason := code
	if len(pe.Error.Errors) > 0 {
		reason = pe.Error.Errors[0].Reason
	}
	switch {
	case status == http.StatusGone || strings.EqualFold(code, "SyncStateNotFound") || strings.EqualFold(code, "resyncRequired") || reason == "fullSyncRequired":
		if status == http.StatusGone || status == http.StatusBadRequest {
			return fmt.Errorf("%w (%s)", errSyncGone, reason)
		}
	}
	switch status {
	case http.StatusUnauthorized:
		return httpx.NewError(http.StatusConflict, app.ConnectionNeedsAuth, "Reconnect "+p.name+" to continue.")
	case http.StatusTooManyRequests:
		return httpx.NewError(http.StatusTooManyRequests, app.ConnectionRateLimited, p.name+" is limiting requests. Try again in a minute.")
	case http.StatusForbidden:
		switch reason {
		case "rateLimitExceeded", "userRateLimitExceeded", "quotaExceeded":
			return httpx.NewError(http.StatusTooManyRequests, app.ConnectionRateLimited, p.name+" is limiting requests. Try again in a minute.")
		case "insufficientPermissions", "ACCESS_TOKEN_SCOPE_INSUFFICIENT", "ErrorAccessDenied":
			return httpx.NewError(http.StatusConflict, app.ConnectionScopeMissing, "Grant "+p.name+" access again in Settings → Connections.")
		}
		return httpx.NewError(http.StatusForbidden, "connection_forbidden", p.name+" didn't allow that for this account.")
	case http.StatusNotFound:
		return errPlatformGone
	case http.StatusConflict, http.StatusPreconditionFailed:
		return httpx.NewError(http.StatusConflict, "connection_conflict", "The event changed on "+p.name+" meanwhile. Reload and try again.")
	case http.StatusBadRequest:
		msg := strings.TrimSpace(pe.Error.Message)
		if msg == "" || len(msg) > 300 {
			msg = "the change wasn't accepted"
		}
		return httpx.NewError(http.StatusBadRequest, "connection_rejected", p.name+": "+msg)
	}
	if status >= 500 {
		return httpx.NewError(http.StatusBadGateway, app.ConnectionUnavailable, p.name+" had a problem. Try again.")
	}
	return httpx.NewError(http.StatusBadGateway, app.ConnectionUnavailable, fmt.Sprintf("%s answered %d.", p.name, status))
}

var (
	zoomLink  = regexp.MustCompile(`https://[a-z0-9.-]*zoom\.us/(?:j|my|w)/[^\s<>"']+`)
	teamsLink = regexp.MustCompile(`https://teams\.(?:microsoft|live)\.com/(?:l/meetup-join|meet)/[^\s<>"']+`)
	meetLink  = regexp.MustCompile(`https://meet\.google\.com/[a-z]{3,}-[a-z]{3,}-[a-z]{3,}`)
)

// meetingIn finds a meeting link written into an event's place or notes.
func meetingIn(texts ...string) (url, provider string) {
	for _, t := range texts {
		switch {
		case zoomLink.MatchString(t):
			return zoomLink.FindString(t), ProviderZoom
		case teamsLink.MatchString(t):
			return teamsLink.FindString(t), ProviderTeams
		case meetLink.MatchString(t):
			return meetLink.FindString(t), ProviderGoogleMeet
		}
	}
	return "", ""
}

// joinText is the line a meeting made elsewhere adds to an event's notes.
func joinText(label, url string) string {
	if label == "" {
		label = "the video call"
	}
	return "Join " + label + ": " + url
}

// withJoin adds the join line to a description (once).
func withJoin(desc, label, url string) string {
	if url == "" || strings.Contains(desc, url) {
		return desc
	}
	line := joinText(label, url)
	if strings.TrimSpace(desc) == "" {
		return line
	}
	return line + "\n\n" + desc
}
