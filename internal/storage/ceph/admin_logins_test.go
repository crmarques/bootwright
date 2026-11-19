package ceph

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/ansible"
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

func gatewayLoginRequest() Request {
	r := coreRequest()
	document := json.RawMessage(`{"placement":{"hosts":["node-1"]},"service_type":"mgmt-gateway"}`)
	r.Action, r.Service = "service", &ServiceUnit{Type: "mgmt-gateway", Documents: []json.RawMessage{document}, Monitoring: []string{"alertmanager", "grafana", "prometheus"},
		Bindings: []ServiceBinding{{Path: "/spec/ssl_cert", SecretRef: "gateway-tls", ServiceRef: "mgmt-gateway"}, {Path: "/spec/ssl_key", SecretRef: "gateway-tls", ServiceRef: "mgmt-gateway"}}}
	return r
}

func monitoringLoginRequest(bound bool) Request {
	r := coreRequest()
	document := json.RawMessage(`{"placement":{"hosts":["node-1"]},"service_type":"grafana"}`)
	r.Action, r.Service = "service", &ServiceUnit{Type: "monitoring", Documents: []json.RawMessage{document}}
	if bound {
		r.Service.Bindings = []ServiceBinding{{Path: "/spec/initial_admin_password", SecretRef: "grafana-admin", ServiceRef: "grafana"}}
	}
	return r
}

func loginExecution(t *testing.T, r Request) lifecycle.Execution {
	t.Helper()
	e := withMaterial(executionFor(t, r), r)
	e.Material["gateway-tls"] = secrets.NewMaterial(map[secrets.Part][]byte{secrets.CertificatePart: []byte(gatewayCertificatePEM), secrets.PrivateKeyPart: []byte(gatewayKeyPEM)})
	e.Material["grafana-admin"] = secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("grafana-bound-password")})
	e.PreviousEvidence = evidenceBytes(t, readyEvidence(r))
	return e
}

func loginBlock(t *testing.T, r Request) reconciliation.Block {
	t.Helper()
	data, err := r.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	return reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{Kind: string(api.CephCluster), Object: r.Cluster, Implementation: Implementation, Request: data}}
}

func TestCephAdminLoginsPerBlock(t *testing.T) {
	capability := New(api.CephCluster, &credentialRunner{})
	for name, test := range map[string]struct {
		request Request
		want    []lifecycle.AdminLogin
	}{
		"cluster":            {coreRequest(), []lifecycle.AdminLogin{dashboardLogin}},
		"gateway":            {gatewayLoginRequest(), []lifecycle.AdminLogin{{Name: "alertmanager", Output: MonitoringOutput, Custody: lifecycle.AdminProducedLogin}, {Name: "prometheus", Output: MonitoringOutput, Custody: lifecycle.AdminProducedLogin}}},
		"bound monitoring":   {monitoringLoginRequest(true), []lifecycle.AdminLogin{{Name: "grafana", User: "admin", Secret: "grafana-admin", Custody: lifecycle.AdminBoundSecret}}},
		"unbound monitoring": {monitoringLoginRequest(false), nil},
		"nfs":                {nfsUnitRequest(), nil},
	} {
		logins, err := capability.AdminLogins(loginBlock(t, test.request))
		if err != nil || !slices.Equal(logins, test.want) {
			t.Errorf("%s: logins %+v (%v), want %+v", name, logins, err, test.want)
		}
	}
}

func loginRunner(e lifecycle.Execution) *credentialRunner {
	return &credentialRunner{result: lifecycle.RunResult{Evidence: credentialProof(e, true), Produced: []lifecycle.Produced{
		{Name: "guard", Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(`{"guard":{"fsid":"x","login":"l"}}`)})},
	}}}
}

func boundSecrets(request lifecycle.RunRequest) []string {
	var bound []string
	for _, file := range request.Materials {
		if file.Secret != "fleet-key" && file.Secret != "admin-update" && file.Variable != "knownHosts" {
			bound = append(bound, file.Variable)
		}
	}
	return bound
}

func TestGatewayLoginBindLendsNoBoundLeaf(t *testing.T) {
	r := gatewayLoginRequest()
	e := loginExecution(t, r)
	login := lifecycle.AdminLogin{Name: "prometheus", Output: MonitoringOutput, Custody: lifecycle.AdminProducedLogin}
	runner := loginRunner(e)
	guard, err := New(api.CephCluster, runner).PrepareAdminCredential(context.Background(), e, login)
	value, _ := guard.Part(secrets.ValuePart)
	if err != nil || string(value) != `{"fsid":"x","login":"l"}` {
		t.Fatalf("guard %q (%v)", value, err)
	}
	guard.Clear()
	prepare := runner.request
	update := secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(`{"candidate":"update"}`)})
	if err := New(api.CephCluster, runner).UpdateAdminCredential(context.Background(), e, login, update); err != nil {
		t.Fatal(err)
	}
	for _, request := range []lifecycle.RunRequest{prepare, runner.request} {
		values := map[string]string{"incarnation": monmapCreatedAt, "login": "prometheus"}
		if !maps.Equal(request.MaterialValues, values) || request.Deadline != 6*time.Minute || len(boundSecrets(request)) != 0 || request.Material["gateway-tls"].Size() != 0 {
			t.Fatalf("%s lent %v with values %v and deadline %s", request.Operation, boundSecrets(request), request.MaterialValues, request.Deadline)
		}
	}
	if !slices.ContainsFunc(runner.request.Materials, func(file lifecycle.MaterialFile) bool { return file.Variable == "update" }) {
		t.Fatalf("the update was not lent: %+v", runner.request.Materials)
	}
}

