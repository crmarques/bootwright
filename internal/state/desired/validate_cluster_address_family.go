package desiredstate

import (
	"fmt"
	"net"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/state/view"
)

type clusterAddressFamilyValue struct {
	field  string
	value  string
	family string
}

func validateClusterSingleAddressFamily(state v1alpha1.State, ocp v1alpha1.ContainerCluster, networkConfigs map[string]v1alpha1.NetworkConfig) []string {
	values := clusterAddressFamilyValues(state, ocp, networkConfigs)
	if len(values) < 2 {
		return nil
	}
	anchor := values[0]
	for _, value := range values[1:] {
		if value.family == anchor.family {
			continue
		}
		return []string{fmt.Sprintf("ContainerCluster/%s mixes IP address families: %s %s is %s but %s %s is %s; single-stack is the current v1alpha1 scope, so one container cluster carries one address family. spec.install.endpoints.<slot>.address is a single address and cannot express the one VIP per family a dual-stack cluster needs, so the mixed input would render a single-stack install-config nobody authored. Author one family for this cluster",
			ocp.Metadata.Name, anchor.field, anchor.value, anchor.family, value.field, value.value, value.family)}
	}
	return nil
}

func clusterAddressFamilyValues(state v1alpha1.State, ocp v1alpha1.ContainerCluster, networkConfigs map[string]v1alpha1.NetworkConfig) []clusterAddressFamilyValue {
	var values []clusterAddressFamilyValue
	ci, hasInstall := stateview.ClusterInstallForContainerCluster(state, ocp)
	if hasInstall {
		values = append(values, clusterMachineNetworkFamilyValues(ci, networkConfigs)...)
	}
	if networking := ocp.Spec.Networking; networking != nil {
		for i, entry := range networking.ClusterNetwork {
			values = appendCIDRAddressFamily(values, fmt.Sprintf("spec.networking.clusterNetwork[%d].cidr", i), entry.CIDR)
		}
		for i, cidr := range networking.ServiceNetwork {
			values = appendCIDRAddressFamily(values, fmt.Sprintf("spec.networking.serviceNetwork[%d]", i), cidr)
		}
	}
	if hasInstall {
		values = append(values, clusterEndpointFamilyValues(state, ci)...)
	}
	return values
}

func clusterMachineNetworkFamilyValues(ci v1alpha1.ClusterInstall, networkConfigs map[string]v1alpha1.NetworkConfig) []clusterAddressFamilyValue {
	var values []clusterAddressFamilyValue
	for _, name := range stateview.ClusterConsumedNetworkConfigs(ci) {
		config, ok := networkConfigs[name]
		if !ok {
			continue
		}
		for i, machineNetwork := range config.Spec.MachineNetwork {
			values = appendCIDRAddressFamily(values, fmt.Sprintf("NetworkConfig/%s spec.machineNetwork[%d].cidr", name, i), machineNetwork.CIDR)
		}
	}
	for _, machine := range ci.Machines {
		if machine.Network.Spec == nil {
			continue
		}
		for i, machineNetwork := range machine.Network.Spec.MachineNetwork {
			values = appendCIDRAddressFamily(values, fmt.Sprintf("Machine/%s spec.network.config.spec.machineNetwork[%d].cidr", machine.Name, i), machineNetwork.CIDR)
		}
	}
	return values
}

func clusterEndpointFamilyValues(state v1alpha1.State, ci v1alpha1.ClusterInstall) []clusterAddressFamilyValue {
	var values []clusterAddressFamilyValue
	for _, name := range []string{v1alpha1.EndpointAPI, v1alpha1.EndpointAPIInt, v1alpha1.EndpointIngress} {
		endpoint, ok := ci.Endpoints[name]
		if !ok {
			continue
		}
		address := stateview.EndpointAddress(state, ci, name)
		family, valid := addressIPFamily(address)
		if !valid {
			continue
		}
		field := "spec.install.endpoints." + name + ".address"
		if endpoint.Source.Type == v1alpha1.EndpointSourceInfraComponent {
			field = "spec.install.endpoints." + name + " source.bindAddressRef resolves to"
		}
		values = append(values, clusterAddressFamilyValue{field: field, value: strings.TrimSpace(address), family: family})
	}
	return values
}

func appendCIDRAddressFamily(values []clusterAddressFamilyValue, field, cidr string) []clusterAddressFamilyValue {
	family, ok := cidrIPFamily(cidr)
	if !ok {
		return values
	}
	return append(values, clusterAddressFamilyValue{field: field, value: strings.TrimSpace(cidr), family: family})
}

func addressIPFamily(address string) (string, bool) {
	ip := net.ParseIP(strings.TrimSpace(address))
	if ip == nil {
		return "", false
	}
	if ip.To4() != nil {
		return "IPv4", true
	}
	return "IPv6", true
}
