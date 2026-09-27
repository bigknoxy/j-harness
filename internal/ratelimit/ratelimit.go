// Package ratelimit wraps an llm.Client with per-endpoint concurrency and rate
// limiting, so a backend that can only handle N in-flight requests (or R
// requests/sec) is never over-driven no matter how many jobs the worker pool
// or pipelines throw at it.
//
// A concurrency cap (bounded semaphore) is keyed on the resolved BaseURL: N
// concurrent requests against the same backend never exceed the limit, while
// requests to different endpoints proceed independently. An optional token
// bucket provides soft burst shaping on top.
//
// Defaults are conservative for local single-server backends (e.g. one
// llama.cpp server on a memory-constrained laptop): concurrency defaults to 1
// and rate limiting is off unless configured. Cloud backends typically want a
// higher concurrency and an explicit rate limit.
//
// Configuration (env-only, never logged):
//
//	HARNESS_CONCURRENCY_DEFAULT   int   (default 1) max in-flight reqs per endpoint
//	HARNESS_RATE_LIMIT            str   "5/2" = 5 req/s, burst 2 (default: off)
//	Per-endpoint overrides are keyed on the endpoint host:port, uppercased and
//	joined with _, e.g. HARNESS_CONCURRENCY_192_168_8_149_8081.
package ratelimit

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"golang.org/x/time/rate"

	"github.com/bigknoxy/j-harness/internal/llm"
)

// Environment-variable prefixes for per-endpoint overrides. Per-endpoint
// overrides are keyed on the endpoint host:port (see envKey), e.g.
// HARNESS_CONCURRENCY_192_168_8_149_8081. Declared as constants so the names
// are built from a single source rather than string-literal concatenation at
// call sites.
const (
	concurrencyPrefix = "HARNESS_CONCURRENCY_"
	rateLimitPrefix   = "HARNESS_RATE_LIMIT_"
)

// Config sets the limiter defaults.
type Config struct {
	// DefaultConcurrency is the max in-flight requests per endpoint when no
	// per-endpoint override is set. Defaults to 1 (conservative: a single
	// llama.cpp server on a laptop).
	DefaultConcurrency int
	// DefaultRate and DefaultBurst apply a token-bucket per endpoint when set
	// (Rate > 0). Zero disables rate limiting.
	DefaultRate  rate.Limit
	DefaultBurst int
}

// FromEnv builds Config from environment variables, allowing per-endpoint
// overrides named HARNESS_CONCURRENCY_<HOST> and HARNESS_RATE_LIMIT_<HOST>,
// where <HOST> is the base_url host:port with dots/dots/slashes replaced by _.
func FromEnv() Config {
	c := Config{}
	if v := envInt("HARNESS_CONCURRENCY_DEFAULT", 0); v > 0 {
		c.DefaultConcurrency = v
	}
	if r, b, ok := parseRate(envOr("HARNESS_RATE_LIMIT", "")); ok {
		c.DefaultRate = r
		c.DefaultBurst = b
	}
	return c
}

// override carries optional per-request overrides (set by the caller for a
// specific blueprint).
type override struct {
	concurrency int    // > 0 to override the configured value for this endpoint
	rate        string // non-empty to override the rate limit for this endpoint
}

type overrideKey struct{}

// WithOverride attaches per-request concurrency/rate overrides to a context.
// A concurrency <= 0 or empty rate means "use configured default".
func WithOverride(ctx context.Context, concurrency int, rate string) context.Context {
	return context.WithValue(ctx, overrideKey{}, override{
		concurrency: concurrency, rate: rate,
	})
}

// Limiter wraps a Client with per-endpoint concurrency semaphores and optional
// token buckets. Each distinct endpoint gets its own slot; endpoints are
// independent.
type Limiter struct {
	llm.Client
	cfg Config

	mu      sync.Mutex
	sem     map[string]chan struct{}
	limiter map[string]*rate.Limiter
}

// New wraps client and applies cfg across endpoints.
func New(client llm.Client, cfg Config) *Limiter {
	if client == nil {
		panic("ratelimit: client is required")
	}
	if cfg.DefaultConcurrency < 1 {
		cfg.DefaultConcurrency = 1
	}
	return &Limiter{
		Client:  client,
		cfg:     cfg,
		sem:     make(map[string]chan struct{}),
		limiter: make(map[string]*rate.Limiter),
	}
}

