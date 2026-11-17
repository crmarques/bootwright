package substrate

import (
	"fmt"
	"net/netip"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/network"
)

func normalizeNetworkArm(o api.Object) api.Object {
	arm := o.Spec().Get(ArmLibvirt)
	if !arm.Present() || HostAddressingDisabled(o) {
		return o
	}
	for _, family := range []string{"ipv4", "ipv6"} {
		if gateway, ok := network.Gateway(o, family); ok {
			arm = arm.Default("hostAddress", api.StringValue(gateway.String()))
		}
	}
	if address, err := netip.ParseAddr(arm.Get("hostAddress").Text()); err == nil {
		arm = arm.With("hostAddress", api.StringValue(address.String()))
	}
	return o.WithSpec(o.Spec().With(ArmLibvirt, arm))
}

func validateNetworkArm(o api.Object, c api.Catalog) []api.Issue {
	arm := o.Spec().Get(ArmLibvirt)
	if !arm.Present() || o.Spec().Get("provided").Bool() {
		return nil
	}
	const path = "$.spec.libvirt"
	var issues []api.Issue
	if provider, found := c.Find(api.InfraProvider, arm.Get("providerRef").Text()); found && Variant(provider) != ArmLibvirt {
		issues = api.AppendIssues(issues, api.Issue{Code: "api.invariant", Field: path + ".providerRef",
			Message:     "the libvirt arm of " + o.Identity() + " names " + provider.Identity() + ", which is not a libvirt provider, so no libvirt host defines the network",
			Remediation: "set spec.libvirt.providerRef on " + o.Identity() + " to an InfraProvider with a spec.libvirt arm"})
	}
	for _, family := range []string{"ipv4", "ipv6"} {
		if prefix, ok := network.Prefix(o, family); ok && IsManagedNetwork(o) && prefix.IsSingleIP() {
			issues = api.AppendIssues(issues, api.Issue{Code: "api.invariant", Field: "$.spec." + family + ".cidr",
				Message:     fmt.Sprintf("managed %s spans the single address %s, which the host's own address on its bridge takes, so no guest has an address on it", o.Identity(), prefix),
				Remediation: fmt.Sprintf("widen spec.%s.cidr of %s to a subnet that holds guest addresses beside the host's", family, o.Identity())})
		}
	}
	issues = api.AppendIssues(issues, validateHostNetworkAddress(o)...)
	issues = api.AppendIssues(issues, validateSegmentName(o, c)...)
	issues = api.AppendIssues(issues, validateSharedHostNetworks(o, c)...)
	_, leg := TaggedManagedNetwork(o)
	carried := "carried untagged on its own bridge"
	if leg {
		carried = "carried tagged as its own leg on its trunk"
	}
	for i, name := range arm.Get("taggedNetworkRefs").Strings() {
		field := fmt.Sprintf("%s.taggedNetworkRefs[%d]", path, i)
		tagged, found := c.Find(api.Network, name)
		switch _, vlan := network.VLAN(tagged); {
		case name == o.Name():
			issues = api.AppendIssues(issues, api.Issue{Code: "api.invariant", Field: field, Message: o.Identity() + " is " + carried + ", so it cannot also be one of its tagged networks",
				Remediation: "remove " + name + " from spec.libvirt.taggedNetworkRefs of " + o.Identity()})
		case found && !vlan:
			issues = api.AppendIssues(issues, api.Issue{Code: "api.invariant", Field: field, Message: tagged.Identity() + " declares no vlan, so " + o.Identity() + " cannot carry it tagged",
				Remediation: "set spec.vlan on " + tagged.Identity() + ", or remove it from spec.libvirt.taggedNetworkRefs of " + o.Identity()})
		case found && leg && networkMTU(tagged) != defaultMTU:
			issues = api.AppendIssues(issues, api.Issue{Code: "api.invariant", Field: field,
				Message:     fmt.Sprintf("the trunk of tagged managed %s carries %s, whose mtu is %d, and an MTU other than %d on a trunk or leg is not yet admitted (B1285)", o.Identity(), tagged.Identity(), networkMTU(tagged), defaultMTU),
				Remediation: fmt.Sprintf("set spec.mtu of %s to %d, or remove it from spec.libvirt.taggedNetworkRefs of %s", tagged.Identity(), defaultMTU, o.Identity())})
		}
	}
	issues = api.AppendIssues(issues, validateLeg(o)...)
	issues = api.AppendIssues(issues, validateLegConnectionName(o, c)...)
	return api.AppendIssues(issues, validateRoutedNetworks(o, c)...)
}

