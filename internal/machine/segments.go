package machine

import (
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/network"
	"github.com/crmarques/bootwright/internal/substrate"
)

func validateSegments(c api.Catalog, o, provider api.Object, native api.Value) []api.Issue {
	variant := substrate.Variant(provider)
	issues := []api.Issue{}
	if substrate.VirtualArm(variant) {
		issues = appendIssues(issues, validateVirtualMACs(o, native, variant)...)
	}
	issues = appendIssues(issues, validateBondSegments(o, native, "$.spec.substrate."+variant+".nics")...)
	issues = appendIssues(issues, validateNICSegmentSelection(c, o, provider, native)...)
	return appendIssues(issues, validateCapacities(c, o, provider, native)...)
}

func validateNICSegmentSelection(c api.Catalog, o, provider api.Object, native api.Value) []api.Issue {
	variant := substrate.Variant(provider)
	segments := substrate.Segments(c, provider)
	if substrate.VirtualArm(variant) || segments.Len() < 2 || !network.Declared(c) {
		return nil
	}
	var names []string
	for _, segment := range segments.Items() {
		names = append(names, segment.Get("name").Text())
	}
	matches, matchIssues := resolveMatches(o, c, native)
	if len(matchIssues) != 0 {
		return nil
	}
	var issues []api.Issue
	for _, match := range matches.Items() {
		nic := match.Get("nicRef").Text()
		if !slices.Contains(substrate.MachineNICs(o), nic) || substrate.NICSegment(c, provider, o, nic) != "" {
			continue
		}
		field := "$.spec.substrate." + variant + ".nics"
		for i, entry := range substrate.MachineNICSegments(o).Items() {
			if entry.Get("nicRef").Text() == nic {
				field = fmt.Sprintf("%s[%d]", field, i)
			}
		}
		issues = appendIssues(issues, invariant(field, "NIC "+nic+" of "+o.Identity()+" carries interface "+match.Get("interface").Text()+", and "+provider.Identity()+" declares segments "+strings.Join(names, ", ")+", so the NIC needs its segment",
			"set segmentRef on the spec.substrate."+variant+".nics entry of NIC "+nic+" on "+o.Identity()+" to one of "+strings.Join(names, ", ")))
	}
	return issues
}

func effectiveMTU(native api.Value, name string, visiting map[string]bool) (int64, string) {
	if visiting[name] {
		return 1500, ""
	}
	visiting[name] = true
	iface, _ := namedValue(native.Get("interfaces"), name)
	if mtu, ok := iface.Get("mtu").Int64(); ok {
		return mtu, name
	}
	if parent := iface.Get("vlan", "base-iface").Text(); parent != "" {
		return effectiveMTU(native, parent, visiting)
	}
	if controller := interfaceController(native, name); controller != "" {
		return effectiveMTU(native, controller, visiting)
	}
	return 1500, ""
}

func interfaceController(native api.Value, name string) string {
	iface, _ := namedValue(native.Get("interfaces"), name)
	if controller := iface.Get("controller").Text(); controller != "" {
		return controller
	}
	for _, candidate := range native.Get("interfaces").Items() {
		if slices.Contains(candidate.Get("link-aggregation", "port").Strings(), name) {
			return candidate.Get("name").Text()
		}
	}
	return ""
}

