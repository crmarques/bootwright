package substrate

import (
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func issueLines(issues []api.Issue) []string {
	lines := make([]string, 0, len(issues))
	for _, found := range issues {
		lines = append(lines, found.Field+": "+found.Message)
	}
	return lines
}

func armDefects() map[Arm]api.Value {
	domains := list(
		m("name", "a", "server", "missing.example.test", "topology", m("datacenter", "dc")),
		m("name", "b", "server", "vcenter.example.test", "topology", m("datacenter", "elsewhere")),
	)
	vsphere := m("vsphere", m(
		"vcenters", list(m("server", "vcenter.example.test", "datacenters", api.StringList("dc"), "credentialsRef", "vcenter")),
		"failureDomains", domains,
		"machineProfiles", list(m("name", "small", "cpu", api.IntegerValue("0"), "memoryMiB", api.IntegerValue("4096"), "diskGiB", api.IntegerValue("20"))),
	), "segments", list(m("name", "plain", "vsphere", m()), m("name", "wrong", "kubevirt", m())))
	return map[Arm]api.Value{
		"baremetal": m("baremetal", m("defaults", m("bmc", m("address", "http://10.0.0.1",
			"tls", m("trustBundleRef", "ca"),
			"virtualMedia", m("tls", m("trust", "importCertificate", "restoreVerificationAfterBoot", true, "enableVerification", true))))),
			"segments", list(m("name", "n", "libvirt", m("bridge", "b")))),
		"libvirt": m("libvirt", m("machineRef", "host", "uri", "qemu:///system", "bmcEmulation", m("bindAddress", "0.0.0.0", "port", api.IntegerValue("65535"), "auth", m("credentialsRef", "bmc"))),
			"segments", list(m("name", "n", "baremetal", m()))),
		"vsphere": vsphere,
		"kubevirt": m("kubevirt", m("namespace", "workloads"), "segments", list(
			m("name", "a", "kubevirt", m("networkRef", m("kind", "ClusterUserDefinedNetwork", "apiGroup", "k8s.ovn.org", "namespace", "workloads", "name", "a"))),
			m("name", "b", "kubevirt", m("networkRef", m("kind", "CustomNetwork", "name", "b"))),
			m("name", "c", "libvirt", m("bridge", "br")))),
	}
}

func TestEachArmRefusesItsOwnDefectsInOrder(t *testing.T) {
	host := obj(api.Machine, "host", m())
	one := obj(api.Machine, "one", m("hardware", m("providerRef", "libvirt")))
	two := obj(api.Machine, "two", m("hardware", m("providerRef", "libvirt")))
	got := map[Arm][]string{}
	for arm, spec := range armDefects() {
		provider := obj(api.InfraProvider, string(arm), spec)
		got[arm] = issueLines(Validate(provider, api.NewCatalog([]api.Object{provider, host, one, two})))
	}
	want := armDefectIssues()
	for _, arm := range Arms() {
		if !slices.Equal(got[arm], want[arm]) {
			t.Errorf("%s issues = %q, want %q", arm, got[arm], want[arm])
		}
	}
}

func TestAProviderNormalizesOnlyItsOwnArm(t *testing.T) {
	normalized := func(spec api.Value) api.Object {
		o, issues := Normalize(obj(api.InfraProvider, "p", spec), api.Catalog{})
		if len(issues) != 0 {
			t.Fatal(issues)
		}
		return o
	}
	if got := normalized(m("libvirt", m("bmcEmulation", m("bindAddress", "2001:DB8::1")))).Spec().Get("libvirt", "bmcEmulation", "bindAddress").Text(); got != "2001:db8::1" {
		t.Errorf("libvirt bindAddress = %q", got)
	}
	bmc := normalized(m("baremetal", m("defaults", m("bmc", m("credentialsRef", "bmc")))))
	if tls := bmc.Spec().Get("baremetal", "defaults", "bmc", "virtualMedia", "tls"); tls.Get("trust").Text() != TrustImportCertificate || !tls.Get("enableVerification").Bool() {
		t.Errorf("baremetal media tls = %v", tls)
	}
	two := normalized(m("vsphere", m("failureDomains", list(m("name", "a"), m("name", "b")), "machineProfiles", list(m("name", "small")), "isoStaging", m())))
	if profile := two.Spec().Get("vsphere", "machineProfiles").Items()[0]; profile.Has("failureDomainRef") {
		t.Errorf("two failure domains defaulted a profile: %v", profile)
	}
	if staging := two.Spec().Get("vsphere", "isoStaging"); staging.Get("folder").Text() != "bootwright-vmedia" || staging.Has("datastore") {
		t.Errorf("two-domain staging = %v", staging)
	}
	one := normalized(m("vsphere", m("failureDomains", list(m("name", "a", "topology", m("datastore", "store"))), "machineProfiles", list(m("name", "small")), "isoStaging", m())))
	if profile := one.Spec().Get("vsphere", "machineProfiles").Items()[0]; profile.Get("failureDomainRef").Text() != "a" {
		t.Errorf("sole failure domain not defaulted: %v", profile)
	}
	if staging := one.Spec().Get("vsphere", "isoStaging"); staging.Get("datastore").Text() != "store" || staging.Get("folder").Text() != "bootwright-vmedia" {
		t.Errorf("sole-domain staging = %v", staging)
	}
	none, issues := Normalize(obj(api.InfraProvider, "p", m("segments", list())), api.Catalog{})
	if len(issues) != 0 || Variant(none) != "" {
		t.Errorf("an armless provider: %v %q", issues, Variant(none))
	}
}

func TestAPartialProviderRefusesASegmentOfAnotherArm(t *testing.T) {
	for arm, spec := range armDefects() {
		issues := ValidatePartial(obj(api.InfraProvider, string(arm), spec), api.Catalog{})
		mismatched := 0
		for _, found := range issues {
			if found.Field == "$.spec.segments" && found.Message == "segment arm must match its provider substrate" {
				mismatched++
			}
		}
		if mismatched == 0 {
			t.Errorf("%s: no segment of another arm refused: %q", arm, issueLines(issues))
		}
	}
}

func armDefectIssues() map[Arm][]string {
	return map[Arm][]string{
		"baremetal": {
			"$.spec.baremetal.defaults.bmc.tls.trustBundleRef: a trust bundle anchors only a TLS leg, and a plain-HTTP BMC address has none",
			"$.spec.baremetal.defaults.bmc.virtualMedia.tls.restoreVerificationAfterBoot: restoring verification requires disableVerification trust",
			"$.spec.baremetal.defaults.bmc.virtualMedia.tls.enableVerification: enabling media verification requires verified HTTPS BMC access and importCertificate or established trust",
			"$.spec.segments[0]: segment arm must match its provider substrate",
		},
		"libvirt": {
			"$.spec.libvirt.bmcEmulation.bindAddress: the emulated BMC listens on one unicast address its Machines' BMC endpoints can name: not unspecified, multicast or the IPv4 broadcast address, and not an IPv6 link-local, zoned or IPv4-mapped address",
			"$.spec.libvirt.bmcEmulation.port: the emulated BMC port range must end at or below 65535",
			"$.spec.segments[0]: segment arm must match its provider substrate",
		},
		"vsphere": {
			"$.spec.vsphere.failureDomains[0].server: failure-domain server must name a declared vCenter",
			"$.spec.vsphere.failureDomains[1].topology.datacenter: failure-domain datacenter must be declared on its vCenter",
			"$.spec.vsphere.machineProfiles[0].cpu: vSphere machine profiles require a positive capacity",
			"$.spec.vsphere.machineProfiles[0].failureDomainRef: multiple failure domains require an explicit profile selection",
			"$.spec.segments[0].vsphere.distributedSwitch: multiple failure domains require an explicit distributed switch",
			"$.spec.segments[0].untaggedNetworkRef: a vsphere segment delivers exactly one network to each NIC on it, and segment plain of InfraProvider/vsphere names none",
			"$.spec.segments[1]: segment arm must match its provider substrate",
			"$.spec.segments[1].vsphere.distributedSwitch: multiple failure domains require an explicit distributed switch",
			"$.spec.segments[1].untaggedNetworkRef: a vsphere segment delivers exactly one network to each NIC on it, and segment wrong of InfraProvider/vsphere names none",
		},
		"kubevirt": {
			"$.spec.segments[0].kubevirt.networkRef.namespace: cluster-scoped networks forbid a namespace",
			"$.spec.segments[0].untaggedNetworkRef: a kubevirt segment delivers exactly one network to each NIC on it, and segment a of InfraProvider/kubevirt names none",
			"$.spec.segments[1].kubevirt.networkRef.apiGroup: unknown external network kinds require an explicit API group",
			"$.spec.segments[1].untaggedNetworkRef: a kubevirt segment delivers exactly one network to each NIC on it, and segment b of InfraProvider/kubevirt names none",
			"$.spec.segments[2]: segment arm must match its provider substrate",
			"$.spec.segments[2].untaggedNetworkRef: a kubevirt segment delivers exactly one network to each NIC on it, and segment c of InfraProvider/kubevirt names none",
		},
	}
}
