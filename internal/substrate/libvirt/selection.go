package libvirt

import (
	machineref "github.com/crmarques/bootwright/internal/machine"
	"net/netip"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/substrate"
)

// Unsupported lists every selected object this contract cannot realize: an
// arm no capability implements, which the substrate root owns so that adding
// one narrows the refusal in a single place, and a libvirt provider whose
// controllers would listen on an address their endpoints cannot name.
// Admission refuses the same provider; this keeps the refusal for state that
// did not pass through it.
func Unsupported(catalog api.Catalog) []string {
	found := substrate.Unrealizable(catalog)
	for _, provider := range substrate.ProvidersOn(catalog, substrate.ArmLibvirt) {
		if address := provider.Spec().Get("libvirt", "bmcEmulationDefaults", "bindAddress").Text(); !substrate.NameableListener(address) {
			found = append(found, provider.Identity())
		}
	}
	slices.Sort(found)
	return slices.Compact(found)
}

// HostRequests derives one frozen request per libvirt provider host, in
// canonical object order. It reads no host, endpoint or Secret material.
func HostRequests(catalog api.Catalog, controllerMachine, contextName string) ([]HostRequest, error) {
	if !api.ValidLexical("name", contextName) {
		return nil, refusal("lifecycle.state", "the lifecycle context identity is invalid", "")
	}
	var requests []HostRequest
	for _, provider := range substrate.ProvidersOn(catalog, substrate.ArmLibvirt) {
		request, err := hostRequestFor(catalog, provider, controllerMachine, contextName)
		if err != nil {
			return nil, err
		}
		requests = append(requests, request)
	}
	return requests, nil
}

func hostRequestFor(catalog api.Catalog, provider api.Object, controllerMachine, contextName string) (HostRequest, error) {
	name := provider.Name()
	if !substrate.SafeSegment(name) {
		return HostRequest{}, refusal("lifecycle.state", "the provider name is not a safe host identifier", "rename "+provider.Identity())
	}
	placement, err := placementFor(catalog, provider, controllerMachine)
	if err != nil {
		return HostRequest{}, err
	}
	networks, err := networksFor(provider, contextName)
	if err != nil {
		return HostRequest{}, err
	}
	uri := provider.Spec().Get("libvirt", "uri").Text()
	if uri == "" {
		return HostRequest{}, refusal("api.required", "the libvirt provider declares no connection URI", "set spec.libvirt.uri on "+provider.Identity())
	}
	return HostRequest{
		Identity:  Identity{Block: HostBlockID(name), Context: contextName, Object: name},
		Networks:  networks,
		Packages:  HypervisorPackages(),
		Placement: placement,
		PoolName:  substrate.PoolName(contextName, name),
		PoolPath:  substrate.PoolPath(contextName, name),
		// The controller stage installs the closure on the controller, so this
		// block proves it there and installs it only on a host reached over SSH.
		Provisioned: !placement.Local(),
		Services:    ServiceUnits(),
		URI:         uri,
		Version:     hostRequestVersion,
	}, nil
}

// networksFor derives one entry per declared attachment. A managed entry is
// what this context defines; an external one names a bridge it only proves.
func networksFor(provider api.Object, contextName string) ([]Network, error) {
	networks := []Network{}
	for _, attachment := range provider.Spec().Get("networkAttachments").Items() {
		name := attachment.Get("name").Text()
		arm := attachment.Get("libvirt")
		if !arm.Present() {
			return nil, refusal("lifecycle.state", "a libvirt provider attachment declares another substrate arm", "correct networkAttachments on the provider")
		}
		if !substrate.SafeSegment(name) {
			return nil, refusal("lifecycle.state", "the attachment name is not a safe host identifier", "rename the "+name+" attachment")
		}
		network := Network{Bridge: arm.Get("bridge").Text(), Name: substrate.NetworkName(contextName, name)}
		if network.Bridge == "" {
			return nil, refusal("api.required", "a libvirt attachment declares no bridge", "set networkAttachments[].libvirt.bridge")
		}
		if arm.Get("management").Text() != "managed" {
			networks = append(networks, network)
			continue
		}
		address := arm.Get("address").Text()
		if _, err := netip.ParsePrefix(address); err != nil {
			return nil, refusal("api.value", "a managed libvirt attachment declares no host address and prefix", "set networkAttachments[].libvirt.address")
		}
		network.Address, network.Forward, network.Managed = address, arm.Get("forward").Text(), true
		networks = append(networks, network)
	}
	slices.SortFunc(networks, func(x, y Network) int { return strings.Compare(x.Name, y.Name) })
	return networks, nil
}

