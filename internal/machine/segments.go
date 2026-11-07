package machine

import (
	"fmt"
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
	if substrate.VirtualArm(variant) || segments.Len() < 2 {
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
		remedy := "add an entry {nicRef: " + nic + ", macAddress: <its MAC>, segmentRef: <segment>} to spec.substrate." + variant + ".nics of " + o.Identity() + ", with segmentRef one of " + strings.Join(names, ", ")
		for i, entry := range substrate.MachineNICSegments(o).Items() {
			if entry.Get("nicRef").Text() == nic {
				field = fmt.Sprintf("%s[%d]", field, i)
				remedy = "set segmentRef on the spec.substrate." + variant + ".nics entry of NIC " + nic + " on " + o.Identity() + " to one of " + strings.Join(names, ", ")
			}
		}
		issues = appendIssues(issues, invariant(field, "NIC "+nic+" of "+o.Identity()+" carries interface "+match.Get("interface").Text()+", and "+provider.Identity()+" declares segments "+strings.Join(names, ", ")+", so the NIC needs its segment", remedy))
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
					mtuRemedy(o, name, source, capacity)+", or raise "+substrate.CapacityField(c, provider, reference)))
			}
		}
	}
	for _, entry := range o.Spec().Get("network", "addresses").Items() {
		name := entry.Get("interface").Text()
		iface, declared := namedValue(native.Get("interfaces"), name)
		match, assigned := AssignedNetwork(o, c, entry)
		if !assigned || !declared || unavailableInterface(iface) {
			continue
		}
		address := entry.Get("address").Text()
		capacity, ok := match.Network.Spec().Get("mtu").Int64()
		if !ok {
			capacity = 1500
		}
		if mtu, source := effectiveMTU(native, name, map[string]bool{}); mtu > capacity {
			issues = appendIssues(issues, invariant("$.spec.network", fmt.Sprintf("interface %s of %s has MTU %d above capacity %d of %s, on which it holds address %s", name, o.Identity(), mtu, capacity, match.Network.Identity(), address),
				mtuRemedy(o, name, source, capacity)+", or raise spec.mtu of "+match.Network.Identity()))
		}
	}
	return issues
}

func mtuRemedy(o api.Object, name, source string, capacity int64) string {
	if source == name {
		return "lower the interface mtu in the network configuration of " + o.Identity()
	}
	return fmt.Sprintf("set mtu on interface %s to at most %d in the network configuration of %s", name, capacity, o.Identity())
}

type stackLeaf struct {
	name   string
	tagged bool
	bond   string
	path   []string
}

func stackLeaves(native api.Value, name string, tagged bool, visiting map[string]bool) []stackLeaf {
	return walkStack(native, name, stackLeaf{tagged: tagged}, visiting)
}

func walkStack(native api.Value, name string, at stackLeaf, visiting map[string]bool) []stackLeaf {
	if visiting[name] {
		return nil
	}
	visiting[name] = true
	defer delete(visiting, name)
	at.path = append(slices.Clone(at.path), name)
	iface, _ := namedValue(native.Get("interfaces"), name)
	if parent := iface.Get("vlan", "base-iface").Text(); parent != "" {
		at.tagged = true
		return walkStack(native, parent, at, visiting)
	}
	if iface.Get("type").Text() == "bond" {
		at.bond = name
	}
	bridge, ports := stackPorts(native, iface, name, visiting)
	if bridge != "" {
		visiting[bridge] = true
		defer delete(visiting, bridge)
		at.path = append(at.path, bridge)
	}
	if len(ports) == 0 {
		at.name = name
		return []stackLeaf{at}
	}
	var leaves []stackLeaf
	for _, port := range ports {
		leaves = append(leaves, walkStack(native, port, at, visiting)...)
	}
	return leaves
}

