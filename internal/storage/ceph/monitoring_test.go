package ceph

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/storage"
)

const monitoringSentinel = "monitoring-sentinel-never-frozen"

func monitoringServices() []api.Value {
	return []api.Value{
		auxiliaryService("prometheus", "", requestMap("port", 9095, "retention_time", "15d", "retention_size", "10GB")),
		auxiliaryService("alertmanager", "", requestMap("user_data", requestMap("default_webhook_urls", api.ListValue(api.StringValue("https://alerts.example.com/hook"))))),
		auxiliaryService("grafana", "", requestMap("anonymous_access", false, "protocol", "https")),
		auxiliaryService("node-exporter", "", requestMap("port", 9100)),
		auxiliaryService("ceph-exporter", "", requestMap("prio_limit", 5, "stats_period", 10)),
		auxiliaryService("snmp-gateway", "", requestMap("snmp_version", "V3", "snmp_destination", "192.0.2.80:162", "engine_id", "8000c53f00000000", "auth_protocol", "SHA", "privacy_protocol", "AES")),
	}
}

func grafanaPasswordBinding() api.Value {
	return requestMap("serviceRef", "grafana", "secrets", api.ListValue(requestMap("path", "/spec/initial_admin_password", "secretRef", "grafana-admin")))
}

func monitoringBindings() []api.Value {
	return []api.Value{grafanaPasswordBinding(), requestMap("serviceRef", "snmp-gateway", "secrets", api.ListValue(
		requestMap("path", "/spec/credentials/snmp_v3_priv_password", "secretRef", "snmp-priv"),
		requestMap("path", "/spec/credentials/snmp_v3_auth_username", "secretRef", "snmp-auth"),
		requestMap("path", "/spec/credentials/snmp_v3_auth_password", "secretRef", "snmp-auth")))}
}

func monitoringCluster(t *testing.T, services, bindings []api.Value) api.Object {
	t.Helper()
	cluster, found := delegatedCatalog(services, bindings).Find(api.StorageCluster, "example")
	if !found {
		t.Fatal("the catalog has no StorageCluster example")
	}
	return cluster
}

func monitoringUnit(t *testing.T, services, bindings []api.Value) ServiceUnit {
	t.Helper()
	units, err := monitoringUnits(monitoringCluster(t, services, bindings))
	if err != nil || len(units) != 1 {
		t.Fatalf("the monitoring units are %+v (%+v)", units, diagnostics.Of(err))
	}
	return units[0]
}

func monitoringRequest(t *testing.T) Request {
	t.Helper()
	r := frozenGoldenRequest()
	unit := monitoringUnit(t, monitoringServices(), monitoringBindings())
	r.Action, r.Service = "service", &unit
	for _, document := range unitDocuments(r) {
		r.Delegated = append(r.Delegated, DelegatedService{Name: documentName(document), Hosts: document.Placement.Hosts})
	}
	for i, name := range []string{"alertmanager", "grafana", "node_exporter", "prometheus", "snmp_gateway"} {
		r.Auxiliary = append(r.Auxiliary, AuxiliaryReference{Name: name, Image: PinnedRegistry + "/synthetic/" + name + "@sha256:" + strings.Repeat(string("cdef1"[i]), 64)})
	}
	return r
}

func TestMonitoringRequestGolden(t *testing.T) {
	r := monitoringRequest(t)
	if len(r.Delegated) != 6 || len(r.Service.Bindings) != 4 {
		t.Fatalf("the monitoring request delegates %+v and binds %+v", r.Delegated, r.Service.Bindings)
	}
	data, err := r.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "request-cluster-monitoring", data, false)
	decoded, err := DecodeRequest(data)
	if err != nil {
		t.Fatal(err)
	}
	roundtrip, err := decoded.Canonical()
	if err != nil || !bytes.Equal(data, roundtrip) {
		t.Fatal("the monitoring request did not round-trip")
	}
	for _, leak := range []string{monitoringSentinel, `"initial_admin_password":`, `"credentials":`} {
		if bytes.Contains(data, []byte(leak)) {
			t.Fatalf("the monitoring request carries %q", leak)
		}
	}
	for _, document := range decoded.Service.Documents {
		if bytes.Contains(document, []byte(`"service_id"`)) {
			t.Fatalf("the singleton document %s carries a service_id", document)
		}
	}
}

