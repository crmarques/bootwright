package lifecycle

import (
	"context"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

func ownedClusterBlock(id string, kind api.Kind, object string, dependencies ...string) reconciliation.BlockDefinition {
	block := stagedDefinition(id, reconciliation.StageClusters, dependencies...)
	block.Kind, block.Object = string(kind), object
	return block
}

func clusterAddOn(id, cluster string, dependencies ...string) reconciliation.BlockDefinition {
	block := stagedDefinition(id, reconciliation.StageAddOns, dependencies...)
	block.Object = cluster + "." + id
	block.Requires = []reconciliation.ObjectRef{{Kind: string(api.OpenShiftCluster), Object: cluster}}
	return block
}

func clustersHarness(t *testing.T, definitions ...reconciliation.BlockDefinition) *harness {
	t.Helper()
	h := newPlannedHarness(t, append([]reconciliation.BlockDefinition{stagedDefinition("alpha", reconciliation.StageInfrastructureServices)}, definitions...))
	h.service.capabilities = anyKindResolver{testResolver{capability: h.capability}}
	return h
}

func twoClusters(t *testing.T, extra ...reconciliation.BlockDefinition) *harness {
	t.Helper()
	return clustersHarness(t, append([]reconciliation.BlockDefinition{
		ownedClusterBlock("cluster-a", api.OpenShiftCluster, "a", "alpha"),
		ownedClusterBlock("cluster-b", api.OpenShiftCluster, "b", "alpha"),
		clusterAddOn("gitops-a", "a"),
	}, extra...)...)
}

func clusterDestroy(h *harness, floor reconciliation.Stage, clusters ...string) (*OperationResult, error) {
	return h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(floor), Clusters: clusters, SkipConfirmation: true})
}

// A staged destroy narrowed to one cluster takes back that cluster's steps and
// keeps the other cluster's; the record, the preview and status carry the
// clusters, a later one naming the other cluster widens the taken-back set
// cumulatively, and a destroy without --stage takes back what both kept.
func TestAStagedDestroyNarrowedToAClusterKeepsTheOtherCluster(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := twoClusters(t)
	completeApply(t, h)
	preview, err := h.service.Plan(ctx, PlanRequest{ContextName: testContextName, Verb: reconciliation.Destroy, Floor: string(reconciliation.StageClusters), FloorClusters: []string{"a"}})
	if err != nil || preview.Scope == nil || !slices.Equal(preview.Scope.Clusters, []string{"a"}) || !slices.Equal(preview.Scope.Kept, []string{"alpha", "cluster-b"}) {
		t.Fatalf("the narrowed preview reads %+v (%v)", preview, err)
	}
	if _, err := clusterDestroy(h, reconciliation.StageClusters, "a"); err != nil {
		t.Fatalf("destroy --stage clusters --clusters a: %+v", diagnostics.Of(err))
	}
	first, plan := currentRecord(t, h)
	if !trimmed(first) || !slices.Equal(first.Stage.Clusters, []string{"a"}) || !slices.Equal(first.Stage.TakenBack, []string{"cluster-a", "gitops-a"}) ||
		!slices.Equal(planIDs(plan), []string{"gitops-a", "cluster-a"}) {
		t.Fatalf("the narrowed destroy recorded %+v over %v", first, planIDs(plan))
	}
	status, err := h.service.Status(ctx, StatusRequest{ContextName: testContextName})
	if err != nil || status.Lifecycle == nil || !slices.Equal(status.Lifecycle.Clusters, []string{"a"}) {
		t.Fatalf("status reads %+v (%v)", status, err)
	}
	if _, err := clusterDestroy(h, reconciliation.StageClusters, "b"); err != nil {
		t.Fatalf("destroy --stage clusters --clusters b: %+v", diagnostics.Of(err))
	}
	second, plan := currentRecord(t, h)
	if second.Replaces != first.ID || !slices.Equal(second.Stage.Clusters, []string{"b"}) ||
		!slices.Equal(second.Stage.TakenBack, []string{"cluster-a", "cluster-b", "gitops-a"}) || !slices.Equal(planIDs(plan), []string{"cluster-b"}) {
		t.Fatalf("the second narrowed destroy recorded %+v over %v", second, planIDs(plan))
	}
	if _, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if whole, plan := currentRecord(t, h); whole.Stage != nil || !slices.Equal(planIDs(plan), []string{"alpha"}) {
		t.Fatalf("the whole destroy recorded %+v over %v", whole, planIDs(plan))
	}
	if destroyed := h.capability.destroys; !slices.Equal(destroyed, []string{"gitops-a", "cluster-a", "cluster-b", "alpha"}) {
		t.Fatalf("the destroys removed %v", destroyed)
	}
}

