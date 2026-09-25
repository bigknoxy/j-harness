// Package llmstub provides a deterministic, offline OpenAI-compatible HTTP
// server for tests. It implements only POST /v1/chat/completions and returns a
// response computed by a caller-supplied Script, so the real internal/llm OpenAI
// client (and everything above it) can be exercised with no network and no model.
package llmstub

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"

	"github.com/bigknoxy/j-harness/internal/llm"
)

// Script computes the model response for one request. It is called once per
// /v1/chat/completions request and may inspect the full conversation (system
// prompt, prior turns, and whether a tool result is already present).
type Script func(req llm.Request) llm.Response

// Server is an httptest.Server speaking the OpenAI chat-completions protocol.
type Server struct {
	ts     *httptest.Server
	script Script

	mu       sync.Mutex
	requests []llm.Request
}

// New starts the stub. Call Close when done.
func New(script Script) *Server {
	s := &Server{script: script}
	s.ts = httptest.NewServer(http.HandlerFunc(s.serve))
	return s
}

// URL returns the server's base URL with no path suffix, e.g.
// http://127.0.0.1:1234.
func (s *Server) URL() string { return s.ts.URL }

// BaseURL returns the OpenAI base URL, i.e. URL + "/v1".
func (s *Server) BaseURL() string { return s.ts.URL + "/v1" }

// Close shuts the server down.
func (s *Server) Close() { s.ts.Close() }

// Requests returns a copy of every request seen so far.
func (s *Server) Requests() []llm.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]llm.Request, len(s.requests))
	copy(out, s.requests)
	return out
}

// CallCount returns how many completion requests the stub has handled.
func (s *Server) CallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.requests)
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []llm.Message `json:"messages"`
	Tools    []llm.Tool    `json:"tools"`
}

type choice struct {
	Message llm.Message `json:"message"`
}

type chatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type chatResponse struct {
	Choices []choice  `json:"choices"`
	Usage   chatUsage `json:"usage"`
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	var req chatRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	full := llm.Request{Model: req.Model, Messages: req.Messages, Tools: req.Tools}
	s.mu.Lock()
	s.requests = append(s.requests, full)
	s.mu.Unlock()

	resp := s.script(full)
	total := resp.TotalTokens
	if total == 0 {
		total = 1
	}
	out := chatResponse{
		Choices: []choice{{Message: llm.Message{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		}}},
		Usage: chatUsage{
			PromptTokens:     resp.PromptTokens,
			CompletionTokens: resp.OutputTokens,
			TotalTokens:      total,
		},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// --- helpers for building deterministic scripts ---

// SystemPrompt returns the content of the first system message, or "".
func SystemPrompt(req llm.Request) string {
	for _, m := range req.Messages {
		if m.Role == "system" {
			return m.Content
		}
	}
	return ""
}

// LastUser returns the content of the last user-role message, or "".
func LastUser(req llm.Request) string {
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "user" {
			return req.Messages[i].Content
		}
	}
	return ""
}

// HasToolResult reports whether the conversation already contains a tool reply.
func HasToolResult(req llm.Request) bool {
	for _, m := range req.Messages {
		if m.Role == "tool" {
			return true
		}
	}
	return false
}

// JSONResponse returns a response whose content is the JSON encoding of v.
func JSONResponse(v any) llm.Response {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return llm.Response{Content: string(b), TotalTokens: 1}
}

// ByPrompt returns a Script that picks a response by matching system prompts
// against the given map. The "*" key is the fallback. Matching is a substring
// test, which is order-independent and safe for concurrent fan-out.
func ByPrompt(responses map[string]string) Script {
	return func(req llm.Request) llm.Response {
		sp := SystemPrompt(req)
		for match, content := range responses {
			if match == "*" {
				continue
			}
			if strings.Contains(sp, match) {
				return llm.Response{Content: content, TotalTokens: 1}
			}
		}
		if content, ok := responses["*"]; ok {
			return llm.Response{Content: content, TotalTokens: 1}
		}
		return llm.Response{Content: "", TotalTokens: 1}
	}
}