// MachineRequests derives one frozen request per realized Machine, in canonical
// object order across providers. The emulated BMC port comes from the
// provider's base port and the Machine's position in that provider's own order.
func MachineRequests(catalog api.Catalog, controllerMachine, contextName string) ([]MachineRequest, error) {
	if !api.ValidLexical("name", contextName) {
		return nil, refusal("lifecycle.state", "the lifecycle context identity is invalid", "")
	}
	var requests []MachineRequest
	for _, provider := range substrate.ProvidersOn(catalog, substrate.ArmLibvirt) {
		placement, err := placementFor(catalog, provider, controllerMachine)
		if err != nil {
			return nil, err
		}
		base, ok := provider.Spec().Get("libvirt", "bmcEmulationDefaults", "port").Int64()
		if !ok || base < 1 {
			return nil, refusal("api.value", "the provider declares no emulated controller port", "set spec.libvirt.bmcEmulationDefaults.port on "+provider.Identity())
		}
		for ordinal, machine := range substrate.HostedMachines(catalog, provider.Name()) {
			request, err := machineRequestFor(catalog, provider, machine, placement, contextName, int(base)+ordinal)
			if err != nil {
				return nil, err
			}
			requests = append(requests, request)
		}
	}
	slices.SortFunc(requests, func(x, y MachineRequest) int { return strings.Compare(x.Identity.Object, y.Identity.Object) })
	return requests, nil
}

func machineRequestFor(catalog api.Catalog, provider, machine api.Object, placement machineref.Placement, contextName string, port int) (MachineRequest, error) {
	name := machine.Name()
	if !substrate.SafeSegment(name) {
		return MachineRequest{}, refusal("lifecycle.state", "the Machine name is not a safe host identifier", "rename "+machine.Identity())
	}
	if port > 65535 {
		return MachineRequest{}, refusal("api.value", "the provider's emulated controller range leaves the port space", "lower spec.libvirt.bmcEmulationDefaults.port on "+provider.Identity())
	}
	profile, found := findNamed(provider.Spec().Get("libvirt", "machineProfiles"), "name", machine.Spec().Get("substrate", "profileRef").Text())
	if !found {
		return MachineRequest{}, refusal("api.reference", "the Machine's profile does not resolve on its provider", "correct spec.substrate.profileRef on "+machine.Identity())
	}
	disks, err := disksFor(profile, machine.Identity(), contextName, name)
	if err != nil {
		return MachineRequest{}, err
	}
	interfaces, err := interfacesFor(catalog, provider, machine, contextName)
	if err != nil {
		return MachineRequest{}, err
	}
	vcpu, _ := profile.Get("cpu").Int64()
	memory, _ := profile.Get("memoryMiB").Int64()
	if vcpu < 1 || memory < 1 {
		return MachineRequest{}, refusal("api.value", "the Machine's profile declares no vCPU count or memory", "set cpu and memoryMiB on the profile of "+machine.Identity())
	}
	credentials := provider.Spec().Get("libvirt", "bmcEmulationDefaults", "auth", "credentialsRef").Text()
	if credentials == "" {
		return MachineRequest{}, refusal("api.required", "the provider declares no emulated controller credential", "set spec.libvirt.bmcEmulationDefaults.auth.credentialsRef on "+provider.Identity())
	}
	address := provider.Spec().Get("libvirt", "bmcEmulationDefaults", "bindAddress").Text()
	uuid := substrate.DomainUUID(contextName, name)
	return MachineRequest{
		Controller: Controller{
			Address: address, CredentialsRef: credentials, Endpoint: substrate.ControllerEndpoint(address, port, uuid),
			Image: emulatorImage, Port: port, Unit: substrate.ControllerUnitName(contextName, name),
		},
		Directory:  substrate.DiskDirectory(contextName, name),
		Disks:      disks,
		Domain:     substrate.DomainName(contextName, name),
		Identity:   Identity{Block: MachineBlockID(name), Context: contextName, Object: name},
		Interfaces: interfaces,
		MemoryMiB:  int(memory),
		Placement:  placement,
		PoolName:   substrate.PoolName(contextName, provider.Name()),
		PoolPath:   substrate.PoolPath(contextName, provider.Name()),
		TPM:        profile.Has("tpm"),
		URI:        provider.Spec().Get("libvirt", "uri").Text(),
		UUID:       uuid,
		VCPU:       int(vcpu),
		Version:    machineRequestVersion,
	}, nil
}

