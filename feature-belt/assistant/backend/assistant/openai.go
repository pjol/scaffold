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

// OpenAI Responses API over plain HTTP + SSE (POST /v1/responses), distilled
// from ChatMTG's client: store=false with encrypted reasoning carried in the
// replayed items, the static system block sent as a developer message with
// an explicit prompt-cache breakpoint, a per-user prompt_cache_key and a
// hashed safety_identifier. There is deliberately no temperature or top_p.

const openAIDefaultBaseURL = "https://api.openai.com/v1"

type openAIProvider struct {
	key   string
	base  string
	http  *http.Client
	retry retrier
}

func newOpenAI(key, base string) *openAIProvider {
	return &openAIProvider{key: key, base: strings.TrimRight(base, "/"), http: newHTTPClient(), retry: newRetrier()}
}

func (p *openAIProvider) Name() string  { return "openai" }
func (p *openAIProvider) Label() string { return providerLabels["openai"] }

// ---- request ---------------------------------------------------------------

type oaContent struct {
	Type            string        `json:"type"`
	Text            string        `json:"text"`
	CacheBreakpoint *oaBreakpoint `json:"prompt_cache_breakpoint,omitempty"`
}

type oaBreakpoint struct {
	Mode string `json:"mode"`
}

type oaMessage struct {
	Type    string      `json:"type"`
	Role    string      `json:"role"`
	Content []oaContent `json:"content"`
}

type oaTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
	Strict      bool            `json:"strict"`
}

type oaReasoning struct {
	Effort string `json:"effort,omitempty"`
}

type oaCacheOptions struct {
	Mode string `json:"mode"`
}

type oaRequest struct {
	Model              string            `json:"model"`
	Input              []json.RawMessage `json:"input"`
	Tools              []oaTool          `json:"tools,omitempty"`
	ToolChoice         string            `json:"tool_choice,omitempty"`
	ParallelToolCalls  *bool             `json:"parallel_tool_calls,omitempty"`
	Reasoning          *oaReasoning      `json:"reasoning,omitempty"`
	MaxOutputTokens    int               `json:"max_output_tokens,omitempty"`
	Store              bool              `json:"store"`
	Stream             bool              `json:"stream,omitempty"`
	Include            []string          `json:"include,omitempty"`
	PromptCacheKey     string            `json:"prompt_cache_key,omitempty"`
	PromptCacheOptions *oaCacheOptions   `json:"prompt_cache_options,omitempty"`
	SafetyIdentifier   string            `json:"safety_identifier,omitempty"`
}

func (p *openAIProvider) build(req Request, stream bool) ([]byte, error) {
	if req.Model == "" {
		return nil, errors.New("assistant: request without a model")
	}
	w := oaRequest{Model: req.Model, MaxOutputTokens: req.MaxTokens, Store: false, Stream: stream,
		Include: []string{"reasoning.encrypted_content"}}
	for _, s := range req.System {
		c := oaContent{Type: "input_text", Text: s.Text}
		if s.Cache {
			c.CacheBreakpoint = &oaBreakpoint{Mode: "explicit"}
		}
		w.Input = append(w.Input, mustRaw(oaMessage{Type: "message", Role: "developer", Content: []oaContent{c}}))
	}
	w.Input = append(w.Input, openAIItems(req.Messages, req.Model)...)
	for _, t := range req.Tools {
		w.Tools = append(w.Tools, oaTool{Type: "function", Name: t.Name, Description: t.Description, Parameters: t.InputSchema})
	}
	if len(w.Tools) > 0 {
		w.ToolChoice = "auto"
		if req.ToolChoice == "none" {
			w.ToolChoice = "none"
		}
		parallel := true
		w.ParallelToolCalls = &parallel
	}
	if req.Effort != "" {
		w.Reasoning = &oaReasoning{Effort: req.Effort}
	}
	if req.UserHash != "" {
		w.PromptCacheKey = "scaffoldapp:" + req.UserHash
		w.SafetyIdentifier = "u_" + req.UserHash
		w.PromptCacheOptions = &oaCacheOptions{Mode: "implicit"}
	}
	return json.Marshal(w)
}

