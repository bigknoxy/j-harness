// Package e2e contains real end-to-end HTTP tests. They stand up the full stack
// over httptest.Server: a temp SQLite store, the registry, the engine, the worker
// pool, api.Handler(), and an OpenAI-compatible stub endpoint that the real
// internal/llm client talks to. No network access is required.
package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bigknoxy/j-harness/internal/api"
	"github.com/bigknoxy/j-harness/internal/engine"
	"github.com/bigknoxy/j-harness/internal/llm"
	"github.com/bigknoxy/j-harness/internal/llmstub"
	"github.com/bigknoxy/j-harness/internal/metrics"
	"github.com/bigknoxy/j-harness/internal/model"
	"github.com/bigknoxy/j-harness/internal/registry"
	"github.com/bigknoxy/j-harness/internal/store"
)

// TestMain prefers a tmpfs temp dir when available; SQLite setup dominates the
// suite on hosts whose /tmp is on a slow disk.
func TestMain(m *testing.M) {
	if _, err := os.Stat("/dev/shm"); err == nil {
		if dir, err := os.MkdirTemp("/dev/shm", "jharness-e2e"); err == nil {
			_ = os.Setenv("TMPDIR", "/dev/shm")
			code := m.Run()
			_ = os.RemoveAll(dir)
			os.Exit(code)
		}
	}
	os.Exit(m.Run())
}

const (
	// registryPath is the checked-in registry relative to internal/e2e.
	registryPath = "../../agent-registry"
	// pollDeadline bounds polling for terminal job status. The stub is instant,
	// so this only guards against a genuine hang.
	pollDeadline = 5 * time.Second
)

// stack is a fully-wired harness reachable over real HTTP.
type stack struct {
	base         string // harness base URL, e.g. http://127.0.0.1:port
	stub         *llmstub.Server
	registryRoot string
	client       *http.Client
}

// newStack builds the full stack with an OpenAI-compatible stub. When writable
// is true the registry is a throwaway copy so write tests never touch the
// checked-in files.
func newStack(t *testing.T, script llmstub.Script, token string, writable bool) *stack {
	t.Helper()

	root := registryPath
	if writable {
		root = filepath.Join(t.TempDir(), "registry")
		copyDir(t, registryPath, root)
	}
	reg, err := registry.Load(root)
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}

	st, err := store.Open(filepath.Join(t.TempDir(), "e2e.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	srv := llmstub.New(script)
	t.Cleanup(srv.Close)

	baseClient, err := llm.NewOpenAI(llm.OpenAIConfig{
		BaseURL: srv.BaseURL(),
		Model:   "stub-model",
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("llm.NewOpenAI: %v", err)
	}
	// Wrap with retries to exercise the production client path; the stub always
	// succeeds so no retry actually fires.
	client := llm.NewRetry(baseClient, llm.Retry{MaxAttempts: 2, BaseDelay: 5 * time.Millisecond})

	eng, err := engine.New(reg, client)
	if err != nil {
		t.Fatalf("engine.New: %v", err)
	}
	met := metrics.New()
	eng.SetMetrics(met)

	pool, err := engine.NewPool(engine.PoolConfig{
		Store: st, Engine: eng, Workers: 4, Logger: log.New(io.Discard, "", 0), Metrics: met,
	})
	if err != nil {
		t.Fatalf("engine.NewPool: %v", err)
	}
	t.Cleanup(pool.Close)

	a, err := api.New(api.Config{
		Engine: eng, Registry: reg, Pool: pool, Store: st,
		Version: "e2e", AuthToken: token, Metrics: met,
		Logger: log.New(io.Discard, "", 0),
	})
	if err != nil {
		t.Fatalf("api.New: %v", err)
	}
	ts := httptest.NewServer(a.Handler())
	t.Cleanup(ts.Close)

	return &stack{
		base:         ts.URL,
		stub:         srv,
		registryRoot: root,
		client:       &http.Client{Timeout: 10 * time.Second},
	}
}

// do issues one HTTP request and returns status + body.
func (s *stack) do(t *testing.T, method, path, token, body string) (int, []byte) {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, s.base+path, r)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, data
}

// --- wire types (mirrors the api package's unexported envelope) ---

type submitResp struct {
	SessionID string `json:"session_id"`
	Status    string `json:"status"`
}

type sessionResp struct {
	SessionID string `json:"session_id"`
	Kind      string `json:"kind"`
	TargetID  string `json:"target_id"`
	Status    string `json:"status"`
	Result    string `json:"result"`
	Error     string `json:"error"`
}

type stepResp struct {
	StepID string `json:"step_id"`
	Status string `json:"status"`
	Output string `json:"output"`
	Error  string `json:"error"`
}

type stepsResp struct {
	SessionID string     `json:"session_id"`
	Steps     []stepResp `json:"steps"`
}

func decode[T any](t *testing.T, data []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("decode %q: %v", string(data), err)
	}
	return v
}

