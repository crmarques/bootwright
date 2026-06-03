package topology

import (
	"fmt"
	"sort"

	"dario.cat/mergo"
	"github.com/crmarques/bootwright/api/v1alpha1"
)

func FailureDomain(cluster v1alpha1.StorageCluster) string {
	if stretch := cluster.Spec.Ceph.Topology.Stretch; stretch != nil && stretch.FailureDomain != "" {
		return stretch.FailureDomain
	}
	return "host"
}

func MonitorEndpoints(state v1alpha1.State, cluster v1alpha1.StorageCluster) []string {
	var endpoints []string
	for _, node := range cluster.Spec.Ceph.Topology.Nodes {
		if !NodeHasRole(node, v1alpha1.StorageCephRoleMON) {
			continue
		}
		if ip := NodeAddress(state, cluster, node.Name); ip != "" {
			endpoints = append(endpoints, fmt.Sprintf("%s=%s:6789", node.Name, ip))
		}
	}
	sort.Strings(endpoints)
	return endpoints
}

func NodeAddress(state v1alpha1.State, cluster v1alpha1.StorageCluster, node string) string {
	infra, ok := ClusterInfraByName(state, cluster.Spec.ClusterInfraRef.Name)
	if !ok {
		return ""
	}
	for _, machine := range infra.Spec.Components.Machines {
		if machine.Name != node {
			continue
		}
		return networkConfigPrimaryIP(machineNetworkConfigTemplate(state, infra, machine))
	}
	return ""
}

func CephHostsWithRole(cluster v1alpha1.StorageCluster, role string) []string {
	var hosts []string
	for _, node := range cluster.Spec.Ceph.Topology.Nodes {
		if NodeHasRole(node, role) {
			hosts = append(hosts, node.Name)
		}
	}
	sort.Strings(hosts)
	return hosts
}

func NodeHasRole(node v1alpha1.StorageCephNode, role string) bool {
	for _, item := range node.Roles {
		if item == role {
			return true
		}
	}
	return false
}

func FilesystemDefaultDataPool(fs v1alpha1.StorageFilesystem) string {
	for _, ref := range fs.Spec.CephFS.DataPoolRefs {
		if ref.Default {
			return ref.Name
		}
	}
	if len(fs.Spec.CephFS.DataPoolRefs) > 0 {
		return fs.Spec.CephFS.DataPoolRefs[0].Name
	}
	return ""
}

func ClusterByName(state v1alpha1.State, name string) (v1alpha1.StorageCluster, bool) {
	for _, cluster := range state.StorageClusters {
		if cluster.Metadata.Name == name {
			return cluster, true
		}
	}
	return v1alpha1.StorageCluster{}, false
}

func ExportByName(state v1alpha1.State, name string) (v1alpha1.StorageExport, bool) {
	for _, export := range state.StorageExports {
		if export.Metadata.Name == name {
			return export, true
		}
	}
	return v1alpha1.StorageExport{}, false
}

func FilesystemByName(state v1alpha1.State, name string) (v1alpha1.StorageFilesystem, bool) {
	for _, fs := range state.StorageFilesystems {
		if fs.Metadata.Name == name {
			return fs, true
		}
	}
	return v1alpha1.StorageFilesystem{}, false
}

func GatewayByName(state v1alpha1.State, name string) (v1alpha1.StorageObjectGateway, bool) {
	for _, gateway := range state.StorageObjectGateways {
		if gateway.Metadata.Name == name {
			return gateway, true
		}
	}
	return v1alpha1.StorageObjectGateway{}, false
}

func GatewayEndpoint(state v1alpha1.State, gateway v1alpha1.StorageObjectGateway, ref v1alpha1.EndpointRef) (v1alpha1.Endpoint, bool) {
	cluster, ok := ClusterByName(state, gateway.Spec.StorageClusterRef.Name)
	if !ok {
		return v1alpha1.Endpoint{}, false
	}
	infra, ok := ClusterInfraByName(state, cluster.Spec.ClusterInfraRef.Name)
	if !ok {
		return v1alpha1.Endpoint{}, false
	}
	endpoint, ok := infra.Spec.Endpoints[ref.Name]
	return endpoint, ok
}

func EndpointPort(endpoint v1alpha1.Endpoint, defaultPort int) int {
	if endpoint.Port != 0 {
		return endpoint.Port
	}
	return defaultPort
}

func CephadmVirtualIP(endpoint v1alpha1.Endpoint) string {
	if endpoint.PrefixLength > 0 {
		return fmt.Sprintf("%s/%d", endpoint.Address, endpoint.PrefixLength)
	}
	return endpoint.Address
}

func ClusterInfraByName(state v1alpha1.State, name string) (v1alpha1.ClusterInfra, bool) {
	for _, infra := range state.ClusterInfras {
		if infra.Metadata.Name == name {
			return infra, true
		}
	}
	return v1alpha1.ClusterInfra{}, false
}

func machineNetworkConfigTemplate(state v1alpha1.State, infra v1alpha1.ClusterInfra, machine v1alpha1.ClusterMachineComponent) map[string]any {
	network, ok := machineNetworkDefinition(state, infra, machine)
	if !ok {
		return nil
	}
	out := cloneYAMLMap(network.Spec.Template.NetworkConfig)
	if len(machine.NetworkConfig.Overrides) > 0 {
		mergeNetworkConfigOverrides(out, machine.NetworkConfig.Overrides)
	}
	return out
}

