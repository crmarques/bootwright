package substrate

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// targetCatalog holds one provider of each realized arm, a guest on the
// libvirt one, a server on the bare-metal one, and an OS-ready Machine that
// authors a controller without being on any provider at all.
func targetCatalog() api.Catalog {
	environment := obj(api.Environment, "env", m("controller", m("machineRef", "controller")))
	controller := obj(api.Machine, "controller", m("os", m("provided", true)))
	host := obj(api.Machine, "host", m("capabilities", api.StringList("libvirt"), "os", m("provided", true)))
	libvirt := obj(api.InfraProvider, "lab", m("libvirt", m("machineRef", "host", "uri", "qemu:///system",
		"bmcEmulationDefaults", m("bindAddress", "192.0.2.1", "port", api.IntegerValue("8000"),
			"auth", m("credentialsRef", "emulated-bmc")))))
	guest := obj(api.Machine, "guest", m("substrate", m("providerRef", "lab"),
		"os", m("provided", false, "installProfileRef", "rhel")))
	metal := obj(api.InfraProvider, "floor", m("baremetal", m("defaults", m("bmc", m("credentialsRef", "shared")))))
	server := obj(api.Machine, "server", m("substrate", m("providerRef", "floor"),
		"os", m("provided", false, "installProfileRef", "rhel", "install",
			m("hostKeyRef", "server-host-key", "rootDeviceHints", m("deviceName", "/dev/sda"))),
		"hardware", m("nics", list(m("name", "eno1", "macAddress", "AA:BB:CC:DD:EE:01"),
			m("name", "eno2", "macAddress", "aa:bb:cc:dd:ee:02")),
			"management", m("bmc", m("address", "https://bmc.example.test/redfish/v1/Systems/1/",
				"credentialsRef", "server-bmc", "tls", m("verify", false))))))
	standalone := obj(api.Machine, "bastion", m("os", m("provided", true),
		"hardware", m("management", m("bmc", m("address", "https://bastion-bmc.example.test/redfish/v1/Systems/self",
			"credentialsRef", "bastion-bmc")))))
	return api.NewCatalog([]api.Object{environment, controller, host, libvirt, guest, metal, server, standalone})
}

func targetOf(t *testing.T, name string) Target {
	t.Helper()
	catalog := targetCatalog()
	machine, ok := catalog.Find(api.Machine, name)
	if !ok {
		t.Fatalf("fixture has no Machine %q", name)
	}
	target, err := TargetFor(catalog, machine, "lab", "controller")
	if err != nil {
		t.Fatalf("deriving %s: %v", name, diagnostics.Of(err))
	}
	return target
}

// A Machine its substrate creates is reached through the controller that
// realization emulates, proved through the hypervisor's own channel, and acted
// on from the host running it.
func TestAVirtualMachineIsReachedThroughItsEmulatedController(t *testing.T) {
	target := targetOf(t, "guest")
	if target.Substrate != ArmLibvirt || target.Physical {
		t.Fatalf("target = %+v", target)
	}
	if target.Controller.Endpoint != ControllerEndpoint("192.0.2.1", 8000, DomainUUID("lab", "guest")) {
		t.Fatalf("endpoint = %q", target.Controller.Endpoint)
	}
	if target.Controller.CredentialsRef != "emulated-bmc" {
		t.Fatalf("credentials = %q", target.Controller.CredentialsRef)
	}
	if target.Identity.Channel != ChannelGuestAgent {
		t.Fatalf("channel = %q", target.Identity.Channel)
	}
	if target.Identity.Domain != DomainName("lab", "guest") || target.Identity.URI != "qemu:///system" {
		t.Fatalf("identity = %+v", target.Identity)
	}
	if target.PlacementMachine.Name() != "host" {
		t.Fatalf("placement = %q", target.PlacementMachine.Name())
	}
	if len(target.Hardware.Interfaces) != 0 {
		t.Fatal("a machine its substrate creates proves no hardware of its own")
	}
}

// A physical Machine is reached at the controller it authors, from the
// controller Machine, and carries both the hardware it must prove and the key
// its installation will deliver.
func TestAPhysicalMachineIsReachedAtTheControllerItAuthors(t *testing.T) {
	target := targetOf(t, "server")
	if target.Substrate != ArmBaremetal || !target.Physical {
		t.Fatalf("target = %+v", target)
	}
	if target.Controller.Endpoint != "https://bmc.example.test/redfish/v1/Systems/1" {
		t.Fatalf("endpoint = %q", target.Controller.Endpoint)
	}
	if target.Controller.TLSVerify {
		t.Fatal("a declared verification opt-out must reach the consumer")
	}
	if target.Identity.Channel != ChannelDeliveredKey || target.Identity.HostKeyRef != "server-host-key" {
		t.Fatalf("identity = %+v", target.Identity)
	}
	if target.Identity.Domain != "" || target.Identity.URI != "" {
		t.Fatal("a machine with no hypervisor names none")
	}
	if target.Hardware.RootDevice != "/dev/sda" {
		t.Fatalf("root device = %q", target.Hardware.RootDevice)
	}
	if target.PlacementMachine.Name() != "controller" {
		t.Fatalf("placement = %q", target.PlacementMachine.Name())
	}
}

