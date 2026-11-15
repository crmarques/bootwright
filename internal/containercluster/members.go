package containercluster

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/environment"
)

func MemberMachines(object api.Object) (environment.Membership, bool) {
	if object.Kind() != api.OpenShiftCluster {
		return environment.Membership{}, false
	}
	field, nodes, _ := api.ClusterNodes(object)
	return environment.MembersOf(field, nodes), true
}

func SessionNode(object api.Object) string {
	membership, ok := MemberMachines(object)
	if !ok {
		return ""
	}
	var first string
	for _, node := range membership.Nodes {
		if first == "" || node.Name < first {
			first = node.Name
		}
	}
	return first
}
