//go:build linux && amd64

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/storage/ceph"
	"github.com/crmarques/bootwright/internal/substrate/baremetal"
)

func TestPhysicalCephRetirementGraphOrdersEraseBeforeProofAndClaimRelease(t *testing.T) {
	plan := physicalCephRetirementPlan(t, false)
	policy := ceph.PhysicalRetirementBlockID("ceph-lab")
	for _, machine := range []string{"metal-01", "metal-02", "metal-03"} {
		chain := []string{baremetal.BlockID(machine), policy, installation.EraseBlockID(machine), installation.BlockID(machine), ceph.HostBlockID("ceph-lab", machine), ceph.ClusterBlockID("ceph-lab")}
		for _, verb := range []reconciliation.Verb{reconciliation.Apply, reconciliation.Destroy} {
			ordered := plan
			if verb == reconciliation.Destroy {
				var err error
				ordered, err = plan.Inverse()
				if err != nil {
					t.Fatal(err)
				}
			}
			for index := 1; index < len(chain); index++ {
				parent, child := chain[index-1], chain[index]
				if verb == reconciliation.Destroy {
					parent, child = child, parent
				}
				if !physicalCephAncestors(ordered, child)[parent] {
					t.Fatalf("%s lacks %s before %s", verb, parent, child)
				}
			}
		}
	}
}

