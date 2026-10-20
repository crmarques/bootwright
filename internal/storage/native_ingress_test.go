package storage

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

const (
	rgwAt          = "$.spec.ceph.services[3]"
	rgwIngressAt   = "$.spec.ceph.services[4]"
	nfsAt          = "$.spec.ceph.services[5]"
	nfsIngressAt   = "$.spec.ceph.services[6]"
	haproxyHidden  = "an NFS ingress without the HAProxy protocol hides client addresses, so export client restrictions would not function"
	flagsDiffer    = "enable_haproxy_protocol must be equal on the NFS service and its ingress"
	vipTaken       = "this ingress virtual_ip is already used by another ingress service"
	keepaliveCount = "an NFS service behind a keepalive-only ingress runs exactly one daemon"
)

func graphIngress(id, backend, vip string, spec api.Value) api.Value {
	base := m("backend_service", backend, "virtual_ip", vip)
	for _, f := range spec.Fields() {
		base = base.With(f.Name, f.Value)
	}
	return m("service_type", "ingress", "service_id", id, "placement", m("label", "storage"), "spec", base)
}

func graphServices() []api.Value {
	return []api.Value{
		rgwService(qualifiedRGWSpec()),
		graphIngress("rgw.objects", "rgw.objects", "192.0.2.61/24", m("frontend_port", 8080, "monitor_port", 1967)),
		nfsService(m("port", 12049, "enable_haproxy_protocol", true)),
		graphIngress("nfs.files", "nfs.files", "192.0.2.60/24", m("frontend_port", 2049, "monitor_port", 9049, "enable_haproxy_protocol", true, "first_virtual_router_id", 51)),
	}
}

func keepaliveServices() []api.Value {
	services := graphServices()
	services[2] = nfsService(m("virtual_ip", "192.0.2.60")).With("placement", m("label", "storage", "count", 1))
	services[3] = graphIngress("nfs.files", "nfs.files", "192.0.2.60/24", m("keepalive_only", true, "first_virtual_router_id", 51))
	return services
}

func withGraph(t *testing.T, catalog api.Catalog, services []api.Value, bindings ...api.Value) (api.Object, api.Catalog) {
	t.Helper()
	cluster := find(t, catalog, api.StorageCluster, "storage")
	all := append(cluster.Spec().Get("ceph", "services").Items(), services...)
	spec := cluster.Spec().WithPath(api.ListValue(all...), "ceph", "services")
	if len(bindings) != 0 {
		spec = spec.WithPath(api.ListValue(bindings...), "ceph", "serviceBindings")
	}
	changed := cluster.WithSpec(spec)
	return changed, replace(catalog, changed)
}

func graphIssues(t *testing.T, services []api.Value, bindings ...api.Value) []api.Issue {
	t.Helper()
	cluster, catalog := withGraph(t, bindingCatalog(), services, bindings...)
	return Validate(cluster, catalog)
}

func requireGraphAdmits(t *testing.T, issues []api.Issue) {
	t.Helper()
	found := issuesUnder(issues, "$.spec.ceph.serviceBindings")
	for _, at := range []string{rgwAt, rgwIngressAt, nfsAt, nfsIngressAt, "$.spec.ceph.services[7]"} {
		found = append(found, issuesUnder(issues, at)...)
	}
	if len(found) != 0 {
		t.Fatalf("admitted graph refused: %#v", found)
	}
}

func requireOnly(t *testing.T, issues []api.Issue, field, code, message, remedy string) {
	t.Helper()
	found := issuesAt(issues, field)
	if len(found) != 1 || found[0].Code != code || found[0].Message != message || (remedy != "" && found[0].Remediation != remedy) {
		t.Fatalf("issues at %s: %#v", field, found)
	}
	for _, text := range []string{"192.0.2", "198.51.100", "2001:", "8080", "objects", "files"} {
		if strings.Contains(found[0].Message, text) {
			t.Fatalf("diagnostic echoed authored text %q: %#v", text, found[0])
		}
	}
}

