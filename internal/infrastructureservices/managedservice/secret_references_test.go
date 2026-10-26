package managedservice

import (
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// A managed service references only the keys its SSH placement names.
func TestAManagedServiceReportsTheSecretsItsPlacementNames(t *testing.T) {
	capability := NewCapability(testDefinition(), nil)
	requests, err := capability.Requests(catalogOf(controller(), service(api.Proxy, "lab-proxy")), "controller", testContext)
	if err != nil {
		t.Fatal(err)
	}
	local := requests[0]
	remote := local
	remote.Placement = machine.Placement{Connection: machine.ConnectionSSH, Machine: "services", Address: "192.0.2.9", Port: 22, User: "root", PrivateKeyRef: "services-key", KnownHostsRef: "host-key"}
	for name, tc := range map[string]struct {
		request Request
		want    []string
	}{
		"local":  {local, nil},
		"remote": {remote, []string{"host-key", "services-key"}},
	} {
		t.Run(name, func(t *testing.T) {
			data, err := tc.request.Canonical()
			if err != nil {
				t.Fatal(err)
			}
			references, err := capability.SecretReferences(reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{Request: data}})
			if err != nil || !slices.Equal(references, tc.want) {
				t.Fatalf("references = %v (%v), want %v", references, err, tc.want)
			}
		})
	}
}
