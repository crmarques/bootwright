package openshift

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestClusterNameFitsInstallationBlock(t *testing.T) {
	base, c := fixture("libvirt", 1, false)
	for _, size := range []int{47, 48, 63} {
		o := obj(api.OpenShiftCluster, strings.Repeat("a", size), base.Spec())
		effective, _ := Normalize(o, c)
		for _, issues := range [][]api.Issue{ValidatePartial(o, c), Validate(effective, c)} {
			for _, issue := range issues {
				if issue.Field == "$.metadata.name" {
					t.Fatalf("%d-byte cluster name refused: %v", size, issue)
				}
			}
		}
	}
}

func TestReleaseImageNormalizationChangesOnlyDigest(t *testing.T) {
	for _, image := range []string{"registry.example.test/release:VersionA", "registry.example.test/release@SHA256:" + strings.Repeat("AB", 32)} {
		o := obj(api.OpenShiftCluster, "cluster", m("distribution", m("release", m("image", image))))
		normalized, issues := Normalize(o, api.NewCatalog(nil))
		if len(issues) != 0 || normalized.Spec().Get("distribution", "release", "image").Text() != api.CanonicalImage(image) || o.Spec().Get("distribution", "release", "image").Text() != image {
			t.Fatal("digest normalization or authored preservation", normalized, issues)
		}
		again, _ := Normalize(normalized, api.NewCatalog(nil))
		if !again.Value().Equal(normalized.Value()) {
			t.Fatal("normalization is not idempotent")
		}
	}
}

func TestNodeResolverDeclarationConflict(t *testing.T) {
	for _, reference := range []bool{false, true} {
		for _, override := range []bool{false, true} {
			for _, selected := range []bool{false, true} {
				o, c := fixture("libvirt", 1, false)
				objects := c.Objects()
				for i, bound := range objects {
					if bound.Kind() != api.Machine {
						continue
					}
					config := bound.Spec().Get("network", "inline")
					if selected {
						config = config.With("dns", list(m("serverRef", "dns", "endpointRef", "ip")))
					}
					resolver := m("dns-resolver", m("config", m("server", api.StringList("192.0.2.53"))))
					if override {
						bound = bound.WithSpec(bound.Spec().WithPath(resolver, "network", "overrides"))
					} else {
						config = config.With("nmstate", config.Get("nmstate").With("dns-resolver", resolver.Get("dns-resolver")))
					}
					if reference {
						objects = append(objects, obj(api.NetworkConfig, "network", config))
						bound = bound.WithSpec(bound.Spec().With("network", bound.Spec().Get("network").Without("inline").With("configRef", api.StringValue("network"))))
					} else {
						bound = bound.WithSpec(bound.Spec().WithPath(config, "network", "inline"))
					}
					objects[i] = bound
				}
				c = api.NewCatalog(objects)
				o, _ = Normalize(o, c)
				found := false
				for _, issue := range Validate(o, c) {
					if issue.Field == "$.spec.nodes[0].machineRef" && strings.Contains(issue.Message, "dns-resolver.config.server") {
						found = true
						if issue.Code != "api.invariant" || !strings.Contains(issue.Message, "dns") || !strings.Contains(issue.Remediation, "keep one") {
							t.Fatal(issue)
						}
					}
				}
				if found != selected {
					t.Fatalf("reference=%v override=%v selected=%v conflict=%v", reference, override, selected, found)
				}
			}
		}
	}
}