// A step two clusters own, as Ceph replication is, is taken back when either
// of them is named, since removing either end takes the replication back.
func TestANarrowedStagedDestroyTakesBackAJointStepOfEitherCluster(t *testing.T) {
	t.Parallel()
	replication := stagedDefinition("replication", reconciliation.StageClusters, "alpha")
	replication.ClusterOwners = []string{"a", "b"}
	for _, named := range []string{"a", "b"} {
		t.Run(named, func(t *testing.T) {
			t.Parallel()
			h := twoClusters(t, replication)
			completeApply(t, h)
			if _, err := clusterDestroy(h, reconciliation.StageClusters, named); err != nil {
				t.Fatalf("destroy --stage clusters --clusters %s: %+v", named, diagnostics.Of(err))
			}
			removal, _ := currentRecord(t, h)
			if !slices.Contains(removal.Stage.TakenBack, "replication") || !slices.Contains(removal.Stage.TakenBack, "cluster-"+named) ||
				slices.Contains(removal.Stage.TakenBack, "alpha") || len(removal.Stage.TakenBack) == 5 {
				t.Fatalf("--clusters %s took back %v", named, removal.Stage.TakenBack)
			}
		})
	}
}

// A step of a cluster the destroy does not name that depends on what it takes
// back, as an add-on of a kept OpenShift cluster consuming a named Ceph cluster,
// refuses before anything registers, naming it and the command that also takes
// back its cluster, which then takes both back.
func TestANarrowedStagedDestroyRefusesADependentOfAnotherCluster(t *testing.T) {
	t.Parallel()
	h := clustersHarness(t,
		ownedClusterBlock("storage", api.CephCluster, "ceph", "alpha"),
		ownedClusterBlock("openshift", api.OpenShiftCluster, "ocp", "alpha"),
		clusterAddOn("fdf", "ocp", "storage"),
	)
	completeApply(t, h)
	before := untouchedOf(h)
	_, err := clusterDestroy(h, reconciliation.StageClusters, "ceph")
	reported := diagnostics.Of(err)
	if firstCode(err) != destroyStageOtherCluster.Code || !strings.Contains(reported[0].Message, "fdf (cluster ocp)") ||
		!strings.Contains(reported[0].Remediation, "destroy --dry-run --context "+testContextName+" --stage clusters --clusters ceph,ocp") {
		t.Fatalf("destroy --stage clusters --clusters ceph returned %v %+v", err, reported)
	}
	before.require(t, h)
	if _, err := clusterDestroy(h, reconciliation.StageClusters, "ceph", "ocp"); err != nil {
		t.Fatalf("the widened command: %+v", diagnostics.Of(err))
	}
	if removal, _ := currentRecord(t, h); !slices.Equal(removal.Stage.TakenBack, []string{"fdf", "openshift", "storage"}) {
		t.Fatalf("the widened command took back %v", removal.Stage.TakenBack)
	}
}

// A name that owns no step of the context, or a cluster that holds nothing at
// or above the floor, refuses before anything registers.
func TestANarrowedStagedDestroyRefusesAnUnknownCluster(t *testing.T) {
	t.Parallel()
	h := twoClusters(t)
	completeApply(t, h)
	before := untouchedOf(h)
	for clusters, want := range map[string]string{"c": "names c, which owns no step", "c,d": "names c, d, which own no step"} {
		_, err := clusterDestroy(h, reconciliation.StageClusters, strings.Split(clusters, ",")...)
		if reported := diagnostics.Of(err); firstCode(err) != engineInputInvalid.Code || !strings.Contains(reported[0].Message, want) {
			t.Fatalf("--clusters %s returned %v %+v", clusters, err, reported)
		}
	}
	_, err := clusterDestroy(h, reconciliation.StageAddOns, "b")
	if reported := diagnostics.Of(err); firstCode(err) != engineInputInvalid.Code || !strings.Contains(reported[0].Message, "names b, which holds nothing at or above the add-ons stage") ||
		!strings.Contains(reported[0].Remediation, "destroy --dry-run --context "+testContextName+" --stage clusters --clusters b") {
		t.Fatalf("--stage add-ons --clusters b returned %v %+v", err, reported)
	}
	for _, floor := range []reconciliation.Stage{"", reconciliation.StageMachines} {
		if _, err := clusterDestroy(h, floor, "a"); firstCode(err) != engineInputInvalid.Code {
			t.Fatalf("--stage %q --clusters a returned %v", floor, err)
		}
	}
	before.require(t, h)
}

