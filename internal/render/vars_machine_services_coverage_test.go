package render

import (
	"testing"

	"github.com/crmarques/bootwright/internal/infra/support"
)

func TestMachineServiceVarBuildersCoverRegistry(t *testing.T) {
	for _, entry := range support.ServiceEntries() {
		if _, ok := machineServiceVarBuilders[entry.Key.Kind]; !ok {
			t.Fatalf("support registry defines service %v but render has no machineServiceVarBuilders entry for kind %q; its rendered vars would be silently dropped", entry.Key, entry.Key.Kind)
		}
	}
}
