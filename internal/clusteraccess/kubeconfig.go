package clusteraccess

import (
	"fmt"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/secrets"
)

func Kubeconfig(state v1alpha1.State, contextName, clustersDir, clusterName string) ([]byte, error) {
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
	store := secret.NewContextStore(contextName, workflow.ClusterSecretsDir(clustersDir, clusterName))
	if _, err := store.MigratePlaintext(func(string) secret.MaterialRole { return secret.MaterialPrimary }); err != nil {
		return nil, fmt.Errorf("encrypt kubeconfig for %s: %w", clusterName, err)
	}
	data, err := store.Read(secret.MaterialKey{Name: "kubeconfig", Role: secret.MaterialPrimary})
	if err != nil {
		return nil, fmt.Errorf("read kubeconfig for %s: %w", clusterName, err)
	}
	return data, nil
}
