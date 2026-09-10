package containercluster

import (
	"fmt"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
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

func fixture(variant string, count int, ipv6 bool) (api.Object, api.Catalog) {
	cidr, ip := "192.0.2.0/24", "192.0.2."
	if ipv6 {
		cidr, ip = "2001:db8::/64", "2001:db8::"
	}
	objects := []api.Object{obj(api.Environment, "env", m("domains", m("base", "example.test", "containerClusters", "clusters.example.test"))), obj(api.InfraProvider, "provider", m(variant, m()))}
	nodes := []api.Value{}
	for i := range count {
		name := fmt.Sprintf("node-%d", i)
		address := fmt.Sprintf("%s%d/24", ip, i+10)
		if ipv6 {
			address = fmt.Sprintf("%s%d/64", ip, i+10)
		}
		machine := obj(api.Machine, name, m("capabilities", api.StringList("openshift-node"), "os", m("provided", false), "substrate", m("providerRef", "provider"), "hardware", m("nics", list(m("name", "eth0", "macAddress", fmt.Sprintf("02:00:00:00:00:%02x", i+1)))), "network", m("inline", m("machineNetwork", list(m("cidr", cidr)), "nmstate", m("interfaces", list(m("name", "eth0", "type", "ethernet")))), "addresses", list(m("name", "primary", "address", address, "interface", "eth0")))))
		objects = append(objects, machine)
		role := "worker"
		if i == 0 {
			role = "master"
		}
		nodes = append(nodes, m("name", name, "role", role, "machineRef", name))
	}
	apiIP, ingressIP := "192.0.2.2", "192.0.2.3"
	if ipv6 {
		apiIP, ingressIP = "2001:db8::2", "2001:db8::3"
	}
	endpoint := func(address string) api.Value {
		if count == 1 {
			return m("source", m("type", "node"))
		}
		return m("address", address, "source", m("type", "external"))
	}
	cluster := obj(api.ContainerCluster, "cluster", m("distribution", m("type", "openshift", "release", m("version", "4.99.1")), "install", m("method", "agent", "mode", "connected", "endpoints", m("api", endpoint(apiIP), "ingress", endpoint(ingressIP))), "nodes", list(nodes...)))
	return cluster, api.NewCatalog(objects)
}

func TestPlatformDerivationAndAuthoredExternalPreservation(t *testing.T) {
	for variant, want := range map[string]string{"baremetal": "baremetal", "libvirt": "baremetal", "vsphere": "vsphere", "kubevirt": "none"} {
		t.Run(variant, func(t *testing.T) {
			o, c := fixture(variant, 2, false)
			effective, issues := Normalize(o, c)
			if len(issues) > 0 {
				t.Fatal(issues)
			}
			if got := effective.Spec().Get("install", "platform", "type").Text(); got != want {
				t.Fatal(got)
			}
			if want == "baremetal" && effective.Spec().Get("install", "platform", "baremetal", "provisioningNetwork").Text() != "disabled" {
				t.Fatal("provisioning default missing")
			}
			o, c = fixture(variant, 1, false)
			effective, _ = Normalize(o, c)
			if effective.Spec().Get("install", "platform", "type").Text() != "none" {
				t.Fatal("single-node platform")
			}
		})
	}
	o, c := fixture("kubevirt", 2, false)
	external := m("type", "external", "external", m("providerName", "example", "nativeList", list(api.StringValue("untouched")), "threshold", api.NumberValue("1.5")))
	o = o.WithSpec(o.Spec().WithPath(external, "install", "platform"))
	effective, _ := Normalize(o, c)
	if !effective.Spec().Get("install", "platform").Equal(external) {
		t.Fatal("external native platform changed")
	}
	objects := c.Objects()
	peer, _ := c.Find(api.Machine, "node-1")
	peer = peer.WithSpec(peer.Spec().WithPath(api.StringValue("second"), "substrate", "providerRef"))
	for i, x := range objects {
		if x.Identity() == peer.Identity() {
			objects[i] = peer
		}
	}
	objects = append(objects, obj(api.InfraProvider, "second", m("vsphere", m())))
	o = o.WithSpec(o.Spec().With("install", o.Spec().Get("install").Without("platform")))
	effective, _ = Normalize(o, api.NewCatalog(objects))
	if effective.Spec().Has("install", "platform") {
		t.Fatal("mixed provider fallback")
	}
	if issues := Validate(effective, api.NewCatalog(objects)); !hasField(issues, "$.spec.install.platform") {
		t.Fatal(issues)
	}
}

func TestSingleNodeSourceDerivationAndAuthoredRestrictions(t *testing.T) {
	o, c := fixture("kubevirt", 1, false)
	effective, issues := Normalize(o, c)
	if len(issues) > 0 {
		t.Fatal(issues)
	}
	if issues = Validate(effective, c); len(issues) > 0 {
		t.Fatal(issues)
	}
	for _, slot := range endpointSlots {
		endpoint := effective.Spec().Get("install", "endpoints", slot)
		if endpoint.Get("address").Text() != "192.0.2.10" || endpoint.Get("source", "type").Text() != "node" {
			t.Fatal("derived node source missing", slot)
		}
	}
	if o.Spec().Get("install", "endpoints", "api").Has("address") {
		t.Fatal("authored state changed")
	}
	if issues := ValidateAuthored(effective, c); len(issues) != 3 {
		t.Fatal("inspection output passed authored-source checks", issues)
	}
	bad := o.WithSpec(o.Spec().WithPath(m(), "install", "endpoints", "api", "source"))
	bad, _ = Normalize(bad, c)
	if issues := Validate(bad, c); !hasField(issues, "$.spec.install.endpoints.api.source.type") {
		t.Fatal(issues)
	}
	if issues := ValidateAuthored(o.WithSpec(o.Spec().WithPath(api.StringValue("192.0.2.10"), "install", "endpoints", "api", "address")), c); len(issues) == 0 {
		t.Fatal("authored node address accepted")
	}
}

func TestAPIInternalCopiesOnlyAddressAndSource(t *testing.T) {
	o, c := fixture("vsphere", 2, false)
	apiEndpoint := o.Spec().Get("install", "endpoints", "api").With("dnsName", api.StringValue("api.example.test")).With("port", api.IntegerValue("6443")).With("scheme", api.StringValue("https")).With("prefixLength", api.IntegerValue("24")).With("interfaceNetworks", api.StringList("192.0.2.7/24"))
	o = o.WithSpec(o.Spec().WithPath(apiEndpoint, "install", "endpoints", "api"))
	effective, _ := Normalize(o, c)
	internal := effective.Spec().Get("install", "endpoints", "api-int")
	if !internal.Get("address").Equal(apiEndpoint.Get("address")) || !internal.Get("source").Equal(apiEndpoint.Get("source")) {
		t.Fatal("source/address copy")
	}
	for _, key := range []string{"dnsName", "port", "scheme", "prefixLength", "interfaceNetworks"} {
		if internal.Has(key) {
			t.Fatal("copied extra api endpoint field", key)
		}
	}
	if got := effective.Spec().Get("install", "endpoints", "api", "interfaceNetworks").Items()[0].Text(); got != "192.0.2.0/24" {
		t.Fatal(got)
	}
	explicit := m("address", "192.0.2.4", "source", m("type", "external"), "dnsName", "internal.example.test")
	o = o.WithSpec(o.Spec().WithPath(explicit, "install", "endpoints", "api-int"))
	effective, _ = Normalize(o, c)
	if !effective.Spec().Get("install", "endpoints", "api-int").Equal(explicit) {
		t.Fatal("authored internal endpoint replaced")
	}
}

func TestLoadBalancerSelectionAndVIPCollisions(t *testing.T) {
	o, c := fixture("vsphere", 2, false)
	lb := obj(api.LoadBalancer, "lb", m("management", "external", "bindAddresses", list(m("name", "api", "address", "192.0.2.2"), m("name", "ingress", "address", "192.0.2.3"))))
	c = api.NewCatalog(append(c.Objects(), lb))
	o = o.WithSpec(o.Spec().WithPath(m("source", m("type", "loadBalancer", "loadBalancerRef", "lb", "bindAddressRef", "api")), "install", "endpoints", "api"))
	effective, _ := Normalize(o, c)
	if issues := Validate(effective, c); len(issues) > 0 {
		t.Fatal(issues)
	}
	if effective.Spec().Get("install", "endpoints", "api", "address").Text() != "192.0.2.2" {
		t.Fatal("load-balancer address missing")
	}
	bad := o.WithSpec(o.Spec().WithPath(m("type", "loadBalancer", "loadBalancerRef", "lb"), "install", "endpoints", "api", "source"))
	bad, _ = Normalize(bad, c)
	if issues := Validate(bad, c); !hasField(issues, "$.spec.install.endpoints.api.source.bindAddressRef") {
		t.Fatal(issues)
	}
	bad = effective.WithSpec(effective.Spec().WithPath(api.StringValue("192.0.2.10"), "install", "endpoints", "ingress", "address"))
	if issues := Validate(bad, c); !hasField(issues, "$.spec.install.endpoints.ingress.address") {
		t.Fatal("VIP collision accepted", issues)
	}
	bad = effective.WithSpec(effective.Spec().WithPath(api.StringValue("198.51.100.3"), "install", "endpoints", "ingress", "address"))
	if issues := Validate(bad, c); !hasField(issues, "$.spec.install.endpoints.ingress.address") {
		t.Fatal("out-of-network VIP accepted", issues)
	}
	missing := o.WithSpec(o.Spec().WithPath(api.StringValue("unknown"), "install", "endpoints", "api", "source", "loadBalancerRef"))
	if _, _, issues := endpointAddress(missing, missing.Spec().Get("install", "endpoints", "api"), c, "$.endpoint"); len(issues) != 0 {
		t.Fatal("unresolved component cascaded", issues)
	}
}

func TestNetworkDefaultsAndFamilyConstraints(t *testing.T) {
	o, c := fixture("kubevirt", 1, true)
	effective, _ := Normalize(o, c)
	if effective.Spec().Get("networking", "clusterNetwork").Items()[0].Get("cidr").Text() != "fd01::/48" || effective.Spec().Get("networking", "serviceNetwork").Items()[0].Text() != "fd02::/112" {
		t.Fatal("IPv6 defaults")
	}
	if issues := Validate(effective, c); len(issues) > 0 {
		t.Fatal(issues)
	}
	custom := m("clusterNetwork", list(m("cidr", "2001:db8:1::123/48", "hostPrefix", api.IntegerValue("64"))))
	o = o.WithSpec(o.Spec().With("networking", custom))
	effective, _ = Normalize(o, c)
	if effective.Spec().Get("networking", "clusterNetwork").Items()[0].Get("cidr").Text() != "2001:db8:1::/48" {
		t.Fatal("authored network not preserved/canonicalized")
	}
	bad := effective.WithSpec(effective.Spec().WithPath(api.StringList("172.30.0.0/16"), "networking", "serviceNetwork"))
	if issues := Validate(bad, c); !hasField(issues, "$.spec.networking") {
		t.Fatal("mixed family accepted", issues)
	}
	for _, prefix := range []string{"48", "129"} {
		bad := effective.WithSpec(effective.Spec().WithPath(list(m("cidr", "fd01::/48", "hostPrefix", api.IntegerValue(prefix))), "networking", "clusterNetwork"))
		if issues := Validate(bad, c); !hasField(issues, "$.spec.networking.clusterNetwork[0].hostPrefix") {
			t.Fatal(issues)
		}
	}
	missing := obj(api.ContainerCluster, "missing", m("nodes", list(m("name", "master", "role", "master", "machineRef", "missing")), "install", m("endpoints", m("api", m("source", m("type", "node")), "ingress", m("source", m("type", "node"))))))
	missing, _ = Normalize(missing, api.Catalog{})
	if missing.Spec().Get("networking", "serviceNetwork").Items()[0].Text() != "172.30.0.0/16" {
		t.Fatal("unresolved evidence did not use IPv4 default")
	}
}

func TestReleaseCredentialsSecurityAndPlacement(t *testing.T) {
	o, c := fixture("kubevirt", 2, false)
	o = o.WithSpec(o.Spec().WithPath(m("unlock", m("tpm2", m())), "security", "diskEncryption"))
	effective, _ := Normalize(o, c)
	if effective.Spec().Get("distribution", "release", "channel").Text() != "stable-4.99" || effective.Spec().Get("install", "pullSecretRef").Text() != "openshift-pull-secret" || effective.Spec().Get("install", "nodeSSH", "keyPairRef").Text() != "cluster-cluster-admin-ssh-key" {
		t.Fatal("credential/release defaults missing")
	}
	if strings.Join(effective.Spec().Get("security", "diskEncryption", "roles").Strings(), ",") != "master,worker" {
		t.Fatal("represented encryption roles missing")
	}
	if effective.Spec().Get("nodes").Items()[0].Get("fqdn").Text() != "node-0.cluster.clusters.example.test" {
		t.Fatal("node DNS composition")
	}
	badCases := map[string]api.Value{
		"FIPS OKD":                 effective.Spec().WithPath(api.StringValue("okd"), "distribution", "type").WithPath(api.BoolValue(true), "security", "fips", "enabled"),
		"unrepresented role":       effective.Spec().WithPath(api.StringList("infra"), "security", "diskEncryption", "roles"),
		"mixed SSH":                effective.Spec().WithPath(api.StringValue("public"), "install", "nodeSSH", "publicKeyRef"),
		"private SSH only":         effective.Spec().WithPath(m("privateKeyRef", "private"), "install", "nodeSSH"),
		"internal certificate":     effective.Spec().WithPath(list(m("names", api.StringList("api-int.cluster.clusters.example.test"), "secretRef", "certificate")), "install", "servingCertificates", "apiServer", "namedCertificates"),
		"connected boot selection": effective.Spec().WithPath(m("artifactServerEndpoint", m("endpointRef", "media")), "install", "agent", "bootArtifacts"),
	}
	for name, spec := range badCases {
		t.Run(name, func(t *testing.T) {
			if issues := Validate(effective.WithSpec(spec), c); len(issues) == 0 {
				t.Fatal("invalid declaration admitted")
			}
		})
	}
	image := o.WithSpec(o.Spec().WithPath(m("image", "quay.io/example/release:4.99.1"), "distribution", "release"))
	image, _ = Normalize(image, c)
	if image.Spec().Has("distribution", "release", "channel") {
		t.Fatal("image acquired channel")
	}
}

func TestKubernetesLabelsAndRepeatedTaints(t *testing.T) {
	o, c := fixture("kubevirt", 1, false)
	nodes := o.Spec().Get("nodes").Items()
	taint := m("key", "node-role.kubernetes.io/infra", "effect", "NoSchedule")
	nodes[0] = nodes[0].With("labels", m("example.test/Zone_Name", "rack.A", "plain", "")).With("taints", list(taint, taint))
	o = o.WithSpec(o.Spec().With("nodes", list(nodes...)))
	effective, _ := Normalize(o, c)
	if issues := Validate(effective, c); len(issues) > 0 {
		t.Fatal(issues)
	}
	if effective.Spec().Get("nodes").Items()[0].Get("taints").Len() != 2 {
		t.Fatal("authored taints deduplicated")
	}
	for _, key := range []string{"bad/key/extra", "Upper.Example/key", "bad space", "/empty", strings.Repeat("a", 64)} {
		nodes[0] = nodes[0].With("labels", m(key, "value"))
		if issues := ValidatePartial(o.WithSpec(o.Spec().With("nodes", list(nodes...))), c); len(issues) == 0 {
			t.Fatal("invalid label accepted", key)
		}
	}
	if !labelKey("kubernetes.io/role") || !labelValue("") || labelValue("bad value") {
		t.Fatal("label grammar")
	}
}

func TestDisconnectedAndBaremetalArtifactSelections(t *testing.T) {
	o, c := fixture("baremetal", 2, false)
	o, _ = Normalize(o, c)
	if issues := Validate(o, c); !hasField(issues, "$.spec.install.agent.redfishVirtualMedia.artifactServerEndpoint") {
		t.Fatal(issues)
	}
	o = o.WithSpec(o.Spec().WithPath(api.StringValue("disconnected"), "install", "mode"))
	issues := Validate(o, c)
	if !hasField(issues, "$.spec.install.agent.bootArtifacts.artifactServerEndpoint") || !hasField(issues, "$.spec.install.registries.mirror") {
		t.Fatal(issues)
	}
	server := obj(api.ArtifactServer, "artifacts", m("management", "managed", "machineRef", "services", "endpoints", list(m("name", "media"))))
	registry := obj(api.Registry, "mirror", m("management", "external", "url", "mirror.example.test", "trustBundleRef", "mirror-ca"))
	c = api.NewCatalog(append(c.Objects(), server, registry))
	o = o.WithSpec(o.Spec().WithPath(m("registryRef", "mirror"), "install", "registries", "mirror"))
	for _, consumer := range []string{"redfishVirtualMedia", "bootArtifacts"} {
		o = o.WithSpec(o.Spec().WithPath(m("serverRef", "artifacts", "endpointRef", "media"), "install", "agent", consumer, "artifactServerEndpoint"))
	}
	o, _ = Normalize(o, c)
	if issues := Validate(o, c); len(issues) > 0 {
		t.Fatal(issues)
	}
	missing := o.WithSpec(o.Spec().WithPath(m("endpointRef", "media"), "install", "agent", "bootArtifacts", "artifactServerEndpoint"))
	missing, _ = Normalize(missing, c)
	if missing.Spec().Has("install", "agent", "bootArtifacts", "artifactServerEndpoint", "serverRef") {
		t.Fatal("artifact server inferred from catalog")
	}
	if issues := Validate(missing, c); !hasField(issues, "$.spec.install.agent.bootArtifacts.artifactServerEndpoint.serverRef") {
		t.Fatal("missing explicit artifact reference admitted", issues)
	}
	for _, bad := range []api.Object{
		registry.WithSpec(registry.Spec().Without("trustBundleRef")),
		server.WithSpec(server.Spec().With("machineRef", api.StringValue("node-0"))),
	} {
		objects := c.Objects()
		for i, object := range objects {
			if object.Identity() == bad.Identity() {
				objects[i] = bad
			}
		}
		if issues := Validate(o, api.NewCatalog(objects)); len(issues) == 0 {
			t.Fatal("unsafe disconnected installation admitted")
		}
	}
}

func TestMissingPrerequisitesSuppressSecondaryErrors(t *testing.T) {
	o, _ := fixture("vsphere", 2, false)
	o, _ = Normalize(o, api.Catalog{})
	for _, issue := range Validate(o, api.Catalog{}) {
		if issue.Field == "$.spec.install.platform" || strings.Contains(issue.Message, "Machine requires") {
			t.Fatal("missing prerequisites cascaded", issue)
		}
	}
	fragment := obj(api.ContainerCluster, "defaults", m("install", m("nodeSSH", m("privateKeyRef", "private"), "endpoints", m("api", m("source", m("type", "loadBalancer"))))))
	if issues := ValidatePartial(fragment, api.Catalog{}); len(issues) != 0 {
		t.Fatal("partial required fields cascaded", issues)
	}
}

func hasField(issues []api.Issue, field string) bool {
	for _, issue := range issues {
		if issue.Field == field {
			return true
		}
	}
	return false
}
