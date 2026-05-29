package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/api/v1alpha1"
	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/clusterextensions"
	"github.com/crmarques/bootwright/internal/provisioning/render"
)

func newExtensionsCheckCmd(stdout io.Writer) *cobra.Command {
	var clusterScope string
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Check extensions prerequisites",
		Args:  cobra.NoArgs,
		Example: `  # Check oc and local kubeconfig material for selected extensions
  bootwright check extensions`,
	}
	cf := addCommonFlags()
	cmd.Flags().StringVar(&clusterScope, "scope", "", "comma-separated ContainerCluster names to check")
	cmd.RunE = func(_ *cobra.Command, _ []string) error {
		p := cliout.New(stdout)
		p.Command("extensions check")
		p.Section("Prepare")
		p.List([]cliout.Item{{Label: "Load desired state"}})
		state, err := loadDesiredState(cf)
		if err != nil {
			return failErr(1, err)
		}
		state, err = scopeState(state, extensionsScope.name, clusterScope)
		if err != nil {
			return failErr(1, err)
		}
		checks := extensionPreflightChecks(controllerRuntimeDir(cf.ctx.Name), state)
		p.Checks(checks)
		for _, check := range checks {
			if check.Status != cliout.StatusOK {
				return failf(1, "extensions check failed")
			}
		}
		p.Summary(cliout.StatusOK, "extensions check", fmt.Sprintf("all %d check(s) passed", len(checks)))
		return nil
	}
	return cmd
}

func extensionPreflightChecks(runtimeDir string, state v1alpha1.State) []cliout.Check {
	checks := []cliout.Check{
		binaryCheck("Extension tools", "oc", nil, "install oc on PATH", defaultPreflightDeps),
	}
	plans, err := clusterextensions.BindingPlans(state)
	if err != nil {
		return append(checks, failCheck("Extension plan", "extension expansion", err.Error(), "Extension bindings cannot be expanded", "fix ClusterExtensionSet and ClusterExtensionBinding references"))
	}
	if len(plans) == 0 {
		return append(checks, cliout.Check{Group: "Extension plan", Name: "extensions", Status: cliout.StatusOK, Evidence: "no ClusterExtensionBinding resources selected"})
	}
	seenClusters := map[string]bool{}
	for _, plan := range plans {
		if seenClusters[plan.Cluster] {
			continue
		}
		seenClusters[plan.Cluster] = true
		path := filepath.Join(runtimeDir, render.RuntimeRelativeDir, plan.Cluster, "auth", "kubeconfig")
		info, err := os.Stat(path)
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
