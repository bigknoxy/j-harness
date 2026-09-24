package model

import "testing"

func TestConditionOperator(t *testing.T) {
	cases := []struct {
		name string
		cond Condition
		want string
	}{
		{"equals", Condition{Field: "c", Equals: "billing"}, OpEquals},
		{"not_equals", Condition{Field: "c", NotEquals: "x"}, OpNotEquals},
		{"in", Condition{Field: "c", In: []any{"a", "b"}}, OpIn},
		{"matches", Condition{Field: "c", Matches: "^a"}, OpMatches},
		{"exists", Condition{Field: "c", Exists: true}, OpExists},
		{"none", Condition{Field: "c"}, ""},
		{"multiple", Condition{Field: "c", Equals: 1, Matches: "x"}, ""},
	}
	for _, tc := range cases {
		if got := tc.cond.Operator(); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}
