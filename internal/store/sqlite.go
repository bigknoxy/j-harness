package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite" // CGO-free SQLite driver

	"github.com/bigknoxy/j-harness/internal/model"
)

// SQLiteStore is the embedded SQLite implementation of Store. It runs in WAL
// mode for concurrent readers alongside a single writer.
type SQLiteStore struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS jobs (
    session_id TEXT PRIMARY KEY,
    kind       TEXT NOT NULL,
    target_id  TEXT NOT NULL,
    status     TEXT NOT NULL,
    input      TEXT NOT NULL DEFAULT '',
    result     TEXT NOT NULL DEFAULT '',
    error      TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status);

CREATE TABLE IF NOT EXISTS step_results (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id  TEXT NOT NULL,
    step_id     TEXT NOT NULL,
    status      TEXT NOT NULL,
    output      TEXT NOT NULL DEFAULT '',
    error       TEXT NOT NULL DEFAULT '',
    tokens      INTEGER NOT NULL DEFAULT 0,
    duration_ms INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_step_results_session ON step_results(session_id);
`

// Open opens (creating if needed) the SQLite database at path and applies the
// schema. The parent directory is created if it does not exist.
func Open(path string) (*SQLiteStore, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("store: path is required")
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("store: create dir: %w", err)
		}
	}

	// synchronous=NORMAL is the recommended WAL pairing: durable across process
	// crashes, and it avoids an fsync per commit (the target host's disk has
	// expensive fsyncs).
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}
	// modernc's SQLite is safe with a small pool; a single writer avoids SQLITE_BUSY.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: apply schema: %w", err)
	}
	return &SQLiteStore{db: db}, nil
}

// Close closes the database.
func (s *SQLiteStore) Close() error { return s.db.Close() }

// Ping verifies the database connection is usable.
func (s *SQLiteStore) Ping(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("store: ping: %w", err)
	}
	return nil
}

// CreateJob inserts a new job.
func (s *SQLiteStore) CreateJob(ctx context.Context, job model.Job) error {
	if strings.TrimSpace(job.SessionID) == "" {
		return fmt.Errorf("store: session id is required")
	}
	if job.Status == "" {
		job.Status = model.StatusPending
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO jobs (session_id, kind, target_id, status, input, result, error)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		job.SessionID, string(job.Kind), job.TargetID, string(job.Status),
		job.Input, job.Result, job.Error)
	if err != nil {
		return fmt.Errorf("store: create job: %w", err)
	}
	return nil
}

// GetJob returns a job by session id.
func (s *SQLiteStore) GetJob(ctx context.Context, sessionID string) (model.Job, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT session_id, kind, target_id, status, input, result, error
		 FROM jobs WHERE session_id = ?`, sessionID)
	return scanJob(row)
}

// SetJobStatus updates a job's status and outcome fields.
func (s *SQLiteStore) SetJobStatus(ctx context.Context, sessionID string, status model.JobStatus, result, errMsg string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE jobs SET status = ?, result = ?, error = ?,
		 updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')
		 WHERE session_id = ?`,
		string(status), result, errMsg, sessionID)
	if err != nil {
		return fmt.Errorf("store: set status: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: set status: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListJobsByStatus returns jobs in the given status, oldest first.
func (s *SQLiteStore) ListJobsByStatus(ctx context.Context, status model.JobStatus) ([]model.Job, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT session_id, kind, target_id, status, input, result, error
		 FROM jobs WHERE status = ? ORDER BY created_at, session_id`, string(status))
	if err != nil {
		return nil, fmt.Errorf("store: list jobs: %w", err)
	}
	defer rows.Close()

	var jobs []model.Job
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

// AppendStepResult records one step outcome.
func (s *SQLiteStore) AppendStepResult(ctx context.Context, sr model.StepResult) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO step_results (session_id, step_id, status, output, error, tokens, duration_ms)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		sr.SessionID, sr.StepID, sr.Status, sr.Output, sr.Error, sr.Tokens, sr.DurationMS)
	if err != nil {
		return fmt.Errorf("store: append step result: %w", err)
	}
	return nil
}

// ListStepResults returns a session's step results in insertion order.
func (s *SQLiteStore) ListStepResults(ctx context.Context, sessionID string) ([]model.StepResult, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT session_id, step_id, status, output, error, tokens, duration_ms
		 FROM step_results WHERE session_id = ? ORDER BY id`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("store: list step results: %w", err)
	}
	defer rows.Close()

	var out []model.StepResult
	for rows.Next() {
		var sr model.StepResult
		if err := rows.Scan(&sr.SessionID, &sr.StepID, &sr.Status, &sr.Output,
			&sr.Error, &sr.Tokens, &sr.DurationMS); err != nil {
			return nil, fmt.Errorf("store: scan step result: %w", err)
		}
		out = append(out, sr)
	}
	return out, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanJob(row rowScanner) (model.Job, error) {
	var job model.Job
	var kind, status string
	if err := row.Scan(&job.SessionID, &kind, &job.TargetID, &status,
		&job.Input, &job.Result, &job.Error); err != nil {
		if err == sql.ErrNoRows {
			return model.Job{}, ErrNotFound
		}
		return model.Job{}, fmt.Errorf("store: scan job: %w", err)
	}
	job.Kind = model.JobKind(kind)
	job.Status = model.JobStatus(status)
	return job, nil
}