// disksFor derives the root disk and every declared data disk, in the target
// order the domain presents them, so a replay compares the same devices.
func disksFor(profile api.Value, identity, contextName, machine string) ([]Disk, error) {
	root, ok := profile.Get("diskGiB").Int64()
	if !ok || root < 1 {
		return nil, refusal("api.value", "the Machine's profile declares no root disk size", "set diskGiB on the profile of "+identity)
	}
	directory := substrate.DiskDirectory(contextName, machine)
	disks := []Disk{{Name: "root", Path: directory + "/root.qcow2", SizeGiB: int(root), Target: "vda"}}
	for index, data := range profile.Get("dataDisks").Items() {
		name := data.Get("name").Text()
		size, ok := data.Get("sizeGiB").Int64()
		if !substrate.SafeSegment(name) || !ok || size < 1 {
			return nil, refusal("api.value", "a data disk declares no safe name or positive size", "correct dataDisks on the profile of "+identity)
		}
		if index >= len(diskTargets) {
			return nil, refusal("api.value", "the Machine's profile declares more data disks than the domain can present", "reduce dataDisks on the profile of "+identity)
		}
		disks = append(disks, Disk{Name: name, Path: directory + "/" + name + ".qcow2", SizeGiB: int(size), Target: diskTargets[index]})
	}
	return disks, nil
}

// diskTargets are the virtio device names after the root disk, in the order a
// domain presents them.
var diskTargets = []string{"vdb", "vdc", "vdd", "vde", "vdf", "vdg", "vdh"}

// interfacesFor wires every effective physical interface to the one attachment
// the Machine selects, because that is what the attachment reference means.
func interfacesFor(catalog api.Catalog, provider, machine api.Object, contextName string) ([]Interface, error) {
	network := machine.Spec().Get("network")
	reference := network.Get("attachmentRef").Text()
	attachment, found := findNamed(provider.Spec().Get("networkAttachments"), "name", reference)
	if !found {
		return nil, refusal("api.reference", "the Machine's attachment does not resolve on its provider", "correct spec.network.attachmentRef on "+machine.Identity())
	}
	bridge := attachment.Get("libvirt", "bridge").Text()
	defined := ""
	if attachment.Get("libvirt", "management").Text() == "managed" {
		defined = substrate.NetworkName(contextName, reference)
	}
	names, err := substrate.EthernetInterfaces(catalog, machine)
	if err != nil {
		return nil, err
	}
	interfaces := make([]Interface, 0, len(names))
	for _, name := range names {
		interfaces = append(interfaces, Interface{
			Bridge: bridge, MACAddress: substrate.InterfaceMAC(contextName, machine.Name(), name), Name: name, Network: defined,
		})
	}
	return interfaces, nil
}

// placementFor selects the arm this provider's effects run through: the
// controller when it hosts the provider, otherwise that host's own SSH access.
func placementFor(catalog api.Catalog, provider api.Object, controllerMachine string) (machineref.Placement, error) {
	reference := provider.Spec().Get("libvirt", "machineRef").Text()
	host, found := catalog.Find(api.Machine, reference)
	if !found {
		return machineref.Placement{}, refusal("api.reference", "the provider's host Machine is not in the selected graph", "declare "+reference+" or change spec.libvirt.machineRef")
	}
	return machineref.PlacementFor(host, controllerMachine)
}

func findNamed(values api.Value, key, name string) (api.Value, bool) {
	if name == "" {
		return api.Value{}, false
	}
	for _, value := range values.Items() {
		if value.Get(key).Text() == name {
			return value, true
		}
	}
	return api.Value{}, false
}

func refusal(code, message, remediation string) error {
	return diagnostics.NewFailureWithRemediation(code, message, "", remediation)
}