func TestIngressBackendMustResolveDeclaredService(t *testing.T) {
	requireGraphAdmits(t, graphIssues(t, graphServices()))
	const unresolved = "ingress backend_service must resolve to one declared service of this cluster"
	for _, tt := range []struct{ name, backend, message string }{
		{"no arm", "mgr", "ingress backend_service must name a declared rgw, nfs or mgmt-gateway service of this cluster"},
		{"absent", "rgw.absent", unresolved},
		{"type only", "rgw", unresolved},
	} {
		t.Run(tt.name, func(t *testing.T) {
			services := graphServices()
			services[1] = graphIngress("rgw.objects", tt.backend, "192.0.2.61/24", m("frontend_port", 8080, "monitor_port", 1967))
			requireOnly(t, graphIssues(t, services), rgwIngressAt+".spec.backend_service", "api.invariant", tt.message, "")
		})
	}
	services := append(graphServices(), graphIngress("rgw.second", "rgw.objects", "192.0.2.62/24", m("frontend_port", 8081, "monitor_port", 1968)))
	issues := graphIssues(t, services)
	requireOnly(t, issues, "$.spec.ceph.services[7].spec.backend_service", "api.invariant", "one ingress serves each backend service in this build", "remove the second ingress for this backend")
	if found := issuesAt(issues, rgwIngressAt+".spec.backend_service"); len(found) != 0 {
		t.Fatalf("first ingress refused: %#v", found)
	}
}

func TestIngressArmMembersFollowTheBackend(t *testing.T) {
	for _, tt := range []struct {
		name, member, arm string
		index             int
		value             any
	}{
		{"keepalive on rgw", "keepalive_only", "nfs or mgmt-gateway", 1, true},
		{"peer port on rgw", "haproxy_peer_communication_port", "nfs", 1, 1025},
		{"ssl on nfs", "ssl", "rgw", 3, false},
		{"certificate source on nfs", "certificate_source", "rgw", 3, "inline"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			services := graphServices()
			services[tt.index] = services[tt.index].WithPath(v(tt.value), "spec", tt.member)
			at := []string{rgwAt, rgwIngressAt, nfsAt, nfsIngressAt}[tt.index]
			requireOnly(t, graphIssues(t, services), at+".spec."+tt.member, "api.invariant", "spec."+tt.member+" applies only to an ingress whose backend is a "+tt.arm+" service", "omit spec."+tt.member+" for this backend")
		})
	}
	tls := graphServices()
	tls[1] = tls[1].WithPath(api.BoolValue(true), "spec", "ssl")
	requireGraphAdmits(t, graphIssues(t, tls, leaves("ingress.rgw.objects", "/spec/ssl_cert", "tls-a", "/spec/ssl_key", "tls-a")))
	requireOnly(t, graphIssues(t, tls), rgwIngressAt+".spec.ssl", "api.invariant", "TLS needs the /spec/ssl_cert and /spec/ssl_key bindings of one tlsCertificate Secret", "")
	issues := graphIssues(t, graphServices(), leaves("ingress.nfs.files", "/spec/ssl_cert", "tls-a", "/spec/ssl_key", "tls-a"))
	requireOnly(t, issues, "$.spec.ceph.serviceBindings[0].serviceRef", "api.invariant", "the NFS service and its ingress take no secret bindings: Bootwright does not support NFS over TLS or kmip yet", "remove this spec.ceph.serviceBindings entry")
	if found := issuesUnder(issues, "$.spec.ceph.serviceBindings[0].secrets"); len(found) != 0 {
		t.Fatalf("NFS ingress binding leaves were also checked: %#v", found)
	}
}

