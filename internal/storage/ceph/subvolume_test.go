package ceph

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const subvolumeID = "0f8fad5b-d9cb-469f-a165-70867728950e"

func subvolumeOf(pairs ...any) api.Object {
	return childOf(api.StorageSubvolume, "docs", append([]any{"filesystemRef", "files"}, pairs...)...)
}

func plannedSubvolume(t *testing.T, catalog api.Catalog) Request {
	t.Helper()
	r, err := planned(t, catalog, api.StorageSubvolume, "docs")
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	return r
}

func frozenSubvolume() Request {
	r := coreRequest()
	r.Action, r.Subvolume = "subvolume", &Subvolume{Filesystem: "files", Mode: "755", Name: "docs", Pool: "fs-data"}
	return r
}

func TestSubvolumeDefaultsInheritGroup(t *testing.T) {
	group := requestMap("name", "apps", "poolLayoutRef", "fs-alt", "uid", 1000, "gid", 2000, "mode", "0700")
	refs := api.ListValue(requestMap("name", "fs-data", "default", true), api.StringValue("fs-alt"))
	fs := filesystemOf("dataPoolRefs", refs, "subvolumeGroups", api.ListValue(group))
	catalog := func(subvolume api.Object) api.Catalog {
		return filesystemCatalog(withMDS, cephfsPool("fs-alt", "cephfs-data"), fs, subvolume)
	}
	for _, tt := range []struct {
		subvolume api.Object
		want      Subvolume
	}{
		{subvolumeOf("group", "apps"), Subvolume{Filesystem: "files", Group: "apps", Pool: "fs-alt", UID: 1000, GID: 2000, Mode: "755", Name: "docs"}},
		{subvolumeOf("group", "apps", "uid", 0, "gid", 0, "mode", "0750", "poolLayoutRef", "fs-data", "sizeBytes", 1073741824), Subvolume{Filesystem: "files", Group: "apps", Pool: "fs-data", Mode: "750", Name: "docs", SizeBytes: 1073741824}},
		{subvolumeOf(), Subvolume{Filesystem: "files", Pool: "fs-data", Mode: "755", Name: "docs"}},
	} {
		r := plannedSubvolume(t, catalog(tt.subvolume))
		if r.Action != "subvolume" || r.Filesystem != nil || r.Pool != nil || r.Subvolume == nil || *r.Subvolume != tt.want {
			t.Fatalf("subvolume %v froze as %+v %+v, want %+v", tt.subvolume.Spec(), r, r.Subvolume, tt.want)
		}
	}
}

