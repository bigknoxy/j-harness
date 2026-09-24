package pipeline

import (
	"fmt"

	"github.com/bigknoxy/j-harness/internal/model"
)

// Deps computes the dependency edges of a pipeline: for each step id it returns
// the ids of the steps that must finish before it can run. Edges come from three
// sources:
//
//   - template references in the step's input ({{ steps.<id>.output }})
//   - explicit needs entries
//   - router branches: every goto target depends on its router step
//
// The returned graph includes an entry for every step (possibly empty). It
// returns an error if a reference or needs entry names a step that does not
// exist, a step needs itself, or the graph contains a cycle.
func Deps(p model.Pipeline) (map[string][]string, error) {
	ids := make(map[string]bool, len(p.Steps))
	for _, s := range p.Steps {
		ids[s.ID] = true
	}

	deps := make(map[string][]string, len(p.Steps))
	seen := make(map[string]map[string]bool, len(p.Steps))
	add := func(from, to string) {
		if seen[to] == nil {
			seen[to] = map[string]bool{}
		}
		if seen[to][from] {
			return
		}
		seen[to][from] = true
		deps[to] = append(deps[to], from)
	}

	for _, s := range p.Steps {
		if _, ok := deps[s.ID]; !ok {
			deps[s.ID] = nil
		}
		refs, err := ParseRefs(s.Input)
		if err != nil {
			return nil, fmt.Errorf("step %q input: %w", s.ID, err)
		}
		for _, ref := range refs {
			if ref.Kind != RefStepOutput {
				continue
			}
			if !ids[ref.StepID] {
				return nil, fmt.Errorf("step %q references unknown step %q", s.ID, ref.StepID)
			}
			add(ref.StepID, s.ID)
		}
		for _, need := range s.Needs {
			if need == s.ID {
				return nil, fmt.Errorf("step %q needs itself", s.ID)
			}
			if !ids[need] {
				return nil, fmt.Errorf("step %q needs unknown step %q", s.ID, need)
			}
			add(need, s.ID)
		}
		if s.Router {
			for _, rt := range s.Routes {
				if !ids[rt.Goto] {
					return nil, fmt.Errorf("router step %q: goto %q is not a step", s.ID, rt.Goto)
				}
				add(s.ID, rt.Goto)
			}
		}
	}

	if err := checkAcyclic(p.Steps, deps); err != nil {
		return nil, err
	}
	return deps, nil
}

// checkAcyclic reports the first cycle found in the dependency graph.
func checkAcyclic(steps []model.Step, deps map[string][]string) error {
	const (
		white = 0 // unvisited
		gray  = 1 // on the current path
		black = 2 // fully explored
	)
	color := make(map[string]int, len(steps))
	var visit func(id string) error
	visit = func(id string) error {
		color[id] = gray
		for _, dep := range deps[id] {
			switch color[dep] {
			case gray:
				return fmt.Errorf("dependency cycle involving step %q", dep)
			case white:
				if err := visit(dep); err != nil {
					return err
				}
			}
		}
		color[id] = black
		return nil
	}
	for _, s := range steps {
		if color[s.ID] == white {
			if err := visit(s.ID); err != nil {
				return err
			}
		}
	}
	return nil
}
