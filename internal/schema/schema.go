// Package schema is a deliberately small JSON Schema (draft-07 subset) validator
// used to enforce an agent's structured output. It supports the keywords that
// matter for structured LLM output: type, required, properties,
// additionalProperties (boolean), enum, minimum/maximum, minLength/maxLength,
// minItems/maxItems, and items. It is not a general-purpose validator and does
// not follow $ref, allOf, oneOf, or format.
package schema

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Schema is a compiled schema node.
type Schema struct {
	raw map[string]any
}

// Compile parses raw JSON into a Schema. It validates the schema itself (for
// example, that type names are known) so a bad schema fails at load time rather
// than on the first request.
func Compile(raw []byte) (*Schema, error) {
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("schema: %w", err)
	}
	obj, ok := doc.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("schema: root must be a JSON object")
	}
	s := &Schema{raw: obj}
	if err := s.check(); err != nil {
		return nil, err
	}
	return s, nil
}

// Validate checks a JSON document against the schema. The document is passed as
// its raw text (the model output).
func (s *Schema) Validate(document string) error {
	var doc any
	if err := json.Unmarshal([]byte(document), &doc); err != nil {
		return fmt.Errorf("not valid JSON: %w", err)
	}
	return s.validate(doc, "")
}

func (s *Schema) check() error {
	return checkNode(s.raw, "")
}

func checkNode(node map[string]any, path string) error {
	if t, ok := node["type"]; ok {
		switch v := t.(type) {
		case string:
			if !knownType(v) {
				return fmt.Errorf("schema%s: unknown type %q", path, v)
			}
		case []any:
			for _, item := range v {
				name, ok := item.(string)
				if !ok || !knownType(name) {
					return fmt.Errorf("schema%s: unknown type %q", path, item)
				}
			}
		default:
			return fmt.Errorf("schema%s: type must be a string or array of strings", path)
		}
	}
	if props, ok := node["properties"].(map[string]any); ok {
		for name, sub := range props {
			child, ok := sub.(map[string]any)
			if !ok {
				return fmt.Errorf("schema%s.properties.%s: must be an object", path, name)
			}
			if err := checkNode(child, path+".properties."+name); err != nil {
				return err
			}
		}
	}
	if items, ok := node["items"].(map[string]any); ok {
		if err := checkNode(items, path+".items"); err != nil {
			return err
		}
	}
	return nil
}

func knownType(name string) bool {
	switch name {
	case "object", "array", "string", "number", "integer", "boolean", "null":
		return true
	}
	return false
}

func (s *Schema) validate(doc any, path string) error {
	if err := s.checkType(doc, path); err != nil {
		return err
	}
	if err := s.checkEnum(doc, path); err != nil {
		return err
	}
	if err := s.checkNumber(doc, path); err != nil {
		return err
	}
	if err := s.checkString(doc, path); err != nil {
		return err
	}
	if err := s.checkArray(doc, path); err != nil {
		return err
	}
	if err := s.checkObject(doc, path); err != nil {
		return err
	}
	return nil
}

func (s *Schema) checkType(doc any, path string) error {
	allowed := typeNames(s.raw["type"])
	if len(allowed) == 0 {
		return nil
	}
	for _, name := range allowed {
		if matchesType(name, doc) {
			return nil
		}
	}
	return fmt.Errorf("%s: expected %s, got %s", loc(path), strings.Join(allowed, " or "), jsonType(doc))
}

func (s *Schema) checkEnum(doc any, path string) error {
	values, ok := s.raw["enum"].([]any)
	if !ok {
		return nil
	}
	for _, candidate := range values {
		if jsonEqual(candidate, doc) {
			return nil
		}
	}
	return fmt.Errorf("%s: %s is not one of %s", loc(path), compact(doc), compact(values))
}

