package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/environment/inspection"
	"github.com/crmarques/bootwright/internal/secrets"
)

func gatewayCeph() *inspection.ManagedCeph {
	ceph := readyCeph()
	ceph.Dashboard.URLs = []inspection.DashboardURL{
		{Host: "mgmt-gateway", URL: "https://mgr.storage.lab.test:9443/", Role: inspection.DashboardGateway},
		{Host: "rhel-01", URL: "https://rhel-01.lab.test:8443/", Role: inspection.DashboardActive},
	}
	ceph.Monitoring = []inspection.MonitoringAccess{
		{Service: "alertmanager", URL: "https://mgr.storage.lab.test:9443/alertmanager/"},
		{Service: "grafana", URL: "https://mgr.storage.lab.test:9443/grafana/"},
		{Service: "prometheus", URL: "https://mgr.storage.lab.test:9443/prometheus/"},
	}
	ceph.Endpoints[0].URLs = []string{"https://mgr.storage.lab.test:9443/"}
	return ceph
}

func TestClusterInfoShowsGatewayDashboardAndMonitoringPaths(t *testing.T) {
	var out bytes.Buffer
	if err := writeClusterInfo(WithPresentation(&out, PresentationOptions{Format: "text", Width: 220, Detail: "compact"}), io.Discard, "cluster info", storageOnly(gatewayCeph()), false); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(out.String(), "\n")
	for _, want := range [][]string{
		{"Dashboard", "https://mgr.storage.lab.test:9443/", "(via", "management", "gateway)"},
		{"Direct", "https://rhel-01.lab.test:8443/", "(active", "MGR", "rhel-01", "at", "last", "observation)"},
		{"Alertmanager", "https://mgr.storage.lab.test:9443/alertmanager/"},
		{"Grafana", "https://mgr.storage.lab.test:9443/grafana/"},
		{"Prometheus", "https://mgr.storage.lab.test:9443/prometheus/"},
	} {
		if !hasFields(lines, want...) {
			t.Fatalf("gateway access misses %v:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "standby") {
		t.Fatalf("a gateway dashboard lists a standby manager:\n%s", out.String())
	}
	out.Reset()
	if err := writeClusterInfo(WithPresentation(&out, PresentationOptions{Format: "json"}), io.Discard, "cluster info", storageOnly(gatewayCeph()), true); err != nil {
		t.Fatal(err)
	}
	var shown struct {
		Result struct {
			Storage []struct {
				Dashboard struct {
					URLs []dashboardURLPresentation `json:"urls"`
				} `json:"dashboard"`
				Monitoring []monitoringPresentation `json:"monitoring"`
			} `json:"storage"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out.Bytes(), &shown); err != nil || len(shown.Result.Storage) != 1 {
		t.Fatalf("json %s (%v)", out.String(), err)
	}
	ceph := shown.Result.Storage[0]
	if len(ceph.Dashboard.URLs) != 2 || ceph.Dashboard.URLs[0].Role != "gateway" || len(ceph.Monitoring) != 3 ||
		ceph.Monitoring[1] != (monitoringPresentation{Service: "grafana", URL: "https://mgr.storage.lab.test:9443/grafana/"}) {
		t.Fatalf("json gateway access %+v", ceph)
	}
	out.Reset()
	if err := writeClusterInfo(WithPresentation(&out, PresentationOptions{Format: "json"}), io.Discard, "cluster info", storageOnly(readyCeph()), true); err != nil || strings.Contains(out.String(), `"monitoring"`) {
		t.Fatalf("json without a gateway names monitoring (%v):\n%s", err, out.String())
	}
}

func TestClusterInfoGatewayRowsMustBeConsistent(t *testing.T) {
	if !validClusterInfo(storageOnly(gatewayCeph())) {
		t.Fatal("the gateway fixture is not valid")
	}
	for name, edit := range map[string]func(*inspection.ManagedCeph){
		"a standby beside a gateway": func(c *inspection.ManagedCeph) {
			c.Dashboard.URLs = append(c.Dashboard.URLs, inspection.DashboardURL{Host: "rhel-02", URL: "https://rhel-02.lab.test:8443/", Role: inspection.DashboardStandby})
		},
		"a gateway after the active manager": func(c *inspection.ManagedCeph) {
			c.Dashboard.URLs[0], c.Dashboard.URLs[1] = c.Dashboard.URLs[1], c.Dashboard.URLs[0]
		},
		"an unknown monitored service": func(c *inspection.ManagedCeph) { c.Monitoring[0].Service = "loki" },
		"unsorted monitoring":          func(c *inspection.ManagedCeph) { c.Monitoring[0], c.Monitoring[1] = c.Monitoring[1], c.Monitoring[0] },
		"a plaintext monitoring URL": func(c *inspection.ManagedCeph) {
			c.Monitoring[0].URL = "http://mgr.storage.lab.test:9443/alertmanager/"
		},
		"monitoring without a gateway": func(c *inspection.ManagedCeph) { c.Dashboard.URLs = c.Dashboard.URLs[1:] },
	} {
		ceph := gatewayCeph()
		edit(ceph)
		if validClusterInfo(storageOnly(ceph)) {
			t.Errorf("%s was accepted", name)
		}
	}
}

func revealedGatewayCeph(anonymous, unproved bool) *inspection.ManagedCeph {
	ceph := gatewayCeph()
	ceph.Dashboard.Password = secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(clusterDashboardPassword)})
	ceph.Dashboard.Revealed = true
	logins := map[string][2]string{"alertmanager": {"am-admin", "am-Secret.42"}, "grafana": {"admin", "gr-Secret.99"}, "prometheus": {"pr-admin", "pr-Secret.17"}}
	for i := range ceph.Monitoring {
		access := &ceph.Monitoring[i]
		if anonymous && access.Service == "grafana" {
			access.Anonymous = true
			continue
		}
		access.User, access.Revealed = logins[access.Service][0], true
		access.Password = secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(logins[access.Service][1])})
		access.Warn = unproved && access.Service == "prometheus"
	}
	return ceph
}

func TestClusterInfoSecretsShowsEachMonitoringLogin(t *testing.T) {
	var out bytes.Buffer
	if err := writeClusterInfo(WithPresentation(&out, PresentationOptions{Format: "text", Width: 220, Detail: "compact"}), io.Discard, "cluster info", storageOnly(revealedGatewayCeph(false, false)), false); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(out.String(), "\n")
	for _, want := range [][]string{
		{"Alertmanager", "user", "am-admin"}, {"Alertmanager", "password", "am-Secret.42"},
		{"Grafana", "user", "admin"}, {"Grafana", "password", "gr-Secret.99"},
		{"Prometheus", "user", "pr-admin"}, {"Prometheus", "password", "pr-Secret.17"},
	} {
		if !hasFields(lines, want...) {
			t.Fatalf("the revealed custody misses %v:\n%s", want, out.String())
		}
	}
	out.Reset()
	if err := writeClusterInfo(WithPresentation(&out, PresentationOptions{Format: "text", Width: 220, Detail: "compact"}), io.Discard, "cluster info", storageOnly(revealedGatewayCeph(true, false)), false); err != nil {
		t.Fatal(err)
	}
	if !hasFields(strings.Split(out.String(), "\n"), "Grafana", "login", "anonymous", "access;", "no", "admin", "password", "is", "bound") || strings.Contains(out.String(), "gr-Secret.99") {
		t.Fatalf("an anonymous Grafana renders:\n%s", out.String())
	}
	out.Reset()
	if err := writeClusterInfo(WithPresentation(&out, PresentationOptions{Format: "json"}), io.Discard, "cluster info", storageOnly(revealedGatewayCeph(true, false)), true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"service":"grafana","url":"https://mgr.storage.lab.test:9443/grafana/","anonymous":true}`) ||
		!strings.Contains(out.String(), `"service":"prometheus","url":"https://mgr.storage.lab.test:9443/prometheus/","user":"pr-admin","passwordValue":"pr-Secret.17"}`) {
		t.Fatalf("json custody:\n%s", out.String())
	}
	if warnings := dashboardWarnings(storageOnly(revealedGatewayCeph(false, true))); len(warnings) != 1 || warnings[0].Code != "access.unproved" || !strings.Contains(warnings[0].Message, "Prometheus") {
		t.Fatalf("an unproved login warns %+v", warnings)
	}
	for name, edit := range map[string]func(*inspection.ManagedCeph){
		"an anonymous Prometheus": func(c *inspection.ManagedCeph) {
			c.Monitoring[2] = inspection.MonitoringAccess{Service: "prometheus", URL: c.Monitoring[2].URL, Anonymous: true}
		},
		"an anonymous revealed Grafana": func(c *inspection.ManagedCeph) { c.Monitoring[1].Anonymous = true },
		"a warning unrevealed": func(c *inspection.ManagedCeph) {
			c.Monitoring[0] = inspection.MonitoringAccess{Service: "alertmanager", URL: c.Monitoring[0].URL, Warn: true}
		},
	} {
		ceph := revealedGatewayCeph(false, false)
		edit(ceph)
		if validClusterInfo(storageOnly(ceph)) {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestClusterInfoGatewayAttributesEachCertificateToItsURL(t *testing.T) {
	render := func(ceph *inspection.ManagedCeph) []string {
		var out bytes.Buffer
		if err := writeClusterInfo(WithPresentation(&out, PresentationOptions{Format: "text", Width: 220, Detail: "compact"}), io.Discard, "cluster info", storageOnly(ceph), false); err != nil {
			t.Fatal(err)
		}
		return strings.Split(out.String(), "\n")
	}
	manager := "AB" + strings.Repeat(":0C", 31)
	gateway := "CD" + strings.Repeat(":0E", 31)
	warning := []string{"compare", "before", "accepting", "the", "browser", "warning)"}
	lines := render(gatewayCeph())
	if !hasFields(lines, append([]string{"Certificate", "SHA-256", gateway, "(cephadm-signed;"}, warning...)...) {
		t.Fatalf("the gateway URL lacks its own leaf:\n%s", strings.Join(lines, "\n"))
	}
	if !hasFields(lines, append([]string{"Direct", "SHA-256", manager, "(self-signed;"}, warning...)...) {
		t.Fatalf("the direct URL lacks the manager's leaf:\n%s", strings.Join(lines, "\n"))
	}
	if hasFields(lines, append([]string{"Certificate", "SHA-256", manager, "(self-signed;"}, warning...)...) {
		t.Fatalf("the manager's leaf is attributed to the gateway URL:\n%s", strings.Join(lines, "\n"))
	}
	bound := gatewayCeph()
	bound.Endpoints[0].Issuer = inspection.IssuerBound
	if lines := render(bound); !hasFields(lines, "Certificate", "SHA-256", gateway, "(bound", "certificate)") {
		t.Fatalf("a bound gateway leaf renders:\n%s", strings.Join(lines, "\n"))
	}
	unproved := gatewayCeph()
	unproved.Endpoints[0] = inspection.Endpoint{Service: "mgmt-gateway", URLs: unproved.Endpoints[0].URLs, Issuer: inspection.IssuerCephadm, Status: inspection.EndpointUnproved, Reason: "the apply did not prove this endpoint reachable"}
	for _, line := range render(unproved) {
		if strings.Contains(line, "CD:0E") || strings.Contains(line, "Certificate SHA-256") {
			t.Fatalf("an unproved gateway shows a leaf: %q", line)
		}
	}
}
