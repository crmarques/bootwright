package cephcluster

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func rbdImage(name string, pairs ...any) api.Object {
	spec := m("clusterRef", "storage", "poolRef", "block", "sizeBytes", 1073741824)
	for i := 0; i < len(pairs); i += 2 {
		spec = spec.With(pairs[i].(string), v(pairs[i+1]))
	}
	return object(api.CephRBDImage, name, spec)
}

func imageCatalog(extra ...api.Object) api.Catalog {
	catalog := fixtureCatalog()
	for _, object := range extra {
		catalog = replace(catalog, object)
	}
	return catalog
}

func ecBlock(name string) api.Object { return ecPool(name, chunks(4, 2)) }

func replicatedBlock(name string) api.Object {
	return object(api.CephPool, name, m("clusterRef", "storage", "role", "rbd"))
}

func features(names ...string) api.Value { return api.StringList(names...) }

func TestRBDImagePoolMustBeReplicatedRBD(t *testing.T) {
	catalog := imageCatalog(ecBlock("ec-block"))
	noIssues(t, admitted(catalog, rbdImage("disk")))
	onlyIssue(t, admitted(catalog, rbdImage("disk", "poolRef", "data")), "$.spec.poolRef", "referenced pool does not have the consumer's required role", "set $.spec.poolRef to a CephPool with spec.role rbd")
	onlyIssue(t, admitted(catalog, rbdImage("disk", "poolRef", "ec-block")), "$.spec.poolRef", "CephPool ec-block is erasure-coded; an RBD image keeps its header and metadata in a replicated pool", "point spec.poolRef at a replicated rbd pool and name the erasure-coded pool in spec.dataPoolRef")
}

func TestRBDImageDataPoolMustDifferAndBeRBD(t *testing.T) {
	catalog := imageCatalog(ecBlock("ec-block"), replicatedBlock("other-block"))
	onlyIssue(t, admitted(catalog, rbdImage("disk", "dataPoolRef", "block")), "$.spec.dataPoolRef", "the data pool is the image pool; omitting dataPoolRef already keeps the image's data there", "remove spec.dataPoolRef or name another rbd pool")
	onlyIssue(t, admitted(catalog, rbdImage("disk", "dataPoolRef", "data")), "$.spec.dataPoolRef", "referenced pool does not have the consumer's required role", "set $.spec.dataPoolRef to a CephPool with spec.role rbd")
	noIssues(t, admitted(catalog, rbdImage("disk", "dataPoolRef", "ec-block")))
	noIssues(t, admitted(catalog, rbdImage("disk", "dataPoolRef", "other-block")))
}

func TestRBDImageFeatureDependencies(t *testing.T) {
	for _, tc := range []struct {
		features []string
		message  string
	}{
		{[]string{"fast-diff"}, "feature fast-diff requires object-map, which librbd refuses to omit"},
		{[]string{"object-map", "fast-diff"}, "feature object-map requires exclusive-lock, which librbd refuses to omit"},
		{[]string{"journaling"}, "feature journaling requires exclusive-lock, which librbd refuses to omit"},
		{[]string{"exclusive-lock", "journaling"}, ""},
		{[]string{"layering", "exclusive-lock", "object-map", "fast-diff", "deep-flatten"}, ""},
		{[]string{}, ""},
		{[]string{"layering"}, ""},
	} {
		issues := admitted(fixtureCatalog(), rbdImage("disk", "features", features(tc.features...)))
		if tc.message == "" {
			noIssues(t, issues)
			continue
		}
		if len(issues) != 1 || issues[0].Code != "api.invariant" || issues[0].Field != "$.spec.features" || issues[0].Message != tc.message {
			t.Fatalf("features %v: issues %+v, want %q", tc.features, issues, tc.message)
		}
	}
}

