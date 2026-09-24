// Package pipeline will hold DAG resolution, template resolution, and router
// conditions. This file implements the strict, non-evaluating template grammar
// used by both registry validation (load time) and step execution (runtime).
//
// Grammar (see docs/SCHEMA.md):
//
//	{{ inputs.<name> }}
//	{{ steps.<id>.output }}
//	{{ steps.<id>.output.<json.path> }}
//
// Nothing else is valid. There are no expressions, function calls, or shell
// expansion. Refs are resolved against a caller-supplied scope, never executed.
package pipeline

import (
	"fmt"
	"regexp"
	"strings"
)

// RefKind identifies what a template reference points at.
type RefKind int

const (
	// RefInput references a declared pipeline input.
	RefInput RefKind = iota
	// RefStepOutput references a prior step's named output.
	RefStepOutput
)

// Ref is one parsed template reference.
type Ref struct {
	Kind   RefKind
	Name   string   // RefInput: the input name
	StepID string   // RefStepOutput: the producing step id
	Path   []string // RefStepOutput: optional JSON path segments (may be empty)
	Raw    string   // the full "{{ ... }}" text as written
}

var (
	refPattern  = regexp.MustCompile(`\{\{\s*([^{}]+?)\s*\}\}`)
	idPattern   = regexp.MustCompile(`^[a-z0-9_-]+$`)
	pathPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
)

// ParseRefs extracts and validates every template reference in s. It returns an
// error if the string contains a malformed or stray `{{`/`}}` sequence.
func ParseRefs(s string) ([]Ref, error) {
	var refs []Ref
	for _, m := range refPattern.FindAllStringSubmatchIndex(s, -1) {
		raw := s[m[0]:m[1]]
		inner := strings.TrimSpace(s[m[2]:m[3]])
		ref, err := parseRef(inner, raw)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}

	// Any braces left after removing well-formed refs are malformed.
	if rest := refPattern.ReplaceAllString(s, ""); strings.ContainsAny(rest, "{}") {
		return nil, fmt.Errorf("malformed template %q: unbalanced or invalid braces", s)
	}
	return refs, nil
}

func parseRef(inner, raw string) (Ref, error) {
	parts := strings.Split(inner, ".")
	switch {
	case len(parts) == 2 && parts[0] == "inputs":
		if !idPattern.MatchString(parts[1]) {
			return Ref{}, fmt.Errorf("invalid input reference %q", raw)
		}
		return Ref{Kind: RefInput, Name: parts[1], Raw: raw}, nil

	case len(parts) >= 3 && parts[0] == "steps" && parts[2] == "output":
		if !idPattern.MatchString(parts[1]) {
			return Ref{}, fmt.Errorf("invalid step reference %q", raw)
		}
		path := parts[3:]
		for _, seg := range path {
			if !pathPattern.MatchString(seg) {
				return Ref{}, fmt.Errorf("invalid JSON path in reference %q", raw)
			}
		}
		return Ref{Kind: RefStepOutput, StepID: parts[1], Path: path, Raw: raw}, nil

	default:
		return Ref{}, fmt.Errorf("invalid template reference %q (want inputs.<name> or steps.<id>.output[.path])", raw)
	}
}
