// Package cliargs turns command-line arguments into an operation's JSON input
// by reflecting over its input struct. Operations never parse flags.
package cliargs

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

type field struct {
	key     string // json name
	flag    string // kebab-case flag
	kind    reflect.Kind
	elem    reflect.Kind // for slices
	arg     string       // "", "0", "1", "rest"
	help    string
	present bool
}

func fields(t reflect.Type) []field {
	var out []field
	for i := range t.NumField() {
		sf := t.Field(i)
		if !sf.IsExported() {
			continue
		}
		key := strings.Split(sf.Tag.Get("json"), ",")[0]
		if key == "" || key == "-" {
			continue
		}
		f := field{
			key:  key,
			flag: strings.ReplaceAll(key, "_", "-"),
			kind: sf.Type.Kind(),
			arg:  sf.Tag.Get("arg"),
			help: sf.Tag.Get("jsonschema"),
		}
		if f.kind == reflect.Slice {
			f.elem = sf.Type.Elem().Kind()
		}
		out = append(out, f)
	}
	return out
}

// Parse converts args into JSON for an input struct of type t.
// Flags: --name value, --name=value, --flag (bool), --list a,b (repeatable).
func Parse(t reflect.Type, args []string) (json.RawMessage, error) {
	fs := fields(t)
	byFlag := map[string]*field{}
	var positional []*field
	var rest *field
	for i := range fs {
		f := &fs[i]
		byFlag[f.flag] = f
		switch {
		case f.arg == "rest":
			rest = f
		case f.arg != "":
			positional = append(positional, f)
		}
	}
	sort.Slice(positional, func(i, j int) bool { return positional[i].arg < positional[j].arg })

	vals := map[string]any{}
	var loose []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			loose = append(loose, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "--") {
			loose = append(loose, a)
			continue
		}
		name, val, hasVal := strings.Cut(strings.TrimPrefix(a, "--"), "=")
		f, ok := byFlag[name]
		if !ok {
			return nil, fmt.Errorf("unknown flag --%s", name)
		}
		if f.kind == reflect.Bool {
			b := true
			if hasVal {
				var err error
				if b, err = strconv.ParseBool(val); err != nil {
					return nil, fmt.Errorf("--%s: %w", name, err)
				}
			}
			vals[f.key] = b
			continue
		}
		if !hasVal {
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--%s needs a value", name)
			}
			i++
			val = args[i]
		}
		if err := set(vals, f, val); err != nil {
			return nil, err
		}
	}
	for i, a := range loose {
		switch {
		case i < len(positional):
			if err := set(vals, positional[i], a); err != nil {
				return nil, err
			}
		case rest != nil:
			if err := set(vals, rest, a); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("unexpected argument %q", a)
		}
	}
	return json.Marshal(vals)
}

func set(vals map[string]any, f *field, raw string) error {
	switch f.kind {
	case reflect.String:
		vals[f.key] = raw
	case reflect.Int, reflect.Int64:
		n, err := strconv.Atoi(raw)
		if err != nil {
			return fmt.Errorf("--%s: %w", f.flag, err)
		}
		vals[f.key] = n
	case reflect.Slice:
		cur, _ := vals[f.key].([]string)
		for part := range strings.SplitSeq(raw, ",") {
			if part = strings.TrimSpace(part); part != "" {
				cur = append(cur, part)
			}
		}
		vals[f.key] = cur
	default:
		return fmt.Errorf("--%s: unsupported field kind %s", f.flag, f.kind)
	}
	return nil
}

// Usage renders flag help for an input struct.
func Usage(t reflect.Type) string {
	var b strings.Builder
	for _, f := range fields(t) {
		switch {
		case f.arg == "rest":
			fmt.Fprintf(&b, "  <%s...>%s%s\n", f.key, pad(len(f.key)+5), f.help)
		case f.arg != "":
			fmt.Fprintf(&b, "  <%s>%s%s\n", f.key, pad(len(f.key)+2), f.help)
		default:
			label := "--" + f.flag
			switch f.kind {
			case reflect.Bool:
			case reflect.Slice:
				label += " a,b"
			default:
				label += " value"
			}
			fmt.Fprintf(&b, "  %s%s%s\n", label, pad(len(label)), f.help)
		}
	}
	return b.String()
}

func pad(n int) string {
	if n >= 26 {
		return "  "
	}
	return strings.Repeat(" ", 28-n)
}
