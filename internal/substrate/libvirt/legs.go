package libvirt

import (
	"net/netip"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/substrate"
)

func legsFor(catalog api.Catalog, provider api.Object, contextName string) ([]Leg, []Trunk, error) {
	var legs []Leg
	var trunks []Trunk
	for _, segment := range substrate.Segments(catalog, provider).Items() {
		managed, isManaged := substrate.ManagedSegmentNetwork(segment)
		if !isManaged || !managed.Tagged() {
			continue
		}
		name := segment.Get("name").Text()
		device := managed.HostDevice()
		if !substrate.SafeSegment(name) || !api.ValidLexical("bridge", managed.Bridge) || len(device) > substrate.MaxInterfaceName {
			return nil, nil, refusal("lifecycle.state", "the tagged managed Network/"+name+" names no safe leg device", "rename Network/"+name+" or shorten its spec.libvirt.bridge")
		}
		if prefix, err := netip.ParsePrefix(managed.Prefix); err != nil || !prefix.Addr().Is4() {
			return nil, nil, refusal("api.value", "the tagged managed Network/"+name+" has no IPv4 host address inside its subnet", "set spec.libvirt.hostAddress or an IPv4 gateway on Network/"+name)
		}
		connection := substrate.LegConnectionName(contextName, name)
		legs = append(legs, Leg{Address: managed.Prefix, Device: device, Forward: managed.Forward, Name: connection, Network: name,
			Trunk: managed.Bridge, UUID: substrate.ConnectionUUID(contextName, connection), VLAN: managed.VLAN})
		if !slices.ContainsFunc(trunks, func(trunk Trunk) bool { return trunk.Bridge == managed.Bridge }) {
			trunk := substrate.TrunkConnectionName(contextName, provider.Name(), managed.Bridge)
			trunks = append(trunks, Trunk{Bridge: managed.Bridge, MAC: substrate.TrunkMAC(contextName, provider.Name(), managed.Bridge), Name: trunk,
				UUID: substrate.ConnectionUUID(contextName, trunk)})
		}
	}
	slices.SortFunc(legs, func(x, y Leg) int { return strings.Compare(x.Name, y.Name) })
	slices.SortFunc(trunks, func(x, y Trunk) int { return strings.Compare(x.Name, y.Name) })
	return legs, trunks, nil
}
