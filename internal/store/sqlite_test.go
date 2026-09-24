package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/bigknoxy/j-harness/internal/model"
)

func newStore(t *testing.T) *SQLiteStore {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestCreateAndGetJob(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	job := model.Job{
		SessionID: "s1",
		Kind:      model.KindAgent,
		TargetID:  "triage",
		Input:     "hello",
	}
	if err := s.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	got, err := s.GetJob(ctx, "s1")
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if got.SessionID != "s1" || got.Kind != model.KindAgent || got.TargetID != "triage" {
		t.Errorf("job = %+v", got)
	}
	if got.Status != model.StatusPending {
		t.Errorf("status = %q, want PENDING", got.Status)
	}
	if got.Input != "hello" {
		t.Errorf("input = %q", got.Input)
	}
}

func TestGetJobNotFound(t *testing.T) {
	s := newStore(t)
	if _, err := s.GetJob(context.Background(), "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestCreateJobDuplicate(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	job := model.Job{SessionID: "dup", Kind: model.KindAgent, TargetID: "a"}
	if err := s.CreateJob(ctx, job); err != nil {
		t.Fatalf("first CreateJob: %v", err)
	}
	if err := s.CreateJob(ctx, job); err == nil {
		t.Fatal("expected duplicate CreateJob to fail")
	}
}

func TestSetJobStatus(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	if err := s.CreateJob(ctx, model.Job{SessionID: "s1", Kind: model.KindAgent, TargetID: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetJobStatus(ctx, "s1", model.StatusCompleted, "out", ""); err != nil {
		t.Fatalf("SetJobStatus: %v", err)
	}
	got, _ := s.GetJob(ctx, "s1")
	if got.Status != model.StatusCompleted || got.Result != "out" {
		t.Errorf("job = %+v", got)
	}
	if err := s.SetJobStatus(ctx, "missing", model.StatusFailed, "", "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing err = %v, want ErrNotFound", err)
	}
}

func TestListJobsByStatusOrder(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	for _, id := range []string{"a", "b", "c"} {
		if err := s.CreateJob(ctx, model.Job{SessionID: id, Kind: model.KindAgent, TargetID: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetJobStatus(ctx, "b", model.StatusRunning, "", ""); err != nil {
		t.Fatal(err)
	}
	running, err := s.ListJobsByStatus(ctx, model.StatusRunning)
	if err != nil {
		t.Fatalf("ListJobsByStatus: %v", err)
	}
	if len(running) != 1 || running[0].SessionID != "b" {
		t.Fatalf("running = %+v", running)
	}
	pending, _ := s.ListJobsByStatus(ctx, model.StatusPending)
	if len(pending) != 2 || pending[0].SessionID != "a" || pending[1].SessionID != "c" {
		t.Errorf("pending = %+v", pending)
	}
}

func TestStepResultsRoundTrip(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	results := []model.StepResult{
		{SessionID: "s1", StepID: "triage", Status: "COMPLETED", Output: "{}", Tokens: 10, DurationMS: 5},
		{SessionID: "s1", StepID: "reply", Status: "FAILED", Error: "boom", DurationMS: 1},
		{SessionID: "s2", StepID: "other", Status: "COMPLETED"},
	}
	for _, sr := range results {
		if err := s.AppendStepResult(ctx, sr); err != nil {
			t.Fatalf("AppendStepResult: %v", err)
		}
	}
	got, err := s.ListStepResults(ctx, "s1")
	if err != nil {
		t.Fatalf("ListStepResults: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].StepID != "triage" || got[0].Tokens != 10 || got[0].DurationMS != 5 {
		t.Errorf("step0 = %+v", got[0])
	}
	if got[1].StepID != "reply" || got[1].Error != "boom" {
		t.Errorf("step1 = %+v", got[1])
	}
}

func TestOpenRequiresPath(t *testing.T) {
	if _, err := Open(""); err == nil {
		t.Fatal("expected error for empty path")
	}
}
