package openshift

import (
	"net/netip"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func withMachineNetworkRefs(cluster api.Object, c api.Catalog, refs ...string) (api.Object, api.Catalog) {
	objects := append(c.Objects(), obj(api.Network, "routed", m("provided", true, "ipv4", m("cidr", "203.0.113.0/24"))), obj(api.Network, "v6-only", m("provided", true, "ipv6", m("cidr", "2001:db8:9::/64"))))
	return cluster.WithSpec(cluster.Spec().WithPath(api.StringList(refs...), "networking", "machineNetworkRefs")), api.NewCatalog(objects)
}

func TestMachineNetworksDefaultToTheInstallNetworks(t *testing.T) {
	cluster, c := fixture("libvirt", 3, false)
	if got := MachineNetworks(cluster, c); !slices.Equal(got, []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}) {
		t.Fatal(got)
	}
	cluster, c = withMachineNetworkRefs(cluster, c, "routed", "lan")
	if got := MachineNetworks(cluster, c); !slices.Equal(got, []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("192.0.2.0/24")}) {
		t.Fatal(got)
	}
	if issues := validateMachineNetworkRefs(cluster, c); len(issues) != 0 {
		t.Fatal(issues)
	}
}

func TestAuthoredMachineNetworksKeepEveryInstallNetwork(t *testing.T) {
	cluster, c := fixture("libvirt", 3, false)
	cluster, c = withMachineNetworkRefs(cluster, c, "routed")
	issues := validateMachineNetworkRefs(cluster, c)
	if len(issues) != 1 || issues[0].Field != "$.spec.networking.machineNetworkRefs" || !strings.Contains(issues[0].Message, "omits Network/lan") || !strings.Contains(issues[0].Message, "node node-0") || !strings.Contains(issues[0].Remediation, "add lan") {
		t.Fatalf("%+v", issues)
	}
	cluster, c = fixture("libvirt", 3, false)
	cluster, c = withMachineNetworkRefs(cluster, c, "lan", "v6-only")
	issues = validateMachineNetworkRefs(cluster, c)
	if len(issues) != 1 || issues[0].Field != "$.spec.networking.machineNetworkRefs[1]" || !strings.Contains(issues[0].Message, "no ipv4 cidr") {
		t.Fatalf("%+v", issues)
	}
}

func TestAuthoredMachineNetworksContainEndpoints(t *testing.T) {
	cluster, c := fixture("libvirt", 3, false)
	cluster = cluster.WithSpec(cluster.Spec().WithPath(api.StringValue("203.0.113.2"), "install", "endpoints", "api", "address"))
	refused := func(o api.Object, c api.Catalog) bool {
		return slices.ContainsFunc(validateEndpoints(o, c), func(issue api.Issue) bool {
			return issue.Field == "$.spec.install.endpoints.api.address" && strings.Contains(issue.Message, "consumed machine network") && strings.Contains(issue.Remediation, "or add the Network that holds it to spec.networking.machineNetworkRefs")
		})
	}
	if !refused(cluster, c) {
		t.Fatal("an endpoint outside the install Network was admitted")
	}
	cluster, c = withMachineNetworkRefs(cluster, c, "lan", "routed")
	if refused(cluster, c) {
		t.Fatal("an endpoint inside an authored machine network was refused")
	}
}