// An unfinished narrowed staged destroy continues under its clusters, which
// every remedy repeats; naming clusters that do not cover it refuses, naming the
// covering command, while a covering set widens it.
func TestAnUnfinishedNarrowedStagedDestroyContinuesAndWidensByItsClusters(t *testing.T) {
	t.Parallel()
	h := twoClusters(t)
	completeApply(t, h)
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeFailed}}
	_, err := clusterDestroy(h, reconciliation.StageClusters, "a")
	if reported := diagnostics.Of(err); err == nil || !strings.Contains(reported[0].Remediation, "--stage clusters --clusters a") {
		t.Fatalf("the failed narrowed destroy returned %v %+v", err, reported)
	}
	h.capability.outcomes = nil
	failed, _ := currentRecord(t, h)
	before := untouchedOf(h)
	_, err = clusterDestroy(h, reconciliation.StageClusters, "b")
	if reported := diagnostics.Of(err); firstCode(err) != destroyStageChanged.Code ||
		!strings.Contains(reported[0].Message, "froze the clusters stage of cluster a, not the clusters stage of cluster b") ||
		!strings.Contains(reported[0].Remediation, "widen it with bootwright destroy --dry-run --context "+testContextName+" --stage clusters --clusters a,b") {
		t.Fatalf("--clusters b over the unfinished destroy returned %v %+v", err, reported)
	}
	before.require(t, h)
	preview, err := h.service.Plan(context.Background(), PlanRequest{ContextName: testContextName, Verb: reconciliation.Destroy, Floor: string(reconciliation.StageClusters), FloorClusters: []string{"a", "b"}})
	if err != nil || preview.Widens == nil || preview.Widens.Operation != failed.ID || !slices.Equal(preview.Widens.FromClusters, []string{"a"}) {
		t.Fatalf("the covering preview reads %+v (%v)", preview, err)
	}
	if _, err := clusterDestroy(h, reconciliation.StageClusters, "a"); err != nil {
		t.Fatalf("the continuation: %+v", diagnostics.Of(err))
	}
	continued, _ := currentRecord(t, h)
	if !trimmed(continued) || !slices.Equal(continued.Stage.Clusters, []string{"a"}) || !slices.Equal(continued.Stage.TakenBack, []string{"cluster-a", "gitops-a"}) {
		t.Fatalf("the continuation recorded %+v", continued)
	}
}

// Over an unfinished narrowed staged destroy, the same clusters with more of
// them widen it and freeze the cumulative taken-back set.
func TestACoveringClusterSetWidensAnUnfinishedNarrowedStagedDestroy(t *testing.T) {
	t.Parallel()
	h := twoClusters(t)
	completeApply(t, h)
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeFailed}}
	if _, err := clusterDestroy(h, reconciliation.StageClusters, "a"); err == nil {
		t.Fatal("a narrowed destroy whose block failed reported success")
	}
	h.capability.outcomes = nil
	failed, _ := currentRecord(t, h)
	if _, err := clusterDestroy(h, reconciliation.StageClusters, "a", "b"); err != nil {
		t.Fatalf("the widening: %+v", diagnostics.Of(err))
	}
	widened, _ := currentRecord(t, h)
	if widened.Replaces != failed.ID || !trimmed(widened) || !slices.Equal(widened.Stage.Clusters, []string{"a", "b"}) ||
		!slices.Equal(widened.Stage.TakenBack, []string{"cluster-a", "cluster-b", "gitops-a"}) {
		t.Fatalf("the widening recorded %+v", widened)
	}
}

