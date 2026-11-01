package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// mockProvider is the development-only scripted provider
// (ASSISTANT_PROVIDER=mock). It calls no AI service: replies are fixed text
// that says so, streamed word by word so the client's streaming, tool
// activity and stop paths can be exercised without a key. New refuses it
// outside APP_ENV=development.
type mockProvider struct{}

func (mockProvider) Name() string  { return "mock" }
func (mockProvider) Label() string { return providerLabels["mock"] }

const mockPrefix = "[Development mock] "

func (m mockProvider) Stream(ctx context.Context, req Request, on func(Event)) (*Response, error) {
	resp := m.plan(req)
	text := resp.Text()
	pace := 15 * time.Millisecond
	if len(text) > 1000 {
		pace = 40 * time.Millisecond
	}
	var sent strings.Builder
	for _, word := range strings.SplitAfter(text, " ") {
		select {
		case <-ctx.Done():
			return &Response{Model: req.Model, Parts: []Part{{Type: PartText, Text: sent.String()}}, Stop: StopOther,
				Usage: estimateUsage(req, sent.String()), UsageKnown: true}, ctx.Err()
		case <-time.After(pace):
		}
		if on != nil && word != "" {
			on(Event{Type: EventText, Text: word})
			sent.WriteString(word)
		}
	}
	return resp, nil
}

func (m mockProvider) Complete(_ context.Context, req Request) (*Response, error) {
	last := lastUserText(req.Messages)
	out := "Summary of the earlier conversation."
	if len(req.System) > 0 && strings.Contains(req.System[0].Text, "title") {
		words := strings.Fields(last)
		out = strings.Join(words[:min(4, len(words))], " ")
		if out == "" {
			out = "New conversation"
		}
	}
	return &Response{Model: req.Model, Parts: []Part{{Type: PartText, Text: out}}, Stop: StopEndTurn,
		Usage: estimateUsage(req, out), UsageKnown: true}, nil
}

// plan decides the scripted reply: after tool results, a short report; when
// the user mentions usage and the usage tool is offered, a tool call;
// otherwise a fixed reply quoting the start of the message.
func (m mockProvider) plan(req Request) *Response {
	resp := &Response{Model: req.Model, Stop: StopEndTurn, UsageKnown: true}
	if n := len(req.Messages); n > 0 {
		lastMsg := req.Messages[n-1]
		var results []Part
		for _, p := range lastMsg.Parts {
			if p.Type == PartToolResult {
				results = append(results, p)
			}
		}
		if len(results) > 0 {
			var names []string
			for _, r := range results {
				status := "returned data"
				if r.IsError {
					status = "failed"
				}
				names = append(names, fmt.Sprintf("%s %s", r.Name, status))
			}
			text := mockPrefix + "Tool results: " + strings.Join(names, "; ") + ". This reply is scripted; no AI provider was called."
			resp.Parts = []Part{{Type: PartText, Text: text}}
			resp.Usage = estimateUsage(req, text)
			return resp
		}
	}
	last := lastUserText(req.Messages)
	if req.ToolChoice != "none" {
		// "call <tool_name> {json}" calls that tool when it is offered, so
		// development can exercise any tool (connected services included)
		// without a model.
		if m := mockCallRE.FindStringSubmatch(last); m != nil {
			for _, t := range req.Tools {
				if !strings.EqualFold(t.Name, m[1]) {
					continue
				}
				input := json.RawMessage(`{}`)
				if raw := strings.TrimSpace(m[2]); raw != "" && json.Valid([]byte(raw)) {
					input = json.RawMessage(raw)
				}
				resp.Parts = []Part{
					{Type: PartText, Text: mockPrefix + "Calling " + t.Name + ". "},
					{Type: PartToolCall, CallID: fmt.Sprintf("mock_%d", time.Now().UnixNano()), Name: t.Name, Input: input},
				}
				resp.Stop = StopToolUse
				resp.Usage = estimateUsage(req, "Calling "+t.Name)
				return resp
			}
		}
	}
	if req.ToolChoice != "none" && strings.Contains(strings.ToLower(last), "usage") {
		for _, t := range req.Tools {
			if t.Name == "assistant_get_usage" {
				resp.Parts = []Part{
					{Type: PartText, Text: mockPrefix + "Checking your usage. "},
					{Type: PartToolCall, CallID: fmt.Sprintf("mock_%d", time.Now().UnixNano()), Name: t.Name, Input: json.RawMessage(`{}`)},
				}
				resp.Stop = StopToolUse
				resp.Usage = estimateUsage(req, "Checking your usage.")
				return resp
			}
		}
	}
	if strings.Contains(strings.ToLower(last), "long reply") {
		// A slow, long reply for exercising stop and resume.
		var b strings.Builder
		b.WriteString(mockPrefix + "This is a long scripted reply for testing stop and resume.")
		for i := 1; i <= 12; i++ {
			fmt.Fprintf(&b, "\n\nParagraph %d. Nothing here comes from a model; the words arrive one at a time so a client can stop the reply or reconnect while it is still streaming.", i)
		}
		resp.Parts = []Part{{Type: PartText, Text: b.String()}}
		resp.Usage = estimateUsage(req, b.String())
		return resp
	}
	quote := last
	if utf8.RuneCountInString(quote) > 80 {
		quote = string([]rune(quote)[:80]) + "…"
	}
	text := mockPrefix + "This reply is scripted; no AI provider was called. You wrote: \"" + quote +
		"\". Set ASSISTANT_PROVIDER and an API key to talk to a real model."
	resp.Parts = []Part{{Type: PartText, Text: text}}
	resp.Usage = estimateUsage(req, text)
	return resp
}

var mockCallRE = regexp.MustCompile(`(?is)\bcall\s+([a-zA-Z0-9_-]{1,64})\s*(\{.*\})?\s*$`)

// lastUserText returns the text of the latest user message without fences.
func lastUserText(msgs []Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != RoleUser {
			continue
		}
		var b strings.Builder
		for _, p := range msgs[i].Parts {
			if p.Type == PartText && strings.HasPrefix(p.Text, fenceOpen) {
				b.WriteString(unfence(p.Text))
			}
		}
		if b.Len() > 0 {
			return b.String()
		}
	}
	return ""
}