func TestAdmissionRemediationNamesTheFieldToChange(t *testing.T) {
	o, c := fixture("kubevirt", 1, false)
	o, _ = Normalize(o, c)
	s := o.Spec()
	cases := []struct {
		name, path, remedy string
		value              api.Value
	}{
		{"missing release", "distribution.release", "spec.distribution.release.version", m()},
		{"OKD channel", "distribution", "spec.distribution.release.channel", m("type", "okd", "release", m("version", "4.21.15", "channel", "stable"))},
		{"connected boot artifacts", "install.agent.bootArtifacts", "spec.install.agent.bootArtifacts", m()},
		{"SSH choice", "install.nodeSSH", "spec.install.nodeSSH", m("keyPairRef", "pair", "publicKeyRef", "public")},
		{"SSH public key", "install.nodeSSH", "spec.install.nodeSSH.publicKeyRef", m("privateKeyRef", "private")},
		{"labels", "nodes", "nodes[0].labels", list(m("role", "controlPlane", "labels", m("bad label", "value")))},
		{"taints", "nodes", "nodes[0].taints[0]", list(m("role", "controlPlane", "taints", list(m("key", "bad key"))))},
		{"no master", "nodes", "spec.nodes", list(m("role", "worker"))},
		{"encryption roles empty", "security.diskEncryption.roles", "spec.security.diskEncryption.roles", list()},
		{"encryption undeclared role", "security.diskEncryption.roles", "spec.security.diskEncryption.roles", api.StringList("infra")},
		{"endpoint wrong arm", "install.endpoints.api.source", "source.loadBalancerRef", m("type", "external", "loadBalancerRef", "lb")},
		{"single node VIP", "install.endpoints.api.source.type", "source.type", api.StringValue("installer")},
		{"endpoint prefix family", "install.endpoints.api.prefixLength", "prefixLength", api.IntegerValue("64")},
		{"internal certificate", "install.servingCertificates.apiServer.namedCertificates", "namedCertificates[].names", list(m("names", api.StringList("api-int")))},
		{"digest source uniqueness", "install.registries.imageDigestSources", "imageDigestSources[1].source", list(m("source", "registry.example.test/release"), m("source", "registry.example.test/release"))},
		{"missing LoadBalancer", "install.endpoints.api.source", "source.loadBalancerRef", m("type", "loadBalancer")},
		{"IPv4 compatible endpoint", "install.endpoints.api", "spec.install.endpoints.api.address", m("source", m("type", "external"), "address", "::c000:201")},
		{"endpoint outside network", "install.endpoints.api", "spec.install.endpoints.api.address", m("source", m("type", "external"), "address", "198.51.100.1")},
		{"interface network", "install.endpoints.api.interfaceNetworks", "interfaceNetworks", api.StringList("198.51.100.0/24")},
		{"mixed family", "networking.serviceNetwork", "spec.networking", api.StringList("fd00::/112")},
		{"cluster host prefix", "networking.clusterNetwork", "clusterNetwork[0].hostPrefix", list(m("cidr", "10.128.0.0/14", "hostPrefix", api.IntegerValue("13")))},
		{"API endpoint required", "install.endpoints.api", "spec.install.endpoints.api.address", m("source", m("type", "external"))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			issues := Validate(o.WithSpec(s.WithPath(tc.value, strings.Split(tc.path, ".")...)), c)
			if len(issues) == 0 {
				t.Fatal("invalid declaration admitted")
			}
			found := false
			for _, issue := range issues {
				if strings.Contains(issue.Remediation, tc.remedy) {
					found = true
				}
				if strings.Contains(issue.Remediation, "make container-cluster intent consistent") {
					t.Fatal("generic remediation survived", issue)
				}
			}
			if !found {
				t.Fatalf("no remedy naming %s: %v", tc.remedy, issues)
			}
		})
	}
	contradictory := o.WithSpec(s.WithPath(api.StringValue("192.0.2.11"), "install", "endpoints", "api", "address"))
	for _, issue := range Validate(contradictory, c) {
		if strings.Contains(issue.Message, "effective endpoint address must agree") && issue.Remediation != "" {
			t.Fatal("internal effective-state contradiction supplied a speculative remedy", issue)
		}
	}
}

func rewrite(c api.Catalog, kind api.Kind, name string, change func(api.Value) api.Value) api.Catalog {
	objects := c.Objects()
	for i, bound := range objects {
		if bound.Kind() == kind && bound.Name() == name {
			objects[i] = bound.WithSpec(change(bound.Spec()))
		}
	}
	return api.NewCatalog(objects)
}

func TestAdmissionRequiresTheEffectiveAPIEndpointAddress(t *testing.T) {
	o, c := fixture("kubevirt", 1, false)
	o, _ = Normalize(o, c)
	if o.Spec().Get("install", "endpoints", "api", "address").Text() == "" {
		t.Fatal("fixture lost its resolved API address")
	}
	emptied := o.WithSpec(o.Spec().WithPath(m(), "install", "endpoints"))
	var matched []api.Issue
	for _, issue := range Validate(emptied, c) {
		if issue.Field == "$.spec.install.endpoints.api.address" {
			matched = append(matched, issue)
		}
	}
	if len(matched) != 1 || matched[0].Code != "api.invariant" || !strings.Contains(matched[0].Message, "effective API endpoint address") || !strings.Contains(matched[0].Remediation, "spec.install.endpoints.api.address") {
		t.Fatal(matched)
	}
}

