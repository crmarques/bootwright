package libvirt

import (
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// A guest's removal is gated on its domain being shut off, which only the host
// shows, so a destroy's plan names it as a Machine to stop first. A provider
// host's quiescence is its guests', and it is not named.
func TestAGuestIsAMachineADestroyStopsFirst(t *testing.T) {
	var machine lifecycle.Capability = MachineCapability{}
	if prober, ok := machine.(lifecycle.QuiescenceProber); !ok || !prober.ProbesQuiescence() {
		t.Fatal("a guest's removal is not gated on an observed quiescence")
	}
	var host lifecycle.Capability = HostCapability{}
	if prober, ok := host.(lifecycle.QuiescenceProber); ok && prober.ProbesQuiescence() {
		t.Fatal("a provider host was named as a Machine to stop")
	}
}

func TestAGuestRemovalDestroysItsHost(t *testing.T) {
	var machine lifecycle.Capability = MachineCapability{}
	if removal, ok := machine.(lifecycle.HostRemoval); !ok || !removal.RemovesHost(reconciliation.Block{}) {
		t.Fatal("a guest's removal does not report that it destroys the host")
	}
	var host lifecycle.Capability = HostCapability{}
	if _, ok := host.(lifecycle.HostRemoval); ok {
		t.Fatal("a provider host's removal claims to destroy a guest")
	}
}