func validateLegConnectionName(o api.Object, c api.Catalog) []api.Issue {
	if _, leg := TaggedManagedNetwork(o); !leg {
		return nil
	}
	for _, peer := range c.OfKind(api.Network) {
		if _, tagged := TaggedManagedNetwork(peer); !tagged {
			continue
		}
		provider, bridge := ManagedNetworkProvider(peer), peer.Spec().Get(ArmLibvirt, "bridge").Text()
		if trunk := provider + "-trunk-" + bridge; o.Name() == trunk {
			return []api.Issue{{Code: "api.invariant", Field: "$.metadata.name",
				Message:     "the VLAN leg connection of tagged managed " + o.Identity() + " would take the name and UUID of the NetworkManager connection of trunk " + bridge + " of InfraProvider/" + provider + ", which is named after " + trunk,
				Remediation: "rename " + o.Identity() + " so it is not " + trunk}}
		}
	}
	return nil
}

func validateRoutedNetworks(o api.Object, c api.Catalog) []api.Issue {
	const path = "$.spec.libvirt.routedNetworkRefs"
	refs := o.Spec().Get(ArmLibvirt, "routedNetworkRefs").Strings()
	if len(refs) == 0 {
		return nil
	}
	if _, leg := TaggedManagedNetwork(o); !leg {
		return []api.Issue{{Code: "api.invariant", Field: path,
			Message:     "untagged managed " + o.Identity() + " is a libvirt network whose own firewall rules refuse forwarding to any other network, and routing to an untagged libvirt network is not yet admitted (B1287)",
			Remediation: "remove spec.libvirt.routedNetworkRefs from " + o.Identity() + ", or set spec.vlan on it to realize it as a host VLAN leg"}}
	}
	var issues []api.Issue
	for i, name := range refs {
		field := fmt.Sprintf("%s[%d]", path, i)
		peer, found := c.Find(api.Network, name)
		_, tagged := TaggedManagedNetwork(peer)
		switch {
		case found && (HostAddressingDisabled(o) || HostAddressingDisabled(peer)):
			issues = api.AppendIssues(issues, api.Issue{Code: "api.invariant", Field: field,
				Message:     o.Identity() + " cannot route to or from addressless " + peer.Identity(),
				Remediation: "remove " + name + " from spec.libvirt.routedNetworkRefs of " + o.Identity()})
		case name == o.Name():
			issues = api.AppendIssues(issues, api.Issue{Code: "api.invariant", Field: field, Message: o.Identity() + " cannot be routed to itself",
				Remediation: "remove " + name + " from spec.libvirt.routedNetworkRefs of " + o.Identity()})
		case found && (!tagged || ManagedNetworkProvider(peer) != ManagedNetworkProvider(o)):
			issues = api.AppendIssues(issues, api.Issue{Code: "api.invariant", Field: field,
				Message:     o.Identity() + " routes only to tagged managed Networks of its own provider InfraProvider/" + ManagedNetworkProvider(o) + ", and " + peer.Identity() + " is not one; routing to an untagged libvirt network or across providers is not yet admitted (B1287)",
				Remediation: "remove " + name + " from spec.libvirt.routedNetworkRefs of " + o.Identity() + ", or make " + peer.Identity() + " a managed Network with spec.vlan naming InfraProvider/" + ManagedNetworkProvider(o)})
		}
	}
	return issues
}

