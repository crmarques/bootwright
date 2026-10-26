package baremetal

import (
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// A machine references its controller credentials and, when declared, the
// trust bundle its controller connection verifies against.
func TestAMachineReportsTheSecretsItsControllerNames(t *testing.T) {
	for name, tc := range map[string]struct {
		catalog api.Catalog
		want    []string
	}{
		"credentials only": {catalogOf(), []string{"server-bmc"}},
		"with a bundle":    {catalogWithControllerTLS(m("verify", true, "trustBundleRef", "server-bmc-ca")), []string{"server-bmc", "server-bmc-ca"}},
	} {
		t.Run(name, func(t *testing.T) {
			definition := planIn(t, reconciliation.Apply, tc.catalog).Definitions[0]
			references, err := NewMachine(nil).SecretReferences(reconciliation.Block{BlockDefinition: definition})
			if err != nil || !slices.Equal(references, tc.want) {
				t.Fatalf("references = %v (%v), want %v", references, err, tc.want)
			}
		})
	}
}
