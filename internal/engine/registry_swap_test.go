package engine

import (
	"context"
	"testing"

	"github.com/bigknoxy/j-harness/internal/llm"
	"github.com/bigknoxy/j-harness/internal/registry"
)

func TestSetRegistrySwapsSnapshot(t *testing.T) {
	reg, err := registry.Load("../../agent-registry")
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	eng, err := New(reg, llm.NewFake(llm.Response{Content: "ok"}))
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	if _, err := eng.RunAgent(context.Background(), "does_not_exist", "x"); err == nil {
		t.Fatal("expected unknown agent before swap")
	}

	// Reload the same registry and swap it in: lookups should still work.
	reloaded, err := registry.Load("../../agent-registry")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	eng.SetRegistry(reloaded)

	if _, err := eng.RunAgent(context.Background(), "generic_agent", "x"); err != nil {
		t.Fatalf("run after swap: %v", err)
	}

	// A nil swap is a no-op.
	eng.SetRegistry(nil)
	if _, err := eng.RunAgent(context.Background(), "generic_agent", "x"); err != nil {
		t.Fatalf("run after nil swap: %v", err)
	}
}
