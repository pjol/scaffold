// Package mail sends transactional email. Resend is the default provider;
// without RESEND_API_KEY (development) messages are logged, or written as
// Markdown files to MAIL_SINK_DIR when set (dev-up sets it), so flows keep
// working and every message can be read. Add a provider by implementing Mailer.
package mail

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
	"strings"
	"time"
)

type Message struct {
	To      string
	Subject string
	Text    string // required: plain-text body
	HTML    string // optional: see Layout
	ReplyTo string
}

type Mailer interface {
	Send(ctx context.Context, m Message) error
}

// New returns Resend when apiKey is set, else a FileSink when sinkDir is
// set, else Log.
func New(apiKey, from, sinkDir string) Mailer {
	switch {
	case apiKey != "":
		return &Resend{APIKey: apiKey, From: from, Client: &http.Client{Timeout: 10 * time.Second}}
	case sinkDir != "":
		return FileSink{Dir: sinkDir, From: from}
	}
	return Log{}
}

// FileSink writes each message to Dir as a Markdown file (development).
type FileSink struct{ Dir, From string }

var unsafeName = regexp.MustCompile(`[^a-z0-9]+`)

func (f FileSink) Send(ctx context.Context, m Message) error {
	if err := os.MkdirAll(f.Dir, 0o755); err != nil {
		return err
	}
	slug := strings.Trim(unsafeName.ReplaceAllString(strings.ToLower(m.Subject), "-"), "-")
	if len(slug) > 40 {
		slug = slug[:40]
	}
	name := time.Now().UTC().Format("20060102-150405.000") + "-" + slug + ".md"
	body := fmt.Sprintf("---\nfrom: %s\nto: %s\nsubject: %s\n---\n\n%s\n", f.From, m.To, m.Subject, m.Text)
	slog.InfoContext(ctx, "mail written to sink", "to", m.To, "subject", m.Subject, "file", name)
	return os.WriteFile(filepath.Join(f.Dir, name), []byte(body), 0o644)
}

// Log writes messages to the log (development).
type Log struct{}

func (Log) Send(ctx context.Context, m Message) error {
	slog.InfoContext(ctx, "mail (not sent: RESEND_API_KEY unset)", "to", m.To, "subject", m.Subject, "text", m.Text)
	return nil
}

// Resend sends through https://resend.com.
type Resend struct {
	APIKey string
	From   string
	Client *http.Client
}

func (r *Resend) Send(ctx context.Context, m Message) error {
	body := map[string]any{"from": r.From, "to": []string{m.To}, "subject": m.Subject, "text": m.Text}
	if m.HTML != "" {
		body["html"] = m.HTML
	}
	if m.ReplyTo != "" {
		body["reply_to"] = m.ReplyTo
	}
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.resend.com/emails", bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+r.APIKey)
	req.Header.Set("Content-Type", "application/json")
	res, err := r.Client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return fmt.Errorf("resend: %s: %s", res.Status, msg)
	}
	return nil
}

// Layout wraps a short HTML fragment in the house email shell: one column,
// system fonts, no images. Keep copy factual (concrete/writing.md).
func Layout(appName, bodyHTML string) string {
	return `<!doctype html><html><body style="margin:0;padding:24px;background:#f6f6f4;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;color:#1a1a1a">` +
		`<div style="max-width:520px;margin:0 auto;background:#ffffff;border-radius:12px;padding:32px">` +
		`<div style="font-weight:600;font-size:15px;margin-bottom:24px">` + appName + `</div>` +
		`<div style="font-size:15px;line-height:24px">` + bodyHTML + `</div>` +
		`</div></body></html>`
}
