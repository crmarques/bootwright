package ceph

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func withMDS(spec api.Value) api.Value {
	services := append(spec.Get("services").Items(), mdsRequestService("files"))
	return withCorePair(spec.WithPath(api.ListValue(services...), "services"))
}

func cephfsPool(name, role string, pairs ...any) api.Object {
	return childOf(api.CephPool, name, append([]any{"role", role, "replicated", requestMap("size", 2, "minSize", 2)}, pairs...)...)
}

func filesystemOf(pairs ...any) api.Object {
	base := []any{"mdsServiceRef", "mds.files", "metadataPoolRef", "fs-meta"}
	if !slices.Contains(pairs, any("dataPoolRefs")) {
		base = append(base, "dataPoolRefs", api.StringList("fs-data"))
	}
	return childOf(api.CephFilesystem, "files", append(base, pairs...)...)
}

func filesystemCatalog(change func(api.Value) api.Value, children ...api.Object) api.Catalog {
	return planCatalog(3, 3, change, append([]api.Object{cephfsPool("fs-meta", "cephfsMetadata"), cephfsPool("fs-data", "cephfsData")}, children...)...)
}

func plannedFilesystem(t *testing.T, catalog api.Catalog) Request {
	t.Helper()
	r, err := planned(t, catalog, api.CephFilesystem, "files")
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	return r
}

func TestFilesystemRequestGoldenFreezesDefaults(t *testing.T) {
	r := plannedFilesystem(t, filesystemCatalog(withMDS, filesystemOf("subvolumeGroups", api.ListValue(requestMap("name", "csi")))))
	f := r.Filesystem
	want := Filesystem{ActiveCount: 1, DataPools: []string{"fs-data"}, DefaultDataPool: "fs-data", MDSService: "mds.files", MetadataPool: "fs-meta", Name: "files"}
	if r.Action != "filesystem" || r.Pool != nil || f == nil || f.ActiveCount != want.ActiveCount || f.StandbyReplay || f.StandbyCountWanted != 0 || !slices.Equal(f.DataPools, want.DataPools) ||
		f.DefaultDataPool != want.DefaultDataPool || f.MDSService != want.MDSService || f.MetadataPool != want.MetadataPool || f.Name != want.Name {
		t.Fatalf("a filesystem without an mds block planned as %+v %+v", r, f)
	}
	if !slices.Equal(f.Groups, []FilesystemGroup{{Mode: "755", Name: "csi", Pool: "fs-data"}}) {
		t.Fatalf("a group with only its name froze as %+v", f.Groups)
	}
	data, err := r.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "request-filesystem", data, false)
}

func TestFilesystemFreezesDeclaredValuesAndOrders(t *testing.T) {
	groups := api.ListValue(requestMap("name", "zeta", "mode", "0770", "uid", 1000, "gid", 2000, "sizeBytes", 1073741824, "poolLayoutRef", "fs-extra"), requestMap("name", "alpha", "mode", "700"))
	refs := api.ListValue(api.StringValue("fs-extra"), requestMap("name", "fs-data", "default", true))
	fs := filesystemOf("dataPoolRefs", refs, "mds", requestMap("activeCount", 2, "standbyReplay", true, "standbyCountWanted", 1), "subvolumeGroups", groups)
	f := plannedFilesystem(t, filesystemCatalog(withMDS, cephfsPool("fs-extra", "cephfsData"), fs)).Filesystem
	if f.ActiveCount != 2 || !f.StandbyReplay || f.StandbyCountWanted != 1 || !slices.Equal(f.DataPools, []string{"fs-data", "fs-extra"}) || f.DefaultDataPool != "fs-data" {
		t.Fatalf("declared mds settings or data pools froze as %+v", f)
	}
	want := []FilesystemGroup{{Mode: "700", Name: "alpha", Pool: "fs-data"}, {GID: 2000, Mode: "770", Name: "zeta", Pool: "fs-extra", SizeBytes: 1073741824, UID: 1000}}
	if !slices.Equal(f.Groups, want) {
		t.Fatalf("declared groups froze as %+v", f.Groups)
	}
}

