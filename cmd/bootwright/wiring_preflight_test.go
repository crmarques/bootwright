//go:build linux && amd64

package main

import (
	"context"
	"errors"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate/libvirt"
)

func TestStorageCapacityAdmissionSkipsTransitionsThatAllocateNoGuest(t *testing.T) {
	sources := editedExample(t, "libvirt-3-nodes-ceph", exampleEdit{
		file: "infra/providers/lab-libvirt.yaml",
		old:  "    capacityReserve:\n      cpu: 1\n      memoryMiB: 2048\n      diskGiB: 10\n",
		new:  "",
	})
	state, _ := compileAcceptance(t, sources)
	catalog := state.Effective()
	allocations, err := storageAllocations(catalog, "lab")
	if err != nil || len(allocations) == 0 {
		t.Fatalf("allocations = %v, %v", allocations, err)
	}
	block := func(stage reconciliation.Stage, kind, object string) reconciliation.Block {
		return reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: kind + "/" + object, Stage: stage, Kind: kind, Object: object}}
	}
	owned := map[string]lifecycle.ObjectOwnership{}
	cases := map[string][]reconciliation.Block{
		"a controller-stage block":         {block(reconciliation.StageController, "ArtifactServer", "mirror")},
		"Machines done, only Ceph remains": {block(reconciliation.StageClusters, "StorageCluster", "lab-ceph")},
		"a Machine no allocation projects": {block(reconciliation.StageMachines, libvirt.MachineKind, "absent")},
		"nothing left to start":            {},
	}
	for name, starting := range cases {
		if err := storageCapacityAdmission(context.Background(), "lab", catalog, owned, starting); err != nil {
			t.Fatalf("%s: admission = %v", name, diagnostics.Of(err))
		}
	}
	starting := []reconciliation.Block{block(reconciliation.StageClusters, "StorageCluster", "lab-ceph"), block(reconciliation.StageMachines, libvirt.MachineKind, allocations[0].Machine)}
	err = storageCapacityAdmission(context.Background(), "lab", catalog, owned, starting)
	var failure *diagnostics.Failure
	if !errors.As(err, &failure) || len(failure.Diagnostics) == 0 {
		t.Fatalf("a transition starting a libvirt Machine with unknown capacity = %v", err)
	}
	for _, diagnostic := range failure.Diagnostics {
		if diagnostic.Code != "preflight.unknown" {
			t.Fatalf("diagnostic = %+v, want preflight.unknown", diagnostic)
		}
	}
}
