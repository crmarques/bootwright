package machine

import (
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/network"
	"github.com/crmarques/bootwright/internal/substrate"
)

type Reach struct {
	NIC, Segment, Carriage string
	VLAN                   int
	StackVLAN              string
	Network                api.Object
}

func (r Reach) Found() bool { return r.Network.Name() != "" }

func Reached(o api.Object, c api.Catalog, native api.Value, iface string, vlan int) []Reach {
	provider, found := Provider(o, c)
	if !found {
		return nil
	}
	var reached []Reach
	seen := map[string]bool{}
	for _, leaf := range stackLeaves(native, iface, false, map[string]bool{}) {
		nic := substrate.InterfaceNIC(o, leaf.name)
		reference := substrate.NICSegment(c, provider, o, nic)
		segment, ok := substrate.NamedSegment(c, provider, reference)
		if !ok || !slices.Contains(substrate.MachineNICs(o), nic) || seen[reference] {
			continue
		}
		seen[reference] = true
		entry := Reach{NIC: nic, Segment: substrate.SegmentLabel(c, provider, reference), VLAN: vlan}
		if leaf.tagged {
			entry.StackVLAN = stackVLAN(native, leaf.path)
			stacked, _ := namedValue(native.Get("interfaces"), entry.StackVLAN)
			id, _ := stacked.Get("vlan", "id").Int64()
			entry.VLAN = int(id)
		}
		if entry.VLAN == 0 || substrate.TaggedCarriage(substrate.Variant(provider)) {
			entry.Carriage = substrate.CarriageField(c, provider, reference, entry.VLAN != 0)
		}
		candidates := substrate.SegmentTagged(segment)
		if entry.VLAN == 0 {
			candidates = []string{substrate.SegmentUntagged(segment)}
		}
		for _, name := range candidates {
			held, ok := c.Find(api.Network, name)
			if id, tagged := network.VLAN(held); ok && (entry.VLAN == 0 || tagged && id == entry.VLAN) {
				entry.Network = held
				break
			}
		}
		reached = append(reached, entry)
	}
	return reached
}
