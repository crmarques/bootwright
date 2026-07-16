package converge

import (
	"slices"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/ownership"
	"github.com/crmarques/bootwright/internal/workspace"
)

func TestPlanInfraComponentDestroyBlocksOwnerTeardownWhileReferenced(t *testing.T) {
	t.Cleanup(workspace.SetRootDirForTest(t.TempDir()))
	ctxHub := mustContext(t, "hub")
	ctxSpoke := mustContext(t, "spoke")

	owner := sharedArtifactRecord()
	reference := owner
	reference.Role = ownership.RoleReference
	reference.Context = "spoke"
	saveRecord(t, ctxHub.OwnershipDir, owner)
	saveRecord(t, ctxSpoke.OwnershipDir, reference)

	decision, err := PlanInfraComponentDestroyBlocks("hub", v1alpha1.State{}, []ownership.ResourceRecord{owner}, false)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(decision.Blocks) != 1 {
		t.Fatalf("want 1 block, got %d (%+v)", len(decision.Blocks), decision.Blocks)
	}
	block := decision.Blocks[0]
	if block.Name != "prov1-edge" || block.ComponentKind != "artifacts" || !slices.Equal(block.Contexts, []string{"spoke"}) {
		t.Fatalf("unexpected block %+v", block)
	}
	if refErr := InfraComponentDestroyBlockError(decision.Blocks); refErr == nil {
		t.Fatalf("blocks must render a refusal error")
	}
}

func TestPlanInfraComponentDestroyBlocksOwnerTeardownWhileCoOwned(t *testing.T) {
	t.Cleanup(workspace.SetRootDirForTest(t.TempDir()))
	ctxHub := mustContext(t, "hub")
	ctxSpoke := mustContext(t, "spoke")

	owner := sharedArtifactRecord()
	coOwner := owner
	coOwner.Context = "spoke"
	saveRecord(t, ctxHub.OwnershipDir, owner)
	saveRecord(t, ctxSpoke.OwnershipDir, coOwner)

	decision, err := PlanInfraComponentDestroyBlocks("hub", v1alpha1.State{}, []ownership.ResourceRecord{owner}, false)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(decision.Blocks) != 1 {
		t.Fatalf("want 1 block from co-owner, got %d (%+v)", len(decision.Blocks), decision.Blocks)
	}
	if block := decision.Blocks[0]; block.Name != "prov1-edge" || !slices.Equal(block.Contexts, []string{"spoke"}) {
		t.Fatalf("unexpected co-owner block %+v", block)
	}
	if InfraComponentDestroyBlockError(decision.Blocks) == nil {
		t.Fatalf("co-owner block must render a refusal error")
	}
}

func TestPlanInfraComponentDestroyBlocksTearsDownWhenSoleOwner(t *testing.T) {
	t.Cleanup(workspace.SetRootDirForTest(t.TempDir()))
	ctxA := mustContext(t, "ctx-a")
	mustContext(t, "ctx-b")

	shared := sharedArtifactRecord()
	saveRecord(t, ctxA.OwnershipDir, shared)

	decision, err := PlanInfraComponentDestroyBlocks("ctx-a", v1alpha1.State{}, []ownership.ResourceRecord{shared}, false)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(decision.Blocks) != 0 {
		t.Fatalf("sole owner must tear down unblocked, got blocks %+v", decision.Blocks)
	}
}

func sharedArtifactRecord() ownership.ResourceRecord {
	return ownership.ResourceRecord{
		Kind:   "infra-component",
		Name:   "prov1-edge",
		Host:   "bastion.lab",
		Owner:  ownership.Owner,
		Labels: map[string]string{"bootwright.kind": "artifacts"},
	}
}

func mustContext(t *testing.T, name string) workspace.Context {
	t.Helper()
	ctx, err := workspace.NewContext(name)
	if err != nil {
		t.Fatalf("new context %s: %v", name, err)
	}
	if err := workspace.EnsureDirs(ctx); err != nil {
		t.Fatalf("ensure dirs %s: %v", name, err)
	}
	return ctx
}

func saveRecord(t *testing.T, dir string, record ownership.ResourceRecord) {
	t.Helper()
	if err := ownership.SaveResource(dir, record); err != nil {
		t.Fatalf("save record in %s: %v", dir, err)
	}
}
