package inspection

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/secrets"
)

const gatewayURL = "https://mgr.storage.lab.test:8443/"

func (f *fixture) MonitoringCredentials(_ context.Context, contextName, cluster, service string) (Custodied, bool, error) {
	f.monitoringRead = append(f.monitoringRead, contextName+"/"+cluster+"/"+service)
	found, ok := f.logins[service]
	return found, ok, f.readError
}

func (f *fixture) GrafanaPassword(_ context.Context, contextName, cluster string) (Custodied, bool, error) {
	f.monitoringRead = append(f.monitoringRead, contextName+"/"+cluster+"/grafana")
	if f.grafana == nil {
		return Custodied{}, false, f.readError
	}
	return *f.grafana, true, f.readError
}

func login(user, password string) Custodied {
	return Custodied{Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.UsernamePart: []byte(user), secrets.PasswordPart: []byte(password)})}
}

func gatewayFixture(t *testing.T) *fixture {
	f := newFixture(t)
	proof := f.cephFor["storage"]
	proof.Dashboard.URLs = []DashboardURL{{Host: "mgmt-gateway", URL: gatewayURL, Role: DashboardGateway}, {Host: "rhel-01", URL: "https://rhel-01.lab.test:8443/", Role: DashboardActive}}
	proof.Monitoring = []MonitoringAccess{{Service: "alertmanager", URL: gatewayURL + "alertmanager/"}, {Service: "grafana", URL: gatewayURL + "grafana/"}, {Service: "prometheus", URL: gatewayURL + "prometheus/"}}
	f.cephFor["storage"] = proof
	f.logins = map[string]Custodied{"alertmanager": login("am-admin", "am-Secret.42"), "prometheus": login("pr-admin", "pr-Secret.17")}
	grafana := Custodied{Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("gr-Secret.99")})}
	f.grafana = &grafana
	return f
}

func monitoringLogin(access MonitoringAccess) (string, string) {
	value, _ := access.Password.Part(secrets.ValuePart)
	defer clear(value)
	return access.User, string(value)
}

func cleared(material secrets.Material) bool {
	for _, part := range material.Parts() {
		if value, _ := material.Part(part); strings.Trim(string(value), "\x00") != "" {
			return false
		}
	}
	return true
}

func TestSecretsRevealsEachMonitoringLogin(t *testing.T) {
	f := gatewayFixture(t)
	result, err := f.service("lab").ClusterInfo(context.Background(), ClusterInfoRequest{Names: []string{"storage"}})
	if err != nil || len(f.monitoringRead) != 0 || slices.ContainsFunc(result.Storage[0].Ceph.Monitoring, func(a MonitoringAccess) bool { return a.Revealed || a.User != "" }) {
		t.Fatalf("an inspection without --secrets read %v and returned %+v (%v)", f.monitoringRead, result, err)
	}
	result, err = f.service("lab").ClusterInfo(context.Background(), ClusterInfoRequest{Names: []string{"storage"}, Secrets: true})
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	want := map[string][2]string{"alertmanager": {"am-admin", "am-Secret.42"}, "grafana": {"admin", "gr-Secret.99"}, "prometheus": {"pr-admin", "pr-Secret.17"}}
	rows := result.Storage[0].Ceph.Monitoring
	for _, access := range rows {
		user, password := monitoringLogin(access)
		if !access.Revealed || access.Anonymous || [2]string{user, password} != want[access.Service] || !slices.Equal(access.Password.Parts(), []secrets.Part{secrets.ValuePart}) {
			t.Fatalf("%s reveals %+v", access.Service, access)
		}
	}
	if len(rows) != 3 || !slices.Equal(f.monitoringRead, []string{"lab/storage/alertmanager", "lab/storage/grafana", "lab/storage/prometheus"}) {
		t.Fatalf("the reveal read %v into %+v", f.monitoringRead, rows)
	}
	if !cleared(f.logins["prometheus"].Material) {
		t.Fatal("the custodied login stayed beside its revealed copy")
	}
	ClearSecrets(result)
	f = gatewayFixture(t)
	f.grafana = nil
	result, err = f.service("lab").ClusterInfo(context.Background(), ClusterInfoRequest{Names: []string{"storage"}, Secrets: true})
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if grafana := result.Storage[0].Ceph.Monitoring[1]; grafana.Service != "grafana" || !grafana.Anonymous || grafana.Revealed || grafana.User != "" || len(grafana.Password.Parts()) != 0 {
		t.Fatalf("an anonymous Grafana reveals %+v", grafana)
	}
	ClearSecrets(result)
	f = gatewayFixture(t)
	unproved := f.logins["prometheus"]
	unproved.Unproved = true
	f.logins["prometheus"] = unproved
	result, err = f.service("lab").ClusterInfo(context.Background(), ClusterInfoRequest{Names: []string{"storage"}, Secrets: true})
	if err != nil || !result.Storage[0].Ceph.Monitoring[2].Warn || result.Storage[0].Ceph.Monitoring[0].Warn {
		t.Fatalf("an unproved login warns %+v (%v)", result, err)
	}
	ClearSecrets(result)
}

