package registry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/bigknoxy/j-harness/internal/model"
)

// WriteBlueprint validates and atomically writes a blueprint and its prompt.
// Existing files are replaced only on success.
func (r *Registry) WriteBlueprint(bp model.AgentBlueprint, prompt string) error {
	if err := r.validateBlueprint(bp.ID, &bp); err != nil {
		return err
	}
	rel, err := safeRel(bp.PromptPath)
	if err != nil {
		return err
	}
	if prompt != "" {
		if err := atomicWrite(filepath.Join(r.root, rel), []byte(prompt)); err != nil {
			return err
		}
	} else if _, err := os.Stat(filepath.Join(r.root, rel)); err != nil {
		return fmt.Errorf("prompt_path %q does not exist and no prompt was provided", bp.PromptPath)
	}
	data, err := json.MarshalIndent(bp, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(r.root, BlueprintsDir, bp.ID+".json"), append(data, '\n'))
}

// WritePipeline validates and atomically writes a pipeline definition.
func (r *Registry) WritePipeline(p model.Pipeline) error {
	if err := r.validatePipeline(p.PipelineID, &p); err != nil {
		return err
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(r.root, PipelinesDir, p.PipelineID+".json"), append(data, '\n'))
}

// atomicWrite writes data to path via a temp file in the same directory followed
// by an atomic rename, so readers never observe a partially written file.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}