// openAIItems maps the neutral transcript to Responses input items. Raw
// items from this provider and model replay verbatim (reasoning must stay
// next to the item it belongs to); anything else is rebuilt without item ids
// and reasoning from other models is dropped.
func openAIItems(msgs []Message, model string) []json.RawMessage {
	out := []json.RawMessage{}
	for _, m := range msgs {
		var userText []oaContent
		flushUser := func() {
			if len(userText) > 0 {
				out = append(out, mustRaw(oaMessage{Type: "message", Role: "user", Content: userText}))
				userText = nil
			}
		}
		for _, part := range m.Parts {
			native := part.Provider == "openai" && part.Model == model && len(part.Raw) > 0
			switch part.Type {
			case PartText:
				if strings.TrimSpace(part.Text) == "" {
					continue
				}
				if m.Role == RoleAssistant {
					if native {
						out = append(out, part.Raw)
					} else {
						out = append(out, mustRaw(oaMessage{Type: "message", Role: "assistant",
							Content: []oaContent{{Type: "output_text", Text: part.Text}}}))
					}
					continue
				}
				userText = append(userText, oaContent{Type: "input_text", Text: part.Text})
			case PartReasoning:
				if native && m.Role == RoleAssistant {
					out = append(out, part.Raw)
				}
			case PartToolCall:
				if native {
					out = append(out, part.Raw)
					continue
				}
				args := string(part.Input)
				if args == "" || !json.Valid(part.Input) {
					args = "{}"
				}
				out = append(out, mustRaw(map[string]string{"type": "function_call", "call_id": part.CallID,
					"name": part.Name, "arguments": args}))
			case PartToolResult:
				flushUser()
				out = append(out, mustRaw(map[string]string{"type": "function_call_output", "call_id": part.CallID,
					"output": part.Content}))
			}
		}
		flushUser()
	}
	return out
}

func (p *openAIProvider) newRequest(ctx context.Context, body []byte, stream bool) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.base+"/responses", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.key)
	req.Header.Set("Content-Type", "application/json")
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	return req, nil
}

// ---- calls -----------------------------------------------------------------

func (p *openAIProvider) Stream(ctx context.Context, req Request, on func(Event)) (*Response, error) {
	body, err := p.build(req, true)
	if err != nil {
		return nil, err
	}
	var resp *Response
	err = p.retry.do(ctx, p.Name(), req.Model, func() (bool, error) {
		reqCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		hreq, err := p.newRequest(reqCtx, body, true)
		if err != nil {
			return false, err
		}
		res, err := openStream(ctx, p.http, hreq, cancel)
		if err != nil {
			return false, err
		}
		defer res.Body.Close()
		ir := newIdleReader(res.Body, streamIdleTimeout, cancel)
		defer ir.stop()
		r, delivered, err := decodeOpenAIStream(ir, req.Model, on)
		resp = r
		if err != nil && ctx.Err() != nil {
			return delivered, ctx.Err()
		}
		return delivered, err
	})
	return resp, err
}

func (p *openAIProvider) Complete(ctx context.Context, req Request) (*Response, error) {
	body, err := p.build(req, false)
	if err != nil {
		return nil, err
	}
	var resp *Response
	err = p.retry.do(ctx, p.Name(), req.Model, func() (bool, error) {
		hreq, err := p.newRequest(ctx, body, false)
		if err != nil {
			return false, err
		}
		raw, err := postJSON(ctx, p.http, hreq)
		if err != nil {
			return false, err
		}
		var w oaResponse
		if err := json.Unmarshal(raw, &w); err != nil {
			return false, &providerError{Type: "bad_response", Message: "malformed response: " + err.Error()}
		}
		if w.Status == "failed" {
			pe := &providerError{Code: "server_error", Message: "response failed"}
			if w.Error != nil {
				pe.Type, pe.Code, pe.Message = w.Error.Type, rawCode(w.Error.Code), w.Error.Message
			}
			return false, pe
		}
		resp = w.response(req.Model)
		return false, nil
	})
	return resp, err
}

