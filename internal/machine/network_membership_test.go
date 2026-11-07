package machine

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func lan(spec api.Value) api.Object {
	return object(api.Network, "lan", spec.Default("provided", api.BoolValue(true)))
}

func membershipIssues(t *testing.T, machine api.Object, extra ...api.Object) []api.Issue {
	t.Helper()
	_, catalog := fixture()
	var objects []api.Object
	for _, o := range catalog.Objects() {
		if o.Kind() != api.Machine {
			objects = append(objects, o)
		}
	}
	for _, o := range extra {
		if found := slicesIndex(objects, o); found >= 0 {
			objects[found] = o
		} else {
			objects = append(objects, o)
		}
	}
	normalized, issues := Normalize(machine, api.NewCatalog(append(objects, machine)))
	if len(issues) > 0 {
		t.Fatal(issues)
	}
	return Validate(normalized, api.NewCatalog(append(objects, normalized)))
}

func slicesIndex(objects []api.Object, o api.Object) int {
	for i, candidate := range objects {
		if candidate.Identity() == o.Identity() {
			return i
		}
	}
	return -1
}

func issueAt(issues []api.Issue, field, text string) bool {
	for _, issue := range issues {
		if issue.Field == field && strings.Contains(issue.Message, text) {
			return true
		}
	}
	return false
}

func withNetwork(machine api.Object, path []string, value api.Value) api.Object {
	return machine.WithSpec(machine.Spec().WithPath(value, path...))
}

func vlanConfig(id string) api.Object {
	return object(api.NetworkConfig, "net", m("nmstate", m("interfaces", list(
		m("name", "eth0", "type", "ethernet"),
		m("name", "br0", "type", "linux-bridge", "bridge", m("port", list(m("name", "eth0.200")))),
		m("name", "eth0.200", "type", "vlan", "vlan", m("base-iface", "eth0", "id", api.IntegerValue(id)))),
		"routes", m("config", list(m("destination", "0.0.0.0/0", "next-hop-interface", "br0"))))))
}

func TestAMachineAssignmentInItsNetworkIsAdmitted(t *testing.T) {
	machine, _ := fixture()
	for _, issue := range membershipIssues(t, machine, lan(m("ipv4", m("cidr", "192.0.2.0/24")))) {
		if strings.HasPrefix(issue.Field, "$.spec.network") {
			t.Errorf("issue = %+v", issue)
		}
	}
}

func TestAnAssignmentOutsideEveryNetworkIsRefused(t *testing.T) {
	machine, _ := fixture()
	if issues := membershipIssues(t, machine, lan(m("ipv4", m("cidr", "198.51.100.0/24")))); !issueAt(issues, "$.spec.network.addresses[0].address", "lies in no declared Network") {
		t.Fatalf("issues = %+v", issues)
	}
}

func TestAnAssignmentTakesThePrefixOfItsNetwork(t *testing.T) {
	machine, catalog := fixture()
	objects := []api.Object{}
	for _, o := range catalog.Objects() {
		if o.Kind() != api.Network {
			objects = append(objects, o)
		}
	}
	narrow := api.NewCatalog(append(objects, lan(m("ipv4", m("cidr", "192.0.2.0/25")))))
	composed, issues := ComposeNetwork(machine, narrow)
	address := composed.Get("interfaces").Items()[0].Get("ipv4", "address").Items()
	if len(issues) != 0 || len(address) != 1 || address[0].Get("ip").Text() != "192.0.2.11" || address[0].Get("prefix-length").Text() != "25" {
		t.Fatalf("composed = %v, issues = %+v", address, issues)
	}
	if prefix, ok := AssignedPrefix(machine, narrow, machine.Spec().Get("network", "addresses").Items()[0]); !ok || prefix.String() != "192.0.2.11/25" {
		t.Fatalf("assigned prefix = %v", prefix)
	}
	if _, issues := ComposeNetwork(machine, api.NewCatalog(objects)); !issueAt(issues, "$.spec.network.addresses[0].address", "lies in no declared Network") {
		t.Fatalf("an assignment in a graph without Networks: %+v", issues)
	}
}