func TestAStagedFloorCoversOnlyAWiderScope(t *testing.T) {
	t.Parallel()
	of := func(stage reconciliation.Stage, clusters ...string) stagedFloor {
		return stagedFloor{stage: stage, clusters: clusters}
	}
	for _, test := range []struct {
		wider, held stagedFloor
		covers      bool
	}{
		{of(reconciliation.StageClusters), of(reconciliation.StageClusters, "a"), true},
		{of(reconciliation.StageClusters, "a", "b"), of(reconciliation.StageClusters, "a"), true},
		{of(reconciliation.StageClusters, "a"), of(reconciliation.StageAddOns, "a"), true},
		{of(reconciliation.StageMachines), of(reconciliation.StageClusters, "a"), true},
		{of(reconciliation.StageClusters, "b"), of(reconciliation.StageClusters, "a"), false},
		{of(reconciliation.StageClusters, "a"), of(reconciliation.StageClusters), false},
		{of(reconciliation.StageAddOns, "a", "b"), of(reconciliation.StageClusters, "a"), false},
	} {
		if got := test.wider.covers(test.held); got != test.covers {
			t.Errorf("%+v covers %+v = %v", test.wider, test.held, got)
		}
	}
	if joined := of(reconciliation.StageAddOns, "b").joined(of(reconciliation.StageClusters, "a")); !joined.same(of(reconciliation.StageClusters, "a", "b")) {
		t.Errorf("joined = %+v", joined)
	}
}

// The restoring apply after a narrowed staged destroy re-creates exactly the
// named cluster's steps and runs none of the other cluster's.
func TestTheRestoreAfterANarrowedStagedDestroyRecreatesOnlyWhatItTookBack(t *testing.T) {
	t.Parallel()
	h := twoClusters(t)
	completeApply(t, h)
	if _, err := clusterDestroy(h, reconciliation.StageClusters, "a"); err != nil {
		t.Fatalf("destroy --stage clusters --clusters a: %+v", diagnostics.Of(err))
	}
	before := len(h.capability.applies)
	completeApply(t, h)
	restored := slices.Sorted(slices.Values(h.capability.applies[before:]))
	if !slices.Equal(restored, []string{"cluster-a", "gitops-a"}) {
		t.Fatalf("the restore applied %v", restored)
	}
}

type managedMachineResolver struct {
	steps   Capability
	machine Capability
}

func (r managedMachineResolver) Bindings() []CapabilityBinding {
	return []CapabilityBinding{{Kind: "ArtifactServer", Implementation: "artifact-server-nginx-v1"},
		{Kind: string(api.Machine), Implementation: "artifact-server-nginx-v1", Family: reconciliation.ManagedMachineFamily}}
}

func (r managedMachineResolver) Resolve(kind, _ string) (Capability, bool) {
	if kind == string(api.Machine) {
		return r.machine, true
	}
	return r.steps, true
}