func validateCapacities(c api.Catalog, o, provider api.Object, native api.Value) []api.Issue {
	var issues []api.Issue
	for _, iface := range native.Get("interfaces").Items() {
		name := iface.Get("name").Text()
		if unavailableInterface(iface) {
			continue
		}
		mtu, source := effectiveMTU(native, name, map[string]bool{})
		if source != "" && source != name {
			continue
		}
		seen := map[string]bool{}
		for _, leaf := range stackLeaves(native, name, false, map[string]bool{}) {
			nic := substrate.InterfaceNIC(o, leaf.name)
			reference := substrate.NICSegment(c, provider, o, nic)
			segment, found := substrate.NamedSegment(c, provider, reference)
			if !slices.Contains(substrate.MachineNICs(o), nic) || !found || seen[reference] {
				continue
			}
			seen[reference] = true
			if capacity := substrate.SegmentMTU(segment); mtu > capacity {
				issues = appendIssues(issues, invariant("$.spec.network", fmt.Sprintf("interface %s of %s has MTU %d above capacity %d of %s, which its NIC %s is on", name, o.Identity(), mtu, capacity, substrate.SegmentLabel(c, provider, reference), nic),
					"lower the interface mtu in the network configuration of "+o.Identity()+", or raise "+substrate.CapacityField(c, provider, reference)))
			}
		}
	}
	if !network.Declared(c) {
		return issues
	}
	for _, entry := range o.Spec().Get("network", "addresses").Items() {
		prefix, err := netip.ParsePrefix(entry.Get("address").Text())
		name := entry.Get("interface").Text()
		iface, declared := namedValue(native.Get("interfaces"), name)
		if err != nil || !declared || unavailableInterface(iface) {
			continue
		}
		match, _, _ := assignmentNetwork(o, c, prefix.Addr())
		if match.Network.Name() == "" {
			continue
		}
		capacity, ok := match.Network.Spec().Get("mtu").Int64()
		if !ok {
			capacity = 1500
		}
		if mtu, _ := effectiveMTU(native, name, map[string]bool{}); mtu > capacity {
			issues = appendIssues(issues, invariant("$.spec.network", fmt.Sprintf("interface %s of %s has MTU %d above capacity %d of %s, on which it holds address %s", name, o.Identity(), mtu, capacity, match.Network.Identity(), prefix.Addr()),
				"lower the interface mtu in the network configuration of "+o.Identity()+", or raise spec.mtu of "+match.Network.Identity()))
		}
	}
	return issues
}

type stackLeaf struct {
	name   string
	tagged bool
}

func stackLeaves(native api.Value, name string, tagged bool, visiting map[string]bool) []stackLeaf {
	if visiting[name] {
		return nil
	}
	visiting[name] = true
	defer delete(visiting, name)
	iface, _ := namedValue(native.Get("interfaces"), name)
	if parent := iface.Get("vlan", "base-iface").Text(); parent != "" {
		return stackLeaves(native, parent, true, visiting)
	}
	ports := bondPorts(native, name)
	for _, port := range iface.Get("bridge", "port").Items() {
		if member := port.Get("name").Text(); member != "" && !slices.Contains(ports, member) {
			ports = append(ports, member)
		}
	}
	if len(ports) == 0 {
		return []stackLeaf{{name: name, tagged: tagged}}
	}
	var leaves []stackLeaf
	for _, port := range ports {
		leaves = append(leaves, stackLeaves(native, port, tagged, visiting)...)
	}
	return leaves
}

