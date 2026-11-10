package libvirt

import (
	"fmt"
	"net/netip"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate"
)

// Refusals refuses every selected object this contract cannot realize, an arm
// no capability implements or a libvirt provider whose controllers would
// listen on an address their endpoints cannot name, with each object's reason
// and remedy.
func Refusals(catalog api.Catalog) []lifecycle.Refusal {
	var found []lifecycle.Refusal
	for _, unrealized := range substrate.Unrealizable(catalog) {
		found = append(found, lifecycle.RefusalOf(unrealized.Object, unrealized.Reason, unrealized.Remediation))
	}
	for _, provider := range substrate.ProvidersOn(catalog, substrate.ArmLibvirt) {
		if address := provider.Spec().Get("libvirt", "bmcEmulation", "bindAddress").Text(); !substrate.NameableListener(address) {
			found = append(found, lifecycle.RefusalOf(provider,
				"an emulated BMC listens on one unicast address its Machines' BMC endpoints can name, and this provider's bindAddress is not one",
				"set spec.libvirt.bmcEmulation.bindAddress on "+provider.Identity()+" to one unicast host address"))
		}
		if len(substrate.ExternalBridges(provider)) > substrate.MaxExternalBridges {
			found = append(found, lifecycle.RefusalOf(provider,
				fmt.Sprintf("a libvirt provider's bridge-use reservation claims one shared key for each bridge its existing-bridge segments use, within the 64 keys one reservation holds, so they use at most %d bridges", substrate.MaxExternalBridges),
				fmt.Sprintf("name at most %d distinct bridges in spec.segments of %s", substrate.MaxExternalBridges, provider.Identity())))
		}
	}
	return lifecycle.SortRefusals(found)
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
	networks, err := networksFor(catalog, provider, contextName)
	if err != nil {
		return HostRequest{}, err
	}
	uri := provider.Spec().Get("libvirt", "uri").Text()
	if uri == "" {
		return HostRequest{}, refusal("api.required", "the libvirt provider declares no connection URI", "set spec.libvirt.uri on "+provider.Identity())
	}
	readyUnit := ""
	for _, network := range networks {
		if network.Managed {
			readyUnit = "bootwright-" + contextName + "-" + name + "-firewalld-ready.service"
			break
		}
	}
	refused := refusedPrefixes(catalog, provider)
	if len(refused) > MaxRefusedPrefixes {
		return HostRequest{}, &diagnostics.Failure{Diagnostics: []diagnostics.Diagnostic{{Severity: "error", Code: "api.invariant",
			Message:     fmt.Sprintf("the managed Networks of %s carry %d subnets in spec.libvirt.taggedNetworkRefs that its host holds no address in, and one provider refuses at most %d", provider.Identity(), len(refused), MaxRefusedPrefixes),
			Object:      &diagnostics.ObjectIdentity{APIVersion: api.APIVersion, Kind: string(provider.Kind()), Name: provider.Name()},
			Field:       "$.spec.libvirt.taggedNetworkRefs",
			Remediation: fmt.Sprintf("name at most %d such subnets across spec.libvirt.taggedNetworkRefs of the managed Networks of %s, or give the host an address in some of them", MaxRefusedPrefixes, provider.Identity())}}}
	}
	refusalUnit := ""
	if readyUnit != "" {
		refusalUnit = VLANRefusalUnit(contextName, name)
	}
	return HostRequest{
		FirewallReadyUnit: readyUnit,
		Identity:          Identity{Block: HostBlockID(name), Context: contextName, Object: name},
		Networks:          networks,
		Packages:          HypervisorPackages(),
		Placement:         placement,
		PoolName:          substrate.PoolName(contextName, name),
		PoolPath:          substrate.PoolPath(contextName, name),
		// The controller stage installs the closure on the controller, so this
		// block proves it there and installs it only on a host reached over SSH.
		Provisioned:     !placement.Local(),
		RefusedPrefixes: refused,
		Services:        ServiceUnits(),
		URI:             uri,
		Version:         hostRequestVersion,
		VLANRefusalUnit: refusalUnit,
	}, nil
}

// networksFor derives one entry per segment: a managed Network naming the
// provider is what this context defines; a provider segment names an existing
// bridge it only proves.
func networksFor(catalog api.Catalog, provider api.Object, contextName string) ([]Network, error) {
	networks := []Network{}
	for _, segment := range substrate.Segments(catalog, provider).Items() {
		name := segment.Get("name").Text()
		arm := segment.Get("libvirt")
		if !arm.Present() {
			return nil, refusal("lifecycle.state", "a libvirt provider segment declares another substrate arm", "correct spec.segments on the provider")
		}
		if !substrate.SafeSegment(name) {
			return nil, refusal("lifecycle.state", "the segment or Network name "+name+" is not a safe host identifier", "rename the "+name+" segment or Network")
		}
		network := Network{Bridge: arm.Get("bridge").Text(), Name: substrate.NetworkName(contextName, name), MTU: substrate.EmittedMTU(segment)}
		if network.Bridge == "" {
			return nil, refusal("api.required", "the "+name+" segment of "+provider.Identity()+" declares no bridge", "set spec.libvirt.bridge on Network/"+name+" or spec.segments[].libvirt.bridge on "+provider.Identity())
		}
		managed, isManaged := substrate.ManagedSegmentNetwork(segment)
		if !isManaged {
			networks = append(networks, network)
			continue
		}
		if _, err := netip.ParsePrefix(managed.Prefix); err != nil {
			return nil, refusal("api.value", "the managed Network/"+name+" has no host address inside its subnet", "set spec.libvirt.hostAddress or a gateway on Network/"+name)
		}
		network.Address, network.Forward, network.Managed = managed.Prefix, managed.Forward, true
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
		host, _ := catalog.Find(api.Machine, provider.Spec().Get("libvirt", "machineRef").Text())
		egress, err := managedservice.EgressFor(catalog, host)
		if err != nil {
			return nil, err
		}
		base, ok := provider.Spec().Get("libvirt", "bmcEmulation", "port").Int64()
		if !ok || base < 1 {
			return nil, refusal("api.value", "the provider declares no emulated BMC port", "set spec.libvirt.bmcEmulation.port on "+provider.Identity())
		}
		for ordinal, machine := range substrate.HostedMachines(catalog, provider.Name()) {
			request, err := machineRequestFor(catalog, provider, machine, placement, egress, contextName, int(base)+ordinal)
			if err != nil {
				return nil, err
			}
			requests = append(requests, request)
		}
	}
	slices.SortFunc(requests, func(x, y MachineRequest) int { return strings.Compare(x.Identity.Object, y.Identity.Object) })
	return requests, nil
}

