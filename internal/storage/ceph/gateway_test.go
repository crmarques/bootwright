package ceph

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func withRGW(spec api.Value) api.Value {
	return spec.WithPath(api.ListValue(append(spec.Get("ceph", "services").Items(), rgwService("objects"))...), "ceph", "services")
}

func withPairAndRGW(spec api.Value) api.Value { return withRGW(withCorePair(spec)) }

func objectGateway(name string, pairs ...any) api.Object {
	return childOf(api.StorageObjectGateway, name, append([]any{"serviceRef", "rgw.objects", "realm", "objects", "zoneGroup", "objects", "zone", "objects"}, pairs...)...)
}

func rgwPool(name string) api.Object {
	return childOf(api.StoragePool, name, "type", "replicated", "role", "rgw", "replicated", requestMap("size", 2, "minSize", 2))
}

func kindPlan(kind api.Kind, catalog api.Catalog) (lifecycle.CapabilityPlan, error) {
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: "synthetic"}, State: compilation.NewState(catalog, catalog, nil), Controller: "controller"}
	return New(kind, nil).Plan(context.Background(), input)
}

func gatewayBlocks(t *testing.T, catalog api.Catalog) []reconciliation.BlockDefinition {
	t.Helper()
	plan, err := kindPlan(api.StorageObjectGateway, catalog)
	if err != nil {
		t.Fatalf("the gateway did not plan: %v", diagnostics.Of(err))
	}
	return plan.Definitions
}

func blockIDs(definitions []reconciliation.BlockDefinition) []string {
	ids := []string{}
	for _, d := range definitions {
		ids = append(ids, d.ID)
	}
	return ids
}

func poolBlocks(definitions []reconciliation.BlockDefinition) []reconciliation.BlockDefinition {
	out := []reconciliation.BlockDefinition{}
	for _, d := range definitions {
		if strings.HasPrefix(d.ID, "ceph-gateway-"+d.Object+"-pool-") {
			out = append(out, d)
		}
	}
	return out
}

func zonePool(suffix string, size int) Pool {
	return Pool{Name: "objects.rgw." + suffix, Type: "replicated", Role: "rgw", Application: "rgw", CrushRule: "replicated_rule", Autoscale: PoolAutoscale{Mode: "on"}, PGNum: 32, Size: size, MinSize: 2}
}

