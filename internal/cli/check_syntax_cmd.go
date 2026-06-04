package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/api/v1alpha1"
	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/state/desired"
)

// newCheckSyntaxCmd exposes a pure-syntax check: load + normalize +
// validate the current context input YAML without running Ansible, probing hosts,
// or rendering installer files. Designed to be safe and instant on
// every edit.
func newCheckSyntaxCmd(stdout io.Writer) *cobra.Command {
	return newSyntaxValidationCmd(stdout, syntaxValidationCommand{
		use:   "syntax",
		short: "Validate context input YAML offline (no Ansible, no host probes)",
		label: "syntax check",
		rerun: "bootwright check syntax",
		example: `  # Validate the current context
  bootwright check syntax

  # Validate an input directory before importing a context
  bootwright check syntax -f ./lab-input

  # Machine-readable output for CI
  bootwright check syntax --output json`,
	})
}

func newValidateCmd(stdout io.Writer) *cobra.Command {
	return newSyntaxValidationCmd(stdout, syntaxValidationCommand{
		use:   "validate",
		short: "Validate desired-state YAML offline",
		label: "validate",
		rerun: "bootwright validate",
		example: `  # Validate the current context
  bootwright validate

  # Validate an input directory before importing a context
  bootwright validate -f ./lab-input

  # Machine-readable output for CI
  bootwright validate --output json`,
	})
}

type syntaxValidationCommand struct {
	use     string
	short   string
	label   string
	rerun   string
	example string
}

func newSyntaxValidationCmd(stdout io.Writer, spec syntaxValidationCommand) *cobra.Command {
	output := outputText
	var files []string
	cmd := &cobra.Command{
		Use:     spec.use,
		Short:   spec.short,
		Args:    cobra.NoArgs,
		Example: spec.example,
	}
	cf := addCommonFlags()
	cmd.Flags().StringArrayVarP(&files, "file", "f", nil, "Bootwright YAML file or directory to validate before context import; may be repeated")
	cmd.Flags().StringVar(&output, "output", output, "output format: text|json")
	cmd.RunE = func(_ *cobra.Command, _ []string) error {
		if err := validateOutputFormat(output); err != nil {
			return failErr(2, err)
		}
		state, err := loadSyntaxCheckState(cf, files)
		if err != nil {
			if output == outputJSON {
				if encodeErr := writeSyntaxCheckJSON(stdout, state, err); encodeErr != nil {
					return failErr(1, encodeErr)
				}
				return silentExit(1)
			}
			p := outputpkg(stdout)
			p.Command(spec.label)
			checks := syntaxDiagnosticChecks(err, spec.rerun)
			p.Checks(checks)
			p.Summary(cliout.StatusFail, spec.label, checkSummary(len(checks), failedCheckCount(checks)))
			return failf(1, "%s failed: %v", spec.label, err)
		}
		if output == outputJSON {
			return writeSyntaxCheckJSON(stdout, state, nil)
		}
		p := outputpkg(stdout)
		p.Command(spec.label)
		p.Section("Objects")
		p.Fields(stateCountFields(state))
		if err := renderCheckResults(stdout, spec.label, []preflightCheck{
			okCheck("Desired state", "context input", "loads, normalizes, and validates"),
		}); err != nil {
			return err
		}
		return nil
	}
	return cmd
}

func loadSyntaxCheckState(cf *commonFlags, files []string) (v1alpha1.State, error) {
	if len(files) > 0 {
		return desiredstate.LoadNormalizeValidateInputFiles(files)
	}
	return loadDesiredState(cf)
}

