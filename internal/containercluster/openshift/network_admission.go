package openshift

import (
	"fmt"
	"net/netip"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/network"
	"github.com/crmarques/bootwright/internal/substrate"
)

func NormalizeNetworkIntent(o api.Object) api.Object {
	if o.Kind() != api.OpenShiftCluster {
		return o
	}
	spec := o.Spec()
	networks := spec.Get("networking", "secondaryNetworks")
	if !networks.Present() {
		return o
	}
	items := networks.Items()
	for i, n := range items {
		ipam := n.Get("ipam").Default("mode", api.StringValue("Disabled"))
		for _, key := range []string{"subnets", "excludeSubnets"} {
			if ipam.Has(key) {
				ipam = ipam.With(key, maskCIDRs(ipam.Get(key)))
			}
		}
		items[i] = n.With("ipam", ipam)
	}
	slices.SortFunc(items, func(a, b api.Value) int { return strings.Compare(a.Get("name").Text(), b.Get("name").Text()) })
	return o.WithSpec(spec.WithPath(api.ListValue(items...), "networking", "secondaryNetworks"))
}

func ValidateNetworkIntent(o api.Object, c api.Catalog) []api.Issue {
	if o.Kind() != api.OpenShiftCluster {
		return nil
	}
	issues := []api.Issue{}
	mtu, hasMTU := o.Spec().Get("networking", "ovnKubernetesConfig", "mtu").Int64()
	familyBits := 0
	networks, _ := machineNetworks(o, c)
	if len(networks) > 0 {
		familyBits = networks[0].Addr().BitLen()
	}
	reported := map[string]bool{}
	first := map[string]reachedNetwork{}
	for nodeIndex, node := range o.Spec().Get("nodes").Items() {
		issues = append(issues, validateBridgeInterfaces(o, nodeIndex, node)...)
		bound, ok := c.Find(api.Machine, node.Get("machineRef").Text())
		if !ok {
			continue
		}
		native, errors := machine.ComposeNetwork(bound, c)
		if len(errors) > 0 {
			continue
		}
		for _, issue := range NodeNetworkIssues(bound, c, native) {
			issue.Field = fmt.Sprintf("$.spec.nodes[%d].machineRef", nodeIndex)
			issues = append(issues, issue)
		}
		address, _ := machine.InstallAddress(bound, c)
		install := address.Get("interface").Text()
		if install == "" {
			install = defaultRouteInterface(native, familyBits)
		}
		if hasMTU {
			limit := interfaceMTU(native, install) - 100
			if mtu > limit {
				issues = append(issues, invariantRemediation("$.spec.networking.ovnKubernetesConfig.mtu", fmt.Sprintf("cluster MTU %d exceeds the default-route interface MTU minus 100 on Machine/%s", mtu, bound.Name()), fmt.Sprintf("set the cluster MTU to at most %d, or increase the interface MTU", limit)))
			}
		}
		for i, network := range o.Spec().Get("networking", "secondaryNetworks").Items() {
			iface := BridgeInterface(node, network)
			issues = append(issues, kernelNameWarning(network, i, iface, nodeIndex, node, native, reported)...)
			for _, issue := range validateBridgeMapping(network, i, iface, bound, native, install) {
				if at := overrideIndex(node, network); at >= 0 && issue.Field == fmt.Sprintf("$.spec.networking.secondaryNetworks[%d].bridgeMapping.interface", i) {
					issue.Field = fmt.Sprintf("$.spec.nodes[%d].bridgeInterfaces[%d].interface", nodeIndex, at)
				}
				issues = append(issues, issue)
			}
			issues = append(issues, validateSecondaryCarriage(network, i, iface, carriageNode{index: nodeIndex, value: node, bound: bound}, c, native, install, reported, first)...)
		}
	}
	issues = append(issues, validateSecondaryNetworks(o)...)
	return append(issues, validatePodIPAMHosts(o, c, first)...)
}

