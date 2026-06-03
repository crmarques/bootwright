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

func Host(state v1alpha1.State, name string) (v1alpha1.Host, bool) {
	for _, host := range state.Hosts {
		if host.Metadata.Name == name {
			return host, true
		}
	}
	return v1alpha1.Host{}, false
}

func NetworkConfig(state v1alpha1.State, name string) (v1alpha1.NetworkConfig, bool) {
	for _, network := range state.NetworkConfigs {
		if network.Metadata.Name == name {
			return network, true
		}
	}
	return v1alpha1.NetworkConfig{}, false
}

func ClusterInfra(state v1alpha1.State, name string) (v1alpha1.ClusterInfra, bool) {
	for _, infra := range state.ClusterInfras {
		if infra.Metadata.Name == name {
			return infra, true
		}
	}
	return v1alpha1.ClusterInfra{}, false
}

func InfraComponent(state v1alpha1.State, name string) (v1alpha1.InfraComponent, bool) {
	for _, component := range state.InfraComponents {
		if component.Metadata.Name == name {
			return component, true
		}
	}
	return v1alpha1.InfraComponent{}, false
}

func MachineProfile(provider v1alpha1.InfraProvider, name string) (v1alpha1.MachineProfileCapability, bool) {
	for _, profile := range provider.Spec.MachineProfiles {
		if profile.Name == name {
			return profile, true
		}
	}
	return v1alpha1.MachineProfileCapability{}, false
}

func Machine(provider v1alpha1.InfraProvider, name string) (v1alpha1.MachineCapability, bool) {
	for _, machine := range provider.Spec.Machines {
		if machine.Name == name {
			return machine, true
		}
	}
	return v1alpha1.MachineCapability{}, false
}

func HostHasCapability(host v1alpha1.Host, want string) bool {
	for _, capability := range host.Spec.Capabilities {
		if capability == want {
			return true
		}
	}
	return false
}

func ClusterInfraNames(cluster v1alpha1.ContainerCluster) []string {
	seen := map[string]bool{}
	var out []string
	for _, node := range cluster.Spec.Nodes {
		ref := node.InfraNodeRef.ClusterInfra
		if ref == "" || seen[ref] {
			continue
		}
		seen[ref] = true
		out = append(out, ref)
	}
	sort.Strings(out)
	return out
}

func ClusterForInfra(state v1alpha1.State, infra v1alpha1.ClusterInfra) (v1alpha1.ContainerCluster, bool) {
	for _, cluster := range state.ContainerClusters {
		for _, ref := range ClusterInfraNames(cluster) {
			if ref == infra.Metadata.Name {
				return cluster, true
			}
		}
	}
	return v1alpha1.ContainerCluster{}, false
}

func ClusterNodesForInfra(state v1alpha1.State, infra v1alpha1.ClusterInfra) map[string]v1alpha1.OCPNodeSpec {
	for _, cluster := range state.ContainerClusters {
		out := map[string]v1alpha1.OCPNodeSpec{}
		for _, node := range cluster.Spec.Nodes {
			if node.InfraNodeRef.ClusterInfra == infra.Metadata.Name {
				out[node.Hostname] = node
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

func EndpointAddress(state v1alpha1.State, infra v1alpha1.ClusterInfra, name string) string {
	endpoint, ok := infra.Spec.Endpoints[name]
	if !ok {
		return ""
	}
	if endpoint.Source.Type == v1alpha1.EndpointSourceInfraComponent {
		if bind, ok := LoadBalancerBindAddress(state, endpoint.Source); ok {
			return bind.IP
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
	if source.BindAddress == "" {
		if len(binds) == 1 {
			return binds[0], true
		}
		return v1alpha1.LoadBalancerBindAddress{}, false
	}
	for _, bind := range binds {
		if bind.Name == source.BindAddress {
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

func EndpointNetworkConfig(state v1alpha1.State, infra v1alpha1.ClusterInfra, address string) (v1alpha1.NetworkConfig, bool) {
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

func ClusterNetworkConfigs(state v1alpha1.State, infra v1alpha1.ClusterInfra) []v1alpha1.NetworkConfig {
	var out []v1alpha1.NetworkConfig
	for _, name := range ClusterConsumedNetworkConfigs(infra) {
		if network, ok := NetworkConfig(state, name); ok {
			out = append(out, network)
		}
	}
	for _, machine := range infra.Spec.Components.Nodes {
		if machine.Network.Spec == nil {
			continue
		}
		out = append(out, v1alpha1.NetworkConfig{
			Metadata: v1alpha1.Metadata{Name: infra.Metadata.Name + "/" + machine.Name},
			Spec:     *machine.Network.Spec,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Metadata.Name < out[j].Metadata.Name
	})
	return out
}

func ClusterConsumedNetworkConfigs(infra v1alpha1.ClusterInfra) []string {
	used := map[string]bool{}
	for _, machine := range infra.Spec.Components.Nodes {
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

func ClusterFacingHostAddress(state v1alpha1.State, hostName string, infra v1alpha1.ClusterInfra) string {
	return HostRouteAddress(state, hostName, "", infra)
}

func HostRouteAddress(state v1alpha1.State, hostName, addressName string, infra v1alpha1.ClusterInfra) string {
	if addressName != "" {
		if address, ok := NamedHostAddress(state, hostName, addressName); ok {
			return address
		}
		return ""
	}
	return fallbackHostRouteAddress(state, hostName, infra)
}

func NamedHostAddress(state v1alpha1.State, hostName, addressName string) (string, bool) {
	host, ok := Host(state, hostName)
	if !ok {
		return "", false
	}
	return v1alpha1.HostAddressByName(host, addressName)
}

func fallbackHostRouteAddress(state v1alpha1.State, hostName string, infra v1alpha1.ClusterInfra) string {
	host, ok := Host(state, hostName)
	if !ok {
		return ""
	}
	if address := v1alpha1.HostSSHAddress(host); address != "" && !IsLoopbackAlias(address) {
		return address
	}
	if gateway := PrimaryNetworkGateway(state, infra); gateway != "" {
		return gateway
	}
	return ""
}

func PrimaryNetworkGateway(state v1alpha1.State, infra v1alpha1.ClusterInfra) string {
	if network := PrimaryClusterNetworkConfig(state, infra); network != nil {
		return GatewayFromNetworkConfig(*network)
	}
	return ""
}

func PrimaryClusterNetworkConfig(state v1alpha1.State, infra v1alpha1.ClusterInfra) *v1alpha1.NetworkConfig {
	names := make([]string, 0, len(infra.Spec.Endpoints))
	for name := range infra.Spec.Endpoints {
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
