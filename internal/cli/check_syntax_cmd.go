package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/api/v1alpha1"
	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/state/advice"
	"github.com/crmarques/bootwright/internal/state/desired"
)

// newValidateCmd exposes a pure offline validation: load + normalize + validate
// the current context input YAML without running Ansible, probing hosts, or
// rendering installer files. Designed to be safe and instant on every edit.
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
			checks := syntaxDiagnosticChecks(err, spec.rerun)
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
		checks := []preflightCheck{okCheck("Desired state", "context input", "loads, normalizes, and validates")}
		checks = append(checks, contextSecretChecks(cf, files, state)...)
		checks = append(checks, environmentSelectionChecks(exclusions)...)
		checks = append(checks, storageBestPracticeChecks(state)...)
		checks = append(checks, stretchPoolInheritanceChecks(state)...)
		if err := renderCheckResults(stdout, spec.label, checks); err != nil {
			return err
		}
		return nil
	}
	return cmd
}

// contextSecretChecks reports declared-secret material status during `validate`
// against the current context, mirroring the secret checks `status` shows so
// validate makes plain which declared secrets are still missing locally.
// Missing material is a WARN, not a failure: offline validation checks
// desired-state YAML, and unsynced secrets are a readiness concern that must not
// fail validate. It returns nothing when validating explicit -f input, which has
// no context secrets directory to inspect.
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

// environmentSelectionChecks warns about loaded clusters that Environment
// spec.containerClusters / spec.storageClusters selection excludes from the
// effective state: excluded clusters are never validated and apply never
// touches them, so a cluster file missing from the selection lists would
// otherwise disappear silently.
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

// storageBestPracticeChecks surfaces the non-fatal Ceph best-practice advisories
// (sub-quorum monitors, a single manager, an unpinned subscription image) as
// warnings. They never fail validate — a single-node or lab cluster is a valid
// authored shape — but make a production cluster's departure from IBM Storage
// Ceph recommendations visible at author time.
func storageBestPracticeChecks(state v1alpha1.State) []preflightCheck {
	advisories := advice.StorageAdvisories(state)
	checks := make([]preflightCheck, 0, len(advisories))
	for _, advisory := range advisories {
		checks = append(checks, warnCheck(
			"Ceph best practice",
			advisory.Object,
			advisory.Finding,
			advisory.Impact,
			advisory.Remediation,
		))
	}
	return checks
}

// stretchPoolInheritanceChecks notices stretch pool inheritance: every
// policy-less pool on a stretch-mode cluster renders with the stretch CRUSH
// rule and size 4 / minSize 2 regardless of its own YAML, so authoring
// topology.stretch on an existing cluster re-rules and resizes those pools
// on the next apply. The pool author reading only the StoragePool file would
// predict the global 3/2 default, hence the one-line notice.
func stretchPoolInheritanceChecks(state v1alpha1.State) []preflightCheck {
	var checks []preflightCheck
	for _, cluster := range state.StorageClusters {
		if cluster.Spec.Ceph == nil || cluster.Spec.Ceph.Topology.Stretch == nil {
			continue
		}
		var pools []string
		for _, pool := range state.StoragePools {
			if pool.Spec.StorageClusterRef.Name == cluster.Metadata.Name && pool.Spec.PlacementPolicyRef.Name == "" {
				pools = append(pools, pool.Metadata.Name)
			}
		}
		if len(pools) == 0 {
			continue
		}
		checks = append(checks, infoCheck(
			"Stretch pools",
			v1alpha1.KindStorageCluster+"/"+cluster.Metadata.Name,
			"policy-less pools inherit the stretch rule and size 4/minSize 2: "+strings.Join(pools, ", "),
		))
	}
	return checks
}

type syntaxCheckReport struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	// Excluded* name loaded clusters that Environment spec.containerClusters /
	// spec.storageClusters selection drops from the effective state; they are
	// not validated and apply never touches them.
	ExcludedContainerClusters []string     `json:"excludedContainerClusters,omitempty"`
	ExcludedStorageClusters   []string     `json:"excludedStorageClusters,omitempty"`
	Diagnostics               []Diagnostic `json:"diagnostics,omitempty"`
	// Advisories are non-fatal Ceph best-practice warnings; their presence does
	// not set OK=false.
	Advisories               []advice.StorageAdvisory `json:"advisories,omitempty"`
	Environments             int                      `json:"environments"`
	Machines                 int                      `json:"machines"`
	MachineImages            int                      `json:"machineImages"`
	MachineInstallProfiles   int                      `json:"machineInstallProfiles"`
	NetworkConfigs           int                      `json:"networkConfigs"`
	InfraProviders           int                      `json:"infraProviders"`
	ContainerClusters        int                      `json:"containerClusters"`
	StorageClusters          int                      `json:"storageClusters"`
	StoragePlacementPolicies int                      `json:"storagePlacementPolicies"`
	StoragePools             int                      `json:"storagePools"`
	StorageFilesystems       int                      `json:"storageFilesystems"`
	StorageObjectGateways    int                      `json:"storageObjectGateways"`
	StorageNFSExports        int                      `json:"storageNFSExports"`
	StorageExports           int                      `json:"storageExports"`
	ClusterAddons            int                      `json:"clusterAddons"`
	Profiles                 int                      `json:"clusterAddonProfiles"`
	ExtensionBindings        int                      `json:"clusterAddonBindings"`
}

func writeSyntaxCheckJSON(stdout io.Writer, state v1alpha1.State, exclusions desiredstate.ClusterSelectionExclusions, checkErr error) error {
	report := syntaxCheckReport{
		OK:                        checkErr == nil,
		ExcludedContainerClusters: exclusions.ContainerClusters,
		ExcludedStorageClusters:   exclusions.StorageClusters,
		Advisories:                advice.StorageAdvisories(state),
		Environments:              len(state.Environments),
		Machines:                  len(state.Machines),
		MachineImages:             len(state.MachineImages),
		MachineInstallProfiles:    len(state.MachineInstallProfiles),
		NetworkConfigs:            len(state.NetworkConfigs),
		InfraProviders:            len(state.InfraProviders),
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
	}
	if checkErr != nil {
		report.Error = checkErr.Error()
		report.Diagnostics = diagnosticsFromError(checkErr)
	}
	return cliout.JSON(stdout, report)
}

func syntaxDiagnosticChecks(err error, rerun string) []preflightCheck {
	diagnostics := diagnosticsFromError(err)
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
		{Key: "StorageNFSExports", Value: fmt.Sprint(len(state.StorageNFSExports))},
		{Key: "StorageExports", Value: fmt.Sprint(len(state.StorageExports))},
		{Key: "ClusterAddons", Value: fmt.Sprint(len(state.ClusterAddons))},
		{Key: "ClusterAddonProfiles", Value: fmt.Sprint(len(state.ClusterAddonProfiles))},
		{Key: "ClusterAddonBindings", Value: fmt.Sprint(len(state.ClusterAddonBindings))},
	}
}
