package topology

import (
	"fmt"
	"sort"

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
	return NodeAddressByRef(state, cluster, node, cluster.Spec.Ceph.Cephadm.AddressRef.Name)
}

func NodeAddressByRef(state v1alpha1.State, cluster v1alpha1.StorageCluster, node string, addressName string) string {
	machine, ok := NodeMachine(state, cluster, node)
	if !ok {
		return ""
	}
	if addressName == "" && machine.Spec.OS.SSH != nil {
		addressName = machine.Spec.OS.SSH.AddressName
	}
	address, _ := v1alpha1.MachineAddressByName(machine, addressName)
	return address
}

func NodeMachine(state v1alpha1.State, cluster v1alpha1.StorageCluster, node string) (v1alpha1.Machine, bool) {
	cephNode, ok := CephNodeByName(cluster, node)
	if !ok || cephNode.MachineRef.Name == "" {
		return v1alpha1.Machine{}, false
	}
	return MachineByName(state, cephNode.MachineRef.Name)
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
	for _, cluster := range state.ContainerClusters {
		if endpoint, ok := cluster.Spec.Install.Endpoints[ref.Name]; ok {
			return endpoint, true
		}
	}
	return v1alpha1.Endpoint{}, false
}

func CephadmVirtualIP(endpoint v1alpha1.Endpoint) string {
	if endpoint.PrefixLength > 0 {
		return fmt.Sprintf("%s/%d", endpoint.Address, endpoint.PrefixLength)
	}
	return endpoint.Address
}

func EndpointPort(endpoint v1alpha1.Endpoint, defaultPort int) int {
	if endpoint.Port != 0 {
		return endpoint.Port
	}
	return defaultPort
}

func CephNodeByName(cluster v1alpha1.StorageCluster, name string) (v1alpha1.StorageCephNode, bool) {
	if cluster.Spec.Ceph == nil {
		return v1alpha1.StorageCephNode{}, false
	}
	for _, node := range cluster.Spec.Ceph.Topology.Nodes {
		if node.Name == name {
			return node, true
		}
	}
	return v1alpha1.StorageCephNode{}, false
}

func MachineByName(state v1alpha1.State, name string) (v1alpha1.Machine, bool) {
	for _, machine := range state.Machines {
		if machine.Metadata.Name == name {
			return machine, true
		}
	}
	return v1alpha1.Machine{}, false
}