func TestCephFSPoolRequestCarriesDerivedFilesystem(t *testing.T) {
	catalog := filesystemCatalog(withMDS, replicatedPool("volumes"), cephfsPool("spare", "cephfsData"), filesystemOf())
	for pool, want := range map[string]string{"fs-meta": "files", "fs-data": "files", "volumes": "", "spare": ""} {
		r, err := planned(t, catalog, api.CephPool, pool)
		if err != nil || r.Pool.Filesystem != want {
			t.Fatalf("pool %s froze filesystem %q (%v), want %q", pool, r.Pool.Filesystem, diagnostics.Of(err), want)
		}
	}
	data, err := planned(t, filesystemCatalog(withMDS), api.CephPool, "fs-meta")
	if err != nil || data.Pool.Filesystem != "" {
		t.Fatalf("a metadata pool without a filesystem froze %q", data.Pool.Filesystem)
	}
	encoded, err := data.Canonical()
	if err != nil || !strings.Contains(string(encoded), `"filesystem":""`) {
		t.Fatalf("an unowned pool's filesystem member is not always present: %s", encoded)
	}
}

func TestFilesystemRequiresAllPools(t *testing.T) {
	refs := api.ListValue(requestMap("name", "fs-data", "default", true), api.StringValue("fs-extra"))
	catalog := filesystemCatalog(withMDS, cephfsPool("fs-extra", "cephfsData"), filesystemOf("dataPoolRefs", refs))
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: "synthetic"}, State: compilation.NewState(catalog, catalog, nil), Controller: "controller"}
	plan, err := New(api.CephFilesystem, nil).Plan(context.Background(), input)
	if err != nil || len(plan.Definitions) != 1 {
		t.Fatalf("the filesystem planned %+v (%v)", plan.Definitions, diagnostics.Of(err))
	}
	block := plan.Definitions[0]
	pool := func(name string) reconciliation.ObjectRef {
		return reconciliation.ObjectRef{Kind: string(api.CephPool), Object: name}
	}
	if block.ID != "ceph-filesystem-files" || block.Kind != string(api.CephFilesystem) || block.Object != "files" || !slices.Equal(block.Requires, []reconciliation.ObjectRef{pool("fs-meta"), pool("fs-data"), pool("fs-extra")}) {
		t.Fatalf("the filesystem block is %+v", block)
	}
	if !slices.Equal(block.Dependencies, []string{ClusterBlockID("example"), ImageBlockID("example")}) {
		t.Fatalf("the filesystem block depends on %v", block.Dependencies)
	}
}

func filesystemPlan(t *testing.T) reconciliation.Plan {
	t.Helper()
	change := func(spec api.Value) api.Value {
		return spec.WithPath(api.ListValue(append(spec.Get("services").Items(), mdsRequestService("files"))...), "services")
	}
	refs := api.ListValue(requestMap("name", "fs-data", "default", true), api.StringValue("fs-extra"))
	return plannedCephPlanWith(t, change, cephfsPool("fs-meta", "cephfsMetadata"), cephfsPool("fs-data", "cephfsData"), cephfsPool("fs-extra", "cephfsData"), filesystemOf("dataPoolRefs", refs))
}

func frozenFilesystem(groups ...FilesystemGroup) Request {
	r := coreRequest()
	r.Services = append(r.Services, json.RawMessage(`{"placement":{"count":2,"hosts":["node-1","node-2","node-3"]},"service_id":"files","service_type":"mds"}`))
	r.Action, r.Filesystem = "filesystem", &Filesystem{ActiveCount: 1, DataPools: []string{"fs-data"}, DefaultDataPool: "fs-data", Groups: groups, MDSService: "mds.files", MetadataPool: "fs-meta", Name: "files"}
	return r
}

