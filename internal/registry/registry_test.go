package registry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/bigknoxy/j-harness/internal/model"
)

// writeFixture creates a minimal valid registry and returns its root.
func writeFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, BlueprintsDir))
	mustMkdir(t, filepath.Join(root, PromptsDir))
	mustMkdir(t, filepath.Join(root, PipelinesDir))

	mustWrite(t, filepath.Join(root, PromptsDir, "triage.md"), "classify")
	mustWriteJSON(t, filepath.Join(root, BlueprintsDir, "triage.json"), model.AgentBlueprint{
		ID: "triage", PromptPath: "prompts/triage.md", Model: "llama3.2",
		OutputFormat: model.OutputJSON, Version: 1,
	})
	mustWriteJSON(t, filepath.Join(root, PipelinesDir, "flow.json"), model.Pipeline{
		PipelineID: "flow", Version: 1, Inputs: []string{"user_input"},
		Steps: []model.Step{
			{ID: "triage", AgentID: "triage", Input: "{{ inputs.user_input }}", Output: "classification"},
		},
		Output: "{{ steps.triage.output }}",
	})
	return root
}

func TestLoadValid(t *testing.T) {
	r, err := Load(writeFixture(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := r.Blueprint("triage"); !ok {
		t.Error("missing blueprint triage")
	}
	if p, ok := r.Prompt("triage"); !ok || p != "classify" {
		t.Errorf("prompt = %q, %v", p, ok)
	}
	if _, ok := r.Pipeline("flow"); !ok {
		t.Error("missing pipeline flow")
	}
}

func TestLoadRejectsUnknownField(t *testing.T) {
	root := writeFixture(t)
	mustWrite(t, filepath.Join(root, BlueprintsDir, "bad.json"),
		`{"id":"bad","prompt_path":"prompts/triage.md","model":"m","version":1,"bogus":true}`)
	if _, err := Load(root); err == nil {
		t.Fatal("expected error for unknown field")
	}
}

func TestLoadRejectsBadID(t *testing.T) {
	root := writeFixture(t)
	mustWriteJSON(t, filepath.Join(root, BlueprintsDir, "bad.json"), model.AgentBlueprint{
		ID: "Bad_ID", PromptPath: "prompts/triage.md", Model: "m", Version: 1,
	})
	if _, err := Load(root); err == nil {
		t.Fatal("expected error for invalid id")
	}
}

func TestLoadRejectsIDFilenameMismatch(t *testing.T) {
	root := writeFixture(t)
	mustWriteJSON(t, filepath.Join(root, BlueprintsDir, "mismatch.json"), model.AgentBlueprint{
		ID: "other", PromptPath: "prompts/triage.md", Model: "m", Version: 1,
	})
	if _, err := Load(root); err == nil {
		t.Fatal("expected error for id/filename mismatch")
	}
}

func TestLoadRejectsMissingPrompt(t *testing.T) {
	root := writeFixture(t)
	mustWriteJSON(t, filepath.Join(root, BlueprintsDir, "noprompt.json"), model.AgentBlueprint{
		ID: "noprompt", PromptPath: "prompts/missing.md", Model: "m", Version: 1,
	})
	if _, err := Load(root); err == nil {
		t.Fatal("expected error for missing prompt file")
	}
}

func TestLoadRejectsTraversal(t *testing.T) {
	root := writeFixture(t)
	mustWriteJSON(t, filepath.Join(root, BlueprintsDir, "evil.json"), model.AgentBlueprint{
		ID: "evil", PromptPath: "../../etc/passwd", Model: "m", Version: 1,
	})
	if _, err := Load(root); err == nil {
		t.Fatal("expected error for path traversal")
	}
}

func TestLoadRejectsBadVersion(t *testing.T) {
	root := writeFixture(t)
	mustWriteJSON(t, filepath.Join(root, BlueprintsDir, "old.json"), model.AgentBlueprint{
		ID: "old", PromptPath: "prompts/triage.md", Model: "m", Version: 99,
	})
	if _, err := Load(root); err == nil {
		t.Fatal("expected error for unsupported version")
	}
}

func TestPipelineForwardRefAllowed(t *testing.T) {
	root := writeFixture(t)
	mustWriteJSON(t, filepath.Join(root, PipelinesDir, "fwd.json"), model.Pipeline{
		PipelineID: "fwd", Version: 1,
		Steps: []model.Step{
			{ID: "a", AgentID: "triage", Input: "{{ steps.b.output }}", Output: "x"},
			{ID: "b", AgentID: "triage", Input: "hi", Output: "b_out"},
		},
	})
	r, err := Load(root)
	if err != nil {
		t.Fatalf("expected forward reference to load as a DAG edge: %v", err)
	}
	if _, ok := r.Pipeline("fwd"); !ok {
		t.Error("missing pipeline fwd")
	}
}

func TestPipelineCycleRejected(t *testing.T) {
	root := writeFixture(t)
	mustWriteJSON(t, filepath.Join(root, PipelinesDir, "cycle.json"), model.Pipeline{
		PipelineID: "cycle", Version: 1, Inputs: []string{"user_input"},
		Steps: []model.Step{
			{ID: "a", AgentID: "triage", Input: "{{ steps.b.output }}", Output: "x"},
			{ID: "b", AgentID: "triage", Input: "{{ steps.a.output }}", Output: "y"},
		},
	})
	if _, err := Load(root); err == nil {
		t.Fatal("expected error for dependency cycle")
	}
}

func TestPipelineRejectsUnknownStepRef(t *testing.T) {
	root := writeFixture(t)
	mustWriteJSON(t, filepath.Join(root, PipelinesDir, "unknownstep.json"), model.Pipeline{
		PipelineID: "unknownstep", Version: 1, Inputs: []string{"user_input"},
		Steps: []model.Step{
			{ID: "a", AgentID: "triage", Input: "{{ steps.ghost.output }}", Output: "x"},
		},
	})
	if _, err := Load(root); err == nil {
		t.Fatal("expected error for reference to unknown step")
	}
}

func TestPipelineRejectsUnknownNeeds(t *testing.T) {
	root := writeFixture(t)
	mustWriteJSON(t, filepath.Join(root, PipelinesDir, "badneeds.json"), model.Pipeline{
		PipelineID: "badneeds", Version: 1, Inputs: []string{"user_input"},
		Steps: []model.Step{
			{ID: "a", AgentID: "triage", Input: "hi", Output: "x", Needs: []string{"ghost"}},
		},
	})
	if _, err := Load(root); err == nil {
		t.Fatal("expected error for unknown needs entry")
	}
}

func TestPipelineUnknownInput(t *testing.T) {
	root := writeFixture(t)
	mustWriteJSON(t, filepath.Join(root, PipelinesDir, "unknown.json"), model.Pipeline{
		PipelineID: "unknown", Version: 1, Inputs: []string{"a"},
		Steps: []model.Step{
			{ID: "s", AgentID: "triage", Input: "{{ inputs.b }}", Output: "x"},
		},
	})
	if _, err := Load(root); err == nil {
		t.Fatal("expected error for unknown input")
	}
}

func TestWriteBlueprintThenLoad(t *testing.T) {
	root := writeFixture(t)
	// Build a registry, then write a new blueprint via the API and reload.
	r, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	temp := 0.2
	bp := model.AgentBlueprint{
		ID: "summarizer", PromptPath: "prompts/summarizer.md", Model: "llama3.2",
		Temperature: &temp, Version: 1,
	}
	if err := r.WriteBlueprint(bp, "summarize this"); err != nil {
		t.Fatalf("WriteBlueprint: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, BlueprintsDir, "summarizer.json")); err != nil {
		t.Fatalf("blueprint file not written: %v", err)
	}
	r2, err := Load(root)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if _, ok := r2.Blueprint("summarizer"); !ok {
		t.Error("reloaded registry missing summarizer")
	}
	if p, ok := r2.Prompt("summarizer"); !ok || p != "summarize this" {
		t.Errorf("prompt = %q, %v", p, ok)
	}
}

func TestWriteBlueprintRequiresExistingPromptWhenEmpty(t *testing.T) {
	root := writeFixture(t)
	r, _ := Load(root)
	bp := model.AgentBlueprint{ID: "x", PromptPath: "prompts/x.md", Model: "m", Version: 1}
	if err := r.WriteBlueprint(bp, ""); err == nil {
		t.Fatal("expected error when prompt is empty and file is missing")
	}
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustWriteJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, path, string(data))
}