func TestSecretsRefusesWhenMonitoringCredentialsAreMissing(t *testing.T) {
	for _, service := range []string{"alertmanager", "prometheus"} {
		f := gatewayFixture(t)
		other := f.logins[map[string]string{"alertmanager": "prometheus", "prometheus": "alertmanager"}[service]]
		delete(f.logins, service)
		result, err := f.service("lab").ClusterInfo(context.Background(), ClusterInfoRequest{Names: []string{"storage"}, Secrets: true})
		failure := diagnostics.Of(err)
		if result != nil || len(failure) != 1 || failure[0].Code != "access.unavailable" || failure[0].Remediation != "bootwright apply --context lab --stage clusters" {
			t.Fatalf("a missing %s login returned %+v (%+v)", service, result, failure)
		}
		if !strings.Contains(failure[0].Message, "CephCluster storage") || strings.Contains(failure[0].Message, "Secret.") {
			t.Fatalf("the refusal reads %q", failure[0].Message)
		}
		read := service == "prometheus"
		if read && (!cleared(other.Material) || !cleared(f.grafana.Material)) || !cleared(f.passwords["storage"].Material) {
			t.Fatal("a refused reveal kept a value it had read")
		}
	}
	f := gatewayFixture(t)
	f.logins["prometheus"] = Custodied{Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("no user")})}
	if result, err := f.service("lab").ClusterInfo(context.Background(), ClusterInfoRequest{Names: []string{"storage"}, Secrets: true}); result != nil || err == nil {
		t.Fatalf("a malformed login was revealed: %+v", result)
	}
}

func TestClearSecretsClearsMonitoringPasswords(t *testing.T) {
	f := gatewayFixture(t)
	result, err := f.service("lab").ClusterInfo(context.Background(), ClusterInfoRequest{Names: []string{"storage"}, Secrets: true})
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	rows := slices.Clone(result.Storage[0].Ceph.Monitoring)
	ClearSecrets(result)
	for _, access := range rows {
		if !cleared(access.Password) {
			t.Fatalf("%s kept its password after ClearSecrets", access.Service)
		}
	}
}

// A custody read that fails is returned for the login it read, a Grafana
// password with no value or beside another part is refused as corrupt, and
// every value read is cleared.
func TestSecretsRefusesMonitoringCustodyThatFailsOrIsMalformed(t *testing.T) {
	f := gatewayFixture(t)
	f.readError = errors.New("the custody store is unreadable")
	if result, err := f.service("lab").ClusterInfo(context.Background(), ClusterInfoRequest{Names: []string{"storage"}, Secrets: true}); result != nil || !errors.Is(err, f.readError) {
		t.Fatalf("a failing login read = %+v (%v)", result, err)
	}
	onlyGrafana := func(f *fixture) {
		proof := f.cephFor["storage"]
		proof.Monitoring = []MonitoringAccess{{Service: "grafana", URL: gatewayURL + "grafana/"}}
		f.cephFor["storage"] = proof
	}
	f = gatewayFixture(t)
	onlyGrafana(f)
	f.readError = errors.New("the custody store is unreadable")
	if result, err := f.service("lab").ClusterInfo(context.Background(), ClusterInfoRequest{Names: []string{"storage"}, Secrets: true}); result != nil || !errors.Is(err, f.readError) {
		t.Fatalf("a failing Grafana read = %+v (%v)", result, err)
	}
	for name, parts := range map[string]map[secrets.Part][]byte{
		"no value":       {secrets.ValuePart: nil},
		"another part":   {secrets.PasswordPart: []byte("gr-Secret.99")},
		"beside a login": {secrets.ValuePart: []byte("gr-Secret.99"), secrets.UsernamePart: []byte("admin")},
	} {
		f = gatewayFixture(t)
		onlyGrafana(f)
		malformed := Custodied{Material: secrets.NewMaterial(parts)}
		f.grafana = &malformed
		result, err := f.service("lab").ClusterInfo(context.Background(), ClusterInfoRequest{Names: []string{"storage"}, Secrets: true})
		if reported := diagnostics.Of(err); result != nil || len(reported) != 1 || reported[0].Code != "secret.store.corrupt" || !strings.Contains(reported[0].Message, "CephCluster storage") || !cleared(malformed.Material) {
			t.Errorf("%s = %+v (%v)", name, result, err)
		}
	}
}
