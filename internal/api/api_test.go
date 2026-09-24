package api

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bigknoxy/j-harness/internal/engine"
	"github.com/bigknoxy/j-harness/internal/llm"
	"github.com/bigknoxy/j-harness/internal/registry"
)

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
	a, err := New(Config{
		Engine:    eng,
		Registry:  reg,
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

func TestExecuteAgentSuccess(t *testing.T) {
	a, fake := testAPI(t, "", llm.Response{Content: "hello there", TotalTokens: 5})

	rec := do(t, a, http.MethodPost, "/v1/agents/generic_agent/execute", "", `{"input_data":"hi"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body)
	}
	var got executeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.AgentID != "generic_agent" || got.Output != "hello there" || got.Tokens != 5 {
		t.Fatalf("unexpected response: %+v", got)
	}
	if fake.CallCount() != 1 {
		t.Fatalf("client calls = %d, want 1", fake.CallCount())
	}
	if fake.Requests[0].UserInput != "hi" {
		t.Fatalf("user input = %q, want hi", fake.Requests[0].UserInput)
	}
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

func TestExecuteAgentClientError(t *testing.T) {
	a, _ := testAPI(t, "")
	reg, _ := registry.Load("../../agent-registry")
	fake := llm.NewFakeErr(io.ErrUnexpectedEOF)
	eng, _ := engine.New(reg, fake)
	a.engine = eng

	rec := do(t, a, http.MethodPost, "/v1/agents/generic_agent/execute", "", `{"input_data":"x"}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if e := decodeErr(t, rec); e.Error.Code != "execution_failed" {
		t.Fatalf("error code = %q", e.Error.Code)
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
	if rec.Code != http.StatusOK {
		t.Fatalf("valid token: status = %d, want 200 (body %s)", rec.Code, rec.Body)
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

func TestNewRequiresEngineAndRegistry(t *testing.T) {
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
