package ceph

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func httpRGWService(id string) api.Value {
	return requestMap("service_type", "rgw", "service_id", id, "placement", requestMap("label", "mon", "count", 2))
}

func withHTTPRGW(ids ...string) func(api.Value) api.Value {
	return func(spec api.Value) api.Value {
		spec = withCorePair(spec)
		services := spec.Get("ceph", "services").Items()
		for _, id := range ids {
			services = append(services, httpRGWService(id))
		}
		return spec.WithPath(api.ListValue(services...), "ceph", "services")
	}
}

func withPairAndHTTPRGW(spec api.Value) api.Value { return withHTTPRGW("objects")(spec) }

func gatewayBlock(t *testing.T, catalog api.Catalog) reconciliation.BlockDefinition {
	t.Helper()
	for _, d := range gatewayBlocks(t, catalog) {
		if d.ID == "ceph-gateway-objects" {
			return d
		}
	}
	t.Fatal("the gateway block was not planned")
	return reconciliation.BlockDefinition{}
}

func gatewayOf(t *testing.T, d reconciliation.BlockDefinition) Request {
	t.Helper()
	r, err := DecodeRequest(d.Request)
	if err != nil || r.Action != "gateway" || r.Gateway == nil || r.Pool != nil {
		t.Fatalf("the gateway request %+v (%v)", r, err)
	}
	return r
}

func TestGatewayRequestGolden(t *testing.T) {
	d := gatewayBlock(t, planCatalog(3, 3, withPairAndHTTPRGW, objectGateway("objects")))
	matchesGolden(t, "request-gateway", d.Request, false)
	g := gatewayOf(t, d).Gateway
	derived := []string{"objects.rgw.buckets.data", "objects.rgw.buckets.index", "objects.rgw.buckets.non-ec", "objects.rgw.control", "objects.rgw.dedup", "objects.rgw.log", "objects.rgw.meta", "objects.rgw.otp"}
	if !reflect.DeepEqual(g.ZonePools, derived) || g.IndexPool != "objects.rgw.buckets.index" || g.DataPool != "objects.rgw.buckets.data" || g.DataExtraPool != "objects.rgw.buckets.non-ec" ||
		g.Name != "objects" || g.Realm != "objects" || g.ZoneGroup != "objects" || g.Zone != "objects" || g.Endpoint != "" || len(g.Documents) != 1 {
		t.Fatalf("the gateway froze %+v", g)
	}
	var document struct {
		ServiceType string            `json:"service_type"`
		ServiceID   string            `json:"service_id"`
		Spec        map[string]string `json:"spec"`
	}
	if err := json.Unmarshal(g.Documents[0], &document); err != nil || document.ServiceType != "rgw" || document.ServiceID != "objects" ||
		!reflect.DeepEqual(document.Spec, map[string]string{"rgw_realm": "objects", "rgw_zone": "objects", "rgw_zonegroup": "objects"}) {
		t.Fatalf("the rgw document %s (%v)", g.Documents[0], err)
	}
	renamed := childOf(api.StorageObjectGateway, "objects", "serviceRef", "rgw.objects", "realm", "r1", "zoneGroup", "g1", "zone", "z1",
		"pools", requestMap("indexPoolRef", "index"), "endpoint", requestMap("fqdn", "s3.example.test"))
	g = gatewayOf(t, gatewayBlock(t, planCatalog(3, 3, withPairAndHTTPRGW, renamed, rgwPool("index")))).Gateway
	if g.IndexPool != "index" || g.DataPool != "z1.rgw.buckets.data" || !slices.Contains(g.ZonePools, "index") || slices.Contains(g.ZonePools, "z1.rgw.buckets.index") ||
		len(g.ZonePools) != len(ZonePoolSuffixes) || g.Endpoint != "s3.example.test" || g.Realm != "r1" || g.ZoneGroup != "g1" || g.Zone != "z1" {
		t.Fatalf("a declared index pool froze %+v", g)
	}
	if !strings.Contains(string(g.Documents[0]), `"rgw_realm":"r1","rgw_zone":"z1","rgw_zonegroup":"g1"`) {
		t.Fatalf("the rgw document does not carry the gateway's names: %s", g.Documents[0])
	}
}

