package identity

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Provider runs hosted identity verification with an outside service.
// Persona ships here; another vendor implements the same three calls. Without
// a provider the module runs manual review (document photo + selfie, staff
// decide).
type Provider interface {
	Name() string
	// StartInquiry creates an inquiry for the user, or resumes in.InquiryID
	// (a fresh session; providers revive expired inquiries on resume).
	StartInquiry(ctx context.Context, in StartInput) (*Inquiry, error)
	// ParseWebhook verifies the signature over the raw body and decodes the
	// event. It returns ErrBadSignature when verification fails.
	ParseWebhook(h http.Header, body []byte) (*WebhookEvent, error)
	// FetchInquiry returns the inquiry's current state from the provider.
	// Webhook bodies are never trusted for the decision.
	FetchInquiry(ctx context.Context, inquiryID string) (*Inquiry, error)
}

// Redactor is implemented by providers that can erase an inquiry's personal
// data; account deletion queues a redaction for every inquiry the user had.
type Redactor interface {
	Redact(ctx context.Context, inquiryID string) error
}

// StartInput describes a start or resume.
type StartInput struct {
	UserID      uuid.UUID
	InquiryID   string // set: resume this inquiry
	RedirectURI string // validated by the caller; "" = provider default
}

// Inquiry states, provider-neutral.
const (
	statePending   = "pending"   // created or in progress; the user still has steps to do
	stateSubmitted = "submitted" // the user finished; awaiting the provider's or a reviewer's decision
	stateVerified  = "verified"
	stateRejected  = "rejected"
	stateExpired   = "expired"
)

// Inquiry is a provider inquiry's current state.
type Inquiry struct {
	ID           string
	State        string // one of the state constants; "" = a status this code doesn't know
	SessionToken string // for the provider's embedded flow
	HostedURL    string // for the provider's hosted flow
}

// WebhookEvent is a verified provider event.
type WebhookEvent struct {
	ID        string // provider event id (dedupe key)
	Name      string
	InquiryID string // "" when the event is not about an inquiry
}

var (
	ErrBadSignature = errors.New("identity: webhook signature check failed")
	errNotFound     = errors.New("identity: inquiry not found at provider")
)

// ---- Persona ---------------------------------------------------------------------
//
// API (docs.withpersona.com), version pinned below; kebab-case attributes:
//   POST   /inquiries               create from a template; meta auto-creates the account and session
//   POST   /inquiries/{id}/resume   new session token (revives expired inquiries)
//   GET    /inquiries/{id}          current status
//   DELETE /inquiries/{id}          redact the inquiry's personal data
// Webhooks: Persona-Signature "t=<unix>,v1=<hex>", v1 = HMAC-SHA256(secret, "<t>.<body>").

const (
	personaAPIBase      = "https://api.withpersona.com/api/v1"
	personaVersion      = "2026-09-29"
	personaHostedURL    = "https://inquiry.withpersona.com/verify"
	personaDashboardURL = "https://app.withpersona.com/dashboard/inquiries/"
	personaTimeout      = 15 * time.Second
	signatureTolerance  = 5 * time.Minute
)

// Persona implements Provider and Redactor.
type Persona struct {
	APIKey        string
	TemplateID    string
	WebhookSecret string
	EnvironmentID string // optional; passed to the hosted flow and to clients for the embedded flow
	Base          string // API base, default personaAPIBase
	HTTP          *http.Client
}

func (p *Persona) Name() string { return "persona" }

type personaError struct {
	Status int
	Title  string
}

func (e *personaError) Error() string {
	return fmt.Sprintf("persona: status %d: %s", e.Status, e.Title)
}

type personaDocument struct {
	Data struct {
		ID         string `json:"id"`
		Attributes struct {
			Status string `json:"status"`
		} `json:"attributes"`
	} `json:"data"`
	Meta struct {
		SessionToken string `json:"session-token"`
	} `json:"meta"`
}

// personaState maps Persona inquiry statuses. Only "approved" verifies.
func personaState(status string) string {
	switch status {
	case "created", "pending":
		return statePending
	case "completed", "needs_review", "needs-review":
		return stateSubmitted
	case "approved":
		return stateVerified
	case "declined", "failed":
		return stateRejected
	case "expired":
		return stateExpired
	}
	return ""
}

func (p *Persona) StartInquiry(ctx context.Context, in StartInput) (*Inquiry, error) {
	var (
		doc *personaDocument
		err error
	)
	if in.InquiryID != "" {
		doc, err = p.do(ctx, http.MethodPost, "/inquiries/"+url.PathEscape(in.InquiryID)+"/resume", nil)
	} else {
		attrs := map[string]any{"inquiry-template-id": p.TemplateID}
		if in.RedirectURI != "" {
			attrs["redirect-uri"] = in.RedirectURI
		}
		doc, err = p.do(ctx, http.MethodPost, "/inquiries", map[string]any{
			"data": map[string]any{"attributes": attrs},
			"meta": map[string]any{
				"auto-create-account-reference-id": in.UserID.String(),
				"auto-create-inquiry-session":      true,
			},
		})
	}
	if err != nil {
		return nil, err
	}
	inq := &Inquiry{ID: doc.Data.ID, State: personaState(doc.Data.Attributes.Status), SessionToken: doc.Meta.SessionToken}
	if inq.State == "" || inq.State == stateExpired {
		inq.State = statePending // a resume revives the inquiry
	}
	q := url.Values{"inquiry-id": {inq.ID}}
	if inq.SessionToken != "" {
		q.Set("session-token", inq.SessionToken)
	}
	if in.RedirectURI != "" {
		q.Set("redirect-uri", in.RedirectURI)
	}
	if p.EnvironmentID != "" {
		q.Set("environment-id", p.EnvironmentID)
	}
	inq.HostedURL = personaHostedURL + "?" + q.Encode()
	return inq, nil
}

