package libvirt

import (
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate"
)

func expectRefusal(t *testing.T, err error, code string) {
	t.Helper()
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != code {
		t.Fatalf("refusal = %#v, want %s", reported, code)
	}
}

func TestHostRequestFreezesTheNetworksAndPoolItOwns(t *testing.T) {
	requests, err := HostRequests(labCatalog(), "controller", testContext)
	if err != nil || len(requests) != 1 {
		t.Fatalf("requests = %d (%v)", len(requests), err)
	}
	request := requests[0]
	if request.Identity.Block != "substrate-host-lab-libvirt" || request.URI != "qemu:///system" {
		t.Fatalf("request = %+v", request)
	}
	if request.PoolName != "bootwright-lab-lab-libvirt-vmedia" || request.PoolPath != "/var/lib/libvirt/images/bootwright/lab/lab-libvirt/vmedia" {
		t.Fatalf("pool = %q at %q", request.PoolName, request.PoolPath)
	}
	if len(request.Networks) != 1 {
		t.Fatalf("networks = %+v", request.Networks)
	}
	network := request.Networks[0]
	if !network.Managed || network.Name != "bootwright-lab-lab-guests" || network.Bridge != "virbr-lab" {
		t.Fatalf("network = %+v", network)
	}
	if network.Address != "198.51.100.1/24" || network.Forward != "nat" {
		t.Fatalf("managed network lost its address or forwarding: %+v", network)
	}
	if request.Placement.Connection != "local" || request.Placement.Machine != "controller" {
		t.Fatalf("placement = %+v", request.Placement)
	}
	if request.Provisioned {
		t.Fatal("a provider hosted on the controller installs the closure the controller stage installs")
	}
}

// An external attachment names a bridge this context proves, never one it
// defines, so the frozen entry carries no address or forwarding to apply.
func TestAnExternalAttachmentFreezesNoManagedNetworkFields(t *testing.T) {
	external := provider(field("networkAttachments", api.ListValue(api.MapValue(
		text("name", "lab-guests"),
		field("libvirt", api.MapValue(text("bridge", "br0"), text("management", "external"))),
	))))
	requests, err := HostRequests(catalogOf(controller(), external, networkConfig()), "controller", testContext)
	if err != nil || len(requests) != 1 {
		t.Fatalf("requests = %d (%v)", len(requests), err)
	}
	network := requests[0].Networks[0]
	if network.Managed || network.Address != "" || network.Forward != "" {
		t.Fatalf("external network = %+v", network)
	}
	keys := requests[0].ReservationKeys()
	if slices.ContainsFunc(keys, func(key string) bool {
		return strings.HasPrefix(key, "bridge:") || strings.HasPrefix(key, "libvirt-network:") || strings.HasPrefix(key, "prefix:")
	}) {
		t.Fatalf("an external attachment claimed a host resource: %v", keys)
	}
}

// A domain wired to a bridge name starts whether or not the network that owns
// the bridge is up, and a host that restarts brings neither back on its own.
// Naming the network instead hands libvirt the dependency.
func TestAManagedAttachmentWiresTheDomainToItsLibvirtNetwork(t *testing.T) {
	requests, err := MachineRequests(labCatalog(), "controller", testContext)
	if err != nil || len(requests) == 0 {
		t.Fatalf("requests = %d (%v)", len(requests), err)
	}
	wired := requests[0].Interfaces[0]
	if wired.Network != "bootwright-lab-lab-guests" || wired.Bridge != "virbr-lab" {
		t.Fatalf("managed interface = %+v", wired)
	}
	external := provider(field("networkAttachments", api.ListValue(api.MapValue(
		text("name", "lab-guests"),
		field("libvirt", api.MapValue(text("bridge", "br0"), text("management", "external"))),
	))))
	requests, err = MachineRequests(catalogOf(controller(), external, networkConfig(), guest("rhel-01")), "controller", testContext)
	if err != nil || len(requests) == 0 {
		t.Fatalf("external requests = %d (%v)", len(requests), err)
	}
	if named := requests[0].Interfaces[0]; named.Network != "" || named.Bridge != "br0" {
		t.Fatalf("external interface = %+v", named)
	}
}

