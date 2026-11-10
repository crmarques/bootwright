package substrate

import (
	"fmt"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func taggedNetwork(name, provider, bridge, address string, vlan int) api.Object {
	o := managedNetwork(name, provider, bridge, address)
	return obj(api.Network, name, o.Spec().With("vlan", api.IntegerValue(fmt.Sprint(vlan))))
}

func withArm(o api.Object, key string, value api.Value) api.Object {
	return obj(api.Network, o.Name(), o.Spec().With("libvirt", o.Spec().Get("libvirt").With(key, value)))
}

func TestATaggedManagedNetworkIsASegmentCarryingItselfTagged(t *testing.T) {
	lab := topologyProvider("lab", "host", "8000", topologyProfile())
	public := withArm(taggedNetwork("ceph-public", "lab", "br-trunk", "192.0.2.1/24", 140), "taggedNetworkRefs", api.StringList("ceph-cluster"))
	cluster := obj(api.Network, "ceph-cluster", m("provided", true, "vlan", api.IntegerValue("120"), "ipv4", m("cidr", "198.51.100.0/24")))
	catalog := api.NewCatalog([]api.Object{lab, public, cluster, topologyHost("host")})
	segment, found := NamedSegment(catalog, lab, "ceph-public")
	if !found {
		t.Fatal("a tagged managed Network is no segment of its provider")
	}
	if SegmentUntagged(segment) != "" || !SegmentCarriesTagged(segment, "ceph-public") || !SegmentCarriesTagged(segment, "ceph-cluster") || len(SegmentTagged(segment)) != 2 {
		t.Fatalf("carriage = %q, %v", SegmentUntagged(segment), SegmentTagged(segment))
	}
	managed, isManaged := ManagedSegmentNetwork(segment)
	want := ManagedNetwork{Prefix: "192.0.2.1/24", Bridge: "br-trunk", Forward: "nat", VLAN: 140}
	if !isManaged || managed != want || managed.HostDevice() != "br-trunk.140" {
		t.Fatalf("managed = %+v (%v), want %+v", managed, isManaged, want)
	}
	if got := SegmentLabel(catalog, lab, "ceph-public"); got != "segment ceph-public (tagged managed Network/ceph-public on trunk br-trunk of InfraProvider/lab)" {
		t.Fatalf("label = %q", got)
	}
	if got := CarriageField(catalog, lab, "ceph-public", false); got != "" {
		t.Fatalf("a tagged managed Network's untagged carriage is settable at %q", got)
	}
	if issues := Validate(public, catalog); len(issues) != 0 {
		t.Fatalf("issues = %v", issues)
	}
}

func TestTaggedManagedNetworksOfOneProviderShareATrunk(t *testing.T) {
	lab := topologyProvider("lab", "host", "8000", topologyProfile())
	public := taggedNetwork("ceph-public", "lab", "br-trunk", "192.0.2.1/24", 140)
	arbiter := taggedNetwork("ceph-arbiter", "lab", "br-trunk", "203.0.113.1/24", 150)
	catalog := api.NewCatalog([]api.Object{lab, public, arbiter, topologyHost("host")})
	for _, o := range []api.Object{public, arbiter} {
		if issues := Validate(o, catalog); len(issues) != 0 {
			t.Fatalf("%s sharing its trunk was refused: %v", o.Identity(), issues)
		}
	}
}

func TestATrunkRefusesAnUntaggedOwnerOrAProviderSegment(t *testing.T) {
	const field = "$.spec.libvirt.bridge"
	host := topologyHost("host")
	lab := topologyProvider("lab", "host", "8000", topologyProfile())
	public := taggedNetwork("ceph-public", "lab", "br-trunk", "192.0.2.1/24", 140)
	for name, test := range map[string]struct {
		objects []api.Object
		label   string
	}{
		"an untagged managed Network": {[]api.Object{managedNetwork("guests", "lab", "br-trunk", "198.51.100.1/24")}, "Network/guests of InfraProvider/lab"},
		"a provider segment":          {[]api.Object{topologyProvider("other", "host", "8001", topologyProfile(), externalAttachment("uplink", "br-trunk"))}, "spec.segments[0] of InfraProvider/other"},
		"another provider's tagged Network": {[]api.Object{topologyProvider("other", "host", "8001", topologyProfile()), taggedNetwork("storage", "other", "br-trunk", "198.51.100.1/24", 160)},
			"Network/storage of InfraProvider/other"},
	} {
		t.Run(name, func(t *testing.T) {
			catalog := api.NewCatalog(append([]api.Object{host, lab, public}, test.objects...))
			refused := issuesAt(Validate(public, catalog), field)
			want := "bridge br-trunk of Network/ceph-public of InfraProvider/lab is also named by " + test.label + " on host Machine/host; a trunk Bootwright creates carries only the tagged managed Networks of one provider"
			if len(refused) != 1 || refused[0].Code != "api.invariant" || refused[0].Message != want {
				t.Fatalf("refused = %v", refused)
			}
		})
	}
	leg := topologyProvider("other", "host", "8001", topologyProfile(), externalAttachment("uplink", "br-trunk.140"))
	refused := issuesAt(Validate(public, api.NewCatalog([]api.Object{host, lab, public, leg})), field)
	if len(refused) != 1 || refused[0].Message != "the leg of Network/ceph-public of InfraProvider/lab is the host VLAN device br-trunk.140, which spec.segments[0] of InfraProvider/other also names on host Machine/host" {
		t.Fatalf("a bridge named as a leg device: %v", refused)
	}
}

func TestATrunkRefusesTwoNetworksOfOneVLAN(t *testing.T) {
	lab := topologyProvider("lab", "host", "8000", topologyProfile())
	public := taggedNetwork("ceph-public", "lab", "br-trunk", "192.0.2.1/24", 140)
	east := obj(api.Network, "east", public.Spec().With("sites", list(api.StringValue("east"))))
	west := taggedNetwork("west", "lab", "br-trunk", "203.0.113.1/24", 140)
	west = obj(api.Network, "west", west.Spec().With("sites", list(api.StringValue("west"))))
	catalog := api.NewCatalog([]api.Object{lab, east, west, topologyHost("host")})
	refused := issuesAt(Validate(east, catalog), "$.spec.vlan")
	if len(refused) != 1 || refused[0].Message != "VLAN 140 of Network/east of InfraProvider/lab is also the VLAN of Network/west of InfraProvider/lab on trunk br-trunk on host Machine/host; the tagged managed Networks of one trunk carry distinct VLANs" {
		t.Fatalf("refused = %v", refused)
	}
	other := taggedNetwork("west", "lab", "br-other", "203.0.113.1/24", 140)
	other = obj(api.Network, "west", other.Spec().With("sites", list(api.StringValue("west"))))
	if issues := Validate(east, api.NewCatalog([]api.Object{lab, east, other, topologyHost("host")})); len(issues) != 0 {
		t.Fatalf("one VLAN on two trunks was refused: %v", issues)
	}
}

func TestALegDeviceFitsTheInterfaceNameBound(t *testing.T) {
	lab := topologyProvider("lab", "host", "8000", topologyProfile())
	fits := taggedNetwork("fits", "lab", "br-trunk-0", "192.0.2.1/24", 4094)
	long := taggedNetwork("long", "lab", "br-trunk-01", "203.0.113.1/24", 4094)
	catalog := api.NewCatalog([]api.Object{lab, fits, long, topologyHost("host")})
	if issues := Validate(fits, catalog); len(issues) != 0 {
		t.Fatalf("a 15-byte leg device was refused: %v", issues)
	}
	refused := issuesAt(Validate(long, catalog), "$.spec.libvirt.bridge")
	want := api.Issue{Code: "api.invariant", Field: "$.spec.libvirt.bridge",
		Message:     "the leg of tagged managed Network/long is the host VLAN device br-trunk-01.4094, 16 bytes, and a Linux interface name holds at most 15",
		Remediation: "shorten spec.libvirt.bridge of Network/long to at most 10 bytes"}
	if len(refused) != 1 || refused[0] != want {
		t.Fatalf("refused = %v", refused)
	}
}

func TestATaggedManagedNetworkRefusesIPv6AndAnMTUOtherThan1500(t *testing.T) {
	lab := topologyProvider("lab", "host", "8000", topologyProfile())
	dual := taggedNetwork("dual", "lab", "br-trunk", "192.0.2.1/24", 140)
	dual = obj(api.Network, "dual", dual.Spec().With("ipv6", m("cidr", "fd00:1::/64")).With("mtu", api.IntegerValue("9000")))
	jumbo := obj(api.Network, "jumbo", m("provided", true, "vlan", api.IntegerValue("120"), "mtu", api.IntegerValue("9000"), "ipv4", m("cidr", "198.51.100.0/24")))
	dual = withArm(dual, "taggedNetworkRefs", api.StringList("jumbo"))
	issues := Validate(dual, api.NewCatalog([]api.Object{lab, dual, jumbo, topologyHost("host")}))
	requireArmIssue(t, issues, "$.spec.ipv6", "IPv6 on a leg is not yet admitted (B1284)")
	requireArmIssue(t, issues, "$.spec.mtu", "is not yet admitted (B1285), so its mtu 9000 is refused")
	requireArmIssue(t, issues, "$.spec.libvirt.taggedNetworkRefs[0]", "carries Network/jumbo, whose mtu is 9000")
	untagged := managedNetwork("plain", "lab", "virbr-plain", "203.0.113.1/24")
	untagged = withArm(obj(api.Network, "plain", untagged.Spec().With("mtu", api.IntegerValue("9000"))), "taggedNetworkRefs", api.StringList("jumbo"))
	if issues := Validate(untagged, api.NewCatalog([]api.Object{lab, untagged, jumbo, topologyHost("host")})); len(issues) != 0 {
		t.Fatalf("an untagged managed Network was held to the leg rules: %v", issues)
	}
}

func TestTheManagedNetworkBoundCountsEachTrunk(t *testing.T) {
	const field = "$.spec.libvirt.providerRef"
	objects := []api.Object{topologyHost("host"), topologyProvider("a", "host", "8000", topologyProfile())}
	for index := range MaxManagedNetworks - 3 {
		objects = append(objects, managedNetwork(fmt.Sprintf("net-%02d", index), "a", fmt.Sprintf("br-%02d", index), fmt.Sprintf("10.%d.0.1/24", index)))
	}
	within := append(objects, taggedNetwork("vlan-a", "a", "br-trunk", "10.200.0.1/24", 140), taggedNetwork("vlan-b", "a", "br-trunk", "10.201.0.1/24", 150))
	catalog := api.NewCatalog(within)
	for _, o := range catalog.OfKind(api.Network) {
		if refused := issuesAt(Validate(o, catalog), field); len(refused) != 0 {
			t.Fatalf("%d Networks on one trunk were refused: %v", MaxManagedNetworks-1, refused)
		}
	}
	over := api.NewCatalog(append(within, taggedNetwork("vlan-c", "a", "br-other", "10.202.0.1/24", 160)))
	refused := map[string][]api.Issue{}
	for _, o := range over.OfKind(api.Network) {
		if issues := issuesAt(Validate(o, over), field); len(issues) != 0 {
			refused[o.Name()] = issues
		}
	}
	want := "one libvirt host defines at most 20 managed Networks across its providers, each trunk of tagged ones counting as one more, and 20 name providers on host Machine/host on 2 trunks; over the bound: Network/vlan-c"
	if len(refused) != 1 || len(refused["vlan-c"]) != 1 || refused["vlan-c"][0].Message != want {
		t.Fatalf("refused = %v", refused)
	}
}