func kernelNameWarning(n api.Value, index int, iface string, nodeIndex int, node, native api.Value, reported map[string]bool) []api.Issue {
	field := fmt.Sprintf("$.spec.networking.secondaryNetworks[%d].bridgeMapping.interface", index)
	if at := overrideIndex(node, n); at >= 0 {
		field = fmt.Sprintf("$.spec.nodes[%d].bridgeInterfaces[%d].interface", nodeIndex, at)
	}
	if n.Get("bridgeMapping", "bridge").Text() == "br-ex" || interfaceType(native, iface) != "ethernet" || reported["unverified\x00"+field] {
		return nil
	}
	reported["unverified\x00"+field] = true
	name := node.Get("name").Text()
	return []api.Issue{{Code: "api.unverified", Field: field, Message: fmt.Sprintf("bridge interface %s of secondary network %s is ethernet on node %s, which keeps its kernel name after the agent installation", iface, n.Get("name").Text(), name),
		Remediation: fmt.Sprintf("use the kernel name node %s reports, or a bond, VLAN or bridge its NetworkConfig creates", name)}}
}

type heldAddress struct {
	address netip.Addr
	holder  string
}

func validatePodIPAMHosts(o api.Object, c api.Catalog, first map[string]reachedNetwork) []api.Issue {
	issues := []api.Issue{}
	for i, n := range o.Spec().Get("networking", "secondaryNetworks").Items() {
		if n.Get("ipam", "mode").Text() != "Enabled" {
			continue
		}
		base := fmt.Sprintf("$.spec.networking.secondaryNetworks[%d]", i)
		name := n.Get("name").Text()
		excluded := []netip.Prefix{}
		for _, raw := range n.Get("ipam", "excludeSubnets").Strings() {
			if p, err := netip.ParsePrefix(raw); err == nil {
				excluded = append(excluded, p.Masked())
			}
		}
		for _, raw := range n.Get("ipam", "subnets").Strings() {
			subnet, err := netip.ParsePrefix(raw)
			if err != nil {
				continue
			}
			subnet = subnet.Masked()
			held, ok := ipamNetwork(c, first[name].network, subnet)
			if !ok {
				continue
			}
			for _, h := range networkHolders(o, c, held, network.Family(subnet.Addr())) {
				if !subnet.Contains(h.address) || slices.ContainsFunc(excluded, func(p netip.Prefix) bool { return p.Contains(h.address) }) {
					continue
				}
				issues = append(issues, invariantRemediation(base+".ipam.excludeSubnets", fmt.Sprintf("IPAM subnet %s of secondary network %s spans %s, so pods can take %s, held by %s", subnet, name, held.Identity(), h.address, h.holder),
					fmt.Sprintf("add %s, or a range holding it, to ipam.excludeSubnets of secondary network %s", netip.PrefixFrom(h.address, h.address.BitLen()), name)))
			}
		}
	}
	return issues
}

func ipamNetwork(c api.Catalog, reached api.Object, subnet netip.Prefix) (api.Object, bool) {
	family := network.Family(subnet.Addr())
	if reached.Name() != "" {
		cidr, ok := network.Prefix(reached, family)
		return reached, ok && cidr == subnet
	}
	var match []api.Object
	for _, candidate := range network.CIDRs(c) {
		if candidate.Prefix == subnet {
			match = append(match, candidate.Network)
		}
	}
	if len(match) != 1 {
		return api.Object{}, false
	}
	return match[0], true
}

func networkHolders(o api.Object, c api.Catalog, held api.Object, family string) []heldAddress {
	nodes := map[string]string{}
	for _, node := range o.Spec().Get("nodes").Items() {
		nodes[node.Get("machineRef").Text()] = node.Get("name").Text()
	}
	holders := []heldAddress{}
	add := func(raw, holder string) {
		if a, err := netip.ParseAddr(raw); err == nil && network.Family(a) == family && !slices.ContainsFunc(holders, func(h heldAddress) bool { return h.address == a }) {
			holders = append(holders, heldAddress{address: a, holder: holder})
		}
	}
	if gateway, ok := network.Gateway(held, family); ok {
		add(gateway.String(), "the gateway of "+held.Identity())
	}
	if host, ok := substrate.HostAddress(held); ok && substrate.IsManagedNetwork(held) {
		add(host.Addr().String(), "the provider host's address on managed "+held.Identity())
	}
	for _, slot := range endpointSlots {
		add(o.Spec().Get("install", "endpoints", slot, "address").Text(), "the "+slot+" endpoint of "+o.Identity())
	}
	for _, m := range c.OfKind(api.Machine) {
		holder := m.Identity()
		if node, ok := nodes[m.Name()]; ok {
			holder = "node " + node + " (" + m.Identity() + ")"
		}
		for _, entry := range m.Spec().Get("network", "addresses").Items() {
			if assigned, ok := machine.AssignedNetwork(m, c, entry); ok && assigned.Network.Name() == held.Name() {
				add(entry.Get("address").Text(), holder)
			}
		}
	}
	return holders
}