func TestGrafanaLoginBindLendsOnlyTheBoundPassword(t *testing.T) {
	r := monitoringLoginRequest(true)
	e := loginExecution(t, r)
	login := lifecycle.AdminLogin{Name: "grafana", User: "admin", Secret: "grafana-admin", Custody: lifecycle.AdminBoundSecret}
	runner := loginRunner(e)
	guard, err := New(api.CephCluster, runner).PrepareAdminCredential(context.Background(), e, login)
	if err != nil {
		t.Fatal(err)
	}
	guard.Clear()
	prepare := runner.request
	update := secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(`{"candidate":"update"}`)})
	if err := New(api.CephCluster, runner).UpdateAdminCredential(context.Background(), e, login, update); err != nil {
		t.Fatal(err)
	}
	for _, request := range []lifecycle.RunRequest{prepare, runner.request} {
		values := map[string]string{"incarnation": monmapCreatedAt, "login": "grafana"}
		if !maps.Equal(request.MaterialValues, values) || request.Deadline != 5*time.Minute || !slices.Equal(boundSecrets(request), []string{"grafanaAdminPassword"}) || request.Material["gateway-tls"].Size() != 0 {
			t.Fatalf("%s lent %v with values %v and deadline %s", request.Operation, boundSecrets(request), request.MaterialValues, request.Deadline)
		}
	}
	dashboard := loginRunner(e)
	if guard, err := New(api.CephCluster, dashboard).PrepareAdminCredential(context.Background(), credentialExecution(t), dashboardLogin); err != nil || dashboard.request.MaterialValues["login"] != "dashboard" {
		t.Fatalf("the dashboard run named login %q (%v)", dashboard.request.MaterialValues["login"], err)
	} else {
		guard.Clear()
	}
}

func TestLoginRefusalsCarryNoValue(t *testing.T) {
	r := gatewayLoginRequest()
	e := loginExecution(t, r)
	login := lifecycle.AdminLogin{Name: "prometheus", Output: MonitoringOutput, Custody: lifecycle.AdminProducedLogin}
	for reason, want := range map[string]struct {
		code      string
		unapplied bool
	}{
		"monitoring-login-drift": {cephDrifted.Code, true}, "grafana-login-drift": {cephDrifted.Code, false}, "grafana-login-drift-unapplied": {cephDrifted.Code, true},
		"grafana-login-refused": {cephUnproved.Code, false}, "monitoring-login-unproved": {cephUnproved.Code, false},
	} {
		update := secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(`{"password":"candidate-secret-value"}`)})
		err := New(api.CephCluster, &refusingRunner{reason: reason}).UpdateAdminCredential(context.Background(), e, login, update)
		d := diagnostics.Of(err)
		if len(d) != 1 || d[0].Code != want.code || d[0].Remediation == "" || errors.Is(err, lifecycle.ErrAdminCandidateUnapplied) != want.unapplied {
			t.Fatalf("%s reached the operator as %+v (unapplied %t)", reason, d, errors.Is(err, lifecycle.ErrAdminCandidateUnapplied))
		}
		if d[0].Remediation != lifecycle.RepeatAdminCredentialUpdate && strings.Contains(d[0].Remediation, "update-admin-creds") {
			t.Fatalf("%s names the repeat the credential update appends with its remaining logins: %+v", reason, d)
		}
		for _, value := range []string{"candidate-secret-value", "grafana-bound-password"} {
			if strings.Contains(d[0].Message+d[0].Remediation, value) {
				t.Fatalf("%s carries a value: %+v", reason, d)
			}
		}
		if _, cluster := adapterRefusals()[reason]; cluster {
			t.Fatalf("%s is a cluster block refusal, which ceph_core.py does not publish", reason)
		}
	}
}

func TestEveryReasonTheAdminCredentialModulePublishesIsNamedByItsRunner(t *testing.T) {
	const path = "collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_core.py"
	_, tuple, found := strings.Cut(string(ansible.Assets()[path]), "\nADMIN_REASONS = (\n")
	tuple, _, closed := strings.Cut(tuple, "\n)\n")
	if !found || !closed {
		t.Fatalf("%s declares no ADMIN_REASONS tuple", path)
	}
	var published []string
	for _, match := range regexp.MustCompile(`"([a-z0-9-]+)"`).FindAllStringSubmatch(tuple, -1) {
		published = append(published, match[1])
	}
	for _, reason := range []string{"monitoring-login-drift", "grafana-login-drift", "grafana-login-drift-unapplied", "grafana-login-refused", "monitoring-login-unproved", "dashboard-drift"} {
		if !slices.Contains(published, reason) {
			t.Errorf("the admin credential module does not publish %q", reason)
		}
	}
	e := loginExecution(t, gatewayLoginRequest())
	for _, reason := range published {
		if loginRefusals()[reason] == nil {
			t.Errorf("the admin credential module publishes %q, which loginRefusals does not name", reason)
			continue
		}
		update := secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(`{"password":"candidate-secret-value"}`)})
		runner := &refusingRunner{reason: reason}
		err := New(api.CephCluster, runner).UpdateAdminCredential(context.Background(), e, dashboardLogin, update)
		if d := diagnostics.Of(err); len(d) != 1 || d[0].Remediation == "" || strings.Contains(d[0].Message+d[0].Remediation, "candidate-secret-value") {
			t.Errorf("%s reached the operator as %+v", reason, d)
		}
	}
}
