package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bigknoxy/j-harness/internal/llm"
	"github.com/bigknoxy/j-harness/internal/registry"
	"github.com/bigknoxy/j-harness/internal/tools"
)

// scriptedClient returns queued responses in order and records every request.
type scriptedClient struct {
	responses []llm.Response
	requests  []llm.Request
	next      int
}

func (c *scriptedClient) Complete(_ context.Context, req llm.Request) (llm.Response, error) {
	c.requests = append(c.requests, req)
	i := c.next
	if i >= len(c.responses) {
		i = len(c.responses) - 1
	}
	c.next++
	return c.responses[i], nil
}

func writeToolRegistry(t *testing.T) *registry.Registry {
	t.Helper()
	dir := t.TempDir()

	tw(t, filepath.Join(dir, "blueprints", "solver.json"), `{
  "id": "solver",
  "prompt_path": "prompts/solver.md",
  "model": "test-model",
  "tools": ["math_eval"],
  "version": 1
}`)
	tw(t, filepath.Join(dir, "blueprints", "no_tools.json"), `{
  "id": "no_tools",
  "prompt_path": "prompts/no_tools.md",
  "model": "test-model",
  "version": 1
}`)
	tw(t, filepath.Join(dir, "prompts", "solver.md"), "You solve math. Use tools.")
	tw(t, filepath.Join(dir, "prompts", "no_tools.md"), "Plain agent.")

	reg, err := registry.Load(dir)
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	return reg
}

func tw(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRunAgentToolCallRoundTrip(t *testing.T) {
	reg := writeToolRegistry(t)
	client := &scriptedClient{responses: []llm.Response{
		{ToolCalls: []llm.ToolCall{{ID: "c1", Type: "function", Function: llm.ToolCallFunction{Name: "math_eval", Arguments: `{"expression":"6*7"}`}}}, TotalTokens: 5},
		{Content: "the answer is 42", TotalTokens: 7},
	}}
	eng, err := New(reg, client)
	if err != nil {
		t.Fatal(err)
	}
	toolReg, err := tools.New("math_eval")
	if err != nil {
		t.Fatal(err)
	}
	eng.SetTools(toolReg)

	res, err := eng.RunAgent(context.Background(), "solver", "what is 6*7?")
	if err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	if res.Output != "the answer is 42" {
		t.Fatalf("output = %q", res.Output)
	}
	if res.Tokens != 12 {
		t.Fatalf("tokens = %d, want 12 (summed)", res.Tokens)
	}
	if len(client.requests) != 2 {
		t.Fatalf("requests = %d, want 2 (tool round + final)", len(client.requests))
	}
	// second request should contain the assistant tool_call and the tool result.
	second := client.requests[1]
	foundTool := false
	for _, m := range second.Messages {
		if m.Role == "tool" && strings.Contains(m.Content, "42") {
			foundTool = true
		}
	}
	if !foundTool {
		t.Fatalf("second request missing tool result: %#v", second.Messages)
	}
	if len(second.Tools) != 1 || second.Tools[0].Function.Name != "math_eval" {
		t.Fatalf("tools not forwarded: %#v", second.Tools)
	}
}

func TestRunAgentToolsDisabledFailsClosed(t *testing.T) {
	reg := writeToolRegistry(t)
	eng, _ := New(reg, &scriptedClient{responses: []llm.Response{{Content: "x"}}})
	_, err := eng.RunAgent(context.Background(), "solver", "hi")
	if err == nil || !strings.Contains(err.Error(), "tools are disabled") {
		t.Fatalf("expected tools-disabled error, got %v", err)
	}
}

func TestRunAgentUnknownToolFailsClosed(t *testing.T) {
	reg := writeToolRegistry(t)
	eng, _ := New(reg, &scriptedClient{responses: []llm.Response{{Content: "x"}}})
	toolReg, _ := tools.New("word_count") // math_eval intentionally absent
	eng.SetTools(toolReg)
	_, err := eng.RunAgent(context.Background(), "solver", "hi")
	if err == nil || !strings.Contains(err.Error(), "unknown tool") {
		t.Fatalf("expected unknown-tool error, got %v", err)
	}
}

func TestRunAgentNoToolsUnaffected(t *testing.T) {
	reg := writeToolRegistry(t)
	client := &scriptedClient{responses: []llm.Response{{Content: "plain", TotalTokens: 3}}}
	eng, _ := New(reg, client)
	toolReg, _ := tools.New("math_eval")
	eng.SetTools(toolReg)
	res, err := eng.RunAgent(context.Background(), "no_tools", "hi")
	if err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	if res.Output != "plain" || len(client.requests[0].Tools) != 0 {
		t.Fatalf("tool-free run leaked tools: %#v", client.requests[0].Tools)
	}
}

func TestRunAgentToolLoopBounded(t *testing.T) {
	reg := writeToolRegistry(t)
	// Always returns a tool call -> the loop must stop after maxToolRounds.
	call := llm.Response{ToolCalls: []llm.ToolCall{{ID: "c", Type: "function", Function: llm.ToolCallFunction{Name: "math_eval", Arguments: `{"expression":"1+1"}`}}}}
	client := &scriptedClient{responses: []llm.Response{call}}
	eng, _ := New(reg, client)
	toolReg, _ := tools.New("math_eval")
	eng.SetTools(toolReg)
	_, err := eng.RunAgent(context.Background(), "solver", "loop")
	if err == nil || !strings.Contains(err.Error(), "exceeded") {
		t.Fatalf("expected bounded-loop error, got %v", err)
	}
	if len(client.requests) != maxToolRounds {
		t.Fatalf("requests = %d, want %d", len(client.requests), maxToolRounds)
	}
}

func TestRunAgentRejectsToolOutsideBlueprintAllowlist(t *testing.T) {
	reg := writeToolRegistry(t)
	// The model calls word_count, which is enabled globally but not declared by
	// the solver blueprint (which lists only math_eval).
	client := &scriptedClient{responses: []llm.Response{
		{ToolCalls: []llm.ToolCall{{ID: "c1", Type: "function", Function: llm.ToolCallFunction{Name: "word_count", Arguments: `{"text":"a b c"}`}}}},
		{Content: "done"},
	}}
	eng, _ := New(reg, client)
	toolReg, _ := tools.New("math_eval", "word_count")
	eng.SetTools(toolReg)

	if _, err := eng.RunAgent(context.Background(), "solver", "hi"); err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	if len(client.requests) < 2 {
		t.Fatalf("requests = %d, want >= 2", len(client.requests))
	}
	for _, m := range client.requests[1].Messages {
		if m.Role == "tool" && !strings.Contains(m.Content, "not enabled") {
			t.Fatalf("disallowed tool should be refused, got %q", m.Content)
		}
	}
}
