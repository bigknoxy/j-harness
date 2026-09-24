// Package llm defines the model-client boundary. The engine depends only on the
// Client interface, so any OpenAI-compatible endpoint (OpenAI, Ollama, vLLM,
// llama.cpp server) works, and tests can substitute a fake.
package llm

import (
	"context"
	"errors"
)

// Message is a single chat message.
type Message struct {
	Role    string `json:"role"` // "system", "user", "assistant"
	Content string `json:"content"`
}

// Request is one completion request. Fields left zero fall back to the client's
// defaults.
type Request struct {
	Model        string
	BaseURL      string // per-request endpoint override
	SystemPrompt string // sent as a leading system message when non-empty
	UserInput    string
	Temperature  *float64
	MaxTokens    int
	JSONOutput   bool // request a JSON object response
}

// Response is one completion result.
type Response struct {
	Content      string
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
