package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/bigknoxy/j-harness/internal/model"
	"github.com/bigknoxy/j-harness/internal/store"
)

// ErrQueueFull is returned when the worker queue has no room for a new job.
var ErrQueueFull = errors.New("engine: job queue is full")

// Pool executes jobs asynchronously with a bounded number of workers. Jobs are
// durable: a job is written to the store before it is queued, and a worker
// transitions it through the lifecycle PENDING → RUNNING → COMPLETED|FAILED.
type Pool struct {
	store   store.Store
	eng     *Engine
	logger  *log.Logger
	queue   chan string
	workers int
	wg      sync.WaitGroup
	quit    chan struct{}
	once    sync.Once
}

// PoolConfig configures a Pool. Store and Engine are required.
type PoolConfig struct {
	Store   store.Store
	Engine  *Engine
	Workers int
	Queue   int
	Logger  *log.Logger
}

// NewPool builds and starts a worker pool.
func NewPool(cfg PoolConfig) (*Pool, error) {
	if cfg.Store == nil {
		return nil, fmt.Errorf("engine: pool store is required")
	}
	if cfg.Engine == nil {
		return nil, fmt.Errorf("engine: pool engine is required")
	}
	if cfg.Workers < 1 {
		cfg.Workers = 1
	}
	if cfg.Queue < 1 {
		cfg.Queue = cfg.Workers * 4
	}
	logger := cfg.Logger
	if logger == nil {
		logger = log.Default()
	}
	p := &Pool{
		store:   cfg.Store,
		eng:     cfg.Engine,
		logger:  logger,
		queue:   make(chan string, cfg.Queue),
		workers: cfg.Workers,
		quit:    make(chan struct{}),
	}
	p.wg.Add(cfg.Workers)
	for i := 0; i < cfg.Workers; i++ {
		go p.worker(i)
	}
	return p, nil
}

// Submit persists a new job (PENDING) and enqueues it. It returns the session id.
// If the queue is full the job stays PENDING in the store and ErrQueueFull is
// returned; a later RequeueOrphans/Submit can pick it up.
func (p *Pool) Submit(ctx context.Context, job model.Job) error {
	if job.Status == "" {
		job.Status = model.StatusPending
	}
	if err := p.store.CreateJob(ctx, job); err != nil {
		return err
	}
	select {
	case p.queue <- job.SessionID:
		return nil
	default:
		return ErrQueueFull
	}
}

// Enqueue schedules an already-persisted job for execution.
func (p *Pool) Enqueue(sessionID string) bool {
	select {
	case p.queue <- sessionID:
		return true
	case <-p.quit:
		return false
	default:
		return false
	}
}

// RequeueOrphans resets any RUNNING jobs (left over from a process that died
// mid-flight) to PENDING and enqueues them. Call once at startup.
func (p *Pool) RequeueOrphans(ctx context.Context) (int, error) {
	jobs, err := p.store.ListJobsByStatus(ctx, model.StatusRunning)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, job := range jobs {
		if err := p.store.SetJobStatus(ctx, job.SessionID, model.StatusPending, "", ""); err != nil {
			return n, err
		}
		if p.Enqueue(job.SessionID) {
			n++
		}
	}
	if n > 0 {
		p.logger.Printf("engine: requeued %d orphaned job(s)", n)
	}
	return n, nil
}

// Close stops accepting work, waits for in-flight jobs, and returns.
func (p *Pool) Close() {
	p.once.Do(func() { close(p.quit) })
	p.wg.Wait()
}

func (p *Pool) worker(id int) {
	defer p.wg.Done()
	for {
		select {
		case <-p.quit:
			return
		case sessionID := <-p.queue:
			p.run(sessionID)
		}
	}
}

// run loads the job, executes it, and persists the terminal status.
func (p *Pool) run(sessionID string) {
	ctx := context.Background()

	job, err := p.store.GetJob(ctx, sessionID)
	if err != nil {
		p.logger.Printf("engine: worker load job %s: %v", sessionID, err)
		return
	}
	if job.Status != model.StatusPending {
		return // already handled (e.g. requeued twice)
	}
	if err := p.store.SetJobStatus(ctx, sessionID, model.StatusRunning, "", ""); err != nil {
		p.logger.Printf("engine: worker mark running %s: %v", sessionID, err)
		return
	}

	start := time.Now()
	result, errMsg := p.execute(ctx, job)
	dur := time.Since(start)

	if errMsg != "" {
		if serr := p.store.SetJobStatus(ctx, sessionID, model.StatusFailed, result, errMsg); serr != nil {
			p.logger.Printf("engine: worker mark failed %s: %v", sessionID, serr)
		}
		p.logger.Printf("engine: job %s (%s %s) failed in %s: %s", sessionID, job.Kind, job.TargetID, dur, errMsg)
		return
	}
	if serr := p.store.SetJobStatus(ctx, sessionID, model.StatusCompleted, result, ""); serr != nil {
		p.logger.Printf("engine: worker mark completed %s: %v", sessionID, serr)
	}
	p.logger.Printf("engine: job %s (%s %s) completed in %s", sessionID, job.Kind, job.TargetID, dur)
}

// execute runs a job and returns (result, errorMessage).
func (p *Pool) execute(ctx context.Context, job model.Job) (string, string) {
	switch job.Kind {
	case model.KindAgent:
		res, err := p.eng.RunAgent(ctx, job.TargetID, job.Input)
		_ = p.store.AppendStepResult(ctx, model.StepResult{
			SessionID:  job.SessionID,
			StepID:     job.TargetID,
			Status:     stepStatus(err),
			Output:     res.Output,
			Error:      errString(err),
			Tokens:     res.Tokens,
			DurationMS: res.DurationMS,
		})
		if err != nil {
			return "", err.Error()
		}
		return res.Output, ""

	case model.KindPipeline:
		res, err := p.eng.RunPipeline(ctx, job.TargetID, parseJobInput(job.Input))
		for _, s := range res.Steps {
			_ = p.store.AppendStepResult(ctx, model.StepResult{
				SessionID:  job.SessionID,
				StepID:     s.StepID,
				Status:     s.Status,
				Output:     s.Output,
				Error:      s.Error,
				Tokens:     s.Tokens,
				DurationMS: s.DurationMS,
			})
		}
		if err != nil {
			return "", err.Error()
		}
		return res.Output, ""

	default:
		return "", fmt.Sprintf("engine: unsupported job kind %q", job.Kind)
	}
}

// parseJobInput decodes the stored input payload. Agent jobs store the raw
// input string; pipeline jobs store a JSON object of named inputs. Both are
// accepted, so a plain string is exposed as the conventional "input" key.
func parseJobInput(raw string) map[string]string {
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(trimmed, "{") {
		var m map[string]string
		if err := json.Unmarshal([]byte(trimmed), &m); err == nil {
			return m
		}
	}
	return map[string]string{"input": raw}
}

func stepStatus(err error) string {
	if err != nil {
		return string(model.StatusFailed)
	}
	return string(model.StatusCompleted)
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
