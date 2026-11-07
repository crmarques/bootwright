package openshift

import (
	"fmt"
	"net/netip"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/network"
)

type heldSubnet struct {
	prefix netip.Prefix
	holder string
}

func nodeSubnets(o api.Object, c api.Catalog) []heldSubnet {
	held := []heldSubnet{}
	for _, node := range o.Spec().Get("nodes").Items() {
		bound, ok := c.Find(api.Machine, node.Get("machineRef").Text())
		if !ok {
			continue
		}
		for _, assignment := range bound.Spec().Get("network", "addresses").Items() {
			iface := assignment.Get("interface").Text()
			if prefix, err := netip.ParsePrefix(assignment.Get("address").Text()); err == nil && iface != "" {
				held = append(held, heldSubnet{prefix.Masked(), "the subnet of interface " + iface + " on " + bound.Identity()})
			}
		}
	}
	return held
}

func validateOverlayOverlaps(o api.Object, c api.Catalog) []api.Issue {
	held := nodeSubnets(o, c)
	for _, secondary := range o.Spec().Get("networking", "secondaryNetworks").Items() {
		for _, raw := range secondary.Get("ipam", "subnets").Strings() {
			if prefix, err := netip.ParsePrefix(raw); err == nil {
				held = append(held, heldSubnet{prefix.Masked(), "the IPAM subnet of secondary network " + secondary.Get("name").Text()})
			}
		}
	}
	for _, declared := range network.CIDRs(c) {
		held = append(held, heldSubnet{declared.Prefix, "the cidr of " + declared.Network.Identity()})
	}
	var issues []api.Issue
	check := func(field, kind, raw string) {
		overlay, err := netip.ParsePrefix(raw)
		if err != nil {
			return
		}
		reported := map[netip.Prefix]bool{}
		for _, subnet := range held {
			if reported[subnet.prefix] || !overlay.Masked().Overlaps(subnet.prefix) {
				continue
			}
			reported[subnet.prefix] = true
			issues = add(issues, invariantRemediation(field, fmt.Sprintf("the %s %s of %s overlaps %s, %s, so traffic to that subnet would stay inside the cluster", kind, overlay.Masked(), o.Identity(), subnet.prefix, subnet.holder),
				fmt.Sprintf("choose a %s outside every declared Network and every node interface subnet and secondary-network IPAM subnet of %s", kind, o.Identity())))
		}
	}
	for i, entry := range o.Spec().Get("networking", "clusterNetwork").Items() {
		check(fmt.Sprintf("$.spec.networking.clusterNetwork[%d].cidr", i), "cluster network", entry.Get("cidr").Text())
	}
	for i, raw := range o.Spec().Get("networking", "serviceNetwork").Strings() {
		check(fmt.Sprintf("$.spec.networking.serviceNetwork[%d]", i), "service network", raw)
	}
	return issues
}

func validateVIPAdjacency(o api.Object, c api.Catalog) []api.Issue {
	if !installerHoldsVIPs(o) {
		return nil
	}
	nodes := o.Spec().Get("nodes").Items()
	var issues []api.Issue
	for _, slot := range []string{"api", "ingress"} {
		endpoint := o.Spec().Get("install", "endpoints", slot)
		address, err := netip.ParseAddr(endpoint.Get("address").Text())
		if sourceType(endpoint) != "installer" || err != nil {
			continue
		}
		for _, node := range nodes {
			if slot == "api" && node.Get("role").Text() != "controlPlane" {
				continue
			}
			bound, ok := c.Find(api.Machine, node.Get("machineRef").Text())
			if !ok || holdsSubnetOf(bound, address.Unmap()) {
				continue
			}
			issues = add(issues, invariantRemediation("$.spec.install.endpoints."+slot+".address",
				fmt.Sprintf("the %s VIP %s lies in no interface subnet of node %s (%s), which can hold it, and keepalived moves a VIP only among nodes on its layer-2 network", slot, address, node.Get("name").Text(), bound.Identity()),
				fmt.Sprintf("assign %s an interface address whose prefix contains %s, or choose a VIP in a subnet every node that can hold it is addressed in", bound.Identity(), address)))
		}
	}
	return issues
}

func holdsSubnetOf(m api.Object, address netip.Addr) bool {
	assigned := false
	for _, assignment := range m.Spec().Get("network", "addresses").Items() {
		prefix, err := netip.ParsePrefix(assignment.Get("address").Text())
		if err != nil || assignment.Get("interface").Text() == "" {
			continue
		}
		assigned = true
		if prefix.Masked().Contains(address) {
			return true
		}
	}
	return !assigned
}