func stackPorts(native, iface api.Value, name string, visiting map[string]bool) (string, []string) {
	ports := bridgeMembers(native, name)
	if bridge := portOwner(native, name); len(ports) == 0 && iface.Get("type").Text() == "ovs-interface" && bridge != "" && !visiting[bridge] {
		return bridge, slices.DeleteFunc(bridgeMembers(native, bridge), func(port string) bool { return port == name })
	}
	return "", ports
}

func bridgeMembers(native api.Value, name string) []string {
	iface, _ := namedValue(native.Get("interfaces"), name)
	ports := bondPorts(native, name)
	for _, port := range iface.Get("bridge", "port").Items() {
		if member := port.Get("name").Text(); member != "" && !slices.Contains(ports, member) {
			ports = append(ports, member)
		}
	}
	return ports
}

func portOwner(native api.Value, name string) string {
	for _, candidate := range native.Get("interfaces").Items() {
		if owner := candidate.Get("name").Text(); owner != name && slices.Contains(bridgeMembers(native, owner), name) {
			return owner
		}
	}
	return ""
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
	bonds := map[string]map[string]bool{}
	for _, leaf := range stackLeaves(native, iface, false, map[string]bool{}) {
		nic := substrate.InterfaceNIC(o, leaf.name)
		reference := substrate.NICSegment(c, provider, o, nic)
		if segment, ok := substrate.NamedSegment(c, provider, reference); ok && slices.Contains(substrate.MachineNICs(o), nic) {
			leaves = append(leaves, placed{leaf, nic, reference, segment})
			if bonds[leaf.bond] == nil {
				bonds[leaf.bond] = map[string]bool{}
			}
			bonds[leaf.bond][reference] = true
		}
	}
	variant := substrate.Variant(provider)
	fabric := !substrate.TaggedCarriage(variant)
	var issues []api.Issue
	seen := map[string]bool{}
	for _, leaf := range leaves {
		nic, reference, segment := leaf.nic, leaf.reference, leaf.segment
		key := reference + "/" + strconv.FormatBool(leaf.tagged)
		if seen[key] || leaf.bond != "" && len(bonds[leaf.bond]) > 1 {
			continue
		}
		seen[key] = true
		label := substrate.SegmentLabel(c, provider, reference)
		switch untagged := substrate.SegmentUntagged(segment); {
		case leaf.tagged && !substrate.SegmentCarriesTagged(segment, owner.Name()):
			remedy := "add " + owner.Name() + " to " + substrate.CarriageField(c, provider, reference, true) + ", or " + moveNIC(c, provider, o, nic, "carries it")
			if fabric {
				remedy = "remove VLAN interface " + stackVLAN(native, leaf.path) + " from the stack of interface " + iface + " on " + o.Identity() + ", or " + moveNIC(c, provider, o, nic, "has untaggedNetworkRef "+owner.Name())
			}
			issues = appendIssues(issues, invariant(path, "interface "+iface+" of "+o.Identity()+" reaches "+owner.Identity()+" through a VLAN on NIC "+nic+", and "+label+" does not carry it tagged", remedy))
		case !leaf.tagged && untagged != owner.Name():
			held := "carries no Network untagged"
			if untagged != "" {
				held = "carries Network/" + untagged + " untagged"
			}
			remedy := moveNIC(c, provider, o, nic, "carries "+owner.Identity()+" untagged")
			if field := substrate.CarriageField(c, provider, reference, false); field != "" {
				remedy = "set " + field + " to " + owner.Name() + ", or " + moveNIC(c, provider, o, nic, "carries it")
			}
			issues = appendIssues(issues, invariant(path, "interface "+iface+" of "+o.Identity()+" reaches "+owner.Identity()+" untagged on NIC "+nic+", and "+label+" "+held, remedy))
		}
	}
	return issues
}

func stackVLAN(native api.Value, path []string) string {
	for _, member := range path {
		if iface, _ := namedValue(native.Get("interfaces"), member); iface.Get("vlan", "base-iface").Text() != "" {
			return member
		}
	}
	return ""
}

