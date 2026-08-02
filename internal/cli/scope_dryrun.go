package cli

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func runScopeDryRunJSON(cmd *cobra.Command, stdout io.Writer, cf *commonFlags, flags scopeCommonFlags, scope converge.Scope, action string, state v1alpha1.State, selected []converge.Phase, playbook string, limit string, extraVarPairs []string, artifactsBaseName string, check bool, askBecomePass bool, resolveInstaller bool, limits workflow.ConcurrencyLimits, tasks []workflow.ApplyTask, destroySafety *converge.DryRunDestroySafety, transitions *converge.DryRunTransitions, forks int) error {
	ctx := cf.ctx
	bundleDir, err := resolveBundleDir()
	if err != nil {
		return failErr(1, err)
	}
	report, err := converge.BuildScopeDryRunReport(cmd.Context(), ctx, flags.executable, bundleDir, scope, action, state, selected, playbook, limit, extraVarPairs, artifactsBaseName, check, askBecomePass, resolveInstaller, limits, tasks, destroySafety, transitions, forks)
	if err != nil {
		return failErr(1, err)
	}
	return output.JSON(stdout, report)
}
