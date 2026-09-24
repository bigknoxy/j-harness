package llm

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"
)

// HTTPError describes a non-2xx response from an OpenAI-compatible endpoint.
// It lets the retry layer classify failures by status code.
type HTTPError struct {
	StatusCode int
	Message    string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("llm: endpoint error %d: %s", e.StatusCode, e.Message)
}

// Retry configures the retry decorator. MaxAttempts counts the initial call, so
// MaxAttempts 3 means one try plus two retries. MaxAttempts <= 1 disables retry.
type Retry struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	OnRetry     func() // optional; called once per retry (e.g. a metrics counter)
}

// RetryClient retries a wrapped Client on transient failures.
type RetryClient struct {
	client Client
	cfg    Retry
}

// NewRetry wraps client with retry behavior. When retries are disabled
// (MaxAttempts <= 1) it returns the underlying client unchanged.
func NewRetry(client Client, cfg Retry) Client {
	if cfg.MaxAttempts <= 1 {
		return client
	}
	if cfg.BaseDelay <= 0 {
		cfg.BaseDelay = 250 * time.Millisecond
	}
	if cfg.MaxDelay <= 0 {
		cfg.MaxDelay = 5 * time.Second
	}
	return &RetryClient{client: client, cfg: cfg}
}

// Complete calls the wrapped client, retrying transient failures with
// exponential backoff and jitter.
func (r *RetryClient) Complete(ctx context.Context, req Request) (Response, error) {
	var lastErr error
	for attempt := 1; attempt <= r.cfg.MaxAttempts; attempt++ {
		resp, err := r.client.Complete(ctx, req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if !Retryable(err) || attempt == r.cfg.MaxAttempts {
			return Response{}, err
		}
		if r.cfg.OnRetry != nil {
			r.cfg.OnRetry()
		}
		if err := sleep(ctx, r.backoff(attempt)); err != nil {
			return Response{}, err
		}
	}
	return Response{}, lastErr
}

// Retryable reports whether err is worth retrying: transport failures and HTTP
// 429/5xx are transient; other HTTP errors (bad request, auth, not found) and
// context cancellation are not.
func Retryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.StatusCode == 429 || httpErr.StatusCode >= 500
	}
	// Transport-level errors (dial, reset, EOF) are retryable.
	return true
}

func (r *RetryClient) backoff(attempt int) time.Duration {
	d := r.cfg.BaseDelay
	for i := 1; i < attempt; i++ {
		d *= 2
		if d >= r.cfg.MaxDelay {
			d = r.cfg.MaxDelay
			break
		}
	}
	if d <= 0 {
		return 0
	}
	// Full jitter in [d/2, d).
	half := d / 2
	return half + time.Duration(rand.Int64N(int64(half)+1))
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
