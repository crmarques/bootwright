package artifactserver

import (
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

// A server references its serving certificate and the keys its SSH placement
// names, and nothing else.
func TestAServerReportsTheSecretsItsRequestReferences(t *testing.T) {
	requests, err := Requests(catalogOf(controller(), artifactServer()), "controller", testContext)
	if err != nil {
		t.Fatal(err)
	}
	plain := requests[0]
	remote := plain
	remote.Placement = Placement{Connection: connectionSSH, Machine: "services", Address: "192.0.2.1", Port: 22, User: "root", PrivateKeyRef: "services-key", KnownHostsRef: "services-host-key"}
	insecure := plain
	insecure.TLS = nil
	for name, tc := range map[string]struct {
		request Request
		want    []string
	}{
		"local":    {plain, []string{"artifact-server-tls"}},
		"remote":   {remote, []string{"artifact-server-tls", "services-host-key", "services-key"}},
		"insecure": {insecure, nil},
	} {
		t.Run(name, func(t *testing.T) {
			data, err := tc.request.Canonical()
			if err != nil {
				t.Fatal(err)
			}
			references, err := New(nil, fixedClock{}).SecretReferences(reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{Request: data}})
			if err != nil || !slices.Equal(references, tc.want) {
				t.Fatalf("references = %v (%v), want %v", references, err, tc.want)
			}
		})
	}
}
