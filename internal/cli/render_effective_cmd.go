package cli

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/api/v1alpha1"
	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func newRenderEffectiveCmd(stdout io.Writer, _ io.Writer) *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "effective",
		Short: "Render normalized effective state",
		Args:  cobra.NoArgs,
		Example: `  # Write normalized desired state with defaults applied
  bootwright render effective

  # Machine-readable output for CI
  bootwright render effective --output json`,
	}
	cf := addCommonFlags()
	addOutputFlag(cmd, &output)
	cmd.RunE = func(_ *cobra.Command, _ []string) error {
		if err := validateOutputFormat(output); err != nil {
			return failErr(2, err)
		}
		state, err := loadDesiredState(cf)
		if err != nil {
			return failErr(1, err)
		}
		result, err := workflow.RenderEffective(cf.ctx.RenderedDir, state)
		if err != nil {
			return failErr(1, err)
		}
		if output == outputJSON {
			return cliout.JSON(stdout, renderEffectiveReport{
				EffectiveStatePath: result.EffectiveStatePath,
				Counts:             stateCountsReport(state),
			})
		}
		p := outputpkg(stdout)
		p.Command("effective-state render")
		p.Section("Rendered artifacts")
		p.Artifacts([]cliout.ArtifactGroup{{Name: "Effective state", Paths: []string{result.EffectiveStatePath}}})
		p.Section("Objects")
		p.Fields(stateCountFields(state))
		return nil
	}
	return cmd
}

type renderEffectiveReport struct {
	EffectiveStatePath string            `json:"effectiveStatePath"`
	Counts             stateObjectCounts `json:"counts"`
}

type stateObjectCounts struct {
	Environments             int `json:"environments"`
	Machines                 int `json:"machines"`
	MachineImages            int `json:"machineImages"`
	MachineInstallProfiles   int `json:"machineInstallProfiles"`
	NetworkConfigs           int `json:"networkConfigs"`
	InfraProviders           int `json:"infraProviders"`
	ContainerClusters        int `json:"containerClusters"`
	StorageClusters          int `json:"storageClusters"`
	StoragePlacementPolicies int `json:"storagePlacementPolicies"`
	StoragePools             int `json:"storagePools"`
	StorageFilesystems       int `json:"storageFilesystems"`
	StorageObjectGateways    int `json:"storageObjectGateways"`
	StorageExports           int `json:"storageExports"`
	ClusterAddons            int `json:"clusterAddons"`
	Profiles                 int `json:"clusterAddonProfiles"`
	ExtensionBindings        int `json:"clusterAddonBindings"`
}

func stateCountsReport(stateCounted v1alpha1.State) stateObjectCounts {
	return stateObjectCounts{
		Environments:             len(stateCounted.Environments),
		Machines:                 len(stateCounted.Machines),
		MachineImages:            len(stateCounted.MachineImages),
		MachineInstallProfiles:   len(stateCounted.MachineInstallProfiles),
		NetworkConfigs:           len(stateCounted.NetworkConfigs),
		InfraProviders:           len(stateCounted.InfraProviders),
		ContainerClusters:        len(stateCounted.ContainerClusters),
		StorageClusters:          len(stateCounted.StorageClusters),
		StoragePlacementPolicies: len(stateCounted.StoragePlacementPolicies),
		StoragePools:             len(stateCounted.StoragePools),
		StorageFilesystems:       len(stateCounted.StorageFilesystems),
		StorageObjectGateways:    len(stateCounted.StorageObjectGateways),
		StorageExports:           len(stateCounted.StorageExports),
		ClusterAddons:            len(stateCounted.ClusterAddons),
		Profiles:                 len(stateCounted.ClusterAddonProfiles),
		ExtensionBindings:        len(stateCounted.ClusterAddonBindings),
	}
}