func TestNFSModeMatrix(t *testing.T) {
	direct := graphServices()[:3]
	direct[2] = nfsService(api.MapValue())
	requireGraphAdmits(t, graphIssues(t, direct))
	requireGraphAdmits(t, graphIssues(t, keepaliveServices()))
	requireGraphAdmits(t, graphIssues(t, graphServices()))
	standard := graphServices()
	standard[2] = nfsService(api.MapValue())
	standard[3] = graphIngress("nfs.files", "nfs.files", "192.0.2.60/24", m("frontend_port", 2049, "monitor_port", 9049))
	requireOnly(t, graphIssues(t, standard), nfsIngressAt+".spec.enable_haproxy_protocol", "api.invariant", haproxyHidden, "set enable_haproxy_protocol: true on both the NFS service and its ingress, or set keepalive_only: true on the ingress")
	standard[2] = nfsService(m("enable_haproxy_protocol", true))
	issues := graphIssues(t, standard)
	requireOnly(t, issues, nfsIngressAt+".spec.enable_haproxy_protocol", "api.invariant", haproxyHidden, "")
	requireOnly(t, issues, nfsAt+".spec.enable_haproxy_protocol", "api.invariant", flagsDiffer, "set enable_haproxy_protocol: true on both")
	unequal := graphServices()
	unequal[2] = nfsService(api.MapValue())
	requireOnly(t, graphIssues(t, unequal), nfsAt+".spec.enable_haproxy_protocol", "api.invariant", flagsDiffer, "set enable_haproxy_protocol: true on both")
	direct[2] = nfsService(m("virtual_ip", "192.0.2.60"))
	requireOnly(t, graphIssues(t, direct), nfsAt+".spec.virtual_ip", "api.invariant", "an NFS virtual_ip needs an ingress service that fronts this NFS service", "declare an ingress for this NFS service or omit spec.virtual_ip")
	direct[2] = nfsService(m("enable_haproxy_protocol", true))
	requireOnly(t, graphIssues(t, direct), nfsAt+".spec.enable_haproxy_protocol", "api.invariant", "the HAProxy protocol needs an ingress with the same setting", "declare an ingress with enable_haproxy_protocol: true for this NFS service or omit the flag")
	both := graphServices()
	both[3] = both[3].WithPath(api.BoolValue(true), "spec", "keepalive_only")
	requireOnly(t, graphIssues(t, both), nfsIngressAt+".spec.keepalive_only", "api.invariant", "keepalive-only and HAProxy protocol modes are exclusive", "set only one of spec.keepalive_only and spec.enable_haproxy_protocol")
	multi := graphServices()
	multi[2] = multi[2].With("placement", m("label", "storage", "count", 3))
	requireOnly(t, graphIssues(t, multi), nfsAt+".placement.count", "api.invariant", "a multi-active NFS service needs an explicit placement count below its eligible host count", "set placement.count below the number of eligible NFS hosts")
	multi[2] = multi[2].With("placement", m("label", "storage")).WithPath(api.StringValue("192.0.2.62"), "spec", "virtual_ip")
	issues = graphIssues(t, multi)
	requireOnly(t, issues, nfsAt+".placement.count", "api.invariant", "a multi-active NFS service needs an explicit placement count below its eligible host count", "")
	requireOnly(t, issues, nfsAt+".spec.virtual_ip", "api.invariant", "the NFS virtual_ip must equal its ingress virtual IP address", "")
}

func TestKeepaliveOnlyRequiresCountOneAndMatchingVIP(t *testing.T) {
	for _, tt := range []struct {
		name, field, code, message string
		change                     func(api.Value) api.Value
	}{
		{"count two", ".placement.count", "api.invariant", keepaliveCount, func(s api.Value) api.Value { return s.With("placement", m("label", "storage", "count", 2)) }},
		{"count absent", ".placement.count", "api.invariant", keepaliveCount, func(s api.Value) api.Value { return s.With("placement", m("label", "storage")) }},
		{"other VIP", ".spec.virtual_ip", "api.invariant", "the NFS virtual_ip must equal its ingress virtual IP address", func(s api.Value) api.Value {
			return s.WithPath(api.StringValue("192.0.2.62"), "spec", "virtual_ip")
		}},
		{"VIP absent", ".spec.virtual_ip", "api.required", "a keepalive-only NFS service binds the ingress virtual IP", func(s api.Value) api.Value { return s.With("spec", api.MapValue()) }},
		{"one host", ".placement", "api.invariant", "a keepalive-only NFS service needs at least two eligible hosts to move to", func(s api.Value) api.Value {
			return s.With("placement", m("host_pattern", "node-1*", "count", 1))
		}},
		{"HAProxy flag", ".spec.enable_haproxy_protocol", "api.invariant", "keepalive-only and HAProxy protocol modes are exclusive", func(s api.Value) api.Value {
			return s.WithPath(api.BoolValue(true), "spec", "enable_haproxy_protocol")
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			services := keepaliveServices()
			services[2] = tt.change(services[2])
			requireOnly(t, graphIssues(t, services), nfsAt+tt.field, tt.code, tt.message, "")
		})
	}
}

