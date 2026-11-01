package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
)

// Anthropic Messages API over plain HTTP + SSE (POST /v1/messages).
//
// Request shape (API version 2026-10-21): adaptive thinking, effort in
// output_config, no sampling parameters (they are a 400 on this version),
// tool_choice auto|none (forced tool use is a 400 on current models), an
// explicit cache breakpoint on the static system block plus top-level
// automatic caching for the growing conversation, eager input streaming on
// every tool (inputs are validated server-side before a tool runs), the
// server-side refusal fallback, and thinking.block_binding drop_block so a
// changed system prompt or tool list (a deploy) degrades instead of failing
// on replayed thinking blocks.

const (
	anthropicVersion        = "2026-10-21"
	anthropicBetaFallback   = "server-side-fallback-2026-07-01"
	anthropicBetaThinkBind  = "thinking-binding-controls-2026-08-01"
	anthropicDefaultBaseURL = "https://api.anthropic.com"
)

type anthropicProvider struct {
	key       string
	base      string
	fallbacks bool
	http      *http.Client
	retry     retrier
}

func newAnthropic(key, base string, fallbacks bool) *anthropicProvider {
	return &anthropicProvider{key: key, base: strings.TrimRight(base, "/"), fallbacks: fallbacks,
		http: newHTTPClient(), retry: newRetrier()}
}

func (p *anthropicProvider) Name() string  { return "anthropic" }
func (p *anthropicProvider) Label() string { return providerLabels["anthropic"] }

// ---- request ---------------------------------------------------------------

type antCache struct {
	Type string `json:"type"`
}

type antSystem struct {
	Type         string    `json:"type"`
	Text         string    `json:"text"`
	CacheControl *antCache `json:"cache_control,omitempty"`
}

type antMessage struct {
	Role    string            `json:"role"`
	Content []json.RawMessage `json:"content"`
}

type antTool struct {
	Name                string          `json:"name"`
	Description         string          `json:"description"`
	InputSchema         json.RawMessage `json:"input_schema"`
	EagerInputStreaming bool            `json:"eager_input_streaming,omitempty"`
}

type antThinking struct {
	Type         string           `json:"type"`
	BlockBinding *antBlockBinding `json:"block_binding,omitempty"`
}

type antBlockBinding struct {
	PrefixMismatchBehavior string `json:"prefix_mismatch_behavior"`
}

type antRequest struct {
	Model        string       `json:"model"`
	MaxTokens    int          `json:"max_tokens"`
	System       []antSystem  `json:"system,omitempty"`
	Messages     []antMessage `json:"messages"`
	Tools        []antTool    `json:"tools,omitempty"`
	ToolChoice   *antChoice   `json:"tool_choice,omitempty"`
	Thinking     *antThinking `json:"thinking,omitempty"`
	OutputConfig *antOutput   `json:"output_config,omitempty"`
	Metadata     *antMetadata `json:"metadata,omitempty"`
	CacheControl *antCache    `json:"cache_control,omitempty"`
	Fallbacks    string       `json:"fallbacks,omitempty"`
	Stream       bool         `json:"stream,omitempty"`
}

type antChoice struct {
	Type string `json:"type"`
}

type antOutput struct {
	Effort string `json:"effort,omitempty"`
}

type antMetadata struct {
	UserID string `json:"user_id,omitempty"`
}

