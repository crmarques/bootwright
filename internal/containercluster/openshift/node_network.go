package openshift

import (
	"net/netip"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
)

func NodeNetworkIssues(node api.Object, catalog api.Catalog, native api.Value) []api.Issue {
	issues := []api.Issue{}
	add := func(message, remedy string) {
		issues = append(issues, api.Issue{Code: "api.invariant", Field: "$.spec.network", Message: message, Remediation: remedy})
	}
	if native.Has("ovn") || native.Has("ovs-db") {
		add("agent network configuration of "+node.Identity()+" contains an ovn or ovs-db section", "declare OVN bridges through OpenShiftCluster secondaryNetworks")
	}
	for _, entry := range native.Get("interfaces").Items() {
		name := entry.Get("name").Text()
		if slices.Contains([]string{"ovs-bridge", "ovs-interface", "ovs-port", "linux-bridge"}, entry.Get("type").Text()) {
			add("agent network configuration of "+node.Identity()+" contains an unqualified bridge interface "+name, "declare bridges through OpenShiftCluster secondaryNetworks")
		}
		if reservedOVNInterface(name) {
			add("agent network configuration of "+node.Identity()+" uses reserved interface "+name, "choose an interface name not reserved by OVN-Kubernetes")
		}
	}
	address, errors := machine.InstallAddress(node, catalog)
	if len(errors) == 0 && address.Present() {
		prefix, err := netip.ParsePrefix(address.Get("address").Text())
		if err == nil {
			routes := defaultRouteInterfaces(native, prefix.Addr().BitLen())
			if len(routes) > 1 {
				add("installation address of "+node.Identity()+" has default routes on multiple interfaces", "use one default-route interface for the installation address family")
			} else if len(routes) == 1 && routes[0] != address.Get("interface").Text() {
				add("installation address of "+node.Identity()+" is not on its default-route interface", "assign the installation address to the interface carrying its default route")
			}
		}
	}
	return issues
}
