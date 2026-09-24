// Package llm defines the model-client boundary. The engine depends only on the
// Client interface, so any OpenAI-compatible endpoint (OpenAI, Ollama, vLLM,
// llama.cpp server) works, and tests can substitute a fake.
package llm

import (
	"context"
	"errors"
)

// Message is a single chat message. Assistant messages may carry ToolCalls;
// tool-result messages set Role "tool" and ToolCallID.
type Message struct {
	Role       string     `json:"role"` // "system", "user", "assistant", "tool"
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolFunction describes a callable function's signature.
type ToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// Tool is a function the model may call.
type Tool struct {
	Type     string       `json:"type"` // always "function"
	Function ToolFunction `json:"function"`
}

// ToolCall is one function invocation requested by the model.
type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type,omitempty"`
	Function ToolCallFunction `json:"function"`
}

// ToolCallFunction carries the name and raw JSON arguments of a tool call.
type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Request is one completion request. Fields left zero fall back to the client's
// defaults. Messages, when set, is sent verbatim and overrides SystemPrompt and
// UserInput.
type Request struct {
	Model        string
	BaseURL      string // per-request endpoint override
	SystemPrompt string // sent as a leading system message when non-empty
	UserInput    string
	Messages     []Message // full conversation; overrides SystemPrompt/UserInput
	Tools        []Tool    // functions the model may call
	Temperature  *float64
	MaxTokens    int
	JSONOutput   bool // request a JSON object response
}

// Response is one completion result.
type Response struct {
	Content      string
	ToolCalls    []ToolCall
	PromptTokens int
	OutputTokens int
	TotalTokens  int
}

// ErrUnsupported is returned when a client cannot satisfy a request.
var ErrUnsupported = errors.New("llm: unsupported request")

// Client is a chat-completion backend.
type Client interface {
	// Complete runs one non-streaming chat completion.
	Complete(ctx context.Context, req Request) (Response, error)
}
