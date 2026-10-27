//go:build linux && amd64

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/inputfs"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/substrate"
)

const labGuest = "infra/machines/rhel-01.yaml"

// onlyRefusal compiles sources and requires exactly one diagnostic, returning
// it.
func onlyRefusal(t *testing.T, sources desiredstate.Sources) diagnostics.Diagnostic {
	t.Helper()
	_, _, err := wireCompiler().Compile(context.Background(), sources)
	if found := diagnostics.Of(err); len(found) != 1 {
		t.Fatalf("diagnostics = %#v, want exactly one", found)
	}
	return diagnostics.Of(err)[0]
}

// Machine admission refuses, at validate, what the Machine's substrate cannot
// realize, and a mistake in a name or a fleet key yields only its own
// diagnostic.
func TestMachineAdmissionRefusesWhatItsSubstrateCannotRealize(t *testing.T) {
	compileAcceptance(t, exampleDirectory(t, "lab-rhel"))
	const guest = "Machine/rhel-01"
	t.Run("an invalid name without its contact", func(t *testing.T) {
		refusal := onlyRefusal(t, editedExample(t, "lab-rhel", exampleEdit{labGuest, "name: rhel-01", "name: Rhel_01"}))
		if refusal.Field != "$.metadata.name" {
			t.Fatalf("refusal = %#v", refusal)
		}
	})
	t.Run("an unresolved fleet key", func(t *testing.T) {
		refusal := onlyRefusal(t, editedExample(t, "lab-rhel", exampleEdit{"environment.yaml", "keyRef: bootwright-machine-key", "keyRef: missing-key"}))
		if refusal.Object == nil || refusal.Object.Kind != "Environment" || refusal.Field != "$.spec.remoteMachinesAccessKey.keyRef" {
			t.Fatalf("refusal = %#v", refusal)
		}
	})
	installerProvisioned := []exampleEdit{
		{labGuest, "    installProfileRef: rhel-9-8\n", ""},
		{labGuest, "  hardware:\n    rootDevice:\n      deviceName: /dev/vda\n\n", ""},
		{labGuest, "    configRef: lab-guests\n    # attachmentRef defaults to the configRef name, lab-guests.\n    installAddressRef: ip\n\n", ""},
		{labGuest, "\n    addresses:\n      - name: ip\n        address: 198.51.100.11/24\n        interface: enp1s0\n", "\n    addresses:\n      - name: fqdn\n        address: rhel-01.lab.example.test\n"},
	}
	// A /32 assignment reserves no address of its own, so only the managed
	// bridge's prefix refuses it.
	installAt := func(address string) []exampleEdit {
		return []exampleEdit{{labGuest, "address: 198.51.100.11/24", "address: " + address}}
	}
	cases := map[string]struct {
		edits               []exampleEdit
		code, object, field string
	}{
		"an installer-provisioned Machine without a network": {installerProvisioned, "api.invariant", guest, "$.spec.network"},
		"a bond-only network configuration": {[]exampleEdit{{"infra/networkconfigs/lab-guests.yaml", "type: ethernet", "type: bond"}},
			"api.invariant", guest, "$.spec.network.configRef"},
		"the bridge's host address":        {installAt("198.51.100.1/24"), "api.invariant", guest, "$.spec.network.installAddressRef"},
		"the bridge's network address":     {installAt("198.51.100.0/32"), "api.invariant", guest, "$.spec.network.installAddressRef"},
		"the bridge's broadcast address":   {installAt("198.51.100.255/32"), "api.invariant", guest, "$.spec.network.installAddressRef"},
		"its own prefix's network address": {installAt("198.51.100.0/24"), "api.value", guest, "$.spec.network.addresses[0].address"},
		"an interface binding": {[]exampleEdit{{labGuest, "    installAddressRef: ip\n", "    installAddressRef: ip\n    interfaceBinding:\n      - nicRef: enp1s0\n        interfaceName: enp1s0\n"}},
			"api.invariant", guest, "$.spec.network.interfaceBinding"},
		"a bare-metal arm": {[]exampleEdit{{labGuest, "      profileRef: rhel\n", "      profileRef: rhel\n    baremetal:\n      bmc:\n        address: https://bmc.example.test/redfish/v1/Systems/1\n        credentialsRef: lab-bmc-credentials\n"}},
			"api.invariant", guest, "$.spec.substrate.baremetal"},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			requireRefusal(t, editedExample(t, "lab-rhel", test.edits...), test.code, test.object, test.field)
		})
	}
}

