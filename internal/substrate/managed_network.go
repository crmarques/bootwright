package substrate

import (
	"fmt"
	"net/netip"

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
