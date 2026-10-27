package ceph

import (
	"bytes"
	"maps"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func corePair() api.Value {
	return requestMap("osd", requestMap("osd_memory_target_autotune", "false", "osd_memory_target", "2147483648"),
		"global", requestMap("osd_pool_default_size", "2", "osd_pool_default_min_size", "2"))
}

func planCatalog(hosts, osdHosts int, change func(api.Value) api.Value, children ...api.Object) api.Catalog {
	objects := poolRequestCatalog(hosts, osdHosts).Objects()
	for i, object := range objects {
		if object.Kind() == api.CephCluster && change != nil {
			objects[i] = object.WithSpec(change(object.Spec()))
		}
	}
	return api.NewCatalog(append(objects, children...))
}

func withCorePair(spec api.Value) api.Value { return spec.WithPath(corePair(), "config") }

func childOf(kind api.Kind, name string, pairs ...any) api.Object {
	return api.NewObject(kind, name, api.Value{}, requestMap(append([]any{"clusterRef", "example"}, pairs...)...))
}

func replicatedPool(name string, pairs ...any) api.Object {
	return childOf(api.CephPool, name, append([]any{"role", "rbd", "replicated", requestMap("size", 2, "minSize", 2)}, pairs...)...)
}

func erasurePool(name string, erasure api.Value, pairs ...any) api.Object {
	return childOf(api.CephPool, name, append([]any{"role", "rbd", "erasure", erasure}, pairs...)...)
}

func planned(t *testing.T, catalog api.Catalog, kind api.Kind, name string) (Request, error) {
	t.Helper()
	object, found := catalog.Find(kind, name)
	if !found {
		t.Fatalf("the catalog has no %s %s", kind, name)
	}
	return New(kind, nil).request(catalog, object, "synthetic")
}

func refusedWith(t *testing.T, err error, code, message, remedy string) {
	t.Helper()
	d := diagnostics.Of(err)
	if len(d) != 1 || d[0].Code != code || d[0].Message != message || d[0].Remediation != remedy {
		t.Fatalf("refusal %+v, want %s %q %q", d, code, message, remedy)
	}
}

func TestErasureParametersRefusedAtPlan(t *testing.T) {
	catalog := planCatalog(6, 6, nil, erasurePool("volumes", requestMap("dataChunks", 4, "codingChunks", 2, "parameters", requestMap("l", "3"))))
	_, err := planned(t, catalog, api.CephPool, "volumes")
	refusedWith(t, err, "lifecycle.unsupported", "CephPool volumes declares spec.erasure.parameters; arbitrary native erasure-code parameters are not admitted", "remove spec.erasure.parameters and use the typed erasure fields")
}

func TestAutoscaleWarnRefusesAtPlan(t *testing.T) {
	catalog := planCatalog(3, 3, nil, replicatedPool("volumes", "autoscale", requestMap("mode", "warn")))
	_, err := planned(t, catalog, api.CephPool, "volumes")
	refusedWith(t, err, "lifecycle.unsupported", "CephPool volumes selects autoscale mode warn, whose placement-group health warnings the core readiness never tolerates", "set spec.autoscale.mode to on or off, or omit it (on); Bootwright does not support warn yet")
	for _, mode := range []string{"on", "off"} {
		catalog := planCatalog(3, 3, nil, replicatedPool("volumes", "autoscale", requestMap("mode", mode)))
		if r, err := planned(t, catalog, api.CephPool, "volumes"); err != nil || r.Pool.Autoscale.Mode != mode {
			t.Fatalf("autoscale mode %s did not plan: %+v %v", mode, r.Pool, err)
		}
	}
}

func TestMirroringRefusesNamingB483(t *testing.T) {
	catalog := planCatalog(3, 3, nil, replicatedPool("volumes", "mirroring", requestMap("mode", "image")))
	_, err := planned(t, catalog, api.CephPool, "volumes")
	refusedWith(t, err, "lifecycle.unsupported", "CephPool volumes declares mirroring without a CephReplication owner", "declare a CephReplication that names this pool, or remove spec.mirroring")
}

func TestPoolWithoutRoleRefusesAtPlan(t *testing.T) {
	catalog := planCatalog(3, 3, nil, childOf(api.CephPool, "volumes", "replicated", requestMap("size", 2, "minSize", 2)))
	_, err := planned(t, catalog, api.CephPool, "volumes")
	refusedWith(t, err, "lifecycle.unsupported", "CephPool volumes has no role, so no consumer enables its application and the cluster would report POOL_APP_NOT_ENABLED", "set spec.role of CephPool volumes to rbd, cephfsMetadata, cephfsData or rgw")
}

func TestNonHostFailureDomainRefuses(t *testing.T) {
	catalog := planCatalog(3, 3, nil, childOf(api.CephPlacementPolicy, "racks", "ruleName", "racks", "failureDomain", "rack"))
	_, err := planned(t, catalog, api.CephPlacementPolicy, "racks")
	refusedWith(t, err, "lifecycle.unsupported", "placement policy racks selects CRUSH failure domain rack; only host is realized", "set spec.failureDomain to host or omit it; Bootwright does not support site or rack failure domains yet")
	for _, domain := range []any{"host", nil} {
		pairs := []any{"ruleName", "fast", "crushDeviceClass", "ssd", "replicated", requestMap("size", 2)}
		if domain != nil {
			pairs = append(pairs, "failureDomain", domain)
		}
		catalog := planCatalog(3, 3, nil, childOf(api.CephPlacementPolicy, "fast", pairs...))
		r, err := planned(t, catalog, api.CephPlacementPolicy, "fast")
		want := Policy{DeviceClass: "ssd", FailureDomain: "host", Name: "fast", Root: "default", Rule: "fast", Size: 2}
		if err != nil || r.Action != "policy" || r.Policy == nil || *r.Policy != want {
			t.Fatalf("failure domain %v did not plan as host: %+v %v", domain, r.Policy, err)
		}
	}
}

func TestNonDefaultCrushRootRefusesAtPlan(t *testing.T) {
	catalog := planCatalog(6, 6, nil, erasurePool("volumes", requestMap("dataChunks", 4, "codingChunks", 2, "crushRoot", "fast-root")))
	_, err := planned(t, catalog, api.CephPool, "volumes")
	refusedWith(t, err, "lifecycle.unsupported", "CephPool volumes selects CRUSH root fast-root; only the bootstrap root default is realized", "omit spec.erasure.crushRoot or set it to default; Bootwright does not support other CRUSH roots yet")
	catalog = planCatalog(6, 6, nil, erasurePool("volumes", requestMap("dataChunks", 4, "codingChunks", 2, "crushRoot", "default")))
	if r, err := planned(t, catalog, api.CephPool, "volumes"); err != nil || r.Pool.Erasure.Root != "default" {
		t.Fatalf("the default root did not plan: %v", err)
	}
}

func TestPolicyNamingStretchRuleRefusesNamingB486(t *testing.T) {
	stretch := func(spec api.Value) api.Value {
		return spec.WithPath(requestMap("failureDomain", "datacenter"), "topology", "stretch")
	}
	catalog := planCatalog(3, 3, stretch, childOf(api.CephPlacementPolicy, "stretched", "ruleName", "stretch-rule", "failureDomain", "datacenter"),
		childOf(api.CephPlacementPolicy, "other", "ruleName", "other"))
	_, err := planned(t, catalog, api.CephPlacementPolicy, "stretched")
	refusedWith(t, err, "lifecycle.unsupported", "placement policy stretched names the stretch rule stretch-rule, which stretch mode (B486) owns", "B486 decides whether a stretch policy binds the stretch rule; until then omit this policy or give it another ruleName")
	_, err = planned(t, catalog, api.CephPlacementPolicy, "other")
	if d := diagnostics.Of(err); len(d) != 1 || strings.Contains(d[0].Message, "B486") || !strings.Contains(d[0].Message, "topology profile") {
		t.Fatalf("a policy naming another rule on a stretch cluster: %+v", d)
	}
}

func TestPGBudgetCountsWidthAndMetadataMinimum(t *testing.T) {
	for _, test := range []struct {
		pgs  int
		want string
	}{{374, ""}, {375, "pool placement groups for CRUSH rule replicated_rule exceed the per-OSD bound (752 > 250 × 3)"}} {
		catalog := planCatalog(3, 3, withCorePair, replicatedPool("volumes", "autoscale", requestMap("pgNumMin", test.pgs)))
		r, err := planned(t, catalog, api.CephPool, "volumes")
		if test.want == "" {
			if err != nil || r.Pool.PGNum != test.pgs {
				t.Fatalf("pgNumMin %d did not plan: %v", test.pgs, err)
			}
			continue
		}
		refusedWith(t, err, "api.invariant", test.want, "add OSDs, lower pgNumMin, or declare fewer pools on that rule")
	}
	for pgs, want := range map[int]string{62: "", 250: "", 251: "pool placement groups for CRUSH rule volumes exceed the per-OSD bound (1506 > 250 × 6)"} {
		catalog := planCatalog(6, 6, nil, erasurePool("volumes", requestMap("dataChunks", 4, "codingChunks", 2), "autoscale", requestMap("pgNumMin", pgs)))
		_, err := planned(t, catalog, api.CephPool, "volumes")
		if want == "" && err != nil {
			t.Fatalf("a 4+2 pool of %d placement groups did not plan: %v", pgs, err)
		}
		if want != "" {
			refusedWith(t, err, "api.invariant", want, "add OSDs, lower pgNumMin, or declare fewer pools on that rule")
		}
	}
	metadata := childOf(api.CephPool, "metadata", "role", "cephfsMetadata", "replicated", requestMap("size", 2), "autoscale", requestMap("pgNumMax", 8))
	if _, _, width, pgs, ok := placementOf(api.NewCatalog(nil), metadata); !ok || width != 2 || pgs != 16 {
		t.Fatalf("a cephfsMetadata pool counts %d placement groups of width %d, want 16 of width 2", pgs, width)
	}
}

func TestPGBudgetIsPerCrushRule(t *testing.T) {
	policy := childOf(api.CephPlacementPolicy, "fast", "ruleName", "fast-hosts", "crushDeviceClass", "ssd", "replicated", requestMap("size", 2, "minSize", 2))
	bulk := replicatedPool("bulk", "autoscale", requestMap("pgNumMin", 300))
	catalog := planCatalog(6, 6, withCorePair, policy, bulk,
		replicatedPool("first", "placementPolicyRef", "fast", "autoscale", requestMap("pgNumMin", 200)),
		replicatedPool("second", "placementPolicyRef", "fast", "autoscale", requestMap("pgNumMin", 200)))
	_, err := planned(t, catalog, api.CephPool, "first")
	refusedWith(t, err, "api.invariant", "pool placement groups for CRUSH rule fast-hosts exceed the per-OSD bound (800 > 250 × 3)", "add OSDs, lower pgNumMin, or declare fewer pools on that rule")
	if r, err := planned(t, catalog, api.CephPool, "bulk"); err != nil || r.Pool.CrushRule != defaultRule {
		t.Fatalf("a replicated_rule pool counted the placement groups of another rule: %v", err)
	}
	catalog = planCatalog(3, 3, withCorePair, policy, bulk, replicatedPool("solo", "placementPolicyRef", "fast", "autoscale", requestMap("pgNumMin", 300)))
	for _, name := range []string{"bulk", "solo"} {
		if _, err := planned(t, catalog, api.CephPool, name); err != nil {
			t.Fatalf("pool %s fits its own CRUSH rule but did not plan: %v", name, err)
		}
	}
}

func TestPGBudgetMgrPoolUsesEffectiveDefaultSize(t *testing.T) {
	pool := replicatedPool("volumes", "autoscale", requestMap("pgNumMin", 374))
	if _, err := planned(t, planCatalog(3, 3, withCorePair, pool), api.CephPool, "volumes"); err != nil {
		t.Fatalf("748 placement groups and a two-replica .mgr did not plan: %v", err)
	}
	_, err := planned(t, planCatalog(3, 3, nil, pool), api.CephPool, "volumes")
	refusedWith(t, err, "api.invariant", "pool placement groups for CRUSH rule replicated_rule exceed the per-OSD bound (751 > 250 × 3)", "add OSDs, lower pgNumMin, or declare fewer pools on that rule")
}

func TestRatiosFrozenAsDecimalStrings(t *testing.T) {
	pool := replicatedPool("volumes", "autoscale", requestMap("targetSizeRatio", api.NumberValue("0.2")),
		"compression", requestMap("mode", "aggressive", "requiredRatio", api.NumberValue("0.875")))
	r, err := planned(t, planCatalog(3, 3, nil, pool), api.CephPool, "volumes")
	if err != nil || r.Pool.Autoscale.TargetRatio != "0.2" || r.Pool.Compression.RequiredRatio != "0.875" || r.Pool.Compression.Mode != "aggressive" {
		t.Fatalf("ratios froze as %+v %+v (%v)", r.Pool.Autoscale, r.Pool.Compression, err)
	}
	first, err := r.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeRequest(first)
	if err != nil {
		t.Fatal(err)
	}
	if again, err := decoded.Canonical(); err != nil || !bytes.Equal(first, again) {
		t.Fatal("a frozen ratio did not re-freeze byte-identically")
	}
}

func TestTargetSizeBytesConvertsIEC(t *testing.T) {
	for text, want := range map[string]int64{"10G": 10737418240, "1KB": 1024, "7": 7, "3B": 3} {
		pool := replicatedPool("volumes", "autoscale", requestMap("targetSizeBytes", text))
		r, err := planned(t, planCatalog(3, 3, nil, pool), api.CephPool, "volumes")
		if err != nil || r.Pool.Autoscale.TargetBytes != want {
			t.Fatalf("%s froze as %d (%v), want %d", text, r.Pool.Autoscale.TargetBytes, err, want)
		}
	}
	pool := replicatedPool("volumes", "autoscale", requestMap("targetSizeBytes", "8EB"))
	_, err := planned(t, planCatalog(3, 3, nil, pool), api.CephPool, "volumes")
	refusedWith(t, err, "api.invariant", "CephPool volumes sets spec.autoscale.targetSizeBytes above 9223372036854775807, which the native option cannot hold", "lower spec.autoscale.targetSizeBytes to at most 9223372036854775807")
	pool = replicatedPool("volumes", "autoscale", requestMap("pgNumMax", 32769))
	_, err = planned(t, planCatalog(3, 3, nil, pool), api.CephPool, "volumes")
	refusedWith(t, err, "api.invariant", "CephPool volumes sets spec.autoscale.pgNumMax above 32768, which the native option cannot hold", "lower spec.autoscale.pgNumMax to at most 32768")
}

func TestClientOnErasurePoolRefusesAtPlan(t *testing.T) {
	catalog := planCatalog(6, 6, nil, erasurePool("volumes", requestMap("dataChunks", 4, "codingChunks", 2)),
		childOf(api.CephClient, "consumer", "poolRef", "volumes", "access", "readWrite"))
	_, err := planned(t, catalog, api.CephClient, "consumer")
	refusedWith(t, err, "lifecycle.unsupported", "CephClient consumer names erasure-coded pool volumes; RBD image headers and the client's pool-scoped caps need a replicated RBD pool", "point spec.poolRef at a replicated rbd pool and name the erasure-coded pool in the spec.dataPoolRef of the CephRBDImages on it; the client's caps then include that data pool")
}

func TestPoolRequestGoldenReplicatedWithPolicy(t *testing.T) {
	policy := childOf(api.CephPlacementPolicy, "fast", "ruleName", "fast-hosts", "crushDeviceClass", "ssd", "replicated", requestMap("size", 2, "minSize", 2))
	pool := childOf(api.CephPool, "volumes", "role", "rbd", "placementPolicyRef", "fast",
		"quota", requestMap("maxBytes", 1073741824), "compression", requestMap("mode", "passive", "algorithm", "zstd"))
	r, err := planned(t, planCatalog(3, 3, withCorePair, policy, pool), api.CephPool, "volumes")
	if err != nil || r.Pool.CrushRule != "fast-hosts" || r.Pool.Size != 2 || r.Pool.MinSize != 2 || r.Pool.Overwrites || r.Pool.Erasure != nil {
		t.Fatalf("a policy pool planned as %+v (%v)", r.Pool, err)
	}
	data, err := r.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "request-pool-policy", data, false)
}

