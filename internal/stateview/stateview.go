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

func LoadBalancer(provider v1alpha1.InfraProvider, name string) (v1alpha1.LoadBalancerCapability, bool) {
	for _, loadBalancer := range provider.Spec.LoadBalancers {
		if loadBalancer.Name == name {
			return loadBalancer, true
		}
	}
	return v1alpha1.LoadBalancerCapability{}, false
}

func Proxy(provider v1alpha1.InfraProvider, name string) (v1alpha1.ProxyCapability, bool) {
	for _, proxy := range provider.Spec.Proxies {
		if proxy.Name == name {
			return proxy, true
		}
	}
	return v1alpha1.ProxyCapability{}, false
}

func DNS(provider v1alpha1.InfraProvider, name string) (v1alpha1.DNSCapability, bool) {
	for _, dns := range provider.Spec.DNS {
		if dns.Name == name {
			return dns, true
		}
	}
	return v1alpha1.DNSCapability{}, false
}

func Registry(provider v1alpha1.InfraProvider, name string) (v1alpha1.RegistryCapability, bool) {
	for _, registry := range provider.Spec.Registries {
		if registry.Name == name {
			return registry, true
		}
	}
	return v1alpha1.RegistryCapability{}, false
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
		ref := node.MachineRef.ClusterInfra
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
			if node.MachineRef.ClusterInfra == infra.Metadata.Name {
				out[node.Hostname] = node
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

func EndpointAddress(infra v1alpha1.ClusterInfra, name string) string {
	endpoint, ok := infra.Spec.Endpoints[name]
	if !ok {
		return ""
	}
	switch {
	case endpoint.VIP != "":
		return endpoint.VIP
	case endpoint.ExternalVIP != "":
		return endpoint.ExternalVIP
	case endpoint.ProvidedBy != nil:
		if bind, ok := LoadBalancerBindAddress(infra, *endpoint.ProvidedBy); ok {
			return bind.IP
		}
	}
	return ""
}

func LoadBalancerBindAddress(infra v1alpha1.ClusterInfra, ref v1alpha1.EndpointProvidedBy) (v1alpha1.LoadBalancerBindAddress, bool) {
	for _, loadBalancer := range infra.Spec.Components.LoadBalancers {
		if loadBalancer.Name != ref.LoadBalancer || len(loadBalancer.BindAddresses) == 0 {
			continue
		}
		if ref.Address == "" {
			if len(loadBalancer.BindAddresses) == 1 {
				return loadBalancer.BindAddresses[0], true
			}
			return v1alpha1.LoadBalancerBindAddress{}, false
		}
		for _, bind := range loadBalancer.BindAddresses {
			if bind.Name == ref.Address {
				return bind, true
			}
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
	for _, name := range ClusterConsumedNetworkConfigs(infra) {
		network, ok := NetworkConfig(state, name)
		if ok && NetworkConfigContainsIP(network, ip) {
			return network, true
		}
	}
	return v1alpha1.NetworkConfig{}, false
}

func ClusterConsumedNetworkConfigs(infra v1alpha1.ClusterInfra) []string {
	used := map[string]bool{}
	for _, machine := range infra.Spec.Components.Machines {
		if machine.NetworkConfig.Ref.Name != "" {
			used[machine.NetworkConfig.Ref.Name] = true
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
	if address := v1alpha1.HostSSHAddress(host); address != "" && !isLoopbackAlias(address) {
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
	if address := EndpointAddress(infra, v1alpha1.EndpointAPI); address != "" {
		if network, ok := EndpointNetworkConfig(state, infra, address); ok {
			return &network
		}
	}
	for _, name := range []string{v1alpha1.EndpointAPIInt, v1alpha1.EndpointIngress} {
		if address := EndpointAddress(infra, name); address != "" {
			if network, ok := EndpointNetworkConfig(state, infra, address); ok {
				return &network
			}
		}
	}
	for _, machine := range infra.Spec.Components.Machines {
		if machine.NetworkConfig.Ref.Name != "" {
			if network, ok := NetworkConfig(state, machine.NetworkConfig.Ref.Name); ok {
				return &network
			}
		}
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

func isLoopbackAlias(s string) bool {
	if strings.EqualFold(s, "localhost") {
		return true
	}
	if addr, err := netip.ParseAddr(s); err == nil {
		return addr.IsLoopback() || addr.IsUnspecified()
	}
	return false
}