func TaggedManagedNetwork(o api.Object) (int, bool) {
	if !IsManagedNetwork(o) {
		return 0, false
	}
	return network.VLAN(o)
}

func networkMTU(o api.Object) int64 {
	if mtu, ok := o.Spec().Get("mtu").Int64(); ok {
		return mtu
	}
	return defaultMTU
}

func validateLeg(o api.Object) []api.Issue {
	vlan, leg := TaggedManagedNetwork(o)
	if !leg {
		return nil
	}
	var issues []api.Issue
	if o.Spec().Has("ipv6") {
		issues = api.AppendIssues(issues, api.Issue{Code: "api.invariant", Field: "$.spec.ipv6",
			Message:     "tagged managed " + o.Identity() + " is a host VLAN leg holding one IPv4 address, and IPv6 on a leg is not yet admitted (B1284)",
			Remediation: "remove spec.ipv6 from " + o.Identity() + ", or remove spec.vlan to realize it as an untagged libvirt network"})
	}
	if mtu := networkMTU(o); mtu != defaultMTU {
		issues = api.AppendIssues(issues, api.Issue{Code: "api.invariant", Field: "$.spec.mtu",
			Message:     fmt.Sprintf("tagged managed %s is a host VLAN leg on a trunk, and an MTU other than %d on a trunk or leg is not yet admitted (B1285), so its mtu %d is refused", o.Identity(), defaultMTU, mtu),
			Remediation: fmt.Sprintf("set spec.mtu of %s to %d, or remove spec.vlan to realize it as an untagged libvirt network", o.Identity(), defaultMTU)})
	}
	bridge := o.Spec().Get(ArmLibvirt, "bridge").Text()
	if strings.Contains(bridge, ".") {
		issues = api.AppendIssues(issues, api.Issue{Code: "api.invariant", Field: "$.spec.libvirt.bridge",
			Message:     "the trunk " + bridge + " of tagged managed " + o.Identity() + " is a bridge Bootwright creates, and a dot in a bridge name reads as the VLAN device of another link",
			Remediation: "name the trunk without a dot in spec.libvirt.bridge of " + o.Identity() + ", such as " + strings.ReplaceAll(bridge, ".", "-")})
	}
	if device := LegDevice(bridge, vlan); api.ValidLexical("bridge", bridge) && len(device) > MaxInterfaceName {
		issues = api.AppendIssues(issues, api.Issue{Code: "api.invariant", Field: "$.spec.libvirt.bridge",
			Message:     fmt.Sprintf("the leg of tagged managed %s is the host VLAN device %s, %d bytes, and a Linux interface name holds at most %d", o.Identity(), device, len(device), MaxInterfaceName),
			Remediation: fmt.Sprintf("shorten spec.libvirt.bridge of %s to at most %d bytes", o.Identity(), MaxInterfaceName-1-len(FormatPort(vlan)))})
	}
	return issues
}

const defaultMTU = 1500

func IsManagedNetwork(o api.Object) bool {
	provided := o.Spec().Get("provided")
	return o.Kind() == api.Network && provided.Present() && !provided.Bool() && o.Spec().Has(ArmLibvirt)
}

func ManagedNetworkProvider(o api.Object) string {
	return o.Spec().Get(ArmLibvirt, "providerRef").Text()
}

func ManagedNetworksOf(c api.Catalog, provider api.Object) []api.Object {
	var found []api.Object
	for _, o := range c.OfKind(api.Network) {
		if IsManagedNetwork(o) && ManagedNetworkProvider(o) == provider.Name() {
			found = append(found, o)
		}
	}
	slices.SortFunc(found, func(x, y api.Object) int { return strings.Compare(x.Name(), y.Name()) })
	return found
}