func TestGatewayBlockDependsOnDerivedPoolsAndCluster(t *testing.T) {
	d := gatewayBlock(t, planCatalog(3, 3, withPairAndHTTPRGW, objectGateway("objects")))
	want := []string{"ceph-cluster-example", "ceph-gateway-objects-pool-buckets-data", "ceph-gateway-objects-pool-buckets-index", "ceph-gateway-objects-pool-buckets-non-ec",
		"ceph-gateway-objects-pool-control", "ceph-gateway-objects-pool-dedup", "ceph-gateway-objects-pool-log", "ceph-gateway-objects-pool-meta", "ceph-gateway-objects-pool-otp", "ceph-image-example"}
	if d.Kind != string(api.StorageObjectGateway) || d.Object != "objects" || d.Stage != reconciliation.StageClusters || !reflect.DeepEqual(d.Exclusive, []string{"ceph/example"}) ||
		len(d.Requires) != 0 || !reflect.DeepEqual(d.Dependencies, want) || d.Description != "realize Ceph object gateway objects" || d.ID != GatewayBlockID("objects") {
		t.Fatalf("the gateway block %+v", d)
	}
	d = gatewayBlock(t, planCatalog(3, 3, withPairAndHTTPRGW, objectGateway("objects", "pools", requestMap("indexPoolRef", "index")), rgwPool("index")))
	if slices.Contains(d.Dependencies, "ceph-gateway-objects-pool-buckets-index") || len(d.Dependencies) != len(want)-1 ||
		!reflect.DeepEqual(d.Requires, []reconciliation.ObjectRef{{Kind: string(api.StoragePool), Object: "index"}}) {
		t.Fatalf("the gateway block with a declared index pool %+v", d)
	}
	removal, err := New(api.StorageObjectGateway, nil).Removal(context.Background(), reconciliation.Block{BlockDefinition: d})
	if err != nil || removal.Description != "remove Ceph object gateway objects" {
		t.Fatalf("the gateway removal %+v (%v)", removal, err)
	}
}

func TestGatewayDeadlineCoversBoundedPhases(t *testing.T) {
	for _, hosts := range []int{3, 6} {
		r := gatewayOf(t, gatewayBlock(t, planCatalog(hosts, hosts, withPairAndHTTPRGW, objectGateway("objects"))))
		for operation, want := range map[string]time.Duration{"apply": 3600 * time.Second, "observe": 720 * time.Second, "destroy": 1380 * time.Second} {
			if got, err := cephDeadline(r, operation); err != nil || got != want {
				t.Fatalf("%d hosts %s deadline %s (%v), want %s", hosts, operation, got, err, want)
			}
		}
	}
}

func gatewayExecution(t *testing.T, d reconciliation.BlockDefinition, change func(*Request)) lifecycle.Execution {
	t.Helper()
	r, err := DecodeRequest(d.Request)
	if err != nil {
		t.Fatal(err)
	}
	if change != nil {
		change(&r)
	}
	data, err := r.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	return lifecycle.Execution{ApplyOperation: "apply-original", Operation: "apply-original", Context: "example",
		Block: reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: d.ID, Kind: d.Kind, Object: d.Object, Request: data}, RequestDigest: strings.Repeat("b", 64)}}
}

func TestGatewayIdentity(t *testing.T) {
	definitions := gatewayBlocks(t, planCatalog(3, 3, withPairAndHTTPRGW, objectGateway("objects")))
	d := definitions[len(definitions)-1]
	capability := New(api.StorageObjectGateway, nil)
	r, err := capability.identity(context.Background(), gatewayExecution(t, d, nil))
	if err != nil || r.Gateway == nil || r.Owner != "apply-original" || r.FSID != FSIDFor("apply-original", "example") {
		t.Fatalf("the gateway block identity %+v (%v)", r, err)
	}
	foreign, renumbered := d, d
	foreign.Object = "other"
	renumbered.ID = "ceph-gateway-other"
	gateway := gatewayOf(t, d).Gateway
	for name, e := range map[string]lifecycle.Execution{
		"another object":             gatewayExecution(t, foreign, nil),
		"another identity":           gatewayExecution(t, renumbered, nil),
		"gateway without its member": gatewayExecution(t, d, func(r *Request) { r.Gateway = nil }),
		"pool carrying a gateway":    gatewayExecution(t, definitions[0], func(r *Request) { r.Gateway = gateway }),
		"another gateway name":       gatewayExecution(t, d, func(r *Request) { r.Gateway.Name = "other" }),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := capability.identity(context.Background(), e)
			refusedWith(t, err, "lifecycle.state", "Ceph request does not match its frozen resource identity", "")
		})
	}
	_, err = New(api.StoragePool, nil).identity(context.Background(), gatewayExecution(t, d, nil))
	refusedWith(t, err, "lifecycle.state", "Ceph request does not match its frozen resource identity", "")
}

func gatewayRuntimeRequest() Request {
	r := runtimeRequest()
	r.Action = "gateway"
	r.Gateway = &Gateway{Name: "objects", Realm: "objects", ZoneGroup: "objects", Zone: "objects", Endpoint: "s3.example.test", Documents: []json.RawMessage{
		json.RawMessage(`{"placement":{"count":2,"hosts":["node-1","node-2"]},"service_id":"objects","service_type":"rgw","spec":{"rgw_realm":"objects","rgw_zone":"objects","rgw_zonegroup":"objects"}}`)}}
	return r
}

