package substrate

import (
	"fmt"
	"net/netip"
	"os"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/network"
)

func topologyHost(name string) api.Object {
	return obj(api.Machine, name, m())
}

func topologyProfile(disks ...api.Value) api.Value {
	return m("name", "small", "cpu", api.IntegerValue("2"), "memoryMiB", api.IntegerValue("2048"), "diskGiB", api.IntegerValue("20"), "dataDisks", list(disks...))
}

func topologyProvider(name, host, port string, profile api.Value, attachments ...api.Value) api.Object {
	arm := m("machineRef", host, "uri", "qemu:///system",
		"bmcEmulation", m("port", api.IntegerValue(port), "bindAddress", "192.0.2.1", "auth", m("credentialsRef", "bmc")),
		"machineProfiles", list(profile))
	return obj(api.InfraProvider, name, m("libvirt", arm, "segments", list(attachments...)))
}

func managedNetwork(name, provider, bridge, address string) api.Object {
	prefix := netip.MustParsePrefix(address)
	family := "ipv4"
	if prefix.Addr().Is6() {
		family = "ipv6"
	}
	return obj(api.Network, name, m("provided", false, "mtu", api.IntegerValue("1500"), family, m("cidr", prefix.Masked().String()),
		"libvirt", m("providerRef", provider, "bridge", bridge, "forward", "nat", "hostAddress", prefix.Addr().String())))
}

func externalAttachment(name, bridge string) api.Value {
	return m("name", name, "libvirt", m("bridge", bridge))
}

func issuesAt(issues []api.Issue, field string) []api.Issue {
	var found []api.Issue
	for _, issue := range issues {
		if issue.Field == field {
			found = append(found, issue)
		}
	}
	return found
}

func TestLibvirtDataDisksAreAtMostSevenAndNeverRoot(t *testing.T) {
	disks := func(count int) []api.Value {
		var found []api.Value
		for index := range count {
			found = append(found, m("name", fmt.Sprintf("data-%d", index+1), "sizeGiB", api.IntegerValue("10")))
		}
		return found
	}
	host := topologyHost("host")
	validate := func(profile api.Value) []api.Issue {
		provider := topologyProvider("lab", "host", "8000", profile)
		return Validate(provider, api.NewCatalog([]api.Object{provider, host}))
	}
	if issues := validate(topologyProfile(disks(MaxDataDisks)...)); len(issues) != 0 {
		t.Fatalf("seven data disks were refused: %v", issues)
	}
	issues := validate(topologyProfile(disks(MaxDataDisks + 1)...))
	bound := issuesAt(issues, "$.spec.libvirt.machineProfiles[0].dataDisks")
	if len(issues) != 1 || len(bound) != 1 || bound[0].Code != "api.invariant" || !strings.Contains(bound[0].Message, "at most 7 data disks beside its root disk") ||
		bound[0].Remediation != "declare at most 7 dataDisks on spec.libvirt.machineProfiles[0] of InfraProvider/lab" {
		t.Fatalf("eight data disks: %v", issues)
	}
	issues = validate(topologyProfile(m("name", "data", "sizeGiB", api.IntegerValue("10")), m("name", "root", "sizeGiB", api.IntegerValue("10"))))
	root := issuesAt(issues, "$.spec.libvirt.machineProfiles[0].dataDisks[1].name")
	if len(issues) != 1 || len(root) != 1 || root[0].Code != "api.invariant" || !strings.Contains(root[0].Message, "reserved for the root disk") ||
		root[0].Remediation != "rename spec.libvirt.machineProfiles[0].dataDisks[1] on InfraProvider/lab" {
		t.Fatalf("a data disk named root: %v", issues)
	}
}

