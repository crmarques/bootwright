package cli

import (
	"io"

	"github.com/spf13/cobra"

	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge"
)

// scopeCommonFlags collects the four flags shared by every scope
// subcommand (check / apply / destroy). The pointers are bound by
// registerScopeCommonFlags and read back from the surrounding RunE
// closure once Cobra has populated them.
type scopeCommonFlags struct {
	executable   string
	clusterScope string
	output       string
}

// registerScopeCommonFlags wires the standard flag set onto cmd and
// gates --clusters on whether the scope accepts cluster-scoped filtering
// (i.e. infra / clusters / all-for-check-apply; destroy never accepts
// "all" because AllScope.DestroyPlaybook is empty).
func registerScopeCommonFlags(cmd *cobra.Command, f *scopeCommonFlags, allowClusterScope bool, scopeAction string) {
	registerScopeCommonFlagsWithAnsibleTarget(cmd, f, allowClusterScope, scopeAction, true, "ContainerCluster")
}

func registerScopeCommonFlagsWithAnsibleTarget(cmd *cobra.Command, f *scopeCommonFlags, allowClusterScope bool, scopeAction string, includeAnsible bool, targetKind string) {
	if includeAnsible {
		addAnsiblePlaybookFlag(cmd, &f.executable)
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

// scopeAllowsClusterScope reports whether the --clusters flag is meaningful
// for this command. The "all" scope has no DestroyPlaybook so destroy
// commands exclude it via destroyOnly=true; check/apply include it.
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
