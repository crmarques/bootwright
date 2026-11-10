package cephcluster

import (
	"net/netip"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/network"
)

var cidrRoles = []struct{ key, role string }{{"publicCIDRs", "public"}, {"clusterCIDRs", "replication"}}

func (a *admission) networkCoverage(ceph api.Value) {
	a.declaredNetworks(ceph)
	nodes := ceph.Get("topology", "nodes").Items()
	declared := []netip.Addr{}
	for _, node := range nodes {
		m, ok := a.catalog.Find(api.Machine, textOf(node, "machineRef"))
		if !ok {
			return
		}
		for _, text := range machine.IPAddresses(m) {
			if address, err := netip.ParseAddr(text); err == nil {
				declared = append(declared, address)
			}
		}
	}
	if len(nodes) == 0 {
		return
	}
	for _, entry := range cidrRoles {
		for i, cidr := range ceph.Get("networks", entry.key).Strings() {
			prefix, err := netip.ParsePrefix(cidr)
			if err != nil || slices.ContainsFunc(declared, prefix.Masked().Contains) {
				continue
			}
			a.invalid(indexed("$.spec.networks."+entry.key, i), "the "+entry.role+" CIDR "+cidr+" contains no address a topology Machine declares, so no Ceph host is on that network",
				"remove "+cidr+" from spec.networks."+entry.key+", or declare an address inside it in spec.network.addresses of a Machine the topology names")
		}
	}
	a.replicationCoverage(ceph, nodes)
}

func (a *admission) replicationCoverage(ceph api.Value, nodes []api.Value) {
	cidrs := ceph.Get("networks", "clusterCIDRs").Strings()
	if len(cidrs) == 0 {
		return
	}
	listed := strings.Join(cidrs, ", ")
	osdHosts := map[string]bool{}
	for _, service := range ceph.Get("services").Items() {
		if textOf(service, "service_type") != "osd" {
			continue
		}
		names, _ := ResolveServicePlacement(a.object, service)
		for _, name := range names {
			osdHosts[name] = true
		}
	}
	tiebreaker := textOf(ceph, "topology", "stretch", "tiebreaker", "node")
	for i, node := range nodes {
		name := textOf(node, "name")
		if !osdHosts[name] || (tiebreaker != "" && name == tiebreaker) {
			continue
		}
		m, found := a.catalog.Find(api.Machine, textOf(node, "machineRef"))
		if !found || insideAnyNetwork(machine.IPAddresses(m), cidrs) {
			continue
		}
		a.invalid(indexed("$.spec.topology.nodes", i)+".machineRef", "Ceph host "+NativeHostname(node)+" (Machine "+m.Name()+") has no address inside the replication networks "+listed,
			"add an address inside one of "+listed+" to the network of "+m.Name()+", or remove spec.networks.clusterCIDRs")
	}
}

func insideAnyNetwork(addresses, cidrs []string) bool {
	for _, value := range addresses {
		address, err := netip.ParseAddr(value)
		if err != nil {
			continue
		}
		for _, cidr := range cidrs {
			if prefix, err := netip.ParsePrefix(cidr); err == nil && prefix.Masked().Contains(address) {
				return true
			}
		}
	}
	return false
}

func (a *admission) ingressAdjacency(service api.Value, field string, placed []api.Value) {
	vip, ok := ingressAddress(service)
	if !ok || !slices.ContainsFunc(declaredPublicNetworks(a.object, a.catalog), func(network netip.Prefix) bool { return network.Contains(vip) }) {
		return
	}
	interfaceNetworks := []netip.Prefix{}
	for _, entry := range service.Get("spec").Get("virtual_interface_networks").Strings() {
		if prefix, err := netip.ParsePrefix(entry); err == nil {
			interfaceNetworks = append(interfaceNetworks, prefix.Masked())
		}
	}
	for _, node := range placed {
		m, found := a.catalog.Find(api.Machine, textOf(node, "machineRef"))
		if !found {
			continue
		}
		prefixes := machinePrefixes(m, a.catalog)
		if len(prefixes) == 0 || slices.ContainsFunc(prefixes, adjacentTo(vip, interfaceNetworks)) {
			continue
		}
		if len(interfaceNetworks) > 0 {
			a.invalid(field+".spec.virtual_interface_networks", "ingress host "+NativeHostname(node)+" (Machine "+m.Name()+") has no address inside the virtual_interface_networks, so keepalived there has no interface for the virtual IP",
				"declare an address inside one of the virtual_interface_networks on Machine "+m.Name()+", or place the ingress on hosts that have one")
			continue
		}
		a.invalid(field+".spec.virtual_ip", "ingress host "+NativeHostname(node)+" (Machine "+m.Name()+") has no address whose prefix contains the virtual IP, so keepalived cannot move the virtual IP there on one layer-2 network",
			"declare an address in the virtual IP's subnet on Machine "+m.Name()+", or place the ingress on hosts addressed in that subnet")
	}
}

func machinePrefixes(m api.Object, c api.Catalog) []netip.Prefix {
	out := []netip.Prefix{}
	for _, entry := range m.Spec().Get("network", "addresses").Items() {
		if prefix, ok := machine.AssignedPrefix(m, c, entry); ok {
			out = append(out, prefix)
		}
	}
	return out
}

func adjacentTo(vip netip.Addr, interfaceNetworks []netip.Prefix) func(netip.Prefix) bool {
	return func(prefix netip.Prefix) bool {
		if len(interfaceNetworks) > 0 {
			return slices.ContainsFunc(interfaceNetworks, func(network netip.Prefix) bool { return network.Contains(prefix.Addr()) })
		}
		return prefix.Masked().Contains(vip)
	}
}

func (a *admission) declaredNetworks(ceph api.Value) {
	declared := network.CIDRs(a.catalog)
	for _, entry := range cidrRoles {
		for i, cidr := range ceph.Get("networks", entry.key).Strings() {
			prefix, err := netip.ParsePrefix(cidr)
			if err != nil || slices.ContainsFunc(declared, func(held network.Held) bool { return held.Prefix == prefix }) {
				continue
			}
			a.invalid(indexed("$.spec.networks."+entry.key, i), "the "+entry.role+" CIDR "+cidr+" of "+a.object.Identity()+" is the cidr of no declared Network, so Bootwright cannot check which network carries it",
				"set "+cidr+" in spec.networks."+entry.key+" of "+a.object.Identity()+" to the spec.ipv4.cidr or spec.ipv6.cidr of the Network it names, or declare that Network")
		}
	}
}
