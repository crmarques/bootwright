package cephcluster

import (
	"math/big"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

var rbdFeatureOrder = []string{"layering", "exclusive-lock", "object-map", "fast-diff", "deep-flatten", "journaling"}

var defaultRBDFeatures = []string{"layering", "exclusive-lock", "object-map", "fast-diff", "deep-flatten"}

const RBDObjectMapMaxBytes = int64(256000000) << 22

func RBDImageFeatures(spec api.Value) []string {
	if !spec.Has("features") {
		return slices.Clone(defaultRBDFeatures)
	}
	declared := spec.Get("features").Strings()
	features := []string{}
	for _, feature := range rbdFeatureOrder {
		if slices.Contains(declared, feature) {
			features = append(features, feature)
		}
	}
	return features
}

type rbdImageRule struct {
	broken                      bool
	message, remediation, field string
}

func (a *admission) rbdImageIntrinsic() {
	spec := a.object.Spec()
	features := spec.Get("features").Strings()
	if !spec.Has("features") && !a.partial {
		features = RBDImageFeatures(spec)
	}
	has := func(feature string) bool { return slices.Contains(features, feature) }
	for _, r := range []rbdImageRule{
		{has("fast-diff") && !has("object-map"), "feature fast-diff requires object-map, which librbd refuses to omit", "add object-map to spec.features or remove fast-diff", "$.spec.features"},
		{has("object-map") && !has("exclusive-lock"), "feature object-map requires exclusive-lock, which librbd refuses to omit", "add exclusive-lock to spec.features or remove object-map", "$.spec.features"},
		{has("journaling") && !has("exclusive-lock"), "feature journaling requires exclusive-lock, which librbd refuses to omit", "add exclusive-lock to spec.features or remove journaling", "$.spec.features"},
		{has("object-map") && !has("fast-diff"), "feature object-map without fast-diff is not admitted: librbd enables fast-diff with object-map, so the created image would differ from the declared set", "add fast-diff to spec.features or remove object-map", "$.spec.features"},
		{spec.Has("dataPoolRef") && textOf(spec, "dataPoolRef") == textOf(spec, "poolRef"), "the data pool is the image pool; omitting dataPoolRef already keeps the image's data there", "remove spec.dataPoolRef or name another rbd pool", "$.spec.dataPoolRef"},
		{has("object-map") && numeric(spec.Get("sizeBytes")).Cmp(big.NewInt(RBDObjectMapMaxBytes)) > 0, "an image with object-map in 4 MiB objects addresses at most 1073741824000000 bytes (256000000 objects)", "lower spec.sizeBytes to at most 1073741824000000, or declare features without object-map and fast-diff", "$.spec.sizeBytes"},
	} {
		if r.broken {
			a.issue("api.invariant", r.field, r.message, r.remediation)
		}
	}
}

func (a *admission) rbdImage(cluster api.Object) {
	spec := a.object.Spec()
	a.required(spec, "poolRef", "$.spec")
	a.required(spec, "sizeBytes", "$.spec")
	name := textOf(spec, "poolRef")
	a.poolRole(name, "$.spec.poolRef", "rbd", cluster)
	if pool, ok := a.catalog.Find(api.CephPool, name); ok && textOf(pool.Spec(), "clusterRef") == cluster.Name() && PoolProtection(pool.Spec()) == "erasure" {
		a.issue("api.invariant", "$.spec.poolRef", "CephPool "+name+" is erasure-coded; an RBD image keeps its header and metadata in a replicated pool", "point spec.poolRef at a replicated rbd pool and name the erasure-coded pool in spec.dataPoolRef")
	}
	if spec.Has("dataPoolRef") {
		a.poolRole(textOf(spec, "dataPoolRef"), "$.spec.dataPoolRef", "rbd", cluster)
	}
}
