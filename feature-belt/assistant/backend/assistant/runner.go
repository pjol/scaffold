package assistant

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
)

// The turn loop: up to ASSISTANT_MAX_TOOL_ROUNDS rounds of model call →
// parallel tool calls, then one final round with tool_choice "none" (the
// tools stay declared so the cached prefix survives) that must answer. Text
// passes through the canary scanner before it reaches the client; partial
// text survives a stop or an error.

const (
	turnMaxTokens = 64000

	textRefused = "I can't help with that request."
	textBlocked = "I can't share that."
	textEmpty   = "I couldn't put an answer together. Try rephrasing your question."
	textLength  = "\n\n_(The answer hit the length limit. Ask me to continue.)_"
	budgetNote  = "You've used this turn's tool budget. Don't call more tools: answer now with what you have, and say briefly what you couldn't check."
)

// Turn endings beyond the provider stop reasons.
const (
	stopBlocked   = "blocked"
	stopCancelled = "cancelled"
	stopError     = "error"
)

var (
	errStopped  = errors.New("stopped by the user")
	errShutdown = errors.New("server shutting down")
	errTimeout  = errors.New("turn timed out")
)

type turnInput struct {
	caller    *auth.Identity
	convID    uuid.UUID
	messageID uuid.UUID
	system    []SystemBlock
	canary    *canary
	history   []Message
	reg       *registry
	approve   approvalFunc
	policy    app.AgentPolicy
}

type runResult struct {
	text       string
	items      []Message
	usage      Usage
	estimated  bool
	rounds     int
	stop       string
	model      string
	lastPrompt int // prompt + output tokens of the last round (drives compaction)
	err        error
}

type runner struct {
	s    *Service
	in   turnInput
	emit func(name string, data any)
}

type deltaEvent struct {
	MessageID uuid.UUID `json:"message_id"`
	Text      string    `json:"text"`
}

