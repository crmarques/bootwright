package render

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render/installer"
)

func ContainerNodePrimaryAddress(state v1alpha1.State, cluster v1alpha1.ContainerCluster, machineName string) string {
	return installer.ContainerNodePrimaryAddress(state, cluster, machineName)
}
