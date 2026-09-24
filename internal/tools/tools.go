// Package tools provides the built-in, side-effect-free functions an agent may
// call when function calling is enabled. There is deliberately no shell,
// filesystem, or arbitrary-network tool: the default deployment stays at zero
// remote-code-execution surface. See docs/MEMORY.md (Phase 8).
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Func is a callable tool. Arguments is the raw JSON object the model supplied.
type Func struct {
	Name        string
	Description string
	Parameters  map[string]any
	Run         func(ctx context.Context, arguments string) (string, error)
}

// Registry is an immutable set of tools resolved from a fixed allowlist.
type Registry struct {
	tools map[string]Func
}

// New builds a Registry from the named built-in tools. An unknown name is an
// error, so a misconfigured deployment fails fast instead of silently dropping
// a capability.
func New(names ...string) (*Registry, error) {
	r := &Registry{tools: map[string]Func{}}
	for _, name := range names {
		fn, ok := builtins[name]
		if !ok {
			return nil, fmt.Errorf("tools: unknown tool %q", name)
		}
		r.tools[name] = fn
	}
	return r, nil
}

// Get returns the named tool.
func (r *Registry) Get(name string) (Func, bool) {
	if r == nil {
		return Func{}, false
	}
	fn, ok := r.tools[name]
	return fn, ok
}

// IsBuiltin reports whether name is a known built-in tool. Registry validation
// uses it so a blueprint listing a typo is rejected at load time.
func IsBuiltin(name string) bool {
	_, ok := builtins[name]
	return ok
}

// Names returns the tool names, sorted.
func (r *Registry) Names() []string {
	if r == nil {
		return nil
	}
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Len returns how many tools the registry holds.
func (r *Registry) Len() int {
	if r == nil {
		return 0
	}
	return len(r.tools)
}

// builtins is the fixed allowlist. Add tools here, not dynamically.
var builtins = map[string]Func{
	"current_time": {
		Name:        "current_time",
		Description: "Return the current UTC time in RFC 3339 format.",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
		Run: func(_ context.Context, _ string) (string, error) {
			return time.Now().UTC().Format(time.RFC3339), nil
		},
	},
	"word_count": {
		Name:        "word_count",
		Description: "Count the whitespace-separated words in the given text.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{"type": "string", "description": "Text to count."},
			},
			"required": []string{"text"},
		},
		Run: func(_ context.Context, arguments string) (string, error) {
			var args struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal([]byte(arguments), &args); err != nil {
				return "", fmt.Errorf("word_count: %w", err)
			}
			return fmt.Sprintf("%d", len(strings.Fields(args.Text))), nil
		},
	},
	"math_eval": {
		Name:        "math_eval",
		Description: "Evaluate a basic arithmetic expression using +, -, *, / and parentheses.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"expression": map[string]any{"type": "string", "description": "Arithmetic expression."},
			},
			"required": []string{"expression"},
		},
		Run: func(_ context.Context, arguments string) (string, error) {
			var args struct {
				Expression string `json:"expression"`
			}
			if err := json.Unmarshal([]byte(arguments), &args); err != nil {
				return "", fmt.Errorf("math_eval: %w", err)
			}
			v, err := eval(args.Expression)
			if err != nil {
				return "", fmt.Errorf("math_eval: %w", err)
			}
			return formatFloat(v), nil
		},
	},
}