// submit executes an agent or pipeline and returns the 202 session id.
func (s *stack) submit(t *testing.T, path, body string) submitResp {
	t.Helper()
	code, data := s.do(t, http.MethodPost, path, "", body)
	if code != http.StatusAccepted {
		t.Fatalf("POST %s = %d, want 202 (body %s)", path, code, data)
	}
	sub := decode[submitResp](t, data)
	if sub.SessionID == "" {
		t.Fatalf("POST %s returned no session_id: %s", path, data)
	}
	return sub
}

// waitSession polls until the session reaches a terminal state or the deadline.
func (s *stack) waitSession(t *testing.T, sessionID string) sessionResp {
	t.Helper()
	deadline := time.Now().Add(pollDeadline)
	for time.Now().Before(deadline) {
		code, data := s.do(t, http.MethodGet, "/v1/sessions/"+sessionID, "", "")
		if code != http.StatusOK {
			t.Fatalf("get session = %d (body %s)", code, data)
		}
		got := decode[sessionResp](t, data)
		if got.Status == string(model.StatusCompleted) || got.Status == string(model.StatusFailed) {
			return got
		}
		time.Sleep(3 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for session %s", sessionID)
	return sessionResp{}
}

// steps fetches the persisted step results for a session.
func (s *stack) steps(t *testing.T, sessionID string) []stepResp {
	t.Helper()
	code, data := s.do(t, http.MethodGet, "/v1/sessions/"+sessionID+"/steps", "", "")
	if code != http.StatusOK {
		t.Fatalf("get steps = %d (body %s)", code, data)
	}
	return decode[stepsResp](t, data).Steps
}

// stepStatus returns a step's status, or "MISSING".
func stepStatus(steps []stepResp, id string) string {
	for _, st := range steps {
		if st.StepID == id {
			return st.Status
		}
	}
	return "MISSING"
}

// scriptRouter routes stub responses by the agent system prompt and, for triage,
// by the inbound message. This is order-independent, so it is safe for
// concurrent DAG fan-out.
func scriptRouter(req llm.Request) llm.Response {
	sp := llmstub.SystemPrompt(req)
	switch {
	case strings.Contains(sp, "support-triage classifier"):
		return llm.Response{Content: triageClassification(llmstub.LastUser(req)), TotalTokens: 2}
	case strings.Contains(sp, "technical summarizer"):
		return llm.Response{Content: "The customer reported an outage.", TotalTokens: 2}
	case strings.Contains(sp, "risk assessor"):
		return llm.Response{Content: "High urgency: production is down.", TotalTokens: 2}
	default:
		return llm.Response{Content: "support reply", TotalTokens: 2}
	}
}

// triageClassification maps a customer message to a deterministic triage result.
func triageClassification(input string) string {
	lower := strings.ToLower(input)
	switch {
	case strings.Contains(lower, "charge") || strings.Contains(lower, "invoice") ||
		strings.Contains(lower, "refund") || strings.Contains(lower, "billing"):
		return `{"category":"billing","priority":"high"}`
	case strings.Contains(lower, "crash") || strings.Contains(lower, "error") ||
		strings.Contains(lower, "bug") || strings.Contains(lower, "technical"):
		return `{"category":"technical","priority":"normal"}`
	default:
		return `{"category":"other","priority":"low"}`
	}
}

func TestE2EAgentExecuteAsync(t *testing.T) {
	s := newStack(t, scriptRouter, "", false)

	sub := s.submit(t, "/v1/agents/triage/execute", `{"input_data":"I was charged twice for my subscription"}`)
	if sub.Status != string(model.StatusPending) {
		t.Fatalf("submit status = %q, want PENDING", sub.Status)
	}

	got := s.waitSession(t, sub.SessionID)
	if got.Status != string(model.StatusCompleted) {
		t.Fatalf("status = %s error = %s", got.Status, got.Error)
	}
	if got.Kind != string(model.KindAgent) || got.TargetID != "triage" {
		t.Fatalf("session = %+v", got)
	}
	if got.Result != `{"category":"billing","priority":"high"}` {
		t.Fatalf("result = %q", got.Result)
	}

	steps := s.steps(t, sub.SessionID)
	if len(steps) != 1 {
		t.Fatalf("steps = %+v, want 1", steps)
	}
	if steps[0].StepID != "triage" || steps[0].Status != string(model.StatusCompleted) {
		t.Fatalf("step = %+v", steps[0])
	}
	if !strings.Contains(steps[0].Output, `"billing"`) {
		t.Fatalf("step output = %q", steps[0].Output)
	}
	if s.stub.CallCount() != 1 {
		t.Fatalf("stub calls = %d, want 1", s.stub.CallCount())
	}
}

func TestE2EPipelineRoutingBranches(t *testing.T) {
	s := newStack(t, scriptRouter, "", false)

	cases := []struct {
		name        string
		input       string
		wantRan     string
		wantSkipped []string
	}{
		{"billing", "I was overcharged on my invoice", "billing_reply", []string{"tech_reply", "generic_reply"}},
		{"technical", "The app crashes with an error on startup", "tech_reply", []string{"billing_reply", "generic_reply"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"user_input":%q}`, tc.input)
			sub := s.submit(t, "/v1/pipelines/support_flow/execute", body)
			got := s.waitSession(t, sub.SessionID)
			if got.Status != string(model.StatusCompleted) {
				t.Fatalf("status = %s error = %s", got.Status, got.Error)
			}
			if got.Kind != string(model.KindPipeline) {
				t.Fatalf("kind = %q", got.Kind)
			}

			steps := s.steps(t, sub.SessionID)
			if st := stepStatus(steps, "triage"); st != string(model.StatusCompleted) {
				t.Errorf("triage status = %s", st)
			}
			if st := stepStatus(steps, "route"); st != string(model.StatusCompleted) {
				t.Errorf("route status = %s", st)
			}
			if st := stepStatus(steps, tc.wantRan); st != string(model.StatusCompleted) {
				t.Errorf("%s status = %s, want COMPLETED (steps %+v)", tc.wantRan, st, steps)
			}
			for _, id := range tc.wantSkipped {
				if st := stepStatus(steps, id); st != string(model.StatusSkipped) {
					t.Errorf("%s status = %s, want SKIPPED", id, st)
				}
			}
		})
	}
}

func TestE2EMetricsAfterJobs(t *testing.T) {
	s := newStack(t, llmstub.ByPrompt(map[string]string{"*": "ok"}), "", false)

	sub := s.submit(t, "/v1/agents/generic_agent/execute", `{"input_data":"hi"}`)
	s.waitSession(t, sub.SessionID)

	code, data := s.do(t, http.MethodGet, "/metrics", "", "")
	if code != http.StatusOK {
		t.Fatalf("/metrics = %d", code)
	}
	for _, want := range []string{"harness_jobs_submitted_total", "harness_jobs_completed_total"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("/metrics missing %s: %s", want, data)
		}
	}
}

func TestE2EDAGPipeline(t *testing.T) {
	s := newStack(t, scriptRouter, "", false)

	sub := s.submit(t, "/v1/pipelines/digest_flow/execute", `{"user_input":"Service was down for an hour"}`)
	got := s.waitSession(t, sub.SessionID)
	if got.Status != string(model.StatusCompleted) {
		t.Fatalf("status = %s error = %s", got.Status, got.Error)
	}
	if got.Result != "support reply" {
		t.Fatalf("result = %q, want support reply", got.Result)
	}

	steps := s.steps(t, sub.SessionID)
	for _, id := range []string{"summarize", "assess_risk", "combine"} {
		if st := stepStatus(steps, id); st != string(model.StatusCompleted) {
			t.Errorf("%s status = %s, want COMPLETED (steps %+v)", id, st, steps)
		}
	}
}

func TestE2EAuthBoundary(t *testing.T) {
	const token = "e2e-secret"
	s := newStack(t, llmstub.ByPrompt(map[string]string{"*": "ok"}), token, false)

	// /v1 without a bearer token is rejected.
	if code, _ := s.do(t, http.MethodGet, "/v1/sessions/whatever", "", ""); code != http.StatusUnauthorized {
		t.Errorf("unauthenticated /v1 = %d, want 401", code)
	}
	if code, _ := s.do(t, http.MethodPost, "/v1/agents/generic_agent/execute", "", `{"input_data":"x"}`); code != http.StatusUnauthorized {
		t.Errorf("unauthenticated execute = %d, want 401", code)
	}
	// With the token, the route works (unknown session -> 404).
	if code, _ := s.do(t, http.MethodGet, "/v1/sessions/whatever", token, ""); code != http.StatusNotFound {
		t.Errorf("authenticated unknown session = %d, want 404", code)
	}
	// Health/readiness/metrics stay unauthenticated.
	for _, path := range []string{"/healthz", "/readyz", "/metrics"} {
		if code, data := s.do(t, http.MethodGet, path, "", ""); code != http.StatusOK {
			t.Errorf("%s = %d, want 200 (body %s)", path, code, data)
		}
	}
}

func TestE2ERegistryCRUDOverHTTP(t *testing.T) {
	s := newStack(t, llmstub.ByPrompt(map[string]string{"*": "ok"}), "", true)

	// Create.
	create := `{"blueprint":{"id":"e2e_scribe","prompt_path":"prompts/e2e_scribe.md","model":"m","output_format":"text","version":1},"prompt":"You write."}`
	if code, data := s.do(t, http.MethodPost, "/v1/registry/agents/e2e_scribe", "", create); code != http.StatusOK {
		t.Fatalf("create = %d (body %s)", code, data)
	}

	// List includes it.
	code, data := s.do(t, http.MethodGet, "/v1/registry/agents", "", "")
	if code != http.StatusOK {
		t.Fatalf("list = %d", code)
	}
	if !strings.Contains(string(data), "e2e_scribe") {
		t.Fatalf("list missing e2e_scribe: %s", data)
	}

	// Detail.
	code, data = s.do(t, http.MethodGet, "/v1/registry/agents/e2e_scribe", "", "")
	if code != http.StatusOK {
		t.Fatalf("detail = %d", code)
	}
	var detail struct {
		Blueprint model.AgentBlueprint `json:"blueprint"`
		Prompt    string               `json:"prompt"`
	}
	if err := json.Unmarshal(data, &detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if detail.Blueprint.Model != "m" || detail.Prompt != "You write." {
		t.Fatalf("detail = %+v", detail)
	}

	// Update.
	update := `{"blueprint":{"id":"e2e_scribe","prompt_path":"prompts/e2e_scribe.md","model":"m2","output_format":"text","version":1},"prompt":"Updated."}`
	if code, data := s.do(t, http.MethodPut, "/v1/registry/agents/e2e_scribe", "", update); code != http.StatusOK {
		t.Fatalf("update = %d (body %s)", code, data)
	}
	_, data = s.do(t, http.MethodGet, "/v1/registry/agents/e2e_scribe", "", "")
	_ = json.Unmarshal(data, &detail)
	if detail.Blueprint.Model != "m2" || detail.Prompt != "Updated." {
		t.Fatalf("after update = %+v", detail)
	}

	// Invalid write (missing model) is a 400 and leaves the registry intact.
	invalid := `{"blueprint":{"id":"e2e_scribe","prompt_path":"prompts/e2e_scribe.md","version":1},"prompt":"x"}`
	if code, _ := s.do(t, http.MethodPut, "/v1/registry/agents/e2e_scribe", "", invalid); code != http.StatusBadRequest {
		t.Fatalf("invalid update = %d, want 400", code)
	}

	// The written files exist on disk in the throwaway copy.
	if _, err := os.Stat(filepath.Join(s.registryRoot, "blueprints", "e2e_scribe.json")); err != nil {
		t.Fatalf("blueprint not written: %v", err)
	}
}

func TestE2EConcurrencySmoke(t *testing.T) {
	s := newStack(t, llmstub.ByPrompt(map[string]string{"*": "done"}), "", false)

	const jobs = 12
	ids := make([]string, 0, jobs)
	for i := 0; i < jobs; i++ {
		sub := s.submit(t, "/v1/agents/generic_agent/execute",
			fmt.Sprintf(`{"input_data":"job %d"}`, i))
		ids = append(ids, sub.SessionID)
	}
	for _, id := range ids {
		got := s.waitSession(t, id)
		if got.Status != string(model.StatusCompleted) || got.Result != "done" {
			t.Fatalf("session %s = %+v", id, got)
		}
	}
	if s.stub.CallCount() != jobs {
		t.Fatalf("stub calls = %d, want %d", s.stub.CallCount(), jobs)
	}
}

// copyDir recursively copies src into dst.
func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dst, err)
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	for _, e := range entries {
		s := filepath.Join(src, e.Name())
		d := filepath.Join(dst, e.Name())
		if e.IsDir() {
			copyDir(t, s, d)
			continue
		}
		data, err := os.ReadFile(s)
		if err != nil {
			t.Fatalf("read %s: %v", s, err)
		}
		if err := os.WriteFile(d, data, 0o644); err != nil {
			t.Fatalf("write %s: %v", d, err)
		}
	}
}