func moveNIC(c api.Catalog, provider, o api.Object, nic, carries string) string {
	variant := substrate.Variant(provider)
	sole := substrate.Segments(c, provider).Len() == 1
	for _, entry := range substrate.MachineNICSegments(o).Items() {
		switch {
		case entry.Get("nicRef").Text() != nic:
		case entry.Has("segmentRef") && !sole:
			return "move NIC " + nic + " with its segmentRef on " + o.Identity() + " to a segment that " + carries
		case !entry.Has("segmentRef") || entry.Has("macAddress"):
			return "set segmentRef on the spec.substrate." + variant + ".nics entry of NIC " + nic + " on " + o.Identity() + " to a segment that " + carries
		}
	}
	entry := "{nicRef: " + nic + ", segmentRef: <segment>}"
	if !substrate.VirtualArm(variant) {
		entry = "{nicRef: " + nic + ", macAddress: <its MAC>, segmentRef: <segment>}"
	}
	return "add an entry " + entry + " to spec.substrate." + variant + ".nics of " + o.Identity() + " naming a segment that " + carries
}

func carriageChecked(o api.Object, c api.Catalog, native api.Value) map[string]bool {
	checked := map[string]bool{}
	for _, entry := range o.Spec().Get("network", "addresses").Items() {
		name := entry.Get("interface").Text()
		iface, declared := namedValue(native.Get("interfaces"), name)
		if !declared || unavailableInterface(iface) {
			continue
		}
		if _, assigned := AssignedNetwork(o, c, entry); !assigned {
			continue
		}
		for _, leaf := range stackLeaves(native, name, false, map[string]bool{}) {
			for _, member := range leaf.path {
				checked[member] = true
			}
		}
	}
	return checked
}

func validateStackedVLANs(o api.Object, c api.Catalog, native api.Value) []api.Issue {
	provider, found := Provider(o, c)
	if !found {
		return nil
	}
	variant := substrate.Variant(provider)
	checked := carriageChecked(o, c, native)
	var issues []api.Issue
	for _, iface := range native.Get("interfaces").Items() {
		id, ok := iface.Get("vlan", "id").Int64()
		base := iface.Get("vlan", "base-iface").Text()
		name := iface.Get("name").Text()
		if !ok || base == "" || unavailableInterface(iface) || checked[name] {
			continue
		}
		seen := map[string]bool{}
		for _, leaf := range stackLeaves(native, base, true, map[string]bool{}) {
			nic := substrate.InterfaceNIC(o, leaf.name)
			reference := substrate.NICSegment(c, provider, o, nic)
			segment, ok := substrate.NamedSegment(c, provider, reference)
			if !slices.Contains(substrate.MachineNICs(o), nic) || !ok || seen[nic] || carriesVLAN(c, segment, int(id)) {
				continue
			}
			seen[nic] = true
			remedy := "remove VLAN interface " + name + " from the network configuration of " + o.Identity() + ", or " + moveNIC(c, provider, o, nic, "carries its Network untagged")
			if substrate.TaggedCarriage(variant) {
				carried := "the Network of VLAN " + strconv.FormatInt(id, 10)
				for _, candidate := range c.OfKind(api.Network) {
					if vlan, tagged := network.VLAN(candidate); tagged && int64(vlan) == id && network.Serves(candidate, o.Spec().Get("placement", "site").Text()) {
						carried = candidate.Name()
					}
				}
				remedy = "add " + carried + " to " + substrate.CarriageField(c, provider, reference, true) + ", " + moveNIC(c, provider, o, nic, "carries it") + ", or correct vlan.id of " + name
			}
			issues = appendIssues(issues, invariant(configurationPath(o), "VLAN interface "+name+" of "+o.Identity()+" tags VLAN "+strconv.FormatInt(id, 10)+" on NIC "+nic+", and "+substrate.SegmentLabel(c, provider, reference)+" carries no tagged Network with that vlan", remedy))
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