func mustRaw(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// build renders req; it returns the body and the beta headers it needs.
func (p *anthropicProvider) build(req Request, stream bool) ([]byte, []string, error) {
	if req.Model == "" {
		return nil, nil, errors.New("assistant: request without a model")
	}
	w := antRequest{Model: req.Model, MaxTokens: req.MaxTokens, Stream: stream, Messages: []antMessage{}}
	if w.MaxTokens <= 0 {
		w.MaxTokens = 16000
	}
	for _, s := range req.System {
		b := antSystem{Type: "text", Text: s.Text}
		if s.Cache {
			b.CacheControl = &antCache{Type: "ephemeral"}
		}
		w.System = append(w.System, b)
	}
	for _, t := range req.Tools {
		w.Tools = append(w.Tools, antTool{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema,
			EagerInputStreaming: stream})
	}
	if len(w.Tools) > 0 {
		choice := "auto"
		if req.ToolChoice == "none" {
			choice = "none"
		}
		w.ToolChoice = &antChoice{Type: choice}
	}
	if req.Effort != "" {
		w.OutputConfig = &antOutput{Effort: req.Effort}
	}
	if req.UserHash != "" {
		w.Metadata = &antMetadata{UserID: req.UserHash}
	}
	var betas []string
	if !req.Utility {
		// Automatic caching for the conversation tail (the explicit marker on
		// the static system block covers tools + system).
		w.CacheControl = &antCache{Type: "ephemeral"}
		w.Thinking = &antThinking{Type: "adaptive", BlockBinding: &antBlockBinding{PrefixMismatchBehavior: "drop_block"}}
		betas = append(betas, anthropicBetaThinkBind)
		if p.fallbacks {
			w.Fallbacks = "default"
			betas = append(betas, anthropicBetaFallback)
		}
	}
	w.Messages = anthropicMessages(req.Messages)
	b, err := json.Marshal(w)
	return b, betas, err
}

// anthropicMessages maps the neutral transcript, merging consecutive turns of
// the same role (the API wants alternation) and dropping empty ones.
func anthropicMessages(msgs []Message) []antMessage {
	out := []antMessage{}
	for _, m := range msgs {
		var content []json.RawMessage
		for _, part := range m.Parts {
			switch part.Type {
			case PartText:
				if strings.TrimSpace(part.Text) == "" {
					continue
				}
				content = append(content, mustRaw(map[string]any{"type": "text", "text": part.Text}))
			case PartReasoning:
				// Thinking blocks go back unchanged; the API drops, unbilled,
				// any block the current model can't read.
				if part.Provider == "anthropic" && len(part.Raw) > 0 && m.Role == RoleAssistant {
					content = append(content, part.Raw)
				}
			case PartToolCall:
				input := part.Input
				if len(input) == 0 || !json.Valid(input) {
					input = json.RawMessage(`{}`)
				}
				content = append(content, mustRaw(map[string]any{"type": "tool_use", "id": part.CallID, "name": part.Name, "input": input}))
			case PartToolResult:
				content = append(content, mustRaw(map[string]any{"type": "tool_result", "tool_use_id": part.CallID,
					"content": part.Content, "is_error": part.IsError}))
			}
		}
		if len(content) == 0 {
			continue
		}
		role := m.Role
		if role != RoleAssistant {
			role = RoleUser
		}
		if n := len(out); n > 0 && out[n-1].Role == role {
			out[n-1].Content = append(out[n-1].Content, content...)
			continue
		}
		out = append(out, antMessage{Role: role, Content: content})
	}
	return out
}

func (p *anthropicProvider) newRequest(ctx context.Context, body []byte, betas []string, stream bool) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.base+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", p.key)
	req.Header.Set("anthropic-version", anthropicVersion)
	req.Header.Set("content-type", "application/json")
	if len(betas) > 0 {
		req.Header.Set("anthropic-beta", strings.Join(betas, ","))
	}
	if stream {
		req.Header.Set("accept", "text/event-stream")
	}
	return req, nil
}

// ---- calls -----------------------------------------------------------------

func (p *anthropicProvider) Stream(ctx context.Context, req Request, on func(Event)) (*Response, error) {
	body, betas, err := p.build(req, true)
	if err != nil {
		return nil, err
	}
	var resp *Response
	err = p.retry.do(ctx, p.Name(), req.Model, func() (bool, error) {
		r, delivered, err := p.streamOnce(ctx, req.Model, body, betas, on)
		resp = r
		return delivered, err
	})
	return resp, err
}

