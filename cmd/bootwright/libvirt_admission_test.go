//go:build linux && amd64

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/inputfs"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

const labProvider = "infra/providers/lab-libvirt.yaml"

// extendedExample is editedExample with whole files added beside the edits.
func extendedExample(t *testing.T, name string, files map[string]string, edits ...exampleEdit) desiredstate.Sources {
	t.Helper()
	root := editedExample(t, name, edits...).Roots[0]
	for file, content := range files {
		path := filepath.Join(root, file)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	sources, err := (inputfs.Reader{}).Read(context.Background(), []string{root})
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	return sources
}

// secondLabProvider is a second libvirt provider on the lab controller with
// one attachment.
func secondLabProvider(attachment string) map[string]string {
	return map[string]string{"infra/providers/lab-libvirt-b.yaml": `apiVersion: bootwright.io/v1alpha1
kind: InfraProvider
metadata:
  name: lab-libvirt-b

spec:
  libvirt:
    machineRef: controller
    uri: qemu:///system

    bmcEmulationDefaults:
      bindAddress: 192.0.2.1
      port: 9000
      auth:
        credentialsRef: lab-bmc-credentials

  networkAttachments:
` + attachment}
}

func withDataDisks(names ...string) exampleEdit {
	return withSizedDataDisks("10", names...)
}

func withSizedDataDisks(size string, names ...string) exampleEdit {
	disks := "        dataDisks:\n"
	for _, name := range names {
		disks += "          - name: " + name + "\n            sizeGiB: " + size + "\n"
	}
	return exampleEdit{labProvider, "        tpm: {}\n", disks + "        tpm: {}\n"}
}

// Validate refuses every libvirt provider, profile and attachment shape the
// host cannot realize, where plan or apply would otherwise fail, each at its
// own field.
func TestLibvirtAdmissionRefusesWhatTheHostCannotRealize(t *testing.T) {
	compileAcceptance(t, exampleDirectory(t, "lab-rhel"))
	const provider, guest = "InfraProvider/lab-libvirt", "Machine/rhel-01"
	cases := map[string]struct {
		edits                        []exampleEdit
		code, object, field, message string
	}{
		"an attachment name outside the label grammar": {[]exampleEdit{{labProvider, "- name: lab-guests", "- name: Lab_Guests"}, {"infra/machines/rhel-01.yaml", "attachmentRef: lab-guests", "attachmentRef: Lab_Guests"}},
			"api.value", provider, "$.spec.networkAttachments[0].name", "value does not match the required name grammar"},
		"a data disk name outside the label grammar": {[]exampleEdit{withDataDisks("Data_1")},
			"api.value", provider, "$.spec.libvirt.machineProfiles[0].dataDisks[0].name", "value does not match the required name grammar"},
		"a data disk named root": {[]exampleEdit{withDataDisks("root")},
			"api.invariant", provider, "$.spec.libvirt.machineProfiles[0].dataDisks[0].name", "the data disk name root is reserved for the root disk, which the domain presents as vda from root.qcow2"},
		"eight data disks": {[]exampleEdit{withDataDisks("d1", "d2", "d3", "d4", "d5", "d6", "d7", "d8")},
			"api.invariant", provider, "$.spec.libvirt.machineProfiles[0].dataDisks", "a libvirt domain presents at most 7 data disks, vdb through vdh, after its root disk"},
		"a vCPU count past int64": {[]exampleEdit{{labProvider, "cpu: 4", "cpu: 100000000000000000000"}},
			"api.value", provider, "$.spec.libvirt.machineProfiles[0].cpu", "number must be between 0 and 1024"},
		"a root disk past its ceiling": {[]exampleEdit{{labProvider, "diskGiB: 60", "diskGiB: 3000000000000"}},
			"api.value", provider, "$.spec.libvirt.machineProfiles[0].diskGiB", "number must be between 0 and 65536"},
		"a memory size past its ceiling": {[]exampleEdit{{labProvider, "memoryMiB: 8192", "memoryMiB: 20000000"}},
			"api.value", provider, "$.spec.libvirt.machineProfiles[0].memoryMiB", "number must be between 0 and 16777216"},
		"a data disk past its ceiling": {[]exampleEdit{withSizedDataDisks("70000", "data")},
			"api.value", provider, "$.spec.libvirt.machineProfiles[0].dataDisks[0].sizeGiB", "number must be between 1 and 65536"},
		"a bridge holding a quote": {[]exampleEdit{{labProvider, "bridge: virbr-lab", `bridge: 'virbr-lab"x'`}},
			"api.value", provider, "$.spec.networkAttachments[0].libvirt.bridge", "value does not match the required bridge grammar"},
		"a bridge longer than an interface name": {[]exampleEdit{{labProvider, "bridge: virbr-lab", "bridge: virbr-lab-guests1"}},
			"api.value", provider, "$.spec.networkAttachments[0].libvirt.bridge", "value does not match the required bridge grammar"},
		"an attachment interface holding a quote": {[]exampleEdit{{"infra/machines/rhel-01.yaml", "    attachmentRef: lab-guests\n", "    interfaceAttachments:\n      - interface: 'enp1s0\"x'\n        attachmentRef: lab-guests\n"}},
			"api.value", guest, "$.spec.network.interfaceAttachments[0].interface", "value does not match the required ifname grammar"},
		"the retired certificate opt-out": {[]exampleEdit{{labProvider, "        credentialsRef: lab-bmc-credentials\n", "        credentialsRef: lab-bmc-credentials\n      disableCertificateVerification: false\n"}},
			"api.field", provider, "$.spec.libvirt.bmcEmulationDefaults.disableCertificateVerification", ""},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			refusal := requireRefusal(t, editedExample(t, "lab-rhel", test.edits...), test.code, test.object, test.field)
			if test.message != "" && refusal.Message != test.message {
				t.Fatalf("the refusal says %q, want %q", refusal.Message, test.message)
			}
		})
	}
	t.Run("a managed attachment name on two providers of one host", func(t *testing.T) {
		sources := extendedExample(t, "lab-rhel", secondLabProvider("    - name: lab-guests\n      libvirt:\n        bridge: virbr-labb\n        management: managed\n        address: 203.0.113.1/24\n"))
		for object, peer := range map[string]string{provider: "InfraProvider/lab-libvirt-b", "InfraProvider/lab-libvirt-b": provider} {
			refusal := requireRefusal(t, sources, "api.invariant", object, "$.spec.networkAttachments[0].name")
			if !strings.Contains(refusal.Message, "managed attachment of "+peer+" on host Machine/controller") {
				t.Fatalf("%s's refusal says %q", object, refusal.Message)
			}
		}
	})
	t.Run("a managed bridge another provider of its host names", func(t *testing.T) {
		sources := extendedExample(t, "lab-rhel", secondLabProvider("    - name: storage\n      libvirt:\n        bridge: virbr-lab\n"))
		for object, peer := range map[string]string{provider: "InfraProvider/lab-libvirt-b", "InfraProvider/lab-libvirt-b": provider} {
			refusal := requireRefusal(t, sources, "api.invariant", object, "$.spec.networkAttachments[0].libvirt.bridge")
			if !strings.Contains(refusal.Message, "networkAttachments[0] of "+peer+" on host Machine/controller") {
				t.Fatalf("%s's refusal says %q", object, refusal.Message)
			}
		}
	})
	t.Run("an OS-ready Machine naming the provider claims no port", func(t *testing.T) {
		compileAcceptance(t, extendedExample(t, "lab-rhel", map[string]string{"infra/machines/ready-01.yaml": `apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata:
  name: ready-01

spec:
  substrate:
    providerRef: lab-libvirt

  os:
    provided: true

  network:
    addresses:
      - name: fqdn
        address: ready-01.lab.example.test
`}, exampleEdit{labProvider, "port: 8000", "port: 65535"}))
	})
}

// The bare-metal attachment configures nothing, so a VLAN it once carried is an
// unknown field rather than a selection nothing applies.
func TestTheBareMetalAttachmentCarriesNoVLAN(t *testing.T) {
	sources := editedExample(t, "lab-baremetal", exampleEdit{"infra/providers/lab-metal.yaml", "      baremetal: {}\n", "      baremetal:\n        vlan: 0\n"})
	refusal := requireRefusal(t, sources, "api.field", "InfraProvider/lab-metal", "$.spec.networkAttachments[0].baremetal.vlan")
	if !strings.Contains(refusal.Message, "permits no fields") {
		t.Fatalf("the VLAN refusal says %q", refusal.Message)
	}
}
