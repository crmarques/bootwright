package libvirt

import (
	"cmp"
	"net/netip"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/substrate"
)

type RoutedPair struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type Routing struct {
	Forwarding     bool         `json:"forwarding"`
	KeptPrefixes   []string     `json:"keptPrefixes"`
	ManagedDevices []string     `json:"managedDevices"`
	NATPrefixes    []string     `json:"natPrefixes"`
	Routed         []RoutedPair `json:"routed"`
	Unit           string       `json:"unit"`
}

func RoutingTable(r Routing) string { return strings.TrimSuffix(r.Unit, ".service") }

func routingFor(catalog api.Catalog, provider api.Object, contextName string, networks []Network, legs []Leg, trunks []Trunk, refused []string) *Routing {
	if len(legs) == 0 {
		return nil
	}
	routing := Routing{KeptPrefixes: []string{}, ManagedDevices: []string{}, NATPrefixes: []string{}, Routed: []RoutedPair{}, Unit: substrate.RoutingUnit(contextName, provider.Name())}
	var kept, nat []netip.Prefix
	devices := map[string]string{}
	for _, network := range networks {
		if !network.Managed {
			continue
		}
		routing.ManagedDevices = append(routing.ManagedDevices, network.Bridge)
		if prefix, err := netip.ParsePrefix(network.Address); err == nil && prefix.Addr().Is4() {
			kept = append(kept, prefix.Masked())
		}
	}
	for _, trunk := range trunks {
		routing.ManagedDevices = append(routing.ManagedDevices, trunk.Bridge)
	}
	for _, leg := range legs {
		devices[leg.Network] = leg.Device
		routing.ManagedDevices = append(routing.ManagedDevices, leg.Device)
		prefix := netip.MustParsePrefix(leg.Address).Masked()
		kept = append(kept, prefix)
		if leg.Forward == "nat" {
			nat = append(nat, prefix)
		}
	}
	for _, value := range refused {
		if prefix, err := netip.ParsePrefix(value); err == nil && prefix.Addr().Is4() {
			kept = append(kept, prefix)
		}
	}
	for _, leg := range legs {
		network, _ := catalog.Find(api.Network, leg.Network)
		for _, peer := range network.Spec().Get(substrate.ArmLibvirt, "routedNetworkRefs").Strings() {
			if device, ok := devices[peer]; ok && device != leg.Device {
				routing.Routed = append(routing.Routed, RoutedPair{From: leg.Device, To: device}, RoutedPair{From: device, To: leg.Device})
			}
		}
	}
	slices.Sort(routing.ManagedDevices)
	routing.ManagedDevices = slices.Compact(routing.ManagedDevices)
	slices.SortFunc(routing.Routed, func(x, y RoutedPair) int { return cmp.Or(strings.Compare(x.From, y.From), strings.Compare(x.To, y.To)) })
	routing.Routed = slices.Compact(routing.Routed)
	routing.KeptPrefixes, routing.NATPrefixes = canonicalPrefixes(kept), canonicalPrefixes(nat)
	routing.Forwarding = len(routing.NATPrefixes) != 0 || len(routing.Routed) != 0
	return &routing
}

func canonicalPrefixes(prefixes []netip.Prefix) []string {
	slices.SortFunc(prefixes, func(x, y netip.Prefix) int {
		return cmp.Or(x.Addr().Compare(y.Addr()), cmp.Compare(x.Bits(), y.Bits()))
	})
	values := []string{}
	for _, prefix := range slices.Compact(prefixes) {
		values = append(values, prefix.String())
	}
	return values
}
