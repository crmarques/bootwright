package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/api/v1alpha1"
	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/clusteraccess"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/preflight"
	"github.com/crmarques/bootwright/internal/workspace"
)

func newAddonsCheckCmd(stdout io.Writer) *cobra.Command {
	var (
		clusterScope string
		output       string
	)
	cmd := &cobra.Command{
		Use:   "preflight",
		Short: "Check add-ons prerequisites",
		Args:  cobra.NoArgs,
		Example: `  # Check oc and local kubeconfig material for selected add-ons
  bootwright preflight add-ons

  # Machine-readable output for CI
  bootwright preflight add-ons --output json`,
	}
	cf := addCommonFlags()
	cmd.Flags().StringVar(&clusterScope, "clusters", "", "comma-separated ContainerCluster names to check (default: all)")
	registerClusterScopeCompletion(cmd, clusterKindContainer)
	addOutputFlag(cmd, &output)
	cmd.RunE = func(_ *cobra.Command, _ []string) error {
		if err := validateOutputFormat(output); err != nil {
			return failErr(2, err)
		}
		var p *cliout.Printer
		if output != outputJSON {
			p = cliout.New(stdout)
			p.Command("add-ons preflight")
			p.Section("Prepare")
			p.List([]cliout.Item{{Label: "Load desired state"}})
		}
		state, err := loadDesiredState(cf)
		if err != nil {
			return failErr(1, err)
		}
		sel, err := clusteraccess.Resolve(state, converge.PhaseAddons, clusterScope)
		if err != nil {
			return failErr(1, err)
		}
		results := extensionPreflightChecks(workspace.ControllerClustersDir(cf.ctx.Name), cf.ctx.SecretsDir, sel.RenderState)
		failed := 0
		for _, check := range results {
			if check.Status != cliout.StatusOK {
				failed++
			}
		}
		if output == outputJSON {
			return writePreflightResultJSON(stdout, cf.ctx.Name, "add-ons", preflightCheckResults(results), "")
		}
		p.Checks(results)
		if failed > 0 {
			return failf(1, "add-ons preflight failed")
		}
		p.Summary(cliout.StatusOK, "add-ons preflight", fmt.Sprintf("all %d check(s) passed", len(results)))
		return nil
	}
	return cmd
}

func extensionPreflightChecks(clustersDir, secretsDir string, state v1alpha1.State) []cliout.Check {
	raw := preflight.ExtensionPreflight(clustersDir, state, preflight.ExtensionDeps{
		LookPath: preflight.DefaultDeps.LookPath,
		StatPath: preflight.DefaultDeps.StatPath,
	})
	raw = append(raw, preflight.AddonSecretChecks(state, secretsDir, preflight.DefaultDeps)...)
	return preflightChecksToOutput(raw)
}
