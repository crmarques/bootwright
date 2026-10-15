package storage

import (
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

const subvolumeNameMax = 48

func (a *admission) subvolume(cluster api.Object) {
	spec := a.object.Spec()
	a.required(spec, "filesystemRef", "$.spec")
	if len(a.object.Name()) > subvolumeNameMax {
		a.issue("api.invariant", "$.metadata.name", "a subvolume name longer than 48 characters does not fit its block identity ceph-subvolume-<name> of at most 63 characters", "shorten metadata.name to at most 48 characters")
	}
	fs, ok := a.sameOwner(api.StorageFilesystem, textOf(spec, "filesystemRef"), "$.spec.filesystemRef", cluster)
	if !ok {
		return
	}
	if spec.Has("group") {
		group := textOf(spec, "group")
		declared := false
		for _, entry := range fs.Spec().Get("subvolumeGroups").Items() {
			declared = declared || textOf(entry, "name") == group
		}
		if !declared {
			a.issue("api.invariant", "$.spec.group", "StorageFilesystem "+fs.Name()+" declares no subvolume group "+group, "add "+group+" to spec.subvolumeGroups of StorageFilesystem "+fs.Name()+", or omit spec.group to use the native default group")
		}
	}
	if !spec.Has("poolLayoutRef") {
		return
	}
	pool := textOf(spec, "poolLayoutRef")
	a.poolRole(pool, "$.spec.poolLayoutRef", "cephfs-data", cluster)
	attached := []string{}
	for _, value := range fs.Spec().Get("dataPoolRefs").Items() {
		attached = append(attached, poolRefName(value))
	}
	if target, found := a.catalog.Find(api.StoragePool, pool); found && textOf(target.Spec(), "clusterRef") == cluster.Name() && !slices.Contains(attached, pool) {
		a.issue("api.invariant", "$.spec.poolLayoutRef", "StoragePool "+pool+" is not a data pool of StorageFilesystem "+fs.Name()+"; a subvolume layout must name an attached data pool", "name one of StorageFilesystem "+fs.Name()+"'s spec.dataPoolRefs in spec.poolLayoutRef, or omit it to inherit the group's layout")
	}
}
