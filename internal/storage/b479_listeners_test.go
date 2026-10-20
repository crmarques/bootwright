package storage

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func requireReserved(t *testing.T, issues []api.Issue, field, label string) {
	t.Helper()
	requireOnly(t, issues, field, "api.invariant", "this listener port is reserved by "+label+" on host node-1", "set "+field+" to a port that no Ceph daemon reserves on the selected hosts")
}

func TestB479DefaultPortCollision(t *testing.T) {
	const s3, s4, s5 = "$.spec.ceph.services[3]", "$.spec.ceph.services[4]", "$.spec.ceph.services[5]"
	requireBound(t, graphIssues(t, []api.Value{b479Service("prometheus"), b479Service("grafana", "port", 9095)}), s4+".spec.port", "node-1", s3+".spec.port")
	requireReserved(t, graphIssues(t, []api.Value{b479Service("mgmt-gateway", "port", 8443)}), s3+".spec.port", "the MGR dashboard (8443)")
	requireReserved(t, graphIssues(t, []api.Value{b479Service("prometheus"), b479Service("node-exporter", "port", 9283)}), s4+".spec.port", "the prometheus manager module (9283)")
	requireNoIssueAt(t, graphIssues(t, []api.Value{b479Service("node-exporter", "port", 9283)}), s3+".spec.port")
	requireReserved(t, graphIssues(t, []api.Value{b479Service("mgmt-gateway", "port", 29443)}), s3+".spec.port", "the management gateway internal listener (29443)")
	requireReserved(t, graphIssues(t, []api.Value{b479Service("grafana", "port", 8765)}), s3+".spec.port", "cephadm service discovery (8765)")
	if found := addedServiceIssues(graphIssues(t, []api.Value{b479Service("ceph-exporter")})); len(found) != 0 {
		t.Fatalf("declared ceph-exporter collided with its own listener: %#v", found)
	}
	proxy := []api.Value{oauth2Proxy(), authGateway(), b479Service("grafana", "port", 4180)}
	secrets := leaves("oauth2-proxy", "/spec/client_secret", "opaque-a", "/spec/cookie_secret", "opaque-a")
	requireBound(t, graphIssues(t, proxy, secrets), s5+".spec.port", "node-1", "oauth2-proxy (4180) of "+s3)
	requireBound(t, graphIssues(t, []api.Value{b479Service("alertmanager"), b479Service("grafana", "port", 9094)}), s4+".spec.port", "node-1", "the Alertmanager cluster listener (9094) of "+s3)
	requireOnly(t, graphIssues(t, []api.Value{b479Service("alertmanager", "port", 9094)}), s3+".spec.port", "api.invariant", "cephadm reserves port 9094 for the Alertmanager cluster listener", "choose another spec.port")
}
