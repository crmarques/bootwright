package render

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/state/view"
)

var standardEndpointNames = []string{
	v1alpha1.EndpointAPI,
	v1alpha1.EndpointAPIInt,
	v1alpha1.EndpointIngress,
}

func endpointAddress(state v1alpha1.State, ci v1alpha1.ClusterInfra, name string) string {
	return stateview.EndpointAddress(state, ci, name)
}

func endpointNetworkConfig(state v1alpha1.State, ci v1alpha1.ClusterInfra, address string) (v1alpha1.NetworkConfig, bool) {
	return stateview.EndpointNetworkConfig(state, ci, address)
}
