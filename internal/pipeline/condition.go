package pipeline

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/bigknoxy/j-harness/internal/model"
)

// EvaluateCondition reports whether cond matches the given router input. The
// input is a JSON document (typically a prior step's output); cond.Field is a
// dotted JSON path into it.
func EvaluateCondition(cond model.Condition, input string) (bool, error) {
	var doc any
	if err := json.Unmarshal([]byte(strings.TrimSpace(input)), &doc); err != nil {
		return false, fmt.Errorf("router input is not JSON: %w", err)
	}
	value, found, err := walkPath(doc, cond.Field)
	if err != nil {
		return false, err
	}

	switch cond.Operator() {
	case model.OpExists:
		return found && stringify(value) != "", nil
	case model.OpEquals:
		return found && looseEqual(value, cond.Equals), nil
	case model.OpNotEquals:
		return found && !looseEqual(value, cond.NotEquals), nil
	case model.OpIn:
		if !found {
			return false, nil
		}
		for _, candidate := range cond.In {
			if looseEqual(value, candidate) {
				return true, nil
			}
		}
		return false, nil
	case model.OpMatches:
		if !found {
			return false, nil
		}
		re, err := regexp.Compile(cond.Matches)
		if err != nil {
			return false, fmt.Errorf("invalid matches pattern %q: %w", cond.Matches, err)
		}
		return re.MatchString(stringify(value)), nil
	default:
		return false, fmt.Errorf("condition has no single operator")
	}
}

// PickRoute returns the first route whose condition matches, else the default.
func PickRoute(routes []model.Route, input string) (string, error) {
	var fallback string
	var hasDefault bool
	for _, r := range routes {
		if r.Default {
			fallback, hasDefault = r.Goto, true
			continue
		}
		if r.When == nil {
			continue
		}
		ok, err := EvaluateCondition(*r.When, input)
		if err != nil {
			return "", err
		}
		if ok {
			return r.Goto, nil
		}
	}
	if hasDefault {
		return fallback, nil
	}
	return "", fmt.Errorf("router: no route matched and no default")
}

// walkPath descends a decoded JSON value by dotted path segments. Numeric
// segments index arrays. found is false when a key or index is absent.
func walkPath(doc any, path string) (value any, found bool, err error) {
	if path == "" {
		return doc, true, nil
	}
	cur := doc
	for _, seg := range strings.Split(path, ".") {
		switch node := cur.(type) {
		case map[string]any:
			v, ok := node[seg]
			if !ok {
				return nil, false, nil
			}
			cur = v
		case []any:
			idx, convErr := atoi(seg)
			if convErr != nil || idx < 0 || idx >= len(node) {
				return nil, false, nil
			}
			cur = node[idx]
		default:
			return nil, false, nil
		}
	}
	return cur, true, nil
}

// looseEqual compares a decoded JSON scalar against a condition operand. The
// operand comes from a Go `any` decoded from the blueprint, so numbers are
// float64; strings and bools compare directly.
func looseEqual(value, operand any) bool {
	if s, ok := operand.(string); ok {
		return stringify(value) == s
	}
	vb, verr := json.Marshal(value)
	ob, oerr := json.Marshal(operand)
	if verr != nil || oerr != nil {
		return false
	}
	return string(vb) == string(ob)
}

func atoi(s string) (int, error) {
	n := 0
	if s == "" {
		return 0, fmt.Errorf("empty")
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("not a number")
		}
		n = n*10 + int(r-'0')
	}
	return n, nil
}