// ---- decoding --------------------------------------------------------------

type oaUsage struct {
	InputTokens        int `json:"input_tokens"`
	InputTokensDetails struct {
		CachedTokens     int `json:"cached_tokens"`
		CacheWriteTokens int `json:"cache_write_tokens"`
	} `json:"input_tokens_details"`
	OutputTokens int `json:"output_tokens"`
}

func (u *oaUsage) usage() Usage {
	if u == nil {
		return Usage{}
	}
	cached, written := u.InputTokensDetails.CachedTokens, u.InputTokensDetails.CacheWriteTokens
	return Usage{InputTokens: max(0, u.InputTokens-cached-written), CacheReadTokens: cached,
		CacheWriteTokens: written, OutputTokens: u.OutputTokens}
}

type oaError struct {
	Type    string          `json:"type"`
	Code    json.RawMessage `json:"code"`
	Message string          `json:"message"`
}

type oaResponse struct {
	ID                string            `json:"id"`
	Model             string            `json:"model"`
	Status            string            `json:"status"`
	Output            []json.RawMessage `json:"output"`
	Usage             *oaUsage          `json:"usage"`
	Error             *oaError          `json:"error"`
	IncompleteDetails *struct {
		Reason string `json:"reason"`
	} `json:"incomplete_details"`
}

// response converts a response object; model is the requested model (the
// replay key for raw items).
func (w *oaResponse) response(model string) *Response {
	parts, refused := openAIParts(w.Output, model)
	r := &Response{Model: model, Parts: parts, Usage: w.Usage.usage(), UsageKnown: w.Usage != nil}
	reason := ""
	if w.IncompleteDetails != nil {
		reason = w.IncompleteDetails.Reason
	}
	switch {
	case refused, reason == "content_filter":
		r.Stop = StopRefusal
	case w.Status == "incomplete" && reason == "max_output_tokens":
		r.Stop = StopMaxTokens
	case w.Status == "incomplete":
		r.Stop = StopOther
	case len(r.ToolCalls()) > 0:
		r.Stop = StopToolUse
	default:
		r.Stop = StopEndTurn
	}
	return r
}

// openAIParts maps output items to parts, reporting whether a refusal part
// was present.
func openAIParts(items []json.RawMessage, model string) ([]Part, bool) {
	var parts []Part
	refused := false
	for _, raw := range items {
		var item struct {
			Type      string `json:"type"`
			Role      string `json:"role"`
			CallID    string `json:"call_id"`
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
			Content   []struct {
				Type    string `json:"type"`
				Text    string `json:"text"`
				Refusal string `json:"refusal"`
			} `json:"content"`
		}
		if json.Unmarshal(raw, &item) != nil {
			continue
		}
		switch item.Type {
		case "reasoning":
			parts = append(parts, Part{Type: PartReasoning, Provider: "openai", Model: model, Raw: raw})
		case "message":
			var b strings.Builder
			for _, c := range item.Content {
				switch c.Type {
				case "output_text":
					b.WriteString(c.Text)
				case "refusal":
					refused = true
				}
			}
			parts = append(parts, Part{Type: PartText, Text: b.String(), Provider: "openai", Model: model, Raw: raw})
		case "function_call":
			p := Part{Type: PartToolCall, CallID: item.CallID, Name: item.Name, Provider: "openai", Model: model, Raw: raw}
			args := strings.TrimSpace(item.Arguments)
			switch {
			case args == "":
				p.Input = json.RawMessage(`{}`)
			case json.Valid([]byte(args)):
				p.Input = json.RawMessage(args)
			default:
				p.Input, p.IsError, p.Text = json.RawMessage(`{}`), true, args
			}
			parts = append(parts, p)
		}
	}
	return parts, refused
}

