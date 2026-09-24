package pipeline

import "testing"

func TestParseRefsValid(t *testing.T) {
	cases := []struct {
		in       string
		wantRefs int
		kind     RefKind
	}{
		{"{{ inputs.user_input }}", 1, RefInput},
		{"{{ steps.triage.output }}", 1, RefStepOutput},
		{"{{ steps.triage.output.category }}", 1, RefStepOutput},
		{"hello {{ inputs.a }} and {{ steps.s.output.x.y }}", 2, RefInput},
	}
	for _, tc := range cases {
		refs, err := ParseRefs(tc.in)
		if err != nil {
			t.Fatalf("ParseRefs(%q) unexpected error: %v", tc.in, err)
		}
		if len(refs) != tc.wantRefs {
			t.Fatalf("ParseRefs(%q) got %d refs, want %d", tc.in, len(refs), tc.wantRefs)
		}
	}
}

func TestParseRefsInvalid(t *testing.T) {
	bad := []string{
		"{{ inputs.UserInput }}",
		"{{ inputs. }}",
		"{{ steps.triage }}",
		"{{ steps.triage.result }}",
		"{{ env.SECRET }}",
		"{{ inputs.a",
		"{{ }}",
		"{{ inputs.a }}}",
		"{{ steps.triage.output.bad*path }}",
	}
	for _, in := range bad {
		if _, err := ParseRefs(in); err == nil {
			t.Errorf("ParseRefs(%q) expected error, got nil", in)
		}
	}
}
