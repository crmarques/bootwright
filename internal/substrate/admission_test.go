package substrate

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"testing"
)

func m(kv ...any) api.Value {
	fields := []api.FieldValue{}
	for i := 0; i < len(kv); i += 2 {
		var v api.Value
		switch x := kv[i+1].(type) {
		case api.Value:
			v = x
		case string:
			v = api.StringValue(x)
		case bool:
			v = api.BoolValue(x)
		}
		fields = append(fields, api.FieldValue{Name: kv[i].(string), Value: v})
	}
	return api.MapValue(fields...)
}
func obj(kind api.Kind, name string, spec api.Value) api.Object {
	return api.NewObject(kind, name, m(), spec)
}
func list(v ...api.Value) api.Value { return api.ListValue(v...) }
func TestProviderVariantsAndNormalization(t *testing.T) {
	bmc := m("enabled", true, "port", api.IntegerValue("8000"), "bindAddress", "192.0.2.1", "auth", m("credentialsRef", "bmc"))
	domain := m("name", "zone-a", "server", "vcenter.example.test", "region", "region", "zone", "zone", "topology", m("datacenter", "dc", "computeCluster", "cluster", "datastore", "store", "networks", api.StringList("network")))
	providers := map[string]api.Value{
		"baremetal": m("baremetal", m("defaults", m("bmc", m("credentialsRef", "bmc")))),
		"libvirt":   m("libvirt", m("machineRef", "host", "uri", "qemu:///system", "bmcEmulationDefaults", bmc), "networkAttachments", list(m("name", "net", "libvirt", m("bridge", "virbr-lab", "management", "managed", "address", "192.0.2.1/24")))),
		"vsphere":   m("vsphere", m("vcenters", list(m("server", "vcenter.example.test", "datacenters", api.StringList("dc"), "credentialsRef", "vcenter")), "failureDomains", list(domain), "isoStaging", m("folder", "media"), "machineProfiles", list(m("name", "small", "cpu", api.IntegerValue("2"), "memoryMiB", api.IntegerValue("4096"), "diskGiB", api.IntegerValue("20"))))),
		"kubevirt":  m("kubevirt", m("kubeconfigRef", "host-access", "namespace", "workloads"), "networkAttachments", list(m("name", "net", "kubevirt", m("networkRef", m("kind", "UserDefinedNetwork", "name", "net"))))),
	}
	host := obj(api.Machine, "host", m("capabilities", api.StringList("libvirt")))
	for variant, spec := range providers {
		t.Run(variant, func(t *testing.T) {
			o := obj(api.InfraProvider, variant, spec)
			o, issues := Normalize(o, api.Catalog{})
			if len(issues) > 0 {
				t.Fatal(issues)
			}
			if issues = Validate(o, api.NewCatalog([]api.Object{host})); len(issues) > 0 {
				t.Fatal(issues)
			}
			switch variant {
			case "baremetal":
				if !o.Spec().Get(variant, "defaults", "bmc", "tls", "verify").Bool() {
					t.Fatal("TLS verification missing")
				}
			case "libvirt":
				if o.Spec().Get("networkAttachments").Items()[0].Get("libvirt", "forward").Text() != "nat" {
					t.Fatal("managed network forward default")
				}
			case "vsphere":
				if o.Spec().Get(variant, "machineProfiles").Items()[0].Get("failureDomainRef").Text() != "zone-a" {
					t.Fatal("sole failure domain")
				}
			case "kubevirt":
				ref := o.Spec().Get("networkAttachments").Items()[0].Get("kubevirt", "networkRef")
				if ref.Get("namespace").Text() != "workloads" || ref.Get("apiGroup").Text() != "k8s.ovn.org" {
					t.Fatal("native network defaults")
				}
			}
		})
	}
}
func TestProviderContradictions(t *testing.T) {
	cases := map[string]api.Value{
		"disabled emulation":      m("libvirt", m("bmcEmulationDefaults", m("enabled", false))),
		"managed without address": m("libvirt", m("bmcEmulationDefaults", m("port", api.IntegerValue("8000"))), "networkAttachments", list(m("name", "net", "libvirt", m("bridge", "br0", "management", "managed")))),
		"external with address":   m("libvirt", m("bmcEmulationDefaults", m("port", api.IntegerValue("8000"))), "networkAttachments", list(m("name", "net", "libvirt", m("bridge", "br0", "address", "192.0.2.1/24")))),
		"network address":         m("libvirt", m("bmcEmulationDefaults", m("port", api.IntegerValue("8000"))), "networkAttachments", list(m("name", "net", "libvirt", m("bridge", "br0", "management", "managed", "address", "192.0.2.0/24")))),
		"wrong attachment":        m("baremetal", m(), "networkAttachments", list(m("name", "net", "libvirt", m("bridge", "br0")))),
		"cluster namespace":       m("kubevirt", m("namespace", "workloads"), "networkAttachments", list(m("name", "net", "kubevirt", m("networkRef", m("name", "net", "kind", "ClusterUserDefinedNetwork", "apiGroup", "k8s.ovn.org", "namespace", "workloads"))))),
		"unknown group":           m("kubevirt", m("namespace", "workloads"), "networkAttachments", list(m("name", "net", "kubevirt", m("networkRef", m("name", "net", "kind", "CustomNetwork"))))),
	}
	for name, spec := range cases {
		t.Run(name, func(t *testing.T) {
			if issues := Validate(obj(api.InfraProvider, "provider", spec), api.Catalog{}); len(issues) == 0 {
				t.Fatal("invalid provider admitted")
			}
		})
	}
	if issues := ValidateAuthored(obj(api.InfraProvider, "provider", m("vsphere", m("isoStaging", m()))), api.Catalog{}); len(issues) == 0 {
		t.Fatal("empty staging admitted")
	}
}
func TestLibvirtBMCPortRangesDoNotOverlapOnOneHost(t *testing.T) {
	provider := func(name, port string) api.Object {
		return obj(api.InfraProvider, name, m("libvirt", m("machineRef", "host", "uri", "qemu:///system", "bmcEmulationDefaults", m("port", api.IntegerValue(port), "bindAddress", "192.0.2.1", "auth", m("credentialsRef", "bmc")))))
	}
	machine := func(name, provider string) api.Object {
		return obj(api.Machine, name, m("substrate", m("providerRef", provider)))
	}
	host := obj(api.Machine, "host", m("capabilities", api.StringList("libvirt")))
	a, b := provider("a", "8000"), provider("b", "8001")
	if issues := Validate(a, api.NewCatalog([]api.Object{a, b, host, machine("one", "a")})); len(issues) != 0 {
		t.Fatal("adjacent ranges rejected", issues)
	}
	if issues := Validate(a, api.NewCatalog([]api.Object{a, b, host, machine("one", "a"), machine("two", "a")})); len(issues) == 0 {
		t.Fatal("overlapping ranges accepted")
	}
	c := provider("c", "65535")
	if issues := Validate(c, api.NewCatalog([]api.Object{c, host, machine("one", "c"), machine("two", "c")})); len(issues) == 0 {
		t.Fatal("range beyond the last port accepted")
	}
}

