// Package registry loads, validates, and atomically writes the file-based agent
// registry: blueprints, prompts, and pipelines. The registry is the source of
// truth for agent behavior and is expected to live in git.
package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/bigknoxy/j-harness/internal/model"
	"github.com/bigknoxy/j-harness/internal/pipeline"
)

// Sub-directory names within a registry root.
const (
	BlueprintsDir = "blueprints"
	PromptsDir    = "prompts"
	PipelinesDir  = "pipelines"
)

var idPattern = regexp.MustCompile(`^[a-z0-9_-]+$`)

// Registry is an immutable snapshot of the agent registry loaded from disk.
// Load it once and replace the whole value on reload; it is safe for concurrent
// reads.
type Registry struct {
	root       string
	blueprints map[string]model.AgentBlueprint
	prompts    map[string]string
	pipelines  map[string]model.Pipeline
}

// Root returns the registry root directory.
func (r *Registry) Root() string { return r.root }

// Blueprint returns the blueprint with the given id.
func (r *Registry) Blueprint(id string) (model.AgentBlueprint, bool) {
	b, ok := r.blueprints[id]
	return b, ok
}

// Prompt returns the prompt text for the given blueprint id.
func (r *Registry) Prompt(agentID string) (string, bool) {
	p, ok := r.prompts[agentID]
	return p, ok
}

// Pipeline returns the pipeline with the given id.
func (r *Registry) Pipeline(id string) (model.Pipeline, bool) {
	p, ok := r.pipelines[id]
	return p, ok
}

// BlueprintIDs returns all blueprint ids, sorted.
func (r *Registry) BlueprintIDs() []string { return sortedKeys(r.blueprints) }

// PipelineIDs returns all pipeline ids, sorted.
func (r *Registry) PipelineIDs() []string { return sortedKeys(r.pipelines) }

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Load reads and validates the entire registry rooted at root.
func Load(root string) (*Registry, error) {
	r := &Registry{
		root:       root,
		blueprints: map[string]model.AgentBlueprint{},
		prompts:    map[string]string{},
		pipelines:  map[string]model.Pipeline{},
	}
	if err := r.loadBlueprints(); err != nil {
		return nil, err
	}
	if err := r.loadPipelines(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Registry) loadBlueprints() error {
	dir := filepath.Join(r.root, BlueprintsDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read blueprints dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".json")
		path := filepath.Join(dir, e.Name())
		var bp model.AgentBlueprint
		if err := decodeStrict(path, &bp); err != nil {
			return err
		}
		if err := r.validateBlueprint(name, &bp); err != nil {
			return fmt.Errorf("blueprint %s: %w", e.Name(), err)
		}
		rel, err := safeRel(bp.PromptPath)
		if err != nil {
			return fmt.Errorf("blueprint %s: prompt_path: %w", e.Name(), err)
		}
		promptBytes, err := os.ReadFile(filepath.Join(r.root, rel))
		if err != nil {
			return fmt.Errorf("blueprint %s: read prompt: %w", e.Name(), err)
		}
		r.prompts[bp.ID] = string(promptBytes)
		r.blueprints[bp.ID] = bp
	}
	return nil
}

func (r *Registry) loadPipelines() error {
	dir := filepath.Join(r.root, PipelinesDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read pipelines dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".json")
		path := filepath.Join(dir, e.Name())
		var p model.Pipeline
		if err := decodeStrict(path, &p); err != nil {
			return err
		}
		if err := r.validatePipeline(name, &p); err != nil {
			return fmt.Errorf("pipeline %s: %w", e.Name(), err)
		}
		r.pipelines[p.PipelineID] = p
	}
	return nil
}

func (r *Registry) validateBlueprint(filenameID string, bp *model.AgentBlueprint) error {
	if bp.Version != model.SchemaVersion {
		return fmt.Errorf("unsupported version %d (want %d)", bp.Version, model.SchemaVersion)
	}
	if !idPattern.MatchString(bp.ID) {
		return fmt.Errorf("invalid id %q", bp.ID)
	}
	if filenameID != bp.ID {
		return fmt.Errorf("id %q does not match filename %q", bp.ID, filenameID)
	}
	if strings.TrimSpace(bp.PromptPath) == "" {
		return errors.New("prompt_path is required")
	}
	if _, err := safeRel(bp.PromptPath); err != nil {
		return fmt.Errorf("prompt_path: %w", err)
	}
	if strings.TrimSpace(bp.Model) == "" {
		return errors.New("model is required")
	}
	switch bp.OutputFormat {
	case "", model.OutputText, model.OutputJSON:
	default:
		return fmt.Errorf("invalid output_format %q", bp.OutputFormat)
	}
	return nil
}

