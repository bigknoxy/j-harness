package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bigknoxy/j-harness/internal/model"
	"github.com/bigknoxy/j-harness/internal/pipeline"
)

// PipelineResult is the outcome of running a pipeline.
type PipelineResult struct {
	PipelineID string
	Output     string
	Tokens     int
	DurationMS int64
	Steps      []StepOutcome
}

// StepOutcome records what happened to one step.
type StepOutcome struct {
	StepID     string
	Status     string
	Output     string
	Error      string
	Tokens     int
	DurationMS int64
}

// RunPipeline executes a pipeline as a DAG. A step is enabled once every step it
// depends on has completed; independent steps run concurrently, bounded by the
// engine's max-parallel limit. Dependencies are derived from template
// references, explicit `needs`, and router branch edges (see pipeline.Deps).
//
// Agent steps store their output under their step id. A router step evaluates
// its input and selects one successor; the other branch targets, and anything
// that only depends on them, are skipped for this run. Template references to a
// skipped step's output would be unresolved, so branch pipelines should omit the
// top-level `output` — in that case the result is the last agent step that ran
// (in declared order), which is exactly "whichever branch executed".
//
// Every step's outcome is returned, including skipped ones, so the caller can
// persist per-step results. Steps are returned in declared order.
func (e *Engine) RunPipeline(ctx context.Context, pipelineID string, inputs map[string]string) (PipelineResult, error) {
	start := time.Now()

	p, ok := e.registry.Pipeline(pipelineID)
	if !ok {
		return PipelineResult{}, fmt.Errorf("engine: unknown pipeline %q", pipelineID)
	}

	deps, err := pipeline.Deps(p)
	if err != nil {
		return PipelineResult{}, fmt.Errorf("engine: pipeline %q: %w", pipelineID, err)
	}

	scope := pipeline.Scope{Inputs: map[string]string{}, Outputs: map[string]string{}}
	for _, name := range p.Inputs {
		val, present := inputs[name]
		if !present {
			return PipelineResult{}, fmt.Errorf("engine: pipeline %q missing required input %q", pipelineID, name)
		}
		scope.Inputs[name] = val
	}

	order := make([]string, len(p.Steps))
	steps := make(map[string]model.Step, len(p.Steps))
	index := make(map[string]int, len(p.Steps))
	for i, s := range p.Steps {
		order[i] = s.ID
		steps[s.ID] = s
		index[s.ID] = i
	}

	state := make(map[string]string, len(p.Steps))
	results := make(map[string]StepOutcome, len(p.Steps))
	pending := make(map[string]bool, len(p.Steps))
	for _, s := range p.Steps {
		pending[s.ID] = true
	}

	type completion struct {
		stepID string
		out    StepOutcome
		gotoID string
	}
	done := make(chan completion)
	maxParallel := e.maxParallel
	if maxParallel < 1 {
		maxParallel = 1
	}
	sem := make(chan struct{}, maxParallel)

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	running := 0
	var failure error
	tokens := 0

	// depStatus reports whether a step's dependencies are all satisfied, or
	// whether the step must be skipped because a dependency was skipped/failed.
	depStatus := func(id string) (ready, skip bool) {
		skip = false
		for _, dep := range deps[id] {
			switch state[dep] {
			case string(model.StatusCompleted):
				// satisfied
			case string(model.StatusSkipped):
				return false, true
			case string(model.StatusFailed):
				return false, true
			default:
				return false, false
			}
		}
		return true, false
	}

	launch := func(step model.Step) {
		decoded, err := pipeline.Resolve(step.Input, scope)
		if err != nil {
			out := StepOutcome{StepID: step.ID, Status: string(model.StatusFailed), Error: err.Error()}
			results[step.ID] = out
			state[step.ID] = string(model.StatusFailed)
			if failure == nil {
				failure = fmt.Errorf("engine: pipeline %q step %q: %w", pipelineID, step.ID, err)
			}
			cancel()
			return
		}
		running++
		go func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			if step.Router {
				gotoID, rerr := pipeline.PickRoute(step.Routes, decoded)
				out := StepOutcome{StepID: step.ID, Status: string(model.StatusCompleted)}
				if rerr != nil {
					out = StepOutcome{StepID: step.ID, Status: string(model.StatusFailed), Error: rerr.Error()}
				}
				if rerr == nil {
					if _, ok := steps[gotoID]; !ok {
						out = StepOutcome{StepID: step.ID, Status: string(model.StatusFailed),
							Error: fmt.Sprintf("goto %q is not a step", gotoID)}
						rerr = fmt.Errorf("goto %q is not a step", gotoID)
					}
				}
				done <- completion{stepID: step.ID, out: out, gotoID: gotoID}
				return
			}
			res, aerr := e.RunAgent(runCtx, step.AgentID, decoded)
			out := StepOutcome{
				StepID: step.ID, Status: stepStatus(aerr), Output: res.Output,
				Error: errString(aerr), Tokens: res.Tokens, DurationMS: res.DurationMS,
			}
			done <- completion{stepID: step.ID, out: out}
		}()
	}

	for len(pending) > 0 {
		// Admit every runnable step (declared order for determinism). Once a
		// step has failed we stop admitting and only drain in-flight work.
		for _, id := range order {
			if failure != nil {
				break
			}
			if !pending[id] {
				continue
			}
			if state[id] == string(model.StatusSkipped) {
				delete(pending, id) // skipped by a branch decision
				results[id] = StepOutcome{StepID: id, Status: string(model.StatusSkipped)}
				continue
			}
			ready, skip := depStatus(id)
			if skip {
				state[id] = string(model.StatusSkipped)
				results[id] = StepOutcome{StepID: id, Status: string(model.StatusSkipped)}
				delete(pending, id)
				continue
			}
			if !ready {
				continue
			}
			delete(pending, id)
			launch(steps[id])
		}

		if len(pending) == 0 && running == 0 {
			break
		}
		if failure != nil && running == 0 {
			break
		}
		if running == 0 {
			// Every remaining step is blocked with no in-flight work; the
			// dependency graph should have been validated acyclic.
			return PipelineResult{DurationMS: time.Since(start).Milliseconds(), Steps: collectOutcomes(order, results)},
				fmt.Errorf("engine: pipeline %q stalled (unresolved dependencies)", pipelineID)
		}

		c := <-done
		running--
		state[c.stepID] = c.out.Status
		results[c.stepID] = c.out
		if c.out.Status == string(model.StatusCompleted) {
			if c.gotoID != "" {
				scope.Outputs[c.stepID] = ""
				for _, rt := range steps[c.stepID].Routes {
					if rt.Goto != c.gotoID {
						state[rt.Goto] = string(model.StatusSkipped)
					}
				}
			} else {
				scope.Outputs[c.stepID] = c.out.Output
				tokens += c.out.Tokens
			}
		} else {
			if failure == nil {
				failure = fmt.Errorf("engine: pipeline %q step %q: %s", pipelineID, c.stepID, c.out.Error)
			}
			cancel()
		}
	}

	outcomes := collectOutcomes(order, results)

	if failure != nil {
		return PipelineResult{DurationMS: time.Since(start).Milliseconds(), Tokens: tokens, Steps: outcomes}, failure
	}

	output, err := finalOutput(p, scope, order, steps, state)
	if err != nil {
		return PipelineResult{DurationMS: time.Since(start).Milliseconds(), Tokens: tokens, Steps: outcomes},
			fmt.Errorf("engine: pipeline %q %w", pipelineID, err)
	}

	return PipelineResult{
		PipelineID: pipelineID,
		Output:     output,
		Tokens:     tokens,
		DurationMS: time.Since(start).Milliseconds(),
		Steps:      outcomes,
	}, nil
}

// finalOutput resolves the pipeline's result: the explicit output template when
// set, otherwise the last agent step (declared order) that completed.
func finalOutput(p model.Pipeline, scope pipeline.Scope, order []string, steps map[string]model.Step, state map[string]string) (string, error) {
	if strings.TrimSpace(p.Output) != "" {
		resolved, err := pipeline.Resolve(p.Output, scope)
		if err != nil {
			return "", fmt.Errorf("output: %w", err)
		}
		return resolved, nil
	}
	last := ""
	found := false
	for _, id := range order {
		if state[id] != string(model.StatusCompleted) {
			continue
		}
		if steps[id].Router {
			continue
		}
		last = scope.Outputs[id]
		found = true
	}
	if !found {
		return "", fmt.Errorf("produced no agent output")
	}
	return last, nil
}

// collectOutcomes returns step outcomes in declared order.
func collectOutcomes(order []string, results map[string]StepOutcome) []StepOutcome {
	out := make([]StepOutcome, 0, len(order))
	for _, id := range order {
		if r, ok := results[id]; ok {
			out = append(out, r)
		} else {
			out = append(out, StepOutcome{StepID: id, Status: string(model.StatusSkipped)})
		}
	}
	return out
}