func (p *anthropicProvider) streamOnce(ctx context.Context, model string, body []byte, betas []string, on func(Event)) (*Response, bool, error) {
	reqCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	hreq, err := p.newRequest(reqCtx, body, betas, true)
	if err != nil {
		return nil, false, err
	}
	res, err := openStream(ctx, p.http, hreq, cancel)
	if err != nil {
		return nil, false, err
	}
	defer res.Body.Close()
	ir := newIdleReader(res.Body, streamIdleTimeout, cancel)
	defer ir.stop()
	resp, delivered, err := decodeAnthropicStream(ir, model, on)
	if err != nil && ctx.Err() != nil {
		return resp, delivered, ctx.Err()
	}
	return resp, delivered, err
}

func (p *anthropicProvider) Complete(ctx context.Context, req Request) (*Response, error) {
	body, betas, err := p.build(req, false)
	if err != nil {
		return nil, err
	}
	var resp *Response
	err = p.retry.do(ctx, p.Name(), req.Model, func() (bool, error) {
		hreq, err := p.newRequest(ctx, body, betas, false)
		if err != nil {
			return false, err
		}
		raw, err := postJSON(ctx, p.http, hreq)
		if err != nil {
			return false, err
		}
		var msg struct {
			Model      string            `json:"model"`
			Content    []json.RawMessage `json:"content"`
			StopReason string            `json:"stop_reason"`
			Usage      antUsage          `json:"usage"`
		}
		if err := json.Unmarshal(raw, &msg); err != nil {
			return false, &providerError{Type: "bad_response", Message: "malformed response: " + err.Error()}
		}
		resp = &Response{Model: msg.Model, Stop: anthropicStop(msg.StopReason), Usage: msg.Usage.usage(), UsageKnown: true}
		for _, c := range msg.Content {
			var head struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			if json.Unmarshal(c, &head) == nil && head.Type == "text" {
				resp.Parts = append(resp.Parts, Part{Type: PartText, Text: head.Text})
			}
		}
		return false, nil
	})
	return resp, err
}

// ---- stream decoding -------------------------------------------------------

type antUsage struct {
	InputTokens              int `json:"input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	Iterations               []struct {
		InputTokens              int `json:"input_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		OutputTokens             int `json:"output_tokens"`
	} `json:"iterations"`
}

// usage normalises Anthropic usage. With a server-side fallback the top-level
// figures cover only the attempt that produced the message; iterations hold
// every billed attempt, so they win when present.
func (u antUsage) usage() Usage {
	if len(u.Iterations) > 0 {
		var t Usage
		for _, it := range u.Iterations {
			t.Add(Usage{InputTokens: it.InputTokens, CacheReadTokens: it.CacheReadInputTokens,
				CacheWriteTokens: it.CacheCreationInputTokens, OutputTokens: it.OutputTokens})
		}
		return t
	}
	return Usage{InputTokens: u.InputTokens, CacheReadTokens: u.CacheReadInputTokens,
		CacheWriteTokens: u.CacheCreationInputTokens, OutputTokens: u.OutputTokens}
}

func anthropicStop(s string) string {
	switch s {
	case "end_turn", "stop_sequence", "pause_turn":
		return StopEndTurn
	case "tool_use":
		return StopToolUse
	case "max_tokens", "model_context_window_exceeded":
		return StopMaxTokens
	case "refusal":
		return StopRefusal
	case "":
		return StopOther
	}
	return StopOther
}

