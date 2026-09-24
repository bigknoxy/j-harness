// Package api exposes the harness over HTTP. It is deliberately thin: handlers
// validate the request, invoke the engine, and render a JSON envelope. All
// execution logic lives in internal/engine so the same code path can later be
// driven by the async worker pool.
package api

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/bigknoxy/j-harness/internal/engine"
	"github.com/bigknoxy/j-harness/internal/registry"
)

// maxBodyBytes bounds request bodies to protect the 2-core host from abuse.
const maxBodyBytes = 1 << 20 // 1 MiB

// Config configures the HTTP API.
type Config struct {
	Engine   *engine.Engine
	Registry *registry.Registry
	Version  string
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
	version  string
	auth     string
	logger   *log.Logger
}

// New validates the config and returns an API.
func New(cfg Config) (*API, error) {
	if cfg.Engine == nil {
		return nil, errors.New("api: engine is required")
	}
	if cfg.Registry == nil {
		return nil, errors.New("api: registry is required")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = log.Default()
	}
	return &API{
		engine:   cfg.Engine,
		registry: cfg.Registry,
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

	return a.recoverer(a.logRequests(a.authenticate(mux)))
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

func (a *API) handleReadyz(w http.ResponseWriter, _ *http.Request) {
	if a.registry == nil {
		writeError(w, http.StatusServiceUnavailable, "not_ready", "registry not loaded")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// handleAgents routes /v1/agents/{id}/execute.
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

func (a *API) handleExecuteAgent(w http.ResponseWriter, r *http.Request, agentID string) {
	if _, ok := a.registry.Blueprint(agentID); !ok {
		writeError(w, http.StatusNotFound, "not_found", "unknown agent "+agentID)
		return
	}

	req, err := decodeExecuteRequest(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}

	res, err := a.engine.RunAgent(r.Context(), agentID, req.InputData)
	if err != nil {
		a.logger.Printf("agent %s failed: %v", agentID, err)
		writeError(w, http.StatusInternalServerError, "execution_failed", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, executeResponse{
		AgentID:    res.AgentID,
		Output:     res.Output,
		Tokens:     res.Tokens,
		DurationMS: res.DurationMS,
	})
}

// --- request/response types ---

type executeRequest struct {
	InputData string `json:"input_data"`
}

type executeResponse struct {
	AgentID    string `json:"agent_id"`
	Output     string `json:"output"`
	Tokens     int    `json:"tokens"`
	DurationMS int64  `json:"duration_ms"`
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

// --- helpers ---

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