func validateCarriage(o api.Object, c api.Catalog, native api.Value, owner api.Object, iface, path string) []api.Issue {
	provider, found := Provider(o, c)
	if !found {
		return nil
	}
	type placed struct {
		stackLeaf
		nic, reference string
		segment        api.Value
	}
	var leaves []placed
	references := map[string]bool{}
	for _, leaf := range stackLeaves(native, iface, false, map[string]bool{}) {
		nic := substrate.InterfaceNIC(o, leaf.name)
		reference := substrate.NICSegment(c, provider, o, nic)
		if segment, ok := substrate.NamedSegment(c, provider, reference); ok && slices.Contains(substrate.MachineNICs(o), nic) {
			leaves = append(leaves, placed{leaf, nic, reference, segment})
			references[reference] = true
		}
	}
	if len(references) > 1 {
		return nil
	}
	var issues []api.Issue
	seen := map[string]bool{}
	for _, leaf := range leaves {
		nic, reference, segment := leaf.nic, leaf.reference, leaf.segment
		if key := reference + "/" + strconv.FormatBool(leaf.tagged); !seen[key] {
			seen[key] = true
		} else {
			continue
		}
		label := substrate.SegmentLabel(c, provider, reference)
		move := ", or move NIC " + nic + " with its segmentRef on " + o.Identity() + " to a segment that carries it"
		switch untagged := substrate.SegmentUntagged(segment); {
		case leaf.tagged && !substrate.SegmentCarriesTagged(segment, owner.Name()):
			issues = appendIssues(issues, invariant(path, "interface "+iface+" of "+o.Identity()+" reaches "+owner.Identity()+" through a VLAN on NIC "+nic+", and "+label+" does not carry it tagged",
				"add "+owner.Name()+" to "+substrate.CarriageField(c, provider, reference, true)+move))
		case !leaf.tagged && untagged != owner.Name():
			held := "carries no Network untagged"
			if untagged != "" {
				held = "carries Network/" + untagged + " untagged"
			}
			remedy := "move NIC " + nic + " with its segmentRef on " + o.Identity() + " to a segment that carries " + owner.Identity() + " untagged"
			if field := substrate.CarriageField(c, provider, reference, false); field != "" {
				remedy = "set " + field + " to " + owner.Name() + move
			}
			issues = appendIssues(issues, invariant(path, "interface "+iface+" of "+o.Identity()+" reaches "+owner.Identity()+" untagged on NIC "+nic+", and "+label+" "+held, remedy))
		}
	}
	return issues
}

func validateStackedVLANs(o api.Object, c api.Catalog, native api.Value) []api.Issue {
	provider, found := Provider(o, c)
	if !found {
		return nil
	}
	var issues []api.Issue
	for _, iface := range native.Get("interfaces").Items() {
		id, ok := iface.Get("vlan", "id").Int64()
		base := iface.Get("vlan", "base-iface").Text()
		if !ok || base == "" || unavailableInterface(iface) {
			continue
		}
		name := iface.Get("name").Text()
		seen := map[string]bool{}
		for _, leaf := range stackLeaves(native, base, true, map[string]bool{}) {
			nic := substrate.InterfaceNIC(o, leaf.name)
			reference := substrate.NICSegment(c, provider, o, nic)
			segment, ok := substrate.NamedSegment(c, provider, reference)
			if !slices.Contains(substrate.MachineNICs(o), nic) || !ok || seen[nic] || carriesVLAN(c, segment, int(id)) {
				continue
			}
			seen[nic] = true
			remedy := "add the Network of VLAN " + strconv.FormatInt(id, 10)
			for _, candidate := range c.OfKind(api.Network) {
				if vlan, tagged := network.VLAN(candidate); tagged && int64(vlan) == id && network.Serves(candidate, o.Spec().Get("placement", "site").Text()) {
					remedy = "add " + candidate.Name()
				}
			}
			issues = appendIssues(issues, invariant(configurationPath(o), "VLAN interface "+name+" of "+o.Identity()+" tags VLAN "+strconv.FormatInt(id, 10)+" on NIC "+nic+", and "+substrate.SegmentLabel(c, provider, reference)+" carries no tagged Network with that vlan",
				remedy+" to "+substrate.CarriageField(c, provider, reference, true)+", move NIC "+nic+" with its segmentRef on "+o.Identity()+" to a segment that carries it, or correct vlan.id of "+name))
		}
	}
	return issues
}

func carriesVLAN(c api.Catalog, segment api.Value, id int) bool {
	for _, name := range substrate.SegmentTagged(segment) {
		if tagged, found := c.Find(api.Network, name); found {
			if vlan, ok := network.VLAN(tagged); ok && vlan == id {
				return true
			}
		}
	}
	return false
}

func namedValueBy(values api.Value, key, name string) (api.Value, bool) {
	for _, value := range values.Items() {
		if value.Get(key).Text() == name {
			return value, true
		}
	}
	return api.Value{}, false
}
