package metrics

import (
	"strings"
	"testing"
)

func TestIncAndGet(t *testing.T) {
	m := New()
	m.Inc("a")
	m.Inc("a")
	m.Add("b", 3)
	if got := m.Get("a"); got != 2 {
		t.Fatalf("a=%d want 2", got)
	}
	if got := m.Get("b"); got != 3 {
		t.Fatalf("b=%d want 3", got)
	}
	if got := m.Get("missing"); got != 0 {
		t.Fatalf("missing=%d want 0", got)
	}
}

func TestRenderIsSortedPrometheus(t *testing.T) {
	m := New()
	m.Inc("zeta")
	m.Add("alpha", 5)
	out := m.Render()
	alpha := strings.Index(out, "# TYPE alpha counter\nalpha 5")
	zeta := strings.Index(out, "# TYPE zeta counter\nzeta 1")
	if alpha < 0 || zeta < 0 || alpha > zeta {
		t.Fatalf("unexpected render:\n%s", out)
	}
}

func TestNilMetricsSafe(t *testing.T) {
	var m *Metrics
	m.Inc("a")
	m.Add("a", 2)
	if got := m.Get("a"); got != 0 {
		t.Fatalf("nil Get=%d want 0", got)
	}
	if m.Render() != "" {
		t.Fatal("nil Render should be empty")
	}
}

func TestConcurrentIncAndRender(t *testing.T) {
	m := New()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 500; i++ {
			m.Inc("dynamic")
			m.Render()
		}
	}()
	for i := 0; i < 500; i++ {
		m.Inc("other")
	}
	<-done
}
