package proxy

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/stateview"
)

func ClusterFacingHostAddress(state v1alpha1.State, hostName string, ci v1alpha1.ClusterInfra) string {
	return stateview.ClusterFacingHostAddress(state, hostName, ci)
}

func HostRouteAddress(state v1alpha1.State, hostName, addressName string, ci v1alpha1.ClusterInfra) string {
	return stateview.HostRouteAddress(state, hostName, addressName, ci)
}