func machineNetworkDefinition(state v1alpha1.State, infra v1alpha1.ClusterInfra, machine v1alpha1.ClusterMachineComponent) (v1alpha1.NetworkConfig, bool) {
	if machine.NetworkConfig.Spec != nil {
		return v1alpha1.NetworkConfig{
			Metadata: v1alpha1.Metadata{Name: fmt.Sprintf("%s/%s", infra.Metadata.Name, machine.Name)},
			Spec:     *machine.NetworkConfig.Spec,
		}, true
	}
	if machine.NetworkConfig.Ref.Name == "" {
		return v1alpha1.NetworkConfig{}, false
	}
	for _, network := range state.NetworkConfigs {
		if network.Metadata.Name == machine.NetworkConfig.Ref.Name {
			return network, true
		}
	}
	return v1alpha1.NetworkConfig{}, false
}

func mergeNetworkConfigOverrides(base map[string]any, overrides map[string]any) {
	patch := cloneYAMLMap(overrides)
	mergeStructuredSequences(base, patch)
	_ = mergo.Merge(&base, patch, mergo.WithOverride)
}

func mergeStructuredSequences(base map[string]any, patch map[string]any) {
	for key, patchValue := range patch {
		baseMap, baseIsMap := base[key].(map[string]any)
		patchMap, patchIsMap := patchValue.(map[string]any)
		if baseIsMap && patchIsMap {
			mergeStructuredSequences(baseMap, patchMap)
			continue
		}
		baseSlice, baseIsSlice := base[key].([]any)
		patchSlice, patchIsSlice := patchValue.([]any)
		if !baseIsSlice || !patchIsSlice {
			continue
		}
		merged, ok := mergeStructuredSequence(baseSlice, patchSlice)
		if !ok {
			continue
		}
		base[key] = merged
		delete(patch, key)
	}
}

func mergeStructuredSequence(base []any, patch []any) ([]any, bool) {
	if len(patch) == 0 {
		return cloneYAMLValue(base).([]any), true
	}
	if sequenceUsesName(patch) {
		return mergeNamedSequence(base, patch), true
	}
	if sequenceUsesMaps(base) && sequenceUsesMaps(patch) {
		return mergePositionalMapSequence(base, patch), true
	}
	return nil, false
}

func mergeNamedSequence(base []any, patch []any) []any {
	out := cloneYAMLValue(base).([]any)
	index := map[string]map[string]any{}
	for _, item := range out {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := entry["name"].(string)
		if name != "" {
			index[name] = entry
		}
	}
	for _, item := range patch {
		entry := item.(map[string]any)
		name := entry["name"].(string)
		if baseEntry, ok := index[name]; ok {
			mergeNetworkConfigOverrides(baseEntry, entry)
			continue
		}
		out = append(out, cloneYAMLMap(entry))
	}
	return out
}

func mergePositionalMapSequence(base []any, patch []any) []any {
	out := cloneYAMLValue(base).([]any)
	for i, item := range patch {
		entry := item.(map[string]any)
		if i < len(out) {
			baseEntry, ok := out[i].(map[string]any)
			if ok {
				mergeNetworkConfigOverrides(baseEntry, entry)
				continue
			}
		}
		out = append(out, cloneYAMLMap(entry))
	}
	return out
}

func sequenceUsesName(items []any) bool {
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			return false
		}
		name, _ := entry["name"].(string)
		if name == "" {
			return false
		}
	}
	return true
}

func sequenceUsesMaps(items []any) bool {
	for _, item := range items {
		if _, ok := item.(map[string]any); !ok {
			return false
		}
	}
	return true
}

func networkConfigPrimaryIP(config map[string]any) string {
	if ip := networkConfigInterfaceIP(config, "", "ipv4"); ip != "" {
		return ip
	}
	return networkConfigInterfaceIP(config, "", "ipv6")
}

func networkConfigInterfaceIP(config map[string]any, interfaceName string, family string) string {
	raw, ok := config["interfaces"].([]any)
	if !ok {
		return ""
	}
	if family == "" {
		family = "ipv4"
	}
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := entry["name"].(string)
		if interfaceName != "" && name != interfaceName {
			continue
		}
		if ip := networkConfigFamilyIP(entry, family); ip != "" {
			return ip
		}
	}
	return ""
}

func networkConfigFamilyIP(entry map[string]any, family string) string {
	familyConfig, ok := entry[family].(map[string]any)
	if !ok {
		return ""
	}
	rawAddresses, ok := familyConfig["address"].([]any)
	if !ok {
		return ""
	}
	for _, raw := range rawAddresses {
		address, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		ip, _ := address["ip"].(string)
		if ip != "" {
			return ip
		}
	}
	return ""
}

func cloneYAMLMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = cloneYAMLValue(v)
	}
	return out
}

func cloneYAMLValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return cloneYAMLMap(t)
	case []any:
		out := make([]any, 0, len(t))
		for _, c := range t {
			out = append(out, cloneYAMLValue(c))
		}
		return out
	default:
		return t
	}
}
