package tools

import "testing"

func TestNewRejectsUnknown(t *testing.T) {
	if _, err := New("current_time", "nope"); err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestRegistryGetAndNames(t *testing.T) {
	r, err := New("word_count", "math_eval")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if r.Len() != 2 {
		t.Fatalf("Len = %d, want 2", r.Len())
	}
	if _, ok := r.Get("word_count"); !ok {
		t.Fatal("word_count missing")
	}
	names := r.Names()
	if len(names) != 2 || names[0] != "math_eval" || names[1] != "word_count" {
		t.Fatalf("Names = %v, want sorted [math_eval word_count]", names)
	}
}

func TestNilRegistry(t *testing.T) {
	var r *Registry
	if _, ok := r.Get("word_count"); ok {
		t.Fatal("nil registry should not resolve tools")
	}
	if r.Len() != 0 || r.Names() != nil {
		t.Fatal("nil registry should be empty")
	}
}

func TestWordCount(t *testing.T) {
	r, _ := New("word_count")
	fn, _ := r.Get("word_count")
	got, err := fn.Run(nil, `{"text":"one two   three"}`)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got != "3" {
		t.Fatalf("got %q, want 3", got)
	}
}

func TestMathEval(t *testing.T) {
	r, _ := New("math_eval")
	fn, _ := r.Get("math_eval")
	cases := map[string]string{
		`{"expression":"2+3*4"}`:       "14",
		`{"expression":"(2+3)*4"}`:     "20",
		`{"expression":"10/4"}`:        "2.5",
		`{"expression":"-3 + 5"}`:      "2",
		`{"expression":"2*(3+(4-1))"}`: "12",
	}
	for args, want := range cases {
		got, err := fn.Run(nil, args)
		if err != nil {
			t.Fatalf("%s: %v", args, err)
		}
		if got != want {
			t.Fatalf("%s = %q, want %q", args, got, want)
		}
	}
}

func TestMathEvalErrors(t *testing.T) {
	r, _ := New("math_eval")
	fn, _ := r.Get("math_eval")
	for _, args := range []string{`{"expression":"1/0"}`, `{"expression":"2+"}`, `{"expression":"abc"}`} {
		if _, err := fn.Run(nil, args); err == nil {
			t.Fatalf("%s: expected error", args)
		}
	}
}

func TestCurrentTime(t *testing.T) {
	r, _ := New("current_time")
	fn, _ := r.Get("current_time")
	got, err := fn.Run(nil, "")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got == "" {
		t.Fatal("empty time")
	}
}
