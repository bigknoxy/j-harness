// Package api exposes the harness over HTTP. It is deliberately thin: handlers
// validate the request, enqueue (or look up) work, and render a JSON envelope.
// All execution logic lives in internal/engine so the same code path is driven
// by the async worker pool.
package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/bigknoxy/j-harness/internal/engine"
	"github.com/bigknoxy/j-harness/internal/model"
	"github.com/bigknoxy/j-harness/internal/registry"
	"github.com/bigknoxy/j-harness/internal/store"
)

// maxBodyBytes bounds request bodies to protect the 2-core host from abuse.
const maxBodyBytes = 1 << 20 // 1 MiB

// Config configures the HTTP API.
type Config struct {
	Engine   *engine.Engine
	Registry *registry.Registry
	// Pool executes submitted jobs asynchronously. Required.
	Pool *engine.Pool
	// Store is used to read job/step state (and readiness). Required.
	Store   store.Store
	Version string
	// AuthToken, when non-empty, requires Authorization: Bearer <token> on
	// every /v1/* request. /healthz and /readyz stay unauthenticated.
	AuthToken string
	// Logger receives request/error logs. Defaults to the std logger.
	Logger *log.Logger
}

// API is the HTTP handler for the harness.
type API struct {
	engine   *engine.Engine
	registry *registry.Registry
	pool     *engine.Pool
	store    store.Store
	version  string
	auth     string
	logger   *log.Logger

	// mu guards swap of the registry snapshot on registry writes.
	mu sync.Mutex
}

// New validates the config and returns an API.
func New(cfg Config) (*API, error) {
	if cfg.Engine == nil {
		return nil, errors.New("api: engine is required")
	}
	if cfg.Registry == nil {
		return nil, errors.New("api: registry is required")
	}
	if cfg.Pool == nil {
		return nil, errors.New("api: pool is required")
	}
	if cfg.Store == nil {
		return nil, errors.New("api: store is required")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = log.Default()
	}
	return &API{
		engine:   cfg.Engine,
		registry: cfg.Registry,
		pool:     cfg.Pool,
		store:    cfg.Store,
		version:  cfg.Version,
		auth:     cfg.AuthToken,
		logger:   logger,
	}, nil
}

// Handler returns the fully-wired HTTP handler including middleware.
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()

	// Health/readiness are always unauthenticated so probes keep working.
	mux.HandleFunc("/healthz", a.handleHealthz)
	mux.HandleFunc("/readyz", a.handleReadyz)

	mux.HandleFunc("/v1/agents/", a.handleAgents)
	mux.HandleFunc("/v1/pipelines/", a.handlePipelines)
	mux.HandleFunc("/v1/sessions/", a.handleSessions)
	mux.HandleFunc("/v1/registry/", a.handleRegistry)

	return a.recoverer(a.logRequests(a.authenticate(mux)))
}

// currentRegistry returns the registry snapshot in use. Writes swap it under
// mu, so reads take mu to get a consistent pointer.
func (a *API) currentRegistry() *registry.Registry {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.registry
}

// mutate applies write under the write lock, then reloads the whole registry
// from disk and swaps it into both the API and the engine. Reloading (rather
// than patching maps) keeps the snapshot identical to a fresh process start and
// lets a single validation pass reject cross-file breakage.
func (a *API) mutate(w http.ResponseWriter, write func(*registry.Registry) error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := write(a.registry); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_registry_entry", err.Error())
		return
	}
	reloaded, err := registry.Load(a.registry.Root())
	if err != nil {
		a.logger.Printf("reload registry after write: %v", err)
		writeError(w, http.StatusInternalServerError, "reload_failed", err.Error())
		return
	}
	a.registry = reloaded
	a.engine.SetRegistry(reloaded)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *API) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.auth == "" || !strings.HasPrefix(r.URL.Path, "/v1/") {
			next.ServeHTTP(w, r)
			return
		}
		token := bearerToken(r.Header.Get("Authorization"))
		if token == "" || token != a.auth {
			writeError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid bearer token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *API) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		a.logger.Printf("%s %s %d %s", r.Method, r.URL.Path, sw.status, time.Since(start).Round(time.Millisecond))
	})
}

func (a *API) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				a.logger.Printf("panic serving %s %s: %v", r.Method, r.URL.Path, rec)
				writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// --- handlers ---

func (a *API) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": a.version})
}

