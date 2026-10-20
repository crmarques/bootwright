package storage

import (
	"fmt"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func gatewayPair(gatewayVIP, ingressVIP string, extra ...any) []api.Value {
	gateway := b479Service("mgmt-gateway", "port", 9443)
	if gatewayVIP != "" {
		gateway = b479Service("mgmt-gateway", "port", 9443, "virtual_ip", gatewayVIP)
	}
	spec := m(append([]any{"backend_service", "mgmt-gateway", "virtual_ip", ingressVIP, "keepalive_only", true}, extra...)...)
	return []api.Value{gateway, m("service_type", "ingress", "service_id", "mgmt-gateway", "placement", m("label", "mgr"), "spec", spec)}
}

func requirePairAdmits(t *testing.T, issues []api.Issue) {
	t.Helper()
	if found := append(issuesUnder(issues, "$.spec.ceph.services[3]"), issuesUnder(issues, "$.spec.ceph.services[4]")...); len(found) != 0 {
		t.Fatalf("management gateway pair refused: %#v", found)
	}
}

func TestMgmtGatewayVirtualIPNeedsKeepaliveIngress(t *testing.T) {
	const gatewayVIP, ingressVIP = "$.spec.ceph.services[3].spec.virtual_ip", "$.spec.ceph.services[4].spec.virtual_ip"
	requirePairAdmits(t, graphIssues(t, gatewayPair("192.0.2.50", "192.0.2.50")))
	requireOnly(t, graphIssues(t, gatewayPair("192.0.2.50", "192.0.2.50")[:1]), gatewayVIP, "api.invariant", "a management gateway virtual_ip works only through a keepalive-only ingress that fronts the gateway", "declare an ingress service with spec.backend_service: mgmt-gateway, spec.keepalive_only: true and the same spec.virtual_ip")
	requireOnly(t, graphIssues(t, gatewayPair("", "192.0.2.50")), ingressVIP, "api.invariant", "a management gateway ingress carries the gateway's virtual_ip, and the gateway declares none", "set the management gateway's spec.virtual_ip to this ingress's address, or remove this ingress")
	catalog := bindingCatalog()
	cluster := find(t, catalog, api.StorageCluster, "storage")
	catalog = replace(catalog, cluster.WithSpec(cluster.Spec().WithPath(api.StringList("192.0.2.0/24", "2001:db8::/64"), "ceph", "networks", "publicCIDRs")))
	dual, all := withGraph(t, catalog, gatewayPair("2001:db8::50", "2001:DB8:0::50"))
	requirePairAdmits(t, Validate(dual, all))
	requireOnly(t, graphIssues(t, gatewayPair("192.0.2.50", "192.0.2.51")), ingressVIP, "api.invariant", "a management gateway ingress virtual_ip must equal the gateway's virtual_ip", "set spec.virtual_ip to the management gateway's spec.virtual_ip")
	requireOnly(t, graphIssues(t, gatewayPair("192.0.2.50", "192.0.2.50/24")), ingressVIP, "api.invariant", "an ingress virtual_ip for the management gateway is a bare address without a prefix length", "write spec.virtual_ip as the management gateway's spec.virtual_ip, without a prefix length")
	pair := gatewayPair("192.0.2.50", "192.0.2.50")
	pair[1] = pair[1].With("spec", pair[1].Get("spec").Without("keepalive_only"))
	issues := graphIssues(t, pair)
	requireOnly(t, issues, "$.spec.ceph.services[4].spec.keepalive_only", "api.invariant", "an ingress whose backend is the management gateway is keepalive-only; the gateway serves its own port", "set spec.keepalive_only: true")
	requireNoIssueAt(t, issues, "$.spec.ceph.services[4].spec.frontend_port", "$.spec.ceph.services[4].spec.monitor_port")
	for address, message := range map[string]string{
		"192.0.2.50/24": "a management gateway virtual_ip is a bare address without a prefix length",
		"198.51.100.7":  "a management gateway virtual_ip lies inside a declared public network, so a host interface carries it",
	} {
		requireOnly(t, graphIssues(t, gatewayPair(address, "")[:1]), gatewayVIP, "api.invariant", message, "")
	}
}

func TestIngressArmRefusesHAProxyMembers(t *testing.T) {
	const ingress = "$.spec.ceph.services[4].spec."
	for member, port := range map[string]int{"frontend_port": 8080, "monitor_port": 1967} {
		requireOnly(t, graphIssues(t, gatewayPair("192.0.2.50", "192.0.2.50", member, port)), ingress+member, "api.invariant", "a keepalive-only management gateway ingress runs no HAProxy, so spec."+member+" has no listener", "omit spec."+member)
	}
	for member, arm := range map[string]string{"enable_haproxy_protocol": "nfs", "ssl": "rgw"} {
		requireOnly(t, graphIssues(t, gatewayPair("192.0.2.50", "192.0.2.50", member, true)), ingress+member, "api.invariant", "spec."+member+" applies only to an ingress whose backend is a "+arm+" service", "omit spec."+member+" for this backend")
	}
	issues := graphIssues(t, gatewayPair("192.0.2.50", "192.0.2.50"), leaves("ingress.mgmt-gateway", "/spec/ssl_cert", "tls-a"))
	oneIssueAt(t, issues, "$.spec.ceph.serviceBindings[0].secrets[0].path", "secret leaf is not qualified for this service schema")
}

func TestMgmtGatewayIngressRouterIDUsesItsPublicNetwork(t *testing.T) {
	const field = "$.spec.ceph.services[8].spec.first_virtual_router_id"
	services := append(graphServices(), gatewayPair("192.0.2.50", "192.0.2.50")...)
	requireOnly(t, graphIssues(t, services), field, "api.invariant", fmt.Sprintf(routerIDMessage, rgwIngressAt), routerIDRemedy)
	services = append(graphServices(), gatewayPair("192.0.2.50", "192.0.2.50", "first_virtual_router_id", 52)...)
	issues := graphIssues(t, services)
	requireNoIssueAt(t, issues, field)
	requireGraphAdmits(t, issues)
	catalog := bindingCatalog()
	cluster := find(t, catalog, api.StorageCluster, "storage")
	catalog = replace(catalog, cluster.WithSpec(cluster.Spec().WithPath(api.StringList("192.0.2.0/24", "198.51.100.0/24"), "ceph", "networks", "publicCIDRs")))
	services = append(graphServices(), gatewayPair("192.0.2.50", "192.0.2.50")...)
	services[1] = graphIngress("rgw.objects", "rgw.objects", "198.51.100.61/24", m("frontend_port", 8080, "monitor_port", 1967, "virtual_interface_networks", api.StringList("192.0.2.128/25")))
	changed, all := withGraph(t, catalog, services)
	requireOnly(t, Validate(changed, all), field, "api.invariant", fmt.Sprintf(routerIDMessage, rgwIngressAt), routerIDRemedy)
}
