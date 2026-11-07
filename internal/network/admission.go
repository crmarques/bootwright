package network

import (
	"fmt"
	"net/netip"
	"slices"
	"strconv"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

var families = []string{"ipv4", "ipv6"}

func Normalize(o api.Object, _ api.Catalog) (api.Object, []api.Issue) {
	if o.Kind() != api.Network {
		return o, nil
	}
	spec := o.Spec()
	for _, family := range families {
		if prefix, err := netip.ParsePrefix(spec.Get(family, "cidr").Text()); err == nil && prefix == prefix.Masked() {
			spec = spec.WithPath(api.StringValue(prefix.String()), family, "cidr")
		}
		if gateway, err := netip.ParseAddr(spec.Get(family, "gateway").Text()); err == nil {
			spec = spec.WithPath(api.StringValue(gateway.String()), family, "gateway")
		}
	}
	return o.WithSpec(spec), nil
}

func Validate(o api.Object, c api.Catalog) []api.Issue {
	if o.Kind() != api.Network {
		return nil
	}
	spec := o.Spec()
	var issues []api.Issue
	issues = append(issues, validateLifecycle(o)...)
	if !spec.Has("ipv4") && !spec.Has("ipv6") {
		issues = append(issues, invariant("$.spec", o.Identity()+" declares no subnet, so no address can lie in it",
			"declare spec.ipv4 or spec.ipv6 with the network's cidr on "+o.Identity()))
	}
	for _, family := range families {
		issues = append(issues, validateSubnet(o, family)...)
	}
	issues = append(issues, validateSites(o, c)...)
	return append(issues, validateDomain(o, c)...)
}

func validateLifecycle(o api.Object) []api.Issue {
	spec := o.Spec()
	provided := spec.Get("provided")
	if !provided.Present() {
		return nil
	}
	var arms []string
	for _, arm := range api.Schema(api.Network).Arms {
		if spec.Has(arm) {
			arms = append(arms, arm)
		}
	}
	if provided.Bool() {
		var issues []api.Issue
		for _, arm := range arms {
			issues = append(issues, invariant("$.spec."+arm, "a provided network is realized by nothing, so "+o.Identity()+" cannot also name spec."+arm,
				"remove spec."+arm+" from "+o.Identity()+", or set spec.provided to false for a network Bootwright realizes"))
		}
		return issues
	}
	if len(arms) == 0 {
		return []api.Issue{invariant("$.spec", o.Identity()+" is not provided, so something must realize it, and it names no realization arm",
			"name the substrate that realizes it, spec.libvirt, or set spec.provided to true on "+o.Identity()+" for a network that exists outside Bootwright")}
	}
	return nil
}

func validateSubnet(o api.Object, family string) []api.Issue {
	subnet := o.Spec().Get(family)
	if !subnet.Present() {
		return nil
	}
	path := "$.spec." + family
	prefix, err := netip.ParsePrefix(subnet.Get("cidr").Text())
	if err != nil {
		return nil
	}
	if prefix.Addr().Is4() != (family == "ipv4") || prefix.Addr().Zone() != "" {
		return []api.Issue{invariant(path+".cidr", "spec."+family+".cidr of "+o.Identity()+" is not an "+familyName(family)+" prefix",
			"set spec."+family+".cidr to an "+familyName(family)+" prefix, or move the subnet to spec."+other(family))}
	}
	if prefix != prefix.Masked() {
		return []api.Issue{invariant(path+".cidr", "spec."+family+".cidr "+subnet.Get("cidr").Text()+" of "+o.Identity()+" sets host bits, so it names an address, not a network",
			"write spec."+family+".cidr as "+prefix.Masked().String()+", and the host address as spec."+family+".gateway if it is the gateway")}
	}
	gateway, err := netip.ParseAddr(subnet.Get("gateway").Text())
	if err != nil {
		return nil
	}
	if reason := ReservedAddress(prefix, gateway); reason != "" {
		return []api.Issue{invariant(path+".gateway", "gateway "+gateway.String()+" of "+o.Identity()+" "+reason,
			"set spec."+family+".gateway to a host address inside "+prefix.String()+", or remove it when the network has no gateway")}
	}
	return nil
}

func ReservedAddress(prefix netip.Prefix, address netip.Addr) string {
	switch {
	case address.Is4() != prefix.Addr().Is4() || !prefix.Contains(address):
		return "lies outside " + prefix.String()
	case address.Is4() && prefix.Bits() < 31 && address == prefix.Addr():
		return "is the network address of " + prefix.String()
	case address.Is4() && prefix.Bits() < 31 && address == lastAddress(prefix):
		return "is the broadcast address of " + prefix.String()
	case address.Is6() && prefix.Bits() < 127 && address == prefix.Addr():
		return "is the Subnet-Router anycast address of " + prefix.String() + " (RFC 4291)"
	case address.Is6() && reservedAnycast(prefix, address):
		return "is a reserved subnet anycast address of " + prefix.String() + " (RFC 2526)"
	}
	return ""
}

func lastAddress(prefix netip.Prefix) netip.Addr {
	bytes := prefix.Addr().AsSlice()
	for bit := prefix.Bits(); bit < len(bytes)*8; bit++ {
		bytes[bit/8] |= 0x80 >> (bit % 8)
	}
	last, _ := netip.AddrFromSlice(bytes)
	return last
}

func reservedAnycast(prefix netip.Prefix, address netip.Addr) bool {
	bytes := address.As16()
	if prefix.Bits() == 64 {
		return bytes[8] == 0xfd && bytes[9] == 0xff && bytes[10] == 0xff && bytes[11] == 0xff && bytes[12] == 0xff && bytes[13] == 0xff && bytes[14] == 0xff && bytes[15] >= 0x80
	}
	if prefix.Bits() > 121 {
		return false
	}
	last := lastAddress(prefix).As16()
	return slices.Equal(bytes[:15], last[:15]) && bytes[15] >= 0x80
}

func validateSites(o api.Object, c api.Catalog) []api.Issue {
	envs := c.OfKind(api.Environment)
	if len(envs) != 1 {
		return nil
	}
	declared := map[string]bool{}
	for _, site := range envs[0].Spec().Get("sites").Items() {
		declared[site.Get("name").Text()] = true
	}
	var issues []api.Issue
	for i, site := range o.Spec().Get("sites").Strings() {
		if !declared[site] {
			issues = append(issues, api.Issue{Code: "api.reference", Field: fmt.Sprintf("$.spec.sites[%d]", i),
				Message:     "site " + site + " of " + o.Identity() + " names no site of " + envs[0].Identity(),
				Remediation: "declare site " + site + " in spec.sites of " + envs[0].Identity() + ", or remove it from spec.sites of " + o.Identity()})
		}
	}
	return issues
}

func validateDomain(o api.Object, c api.Catalog) []api.Issue {
	var issues []api.Issue
	vlan, tagged := VLAN(o)
	for _, peer := range c.OfKind(api.Network) {
		if peer.Identity() == o.Identity() || !sitesOverlap(o, peer) {
			continue
		}
		if id, ok := VLAN(peer); tagged && ok && id == vlan {
			issues = append(issues, invariant("$.spec.vlan", o.Identity()+" and "+peer.Identity()+" share VLAN "+strconv.Itoa(vlan)+" in a site they both reach, and a site has one 802.1Q domain",
				"give "+o.Identity()+" or "+peer.Identity()+" another spec.vlan, or scope them to disjoint spec.sites"))
		}
		for _, family := range families {
			mine, ok := Prefix(o, family)
			theirs, found := Prefix(peer, family)
			if ok && found && mine.Overlaps(theirs) {
				issues = append(issues, invariant("$.spec."+family+".cidr", o.Identity()+" "+mine.String()+" overlaps "+peer.Identity()+" "+theirs.String()+" in a site they both reach, so an address would lie in both",
					"give "+o.Identity()+" or "+peer.Identity()+" a spec."+family+".cidr the other does not overlap, or scope them to disjoint spec.sites"))
			}
		}
	}
	return issues
}

func sitesOverlap(a, b api.Object) bool {
	left, right := a.Spec().Get("sites"), b.Spec().Get("sites")
	if !left.Present() || !right.Present() {
		return true
	}
	return slices.ContainsFunc(left.Strings(), func(site string) bool { return slices.Contains(right.Strings(), site) })
}

func familyName(family string) string {
	if family == "ipv4" {
		return "IPv4"
	}
	return "IPv6"
}

func other(family string) string {
	if family == "ipv4" {
		return "ipv6"
	}
	return "ipv4"
}

func invariant(field, message, remediation string) api.Issue {
	return api.Issue{Code: "api.invariant", Field: field, Message: message, Remediation: remediation}
}