func HostAddress(o api.Object) (netip.Prefix, bool) {
	if HostAddressingDisabled(o) {
		return netip.Prefix{}, false
	}
	arm := o.Spec().Get(ArmLibvirt)
	address, err := netip.ParseAddr(arm.Get("hostAddress").Text())
	if !arm.Has("hostAddress") {
		for _, family := range []string{"ipv4", "ipv6"} {
			if gateway, ok := network.Gateway(o, family); ok {
				address, err = gateway, nil
				break
			}
		}
	}
	if err != nil {
		return netip.Prefix{}, false
	}
	prefix, ok := network.Prefix(o, network.Family(address))
	if !ok {
		return netip.Prefix{}, false
	}
	return netip.PrefixFrom(address.WithZone(""), prefix.Bits()), true
}

func NetworkSegment(o api.Object) api.Value {
	arm := o.Spec().Get(ArmLibvirt)
	forward := arm.Get("forward")
	if !forward.Present() {
		forward = api.StringValue("nat")
	}
	managed := api.MapValue(api.FieldValue{Name: "forward", Value: forward})
	if address, ok := HostAddress(o); ok {
		managed = managed.With("address", api.StringValue(address.String()))
	}
	mtu := o.Spec().Get("mtu")
	if !mtu.Present() {
		mtu = api.IntegerValue(fmt.Sprint(defaultMTU))
	}
	segment := api.MapValue(api.FieldValue{Name: "name", Value: api.StringValue(o.Name())}, api.FieldValue{Name: "untaggedNetworkRef", Value: api.StringValue(o.Name())})
	if arm.Has("taggedNetworkRefs") {
		segment = segment.With("taggedNetworkRefs", arm.Get("taggedNetworkRefs"))
	}
	if vlan, leg := TaggedManagedNetwork(o); leg {
		segment = api.MapValue(api.FieldValue{Name: "name", Value: api.StringValue(o.Name())},
			api.FieldValue{Name: "taggedNetworkRefs", Value: api.StringList(append([]string{o.Name()}, arm.Get("taggedNetworkRefs").Strings()...)...)})
		managed = managed.With("vlan", api.IntegerValue(FormatPort(vlan)))
		if prefix, ok := network.Prefix(o, "ipv4"); ok {
			managed = managed.With("cidr", api.StringValue(prefix.String()))
		}
		if HostAddressingDisabled(o) {
			managed = managed.With("hostAddressing", api.StringValue("none"))
		}
	}
	return segment.With("mtu", mtu).With(ArmLibvirt, api.MapValue(api.FieldValue{Name: "bridge", Value: arm.Get("bridge")})).With("managed", managed)
}

func Segments(c api.Catalog, provider api.Object) api.Value {
	segments := provider.Spec().Get("segments").Items()
	if Variant(provider) == ArmLibvirt {
		for _, o := range ManagedNetworksOf(c, provider) {
			segments = append(segments, NetworkSegment(o))
		}
	}
	return api.ListValue(segments...)
}

func validateSegmentName(o api.Object, c api.Catalog) []api.Issue {
	provider, found := c.Find(api.InfraProvider, ManagedNetworkProvider(o))
	if !found || !IsManagedNetwork(o) {
		return nil
	}
	for index, segment := range provider.Spec().Get("segments").Items() {
		if segment.Get("name").Text() == o.Name() {
			return []api.Issue{{Code: "api.invariant", Field: "$.spec.libvirt.providerRef",
				Message:     fmt.Sprintf("%s is a segment of %s under its own name, and spec.segments[%d] of %s is also named %s, so a Machine NIC naming it would select two segments", o.Identity(), provider.Identity(), index, provider.Identity(), o.Name()),
				Remediation: "rename spec.segments[" + fmt.Sprint(index) + "] of " + provider.Identity() + " or " + o.Identity() + ", with the segmentRef of every Machine that selects it"}}
		}
	}
	return nil
}

