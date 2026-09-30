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
// name it: one unicast address, IPv6 included, in the spelling normalization
// gives it. An absent address is the schema's to refuse, so this rule says
// nothing about it.
func TestAnEmulatedBMCListensOnOneNameableUnicastAddress(t *testing.T) {
	const field = "$.spec.libvirt.bmcEmulationDefaults.bindAddress"
	host := obj(api.Machine, "host", m("capabilities", api.StringList("libvirt")))
	provider := func(defaults api.Value) api.Object {
		return obj(api.InfraProvider, "lab", m("libvirt", m("machineRef", "host", "uri", "qemu:///system", "bmcEmulationDefaults", defaults)))
	}
	for address, nameable := range map[string]bool{
		"0.0.0.0": false, "::": false, "::0": false, "0:0:0:0:0:0:0:0": false, "::ffff:0.0.0.0": false,
		"::ffff:192.0.2.1": false, "fe80::1": false, "fe80::1%eth0": false, "fd00::1%eth0": false,
		"ff02::1": false, "224.0.0.1": false, "255.255.255.255": false,
		"192.0.2.1": true, "127.0.0.1": true, "169.254.1.1": true,
		"2001:db8::1": true, "fd00::1": true, "::1": true, "2001:DB8:0::1": true,
	} {
		t.Run(address, func(t *testing.T) {
			o, _ := Normalize(provider(m("port", api.IntegerValue("8000"), "bindAddress", address, "auth", m("credentialsRef", "bmc"))), api.Catalog{})
			issues := Validate(o, api.NewCatalog([]api.Object{o, host}))
			refused := len(issues) == 1 && issues[0].Field == field && issues[0].Code == "api.invariant"
			if nameable && len(issues) != 0 || !nameable && !refused {
				t.Fatalf("nameable = %v, issues = %v", nameable, issues)
			}
		})
	}
	normalized, _ := Normalize(provider(m("port", api.IntegerValue("8000"), "bindAddress", "2001:DB8:0::1", "auth", m("credentialsRef", "bmc"))), api.Catalog{})
	if address := normalized.Spec().Get("libvirt", "bmcEmulationDefaults", "bindAddress").Text(); address != "2001:db8::1" {
		t.Fatalf("normalized address = %q", address)
	}
	if NameableListener("2001:DB8:0::1") {
		t.Fatal("a second spelling that bypassed normalization was admitted")
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

// fieldsOf lists the fields a set of issues is reported at.
func fieldsOf(issues []api.Issue) []string {
	fields := []string{}
	for _, issue := range issues {
		fields = append(fields, issue.Field)
	}
	return fields
}

// A bundle is an anchor only for a verified leg, so one beside verification
// turned off refuses wherever it meets it: authored on a provider, authored on
// a Machine, and on the effective Machine whose opt-out it inherited.
func TestTrustBundleRequiresVerification(t *testing.T) {
	bundled := m("credentialsRef", "bmc", "tls", m("verify", false, "trustBundleRef", "bmc-ca"))
	provider := obj(api.InfraProvider, "floor", m("baremetal", m("defaults", m("bmc", bundled))))
	want := "$.spec.baremetal.defaults.bmc.tls.trustBundleRef"
	for name, issues := range map[string][]api.Issue{
		"authored provider":  ValidateAuthored(provider, api.Catalog{}),
		"effective provider": Validate(provider, api.Catalog{}),
	} {
		if fields := fieldsOf(issues); len(fields) != 1 || fields[0] != want {
			t.Fatalf("%s: issues at %v, want %s", name, fields, want)
		}
		if issues[0].Code != "api.invariant" || issues[0].Remediation != "set tls.verify: true where the bundle is declared, or remove tls.trustBundleRef" ||
			issues[0].Message != "a controller trust bundle requires verification, and tls.verify is false here, whether authored or inherited" {
			t.Fatalf("%s: issue = %+v", name, issues[0])
		}
	}
	machine := m("address", "https://bmc.example.test/redfish/v1/Systems/1", "tls", bundled.Get("tls"))
	for name, authored := range map[string]bool{"authored Machine": true, "effective Machine under an inherited opt-out": false} {
		if fields := fieldsOf(ValidateBMCDefaults(machine, "$.spec.hardware.management.bmc", authored)); len(fields) != 1 || fields[0] != "$.spec.hardware.management.bmc.tls.trustBundleRef" {
			t.Fatalf("%s: issues at %v", name, fields)
		}
	}
	for _, tls := range []api.Value{m("verify", true, "trustBundleRef", "bmc-ca"), m("trustBundleRef", "bmc-ca"), m("verify", false)} {
		if issues := ValidateBMCDefaults(m("tls", tls), "$.spec.hardware.management.bmc", true); len(issues) != 0 {
			t.Fatalf("tls %v refused: %v", tls, issues)
		}
	}
}

// Importing the server's certificate is the virtual-media trust a controller
// gets unless its Machine declares another, in normalization, in admission and
// in what a target freezes.
func TestVirtualMediaTrustDefaultsToImportCertificate(t *testing.T) {
	tls := NormalizeBMCDefaults(m()).Get("virtualMedia", "tls")
	if tls.Get("trust").Text() != TrustImportCertificate || !tls.Has("removeCertificateAfterBoot") || tls.Get("removeCertificateAfterBoot").Bool() || tls.Has("restoreVerificationAfterBoot") {
		t.Fatalf("normalized trust = %v", tls)
	}
	if issues := ValidateBMCDefaults(m("virtualMedia", m("tls", m("removeCertificateAfterBoot", true))), "$.bmc", true); len(issues) != 0 {
		t.Fatalf("removing a certificate under the default trust refused: %v", issues)
	}
	if fields := fieldsOf(ValidateBMCDefaults(m("virtualMedia", m("tls", m("restoreVerificationAfterBoot", true))), "$.bmc", true)); len(fields) != 1 || fields[0] != "$.bmc.virtualMedia.tls.restoreVerificationAfterBoot" {
		t.Fatalf("restoring verification under the default trust: issues at %v", fields)
	}
	if media := virtualMediaTrust(m()); media != (VirtualMedia{Trust: TrustImportCertificate}) {
		t.Fatalf("frozen trust = %+v", media)
	}
}

// disable-verification is a per-Machine exception, so a provider default that
// would hand it to every Machine the provider hosts refuses, whether authored
// on the provider or offered as an InfraProvider kind default. On a Machine it
// stays admitted.
func TestProviderVirtualMediaDisableVerificationRefuses(t *testing.T) {
	provider := obj(api.InfraProvider, "floor", m("baremetal", m("defaults", m("bmc",
		m("credentialsRef", "bmc", "virtualMedia", m("tls", m("trust", TrustDisableVerification)))))))
	want := "$.spec.baremetal.defaults.bmc.virtualMedia.tls.trust"
	for name, issues := range map[string][]api.Issue{
		"authored":     ValidateAuthored(provider, api.Catalog{}),
		"kind default": ValidatePartial(provider, api.Catalog{}),
	} {
		if fields := fieldsOf(issues); len(fields) != 1 || fields[0] != want {
			t.Fatalf("%s: issues at %v, want %s", name, fields, want)
		}
		if issues[0].Message != "disable-verification is a per-Machine exception and never a provider default" ||
			issues[0].Remediation != "declare hardware.management.bmc.virtualMedia.tls.trust: disable-verification on each Machine that needs it" {
			t.Fatalf("%s: issue = %+v", name, issues[0])
		}
	}
	for _, trust := range []string{TrustImportCertificate, TrustEstablished} {
		allowed := obj(api.InfraProvider, "floor", m("baremetal", m("defaults", m("bmc", m("virtualMedia", m("tls", m("trust", trust)))))))
		if issues := ValidateAuthored(allowed, api.Catalog{}); len(issues) != 0 {
			t.Fatalf("a provider default of %s refused: %v", trust, issues)
		}
	}
	machine := m("virtualMedia", m("tls", m("trust", TrustDisableVerification, "restoreVerificationAfterBoot", false)))
	if issues := ValidateBMCDefaults(machine, "$.spec.hardware.management.bmc", true); len(issues) != 0 {
		t.Fatalf("a Machine's own exception refused: %v", issues)
	}
}

// Each settle option is frozen only under the one trust that acts on it:
// restoring verification only after disabling it, true unless the Machine
// declares otherwise, and removing a certificate only after importing it.
func TestRestoreVerificationFreezesOnlyUnderDisableVerification(t *testing.T) {
	for name, test := range map[string]struct {
		tls  api.Value
		want VirtualMedia
	}{
		"disable-verification":             {m("trust", TrustDisableVerification), VirtualMedia{Trust: TrustDisableVerification, RestoreVerification: true}},
		"disable-verification, kept off":   {m("trust", TrustDisableVerification, "restoreVerificationAfterBoot", false), VirtualMedia{Trust: TrustDisableVerification}},
		"import-certificate":               {m("trust", TrustImportCertificate), VirtualMedia{Trust: TrustImportCertificate}},
		"import-certificate, then removed": {m("trust", TrustImportCertificate, "removeCertificateAfterBoot", true), VirtualMedia{Trust: TrustImportCertificate, RemoveCertificate: true}},
		"established":                      {m("trust", TrustEstablished), VirtualMedia{Trust: TrustEstablished}},
		"the default":                      {m(), VirtualMedia{Trust: TrustImportCertificate}},
	} {
		if media := virtualMediaTrust(test.tls); media != test.want {
			t.Fatalf("%s: frozen %+v, want %+v", name, media, test.want)
		}
	}
}
