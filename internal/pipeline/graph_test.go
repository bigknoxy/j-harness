package pipeline

import (
	"strings"
	"testing"

	"github.com/bigknoxy/j-harness/internal/model"
)

func TestDepsDerivedFromRefsNeedsAndRoutes(t *testing.T) {
	p := model.Pipeline{
		PipelineID: "diamond", Version: 1, Inputs: []string{"user_input"},
		Steps: []model.Step{
			{ID: "start", AgentID: "triage", Input: "{{ inputs.user_input }}", Output: "c"},
			{ID: "left", AgentID: "triage", Input: "{{ steps.start.output }}", Output: "l"},
			{ID: "right", AgentID: "triage", Input: "{{ steps.start.output }}", Output: "r"},
			{ID: "join", AgentID: "triage", Input: "{{ steps.left.output }} {{ steps.right.output }}", Output: "j"},
			{ID: "route", Router: true, Input: "{{ steps.join.output }}",
				Routes: []model.Route{{When: &model.Condition{Field: "x", Equals: "y"}, Goto: "after"}, {Default: true, Goto: "after"}}},
			{ID: "after", AgentID: "triage", Input: "{{ steps.route.output }}", Output: "a", Needs: []string{"join"}},
		},
	}
	deps, err := Deps(p)
	if err != nil {
		t.Fatalf("Deps: %v", err)
	}
	want := map[string][]string{
		"start": nil,
		"left":  {"start"},
		"right": {"start"},
		"join":  {"left", "right"},
		"route": {"join"},
		"after": {"route", "join"}, // route edge + explicit needs, deduped
	}
	for id, exp := range want {
		got := deps[id]
		if len(got) != len(exp) {
			t.Fatalf("deps[%s] = %v, want %v", id, got, exp)
		}
		for i := range exp {
			if got[i] != exp[i] {
				t.Errorf("deps[%s][%d] = %q, want %q (full %v)", id, i, got[i], exp[i], got)
			}
		}
	}
}

func TestDepsRejectsCycle(t *testing.T) {
	p := model.Pipeline{
		PipelineID: "c", Version: 1,
		Steps: []model.Step{
			{ID: "a", AgentID: "triage", Input: "{{ steps.b.output }}", Output: "x"},
			{ID: "b", AgentID: "triage", Input: "{{ steps.a.output }}", Output: "y"},
		},
	}
	if _, err := Deps(p); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected cycle error, got %v", err)
	}
}

func TestDepsRejectsUnknownRefAndNeeds(t *testing.T) {
	both := []model.Pipeline{
		{PipelineID: "r", Version: 1, Steps: []model.Step{
			{ID: "a", AgentID: "triage", Input: "{{ steps.ghost.output }}", Output: "x"}}},
		{PipelineID: "n", Version: 1, Steps: []model.Step{
			{ID: "a", AgentID: "triage", Input: "hi", Output: "x", Needs: []string{"ghost"}}}},
		{PipelineID: "s", Version: 1, Steps: []model.Step{
			{ID: "a", AgentID: "triage", Input: "hi", Output: "x", Needs: []string{"a"}}}},
	}
	for _, p := range both {
		if _, err := Deps(p); err == nil {
			t.Errorf("pipeline %s: expected error", p.PipelineID)
		}
	}
}
