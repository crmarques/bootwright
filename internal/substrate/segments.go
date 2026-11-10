package substrate

import (
	"fmt"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/network"
)

func SegmentUntagged(segment api.Value) string { return segment.Get("untaggedNetworkRef").Text() }

func SegmentTagged(segment api.Value) []string { return segment.Get("taggedNetworkRefs").Strings() }

func SegmentCarriesTagged(segment api.Value, name string) bool {
	return slices.Contains(SegmentTagged(segment), name)
}

func NamedSegment(c api.Catalog, provider api.Object, name string) (api.Value, bool) {
	if name == "" {
		return api.Value{}, false
	}
	return findNamed(Segments(c, provider), "name", name)
}

func SegmentLabel(c api.Catalog, provider api.Object, name string) string {
	for index, segment := range provider.Spec().Get("segments").Items() {
		if segment.Get("name").Text() == name {
			return fmt.Sprintf("segment %s (spec.segments[%d] of %s)", name, index, provider.Identity())
		}
	}
	if o, found := c.Find(api.Network, name); found && IsManagedNetwork(o) {
		if _, leg := TaggedManagedNetwork(o); leg {
			return "segment " + name + " (tagged managed " + o.Identity() + " on trunk " + o.Spec().Get(ArmLibvirt, "bridge").Text() + " of " + provider.Identity() + ")"
		}
		return "segment " + name + " (managed " + o.Identity() + " of " + provider.Identity() + ")"
	}
	return "segment " + name + " of " + provider.Identity()
}

func validateCarriage(o api.Object, c api.Catalog, segment api.Value, variant string, index int) []api.Issue {
	var issues []api.Issue
	entry := fmt.Sprintf("spec.segments[%d]", index)
	path := "$." + entry
	name := segment.Get("name").Text()
	untagged := SegmentUntagged(segment)
	fabric := !TaggedCarriage(variant)
	if fabric && untagged == "" {
		issues = add(issues, api.Issue{Code: "api.required", Field: path + ".untaggedNetworkRef",
			Message:     "a " + variant + " segment delivers exactly one network to each NIC on it, and segment " + name + " of " + o.Identity() + " names none",
			Remediation: "set " + entry + ".untaggedNetworkRef on " + o.Identity() + " to the Network its " + variant + " network carries"})
	}
	for i, tagged := range SegmentTagged(segment) {
		field := fmt.Sprintf("%s.taggedNetworkRefs[%d]", path, i)
		if fabric {
			issues = add(issues, api.Issue{Code: "api.invariant", Field: field,
				Message:     "a " + variant + " segment carries its one network untagged, and tagged carriage over a " + variant + " network is not yet admitted (B1172), so segment " + name + " of " + o.Identity() + " cannot carry Network/" + tagged + " tagged",
				Remediation: "remove taggedNetworkRefs from " + entry + " of " + o.Identity() + ", and give Network/" + tagged + " a segment of its own with untaggedNetworkRef"})
			continue
		}
		owner, found := c.Find(api.Network, tagged)
		_, vlan := network.VLAN(owner)
		switch {
		case tagged == untagged:
			issues = add(issues, api.Issue{Code: "api.invariant", Field: field,
				Message:     "segment " + name + " of " + o.Identity() + " carries Network/" + tagged + " untagged, so it cannot also carry it tagged",
				Remediation: "remove Network/" + tagged + " from " + entry + ".taggedNetworkRefs or from untaggedNetworkRef on " + o.Identity()})
		case found && !vlan:
			issues = add(issues, api.Issue{Code: "api.invariant", Field: field,
				Message:     owner.Identity() + " declares no vlan, so segment " + name + " of " + o.Identity() + " cannot carry it tagged",
				Remediation: "set spec.vlan on " + owner.Identity() + ", or remove it from " + entry + ".taggedNetworkRefs of " + o.Identity()})
		}
	}
	return issues
}

func validateHostedSegmentMTUs(o api.Object, c api.Catalog) []api.Issue {
	if Variant(o) != ArmKubeVirt {
		return nil
	}
	host, hosted := c.Find(api.OpenShiftCluster, HostCluster(o))
	if !hosted {
		return nil
	}
	var issues []api.Issue
	for index, segment := range o.Spec().Get("segments").Items() {
		ref := segment.Get(ArmKubeVirt, "networkRef")
		if !segment.Has("mtu") || !ref.Present() {
			continue
		}
		resolved := segment.WithPath(defaultedNetworkRef(ref, o.Spec().Get(ArmKubeVirt, "namespace")), ArmKubeVirt, "networkRef")
		if name, _, ok := HostNetworkMTU(host, resolved); ok {
			issues = add(issues, api.Issue{Code: "api.invariant", Field: fmt.Sprintf("$.spec.segments[%d].mtu", index),
				Message:     "segment " + segment.Get("name").Text() + " of " + o.Identity() + " is secondary network " + name + " of " + host.Identity() + ", whose mtu is the segment's capacity, so the segment declares none of its own",
				Remediation: fmt.Sprintf("remove spec.segments[%d].mtu from %s, and set mtu on secondary network %s of %s", index, o.Identity(), name, host.Identity())})
		}
	}
	return issues
}

func CapacityField(c api.Catalog, provider api.Object, name string) string {
	for index, segment := range provider.Spec().Get("segments").Items() {
		if segment.Get("name").Text() != name {
			continue
		}
		if host, hosted := HostClusterObject(c, provider); hosted {
			if network, _, ok := HostNetworkMTU(host, segment); ok {
				return "mtu of secondary network " + network + " of " + host.Identity()
			}
		}
		return fmt.Sprintf("spec.segments[%d].mtu of %s", index, provider.Identity())
	}
	return "spec.mtu of Network/" + name
}

func TaggedCarriage(arm string) bool { return arm != ArmKubeVirt && arm != ArmVSphere }

func CarriageField(c api.Catalog, provider api.Object, name string, tagged bool) string {
	for index, segment := range provider.Spec().Get("segments").Items() {
		if segment.Get("name").Text() != name {
			continue
		}
		if tagged {
			return fmt.Sprintf("spec.segments[%d].taggedNetworkRefs of %s", index, provider.Identity())
		}
		return fmt.Sprintf("spec.segments[%d].untaggedNetworkRef of %s", index, provider.Identity())
	}
	if tagged {
		return "spec.libvirt.taggedNetworkRefs of Network/" + name
	}
	return ""
}
