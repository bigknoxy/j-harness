package engine

import (
	"context"
	"io"
	"log"
	"path/filepath"
	"testing"
	"time"

	"github.com/bigknoxy/j-harness/internal/llm"
	"github.com/bigknoxy/j-harness/internal/model"
	"github.com/bigknoxy/j-harness/internal/registry"
	"github.com/bigknoxy/j-harness/internal/store"
)

func testPool(t *testing.T, workers int, responses ...llm.Response) (*Pool, store.Store, *llm.Fake) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "pool.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	reg, err := registry.Load(filepath.Join("..", "..", "agent-registry"))
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	fake := llm.NewFake(responses...)
	eng, err := New(reg, fake)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	pool, err := NewPool(PoolConfig{
		Store:   st,
		Engine:  eng,
		Workers: workers,
		Logger:  log.New(io.Discard, "", 0),
	})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool, st, fake
}

// waitForStatus polls the store until the job reaches want or the deadline passes.
func waitForStatus(t *testing.T, st store.Store, sessionID string, want model.JobStatus) model.Job {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		job, err := st.GetJob(context.Background(), sessionID)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if job.Status == want {
			return job
		}
		if job.Status == model.StatusCompleted || job.Status == model.StatusFailed {
			t.Fatalf("job reached terminal %q, want %q (err=%q)", job.Status, want, job.Error)
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for status %q", want)
	return model.Job{}
}

func TestPoolRunsAgentJob(t *testing.T) {
	pool, st, _ := testPool(t, 1, llm.Response{Content: "done", TotalTokens: 7})

	job := model.Job{SessionID: "j1", Kind: model.KindAgent, TargetID: "generic_agent", Input: "hi"}
	if err := pool.Submit(context.Background(), job); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	got := waitForStatus(t, st, "j1", model.StatusCompleted)
	if got.Result != "done" {
		t.Errorf("result = %q", got.Result)
	}

	steps, err := st.ListStepResults(context.Background(), "j1")
	if err != nil {
		t.Fatalf("ListStepResults: %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("steps = %d, want 1", len(steps))
	}
	if steps[0].StepID != "generic_agent" || steps[0].Output != "done" || steps[0].Tokens != 7 {
		t.Errorf("step = %+v", steps[0])
	}
}

func TestPoolFailedJob(t *testing.T) {
	// Unknown agent -> RunAgent fails -> job FAILED with error recorded.
	pool, st, _ := testPool(t, 1)
	job := model.Job{SessionID: "j2", Kind: model.KindAgent, TargetID: "nope", Input: "hi"}
	if err := pool.Submit(context.Background(), job); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	got := waitForStatus(t, st, "j2", model.StatusFailed)
	if got.Error == "" {
		t.Error("expected error message on failed job")
	}
}

func TestPoolUnsupportedKind(t *testing.T) {
	pool, st, _ := testPool(t, 1)
	job := model.Job{SessionID: "j3", Kind: model.KindPipeline, TargetID: "support_flow"}
	if err := pool.Submit(context.Background(), job); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	got := waitForStatus(t, st, "j3", model.StatusFailed)
	if got.Error == "" {
		t.Error("expected error for unsupported kind")
	}
}

func TestRequeueOrphans(t *testing.T) {
	pool, st, _ := testPool(t, 1, llm.Response{Content: "resumed"})
	ctx := context.Background()

	// Simulate a job left RUNNING by a dead process.
	if err := st.CreateJob(ctx, model.Job{SessionID: "orphan", Kind: model.KindAgent, TargetID: "generic_agent", Input: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetJobStatus(ctx, "orphan", model.StatusRunning, "", ""); err != nil {
		t.Fatal(err)
	}

	n, err := pool.RequeueOrphans(ctx)
	if err != nil {
		t.Fatalf("RequeueOrphans: %v", err)
	}
	if n != 1 {
		t.Fatalf("requeued = %d, want 1", n)
	}
	got := waitForStatus(t, st, "orphan", model.StatusCompleted)
	if got.Result != "resumed" {
		t.Errorf("result = %q", got.Result)
	}
}

func TestSubmitQueueFull(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "full.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	// Engine whose client blocks, so the single worker stays busy.
	reg, err := registry.Load(filepath.Join("..", "..", "agent-registry"))
	if err != nil {
		t.Fatal(err)
	}
	blocking := &blockingClient{release: make(chan struct{})}
	eng, _ := New(reg, blocking)
	pool, err := NewPool(PoolConfig{Store: st, Engine: eng, Workers: 1, Queue: 1, Logger: log.New(io.Discard, "", 0)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	ctx := context.Background()

	if err := pool.Submit(ctx, model.Job{SessionID: "b1", Kind: model.KindAgent, TargetID: "generic_agent"}); err != nil {
		t.Fatalf("submit b1: %v", err)
	}
	if err := pool.Submit(ctx, model.Job{SessionID: "b2", Kind: model.KindAgent, TargetID: "generic_agent"}); err != nil {
		t.Fatalf("submit b2: %v", err)
	}
	if err := pool.Submit(ctx, model.Job{SessionID: "b3", Kind: model.KindAgent, TargetID: "generic_agent"}); err != ErrQueueFull {
		t.Fatalf("submit b3 err = %v, want ErrQueueFull", err)
	}
	close(blocking.release)
}

// blockingClient blocks Complete until release is closed.
type blockingClient struct{ release chan struct{} }

func (b *blockingClient) Complete(ctx context.Context, req llm.Request) (llm.Response, error) {
	<-b.release
	return llm.Response{Content: "ok"}, nil
}