func TestHostReservationsClaimEveryManagedNetworkAndThePool(t *testing.T) {
	requests, _ := HostRequests(labCatalog(), "controller", testContext)
	want := []string{"bridge:virbr-lab", "libvirt-network:bootwright-lab-lab-guests", "libvirt-pool:bootwright-lab-lab-libvirt-vmedia",
		"path:/var/lib/libvirt/images/bootwright/lab/lab-libvirt/vmedia", "prefix:198.51.100.0/24"}
	if !slices.Equal(requests[0].ReservationKeys(), want) {
		t.Fatalf("keys = %v", requests[0].ReservationKeys())
	}
}

// The provider host is reached through the same placement arms a managed
// service uses, so a remote host must author the SSH access the operation binds.
func TestARemoteProviderHostUsesItsOwnAuthoredAccess(t *testing.T) {
	remote := provider(field("libvirt", provider().Spec().Get("libvirt").With("machineRef", api.StringValue("hypervisor"))))
	requests, err := HostRequests(catalogOf(controller(), remoteHost(), remote, networkConfig()), "controller", testContext)
	if err != nil || len(requests) != 1 {
		t.Fatalf("requests = %d (%v)", len(requests), err)
	}
	placement := requests[0].Placement
	if placement.Connection != "ssh" || placement.Machine != "hypervisor" || placement.Address != "192.0.2.5" || placement.User != "root" {
		t.Fatalf("placement = %+v", placement)
	}
	if !requests[0].Provisioned {
		t.Fatal("a provider host reached over SSH does not install its own closure")
	}
	if !slices.Equal(requests[0].SecretReferences(), []string{"host-key", "hypervisor-key"}) {
		t.Fatalf("secrets = %v", requests[0].SecretReferences())
	}
}

func TestMachineRequestDerivesTheDomainDisksAndController(t *testing.T) {
	requests, err := MachineRequests(labCatalog(), "controller", testContext)
	if err != nil || len(requests) != 1 {
		t.Fatalf("requests = %d (%v)", len(requests), err)
	}
	request := requests[0]
	if request.Domain != "bootwright-lab-rhel-01" || request.UUID != substrate.DomainUUID(testContext, "rhel-01") {
		t.Fatalf("domain = %q uuid = %q", request.Domain, request.UUID)
	}
	if request.VCPU != 4 || request.MemoryMiB != 8192 || !request.TPM {
		t.Fatalf("profile = %+v", request)
	}
	if len(request.Disks) != 1 || request.Disks[0].SizeGiB != 60 || request.Disks[0].Target != "vda" {
		t.Fatalf("disks = %+v", request.Disks)
	}
	if request.Disks[0].Path != "/var/lib/libvirt/images/bootwright/lab/rhel-01/root.qcow2" {
		t.Fatalf("root disk = %q", request.Disks[0].Path)
	}
	if len(request.Interfaces) != 1 || request.Interfaces[0].Bridge != "virbr-lab" || request.Interfaces[0].Name != "enp1s0" {
		t.Fatalf("interfaces = %+v", request.Interfaces)
	}
	if !strings.HasPrefix(request.Interfaces[0].MACAddress, "52:54:00:") {
		t.Fatalf("mac = %q", request.Interfaces[0].MACAddress)
	}
	controller := request.Controller
	if controller.Port != 8000 || controller.Address != "192.0.2.1" || controller.Unit != "bootwright-lab-bmc-rhel-01" {
		t.Fatalf("controller = %+v", controller)
	}
	// The controller uploads inserted media into the provider's own pool, so
	// the request names that pool as well as its path.
	if request.PoolName != "bootwright-lab-lab-libvirt-vmedia" || request.PoolPath != "/var/lib/libvirt/images/bootwright/lab/lab-libvirt/vmedia" {
		t.Fatalf("pool = %q at %q", request.PoolName, request.PoolPath)
	}
	if controller.Endpoint != "http://192.0.2.1:8000/redfish/v1/Systems/"+request.UUID {
		t.Fatalf("endpoint = %q", controller.Endpoint)
	}
	if !strings.Contains(controller.Image, "@sha256:") {
		t.Fatalf("emulator image is not digest pinned: %q", controller.Image)
	}
}

