package clusteraccess

import (
	"fmt"
	"os"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
)

func Kubeconfig(state v1alpha1.State, clustersDir, clusterName string) ([]byte, error) {
	summaries := FilterClusterSummaries(
		ClusterSummariesFromAssets(state, render.InstallerAssets(clustersDir, state)),
		clusterName,
	)
	if len(summaries) == 0 {
		return nil, fmt.Errorf("%q is not an installed container cluster", clusterName)
	}
	summary := summaries[0]
	if !summary.Kubeconfig.Present {
		return nil, fmt.Errorf("kubeconfig for %q not found at %s; install the cluster first", clusterName, summary.KubeconfigPath)
	}
	data, err := os.ReadFile(summary.KubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("read kubeconfig for %s: %w", clusterName, err)
	}
	return data, nil
}