type antEvent struct {
	Type         string          `json:"type"`
	Index        int             `json:"index"`
	Message      json.RawMessage `json:"message"`
	ContentBlock json.RawMessage `json:"content_block"`
	Delta        json.RawMessage `json:"delta"`
	Usage        *antUsage       `json:"usage"`
	Error        *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// antBlock accumulates one streamed content block.
type antBlock struct {
	index     int
	typ       string
	startRaw  json.RawMessage // content_block as received
	start     map[string]json.RawMessage
	text      strings.Builder
	thinking  strings.Builder
	signature strings.Builder
	input     strings.Builder
	citations []json.RawMessage
}

type antStream struct {
	model     string
	blocks    map[int]*antBlock
	done      []Part // finished parts by stream order (index order within a message)
	stop      string
	usage     antUsage
	gotUsage  bool
	delivered bool
}

// decodeAnthropicStream reads the Messages SSE stream until message_stop. It
// returns the (possibly partial) response, whether any text reached on (a
// retry is only safe before that) and a providerError for error events or a
// truncated stream.
func decodeAnthropicStream(r io.Reader, model string, on func(Event)) (*Response, bool, error) {
	st := &antStream{model: model, blocks: map[int]*antBlock{}}
	sr := newSSEReader(r)
	for {
		frame, err := sr.Next()
		if err != nil {
			msg := "stream ended before the message completed"
			if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
				msg = err.Error()
			}
			return st.response(), st.delivered, &providerError{Transport: true, Message: msg}
		}
		data := strings.TrimSpace(frame.Data)
		if data == "" {
			continue
		}
		var ev antEvent
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			return st.response(), st.delivered, &providerError{Type: "bad_event", Message: "malformed stream event: " + err.Error()}
		}
		if ev.Type == "" {
			ev.Type = frame.Event
		}
		switch ev.Type {
		case "message_start":
			var m struct {
				Model string   `json:"model"`
				Usage antUsage `json:"usage"`
			}
			if json.Unmarshal(ev.Message, &m) == nil {
				if m.Model != "" {
					st.model = m.Model
				}
				st.usage, st.gotUsage = m.Usage, true
			}
		case "content_block_start":
			b := &antBlock{index: ev.Index, startRaw: ev.ContentBlock, start: map[string]json.RawMessage{}}
			_ = json.Unmarshal(ev.ContentBlock, &b.start)
			_ = json.Unmarshal(b.start["type"], &b.typ)
			st.blocks[ev.Index] = b
		case "content_block_delta":
			b := st.blocks[ev.Index]
			if b == nil {
				continue
			}
			var d struct {
				Type        string          `json:"type"`
				Text        string          `json:"text"`
				Thinking    string          `json:"thinking"`
				Signature   string          `json:"signature"`
				PartialJSON string          `json:"partial_json"`
				Citation    json.RawMessage `json:"citation"`
			}
			if json.Unmarshal(ev.Delta, &d) != nil {
				continue
			}
			switch d.Type {
			case "text_delta":
				b.text.WriteString(d.Text)
				if d.Text != "" {
					st.delivered = true
					if on != nil {
						on(Event{Type: EventText, Text: d.Text})
					}
				}
			case "thinking_delta":
				b.thinking.WriteString(d.Thinking)
			case "signature_delta":
				b.signature.WriteString(d.Signature)
			case "input_json_delta":
				b.input.WriteString(d.PartialJSON)
			case "citations_delta":
				if len(d.Citation) > 0 {
					b.citations = append(b.citations, d.Citation)
				}
			}
		case "content_block_stop":
			if b := st.blocks[ev.Index]; b != nil {
				st.finish(b)
				delete(st.blocks, ev.Index)
			}
		case "message_delta":
			var d struct {
				StopReason string `json:"stop_reason"`
			}
			if json.Unmarshal(ev.Delta, &d) == nil && d.StopReason != "" {
				st.stop = d.StopReason
			}
			if ev.Usage != nil {
				st.mergeUsage(*ev.Usage)
			}
		case "message_stop":
			return st.response(), st.delivered, nil
		case "error":
			pe := &providerError{Type: "api_error", Message: "stream error"}
			if ev.Error != nil {
				pe.Type, pe.Message = ev.Error.Type, ev.Error.Message
			}
			return st.response(), st.delivered, pe
		}
	}
}

