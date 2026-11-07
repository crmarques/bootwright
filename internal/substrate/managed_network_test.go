package substrate

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func armNetwork(name string, subnet, arm api.Value) api.Object {
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
	network := armNetwork("lab", m("cidr", "192.0.2.0/24", "gateway", "192.0.2.1"), m("providerRef", "lab", "bridge", "virbr-lab", "taggedNetworkRefs", list(api.StringValue("tagged"))))
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
	requireArmIssue(t, networkArmIssues(armNetwork("lab", m("cidr", "192.0.2.0/24", "gateway", "192.0.2.1"), m("providerRef", "metal", "bridge", "virbr-lab")), metal),
		"$.spec.libvirt.providerRef", "which is not a libvirt provider")
	requireArmIssue(t, networkArmIssues(armNetwork("lab", m("cidr", "192.0.2.0/24"), m("providerRef", "lab", "bridge", "virbr-lab"))),
		"$.spec.libvirt.hostAddress", "declares no gateway")
	requireArmIssue(t, networkArmIssues(armNetwork("lab", m("cidr", "192.0.2.0/24"), m("providerRef", "lab", "bridge", "virbr-lab", "hostAddress", "192.0.2.255"))),
		"$.spec.libvirt.hostAddress", "is the broadcast address of 192.0.2.0/24")
	requireArmIssue(t, networkArmIssues(armNetwork("lab", m("cidr", "192.0.2.0/24"), m("providerRef", "lab", "bridge", "virbr-lab", "hostAddress", "2001:db8::1"))),
		"$.spec.libvirt.hostAddress", "declares no subnet for")
	untagged := obj(api.Network, "untagged", m("provided", true, "ipv4", m("cidr", "203.0.113.0/24")))
	issues := networkArmIssues(armNetwork("lab", m("cidr", "192.0.2.0/24", "gateway", "192.0.2.1"), m("providerRef", "lab", "bridge", "virbr-lab", "taggedNetworkRefs", list(api.StringValue("untagged"), api.StringValue("lab")))), untagged)
	requireArmIssue(t, issues, "$.spec.libvirt.taggedNetworkRefs[0]", "Network/untagged declares no vlan")
	requireArmIssue(t, issues, "$.spec.libvirt.taggedNetworkRefs[1]", "carried untagged on its own bridge")
}

// A managed Network is a segment of the provider its arm names, under its own
// name, carrying the host address with its subnet's prefix and its forward
// mode; an MTU of 1500 is the default libvirt already gives and is not carried.
func TestAManagedNetworkIsASegmentOfItsProvider(t *testing.T) {
	lab := obj(api.InfraProvider, "lab", m("libvirt", m("machineRef", "host"), "segments", list(m("name", "uplink", "libvirt", m("bridge", "br0")))))
	routed := armNetwork("routed", m("cidr", "192.0.2.128/25"), m("providerRef", "lab", "bridge", "virbr-routed", "forward", "none", "hostAddress", "192.0.2.129"))
	routed = routed.WithSpec(routed.Spec().With("mtu", api.IntegerValue("9000")))
	guests := armNetwork("guests", m("cidr", "198.51.100.0/24", "gateway", "198.51.100.1"), m("providerRef", "lab", "bridge", "virbr-lab"))
	guests = guests.WithSpec(guests.Spec().With("mtu", api.IntegerValue("1500")))
	elsewhere := armNetwork("elsewhere", m("cidr", "203.0.113.0/24", "gateway", "203.0.113.1"), m("providerRef", "other", "bridge", "virbr-other"))
	provided := obj(api.Network, "provided", m("provided", true, "ipv4", m("cidr", "198.18.0.0/24")))
	segments := Segments(api.NewCatalog([]api.Object{lab, routed, guests, elsewhere, provided}), lab).Items()
	var names []string
	for _, segment := range segments {
		names = append(names, segment.Get("name").Text())
	}
	if strings.Join(names, ",") != "uplink,guests,routed" {
		t.Fatalf("segments = %v", names)
	}
	if _, managed := ManagedSegmentNetwork(segments[0]); managed {
		t.Fatal("a provider segment read as a managed network")
	}
	for index, want := range []ManagedNetwork{{Prefix: "198.51.100.1/24", Bridge: "virbr-lab", Forward: "nat"}, {Prefix: "192.0.2.129/25", Bridge: "virbr-routed", Forward: "none"}} {
		if got, managed := ManagedSegmentNetwork(segments[index+1]); !managed || got != want {
			t.Fatalf("segment %s = %+v (%v), want %+v", names[index+1], got, managed, want)
		}
	}
	if mtu := EmittedMTU(segments[1]); mtu != 0 {
		t.Fatalf("the default MTU was emitted as %d", mtu)
	}
	if mtu := EmittedMTU(segments[2]); mtu != 9000 {
		t.Fatalf("mtu = %d", mtu)
	}
	if untagged, tagged := SegmentUntagged(segments[2]), SegmentTagged(segments[2]); untagged != "routed" || len(tagged) != 0 {
		t.Fatalf("carriage of routed = %q, %v", untagged, tagged)
	}
	if claims := AddressClaims(routed); len(claims) != 1 || claims[0] != (AddressClaim{Path: "spec.libvirt.hostAddress", Address: "192.0.2.129"}) {
		t.Fatalf("claims = %+v", claims)
	}
	if _, bears := EffectBearing(provided); bears {
		t.Fatal("a provided Network bears an effect")
	}
	if _, bears := EffectBearing(routed); !bears {
		t.Fatal("a managed Network bears no effect")
	}
}
