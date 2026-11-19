package ceph

import (
	"context"
	"net"
	"reflect"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

const managementFQDN = "mgr.example.ceph.example.test"

func withManagementFQDN(catalog api.Catalog, fqdn string) api.Catalog {
	objects := catalog.Objects()
	for i, object := range objects {
		if object.Kind() == api.CephCluster && object.Name() == "example" {
			objects[i] = object.WithSpec(object.Spec().WithPath(api.StringValue(fqdn), "managementGateway", "fqdn"))
		}
	}
	return api.NewCatalog(objects)
}

func TestGatewayUnitRequestCarriesEndpointAndMonitoring(t *testing.T) {
	services := append(vipGateway("192.0.2.60"), auxiliaryService("prometheus", "", api.Value{}), auxiliaryService("grafana", "", api.Value{}), auxiliaryService("node-exporter", "", api.Value{}))
	catalog := withManagementFQDN(gatewayCatalog(t, services, nil), managementFQDN)
	unit := gatewayUnitOf(t, catalog)
	if unit.Endpoint != managementFQDN || !slices.Equal(unit.Monitoring, []string{"grafana", "prometheus"}) {
		t.Fatalf("gateway unit endpoint %q monitoring %v", unit.Endpoint, unit.Monitoring)
	}
	r, err := DecodeRequest(gatewayUnitBlock(t, catalog).Request)
	if err != nil || r.Service == nil || r.Service.Endpoint != managementFQDN || !slices.Equal(r.Service.Monitoring, []string{"grafana", "prometheus"}) {
		t.Fatalf("frozen gateway unit %+v (%v)", r.Service, err)
	}
	monitoring := blockOf(t, cephPlan(t, catalog), "ceph-service-example-monitoring")
	if r, err := DecodeRequest(monitoring.Request); err != nil || r.Service.Endpoint != "" || r.Service.Monitoring != nil {
		t.Fatalf("the monitoring unit carries gateway members %+v (%v)", r.Service, err)
	}
	bare := gatewayUnitOf(t, gatewayCatalog(t, vipGateway("192.0.2.60"), nil))
	if bare.Endpoint != "" || bare.Monitoring != nil {
		t.Fatalf("a gateway without a management FQDN or monitoring carries %q %v", bare.Endpoint, bare.Monitoring)
	}
}

func TestGatewayCertificateMustCoverTheManagementFQDN(t *testing.T) {
	ctx, capability := context.Background(), New(api.CephCluster, nil)
	bindings := []api.Value{tlsBinding("mgmt-gateway", "mgmt-tls")}
	unnamed := func(err error) {
		t.Helper()
		refusedWith(t, err, "secret.part", "the certificate of Secret mgmt-tls does not name "+managementFQDN+", the name clients use for the management gateway of CephCluster example", "reissue the certificate with "+managementFQDN+
			" as a DNS name (or a matching wildcard) in its subject alternative names and store it with "+gatewaySet+", or set spec.managementGateway.fqdn of CephCluster example to a DNS name the certificate already holds, then apply again")
		if strings.Contains(diagnostics.Of(err)[0].Message+diagnostics.Of(err)[0].Remediation, gatewaySentinel) {
			t.Fatal("the refusal carries bound material")
		}
	}
	vip := gatewayUnitBlock(t, withManagementFQDN(gatewayCatalog(t, vipGateway("192.0.2.60"), bindings), managementFQDN))
	unnamed(capability.ProveBinding(ctx, "example", vip, mgmtMaterial(leafPEM(t, []string{"node-1"}, net.ParseIP("192.0.2.60")))))
	for _, names := range [][]string{{managementFQDN}, {"*.example.ceph.example.test"}} {
		if err := capability.ProveBinding(ctx, "example", vip, mgmtMaterial(leafPEM(t, names, net.ParseIP("192.0.2.60")))); err != nil {
			t.Fatalf("a certificate naming %v and the virtual IP refuses %+v", names, diagnostics.Of(err))
		}
	}
	hosts := gatewayUnitBlock(t, withManagementFQDN(gatewayCatalog(t, []api.Value{hostGateway("node-1", "node-2")}, bindings), managementFQDN))
	unnamed(capability.ProveBinding(ctx, "example", hosts, mgmtMaterial(leafPEM(t, []string{"node-1", "node-2"}))))
	if err := capability.ProveBinding(ctx, "example", hosts, mgmtMaterial(leafPEM(t, []string{"node-1", "node-2", managementFQDN}))); err != nil {
		t.Fatalf("a certificate naming every host and the management FQDN refuses %+v", diagnostics.Of(err))
	}
}

func managementBlock(t *testing.T, fqdn string, services ...string) reconciliation.Block {
	t.Helper()
	unit := gatewayUnitOf(t, gatewayCatalog(t, vipGateway("192.0.2.60"), nil))
	unit.Endpoint, unit.Monitoring = fqdn, services
	r := runtimeRequest()
	r.Cluster, r.Action, r.Service = "storage", "service", &unit
	f := &endpointFixture{}
	return f.block(t, string(api.CephCluster), "storage", r)
}

func TestInspectComposesTheActiveDashboardURLFromTheNodeFQDN(t *testing.T) {
	catalog, cluster := inspectedCluster()
	proofs := provedPair(t)
	proofs[1].Evidence = inspectedClusterEvidence(t, func(e *Evidence) { e.Dashboard.URL = "https://192.0.2.11:8443/" })
	got, reason := Inspect(catalog, cluster, nil, proofs)
	want := []DashboardURL{{Host: "rhel-01", URL: "https://rhel-01.lab.test:8443/", Role: DashboardActive}, {Host: "rhel-03", URL: "https://rhel-03:8443/", Role: DashboardStandby}}
	if reason != "" || got.Dashboard.Status != DashboardReady || !reflect.DeepEqual(got.Dashboard.URLs, want) || got.Monitoring != nil {
		t.Fatalf("inspection %+v (%q)", got, reason)
	}
}

func TestInspectDashboardLeadsWithTheManagementGateway(t *testing.T) {
	catalog, cluster := inspectedCluster()
	proofs := provedPair(t)
	proofs[1].Evidence = inspectedClusterEvidence(t, func(e *Evidence) { e.Dashboard.URL = "https://192.0.2.11:8443/" })
	frozen := []reconciliation.Block{managementBlock(t, "mgr.storage.lab.test", "alertmanager", "grafana", "prometheus")}
	got, reason := Inspect(catalog, cluster, frozen, proofs)
	urls := []DashboardURL{{Host: "mgmt-gateway", URL: "https://mgr.storage.lab.test:9443/", Role: DashboardGateway}, {Host: "rhel-01", URL: "https://rhel-01.lab.test:8443/", Role: DashboardActive}}
	monitoring := []MonitoringInspection{{Service: "alertmanager", URL: "https://mgr.storage.lab.test:9443/alertmanager/"}, {Service: "grafana", URL: "https://mgr.storage.lab.test:9443/grafana/"},
		{Service: "prometheus", URL: "https://mgr.storage.lab.test:9443/prometheus/"}}
	if reason != "" || !reflect.DeepEqual(got.Dashboard.URLs, urls) || !reflect.DeepEqual(got.Monitoring, monitoring) {
		t.Fatalf("inspection %+v (%q)", got, reason)
	}
	unnamed := []reconciliation.Block{managementBlock(t, "")}
	got, reason = Inspect(catalog, cluster, unnamed, proofs)
	if reason != "" || len(got.Dashboard.URLs) != 2 || got.Dashboard.URLs[0] != (DashboardURL{Host: "mgmt-gateway", URL: "https://192.0.2.60:9443/", Role: DashboardGateway}) || got.Monitoring != nil {
		t.Fatalf("a gateway without a management FQDN %+v (%q)", got, reason)
	}
	unserved := provedPair(t)
	unserved[1].Evidence = inspectedClusterEvidence(t, func(e *Evidence) { e.Dashboard = DashboardEvidence{CertificateSHA256: inspectedCertificate} })
	got, reason = Inspect(catalog, cluster, frozen, unserved)
	if reason != "" || got.Dashboard.Status != DashboardUnproved || len(got.Dashboard.URLs) != 1 || got.Dashboard.URLs[0].Role != DashboardGateway {
		t.Fatalf("an unserved dashboard behind a gateway %+v (%q)", got, reason)
	}
	other := []reconciliation.Block{managementBlock(t, "mgr.storage.lab.test")}
	other[0].Object = "elsewhere"
	if got, _ := Inspect(catalog, cluster, other, proofs); len(got.Dashboard.URLs) != 2 || got.Dashboard.URLs[0].Role != DashboardActive {
		t.Fatalf("another cluster's gateway led the dashboard %+v", got.Dashboard)
	}
}