// Every Machine gets its own controller, allocated from the provider's base
// port in the provider's own canonical order, so two guests never share one.
// A consumer that has to name this Machine's hardware to an installer reads
// the realized target, so what the domain presents and what that target
// reports are one derivation rather than two that can drift apart.
func TestTheDomainPresentsTheInterfacesTheRealizedTargetReports(t *testing.T) {
	catalog := labCatalog()
	requests, err := MachineRequests(catalog, "controller", testContext)
	if err != nil || len(requests) != 1 {
		t.Fatalf("requests = %d (%v)", len(requests), err)
	}
	machine, ok := catalog.Find(api.Machine, "rhel-01")
	if !ok {
		t.Fatal("fixture has no installed Machine")
	}
	target, err := substrate.TargetFor(catalog, machine, testContext, "controller")
	if err != nil {
		t.Fatalf("deriving the target: %v", err)
	}
	if len(target.Interfaces) != len(requests[0].Interfaces) {
		t.Fatalf("target interfaces = %+v, domain interfaces = %+v", target.Interfaces, requests[0].Interfaces)
	}
	for index, presented := range requests[0].Interfaces {
		reported := target.Interfaces[index]
		if reported.Name != presented.Name || reported.MACAddress != presented.MACAddress {
			t.Fatalf("interface %d: target %+v, domain %+v", index, reported, presented)
		}
	}
	if target.RootDeviceHints.DeviceName != "/dev/vda" {
		t.Fatalf("root device = %q", target.RootDeviceHints.DeviceName)
	}
}

func TestEachMachineReceivesItsOwnControllerPort(t *testing.T) {
	catalog := catalogOf(controller(), provider(), networkConfig(), guest("rhel-01"), guest("rhel-02"), guest("rhel-03"))
	requests, err := MachineRequests(catalog, "controller", testContext)
	if err != nil || len(requests) != 3 {
		t.Fatalf("requests = %d (%v)", len(requests), err)
	}
	ports := map[string]int{}
	for _, request := range requests {
		ports[request.Identity.Object] = request.Controller.Port
	}
	if ports["rhel-01"] != 8000 || ports["rhel-02"] != 8001 || ports["rhel-03"] != 8002 {
		t.Fatalf("ports = %v", ports)
	}
}

func TestMachineReservationsClaimItsDomainUnitSocketAndDisks(t *testing.T) {
	requests, _ := MachineRequests(labCatalog(), "controller", testContext)
	want := []string{
		"libvirt-domain:bootwright-lab-rhel-01",
		"path:/var/lib/libvirt/images/bootwright/lab/rhel-01",
		"socket:192.0.2.1:8000",
		"unit:bootwright-lab-bmc-rhel-01",
	}
	if !slices.Equal(requests[0].ReservationKeys(), want) {
		t.Fatalf("keys = %v", requests[0].ReservationKeys())
	}
}

// An IPv6 emulated BMC is reached at its bracketed endpoint and printed so in
// the plan, while its listener claims the one unbracketed socket key a managed
// service on the same address and port claims, so the two conflict.
func TestAnIPv6ControllerIsBracketedAndClaimsTheSharedSocketKey(t *testing.T) {
	defaults := provider().Spec().Get("libvirt", "bmcEmulationDefaults").With("bindAddress", api.StringValue("fd00::1"))
	listener := provider(field("libvirt", provider().Spec().Get("libvirt").With("bmcEmulationDefaults", defaults)))
	requests, err := MachineRequests(catalogOf(controller(), listener, networkConfig(), guest("rhel-01")), "controller", testContext)
	if err != nil || len(requests) != 1 {
		t.Fatalf("requests = %d (%v)", len(requests), err)
	}
	emulated := requests[0].Controller
	if emulated.Address != "fd00::1" || emulated.Endpoint != "http://[fd00::1]:8000/redfish/v1/Systems/"+requests[0].UUID {
		t.Fatalf("controller = %+v", emulated)
	}
	if impacts := machineImpacts(reconciliation.Apply, requests[0]); !slices.Contains(impacts, "open-listener [fd00::1]:8000") {
		t.Fatalf("impacts = %v", impacts)
	}
	service := managedservice.SocketKeys("fd00::1", 8000, nil)
	if !slices.Equal(service, []string{"socket:fd00::1:8000"}) || !slices.Contains(requests[0].ReservationKeys(), service[0]) {
		t.Fatalf("machine keys = %v, managed service keys = %v", requests[0].ReservationKeys(), service)
	}
	held := []prerequisites.HostReservation{{Context: "other", Kind: "proxy", Service: "proxy",
		Keys: managedservice.ReservationKeys("bootwright-other-proxy", "/var/lib/bootwright-services/other/proxy", service)}}
	wanted := []prerequisites.HostReservation{{Context: testContext, Kind: "substrate-machine", Service: "rhel-01", Keys: requests[0].ReservationKeys()}}
	conflicts := prerequisites.Conflicts(held, wanted)
	if len(conflicts) != 1 || conflicts[0].Held.Context != "other" || conflicts[0].HeldKey != service[0] || conflicts[0].WantedKey != service[0] {
		t.Fatalf("conflicts = %+v", conflicts)
	}
}

