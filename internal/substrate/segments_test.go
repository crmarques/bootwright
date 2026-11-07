package substrate

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func cudnSegment(name, network string, more ...any) api.Value {
	return m(append([]any{"name", name, "kubevirt", m("networkRef", m("name", network))}, more...)...)
}

func segmentIssues(provider api.Object, others ...api.Object) []api.Issue {
	catalog := api.NewCatalog(append([]api.Object{provider}, others...))
	normalized, _ := Normalize(provider, catalog)
	issues := ValidateAuthored(provider, catalog)
	return append(issues, Validate(normalized, api.NewCatalog(append([]api.Object{normalized}, others...)))...)
}

func requireNoIssueAt(t *testing.T, issues []api.Issue, field string) {
	t.Helper()
	for _, issue := range issues {
		if issue.Field == field {
			t.Fatalf("unexpected issue at %s: %+v", field, issue)
		}
	}
}

// A segment carries at most one network untagged and an allow-list of tagged
// ones, each declaring a vlan; a KubeVirt or vSphere segment carries exactly
// one untagged network, and no tagged one until B1172 observes what the fabric
// carries.
func TestASegmentsCarriageNamesItsNetworks(t *testing.T) {
	plain := obj(api.Network, "plain", m("provided", true, "ipv4", m("cidr", "192.0.2.0/24")))
	tagged := obj(api.Network, "tagged", m("provided", true, "vlan", api.IntegerValue("200"), "ipv4", m("cidr", "198.51.100.0/24")))
	metal := func(segment api.Value) api.Object {
		return obj(api.InfraProvider, "metal", m("baremetal", m(), "segments", list(segment)))
	}
	if issues := segmentIssues(metal(m("name", "net", "untaggedNetworkRef", "plain", "taggedNetworkRefs", api.StringList("tagged"), "baremetal", m())), plain, tagged); len(issues) != 0 {
		t.Fatalf("a segment carrying one untagged and one tagged network was refused: %+v", issues)
	}
	requireArmIssue(t, segmentIssues(metal(m("name", "net", "taggedNetworkRefs", api.StringList("plain"), "baremetal", m())), plain),
		"$.spec.segments[0].taggedNetworkRefs[0]", "Network/plain declares no vlan, so segment net of InfraProvider/metal cannot carry it tagged")
	requireArmIssue(t, segmentIssues(metal(m("name", "net", "untaggedNetworkRef", "tagged", "taggedNetworkRefs", api.StringList("tagged"), "baremetal", m())), tagged),
		"$.spec.segments[0].taggedNetworkRefs[0]", "carries Network/tagged untagged, so it cannot also carry it tagged")

	kubevirt := func(segment api.Value) api.Object {
		return obj(api.InfraProvider, "kv", m("kubevirt", m("kubeconfigRef", "host", "namespace", "guests"), "segments", list(segment)))
	}
	requireArmIssue(t, segmentIssues(kubevirt(cudnSegment("guests", "guests"))), "$.spec.segments[0].untaggedNetworkRef", "a kubevirt segment delivers exactly one network to each NIC on it")
	issues := segmentIssues(kubevirt(cudnSegment("guests", "guests")), plain)
	requireArmIssue(t, issues, "$.spec.segments[0].untaggedNetworkRef", "a kubevirt segment delivers exactly one network to each NIC on it")
	if issues[0].Code != "api.required" {
		t.Fatalf("code = %s", issues[0].Code)
	}
	requireArmIssue(t, segmentIssues(kubevirt(cudnSegment("guests", "guests", "untaggedNetworkRef", "plain", "taggedNetworkRefs", api.StringList("tagged"))), plain, tagged),
		"$.spec.segments[0].taggedNetworkRefs[0]", "tagged carriage over a kubevirt network is not yet admitted (B1172)")
	vsphere := obj(api.InfraProvider, "vc", m("vsphere", m(), "segments", list(m("name", "pg", "vsphere", m("portgroup", "pg")))))
	requireArmIssue(t, segmentIssues(vsphere, plain), "$.spec.segments[0].untaggedNetworkRef", "a vsphere segment delivers exactly one network")
}

