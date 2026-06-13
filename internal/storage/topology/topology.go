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
	for _, node := range cluster.Spec.Ceph.Topology.Hosts {
		if !NodeHasRole(node, v1alpha1.StorageCephRoleMON) {
			continue
		}
		if ip := NodeAddress(state, cluster, node.Hostname); ip != "" {
			endpoints = append(endpoints, fmt.Sprintf("%s=%s:6789", node.Hostname, ip))
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
	if addressName == "" && machine.Spec.Access.SSH != nil {
		addressName = machine.Spec.Access.SSH.AddressRef.Name
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
	for _, node := range cluster.Spec.Ceph.Topology.Hosts {
		if NodeHasRole(node, role) {
			hosts = append(hosts, node.Hostname)
		}
	}
	sort.Strings(hosts)
	return hosts
}

// HostByName returns the topology host identified by an authored token: its
// registered cephadm hostname (the FQDN) or its backing machine name. The two
// diverge once hostnames are fully qualified, so both spellings resolve.
func HostByName(cluster v1alpha1.StorageCluster, name string) (v1alpha1.StorageCephHost, bool) {
	for _, host := range cluster.Spec.Ceph.Topology.Hosts {
		if host.Hostname == name || host.MachineRef.Name == name {
			return host, true
		}
	}
	return v1alpha1.StorageCephHost{}, false
}

// CanonicalHostname maps an authored host token — a topology host's machine
// name or its registered hostname — to the registered hostname cephadm uses.
// An unmatched token passes through unchanged.
func CanonicalHostname(cluster v1alpha1.StorageCluster, token string) string {
	if host, ok := HostByName(cluster, token); ok && host.Hostname != "" {
		return host.Hostname
	}
	return token
}

// ResolvePlacement resolves a placement to concrete topology hostnames: the
// explicit hosts when authored, else every topology host carrying the role
// (every topology host when role is empty — passthrough services have no
// role), optionally narrowed to the named sites.
func ResolvePlacement(cluster v1alpha1.StorageCluster, placement v1alpha1.StoragePlacement, role string) []string {
	var base []string
	switch {
	case len(placement.Hosts) > 0:
		// Authored tokens may be machine names; canonicalize to the registered
		// hostname so cephadm matches them against the host spec.
		for _, token := range placement.Hosts {
			base = append(base, CanonicalHostname(cluster, token))
		}
	case role != "":
		base = CephHostsWithRole(cluster, role)
	default:
		for _, host := range cluster.Spec.Ceph.Topology.Hosts {
			base = append(base, host.Hostname)
		}
		sort.Strings(base)
	}
	if len(placement.Sites) == 0 {
		return base
	}
	sites := map[string]bool{}
	for _, site := range placement.Sites {
		sites[site] = true
	}
	var out []string
	for _, hostname := range base {
		if host, ok := HostByName(cluster, hostname); ok && sites[host.Site] {
			out = append(out, hostname)
		}
	}
	return out
}

func NodeHasRole(node v1alpha1.StorageCephHost, role string) bool {
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

// GatewayPublicEndpoint returns the storage-owned public S3 endpoint of the RGW
// service. Ownership is on the gateway itself, so no ContainerCluster lookup is
// involved and a storage-only object store needs no consumer cluster.
func GatewayPublicEndpoint(gateway v1alpha1.StorageObjectGateway) (v1alpha1.Endpoint, bool) {
	public := gateway.Spec.Public
	if public.DNSName == "" {
		return v1alpha1.Endpoint{}, false
	}
	return v1alpha1.Endpoint{DNSName: public.DNSName, Scheme: public.Scheme, Port: public.Port}, true
}

// GatewayIngressEndpoint returns one storage-owned RGW ingress VIP.
func GatewayIngressEndpoint(ingress v1alpha1.StorageObjectGatewayIngress) (v1alpha1.Endpoint, bool) {
	if ingress.Address == "" {
		return v1alpha1.Endpoint{}, false
	}
	return v1alpha1.Endpoint{Address: ingress.Address, PrefixLength: ingress.PrefixLength, InterfaceNetworks: ingress.VirtualInterfaceNetworks}, true
}

// ManagementIngressEndpoint returns the storage-owned management VIP fronting
// the mgmt-gateway.
func ManagementIngressEndpoint(ingress v1alpha1.StorageCephManagementIngress) (v1alpha1.Endpoint, bool) {
	if ingress.Address == "" {
		return v1alpha1.Endpoint{}, false
	}
	return v1alpha1.Endpoint{Address: ingress.Address, PrefixLength: ingress.PrefixLength, InterfaceNetworks: ingress.VirtualInterfaceNetworks}, true
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

func CephNodeByName(cluster v1alpha1.StorageCluster, name string) (v1alpha1.StorageCephHost, bool) {
	if cluster.Spec.Ceph == nil {
		return v1alpha1.StorageCephHost{}, false
	}
	for _, node := range cluster.Spec.Ceph.Topology.Hosts {
		if node.Hostname == name || node.MachineRef.Name == name {
			return node, true
		}
	}
	return v1alpha1.StorageCephHost{}, false
}

func MachineByName(state v1alpha1.State, name string) (v1alpha1.Machine, bool) {
	for _, machine := range state.Machines {
		if machine.Metadata.Name == name {
			return machine, true
		}
	}
	return v1alpha1.Machine{}, false
}