// Two contexts' managed networks on one host whose prefixes overlap would each
// route that prefix to their own bridge, so the second refuses on the prefix
// even though its bridge, network, pool and path all differ.
func TestTwoContextsOverlappingManagedPrefixesConflict(t *testing.T) {
	hostOf := func(contextName, bridge string) HostRequest {
		t.Helper()
		attached := provider(field("networkAttachments", api.ListValue(api.MapValue(
			text("name", "lab-guests"),
			field("libvirt", api.MapValue(text("bridge", bridge), text("management", "managed"), text("address", "198.51.100.1/24"), text("forward", "nat"))),
		))))
		requests, err := HostRequests(catalogOf(controller(), attached, networkConfig(), guest("rhel-01")), "controller", contextName)
		if err != nil || len(requests) != 1 {
			t.Fatalf("requests = %d (%v)", len(requests), err)
		}
		return requests[0]
	}
	held := []prerequisites.HostReservation{{Context: "other", Kind: "substrate-host", Service: "lab-libvirt", Keys: hostOf("other", "virbr-a").ReservationKeys()}}
	wanted := []prerequisites.HostReservation{{Context: "lab", Kind: "substrate-host", Service: "lab-libvirt", Keys: hostOf("lab", "virbr-b").ReservationKeys()}}
	conflicts := prerequisites.Conflicts(held, wanted)
	if len(conflicts) != 1 || conflicts[0].HeldKey != "prefix:198.51.100.0/24" || conflicts[0].WantedKey != "prefix:198.51.100.0/24" {
		t.Fatalf("conflicts = %+v, want exactly the one prefix", conflicts)
	}
}

// A data disk becomes its own image at its own target, so a replay compares the
// same devices rather than a set that shifted.
func TestDataDisksBecomeTheirOwnImagesInTargetOrder(t *testing.T) {
	withData := provider(field("libvirt", provider().Spec().Get("libvirt").With("machineProfiles", api.ListValue(api.MapValue(
		text("name", "rhel"), number("cpu", "4"), number("memoryMiB", "8192"), number("diskGiB", "60"),
		field("dataDisks", api.ListValue(
			api.MapValue(text("name", "data"), number("sizeGiB", "20")),
			api.MapValue(text("name", "logs"), number("sizeGiB", "10")),
		)),
	)))))
	requests, err := MachineRequests(catalogOf(controller(), withData, networkConfig(), guest("rhel-01")), "controller", testContext)
	if err != nil {
		t.Fatal(err)
	}
	targets := []string{}
	for _, disk := range requests[0].Disks {
		targets = append(targets, disk.Name+"="+disk.Target)
	}
	if !slices.Equal(targets, []string{"root=vda", "data=vdb", "logs=vdc"}) {
		t.Fatalf("disks = %v", targets)
	}
	if requests[0].TPM {
		t.Fatal("a profile without tpm requested an emulated TPM")
	}
}

// A provided Machine is never realized, whatever provider it names.
func TestProvidedMachinesAreNeverRealized(t *testing.T) {
	provided := guest("ready", field("os", api.MapValue(field("provided", api.BoolValue(true)))))
	requests, err := MachineRequests(catalogOf(controller(), provider(), networkConfig(), provided), "controller", testContext)
	if err != nil || len(requests) != 0 {
		t.Fatalf("requests = %d (%v)", len(requests), err)
	}
}

