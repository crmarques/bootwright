package render

import (
	"fmt"
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	addoninputs "github.com/crmarques/bootwright/internal/addons/inputs"
)

func storageFailureDomain(cluster v1alpha1.StorageCluster) string {
	if stretch := cluster.Spec.Ceph.Topology.Stretch; stretch != nil && stretch.FailureDomain != "" {
		return stretch.FailureDomain
	}
	return "host"
}

type StorageAttachment struct {
	Binding v1alpha1.ClusterAddonBinding
	Addon   v1alpha1.ClusterAddonBindingAddon
	Input   v1alpha1.ClusterAddonBindingInput
}

func storageAttachmentsByStorageCluster(state v1alpha1.State) map[string][]StorageAttachment {
	exports := map[string]v1alpha1.StorageExport{}
	for _, export := range state.StorageExports {
		exports[export.Metadata.Name] = export
	}
	out := map[string][]StorageAttachment{}
	for _, effect := range addoninputs.EffectBindings(state, v1alpha1.ClusterAddonInputEffectStorageExportAttachment, v1alpha1.ClusterAddonProvidesDataFoundation) {
		exportRef := addoninputs.LocalObjectReferenceValue(effect.Input.Values, "exportRef")
		export, ok := exports[exportRef.Name]
		if !ok {
			continue
		}
		out[export.Spec.StorageClusterRef.Name] = append(out[export.Spec.StorageClusterRef.Name], StorageAttachment{
			Binding: effect.Binding,
			Addon:   effect.Addon,
			Input:   effect.Input,
		})
	}
	return out
}

func storageMonitorEndpoints(state v1alpha1.State, cluster v1alpha1.StorageCluster) []string {
	var endpoints []string
	for _, node := range cluster.Spec.Ceph.Topology.Nodes {
		if !storageNodeHasRole(node, v1alpha1.StorageCephRoleMON) {
			continue
		}
		if ip := storageNodeAddress(state, cluster, node.Name); ip != "" {
			endpoints = append(endpoints, fmt.Sprintf("%s=%s:6789", node.Name, ip))
		}
	}
	sort.Strings(endpoints)
	return endpoints
}

func storageNodeAddress(state v1alpha1.State, cluster v1alpha1.StorageCluster, node string) string {
	infra, ok := storageClusterInfraByName(state, cluster.Spec.ClusterInfraRef.Name)
	if !ok {
		return ""
	}
	for _, machine := range infra.Spec.Components.Machines {
		if machine.Name != node {
			continue
		}
		return networkConfigPrimaryIP(agentNetworkConfig(state, infra, machine, ""))
	}
	return ""
}

func storageCephHostsWithRole(cluster v1alpha1.StorageCluster, role string) []string {
	var hosts []string
	for _, node := range cluster.Spec.Ceph.Topology.Nodes {
		if storageNodeHasRole(node, role) {
			hosts = append(hosts, node.Name)
		}
	}
	sort.Strings(hosts)
	return hosts
}

func storageNodeHasRole(node v1alpha1.StorageCephNode, role string) bool {
	for _, item := range node.Roles {
		if item == role {
			return true
		}
	}
	return false
}

func storageFilesystemDefaultDataPool(fs v1alpha1.StorageFilesystem) string {
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

func storageClusterByName(state v1alpha1.State, name string) (v1alpha1.StorageCluster, bool) {
	for _, cluster := range state.StorageClusters {
		if cluster.Metadata.Name == name {
			return cluster, true
		}
	}
	return v1alpha1.StorageCluster{}, false
}

func storageAttachmentByName(state v1alpha1.State, clusterName, addonName, inputName string) (StorageAttachment, bool) {
	for _, effect := range addoninputs.EffectBindings(state, v1alpha1.ClusterAddonInputEffectStorageExportAttachment, v1alpha1.ClusterAddonProvidesDataFoundation) {
		if effect.Binding.Spec.ClusterRef.Name != clusterName {
			continue
		}
		if effect.Addon.Name == addonName && effect.Input.Name == inputName {
			return StorageAttachment{Binding: effect.Binding, Addon: effect.Addon, Input: effect.Input}, true
		}
	}
	return StorageAttachment{}, false
}

func storageExportByName(state v1alpha1.State, name string) (v1alpha1.StorageExport, bool) {
	for _, export := range state.StorageExports {
		if export.Metadata.Name == name {
			return export, true
		}
	}
	return v1alpha1.StorageExport{}, false
}

func storageFilesystemByName(state v1alpha1.State, name string) (v1alpha1.StorageFilesystem, bool) {
	for _, fs := range state.StorageFilesystems {
		if fs.Metadata.Name == name {
			return fs, true
		}
	}
	return v1alpha1.StorageFilesystem{}, false
}

func storageGatewayByName(state v1alpha1.State, name string) (v1alpha1.StorageObjectGateway, bool) {
	for _, gateway := range state.StorageObjectGateways {
		if gateway.Metadata.Name == name {
			return gateway, true
		}
	}
	return v1alpha1.StorageObjectGateway{}, false
}

func storageGatewayEndpoint(state v1alpha1.State, gateway v1alpha1.StorageObjectGateway, ref v1alpha1.EndpointRef) (v1alpha1.Endpoint, bool) {
	cluster, ok := storageClusterByName(state, gateway.Spec.StorageClusterRef.Name)
	if !ok {
		return v1alpha1.Endpoint{}, false
	}
	infra, ok := storageClusterInfraByName(state, cluster.Spec.ClusterInfraRef.Name)
	if !ok {
		return v1alpha1.Endpoint{}, false
	}
	endpoint, ok := infra.Spec.Endpoints[ref.Name]
	return endpoint, ok
}

func endpointPort(endpoint v1alpha1.Endpoint, defaultPort int) int {
	if endpoint.Port != 0 {
		return endpoint.Port
	}
	return defaultPort
}

func cephadmVirtualIP(endpoint v1alpha1.Endpoint) string {
	if endpoint.PrefixLength > 0 {
		return fmt.Sprintf("%s/%d", endpoint.Address, endpoint.PrefixLength)
	}
	return endpoint.Address
}

func storageClusterInfraByName(state v1alpha1.State, name string) (v1alpha1.ClusterInfra, bool) {
	for _, infra := range state.ClusterInfras {
		if infra.Metadata.Name == name {
			return infra, true
		}
	}
	return v1alpha1.ClusterInfra{}, false
}
