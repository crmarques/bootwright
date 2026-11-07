package machine

import (
	"net/netip"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/network"
)

func AssignedNetwork(o api.Object, c api.Catalog, entry api.Value) (network.Held, bool) {
	address, err := netip.ParseAddr(entry.Get("address").Text())
	if err != nil || entry.Get("interface").Text() == "" {
		return network.Held{}, false
	}
	match, _, _ := assignmentNetwork(o, c, address)
	return match, match.Network.Name() != ""
}

func AssignedPrefix(o api.Object, c api.Catalog, entry api.Value) (netip.Prefix, bool) {
	match, ok := AssignedNetwork(o, c, entry)
	if !ok {
		return netip.Prefix{}, false
	}
	address, _ := netip.ParseAddr(entry.Get("address").Text())
	return netip.PrefixFrom(address, match.Prefix.Bits()), true
}

func AssignedNetworks(o api.Object, c api.Catalog) []network.Held {
	var held []network.Held
	for _, entry := range o.Spec().Get("network", "addresses").Items() {
		if match, ok := AssignedNetwork(o, c, entry); ok {
			held = append(held, match)
		}
	}
	return held
}

func composedPrefix(o api.Object, c api.Catalog, entry api.Value, field string) (netip.Prefix, []api.Issue) {
	if membership := assignmentIssues(o, c, entry, field); len(membership) != 0 {
		return netip.Prefix{}, membership
	}
	prefix, assigned := AssignedPrefix(o, c, entry)
	if assigned && reservedEnd(prefix, prefix.Addr()) != "" {
		return netip.Prefix{}, []api.Issue{valueIssue(field+".address", "an IPv4 assignment is a host address of its Network, never its network or broadcast address",
			"correct "+field[2:]+".address on "+o.Identity()+" to a host address inside "+prefix.Masked().String())}
	}
	return prefix, nil
}
