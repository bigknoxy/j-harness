package llm

import (
	"context"
	"sync"
)

// Fake is a deterministic in-memory Client for tests. Responses are returned in
// order; the last one repeats once exhausted. It records every request.
type Fake struct {
	mu        sync.Mutex
	responses []Response
	errs      []error
	Requests  []Request
	next      int
}

// NewFake builds a Fake that returns the given responses in order.
func NewFake(responses ...Response) *Fake {
	return &Fake{responses: responses}
}

// NewFakeErr builds a Fake whose calls fail with err.
func NewFakeErr(err error) *Fake {
	return &Fake{errs: []error{err}}
}

// Complete implements Client.
func (f *Fake) Complete(_ context.Context, req Request) (Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Requests = append(f.Requests, req)

	if len(f.errs) > 0 {
		i := f.next
		if i >= len(f.errs) {
			i = len(f.errs) - 1
		}
		f.next++
		return Response{}, f.errs[i]
	}
	if len(f.responses) == 0 {
		return Response{}, ErrUnsupported
	}
	i := f.next
	if i >= len(f.responses) {
		i = len(f.responses) - 1
	}
	f.next++
	return f.responses[i], nil
}

// CallCount returns how many completions have been requested.
func (f *Fake) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.Requests)
}
