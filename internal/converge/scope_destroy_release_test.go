package converge

import (
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/ownership"
	"github.com/crmarques/bootwright/internal/workspace"
)

func TestPlanInfraComponentReleasesBlocksOwnerTeardownWhileReferenced(t *testing.T) {
	t.Cleanup(workspace.SetRootDirForTest(t.TempDir()))
	ctxHub := mustContext(t, "hub")
	ctxSpoke := mustContext(t, "spoke")

	owner := sharedArtifactRecord() // hub owns the base (role empty => owner)
	reference := owner
	reference.Role = ownership.RoleReference
	reference.Context = "spoke"
	saveRecord(t, ctxHub.OwnershipDir, owner)
	saveRecord(t, ctxSpoke.OwnershipDir, reference)

	// hub (the owner) must be blocked from tearing the base down: spoke references it.
	decision, err := PlanInfraComponentReleases("hub", []ownership.ResourceRecord{owner})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(decision.Releases) != 0 {
		t.Fatalf("owner teardown is not a release, got %+v", decision.Releases)
	}
	if len(decision.Blocks) != 1 {
		t.Fatalf("want 1 block, got %d (%+v)", len(decision.Blocks), decision.Blocks)
	}
	block := decision.Blocks[0]
	if block.Name != "prov1-edge" || block.ComponentKind != "artifacts" || !slices.Equal(block.Referrers, []string{"spoke"}) {
		t.Fatalf("unexpected block %+v", block)
	}
	if refErr := ReferencedOwnerError(decision.Blocks); refErr == nil {
		t.Fatalf("blocks must render a refusal error")
	}
}

func TestPlanInfraComponentReleasesReleasesReferenceForAllKinds(t *testing.T) {
	t.Cleanup(workspace.SetRootDirForTest(t.TempDir()))
	ctxHub := mustContext(t, "hub")
	ctxSpoke := mustContext(t, "spoke")

	// A reference is released (fragment-only) regardless of component kind, including
	// a degrading service (load-balancer) the old model could never release.
	base := ownership.ResourceRecord{
		Kind: "infra-component", Name: "prov1-lb", Host: "bastion.lab",
		Owner: ownership.Owner, Labels: map[string]string{"bootwright.kind": "load-balancer"},
	}
	reference := base
	reference.Role = ownership.RoleReference
	reference.Context = "spoke"
	saveRecord(t, ctxHub.OwnershipDir, base)
	saveRecord(t, ctxSpoke.OwnershipDir, reference)

	decision, err := PlanInfraComponentReleases("spoke", []ownership.ResourceRecord{reference})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(decision.Blocks) != 0 {
		t.Fatalf("a referrer teardown is never blocked, got %+v", decision.Blocks)
	}
	if names := decision.Names(); !slices.Equal(names, []string{"prov1-lb"}) {
		t.Fatalf("want reference released [prov1-lb], got %v", names)
	}
}

func TestPlanInfraComponentReleasesTearsDownWhenSoleOwner(t *testing.T) {
	t.Cleanup(workspace.SetRootDirForTest(t.TempDir()))
	ctxA := mustContext(t, "ctx-a")
	mustContext(t, "ctx-b") // exists but records nothing

	shared := sharedArtifactRecord()
	saveRecord(t, ctxA.OwnershipDir, shared)

	decision, err := PlanInfraComponentReleases("ctx-a", []ownership.ResourceRecord{shared})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(decision.Releases) != 0 || len(decision.Blocks) != 0 {
		t.Fatalf("sole owner must tear down unblocked, got releases %+v blocks %+v", decision.Releases, decision.Blocks)
	}
}

func TestApplyInfraComponentReleaseExtraVar(t *testing.T) {
	plan := &WorkflowPlan{}
	ApplyInfraComponentReleaseExtraVar(plan, nil)
	if len(plan.ExtraVarPairs) != 0 {
		t.Fatalf("empty release set must add no extra var, got %v", plan.ExtraVarPairs)
	}
	ApplyInfraComponentReleaseExtraVar(plan, []string{"prov1-edge", "prov1-proxy"})
	want := InfraComponentReleaseExtraVar + "=prov1-edge,prov1-proxy"
	if len(plan.ExtraVarPairs) != 1 || plan.ExtraVarPairs[0] != want {
		t.Fatalf("want %q, got %v", want, plan.ExtraVarPairs)
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
