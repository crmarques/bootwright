//go:build linux && amd64

package main

import (
	"strings"
	"testing"
)

const (
	labImage   = "infra/os/rhel-9-8-boot.yaml"
	labMachine = "infra/machines/rhel-01.yaml"
)

// Validate refuses every installation shape the only installation cannot use,
// each on the object and field to change and with the change that fixes it,
// rather than admitting it and refusing at plan.
func TestInstallationShapesTheInstallationCannotUseRefuseAtValidate(t *testing.T) {
	compileAcceptance(t, exampleDirectory(t, "lab-rhel"))
	const (
		bootMedia  = "bootMedia: local-media:rhel-9.8-x86_64-boot.iso"
		treeMedia  = "fromMedia: local-media:rhel-9.8-x86_64-dvd.iso"
		treeServer = "$.spec.installer.anaconda.packageSource.hostedTree.artifactServerEndpoint.serverRef"
	)
	cases := map[string]struct {
		edits               []exampleEdit
		code, object, field string
		message, remedy     string
	}{
		"boot media fetched over https": {[]exampleEdit{{labImage, bootMedia, "bootMedia: https://images.example.test/rhel-9.8-x86_64-boot.iso"}},
			"api.value", "MachineImage/rhel-9-8-boot", "$.spec.bootMedia", "host media store", "bootwright media add"},
		"boot media the store cannot name": {[]exampleEdit{{labImage, bootMedia, "bootMedia: local-media:CON.iso"}},
			"api.value", "MachineImage/rhel-9-8-boot", "$.spec.bootMedia", "host media store", "bootwright media add"},
		"tree media read from a file URI": {[]exampleEdit{{labProfile, treeMedia, "fromMedia: file:///var/lib/media/rhel-9.8-x86_64-dvd.iso"}},
			"api.value", "MachineInstallProfile/rhel-9-8", "$.spec.installer.anaconda.packageSource.hostedTree.fromMedia", "host media store", "bootwright media add"},
		"a profile without its virtual media endpoint": {[]exampleEdit{{labProfile, "      redfishVirtualMedia:\n        artifactServerEndpoint:\n          serverRef: lab-artifacts\n          endpointRef: ip-https\n\n", ""}},
			"api.invariant", "MachineInstallProfile/rhel-9-8", "$.spec.installer.anaconda.redfishVirtualMedia.artifactServerEndpoint", "Redfish virtual media",
			"set spec.installer.anaconda.redfishVirtualMedia.artifactServerEndpoint on MachineInstallProfile/rhel-9-8"},
		"a DHCP-only install network": {[]exampleEdit{
			{"infra/networkconfigs/lab-guests.yaml", "dhcp: false", "dhcp: true"},
			{labMachine, "    installAddressRef: ip\n", ""},
			{labMachine, "\n      - name: ip\n        address: 198.51.100.11/24\n        interface: enp1s0\n", "\n"},
		}, "api.invariant", "Machine/rhel-01", "$.spec.network.installAddressRef", "DHCP installation is not supported", "spec.network.addresses"},
		"an install Machine with no network configuration": {[]exampleEdit{
			{labMachine, "    configRef: lab-guests\n    attachmentRef: lab-guests\n    installAddressRef: ip\n", ""},
			{labMachine, "\n      - name: ip\n        address: 198.51.100.11/24\n        interface: enp1s0\n", "\n"},
		}, "api.invariant", "Machine/rhel-01", "$.spec.network.installAddressRef", "DHCP installation is not supported",
			"select a network configuration with spec.network.configRef or declare one in spec.network.inline, then assign an IPv4 address"},
		"an install address with no network configuration": {[]exampleEdit{
			{labMachine, "    configRef: lab-guests\n    attachmentRef: lab-guests\n", ""},
		}, "api.invariant", "Machine/rhel-01", "$.spec.network.installAddressRef", "DHCP installation is not supported", "spec.network.configRef"},
		"a profile named like the Machine it installs": {[]exampleEdit{{labProfile, "  name: rhel-9-8\n", "  name: rhel-01\n"}, {labMachine, "installProfileRef: rhel-9-8", "installProfileRef: rhel-01"}},
			"api.invariant", "MachineInstallProfile/rhel-01", treeServer, "os/rhel-01/", "rename Machine/rhel-01 or MachineInstallProfile/rhel-01"},
		"an artifact server on the Machine it installs": {[]exampleEdit{{"infra/components/artifact-server.yaml", "machineRef: controller", "machineRef: rhel-01"}},
			"api.invariant", "MachineInstallProfile/rhel-9-8", "$.spec.installer.anaconda.redfishVirtualMedia.artifactServerEndpoint.serverRef",
			"cannot serve until the installation completes", "place ArtifactServer/lab-artifacts on the controller Machine"},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			diagnostic := requireRefusal(t, editedExample(t, "lab-rhel", test.edits...), test.code, test.object, test.field)
			if !strings.Contains(diagnostic.Message, test.message) || !strings.Contains(diagnostic.Remediation, test.remedy) {
				t.Fatalf("refusal = %#v, want a message naming %q and a remediation naming %q", diagnostic, test.message, test.remedy)
			}
		})
	}
}
