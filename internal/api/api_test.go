package api

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bigknoxy/j-harness/internal/engine"
	"github.com/bigknoxy/j-harness/internal/llm"
	"github.com/bigknoxy/j-harness/internal/model"
	"github.com/bigknoxy/j-harness/internal/registry"
	"github.com/bigknoxy/j-harness/internal/store"
)

// TestMain prefers a tmpfs temp dir when available (SQLite setup on a slow disk
// otherwise dominates the suite).
func TestMain(m *testing.M) {
	if _, err := os.Stat("/dev/shm"); err == nil {
		if dir, err := os.MkdirTemp("/dev/shm", "jharness-api"); err == nil {
			_ = os.Setenv("TMPDIR", "/dev/shm")
			code := m.Run()
			_ = os.RemoveAll(dir)
			os.Exit(code)
		}
	}
	os.Exit(m.Run())
}

func testAPI(t *testing.T, token string, responses ...llm.Response) (*API, *llm.Fake) {
	t.Helper()
	reg, err := registry.Load("../../agent-registry")
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	fake := llm.NewFake(responses...)
	eng, err := engine.New(reg, fake)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	pool, err := engine.NewPool(engine.PoolConfig{
		Store: st, Engine: eng, Workers: 1, Logger: log.New(io.Discard, "", 0),
	})
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	t.Cleanup(pool.Close)

	a, err := New(Config{
		Engine:    eng,
		Registry:  reg,
		Pool:      pool,
		Store:     st,
		Version:   "test",
		AuthToken: token,
		Logger:    log.New(io.Discard, "", 0),
	})
	if err != nil {
		t.Fatalf("new api: %v", err)
	}
	return a, fake
}

func do(t *testing.T, a *API, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	return rec
}

func decodeErr(t *testing.T, rec *httptest.ResponseRecorder) errorBody {
	t.Helper()
	var e errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatalf("decode error body %q: %v", rec.Body.String(), err)
	}
	return e
}

// waitSession polls GET /v1/sessions/{id} until terminal or deadline.
func waitSession(t *testing.T, a *API, sessionID string) sessionResponse {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		rec := do(t, a, http.MethodGet, "/v1/sessions/"+sessionID, "", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("get session: status %d body %s", rec.Code, rec.Body)
		}
		var got sessionResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode session: %v", err)
		}
		if got.Status == string(model.StatusCompleted) || got.Status == string(model.StatusFailed) {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for session %s", sessionID)
	return sessionResponse{}
}

func TestExecuteAgentAsyncSuccess(t *testing.T) {
	a, fake := testAPI(t, "", llm.Response{Content: "hello there", TotalTokens: 5})

	rec := do(t, a, http.MethodPost, "/v1/agents/generic_agent/execute", "", `{"input_data":"hi"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (body %s)", rec.Code, rec.Body)
	}
	var sub submitResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &sub); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if sub.SessionID == "" {
		t.Fatal("expected a session_id")
	}
	if sub.Status != string(model.StatusPending) {
		t.Fatalf("status = %q, want PENDING", sub.Status)
	}

	got := waitSession(t, a, sub.SessionID)
	if got.Status != string(model.StatusCompleted) || got.Result != "hello there" {
		t.Fatalf("session = %+v", got)
	}
	if got.TargetID != "generic_agent" || got.Kind != string(model.KindAgent) {
		t.Fatalf("session = %+v", got)
	}
	if fake.CallCount() != 1 {
		t.Fatalf("client calls = %d, want 1", fake.CallCount())
	}
	if fake.Requests[0].UserInput != "hi" {
		t.Fatalf("user input = %q, want hi", fake.Requests[0].UserInput)
	}
}

func TestExecuteAgentAsyncFailure(t *testing.T) {
	// A Fake with no responses returns ErrUnsupported, so RunAgent fails and the
	// job ends FAILED with an error message.
	a, _ := testAPINoResponses(t)

	rec := do(t, a, http.MethodPost, "/v1/agents/triage/execute", "", `{"input_data":"x"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rec.Code)
	}
	var sub submitResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &sub)
	got := waitSession(t, a, sub.SessionID)
	if got.Status != string(model.StatusFailed) || got.Error == "" {
		t.Fatalf("session = %+v, want FAILED with error", got)
	}
}