func TestPoolRequestGoldenErasureRBD(t *testing.T) {
	r, err := planned(t, planCatalog(6, 6, nil, erasurePool("volumes", requestMap("dataChunks", 4, "codingChunks", 2))), api.CephPool, "volumes")
	if err != nil {
		t.Fatal(err)
	}
	e := r.Pool.Erasure
	if !r.Pool.Overwrites || r.Pool.CrushRule != "volumes" || e.Profile != "volumes" || e.StripeUnit != 4096 || e.Plugin != "isa" || e.Technique != "reed_sol_van" ||
		r.Pool.Size != 6 || r.Pool.MinSize != 5 || r.Pool.Application != "rbd" || e.FailureDomain != "host" || e.Root != "default" {
		t.Fatalf("an erasure-coded rbd pool planned as %+v %+v", r.Pool, e)
	}
	data, err := r.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "request-pool-erasure", data, false)
	r, err = planned(t, planCatalog(6, 6, nil, childOf(api.CephPool, "objects", "role", "rgw", "erasure", requestMap("dataChunks", 4, "codingChunks", 2))), api.CephPool, "objects")
	if err != nil || r.Pool.Overwrites || r.Pool.Application != "rgw" {
		t.Fatalf("an erasure-coded rgw pool enables overwrites: %+v %v", r.Pool, err)
	}
	r, err = planned(t, planCatalog(6, 6, nil, childOf(api.CephPool, "files", "role", "cephfsData", "erasure", requestMap("dataChunks", 4, "codingChunks", 2))), api.CephPool, "files")
	if err != nil || !r.Pool.Overwrites || r.Pool.Application != "cephfs" {
		t.Fatalf("an erasure-coded cephfsData pool does not enable overwrites: %+v %v", r.Pool, err)
	}
}

