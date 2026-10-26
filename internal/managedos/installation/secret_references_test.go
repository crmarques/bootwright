package installation

import (
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

func frozenBlock(t *testing.T, request interface{ Canonical() ([]byte, error) }) reconciliation.Block {
	t.Helper()
	data, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	return reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{Request: data}}
}

// Each installation block reports the declarations its frozen request names:
// the controller's, the fleet and host keys, the publications' certificates;
// an erase reports its install request's, and a tree its own certificate.
func TestInstallationBlocksReportTheSecretsTheirRequestsReference(t *testing.T) {
	call, request := execution(t, "digest")
	machine := eraseMachine()
	install := requestOf(t, labCatalog(metalProvider(), machine, sshKeySecret(api.MapValue(field("generated", api.MapValue())))), machine.Name())
	erase, err := eraseRequest(machine, install)
	if err != nil {
		t.Fatal(err)
	}
	_, tree := treeFixture(t)
	secured := tree
	secured.Publication.CertificateRef = "tree-tls"
	for name, tc := range map[string]struct {
		references func(reconciliation.Block) ([]string, error)
		block      reconciliation.Block
		want       []string
	}{
		"install":      {New(nil).SecretReferences, call.Block, []string{"bootwright-machine-key", "lab-artifacts-tls", "lab-bmc-credentials", "lab-bmc-tls"}},
		"erase":        {NewErase(nil).SecretReferences, frozenBlock(t, erase), []string{"bootwright-machine-key", "lab-artifacts-tls", "lab-bmc-credentials", "metal-01-host-key"}},
		"tree":         {NewTree(nil).SecretReferences, frozenBlock(t, tree), nil},
		"secured tree": {NewTree(nil).SecretReferences, frozenBlock(t, secured), []string{"tree-tls"}},
	} {
		t.Run(name, func(t *testing.T) {
			references, err := tc.references(tc.block)
			if err != nil || !slices.Equal(references, tc.want) {
				t.Fatalf("references = %v (%v), want %v", references, err, tc.want)
			}
		})
	}
	private := request
	private.Private = &Publication{CertificateRef: "private-tls"}
	if !slices.Contains(referencedSecrets(private), "private-tls") {
		t.Fatalf("a private publication's certificate is not among %v", referencedSecrets(private))
	}
	if _, err := New(nil).SecretReferences(reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{Request: []byte(`{"version":"other"}`)}}); err == nil {
		t.Fatal("an unread install request was reported as naming no Secret")
	}
}
