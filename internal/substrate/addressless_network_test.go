package substrate

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func addresslessNetwork() api.Object {
	return obj(api.Network, "staging", m("provided", false, "vlan", api.IntegerValue("10"),
		"ipv4", m("cidr", "192.0.2.0/24", "gateway", "192.0.2.1"),
		"libvirt", m("providerRef", "lab", "bridge", "br-trunk", "forward", "none", "hostAddressing", "none")))
}

func TestAddresslessTaggedNetworkNeverDefaultsAGatewayOntoTheHost(t *testing.T) {
	o, _ := Normalize(addresslessNetwork(), api.Catalog{})
	if o.Spec().Get("libvirt").Has("hostAddress") {
		t.Fatal("an addressless network acquired a host address")
	}
	if _, ok := HostAddress(o); ok || len(AddressClaims(o)) != 0 {
		t.Fatal("an addressless network claims a host IP")
	}
	if issues := networkArmIssues(o); len(issues) != 0 {
		t.Fatal(issues)
	}
}

func TestAddresslessTaggedNetworkRefusesHostAddressNATAndRouting(t *testing.T) {
	for name, test := range map[string]struct {
		object api.Object
		field  string
	}{
		"untagged": {obj(api.Network, "staging", addresslessNetwork().Spec().Without("vlan")), "$.spec.libvirt.hostAddressing"},
		"address":  {withArm(addresslessNetwork(), "hostAddress", api.StringValue("192.0.2.2")), "$.spec.libvirt.hostAddress"},
		"NAT":      {withArm(addresslessNetwork(), "forward", api.StringValue("nat")), "$.spec.libvirt.forward"},
		"routing":  {withArm(addresslessNetwork(), "routedNetworkRefs", api.StringList("other")), "$.spec.libvirt.routedNetworkRefs"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := issuesAt(networkArmIssues(test.object), test.field); len(got) == 0 {
				t.Fatalf("addressless %s accepted", name)
			}
		})
	}
	peer := taggedNetwork("other", "lab", "br-trunk", "198.51.100.1/24", 20)
	peer = withArm(peer, "routedNetworkRefs", api.StringList("staging"))
	if got := issuesAt(networkArmIssues(peer, addresslessNetwork()), "$.spec.libvirt.routedNetworkRefs[0]"); len(got) == 0 {
		t.Fatal("routing into an addressless network accepted")
	}
}
