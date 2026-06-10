package preflight

import (
	"os"
	"path/filepath"

	"github.com/crmarques/bootwright/api/v1alpha1"
	extensionplan "github.com/crmarques/bootwright/internal/addons/plan"
)

type ExtensionDeps struct {
	LookPath func(name string, extraDirs []string) (string, error)
	StatPath func(path string) (os.FileInfo, error)
}

func ExtensionPreflight(clustersDir string, state v1alpha1.State, deps ExtensionDeps) []Check {
	checks := []Check{
		binaryCheck("Addon tools", "oc", nil, "install oc on PATH", Deps{LookPath: deps.LookPath}),
	}
	plans, err := extensionplan.BindingPlans(state)
	if err != nil {
		return append(checks, failCheck("Addon plan", "addon expansion", err.Error(), "Addon bindings cannot be expanded", "fix ClusterAddonProfile and ClusterAddonBinding references"))
	}
	if len(plans) == 0 {
		return append(checks, Check{Group: "Addon plan", Name: "addons", Status: StatusOK, Evidence: "no ClusterAddonBinding resources selected"})
	}
	seenClusters := map[string]bool{}
	for _, plan := range plans {
		if seenClusters[plan.Cluster] {
			continue
		}
		seenClusters[plan.Cluster] = true
		path := filepath.Join(clustersDir, plan.Cluster, "secrets", "kubeconfig")
		info, err := deps.StatPath(path)
		switch {
		case err != nil:
			checks = append(checks, failCheck("Cluster access", plan.Cluster+" kubeconfig", path+" missing", "Addons need the installed cluster kubeconfig", "run bootwright apply --stage clusters --clusters "+plan.Cluster+" --yes before applying addons"))
		case info.IsDir():
			checks = append(checks, failCheck("Cluster access", plan.Cluster+" kubeconfig", path+" is a directory", "Addons need a kubeconfig file", "replace "+path+" with the cluster kubeconfig"))
		default:
			checks = append(checks, okCheck("Cluster access", plan.Cluster+" kubeconfig", path))
		}
	}
	return checks
}
