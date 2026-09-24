package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/bigknoxy/j-harness/internal/model"
	"github.com/bigknoxy/j-harness/internal/registry"
)

// handleRegistry routes everything under /v1/registry/.
//
//	GET  /v1/registry/agents           list agents
//	GET  /v1/registry/agents/{id}      read one agent (blueprint + prompt)
//	POST /v1/registry/agents/{id}      create an agent
//	PUT  /v1/registry/agents/{id}      update an agent
//	GET  /v1/registry/pipelines        list pipelines
//	GET  /v1/registry/pipelines/{id}   read one pipeline
//	POST /v1/registry/pipelines/{id}   create a pipeline
//	PUT  /v1/registry/pipelines/{id}   update a pipeline
func (a *API) handleRegistry(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/v1/registry/")
	kind, id, ok := splitTwo(rest)
	if !ok {
		// Collection route: "{kind}".
		a.handleRegistryCollection(w, r, rest)
		return
	}
	if id == "" {
		writeError(w, http.StatusNotFound, "not_found", "unknown route")
		return
	}
	switch kind {
	case "agents":
		a.handleRegistryAgent(w, r, id)
	case "pipelines":
		a.handleRegistryPipeline(w, r, id)
	default:
		writeError(w, http.StatusNotFound, "not_found", "unknown route")
	}
}

// handleRegistryCollection handles GET /v1/registry/{agents,pipelines}.
func (a *API) handleRegistryCollection(w http.ResponseWriter, r *http.Request, kind string) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	reg := a.currentRegistry()
	switch kind {
	case "agents":
		ids := reg.BlueprintIDs()
		out := make([]agentSummary, 0, len(ids))
		for _, id := range ids {
			bp, _ := reg.Blueprint(id)
			out = append(out, agentSummary{
				ID:           bp.ID,
				Description:  bp.Description,
				Model:        bp.Model,
				OutputFormat: string(bp.OutputFormat),
			})
		}
		writeJSON(w, http.StatusOK, agentsEnvelope{Agents: out})
	case "pipelines":
		ids := reg.PipelineIDs()
		out := make([]pipelineSummary, 0, len(ids))
		for _, id := range ids {
			p, _ := reg.Pipeline(id)
			out = append(out, pipelineSummary{PipelineID: p.PipelineID, Steps: len(p.Steps), Inputs: p.Inputs})
		}
		writeJSON(w, http.StatusOK, pipelinesEnvelope{Pipelines: out})
	default:
		writeError(w, http.StatusNotFound, "not_found", "unknown route")
	}
}

func (a *API) handleRegistryAgent(w http.ResponseWriter, r *http.Request, id string) {
	reg := a.currentRegistry()
	switch r.Method {
	case http.MethodGet:
		bp, ok := reg.Blueprint(id)
		if !ok {
			writeError(w, http.StatusNotFound, "not_found", "unknown agent "+id)
			return
		}
		prompt, _ := reg.Prompt(id)
		writeJSON(w, http.StatusOK, agentDetail{Blueprint: bp, Prompt: prompt})
	case http.MethodPost, http.MethodPut:
		_, exists := reg.Blueprint(id)
		if r.Method == http.MethodPost && exists {
			writeError(w, http.StatusConflict, "conflict", "agent "+id+" already exists")
			return
		}
		if r.Method == http.MethodPut && !exists {
			writeError(w, http.StatusNotFound, "not_found", "unknown agent "+id)
			return
		}
		req, err := decodeAgentWrite(w, r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		if req.Blueprint.ID != id {
			writeError(w, http.StatusBadRequest, "bad_request", "blueprint id must match path id "+id)
			return
		}
		a.mutate(w, func(reg *registry.Registry) error {
			return reg.WriteBlueprint(req.Blueprint, req.Prompt)
		})
	default:
		methodNotAllowed(w, "GET, POST, PUT")
	}
}

func (a *API) handleRegistryPipeline(w http.ResponseWriter, r *http.Request, id string) {
	reg := a.currentRegistry()
	switch r.Method {
	case http.MethodGet:
		p, ok := reg.Pipeline(id)
		if !ok {
			writeError(w, http.StatusNotFound, "not_found", "unknown pipeline "+id)
			return
		}
		writeJSON(w, http.StatusOK, pipelineDetail{Pipeline: p})
	case http.MethodPost, http.MethodPut:
		_, exists := reg.Pipeline(id)
		if r.Method == http.MethodPost && exists {
			writeError(w, http.StatusConflict, "conflict", "pipeline "+id+" already exists")
			return
		}
		if r.Method == http.MethodPut && !exists {
			writeError(w, http.StatusNotFound, "not_found", "unknown pipeline "+id)
			return
		}
		req, err := decodePipelineWrite(w, r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		if req.Pipeline.PipelineID != id {
			writeError(w, http.StatusBadRequest, "bad_request", "pipeline_id must match path id "+id)
			return
		}
		a.mutate(w, func(reg *registry.Registry) error {
			return reg.WritePipeline(req.Pipeline)
		})
	default:
		methodNotAllowed(w, "GET, POST, PUT")
	}
}

// --- request/response types ---

type agentWriteRequest struct {
	Blueprint model.AgentBlueprint `json:"blueprint"`
	Prompt    string               `json:"prompt"`
}

type pipelineWriteRequest struct {
	Pipeline model.Pipeline `json:"pipeline"`
}

type agentSummary struct {
	ID           string `json:"id"`
	Description  string `json:"description,omitempty"`
	Model        string `json:"model"`
	OutputFormat string `json:"output_format,omitempty"`
}

type agentDetail struct {
	Blueprint model.AgentBlueprint `json:"blueprint"`
	Prompt    string               `json:"prompt"`
}

type agentsEnvelope struct {
	Agents []agentSummary `json:"agents"`
}

type pipelineSummary struct {
	PipelineID string   `json:"pipeline_id"`
	Steps      int      `json:"steps"`
	Inputs     []string `json:"inputs,omitempty"`
}

type pipelineDetail struct {
	Pipeline model.Pipeline `json:"pipeline"`
}

type pipelinesEnvelope struct {
	Pipelines []pipelineSummary `json:"pipelines"`
}

func decodeAgentWrite(w http.ResponseWriter, r *http.Request) (agentWriteRequest, error) {
	var req agentWriteRequest
	if err := decodeStrictBody(w, r, &req); err != nil {
		return agentWriteRequest{}, err
	}
	return req, nil
}

func decodePipelineWrite(w http.ResponseWriter, r *http.Request) (pipelineWriteRequest, error) {
	var req pipelineWriteRequest
	if err := decodeStrictBody(w, r, &req); err != nil {
		return pipelineWriteRequest{}, err
	}
	return req, nil
}

// decodeStrictBody decodes a single JSON object, rejecting unknown fields and
// trailing data.
func decodeStrictBody(w http.ResponseWriter, r *http.Request, v any) error {
	body := http.MaxBytesReader(w, r.Body, maxBodyBytes)
	defer body.Close()

	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("empty request body")
		}
		return errors.New("invalid JSON body: " + err.Error())
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain a single JSON object")
	}
	return nil
}
