package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/api/v1alpha1"
	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge/checks"
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
		checks := extensionPreflightChecks(controllerClustersDir(cf.ctx.Name), state)
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

func extensionPreflightChecks(clustersDir string, state v1alpha1.State) []cliout.Check {
	raw := checks.ExtensionPreflight(clustersDir, state, checks.ExtensionDeps{
		LookPath: defaultPreflightDeps.lookPath,
		StatPath: defaultPreflightDeps.statPath,
	})
	out := make([]cliout.Check, 0, len(raw))
	for _, check := range raw {
		out = append(out, cliout.Check{
			Group:       check.Group,
			Name:        check.Name,
			Status:      cliout.Status(check.Status),
			Evidence:    check.Evidence,
			Impact:      check.Impact,
			Remediation: check.Remediation,
		})
	}
	return out
}
