package cli

import (
	"io"
	"strings"

	"github.com/spf13/cobra"

	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge"
)

// registerFlagCompletion wires shell completion for a command flag to a fixed
// value set. Callers pass the values from internal/converge's stage accessors
// (ApplyStageNames/DestroyStageNames), so completion, validation, and the
// error/help text all derive from one source and never drift. --stage and
// --through share the same vocabulary through this one helper.
func registerFlagCompletion(cmd *cobra.Command, flag string, values []string) {
	_ = cmd.RegisterFlagCompletionFunc(flag, func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return values, cobra.ShellCompDirectiveNoFileComp
	})
}

// registerStageCompletion is the --stage shorthand for registerFlagCompletion.
func registerStageCompletion(cmd *cobra.Command, values []string) {
	registerFlagCompletion(cmd, "stage", values)
}

// printStageScopeNotices annotates a scoped dry-run/plan so a narrow --stage
// selection is not misread as the full graph: it lists the phases the plan
// leaves out and, when the selection skips earlier phases, warns that those are
// assumed to have completed in a prior apply (a surgical rerun). Silent for the
// full graph.
func printStageScopeNotices(stdout io.Writer, scope converge.Scope) {
	omitted, assumedPrior := converge.StageScopeOmissions(scope)
	if len(omitted) == 0 {
		return
	}
	c := cliout.NewContinuation(stdout)
	c.Status(cliout.StatusOK, "scope", "phases not in this plan: "+strings.Join(omitted, ", "))
	if len(assumedPrior) > 0 {
		c.Warning("scope", "assumes a prior apply completed: "+strings.Join(assumedPrior, ", "))
	}
}
