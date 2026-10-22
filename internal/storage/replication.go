package storage

import (
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func ReplicationOwnsPool(c api.Catalog, name string) bool {
	for _, r := range c.OfKind(api.StorageReplication) {
		if r.Spec().Get("poolRef").Text() == name || r.Spec().Get("peerPoolRef").Text() == name {
			return true
		}
	}
	return false
}

func ReplicationImages(c api.Catalog, r api.Object) []api.Object {
	refs := r.Spec().Get("imageRefs").Strings()
	out := []api.Object{}
	for _, image := range c.OfKind(api.StorageRBDImage) {
		if image.Spec().Get("poolRef").Text() == r.Spec().Get("poolRef").Text() && (len(refs) == 0 || slices.Contains(refs, image.Name())) {
			out = append(out, image)
		}
	}
	slices.SortFunc(out, func(a, b api.Object) int {
		if a.Name() < b.Name() {
			return -1
		}
		if a.Name() > b.Name() {
			return 1
		}
		return 0
	})
	return out
}

func (a *admission) storageReplication(primary api.Object) {
	spec := a.object.Spec()
	peer, found := a.catalog.Find(api.StorageCluster, spec.Get("peerClusterRef").Text())
	if !found || peer.Name() == primary.Name() || fallback(peer.Spec().Get("management"), "managed") != "managed" {
		a.invalid("$.spec.peerClusterRef", "replication requires a distinct managed StorageCluster in the same context")
		return
	}
	for _, node := range primary.Spec().Get("ceph", "topology", "nodes").Items() {
		for _, other := range peer.Spec().Get("ceph", "topology", "nodes").Items() {
			if node.Get("machineRef").Text() == other.Get("machineRef").Text() {
				a.invalid("$.spec.peerClusterRef", "the two clusters must use disjoint Machines")
			}
		}
	}
	left, lok := a.sameOwner(api.StoragePool, spec.Get("poolRef").Text(), "$.spec.poolRef", primary)
	right, rok := a.sameOwner(api.StoragePool, spec.Get("peerPoolRef").Text(), "$.spec.peerPoolRef", peer)
	if !lok || !rok {
		return
	}
	mode := left.Spec().Get("mirroring", "mode").Text()
	if mode != "image" && mode != "pool" || right.Spec().Get("mirroring", "mode").Text() != mode {
		a.invalid("$.spec.poolRef", "both pools must explicitly select the same mirroring.mode, image or pool")
	}
	if PoolName(left) != PoolName(right) {
		a.invalid("$.spec.peerPoolRef", "both pools must have the same nativeName; metadata.name remains distinct")
	}
	for _, p := range []api.Object{left, right} {
		if p.Spec().Get("role").Text() != "rbd" || fallback(p.Spec().Get("type"), "replicated") != "replicated" {
			a.invalid("$.spec.poolRef", "mirroring endpoints require replicated pools with role rbd")
		}
	}
	if _, ok := NativeService(peer, "rbd-mirror"); !ok {
		a.invalid("$.spec.peerClusterRef", "the receiving cluster must declare its rbd-mirror service")
	}
	for _, other := range a.catalog.OfKind(api.StorageReplication) {
		if other.Name() == a.object.Name() {
			continue
		}
		for _, cluster := range []string{primary.Name(), peer.Name()} {
			if other.Spec().Get("clusterRef").Text() == cluster || other.Spec().Get("peerClusterRef").Text() == cluster {
				a.invalid("$.spec.clusterRef", "a managed cluster participates in at most one replication relationship")
			}
		}
	}
	refs := spec.Get("imageRefs").Strings()
	if mode == "image" && len(refs) == 0 || mode == "pool" && spec.Has("imageRefs") {
		a.invalid("$.spec.imageRefs", "image mode requires explicit imageRefs; pool mode derives every declared source image and forbids imageRefs")
	}
	for _, ref := range refs {
		image, ok := a.sameOwner(api.StorageRBDImage, ref, "$.spec.imageRefs", primary)
		if ok && image.Spec().Get("poolRef").Text() != left.Name() {
			a.invalid("$.spec.imageRefs", "every image must belong to poolRef")
		}
	}
	images := ReplicationImages(a.catalog, a.object)
	if len(images) == 0 || len(images) > 3 {
		a.invalid("$.spec.imageRefs", "a replication relationship requires one to three source images")
	}
	for _, image := range images {
		if !slices.Contains(RBDImageFeatures(image.Spec()), "journaling") || image.Spec().Has("dataPoolRef") {
			a.invalid("$.spec.imageRefs", "replicated images require journaling and no separate dataPoolRef")
		}
	}
	for _, image := range a.catalog.OfKind(api.StorageRBDImage) {
		if image.Spec().Get("poolRef").Text() == right.Name() {
			a.invalid("$.spec.peerPoolRef", "the receiving pool must contain no independently declared images")
		}
	}
}
