package ceph

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

const (
	monitoringPassword = "pr-Secret.17"
	monitoringLogins   = `{"alertmanager":{"password":"am-Secret.42","user":"am-admin"},"prometheus":{"password":"pr-Secret.17","user":"pr-admin"}}`
)

func monitoredGateway(t *testing.T, services ...string) Request {
	t.Helper()
	declared := vipGateway("192.0.2.60")
	for _, service := range services {
		declared = append(declared, auxiliaryService(service, "", api.Value{}))
	}
	unit := gatewayUnitOf(t, withManagementFQDN(delegatedCatalog(declared, nil), managementFQDN))
	r := runtimeRequest()
	r.Action, r.Service = "service", &unit
	return r
}

func monitoringProduced(values ...string) []lifecycle.Produced {
	produced := []lifecycle.Produced{}
	for _, value := range values {
		produced = append(produced, lifecycle.Produced{Name: MonitoringOutput, Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(value)})})
	}
	return produced
}

func gatewayRunOutputs(t *testing.T, r Request, operation string, recorded bool) []lifecycle.OutputFile {
	t.Helper()
	e := withMaterial(executionFor(t, r), r)
	e.Block.ID = GatewayBlock(r.Cluster)
	if recorded {
		e.Attempt, e.Resolution = 1, 1
	}
	runner := &recordingRunner{}
	if _, err := New(api.CephCluster, runner).WithProofs(dependencyProofs{}).run(context.Background(), e, operation, r); err != nil {
		t.Fatalf("%s: %+v", operation, diagnostics.Of(err))
	}
	if strings.Contains(string(runner.request.Canonical), monitoringPassword) {
		t.Fatalf("the %s request carries a login", operation)
	}
	return runner.request.Outputs
}

func TestGatewayUnitDeclaresTheMonitoringOutputOnApplyAndRecordedObserve(t *testing.T) {
	r := monitoredGateway(t, "alertmanager", "grafana", "prometheus")
	want := []lifecycle.OutputFile{{Name: MonitoringOutput, Variable: MonitoringOutput}}
	for _, tt := range []struct {
		operation string
		recorded  bool
		declared  bool
	}{{"apply", false, true}, {"apply", true, true}, {"observe", true, true}, {"observe", false, false}, {"destroy", true, false}} {
		outputs := gatewayRunOutputs(t, r, tt.operation, tt.recorded)
		if tt.declared && !reflect.DeepEqual(outputs, want) || !tt.declared && len(outputs) != 0 {
			t.Fatalf("%s recorded %t declares %+v", tt.operation, tt.recorded, outputs)
		}
	}
	if GatewayBlock("example") != "ceph-service-example-mgmt-gateway" {
		t.Fatalf("the gateway block is %s", GatewayBlock("example"))
	}
	for operation, want := range map[string]int{"apply": 4, "observe": 2} {
		if got := unitTypes["mgmt-gateway"]; operation == "apply" && got.apply.calls != want || operation == "observe" && got.observe.calls != want {
			t.Fatalf("the gateway %s budget is %+v", operation, got)
		}
	}
}

func TestGatewayUnitWithoutPrometheusOrAlertmanagerProducesNothing(t *testing.T) {
	for _, r := range []Request{monitoredGateway(t, "grafana"), monitoredGateway(t)} {
		for _, operation := range []string{"apply", "observe"} {
			if outputs := gatewayRunOutputs(t, r, operation, true); len(outputs) != 0 {
				t.Fatalf("a gateway monitoring %v declares %+v on %s", r.Service.Monitoring, outputs, operation)
			}
		}
		produced := monitoringProduced(`{"prometheus":{"password":"p","user":"u"}}`)
		if err := validateProduced(produced, r, true); err == nil {
			t.Fatalf("a gateway monitoring %v accepted a login", r.Service.Monitoring)
		}
		lifecycle.ClearProduced(produced)
	}
	nfs := runtimeRequest()
	nfs.Action, nfs.Service = "service", &ServiceUnit{Type: "nfs", ID: "files", Monitoring: []string{"prometheus"}}
	if credentialServices(nfs) != nil {
		t.Fatalf("an nfs unit reads %v", credentialServices(nfs))
	}
}

