package access

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/environment"
)

func storageMembers(object api.Object) (environment.Membership, bool) {
	if object.Kind() != api.StorageCluster {
		return environment.Membership{}, false
	}
	return environment.MembersOf("$.spec.ceph.topology.nodes", object.Spec().Get("ceph", "topology", "nodes")), true
}