// An emulated BMC listens where every hosted Machine's controller endpoint can
// name it: one IPv4 unicast address. An absent address is the schema's to
// refuse, so this rule says nothing about it.
func TestAnEmulatedBMCListensOnOneNameableUnicastAddress(t *testing.T) {
	const field = "$.spec.libvirt.bmcEmulationDefaults.bindAddress"
	host := obj(api.Machine, "host", m("capabilities", api.StringList("libvirt")))
	provider := func(defaults api.Value) api.Object {
		return obj(api.InfraProvider, "lab", m("libvirt", m("machineRef", "host", "uri", "qemu:///system", "bmcEmulationDefaults", defaults)))
	}
	for address, nameable := range map[string]bool{
		"0.0.0.0": false, "::": false, "::0": false, "0:0:0:0:0:0:0:0": false, "::ffff:0.0.0.0": false,
		"::ffff:192.0.2.1": false, "2001:db8::1": false, "::1": false, "fe80::1": false, "ff02::1": false,
		"224.0.0.1": false, "255.255.255.255": false,
		"192.0.2.1": true, "127.0.0.1": true, "169.254.1.1": true,
	} {
		t.Run(address, func(t *testing.T) {
			o := provider(m("port", api.IntegerValue("8000"), "bindAddress", address, "auth", m("credentialsRef", "bmc")))
			issues := Validate(o, api.NewCatalog([]api.Object{o, host}))
			refused := len(issues) == 1 && issues[0].Field == field && issues[0].Code == "api.invariant"
			if nameable && len(issues) != 0 || !nameable && !refused {
				t.Fatalf("nameable = %v, issues = %v", nameable, issues)
			}
		})
	}
	absent := provider(m("port", api.IntegerValue("8000"), "auth", m("credentialsRef", "bmc")))
	if issues := Validate(absent, api.NewCatalog([]api.Object{absent, host})); len(issues) != 0 {
		t.Fatal("an absent address was judged beside the schema's refusal", issues)
	}
}

// A libvirt profile creates a machine of its own size, so a size that cannot
// is refused where it is declared instead of at planning. KubeVirt keeps the
// materialized zero.
func TestLibvirtProfilesRequirePositiveCapacity(t *testing.T) {
	host := obj(api.Machine, "host", m("capabilities", api.StringList("libvirt")))
	positive := m("name", "large", "cpu", api.IntegerValue("4"), "memoryMiB", api.IntegerValue("8192"), "diskGiB", api.IntegerValue("60"))
	libvirt := func(profiles ...api.Value) api.Object {
		return obj(api.InfraProvider, "lab", m("libvirt", m("machineRef", "host", "uri", "qemu:///system",
			"bmcEmulationDefaults", m("port", api.IntegerValue("8000"), "bindAddress", "192.0.2.1", "auth", m("credentialsRef", "bmc")),
			"machineProfiles", list(profiles...))))
	}
	if o := libvirt(positive); len(Validate(o, api.NewCatalog([]api.Object{o, host}))) != 0 {
		t.Fatal("a positive profile was refused")
	}
	for _, size := range []string{"cpu", "memoryMiB", "diskGiB"} {
		for _, value := range []string{"0", "-1"} {
			t.Run(size+"="+value, func(t *testing.T) {
				o := libvirt(positive, positive.With("name", api.StringValue("small")).With(size, api.IntegerValue(value)))
				issues := Validate(o, api.NewCatalog([]api.Object{o, host}))
				if len(issues) != 1 || issues[0].Field != "$.spec.libvirt.machineProfiles[1]."+size || issues[0].Message != "libvirt machine profiles require a positive capacity" {
					t.Fatal(issues)
				}
			})
		}
	}
	zero := m("name", "small", "cpu", api.IntegerValue("0"), "memoryMiB", api.IntegerValue("0"), "diskGiB", api.IntegerValue("0"))
	kubevirt := obj(api.InfraProvider, "cluster", m("kubevirt", m("kubeconfigRef", "host-access", "namespace", "workloads", "machineProfiles", list(zero))))
	if issues := Validate(kubevirt, api.NewCatalog([]api.Object{kubevirt})); len(issues) != 0 {
		t.Fatal("a KubeVirt profile was held to libvirt sizes", issues)
	}
}
