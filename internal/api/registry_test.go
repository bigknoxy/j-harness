package api

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/bigknoxy/j-harness/internal/engine"
	"github.com/bigknoxy/j-harness/internal/llm"
	"github.com/bigknoxy/j-harness/internal/registry"
	"github.com/bigknoxy/j-harness/internal/store"
)

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

// testAPIWritable returns an API backed by a throwaway copy of the repo
// registry so write tests never touch the checked-in files.
func testAPIWritable(t *testing.T) (*API, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "registry")
	copyDir(t, "../../agent-registry", root)

	reg, err := registry.Load(root)
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	fake := llm.NewFake(llm.Response{Content: "ok"})
	eng, err := engine.New(reg, fake)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "reg.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	pool, err := engine.NewPool(engine.PoolConfig{Store: st, Engine: eng, Workers: 1, Logger: log.New(io.Discard, "", 0)})
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	t.Cleanup(pool.Close)

	a, err := New(Config{
		Engine: eng, Registry: reg, Pool: pool, Store: st,
		Version: "test", Logger: log.New(io.Discard, "", 0),
	})
	if err != nil {
		t.Fatalf("new api: %v", err)
	}
	return a, root
}

func TestRegistryListAgents(t *testing.T) {
	a, _ := testAPIWritable(t)
	rec := do(t, a, http.MethodGet, "/v1/registry/agents", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", rec.Code, rec.Body)
	}
	var env agentsEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(env.Agents) == 0 {
		t.Fatal("expected agents")
	}
	seen := map[string]bool{}
	for _, ag := range env.Agents {
		seen[ag.ID] = true
	}
	if !seen["generic_agent"] || !seen["triage"] {
		t.Fatalf("missing expected agents: %+v", env.Agents)
	}
}

