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

func TestTheProviderNameLimitIsTheHostBlockIdentity(t *testing.T) {
	if block := HostBlockID(strings.Repeat("a", substrate.ProviderNameLimit)); !reconciliation.ValidSegment(block) {
		t.Fatalf("the host block %q of a provider name at the limit is not a block identity", block)
	}
	if block := HostBlockID(strings.Repeat("a", substrate.ProviderNameLimit+1)); reconciliation.ValidSegment(block) {
		t.Fatalf("the host block %q of a provider name past the limit is still a block identity, so the limit is too low", block)
	}
}
