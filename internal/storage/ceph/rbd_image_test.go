package ceph

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

var defaultFeatures = []string{"layering", "exclusive-lock", "object-map", "fast-diff", "deep-flatten"}

func ecData() api.Object {
	return erasurePool("ec-data", requestMap("dataChunks", 4, "codingChunks", 2))
}

func imageOf(name, pool string, pairs ...any) api.Object {
	return childOf(api.StorageRBDImage, name, append([]any{"poolRef", pool, "sizeBytes", 10737418240}, pairs...)...)
}

func plannedImage(t *testing.T, children ...api.Object) Request {
	t.Helper()
	r, err := planned(t, planCatalog(6, 6, nil, children...), api.StorageRBDImage, "disk")
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	return r
}

func TestRBDImageDefaultFeaturesFrozen(t *testing.T) {
	r := plannedImage(t, replicatedPool("volumes"), imageOf("disk", "volumes"))
	if r.Action != "rbd" || r.Pool != nil || r.RBD == nil || !slices.Equal(r.RBD.Features, defaultFeatures) || r.RBD.DataPool != "" || r.RBD.SizeBytes != 10737418240 || r.RBD.Name != "disk" || r.RBD.Pool != "volumes" {
		t.Fatalf("an image without features planned as %+v %+v", r, r.RBD)
	}
	r = plannedImage(t, replicatedPool("volumes"), imageOf("disk", "volumes", "features", api.StringList("deep-flatten", "layering")))
	if !slices.Equal(r.RBD.Features, []string{"layering", "deep-flatten"}) {
		t.Fatalf("declared features froze as %v", r.RBD.Features)
	}
}