func TestEveryAdmissionRuleRemediationNamesWhatToChange(t *testing.T) {
	type scenario struct {
		name, field, message string
		remedy               []string
		build                func() (api.Object, api.Catalog)
	}
	machineSpec := func(change func(api.Value) api.Value) func() (api.Object, api.Catalog) {
		return func() (api.Object, api.Catalog) {
			o, c := fixture("libvirt", 1, false)
			return o, rewrite(c, api.Machine, "node-0", change)
		}
	}
	install := func(variant string, path string, value api.Value) func() (api.Object, api.Catalog) {
		return func() (api.Object, api.Catalog) {
			o, c := fixture(variant, 1, false)
			o, _ = Normalize(o, c)
			return o.WithSpec(o.Spec().WithPath(value, strings.Split(path, ".")...)), c
		}
	}
	loadBalancer := func(ref string) func() (api.Object, api.Catalog) {
		return func() (api.Object, api.Catalog) {
			o, c := fixture("libvirt", 3, false)
			objects := append(c.Objects(), obj(api.LoadBalancer, "lb", m("management", "external", "bindAddresses", list(m("name", "api", "address", "192.0.2.2"), m("name", "ingress", "address", "192.0.2.3")))))
			source := m("type", "loadBalancer", "loadBalancerRef", "lb")
			if ref != "" {
				source = source.With("bindAddressRef", api.StringValue(ref))
			}
			o = o.WithSpec(o.Spec().WithPath(m("source", source), "install", "endpoints", "api"))
			return o, api.NewCatalog(objects)
		}
	}
	disconnected := func(change func(api.Object, api.Catalog) (api.Object, api.Catalog)) func() (api.Object, api.Catalog) {
		return func() (api.Object, api.Catalog) {
			o, c := fixture("libvirt", 1, false)
			o = o.WithSpec(o.Spec().WithPath(api.StringValue("disconnected"), "install", "mode"))
			return change(o, c)
		}
	}
	cases := []scenario{
		{"mixed provider platform", "$.spec.install.platform", "mixed provider types", []string{"spec.install.platform"}, func() (api.Object, api.Catalog) {
			o, c := fixture("libvirt", 2, false)
			c = rewrite(c, api.Machine, "node-1", func(s api.Value) api.Value { return s.WithPath(api.StringValue("other"), "substrate", "providerRef") })
			return o, api.NewCatalog(append(c.Objects(), obj(api.InfraProvider, "other", m("kubevirt", m()))))
		}},
		{"os provided", "$.spec.nodes[0].machineRef", "OS is not provided", []string{"spec.os.provided", "node-0"}, machineSpec(func(s api.Value) api.Value { return s.WithPath(api.BoolValue(true), "os", "provided") })},
		{"Machine networking", "$.spec.nodes[0].machineRef", "configured Machine networking", []string{"spec.network.configRef", "spec.network.inline", "node-0"}, machineSpec(func(s api.Value) api.Value { return s.Without("network") })},
		{"redfish artifact endpoint", "$.spec.install.agent.redfishVirtualMedia.artifactServerEndpoint", "virtual-media artifact endpoint", []string{"spec.install.agent.redfishVirtualMedia.artifactServerEndpoint"}, func() (api.Object, api.Catalog) { return fixture("baremetal", 1, false) }},
		{"boot artifacts endpoint", "$.spec.install.agent.bootArtifacts.artifactServerEndpoint", "boot-artifacts endpoint", []string{"spec.install.agent.bootArtifacts.artifactServerEndpoint"}, disconnected(func(o api.Object, c api.Catalog) (api.Object, api.Catalog) { return o, c })},
		{"registry mirror", "$.spec.install.registries.mirror", "registry mirror selection", []string{"spec.install.registries.mirror"}, disconnected(func(o api.Object, c api.Catalog) (api.Object, api.Catalog) { return o, c })},
		{"mirror trustBundleRef", "$.spec.install.registries.mirror.registryRef", "trustBundleRef", []string{"trustBundleRef", "spec.install.registries.mirror.registryRef"}, disconnected(func(o api.Object, c api.Catalog) (api.Object, api.Catalog) {
			o = o.WithSpec(o.Spec().WithPath(m("registryRef", "mirror"), "install", "registries", "mirror"))
			return o, api.NewCatalog(append(c.Objects(), obj(api.Registry, "mirror", m())))
		})},
		{"FIPS needs OpenShift", "$.spec.security.fips.enabled", "FIPS requires OpenShift", []string{"spec.security.fips.enabled", "spec.distribution.type"}, func() (api.Object, api.Catalog) {
			o, c := fixture("libvirt", 1, false)
			return o.WithSpec(o.Spec().With("distribution", m("type", "okd", "release", m("version", "4.21.15"))).With("security", m("fips", m("enabled", true)))), c
		}},
		{"prefixLength without address", "$.spec.install.endpoints.api.prefixLength", "prefix length requires an address", []string{"spec.install.endpoints.api.address", "spec.install.endpoints.api.prefixLength"}, install("kubevirt", "install.endpoints.api", m("source", m("type", "external"), "prefixLength", api.IntegerValue("24")))},
		{"ambiguous bind address", "$.spec.install.endpoints.api.source.bindAddressRef", "explicit selection", []string{"spec.install.endpoints.api.source.bindAddressRef", "bindAddresses[].name"}, loadBalancer("")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o, c := tc.build()
			found := false
			for _, issue := range Validate(o, c) {
				if issue.Field != tc.field || !strings.Contains(issue.Message, tc.message) {
					continue
				}
				found = true
				for _, want := range tc.remedy {
					if !strings.Contains(issue.Remediation, want) {
						t.Fatalf("remedy %q does not name %q", issue.Remediation, want)
					}
				}
			}
			if !found {
				t.Fatalf("rule %q did not fire at %s: %v", tc.message, tc.field, Validate(o, c))
			}
		})
	}
	o, c := loadBalancer("missing")()
	for _, issue := range Validate(o, c) {
		if issue.Field == "$.spec.install.endpoints.api.source.bindAddressRef" {
			t.Fatalf("an unknown bind address, which the shared reference check reports, was refused again: %v", issue)
		}
	}
}