type oaEvent struct {
	Type        string          `json:"type"`
	Delta       string          `json:"delta"`
	Item        json.RawMessage `json:"item"`
	OutputIndex *int            `json:"output_index"`
	Response    json.RawMessage `json:"response"`
	Code        json.RawMessage `json:"code"`
	Message     string          `json:"message"`
	Error       *oaError        `json:"error"`
}

// decodeOpenAIStream reads a Responses SSE stream until a terminal event.
func decodeOpenAIStream(r io.Reader, model string, on func(Event)) (*Response, bool, error) {
	items := map[int]json.RawMessage{}
	var text strings.Builder
	delivered := false
	partial := func() *Response {
		idx := make([]int, 0, len(items))
		for i := range items {
			idx = append(idx, i)
		}
		sort.Ints(idx)
		out := make([]json.RawMessage, 0, len(idx))
		for _, i := range idx {
			out = append(out, items[i])
		}
		parts, _ := openAIParts(out, model)
		resp := &Response{Model: model, Parts: parts, Stop: StopOther}
		if len(parts) == 0 && text.Len() > 0 {
			resp.Parts = []Part{{Type: PartText, Text: text.String()}}
		}
		return resp
	}
	sr := newSSEReader(r)
	for {
		frame, err := sr.Next()
		if err != nil {
			msg := "stream ended before the response completed"
			if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
				msg = err.Error()
			}
			return partial(), delivered, &providerError{Transport: true, Message: msg}
		}
		data := strings.TrimSpace(frame.Data)
		if data == "" || data == "[DONE]" {
			continue
		}
		var ev oaEvent
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			return partial(), delivered, &providerError{Type: "bad_event", Message: "malformed stream event: " + err.Error()}
		}
		if ev.Type == "" {
			ev.Type = frame.Event
		}
		switch ev.Type {
		case "response.output_text.delta":
			text.WriteString(ev.Delta)
			if ev.Delta != "" {
				delivered = true
				if on != nil {
					on(Event{Type: EventText, Text: ev.Delta})
				}
			}
		case "response.output_item.done":
			if ev.OutputIndex != nil && len(ev.Item) > 0 {
				items[*ev.OutputIndex] = ev.Item
			}
		case "response.completed", "response.incomplete":
			var w oaResponse
			if err := json.Unmarshal(ev.Response, &w); err != nil {
				return partial(), delivered, &providerError{Type: "bad_event", Message: "malformed " + ev.Type}
			}
			if len(w.Output) == 0 && len(items) > 0 {
				p := partial()
				w.Output = nil
				resp := w.response(model)
				resp.Parts = p.Parts
				if len(resp.ToolCalls()) > 0 && resp.Stop == StopEndTurn {
					resp.Stop = StopToolUse
				}
				return resp, delivered, nil
			}
			if w.Status == "" {
				w.Status = map[string]string{"response.completed": "completed", "response.incomplete": "incomplete"}[ev.Type]
			}
			return w.response(model), delivered, nil
		case "response.failed":
			var w oaResponse
			_ = json.Unmarshal(ev.Response, &w)
			resp := partial()
			resp.Usage, resp.UsageKnown = w.Usage.usage(), w.Usage != nil
			pe := &providerError{Code: "server_error", Message: "response failed"}
			if w.Error != nil {
				pe.Type, pe.Code, pe.Message = w.Error.Type, rawCode(w.Error.Code), w.Error.Message
			}
			return resp, delivered, pe
		case "error":
			pe := &providerError{Code: rawCode(ev.Code), Message: ev.Message}
			if ev.Error != nil {
				pe.Type, pe.Message = ev.Error.Type, ev.Error.Message
				if c := rawCode(ev.Error.Code); c != "" {
					pe.Code = c
				}
			}
			return partial(), delivered, pe
		}
	}
}