func TestGatewayDerivedPoolsAreFrozenSuffixSet(t *testing.T) {
	suffixes := []string{"meta", "control", "dedup", "log", "otp", "buckets-index", "buckets-non-ec", "buckets-data"}
	native := []string{"meta", "control", "dedup", "log", "otp", "buckets.index", "buckets.non-ec", "buckets.data"}
	blocks := gatewayBlocks(t, planCatalog(3, 3, withPairAndRGW, objectGateway("objects", "placementPolicyRef", "fast")))
	want := []string{}
	for _, suffix := range suffixes {
		want = append(want, "ceph-gateway-objects-pool-"+suffix)
	}
	if got := blockIDs(blocks); !reflect.DeepEqual(got, append(slices.Clone(want), "ceph-gateway-objects")) {
		t.Fatalf("derived blocks %v, want %v and the gateway", got, want)
	}
	definitions := poolBlocks(blocks)
	for i, d := range definitions {
		if !reconciliation.ValidSegment(d.ID) || d.Kind != string(api.StorageObjectGateway) || d.Object != "objects" || len(d.Requires) != 0 || d.Stage != reconciliation.StageClusters ||
			!reflect.DeepEqual(d.Dependencies, []string{"ceph-cluster-example", "ceph-image-example"}) || !reflect.DeepEqual(d.Exclusive, []string{"ceph/example"}) {
			t.Fatalf("derived block %+v", d)
		}
		if d.Description != "realize Ceph zone pool objects.rgw."+native[i]+" of object gateway objects" {
			t.Fatalf("derived block description %q", d.Description)
		}
		r, err := DecodeRequest(d.Request)
		if err != nil || r.Action != "pool" || r.Pool == nil || !reflect.DeepEqual(*r.Pool, zonePool(native[i], 2)) || r.Controller != "controller" {
			t.Fatalf("derived request %+v (%v)", r.Pool, err)
		}
	}
	if len(slices.Compact(slices.Sorted(slices.Values(blockIDs(definitions))))) != len(ZonePoolSuffixes) {
		t.Fatal("derived block identities collide")
	}
	for _, d := range poolBlocks(gatewayBlocks(t, planCatalog(4, 4, withRGW, objectGateway("objects")))) {
		if r, err := DecodeRequest(d.Request); err != nil || r.Pool.Size != 3 || r.Pool.MinSize != 2 {
			t.Fatalf("default pair froze %+v (%v)", r.Pool, err)
		}
	}
	_, err := kindPlan(api.StorageObjectGateway, planCatalog(3, 3, withRGW, objectGateway("objects")))
	refusedWith(t, err, "api.invariant", "StorageObjectGateway objects derives its zone pools with the cluster's default replica size 3, which needs more than 3 OSD-bearing hosts; 3 are declared",
		"add OSD-bearing hosts to StorageCluster example, or set osd_pool_default_size and osd_pool_default_min_size to \"2\" under its spec.ceph.config.global")
	_, err = kindPlan(api.StorageObjectGateway, planCatalog(3, 2, withPairAndRGW, objectGateway("objects")))
	refusedWith(t, err, "api.invariant", "StorageObjectGateway objects derives its zone pools with the cluster's default replica size 2, which needs more than 2 OSD-bearing hosts; 2 are declared",
		"add OSD-bearing hosts to StorageCluster example")
	long := strings.Repeat("g", 31)
	_, err = kindPlan(api.StorageObjectGateway, planCatalog(3, 3, withPairAndRGW, objectGateway(long)))
	refusedWith(t, err, "api.invariant", "StorageObjectGateway "+long+" has a name too long to identify its derived zone pool blocks, whose identities hold at most 63 characters", "rename the StorageObjectGateway to at most 30 characters")
	if got := gatewayBlocks(t, planCatalog(3, 3, withPairAndRGW, objectGateway(long[:30]))); len(got) != 9 {
		t.Fatalf("a 30-character gateway planned %v", blockIDs(got))
	}
}

func TestDeclaredPoolRefReplacesDerivedBlock(t *testing.T) {
	catalog := planCatalog(3, 3, withPairAndRGW, objectGateway("objects", "pools", requestMap("indexPoolRef", "index")), rgwPool("index"))
	definitions := gatewayBlocks(t, catalog)
	if len(definitions) != 8 || slices.Contains(blockIDs(definitions), "ceph-gateway-objects-pool-buckets-index") {
		t.Fatalf("a declared index pool planned %v", blockIDs(definitions))
	}
	for _, d := range poolBlocks(definitions) {
		if r, _ := DecodeRequest(d.Request); r.Pool.Name == "objects.rgw.buckets.index" {
			t.Fatal("a declared index pool is still derived")
		}
	}
	plan, err := kindPlan(api.StoragePool, catalog)
	if err != nil || !reflect.DeepEqual(blockIDs(plan.Definitions), []string{"ceph-pool-index"}) {
		t.Fatalf("the declared pool planned %v (%v)", blockIDs(plan.Definitions), err)
	}
	all := planCatalog(3, 3, withPairAndRGW, objectGateway("objects", "pools", requestMap("indexPoolRef", "index", "dataPoolRef", "data", "dataExtraPoolRef", "extra")),
		rgwPool("index"), rgwPool("data"), rgwPool("extra"))
	want := []string{"ceph-gateway-objects-pool-meta", "ceph-gateway-objects-pool-control", "ceph-gateway-objects-pool-dedup", "ceph-gateway-objects-pool-log", "ceph-gateway-objects-pool-otp", "ceph-gateway-objects"}
	if got := blockIDs(gatewayBlocks(t, all)); !reflect.DeepEqual(got, want) {
		t.Fatalf("three declared refs planned %v", got)
	}
}