func TestRegistryGetAgent(t *testing.T) {
	a, _ := testAPIWritable(t)
	rec := do(t, a, http.MethodGet, "/v1/registry/agents/generic_agent", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", rec.Code, rec.Body)
	}
	var d agentDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if d.Blueprint.ID != "generic_agent" || d.Prompt == "" {
		t.Fatalf("detail = %+v", d)
	}

	rec = do(t, a, http.MethodGet, "/v1/registry/agents/nope", "", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestRegistryCreateAndUpdateAgent(t *testing.T) {
	a, root := testAPIWritable(t)
	body := `{"blueprint":{"id":"scribe","prompt_path":"prompts/scribe.md","model":"m","output_format":"text","version":1},"prompt":"You summarize."}`

	rec := do(t, a, http.MethodPost, "/v1/registry/agents/scribe", "", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d body %s", rec.Code, rec.Body)
	}
	if _, err := os.Stat(filepath.Join(root, "blueprints", "scribe.json")); err != nil {
		t.Fatalf("blueprint not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "prompts", "scribe.md")); err != nil {
		t.Fatalf("prompt not written: %v", err)
	}

	// The new agent is visible immediately (live reload).
	rec = do(t, a, http.MethodGet, "/v1/registry/agents/scribe", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get after create = %d", rec.Code)
	}

	// Duplicate create is a conflict.
	rec = do(t, a, http.MethodPost, "/v1/registry/agents/scribe", "", body)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate create = %d, want 409", rec.Code)
	}

	// Update via PUT.
	upd := `{"blueprint":{"id":"scribe","prompt_path":"prompts/scribe.md","model":"m2","output_format":"text","version":1},"prompt":"Updated."}`
	rec = do(t, a, http.MethodPut, "/v1/registry/agents/scribe", "", upd)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d body %s", rec.Code, rec.Body)
	}
	rec = do(t, a, http.MethodGet, "/v1/registry/agents/scribe", "", "")
	var d agentDetail
	_ = json.Unmarshal(rec.Body.Bytes(), &d)
	if d.Blueprint.Model != "m2" || d.Prompt != "Updated." {
		t.Fatalf("after update: %+v", d)
	}

	// PUT on a missing agent is 404.
	rec = do(t, a, http.MethodPut, "/v1/registry/agents/ghost", "", `{"blueprint":{"id":"ghost","prompt_path":"prompts/ghost.md","model":"m","version":1},"prompt":"x"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("put missing = %d, want 404", rec.Code)
	}
}

func TestRegistryCreateAgentValidation(t *testing.T) {
	a, _ := testAPIWritable(t)

	// id mismatch between path and body.
	rec := do(t, a, http.MethodPost, "/v1/registry/agents/foo", "",
		`{"blueprint":{"id":"bar","prompt_path":"prompts/foo.md","model":"m","version":1},"prompt":"x"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("id mismatch = %d, want 400", rec.Code)
	}

	// invalid blueprint (missing model) must not be written.
	rec = do(t, a, http.MethodPost, "/v1/registry/agents/bad", "",
		`{"blueprint":{"id":"bad","prompt_path":"prompts/bad.md","version":1},"prompt":"x"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid blueprint = %d, want 400", rec.Code)
	}

	// unknown field rejected.
	rec = do(t, a, http.MethodPost, "/v1/registry/agents/weird", "",
		`{"blueprint":{"id":"weird","prompt_path":"prompts/weird.md","model":"m","version":1,"bogus":true},"prompt":"x"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown field = %d, want 400", rec.Code)
	}
}

func TestRegistryPipelineCRUD(t *testing.T) {
	a, _ := testAPIWritable(t)

	rec := do(t, a, http.MethodGet, "/v1/registry/pipelines", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d", rec.Code)
	}
	var env pipelinesEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(env.Pipelines) == 0 {
		t.Fatal("expected pipelines")
	}

	body := `{"pipeline":{"pipeline_id":"greet","version":1,"inputs":["user_input"],"steps":[{"id":"say","agent_id":"generic_agent","input":"{{ inputs.user_input }}","output":"reply"}],"output":"{{ steps.say.output }}"}}`
	rec = do(t, a, http.MethodPost, "/v1/registry/pipelines/greet", "", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("create pipeline = %d body %s", rec.Code, rec.Body)
	}

	rec = do(t, a, http.MethodGet, "/v1/registry/pipelines/greet", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get pipeline = %d", rec.Code)
	}
	var d pipelineDetail
	_ = json.Unmarshal(rec.Body.Bytes(), &d)
	if d.Pipeline.PipelineID != "greet" || len(d.Pipeline.Steps) != 1 {
		t.Fatalf("pipeline detail = %+v", d.Pipeline)
	}

	// Invalid pipeline (unknown agent) rejected.
	bad := `{"pipeline":{"pipeline_id":"bad","version":1,"steps":[{"id":"s","agent_id":"nope","input":"x","output":"o"}]}}`
	rec = do(t, a, http.MethodPost, "/v1/registry/pipelines/bad", "", bad)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid pipeline = %d, want 400", rec.Code)
	}
}

func TestRegistryMethodNotAllowed(t *testing.T) {
	a, _ := testAPIWritable(t)
	rec := do(t, a, http.MethodDelete, "/v1/registry/agents/generic_agent", "", "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	if rec.Header().Get("Allow") == "" {
		t.Fatal("expected Allow header")
	}
}

func TestRegistryAuthRequired(t *testing.T) {
	root := filepath.Join(t.TempDir(), "registry")
	copyDir(t, "../../agent-registry", root)
	reg, err := registry.Load(root)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	eng, _ := engine.New(reg, llm.NewFake())
	st, err := store.Open(filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	pool, _ := engine.NewPool(engine.PoolConfig{Store: st, Engine: eng, Workers: 1, Logger: log.New(io.Discard, "", 0)})
	t.Cleanup(pool.Close)
	a, err := New(Config{Engine: eng, Registry: reg, Pool: pool, Store: st, Version: "t", AuthToken: "secret", Logger: log.New(io.Discard, "", 0)})
	if err != nil {
		t.Fatalf("new api: %v", err)
	}

	rec := do(t, a, http.MethodGet, "/v1/registry/agents", "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token = %d, want 401", rec.Code)
	}
	rec = do(t, a, http.MethodGet, "/v1/registry/agents", "secret", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("with token = %d, want 200", rec.Code)
	}
}
