package main

import (
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/storage/ceph"
	"github.com/crmarques/bootwright/internal/substrate/libvirt"
)

// B1862, B1861: the composed operation store admits exactly the kept-host
// contracts the Ceph capabilities and the libvirt root disk reset record
// beside a staged destroy's plan, each only beside a plan that needs it.
func TestTheOperationStoreAdmitsTheKeptHostContracts(t *testing.T) {
	store := operationStore(systemClock{}, lifecycle.Executable{}, nil, nil)
	for implementation, need := range map[string]operationstore.KeptNeed{
		ceph.KeptImplementation:         operationstore.KeepsHosts,
		ceph.KeptHostImplementation:     operationstore.KeepsHosts,
		libvirt.RootResetImplementation: operationstore.BlanksRoots,
	} {
		if admitted := store.AdmitsKeptContract(implementation); admitted != need {
			t.Fatalf("the operation store admits the kept-host contract %s by need %d, want %d", implementation, admitted, need)
		}
	}
	for _, implementation := range []string{ceph.Implementation, libvirt.MachineImplementation} {
		if store.AdmitsKeptContract(implementation) != 0 {
			t.Fatalf("the operation store admits the block contract %s as a kept-host one", implementation)
		}
	}
}
