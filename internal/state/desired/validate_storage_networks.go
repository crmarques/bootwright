package desiredstate

import (
	"fmt"
	"net"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

func validateStorageCephBootstrapPublicNetwork(prefix string, cluster v1alpha1.StorageCluster, machines map[string]v1alpha1.Machine) []string {
	ceph := cluster.Spec.Ceph
	if ceph == nil || len(ceph.Networks.PublicCIDRs) == 0 {
		return nil
	}
	host := ceph.Cephadm.Bootstrap.Node
	node, ok := storageCephNodeByName(cluster, host)
	if host == "" || !ok {
		return nil
	}
	machine, ok := machines[node.MachineRef.Name]
	if !ok {
		return nil
	}
	addrName := ceph.Cephadm.Bootstrap.AddressRef.Name
	if addrName == "" && machine.Spec.Access.SSH != nil {
		addrName = machine.Spec.Access.SSH.AddressRef.Name
	}
	monIP, ok := v1alpha1.MachineAddressByName(machine, addrName)
	if addrName == "" || !ok || monIP == "" {
		return nil
	}
	publics := ceph.Networks.PublicCIDRs
	var subnets []string
	for _, ia := range machine.Spec.Network.Config.InterfaceAddresses {
		if ia.AddressRef.Name != addrName || ia.PrefixLength <= 0 {
			continue
		}
		if subnet, ok := storageConnectedSubnet(monIP, ia.PrefixLength); ok {
			subnets = append(subnets, subnet)
		}
	}
	if len(subnets) == 0 {
		if storageIPWithinAny(monIP, publics) {
			return nil
		}
		return []string{fmt.Sprintf("%s.networks.publicCIDRs: cephadm bootstrap host %q mon-ip %s falls outside every entry (%s); the bootstrap monitor cannot bind inside the declared public network", prefix, host, monIP, strings.Join(publics, ","))}
	}
	declared := storageCanonicalNetworks(publics)
	for _, subnet := range subnets {
		if declared[subnet] {
			return nil
		}
	}
	return []string{fmt.Sprintf("%s.networks.publicCIDRs: cephadm bootstrap host %q carries mon-ip %s on interface subnet %s, absent from the declared entries (%s); cephadm bootstrap aborts with \"public CIDR network ... is not configured locally\" unless a publicCIDRs entry exactly equals a locally-configured interface subnet — set the Machine/%s interfaceAddresses prefixLength and the publicCIDRs entry to the same CIDR", prefix, host, monIP, strings.Join(subnets, ","), strings.Join(publics, ","), machine.Metadata.Name)}
}

func validateStorageCephMonPublicNetwork(prefix string, cluster v1alpha1.StorageCluster, machines map[string]v1alpha1.Machine) []string {
	ceph := cluster.Spec.Ceph
	if ceph == nil || len(ceph.Networks.PublicCIDRs) == 0 {
		return nil
	}
	publics := ceph.Networks.PublicCIDRs
	var errs []string
	for _, node := range ceph.Topology.Nodes {
		if !topology.NodeHasRole(node, v1alpha1.StorageCephRoleMON) {
			continue
		}
		machine, ok := machines[node.MachineRef.Name]
		if !ok {
			continue
		}
		addrName := ceph.Cephadm.AddressRef.Name
		if addrName == "" && machine.Spec.Access.SSH != nil {
			addrName = machine.Spec.Access.SSH.AddressRef.Name
		}
		address, _ := v1alpha1.MachineAddressByName(machine, addrName)
		subnets := storageMachineConnectedSubnets(machine)
		if len(subnets) > 0 {
			if storageNetworksOverlapAny(subnets, publics) {
				continue
			}
			errs = append(errs, fmt.Sprintf("%s.networks.publicCIDRs: Ceph mon host %q (Machine/%s) is connected to %s, which overlaps no declared entry (%s); cephadm places a mon only on a host holding an address inside public_network and skips the rest with `Filtered out host <host>: does not belong to mon public_network(s)`, so the mon is never deployed and never joins the monmap, and the topology operations that address mons by name (`ceph mon set_location`, `ceph mon enable_stretch_mode`) then fail with `ENOENT: mon.<name> does not exist` — public_network takes a comma-separated list, so add this host's own subnet to publicCIDRs (this is how a stretch arbiter standing on its own network is admitted), or move the host onto a declared public network", prefix, node.Name, machine.Metadata.Name, strings.Join(subnets, ","), strings.Join(publics, ",")))
			continue
		}
		if address == "" || storageIPWithinAny(address, publics) {
			continue
		}
		errs = append(errs, fmt.Sprintf("%s.networks.publicCIDRs: Ceph mon host %q (Machine/%s) answers at %s, outside every declared entry (%s); cephadm places a mon only on a host holding an address inside public_network and skips the rest with `Filtered out host <host>: does not belong to mon public_network(s)`, so the mon is never deployed and never joins the monmap, and the topology operations that address mons by name (`ceph mon set_location`, `ceph mon enable_stretch_mode`) then fail with `ENOENT: mon.<name> does not exist` — public_network takes a comma-separated list, so add this host's own subnet to publicCIDRs (this is how a stretch arbiter standing on its own network is admitted), or move the host onto a declared public network", prefix, node.Name, machine.Metadata.Name, address, strings.Join(publics, ",")))
	}
	return errs
}

func validateStorageCephUnusedPublicNetwork(prefix string, cluster v1alpha1.StorageCluster, machines map[string]v1alpha1.Machine, state v1alpha1.State) []string {
	ceph := cluster.Spec.Ceph
	if ceph == nil || len(ceph.Networks.PublicCIDRs) < 2 {
		return nil
	}
	declared := make([]storagePublicFootprint, 0, len(ceph.Topology.Nodes))
	for _, node := range ceph.Topology.Nodes {
		machine, ok := machines[node.MachineRef.Name]
		if !ok {
			return nil
		}
		footprint, ok := storageMachinePublicFootprint(ceph, machine)
		if !ok {
			return nil
		}
		declared = append(declared, footprint)
	}
	if len(declared) == 0 {
		return nil
	}
	var errs []string
	for _, cidr := range ceph.Networks.PublicCIDRs {
		if storageFootprintsStandOn(declared, cidr) {
			continue
		}
		if storageArbiterCandidateStandsOn(state, cluster, machines, declared, cidr) {
			continue
		}
		errs = append(errs, fmt.Sprintf("%s.networks.publicCIDRs: no declared Ceph host stands on %s; public_network is the network Ceph daemons bind within and clients reach them on, so an entry no host of this cluster sits in binds nothing and reaches nobody — it is a declaration this estate does not serve. This is what an arbiter subnet copied from another environment looks like: check it against the addresses under spec.ceph.topology.nodes and against the Machines declaring capability %q — an arbiter candidate names no cluster, so it holds an entry of this one only when it shares a network with a declared topology node or stands on a subnet no other StorageCluster declares — then drop the entry or correct the host that was meant to stand on it", prefix, cidr, v1alpha1.MachineCapabilityCephArbiter))
	}
	return errs
}

func storageFootprintsStandOn(footprints []storagePublicFootprint, cidr string) bool {
	for _, footprint := range footprints {
		if len(footprint.subnets) > 0 {
			if storageNetworksOverlapAny(footprint.subnets, []string{cidr}) {
				return true
			}
			continue
		}
		if footprint.address != "" && storageIPWithinAny(footprint.address, []string{cidr}) {
			return true
		}
	}
	return false
}

func storageArbiterCandidateStandsOn(state v1alpha1.State, cluster v1alpha1.StorageCluster, machines map[string]v1alpha1.Machine, declared []storagePublicFootprint, cidr string) bool {
	ceph := cluster.Spec.Ceph
	exclusive := !storageCIDRDeclaredElsewhere(state, cluster.Metadata.Name, cidr)
	for _, machine := range machines {
		if !v1alpha1.MachineHasCapability(machine, v1alpha1.MachineCapabilityCephArbiter) {
			continue
		}
		footprint, ok := storageMachinePublicFootprint(ceph, machine)
		if !ok {
			continue
		}
		if !storageFootprintsStandOn([]storagePublicFootprint{footprint}, cidr) {
			continue
		}
		if exclusive || storageFootprintNeighborsAny(footprint, declared) {
			return true
		}
	}
	return false
}

func storageCIDRDeclaredElsewhere(state v1alpha1.State, clusterName string, cidr string) bool {
	for _, other := range state.StorageClusters {
		if other.Metadata.Name == clusterName || other.Spec.Ceph == nil {
			continue
		}
		if storageNetworksOverlapAny([]string{cidr}, other.Spec.Ceph.Networks.PublicCIDRs) {
			return true
		}
	}
	return false
}

func storageFootprintNeighborsAny(footprint storagePublicFootprint, others []storagePublicFootprint) bool {
	for _, other := range others {
		if len(footprint.subnets) > 0 && len(other.subnets) > 0 && storageNetworksOverlapAny(footprint.subnets, other.subnets) {
			return true
		}
		if len(footprint.subnets) > 0 && other.address != "" && storageIPWithinAny(other.address, footprint.subnets) {
			return true
		}
		if footprint.address != "" && len(other.subnets) > 0 && storageIPWithinAny(footprint.address, other.subnets) {
			return true
		}
	}
	return false
}

type storagePublicFootprint struct {
	subnets []string
	address string
}

func storageMachinePublicFootprint(ceph *v1alpha1.StorageClusterCephSpec, machine v1alpha1.Machine) (storagePublicFootprint, bool) {
	addrName := ceph.Cephadm.AddressRef.Name
	if addrName == "" && machine.Spec.Access.SSH != nil {
		addrName = machine.Spec.Access.SSH.AddressRef.Name
	}
	address, _ := v1alpha1.MachineAddressByName(machine, addrName)
	subnets := storageMachineConnectedSubnets(machine)
	if len(subnets) == 0 && address == "" {
		return storagePublicFootprint{}, false
	}
	return storagePublicFootprint{subnets: subnets, address: address}, true
}

func storageMachineConnectedSubnets(machine v1alpha1.Machine) []string {
	var subnets []string
	for _, ia := range machine.Spec.Network.Config.InterfaceAddresses {
		if ia.PrefixLength <= 0 {
			continue
		}
		ip, ok := v1alpha1.MachineAddressByName(machine, ia.AddressRef.Name)
		if !ok || ip == "" {
			continue
		}
		subnet, ok := storageConnectedSubnet(ip, ia.PrefixLength)
		if !ok || slices.Contains(subnets, subnet) {
			continue
		}
		subnets = append(subnets, subnet)
	}
	return subnets
}

func storageNetworksOverlapAny(subnets []string, cidrs []string) bool {
	for _, subnet := range subnets {
		_, subnetNet, err := net.ParseCIDR(subnet)
		if err != nil {
			continue
		}
		for _, cidr := range cidrs {
			_, declared, err := net.ParseCIDR(cidr)
			if err != nil {
				continue
			}
			if subnetNet.Contains(declared.IP) || declared.Contains(subnetNet.IP) {
				return true
			}
		}
	}
	return false
}

func validateStorageCephClusterNetwork(prefix string, cluster v1alpha1.StorageCluster, machines map[string]v1alpha1.Machine) []string {
	ceph := cluster.Spec.Ceph
	if ceph == nil || len(ceph.Networks.ClusterCIDRs) == 0 {
		return nil
	}
	clusters := ceph.Networks.ClusterCIDRs
	var errs []string
	for _, node := range ceph.Topology.Nodes {
		if !storageCephHostRunsOSD(node) {
			continue
		}
		machine, ok := machines[node.MachineRef.Name]
		if !ok || len(machine.Spec.Network.Config.InterfaceAddresses) == 0 {
			continue
		}
		if storageMachineHasClusterNetworkAddress(machine, clusters) {
			continue
		}
		errs = append(errs, fmt.Sprintf("%s.networks.clusterCIDRs: Ceph OSD host %q (Machine/%s) configures no interface address inside the declared cluster network (%s); ceph-osd binds its replication socket inside cluster_network and, finding no local address there, aborts with 'unable to find any IP address in network(s)', so every OSD on the host stays down and stray outside the CRUSH map; add an interfaceAddresses entry (matching prefixLength) that places the node on the cluster network, or drop clusterCIDRs to run replication over the public network", prefix, node.Name, machine.Metadata.Name, strings.Join(clusters, ",")))
	}
	return errs
}

func storageCephHostRunsOSD(node v1alpha1.StorageCephNode) bool {
	if node.OSD != nil {
		return true
	}
	for _, role := range node.Roles {
		if role == v1alpha1.StorageCephRoleOSD {
			return true
		}
	}
	return false
}

func storageMachineHasClusterNetworkAddress(machine v1alpha1.Machine, cidrs []string) bool {
	for _, ia := range machine.Spec.Network.Config.InterfaceAddresses {
		ip, ok := v1alpha1.MachineAddressByName(machine, ia.AddressRef.Name)
		if !ok || ip == "" {
			continue
		}
		if storageIPWithinAny(ip, cidrs) {
			return true
		}
	}
	return false
}

func storageConnectedSubnet(ip string, prefixLength int) (string, bool) {
	_, ipNet, err := net.ParseCIDR(fmt.Sprintf("%s/%d", ip, prefixLength))
	if err != nil {
		return "", false
	}
	return ipNet.String(), true
}

func storageCanonicalNetworks(cidrs []string) map[string]bool {
	out := make(map[string]bool, len(cidrs))
	for _, cidr := range cidrs {
		if _, ipNet, err := net.ParseCIDR(cidr); err == nil {
			out[ipNet.String()] = true
		}
	}
	return out
}

func storageIPWithinAny(ip string, cidrs []string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return true
	}
	for _, cidr := range cidrs {
		if _, ipNet, err := net.ParseCIDR(cidr); err == nil && ipNet.Contains(parsed) {
			return true
		}
	}
	return false
}