func validateSecondaryNetworks(o api.Object) []api.Issue {
	issues := []api.Issue{}
	seen := map[string]string{}
	interfaces := map[string]string{}
	bridges := map[string]string{}
	placements := map[string]hostPlacement{}
	for i, n := range o.Spec().Get("networking", "secondaryNetworks").Items() {
		base := fmt.Sprintf("$.spec.networking.secondaryNetworks[%d]", i)
		name := n.Get("name").Text()
		if name == "physnet" || name == "exgwphysnet" {
			issues = append(issues, invariantRemediation(base+".name", "secondary network "+name+" uses a reserved physical network name", "choose a name other than physnet or exgwphysnet"))
		}
		bridge := n.Get("bridgeMapping", "bridge").Text()
		iface := n.Get("bridgeMapping", "interface").Text()
		if !api.ValidLexical("ifname", bridge) || bridge != "br-ex" && reservedOVNInterface(bridge) {
			issues = append(issues, invariantRemediation(base+".bridgeMapping.bridge", "secondary network "+name+" has an invalid or reserved bridge name", "use br-ex or a non-reserved Linux interface name of at most 15 characters"))
		}
		if bridge == "br-ex" && iface != "" || bridge != "br-ex" && iface == "" {
			issues = append(issues, invariantRemediation(base+".bridgeMapping", "secondary network "+name+" must select br-ex alone or a dedicated interface and bridge", "omit interface when bridge is br-ex, otherwise supply both interface and bridge"))
		}
		if iface != "" {
			if other, ok := interfaces[iface]; ok && other != bridge {
				issues = append(issues, invariantRemediation(base+".bridgeMapping.bridge", "interface "+iface+" is assigned to multiple OVS bridges", "use one bridge name for every network sharing an interface"))
			}
			interfaces[iface] = bridge
		}
		if other, ok := bridges[bridge]; ok && other != iface {
			issues = append(issues, invariantRemediation(base+".bridgeMapping.interface", "bridge "+bridge+" selects multiple bridge interfaces", "use one interface for every network sharing a bridge"))
		}
		bridges[bridge] = iface
		placement := hostPlacement{network: name, bridge: n.Get("bridgeMapping", "hostAddresses").Text() == "bridge"}
		if placement.bridge && bridge == "br-ex" {
			issues = append(issues, invariantRemediation(base+".bridgeMapping.hostAddresses", "secondary network "+name+" asks br-ex to carry host addresses, which OVN-Kubernetes manages on the install interface", "use a dedicated bridge and interface, or omit bridgeMapping.hostAddresses"))
		}
		if other, ok := placements[bridge]; ok && other.bridge != placement.bridge {
			issues = append(issues, invariantRemediation(base+".bridgeMapping.hostAddresses", "secondary networks "+other.network+" and "+name+" share bridge "+bridge+" but differ in bridgeMapping.hostAddresses", "set the same bridgeMapping.hostAddresses on every network sharing a bridge"))
		}
		placements[bridge] = placement
		for _, label := range n.Get("namespaceSelector", "matchLabels").Fields() {
			if !api.ValidLexical("kubernetes-label-key", label.Name) || !api.ValidLexical("kubernetes-label-value", label.Value.Text()) {
				issues = append(issues, invariantRemediation(base+".namespaceSelector.matchLabels", "namespace selector has an invalid label key or value", "use valid Kubernetes label keys and values"))
			}
		}
		key := bridge + "/" + n.Get("vlan").Text()
		if other, ok := seen[key]; ok {
			issues = append(issues, invariantRemediation(base+".vlan", "secondary networks "+other+" and "+name+" share a bridge VLAN", "use distinct VLANs on a shared bridge, with at most one untagged network"))
		}
		seen[key] = name
		if n.Get("namespaceSelector", "matchLabels").Len() == 0 {
			issues = append(issues, invariantRemediation(base+".namespaceSelector.matchLabels", "secondary network "+name+" selects no namespace labels", "supply at least one namespace label"))
		}
		issues = append(issues, validateNetworkIPAM(n, base)...)
	}
	return issues
}

type hostPlacement struct {
	network string
	bridge  bool
}

