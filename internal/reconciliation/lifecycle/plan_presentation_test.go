package lifecycle

import (
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

func TestPlanStepRetainsFrozenActionMetadata(t *testing.T) {
	block := reconciliation.BlockDefinition{ID: "opaque", Kind: "Machine", Object: "demo-node", Implementation: "os-install-anaconda-v1", Description: "install the operating system of demo-node"}
	got := steps(reconciliation.Plan{Blocks: []reconciliation.Block{{BlockDefinition: block}}}, nil)
	if len(got) != 1 || got[0].Kind != block.Kind || got[0].Object != block.Object || got[0].Implementation != block.Implementation || got[0].Description != block.Description {
		t.Fatalf("frozen action projection = %+v", got)
	}
}