func TestErasureProfileDefaultsFrozenPerPlugin(t *testing.T) {
	base := map[string]string{"crush-num-failure-domains": "0", "crush-osds-per-failure-domain": "0"}
	unclassed := maps.Clone(base)
	unclassed["crush-device-class"] = ""
	jerasure := maps.Clone(unclassed)
	jerasure["w"] = "8"
	jerasure["jerasure-per-chunk-alignment"] = "false"
	for name, test := range map[string]struct {
		erasure api.Value
		want    map[string]string
	}{
		"isa without a class": {requestMap("dataChunks", 4, "codingChunks", 2), unclassed},
		"isa with ssd":        {requestMap("dataChunks", 4, "codingChunks", 2, "crushDeviceClass", "ssd"), base},
		"jerasure 4+2":        {requestMap("dataChunks", 4, "codingChunks", 2, "plugin", "jerasure"), jerasure},
		"jerasure 2+2":        {requestMap("dataChunks", 2, "codingChunks", 2, "plugin", "jerasure"), jerasure},
		"isa 3+2":             {requestMap("dataChunks", 3, "codingChunks", 2), unclassed},
	} {
		r, err := planned(t, planCatalog(6, 6, nil, erasurePool("volumes", test.erasure)), api.CephPool, "volumes")
		if err != nil || !maps.Equal(r.Pool.Erasure.ProfileDefaults, test.want) {
			t.Fatalf("%s froze profile defaults %v (%v), want %v", name, r.Pool.Erasure, err, test.want)
		}
	}
}