func defaultRouteInterface(native api.Value, familyBits int) string {
	interfaces := defaultRouteInterfaces(native, familyBits)
	if len(interfaces) == 1 {
		return interfaces[0]
	}
	return ""
}

func defaultRouteInterfaces(native api.Value, familyBits int) []string {
	interfaces := []string{}
	for _, route := range native.Get("routes", "config").Items() {
		if route.Get("state").Text() == "absent" {
			continue
		}
		if p, err := netip.ParsePrefix(route.Get("destination").Text()); err == nil && p.Bits() == 0 && (familyBits == 0 || p.Addr().BitLen() == familyBits) {
			if name := route.Get("next-hop-interface").Text(); name != "" {
				interfaces = append(interfaces, name)
			}
		}
	}
	slices.Sort(interfaces)
	return slices.Compact(interfaces)
}

func interfaceMTU(native api.Value, name string) int64 {
	return effectiveInterfaceMTU(native, name, map[string]bool{})
}

func effectiveInterfaceMTU(native api.Value, name string, visiting map[string]bool) int64 {
	if visiting[name] {
		return 1500
	}
	visiting[name] = true
	for _, nic := range native.Get("interfaces").Items() {
		if nic.Get("name").Text() != name {
			continue
		}
		if n, ok := nic.Get("mtu").Int64(); ok {
			return n
		}
		if parent := nic.Get("vlan", "base-iface").Text(); parent != "" {
			return effectiveInterfaceMTU(native, parent, visiting)
		}
	}
	if controller := interfaceController(native, name); controller != "" {
		return effectiveInterfaceMTU(native, controller, visiting)
	}
	return 1500
}

func interfaceController(native api.Value, name string) string {
	for _, nic := range native.Get("interfaces").Items() {
		if nic.Get("name").Text() == name && nic.Get("controller").Text() != "" {
			return nic.Get("controller").Text()
		}
	}
	for _, nic := range native.Get("interfaces").Items() {
		if slices.Contains(nic.Get("link-aggregation", "port").Strings(), name) {
			return nic.Get("name").Text()
		}
	}
	return ""
}

func validateBridgeMapping(n api.Value, index int, iface string, bound api.Object, native api.Value, install string) []api.Issue {
	issues := []api.Issue{}
	base := fmt.Sprintf("$.spec.networking.secondaryNetworks[%d]", index)
	name := n.Get("name").Text()
	bridge := n.Get("bridgeMapping", "bridge").Text()
	onBridge := n.Get("bridgeMapping", "hostAddresses").Text() == "bridge"
	mtuInterface := iface
	if bridge == "br-ex" {
		mtuInterface = install
		if n.Has("vlan") && interfaceType(native, install) == "vlan" {
			issues = append(issues, invariantRemediation(base+".vlan", fmt.Sprintf("network %s tags VLAN %s on br-ex, which node %s builds on VLAN interface %s", name, n.Get("vlan").Text(), bound.Name(), install), "give the network its own bridge over an addressless VLAN interface"))
		}
	} else {
		found := false
		for _, nic := range native.Get("interfaces").Items() {
			if nic.Get("name").Text() != iface {
				continue
			}
			found = true
			if !slices.Contains([]string{"ethernet", "bond", "vlan"}, nic.Get("type").Text()) || slices.Contains([]string{"absent", "ignore"}, nic.Get("state").Text()) {
				issues = append(issues, invariantRemediation(base+".bridgeMapping.interface", "bridge interface "+iface+" is not an active ethernet, bond or vlan on Machine/"+bound.Name(), "choose an active ethernet, bond or vlan interface"))
			}
			static, dynamic := false, false
			for _, family := range []string{"ipv4", "ipv6"} {
				static = static || nic.Get(family, "address").Len() > 0
				dynamic = dynamic || nic.Get(family, "dhcp").Bool() || nic.Get(family, "autoconf").Bool()
			}
			switch {
			case dynamic && onBridge:
				issues = append(issues, invariantRemediation(base+".bridgeMapping.interface", "bridge interface "+iface+" of network "+name+" is addressed by DHCP or autoconf on Machine/"+bound.Name()+", which cannot move onto bridge "+bridge, "declare static addresses on "+iface+" in network.addresses, or choose an addressless interface and omit bridgeMapping.hostAddresses"))
			case dynamic:
				issues = append(issues, invariantRemediation(base+".bridgeMapping.interface", "bridge interface "+iface+" carries an address on Machine/"+bound.Name(), "choose an addressless interface"))
			case static && !onBridge:
				issues = append(issues, invariantRemediation(base+".bridgeMapping.interface", "bridge interface "+iface+" carries an address on Machine/"+bound.Name(), "choose an addressless interface, or set bridgeMapping.hostAddresses: bridge to move its addresses onto bridge "+bridge))
			}
			if onBridge && routesVia(native, iface) {
				issues = append(issues, invariantRemediation(base+".bridgeMapping.interface", "bridge interface "+iface+" of network "+name+" is the next-hop interface of a route on Machine/"+bound.Name()+", which leaves it once bridge "+bridge+" carries its addresses", "remove the route, or choose an interface no route names"))
			}
		}
		if !found || iface == install || reservedOVNInterface(iface) {
			issues = append(issues, invariantRemediation(base+".bridgeMapping.interface", "bridge interface "+iface+" is missing, reserved or the install interface on Machine/"+bound.Name(), "declare an addressless dedicated interface on every node, or use bridge br-ex"))
		}
		if other := interfaceBuiltOn(native, iface); other != "" {
			issues = append(issues, invariantRemediation(base+".bridgeMapping.interface", fmt.Sprintf("bridge interface %s of network %s carries interface %s on node %s", iface, name, other, bound.Name()), "use an addressless VLAN interface of "+iface+" as the bridge interface"))
		}
		for _, nic := range native.Get("interfaces").Items() {
			if nic.Get("name").Text() == bridge {
				issues = append(issues, invariantRemediation(base+".bridgeMapping.bridge", "bridge "+bridge+" already names a node interface on Machine/"+bound.Name(), "choose a new OVS bridge name"))
			}
		}
	}
	if mtu, ok := n.Get("mtu").Int64(); ok && mtu > interfaceMTU(native, mtuInterface) {
		issues = append(issues, invariantRemediation(base+".mtu", "secondary network "+name+" MTU exceeds its bridge interface MTU on Machine/"+bound.Name(), fmt.Sprintf("set mtu to at most %d", interfaceMTU(native, mtuInterface))))
	}
	return issues
}

