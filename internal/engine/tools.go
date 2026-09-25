package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/bigknoxy/j-harness/internal/llm"
	"github.com/bigknoxy/j-harness/internal/model"
)

// maxToolRounds caps how many model turns a single agent run may take when
// calling tools. Each round executes the requested tools and re-asks the model.
const maxToolRounds = 5

// resolveTools validates a blueprint's requested tools against the engine's
// allowlist and returns them as llm.Tool descriptors. It fails closed: if the
// blueprint asks for tools but tools are disabled, or a name is unknown, the run
// errors instead of silently dropping the capability.
func (e *Engine) resolveTools(bp model.AgentBlueprint) ([]llm.Tool, error) {
	if len(bp.Tools) == 0 {
		return nil, nil
	}
	reg := e.enabledTools()
	if reg == nil || reg.Len() == 0 {
		return nil, fmt.Errorf("engine: agent %q requests tools but tools are disabled (set ENABLE_TOOLS=true)", bp.ID)
	}
	out := make([]llm.Tool, 0, len(bp.Tools))
	for _, name := range bp.Tools {
		fn, ok := reg.Get(name)
		if !ok {
			return nil, fmt.Errorf("engine: agent %q requests unknown tool %q", bp.ID, name)
		}
		out = append(out, llm.Tool{
			Type: "function",
			Function: llm.ToolFunction{
				Name:        fn.Name,
				Description: fn.Description,
				Parameters:  fn.Parameters,
			},
		})
	}
	return out, nil
}

// completeWithTools runs the bounded tool-call loop. It returns the final
// assistant content plus accumulated token usage. allowed is the blueprint's
// allowlist; a model call for any other enabled tool is refused.
func (e *Engine) completeWithTools(ctx context.Context, req llm.Request, allowed map[string]bool) (llm.Response, error) {
	var usage llm.Response
	for round := 0; round < maxToolRounds; round++ {
		resp, err := e.client.Complete(ctx, req)
		if err != nil {
			return usage, err
		}
		usage.PromptTokens += resp.PromptTokens
		usage.OutputTokens += resp.OutputTokens
		usage.TotalTokens += resp.TotalTokens

		if len(resp.ToolCalls) == 0 {
			usage.Content = resp.Content
			return usage, nil
		}

		req.Messages = append(req.Messages, llm.Message{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})
		for _, call := range resp.ToolCalls {
			result := e.runTool(ctx, call, allowed)
			req.Messages = append(req.Messages, llm.Message{
				Role:       "tool",
				ToolCallID: call.ID,
				Content:    result,
			})
		}
	}
	return usage, fmt.Errorf("engine: exceeded %d tool rounds", maxToolRounds)
}

// runTool executes one tool call and returns its textual result. Tool errors are
// returned as text so the model can react rather than failing the whole run.
func (e *Engine) runTool(ctx context.Context, call llm.ToolCall, allowed map[string]bool) string {
	if !allowed[call.Function.Name] {
		return fmt.Sprintf("error: tool %q is not enabled for this agent", call.Function.Name)
	}
	fn, ok := e.enabledTools().Get(call.Function.Name)
	if !ok {
		return fmt.Sprintf("error: unknown tool %q", call.Function.Name)
	}
	args := strings.TrimSpace(call.Function.Arguments)
	if args == "" {
		args = "{}"
	}
	out, err := fn.Run(ctx, args)
	if err != nil {
		return "error: " + err.Error()
	}
	return out
}

// buildToolMessages assembles the initial conversation for a tool-enabled run.
func buildToolMessages(systemPrompt, input string) []llm.Message {
	msgs := make([]llm.Message, 0, 2)
	if systemPrompt != "" {
		msgs = append(msgs, llm.Message{Role: "system", Content: systemPrompt})
	}
	msgs = append(msgs, llm.Message{Role: "user", Content: input})
	return msgs
}
