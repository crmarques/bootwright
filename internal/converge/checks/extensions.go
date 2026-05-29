package checks

import (
	"os"
	"path/filepath"

	"github.com/crmarques/bootwright/api/v1alpha1"
	extensionplan "github.com/crmarques/bootwright/internal/extensions/plan"
)

type Status string

const (
	StatusOK   Status = "OK"
	StatusFail Status = "FAIL"
)

type Check struct {
	Group       string
	Name        string
	Status      Status
	Evidence    string
	Impact      string
	Remediation string
}

type ExtensionDeps struct {
	LookPath func(name string, extraDirs []string) (string, error)
	StatPath func(path string) (os.FileInfo, error)
}

func ExtensionPreflight(clustersDir string, state v1alpha1.State, deps ExtensionDeps) []Check {
	checks := []Check{
		binaryCheck("Extension tools", "oc", nil, "install oc on PATH", deps),
	}
	plans, err := extensionplan.BindingPlans(state)
	if err != nil {
		return append(checks, failCheck("Extension plan", "extension expansion", err.Error(), "Extension bindings cannot be expanded", "fix ClusterExtensionSet and ClusterExtensionBinding references"))
	}
	if len(plans) == 0 {
		return append(checks, Check{Group: "Extension plan", Name: "extensions", Status: StatusOK, Evidence: "no ClusterExtensionBinding resources selected"})
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
			checks = append(checks, failCheck("Cluster access", plan.Cluster+" kubeconfig", path+" missing", "Extensions need the installed cluster kubeconfig", "run bootwright apply cluster --yes before applying extensions"))
		case info.IsDir():
			checks = append(checks, failCheck("Cluster access", plan.Cluster+" kubeconfig", path+" is a directory", "Extensions need a kubeconfig file", "replace "+path+" with the cluster kubeconfig"))
		default:
			checks = append(checks, okCheck("Cluster access", plan.Cluster+" kubeconfig", path))
		}
	}
	return checks
}

func binaryCheck(group, name string, extraDirs []string, remediation string, deps ExtensionDeps) Check {
	path, err := deps.LookPath(name, extraDirs)
	if err != nil {
		if remediation == "" {
			remediation = "install " + name + " on PATH"
		}
		return failCheck(group, name, "not found", "Required command is unavailable to this workflow", remediation)
	}
	return okCheck(group, name, path)
}

func okCheck(group, name, evidence string) Check {
	return Check{
		Group:    group,
		Name:     name,
		Status:   StatusOK,
		Evidence: evidence,
	}
}

func failCheck(group, name, evidence, impact, remediation string) Check {
	return Check{
		Group:       group,
		Name:        name,
		Status:      StatusFail,
		Evidence:    evidence,
		Impact:      impact,
		Remediation: remediation,
	}
}