func TestPGBudgetCountsDerivedZonePools(t *testing.T) {
	const remedy = "add OSDs, lower pgNumMin, or declare fewer pools on that rule"
	for _, gateway := range []api.Object{objectGateway("objects"), objectGateway("objects", "pools", requestMap("indexPoolRef", "index"))} {
		catalogFor := func(n int, others ...api.Object) api.Catalog {
			children := append([]api.Object{gateway, replicatedPool("volumes", "autoscale", requestMap("pgNumMin", n))}, others...)
			if gateway.Spec().Has("pools") {
				children = append(children, rgwPool("index"))
			}
			return planCatalog(3, 3, withPairAndRGW, children...)
		}
		elsewhere := api.NewObject(api.StorageObjectGateway, "elsewhere", api.Value{}, requestMap("clusterRef", "other", "serviceRef", "rgw.objects", "zone", "elsewhere"))
		if _, err := planned(t, catalogFor(86, elsewhere), api.StoragePool, "volumes"); err != nil {
			t.Fatalf("750 placement groups did not plan: %v", diagnostics.Of(err))
		}
		if _, err := kindPlan(api.StorageObjectGateway, catalogFor(86)); err != nil {
			t.Fatalf("the gateway at 750 placement groups did not plan: %v", diagnostics.Of(err))
		}
		_, err := planned(t, catalogFor(87), api.StoragePool, "volumes")
		refusedWith(t, err, "api.invariant", "pool placement groups for CRUSH rule replicated_rule exceed the per-OSD bound (752 > 250 × 3)", remedy)
		_, err = kindPlan(api.StorageObjectGateway, catalogFor(87))
		refusedWith(t, err, "api.invariant", "pool placement groups for CRUSH rule replicated_rule exceed the per-OSD bound (752 > 250 × 3)", remedy)
	}
}

func TestDerivedZonePoolBlockIdentityIsBoundToItsGateway(t *testing.T) {
	definitions := gatewayBlocks(t, planCatalog(3, 3, withPairAndRGW, objectGateway("objects")))
	execution := func(d reconciliation.BlockDefinition, change func(*Request)) lifecycle.Execution {
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
	gateway := New(api.StorageObjectGateway, nil)
	r, err := gateway.identity(context.Background(), execution(definitions[0], nil))
	if err != nil || r.Owner != "apply-original" || r.FSID != FSIDFor("apply-original", "example") {
		t.Fatalf("the derived block identity %+v (%v)", r, err)
	}
	other := definitions[0]
	other.ID = definitions[1].ID
	foreign := definitions[0]
	foreign.Object = "other"
	for name, e := range map[string]lifecycle.Execution{
		"another suffix": execution(other, nil), "another gateway": execution(foreign, nil),
		"rbd role":  execution(definitions[0], func(r *Request) { r.Pool.Role = "rbd" }),
		"bare name": execution(definitions[0], func(r *Request) { r.Pool.Name = "objects-meta" }),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := gateway.identity(context.Background(), e)
			refusedWith(t, err, "lifecycle.state", "Ceph request does not match its frozen resource identity", "")
		})
	}
	_, err = New(api.StoragePool, nil).identity(context.Background(), execution(definitions[0], nil))
	refusedWith(t, err, "lifecycle.state", "Ceph request does not match its frozen resource identity", "")
	pools, err := kindPlan(api.StoragePool, planCatalog(3, 3, withPairAndRGW, rgwPool("index")))
	if err != nil || len(pools.Definitions) != 1 {
		t.Fatalf("the declared pool planned %v (%v)", pools.Definitions, err)
	}
	if _, err := New(api.StoragePool, nil).identity(context.Background(), execution(pools.Definitions[0], nil)); err != nil {
		t.Fatalf("a declared pool block lost its identity: %v", err)
	}
}
