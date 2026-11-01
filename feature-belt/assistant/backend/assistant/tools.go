package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
)

// The tool registry collects every module's app.ToolProvider tools at first
// use (Deps.Modules is filled after construction). Read-only tools are always
// offered; tools that change data only with ASSISTANT_ALLOW_WRITES=true. A
// tool whose name suggests deleting, sending, emailing or fetching URLs is
// refused outright: the model must have no exfiltration channel.

const (
	maxParallelTools = 6
	toolTimeout      = 30 * time.Second
	// maxToolBytes is about 12k tokens of tool output per call.
	maxToolBytes      = 48_000
	maxWritesPerTurn  = 5
	maxToolNameLength = 64
)

var (
	toolNameRE  = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
	bannedWords = []string{"delete", "send", "email", "fetch_url", "fetchurl", "browse"}
)

type regTool struct {
	app.Tool
	schema *compiledSchema
}

type registry struct {
	tools  []*regTool // sorted by name: a stable prompt prefix
	byName map[string]*regTool
	lower  map[string]*regTool
	specs  []ToolSpec
}

// registry builds (once) the tool set offered to the model.
func (s *Service) registry() *registry {
	s.regOnce.Do(func() {
		var all []app.Tool
		for _, m := range s.d.Modules {
			if tp, ok := m.(app.ToolProvider); ok {
				all = append(all, tp.Tools()...)
			}
		}
		s.reg = buildRegistry(all, s.cfg.allowWrites)
	})
	return s.reg
}

func buildRegistry(all []app.Tool, allowWrites bool) *registry {
	reg := &registry{byName: map[string]*regTool{}, lower: map[string]*regTool{}}
	for _, t := range all {
		if reason := refuseTool(t); reason != "" {
			slog.Error("assistant: tool refused", "tool", t.Name, "reason", reason)
			continue
		}
		if !t.ReadOnly && !allowWrites {
			continue
		}
		if _, dup := reg.byName[t.Name]; dup {
			slog.Error("assistant: duplicate tool name refused", "tool", t.Name)
			continue
		}
		cs, err := compileSchema(t.InputSchema)
		if err != nil {
			slog.Error("assistant: tool schema refused", "tool", t.Name, "err", err)
			continue
		}
		rt := &regTool{Tool: t, schema: cs}
		reg.byName[t.Name] = rt
		reg.lower[strings.ToLower(t.Name)] = rt
		reg.tools = append(reg.tools, rt)
	}
	sort.Slice(reg.tools, func(i, j int) bool { return reg.tools[i].Name < reg.tools[j].Name })
	for _, t := range reg.tools {
		reg.specs = append(reg.specs, ToolSpec{Name: t.Name, Description: t.Description, InputSchema: canonicalSchema(t.InputSchema)})
	}
	return reg
}

// refuseTool is the startup check: a name that suggests an outward or
// destructive capability, or a malformed name or handler, keeps the tool out.
func refuseTool(t app.Tool) string {
	if !toolNameRE.MatchString(t.Name) || len(t.Name) > maxToolNameLength {
		return "name must match ^[a-zA-Z0-9_-]{1,64}$"
	}
	lower := strings.ToLower(t.Name)
	for _, w := range bannedWords {
		if strings.Contains(lower, w) {
			return "names containing " + w + " are not allowed (no tool may delete user data, send anything outward or fetch URLs)"
		}
	}
	if t.Handler == nil {
		return "no handler"
	}
	if strings.TrimSpace(t.Description) == "" {
		return "no description"
	}
	return ""
}

// canonicalSchema compacts a schema and drops $schema/$id so the tool list
// renders byte-identically on every request (cache prefix).
func canonicalSchema(raw json.RawMessage) json.RawMessage {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return raw
	}
	delete(m, "$schema")
	delete(m, "$id")
	b, err := json.Marshal(m) // map keys marshal sorted
	if err != nil {
		return raw
	}
	var out bytes.Buffer
	if json.Compact(&out, b) != nil {
		return b
	}
	return out.Bytes()
}

// lookup finds a tool, accepting a name that differs only in letter case.
func (r *registry) lookup(name string) (*regTool, bool) {
	if t, ok := r.byName[name]; ok {
		return t, true
	}
	t, ok := r.lower[strings.ToLower(name)]
	return t, ok
}

// ---- running calls ---------------------------------------------------------

