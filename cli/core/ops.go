package core

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"
)

// Operation is one scaffold capability. Every adapter (CLI, MCP, HTTP)
// discovers operations through Kit.Operations and never special-cases one.
type Operation interface {
	Name() string
	Summary() string
	// InputType is the struct type the operation accepts. Field tags:
	//   json:"name,omitempty"        wire name (CLI flag is the kebab form)
	//   jsonschema:"text"            description (help text, MCP schema)
	//   arg:"0" | arg:"rest"         CLI positional argument
	InputType() reflect.Type
	InputSchema() (*jsonschema.Schema, error)
	// Invoke decodes raw JSON input and runs the operation.
	Invoke(ctx context.Context, raw json.RawMessage) (any, error)
}

// Texter is implemented by outputs that have a human-readable form.
type Texter interface{ Text() string }

type op[In, Out any] struct {
	name, summary string
	run           func(context.Context, In) (Out, error)
}

func define[In, Out any](name, summary string, run func(context.Context, In) (Out, error)) Operation {
	return &op[In, Out]{name: name, summary: summary, run: run}
}

func (o *op[In, Out]) Name() string            { return o.name }
func (o *op[In, Out]) Summary() string         { return o.summary }
func (o *op[In, Out]) InputType() reflect.Type { return reflect.TypeFor[In]() }
func (o *op[In, Out]) InputSchema() (*jsonschema.Schema, error) {
	return jsonschema.For[In](nil)
}

func (o *op[In, Out]) Invoke(ctx context.Context, raw json.RawMessage) (any, error) {
	var in In
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, fmt.Errorf("%s: invalid input: %w", o.name, err)
		}
	}
	return o.run(ctx, in)
}

// Operations lists every operation in display order.
func (k *Kit) Operations() []Operation {
	return []Operation{
		define("list", "List feature-belt modules and what each one adds.", k.List),
		define("show", "Show one feature's manifest and its FEATURE.md spec.", k.Show),
		define("spec", "Print a scaffold spec document (e.g. concrete/design). With no path, list them.", k.Spec),
		define("new", "Create a project from the concrete template, optionally with features.", k.New),
		define("add", "Install features (and their requirements) into an existing project.", k.Add),
		define("env", "Report the project's env vars; --write regenerates ENV.md and the .env.example files.", k.Env),
		define("doctor", "Check a project against the concrete contract: manifest, migrations, anchors, env, house rules.", k.Doctor),
	}
}

// Find returns the named operation.
func (k *Kit) Find(name string) (Operation, bool) {
	for _, o := range k.Operations() {
		if o.Name() == name {
			return o, true
		}
	}
	return nil, false
}
