package engine

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bigknoxy/j-harness/internal/llm"
	"github.com/bigknoxy/j-harness/internal/registry"
)

func testEngine(t *testing.T, responses ...llm.Response) (*Engine, *llm.Fake) {
	t.Helper()
	reg, err := registry.Load(filepath.Join("..", "..", "agent-registry"))
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	fake := llm.NewFake(responses...)
	eng, err := New(reg, fake)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return eng, fake
}

func TestRunAgentText(t *testing.T) {
	eng, fake := testEngine(t, llm.Response{Content: "  a reply  ", TotalTokens: 12})
	res, err := eng.RunAgent(context.Background(), "generic_agent", "help me")
	if err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	if res.Output != "a reply" {
		t.Errorf("output = %q", res.Output)
	}
	if res.Tokens != 12 {
		t.Errorf("tokens = %d", res.Tokens)
	}
	if fake.Requests[0].SystemPrompt == "" {
		t.Error("expected system prompt from prompt file")
	}
	if fake.Requests[0].SystemPrompt == "" || !strings.Contains(fake.Requests[0].SystemPrompt, "support responder") {
		t.Errorf("unexpected system prompt: %q", fake.Requests[0].SystemPrompt)
	}
}

func TestRunAgentJSONValid(t *testing.T) {
	eng, _ := testEngine(t, llm.Response{Content: "```json\n{\"category\":\"billing\",\"priority\":\"high\"}\n```"})
	res, err := eng.RunAgent(context.Background(), "triage", "my bill is wrong")
	if err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	if !strings.Contains(res.Output, "billing") {
		t.Errorf("output = %q", res.Output)
	}
}

func TestRunAgentSchemaRepair(t *testing.T) {
	eng, fake := testEngine(t,
		llm.Response{Content: `{"category":"billing"}`, TotalTokens: 10},
		llm.Response{Content: `{"category":"billing","priority":"high"}`, TotalTokens: 5},
	)
	res, err := eng.RunAgent(context.Background(), "triage", "my bill is wrong")
	if err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	if !strings.Contains(res.Output, "priority") {
		t.Errorf("repaired output = %q", res.Output)
	}
	if res.Tokens != 15 {
		t.Errorf("tokens = %d, want 15", res.Tokens)
	}
	if fake.CallCount() != 2 {
		t.Errorf("calls = %d, want 2", fake.CallCount())
	}
	if len(fake.Requests[1].Messages) == 0 {
		t.Error("repair request should carry the conversation")
	}
}

func TestRunAgentSchemaRepairFails(t *testing.T) {
	eng, fake := testEngine(t,
		llm.Response{Content: `{"category":"billing"}`},
		llm.Response{Content: `{"category":"billing"}`},
	)
	if _, err := eng.RunAgent(context.Background(), "triage", "x"); err == nil {
		t.Fatal("expected schema error after failed repair")
	}
	if fake.CallCount() != 2 {
		t.Errorf("calls = %d, want 2 (one repair attempt)", fake.CallCount())
	}
}

func TestRunAgentJSONInvalid(t *testing.T) {
	eng, _ := testEngine(t, llm.Response{Content: "not json at all"})
	if _, err := eng.RunAgent(context.Background(), "triage", "x"); err == nil {
		t.Fatal("expected error for invalid JSON output")
	}
}

func TestRunAgentJSONNullRejected(t *testing.T) {
	// A bare `null` unmarshals into a nil map without error, but it is not a
	// JSON object, so it must be rejected.
	eng, _ := testEngine(t, llm.Response{Content: "null"})
	if _, err := eng.RunAgent(context.Background(), "triage", "x"); err == nil {
		t.Fatal("expected error for null JSON output")
	}
}

func TestRunAgentUnknown(t *testing.T) {
	eng, _ := testEngine(t)
	if _, err := eng.RunAgent(context.Background(), "nope", "x"); err == nil {
		t.Fatal("expected error for unknown agent")
	}
}

func TestRunAgentClientError(t *testing.T) {
	reg, err := registry.Load(filepath.Join("..", "..", "agent-registry"))
	if err != nil {
		t.Fatal(err)
	}
	eng, _ := New(reg, llm.NewFakeErr(context.DeadlineExceeded))
	if _, err := eng.RunAgent(context.Background(), "generic_agent", "x"); err == nil {
		t.Fatal("expected error from client")
	}
}

func TestStripCodeFence(t *testing.T) {
	cases := map[string]string{
		"{\"a\":1}":               "{\"a\":1}",
		"```json\n{\"a\":1}\n```": "{\"a\":1}",
		"```\n{\"a\":1}\n```":     "{\"a\":1}",
		"  ```JSON\n{}\n```  ":    "{}",
	}
	for in, want := range cases {
		if got := stripCodeFence(in); got != want {
			t.Errorf("stripCodeFence(%q) = %q, want %q", in, got, want)
		}
	}
}