func interfaceType(native api.Value, name string) string {
	for _, nic := range native.Get("interfaces").Items() {
		if nic.Get("name").Text() == name {
			return nic.Get("type").Text()
		}
	}
	return ""
}

func routesVia(native api.Value, name string) bool {
	for _, route := range native.Get("routes", "config").Items() {
		if route.Get("state").Text() != "absent" && route.Get("next-hop-interface").Text() == name {
			return true
		}
	}
	return false
}

func interfaceBuiltOn(native api.Value, name string) string {
	for _, nic := range native.Get("interfaces").Items() {
		if slices.Contains([]string{"absent", "ignore"}, nic.Get("state").Text()) {
			continue
		}
		other := nic.Get("name").Text()
		if nic.Get("type").Text() == "vlan" && nic.Get("vlan", "base-iface").Text() == name || slices.Contains(nic.Get("link-aggregation", "port").Strings(), name) {
			return other
		}
		if other == name && nic.Get("controller").Text() != "" {
			return nic.Get("controller").Text()
		}
	}
	return ""
}
func validateNetworkIPAM(n api.Value, base string) []api.Issue {
	issues := []api.Issue{}
	ipam := n.Get("ipam")
	enabled := ipam.Get("mode").Text() == "Enabled"
	subnets := ipam.Get("subnets").Strings()
	exclude := ipam.Get("excludeSubnets").Strings()
	if !enabled && (len(subnets) > 0 || len(exclude) > 0 || ipam.Has("lifecycle")) {
		issues = append(issues, invariantRemediation(base+".ipam", "disabled IPAM cannot declare subnets, excluded subnets or lifecycle", "set ipam.mode to Enabled or remove those fields"))
	}
	if enabled && (len(subnets) < 1 || len(subnets) > 2) {
		issues = append(issues, invariantRemediation(base+".ipam.subnets", "enabled IPAM requires one or two subnets", "declare one subnet per address family"))
	}
	families := map[int]bool{}
	parsed := []netip.Prefix{}
	for _, raw := range subnets {
		p, err := netip.ParsePrefix(raw)
		if err != nil {
			continue
		}
		parsed = append(parsed, p)
		if families[p.Addr().BitLen()] {
			issues = append(issues, invariantRemediation(base+".ipam.subnets", "IPAM subnets repeat an address family", "declare at most one IPv4 and one IPv6 subnet"))
		}
		families[p.Addr().BitLen()] = true
		if mtu, ok := n.Get("mtu").Int64(); ok && p.Addr().Is6() && mtu < 1280 {
			issues = append(issues, invariantRemediation(base+".mtu", "IPv6 secondary network MTU is below 1280", "set mtu to at least 1280"))
		}
	}
	if len(exclude) > 25 {
		issues = append(issues, invariantRemediation(base+".ipam.excludeSubnets", "IPAM declares more than 25 excluded subnets", "declare at most 25 excluded subnets"))
	}
	for i, raw := range exclude {
		p, err := netip.ParsePrefix(raw)
		if err != nil {
			continue
		}
		inside := false
		for _, parent := range parsed {
			if parent.Contains(p.Masked().Addr()) && parent.Bits() <= p.Bits() {
				inside = true
			}
		}
		if !inside {
			issues = append(issues, invariantRemediation(fmt.Sprintf("%s.ipam.excludeSubnets[%d]", base, i), "excluded subnet "+raw+" is outside IPAM subnets", "declare an excluded subnet contained by an IPAM subnet"))
		}
	}
	return issues
}

