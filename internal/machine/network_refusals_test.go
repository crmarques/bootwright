package machine

import (
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// An OS-ready Machine is told once to remove a network selection, never also
// to select a configuration it may not declare.
func TestAnOSReadyMachineIsToldOnlyToRemoveASelection(t *testing.T) {
	ready := object(api.Machine, "ready", m("os", m("provided", true), "network", m("installAddressRef", "ip")))
	var found []api.Issue
	for _, issue := range Validate(ready, api.NewCatalog([]api.Object{ready})) {
		if issue.Field == "$.spec.network.installAddressRef" {
			found = append(found, issue)
		}
	}
	if len(found) != 1 || found[0].Message != "OS-ready Machines declare contacts only" {
		t.Fatalf("issues = %+v", found)
	}
}

// Nothing installs an OS-ready Machine, so a root-device hint only a disk
// selector reads warns api.deferred, while deviceName and wwn, which keep the
// root disk out of Ceph OSD selections, do not.
func TestAnOSReadyMachineWarnsOfRootHintsNothingReads(t *testing.T) {
	ready := object(api.Machine, "ready", m("os", m("provided", true),
		"hardware", m("rootDevice", m("deviceName", "/dev/vda", "model", "disk", "vendor", "acme", "minSizeGigabytes", api.IntegerValue("10"), "rotational", false)),
		"substrate", m("baremetal", m("rootDevice", m("wwn", "0x5000c500a1b2c3d4", "hctl", "0:0:0:0", "serialNumber", "s1")))))
	deferred := []string{}
	for _, issue := range Validate(ready, api.NewCatalog([]api.Object{ready})) {
		if issue.Code == "api.deferred" {
			deferred = append(deferred, issue.Field)
		}
	}
	want := []string{"$.spec.substrate.baremetal.rootDevice.hctl", "$.spec.hardware.rootDevice.minSizeGigabytes", "$.spec.hardware.rootDevice.model", "$.spec.hardware.rootDevice.rotational", "$.spec.substrate.baremetal.rootDevice.serialNumber", "$.spec.hardware.rootDevice.vendor"}
	if !slices.Equal(deferred, want) {
		t.Fatalf("deferred = %v, want %v", deferred, want)
	}
}

// twoAttachmentLab is the lab with a second, external attachment storage.
func twoAttachmentLab(interfaces api.Value) []api.Object {
	objects := libvirtLab(interfaces)
	for i, o := range objects {
		if o.Kind() == api.InfraProvider {
			attachments := append(o.Spec().Get("networkAttachments").Items(), m("name", "storage", "libvirt", m("bridge", "br-storage")))
			objects[i] = o.WithSpec(o.Spec().With("networkAttachments", list(attachments...)))
		}
	}
	return objects
}

// A provider's sole attachment is the default whatever its name, for a
// referenced or an inline configuration; the NetworkConfig's name selects
// nothing, and two attachments still need an explicit selection.
func TestAMachineDefaultsToItsProvidersSoleAttachment(t *testing.T) {
	lab := libvirtLab(list(m("name", "enp1s0", "type", "ethernet")))
	for i, o := range lab {
		if o.Kind() == api.NetworkConfig {
			lab[i] = api.NewObject(api.NetworkConfig, "profile", api.MapValue(), o.Spec())
		}
	}
	addresses := list(m("name", "ip", "address", "192.0.2.11/24", "interface", "enp1s0"))
	inline := m("machineNetwork", list(m("cidr", "192.0.2.0/24")), "nmstate", m("interfaces", list(m("name", "enp1s0", "type", "ethernet"))))
	for name, network := range map[string]api.Value{
		"a referenced configuration": m("configRef", "profile", "addresses", addresses),
		"an inline configuration":    m("inline", inline, "addresses", addresses),
	} {
		t.Run(name, func(t *testing.T) {
			machine := guest(network)
			catalog := api.NewCatalog(append([]api.Object{machine}, lab...))
			normalized, _ := Normalize(machine, catalog)
			if got := normalized.Spec().Get("network", "attachmentRef").Text(); got != "net" {
				t.Fatalf("attachmentRef = %q, want the sole attachment net", got)
			}
			if issues := admission(machine, lab...); len(issues) != 0 {
				t.Fatalf("a Machine on its provider's sole attachment was refused: %v", issues)
			}
			selected := machine.WithSpec(machine.Spec().WithPath(list(m("interface", "enp1s0", "attachmentRef", "net")), "substrate", "libvirt", "interfaceAttachments"))
			if normalized, _ := Normalize(selected, catalog); normalized.Spec().Has("network", "attachmentRef") {
				t.Fatal("a default was added beside authored interfaceAttachments")
			}
		})
	}
	machine := guest(m("configRef", "net", "addresses", addresses))
	requireOneIssue(t, admission(machine, twoAttachmentLab(list(m("name", "enp1s0", "type", "ethernet")))...), api.Issue{Code: "api.invariant", Field: "$.spec.network.attachmentRef",
		Message:     "provider-backed configured networks require an explicit attachment unless their provider has exactly one",
		Remediation: "set spec.network.attachmentRef on Machine/guest to an attachment of InfraProvider/lab"})
}
