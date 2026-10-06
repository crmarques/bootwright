package containercluster

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"go.yaml.in/yaml/v3"
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
		if i < 3 {
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
	cluster := obj(api.ContainerCluster, "cluster", m("distribution", m("type", "openshift", "release", m("version", "4.21.15")), "install", m("method", "agent", "mode", "connected", "endpoints", m("api", endpoint(apiIP), "ingress", endpoint(ingressIP))), "nodes", list(nodes...)))
	return cluster, api.NewCatalog(objects)
}

func TestPlatformDerivationAndAuthoredExternalPreservation(t *testing.T) {
	for variant, want := range map[string]string{"baremetal": "baremetal", "libvirt": "baremetal", "vsphere": "vsphere", "kubevirt": "none"} {
		t.Run(variant, func(t *testing.T) {
			o, c := fixture(variant, 4, false)
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
	o, c := fixture("kubevirt", 4, false)
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

// Resolution before boot proves only a slot that froze an address, so a slot
// the cluster or the operator answers owns one; a DNS name alone satisfies
// none, whatever the topology.
func TestAnEndpointWithoutAnAddressIsRefused(t *testing.T) {
	for name, nodes := range map[string]int{"single-node": 1, "multi-node platform none": 3} {
		t.Run(name, func(t *testing.T) {
			o, c := fixture("kubevirt", nodes, false)
			named := m("dnsName", "api.cluster.clusters.example.test", "source", m("type", "external"))
			for endpoint, admitted := range map[string]bool{"a DNS name alone": false, "an address": true} {
				slot := named
				if admitted {
					slot = named.With("address", api.StringValue("192.0.2.2"))
				}
				effective, _ := Normalize(o.WithSpec(o.Spec().WithPath(slot, "install", "endpoints", "api")), c)
				issues := Validate(effective, c)
				if refused := hasIssue(issues, "$.spec.install.endpoints.api.address", "requires an IP address"); refused == admitted || admitted && len(issues) != 0 {
					t.Fatal(endpoint, issues)
				}
			}
		})
	}
}

// An address in ::/96 freezes in netip's spelling, which glibc prints another
// way, so resolution before boot could never match it. Every other IPv6
// address, IPv4-mapped included, is judged by the ordinary rules alone.
func TestAnIPv4CompatibleEndpointAddressIsRefused(t *testing.T) {
	o, c := fixture("kubevirt", 3, false)
	for address, refused := range map[string]bool{"::192.0.2.1": true, "::1": true, "::": true, "2001:db8::10": false, "::ffff:192.0.2.10": false} {
		t.Run(address, func(t *testing.T) {
			effective, _ := Normalize(o.WithSpec(o.Spec().WithPath(api.StringValue(address), "install", "endpoints", "api", "address")), c)
			issues := Validate(effective, c)
			if hasIssue(issues, "$.spec.install.endpoints.api.address", "IPv4-compatible IPv6 endpoint address (::/96)") != refused {
				t.Fatalf("want refused = %v: %v", refused, issues)
			}
		})
	}
}

func TestAPIInternalCopiesOnlyAddressAndSource(t *testing.T) {
	o, c := fixture("vsphere", 4, false)
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
	o, c := fixture("vsphere", 4, false)
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
	o, c := fixture("kubevirt", 4, false)
	o = o.WithSpec(o.Spec().WithPath(m("unlock", m("tpm2", m())), "security", "diskEncryption"))
	effective, _ := Normalize(o, c)
	if effective.Spec().Get("distribution", "release", "channel").Text() != "stable-4.21" || effective.Spec().Get("install", "pullSecretRef").Text() != "openshift-pull-secret" || effective.Spec().Get("install", "nodeSSH", "keyPairRef").Text() != "cluster-cluster-admin-ssh-key" {
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
	image := o.WithSpec(o.Spec().WithPath(m("image", "quay.io/example/release:4.21.15"), "distribution", "release"))
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
	o, c := fixture("baremetal", 4, false)
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

// A server placed on one of a cluster's nodes cannot serve that node's
// installation, and the refusal says so rather than the opposite rule.
func TestAClusterCycleIsStatedAsRefused(t *testing.T) {
	o, c := fixture("baremetal", 4, false)
	server := obj(api.ArtifactServer, "artifacts", m("management", "managed", "machineRef", "node-0", "endpoints", list(m("name", "media"))))
	c = api.NewCatalog(append(c.Objects(), server))
	o = o.WithSpec(o.Spec().WithPath(m("serverRef", "artifacts", "endpointRef", "media"), "install", "agent", "redfishVirtualMedia", "artifactServerEndpoint"))
	o, _ = Normalize(o, c)
	const field = "$.spec.install.agent.redfishVirtualMedia.artifactServerEndpoint.serverRef"
	var found []api.Issue
	for _, issue := range Validate(o, c) {
		if issue.Field == field {
			found = append(found, issue)
		}
	}
	if len(found) != 1 || found[0].Code != "api.invariant" ||
		found[0].Message != "a cluster installation cannot publish through an artifact server placed on one of its nodes, because that server cannot serve until the node is installed" ||
		found[0].Remediation != "place ArtifactServer/artifacts on the controller Machine, or select a server placed there" {
		t.Fatalf("refusal at %s = %#v, want the cycle stated as refused with a remedy naming the server and the controller", field, found)
	}
}

func TestMissingPrerequisitesSuppressSecondaryErrors(t *testing.T) {
	o, _ := fixture("vsphere", 4, false)
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

// roster is a cluster on the fixture's release whose nodes take these roles in
// order, normalized as admission receives it. Edits apply to the authored
// object first.
func roster(variant string, roles []string, edits ...func(api.Value) api.Value) (api.Object, api.Catalog) {
	o, c := fixture(variant, len(roles), false)
	nodes := o.Spec().Get("nodes").Items()
	for i, role := range roles {
		nodes[i] = nodes[i].With("role", api.StringValue(role))
	}
	spec := o.Spec().With("nodes", list(nodes...))
	for _, edit := range edits {
		spec = edit(spec)
	}
	o, _ = Normalize(o.WithSpec(spec), c)
	return o, c
}

func TestTopologyAdmitsOnlyTheControlPlaneCountsItsReleaseAccepts(t *testing.T) {
	for masters := range 7 {
		t.Run(fmt.Sprint(masters), func(t *testing.T) {
			roles := slices.Repeat([]string{"master"}, masters)
			if masters == 0 {
				roles = []string{"worker"}
			}
			issues := Validate(roster("libvirt", roles))
			switch masters {
			case 1, 3, 4, 5:
				if len(issues) > 0 {
					t.Fatal(issues)
				}
			case 0:
				if !hasIssue(issues, "$.spec.nodes", "requires at least one master node") {
					t.Fatal("a cluster without a master admitted", issues)
				}
			default:
				if !hasIssue(issues, "$.spec.nodes", fmt.Sprintf("release 4.21 accepts 1, 3, 4 or 5 master nodes, not %d", masters)) {
					t.Fatal("a control plane the release refuses admitted", issues)
				}
			}
		})
	}
}

func TestTopologyRefusesComputeBesideOneMaster(t *testing.T) {
	for _, roles := range [][]string{{"master", "worker"}, {"master", "infra"}, {"master", "worker", "infra", "worker"}} {
		t.Run(strings.Join(roles, ","), func(t *testing.T) {
			if issues := Validate(roster("libvirt", roles)); !hasIssue(issues, "$.spec.nodes", "a single-master cluster admits no worker or infra node") {
				t.Fatal("compute beside a single master admitted", issues)
			}
		})
	}
	if issues := Validate(roster("libvirt", []string{"master", "master", "master", "worker", "infra"})); len(issues) > 0 {
		t.Fatal("compute beside three masters refused", issues)
	}
	imageOnly := func(spec api.Value) api.Value {
		return spec.WithPath(m("image", "quay.io/example/release:4.21.15"), "distribution", "release")
	}
	if issues := Validate(roster("libvirt", []string{"master", "worker"}, imageOnly)); !hasIssue(issues, "$.spec.nodes", "a single-master cluster admits no worker or infra node") {
		t.Fatal("compute beside a single master admitted for a release pinned by image", issues)
	}
}

func TestTopologyRefusesAReleaseTheTableDoesNotQualify(t *testing.T) {
	for version, minor := range map[string]string{"4.20.3": "4.20", "4.22.0": "4.22", "5.0.1": "5.0", "latest": "latest"} {
		t.Run(version, func(t *testing.T) {
			o, c := roster("libvirt", []string{"master", "master"}, func(spec api.Value) api.Value {
				return spec.WithPath(api.StringValue(version), "distribution", "release", "version")
			})
			issues := Validate(o, c)
			if !hasIssue(issues, "$.spec.distribution.release.version", "the topology table qualifies no release "+minor) {
				t.Fatal("an unqualified release admitted", issues)
			}
			for _, issue := range issues {
				if strings.Contains(issue.Message, "topology table") && !strings.Contains(issue.Remediation, "qualifies: 4.21") {
					t.Fatal("the remedy names no qualified release", issue)
				}
				if issue.Field == "$.spec.nodes" {
					t.Fatal("a release with no row judged its control plane", issue)
				}
			}
		})
	}
	o, c := roster("kubevirt", []string{"master"}, func(spec api.Value) api.Value {
		return spec.WithPath(m("image", "quay.io/example/release:4.21.15"), "distribution", "release")
	})
	if issues := Validate(o, c); len(issues) > 0 {
		t.Fatal("a release pinned by image alone refused at admission instead of selection", issues)
	}
}

func TestTopologyRefusesInstallerOwnedEndpointsOnAMultiNodePlatformNone(t *testing.T) {
	masters := []string{"master", "master", "master"}
	installerOwned := func(spec api.Value) api.Value {
		for _, slot := range []string{"api", "ingress"} {
			spec = spec.WithPath(api.StringValue("openshift"), "install", "endpoints", slot, "source", "type")
		}
		return spec
	}
	authoredNone := func(spec api.Value) api.Value {
		return spec.WithPath(m("type", "none"), "install", "platform")
	}
	for name, o := range map[string]func() (api.Object, api.Catalog){
		"derived none":  func() (api.Object, api.Catalog) { return roster("kubevirt", masters, installerOwned) },
		"authored none": func() (api.Object, api.Catalog) { return roster("libvirt", masters, installerOwned, authoredNone) },
	} {
		t.Run(name, func(t *testing.T) {
			issues := Validate(o())
			for _, slot := range endpointSlots {
				if !hasIssue(issues, "$.spec.install.endpoints."+slot+".source.type", "platform none installs no endpoint VIPs") {
					t.Fatal("an installer-owned endpoint admitted without VIPs", slot, issues)
				}
			}
		})
	}
	for name, o := range map[string]func() (api.Object, api.Catalog){
		"derived baremetal": func() (api.Object, api.Catalog) { return roster("libvirt", masters, installerOwned) },
		"external on none":  func() (api.Object, api.Catalog) { return roster("kubevirt", masters) },
	} {
		t.Run(name, func(t *testing.T) {
			if issues := Validate(o()); len(issues) > 0 {
				t.Fatal(issues)
			}
		})
	}
}

// TestEveryExampleReleaseHasATopologyRow makes a release bump in an example
// review the topology table first.
func TestEveryExampleReleaseHasATopologyRow(t *testing.T) {
	clusters := 0
	err := filepath.WalkDir(filepath.Join("..", "..", "examples"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && (entry.Name() == "wip" || entry.Name() == "secrets") {
			return fs.SkipDir
		}
		if entry.IsDir() || filepath.Ext(path) != ".yaml" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		decoder := yaml.NewDecoder(bytes.NewReader(data))
		for {
			var document struct {
				Kind string    `yaml:"kind"`
				Spec yaml.Node `yaml:"spec"`
			}
			if err := decoder.Decode(&document); errors.Is(err, io.EOF) {
				return nil
			} else if err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			if document.Kind != string(api.ContainerCluster) {
				continue
			}
			clusters++
			var spec struct {
				Distribution struct{ Release struct{ Version string } }
			}
			if err := document.Spec.Decode(&spec); err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			version := spec.Distribution.Release.Version
			if minor, ok := releaseMinor(version); !ok || releaseTopologies[minor].controlPlane == nil {
				t.Errorf("%s declares release %q, whose minor has no row in releaseTopologies; read its installer branch and add the row first", path, version)
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if clusters == 0 {
		t.Fatal("no example declares a ContainerCluster")
	}
}

// TestTopologyTableMatchesSpec keeps the spec's table and the code's rows one
// table: each row states its minor, its counts and every upstream source.
func TestTopologyTableMatchesSpec(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "specs", "api", "container-clusters.md"))
	if err != nil {
		t.Fatal(err)
	}
	rows := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "| `") && strings.Contains(line, "openshift/installer/blob/release-") {
			rows++
		}
	}
	if rows != len(releaseTopologies) {
		t.Fatalf("the spec lists %d topology rows, the code %d", rows, len(releaseTopologies))
	}
	for minor, row := range releaseTopologies {
		counts := make([]string, len(row.controlPlane))
		for i, count := range row.controlPlane {
			counts[i] = fmt.Sprint(count)
		}
		prefix := fmt.Sprintf("| `%s` | %s |", minor, strings.Join(counts, ", "))
		found := false
		for _, line := range strings.Split(string(data), "\n") {
			if !strings.HasPrefix(line, prefix) {
				continue
			}
			found = true
			for _, source := range row.sources {
				if !strings.Contains(line, "("+source+")") {
					t.Errorf("the spec row for %s does not cite %s", minor, source)
				}
			}
		}
		if !found || len(row.sources) == 0 {
			t.Errorf("the spec has no row %q, or the code row cites no source", prefix)
		}
	}
}

func hasIssue(issues []api.Issue, field, fragment string) bool {
	for _, issue := range issues {
		if issue.Field == field && issue.Code == "api.invariant" && strings.Contains(issue.Message, fragment) {
			return true
		}
	}
	return false
}

func hasField(issues []api.Issue, field string) bool {
	for _, issue := range issues {
		if issue.Field == field {
			return true
		}
	}
	return false
}
