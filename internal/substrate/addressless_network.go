package substrate

import (
	"net/netip"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/network"
)

func HostAddressingDisabled(o api.Object) bool {
	return o.Spec().Get("libvirt", "hostAddressing").Text() == "none"
}

func validateAddresslessNetwork(o api.Object) []api.Issue {
	if !HostAddressingDisabled(o) {
		return nil
	}
	arm := o.Spec().Get("libvirt")
	var issues []api.Issue
	for _, rule := range []struct {
		invalid bool
		field   string
		message string
		remedy  string
	}{
		{!o.Spec().Has("vlan"), "hostAddressing", "addressless host networking requires a tagged managed Network", "declare spec.vlan or use hostAddressing: static"},
		{arm.Has("hostAddress"), "hostAddress", "an addressless Network cannot assign a host address", "remove spec.libvirt.hostAddress"},
		{arm.Get("forward").Text() != "none", "forward", "an addressless Network cannot enable NAT", "set spec.libvirt.forward: none"},
		{arm.Get("routedNetworkRefs").Len() != 0, "routedNetworkRefs", "an addressless Network cannot enable host routing", "remove spec.libvirt.routedNetworkRefs"},
	} {
		if rule.invalid {
			issues = api.AppendIssues(issues, api.Issue{Code: "api.invariant", Field: "$.spec.libvirt." + rule.field,
				Message: o.Identity() + ": " + rule.message, Remediation: rule.remedy + " on " + o.Identity()})
		}
	}
	return issues
}

func validateHostNetworkAddress(o api.Object) []api.Issue {
	if HostAddressingDisabled(o) {
		return validateAddresslessNetwork(o)
	}
	arm := o.Spec().Get("libvirt")
	const path = "$.spec.libvirt"
	var issues []api.Issue
	if !arm.Has("hostAddress") {
		issues = api.AppendIssues(issues, api.Issue{Code: "api.invariant", Field: path + ".hostAddress",
			Message:     o.Identity() + " declares no gateway, so the libvirt host's own address on the bridge has nothing to default to",
			Remediation: "set spec.libvirt.hostAddress on " + o.Identity() + " to the host's address inside the network, or declare the gateway it defaults to"})
	} else if address, err := netip.ParseAddr(arm.Get("hostAddress").Text()); err == nil {
		prefix, declared := network.Prefix(o, network.Family(address))
		reason := "is of an address family " + o.Identity() + " declares no subnet for"
		if declared {
			reason = network.ReservedAddress(prefix, address)
		}
		if reason != "" {
			issues = api.AppendIssues(issues, api.Issue{Code: "api.invariant", Field: path + ".hostAddress",
				Message:     "host address " + address.String() + " of " + o.Identity() + " " + reason,
				Remediation: "set spec.libvirt.hostAddress on " + o.Identity() + " to a host address inside the network's cidr"})
		}
	}
	return issues
}
