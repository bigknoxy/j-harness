package pipeline

import (
	"testing"

	"github.com/bigknoxy/j-harness/internal/model"
)

func TestEvaluateCondition(t *testing.T) {
	input := `{"category":"billing","priority":"high","tags":["a","b"],"empty":""}`

	cases := []struct {
		name string
		cond model.Condition
		want bool
	}{
		{"equals match", model.Condition{Field: "category", Equals: "billing"}, true},
		{"equals miss", model.Condition{Field: "category", Equals: "technical"}, false},
		{"not_equals", model.Condition{Field: "category", NotEquals: "technical"}, true},
		{"in match", model.Condition{Field: "priority", In: []any{"low", "high"}}, true},
		{"in miss", model.Condition{Field: "priority", In: []any{"low"}}, false},
		{"matches", model.Condition{Field: "category", Matches: `^bill`}, true},
		{"matches miss", model.Condition{Field: "category", Matches: `^tech`}, false},
		{"exists true", model.Condition{Field: "category", Exists: true}, true},
		{"exists empty", model.Condition{Field: "empty", Exists: true}, false},
		{"exists missing", model.Condition{Field: "nope", Exists: true}, false},
		{"equals missing field", model.Condition{Field: "nope", Equals: "x"}, false},
	}

	for _, tc := range cases {
		got, err := EvaluateCondition(tc.cond, input)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestEvaluateConditionBadInput(t *testing.T) {
	if _, err := EvaluateCondition(model.Condition{Field: "x", Equals: "y"}, "not json"); err == nil {
		t.Error("expected error for non-JSON input")
	}
}

func TestPickRoute(t *testing.T) {
	routes := []model.Route{
		{When: &model.Condition{Field: "category", Equals: "billing"}, Goto: "billing_reply"},
		{When: &model.Condition{Field: "category", Equals: "technical"}, Goto: "tech_reply"},
		{Default: true, Goto: "generic_reply"},
	}

	got, err := PickRoute(routes, `{"category":"billing"}`)
	if err != nil || got != "billing_reply" {
		t.Fatalf("billing: got %q err %v", got, err)
	}
	got, err = PickRoute(routes, `{"category":"other"}`)
	if err != nil || got != "generic_reply" {
		t.Fatalf("default: got %q err %v", got, err)
	}
	if _, err := PickRoute([]model.Route{{When: &model.Condition{Field: "x", Equals: "y"}, Goto: "z"}}, `{"x":"a"}`); err == nil {
		t.Error("expected error when no route matches and no default")
	}
}
