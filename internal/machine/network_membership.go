package machine

import (
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/network"
)

func assignmentNetwork(o api.Object, c api.Catalog, address netip.Addr) (network.Held, []network.Held, []network.Held) {
	site := o.Spec().Get("placement", "site").Text()
	held := network.Containing(c, address)
	var scoped []network.Held
	for _, candidate := range held {
		if network.Serves(candidate.Network, site) {
			scoped = append(scoped, candidate)
		}
	}
	if len(scoped) == 1 {
		return scoped[0], held, scoped
	}
	return network.Held{}, held, scoped
}

func validateNetworkMembership(o api.Object, c api.Catalog) []api.Issue {
	if !network.Declared(c) {
		return nil
	}
	var issues []api.Issue
	native, composed := ComposeNetwork(o, c)
	site := o.Spec().Get("placement", "site").Text()
	for i, entry := range o.Spec().Get("network", "addresses").Items() {
		prefix, err := netip.ParsePrefix(entry.Get("address").Text())
		iface := entry.Get("interface").Text()
		if err != nil || iface == "" {
			continue
		}
		path := fmt.Sprintf("$.spec.network.addresses[%d]", i)
		address := prefix.Addr()
		family := network.Family(address)
		match, held, scoped := assignmentNetwork(o, c, address)
		switch {
		case len(held) == 0:
			issues = appendIssues(issues, invariant(path+".address", "address "+address.String()+" of interface "+iface+" on "+o.Identity()+" lies in no declared Network",
				"declare a Network whose spec."+family+".cidr contains "+address.String()+", or correct "+path[2:]+".address on "+o.Identity()))
			continue
		case len(scoped) == 0:
			issues = appendIssues(issues, invariant(path+".address", "address "+address.String()+" of interface "+iface+" on "+o.Identity()+" lies in "+heldNames(held)+", not scoped to site "+site+" where the Machine is placed",
				"add site "+site+" to spec.sites of "+heldNames(held)+", or assign "+o.Identity()+" an address in a Network of site "+site))
			continue
		case len(scoped) > 1:
			issues = appendIssues(issues, invariant(path+".address", "address "+address.String()+" of interface "+iface+" on "+o.Identity()+" lies in more than one Network, "+heldNames(scoped),
				"set spec.placement.site on "+o.Identity()+" to the site whose Network it uses, or scope those Networks to disjoint spec.sites"))
			continue
		}
		if prefix.Bits() != match.Prefix.Bits() {
			issues = appendIssues(issues, invariant(path+".address", "address "+prefix.String()+" of interface "+iface+" on "+o.Identity()+" has prefix length /"+strconv.Itoa(prefix.Bits())+", and its "+match.Network.Identity()+" is "+match.Prefix.String(),
				"write "+path[2:]+".address on "+o.Identity()+" as "+netip.PrefixFrom(address, match.Prefix.Bits()).String()))
		}
		if len(composed) == 0 && native.Present() {
			issues = appendIssues(issues, validateAssignmentVLAN(o, native, match.Network, iface, path)...)
		}
	}
	return issues
}

func validateAssignmentVLAN(o api.Object, native api.Value, owner api.Object, iface, path string) []api.Issue {
	ids := stackVLANs(native, iface, map[string]bool{})
	if len(ids) == 0 {
		return nil
	}
	vlan, tagged := network.VLAN(owner)
	if !tagged {
		return []api.Issue{invariant(path+".interface", "interface "+iface+" on "+o.Identity()+" tags VLAN "+joinIDs(ids)+", and its "+owner.Identity()+" declares no vlan, so it is carried untagged",
			"set spec.vlan on "+owner.Identity()+" to the VLAN its switch ports tag, or assign the address to an untagged interface of "+o.Identity())}
	}
	for _, id := range ids {
		if id != vlan {
			return []api.Issue{invariant(path+".interface", "interface "+iface+" on "+o.Identity()+" tags VLAN "+strconv.Itoa(id)+", and its "+owner.Identity()+" is VLAN "+strconv.Itoa(vlan),
				"set vlan.id "+strconv.Itoa(vlan)+" on the VLAN interface "+iface+" stacks on, or assign the address to the interface of "+owner.Identity())}
		}
	}
	return nil
}

func stackVLANs(native api.Value, name string, visiting map[string]bool) []int {
	if visiting[name] {
		return nil
	}
	visiting[name] = true
	defer delete(visiting, name)
	iface, _ := namedValue(native.Get("interfaces"), name)
	if id, ok := iface.Get("vlan", "id").Int64(); ok && (iface.Get("type").Text() == "vlan" || iface.Has("vlan", "base-iface")) {
		return []int{int(id)}
	}
	ports := bondPorts(native, name)
	for _, port := range iface.Get("bridge", "port").Items() {
		if member := port.Get("name").Text(); member != "" && !slices.Contains(ports, member) {
			ports = append(ports, member)
		}
	}
	var ids []int
	for _, port := range ports {
		for _, id := range stackVLANs(native, port, visiting) {
			if !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

func validateNetworkGateway(o api.Object, c api.Catalog, prefix netip.Prefix, hop netip.Addr, source string) []api.Issue {
	match, _, _ := assignmentNetwork(o, c, prefix.Addr())
	if match.Network.Name() == "" || hop.IsLinkLocalUnicast() {
		return nil
	}
	gateway, declared := network.Gateway(match.Network, network.Family(prefix.Addr()))
	if !declared || gateway == hop {
		return nil
	}
	return []api.Issue{invariant("$.spec.network.installAddressRef",
		"install gateway "+hop.String()+", the next hop of the default route in "+source+" of "+o.Identity()+", is not gateway "+gateway.String()+" of its install network "+match.Network.Identity(),
		"set that default route's next-hop-address in "+source+" to "+gateway.String()+", or correct spec."+network.Family(prefix.Addr())+".gateway of "+match.Network.Identity())}
}

func heldNames(held []network.Held) string {
	var names []string
	for _, entry := range held {
		if identity := entry.Network.Identity(); !slices.Contains(names, identity) {
			names = append(names, identity)
		}
	}
	return strings.Join(names, " and ")
}

func joinIDs(ids []int) string {
	var text []string
	for _, id := range ids {
		text = append(text, strconv.Itoa(id))
	}
	return strings.Join(text, " and ")
}
