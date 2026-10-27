package libvirt

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/substrate"
)

func TestTheDataDiskBoundIsTheDomainsTargets(t *testing.T) {
	for _, ssd := range []bool{false, true} {
		disks := []api.Value{}
		for range substrate.MaxDataDisks {
			disks = append(disks, mediaDisk("data", ssd))
		}
		if targets, valid := substrate.DataDiskPresentations(disks); !valid || len(targets) != substrate.MaxDataDisks {
			t.Fatal("the domain target bound differs from substrate admission")
		}
		if _, valid := substrate.DataDiskPresentations(append(disks, mediaDisk("excess", ssd))); valid {
			t.Fatal("the domain target selector exceeded substrate admission")
		}
	}
}

func TestEveryProviderNameYieldsAHostBlockIdentity(t *testing.T) {
	if got, want := HostBlockID(strings.Repeat("a", 48)), "substrate-host-"+strings.Repeat("a", 48); got != want {
		t.Fatalf("the 63-byte host block moved to %q", got)
	}
	if block := HostBlockID(strings.Repeat("a", 63)); !reconciliation.ValidSegment(block) || block == HostBlockID(strings.Repeat("a", 62)+"b") {
		t.Fatalf("a 63-byte provider name yields %q, not a distinct block identity", block)
	}
}
