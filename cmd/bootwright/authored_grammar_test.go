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

// exampleEdit replaces one exact text of one example file.
type exampleEdit struct {
	file, old, new string
}

// editedExample copies an example, applies each edit and reads the copy back
// the way an operator's input directory is read.
func editedExample(t *testing.T, name string, edits ...exampleEdit) desiredstate.Sources {
	t.Helper()
	root := t.TempDir()
	copySources(t, exampleDirectory(t, name), root)
	for _, edit := range edits {
		path := filepath.Join(root, edit.file)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(string(data), edit.old) != 1 {
			t.Fatalf("%s holds %q %d times, want once", edit.file, edit.old, strings.Count(string(data), edit.old))
		}
		if err := os.WriteFile(path, []byte(strings.Replace(string(data), edit.old, edit.new, 1)), 0600); err != nil {
			t.Fatal(err)
		}
	}
	sources, err := (inputfs.Reader{}).Read(context.Background(), []string{root})
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	return sources
}

// requireRefusal compiles sources and requires a diagnostic of code at field on
// the object kind/name, returning it.
func requireRefusal(t *testing.T, sources desiredstate.Sources, code, object, field string) diagnostics.Diagnostic {
	t.Helper()
	state, report, err := wireCompiler().Compile(context.Background(), sources)
	if err == nil || state != nil || report != nil {
		t.Fatalf("the edited example compiled; want %s at %s on %s", code, field, object)
	}
	found := diagnostics.Of(err)
	for _, diagnostic := range found {
		if diagnostic.Code == code && diagnostic.Field == field && diagnostic.Object != nil && diagnostic.Object.Kind+"/"+diagnostic.Object.Name == object {
			return diagnostic
		}
	}
	t.Fatalf("missing %s at %s on %s: %#v", code, field, object, found)
	return diagnostics.Diagnostic{}
}

const labProfile = "infra/os/rhel-9-8.yaml"

// withRepository adds one configured repository with baseURL to the lab
// install profile.
func withRepository(baseURL string) exampleEdit {
	return exampleEdit{labProfile, "\n    services:\n", "\n    repositories:\n      configure:\n        - id: extras\n          baseURL: \"" + baseURL + "\"\n          gpgCheck: false\n\n    services:\n"}
}

// Every authored value that reaches the Kickstart is refused before a plan is
// made when it could end its line, open a section or add an option.
func TestAuthoredKickstartValuesAreRefusedBeforePlan(t *testing.T) {
	compileAcceptance(t, exampleDirectory(t, "lab-rhel"))
	withAddressInterface := exampleEdit{"infra/machines/rhel-01.yaml", "interface: enp1s0", `interface: "enp1s0\nnetwork --bootproto=dhcp"`}
	cases := map[string]struct {
		edits               []exampleEdit
		code, object, field string
	}{
		"a service holding a section": {[]exampleEdit{{labProfile, "        - chronyd\n", "        - \"chronyd\\n%post\\nPROBE\\n%end\"\n"}},
			"api.value", "MachineInstallProfile/rhel-9-8", "$.spec.customizations.services.enabled[0]"},
		"a package closing the section": {[]exampleEdit{{labProfile, "        - chrony\n", "        - chrony\n        - \"%end\"\n"}},
			"api.value", "MachineInstallProfile/rhel-9-8", "$.spec.customizations.packages.install[1]"},
		"a base URL adding an option": {[]exampleEdit{withRepository("https://mirror.example.test/extras --noverifyssl")},
			"api.value", "MachineInstallProfile/rhel-9-8", "$.spec.customizations.repositories.configure[0].baseURL"},
		"a base URL ending the line": {[]exampleEdit{withRepository("https://mirror.example.test/extras#frag")},
			"api.value", "MachineInstallProfile/rhel-9-8", "$.spec.customizations.repositories.configure[0].baseURL"},
		"a next hop holding a section": {[]exampleEdit{{"infra/networkconfigs/lab-guests.yaml", "next-hop-address: 198.51.100.1", `next-hop-address: "198.51.100.1\n%post"`}},
			"api.value", "NetworkConfig/lab-guests", "$.spec.nmstate.routes.config[0].next-hop-address"},
		"an NMState interface holding a directive": {[]exampleEdit{{"infra/networkconfigs/lab-guests.yaml", "- name: enp1s0", `- name: "enp1s0\nnetwork --bootproto=dhcp"`}, withAddressInterface},
			"api.value", "NetworkConfig/lab-guests", "$.spec.nmstate.interfaces[0].name"},
		"an address interface holding a directive": {[]exampleEdit{{"infra/networkconfigs/lab-guests.yaml", "- name: enp1s0", `- name: "enp1s0\nnetwork --bootproto=dhcp"`}, withAddressInterface},
			"api.value", "Machine/rhel-01", "$.spec.network.addresses[1].interface"},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			requireRefusal(t, editedExample(t, "lab-rhel", test.edits...), test.code, test.object, test.field)
		})
	}
}