// testAPINoResponses builds an API whose Fake returns ErrUnsupported (no
// responses configured), so any RunAgent call fails.
func testAPINoResponses(t *testing.T) (*API, *llm.Fake) {
	t.Helper()
	reg, err := registry.Load("../../agent-registry")
	if err != nil {
		t.Fatal(err)
	}
	fake := llm.NewFake()
	eng, _ := engine.New(reg, fake)
	st, err := store.Open(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	pool, err := engine.NewPool(engine.PoolConfig{Store: st, Engine: eng, Workers: 1, Logger: log.New(io.Discard, "", 0)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	a, err := New(Config{Engine: eng, Registry: reg, Pool: pool, Store: st, Version: "test", Logger: log.New(io.Discard, "", 0)})
	if err != nil {
		t.Fatal(err)
	}
	return a, fake
}

func TestExecuteAgentUnknown(t *testing.T) {
	a, fake := testAPI(t, "")
	rec := do(t, a, http.MethodPost, "/v1/agents/nope/execute", "", `{"input_data":"x"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if e := decodeErr(t, rec); e.Error.Code != "not_found" {
		t.Fatalf("error code = %q", e.Error.Code)
	}
	if fake.CallCount() != 0 {
		t.Fatalf("client should not be called")
	}
}

func TestExecuteAgentBadBody(t *testing.T) {
	a, _ := testAPI(t, "")
	cases := []struct{ name, body string }{
		{"empty", ""},
		{"missing input", `{}`},
		{"blank input", `{"input_data":"   "}`},
		{"unknown field", `{"input_data":"x","extra":1}`},
		{"malformed", `{"input_data":`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(t, a, http.MethodPost, "/v1/agents/generic_agent/execute", "", tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body)
			}
		})
	}
}

func TestExecuteAgentMethodNotAllowed(t *testing.T) {
	a, _ := testAPI(t, "")
	rec := do(t, a, http.MethodGet, "/v1/agents/generic_agent/execute", "", "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	if got := rec.Header().Get("Allow"); got != http.MethodPost {
		t.Fatalf("Allow = %q, want POST", got)
	}
}

func TestExecuteAgentUnknownRoute(t *testing.T) {
	a, _ := testAPI(t, "")
	for _, path := range []string{
		"/v1/agents/generic_agent",
		"/v1/agents/generic_agent/run",
		"/v1/agents/a/b/execute",
	} {
		rec := do(t, a, http.MethodPost, path, "", `{"input_data":"x"}`)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404", path, rec.Code)
		}
	}
}

func TestSessionUnknown(t *testing.T) {
	a, _ := testAPI(t, "")
	rec := do(t, a, http.MethodGet, "/v1/sessions/nope", "", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	rec = do(t, a, http.MethodGet, "/v1/sessions/nope/steps", "", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("steps status = %d, want 404", rec.Code)
	}
}

func TestSessionSteps(t *testing.T) {
	a, _ := testAPI(t, "", llm.Response{Content: "ok", TotalTokens: 3})
	rec := do(t, a, http.MethodPost, "/v1/agents/generic_agent/execute", "", `{"input_data":"hi"}`)
	var sub submitResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &sub)
	waitSession(t, a, sub.SessionID)

	rec = do(t, a, http.MethodGet, "/v1/sessions/"+sub.SessionID+"/steps", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body)
	}
	var got stepsEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Steps) != 1 || got.Steps[0].StepID != "generic_agent" || got.Steps[0].Output != "ok" {
		t.Fatalf("steps = %+v", got)
	}
}

func TestSessionMethodNotAllowed(t *testing.T) {
	a, _ := testAPI(t, "")
	rec := do(t, a, http.MethodPost, "/v1/sessions/x", "", "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	if got := rec.Header().Get("Allow"); got != http.MethodGet {
		t.Fatalf("Allow = %q, want GET", got)
	}
}

func TestAuthRequired(t *testing.T) {
	a, _ := testAPI(t, "s3cret", llm.Response{Content: "ok"})

	rec := do(t, a, http.MethodPost, "/v1/agents/generic_agent/execute", "", `{"input_data":"x"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: status = %d, want 401", rec.Code)
	}
	rec = do(t, a, http.MethodPost, "/v1/agents/generic_agent/execute", "wrong", `{"input_data":"x"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token: status = %d, want 401", rec.Code)
	}
	rec = do(t, a, http.MethodPost, "/v1/agents/generic_agent/execute", "s3cret", `{"input_data":"x"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("valid token: status = %d, want 202 (body %s)", rec.Code, rec.Body)
	}
}

func TestHealthEndpointsUnauthenticated(t *testing.T) {
	a, _ := testAPI(t, "s3cret")
	for _, path := range []string{"/healthz", "/readyz"} {
		rec := do(t, a, http.MethodGet, path, "", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200", path, rec.Code)
		}
	}
}

func TestHealthzVersion(t *testing.T) {
	a, _ := testAPI(t, "")
	rec := do(t, a, http.MethodGet, "/healthz", "", "")
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ok" || body["version"] != "test" {
		t.Fatalf("unexpected healthz body: %v", body)
	}
}

func TestNewRequiresConfig(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("expected error for missing engine")
	}
}

func TestRecovererHandlesPanic(t *testing.T) {
	a, _ := testAPI(t, "")
	boom := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") })
	rec := httptest.NewRecorder()
	a.recoverer(boom).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}
