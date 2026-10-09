package main

import (
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func TestLifecycleConcurrencyBound(t *testing.T) {
	service, ok := isolatedServices(t).Lifecycle.(lifecycle.Service)
	if !ok {
		t.Fatal("the composition root did not assemble the lifecycle engine")
	}
	if bound := service.Concurrency(); bound != lifecycle.MaxRunningBlocks {
		t.Fatalf("composition binds lifecycle concurrency %d, want %d", bound, lifecycle.MaxRunningBlocks)
	}
}
