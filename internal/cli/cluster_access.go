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
	"github.com/crmarques/bootwright/internal/runtime/fs"
	"github.com/crmarques/bootwright/internal/state/view"
)

type clusterAccessArtifact struct {
	Path    string `json:"path"`
	Present bool   `json:"present"`
	Detail  string `json:"detail,omitempty"`
}

type clusterAccessSummary struct {
	Name                     string                `json:"name"`
	InstallMode              string                `json:"installMode"`
	InstallMethod            string                `json:"installMethod"`
	KubeconfigPath           string                `json:"kubeconfigPath"`
	KubeContextCommand       string                `json:"kubeContextCommand"`
	APIURL                   string                `json:"apiURL"`
	ConsoleURL               string                `json:"consoleURL"`
	KubeadminUsername        string                `json:"kubeadminUsername"`
	KubeadminPasswordPath    string                `json:"kubeadminPasswordPath"`
	KubeadminPasswordCommand string                `json:"kubeadminPasswordCommand"`
	Kubeconfig               clusterAccessArtifact `json:"kubeconfig"`
	KubeadminPassword        clusterAccessArtifact `json:"kubeadminPassword"`
	Ready                    bool                  `json:"ready"`
}

func printClusterAccess(stdout io.Writer, state v1alpha1.State, result render.Result, ledger workflow.RunLedger) {
	summaries := clusterAccessSummariesForApply(state, result, ledger)
	if len(summaries) == 0 {
		return
	}
	printClusterAccessContinuation(stdout, summaries)
}

func printClusterAccessContinuation(stdout io.Writer, summaries []clusterAccessSummary) {
	p := cliout.NewContinuation(stdout)
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

func clusterAccessSummariesForApply(state v1alpha1.State, result render.Result, ledger workflow.RunLedger) []clusterAccessSummary {
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
	summaries := clusterAccessSummariesFromAssets(state, result.InstallerAssets)
	out := make([]clusterAccessSummary, 0, len(successfulClusters))
	for _, summary := range summaries {
		if successfulClusters[summary.Name] {
			out = append(out, summary)
		}
	}
	return out
}

func clusterAccessSummariesFromAssets(state v1alpha1.State, assets []render.InstallerAsset) []clusterAccessSummary {
	assetsByName := map[string]render.InstallerAsset{}
	for _, asset := range assets {
		assetsByName[asset.ClusterName] = asset
	}
	names := make([]string, 0, len(state.ContainerClusters))
	clustersByName := map[string]v1alpha1.ContainerCluster{}
	for _, cluster := range state.ContainerClusters {
		names = append(names, cluster.Metadata.Name)
		clustersByName[cluster.Metadata.Name] = cluster
	}
	sort.Strings(names)
	baseDomain := clusterAccessBaseDomain(state)
	out := make([]clusterAccessSummary, 0, len(names))
	for _, name := range names {
		cluster := clustersByName[name]
		asset, ok := assetsByName[name]
		if !ok {
			asset = render.InstallerAsset{ClusterName: name}
		}
		clusterSecretsDir := asset.ClusterSecretsDir
		if clusterSecretsDir == "" && asset.WorkDir != "" {
			clusterSecretsDir = filepath.Join(filepath.Dir(asset.WorkDir), "..", "secrets")
		}
		kubeconfigPath := filepath.Join(clusterSecretsDir, "kubeconfig")
		passwordPath := filepath.Join(clusterSecretsDir, "kubeadmin-password")
		kubeconfig := clusterAccessFileStatus(kubeconfigPath)
		password := clusterAccessFileStatus(passwordPath)
		out = append(out, clusterAccessSummary{
			Name:                     name,
			InstallMode:              v1alpha1.InstallMode(cluster),
			InstallMethod:            cluster.Spec.Install.Method,
			KubeconfigPath:           kubeconfigPath,
			KubeContextCommand:       "KUBECONFIG=" + workflow.ShellQuote([]string{kubeconfigPath}),
			APIURL:                   clusterAPIURL(name, baseDomain),
			ConsoleURL:               clusterConsoleURL(name, baseDomain),
			KubeadminUsername:        "kubeadmin",
			KubeadminPasswordPath:    passwordPath,
			KubeadminPasswordCommand: "sudo cat " + workflow.ShellQuote([]string{passwordPath}),
			Kubeconfig:               kubeconfig,
			KubeadminPassword:        password,
			Ready:                    kubeconfig.Present && password.Present,
		})
	}
	return out
}

func clusterAccessFileStatus(path string) clusterAccessArtifact {
	exists, err := safefs.RegularFileExists(path)
	if err != nil {
		return clusterAccessUnavailable(path, err)
	}
	if !exists {
		return clusterAccessUnavailable(path, nil)
	}
	return clusterAccessArtifact{Path: path, Present: true}
}

func clusterAccessUnavailable(path string, err error) clusterAccessArtifact {
	if err == nil {
		return clusterAccessArtifact{Path: path, Present: false, Detail: path + " missing"}
	}
	return clusterAccessArtifact{Path: path, Present: false, Detail: fmt.Sprintf("%v", err)}
}

func clusterAPIURL(name, baseDomain string) string {
	if baseDomain == "" {
		return ""
	}
	return fmt.Sprintf("https://api.%s.%s:6443", name, baseDomain)
}

func clusterConsoleURL(name, baseDomain string) string {
	if baseDomain == "" {
		return ""
	}
	return fmt.Sprintf("https://console-openshift-console.apps.%s.%s", name, baseDomain)
}

func clusterAccessBaseDomain(state v1alpha1.State) string {
	if env := stateview.Environment(state); env != nil {
		return strings.TrimSpace(env.Spec.BaseDomain)
	}
	return ""
}