// Activity is a tool call as shown in the UI and stored with the message, so
// history re-renders tool activity without re-running tools.
type Activity struct {
	CallID     string          `json:"call_id"`
	Name       string          `json:"name"`
	Status     string          `json:"status"` // running | ok | error
	Summary    string          `json:"summary"`
	Input      json.RawMessage `json:"input,omitempty"`
	StartedAt  time.Time       `json:"started_at"`
	DurationMS int64           `json:"duration_ms"`
}

type toolStartEvent struct {
	MessageID uuid.UUID       `json:"message_id"`
	CallID    string          `json:"call_id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	StartedAt time.Time       `json:"started_at"`
}

type toolEndEvent struct {
	MessageID  uuid.UUID `json:"message_id"`
	CallID     string    `json:"call_id"`
	Name       string    `json:"name"`
	Status     string    `json:"status"`
	Summary    string    `json:"summary"`
	DurationMS int64     `json:"duration_ms"`
}

// toolRunner runs one turn's tool calls as the caller.
type toolRunner struct {
	reg       *registry
	caller    *auth.Identity
	messageID uuid.UUID
	convID    uuid.UUID
	emit      func(name string, data any)
	writes    atomic.Int32
}

// runCalls runs a round's calls in parallel (at most 6 at once, 30 s each)
// and returns their results in call order.
func (tr *toolRunner) runCalls(ctx context.Context, calls []Part) []Part {
	results := make([]Part, len(calls))
	sem := make(chan struct{}, maxParallelTools)
	var wg sync.WaitGroup
	for i, call := range calls {
		started := time.Now().UTC()
		tr.emit("tool_start", toolStartEvent{MessageID: tr.messageID, CallID: call.CallID, Name: call.Name,
			Input: displayInput(call.Input), StartedAt: started})
		wg.Add(1)
		go func(i int, call Part, started time.Time) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			res, status, summary := tr.runCall(ctx, call)
			results[i] = res
			tr.emit("tool_end", toolEndEvent{MessageID: tr.messageID, CallID: call.CallID, Name: res.Name,
				Status: status, Summary: summary, DurationMS: time.Since(started).Milliseconds()})
		}(i, call, started)
	}
	wg.Wait()
	return results
}

func displayInput(in json.RawMessage) json.RawMessage {
	if len(in) == 0 || !json.Valid(in) || len(in) > 4000 {
		return json.RawMessage(`{}`)
	}
	return in
}

type toolFailure struct {
	code, message string
}

// runCall validates and runs one call. It never returns an error: failures
// become error results the model can read and recover from.
func (tr *toolRunner) runCall(ctx context.Context, call Part) (Part, string, string) {
	out := Part{Type: PartToolResult, CallID: call.CallID, Name: call.Name}
	fail := func(f toolFailure) (Part, string, string) {
		env := envelope{Tool: out.Name}
		env.Error = &struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}{f.code, f.message}
		out.Content, out.IsError = wrapData(env), true
		return out, "error", f.message
	}
	tool, ok := tr.reg.lookup(call.Name)
	if !ok {
		return fail(toolFailure{"unknown_tool", fmt.Sprintf("There is no tool called %q.", call.Name)})
	}
	out.Name = tool.Name
	if call.IsError {
		raw, _ := json.Marshal(map[string]string{"INVALID_JSON": truncateRunes(call.Text, 500)})
		return fail(toolFailure{"invalid_json", "The input was not valid JSON: " + string(raw)})
	}
	if err := tool.schema.validate(call.Input); err != nil {
		return fail(toolFailure{"invalid_input", "Invalid input: " + err.Error()})
	}
	if !tool.ReadOnly && tr.writes.Add(1) > maxWritesPerTurn {
		return fail(toolFailure{"write_limit", fmt.Sprintf("This turn already made %d changes. Ask the user before making more.", maxWritesPerTurn)})
	}
	data, err := tr.safeRun(ctx, tool, call.Input)
	if err != nil {
		return fail(tr.failure(ctx, tool.Name, err))
	}
	fitted, note := fitData(data, maxToolBytes)
	env := envelope{Tool: tool.Name, Data: fitted, Truncated: note}
	if rep := scanStrings(fitted); rep.Flagged {
		env.Notice = injectionNotice
		slog.WarnContext(ctx, "assistant: injection heuristic in tool data", "tool", tool.Name,
			"conversation_id", tr.convID, "message_id", tr.messageID, "score", rep.Score, "hits", rep.Hits)
	}
	out.Content = wrapData(env)
	return out, "ok", summarize(fitted)
}

func (tr *toolRunner) safeRun(ctx context.Context, tool *regTool, input json.RawMessage) (data any, err error) {
	ctx, cancel := context.WithTimeout(ctx, toolTimeout)
	defer cancel()
	defer func() {
		if rec := recover(); rec != nil {
			slog.ErrorContext(ctx, "assistant: tool panicked", "tool", tool.Name, "panic", fmt.Sprint(rec))
			err = errToolPanic
		}
	}()
	return tool.Handler(ctx, tr.caller, input)
}

var errToolPanic = errors.New("tool panicked")

func (tr *toolRunner) failure(ctx context.Context, name string, err error) toolFailure {
	var apiErr *httpx.Error
	switch {
	case errors.As(err, &apiErr):
		return toolFailure{apiErr.Code, apiErr.Message}
	case errors.Is(err, context.DeadlineExceeded):
		return toolFailure{"timeout", "The tool took too long to answer."}
	case errors.Is(err, context.Canceled):
		return toolFailure{"cancelled", "The tool was stopped."}
	}
	slog.ErrorContext(ctx, "assistant: tool failed", "tool", name, "conversation_id", tr.convID, "err", err)
	return toolFailure{"tool_failed", "The tool failed unexpectedly."}
}

// summarize is the one-line activity summary for a successful call.
func summarize(v any) string {
	switch x := v.(type) {
	case []any:
		return fmt.Sprintf("%d results", len(x))
	case map[string]any:
		if items, ok := x["items"].([]any); ok {
			return fmt.Sprintf("%d results", len(items))
		}
	}
	return "Done"
}

// fitData shrinks a tool result to about max bytes of JSON: it halves the
// largest arrays first, then shortens long strings, then cuts the JSON text.
// The note says what was dropped so the model doesn't present a partial
// result as complete.
func fitData(v any, maxBytes int) (any, string) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, "The result could not be encoded."
	}
	var generic any
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if dec.Decode(&generic) != nil {
		return nil, "The result could not be encoded."
	}
	if len(b) <= maxBytes {
		return generic, ""
	}
	size := func() int { b, _ := json.Marshal(generic); return len(b) }
	note := ""
	holder := map[string]any{"v": generic}
	for i := 0; i < 32 && size() > maxBytes; i++ {
		if !halveLargestArray(holder) {
			break
		}
		generic = holder["v"]
		note = "Some list items were left out to fit the size limit; ask for fewer or narrower results."
	}
	for _, limit := range []int{2000, 500, 120} {
		if size() <= maxBytes {
			break
		}
		generic = shortenStrings(generic, limit)
		note = "Long text was shortened to fit the size limit."
	}
	if b, _ := json.Marshal(generic); len(b) > maxBytes {
		cut := b[:maxBytes]
		for len(cut) > 0 && cut[len(cut)-1]&0xC0 == 0x80 {
			cut = cut[:len(cut)-1]
		}
		return string(cut), "The result was cut to fit the size limit."
	}
	return generic, note
}

// halveLargestArray drops the second half of the longest array under
// holder (a map wrapping the value, so a root array has a parent to update).
func halveLargestArray(holder map[string]any) bool {
	bestLen := 0
	var bestArr []any
	var bestSet func([]any)
	var walk func(x any)
	walk = func(x any) {
		switch t := x.(type) {
		case []any:
			for i, e := range t {
				if arr, ok := e.([]any); ok && len(arr) > bestLen {
					bestLen, bestArr, bestSet = len(arr), arr, func(n []any) { t[i] = n }
				}
				walk(e)
			}
		case map[string]any:
			for k, e := range t {
				if arr, ok := e.([]any); ok && len(arr) > bestLen {
					bestLen, bestArr, bestSet = len(arr), arr, func(n []any) { t[k] = n }
				}
				walk(e)
			}
		}
	}
	walk(holder)
	if bestLen < 2 || bestSet == nil {
		return false
	}
	bestSet(bestArr[:bestLen/2])
	return true
}

func shortenStrings(v any, limit int) any {
	switch t := v.(type) {
	case string:
		return truncateRunes(t, limit)
	case []any:
		for i, e := range t {
			t[i] = shortenStrings(e, limit)
		}
		return t
	case map[string]any:
		for k, e := range t {
			t[k] = shortenStrings(e, limit)
		}
		return t
	}
	return v
}
