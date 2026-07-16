package installer

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/nmstate"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

func ContainerNodePrimaryAddress(state v1alpha1.State, cluster v1alpha1.ContainerCluster, machineName string) string {
	install, ok := stateview.ClusterInstallForContainerCluster(state, cluster)
	if !ok {
		return ""
	}
	machine, ok := stateview.InstallMachine(install, machineName)
	if !ok {
		return ""
	}
	return nmstate.NetworkConfigPrimaryIP(AgentNetworkConfig(state, install, machine, cluster.Metadata.Name))
}