// A managed Network is a segment of its provider under its own name, so an
// attachment of that provider with the same name refuses at the Network.
func TestAManagedNetworkNameIsNoAttachmentOfItsProvider(t *testing.T) {
	const field = "$.spec.libvirt.providerRef"
	host := topologyHost("host")
	a := topologyProvider("a", "host", "8000", topologyProfile(), externalAttachment("guests", "br-guests"))
	b := topologyProvider("b", "host", "8001", topologyProfile())
	guests := managedNetwork("guests", "a", "virbr-a", "198.51.100.1/24")
	refused := issuesAt(Validate(guests, api.NewCatalog([]api.Object{a, b, host, guests})), field)
	if len(refused) != 1 || refused[0].Code != "api.invariant" ||
		!strings.Contains(refused[0].Message, "spec.segments[0] of InfraProvider/a is also named guests") ||
		!strings.Contains(refused[0].Remediation, "InfraProvider/a") || !strings.Contains(refused[0].Remediation, "Network/guests") {
		t.Fatalf("a managed Network named like its provider's attachment: %v", refused)
	}
	elsewhere := managedNetwork("guests", "b", "virbr-b", "198.51.100.1/24")
	if issues := Validate(elsewhere, api.NewCatalog([]api.Object{a, b, host, elsewhere})); len(issues) != 0 {
		t.Fatalf("an attachment of another provider sharing the name was refused: %v", issues)
	}
}

func TestAManagedBridgeBelongsToOneNetworkOnItsHost(t *testing.T) {
	const field = "$.spec.libvirt.bridge"
	host, other := topologyHost("host"), topologyHost("other")
	a := topologyProvider("a", "host", "8000", topologyProfile())
	guests := managedNetwork("guests", "a", "virbr-x", "198.51.100.1/24")
	for name, test := range map[string]struct {
		peer  api.Object
		label string
		extra []api.Object
	}{
		"two managed Networks of two providers": {managedNetwork("storage", "b", "virbr-x", "203.0.113.1/24"), "Network/storage of InfraProvider/b", []api.Object{topologyProvider("b", "host", "8001", topologyProfile())}},
		"two managed Networks of one provider":  {managedNetwork("storage", "a", "virbr-x", "203.0.113.1/24"), "Network/storage of InfraProvider/a", nil},
		"a managed Network and an attachment":   {api.Object{}, "spec.segments[0] of InfraProvider/b", []api.Object{topologyProvider("b", "host", "8001", topologyProfile(), externalAttachment("storage", "virbr-x"))}},
	} {
		t.Run(name, func(t *testing.T) {
			objects := append([]api.Object{a, host, guests}, test.extra...)
			if test.peer.Kind() != "" {
				objects = append(objects, test.peer)
			}
			refused := issuesAt(Validate(guests, api.NewCatalog(objects)), field)
			if len(refused) != 1 || refused[0].Code != "api.invariant" ||
				refused[0].Message != "bridge virbr-x of Network/guests of InfraProvider/a is also named by "+test.label+" on host Machine/host; a bridge a managed Network defines belongs to that Network alone" ||
				refused[0].Remediation != "give Network/guests or "+test.label+" its own bridge" {
				t.Fatalf("Network/guests sharing a bridge with %s: %v", test.label, refused)
			}
		})
	}
	b := topologyProvider("b", "host", "8001", topologyProfile(), externalAttachment("guests", "virbr-y"), externalAttachment("storage", "virbr-y"))
	if issues := Validate(b, api.NewCatalog([]api.Object{b, host})); len(issues) != 0 {
		t.Fatalf("two attachments sharing an existing bridge were refused: %v", issues)
	}
	c := topologyProvider("c", "other", "8000", topologyProfile())
	elsewhere := managedNetwork("storage", "c", "virbr-x", "203.0.113.1/24")
	if issues := Validate(guests, api.NewCatalog([]api.Object{a, c, guests, elsewhere, host, other})); len(issues) != 0 {
		t.Fatalf("a bridge of the same name on another host was refused: %v", issues)
	}
	invalid, peer := managedNetwork("guests", "a", `virbr"x`, "198.51.100.1/24"), managedNetwork("storage", "a", `virbr"x`, "203.0.113.1/24")
	if issues := Validate(invalid, api.NewCatalog([]api.Object{a, invalid, peer, host})); len(issues) != 0 {
		t.Fatalf("a bridge outside its grammar was compared beside the schema's refusal: %v", issues)
	}
}

