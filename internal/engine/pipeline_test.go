package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/bigknoxy/j-harness/internal/llm"
)

func TestRunPipelineRoutesBilling(t *testing.T) {
	eng, fake := testEngine(t,
		llm.Response{Content: `{"category":"billing","priority":"high"}`, TotalTokens: 10},
		llm.Response{Content: "billing reply text", TotalTokens: 20},
	)

	res, err := eng.RunPipeline(context.Background(), "support_flow", map[string]string{"user_input": "I was overcharged"})
	if err != nil {
		t.Fatalf("RunPipeline: %v", err)
	}
	if res.Output != "billing reply text" {
		t.Errorf("output = %q", res.Output)
	}
	if res.Tokens != 30 {
		t.Errorf("tokens = %d, want 30", res.Tokens)
	}
	if fake.CallCount() != 2 {
		t.Errorf("client calls = %d, want 2 (triage + one reply branch)", fake.CallCount())
	}

	statuses := map[string]string{}
	for _, s := range res.Steps {
		statuses[s.StepID] = s.Status
	}
	for _, id := range []string{"triage", "route", "billing_reply"} {
		if statuses[id] != "COMPLETED" {
			t.Errorf("step %s status = %q, want COMPLETED", id, statuses[id])
		}
	}
	for _, id := range []string{"tech_reply", "generic_reply"} {
		if statuses[id] != "SKIPPED" {
			t.Errorf("step %s status = %q, want SKIPPED", id, statuses[id])
		}
	}
}

func TestRunPipelineDefaultsToGeneric(t *testing.T) {
	eng, fake := testEngine(t,
		llm.Response{Content: `{"category":"other","priority":"low"}`},
		llm.Response{Content: "generic reply text"},
	)
	res, err := eng.RunPipeline(context.Background(), "support_flow", map[string]string{"user_input": "hi"})
	if err != nil {
		t.Fatalf("RunPipeline: %v", err)
	}
	if res.Output != "generic reply text" {
		t.Errorf("output = %q", res.Output)
	}
	if fake.CallCount() != 2 {
		t.Errorf("client calls = %d, want 2", fake.CallCount())
	}
}

func TestRunPipelineUnknownPipeline(t *testing.T) {
	eng, _ := testEngine(t)
	if _, err := eng.RunPipeline(context.Background(), "nope", nil); err == nil {
		t.Fatal("expected error for unknown pipeline")
	}
}

func TestRunPipelineMissingInput(t *testing.T) {
	eng, _ := testEngine(t)
	if _, err := eng.RunPipeline(context.Background(), "support_flow", map[string]string{}); err == nil {
		t.Fatal("expected error for missing input")
	}
}

func TestRunPipelineStepFailure(t *testing.T) {
	eng, _ := testEngine(t, llm.Response{Content: "not json at all"})
	res, err := eng.RunPipeline(context.Background(), "support_flow", map[string]string{"user_input": "hi"})
	if err == nil {
		t.Fatal("expected error when triage returns invalid JSON")
	}
	if !strings.Contains(err.Error(), "triage") {
		t.Errorf("error should name the failing step: %v", err)
	}
	if len(res.Steps) == 0 || res.Steps[0].Status != "FAILED" {
		t.Errorf("expected failed first step, got %+v", res.Steps)
	}
}
