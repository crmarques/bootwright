package containercluster

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestClusterNameFitsInstallationBlock(t *testing.T) {
	base, c := fixture("libvirt", 1, false)
	for _, size := range []int{47, 48, 49, 63} {
		o := obj(api.ContainerCluster, strings.Repeat("a", size), base.Spec())
		effective, _ := Normalize(o, c)
		for _, issues := range [][]api.Issue{ValidatePartial(o, c), Validate(effective, c)} {
			found := false
			for _, issue := range issues {
				if issue.Field == "$.metadata.name" {
					found = true
					if issue.Code != "api.invariant" || !strings.Contains(issue.Message, "47") || !strings.Contains(issue.Remediation, "metadata.name") {
						t.Fatal(issue)
					}
				}
			}
			if found != (size > 47) {
				t.Fatalf("%d-byte cluster name: %v", size, issues)
			}
		}
	}
}

func TestReleaseImageNormalizationChangesOnlyDigest(t *testing.T) {
	for _, image := range []string{"registry.example.test/release:VersionA", "registry.example.test/release@SHA256:" + strings.Repeat("AB", 32)} {
		o := obj(api.ContainerCluster, "cluster", m("distribution", m("release", m("image", image))))
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
		{"platform union", "install.platform", "spec.install.platform.type", m("type", "none", "external", m())},
		{"connected boot artifacts", "install.agent.bootArtifacts", "spec.install.agent.bootArtifacts", m()},
		{"SSH choice", "install.nodeSSH", "spec.install.nodeSSH", m("keyPairRef", "pair", "publicKeyRef", "public")},
		{"SSH public key", "install.nodeSSH", "spec.install.nodeSSH.publicKeyRef", m("privateKeyRef", "private")},
		{"labels", "nodes", "nodes[0].labels", list(m("role", "master", "labels", m("bad label", "value")))},
		{"taints", "nodes", "nodes[0].taints[0]", list(m("role", "master", "taints", list(m("key", "bad key"))))},
		{"no master", "nodes", "spec.nodes", list(m("role", "worker"))},
		{"encryption roles empty", "security.diskEncryption.roles", "spec.security.diskEncryption.roles", list()},
		{"encryption undeclared role", "security.diskEncryption.roles", "spec.security.diskEncryption.roles", api.StringList("infra")},
		{"endpoint wrong arm", "install.endpoints.api.source", "source.loadBalancerRef", m("type", "external", "loadBalancerRef", "lb")},
		{"single node VIP", "install.endpoints.api.source.type", "source.type", api.StringValue("openshift")},
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
