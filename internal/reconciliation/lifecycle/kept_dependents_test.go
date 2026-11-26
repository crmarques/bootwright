package lifecycle

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

// B1885, B1895: a step taken back from kept hosts while they run refuses
// when the staged destroy deletes its other hosts, which its teardown would
// then have to reach first, naming the clusters floor that keeps them all.
func TestAStepTakenBackFromRunningHostsRefusesWhenTheStagedDestroyDeletesItsOtherHosts(t *testing.T) {
	t.Parallel()
	cluster := keptOnlineHarness(t, keptOnlineOptions{})
	h := cluster.h
	cluster.failApply(t)
	before := untouchedOf(h)
	_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageMachines), SkipConfirmation: true})
	reported := diagnostics.Of(err)
	if firstCode(err) != destroyStageKeepsHost.Code || !strings.Contains(reported[0].Message, "the step storage is taken back from Machine/provided-1 while they run") ||
		!strings.Contains(reported[0].Message, "(B1895)") || !strings.Contains(reported[0].Remediation, "bootwright destroy --dry-run --context lab --stage clusters") {
		t.Fatalf("destroy --stage machines over storage on a deleted and a kept host returned %v", reported)
	}
	before.require(t, h)
}

// B1885: a step whose removal on running kept hosts goes with the teardown of
// a step it depends on, here a replication beside its storage cluster, refuses
// while the staged destroy keeps that step, naming the floor without a
// cluster narrowing; with both in the scope it is taken back with them.
func TestAStepThatGoesWithAKeptTeardownRefusesWithoutIt(t *testing.T) {
	t.Parallel()
	cluster := keptOnlineHarness(t, keptOnlineOptions{})
	h := cluster.h
	mirror := keptBlock("mirror", "StorageMirror", "mirror-v1", reconciliation.StageAddOns, "storage")
	mirror.Object = "mirror"
	dependent := &keptOnlineStorage{testCapability: &testCapability{definitions: []reconciliation.BlockDefinition{mirror}}, hosts: []string{"node-1", "provided-1"},
		contract: strings.Repeat("c", 64), residue: map[string]string{}}
	h.service.capabilities.(keptResolver)[CapabilityBinding{Kind: "StorageMirror", Implementation: "mirror-v1"}] = dependent
	completeApply(t, h)
	cluster.power("node-1", true)
	before := untouchedOf(h)
	_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageAddOns), SkipConfirmation: true})
	reported := diagnostics.Of(err)
	if firstCode(err) != destroyStageKeepsHost.Code || !strings.Contains(reported[0].Message, "the step mirror is taken back from its kept hosts with the teardown of the step storage it depends on") ||
		!strings.Contains(reported[0].Remediation, "bootwright destroy --dry-run --context lab --stage add-ons, which names no cluster") {
		t.Fatalf("destroy --stage add-ons over the mirror alone returned %v", reported)
	}
	before.require(t, h)
	cluster.storage.refuse = ""
	stagedDestroy(t, h, reconciliation.StageClusters)
	_, plan := currentRecord(t, h)
	if !slices.Equal(planIDs(plan), []string{"mirror", "storage"}) || !slices.Equal(keptHostsOf(plan, "mirror"), []string{"node-1", "provided-1"}) {
		t.Fatalf("the staged destroy of both took back %v, the mirror on %v", planIDs(plan), keptHostsOf(plan, "mirror"))
	}
}

type completionRecords struct {
	recordReader
	attempts    map[string]int
	effects     map[string]reconciliation.EffectState
	resolutions map[string]reconciliation.EffectState
}

func (r completionRecords) Block(_ context.Context, operation, block string) (operationstore.BlockRecord, error) {
	return operationstore.BlockRecord{Block: block, Attempts: r.attempts[operation]}, nil
}

func (r completionRecords) Attempt(_ context.Context, operation, block string, number int) (operationstore.Attempt, error) {
	return operationstore.Attempt{Block: block, Number: number, Phase: "observed", Effect: r.effects[operation+"/"+strconv.Itoa(number)]}, nil
}

func (r completionRecords) LastResolution(_ context.Context, operation, block string, number int) (operationstore.Attempt, bool, error) {
	effect, found := r.resolutions[operation+"/"+strconv.Itoa(number)]
	return operationstore.Attempt{Block: block, Number: number, Phase: "observed", Effect: effect}, found, nil
}

