package openshift

import (
	"cmp"
	"net/netip"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/network"
)

func AddonFacts(catalog api.Catalog, name string) addons.ClusterFacts {
	facts := addons.ClusterFacts{Name: name}
	cluster, found := catalog.Find(api.OpenShiftCluster, name)
	if !found {
		return facts
	}
	spec := cluster.Spec()
	facts.Trust = addons.TrustFacts{Policy: spec.Get("install", "additionalTrustBundlePolicy").Text(), Bundles: spec.Get("install", "additionalTrustBundleRefs").Len()}
	facts.Release = spec.Get("distribution", "release", "version").Text()
	networks, _ := machineNetworks(cluster, catalog)
	facts.MachineNetworks = uniquePrefixes(networks)
	if len(networks) > 0 {
		facts.Family = "ipv6"
		if networks[0].Addr().Is4() {
			facts.Family = "ipv4"
		}
	}
	for _, network := range spec.Get("networking", "clusterNetwork").Items() {
		if p, e := netip.ParsePrefix(network.Get("cidr").Text()); e == nil {
			facts.ClusterNetworks = append(facts.ClusterNetworks, p.Masked())
		}
	}
	for _, raw := range spec.Get("networking", "serviceNetwork").Strings() {
		if p, e := netip.ParsePrefix(raw); e == nil {
			facts.ServiceNetworks = append(facts.ServiceNetworks, p.Masked())
		}
	}
	facts.ClusterNetworks = uniquePrefixes(facts.ClusterNetworks)
	facts.ServiceNetworks = uniquePrefixes(facts.ServiceNetworks)
	facts.Networks = clusterNetworkFacts(cluster, catalog, facts.Family)
	for _, node := range spec.Get("nodes").Items() {
		name := node.Get("machineRef").Text()
		facts.Nodes = append(facts.Nodes, name)
		bound, found := catalog.Find(api.Machine, name)
		if found {
			facts.NodeNetworks = append(facts.NodeNetworks, nodeAddonNetwork(bound, catalog, facts.Family))
		}
	}
	for _, slot := range endpointSlots {
		if a, e := netip.ParseAddr(spec.Get("install", "endpoints", slot, "address").Text()); e == nil {
			facts.Endpoints = append(facts.Endpoints, addons.AddressHolder{Addr: a.Unmap(), Holder: "the " + slot + " endpoint"})
		}
	}
	configs := []string{}
	for _, node := range facts.Nodes {
		if bound, found := catalog.Find(api.Machine, node); found {
			configs = append(configs, bound.Spec().Get("network", "configRef").Text())
		}
	}
	for _, claim := range addressClaims(catalog) {
		if claim.Object.Kind() == api.OpenShiftCluster && claim.Object.Name() == name || claim.Object.Kind() == api.Machine && slices.Contains(facts.Nodes, claim.Object.Name()) || claim.Object.Kind() == api.NetworkConfig && slices.Contains(configs, claim.Object.Name()) {
			continue
		}
		facts.Reserved = append(facts.Reserved, addons.AddressHolder{Addr: claim.Address, Holder: claim.Object.Identity()})
	}
	slices.SortFunc(facts.Reserved, func(a, b addons.AddressHolder) int {
		if n := a.Addr.Compare(b.Addr); n != 0 {
			return n
		}
		return cmp.Compare(a.Holder, b.Holder)
	})
	facts.Reserved = slices.Compact(facts.Reserved)
	gateway := spec.Get("networking", "ovnKubernetesConfig", "gatewayConfig")
	facts.Gateway = addons.OVNGateway{RoutingViaHost: gateway.Get("routingViaHost").Bool(), IPForwarding: gateway.Get("ipForwarding").Text()}
	for _, network := range spec.Get("networking", "secondaryNetworks").Items() {
		entry := addons.SecondaryNetwork{Name: network.Get("name").Text(), Interface: network.Get("bridgeMapping", "interface").Text(), Bridge: network.Get("bridgeMapping", "bridge").Text(), HostAddresses: network.Get("bridgeMapping", "hostAddresses").Text() == "bridge"}
		for _, node := range spec.Get("nodes").Items() {
			if iface := BridgeInterface(node, network); iface != entry.Interface {
				if entry.NodeInterfaces == nil {
					entry.NodeInterfaces = map[string]string{}
				}
				entry.NodeInterfaces[node.Get("machineRef").Text()] = iface
			}
		}
		if network.Get("ipam", "mode").Text() == "Enabled" {
			for _, raw := range network.Get("ipam", "subnets").Strings() {
				if p, e := netip.ParsePrefix(raw); e == nil {
					entry.Subnets = append(entry.Subnets, p.Masked())
				}
			}
			for _, raw := range network.Get("ipam", "excludeSubnets").Strings() {
				if p, e := netip.ParsePrefix(raw); e == nil {
					entry.Excluded = append(entry.Excluded, p.Masked())
				}
			}
		}
		entry.Subnets = uniquePrefixes(entry.Subnets)
		facts.SecondaryNetworks = append(facts.SecondaryNetworks, entry)
	}
	slices.SortFunc(facts.SecondaryNetworks, func(a, b addons.SecondaryNetwork) int { return cmp.Compare(a.Name, b.Name) })
	for _, network := range facts.SecondaryNetworks {
		if network.HostAddresses {
			for i := range facts.NodeNetworks {
				facts.NodeNetworks[i] = moveToBridge(facts.NodeNetworks[i], network)
			}
		}
	}

	return facts
}

