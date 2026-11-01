package assistant

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Server-side validation of tool inputs against each tool's JSON Schema,
// before the tool runs. The provider's own schema handling is not trusted
// (eager input streaming turns it off on Anthropic, OpenAI tools are not
// strict). Supports the subset tool schemas use: type, enum, const,
// properties, required, additionalProperties, items, prefix-free arrays,
// min/max (length, items, value), pattern, format (uuid, date-time, date,
// email), anyOf/oneOf/allOf and local $ref into $defs/definitions.

type schemaNode = map[string]any

// compiledSchema is a parsed tool schema. steps counts the nodes one
// validate call has visited (each call works on its own copy).
type compiledSchema struct {
	root  schemaNode
	steps int
}

func compileSchema(raw json.RawMessage) (*compiledSchema, error) {
	var root schemaNode
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&root); err != nil {
		return nil, fmt.Errorf("schema is not a JSON object: %w", err)
	}
	if t, _ := root["type"].(string); t != "object" {
		return nil, fmt.Errorf(`schema root must have "type": "object"`)
	}
	return &compiledSchema{root: root}, nil
}

// validate checks a JSON input against the schema; the error text is shown
// to the model so it can correct the call.
func (c *compiledSchema) validate(input json.RawMessage) error {
	var v any
	dec := json.NewDecoder(bytes.NewReader(input))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return fmt.Errorf("input is not valid JSON")
	}
	if dec.More() {
		return fmt.Errorf("input has trailing data after the JSON object")
	}
	run := &compiledSchema{root: c.root} // per-call step count; c is shared
	return run.check("input", run.root, v, 0)
}

// maxSchemaDepth bounds nesting; maxSchemaSteps bounds the whole walk.
// Schemas from connected servers are untrusted: anyOf/oneOf over $refs that
// point back at themselves branch at every level, so depth alone still
// allows billions of visits (a CPU-bound call that ignores its context).
const (
	maxSchemaDepth = 32
	maxSchemaSteps = 20_000
)

