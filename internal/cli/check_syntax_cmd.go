package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/api/v1alpha1"
	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/desiredstate"
)

// newCheckSyntaxCmd exposes a pure-syntax check: load + normalize +
// validate the current context input YAML without running Ansible, probing hosts,
// or rendering installer files. Designed to be safe and instant on
// every edit.
func newCheckSyntaxCmd(stdout io.Writer) *cobra.Command {
	output := outputText
	cmd := &cobra.Command{
		Use:   "syntax",
		Short: "Validate context input YAML offline (no Ansible, no host probes)",
		Args:  cobra.NoArgs,
		Example: `  # Validate the current context
  bootwright check syntax

  # Machine-readable output for CI
  bootwright check syntax --output json`,
	}
	cf := addCommonFlags()
	cmd.Flags().StringVar(&output, "output", output, "output format: text|json")
	cmd.RunE = func(_ *cobra.Command, _ []string) error {
		if err := validateOutputFormat(output); err != nil {
			return failErr(2, err)
		}
		state, err := loadDesiredState(cf)
		if err != nil {
			if output == outputJSON {
				if encodeErr := writeSyntaxCheckJSON(stdout, state, err); encodeErr != nil {
					return failErr(1, encodeErr)
				}
				return silentExit(1)
			}
			p := outputpkg(stdout)
			p.Command("syntax check")
			checks := syntaxDiagnosticChecks(err)
			p.Checks(checks)
			p.Summary(cliout.StatusFail, "syntax check", checkSummary(len(checks), failedCheckCount(checks)))
			return failf(1, "syntax check failed: %v", err)
		}
		if output == outputJSON {
			return writeSyntaxCheckJSON(stdout, state, nil)
		}
		p := outputpkg(stdout)
		p.Command("syntax check")
		p.Section("Objects")
		p.Fields(stateCountFields(state))
		if err := renderCheckResults(stdout, "syntax check", []preflightCheck{
			okCheck("Desired state", "context input", "loads, normalizes, and validates"),
		}); err != nil {
			return err
		}
		return nil
	}
	return cmd
}

type syntaxCheckReport struct {
	OK                bool                      `json:"ok"`
	Error             string                    `json:"error,omitempty"`
	Diagnostics       []desiredstate.Diagnostic `json:"diagnostics,omitempty"`
	Environments      int                       `json:"environments"`
	Hosts             int                       `json:"hosts"`
	NetworkConfigs    int                       `json:"networkConfigs"`
	InfraProviders    int                       `json:"infraProviders"`
	ClusterInfras     int                       `json:"clusterInfras"`
	ContainerClusters int                       `json:"containerClusters"`
}

func writeSyntaxCheckJSON(stdout io.Writer, state v1alpha1.State, checkErr error) error {
	report := syntaxCheckReport{
		OK:                checkErr == nil,
		Environments:      len(state.Environments),
		Hosts:             len(state.Hosts),
		NetworkConfigs:    len(state.NetworkConfigs),
		InfraProviders:    len(state.InfraProviders),
		ClusterInfras:     len(state.ClusterInfras),
		ContainerClusters: len(state.ContainerClusters),
	}
	if checkErr != nil {
		report.Error = checkErr.Error()
		report.Diagnostics = desiredstate.Diagnostics(checkErr)
	}
	return cliout.JSON(stdout, report)
}

func syntaxDiagnosticChecks(err error) []preflightCheck {
	diagnostics := desiredstate.Diagnostics(err)
	if len(diagnostics) == 0 {
		return []preflightCheck{failCheck("Desired state", "context input", err.Error(), "Bootwright cannot render or apply invalid desired state", "fix the named YAML field or file and rerun bootwright check syntax")}
	}
	checks := make([]preflightCheck, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		name := "context input"
		if diagnostic.Field != "" {
			name = diagnostic.Field
		}
		if diagnostic.Object != "" {
			if diagnostic.Field == "" {
				name = diagnostic.Object
			} else {
				name = diagnostic.Object + " " + diagnostic.Field
			}
		}
		checks = append(checks, failCheck(
			"Desired state",
			name,
			diagnostic.Message,
			"Bootwright cannot render or apply invalid desired state",
			diagnostic.Remediation,
		))
	}
	return checks
}

func stateCountFields(state v1alpha1.State) []cliout.Field {
	return []cliout.Field{
		{Key: "Environments", Value: fmt.Sprint(len(state.Environments))},
		{Key: "Hosts", Value: fmt.Sprint(len(state.Hosts))},
		{Key: "NetworkConfigs", Value: fmt.Sprint(len(state.NetworkConfigs))},
		{Key: "InfraProviders", Value: fmt.Sprint(len(state.InfraProviders))},
		{Key: "ClusterInfras", Value: fmt.Sprint(len(state.ClusterInfras))},
		{Key: "ContainerClusters", Value: fmt.Sprint(len(state.ContainerClusters))},
	}
}