func TestFilesystemInverseBeforePools(t *testing.T) {
	plan := filesystemPlan(t)
	for _, block := range plan.Blocks {
		if block.ID == "ceph-filesystem-files" {
			for _, dependency := range []string{"ceph-cluster-example", "ceph-image-example", "ceph-pool-fs-meta", "ceph-pool-fs-data", "ceph-pool-fs-extra"} {
				if !slices.Contains(block.Dependencies, dependency) {
					t.Fatalf("the planned filesystem block lacks dependency %s: %v", dependency, block.Dependencies)
				}
			}
		}
	}
	inverse, err := plan.Inverse()
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	order := map[string]int{}
	for i, block := range inverse.Blocks {
		order[block.ID] = i
	}
	for _, pool := range []string{"ceph-pool-fs-meta", "ceph-pool-fs-data", "ceph-pool-fs-extra"} {
		if _, ok := order[pool]; !ok || order["ceph-filesystem-files"] > order[pool] {
			t.Fatalf("the filesystem does not precede pool %s in removal: %v", pool, order)
		}
	}
	capability := New(api.CephFilesystem, &recordingRunner{})
	e := executionFor(t, frozenFilesystem())
	e.Block.ID, e.Block.Kind, e.Block.Object = "ceph-filesystem-files", string(api.CephFilesystem), "files"
	if q, err := capability.Quiescent(context.Background(), lifecycle.Probe{Block: e.Block, RetiredWithHosts: true}); err != nil || q.State != lifecycle.Quiescent || q.Reason != retiredReason {
		t.Fatalf("a retired filesystem is not quiescent with its hosts: %+v %v", q, err)
	}
	e.Operation, e.RetiredWithHosts, e.ApplyProved = "destroy-later", true, nil
	result, err := capability.Destroy(context.Background(), e)
	var recorded Retirement
	if err != nil || json.Unmarshal(result.Evidence, &recorded) != nil || recorded.Action != "filesystem" || !slices.Equal(recorded.RetiredWith, []string{"node-1", "node-2", "node-3"}) {
		t.Fatalf("the retired filesystem destroy recorded %+v (%v)", recorded, err)
	}
}

func TestFilesystemExecutionMatchesItsFrozenIdentity(t *testing.T) {
	capability := New(api.CephFilesystem, nil)
	e := executionFor(t, frozenFilesystem())
	e.Block.ID, e.Block.Kind, e.Block.Object = "ceph-filesystem-files", string(api.CephFilesystem), "files"
	prepared, err := capability.prepare(context.Background(), e)
	if err != nil || prepared.Action != "filesystem" || prepared.Filesystem == nil || prepared.Filesystem.Name != "files" || prepared.Owner != "apply-original" {
		t.Fatalf("a planned filesystem block did not match its frozen identity: %+v %v", prepared.Filesystem, err)
	}
	e.Block.Object = "fs-meta"
	if _, err := capability.prepare(context.Background(), e); len(diagnostics.Of(err)) != 1 || diagnostics.Of(err)[0].Code != "lifecycle.state" {
		t.Fatalf("a filesystem block named after its pool matched the filesystem identity: %v", err)
	}
	e.Block.Object, e.Block.Kind = "files", string(api.CephPool)
	if _, err := New(api.CephPool, nil).prepare(context.Background(), e); len(diagnostics.Of(err)) != 1 || diagnostics.Of(err)[0].Code != "lifecycle.state" {
		t.Fatalf("a pool capability accepted a frozen filesystem request: %v", err)
	}
}

func TestFilesystemPlanRefusesErasureDefaultAndPropagatesPoolRefusals(t *testing.T) {
	ec := childOf(api.CephPool, "fs-data", "role", "cephfsData", "erasure", requestMap("dataChunks", 4, "codingChunks", 2))
	catalog := planCatalog(6, 6, withMDS, cephfsPool("fs-meta", "cephfsMetadata"), ec, filesystemOf())
	_, err := planned(t, catalog, api.CephFilesystem, "files")
	refusedWith(t, err, "lifecycle.unsupported", "CephFilesystem files names erasure-coded pool fs-data as its default data pool; ceph fs new refuses it unless forced, and Bootwright never forces it", "mark a replicated cephfsData pool default in spec.dataPoolRefs and attach erasure-coded pools as additional data pools")
	refs := api.ListValue(requestMap("name", "fs-data", "default", true), api.StringValue("fs-ec"))
	ec = childOf(api.CephPool, "fs-ec", "role", "cephfsData", "erasure", requestMap("dataChunks", 4, "codingChunks", 2))
	if r, err := planned(t, planCatalog(6, 6, withMDS, cephfsPool("fs-meta", "cephfsMetadata"), cephfsPool("fs-data", "cephfsData"), ec, filesystemOf("dataPoolRefs", refs)), api.CephFilesystem, "files"); err != nil || !slices.Equal(r.Filesystem.DataPools, []string{"fs-data", "fs-ec"}) {
		t.Fatalf("an erasure-coded extra data pool did not plan: %+v %v", r.Filesystem, diagnostics.Of(err))
	}
	refs = api.ListValue(requestMap("name", "fs-data", "default", true), api.StringValue("fs-extra"))
	mirrored := cephfsPool("fs-extra", "cephfsData", "mirroring", requestMap("mode", "image"))
	_, err = planned(t, filesystemCatalog(withMDS, mirrored, filesystemOf("dataPoolRefs", refs)), api.CephFilesystem, "files")
	refusedWith(t, err, "lifecycle.unsupported", "CephPool fs-extra declares mirroring without a CephReplication owner", "declare a CephReplication that names this pool, or remove spec.mirroring")
	_, err = planned(t, filesystemCatalog(withCorePair, filesystemOf()), api.CephFilesystem, "files")
	refusedWith(t, err, "api.invariant", "CephCluster example projects no MDS service mds.files for CephFilesystem files", "declare the mds service entry with service_id files in spec.services")
}