func TestRBDImageObjectMapWithoutFastDiffRefuses(t *testing.T) {
	onlyIssue(t, admitted(fixtureCatalog(), rbdImage("disk", "features", features("exclusive-lock", "object-map"))), "$.spec.features", "feature object-map without fast-diff is not admitted: librbd enables fast-diff with object-map, so the created image would differ from the declared set", "add fast-diff to spec.features or remove object-map")
}

func TestRBDImageObjectMapSizeBound(t *testing.T) {
	sized := func(size string, pairs ...any) api.Object {
		return rbdImage("disk", append([]any{"sizeBytes", api.IntegerValue(size)}, pairs...)...)
	}
	onlyIssue(t, admitted(fixtureCatalog(), sized("1073741824000001")), "$.spec.sizeBytes", "an image with object-map in 4 MiB objects addresses at most 1073741824000000 bytes (256000000 objects)", "lower spec.sizeBytes to at most 1073741824000000, or declare features without object-map and fast-diff")
	noIssues(t, admitted(fixtureCatalog(), sized("1073741824000000")))
	noIssues(t, admitted(fixtureCatalog(), sized("9223372036854775807", "features", features("layering"))))
}

func TestRBDImageNameHasNoBlockDerivedLimit(t *testing.T) {
	noIssues(t, admitted(fixtureCatalog(), rbdImage(strings.Repeat("a", 63))))
}

func TestRBDImageWrongClusterReferenceRefuses(t *testing.T) {
	storage := find(t, fixtureCatalog(), api.CephCluster, "storage")
	other := api.NewObject(api.CephCluster, "other", api.MapValue(), storage.Spec())
	catalog := imageCatalog(other, object(api.CephPool, "other-block", m("clusterRef", "other", "role", "rbd")))
	message := "referenced storage child belongs to a different cluster"
	onlyIssue(t, admitted(catalog, rbdImage("disk", "poolRef", "other-block")), "$.spec.poolRef", message, "select a referenced resource whose spec.clusterRef matches this resource")
	onlyIssue(t, admitted(catalog, rbdImage("disk", "dataPoolRef", "other-block")), "$.spec.dataPoolRef", message, "select a referenced resource whose spec.clusterRef matches this resource")
}

func TestRBDImagePartialChecksOnlyIntrinsicRules(t *testing.T) {
	partial := func(pairs ...any) []api.Issue {
		return ValidatePartial(object(api.CephRBDImage, "partial", m(pairs...)), api.Catalog{})
	}
	noIssues(t, partial())
	onlyIssue(t, partial("features", features("fast-diff")), "$.spec.features", "feature fast-diff requires object-map, which librbd refuses to omit", "add object-map to spec.features or remove fast-diff")
	onlyIssue(t, partial("poolRef", "block", "dataPoolRef", "block"), "$.spec.dataPoolRef", "the data pool is the image pool; omitting dataPoolRef already keeps the image's data there", "remove spec.dataPoolRef or name another rbd pool")
	noIssues(t, partial("sizeBytes", api.IntegerValue("1073741824000001")))
	requireIssue(t, partial("sizeBytes", api.IntegerValue("1073741824000001"), "features", features("exclusive-lock", "object-map", "fast-diff")), "$.spec.sizeBytes")
}

func TestRBDImageFeaturesFreezeInBitOrder(t *testing.T) {
	if got := RBDImageFeatures(m()); strings.Join(got, ",") != "layering,exclusive-lock,object-map,fast-diff,deep-flatten" {
		t.Fatalf("omitted features froze %v", got)
	}
	if got := RBDImageFeatures(m("features", features("deep-flatten", "layering"))); strings.Join(got, ",") != "layering,deep-flatten" {
		t.Fatalf("declared features froze %v", got)
	}
	if got := RBDImageFeatures(m("features", features())); got == nil || len(got) != 0 {
		t.Fatalf("explicit empty features froze %#v", got)
	}
	defaults := RBDImageFeatures(m())
	defaults[0] = "mutated"
	if RBDImageFeatures(m())[0] != "layering" {
		t.Fatal("the default feature set is shared, not copied")
	}
}
