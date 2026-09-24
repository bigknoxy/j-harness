// Package engine executes agents (and, in later phases, pipelines) by combining
// the registry's definitions with an llm.Client. It is transport-agnostic so both
// the synchronous API and the async worker pool can drive it.
package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/bigknoxy/j-harness/internal/llm"
	"github.com/bigknoxy/j-harness/internal/model"
	"github.com/bigknoxy/j-harness/internal/registry"
)

// Engine runs agents and pipelines against an LLM backend.
type Engine struct {
	registry    *registry.Registry
	client      llm.Client
	maxParallel int
}

// New builds an Engine. Both arguments are required. Pipeline steps run
// concurrently up to maxParallel (defaults to GOMAXPROCS, minimum 1).
func New(reg *registry.Registry, client llm.Client) (*Engine, error) {
	if reg == nil {
		return nil, fmt.Errorf("engine: registry is required")
	}
	if client == nil {
		return nil, fmt.Errorf("engine: llm client is required")
	}
	return &Engine{registry: reg, client: client, maxParallel: runtime.GOMAXPROCS(0)}, nil
}

// AgentResult is the outcome of running a single agent.
type AgentResult struct {
	AgentID    string
	Output     string
	Tokens     int
	DurationMS int64
}

// RunAgent executes the agent identified by agentID with the given input.
func (e *Engine) RunAgent(ctx context.Context, agentID, input string) (AgentResult, error) {
	start := time.Now()

	bp, ok := e.registry.Blueprint(agentID)
	if !ok {
		return AgentResult{}, fmt.Errorf("engine: unknown agent %q", agentID)
	}
	prompt, ok := e.registry.Prompt(agentID)
	if !ok {
		return AgentResult{}, fmt.Errorf("engine: missing prompt for agent %q", agentID)
	}

	if bp.TimeoutSeconds > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(bp.TimeoutSeconds)*time.Second)
		defer cancel()
	}

	req := llm.Request{
		Model:        bp.Model,
		BaseURL:      bp.BaseURL,
		SystemPrompt: prompt,
		UserInput:    input,
		Temperature:  bp.Temperature,
		MaxTokens:    bp.MaxTokens,
		JSONOutput:   bp.OutputFormat == model.OutputJSON,
	}

	resp, err := e.client.Complete(ctx, req)
	if err != nil {
		return AgentResult{DurationMS: time.Since(start).Milliseconds()}, err
	}

	content := strings.TrimSpace(resp.Content)
	if bp.OutputFormat == model.OutputJSON {
		if err := validateJSONObject(content); err != nil {
			return AgentResult{DurationMS: time.Since(start).Milliseconds()},
				fmt.Errorf("engine: agent %q returned invalid JSON: %w", agentID, err)
		}
	}

	return AgentResult{
		AgentID:    agentID,
		Output:     content,
		Tokens:     resp.TotalTokens,
		DurationMS: time.Since(start).Milliseconds(),
	}, nil
}

// validateJSONObject rejects content that is not a single JSON object. Models
// sometimes wrap JSON in prose or code fences; we strip fences but otherwise
// require a clean object.
func validateJSONObject(content string) error {
	trimmed := stripCodeFence(content)
	var obj map[string]any
	if err := json.Unmarshal([]byte(trimmed), &obj); err != nil {
		return err
	}
	return nil
}

func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```JSON")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	return strings.TrimSpace(s)
}
