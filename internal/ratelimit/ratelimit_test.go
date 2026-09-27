package ratelimit

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"golang.org/x/time/rate"

	"github.com/bigknoxy/j-harness/internal/llm"
)

// fake is a minimal llm.Client that records concurrency.
type fake struct {
	mu       sync.Mutex
	maxPar   int
	seen     int
	inFlight int
	delay    time.Duration
}

func (f *fake) Complete(ctx context.Context, _ llm.Request) (llm.Response, error) {
	f.mu.Lock()
	f.inFlight++
	f.seen++
	if f.inFlight > f.maxPar {
		f.maxPar = f.inFlight
	}
	f.mu.Unlock()

	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			f.mu.Lock()
			f.inFlight--
			f.mu.Unlock()
			return llm.Response{}, ctx.Err()
		}
	}
	f.mu.Lock()
	f.inFlight--
	f.mu.Unlock()
	return llm.Response{Content: "ok"}, nil
}

func TestConcurrencyCapPerEndpoint(t *testing.T) {
	f := &fake{delay: 50 * time.Millisecond}
	lim := New(f, Config{DefaultConcurrency: 1})

	const n = 4
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			if _, err := lim.Complete(context.Background(), llm.Request{BaseURL: "http://a:8080/v1"}); err != nil {
				t.Errorf("unexpected err: %v", err)
			}
		}()
	}
	wg.Wait()

	if got := f.maxPar; got != 1 {
		t.Fatalf("max in-flight = %d, want 1 (single endpoint concurrency cap)", got)
	}
	if f.seen != n {
		t.Fatalf("requests completed = %d, want %d", f.seen, n)
	}
}

func TestConcurrencyIndependentAcrossEndpoints(t *testing.T) {
	f := &fake{delay: 40 * time.Millisecond}
	lim := New(f, Config{DefaultConcurrency: 1})

	var wg sync.WaitGroup
	// 2 calls to endpoint A + 2 calls to endpoint B → each endpoint sees max 1,
	// but across endpoints they overlap (maxPar counts peak across all calls).
	for _, base := range []string{"http://a:8080/v1", "http://b:8081/v1"} {
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func(b string) {
				defer wg.Done()
				lim.Complete(context.Background(), llm.Request{BaseURL: b})
			}(base)
		}
	}
	wg.Wait()
	// peak in-flight across both endpoints can be 2 (one per endpoint)
	if f.maxPar > 2 {
		t.Fatalf("max in-flight = %d, want <= 2", f.maxPar)
	}
}

func TestBlueprintOverrideRaisesConcurrency(t *testing.T) {
	f := &fake{delay: 50 * time.Millisecond}
	lim := New(f, Config{DefaultConcurrency: 1})

	const n = 4
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			ctx := WithOverride(context.Background(), 4, "")
			lim.Complete(ctx, llm.Request{BaseURL: "http://a:8080/v1"})
		}()
	}
	wg.Wait()
	if got := f.maxPar; got != 4 {
		t.Fatalf("max in-flight with override=4 = %d, want 4", got)
	}
}

func TestRateLimitShapesBurst(t *testing.T) {
	f := &fake{}
	lim := New(f, Config{
		DefaultConcurrency: 4,
		DefaultRate:        rate.Limit(2), // 2 req/s
		DefaultBurst:       1,
	})

	start := time.Now()
	const n = 3
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() { defer wg.Done(); lim.Complete(context.Background(), llm.Request{BaseURL: "http://a:8080/v1"}) }()
	}
	wg.Wait()
	elapsed := time.Since(start)
	// 3 requests at 2/s with burst 1 → the 2nd and 3rd must wait ~1s each.
	if elapsed < 900*time.Millisecond {
		t.Fatalf("elapsed=%v, rate limiting did not engage", elapsed)
	}
}

func TestContextCancellationReleasesSlot(t *testing.T) {
	f := &fake{delay: 5 * time.Second} // holds the single slot for a while
	lim := New(f, Config{DefaultConcurrency: 1})

	// occupy the only slot with a long-running request
	go func() {
		lim.Complete(context.Background(), llm.Request{BaseURL: "http://a:8080/v1"})
	}()
	// give it the slot
	time.Sleep(30 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	errCh := make(chan error, 1)
	go func() { _, err := lim.Complete(ctx, llm.Request{BaseURL: "http://a:8080/v1"}); errCh <- err }()
	if err := <-errCh; !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v, want context.Canceled", err)
	}
}

func TestParseRate(t *testing.T) {
	cases := []struct {
		in string
		ok bool
	}{
		{"", false}, {"0", false}, {"off", false},
		{"4", true}, {"4/2", true}, {"10/1", true},
		{"x", false}, {"4/x", false},
	}
	for _, c := range cases {
		_, _, ok := parseRate(c.in)
		if ok != c.ok {
			t.Errorf("parseRate(%q) ok=%v want %v", c.in, ok, c.ok)
		}
	}
	if lim, b, ok := parseRate("4/2"); !ok || lim != 2 || b != 4 {
		t.Errorf("4/2 -> %v,%d,%v; want 2,4,true", lim, b, ok)
	}
}

func TestEndpointKey(t *testing.T) {
	cases := map[string]string{
		"http://192.168.8.149:8081/v1": "192.168.8.149:8081",
		"http://A:8080/v1/":            "a:8080",
		"":                             "default",
		"http://x/v1?foo=1":            "x",
	}
	for in, want := range cases {
		if got := endpointKey(in); got != want {
			t.Errorf("endpointKey(%q)=%q want %q", in, got, want)
		}
	}
}
