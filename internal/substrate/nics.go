package substrate

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func MachineNICs(machine api.Object) []string {
	var names []string
	for _, nic := range machine.Spec().Get("hardware", "nics").Items() {
		names = append(names, nic.Get("name").Text())
	}
	return names
}

func MachineNICSegments(machine api.Object) api.Value {
	return machine.Spec().Get("hardware", "nics")
}

func NICSegment(catalog api.Catalog, provider, machine api.Object, nic string) string {
	for _, entry := range MachineNICSegments(machine).Items() {
		if entry.Get("name").Text() == nic && entry.Has("segmentRef") {
			return entry.Get("segmentRef").Text()
		}
	}
	if segments := Segments(catalog, provider); segments.Len() == 1 {
		return segments.Items()[0].Get("name").Text()
	}
	return ""
}

func InterfaceNIC(machine api.Object, iface string) string {
	for _, match := range machine.Spec().Get("network", "interfaceMatches").Items() {
		if match.Get("interface").Text() == iface {
			return match.Get("nicRef").Text()
		}
	}
	return iface
}

func VirtualInterfaces(contextName string, machine api.Object) ([]Interface, error) {
	names := MachineNICs(machine)
	if len(names) == 0 {
		return nil, refusal("api.required", "the Machine declares no NIC for its provider to create",
			"declare each vNIC of "+machine.Identity()+" in spec.hardware.nics, in device order")
	}
	interfaces := make([]Interface, 0, len(names))
	for _, name := range names {
		interfaces = append(interfaces, Interface{Name: name, MACAddress: InterfaceMAC(contextName, machine.Name(), name)})
	}
	return interfaces, nil
}

func InstallVLANConflict(catalog api.Catalog, machine api.Object, nic string, vlan int) (string, ManagedNetwork, bool) {
	provider, found := catalog.Find(api.InfraProvider, MachineProviderRef(machine))
	if !found || !BridgedGuests(Variant(provider)) {
		return "", ManagedNetwork{}, false
	}
	reference := NICSegment(catalog, provider, machine, InterfaceNIC(machine, nic))
	segment, attached := Segments(catalog, provider).Named("name", reference)
	network, managed := ManagedSegmentNetwork(segment)
	return reference, network, attached && managed && network.VLAN != vlan
}
