package libvirt

import (
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// A libvirt host reached over SSH presents its key only to authenticate and
// verifies the host against its known_hosts entry, so a binding holds
// neither and both renew or are replaced while the host stays applied; the
// emulated BMC's credentials and certificate its machine gives the emulator
// stay held.
func TestALibvirtHostBindingHoldsNeitherItsSSHKeyNorItsKnownHosts(t *testing.T) {
	host := hostRequest(t)
	host.Placement = machine.Placement{Connection: machine.ConnectionSSH, Machine: "hypervisor", Address: "192.0.2.10", Port: 22, User: "root", PrivateKeyRef: "hypervisor-key", KnownHostsRef: "hypervisor-known-hosts"}
	canonical, err := host.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	block := reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: "host", Request: canonical}}
	references, err := NewHost(nil).SecretReferences(block)
	if err != nil || !slices.Equal(references, []string{"hypervisor-key", "hypervisor-known-hosts"}) {
		t.Fatalf("references = %v (%v), want the SSH key and its known_hosts entry", references, err)
	}
	held, classified := lifecycle.EffectMaterial(NewHost(nil), block)
	if !classified || len(held) != 0 {
		t.Fatalf("held = %v (classified %v), want nothing held", held, classified)
	}
	request := machineRequest(t)
	request.Placement = host.Placement
	if request.Controller.CredentialsRef == "" || request.Controller.CertificateRef == "" {
		t.Fatalf("the fixture's emulated BMC names no credentials or certificate: %+v", request.Controller)
	}
	canonical, err = request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	block = reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: "machine", Request: canonical}}
	held, classified = lifecycle.EffectMaterial(NewMachine(nil), block)
	want := []string{request.Controller.CertificateRef, request.Controller.CredentialsRef}
	slices.Sort(want)
	slices.Sort(held)
	if !classified || !slices.Equal(held, want) {
		t.Fatalf("held = %v (classified %v), want the emulated BMC's material %v alone", held, classified, want)
	}
}