// A narrowed staged destroy checks against the Machines it keeps only the
// never-started steps its clusters reach: a restoring apply that left another
// cluster's step on a kept Machine never started neither refuses the destroy
// nor freezes a power target for that Machine. The fold still reads the step
// taken back, as it reads every step the lineage never started, so the later
// restore re-creates it, and that restore proves the kept Machine the step is
// bound to off first: running, it refuses lifecycle.start-power naming it;
// off, the restore installs the step.
func TestANarrowedStagedDestroyLeavesAnotherClustersNeverStartedSteps(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	machine := stagedDefinition("machine", reconciliation.StageMachines, "host")
	machine.Kind = string(api.Machine)
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{
		stagedDefinition("alpha", reconciliation.StageInfrastructureServices),
		stagedDefinition("host", reconciliation.StageSubstrates, "alpha"),
		ownedClusterBlock("cluster-a", api.OpenShiftCluster, "a", "alpha"),
		ownedClusterBlock("cluster-b", api.OpenShiftCluster, "b", "machine"),
		clusterAddOn("gitops-a", "a"),
	})
	var read []Probe
	power := &restorePowerMachine{testCapability: &testCapability{definitions: []reconciliation.BlockDefinition{machine}}, on: map[string]bool{}, read: &read, deletes: map[string]bool{"machine": true}}
	h.service.capabilities = managedMachineResolver{steps: h.capability, machine: power}
	completeApply(t, h)
	stagedDestroy(t, h, reconciliation.StageMachines)
	for _, request := range []ApplyRequest{{Machines: []string{"machine"}}, {Clusters: []string{"a"}}} {
		request.ContextName, request.SkipConfirmation = testContextName, true
		if _, err := h.service.Apply(ctx, request); err != nil {
			t.Fatalf("the restoring apply of %+v: %+v", request, diagnostics.Of(err))
		}
	}
	if restoring, _ := currentRecord(t, h); restoring.Restores == "" || restoring.State != reconciliation.OperationPaused {
		t.Fatalf("the targeted restores left %+v", restoring)
	}
	if _, err := clusterDestroy(h, reconciliation.StageClusters, "a"); err != nil {
		t.Fatalf("destroy --stage clusters --clusters a: %+v", diagnostics.Of(err))
	}
	removal, plan := currentRecord(t, h)
	if !slices.Equal(removal.Stage.TakenBack, []string{"cluster-a", "cluster-b", "gitops-a"}) || len(removal.Stage.KeptPower) != 0 || !slices.Equal(planIDs(plan), []string{"gitops-a", "cluster-a"}) {
		t.Fatalf("the narrowed destroy recorded %+v over %v", removal.Stage, planIDs(plan))
	}
	before := len(h.capability.applies)
	read, power.on["machine"] = nil, true
	_, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
	if reported := diagnostics.Of(err); firstCode(err) != startPowerRefused.Code || !strings.Contains(reported[0].Message, "Machine/machine (powered on)") ||
		!strings.Contains(reported[0].Remediation, "prove the named machines powered off or absent and repeat bootwright apply --context "+testContextName) {
		t.Fatalf("the restore over the running kept Machine returned %v %+v", err, reported)
	}
	if len(h.capability.applies) != before || !slices.ContainsFunc(read, func(probe Probe) bool { return probe.Block.Object == "machine" }) {
		t.Fatalf("the refused restore applied %v after reading %+v", h.capability.applies[before:], read)
	}
	power.on["machine"] = false
	if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
		t.Fatalf("the restore: %+v", diagnostics.Of(err))
	}
	if restored := slices.Sorted(slices.Values(h.capability.applies[before:])); !slices.Equal(restored, []string{"cluster-a", "cluster-b", "gitops-a"}) {
		t.Fatalf("the restore applied %v", restored)
	}
}

// A frozen plan with a cluster child that names no owning cluster cannot be
// narrowed, as a scoped apply over it refuses, so a narrowed staged destroy
// refuses before anything registers.
func TestANarrowedStagedDestroyRefusesAPlanLackingClusterOwnership(t *testing.T) {
	t.Parallel()
	h := twoClusters(t, stagedDefinition("orphan", reconciliation.StageAddOns))
	completeApply(t, h)
	before := untouchedOf(h)
	_, err := clusterDestroy(h, reconciliation.StageClusters, "a")
	if reported := diagnostics.Of(err); firstCode(err) != engineInputInvalid.Code || !strings.Contains(reported[0].Message, "lacks cluster ownership for orphan") {
		t.Fatalf("--clusters a over a plan lacking ownership returned %v %+v", err, reported)
	}
	before.require(t, h)
}

// A narrowed staged destroy that would keep an unfinished step of another
// cluster also offers naming that cluster.
func TestANarrowedStagedDestroyKeepingAnUnfinishedStepNamesItsCluster(t *testing.T) {
	t.Parallel()
	h := twoClusters(t)
	applyChained(t, h, map[string]Result{"cluster-b": {Outcome: reconciliation.OutcomeFailed}})
	before := untouchedOf(h)
	_, err := clusterDestroy(h, reconciliation.StageClusters, "a")
	if reported := diagnostics.Of(err); firstCode(err) != destroyStageKeepsUnfinished.Code || !strings.Contains(reported[0].Message, "cluster-b (failed)") ||
		!strings.Contains(reported[0].Remediation, "by naming their clusters too with bootwright destroy --dry-run --context "+testContextName+" --stage clusters --clusters a,b") {
		t.Fatalf("--clusters a keeping a failed step of cluster b returned %v %+v", err, reported)
	}
	before.require(t, h)
}