// The host routes each managed prefix to its own bridge, so two managed
// Networks on one host, of one provider or two, whose prefixes overlap,
// identical or nested, IPv4 or IPv6, are refused naming both. Two that reach a
// common site are already refused by the Network CIDR rule, so only Networks of
// disjoint sites read this refusal.
func TestOneHostNeverOverlapsTwoManagedPrefixes(t *testing.T) {
	const field = "$.spec.libvirt.hostAddress"
	host, other := topologyHost("host"), topologyHost("other")
	a, b := topologyProvider("a", "host", "8000", topologyProfile()), topologyProvider("b", "host", "8001", topologyProfile())
	for name, test := range map[string]struct {
		own, peer         string
		ownMask, peerMask string
	}{
		"identical IPv4": {"198.51.100.1/24", "198.51.100.2/24", "198.51.100.0/24", "198.51.100.0/24"},
		"nested IPv4":    {"198.51.100.1/24", "198.51.100.129/25", "198.51.100.0/24", "198.51.100.128/25"},
		"identical IPv6": {"fd00:1::1/64", "fd00:1::2/64", "fd00:1::/64", "fd00:1::/64"},
		"nested IPv6":    {"fd00:1::1/48", "fd00:1:0:5::1/64", "fd00:1::/48", "fd00:1:0:5::/64"},
	} {
		t.Run(name, func(t *testing.T) {
			guests, storage := managedNetwork("guests", "a", "virbr-a", test.own), managedNetwork("storage", "b", "virbr-b", test.peer)
			family := "ipv4"
			if strings.HasSuffix(name, "IPv6") {
				family = "ipv6"
			}
			if !network.Overlapping(guests, storage, family) {
				t.Fatalf("the Network CIDR rule does not refuse %s beside %s", guests.Identity(), storage.Identity())
			}
			for _, sited := range []api.Object{guests, storage} {
				if issues := Validate(sited, api.NewCatalog([]api.Object{a, b, host, guests, storage})); len(issues) != 0 {
					t.Fatalf("%s repeats the Network CIDR rule's refusal: %v", sited.Identity(), issues)
				}
			}
			guests = guests.WithSpec(guests.Spec().With("sites", list(api.StringValue("east"))))
			storage = storage.WithSpec(storage.Spec().With("sites", list(api.StringValue("west"))))
			catalog := api.NewCatalog([]api.Object{a, b, host, guests, storage})
			for _, pair := range []struct {
				own, peer         api.Object
				ownMask, peerMask string
				ownBy, peerBy     string
			}{{guests, storage, test.ownMask, test.peerMask, "a", "b"}, {storage, guests, test.peerMask, test.ownMask, "b", "a"}} {
				issues := Validate(pair.own, catalog)
				refused := issuesAt(issues, field)
				message := fmt.Sprintf("managed prefix %s of %s of InfraProvider/%s overlaps managed prefix %s of %s of InfraProvider/%s on host Machine/host, and the host routes each managed prefix to its own bridge",
					pair.ownMask, pair.own.Identity(), pair.ownBy, pair.peerMask, pair.peer.Identity(), pair.peerBy)
				remediation := fmt.Sprintf("give %s or %s a subnet no other managed Network on host Machine/host overlaps", pair.own.Identity(), pair.peer.Identity())
				if len(issues) != 1 || len(refused) != 1 || refused[0].Code != "api.invariant" || refused[0].Message != message || refused[0].Remediation != remediation {
					t.Fatalf("%s beside %s: %v", pair.own.Identity(), pair.peer.Identity(), issues)
				}
			}
		})
	}
	for name, test := range map[string]struct {
		peer api.Object
		host string
	}{
		"disjoint IPv4":    {managedNetwork("storage", "b", "virbr-b", "198.51.100.129/25"), "host"},
		"IPv4 beside IPv6": {managedNetwork("storage", "b", "virbr-b", "fd00:1::1/64"), "host"},
		"another host":     {managedNetwork("storage", "b", "virbr-b", "198.51.100.2/25"), "other"},
	} {
		t.Run(name, func(t *testing.T) {
			provider := topologyProvider("b", test.host, "8001", topologyProfile())
			guests := managedNetwork("guests", "a", "virbr-a", "198.51.100.1/25")
			for _, network := range []api.Object{guests, test.peer} {
				if issues := Validate(network, api.NewCatalog([]api.Object{a, provider, guests, test.peer, host, other})); len(issues) != 0 {
					t.Fatalf("%s was refused: %v", network.Identity(), issues)
				}
			}
		})
	}
}

