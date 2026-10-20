//go:build linux && amd64

package main

import (
	"context"
	"errors"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate/capacity"
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
	allocations, err := libvirtAllocations(catalog, "lab")
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
		if err := libvirtCapacityAdmission(nil)(context.Background(), "lab", catalog, owned, starting); err != nil {
			t.Fatalf("%s: admission = %v", name, diagnostics.Of(err))
		}
	}
	starting := []reconciliation.Block{block(reconciliation.StageClusters, "StorageCluster", "lab-ceph"), block(reconciliation.StageMachines, libvirt.MachineKind, allocations[0].Machine)}
	err = libvirtCapacityAdmission(nil)(context.Background(), "lab", catalog, owned, starting)
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

type capacityCheckFunc func(context.Context, []capacity.Allocation, bool) ([]capacity.Check, error)

func (f capacityCheckFunc) Check(ctx context.Context, a []capacity.Allocation, dry bool) ([]capacity.Check, error) {
	return f(ctx, a, dry)
}

func TestContainerOnlyApplyProvesAllDeclaredLibvirtGuestsBeforeAdmission(t *testing.T) {
	state, _ := compileAcceptance(t, exampleDirectory(t, "lab-sno"))
	catalog := state.Effective()
	if len(catalog.OfKind(api.StorageCluster)) != 0 {
		t.Fatal("test must have no storage cluster")
	}
	allocations, err := libvirtAllocations(catalog, "lab")
	if err != nil || len(allocations) != 1 {
		t.Fatalf("projection: %v %v", allocations, err)
	}
	called := 0
	checker := capacityCheckFunc(func(_ context.Context, got []capacity.Allocation, dry bool) ([]capacity.Check, error) {
		called++
		if dry || len(got) != len(allocations) || !got[0].Reserve.Configured {
			t.Fatalf("capacity admission lost declared allocation or reserve: %+v dry %t", got, dry)
		}
		return []capacity.Check{{ID: "capacity.memory", Target: got[0].Host, Status: capacity.Failed, Reason: "declared guests exceed available memory"}}, nil
	})
	block := reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{Kind: libvirt.MachineKind, Object: allocations[0].Machine}}
	if err := admitLibvirtCapacity(context.Background(), "lab", catalog, nil, []reconciliation.Block{block}, checker); err == nil {
		t.Fatal("insufficient container-only host capacity admitted")
	}
	if err := admitLibvirtCapacity(context.Background(), "lab", catalog, nil, nil, checker); err != nil {
		t.Fatal(err)
	}
	if called != 1 {
		t.Fatalf("continuation with all machine blocks done observed capacity: %d", called)
	}
}
