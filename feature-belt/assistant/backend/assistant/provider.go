package assistant

import (
	"context"
	"encoding/json"
	"strings"
)

// Provider-neutral transcript. The runner, history and storage speak only
// these types; each Provider maps them to its wire format. Provider-native
// blocks that must round-trip byte for byte (Anthropic thinking blocks with
// their signatures, OpenAI reasoning items) travel in Part.Raw, tagged with the
// provider and model that produced them, and are replayed only to a provider
// that can read them.

// Message roles.
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

// Part types.
const (
	PartText       = "text"
	PartToolCall   = "tool_call"
	PartToolResult = "tool_result"
	PartReasoning  = "reasoning" // opaque provider block (thinking, encrypted reasoning)
)

// Part is one piece of a message.
type Part struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`

	// tool_call: CallID, Name, Input (a JSON object). IsError on a tool_call
	// means the provider sent arguments that are not valid JSON; Text then
	// holds what arrived.
	// tool_result: CallID, Name, Content, IsError.
	CallID  string          `json:"call_id,omitempty"`
	Name    string          `json:"name,omitempty"`
	Input   json.RawMessage `json:"input,omitempty"`
	Content string          `json:"content,omitempty"`
	IsError bool            `json:"is_error,omitempty"`

	// Raw is the provider-native block or item, replayed verbatim to the same
	// provider (and, for OpenAI, the same model).
	Provider string          `json:"provider,omitempty"`
	Model    string          `json:"model,omitempty"`
	Raw      json.RawMessage `json:"raw,omitempty"`
}

// Message is one turn of the transcript.
type Message struct {
	Role  string `json:"role"`
	Parts []Part `json:"parts"`
}

// SystemBlock is a piece of the system prompt. Cache marks the end of the
// stable prefix (tools + everything up to and including this block).
type SystemBlock struct {
	Text  string
	Cache bool
}

// ToolSpec is a tool definition as the model sees it.
type ToolSpec struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

// Request is one model call.
type Request struct {
	Model      string
	System     []SystemBlock
	Messages   []Message
	Tools      []ToolSpec
	ToolChoice string // "auto" (default) or "none": tools stay declared (cache) but can't be called
	Effort     string // low | medium | high | xhigh | max
	MaxTokens  int
	// UserHash is a stable, non-reversible id for the end user (provider
	// abuse tooling and per-user cache keys). Never the raw id or an email.
	UserHash string
	// Utility marks short tool-less calls (titles, summaries): no refusal
	// fallback, no thinking controls.
	Utility bool
}

// Stop reasons, normalised across providers.
const (
	StopEndTurn   = "end_turn"
	StopToolUse   = "tool_use"
	StopMaxTokens = "max_tokens"
	StopRefusal   = "refusal"
	StopOther     = "other"
)

// Usage is token accounting for one or more model calls. InputTokens is the
// uncached remainder; the prompt size is InputTokens + CacheReadTokens +
// CacheWriteTokens.
type Usage struct {
	InputTokens      int `json:"input_tokens"`
	CacheReadTokens  int `json:"cache_read_tokens"`
	CacheWriteTokens int `json:"cache_write_tokens"`
	OutputTokens     int `json:"output_tokens"`
}

// Add accumulates o into u.
func (u *Usage) Add(o Usage) {
	u.InputTokens += o.InputTokens
	u.CacheReadTokens += o.CacheReadTokens
	u.CacheWriteTokens += o.CacheWriteTokens
	u.OutputTokens += o.OutputTokens
}

// PromptTokens is the full prompt size of a call.
func (u Usage) PromptTokens() int { return u.InputTokens + u.CacheReadTokens + u.CacheWriteTokens }

// QuotaTokens is what counts against ASSISTANT_DAILY_TOKENS: everything the
// provider processed except cache reads (which cost about a tenth).
func (u Usage) QuotaTokens() int { return u.InputTokens + u.CacheWriteTokens + u.OutputTokens }

// Response is a finished (or partial) model call.
type Response struct {
	Model string // the model that produced it (a server-side fallback can change it)
	Parts []Part // output parts in order
	Stop  string
	Usage Usage
	// UsageKnown is false when the provider reported nothing (a stream that
	// failed early); the runner then estimates.
	UsageKnown bool
}

// Text concatenates the text parts.
func (r *Response) Text() string {
	if r == nil {
		return ""
	}
	var b strings.Builder
	for _, p := range r.Parts {
		if p.Type == PartText {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

// ToolCalls returns the tool_call parts.
func (r *Response) ToolCalls() []Part {
	if r == nil {
		return nil
	}
	var out []Part
	for _, p := range r.Parts {
		if p.Type == PartToolCall {
			out = append(out, p)
		}
	}
	return out
}

// Event types streamed by Provider.Stream.
const (
	EventText = "text" // Event.Text is a text delta
)

// Event is one streamed increment.
type Event struct {
	Type string
	Text string
}

// Provider is a model API. Implementations: Anthropic Messages API, OpenAI
// Responses API and, in development only, a scripted mock.
type Provider interface {
	// Name is the stable id stored with consent and messages: anthropic | openai | mock.
	Name() string
	// Label names the company the user's content is sent to (consent dialog).
	Label() string
	// Stream runs req, calling on for every event in order. A failure after
	// output has streamed returns the partial response with the error.
	Stream(ctx context.Context, req Request, on func(Event)) (*Response, error)
	// Complete runs req without streaming (titles, summaries).
	Complete(ctx context.Context, req Request) (*Response, error)
}

// providerInfo names a provider for clients.
type providerInfo struct {
	Name  string `json:"name"`
	Label string `json:"label"`
}

var providerLabels = map[string]string{
	"anthropic": "Anthropic",
	"openai":    "OpenAI",
	"mock":      "Development mock (no AI provider)",
}
