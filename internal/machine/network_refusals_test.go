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