func TestMonitoringProjectionDocuments(t *testing.T) {
	hosts := `"placement":{"count":1,"hosts":["node-1","node-2","node-3"]}`
	want := []string{
		`{` + hosts + `,"service_type":"alertmanager","spec":{"user_data":{"default_webhook_urls":["https://alerts.example.com/hook"]}}}`,
		`{` + hosts + `,"service_type":"ceph-exporter","spec":{"prio_limit":5,"stats_period":10}}`,
		`{` + hosts + `,"service_type":"grafana","spec":{"anonymous_access":false,"protocol":"https"}}`,
		`{` + hosts + `,"service_type":"node-exporter","spec":{"port":9100}}`,
		`{` + hosts + `,"service_type":"prometheus","spec":{"port":9095,"retention_size":"10GB","retention_time":"15d"}}`,
		`{` + hosts + `,"service_type":"snmp-gateway","spec":{"auth_protocol":"SHA","engine_id":"8000c53f00000000","privacy_protocol":"AES","snmp_destination":"192.0.2.80:162","snmp_version":"V3"}}`,
	}
	unit := monitoringUnit(t, monitoringServices(), monitoringBindings())
	if unit.Type != "monitoring" || unit.ID != "" || !slices.Equal(documentsOf(unit), want) {
		t.Fatalf("the monitoring unit is %s %q:\n%s", unit.Type, unit.ID, strings.Join(documentsOf(unit), "\n"))
	}
	wantBindings := []ServiceBinding{{Path: "/spec/initial_admin_password", SecretRef: "grafana-admin", ServiceRef: "grafana"},
		{Path: "/spec/credentials/snmp_v3_auth_password", SecretRef: "snmp-auth", ServiceRef: "snmp-gateway"},
		{Path: "/spec/credentials/snmp_v3_auth_username", SecretRef: "snmp-auth", ServiceRef: "snmp-gateway"},
		{Path: "/spec/credentials/snmp_v3_priv_password", SecretRef: "snmp-priv", ServiceRef: "snmp-gateway"}}
	if !reflect.DeepEqual(unit.Bindings, wantBindings) {
		t.Fatalf("the monitoring unit binds %+v", unit.Bindings)
	}
	plain := monitoringUnit(t, []api.Value{auxiliaryService("grafana", "", api.Value{}), auxiliaryService("alertmanager", "", requestMap("port", 9095))}, nil)
	wantPlain := []string{`{` + hosts + `,"service_type":"alertmanager","spec":{"port":9095}}`, `{` + hosts + `,"service_type":"grafana"}`}
	if !slices.Equal(documentsOf(plain), wantPlain) || plain.Bindings != nil {
		t.Fatalf("the unauthored grafana unit is %q %+v", documentsOf(plain), plain.Bindings)
	}
	if units, err := monitoringUnits(monitoringCluster(t, nil, nil)); err != nil || len(units) != 0 {
		t.Fatalf("a cluster without monitoring services plans %+v (%v)", units, err)
	}
}

func TestMonitoringProjectionRefusesUnrealizedMembers(t *testing.T) {
	admitted := requestMap("only_bind_port_on_networks", true, "port", 9095, "retention_size", "10GB", "retention_time", "15d")
	if _, err := monitoringUnits(monitoringCluster(t, []api.Value{auxiliaryService("prometheus", "", admitted)}, nil)); err != nil {
		t.Fatalf("every admitted prometheus member refuses: %+v", diagnostics.Of(err))
	}
	prometheus := auxiliaryService("prometheus", "", admitted.With("targets", api.ListValue(api.StringValue("192.0.2.90:9100"))))
	_, err := monitoringUnits(monitoringCluster(t, []api.Value{prometheus}, nil))
	refusedWith(t, err, "lifecycle.unsupported", "the native service prometheus carries spec.targets, which Bootwright does not realize for this service",
		"remove spec.targets from prometheus in spec.ceph.services")
}

