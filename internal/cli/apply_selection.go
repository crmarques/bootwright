package cli

import (
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/clusteraccess"
	"github.com/crmarques/bootwright/internal/converge"
)

func validateKubeVirtClusterSelection(state v1alpha1.State, scope converge.Scope, clusters string, clustersDir string) error {
	if strings.TrimSpace(clusters) == "" {
		return nil
	}
	if !converge.ScopeProvisionsClusterWorkload(scope) {
		return nil
	}
	containerNames, _, err := clusteraccess.ClusterRootNamesForTarget(state, clusters)
	if err != nil {
		return err
	}
	return converge.ValidateKubeVirtClusterSelection(state, containerNames, clustersDir)
}