func TestSelectionRefusesWhatItCannotDerive(t *testing.T) {
	noProfile := guest("rhel-01", field("substrate", api.MapValue(text("providerRef", "lab-libvirt"), text("profileRef", "absent"))))
	noAttachment := guest("rhel-01", field("network", guest("rhel-01").Spec().Get("network").With("attachmentRef", api.StringValue("absent"))))
	for name, test := range map[string]struct {
		catalog api.Catalog
		code    string
	}{
		"missing profile":    {catalogOf(controller(), provider(), networkConfig(), noProfile), "api.reference"},
		"missing attachment": {catalogOf(controller(), provider(), networkConfig(), noAttachment), "api.reference"},
		"missing network":    {catalogOf(controller(), provider(), guest("rhel-01")), "api.reference"},
		"missing host":       {catalogOf(provider(), networkConfig(), guest("rhel-01")), "api.reference"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := MachineRequests(test.catalog, "controller", testContext)
			expectRefusal(t, err, test.code)
		})
	}
}

// Every object this contract cannot realize is named before registration, so an
// operation never applies part of a graph it only half supports.
func TestUnsupportedNamesEveryObjectThisContractCannotRealize(t *testing.T) {
	// Bare metal is realized by its own implementation, so the refusal covers
	// only the arms no capability implements and every Machine hosted on one.
	vsphere := api.NewObject(api.InfraProvider, "vc", api.Value{}, api.MapValue(field("vsphere", api.MapValue())))
	hosted := api.NewObject(api.Machine, "node-01", api.Value{}, api.MapValue(
		field("substrate", api.MapValue(text("providerRef", "vc"))),
		field("os", api.MapValue(field("provided", api.BoolValue(false)))),
	))
	metal := api.NewObject(api.InfraProvider, "rack", api.Value{}, api.MapValue(field("baremetal", api.MapValue())))
	unsupported := Unsupported(catalogOf(controller(), provider(), networkConfig(), guest("rhel-01"), vsphere, hosted, metal))
	if !slices.Equal(unsupported, []string{"InfraProvider/vc", "Machine/node-01"}) {
		t.Fatalf("unsupported = %v", unsupported)
	}
	// Each refusal says why and what to change, the Machine's naming the
	// provider that makes it unrealizable.
	var reasons []string
	for _, refused := range Refusals(catalogOf(controller(), provider(), networkConfig(), guest("rhel-01"), vsphere, hosted, metal)) {
		reasons = append(reasons, refused.Kind+"/"+refused.Name+": "+refused.Reason+"; "+refused.Remediation)
	}
	want := []string{
		"InfraProvider/vc: this executable realizes no provider on the substrate it declares; " +
			"remove InfraProvider/vc and the Machines it hosts from the selected Environment, or declare them on a baremetal or libvirt provider",
		"Machine/node-01: its provider InfraProvider/vc is on a substrate this executable does not realize; " +
			"host Machine/node-01 on a baremetal or libvirt provider, declare its operating system provided, or remove it from the selected Environment",
	}
	if !slices.Equal(reasons, want) {
		t.Fatalf("refusals =\n%q\nwant\n%q", reasons, want)
	}
}