func (s *Schema) checkNumber(doc any, path string) error {
	n, ok := toFloat(doc)
	if !ok {
		return nil
	}
	if min, ok := toFloat(s.raw["minimum"]); ok && n < min {
		return fmt.Errorf("%s: %v is less than minimum %v", loc(path), n, min)
	}
	if max, ok := toFloat(s.raw["maximum"]); ok && n > max {
		return fmt.Errorf("%s: %v is greater than maximum %v", loc(path), n, max)
	}
	return nil
}

func (s *Schema) checkString(doc any, path string) error {
	str, ok := doc.(string)
	if !ok {
		return nil
	}
	length := len([]rune(str))
	if min, ok := toFloat(s.raw["minLength"]); ok && float64(length) < min {
		return fmt.Errorf("%s: length %d is less than minLength %v", loc(path), length, min)
	}
	if max, ok := toFloat(s.raw["maxLength"]); ok && float64(length) > max {
		return fmt.Errorf("%s: length %d is greater than maxLength %v", loc(path), length, max)
	}
	return nil
}

func (s *Schema) checkArray(doc any, path string) error {
	arr, ok := doc.([]any)
	if !ok {
		return nil
	}
	if min, ok := toFloat(s.raw["minItems"]); ok && float64(len(arr)) < min {
		return fmt.Errorf("%s: has %d items, fewer than minItems %v", loc(path), len(arr), min)
	}
	if max, ok := toFloat(s.raw["maxItems"]); ok && float64(len(arr)) > max {
		return fmt.Errorf("%s: has %d items, more than maxItems %v", loc(path), len(arr), max)
	}
	if items, ok := s.raw["items"].(map[string]any); ok {
		for i, item := range arr {
			if err := (&Schema{raw: items}).validate(item, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Schema) checkObject(doc any, path string) error {
	obj, ok := doc.(map[string]any)
	if !ok {
		return nil
	}
	if req, ok := s.raw["required"].([]any); ok {
		for _, item := range req {
			name, ok := item.(string)
			if !ok {
				continue
			}
			if _, present := obj[name]; !present {
				return fmt.Errorf("%s: missing required property %q", loc(path), name)
			}
		}
	}
	props, _ := s.raw["properties"].(map[string]any)
	for name, sub := range props {
		value, present := obj[name]
		if !present {
			continue
		}
		child, ok := sub.(map[string]any)
		if !ok {
			continue
		}
		if err := (&Schema{raw: child}).validate(value, joinPath(path, name)); err != nil {
			return err
		}
	}
	if extra, ok := s.raw["additionalProperties"].(bool); ok && !extra {
		var unknown []string
		for name := range obj {
			if _, defined := props[name]; !defined {
				unknown = append(unknown, name)
			}
		}
		if len(unknown) > 0 {
			sort.Strings(unknown)
			return fmt.Errorf("%s: unexpected property %q", loc(path), unknown[0])
		}
	}
	return nil
}

func typeNames(v any) []string {
	switch t := v.(type) {
	case string:
		return []string{t}
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if name, ok := item.(string); ok {
				out = append(out, name)
			}
		}
		return out
	}
	return nil
}

func matchesType(name string, doc any) bool {
	switch name {
	case "object":
		_, ok := doc.(map[string]any)
		return ok
	case "array":
		_, ok := doc.([]any)
		return ok
	case "string":
		_, ok := doc.(string)
		return ok
	case "number":
		_, ok := toFloat(doc)
		return ok
	case "integer":
		n, ok := toFloat(doc)
		return ok && n == float64(int64(n))
	case "boolean":
		_, ok := doc.(bool)
		return ok
	case "null":
		return doc == nil
	}
	return false
}

func jsonType(doc any) string {
	switch doc.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case float64, int, int64:
		return "number"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return "unknown"
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

func jsonEqual(a, b any) bool {
	ab, errA := json.Marshal(a)
	bb, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		return false
	}
	return string(ab) == string(bb)
}

func compact(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

func loc(path string) string {
	if path == "" {
		return "$"
	}
	return "$" + path
}

func joinPath(path, name string) string {
	if path == "" {
		return "." + name
	}
	return path + "." + name
}
