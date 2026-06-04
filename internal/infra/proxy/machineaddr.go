package proxy

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/state/view"
)

func ClusterFacingMachineAddress(state v1alpha1.State, machineName string, ci v1alpha1.ClusterInstall) string {
	return stateview.MachineRouteAddress(state, machineName, "", ci)
}

func MachineRouteAddress(state v1alpha1.State, machineName, addressName string, ci v1alpha1.ClusterInstall) string {
	return stateview.MachineRouteAddress(state, machineName, addressName, ci)
}
