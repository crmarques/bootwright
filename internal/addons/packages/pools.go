package packages

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
)

type interval struct {
	first, last netip.Addr
	pool        string
}

func (r interval) contains(a netip.Addr) bool {
	return a.IsValid() && a.BitLen() == r.first.BitLen() && a.Compare(r.first) >= 0 && a.Compare(r.last) <= 0
}
func lastAddress(p netip.Prefix) netip.Addr {
	a := p.Masked().Addr()
	b := a.As16()
	bits := p.Bits()
	if a.Is4() {
		bits += 96
	}
	for i := bits; i < 128; i++ {
		b[i/8] |= 1 << uint(7-i%8)
	}
	result := netip.AddrFrom16(b)
	if a.Is4() {
		result = result.Unmap()
	}
	return result
}

var errMixedRange = errors.New("reversed or mixed")

func parseInterval(entry string) (interval, string, error) {
	if strings.Contains(entry, "/") {
		p, err := netip.ParsePrefix(entry)
		if err != nil || p.Addr().Is4In6() {
			return interval{}, "", fmt.Errorf("invalid CIDR")
		}
		canonical := p.Masked().String()
		if canonical != entry {
			return interval{}, canonical, fmt.Errorf("noncanonical")
		}
		return interval{first: p.Addr(), last: lastAddress(p)}, "", nil
	}
	parts := strings.Split(entry, "-")
	if len(parts) != 2 {
		return interval{}, "", fmt.Errorf("invalid range")
	}
	a, ea := netip.ParseAddr(parts[0])
	b, eb := netip.ParseAddr(parts[1])
	if ea != nil || eb != nil || a.Zone() != "" || b.Zone() != "" || a.Is4In6() || b.Is4In6() {
		return interval{}, "", fmt.Errorf("invalid range")
	}
	if a.BitLen() != b.BitLen() || a.Compare(b) > 0 {
		return interval{}, "", errMixedRange
	}
	canonical := a.String() + "-" + b.String()
	if canonical != entry {
		return interval{}, canonical, fmt.Errorf("noncanonical")
	}
	return interval{first: a, last: b}, "", nil
}
func overlaps(a, b interval) bool {
	return a.first.BitLen() == b.first.BitLen() && a.first.Compare(b.last) <= 0 && b.first.Compare(a.last) <= 0
}

type poolIssue func(code, field, message, remedy string)

func admitPools(i addons.Instance, f addons.ClusterFacts) []api.Issue {
	config, err := addons.Decode[api.MetalLBConfig](i.Config)
	if err != nil {
		return []api.Issue{{Code: "api.value", Field: "", Message: "invalid MetalLB configuration", Remediation: "use the typed MetalLB configuration fields"}}
	}
	issues := []api.Issue{}
	add := func(code, field, message, remedy string) {
		issue := api.Issue{Code: "api.invariant", Field: field, Message: message, Remediation: remedy}
		switch code {
		case "api.value":
			issue.Code = "api.value"
		case "api.duplicate":
			issue.Code = "api.duplicate"
		}
		issues = append(issues, issue)
	}
	if len(config.Pools) > 16 {
		add("api.value", ".pools", "MetalLB selects more than 16 pools", "select at most 16 pools")
		return issues
	}
	previous := []interval{}
	for pi, pool := range config.Pools {
		base := fmt.Sprintf(".pools[%d]", pi)
		field := base + ".l2.interface"
		networks := f.MachineNetworks
		if len(pool.Addresses) < 1 || len(pool.Addresses) > 16 {
			add("api.value", base+".addresses", "pool "+pool.Name+" must have 1 to 16 address entries", "declare between 1 and 16 address entries")
			continue
		}
		if pool.L2 != nil && pool.L2.Interface != "" {
			networks = interfaceNetworks(pool, f, field, add)
		}
		previous = admitPoolEntries(pool, f, base, networks, previous, add)
	}
	return issues
}

