package files

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/storage"
)

// platform is the HTTP plumbing both adapters share: JSON requests with the
// person's signing client, and platform errors turned into honest httpx
// errors named after the product.
type platform struct {
	client *http.Client
	name   string            // "Google Drive", "OneDrive"
	wrap   func(error) error // Connections failures (reconnect, blocked, host refused) with their fix link
}

const maxErrorBody = 64 << 10

var jsonUnmarshal = json.Unmarshal

func jsonDecode(r io.Reader, dst any) error {
	if err := json.NewDecoder(io.LimitReader(r, 4<<20)).Decode(dst); err != nil && !errors.Is(err, io.EOF) {
		return httpx.NewError(http.StatusBadGateway, app.ConnectionUnavailable, "The platform sent an answer Files couldn't read. Try again.")
	}
	return nil
}

// json sends in (nil: no body) and decodes a 2xx answer into out (nil:
// discard). Anything else becomes an httpx error.
func (p *platform) json(ctx context.Context, method, u string, in, out any, headers map[string]string) (*http.Response, error) {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := p.send(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if out != nil && resp.StatusCode != http.StatusNoContent {
		if err := json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(out); err != nil && !errors.Is(err, io.EOF) {
			return resp, httpx.NewError(http.StatusBadGateway, app.ConnectionUnavailable, p.name+" sent an answer Files couldn't read. Try again.")
		}
	} else {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	}
	return resp, nil
}

// send runs a request and checks its status; the caller closes the body of
// a 2xx/3xx response.
func (p *platform) send(req *http.Request) (*http.Response, error) {
	resp, err := p.client.Do(req)
	if err != nil {
		if req.Context().Err() != nil {
			return nil, req.Context().Err()
		}
		if code := app.ConnectionErrorCode(err); code != "" {
			return nil, p.wrap(err)
		}
		return nil, httpx.NewError(http.StatusBadGateway, app.ConnectionUnavailable, p.name+" didn't answer. Try again shortly.")
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return nil, p.statusError(resp, b)
	}
	return resp, nil
}

// platformMessage reads Drive's and Graph's error bodies:
// {"error":{"code":…,"message":…,"errors":[{"reason":…}]}}.
func platformMessage(b []byte) (code, reason, message string) {
	var e struct {
		Error struct {
			Code    json.RawMessage `json:"code"`
			Message string          `json:"message"`
			Errors  []struct {
				Reason string `json:"reason"`
			} `json:"errors"`
		} `json:"error"`
	}
	if json.Unmarshal(b, &e) != nil {
		return "", "", ""
	}
	code = strings.Trim(string(e.Error.Code), `"`)
	if len(e.Error.Errors) > 0 {
		reason = e.Error.Errors[0].Reason
	}
	return code, reason, e.Error.Message
}

// statusError says what went wrong on the platform, in the product's words.
func (p *platform) statusError(resp *http.Response, body []byte) error {
	code, reason, msg := platformMessage(body)
	detail := map[string]any{"status": resp.StatusCode}
	if msg != "" {
		detail["platform_message"] = truncate(msg, 300)
	}
	e := func(status int, c, m string) error {
		he := httpx.NewError(status, c, m)
		he.Details = detail
		return he
	}
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return p.wrap(httpx.NewError(http.StatusConflict, app.ConnectionNeedsAuth, "Reconnect "+p.name+" to continue."))
	case http.StatusForbidden:
		if strings.Contains(strings.ToLower(reason), "ratelimit") || strings.Contains(strings.ToLower(reason), "quotaexceeded") {
			return rateLimited(p.name, resp, detail)
		}
		if reason == "storageQuotaExceeded" || code == "quotaLimitReached" {
			return e(http.StatusInsufficientStorage, "connected_quota_exceeded", p.name+" storage is full. Free up space there, then try again.")
		}
		return e(http.StatusForbidden, "connected_forbidden", "This "+p.name+" account doesn't have permission to do that. Ask the owner for access in "+p.name+".")
	case http.StatusNotFound:
		return e(http.StatusNotFound, "not_found", "This isn't in "+p.name+" anymore, or this account can't open it.")
	case http.StatusConflict:
		if code == "nameAlreadyExists" || strings.Contains(strings.ToLower(msg), "name") {
			return e(http.StatusConflict, "name_taken", "Something in this "+p.name+" folder already has that name. Choose another name.")
		}
		return e(http.StatusConflict, "conflict", p.name+" refused the change because of a conflict. Refresh and try again.")
	case http.StatusPreconditionFailed:
		return e(http.StatusConflict, "target_changed", "It changed in "+p.name+" since you opened it. Refresh and try again.")
	case http.StatusRequestEntityTooLarge:
		return e(http.StatusRequestEntityTooLarge, "file_too_large", "That file is too large for "+p.name+".")
	case http.StatusLocked:
		return e(http.StatusConflict, "locked", "It's locked in "+p.name+" (checked out or open elsewhere). Try again later.")
	case http.StatusInsufficientStorage:
		return e(http.StatusInsufficientStorage, "connected_quota_exceeded", p.name+" storage is full. Free up space there, then try again.")
	case http.StatusTooManyRequests:
		return rateLimited(p.name, resp, detail)
	case http.StatusBadRequest:
		return e(http.StatusBadRequest, "invalid_request", p.name+" refused that request.")
	}
	if resp.StatusCode >= 500 {
		return e(http.StatusBadGateway, app.ConnectionUnavailable, p.name+" isn't responding. Try again shortly.")
	}
	return e(http.StatusBadGateway, app.ConnectionUnavailable, p.name+" answered "+strconv.Itoa(resp.StatusCode)+". Try again.")
}

func rateLimited(name string, resp *http.Response, detail map[string]any) error {
	he := httpx.NewError(http.StatusTooManyRequests, app.ConnectionRateLimited, name+" is limiting requests right now. Try again in a minute.")
	if ra := resp.Header.Get("Retry-After"); ra != "" {
		detail["retry_after"] = ra
	}
	he.Details = detail
	return he
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut, _ := truncateUTF8([]byte(s), n)
	return string(cut) + "…"
}

func asHTTPError(err error, dst **httpx.Error) bool { return errors.As(err, dst) }

// newObjectKey is a fresh files/<org id>/… key.
func newObjectKey(orgID uuid.UUID, ct string) (string, error) {
	return storage.NewKey(Purpose, orgID, ct)
}

// isStatus reports an httpx error with that status.
func isStatus(err error, status int) bool {
	var he *httpx.Error
	return errors.As(err, &he) && he.Status == status
}
