package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/api/v1alpha1"
	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/state/advice"
	"github.com/crmarques/bootwright/internal/state/desired"
)

func newValidateCmd(stdout io.Writer) *cobra.Command {
	return newSyntaxValidationCmd(stdout, syntaxValidationCommand{
		use:   "validate",
		short: "Validate desired-state YAML (offline; contacts nothing)",
		long: "Loads, normalizes, and validates the desired-state YAML offline: it never\n" +
			"runs Ansible and contacts no hosts, BMCs, or clusters, and renders no installer\n" +
			"files. Exit codes for automation: 0 valid, 1 load or validation error, 2 usage\n" +
			"error.",
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
	long    string
	label   string
	rerun   string
	example string
}

func newSyntaxValidationCmd(stdout io.Writer, spec syntaxValidationCommand) *cobra.Command {
	var output string
	var files []string
	cmd := &cobra.Command{
		Use:     spec.use,
		Short:   spec.short,
		Long:    spec.long,
		Args:    cobra.NoArgs,
		Example: spec.example,
	}
	cf := addCommonFlags()
	cmd.Flags().StringArrayVarP(&files, "file", "f", nil, "Bootwright YAML file or directory to validate; may be repeated (default: current context input)")
	addOutputFlag(cmd, &output)
	cmd.RunE = func(_ *cobra.Command, _ []string) error {
		if err := validateOutputFormat(output); err != nil {
			return failErr(2, err)
		}
		state, exclusions, err := loadSyntaxCheckState(cf, files)
		if err != nil {
			if output == outputJSON {
				if encodeErr := writeSyntaxCheckJSON(stdout, state, exclusions, err); encodeErr != nil {
					return failErr(1, encodeErr)
				}
				return silentExit(1)
			}
			p := outputpkg(stdout)
			p.Command(spec.label)
			checks := syntaxDiagnosticChecks(err, spec.rerun, syntaxSourceLabel(files))
			p.Checks(checks)
			p.Summary(cliout.StatusFail, spec.label, checkSummary(len(checks), failedCheckCount(checks)))
			return failf(1, "%s failed: %v", spec.label, err)
		}
		if output == outputJSON {
			return writeSyntaxCheckJSON(stdout, state, exclusions, nil)
		}
		p := outputpkg(stdout)
		p.Command(spec.label)
		p.Section("Objects")
		p.Fields(stateCountFields(state))
		checks := []preflightCheck{okCheck("Desired state", syntaxSourceLabel(files), "loads, normalizes, and validates")}
		checks = append(checks, contextSecretChecks(cf, files, state)...)
		checks = append(checks, environmentSelectionChecks(exclusions)...)
		checks = append(checks, storageAdvisoryChecks(state)...)
		if err := renderCheckResults(stdout, spec.label, checks); err != nil {
			return err
		}
		return nil
	}
	return cmd
}

func contextSecretChecks(cf *commonFlags, files []string, state v1alpha1.State) []preflightCheck {
	if len(files) > 0 || cf.ctx.Name == "" {
		return nil
	}
	checks := declaredSecretContextChecks(cf.ctx.Name, cf.ctx.SecretsDir, state)
	for i := range checks {
		checks[i].Group = "Declared secrets"
	}
	return checks
}

func loadSyntaxCheckState(cf *commonFlags, files []string) (v1alpha1.State, desiredstate.ClusterSelectionExclusions, error) {
	if len(files) > 0 {
		return desiredstate.LoadNormalizeValidateWithExclusions(files)
	}
	return loadDesiredStateWithExclusions(cf)
}

func environmentSelectionChecks(exclusions desiredstate.ClusterSelectionExclusions) []preflightCheck {
	checks := make([]preflightCheck, 0, len(exclusions.ContainerClusters)+len(exclusions.StorageClusters))
	for _, name := range exclusions.ContainerClusters {
		checks = append(checks, warnCheck(
			"Environment selection",
			v1alpha1.KindContainerCluster+"/"+name,
			"loaded but excluded by Environment cluster selection",
			"excluded clusters are not validated and apply never touches them",
			fmt.Sprintf("add %q to Environment spec.containerClusters or remove its YAML", name),
		))
	}
	for _, name := range exclusions.StorageClusters {
		checks = append(checks, warnCheck(
			"Environment selection",
			v1alpha1.KindStorageCluster+"/"+name,
			"loaded but excluded by Environment cluster selection",
			"excluded clusters are not validated and apply never touches them",
			fmt.Sprintf("add %q to Environment spec.storageClusters or remove its YAML", name),
		))
	}
	return checks
}

func storageAdvisoryChecks(state v1alpha1.State) []preflightCheck {
	advisories := advice.StorageAdvisories(state)
	checks := make([]preflightCheck, 0, len(advisories))
	for _, advisory := range advisories {
		if advisory.Severity == advice.SeverityInfo {
			checks = append(checks, infoCheck(advisory.Group, advisory.Object, advisory.Finding))
			continue
		}
		checks = append(checks, warnCheck(
			advisory.Group,
			advisory.Object,
			advisory.Finding,
			advisory.Impact,
			advisory.Remediation,
		))
	}
	return checks
}

type syntaxCheckReport struct {
	OK                        bool                     `json:"ok"`
	ExitCode                  int                      `json:"exitCode"`
	Error                     string                   `json:"error,omitempty"`
	ExcludedContainerClusters []string                 `json:"excludedContainerClusters,omitempty"`
	ExcludedStorageClusters   []string                 `json:"excludedStorageClusters,omitempty"`
	Diagnostics               []Diagnostic             `json:"diagnostics,omitempty"`
	Advisories                []advice.StorageAdvisory `json:"advisories,omitempty"`
	Environments              int                      `json:"environments"`
	Machines                  int                      `json:"machines"`
	MachineImages             int                      `json:"machineImages"`
	MachineInstallProfiles    int                      `json:"machineInstallProfiles"`
	NetworkConfigs            int                      `json:"networkConfigs"`
	InfraProviders            int                      `json:"infraProviders"`
	InfraComponents           int                      `json:"infraComponents"`
	ContainerClusters         int                      `json:"containerClusters"`
	StorageClusters           int                      `json:"storageClusters"`
	StoragePlacementPolicies  int                      `json:"storagePlacementPolicies"`
	StoragePools              int                      `json:"storagePools"`
	StorageFilesystems        int                      `json:"storageFilesystems"`
	StorageObjectGateways     int                      `json:"storageObjectGateways"`
	StorageNFSExports         int                      `json:"storageNFSExports"`
	StorageExports            int                      `json:"storageExports"`
	ClusterAddons             int                      `json:"clusterAddons"`
	Profiles                  int                      `json:"clusterAddonProfiles"`
	ExtensionBindings         int                      `json:"clusterAddonBindings"`
	ProvisioningPlaybooks     int                      `json:"provisioningPlaybooks"`
}

func writeSyntaxCheckJSON(stdout io.Writer, state v1alpha1.State, exclusions desiredstate.ClusterSelectionExclusions, checkErr error) error {
	exitCode := 0
	if checkErr != nil {
		exitCode = 1
	}
	report := syntaxCheckReport{
		OK:                        checkErr == nil,
		ExitCode:                  exitCode,
		ExcludedContainerClusters: exclusions.ContainerClusters,
		ExcludedStorageClusters:   exclusions.StorageClusters,
		Advisories:                advice.StorageAdvisories(state),
		Environments:              len(state.Environments),
		Machines:                  len(state.Machines),
		MachineImages:             len(state.MachineImages),
		MachineInstallProfiles:    len(state.MachineInstallProfiles),
		NetworkConfigs:            len(state.NetworkConfigs),
		InfraProviders:            len(state.InfraProviders),
		InfraComponents:           len(state.InfraComponents),
		ContainerClusters:         len(state.ContainerClusters),
		StorageClusters:           len(state.StorageClusters),
		StoragePlacementPolicies:  len(state.StoragePlacementPolicies),
		StoragePools:              len(state.StoragePools),
		StorageFilesystems:        len(state.StorageFilesystems),
		StorageObjectGateways:     len(state.StorageObjectGateways),
		StorageNFSExports:         len(state.StorageNFSExports),
		StorageExports:            len(state.StorageExports),
		ClusterAddons:             len(state.ClusterAddons),
		Profiles:                  len(state.ClusterAddonProfiles),
		ExtensionBindings:         len(state.ClusterAddonBindings),
		ProvisioningPlaybooks:     len(state.ProvisioningPlaybooks),
	}
	if checkErr != nil {
		report.Error = checkErr.Error()
		report.Diagnostics = diagnosticsFromError(checkErr)
	}
	return cliout.JSON(stdout, report)
}

func syntaxSourceLabel(files []string) string {
	if len(files) > 0 {
		return "input files"
	}
	return "context input"
}

func syntaxDiagnosticChecks(err error, rerun, source string) []preflightCheck {
	diagnostics := diagnosticsFromError(err)
	if len(diagnostics) == 0 {
		return []preflightCheck{failCheck("Desired state", source, err.Error(), "Bootwright cannot render or apply invalid desired state", "fix the named YAML field or file and rerun "+rerun)}
	}
	checks := make([]preflightCheck, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		name := source
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
		{Key: "InfraComponents", Value: fmt.Sprint(len(state.InfraComponents))},
		{Key: "ContainerClusters", Value: fmt.Sprint(len(state.ContainerClusters))},
		{Key: "StorageClusters", Value: fmt.Sprint(len(state.StorageClusters))},
		{Key: "StoragePlacementPolicies", Value: fmt.Sprint(len(state.StoragePlacementPolicies))},
		{Key: "StoragePools", Value: fmt.Sprint(len(state.StoragePools))},
		{Key: "StorageFilesystems", Value: fmt.Sprint(len(state.StorageFilesystems))},
		{Key: "StorageObjectGateways", Value: fmt.Sprint(len(state.StorageObjectGateways))},
		{Key: "StorageNFSExports", Value: fmt.Sprint(len(state.StorageNFSExports))},
		{Key: "StorageExports", Value: fmt.Sprint(len(state.StorageExports))},
		{Key: "ClusterAddons", Value: fmt.Sprint(len(state.ClusterAddons))},
		{Key: "ClusterAddonProfiles", Value: fmt.Sprint(len(state.ClusterAddonProfiles))},
		{Key: "ClusterAddonBindings", Value: fmt.Sprint(len(state.ClusterAddonBindings))},
		{Key: "ProvisioningPlaybooks", Value: fmt.Sprint(len(state.ProvisioningPlaybooks))},
	}
}