type syntaxCheckReport struct {
	OK                       bool                      `json:"ok"`
	Error                    string                    `json:"error,omitempty"`
	Diagnostics              []desiredstate.Diagnostic `json:"diagnostics,omitempty"`
	Environments             int                       `json:"environments"`
	Machines                 int                       `json:"machines"`
	MachineImages            int                       `json:"machineImages"`
	MachineInstallProfiles   int                       `json:"machineInstallProfiles"`
	NetworkConfigs           int                       `json:"networkConfigs"`
	InfraProviders           int                       `json:"infraProviders"`
	ContainerClusters        int                       `json:"containerClusters"`
	StorageClusters          int                       `json:"storageClusters"`
	StoragePlacementPolicies int                       `json:"storagePlacementPolicies"`
	StoragePools             int                       `json:"storagePools"`
	StorageFilesystems       int                       `json:"storageFilesystems"`
	StorageObjectGateways    int                       `json:"storageObjectGateways"`
	StorageExports           int                       `json:"storageExports"`
	ClusterAddons            int                       `json:"clusterAddons"`
	Profiles                 int                       `json:"clusterAddonProfiles"`
	ExtensionBindings        int                       `json:"clusterAddonBindings"`
}

func writeSyntaxCheckJSON(stdout io.Writer, state v1alpha1.State, checkErr error) error {
	report := syntaxCheckReport{
		OK:                       checkErr == nil,
		Environments:             len(state.Environments),
		Machines:                 len(state.Machines),
		MachineImages:            len(state.MachineImages),
		MachineInstallProfiles:   len(state.MachineInstallProfiles),
		NetworkConfigs:           len(state.NetworkConfigs),
		InfraProviders:           len(state.InfraProviders),
		ContainerClusters:        len(state.ContainerClusters),
		StorageClusters:          len(state.StorageClusters),
		StoragePlacementPolicies: len(state.StoragePlacementPolicies),
		StoragePools:             len(state.StoragePools),
		StorageFilesystems:       len(state.StorageFilesystems),
		StorageObjectGateways:    len(state.StorageObjectGateways),
		StorageExports:           len(state.StorageExports),
		ClusterAddons:            len(state.ClusterAddons),
		Profiles:                 len(state.ClusterAddonProfiles),
		ExtensionBindings:        len(state.ClusterAddonBindings),
	}
	if checkErr != nil {
		report.Error = checkErr.Error()
		report.Diagnostics = desiredstate.Diagnostics(checkErr)
	}
	return cliout.JSON(stdout, report)
}

func syntaxDiagnosticChecks(err error, rerun string) []preflightCheck {
	diagnostics := desiredstate.Diagnostics(err)
	if len(diagnostics) == 0 {
		return []preflightCheck{failCheck("Desired state", "context input", err.Error(), "Bootwright cannot render or apply invalid desired state", "fix the named YAML field or file and rerun "+rerun)}
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
		{Key: "Machines", Value: fmt.Sprint(len(state.Machines))},
		{Key: "MachineImages", Value: fmt.Sprint(len(state.MachineImages))},
		{Key: "MachineInstallProfiles", Value: fmt.Sprint(len(state.MachineInstallProfiles))},
		{Key: "NetworkConfigs", Value: fmt.Sprint(len(state.NetworkConfigs))},
		{Key: "InfraProviders", Value: fmt.Sprint(len(state.InfraProviders))},
		{Key: "ContainerClusters", Value: fmt.Sprint(len(state.ContainerClusters))},
		{Key: "StorageClusters", Value: fmt.Sprint(len(state.StorageClusters))},
		{Key: "StoragePlacementPolicies", Value: fmt.Sprint(len(state.StoragePlacementPolicies))},
		{Key: "StoragePools", Value: fmt.Sprint(len(state.StoragePools))},
		{Key: "StorageFilesystems", Value: fmt.Sprint(len(state.StorageFilesystems))},
		{Key: "StorageObjectGateways", Value: fmt.Sprint(len(state.StorageObjectGateways))},
		{Key: "StorageExports", Value: fmt.Sprint(len(state.StorageExports))},
		{Key: "ClusterAddons", Value: fmt.Sprint(len(state.ClusterAddons))},
		{Key: "ClusterAddonProfiles", Value: fmt.Sprint(len(state.ClusterAddonProfiles))},
		{Key: "ClusterAddonBindings", Value: fmt.Sprint(len(state.ClusterAddonBindings))},
	}
}
