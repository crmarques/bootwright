package ceph

import (
	"context"
	"encoding/json"
	"maps"
	"reflect"
	"slices"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func haproxyProtocolNFS() []api.Value {
	return []api.Value{
		requestMap("service_type", "nfs", "service_id", "files", "placement", requestMap("label", "mon", "count", 2),
			"spec", requestMap("port", 12049, "enable_haproxy_protocol", true)),
		requestMap("service_type", "ingress", "service_id", "nfs.files", "placement", requestMap("label", "mon", "count", 2),
			"spec", requestMap("backend_service", "nfs.files", "virtual_ip", "192.0.2.50/24", "frontend_port", 2049, "monitor_port", 9049, "enable_haproxy_protocol", true)),
	}
}

func unitsOf(t *testing.T, services ...api.Value) []ServiceUnit {
	t.Helper()
	cluster, _ := delegatedCatalog(services, nil).Find(api.StorageCluster, "example")
	units, err := nfsUnits(cluster)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	return units
}

func documentsOf(u ServiceUnit) []string {
	out := []string{}
	for _, document := range u.Documents {
		out = append(out, string(document))
	}
	return out
}

func unitRequest(r Request) Request {
	r.Action = "service"
	r.Service = &ServiceUnit{Type: "nfs", ID: "files", Documents: []json.RawMessage{
		json.RawMessage(`{"placement":{"count":2,"hosts":["node-1","node-2"]},"service_id":"nfs.files","service_type":"ingress","spec":{"backend_service":"nfs.files","virtual_ip":"192.0.2.50/24"}}`),
		json.RawMessage(`{"placement":{"hosts":["node-2","node-3"]},"service_id":"files","service_type":"nfs"}`)}}
	return r
}

func TestServiceUnitBlocksFollowClusterBlock(t *testing.T) {
	plan := cephPlan(t, settingsCatalog(t, []api.Value{nfsService("files")}))
	unit := blockOf(t, plan, "ceph-service-example-nfs-files")
	if unit.Kind != string(api.StorageCluster) || unit.Object != "example" || unit.Implementation != Implementation || unit.Stage != reconciliation.StageClusters ||
		!slices.Equal(unit.Exclusive, []string{"ceph/example"}) || len(unit.Requires) != 0 ||
		!slices.Equal(unit.Dependencies, []string{"ceph-cluster-example", "ceph-image-example", "ceph-settings-example"}) ||
		!slices.Equal(unit.Impacts, []string{"realize Ceph service nfs.files of example"}) {
		t.Fatalf("the unit block is %+v", unit.BlockDefinition)
	}
	ids := cephIDs(plan)
	if at := slices.Index(ids, unit.ID); at < slices.Index(ids, "ceph-cluster-example") || at < slices.Index(ids, "ceph-settings-example") {
		t.Fatalf("the unit block is not ordered after the cluster and settings blocks: %v", ids)
	}
	if pool := blockOf(t, plan, "ceph-pool-volumes"); !slices.Equal(pool.Dependencies, []string{"ceph-cluster-example", "ceph-image-example"}) {
		t.Fatalf("the pool depends on %v", pool.Dependencies)
	}
	r, err := DecodeRequest(unit.Request)
	if err != nil || r.Action != "service" || r.Settings != nil || r.Service == nil || r.Service.name() != "nfs.files" || len(r.Service.Documents) != 1 {
		t.Fatalf("the unit request is %+v (%v)", r, err)
	}
	removal, err := New(api.StorageCluster, nil).Removal(context.Background(), unit)
	if err != nil || removal.Description != "remove Ceph service nfs.files of example" {
		t.Fatalf("the unit removal is %+v (%v)", removal, err)
	}
}

func TestServiceUnitDeadlineIndependentOfHostCount(t *testing.T) {
	for _, hosts := range []int{3, 6} {
		catalog := planCatalog(hosts, hosts, func(spec api.Value) api.Value {
			return spec.WithPath(api.ListValue(append(spec.Get("ceph", "services").Items(), nfsService("files"))...), "ceph", "services")
		})
		cluster, _ := catalog.Find(api.StorageCluster, "example")
		base, err := New(api.StorageCluster, nil).request(catalog, cluster, "synthetic")
		if err != nil {
			t.Fatal(diagnostics.Of(err))
		}
		units, err := serviceUnitRequests(base, cluster)
		if err != nil || len(units) != 1 || len(units[0].Hosts) != hosts {
			t.Fatalf("%d hosts plan units %+v (%v)", hosts, units, err)
		}
		for operation, want := range map[string]time.Duration{"apply": 2280 * time.Second, "observe": 720 * time.Second, "destroy": 1380 * time.Second} {
			if got, err := cephDeadline(units[0], operation); err != nil || got != want {
				t.Fatalf("with %d hosts the unit %s deadline is %v (%v), want %v", hosts, operation, got, err, want)
			}
		}
	}
	unknown := unitRequest(coreRequest())
	unknown.Service.Type = "unknown"
	_, err := cephDeadline(unknown, "apply")
	if d := diagnostics.Of(err); len(d) != 1 || d[0].Code != "lifecycle.state" || d[0].Message != "the Ceph service unit type is not known to this build" {
		t.Fatalf("an unknown unit type gives %+v", d)
	}
}

func TestUnitTypeRegistryCarriesHooksAndExtraDependencies(t *testing.T) {
	if keys := slices.Sorted(maps.Keys(unitTypes)); !slices.Equal(keys, []string{"nfs"}) {
		t.Fatalf("the registry holds %v", keys)
	}
	nfs := unitTypes["nfs"]
	if nfs.units == nil || nfs.dependencies == nil || nfs.apply != (unitBudget{2, 1}) || nfs.observe != (unitBudget{1, 0}) || nfs.destroy != (unitBudget{2, 0}) {
		t.Fatalf("the nfs unit type is %+v", nfs)
	}
	t.Cleanup(func() { delete(unitTypes, "synthetic") })
	unitTypes["synthetic"] = unitType{
		units: func(api.Object) ([]ServiceUnit, error) {
			return []ServiceUnit{{Type: "synthetic", Documents: []json.RawMessage{json.RawMessage(`{"placement":{"hosts":["node-1"]},"service_id":"","service_type":"synthetic"}`)}}}, nil
		},
		dependencies: func(api.Object, ServiceUnit) []string { return []string{"ceph-pool-gateways"} },
		apply:        unitBudget{calls: 1}, observe: unitBudget{calls: 1}, destroy: unitBudget{calls: 1},
	}
	for _, definition := range cephDefinitions(t, settingsCatalog(t, nil)) {
		if definition.ID != "ceph-service-example-synthetic" {
			continue
		}
		if !slices.Equal(definition.Dependencies, []string{"ceph-cluster-example", "ceph-image-example", "ceph-pool-gateways"}) {
			t.Fatalf("the synthetic unit depends on %v", definition.Dependencies)
		}
		return
	}
	t.Fatal("the synthetic unit type planned no ceph-service-example-synthetic block")
}

func TestServiceUnitProjection(t *testing.T) {
	units := unitsOf(t, haproxyProtocolNFS()...)
	want := []string{
		`{"placement":{"count":2,"hosts":["node-1","node-2","node-3"]},"service_id":"nfs.files","service_type":"ingress","spec":{"backend_service":"nfs.files","enable_haproxy_protocol":true,"frontend_port":2049,"monitor_port":9049,"virtual_ip":"192.0.2.50/24"}}`,
		`{"placement":{"count":2,"hosts":["node-1","node-2","node-3"]},"service_id":"files","service_type":"nfs","spec":{"enable_haproxy_protocol":true,"port":12049}}`,
	}
	if len(units) != 1 || units[0].Type != "nfs" || units[0].ID != "files" || !slices.Equal(documentsOf(units[0]), want) {
		t.Fatalf("the haproxy-protocol unit is %+v %q", units, documentsOf(units[0]))
	}
	direct := unitsOf(t, nfsService("direct"))
	if len(direct) != 1 || !slices.Equal(documentsOf(direct[0]), []string{`{"placement":{"count":2,"hosts":["node-1","node-2","node-3"]},"service_id":"direct","service_type":"nfs"}`}) {
		t.Fatalf("the direct unit is %q", documentsOf(direct[0]))
	}
	two := unitsOf(t, nfsService("zeta"), nfsService("alpha"))
	if len(two) != 2 || two[0].ID != "alpha" || two[1].ID != "zeta" || !reflect.DeepEqual(ServiceUnitBlockID("example", two[0]), "ceph-service-example-nfs-alpha") {
		t.Fatalf("two nfs services make %+v", two)
	}
}

func planError(t *testing.T, catalog api.Catalog) error {
	t.Helper()
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: "synthetic"}, State: compilation.NewState(catalog, catalog, nil), Controller: "controller"}
	_, err := New(api.StorageCluster, nil).Plan(context.Background(), input)
	return err
}