func TestVIPMustBeFreeAndInsidePublicCIDR(t *testing.T) {
	const outside = "an ingress virtual_ip must lie inside a declared public CIDR and carry that CIDR's prefix length"
	const edge = "an ingress virtual_ip cannot be the network or broadcast address of its public CIDR"
	for _, tt := range []struct{ vip, message string }{
		{"192.0.2.2/24", "an ingress virtual_ip equals a declared Machine address"},
		{"198.51.100.10/24", outside},
		{"192.0.2.10/25", outside},
		{"192.0.2.0/24", edge},
		{"192.0.2.255/24", edge},
		{"192.0.2.10", "an ingress virtual_ip is an address with its network prefix length"},
	} {
		t.Run(tt.vip, func(t *testing.T) {
			services := graphServices()
			services[1] = services[1].WithPath(api.StringValue(tt.vip), "spec", "virtual_ip")
			requireOnly(t, graphIssues(t, services), rgwIngressAt+".spec.virtual_ip", "api.invariant", tt.message, "")
		})
	}
	services := graphServices()
	services[1] = services[1].WithPath(api.StringValue("192.0.2.10/24"), "spec", "virtual_ip")
	requireGraphAdmits(t, graphIssues(t, services))
	services[3] = services[3].WithPath(api.StringValue("192.0.2.10/24"), "spec", "virtual_ip")
	requireOnly(t, graphIssues(t, services), nfsIngressAt+".spec.virtual_ip", "api.invariant", vipTaken, "choose a virtual IP no other ingress uses")
	other := object(api.StorageCluster, "other", m("type", "ceph", "ceph", m("services", api.ListValue(graphIngress("rgw.objects", "rgw.objects", "192.0.2.61/24", api.MapValue())))))
	cluster, catalog := withGraph(t, replace(bindingCatalog(), other), graphServices())
	requireOnly(t, Validate(cluster, catalog), rgwIngressAt+".spec.virtual_ip", "api.invariant", vipTaken, "")
}

func TestIngressVIPsCompareParsedAddresses(t *testing.T) {
	catalog := bindingCatalog()
	storage := find(t, catalog, api.StorageCluster, "storage")
	dual := storage.WithSpec(storage.Spec().WithPath(api.StringList("192.0.2.0/24", "2001:db8::/64"), "ceph", "networks", "publicCIDRs"))
	catalog = replace(catalog, dual)
	services := graphServices()
	services[1] = services[1].WithPath(api.StringValue("2001:db8::10/64"), "spec", "virtual_ip")
	services[3] = services[3].WithPath(api.StringValue("2001:DB8:0::10/64"), "spec", "virtual_ip")
	cluster, changed := withGraph(t, catalog, services)
	requireOnly(t, Validate(cluster, changed), nfsIngressAt+".spec.virtual_ip", "api.invariant", vipTaken, "")
	keepalive := keepaliveServices()
	keepalive[2] = keepalive[2].WithPath(api.StringValue("2001:DB8::10"), "spec", "virtual_ip")
	keepalive[3] = keepalive[3].WithPath(api.StringValue("2001:db8:0:0::10/64"), "spec", "virtual_ip")
	cluster, changed = withGraph(t, catalog, keepalive)
	requireGraphAdmits(t, Validate(cluster, changed))
	if prefix, ok := ParseVirtualIP("2001:DB8:0::10/64", true); !ok || prefix.String() != "2001:db8::10/64" {
		t.Fatalf("canonical rendering: %v %v", prefix, ok)
	}
	for _, tt := range []struct {
		text     string
		prefixed bool
	}{{"fe80::1%eth0", false}, {"::ffff:192.0.2.60", false}, {"::ffff:192.0.2.60/120", true}, {"192.0.2.60/24", false}, {"192.0.2.60", true}} {
		if _, ok := ParseVirtualIP(tt.text, tt.prefixed); ok {
			t.Fatalf("ParseVirtualIP admitted %q prefixed=%v", tt.text, tt.prefixed)
		}
	}
}

