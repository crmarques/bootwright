package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/workspace"
)

func newScopeDestroyCommand(labels scopeDestroyLabels, options scopeDestroyOptions) *cobra.Command {
	return &cobra.Command{
		Use:     labels.use,
		Short:   labels.short,
		Long:    options.long,
		Args:    cobra.NoArgs,
		Example: options.example,
	}
}

func registerScopeDestroySelectionFlags(cmd *cobra.Command, flags *scopeCommonFlags, scope converge.Scope, stageSelector bool, stage, machinesScope *string) {
	if !stageSelector {
		registerScopeCommonFlags(cmd, flags, scopeAllowsClusterScope(scope, true), "destroy")
		return
	}
	flags.executable = workspace.ResolveAnsiblePlaybook()
	addOutputFlagDryRun(cmd, &flags.output)
	cmd.Flags().StringVar(stage, "stage", "", fmt.Sprintf("stage to destroy: %s (sub-phases %s are apply-only); default: full lifecycle teardown for the selected work set", strings.Join(converge.DestroyStageNames(), "|"), strings.Join(converge.SubPhaseStageNames(), "|")))
	registerStageCompletion(cmd, converge.DestroyStageNames())
	cmd.Flags().StringVar(&flags.clusterScope, "clusters", "", "comma-separated ContainerCluster or StorageCluster names to destroy (default: all); without --stage, tears down the selected clusters and their exclusively owned infrastructure; with --stage infra, the literal artifact-server removes only the generated artifact publication service")
	registerClusterScopeCompletion(cmd, clusterKindAny)
	cmd.Flags().StringVar(machinesScope, "machines", "", flagMachinesDestroyUsage)
	registerMachineScopeCompletion(cmd)
}
