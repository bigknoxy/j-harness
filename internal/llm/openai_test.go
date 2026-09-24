package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOpenAICompleteSuccess(t *testing.T) {
	var gotBody map[string]any
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"choices":[{"message":{"role":"assistant","content":"hello"}}],
			"usage":{"prompt_tokens":3,"completion_tokens":5,"total_tokens":8}
		}`))
	}))
	defer srv.Close()

	c, err := NewOpenAI(OpenAIConfig{BaseURL: srv.URL + "/v1", Model: "m", APIKey: "secret", Timeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.Complete(context.Background(), Request{SystemPrompt: "be nice", UserInput: "hi"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Content != "hello" || resp.TotalTokens != 8 {
		t.Errorf("unexpected response %+v", resp)
	}
	if gotAuth != "Bearer secret" {
		t.Errorf("auth header = %q", gotAuth)
	}
	msgs, _ := gotBody["messages"].([]any)
	if len(msgs) != 2 {
		t.Errorf("expected system+user messages, got %v", gotBody["messages"])
	}
	if gotBody["stream"] != false {
		t.Errorf("expected stream=false, got %v", gotBody["stream"])
	}
}

func TestOpenAICompleteJSONFormat(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{}"}}]}`))
	}))
	defer srv.Close()

	c, _ := NewOpenAI(OpenAIConfig{BaseURL: srv.URL, Model: "m"})
	if _, err := c.Complete(context.Background(), Request{UserInput: "x", JSONOutput: true}); err != nil {
		t.Fatal(err)
	}
	rf, ok := gotBody["response_format"].(map[string]any)
	if !ok || rf["type"] != "json_object" {
		t.Errorf("expected json_object response_format, got %v", gotBody["response_format"])
	}
}

func TestOpenAICompleteErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"boom"}}`))
	}))
	defer srv.Close()

	c, _ := NewOpenAI(OpenAIConfig{BaseURL: srv.URL, Model: "m"})
	_, err := c.Complete(context.Background(), Request{UserInput: "x"})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected error containing 'boom', got %v", err)
	}
}

func TestOpenAIRequiresBaseURL(t *testing.T) {
	if _, err := NewOpenAI(OpenAIConfig{}); err == nil {
		t.Fatal("expected error for missing base_url")
	}
}

func TestFakeReturnsInOrder(t *testing.T) {
	f := NewFake(Response{Content: "a"}, Response{Content: "b"})
	ctx := context.Background()
	r1, _ := f.Complete(ctx, Request{})
	r2, _ := f.Complete(ctx, Request{})
	r3, _ := f.Complete(ctx, Request{})
	if r1.Content != "a" || r2.Content != "b" || r3.Content != "b" {
		t.Errorf("got %q %q %q", r1.Content, r2.Content, r3.Content)
	}
	if f.CallCount() != 3 {
		t.Errorf("CallCount = %d", f.CallCount())
	}
}
