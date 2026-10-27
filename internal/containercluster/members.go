package containercluster

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/environment"
)

func MemberMachines(object api.Object) (environment.Membership, bool) {
	if object.Kind() != api.OpenShiftCluster {
		return environment.Membership{}, false
	}
	return environment.MembersOf("$.spec.nodes", object.Spec().Get("nodes")), true
}