func monitoringFacts() []string {
	return []string{"realize Ceph service monitoring of example", "Grafana grants anonymous viewer access",
		"monitoring endpoints are unauthenticated without a management gateway",
		"cephadm enables the prometheus manager module, whose exporter listens on port 9283 of every MGR host"}
}

func TestMonitoringUnitFollowsClusterAndImageBlocks(t *testing.T) {
	services := []api.Value{auxiliaryService("prometheus", "", api.Value{}), auxiliaryService("alertmanager", "", api.Value{}), auxiliaryService("grafana", "", api.Value{})}
	plan := cephPlan(t, settingsCatalog(t, services))
	unit := blockOf(t, plan, "ceph-service-example-monitoring")
	if unit.Kind != string(api.StorageCluster) || unit.Object != "example" || unit.Implementation != Implementation || unit.Stage != reconciliation.StageClusters ||
		!slices.Equal(unit.Exclusive, []string{"ceph/example"}) || len(unit.Requires) != 0 ||
		!slices.Equal(unit.Dependencies, []string{"ceph-cluster-example", "ceph-image-example"}) ||
		unit.Description != "realize Ceph service monitoring of example" || !slices.Equal(unit.Impacts, monitoringFacts()) {
		t.Fatalf("the monitoring block is %s %s %v %v %q", unit.Kind, unit.Object, unit.Exclusive, unit.Dependencies, unit.Impacts)
	}
	ids := cephIDs(plan)
	if slices.Index(ids, unit.ID) < slices.Index(ids, "ceph-cluster-example") {
		t.Fatalf("the monitoring block is not ordered after the cluster block: %v", ids)
	}
	core, err := DecodeRequest(blockOf(t, plan, "ceph-cluster-example").Request)
	if err != nil || len(core.Services) != 3 || len(core.Delegated) != 3 || core.Service != nil {
		t.Fatalf("the cluster block carries %+v (%v)", core, err)
	}
	removal, err := New(api.StorageCluster, nil).Removal(context.Background(), unit)
	if err != nil || removal.Description != "remove Ceph service monitoring of example" || !slices.Equal(removal.Impacts, []string{"remove Ceph service monitoring of example"}) {
		t.Fatalf("the monitoring removal is %+v (%v)", removal, err)
	}
	destroy := destroyDefinition(t, settingsCatalog(t, services), unit.ID)
	if !slices.Equal(destroy.Impacts, []string{"remove Ceph service monitoring of example"}) {
		t.Fatalf("the destroy plan lists %v", destroy.Impacts)
	}
	cluster, _ := settingsCatalog(t, services).Find(api.StorageCluster, "example")
	r := monitoringRequest(t)
	r.Cluster = "example"
	if got := unitDependencies(r, cluster); !slices.Equal(got, []string{"ceph-cluster-example", "ceph-image-example", "ceph-settings-example"}) {
		t.Fatalf("a monitoring unit with frozen auxiliary images depends on %v", got)
	}
}

func destroyDefinition(t *testing.T, catalog api.Catalog, id string) reconciliation.BlockDefinition {
	t.Helper()
	input := lifecycle.PlanInput{Verb: reconciliation.Destroy, Context: lifecycle.ContextIdentity{Name: "synthetic"}, State: compilation.NewState(catalog, catalog, nil), Controller: "controller"}
	plan, err := New(api.StorageCluster, nil).Plan(context.Background(), input)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	for _, definition := range plan.Definitions {
		if definition.ID == id {
			return definition
		}
	}
	t.Fatalf("the destroy plan has no %s", id)
	return reconciliation.BlockDefinition{}
}

func TestMonitoringBlockIdentityFitsItsCluster(t *testing.T) {
	cluster := monitoringCluster(t, []api.Value{auxiliaryService("prometheus", "", api.Value{})}, nil)
	long := api.NewObject(api.StorageCluster, strings.Repeat("c", 40), api.Value{}, cluster.Spec())
	_, err := monitoringUnits(long)
	refusedWith(t, err, "api.invariant", "StorageCluster "+long.Name()+" has a name too long to identify its monitoring service block, whose identity holds at most 63 characters",
		"rename the StorageCluster to at most 39 characters")
	fits := api.NewObject(api.StorageCluster, strings.Repeat("c", 39), api.Value{}, cluster.Spec())
	units, err := monitoringUnits(fits)
	if err != nil || len(units) != 1 || len(ServiceUnitBlockID(fits.Name(), units[0])) != 63 {
		t.Fatalf("a 39-character cluster plans %+v (%+v)", units, diagnostics.Of(err))
	}
}