func ValidateNetworkProviders(o api.Object, c api.Catalog, r addons.Registry) []api.Issue {
	if o.Kind() != api.OpenShiftCluster {
		return nil
	}
	secondary := o.Spec().Get("networking", "secondaryNetworks").Len() > 0
	forwarded := ForwardedInterfaces(o, c, r)
	if !secondary && len(forwarded) == 0 || providesNMState(o, c, r) {
		return nil
	}
	if secondary {
		return []api.Issue{invariantRemediation("$.spec.networking.secondaryNetworks", "secondary networks of OpenShiftCluster/"+o.Name()+" require capability nmstate", "select a package or add-on providing nmstate in a binding for this cluster")}
	}
	return []api.Issue{invariantRemediation("$.spec.networking.ovnKubernetesConfig.gatewayConfig", fmt.Sprintf("MetalLB pools of OpenShiftCluster/%s announce on %s, where Bootwright enables IP forwarding through capability nmstate", o.Name(), strings.Join(forwarded, ", ")), "select a package or add-on providing nmstate in a binding for this cluster, or set gatewayConfig.ipForwarding: Global")}
}

func ForwardedInterfaces(o api.Object, c api.Catalog, r addons.Registry) []string {
	if o.Kind() != api.OpenShiftCluster || o.Spec().Get("networking", "ovnKubernetesConfig", "gatewayConfig", "ipForwarding").Text() == "Global" {
		return nil
	}
	instances, _ := addons.Instances(c, r)
	names := []string{}
	for _, i := range instances {
		if i.Instance.Cluster != o.Name() || i.Package.Name != "metallb" {
			continue
		}
		for _, pool := range i.Instance.Config.Get("pools").Items() {
			if iface := pool.Get("l2", "interface").Text(); iface != "" && ipv4Pool(pool) && !slices.Contains(names, iface) {
				names = append(names, iface)
			}
		}
	}
	slices.Sort(names)
	return names
}

func ipv4Pool(pool api.Value) bool {
	for _, entry := range pool.Get("addresses").Strings() {
		first, _, _ := strings.Cut(entry, "-")
		first, _, _ = strings.Cut(first, "/")
		if a, err := netip.ParseAddr(first); err == nil {
			return a.Unmap().Is4()
		}
	}
	return false
}

func providesNMState(o api.Object, c api.Catalog, r addons.Registry) bool {
	instances, _ := addons.Instances(c, r)
	for _, i := range instances {
		if i.Instance.Cluster == o.Name() && slices.Contains(i.Package.Provides, "nmstate") {
			return true
		}
	}
	for _, b := range c.OfKind(api.ClusterAddonBinding) {
		if b.Spec().Get("clusterRef").Text() != o.Name() {
			continue
		}
		selected, _ := addons.ExpandBinding(b, c)
		for _, a := range selected {
			if slices.Contains(a.Spec().Get("provides").Strings(), "nmstate") {
				return true
			}
		}
		for _, f := range b.Spec().Get("packages").Fields() {
			if p, ok := r.Find(f.Name); ok && slices.Contains(p.Provides, "nmstate") {
				return true
			}
		}
	}
	return false
}

