// Package redis implements store.Store on top of Redis. It is an optional
// alternative to the embedded SQLite store for deployments that want job state
// to survive a container restart or be shared across replicas.
//
// The adapter has no third-party dependency: it speaks RESP to Redis over a
// small pooled client (see client.go). Data model:
//
//	jh:seq                 monotonic counter (ordering + step ids)
//	jh:job:<id>            hash of job fields
//	jh:jobs:<STATUS>       sorted set of session ids scored by creation order
//	jh:steps:<id>          list of JSON-encoded step results, insertion order
package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/bigknoxy/j-harness/internal/model"
	"github.com/bigknoxy/j-harness/internal/store"
)

const prefix = "jh:"

// Config configures a RedisStore.
type Config struct {
	// Addr is host:port of the Redis server. Defaults to 127.0.0.1:6379.
	Addr string
	// Password authenticates with AUTH when non-empty.
	Password string
	// DB selects a logical database with SELECT when non-zero.
	DB int
	// KeyPrefix namespaces every key, so several harness deployments can share
	// one Redis. Defaults to "jh:".
	KeyPrefix string
}

// RedisStore is the Redis-backed implementation of store.Store.
type RedisStore struct {
	pool *pool
	pfx  string
}

// Open connects to Redis and verifies the connection with PING.
func Open(cfg Config) (*RedisStore, error) {
	addr := cfg.Addr
	if strings.TrimSpace(addr) == "" {
		addr = "127.0.0.1:6379"
	}
	pfx := cfg.KeyPrefix
	if pfx == "" {
		pfx = prefix
	}
	p := newPool(addr, cfg.Password, cfg.DB)
	s := &RedisStore{pool: p, pfx: pfx}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Ping(ctx); err != nil {
		p.close()
		return nil, err
	}
	return s, nil
}

// Close releases all pooled connections.
func (s *RedisStore) Close() error {
	s.pool.close()
	return nil
}

// Ping verifies the connection is usable.
func (s *RedisStore) Ping(ctx context.Context) error {
	_, err := s.cmd(ctx, "PING")
	if err != nil {
		return fmt.Errorf("redis: ping: %w", err)
	}
	return nil
}

// cmd runs a single command on a pooled connection.
func (s *RedisStore) cmd(ctx context.Context, args ...string) (reply, error) {
	c, err := s.pool.get(ctx)
	if err != nil {
		return reply{}, err
	}
	r, err := c.do(ctx, args...)
	if err != nil {
		// Discard the connection on any error except a Redis-level reply
		// (the connection is still healthy in that case).
		var re *redisError
		if !errors.As(err, &re) {
			c.close()
			return reply{}, err
		}
		// Redis error reply: connection is fine, keep it.
		s.pool.put(c)
		return reply{}, err
	}
	s.pool.put(c)
	return r, nil
}

// multi runs a transaction (MULTI/EXEC) on a pooled connection.
func (s *RedisStore) multi(ctx context.Context, cmds [][]string) ([]reply, error) {
	c, err := s.pool.get(ctx)
	if err != nil {
		return nil, err
	}
	replies, err := c.doMulti(ctx, cmds)
	if err != nil {
		var re *redisError
		if !errors.As(err, &re) {
			c.close()
			return nil, err
		}
		s.pool.put(c)
		return nil, err
	}
	s.pool.put(c)
	return replies, nil
}

