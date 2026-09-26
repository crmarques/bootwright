package main

import (
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// Roles still write to fixed host-global scratch directories, so two blocks
// running at once could publish into each other's work. The composition root
// binds the lifecycle it assembles to one block at a time until every role has
// a private scratch directory per invocation; an unbound service would take
// the engine's default and run several.
func TestLifecycleConcurrencyBound(t *testing.T) {
	service, ok := isolatedServices(t).Lifecycle.(lifecycle.Service)
	if !ok {
		t.Fatal("the composition root did not assemble the lifecycle engine")
	}
	if bound := service.Concurrency(); bound != 1 {
		t.Fatalf("composition binds lifecycle concurrency %d, want 1", bound)
	}
}
