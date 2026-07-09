package cli

import (
	"io"

	"github.com/spf13/cobra"

	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/workspace"
)

type scopeCommonFlags struct {
	executable   string
	clusterScope string
	output       string
}

func registerScopeCommonFlags(cmd *cobra.Command, f *scopeCommonFlags, allowClusterScope bool, scopeAction string) {
	registerScopeCommonFlagsWithAnsibleTarget(cmd, f, allowClusterScope, scopeAction, true, "ContainerCluster")
}

func registerScopeCommonFlagsWithAnsibleTarget(cmd *cobra.Command, f *scopeCommonFlags, allowClusterScope bool, scopeAction string, includeAnsible bool, targetKind string) {
	if includeAnsible {
		f.executable = workspace.ResolveAnsiblePlaybook()
	}
	addOutputFlagDryRun(cmd, &f.output)
	if allowClusterScope {
		cmd.Flags().StringVar(&f.clusterScope, "clusters", "", "comma-separated "+targetKind+" names to "+scopeAction+" (default: all)")
	}
}

func scopeTargetKind(scope converge.Scope) string {
	switch scope.Name {
	case "clusters", "infra", "all":
		return "cluster"
	case "storage-cluster":
		return "StorageCluster"
	default:
		return "ContainerCluster"
	}
}

func printBundlePath(stdout io.Writer, bundleDir string) {
	p := cliout.NewContinuation(stdout)
	p.Section("Bundle")
	p.Fields([]cliout.Field{{Key: "ansible bundle", Value: bundleDir}})
}

func scopeAllowsClusterScope(scope converge.Scope, destroyOnly bool) bool {
	switch scope.Name {
	case "clusters", "container-cluster", "storage-cluster", "infra", "add-ons":
		return true
	case "all":
		return !destroyOnly
	default:
		return false
	}
}