func TestOneHostHoldsAtMostTwentyManagedNetworks(t *testing.T) {
	const field = "$.spec.libvirt.providerRef"
	host := topologyHost("host")
	validate := func(count int) map[string][]api.Issue {
		objects := []api.Object{host, topologyProvider("a", "host", "8000", topologyProfile()), topologyProvider("b", "host", "8001", topologyProfile())}
		for index := range count {
			objects = append(objects, managedNetwork(fmt.Sprintf("net-%02d", index), []string{"a", "b"}[index%2], fmt.Sprintf("br-%02d", index), fmt.Sprintf("10.%d.0.1/24", index)))
		}
		catalog := api.NewCatalog(objects)
		refused := map[string][]api.Issue{}
		for _, network := range catalog.OfKind(api.Network) {
			if issues := issuesAt(Validate(network, catalog), field); len(issues) != 0 {
				refused[network.Name()] = issues
			}
		}
		return refused
	}
	if refused := validate(MaxManagedNetworks); len(refused) != 0 {
		t.Fatalf("%d managed Networks were refused: %v", MaxManagedNetworks, refused)
	}
	refused := validate(MaxManagedNetworks + 2)
	want := api.Issue{Code: "api.value", Field: field,
		Message:     "one libvirt host defines at most 20 managed Networks across its providers, and 22 name providers on host Machine/host; over the bound: Network/net-20, Network/net-21",
		Remediation: "make Network/net-20, Network/net-21 provided networks on existing bridges, or name a provider on another host in their spec.libvirt.providerRef"}
	if len(refused) != 1 || len(refused["net-20"]) != 1 || refused["net-20"][0] != want {
		t.Fatalf("%d managed Networks: %v", MaxManagedNetworks+2, refused)
	}
}

func TestTheEmulatedBMCRangeCountsOnlyRealizedMachines(t *testing.T) {
	machine := func(name, provider string, provided bool) api.Object {
		return obj(api.Machine, name, m("substrate", m("providerRef", provider), "os", m("provided", provided)))
	}
	host := topologyHost("host")
	last := topologyProvider("last", "host", "65535", topologyProfile())
	if issues := Validate(last, api.NewCatalog([]api.Object{last, host, machine("one", "last", false), machine("two", "last", true)})); len(issues) != 0 {
		t.Fatalf("an OS-ready Machine was counted into the port range: %v", issues)
	}
	issues := issuesAt(Validate(last, api.NewCatalog([]api.Object{last, host, machine("one", "last", false), machine("two", "last", false)})), "$.spec.libvirt.bmcEmulation.port")
	if len(issues) != 1 || issues[0].Message != "the emulated BMC port range must end at or below 65535" {
		t.Fatalf("two realized Machines from port 65535: %v", issues)
	}
	a := topologyProvider("a", "host", "8000", topologyProfile())
	b := topologyProvider("b", "host", "8001", topologyProfile())
	catalog := api.NewCatalog([]api.Object{a, b, host, machine("one", "a", false), machine("two", "a", true)})
	for _, provider := range []api.Object{a, b} {
		if issues := Validate(provider, catalog); len(issues) != 0 {
			t.Fatalf("an OS-ready Machine overlapped %s with its neighbour: %v", provider.Identity(), issues)
		}
	}
}

func TestAnInfraProviderNameHasNoBlockDerivedLimit(t *testing.T) {
	host := topologyHost("host")
	for _, name := range []string{strings.Repeat("a", 63), strings.Repeat("a", 70)} {
		provider := topologyProvider(name, "host", "8000", topologyProfile())
		if issues := Validate(provider, api.NewCatalog([]api.Object{provider, host})); len(issues) != 0 {
			t.Fatalf("a %d-byte provider name got a per-kind diagnostic: %v", len(name), issues)
		}
	}
}

func TestMachineSpecStatesTheTopologyBounds(t *testing.T) {
	spec, err := os.ReadFile("../../specs/api/machines.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, phrase := range []string{
		fmt.Sprintf("A libvirt profile declares at most %d data disks", MaxDataDisks),
	} {
		if count := strings.Count(string(spec), phrase); count != 1 {
			t.Errorf("specs/api/machines.md states %q %d times, want once", phrase, count)
		}
	}
}
