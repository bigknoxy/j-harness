package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bigknoxy/j-harness/internal/llm"
	"github.com/bigknoxy/j-harness/internal/model"
	"github.com/bigknoxy/j-harness/internal/registry"
)

// dagRegistry writes a temporary registry containing a diamond pipeline
// (start -> left/right -> join) and returns the loaded registry.
func dagRegistry(t *testing.T) *registry.Registry {
	t.Helper()
	root := t.TempDir()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(root, "blueprints"), 0o755))
	must(os.MkdirAll(filepath.Join(root, "prompts"), 0o755))
	must(os.MkdirAll(filepath.Join(root, "pipelines"), 0o755))
	must(os.WriteFile(filepath.Join(root, "prompts", "a.md"), []byte("you are a"), 0o644))
	must(os.WriteFile(filepath.Join(root, "blueprints", "a.json"),
		[]byte(`{"id":"a","prompt_path":"prompts/a.md","model":"m","version":1}`), 0o644))
	p := model.Pipeline{
		PipelineID: "diamond", Version: 1, Inputs: []string{"user_input"},
		Steps: []model.Step{
			{ID: "start", AgentID: "a", Input: "{{ inputs.user_input }}", Output: "c"},
			{ID: "left", AgentID: "a", Input: "L:{{ steps.start.output }}", Output: "l"},
			{ID: "right", AgentID: "a", Input: "R:{{ steps.start.output }}", Output: "r"},
			{ID: "join", AgentID: "a",
				Input: "{{ steps.left.output }}|{{ steps.right.output }}", Output: "j"},
		},
		Output: "{{ steps.join.output }}",
	}
	data, err := json.Marshal(p)
	must(err)
	must(os.WriteFile(filepath.Join(root, "pipelines", "diamond.json"), data, 0o644))

	reg, err := registry.Load(root)
	must(err)
	return reg
}

// concurrentClient blocks the fan-out branch calls on a barrier, so the test
// deterministically proves left and right run at the same time. It records the
// peak number of in-flight calls.
type concurrentClient struct {
	mu       sync.Mutex
	inFlight int
	peak     int
	calls    []llm.Request

	barrier *sync.WaitGroup
}

func newConcurrentClient() *concurrentClient {
	wg := &sync.WaitGroup{}
	wg.Add(2)
	return &concurrentClient{barrier: wg}
}

func (c *concurrentClient) Complete(_ context.Context, req llm.Request) (llm.Response, error) {
	c.mu.Lock()
	c.inFlight++
	if c.inFlight > c.peak {
		c.peak = c.inFlight
	}
	c.calls = append(c.calls, req)
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		c.inFlight--
		c.mu.Unlock()
	}()

	branch := strings.HasPrefix(req.UserInput, "L:") || strings.HasPrefix(req.UserInput, "R:")
	if branch {
		c.barrier.Done()
		// Wait for the sibling branch to arrive (or time out to fail fast).
		wait := make(chan struct{})
		go func() { c.barrier.Wait(); close(wait) }()
		select {
		case <-wait:
		case <-time.After(2 * time.Second):
		}
	}
	return llm.Response{Content: "out:" + req.UserInput, TotalTokens: 1}, nil
}

func (c *concurrentClient) CallCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.calls)
}

func TestRunPipelineDAGFanOutFanIn(t *testing.T) {
	reg := dagRegistry(t)
	client := newConcurrentClient()
	eng, err := New(reg, client)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	eng.maxParallel = 4

	res, err := eng.RunPipeline(context.Background(), "diamond", map[string]string{"user_input": "hi"})
	if err != nil {
		t.Fatalf("RunPipeline: %v", err)
	}
	if res.Output != "out:out:L:out:hi|out:R:out:hi" {
		t.Errorf("output = %q", res.Output)
	}
	if client.CallCount() != 4 {
		t.Errorf("client calls = %d, want 4", client.CallCount())
	}
	client.mu.Lock()
	peak := client.peak
	client.mu.Unlock()
	if peak < 2 {
		t.Errorf("peak in-flight = %d, want >= 2 (left and right should run concurrently)", peak)
	}
}

func TestRunPipelineDAGStepFailureStops(t *testing.T) {
	reg := dagRegistry(t)
	eng, err := New(reg, llm.NewFakeErr(context.DeadlineExceeded))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	res, err := eng.RunPipeline(context.Background(), "diamond", map[string]string{"user_input": "hi"})
	if err == nil {
		t.Fatal("expected error when the start step fails")
	}
	if len(res.Steps) == 0 || res.Steps[0].Status != string(model.StatusFailed) {
		t.Errorf("expected failed start step, got %+v", res.Steps)
	}
}