func TestAZeroLengthNetworkPrefixComposesNoAssignment(t *testing.T) {
	machine, catalog := fixture()
	objects := []api.Object{}
	for _, o := range catalog.Objects() {
		if o.Kind() != api.Network {
			objects = append(objects, o)
		}
	}
	_, issues := ComposeNetwork(machine, api.NewCatalog(append(objects, lan(m("ipv4", m("cidr", "0.0.0.0/0"))))))
	for _, issue := range issues {
		if issue.Code == "api.value" && issue.Field == "$.spec.network.addresses[0].address" && strings.Contains(issue.Message, "zero-length prefix 0.0.0.0/0") && issue.Remediation == "set spec.ipv4.cidr on Network/lan to the subnet its hosts actually share" {
			return
		}
	}
	t.Fatalf("issues = %+v", issues)
}

func TestATaggedStackMustCarryItsNetworksVLAN(t *testing.T) {
	machine, _ := fixture()
	machine = withNetwork(machine, []string{"network", "addresses"}, list(m("name", "primary", "address", "192.0.2.11", "interface", "br0")))
	tagged := lan(m("vlan", api.IntegerValue("200"), "ipv4", m("cidr", "192.0.2.0/24")))
	_, catalog := fixture()
	provider, _ := catalog.Find(api.InfraProvider, "metal")
	trunk := provider.WithSpec(provider.Spec().With("segments", list(m("name", "net", "taggedNetworkRefs", api.StringList("lan"), "baremetal", m()))))
	if issues := membershipIssues(t, machine, vlanConfig("200"), tagged, trunk); issueAt(issues, "$.spec.network.addresses[0].interface", "VLAN") || issueAt(issues, "$.spec.network.configRef", "VLAN") {
		t.Fatalf("a matching VLAN is refused: %+v", issues)
	}
	if issues := membershipIssues(t, machine, vlanConfig("300"), tagged); !issueAt(issues, "$.spec.network.addresses[0].interface", "tags VLAN 300, and its Network/lan is VLAN 200") {
		t.Fatalf("issues = %+v", issues)
	}
	if issues := membershipIssues(t, machine, vlanConfig("200"), lan(m("ipv4", m("cidr", "192.0.2.0/24")))); !issueAt(issues, "$.spec.network.addresses[0].interface", "declares no vlan") {
		t.Fatalf("issues = %+v", issues)
	}
}

func TestAPlacedMachineUsesOnlyNetworksOfItsSite(t *testing.T) {
	machine, _ := fixture()
	machine = withNetwork(machine, []string{"placement", "site"}, api.StringValue("a"))
	env := object(api.Environment, "env", m("domains", m("base", "example.test"), "sites", list(m("name", "a"), m("name", "b"))))
	if issues := membershipIssues(t, machine, env, lan(m("sites", list(api.StringValue("b")), "ipv4", m("cidr", "192.0.2.0/24")))); !issueAt(issues, "$.spec.network.addresses[0].address", "not scoped to site a") {
		t.Fatalf("issues = %+v", issues)
	}
}

func TestTheInstallRouteUsesItsNetworksGateway(t *testing.T) {
	machine, _ := fixture()
	config := object(api.NetworkConfig, "net", m("nmstate", m("interfaces", list(m("name", "eth0", "type", "ethernet")),
		"routes", m("config", list(m("destination", "0.0.0.0/0", "next-hop-interface", "eth0", "next-hop-address", "192.0.2.1"))))))
	if issues := membershipIssues(t, machine, config, lan(m("ipv4", m("cidr", "192.0.2.0/24", "gateway", "192.0.2.1")))); issueAt(issues, "$.spec.network.installAddressRef", "gateway") {
		t.Fatalf("the Network's own gateway is refused: %+v", issues)
	}
	if issues := membershipIssues(t, machine, config, lan(m("ipv4", m("cidr", "192.0.2.0/24", "gateway", "192.0.2.254")))); !issueAt(issues, "$.spec.network.installAddressRef", "the default route in NetworkConfig/net of ") || !issueAt(issues, "$.spec.network.installAddressRef", "is not gateway 192.0.2.254 of its install network Network/lan") {
		t.Fatalf("issues = %+v", issues)
	}
}
