//go:build linux && amd64

package main

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/substrate/libvirt"
)

func TestExamplePoolsPlansSixHostsAndOwnedECImages(t *testing.T) {
	state, _ := compileAcceptance(t, exampleDirectory(t, "libvirt-6-nodes-ceph-pools"))
	if refused := productionUnsupported(t, state); len(refused) != 0 {
		t.Fatalf("refused %+v", refused)
	}
	definitions := productionDefinitions(t, state, "libvirt-6-nodes-ceph-pools")
	if _, err := reconciliation.NewPlan(reconciliation.Apply, definitions); err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if got := len(hostBlockIDs(definitions, "ceph-lab")); got != 6 {
		t.Fatalf("got %d storage hosts", got)
	}
	catalog := state.Effective()
	machines, err := libvirt.MachineRequests(catalog, "controller", "pools-lab")
	if err != nil || len(machines) != 6 {
		t.Fatalf("machine requests: %d %v", len(machines), err)
	}
	for _, machine := range machines {
		if !machine.TPM || len(machine.Interfaces) != 2 || len(machine.Disks) != 4 {
			t.Fatalf("missing P1 prerequisite: %+v", machine)
		}
		for i, want := range []struct {
			target, media  string
			size, rotation int
		}{
			{"sda", "ssd", 40, 1}, {"sdb", "hdd", 40, 7200}, {"sdc", "ssd", 10, 1},
		} {
			disk := machine.Disks[i+1]
			if disk.Target != want.target || disk.Media != want.media || disk.SizeGiB != want.size || disk.RotationRate != want.rotation {
				t.Fatalf("wrong data/DB identity: %+v", disk)
			}
		}
	}
	for _, name := range []string{"ec42-data", "ec22-data"} {
		pool := requireObject(t, catalog, api.StoragePool, name)
		if pool.Spec().Get("type").Text() != "erasure" {
			t.Fatalf("%s is not erasure coded", name)
		}
	}
	image := requireObject(t, catalog, api.StorageRBDImage, "ec42-disk")
	if image.Spec().Get("poolRef").Text() != "rbd-headers" || image.Spec().Get("dataPoolRef").Text() != "ec42-data" {
		t.Fatal("EC image lost separate replicated headers")
	}
}
