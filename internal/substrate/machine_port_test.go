package substrate

import "testing"

// The port object carries the controller's own access and names the material
// each consumer writes, exactly as the roles derived them before it was frozen.
func TestTheMachinePortFreezesTheControllerAccessAndItsMaterialNames(t *testing.T) {
	for _, row := range []struct {
		arm        string
		controller Controller
		suffix     string
		want       MachinePort
	}{
		{ArmBaremetal, Controller{Endpoint: "https://bmc/redfish/v1/Systems/1", TLSVerify: true, TrustBundleRef: "bmc-ca", TLSVerification: "certificate"}, "Node1",
			MachinePort{Endpoint: "https://bmc/redfish/v1/Systems/1", Verify: true, Verification: "certificate", UserVariable: "controllerUserNode1",
				PasswordVariable: "controllerPasswordNode1", CAVariable: "controllerCANode1", PinnedUUIDVariable: "pinnedUUIDBase64Node1", PinnedSerialVariable: "pinnedSerialBase64Node1"}},
		{ArmLibvirt, Controller{Endpoint: "https://controller:8443/redfish/v1/Systems/u", TLSVerify: true}, "",
			MachinePort{Endpoint: "https://controller:8443/redfish/v1/Systems/u", Verify: true, UserVariable: "controllerUser",
				PasswordVariable: "controllerPassword", PinnedUUIDVariable: "pinnedUUIDBase64", PinnedSerialVariable: "pinnedSerialBase64"}},
		{ArmKubeVirt, Controller{Endpoint: KubeVirtEndpoint("ns", "vm-1"), TLSVerify: true, MediaVolume: KubeVirtMediaVolume("vm-1")}, "0",
			MachinePort{Endpoint: "kubevirt:ns/vm-1", Verify: true, MediaVolume: "vm-1-media", UserVariable: "controllerUser0",
				PinnedUUIDVariable: "pinnedUUIDBase640", PinnedSerialVariable: "pinnedSerialBase640"}},
	} {
		if got := NewMachinePort(row.arm, row.controller, row.suffix); got != row.want {
			t.Errorf("%s port = %+v, want %+v", row.arm, got, row.want)
		}
	}
}
