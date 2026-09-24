// Package model defines the core data types shared across j-harness: agent
// blueprints, pipelines, their steps, and runtime job records.
//
// These types are the serialization contract for the JSON files under
// agent-registry/ and for job/step rows in the store. Keep them stable and
// versioned; see docs/SCHEMA.md.
package model

// SchemaVersion is the current blueprint/pipeline schema version.
const SchemaVersion = 1

// OutputFormat enumerates supported agent output formats.
type OutputFormat string

const (
	// OutputText returns the model response as plain text.
	OutputText OutputFormat = "text"
	// OutputJSON requests a JSON object from the model.
	OutputJSON OutputFormat = "json"
)

// AgentBlueprint defines one agent: which prompt to load, which model and
// parameters to use, and optionally which tools it may call.
type AgentBlueprint struct {
	ID             string       `json:"id"`
	Description    string       `json:"description,omitempty"`
	PromptPath     string       `json:"prompt_path"`
	Model          string       `json:"model"`
	BaseURL        string       `json:"base_url,omitempty"`
	Temperature    *float64     `json:"temperature,omitempty"`
	MaxTokens      int          `json:"max_tokens,omitempty"`
	TimeoutSeconds int          `json:"timeout_seconds,omitempty"`
	OutputFormat   OutputFormat `json:"output_format,omitempty"`
	OutputSchema   string       `json:"output_schema,omitempty"`
	Tools          []string     `json:"tools,omitempty"`
	Version        int          `json:"version"`
}

// Router condition operators.
const (
	OpEquals    = "equals"
	OpNotEquals = "not_equals"
	OpIn        = "in"
	OpMatches   = "matches"
	OpExists    = "exists"
)

// Route is one branch of a router step. Exactly one of When or Default must be
// set. When selects on the resolved input; Default is the fallback.
type Route struct {
	When    *Condition `json:"when,omitempty"`
	Default bool       `json:"default,omitempty"`
	Goto    string     `json:"goto"`
}

// Condition is a single comparison used by a Route. Only one operator field is
// populated; Field is a JSON path into the router step's resolved input.
type Condition struct {
	Field     string `json:"field"`
	Equals    any    `json:"equals,omitempty"`
	NotEquals any    `json:"not_equals,omitempty"`
	In        []any  `json:"in,omitempty"`
	Matches   string `json:"matches,omitempty"`
	Exists    bool   `json:"exists,omitempty"`
}

// Operator returns the single operator name set on the condition, or "" when
// zero or multiple operators are set (invalid).
func (c Condition) Operator() string {
	count := 0
	op := ""
	if c.Equals != nil {
		count, op = count+1, OpEquals
	}
	if c.NotEquals != nil {
		count, op = count+1, OpNotEquals
	}
	if len(c.In) > 0 {
		count, op = count+1, OpIn
	}
	if c.Matches != "" {
		count, op = count+1, OpMatches
	}
	if c.Exists {
		count, op = count+1, OpExists
	}
	if count != 1 {
		return ""
	}
	return op
}

// Step is a single node in a pipeline. An ordinary step runs an agent; a router
// step branches based on a condition over its resolved input and does not run
// an agent.
type Step struct {
	ID      string   `json:"id"`
	AgentID string   `json:"agent_id,omitempty"`
	Input   string   `json:"input"`
	Output  string   `json:"output,omitempty"`
	Router  bool     `json:"router,omitempty"`
	Routes  []Route  `json:"routes,omitempty"`
	Needs   []string `json:"needs,omitempty"`
}

// Pipeline is a DAG of steps with declared inputs and a final output template.
type Pipeline struct {
	PipelineID string   `json:"pipeline_id"`
	Version    int      `json:"version"`
	Inputs     []string `json:"inputs,omitempty"`
	Steps      []Step   `json:"steps"`
	Output     string   `json:"output,omitempty"`
}

// JobKind distinguishes what a job executes.
type JobKind string

const (
	// KindAgent runs a single agent.
	KindAgent JobKind = "agent"
	// KindPipeline runs a pipeline.
	KindPipeline JobKind = "pipeline"
)

// JobStatus is the lifecycle state of a job.
type JobStatus string

const (
	// StatusPending is a job queued but not yet running.
	StatusPending JobStatus = "PENDING"
	// StatusRunning is a job currently executing.
	StatusRunning JobStatus = "RUNNING"
	// StatusCompleted is a job that finished successfully.
	StatusCompleted JobStatus = "COMPLETED"
	// StatusFailed is a job that ended with an error.
	StatusFailed JobStatus = "FAILED"
	// StatusCanceled is a job stopped at the caller's request.
	StatusCanceled JobStatus = "CANCELED"
	// StatusSkipped is a pipeline step not executed because a branch was taken
	// or a dependency was skipped.
	StatusSkipped JobStatus = "SKIPPED"
)

// Job is a unit of work submitted through the API and tracked in the store.
type Job struct {
	SessionID string    `json:"session_id"`
	Kind      JobKind   `json:"kind"`
	TargetID  string    `json:"target_id"`
	Status    JobStatus `json:"status"`
	Input     string    `json:"input,omitempty"`
	Result    string    `json:"result,omitempty"`
	Error     string    `json:"error,omitempty"`
}

// StepResult records the outcome and cost of a single executed step.
type StepResult struct {
	SessionID  string `json:"session_id"`
	StepID     string `json:"step_id"`
	Status     string `json:"status"`
	Output     string `json:"output,omitempty"`
	Error      string `json:"error,omitempty"`
	Tokens     int    `json:"tokens,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
}
