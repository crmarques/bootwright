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
		output       = outputText
	)
	cmd := &cobra.Command{
		Use:   "preflight",
		Short: "Check addons prerequisites",
		Args:  cobra.NoArgs,
		Example: `  # Check oc and local kubeconfig material for selected addons
  bootwright preflight addons

  # Machine-readable output for CI
  bootwright preflight addons --output json`,
	}
	cf := addCommonFlags()
	cmd.Flags().StringVar(&clusterScope, "clusters", "", "comma-separated ContainerCluster names to check")
	cmd.Flags().StringVar(&output, "output", output, "output format: text|json")
	cmd.RunE = func(_ *cobra.Command, _ []string) error {
		if err := validateOutputFormat(output); err != nil {
			return failErr(2, err)
		}
		var p *cliout.Printer
		if output != outputJSON {
			p = cliout.New(stdout)
			p.Command("addons preflight")
			p.Section("Prepare")
			p.List([]cliout.Item{{Label: "Load desired state"}})
		}
		state, err := loadDesiredState(cf)
		if err != nil {
			return failErr(1, err)
		}
		state, err = clusteraccess.ScopeState(state, converge.AddonsScope.Name, clusterScope)
		if err != nil {
			return failErr(1, err)
		}
		results := extensionPreflightChecks(workspace.ControllerClustersDir(cf.ctx.Name), state)
		failed := 0
		for _, check := range results {
			if check.Status != cliout.StatusOK {
				failed++
			}
		}
		if output == outputJSON {
			if err := writeAddonsCheckJSON(stdout, results, failed == 0); err != nil {
				return failErr(1, err)
			}
			if failed > 0 {
				return silentExit(1)
			}
			return nil
		}
		p.Checks(results)
		if failed > 0 {
			return failf(1, "addons preflight failed")
		}
		p.Summary(cliout.StatusOK, "addons preflight", fmt.Sprintf("all %d check(s) passed", len(results)))
		return nil
	}
	return cmd
}

type addonsCheckResult struct {
	Group       string `json:"group,omitempty"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	Evidence    string `json:"evidence,omitempty"`
	Impact      string `json:"impact,omitempty"`
	Remediation string `json:"remediation,omitempty"`
}

type addonsCheckReport struct {
	OK     bool                `json:"ok"`
	Checks []addonsCheckResult `json:"checks"`
}

func writeAddonsCheckJSON(stdout io.Writer, checks []cliout.Check, ok bool) error {
	report := addonsCheckReport{OK: ok, Checks: make([]addonsCheckResult, 0, len(checks))}
	for _, check := range checks {
		report.Checks = append(report.Checks, addonsCheckResult{
			Group:       check.Group,
			Name:        check.Name,
			Status:      string(check.Status),
			Evidence:    check.Evidence,
			Impact:      check.Impact,
			Remediation: check.Remediation,
		})
	}
	return cliout.JSON(stdout, report)
}

func extensionPreflightChecks(clustersDir string, state v1alpha1.State) []cliout.Check {
	raw := preflight.ExtensionPreflight(clustersDir, state, preflight.ExtensionDeps{
		LookPath: preflight.DefaultDeps.LookPath,
		StatPath: preflight.DefaultDeps.StatPath,
	})
	return preflightChecksToOutput(raw)
}