func (c *compiledSchema) check(path string, s schemaNode, v any, depth int) error {
	if depth > maxSchemaDepth {
		return fmt.Errorf("%s nests too deeply", path)
	}
	if c.steps++; c.steps > maxSchemaSteps {
		return fmt.Errorf("the tool's input schema is too complex to check")
	}
	if ref, ok := s["$ref"].(string); ok {
		target, err := c.resolve(ref)
		if err != nil {
			return err
		}
		return c.check(path, target, v, depth+1)
	}
	if b, ok := s["const"]; ok && !jsonEqual(b, v) {
		return fmt.Errorf("%s must be %s", path, jsonText(b))
	}
	if e, ok := s["enum"].([]any); ok {
		if !slices.ContainsFunc(e, func(x any) bool { return jsonEqual(x, v) }) {
			vals := make([]string, 0, len(e))
			for _, x := range e {
				vals = append(vals, jsonText(x))
			}
			return fmt.Errorf("%s must be one of %s", path, strings.Join(vals, ", "))
		}
	}
	if types := typeList(s["type"]); len(types) > 0 {
		kind := jsonKind(v)
		if !slices.Contains(types, kind) && !(kind == "integer" && slices.Contains(types, "number")) {
			return fmt.Errorf("%s must be %s", path, strings.Join(types, " or "))
		}
	}
	for _, sub := range nodes(s["allOf"]) {
		if err := c.check(path, sub, v, depth+1); err != nil {
			return err
		}
	}
	if subs := nodes(s["anyOf"]); len(subs) > 0 {
		var first error
		ok := false
		for _, sub := range subs {
			if err := c.check(path, sub, v, depth+1); err == nil {
				ok = true
				break
			} else if first == nil {
				first = err
			}
		}
		if !ok {
			return first
		}
	}
	if subs := nodes(s["oneOf"]); len(subs) > 0 {
		matched := 0
		var first error
		for _, sub := range subs {
			if err := c.check(path, sub, v, depth+1); err == nil {
				matched++
			} else if first == nil {
				first = err
			}
		}
		if matched == 0 {
			return first
		}
		if matched > 1 {
			return fmt.Errorf("%s matches more than one allowed shape", path)
		}
	}
	switch x := v.(type) {
	case string:
		return checkString(path, s, x)
	case json.Number:
		return checkNumber(path, s, x)
	case []any:
		if n, ok := num(s["minItems"]); ok && float64(len(x)) < n {
			return fmt.Errorf("%s needs at least %v items", path, n)
		}
		if n, ok := num(s["maxItems"]); ok && float64(len(x)) > n {
			return fmt.Errorf("%s allows at most %v items", path, n)
		}
		if u, _ := s["uniqueItems"].(bool); u {
			for i := range x {
				for j := i + 1; j < len(x); j++ {
					if jsonEqual(x[i], x[j]) {
						return fmt.Errorf("%s has duplicate items", path)
					}
				}
			}
		}
		if items, ok := s["items"].(schemaNode); ok {
			for i, e := range x {
				if err := c.check(fmt.Sprintf("%s[%d]", path, i), items, e, depth+1); err != nil {
					return err
				}
			}
		}
	case map[string]any:
		props, _ := s["properties"].(schemaNode)
		for _, req := range strList(s["required"]) {
			if _, ok := x[req]; !ok {
				return fmt.Errorf("%s.%s is required", path, req)
			}
		}
		if n, ok := num(s["minProperties"]); ok && float64(len(x)) < n {
			return fmt.Errorf("%s needs at least %v fields", path, n)
		}
		if n, ok := num(s["maxProperties"]); ok && float64(len(x)) > n {
			return fmt.Errorf("%s allows at most %v fields", path, n)
		}
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			if ps, ok := props[k].(schemaNode); ok {
				if err := c.check(path+"."+k, ps, x[k], depth+1); err != nil {
					return err
				}
				continue
			}
			switch ap := s["additionalProperties"].(type) {
			case bool:
				if !ap {
					return fmt.Errorf("%s has unknown field %q", path, k)
				}
			case schemaNode:
				if err := c.check(path+"."+k, ap, x[k], depth+1); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (c *compiledSchema) resolve(ref string) (schemaNode, error) {
	for _, prefix := range []string{"#/$defs/", "#/definitions/"} {
		if name, ok := strings.CutPrefix(ref, prefix); ok {
			defs, _ := c.root[strings.Trim(prefix[2:], "/")].(schemaNode)
			if target, ok := defs[name].(schemaNode); ok {
				return target, nil
			}
		}
	}
	if ref == "#" {
		return c.root, nil
	}
	return nil, fmt.Errorf("schema reference %q is not supported", ref)
}

var (
	patternMu    sync.Mutex
	patternCache = map[string]*regexp.Regexp{}
	dateTimeRE   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}[Tt ]\d{2}:\d{2}:\d{2}(\.\d+)?([Zz]|[+-]\d{2}:\d{2})$`)
	dateRE       = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	emailRE      = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
)

func checkString(path string, s schemaNode, x string) error {
	n := utf8.RuneCountInString(x)
	if m, ok := num(s["minLength"]); ok && float64(n) < m {
		return fmt.Errorf("%s must be at least %v characters", path, m)
	}
	if m, ok := num(s["maxLength"]); ok && float64(n) > m {
		return fmt.Errorf("%s must be at most %v characters", path, m)
	}
	if p, ok := s["pattern"].(string); ok {
		patternMu.Lock()
		re, cached := patternCache[p]
		if !cached {
			re, _ = regexp.Compile(p)
			patternCache[p] = re
		}
		patternMu.Unlock()
		if re != nil && !re.MatchString(x) {
			return fmt.Errorf("%s has the wrong format", path)
		}
	}
	switch s["format"] {
	case "uuid":
		if _, err := uuid.Parse(x); err != nil {
			return fmt.Errorf("%s must be a UUID", path)
		}
	case "date-time":
		if !dateTimeRE.MatchString(x) {
			return fmt.Errorf("%s must be an RFC 3339 date-time", path)
		}
	case "date":
		if !dateRE.MatchString(x) {
			return fmt.Errorf("%s must be a date (YYYY-MM-DD)", path)
		}
	case "email":
		if !emailRE.MatchString(x) {
			return fmt.Errorf("%s must be an email address", path)
		}
	}
	return nil
}

func checkNumber(path string, s schemaNode, x json.Number) error {
	f, err := x.Float64()
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return fmt.Errorf("%s is not a number", path)
	}
	if m, ok := num(s["minimum"]); ok && f < m {
		return fmt.Errorf("%s must be at least %v", path, m)
	}
	if m, ok := num(s["maximum"]); ok && f > m {
		return fmt.Errorf("%s must be at most %v", path, m)
	}
	if m, ok := num(s["exclusiveMinimum"]); ok && f <= m {
		return fmt.Errorf("%s must be more than %v", path, m)
	}
	if m, ok := num(s["exclusiveMaximum"]); ok && f >= m {
		return fmt.Errorf("%s must be less than %v", path, m)
	}
	return nil
}

func jsonKind(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case bool:
		return "boolean"
	case json.Number:
		if _, err := x.Int64(); err == nil {
			return "integer"
		}
		if f, err := x.Float64(); err == nil && f == math.Trunc(f) && !strings.ContainsAny(x.String(), ".eE") {
			return "integer"
		}
		return "number"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return "unknown"
}

func typeList(t any) []string {
	switch x := t.(type) {
	case string:
		return []string{x}
	case []any:
		return strList(x)
	}
	return nil
}

func strList(v any) []string {
	arr, _ := v.([]any)
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func nodes(v any) []schemaNode {
	arr, _ := v.([]any)
	out := make([]schemaNode, 0, len(arr))
	for _, e := range arr {
		if n, ok := e.(schemaNode); ok {
			out = append(out, n)
		}
	}
	return out
}

func num(v any) (float64, bool) {
	switch x := v.(type) {
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	case float64:
		return x, true
	}
	return 0, false
}

func jsonEqual(a, b any) bool {
	return jsonText(a) == jsonText(b)
}

func jsonText(v any) string {
	if n, ok := v.(json.Number); ok {
		if f, err := n.Float64(); err == nil {
			b, _ := json.Marshal(f)
			return string(b)
		}
	}
	b, _ := json.Marshal(v)
	return string(b)
}
