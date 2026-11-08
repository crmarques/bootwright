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

const labProvider, labNetwork = "infra/providers/lab-libvirt.yaml", "infra/networks/lab-guests.yaml"

// withUplink gives the lab provider an segment to an existing bridge.
func withUplink(name, bridge string) exampleEdit {
	return exampleEdit{labProvider, "        tpm: {}\n", "        tpm: {}\n\n  segments:\n    - name: " + name + "\n      libvirt:\n        bridge: " + bridge + "\n"}
}

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
// one segment.
func secondLabProvider(segment string) map[string]string {
	return map[string]string{"infra/providers/lab-libvirt-b.yaml": `apiVersion: bootwright.io/v1alpha1
kind: InfraProvider
metadata:
  name: lab-libvirt-b

spec:
  libvirt:
    machineRef: controller
    uri: qemu:///system

    bmcEmulation:
      bindAddress: 192.0.2.1
      port: 9000
      certificateRef: artifact-server-tls
      auth:
        credentialsRef: lab-bmc-credentials

  segments:
` + segment}
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

// Validate refuses every libvirt provider, profile and segment shape the
// host cannot realize, where plan or apply would otherwise fail, each at its
// own field.
func TestLibvirtAdmissionRefusesWhatTheHostCannotRealize(t *testing.T) {
	compileAcceptance(t, exampleDirectory(t, "lab-rhel"))
	const provider, guest, network = "InfraProvider/lab-libvirt", "Machine/rhel-01", "Network/lab-guests"
	cases := map[string]struct {
		edits                        []exampleEdit
		code, object, field, message string
	}{
		"an segment name outside the label grammar": {[]exampleEdit{withUplink("Lab_Uplink", "br-uplink")},
			"api.value", provider, "$.spec.segments[0].name", "value does not match the required name grammar"},
		"a data disk name outside the label grammar": {[]exampleEdit{withDataDisks("Data_1")},
			"api.value", provider, "$.spec.libvirt.machineProfiles[0].dataDisks[0].name", "value does not match the required name grammar"},
		"a data disk named root": {[]exampleEdit{withDataDisks("root")},
			"api.invariant", provider, "$.spec.libvirt.machineProfiles[0].dataDisks[0].name", "the data disk name root is reserved for the root disk, which the domain presents as vda from root.qcow2"},
		"eight data disks": {[]exampleEdit{withDataDisks("d1", "d2", "d3", "d4", "d5", "d6", "d7", "d8")},
			"api.invariant", provider, "$.spec.libvirt.machineProfiles[0].dataDisks", "a libvirt domain presents at most 7 data disks beside its root disk"},
		"a vCPU count past int64": {[]exampleEdit{{labProvider, "cpu: 4", "cpu: 100000000000000000000"}},
			"api.value", provider, "$.spec.libvirt.machineProfiles[0].cpu", "number must be between 0 and 1024"},
		"a root disk past its ceiling": {[]exampleEdit{{labProvider, "diskGiB: 60", "diskGiB: 3000000000000"}},
			"api.value", provider, "$.spec.libvirt.machineProfiles[0].diskGiB", "number must be between 0 and 65536"},
		"a memory size past its ceiling": {[]exampleEdit{{labProvider, "memoryMiB: 8192", "memoryMiB: 20000000"}},
			"api.value", provider, "$.spec.libvirt.machineProfiles[0].memoryMiB", "number must be between 0 and 16777216"},
		"a data disk past its ceiling": {[]exampleEdit{withSizedDataDisks("70000", "data")},
			"api.value", provider, "$.spec.libvirt.machineProfiles[0].dataDisks[0].sizeGiB", "number must be between 1 and 65536"},
		"a bridge holding a quote": {[]exampleEdit{{labNetwork, "bridge: virbr-lab", `bridge: 'virbr-lab"x'`}},
			"api.value", network, "$.spec.libvirt.bridge", "value does not match the required bridge grammar"},
		"a bridge longer than an interface name": {[]exampleEdit{{labNetwork, "bridge: virbr-lab", "bridge: virbr-lab-guests1"}},
			"api.value", network, "$.spec.libvirt.bridge", "value does not match the required bridge grammar"},
		"an segment bridge holding a quote": {[]exampleEdit{withUplink("uplink", `'br-uplink"x'`)},
			"api.value", provider, "$.spec.segments[0].libvirt.bridge", "value does not match the required bridge grammar"},
		"an interface match holding a quote": {[]exampleEdit{{"infra/machines/rhel-01.yaml", "    installAddressRef: ip\n", "    installAddressRef: ip\n    interfaceMatches:\n      - interface: 'enp1s0\"x'\n        nicRef: enp1s0\n"}},
			"api.value", guest, "$.spec.network.interfaceMatches[0].interface", "value does not match the required ifname grammar"},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			refusal := requireRefusal(t, editedExample(t, "lab-rhel", test.edits...), test.code, test.object, test.field)
			if test.message != "" && refusal.Message != test.message {
				t.Fatalf("the refusal says %q, want %q", refusal.Message, test.message)
			}
		})
	}
	t.Run("a managed Network named like an segment of its provider", func(t *testing.T) {
		refusal := requireRefusal(t, editedExample(t, "lab-rhel", withUplink("lab-guests", "br-uplink")), "api.invariant", network, "$.spec.libvirt.providerRef")
		if !strings.Contains(refusal.Message, "spec.segments[0] of "+provider+" is also named lab-guests") {
			t.Fatalf("the refusal says %q", refusal.Message)
		}
	})
	t.Run("a managed bridge another provider of its host names", func(t *testing.T) {
		sources := extendedExample(t, "lab-rhel", secondLabProvider("    - name: storage\n      libvirt:\n        bridge: virbr-lab\n"))
		refusal := requireRefusal(t, sources, "api.invariant", network, "$.spec.libvirt.bridge")
		if !strings.Contains(refusal.Message, "spec.segments[0] of InfraProvider/lab-libvirt-b on host Machine/controller") {
			t.Fatalf("the refusal says %q", refusal.Message)
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
