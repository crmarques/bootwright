package render

import "github.com/crmarques/bootwright/api/v1alpha1"

// agentHosts renders agent-config.yaml's hosts[] block and picks the
// rendezvous IP. The rendezvous host is the first master node in sorted
// hostname order.
func agentHosts(state v1alpha1.State, ci v1alpha1.ClusterInfra, ocp v1alpha1.ContainerCluster) ([]any, string) {
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
		if nc := agentNetworkConfig(state, ci, machine, ocp.Metadata.Name); len(nc) > 0 {
			host["networkConfig"] = nc
		}
		if rendezvous == "" && node.Role == v1alpha1.NodeRoleMaster {
			rendezvous = primaryIP(machine)
		}
		hosts = append(hosts, host)
	}
	return hosts, rendezvous
}

func agentHostInterfaces(state v1alpha1.State, machine v1alpha1.ClusterMachineComponent, clusterName string) []any {
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

func agentNetworkConfig(state v1alpha1.State, ci v1alpha1.ClusterInfra, machine v1alpha1.ClusterMachineComponent, clusterName string) map[string]any {
	var out map[string]any
	network, hasNetwork := findNetworkConfig(state, machine.NetworkConfig.Ref.Name)
	if len(machine.NetworkConfig.NetworkConfig) > 0 {
		out = cloneYAMLMap(machine.NetworkConfig.NetworkConfig)
	} else if hasNetwork {
		out = cloneYAMLMap(network.Spec.Template.NetworkConfig)
	}
	if out == nil {
		return nil
	}
	renderMachineMACs(out, machineInterfaces(state, machine, clusterName))
	renderAddressOverlays(out, machine.NetworkConfig.Addresses)
	if hasNetwork {
		renderDNSServers(out, resolveClusterDNSServersFromConfig(state, ci, network, out))
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func machineInterfaces(state v1alpha1.State, machine v1alpha1.ClusterMachineComponent, clusterName string) []v1alpha1.MachineInterface {
	if interfaces := providerMachineInterfaces(state, machine); len(interfaces) > 0 {
		return interfaces
	}
	if clusterName == "" || machine.From.Profile == "" {
		return nil
	}
	provider, ok := findProvider(state, machine.From.Provider)
	if !ok {
		return nil
	}
	profile, ok := findProfile(provider, machine.From.Profile)
	if !ok || v1alpha1.ProfileProvisionerKind(profile) != v1alpha1.ProvisionerLibvirt {
		return nil
	}
	names := clusterMachineInterfaceNames(machine)
	out := make([]v1alpha1.MachineInterface, 0, len(names))
	for _, name := range names {
		out = append(out, v1alpha1.MachineInterface{
			Name:       name,
			MACAddress: libvirtMACAddress(clusterName, machine.Name, name),
		})
	}
	return out
}

func clusterMachineInterfaceNames(machine v1alpha1.ClusterMachineComponent) []string {
	seen := map[string]bool{}
	var out []string
	for _, addr := range machine.NetworkConfig.Addresses {
		if addr.Interface == "" || seen[addr.Interface] {
			continue
		}
		seen[addr.Interface] = true
		out = append(out, addr.Interface)
	}
	if len(out) == 0 {
		out = append(out, "primary")
	}
	return out
}

func providerMachineInterfaces(state v1alpha1.State, machine v1alpha1.ClusterMachineComponent) []v1alpha1.MachineInterface {
	if machine.From.Name == "" {
		return nil
	}
	provider, ok := findProvider(state, machine.From.Provider)
	if !ok {
		return nil
	}
	pm, ok := findProviderMachine(provider, machine.From.Name)
	if !ok || pm.BareMetal == nil {
		return nil
	}
	return append([]v1alpha1.MachineInterface(nil), pm.BareMetal.Interfaces...)
}

func machineRootDeviceHints(state v1alpha1.State, machine v1alpha1.ClusterMachineComponent) *v1alpha1.RootDeviceHints {
	if machine.RootDeviceHints != nil {
		return machine.RootDeviceHints
	}
	if machine.From.Name == "" {
		return nil
	}
	provider, ok := findProvider(state, machine.From.Provider)
	if !ok {
		return nil
	}
	pm, ok := findProviderMachine(provider, machine.From.Name)
	if !ok || pm.BareMetal == nil {
		return nil
	}
	return pm.BareMetal.RootDeviceHints
}

func renderMachineMACs(config map[string]any, ifaces []v1alpha1.MachineInterface) {
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

func renderAddressOverlays(config map[string]any, addresses []v1alpha1.NetworkConfigAddress) {
	if len(addresses) == 0 {
		return
	}
	interfaces := ensureInterfaceList(config)
	index := interfaceIndex(interfaces)
	for _, addr := range addresses {
		entry := index[addr.Interface]
		if entry == nil {
			entry = map[string]any{
				"name":  addr.Interface,
				"type":  "ethernet",
				"state": "up",
			}
			interfaces = append(interfaces, entry)
			index[addr.Interface] = entry
		}
		if len(addr.IPv4) > 0 {
			entry["ipv4"] = addressFamilyConfig(addr.IPv4)
		}
		if len(addr.IPv6) > 0 {
			entry["ipv6"] = addressFamilyConfig(addr.IPv6)
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

func addressFamilyConfig(addresses []v1alpha1.NetworkIPAddress) map[string]any {
	rendered := make([]any, 0, len(addresses))
	for _, address := range addresses {
		rendered = append(rendered, map[string]any{
			"ip":            address.IP,
			"prefix-length": address.PrefixLength,
		})
	}
	return map[string]any{
		"enabled": true,
		"dhcp":    false,
		"address": rendered,
	}
}

func primaryIP(machine v1alpha1.ClusterMachineComponent) string {
	for _, addr := range machine.NetworkConfig.Addresses {
		if len(addr.IPv4) > 0 {
			return addr.IPv4[0].IP
		}
		if len(addr.IPv6) > 0 {
			return addr.IPv6[0].IP
		}
	}
	return ""
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
