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

// RunPipeline executes a pipeline's steps in declared order.
//
// Agent steps always run and store their output under their output name (keyed
// by step id). A router step evaluates its input and selects one successor;
// the other branch targets are skipped for this run. Template references to a
// skipped step's output would be unresolved, so branch pipelines should omit
// the top-level `output` — in that case the result is the last agent step that
// ran, which is exactly "whichever branch executed".
//
// Inputs are the declared pipeline inputs by name. Every step's outcome is
// returned so the caller can persist per-step results.
func (e *Engine) RunPipeline(ctx context.Context, pipelineID string, inputs map[string]string) (PipelineResult, error) {
	start := time.Now()

	p, ok := e.registry.Pipeline(pipelineID)
	if !ok {
		return PipelineResult{}, fmt.Errorf("engine: unknown pipeline %q", pipelineID)
	}

	scope := pipeline.Scope{Inputs: map[string]string{}, Outputs: map[string]string{}}
	for _, name := range p.Inputs {
		val, present := inputs[name]
		if !present {
			return PipelineResult{}, fmt.Errorf("engine: pipeline %q missing required input %q", pipelineID, name)
		}
		scope.Inputs[name] = val
	}

	steps := make(map[string]model.Step, len(p.Steps))
	for _, s := range p.Steps {
		steps[s.ID] = s
	}

	var outcomes []StepOutcome
	skipped := map[string]bool{}
	tokens := 0
	lastOutput := ""
	lastRan := false

	record := func(o StepOutcome) {
		outcomes = append(outcomes, o)
	}

	for i := range p.Steps {
		step := p.Steps[i]
		if skipped[step.ID] {
			record(StepOutcome{StepID: step.ID, Status: "SKIPPED"})
			continue
		}

		if step.Router {
			decoded, err := pipeline.Resolve(step.Input, scope)
			if err != nil {
				return PipelineResult{DurationMS: time.Since(start).Milliseconds(), Steps: outcomes},
					fmt.Errorf("engine: pipeline %q step %q: %w", pipelineID, step.ID, err)
			}
			gotoID, err := pipeline.PickRoute(step.Routes, decoded)
			if err != nil {
				return PipelineResult{DurationMS: time.Since(start).Milliseconds(), Steps: outcomes},
					fmt.Errorf("engine: pipeline %q step %q: %w", pipelineID, step.ID, err)
			}
			if _, ok := steps[gotoID]; !ok {
				return PipelineResult{DurationMS: time.Since(start).Milliseconds(), Steps: outcomes},
					fmt.Errorf("engine: pipeline %q step %q: goto %q is not a step", pipelineID, step.ID, gotoID)
			}
			for _, r := range step.Routes {
				if r.Goto != gotoID {
					skipped[r.Goto] = true
				}
			}
			record(StepOutcome{StepID: step.ID, Status: string(model.StatusCompleted)})
			continue
		}

		decoded, err := pipeline.Resolve(step.Input, scope)
		if err != nil {
			record(StepOutcome{StepID: step.ID, Status: string(model.StatusFailed), Error: err.Error()})
			return PipelineResult{DurationMS: time.Since(start).Milliseconds(), Steps: outcomes},
				fmt.Errorf("engine: pipeline %q step %q: %w", pipelineID, step.ID, err)
		}

		res, err := e.RunAgent(ctx, step.AgentID, decoded)
		if err != nil {
			record(StepOutcome{
				StepID: step.ID, Status: string(model.StatusFailed),
				Error: err.Error(), DurationMS: res.DurationMS,
			})
			return PipelineResult{DurationMS: time.Since(start).Milliseconds(), Steps: outcomes},
				fmt.Errorf("engine: pipeline %q step %q: %w", pipelineID, step.ID, err)
		}

		scope.Outputs[step.ID] = res.Output
		tokens += res.Tokens
		lastOutput, lastRan = res.Output, true
		record(StepOutcome{
			StepID: step.ID, Status: string(model.StatusCompleted),
			Output: res.Output, Tokens: res.Tokens, DurationMS: res.DurationMS,
		})
	}

	output := lastOutput
	if strings.TrimSpace(p.Output) != "" {
		resolved, err := pipeline.Resolve(p.Output, scope)
		if err != nil {
			return PipelineResult{DurationMS: time.Since(start).Milliseconds(), Steps: outcomes},
				fmt.Errorf("engine: pipeline %q output: %w", pipelineID, err)
		}
		output = resolved
	} else if !lastRan {
		return PipelineResult{DurationMS: time.Since(start).Milliseconds(), Steps: outcomes},
			fmt.Errorf("engine: pipeline %q produced no agent output", pipelineID)
	}

	return PipelineResult{
		PipelineID: pipelineID,
		Output:     output,
		Tokens:     tokens,
		DurationMS: time.Since(start).Milliseconds(),
		Steps:      outcomes,
	}, nil
}
