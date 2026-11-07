package substrate

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func managedNetwork(name string, subnet, arm api.Value) api.Object {
	return obj(api.Network, name, m("provided", false, "ipv4", subnet, "libvirt", arm))
}

func networkArmIssues(o api.Object, others ...api.Object) []api.Issue {
	normalized, _ := Normalize(o, api.Catalog{})
	return Validate(normalized, api.NewCatalog(append([]api.Object{normalized}, others...)))
}

func requireArmIssue(t *testing.T, issues []api.Issue, field, text string) {
	t.Helper()
	for _, issue := range issues {
		if issue.Field == field && strings.Contains(issue.Message, text) {
			return
		}
	}
	t.Fatalf("no issue at %s with %q in %+v", field, text, issues)
}

func TestAManagedNetworkHostAddressDefaultsToItsGateway(t *testing.T) {
	lab := obj(api.InfraProvider, "lab", m("libvirt", m("machineRef", "host")))
	tagged := obj(api.Network, "tagged", m("provided", true, "vlan", api.IntegerValue("140"), "ipv4", m("cidr", "203.0.113.0/24")))
	network := managedNetwork("lab", m("cidr", "192.0.2.0/24", "gateway", "192.0.2.1"), m("providerRef", "lab", "bridge", "virbr-lab", "taggedNetworkRefs", list(api.StringValue("tagged"))))
	normalized, _ := Normalize(network, api.Catalog{})
	if got := normalized.Spec().Get("libvirt", "hostAddress").Text(); got != "192.0.2.1" {
		t.Fatalf("hostAddress = %q", got)
	}
	if issues := networkArmIssues(network, lab, tagged); len(issues) != 0 {
		t.Fatalf("issues = %+v", issues)
	}
}

func TestAManagedNetworkArmIsRefusedWhereNoLibvirtHostDefinesIt(t *testing.T) {
	metal := obj(api.InfraProvider, "metal", m("baremetal", m()))
	requireArmIssue(t, networkArmIssues(managedNetwork("lab", m("cidr", "192.0.2.0/24", "gateway", "192.0.2.1"), m("providerRef", "metal", "bridge", "virbr-lab")), metal),
		"$.spec.libvirt.providerRef", "which is not a libvirt provider")
	requireArmIssue(t, networkArmIssues(managedNetwork("lab", m("cidr", "192.0.2.0/24"), m("providerRef", "lab", "bridge", "virbr-lab"))),
		"$.spec.libvirt.hostAddress", "declares no gateway")
	requireArmIssue(t, networkArmIssues(managedNetwork("lab", m("cidr", "192.0.2.0/24"), m("providerRef", "lab", "bridge", "virbr-lab", "hostAddress", "192.0.2.255"))),
		"$.spec.libvirt.hostAddress", "is the broadcast address of 192.0.2.0/24")
	requireArmIssue(t, networkArmIssues(managedNetwork("lab", m("cidr", "192.0.2.0/24"), m("providerRef", "lab", "bridge", "virbr-lab", "hostAddress", "2001:db8::1"))),
		"$.spec.libvirt.hostAddress", "declares no subnet for")
	untagged := obj(api.Network, "untagged", m("provided", true, "ipv4", m("cidr", "203.0.113.0/24")))
	issues := networkArmIssues(managedNetwork("lab", m("cidr", "192.0.2.0/24", "gateway", "192.0.2.1"), m("providerRef", "lab", "bridge", "virbr-lab", "taggedNetworkRefs", list(api.StringValue("untagged"), api.StringValue("lab")))), untagged)
	requireArmIssue(t, issues, "$.spec.libvirt.taggedNetworkRefs[0]", "Network/untagged declares no vlan")
	requireArmIssue(t, issues, "$.spec.libvirt.taggedNetworkRefs[1]", "carried untagged on its own bridge")
}

func TestPlanningRefusesEveryManagedNetwork(t *testing.T) {
	managed := managedNetwork("lab", m("cidr", "192.0.2.0/24", "gateway", "192.0.2.1"), m("providerRef", "lab", "bridge", "virbr-lab"))
	provided := obj(api.Network, "provided", m("provided", true, "ipv4", m("cidr", "198.51.100.0/24")))
	found := Unrealizable(api.NewCatalog([]api.Object{managed, provided}))
	if len(found) != 1 || found[0].Object.Identity() != "Network/lab" || !strings.Contains(found[0].Remediation, "set spec.provided to true on Network/lab") {
		t.Fatalf("unrealized = %+v", found)
	}
}
