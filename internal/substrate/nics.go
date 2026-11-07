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

func VirtualArm(arm string) bool { return arm == ArmLibvirt || arm == ArmKubeVirt }

func MachineNICAttachments(machine api.Object) api.Value {
	if arm := MachineArm(machine); VirtualArm(arm) {
		return machine.Spec().Get("substrate", arm, "nics")
	}
	return api.Value{}
}

func NICAttachment(catalog api.Catalog, provider, machine api.Object, nic string) string {
	for _, entry := range MachineNICAttachments(machine).Items() {
		if entry.Get("nicRef").Text() == nic {
			return entry.Get("attachmentRef").Text()
		}
	}
	if attachments := Segments(catalog, provider); attachments.Len() == 1 {
		return attachments.Items()[0].Get("name").Text()
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

func NICInterface(machine api.Object, nic string) string {
	for _, match := range machine.Spec().Get("network", "interfaceMatches").Items() {
		if match.Get("nicRef").Text() == nic {
			return match.Get("interface").Text()
		}
	}
	return nic
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
