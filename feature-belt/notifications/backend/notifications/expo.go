package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Expo push service endpoints and limits.
const (
	expoSendURL     = "https://exp.host/--/api/v2/push/send"
	expoReceiptsURL = "https://exp.host/--/api/v2/push/getReceipts"
	expoBatchSize   = 100 // messages per send request
	expoReceiptIDs  = 300 // ticket ids per getReceipts request
	expoMaxRetries  = 2   // per request, on 429 and 5xx
)

// expoTokenRe matches the token shapes expo-server-sdk accepts:
// ExponentPushToken[…] / ExpoPushToken[…], or a bare UUID.
var expoTokenRe = regexp.MustCompile(
	`^(?:Expo(?:nent)?PushToken\[[^\[\]\s]{1,180}\]|[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})$`)

// Push delivery modes. expo calls the Expo push service; sink writes each
// batch as JSON to PUSH_SINK_DIR; log only logs. sink and log are chosen
// only in development without EXPO_ACCESS_TOKEN.
const (
	modeExpo = "expo"
	modeSink = "sink"
	modeLog  = "log"
)

// expoMessage is one Expo push message (one recipient token).
type expoMessage struct {
	To       string         `json:"to"`
	Title    string         `json:"title,omitempty"`
	Body     string         `json:"body,omitempty"`
	Data     map[string]any `json:"data,omitempty"`
	Sound    string         `json:"sound,omitempty"`
	Badge    *int           `json:"badge,omitempty"`
	Priority string         `json:"priority,omitempty"`
}

// expoTicket is Expo's per-message answer, positional to the request.
type expoTicket struct {
	Status  string `json:"status"` // ok | error
	ID      string `json:"id"`
	Message string `json:"message"`
	Details struct {
		Error string `json:"error"` // DeviceNotRegistered, MessageTooBig, MessageRateExceeded, …
	} `json:"details"`
}

type expoReceipt struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Details struct {
		Error string `json:"error"`
	} `json:"details"`
}

type expoRequestError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// expoClient talks to the Expo push service, or stands in for it in
// development (sink/log).
type expoClient struct {
	mode        string
	token       string // EXPO_ACCESS_TOKEN, optional
	sinkDir     string
	http        *http.Client
	sendURL     string
	receiptsURL string
	retryBase   time.Duration
}

func newExpoClient(accessToken, sinkDir string, development bool) *expoClient {
	c := &expoClient{
		mode: modeExpo, token: accessToken, sinkDir: sinkDir, http: &http.Client{Timeout: 15 * time.Second},
		sendURL: expoSendURL, receiptsURL: expoReceiptsURL, retryBase: time.Second,
	}
	if development && accessToken == "" {
		c.mode = modeLog
		if sinkDir != "" {
			c.mode = modeSink
		}
	}
	return c
}

// send delivers one batch (≤ expoBatchSize) and returns a ticket per message.
// In sink and log modes it records the batch and returns no tickets.
func (c *expoClient) send(ctx context.Context, label string, msgs []expoMessage) ([]expoTicket, error) {
	switch c.mode {
	case modeSink:
		return nil, c.writeSink(label, msgs)
	case modeLog:
		return nil, nil
	}
	var out struct {
		Data   []expoTicket       `json:"data"`
		Errors []expoRequestError `json:"errors"`
	}
	if err := c.post(ctx, c.sendURL, msgs, &out); err != nil {
		return nil, err
	}
	if len(out.Errors) > 0 {
		return nil, fmt.Errorf("expo push: %s: %s", out.Errors[0].Code, out.Errors[0].Message)
	}
	return out.Data, nil
}

// pushOutcome is what happened to one message of a sendAll.
type pushOutcome struct {
	Token    string
	TicketID string // accepted by Expo; check the receipt later
	Dead     bool   // DeviceNotRegistered: remove the token
	Err      string // any other failure (batch or message)
}