func interfaceNetworks(pool api.MetalLBPool, f addons.ClusterFacts, field string, add poolIssue) []netip.Prefix {
	var networks []netip.Prefix
	iface := pool.L2.Interface
	networks = nil
	var shared netip.Prefix
	sharedNode := ""
	for _, machine := range f.Nodes {
		found := false
		for _, node := range f.NodeNetworks {
			if node.Machine == machine {
				found = true
				break
			}
		}
		if !found {
			add("api.invariant", field, fmt.Sprintf("interface %s of pool %s is not declared on node %s of OpenShiftCluster/%s", iface, pool.Name, machine, f.Name), fmt.Sprintf("declare %s on every node of %s, or omit l2.interface", iface, f.Name))
		}
	}
	for _, node := range f.NodeNetworks {
		if iface == "br-ex" || iface == node.DefaultRouteInterface {
			add("api.invariant", field, fmt.Sprintf("interface %s of pool %s is the default-route interface of node %s or br-ex", iface, pool.Name, node.Machine), "omit l2.interface to announce a pool on the machine network")
		}
		found := false
		addressed := false
		for _, nic := range node.Interfaces {
			if nic.Name != iface {
				continue
			}
			found = true
			for _, address := range nic.Addresses {
				if address.Addr().Is4() != (f.Family == "ipv4") {
					continue
				}
				addressed = true
				subnet := address.Masked()
				if !shared.IsValid() {
					shared = subnet
					sharedNode = node.Machine
				} else if shared != subnet {
					add("api.invariant", field, fmt.Sprintf("interface %s of pool %s is on %s on node %s and on %s on node %s", iface, pool.Name, shared, sharedNode, subnet, node.Machine), fmt.Sprintf("assign every node's %s address in one subnet", iface))
				}
			}
		}
		if !found {
			add("api.invariant", field, fmt.Sprintf("interface %s of pool %s is not declared on node %s of OpenShiftCluster/%s", iface, pool.Name, node.Machine, f.Name), fmt.Sprintf("declare %s on every node of %s, or omit l2.interface", iface, f.Name))
		} else if !addressed {
			declared := iface
			for _, network := range f.SecondaryNetworks {
				if network.HostAddresses && network.UplinkBridge == iface {
					declared = network.UplinkInterface
				}
			}
			add("api.invariant", field, fmt.Sprintf("interface %s of pool %s carries no %s address on node %s", iface, pool.Name, f.Family, node.Machine), fmt.Sprintf("assign %s an address in the pool's subnet on every node through network.addresses", declared))
		}
	}
	if len(f.NodeNetworks) == 0 {
		add("api.invariant", field, "interface "+iface+" has no admitted node network facts", "declare the interface on every node before selecting it")
	}
	if shared.IsValid() {
		networks = []netip.Prefix{shared}
	}
	for _, network := range f.SecondaryNetworks {
		switch {
		case network.HostAddresses && iface == network.UplinkInterface:
			add("api.invariant", field, fmt.Sprintf("interface %s of pool %s is the uplink of secondary network %s, whose bridge %s carries its node addresses", iface, pool.Name, network.Name, network.UplinkBridge), "set l2.interface to "+network.UplinkBridge)
		case !network.HostAddresses && (iface == network.UplinkInterface || iface == network.UplinkBridge):
			add("api.invariant", field, fmt.Sprintf("interface %s of pool %s is the uplink of secondary network %s, which carries no host address", iface, pool.Name, network.Name), "name an interface that carries a node address, or omit l2.interface")
		}
	}
	if !f.Gateway.RoutingViaHost {
		add("api.invariant", field, fmt.Sprintf("pool %s announces on %s, which needs gatewayConfig.routingViaHost: true on OpenShiftCluster/%s", pool.Name, iface, f.Name), fmt.Sprintf("set spec.networking.ovnKubernetesConfig.gatewayConfig.routingViaHost: true on %s, or omit l2.interface", f.Name))
	}
	if f.Gateway.IPForwarding != "Global" {
		add("api.invariant", field, fmt.Sprintf("pool %s announces on %s, which needs gatewayConfig.ipForwarding: Global on OpenShiftCluster/%s", pool.Name, iface, f.Name), fmt.Sprintf("set spec.networking.ovnKubernetesConfig.gatewayConfig.ipForwarding: Global on %s, or omit l2.interface", f.Name))
	}
	return networks
}

