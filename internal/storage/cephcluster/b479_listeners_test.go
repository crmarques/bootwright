package cephcluster

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func requireReserved(t *testing.T, issues []api.Issue, field, label string) {
	t.Helper()
	requireOnly(t, issues, field, "api.invariant", "this listener port is reserved by "+label+" on host node-1", "set "+field+" to a port that no Ceph daemon reserves on the selected hosts")
}

func TestB479DefaultPortCollision(t *testing.T) {
	const s3, s4, s5 = "$.spec.services[3]", "$.spec.services[4]", "$.spec.services[5]"
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
	requireBound(t, graphIssues(t, []api.Value{b479Service("loki"), b479Service("prometheus"), b479Service("alloy", "port", 9095)}), s5+".spec.port", "node-1", s4+".spec.port")
	requireNoIssueAt(t, graphIssues(t, []api.Value{b479Service("loki"), b479Service("alloy"), b479Service("grafana", "port", 12345)}), s5+".spec.port")
}

func TestB479OmittedPortReservesItsDefault(t *testing.T) {
	snmp := b479Service("snmp-gateway", "snmp_version", "V2c", "snmp_destination", "192.0.2.9:162")
	for _, tc := range []struct {
		name     string
		services []api.Value
		later    string
		earlier  string
	}{
		{"grafana 3000", []api.Value{b479Service("grafana"), b479Service("node-exporter", "port", 3000)}, "[4]", "[3]"},
		{"node-exporter 9100", []api.Value{b479Service("node-exporter"), b479Service("grafana", "port", 9100)}, "[4]", "[3]"},
		{"ceph-exporter 9926", []api.Value{b479Service("ceph-exporter"), b479Service("grafana", "port", 9926)}, "[4]", "[3]"},
		{"snmp-gateway 9464", []api.Value{snmp, b479Service("alertmanager"), b479Service("grafana", "port", 9464)}, "[5]", "[3]"},
		{"alertmanager 9093", []api.Value{b479Service("alertmanager"), b479Service("grafana", "port", 9093)}, "[4]", "[3]"},
		{"loki 3100", []api.Value{b479Service("loki"), b479Service("grafana", "port", 3100)}, "[4]", "[3]"},
		{"promtail 9080", []api.Value{b479Service("loki"), b479Service("promtail"), b479Service("grafana", "port", 9080)}, "[5]", "[4]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const at = "$.spec.services"
			requireBound(t, graphIssues(t, tc.services), at+tc.later+".spec.port", "node-1", at+tc.earlier+".spec.port")
		})
	}
}

func TestB479LogCollectionFixedListeners(t *testing.T) {
	const at = "$.spec.services"
	for _, tc := range []struct {
		name     string
		services []api.Value
		later    string
		label    string
	}{
		{"loki authored port keeps HTTP 3100", []api.Value{b479Service("loki", "port", 3200), b479Service("grafana", "port", 3100)}, "[4]", "the loki HTTP listener (3100) of " + at + "[3]"},
		{"loki gRPC 8080", []api.Value{b479Service("loki"), b479Service("grafana", "port", 8080)}, "[4]", "the loki gRPC listener (8080) of " + at + "[3]"},
		{"promtail authored port keeps 9080", []api.Value{b479Service("loki"), b479Service("promtail", "port", 9180), b479Service("grafana", "port", 9080)}, "[5]", "the promtail listener (9080) of " + at + "[4]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			field := at + tc.later + ".spec.port"
			found := issuesAt(graphIssues(t, tc.services), field)
			want := api.Issue{Code: "api.invariant", Field: field, Message: "this listener port is already bound on host node-1 by " + tc.label, Remediation: "set " + field + " to another port, or place the two services on disjoint hosts"}
			if len(found) != 1 || found[0] != want {
				t.Fatalf("issues at %s: %#v", field, found)
			}
		})
	}
	for _, services := range [][]api.Value{
		{b479Service("loki"), b479Service("promtail")},
		{b479Service("loki", "port", 8080), b479Service("promtail", "port", 9080)},
	} {
		if found := addedServiceIssues(graphIssues(t, services)); len(found) != 0 {
			t.Fatalf("log collection collided with its own fixed listeners: %#v", found)
		}
	}
}

func TestB479NetworkBoundListenersCoexistWithAVIP(t *testing.T) {
	const at = "$.spec.services[7].spec.port"
	for _, serviceType := range []string{"grafana", "prometheus"} {
		t.Run(serviceType, func(t *testing.T) {
			bound := b479Service(serviceType, "port", 2049, "only_bind_port_on_networks", true).With("networks", api.StringList("192.0.2.0/24"))
			requireGraphAdmits(t, graphIssues(t, append(keepaliveServices(), bound)))
			requireBound(t, graphIssues(t, append(keepaliveServices(), b479Service(serviceType, "port", 2049))), at, "node-1", nfsAt+".spec.port")
		})
	}
}
