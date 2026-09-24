// Package store persists jobs and step results. The Store interface isolates the
// engine and API from storage details so the SQLite implementation can later be
// swapped for a Redis adapter (Phase 11) without touching callers.
package store

import (
	"context"
	"errors"

	"github.com/bigknoxy/j-harness/internal/model"
)

// ErrNotFound is returned when a job (session) does not exist.
var ErrNotFound = errors.New("store: not found")

// Store is the persistence contract for job state and per-step results.
type Store interface {
	// CreateJob inserts a new job. It errors if the session id already exists.
	CreateJob(ctx context.Context, job model.Job) error
	// GetJob returns a job by session id, or ErrNotFound.
	GetJob(ctx context.Context, sessionID string) (model.Job, error)
	// SetJobStatus updates a job's status plus its result/error fields.
	SetJobStatus(ctx context.Context, sessionID string, status model.JobStatus, result, errMsg string) error
	// ListJobsByStatus returns jobs in the given status, oldest first.
	ListJobsByStatus(ctx context.Context, status model.JobStatus) ([]model.Job, error)
	// AppendStepResult records the outcome of one executed step.
	AppendStepResult(ctx context.Context, sr model.StepResult) error
	// ListStepResults returns a session's step results in insertion order.
	ListStepResults(ctx context.Context, sessionID string) ([]model.StepResult, error)
	// Ping reports whether the store is reachable (used by readiness probes).
	Ping(ctx context.Context) error
	// Close releases the underlying resources.
	Close() error
}
