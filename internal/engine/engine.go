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
	"sync"
	"time"

	"github.com/bigknoxy/j-harness/internal/llm"
	"github.com/bigknoxy/j-harness/internal/metrics"
	"github.com/bigknoxy/j-harness/internal/model"
	"github.com/bigknoxy/j-harness/internal/registry"
	"github.com/bigknoxy/j-harness/internal/schema"
	"github.com/bigknoxy/j-harness/internal/tools"
)

// Engine runs agents and pipelines against an LLM backend.
type Engine struct {
	registry    *registry.Registry
	client      llm.Client
	maxParallel int

	mu           sync.RWMutex
	toolsEnabled *tools.Registry
	metrics      *metrics.Metrics
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

// SetTools enables function calling with the given allowlist. A nil registry
// leaves tools disabled. Call once at startup.
func (e *Engine) SetTools(reg *tools.Registry) {
	e.mu.Lock()
	e.toolsEnabled = reg
	e.mu.Unlock()
}

// SetMetrics attaches counters used to report schema failures. Optional.
func (e *Engine) SetMetrics(m *metrics.Metrics) {
	e.mu.Lock()
	e.metrics = m
	e.mu.Unlock()
}

func (e *Engine) getMetrics() *metrics.Metrics {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.metrics
}

// enabledTools returns the enabled tool allowlist, or nil when tools are off.
func (e *Engine) enabledTools() *tools.Registry {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.toolsEnabled
}

// SetRegistry swaps the registry snapshot used by subsequent executions. It is
// safe to call while jobs are running: an in-flight run keeps the snapshot it
// started with, and new runs see the new one.
func (e *Engine) SetRegistry(reg *registry.Registry) {
	if reg == nil {
		return
	}
	e.mu.Lock()
	e.registry = reg
	e.mu.Unlock()
}

// registry returns the current registry snapshot.
func (e *Engine) getRegistry() *registry.Registry {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.registry
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

	reg := e.getRegistry()
	bp, ok := reg.Blueprint(agentID)
	if !ok {
		return AgentResult{}, fmt.Errorf("engine: unknown agent %q", agentID)
	}
	prompt, ok := reg.Prompt(agentID)
	if !ok {
		return AgentResult{}, fmt.Errorf("engine: missing prompt for agent %q", agentID)
	}

	if bp.TimeoutSeconds > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(bp.TimeoutSeconds)*time.Second)
		defer cancel()
	}

	toolDefs, err := e.resolveTools(bp)
	if err != nil {
		return AgentResult{DurationMS: time.Since(start).Milliseconds()}, err
	}

	req := llm.Request{
		Model:        bp.Model,
		BaseURL:      bp.BaseURL,
		SystemPrompt: prompt,
		UserInput:    input,
		Temperature:  bp.Temperature,
		MaxTokens:    bp.MaxTokens,
		JSONOutput:   bp.OutputFormat == model.OutputJSON && len(toolDefs) == 0,
		Tools:        toolDefs,
	}

	var resp llm.Response
	if len(toolDefs) > 0 {
		req.Messages = buildToolMessages(prompt, input)
		allowed := make(map[string]bool, len(bp.Tools))
		for _, name := range bp.Tools {
			allowed[name] = true
		}
		resp, err = e.completeWithTools(ctx, req, allowed)
	} else {
		resp, err = e.client.Complete(ctx, req)
	}
	if err != nil {
		return AgentResult{DurationMS: time.Since(start).Milliseconds()}, err
	}
	tokens := resp.TotalTokens

	content := strings.TrimSpace(resp.Content)
	if bp.OutputFormat == model.OutputJSON {
		if err := validateJSONObject(content); err != nil {
			return AgentResult{DurationMS: time.Since(start).Milliseconds()},
				fmt.Errorf("engine: agent %q returned invalid JSON: %w", agentID, err)
		}
		if outSchema := reg.OutputSchema(agentID); outSchema != nil {
			if err := outSchema.Validate(stripCodeFence(content)); err != nil {
				repaired, repairTokens, repairedErr := e.repairSchema(ctx, req, outSchema, content, err)
				if repairedErr != nil {
					e.getMetrics().Inc(metrics.SchemaFailures)
					return AgentResult{DurationMS: time.Since(start).Milliseconds()},
						fmt.Errorf("engine: agent %q output does not match schema: %w", agentID, err)
				}
				content = repaired
				tokens += repairTokens
			}
		}
	}

	return AgentResult{
		AgentID:    agentID,
		Output:     content,
		Tokens:     tokens,
		DurationMS: time.Since(start).Milliseconds(),
	}, nil
}

// repairSchema makes a single corrective turn when a schema-validated output
// fails validation. It re-validates the correction against outSchema and
// returns the corrected content, the extra tokens spent, or an error.
func (e *Engine) repairSchema(ctx context.Context, base llm.Request, outSchema *schema.Schema, content string, verr error) (string, int, error) {
	req := base
	req.Messages = []llm.Message{
		{Role: "system", Content: base.SystemPrompt},
		{Role: "user", Content: base.UserInput},
		{Role: "assistant", Content: content},
		{Role: "user", Content: fmt.Sprintf(
			"Your previous reply did not satisfy the required JSON schema. Problem: %s. "+
				"Reply with only a corrected JSON object that satisfies the schema.", verr),
		},
	}
	req.Tools = nil
	req.JSONOutput = true

	resp, err := e.client.Complete(ctx, req)
	if err != nil {
		return "", 0, err
	}
	out := strings.TrimSpace(resp.Content)
	trimmed := stripCodeFence(out)
	if err := validateJSONObject(trimmed); err != nil {
		return "", resp.TotalTokens, err
	}
	if err := outSchema.Validate(trimmed); err != nil {
		return "", resp.TotalTokens, err
	}
	return out, resp.TotalTokens, nil
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
	if obj == nil {
		return fmt.Errorf("not a JSON object")
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
