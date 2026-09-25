// Package eval is the checked-in, deterministic evaluation suite. It runs a
// versioned set of cases from cases.json against the checked-in agent registry
// and a scripted OpenAI-compatible stub, then scores each case CORRECT or
// INCORRECT. It makes no network calls and needs no model server; the same cases
// can be replayed against a real OpenAI-compatible endpoint (see docs/EVALS.md).
//
// The suite is intentionally a plain `go test`: run it with
//
//	go test ./internal/eval/...
package eval

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bigknoxy/j-harness/internal/engine"
	"github.com/bigknoxy/j-harness/internal/llm"
	"github.com/bigknoxy/j-harness/internal/llmstub"
	"github.com/bigknoxy/j-harness/internal/registry"
	"github.com/bigknoxy/j-harness/internal/schema"
	"github.com/bigknoxy/j-harness/internal/tools"
)

//go:embed cases.json
var casesJSON []byte

// registryPath is the checked-in registry relative to internal/eval.
const registryPath = "../../agent-registry"

// suite mirrors cases.json.
type suite struct {
	Version int        `json:"version"`
	Cases   []evalCase `json:"cases"`
}

// evalCase is one scored evaluation case.
type evalCase struct {
	Name           string                `json:"name"`
	Kind           string                `json:"kind"` // "agent" | "pipeline"
	Agent          string                `json:"agent,omitempty"`
	Pipeline       string                `json:"pipeline,omitempty"`
	Input          string                `json:"input,omitempty"`
	Inputs         map[string]string     `json:"inputs,omitempty"`
	Script         []scriptStep          `json:"script,omitempty"`
	ScriptByPrompt map[string]scriptStep `json:"script_by_prompt,omitempty"`
	Expect         expectation           `json:"expect"`
}

// scriptStep is one scripted stub response. Exactly one of Content or ToolName
// should be set: Content is a final assistant reply, ToolName requests a tool.
type scriptStep struct {
	Content     string `json:"content,omitempty"`
	ToolName    string `json:"tool_name,omitempty"`
	ToolArgs    string `json:"tool_args,omitempty"`
	TotalTokens int    `json:"total_tokens,omitempty"`
}

// expectation is the pass condition for a case. Zero-valued fields are not
// checked.
type expectation struct {
	Result         string            `json:"result,omitempty"`
	JSONFields     map[string]string `json:"json_fields,omitempty"`
	Schema         string            `json:"schema,omitempty"`
	Calls          int               `json:"calls,omitempty"`
	Failed         bool              `json:"failed,omitempty"`
	StepsCompleted []string          `json:"steps_completed,omitempty"`
	StepsSkipped   []string          `json:"steps_skipped,omitempty"`
}

// result is the scored outcome of one case.
type result struct {
	name    string
	correct bool
	detail  string
}

// agentScript returns a stub script that replays the case's scripted steps in
// order, repeating the last step once exhausted.
func agentScript(steps []scriptStep) llmstub.Script {
	var mu sync.Mutex
	next := 0
	return func(_ llm.Request) llm.Response {
		mu.Lock()
		defer mu.Unlock()
		i := next
		if i >= len(steps) {
			i = len(steps) - 1
		}
		next++
		if i < 0 {
			return llm.Response{Content: "", TotalTokens: 1}
		}
		return steps[i].response()
	}
}

func (s scriptStep) response() llm.Response {
	if s.ToolName != "" {
		return llm.Response{
			ToolCalls: []llm.ToolCall{{
				ID:       "call_" + s.ToolName,
				Type:     "function",
				Function: llm.ToolCallFunction{Name: s.ToolName, Arguments: s.ToolArgs},
			}},
			TotalTokens: s.TotalTokens,
		}
	}
	return llm.Response{Content: s.Content, TotalTokens: s.TotalTokens}
}

// promptScript returns a stub script that picks a scripted response by system
// prompt substring. The "*" key is the fallback.
func promptScript(byPrompt map[string]scriptStep) llmstub.Script {
	return func(req llm.Request) llm.Response {
		sp := llmstub.SystemPrompt(req)
		for match, step := range byPrompt {
			if match == "*" {
				continue
			}
			if strings.Contains(sp, match) {
				return step.response()
			}
		}
		if step, ok := byPrompt["*"]; ok {
			return step.response()
		}
		return llm.Response{Content: "", TotalTokens: 1}
	}
}

