package physicalerase

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

// An erase reports the declarations its install request names.
func TestEraseBlockReportsTheSecretsItsInstallRequestReferences(t *testing.T) {
	machine := eraseMachine()
	install := requestOf(t, labCatalog(metalProvider(), machine, sshKeySecret(api.MapValue(field("generated", api.MapValue())))), machine.Name())
	erase, err := eraseRequest(machine, install)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"bootwright-machine-key", "lab-artifacts-tls", "lab-bmc-credentials", "metal-01-host-key"}
	references, err := New(nil).SecretReferences(frozenBlock(t, erase))
	if err != nil || !slices.Equal(references, want) {
		t.Fatalf("references = %v (%v), want %v", references, err, want)
	}
}