func checkPoolConflicts(pool api.MetalLBPool, f addons.ClusterFacts, field, entry string, r interval, previous []interval, add poolIssue) {
	holders := append([]addons.AddressHolder{}, f.Endpoints...)
	holders = append(holders, f.Reserved...)
	for _, node := range f.NodeNetworks {
		for _, nic := range node.Interfaces {
			for _, a := range nic.Addresses {
				holders = append(holders, addons.AddressHolder{Addr: a.Addr(), Holder: "Machine/" + node.Machine})
			}
		}
		for _, a := range node.NextHops {
			holders = append(holders, addons.AddressHolder{Addr: a, Holder: "the next hop of Machine/" + node.Machine})
		}
	}
	seenHolders := map[netip.Addr]bool{}
	for _, h := range holders {
		if r.contains(h.Addr) && !seenHolders[h.Addr] {
			seenHolders[h.Addr] = true
			add("api.invariant", field, fmt.Sprintf("address %s of pool %s includes %s, held by %s", entry, pool.Name, h.Addr, h.Holder), "exclude "+h.Addr.String()+" from the pool")
		}
	}
	forbidden := []struct {
		kind     string
		prefixes []netip.Prefix
	}{{"cluster network", f.ClusterNetworks}, {"service network", f.ServiceNetworks}}
	for _, n := range f.SecondaryNetworks {
		forbidden = append(forbidden, struct {
			kind     string
			prefixes []netip.Prefix
		}{"secondary network " + n.Name, n.Subnets})
	}
	for _, group := range forbidden {
		for _, n := range group.prefixes {
			if overlaps(r, interval{first: n.Masked().Addr(), last: lastAddress(n)}) {
				add("api.invariant", field, fmt.Sprintf("address %s of pool %s overlaps %s, the %s of OpenShiftCluster/%s", entry, pool.Name, n, group.kind, f.Name), "use addresses outside "+n.String())
			}
		}
	}
	for _, other := range previous {
		if overlaps(r, other) {
			address := r.first
			if other.first.Compare(address) > 0 {
				address = other.first
			}
			add("api.duplicate", field, fmt.Sprintf("pool %s overlaps pool %s at %s", pool.Name, other.pool, address), fmt.Sprintf("make the addresses of %s and %s disjoint", pool.Name, other.pool))
		}
	}
}

func admitPoolEntries(pool api.MetalLBPool, f addons.ClusterFacts, base string, networks []netip.Prefix, previous []interval, add poolIssue) []interval {
	var poolNetwork netip.Prefix
	for ai, entry := range pool.Addresses {
		field := fmt.Sprintf("%s.addresses[%d]", base, ai)
		r, canonical, err := parseInterval(entry)
		if err != nil {
			if errors.Is(err, errMixedRange) {
				add("api.value", field, fmt.Sprintf("range %s of pool %s ends before it starts or mixes address families", entry, pool.Name), "write the range as <first>-<last> of one family, first not after last")
			} else {
				remedy := "write it as a canonical CIDR or <first>-<last> range"
				if canonical != "" {
					remedy = "write it as " + canonical
				}
				add("api.value", field, fmt.Sprintf("address %s of pool %s is not a canonical CIDR or <first>-<last> range", entry, pool.Name), remedy)
			}
			continue
		}
		r.pool = pool.Name
		if f.Family != "ipv4" && f.Family != "ipv6" {
			add("api.invariant", field, "OpenShiftCluster/"+f.Name+" has no admitted address family", "declare the cluster networks before selecting pools")
			continue
		}
		if r.first.Is4() != (f.Family == "ipv4") {
			add("api.invariant", field, fmt.Sprintf("address %s of pool %s is not of the %s family of OpenShiftCluster/%s", entry, pool.Name, f.Family, f.Name), fmt.Sprintf("use %s addresses for the pools of %s", f.Family, f.Name))
			continue
		}
		var network netip.Prefix
		for _, n := range networks {
			if n.Contains(r.first) && n.Contains(r.last) {
				network = n.Masked()
				break
			}
		}
		if !network.IsValid() {
			if pool.L2 != nil && pool.L2.Interface != "" {
				if len(networks) > 0 {
					add("api.invariant", field, fmt.Sprintf("address %s of pool %s lies outside %s, the subnet of %s", entry, pool.Name, networks[0], pool.L2.Interface), "use addresses inside "+networks[0].String())
				}
			} else {
				cidrs := []string{}
				for _, n := range networks {
					cidrs = append(cidrs, n.String())
				}
				add("api.invariant", field, fmt.Sprintf("address %s of pool %s lies outside every machine network of OpenShiftCluster/%s", entry, pool.Name, f.Name), "use addresses inside "+strings.Join(cidrs, ", ")+", or name the interface whose subnet holds them under l2.interface")
			}
		} else {
			if poolNetwork.IsValid() && poolNetwork != network {
				add("api.invariant", base+".addresses", fmt.Sprintf("pool %s spans %s and %s", pool.Name, poolNetwork, network), "split the pool so that each pool lies inside one network")
			}
			poolNetwork = network
			if network.Addr().Is4() {
				for _, a := range []netip.Addr{network.Addr(), lastAddress(network)} {
					if r.contains(a) {
						add("api.invariant", field, fmt.Sprintf("address %s of pool %s includes %s, the network or broadcast address of %s", entry, pool.Name, a, network), "exclude "+a.String()+" from the pool")
					}
				}
			}
		}
		checkPoolConflicts(pool, f, field, entry, r, previous, add)
		previous = append(previous, r)
	}
	return previous
}