func machineRequestFor(catalog api.Catalog, provider, machine api.Object, placement machineref.Placement, egress managedservice.Egress, contextName string, port int) (MachineRequest, error) {
	name := machine.Name()
	if !substrate.SafeSegment(name) {
		return MachineRequest{}, refusal("lifecycle.state", "the Machine name is not a safe host identifier", "rename "+machine.Identity())
	}
	if port > 65535 {
		return MachineRequest{}, refusal("api.value", "the provider's emulated BMC range leaves the port space", "lower spec.libvirt.bmcEmulation.port on "+provider.Identity())
	}
	profile, found := findNamed(provider.Spec().Get("libvirt", "machineProfiles"), "name", substrate.MachineProfileRef(machine))
	if !found {
		return MachineRequest{}, refusal("api.reference", "the Machine's profile does not resolve on its provider", "correct spec.substrate.libvirt.profileRef on "+machine.Identity())
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
	credentials := provider.Spec().Get("libvirt", "bmcEmulation", "auth", "credentialsRef").Text()
	if credentials == "" {
		return MachineRequest{}, refusal("api.required", "the provider declares no emulated BMC credential", "set spec.libvirt.bmcEmulation.auth.credentialsRef on "+provider.Identity())
	}
	certificate := provider.Spec().Get("libvirt", "bmcEmulation", "certificateRef").Text()
	if certificate == "" {
		return MachineRequest{}, refusal("api.required", "the provider declares no emulated BMC serving certificate", "set spec.libvirt.bmcEmulation.certificateRef on "+provider.Identity())
	}
	address := provider.Spec().Get("libvirt", "bmcEmulation", "bindAddress").Text()
	uuid := substrate.DomainUUID(contextName, name)
	image, source := managedservice.MirroredImage(catalog, emulatorImage)
	return MachineRequest{
		Controller: Controller{
			Address: address, CertificateRef: certificate, CredentialsRef: credentials, Endpoint: substrate.ControllerEndpoint(address, port, uuid),
			Image: image, ImageSource: source, Port: port, Unit: substrate.ControllerUnitName(contextName, name),
		},
		Directory:  substrate.DiskDirectory(contextName, name),
		Disks:      disks,
		Domain:     substrate.DomainName(contextName, name),
		Egress:     egress,
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
	dataDisks := profile.Get("dataDisks").Items()
	presentations, valid := substrate.DataDiskPresentations(dataDisks)
	if !valid {
		return nil, refusal("api.value", "a libvirt profile declares unsupported data disk media or targets", "use at most seven dataDisks, omit media or set media: ssd or hdd, and keep root reserved on the profile of "+identity)
	}
	for index, data := range dataDisks {
		name := data.Get("name").Text()
		size, ok := data.Get("sizeGiB").Int64()
		if !substrate.SafeSegment(name) || name == "root" || !ok || size < 1 {
			return nil, refusal("api.value", "a data disk declares no safe name or positive size", "correct dataDisks on the profile of "+identity)
		}
		disk := Disk{Name: name, Path: directory + "/" + name + ".qcow2", Serial: substrate.DataDiskSerial(contextName, machine, name), SizeGiB: int(size)}
		presentation := presentations[index]
		disk.Bus, disk.Media, disk.RotationRate, disk.Target = presentation.Bus, presentation.Media, presentation.RotationRate, presentation.Target
		disks = append(disks, disk)
	}
	return disks, nil
}

func interfacesFor(catalog api.Catalog, provider, machine api.Object, contextName string) ([]Interface, error) {
	nics, err := substrate.VirtualInterfaces(contextName, machine)
	if err != nil {
		return nil, err
	}
	interfaces := make([]Interface, 0, len(nics))
	for _, nic := range nics {
		reference := substrate.NICSegment(catalog, provider, machine, nic.Name)
		segment, found := findNamed(substrate.Segments(catalog, provider), "name", reference)
		if !found || !segment.Has("libvirt") {
			return nil, refusal("api.reference", "NIC "+nic.Name+" of the Machine selects no libvirt segment of its provider", "add a spec.substrate.libvirt.nics entry for NIC "+nic.Name+" naming a libvirt segment of "+provider.Identity()+" on "+machine.Identity())
		}
		defined := ""
		if _, managed := substrate.ManagedSegmentNetwork(segment); managed {
			defined = substrate.NetworkName(contextName, reference)
		}
		interfaces = append(interfaces, Interface{
			Bridge: segment.Get("libvirt", "bridge").Text(), MACAddress: nic.MACAddress, Name: nic.Name, Network: defined, MTU: substrate.EmittedMTU(segment),
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
