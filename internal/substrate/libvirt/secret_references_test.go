package libvirt

import (
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// A provider host references its placement's Secrets and a machine adds its
// controller's credentials and certificate.
func TestLibvirtBlocksReportTheSecretsTheirRequestsReference(t *testing.T) {
	remote := provider(field("libvirt", provider().Spec().Get("libvirt").With("machineRef", api.StringValue("hypervisor"))))
	remoteHosts, err := HostRequests(catalogOf(controller(), remoteHost(), remote, networkConfig()), "controller", testContext)
	if err != nil {
		t.Fatal(err)
	}
	frozen := func(t *testing.T, request interface{ Canonical() ([]byte, error) }) reconciliation.Block {
		data, err := request.Canonical()
		if err != nil {
			t.Fatal(err)
		}
		return reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{Request: data}}
	}
	for name, tc := range map[string]struct {
		references func(reconciliation.Block) ([]string, error)
		block      reconciliation.Block
		want       []string
	}{
		"local host":  {NewHost(nil).SecretReferences, frozen(t, hostRequest(t)), nil},
		"remote host": {NewHost(nil).SecretReferences, frozen(t, remoteHosts[0]), []string{"host-key", "hypervisor-key"}},
		"machine":     {NewMachine(nil).SecretReferences, frozen(t, machineRequest(t)), []string{"lab-bmc-credentials", "lab-bmc-tls"}},
	} {
		t.Run(name, func(t *testing.T) {
			references, err := tc.references(tc.block)
			if err != nil || !slices.Equal(references, tc.want) {
				t.Fatalf("references = %v (%v), want %v", references, err, tc.want)
			}
		})
	}
}
