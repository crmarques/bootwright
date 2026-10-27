package cephcluster

import (
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func (a *admission) subvolume(cluster api.Object) {
	spec := a.object.Spec()
	a.required(spec, "filesystemRef", "$.spec")
	fs, ok := a.sameOwner(api.CephFilesystem, textOf(spec, "filesystemRef"), "$.spec.filesystemRef", cluster)
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
			a.issue("api.invariant", "$.spec.group", "CephFilesystem "+fs.Name()+" declares no subvolume group "+group, "add "+group+" to spec.subvolumeGroups of CephFilesystem "+fs.Name()+", or omit spec.group to use the native default group")
		}
	}
	if !spec.Has("poolLayoutRef") {
		return
	}
	pool := textOf(spec, "poolLayoutRef")
	a.poolRole(pool, "$.spec.poolLayoutRef", "cephfsData", cluster)
	attached := []string{}
	for _, value := range fs.Spec().Get("dataPoolRefs").Items() {
		attached = append(attached, poolRefName(value))
	}
	if target, found := a.catalog.Find(api.CephPool, pool); found && textOf(target.Spec(), "clusterRef") == cluster.Name() && !slices.Contains(attached, pool) {
		a.issue("api.invariant", "$.spec.poolLayoutRef", "CephPool "+pool+" is not a data pool of CephFilesystem "+fs.Name()+"; a subvolume layout must name an attached data pool", "name one of CephFilesystem "+fs.Name()+"'s spec.dataPoolRefs in spec.poolLayoutRef, or omit it to inherit the group's layout")
	}
}