func (p *Persona) FetchInquiry(ctx context.Context, inquiryID string) (*Inquiry, error) {
	doc, err := p.do(ctx, http.MethodGet, "/inquiries/"+url.PathEscape(inquiryID), nil)
	var pe *personaError
	if errors.As(err, &pe) && pe.Status == http.StatusNotFound {
		return nil, errNotFound
	}
	if err != nil {
		return nil, err
	}
	return &Inquiry{ID: doc.Data.ID, State: personaState(doc.Data.Attributes.Status)}, nil
}

func (p *Persona) Redact(ctx context.Context, inquiryID string) error {
	_, err := p.do(ctx, http.MethodDelete, "/inquiries/"+url.PathEscape(inquiryID), nil)
	var pe *personaError
	if errors.As(err, &pe) && pe.Status == http.StatusNotFound {
		return nil // already gone
	}
	return err
}

type personaEvent struct {
	Data struct {
		ID         string `json:"id"` // evt_…
		Attributes struct {
			Name    string `json:"name"`
			Payload struct {
				Data struct {
					Type string `json:"type"`
					ID   string `json:"id"` // inq_…
				} `json:"data"`
			} `json:"payload"`
		} `json:"attributes"`
	} `json:"data"`
}

func (p *Persona) ParseWebhook(h http.Header, body []byte) (*WebhookEvent, error) {
	if !verifyPersonaSignature(h.Get("Persona-Signature"), body, p.WebhookSecret, time.Now()) {
		return nil, ErrBadSignature
	}
	var ev personaEvent
	if err := json.Unmarshal(body, &ev); err != nil || ev.Data.ID == "" {
		return nil, fmt.Errorf("identity: unexpected persona payload")
	}
	out := &WebhookEvent{ID: ev.Data.ID, Name: ev.Data.Attributes.Name}
	if d := ev.Data.Attributes.Payload.Data; d.Type == "inquiry" && strings.HasPrefix(out.Name, "inquiry.") {
		out.InquiryID = d.ID
	}
	return out, nil
}

func (p *Persona) do(ctx context.Context, method, path string, in any) (*personaDocument, error) {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(b)
	}
	base := p.Base
	if base == "" {
		base = personaAPIBase
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(base, "/")+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	req.Header.Set("Persona-Version", personaVersion)
	req.Header.Set("Key-Inflection", "kebab")
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := p.HTTP
	if client == nil {
		client = &http.Client{Timeout: personaTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("persona: %s %s: %w", method, strings.SplitN(path, "?", 2)[0], err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var e struct {
			Errors []struct {
				Title string `json:"title"`
			} `json:"errors"`
		}
		_ = json.Unmarshal(data, &e)
		pe := &personaError{Status: resp.StatusCode, Title: http.StatusText(resp.StatusCode)}
		if len(e.Errors) > 0 && e.Errors[0].Title != "" {
			pe.Title = e.Errors[0].Title
		}
		return nil, pe
	}
	var doc personaDocument
	if len(bytes.TrimSpace(data)) > 0 {
		if err := json.Unmarshal(data, &doc); err != nil {
			return nil, fmt.Errorf("persona: decode response: %w", err)
		}
	}
	return &doc, nil
}

// verifyPersonaSignature checks "t=<unix>,v1=<hex>[,v1=…]" sets (several,
// space-separated, during secret rotation) within 5 minutes, constant-time.
// An empty secret never verifies.
func verifyPersonaSignature(header string, body []byte, secret string, now time.Time) bool {
	if secret == "" {
		return false
	}
	for _, set := range strings.Fields(header) {
		var ts int64
		haveT := false
		var sigs []string
		for kv := range strings.SplitSeq(set, ",") {
			k, v, ok := strings.Cut(strings.TrimSpace(kv), "=")
			if !ok {
				continue
			}
			switch k {
			case "t":
				n, err := strconv.ParseInt(v, 10, 64)
				if err != nil {
					continue
				}
				ts, haveT = n, true
			case "v1":
				sigs = append(sigs, v)
			}
		}
		if !haveT {
			continue
		}
		if d := now.Sub(time.Unix(ts, 0)); d > signatureTolerance || d < -signatureTolerance {
			continue
		}
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(strconv.FormatInt(ts, 10) + "."))
		mac.Write(body)
		want := mac.Sum(nil)
		for _, s := range sigs {
			if got, err := hex.DecodeString(s); err == nil && hmac.Equal(got, want) {
				return true
			}
		}
	}
	return false
}