type hostSegment struct {
	owner    api.Object
	provider api.Object
	index    int
	bridge   string
	device   string
	vlan     int
	prefix   netip.Prefix
}

func (s hostSegment) label() string {
	if s.owner.Kind() == api.Network {
		return s.owner.Identity() + " of " + s.provider.Identity()
	}
	return fmt.Sprintf("spec.segments[%d] of %s", s.index, s.provider.Identity())
}

func (s hostSegment) trunk() bool { return s.vlan != 0 }

func (s hostSegment) sharesTrunk(peer hostSegment) bool {
	return s.trunk() && peer.trunk() && peer.owner.Kind() == api.Network && s.provider.Identity() == peer.provider.Identity()
}

func segmentsOnHost(c api.Catalog, host string) (managed, external []hostSegment) {
	for _, provider := range ProvidersOn(c, ArmLibvirt) {
		if HostMachine(provider) != host {
			continue
		}
		for index, segment := range provider.Spec().Get("segments").Items() {
			if bridge := segment.Get(ArmLibvirt, "bridge").Text(); api.ValidLexical("bridge", bridge) {
				external = append(external, hostSegment{owner: provider, provider: provider, index: index, bridge: bridge})
			}
		}
		for _, o := range ManagedNetworksOf(c, provider) {
			segment := hostSegment{owner: o, provider: provider}
			if bridge := o.Spec().Get(ArmLibvirt, "bridge").Text(); api.ValidLexical("bridge", bridge) {
				segment.bridge = bridge
			}
			if vlan, leg := TaggedManagedNetwork(o); leg {
				segment.vlan = vlan
				if segment.bridge != "" {
					segment.device = LegDevice(segment.bridge, vlan)
				}
			}
			if address, ok := HostAddress(o); ok {
				segment.prefix = address.Masked()
			} else if HostAddressingDisabled(o) {
				segment.prefix, _ = network.Prefix(o, "ipv4")
			}
			managed = append(managed, segment)
		}
	}
	return managed, external
}