func (r *Registry) validatePipeline(filenameID string, p *model.Pipeline) error {
	if p.Version != model.SchemaVersion {
		return fmt.Errorf("unsupported version %d (want %d)", p.Version, model.SchemaVersion)
	}
	if !idPattern.MatchString(p.PipelineID) {
		return fmt.Errorf("invalid pipeline_id %q", p.PipelineID)
	}
	if filenameID != p.PipelineID {
		return fmt.Errorf("pipeline_id %q does not match filename %q", p.PipelineID, filenameID)
	}
	if len(p.Steps) == 0 {
		return errors.New("pipeline has no steps")
	}

	inputs := map[string]bool{}
	for _, in := range p.Inputs {
		if !idPattern.MatchString(in) {
			return fmt.Errorf("invalid input name %q", in)
		}
		inputs[in] = true
	}

	allIDs := map[string]bool{}
	for _, st := range p.Steps {
		if !idPattern.MatchString(st.ID) {
			return fmt.Errorf("step %q: invalid id", st.ID)
		}
		if allIDs[st.ID] {
			return fmt.Errorf("duplicate step id %q", st.ID)
		}
		allIDs[st.ID] = true
	}

	seen := map[string]bool{} // steps whose output is available to later steps
	for _, st := range p.Steps {
		refs, err := pipeline.ParseRefs(st.Input)
		if err != nil {
			return fmt.Errorf("step %q input: %w", st.ID, err)
		}
		for _, ref := range refs {
			if err := checkRef(ref, inputs, seen, st.ID); err != nil {
				return fmt.Errorf("step %q input: %w", st.ID, err)
			}
		}

		if st.Router {
			if len(st.Routes) == 0 {
				return fmt.Errorf("router step %q has no routes", st.ID)
			}
			defaults := 0
			for _, rt := range st.Routes {
				if rt.Goto == "" {
					return fmt.Errorf("router step %q: route missing goto", st.ID)
				}
				if !allIDs[rt.Goto] {
					return fmt.Errorf("router step %q: goto %q is not a step in this pipeline", st.ID, rt.Goto)
				}
				if rt.Default {
					defaults++
					continue
				}
				if rt.When == nil || rt.When.Operator() == "" {
					return fmt.Errorf("router step %q: route must set exactly one operator or default", st.ID)
				}
			}
			if defaults > 1 {
				return fmt.Errorf("router step %q has multiple default routes", st.ID)
			}
			seen[st.ID] = true
			continue
		}

		// Ordinary agent step.
		if st.AgentID == "" {
			return fmt.Errorf("step %q has neither agent_id nor router", st.ID)
		}
		if _, ok := r.blueprints[st.AgentID]; !ok {
			return fmt.Errorf("step %q references unknown agent %q", st.ID, st.AgentID)
		}
		if st.Output == "" {
			return fmt.Errorf("step %q must declare an output name", st.ID)
		}
		if !idPattern.MatchString(st.Output) {
			return fmt.Errorf("step %q: invalid output name %q", st.ID, st.Output)
		}
		seen[st.ID] = true
	}

	if p.Output != "" {
		refs, err := pipeline.ParseRefs(p.Output)
		if err != nil {
			return fmt.Errorf("pipeline output: %w", err)
		}
		for _, ref := range refs {
			if err := checkRef(ref, inputs, allIDs, ""); err != nil {
				return fmt.Errorf("pipeline output: %w", err)
			}
		}
	}
	return nil
}

func checkRef(ref pipeline.Ref, inputs, stepIDs map[string]bool, stepID string) error {
	switch ref.Kind {
	case pipeline.RefInput:
		if !inputs[ref.Name] {
			return fmt.Errorf("unknown input %q", ref.Name)
		}
	case pipeline.RefStepOutput:
		if !stepIDs[ref.StepID] {
			if stepID != "" {
				return fmt.Errorf("references step %q output before it is produced", ref.StepID)
			}
			return fmt.Errorf("references unknown step %q", ref.StepID)
		}
	}
	return nil
}

// decodeStrict reads JSON from path, rejecting unknown fields and trailing data.
func decodeStrict(path string, v any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	var extra any
	if err := dec.Decode(&extra); err == nil {
		return fmt.Errorf("%s: unexpected trailing data", path)
	}
	return nil
}

// safeRel rejects absolute paths and any path that escapes the registry root.
func safeRel(p string) (string, error) {
	if filepath.IsAbs(p) {
		return "", fmt.Errorf("absolute paths are not allowed")
	}
	clean := filepath.Clean(p)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes registry root")
	}
	return clean, nil
}
