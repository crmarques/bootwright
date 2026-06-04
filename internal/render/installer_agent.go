package render

import "github.com/crmarques/bootwright/api/v1alpha1"

// agentHosts renders agent-config.yaml's hosts[] block and picks the
// rendezvous IP. The rendezvous host is the first master node in sorted
// hostname order.
func agentHosts(state v1alpha1.State, ci v1alpha1.ClusterInstall, ocp v1alpha1.ContainerCluster) ([]any, string) {
	nodes := sortedNodes(ocp.Spec.Nodes)
	hosts := make([]any, 0, len(nodes))
	rendezvous := ""
	for _, node := range nodes {
		machine, ok := findClusterMachine(ci, node.MachineRef.Name)
		if !ok {
			continue
		}
		host := map[string]any{
			"hostname":   node.Hostname,
			"role":       installerNodeRole(node.Role),
			"interfaces": agentHostInterfaces(state, machine, ocp.Metadata.Name),
		}
		if hints := rootDeviceHintsConfig(machineRootDeviceHints(state, machine)); len(hints) > 0 {
			host["rootDeviceHints"] = hints
		}
		networkConfig := agentNetworkConfig(state, ci, machine, ocp.Metadata.Name)
		if len(networkConfig) > 0 {
			host["networkConfig"] = networkConfig
		}
		if rendezvous == "" && node.Role == v1alpha1.NodeRoleMaster {
			rendezvous = networkConfigPrimaryIP(networkConfig)
		}
		hosts = append(hosts, host)
	}
	return hosts, rendezvous
}

func agentHostInterfaces(state v1alpha1.State, machine v1alpha1.InstallMachine, clusterName string) []any {
	interfaces := machineInterfaces(state, machine, clusterName)
	out := make([]any, 0, len(interfaces))
	for _, iface := range interfaces {
		out = append(out, map[string]any{
			"name":       iface.Name,
			"macAddress": iface.MACAddress,
		})
	}
	return out
}

func agentNetworkConfig(state v1alpha1.State, ci v1alpha1.ClusterInstall, machine v1alpha1.InstallMachine, clusterName string) map[string]any {
	network, hasNetwork := machineNetworkDefinition(state, ci, machine)
	out := machineNetworkConfigTemplate(state, ci, machine)
	if out == nil {
		return nil
	}
	renderMachineMACs(out, machineInterfaces(state, machine, clusterName))
	if hasNetwork {
		renderDNSServers(out, resolveClusterDNSServersFromConfig(state, ci, network, out))
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func machineInterfaces(state v1alpha1.State, machine v1alpha1.InstallMachine, clusterName string) []v1alpha1.MachineNIC {
	if interfaces := providerMachineInterfaces(state, machine); len(interfaces) > 0 {
		return interfaces
	}
	if clusterName == "" || machine.Source.ProfileRef.Name == "" {
		return nil
	}
	provider, ok := findProvider(state, machine.Source.ProviderRef.Name)
	if !ok {
		return nil
	}
	if _, ok := findProfile(provider, machine.Source.ProfileRef.Name); !ok {
		return nil
	}
	if provider.Spec.Type != v1alpha1.ProvisionerLibvirt && provider.Spec.Type != v1alpha1.ProvisionerKubeVirt {
		return nil
	}
	names := clusterMachineInterfaceNames(state, machine)
	out := make([]v1alpha1.MachineNIC, 0, len(names))
	for _, name := range names {
		out = append(out, v1alpha1.MachineNIC{
			Name:       name,
			MACAddress: libvirtMACAddress(clusterName, machine.Name, name),
		})
	}
	return out
}

func clusterMachineInterfaceNames(state v1alpha1.State, machine v1alpha1.InstallMachine) []string {
	out := networkConfigInterfaceNames(machineNetworkConfigTemplate(state, v1alpha1.ClusterInstall{}, machine))
	if len(out) == 0 {
		out = append(out, "primary")
	}
	return out
}

func providerMachineInterfaces(state v1alpha1.State, machine v1alpha1.InstallMachine) []v1alpha1.MachineNIC {
	if machine.Source.MachineRef.Name == "" {
		return nil
	}
	pm, ok := findProviderMachine(state, machine.Source.MachineRef.Name)
	if !ok || len(pm.Spec.Hardware.NICs) == 0 {
		return nil
	}
	nics := map[string]v1alpha1.MachineNIC{}
	for _, nic := range pm.Spec.Hardware.NICs {
		nics[nic.Name] = nic
	}
	if len(machine.InterfaceBinding) == 0 {
		return append([]v1alpha1.MachineNIC(nil), pm.Spec.Hardware.NICs...)
	}
	out := make([]v1alpha1.MachineNIC, 0, len(machine.InterfaceBinding))
	for _, binding := range machine.InterfaceBinding {
		nic := nics[binding.NICRef.Name]
		out = append(out, v1alpha1.MachineNIC{
			Name:       binding.InterfaceName,
			MACAddress: nic.MACAddress,
		})
	}
	return out
}

func machineRootDeviceHints(state v1alpha1.State, machine v1alpha1.InstallMachine) *v1alpha1.RootDeviceHints {
	if machine.RootDeviceHints != nil {
		return machine.RootDeviceHints
	}
	if machine.Source.MachineRef.Name == "" {
		return nil
	}
	pm, ok := findProviderMachine(state, machine.Source.MachineRef.Name)
	if !ok {
		return nil
	}
	return pm.Spec.OS.Install.RootDeviceHints
}

func renderMachineMACs(config map[string]any, ifaces []v1alpha1.MachineNIC) {
	if len(ifaces) == 0 {
		return
	}
	interfaces := ensureInterfaceList(config)
	index := interfaceIndex(interfaces)
	for _, iface := range ifaces {
		entry := index[iface.Name]
		if entry == nil {
			entry = map[string]any{
				"name":  iface.Name,
				"type":  "ethernet",
				"state": "up",
			}
			interfaces = append(interfaces, entry)
			index[iface.Name] = entry
		}
		if iface.MACAddress != "" {
			entry["mac-address"] = iface.MACAddress
		}
	}
	config["interfaces"] = interfaces
}

func ensureInterfaceList(config map[string]any) []any {
	if raw, ok := config["interfaces"].([]any); ok {
		return raw
	}
	out := []any{}
	config["interfaces"] = out
	return out
}

func interfaceIndex(interfaces []any) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, item := range interfaces {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := entry["name"].(string)
		if name != "" {
			out[name] = entry
		}
	}
	return out
}

func rootDeviceHintsConfig(hints *v1alpha1.RootDeviceHints) map[string]any {
	if hints == nil {
		return nil
	}
	out := map[string]any{}
	if hints.DeviceName != "" {
		out["deviceName"] = hints.DeviceName
	}
	if hints.HCTL != "" {
		out["hctl"] = hints.HCTL
	}
	if hints.Model != "" {
		out["model"] = hints.Model
	}
	if hints.Vendor != "" {
		out["vendor"] = hints.Vendor
	}
	if hints.SerialNumber != "" {
		out["serialNumber"] = hints.SerialNumber
	}
	if hints.MinSizeGigabytes > 0 {
		out["minSizeGigabytes"] = hints.MinSizeGigabytes
	}
	if hints.WWN != "" {
		out["wwn"] = hints.WWN
	}
	if hints.Rotational != nil {
		out["rotational"] = *hints.Rotational
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