// endpointKey normalizes a BaseURL to a stable per-backend key
// (host:port, lowercased), so limits are per server rather than per path.
func endpointKey(baseURL string) string {
	s := strings.TrimSpace(baseURL)
	if s == "" {
		s = "default"
	}
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	return strings.ToLower(s)
}

func (l *Limiter) concurrency(key string, ov override) int {
	// per-endpoint env override wins
	if v := envInt(concurrencyPrefix+envKey(key), 0); v > 0 {
		return v
	}
	if ov.concurrency > 0 {
		return ov.concurrency
	}
	return l.cfg.DefaultConcurrency
}

func (l *Limiter) rateLimit(key string, ov override) (rate.Limit, int) {
	if r, b, ok := parseRate(ov.rate); ok {
		return r, b
	}
	if r, b, ok := parseRate(envOr(rateLimitPrefix+envKey(key), "")); ok {
		return r, b
	}
	return l.cfg.DefaultRate, l.cfg.DefaultBurst
}

// envKey turns an endpoint host:port into an env-var-safe suffix (dots and
// colons -> underscores), e.g. "192.168.8.149:8081" -> "192_168_8_149_8081".
func envKey(host string) string {
	s := strings.NewReplacer(".", "_", ":", "_", "/", "_", "\\", "_").Replace(host)
	return strings.ToUpper(s)
}

// semFor returns the semaphore for an endpoint, resizing its buffer to match
// the requested concurrency.
func (l *Limiter) semFor(key string, n int) chan struct{} {
	l.mu.Lock()
	defer l.mu.Unlock()
	cur := l.sem[key]
	if cur != nil && cap(cur) == n {
		return cur
	}
	ch := make(chan struct{}, n)
	l.sem[key] = ch
	return ch
}

// rateLimiterFor returns (and caches) the token bucket for an endpoint, or nil
// if rate limiting is disabled for it.
func (l *Limiter) rateLimiterFor(key string, limit rate.Limit, burst int) *rate.Limiter {
	if limit <= 0 || burst <= 0 {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if rl, ok := l.limiter[key]; ok {
		return rl
	}
	rl := rate.NewLimiter(limit, burst)
	l.limiter[key] = rl
	return rl
}

// Complete acquires the endpoint's concurrency slot and optional rate token
// before delegating. Context cancellation is honored at both waits so a
// canceled caller never blocks the pool.
func (l *Limiter) Complete(ctx context.Context, req llm.Request) (llm.Response, error) {
	key := endpointKey(req.BaseURL)
	ov, _ := ctx.Value(overrideKey{}).(override)

	rl, burst := l.rateLimit(key, ov)
	if bucket := l.rateLimiterFor(key, rl, burst); bucket != nil {
		if err := bucket.Wait(ctx); err != nil {
			return llm.Response{}, fmt.Errorf("ratelimit: rate wait failed: %w", err)
		}
	}

	sem := l.semFor(key, l.concurrency(key, ov))
	select {
	case sem <- struct{}{}:
		defer func() { <-sem }()
	case <-ctx.Done():
		return llm.Response{}, ctx.Err()
	}

	return l.Client.Complete(ctx, req)
}

// parseRate parses "N/M" (N req per M sec, burst N) or "N" (N req/sec, burst N).
// "0"/"" disables rate limiting.
func parseRate(s string) (rate.Limit, int, bool) {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" {
		return 0, 0, false
	}
	if !strings.Contains(s, "/") {
		n := 0
		if _, err := fmt.Sscanf(s, "%d", &n); err != nil || n <= 0 {
			return 0, 0, false
		}
		return rate.Limit(n), n, true
	}
	var n, d int
	if _, err := fmt.Sscanf(s, "%d/%d", &n, &d); err != nil || n <= 0 || d <= 0 {
		return 0, 0, false
	}
	return rate.Limit(n) / rate.Limit(d), n, true
}

func envOr(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	n := 0
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil || n < 1 {
		return fallback
	}
	return n
}
