package converge

import (
	"slices"
	"strings"
	"testing"

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

	decision, err := PlanInfraComponentDestroyBlocks("hub", nil, []ownership.ResourceRecord{owner}, false)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(decision.Blocks) != 1 {
		t.Fatalf("want 1 block, got %d (%+v)", len(decision.Blocks), decision.Blocks)
	}
	block := decision.Blocks[0]
	if block.Name != "prov1-edge" || block.Host != "bastion.lab" || block.ComponentKind != "artifacts" || !slices.Equal(block.Contexts, []string{"spoke"}) {
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

	decision, err := PlanInfraComponentDestroyBlocks("hub", nil, []ownership.ResourceRecord{owner}, false)
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

	decision, err := PlanInfraComponentDestroyBlocks("ctx-a", nil, []ownership.ResourceRecord{shared}, false)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(decision.Blocks) != 0 {
		t.Fatalf("sole owner must tear down unblocked, got blocks %+v", decision.Blocks)
	}
}

func TestPlanInfraComponentDestroyBlocksSiblingOwnedUnrecordedService(t *testing.T) {
	t.Cleanup(workspace.SetRootDirForTest(t.TempDir()))
	mustContext(t, "spoke")
	ctxHub := mustContext(t, "hub")

	owner := sharedArtifactRecord()
	saveRecord(t, ctxHub.OwnershipDir, owner)

	services := []InfraComponentServiceRef{{Name: "prov1-edge", Kind: "artifactServer", Host: "bastion.lab"}}
	decision, err := PlanInfraComponentDestroyBlocks("spoke", services, nil, false)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(decision.Blocks) != 1 {
		t.Fatalf("a rendered service owned by a sibling with no local record must block, got %+v", decision.Blocks)
	}
	block := decision.Blocks[0]
	if block.Name != "prov1-edge" || !block.Unrecorded || !slices.Equal(block.Contexts, []string{"hub"}) {
		t.Fatalf("unexpected sibling-owner block %+v", block)
	}
	if InfraComponentDestroyBlockError(decision.Blocks) == nil {
		t.Fatalf("sibling-owner block must render a refusal error")
	}
}

func TestPlanInfraComponentDestroyReleasesLocalReferenceWithoutTearingOwner(t *testing.T) {
	t.Cleanup(workspace.SetRootDirForTest(t.TempDir()))
	spoke := mustContext(t, "spoke")
	hub := mustContext(t, "hub")
	owner := sharedArtifactRecord()
	reference := owner
	reference.Role = ownership.RoleReference
	reference.Context = "spoke"
	saveRecord(t, hub.OwnershipDir, owner)
	saveRecord(t, spoke.OwnershipDir, reference)

	services := []InfraComponentServiceRef{{Name: owner.Name, Kind: "artifactServer", Host: owner.Host}}
	decision, err := PlanInfraComponentDestroyBlocks("spoke", services, []ownership.ResourceRecord{reference}, false)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(decision.Blocks) != 0 || len(decision.Warnings) != 0 {
		t.Fatalf("reference release must not authorize or plan base teardown: %+v", decision)
	}
}

func TestPlanInfraComponentDestroyFailsClosedOnIncompleteLocalReference(t *testing.T) {
	t.Cleanup(workspace.SetRootDirForTest(t.TempDir()))
	mustContext(t, "ctx-a")
	decision, err := PlanInfraComponentDestroyBlocks("ctx-a", []InfraComponentServiceRef{{Name: "prov1-artifacts", Kind: "artifacts", Host: "bastion.lab"}}, []ownership.ResourceRecord{{Kind: ownershipInfraComponentKind, Name: "prov1-artifacts", Role: ownership.RoleReference}}, false)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(decision.Warnings) != 1 || !strings.Contains(decision.Warnings[0].Error(), "reference record") || !strings.Contains(decision.Warnings[0].Error(), "name/host") {
		t.Fatalf("incomplete local reference must fail closed, got %#v", decision.Warnings)
	}
}

func TestPlanInfraComponentDestroyFailsClosedOnIncompleteSiblingIdentity(t *testing.T) {
	t.Cleanup(workspace.SetRootDirForTest(t.TempDir()))
	mustContext(t, "spoke")
	hub := mustContext(t, "hub")
	if err := ownership.SaveResource(hub.OwnershipDir, ownership.ResourceRecord{Kind: ownershipInfraComponentKind, Name: "prov1-edge", Owner: ownership.Owner}); err != nil {
		t.Fatalf("save incomplete owner: %v", err)
	}

	decision, err := PlanInfraComponentDestroyBlocks("spoke", []InfraComponentServiceRef{{Name: "prov1-edge", Kind: "loadBalancer", Host: "bastion.lab"}}, nil, false)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if warningErr := InfraComponentDestroyScanWarningError(decision.Warnings); warningErr == nil {
		t.Fatalf("identity-incomplete sibling record must produce a hard scan warning: %+v", decision)
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
