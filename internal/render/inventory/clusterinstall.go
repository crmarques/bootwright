package inventory

import (
	"fmt"

	"github.com/crmarques/bootwright/api/v1alpha1"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

// clusterInstallForOCP resolves the ClusterInstall for a container cluster, or
// an error when no machines or install infrastructure are declared. It keeps
// this state-graph lookup inside inventory and over stateview directly, instead
// of routing it through the installer render family (the inventory -> installer
// edge is for composing installer output, not for state lookups).
func clusterInstallForOCP(state v1alpha1.State, ocp v1alpha1.ContainerCluster) (v1alpha1.ClusterInstall, error) {
	ci, ok := stateview.ClusterInstallForContainerCluster(state, ocp)
	if !ok {
		return v1alpha1.ClusterInstall{}, fmt.Errorf("%s: no machines or install infrastructure declared", ocp.Metadata.Name)
	}
	return ci, nil
}
