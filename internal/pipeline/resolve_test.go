package pipeline

import "testing"

func TestResolveInputsAndSteps(t *testing.T) {
	scope := Scope{
		Inputs:  map[string]string{"user_input": "hello world", "name": "Ada"},
		Outputs: map[string]string{"triage": `{"category":"billing","priority":"high"}`},
	}

	cases := []struct {
		in   string
		want string
	}{
		{"{{ inputs.user_input }}", "hello world"},
		{"Hi {{ inputs.name }}!", "Hi Ada!"},
		{"{{ inputs.name }} {{ inputs.name }}", "Ada Ada"},
		{"{{ steps.triage.output }}", `{"category":"billing","priority":"high"}`},
		{"{{ steps.triage.output.category }}", "billing"},
		{"cat={{ steps.triage.output.category }} pri={{ steps.triage.output.priority }}", "cat=billing pri=high"},
		{"literal only", "literal only"},
		{"", ""},
	}

	for _, tc := range cases {
		got, err := Resolve(tc.in, scope)
		if err != nil {
			t.Fatalf("Resolve(%q) error: %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("Resolve(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestResolveErrors(t *testing.T) {
	scope := Scope{
		Inputs:  map[string]string{"a": "1"},
		Outputs: map[string]string{"s1": `{"k":"v"}`, "s2": "not json"},
	}

	cases := []string{
		"{{ inputs.missing }}",
		"{{ steps.nope.output }}",
		"{{ steps.s1.output.absent }}",
		"{{ steps.s2.output.k }}",
		"{{ inputs.a }}}",
		"{{ bogus }}",
	}
	for _, in := range cases {
		if _, err := Resolve(in, scope); err == nil {
			t.Errorf("Resolve(%q) expected error, got nil", in)
		}
	}
}

func TestResolvePathTypes(t *testing.T) {
	scope := Scope{Outputs: map[string]string{"s": `{"n":3.0,"b":true,"arr":[{"x":"y"}],"f":1.5}`}}

	cases := []struct{ in, want string }{
		{"{{ steps.s.output.n }}", "3"},
		{"{{ steps.s.output.b }}", "true"},
		{"{{ steps.s.output.arr.0.x }}", "y"},
		{"{{ steps.s.output.f }}", "1.5"},
	}
	for _, tc := range cases {
		got, err := Resolve(tc.in, scope)
		if err != nil {
			t.Fatalf("Resolve(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("Resolve(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
