package cli

import (
	"io"

	"github.com/spf13/cobra"

	cliout "github.com/crmarques/bootwright/internal/cli/output"
)

// Paths and Ansible --limit groups shared across scope command builders.
// Centralised so a single edit reaches all three (check/apply/destroy).
const (
	preflightPlaybookPath = "bootwright.core.check_preflight"
	// infraAnsibleLimit pins the inventory groups `apply --stage infra` and
	// `check infra` target. `bootwright_ocp_hosts` is included so
	// bastion-side external_validate can run in every context input set,
	// including bare-metal/all-external shapes like test 002 where the
	// other remote groups would otherwise be empty and ansible would abort
	// with "no hosts to target".
	infraAnsibleLimit    = "bootwright_provider_hosts:bootwright_infra_component_hosts:bootwright_infra_hosts:bootwright_ocp_hosts"
	clustersAnsibleLimit = "bootwright_infra_hosts:bootwright_ocp_hosts:bootwright_boot_hosts"
	clusterAnsibleLimit  = "bootwright_ocp_hosts:bootwright_boot_hosts"
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
// gates --scope on whether the scope accepts cluster-scoped filtering
// (i.e. infra / clusters / all-for-check-apply; destroy never accepts
// "all" because allScope.destroyPlaybook is empty).
func registerScopeCommonFlags(cmd *cobra.Command, f *scopeCommonFlags, allowClusterScope bool, scopeAction string) {
	registerScopeCommonFlagsWithAnsibleTarget(cmd, f, allowClusterScope, scopeAction, true, "ContainerCluster")
}

func registerScopeCommonFlagsWithAnsibleTarget(cmd *cobra.Command, f *scopeCommonFlags, allowClusterScope bool, scopeAction string, includeAnsible bool, targetKind string) {
	f.output = outputText
	if includeAnsible {
		cmd.Flags().StringVar(&f.executable, "ansible-playbook", resolveAnsiblePlaybook(), "ansible-playbook executable to run (defaults to the bootwright-managed venv when present)")
	}
	cmd.Flags().StringVar(&f.output, "output", f.output, "output format: text|json (json is supported for --dry-run)")
	if allowClusterScope {
		scopeUsage := "comma-separated " + targetKind + " names to " + scopeAction
		if targetKind == "ContainerCluster" {
			scopeUsage += " (restricts the matching ClusterInstall/Provider sets)"
		}
		cmd.Flags().StringVar(&f.clusterScope, "scope", "", scopeUsage)
	}
}

func scopeTargetKind(scope scopeSpec) string {
	switch scope.name {
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

// scopeAllowsClusterScope reports whether the --scope flag is meaningful
// for this command. The "all" scope has no destroyPlaybook so destroy
// commands exclude it via destroyOnly=true; check/apply include it.
func scopeAllowsClusterScope(scope scopeSpec, destroyOnly bool) bool {
	switch scope.name {
	case "clusters", "container-cluster", "storage-cluster", "infra", "addons":
		return true
	case "all":
		return !destroyOnly
	default:
		return false
	}
}

func ansibleLimitForScope(name string) string {
	switch name {
	case "infra":
		return infraAnsibleLimit
	case "clusters":
		return clustersAnsibleLimit
	case "container-cluster":
		return clusterAnsibleLimit
	default:
		return ""
	}
}