func TestFilesystemDeadlineCountsPoolsGroupsAndWaits(t *testing.T) {
	r := frozenFilesystem()
	for operation, want := range map[string]time.Duration{"apply": 7560 * time.Second, "observe": 3360 * time.Second, "destroy": 3360 * time.Second} {
		if got, err := cephDeadline(r, operation); err != nil || got != want {
			t.Fatalf("%s deadline without groups is %s (%v), want %s", operation, got, err, want)
		}
	}
	r = frozenFilesystem(FilesystemGroup{Name: "a"}, FilesystemGroup{Name: "b"})
	r.Filesystem.DataPools = []string{"fs-data", "fs-extra"}
	if got, err := cephDeadline(r, "apply"); err != nil || got != (13*660+2*900+60)*time.Second {
		t.Fatalf("apply deadline with two groups and two data pools is %s (%v)", got, err)
	}
}

func TestEvidenceFilesystemIDIsDecimal(t *testing.T) {
	r := runtimeRequest()
	r.Action, r.Filesystem = "filesystem", frozenFilesystem().Filesystem
	for id, want := range map[string]reconciliation.EffectState{"7": reconciliation.EffectCompleted, "0": reconciliation.EffectCompleted, "": reconciliation.EffectUnknown,
		"0x7": reconciliation.EffectUnknown, "abc": reconciliation.EffectUnknown, "07": reconciliation.EffectUnknown} {
		e := readyEvidence(r)
		e.Resource = ResourceEvidence{ID: id}
		effect, err := EvaluateEvidence(evidenceBytes(t, e), r, "digest", false)
		if effect != want || (want == reconciliation.EffectUnknown) != (err != nil) {
			t.Fatalf("ready filesystem evidence with id %q is %s (%v), want %s", id, effect, err, want)
		}
	}
	partial := readyEvidence(r)
	partial.Ready, partial.Partial, partial.Resource = false, true, ResourceEvidence{ID: "abc"}
	if effect, err := EvaluateEvidence(evidenceBytes(t, partial), r, "digest", false); err == nil || effect != reconciliation.EffectUnknown {
		t.Fatalf("partial filesystem evidence with a hex id is %s (%v)", effect, err)
	}
	partial.Resource = ResourceEvidence{}
	if effect, err := EvaluateEvidence(evidenceBytes(t, partial), r, "digest", false); err != nil || effect != reconciliation.EffectPartial {
		t.Fatalf("partial filesystem evidence without an id is %s (%v)", effect, err)
	}
	r.Filesystem = nil
	if effect, err := EvaluateEvidence(evidenceBytes(t, readyEvidence(r)), r, "digest", false); err == nil || effect != reconciliation.EffectUnknown {
		t.Fatalf("filesystem evidence without a frozen filesystem is %s (%v)", effect, err)
	}
}

func TestFilesystemRefusalsNameTheirNextStep(t *testing.T) {
	refusals := adapterRefusals()
	generic := diagnostics.Of(refusals["native-output"])[0].Remediation
	for _, reason := range []string{"foreign-filesystem", "filesystem-identity", "filesystem-multiple-disabled", "foreign-subvolume-group", "mds-coverage"} {
		d := diagnostics.Of(refusals[reason])
		if len(d) != 1 || d[0].Code != "lifecycle.unknown" || d[0].Remediation == generic || strings.Contains(d[0].Message, "could not prove its") {
			t.Fatalf("%s reaches the operator as %+v", reason, d)
		}
	}
}