func (rn *runner) run(ctx context.Context) *runResult {
	s, in := rn.s, rn.in
	res := &runResult{model: s.cfg.model}
	msgs := append([]Message(nil), in.history...)
	tools := &toolRunner{reg: in.reg, caller: in.caller, messageID: in.messageID, convID: in.convID, emit: rn.emit, approve: in.approve, policy: in.policy}
	scanner := in.canary.scanner()

	var (
		mu       sync.Mutex
		shown    strings.Builder // text the client has received
		blocked  bool
		pendSep  bool
		cancelRd context.CancelFunc
	)
	show := func(t string) {
		if t == "" {
			return
		}
		if pendSep {
			t, pendSep = "\n\n"+t, false
		}
		shown.WriteString(t)
		rn.emit("delta", deltaEvent{MessageID: in.messageID, Text: t})
	}
	finish := func(stop string) *runResult {
		res.stop = stop
		res.text = shown.String()
		return res
	}

	for round := 0; ; round++ {
		final := round >= s.cfg.maxToolRounds
		req := Request{Model: s.cfg.model, System: in.system, Messages: msgs, Tools: in.reg.specs,
			Effort: s.cfg.effort, MaxTokens: turnMaxTokens, UserHash: userHash(in.caller.UserID)}
		if final {
			req.ToolChoice = "none"
		}
		mu.Lock()
		pendSep = shown.Len() > 0
		mu.Unlock()

		roundCtx, cancel := context.WithCancel(ctx)
		cancelRd = cancel
		start := time.Now()
		resp, err := s.provider.Stream(roundCtx, req, func(ev Event) {
			if ev.Type != EventText {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			out, tripped := scanner.push(ev.Text)
			if tripped {
				blocked = true
				cancelRd()
				return
			}
			show(out)
		})
		cancel()
		res.rounds++
		mu.Lock()
		if !blocked {
			tail, tripped := scanner.flush()
			if tripped {
				blocked = true
			} else {
				show(tail)
			}
		}
		mu.Unlock()

		// Usage: what the provider reported, else an estimate.
		var u Usage
		if resp != nil && resp.UsageKnown {
			u = resp.Usage
		} else if err == nil || resp != nil || shown.Len() > 0 {
			u, res.estimated = estimateUsage(req, resp.Text()), true
		}
		res.usage.Add(u)
		res.lastPrompt = u.PromptTokens() + u.OutputTokens
		if resp != nil && resp.Model != "" {
			res.model = resp.Model
		}
		stopReason := ""
		if resp != nil {
			stopReason = resp.Stop
		}
		slog.InfoContext(ctx, "assistant: round", "conversation_id", in.convID, "message_id", in.messageID,
			"round", round, "provider", s.provider.Name(), "model", res.model, "stop", stopReason,
			"input_tokens", u.InputTokens, "cache_read_tokens", u.CacheReadTokens,
			"cache_write_tokens", u.CacheWriteTokens, "output_tokens", u.OutputTokens,
			"latency_ms", time.Since(start).Milliseconds())

		if blocked {
			slog.WarnContext(ctx, "assistant: canary in model output; reply withheld", "conversation_id", in.convID,
				"message_id", in.messageID, "user_id", in.caller.UserID)
			return rn.blockedResult(res)
		}
		if err != nil {
			// Keep the text that streamed so the next turn sees what the user saw.
			if resp != nil {
				if parts := textOnly(resp.Parts); len(parts) > 0 {
					res.items = append(res.items, Message{Role: RoleAssistant, Parts: parts})
				}
			}
			if ctx.Err() != nil {
				return finish(stopCancelled)
			}
			res.err = err
			return finish(stopError)
		}

		calls := resp.ToolCalls()
		for _, c := range calls {
			if in.canary.contains(string(c.Input)) || in.canary.contains(c.Text) {
				slog.WarnContext(ctx, "assistant: canary in tool input; reply withheld", "conversation_id", in.convID,
					"message_id", in.messageID, "tool", c.Name)
				return rn.blockedResult(res)
			}
		}
		switch {
		case resp.Stop == StopRefusal:
			// A refusal can cut output (and tool calls) off mid-way: discard
			// what streamed and never run that round's tools.
			res.text, res.stop = textRefused, StopRefusal
			res.items = append(res.items, Message{Role: RoleAssistant, Parts: []Part{{Type: PartText, Text: textRefused}}})
			return res
		case len(calls) == 0 || final || resp.Stop == StopMaxTokens:
			parts := resp.Parts
			if len(calls) > 0 {
				// Truncated or over-budget calls are not run.
				parts = withoutToolCalls(parts)
			}
			res.items = append(res.items, Message{Role: RoleAssistant, Parts: parts})
			r := finish(normaliseStop(resp.Stop))
			if r.stop == StopMaxTokens {
				r.text += textLength
			}
			return r
		}

		results := tools.runCalls(ctx, calls)
		assistantMsg := Message{Role: RoleAssistant, Parts: resp.Parts}
		toolMsg := Message{Role: RoleUser, Parts: results}
		if round+1 >= s.cfg.maxToolRounds {
			toolMsg.Parts = append(toolMsg.Parts, Part{Type: PartText, Text: serverNotice(budgetNote)})
		}
		res.items = append(res.items, assistantMsg, toolMsg)
		msgs = append(msgs, assistantMsg, toolMsg)
		if ctx.Err() != nil {
			return finish(stopCancelled)
		}
	}
}

func (rn *runner) blockedResult(res *runResult) *runResult {
	res.text, res.stop = textBlocked, stopBlocked
	// Nothing from the leaking round is kept for replay.
	res.items = append(res.items, Message{Role: RoleAssistant, Parts: []Part{{Type: PartText, Text: textBlocked}}})
	return res
}

func withoutToolCalls(parts []Part) []Part {
	out := make([]Part, 0, len(parts))
	for _, p := range parts {
		if p.Type != PartToolCall {
			out = append(out, p)
		}
	}
	return out
}

func textOnly(parts []Part) []Part {
	var out []Part
	for _, p := range parts {
		if p.Type == PartText && strings.TrimSpace(p.Text) != "" {
			out = append(out, Part{Type: PartText, Text: p.Text})
		}
	}
	return out
}

func normaliseStop(s string) string {
	switch s {
	case StopToolUse, "":
		return StopEndTurn
	}
	return s
}