// A controller that binds a wildcard gives its guests no endpoint a consumer
// can name, so the provider refuses rather than realizing an unreachable BMC.
// Selection agrees with admission over every address, for state that bypassed
// it, and an absent address or a second spelling refuses here too.
func TestAWildcardControllerAddressRefuses(t *testing.T) {
	for address, nameable := range map[string]bool{
		"": false, "0.0.0.0": false, "::": false, "::0": false, "0:0:0:0:0:0:0:0": false,
		"::ffff:0.0.0.0": false, "::ffff:192.0.2.1": false, "fe80::1": false, "fe80::1%eth0": false,
		"fd00::1%eth0": false, "ff02::1": false, "224.0.0.1": false, "255.255.255.255": false, "2001:DB8::1": false,
		"192.0.2.1": true, "127.0.0.1": true, "169.254.1.1": true, "2001:db8::1": true, "::1": true,
	} {
		defaults := provider().Spec().Get("libvirt", "bmcEmulationDefaults").With("bindAddress", api.StringValue(address))
		listener := provider(field("libvirt", provider().Spec().Get("libvirt").With("bmcEmulationDefaults", defaults)))
		if unsupported := Unsupported(catalogOf(controller(), listener)); slices.Contains(unsupported, "InfraProvider/lab-libvirt") == nameable {
			t.Fatalf("%q: nameable = %v, unsupported = %v", address, nameable, unsupported)
		}
	}
	defaults := provider().Spec().Get("libvirt", "bmcEmulationDefaults").With("bindAddress", api.StringValue("0.0.0.0"))
	listener := provider(field("libvirt", provider().Spec().Get("libvirt").With("bmcEmulationDefaults", defaults)))
	want := []lifecycle.Refusal{{
		Kind: "InfraProvider", Name: "lab-libvirt",
		Reason:      "an emulated BMC listens on one unicast address its controller endpoints can name, and this provider's bindAddress is not one",
		Remediation: "set spec.libvirt.bmcEmulationDefaults.bindAddress on InfraProvider/lab-libvirt to one unicast host address",
	}}
	if refused := Refusals(catalogOf(controller(), listener)); !slices.Equal(refused, want) {
		t.Fatalf("refusals = %+v, want %+v", refused, want)
	}
}

// A frozen request must survive the round trip the plan and the adapter both
// perform, byte for byte.
func TestFrozenRequestsRoundTripExactly(t *testing.T) {
	hosts, _ := HostRequests(labCatalog(), "controller", testContext)
	machines, _ := MachineRequests(labCatalog(), "controller", testContext)
	hostData, err := hosts[0].Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if decoded, err := DecodeHostRequest(hostData); err != nil || decoded.Identity != hosts[0].Identity {
		t.Fatalf("host round trip: %+v (%v)", decoded, err)
	}
	machineData, err := machines[0].Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if decoded, err := DecodeMachineRequest(machineData); err != nil || decoded.UUID != machines[0].UUID {
		t.Fatalf("machine round trip: %+v (%v)", decoded, err)
	}
	for name, data := range map[string][]byte{
		"trailing data":  append(slices.Clone(machineData), '{', '}'),
		"unknown field":  []byte(`{"unexpected":1}`),
		"empty":          {},
		"wrong contract": hostData,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeMachineRequest(data); err == nil {
				t.Fatal("a malformed frozen request was accepted")
			}
		})
	}
}

// The emulated BMC's image is acquired through the provider host Machine's own
// proxy choice, under the rule a managed service's image acquisition follows.
func TestTheEmulatorPullEgressesThroughItsHostMachinesProxyChoice(t *testing.T) {
	requests, err := MachineRequests(labCatalog(), "controller", testContext)
	if err != nil {
		t.Fatal(err)
	}
	if got := requests[0].Egress; got.HTTPProxy != "" || got.HTTPSProxy != "" || got.NoProxy == nil || len(got.NoProxy) != 0 {
		t.Fatalf("a direct host froze egress %#v", got)
	}
	proxied := func(management string) api.Catalog {
		host := controller()
		spec := host.Spec().With("proxy", api.MapValue(text("proxyRef", "lab-proxy"),
			field("noProxy", api.StringList("192.0.2.0/24", "lab.example.test"))))
		proxy := api.NewObject(api.Proxy, "lab-proxy", api.Value{}, api.MapValue(text("management", management),
			field("connection", api.MapValue(text("httpProxy", "http://proxy.example.test:3128")))))
		return catalogOf(api.NewObject(api.Machine, "controller", api.Value{}, spec), proxy, provider(), networkConfig(), guest("rhel-01"))
	}
	requests, err = MachineRequests(proxied("external"), "controller", testContext)
	if err != nil {
		t.Fatal(err)
	}
	want := managedservice.Egress{HTTPProxy: "http://proxy.example.test:3128", NoProxy: []string{"192.0.2.0/24", "lab.example.test"}}
	if got := requests[0].Egress; got.HTTPProxy != want.HTTPProxy || got.HTTPSProxy != "" || !slices.Equal(got.NoProxy, want.NoProxy) {
		t.Fatalf("egress = %#v, want %#v", got, want)
	}
	_, err = MachineRequests(proxied("managed"), "controller", testContext)
	expectRefusal(t, err, "lifecycle.state")
}
