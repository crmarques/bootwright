package compilation_test

import (
	"context"
	"fmt"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

const libvirtProviderYAML = "\n---\napiVersion: bootwright.io/v1alpha1\nkind: InfraProvider\nmetadata: {name: lab-libvirt}\nspec:\n" +
	"  libvirt:\n    machineRef: controller\n    uri: qemu:///system\n    bmcEmulationDefaults: {bindAddress: 192.0.2.1, auth: {credentialsRef: bmc}}\n" +
	"  networkAttachments:\n  - name: guests\n    libvirt: {bridge: %q, management: managed, address: 198.51.100.1/24, forward: nat}\n"

// firewalld reads a trailing `+` as an interface wildcard and a managed
// network puts its bridge in zone trusted by name, so validate refuses a
// bridge name holding one where an interface name admits it.
func TestValidateRefusesAWildcardBridgeName(t *testing.T) {
	const field = "$.spec.networkAttachments[0].libvirt.bridge"
	at := func(sink []diagnostics.Diagnostic) []diagnostics.Diagnostic {
		var found []diagnostics.Diagnostic
		for _, d := range sink {
			if d.Field == field && d.Object != nil && d.Object.Kind == string(api.InfraProvider) {
				found = append(found, d)
			}
		}
		return found
	}
	state, report, err := regressionCompiler().Compile(context.Background(), sources(environmentYAML+fmt.Sprintf(libvirtProviderYAML, "virbr+")))
	if refused := at(requireCompilationFailure(t, state, report, err)); len(refused) != 1 {
		t.Fatalf("a bridge named virbr+ was not refused at %s: %#v", field, diagnostics.Of(err))
	}
	_, _, err = regressionCompiler().Compile(context.Background(), sources(environmentYAML+fmt.Sprintf(libvirtProviderYAML, "virbr-lab")))
	if refused := at(diagnostics.Of(err)); len(refused) != 0 {
		t.Fatalf("a bridge named virbr-lab was refused: %#v", refused)
	}
}
