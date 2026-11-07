package network

import (
	"net/netip"
	"slices"
	"strconv"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

type Held struct {
	Network api.Object
	Prefix  netip.Prefix
}

func Family(address netip.Addr) string {
	if address.Is4() {
		return "ipv4"
	}
	return "ipv6"
}

func Prefix(o api.Object, family string) (netip.Prefix, bool) {
	prefix, err := netip.ParsePrefix(o.Spec().Get(family, "cidr").Text())
	if err != nil || prefix.Addr().Is4() != (family == "ipv4") {
		return netip.Prefix{}, false
	}
	return prefix.Masked(), true
}

func Gateway(o api.Object, family string) (netip.Addr, bool) {
	gateway, err := netip.ParseAddr(o.Spec().Get(family, "gateway").Text())
	return gateway, err == nil && gateway.Is4() == (family == "ipv4")
}

func VLAN(o api.Object) (int, bool) {
	vlan := o.Spec().Get("vlan")
	if !vlan.Present() {
		return 0, false
	}
	id, err := strconv.Atoi(vlan.Text())
	return id, err == nil
}

func Serves(o api.Object, site string) bool {
	sites := o.Spec().Get("sites")
	return site == "" || !sites.Present() || slices.Contains(sites.Strings(), site)
}

func CIDRs(c api.Catalog) []Held {
	var held []Held
	for _, o := range c.OfKind(api.Network) {
		for _, family := range families {
			if prefix, ok := Prefix(o, family); ok {
				held = append(held, Held{Network: o, Prefix: prefix})
			}
		}
	}
	return held
}

func Containing(c api.Catalog, address netip.Addr) []Held {
	var held []Held
	for _, candidate := range CIDRs(c) {
		if candidate.Prefix.Contains(address) {
			held = append(held, candidate)
		}
	}
	return held
}
