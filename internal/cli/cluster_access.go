package cli

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/state/view"
)

type clusterAccessSummary struct {
	Name                  string
	KubeconfigPath        string
	KubeContextCommand    string
	APIURL                string
	ConsoleURL            string
	KubeadminPasswordPath string
}

func printClusterAccess(stdout io.Writer, state v1alpha1.State, result render.Result, ledger workflow.RunLedger) {
	summaries := clusterAccessSummaries(state, result, ledger)
	if len(summaries) == 0 {
		return
	}
	p := cliout.NewContinuation(stdout)
	p.Section("Cluster access")
	for _, summary := range summaries {
		p.List([]cliout.Item{{Label: "cluster " + summary.Name}})
		p.Fields([]cliout.Field{
			{Key: "Kubeconfig", Value: summary.KubeconfigPath},
			{Key: "Kube context", Value: summary.KubeContextCommand},
			{Key: "API", Value: summary.APIURL},
			{Key: "Console", Value: summary.ConsoleURL},
			{Key: "Credentials", Value: "user kubeadmin; password file " + summary.KubeadminPasswordPath},
		})
	}
}

func clusterAccessSummaries(state v1alpha1.State, result render.Result, ledger workflow.RunLedger) []clusterAccessSummary {
	if ledger.Status != workflow.RunStatusOK {
		return nil
	}
	successfulClusters := map[string]bool{}
	for _, task := range ledger.Tasks {
		if task.Kind == workflow.ApplyTaskKindInstallWait && task.Status == workflow.TaskStatusOK && task.Cluster != "" {
			successfulClusters[task.Cluster] = true
		}
	}
	if len(successfulClusters) == 0 {
		return nil
	}
	assets := map[string]render.InstallerAsset{}
	for _, asset := range result.InstallerAssets {
		assets[asset.ClusterName] = asset
	}
	names := make([]string, 0, len(successfulClusters))
	for _, cluster := range state.ContainerClusters {
		if successfulClusters[cluster.Metadata.Name] {
			names = append(names, cluster.Metadata.Name)
		}
	}
	sort.Strings(names)
	out := make([]clusterAccessSummary, 0, len(names))
	for _, name := range names {
		asset, ok := assets[name]
		if !ok {
			continue
		}
		baseDomain := clusterAccessBaseDomain(state)
		if baseDomain == "" {
			continue
		}
		kubeconfigPath := filepath.Join(asset.WorkDir, "auth", "kubeconfig")
		out = append(out, clusterAccessSummary{
			Name:                  name,
			KubeconfigPath:        kubeconfigPath,
			KubeContextCommand:    "KUBECONFIG=" + workflow.ShellQuote([]string{kubeconfigPath}),
			APIURL:                fmt.Sprintf("https://api.%s.%s:6443", name, baseDomain),
			ConsoleURL:            fmt.Sprintf("https://console-openshift-console.apps.%s.%s", name, baseDomain),
			KubeadminPasswordPath: filepath.Join(asset.WorkDir, "auth", "kubeadmin-password"),
		})
	}
	return out
}

func clusterAccessBaseDomain(state v1alpha1.State) string {
	if env := stateview.Environment(state); env != nil {
		return strings.TrimSpace(env.Spec.BaseDomain)
	}
	return ""
}