func TestSubvolumeEvidencePathShape(t *testing.T) {
	r := runtimeRequest()
	r.Action, r.Subvolume = "subvolume", frozenSubvolume().Subvolume
	exact := "/volumes/_nogroup/docs/" + subvolumeID
	for _, tt := range []struct {
		resource ResourceEvidence
		want     reconciliation.EffectState
	}{
		{ResourceEvidence{ID: subvolumeID, Path: exact}, reconciliation.EffectCompleted},
		{ResourceEvidence{ID: subvolumeID, Path: "/srv/docs"}, reconciliation.EffectUnknown},
		{ResourceEvidence{ID: subvolumeID, Path: "/volumes/apps/docs/" + subvolumeID}, reconciliation.EffectUnknown},
		{ResourceEvidence{ID: "7c9e6679-7425-40de-944b-e07fc1f90ae7", Path: exact}, reconciliation.EffectUnknown},
		{ResourceEvidence{ID: strings.ToUpper(subvolumeID), Path: "/volumes/_nogroup/docs/" + strings.ToUpper(subvolumeID)}, reconciliation.EffectUnknown},
		{ResourceEvidence{Path: exact}, reconciliation.EffectUnknown},
		{ResourceEvidence{ID: subvolumeID}, reconciliation.EffectUnknown},
		{ResourceEvidence{}, reconciliation.EffectUnknown},
	} {
		e := readyEvidence(r)
		e.Resource = tt.resource
		effect, err := EvaluateEvidence(evidenceBytes(t, e), r, "digest", false)
		if effect != tt.want || (tt.want == reconciliation.EffectUnknown) != (err != nil) {
			t.Fatalf("ready subvolume evidence %+v is %s (%v), want %s", tt.resource, effect, err, tt.want)
		}
	}
	partial := readyEvidence(r)
	partial.Ready, partial.Partial = false, true
	if effect, err := EvaluateEvidence(evidenceBytes(t, partial), r, "digest", false); err != nil || effect != reconciliation.EffectPartial {
		t.Fatalf("partial subvolume evidence without a resource is %s (%v)", effect, err)
	}
	r.Subvolume.Group = "apps"
	grouped := readyEvidence(r)
	grouped.Resource = ResourceEvidence{ID: subvolumeID, Path: "/volumes/apps/docs/" + subvolumeID}
	if effect, err := EvaluateEvidence(evidenceBytes(t, grouped), r, "digest", false); err != nil || effect != reconciliation.EffectCompleted {
		t.Fatalf("ready evidence in group apps is %s (%v)", effect, err)
	}
	rbd := runtimeRequest()
	rbd.Action, rbd.RBD = "rbd", &RBDImage{Name: "disk", Pool: "volumes", SizeBytes: 1 << 30, Features: []string{"layering"}}
	e := readyEvidence(rbd)
	e.Resource = ResourceEvidence{ID: "abc", Path: exact}
	if effect, err := EvaluateEvidence(evidenceBytes(t, e), rbd, "digest", false); err == nil || effect != reconciliation.EffectUnknown {
		t.Fatalf("rbd evidence with a path is %s (%v)", effect, err)
	}
}

func TestSubvolumeBlockIDAndRequires(t *testing.T) {
	catalog := filesystemCatalog(withMDS, filesystemOf(), subvolumeOf())
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: "synthetic"}, State: compilation.NewState(catalog, catalog, nil), Controller: "controller"}
	plan, err := New(api.StorageSubvolume, nil).Plan(context.Background(), input)
	if err != nil || len(plan.Definitions) != 1 {
		t.Fatalf("the subvolume planned %+v (%v)", plan.Definitions, diagnostics.Of(err))
	}
	block := plan.Definitions[0]
	if block.ID != "ceph-subvolume-docs" || block.Kind != string(api.StorageSubvolume) || block.Object != "docs" ||
		!slices.Equal(block.Requires, []reconciliation.ObjectRef{{Kind: string(api.StorageFilesystem), Object: "files"}}) {
		t.Fatalf("the subvolume block is %+v", block)
	}
	if !slices.Equal(block.Dependencies, []string{ClusterBlockID("example"), ImageBlockID("example")}) || !slices.Equal(block.Exclusive, []string{"ceph/example"}) {
		t.Fatalf("the subvolume block depends on %v, exclusive %v", block.Dependencies, block.Exclusive)
	}
}

