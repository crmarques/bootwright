package inventory

import (
	"fmt"

	"github.com/crmarques/bootwright/api/v1alpha1"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

func clusterInstallForOCP(state v1alpha1.State, ocp v1alpha1.ContainerCluster) (v1alpha1.ClusterInstall, error) {
	ci, ok := stateview.ClusterInstallForContainerCluster(state, ocp)
	if !ok {
		return v1alpha1.ClusterInstall{}, fmt.Errorf("%s: no machines or install infrastructure declared", ocp.Metadata.Name)
	}
	return ci, nil
}
