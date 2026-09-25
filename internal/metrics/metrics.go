// Package metrics holds tiny in-process counters rendered as Prometheus text.
// It deliberately avoids a metrics dependency: five atomic counters are enough
// to see whether the harness is healthy.
package metrics

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// Metrics is a set of named counters. The zero value is not usable; call New.
type Metrics struct {
	mu       sync.RWMutex
	counters map[string]*atomic.Int64
}

// New returns a Metrics with the standard harness counters pre-registered so
// they are always visible on /metrics, even before the first increment.
func New() *Metrics {
	m := &Metrics{counters: map[string]*atomic.Int64{}}
	for _, name := range []string{JobsSubmitted, JobsCompleted, JobsFailed, LLMRetries, SchemaFailures} {
		m.counters[name] = &atomic.Int64{}
	}
	return m
}

func (m *Metrics) counter(name string) *atomic.Int64 {
	if m == nil {
		return &atomic.Int64{}
	}
	m.mu.RLock()
	c, ok := m.counters[name]
	m.mu.RUnlock()
	if ok {
		return c
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.counters[name]; ok {
		return c
	}
	c = &atomic.Int64{}
	m.counters[name] = c
	return c
}

// Inc increments the counter named name by one.
func (m *Metrics) Inc(name string) { m.counter(name).Add(1) }

// Add increments the counter named name by n.
func (m *Metrics) Add(name string, n int64) { m.counter(name).Add(n) }

// Get returns the current value of a counter.
func (m *Metrics) Get(name string) int64 { return m.counter(name).Load() }

// Render returns all counters in Prometheus text exposition format.
func (m *Metrics) Render() string {
	if m == nil {
		return ""
	}
	m.mu.RLock()
	names := make([]string, 0, len(m.counters))
	for name := range m.counters {
		names = append(names, name)
	}
	m.mu.RUnlock()
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		fmt.Fprintf(&b, "# TYPE %s counter\n%s %d\n", name, name, m.counter(name).Load())
	}
	return b.String()
}

// Metric names used by the harness. Keep these stable; they are a public API.
const (
	JobsSubmitted  = "harness_jobs_submitted_total"
	JobsCompleted  = "harness_jobs_completed_total"
	JobsFailed     = "harness_jobs_failed_total"
	LLMRetries     = "harness_llm_retries_total"
	SchemaFailures = "harness_schema_failures_total"
)