// CreateJob inserts a new job. A duplicate session id returns an error.
func (s *RedisStore) CreateJob(ctx context.Context, job model.Job) error {
	if strings.TrimSpace(job.SessionID) == "" {
		return fmt.Errorf("store: session id is required")
	}
	if job.Status == "" {
		job.Status = model.StatusPending
	}

	// Reserve a creation-order score and claim the session id. HSETNX makes
	// the claim atomic: a second creator for the same id gets 0 and errors.
	seqReply, err := s.cmd(ctx, "INCR", s.pfx+"seq")
	if err != nil {
		return fmt.Errorf("store: create job: %w", err)
	}
	seq, err := seqReply.int64()
	if err != nil {
		return fmt.Errorf("store: create job: %w", err)
	}
	claim, err := s.cmd(ctx, "HSETNX", s.jobKey(job.SessionID), "session_id", job.SessionID)
	if err != nil {
		return fmt.Errorf("store: create job: %w", err)
	}
	created, _ := claim.int64()
	if created == 0 {
		return fmt.Errorf("store: create job: session %q already exists", job.SessionID)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	cmds := [][]string{
		{"HSET", s.jobKey(job.SessionID),
			"kind", string(job.Kind),
			"target_id", job.TargetID,
			"status", string(job.Status),
			"input", job.Input,
			"result", job.Result,
			"error", job.Error,
			"seq", strconv.FormatInt(seq, 10),
			"created_at", now,
			"updated_at", now},
		{"ZADD", s.statusKey(job.Status), strconv.FormatInt(seq, 10), job.SessionID},
	}
	if _, err := s.multi(ctx, cmds); err != nil {
		return fmt.Errorf("store: create job: %w", err)
	}
	return nil
}

// GetJob returns a job by session id.
func (s *RedisStore) GetJob(ctx context.Context, sessionID string) (model.Job, error) {
	r, err := s.cmd(ctx, "HGETALL", s.jobKey(sessionID))
	if err != nil {
		return model.Job{}, fmt.Errorf("store: get job: %w", err)
	}
	fields, err := hashToMap(r)
	if err != nil {
		return model.Job{}, fmt.Errorf("store: get job: %w", err)
	}
	if len(fields) == 0 {
		return model.Job{}, store.ErrNotFound
	}
	return jobFromFields(fields), nil
}

// SetJobStatus updates a job's status and outcome fields, moving it between the
// per-status index sets.
func (s *RedisStore) SetJobStatus(ctx context.Context, sessionID string, status model.JobStatus, result, errMsg string) error {
	cur, err := s.cmd(ctx, "HMGET", s.jobKey(sessionID), "status", "seq")
	if err != nil {
		return fmt.Errorf("store: set status: %w", err)
	}
	vals := cur.strings()
	if len(vals) < 2 || vals[0] == "" && vals[1] == "" {
		// A missing job yields an array of nils, decoded as empty strings.
		if _, gerr := s.GetJob(ctx, sessionID); gerr != nil {
			return gerr
		}
	}
	oldStatus := vals[0]
	now := time.Now().UTC().Format(time.RFC3339Nano)
	cmds := [][]string{
		{"HSET", s.jobKey(sessionID), "status", string(status), "result", result, "error", errMsg, "updated_at", now},
	}
	if oldStatus != "" && oldStatus != string(status) {
		cmds = append(cmds, []string{"ZREM", s.statusKey(model.JobStatus(oldStatus)), sessionID})
		cmds = append(cmds, []string{"ZADD", s.statusKey(status), scoreOrDefault(vals), sessionID})
	}
	if _, err := s.multi(ctx, cmds); err != nil {
		return fmt.Errorf("store: set status: %w", err)
	}
	return nil
}

// ListJobsByStatus returns jobs in the given status, oldest first.
func (s *RedisStore) ListJobsByStatus(ctx context.Context, status model.JobStatus) ([]model.Job, error) {
	r, err := s.cmd(ctx, "ZRANGE", s.statusKey(status), "0", "-1")
	if err != nil {
		return nil, fmt.Errorf("store: list jobs: %w", err)
	}
	ids := r.strings()
	jobs := make([]model.Job, 0, len(ids))
	for _, id := range ids {
		job, err := s.GetJob(ctx, id)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				continue
			}
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

// AppendStepResult records one step outcome.
func (s *RedisStore) AppendStepResult(ctx context.Context, sr model.StepResult) error {
	raw, err := json.Marshal(sr)
	if err != nil {
		return fmt.Errorf("store: append step result: %w", err)
	}
	if _, err := s.cmd(ctx, "RPUSH", s.stepsKey(sr.SessionID), string(raw)); err != nil {
		return fmt.Errorf("store: append step result: %w", err)
	}
	return nil
}

// ListStepResults returns a session's step results in insertion order.
func (s *RedisStore) ListStepResults(ctx context.Context, sessionID string) ([]model.StepResult, error) {
	r, err := s.cmd(ctx, "LRANGE", s.stepsKey(sessionID), "0", "-1")
	if err != nil {
		return nil, fmt.Errorf("store: list step results: %w", err)
	}
	items := r.strings()
	out := make([]model.StepResult, 0, len(items))
	for _, item := range items {
		var sr model.StepResult
		if err := json.Unmarshal([]byte(item), &sr); err != nil {
			return nil, fmt.Errorf("store: decode step result: %w", err)
		}
		out = append(out, sr)
	}
	return out, nil
}

func (s *RedisStore) jobKey(sessionID string) string      { return s.pfx + "job:" + sessionID }
func (s *RedisStore) statusKey(st model.JobStatus) string { return s.pfx + "jobs:" + string(st) }
func (s *RedisStore) stepsKey(sessionID string) string    { return s.pfx + "steps:" + sessionID }

// hashToMap converts a flat HGETALL reply into a field map.
func hashToMap(r reply) (map[string]string, error) {
	if r.nil {
		return nil, nil
	}
	if r.kind != '*' {
		return nil, fmt.Errorf("expected array reply, got %q", r.kind)
	}
	m := make(map[string]string, len(r.arr)/2)
	for i := 0; i+1 < len(r.arr); i += 2 {
		m[r.arr[i].str] = r.arr[i+1].str
	}
	return m, nil
}

func jobFromFields(f map[string]string) model.Job {
	return model.Job{
		SessionID: f["session_id"],
		Kind:      model.JobKind(f["kind"]),
		TargetID:  f["target_id"],
		Status:    model.JobStatus(f["status"]),
		Input:     f["input"],
		Result:    f["result"],
		Error:     f["error"],
	}
}

// scoreOrDefault returns the stored creation-order score, or a fresh one when
// the job hash is missing it (e.g. a job created before the field existed).
func scoreOrDefault(vals []string) string {
	if len(vals) >= 2 && vals[1] != "" {
		return vals[1]
	}
	return strconv.FormatInt(time.Now().UnixNano(), 10)
}

// Compile-time check that RedisStore satisfies the Store contract.
var _ store.Store = (*RedisStore)(nil)
