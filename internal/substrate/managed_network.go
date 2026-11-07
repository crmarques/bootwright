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
	if !arm.Present() {
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
		issues = add(issues, api.Issue{Code: "api.invariant", Field: path + ".providerRef",
			Message:     "the libvirt arm of " + o.Identity() + " names " + provider.Identity() + ", which is not a libvirt provider, so no libvirt host defines the network",
			Remediation: "set spec.libvirt.providerRef on " + o.Identity() + " to an InfraProvider with a spec.libvirt arm"})
	}
	for _, family := range []string{"ipv4", "ipv6"} {
		if prefix, ok := network.Prefix(o, family); ok && IsManagedNetwork(o) && prefix.IsSingleIP() {
			issues = add(issues, api.Issue{Code: "api.invariant", Field: "$.spec." + family + ".cidr",
				Message:     fmt.Sprintf("managed %s spans the single address %s, which the host's own address on its bridge takes, so no guest has an address on it", o.Identity(), prefix),
				Remediation: fmt.Sprintf("widen spec.%s.cidr of %s to a subnet that holds guest addresses beside the host's", family, o.Identity())})
		}
	}
	if !arm.Has("hostAddress") {
		issues = add(issues, api.Issue{Code: "api.invariant", Field: path + ".hostAddress",
			Message:     o.Identity() + " declares no gateway, so the libvirt host's own address on the bridge has nothing to default to",
			Remediation: "set spec.libvirt.hostAddress on " + o.Identity() + " to the host's address inside the network, or declare the gateway it defaults to"})
	} else if address, err := netip.ParseAddr(arm.Get("hostAddress").Text()); err == nil {
		prefix, declared := network.Prefix(o, network.Family(address))
		reason := "is of an address family " + o.Identity() + " declares no subnet for"
		if declared {
			reason = network.ReservedAddress(prefix, address)
		}
		if reason != "" {
			issues = add(issues, api.Issue{Code: "api.invariant", Field: path + ".hostAddress",
				Message:     "host address " + address.String() + " of " + o.Identity() + " " + reason,
				Remediation: "set spec.libvirt.hostAddress on " + o.Identity() + " to a host address inside the network's cidr"})
		}
	}
	issues = add(issues, validateSegmentName(o, c)...)
	issues = add(issues, validateSharedHostNetworks(o, c)...)
	for i, name := range arm.Get("taggedNetworkRefs").Strings() {
		field := fmt.Sprintf("%s.taggedNetworkRefs[%d]", path, i)
		tagged, found := c.Find(api.Network, name)
		switch _, vlan := network.VLAN(tagged); {
		case name == o.Name():
			issues = add(issues, api.Issue{Code: "api.invariant", Field: field, Message: o.Identity() + " is carried untagged on its own bridge, so it cannot also be one of its tagged networks",
				Remediation: "remove " + name + " from spec.libvirt.taggedNetworkRefs of " + o.Identity()})
		case found && !vlan:
			issues = add(issues, api.Issue{Code: "api.invariant", Field: field, Message: tagged.Identity() + " declares no vlan, so " + o.Identity() + " cannot carry it tagged",
				Remediation: "set spec.vlan on " + tagged.Identity() + ", or remove it from spec.libvirt.taggedNetworkRefs of " + o.Identity()})
		}
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
	prefix   netip.Prefix
}

func (s hostSegment) label() string {
	if s.owner.Kind() == api.Network {
		return s.owner.Identity() + " of " + s.provider.Identity()
	}
	return fmt.Sprintf("spec.segments[%d] of %s", s.index, s.provider.Identity())
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
			if address, ok := HostAddress(o); ok {
				segment.prefix = address.Masked()
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
		if own.bridge != "" && own.bridge == peer.bridge {
			issues = add(issues, api.Issue{Code: "api.invariant", Field: "$.spec.libvirt.bridge",
				Message:     fmt.Sprintf("bridge %s of %s is also named by %s on host Machine/%s; a bridge a managed Network defines belongs to that Network alone", own.bridge, own.label(), peer.label(), host),
				Remediation: fmt.Sprintf("give %s or %s its own bridge", o.Identity(), peer.label())})
		}
		if peer.owner.Kind() == api.Network && own.prefix.IsValid() && peer.prefix.IsValid() && own.prefix.Overlaps(peer.prefix) && !network.Overlapping(o, peer.owner, network.Family(own.prefix.Addr())) {
			issues = add(issues, api.Issue{Code: "api.invariant", Field: "$.spec.libvirt.hostAddress",
				Message:     fmt.Sprintf("managed prefix %s of %s overlaps managed prefix %s of %s on host Machine/%s, and the host routes each managed prefix to its own bridge", own.prefix, own.label(), peer.prefix, peer.label(), host),
				Remediation: fmt.Sprintf("give %s or %s a subnet no other managed Network on host Machine/%s overlaps", o.Identity(), peer.owner.Identity(), host)})
		}
	}
	names := make([]string, 0, len(managed))
	for _, segment := range managed {
		names = append(names, segment.owner.Identity())
	}
	slices.Sort(names)
	if len(names) > MaxManagedNetworks && names[MaxManagedNetworks] == o.Identity() {
		over := names[MaxManagedNetworks:]
		issues = add(issues, api.Issue{Code: "api.value", Field: "$.spec.libvirt.providerRef",
			Message:     fmt.Sprintf("one libvirt host defines at most %d managed Networks across its providers, and %d name providers on host Machine/%s; over the bound: %s", MaxManagedNetworks, len(managed), host, strings.Join(over, ", ")),
			Remediation: fmt.Sprintf("make %s provided networks on existing bridges, or name a provider on another host in their spec.libvirt.providerRef", strings.Join(over, ", "))})
	}
	return issues
}
