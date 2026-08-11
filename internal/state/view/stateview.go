package stateview

import (
	"net"
	"net/netip"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func Environment(state v1alpha1.State) *v1alpha1.Environment {
	if len(state.Environments) == 0 {
		return nil
	}
	return &state.Environments[0]
}

func Provider(state v1alpha1.State, name string) (v1alpha1.InfraProvider, bool) {
	for _, provider := range state.InfraProviders {
		if provider.Metadata.Name == name {
			return provider, true
		}
	}
	return v1alpha1.InfraProvider{}, false
}

func Machine(state v1alpha1.State, name string) (v1alpha1.Machine, bool) {
	for _, machine := range state.Machines {
		if machine.Metadata.Name == name {
			return machine, true
		}
	}
	return v1alpha1.Machine{}, false
}

func NetworkConfig(state v1alpha1.State, name string) (v1alpha1.NetworkConfig, bool) {
	for _, network := range state.NetworkConfigs {
		if network.Metadata.Name == name {
			return network, true
		}
	}
	return v1alpha1.NetworkConfig{}, false
}

func InfraComponent(state v1alpha1.State, name string) (v1alpha1.InfraComponent, bool) {
	for _, component := range state.InfraComponents {
		if component.Metadata.Name == name {
			return component, true
		}
	}
	return v1alpha1.InfraComponent{}, false
}

func MachineProfile(provider v1alpha1.InfraProvider, name string) (v1alpha1.MachineProfile, bool) {
	for _, profile := range ProviderMachineProfiles(provider) {
		if profile.Name == name {
			return profile, true
		}
	}
	return v1alpha1.MachineProfile{}, false
}

func ProviderMachineProfiles(provider v1alpha1.InfraProvider) []v1alpha1.MachineProfile {
	switch provider.Spec.Type {
	case v1alpha1.ProvisionerLibvirt:
		if provider.Spec.Libvirt != nil {
			return provider.Spec.Libvirt.MachineProfiles
		}
	case v1alpha1.ProvisionerVSphere:
		if provider.Spec.VSphere != nil {
			return provider.Spec.VSphere.MachineProfiles
		}
	case v1alpha1.ProvisionerKubeVirt:
		if provider.Spec.KubeVirt != nil {
			return provider.Spec.KubeVirt.MachineProfiles
		}
	}
	return nil
}

func VSphereProfileFailureDomain(spec *v1alpha1.InfraProviderVSphere, profile v1alpha1.MachineProfile) (v1alpha1.VSphereFailureDomain, bool) {
	if spec == nil {
		return v1alpha1.VSphereFailureDomain{}, false
	}
	if profile.FailureDomainRef.Name == "" {
		if len(spec.FailureDomains) == 1 {
			return spec.FailureDomains[0], true
		}
		return v1alpha1.VSphereFailureDomain{}, false
	}
	for _, fd := range spec.FailureDomains {
		if fd.Name == profile.FailureDomainRef.Name {
			return fd, true
		}
	}
	return v1alpha1.VSphereFailureDomain{}, false
}

func VSphereVCenterForServer(spec *v1alpha1.InfraProviderVSphere, server string) (v1alpha1.VSphereVCenter, bool) {
	if spec == nil {
		return v1alpha1.VSphereVCenter{}, false
	}
	for _, vc := range spec.VCenters {
		if vc.Server == server {
			return vc, true
		}
	}
	return v1alpha1.VSphereVCenter{}, false
}

func MachineHasCapability(machine v1alpha1.Machine, want string) bool {
	return v1alpha1.MachineHasCapability(machine, want)
}

func ClusterInstallForContainerCluster(state v1alpha1.State, cluster v1alpha1.ContainerCluster) (v1alpha1.ClusterInstall, bool) {
	nodes := make([]v1alpha1.InstallMachine, 0, len(cluster.Spec.Nodes))
	networkBindings := make([]v1alpha1.MachineNetworkBinding, 0, len(cluster.Spec.Nodes))
	for _, node := range cluster.Spec.Nodes {
		machine, ok := Machine(state, node.MachineRef.Name)
		if !ok {
			continue
		}
		nodes = append(nodes, clusterNodeFromMachine(machine))
		network := machine.Spec.Network.Config
		if network.NetworkConfigRef.Name != "" && (network.AttachmentRef.Name != "" || len(network.InterfaceAttachments) > 0) && machine.Spec.Substrate.ProviderRef.Name != "" {
			networkBindings = append(networkBindings, v1alpha1.MachineNetworkBinding{
				MachineName:          machine.Metadata.Name,
				NetworkConfigRef:     network.NetworkConfigRef,
				ProviderRef:          machine.Spec.Substrate.ProviderRef,
				AttachmentRef:        network.AttachmentRef,
				InterfaceAttachments: append([]v1alpha1.MachineInterfaceAttachment(nil), network.InterfaceAttachments...),
			})
		}
	}
	return v1alpha1.ClusterInstall{
		Metadata:        v1alpha1.Metadata{Name: cluster.Metadata.Name},
		Platform:        cluster.Spec.Install.Platform,
		Endpoints:       cluster.Spec.Install.Endpoints,
		Agent:           cluster.Spec.Install.Agent,
		NetworkBindings: networkBindings,
		Machines:        nodes,
	}, len(nodes) > 0 || len(cluster.Spec.Install.Endpoints) > 0 || cluster.Spec.Install.Platform.Type != ""
}

func InstallMachineFromMachine(machine v1alpha1.Machine) v1alpha1.InstallMachine {
	return clusterNodeFromMachine(machine)
}

func StorageClusterArtifactInstall(state v1alpha1.State, cluster v1alpha1.StorageCluster) (v1alpha1.ClusterInstall, bool) {
	if cluster.Spec.Ceph == nil {
		return v1alpha1.ClusterInstall{}, false
	}
	seen := map[string]bool{}
	var machines []v1alpha1.InstallMachine
	bareMetalManagedOS := false
	for _, node := range cluster.Spec.Ceph.Topology.Nodes {
		if node.MachineRef.Name == "" || seen[node.MachineRef.Name] {
			continue
		}
		machine, ok := Machine(state, node.MachineRef.Name)
		if !ok {
			continue
		}
		seen[node.MachineRef.Name] = true
		machines = append(machines, clusterNodeFromMachine(machine))
		if !v1alpha1.MachineInstallsOS(machine) {
			continue
		}
		if provider, ok := Provider(state, machine.Spec.Substrate.ProviderRef.Name); ok && provider.Spec.Type == v1alpha1.ProvisionerBareMetal {
			bareMetalManagedOS = true
		}
	}
	if !bareMetalManagedOS {
		return v1alpha1.ClusterInstall{}, false
	}
	ci := v1alpha1.ClusterInstall{
		Metadata: v1alpha1.Metadata{Name: cluster.Metadata.Name},
		Machines: machines,
	}
	return ci, true
}

func clusterNodeFromMachine(machine v1alpha1.Machine) v1alpha1.InstallMachine {
	node := v1alpha1.InstallMachine{
		Name: machine.Metadata.Name,
		Source: v1alpha1.InstallMachineSource{
			ProviderRef: machine.Spec.Substrate.ProviderRef,
			MachineRef:  v1alpha1.LocalObjectReference{Name: machine.Metadata.Name},
			ProfileRef:  v1alpha1.MachineProfileRef(machine),
		},
		Network:          machine.Spec.Network.Config,
		InterfaceBinding: append([]v1alpha1.MachineNetworkInterfaceBinding(nil), machine.Spec.Network.InterfaceBinding...),
		Addresses:        append([]v1alpha1.MachineAddress(nil), machine.Spec.Addresses...),
		RootDeviceHints:  machine.Spec.OS.Install.RootDeviceHints,
	}
	return node
}

func ClusterNodesForInstall(state v1alpha1.State, infra v1alpha1.ClusterInstall) map[string]v1alpha1.OCPNodeSpec {
	for _, cluster := range state.ContainerClusters {
		if cluster.Metadata.Name != infra.Metadata.Name {
			continue
		}
		out := map[string]v1alpha1.OCPNodeSpec{}
		for _, node := range cluster.Spec.Nodes {
			out[node.Name] = node
		}
		return out
	}
	return nil
}

func EndpointAddress(state v1alpha1.State, infra v1alpha1.ClusterInstall, name string) string {
	endpoint, ok := infra.Endpoints[name]
	if !ok {
		return ""
	}
	if endpoint.Source.Type == v1alpha1.EndpointSourceInfraComponent {
		if bind, ok := LoadBalancerBindAddress(state, endpoint.Source); ok {
			return bind.Address
		}
		return ""
	}
	return endpoint.Address
}

func LoadBalancerBindAddress(state v1alpha1.State, source v1alpha1.EndpointSource) (v1alpha1.LoadBalancerBindAddress, bool) {
	component, ok := InfraComponent(state, source.ComponentRef.Name)
	if !ok || component.Spec.LoadBalancer == nil {
		return v1alpha1.LoadBalancerBindAddress{}, false
	}
	binds := component.Spec.LoadBalancer.BindAddresses
	if source.BindAddressRef.Name == "" {
		if len(binds) == 1 {
			return binds[0], true
		}
		return v1alpha1.LoadBalancerBindAddress{}, false
	}
	for _, bind := range binds {
		if bind.Name == source.BindAddressRef.Name {
			return bind, true
		}
	}
	return v1alpha1.LoadBalancerBindAddress{}, false
}

func NetworkConfigContainsIP(network v1alpha1.NetworkConfig, ip net.IP) bool {
	for _, machineNetwork := range network.Spec.MachineNetwork {
		if _, cidr, err := net.ParseCIDR(machineNetwork.CIDR); err == nil && cidr.Contains(ip) {
			return true
		}
	}
	return false
}

func EndpointNetworkConfig(state v1alpha1.State, infra v1alpha1.ClusterInstall, address string) (v1alpha1.NetworkConfig, bool) {
	ip := net.ParseIP(address)
	if ip == nil {
		return v1alpha1.NetworkConfig{}, false
	}
	for _, network := range ClusterNetworkConfigs(state, infra) {
		if NetworkConfigContainsIP(network, ip) {
			return network, true
		}
	}
	return v1alpha1.NetworkConfig{}, false
}

func ClusterNetworkConfigs(state v1alpha1.State, infra v1alpha1.ClusterInstall) []v1alpha1.NetworkConfig {
	var out []v1alpha1.NetworkConfig
	for _, name := range ClusterConsumedNetworkConfigs(infra) {
		if network, ok := NetworkConfig(state, name); ok {
			out = append(out, network)
		}
	}
	for _, machine := range infra.Machines {
		if machine.Network.Spec == nil {
			continue
		}
		out = append(out, v1alpha1.NetworkConfig{
			Metadata: v1alpha1.Metadata{Name: infra.Metadata.Name + "/" + machine.Name},
			Spec:     *machine.Network.Spec,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if vi, vj := networkConfigHasIPv4(out[i]), networkConfigHasIPv4(out[j]); vi != vj {
			return vi
		}
		return out[i].Metadata.Name < out[j].Metadata.Name
	})
	return out
}

func networkConfigHasIPv4(network v1alpha1.NetworkConfig) bool {
	for _, machineNetwork := range network.Spec.MachineNetwork {
		if ip, _, err := net.ParseCIDR(machineNetwork.CIDR); err == nil && ip.To4() != nil {
			return true
		}
	}
	return false
}

func ClusterConsumedNetworkConfigs(infra v1alpha1.ClusterInstall) []string {
	used := map[string]bool{}
	for _, machine := range infra.Machines {
		if machine.Network.NetworkConfigRef.Name != "" {
			used[machine.Network.NetworkConfigRef.Name] = true
		}
	}
	names := make([]string, 0, len(used))
	for name := range used {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func MachineRouteAddress(state v1alpha1.State, machineName, addressName string, infra v1alpha1.ClusterInstall) string {
	if addressName != "" {
		if address, ok := NamedMachineAddress(state, machineName, addressName); ok {
			return address
		}
		return ""
	}
	return fallbackMachineRouteAddress(state, machineName, infra)
}

func NamedMachineAddress(state v1alpha1.State, machineName, addressName string) (string, bool) {
	machine, ok := Machine(state, machineName)
	if !ok {
		return "", false
	}
	return v1alpha1.MachineAddressByName(machine, addressName)
}

func fallbackMachineRouteAddress(state v1alpha1.State, machineName string, infra v1alpha1.ClusterInstall) string {
	machine, ok := Machine(state, machineName)
	if !ok {
		return ""
	}
	if address := v1alpha1.MachineSSHAddress(machine); address != "" && !IsLoopbackAlias(address) {
		return address
	}
	if gateway := PrimaryNetworkGateway(state, infra); gateway != "" {
		return gateway
	}
	return ""
}

func PrimaryNetworkGateway(state v1alpha1.State, infra v1alpha1.ClusterInstall) string {
	if network := PrimaryClusterNetworkConfig(state, infra); network != nil {
		return GatewayFromNetworkConfig(*network)
	}
	return ""
}

func PrimaryClusterNetworkConfig(state v1alpha1.State, infra v1alpha1.ClusterInstall) *v1alpha1.NetworkConfig {
	names := make([]string, 0, len(infra.Endpoints))
	for name := range infra.Endpoints {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if address := EndpointAddress(state, infra, name); address != "" {
			if network, ok := EndpointNetworkConfig(state, infra, address); ok {
				return &network
			}
		}
	}
	for _, network := range ClusterNetworkConfigs(state, infra) {
		return &network
	}
	return nil
}

func GatewayFromNetworkConfig(network v1alpha1.NetworkConfig) string {
	routes, ok := network.Spec.Template.NetworkConfig["routes"].(map[string]any)
	if !ok {
		return ""
	}
	config, ok := routes["config"].([]any)
	if !ok {
		return ""
	}
	for _, item := range config {
		route, ok := item.(map[string]any)
		if !ok {
			continue
		}
		destination, _ := route["destination"].(string)
		if destination != "" && destination != "0.0.0.0/0" && destination != "::/0" {
			continue
		}
		if nextHop, _ := route["next-hop-address"].(string); nextHop != "" {
			return nextHop
		}
	}
	return ""
}

func IsLoopbackAlias(s string) bool {
	if strings.EqualFold(s, "localhost") {
		return true
	}
	if addr, err := netip.ParseAddr(s); err == nil {
		return addr.IsLoopback() || addr.IsUnspecified()
	}
	return false
}
