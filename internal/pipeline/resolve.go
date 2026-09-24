// Package pipeline: runtime template resolution. ParseRefs (template.go)
// handles the grammar; Resolve substitutes values against a caller-supplied
// scope. No expression evaluation happens here — only lookup.
package pipeline

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Scope supplies the values a template resolves against.
type Scope struct {
	// Inputs holds declared pipeline inputs by name.
	Inputs map[string]string
	// Outputs holds each completed step's output, keyed by step id.
	Outputs map[string]string
}

// Resolve replaces every template reference in s with its value from scope.
// A reference to an unknown input or step, or a JSON path that does not exist
// in a step's output, is an error. Adjacent refs and surrounding literals are
// preserved, so "hi {{ inputs.name }}" resolves to "hi Ada".
func Resolve(s string, scope Scope) (string, error) {
	refs, err := ParseRefs(s)
	if err != nil {
		return "", err
	}
	if len(refs) == 0 {
		return s, nil
	}

	replacements := make(map[string]string, len(refs))
	for _, ref := range refs {
		val, err := resolveRef(ref, scope)
		if err != nil {
			return "", err
		}
		replacements[ref.Raw] = val
	}

	var b strings.Builder
	last := 0
	for _, m := range refPattern.FindAllStringIndex(s, -1) {
		b.WriteString(s[last:m[0]])
		b.WriteString(replacements[s[m[0]:m[1]]])
		last = m[1]
	}
	b.WriteString(s[last:])
	return b.String(), nil
}

func resolveRef(ref Ref, scope Scope) (string, error) {
	switch ref.Kind {
	case RefInput:
		val, ok := scope.Inputs[ref.Name]
		if !ok {
			return "", fmt.Errorf("unresolved input %q", ref.Name)
		}
		return val, nil

	case RefStepOutput:
		raw, ok := scope.Outputs[ref.StepID]
		if !ok {
			return "", fmt.Errorf("unresolved step output %q", ref.StepID)
		}
		if len(ref.Path) == 0 {
			return raw, nil
		}
		return lookupPath(raw, ref.Path)

	default:
		return "", fmt.Errorf("unresolved reference %q", ref.Raw)
	}
}

// lookupPath deserializes JSON and walks a dotted path. Array indices are
// allowed as numeric segments (e.g. items.0.name).
func lookupPath(raw string, path []string) (string, error) {
	var doc any
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &doc); err != nil {
		return "", fmt.Errorf("path %s: output is not JSON: %w", strings.Join(path, "."), err)
	}
	cur := doc
	for _, seg := range path {
		switch node := cur.(type) {
		case map[string]any:
			v, ok := node[seg]
			if !ok {
				return "", fmt.Errorf("path %s: key %q not found", strings.Join(path, "."), seg)
			}
			cur = v
		case []any:
			idx, err := strconv.Atoi(seg)
			if err != nil || idx < 0 || idx >= len(node) {
				return "", fmt.Errorf("path %s: bad index %q", strings.Join(path, "."), seg)
			}
			cur = node[idx]
		default:
			return "", fmt.Errorf("path %s: cannot descend into %T", strings.Join(path, "."), cur)
		}
	}
	return stringify(cur), nil
}

// stringify renders a resolved JSON node for template substitution. Strings
// come back unquoted; other scalars use their JSON encoding; objects and
// arrays are re-marshaled compactly.
func stringify(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		out, err := json.Marshal(t)
		if err != nil {
			return fmt.Sprint(t)
		}
		return string(out)
	}
}
