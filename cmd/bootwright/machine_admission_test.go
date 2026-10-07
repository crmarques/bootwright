//go:build linux && amd64

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
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
	t.Run("a name past the per-kind limit", func(t *testing.T) {
		name := strings.Repeat("r", 63)
		refusal := onlyRefusal(t, editedExample(t, "lab-rhel", exampleEdit{labGuest, "name: rhel-01", "name: " + name}))
		if refusal.Code != "api.value" || refusal.Field != "$.metadata.name" || refusal.Object == nil || refusal.Object.Name != name ||
			!strings.Contains(refusal.Message, "at most 52 bytes") {
			t.Fatalf("refusal = %#v", refusal)
		}
	})
	t.Run("an invalid name without its contact", func(t *testing.T) {
		refusal := onlyRefusal(t, editedExample(t, "lab-rhel", exampleEdit{labGuest, "name: rhel-01", "name: Rhel_01"},
			exampleEdit{labGuest, "      - name: fqdn\n        address: rhel-01.lab.example.test\n\n", ""}))
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
		{labGuest, "    installProfileRef: rhel-9-8\n\n    install:\n      rootDeviceHints:\n        deviceName: /dev/vda\n\n      ntp:\n        - serverRef: lab-ntp\n          endpointRef: ip\n", ""},
		{labGuest, "    configRef: lab-guests\n    attachmentRef: lab-guests\n    installAddressRef: ip\n\n", ""},
		{labGuest, "\n      - name: ip\n        address: 198.51.100.11/24\n        interface: enp1s0\n", ""},
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
		"its own prefix's network address": {installAt("198.51.100.0/24"), "api.value", guest, "$.spec.network.addresses[1].address"},
		"an authored MAC": {[]exampleEdit{{labGuest, "\n  network:\n", "\n  hardware:\n    nics:\n      - name: enp1s0\n        macAddress: 52:54:00:00:00:01\n\n  network:\n"}},
			"api.invariant", guest, "$.spec.hardware.nics[0].macAddress"},
		"an authored controller": {[]exampleEdit{{labGuest, "\n  network:\n", "\n  hardware:\n    management:\n      bmc:\n        address: https://bmc.example.test/redfish/v1/Systems/1\n        credentialsRef: lab-bmc-credentials\n\n  network:\n"}},
			"api.invariant", guest, "$.spec.hardware.management.bmc"},
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
	refusal := requireRefusal(t, editedExample(t, "lab-baremetal", exampleEdit{metal, "\n    management:\n", "\n    boot:\n      nicRef: eno1\n\n    management:\n"}),
		"api.field", "Machine/metal-01", "$.spec.hardware.boot")
	if !strings.Contains(refusal.Message, "nics, management") {
		t.Fatalf("the boot refusal says %q", refusal.Message)
	}
	compileAcceptance(t, editedExample(t, "lab-baremetal",
		exampleEdit{metal, "    attachmentRef: lab-metal\n", ""},
		exampleEdit{"infra/providers/lab-metal.yaml", "\n  networkAttachments:\n    - name: lab-metal\n      baremetal: {}\n", ""}))
}