// mergeUsage folds a message_delta usage into the running totals: output is
// cumulative; input fields appear again after a mid-stream fallback.
func (st *antStream) mergeUsage(u antUsage) {
	st.gotUsage = true
	if u.OutputTokens > 0 {
		st.usage.OutputTokens = u.OutputTokens
	}
	if u.InputTokens > 0 {
		st.usage.InputTokens = u.InputTokens
	}
	if u.CacheCreationInputTokens > 0 {
		st.usage.CacheCreationInputTokens = u.CacheCreationInputTokens
	}
	if u.CacheReadInputTokens > 0 {
		st.usage.CacheReadInputTokens = u.CacheReadInputTokens
	}
	if len(u.Iterations) > 0 {
		st.usage.Iterations = u.Iterations
	}
}

// finish turns a completed block into a Part.
func (st *antStream) finish(b *antBlock) {
	switch b.typ {
	case "text":
		st.done = append(st.done, Part{Type: PartText, Text: b.text.String()})
	case "thinking":
		// Rebuilt in the API's field order; replayed byte for byte from here on.
		raw := mustRaw(struct {
			Type      string `json:"type"`
			Thinking  string `json:"thinking"`
			Signature string `json:"signature"`
		}{"thinking", b.thinking.String(), b.signature.String()})
		st.done = append(st.done, Part{Type: PartReasoning, Provider: "anthropic", Model: st.model, Raw: raw})
	case "redacted_thinking":
		st.done = append(st.done, Part{Type: PartReasoning, Provider: "anthropic", Model: st.model, Raw: compactRaw(b.startRaw)})
	case "tool_use":
		var id, name string
		_ = json.Unmarshal(b.start["id"], &id)
		_ = json.Unmarshal(b.start["name"], &name)
		part := Part{Type: PartToolCall, CallID: id, Name: name}
		in := strings.TrimSpace(b.input.String())
		switch {
		case in == "":
			part.Input = json.RawMessage(`{}`)
		case json.Valid([]byte(in)):
			part.Input = json.RawMessage(in)
		default:
			part.Input, part.IsError, part.Text = json.RawMessage(`{}`), true, in
		}
		st.done = append(st.done, part)
	case "fallback":
		// A refusal handed the turn to a fallback model on the same stream.
		// Text already streamed stays; thinking and tool calls produced
		// before the switch must not be echoed back (or run).
		var to struct {
			Model string `json:"model"`
		}
		if json.Unmarshal(b.start["to"], &to) == nil && to.Model != "" {
			st.model = to.Model
		}
		kept := st.done[:0]
		for _, p := range st.done {
			if p.Type == PartText {
				kept = append(kept, p)
			}
		}
		st.done = kept
	default:
		// Unknown model-internal block: keep it for replay to Anthropic only.
		st.done = append(st.done, Part{Type: PartReasoning, Provider: "anthropic", Model: st.model, Raw: compactRaw(b.startRaw)})
	}
}

func compactRaw(raw json.RawMessage) json.RawMessage {
	var buf bytes.Buffer
	if json.Compact(&buf, raw) != nil {
		return raw
	}
	return buf.Bytes()
}

// response builds the Response from what has streamed so far. Blocks still
// open (a broken stream) contribute their text only.
func (st *antStream) response() *Response {
	parts := append([]Part(nil), st.done...)
	open := make([]*antBlock, 0, len(st.blocks))
	for _, b := range st.blocks {
		open = append(open, b)
	}
	sort.Slice(open, func(i, j int) bool { return open[i].index < open[j].index })
	for _, b := range open {
		if b.typ == "text" && b.text.Len() > 0 {
			parts = append(parts, Part{Type: PartText, Text: b.text.String()})
		}
	}
	return &Response{Model: st.model, Parts: parts, Stop: anthropicStop(st.stop), Usage: st.usage.usage(), UsageKnown: st.gotUsage}
}
