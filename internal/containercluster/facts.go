package containercluster

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/containercluster/openshift"
)

type (
	Tool      = openshift.ControllerTool
	VirtualIP = openshift.VirtualIP
	Endpoint  = openshift.Endpoint
	NodeName  = openshift.NodeName
)

type Facts struct {
	ControllerTools func(cluster api.Object) ([]Tool, *api.Issue)
	VirtualIPs      func(catalog api.Catalog, cluster api.Object, controller string) []VirtualIP
	Endpoints       func(catalog api.Catalog, cluster api.Object) []Endpoint
	NodeNames       func(cluster api.Object) []NodeName
}

var kindFacts = map[api.Kind]Facts{
	api.OpenShiftCluster: {ControllerTools: openshift.ControllerTools, VirtualIPs: openshift.VirtualIPs, Endpoints: openshift.Endpoints, NodeNames: openshift.NodeNames},
}

func FactsOf(kind api.Kind) (Facts, bool) {
	facts, ok := kindFacts[kind]
	return facts, ok
}
