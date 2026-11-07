package compilation_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

const libvirtProviderYAML = "\n---\napiVersion: bootwright.io/v1alpha1\nkind: InfraProvider\nmetadata: {name: lab-libvirt}\nspec:\n" +
	"  libvirt:\n    machineRef: controller\n    uri: qemu:///system\n    bmcEmulation: {bindAddress: 192.0.2.1, certificateRef: bmc-tls, auth: {credentialsRef: bmc}}\n" +
	"  segments:\n  - name: guests\n    libvirt: {bridge: %q}\n"

// firewalld reads a trailing `+` as an interface wildcard and a managed
// network puts its bridge in zone trusted by name, so validate refuses a
// bridge name holding one where an interface name admits it.
func TestValidateRefusesAWildcardBridgeName(t *testing.T) {
	const field = "$.spec.segments[0].libvirt.bridge"
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

// A provider's libvirt segment arm names an existing bridge and nothing else: a
// managed network is a Network and the capacity is the segment's mtu, so each
// field the arm held is an unknown field.
func TestALibvirtSegmentArmCarriesOnlyItsBridge(t *testing.T) {
	provider := strings.Replace(fmt.Sprintf(libvirtProviderYAML, "virbr-lab"), "{bridge: \"virbr-lab\"}", "{bridge: virbr-lab, management: managed, address: 198.51.100.1, forward: nat, mtu: 9000}", 1)
	state, report, err := regressionCompiler().Compile(context.Background(), sources(environmentYAML+provider))
	refused := map[string]bool{}
	for _, d := range requireCompilationFailure(t, state, report, err) {
		if d.Code == "api.field" && d.Message == "unknown field; spec.segments[0].libvirt permits bridge" {
			refused[d.Field] = true
		}
	}
	for _, removed := range []string{"management", "address", "forward", "mtu"} {
		if !refused["$.spec.segments[0].libvirt."+removed] {
			t.Errorf("a segment arm's %s was not refused as an unknown field: %#v", removed, diagnostics.Of(err))
		}
	}
}