func validateSharedHostNetworks(o api.Object, c api.Catalog) []api.Issue {
	provider, found := c.Find(api.InfraProvider, ManagedNetworkProvider(o))
	host := HostMachine(provider)
	if !found || !IsManagedNetwork(o) || Variant(provider) != ArmLibvirt || !api.ValidLexical("name", host) {
		return nil
	}
	managed, external := segmentsOnHost(c, host)
	index := slices.IndexFunc(managed, func(s hostSegment) bool { return s.owner.Identity() == o.Identity() })
	if index < 0 {
		return nil
	}
	own := managed[index]
	issues := []api.Issue{}
	for _, peer := range append(slices.Clone(managed), external...) {
		if peer.owner.Identity() == o.Identity() {
			continue
		}
		switch {
		case own.bridge == "" || own.bridge != peer.bridge:
		case own.sharesTrunk(peer) && own.vlan == peer.vlan:
			issues = api.AppendIssues(issues, api.Issue{Code: "api.invariant", Field: "$.spec.vlan",
				Message:     fmt.Sprintf("VLAN %d of %s is also the VLAN of %s on trunk %s on host Machine/%s; the tagged managed Networks of one trunk carry distinct VLANs", own.vlan, own.label(), peer.label(), own.bridge, host),
				Remediation: fmt.Sprintf("give %s or %s another vlan, or its own bridge", o.Identity(), peer.owner.Identity())})
		case own.sharesTrunk(peer):
		case own.trunk() || peer.trunk():
			issues = api.AppendIssues(issues, api.Issue{Code: "api.invariant", Field: "$.spec.libvirt.bridge",
				Message:     fmt.Sprintf("bridge %s of %s is also named by %s on host Machine/%s; a trunk Bootwright creates carries only the tagged managed Networks of one provider", own.bridge, own.label(), peer.label(), host),
				Remediation: fmt.Sprintf("give %s or %s its own bridge", o.Identity(), peer.label())})
		default:
			issues = api.AppendIssues(issues, api.Issue{Code: "api.invariant", Field: "$.spec.libvirt.bridge",
				Message:     fmt.Sprintf("bridge %s of %s is also named by %s on host Machine/%s; a bridge a managed Network defines belongs to that Network alone", own.bridge, own.label(), peer.label(), host),
				Remediation: fmt.Sprintf("give %s or %s its own bridge", o.Identity(), peer.label())})
		}
		if own.device != "" && own.device == peer.bridge {
			issues = api.AppendIssues(issues, api.Issue{Code: "api.invariant", Field: "$.spec.libvirt.bridge",
				Message:     fmt.Sprintf("the leg of %s is the host VLAN device %s, which %s also names on host Machine/%s", own.label(), own.device, peer.label(), host),
				Remediation: fmt.Sprintf("give %s or %s another bridge", o.Identity(), peer.label())})
		}
		if own.bridge != "" && own.bridge == peer.device {
			issues = api.AppendIssues(issues, api.Issue{Code: "api.invariant", Field: "$.spec.libvirt.bridge",
				Message:     fmt.Sprintf("bridge %s of %s is the host VLAN device of the leg of %s on host Machine/%s", own.bridge, own.label(), peer.label(), host),
				Remediation: fmt.Sprintf("give %s or %s another bridge", o.Identity(), peer.label())})
		}
		if peer.owner.Kind() == api.Network && own.prefix.IsValid() && peer.prefix.IsValid() && own.prefix.Overlaps(peer.prefix) && !network.Overlapping(o, peer.owner, network.Family(own.prefix.Addr())) {
			issues = api.AppendIssues(issues, api.Issue{Code: "api.invariant", Field: "$.spec.libvirt.hostAddress",
				Message:     fmt.Sprintf("managed prefix %s of %s overlaps managed prefix %s of %s on host Machine/%s, and the host routes each managed prefix to its own bridge", own.prefix, own.label(), peer.prefix, peer.label(), host),
				Remediation: fmt.Sprintf("give %s or %s a subnet no other managed Network on host Machine/%s overlaps", o.Identity(), peer.owner.Identity(), host)})
		}
	}
	return api.AppendIssues(issues, validateManagedBound(o, managed, host)...)
}

func validateManagedBound(o api.Object, managed []hostSegment, host string) []api.Issue {
	ordered := slices.Clone(managed)
	slices.SortFunc(ordered, func(x, y hostSegment) int { return strings.Compare(x.owner.Identity(), y.owner.Identity()) })
	trunks := map[string]bool{}
	count, first := 0, -1
	for index, segment := range ordered {
		count++
		if segment.trunk() && segment.bridge != "" && !trunks[segment.bridge] {
			trunks[segment.bridge] = true
			count++
		}
		if count > MaxManagedNetworks && first < 0 {
			first = index
		}
	}
	if first < 0 || ordered[first].owner.Identity() != o.Identity() {
		return nil
	}
	var over []string
	for _, segment := range ordered[first:] {
		over = append(over, segment.owner.Identity())
	}
	message := fmt.Sprintf("one libvirt host defines at most %d managed Networks across its providers, and %d name providers on host Machine/%s; over the bound: %s", MaxManagedNetworks, len(managed), host, strings.Join(over, ", "))
	if len(trunks) != 0 {
		message = fmt.Sprintf("one libvirt host defines at most %d managed Networks across its providers, each trunk of tagged ones counting as one more, and %d name providers on host Machine/%s on %d trunks; over the bound: %s", MaxManagedNetworks, len(managed), host, len(trunks), strings.Join(over, ", "))
	}
	return []api.Issue{{Code: "api.value", Field: "$.spec.libvirt.providerRef", Message: message,
		Remediation: fmt.Sprintf("make %s provided networks on existing bridges, or name a provider on another host in their spec.libvirt.providerRef", strings.Join(over, ", "))}}
}
