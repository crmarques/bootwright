package storage

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/environment"
)

func MemberMachines(object api.Object) (environment.Membership, bool) {
	if object.Kind() != api.CephCluster {
		return environment.Membership{}, false
	}
	return environment.MembersOf("$.spec.topology.nodes", object.Spec().Get("topology", "nodes")), true
}