// Every declared address is compared in one spelling, so hardware reporting a
// different case still proves the machine.
func TestDeclaredHardwareAddressesAreComparableInOneSpelling(t *testing.T) {
	interfaces := targetOf(t, "server").Hardware.Interfaces
	if len(interfaces) != 2 {
		t.Fatalf("interfaces = %+v", interfaces)
	}
	for _, declared := range interfaces {
		if declared.MACAddress != "aa:bb:cc:dd:ee:01" && declared.MACAddress != "aa:bb:cc:dd:ee:02" {
			t.Fatalf("address is not canonical: %q", declared.MACAddress)
		}
	}
}

// A Machine no substrate realizes is still reachable when it authors its own
// controller, and needs no realization to have happened first.
func TestAnAuthoredControllerAnswersWithoutAProvider(t *testing.T) {
	target := targetOf(t, "bastion")
	if target.Substrate != "" || !target.Physical {
		t.Fatalf("target = %+v", target)
	}
	if target.Controller.Endpoint != "https://bastion-bmc.example.test/redfish/v1/Systems/self" {
		t.Fatalf("endpoint = %q", target.Controller.Endpoint)
	}
	if !target.Controller.TLSVerify {
		t.Fatal("verification is on unless the declaration opts out")
	}
	if target.Identity.Channel != "" {
		t.Fatal("nothing installs this machine, so it proves no identity")
	}
}

// An address that does not name one exact ComputerSystem is refused. It is
// what a destructive operation is aimed at, so it may never be ambiguous.
func TestAControllerAddressMustNameOneExactSystem(t *testing.T) {
	for name, address := range map[string]string{
		"the collection":  "https://bmc.example.test/redfish/v1/Systems",
		"a relative path": "/redfish/v1/Systems/1",
		"another tree":    "https://bmc.example.test/redfish/v1/Chassis/1",
		"embedded user":   "https://root:secret@bmc.example.test/redfish/v1/Systems/1",
		"a nested child":  "https://bmc.example.test/redfish/v1/Systems/1/EthernetInterfaces",
		"no scheme":       "bmc.example.test/redfish/v1/Systems/1",
		"empty":           "",
	} {
		t.Run(name, func(t *testing.T) {
			if _, ok := NormalizeControllerEndpoint(address); ok {
				t.Fatalf("%q was accepted", address)
			}
		})
	}
	endpoint, ok := NormalizeControllerEndpoint("  https://bmc.example.test:8443/redfish/v1/Systems/System.1/  ")
	if !ok || endpoint != "https://bmc.example.test:8443/redfish/v1/Systems/System.1" {
		t.Fatalf("normalized = %q, ok = %v", endpoint, ok)
	}
}

// One physical machine is one claim, whichever spelling of its endpoint the
// declaration used, so two contexts cannot both drive it.
func TestOneControllerIsOneClaimHoweverItIsSpelled(t *testing.T) {
	for address, key := range map[string]string{
		"https://bmc.example.test/redfish/v1/Systems/1":              "bmc:bmc.example.test:443/1",
		"http://bmc.example.test/redfish/v1/Systems/1":               "bmc:bmc.example.test:80/1",
		"https://bmc.example.test:8443/redfish/v1/Systems/1":         "bmc:bmc.example.test:8443/1",
		"https://[2001:db8::1]/redfish/v1/Systems/System.Embedded.1": "bmc:[2001:db8::1]:443/System.Embedded.1",
	} {
		t.Run(address, func(t *testing.T) {
			if got := ControllerReservationKey(address); got != key {
				t.Fatalf("key = %q, want %q", got, key)
			}
		})
	}
}

// An arm no capability implements refuses before anything registers, and the
// refusal is derived in one place so adding an arm narrows it exactly once.
func TestAnUnrealizedSubstrateRefuses(t *testing.T) {
	vsphere := obj(api.InfraProvider, "vc", m("vsphere", m("vcenters", list(m("server", "vcenter.example.test")))))
	machine := obj(api.Machine, "vm", m("substrate", m("providerRef", "vc"), "os", m("provided", false)))
	catalog := api.NewCatalog([]api.Object{
		obj(api.Environment, "env", m("controller", m("machineRef", "controller"))),
		obj(api.Machine, "controller", m("os", m("provided", true))), vsphere, machine,
	})
	if _, err := TargetFor(catalog, machine, "lab", "controller"); err == nil {
		t.Fatal("an unrealized substrate was accepted")
	}
	unrealizable := Unrealizable(catalog)
	if len(unrealizable) != 2 || unrealizable[0] != "InfraProvider/vc" || unrealizable[1] != "Machine/vm" {
		t.Fatalf("unrealizable = %v", unrealizable)
	}
	if len(Unrealizable(targetCatalog())) != 0 {
		t.Fatal("every arm of the fixture is realized")
	}
}

// A physical machine that declares no hardware to prove itself by is refused
// at derivation, because the proof before an erasure has nothing to compare.
func TestAPhysicalMachineWithoutDeclaredHardwareRefuses(t *testing.T) {
	metal := obj(api.InfraProvider, "floor", m("baremetal", m()))
	machine := obj(api.Machine, "bare", m("substrate", m("providerRef", "floor"),
		"os", m("provided", false, "installProfileRef", "rhel"),
		"hardware", m("management", m("bmc", m("address", "https://b.example.test/redfish/v1/Systems/1",
			"credentialsRef", "bmc")))))
	catalog := api.NewCatalog([]api.Object{
		obj(api.Environment, "env", m("controller", m("machineRef", "controller"))),
		obj(api.Machine, "controller", m("os", m("provided", true))), metal, machine,
	})
	if _, err := TargetFor(catalog, machine, "lab", "controller"); err == nil {
		t.Fatal("a physical machine with no declared NIC was accepted")
	}
}
