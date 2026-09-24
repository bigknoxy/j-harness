package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAIConfig configures an OpenAI-compatible client.
type OpenAIConfig struct {
	BaseURL string        // e.g. https://api.openai.com/v1 or http://ollama:11434/v1
	Model   string        // default model when a request omits one
	APIKey  string        // optional; local servers often ignore it
	Timeout time.Duration // per-request timeout (default 300s)
}

// OpenAIClient talks to any OpenAI-compatible chat-completions endpoint.
type OpenAIClient struct {
	defaultBaseURL string
	defaultModel   string
	apiKey         string
	httpClient     *http.Client
}

// NewOpenAI builds a client from cfg. BaseURL is required.
func NewOpenAI(cfg OpenAIConfig) (*OpenAIClient, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, fmt.Errorf("llm: base_url is required")
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 300 * time.Second
	}
	return &OpenAIClient{
		defaultBaseURL: strings.TrimRight(cfg.BaseURL, "/"),
		defaultModel:   cfg.Model,
		apiKey:         cfg.APIKey,
		httpClient:     &http.Client{Timeout: timeout},
	}, nil
}

type chatRequest struct {
	Model          string          `json:"model"`
	Messages       []Message       `json:"messages"`
	Tools          []Tool          `json:"tools,omitempty"`
	Temperature    *float64        `json:"temperature,omitempty"`
	MaxTokens      int             `json:"max_tokens,omitempty"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
	Stream         bool            `json:"stream"`
}

type responseFormat struct {
	Type string `json:"type"`
}

type chatResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// Complete implements Client.
func (c *OpenAIClient) Complete(ctx context.Context, req Request) (Response, error) {
	baseURL := c.defaultBaseURL
	if strings.TrimSpace(req.BaseURL) != "" {
		baseURL = strings.TrimRight(req.BaseURL, "/")
	}
	model := req.Model
	if model == "" {
		model = c.defaultModel
	}
	if model == "" {
		return Response{}, fmt.Errorf("llm: model is required")
	}

	var messages []Message
	if len(req.Messages) > 0 {
		messages = req.Messages
	} else {
		if req.SystemPrompt != "" {
			messages = append(messages, Message{Role: "system", Content: req.SystemPrompt})
		}
		messages = append(messages, Message{Role: "user", Content: req.UserInput})
	}

	body := chatRequest{
		Model:       model,
		Messages:    messages,
		Tools:       req.Tools,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	}
	if req.JSONOutput {
		body.ResponseFormat = &responseFormat{Type: "json_object"}
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return Response{}, err
	}

	endpoint := baseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return Response{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("llm: request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return Response{}, fmt.Errorf("llm: read response: %w", err)
	}

	var parsed chatResponse
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return Response{}, fmt.Errorf("llm: decode response (status %d): %w", resp.StatusCode, err)
		}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := truncate(string(raw), 300)
		if parsed.Error != nil && parsed.Error.Message != "" {
			msg = parsed.Error.Message
		}
		return Response{}, &HTTPError{StatusCode: resp.StatusCode, Message: msg}
	}
	if len(parsed.Choices) == 0 {
		return Response{}, fmt.Errorf("llm: response contained no choices")
	}

	out := Response{Content: parsed.Choices[0].Message.Content, ToolCalls: parsed.Choices[0].Message.ToolCalls}
	if parsed.Usage != nil {
		out.PromptTokens = parsed.Usage.PromptTokens
		out.OutputTokens = parsed.Usage.CompletionTokens
		out.TotalTokens = parsed.Usage.TotalTokens
	}
	return out, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