// B1885, M1: a step's records read completed when any member of the lineage
// recorded it done or any attempt or resolution of it observed its effect
// completed, whatever its holder's state now; only then is it never used.
func TestARemovalReadsAStepCompletedWhenAnyEarlierRecordCompletedIt(t *testing.T) {
	t.Parallel()
	member := func(id string, state reconciliation.BlockState) lineageMember {
		return lineageMember{operation: operationstore.Operation{ID: id}, states: map[string]reconciliation.BlockState{"storage": state}}
	}
	for name, test := range map[string]struct {
		held      lineage
		records   completionRecords
		completed bool
	}{
		"done then failed":      {lineage{member("a2", reconciliation.BlockFailed), member("a1", reconciliation.BlockDone)}, completionRecords{attempts: map[string]int{"a2": 1}}, true},
		"failed after effect":   {lineage{member("a1", reconciliation.BlockFailed)}, completionRecords{attempts: map[string]int{"a1": 2}, effects: map[string]reconciliation.EffectState{"a1/1": reconciliation.EffectCompleted, "a1/2": reconciliation.EffectPartial}}, true},
		"resolved completed":    {lineage{member("a1", reconciliation.BlockFailed)}, completionRecords{attempts: map[string]int{"a1": 1}, resolutions: map[string]reconciliation.EffectState{"a1/1": reconciliation.EffectCompleted}}, true},
		"failed without effect": {lineage{member("a1", reconciliation.BlockFailed)}, completionRecords{attempts: map[string]int{"a1": 1}, effects: map[string]reconciliation.EffectState{"a1/1": reconciliation.EffectPartial}}, false},
		"never planned":         {lineage{{operation: operationstore.Operation{ID: "a1"}, states: map[string]reconciliation.BlockState{}}}, completionRecords{attempts: map[string]int{"a1": 1}, effects: map[string]reconciliation.EffectState{"a1/1": reconciliation.EffectCompleted}}, false},
	} {
		completed, err := everCompleted(context.Background(), test.records, test.held, "storage")
		if err != nil || completed != test.completed {
			t.Fatalf("%s: read completed %t (%v), want %t", name, completed, err, test.completed)
		}
	}
}

// B1885, L3: a --clusters narrowing to one end of a replication between two
// storage clusters on running kept hosts takes the replication back, since it
// names either end, and so refuses while it keeps the other end's teardown;
// naming both takes all three back.
func TestANarrowedStagedDestroyTakesAReplicationBackOnlyWithBothEnds(t *testing.T) {
	t.Parallel()
	cluster := keptOnlineHarness(t, keptOnlineOptions{})
	h := cluster.h
	definitions := cluster.storage.definitions
	definitions[0].ClusterOwners = []string{"a"}
	second := keptBlock("storage-b", "StorageCluster", "store-v1", reconciliation.StageClusters, "node-1", "provided-1")
	second.Object, second.ClusterOwners = "store-b", []string{"b"}
	cluster.storage.definitions = append(definitions, second)
	mirror := keptBlock("replication", "StorageMirror", "mirror-v1", reconciliation.StageClusters, "storage", "storage-b")
	mirror.Object, mirror.ClusterOwners = "mirror", []string{"a", "b"}
	h.service.capabilities.(keptResolver)[CapabilityBinding{Kind: "StorageMirror", Implementation: "mirror-v1"}] = &keptOnlineStorage{
		testCapability: &testCapability{definitions: []reconciliation.BlockDefinition{mirror}}, hosts: []string{"node-1", "provided-1"}, contract: strings.Repeat("c", 64), residue: map[string]string{}}
	cluster.storage.refuse = ""
	completeApply(t, h)
	cluster.power("node-1", true)
	before := untouchedOf(h)
	_, err := clusterDestroy(h, reconciliation.StageClusters, "a")
	reported := diagnostics.Of(err)
	if firstCode(err) != destroyStageKeepsHost.Code || !strings.Contains(reported[0].Message, "the step replication is taken back from its kept hosts with the teardown of the step storage-b it depends on") ||
		!strings.Contains(reported[0].Remediation, "--stage clusters, which names no cluster") {
		t.Fatalf("destroy --stage clusters --clusters a over a replication to b returned %v", reported)
	}
	before.require(t, h)
	if _, err := clusterDestroy(h, reconciliation.StageClusters, "a", "b"); err != nil {
		t.Fatalf("destroy --stage clusters --clusters a,b: %+v", diagnostics.Of(err))
	}
	_, plan := currentRecord(t, h)
	if !slices.Equal(slices.Sorted(slices.Values(planIDs(plan))), []string{"replication", "storage", "storage-b"}) {
		t.Fatalf("the staged destroy of both ends took back %v", planIDs(plan))
	}
}
