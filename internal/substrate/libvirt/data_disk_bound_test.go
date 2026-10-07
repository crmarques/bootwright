package libvirt

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/substrate"
)

func TestTheDataDiskBoundIsTheDomainsTargets(t *testing.T) {
	if len(diskTargets) != substrate.MaxDataDisks {
		t.Fatalf("the domain presents %d data disk targets, and admission admits %d data disks", len(diskTargets), substrate.MaxDataDisks)
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