func reservedOVNInterface(name string) bool {
	if slices.Contains([]string{"lo", "br-ex", "br-ext", "br-int", "br-local", "br-nexthop", "br0", "ext-vxlan", "ext", "int", "tun0"}, name) {
		return true
	}
	for _, prefix := range []string{"genev_sys_", "k8s-", "ovn-k8s-", "patch-br-", "vxlan_sys_"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func BridgeInterface(node, secondary api.Value) string {
	if at := overrideIndex(node, secondary); at >= 0 {
		return node.Get("bridgeInterfaces").Items()[at].Get("interface").Text()
	}
	return secondary.Get("bridgeMapping", "interface").Text()
}

func overrideIndex(node, secondary api.Value) int {
	return slices.IndexFunc(node.Get("bridgeInterfaces").Items(), func(entry api.Value) bool {
		return entry.Get("networkRef").Text() == secondary.Get("name").Text()
	})
}

func validateBridgeInterfaces(o api.Object, nodeIndex int, node api.Value) []api.Issue {
	issues := []api.Issue{}
	networks := o.Spec().Get("networking", "secondaryNetworks").Items()
	for i, entry := range node.Get("bridgeInterfaces").Items() {
		at := slices.IndexFunc(networks, func(n api.Value) bool { return n.Get("name").Text() == entry.Get("networkRef").Text() })
		if at < 0 || networks[at].Get("bridgeMapping", "bridge").Text() != "br-ex" {
			continue
		}
		issues = append(issues, invariantRemediation(fmt.Sprintf("$.spec.nodes[%d].bridgeInterfaces[%d].networkRef", nodeIndex, i), fmt.Sprintf("node %s overrides the bridge interface of secondary network %s, which shares br-ex, the bridge OVN-Kubernetes builds on the install interface", node.Get("name").Text(), entry.Get("networkRef").Text()),
			"remove the entry, or give "+entry.Get("networkRef").Text()+" a dedicated bridge and bridgeMapping.interface"))
	}
	owners := map[string]api.Value{}
	for _, n := range networks {
		bridge := n.Get("bridgeMapping", "bridge").Text()
		iface := BridgeInterface(node, n)
		if bridge == "br-ex" || iface == "" {
			continue
		}
		other, ok := owners[iface]
		if !ok {
			owners[iface] = n
			continue
		}
		if other.Get("bridgeMapping", "bridge").Text() == bridge {
			continue
		}
		subject, at := n, overrideIndex(node, n)
		if at < 0 {
			subject, at = other, overrideIndex(node, other)
		}
		if at < 0 {
			continue
		}
		issues = append(issues, invariantRemediation(fmt.Sprintf("$.spec.nodes[%d].bridgeInterfaces[%d].interface", nodeIndex, at), fmt.Sprintf("node %s assigns interface %s to bridge %s of secondary network %s and to bridge %s of secondary network %s", node.Get("name").Text(), iface, other.Get("bridgeMapping", "bridge").Text(), other.Get("name").Text(), bridge, n.Get("name").Text()),
			fmt.Sprintf("set a bridge interface of its own for secondary network %s on node %s, or put both networks on one bridge", subject.Get("name").Text(), node.Get("name").Text())))
	}
	for _, bridge := range sharedBridges(networks) {
		chosen := map[string]string{}
		for _, n := range networks {
			if n.Get("bridgeMapping", "bridge").Text() == bridge {
				chosen[BridgeInterface(node, n)] = n.Get("name").Text()
			}
		}
		if len(chosen) > 1 {
			issues = append(issues, invariantRemediation(fmt.Sprintf("$.spec.nodes[%d].bridgeInterfaces", nodeIndex), fmt.Sprintf("node %s gives the secondary networks on bridge %s different bridge interfaces", node.Get("name").Text(), bridge),
				"set one interface for every secondary network on bridge "+bridge+" on node "+node.Get("name").Text()))
		}
	}
	return issues
}

func sharedBridges(networks []api.Value) []string {
	count := map[string]int{}
	for _, n := range networks {
		count[n.Get("bridgeMapping", "bridge").Text()]++
	}
	shared := []string{}
	for bridge, n := range count {
		if n > 1 && bridge != "br-ex" {
			shared = append(shared, bridge)
		}
	}
	slices.Sort(shared)
	return shared
}

type reachedNetwork struct {
	node, segment string
	network       api.Object
}

type carriageNode struct {
	index int
	value api.Value
	bound api.Object
}

func validateSecondaryCarriage(n api.Value, index int, iface string, at carriageNode, c api.Catalog, native api.Value, install string, reported map[string]bool, first map[string]reachedNetwork) []api.Issue {
	node, bound := at.value, at.bound
	base := fmt.Sprintf("$.spec.networking.secondaryNetworks[%d]", index)
	name := n.Get("name").Text()
	dedicated := n.Get("bridgeMapping", "bridge").Text() != "br-ex"
	if !dedicated {
		iface = install
	}
	if iface == "" {
		return nil
	}
	vlan, _ := n.Get("vlan").Int64()
	on := fmt.Sprintf("interface %s of node %s (%s)", iface, node.Get("name").Text(), bound.Identity())
	issues := []api.Issue{}
	report := func(key, field, message, remedy string) {
		if !reported[key] {
			reported[key] = true
			issues = append(issues, invariantRemediation(field, message, remedy))
		}
	}
	for _, reach := range machine.Reached(bound, c, native, iface, int(vlan)) {
		path := on + ", NIC " + reach.NIC + " on " + reach.Segment
		switch {
		case reach.StackVLAN != "" && n.Has("vlan") && dedicated:
			report(base+".vlan", base+".vlan", fmt.Sprintf("secondary network %s tags VLAN %d on %s, which VLAN interface %s already tags", name, vlan, on, reach.StackVLAN),
				"remove vlan from secondary network "+name+", or name an untagged bridge interface")
		case reach.StackVLAN != "" && n.Has("vlan"):
		case !reach.Found():
			carried := "carries no Network untagged"
			field := base + ".bridgeMapping"
			if reach.VLAN != 0 {
				carried = fmt.Sprintf("carries no tagged Network with vlan %d", reach.VLAN)
			}
			if n.Has("vlan") {
				field = base + ".vlan"
			}
			remedy := "set vlan on secondary network " + name + " to the vlan of a Network the segment carries, or omit it for the one it carries untagged"
			if reach.Carriage != "" {
				remedy += ", or add the Network to " + reach.Carriage
			}
			report(field+"\x00"+reach.Segment, field, fmt.Sprintf("secondary network %s reaches %s, which %s", name, path, carried), remedy)
		default:
			if earlier, ok := first[name]; !ok {
				first[name] = reachedNetwork{node: node.Get("name").Text(), segment: reach.Segment, network: reach.Network}
			} else if earlier.network.Name() != reach.Network.Name() {
				field := base + ".bridgeMapping"
				if n.Has("vlan") {
					field = base + ".vlan"
				}
				report(field+"\x00split\x00"+reach.Network.Name(), field, fmt.Sprintf("secondary network %s reaches %s on node %s through %s, and %s through %s", name, earlier.network.Identity(), earlier.node, earlier.segment, reach.Network.Identity(), path),
					fmt.Sprintf("carry one Network for secondary network %s on every node: move NIC %s of %s with segmentRef to a segment that carries %s, or override the bridge interface of node %s at spec.nodes[%d].bridgeInterfaces", name, reach.NIC, bound.Identity(), earlier.network.Identity(), node.Get("name").Text(), at.index))
			}
			secondarySubnets(n, base, name, path, reach.Network, report)
		}
	}
	return issues
}

func secondarySubnets(n api.Value, base, name, path string, held api.Object, report func(key, field, message, remedy string)) {
	if n.Get("ipam", "mode").Text() != "Enabled" {
		return
	}
	for i, raw := range n.Get("ipam", "subnets").Strings() {
		subnet, err := netip.ParsePrefix(raw)
		if err != nil {
			continue
		}
		family := network.Family(subnet.Addr())
		cidr, ok := network.Prefix(held, family)
		if ok && cidr == subnet.Masked() {
			continue
		}
		field := fmt.Sprintf("%s.ipam.subnets[%d]", base, i)
		remedy := "set it to spec." + family + ".cidr " + cidr.String() + " of " + held.Identity() + ", keeping addresses from pods with ipam.excludeSubnets"
		if !ok {
			remedy = "declare spec." + family + ".cidr on " + held.Identity() + ", or remove the subnet"
		}
		report(field+"\x00"+held.Name(), field, fmt.Sprintf("IPAM subnet %s of secondary network %s is not the %s cidr of %s, which it carries through %s", raw, name, family, held.Identity(), path), remedy)
	}
}
