package libvirt

import (
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
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
		return strings.HasPrefix(key, "bridge:") || strings.HasPrefix(key, "libvirt-network:")
	}) {
		t.Fatalf("an external attachment claimed a host resource: %v", keys)
	}
}

func TestHostReservationsClaimEveryManagedNetworkAndThePool(t *testing.T) {
	requests, _ := HostRequests(labCatalog(), "controller", testContext)
	want := []string{"bridge:virbr-lab", "libvirt-network:bootwright-lab-lab-guests", "path:/var/lib/libvirt/images/bootwright/lab/lab-libvirt/vmedia"}
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
	if placement.Connection != "ssh" || placement.Machine != "hypervisor" || placement.Address != "192.0.2.5" || placement.User != "operator" {
		t.Fatalf("placement = %+v", placement)
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
	metal := api.NewObject(api.InfraProvider, "rack", api.Value{}, api.MapValue(field("baremetal", api.MapValue())))
	hosted := api.NewObject(api.Machine, "node-01", api.Value{}, api.MapValue(
		field("substrate", api.MapValue(text("providerRef", "rack"))),
		field("os", api.MapValue(field("provided", api.BoolValue(false)))),
	))
	unsupported := Unsupported(catalogOf(controller(), provider(), networkConfig(), guest("rhel-01"), metal, hosted))
	if !slices.Equal(unsupported, []string{"InfraProvider/rack", "Machine/node-01"}) {
		t.Fatalf("unsupported = %v", unsupported)
	}
}

// A controller that binds a wildcard gives its guests no endpoint a consumer
// can name, so the provider refuses rather than realizing an unreachable BMC.
func TestAWildcardControllerAddressRefuses(t *testing.T) {
	for _, address := range []string{"0.0.0.0", "::", ""} {
		defaults := provider().Spec().Get("libvirt", "bmcEmulationDefaults").With("bindAddress", api.StringValue(address))
		wildcard := provider(field("libvirt", provider().Spec().Get("libvirt").With("bmcEmulationDefaults", defaults)))
		if unsupported := Unsupported(catalogOf(controller(), wildcard)); !slices.Contains(unsupported, "InfraProvider/lab-libvirt") {
			t.Fatalf("%q was accepted: %v", address, unsupported)
		}
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