func TestErasurePoolMinSizeIsTheMonitorDerivation(t *testing.T) {
	for _, test := range []struct{ k, m, size, minSize int }{{2, 1, 3, 2}, {4, 1, 5, 4}, {2, 2, 4, 3}, {4, 2, 6, 5}} {
		r, err := planned(t, planCatalog(6, 6, nil, erasurePool("volumes", requestMap("dataChunks", test.k, "codingChunks", test.m))), api.CephPool, "volumes")
		if err != nil || r.Pool.Size != test.size || r.Pool.MinSize != test.minSize {
			t.Fatalf("an erasure-coded %d+%d pool planned %+v (%v), want size %d and min_size %d", test.k, test.m, r.Pool, err, test.size, test.minSize)
		}
	}
}

func TestPoolReplicasFallBackToPolicyBeforeDefaults(t *testing.T) {
	policy := childOf(api.CephPlacementPolicy, "durable", "ruleName", "durable-hosts", "replicated", requestMap("size", 3, "minSize", 3))
	inherited := childOf(api.CephPool, "volumes", "role", "rbd", "placementPolicyRef", "durable")
	r, err := planned(t, planCatalog(4, 4, nil, policy, inherited), api.CephPool, "volumes")
	if err != nil || r.Pool.Size != 3 || r.Pool.MinSize != 3 {
		t.Fatalf("a pool without replicated values planned %+v (%v), want the policy's 3/3", r.Pool, err)
	}
	sized := childOf(api.CephPool, "volumes", "role", "rbd", "placementPolicyRef", "durable", "replicated", requestMap("size", 3))
	r, err = planned(t, planCatalog(4, 4, nil, policy, sized), api.CephPool, "volumes")
	if err != nil || r.Pool.Size != 3 || r.Pool.MinSize != 3 {
		t.Fatalf("a pool overriding only size planned %+v (%v), want minSize 3 from the policy", r.Pool, err)
	}
}