func TestRBDImageExplicitEmptyFeaturesFreezeEmpty(t *testing.T) {
	empty := plannedImage(t, replicatedPool("volumes"), imageOf("disk", "volumes", "features", api.StringList()))
	if empty.RBD.Features == nil || len(empty.RBD.Features) != 0 {
		t.Fatalf("explicit empty features froze as %#v", empty.RBD.Features)
	}
	data, err := empty.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	omitted, err := plannedImage(t, replicatedPool("volumes"), imageOf("disk", "volumes")).Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"features":[]`)) || bytes.Equal(data, omitted) {
		t.Fatalf("explicit empty features did not freeze apart from the omission: %s", data)
	}
	decoded, err := DecodeRequest(data)
	if err != nil || decoded.RBD.Features == nil || len(decoded.RBD.Features) != 0 {
		t.Fatalf("explicit empty features did not round-trip: %#v %v", decoded.RBD, err)
	}
}

func TestClientOnPoolWithDataPoolImagePlans(t *testing.T) {
	client := childOf(api.StorageClient, "consumer", "poolRef", "volumes", "access", "read-write")
	plannedClient := func(catalog api.Catalog) []string {
		t.Helper()
		r, err := planned(t, catalog, api.StorageClient, "consumer")
		if err != nil || r.Client == nil {
			t.Fatalf("the client did not plan: %v", diagnostics.Of(err))
		}
		return r.Client.DataPools
	}
	for _, data := range []api.Object{ecData(), replicatedPool("archive")} {
		catalog := planCatalog(6, 6, nil, replicatedPool("volumes"), data, client, imageOf("disk", "volumes", "dataPoolRef", data.Name()))
		if got := plannedClient(catalog); !slices.Equal(got, []string{data.Name()}) {
			t.Fatalf("a client beside an image on data pool %s froze %v", data.Name(), got)
		}
	}
	for _, image := range []api.Object{imageOf("disk", "volumes"), imageOf("disk", "other", "dataPoolRef", "archive")} {
		catalog := planCatalog(6, 6, nil, replicatedPool("volumes"), replicatedPool("other"), replicatedPool("archive"), client, image)
		if got := plannedClient(catalog); got == nil || len(got) != 0 {
			t.Fatalf("a client beside image %s froze data pools %#v", image.Spec().Get("poolRef").Text(), got)
		}
	}
	catalog := planCatalog(6, 6, nil, replicatedPool("volumes"), replicatedPool("archive"), ecData(), client,
		imageOf("zeta", "volumes", "dataPoolRef", "ec-data"), imageOf("disk", "volumes", "dataPoolRef", "archive"))
	if got := plannedClient(catalog); !slices.Equal(got, []string{"archive", "ec-data"}) {
		t.Fatalf("a client beside two data-pool images froze %v", got)
	}
}

func TestRBDImageOnErasurePoolRefusesAtPlan(t *testing.T) {
	_, err := planned(t, planCatalog(6, 6, nil, ecData(), imageOf("disk", "ec-data")), api.StorageRBDImage, "disk")
	refusedWith(t, err, "lifecycle.unsupported", "StorageRBDImage disk names erasure-coded pool ec-data; an RBD image keeps its header and metadata in a replicated pool", "point spec.poolRef at a replicated rbd pool and name the erasure-coded pool in spec.dataPoolRef")
}

func TestRBDImagePoolRefusalsPropagate(t *testing.T) {
	catalog := planCatalog(6, 6, nil, replicatedPool("volumes"), replicatedPool("archive", "mirroring", requestMap("mode", "image")), imageOf("disk", "volumes", "dataPoolRef", "archive"))
	_, err := planned(t, catalog, api.StorageRBDImage, "disk")
	refusedWith(t, err, "lifecycle.unsupported", "StoragePool archive declares mirroring, which B483 delivers", "remove spec.mirroring; B483 delivers mirroring")
	r := plannedImage(t, replicatedPool("volumes"), ecData(), imageOf("disk", "volumes", "dataPoolRef", "ec-data"))
	if r.RBD.DataPool != "ec-data" {
		t.Fatalf("an erasure-coded data pool froze as %q", r.RBD.DataPool)
	}
}

func TestRBDImageBlockIDAndRequires(t *testing.T) {
	catalog := planCatalog(6, 6, nil, replicatedPool("volumes"), ecData(), imageOf("disk", "volumes", "dataPoolRef", "ec-data"))
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: "synthetic"}, State: compilation.NewState(catalog, catalog, nil), Controller: "controller"}
	plan, err := New(api.StorageRBDImage, nil).Plan(context.Background(), input)
	if err != nil || len(plan.Definitions) != 1 {
		t.Fatalf("the image planned %+v (%v)", plan.Definitions, diagnostics.Of(err))
	}
	block := plan.Definitions[0]
	requires := []reconciliation.ObjectRef{{Kind: string(api.StoragePool), Object: "volumes"}, {Kind: string(api.StoragePool), Object: "ec-data"}}
	if block.ID != "ceph-rbd-disk" || block.Kind != string(api.StorageRBDImage) || block.Object != "disk" || !slices.Equal(block.Requires, requires) {
		t.Fatalf("the image block is %+v", block)
	}
	if !slices.Equal(block.Dependencies, []string{ClusterBlockID("example"), ImageBlockID("example")}) {
		t.Fatalf("the image block depends on %v", block.Dependencies)
	}
	blocks := map[string]reconciliation.Block{}
	for _, b := range plannedCephPlan(t, replicatedPool("images"), replicatedPool("archive"), imageOf("disk", "images", "dataPoolRef", "archive")).Blocks {
		blocks[b.ID] = b
	}
	for _, dependency := range []string{"ceph-cluster-example", "ceph-image-example", "ceph-pool-images", "ceph-pool-archive"} {
		if !slices.Contains(blocks["ceph-rbd-disk"].Dependencies, dependency) {
			t.Fatalf("the planned image block lacks dependency %s: %v", dependency, blocks["ceph-rbd-disk"].Dependencies)
		}
	}
}

func rbdRequest() Request {
	r := coreRequest()
	r.Action, r.RBD = "rbd", &RBDImage{Name: "disk", Pool: "volumes", SizeBytes: 10737418240, Features: defaultFeatures}
	return r
}

func TestRBDImageExecutionMatchesItsFrozenIdentity(t *testing.T) {
	capability := New(api.StorageRBDImage, nil)
	e := executionFor(t, rbdRequest())
	e.Block.ID, e.Block.Kind, e.Block.Object = "ceph-rbd-disk", string(api.StorageRBDImage), "disk"
	prepared, err := capability.prepare(context.Background(), e)
	if err != nil || prepared.Action != "rbd" || prepared.RBD == nil || prepared.RBD.Name != "disk" || prepared.Owner != "apply-original" {
		t.Fatalf("a planned image block did not match its frozen identity: %+v %v", prepared.RBD, err)
	}
	e.Block.Object = "volumes"
	if _, err := capability.prepare(context.Background(), e); len(diagnostics.Of(err)) != 1 || diagnostics.Of(err)[0].Code != "lifecycle.state" {
		t.Fatalf("an image block named after its pool matched the image identity: %v", err)
	}
}

func TestRBDImageInverseOrdersImageBeforePool(t *testing.T) {
	inverse, err := plannedCephPlan(t, replicatedPool("images"), replicatedPool("archive"), imageOf("disk", "images", "dataPoolRef", "archive")).Inverse()
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	order := map[string]int{}
	for i, block := range inverse.Blocks {
		order[block.ID] = i
		if (block.ID == "ceph-pool-images" || block.ID == "ceph-pool-archive") && !slices.Contains(block.Dependencies, "ceph-rbd-disk") {
			t.Fatalf("removing pool %s does not wait for the image: %v", block.ID, block.Dependencies)
		}
	}
	if order["ceph-rbd-disk"] > order["ceph-pool-images"] || order["ceph-rbd-disk"] > order["ceph-pool-archive"] {
		t.Fatalf("the image does not precede its pools in removal: %v", order)
	}
	capability := New(api.StorageRBDImage, &recordingRunner{})
	e := executionFor(t, rbdRequest())
	e.Block.ID, e.Block.Kind, e.Block.Object = "ceph-rbd-disk", string(api.StorageRBDImage), "disk"
	if q, err := capability.Quiescent(context.Background(), lifecycle.Probe{Block: e.Block, RetiredWithHosts: true}); err != nil || q.State != lifecycle.Quiescent || q.Reason != retiredReason {
		t.Fatalf("a retired image is not quiescent with its hosts: %+v %v", q, err)
	}
	e.Operation, e.RetiredWithHosts, e.ApplyProved = "destroy-later", true, nil
	result, err := capability.Destroy(context.Background(), e)
	var recorded Retirement
	if err != nil || json.Unmarshal(result.Evidence, &recorded) != nil || recorded.Action != "rbd" || !slices.Equal(recorded.RetiredWith, []string{"node-1", "node-2", "node-3"}) {
		t.Fatalf("the retired image destroy recorded %+v (%v)", recorded, err)
	}
}

func TestEvidenceRBDImageIDIsLowercaseHex(t *testing.T) {
	r := runtimeRequest()
	r.Action, r.RBD = "rbd", &RBDImage{Name: "disk", Pool: "volumes", SizeBytes: 1 << 30, Features: []string{}}
	for id, want := range map[string]reconciliation.EffectState{"10226b8b4567": reconciliation.EffectCompleted, "": reconciliation.EffectUnknown, "10226B8B4567": reconciliation.EffectUnknown,
		"0123456789abcde": reconciliation.EffectUnknown, "12-34": reconciliation.EffectUnknown} {
		e := readyEvidence(r)
		e.Resource = ResourceEvidence{ID: id}
		effect, err := EvaluateEvidence(evidenceBytes(t, e), r, "digest", false)
		if effect != want || (want == reconciliation.EffectUnknown) != (err != nil) {
			t.Fatalf("ready image evidence with id %q is %s (%v), want %s", id, effect, err, want)
		}
	}
	partial := readyEvidence(r)
	partial.Ready, partial.Partial = false, true
	if effect, err := EvaluateEvidence(evidenceBytes(t, partial), r, "digest", false); err != nil || effect != reconciliation.EffectPartial {
		t.Fatalf("partial image evidence without an id is %s (%v)", effect, err)
	}
	pool := resourceRequest("pool")
	e := readyEvidence(pool)
	e.Resource = ResourceEvidence{ID: "abc"}
	if effect, err := EvaluateEvidence(evidenceBytes(t, e), pool, "digest", false); err == nil || effect != reconciliation.EffectUnknown {
		t.Fatalf("a pool accepted a hex id: %s %v", effect, err)
	}
}
