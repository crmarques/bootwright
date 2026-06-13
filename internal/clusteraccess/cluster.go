// Package clusteraccess derives installed-cluster access data — local access
// artifacts, access summaries, selection by name/scope, and kubeconfig
// retrieval — from desired state and on-disk artifacts. It returns plain data;
// presentation lives with the callers.
package clusteraccess

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/host/safefs"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/state/view"
)

type Artifact struct {
	Path    string `json:"path"`
	Present bool   `json:"present"`
	Detail  string `json:"detail,omitempty"`
}

type ClusterSummary struct {
	Name                     string   `json:"name"`
	InstallMode              string   `json:"installMode"`
	InstallMethod            string   `json:"installMethod"`
	Substrate                string   `json:"substrate,omitempty"`
	HostCluster              string   `json:"hostCluster,omitempty"`
	KubeconfigPath           string   `json:"kubeconfigPath"`
	KubeContextCommand       string   `json:"kubeContextCommand"`
	APIURL                   string   `json:"apiURL"`
	ConsoleURL               string   `json:"consoleURL"`
	KubeadminUsername        string   `json:"kubeadminUsername"`
	KubeadminPasswordPath    string   `json:"kubeadminPasswordPath"`
	KubeadminPasswordCommand string   `json:"kubeadminPasswordCommand"`
	Kubeconfig               Artifact `json:"kubeconfig"`
	KubeadminPassword        Artifact `json:"kubeadminPassword"`
	Ready                    bool     `json:"ready"`
}

func ClusterSummariesForApply(state v1alpha1.State, result render.Result, ledger workflow.RunLedger) []ClusterSummary {
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
	summaries := ClusterSummariesFromAssets(state, result.InstallerAssets)
	out := make([]ClusterSummary, 0, len(successfulClusters))
	for _, summary := range summaries {
		if successfulClusters[summary.Name] {
			out = append(out, summary)
		}
	}
	return out
}

func ClusterSummariesFromAssets(state v1alpha1.State, assets []render.InstallerAsset) []ClusterSummary {
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
	out := make([]ClusterSummary, 0, len(names))
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
		kubeconfig := FileStatus(kubeconfigPath)
		password := FileStatus(passwordPath)
		sub := stateview.ContainerClusterSubstrate(state, cluster)
		out = append(out, ClusterSummary{
			Name:                     name,
			InstallMode:              v1alpha1.InstallMode(cluster),
			InstallMethod:            cluster.Spec.Install.Method,
			Substrate:                sub.Provider,
			HostCluster:              sub.Host,
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

func FileStatus(path string) Artifact {
	exists, err := safefs.RegularFileExists(path)
	if err != nil {
		return clusterAccessUnavailable(path, err)
	}
	if !exists {
		return clusterAccessUnavailable(path, nil)
	}
	return Artifact{Path: path, Present: true}
}

func clusterAccessUnavailable(path string, err error) Artifact {
	if err == nil {
		return Artifact{Path: path, Present: false, Detail: path + " missing"}
	}
	return Artifact{Path: path, Present: false, Detail: fmt.Sprintf("%v", err)}
}

func clusterAPIURL(name, baseDomain string) string {
	if baseDomain == "" {
		return ""
	}
	return stateview.ClusterAPIURL(name, baseDomain)
}

func clusterConsoleURL(name, baseDomain string) string {
	if baseDomain == "" {
		return ""
	}
	return stateview.ClusterConsoleURL(name, baseDomain)
}

func clusterAccessBaseDomain(state v1alpha1.State) string {
	if env := stateview.Environment(state); env != nil {
		return strings.TrimSpace(env.Spec.BaseDomain)
	}
	return ""
}

func FilterClusterSummaries(summaries []ClusterSummary, name string) []ClusterSummary {
	if name == "" {
		return summaries
	}
	for _, summary := range summaries {
		if summary.Name == name {
			return []ClusterSummary{summary}
		}
	}
	return nil
}