// buildEngine loads the registry and returns an engine wired to the stub and the
// built-in tools (so tool-call cases run with function calling enabled).
func buildEngine(t *testing.T, server *llmstub.Server) *engine.Engine {
	t.Helper()
	reg, err := registry.Load(registryPath)
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	client, err := llm.NewOpenAI(llm.OpenAIConfig{
		BaseURL: server.BaseURL(), Model: "stub-model", Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("llm.NewOpenAI: %v", err)
	}
	eng, err := engine.New(reg, client)
	if err != nil {
		t.Fatalf("engine.New: %v", err)
	}
	toolReg, err := tools.New("current_time", "word_count", "math_eval")
	if err != nil {
		t.Fatalf("tools.New: %v", err)
	}
	eng.SetTools(toolReg)
	return eng
}

// runCase executes one case and returns whether it met every expectation.
func runCase(t *testing.T, c evalCase) result {
	t.Helper()

	var script llmstub.Script
	switch {
	case len(c.Script) > 0:
		script = agentScript(c.Script)
	case len(c.ScriptByPrompt) > 0:
		script = promptScript(c.ScriptByPrompt)
	default:
		return result{name: c.Name, correct: false, detail: "case has no script"}
	}

	server := llmstub.New(script)
	defer server.Close()
	eng := buildEngine(t, server)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	switch c.Kind {
	case "agent":
		return scoreAgent(t, ctx, eng, server, c)
	case "pipeline":
		return scorePipeline(t, ctx, eng, server, c)
	default:
		return result{name: c.Name, correct: false, detail: fmt.Sprintf("unknown kind %q", c.Kind)}
	}
}

func scoreAgent(t *testing.T, ctx context.Context, eng *engine.Engine, server *llmstub.Server, c evalCase) result {
	res, err := eng.RunAgent(ctx, c.Agent, c.Input)

	if c.Expect.Failed {
		if err == nil {
			return result{name: c.Name, correct: false, detail: "expected failure but run succeeded"}
		}
		return result{name: c.Name, correct: true, detail: "failed as expected: " + err.Error()}
	}
	if err != nil {
		return result{name: c.Name, correct: false, detail: "run error: " + err.Error()}
	}

	if c.Expect.Result != "" && res.Output != c.Expect.Result {
		return result{name: c.Name, correct: false, detail: fmt.Sprintf("result = %q, want %q", res.Output, c.Expect.Result)}
	}
	if c.Expect.Calls > 0 && server.CallCount() != c.Expect.Calls {
		return result{name: c.Name, correct: false, detail: fmt.Sprintf("calls = %d, want %d", server.CallCount(), c.Expect.Calls)}
	}
	if c.Expect.JSONFields != nil {
		fields, ferr := jsonFields(res.Output)
		if ferr != nil {
			return result{name: c.Name, correct: false, detail: "result is not JSON: " + ferr.Error()}
		}
		for k, want := range c.Expect.JSONFields {
			if got := fmt.Sprint(fields[k]); got != want {
				return result{name: c.Name, correct: false, detail: fmt.Sprintf("field %q = %q, want %q", k, got, want)}
			}
		}
	}
	if c.Expect.Schema != "" {
		if serr := validateSchema(c.Expect.Schema, res.Output); serr != nil {
			return result{name: c.Name, correct: false, detail: "schema violation: " + serr.Error()}
		}
	}
	return result{name: c.Name, correct: true, detail: "ok"}
}

func scorePipeline(t *testing.T, ctx context.Context, eng *engine.Engine, _ *llmstub.Server, c evalCase) result {
	res, err := eng.RunPipeline(ctx, c.Pipeline, c.Inputs)
	if err != nil {
		return result{name: c.Name, correct: false, detail: "run error: " + err.Error()}
	}
	status := map[string]string{}
	for _, s := range res.Steps {
		status[s.StepID] = s.Status
	}
	for _, id := range c.Expect.StepsCompleted {
		if status[id] != "COMPLETED" {
			return result{name: c.Name, correct: false,
				detail: fmt.Sprintf("step %q = %q, want COMPLETED (all: %v)", id, status[id], status)}
		}
	}
	for _, id := range c.Expect.StepsSkipped {
		if status[id] != "SKIPPED" {
			return result{name: c.Name, correct: false,
				detail: fmt.Sprintf("step %q = %q, want SKIPPED (all: %v)", id, status[id], status)}
		}
	}
	return result{name: c.Name, correct: true, detail: "ok"}
}

// jsonFields parses a model result (optionally wrapped in a code fence) into a
// field map.
func jsonFields(content string) (map[string]any, error) {
	var out map[string]any
	if err := json.Unmarshal([]byte(stripCodeFence(content)), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// validateSchema compiles the named registry schema and validates content
// against it, mirroring how the engine enforces structured output.
func validateSchema(rel, content string) error {
	raw, err := os.ReadFile(filepath.Join(registryPath, rel))
	if err != nil {
		return err
	}
	compiled, err := schema.Compile(raw)
	if err != nil {
		return err
	}
	return compiled.Validate(stripCodeFence(content))
}

// stripCodeFence mirrors the engine's handling of fenced model output.
func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```JSON")
	s = strings.TrimPrefix(s, "```")
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "```"))
}

func TestEvalSuite(t *testing.T) {
	var s suite
	if err := json.Unmarshal(casesJSON, &s); err != nil {
		t.Fatalf("parse cases.json: %v", err)
	}
	if len(s.Cases) == 0 {
		t.Fatal("cases.json has no cases")
	}

	results := make([]result, 0, len(s.Cases))
	for _, c := range s.Cases {
		c := c
		t.Run(c.Name, func(t *testing.T) {
			results = append(results, runCase(t, c))
		})
	}

	sort.Slice(results, func(i, j int) bool { return results[i].name < results[j].name })
	passed := 0
	for _, r := range results {
		verdict := "INCORRECT"
		if r.correct {
			verdict, passed = "CORRECT", passed+1
		}
		t.Logf("%-9s %s: %s", verdict, r.name, r.detail)
	}
	t.Logf("eval summary: %d passed / %d failed (of %d cases)", passed, len(results)-passed, len(results))
	if passed != len(results) {
		t.Fatalf("eval suite failed: %d/%d correct", passed, len(results))
	}
}
