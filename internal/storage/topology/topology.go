package topology

import (
	"fmt"
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	stateview "github.com/crmarques/bootwright/internal/state/view"
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
	return stateview.Machine(state, cephNode.MachineRef.Name)
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

func HostByName(cluster v1alpha1.StorageCluster, name string) (v1alpha1.StorageCephHost, bool) {
	for _, host := range cluster.Spec.Ceph.Topology.Hosts {
		if host.Hostname == name || stateview.NodeShortName(host.Hostname) == name {
			return host, true
		}
	}
	return v1alpha1.StorageCephHost{}, false
}

func CanonicalHostname(cluster v1alpha1.StorageCluster, token string) string {
	if host, ok := HostByName(cluster, token); ok && host.Hostname != "" {
		return host.Hostname
	}
	return token
}

func ResolvePlacement(cluster v1alpha1.StorageCluster, placement v1alpha1.StoragePlacement, role string) []string {
	var base []string
	switch {
	case len(placement.Hosts) > 0:
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

func GatewayPublicEndpoint(gateway v1alpha1.StorageObjectGateway) (v1alpha1.Endpoint, bool) {
	public := gateway.Spec.Public
	if public.DNSName == "" {
		return v1alpha1.Endpoint{}, false
	}
	return v1alpha1.Endpoint{DNSName: public.DNSName, Scheme: public.Scheme, Port: public.Port}, true
}

func GatewayIngressEndpoint(ingress v1alpha1.StorageObjectGatewayIngress) (v1alpha1.Endpoint, bool) {
	if ingress.Address == "" {
		return v1alpha1.Endpoint{}, false
	}
	return v1alpha1.Endpoint{Address: ingress.Address, PrefixLength: ingress.PrefixLength, InterfaceNetworks: ingress.VirtualInterfaceNetworks}, true
}

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
		if node.Hostname == name || stateview.NodeShortName(node.Hostname) == name || node.MachineRef.Name == name {
			return node, true
		}
	}
	return v1alpha1.StorageCephHost{}, false
}

func OSDHostDataDevices(cluster v1alpha1.StorageCluster, host v1alpha1.StorageCephHost) []string {
	if cluster.Spec.Ceph == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	add := func(paths []string) {
		for _, p := range paths {
			if p == "" || seen[p] {
				continue
			}
			seen[p] = true
			out = append(out, p)
		}
	}
	add(host.Devices)
	add(dataDeviceStaticPaths(host.OSD))
	name := host.Hostname
	for i := range cluster.Spec.Ceph.Topology.OSDDrivegroups {
		dg := cluster.Spec.Ceph.Topology.OSDDrivegroups[i]
		for _, placed := range ResolvePlacement(cluster, dg.Placement, v1alpha1.StorageCephRoleOSD) {
			if placed == name {
				add(dataDeviceStaticPaths(&cluster.Spec.Ceph.Topology.OSDDrivegroups[i].OSD))
				break
			}
		}
	}
	return out
}

func dataDeviceStaticPaths(osd *v1alpha1.StorageCephHostOSD) []string {
	if osd == nil || osd.DataDevices == nil {
		return nil
	}
	var paths []string
	paths = append(paths, osd.DataDevices.Paths...)
	for _, p := range osd.DataDevices.PathSpecs {
		if p.Path != "" {
			paths = append(paths, p.Path)
		}
	}
	return paths
}

func OSDHostAllStaticDevices(cluster v1alpha1.StorageCluster, host v1alpha1.StorageCephHost) []string {
	if cluster.Spec.Ceph == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	add := func(paths []string) {
		for _, p := range paths {
			if p == "" || seen[p] {
				continue
			}
			seen[p] = true
			out = append(out, p)
		}
	}
	add(host.Devices)
	add(allDeviceStaticPaths(host.OSD))
	name := host.Hostname
	for i := range cluster.Spec.Ceph.Topology.OSDDrivegroups {
		dg := cluster.Spec.Ceph.Topology.OSDDrivegroups[i]
		for _, placed := range ResolvePlacement(cluster, dg.Placement, v1alpha1.StorageCephRoleOSD) {
			if placed == name {
				add(allDeviceStaticPaths(&cluster.Spec.Ceph.Topology.OSDDrivegroups[i].OSD))
				break
			}
		}
	}
	return out
}

func allDeviceStaticPaths(osd *v1alpha1.StorageCephHostOSD) []string {
	if osd == nil {
		return nil
	}
	var paths []string
	for _, sel := range []*v1alpha1.StorageCephDeviceSelection{osd.DataDevices, osd.DBDevices, osd.WALDevices} {
		if sel == nil {
			continue
		}
		paths = append(paths, sel.Paths...)
		for _, p := range sel.PathSpecs {
			if p.Path != "" {
				paths = append(paths, p.Path)
			}
		}
	}
	return paths
}
