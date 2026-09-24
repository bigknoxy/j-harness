package registry

import (
	"path/filepath"
	"testing"
)

// TestLoadRepoRegistry guards the checked-in agent-registry/ fixtures: the
// example agents and pipelines must always load and validate.
func TestLoadRepoRegistry(t *testing.T) {
	root := filepath.Join("..", "..", "agent-registry")
	r, err := Load(root)
	if err != nil {
		t.Fatalf("Load(%s): %v", root, err)
	}
	if len(r.BlueprintIDs()) == 0 {
		t.Error("expected at least one blueprint in repo registry")
	}
	if len(r.PipelineIDs()) == 0 {
		t.Error("expected at least one pipeline in repo registry")
	}
}
