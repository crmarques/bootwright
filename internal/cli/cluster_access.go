package cli

import (
	"io"

	"github.com/crmarques/bootwright/api/v1alpha1"
	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/clusteraccess"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/render"
)

func printClusterAccess(stdout io.Writer, state v1alpha1.State, result render.Result, ledger workflow.RunLedger, contextName, clustersDir string) {
	container := clusteraccess.ClusterSummariesForApply(state, result, ledger)
	storage := clusteraccess.StorageSummariesForApply(state, ledger, clustersDir)
	if len(container) == 0 && len(storage) == 0 {
		return
	}
	p := cliout.NewContinuation(stdout)
	printClusterAccessSections(p, container)
	printStorageAccessSections(p, contextName, clustersDir, storage, false)
}

func printClusterAccessSections(p *cliout.Printer, summaries []clusteraccess.ClusterSummary) {
	if len(summaries) == 0 {
		return
	}
	p.Section("Cluster access")
	for _, summary := range summaries {
		p.List([]cliout.Item{{Label: "cluster " + summary.Name}})
		p.Fields([]cliout.Field{
			{Key: "API", Value: summary.APIURL},
			{Key: "Console", Value: summary.ConsoleURL},
			{Key: "Kubeconfig", Value: summary.KubeconfigPath},
			{Key: "Kube context", Value: summary.KubeContextCommand},
			{Key: "Kubeadmin user", Value: summary.KubeadminUsername},
			{Key: "Password file", Value: summary.KubeadminPasswordPath},
			{Key: "Show password", Value: summary.KubeadminPasswordCommand},
		})
		p.Status(accessArtifactStatus(summary.Kubeconfig), "kubeconfig", accessArtifactDetail(summary.Kubeconfig))
		p.Status(accessArtifactStatus(summary.KubeadminPassword), "kubeadmin password", accessArtifactDetail(summary.KubeadminPassword))
	}
}
