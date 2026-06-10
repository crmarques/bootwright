package cli

import (
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/clusteraccess"
	"github.com/crmarques/bootwright/internal/converge"
)

// validateKubeVirtClusterSelection resolves the --clusters selection to
// container cluster root names (cluster selection is a CLI concern) and
// hands the host-cluster readiness gate to converge.
func validateKubeVirtClusterSelection(state v1alpha1.State, scope converge.Scope, clusters string, clustersDir string) error {
	if strings.TrimSpace(clusters) == "" {
		return nil
	}
	switch scope.Name {
	case "infra", "clusters", "all", "machines", "deps", "base":
	default:
		return nil
	}
	containerNames, _, err := clusteraccess.ClusterRootNamesForTarget(state, clusters)
	if err != nil {
		return err
	}
	return converge.ValidateKubeVirtClusterSelection(state, containerNames, clustersDir)
}
