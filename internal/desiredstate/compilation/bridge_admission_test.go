package compilation_test

import (
	"context"
	"fmt"
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

// A managed Network is a segment of its provider under its own name, so a
// segment of that name makes a Machine's segmentRef ambiguous, and the refusal
// names the Network beside the segment rather than two segments.
func TestASegmentRefSharedByASegmentAndAManagedNetworkNamesBoth(t *testing.T) {
	const field = "$.spec.substrate.libvirt.nics[0].segmentRef"
	network := "\n---\napiVersion: bootwright.io/v1alpha1\nkind: Network\nmetadata: {name: guests}\nspec:\n  provided: false\n  ipv4: {cidr: 198.51.100.0/24, gateway: 198.51.100.1}\n  libvirt: {providerRef: lab-libvirt, bridge: virbr-guests}\n"
	machine := "\n---\napiVersion: bootwright.io/v1alpha1\nkind: Machine\nmetadata: {name: guest}\nspec:\n  os: {provided: true}\n  hardware: {nics: [{name: enp1s0}]}\n  substrate: {providerRef: lab-libvirt, libvirt: {nics: [{nicRef: enp1s0, segmentRef: guests}]}}\n"
	state, report, err := regressionCompiler().Compile(context.Background(), sources(environmentYAML+fmt.Sprintf(libvirtProviderYAML, "virbr-lab")+network+machine))
	var refused []diagnostics.Diagnostic
	for _, d := range requireCompilationFailure(t, state, report, err) {
		if d.Field == field {
			refused = append(refused, d)
		}
	}
	if len(refused) != 1 || refused[0].Message != "the named guests is ambiguous: it names an entry of spec.segments on InfraProvider/lab-libvirt and Network/guests, whose spec.libvirt.providerRef names lab-libvirt" ||
		refused[0].Remediation != "rename the entry of spec.segments on InfraProvider/lab-libvirt named guests, or Network/guests, with every reference to it" {
		t.Fatalf("refusals at %s: %#v", field, refused)
	}
}
