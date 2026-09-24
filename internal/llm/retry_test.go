package llm

import (
	"context"
	"errors"
	"testing"
	"time"
)

type seqClient struct {
	errs  []error
	calls int
}

func (c *seqClient) Complete(ctx context.Context, req Request) (Response, error) {
	i := c.calls
	c.calls++
	if i < len(c.errs) {
		if err := c.errs[i]; err != nil {
			return Response{}, err
		}
	}
	return Response{Content: "ok"}, nil
}

func TestRetryDisabledReturnsUnderlying(t *testing.T) {
	base := &seqClient{}
	c := NewRetry(base, Retry{MaxAttempts: 1})
	if _, ok := c.(*seqClient); !ok {
		t.Fatalf("expected underlying client, got %T", c)
	}
}

func TestRetrySucceedsAfterTransient(t *testing.T) {
	base := &seqClient{errs: []error{&HTTPError{StatusCode: 503, Message: "busy"}, nil}}
	retries := 0
	c := NewRetry(base, Retry{MaxAttempts: 3, BaseDelay: time.Millisecond, OnRetry: func() { retries++ }})
	resp, err := c.Complete(context.Background(), Request{UserInput: "hi"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "ok" || base.calls != 2 || retries != 1 {
		t.Fatalf("content=%q calls=%d retries=%d", resp.Content, base.calls, retries)
	}
}

func TestRetryExhaustsAttempts(t *testing.T) {
	base := &seqClient{errs: []error{
		&HTTPError{StatusCode: 500},
		&HTTPError{StatusCode: 500},
		&HTTPError{StatusCode: 500},
	}}
	retries := 0
	c := NewRetry(base, Retry{MaxAttempts: 3, BaseDelay: time.Millisecond, OnRetry: func() { retries++ }})
	_, err := c.Complete(context.Background(), Request{})
	if err == nil {
		t.Fatal("expected error")
	}
	if base.calls != 3 || retries != 2 {
		t.Fatalf("calls=%d retries=%d", base.calls, retries)
	}
}

func TestRetryFailsFastOnClientError(t *testing.T) {
	base := &seqClient{errs: []error{&HTTPError{StatusCode: 400, Message: "bad"}, nil}}
	c := NewRetry(base, Retry{MaxAttempts: 3, BaseDelay: time.Millisecond})
	_, err := c.Complete(context.Background(), Request{})
	if err == nil {
		t.Fatal("expected error")
	}
	if base.calls != 1 {
		t.Fatalf("expected fail-fast, calls=%d", base.calls)
	}
}

func TestRetryableClassification(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"canceled", context.Canceled, false},
		{"deadline", context.DeadlineExceeded, false},
		{"429", &HTTPError{StatusCode: 429}, true},
		{"500", &HTTPError{StatusCode: 500}, true},
		{"503", &HTTPError{StatusCode: 503}, true},
		{"400", &HTTPError{StatusCode: 400}, false},
		{"401", &HTTPError{StatusCode: 401}, false},
		{"404", &HTTPError{StatusCode: 404}, false},
		{"transport", errors.New("connection reset"), true},
	}
	for _, tc := range cases {
		if got := Retryable(tc.err); got != tc.want {
			t.Errorf("%s: Retryable=%v want %v", tc.name, got, tc.want)
		}
	}
}

func TestRetryContextCanceledStops(t *testing.T) {
	base := &seqClient{errs: []error{&HTTPError{StatusCode: 503}}}
	c := NewRetry(base, Retry{MaxAttempts: 5, BaseDelay: 50 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Complete(ctx, Request{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled, got %v", err)
	}
}