func moveToBridge(node addons.NodeNetwork, network addons.SecondaryNetwork) addons.NodeNetwork {
	if slices.ContainsFunc(node.Interfaces, func(nic addons.NodeInterface) bool { return nic.Name == network.Bridge }) {
		return node
	}
	node.Interfaces = slices.Clone(node.Interfaces)
	for i, nic := range node.Interfaces {
		if nic.Name == network.InterfaceOn(node.Machine) {
			node.Interfaces[i].Addresses = nil
			node.Interfaces = append(node.Interfaces, addons.NodeInterface{Name: network.Bridge, Type: "ovs-interface", Addresses: nic.Addresses, Networks: nic.Networks})
			break
		}
	}
	return node
}

func clusterNetworkFacts(cluster api.Object, catalog api.Catalog, family string) []addons.NetworkFact {
	sites := []string{}
	for _, node := range cluster.Spec().Get("nodes").Items() {
		if bound, found := catalog.Find(api.Machine, node.Get("machineRef").Text()); found {
			sites = append(sites, bound.Spec().Get("placement", "site").Text())
		}
	}
	facts := []addons.NetworkFact{}
	for _, held := range network.CIDRs(catalog) {
		if network.Family(held.Prefix.Addr()) == family && slices.ContainsFunc(sites, func(site string) bool { return network.Serves(held.Network, site) }) {
			facts = append(facts, addons.NetworkFact{Name: held.Network.Name(), Prefix: held.Prefix})
		}
	}
	return facts
}

func uniquePrefixes(prefixes []netip.Prefix) []netip.Prefix {
	slices.SortFunc(prefixes, func(a, b netip.Prefix) int {
		if n := a.Addr().Compare(b.Addr()); n != 0 {
			return n
		}
		return cmp.Compare(a.Bits(), b.Bits())
	})
	return slices.Compact(prefixes)
}

func nodeAddonNetwork(node api.Object, catalog api.Catalog, family string) addons.NodeNetwork {
	result := addons.NodeNetwork{Machine: node.Name()}
	native, issues := machine.ComposeNetwork(node, catalog)
	if len(issues) > 0 {
		return result
	}
	for _, entry := range native.Get("interfaces").Items() {
		if state := entry.Get("state").Text(); state == "absent" || state == "ignore" {
			continue
		}
		iface := addons.NodeInterface{Name: entry.Get("name").Text(), Type: entry.Get("type").Text()}
		for _, assignment := range node.Spec().Get("network", "addresses").Items() {
			if assignment.Get("interface").Text() == iface.Name {
				if p, ok := machine.AssignedPrefix(node, catalog, assignment); ok {
					iface.Addresses = append(iface.Addresses, p)
				}
				if held, ok := machine.AssignedNetwork(node, catalog, assignment); ok {
					iface.Networks = append(iface.Networks, held.Network.Name())
				}
			}
		}
		for _, reach := range machine.Reached(node, catalog, native, iface.Name, 0) {
			if reach.Found() {
				iface.Networks = append(iface.Networks, reach.Network.Name())
			}
		}
		slices.Sort(iface.Networks)
		iface.Networks = slices.Compact(iface.Networks)
		result.Interfaces = append(result.Interfaces, iface)
	}
	for _, route := range native.Get("routes", "config").Items() {
		if route.Get("state").Text() == "absent" {
			continue
		}
		if p, e := netip.ParsePrefix(route.Get("destination").Text()); e == nil && p.Bits() == 0 && p.Addr().Is4() == (family == "ipv4") {
			result.DefaultRouteInterface = route.Get("next-hop-interface").Text()
		}
		if a, e := netip.ParseAddr(route.Get("next-hop-address").Text()); e == nil {
			result.NextHops = append(result.NextHops, a.Unmap())
		}
	}
	slices.SortFunc(result.NextHops, func(a, b netip.Addr) int { return a.Compare(b) })
	result.NextHops = slices.Compact(result.NextHops)
	return result
}
