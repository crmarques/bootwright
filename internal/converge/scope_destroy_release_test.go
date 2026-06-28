package converge

import (
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/ownership"
	"github.com/crmarques/bootwright/internal/workspace"
)

func TestPlanInfraComponentReleasesKeepsSharedServiceReferencedByOtherContext(t *testing.T) {
	t.Cleanup(workspace.SetRootDirForTest(t.TempDir()))
	ctxA := mustContext(t, "ctx-a")
	ctxB := mustContext(t, "ctx-b")

	shared := sharedArtifactRecord()
	saveRecord(t, ctxA.OwnershipDir, shared)
	saveRecord(t, ctxB.OwnershipDir, shared)

	decision, err := PlanInfraComponentReleases("ctx-a", []ownership.ResourceRecord{shared})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(decision.Releases) != 1 {
		t.Fatalf("want 1 release, got %d (%+v)", len(decision.Releases), decision.Releases)
	}
	release := decision.Releases[0]
	if release.Name != "prov1-edge" || release.ComponentKind != "artifacts" || !slices.Equal(release.Referrers, []string{"ctx-b"}) {
		t.Fatalf("unexpected release %+v", release)
	}
	if names := decision.Names(); !slices.Equal(names, []string{"prov1-edge"}) {
		t.Fatalf("want release names [prov1-edge], got %v", names)
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
	if len(decision.Releases) != 0 {
		t.Fatalf("sole owner must tear down, got releases %+v", decision.Releases)
	}
}

func TestPlanInfraComponentReleasesIgnoresDegradingSharedService(t *testing.T) {
	t.Cleanup(workspace.SetRootDirForTest(t.TempDir()))
	ctxA := mustContext(t, "ctx-a")
	ctxB := mustContext(t, "ctx-b")

	lb := ownership.ResourceRecord{
		Kind: "infra-component", Name: "prov1-lb", Host: "bastion.lab",
		Owner: ownership.Owner, Labels: map[string]string{"bootwright.kind": "load-balancer"},
	}
	saveRecord(t, ctxA.OwnershipDir, lb)
	saveRecord(t, ctxB.OwnershipDir, lb)

	decision, err := PlanInfraComponentReleases("ctx-a", []ownership.ResourceRecord{lb})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(decision.Releases) != 0 {
		t.Fatalf("degrading shared service must never be released, got %+v", decision.Releases)
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

func TestRecordedComponentKindIsSelfContainedShared(t *testing.T) {
	for _, kind := range []string{"artifacts", "registry", "proxy"} {
		if !recordedComponentKindIsSelfContainedShared(kind) {
			t.Errorf("componentKind %q should be self-contained shared (release-aware)", kind)
		}
	}
	for _, kind := range []string{"load-balancer", "nameResolution", "ntp", "", "libvirt-domain"} {
		if recordedComponentKindIsSelfContainedShared(kind) {
			t.Errorf("componentKind %q must not be release-aware", kind)
		}
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