// The hardware boot block selected nothing any substrate read, so it is an
// unknown field, and a bare-metal Machine selects no attachment, which
// configures nothing.
func TestABaremetalMachineCarriesNoBootNICOrAttachment(t *testing.T) {
	const metal = "infra/machines/metal-01.yaml"
	refusal := requireRefusal(t, editedExample(t, "lab-baremetal", exampleEdit{metal, "\n    nics:\n", "\n    boot:\n      nicRef: eno1\n\n    nics:\n"}),
		"api.field", "Machine/metal-01", "$.spec.hardware.boot")
	if !strings.Contains(refusal.Message, "eraseOnRemoval, nics, rootDevice") {
		t.Fatalf("the boot refusal says %q", refusal.Message)
	}
	compileAcceptance(t, editedExample(t, "lab-baremetal",
		exampleEdit{"infra/providers/lab-metal.yaml", "\n  networkAttachments:\n    - name: lab-metal\n      baremetal: {}\n", ""}))
}

// An operator-owned Machine no provider realizes, such as a bastion, authors
// its management controller in a bare-metal arm with no provider, and power
// targets it through that controller from the controller host.
func TestAProviderlessMachineIsPoweredThroughTheControllerItAuthors(t *testing.T) {
	const address = "https://bastion-bmc.lab.example.test/redfish/v1/Systems/1"
	root := t.TempDir()
	copySources(t, exampleDirectory(t, "lab-rhel"), root)
	bastion := "apiVersion: bootwright.io/v1alpha1\nkind: Machine\nmetadata:\n  name: bastion\n\nspec:\n  os:\n    provided: true\n\n" +
		"  hardware:\n    nics:\n      - name: eno1\n\n  substrate:\n    baremetal:\n      bmc:\n        address: " + address +
		"\n        credentialsRef: lab-bmc-credentials\n\n      nics:\n        - nicRef: eno1\n          macAddress: 02:00:00:00:20:01\n\n" +
		"  network:\n    addresses:\n      - name: fqdn\n        address: bastion.lab.example.test\n"
	if err := os.WriteFile(filepath.Join(root, "infra", "machines", "bastion.yaml"), []byte(bastion), 0o600); err != nil {
		t.Fatal(err)
	}
	sources, err := (inputfs.Reader{}).Read(context.Background(), []string{root})
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	state, _ := compileAcceptance(t, sources)
	effective := state.Effective()
	target, err := substrate.TargetFor(effective, requireObject(t, effective, api.Machine, "bastion"), "lab", "controller")
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if !target.Physical || target.Provider != "" || target.Controller.Endpoint != address ||
		target.Controller.CredentialsRef != "lab-bmc-credentials" || target.PlacementMachine.Name() != "controller" {
		t.Fatalf("target = %+v", target)
	}
}

// A value with exactly one valid derivation is derived (D183, Q82 b): a
// managed service binds the one address its endpoints name, and the wildcard
// when they name several; a generated certificate names what its consumers
// serve it at; an authored value overrides either.
func TestSingleDerivationValuesAreDerived(t *testing.T) {
	const proxy, tls, bmc = "infra/components/proxy.yaml", "secret-descriptors/artifact-server-tls.yaml", "secret-descriptors/lab-bmc-tls.yaml"
	sans := "      dnsNames:\n        - controller.lab.example.test\n\n      ipAddresses:\n        - 192.0.2.1\n\n"
	state, _ := compileAcceptance(t, editedExample(t, "lab-rhel", exampleEdit{tls, sans, ""},
		exampleEdit{bmc, "      dnsNames:\n        - localhost\n\n      ipAddresses:\n        - 127.0.0.1\n\n", ""}))
	effective := state.Effective()
	if bind := requireObject(t, effective, api.DNSServer, "lab-dns").Spec().Get("bindAddress").Text(); bind != "192.0.2.1" {
		t.Fatalf("derived DNS bind = %q, want 192.0.2.1", bind)
	}
	for name, want := range map[string]string{"artifact-server-tls": "192.0.2.1", "lab-bmc-tls": "127.0.0.1"} {
		generated := requireObject(t, effective, api.Secret, name).Spec().Get("source", "generated")
		if got := generated.Get("ipAddresses").Strings(); len(got) != 1 || got[0] != want || generated.Has("dnsNames") {
			t.Fatalf("%s derived names = %v, want ipAddresses [%s]", name, generated, want)
		}
	}
	state, _ = compileAcceptance(t, editedExample(t, "lab-rhel", exampleEdit{proxy, "    - name: ip\n      addressRef: ip\n",
		"    - name: ip\n      addressRef: ip\n\n    - name: name\n      addressRef: fqdn\n"}))
	if bind := requireObject(t, state.Effective(), api.Proxy, "lab-proxy").Spec().Get("bindAddress").Text(); bind != "0.0.0.0" {
		t.Fatalf("a proxy whose endpoints name an address and a name binds %q, want the wildcard", bind)
	}
	authored := requireObject(t, state.Effective(), api.Secret, "artifact-server-tls").Spec().Get("source", "generated", "dnsNames").Strings()
	if len(authored) != 1 || authored[0] != "controller.lab.example.test" {
		t.Fatalf("authored names were not kept as the override: %v", authored)
	}
}