func gatewayEvidence(r Request) Evidence {
	e := readyEvidence(r)
	e.Resource = ResourceEvidence{ID: "0b8c6f2e-3c1d-4a5e-9f70-1a2b3c4d5e6f", Path: "s3.example.test"}
	e.Daemons = []UnitDaemon{{Host: "node-2", Name: "rgw.objects.node-2.abcdef", Service: "rgw.objects"}}
	return e
}

func TestGatewayEvidence(t *testing.T) {
	r := gatewayRuntimeRequest()
	if effect, err := EvaluateEvidence(evidenceBytes(t, gatewayEvidence(r)), r, "digest", false); err != nil || effect != reconciliation.EffectCompleted {
		t.Fatalf("ready gateway evidence is %s (%v)", effect, err)
	}
	cases := map[string]func(*Evidence){
		"another endpoint":          func(e *Evidence) { e.Resource.Path = "other.example.test" },
		"no endpoint":               func(e *Evidence) { e.Resource.Path = "" },
		"uppercase zone id":         func(e *Evidence) { e.Resource.ID = strings.ToUpper(e.Resource.ID) },
		"numeric zone id":           func(e *Evidence) { e.Resource.ID = "7" },
		"no zone id":                func(e *Evidence) { e.Resource.ID = "" },
		"daemon off document hosts": func(e *Evidence) { e.Daemons[0].Host = "node-3" },
		"daemon of another service": func(e *Evidence) { e.Daemons[0].Service = "rgw.other" },
		"no daemon":                 func(e *Evidence) { e.Daemons = []UnitDaemon{} },
		"client":                    func(e *Evidence) { e.Client = "client.objects" },
	}
	for name, mutate := range cases {
		e := gatewayEvidence(r)
		mutate(&e)
		if effect, err := EvaluateEvidence(evidenceBytes(t, e), r, "digest", false); err == nil || effect != reconciliation.EffectUnknown {
			t.Errorf("gateway evidence with %s is %s (%v)", name, effect, err)
		}
	}
	absent := Evidence{Version: EvidenceVersion, Request: "digest", Action: r.Action, Owner: r.Owner, FSID: r.FSID, Owned: true, Hosts: hostNames(r),
		Monitors: []string{}, MGRStandbys: []string{}, MonAddrs: []MonitorAddress{}, OSDs: []OSDEvidence{}, Daemons: []UnitDaemon{}}
	if effect, err := EvaluateEvidence(evidenceBytes(t, absent), r, "digest", false); err != nil || effect != reconciliation.EffectNoEffect {
		t.Fatalf("absent gateway evidence is %s (%v)", effect, err)
	}
	for name, mutate := range map[string]func(*Evidence){
		"a daemon":   func(e *Evidence) { e.Daemons = gatewayEvidence(r).Daemons },
		"a resource": func(e *Evidence) { e.Resource = gatewayEvidence(r).Resource },
	} {
		e := absent
		mutate(&e)
		if effect, err := EvaluateEvidence(evidenceBytes(t, e), r, "digest", false); err == nil || effect != reconciliation.EffectUnknown {
			t.Errorf("absent gateway evidence carrying %s is %s (%v)", name, effect, err)
		}
	}
	pool := runtimeRequest()
	pool.Action, pool.Pool = "pool", &Pool{Name: "objects.rgw.meta"}
	carried := readyEvidence(pool)
	carried.Resource = ResourceEvidence{ID: "7", Path: "s3.example.test"}
	if effect, err := EvaluateEvidence(evidenceBytes(t, carried), pool, "digest", false); err == nil || effect != reconciliation.EffectUnknown {
		t.Fatalf("pool evidence carrying an endpoint path is %s (%v)", effect, err)
	}
}

func TestPGBudgetCountsTheSharedRGWRootOnce(t *testing.T) {
	const remedy = "add OSDs, lower pgNumMin, or declare fewer pools on that rule"
	catalogFor := func(n int) api.Catalog {
		children := []api.Object{replicatedPool("volumes", "autoscale", requestMap("pgNumMin", n))}
		for _, name := range []string{"first", "second"} {
			children = append(children, childOf(api.StorageObjectGateway, name, "serviceRef", "rgw."+name, "realm", name, "zoneGroup", name, "zone", name))
		}
		return planCatalog(6, 6, withHTTPRGW("first", "second"), children...)
	}
	if _, err := kindPlan(api.StorageObjectGateway, catalogFor(205)); err != nil {
		t.Fatalf("two gateways at 1500 placement groups did not plan: %v", diagnostics.Of(err))
	}
	_, err := kindPlan(api.StorageObjectGateway, catalogFor(206))
	refusedWith(t, err, "api.invariant", "pool placement groups for CRUSH rule replicated_rule exceed the per-OSD bound (1502 > 250 × 6)", remedy)
}