func TestRGWBehindIngressNeedsTwoHostsAndPlainHTTP(t *testing.T) {
	for _, tt := range []struct {
		name, field, message string
		change               func(api.Value) api.Value
	}{
		{"one host", ".placement", "an RGW service behind an ingress needs at least two eligible hosts", func(s api.Value) api.Value { return s.With("placement", m("host_pattern", "node-1*")) }},
		{"one daemon", ".placement.count", "an RGW service behind an ingress runs at least two daemons", func(s api.Value) api.Value { return s.With("placement", m("label", "storage", "count", 1)) }},
		{"TLS at the daemons", ".spec.ssl", "TLS terminates at the ingress, not at the RGW daemons", func(s api.Value) api.Value { return s.WithPath(api.BoolValue(true), "spec", "ssl") }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			services := graphServices()
			services[0] = tt.change(services[0])
			requireOnly(t, graphIssues(t, services, leaves("rgw.objects", "/spec/ssl_cert", "tls-a", "/spec/ssl_key", "tls-a")), rgwAt+tt.field, "api.invariant", tt.message, "")
		})
	}
	alone := []api.Value{rgwService(qualifiedRGWSpec().With("ssl", api.BoolValue(true)))}
	requireGraphAdmits(t, graphIssues(t, alone, leaves("rgw.objects", "/spec/ssl_cert", "tls-a", "/spec/ssl_key", "tls-a")))
}

func TestIngressPortsAreDistinctIncludingTheNFSPeerDefault(t *testing.T) {
	const differ = "ingress frontend, monitor and peer ports must differ"
	services := graphServices()
	services[1] = services[1].WithPath(api.IntegerValue("8080"), "spec", "monitor_port")
	requireOnly(t, graphIssues(t, services), rgwIngressAt+".spec.frontend_port", "api.invariant", differ, "choose distinct ports; an NFS ingress reserves 1024 for HAProxy peers unless spec.haproxy_peer_communication_port names another")
	services = graphServices()
	services[3] = services[3].WithPath(api.IntegerValue("1024"), "spec", "frontend_port")
	requireOnly(t, graphIssues(t, services), nfsIngressAt+".spec.frontend_port", "api.invariant", differ, "")
	services[3] = services[3].WithPath(api.IntegerValue("1025"), "spec", "haproxy_peer_communication_port")
	requireGraphAdmits(t, graphIssues(t, services))
	services[3] = services[3].WithPath(api.IntegerValue("9049"), "spec", "haproxy_peer_communication_port")
	requireOnly(t, graphIssues(t, services), nfsIngressAt+".spec.monitor_port", "api.invariant", differ, "")
	services = graphServices()
	services[1] = graphIngress("rgw.objects", "rgw.objects", "192.0.2.61/24", m("monitor_port", 1967))
	requireOnly(t, graphIssues(t, services), rgwIngressAt+".spec.frontend_port", "api.required", "an ingress that runs HAProxy needs frontend_port", "supply spec.frontend_port")
	requireGraphAdmits(t, graphIssues(t, keepaliveServices()))
}

func TestIngressNeedsTwoHostsAndPublicInterfaceNetworks(t *testing.T) {
	for _, tt := range []struct {
		name, field, message string
		change               func(api.Value) api.Value
	}{
		{"one host", ".placement", "an ingress service needs at least two eligible hosts for failover", func(s api.Value) api.Value { return s.With("placement", m("host_pattern", "node-1*")) }},
		{"interval", ".spec.health_check_interval", "health_check_interval must be a whole number of seconds, minutes or hours", func(s api.Value) api.Value {
			return s.WithPath(api.StringValue(" 5s"), "spec", "health_check_interval")
		}},
		{"host bits", ".spec.virtual_interface_networks[0]", "virtual_interface_networks entries are network prefixes without host bits", func(s api.Value) api.Value {
			return s.WithPath(api.StringList("192.0.2.5/24"), "spec", "virtual_interface_networks")
		}},
		{"outside public", ".spec.virtual_interface_networks[0]", "service network must be reachable through the cluster's declared public networks", func(s api.Value) api.Value {
			return s.WithPath(api.StringList("198.51.100.0/24"), "spec", "virtual_interface_networks")
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			services := graphServices()
			services[1] = tt.change(services[1])
			requireOnly(t, graphIssues(t, services), rgwIngressAt+tt.field, "api.invariant", tt.message, "")
		})
	}
	services := graphServices()
	services[1] = services[1].WithPath(api.StringValue("30s"), "spec", "health_check_interval").WithPath(api.StringList("192.0.2.0/24"), "spec", "virtual_interface_networks")
	requireGraphAdmits(t, graphIssues(t, services))
}
