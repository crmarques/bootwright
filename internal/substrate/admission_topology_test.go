package substrate

import (
	"fmt"
	"os"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func topologyHost(name string) api.Object {
	return obj(api.Machine, name, m("capabilities", api.StringList("libvirt")))
}

func topologyProfile(disks ...api.Value) api.Value {
	return m("name", "small", "cpu", api.IntegerValue("2"), "memoryMiB", api.IntegerValue("2048"), "diskGiB", api.IntegerValue("20"), "dataDisks", list(disks...))
}

func topologyProvider(name, host, port string, profile api.Value, attachments ...api.Value) api.Object {
	arm := m("machineRef", host, "uri", "qemu:///system",
		"bmcEmulationDefaults", m("port", api.IntegerValue(port), "bindAddress", "192.0.2.1", "auth", m("credentialsRef", "bmc")),
		"machineProfiles", list(profile))
	return obj(api.InfraProvider, name, m("libvirt", arm, "networkAttachments", list(attachments...)))
}

func managedAttachment(name, bridge, address string) api.Value {
	return m("name", name, "libvirt", m("bridge", bridge, "management", "managed", "address", address, "forward", "nat"))
}

func externalAttachment(name, bridge string) api.Value {
	return m("name", name, "libvirt", m("bridge", bridge, "management", "external"))
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
	if len(issues) != 1 || len(bound) != 1 || bound[0].Code != "api.invariant" || !strings.Contains(bound[0].Message, "at most 7 data disks, vdb through vdh") ||
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

func TestOneHostNeverSharesAManagedAttachmentName(t *testing.T) {
	const field = "$.spec.networkAttachments[0].name"
	host, other := topologyHost("host"), topologyHost("other")
	a := topologyProvider("a", "host", "8000", topologyProfile(), managedAttachment("guests", "virbr-a", "198.51.100.1/24"))
	b := topologyProvider("b", "host", "8001", topologyProfile(), managedAttachment("guests", "virbr-b", "203.0.113.1/24"))
	catalog := api.NewCatalog([]api.Object{a, b, host})
	for _, pair := range [][2]api.Object{{a, b}, {b, a}} {
		refused := issuesAt(Validate(pair[0], catalog), field)
		if len(refused) != 1 || refused[0].Code != "api.invariant" ||
			!strings.Contains(refused[0].Message, "managed attachment guests is also a managed attachment of "+pair[1].Identity()+" on host Machine/host") ||
			!strings.Contains(refused[0].Remediation, pair[0].Identity()) || !strings.Contains(refused[0].Remediation, pair[1].Identity()) {
			t.Fatalf("%s sharing a managed attachment name with %s: %v", pair[0].Identity(), pair[1].Identity(), refused)
		}
	}
	elsewhere := topologyProvider("c", "other", "8000", topologyProfile(), managedAttachment("guests", "virbr-c", "192.0.2.129/25"))
	if issues := Validate(elsewhere, api.NewCatalog([]api.Object{a, elsewhere, host, other})); len(issues) != 0 {
		t.Fatalf("a provider on another host was refused: %v", issues)
	}
	external := topologyProvider("d", "host", "8001", topologyProfile(), externalAttachment("guests", "virbr-d"))
	catalog = api.NewCatalog([]api.Object{a, external, host})
	for _, provider := range []api.Object{a, external} {
		if issues := Validate(provider, catalog); len(issues) != 0 {
			t.Fatalf("an external attachment sharing a managed name refused %s: %v", provider.Identity(), issues)
		}
	}
}

func TestAManagedBridgeBelongsToOneAttachmentOnItsHost(t *testing.T) {
	const field = "$.spec.networkAttachments[0].libvirt.bridge"
	host, other := topologyHost("host"), topologyHost("other")
	for name, peer := range map[string]api.Value{
		"two managed attachments":              managedAttachment("storage", "virbr-x", "203.0.113.1/24"),
		"a managed and an external attachment": externalAttachment("storage", "virbr-x"),
	} {
		t.Run(name, func(t *testing.T) {
			a := topologyProvider("a", "host", "8000", topologyProfile(), managedAttachment("guests", "virbr-x", "198.51.100.1/24"))
			b := topologyProvider("b", "host", "8001", topologyProfile(), peer)
			catalog := api.NewCatalog([]api.Object{a, b, host})
			for _, pair := range [][2]api.Object{{a, b}, {b, a}} {
				refused := issuesAt(Validate(pair[0], catalog), field)
				if len(refused) != 1 || refused[0].Code != "api.invariant" ||
					!strings.Contains(refused[0].Message, "bridge virbr-x of networkAttachments[0] is also named by networkAttachments[0] of "+pair[1].Identity()+" on host Machine/host") ||
					!strings.Contains(refused[0].Remediation, pair[0].Identity()) || !strings.Contains(refused[0].Remediation, pair[1].Identity()) {
					t.Fatalf("%s sharing a bridge with %s: %v", pair[0].Identity(), pair[1].Identity(), refused)
				}
			}
		})
	}
	a := topologyProvider("a", "host", "8000", topologyProfile(), externalAttachment("guests", "virbr-x"))
	b := topologyProvider("b", "host", "8001", topologyProfile(), externalAttachment("storage", "virbr-x"))
	for _, provider := range []api.Object{a, b} {
		if issues := Validate(provider, api.NewCatalog([]api.Object{a, b, host})); len(issues) != 0 {
			t.Fatalf("two external attachments sharing a bridge refused %s: %v", provider.Identity(), issues)
		}
	}
	both := topologyProvider("a", "host", "8000", topologyProfile(),
		managedAttachment("guests", "virbr-x", "198.51.100.1/24"), managedAttachment("storage", "virbr-x", "203.0.113.1/24"))
	refused := issuesAt(Validate(both, api.NewCatalog([]api.Object{both, host})), "$.spec.networkAttachments[1].libvirt.bridge")
	if len(refused) != 1 || !strings.Contains(refused[0].Message, "bridge virbr-x of networkAttachments[1] is also named by networkAttachments[0] of InfraProvider/a") {
		t.Fatalf("one provider defining one bridge twice: %v", refused)
	}
	managed := topologyProvider("a", "host", "8000", topologyProfile(), managedAttachment("guests", "virbr-x", "198.51.100.1/24"))
	elsewhere := topologyProvider("c", "other", "8000", topologyProfile(), managedAttachment("storage", "virbr-x", "203.0.113.1/24"))
	if issues := Validate(managed, api.NewCatalog([]api.Object{managed, elsewhere, host, other})); len(issues) != 0 {
		t.Fatalf("a bridge of the same name on another host was refused: %v", issues)
	}
	invalid := topologyProvider("a", "host", "8000", topologyProfile(),
		managedAttachment("guests", `virbr"x`, "198.51.100.1/24"), managedAttachment("storage", `virbr"x`, "203.0.113.1/24"))
	if issues := Validate(invalid, api.NewCatalog([]api.Object{invalid, host})); len(issues) != 0 {
		t.Fatalf("a bridge outside its grammar was compared beside the schema's refusal: %v", issues)
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
	issues := issuesAt(Validate(last, api.NewCatalog([]api.Object{last, host, machine("one", "last", false), machine("two", "last", false)})), "$.spec.libvirt.bmcEmulationDefaults.port")
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

func TestAnInfraProviderNameFitsItsHostBlock(t *testing.T) {
	host := topologyHost("host")
	validate := func(name string) []api.Issue {
		provider := topologyProvider(name, "host", "8000", topologyProfile())
		return Validate(provider, api.NewCatalog([]api.Object{provider, host}))
	}
	if issues := validate(strings.Repeat("a", ProviderNameLimit)); len(issues) != 0 {
		t.Fatalf("a %d-byte name was refused: %v", ProviderNameLimit, issues)
	}
	name := strings.Repeat("a", ProviderNameLimit+1)
	issues := validate(name)
	if len(issues) != 1 || issues[0].Code != "api.value" || issues[0].Field != "$.metadata.name" ||
		!strings.Contains(issues[0].Message, "at most 48 bytes, because its host block substrate-host-<name> is a 63-byte identity") ||
		issues[0].Remediation != "rename InfraProvider/"+name+" to at most 48 bytes, with every providerRef that names it" {
		t.Fatalf("a %d-byte name: %v", ProviderNameLimit+1, issues)
	}
	if issues := validate(strings.Repeat("a", 70)); len(issues) != 0 {
		t.Fatalf("a name outside the label grammar got a per-kind diagnostic beside the compiler's: %v", issues)
	}
}

func TestMachineSpecStatesTheTopologyBounds(t *testing.T) {
	spec, err := os.ReadFile("../../specs/api/machines.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, phrase := range []string{
		fmt.Sprintf("A libvirt profile declares at most %d data disks", MaxDataDisks),
		fmt.Sprintf("An `InfraProvider` name is at most %d bytes", ProviderNameLimit),
		fmt.Sprintf("A `Machine` name is at most %d bytes", MachineNameLimit),
	} {
		if count := strings.Count(string(spec), phrase); count != 1 {
			t.Errorf("specs/api/machines.md states %q %d times, want once", phrase, count)
		}
	}
}
