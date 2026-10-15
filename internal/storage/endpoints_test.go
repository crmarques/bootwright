package storage

import (
	"fmt"
	"reflect"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

const (
	routerIDMessage = "this ingress uses the same keepalived virtual router ID as %s on an overlapping network; an omitted first_virtual_router_id is 50"
	routerIDRemedy  = "set spec.first_virtual_router_id to an ID from 1 to 255 that no other ingress on these networks uses"
	qosListener     = "this listener port is already bound on host node-1 by the NFS cluster QoS listener (31311) of $.spec.ceph.services[3]"
)

func plainRGW(spec api.Value) api.Value {
	return rgwService(spec.Without("only_bind_port_on_networks"))
}

func rgwPort(port int) api.Value {
	return qualifiedRGWSpec().With("rgw_frontend_port", api.IntegerValue(fmt.Sprint(port)))
}

func requireBound(t *testing.T, issues []api.Issue, field, host, other string) {
	t.Helper()
	requireOnly(t, issues, field, "api.invariant", "this listener port is already bound on host "+host+" by "+other, "set "+field+" to another port, or place the two services on disjoint hosts")
}

func requireNoIssueAt(t *testing.T, issues []api.Issue, fields ...string) {
	t.Helper()
	for _, field := range fields {
		if found := issuesAt(issues, field); len(found) != 0 {
			t.Fatalf("issues at %s: %#v", field, found)
		}
	}
}

func withIngressSpec(service api.Value, pairs ...any) api.Value {
	spec := service.Get("spec")
	for i := 0; i < len(pairs); i += 2 {
		spec = spec.With(pairs[i].(string), v(pairs[i+1]))
	}
	return service.With("spec", spec)
}

func TestListenerRegistryRefusesPortOverlap(t *testing.T) {
	for _, tt := range []struct {
		port  int
		label string
	}{
		{6800, "the Ceph messenger range 6800-7568 of OSD, MGR and MDS daemons"},
		{7568, "the Ceph messenger range 6800-7568 of OSD, MGR and MDS daemons"},
		{3300, "the Ceph monitors (3300 and 6789)"},
		{6789, "the Ceph monitors (3300 and 6789)"},
		{9926, "ceph-exporter (9926)"},
	} {
		field := rgwAt + ".spec.rgw_frontend_port"
		issues := graphIssues(t, []api.Value{rgwService(rgwPort(tt.port))})
		requireOnly(t, issues, field, "api.invariant", "this listener port is reserved by "+tt.label+" on host node-1", "set "+field+" to a port that no Ceph daemon reserves on the selected hosts")
	}
	for _, port := range []int{6799, 7569} {
		if found := issuesUnder(graphIssues(t, []api.Value{rgwService(rgwPort(port))}), rgwAt); len(found) != 0 {
			t.Fatalf("port %d refused: %#v", port, found)
		}
	}
	requireGraphAdmits(t, graphIssues(t, graphServices()))
	services := graphServices()
	services[0] = plainRGW(qualifiedRGWSpec())
	requireBound(t, graphIssues(t, services), rgwIngressAt+".spec.frontend_port", "node-1", rgwAt+".spec.rgw_frontend_port")
	services[0] = rgwService(qualifiedRGWSpec()).With("networks", api.StringList("192.0.2.128/25"))
	requireBound(t, graphIssues(t, services), rgwIngressAt+".spec.frontend_port", "node-1", rgwAt+".spec.rgw_frontend_port")
	services = graphServices()
	services[2] = nfsService(m("enable_haproxy_protocol", true))
	requireBound(t, graphIssues(t, services), nfsIngressAt+".spec.frontend_port", "node-1", nfsAt+".spec.port")
	requireGraphAdmits(t, graphIssues(t, keepaliveServices()))
	services = keepaliveServices()
	services[0] = plainRGW(rgwPort(2049))
	requireBound(t, graphIssues(t, services), nfsAt+".spec.port", "node-1", rgwAt+".spec.rgw_frontend_port")
	services[0] = rgwService(rgwPort(2049))
	requireGraphAdmits(t, graphIssues(t, services))
	services = keepaliveServices()
	services[3] = withIngressSpec(services[3], "frontend_port", 2049)
	requireBound(t, graphIssues(t, services), nfsIngressAt+".spec.frontend_port", "node-1", nfsAt+".spec.port")
	rgw := plainRGW(rgwPort(2049)).With("placement", m("hosts", api.StringList("node-3.storage.example.test")))
	disjoint := []api.Value{rgw, nfsService(api.MapValue()).With("placement", m("hosts", api.StringList("node-1.storage.example.test", "node-2.storage.example.test")))}
	requireNoIssueAt(t, graphIssues(t, disjoint), rgwAt+".spec.rgw_frontend_port", "$.spec.ceph.services[4].spec.port")
	shared := []api.Value{rgw, nfsService(api.MapValue()).With("placement", m("hosts", api.StringList("node-2.storage.example.test", "node-3.storage.example.test")))}
	requireBound(t, graphIssues(t, shared), "$.spec.ceph.services[4].spec.port", "node-3", rgwAt+".spec.rgw_frontend_port")
}

func TestVRRPDefaultFiftyConflicts(t *testing.T) {
	field := nfsIngressAt + ".spec.first_virtual_router_id"
	services := graphServices()
	services[3] = services[3].With("spec", services[3].Get("spec").Without("first_virtual_router_id"))
	omitted := graphIssues(t, services)
	requireOnly(t, omitted, field, "api.invariant", fmt.Sprintf(routerIDMessage, rgwIngressAt), routerIDRemedy)
	services[1] = withIngressSpec(services[1], "first_virtual_router_id", 50)
	if authored := graphIssues(t, services); !reflect.DeepEqual(issuesUnder(omitted, nfsIngressAt), issuesUnder(authored, nfsIngressAt)) {
		t.Fatalf("authored 50 differs from omitted: %#v", authored)
	}
	services[1] = withIngressSpec(services[1], "first_virtual_router_id", 255)
	services[3] = withIngressSpec(services[3], "first_virtual_router_id", 1)
	requireNoIssueAt(t, graphIssues(t, services), field, rgwIngressAt+".spec.first_virtual_router_id")
	services[1] = withIngressSpec(services[1], "first_virtual_router_id", 51)
	services[3] = withIngressSpec(services[3], "first_virtual_router_id", 51)
	requireOnly(t, graphIssues(t, services), field, "api.invariant", fmt.Sprintf(routerIDMessage, rgwIngressAt), routerIDRemedy)

	catalog := bindingCatalog()
	cluster := find(t, catalog, api.StorageCluster, "storage")
	catalog = replace(catalog, cluster.WithSpec(cluster.Spec().WithPath(api.StringList("192.0.2.0/24", "198.51.100.0/24"), "ceph", "networks", "publicCIDRs")))
	services = graphServices()
	services[3] = graphIngress("nfs.files", "nfs.files", "198.51.100.60/24", m("frontend_port", 2049, "monitor_port", 9049, "enable_haproxy_protocol", true))
	changed, all := withGraph(t, catalog, services)
	requireNoIssueAt(t, Validate(changed, all), field)
	services[3] = withIngressSpec(services[3], "virtual_interface_networks", api.StringList("192.0.2.0/24"))
	changed, all = withGraph(t, catalog, services)
	requireOnly(t, Validate(changed, all), field, "api.invariant", fmt.Sprintf(routerIDMessage, rgwIngressAt), routerIDRemedy)

	other := object(api.StorageCluster, "other", m("type", "ceph", "ceph", m("services", api.ListValue(graphIngress("rgw.objects", "rgw.objects", "192.0.2.70/24", api.MapValue())))))
	changed, all = withGraph(t, replace(bindingCatalog(), other), graphServices())
	issues := Validate(changed, all)
	requireOnly(t, issues, rgwIngressAt+".spec.first_virtual_router_id", "api.invariant", fmt.Sprintf(routerIDMessage, "StorageCluster/other $.spec.ceph.services[0]"), routerIDRemedy)
	requireNoIssueAt(t, issues, field)
}

func TestRegistryResolvesOmittedNativePorts(t *testing.T) {
	const second = "$.spec.ceph.services[4]"
	monitoring := graphIssues(t, []api.Value{plainRGW(rgwPort(9587)), nfsService(api.MapValue())})
	requireBound(t, monitoring, second+".spec.monitoring_port", "node-1", rgwAt+".spec.rgw_frontend_port")
	if authored := graphIssues(t, []api.Value{plainRGW(rgwPort(9587)), nfsService(m("monitoring_port", 9587))}); !reflect.DeepEqual(monitoring, authored) {
		t.Fatalf("authored monitoring_port differs: %#v", authored)
	}
	defaulted := graphIssues(t, []api.Value{plainRGW(qualifiedRGWSpec().Without("rgw_frontend_port")), nfsService(m("port", 80))})
	requireBound(t, defaulted, second+".spec.port", "node-1", rgwAt+".spec.rgw_frontend_port")
	if authored := graphIssues(t, []api.Value{plainRGW(rgwPort(80)), nfsService(m("port", 80))}); !reflect.DeepEqual(defaulted, authored) {
		t.Fatalf("authored rgw_frontend_port differs: %#v", authored)
	}
	tls := []api.Value{plainRGW(qualifiedRGWSpec().Without("rgw_frontend_port").With("ssl", api.BoolValue(true))), nfsService(m("port", 443))}
	requireBound(t, graphIssues(t, tls, leaves("rgw.objects", "/spec/ssl_cert", "tls-a", "/spec/ssl_key", "tls-a")), second+".spec.port", "node-1", rgwAt+".spec.rgw_frontend_port")
	services := graphServices()
	services[0] = plainRGW(rgwPort(1024))
	peer := graphIssues(t, services)
	requireBound(t, peer, nfsIngressAt+".spec.haproxy_peer_communication_port", "node-1", rgwAt+".spec.rgw_frontend_port")
	services[3] = withIngressSpec(services[3], "haproxy_peer_communication_port", 1024)
	if authored := graphIssues(t, services); !reflect.DeepEqual(peer, authored) {
		t.Fatalf("authored peer port differs: %#v", authored)
	}
	field := "$.spec.ceph.services[3].spec.port"
	requireOnly(t, graphIssues(t, []api.Value{nfsService(m("port", 31311))}), field, "api.invariant", qosListener, "set "+field+" to another port, or place the two services on disjoint hosts")
	first := nfsService(m("port", 12049, "monitoring_port", 9600))
	more := nfsService(m("port", 12050, "monitoring_port", 9601)).With("service_id", api.StringValue("more"))
	requireOnly(t, graphIssues(t, []api.Value{first, more}), second+".placement", "api.invariant", qosListener, "place the two services on disjoint hosts")
}

func TestHAProxyProtocolNFSBindsAllAddresses(t *testing.T) {
	services := graphServices()
	services[0] = rgwService(rgwPort(12049))
	services[2] = nfsService(m("port", 12049, "virtual_ip", "192.0.2.60", "enable_haproxy_protocol", true))
	requireBound(t, graphIssues(t, services), nfsAt+".spec.port", "node-1", rgwAt+".spec.rgw_frontend_port")
}

func TestListenerRegistryStaysOnItsCluster(t *testing.T) {
	services := graphServices()
	services[0] = rgwService(rgwPort(6800))
	services[3] = services[3].With("spec", services[3].Get("spec").Without("first_virtual_router_id"))
	cluster, catalog := withGraph(t, bindingCatalog(), services)
	issues := Validate(cluster, catalog)
	if len(issuesAt(issues, rgwAt+".spec.rgw_frontend_port")) != 1 || len(issuesAt(issues, nfsIngressAt+".spec.first_virtual_router_id")) != 1 {
		t.Fatalf("cluster admitted its conflicts: %#v", issues)
	}
	children := 0
	for _, object := range catalog.Objects() {
		if object.Identity() == cluster.Identity() {
			continue
		}
		children++
		if found := issuesUnder(Validate(object, catalog), "$.spec.ceph.services"); len(found) != 0 {
			t.Fatalf("%s carries its cluster's listener issues: %#v", object.Identity(), found)
		}
	}
	if children == 0 {
		t.Fatal("catalog has no other objects")
	}
}

func TestListenerRegistryCoversIngressPortsOnce(t *testing.T) {
	const messenger = "this listener port is reserved by the Ceph messenger range 6800-7568 of OSD, MGR and MDS daemons on host node-1"
	monitor := rgwIngressAt + ".spec.monitor_port"
	services := graphServices()
	services[1] = withIngressSpec(services[1], "monitor_port", 6800)
	requireOnly(t, graphIssues(t, services), monitor, "api.invariant", messenger, "set "+monitor+" to a port that no Ceph daemon reserves on the selected hosts")
	peer := rgwIngressAt + ".spec.haproxy_peer_communication_port"
	services = graphServices()
	services[1] = withIngressSpec(services[1], "haproxy_peer_communication_port", 6800)
	found := issuesAt(graphIssues(t, services), peer)
	if len(found) != 2 || found[0].Message == found[1].Message || (found[0].Message != messenger && found[1].Message != messenger) {
		t.Fatalf("issues at %s: %#v", peer, found)
	}
	more := plainRGW(rgwPort(2049)).With("service_id", api.StringValue("more"))
	requireBound(t, graphIssues(t, []api.Value{plainRGW(rgwPort(2049)), more, nfsService(api.MapValue())}), "$.spec.ceph.services[5].spec.port", "node-1", rgwAt+".spec.rgw_frontend_port")
	services = graphServices()
	services[1] = withIngressSpec(services[1], "frontend_port", 1967)
	issues := graphIssues(t, services)
	requireOnly(t, issues, rgwIngressAt+".spec.frontend_port", "api.invariant", "ingress frontend, monitor and peer ports must differ", "")
	requireNoIssueAt(t, issues, monitor)
	services = graphServices()
	services[3] = withIngressSpec(services[3], "frontend_port", 1024)
	issues = graphIssues(t, services)
	requireOnly(t, issues, nfsIngressAt+".spec.frontend_port", "api.invariant", "ingress frontend, monitor and peer ports must differ", "")
	requireNoIssueAt(t, issues, nfsIngressAt+".spec.haproxy_peer_communication_port")
}
