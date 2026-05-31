package render

import (
	"fmt"
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func storageFailureDomain(cluster v1alpha1.StorageCluster) string {
	if stretch := cluster.Spec.Ceph.Topology.Stretch; stretch != nil && stretch.FailureDomain != "" {
		return stretch.FailureDomain
	}
	return "host"
}

type StorageAttachment struct {
	Binding v1alpha1.ClusterAddonBinding
	Storage v1alpha1.ClusterAddonBindingStorage
}

func storageAttachmentsByStorageCluster(state v1alpha1.State) map[string][]StorageAttachment {
	exports := map[string]v1alpha1.StorageExport{}
	for _, export := range state.StorageExports {
		exports[export.Metadata.Name] = export
	}
	out := map[string][]StorageAttachment{}
	for _, binding := range state.ClusterAddonBindings {
		for _, storage := range binding.Spec.Storage {
			export, ok := exports[storage.ExportRef.Name]
			if !ok {
				continue
			}
			out[export.Spec.StorageClusterRef.Name] = append(out[export.Spec.StorageClusterRef.Name], StorageAttachment{
				Binding: binding,
				Storage: storage,
			})
		}
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
		for _, address := range machine.NetworkConfig.Addresses {
			if len(address.IPv4) > 0 {
				return address.IPv4[0].IP
			}
			if len(address.IPv6) > 0 {
				return address.IPv6[0].IP
			}
		}
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

func storageAttachmentByName(state v1alpha1.State, bindingName, storageName string) (StorageAttachment, bool) {
	for _, binding := range state.ClusterAddonBindings {
		if binding.Metadata.Name != bindingName {
			continue
		}
		for _, storage := range binding.Spec.Storage {
			if storage.Name == storageName {
				return StorageAttachment{Binding: binding, Storage: storage}, true
			}
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

func storageClusterInfraByName(state v1alpha1.State, name string) (v1alpha1.ClusterInfra, bool) {
	for _, infra := range state.ClusterInfras {
		if infra.Metadata.Name == name {
			return infra, true
		}
	}
	return v1alpha1.ClusterInfra{}, false
}