func (a *API) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if a.currentRegistry() == nil {
		writeError(w, http.StatusServiceUnavailable, "not_ready", "registry not loaded")
		return
	}
	if err := a.store.Ping(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "not_ready", "store unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// handleAgents routes POST /v1/agents/{id}/execute.
func (a *API) handleAgents(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/v1/agents/")
	id, action, ok := splitTwo(rest)
	if !ok || action != "execute" || id == "" {
		writeError(w, http.StatusNotFound, "not_found", "unknown route")
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	a.handleExecuteAgent(w, r, id)
}

// handleExecuteAgent validates the target, persists a PENDING job, enqueues it,
// and returns 202 with the session id.
func (a *API) handleExecuteAgent(w http.ResponseWriter, r *http.Request, agentID string) {
	if _, ok := a.currentRegistry().Blueprint(agentID); !ok {
		writeError(w, http.StatusNotFound, "not_found", "unknown agent "+agentID)
		return
	}

	req, err := decodeExecuteRequest(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}

	job := model.Job{
		SessionID: newSessionID(),
		Kind:      model.KindAgent,
		TargetID:  agentID,
		Status:    model.StatusPending,
		Input:     req.InputData,
	}
	if err := a.pool.Submit(r.Context(), job); err != nil {
		if errors.Is(err, engine.ErrQueueFull) {
			writeError(w, http.StatusServiceUnavailable, "queue_full", "job queue is full, retry later")
			return
		}
		a.logger.Printf("submit agent job %s: %v", agentID, err)
		writeError(w, http.StatusInternalServerError, "submit_failed", err.Error())
		return
	}

	writeJSON(w, http.StatusAccepted, submitResponse{SessionID: job.SessionID, Status: string(model.StatusPending)})
}

// handlePipelines routes POST /v1/pipelines/{id}/execute.
func (a *API) handlePipelines(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/v1/pipelines/")
	id, action, ok := splitTwo(rest)
	if !ok || action != "execute" || id == "" {
		writeError(w, http.StatusNotFound, "not_found", "unknown route")
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	a.handleExecutePipeline(w, r, id)
}

// handleExecutePipeline validates the target, persists a PENDING job whose
// input is a JSON object of named pipeline inputs, and enqueues it.
func (a *API) handleExecutePipeline(w http.ResponseWriter, r *http.Request, pipelineID string) {
	p, ok := a.currentRegistry().Pipeline(pipelineID)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "unknown pipeline "+pipelineID)
		return
	}

	inputs, err := decodePipelineRequest(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	for _, name := range p.Inputs {
		if strings.TrimSpace(inputs[name]) == "" {
			writeError(w, http.StatusBadRequest, "bad_request", "missing required input "+name)
			return
		}
	}
	encoded, err := json.Marshal(inputs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	job := model.Job{
		SessionID: newSessionID(),
		Kind:      model.KindPipeline,
		TargetID:  pipelineID,
		Status:    model.StatusPending,
		Input:     string(encoded),
	}
	if err := a.pool.Submit(r.Context(), job); err != nil {
		if errors.Is(err, engine.ErrQueueFull) {
			writeError(w, http.StatusServiceUnavailable, "queue_full", "job queue is full, retry later")
			return
		}
		a.logger.Printf("submit pipeline job %s: %v", pipelineID, err)
		writeError(w, http.StatusInternalServerError, "submit_failed", err.Error())
		return
	}

	writeJSON(w, http.StatusAccepted, submitResponse{SessionID: job.SessionID, Status: string(model.StatusPending)})
}

// handleSessions routes GET /v1/sessions/{id} and GET /v1/sessions/{id}/steps.
func (a *API) handleSessions(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/v1/sessions/")
	if rest == "" {
		writeError(w, http.StatusNotFound, "not_found", "unknown route")
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}

	// Either "{id}" or "{id}/steps".
	if id, sub, ok := splitTwo(rest); ok {
		if sub != "steps" {
			writeError(w, http.StatusNotFound, "not_found", "unknown route")
			return
		}
		a.handleSessionSteps(w, r, id)
		return
	}
	a.handleSession(w, r, rest)
}

func (a *API) handleSession(w http.ResponseWriter, r *http.Request, sessionID string) {
	job, err := a.store.GetJob(r.Context(), sessionID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "unknown session "+sessionID)
			return
		}
		a.logger.Printf("get session %s: %v", sessionID, err)
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, sessionResponse{
		SessionID: job.SessionID,
		Kind:      string(job.Kind),
		TargetID:  job.TargetID,
		Status:    string(job.Status),
		Result:    job.Result,
		Error:     job.Error,
	})
}

func (a *API) handleSessionSteps(w http.ResponseWriter, r *http.Request, sessionID string) {
	if _, err := a.store.GetJob(r.Context(), sessionID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "unknown session "+sessionID)
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	steps, err := a.store.ListStepResults(r.Context(), sessionID)
	if err != nil {
		a.logger.Printf("list steps %s: %v", sessionID, err)
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	out := make([]stepResponse, 0, len(steps))
	for _, s := range steps {
		out = append(out, stepResponse{
			StepID:     s.StepID,
			Status:     s.Status,
			Output:     s.Output,
			Error:      s.Error,
			Tokens:     s.Tokens,
			DurationMS: s.DurationMS,
		})
	}
	writeJSON(w, http.StatusOK, stepsEnvelope{SessionID: sessionID, Steps: out})
}

// --- request/response types ---

type executeRequest struct {
	InputData string `json:"input_data"`
}

type submitResponse struct {
	SessionID string `json:"session_id"`
	Status    string `json:"status"`
}

type sessionResponse struct {
	SessionID string `json:"session_id"`
	Kind      string `json:"kind"`
	TargetID  string `json:"target_id"`
	Status    string `json:"status"`
	Result    string `json:"result,omitempty"`
	Error     string `json:"error,omitempty"`
}

type stepResponse struct {
	StepID     string `json:"step_id"`
	Status     string `json:"status"`
	Output     string `json:"output,omitempty"`
	Error      string `json:"error,omitempty"`
	Tokens     int    `json:"tokens,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
}

type stepsEnvelope struct {
	SessionID string         `json:"session_id"`
	Steps     []stepResponse `json:"steps"`
}

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// decodeExecuteRequest strictly decodes an execute body.
func decodeExecuteRequest(w http.ResponseWriter, r *http.Request) (executeRequest, error) {
	var req executeRequest
	body := http.MaxBytesReader(w, r.Body, maxBodyBytes)
	defer body.Close()

	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		if errors.Is(err, io.EOF) {
			return executeRequest{}, errors.New("empty request body")
		}
		return executeRequest{}, errors.New("invalid JSON body: " + err.Error())
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return executeRequest{}, errors.New("request body must contain a single JSON object")
	}
	if strings.TrimSpace(req.InputData) == "" {
		return executeRequest{}, errors.New("field \"input_data\" is required")
	}
	return req, nil
}

// decodePipelineRequest strictly decodes a pipeline execute body: a JSON object
// mapping input names to string values.
func decodePipelineRequest(w http.ResponseWriter, r *http.Request) (map[string]string, error) {
	body := http.MaxBytesReader(w, r.Body, maxBodyBytes)
	defer body.Close()

	var inputs map[string]string
	dec := json.NewDecoder(body)
	if err := dec.Decode(&inputs); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("empty request body")
		}
		return nil, errors.New("invalid JSON body: " + err.Error())
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("request body must contain a single JSON object")
	}
	if len(inputs) == 0 {
		return nil, errors.New("request body must contain at least one input")
	}
	return inputs, nil
}

// --- helpers ---

// newSessionID returns a random, URL-safe session identifier.
func newSessionID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand should never fail; fall back to a time-based id.
		return "sess_" + time.Now().UTC().Format("20060102T150405.000000000")
	}
	return "sess_" + hex.EncodeToString(b[:])
}

// splitTwo splits "a/b" into ("a","b"); more than one slash is not ok.
func splitTwo(s string) (first, second string, ok bool) {
	i := strings.IndexByte(s, '/')
	if i < 0 {
		return s, "", false
	}
	first, second = s[:i], s[i+1:]
	if strings.Contains(second, "/") {
		return "", "", false
	}
	return first, second, true
}

func bearerToken(header string) string {
	const prefix = "Bearer "
	if len(header) < len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(header[len(prefix):])
}

func methodNotAllowed(w http.ResponseWriter, allowed string) {
	w.Header().Set("Allow", allowed)
	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed, use "+allowed)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, errCode, msg string) {
	writeJSON(w, code, errorBody{Error: errorDetail{Code: errCode, Message: msg}})
}

// statusWriter records the status code for request logging.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}