// A hosted KubeVirt segment that is a typed secondary network of its host takes
// that network's mtu as its capacity, 1500 when it declares none, and refuses
// an mtu of its own; a segment on no typed host network keeps the default.
func TestAHostedKubeVirtSegmentTakesItsHostNetworksMTU(t *testing.T) {
	host := obj(api.OpenShiftCluster, "host", m("networking", m("secondaryNetworks", list(m("name", "jumbo", "mtu", api.IntegerValue("9000")), m("name", "plain")))))
	provider := obj(api.InfraProvider, "kv", m("kubevirt", m("hostClusterRef", "host", "namespace", "guests"),
		"segments", list(cudnSegment("jumbo", "jumbo", "mtu", api.IntegerValue("1500")), cudnSegment("plain", "plain", "mtu", api.IntegerValue("1500")), cudnSegment("other", "undeclared", "mtu", api.IntegerValue("1500")))))
	normalized, _ := Normalize(provider, api.NewCatalog([]api.Object{provider, host}))
	for index, want := range []int{9000, 0, 0} {
		if got := EmittedMTU(normalized.Spec().Get("segments").Items()[index]); got != want {
			t.Fatalf("segment %d capacity emitted as %d, want %d", index, got, want)
		}
	}
	authored := obj(api.InfraProvider, "kv", m("kubevirt", m("hostClusterRef", "host", "namespace", "guests"),
		"segments", list(cudnSegment("jumbo", "jumbo", "mtu", api.IntegerValue("9000")), cudnSegment("other", "undeclared", "mtu", api.IntegerValue("9000")))))
	issues := ValidateAuthored(authored, api.NewCatalog([]api.Object{authored, host}))
	requireArmIssue(t, issues, "$.spec.segments[0].mtu", "is secondary network jumbo of OpenShiftCluster/host, whose mtu is the segment's capacity")
	requireNoIssueAt(t, issues, "$.spec.segments[1].mtu")
	if issues[0].Remediation != "remove spec.segments[0].mtu from InfraProvider/kv, and set mtu on secondary network jumbo of OpenShiftCluster/host" {
		t.Fatalf("remediation = %q", issues[0].Remediation)
	}
}

// A managed Network is a segment carrying itself untagged and its own
// taggedNetworkRefs tagged, with its mtu as the capacity.
func TestAManagedNetworkSegmentCarriesItself(t *testing.T) {
	network := armNetwork("guests", m("cidr", "192.0.2.0/24", "gateway", "192.0.2.1"), m("providerRef", "lab", "bridge", "virbr-lab", "taggedNetworkRefs", api.StringList("storage")))
	network = network.WithSpec(network.Spec().With("mtu", api.IntegerValue("9000")))
	segment := NetworkSegment(network)
	if SegmentUntagged(segment) != "guests" || !SegmentCarriesTagged(segment, "storage") || SegmentMTU(segment) != 9000 {
		t.Fatalf("segment = %v", segment)
	}
	lab := obj(api.InfraProvider, "lab", m("libvirt", m("machineRef", "host"), "segments", list(m("name", "trunk", "libvirt", m("bridge", "br0")))))
	catalog := api.NewCatalog([]api.Object{lab, network})
	for name, want := range map[string]string{
		"guests": "segment guests (managed Network/guests of InfraProvider/lab)",
		"trunk":  "segment trunk (spec.segments[0] of InfraProvider/lab)",
	} {
		if got := SegmentLabel(catalog, lab, name); got != want {
			t.Fatalf("label of %s = %q", name, got)
		}
	}
	if got := CarriageField(catalog, lab, "guests", false); got != "" {
		t.Fatalf("a managed Network's untagged carriage is settable at %q", got)
	}
	if got := CapacityField(catalog, lab, "trunk"); got != "spec.segments[0].mtu of InfraProvider/lab" {
		t.Fatalf("capacity field = %q", got)
	}
}