func TestServiceUnitProjectionRefusesUnrealizedOptions(t *testing.T) {
	nfs, ingress := haproxyProtocolNFS()[0], haproxyProtocolNFS()[1]
	refusedWith(t, planError(t, delegatedCatalog([]api.Value{nfs.With("placement", requestMap("label", "mon", "count_per_host", 1)), ingress}, nil)), "lifecycle.unsupported",
		"the native service nfs.files places more than one daemon of a type per host, which Bootwright does not realize", "omit placement.count_per_host on nfs.files in spec.ceph.services")
	refusedWith(t, planError(t, delegatedCatalog([]api.Value{nfs.With("config", requestMap("log_level", "debug")), ingress}, nil)), "lifecycle.unsupported",
		"the native service nfs.files carries config, extra container or entrypoint arguments or custom configs, which Bootwright does not realize", "remove those options from nfs.files in spec.ceph.services")
	refusedWith(t, planError(t, delegatedCatalog([]api.Value{nfs, ingress.WithPath(api.BoolValue(true), "spec", "ssl")}, nil)), "lifecycle.unsupported",
		"the native service ingress.nfs.files carries spec.ssl, which Bootwright does not realize for this service", "remove spec.ssl from ingress.nfs.files in spec.ceph.services")
	if err := planError(t, delegatedCatalog(haproxyProtocolNFS(), nil)); err != nil {
		t.Fatalf("the authored unit refuses: %+v", diagnostics.Of(err))
	}
}