// labHostedTree is the lab install profile's package source, replaced by a
// mirror in the rows that need one.
const labHostedTree = `        hostedTree:
          fromMedia: local-media:rhel-9.8-x86_64-dvd.iso
          artifactServerEndpoint:
            serverRef: lab-artifacts
            endpointRef: ip-http
`

// Each field that names a Kickstart repository or a Kickstart device carries
// its grammar, not only the rule it shares with others: a mirror's base URL
// and each of its repositories' base URLs refuse a fragment, and a bound
// interface name is a Linux interface name.
func TestEveryKickstartBoundFieldCarriesItsGrammar(t *testing.T) {
	mirror := func(baseURL, repositoryURL string) exampleEdit {
		return exampleEdit{labProfile, labHostedTree, "        mirror:\n          baseURL: \"" + baseURL + "\"\n          repositories:\n            - id: appstream\n              baseURL: \"" + repositoryURL + "\"\n"}
	}
	binding := exampleEdit{"infra/machines/metal-01.yaml", "    installAddressRef: ip\n",
		"    installAddressRef: ip\n\n    interfaceBinding:\n      - nicRef: eno1\n        interfaceName: \"eno1\\nnetwork --bootproto=dhcp\"\n"}
	cases := map[string]struct {
		example             string
		edit                exampleEdit
		code, object, field string
	}{
		"a mirror base URL ending the line": {"lab-rhel", mirror("https://mirror.example.test/os#frag", "https://mirror.example.test/appstream"),
			"api.value", "MachineInstallProfile/rhel-9-8", "$.spec.installer.anaconda.packageSource.mirror.baseURL"},
		"a mirror repository base URL ending the line": {"lab-rhel", mirror("https://mirror.example.test/os", "https://mirror.example.test/appstream#frag"),
			"api.value", "MachineInstallProfile/rhel-9-8", "$.spec.installer.anaconda.packageSource.mirror.repositories[0].baseURL"},
		"a bound interface name holding a directive": {"lab-baremetal", binding,
			"api.value", "Machine/metal-01", "$.spec.network.interfaceBinding[0].interfaceName"},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			requireRefusal(t, editedExample(t, test.example, test.edit), test.code, test.object, test.field)
		})
	}
}

// The emulated BMC mounts only its host's libvirt socket and writes the URI
// into its Python configuration, so the one URI admitted is the local system
// daemon's: no remote or command transport, and no value that could leave its
// string literal.
func TestALibvirtProviderConnectsOnlyToTheLocalSystemDaemon(t *testing.T) {
	for _, uri := range []string{
		`qemu:///system' + __import__('os').system('id') + '`,
		"qemu+ssh://root@hv.example.test/system",
		"qemu+ext:///system?command=/bin/true",
		"qemu:///session",
	} {
		t.Run(uri, func(t *testing.T) {
			edit := exampleEdit{"infra/providers/lab-libvirt.yaml", "uri: qemu:///system", "uri: \"" + uri + "\""}
			requireRefusal(t, editedExample(t, "lab-rhel", edit), "api.value", "InfraProvider/lab-libvirt", "$.spec.libvirt.uri")
		})
	}
	compileAcceptance(t, editedExample(t, "lab-rhel", exampleEdit{"infra/providers/lab-libvirt.yaml", "uri: qemu:///system", `uri: "qemu:///system"`}))
}

// A bare-metal host key that is also the fleet key would let every holder of
// the fleet key answer as this machine; the refusal names both objects.
func TestABaremetalHostKeyIsNotTheFleetKey(t *testing.T) {
	compileAcceptance(t, exampleDirectory(t, "lab-baremetal"))
	edit := exampleEdit{"infra/machines/metal-01.yaml", "hostKeyRef: metal-01-host-key", "hostKeyRef: bootwright-machine-key"}
	refusal := requireRefusal(t, editedExample(t, "lab-baremetal", edit), "api.invariant", "Machine/metal-01", "$.spec.os.install.hostKeyRef")
	if !strings.Contains(refusal.Message, "bootwright-machine-key") || !strings.Contains(refusal.Message, "Environment/lab-baremetal") ||
		!strings.Contains(refusal.Remediation, "Machine/metal-01") {
		t.Fatalf("the refusal does not name both objects: %#v", refusal)
	}
}

// Generation refuses a NUL, so validate refuses it first.
func TestAGeneratedSecretParameterHoldingNULIsRefused(t *testing.T) {
	for _, test := range []struct {
		edit          exampleEdit
		object, field string
	}{
		{exampleEdit{"secret-descriptors/lab-bmc-credentials.yaml", "username: admin", `username: "ad\0min"`},
			"Secret/lab-bmc-credentials", "$.spec.source.generated.username"},
		{exampleEdit{"secret-descriptors/bootwright-machine-key.yaml", "comment: bootwright-machine-key", `comment: "bootwright\0machine-key"`},
			"Secret/bootwright-machine-key", "$.spec.source.generated.comment"},
	} {
		t.Run(test.field, func(t *testing.T) {
			requireRefusal(t, editedExample(t, "lab-rhel", test.edit), "api.invariant", test.object, test.field)
		})
	}
}