func TestSubvolumeInverseBeforeFilesystemBeforePools(t *testing.T) {
	change := func(spec api.Value) api.Value {
		return spec.WithPath(api.ListValue(append(spec.Get("ceph", "services").Items(), mdsRequestService("files"))...), "ceph", "services")
	}
	plan := plannedCephPlanWith(t, change, cephfsPool("fs-meta", "cephfs-metadata"), cephfsPool("fs-data", "cephfs-data"), filesystemOf(), subvolumeOf())
	for _, block := range plan.Blocks {
		if block.ID == "ceph-subvolume-docs" && !slices.Contains(block.Dependencies, "ceph-filesystem-files") {
			t.Fatalf("the planned subvolume block lacks its filesystem: %v", block.Dependencies)
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
	subvolume, okSubvolume := order["ceph-subvolume-docs"]
	filesystem, okFilesystem := order["ceph-filesystem-files"]
	if !okSubvolume || !okFilesystem || subvolume > filesystem {
		t.Fatalf("the subvolume does not precede its filesystem in removal: %v", order)
	}
	for _, pool := range []string{"ceph-pool-fs-meta", "ceph-pool-fs-data"} {
		if _, ok := order[pool]; !ok || filesystem > order[pool] {
			t.Fatalf("the filesystem does not precede pool %s in removal: %v", pool, order)
		}
	}
	capability := New(api.StorageSubvolume, &recordingRunner{})
	e := executionFor(t, frozenSubvolume())
	e.Block.ID, e.Block.Kind, e.Block.Object = "ceph-subvolume-docs", string(api.StorageSubvolume), "docs"
	if q, err := capability.Quiescent(context.Background(), lifecycle.Probe{Block: e.Block, RetiredWithHosts: true}); err != nil || q.State != lifecycle.Quiescent || q.Reason != retiredReason {
		t.Fatalf("a retired subvolume is not quiescent with its hosts: %+v %v", q, err)
	}
	e.Operation, e.RetiredWithHosts, e.ApplyProved = "destroy-later", true, nil
	result, err := capability.Destroy(context.Background(), e)
	var recorded Retirement
	if err != nil || json.Unmarshal(result.Evidence, &recorded) != nil || recorded.Action != "subvolume" || !slices.Equal(recorded.RetiredWith, []string{"node-1", "node-2", "node-3"}) {
		t.Fatalf("the retired subvolume destroy recorded %+v (%v)", recorded, err)
	}
}

func TestSubvolumeExecutionMatchesItsFrozenIdentity(t *testing.T) {
	capability := New(api.StorageSubvolume, nil)
	e := executionFor(t, frozenSubvolume())
	e.Block.ID, e.Block.Kind, e.Block.Object = "ceph-subvolume-docs", string(api.StorageSubvolume), "docs"
	prepared, err := capability.prepare(context.Background(), e)
	if err != nil || prepared.Action != "subvolume" || prepared.Subvolume == nil || prepared.Subvolume.Name != "docs" || prepared.Owner != "apply-original" {
		t.Fatalf("a planned subvolume block did not match its frozen identity: %+v %v", prepared.Subvolume, err)
	}
	e.Block.Object = "files"
	if _, err := capability.prepare(context.Background(), e); len(diagnostics.Of(err)) != 1 || diagnostics.Of(err)[0].Code != "lifecycle.state" {
		t.Fatalf("a subvolume block named after its filesystem matched the subvolume identity: %v", err)
	}
	e.Block.Object, e.Block.Kind = "docs", string(api.StorageFilesystem)
	if _, err := New(api.StorageFilesystem, nil).prepare(context.Background(), e); len(diagnostics.Of(err)) != 1 || diagnostics.Of(err)[0].Code != "lifecycle.state" {
		t.Fatalf("a filesystem capability accepted a frozen subvolume request: %v", err)
	}
}

func TestSubvolumePlanPropagatesFilesystemRefusals(t *testing.T) {
	ec := childOf(api.StoragePool, "fs-data", "type", "erasure", "role", "cephfs-data", "erasure", requestMap("dataChunks", 4, "codingChunks", 2))
	_, err := planned(t, planCatalog(6, 6, withMDS, cephfsPool("fs-meta", "cephfs-metadata"), ec, filesystemOf(), subvolumeOf()), api.StorageSubvolume, "docs")
	refusedWith(t, err, "lifecycle.unsupported", "StorageFilesystem files names erasure-coded pool fs-data as its default data pool; ceph fs new refuses it unless forced, and Bootwright never forces it", "mark a replicated cephfs-data pool default in spec.dataPoolRefs and attach erasure-coded pools as additional data pools")
	_, err = planned(t, filesystemCatalog(withCorePair, filesystemOf(), subvolumeOf()), api.StorageSubvolume, "docs")
	refusedWith(t, err, "api.invariant", "StorageCluster example projects no MDS service mds.files for StorageFilesystem files", "declare the mds service entry with service_id files in spec.ceph.services")
}
