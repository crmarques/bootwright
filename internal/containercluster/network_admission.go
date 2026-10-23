package containercluster

import (
	"fmt"
	"net/netip"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	"github.com/crmarques/bootwright/internal/machine"
)

func NormalizeNetworkIntent(o api.Object) api.Object {
	if o.Kind() != api.ContainerCluster {
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
	if o.Kind() != api.ContainerCluster {
		return nil
	}
	issues := []api.Issue{}
	mtu, hasMTU := o.Spec().Get("networking", "ovnKubernetesConfig", "mtu").Int64()
	familyBits := 0
	networks, _ := machineNetworks(o, c)
	if len(networks) > 0 {
		familyBits = networks[0].Addr().BitLen()
	}
	for nodeIndex, node := range o.Spec().Get("nodes").Items() {
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
		install := defaultRouteInterface(native, familyBits)
		if hasMTU {
			limit := interfaceMTU(native, install) - 100
			if mtu > limit {
				issues = append(issues, invariantRemediation("$.spec.networking.ovnKubernetesConfig.mtu", fmt.Sprintf("cluster MTU %d exceeds the default-route interface MTU minus 100 on Machine/%s", mtu, bound.Name()), fmt.Sprintf("set the cluster MTU to at most %d, or increase the interface MTU", limit)))
			}
		}
		for i, network := range o.Spec().Get("networking", "secondaryNetworks").Items() {
			issues = append(issues, validateNetworkUplink(network, i, bound, native, install)...)
		}
	}
	seen := map[string]string{}
	uplinks := map[string]string{}
	bridges := map[string]string{}
	for i, n := range o.Spec().Get("networking", "secondaryNetworks").Items() {
		base := fmt.Sprintf("$.spec.networking.secondaryNetworks[%d]", i)
		name := n.Get("name").Text()
		if name == "physnet" || name == "exgwphysnet" {
			issues = append(issues, invariantRemediation(base+".name", "secondary network "+name+" uses a reserved physical network name", "choose a name other than physnet or exgwphysnet"))
		}
		bridge := n.Get("uplink", "bridge").Text()
		iface := n.Get("uplink", "interface").Text()
		if !api.ValidLexical("ifname", bridge) || bridge != "br-ex" && reservedOVNInterface(bridge) {
			issues = append(issues, invariantRemediation(base+".uplink.bridge", "secondary network "+name+" has an invalid or reserved bridge name", "use br-ex or a non-reserved Linux interface name of at most 15 characters"))
		}
		if bridge == "br-ex" && iface != "" || bridge != "br-ex" && iface == "" {
			issues = append(issues, invariantRemediation(base+".uplink", "secondary network "+name+" must select br-ex alone or a dedicated interface and bridge", "omit interface when bridge is br-ex, otherwise supply both interface and bridge"))
		}
		if iface != "" {
			if other, ok := uplinks[iface]; ok && other != bridge {
				issues = append(issues, invariantRemediation(base+".uplink.bridge", "interface "+iface+" is assigned to multiple OVS bridges", "use one bridge name for every network sharing an interface"))
			}
			uplinks[iface] = bridge
		}
		if other, ok := bridges[bridge]; ok && other != iface {
			issues = append(issues, invariantRemediation(base+".uplink.interface", "bridge "+bridge+" selects multiple uplink interfaces", "use one interface for every network sharing a bridge"))
		}
		bridges[bridge] = iface
		for _, label := range n.Get("namespaceSelector", "matchLabels").Fields() {
			if !api.ValidLexical("kubernetes-label-key", label.Name) || !api.ValidLexical("kubernetes-label-value", label.Value.Text()) {
				issues = append(issues, invariantRemediation(base+".namespaceSelector.matchLabels", "namespace selector has an invalid label key or value", "use valid Kubernetes label keys and values"))
			}
		}
		key := bridge + "/" + n.Get("vlan").Text()
		if other, ok := seen[key]; ok {
			issues = append(issues, invariantRemediation(base+".vlan", "secondary networks "+other+" and "+name+" share an uplink VLAN", "use distinct VLANs on a shared uplink, with at most one untagged network"))
		}
		seen[key] = name
		if n.Get("namespaceSelector", "matchLabels").Len() == 0 {
			issues = append(issues, invariantRemediation(base+".namespaceSelector.matchLabels", "secondary network "+name+" selects no namespace labels", "supply at least one namespace label"))
		}
		issues = append(issues, validateNetworkIPAM(n, base)...)
	}
	return issues
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
	for _, nic := range native.Get("interfaces").Items() {
		if nic.Get("name").Text() == name {
			if n, ok := nic.Get("mtu").Int64(); ok {
				return n
			}
		}
	}
	return 1500
}
func validateNetworkUplink(n api.Value, index int, bound api.Object, native api.Value, install string) []api.Issue {
	issues := []api.Issue{}
	base := fmt.Sprintf("$.spec.networking.secondaryNetworks[%d]", index)
	name := n.Get("name").Text()
	iface := n.Get("uplink", "interface").Text()
	bridge := n.Get("uplink", "bridge").Text()
	mtuInterface := iface
	if bridge == "br-ex" {
		mtuInterface = install
	} else {
		found := false
		for _, nic := range native.Get("interfaces").Items() {
			if nic.Get("name").Text() != iface {
				continue
			}
			found = true
			if !slices.Contains([]string{"ethernet", "bond", "vlan"}, nic.Get("type").Text()) || slices.Contains([]string{"absent", "ignore"}, nic.Get("state").Text()) {
				issues = append(issues, invariantRemediation(base+".uplink.interface", "uplink "+iface+" is not an active ethernet, bond or vlan on Machine/"+bound.Name(), "choose an active ethernet, bond or vlan interface"))
			}
			for _, family := range []string{"ipv4", "ipv6"} {
				if nic.Get(family, "address").Len() > 0 || nic.Get(family, "dhcp").Bool() || nic.Get(family, "autoconf").Bool() {
					issues = append(issues, invariantRemediation(base+".uplink.interface", "uplink "+iface+" carries an address on Machine/"+bound.Name(), "choose an addressless interface"))
				}
			}
		}
		if !found || iface == install || reservedOVNInterface(iface) {
			issues = append(issues, invariantRemediation(base+".uplink.interface", "uplink "+iface+" is missing, reserved or the install interface on Machine/"+bound.Name(), "declare an addressless dedicated interface on every node, or use bridge br-ex"))
		}
		for _, nic := range native.Get("interfaces").Items() {
			if nic.Get("name").Text() == bridge {
				issues = append(issues, invariantRemediation(base+".uplink.bridge", "bridge "+bridge+" already names a node interface on Machine/"+bound.Name(), "choose a new OVS bridge name"))
			}
		}
	}
	if mtu, ok := n.Get("mtu").Int64(); ok && mtu > interfaceMTU(native, mtuInterface) {
		issues = append(issues, invariantRemediation(base+".mtu", "secondary network "+name+" MTU exceeds its uplink MTU on Machine/"+bound.Name(), fmt.Sprintf("set mtu to at most %d", interfaceMTU(native, mtuInterface))))
	}
	return issues
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
	if o.Kind() != api.ContainerCluster || o.Spec().Get("networking", "secondaryNetworks").Len() == 0 {
		return nil
	}
	instances, _ := addons.Instances(c, r)
	for _, i := range instances {
		if i.Instance.Cluster == o.Name() && slices.Contains(i.Package.Provides, "nmstate") {
			return nil
		}
	}
	for _, b := range c.OfKind(api.ClusterAddonBinding) {
		if b.Spec().Get("clusterRef").Text() != o.Name() {
			continue
		}
		selected, _ := addons.ExpandBinding(b, c)
		for _, a := range selected {
			if slices.Contains(a.Spec().Get("provides").Strings(), "nmstate") {
				return nil
			}
		}
	}
	return []api.Issue{invariantRemediation("$.spec.networking.secondaryNetworks", "secondary networks of ContainerCluster/"+o.Name()+" require capability nmstate", "select a package or add-on providing nmstate in a binding for this cluster")}
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