func TestMonitoringUnitDeadline(t *testing.T) {
	for _, hosts := range []int{3, 6} {
		catalog := planCatalog(hosts, hosts, func(spec api.Value) api.Value {
			return spec.WithPath(api.ListValue(append(spec.Get("ceph", "services").Items(), auxiliaryService("prometheus", "", api.Value{}))...), "ceph", "services")
		})
		cluster, _ := catalog.Find(api.StorageCluster, "example")
		base, err := New(api.StorageCluster, nil).request(catalog, cluster, "synthetic")
		if err != nil {
			t.Fatal(diagnostics.Of(err))
		}
		units, err := serviceUnitRequests(base, cluster)
		if err != nil || len(units) != 1 || units[0].Service.Type != "monitoring" || len(units[0].Hosts) != hosts {
			t.Fatalf("%d hosts plan units %+v (%v)", hosts, units, err)
		}
		for operation, want := range map[string]time.Duration{"apply": 2280 * time.Second, "observe": 720 * time.Second, "destroy": 1380 * time.Second} {
			if got, err := cephDeadline(units[0], operation); err != nil || got != want {
				t.Fatalf("with %d hosts the monitoring %s deadline is %v (%v), want %v", hosts, operation, got, err, want)
			}
		}
	}
}

func TestMonitoringLeavesFollowTheBindingRegistry(t *testing.T) {
	cluster := monitoringCluster(t, monitoringServices(), nil)
	for key, leaf := range monitoringLeaves {
		service, path, _ := strings.Cut(key, " ")
		slot, found := storage.NativeBindingSlot(cluster, service, path)
		if !found || slot.Part != leaf.Part || leaf.Variable == "" || leaf.File == "" {
			t.Errorf("the leaf %s is %+v, the binding registry %+v (%v)", key, leaf, slot, found)
		}
	}
}

func TestMonitoringCredentialBindingsPlan(t *testing.T) {
	grafana := auxiliaryService("grafana", "", requestMap("anonymous_access", false))
	snmp := auxiliaryService("snmp-gateway", "", requestMap("snmp_version", "V3", "snmp_destination", "192.0.2.80:162", "engine_id", "8000c53f00000000", "privacy_protocol", "AES"))
	catalog := delegatedCatalog([]api.Value{grafana, snmp}, monitoringBindings())
	r, err := planned(t, catalog, api.StorageCluster, "example")
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	data, err := r.Canonical()
	if err != nil || bytes.Contains(data, []byte("bindings")) || bytes.Contains(data, []byte("grafana-admin")) || r.Service != nil {
		t.Fatalf("the core request carries a binding: %v", err)
	}
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: "synthetic"}, State: compilation.NewState(catalog, catalog, nil), Controller: "controller"}
	plan, err := New(api.StorageCluster, nil).Plan(context.Background(), input)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	for _, secret := range []string{"grafana-admin", "snmp-auth", "snmp-priv"} {
		if !slices.Contains(plan.Secrets, secret) {
			t.Fatalf("the plan holds secrets %v without %s", plan.Secrets, secret)
		}
	}
	at := slices.IndexFunc(plan.Definitions, func(d reconciliation.BlockDefinition) bool { return d.ID == "ceph-service-example-monitoring" })
	if at < 0 {
		t.Fatal("the plan has no monitoring block")
	}
	if got := plan.Definitions[at].Impacts; !slices.Equal(got, []string{monitoringFacts()[0], monitoringFacts()[2]}) {
		t.Fatalf("the monitoring block of a password-protected Grafana lists %q", got)
	}
}

func monitoringExecution(t *testing.T, r Request) lifecycle.Execution {
	t.Helper()
	e := withMaterial(executionFor(t, r), r)
	e.Material["grafana-admin"] = secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(monitoringSentinel)})
	return e
}