func TestValidateMonitoringConfinesTheProducedEntry(t *testing.T) {
	r := monitoredGateway(t, "alertmanager", "grafana", "prometheus")
	good := monitoringProduced(monitoringLogins)
	if err := validateProduced(good, r, true); err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	lifecycle.ClearProduced(good)
	only := monitoredGateway(t, "prometheus")
	alone := monitoringProduced(`{"prometheus":{"password":"pr-Secret.17","user":"pr-admin"}}`)
	if err := validateProduced(alone, only, true); err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	lifecycle.ClearProduced(alone)
	refused := map[string][]lifecycle.Produced{
		"none":                monitoringProduced(),
		"two":                 monitoringProduced(monitoringLogins, monitoringLogins),
		"a missing service":   monitoringProduced(`{"prometheus":{"password":"pr-Secret.17","user":"pr-admin"}}`),
		"an extra service":    monitoringProduced(strings.Replace(monitoringLogins, `{"alertmanager"`, `{"a":{"password":"p","user":"u"},"alertmanager"`, 1)),
		"grafana":             monitoringProduced(strings.Replace(monitoringLogins, `"alertmanager"`, `"grafana"`, 1)),
		"an extra member":     monitoringProduced(strings.Replace(monitoringLogins, `{"password":"am`, `{"certificate":"c","password":"am`, 1)),
		"no user":             monitoringProduced(`{"alertmanager":{"password":"am-Secret.42"},"prometheus":{"password":"pr-Secret.17","user":"pr-admin"}}`),
		"an empty password":   monitoringProduced(strings.Replace(monitoringLogins, `"am-Secret.42"`, `""`, 1)),
		"a space":             monitoringProduced(strings.Replace(monitoringLogins, `"am-Secret.42"`, `"am Secret"`, 1)),
		"an ampersand":        monitoringProduced(strings.Replace(monitoringLogins, `"am-admin"`, `"am&admin"`, 1)),
		"a long password":     monitoringProduced(strings.Replace(monitoringLogins, `"am-Secret.42"`, `"`+strings.Repeat("p", 129)+`"`, 1)),
		"not canonical":       monitoringProduced(`{"prometheus":{"password":"pr-Secret.17","user":"pr-admin"},"alertmanager":{"password":"am-Secret.42","user":"am-admin"}}`),
		"a duplicated member": monitoringProduced(`{"alertmanager":{"password":"x","password":"am-Secret.42","user":"am-admin"},"prometheus":{"password":"pr-Secret.17","user":"pr-admin"}}`),
		"another output":      {{Name: DashboardOutput, Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(monitoringLogins)})}},
		"two parts":           {{Name: MonitoringOutput, Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(monitoringLogins), secrets.PasswordPart: []byte("p")})}},
	}
	for name, produced := range refused {
		if err := validateProduced(produced, r, true); err == nil {
			t.Errorf("produced monitoring logins with %s were accepted", name)
		}
		lifecycle.ClearProduced(produced)
	}
	removal := monitoringProduced(monitoringLogins)
	if err := validateProduced(removal, r, false); err == nil {
		t.Fatal("an unrecorded observation accepted produced monitoring logins")
	}
	lifecycle.ClearProduced(removal)
	login, ok := MonitoringCredentials([]byte(monitoringLogins), "prometheus")
	user, _ := login.Part(secrets.UsernamePart)
	password, _ := login.Part(secrets.PasswordPart)
	if !ok || string(user) != "pr-admin" || string(password) != monitoringPassword || !slices.Equal(login.Parts(), []secrets.Part{secrets.PasswordPart, secrets.UsernamePart}) {
		t.Fatalf("the prometheus login reads %q %q (%t)", user, password, ok)
	}
	login.Clear()
	for _, value := range []string{`{"alertmanager":{"password":"am-Secret.42","user":"am-admin"}}`, monitoringLogins + " ", `{"prometheus":{"password":"p q","user":"u"}}`} {
		if _, ok := MonitoringCredentials([]byte(value), "prometheus"); ok {
			t.Errorf("the prometheus login was read from %s", value)
		}
	}
}