func TestServiceUnitIdentity(t *testing.T) {
	r := unitRequest(coreRequest())
	if _, err := New(api.StorageCluster, nil).identity(context.Background(), executionFor(t, r)); err != nil {
		t.Fatalf("a unit block fails its identity: %+v", diagnostics.Of(err))
	}
	unset := r
	unset.Service = nil
	carried := coreRequest()
	carried.Service = r.Service
	for name, request := range map[string]Request{"service without its member": unset, "cluster with a service member": carried} {
		_, err := New(api.StorageCluster, nil).identity(context.Background(), executionFor(t, request))
		if d := diagnostics.Of(err); len(d) != 1 || d[0].Code != "lifecycle.state" || d[0].Message != "Ceph request does not match its frozen resource identity" {
			t.Fatalf("a %s passes its identity: %+v", name, d)
		}
	}
	_, err := New(api.StoragePool, nil).identity(context.Background(), executionFor(t, r))
	if d := diagnostics.Of(err); len(d) != 1 || d[0].Code != "lifecycle.state" {
		t.Fatalf("a pool capability executes a unit request: %+v", d)
	}
}

func unitEvidence(r Request) Evidence {
	e := readyEvidence(r)
	e.Daemons = []UnitDaemon{
		{Host: "node-1", Name: "haproxy.nfs.files.node-1.abcdef", Service: "ingress.nfs.files"},
		{Host: "node-3", Name: "nfs.files.0.0.node-3.ghijkl", Service: "nfs.files"},
	}
	return e
}

func TestServiceUnitEvidence(t *testing.T) {
	r := unitRequest(runtimeRequest())
	if effect, err := EvaluateEvidence(evidenceBytes(t, unitEvidence(r)), r, "digest", false); err != nil || effect != reconciliation.EffectCompleted {
		t.Fatalf("ready unit evidence is %s (%v)", effect, err)
	}
	cases := map[string]func(*Evidence){
		"daemon off its document's hosts": func(e *Evidence) { e.Daemons[1].Host = "node-1" },
		"daemon of a foreign service":     func(e *Evidence) { e.Daemons[1].Service = "nfs.other" },
		"document without a daemon":       func(e *Evidence) { e.Daemons = e.Daemons[1:] },
		"duplicate daemon name":           func(e *Evidence) { e.Daemons[1].Name, e.Daemons[1].Host = e.Daemons[0].Name, "node-2" },
		"malformed daemon name":           func(e *Evidence) { e.Daemons[1].Name = "nfs" },
		"resource":                        func(e *Evidence) { e.Resource = ResourceEvidence{ID: "7"} },
	}
	for name, mutate := range cases {
		e := unitEvidence(r)
		mutate(&e)
		if effect, err := EvaluateEvidence(evidenceBytes(t, e), r, "digest", false); err == nil || effect != reconciliation.EffectUnknown {
			t.Errorf("unit evidence with a %s is %s (%v)", name, effect, err)
		}
	}
	pool := runtimeRequest()
	pool.Action, pool.Pool = "pool", &Pool{Name: "volumes"}
	clean := readyEvidence(pool)
	clean.Resource = ResourceEvidence{ID: "7"}
	if effect, err := EvaluateEvidence(evidenceBytes(t, clean), pool, "digest", false); err != nil || effect != reconciliation.EffectCompleted {
		t.Fatalf("ready pool evidence is %s (%v)", effect, err)
	}
	clean.Daemons = unitEvidence(r).Daemons[1:]
	if effect, err := EvaluateEvidence(evidenceBytes(t, clean), pool, "digest", false); err == nil || effect != reconciliation.EffectUnknown {
		t.Fatalf("pool evidence carrying a daemon is %s (%v)", effect, err)
	}
	absent := Evidence{Version: EvidenceVersion, Request: "digest", Action: r.Action, Owner: r.Owner, FSID: r.FSID, Owned: true, Hosts: hostNames(r),
		Monitors: []string{}, MGRStandbys: []string{}, MonAddrs: []MonitorAddress{}, OSDs: []OSDEvidence{}, Daemons: []UnitDaemon{}}
	if effect, err := EvaluateEvidence(evidenceBytes(t, absent), r, "digest", false); err != nil || effect != reconciliation.EffectNoEffect {
		t.Fatalf("absent unit evidence is %s (%v)", effect, err)
	}
	absent.Daemons = unitEvidence(r).Daemons[1:]
	if effect, err := EvaluateEvidence(evidenceBytes(t, absent), r, "digest", false); err == nil || effect != reconciliation.EffectUnknown {
		t.Fatalf("absent unit evidence carrying a daemon is %s (%v)", effect, err)
	}
}