func TestNoBoundMaterialInPlanRequestEvidence(t *testing.T) {
	grafana := auxiliaryService("grafana", "", requestMap("anonymous_access", false))
	catalog := delegatedCatalog([]api.Value{grafana}, []api.Value{grafanaPasswordBinding()})
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: "synthetic"}, State: compilation.NewState(catalog, catalog, nil), Controller: "controller"}
	plan, err := New(api.StorageCluster, nil).Plan(context.Background(), input)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	definitions, err := json.Marshal(plan.Definitions)
	if err != nil || bytes.Contains(definitions, []byte(monitoringSentinel)) || !slices.Contains(plan.Secrets, "grafana-admin") {
		t.Fatalf("the plan holds %v and leaks the bound value (%v)", plan.Secrets, err)
	}
	r := runtimeRequest()
	unit := monitoringUnit(t, []api.Value{grafana}, []api.Value{grafanaPasswordBinding()})
	r.Action, r.Service = "service", &unit
	evidence := readyEvidence(r)
	evidence.Daemons = []UnitDaemon{{Host: "node-1", Name: "grafana.node-1", Service: "grafana"}}
	want := lifecycle.MaterialFile{Name: "grafana-admin-password", Secret: "grafana-admin", Part: secrets.ValuePart, Variable: "grafanaAdminPassword"}
	for _, operation := range []string{"apply", "observe", "destroy"} {
		runner := &recordingRunner{}
		if _, err := New(api.StorageCluster, runner).WithProofs(dependencyProofs{}).run(context.Background(), monitoringExecution(t, r), operation, r); err != nil {
			t.Fatalf("%s: %+v", operation, diagnostics.Of(err))
		}
		call := runner.request
		visible, err := json.Marshal([]any{call.Canonical, call.Materials, call.MaterialValues, call.Description, evidenceBytes(t, evidence)})
		if err != nil || bytes.Contains(visible, []byte(monitoringSentinel)) || bytes.Contains(call.Canonical, []byte(monitoringSentinel)) {
			t.Fatalf("%s leaks the bound value (%v)", operation, err)
		}
		bound := slices.ContainsFunc(call.Materials, func(m lifecycle.MaterialFile) bool { return m.Secret == "grafana-admin" })
		if operation == "apply" && !slices.Contains(call.Materials, want) || operation != "apply" && bound {
			t.Fatalf("%s binds %+v", operation, call.Materials)
		}
	}
	missing := monitoringExecution(t, r)
	delete(missing.Material, "grafana-admin")
	_, err = New(api.StorageCluster, &recordingRunner{}).WithProofs(dependencyProofs{}).run(context.Background(), missing, "apply", r)
	refusedWith(t, err, "lifecycle.state", "a required Ceph credential component is not bound", "")
	foreign := r
	foreign.Service = &ServiceUnit{Bindings: []ServiceBinding{{Path: "/spec/ssl_cert", SecretRef: "grafana-admin", ServiceRef: "grafana"}}, Documents: unit.Documents, Type: "monitoring"}
	_, err = New(api.StorageCluster, &recordingRunner{}).WithProofs(dependencyProofs{}).run(context.Background(), monitoringExecution(t, foreign), "apply", foreign)
	refusedWith(t, err, "lifecycle.state", "a Ceph service unit binds a credential leaf this build does not realize", "")
}

func TestUnrealizedMonitoringServicesKeepTheCoreRefusal(t *testing.T) {
	for _, serviceType := range []string{"ceph-exporter", "mgmt-gateway", "oauth2-proxy"} {
		t.Run(serviceType, func(t *testing.T) {
			_, err := planned(t, delegatedCatalog([]api.Value{auxiliaryService(serviceType, "", api.Value{})}, nil), api.StorageCluster, "example")
			refusedWith(t, err, "lifecycle.unsupported", "a selected service has no core lifecycle", "use only MON, MGR, OSD and MDS services until its feature item is delivered")
		})
	}
	r, err := planned(t, delegatedCatalog([]api.Value{auxiliaryService("prometheus", "", api.Value{})}, nil), api.StorageCluster, "example")
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if len(r.Delegated) != 1 || r.Delegated[0].Name != "prometheus" {
		t.Fatalf("delegated %+v, want only prometheus", r.Delegated)
	}
}