func TestMonitoringCredentialsNeverReachDiagnostics(t *testing.T) {
	r := monitoredGateway(t, "alertmanager", "grafana", "prometheus")
	frozen := r
	frozen.Owner, frozen.FSID = "", ""
	e := withMaterial(executionFor(t, frozen), r)
	e.Block.ID = GatewayBlock(r.Cluster)
	e.Attempt, e.Resolution = 1, 1
	runtime := frozen
	runtime.Owner, runtime.FSID = e.ApplyOperation, FSIDFor(e.ApplyOperation, r.Cluster)
	evidence := readyEvidence(runtime)
	evidence.Request = e.Block.RequestDigest
	evidence.Daemons = []UnitDaemon{{Host: "node-1", Name: "keepalived.mgmt.node-1.abcdef", Service: "ingress.mgmt"}, {Host: "node-1", Name: "mgmt-gateway.node-1", Service: "mgmt-gateway"}}
	evidence.Endpoint = EndpointEvidence{CertificateSHA256: strings.Repeat("a", 64), Reachable: true}
	runner := &recordingRunner{result: lifecycle.RunResult{Evidence: evidenceBytes(t, evidence), Produced: monitoringProduced(monitoringLogins)}}
	observation, err := New(api.CephCluster, runner).WithProofs(dependencyProofs{}).Observe(context.Background(), e)
	if err != nil || observation.Effect != reconciliation.EffectCompleted || len(observation.Produced) != 1 || observation.Produced[0].Name != MonitoringOutput {
		t.Fatalf("the gateway observation returned %+v (%v)", observation, diagnostics.Of(err))
	}
	if want := "cluster fsid " + runtime.FSID + " created " + monmapCreatedAt + " as monitoring basic auth"; observation.Produced[0].Identity != want {
		t.Fatalf("the gateway observation presents %q, want %q", observation.Produced[0].Identity, want)
	}
	lifecycle.ClearProduced(observation.Produced)
	runner.result.Produced = monitoringProduced(strings.Replace(monitoringLogins, `"pr-admin"`, `"pr admin"`, 1))
	observation, err = New(api.CephCluster, runner).WithProofs(dependencyProofs{}).Observe(context.Background(), e)
	if err == nil || observation.Effect != reconciliation.EffectUnknown || len(observation.Produced) != 0 {
		t.Fatalf("a malformed login was accepted: %+v", observation)
	}
	shown, _ := json.Marshal(diagnostics.Of(err))
	for _, value := range []string{monitoringPassword, "am-Secret.42", "pr admin", "am-admin"} {
		if strings.Contains(string(shown)+err.Error(), value) {
			t.Fatalf("the refusal %s carries %q", shown, value)
		}
	}
}

func TestGatewayRemovalObservationDeclaresNoMonitoringOutput(t *testing.T) {
	r := monitoredGateway(t, "alertmanager", "prometheus")
	r.Owner, r.FSID, r.Auxiliary = "", "", nil
	e := withMaterial(executionFor(t, r), r)
	e.Block.ID = GatewayBlock(r.Cluster)
	e.Attempt, e.Resolution = 1, 1
	for name, observe := range map[string]func(Capability) error{
		"removal":     func(c Capability) error { _, err := c.ObserveRemoval(context.Background(), e); return err },
		"observation": func(c Capability) error { _, err := c.Observe(context.Background(), e); return err },
	} {
		runner := &recordingRunner{}
		err := observe(New(api.CephCluster, runner).WithProofs(dependencyProofs{}))
		if runner.calls != 1 {
			t.Fatalf("the %s ran %d times: %+v", name, runner.calls, diagnostics.Of(err))
		}
		if declared := len(runner.request.Outputs) != 0; declared != (name == "observation") {
			t.Fatalf("the recorded %s declares %+v", name, runner.request.Outputs)
		}
	}
}

func TestAnUnretainedMonitoringLoginNamesTheReset(t *testing.T) {
	d := diagnostics.Of(adapterRefusals()["monitoring-login-unsupported"])
	const message = "a Prometheus or Alertmanager login that cephadm holds behind the management gateway is not one Bootwright retains: each user and password is 1 to 128 visible ASCII characters other than &, < and >"
	const remedy = "the native error names the service and whether its user or password differs, never the value; reset that service's login with ceph orch prometheus set-credentials <user> <password> or ceph orch alertmanager set-credentials <user> <password> to visible ASCII characters other than &, < and >, then apply again"
	if len(d) != 1 || d[0].Code != cephUnqualified.Code || d[0].Message != message || d[0].Remediation != remedy {
		t.Fatalf("an unretained monitoring login reached the operator as %+v", d)
	}
}

func TestAnObservationMayRecaptureNoMonitoringLogin(t *testing.T) {
	r := monitoredGateway(t, "alertmanager", "prometheus")
	if err := validateObserved(nil, r, true); err != nil {
		t.Fatalf("a recorded observation without logins refused: %+v", diagnostics.Of(err))
	}
	partial := monitoringProduced(`{"prometheus":{"password":"p","user":"u"}}`)
	if err := validateObserved(partial, r, true); err == nil {
		t.Fatal("a recorded observation accepted a partial login set")
	}
	lifecycle.ClearProduced(partial)
	whole := monitoringProduced(monitoringLogins)
	if err := validateObserved(whole, r, true); err != nil {
		t.Fatalf("a recorded observation refused its logins: %+v", diagnostics.Of(err))
	}
	lifecycle.ClearProduced(whole)
	cluster := runtimeRequest()
	cluster.Action = "cluster"
	if err := validateObserved(nil, cluster, true); err == nil {
		t.Fatal("a recorded cluster observation accepted no dashboard password")
	}
}