func physicalCephRetirementPlan(t *testing.T, omitted bool) reconciliation.Plan {
	t.Helper()
	sources := exampleDirectory(t, "physical-3-nodes-ceph")
	if omitted {
		edits := []exampleEdit{}
		for number := 1; number <= 3; number++ {
			path := fmt.Sprintf("infra/machines/metal-%02d.yaml", number)
			old := fmt.Sprintf("    eraseOnRemoval:\n      method: logicalOverwrite\n\n      targets:\n        - deviceName: /dev/sda\n          wwn: '0x600000000000000%d'\n          serialNumber: SYNTHETIC-ROOT-%02d\n          sizeBytes: 53687091200\n\n        - deviceName: /dev/disk/by-id/wwn-0x500000000000000%d\n          wwn: '0x500000000000000%d'\n          serialNumber: SYNTHETIC-CEPH-%02d\n          sizeBytes: 107374182400\n\n", number, number, number, number, number)
			edits = append(edits, exampleEdit{path, old, ""})
		}
		sources = editedExample(t, "physical-3-nodes-ceph", edits...)
	}
	state, _ := compileAcceptance(t, sources)
	plan, err := reconciliation.NewPlan(reconciliation.Apply, productionDefinitions(t, state, "physical-3-nodes-ceph"))
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func physicalCephAncestors(plan reconciliation.Plan, id string) map[string]bool {
	wanted := map[string]bool{}
	visit := func(string) {}
	visit = func(next string) {
		block, _ := plan.Block(next)
		for _, dependency := range block.Dependencies {
			if !wanted[dependency] {
				wanted[dependency] = true
				visit(dependency)
			}
		}
	}
	visit(id)
	return wanted
}

func TestPhysicalCephUnstartedPoliciesNeverSelectDiskEffects(t *testing.T) {
	plan := physicalCephRetirementPlan(t, false)
	for _, claimsStarted := range []bool{false, true} {
		states := map[string]reconciliation.BlockState{}
		if claimsStarted {
			for id := range physicalCephAncestors(plan, ceph.PhysicalRetirementBlockID("ceph-lab")) {
				states[id] = reconciliation.BlockDone
			}
		}
		inverse, err := reconciliation.OwnedSubset(plan, states).Inverse()
		if err != nil {
			t.Fatal(err)
		}
		for _, block := range inverse.Blocks {
			if block.Implementation == installation.EraseImplementation || block.Implementation == installation.Implementation || strings.HasPrefix(block.Implementation, "storage-ceph") {
				t.Fatalf("unstarted policy selected a disk effect: %s", block.ID)
			}
		}
	}
}

func TestPhysicalCephPartialApplyCanReleaseUnusedClaimsWithoutErasing(t *testing.T) {
	for _, phase := range []string{"before-erase-policy", "after-erase-policy", "omitted-erase-policy"} {
		t.Run(phase, func(t *testing.T) {
			plan := physicalCephRetirementPlan(t, phase == "omitted-erase-policy")
			policy, _ := plan.Block(ceph.PhysicalRetirementBlockID("ceph-lab"))
			states := map[string]reconciliation.BlockState{policy.ID: reconciliation.BlockDone}
			for id := range physicalCephAncestors(plan, policy.ID) {
				states[id] = reconciliation.BlockDone
			}
			if phase == "after-erase-policy" {
				for _, block := range plan.Blocks {
					if block.Implementation == installation.EraseImplementation {
						states[block.ID] = reconciliation.BlockDone
						for id := range physicalCephAncestors(plan, block.ID) {
							states[id] = reconciliation.BlockDone
						}
					}
				}
			}
			owned := reconciliation.OwnedSubset(plan, states)
			inverse, err := owned.Inverse()
			if err != nil {
				t.Fatal(err)
			}
			for _, block := range inverse.Blocks {
				if block.Implementation == installation.Implementation || block.Implementation == ceph.Implementation || block.Implementation == ceph.HostImplementation {
					t.Fatalf("unstarted install or storage block was selected for removal: %s", block.ID)
				}
			}
			execution := physicalCephUnstartedExecution(t, plan, inverse, states, policy)
			capability := ceph.NewPhysicalRetirement().WithProofs(cephDependencyProofs{}).WithUninstalledProofs(installation.PhysicalEraseUnstarted).
				WithErasureProofs(func(reconciliation.Block, lifecycle.BlockEvidence, string) (ceph.PhysicalErasure, bool, error) {
					t.Fatal("unstarted physical installation consulted an erase receipt")
					return ceph.PhysicalErasure{}, false, nil
				})
			result, err := capability.Destroy(context.Background(), execution)
			var proof ceph.PhysicalRetirementEvidence
			if err != nil || json.Unmarshal(result.Evidence, &proof) != nil || !proof.Retired || len(proof.Receipts) != 3 {
				t.Fatalf("partial apply cleanup refused: %s %+v", result.Evidence, diagnostics.Of(err))
			}
			for _, receipt := range proof.Receipts {
				if !receipt.RetainedWithoutInstall || receipt.Block != "" || receipt.Digest != "" || receipt.Request != "" {
					t.Fatal("unused claim release claimed disk erasure", receipt)
				}
			}
		})
	}
}

func physicalCephUnstartedExecution(t *testing.T, plan, inverse reconciliation.Plan, states map[string]reconciliation.BlockState, policy reconciliation.Block) lifecycle.Execution {
	t.Helper()
	execution := lifecycle.Execution{ApplyOperation: "apply-original", Operation: "remove-original", Block: policy}
	for _, block := range plan.Blocks {
		state := states[block.ID]
		if state == "" {
			state = reconciliation.BlockPending
		}
		original := lifecycle.BlockEvidence{Kind: block.Kind, Object: block.Object, Implementation: block.Implementation, Verb: reconciliation.Apply, State: state, Block: block}
		if block.Implementation == baremetal.Implementation {
			request, err := baremetal.DecodeRequest(block.Request)
			if err != nil {
				t.Fatal(err)
			}
			original.Evidence, _ = json.Marshal(baremetal.Evidence{Addresses: request.Addresses(), UUID: "11111111-1111-4111-8111-111111111111", Power: "On", Postcondition: true, Request: block.RequestDigest})
			execution.ApplyProved = append(execution.ApplyProved, original)
		}
		if block.Implementation == installation.EraseImplementation && state == reconciliation.BlockDone {
			original.Evidence, _ = json.Marshal(installation.EraseEvidence{Version: installation.EraseImplementation, Request: block.RequestDigest, Prepared: true})
			removed := original
			removed.Block, _ = inverse.Block(block.ID)
			removed.Verb = reconciliation.Destroy
			removed.Evidence, _ = json.Marshal(installation.EraseEvidence{Version: installation.EraseImplementation, Request: block.RequestDigest, Prepared: true, Removed: true, Retained: true})
			execution.RemovalProved = append(execution.RemovalProved, lifecycle.RemovalEvidence{Applied: original, Removed: removed})
		}
		if physicalCephAncestors(plan, block.ID)[policy.ID] {
			execution.OriginalRemovalDependents = append(execution.OriginalRemovalDependents, original)
		}
	}
	for _, proof := range execution.OriginalRemovalDependents {
		if proof.Block.ID == ceph.ClusterBlockID("ceph-lab") && proof.State != reconciliation.BlockPending {
			t.Fatal("partial apply fixture started Ceph")
		}
	}
	if slices.ContainsFunc(execution.ApplyProved, func(proof lifecycle.BlockEvidence) bool {
		return proof.Kind != string(api.Machine) || !strings.HasPrefix(proof.Block.ID, "machine-")
	}) {
		t.Fatal("partial apply fixture invented original Machine evidence")
	}
	return execution
}