// sendAll sends msgs in batches of expoBatchSize and reports an outcome per
// message. A failed batch or rejected message never stops the rest. In sink
// and log modes every message counts as sent with no ticket.
func (c *expoClient) sendAll(ctx context.Context, label string, msgs []expoMessage) []pushOutcome {
	out := make([]pushOutcome, len(msgs))
	for start := 0; start < len(msgs); start += expoBatchSize {
		end := min(start+expoBatchSize, len(msgs))
		for i := start; i < end; i++ {
			out[i].Token = msgs[i].To
		}
		tickets, err := c.send(ctx, label, msgs[start:end])
		if err != nil {
			for i := start; i < end; i++ {
				out[i].Err = err.Error()
			}
			continue
		}
		if c.mode != modeExpo {
			continue
		}
		for i := start; i < end; i++ {
			j := i - start
			switch {
			case j >= len(tickets):
				out[i].Err = "no ticket returned"
			case tickets[j].Status == "ok" && tickets[j].ID != "":
				out[i].TicketID = tickets[j].ID
			case tickets[j].Details.Error == "DeviceNotRegistered":
				out[i].Dead = true
			default:
				// Expo's message can quote the token; log the error code only.
				out[i].Err = tickets[j].Details.Error
				if out[i].Err == "" {
					out[i].Err = "rejected"
				}
			}
		}
	}
	return out
}

// receipts fetches receipts for ticket ids (≤ expoReceiptIDs). Ids missing
// from the result are not ready yet.
func (c *expoClient) receipts(ctx context.Context, ids []string) (map[string]expoReceipt, error) {
	var out struct {
		Data   map[string]expoReceipt `json:"data"`
		Errors []expoRequestError     `json:"errors"`
	}
	if err := c.post(ctx, c.receiptsURL, map[string]any{"ids": ids}, &out); err != nil {
		return nil, err
	}
	if len(out.Errors) > 0 {
		return nil, fmt.Errorf("expo receipts: %s: %s", out.Errors[0].Code, out.Errors[0].Message)
	}
	return out.Data, nil
}

// post sends JSON and decodes the reply, retrying 429 and 5xx with backoff
// (Retry-After when Expo sends it).
func (c *expoClient) post(ctx context.Context, url string, body, dst any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	var lastErr error
	for attempt := 0; attempt <= expoMaxRetries; attempt++ {
		if attempt > 0 {
			wait := time.Duration(attempt) * c.retryBase
			if ra, ok := lastErr.(retryAfter); ok && ra.after > 0 && ra.after < 30*time.Second {
				wait = ra.after
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		if c.token != "" {
			req.Header.Set("Authorization", "Bearer "+c.token)
		}
		res, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		b, readErr := io.ReadAll(io.LimitReader(res.Body, 4<<20))
		res.Body.Close()
		if res.StatusCode == http.StatusTooManyRequests || res.StatusCode >= 500 {
			secs, _ := strconv.Atoi(res.Header.Get("Retry-After"))
			lastErr = retryAfter{status: res.StatusCode, after: time.Duration(secs) * time.Second}
			continue
		}
		if readErr != nil {
			return readErr
		}
		if res.StatusCode >= 300 {
			// Expo returns {"errors":[…]} on request-level failures.
			if json.Unmarshal(b, dst) == nil {
				return nil
			}
			return fmt.Errorf("expo: %s", res.Status)
		}
		return json.Unmarshal(b, dst)
	}
	return lastErr
}

type retryAfter struct {
	status int
	after  time.Duration
}

func (e retryAfter) Error() string { return fmt.Sprintf("expo: status %d", e.status) }

// writeSink writes a batch to PUSH_SINK_DIR as one JSON file (development).
func (c *expoClient) writeSink(label string, msgs []expoMessage) error {
	if err := os.MkdirAll(c.sinkDir, 0o755); err != nil {
		return err
	}
	name := time.Now().UTC().Format("20060102-150405.000") + "-" + sanitizeFileName(label) + ".json"
	b, err := json.MarshalIndent(map[string]any{"messages": msgs}, "", "  ")
	if err != nil {
		return err
	}
	slog.Info("push written to sink", "file", name, "messages", len(msgs))
	return os.WriteFile(filepath.Join(c.sinkDir, name), append(b, '\n'), 0o644)
}

var unsafeFileChars = regexp.MustCompile(`[^a-z0-9.-]+`)

func sanitizeFileName(s string) string {
	s = strings.Trim(unsafeFileChars.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if len(s) > 60 {
		s = s[:60]
	}
	return s
}
