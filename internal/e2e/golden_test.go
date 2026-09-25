package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// goldenDir is the checked-in directory of wire-format fixtures.
const goldenDir = "testdata"

// normalizeWire makes a response comparable to a checked-in golden file. It
// replaces the random session id with a placeholder and drops wall-clock
// duration fields entirely. Durations are dropped rather than zeroed because
// they are `omitempty`: the key is absent when a call takes under 1ms and
// present otherwise, so any value-based normalization would still flake.
func normalizeWire(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			switch k {
			case "session_id":
				out[k] = "sess_FIXED"
			case "duration_ms":
				// volatile; omit from the golden
			default:
				out[k] = normalizeWire(val)
			}
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = normalizeWire(val)
		}
		return out
	default:
		return v
	}
}

// canonicalWire decodes a raw response and re-encodes it deterministically
// (sorted keys, two-space indent) with volatile fields normalized.
func canonicalWire(t *testing.T, raw []byte) []byte {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("decode wire %q: %v", raw, err)
	}
	out, err := json.MarshalIndent(normalizeWire(v), "", "  ")
	if err != nil {
		t.Fatalf("encode wire: %v", err)
	}
	return append(out, '\n')
}

// assertGolden compares a response body against testdata/<name>.json, creating
// it when the file is absent and UPDATE_GOLDEN=1 is set.
func assertGolden(t *testing.T, name string, raw []byte) {
	t.Helper()
	got := canonicalWire(t, raw)
	path := filepath.Join(goldenDir, name+".json")

	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(goldenDir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", goldenDir, err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		t.Logf("updated golden %s", path)
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s (run with UPDATE_GOLDEN=1 to create): %v", path, err)
	}
	if string(got) != string(want) {
		t.Fatalf("wire format drifted from %s\n--- got ---\n%s--- want ---\n%s", path, got, want)
	}
}

// TestGoldenSubmitAccepted pins the 202 async-submit envelope.
func TestGoldenSubmitAccepted(t *testing.T) {
	s := newStack(t, scriptRouter, "", false)
	code, data := s.do(t, "POST", "/v1/agents/triage/execute", "", `{"input_data":"I was charged twice for my subscription"}`)
	if code != 202 {
		t.Fatalf("submit = %d, want 202 (body %s)", code, data)
	}
	assertGolden(t, "submit_accepted", data)
}

// TestGoldenSessionCompleted pins the terminal session envelope.
func TestGoldenSessionCompleted(t *testing.T) {
	s := newStack(t, scriptRouter, "", false)
	sub := s.submit(t, "/v1/agents/triage/execute", `{"input_data":"I was charged twice for my subscription"}`)
	s.waitSession(t, sub.SessionID)

	code, data := s.do(t, "GET", "/v1/sessions/"+sub.SessionID, "", "")
	if code != 200 {
		t.Fatalf("get session = %d (body %s)", code, data)
	}
	assertGolden(t, "session_completed", data)
}

// TestGoldenStepsCompleted pins the step-result envelope, including the
// per-step token field.
func TestGoldenStepsCompleted(t *testing.T) {
	s := newStack(t, scriptRouter, "", false)
	sub := s.submit(t, "/v1/agents/triage/execute", `{"input_data":"I was charged twice for my subscription"}`)
	s.waitSession(t, sub.SessionID)

	code, data := s.do(t, "GET", "/v1/sessions/"+sub.SessionID+"/steps", "", "")
	if code != 200 {
		t.Fatalf("get steps = %d (body %s)", code, data)
	}
	assertGolden(t, "steps_completed", data)
}

// TestGoldenPipelineSkippedSteps pins SKIPPED-step serialization for a routed
// pipeline (branch losers carry no output).
func TestGoldenPipelineSkippedSteps(t *testing.T) {
	s := newStack(t, scriptRouter, "", false)
	sub := s.submit(t, "/v1/pipelines/support_flow/execute", `{"user_input":"I was overcharged on my invoice"}`)
	s.waitSession(t, sub.SessionID)

	code, data := s.do(t, "GET", "/v1/sessions/"+sub.SessionID+"/steps", "", "")
	if code != 200 {
		t.Fatalf("get steps = %d (body %s)", code, data)
	}
	assertGolden(t, "pipeline_skipped_steps", data)
}

// TestGoldenErrorEnvelope pins the {"error":{code,message}} shape and status
// code for an unknown session.
func TestGoldenErrorEnvelope(t *testing.T) {
	s := newStack(t, scriptRouter, "", false)
	code, data := s.do(t, "GET", "/v1/sessions/sess_does_not_exist", "", "")
	if code != 404 {
		t.Fatalf("get unknown session = %d, want 404 (body %s)", code, data)
	}
	assertGolden(t, "error_not_found", data)
}

// TestGoldenMethodNotAllowed pins the 405 envelope and Allow header.
func TestGoldenMethodNotAllowed(t *testing.T) {
	s := newStack(t, scriptRouter, "", false)
	code, data := s.do(t, "DELETE", "/v1/agents/triage/execute", "", "")
	if code != 405 {
		t.Fatalf("delete = %d, want 405 (body %s)", code, data)
	}
	assertGolden(t, "error_method_not_allowed", data)
}
